package sessionmemory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/stringutil"
)

const (
	TargetProjectMemory = "project_memory"
	TargetCheckpoint    = "checkpoint"
	TargetNotes         = "notes"

	defaultFileMode = 0o644
	defaultDirMode  = 0o755

	projectMemoryTokenBudget = 10000
	checkpointTokenBudget    = 11000
	notesTokenBudget         = 4000
)

// Paths groups the durable memory files for one workspace and session.
type Paths struct {
	ProjectMemory string `json:"project_memory"`
	Checkpoint    string `json:"checkpoint"`
	Notes         string `json:"notes"`
	TasksDir      string `json:"tasks_dir"`
}

type FileStatus struct {
	Target string `json:"target"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Bytes  int64  `json:"bytes"`
}

type DreamState struct {
	LastRunAt time.Time `json:"last_run_at,omitempty"`
}

func PathsFor(workspaceStateDir, sessionArtifactDir string) Paths {
	paths := Paths{}
	if dir := strings.TrimSpace(workspaceStateDir); dir != "" {
		workspaceMemoryDir := filepath.Join(dir, "memory")
		paths.ProjectMemory = filepath.Join(workspaceMemoryDir, "MEMORY.md")
	}
	if dir := strings.TrimSpace(sessionArtifactDir); dir != "" {
		sessionMemoryDir := filepath.Join(dir, "memory")
		paths.Checkpoint = filepath.Join(sessionMemoryDir, "checkpoint.md")
		paths.Notes = filepath.Join(sessionMemoryDir, "notes.md")
		paths.TasksDir = filepath.Join(sessionMemoryDir, "tasks")
	}
	return paths
}

func DreamStatePath(workspaceStateDir string) string {
	if strings.TrimSpace(workspaceStateDir) == "" {
		return ""
	}
	return filepath.Join(workspaceStateDir, "memory", "dream_state.json")
}

func LoadDreamState(workspaceStateDir string) (DreamState, error) {
	path := DreamStatePath(workspaceStateDir)
	if strings.TrimSpace(path) == "" {
		return DreamState{}, fmt.Errorf("workspace state directory is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DreamState{}, nil
		}
		return DreamState{}, fmt.Errorf("read dream state: %w", err)
	}
	var state DreamState
	if err := json.Unmarshal(data, &state); err != nil {
		return DreamState{}, fmt.Errorf("parse dream state: %w", err)
	}
	return state, nil
}

func SaveDreamState(workspaceStateDir string, state DreamState) error {
	path := DreamStatePath(workspaceStateDir)
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("workspace state directory is required")
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode dream state: %w", err)
	}
	return writeAtomic(path, append(data, '\n'))
}

func Status(workspaceStateDir, sessionArtifactDir string) ([]FileStatus, error) {
	paths := PathsFor(workspaceStateDir, sessionArtifactDir)
	targets := []struct {
		name string
		path string
	}{
		{TargetProjectMemory, paths.ProjectMemory},
		{TargetCheckpoint, paths.Checkpoint},
		{TargetNotes, paths.Notes},
	}
	out := make([]FileStatus, 0, len(targets))
	for _, target := range targets {
		stat := FileStatus{Target: target.name, Path: target.path}
		if strings.TrimSpace(target.path) == "" {
			out = append(out, stat)
			continue
		}
		info, err := os.Stat(target.path)
		switch {
		case err == nil:
			stat.Exists = true
			stat.Bytes = info.Size()
		case os.IsNotExist(err):
		default:
			return nil, fmt.Errorf("stat %s: %w", target.name, err)
		}
		out = append(out, stat)
	}
	return out, nil
}

func ReadTarget(workspaceStateDir, sessionArtifactDir, target string) (string, string, bool, error) {
	path, err := targetPath(workspaceStateDir, sessionArtifactDir, target)
	if err != nil {
		return "", "", false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, "", false, nil
		}
		return path, "", false, fmt.Errorf("read %s: %w", target, err)
	}
	return path, string(data), true, nil
}

func AppendTarget(workspaceStateDir, sessionArtifactDir, target, content, source string, now time.Time) (string, int, error) {
	path, err := targetPath(workspaceStateDir, sessionArtifactDir, target)
	if err != nil {
		return "", 0, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", 0, fmt.Errorf("%s content is required", target)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	existing := ""
	if data, readErr := os.ReadFile(path); readErr == nil {
		existing = string(data)
	} else if !os.IsNotExist(readErr) {
		return "", 0, fmt.Errorf("read %s before append: %w", target, readErr)
	} else {
		existing = templateForTarget(target)
	}
	if strings.TrimSpace(existing) != "" && !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	entry := formatAppendEntry(content, source, now)
	next := strings.TrimRight(existing, "\n") + "\n\n" + entry + "\n"
	if err := writeAtomic(path, []byte(next)); err != nil {
		return "", 0, err
	}
	return path, len([]rune(content)), nil
}

func ReplaceTarget(workspaceStateDir, sessionArtifactDir, target, content string) (string, int, error) {
	path, err := targetPath(workspaceStateDir, sessionArtifactDir, target)
	if err != nil {
		return "", 0, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", 0, fmt.Errorf("%s content is required", target)
	}
	if err := writeAtomic(path, []byte(content+"\n")); err != nil {
		return "", 0, err
	}
	return path, len([]rune(content)), nil
}

func ContextBlocks(workspaceStateDir, sessionArtifactDir string) []wuucontext.Block {
	paths := PathsFor(workspaceStateDir, sessionArtifactDir)
	var blocks []wuucontext.Block
	if block, ok := fileBlock(TargetProjectMemory, paths.ProjectMemory, wuucontext.BlockMemory, "Workspace project memory", "workspace.memory", projectMemoryTokenBudget); ok {
		blocks = append(blocks, block)
	}
	if block, ok := fileBlock(TargetCheckpoint, paths.Checkpoint, wuucontext.BlockTaskState, "Session checkpoint", "session.checkpoint", checkpointTokenBudget); ok {
		blocks = append(blocks, block)
	}
	if block, ok := fileBlock(TargetNotes, paths.Notes, wuucontext.BlockTaskState, "Session notes", "session.notes", notesTokenBudget); ok {
		blocks = append(blocks, block)
	}
	return blocks
}

func targetPath(workspaceStateDir, sessionArtifactDir, target string) (string, error) {
	paths := PathsFor(workspaceStateDir, sessionArtifactDir)
	switch strings.TrimSpace(target) {
	case TargetProjectMemory:
		if strings.TrimSpace(workspaceStateDir) == "" {
			return "", fmt.Errorf("workspace state directory is required for %s", TargetProjectMemory)
		}
		return paths.ProjectMemory, nil
	case TargetCheckpoint:
		if strings.TrimSpace(sessionArtifactDir) == "" {
			return "", fmt.Errorf("session artifact directory is required for %s", TargetCheckpoint)
		}
		return paths.Checkpoint, nil
	case TargetNotes:
		if strings.TrimSpace(sessionArtifactDir) == "" {
			return "", fmt.Errorf("session artifact directory is required for %s", TargetNotes)
		}
		return paths.Notes, nil
	default:
		return "", fmt.Errorf("unknown session memory target %q", target)
	}
}

func fileBlock(target, path string, kind wuucontext.BlockKind, title, source string, tokenBudget int) (wuucontext.Block, bool) {
	if strings.TrimSpace(path) == "" {
		return wuucontext.Block{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return wuucontext.Block{}, false
	}
	maxBytes := tokenBudget * 4
	content := strings.TrimSpace(string(data))
	if maxBytes > 0 && len(content) > maxBytes {
		content = stringutil.HeadTail(content, maxBytes/2, maxBytes/2, "\n\n[trimmed session memory]\n\n")
	}
	return wuucontext.Block{
		Kind:        kind,
		Title:       title,
		Source:      source + ":" + target,
		TokenBudget: tokenBudget,
		Content:     content,
	}, true
}

func writeAtomic(path string, data []byte) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("path is required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, defaultDirMode); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(defaultFileMode); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace memory file: %w", err)
	}
	return nil
}

func templateForTarget(target string) string {
	switch target {
	case TargetProjectMemory:
		return "# Project Memory\n\nDurable workspace facts that should survive across sessions.\n\n## Project Context\n\n## Rules\n\n## Architecture Decisions\n\n## Discovered Durable Knowledge\n"
	case TargetCheckpoint:
		return "# Session Checkpoint\n\nCompact recoverable state for the active session.\n\n## Active Intent\n\n## Next Action\n\n## Current Work\n\n## Decisions\n\n## Open Questions\n"
	case TargetNotes:
		return "# Session Notes\n\nScratch notes for this session. Promote durable facts to project memory or profile memory.\n"
	default:
		return ""
	}
}

func formatAppendEntry(content, source string, now time.Time) string {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "agent"
	}
	return fmt.Sprintf("## %s (%s)\n\n%s", now.UTC().Format(time.RFC3339), source, content)
}
