package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/stringutil"
)

const (
	maxGrepMatchContentBytes = 4 * 1024
	grepPageSize             = 250
	globPageSize             = 500
)

// ---------------------------------------------------------------------------
// grep
// ---------------------------------------------------------------------------

type GrepTool struct{ env *Env }

func NewGrepTool(env *Env) *GrepTool { return &GrepTool{env: env} }

func (t *GrepTool) Name() string            { return "grep" }
func (t *GrepTool) IsReadOnly() bool        { return true }
func (t *GrepTool) IsConcurrencySafe() bool { return true }

func (t *GrepTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "grep",
		Description: "Search file contents with a regex. Supports include globs, context lines, output modes, case-insensitive search, and stable offset continuation. Returns file paths, line numbers, matches, workspace_revision, and page.next when more records are available.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Regex pattern to search for.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Directory or file to search in. Default is workspace root.",
				},
				"include": map[string]any{
					"type":        "string",
					"description": "Glob pattern to filter files (e.g. '*.go', '*.ts').",
				},
				"output_mode": map[string]any{
					"type":        "string",
					"description": "content (default), files_with_matches, or count.",
				},
				"context": map[string]any{
					"type":        "integer",
					"description": "Number of context lines before and after each match.",
				},
				"before": map[string]any{
					"type":        "integer",
					"description": "Number of lines to show before each match.",
				},
				"after": map[string]any{
					"type":        "integer",
					"description": "Number of lines to show after each match.",
				},
				"ignore_case": map[string]any{
					"type":        "boolean",
					"description": "Case insensitive search.",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Zero-based record offset. For continuation, use page.next.offset from the previous result.",
				},
				"expected_revision": map[string]any{
					"type":        "string",
					"description": "Required with non-zero offset; use page.next.expected_revision to reject stale continuation.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

func (t *GrepTool) ValidateInput(argsJSON string) error {
	var args struct {
		Pattern    string `json:"pattern"`
		IgnoreCase bool   `json:"ignore_case"`
		Offset     int    `json:"offset"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return errors.New("grep requires pattern")
	}
	if args.Offset < 0 {
		return errors.New("grep offset must be non-negative")
	}
	validationPattern := args.Pattern
	if args.IgnoreCase {
		validationPattern = "(?i)" + args.Pattern
	}
	if _, err := regexp.Compile(validationPattern); err != nil {
		return fmt.Errorf("invalid regex: %w", err)
	}
	return nil
}

func (t *GrepTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Pattern          string `json:"pattern"`
		Path             string `json:"path"`
		Include          string `json:"include"`
		OutputMode       string `json:"output_mode"`
		Context          int    `json:"context"`
		Before           int    `json:"before"`
		After            int    `json:"after"`
		IgnoreCase       bool   `json:"ignore_case"`
		Offset           int    `json:"offset"`
		ExpectedRevision string `json:"expected_revision"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return "", errors.New("grep requires pattern")
	}

	// For regex validation, prepend (?i) if ignore_case so the compiled regex
	// matches the same way ripgrep will.
	validationPattern := args.Pattern
	if args.IgnoreCase {
		validationPattern = "(?i)" + args.Pattern
	}
	if _, err := regexp.Compile(validationPattern); err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	// Worktree-bound execution: the search root switches to the checkout
	// after the ordinary sandbox check accepted the workspace path.
	execRoot, err := t.env.ExecRootDir(ctx)
	if err != nil {
		return "", err
	}
	searchRoot := execRoot
	if strings.TrimSpace(args.Path) != "" {
		resolved, err := t.env.ResolvePath(args.Path)
		if err != nil {
			return "", err
		}
		resolved, err = t.env.ExecPath(ctx, resolved)
		if err != nil {
			return "", err
		}
		searchRoot = resolved
	}

	opts := grepOptions{
		outputMode: args.OutputMode,
		context:    args.Context,
		before:     args.Before,
		after:      args.After,
		ignoreCase: args.IgnoreCase,
	}
	if opts.outputMode == "" {
		opts.outputMode = "content"
	}

	switch opts.outputMode {
	case "files_with_matches":
		files, err := grepFilesWithMatches(t.env, execRoot, args.Pattern, searchRoot, args.Include, opts, 0)
		if err != nil {
			return "", err
		}
		revision := continuationSnapshotRevision(workspaceRevision(ctx, t.env.RevisionRoot(ctx)), "grep:files_with_matches", files)
		if err := validateStableOffset("grep", args.Offset, args.ExpectedRevision, revision); err != nil {
			return "", err
		}
		page, hasMore := pageWindow(files, args.Offset, grepPageSize)
		result := map[string]any{
			"action":             "grep",
			"pattern":            args.Pattern,
			"workspace_revision": revision,
			"total":              len(files),
			"offset":             args.Offset,
			"returned_count":     len(page),
			"has_more":           hasMore,
			"page":               continuationPage(args.Offset, len(page), hasMore, revision),
			"truncated":          hasMore,
			"files":              page,
			"next_suggestions":   searchNextSuggestions("grep", "files_with_matches", len(page), hasMore),
		}
		return mustJSON(result)

	case "count":
		counts, total, err := grepCountMatches(execRoot, args.Pattern, searchRoot, args.Include, opts, 0)
		if err != nil {
			return "", err
		}
		revision := continuationSnapshotRevision(workspaceRevision(ctx, t.env.RevisionRoot(ctx)), "grep:count", counts)
		if err := validateStableOffset("grep", args.Offset, args.ExpectedRevision, revision); err != nil {
			return "", err
		}
		page, hasMore := pageWindow(counts, args.Offset, grepPageSize)
		result := map[string]any{
			"action":             "grep",
			"pattern":            args.Pattern,
			"workspace_revision": revision,
			"total":              total,
			"record_total":       len(counts),
			"offset":             args.Offset,
			"returned_count":     len(page),
			"has_more":           hasMore,
			"page":               continuationPage(args.Offset, len(page), hasMore, revision),
			"truncated":          hasMore,
			"counts":             page,
			"next_suggestions":   searchNextSuggestions("grep", "count", total, hasMore),
		}
		return mustJSON(result)

	default: // "content"
		matches, err := grepWithRipgrep(t.env, execRoot, args.Pattern, searchRoot, args.Include, opts, 0)
		if err != nil {
			matches, err = grepWithFallback(t.env, execRoot, args.Pattern, searchRoot, args.Include, opts, 0)
			if err != nil {
				return "", err
			}
		}
		revision := continuationSnapshotRevision(workspaceRevision(ctx, t.env.RevisionRoot(ctx)), "grep:content", matches)
		if err := validateStableOffset("grep", args.Offset, args.ExpectedRevision, revision); err != nil {
			return "", err
		}
		page, hasMore := pageWindow(matches, args.Offset, grepPageSize)
		return grepContentResultJSON(args.Pattern, page, len(matches), args.Offset, hasMore, revision)
	}
}

// ---------------------------------------------------------------------------
// glob
// ---------------------------------------------------------------------------

type GlobTool struct{ env *Env }

func NewGlobTool(env *Env) *GlobTool { return &GlobTool{env: env} }

func (t *GlobTool) Name() string            { return "glob" }
func (t *GlobTool) IsReadOnly() bool        { return true }
func (t *GlobTool) IsConcurrencySafe() bool { return true }

func (t *GlobTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "glob",
		Description: "Find files by glob pattern. Returns matching paths, workspace_revision, and page.next for stable non-overlapping continuation; use grep for content search.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Glob pattern (e.g. '**/*.go', 'src/**/*.ts', '*.json').",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Directory to search in. Default is workspace root.",
				},
				"offset": map[string]any{
					"type":        "integer",
					"description": "Zero-based record offset. For continuation, use page.next.offset from the previous result.",
				},
				"expected_revision": map[string]any{
					"type":        "string",
					"description": "Required with non-zero offset; use page.next.expected_revision to reject stale continuation.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

func (t *GlobTool) ValidateInput(argsJSON string) error {
	var args struct {
		Pattern string `json:"pattern"`
		Offset  int    `json:"offset"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return errors.New("glob requires pattern")
	}
	if args.Offset < 0 {
		return errors.New("glob offset must be non-negative")
	}
	return nil
}

func (t *GlobTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Pattern          string `json:"pattern"`
		Path             string `json:"path"`
		Offset           int    `json:"offset"`
		ExpectedRevision string `json:"expected_revision"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return "", errors.New("glob requires pattern")
	}

	// Worktree-bound execution: the search root switches to the checkout
	// after the ordinary sandbox check accepted the workspace path.
	execRoot, err := t.env.ExecRootDir(ctx)
	if err != nil {
		return "", err
	}
	searchRoot := execRoot
	if strings.TrimSpace(args.Path) != "" {
		resolved, err := t.env.ResolvePath(args.Path)
		if err != nil {
			return "", err
		}
		resolved, err = t.env.ExecPath(ctx, resolved)
		if err != nil {
			return "", err
		}
		searchRoot = resolved
	}

	matches, err := globWithRipgrep(execRoot, searchRoot, args.Pattern, 0)
	if err != nil {
		matches, err = globWithFallback(execRoot, searchRoot, args.Pattern, 0)
		if err != nil {
			return "", err
		}
	}
	revision := continuationSnapshotRevision(workspaceRevision(ctx, t.env.RevisionRoot(ctx)), "glob", matches)
	if err := validateStableOffset("glob", args.Offset, args.ExpectedRevision, revision); err != nil {
		return "", err
	}
	page, hasMore := pageWindow(matches, args.Offset, globPageSize)

	result := map[string]any{
		"action":             "glob",
		"pattern":            args.Pattern,
		"workspace_revision": revision,
		"total":              len(matches),
		"offset":             args.Offset,
		"returned_count":     len(page),
		"has_more":           hasMore,
		"page":               continuationPage(args.Offset, len(page), hasMore, revision),
		"truncated":          hasMore,
		"files":              page,
		"next_suggestions":   searchNextSuggestions("glob", "", len(page), hasMore),
	}
	return mustJSON(result)
}

func searchNextSuggestions(toolName, outputMode string, total int, truncated bool) []string {
	if truncated {
		switch toolName {
		case "glob":
			return []string{"use page.next with the same glob pattern and path for more files, or narrow the search"}
		default:
			return []string{"use page.next with the same grep arguments for more records, or narrow the search"}
		}
	}
	if total == 0 {
		switch toolName {
		case "glob":
			return []string{"try a broader glob pattern or inspect the directory with list_files"}
		default:
			return []string{"try a broader or case-insensitive grep pattern, or use glob to find candidate files first"}
		}
	}
	if toolName == "glob" {
		return []string{"use read_file for specific candidate files or grep within the matched set before editing"}
	}
	switch outputMode {
	case "files_with_matches":
		return []string{"use grep output_mode=content or read_file on the most relevant matched files"}
	case "count":
		return []string{"rerun grep with output_mode=content for the highest-value files before editing"}
	default:
		return []string{"read_file the relevant match ranges before editing or making a conclusion"}
	}
}

func grepContentResultJSON(pattern string, matches []grepMatch, totalRecords, offset int, sourceHasMore bool, revision string) (string, error) {
	budgetedMatches := make([]grepMatch, len(matches))
	contentTruncated := false
	for i, match := range matches {
		if len(match.Content) > maxGrepMatchContentBytes {
			match.Content = stringutil.HeadTail(match.Content, maxGrepMatchContentBytes/2, maxGrepMatchContentBytes/4, "\n...[line truncated]...\n")
			contentTruncated = true
		}
		budgetedMatches[i] = match
	}

	omitted := 0
	for {
		hasMore := sourceHasMore || omitted > 0
		truncated := hasMore || contentTruncated
		result := map[string]any{
			"action":               "grep",
			"pattern":              pattern,
			"workspace_revision":   revision,
			"total":                totalRecords,
			"offset":               offset,
			"has_more":             hasMore,
			"truncated":            truncated,
			"matches":              budgetedMatches,
			"omitted_match_count":  omitted,
			"content_truncated":    contentTruncated,
			"next_suggestions":     searchNextSuggestions("grep", "content", totalRecords, truncated),
			"result_budget_bytes":  maxGrepOutputBytes,
			"returned_match_count": len(budgetedMatches),
			"returned_count":       len(budgetedMatches),
			"page":                 continuationPage(offset, len(budgetedMatches), hasMore, revision),
		}
		out, err := mustJSON(result)
		if err != nil {
			return "", err
		}
		if len(out) <= maxGrepOutputBytes || len(budgetedMatches) == 0 {
			return out, nil
		}
		budgetedMatches = budgetedMatches[:len(budgetedMatches)-1]
		omitted++
	}
}

// ---------------------------------------------------------------------------
// Shared grep/glob implementation (extracted from old Toolkit methods)
// ---------------------------------------------------------------------------

func searchResultCapacity(limit int) int {
	if limit <= 0 || limit > 16 {
		return 16
	}
	return limit
}

func grepWithRipgrep(env *Env, rootDir, pattern, searchRoot, include string, opts grepOptions, limit int) ([]grepMatch, error) {
	relSearchRoot, err := filepath.Rel(rootDir, searchRoot)
	if err != nil {
		return nil, err
	}
	if relSearchRoot == "." {
		relSearchRoot = ""
	}
	cmd := buildRGGrepCommand(context.Background(), pattern, relSearchRoot, include, opts)
	if cmd == nil {
		return nil, errors.New("ripgrep not available")
	}
	cmd.Dir = rootDir

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return []grepMatch{}, nil
		}
		return nil, err
	}

	matches := make([]grepMatch, 0, searchResultCapacity(limit))
	for _, line := range bytes.Split(bytes.TrimSpace(output), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event rgJSONEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("parse ripgrep output: %w", err)
		}
		if event.Type != "match" {
			continue
		}
		matchPath := event.Data.Path.Text
		if !filepath.IsAbs(matchPath) {
			matchPath = filepath.Join(rootDir, matchPath)
		}
		rel, err := filepath.Rel(rootDir, matchPath)
		if err != nil {
			continue
		}
		matches = append(matches, grepMatch{
			File:    filepath.ToSlash(rel),
			Line:    event.Data.LineNumber,
			Content: grepMatchContentForPath(env, filepath.ToSlash(rel), strings.TrimRight(event.Data.Lines.Text, "\r\n")),
		})
		if limit > 0 && len(matches) >= limit {
			break
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].File == matches[j].File {
			return matches[i].Line < matches[j].Line
		}
		return matches[i].File < matches[j].File
	})
	return matches, nil
}

func grepWithFallback(env *Env, rootDir, pattern, searchRoot, include string, opts grepOptions, limit int) ([]grepMatch, error) {
	compilePattern := pattern
	if opts.ignoreCase {
		compilePattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(compilePattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}

	matches := make([]grepMatch, 0, searchResultCapacity(limit))
	walkErr := filepath.Walk(searchRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if isSkippedDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if limit > 0 && len(matches) >= limit {
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
		if isBinaryFile(path) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		scanner := bufio.NewScanner(bytes.NewReader(data))
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				matches = append(matches, grepMatch{
					File:    rel,
					Line:    lineNum,
					Content: grepMatchContentForPath(env, rel, line),
				})
				if limit > 0 && len(matches) >= limit {
					break
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("scan %s: %w", rel, err)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].File == matches[j].File {
			return matches[i].Line < matches[j].Line
		}
		return matches[i].File < matches[j].File
	})
	return matches, nil
}

func grepMatchContentForPath(env *Env, path, content string) string {
	if !isSensitivePath(path) {
		return content
	}
	// Unconfined lifts the read gate but not secret redaction: the match
	// reaches the model with credential values masked.
	if env.BypassToolHardProtections() {
		return redactToolOutput(content)
	}
	return "[REDACTED: sensitive file content]"
}

func globWithRipgrep(rootDir, searchRoot, pattern string, limit int) ([]string, error) {
	cmd := buildRGFilesCommand(context.Background(), pattern)
	if cmd == nil {
		return nil, errors.New("ripgrep not available")
	}
	cmd.Dir = searchRoot

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return []string{}, nil
		}
		return nil, err
	}

	matches := make([]string, 0, searchResultCapacity(limit))
	for _, entry := range bytes.Split(output, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		p := string(entry)
		// rg outputs paths relative to cmd.Dir (searchRoot).
		// Convert to absolute then back to rootDir-relative.
		if !filepath.IsAbs(p) {
			p = filepath.Join(searchRoot, p)
		}
		rel, err := filepath.Rel(rootDir, p)
		if err != nil {
			continue
		}
		matches = append(matches, filepath.ToSlash(rel))
		if limit > 0 && len(matches) >= limit {
			break
		}
	}
	sort.Strings(matches)
	return matches, nil
}

func globWithFallback(rootDir, searchRoot, pattern string, limit int) ([]string, error) {
	matches := make([]string, 0, searchResultCapacity(limit))
	_ = filepath.Walk(searchRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if isSkippedDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if matchGlob(pattern, rel) {
			matches = append(matches, rel)
		}
		if limit > 0 && len(matches) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Strings(matches)
	return matches, nil
}

// ---------------------------------------------------------------------------
// grep output_mode: files_with_matches
// ---------------------------------------------------------------------------

func grepFilesWithMatches(env *Env, rootDir, pattern, searchRoot, include string, opts grepOptions, limit int) ([]string, error) {
	files, err := grepFilesWithMatchesRG(rootDir, pattern, searchRoot, include, opts, limit)
	if err != nil {
		return grepFilesWithMatchesFallback(rootDir, pattern, searchRoot, include, opts, limit)
	}
	return files, nil
}

func grepFilesWithMatchesRG(rootDir, pattern, searchRoot, include string, opts grepOptions, limit int) ([]string, error) {
	name := lookupRG()
	if name == "" {
		return nil, errors.New("ripgrep not available")
	}

	relSearchRoot, err := filepath.Rel(rootDir, searchRoot)
	if err != nil {
		return nil, err
	}
	if relSearchRoot == "." {
		relSearchRoot = ""
	}

	args := []string{"--files-with-matches", "--hidden", "-H"}
	if opts.ignoreCase {
		args = append(args, "-i")
	}
	args = append(args, pattern)
	if include != "" {
		args = append(args, "--glob", include)
	}
	if strings.TrimSpace(relSearchRoot) != "" {
		args = append(args, relSearchRoot)
	}

	cmd := rgCommand(context.Background(), name, args...)
	cmd.Dir = rootDir

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return []string{}, nil
		}
		return nil, err
	}

	files := make([]string, 0, searchResultCapacity(limit))
	for _, line := range bytes.Split(bytes.TrimSpace(output), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		p := string(line)
		if !filepath.IsAbs(p) {
			p = filepath.Join(rootDir, p)
		}
		rel, err := filepath.Rel(rootDir, p)
		if err != nil {
			continue
		}
		files = append(files, filepath.ToSlash(rel))
		if limit > 0 && len(files) >= limit {
			break
		}
	}
	sort.Strings(files)
	return files, nil
}

func grepFilesWithMatchesFallback(rootDir, pattern, searchRoot, include string, opts grepOptions, limit int) ([]string, error) {
	compilePattern := pattern
	if opts.ignoreCase {
		compilePattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(compilePattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}

	files := make([]string, 0, searchResultCapacity(limit))
	walkErr := filepath.Walk(searchRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if isSkippedDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if limit > 0 && len(files) >= limit {
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
		if isBinaryFile(path) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if re.Match(data) {
			files = append(files, rel)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(files)
	return files, nil
}

// ---------------------------------------------------------------------------
// grep output_mode: count
// ---------------------------------------------------------------------------

func grepCountMatches(rootDir, pattern, searchRoot, include string, opts grepOptions, limit int) ([]grepCountResult, int, error) {
	counts, total, err := grepCountMatchesRG(rootDir, pattern, searchRoot, include, opts, limit)
	if err != nil {
		return grepCountMatchesFallback(rootDir, pattern, searchRoot, include, opts, limit)
	}
	return counts, total, nil
}

func grepCountMatchesRG(rootDir, pattern, searchRoot, include string, opts grepOptions, limit int) ([]grepCountResult, int, error) {
	name := lookupRG()
	if name == "" {
		return nil, 0, errors.New("ripgrep not available")
	}

	relSearchRoot, err := filepath.Rel(rootDir, searchRoot)
	if err != nil {
		return nil, 0, err
	}
	if relSearchRoot == "." {
		relSearchRoot = ""
	}

	args := []string{"--count", "--hidden", "-H"}
	if opts.ignoreCase {
		args = append(args, "-i")
	}
	args = append(args, pattern)
	if include != "" {
		args = append(args, "--glob", include)
	}
	if strings.TrimSpace(relSearchRoot) != "" {
		args = append(args, relSearchRoot)
	}

	cmd := rgCommand(context.Background(), name, args...)
	cmd.Dir = rootDir

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return []grepCountResult{}, 0, nil
		}
		return nil, 0, err
	}

	counts := make([]grepCountResult, 0, searchResultCapacity(limit))
	total := 0
	for _, line := range bytes.Split(bytes.TrimSpace(output), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		// rg --count output: "file:count"
		parts := bytes.SplitN(line, []byte{':'}, 2)
		if len(parts) != 2 {
			continue
		}
		p := string(parts[0])
		if !filepath.IsAbs(p) {
			p = filepath.Join(rootDir, p)
		}
		rel, err := filepath.Rel(rootDir, p)
		if err != nil {
			continue
		}
		var count int
		if _, err := fmt.Sscanf(string(parts[1]), "%d", &count); err != nil {
			continue
		}
		total += count
		if limit <= 0 || len(counts) < limit {
			counts = append(counts, grepCountResult{
				File:  filepath.ToSlash(rel),
				Count: count,
			})
		}
	}
	sort.SliceStable(counts, func(i, j int) bool {
		return counts[i].File < counts[j].File
	})
	return counts, total, nil
}

func grepCountMatchesFallback(rootDir, pattern, searchRoot, include string, opts grepOptions, limit int) ([]grepCountResult, int, error) {
	compilePattern := pattern
	if opts.ignoreCase {
		compilePattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(compilePattern)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid regex: %w", err)
	}

	counts := make([]grepCountResult, 0, searchResultCapacity(limit))
	total := 0
	walkErr := filepath.Walk(searchRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if isSkippedDir(info.Name()) {
				return filepath.SkipDir
			}
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
		if isBinaryFile(path) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		matches := re.FindAll(data, -1)
		if len(matches) > 0 {
			total += len(matches)
			if limit <= 0 || len(counts) < limit {
				counts = append(counts, grepCountResult{
					File:  rel,
					Count: len(matches),
				})
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, 0, walkErr
	}
	sort.SliceStable(counts, func(i, j int) bool {
		return counts[i].File < counts[j].File
	})
	return counts, total, nil
}
