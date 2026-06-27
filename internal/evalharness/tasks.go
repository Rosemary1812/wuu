package evalharness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

// Task is one deterministic local evaluation scenario.
type Task struct {
	ID                   string
	Name                 string
	Description          string
	Prompt               string
	RequiredTools        []string
	ForbiddenTools       []string
	RequiredToolCalls    []ToolCallRequirement
	RequiredToolSequence []ToolCallRequirement
	RequiredErrors       []ToolErrorRequirement
	IsolateWuuHome       bool
	Setup                func(root string) error
	Configure            func(root string, cfg config.Config) config.Config
	Verify               func(ctx context.Context, root, answer string) (Verification, error)
}

type ToolErrorRequirement struct {
	ToolName      string `json:"tool_name"`
	ErrorContains string `json:"error_contains"`
}

type ToolCallRequirement struct {
	ToolName       string            `json:"tool_name"`
	ArgumentEquals map[string]string `json:"argument_equals,omitempty"`
	ArgsContains   []string          `json:"args_contains,omitempty"`
}

type Verification struct {
	Passed   bool                   `json:"passed"`
	Reason   string                 `json:"reason,omitempty"`
	Evidence []VerificationEvidence `json:"evidence,omitempty"`
}

type VerificationEvidence struct {
	Check    string `json:"check"`
	Passed   bool   `json:"passed"`
	Summary  string `json:"summary,omitempty"`
	Path     string `json:"path,omitempty"`
	Command  string `json:"command,omitempty"`
	Expected string `json:"expected,omitempty"`
	Observed string `json:"observed,omitempty"`
}

type Result struct {
	TaskID               string                   `json:"task_id"`
	TaskName             string                   `json:"task_name"`
	Success              bool                     `json:"success"`
	DurationMS           int64                    `json:"duration_ms"`
	Turns                int                      `json:"turns"`
	ToolCalls            int                      `json:"tool_calls"`
	ToolNames            []string                 `json:"tool_names,omitempty"`
	ToolSequence         []string                 `json:"tool_sequence,omitempty"`
	MissingTools         []string                 `json:"missing_tools,omitempty"`
	ForbiddenToolsUsed   []string                 `json:"forbidden_tools,omitempty"`
	MissingToolCalls     []string                 `json:"missing_tool_calls,omitempty"`
	MissingToolSeq       []string                 `json:"missing_tool_sequence,omitempty"`
	MissingErrors        []string                 `json:"missing_errors,omitempty"`
	WorkflowIssues       []string                 `json:"workflow_issues,omitempty"`
	InputTokens          int                      `json:"input_tokens"`
	OutputTokens         int                      `json:"output_tokens"`
	VerificationReason   string                   `json:"verification_reason,omitempty"`
	VerificationEvidence []VerificationEvidence   `json:"verification_evidence,omitempty"`
	Validation           *ValidationReplaySummary `json:"validation,omitempty"`
	Error                string                   `json:"error,omitempty"`
	Workdir              string                   `json:"workdir,omitempty"`
	Observability        *Observability           `json:"observability,omitempty"`
}

type Observability struct {
	SessionID          string                      `json:"session_id,omitempty"`
	StateDir           string                      `json:"state_dir,omitempty"`
	SessionDir         string                      `json:"session_dir,omitempty"`
	TracePath          string                      `json:"trace_path,omitempty"`
	HarnessDir         string                      `json:"harness_dir,omitempty"`
	WorkflowDir        string                      `json:"workflow_dir,omitempty"`
	TaskWorkdir        string                      `json:"task_workdir,omitempty"`
	TaskWorkdirKept    bool                        `json:"task_workdir_kept,omitempty"`
	FinalAnswerPreview string                      `json:"final_answer_preview,omitempty"`
	ModelProfile       *ModelProfileObservation    `json:"model_profile,omitempty"`
	ContextBlocks      []ContextBlockObservation   `json:"context_blocks,omitempty"`
	ContextRequests    []ContextRequestObservation `json:"context_requests,omitempty"`
	ToolInventory      []ToolInventoryObservation  `json:"tool_inventory,omitempty"`
	ToolRecords        []ToolObservation           `json:"tool_records,omitempty"`
	GoalAttention      []GoalAttentionObservation  `json:"goal_attention,omitempty"`
	WorkflowRuns       []WorkflowRunObservation    `json:"workflow_runs,omitempty"`
	HarnessTasks       []HarnessTaskObservation    `json:"harness_tasks,omitempty"`
	HarnessReports     []HarnessReportObservation  `json:"harness_reports,omitempty"`
	Warnings           []string                    `json:"warnings,omitempty"`
}

type ContextBlockObservation struct {
	Kind           string `json:"kind"`
	Title          string `json:"title,omitempty"`
	Source         string `json:"source,omitempty"`
	TokenBudget    int    `json:"token_budget,omitempty"`
	ContentBytes   int    `json:"content_bytes,omitempty"`
	ContentPreview string `json:"content_preview,omitempty"`
}

type ContextRequestObservation struct {
	StepIndex         int      `json:"step_index"`
	TransientMessages int      `json:"transient_messages,omitempty"`
	ContentBytes      int      `json:"content_bytes,omitempty"`
	BlockKinds        []string `json:"block_kinds,omitempty"`
	MessageCount      int      `json:"message_count,omitempty"`
	SystemMessages    int      `json:"system_messages,omitempty"`
	HiddenMessages    int      `json:"hidden_messages,omitempty"`
	ToolCount         int      `json:"tool_count,omitempty"`
	StablePrefix      int      `json:"stable_prefix,omitempty"`
	DynamicBytes      int      `json:"dynamic_context_bytes,omitempty"`
	SystemBytes       int      `json:"system_bytes,omitempty"`
	StablePrefixBytes int      `json:"stable_prefix_bytes,omitempty"`
	MessageBytes      int      `json:"message_bytes,omitempty"`
	ToolSchemaBytes   int      `json:"tool_schema_bytes,omitempty"`
	SystemHash        string   `json:"system_hash,omitempty"`
	StablePrefixHash  string   `json:"stable_prefix_hash,omitempty"`
	ToolSurfaceHash   string   `json:"tool_surface_hash,omitempty"`
	PromptCacheKey    string   `json:"prompt_cache_key,omitempty"`
}

type ModelProfileObservation struct {
	ProviderName              string `json:"provider_name,omitempty"`
	Model                     string `json:"model,omitempty"`
	APIModel                  string `json:"api_model,omitempty"`
	Family                    string `json:"family,omitempty"`
	ToolCalling               string `json:"tool_calling,omitempty"`
	FreeformTool              bool   `json:"freeform_tool,omitempty"`
	ParallelToolCalls         bool   `json:"parallel_tool_calls,omitempty"`
	ContextWindowTokens       int    `json:"context_window_tokens,omitempty"`
	DefaultWriteMode          string `json:"default_write_mode,omitempty"`
	DefaultSearchBudget       int    `json:"default_search_budget,omitempty"`
	DefaultMaxAutonomousSteps int    `json:"default_max_autonomous_steps,omitempty"`
	NeedsReadBeforeWrite      bool   `json:"needs_read_before_write,omitempty"`
	AllowParallelReadOnly     bool   `json:"allow_parallel_read_only,omitempty"`
	AllowDirectShell          bool   `json:"allow_direct_shell,omitempty"`
}

type ModelProfileRecommendation struct {
	ProviderName     string   `json:"provider_name,omitempty"`
	Model            string   `json:"model,omitempty"`
	Field            string   `json:"field"`
	CurrentValue     string   `json:"current_value,omitempty"`
	RecommendedValue string   `json:"recommended_value"`
	Reason           string   `json:"reason"`
	Evidence         []string `json:"evidence,omitempty"`
}

type ToolInventoryObservation struct {
	Name            string `json:"name"`
	Kind            string `json:"kind,omitempty"`
	Exposure        string `json:"exposure,omitempty"`
	Risk            string `json:"risk,omitempty"`
	ReadOnly        bool   `json:"read_only"`
	ConcurrencySafe bool   `json:"concurrency_safe"`
	Destructive     bool   `json:"destructive"`
	Reason          string `json:"reason,omitempty"`
}

type ToolObservation struct {
	Name                 string                `json:"name"`
	CallID               string                `json:"call_id,omitempty"`
	ArgumentsSHA256      string                `json:"arguments_sha256,omitempty"`
	ResultAction         string                `json:"result_action,omitempty"`
	Kind                 string                `json:"kind,omitempty"`
	Exposure             string                `json:"exposure,omitempty"`
	Risk                 string                `json:"risk,omitempty"`
	ClassificationReason string                `json:"classification_reason,omitempty"`
	PolicyAction         string                `json:"policy_action,omitempty"`
	PolicyReason         string                `json:"policy_reason,omitempty"`
	ReadOnly             bool                  `json:"read_only"`
	ConcurrencySafe      bool                  `json:"concurrency_safe"`
	StartedAt            time.Time             `json:"started_at,omitempty"`
	DurationMS           int64                 `json:"duration_ms"`
	RevisionBefore       string                `json:"revision_before,omitempty"`
	RevisionAfter        string                `json:"revision_after,omitempty"`
	Success              bool                  `json:"success"`
	Error                string                `json:"error,omitempty"`
	ErrorKind            string                `json:"error_kind,omitempty"`
	RawOutputBytes       int                   `json:"raw_output_bytes,omitempty"`
	ReturnedOutputBytes  int                   `json:"returned_output_bytes,omitempty"`
	ResultBudgeted       bool                  `json:"result_budgeted,omitempty"`
	ResultRef            string                `json:"result_ref,omitempty"`
	ArtifactRefs         []string              `json:"artifact_refs,omitempty"`
	ApprovalRef          string                `json:"approval_ref,omitempty"`
	PatchRiskSummary     *PatchRiskObservation `json:"patch_risk_summary,omitempty"`
	ResultEnvelope       *toolresult.Envelope  `json:"result_envelope,omitempty"`
}

type PatchRiskObservation struct {
	FileCount      int            `json:"file_count"`
	HunkCount      int            `json:"hunk_count"`
	AddedLines     int            `json:"added_lines"`
	DeletedLines   int            `json:"deleted_lines"`
	Actions        map[string]int `json:"actions,omitempty"`
	MultiFile      bool           `json:"multi_file,omitempty"`
	ContainsDelete bool           `json:"contains_delete,omitempty"`
	ContainsMove   bool           `json:"contains_move,omitempty"`
	RiskLevel      string         `json:"risk_level"`
	ReviewHint     string         `json:"review_hint,omitempty"`
}

type GoalAttentionObservation struct {
	Source  string `json:"source"`
	ID      string `json:"id,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
	Path    string `json:"path,omitempty"`
}

type WorkflowRunObservation struct {
	ID              string                        `json:"id"`
	RunDir          string                        `json:"run_dir,omitempty"`
	EventLogPath    string                        `json:"event_log_path,omitempty"`
	DefinitionName  string                        `json:"definition_name,omitempty"`
	Driver          string                        `json:"driver,omitempty"`
	Entrypoint      string                        `json:"entrypoint,omitempty"`
	Status          string                        `json:"status"`
	Error           string                        `json:"error,omitempty"`
	ScriptPath      string                        `json:"script_path,omitempty"`
	FinalReportPath string                        `json:"final_report_path,omitempty"`
	GoalID          string                        `json:"goal_id,omitempty"`
	GoalDir         string                        `json:"goal_dir,omitempty"`
	WorkflowTeam    *WorkflowTeamObservation      `json:"workflow_team,omitempty"`
	TeamArbitration WorkflowTeamArbitration       `json:"team_arbitration,omitempty"`
	Phases          []WorkflowPhaseObservation    `json:"phases,omitempty"`
	AgentRuns       []WorkflowAgentRunObservation `json:"agent_runs,omitempty"`
	EventCount      int                           `json:"event_count,omitempty"`
}

type WorkflowTeamObservation struct {
	Members   []WorkflowTeamMemberObservation `json:"members,omitempty"`
	CreatedAt time.Time                       `json:"created_at,omitempty"`
	UpdatedAt time.Time                       `json:"updated_at,omitempty"`
}

type WorkflowTeamMemberObservation struct {
	ID             string `json:"id,omitempty"`
	Role           string `json:"role,omitempty"`
	Mode           string `json:"mode,omitempty"`
	AgentProfile   string `json:"agent_profile,omitempty"`
	TaskName       string `json:"task_name,omitempty"`
	PhaseID        string `json:"phase_id,omitempty"`
	CreatedProfile bool   `json:"created_profile,omitempty"`
}

type WorkflowTeamArbitration struct {
	Status              string                                  `json:"status,omitempty"`
	OpenAgentRuns       []string                                `json:"open_agent_runs,omitempty"`
	MissingReports      []string                                `json:"missing_reports,omitempty"`
	FailedAgentRuns     []string                                `json:"failed_agent_runs,omitempty"`
	ChangedFileOverlaps []WorkflowChangedFileOverlapObservation `json:"changed_file_overlaps,omitempty"`
	NextActions         []string                                `json:"next_actions,omitempty"`
}

type WorkflowChangedFileOverlapObservation struct {
	File        string   `json:"file"`
	AgentRunIDs []string `json:"agent_run_ids"`
}

type WorkflowPhaseObservation struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Status      string   `json:"status"`
	Error       string   `json:"error,omitempty"`
	AgentRunIDs []string `json:"agent_run_ids,omitempty"`
}

type WorkflowAgentRunObservation struct {
	ID            string   `json:"id"`
	PhaseID       string   `json:"phase_id,omitempty"`
	AgentID       string   `json:"agent_id,omitempty"`
	AgentPath     string   `json:"agent_path,omitempty"`
	TaskName      string   `json:"task_name,omitempty"`
	AgentProfile  string   `json:"agent_profile,omitempty"`
	Status        string   `json:"status"`
	ReportPath    string   `json:"report_path,omitempty"`
	ReportMissing bool     `json:"report_missing,omitempty"`
	ChangedFiles  []string `json:"changed_files,omitempty"`
	Artifacts     []string `json:"artifacts,omitempty"`
	WorktreePath  string   `json:"worktree_path,omitempty"`
	InputTokens   int      `json:"input_tokens,omitempty"`
	OutputTokens  int      `json:"output_tokens,omitempty"`
	DurationMS    int64    `json:"duration_ms,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type HarnessTaskObservation struct {
	ID            string   `json:"id"`
	ParentID      string   `json:"parent_id,omitempty"`
	Path          string   `json:"path,omitempty"`
	Name          string   `json:"name,omitempty"`
	Role          string   `json:"role,omitempty"`
	GoalID        string   `json:"goal_id,omitempty"`
	GoalDir       string   `json:"goal_dir,omitempty"`
	Status        string   `json:"status"`
	ReportPath    string   `json:"report_path,omitempty"`
	ArtifactPaths []string `json:"artifact_paths,omitempty"`
	InputTokens   int      `json:"input_tokens,omitempty"`
	OutputTokens  int      `json:"output_tokens,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type HarnessReportObservation struct {
	ID           string   `json:"id"`
	TaskID       string   `json:"task_id,omitempty"`
	RunID        string   `json:"run_id,omitempty"`
	AgentID      string   `json:"agent_id,omitempty"`
	AgentPath    string   `json:"agent_path,omitempty"`
	Outcome      string   `json:"outcome"`
	Summary      string   `json:"summary,omitempty"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	Verification []string `json:"verification,omitempty"`
	Artifacts    []string `json:"artifacts,omitempty"`
	ReportPath   string   `json:"report_path,omitempty"`
}

func Catalog() []Task {
	return []Task{
		{
			ID:            "test_failure_fix",
			Name:          "Fix a failing unit test",
			Description:   "Small Go module with one implementation bug and a failing test.",
			Prompt:        "Use bash to run go test ./..., find the implementation bug, fix the source code without changing tests, and use bash again to verify the tests pass.",
			RequiredTools: []string{"bash"},
			Setup:         setupTestFailureFix,
			Verify:        verifyGoTests,
		},
		{
			ID:          "git_test_failure_fix",
			Name:        "Fix a failing unit test in git",
			Description: "Small committed Go module with one implementation bug; eval verifies tests and final git diff.",
			Prompt: "This workspace is a git repo. Use bash to run go test ./... and reproduce the failure, find the implementation bug, " +
				"fix source code without changing tests, use bash again to verify tests pass, then inspect the final git diff with bash before answering. The final diff should only change calc.go.",
			RequiredTools: []string{"bash"},
			Setup:         setupGitTestFailureFix,
			Verify:        verifyGitTestFailureFix,
		},
		{
			ID:            "multi_file_pricing",
			Name:          "Fix behavior across two files",
			Description:   "Go package where the final behavior requires edits in two implementation files.",
			Prompt:        "Fix the pricing package so all tests pass. Use bash for test verification. The bug spans multiple source files. Do not change tests.",
			RequiredTools: []string{"bash"},
			Setup:         setupMultiFilePricing,
			Verify:        verifyGoTests,
		},
		{
			ID:          "patch_review_risk",
			Name:        "Record patch review risk for a multi-file fix",
			Description: "Git-backed Go package where the agent must use one multi-file apply_patch and verify the resulting diff.",
			Prompt: "This workspace is a git repo. Use bash to run go test ./... and reproduce the failing pricing tests, then read the relevant pricing source files. " +
				"Fix the behavior with a single apply_patch call that updates both pricing/subtotal.go and pricing/tax.go. Do not change tests. " +
				"Use bash to verify go test ./... passes, then inspect the final git diff with bash before answering.",
			RequiredTools: []string{"bash", "read_file", "apply_patch"},
			RequiredToolCalls: []ToolCallRequirement{
				{ToolName: "apply_patch", ArgsContains: []string{"pricing/subtotal.go", "pricing/tax.go"}},
			},
			Setup:  setupPatchReviewRisk,
			Verify: verifyPatchReviewRisk,
		},
		{
			ID:          "ast_search_navigation",
			Name:        "Navigate to a symbol with AST search",
			Description: "Go package where the agent must use ast_search to locate a failing symbol before editing.",
			Prompt: "Use ast_search first with query='NormalizeSKU' and kind='function' to locate the failing function before reading any source file. " +
				"Then read the matched source, fix the implementation bug without changing tests, and use bash to verify go test ./... passes.",
			RequiredTools: []string{"ast_search", "read_file", "bash"},
			RequiredToolCalls: []ToolCallRequirement{
				{ToolName: "ast_search", ArgumentEquals: map[string]string{"query": "NormalizeSKU", "kind": "function"}},
			},
			Setup:  setupASTSearchNavigation,
			Verify: verifyGoTests,
		},
		{
			ID:          "semantic_search_navigation",
			Name:        "Navigate to behavior with semantic search",
			Description: "Go package where the agent must use semantic_search to find the behavior area before editing.",
			Prompt: "Use semantic_search first with query='checkout discount total' to find the candidate file before reading any source file. " +
				"Then read the matched source, fix the implementation bug without changing tests, and use bash to verify go test ./... passes.",
			RequiredTools: []string{"semantic_search", "read_file", "bash"},
			RequiredToolCalls: []ToolCallRequirement{
				{ToolName: "semantic_search", ArgumentEquals: map[string]string{"query": "checkout discount total"}},
			},
			Setup:  setupSemanticSearchNavigation,
			Verify: verifyGoTests,
		},
		{
			ID:            "long_process_output",
			Name:          "Read a long-running process log",
			Description:   "Script prints a readiness marker after a delay and keeps running.",
			Prompt:        "Use bash action=start_background to start ./dev.sh, use bash action=read_background until READY_FOR_EVAL appears, write observed.txt containing READY_FOR_EVAL, then use bash action=stop_background to stop the process.",
			RequiredTools: []string{"bash"},
			Setup:         setupLongProcessOutput,
			Verify:        verifyObservedReadyFile,
		},
		{
			ID:            "tool_search_deferred",
			Name:          "Discover and use a deferred tool",
			Description:   "The cron listing tool starts deferred and must be exposed through tool_search.",
			Prompt:        "Find the tool for listing scheduled tasks using tool_search, call that tool, then write tool_search_result.txt containing DEFERRED_TOOL_FOUND.",
			RequiredTools: []string{"tool_search", "list_cron"},
			Setup:         setupEmptyTask,
			Verify:        verifyDeferredToolFoundFile,
		},
		{
			ID:          "stale_read_guard",
			Name:        "Recover from a stale file read",
			Description: "Simulates an external file change after read_file; eval requires edit_file to reject the stale read before recovery.",
			Prompt: "Read target.txt, then run ./mutate_after_read.sh to simulate an external edit. Next, try to use edit_file to change " +
				"\"version: external\" to \"version: final\". If edit_file reports that the file changed since last read, read target.txt " +
				"again and retry the edit. Finally write stale_read_result.txt containing STALE_READ_GUARD_DONE.",
			RequiredTools:  []string{"read_file", "bash", "edit_file", "write_file"},
			RequiredErrors: []ToolErrorRequirement{{ToolName: "edit_file", ErrorContains: "changed since last read"}},
			Setup:          setupStaleReadGuard,
			Verify:         verifyStaleReadGuard,
		},
		{
			ID:          "mcp_readonly_concurrency",
			Name:        "Run read-only MCP tools concurrently",
			Description: "Local MCP server exposes readOnlyHint=true slow tools; eval passes only if their calls overlap.",
			Prompt: "Use tool_search to find the MCP eval read-only slow tools. Expose both of them, then call " +
				"mcp_eval_slow_a and mcp_eval_slow_b together in the same assistant response before writing " +
				"mcp_readonly_result.txt containing MCP_READONLY_CONCURRENT.",
			RequiredTools: []string{"tool_search", "mcp_eval_slow_a", "mcp_eval_slow_b", "write_file"},
			Setup:         setupMCPReadOnlyConcurrency,
			Configure:     configureMCPReadOnlyConcurrency,
			Verify:        verifyMCPReadOnlyConcurrency,
		},
		{
			ID:          "mcp_live_discovery",
			Name:        "Discover and call an MCP tool",
			Description: "Live-friendly MCP task: model discovers a deferred MCP tool, calls it, and writes a marker.",
			Prompt: "Use tool_search to find the MCP eval read-only slow tool A. Call mcp_eval_slow_a, then write " +
				"mcp_live_result.txt containing MCP_LIVE_DISCOVERY_DONE and the tool result text.",
			RequiredTools: []string{"tool_search", "mcp_eval_slow_a", "write_file"},
			Setup:         setupMCPReadOnlyConcurrency,
			Configure:     configureMCPReadOnlyConcurrency,
			Verify:        verifyMCPLiveDiscovery,
		},
		{
			ID:            "multi_agent_worker",
			Name:          "Delegate work to a sub-agent",
			Description:   "Main agent must spawn a background sub-agent and wait for it to produce a marker file.",
			Prompt:        "Spawn a background general-purpose sub-agent with name='eval_worker', description='Write worker marker', subagent_type='general-purpose', and run_in_background=true. Ask it to write worker_result.txt containing SUBAGENT_EVAL_DONE, then call wait_agent until the agent completes. Do not write worker_result.txt yourself.",
			RequiredTools: []string{"spawn_agent", "wait_agent"},
			Setup:         setupEmptyTask,
			Verify:        verifySubAgentWorkerFile,
		},
		{
			ID:          "checkpoint_rollback",
			Name:        "Rollback with a workspace checkpoint",
			Description: "Main agent must create a file checkpoint, make a bad edit, restore it, and leave a marker.",
			Prompt: "Read target.txt first so you have its file_sha. Then call checkpoint with action=create, checkpoint_id='before_bad_edit', " +
				"and paths ['target.txt','scratch.txt']. After the checkpoint exists, use apply_patch to overwrite target.txt with bad content and create " +
				"scratch.txt with temporary content. Then call checkpoint with action=restore and checkpoint_id='before_bad_edit' to restore the checkpoint. " +
				"After restore, use apply_patch to write checkpoint_result.txt containing CHECKPOINT_ROLLBACK_DONE. The final state must keep target.txt at its original content and scratch.txt must be absent.",
			RequiredTools: []string{"read_file", "checkpoint", "apply_patch"},
			RequiredToolCalls: []ToolCallRequirement{
				{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "create", "checkpoint_id": "before_bad_edit"}, ArgsContains: []string{"scratch.txt"}},
				{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "restore", "checkpoint_id": "before_bad_edit"}},
				{ToolName: "apply_patch", ArgsContains: []string{"target.txt"}},
				{ToolName: "apply_patch", ArgsContains: []string{"scratch.txt"}},
				{ToolName: "apply_patch", ArgsContains: []string{"checkpoint_result.txt"}},
			},
			RequiredToolSequence: []ToolCallRequirement{
				{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "create", "checkpoint_id": "before_bad_edit"}, ArgsContains: []string{"scratch.txt"}},
				{ToolName: "apply_patch", ArgsContains: []string{"target.txt", "scratch.txt"}},
				{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "restore", "checkpoint_id": "before_bad_edit"}},
				{ToolName: "apply_patch", ArgsContains: []string{"checkpoint_result.txt"}},
			},
			Setup:  setupCheckpointRollback,
			Verify: verifyCheckpointRollback,
		},
		{
			ID:          "patch_journal_rollback",
			Name:        "Rollback with an apply_patch journal",
			Description: "Main agent must use apply_patch's patch journal artifact to roll back an applied patch.",
			Prompt: "Read target.txt first so you have its file_sha. Then use apply_patch to overwrite target.txt with bad content and create " +
				"scratch.txt with temporary content. From the apply_patch result, take patch_journal_path or manifest_path and call checkpoint with " +
				"action=restore_patch_journal to roll back that applied patch journal. After restore, use apply_patch to write patch_journal_result.txt " +
				"containing PATCH_JOURNAL_ROLLBACK_DONE. The final state must keep target.txt at its original content and scratch.txt must be absent.",
			RequiredTools: []string{"read_file", "apply_patch", "checkpoint"},
			RequiredToolCalls: []ToolCallRequirement{
				{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "restore_patch_journal"}},
				{ToolName: "apply_patch", ArgsContains: []string{"target.txt"}},
				{ToolName: "apply_patch", ArgsContains: []string{"scratch.txt"}},
				{ToolName: "apply_patch", ArgsContains: []string{"patch_journal_result.txt"}},
			},
			RequiredToolSequence: []ToolCallRequirement{
				{ToolName: "apply_patch", ArgsContains: []string{"target.txt", "scratch.txt"}},
				{ToolName: "checkpoint", ArgumentEquals: map[string]string{"action": "restore_patch_journal"}},
				{ToolName: "apply_patch", ArgsContains: []string{"patch_journal_result.txt"}},
			},
			Setup:  setupCheckpointRollback,
			Verify: verifyPatchJournalRollback,
		},
		{
			ID:          "agent_led_workflow_team",
			Name:        "Run an agent-led workflow team",
			Description: "Main agent must record a Workflow Team, create a saved-profile member, spawn workers, await them, and complete the run.",
			Prompt: "Start an agent-managed workflow run with start_workflow using driver='agent_managed', run_id='eval_agent_led_team', and one phase id='team_work'. " +
				"First call list_agent_profiles. Then call start_workflow for the run, not create_workflow directly. Next, record a Workflow Team with workflow_control action=record_workflow_team containing exactly two members in the team field: " +
				"one create_profile member with role='Marker writer', agent_profile='eval_team_marker_writer', task_name='alpha_writer', phase_id='team_work'; " +
				"and one ephemeral member with role='Independent verifier', task_name='beta_writer', phase_id='team_work'. " +
				"Spawn both workers as fresh general-purpose sub-agents with subagent_type='general-purpose' and self-contained prompts from the Base Agent Brief Contract plus workflow run, phase, team member, and result-binding context. " +
				"The create_profile worker must use agent_profile='eval_team_marker_writer' and write team_alpha.txt containing TEAM_ALPHA_DONE. " +
				"The ephemeral worker must omit agent_profile and write team_beta.txt containing TEAM_BETA_DONE. " +
				"Require both workers to call agent_report. Await both with await_agents, bind the results back to the workflow using workflow_control action=record_await_results, then write a final workflow report with complete_run=true. Do not write team_alpha.txt or team_beta.txt yourself.",
			RequiredTools:  []string{"list_agent_profiles", "start_workflow", "workflow_control", "spawn_agent", "await_agents"},
			ForbiddenTools: []string{"create_workflow", "run_workflow"},
			IsolateWuuHome: true,
			Setup:          setupEmptyTask,
			Verify:         verifyDynamicWorkflowTeam,
		},
	}
}

func ByID(id string) (Task, bool) {
	id = strings.TrimSpace(id)
	for _, task := range Catalog() {
		if task.ID == id {
			return task, true
		}
	}
	return Task{}, false
}

func SetupTask(task Task, root string) error {
	if strings.TrimSpace(task.ID) == "" {
		return errors.New("task id is required")
	}
	if task.Setup == nil {
		return fmt.Errorf("task %q has no setup", task.ID)
	}
	return task.Setup(root)
}

func VerifyTask(ctx context.Context, task Task, root, answer string) (Verification, error) {
	if task.Verify == nil {
		return Verification{}, fmt.Errorf("task %q has no verifier", task.ID)
	}
	return task.Verify(ctx, root, answer)
}

func setupTestFailureFix(root string) error {
	files := map[string]string{
		"go.mod": `module evaltask

go 1.22
`,
		"calc.go": `package evaltask

func Add(a, b int) int {
	return a - b
}
`,
		"calc_test.go": `package evaltask

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d, want 5", got)
	}
}
`,
	}
	return writeFiles(root, files)
}

func setupGitTestFailureFix(root string) error {
	if err := setupTestFailureFix(root); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "eval@example.com"},
		{"config", "user.name", "Eval"},
		{"add", "go.mod", "calc.go", "calc_test.go"},
		{"commit", "-m", "initial"},
	} {
		if err := runEvalGit(root, args...); err != nil {
			return err
		}
	}
	return nil
}

func setupMultiFilePricing(root string) error {
	files := map[string]string{
		"go.mod": `module pricingeval

go 1.22
`,
		"pricing/subtotal.go": `package pricing

func Subtotal(cents []int) int {
	return 0
}
`,
		"pricing/tax.go": `package pricing

func TotalWithTax(cents []int, taxBasisPoints int) int {
	return Subtotal(cents)
}
`,
		"pricing/pricing_test.go": `package pricing

import "testing"

func TestSubtotal(t *testing.T) {
	if got := Subtotal([]int{1200, 350, 450}); got != 2000 {
		t.Fatalf("Subtotal() = %d, want 2000", got)
	}
}

func TestTotalWithTax(t *testing.T) {
	if got := TotalWithTax([]int{1000, 1000}, 875); got != 2175 {
		t.Fatalf("TotalWithTax() = %d, want 2175", got)
	}
}
`,
	}
	return writeFiles(root, files)
}

func setupPatchReviewRisk(root string) error {
	if err := setupMultiFilePricing(root); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "eval@example.com"},
		{"config", "user.name", "Eval"},
		{"add", "go.mod", "pricing/subtotal.go", "pricing/tax.go", "pricing/pricing_test.go"},
		{"commit", "-m", "initial"},
	} {
		if err := runEvalGit(root, args...); err != nil {
			return err
		}
	}
	return nil
}

func setupASTSearchNavigation(root string) error {
	files := map[string]string{
		"go.mod": `module inventoryeval

go 1.22
`,
		"inventory/normalize.go": `package inventory

import "strings"

func NormalizeSKU(input string) string {
	return strings.TrimSpace(input)
}

func IsValidSKU(input string) bool {
	return NormalizeSKU(input) != ""
}
`,
		"inventory/normalize_test.go": `package inventory

import "testing"

func TestNormalizeSKU(t *testing.T) {
	if got := NormalizeSKU(" ab-12 "); got != "AB-12" {
		t.Fatalf("NormalizeSKU() = %q, want %q", got, "AB-12")
	}
}

func TestIsValidSKU(t *testing.T) {
	if !IsValidSKU(" ab-12 ") {
		t.Fatal("IsValidSKU() should accept a non-empty normalized SKU")
	}
}
`,
	}
	return writeFiles(root, files)
}

func setupSemanticSearchNavigation(root string) error {
	files := map[string]string{
		"go.mod": `module checkouteval

go 1.22
`,
		"checkout/discount.go": `package checkout

// CartDiscountTotal computes the final checkout total after promo discounts.
func CartDiscountTotal(subtotalCents int, discountCents int) int {
	return subtotalCents
}
`,
		"checkout/discount_test.go": `package checkout

import "testing"

func TestCartDiscountTotalAppliesDiscount(t *testing.T) {
	if got := CartDiscountTotal(1000, 250); got != 750 {
		t.Fatalf("CartDiscountTotal() = %d, want 750", got)
	}
}
`,
		"auth/session.go": `package auth

func ExpireSession() {}
`,
	}
	return writeFiles(root, files)
}

func setupLongProcessOutput(root string) error {
	files := map[string]string{
		"dev.sh": `#!/usr/bin/env bash
sleep 0.2
echo READY_FOR_EVAL
sleep 20
`,
	}
	if err := writeFiles(root, files); err != nil {
		return err
	}
	return os.Chmod(filepath.Join(root, "dev.sh"), 0o755)
}

func setupStaleReadGuard(root string) error {
	files := map[string]string{
		"target.txt": `version: original
`,
		"mutate_after_read.sh": `#!/usr/bin/env bash
printf 'version: external\n' > target.txt
`,
	}
	if err := writeFiles(root, files); err != nil {
		return err
	}
	return os.Chmod(filepath.Join(root, "mutate_after_read.sh"), 0o755)
}

func setupEmptyTask(root string) error {
	return writeFiles(root, map[string]string{".keep": ""})
}

func setupCheckpointRollback(root string) error {
	return writeFiles(root, map[string]string{
		"target.txt": "status: original\nowner: eval\n",
	})
}

func setupMCPReadOnlyConcurrency(root string) error {
	return writeFiles(root, map[string]string{
		"mcp_eval_server.go": mcpEvalServerSource,
		".keep":              "",
	})
}

func configureMCPReadOnlyConcurrency(root string, cfg config.Config) config.Config {
	servers := make(map[string]config.MCPServerConfig, len(cfg.MCPServers)+1)
	for name, server := range cfg.MCPServers {
		servers[name] = server
	}
	servers["eval"] = config.MCPServerConfig{
		Command: "go",
		Args: []string{
			"run",
			filepath.Join(root, "mcp_eval_server.go"),
			filepath.Join(root, "mcp_max_concurrency.txt"),
		},
	}
	cfg.MCPServers = servers
	return cfg
}

const verificationObservedLimit = 1000

func passVerification(reason string, evidence ...VerificationEvidence) Verification {
	return Verification{Passed: true, Reason: reason, Evidence: evidence}
}

func failVerification(reason string, evidence ...VerificationEvidence) Verification {
	return Verification{Passed: false, Reason: reason, Evidence: evidence}
}

func verificationEvidence(check string, passed bool, summary string) VerificationEvidence {
	return VerificationEvidence{Check: check, Passed: passed, Summary: summary}
}

func verificationObserved(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= verificationObservedLimit {
		return value
	}
	return value[:verificationObservedLimit] + "...[truncated]"
}

func verifyGoTests(ctx context.Context, root, _ string) (Verification, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "go", "test", "./...")
	cmd.Dir = root
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	evidence := VerificationEvidence{
		Check:    "go tests",
		Passed:   err == nil && cmdCtx.Err() == nil,
		Command:  "go test ./...",
		Expected: "exit_code=0",
		Observed: verificationObserved(output.String()),
	}
	if cmdCtx.Err() != nil {
		evidence.Passed = false
		evidence.Summary = "go test timed out"
		return failVerification("go test timed out", evidence), nil
	}
	if err != nil {
		evidence.Summary = "go test failed"
		return failVerification(strings.TrimSpace(output.String()), evidence), nil
	}
	evidence.Summary = "go test passed"
	return passVerification(strings.TrimSpace(output.String()), evidence), nil
}

func verifyGitTestFailureFix(ctx context.Context, root, answer string) (Verification, error) {
	testVerification, err := verifyGoTests(ctx, root, answer)
	if err != nil {
		return Verification{}, err
	}
	diffEvidence, err := verifyGitDiffOnly(ctx, root, []string{"calc.go"})
	if err != nil {
		return Verification{}, err
	}
	evidence := append([]VerificationEvidence(nil), testVerification.Evidence...)
	evidence = append(evidence, diffEvidence)
	if !testVerification.Passed {
		return failVerification(testVerification.Reason, evidence...), nil
	}
	if !diffEvidence.Passed {
		return failVerification(diffEvidence.Summary, evidence...), nil
	}
	return passVerification("go tests passed and git diff only changes calc.go", evidence...), nil
}

func verifyPatchReviewRisk(ctx context.Context, root, answer string) (Verification, error) {
	testVerification, err := verifyGoTests(ctx, root, answer)
	if err != nil {
		return Verification{}, err
	}
	wantFiles := []string{"pricing/subtotal.go", "pricing/tax.go"}
	diffEvidence, err := verifyGitDiffOnly(ctx, root, wantFiles)
	if err != nil {
		return Verification{}, err
	}
	evidence := append([]VerificationEvidence(nil), testVerification.Evidence...)
	evidence = append(evidence, diffEvidence)
	if !testVerification.Passed {
		return failVerification(testVerification.Reason, evidence...), nil
	}
	if !diffEvidence.Passed {
		return failVerification(diffEvidence.Summary, evidence...), nil
	}
	return passVerification("go tests passed and git diff only changes pricing source files", evidence...), nil
}

func verifyGitDiffOnly(ctx context.Context, root string, wantFiles []string) (VerificationEvidence, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", "diff", "--name-only")
	cmd.Dir = root
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	evidence := VerificationEvidence{
		Check:    "git diff files",
		Command:  "git diff --name-only",
		Expected: strings.Join(wantFiles, "\n"),
		Observed: verificationObserved(output.String()),
	}
	if cmdCtx.Err() != nil {
		evidence.Passed = false
		evidence.Summary = "git diff timed out"
		return evidence, nil
	}
	if err != nil {
		evidence.Passed = false
		evidence.Summary = "git diff failed"
		return evidence, nil
	}
	files := strings.Fields(output.String())
	if !stringSlicesEqual(files, wantFiles) {
		evidence.Passed = false
		evidence.Summary = "final diff should only change expected files"
		return evidence, nil
	}
	evidence.Passed = true
	evidence.Summary = "final diff only changes expected files"
	return evidence, nil
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func verifyObservedReadyFile(_ context.Context, root, _ string) (Verification, error) {
	data, err := os.ReadFile(filepath.Join(root, "observed.txt"))
	evidence := VerificationEvidence{Check: "observed readiness marker", Path: "observed.txt", Expected: "contains READY_FOR_EVAL"}
	if err != nil {
		evidence.Passed = false
		evidence.Summary = "observed.txt was not written"
		return failVerification("observed.txt was not written", evidence), nil
	}
	evidence.Observed = verificationObserved(string(data))
	if !strings.Contains(string(data), "READY_FOR_EVAL") {
		evidence.Passed = false
		evidence.Summary = "missing readiness marker"
		return failVerification("observed.txt does not contain READY_FOR_EVAL", evidence), nil
	}
	evidence.Passed = true
	evidence.Summary = "observed readiness marker"
	return passVerification("observed readiness marker", evidence), nil
}

func runEvalGit(root string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, string(out))
	}
	return nil
}

func verifyDeferredToolFoundFile(_ context.Context, root, _ string) (Verification, error) {
	data, err := os.ReadFile(filepath.Join(root, "tool_search_result.txt"))
	evidence := VerificationEvidence{Check: "deferred tool marker", Path: "tool_search_result.txt", Expected: "contains DEFERRED_TOOL_FOUND"}
	if err != nil {
		evidence.Passed = false
		evidence.Summary = "tool_search_result.txt was not written"
		return failVerification("tool_search_result.txt was not written", evidence), nil
	}
	evidence.Observed = verificationObserved(string(data))
	if !strings.Contains(string(data), "DEFERRED_TOOL_FOUND") {
		evidence.Passed = false
		evidence.Summary = "missing deferred tool marker"
		return failVerification("tool_search_result.txt does not contain DEFERRED_TOOL_FOUND", evidence), nil
	}
	evidence.Passed = true
	evidence.Summary = "observed deferred tool marker"
	return passVerification("observed deferred tool marker", evidence), nil
}

func verifyStaleReadGuard(_ context.Context, root, _ string) (Verification, error) {
	target, err := os.ReadFile(filepath.Join(root, "target.txt"))
	targetEvidence := VerificationEvidence{Check: "target final version", Path: "target.txt", Expected: "contains version: final"}
	if err != nil {
		targetEvidence.Passed = false
		targetEvidence.Summary = "target.txt was not readable"
		return failVerification("target.txt was not readable", targetEvidence), nil
	}
	targetEvidence.Observed = verificationObserved(string(target))
	if !strings.Contains(string(target), "version: final") {
		targetEvidence.Passed = false
		targetEvidence.Summary = "target.txt did not reach final version"
		return failVerification("target.txt does not contain final version", targetEvidence), nil
	}
	targetEvidence.Passed = true
	targetEvidence.Summary = "target.txt contains final version"
	data, err := os.ReadFile(filepath.Join(root, "stale_read_result.txt"))
	markerEvidence := VerificationEvidence{Check: "stale-read marker", Path: "stale_read_result.txt", Expected: "contains STALE_READ_GUARD_DONE"}
	if err != nil {
		markerEvidence.Passed = false
		markerEvidence.Summary = "stale_read_result.txt was not written"
		return failVerification("stale_read_result.txt was not written", targetEvidence, markerEvidence), nil
	}
	markerEvidence.Observed = verificationObserved(string(data))
	if !strings.Contains(string(data), "STALE_READ_GUARD_DONE") {
		markerEvidence.Passed = false
		markerEvidence.Summary = "missing stale-read marker"
		return failVerification("stale_read_result.txt does not contain STALE_READ_GUARD_DONE", targetEvidence, markerEvidence), nil
	}
	markerEvidence.Passed = true
	markerEvidence.Summary = "observed stale-read marker"
	return passVerification("observed stale-read recovery marker", targetEvidence, markerEvidence), nil
}

func verifyMCPReadOnlyConcurrency(_ context.Context, root, _ string) (Verification, error) {
	data, err := os.ReadFile(filepath.Join(root, "mcp_readonly_result.txt"))
	markerEvidence := VerificationEvidence{Check: "MCP concurrency marker", Path: "mcp_readonly_result.txt", Expected: "contains MCP_READONLY_CONCURRENT"}
	if err != nil {
		markerEvidence.Passed = false
		markerEvidence.Summary = "mcp_readonly_result.txt was not written"
		return failVerification("mcp_readonly_result.txt was not written", markerEvidence), nil
	}
	markerEvidence.Observed = verificationObserved(string(data))
	if !strings.Contains(string(data), "MCP_READONLY_CONCURRENT") {
		markerEvidence.Passed = false
		markerEvidence.Summary = "missing MCP concurrency marker"
		return failVerification("mcp_readonly_result.txt does not contain MCP_READONLY_CONCURRENT", markerEvidence), nil
	}
	markerEvidence.Passed = true
	markerEvidence.Summary = "observed MCP concurrency marker"

	maxData, err := os.ReadFile(filepath.Join(root, "mcp_max_concurrency.txt"))
	concurrencyEvidence := VerificationEvidence{Check: "MCP read-only overlap", Path: "mcp_max_concurrency.txt", Expected: "2"}
	if err != nil {
		concurrencyEvidence.Passed = false
		concurrencyEvidence.Summary = "MCP server did not record concurrency"
		return failVerification("MCP server did not record concurrency", markerEvidence, concurrencyEvidence), nil
	}
	concurrencyEvidence.Observed = verificationObserved(string(maxData))
	if strings.TrimSpace(string(maxData)) != "2" {
		concurrencyEvidence.Passed = false
		concurrencyEvidence.Summary = "MCP read-only tools did not overlap"
		return failVerification("MCP read-only tools did not overlap; max concurrency was "+strings.TrimSpace(string(maxData)), markerEvidence, concurrencyEvidence), nil
	}
	concurrencyEvidence.Passed = true
	concurrencyEvidence.Summary = "observed overlapping read-only MCP tool calls"
	return passVerification("observed overlapping read-only MCP tool calls", markerEvidence, concurrencyEvidence), nil
}

func verifyMCPLiveDiscovery(_ context.Context, root, _ string) (Verification, error) {
	data, err := os.ReadFile(filepath.Join(root, "mcp_live_result.txt"))
	evidence := VerificationEvidence{Check: "live MCP discovery result", Path: "mcp_live_result.txt", Expected: "contains MCP_LIVE_DISCOVERY_DONE and slow_a_OK"}
	if err != nil {
		evidence.Passed = false
		evidence.Summary = "mcp_live_result.txt was not written"
		return failVerification("mcp_live_result.txt was not written", evidence), nil
	}
	text := string(data)
	evidence.Observed = verificationObserved(text)
	if !strings.Contains(text, "MCP_LIVE_DISCOVERY_DONE") {
		evidence.Passed = false
		evidence.Summary = "missing MCP live discovery marker"
		return failVerification("mcp_live_result.txt does not contain MCP_LIVE_DISCOVERY_DONE", evidence), nil
	}
	if !strings.Contains(text, "slow_a_OK") {
		evidence.Passed = false
		evidence.Summary = "missing MCP tool result"
		return failVerification("mcp_live_result.txt does not contain the MCP tool result", evidence), nil
	}
	evidence.Passed = true
	evidence.Summary = "observed live MCP discovery marker and tool result"
	return passVerification("observed live MCP discovery marker", evidence), nil
}

func verifySubAgentWorkerFile(_ context.Context, root, _ string) (Verification, error) {
	data, err := os.ReadFile(filepath.Join(root, "worker_result.txt"))
	evidence := VerificationEvidence{Check: "sub-agent worker marker", Path: "worker_result.txt", Expected: "contains SUBAGENT_EVAL_DONE"}
	if err != nil {
		evidence.Passed = false
		evidence.Summary = "worker_result.txt was not written"
		return failVerification("worker_result.txt was not written", evidence), nil
	}
	evidence.Observed = verificationObserved(string(data))
	if !strings.Contains(string(data), "SUBAGENT_EVAL_DONE") {
		evidence.Passed = false
		evidence.Summary = "missing sub-agent marker"
		return failVerification("worker_result.txt does not contain SUBAGENT_EVAL_DONE", evidence), nil
	}
	evidence.Passed = true
	evidence.Summary = "observed sub-agent worker marker"
	return passVerification("observed sub-agent worker marker", evidence), nil
}

func verifyCheckpointRollback(_ context.Context, root, _ string) (Verification, error) {
	return verifyRollbackState(root, "checkpoint_result.txt", "CHECKPOINT_ROLLBACK_DONE", "checkpoint rollback marker")
}

func verifyPatchJournalRollback(_ context.Context, root, _ string) (Verification, error) {
	return verifyRollbackState(root, "patch_journal_result.txt", "PATCH_JOURNAL_ROLLBACK_DONE", "patch journal rollback marker")
}

func verifyRollbackState(root, markerPath, markerText, markerCheck string) (Verification, error) {
	target, err := os.ReadFile(filepath.Join(root, "target.txt"))
	targetEvidence := VerificationEvidence{Check: "checkpoint restored target", Path: "target.txt", Expected: "status: original\nowner: eval"}
	if err != nil {
		targetEvidence.Passed = false
		targetEvidence.Summary = "target.txt was not readable"
		return failVerification("target.txt was not readable", targetEvidence), nil
	}
	targetEvidence.Observed = verificationObserved(string(target))
	if string(target) != "status: original\nowner: eval\n" {
		targetEvidence.Passed = false
		targetEvidence.Summary = "target.txt was not restored"
		return failVerification("target.txt was not restored to original content", targetEvidence), nil
	}
	targetEvidence.Passed = true
	targetEvidence.Summary = "target.txt restored to original content"
	scratchEvidence := VerificationEvidence{Check: "checkpoint removed scratch", Path: "scratch.txt", Expected: "file absent"}
	if _, err := os.Stat(filepath.Join(root, "scratch.txt")); err == nil {
		scratchEvidence.Passed = false
		scratchEvidence.Observed = "file exists"
		scratchEvidence.Summary = "scratch.txt still exists after restore"
		return failVerification("scratch.txt still exists after restore", targetEvidence, scratchEvidence), nil
	} else if !os.IsNotExist(err) {
		scratchEvidence.Passed = false
		scratchEvidence.Observed = err.Error()
		scratchEvidence.Summary = "scratch.txt could not be checked"
		return failVerification("scratch.txt could not be checked", targetEvidence, scratchEvidence), nil
	}
	scratchEvidence.Passed = true
	scratchEvidence.Observed = "file absent"
	scratchEvidence.Summary = "scratch.txt absent after restore"
	data, err := os.ReadFile(filepath.Join(root, markerPath))
	markerEvidence := VerificationEvidence{Check: markerCheck, Path: markerPath, Expected: "contains " + markerText}
	if err != nil {
		markerEvidence.Passed = false
		markerEvidence.Summary = markerPath + " was not written"
		return failVerification(markerPath+" was not written", targetEvidence, scratchEvidence, markerEvidence), nil
	}
	markerEvidence.Observed = verificationObserved(string(data))
	if !strings.Contains(string(data), markerText) {
		markerEvidence.Passed = false
		markerEvidence.Summary = "missing rollback marker"
		return failVerification(markerPath+" does not contain "+markerText, targetEvidence, scratchEvidence, markerEvidence), nil
	}
	markerEvidence.Passed = true
	markerEvidence.Summary = "observed rollback marker"
	return passVerification("observed "+markerCheck+" and restored files", targetEvidence, scratchEvidence, markerEvidence), nil
}

func verifyDynamicWorkflowTeam(_ context.Context, root, _ string) (Verification, error) {
	required := map[string]string{
		"team_alpha.txt": "TEAM_ALPHA_DONE",
		"team_beta.txt":  "TEAM_BETA_DONE",
	}
	evidence := make([]VerificationEvidence, 0, len(required))
	for name, marker := range required {
		data, err := os.ReadFile(filepath.Join(root, name))
		item := VerificationEvidence{Check: "workflow team marker", Path: name, Expected: "contains " + marker}
		if err != nil {
			item.Passed = false
			item.Summary = name + " was not written"
			evidence = append(evidence, item)
			return failVerification(name+" was not written", evidence...), nil
		}
		item.Observed = verificationObserved(string(data))
		if !strings.Contains(string(data), marker) {
			item.Passed = false
			item.Summary = name + " missing marker"
			evidence = append(evidence, item)
			return failVerification(name+" does not contain "+marker, evidence...), nil
		}
		item.Passed = true
		item.Summary = name + " contains marker"
		evidence = append(evidence, item)
	}
	return passVerification("observed agent-led workflow team markers", evidence...), nil
}

func writeFiles(root string, files map[string]string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("root is required")
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

const mcpEvalServerSource = `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type rpcRequest struct {
	JSONRPC string          ` + "`json:\"jsonrpc\"`" + `
	ID      any             ` + "`json:\"id,omitempty\"`" + `
	Method  string          ` + "`json:\"method\"`" + `
	Params  json.RawMessage ` + "`json:\"params,omitempty\"`" + `
}

type rpcResponse struct {
	JSONRPC string ` + "`json:\"jsonrpc\"`" + `
	ID      any    ` + "`json:\"id,omitempty\"`" + `
	Result  any    ` + "`json:\"result,omitempty\"`" + `
	Error   any    ` + "`json:\"error,omitempty\"`" + `
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "missing max concurrency path")
		os.Exit(2)
	}
	maxPath := os.Args[1]
	_ = os.WriteFile(maxPath, []byte("0\n"), 0o644)

	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	var sendMu sync.Mutex
	send := func(resp rpcResponse) {
		sendMu.Lock()
		defer sendMu.Unlock()
		_ = encoder.Encode(resp)
	}

	var current int64
	var maxSeen int64
	recordMax := func(value int64) {
		for {
			old := atomic.LoadInt64(&maxSeen)
			if value <= old || atomic.CompareAndSwapInt64(&maxSeen, old, value) {
				_ = os.WriteFile(maxPath, []byte(fmt.Sprintf("%d\n", atomic.LoadInt64(&maxSeen))), 0o644)
				return
			}
		}
	}

	for scanner.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			send(rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities": map[string]any{
						"tools": map[string]any{"listChanged": false},
					},
					"serverInfo": map[string]any{"name": "wuu-eval-mcp", "version": "0.1.0"},
				},
			})
		case "notifications/initialized":
			// Notification only.
		case "tools/list":
			send(rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"tools": []map[string]any{
						{
							"name":        "slow_a",
							"description": "MCP eval read-only slow tool A. Use with slow_b to test concurrency.",
							"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
							"annotations": map[string]any{"readOnlyHint": true},
						},
						{
							"name":        "slow_b",
							"description": "MCP eval read-only slow tool B. Use with slow_a to test concurrency.",
							"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
							"annotations": map[string]any{"readOnlyHint": true},
						},
					},
				},
			})
		case "tools/call":
			var params struct {
				Name string ` + "`json:\"name\"`" + `
			}
			_ = json.Unmarshal(req.Params, &params)
			go func(id any, name string) {
				active := atomic.AddInt64(&current, 1)
				recordMax(active)
				time.Sleep(400 * time.Millisecond)
				atomic.AddInt64(&current, -1)
				send(rpcResponse{
					JSONRPC: "2.0",
					ID:      id,
					Result: map[string]any{
						"content": []map[string]string{{"type": "text", "text": name + "_OK"}},
					},
				})
			}(req.ID, params.Name)
		default:
			send(rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]any{
					"code":    -32601,
					"message": "method not found",
				},
			})
		}
	}
}
`
