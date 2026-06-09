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
		Name: "read_file",
		Description: "Reads a file from the workspace. Returns content with line numbers.\n\n" +
			"Usage:\n" +
			"- The path parameter is relative to the workspace root\n" +
			"- Returns content with cat -n style line number prefixes (number + tab)\n" +
			"- Use offset (1-based line) and limit to read specific portions of large files\n" +
			"- Results include file_sha, range, omitted_ranges, and next_suggestions for follow-up reads\n" +
			"- Files >256KB are rejected unless limit is provided\n" +
			"- Repeated reads of the same file/range return a stub if the file is unchanged\n" +
			"- This tool can only read files, not directories — use list_files for directories\n" +
			"- Binary files are not supported; use run_shell for binary inspection",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative file path in workspace.",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "1-based line number to start reading from. Default 1.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Max lines to return. Omit to read the whole file when it fits size limits.",
				},
			},
			"required": []string{"path"},
		},
	}
}

func (t *ReadFileTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  *int   `json:"limit"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Path) == "" {
		return "", errors.New("read_file requires path")
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

	resolved, err := t.env.ResolvePath(args.Path)
	if err != nil {
		return "", err
	}
	displayPath := t.env.NormalizeDisplayPath(resolved)
	if reason, ok := sensitivePathReason(displayPath); ok {
		return "", fmt.Errorf("read_file refuses to read sensitive path %q (%s). Use a safer metadata command or ask the user for explicit secret handling", displayPath, reason)
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
	if args.Limit == nil && info.Size() > int64(defaultMaxFileBytes) {
		return "", fmt.Errorf("file too large (%d bytes, max %d). Use offset and limit to read portions", info.Size(), defaultMaxFileBytes)
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
		if entry.Offset == args.Offset && entry.Limit == limit {
			unchanged := entry.ContentSHA256 != "" && entry.ContentSHA256 == contentHash
			if entry.ContentSHA256 == "" {
				unchanged = readEntryMatchesInfo(entry, info)
			}
			if unchanged {
				result := map[string]any{
					"path":             t.env.NormalizeDisplayPath(resolved),
					"file_sha":         formatFileSHA(contentHash),
					"range":            readFileRangeMetadata(args.Offset, len(readResult.Lines)),
					"unchanged":        true,
					"message":          "File unchanged since last read. Refer to the earlier read result.",
					"next_suggestions": []string{"use the earlier read result as evidence, or request a different offset/limit if more context is needed"},
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
		"path":             t.env.NormalizeDisplayPath(resolved),
		"file_sha":         formatFileSHA(contentHash),
		"content":          buf.String(),
		"num_lines":        len(readResult.Lines),
		"start_line":       args.Offset,
		"total_lines":      readResult.TotalLines,
		"range":            readFileRangeMetadata(args.Offset, len(readResult.Lines)),
		"omitted_ranges":   readFileOmittedRanges(readResult.TotalLines, args.Offset, len(readResult.Lines)),
		"truncated":        args.Offset <= readResult.TotalLines && args.Offset-1+len(readResult.Lines) < readResult.TotalLines,
		"next_suggestions": readFileNextSuggestions(readResult.TotalLines, args.Offset, len(readResult.Lines)),
	}
	return mustJSON(result)
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
			"- Set create_only=true when the file must not already exist\n" +
			"- Sensitive credential paths such as .env, credentials, secrets, and private keys are rejected\n" +
			"- Returns a structured diff showing what changed",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative file path in workspace.",
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
			},
			"required": []string{"path", "content"},
		},
	}
}

func (t *WriteFileTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		Path           string `json:"path"`
		Content        string `json:"content"`
		ExpectedOldSHA string `json:"expected_old_sha"`
		CreateOnly     bool   `json:"create_only"`
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
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return "", fmt.Errorf("create parent directory: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(args.Content), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	if t.env.OnFileChanged != nil {
		t.env.OnFileChanged(resolved)
	}

	result := map[string]any{
		"path":          t.env.NormalizeDisplayPath(resolved),
		"written_bytes": len(args.Content),
		"new_file_sha":  formatFileSHA(sha256Hex([]byte(args.Content))),
	}

	if fileExists {
		result["old_file_sha"] = formatFileSHA(sha256Hex(oldContent))
		result["diff"] = computeDiff(string(oldContent), args.Content, 3)
	} else {
		lineCount := strings.Count(args.Content, "\n")
		if len(args.Content) > 0 && !strings.HasSuffix(args.Content, "\n") {
			lineCount++
		}
		result["diff"] = DiffResult{NewFile: true, Lines: lineCount}
	}
	return mustJSON(result)
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
		Name: "list_files",
		Description: "Lists entries under a directory in the workspace.\n\n" +
			"Usage:\n" +
			"- Returns name, path, is_dir, and size for each entry\n" +
			"- Defaults to workspace root when path is omitted\n" +
			"- Truncated at 1000 entries for large directories",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative directory path, default is current workspace root.",
				},
			},
		},
	}
}

func (t *ListFilesTool) Execute(_ context.Context, argsJSON string) (string, error) {
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
	for i, entry := range entries {
		if i >= limit {
			break
		}

		item := map[string]any{
			"name":   entry.Name(),
			"path":   t.env.NormalizeDisplayPath(filepath.Join(resolved, entry.Name())),
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
		"path":                t.env.NormalizeDisplayPath(resolved),
		"total":               len(entries),
		"truncated":           len(entries) > limit,
		"omitted_entry_count": max(len(entries)-limit, 0),
		"entries":             resultEntries,
		"next_suggestions":    listFilesNextSuggestions(len(entries), len(resultEntries), len(entries) > limit),
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
			"- Sensitive credential paths such as .env, credentials, secrets, and private keys are rejected\n" +
			"- Returns a structured diff showing what changed",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative file path in workspace.",
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

func (t *EditFileTool) Execute(_ context.Context, argsJSON string) (string, error) {
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
		return "", errors.New("old_text not found in file")
	}

	var newContent string
	if args.ReplaceAll {
		newContent = strings.ReplaceAll(text, args.OldText, args.NewText)
	} else {
		if count > 1 {
			return "", fmt.Errorf("old_text matches %d times, must be unique (use replace_all=true to replace all)", count)
		}
		newContent = strings.Replace(text, args.OldText, args.NewText, 1)
	}

	if err := os.WriteFile(resolved, []byte(newContent), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	if t.env.OnFileChanged != nil {
		t.env.OnFileChanged(resolved)
	}

	diff := computeDiff(text, newContent, 3)
	result := map[string]any{
		"path":             t.env.NormalizeDisplayPath(resolved),
		"old_file_sha":     formatFileSHA(oldSHA),
		"new_file_sha":     formatFileSHA(sha256Hex([]byte(newContent))),
		"diff":             diff,
		"next_suggestions": []string{"run targeted validation with run_test or inspect the resulting diff before finishing"},
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
