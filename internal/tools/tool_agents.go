package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/config"
	prompttext "github.com/blueberrycongee/wuu/internal/prompt"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

// ---------------------------------------------------------------------------
// spawn_agent
// ---------------------------------------------------------------------------

type SpawnAgentTool struct{ env *Env }

func NewSpawnAgentTool(env *Env) *SpawnAgentTool { return &SpawnAgentTool{env: env} }

func (t *SpawnAgentTool) Name() string            { return "spawn_agent" }
func (t *SpawnAgentTool) IsReadOnly() bool        { return false }
func (t *SpawnAgentTool) IsConcurrencySafe() bool { return true }

func (t *SpawnAgentTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "spawn_agent",
		Description: "Launch a new agent to handle a complex, multi-step task autonomously. " +
			"Available subagent_type values are general-purpose for broad code research, search, and implementation, " +
			"and verification for independent post-change verification with a PASS/FAIL/PARTIAL verdict. " +
			"Specify subagent_type to launch a fresh specialized agent. Omit subagent_type to fork yourself: " +
			"the child inherits your full conversation context and runs in the background. For a fresh " +
			"general-purpose agent, set subagent_type='general-purpose'. The child has its own context " +
			"and its final answer is delivered to you when it finishes. " +
			"Use a child only when delegation materially improves the task: independent investigation, " +
			"parallel implementation slices, risky verification, or work that benefits from a separate context. " +
			"Ordinary child agents are temporary and do not use saved profile memory. Set agent_profile only when the user asks to use a named " +
			"Agent Profile with saved memory, or when a workflow/profile policy requires one; the profile name selects the saved memory to reuse. " +
			"Do not set agent_profile for routine one-off delegation. " +
			"Keep work local when the next step is tightly coupled, on the critical path, or simpler to do directly. " +
			"Write a concrete brief using the shared Base Agent Brief Contract. " +
			prompttext.AgentBriefContractSummary() + " " +
			prompttext.ProfileBriefExtensionSummary() + " " +
			prompttext.EphemeralBriefExtensionSummary() + " " +
			"Do not make the child infer missing acceptance criteria from a vague ask. " +
			"For code-edit work, state the files or modules the child owns and any nearby files or modules it must avoid; split parallel edit tasks so these ownership boundaries do not overlap. " +
			"By default the agent runs in the user's current repo, so any files it creates or edits " +
			"land directly in the working tree. Set isolation='worktree' only " +
			"for destructive or broad experiments, overlapping or uncertain concurrent writes, " +
			"generated outputs/formatters that may touch many files, or when the user explicitly " +
			"asked for a sandbox. Do not use a worktree just because the task involves writing " +
			"files; small additive or clearly disjoint edits can share the current repo when shared visibility helps. " +
			"Always include a short description (3-5 words) summarizing what the agent will do. " +
			"Each fresh subagent_type invocation starts without conversation context, so prompt must be complete. " +
			"Forks inherit your current context; do not use a fork when a fresh independent second opinion is needed. " +
			"Use run_in_background=true when you have genuinely independent work to do in parallel. Otherwise keep the " +
			"agent in the foreground so its result can inform your next step. Verification agents always run in the background. " +
			"After spawning background agents, continue meaningful non-overlapping local work when available; otherwise end your turn " +
			"and let the background completion notification resume you. Do not sleep, poll, or loop checking status. Use await_agents only when " +
			"synthesis or integration depends on child output. Spawn multiple independent agents in parallel by calling spawn_agent " +
			"multiple times in the same response.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description": map[string]any{
					"type":        "string",
					"description": "Short 3-5 word summary of what the agent will do.",
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "Concrete task brief. " + prompttext.AgentBriefContractSummary() + " For code edits, include owned files/modules and out-of-scope neighbors. Fresh subagents start without conversation context; forks inherit your current context but still need a specific directive.",
				},
				"subagent_type": map[string]any{
					"type":        "string",
					"description": "Optional specialized agent type. Use 'general-purpose' for fresh broad research/search/implementation or 'verification' for an independent verifier. Omit to fork yourself with full conversation context.",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Optional addressable task name. Must use only lowercase letters, digits, and underscores. If omitted, wuu derives a unique name from description.",
				},
				"agent_profile": map[string]any{
					"type":        "string",
					"description": "Optional Agent Profile name with saved memory. Use only when the user explicitly wants that profile or a workflow/profile policy requires one; omit for ordinary temporary child tasks.",
				},
				"isolation": map[string]any{
					"type":        "string",
					"enum":        []string{"worktree"},
					"description": "Optional. 'worktree' creates a fresh git worktree for sandboxed edits. Omit to run in the current repo.",
				},
				"run_in_background": map[string]any{
					"type":        "boolean",
					"description": "Set to true to run this agent in the background. You will be notified when it completes. Omit or false to keep a fresh specialized agent in the foreground; forks and verification agents run in the background.",
				},
			},
			"required": []string{"description", "prompt"},
		},
	}
}

func (t *SpawnAgentTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t.env.AgentControl == nil {
		return "", errors.New("spawn_agent: agent control not configured (this build does not support sub-agents)")
	}
	var args struct {
		Description     string `json:"description"`
		Prompt          string `json:"prompt"`
		SubagentType    string `json:"subagent_type"`
		Name            string `json:"name"`
		AgentProfile    string `json:"agent_profile"`
		Isolation       string `json:"isolation"`
		RunInBackground bool   `json:"run_in_background"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	description := strings.TrimSpace(args.Description)
	if description == "" {
		return "", errors.New("spawn_agent: description is required")
	}
	prompt := strings.TrimSpace(args.Prompt)
	if prompt == "" {
		return "", errors.New("spawn_agent: prompt is required")
	}
	taskName := strings.TrimSpace(args.Name)
	if taskName == "" {
		taskName = deriveAgentTaskName(description)
	}
	if err := agentthread.ValidateAgentName(taskName); err != nil {
		return "", fmt.Errorf("spawn_agent: invalid name: %w", err)
	}
	isolation := strings.TrimSpace(args.Isolation)
	if isolation != "" && !strings.EqualFold(isolation, string(agentcontrol.IsolationWorktree)) {
		return "", errors.New("spawn_agent: isolation must be omitted or 'worktree'")
	}
	agentProfile := strings.TrimSpace(args.AgentProfile)
	if strings.EqualFold(agentProfile, config.DefaultAgentName) {
		return "", errors.New("spawn_agent: agent_profile \"default\" is reserved for ordinary temporary sessions; omit agent_profile or choose a named profile")
	}
	subagentType := strings.TrimSpace(args.SubagentType)
	if subagentType == "" {
		parentHistory := agent.HistoryFromContext(ctx)
		if len(parentHistory) == 0 {
			return "", errors.New("spawn_agent: fork requires parent history; set subagent_type='general-purpose' for a fresh agent")
		}
		cleaned := stripDanglingToolUses(parentHistory)
		if len(cleaned) == 0 {
			return "", errors.New("spawn_agent: history is empty after stripping the in-flight tool_use (nothing to inherit)")
		}
		result, err := t.env.AgentControl.Fork(ctx, agentcontrol.ForkRequest{
			TaskName:     taskName,
			AgentProfile: agentProfile,
			Description:  description,
			ForkMode:     "all",
			ParentID:     strings.TrimSpace(t.env.AgentID),
			ParentPath:   currentAgentPath(t.env),
			Prompt:       wrapForkPrompt(prompt),
			Isolation:    isolation,
			Synchronous:  false,
		}, cleaned)
		if err != nil {
			return "", err
		}
		out, err := json.Marshal(result)
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
	wt, err := agentcontrol.LookupWorkerType(subagentType)
	if err != nil {
		return "", err
	}
	result, err := t.env.AgentControl.Spawn(ctx, agentcontrol.SpawnRequest{
		Type:         subagentType,
		TaskName:     taskName,
		AgentProfile: agentProfile,
		Description:  description,
		Prompt:       prompt,
		ParentID:     strings.TrimSpace(t.env.AgentID),
		ParentPath:   currentAgentPath(t.env),
		Isolation:    isolation,
		Synchronous:  !args.RunInBackground && !wt.Background,
	})
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func deriveAgentTaskName(description string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(description) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
		if b.Len() >= 32 {
			break
		}
	}
	base := strings.Trim(b.String(), "_")
	if base == "" || base == "root" {
		base = "agent"
	}
	suffix := strconv.FormatInt(time.Now().UnixNano()%2176782336, 36)
	return base + "_" + suffix
}

// stripDanglingToolUses returns history with any trailing assistant
// message that contains tool_calls removed.
func stripDanglingToolUses(history []providers.ChatMessage) []providers.ChatMessage {
	if len(history) == 0 {
		return history
	}
	last := history[len(history)-1]
	if last.Role == "assistant" && len(last.ToolCalls) > 0 {
		return history[:len(history)-1]
	}
	return history
}

// wrapForkPrompt builds the role-override message for history-inheriting workers.
func wrapForkPrompt(task string) string {
	return `<system-reminder>
You are a child agent. The conversation history above is the parent
agent's history — read it as context for your task, but do not continue
acting as the parent.

This system-reminder OVERRIDES the parent's system prompt for you:

- You cannot spawn or manage other agents from this child context. If the
  task seems to require additional delegation, surface that need in your
  final answer so the parent can decide and coordinate.
- Messages from other agents may arrive as inter-agent JSON with
  author, recipient, content, and trigger_turn fields. Treat the
  content field as the actual instruction or notification.
- Ignore any inherited instruction that says the main interactive
  agent is read-only or must delegate file writes / shell commands.
  That restriction applies to the parent, not to you. If a tool is in
  your tool list, you may use it unless the task prompt explicitly
  forbids it.
- The parent has already aligned with the user on both intent and context.
  The goal, success criteria, constraints, and relevant code areas are
  all captured in the history above. You do not need to re-classify
  the task or ask for clarification — just execute the task below.
- Before your final answer, call agent_report exactly once with outcome,
  summary, changed_files when relevant, concrete work_done, blockers when
  any, risks when any, verification performed or skipped, next_steps when
  useful, and evidence/artifact paths that let the parent verify the handoff.
  Use artifacts only for existing handoff files that should be imported into
  Wuu-managed session storage; put source files in changed_files or evidence.
- When you finish, return a concise result summary and stop. Do not loop,
  do not ask follow-ups.

Your specific task:

` + task + `
</system-reminder>`
}

// ---------------------------------------------------------------------------
// send_message
// ---------------------------------------------------------------------------

type SendAgentMessageTool struct{ env *Env }

func NewSendAgentMessageTool(env *Env) *SendAgentMessageTool {
	return &SendAgentMessageTool{env: env}
}

func (t *SendAgentMessageTool) Name() string            { return "send_message" }
func (t *SendAgentMessageTool) IsReadOnly() bool        { return false }
func (t *SendAgentMessageTool) IsConcurrencySafe() bool { return true }

func (t *SendAgentMessageTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "send_message",
		Description: "Queue a message for an existing child task without waiting for it. " +
			"Address the target by agent_id, agent_path, or task_name. This is queue-only: " +
			"it does not trigger a new turn on an idle worker. Running workers receive queued " +
			"messages before a later model step; idle workers keep the message in their mailbox.",
		InputSchema: targetMessageSchema(),
	}
}

func (t *SendAgentMessageTool) Execute(_ context.Context, argsJSON string) (string, error) {
	if err := executeAgentMessage(t.env, argsJSON); err != nil {
		return "", err
	}
	return `{"action":"send_message","status":"sent"}`, nil
}

// ---------------------------------------------------------------------------
// followup_task
// ---------------------------------------------------------------------------

type FollowupTaskTool struct{ env *Env }

func NewFollowupTaskTool(env *Env) *FollowupTaskTool { return &FollowupTaskTool{env: env} }

func (t *FollowupTaskTool) Name() string            { return "followup_task" }
func (t *FollowupTaskTool) IsReadOnly() bool        { return false }
func (t *FollowupTaskTool) IsConcurrencySafe() bool { return true }

func (t *FollowupTaskTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "followup_task",
		Description: "Send a follow-up task message to an existing non-root child task and " +
			"trigger that target to continue. Address the target by agent_id, agent_path, or " +
			"task_name. If the target is mid-turn, the message is queued and starts the " +
			"target's next turn after the current turn completes. If the target is idle, it " +
			"starts a new turn from its saved history.",
		InputSchema: targetMessageSchema(),
	}
}

func (t *FollowupTaskTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	snap, err := executeFollowupTask(ctx, t.env, argsJSON)
	if err != nil {
		return "", err
	}
	resp := snapshotForJSON(snap)
	resp.Action = "followup_task"
	out, err := json.Marshal(resp)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ---------------------------------------------------------------------------
// wait_agent
// ---------------------------------------------------------------------------

type WaitAgentTool struct{ env *Env }

func NewWaitAgentTool(env *Env) *WaitAgentTool { return &WaitAgentTool{env: env} }

func (t *WaitAgentTool) Name() string            { return "wait_agent" }
func (t *WaitAgentTool) IsReadOnly() bool        { return true }
func (t *WaitAgentTool) IsConcurrencySafe() bool { return true }

func (t *WaitAgentTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "wait_agent",
		Description: "Wait briefly for a background agent notification only when the current turn is blocked on that signal. " +
			"This is not a join or result tool: it does not return child output. Do not call wait_agent after spawn_agent just to poll. " +
			"Use await_agents when you need child output for synthesis; otherwise continue local work or end the turn and let automatic completion notifications resume you.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"timeout_ms": map[string]any{
					"type":        "integer",
					"description": "Optional timeout in milliseconds. Defaults to 10000.",
				},
			},
		},
	}
}

func (t *WaitAgentTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t.env.AgentControl == nil {
		return "", errors.New("wait_agent: agent control not configured")
	}
	var args struct {
		TimeoutMS int `json:"timeout_ms"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	timeout := time.Duration(args.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	signal, err := t.env.AgentControl.WaitForAgentNotificationFrom(currentAgentPath(t.env), waitCtx)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(waitAgentResultFromSignal(signal))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type waitAgentResult struct {
	Action     string `json:"action"`
	Message    string `json:"message"`
	TimedOut   bool   `json:"timed_out"`
	SignalType string `json:"signal_type"`
	AgentID    string `json:"agent_id,omitempty"`
	AgentPath  string `json:"agent_path,omitempty"`
	TaskName   string `json:"task_name,omitempty"`
	Status     string `json:"status,omitempty"`
}

func waitAgentResultFromSignal(signal agentcontrol.WaitAgentSignal) waitAgentResult {
	result := waitAgentResult{
		Action:     "wait_agent",
		TimedOut:   !signal.Received,
		SignalType: publicWaitAgentSignalType(signal.SignalType),
		AgentID:    signal.AgentID,
		AgentPath:  signal.AgentPath,
		TaskName:   signal.TaskName,
		Status:     signal.Status,
	}
	if result.SignalType == "" {
		result.SignalType = agentcontrol.WaitAgentSignalTimeout
	}
	result.Message = waitAgentMessage(result)
	return result
}

func publicWaitAgentSignalType(signalType string) string {
	if signalType == agentcontrol.WaitAgentSignalQueuedMessage {
		return "notification_received"
	}
	return signalType
}

func waitAgentMessage(result waitAgentResult) string {
	switch result.SignalType {
	case agentcontrol.WaitAgentSignalCompleted:
		return "Background agent completed."
	case agentcontrol.WaitAgentSignalFailed:
		return "Background agent failed."
	case agentcontrol.WaitAgentSignalCancelled:
		return "Background agent was cancelled."
	case agentcontrol.WaitAgentSignalTimeout:
		return "Wait timed out; no background agent notification arrived before the timeout."
	default:
		return "Background agent notification received."
	}
}

// ---------------------------------------------------------------------------
// await_agents
// ---------------------------------------------------------------------------

type AwaitAgentsTool struct{ env *Env }

func NewAwaitAgentsTool(env *Env) *AwaitAgentsTool { return &AwaitAgentsTool{env: env} }

func (t *AwaitAgentsTool) Name() string            { return "await_agents" }
func (t *AwaitAgentsTool) IsReadOnly() bool        { return true }
func (t *AwaitAgentsTool) IsConcurrencySafe() bool { return true }

func (t *AwaitAgentsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "await_agents",
		Description: "Explicitly join one or more child agents and return their structured results. " +
			"Use this only when the current request depends on the child output, or when synthesis / " +
			"integration of previously spawned work is the next step. Do not call it in the same " +
			"parallel tool-call batch as spawn_agent; wait for spawn_agent to return real IDs first. " +
			"Pass targets to wait for specific agent_ids, task_names, or agent_paths. Omit targets " +
			"only when you intentionally want to await all active descendant agents under the current " +
			"agent path. Results can include status='awaiting_report' when a worker produced final " +
			"text without the required agent_report; treat that as an incomplete handoff and follow up " +
			"or verify before relying on it. Results also include changed_files from structured reports " +
			"and warnings when multiple awaited agents report overlapping changed files.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"targets": map[string]any{
					"type":        "array",
					"description": "Optional list of agent_id, task_name, or agent_path values to await. Omit only to await all active descendant agents.",
					"items":       map[string]any{"type": "string"},
				},
				"timeout_ms": map[string]any{
					"type":        "integer",
					"description": "Optional timeout in milliseconds. Defaults to 600000 (10 minutes). On timeout, returns current per-agent statuses with timed_out=true.",
				},
			},
		},
	}
}

func (t *AwaitAgentsTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t.env.AgentControl == nil {
		return "", errors.New("await_agents: agent control not configured")
	}
	var args struct {
		Targets   []string `json:"targets"`
		TimeoutMS int      `json:"timeout_ms"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	timeout := time.Duration(args.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := t.env.AgentControl.AwaitFrom(currentAgentPath(t.env), waitCtx, args.Targets)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ---------------------------------------------------------------------------
// close_agent
// ---------------------------------------------------------------------------

type CloseAgentTool struct{ env *Env }

func NewCloseAgentTool(env *Env) *CloseAgentTool { return &CloseAgentTool{env: env} }

func (t *CloseAgentTool) Name() string            { return "close_agent" }
func (t *CloseAgentTool) IsReadOnly() bool        { return false }
func (t *CloseAgentTool) IsConcurrencySafe() bool { return true }

func (t *CloseAgentTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "close_agent",
		Description: "Close or cancel an existing child task. Address the target by agent_id, agent_path, or task_name.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target": map[string]any{
					"type":        "string",
					"description": "agent_id, agent_path, or task_name.",
				},
			},
			"required": []string{"target"},
		},
	}
}

func (t *CloseAgentTool) Execute(_ context.Context, argsJSON string) (string, error) {
	if t.env.AgentControl == nil {
		return "", errors.New("close_agent: agent control not configured")
	}
	var args struct {
		Target string `json:"target"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if !t.env.AgentControl.StopFrom(currentAgentPath(t.env), args.Target) {
		return "", fmt.Errorf("agent %q not found", args.Target)
	}
	return `{"action":"close_agent","status":"closed"}`, nil
}

// ---------------------------------------------------------------------------
// list_agents
// ---------------------------------------------------------------------------

type ListAgentsTool struct{ env *Env }

func NewListAgentsTool(env *Env) *ListAgentsTool { return &ListAgentsTool{env: env} }

func (t *ListAgentsTool) Name() string            { return "list_agents" }
func (t *ListAgentsTool) IsReadOnly() bool        { return true }
func (t *ListAgentsTool) IsConcurrencySafe() bool { return true }

func (t *ListAgentsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "list_agents",
		Description: "List all sub-agents in the current session with their status (running, " +
			"completed, failed, cancelled), type, description, and timing info.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path_prefix": map[string]any{
					"type":        "string",
					"description": "Optional relative or absolute agent path prefix to list.",
				},
			},
		},
	}
}

func (t *ListAgentsTool) Execute(_ context.Context, argsJSON string) (string, error) {
	if t.env.AgentControl == nil {
		return "", errors.New("list_agents: agent control not configured")
	}
	var args struct {
		PathPrefix string `json:"path_prefix"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	list := t.env.AgentControl.ListFrom(currentAgentPath(t.env), args.PathPrefix)
	agents := make([]agentSnapshotResponse, 0, len(list))
	for _, snap := range list {
		agents = append(agents, snapshotForJSON(snap))
	}
	out, err := json.Marshal(map[string]any{
		"action": "list_agents",
		"agents": agents,
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ---------------------------------------------------------------------------
// agent_report
// ---------------------------------------------------------------------------

type AgentReportTool struct{ env *Env }

func NewAgentReportTool(env *Env) *AgentReportTool { return &AgentReportTool{env: env} }

func (t *AgentReportTool) Name() string            { return "agent_report" }
func (t *AgentReportTool) IsReadOnly() bool        { return false }
func (t *AgentReportTool) IsConcurrencySafe() bool { return false }

func (t *AgentReportTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "agent_report",
		Description: "Submit a structured handoff report for the current child agent. " +
			"Use this before your final answer so parent and later agents receive a durable " +
			"summary with constraints, work done, blockers, evidence references, and artifact paths. " +
			"Do not use this for casual messages; use send_message for interim communication.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"outcome": map[string]any{
					"type":        "string",
					"enum":        []string{"completed", "stuck", "error", "cancelled"},
					"description": "Final task outcome.",
				},
				"summary": map[string]any{
					"type":        "string",
					"description": "Short handoff summary. Include the user's intent you preserved and the important result.",
				},
				"changed_files": map[string]any{
					"type":        "array",
					"description": "Files changed or inspected as primary evidence, using repo-relative paths when possible.",
					"items":       map[string]any{"type": "string"},
				},
				"work_done": map[string]any{
					"type":        "array",
					"description": "Concrete work completed.",
					"items":       map[string]any{"type": "string"},
				},
				"blockers": map[string]any{
					"type":        "array",
					"description": "Problems that prevented completion, with exact errors when relevant.",
					"items":       map[string]any{"type": "string"},
				},
				"risks": map[string]any{
					"type":        "array",
					"description": "Known risks, uncertain assumptions, or conflict areas the parent should consider.",
					"items":       map[string]any{"type": "string"},
				},
				"verification": map[string]any{
					"type":        "array",
					"description": "Verification performed or intentionally not performed, with command names or reasons.",
					"items":       map[string]any{"type": "string"},
				},
				"next_steps": map[string]any{
					"type":        "array",
					"description": "Specific next actions for the parent or a later agent.",
					"items":       map[string]any{"type": "string"},
				},
				"evidence": map[string]any{
					"type":        "array",
					"description": "Pointers to raw evidence that let later agents verify the report instead of trusting prose.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"type":    map[string]any{"type": "string", "description": "file, command, diff, log, screenshot, or note."},
							"path":    map[string]any{"type": "string", "description": "File or artifact path."},
							"line":    map[string]any{"type": "integer", "description": "Optional 1-based line number."},
							"command": map[string]any{"type": "string", "description": "Command that produced the evidence."},
							"output":  map[string]any{"type": "string", "description": "Important output excerpt."},
							"note":    map[string]any{"type": "string", "description": "Why this evidence matters."},
						},
					},
				},
				"artifacts": map[string]any{
					"type":        "array",
					"description": "Existing handoff artifact files to import into Wuu-managed session storage. Use this for reports, logs, screenshots, or test output that already exists. Relative paths resolve inside the task workspace; $SESSION_DIR/... refs are allowed. Source files belong in changed_files or evidence instead.",
					"items":       map[string]any{"type": "string"},
				},
			},
			"required": []string{"outcome", "summary"},
		},
	}
}

func (t *AgentReportTool) Execute(_ context.Context, argsJSON string) (string, error) {
	if t.env.AgentControl == nil {
		return "", errors.New("agent_report: agent control not configured")
	}
	if strings.TrimSpace(t.env.AgentID) == "" || currentAgentPath(t.env) == agentthread.RootPath {
		return "", errors.New("agent_report is only available to child agents")
	}
	var req agentcontrol.AgentReportRequest
	if err := decodeArgs(argsJSON, &req); err != nil {
		return "", err
	}
	result, err := t.env.AgentControl.RecordAgentReport(t.env.AgentID, currentAgentPath(t.env), req)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func targetMessageSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": "agent_id, agent_path, or task_name.",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Message to deliver.",
			},
		},
		"required": []string{"target", "message"},
	}
}

func executeAgentMessage(env *Env, argsJSON string) error {
	if env.AgentControl == nil {
		return errors.New("send_message: agent control not configured")
	}
	var args struct {
		Target  string `json:"target"`
		Message string `json:"message"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	if strings.TrimSpace(args.Target) == "" {
		return errors.New("send_message: target is required")
	}
	return env.AgentControl.SendMessageFrom(currentAgentPath(env), args.Target, args.Message)
}

func executeFollowupTask(ctx context.Context, env *Env, argsJSON string) (subagent.SubAgentSnapshot, error) {
	if env.AgentControl == nil {
		return subagent.SubAgentSnapshot{}, errors.New("followup_task: agent control not configured")
	}
	var args struct {
		Target  string `json:"target"`
		Message string `json:"message"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return subagent.SubAgentSnapshot{}, err
	}
	if strings.TrimSpace(args.Target) == "" {
		return subagent.SubAgentSnapshot{}, errors.New("followup_task: target is required")
	}
	return env.AgentControl.FollowupTaskFrom(currentAgentPath(env), ctx, args.Target, args.Message)
}

func currentAgentPath(env *Env) string {
	if env == nil || strings.TrimSpace(env.AgentPath) == "" {
		return agentthread.RootPath
	}
	return strings.TrimSpace(env.AgentPath)
}

type agentSnapshotResponse struct {
	Action       string    `json:"action,omitempty"`
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	TaskName     string    `json:"task_name,omitempty"`
	AgentPath    string    `json:"agent_path,omitempty"`
	ParentID     string    `json:"parent_id,omitempty"`
	Description  string    `json:"description,omitempty"`
	Status       string    `json:"status"`
	Result       string    `json:"result,omitempty"`
	Error        string    `json:"error,omitempty"`
	InputTokens  int       `json:"input_tokens,omitempty"`
	OutputTokens int       `json:"output_tokens,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
}

func snapshotForJSON(snap subagent.SubAgentSnapshot) agentSnapshotResponse {
	out := agentSnapshotResponse{
		ID:           snap.ID,
		Type:         snap.Type,
		TaskName:     snap.TaskName,
		AgentPath:    snap.AgentPath,
		ParentID:     snap.ParentID,
		Description:  snap.Description,
		Status:       string(snap.Status),
		Result:       snap.Result,
		InputTokens:  snap.InputTokens,
		OutputTokens: snap.OutputTokens,
		StartedAt:    snap.StartedAt,
		CompletedAt:  snap.CompletedAt,
	}
	if snap.Error != nil {
		out.Error = snap.Error.Error()
	}
	return out
}
