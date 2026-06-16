package harness

import (
	"encoding/json"
	"time"
)

// TaskStatus describes a durable task node in the harness graph.
type TaskStatus string

const (
	TaskStatusPending        TaskStatus = "pending"
	TaskStatusQueued         TaskStatus = "queued"
	TaskStatusRunning        TaskStatus = "running"
	TaskStatusAwaitingReport TaskStatus = "awaiting_report"
	TaskStatusCompleted      TaskStatus = "completed"
	TaskStatusFailed         TaskStatus = "failed"
	TaskStatusCancelled      TaskStatus = "cancelled"
)

// WorkspaceMode records where a task is allowed to execute.
type WorkspaceMode string

const (
	WorkspaceShared   WorkspaceMode = "shared"
	WorkspaceWorktree WorkspaceMode = "worktree"
)

// ArtifactKind classifies durable outputs that later agents can inspect.
type ArtifactKind string

const (
	ArtifactReport       ArtifactKind = "report"
	ArtifactPatch        ArtifactKind = "patch"
	ArtifactArchive      ArtifactKind = "archive"
	ArtifactCommand      ArtifactKind = "command_output"
	ArtifactLog          ArtifactKind = "log"
	ArtifactManifest     ArtifactKind = "manifest"
	ArtifactEvidence     ArtifactKind = "evidence"
	ArtifactResult       ArtifactKind = "agent_result"
	ArtifactConversation ArtifactKind = "conversation"
)

// EventType describes append-only harness lifecycle facts.
type EventType string

const (
	EventTaskCreated       EventType = "task_created"
	EventTaskStatusChanged EventType = "task_status_changed"
	EventWorkspaceAssigned EventType = "workspace_assigned"
	EventRunStarted        EventType = "run_started"
	EventRunCompleted      EventType = "run_completed"
	EventMessageSent       EventType = "message_sent"
	EventReportSubmitted   EventType = "report_submitted"
	EventArtifactRecorded  EventType = "artifact_recorded"
)

// WorkspaceLease is the execution location assigned to a task.
type WorkspaceLease struct {
	Mode      WorkspaceMode `json:"mode"`
	Root      string        `json:"root,omitempty"`
	BaseRepo  string        `json:"base_repo,omitempty"`
	CreatedAt time.Time     `json:"created_at,omitempty"`
}

// Task is the durable source of truth for one harness task node.
type Task struct {
	ID            string         `json:"id"`
	SessionID     string         `json:"session_id,omitempty"`
	ParentID      string         `json:"parent_id,omitempty"`
	ParentPath    string         `json:"parent_path,omitempty"`
	Path          string         `json:"path,omitempty"`
	Name          string         `json:"name,omitempty"`
	Role          string         `json:"role,omitempty"`
	Intent        string         `json:"intent,omitempty"`
	GoalID        string         `json:"goal_id,omitempty"`
	GoalDir       string         `json:"goal_dir,omitempty"`
	Constraints   []string       `json:"constraints,omitempty"`
	Workspace     WorkspaceLease `json:"workspace,omitempty"`
	Status        TaskStatus     `json:"status"`
	LastRunID     string         `json:"last_run_id,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	StartedAt     time.Time      `json:"started_at,omitempty"`
	CompletedAt   time.Time      `json:"completed_at,omitempty"`
	InputTokens   int            `json:"input_tokens,omitempty"`
	OutputTokens  int            `json:"output_tokens,omitempty"`
	Error         string         `json:"error,omitempty"`
	ReportPath    string         `json:"report_path,omitempty"`
	ArtifactPaths []string       `json:"artifact_paths,omitempty"`
}

// AgentRun records one execution attempt for a task.
type AgentRun struct {
	ID           string     `json:"id"`
	TaskID       string     `json:"task_id"`
	AgentID      string     `json:"agent_id,omitempty"`
	Role         string     `json:"role,omitempty"`
	Model        string     `json:"model,omitempty"`
	Status       TaskStatus `json:"status"`
	StartedAt    time.Time  `json:"started_at"`
	CompletedAt  time.Time  `json:"completed_at,omitempty"`
	InputTokens  int        `json:"input_tokens,omitempty"`
	OutputTokens int        `json:"output_tokens,omitempty"`
	Error        string     `json:"error,omitempty"`
}

// Artifact points to durable evidence, reports, patches, or logs.
type Artifact struct {
	ID        string       `json:"id"`
	TaskID    string       `json:"task_id"`
	RunID     string       `json:"run_id,omitempty"`
	Kind      ArtifactKind `json:"kind"`
	Path      string       `json:"path,omitempty"`
	Summary   string       `json:"summary,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
}

// EvidenceRef keeps a report tied to source material instead of only prose.
type EvidenceRef struct {
	Type    string `json:"type,omitempty"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Command string `json:"command,omitempty"`
	Output  string `json:"output,omitempty"`
	Note    string `json:"note,omitempty"`
}

// Report is the structured handoff packet submitted by an agent.
type Report struct {
	ID           string        `json:"id"`
	TaskID       string        `json:"task_id"`
	RunID        string        `json:"run_id,omitempty"`
	AgentID      string        `json:"agent_id,omitempty"`
	AgentPath    string        `json:"agent_path,omitempty"`
	Outcome      string        `json:"outcome"`
	Summary      string        `json:"summary,omitempty"`
	ChangedFiles []string      `json:"changed_files,omitempty"`
	WorkDone     []string      `json:"work_done,omitempty"`
	Blockers     []string      `json:"blockers,omitempty"`
	Risks        []string      `json:"risks,omitempty"`
	Verification []string      `json:"verification,omitempty"`
	NextSteps    []string      `json:"next_steps,omitempty"`
	Evidence     []EvidenceRef `json:"evidence,omitempty"`
	Artifacts    []string      `json:"artifacts,omitempty"`
	RawResult    string        `json:"raw_result,omitempty"`
	ReportPath   string        `json:"report_path,omitempty"`
	SubmittedAt  time.Time     `json:"submitted_at"`
}

// Event is one append-only fact in the task graph.
type Event struct {
	Type      EventType `json:"type"`
	TaskID    string    `json:"task_id,omitempty"`
	RunID     string    `json:"run_id,omitempty"`
	AgentID   string    `json:"agent_id,omitempty"`
	Path      string    `json:"path,omitempty"`
	Status    string    `json:"status,omitempty"`
	Message   string    `json:"message,omitempty"`
	Artifact  string    `json:"artifact,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// QueueItem stores the durable payload needed to start a queued task.
type QueueItem struct {
	ID        string          `json:"id"`
	TaskID    string          `json:"task_id"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}
