package agentcontrol

import (
	"fmt"
	"sort"
	"strings"
)

// IsolationMode controls whether a worker runs in its own git
// worktree or shares the parent's working directory.
type IsolationMode string

const (
	IsolationInplace  IsolationMode = "inplace"
	IsolationWorktree IsolationMode = "worktree"
)

// WorkerType defines a role a worker can adopt.
type WorkerType struct {
	Name             string
	Role             string
	Description      string
	SystemPrompt     string
	AllowedTools     []string
	DisallowedTools  []string
	ContextScope     string
	OutputSchema     string
	SuccessCriteria  []string
	OneShot          bool
	Background       bool
	DefaultIsolation IsolationMode
	// RequiresReport marks verdict-class workers whose value depends on a
	// structured agent_report handoff. When such a worker completes without
	// filing one, the runtime issues a single mechanical closing turn with
	// tool_choice pinned to agent_report before results are delivered. It
	// never becomes a lifecycle state: if the closing turn still files
	// nothing, the run completes with a synthesized final_text report.
	RequiresReport bool
}

const DefaultSubagentType = "general-purpose"

// HelpMeRecoveryWorkerType is the worker type the helpme tool spawns for a
// fresh-context recovery. Its tool surface matches general-purpose; unlike
// general-purpose it carries RequiresReport because the parent-side history
// rewrite is built from the helper's structured agent_report handoff.
const HelpMeRecoveryWorkerType = "helpme_recovery"

const generalPurposeSystemPrompt = `You are a general-purpose sub-agent. Given the caller's prompt, use the available tools to complete the task. Complete the task fully; do not gold-plate, but do not leave it half-done.

Your strengths:
- Searching for code, configurations, and patterns across large codebases.
- Analyzing multiple files to understand system architecture.
- Investigating complex questions that require exploring many files.
- Performing multi-step implementation and verification tasks.

Guidelines:
- For file searches, search broadly when you do not know where something lives. Use read_file when you know the specific file path.
- For analysis, start broad and narrow down. Use multiple search strategies if the first one does not yield results.
- Be thorough: check multiple locations, consider different naming conventions, and look for related files.
- Never create files unless they are necessary for the task. Prefer editing existing files to creating new files.
- Never proactively create documentation files. Only create documentation when explicitly requested.

Rules:
- Make only the changes described in your task prompt. Do not refactor surrounding code.
- Verify your work when applicable using the capabilities exposed in this session.
- Be honest: if you encounter a problem you can't fix, report it clearly instead of papering over it.
- Treat command execution as non-interactive when the active tool surface exposes it. Never rely on editors, pagers, password prompts, or confirmation dialogs.
- If command execution is unavailable under the active tool surface, report skipped command-based verification instead of inventing another path. Profile-specific tool-surface guidance tells you which command capability exists and how to use it.

Output format:
Your final message IS the deliverable the parent receives. State the outcome, what you did, what changed (concrete files and paths), anything left undone or blocked, and a verifiable handle (path, command, ID) for every load-bearing claim.
You may additionally call agent_report to file a structured handoff packet; it is optional and never a substitute for a clear final message. Use agent_report.artifacts only for existing handoff files such as logs, screenshots, or test output that should be imported into Wuu-managed session storage; put source files in changed_files or evidence instead, and do not create project-local report files just to satisfy a handoff.

Response style:
- Report like an engineer, not a salesperson. No fluff, no hedging, no vague optimism.
- If something is broken, say it's broken and show the error.
- If something is unverified, say it's unverified and say why (e.g., "tests not run because the project has no test suite").
- Do not add pleasantries, summaries of the task description, or meta-commentary about your own process.`

var builtinWorkerTypes = map[string]WorkerType{
	DefaultSubagentType: {
		Name:             DefaultSubagentType,
		Role:             "Generalist",
		Description:      "General-purpose agent for researching complex questions, searching for code, and executing multi-step tasks.",
		AllowedTools:     nil,
		OneShot:          false,
		DefaultIsolation: IsolationInplace,
		ContextScope:     "Self-contained task prompt plus repository memory and skills.",
		OutputSchema:     "Final message is the deliverable: outcome, what was done, what changed, blockers, and verifiable evidence. agent_report is an optional structured handoff on top.",
		SuccessCriteria: []string{
			"Task scope is completed or clearly blocked.",
			"Changed files and verification are reported with evidence.",
		},
		SystemPrompt: generalPurposeSystemPrompt,
	},
	HelpMeRecoveryWorkerType: {
		Name:             HelpMeRecoveryWorkerType,
		Role:             "HelpMe Recovery",
		Description:      "Fresh-context recovery helper launched by the helpme tool after the parent got stuck; prefer calling helpme instead of spawning this type directly.",
		AllowedTools:     nil,
		OneShot:          false,
		DefaultIsolation: IsolationInplace,
		ContextScope:     "Self-contained HelpMe handoff brief (goal, ask, reason, constraints, failed attempts, evidence) plus repository memory and skills.",
		OutputSchema:     "Final message is the deliverable. A structured agent_report handoff is additionally required — submitted at close, requested automatically if missing — because the parent's context rewrite is built from it.",
		SuccessCriteria: []string{
			"Recovery ask is completed or clearly blocked.",
			"Changed files and verification are reported with evidence.",
		},
		SystemPrompt:   generalPurposeSystemPrompt + "\n" + reportClosingRule,
		RequiresReport: true,
	},
	"worker": {
		Name:         "worker",
		Role:         "Worker",
		Description:  "Implement a scoped code change in an isolated worktree when edits are required.",
		SystemPrompt: workerSystemPrompt,
		AllowedTools: nil,
		ContextScope: "Concrete task brief, plan step, allowed write set, current worktree status, and verification command.",
		OutputSchema: "Implementation report with changed files, commands run, blockers, risks, and evidence.",
		SuccessCriteria: []string{
			"Only assigned files or behavior are changed.",
			"Targeted verification ran or a blocker explains why it could not run.",
		},
		OneShot:          false,
		Background:       false,
		DefaultIsolation: IsolationWorktree,
	},
}

const workerSystemPrompt = `You are the Worker sub-agent. Implement only the assigned change. Preserve unrelated user work and verify locally before reporting.

Rules:
- Stay inside the assigned scope.
- Preserve unrelated user work.
- Report blockers with exact evidence.
- Your final message is the deliverable: outcome, what changed, and a verifiable handle for every claim.
`

// reportClosingRule is appended to requires_report worker prompts. The
// obligation is phrased as a property of the close ("you will be asked"),
// not a memory test: if the worker finishes without filing, the runtime
// issues one closing turn pinned to agent_report.
const reportClosingRule = "- Close with agent_report: your structured handoff is required and will be requested at close if missing.\n"

// AvailableWorkerTypes returns user-selectable built-in worker roles sorted by
// name. Internal worker types remain available to trusted runtime paths through
// LookupWorkerType but are not advertised to models or accepted by public
// orchestration tools.
func AvailableWorkerTypes() []WorkerType {
	keys := make([]string, 0, len(builtinWorkerTypes))
	for key := range builtinWorkerTypes {
		if key == HelpMeRecoveryWorkerType {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]WorkerType, 0, len(keys))
	for _, key := range keys {
		out = append(out, builtinWorkerTypes[key])
	}
	return out
}

// LookupPublicWorkerType resolves only worker types exposed through
// spawn_agent. HelpMe recovery is an internal implementation detail launched
// by the gated helpme tool.
func LookupPublicWorkerType(name string) (WorkerType, error) {
	wt, err := LookupWorkerType(name)
	if err != nil {
		return WorkerType{}, err
	}
	if wt.Name == HelpMeRecoveryWorkerType {
		return WorkerType{}, fmt.Errorf("worker type %q is internal (available: %s)", name, strings.Join(AvailableWorkerTypeNames(), ", "))
	}
	return wt, nil
}

// AvailableWorkerTypeNames returns all built-in worker role names sorted by name.
func AvailableWorkerTypeNames() []string {
	items := AvailableWorkerTypes()
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}

// LookupWorkerType resolves a worker type name to its definition.
func LookupWorkerType(name string) (WorkerType, error) {
	if name == "" {
		name = DefaultSubagentType
	}
	wt, ok := builtinWorkerTypes[name]
	if !ok {
		return WorkerType{}, fmt.Errorf("unknown worker type %q (available: %s)", name, strings.Join(AvailableWorkerTypeNames(), ", "))
	}
	return wt, nil
}

// alwaysBlockedTools is the set of tools that async sub-agents can never use.
var alwaysBlockedTools = map[string]struct{}{
	"spawn_agent":  {},
	"helpme":       {},
	"send_message": {},
	"close_agent":  {},
}

var participantSpeechTools = map[string]struct{}{
	"post_message":       {},
	"react":              {},
	"manage_participant": {},
	"manage_task":        {},
}

// FilterToolsForWorker returns the subset of fullList that this worker
// type is allowed to call. participantSpeech is an internal capability for
// conversation-native named-agent runs; ordinary subagents never receive the
// chat and task coordination tools even when their worker type otherwise has
// an unrestricted tool list.
func FilterToolsForWorker(wt WorkerType, fullList []string, participantSpeech bool) []string {
	out := make([]string, 0, len(fullList))
	allowSet := map[string]struct{}{}
	for _, t := range wt.AllowedTools {
		allowSet[t] = struct{}{}
	}
	denySet := map[string]struct{}{}
	for _, t := range wt.DisallowedTools {
		denySet[t] = struct{}{}
	}
	for _, name := range fullList {
		if _, blocked := alwaysBlockedTools[name]; blocked {
			continue
		}
		if _, denied := denySet[name]; denied {
			continue
		}
		if _, conversationNative := participantSpeechTools[name]; conversationNative {
			if participantSpeech {
				out = append(out, name)
			}
			continue
		}
		if len(wt.AllowedTools) == 0 {
			// nil means all non-orchestration tools allowed
			out = append(out, name)
			continue
		}
		if _, ok := allowSet[name]; ok {
			out = append(out, name)
		}
	}
	return out
}

// NormalizeIsolation resolves the effective isolation mode for a spawn request.
func NormalizeIsolation(reqIsolation string, wt WorkerType) (IsolationMode, error) {
	if reqIsolation == "" {
		if wt.DefaultIsolation != "" {
			return wt.DefaultIsolation, nil
		}
		return IsolationInplace, nil
	}
	switch strings.ToLower(reqIsolation) {
	case "inplace":
		return IsolationInplace, nil
	case "worktree":
		return IsolationWorktree, nil
	default:
		return "", fmt.Errorf("invalid isolation %q (valid: inplace, worktree)", reqIsolation)
	}
}
