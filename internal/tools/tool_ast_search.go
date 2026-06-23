package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/stringutil"
)

const (
	defaultASTSearchLimit = 100
	maxASTSearchLimit     = 250
	maxASTSignatureBytes  = 500
)

type ASTSearchTool struct{ env *Env }

func NewASTSearchTool(env *Env) *ASTSearchTool { return &ASTSearchTool{env: env} }

func (t *ASTSearchTool) Name() string            { return "ast_search" }
func (t *ASTSearchTool) IsReadOnly() bool        { return true }
func (t *ASTSearchTool) IsConcurrencySafe() bool { return true }

func (t *ASTSearchTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "ast_search",
		Description: "Conservative structure search for common Go, TypeScript/JavaScript, and Python symbols.\n\n" +
			"Usage:\n" +
			"- Use ast_search for definitions, imports, and simple call sites before reading files\n" +
			"- This is not a full compiler or LSP; confirm important matches with read_file before editing\n" +
			"- kind defaults to definition and also accepts function, method, class, type, interface, variable, constant, import, or call\n" +
			"- Results include file, line, end_line, kind, signature, and read_file-ready ranges\n" +
			"- Use path/include/max_results to keep searches narrow",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Symbol, import/package fragment, or callee name to search for.",
				},
				"kind": map[string]any{
					"type":        "string",
					"description": "definition, function, method, class, type, interface, variable, constant, import, or call. Default definition.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Directory or file to search in. Default is workspace root.",
				},
				"include": map[string]any{
					"type":        "string",
					"description": "Glob pattern to filter files, e.g. '*.go' or 'src/**/*.ts'.",
				},
				"max_results": map[string]any{
					"type":        "integer",
					"description": "Maximum matches to return. Default 100, max 250.",
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t *ASTSearchTool) ValidateInput(argsJSON string) error {
	var args struct {
		Query string `json:"query"`
		Kind  string `json:"kind"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	if strings.TrimSpace(args.Query) == "" {
		return errors.New("ast_search requires query")
	}
	if normalizeASTSearchKind(args.Kind) == "" {
		return fmt.Errorf("unsupported ast_search kind %q", args.Kind)
	}
	return nil
}

func (t *ASTSearchTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Query      string `json:"query"`
		Kind       string `json:"kind"`
		Path       string `json:"path"`
		Include    string `json:"include"`
		MaxResults int    `json:"max_results"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	args.Query = strings.TrimSpace(args.Query)
	if args.Query == "" {
		return "", errors.New("ast_search requires query")
	}
	kind := normalizeASTSearchKind(args.Kind)
	if kind == "" {
		return "", fmt.Errorf("unsupported ast_search kind %q", args.Kind)
	}
	limit := args.MaxResults
	if limit <= 0 {
		limit = defaultASTSearchLimit
	}
	if limit > maxASTSearchLimit {
		return "", fmt.Errorf("ast_search max_results must be <= %d", maxASTSearchLimit)
	}

	searchRoot := t.env.RootDir
	if strings.TrimSpace(args.Path) != "" {
		resolved, err := t.env.ResolvePath(args.Path)
		if err != nil {
			return "", err
		}
		searchRoot = resolved
	}

	matches, err := astSearch(t.env.RootDir, searchRoot, args.Include, args.Query, kind, limit, t.env.BypassToolHardProtections())
	if err != nil {
		return "", err
	}
	result := map[string]any{
		"action":             "ast_search",
		"query":              args.Query,
		"kind":               kind,
		"workspace_revision": workspaceRevision(ctx, t.env.RootDir),
		"total":              len(matches),
		"returned":           len(matches),
		"truncated":          len(matches) >= limit,
		"matches":            matches,
		"next_suggestions":   astSearchNextSuggestions(kind, len(matches), len(matches) >= limit),
	}
	return mustJSON(result)
}

type astSearchMatch struct {
	File               string         `json:"file"`
	Line               int            `json:"line"`
	EndLine            int            `json:"end_line,omitempty"`
	Kind               string         `json:"kind"`
	Name               string         `json:"name"`
	Language           string         `json:"language,omitempty"`
	Signature          string         `json:"signature,omitempty"`
	ReadFileRange      map[string]int `json:"read_file_range,omitempty"`
	ReadFileSuggestion string         `json:"read_file_suggestion,omitempty"`
}

func astSearch(rootDir, searchRoot, include, query, kind string, limit int, allowSensitive bool) ([]astSearchMatch, error) {
	var matches []astSearchMatch
	process := func(path string, info os.FileInfo) error {
		if info == nil || info.IsDir() {
			return nil
		}
		if len(matches) >= limit {
			return filepath.SkipAll
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if include != "" && !matchGlob(include, rel) {
			return nil
		}
		if (!allowSensitive && isSensitivePath(rel)) || isBinaryFile(path) || info.Size() > maxReadFileSymbolScanBytes {
			return nil
		}
		language := astSearchLanguage(filepath.Ext(path))
		if language == "" {
			return nil
		}
		fileMatches, err := astSearchFile(path, rel, language, query, kind, limit-len(matches))
		if err != nil {
			return err
		}
		matches = append(matches, fileMatches...)
		return nil
	}

	info, err := os.Stat(searchRoot)
	if err != nil {
		return nil, fmt.Errorf("stat search root: %w", err)
	}
	if !info.IsDir() {
		if err := process(searchRoot, info); err != nil && !errors.Is(err, filepath.SkipAll) {
			return nil, err
		}
		return matches, nil
	}
	err = filepath.Walk(searchRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if isSkippedDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		return process(path, info)
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return nil, err
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].File == matches[j].File {
			return matches[i].Line < matches[j].Line
		}
		return matches[i].File < matches[j].File
	})
	return matches, nil
}

func astSearchFile(path, relPath, language, query, kind string, limit int) ([]astSearchMatch, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", relPath, err)
	}
	lines := splitReadFileLines(string(data))
	out := make([]astSearchMatch, 0, min(limit, 8))
	for i := range lines {
		if len(out) >= limit {
			break
		}
		match, ok := astSearchLine(lines, i, relPath, language, query, kind)
		if ok {
			out = append(out, match)
		}
	}
	return out, nil
}

func astSearchLine(lines []string, lineIdx int, relPath, language, query, kind string) (astSearchMatch, bool) {
	line := lines[lineIdx]
	ext := filepath.Ext(relPath)
	switch kind {
	case "import":
		if !matchASTImportLine(line, query, ext) {
			return astSearchMatch{}, false
		}
		return newASTSearchMatch(relPath, language, query, "import", lineIdx+1, lineIdx+1, line), true
	case "call":
		if _, ok := matchReadFileSymbolLine(line, query, "", ext); ok {
			return astSearchMatch{}, false
		}
		if !containsIdentifierCall(line, query) {
			return astSearchMatch{}, false
		}
		return newASTSearchMatch(relPath, language, query, "call", lineIdx+1, lineIdx+1, line), true
	default:
		expected := ""
		if kind != "definition" {
			expected = kind
		}
		matchedKind, ok := matchReadFileSymbolLine(line, query, expected, ext)
		if !ok {
			return astSearchMatch{}, false
		}
		endLine := inferReadFileSymbolEnd(lines, lineIdx, ext)
		return newASTSearchMatch(relPath, language, query, matchedKind, lineIdx+1, endLine, line), true
	}
}

func newASTSearchMatch(relPath, language, name, kind string, line, endLine int, signature string) astSearchMatch {
	signature = strings.TrimSpace(signature)
	if len(signature) > maxASTSignatureBytes {
		signature = stringutil.HeadTail(signature, maxASTSignatureBytes/2, maxASTSignatureBytes/4, "\n...[signature truncated]...\n")
	}
	return astSearchMatch{
		File:      relPath,
		Line:      line,
		EndLine:   endLine,
		Kind:      kind,
		Name:      name,
		Language:  language,
		Signature: signature,
		ReadFileRange: map[string]int{
			"start_line": line,
			"end_line":   max(line, endLine),
		},
		ReadFileSuggestion: fmt.Sprintf("read_file path=%q range.start_line=%d range.end_line=%d before editing or relying on this match", relPath, line, max(line, endLine)),
	}
}

func normalizeASTSearchKind(kind string) string {
	switch normalizeReadFileSymbolKind(kind) {
	case "", "definition", "symbol":
		return "definition"
	case "function", "method", "class", "type", "interface", "variable", "constant", "import", "call":
		return normalizeReadFileSymbolKind(kind)
	default:
		return ""
	}
}

func astSearchLanguage(ext string) string {
	switch strings.ToLower(ext) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx", ".js", ".jsx", ".mts", ".mjs", ".cts", ".cjs":
		return "typescript"
	default:
		return ""
	}
}

func matchASTImportLine(line, query, ext string) bool {
	trimmed := strings.TrimSpace(line)
	if query != "" && !strings.Contains(trimmed, query) {
		return false
	}
	switch strings.ToLower(ext) {
	case ".go":
		return strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "\"")
	case ".py":
		return strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from ")
	case ".ts", ".tsx", ".js", ".jsx", ".mts", ".mjs", ".cts", ".cjs":
		return strings.HasPrefix(trimmed, "import ") || strings.Contains(trimmed, " require(") || strings.HasPrefix(trimmed, "require(")
	default:
		return false
	}
}

func containsIdentifierCall(line, name string) bool {
	if name == "" {
		return false
	}
	for start := 0; ; {
		idx := strings.Index(line[start:], name)
		if idx < 0 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !isASTIdentifierRune(rune(line[idx-1]))
		after := idx + len(name)
		afterOK := after >= len(line) || !isASTIdentifierRune(rune(line[after]))
		if beforeOK && afterOK {
			for after < len(line) && (line[after] == ' ' || line[after] == '\t') {
				after++
			}
			if after < len(line) && line[after] == '(' {
				return true
			}
		}
		start = idx + len(name)
	}
}

func isASTIdentifierRune(r rune) bool {
	return r == '_' || r == '$' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}

func astSearchNextSuggestions(kind string, total int, truncated bool) []string {
	if truncated {
		return []string{"narrow path, include, kind, or query before reading matches"}
	}
	if total == 0 {
		return []string{"try grep for textual matches, broaden path/include, or verify the symbol spelling"}
	}
	if kind == "call" || kind == "import" {
		return []string{"read_file the highest-value match ranges before changing call sites or imports"}
	}
	return []string{"read_file the matched definition range before editing or making architectural conclusions"}
}
