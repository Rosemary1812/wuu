package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/capability"
	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
	"github.com/blueberrycongee/wuu/internal/goalruntime"
	"github.com/blueberrycongee/wuu/internal/memory/store"
	proc "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/skills"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

// ReadFileEntry tracks a successful read_file invocation for dedup
// and must-read-first guards.
type ReadFileEntry struct {
	MtimeUnix     int64
	MtimeUnixNano int64
	Size          int64
	ContentSHA256 string
	Offset        int
	Limit         int
	BaselineOnly  bool
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

func (r *readFileState) delete(absPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.state, absPath)
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

func (r *readFileState) snapshot() map[string]ReadFileEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]ReadFileEntry, len(r.state))
	for path, entry := range r.state {
		out[path] = entry
	}
	return out
}

type testRunEntry struct {
	CommandHash    string
	Revision       string
	Failed         bool
	Command        string
	Scope          string
	Purpose        string
	ExitCode       int
	TimedOut       bool
	DurationMS     int64
	FailureSummary testFailureSummary
	FullLogRef     string
	CreatedAt      time.Time
}

type testRunState struct {
	mu      sync.RWMutex
	records []testRunEntry
}

func (s *testRunState) record(commandHash, revision string, failed bool) {
	if commandHash == "" || revision == "" {
		return
	}
	s.recordEntry(testRunEntry{
		CommandHash: commandHash,
		Revision:    revision,
		Failed:      failed,
		CreatedAt:   time.Now().UTC(),
	})
}

func (s *testRunState) recordEntry(entry testRunEntry) {
	if entry.CommandHash == "" || entry.Revision == "" {
		return
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, entry)
}

func (s *testRunState) consecutiveFailures(commandHash, revision string) int {
	if commandHash == "" || revision == "" {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for i := len(s.records) - 1; i >= 0; i-- {
		record := s.records[i]
		if record.CommandHash != commandHash || record.Revision != revision {
			continue
		}
		if !record.Failed {
			break
		}
		count++
	}
	return count
}

func (s *testRunState) latestFailure() (testRunEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.records) - 1; i >= 0; i-- {
		if s.records[i].Failed {
			return s.records[i], true
		}
	}
	return testRunEntry{}, false
}

type webEvidenceEntry struct {
	ToolName    string
	Evidence    webEvidence
	Error       string
	ResultCount int
	StatusCode  int
	ContentType string
	Size        int
	Truncated   bool
	CreatedAt   time.Time
}

type webEvidenceState struct {
	mu      sync.RWMutex
	entries []webEvidenceEntry
}

func (s *webEvidenceState) record(entry webEvidenceEntry) {
	if entry.Evidence.ID == "" {
		return
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entry)
}

func (s *webEvidenceState) snapshot() []webEvidenceEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]webEvidenceEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// Env holds shared runtime state that individual tools receive at
// construction time. It replaces the old approach of making every
// handler a method on *Toolkit.
type Env struct {
	RootDir  string
	StateDir string
	// PermissionProfile is the active runtime filesystem/approval profile.
	// Tools use it to align hard runtime behavior with the selected product
	// permission mode.
	PermissionProfile string

	// Optional dependencies — nil means the feature is unavailable.
	// Tools check for nil and return a clear error rather than panic.
	SessionID   string
	SessionDir  string // absolute session artifact path for result budgeting
	GoalRuntime *goalruntime.Runtime
	AgentID     string
	AgentPath   string
	// ToolSearchEnabled means deferred tools are loaded through the
	// model-visible tool_search entrypoint. When false, the active surface is
	// flattened and tool_search guidance must not be emitted.
	ToolSearchEnabled bool
	// NativeDeferredToolDiscovery means the active provider can receive
	// schemas discovered by ordinary tool results without requiring an
	// explicit tool_search call first.
	NativeDeferredToolDiscovery bool
	ProcessMgr                  *proc.Manager
	AgentControl                *agentcontrol.AgentControl
	Skills                      []skills.Skill
	Workflows                   []workflow.Definition
	// ActiveSurface is the compiled model profile surface currently
	// governing this tool environment. Tools with secondary catalogs
	// such as load_skill use it to avoid exposing instructions that
	// require unavailable capabilities.
	ActiveSurface capability.Surface
	// OnFileChanged is called after write_file/edit_file successfully
	// modifies a file. Enables FileChanged hook dispatch without
	// coupling the tools package to the hooks package.
	OnFileChanged func(absPath string)
	// OnPlanUpdated is called after update_plan successfully stores a
	// new snapshot. Consumers can bridge it to runtime events or UI
	// notifications without coupling the plan tool to either layer.
	OnPlanUpdated func(snapshot PlanSnapshot)
	// OnPortsReported is called after report_listening_ports validates
	// the agent's port list. Consumers (the desktop app-server) use it
	// to thread the per-conversation listening ports into the UI and
	// auto-open the in-app browser preview.
	OnPortsReported func(ports []int)

	// Memory is the optional LLM-writable memory backend. When nil,
	// read_memory/write_memory report that memory is not configured. The
	// constructor that builds Env is responsible for wiring a real
	// Provider (typically a *store.FileProvider rooted under profile state).
	// Memory tools may be registered internally even when this is nil; they
	// are hidden from Definitions until a provider is attached.
	Memory store.Provider
	// MemoryCharLimit caps target="memory" entries by character count.
	// Zero uses the built-in default.
	MemoryCharLimit int
	// UserMemoryCharLimit caps target="user" entries by character count.
	// Zero uses the built-in default.
	UserMemoryCharLimit int

	readState *readFileState
	testState testRunState
	planState planState
	webState  webEvidenceState

	toolTelemetry toolTelemetry
}

// RecordRead records a successful read_file invocation.
func (e *Env) RecordRead(absPath string, entry ReadFileEntry) {
	if e.readState == nil {
		e.readState = &readFileState{}
	}
	e.readState.record(absPath, entry)
}

// RecordWriteBaseline records the content just written by a mutating file
// tool. It guards later edits in this agent without claiming that read_file
// already returned the new full file body to the model.
func (e *Env) RecordWriteBaseline(absPath string, content []byte) {
	if e == nil || strings.TrimSpace(absPath) == "" {
		return
	}
	entry := ReadFileEntry{
		Size:          int64(len(content)),
		ContentSHA256: sha256Hex(content),
		BaselineOnly:  true,
	}
	if info, err := os.Stat(absPath); err == nil && !info.IsDir() {
		entry.MtimeUnix = info.ModTime().Unix()
		entry.MtimeUnixNano = info.ModTime().UnixNano()
		entry.Size = info.Size()
	}
	e.RecordRead(absPath, entry)
}

func (e *Env) ForgetRead(absPath string) {
	if e == nil || e.readState == nil {
		return
	}
	e.readState.delete(absPath)
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

func (e *Env) ReadEntries() map[string]ReadFileEntry {
	if e.readState == nil {
		return nil
	}
	return e.readState.snapshot()
}

func (e *Env) RecordTestRun(commandHash, revision string, failed bool) {
	e.testState.record(commandHash, revision, failed)
}

func (e *Env) RecordTestRunResult(entry testRunEntry) {
	e.testState.recordEntry(entry)
}

func (e *Env) ConsecutiveTestFailures(commandHash, revision string) int {
	return e.testState.consecutiveFailures(commandHash, revision)
}

func (e *Env) LatestTestFailure() (testRunEntry, bool) {
	return e.testState.latestFailure()
}

func (e *Env) RecordWebEvidence(entry webEvidenceEntry) {
	e.webState.record(entry)
}

func (e *Env) WebEvidenceEntries() []webEvidenceEntry {
	return e.webState.snapshot()
}

func (e *Env) BypassToolHardProtections() bool {
	return e != nil && strings.TrimSpace(e.PermissionProfile) == PermissionProfileDangerFullAccess
}

func (e *Env) RedactToolOutput(text string) string {
	if e.BypassToolHardProtections() {
		return text
	}
	return redactToolOutput(text)
}

// ResolvePath resolves a user-supplied relative or absolute path. Normal
// permission profiles constrain it to the workspace; full access returns the
// resolved path without applying Wuu's workspace boundary.
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
	if e.BypassToolHardProtections() {
		return resolved, nil
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

// ResolveReadPath resolves paths for read-only tools. In addition to ordinary
// workspace files, it allows files under the current session artifact directory
// so compact tool and agent results can point at Wuu-managed artifacts.
func (e *Env) ResolveReadPath(input string) (absPath, displayPath string, managed bool, err error) {
	candidate := strings.TrimSpace(input)
	if candidate == "" {
		candidate = "."
	}

	if expanded, ok, err := e.expandSessionPathRef(candidate); ok || err != nil {
		if err != nil {
			return "", "", false, err
		}
		display := e.normalizeSessionDisplayPath(expanded)
		return expanded, display, true, nil
	}

	if filepath.IsAbs(candidate) && strings.TrimSpace(e.SessionDir) != "" {
		resolved, err := filepath.Abs(filepath.Clean(candidate))
		if err != nil {
			return "", "", false, fmt.Errorf("resolve path: %w", err)
		}
		if pathWithinRoot(e.SessionDir, resolved) {
			display := e.normalizeSessionDisplayPath(resolved)
			return resolved, display, true, nil
		}
	}

	resolved, err := e.ResolvePath(candidate)
	if err != nil {
		return "", "", false, err
	}
	return resolved, e.NormalizeDisplayPath(resolved), false, nil
}

func (e *Env) expandSessionPathRef(input string) (string, bool, error) {
	sessionDir := strings.TrimSpace(e.SessionDir)
	if sessionDir == "" {
		return "", false, nil
	}
	const prefix = "$SESSION_DIR"
	if input != prefix && !strings.HasPrefix(input, prefix+"/") && !strings.HasPrefix(input, prefix+string(filepath.Separator)) {
		return "", false, nil
	}
	suffix := strings.TrimPrefix(input, prefix)
	suffix = strings.TrimPrefix(suffix, "/")
	suffix = strings.TrimPrefix(suffix, string(filepath.Separator))
	resolved, err := filepath.Abs(filepath.Join(sessionDir, filepath.FromSlash(suffix)))
	if err != nil {
		return "", true, fmt.Errorf("resolve path: %w", err)
	}
	if !pathWithinRoot(sessionDir, resolved) {
		return "", true, fmt.Errorf("path %q escapes session artifact directory", input)
	}
	return resolved, true, nil
}

func (e *Env) normalizeSessionDisplayPath(absPath string) string {
	sessionDir := strings.TrimSpace(e.SessionDir)
	if sessionDir == "" {
		return absPath
	}
	rel, err := filepath.Rel(sessionDir, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return absPath
	}
	if rel == "." {
		return "$SESSION_DIR"
	}
	return "$SESSION_DIR/" + filepath.ToSlash(rel)
}

func pathWithinRoot(root, path string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	evalRoot := absRoot
	if ev, err := filepath.EvalSymlinks(evalRoot); err == nil {
		evalRoot = ev
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	evalPath := absPath
	if ev, err := filepath.EvalSymlinks(evalPath); err == nil {
		evalPath = ev
	} else if rel, relErr := filepath.Rel(absRoot, absPath); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		evalPath = filepath.Join(evalRoot, rel)
	}
	rel, err := filepath.Rel(evalRoot, evalPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
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
	return skills.Find(e.VisibleSkills(), name)
}

// SkillNames returns all available skill names.
func (e *Env) SkillNames() []string {
	visible := e.VisibleSkills()
	out := make([]string, 0, len(visible))
	for _, s := range visible {
		out = append(out, s.Name)
	}
	return out
}

// VisibleSkills returns the skills allowed by the active model surface.
func (e *Env) VisibleSkills() []skills.Skill {
	if e == nil {
		return nil
	}
	return FilterSkillsForSurface(e.Skills, e.ActiveSurface)
}

// ProcessSkillBody processes a skill body with variable substitution. Inline
// shell stays disabled here: loading a skill should expose its instructions and
// resources, not execute code as a side effect.
func (e *Env) ProcessSkillBody(ctx context.Context, skill skills.Skill, arguments string) string {
	return skills.ProcessSkillBody(ctx, skill.Content, skills.ProcessOptions{
		Arguments:        arguments,
		SkillDir:         skill.Dir,
		SessionID:        e.SessionID,
		Shell:            skill.Shell,
		AllowInlineShell: false,
	})
}

// FindWorkflow looks up a workflow definition by name.
func (e *Env) FindWorkflow(name string) (workflow.Definition, bool) {
	return workflow.Find(e.VisibleWorkflows(), name)
}

// WorkflowNames returns all available workflow definition names.
func (e *Env) WorkflowNames() []string {
	visible := e.VisibleWorkflows()
	out := make([]string, 0, len(visible))
	for _, wf := range visible {
		out = append(out, wf.Name)
	}
	return out
}

// VisibleWorkflows returns workflow definitions allowed by the active surface.
func (e *Env) VisibleWorkflows() []workflow.Definition {
	if e == nil {
		return nil
	}
	return FilterWorkflowsForSurface(e.Workflows, e.ActiveSurface)
}

// ProcessWorkflowBody performs workflow-safe variable substitution. Workflow
// definitions do not execute inline shell; they produce orchestration plans.
func (e *Env) ProcessWorkflowBody(def workflow.Definition, arguments string) string {
	return workflow.ProcessBody(def.Content, workflow.ProcessOptions{
		Arguments:   arguments,
		WorkflowDir: def.Dir,
		SessionID:   e.SessionID,
	})
}

// OrchestrationStateDir returns the state root for user-visible orchestration
// artifacts. Interactive turns bind tools to a SessionID, so their Goals and
// Workflow Runs live with that conversation. Headless or workspace-level tools
// without a SessionID keep using workspace state.
func (e *Env) OrchestrationStateDir() (string, error) {
	if e == nil {
		return "", fmt.Errorf("tool environment is required")
	}
	if sessionID := strings.TrimSpace(e.SessionID); sessionID != "" {
		if sessionDir := strings.TrimSpace(e.SessionDir); sessionDir != "" {
			return sessionDir, nil
		}
		stateDir, err := e.WorkspaceStateDir()
		if err != nil {
			return "", err
		}
		return statepath.SessionArtifactDir(stateDir, sessionID), nil
	}
	return e.WorkspaceStateDir()
}

// WorkflowStore returns a durable workflow store rooted in the current
// orchestration scope.
func (e *Env) WorkflowStore() (*workflow.Store, error) {
	stateDir, err := e.OrchestrationStateDir()
	if err != nil {
		return nil, err
	}
	store := workflow.NewStore(stateDir)
	store.SetArtifactSink(goalrunner.NewWorkflowArtifactSink(nil))
	return store, nil
}

// WorkflowGoalStore returns the goal store bound to one Workflow Run. Workflow
// goals live in the same orchestration scope as the run they summarize.
func (e *Env) WorkflowGoalStore(runID string) (*goalrunner.Store, error) {
	stateDir, err := e.OrchestrationStateDir()
	if err != nil {
		return nil, err
	}
	return goalrunner.NewStore(statepath.GoalDir(stateDir, strings.TrimSpace(runID))), nil
}
