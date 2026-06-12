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
	Description      string
	SystemPrompt     string
	AllowedTools     []string
	DisallowedTools  []string
	OneShot          bool
	Background       bool
	DefaultIsolation IsolationMode
}

const DefaultSubagentType = "general-purpose"

var builtinWorkerTypes = map[string]WorkerType{
	DefaultSubagentType: {
		Name:             DefaultSubagentType,
		Description:      "General-purpose agent for researching complex questions, searching for code, and executing multi-step tasks.",
		AllowedTools:     nil,
		OneShot:          false,
		DefaultIsolation: IsolationInplace,
		SystemPrompt: `You are a general-purpose sub-agent. Given the caller's prompt, use the available tools to complete the task. Complete the task fully; do not gold-plate, but do not leave it half-done.

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
- Verify your work when applicable: run tests, lint, or build commands.
- Be honest: if you encounter a problem you can't fix, report it clearly instead of papering over it.
- Treat shell commands as non-interactive. Never rely on editors, pagers, password prompts, or confirmation dialogs.
- For git, prefer explicit non-interactive forms: use ` + "`git commit -m`" + ` (or a heredoc-fed message), and never use ` + "`git commit -e`" + `, ` + "`git rebase -i`" + `, ` + "`git add -i`" + `, or similar editor-driven flows.

Output format:
Before your final message, call agent_report with a structured handoff packet. Include the outcome, a concise summary, changed_files when relevant, concrete work_done, blockers when any, risks when any, verification performed or skipped, next_steps when useful, and evidence entries that point to files, commands, or artifacts. Your final message should match the structure below and may summarize the same report.

When you finish, produce a final message with this exact structure:
1. VERDICT — exactly one of: COMPLETE, PARTIAL, or STUCK.
2. WHAT DONE — a bullet list of specific changes made (file paths, line numbers where relevant).
3. BLOCKERS — any problems you could not solve, with evidence (error messages, failing test names, file:line references).
4. NEXT STEPS — what the orchestrator or user should do next, if anything. Be specific: "run X test", "review Y file", "decide between A and B".
5. EVIDENCE — command outputs, test results, or relevant excerpts that back up your verdict. Include enough detail that the orchestrator doesn't need to re-run the command to trust your result.
Do not omit the verdict line. The orchestrator parses it.

Response style:
- Report like an engineer, not a salesperson. No fluff, no hedging, no vague optimism.
- If something is broken, say it's broken and show the error.
- If something is unverified, say it's unverified and say why (e.g., "tests not run because the project has no test suite").
- Do not add pleasantries, summaries of the task description, or meta-commentary about your own process.`,
	},
	"verification": {
		Name:             "verification",
		Description:      "Verification specialist. Use after non-trivial implementation work to run checks, try to break the change, and return a PASS/FAIL/PARTIAL verdict with evidence.",
		SystemPrompt:     VerificationPreset,
		AllowedTools:     nil,
		DisallowedTools:  []string{"spawn_agent", "send_message", "followup_task", "wait_agent", "await_agents", "close_agent", "list_agents", "write_file", "edit_file", "apply_patch"},
		OneShot:          true,
		Background:       true,
		DefaultIsolation: IsolationInplace,
	},
}

// LookupWorkerType resolves a worker type name to its definition.
func LookupWorkerType(name string) (WorkerType, error) {
	if name == "" {
		name = DefaultSubagentType
	}
	wt, ok := builtinWorkerTypes[name]
	if !ok {
		keys := make([]string, 0, len(builtinWorkerTypes))
		for k := range builtinWorkerTypes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return WorkerType{}, fmt.Errorf("unknown worker type %q (available: %s)", name, strings.Join(keys, ", "))
	}
	return wt, nil
}

// alwaysBlockedTools is the set of tools that async sub-agents can never use.
var alwaysBlockedTools = map[string]struct{}{
	"spawn_agent":   {},
	"send_message":  {},
	"followup_task": {},
	"wait_agent":    {},
	"await_agents":  {},
	"close_agent":   {},
	"list_agents":   {},
}

// FilterToolsForWorker returns the subset of fullList that this worker
// type is allowed to call.
func FilterToolsForWorker(wt WorkerType, fullList []string) []string {
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
