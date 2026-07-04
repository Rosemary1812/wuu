package tools

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// ---------------------------------------------------------------------------
// read_file
// ---------------------------------------------------------------------------

type ReadFileTool struct{ env *Env }

func NewReadFileTool(env *Env) *ReadFileTool { return &ReadFileTool{env: env} }

func (t *ReadFileTool) Name() string            { return "read_file" }
func (t *ReadFileTool) IsReadOnly() bool        { return true }
func (t *ReadFileTool) IsConcurrencySafe() bool { return true }

func (t *ReadFileTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "read_file",
		Description: "Read a workspace file with line numbers. Use offset/limit, range, symbol, and context_lines for focused reads. Results include file_sha, workspace_revision, omitted ranges, and follow-up suggestions. Use list_files for directories.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path relative to workspace root, or a supported artifact path.",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "1-based line number to start reading from. Default 1.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Max lines to return. Omit to read the whole file when it fits size limits.",
				},
				"range": map[string]any{
					"type":        "object",
					"description": "Inclusive line range to read.",
					"properties": map[string]any{
						"start_line": map[string]any{
							"type":        "integer",
							"description": "1-based first line to read.",
						},
						"end_line": map[string]any{
							"type":        "integer",
							"description": "1-based inclusive last line to read.",
						},
					},
					"required": []string{"start_line", "end_line"},
				},
				"symbol": map[string]any{
					"type":        "object",
					"description": "Read a conservative definition range for a named Go, TypeScript/JavaScript, or Python symbol.",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "Function, class, type, interface, variable, or constant name to locate.",
						},
						"kind": map[string]any{
							"type":        "string",
							"description": "Optional expected kind such as function, method, class, type, interface, variable, or constant.",
						},
					},
					"required": []string{"name"},
				},
				"context_lines": map[string]any{
					"type":        "integer",
					"description": "Surrounding lines to include with a focused read. Max 200.",
				},
			},
			"required": []string{"path"},
		},
	}
}

func (t *ReadFileTool) ValidateInput(argsJSON string) error {
	var args struct {
		Path string `json:"path"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	if strings.TrimSpace(args.Path) == "" {
		return errors.New("read_file requires path")
	}
	return nil
}

func (t *ReadFileTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	type lineRangeArgs struct {
		StartLine int `json:"start_line"`
		EndLine   int `json:"end_line"`
	}
	type symbolArgs struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	var args struct {
		Path         string         `json:"path"`
		Offset       int            `json:"offset"`
		Limit        *int           `json:"limit"`
		Range        *lineRangeArgs `json:"range"`
		Symbol       *symbolArgs    `json:"symbol"`
		ContextLines *int           `json:"context_lines"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Path) == "" {
		return "", errors.New("read_file requires path")
	}
	contextLines := 0
	if args.ContextLines != nil {
		if *args.ContextLines < 0 {
			return "", errors.New("read_file context_lines must be non-negative")
		}
		if *args.ContextLines > maxReadFileContextLines {
			return "", fmt.Errorf("read_file context_lines must be <= %d", maxReadFileContextLines)
		}
		contextLines = *args.ContextLines
	}

	resolved, displayPath, managedArtifact, err := t.env.ResolveReadPath(args.Path)
	if err != nil {
		return "", err
	}
	if reason, ok := sensitivePathReason(displayPath); ok && !managedArtifact && !t.env.BypassToolHardProtections() {
		return "", fmt.Errorf("read_file refuses to read sensitive path %q (%s). Use a safer metadata command or ask the user for explicit secret handling", displayPath, reason)
	}
	// Worktree-bound execution: rebase onto the checkout only after the
	// sandbox and sensitive-path checks above accepted the workspace path.
	if !managedArtifact {
		resolved, err = t.env.ExecPath(ctx, resolved)
		if err != nil {
			return "", err
		}
	}

	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			hint := suggestSimilarFile(resolved)
			msg := fmt.Sprintf("File not found: %s", args.Path)
			if hint != "" {
				msg += fmt.Sprintf(". Did you mean: %s?", hint)
			}
			return "", errors.New(msg)
		}
		return "", fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory: %s. Use list_files to inspect directories or read_file on a file inside it", args.Path)
	}
	if args.Symbol == nil && args.Range == nil && args.Limit == nil && info.Size() > int64(defaultMaxFileBytes) {
		return "", fmt.Errorf("file too large (%d bytes, max %d). Use offset and limit to read portions", info.Size(), defaultMaxFileBytes)
	}

	var symbolSelection *readFileSymbolSelection
	if args.Symbol != nil {
		if args.Range != nil || args.Offset > 0 || args.Limit != nil {
			return "", errors.New("read_file accepts symbol instead of range or offset/limit")
		}
		symbolContextLines := contextLines
		if args.ContextLines == nil {
			symbolContextLines = readFileSymbolDefaultAround
		}
		selection, err := findReadFileSymbolRange(resolved, strings.TrimSpace(args.Symbol.Name), strings.TrimSpace(args.Symbol.Kind), symbolContextLines)
		if err != nil {
			return "", err
		}
		symbolSelection = &selection
		args.Offset = selection.ContextStartLine
		rangeLimit := selection.ContextEndLine - selection.ContextStartLine + 1
		args.Limit = &rangeLimit
	} else if args.Range != nil {
		if args.Offset > 0 || args.Limit != nil {
			return "", errors.New("read_file accepts either range or offset/limit, not both")
		}
		if args.Range.StartLine <= 0 {
			return "", errors.New("read_file range.start_line must be positive")
		}
		if args.Range.EndLine < args.Range.StartLine {
			return "", errors.New("read_file range.end_line must be greater than or equal to range.start_line")
		}
		args.Offset = max(1, args.Range.StartLine-contextLines)
		rangeEnd := args.Range.EndLine + contextLines
		rangeLimit := rangeEnd - args.Offset + 1
		args.Limit = &rangeLimit
	} else if args.Limit != nil && contextLines > 0 {
		if args.Offset <= 0 {
			args.Offset = 1
		}
		start := max(1, args.Offset-contextLines)
		end := args.Offset + *args.Limit - 1 + contextLines
		rangeLimit := end - start + 1
		args.Offset = start
		args.Limit = &rangeLimit
	} else if contextLines > 0 {
		return "", errors.New("read_file context_lines requires range, symbol, or offset/limit")
	}
	if args.Offset <= 0 {
		args.Offset = 1
	}
	limit := 0
	if args.Limit != nil {
		if *args.Limit <= 0 {
			return "", errors.New("read_file limit must be positive")
		}
		limit = *args.Limit
	}

	maxSelectedBytes := defaultMaxFileBytes
	if args.Limit != nil {
		maxSelectedBytes = 0
	}
	readResult, err := readFileLineRange(resolved, args.Offset, limit, maxSelectedBytes)
	if err != nil {
		return "", err
	}
	contentHash := readResult.ContentSHA256

	// Dedup check: same file, same range, same content → return stub.
	if entry, ok := t.env.GetReadEntry(resolved); ok {
		if !entry.BaselineOnly && entry.Offset == args.Offset && entry.Limit == limit {
			unchanged := entry.ContentSHA256 != "" && entry.ContentSHA256 == contentHash
			if entry.ContentSHA256 == "" {
				unchanged = readEntryMatchesInfo(entry, info)
			}
			if unchanged {
				result := map[string]any{
					"action":             "read_unchanged",
					"path":               displayPath,
					"file_sha":           formatFileSHA(contentHash),
					"workspace_revision": workspaceRevision(ctx, t.env.RevisionRoot(ctx)),
					"range":              readFileRangeMetadata(args.Offset, len(readResult.Lines)),
					"unchanged":          true,
					"message":            "File unchanged since last read. Refer to the earlier read result.",
					"next_suggestions":   []string{"use the earlier read result as evidence, or request a different offset/limit if more context is needed"},
				}
				if symbolSelection != nil {
					result["symbol"] = symbolSelection.Metadata()
				}
				return mustJSON(result)
			}
		}
	}

	// Format with line numbers (right-aligned to 6 chars + tab).
	var buf strings.Builder
	for i, line := range readResult.Lines {
		lineNum := args.Offset + i
		fmt.Fprintf(&buf, "%6d\t%s\n", lineNum, line)
	}

	// Record read state for dedup and must-read-first.
	t.env.RecordRead(resolved, ReadFileEntry{
		MtimeUnix:     info.ModTime().Unix(),
		MtimeUnixNano: info.ModTime().UnixNano(),
		Size:          info.Size(),
		ContentSHA256: contentHash,
		Offset:        args.Offset,
		Limit:         limit,
	})

	result := map[string]any{
		"action":             "read",
		"path":               displayPath,
		"file_sha":           formatFileSHA(contentHash),
		"workspace_revision": workspaceRevision(ctx, t.env.RevisionRoot(ctx)),
		"content":            buf.String(),
		"num_lines":          len(readResult.Lines),
		"start_line":         args.Offset,
		"total_lines":        readResult.TotalLines,
		"range":              readFileRangeMetadata(args.Offset, len(readResult.Lines)),
		"omitted_ranges":     readFileOmittedRanges(readResult.TotalLines, args.Offset, len(readResult.Lines)),
		"truncated":          args.Offset <= readResult.TotalLines && args.Offset-1+len(readResult.Lines) < readResult.TotalLines,
		"next_suggestions":   readFileNextSuggestions(readResult.TotalLines, args.Offset, len(readResult.Lines)),
	}
	if symbolSelection != nil {
		result["symbol"] = symbolSelection.Metadata()
	}
	return mustJSON(result)
}

const (
	maxReadFileContextLines     = 200
	maxReadFileSymbolScanBytes  = 2 * 1024 * 1024
	readFileSymbolDefaultAround = 20
)

type readFileSymbolSelection struct {
	Name             string
	RequestedKind    string
	MatchedKind      string
	StartLine        int
	EndLine          int
	ContextStartLine int
	ContextEndLine   int
	ContextLines     int
}

func (s readFileSymbolSelection) Metadata() map[string]any {
	out := map[string]any{
		"name":               s.Name,
		"matched_kind":       s.MatchedKind,
		"start_line":         s.StartLine,
		"end_line":           s.EndLine,
		"context_start_line": s.ContextStartLine,
		"context_end_line":   s.ContextEndLine,
		"context_lines":      s.ContextLines,
	}
	if strings.TrimSpace(s.RequestedKind) != "" {
		out["requested_kind"] = s.RequestedKind
	}
	return out
}

func findReadFileSymbolRange(path, name, kind string, contextLines int) (readFileSymbolSelection, error) {
	if name == "" {
		return readFileSymbolSelection{}, errors.New("read_file symbol.name is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return readFileSymbolSelection{}, fmt.Errorf("stat file for symbol lookup: %w", err)
	}
	if info.Size() > maxReadFileSymbolScanBytes {
		return readFileSymbolSelection{}, fmt.Errorf("file too large for symbol lookup (%d bytes, max %d). Use grep_repo to find the symbol and read_file range to inspect it", info.Size(), maxReadFileSymbolScanBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return readFileSymbolSelection{}, fmt.Errorf("read file for symbol lookup: %w", err)
	}
	lines := splitReadFileLines(string(data))
	for i, line := range lines {
		matchedKind, ok := matchReadFileSymbolLine(line, name, kind, filepath.Ext(path))
		if !ok {
			continue
		}
		startLine := i + 1
		endLine := inferReadFileSymbolEnd(lines, i, filepath.Ext(path))
		contextStart := max(1, startLine-contextLines)
		contextEnd := min(len(lines), endLine+contextLines)
		return readFileSymbolSelection{
			Name:             name,
			RequestedKind:    kind,
			MatchedKind:      matchedKind,
			StartLine:        startLine,
			EndLine:          endLine,
			ContextStartLine: contextStart,
			ContextEndLine:   contextEnd,
			ContextLines:     contextLines,
		}, nil
	}
	return readFileSymbolSelection{}, fmt.Errorf("symbol %q not found in %s. Use grep_repo to locate candidate definitions, then read_file the matched range", name, filepath.Base(path))
}

func splitReadFileLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func matchReadFileSymbolLine(line, name, kind, ext string) (string, bool) {
	expected := normalizeReadFileSymbolKind(kind)
	trimmed := strings.TrimSpace(line)
	switch strings.ToLower(ext) {
	case ".go":
		return matchGoSymbolLine(trimmed, name, expected)
	case ".py":
		return matchPythonSymbolLine(trimmed, name, expected)
	case ".ts", ".tsx", ".js", ".jsx", ".mts", ".mjs", ".cts", ".cjs":
		return matchTypeScriptSymbolLine(trimmed, name, expected)
	default:
		return matchPortableSymbolLine(trimmed, name, expected)
	}
}

func normalizeReadFileSymbolKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "func":
		return "function"
	case "const":
		return "constant"
	case "var":
		return "variable"
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func symbolKindAllowed(actual, expected string) bool {
	expected = normalizeReadFileSymbolKind(expected)
	if expected == "" || expected == actual {
		return true
	}
	return expected == "function" && actual == "method"
}

func matchGoSymbolLine(line, name, expected string) (string, bool) {
	if rest, ok := strings.CutPrefix(line, "func "); ok {
		kind := "function"
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(rest, "(") {
			if _, afterReceiver, ok := strings.Cut(rest, ")"); ok {
				rest = strings.TrimSpace(afterReceiver)
				kind = "method"
			}
		}
		if identifierBefore(rest, "(") == name && symbolKindAllowed(kind, expected) {
			return kind, true
		}
	}
	for _, spec := range []struct {
		prefix string
		kind   string
	}{
		{"type ", "type"},
		{"var ", "variable"},
		{"const ", "constant"},
	} {
		if rest, ok := strings.CutPrefix(line, spec.prefix); ok {
			if firstIdentifier(rest) == name && symbolKindAllowed(spec.kind, expected) {
				return spec.kind, true
			}
		}
	}
	return "", false
}

func matchPythonSymbolLine(line, name, expected string) (string, bool) {
	for _, prefix := range []string{"def ", "async def "} {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			if identifierBefore(rest, "(") == name && symbolKindAllowed("function", expected) {
				return "function", true
			}
		}
	}
	if rest, ok := strings.CutPrefix(line, "class "); ok {
		if identifierBeforeAny(rest, "(:") == name && symbolKindAllowed("class", expected) {
			return "class", true
		}
	}
	return "", false
}

func matchTypeScriptSymbolLine(line, name, expected string) (string, bool) {
	line = trimTypeScriptModifiers(line)
	for _, spec := range []struct {
		prefix string
		kind   string
	}{
		{"function ", "function"},
		{"async function ", "function"},
		{"class ", "class"},
		{"interface ", "interface"},
		{"type ", "type"},
		{"const ", "constant"},
		{"let ", "variable"},
		{"var ", "variable"},
	} {
		if rest, ok := strings.CutPrefix(line, spec.prefix); ok {
			nameEnd := "("
			if spec.kind != "function" {
				nameEnd = " =:{<"
			}
			if identifierBeforeAny(rest, nameEnd) == name && symbolKindAllowed(spec.kind, expected) {
				return spec.kind, true
			}
		}
	}
	return "", false
}

func matchPortableSymbolLine(line, name, expected string) (string, bool) {
	for _, marker := range []string{"function ", "def ", "class ", "type ", "interface ", "const ", "let ", "var "} {
		if strings.Contains(line, marker+name) && symbolKindAllowed("symbol", expected) {
			return "symbol", true
		}
	}
	return "", false
}

func trimTypeScriptModifiers(line string) string {
	for {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range []string{"export default ", "export ", "default ", "declare ", "abstract "} {
			if rest, ok := strings.CutPrefix(trimmed, prefix); ok {
				line = rest
				goto next
			}
		}
		return trimmed
	next:
	}
}

func identifierBefore(value, sep string) string {
	if before, _, ok := strings.Cut(value, sep); ok {
		return strings.TrimSpace(before)
	}
	return firstIdentifier(value)
}

func identifierBeforeAny(value, separators string) string {
	end := len(value)
	for _, sep := range separators {
		if idx := strings.IndexRune(value, sep); idx >= 0 && idx < end {
			end = idx
		}
	}
	return strings.TrimSpace(value[:end])
}

func firstIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for i, r := range value {
		if !(r == '_' || r == '$' || r == '-' || r == '.' || r == '#' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return value[:i]
		}
	}
	return value
}

func inferReadFileSymbolEnd(lines []string, startIdx int, ext string) int {
	if startIdx < 0 || startIdx >= len(lines) {
		return 0
	}
	if strings.ToLower(ext) == ".py" {
		return inferPythonSymbolEnd(lines, startIdx)
	}
	if end := inferBraceSymbolEnd(lines, startIdx); end > 0 {
		return end
	}
	return startIdx + 1
}

func inferPythonSymbolEnd(lines []string, startIdx int) int {
	startIndent := leadingWhitespaceCount(lines[startIdx])
	endIdx := startIdx
	for i := startIdx + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			endIdx = i
			continue
		}
		if leadingWhitespaceCount(lines[i]) <= startIndent && !strings.HasPrefix(trimmed, "#") {
			break
		}
		endIdx = i
	}
	return endIdx + 1
}

func inferBraceSymbolEnd(lines []string, startIdx int) int {
	balance := 0
	opened := false
	for i := startIdx; i < len(lines); i++ {
		for _, r := range lines[i] {
			switch r {
			case '{':
				balance++
				opened = true
			case '}':
				if balance > 0 {
					balance--
				}
			}
		}
		if opened && balance == 0 {
			return i + 1
		}
		if !opened && i > startIdx && strings.TrimSpace(lines[i]) == "" {
			return i
		}
	}
	return startIdx + 1
}

func leadingWhitespaceCount(line string) int {
	count := 0
	for _, r := range line {
		if r != ' ' && r != '\t' {
			break
		}
		count++
	}
	return count
}

func formatFileSHA(hexDigest string) string {
	if strings.TrimSpace(hexDigest) == "" {
		return ""
	}
	return "sha256:" + hexDigest
}

func readFileRangeMetadata(offset, lineCount int) map[string]int {
	endLine := offset + lineCount - 1
	if lineCount == 0 {
		endLine = offset - 1
	}
	return map[string]int{
		"start_line": offset,
		"end_line":   endLine,
	}
}

func readFileOmittedRanges(totalLines, offset, lineCount int) []map[string]int {
	var ranges []map[string]int
	if totalLines <= 0 {
		return ranges
	}
	if offset > 1 {
		beforeEnd := min(offset-1, totalLines)
		if beforeEnd >= 1 {
			ranges = append(ranges, map[string]int{"start_line": 1, "end_line": beforeEnd})
		}
	}
	endLine := offset + lineCount - 1
	if lineCount == 0 {
		endLine = offset - 1
	}
	if endLine < totalLines {
		afterStart := max(endLine+1, 1)
		ranges = append(ranges, map[string]int{"start_line": afterStart, "end_line": totalLines})
	}
	return ranges
}

func readFileNextSuggestions(totalLines, offset, lineCount int) []string {
	omitted := readFileOmittedRanges(totalLines, offset, lineCount)
	if len(omitted) > 0 {
		return []string{"read an omitted range or nearby surrounding lines if the current excerpt is insufficient before editing"}
	}
	return []string{"use this file_sha and excerpt as grounded evidence for subsequent edits or analysis"}
}

type readFileLineRangeResult struct {
	Lines         []string
	TotalLines    int
	ContentSHA256 string
}

func readFileLineRange(path string, offset, limit, maxSelectedBytes int) (readFileLineRangeResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return readFileLineRangeResult{}, fmt.Errorf("read file: %w", err)
	}
	defer f.Close()

	reader := bufio.NewReaderSize(f, 64*1024)
	hasher := sha256.New()
	endLine := 0
	if limit > 0 {
		endLine = offset + limit
	}
	currentLine := 1
	selected := make([]string, 0, min(limit, 128))
	var line strings.Builder
	selectedBytes := 0

	appendSelected := func(fragment []byte) error {
		if len(fragment) == 0 {
			return nil
		}
		if maxSelectedBytes > 0 && selectedBytes+len(fragment) > maxSelectedBytes {
			return fmt.Errorf("selected read range too large (over %d bytes). Use a lower limit or a later offset to read a smaller portion", maxSelectedBytes)
		}
		if _, err := line.Write(fragment); err != nil {
			return fmt.Errorf("buffer selected line: %w", err)
		}
		selectedBytes += len(fragment)
		return nil
	}

	for {
		fragment, readErr := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			_, _ = hasher.Write(fragment)
		}
		if readErr != nil && readErr != bufio.ErrBufferFull && readErr != io.EOF {
			return readFileLineRangeResult{}, fmt.Errorf("read file: %w", readErr)
		}
		if len(fragment) == 0 && readErr == io.EOF {
			break
		}

		completeLine := readErr != bufio.ErrBufferFull
		endedWithNewline := completeLine && len(fragment) > 0 && fragment[len(fragment)-1] == '\n'
		lineFragment := fragment
		if endedWithNewline {
			lineFragment = lineFragment[:len(lineFragment)-1]
			if len(lineFragment) > 0 && lineFragment[len(lineFragment)-1] == '\r' {
				lineFragment = lineFragment[:len(lineFragment)-1]
			}
		}

		inSelectedRange := currentLine >= offset && (limit <= 0 || currentLine < endLine)
		if inSelectedRange {
			if err := appendSelected(lineFragment); err != nil {
				return readFileLineRangeResult{}, err
			}
		}

		if completeLine {
			if inSelectedRange {
				lineText := line.String()
				if endedWithNewline && strings.HasSuffix(lineText, "\r") {
					lineText = lineText[:len(lineText)-1]
				}
				selected = append(selected, lineText)
				line.Reset()
			}
			currentLine++
		}
	}

	return readFileLineRangeResult{
		Lines:         selected,
		TotalLines:    currentLine - 1,
		ContentSHA256: hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

// ---------------------------------------------------------------------------
// write_file
// ---------------------------------------------------------------------------

type WriteFileTool struct{ env *Env }

func NewWriteFileTool(env *Env) *WriteFileTool { return &WriteFileTool{env: env} }

const maxImplicitWriteFileOverwriteBytes = 32 * 1024

func (t *WriteFileTool) Name() string            { return "write_file" }
func (t *WriteFileTool) IsReadOnly() bool        { return false }
func (t *WriteFileTool) IsConcurrencySafe() bool { return false }

func (t *WriteFileTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "write_file",
		Description: "Writes full file content to the workspace. Creates parent directories automatically.\n\n" +
			"Usage:\n" +
			"- Prefer edit_file for modifying existing files — it only sends the diff\n" +
			"- Only use this tool to create new files or for complete rewrites\n" +
			"- Existing files require expected_old_sha from read_file or a fresh prior read_file result\n" +
			"- Existing files larger than 32KB require overwrite_policy=\"explicit_user_requested\" or generated-file policy; use the scoped file editing tool exposed in this session for ordinary source edits\n" +
			"- Set create_only=true when the file must not already exist\n" +
			"- Sensitive credential paths such as .env, credentials, secrets, and private keys are rejected unless full access is active\n" +
			"- Returns workspace_revision and a structured diff showing what changed",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path. Relative paths resolve from the workspace root; full access also allows absolute or outside-workspace paths.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "File content.",
				},
				"expected_old_sha": map[string]any{
					"type":        "string",
					"description": "Optional sha256 digest from read_file file_sha for the current existing file content.",
				},
				"create_only": map[string]any{
					"type":        "boolean",
					"description": "If true, fail when the target file already exists.",
				},
				"overwrite_policy": map[string]any{
					"type":        "string",
					"enum":        []string{"small_file", "generated", "explicit_user_requested"},
					"description": "Required for broad existing-file rewrites. Omit for new files and small existing files; use explicit_user_requested only when the user asked for a full rewrite.",
				},
			},
			"required": []string{"path", "content"},
		},
	}
}

func (t *WriteFileTool) ValidateInput(argsJSON string) error {
	var args struct {
		Path string `json:"path"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	if strings.TrimSpace(args.Path) == "" {
		return errors.New("write_file requires path")
	}
	return nil
}

func (t *WriteFileTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Path            string `json:"path"`
		Content         string `json:"content"`
		ExpectedOldSHA  string `json:"expected_old_sha"`
		CreateOnly      bool   `json:"create_only"`
		OverwritePolicy string `json:"overwrite_policy"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Path) == "" {
		return "", errors.New("write_file requires path")
	}

	resolved, err := t.env.ResolvePath(args.Path)
	if err != nil {
		return "", err
	}
	if err := rejectSensitiveToolPath(t.env, "write_file", "write", resolved); err != nil {
		return "", err
	}
	// Worktree-bound execution: rebase onto the checkout only after the
	// sandbox and sensitive-path checks above accepted the workspace path.
	resolved, err = t.env.ExecPath(ctx, resolved)
	if err != nil {
		return "", err
	}

	oldContent, readErr := os.ReadFile(resolved)
	fileExists := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return "", fmt.Errorf("read existing file: %w", readErr)
	}
	if args.CreateOnly && fileExists {
		return "", fmt.Errorf("file already exists: %s", args.Path)
	}
	if fileExists {
		if err := t.validateExistingWrite(resolved, oldContent, strings.TrimSpace(args.ExpectedOldSHA)); err != nil {
			return "", err
		}
		if err := validateWriteFileOverwriteScope(ctx, t.env, resolved, oldContent, args.Content, args.OverwritePolicy); err != nil {
			return "", err
		}
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return "", fmt.Errorf("create parent directory: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(args.Content), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	t.env.RecordWriteBaseline(resolved, []byte(args.Content))
	if t.env.OnFileChanged != nil {
		t.env.OnFileChanged(resolved)
	}

	result := map[string]any{
		"action":             "create",
		"path":               t.env.NormalizeDisplayPathExec(ctx, resolved),
		"written_bytes":      len(args.Content),
		"new_file_sha":       formatFileSHA(sha256Hex([]byte(args.Content))),
		"workspace_revision": workspaceRevision(ctx, t.env.RevisionRoot(ctx)),
	}

	if fileExists {
		result["action"] = "overwrite"
		result["old_file_sha"] = formatFileSHA(sha256Hex(oldContent))
		result["diff"] = writeFileDiffResult(oldContent, args.Content)
	} else {
		lineCount := strings.Count(args.Content, "\n")
		if len(args.Content) > 0 && !strings.HasSuffix(args.Content, "\n") {
			lineCount++
		}
		result["diff"] = DiffResult{NewFile: true, Lines: lineCount}
	}
	return mustJSON(result)
}

func writeFileDiffResult(oldContent []byte, newContent string) DiffResult {
	if len(oldContent) > maxImplicitWriteFileOverwriteBytes || len(newContent) > maxImplicitWriteFileOverwriteBytes {
		return DiffResult{
			OldLines:  countTextLines(string(oldContent)),
			NewLines:  countTextLines(newContent),
			Truncated: true,
			Summary:   "large full-file rewrite; inline diff omitted from tool result",
		}
	}
	return computeDiff(string(oldContent), newContent, 3)
}

func countTextLines(content string) int {
	if content == "" {
		return 0
	}
	lines := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		lines++
	}
	return lines
}

func (t *WriteFileTool) validateExistingWrite(resolved string, oldContent []byte, expectedOldSHA string) error {
	currentSHA := sha256Hex(oldContent)
	if expectedOldSHA != "" {
		if normalizeFileSHA(expectedOldSHA) != currentSHA {
			return fileBaselineError("expected_old_sha_mismatch", "expected_old_sha does not match current file. Use read_file again before overwriting", "write_file", currentSHA, expectedOldSHA)
		}
		return nil
	}
	readEntry, ok := t.env.GetReadEntry(resolved)
	if !ok {
		return fileBaselineError("missing_file_baseline", "existing file has not been read yet. Use read_file first or pass expected_old_sha from read_file before overwriting", "write_file", currentSHA, "")
	}
	if readEntry.ContentSHA256 != "" && readEntry.ContentSHA256 != currentSHA {
		return fileBaselineError("stale_file_baseline", "file changed since last read. Use read_file again before overwriting", "write_file", currentSHA, formatFileSHA(readEntry.ContentSHA256))
	}
	if readEntry.ContentSHA256 == "" {
		info, err := os.Stat(resolved)
		if err != nil {
			return fmt.Errorf("stat file: %w", err)
		}
		if !readEntryMatchesInfo(readEntry, info) {
			return fileBaselineError("stale_file_baseline", "file changed since last read. Use read_file again before overwriting", "write_file", currentSHA, "")
		}
	}
	return nil
}

func validateWriteFileOverwriteScope(ctx context.Context, env *Env, resolved string, oldContent []byte, newContent, overwritePolicy string) error {
	policy := strings.ToLower(strings.TrimSpace(overwritePolicy))
	if policy != "" && !oneOf(policy, "small_file", "generated", "explicit_user_requested") {
		return fmt.Errorf("write_file invalid overwrite_policy %q: use small_file, generated, or explicit_user_requested", overwritePolicy)
	}
	if string(oldContent) == newContent {
		return nil
	}
	if len(oldContent) <= maxImplicitWriteFileOverwriteBytes {
		return nil
	}
	displayPath := resolved
	if env != nil {
		displayPath = env.NormalizeDisplayPathExec(ctx, resolved)
	}
	switch policy {
	case "explicit_user_requested":
		return nil
	case "generated":
		if writeFilePathLooksGenerated(displayPath) {
			return nil
		}
		return broadWriteFileOverwriteError(displayPath, len(oldContent), policy, "generated policy only applies to generated/build artifact paths")
	case "small_file":
		return broadWriteFileOverwriteError(displayPath, len(oldContent), policy, "small_file policy cannot overwrite files larger than the implicit small-file limit")
	default:
		return broadWriteFileOverwriteError(displayPath, len(oldContent), policy, "large existing file overwrite requires explicit policy")
	}
}

func broadWriteFileOverwriteError(path string, size int, policy, reason string) error {
	return fmt.Errorf(
		"write_file refuses broad existing-file overwrite: error_kind=broad_overwrite path=%s current_size_bytes=%d max_implicit_overwrite_bytes=%d overwrite_policy=%q reason=%q safe_retry=%q model_next_action=%q",
		path,
		size,
		maxImplicitWriteFileOverwriteBytes,
		policy,
		reason,
		"Use the scoped file editing tool exposed in this session, or retry write_file with overwrite_policy=explicit_user_requested only if the user explicitly requested a full rewrite.",
		"prefer a scoped edit tool; if a full rewrite is truly required, explain the reason and use an explicit overwrite policy",
	)
}

func writeFilePathLooksGenerated(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	if normalized == "" {
		return false
	}
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		switch part {
		case "generated", "gen", "dist", "build", "out", "coverage":
			return true
		}
	}
	base := parts[len(parts)-1]
	return strings.Contains(base, ".generated.") ||
		strings.Contains(base, ".gen.") ||
		strings.HasSuffix(base, ".pb.go") ||
		strings.HasSuffix(base, ".min.js")
}

func normalizeFileSHA(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "sha256:")
	return strings.ToLower(value)
}

// ---------------------------------------------------------------------------
// list_files
// ---------------------------------------------------------------------------

type ListFilesTool struct{ env *Env }

func NewListFilesTool(env *Env) *ListFilesTool { return &ListFilesTool{env: env} }

func (t *ListFilesTool) Name() string            { return "list_files" }
func (t *ListFilesTool) IsReadOnly() bool        { return true }
func (t *ListFilesTool) IsConcurrencySafe() bool { return true }

func (t *ListFilesTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "list_files",
		Description: "List directory entries. Defaults to workspace root and returns workspace_revision, name, path, is_dir, and size; large directories are capped.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Directory path. Defaults to workspace root.",
				},
			},
		},
	}
}

func (t *ListFilesTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Path) == "" {
		args.Path = "."
	}

	resolved, err := t.env.ResolvePath(args.Path)
	if err != nil {
		return "", err
	}
	if err := rejectSensitiveToolPath(t.env, "list_files", "list", resolved); err != nil {
		return "", err
	}
	// Worktree-bound execution: rebase onto the checkout only after the
	// sandbox and sensitive-path checks above accepted the workspace path.
	resolved, err = t.env.ExecPath(ctx, resolved)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s. Use read_file to inspect files", args.Path)
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return "", fmt.Errorf("list directory: %w", err)
	}

	limit := defaultMaxEntries

	resultEntries := make([]map[string]any, 0, min(limit, len(entries)))
	omittedProtected := 0
	for _, entry := range entries {
		entryPath := filepath.Join(resolved, entry.Name())
		displayPath := t.env.NormalizeDisplayPathExec(ctx, entryPath)
		if _, ok := sensitivePathReason(displayPath); ok && !t.env.BypassToolHardProtections() {
			omittedProtected++
			continue
		}
		if len(resultEntries) >= limit {
			break
		}

		item := map[string]any{
			"name":   entry.Name(),
			"path":   displayPath,
			"is_dir": entry.IsDir(),
		}
		if !entry.IsDir() {
			info, statErr := entry.Info()
			if statErr == nil {
				item["size"] = info.Size()
			}
		}
		resultEntries = append(resultEntries, item)
	}

	result := map[string]any{
		"action":              "list",
		"path":                t.env.NormalizeDisplayPathExec(ctx, resolved),
		"workspace_revision":  workspaceRevision(ctx, t.env.RevisionRoot(ctx)),
		"total":               len(entries),
		"truncated":           len(entries)-omittedProtected > limit,
		"omitted_entry_count": max(len(entries)-omittedProtected-limit, 0),
		"omitted_protected":   omittedProtected,
		"entries":             resultEntries,
		"next_suggestions":    listFilesNextSuggestions(len(entries), len(resultEntries), len(entries)-omittedProtected > limit),
	}
	return mustJSON(result)
}

func listFilesNextSuggestions(total, returned int, truncated bool) []string {
	if truncated {
		return []string{"narrow the list_files path or use glob to find candidate files before reading"}
	}
	if total == 0 {
		return []string{"try a parent directory or glob if the expected files are elsewhere"}
	}
	if returned == 0 {
		return []string{"inspect a parent directory or use glob to locate files"}
	}
	return []string{"use read_file on specific file paths or list_files on subdirectories before editing"}
}

// ---------------------------------------------------------------------------
// edit_file
// ---------------------------------------------------------------------------

type EditFileTool struct{ env *Env }

func NewEditFileTool(env *Env) *EditFileTool { return &EditFileTool{env: env} }

func (t *EditFileTool) Name() string            { return "edit_file" }
func (t *EditFileTool) IsReadOnly() bool        { return false }
func (t *EditFileTool) IsConcurrencySafe() bool { return false }

func (t *EditFileTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "edit_file",
		Description: "Performs exact string replacement in a file.\n\n" +
			"Usage:\n" +
			"- You must read the file before editing — edits are rejected if the file has not been read\n" +
			"- Or pass expected_old_sha from read_file to guard the exact file version being edited\n" +
			"- Provide old_text (must match exactly once) and new_text\n" +
			"- Use replace_all=true to replace every occurrence instead of requiring unique match\n" +
			"- The edit will FAIL if old_text is not unique — provide more context or use replace_all\n" +
			"- old_text and new_text must differ — identical values are rejected\n" +
			"- Use empty new_text to delete a section\n" +
			"- Prefer this over write_file for modifications — it only sends the diff\n" +
			"- Sensitive credential paths such as .env, credentials, secrets, and private keys are rejected unless full access is active\n" +
			"- Returns workspace_revision and a structured diff showing what changed",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path. Relative paths resolve from the workspace root; full access also allows absolute or outside-workspace paths.",
				},
				"old_text": map[string]any{
					"type":        "string",
					"description": "Exact text to find and replace.",
				},
				"new_text": map[string]any{
					"type":        "string",
					"description": "Text to replace old_text with. Use empty string to delete.",
				},
				"replace_all": map[string]any{
					"type":        "boolean",
					"description": "Replace all occurrences. Default false (must match exactly once).",
				},
				"expected_old_sha": map[string]any{
					"type":        "string",
					"description": "Optional sha256 digest from read_file file_sha for the current existing file content.",
				},
			},
			"required": []string{"path", "old_text", "new_text"},
		},
	}
}

func (t *EditFileTool) ValidateInput(argsJSON string) error {
	var args struct {
		Path    string `json:"path"`
		OldText string `json:"old_text"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	if strings.TrimSpace(args.Path) == "" {
		return errors.New("edit_file requires path")
	}
	if args.OldText == "" {
		return errors.New("edit_file requires old_text")
	}
	return nil
}

func (t *EditFileTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Path           string `json:"path"`
		OldText        string `json:"old_text"`
		NewText        string `json:"new_text"`
		ReplaceAll     bool   `json:"replace_all"`
		ExpectedOldSHA string `json:"expected_old_sha"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Path) == "" {
		return "", errors.New("edit_file requires path")
	}
	if args.OldText == "" {
		return "", errors.New("edit_file requires old_text")
	}
	if args.OldText == args.NewText {
		return "", errors.New("old_text and new_text are identical, no changes needed")
	}

	resolved, err := t.env.ResolvePath(args.Path)
	if err != nil {
		return "", err
	}
	if err := rejectSensitiveToolPath(t.env, "edit_file", "edit", resolved); err != nil {
		return "", err
	}
	// Worktree-bound execution: rebase onto the checkout only after the
	// sandbox and sensitive-path checks above accepted the workspace path.
	resolved, err = t.env.ExecPath(ctx, resolved)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory: %s. Use read_file to inspect the file before editing", args.Path)
	}

	content, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	oldSHA := sha256Hex(content)
	if err := t.validateEditBaseline(resolved, info, oldSHA, strings.TrimSpace(args.ExpectedOldSHA)); err != nil {
		return "", err
	}

	text := string(content)
	count := strings.Count(text, args.OldText)
	if count == 0 {
		return "", editTextMatchError{
			Kind:       "old_text_not_found",
			Expected:   args.OldText,
			Candidates: closestEditTextCandidates(text, args.OldText, 3),
		}
	}

	var newContent string
	if args.ReplaceAll {
		newContent = strings.ReplaceAll(text, args.OldText, args.NewText)
	} else {
		if count > 1 {
			return "", editTextMatchError{
				Kind:       "ambiguous_old_text",
				Expected:   args.OldText,
				MatchCount: count,
				Candidates: exactEditTextCandidates(text, args.OldText, 5),
			}
		}
		newContent = strings.Replace(text, args.OldText, args.NewText, 1)
	}

	if err := os.WriteFile(resolved, []byte(newContent), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	t.env.RecordWriteBaseline(resolved, []byte(newContent))
	if t.env.OnFileChanged != nil {
		t.env.OnFileChanged(resolved)
	}

	diff := computeDiff(text, newContent, 3)
	result := map[string]any{
		"action":             "edit",
		"path":               t.env.NormalizeDisplayPathExec(ctx, resolved),
		"old_file_sha":       formatFileSHA(oldSHA),
		"new_file_sha":       formatFileSHA(sha256Hex([]byte(newContent))),
		"workspace_revision": workspaceRevision(ctx, t.env.RevisionRoot(ctx)),
		"diff":               diff,
		"next_suggestions":   []string{"run targeted validation with command execution if that capability is exposed, otherwise inspect the resulting diff before finishing"},
	}
	return mustJSON(result)
}

func (t *EditFileTool) validateEditBaseline(resolved string, info os.FileInfo, currentSHA, expectedOldSHA string) error {
	if expectedOldSHA != "" {
		if normalizeFileSHA(expectedOldSHA) != currentSHA {
			return fileBaselineError("expected_old_sha_mismatch", "expected_old_sha does not match current file. Use read_file again before editing", "edit_file", currentSHA, expectedOldSHA)
		}
		return nil
	}
	readEntry, ok := t.env.GetReadEntry(resolved)
	if !ok {
		return fileBaselineError("missing_file_baseline", "file has not been read yet. Use read_file first or pass expected_old_sha from read_file before editing", "edit_file", currentSHA, "")
	}
	if readEntry.Size != 0 && readEntry.Size != info.Size() {
		return fileBaselineError("stale_file_baseline", "file changed since last read. Use read_file again before editing", "edit_file", currentSHA, formatFileSHA(readEntry.ContentSHA256))
	}
	if readEntry.ContentSHA256 != "" && readEntry.ContentSHA256 != currentSHA {
		return fileBaselineError("stale_file_baseline", "file changed since last read. Use read_file again before editing", "edit_file", currentSHA, formatFileSHA(readEntry.ContentSHA256))
	}
	if readEntry.ContentSHA256 == "" && !readEntryMatchesInfo(readEntry, info) {
		return fileBaselineError("stale_file_baseline", "file changed since last read. Use read_file again before editing", "edit_file", currentSHA, "")
	}
	return nil
}

type editTextMatchError struct {
	Kind       string
	Expected   string
	MatchCount int
	Candidates []patchChunkCandidate
}

func (e editTextMatchError) Error() string {
	var b strings.Builder
	switch e.Kind {
	case "ambiguous_old_text":
		fmt.Fprintf(&b, "ambiguous_old_text: old_text matched %d locations; add more surrounding context or set replace_all=true only if every occurrence should change", e.MatchCount)
	default:
		b.WriteString("old_text_not_found: failed to find old_text in file")
	}
	expected := splitEditCandidateLines(e.Expected)
	if len(expected) > 0 {
		b.WriteString("\nexpected:\n")
		b.WriteString(formatPatchErrorLines(expected, 0, 8))
	}
	if len(e.Candidates) > 0 {
		b.WriteString("\ncandidates:\n")
		for _, candidate := range e.Candidates {
			fmt.Fprintf(&b, "- lines %d-%d", candidate.StartLine, candidate.EndLine)
			if candidate.Score > 0 {
				fmt.Fprintf(&b, " score=%d", candidate.Score)
			}
			b.WriteString(":\n")
			b.WriteString(formatPatchErrorLines(candidate.Snippet, candidate.StartLine, 6))
		}
	}
	b.WriteString("\nsafe_retry: read_file the target range and retry edit_file with exact current old_text and enough surrounding context")
	return b.String()
}

func closestEditTextCandidates(content, oldText string, limit int) []patchChunkCandidate {
	lines := splitEditCandidateLines(content)
	needle := splitEditCandidateLines(oldText)
	return closestPatchChunkCandidates(lines, needle, limit)
}

func exactEditTextCandidates(content, oldText string, limit int) []patchChunkCandidate {
	if limit <= 0 || oldText == "" {
		return nil
	}
	lines := splitEditCandidateLines(content)
	startOffsets := lineStartOffsets(content)
	var candidates []patchChunkCandidate
	for offset := 0; ; {
		idx := strings.Index(content[offset:], oldText)
		if idx < 0 {
			break
		}
		start := offset + idx
		end := start + len(oldText)
		startLine := lineNumberForOffset(startOffsets, start)
		endLine := lineNumberForOffset(startOffsets, max(end-1, start))
		if startLine < 1 {
			startLine = 1
		}
		if endLine < startLine {
			endLine = startLine
		}
		if startLine > len(lines) {
			startLine = len(lines)
		}
		if endLine > len(lines) {
			endLine = len(lines)
		}
		snippet := []string{}
		if startLine > 0 && endLine >= startLine && startLine <= len(lines) {
			snippet = append(snippet, lines[startLine-1:endLine]...)
		}
		candidates = append(candidates, patchChunkCandidate{
			StartLine: startLine,
			EndLine:   endLine,
			Snippet:   snippet,
		})
		offset = end
		if len(candidates) >= limit {
			break
		}
	}
	return candidates
}

func splitEditCandidateLines(text string) []string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func lineStartOffsets(text string) []int {
	offsets := []int{0}
	for i, r := range text {
		if r == '\n' && i+1 < len(text) {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

func lineNumberForOffset(lineStarts []int, offset int) int {
	line := 1
	for i, start := range lineStarts {
		if start > offset {
			break
		}
		line = i + 1
	}
	return line
}

func fileBaselineError(kind, message, toolName, currentSHA, expectedOldSHA string) error {
	current := displayFileSHA(currentSHA)
	expected := displayFileSHA(expectedOldSHA)
	return fmt.Errorf("%s: error_kind=%s current_file_sha=%s expected_old_sha=%s safe_retry=%q model_next_action=%q",
		message,
		kind,
		current,
		expected,
		fmt.Sprintf("Run read_file on the target file and retry %s with the returned file_sha as expected_old_sha.", toolName),
		"Do not retry with stale file content; refresh evidence first.",
	)
}

func displayFileSHA(value string) string {
	normalized := normalizeFileSHA(value)
	if normalized == "" {
		return ""
	}
	return formatFileSHA(normalized)
}

func readEntryMatchesInfo(entry ReadFileEntry, info os.FileInfo) bool {
	if entry.MtimeUnixNano != 0 {
		return entry.MtimeUnixNano == info.ModTime().UnixNano() && entry.Size == info.Size()
	}
	return entry.MtimeUnix == info.ModTime().Unix()
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
