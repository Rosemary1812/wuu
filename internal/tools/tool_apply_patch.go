package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolresult"
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
		Name:        "apply_patch",
		Description: "Apply a structured workspace patch using *** Begin Patch / *** End Patch. Supports Add, Update, optional Move, and Delete sections. Update and delete hunks are validated against the current file content; stale or ambiguous anchors fail. dry_run validates without writing.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"patchText": map[string]any{
					"type":        "string",
					"description": "Full patch text including *** Begin Patch and *** End Patch markers.",
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

func (t *ApplyPatchTool) ValidateInput(argsJSON string) error {
	var args struct {
		PatchText  string `json:"patchText"`
		Patch      string `json:"patch"`
		PatchText2 string `json:"patch_text"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	if strings.TrimSpace(args.PatchText) == "" &&
		strings.TrimSpace(args.PatchText2) == "" &&
		strings.TrimSpace(args.Patch) == "" {
		return errors.New("apply_patch requires patchText")
	}
	return nil
}

func (t *ApplyPatchTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	result, err := t.ExecuteResult(ctx, argsJSON)
	return result.TextProjection(), err
}

func (t *ApplyPatchTool) ExecuteResult(ctx context.Context, argsJSON string) (toolresult.Result, error) {
	var args struct {
		PatchText  string `json:"patchText"`
		Patch      string `json:"patch"`
		PatchText2 string `json:"patch_text"`
		DryRun     bool   `json:"dry_run"`
		DryRun2    bool   `json:"dryRun"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return toolresult.Result{}, err
	}
	patchText := args.PatchText
	if strings.TrimSpace(patchText) == "" {
		patchText = args.PatchText2
	}
	if strings.TrimSpace(patchText) == "" {
		patchText = args.Patch
	}
	if strings.TrimSpace(patchText) == "" {
		return toolresult.Result{}, errors.New("apply_patch requires patchText")
	}

	patch, err := parseApplyPatch(patchText)
	if err != nil {
		return toolresult.Result{}, fmt.Errorf("apply_patch verification failed: %w", err)
	}
	if len(patch.Hunks) == 0 {
		return toolresult.Result{}, errors.New("apply_patch verification failed: no hunks found")
	}

	dryRun := args.DryRun || args.DryRun2
	revisionBefore := workspaceRevision(ctx, t.env.RevisionRoot(ctx))

	files := make([]applyPatchFileResult, 0, len(patch.Hunks))
	changedFiles := make([]string, 0, len(patch.Hunks))
	plans := make([]applyPatchHunkPlan, 0, len(patch.Hunks))
	for _, hunk := range patch.Hunks {
		plan, err := t.planHunk(ctx, hunk)
		if err != nil {
			return toolresult.Result{}, fmt.Errorf("apply_patch verification failed: %w", err)
		}
		plans = append(plans, plan)
		files = append(files, plan.Result)
		changedFiles = append(changedFiles, plan.Result.changedPath())
	}

	var snapshots []patchPathSnapshot
	if !dryRun {
		var err error
		snapshots, err = snapshotPatchPlans(plans)
		if err != nil {
			return toolresult.Result{}, fmt.Errorf("apply_patch verification failed: %w", err)
		}
		if err := t.commitPatchPlans(plans); err != nil {
			_ = rollbackPatchSnapshots(snapshots)
			return toolresult.Result{}, fmt.Errorf("apply_patch apply failed: %w", err)
		}
		t.recordPatchPlanBaselines(plans)
		t.notifyPatchPlans(plans)
	}
	revisionAfter := workspaceRevision(ctx, t.env.RevisionRoot(ctx))
	warnings := []string(nil)
	riskSummary := summarizeApplyPatchRisk(files, len(patch.Hunks))
	var journal *applyPatchJournalManifest
	if !dryRun {
		var err error
		journal, err = t.writePatchJournal(patchText, files, uniqueNonEmptyStrings(changedFiles), len(patch.Hunks), revisionBefore, revisionAfter, snapshots, riskSummary)
		if err != nil {
			warnings = append(warnings, "patch journal could not be written: "+err.Error())
		}
	}

	detail := map[string]any{
		"dry_run":            dryRun,
		"hunk_count":         len(patch.Hunks),
		"changed_files":      uniqueNonEmptyStrings(changedFiles),
		"risk_summary":       riskSummary,
		"workspace_revision": revisionAfter,
		"files":              files,
	}
	if journal != nil {
		detail["manifest_path"] = journal.ManifestPath
		detail["patch_path"] = journal.PatchPath
	}
	if len(warnings) > 0 {
		detail["warnings"] = warnings
	}
	structured, err := json.Marshal(detail)
	if err != nil {
		return toolresult.Result{}, fmt.Errorf("marshal apply_patch detail: %w", err)
	}
	return toolresult.Result{
		Content: []toolresult.ContentPart{{
			Type: toolresult.ContentTypeText,
			Text: applyPatchModelSummary(dryRun, files),
		}},
		StructuredContent: structured,
	}, nil
}

func applyPatchModelSummary(dryRun bool, files []applyPatchFileResult) string {
	header := "Success. Updated the following files:"
	if dryRun {
		header = "Patch validation succeeded. The following files would be updated:"
	}

	records := make([]string, 0, len(files))
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		path := strings.TrimSpace(file.Path)
		displayPath := path
		if movePath := strings.TrimSpace(file.MovePath); movePath != "" {
			displayPath = path + " -> " + movePath
		}
		action := strings.ToLower(strings.TrimSpace(file.Action))
		key := action + "\x00" + displayPath
		if displayPath == "" || seen[key] {
			continue
		}
		seen[key] = true
		marker := "M"
		switch action {
		case "add":
			marker = "A"
		case "delete":
			marker = "D"
		}
		records = append(records, marker+" "+displayPath)
	}

	return strings.Join(append([]string{header}, records...), "\n")
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

type applyPatchJournalManifest struct {
	ID                      string                      `json:"id"`
	CreatedAt               time.Time                   `json:"created_at"`
	RestoredAt              time.Time                   `json:"restored_at,omitempty"`
	Tool                    string                      `json:"tool"`
	SessionID               string                      `json:"session_id,omitempty"`
	AgentID                 string                      `json:"agent_id,omitempty"`
	WorkspaceRevisionBefore string                      `json:"workspace_revision_before,omitempty"`
	WorkspaceRevisionAfter  string                      `json:"workspace_revision_after,omitempty"`
	ChangedFiles            []string                    `json:"changed_files"`
	HunkCount               int                         `json:"hunk_count"`
	ManifestPath            string                      `json:"manifest_path"`
	PatchPath               string                      `json:"patch_path"`
	Snapshots               []applyPatchJournalSnapshot `json:"snapshots,omitempty"`
	Files                   []applyPatchFileResult      `json:"files"`
	RiskSummary             applyPatchRiskSummary       `json:"risk_summary"`
	Provenance              map[string]string           `json:"provenance"`
	RollbackHint            string                      `json:"rollback_hint,omitempty"`
}

type applyPatchRiskSummary struct {
	FileCount      int            `json:"file_count"`
	HunkCount      int            `json:"hunk_count"`
	AddedLines     int            `json:"added_lines"`
	DeletedLines   int            `json:"deleted_lines"`
	Actions        map[string]int `json:"actions,omitempty"`
	MultiFile      bool           `json:"multi_file,omitempty"`
	ContainsDelete bool           `json:"contains_delete,omitempty"`
	ContainsMove   bool           `json:"contains_move,omitempty"`
	RiskLevel      string         `json:"risk_level"`
	ReviewHint     string         `json:"review_hint"`
}

type applyPatchJournalSnapshot struct {
	Path         string `json:"path"`
	Existed      bool   `json:"existed"`
	FileSHA      string `json:"file_sha,omitempty"`
	Size         int64  `json:"size,omitempty"`
	Mode         string `json:"mode,omitempty"`
	SnapshotPath string `json:"snapshot_path,omitempty"`
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

func summarizeApplyPatchRisk(files []applyPatchFileResult, hunkCount int) applyPatchRiskSummary {
	summary := applyPatchRiskSummary{
		FileCount: len(files),
		HunkCount: hunkCount,
		Actions:   map[string]int{},
		RiskLevel: "low",
	}
	for _, file := range files {
		action := strings.TrimSpace(file.Action)
		if action == "" {
			action = "unknown"
		}
		summary.Actions[action]++
		switch action {
		case "add":
			summary.AddedLines += file.Diff.Lines
		case "delete":
			summary.ContainsDelete = true
		case "move":
			summary.ContainsMove = true
		}
		added, deleted := diffLineChangeCounts(file.Diff)
		summary.AddedLines += added
		summary.DeletedLines += deleted
	}
	summary.MultiFile = summary.FileCount > 1
	totalChanged := summary.AddedLines + summary.DeletedLines
	switch {
	case summary.ContainsDelete || summary.ContainsMove || summary.FileCount > 5 || totalChanged > 300:
		summary.RiskLevel = "high"
		summary.ReviewHint = "Inspect the full diff and rollback journal before synthesis; run targeted validation after applying the patch."
	case summary.MultiFile || totalChanged > 50:
		summary.RiskLevel = "medium"
		summary.ReviewHint = "Review changed files as a set and run targeted validation before finishing."
	default:
		summary.ReviewHint = "Review the compact diff and run the smallest relevant validation before finishing."
	}
	return summary
}

func diffLineChangeCounts(diff DiffResult) (added, deleted int) {
	for _, hunk := range diff.Hunks {
		for _, line := range hunk.Lines {
			switch line.Op {
			case "insert":
				added++
			case "delete":
				deleted++
			}
		}
	}
	return added, deleted
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

func (t *ApplyPatchTool) planHunk(ctx context.Context, hunk applyPatchHunk) (applyPatchHunkPlan, error) {
	switch hunk.Type {
	case "add":
		return t.planAddHunk(ctx, hunk)
	case "update":
		return t.planUpdateHunk(ctx, hunk)
	case "delete":
		return t.planDeleteHunk(ctx, hunk)
	default:
		return applyPatchHunkPlan{}, fmt.Errorf("unknown hunk type %q", hunk.Type)
	}
}

func (t *ApplyPatchTool) planAddHunk(ctx context.Context, hunk applyPatchHunk) (applyPatchHunkPlan, error) {
	resolved, err := t.env.ResolvePath(hunk.Path)
	if err != nil {
		return applyPatchHunkPlan{}, err
	}
	if err := t.rejectSensitivePatchPath(resolved, "add"); err != nil {
		return applyPatchHunkPlan{}, err
	}
	// Worktree-bound execution: rebase onto the checkout only after the
	// sandbox and sensitive-path checks above accepted the workspace path.
	resolved, err = t.env.ExecPath(ctx, resolved)
	if err != nil {
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
			Path:       t.env.NormalizeDisplayPathExec(ctx, resolved),
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

func (t *ApplyPatchTool) planUpdateHunk(ctx context.Context, hunk applyPatchHunk) (applyPatchHunkPlan, error) {
	resolved, err := t.env.ResolvePath(hunk.Path)
	if err != nil {
		return applyPatchHunkPlan{}, err
	}
	if err := t.rejectSensitivePatchPath(resolved, "update"); err != nil {
		return applyPatchHunkPlan{}, err
	}
	// Worktree-bound execution: rebase onto the checkout only after the
	// sandbox and sensitive-path checks above accepted the workspace path.
	resolved, err = t.env.ExecPath(ctx, resolved)
	if err != nil {
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
		target, err = t.env.ExecPath(ctx, target)
		if err != nil {
			return applyPatchHunkPlan{}, err
		}
		if _, err := os.Stat(target); err == nil {
			return applyPatchHunkPlan{}, fmt.Errorf("move target already exists: %s", hunk.MovePath)
		} else if !os.IsNotExist(err) {
			return applyPatchHunkPlan{}, fmt.Errorf("stat move target: %w", err)
		}
		action = "move"
		displayMovePath = t.env.NormalizeDisplayPathExec(ctx, target)
		removeSource = true
		notifyPaths = []string{target, resolved}
	}

	newBytes := []byte(newContent)
	return applyPatchHunkPlan{
		Result: applyPatchFileResult{
			Path:       t.env.NormalizeDisplayPathExec(ctx, resolved),
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

func (t *ApplyPatchTool) planDeleteHunk(ctx context.Context, hunk applyPatchHunk) (applyPatchHunkPlan, error) {
	resolved, err := t.env.ResolvePath(hunk.Path)
	if err != nil {
		return applyPatchHunkPlan{}, err
	}
	if err := t.rejectSensitivePatchPath(resolved, "delete"); err != nil {
		return applyPatchHunkPlan{}, err
	}
	// Worktree-bound execution: rebase onto the checkout only after the
	// sandbox and sensitive-path checks above accepted the workspace path.
	resolved, err = t.env.ExecPath(ctx, resolved)
	if err != nil {
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
			Path:       t.env.NormalizeDisplayPathExec(ctx, resolved),
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

func (t *ApplyPatchTool) recordPatchPlanBaselines(plans []applyPatchHunkPlan) {
	for _, plan := range plans {
		if plan.WriteTarget {
			t.env.RecordWriteBaseline(plan.TargetAbs, plan.Content)
		}
		if plan.RemoveSource || plan.DeleteSource {
			t.env.ForgetRead(plan.SourceAbs)
		}
	}
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

func (t *ApplyPatchTool) writePatchJournal(patchText string, files []applyPatchFileResult, changedFiles []string, hunkCount int, revisionBefore, revisionAfter string, snapshots []patchPathSnapshot, riskSummary applyPatchRiskSummary) (*applyPatchJournalManifest, error) {
	root := strings.TrimSpace(t.env.SessionDir)
	if root == "" {
		root = t.env.StateDir
	}
	if root == "" {
		return nil, errors.New("no session or state directory configured")
	}
	patchID := generatedApplyPatchJournalID(patchText)
	dir := filepath.Join(root, "patch-journal", patchID)
	filesDir := filepath.Join(dir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		return nil, fmt.Errorf("create patch journal directory: %w", err)
	}

	patchPath := filepath.Join(dir, "patch.txt")
	if err := os.WriteFile(patchPath, []byte(patchText), 0o644); err != nil {
		return nil, fmt.Errorf("write patch artifact: %w", err)
	}
	journalSnapshots, err := t.writePatchJournalSnapshots(filesDir, snapshots)
	if err != nil {
		return nil, err
	}

	manifestPath := filepath.Join(dir, "manifest.json")
	manifest := applyPatchJournalManifest{
		ID:                      patchID,
		CreatedAt:               time.Now().UTC(),
		Tool:                    "apply_patch",
		SessionID:               strings.TrimSpace(t.env.SessionID),
		AgentID:                 strings.TrimSpace(t.env.AgentID),
		WorkspaceRevisionBefore: revisionBefore,
		WorkspaceRevisionAfter:  revisionAfter,
		ChangedFiles:            append([]string(nil), changedFiles...),
		HunkCount:               hunkCount,
		ManifestPath:            manifestPath,
		PatchPath:               patchPath,
		Snapshots:               journalSnapshots,
		Files:                   append([]applyPatchFileResult(nil), files...),
		RiskSummary:             riskSummary,
		Provenance: map[string]string{
			"tool":   "apply_patch",
			"source": "model_tool_call",
		},
		RollbackHint: "Use the snapshots in this journal or a prior checkpoint to reconstruct rollback, then inspect git diff and rerun validation.",
	}
	if err := writeApplyPatchJournalManifest(manifestPath, manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (t *ApplyPatchTool) writePatchJournalSnapshots(filesDir string, snapshots []patchPathSnapshot) ([]applyPatchJournalSnapshot, error) {
	out := make([]applyPatchJournalSnapshot, 0, len(snapshots))
	for i, snapshot := range snapshots {
		entry := applyPatchJournalSnapshot{
			Path:    t.env.NormalizeDisplayPath(snapshot.Path),
			Existed: snapshot.Exists,
		}
		if snapshot.Exists {
			entry.FileSHA = formatFileSHA(sha256Hex(snapshot.Content))
			entry.Size = int64(len(snapshot.Content))
			entry.Mode = fmt.Sprintf("%04o", snapshot.Mode.Perm())
			snapshotName := fmt.Sprintf("%03d-%s.snapshot", i, sha256Hex([]byte(entry.Path))[:12])
			snapshotPath := filepath.Join(filesDir, snapshotName)
			if err := os.WriteFile(snapshotPath, snapshot.Content, snapshot.Mode.Perm()); err != nil {
				return nil, fmt.Errorf("write patch journal snapshot %s: %w", entry.Path, err)
			}
			entry.SnapshotPath = snapshotPath
		}
		out = append(out, entry)
	}
	return out, nil
}

func generatedApplyPatchJournalID(patchText string) string {
	hash := sha256Hex([]byte(patchText))
	return "patch-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hash[:12]
}

func writeApplyPatchJournalManifest(path string, manifest applyPatchJournalManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal patch journal: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write patch journal manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace patch journal manifest: %w", err)
	}
	return nil
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
		return 0, patchChunkMatchError{
			Kind:       "anchor_not_found",
			Expected:   oldLines,
			Candidates: closestPatchChunkCandidates(lines, oldLines, 3),
		}
	}
	if len(matches) > 1 {
		return 0, patchChunkMatchError{
			Kind:       "ambiguous_anchor",
			Expected:   oldLines,
			MatchCount: len(matches),
			Candidates: patchChunkMatchCandidates(lines, oldLines, matches, 5),
		}
	}
	return matches[0], nil
}

type patchChunkCandidate struct {
	StartLine int
	EndLine   int
	Score     int
	Snippet   []string
}

type patchChunkMatchError struct {
	Kind       string
	Expected   []string
	MatchCount int
	Candidates []patchChunkCandidate
}

func (e patchChunkMatchError) Error() string {
	var b strings.Builder
	switch e.Kind {
	case "ambiguous_anchor":
		fmt.Fprintf(&b, "ambiguous_anchor: expected lines matched %d locations; add more context", e.MatchCount)
	default:
		b.WriteString("anchor_not_found: failed to find expected lines")
	}
	if len(e.Expected) > 0 {
		b.WriteString("\nexpected:\n")
		b.WriteString(formatPatchErrorLines(e.Expected, 0, 8))
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
	b.WriteString("\nsafe_retry: read_file the target range and regenerate the patch against the current file_sha")
	return b.String()
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

func closestPatchChunkCandidates(lines, needle []string, limit int) []patchChunkCandidate {
	if len(lines) == 0 || len(needle) == 0 || limit <= 0 {
		return nil
	}
	windowLen := len(needle)
	if windowLen > len(lines) {
		windowLen = len(lines)
	}
	candidates := make([]patchChunkCandidate, 0, len(lines)-windowLen+1)
	for start := 0; start <= len(lines)-windowLen; start++ {
		window := lines[start : start+windowLen]
		candidates = append(candidates, patchChunkCandidate{
			StartLine: start + 1,
			EndLine:   start + windowLen,
			Score:     patchChunkCandidateScore(window, needle),
			Snippet:   append([]string(nil), window...),
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].StartLine < candidates[j].StartLine
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func patchChunkMatchCandidates(lines, needle []string, matches []int, limit int) []patchChunkCandidate {
	if len(matches) == 0 || limit <= 0 {
		return nil
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	out := make([]patchChunkCandidate, 0, len(matches))
	for _, start := range matches {
		end := start + len(needle)
		if end > len(lines) {
			end = len(lines)
		}
		out = append(out, patchChunkCandidate{
			StartLine: start + 1,
			EndLine:   end,
			Score:     len(needle) * 2,
			Snippet:   append([]string(nil), lines[start:end]...),
		})
	}
	return out
}

func patchChunkCandidateScore(window, needle []string) int {
	score := 0
	max := len(window)
	if len(needle) < max {
		max = len(needle)
	}
	for i := 0; i < max; i++ {
		windowLine := strings.TrimSpace(window[i])
		needleLine := strings.TrimSpace(needle[i])
		switch {
		case windowLine == needleLine:
			score += 2
		case windowLine != "" && needleLine != "" && (strings.Contains(windowLine, needleLine) || strings.Contains(needleLine, windowLine)):
			score++
		}
	}
	return score
}

func formatPatchErrorLines(lines []string, startLine, limit int) string {
	var b strings.Builder
	omitted := 0
	if len(lines) > limit {
		omitted = len(lines) - limit
		lines = lines[:limit]
	}
	for i, line := range lines {
		if startLine > 0 {
			fmt.Fprintf(&b, "  %d| %s\n", startLine+i, line)
			continue
		}
		fmt.Fprintf(&b, "  %s\n", line)
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "  ... %d more lines omitted\n", omitted)
	}
	return b.String()
}
