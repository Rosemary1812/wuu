package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/config"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
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
			"The available subagent_type values and their descriptions are provided in the Subagent Types section of your system context. " +
			"Specify subagent_type to launch a fresh specialized agent. Omit subagent_type to fork yourself: " +
			"the child inherits your full conversation context and always runs in the background. For a fresh " +
			"general-purpose agent, set subagent_type='general-purpose'. The child has its own context " +
			"and its final answer is delivered to you when it finishes. " +
			"Use a child only when delegation materially improves the task: independent investigation, " +
			"parallel implementation slices, risky verification, or work that benefits from a separate context. " +
			"Ordinary child agents are temporary and do not use saved profile memory. Set agent_profile only when the user asks to use a named " +
			"Agent Profile with saved memory, or when an agent-profile policy requires one; the profile name selects the saved memory to reuse. " +
			"Do not set agent_profile for routine one-off delegation. " +
			"Keep work local when the next step is tightly coupled, on the critical path, or simpler to do directly. " +
			"Write a concrete brief using the shared Base Agent Brief Contract. " +
			prompttext.AgentBriefContractSummary() + " " +
			"For a fresh subagent_type invocation, write `prompt` as the helper's first and complete query: include the task, necessary context, scope, constraints, acceptance criteria, and expected report. " +
			"For a fork invocation, write `prompt` as an incremental directive: rely on inherited context and specify the focus, owned scope, non-goals, and deliverable instead of pasting the whole transcript. " +
			"Use helpme, not ordinary spawn_agent, when the purpose is context rescue, repeated-failure recovery, or a fresh second opinion on the parent agent's assumptions. " +
			prompttext.ProfileBriefExtensionSummary() + " " +
			prompttext.EphemeralBriefExtensionSummary() + " " +
			"Do not make the child infer missing acceptance criteria from a vague ask. " +
			"The child's final message is its deliverable: state exactly what it must contain, " +
			"including verifiable handles (paths, commands, IDs) you can check without trusting prose. " +
			"By default the agent runs in the user's current repo, so any files it creates or edits " +
			"land directly in the working tree. Set isolation='worktree' only " +
			"for destructive or broad experiments, overlapping or uncertain concurrent writes, " +
			"generated outputs/formatters that may touch many files, or when the user explicitly " +
			"asked for a sandbox. Do not use a worktree just because the task involves writing " +
			"files; small additive or clearly disjoint edits can share the current repo when shared visibility helps. " +
			"Always include a short description (3-5 words) summarizing what the agent will do. " +
			"Each fresh subagent_type invocation starts without conversation context, so the `prompt` parameter must be a complete brief. " +
			"Forks inherit your current context; do not use a fork when a fresh independent second opinion is needed. " +
			"Fresh agents run in the foreground by default, so the tool returns the child result before you continue. " +
			"Set run_in_background=true only for independent or long-running work that can proceed while you do other work; " +
			"background agents return quickly and send a completion notification when done. " +
			"After spawning background agents, continue meaningful non-overlapping local work when available; otherwise end your turn " +
			"and let the completion notification resume you. Do not sleep, poll, or loop checking status. When the next step truly " +
			"depends on child output, end your turn and let the child's completion notification resume you instead of blocking. " +
			"Spawn multiple independent agents in parallel by calling spawn_agent multiple times in the same response.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description": map[string]any{
					"type":        "string",
					"description": "Short 3-5 word summary of what the agent will do.",
				},
				"prompt": map[string]any{
					"type": "string",
					"description": "Concrete task brief. " + prompttext.AgentBriefContractSummary() +
						" For fresh subagents, this is their first query and must be self-contained: include the task, necessary context, starting points, constraints, acceptance criteria, and expected report. " +
						"For forks, keep this as an incremental directive that relies on inherited context while naming focus, scope, non-goals, and deliverable. " +
						"For code edits, include owned files/modules and out-of-scope neighbors. Use helpme instead when the purpose is context rescue or second-opinion recovery.",
				},
				"subagent_type": map[string]any{
					"type":        "string",
					"description": "Optional specialized agent type. The available values are provided in the Subagent Types section of your system context. Omit to fork yourself with full conversation context.",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Optional addressable task name. Must use only lowercase letters, digits, and underscores. If omitted, wuu derives a unique name from description.",
				},
				"agent_profile": map[string]any{
					"type":        "string",
					"description": "Optional Agent Profile name with saved memory. Use only when the user explicitly wants that profile or an agent-profile policy requires one; omit for ordinary temporary child tasks.",
				},
				"run_in_background": map[string]any{
					"type":        "boolean",
					"description": "Optional. For fresh subagents only. Set true for independent or long-running work that should return immediately and notify you later. Omit or false when the next step needs the result now. Forks always run in the background.",
				},
				"isolation": map[string]any{
					"type":        "string",
					"enum":        []string{"worktree"},
					"description": "Optional. 'worktree' creates a fresh isolated workspace for sandboxed edits. Omit to run in the current repo.",
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
		RunInBackground bool   `json:"run_in_background"`
		Isolation       string `json:"isolation"`
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
		return "", errors.New("spawn_agent: agent_profile \"default\" is reserved for ordinary temporary sessions. Omit agent_profile or choose a named profile")
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
		result.NextSteps = subagentNextStepsForDiscovery(t.env, result.NextSteps)
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
	result.NextSteps = subagentNextStepsForDiscovery(t.env, result.NextSteps)
	out, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func subagentNextStepsForDiscovery(env *Env, steps []string) []string {
	if env == nil || !env.ToolSearchEnabled || env.NativeDeferredToolDiscovery || !subagentStepsMentionManagementTool(steps) {
		return steps
	}
	out := append([]string(nil), steps...)
	out = append(out, "If a subagent management tool is not visible yet, load it first with tool_search using select:send_message or select:close_agent.")
	return out
}

func subagentStepsMentionManagementTool(steps []string) bool {
	for _, step := range steps {
		for _, name := range subagentManagementTools {
			if strings.Contains(step, name) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// helpme
// ---------------------------------------------------------------------------

type HelpMeTool struct{ env *Env }

func NewHelpMeTool(env *Env) *HelpMeTool { return &HelpMeTool{env: env} }

func (t *HelpMeTool) Name() string            { return "helpme" }
func (t *HelpMeTool) IsReadOnly() bool        { return false }
func (t *HelpMeTool) IsConcurrencySafe() bool { return false }

func (t *HelpMeTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "helpme",
		Description: "Start a HelpMe recovery when the main agent may be stuck in a wrong direction, polluted context, or repeated failed attempts. " +
			"This launches a fresh general-purpose subagent with a clean context and returns immediately with its agent_id and agent_path. " +
			"Use this instead of spawn_agent when the purpose is context rescue / second-opinion recovery, especially after user feedback like 'still wrong' or after several unsuccessful local attempts. " +
			"Include the original goal, the current interpretation, failed attempts, constraints, and concrete evidence so the fresh helper can avoid repeating your mistakes. " +
			"The helper runs in the background and resumes you with its result when it finishes; when a structured HelpMe report is available, that completion replaces polluted parent context with a bounded HelpMe recovery summary. " +
			"If you manually use inception after HelpMe, summarize only durable facts, report/result paths, and trace references; do not paste or merge raw parent/helper transcripts.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"reason": map[string]any{
					"type":        "string",
					"description": "Why recovery is needed now. Be concrete about the suspected wrong assumption or repeated failure.",
				},
				"original_goal": map[string]any{
					"type":        "string",
					"description": "The user's original intent or latest task contract, as neutrally as possible.",
				},
				"current_understanding": map[string]any{
					"type":        "string",
					"description": "Your current best understanding before recovery, including uncertainty.",
				},
				"ask": map[string]any{
					"type":        "string",
					"description": "The exact task for the fresh helper to perform.",
				},
				"failed_attempts": map[string]any{
					"type":        "array",
					"description": "String array of specific approaches already tried or now considered low-confidence, with why they did not work. Use [] when there are none.",
					"items":       map[string]any{"type": "string"},
				},
				"constraints": map[string]any{
					"type":        "array",
					"description": "String array of user, repo, product, safety, or verification constraints the helper must preserve. Use [] when there are none.",
					"items":       map[string]any{"type": "string"},
				},
				"evidence": map[string]any{
					"type":        "array",
					"description": "String array of important evidence already observed: files, errors, tests, logs, or facts. Prefer references over long raw output. Use [] when there is none.",
					"items":       map[string]any{"type": "string"},
				},
			},
		},
	}
}

func (t *HelpMeTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t.env.AgentControl == nil {
		return "", errors.New("helpme: agent control not configured (this build does not support sub-agents)")
	}
	if currentAgentPath(t.env) != agentthread.RootPath {
		return "", errors.New("helpme is only available to the main agent")
	}
	var args helpMeArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}

	history := agent.HistoryFromContext(ctx)
	originalGoal := helpMeFirstNonEmpty(args.OriginalGoal, latestUserGoalFromHistory(history), "Continue the user's current coding task.")
	ask := helpMeFirstNonEmpty(args.Ask, originalGoal)
	reason := strings.TrimSpace(args.Reason)
	if reason == "" {
		reason = "The main agent requested a fresh-context recovery."
	}
	// Machine-extract the parent's decision journal as the primary handoff
	// carrier: the self-reported args carry the stuck agent's framing, the
	// extraction does not. Bounded by a hard timeout; any failure degrades
	// to the resolved-brief-only rescue and never fails the call.
	journal := extractHelpMeParentJournal(ctx, t.env, history)
	prompt := buildHelpMePrompt(helpMePromptInput{
		Reason:                 reason,
		OriginalGoal:           originalGoal,
		ParentExecutionJournal: journal,
		CurrentUnderstanding:   strings.TrimSpace(args.CurrentUnderstanding),
		Ask:                    ask,
		FailedAttempts:         trimStringSlice(args.FailedAttempts),
		Constraints:            trimStringSlice(args.Constraints),
		Evidence:               trimStringSlice(args.Evidence),
	})

	result, err := t.env.AgentControl.Spawn(ctx, agentcontrol.SpawnRequest{
		Type:        agentcontrol.HelpMeRecoveryWorkerType,
		TaskName:    deriveAgentTaskName("helpme_recovery"),
		Description: "HelpMe recovery",
		Prompt:      prompt,
		ParentID:    strings.TrimSpace(t.env.AgentID),
		ParentPath:  currentAgentPath(t.env),
		Synchronous: false,
	})
	if err != nil {
		return "", err
	}
	// Register the RESOLVED brief (args first, history fallback, placeholder
	// last) as first-class recovery state, keyed by the helper. The await
	// side rebuilds the joint compact from it instead of re-parsing trace
	// files, so the real user goal survives into the rewrite.
	t.env.AgentControl.RegisterHelpMeRecovery(agentcontrol.HelpMeRecovery{
		HelperID:               result.AgentID,
		ParentPath:             currentAgentPath(t.env),
		ParentExecutionJournal: journal,
		Brief: agentcontrol.HelpMeRecoveryBrief{
			OriginalGoal:         originalGoal,
			Ask:                  ask,
			Reason:               reason,
			CurrentUnderstanding: strings.TrimSpace(args.CurrentUnderstanding),
			Constraints:          trimStringSlice(args.Constraints),
			FailedAttempts:       trimStringSlice(args.FailedAttempts),
			Evidence:             trimStringSlice(args.Evidence),
		},
	})

	response := helpMeResponse{
		Action:    "helpme",
		Status:    result.Status,
		AgentID:   result.AgentID,
		AgentPath: result.AgentPath,
		NextSteps: subagentNextStepsForDiscovery(t.env, result.NextSteps),
	}
	report, reportOK := t.env.AgentControl.AgentReportDetailsForTask(response.AgentID)
	if reportOK {
		response.Report = &report
	}
	response.ReportMissing = helpMeReportMissing(response.Status, reportOK)
	// The main trace is a pure audit artifact now: the helper is already
	// spawned and the recovery state is registered, so a failed write must
	// not fail the call (a retry would only spawn an orphan duplicate).
	mainTracePath, traceErr := writeHelpMeMainTrace(t.env, history, args, result, response.Report, response.ReportMissing)
	if traceErr != nil {
		response.NextSteps = append(response.NextSteps,
			"Audit trace could not be written ("+traceErr.Error()+"); recovery proceeds normally, but main_trace_path is unavailable for this rescue.")
	} else {
		response.MainTracePath = mainTracePath
	}

	out, err := json.Marshal(response)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type helpMeResponse struct {
	Action         string                           `json:"action"`
	Status         string                           `json:"status"`
	AgentID        string                           `json:"agent_id,omitempty"`
	AgentPath      string                           `json:"agent_path,omitempty"`
	Result         string                           `json:"result,omitempty"`
	ResultPath     string                           `json:"result_path,omitempty"`
	MainTracePath  string                           `json:"main_trace_path,omitempty"`
	Error          string                           `json:"error,omitempty"`
	Report         *agentcontrol.AgentReportDetails `json:"report,omitempty"`
	ReportMissing  bool                             `json:"report_missing,omitempty"`
	NextSteps      []string                         `json:"next_steps,omitempty"`
	HistoryRewrite *compact.HelpMeHistoryRewrite    `json:"history_rewrite,omitempty"`
}

type helpMeMainTraceRecord struct {
	SchemaVersion   string                           `json:"schema_version"`
	CreatedAt       time.Time                        `json:"created_at"`
	ParentAgentID   string                           `json:"parent_agent_id,omitempty"`
	ParentPath      string                           `json:"parent_path,omitempty"`
	HelperAgentID   string                           `json:"helper_agent_id,omitempty"`
	HelperAgentPath string                           `json:"helper_agent_path,omitempty"`
	Args            helpMeArgs                       `json:"args"`
	MainHistory     []providers.ChatMessage          `json:"main_history,omitempty"`
	HelperResult    *agentcontrol.SpawnResult        `json:"helper_result,omitempty"`
	Report          *agentcontrol.AgentReportDetails `json:"report,omitempty"`
	ReportMissing   bool                             `json:"report_missing,omitempty"`
}

type helpMeArgs struct {
	Reason               string   `json:"reason"`
	OriginalGoal         string   `json:"original_goal"`
	CurrentUnderstanding string   `json:"current_understanding"`
	Ask                  string   `json:"ask"`
	FailedAttempts       []string `json:"failed_attempts"`
	Constraints          []string `json:"constraints"`
	Evidence             []string `json:"evidence"`
}

func (a *helpMeArgs) UnmarshalJSON(data []byte) error {
	var raw struct {
		Reason               string          `json:"reason"`
		OriginalGoal         string          `json:"original_goal"`
		CurrentUnderstanding string          `json:"current_understanding"`
		Ask                  string          `json:"ask"`
		FailedAttempts       json.RawMessage `json:"failed_attempts"`
		Constraints          json.RawMessage `json:"constraints"`
		Evidence             json.RawMessage `json:"evidence"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	failedAttempts, err := decodeHelpMeStringList(raw.FailedAttempts, "failed_attempts")
	if err != nil {
		return err
	}
	constraints, err := decodeHelpMeStringList(raw.Constraints, "constraints")
	if err != nil {
		return err
	}
	evidence, err := decodeHelpMeStringList(raw.Evidence, "evidence")
	if err != nil {
		return err
	}
	*a = helpMeArgs{
		Reason:               raw.Reason,
		OriginalGoal:         raw.OriginalGoal,
		CurrentUnderstanding: raw.CurrentUnderstanding,
		Ask:                  raw.Ask,
		FailedAttempts:       failedAttempts,
		Constraints:          constraints,
		Evidence:             evidence,
	}
	return nil
}

func decodeHelpMeStringList(raw json.RawMessage, field string) ([]string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err == nil {
		return values, nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if strings.TrimSpace(single) == "" {
			return nil, nil
		}
		return []string{single}, nil
	}
	return nil, fmt.Errorf("%s must be a string or string array", field)
}

type helpMePromptInput struct {
	Reason                 string
	OriginalGoal           string
	ParentExecutionJournal string
	CurrentUnderstanding   string
	Ask                    string
	FailedAttempts         []string
	Constraints            []string
	Evidence               []string
}

func buildHelpMePrompt(input helpMePromptInput) string {
	var b strings.Builder
	b.WriteString("# HelpMe Handoff Brief\n\n")
	b.WriteString("Take over the task below using your own judgment. Treat this brief as context, then verify important facts from the workspace, runtime output, or other evidence before acting.\n\n")
	b.WriteString("Your final message is the deliverable the requester continues from: state the outcome, what changed, what remains, and the verifiable evidence behind each load-bearing claim.\n\n")
	writeHelpMePromptField(&b, "Why this handoff is needed", input.Reason)
	writeHelpMePromptField(&b, "User goal", input.OriginalGoal)
	if journal := strings.TrimSpace(input.ParentExecutionJournal); journal != "" {
		b.WriteString("## Parent execution journal\n")
		b.WriteString("Machine-extracted from the requester's transcript; more reliable than the self-reported fields below. Paths marked ruled out or low-confidence are negative knowledge - do not retry them without new evidence.\n\n")
		b.WriteString(journal)
		b.WriteString("\n\n")
	}
	writeHelpMePromptField(&b, "Current context", input.CurrentUnderstanding)
	writeHelpMePromptList(&b, "Tried or low-confidence paths", input.FailedAttempts)
	writeHelpMePromptList(&b, "Constraints to preserve", input.Constraints)
	writeHelpMePromptList(&b, "Relevant evidence", input.Evidence)
	writeHelpMePromptField(&b, "Task to complete", input.Ask)
	b.WriteString("Work in the current workspace when inspection or changes are needed. If you change files, keep the change scoped and run the most relevant verification you can.\n")
	return strings.TrimSpace(b.String())
}

// helpMeJournalTimeout is the hard wall-clock bound on the synchronous
// parent-journal extraction inside a helpme call. On expiry the rescue
// degrades to the resolved self-reported brief. A variable (not const) so
// tests can shorten it.
var helpMeJournalTimeout = 90 * time.Second

// extractHelpMeParentJournal machine-extracts the parent's decision journal
// using the worker runtime's default client and model. Every failure mode —
// no runtime, no client, empty history, extraction error, timeout — degrades
// to "" so the helpme call itself never fails because of the extraction.
func extractHelpMeParentJournal(ctx context.Context, env *Env, history []providers.ChatMessage) string {
	if env == nil || env.AgentControl == nil || len(history) == 0 {
		return ""
	}
	defaults := env.AgentControl.Manager().RuntimeDefaults()
	if defaults.Client == nil || strings.TrimSpace(defaults.Model) == "" {
		return ""
	}
	jctx, cancel := context.WithTimeout(ctx, helpMeJournalTimeout)
	defer cancel()
	journal, err := compact.BuildHelpMeParentJournal(jctx, defaults.Client, defaults.Model, compact.Budget{
		ContextTokens:       defaults.ContextWindow,
		InputTokens:         defaults.MaxInputTokens,
		OutputReserveTokens: defaults.OutputReserveTokens,
	}, history)
	if err != nil {
		providers.DebugLogf("helpme: parent journal extraction degraded to self-reported brief: %v", err)
		return ""
	}
	return journal
}

func writeHelpMePromptField(b *strings.Builder, label, value string) {
	if value = strings.TrimSpace(value); value != "" {
		fmt.Fprintf(b, "## %s\n%s\n\n", label, value)
	}
}

func writeHelpMePromptList(b *strings.Builder, label string, values []string) {
	values = trimStringSlice(values)
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n", label)
	for _, value := range values {
		fmt.Fprintf(b, "- %s\n", value)
	}
	b.WriteByte('\n')
}

func helpMeReportMissing(status string, reportOK bool) bool {
	if reportOK {
		return false
	}
	switch subagent.Status(strings.TrimSpace(status)) {
	case subagent.StatusCompleted, subagent.StatusFailed, subagent.StatusCancelled:
		return true
	default:
		return false
	}
}

func writeHelpMeMainTrace(env *Env, history []providers.ChatMessage, args helpMeArgs, result *agentcontrol.SpawnResult, report *agentcontrol.AgentReportDetails, reportMissing bool) (string, error) {
	if env == nil || strings.TrimSpace(env.SessionDir) == "" {
		return "", nil
	}
	dir := filepath.Join(env.SessionDir, "helpme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("helpme: create trace dir: %w", err)
	}
	now := time.Now().UTC()
	path := filepath.Join(dir, helpMeMainTraceFilename(now, result))
	payload := helpMeMainTraceRecord{
		SchemaVersion:   "wuu/helpme-main-trace/v0.1",
		CreatedAt:       now,
		ParentAgentID:   strings.TrimSpace(env.AgentID),
		ParentPath:      currentAgentPath(env),
		HelperAgentID:   helpMeResultAgentID(result),
		HelperAgentPath: helpMeResultAgentPath(result),
		Args:            args,
		MainHistory:     providers.CloneChatMessages(history),
		HelperResult:    result,
		Report:          report,
		ReportMissing:   reportMissing,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("helpme: encode trace: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("helpme: write trace: %w", err)
	}
	return helpMeSessionRef(env.SessionDir, path), nil
}

func helpMeSessionRef(sessionDir, path string) string {
	sessionDir = strings.TrimSpace(sessionDir)
	path = strings.TrimSpace(path)
	if sessionDir == "" || path == "" {
		return path
	}
	rel, err := filepath.Rel(sessionDir, path)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return path
	}
	return "$SESSION_DIR/" + filepath.ToSlash(rel)
}

func helpMeMainTraceFilename(now time.Time, result *agentcontrol.SpawnResult) string {
	agentID := safeHelpMeTraceID(helpMeResultAgentID(result))
	if agentID == "" {
		return fmt.Sprintf("%d-main-trace.json", now.UnixNano())
	}
	return fmt.Sprintf("%d-%s-main-trace.json", now.UnixNano(), agentID)
}

func safeHelpMeTraceID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 96 {
			break
		}
	}
	return strings.Trim(b.String(), "_-")
}

func helpMeResultAgentID(result *agentcontrol.SpawnResult) string {
	if result == nil {
		return ""
	}
	return strings.TrimSpace(result.AgentID)
}

func helpMeResultAgentPath(result *agentcontrol.SpawnResult) string {
	if result == nil {
		return ""
	}
	return strings.TrimSpace(result.AgentPath)
}

func latestUserGoalFromHistory(history []providers.ChatMessage) string {
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg.Role != "user" || wuucontext.IsSystemReminder(msg.Name, msg.Content) || wuucontext.IsAgentNotification(msg.Name, msg.Content) {
			continue
		}
		if content := strings.TrimSpace(msg.DisplayContent); content != "" {
			return content
		}
		if content := strings.TrimSpace(msg.Content); content != "" {
			return content
		}
	}
	return ""
}

func helpMeFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
  agent is read-only or must delegate file changes / command execution.
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
		Description: "Send a message to an existing child task (queue-or-resume). Address the " +
			"target by agent_id, agent_path, or task_name. If the target is still running, the " +
			"message is queued and delivered before the child's next model round without " +
			"interrupting its current step. If the target has already finished — completed, " +
			"failed, or cancelled — it is revived in place with its full prior context plus your " +
			"new message. Leave trigger_turn unset (or false) for an interim note that the target " +
			"picks up on its own schedule. Set trigger_turn=true to frame the message as a task " +
			"hand-off that drives the target's next turn immediately and returns its snapshot.",
		InputSchema: targetMessageSchema(),
	}
}

func (t *SendAgentMessageTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Target      string `json:"target"`
		Message     string `json:"message"`
		TriggerTurn bool   `json:"trigger_turn"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if t.env.AgentControl == nil {
		return "", errors.New("send_message: agent control not configured")
	}
	if strings.TrimSpace(args.Target) == "" {
		return "", errors.New("send_message: target is required")
	}
	if !args.TriggerTurn {
		if err := t.env.AgentControl.SendMessageFrom(currentAgentPath(t.env), args.Target, args.Message); err != nil {
			return "", err
		}
		return `{"action":"send_message","status":"sent"}`, nil
	}
	// trigger_turn: fold in the former followup_task behavior — revive the
	// target and drive its next turn, returning the resumed snapshot.
	snap, err := t.env.AgentControl.FollowupTaskFrom(currentAgentPath(t.env), ctx, args.Target, args.Message)
	if err != nil {
		return "", err
	}
	resp := snapshotForJSON(snap)
	resp.Action = "send_message"
	out, err := json.Marshal(resp)
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

// list_agents was removed as a model-facing tool: the live child-agent roster
// (status, type, description, timing) is now injected per turn as the
// <subagent_status> reminder (agentcontrol.ActiveTaskReminder), keeping the
// dynamic roster out of any static tool schema. ListFrom stays as a control
// method for that reminder and the deferred-bundle activation gate.

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
		Description: "Submit an optional structured handoff report for the current child agent. " +
			"Your final message is always the deliverable; filing this report additionally gives parent " +
			"and later agents a durable structured summary with constraints, work done, blockers, evidence " +
			"references, and artifact paths. Not filing it never blocks completion — the runtime synthesizes " +
			"a final_text handoff from your final message and observed facts. Workers whose type " +
			"requires a structured report are asked to file it at close automatically. " +
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
			"trigger_turn": map[string]any{
				"type":        "boolean",
				"description": "Optional. Omit or false to queue an interim note the target reads on its own schedule. Set true to hand off a task that drives the target's next turn immediately and returns its snapshot.",
			},
		},
		"required": []string{"target", "message"},
	}
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
