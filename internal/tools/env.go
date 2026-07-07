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
	"github.com/blueberrycongee/wuu/internal/toolctx"
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
	// Unconfined is the explicit escape hatch for lifting path confinement.
	// Default false means file tools stay inside FileScopeRoots/RootDir.
	Unconfined bool

	// Optional dependencies — nil means the feature is unavailable.
	// Tools check for nil and return a clear error rather than panic.
	SessionID   string
	SessionDir  string // absolute session artifact path for result budgeting
	GoalRuntime *goalruntime.Runtime
	AgentID     string
	AgentPath   string
	// ParticipantID is set for conversation-native named-agent runtimes.
	// It lets participant tools act under the stable participant identity
	// without changing the thread/root agent identity used by other tools.
	ParticipantID string
	// ParticipantSpeechEnabled is an internal app-server authorization for
	// conversation-native participant runs. Ordinary subagents keep this false.
	ParticipantSpeechEnabled bool
	// ResidentParticipantEnabled is true only for long-lived named-agent DM
	// runtimes. It exposes resident-only conversation tools such as
	// fetch_thread_messages.
	ResidentParticipantEnabled bool
	ConversationSessionDir     string
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
	ParticipantSpeech           ParticipantSpeech
	// GroupManager backs the resident-only create_group / add_member actions
	// of manage_participant. Nil means group management is unavailable in this
	// environment (those actions return an execute-time error).
	GroupManager GroupManager
	// TaskManager backs the resident-only manage_task tool (agent task rail,
	// 2026-07-06 design). Nil means the task rail is unavailable in this
	// environment (every action returns an execute-time error).
	TaskManager TaskManager
	// ThreadID is the conversation (cth) thread the current resident turn runs
	// in. Workflow runs started from this turn bind to it so named-participant
	// team members report into the reply subthread instead of the main stream,
	// and so the named-participant pool is scoped to this thread's group
	// members. Empty for ordinary subagents and self-contained runs.
	ThreadID string
	// FileScopeRoots, when non-empty, replaces the single-RootDir file
	// boundary with a whitelist: file tools (read/write/edit/glob/grep/…)
	// may only touch paths inside one of these roots — the agent home,
	// the user's registered workspaces, and the system temp directory.
	// Reads are rejected the same as writes. Empty keeps the ordinary
	// workspace-confinement behavior; assembled only for resident turns
	// and participant task runs
	// (2026-07-03-sidebar-groups-andy-workspaces.md §5.2).
	FileScopeRoots []string
	Skills         []skills.Skill
	Workflows      []workflow.Definition
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

	// Memory is the optional LLM-writable GLOBAL memory backend — the
	// cross-workspace layer of the two-layer long-term memory. When nil,
	// the "global" write_memory scope is unavailable. The constructor that
	// builds Env is responsible for wiring a real Provider (typically a
	// *store.FileProvider rooted at statepath.GlobalMemoryDir). Memory tools
	// may be registered internally even when this is nil; they are hidden
	// from Definitions until at least one memory layer is attached.
	Memory store.Provider
	// WorkspaceMemory is the optional LLM-writable WORKSPACE memory backend —
	// the project-scoped layer of the two-layer long-term memory. It is a
	// *store.FileProvider rooted at statepath.WorkspaceMemoryDir for the
	// active workspace state directory. When nil, the "workspace" write_memory
	// scope is unavailable. read_memory reads both layers when both are set.
	WorkspaceMemory store.Provider
	// DefaultMemoryWriteScope overrides the scope a scope-less write_memory call
	// resolves to. Empty means the tool's built-in default (MemoryScopeWorkspace).
	// The background global-memory reviewer sets it to MemoryScopeGlobal so its
	// scope-less writes land in the global layer it targets.
	DefaultMemoryWriteScope string
	// MemoryCharLimit caps target="memory" entries by character count.
	// Zero uses the built-in default.
	MemoryCharLimit int
	// UserMemoryCharLimit caps target="user" entries by character count.
	// Zero uses the built-in default.
	UserMemoryCharLimit int

	readState      *readFileState
	testState      testRunState
	planState      planState
	webState       webEvidenceState
	inceptionState inceptionFailureState

	toolTelemetry toolTelemetry
}

type PostedMessage struct {
	AgentID       string    `json:"agent_id,omitempty"`
	ParticipantID string    `json:"participant_id,omitempty"`
	Kind          string    `json:"kind"`
	ThreadID      string    `json:"thread_id"`
	Text          string    `json:"text,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
}

type ParticipantSpeech interface {
	PostMessage(ctx context.Context, kind, text, targetThreadID string) (PostedMessage, error)
}

// GroupManager lets resident named agents create group threads and add
// named teammates to groups they belong to. The app server injects an
// implementation per resident runtime; task runs and ordinary subagents
// never receive one (the tools are additionally gated on resident
// participant capability).
type GroupManager interface {
	// CreateGroup creates a group thread with the given title and adds the
	// calling participant as its first member. Returns the new thread ID.
	CreateGroup(ctx context.Context, title string) (string, error)
	// AddGroupMember adds a named participant to a group thread the caller
	// belongs to. Adding an existing member is a no-op success.
	AddGroupMember(ctx context.Context, threadID, participantID string) error
	// ListGroupMembers returns the named-agent members of a group thread the
	// caller belongs to. It is the participant pool a workflow bound to that
	// thread may dispatch named_participant slots to. Non-group or unknown
	// threads return an empty list.
	ListGroupMembers(ctx context.Context, threadID string) ([]GroupMember, error)
}

// GroupMember is a named agent in a group thread, identified by its stable
// participant ID and its display Name (the identity axis). Busy reports that
// the member is currently executing another task/workflow run (decision-five
// concurrency lock), so a workflow that tries to enlist it is told busy
// instead of racing the same resident agent.
type GroupMember struct {
	ID   string
	Name string
	Busy bool
}

// TaskView is the tool-facing snapshot of one task on the agent task rail (a
// cth in task/review status). Owner is work ownership (mutual exclusion,
// reporting duty) — never lead/orchestration authority.
type TaskView struct {
	ID           string `json:"id"`
	ThreadID     string `json:"thread_id"`
	AnchorItemID string `json:"anchor_item_id,omitempty"`
	Title        string `json:"title,omitempty"`
	Status       string `json:"status"`
	Owner        string `json:"owner,omitempty"`
	OwnerName    string `json:"owner_name,omitempty"`
	CreatedBy    string `json:"created_by,omitempty"`
	Summary      string `json:"summary,omitempty"`
}

// TaskManager lets resident named agents run the task rail: create tasks,
// claim/release work ownership, file for review, unfollow, and list the
// board. The app server injects an implementation per resident runtime; task
// runs and ordinary subagents never receive one. Claim mutual exclusion is a
// store-level CAS: losing the race is a normal result (claimed=false), not an
// error.
type TaskManager interface {
	// CreateTask opens a born-task cth in a group thread the caller belongs
	// to. anchorSeq > 0 anchors it on that main-stream message (at most one
	// cth per anchor); 0 creates a standalone task. claim self-owns it
	// atomically in the same call. ackCollisionID is the strict-id-match
	// escape hatch for the standalone-dedup collision check (issue #4 v3):
	// when the same title already exists in the thread as unfinished,
	// passing the existing task's id lets the caller persist a same-titled
	// duplicate (work-splitting case); any other value — empty, wrong id,
	// made-up value — keeps the dedup hard-block. Anchored tasks ignore
	// ackCollisionID — one anchor, one cth, period.
	CreateTask(ctx context.Context, threadID string, anchorSeq int, title string, claim bool, ackCollisionID string) (TaskView, error)
	// EscalateTask converts an open discussion reply the caller belongs to
	// into a board task (open -> task). It grants no lead/orchestration
	// authority; claim self-owns it in the same call.
	EscalateTask(ctx context.Context, subthreadID, title string, claim bool) (TaskView, error)
	// ClaimTask takes work ownership via CAS. claimed=false with a nil error
	// means someone else owns it (see the returned view's Owner).
	ClaimTask(ctx context.Context, subthreadID string) (TaskView, bool, error)
	// UnclaimTask releases ownership (owner-only); the task becomes claimable.
	UnclaimTask(ctx context.Context, subthreadID string) (TaskView, error)
	// FileTaskReview advances an owned task to review with the one-line
	// summary draft (owner-only). Under task_review: auto the summary bubbles
	// to the main stream and the task resolves immediately.
	FileTaskReview(ctx context.Context, subthreadID, summary string) (TaskView, error)
	// UnfollowTask removes the caller from the task's push subset; the task
	// stays readable via fetch_thread_messages.
	UnfollowTask(ctx context.Context, subthreadID string) error
	// ListTasks returns the group's task board (task and review statuses).
	ListTasks(ctx context.Context, threadID string) ([]TaskView, error)
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
	return e != nil && e.Unconfined
}

func (e *Env) RedactToolOutput(text string) string {
	if e.BypassToolHardProtections() {
		return text
	}
	return redactToolOutput(text)
}

// ResolvePath resolves a user-supplied relative or absolute path. Path
// confinement is always enforced unless the runtime is explicitly unconfined.
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
	if len(e.FileScopeRoots) > 0 {
		for _, root := range e.FileScopeRoots {
			if pathWithinRoot(root, resolved) {
				return resolved, nil
			}
		}
		return "", fmt.Errorf("path %q is outside the allowed file scope (agent home directory, registered workspaces, and the system temp directory): 该路径不在工作区内，请用户在侧栏添加该目录为工作区后重试", input)
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

// ---------------------------------------------------------------------------
// Worktree execution root (fork-to-worktree step 5)
//
// A thread forked with mode "worktree" persists the checkout path in its
// session metadata; the turn entry injects it into the tool execution
// context via toolctx.WithWorktreePath. The helpers below apply that
// binding STRICTLY AFTER the ordinary sandbox / whitelist checks:
//
//   - the sandbox keeps judging the model-visible workspace paths against
//     RootDir / FileScopeRoots exactly as before (nothing is loosened, and
//     the checkout — which usually lives under the wuu state directory —
//     is never fed into those checks where it would look out-of-bounds);
//   - only a path that already passed is then rebased onto the checkout,
//     so relative-path resolution, bash cwd, and search roots all switch
//     consistently to the isolated copy;
//   - whitelisted roots outside the workspace (the user memory notebook,
//     the system temp dir) are not mirrored by the checkout and pass
//     through unchanged.
//
// When the toolkit is already rooted at the checkout (the normal thread
// runtime path), the binding equals RootDir and every helper is a no-op.
// ---------------------------------------------------------------------------

// worktreeExecRoot returns the ctx-bound worktree checkout when one is
// bound and differs from RootDir. A bound checkout that is missing on disk
// is an error — tools must fail loudly instead of silently falling back to
// the parent repo the user believes is isolated.
func (e *Env) worktreeExecRoot(ctx context.Context) (string, bool, error) {
	path, ok := toolctx.WorktreePath(ctx)
	if !ok {
		return "", false, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, fmt.Errorf("resolve worktree checkout %q: %w", path, err)
	}
	if ev, err := filepath.EvalSymlinks(abs); err == nil {
		abs = ev
	}
	root := strings.TrimSpace(e.RootDir)
	if root != "" {
		evalRoot := root
		if absRoot, err := filepath.Abs(root); err == nil {
			evalRoot = absRoot
		}
		if ev, err := filepath.EvalSymlinks(evalRoot); err == nil {
			evalRoot = ev
		}
		if filepath.Clean(evalRoot) == filepath.Clean(abs) {
			return "", false, nil
		}
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", false, fmt.Errorf("worktree checkout %q is not ready (missing or not a directory); this thread is bound to an isolated worktree and refuses to fall back to the parent repository", path)
	}
	return abs, true, nil
}

// ExecRootDir returns the directory command-style tools (bash cwd, git,
// search roots) execute in: the bound worktree checkout when present,
// otherwise RootDir.
func (e *Env) ExecRootDir(ctx context.Context) (string, error) {
	wt, ok, err := e.worktreeExecRoot(ctx)
	if err != nil {
		return "", err
	}
	if ok {
		return wt, nil
	}
	return e.RootDir, nil
}

// ExecPath maps a sandbox-approved absolute path onto the bound worktree
// checkout. Call it only AFTER ResolvePath / ResolveReadPath and the
// sensitive-path checks have accepted the workspace path. Paths outside
// RootDir (whitelisted roots such as the user memory notebook or the
// system temp dir) are returned unchanged.
func (e *Env) ExecPath(ctx context.Context, resolved string) (string, error) {
	wt, ok, err := e.worktreeExecRoot(ctx)
	if err != nil {
		return "", err
	}
	if !ok {
		return resolved, nil
	}
	rel, ok := workspaceRelativePath(e.RootDir, resolved)
	if !ok {
		return resolved, nil
	}
	if rel == "." {
		return wt, nil
	}
	return filepath.Join(wt, rel), nil
}

// NormalizeDisplayPathExec keeps the existing display convention for
// execution paths: paths under the bound worktree checkout display
// relative to the checkout (exactly what the same file would display as in
// the parent workspace), everything else falls back to NormalizeDisplayPath.
func (e *Env) NormalizeDisplayPathExec(ctx context.Context, absPath string) string {
	if wt, ok, err := e.worktreeExecRoot(ctx); err == nil && ok && pathWithinRoot(wt, absPath) {
		return normalizeDisplayPath(wt, absPath)
	}
	return e.NormalizeDisplayPath(absPath)
}

// RevisionRoot is the directory workspace_revision telemetry should be
// computed from: the bound worktree checkout when present and ready,
// otherwise RootDir. Telemetry-only; never errors.
func (e *Env) RevisionRoot(ctx context.Context) string {
	if wt, ok, err := e.worktreeExecRoot(ctx); err == nil && ok {
		return wt
	}
	return e.RootDir
}

// workspaceRelativePath returns path relative to root when path resolves
// inside root (symlink-tolerant, missing paths allowed), mirroring the
// pathWithinRoot rules.
func workspaceRelativePath(root, path string) (string, bool) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	evalRoot := absRoot
	if ev, err := filepath.EvalSymlinks(evalRoot); err == nil {
		evalRoot = ev
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	evalPath := absPath
	if ev, err := filepath.EvalSymlinks(evalPath); err == nil {
		evalPath = ev
	} else if rel, relErr := filepath.Rel(absRoot, absPath); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		evalPath = filepath.Join(evalRoot, rel)
	}
	rel, err := filepath.Rel(evalRoot, evalPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
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
