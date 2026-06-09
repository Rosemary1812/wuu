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
			"- Updating, moving, or deleting existing files requires a fresh prior read_file result or expected_old_shas mapping each source path to read_file file_sha\n" +
			"- expected_old_sha may be used for a single existing-file patch; use expected_old_shas for multi-file patches\n" +
			"- Returns changed_files, hunk_count, provenance, per-file old/new sha, and structured diffs showing what changed",
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
				"expected_old_sha": map[string]any{
					"type":        "string",
					"description": "Optional sha256 digest from read_file file_sha for a single existing source file.",
				},
				"expected_old_shas": map[string]any{
					"type":                 "object",
					"additionalProperties": map[string]any{"type": "string"},
					"description":          "Optional mapping of patch source path to read_file file_sha for each updated, moved, or deleted file.",
				},
			},
			"required": []string{"patchText"},
		},
	}
}

func (t *ApplyPatchTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		PatchText       string            `json:"patchText"`
		Patch           string            `json:"patch"`
		PatchText2      string            `json:"patch_text"`
		DryRun          bool              `json:"dry_run"`
		DryRun2         bool              `json:"dryRun"`
		ExpectedOldSHA  string            `json:"expected_old_sha"`
		ExpectedOldSHAs map[string]string `json:"expected_old_shas"`
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
	if !dryRun {
		if err := t.validatePatchBaselines(patch, strings.TrimSpace(args.ExpectedOldSHA), args.ExpectedOldSHAs); err != nil {
			return "", fmt.Errorf("apply_patch verification failed: %w", err)
		}
	}

	files := make([]applyPatchFileResult, 0, len(patch.Hunks))
	changedFiles := make([]string, 0, len(patch.Hunks))
	plans := make([]applyPatchHunkPlan, 0, len(patch.Hunks))
	for _, hunk := range patch.Hunks {
		plan, err := t.planHunk(hunk)
		if err != nil {
			return "", fmt.Errorf("apply_patch verification failed: %w", err)
		}
		plans = append(plans, plan)
		files = append(files, plan.Result)
		changedFiles = append(changedFiles, plan.Result.changedPath())
	}

	if !dryRun {
		snapshots, err := snapshotPatchPlans(plans)
		if err != nil {
			return "", fmt.Errorf("apply_patch verification failed: %w", err)
		}
		if err := t.commitPatchPlans(plans); err != nil {
			_ = rollbackPatchSnapshots(snapshots)
			return "", fmt.Errorf("apply_patch apply failed: %w", err)
		}
		t.notifyPatchPlans(plans)
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
	Path       string     `json:"path"`
	MovePath   string     `json:"move_path,omitempty"`
	Action     string     `json:"action"`
	OldFileSHA string     `json:"old_file_sha,omitempty"`
	NewFileSHA string     `json:"new_file_sha,omitempty"`
	Diff       DiffResult `json:"diff"`
}

type applyPatchHunkPlan struct {
	Result       applyPatchFileResult
	SourceAbs    string
	TargetAbs    string
	Content      []byte
	Mode         os.FileMode
	WriteTarget  bool
	RemoveSource bool
	DeleteSource bool
	NotifyPaths  []string
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

type applyPatchBaselineTarget struct {
	Path        string
	DisplayPath string
	AbsPath     string
	Action      string
}

func (t *ApplyPatchTool) validatePatchBaselines(patch applyPatch, expectedOldSHA string, expectedOldSHAs map[string]string) error {
	targets, err := t.patchBaselineTargets(patch)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}
	if expectedOldSHA != "" && len(targets) > 1 && len(expectedOldSHAs) == 0 {
		return errors.New("expected_old_sha only supports single existing-file patches. Use expected_old_shas for multi-file patches")
	}

	expectedByPath := t.normalizeExpectedPatchSHAs(expectedOldSHAs)
	for _, target := range targets {
		expected := expectedByPath[normalizePatchPathKey(target.DisplayPath)]
		if expected == "" {
			expected = expectedByPath[normalizePatchPathKey(target.Path)]
		}
		if expected == "" && expectedOldSHA != "" && len(targets) == 1 {
			expected = expectedOldSHA
		}
		if err := t.validatePatchBaselineTarget(target, expected); err != nil {
			return err
		}
	}
	return nil
}

func (t *ApplyPatchTool) patchBaselineTargets(patch applyPatch) ([]applyPatchBaselineTarget, error) {
	seen := make(map[string]bool)
	targets := make([]applyPatchBaselineTarget, 0, len(patch.Hunks))
	for _, hunk := range patch.Hunks {
		switch hunk.Type {
		case "update", "delete":
		default:
			continue
		}
		resolved, err := t.env.ResolvePath(hunk.Path)
		if err != nil {
			return nil, err
		}
		action := hunk.Type
		if hunk.Type == "update" && hunk.MovePath != "" {
			action = "move"
		}
		if err := t.rejectSensitivePatchPath(resolved, action); err != nil {
			return nil, err
		}
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		targets = append(targets, applyPatchBaselineTarget{
			Path:        hunk.Path,
			DisplayPath: t.env.NormalizeDisplayPath(resolved),
			AbsPath:     resolved,
			Action:      action,
		})
	}
	return targets, nil
}

func (t *ApplyPatchTool) normalizeExpectedPatchSHAs(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values)*2)
	for rawPath, sha := range values {
		rawPath = strings.TrimSpace(rawPath)
		sha = strings.TrimSpace(sha)
		if rawPath == "" || sha == "" {
			continue
		}
		out[normalizePatchPathKey(rawPath)] = sha
		if resolved, err := t.env.ResolvePath(rawPath); err == nil {
			out[normalizePatchPathKey(t.env.NormalizeDisplayPath(resolved))] = sha
		}
	}
	return out
}

func normalizePatchPathKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func (t *ApplyPatchTool) validatePatchBaselineTarget(target applyPatchBaselineTarget, expectedOldSHA string) error {
	info, err := os.Stat(target.AbsPath)
	if err != nil {
		return fmt.Errorf("read file to %s %s: %w", target.Action, target.DisplayPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory: %s", target.DisplayPath)
	}
	content, err := os.ReadFile(target.AbsPath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	currentSHA := sha256Hex(content)
	if expectedOldSHA != "" {
		if normalizeFileSHA(expectedOldSHA) != currentSHA {
			return fmt.Errorf("expected_old_sha for %s does not match current file. Use read_file again before applying patch", target.DisplayPath)
		}
		return nil
	}

	readEntry, ok := t.env.GetReadEntry(target.AbsPath)
	if !ok {
		return fmt.Errorf("%s has not been read yet. Use read_file first or pass expected_old_shas[%q] from read_file before applying patch", target.DisplayPath, target.DisplayPath)
	}
	if readEntry.Size != 0 && readEntry.Size != info.Size() {
		return fmt.Errorf("%s changed since last read. Use read_file again before applying patch", target.DisplayPath)
	}
	if readEntry.ContentSHA256 != "" && readEntry.ContentSHA256 != currentSHA {
		return fmt.Errorf("%s changed since last read. Use read_file again before applying patch", target.DisplayPath)
	}
	if readEntry.ContentSHA256 == "" && !readEntryMatchesInfo(readEntry, info) {
		return fmt.Errorf("%s changed since last read. Use read_file again before applying patch", target.DisplayPath)
	}
	return nil
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

func (t *ApplyPatchTool) planHunk(hunk applyPatchHunk) (applyPatchHunkPlan, error) {
	switch hunk.Type {
	case "add":
		return t.planAddHunk(hunk)
	case "update":
		return t.planUpdateHunk(hunk)
	case "delete":
		return t.planDeleteHunk(hunk)
	default:
		return applyPatchHunkPlan{}, fmt.Errorf("unknown hunk type %q", hunk.Type)
	}
}

func (t *ApplyPatchTool) planAddHunk(hunk applyPatchHunk) (applyPatchHunkPlan, error) {
	resolved, err := t.env.ResolvePath(hunk.Path)
	if err != nil {
		return applyPatchHunkPlan{}, err
	}
	if err := t.rejectSensitivePatchPath(resolved, "add"); err != nil {
		return applyPatchHunkPlan{}, err
	}
	if _, err := os.Stat(resolved); err == nil {
		return applyPatchHunkPlan{}, fmt.Errorf("file already exists: %s", hunk.Path)
	} else if !os.IsNotExist(err) {
		return applyPatchHunkPlan{}, fmt.Errorf("stat file: %w", err)
	}
	content := []byte(joinPatchLines(hunk.Contents))
	return applyPatchHunkPlan{
		Result: applyPatchFileResult{
			Path:       t.env.NormalizeDisplayPath(resolved),
			Action:     "add",
			NewFileSHA: formatFileSHA(sha256Hex(content)),
			Diff: DiffResult{
				NewFile: true,
				Lines:   countContentLines(string(content)),
			},
		},
		TargetAbs:   resolved,
		Content:     content,
		Mode:        0o644,
		WriteTarget: true,
		NotifyPaths: []string{resolved},
	}, nil
}

func (t *ApplyPatchTool) planUpdateHunk(hunk applyPatchHunk) (applyPatchHunkPlan, error) {
	resolved, err := t.env.ResolvePath(hunk.Path)
	if err != nil {
		return applyPatchHunkPlan{}, err
	}
	if err := t.rejectSensitivePatchPath(resolved, "update"); err != nil {
		return applyPatchHunkPlan{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return applyPatchHunkPlan{}, fmt.Errorf("read file to update %s: %w", hunk.Path, err)
	}
	if info.IsDir() {
		return applyPatchHunkPlan{}, fmt.Errorf("path is a directory: %s", hunk.Path)
	}
	oldBytes, err := os.ReadFile(resolved)
	if err != nil {
		return applyPatchHunkPlan{}, fmt.Errorf("read file: %w", err)
	}
	oldContent := string(oldBytes)
	newContent, err := applyPatchChunks(oldContent, hunk.Chunks)
	if err != nil {
		return applyPatchHunkPlan{}, fmt.Errorf("%s: %w", hunk.Path, err)
	}
	if newContent == oldContent && hunk.MovePath == "" {
		return applyPatchHunkPlan{}, fmt.Errorf("no changes for %s", hunk.Path)
	}

	target := resolved
	action := "update"
	displayMovePath := ""
	removeSource := false
	notifyPaths := []string{resolved}
	if hunk.MovePath != "" {
		target, err = t.env.ResolvePath(hunk.MovePath)
		if err != nil {
			return applyPatchHunkPlan{}, err
		}
		if err := t.rejectSensitivePatchPath(target, "move to"); err != nil {
			return applyPatchHunkPlan{}, err
		}
		if _, err := os.Stat(target); err == nil {
			return applyPatchHunkPlan{}, fmt.Errorf("move target already exists: %s", hunk.MovePath)
		} else if !os.IsNotExist(err) {
			return applyPatchHunkPlan{}, fmt.Errorf("stat move target: %w", err)
		}
		action = "move"
		displayMovePath = t.env.NormalizeDisplayPath(target)
		removeSource = true
		notifyPaths = []string{target, resolved}
	}

	newBytes := []byte(newContent)
	return applyPatchHunkPlan{
		Result: applyPatchFileResult{
			Path:       t.env.NormalizeDisplayPath(resolved),
			MovePath:   displayMovePath,
			Action:     action,
			OldFileSHA: formatFileSHA(sha256Hex(oldBytes)),
			NewFileSHA: formatFileSHA(sha256Hex(newBytes)),
			Diff:       computeDiff(oldContent, newContent, 3),
		},
		SourceAbs:    resolved,
		TargetAbs:    target,
		Content:      newBytes,
		Mode:         info.Mode().Perm(),
		WriteTarget:  true,
		RemoveSource: removeSource,
		NotifyPaths:  notifyPaths,
	}, nil
}

func (t *ApplyPatchTool) planDeleteHunk(hunk applyPatchHunk) (applyPatchHunkPlan, error) {
	resolved, err := t.env.ResolvePath(hunk.Path)
	if err != nil {
		return applyPatchHunkPlan{}, err
	}
	if err := t.rejectSensitivePatchPath(resolved, "delete"); err != nil {
		return applyPatchHunkPlan{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return applyPatchHunkPlan{}, fmt.Errorf("read file to delete %s: %w", hunk.Path, err)
	}
	if info.IsDir() {
		return applyPatchHunkPlan{}, fmt.Errorf("path is a directory: %s", hunk.Path)
	}
	oldBytes, err := os.ReadFile(resolved)
	if err != nil {
		return applyPatchHunkPlan{}, fmt.Errorf("read file: %w", err)
	}
	return applyPatchHunkPlan{
		Result: applyPatchFileResult{
			Path:       t.env.NormalizeDisplayPath(resolved),
			Action:     "delete",
			OldFileSHA: formatFileSHA(sha256Hex(oldBytes)),
			Diff:       computeDiff(string(oldBytes), "", 3),
		},
		SourceAbs:    resolved,
		DeleteSource: true,
		NotifyPaths:  []string{resolved},
	}, nil
}

type patchPathSnapshot struct {
	Path    string
	Exists  bool
	Content []byte
	Mode    os.FileMode
}

func snapshotPatchPlans(plans []applyPatchHunkPlan) ([]patchPathSnapshot, error) {
	seen := map[string]bool{}
	paths := make([]string, 0, len(plans)*2)
	addPath := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	for _, plan := range plans {
		addPath(plan.SourceAbs)
		addPath(plan.TargetAbs)
	}

	snapshots := make([]patchPathSnapshot, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				snapshots = append(snapshots, patchPathSnapshot{Path: path})
				continue
			}
			return nil, fmt.Errorf("snapshot %s: %w", path, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("snapshot path is a directory: %s", path)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", path, err)
		}
		snapshots = append(snapshots, patchPathSnapshot{
			Path:    path,
			Exists:  true,
			Content: content,
			Mode:    info.Mode().Perm(),
		})
	}
	return snapshots, nil
}

func rollbackPatchSnapshots(snapshots []patchPathSnapshot) error {
	var firstErr error
	for i := len(snapshots) - 1; i >= 0; i-- {
		snapshot := snapshots[i]
		if snapshot.Exists {
			if err := os.MkdirAll(filepath.Dir(snapshot.Path), 0o755); err != nil && firstErr == nil {
				firstErr = err
				continue
			}
			if err := os.WriteFile(snapshot.Path, snapshot.Content, snapshot.Mode); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := os.Remove(snapshot.Path); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (t *ApplyPatchTool) commitPatchPlans(plans []applyPatchHunkPlan) error {
	for _, plan := range plans {
		if plan.WriteTarget {
			if err := os.MkdirAll(filepath.Dir(plan.TargetAbs), 0o755); err != nil {
				return fmt.Errorf("create parent directory: %w", err)
			}
			if err := os.WriteFile(plan.TargetAbs, plan.Content, plan.Mode); err != nil {
				return fmt.Errorf("write file: %w", err)
			}
		}
		if plan.RemoveSource {
			if err := os.Remove(plan.SourceAbs); err != nil {
				return fmt.Errorf("remove moved source: %w", err)
			}
		}
		if plan.DeleteSource {
			if err := os.Remove(plan.SourceAbs); err != nil {
				return fmt.Errorf("delete file: %w", err)
			}
		}
	}
	return nil
}

func (t *ApplyPatchTool) notifyPatchPlans(plans []applyPatchHunkPlan) {
	seen := map[string]bool{}
	for _, plan := range plans {
		for _, path := range plan.NotifyPaths {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			t.notifyFileChanged(path)
		}
	}
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
