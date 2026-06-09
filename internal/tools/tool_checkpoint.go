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
	"unicode"

	"github.com/blueberrycongee/wuu/internal/providers"
)

const (
	checkpointMaxFiles         = 100
	checkpointMaxFileBytes     = 2 * 1024 * 1024
	checkpointIDGeneratedLimit = 80
)

type CheckpointTool struct{ env *Env }

func NewCheckpointTool(env *Env) *CheckpointTool { return &CheckpointTool{env: env} }

func (t *CheckpointTool) Name() string            { return "checkpoint" }
func (t *CheckpointTool) IsReadOnly() bool        { return false }
func (t *CheckpointTool) IsConcurrencySafe() bool { return false }

func (t *CheckpointTool) Classify(argsJSON string) ToolClassification {
	var args struct {
		Action string `json:"action"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return ToolClassification{Risk: ToolRiskHigh, Reason: "invalid checkpoint invocation"}
	}
	switch strings.TrimSpace(args.Action) {
	case "list":
		return ToolClassification{ReadOnly: true, ConcurrencySafe: true, Risk: ToolRiskLow, Reason: "checkpoint list is read-only"}
	case "create":
		return ToolClassification{ReadOnly: false, ConcurrencySafe: false, Risk: ToolRiskLow, Reason: "checkpoint create writes rollback artifact"}
	case "restore":
		return ToolClassification{ReadOnly: false, ConcurrencySafe: false, Destructive: true, Risk: ToolRiskHigh, Reason: "checkpoint restore mutates workspace files"}
	default:
		return ToolClassification{Risk: ToolRiskHigh, Reason: "unknown checkpoint action"}
	}
}

func (t *CheckpointTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "checkpoint",
		Description: "Creates, lists, and restores file-level workspace checkpoints.\n\n" +
			"Usage:\n" +
			"- Use action=create before risky edits to snapshot specific files\n" +
			"- Use action=list to inspect available checkpoints\n" +
			"- Use action=restore with checkpoint_id to roll files back to the saved snapshot\n" +
			"- create requires explicit file paths; directories, sensitive paths, and files over 2MB are rejected\n" +
			"- restore may overwrite or delete workspace files and returns workspace_revision after completion",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"description": "One of create, list, restore.",
				},
				"checkpoint_id": map[string]any{
					"type":        "string",
					"description": "Stable checkpoint id. Optional for create; required for restore.",
				},
				"paths": map[string]any{
					"type":        "array",
					"description": "Workspace-relative file paths to snapshot or restore. create requires at least one path; restore may omit paths to restore every file in the checkpoint.",
					"items":       map[string]any{"type": "string"},
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Short reason for the checkpoint or restore.",
				},
			},
			"required": []string{"action"},
		},
	}
}

func (t *CheckpointTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Action       string   `json:"action"`
		CheckpointID string   `json:"checkpoint_id"`
		Paths        []string `json:"paths"`
		Reason       string   `json:"reason"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	switch strings.TrimSpace(args.Action) {
	case "create":
		return t.createCheckpoint(ctx, args.CheckpointID, args.Paths, args.Reason)
	case "list":
		return t.listCheckpoints(ctx)
	case "restore":
		return t.restoreCheckpoint(ctx, args.CheckpointID, args.Paths, args.Reason)
	default:
		return "", errors.New("checkpoint action must be one of create, list, restore")
	}
}

type workspaceFileCheckpoint struct {
	ID                string                    `json:"id"`
	CreatedAt         time.Time                 `json:"created_at"`
	RestoredAt        time.Time                 `json:"restored_at,omitempty"`
	Reason            string                    `json:"reason,omitempty"`
	WorkspaceRevision string                    `json:"workspace_revision,omitempty"`
	ManifestPath      string                    `json:"manifest_path,omitempty"`
	Files             []workspaceCheckpointFile `json:"files"`
}

type workspaceCheckpointFile struct {
	Path         string `json:"path"`
	Existed      bool   `json:"existed"`
	FileSHA      string `json:"file_sha,omitempty"`
	Size         int64  `json:"size,omitempty"`
	Mode         string `json:"mode,omitempty"`
	SnapshotPath string `json:"snapshot_path,omitempty"`
}

type checkpointRestoreResult struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

func (t *CheckpointTool) createCheckpoint(ctx context.Context, checkpointID string, paths []string, reason string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("checkpoint create requires at least one path")
	}
	if len(paths) > checkpointMaxFiles {
		return "", fmt.Errorf("checkpoint create accepts at most %d paths", checkpointMaxFiles)
	}
	checkpointID, err := normalizeCheckpointID(checkpointID)
	if err != nil {
		return "", err
	}
	if checkpointID == "" {
		checkpointID = generatedCheckpointID()
	}

	dir := filepath.Join(t.checkpointRoot(), checkpointID)
	filesDir := filepath.Join(dir, "files")
	manifestPath := filepath.Join(dir, "checkpoint.json")
	if _, err := os.Stat(manifestPath); err == nil {
		return "", fmt.Errorf("checkpoint %q already exists", checkpointID)
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("stat checkpoint: %w", err)
	}
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		return "", fmt.Errorf("create checkpoint directory: %w", err)
	}
	createdOK := false
	defer func() {
		if !createdOK {
			_ = os.RemoveAll(dir)
		}
	}()

	seen := map[string]bool{}
	files := make([]workspaceCheckpointFile, 0, len(paths))
	for i, rawPath := range paths {
		resolved, displayPath, err := t.resolveCheckpointPath(rawPath, "snapshot")
		if err != nil {
			return "", err
		}
		if seen[displayPath] {
			continue
		}
		seen[displayPath] = true
		entry, err := snapshotCheckpointFile(resolved, displayPath, filesDir, i)
		if err != nil {
			return "", err
		}
		files = append(files, entry)
	}
	if len(files) == 0 {
		return "", errors.New("checkpoint create did not receive any unique paths")
	}

	manifest := workspaceFileCheckpoint{
		ID:                checkpointID,
		CreatedAt:         time.Now().UTC(),
		Reason:            strings.TrimSpace(reason),
		WorkspaceRevision: workspaceRevision(ctx, t.env.RootDir),
		ManifestPath:      manifestPath,
		Files:             files,
	}
	if err := writeCheckpointManifest(manifestPath, manifest); err != nil {
		return "", err
	}
	createdOK = true
	return mustJSON(map[string]any{
		"action":             "create",
		"checkpoint":         manifest,
		"manifest_path":      manifestPath,
		"workspace_revision": workspaceRevision(ctx, t.env.RootDir),
		"next_suggestions":   []string{"continue the edit, and use checkpoint restore with this checkpoint_id if rollback is needed"},
	})
}

func (t *CheckpointTool) listCheckpoints(ctx context.Context) (string, error) {
	root := t.checkpointRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return mustJSON(map[string]any{
				"action":             "list",
				"checkpoints":        []workspaceFileCheckpoint{},
				"count":              0,
				"workspace_revision": workspaceRevision(ctx, t.env.RootDir),
				"next_suggestions":   []string{"create a checkpoint before risky edits if rollback may be needed"},
			})
		}
		return "", fmt.Errorf("list checkpoints: %w", err)
	}

	checkpoints := make([]workspaceFileCheckpoint, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		checkpoint, err := loadCheckpointManifest(filepath.Join(root, entry.Name(), "checkpoint.json"))
		if err == nil {
			checkpoints = append(checkpoints, checkpoint)
		}
	}
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].CreatedAt.Before(checkpoints[j].CreatedAt)
	})
	return mustJSON(map[string]any{
		"action":             "list",
		"checkpoints":        checkpoints,
		"count":              len(checkpoints),
		"workspace_revision": workspaceRevision(ctx, t.env.RootDir),
		"next_suggestions":   []string{"restore a specific checkpoint_id only when rollback is required"},
	})
}

func (t *CheckpointTool) restoreCheckpoint(ctx context.Context, checkpointID string, paths []string, reason string) (string, error) {
	checkpointID, err := normalizeCheckpointID(checkpointID)
	if err != nil {
		return "", err
	}
	if checkpointID == "" {
		return "", errors.New("checkpoint restore requires checkpoint_id")
	}
	manifestPath := filepath.Join(t.checkpointRoot(), checkpointID, "checkpoint.json")
	manifest, err := loadCheckpointManifest(manifestPath)
	if err != nil {
		return "", err
	}

	requested := map[string]bool{}
	if len(paths) > 0 {
		for _, rawPath := range paths {
			_, displayPath, err := t.resolveCheckpointPath(rawPath, "restore")
			if err != nil {
				return "", err
			}
			requested[displayPath] = true
		}
	}

	plans := make([]checkpointRestorePlan, 0, len(manifest.Files))
	seenRequested := map[string]bool{}
	for _, file := range manifest.Files {
		if len(requested) > 0 && !requested[file.Path] {
			continue
		}
		resolved, displayPath, err := t.resolveCheckpointPath(file.Path, "restore")
		if err != nil {
			return "", err
		}
		seenRequested[displayPath] = true
		plan, err := buildCheckpointRestorePlan(file, resolved)
		if err != nil {
			return "", err
		}
		plans = append(plans, plan)
	}
	for path := range requested {
		if !seenRequested[path] {
			return "", fmt.Errorf("checkpoint %q does not contain path %q", checkpointID, path)
		}
	}
	if len(plans) == 0 {
		return "", errors.New("checkpoint restore selected no files")
	}

	snapshots, err := snapshotCheckpointRestoreTargets(plans)
	if err != nil {
		return "", err
	}
	if err := commitCheckpointRestorePlans(plans); err != nil {
		_ = rollbackPatchSnapshots(snapshots)
		return "", err
	}
	for _, plan := range plans {
		if t.env.OnFileChanged != nil {
			t.env.OnFileChanged(plan.AbsPath)
		}
	}

	restored := make([]checkpointRestoreResult, 0, len(plans))
	for _, plan := range plans {
		restored = append(restored, checkpointRestoreResult{Path: plan.DisplayPath, Action: plan.Action})
	}
	manifest.RestoredAt = time.Now().UTC()
	return mustJSON(map[string]any{
		"action":             "restore",
		"checkpoint_id":      checkpointID,
		"reason":             strings.TrimSpace(reason),
		"restored_files":     restored,
		"checkpoint":         manifest,
		"workspace_revision": workspaceRevision(ctx, t.env.RootDir),
		"next_suggestions":   []string{"inspect git diff and rerun targeted validation after rollback"},
	})
}

type checkpointRestorePlan struct {
	AbsPath     string
	DisplayPath string
	Action      string
	Content     []byte
	Mode        os.FileMode
}

func buildCheckpointRestorePlan(file workspaceCheckpointFile, absPath string) (checkpointRestorePlan, error) {
	plan := checkpointRestorePlan{
		AbsPath:     absPath,
		DisplayPath: file.Path,
		Action:      "delete",
		Mode:        0o644,
	}
	if !file.Existed {
		return plan, nil
	}
	if strings.TrimSpace(file.SnapshotPath) == "" {
		return checkpointRestorePlan{}, fmt.Errorf("checkpoint file %q is missing snapshot_path", file.Path)
	}
	content, err := os.ReadFile(file.SnapshotPath)
	if err != nil {
		return checkpointRestorePlan{}, fmt.Errorf("read checkpoint snapshot %q: %w", file.Path, err)
	}
	plan.Action = "restore"
	plan.Content = content
	if mode, err := parseCheckpointMode(file.Mode); err == nil {
		plan.Mode = mode
	}
	return plan, nil
}

func snapshotCheckpointRestoreTargets(plans []checkpointRestorePlan) ([]patchPathSnapshot, error) {
	snapshots := make([]patchPathSnapshot, 0, len(plans))
	for _, plan := range plans {
		info, err := os.Stat(plan.AbsPath)
		if err != nil {
			if os.IsNotExist(err) {
				snapshots = append(snapshots, patchPathSnapshot{Path: plan.AbsPath})
				continue
			}
			return nil, fmt.Errorf("snapshot restore target %s: %w", plan.DisplayPath, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("restore target is a directory: %s", plan.DisplayPath)
		}
		content, err := os.ReadFile(plan.AbsPath)
		if err != nil {
			return nil, fmt.Errorf("snapshot restore target %s: %w", plan.DisplayPath, err)
		}
		snapshots = append(snapshots, patchPathSnapshot{
			Path:    plan.AbsPath,
			Exists:  true,
			Content: content,
			Mode:    info.Mode().Perm(),
		})
	}
	return snapshots, nil
}

func commitCheckpointRestorePlans(plans []checkpointRestorePlan) error {
	for _, plan := range plans {
		switch plan.Action {
		case "delete":
			if err := os.Remove(plan.AbsPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete %s: %w", plan.DisplayPath, err)
			}
		case "restore":
			if err := os.MkdirAll(filepath.Dir(plan.AbsPath), 0o755); err != nil {
				return fmt.Errorf("create parent directory for %s: %w", plan.DisplayPath, err)
			}
			if err := os.WriteFile(plan.AbsPath, plan.Content, plan.Mode); err != nil {
				return fmt.Errorf("restore %s: %w", plan.DisplayPath, err)
			}
		default:
			return fmt.Errorf("unknown restore action %q", plan.Action)
		}
	}
	return nil
}

func snapshotCheckpointFile(absPath, displayPath, filesDir string, index int) (workspaceCheckpointFile, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return workspaceCheckpointFile{Path: displayPath, Existed: false}, nil
		}
		return workspaceCheckpointFile{}, fmt.Errorf("stat %s: %w", displayPath, err)
	}
	if info.IsDir() {
		return workspaceCheckpointFile{}, fmt.Errorf("checkpoint path is a directory: %s", displayPath)
	}
	if info.Size() > checkpointMaxFileBytes {
		return workspaceCheckpointFile{}, fmt.Errorf("checkpoint file %s is too large (%d bytes, max %d)", displayPath, info.Size(), checkpointMaxFileBytes)
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return workspaceCheckpointFile{}, fmt.Errorf("read %s: %w", displayPath, err)
	}
	hash := sha256Hex(content)
	snapshotName := fmt.Sprintf("%03d-%s.snapshot", index, sha256Hex([]byte(displayPath))[:12])
	snapshotPath := filepath.Join(filesDir, snapshotName)
	if err := os.WriteFile(snapshotPath, content, info.Mode().Perm()); err != nil {
		return workspaceCheckpointFile{}, fmt.Errorf("write checkpoint snapshot %s: %w", displayPath, err)
	}
	return workspaceCheckpointFile{
		Path:         displayPath,
		Existed:      true,
		FileSHA:      formatFileSHA(hash),
		Size:         info.Size(),
		Mode:         fmt.Sprintf("%04o", info.Mode().Perm()),
		SnapshotPath: snapshotPath,
	}, nil
}

func (t *CheckpointTool) resolveCheckpointPath(path, action string) (absPath, displayPath string, err error) {
	if strings.TrimSpace(path) == "" {
		return "", "", errors.New("checkpoint path must not be empty")
	}
	resolved, err := t.env.ResolvePath(path)
	if err != nil {
		return "", "", err
	}
	if err := rejectSensitiveToolPath(t.env, "checkpoint", action, resolved); err != nil {
		return "", "", err
	}
	return resolved, t.env.NormalizeDisplayPath(resolved), nil
}

func (t *CheckpointTool) checkpointRoot() string {
	return filepath.Join(t.env.StateDir, "checkpoints")
}

func writeCheckpointManifest(path string, manifest workspaceFileCheckpoint) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace manifest: %w", err)
	}
	return nil
}

func loadCheckpointManifest(path string) (workspaceFileCheckpoint, error) {
	var manifest workspaceFileCheckpoint
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest, fmt.Errorf("read checkpoint manifest: %w", err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("load checkpoint: %w", err)
	}
	if manifest.ManifestPath == "" {
		manifest.ManifestPath = path
	}
	return manifest, nil
}

func normalizeCheckpointID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > checkpointIDGeneratedLimit {
		return "", fmt.Errorf("checkpoint_id must be at most %d characters", checkpointIDGeneratedLimit)
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return "", fmt.Errorf("checkpoint_id %q contains unsupported character %q", value, r)
	}
	return value, nil
}

func generatedCheckpointID() string {
	return "checkpoint-" + time.Now().UTC().Format("20060102T150405.000000000Z")
}

func parseCheckpointMode(value string) (os.FileMode, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("empty mode")
	}
	var parsed uint64
	if _, err := fmt.Sscanf(value, "%o", &parsed); err != nil {
		return 0, err
	}
	return os.FileMode(parsed).Perm(), nil
}
