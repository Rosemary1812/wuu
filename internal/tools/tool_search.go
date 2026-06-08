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
		Name: "grep",
		Description: "Search file contents using a regex pattern, powered by ripgrep.\n\n" +
			"Usage:\n" +
			"- ALWAYS use this tool for content search. NEVER invoke grep or rg via run_shell\n" +
			"- Supports full regex syntax (e.g. \"log.*Error\", \"func\\\\s+\\\\w+\")\n" +
			"- Filter files with the include glob parameter (e.g. \"*.go\", \"*.ts\")\n" +
			"- Returns matching lines with file paths and line numbers (max 250 matches)\n" +
			"- Falls back to a pure Go implementation if ripgrep is not installed",
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
					"description": "Output mode: 'content' (default, matching lines), 'files_with_matches' (file paths only), 'count' (match counts per file).",
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
			},
			"required": []string{"pattern"},
		},
	}
}

func (t *GrepTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		Include    string `json:"include"`
		OutputMode string `json:"output_mode"`
		Context    int    `json:"context"`
		Before     int    `json:"before"`
		After      int    `json:"after"`
		IgnoreCase bool   `json:"ignore_case"`
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

	searchRoot := t.env.RootDir
	if strings.TrimSpace(args.Path) != "" {
		resolved, err := t.env.ResolvePath(args.Path)
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

	const limit = 250

	switch opts.outputMode {
	case "files_with_matches":
		files, err := grepFilesWithMatches(t.env.RootDir, args.Pattern, searchRoot, args.Include, opts, limit)
		if err != nil {
			return "", err
		}
		result := map[string]any{
			"pattern":          args.Pattern,
			"total":            len(files),
			"truncated":        len(files) >= limit,
			"files":            files,
			"next_suggestions": searchNextSuggestions("grep", "files_with_matches", len(files), len(files) >= limit),
		}
		return mustJSON(result)

	case "count":
		counts, total, err := grepCountMatches(t.env.RootDir, args.Pattern, searchRoot, args.Include, opts, limit)
		if err != nil {
			return "", err
		}
		result := map[string]any{
			"pattern":          args.Pattern,
			"total":            total,
			"truncated":        len(counts) >= limit,
			"counts":           counts,
			"next_suggestions": searchNextSuggestions("grep", "count", total, len(counts) >= limit),
		}
		return mustJSON(result)

	default: // "content"
		matches, err := grepWithRipgrep(t.env.RootDir, args.Pattern, searchRoot, args.Include, opts, limit)
		if err != nil {
			matches, err = grepWithFallback(t.env.RootDir, args.Pattern, searchRoot, args.Include, opts, limit)
			if err != nil {
				return "", err
			}
		}
		result := map[string]any{
			"pattern":          args.Pattern,
			"total":            len(matches),
			"truncated":        len(matches) >= limit,
			"matches":          matches,
			"next_suggestions": searchNextSuggestions("grep", "content", len(matches), len(matches) >= limit),
		}
		out, err := mustJSON(result)
		if err != nil {
			return "", err
		}
		if len(out) > maxGrepOutputBytes {
			out = out[:maxGrepOutputBytes]
		}
		return out, nil
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
		Name: "glob",
		Description: "Fast file pattern matching tool that works with any codebase size.\n\n" +
			"Usage:\n" +
			"- Supports glob patterns like \"**/*.go\" or \"src/**/*.ts\"\n" +
			"- Returns matching file paths (max 500 matches)\n" +
			"- Use this tool when you need to find files by name patterns\n" +
			"- For content search (finding text inside files), use grep instead",
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
			},
			"required": []string{"pattern"},
		},
	}
}

func (t *GlobTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return "", errors.New("glob requires pattern")
	}

	searchRoot := t.env.RootDir
	if strings.TrimSpace(args.Path) != "" {
		resolved, err := t.env.ResolvePath(args.Path)
		if err != nil {
			return "", err
		}
		searchRoot = resolved
	}

	const limit = 500
	matches, err := globWithRipgrep(t.env.RootDir, searchRoot, args.Pattern, limit)
	if err != nil {
		matches, err = globWithFallback(t.env.RootDir, searchRoot, args.Pattern, limit)
		if err != nil {
			return "", err
		}
	}

	result := map[string]any{
		"pattern":          args.Pattern,
		"total":            len(matches),
		"truncated":        len(matches) >= limit,
		"files":            matches,
		"next_suggestions": searchNextSuggestions("glob", "", len(matches), len(matches) >= limit),
	}
	return mustJSON(result)
}

func searchNextSuggestions(toolName, outputMode string, total int, truncated bool) []string {
	if truncated {
		switch toolName {
		case "glob":
			return []string{"narrow the glob pattern or path before reading files"}
		default:
			return []string{"narrow the grep pattern, path, or include filter before reading matches"}
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

// ---------------------------------------------------------------------------
// Shared grep/glob implementation (extracted from old Toolkit methods)
// ---------------------------------------------------------------------------

func grepWithRipgrep(rootDir, pattern, searchRoot, include string, opts grepOptions, limit int) ([]grepMatch, error) {
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

	matches := make([]grepMatch, 0, min(limit, 16))
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
			Content: strings.TrimRight(event.Data.Lines.Text, "\r\n"),
		})
		if len(matches) >= limit {
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

func grepWithFallback(rootDir, pattern, searchRoot, include string, opts grepOptions, limit int) ([]grepMatch, error) {
	compilePattern := pattern
	if opts.ignoreCase {
		compilePattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(compilePattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}

	matches := make([]grepMatch, 0, min(limit, 16))
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
					Content: line,
				})
				if len(matches) >= limit {
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

	matches := make([]string, 0, min(limit, 16))
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
		if len(matches) >= limit {
			break
		}
	}
	sort.Strings(matches)
	return matches, nil
}

func globWithFallback(rootDir, searchRoot, pattern string, limit int) ([]string, error) {
	matches := make([]string, 0, min(limit, 16))
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
		if len(matches) >= limit {
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

func grepFilesWithMatches(rootDir, pattern, searchRoot, include string, opts grepOptions, limit int) ([]string, error) {
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

	files := make([]string, 0, min(limit, 16))
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
		if len(files) >= limit {
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

	files := make([]string, 0, min(limit, 16))
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
		if len(files) >= limit {
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

	counts := make([]grepCountResult, 0, min(limit, 16))
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
		if len(counts) < limit {
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

	counts := make([]grepCountResult, 0, min(limit, 16))
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
			if len(counts) < limit {
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
