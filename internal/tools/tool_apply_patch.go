package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// ---------------------------------------------------------------------------
// apply_patch
// ---------------------------------------------------------------------------

type ApplyPatchTool struct{ env *Env }

func NewApplyPatchTool(env *Env) *ApplyPatchTool { return &ApplyPatchTool{env: env} }

func (t *ApplyPatchTool) Name() string            { return "apply_patch" }
func (t *ApplyPatchTool) IsReadOnly() bool        { return false }
func (t *ApplyPatchTool) IsConcurrencySafe() bool { return false }

func (t *ApplyPatchTool) Classify(argsJSON string) ToolClassification {
	var args struct {
		DryRun  bool `json:"dry_run"`
		DryRun2 bool `json:"dryRun"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskHigh,
			Reason:          "invalid patch invocation",
		}
	}
	if args.DryRun || args.DryRun2 {
		return ToolClassification{
			ReadOnly:        true,
			ConcurrencySafe: true,
			Risk:            ToolRiskLow,
			Reason:          "patch dry-run preview",
		}
	}
	return ToolClassification{
		ReadOnly:        false,
		ConcurrencySafe: false,
		Risk:            ToolRiskHigh,
		Reason:          "patch applies workspace changes",
	}
}

func (t *ApplyPatchTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "apply_patch",
		Description: "Applies a structured patch to one or more workspace files.\n\n" +
			"Usage:\n" +
			"- Use this for manual code edits when this tool is available\n" +
			"- The patch language uses *** Begin Patch / *** End Patch markers\n" +
			"- Supported operations: *** Add File, *** Update File, optional *** Move to, and *** Delete File\n" +
			"- Prefix added lines with +, removed lines with -, and unchanged context lines with a space\n" +
			"- Paths are relative to the workspace root and cannot escape it\n" +
			"- Set dry_run=true to validate anchors and preview structured diffs without mutating files\n" +
			"- Returns changed_files, hunk_count, provenance, and per-file structured diffs showing what changed",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"patchText": map[string]any{
					"type":        "string",
					"description": "The full patch text, including *** Begin Patch and *** End Patch markers.",
				},
				"dry_run": map[string]any{
					"type":        "boolean",
					"description": "Validate and preview the patch without writing files or firing file-change hooks.",
				},
			},
			"required": []string{"patchText"},
		},
	}
}

func (t *ApplyPatchTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		PatchText  string `json:"patchText"`
		Patch      string `json:"patch"`
		PatchText2 string `json:"patch_text"`
		DryRun     bool   `json:"dry_run"`
		DryRun2    bool   `json:"dryRun"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	patchText := args.PatchText
	if strings.TrimSpace(patchText) == "" {
		patchText = args.PatchText2
	}
	if strings.TrimSpace(patchText) == "" {
		patchText = args.Patch
	}
	if strings.TrimSpace(patchText) == "" {
		return "", errors.New("apply_patch requires patchText")
	}

	patch, err := parseApplyPatch(patchText)
	if err != nil {
		return "", fmt.Errorf("apply_patch verification failed: %w", err)
	}
	if len(patch.Hunks) == 0 {
		return "", errors.New("apply_patch verification failed: no hunks found")
	}

	dryRun := args.DryRun || args.DryRun2
	files := make([]applyPatchFileResult, 0, len(patch.Hunks))
	changedFiles := make([]string, 0, len(patch.Hunks))
	for _, hunk := range patch.Hunks {
		result, err := t.applyHunk(hunk, dryRun)
		if err != nil {
			return "", fmt.Errorf("apply_patch verification failed: %w", err)
		}
		files = append(files, result)
		changedFiles = append(changedFiles, result.changedPath())
	}

	return mustJSON(map[string]any{
		"dry_run":          dryRun,
		"hunk_count":       len(patch.Hunks),
		"changed_files":    uniqueNonEmptyStrings(changedFiles),
		"next_suggestions": applyPatchNextSuggestions(dryRun),
		"provenance": map[string]any{
			"tool":   "apply_patch",
			"source": "model_tool_call",
		},
		"files": files,
	})
}

func applyPatchNextSuggestions(dryRun bool) []string {
	if dryRun {
		return []string{"inspect the previewed diffs, then rerun apply_patch without dry_run only if the preview matches the intended change"}
	}
	return []string{"run targeted validation with run_test, then inspect the resulting diff before finishing"}
}

type applyPatch struct {
	Hunks []applyPatchHunk
}

type applyPatchHunk struct {
	Type     string
	Path     string
	MovePath string
	Contents []string
	Chunks   []applyPatchChunk
}

type applyPatchChunk struct {
	OldLines    []string
	NewLines    []string
	EndOfFile   bool
	ContextHint string
}

type applyPatchFileResult struct {
	Path     string     `json:"path"`
	MovePath string     `json:"move_path,omitempty"`
	Action   string     `json:"action"`
	Diff     DiffResult `json:"diff"`
}

func (r applyPatchFileResult) changedPath() string {
	if strings.TrimSpace(r.MovePath) != "" {
		return strings.TrimSpace(r.MovePath)
	}
	return strings.TrimSpace(r.Path)
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func parseApplyPatch(raw string) (applyPatch, error) {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(raw), "\r\n", "\n"), "\r", "\n"), "\n")
	begin := -1
	end := -1
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case "*** Begin Patch":
			if begin >= 0 {
				return applyPatch{}, errors.New("duplicate Begin Patch marker")
			}
			begin = i
		case "*** End Patch":
			end = i
		}
	}
	if begin < 0 || end < 0 || begin >= end {
		return applyPatch{}, errors.New("missing Begin/End Patch markers")
	}

	var patch applyPatch
	for i := begin + 1; i < end; {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "*** Add File:"):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Add File:"))
			if path == "" {
				return applyPatch{}, errors.New("Add File path is required")
			}
			contents, next, err := parseApplyPatchAdd(lines, i+1, end)
			if err != nil {
				return applyPatch{}, err
			}
			patch.Hunks = append(patch.Hunks, applyPatchHunk{Type: "add", Path: path, Contents: contents})
			i = next
		case strings.HasPrefix(line, "*** Delete File:"):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File:"))
			if path == "" {
				return applyPatch{}, errors.New("Delete File path is required")
			}
			patch.Hunks = append(patch.Hunks, applyPatchHunk{Type: "delete", Path: path})
			i++
		case strings.HasPrefix(line, "*** Update File:"):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Update File:"))
			if path == "" {
				return applyPatch{}, errors.New("Update File path is required")
			}
			i++
			movePath := ""
			if i < end && strings.HasPrefix(lines[i], "*** Move to:") {
				movePath = strings.TrimSpace(strings.TrimPrefix(lines[i], "*** Move to:"))
				if movePath == "" {
					return applyPatch{}, errors.New("Move to path is required")
				}
				i++
			}
			chunks, next, err := parseApplyPatchChunks(lines, i, end)
			if err != nil {
				return applyPatch{}, err
			}
			if len(chunks) == 0 && movePath == "" {
				return applyPatch{}, fmt.Errorf("Update File %s has no chunks", path)
			}
			patch.Hunks = append(patch.Hunks, applyPatchHunk{Type: "update", Path: path, MovePath: movePath, Chunks: chunks})
			i = next
		case strings.TrimSpace(line) == "":
			i++
		default:
			return applyPatch{}, fmt.Errorf("unexpected patch line %q", line)
		}
	}
	return patch, nil
}

func parseApplyPatchAdd(lines []string, start, end int) ([]string, int, error) {
	var contents []string
	for i := start; i < end; i++ {
		line := lines[i]
		if strings.HasPrefix(line, "*** ") {
			return contents, i, nil
		}
		if !strings.HasPrefix(line, "+") {
			return nil, i, fmt.Errorf("Add File lines must start with +: %q", line)
		}
		contents = append(contents, strings.TrimPrefix(line, "+"))
	}
	return contents, end, nil
}

func parseApplyPatchChunks(lines []string, start, end int) ([]applyPatchChunk, int, error) {
	var chunks []applyPatchChunk
	i := start
	for i < end {
		line := lines[i]
		if strings.HasPrefix(line, "*** ") {
			return chunks, i, nil
		}
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		if !strings.HasPrefix(line, "@@") {
			return nil, i, fmt.Errorf("Update File chunk must start with @@: %q", line)
		}
		chunk := applyPatchChunk{
			ContextHint: strings.TrimSpace(strings.TrimPrefix(line, "@@")),
		}
		i++
		for i < end {
			line = lines[i]
			if strings.HasPrefix(line, "@@") || (strings.HasPrefix(line, "*** ") && line != "*** End of File") {
				break
			}
			switch {
			case line == "*** End of File":
				chunk.EndOfFile = true
			case strings.HasPrefix(line, " "):
				text := strings.TrimPrefix(line, " ")
				chunk.OldLines = append(chunk.OldLines, text)
				chunk.NewLines = append(chunk.NewLines, text)
			case strings.HasPrefix(line, "-"):
				chunk.OldLines = append(chunk.OldLines, strings.TrimPrefix(line, "-"))
			case strings.HasPrefix(line, "+"):
				chunk.NewLines = append(chunk.NewLines, strings.TrimPrefix(line, "+"))
			default:
				return nil, i, fmt.Errorf("Update File change lines must start with space, -, or +: %q", line)
			}
			i++
		}
		if len(chunk.OldLines) == 0 && len(chunk.NewLines) == 0 {
			return nil, i, errors.New("Update File chunk is empty")
		}
		chunks = append(chunks, chunk)
	}
	return chunks, i, nil
}

func (t *ApplyPatchTool) applyHunk(hunk applyPatchHunk, dryRun bool) (applyPatchFileResult, error) {
	switch hunk.Type {
	case "add":
		return t.applyAddHunk(hunk, dryRun)
	case "update":
		return t.applyUpdateHunk(hunk, dryRun)
	case "delete":
		return t.applyDeleteHunk(hunk, dryRun)
	default:
		return applyPatchFileResult{}, fmt.Errorf("unknown hunk type %q", hunk.Type)
	}
}

func (t *ApplyPatchTool) applyAddHunk(hunk applyPatchHunk, dryRun bool) (applyPatchFileResult, error) {
	resolved, err := t.env.ResolvePath(hunk.Path)
	if err != nil {
		return applyPatchFileResult{}, err
	}
	if err := t.rejectSensitivePatchPath(resolved, "add"); err != nil {
		return applyPatchFileResult{}, err
	}
	if _, err := os.Stat(resolved); err == nil {
		return applyPatchFileResult{}, fmt.Errorf("file already exists: %s", hunk.Path)
	} else if !os.IsNotExist(err) {
		return applyPatchFileResult{}, fmt.Errorf("stat file: %w", err)
	}
	content := joinPatchLines(hunk.Contents)
	if dryRun {
		return applyPatchFileResult{
			Path:   t.env.NormalizeDisplayPath(resolved),
			Action: "add",
			Diff: DiffResult{
				NewFile: true,
				Lines:   countContentLines(content),
			},
		}, nil
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return applyPatchFileResult{}, fmt.Errorf("create parent directory: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return applyPatchFileResult{}, fmt.Errorf("write file: %w", err)
	}
	t.notifyFileChanged(resolved)
	return applyPatchFileResult{
		Path:   t.env.NormalizeDisplayPath(resolved),
		Action: "add",
		Diff: DiffResult{
			NewFile: true,
			Lines:   countContentLines(content),
		},
	}, nil
}

func (t *ApplyPatchTool) applyUpdateHunk(hunk applyPatchHunk, dryRun bool) (applyPatchFileResult, error) {
	resolved, err := t.env.ResolvePath(hunk.Path)
	if err != nil {
		return applyPatchFileResult{}, err
	}
	if err := t.rejectSensitivePatchPath(resolved, "update"); err != nil {
		return applyPatchFileResult{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return applyPatchFileResult{}, fmt.Errorf("read file to update %s: %w", hunk.Path, err)
	}
	if info.IsDir() {
		return applyPatchFileResult{}, fmt.Errorf("path is a directory: %s", hunk.Path)
	}
	oldBytes, err := os.ReadFile(resolved)
	if err != nil {
		return applyPatchFileResult{}, fmt.Errorf("read file: %w", err)
	}
	oldContent := string(oldBytes)
	newContent, err := applyPatchChunks(oldContent, hunk.Chunks)
	if err != nil {
		return applyPatchFileResult{}, fmt.Errorf("%s: %w", hunk.Path, err)
	}
	if newContent == oldContent && hunk.MovePath == "" {
		return applyPatchFileResult{}, fmt.Errorf("no changes for %s", hunk.Path)
	}

	target := resolved
	action := "update"
	displayMovePath := ""
	if hunk.MovePath != "" {
		target, err = t.env.ResolvePath(hunk.MovePath)
		if err != nil {
			return applyPatchFileResult{}, err
		}
		if err := t.rejectSensitivePatchPath(target, "move to"); err != nil {
			return applyPatchFileResult{}, err
		}
		if _, err := os.Stat(target); err == nil {
			return applyPatchFileResult{}, fmt.Errorf("move target already exists: %s", hunk.MovePath)
		} else if !os.IsNotExist(err) {
			return applyPatchFileResult{}, fmt.Errorf("stat move target: %w", err)
		}
		action = "move"
		displayMovePath = t.env.NormalizeDisplayPath(target)
	}

	if dryRun {
		return applyPatchFileResult{
			Path:     t.env.NormalizeDisplayPath(resolved),
			MovePath: displayMovePath,
			Action:   action,
			Diff:     computeDiff(oldContent, newContent, 3),
		}, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return applyPatchFileResult{}, fmt.Errorf("create parent directory: %w", err)
	}
	if err := os.WriteFile(target, []byte(newContent), info.Mode().Perm()); err != nil {
		return applyPatchFileResult{}, fmt.Errorf("write file: %w", err)
	}
	t.notifyFileChanged(target)
	if hunk.MovePath != "" {
		if err := os.Remove(resolved); err != nil {
			return applyPatchFileResult{}, fmt.Errorf("remove moved source: %w", err)
		}
		t.notifyFileChanged(resolved)
	}

	return applyPatchFileResult{
		Path:     t.env.NormalizeDisplayPath(resolved),
		MovePath: displayMovePath,
		Action:   action,
		Diff:     computeDiff(oldContent, newContent, 3),
	}, nil
}

func (t *ApplyPatchTool) applyDeleteHunk(hunk applyPatchHunk, dryRun bool) (applyPatchFileResult, error) {
	resolved, err := t.env.ResolvePath(hunk.Path)
	if err != nil {
		return applyPatchFileResult{}, err
	}
	if err := t.rejectSensitivePatchPath(resolved, "delete"); err != nil {
		return applyPatchFileResult{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return applyPatchFileResult{}, fmt.Errorf("read file to delete %s: %w", hunk.Path, err)
	}
	if info.IsDir() {
		return applyPatchFileResult{}, fmt.Errorf("path is a directory: %s", hunk.Path)
	}
	oldBytes, err := os.ReadFile(resolved)
	if err != nil {
		return applyPatchFileResult{}, fmt.Errorf("read file: %w", err)
	}
	if dryRun {
		return applyPatchFileResult{
			Path:   t.env.NormalizeDisplayPath(resolved),
			Action: "delete",
			Diff:   computeDiff(string(oldBytes), "", 3),
		}, nil
	}
	if err := os.Remove(resolved); err != nil {
		return applyPatchFileResult{}, fmt.Errorf("delete file: %w", err)
	}
	t.notifyFileChanged(resolved)
	return applyPatchFileResult{
		Path:   t.env.NormalizeDisplayPath(resolved),
		Action: "delete",
		Diff:   computeDiff(string(oldBytes), "", 3),
	}, nil
}

func (t *ApplyPatchTool) rejectSensitivePatchPath(absPath, action string) error {
	return rejectSensitiveToolPath(t.env, "apply_patch", action, absPath)
}

func (t *ApplyPatchTool) notifyFileChanged(absPath string) {
	if t.env.OnFileChanged != nil {
		t.env.OnFileChanged(absPath)
	}
}

func applyPatchChunks(content string, chunks []applyPatchChunk) (string, error) {
	lines, trailingNewline := splitPatchContentLines(content)
	cursor := 0
	for _, chunk := range chunks {
		idx, err := findPatchChunk(lines, chunk.OldLines, cursor, chunk.EndOfFile)
		if err != nil {
			return "", err
		}
		next := make([]string, 0, len(lines)-len(chunk.OldLines)+len(chunk.NewLines))
		next = append(next, lines[:idx]...)
		next = append(next, chunk.NewLines...)
		next = append(next, lines[idx+len(chunk.OldLines):]...)
		lines = next
		cursor = idx + len(chunk.NewLines)
	}
	return joinContentLines(lines, trailingNewline), nil
}

func splitPatchContentLines(content string) ([]string, bool) {
	if content == "" {
		return nil, false
	}
	trailingNewline := strings.HasSuffix(content, "\n")
	if trailingNewline {
		content = strings.TrimSuffix(content, "\n")
	}
	if content == "" {
		return []string{}, trailingNewline
	}
	return strings.Split(content, "\n"), trailingNewline
}

func joinContentLines(lines []string, trailingNewline bool) string {
	if len(lines) == 0 {
		if trailingNewline {
			return "\n"
		}
		return ""
	}
	content := strings.Join(lines, "\n")
	if trailingNewline {
		content += "\n"
	}
	return content
}

func joinPatchLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func countContentLines(content string) int {
	if content == "" {
		return 0
	}
	count := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		count++
	}
	return count
}

func findPatchChunk(lines, oldLines []string, cursor int, endOfFile bool) (int, error) {
	if len(oldLines) == 0 {
		if endOfFile {
			return len(lines), nil
		}
		return cursor, nil
	}
	if cursor < 0 || cursor > len(lines) {
		cursor = 0
	}

	matches := findLineSequence(lines, oldLines, cursor)
	if len(matches) == 0 && cursor > 0 {
		matches = findLineSequence(lines, oldLines, 0)
	}
	if len(matches) == 0 {
		return 0, fmt.Errorf("failed to find expected lines:\n%s", strings.Join(oldLines, "\n"))
	}
	if len(matches) > 1 {
		return 0, fmt.Errorf("expected lines are ambiguous (%d matches); add more context", len(matches))
	}
	return matches[0], nil
}

func findLineSequence(lines, needle []string, start int) []int {
	if len(needle) == 0 || len(needle) > len(lines) {
		return nil
	}
	var matches []int
	for i := start; i <= len(lines)-len(needle); i++ {
		ok := true
		for j := range needle {
			if lines[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			matches = append(matches, i)
		}
	}
	return matches
}
