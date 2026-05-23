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
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/coordinator"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

// ---------------------------------------------------------------------------
// ask_user
// ---------------------------------------------------------------------------

type AskUserTool struct{ env *Env }

func NewAskUserTool(env *Env) *AskUserTool { return &AskUserTool{env: env} }

func (t *AskUserTool) Name() string            { return "ask_user" }
func (t *AskUserTool) IsReadOnly() bool        { return true }
func (t *AskUserTool) IsConcurrencySafe() bool { return false }

func (t *AskUserTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "ask_user",
		Description: "Pause your turn and ask the user a multiple-choice clarifying question. " +
			"Use this BEFORE acting whenever the user's intent is unclear and the answer lives in " +
			"their head (Path A tasks: they have a specific answer you just don't have yet), or " +
			"to offer 2-4 concrete options WITH tradeoffs when the task is genuinely a choice " +
			"(Path B tasks, only AFTER you've done the research that makes the options concrete). " +
			"Send 1-4 questions per call, each with 2-4 options; an \"Other\" escape hatch is " +
			"appended automatically so the user can type a free-text answer if none of your " +
			"options fit. NEVER use this to ask something you could find by reading the code or " +
			"running a command — questions are for things only the user can answer: requirements, " +
			"preferences, tradeoffs, edge-case priorities. If you recommend a specific option, " +
			"put it first in the options list and add \"(recommended)\" at the end of its label.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"questions": map[string]any{
					"type":        "array",
					"minItems":    1,
					"maxItems":    4,
					"description": "Questions to ask the user (1-4 per call, batched into one dialog).",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"question": map[string]any{
								"type":        "string",
								"description": "Full question text the user will read. End with a question mark. Must be unique across questions in this call (used as the answer-map key).",
							},
							"header": map[string]any{
								"type":        "string",
								"description": "Very short chip label (<= 12 chars) shown as a tag on the question nav bar. Examples: \"Auth method\", \"DB driver\", \"Approach\".",
							},
							"options": map[string]any{
								"type":        "array",
								"minItems":    2,
								"maxItems":    4,
								"description": "Available choices (2-4). Each option label must be unique. Do NOT include an \"Other\" option — it is appended automatically.",
								"items": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"label": map[string]any{
											"type":        "string",
											"description": "Short display label (1-5 words). Add \"(recommended)\" to the label if this is your recommendation.",
										},
										"description": map[string]any{
											"type":        "string",
											"description": "Explanation of what this option means or what its tradeoffs are. Shown under the label in the option list.",
										},
										"preview": map[string]any{
											"type":        "string",
											"description": "Optional markdown preview (code snippet, ASCII mockup, diagram) rendered side-by-side with the option list when any option in this question has one. Use it when the user needs to visually compare concrete artifacts, not for simple preference questions.",
										},
									},
									"required": []string{"label", "description"},
								},
							},
							"multi_select": map[string]any{
								"type":        "boolean",
								"description": "Set true when the options are NOT mutually exclusive (user may pick several). Default false.",
							},
						},
						"required": []string{"question", "header", "options"},
					},
				},
			},
			"required": []string{"questions"},
		},
	}
}

func (t *AskUserTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t.env.AskBridge == nil {
		return "", errors.New("ask_user is only available to the main agent in an interactive TUI session (sub-agents cannot interrupt the human)")
	}
	var req AskUserRequest
	if err := decodeArgs(argsJSON, &req); err != nil {
		return "", fmt.Errorf("ask_user: decode arguments: %w", err)
	}
	if err := req.Validate(); err != nil {
		return "", err
	}
	resp, err := t.env.AskBridge.AskUser(ctx, req)
	if err != nil {
		return "", fmt.Errorf("ask_user: %w", err)
	}
	if resp.Cancelled {
		return "", errors.New("ask_user: user dismissed the dialog without answering; reconsider the plan before trying again")
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("ask_user: marshal response: %w", err)
	}
	return string(payload), nil
}

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
		Description: "Spawn a named child task to perform focused work. The sub-agent has its own " +
			"context and its own tools. There is exactly one worker type, 'worker', with the " +
			"full tool set; specialized roles (verification, read-only research) are injected " +
			"by pasting the appropriate preset block at the start of the prompt — see the " +
			"coordinator system prompt for the verbatim preset text. " +
			"By default the spawn runs INPLACE in the user's repo, so any files the worker " +
			"creates or edits land directly in the working tree. Set isolation='worktree' ONLY " +
			"when the work might break the build, when concurrent writers would collide, or " +
			"when the user explicitly asked for a sandbox. Do NOT use a worktree just because " +
			"the task involves writing files — additive writes are not a reason for isolation. " +
			"By default the child receives a fork of your full conversation history. Set " +
			"fork_turns='none' only when the task is fully self-contained and should start " +
			"from a clean slate; set a positive integer string to fork only the last N user turns. " +
			"Give every task a stable task_name so you can address it later by task name, " +
			"agent path, or agent_id. task_name is the child path segment and must use " +
			"only lowercase letters, digits, and underscores, for example inspect_auth_flow. " +
			"By default the spawn is asynchronous: this returns " +
			"immediately with an agent_id and agent_path, and the worker's result will be delivered to you " +
			"automatically as a structured mailbox message once it completes — you will be " +
			"notified without any action on your part. After spawning async workers, END " +
			"YOUR TURN — do NOT generate waiting messages or loop checking status. The system handles notification and auto-resume. " +
			"Set synchronous=true to block until " +
			"the worker finishes. Spawn multiple workers in parallel by calling spawn_agent " +
			"multiple times in the same response — they run concurrently.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_name": map[string]any{
					"type":        "string",
					"description": "Stable child task name used for path-based addressing. Must use only lowercase letters, digits, and underscores. Example: \"inspect_auth_flow\".",
				},
				"agent_type": map[string]any{
					"type":        "string",
					"description": "Worker type. Only 'worker' is supported; omit to use the default.",
				},
				"message": map[string]any{
					"type":        "string",
					"description": "Self-contained task message. The worker cannot see your conversation, so include all needed context: file paths, line numbers, requirements, acceptance criteria.",
				},
				"isolation": map[string]any{
					"type":        "string",
					"enum":        []string{"inplace", "worktree"},
					"description": "Optional. 'inplace' (default) shares the user's repo so writes land in the working tree. 'worktree' creates a fresh git worktree for sandboxed edits — only use this when the work might break the build, when concurrent writers would collide, or when the user explicitly asked for a sandbox.",
				},
				"base_repo": map[string]any{
					"type":        "string",
					"description": "Optional: path to another worker's worktree to chain off. Only valid with isolation=worktree.",
				},
				"synchronous": map[string]any{
					"type":        "boolean",
					"description": "If true, block until the worker completes and return its result inline. If false (default), return immediately and receive the result later via a structured mailbox message.",
				},
				"fork_turns": map[string]any{
					"type":        "string",
					"description": "Context inheritance mode: 'all' (default), 'none', or a positive integer string for the last N user turns.",
				},
			},
			"required": []string{"task_name", "message"},
		},
	}
}

func (t *SpawnAgentTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t.env.Coordinator == nil {
		return "", errors.New("spawn_agent: coordinator not configured (this build does not support sub-agents)")
	}
	var args struct {
		TaskName    string `json:"task_name"`
		AgentType   string `json:"agent_type"`
		Message     string `json:"message"`
		Isolation   string `json:"isolation"`
		BaseRepo    string `json:"base_repo"`
		Synchronous bool   `json:"synchronous"`
		ForkTurns   string `json:"fork_turns"`
		ForkContext *bool  `json:"fork_context"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.TaskName) == "" {
		return "", errors.New("spawn_agent: task_name is required")
	}
	if err := agentthread.ValidateAgentName(strings.TrimSpace(args.TaskName)); err != nil {
		return "", fmt.Errorf("spawn_agent: invalid task_name: %w", err)
	}
	if strings.TrimSpace(args.Message) == "" {
		return "", errors.New("spawn_agent: message is required")
	}
	forkMode, lastNTurns, err := parseSpawnForkTurns(args.ForkTurns, args.ForkContext)
	if err != nil {
		return "", err
	}
	if forkMode != spawnForkNone {
		if strings.TrimSpace(args.Isolation) != "" || strings.TrimSpace(args.BaseRepo) != "" {
			return "", errors.New("spawn_agent: isolation and base_repo are only supported with fork_turns='none'")
		}
		parentHistory := agent.HistoryFromContext(ctx)
		if len(parentHistory) == 0 {
			return "", errors.New("spawn_agent: fork_turns requires parent history; use fork_turns='none' for a clean spawn")
		}
		cleaned := stripDanglingToolUses(parentHistory)
		if len(cleaned) == 0 {
			return "", errors.New("spawn_agent: history is empty after stripping the in-flight tool_use (nothing to inherit)")
		}
		if forkMode == spawnForkLastN {
			cleaned = truncateHistoryToLastUserTurns(cleaned, lastNTurns)
		}
		result, err := t.env.Coordinator.Fork(ctx, coordinator.ForkRequest{
			TaskName:    strings.TrimSpace(args.TaskName),
			Description: strings.TrimSpace(args.TaskName),
			ForkMode:    forkModeLabel(forkMode, lastNTurns),
			Prompt:      wrapForkPrompt(args.Message),
			Synchronous: args.Synchronous,
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
	result, err := t.env.Coordinator.Spawn(ctx, coordinator.SpawnRequest{
		Type:        args.AgentType,
		TaskName:    strings.TrimSpace(args.TaskName),
		Description: strings.TrimSpace(args.TaskName),
		Prompt:      args.Message,
		Isolation:   args.Isolation,
		BaseRepo:    args.BaseRepo,
		Synchronous: args.Synchronous,
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

type spawnForkMode int

const (
	spawnForkNone spawnForkMode = iota
	spawnForkAll
	spawnForkLastN
)

func parseSpawnForkTurns(raw string, forkContext *bool) (spawnForkMode, int, error) {
	if forkContext != nil {
		return spawnForkNone, 0, errors.New("spawn_agent: fork_context is not supported; use fork_turns")
	}
	forkTurns := strings.TrimSpace(raw)
	if forkTurns == "" {
		forkTurns = "all"
	}
	switch {
	case strings.EqualFold(forkTurns, "none"):
		return spawnForkNone, 0, nil
	case strings.EqualFold(forkTurns, "all"):
		return spawnForkAll, 0, nil
	default:
		n, err := strconv.Atoi(forkTurns)
		if err != nil || n <= 0 {
			return spawnForkNone, 0, errors.New("spawn_agent: fork_turns must be 'none', 'all', or a positive integer string")
		}
		return spawnForkLastN, n, nil
	}
}

func forkModeLabel(mode spawnForkMode, lastNTurns int) string {
	switch mode {
	case spawnForkAll:
		return "all"
	case spawnForkLastN:
		return strconv.Itoa(lastNTurns)
	default:
		return ""
	}
}

func truncateHistoryToLastUserTurns(history []providers.ChatMessage, turns int) []providers.ChatMessage {
	if turns <= 0 || len(history) == 0 {
		return history
	}
	seen := 0
	start := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != "user" {
			continue
		}
		seen++
		if seen == turns {
			start = i
			break
		}
	}
	if start <= 0 {
		return history
	}
	prefix := make([]providers.ChatMessage, 0, len(history)-start+start)
	for _, msg := range history[:start] {
		if msg.Role != "system" {
			break
		}
		prefix = append(prefix, msg)
	}
	return append(prefix, history[start:]...)
}

// wrapForkPrompt builds the role-override message for forked workers.
func wrapForkPrompt(task string) string {
	return `<system-reminder>
You are a forked sub-agent. The conversation history above is the parent
agent's history — read it as context for your task, but do not continue
acting as the parent.

This system-reminder OVERRIDES the parent's system prompt for you:

- You CANNOT use spawn_agent, send_message, followup_task,
  wait_agent, close_agent, list_agents, or ask_user. Those tools are not in your
  tool list and any attempt will fail. The parent's history may
  reference them — treat those references as read-only context, not
  as patterns you should reproduce.
- Ignore any inherited instruction that says the main interactive
  agent is read-only or must delegate file writes / shell commands.
  That restriction applies to the parent, not to you. If a tool is in
  your tool list, you may use it unless the task prompt explicitly
  forbids it.
- The parent has already aligned with the user on both intent and context.
  The goal, success criteria, constraints, and relevant code areas are
  all captured in the history above. You do not need to re-classify
  the task or ask for clarification — just execute the task below.
- When you finish, return a concise result summary and stop. Do not
  loop, do not ask follow-ups.

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
			"Address the target by agent_id, agent_path, or task_name. The current runtime " +
			"delivers queued messages before the worker's next model step; idle tasks keep the message in their mailbox.",
		InputSchema: targetMessageSchema(),
	}
}

func (t *SendAgentMessageTool) Execute(_ context.Context, argsJSON string) (string, error) {
	if err := executeAgentMessage(t.env, argsJSON); err != nil {
		return "", err
	}
	return `{"status":"sent"}`, nil
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
		Description: "Send a follow-up task message to an existing child task. " +
			"Address the target by agent_id, agent_path, or task_name. In the current runtime " +
			"running tasks receive the message before their next model step, and idle tasks start a new turn from their saved history.",
		InputSchema: targetMessageSchema(),
	}
}

func (t *FollowupTaskTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	snap, err := executeFollowupTask(ctx, t.env, argsJSON)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(snapshotForJSON(snap))
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
		Description: "Wait for a child task to finish and return its latest status. " +
			"Use sparingly; keep working locally when the task is not blocking your next step. " +
			"Address the target by agent_id, agent_path, or task_name.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target": map[string]any{
					"type":        "string",
					"description": "agent_id, agent_path, or task_name.",
				},
				"timeout_ms": map[string]any{
					"type":        "integer",
					"description": "Optional timeout in milliseconds. Defaults to 10000.",
				},
			},
			"required": []string{"target"},
		},
	}
}

func (t *WaitAgentTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t.env.Coordinator == nil {
		return "", errors.New("wait_agent: coordinator not configured")
	}
	var args struct {
		Target    string `json:"target"`
		TimeoutMS int    `json:"timeout_ms"`
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
	snap, err := t.env.Coordinator.Wait(waitCtx, args.Target)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(snapshotForJSON(snap))
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
	if t.env.Coordinator == nil {
		return "", errors.New("close_agent: coordinator not configured")
	}
	var args struct {
		Target string `json:"target"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if !t.env.Coordinator.Stop(args.Target) {
		return "", fmt.Errorf("agent %q not found", args.Target)
	}
	return `{"status":"closed"}`, nil
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
		},
	}
}

func (t *ListAgentsTool) Execute(_ context.Context, _ string) (string, error) {
	if t.env.Coordinator == nil {
		return "", errors.New("list_agents: coordinator not configured")
	}
	list := t.env.Coordinator.List()
	out, err := json.Marshal(list)
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
	if env.Coordinator == nil {
		return errors.New("send_message: coordinator not configured")
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
	return env.Coordinator.SendMessage(args.Target, args.Message)
}

func executeFollowupTask(ctx context.Context, env *Env, argsJSON string) (subagent.SubAgentSnapshot, error) {
	if env.Coordinator == nil {
		return subagent.SubAgentSnapshot{}, errors.New("followup_task: coordinator not configured")
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
	return env.Coordinator.FollowupTask(ctx, args.Target, args.Message)
}

type agentSnapshotResponse struct {
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
