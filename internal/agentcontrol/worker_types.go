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
}

const DefaultSubagentType = "general-purpose"

var builtinWorkerTypes = map[string]WorkerType{
	DefaultSubagentType: {
		Name:             DefaultSubagentType,
		Role:             "Generalist",
		Description:      "General-purpose agent for researching complex questions, searching for code, and executing multi-step tasks.",
		AllowedTools:     nil,
		OneShot:          false,
		DefaultIsolation: IsolationInplace,
		ContextScope:     "Self-contained task prompt plus repository memory and skills.",
		OutputSchema:     "Use agent_report and the standard VERDICT / WHAT DONE / BLOCKERS / NEXT STEPS / EVIDENCE final format.",
		SuccessCriteria: []string{
			"Task scope is completed or clearly blocked.",
			"Changed files and verification are reported with evidence.",
		},
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
- Verify your work when applicable using the capabilities exposed in this session.
- Be honest: if you encounter a problem you can't fix, report it clearly instead of papering over it.
- Treat command execution as non-interactive when the active tool surface exposes it. Never rely on editors, pagers, password prompts, or confirmation dialogs.
- If command execution is unavailable under the active tool surface, report skipped command-based verification instead of inventing another path. Profile-specific tool-surface guidance tells you which command capability exists and how to use it.

Output format:
Before your final message, call agent_report with a structured handoff packet. Include the outcome, a concise summary, changed_files when relevant, concrete work_done, blockers when any, risks when any, verification performed or skipped, next_steps when useful, and evidence entries that point to files, commands, or artifacts.
Use agent_report.artifacts only for existing handoff files such as logs, screenshots, reports, or test output that should be imported into Wuu-managed session storage. Put source files in changed_files or evidence instead, and do not create project-local report files just to satisfy the handoff.
After agent_report succeeds, return a concise final summary and stop. Do not repeat the full report; mention only the outcome and any blocker or next step the parent must notice.

Response style:
- Report like an engineer, not a salesperson. No fluff, no hedging, no vague optimism.
- If something is broken, say it's broken and show the error.
- If something is unverified, say it's unverified and say why (e.g., "tests not run because the project has no test suite").
- Do not add pleasantries, summaries of the task description, or meta-commentary about your own process.`,
	},
	"verification": {
		Name:            "verification",
		Role:            "Verifier",
		Description:     "Verification specialist. Use after non-trivial implementation work to run checks, try to break the change, and return a PASS/FAIL/PARTIAL verdict with evidence.",
		SystemPrompt:    VerificationPreset,
		AllowedTools:    nil,
		DisallowedTools: []string{"spawn_agent", "helpme", "inception", "send_message", "followup_task", "wait_agent", "await_agents", "close_agent", "list_agents", "write_file", "edit_file", "apply_patch"},
		ContextScope:    "Final diff, acceptance criteria, changed files, and declared verification policy.",
		OutputSchema:    "Use agent_report with outcome, verification, risks, blockers, and evidence; final verdict must be PASS, FAIL, or PARTIAL.",
		SuccessCriteria: []string{
			"Verification commands or manual checks are recorded.",
			"Failures include exact evidence and next debugging step.",
		},
		OneShot:          true,
		Background:       true,
		DefaultIsolation: IsolationInplace,
	},
	"planner": {
		Name:         "planner",
		Role:         "Lead / Planner",
		Description:  "Break a goal into phases, artifacts, verification gates, and escalation points before implementation.",
		SystemPrompt: rolePrompt("Lead / Planner", "Understand the goal, define phases, keep scope tight, and produce an execution plan. Do not edit project files."),
		AllowedTools: readOnlyPlanningTools(),
		ContextScope: "Task goal, current durable state, project rules, known blockers, and research summaries.",
		OutputSchema: "Plan artifact with phases, owners, verification policy, risks, and next action.",
		SuccessCriteria: []string{
			"Plan has ordered steps with verification gates.",
			"Large edits are blocked until research and plan artifacts exist.",
		},
		OneShot:          true,
		Background:       false,
		DefaultIsolation: IsolationInplace,
	},
	"researcher": {
		Name:         "researcher",
		Role:         "Researcher",
		Description:  "Read code, locate relevant files, summarize current behavior, and identify constraints without editing.",
		SystemPrompt: rolePrompt("Researcher", "Inspect the codebase and explain the existing system. Do not edit files."),
		AllowedTools: readOnlyPlanningTools(),
		ContextScope: "Repository rules, task goal, targeted symbols, relevant files, and nearby tests.",
		OutputSchema: "Research report with files read, current behavior, constraints, risks, and recommended next step.",
		SuccessCriteria: []string{
			"Relevant files are cited.",
			"Facts are separated from assumptions.",
		},
		OneShot:          true,
		Background:       false,
		DefaultIsolation: IsolationInplace,
	},
	"worker": {
		Name:         "worker",
		Role:         "Worker",
		Description:  "Implement a scoped code change in an isolated worktree when edits are required.",
		SystemPrompt: rolePrompt("Worker", "Implement only the assigned change. Preserve unrelated user work and verify locally before reporting."),
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
	"reviewer": {
		Name:         "reviewer",
		Role:         "Reviewer",
		Description:  "Review diffs for real bugs, regressions, missing tests, and unsafe behavior without editing.",
		SystemPrompt: rolePrompt("Reviewer", "Review the diff as an independent checker. Report only real bugs, security issues, logic errors, or missing verification."),
		AllowedTools: readOnlyVerificationTools(),
		ContextScope: "Final diff, changed files, acceptance criteria, and verification evidence.",
		OutputSchema: "Review report with findings ordered by severity, open questions, and verification gaps.",
		SuccessCriteria: []string{
			"Findings are actionable and evidence-backed.",
			"Style-only nits and speculative issues are omitted.",
		},
		OneShot:          true,
		Background:       true,
		DefaultIsolation: IsolationInplace,
	},
	"qa": {
		Name:         "qa",
		Role:         "QA / Verifier",
		Description:  "Run product-facing verification, smoke tests, browser checks, and regression checks without editing.",
		SystemPrompt: rolePrompt("QA / Verifier", "Verify the behavior from the user's point of view. Run checks and capture evidence. Do not edit files."),
		AllowedTools: readOnlyVerificationTools(),
		ContextScope: "Expected behavior, target route or command, verification policy, changed files, and failure history.",
		OutputSchema: "QA report with checks run, PASS/FAIL/PARTIAL verdict, failures, screenshots or command evidence when available.",
		SuccessCriteria: []string{
			"Declared verification policy is executed or explicitly blocked.",
			"Failures are reproducible from the report.",
		},
		OneShot:          true,
		Background:       true,
		DefaultIsolation: IsolationInplace,
	},
	"debugger": {
		Name:         "debugger",
		Role:         "Debugger",
		Description:  "Analyze failure logs, reproduce narrow failures, and propose a root-cause fix without editing.",
		SystemPrompt: rolePrompt("Debugger", "Start from failure evidence, reproduce narrowly, identify root cause, and recommend the smallest fix. Do not edit files."),
		AllowedTools: readOnlyVerificationTools(),
		ContextScope: "Failure log, failing command, stack trace, recent diff, and implicated files.",
		OutputSchema: "Debug report with reproduction command, root cause, implicated files, and proposed fix.",
		SuccessCriteria: []string{
			"Root cause is tied to evidence.",
			"Next fix step is concrete and testable.",
		},
		OneShot:          true,
		Background:       false,
		DefaultIsolation: IsolationInplace,
	},
	"integrator": {
		Name:         "integrator",
		Role:         "Integrator",
		Description:  "Combine worker outputs, verification evidence, and review feedback into a final integration report without editing.",
		SystemPrompt: rolePrompt("Integrator", "Synthesize worker, reviewer, and verifier outputs. Check consistency and produce the final handoff. Do not edit files."),
		AllowedTools: readOnlyVerificationTools(),
		ContextScope: "Approved worker reports, review report, verification results, worktree review, and durable state.",
		OutputSchema: "Integration report with accepted changes, rejected changes, final verification, limits, and next steps.",
		SuccessCriteria: []string{
			"Maker and checker outputs are both represented.",
			"Final status is supported by evidence.",
		},
		OneShot:          true,
		Background:       false,
		DefaultIsolation: IsolationInplace,
	},
}

func readOnlyPlanningTools() []string {
	return []string{"read_file", "grep", "glob", "bash", "agent_report"}
}

func readOnlyVerificationTools() []string {
	return []string{"read_file", "grep", "glob", "bash", "agent_report"}
}

func rolePrompt(role, instruction string) string {
	return "You are the " + role + " sub-agent in a goal-driven agent team.\n\n" +
		instruction + "\n\n" +
		"Rules:\n" +
		"- Stay inside the assigned scope.\n" +
		"- Preserve unrelated user work.\n" +
		"- Report blockers with exact evidence.\n" +
		"- Use agent_report before the final message.\n"
}

// AvailableWorkerTypes returns all built-in worker roles sorted by name.
func AvailableWorkerTypes() []WorkerType {
	keys := make([]string, 0, len(builtinWorkerTypes))
	for key := range builtinWorkerTypes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]WorkerType, 0, len(keys))
	for _, key := range keys {
		out = append(out, builtinWorkerTypes[key])
	}
	return out
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

// WorkerTypeHelp returns a compact role list suitable for model-facing prompts.
func WorkerTypeHelp() string {
	var b strings.Builder
	for i, item := range AvailableWorkerTypes() {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(item.Name)
		b.WriteString(": ")
		b.WriteString(item.Description)
	}
	return b.String()
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
	"helpme":        {},
	"inception":     {},
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
