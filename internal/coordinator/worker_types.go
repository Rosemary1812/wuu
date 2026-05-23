package coordinator

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
	OneShot          bool
	DefaultIsolation IsolationMode
}

var builtinWorkerTypes = map[string]WorkerType{
	"worker": {
		Name:             "worker",
		Description:      "General-purpose sub-agent with the full tool set. Use for implementation, testing, and exploration tasks.",
		AllowedTools:     nil,
		OneShot:          false,
		DefaultIsolation: IsolationInplace,
		SystemPrompt: `You are a worker sub-agent. You operate within a working directory provided by the lead agent. You have the same tool set and the same coding discipline as the lead. You are expected to solve tasks independently and report your result concisely.

CRITICAL RULES:
- Make ONLY the changes described in your task prompt. Do not refactor surrounding code.
- Verify your work when applicable: run tests, lint, or build commands.
- Be honest: if you encounter a problem you can't fix, report it clearly instead of papering over it.
- Treat shell commands as non-interactive. Never rely on editors, pagers, password prompts, or confirmation dialogs.
- For git, prefer explicit non-interactive forms: use ` + "`git commit -m`" + ` (or a heredoc-fed message), and never use ` + "`git commit -e`" + `, ` + "`git rebase -i`" + `, ` + "`git add -i`" + `, or similar editor-driven flows.
- If your task prompt starts with a "VERIFICATION mode" or "READ-ONLY RESEARCH mode" preamble, treat that preamble as authoritative and follow its rules — it overrides the generic guidance above.

OUTPUT FORMAT:
When you finish, produce a final message with this exact structure:
1. VERDICT — exactly one of: COMPLETE, PARTIAL, or STUCK.
2. WHAT DONE — a bullet list of specific changes made (file paths, line numbers where relevant).
3. BLOCKERS — any problems you could not solve, with evidence (error messages, failing test names, file:line references).
4. NEXT STEPS — what the orchestrator or user should do next, if anything. Be specific: "run X test", "review Y file", "decide between A and B".
5. EVIDENCE — command outputs, test results, or relevant excerpts that back up your verdict. Include enough detail that the orchestrator doesn't need to re-run the command to trust your result.
Do not omit the verdict line. The orchestrator parses it.

RESPONSE STYLE:
- Report like an engineer, not a salesperson. No fluff, no hedging, no vague optimism.
- If something is broken, say it's broken and show the error.
- If something is unverified, say it's unverified and say why (e.g., "tests not run because the project has no test suite").
- Do NOT add pleasantries, summaries of the task description, or meta-commentary about your own process.`,
	},
	"verification": {
		Name:             "verification",
		Description:      "Read-only adversarial reviewer. Use to verify changes, find bugs, and validate correctness.",
		SystemPrompt:     VerificationPreset,
		AllowedTools:     nil,
		OneShot:          true,
		DefaultIsolation: IsolationInplace,
	},
	"research": {
		Name:             "research",
		Description:      "Read-only researcher. Use to investigate codebases, trace execution paths, and answer questions about existing code.",
		SystemPrompt:     ResearchPreset,
		AllowedTools:     nil,
		OneShot:          true,
		DefaultIsolation: IsolationInplace,
	},
}

// LookupWorkerType resolves a worker type name to its definition.
func LookupWorkerType(name string) (WorkerType, error) {
	if name == "" {
		name = "worker"
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

// alwaysBlockedTools is the set of tools that workers can never use.
var alwaysBlockedTools = map[string]struct{}{
	"ask_user": {},
}

// FilterToolsForWorker returns the subset of fullList that this worker
// type is allowed to call. Workers keep orchestration tools so sub-agents
// can spawn/message their own children like Codex V2; ask_user remains
// blocked because background agents cannot interrupt the human directly.
func FilterToolsForWorker(wt WorkerType, fullList []string) []string {
	out := make([]string, 0, len(fullList))
	allowSet := map[string]struct{}{}
	for _, t := range wt.AllowedTools {
		allowSet[t] = struct{}{}
	}
	for _, name := range fullList {
		if _, blocked := alwaysBlockedTools[name]; blocked {
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
