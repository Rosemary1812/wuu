package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	proc "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/skills"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

// ReadFileEntry tracks a successful read_file invocation for dedup
// and must-read-first guards.
type ReadFileEntry struct {
	MtimeUnix int64
	Offset    int
	Limit     int
}

// readFileState is a thread-safe record of read_file calls.
type readFileState struct {
	mu    sync.RWMutex
	state map[string]ReadFileEntry
}

func (r *readFileState) record(absPath string, entry ReadFileEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == nil {
		r.state = make(map[string]ReadFileEntry)
	}
	r.state[absPath] = entry
}

func (r *readFileState) hasBeenRead(absPath string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.state[absPath]
	return ok
}

func (r *readFileState) getEntry(absPath string) (ReadFileEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.state[absPath]
	return entry, ok
}

// Env holds shared runtime state that individual tools receive at
// construction time. It replaces the old approach of making every
// handler a method on *Toolkit.
type Env struct {
	RootDir  string
	StateDir string

	// Optional dependencies — nil means the feature is unavailable.
	// Tools check for nil and return a clear error rather than panic.
	SessionID    string
	SessionDir   string // absolute session artifact path for result budgeting
	AgentID      string
	AgentPath    string
	ProcessMgr   *proc.Manager
	AskBridge    AskUserBridge
	AgentControl *agentcontrol.AgentControl
	Skills       []skills.Skill
	// OnFileChanged is called after write_file/edit_file successfully
	// modifies a file. Enables FileChanged hook dispatch without
	// coupling the tools package to the hooks package.
	OnFileChanged func(absPath string)

	readState *readFileState
	planState planState

	toolTelemetry toolTelemetry
}

// RecordRead records a successful read_file invocation.
func (e *Env) RecordRead(absPath string, entry ReadFileEntry) {
	if e.readState == nil {
		e.readState = &readFileState{}
	}
	e.readState.record(absPath, entry)
}

// HasBeenRead reports whether a file has been read via read_file.
func (e *Env) HasBeenRead(absPath string) bool {
	if e.readState == nil {
		return false
	}
	return e.readState.hasBeenRead(absPath)
}

// GetReadEntry returns the read state for a file, if any.
func (e *Env) GetReadEntry(absPath string) (ReadFileEntry, bool) {
	if e.readState == nil {
		return ReadFileEntry{}, false
	}
	return e.readState.getEntry(absPath)
}

// ResolvePath resolves a user-supplied relative or absolute path to
// an absolute path within the workspace, preventing sandbox escapes.
func (e *Env) ResolvePath(input string) (string, error) {
	candidate := strings.TrimSpace(input)
	if candidate == "" {
		candidate = "."
	}

	var abs string
	if filepath.IsAbs(candidate) {
		abs = filepath.Clean(candidate)
	} else {
		abs = filepath.Join(e.RootDir, candidate)
	}

	resolved, err := filepath.Abs(abs)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	evalRoot := e.RootDir
	if ev, err := filepath.EvalSymlinks(e.RootDir); err == nil {
		evalRoot = ev
	}
	evalResolved := resolved
	if ev, err := filepath.EvalSymlinks(resolved); err == nil {
		evalResolved = ev
	}

	rel, err := filepath.Rel(evalRoot, evalResolved)
	if err != nil {
		return "", fmt.Errorf("path relation check: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes workspace", input)
	}
	return resolved, nil
}

// NormalizeDisplayPath returns a relative path for display.
func (e *Env) NormalizeDisplayPath(absPath string) string {
	return normalizeDisplayPath(e.RootDir, absPath)
}

// ProcessManager returns the process manager, creating a default one
// if none was injected.
func (e *Env) ProcessManager() (*proc.Manager, error) {
	if e.ProcessMgr != nil {
		return e.ProcessMgr, nil
	}
	stateDir, err := e.WorkspaceStateDir()
	if err != nil {
		return nil, err
	}
	return proc.NewManager(e.RootDir, statepath.RuntimeDir(stateDir))
}

// WorkspaceStateDir returns the user-level state directory for this workspace.
func (e *Env) WorkspaceStateDir() (string, error) {
	if strings.TrimSpace(e.StateDir) != "" {
		return e.StateDir, nil
	}
	wuuHome, err := statepath.Home("")
	if err != nil {
		return "", err
	}
	stateDir, err := statepath.WorkspaceDir(wuuHome, e.RootDir)
	if err != nil {
		return "", err
	}
	e.StateDir = stateDir
	return stateDir, nil
}

// FindSkill looks up a skill by name, returning it and true if found.
func (e *Env) FindSkill(name string) (skills.Skill, bool) {
	return skills.Find(e.Skills, name)
}

// SkillNames returns all available skill names.
func (e *Env) SkillNames() []string {
	out := make([]string, 0, len(e.Skills))
	for _, s := range e.Skills {
		out = append(out, s.Name)
	}
	return out
}

// ProcessSkillBody processes a skill body with variable substitution.
func (e *Env) ProcessSkillBody(ctx context.Context, skill skills.Skill, arguments string) string {
	return skills.ProcessSkillBody(ctx, skill.Content, skills.ProcessOptions{
		Arguments:        arguments,
		SkillDir:         skill.Dir,
		SessionID:        e.SessionID,
		Shell:            skill.Shell,
		AllowInlineShell: true,
	})
}
