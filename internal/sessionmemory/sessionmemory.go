package sessionmemory

import (
	"crypto/rand"
	"encoding/hex"
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
	dreamErrorMaxBytes       = 4096
)

const (
	DreamStatusRunning   = "running"
	DreamStatusCompleted = "completed"
	DreamStatusFailed    = "failed"
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
	LastRunAt      time.Time `json:"last_run_at,omitempty"`
	LastStartedAt  time.Time `json:"last_started_at,omitempty"`
	LastFinishedAt time.Time `json:"last_finished_at,omitempty"`
	LastStatus     string    `json:"last_status,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
}

type dreamLockFile struct {
	PID        int       `json:"pid"`
	AcquiredAt time.Time `json:"acquired_at"`
	Token      string    `json:"token"`
}

type DreamLock struct {
	path  string
	token string
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

func DreamLockPath(workspaceStateDir string) string {
	if strings.TrimSpace(workspaceStateDir) == "" {
		return ""
	}
	return filepath.Join(workspaceStateDir, "memory", "dream.lock")
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

func RecordDreamStarted(workspaceStateDir string, started time.Time) error {
	if started.IsZero() {
		started = time.Now().UTC()
	}
	state, err := LoadDreamState(workspaceStateDir)
	if err != nil {
		return err
	}
	state.LastStartedAt = started.UTC()
	state.LastFinishedAt = time.Time{}
	state.LastStatus = DreamStatusRunning
	state.LastError = ""
	return SaveDreamState(workspaceStateDir, state)
}

func RecordDreamCompleted(workspaceStateDir string, completed time.Time) error {
	if completed.IsZero() {
		completed = time.Now().UTC()
	}
	state, err := LoadDreamState(workspaceStateDir)
	if err != nil {
		return err
	}
	completed = completed.UTC()
	state.LastRunAt = completed
	state.LastFinishedAt = completed
	state.LastStatus = DreamStatusCompleted
	state.LastError = ""
	return SaveDreamState(workspaceStateDir, state)
}

func RecordDreamFailed(workspaceStateDir string, failed time.Time, runErr error) error {
	if failed.IsZero() {
		failed = time.Now().UTC()
	}
	state, err := LoadDreamState(workspaceStateDir)
	if err != nil {
		return err
	}
	state.LastFinishedAt = failed.UTC()
	state.LastStatus = DreamStatusFailed
	if runErr != nil {
		state.LastError = trimDreamError(runErr.Error())
	} else {
		state.LastError = ""
	}
	return SaveDreamState(workspaceStateDir, state)
}

func TryAcquireDreamLock(workspaceStateDir string, staleAfter time.Duration, now time.Time) (*DreamLock, bool, error) {
	path := DreamLockPath(workspaceStateDir)
	if strings.TrimSpace(path) == "" {
		return nil, false, fmt.Errorf("workspace state directory is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(path), defaultDirMode); err != nil {
		return nil, false, fmt.Errorf("create memory dir: %w", err)
	}
	lock, acquired, err := createDreamLock(path, now.UTC())
	if err == nil {
		return lock, acquired, err
	}
	if !os.IsExist(err) {
		return nil, false, err
	}
	if staleAfter <= 0 {
		return nil, false, nil
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return createDreamLock(path, now.UTC())
		}
		return nil, false, fmt.Errorf("stat dream lock: %w", statErr)
	}
	if now.Sub(info.ModTime()) < staleAfter {
		return nil, false, nil
	}
	if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
		return nil, false, fmt.Errorf("remove stale dream lock: %w", removeErr)
	}
	return createDreamLock(path, now.UTC())
}

func (l *DreamLock) Release() error {
	if l == nil || strings.TrimSpace(l.path) == "" {
		return nil
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read dream lock before release: %w", err)
	}
	var holder dreamLockFile
	if err := json.Unmarshal(data, &holder); err != nil {
		return fmt.Errorf("parse dream lock before release: %w", err)
	}
	if holder.Token != l.token {
		return nil
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("release dream lock: %w", err)
	}
	return nil
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

func createDreamLock(path string, now time.Time) (*DreamLock, bool, error) {
	token, err := randomLockToken()
	if err != nil {
		return nil, false, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, defaultFileMode)
	if err != nil {
		if os.IsExist(err) {
			return nil, false, err
		}
		return nil, false, fmt.Errorf("create dream lock: %w", err)
	}
	holder := dreamLockFile{
		PID:        os.Getpid(),
		AcquiredAt: now.UTC(),
		Token:      token,
	}
	data, err := json.MarshalIndent(holder, "", "  ")
	if err != nil {
		file.Close()
		_ = os.Remove(path)
		return nil, false, fmt.Errorf("encode dream lock: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		_ = os.Remove(path)
		return nil, false, fmt.Errorf("write dream lock: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, false, fmt.Errorf("close dream lock: %w", err)
	}
	return &DreamLock{path: path, token: token}, true, nil
}

func randomLockToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("create dream lock token: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

func trimDreamError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= dreamErrorMaxBytes {
		return message
	}
	return stringutil.HeadTail(message, dreamErrorMaxBytes/2, dreamErrorMaxBytes/2, "\n\n[trimmed dream error]\n\n")
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
