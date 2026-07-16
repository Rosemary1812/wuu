package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/automation"
	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/goalruntime"
	proc "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/skills"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/toolctx"
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

	// AllowMutations mirrors WorkspaceBoundary.AllowMutations for the active
	// runtime. Set by Toolkit.SetBoundary. False in read-only mode even when
	// Unconfined is also false; tools use it to allow per-path exemptions
	// (such as the agent's own runtime metadata directory) without
	// re-deriving the boundary state.
	AllowMutations bool

	// Optional dependencies — nil means the feature is unavailable.
	// Tools check for nil and return a clear error rather than panic.
	SessionID  string
	SessionDir string // absolute session artifact path for result budgeting
	// ToolResultProjectionMode selects stable tool-result projection behavior
	// ("off"/"shadow"/"active"); empty resolves to active (on by default). The
	// WUU_TOOL_RESULT_PROJECTION environment variable overrides it.
	ToolResultProjectionMode string
	GoalRuntime              *goalruntime.Runtime
	AgentID                  string
	AgentPath                string
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
	// GitAttributionDisabled is the opt-out bit for WUU's commit trailer.
	// The zero value intentionally means enabled so ordinary toolkits inherit
	// the product default without extra initialization.
	GitAttributionDisabled bool
	// GitWrapperExecutable overrides the WUU executable used by the private
	// bash git launcher. Production resolves os.Executable when this is empty.
	GitWrapperExecutable string
	gitAttributionShell  gitAttributionShellState
	ProcessMgr           *proc.Manager
	AgentControl         *agentcontrol.AgentControl
	AutomationManager    *automation.Manager
	ParticipantSpeech    ParticipantSpeech
	// GroupManager backs the resident-only create_group / add_member actions
	// of manage_participant. Nil means group management is unavailable in this
	// environment (those actions return an execute-time error).
	GroupManager GroupManager
	// TaskManager backs the resident-only group Thread/Task workflow. Nil means
	// that workflow is unavailable in this
	// environment (every action returns an execute-time error).
	TaskManager TaskManager
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
	// Held is set when the post was NOT published because the thread moved
	// since this turn read it: newer messages arrived while the agent was
	// composing, so the draft may now be redundant or out of date. HeldNote
	// carries what arrived. The agent decides its next move: revise (post a
	// new draft), resend unchanged (post again with force=true), or stay
	// silent. Never set for the fresh-post path or for a forced post.
	Held     bool   `json:"held,omitempty"`
	HeldNote string `json:"held_note,omitempty"`
	// BasisSeq echoes the message seq this post declared it was generated
	// against (0 = the agent's read cursor was used). The freshness check holds
	// the post if any message from someone else arrived after this basis.
	BasisSeq int `json:"basis_seq,omitempty"`
}

type ParticipantSpeech interface {
	// PostMessage publishes a participant message. basisSeq is the message seq
	// the agent generated this post against (its view of "latest"); the system
	// mechanically holds the post if the thread moved past that basis (a newer
	// message from someone else). 0 means "use my read cursor as the basis".
	// force=true skips the check (see PostedMessage.Held): use it to resend a
	// draft the agent has decided is still right even though the room moved.
	PostMessage(ctx context.Context, kind, text, targetThreadID string, basisSeq int, force bool) (PostedMessage, error)
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
// the member is currently executing another task run (decision-five
// concurrency lock), so another caller trying to enlist it is told busy
// instead of racing the same resident agent.
type GroupMember struct {
	ID   string
	Name string
	Busy bool
}

// TaskView is the tool-facing snapshot of one group Thread or Task. The Thread
// owner becomes the immutable lead on promotion; the lead orchestrates work
// but is never a plan-piece assignee.
type TaskView struct {
	ID           string `json:"id"`
	ThreadID     string `json:"thread_id"`
	AnchorItemID string `json:"anchor_item_id,omitempty"`
	Title        string `json:"title,omitempty"`
	Status       string `json:"status"`
	// ExecState is the task's execution axis, separate from the approval
	// Status: planning/executing/awaiting_lead/blocked/needs_human/completed/failed; empty
	// when the task never entered execution.
	ExecState   string `json:"exec_state,omitempty"`
	ThreadOwner string `json:"thread_owner,omitempty"`
	Lead        string `json:"lead,omitempty"`
	LeadName    string `json:"lead_name,omitempty"`
	CreatedBy   string `json:"created_by,omitempty"`
	Summary     string `json:"summary,omitempty"`
	// Plan is the lead's declared work breakdown, present only after promotion
	// and planning.
	Plan []TaskPiece `json:"plan,omitempty"`
}

// TaskPiece is one unit of a team task's plan as it crosses the tool boundary
// (mirrors session.TaskPiece). Status: pending -> active -> done. Prompt is
// the only node field the lead authors through set_plan — handoff, attempts,
// and retry budget are engine-owned and never settable from the tool surface.
type TaskPiece struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Assignee         string   `json:"assignee"`
	DependsOn        []string `json:"depends_on,omitempty"`
	Status           string   `json:"status,omitempty"`
	CurrentAttemptID string   `json:"current_attempt_id,omitempty"`
	// Prompt is the lead-authored briefing the assignee is woken with when
	// the engine dispatches the piece.
	Prompt string `json:"prompt,omitempty"`
	// State is the display label the task panel renders, derived purely from
	// Status (completed/failed/blocked/retrying/active/pending) so the panel
	// never depends on the internal status vocabulary (plan §T9).
	State string `json:"state,omitempty"`
	// LastActivityAt / LastProgressAt are the node's two liveness timestamps,
	// exposed raw: activity is any observable action by the assignee (a tool
	// call), progress is a declared step forward (a filed handoff, a public
	// task-thread update). They are surfaced as raw runtime facts; the frontend
	// may describe their recency but does not infer a hidden execution state.
	LastActivityAt time.Time `json:"last_activity_at,omitzero"`
	LastProgressAt time.Time `json:"last_progress_at,omitzero"`
}

// TaskHandoff is the structured result a node hands to its downstream as it
// crosses the tool boundary (mirrors session.TaskHandoff). An assignee attaches
// it to manage_task action=piece_done; the engine writes it onto the downstream
// node and renders it into that node's wake. It is the next node's real input —
// deliberately distinct from a public post_message update, which is prose for the
// human and is never a node's input.
type TaskHandoff struct {
	Done       string   `json:"done,omitempty"`
	Findings   string   `json:"findings,omitempty"`
	Artifacts  []string `json:"artifacts,omitempty"`
	Limits     string   `json:"limits,omitempty"`
	NextGoal   string   `json:"next_goal,omitempty"`
	Acceptance string   `json:"acceptance,omitempty"`
	Notes      string   `json:"notes,omitempty"`
}

type TaskEvent struct {
	Seq       int       `json:"seq"`
	NodeID    string    `json:"node_id,omitempty"`
	AttemptID string    `json:"attempt_id,omitempty"`
	Kind      string    `json:"kind"`
	Actor     string    `json:"actor,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	Payload   string    `json:"payload,omitempty"`
	At        time.Time `json:"at"`
}

// TaskManager lets resident named agents run the group-only Thread -> Task
// workflow. The app server injects one per resident runtime; task workers and
// ordinary subagents never receive it.
type TaskManager interface {
	// OpenThread starts or returns the durable Thread anchored to one real group
	// message. anchorSeq is required. A named parent author owns it; a human
	// parent is owned by the caller. Existing ownership is never transferred.
	OpenThread(ctx context.Context, threadID string, anchorSeq int, title string) (TaskView, error)
	// PromoteThread converts an open Thread to a Task. Only its persisted owner
	// may call it, and that owner becomes immutable Task lead.
	PromoteThread(ctx context.Context, subthreadID, title string) (TaskView, error)
	// ConcludeTask files the task's conclusion and completes it in one act:
	// the summary bubbles to the parent main stream under the caller's
	// identity and the task resolves immediately — no review gate. The
	// task can only be concluded by its lead.
	ConcludeTask(ctx context.Context, subthreadID, summary string) (TaskView, error)
	// NeedHuman flags the task as waiting on a decision that genuinely
	// belongs to the human (exec state needs_human + a blocked trace event
	// with the reason). It wakes nobody — the human decides from the board.
	NeedHuman(ctx context.Context, subthreadID, reason string) (TaskView, error)
	// NeedUpstream is the assignee's fallback when the handoff its upstream
	// gave it is insufficient (plan §T8): it bounces the work back instead of
	// working around a bad input or rewriting the plan. The caller must be the
	// piece's assignee, actively running it, and the piece must depend on an
	// upstream. The downstream node is parked to pending and each upstream is
	// re-dispatched with a directive naming what was missing; when an upstream
	// re-files piece_done the engine re-runs the downstream with its fresh
	// handoff.
	NeedUpstream(ctx context.Context, subthreadID, pieceID, reason string) (TaskView, error)
	// UnfollowTask removes the caller from the task's push subset; the task
	// stays readable via fetch_thread_messages.
	UnfollowTask(ctx context.Context, subthreadID string) error
	// ListWorkflowThreads returns the group's open Threads and active Tasks.
	ListWorkflowThreads(ctx context.Context, threadID string) ([]TaskView, error)
	TraceTask(ctx context.Context, subthreadID string) ([]TaskEvent, error)
	// SetPlan declares the lead's work breakdown for a team task (task-rail
	// design §8): pieces with assignees and dependencies. The engine then
	// dispatches every piece whose dependencies are already satisfied by
	// @-waking its assignee into the task thread. The caller must belong to
	// the task's thread; assignees must be members.
	SetPlan(ctx context.Context, subthreadID string, pieces []TaskPiece) (TaskView, error)
	AddTaskPiece(ctx context.Context, subthreadID string, piece TaskPiece) (TaskView, error)
	ReviseTaskPiece(ctx context.Context, subthreadID, pieceID, title, prompt string, dependsOn []string) (TaskView, error)
	ReassignTaskPiece(ctx context.Context, subthreadID, pieceID, assignee string) (TaskView, error)
	RetryTaskPiece(ctx context.Context, subthreadID, pieceID, reason string) (TaskView, error)
	CancelTaskPiece(ctx context.Context, subthreadID, pieceID, reason string) (TaskView, error)
	ResumeTask(ctx context.Context, subthreadID, reason string) (TaskView, error)
	// PieceDone marks one plan piece complete (called by its assignee) and
	// hands the structured result to the downstream node(s). The engine writes
	// handoff onto every piece that depends on this one — or, for a terminal
	// piece, onto the piece itself — then dispatches any piece whose
	// dependencies are now satisfied (carrying the handoff into its wake), or —
	// when every piece is done — wakes the lead to wrap up and report. handoff
	// is nil when the assignee reported no structured result; the handoff is the
	// downstream's input, not a public thread update. It is the early / rich
	// completion path: a node also completes when its dispatched turn ends, so
	// piece_done is how an assignee finishes early or hands a structured result.
	PieceDone(ctx context.Context, subthreadID, pieceID string, handoff *TaskHandoff) (TaskView, error)
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
	// Agent's own runtime metadata (statepath.Home) is allowed when the
	// boundary permits mutations. Read-only mode keeps the gate; tools
	// that go through this path without AllowMutations still see a denial
	// for paths under the agent's runtime directory.
	if e.AllowMutations && isAgentRuntimeMetadataPath(resolved) {
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

	if _, ok := relInsideRoot(evalRoot, evalResolved); !ok {
		return "", fmt.Errorf("path %q escapes workspace", input)
	}
	return resolved, nil
}

// relInsideRoot reports whether path stays inside root, returning the
// relative remainder when it does. Both inputs must already be absolute
// and cleaned. Windows filesystems compare names case-insensitively, so
// the containment decision folds case there while the returned remainder
// keeps the caller's casing.
func relInsideRoot(root, path string) (string, bool) {
	cmpRoot, cmpPath := root, path
	if runtime.GOOS == "windows" {
		cmpRoot = strings.ToLower(cmpRoot)
		cmpPath = strings.ToLower(cmpPath)
	}
	rel, err := filepath.Rel(cmpRoot, cmpPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if rel == "." {
		return ".", true
	}
	if cmpRoot == root && cmpPath == path {
		return rel, true
	}
	// Case was folded for the decision; slice the remainder out of the
	// original path so its casing survives. Inside-ness guarantees path
	// extends root.
	remainder := strings.TrimLeft(path[len(root):], string(filepath.Separator))
	if remainder == "" {
		return ".", true
	}
	return remainder, true
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
	} else if rel, ok := relInsideRoot(absRoot, absPath); ok {
		evalPath = filepath.Join(evalRoot, rel)
	}
	_, ok := relInsideRoot(evalRoot, evalPath)
	return ok
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
	} else if rel, ok := relInsideRoot(absRoot, absPath); ok {
		evalPath = filepath.Join(evalRoot, rel)
	}
	return relInsideRoot(evalRoot, evalPath)
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

// OrchestrationStateDir returns the state root for user-visible orchestration
// artifacts. Interactive turns bind tools to a SessionID, so their Goals live
// with that conversation. Headless or workspace-level tools without a SessionID
// keep using workspace state.
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
