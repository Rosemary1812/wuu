// Package subagent runs isolated sub-agents that the coordinator can
// spawn to perform focused tasks. Each sub-agent has its own chat
// history, its own tool subset, runs against the same LLM client, and
// returns its final assistant message back to the orchestrator.
//
// This package is provider-agnostic — it operates over the providers
// abstraction so any backend wuu supports also supports sub-agents.
package subagent

import (
	"context"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolledger"
)

// Status describes a sub-agent's lifecycle state.
type Status string

const (
	StatusPending   Status = "pending"
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
	// StatusInterrupted is the terminal state stamped by startup
	// reconciliation on a run whose owning process exited while the run
	// was still live (crash, kill, power loss). No goroutine observed the
	// end of the run, so unlike failed/cancelled it carries no runtime
	// error of its own — only the reconciliation reason. Interrupted runs
	// are resumable through Followup exactly like other terminal states.
	StatusInterrupted Status = "interrupted"
	// StatusWaitingChildren parks a run that produced its final message
	// while direct children were still live. The result is held (not yet
	// delivered to the parent), no goroutine runs and no execution slot is
	// consumed; a child delivery resumes the run through Followup so it can
	// integrate the results before one final parent delivery. Not terminal:
	// the run becomes terminal only once no direct child remains live.
	StatusWaitingChildren Status = "waiting_children"
)

// IsTerminal reports whether the status is a terminal lifecycle state — the
// run has no live goroutine and can accept a Followup to start a new turn.
func IsTerminal(status Status) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusCancelled, StatusInterrupted:
		return true
	default:
		return false
	}
}

// SpawnOptions describes a new sub-agent to launch.
type SpawnOptions struct {
	// ID, when set, is used as the sub-agent ID instead of generating
	// a new one. The coordinator uses this to keep worktree IDs,
	// thread metadata, and visible agent IDs aligned.
	ID string

	// ParticipantID is the conversation participant identity created
	// for this worker. The subagent package carries it through to
	// snapshots so history records and notifications can attribute
	// output to the right participant.
	ParticipantID string

	// Type is a label like "explorer", "worker", "verifier" that the
	// caller uses to track agent roles. The subagent package itself
	// doesn't interpret it — type-specific tool whitelists and prompts
	// are decided by the caller.
	Type string

	// TaskName is a stable human-provided task identifier. AgentPath is
	// the canonical path in the root thread's agent tree.
	TaskName     string
	AgentProfile string
	AgentPath    string
	ParentID     string

	// Description is a short human-readable summary of the task,
	// shown in status displays.
	Description string

	// Prompt is the initial user-role message sent to the sub-agent.
	// It must be self-contained: the sub-agent does not see the
	// orchestrator's history.
	Prompt string

	// SystemPrompt is the system-role message that establishes the
	// sub-agent's role and constraints.
	SystemPrompt string

	// Toolkit is the tool executor the sub-agent will use. It should
	// already be configured with the correct rootDir (e.g. a worktree
	// path) and any tool restrictions.
	Toolkit agent.ToolExecutor

	// Model is the model identifier to use. Empty inherits whatever
	// the runner's default is.
	Model string

	// WorkerRoot is the sub-agent's working directory: the shared parent
	// repo for inplace workers or the worktree path for isolated ones. It
	// is persisted with the run so a follow-up after a restart can rebuild
	// the toolkit rooted at the same place and confirm the directory still
	// exists before resuming.
	WorkerRoot string

	// ModelPin is the raw per-participant model pin (e.g. "provider:model"
	// or "model"). It is persisted so a resumed run can rebuild a
	// cross-provider client the same way queued-spawn restore does. Empty
	// means the run honors only Model.
	ModelPin string

	// Ultra is the worker's inherited Ultra-mode value, snapshotted from
	// its spawner. It is persisted with the run and stays fixed across the
	// worker's lifetime, resumption, and descendants.
	Ultra bool

	// Client, when non-nil, replaces the stream client the runner
	// would otherwise inherit from the manager defaults. Callers use
	// it to honor a per-participant model pin that points at a
	// different provider — the coordinator builds a dedicated stream
	// client for that provider and passes it through here. Runtime
	// fields on the agent control keep the override scoped to this
	// single spawn; the manager defaults are not modified.
	Client providers.StreamClient

	// MaxSteps caps how many tool-use rounds the sub-agent can run
	// before being forced to wrap up. Zero uses the runner default.
	MaxSteps int

	// HistoryPath, if set, is the absolute path where this sub-agent's
	// JSONL history should be persisted.
	HistoryPath string

	// MaxLifetime caps how long a worker can run before being forcibly
	// cancelled. Zero means no hard runtime lifetime; callers can still
	// cancel the worker through the context or Stop.
	MaxLifetime time.Duration

	// InitialHistory, when non-nil, seeds the worker's conversation
	// with this exact message slice instead of starting from
	// [system, user_prompt]. Used by spawn_agent fork spawns so the
	// worker inherits the parent's history verbatim, which is what
	// makes prompt-cache hit work across the fork boundary.
	//
	// When InitialHistory is set:
	//   - SystemPrompt on this struct is IGNORED. The system message
	//     is whatever the caller put at history[0] (typically the
	//     parent's system prompt verbatim, for cache friendliness).
	//   - Prompt becomes the FINAL user message appended to the
	//     history, not the only user message. It is the place to
	//     inject role-override instructions (e.g. wrapped in a
	//     <system-reminder> block).
	//   - The history MUST end with a complete turn — no dangling
	//     tool_use without a matching tool_result, or the provider
	//     API will reject the worker's first request. The caller is
	//     responsible for stripping any in-flight tool_use blocks before
	//     passing the history through.
	InitialHistory []providers.ChatMessage
}

// SubAgent is an isolated agent instance managed by Manager.
type SubAgent struct {
	ID                  string
	ParticipantID       string
	Type                string
	TaskName            string
	AgentProfile        string
	AgentPath           string
	ParentID            string
	Description         string
	Status              Status
	StartedAt           time.Time
	CompletedAt         time.Time
	Result              string // final assistant message text
	Error               error  // populated when Status == failed
	InputTokens         int    // cumulative input tokens used so far
	OutputTokens        int    // cumulative output tokens used so far
	CacheCreationTokens int    // cumulative prompt-cache write tokens
	CacheReadTokens     int    // cumulative prompt-cache read tokens

	// Activity is a short, human-readable phrase describing what the
	// sub-agent is currently doing ("→ read_file", "thinking",
	// "responding"). Mutated by the event callback in Manager.run and
	// read via Snapshot. Only changes when the phase changes, so the
	// observer sees transitions rather than a stream of deltas.
	Activity   string
	ActivityAt time.Time

	// Internal state — read-only from outside.
	prompt          string
	systemPrompt    string
	model           string
	modelPin        string // raw participant pin, persisted for cross-restart resume
	workerRoot      string // working directory, persisted so a resume can validate + reroot
	ultra           bool   // inherited Ultra-mode value, fixed at spawn
	toolkit         agent.ToolExecutor
	historyPath     string
	initialHistory  []providers.ChatMessage
	history         []providers.ChatMessage
	maxSteps        int
	maxLifetime     time.Duration
	runtimeDefaults managerDefaults
	// Follow-up messages queued by the coordinator while this worker
	// is already running. Manager.run drains this queue between model
	// turns and appends each entry as a new user message.
	pendingMessages []string
	// forceToolNextTurn, when non-empty, pins the first request of the
	// next turn to the named tool (forced tool_choice). Set by
	// FollowupForcingTool for mechanical closing turns; consumed and
	// cleared by runTurn.
	forceToolNextTurn string

	// LLM client for the sub-agent's runner. Workers run through the
	// streaming runner so they share the same transport semantics as
	// the interactive main agent — most importantly, they avoid the
	// short idle timeouts that proxies tend to apply to non-stream
	// HTTP requests during long worker runs.
	client     providers.StreamClient
	toolLedger *toolledger.Ledger

	// Lifecycle plumbing.
	cancelFunc context.CancelFunc
	doneCh     chan struct{}

	// Synchronizes Status / Result / Error access between the runner
	// goroutine and observers.
	mu sync.Mutex
}

// Snapshot returns a read-only copy of the sub-agent's public fields.
// Safe to call from any goroutine.
func (s *SubAgent) Snapshot() SubAgentSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return snapshotLocked(s)
}

// HistorySnapshot returns a copy of the worker's current conversation.
func (s *SubAgent) HistorySnapshot() []providers.ChatMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return providers.CloneChatMessages(s.history)
}

// SubAgentSnapshot is an immutable view of a SubAgent's state at a
// point in time.
type SubAgentSnapshot struct {
	ID                  string
	ParticipantID       string
	Type                string
	TaskName            string
	AgentProfile        string
	AgentPath           string
	ParentID            string
	Description         string
	WorkerRoot          string
	Model               string
	ModelPin            string
	Ultra               bool
	Status              Status
	StartedAt           time.Time
	CompletedAt         time.Time
	Result              string
	Error               error
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	PendingMessageCount int
	Activity            string
	ActivityAt          time.Time
}

// Notification is sent to listeners when a sub-agent's status changes
// (started, completed, failed, cancelled).
type Notification struct {
	AgentID  string
	Status   Status
	Snapshot SubAgentSnapshot
}

// StreamNotification is sent to listeners for every model/tool stream event
// emitted by a sub-agent turn.
type StreamNotification struct {
	AgentID  string
	Snapshot SubAgentSnapshot
	Event    providers.StreamEvent
}
