package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/stringutil"
)

const (
	defaultSemanticSearchLimit     = 20
	maxSemanticSearchLimit         = 50
	maxSemanticSearchFileBytes     = 512 * 1024
	maxSemanticSearchScannedFiles  = 2500
	maxSemanticSearchLineSnippets  = 5
	maxSemanticSearchSnippetBytes  = 240
	maxSemanticSearchRationaleByte = 300
)

var semanticSplitRE = regexp.MustCompile(`[^A-Za-z0-9]+`)

type SemanticSearchTool struct{ env *Env }

func NewSemanticSearchTool(env *Env) *SemanticSearchTool { return &SemanticSearchTool{env: env} }

func (t *SemanticSearchTool) Name() string            { return "semantic_search" }
func (t *SemanticSearchTool) IsReadOnly() bool        { return true }
func (t *SemanticSearchTool) IsConcurrencySafe() bool { return true }

func (t *SemanticSearchTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "semantic_search",
		Description: "Lightweight local concept search for candidate files when you do not yet know exact symbols or strings.\n\n" +
			"Usage:\n" +
			"- Use semantic_search for exploratory concept queries such as 'checkout discount total' or 'session expiration middleware'\n" +
			"- This is a deterministic lexical scorer, not an embeddings index; treat results as candidates, not proof\n" +
			"- Confirm important matches with read_file, grep, or ast_search before editing\n" +
			"- Results include candidate files, score, matched terms, compact line snippets, and read_file-ready ranges\n" +
			"- Use path/include/max_results to keep searches narrow",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Natural-language concept or behavior to search for.",
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
					"description": "Maximum candidate files to return. Default 20, max 50.",
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t *SemanticSearchTool) ValidateInput(argsJSON string) error {
	var args struct {
		Query string `json:"query"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return errors.New("semantic_search requires query")
	}
	if len(semanticSearchTerms(query)) == 0 {
		return errors.New("semantic_search query must include searchable terms")
	}
	return nil
}

func (t *SemanticSearchTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Query      string `json:"query"`
		Path       string `json:"path"`
		Include    string `json:"include"`
		MaxResults int    `json:"max_results"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	args.Query = strings.TrimSpace(args.Query)
	if args.Query == "" {
		return "", errors.New("semantic_search requires query")
	}
	terms := semanticSearchTerms(args.Query)
	if len(terms) == 0 {
		return "", errors.New("semantic_search query must include searchable terms")
	}
	limit := args.MaxResults
	if limit <= 0 {
		limit = defaultSemanticSearchLimit
	}
	if limit > maxSemanticSearchLimit {
		return "", fmt.Errorf("semantic_search max_results must be <= %d", maxSemanticSearchLimit)
	}

	searchRoot := t.env.RootDir
	if strings.TrimSpace(args.Path) != "" {
		resolved, err := t.env.ResolvePath(args.Path)
		if err != nil {
			return "", err
		}
		searchRoot = resolved
	}

	result, err := semanticSearch(t.env.RootDir, searchRoot, args.Include, args.Query, terms)
	if err != nil {
		return "", err
	}
	matches := result.Matches
	truncated := result.ScanTruncated || len(matches) > limit
	if len(matches) > limit {
		matches = matches[:limit]
	}
	out := map[string]any{
		"action":             "semantic_search",
		"query":              args.Query,
		"terms":              terms,
		"workspace_revision": workspaceRevision(ctx, t.env.RootDir),
		"total":              len(result.Matches),
		"returned":           len(matches),
		"truncated":          truncated,
		"files_scanned":      result.FilesScanned,
		"scan_truncated":     result.ScanTruncated,
		"matches":            matches,
		"next_suggestions":   semanticSearchNextSuggestions(len(result.Matches), truncated),
	}
	return mustJSON(out)
}

type semanticSearchResult struct {
	Matches       []semanticSearchMatch
	FilesScanned  int
	ScanTruncated bool
}

type semanticSearchMatch struct {
	File               string                    `json:"file"`
	Score              int                       `json:"score"`
	Language           string                    `json:"language,omitempty"`
	MatchedTerms       []string                  `json:"matched_terms"`
	Rationale          string                    `json:"rationale"`
	LineMatches        []semanticSearchLineMatch `json:"line_matches,omitempty"`
	ReadFileRange      map[string]int            `json:"read_file_range,omitempty"`
	ReadFileSuggestion string                    `json:"read_file_suggestion"`
}

type semanticSearchLineMatch struct {
	Line    int      `json:"line"`
	Snippet string   `json:"snippet"`
	Terms   []string `json:"terms"`
}

func semanticSearch(rootDir, searchRoot, include, query string, terms []string) (semanticSearchResult, error) {
	var result semanticSearchResult
	process := func(path string, info os.FileInfo) error {
		if info == nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if include != "" && !matchGlob(include, rel) {
			return nil
		}
		if !semanticSearchFileCandidate(rel, info) || isBinaryFile(path) {
			return nil
		}
		result.FilesScanned++
		if result.FilesScanned > maxSemanticSearchScannedFiles {
			result.ScanTruncated = true
			return filepath.SkipAll
		}
		match, ok, err := semanticSearchFile(path, rel, query, terms)
		if err != nil {
			return err
		}
		if ok {
			result.Matches = append(result.Matches, match)
		}
		return nil
	}

	info, err := os.Stat(searchRoot)
	if err != nil {
		return result, fmt.Errorf("stat search root: %w", err)
	}
	if !info.IsDir() {
		if err := process(searchRoot, info); err != nil && !errors.Is(err, filepath.SkipAll) {
			return result, err
		}
		semanticSortMatches(result.Matches)
		return result, nil
	}
	err = filepath.Walk(searchRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if semanticSearchSkippedDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		return process(path, info)
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return result, err
	}
	semanticSortMatches(result.Matches)
	return result, nil
}

func semanticSearchFile(path, relPath, query string, terms []string) (semanticSearchMatch, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return semanticSearchMatch{}, false, fmt.Errorf("read %s: %w", relPath, err)
	}
	content := string(data)
	lines := splitReadFileLines(content)
	pathLower := strings.ToLower(relPath)
	baseLower := strings.ToLower(filepath.Base(relPath))
	queryLower := strings.ToLower(query)

	score := 0
	matched := map[string]bool{}
	var rationale []string
	if strings.Contains(pathLower, queryLower) {
		score += 50
		rationale = append(rationale, "query phrase appears in path")
	}
	contentLower := strings.ToLower(content)
	if strings.Contains(contentLower, queryLower) {
		score += 15
		rationale = append(rationale, "query phrase appears in content")
	}

	lineMatches := make([]semanticSearchLineMatch, 0, maxSemanticSearchLineSnippets)
	for _, term := range terms {
		if strings.Contains(baseLower, term) {
			score += 12
			matched[term] = true
			rationale = append(rationale, fmt.Sprintf("term %q appears in filename", term))
		} else if strings.Contains(pathLower, term) {
			score += 8
			matched[term] = true
			rationale = append(rationale, fmt.Sprintf("term %q appears in path", term))
		}
	}
	for i, line := range lines {
		lineTerms := semanticTermsInText(line, terms)
		if len(lineTerms) == 0 {
			continue
		}
		for _, term := range lineTerms {
			matched[term] = true
		}
		lineScore := len(lineTerms)
		if len(lineTerms) == len(terms) {
			lineScore += 4
		}
		score += lineScore
		if len(lineMatches) < maxSemanticSearchLineSnippets {
			lineMatches = append(lineMatches, semanticSearchLineMatch{
				Line:    i + 1,
				Snippet: semanticSearchSnippet(line),
				Terms:   lineTerms,
			})
		}
	}
	if score == 0 || len(matched) == 0 {
		return semanticSearchMatch{}, false, nil
	}
	matchedTerms := sortedSemanticTerms(matched)
	if len(rationale) == 0 {
		rationale = append(rationale, "query terms appear in content")
	}
	readRange := semanticReadRange(lineMatches, len(lines))
	return semanticSearchMatch{
		File:               relPath,
		Score:              score,
		Language:           semanticSearchLanguage(filepath.Ext(relPath)),
		MatchedTerms:       matchedTerms,
		Rationale:          semanticSearchRationale(rationale),
		LineMatches:        lineMatches,
		ReadFileRange:      readRange,
		ReadFileSuggestion: semanticReadFileSuggestion(relPath, readRange),
	}, true, nil
}

func semanticSearchFileCandidate(rel string, info os.FileInfo) bool {
	if isSensitivePath(rel) || info.Size() > maxSemanticSearchFileBytes {
		return false
	}
	base := strings.ToLower(filepath.Base(rel))
	if strings.HasSuffix(base, ".lock") || strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".map") {
		return false
	}
	return true
}

func semanticSearchSkippedDir(name string) bool {
	if isSkippedDir(name) {
		return true
	}
	switch name {
	case ".cache", ".next", ".turbo", "build", "coverage", "dist", "out", "target", "tmp":
		return true
	default:
		return false
	}
}

func semanticSearchTerms(query string) []string {
	prepared := splitCamelLike(query)
	raw := semanticSplitRE.Split(prepared, -1)
	seen := map[string]bool{}
	var out []string
	for _, token := range raw {
		token = normalizeSemanticToken(token)
		if token == "" || semanticStopWords[token] || seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

func splitCamelLike(value string) string {
	var b strings.Builder
	var prev rune
	for _, r := range value {
		if prev != 0 && prev >= 'a' && prev <= 'z' && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

func normalizeSemanticToken(token string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	if len(token) <= 2 {
		return ""
	}
	switch {
	case len(token) > 5 && strings.HasSuffix(token, "ing"):
		token = strings.TrimSuffix(token, "ing")
	case len(token) > 4 && strings.HasSuffix(token, "ed"):
		token = strings.TrimSuffix(token, "ed")
	case len(token) > 3 && strings.HasSuffix(token, "s"):
		token = strings.TrimSuffix(token, "s")
	}
	return token
}

var semanticStopWords = map[string]bool{
	"and": true, "are": true, "but": true, "can": true, "for": true, "from": true,
	"has": true, "have": true, "how": true, "into": true, "not": true, "the": true,
	"this": true, "that": true, "use": true, "used": true, "using": true, "what": true,
	"when": true, "where": true, "with": true, "without": true,
}

func semanticTermsInText(text string, terms []string) []string {
	lower := strings.ToLower(text)
	var out []string
	for _, term := range terms {
		if strings.Contains(lower, term) {
			out = append(out, term)
		}
	}
	return out
}

func sortedSemanticTerms(matched map[string]bool) []string {
	out := make([]string, 0, len(matched))
	for term := range matched {
		out = append(out, term)
	}
	sort.Strings(out)
	return out
}

func semanticSearchSnippet(line string) string {
	snippet := strings.TrimSpace(redactToolOutput(line))
	if len(snippet) <= maxSemanticSearchSnippetBytes {
		return snippet
	}
	return stringutil.Truncate(snippet, maxSemanticSearchSnippetBytes, "...")
}

func semanticReadRange(matches []semanticSearchLineMatch, lineCount int) map[string]int {
	if len(matches) == 0 || lineCount <= 0 {
		end := min(lineCount, 80)
		if end < 1 {
			end = 1
		}
		return map[string]int{"start_line": 1, "end_line": end}
	}
	start := max(1, matches[0].Line-5)
	end := min(lineCount, matches[0].Line+20)
	return map[string]int{"start_line": start, "end_line": end}
}

func semanticReadFileSuggestion(path string, readRange map[string]int) string {
	return fmt.Sprintf("read_file path=%q start_line=%d end_line=%d to confirm this candidate", path, readRange["start_line"], readRange["end_line"])
}

func semanticSearchRationale(parts []string) string {
	joined := strings.Join(uniqueNonEmptyStrings(parts), "; ")
	if len(joined) <= maxSemanticSearchRationaleByte {
		return joined
	}
	return stringutil.Truncate(joined, maxSemanticSearchRationaleByte, "...")
}

func semanticSearchLanguage(ext string) string {
	switch strings.ToLower(ext) {
	case ".go":
		return "go"
	case ".ts", ".tsx", ".js", ".jsx", ".mts", ".mjs", ".cts", ".cjs":
		return "typescript/javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".kt", ".kts":
		return "kotlin"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".cs":
		return "csharp"
	case ".c", ".h", ".cc", ".cpp", ".hpp":
		return "c/c++"
	case ".md", ".mdx":
		return "markdown"
	case ".json", ".yaml", ".yml", ".toml", ".xml":
		return "config"
	case ".html", ".css", ".scss":
		return "web"
	case ".sh", ".bash", ".zsh", ".fish":
		return "shell"
	default:
		return ""
	}
}

func semanticSortMatches(matches []semanticSearchMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].File < matches[j].File
		}
		return matches[i].Score > matches[j].Score
	})
}

func semanticSearchNextSuggestions(total int, truncated bool) []string {
	if total == 0 {
		return []string{"try a broader concept query, search exact strings with grep, or inspect the repo map"}
	}
	if truncated {
		return []string{"narrow semantic_search with path/include, then confirm candidate files with read_file, grep, or ast_search before editing"}
	}
	return []string{"confirm candidate files with read_file, grep, or ast_search before editing"}
}
