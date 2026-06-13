package loop

import "time"

type Status string

const (
	StatusPending    Status = "pending"
	StatusRunning    Status = "running"
	StatusBlocked    Status = "blocked"
	StatusNeedsHuman Status = "needs_human"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled"
)

type Step string

const (
	StepInit         Step = "init"
	StepResearch     Step = "research"
	StepPlan         Step = "plan"
	StepApproval     Step = "approval"
	StepExecution    Step = "execution"
	StepVerification Step = "verification"
	StepReview       Step = "review"
	StepIntegration  Step = "integration"
	StepSummary      Step = "summary"
)

type Trigger struct {
	Type    string            `json:"type,omitempty"`
	Source  string            `json:"source,omitempty"`
	Payload map[string]string `json:"payload,omitempty"`
}

type Permissions struct {
	ReadOnly                   bool     `json:"read_only,omitempty"`
	EditAllowedPaths           []string `json:"edit_allowed_paths,omitempty"`
	ShellAllowedCommands       []string `json:"shell_allowed_commands,omitempty"`
	NetworkAllowed             bool     `json:"network_allowed,omitempty"`
	BrowserAllowed             bool     `json:"browser_allowed,omitempty"`
	GitAllowedOperations       []string `json:"git_allowed_operations,omitempty"`
	DestructiveActionApproval  bool     `json:"destructive_action_approval,omitempty"`
	SecretAccessPolicy         string   `json:"secret_access_policy,omitempty"`
	ExternalConnectorPolicy    string   `json:"external_connector_policy,omitempty"`
	ExternalConnectorAllowlist []string `json:"external_connector_allowlist,omitempty"`
}

type WorktreeLease struct {
	Mode         string    `json:"mode,omitempty"`
	Path         string    `json:"path,omitempty"`
	BaseRepo     string    `json:"base_repo,omitempty"`
	BaseHEAD     string    `json:"base_head,omitempty"`
	Branch       string    `json:"branch,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
	TaskID       string    `json:"task_id,omitempty"`
	AgentID      string    `json:"agent_id,omitempty"`
	Dirty        bool      `json:"dirty,omitempty"`
	ChangedFiles []string  `json:"changed_files,omitempty"`
	ManifestPath string    `json:"manifest_path,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

type CommandCheck struct {
	Name           string `json:"name"`
	Command        string `json:"command"`
	WorkDir        string `json:"work_dir,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	Required       bool   `json:"required,omitempty"`
}

type VerificationPolicy struct {
	Commands              []CommandCheck `json:"commands,omitempty"`
	RequireReview         bool           `json:"require_review,omitempty"`
	RequireManualApproval bool           `json:"require_manual_approval,omitempty"`
}

type RetryPolicy struct {
	MaxRetries int `json:"max_retries,omitempty"`
}

type EscalationPolicy struct {
	EscalateOnFailure      bool     `json:"escalate_on_failure,omitempty"`
	EscalateOnRetryExhaust bool     `json:"escalate_on_retry_exhaust,omitempty"`
	HumanReviewRequired    bool     `json:"human_review_required,omitempty"`
	Notify                 []string `json:"notify,omitempty"`
}

type Artifact struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Kind      string    `json:"kind,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type TestResult struct {
	Name       string    `json:"name"`
	Command    string    `json:"command,omitempty"`
	Passed     bool      `json:"passed"`
	ExitCode   int       `json:"exit_code,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Output     string    `json:"output,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Failure struct {
	Step      Step      `json:"step,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	Message   string    `json:"message"`
	Command   string    `json:"command,omitempty"`
	Artifact  string    `json:"artifact,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Decision struct {
	Step      Step      `json:"step,omitempty"`
	Summary   string    `json:"summary"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ProgressEntry struct {
	Step      Step      `json:"step,omitempty"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type State struct {
	SchemaVersion      string             `json:"schema_version"`
	ID                 string             `json:"id"`
	Goal               string             `json:"goal"`
	Task               string             `json:"task,omitempty"`
	Trigger            Trigger            `json:"trigger,omitempty"`
	Status             Status             `json:"status"`
	CurrentStep        Step               `json:"current_step,omitempty"`
	AssignedAgent      string             `json:"assigned_agent,omitempty"`
	Permissions        Permissions        `json:"permissions,omitempty"`
	Worktree           *WorktreeLease     `json:"worktree,omitempty"`
	VerificationPolicy VerificationPolicy `json:"verification_policy,omitempty"`
	RetryPolicy        RetryPolicy        `json:"retry_policy,omitempty"`
	EscalationPolicy   EscalationPolicy   `json:"escalation_policy,omitempty"`
	FinalArtifact      string             `json:"final_artifact,omitempty"`
	Artifacts          []Artifact         `json:"artifacts,omitempty"`
	Progress           []ProgressEntry    `json:"progress,omitempty"`
	Failures           []Failure          `json:"failures,omitempty"`
	Decisions          []Decision         `json:"decisions,omitempty"`
	CompletedSteps     []Step             `json:"completed_steps,omitempty"`
	CurrentBlocker     string             `json:"current_blocker,omitempty"`
	ModifiedFiles      []string           `json:"modified_files,omitempty"`
	TestResults        []TestResult       `json:"test_results,omitempty"`
	NextSteps          []string           `json:"next_steps,omitempty"`
	NeedsHuman         bool               `json:"needs_human,omitempty"`
	RetryCount         int                `json:"retry_count,omitempty"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
}

type Event struct {
	Type      string    `json:"type"`
	LoopID    string    `json:"loop_id,omitempty"`
	Step      Step      `json:"step,omitempty"`
	Agent     string    `json:"agent,omitempty"`
	Message   string    `json:"message,omitempty"`
	Artifact  string    `json:"artifact,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Data      any       `json:"data,omitempty"`
}

type Spec struct {
	ID                 string
	Goal               string
	Task               string
	Trigger            Trigger
	AssignedAgent      string
	Permissions        Permissions
	Worktree           *WorktreeLease
	VerificationPolicy VerificationPolicy
	RetryPolicy        RetryPolicy
	EscalationPolicy   EscalationPolicy
}
