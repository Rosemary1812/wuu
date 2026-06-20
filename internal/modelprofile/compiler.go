package modelprofile

import (
	"sort"
	"strings"

	"github.com/blueberrycongee/wuu/internal/capability"
)

// ProfileKey is the stable identifier of a tool-surface compilation.
// The same ProfileKey is used to build the JSON-RPC initialize result,
// the Settings debug view, and the prompt-fragment cache. Adding a
// new key is a deliberate act; renaming an existing key is a breaking
// change for downstream UIs.
type ProfileKey string

const (
	// ProfileOpenAICodex is the OpenAI / Codex harness. It exposes
	// apply_patch as the preferred editing primitive and treats
	// bash as the single terminal entry point. Reasoning budgets
	// are higher (harness iterates more before replying).
	ProfileOpenAICodex ProfileKey = "openai_codex"

	// ProfileOpenAIGPT covers OpenAI GPT reasoning models that ship
	// with the OpenAI Responses API. It uses the same patch-first
	// editing surface as Codex: apply_patch for file edits and bash
	// as the single terminal entry point.
	ProfileOpenAIGPT ProfileKey = "openai_gpt"

	// ProfileAnthropicClaude is the Anthropic Claude harness. It
	// uses exact-edit primitives (edit_file + write_file) as the
	// preferred file-change path and exposes bash for terminal
	// work. Prompt caching and long-horizon planning are first-class.
	ProfileAnthropicClaude ProfileKey = "anthropic_claude"

	// ProfileGeneric is the catch-all for BYOK providers (Gemini,
	// Kimi, DeepSeek, Qwen, local models, anything we have not
	// explicitly classified). It uses exact-edit primitives and a
	// conservative surface; local models in particular drop the
	// command.bash capability because the underlying profile
	// disables direct shell.
	ProfileGeneric ProfileKey = "generic"
)

// Compiler compiles a model profile into a tool surface.
type Compiler interface {
	Compile(p Profile) capability.Surface
}

// DefaultCompiler returns the built-in compiler. The compiler is
// stateless: callers should keep a single instance and reuse it.
type DefaultCompiler struct{}

// Compile implements Compiler.
func (DefaultCompiler) Compile(p Profile) capability.Surface {
	key := ResolveProfileKey(p)
	b := newBuilder(p, key)
	switch key {
	case ProfileOpenAICodex:
		compileOpenAICodex(b, p)
	case ProfileOpenAIGPT:
		compileOpenAIGPT(b, p)
	case ProfileAnthropicClaude:
		compileAnthropicClaude(b, p)
	default:
		compileGeneric(b, p)
	}
	b.sortCaps()
	return b.surface
}

// ResolveProfileKey returns the stable ProfileKey for a model
// profile. The key is the primary input to the surface compiler; the
// underlying Family and WriteMode fields refine its output but do
// not change which compiler variant runs.
func ResolveProfileKey(p Profile) ProfileKey {
	switch p.Family {
	case FamilyCodex:
		return ProfileOpenAICodex
	case FamilyGPT:
		return ProfileOpenAIGPT
	case FamilyClaude:
		return ProfileAnthropicClaude
	default:
		return ProfileGeneric
	}
}

// surfaceBuilder is a helper that incrementally builds a Surface.
// It enforces the rule that every visible tool and every
// capability lives in exactly one of (Tools, HiddenTools) and
// (Capabilities, HiddenCapabilities).
type surfaceBuilder struct {
	surface capability.Surface
	visible map[capability.Capability]struct{}
	hidden  map[capability.Capability]struct{}
}

func newSurfaceFor(p Profile, key ProfileKey) capability.Surface {
	return capability.Surface{
		ProfileName: string(key),
		Provider:    p.ProviderName,
		Model:       p.Model,
		Tools:       map[string]capability.Capability{},
		HiddenTools: map[string]capability.Capability{},
	}
}

func newBuilder(p Profile, key ProfileKey) *surfaceBuilder {
	return &surfaceBuilder{
		surface: newSurfaceFor(p, key),
		visible: map[capability.Capability]struct{}{},
		hidden:  map[capability.Capability]struct{}{},
	}
}

// addVisible registers a model-visible tool and its capability.
func (b *surfaceBuilder) addVisible(tool string, c capability.Capability) {
	b.surface.Tools[tool] = c
	b.addVisibleCapability(c)
}

func (b *surfaceBuilder) addVisibleCapability(c capability.Capability) {
	if _, ok := b.visible[c]; ok {
		return
	}
	b.visible[c] = struct{}{}
	b.surface.Capabilities = append(b.surface.Capabilities, c)
}

// addHidden registers a profile companion tool that is intentionally
// not model-visible under this surface.
func (b *surfaceBuilder) addHidden(tool string, c capability.Capability) {
	b.surface.HiddenTools[tool] = c
	if _, ok := b.hidden[c]; ok {
		return
	}
	if _, ok := b.visible[c]; ok {
		return
	}
	b.hidden[c] = struct{}{}
	b.surface.HiddenCapabilities = append(b.surface.HiddenCapabilities, c)
}

// skipHidden records that a capability exists on this surface but
// the runtime does not currently implement a tool for it. Used to
// keep the hidden capability list aligned with the visible one
// when a future revision will add the implementation back.
func (b *surfaceBuilder) skipHidden(c capability.Capability) {
	if _, ok := b.hidden[c]; ok {
		return
	}
	if _, ok := b.visible[c]; ok {
		return
	}
	b.hidden[c] = struct{}{}
	b.surface.HiddenCapabilities = append(b.surface.HiddenCapabilities, c)
}

// sortCaps sorts the capability slices for deterministic output.
func (b *surfaceBuilder) sortCaps() {
	sort.SliceStable(b.surface.Capabilities, func(i, j int) bool {
		return string(b.surface.Capabilities[i]) < string(b.surface.Capabilities[j])
	})
	sort.SliceStable(b.surface.HiddenCapabilities, func(i, j int) bool {
		return string(b.surface.HiddenCapabilities[i]) < string(b.surface.HiddenCapabilities[j])
	})
}

// ── Per-profile compilation ───────────────────────────────────────

const openaiPatchEditTool = "apply_patch"

func compileOpenAICodex(b *surfaceBuilder, p Profile) {
	addFileReadTools(b)
	addSearchTools(b)
	addBashFirstTools(b, p)
	addWebTools(b)
	addTaskTools(b)
	addMemoryTools(b)
	addPlanningTools(b)
	addWorkflowTools(b)
	addScheduleTools(b)
	addSkillTools(b)
	addExtensionTools(b)
	addOpenAICodexEditTools(b)
	addOpenAICodexPrompt(b)
}

func compileOpenAIGPT(b *surfaceBuilder, p Profile) {
	addFileReadTools(b)
	addSearchTools(b)
	addBashFirstTools(b, p)
	addWebTools(b)
	addTaskTools(b)
	addMemoryTools(b)
	addPlanningTools(b)
	addWorkflowTools(b)
	addScheduleTools(b)
	addSkillTools(b)
	addExtensionTools(b)
	addOpenAIGPTEditTools(b)
	addOpenAIGPTPrompt(b)
}

func compileAnthropicClaude(b *surfaceBuilder, p Profile) {
	addFileReadTools(b)
	addSearchTools(b)
	addBashFirstTools(b, p)
	addWebTools(b)
	addTaskTools(b)
	addMemoryTools(b)
	addPlanningTools(b)
	addWorkflowTools(b)
	addScheduleTools(b)
	addSkillTools(b)
	addExtensionTools(b)
	addClaudeEditTools(b)
	addClaudePrompt(b)
}

func compileGeneric(b *surfaceBuilder, p Profile) {
	addFileReadTools(b)
	addSearchTools(b)
	addBashFirstTools(b, p)
	addWebTools(b)
	addTaskTools(b)
	addMemoryTools(b)
	addPlanningTools(b)
	addWorkflowTools(b)
	addScheduleTools(b)
	addSkillTools(b)
	addExtensionTools(b)
	addGenericEditTools(b)
	addGenericPrompt(b, p)
}

// ── Shared capability assembly helpers ─────────────────────────────

func addFileReadTools(b *surfaceBuilder) {
	b.addVisible("read_file", capability.CapabilityFileRead)
	b.addVisible("list_files", capability.CapabilityFileList)
}

func addSearchTools(b *surfaceBuilder) {
	b.addVisible("grep", capability.CapabilitySearchGrep)
	b.addVisible("glob", capability.CapabilitySearchGlob)
	b.addVisible("ast_search", capability.CapabilitySearchAST)
	b.addVisible("semantic_search", capability.CapabilitySearchSemantic)
}

func addBashFirstTools(b *surfaceBuilder, p Profile) {
	if p.Workflow.AllowDirectShell {
		b.addVisible("bash", capability.CapabilityCommandBash)
		b.addVisibleCapability(capability.CapabilityCommandBackground)
	}
}

func addWebTools(b *surfaceBuilder) {
	b.addVisible("web_search", capability.CapabilityWebSearch)
	b.addVisible("web_fetch", capability.CapabilityWebFetch)
}

func addTaskTools(b *surfaceBuilder) {
	b.addVisible("spawn_agent", capability.CapabilityTaskSpawn)
	b.addVisible("send_message", capability.CapabilityTaskCommunicate)
	b.addVisible("followup_task", capability.CapabilityTaskCommunicate)
	b.addVisible("wait_agent", capability.CapabilityTaskManage)
	b.addVisible("await_agents", capability.CapabilityTaskManage)
	b.addVisible("close_agent", capability.CapabilityTaskManage)
	b.addVisible("list_agents", capability.CapabilityTaskManage)
	b.addVisible("agent_report", capability.CapabilityTaskManage)
}

func addMemoryTools(b *surfaceBuilder) {
	b.addVisible("session_memory", capability.CapabilityMemorySession)
	// Project-level memory tools are gated on the model surface
	// (not on whether a memory provider is attached). The
	// toolkit still hides individual memory_* tools when no
	// provider is configured; the capability is the contract
	// that says "project memory is a thing on this surface".
	b.addVisible("read_memory", capability.CapabilityMemoryProject)
	b.addVisible("write_memory", capability.CapabilityMemoryProject)
}

func addPlanningTools(b *surfaceBuilder) {
	b.addVisible("update_plan", capability.CapabilityPlan)
	b.addVisible("start_goal", capability.CapabilityGoal)
	b.addVisible("update_goal", capability.CapabilityGoal)
	b.addVisible("complete_goal", capability.CapabilityGoal)
	b.addVisible("goal_status", capability.CapabilityGoal)
}

func addWorkflowTools(b *surfaceBuilder) {
	b.addVisible("list_workflows", capability.CapabilityWorkflow)
	b.addVisible("load_workflow", capability.CapabilityWorkflow)
	b.addVisible("save_workflow", capability.CapabilityWorkflow)
	b.addVisible("list_agent_profiles", capability.CapabilityWorkflow)
	b.addVisible("create_agent_profile", capability.CapabilityWorkflow)
	b.addVisible("start_workflow", capability.CapabilityWorkflow)
	b.addVisible("run_workflow", capability.CapabilityWorkflow)
	b.addVisible("create_workflow", capability.CapabilityWorkflow)
	b.addVisible("workflow_control", capability.CapabilityWorkflow)
	b.addVisible("workflow_status", capability.CapabilityWorkflow)
}

func addScheduleTools(b *surfaceBuilder) {
	b.addVisible("schedule_cron", capability.CapabilitySchedule)
	b.addVisible("cancel_cron", capability.CapabilitySchedule)
	b.addVisible("list_cron", capability.CapabilitySchedule)
}

func addSkillTools(b *surfaceBuilder) {
	b.addVisible("load_skill", capability.CapabilitySkill)
	b.addVisible("tool_search", capability.CapabilityDiscovery)
}

func addExtensionTools(b *surfaceBuilder) {
	b.addVisible("report_listening_ports", capability.CapabilityPorts)
	// MCP has no stable built-in tool name because concrete MCP
	// tools are discovered at runtime. The visible capability says
	// this profile may load MCP tools through tool_search; the tools
	// themselves are still deferred and policy-gated.
	b.addVisibleCapability(capability.CapabilityMCP)
}

func addOpenAICodexEditTools(b *surfaceBuilder) {
	b.addVisible(openaiPatchEditTool, capability.CapabilityFileEdit)
}

func addOpenAIGPTEditTools(b *surfaceBuilder) {
	b.addVisible(openaiPatchEditTool, capability.CapabilityFileEdit)
}

func addClaudeEditTools(b *surfaceBuilder) {
	b.addVisible("edit_file", capability.CapabilityFileEdit)
	b.addVisible("write_file", capability.CapabilityFileEdit)
}

func addGenericEditTools(b *surfaceBuilder) {
	b.addVisible("edit_file", capability.CapabilityFileEdit)
	b.addVisible("write_file", capability.CapabilityFileEdit)
}

// ── Prompt fragments ──────────────────────────────────────────────

// sharedTail is the common closer every profile fragment shares. It
// tells the model that permission prompts come from the harness, not
// from a chat message, and it forbids the "should I continue running
// tests?" pattern that wastes turns.
const sharedTail = `

Permission and approval:
- Permissions, approvals, and audit decisions are made by the Wuu harness and the user-facing approval UI. Do not ask the user chat-side questions like "should I continue running tests?", "do you want me to commit?", or "may I run the build?" — those are policy decisions and will be surfaced as system prompts when relevant.
- The system tells you the active permission profile and the tool surface available to you. Trust it; do not invent restrictions that are not in the system prompt.`

const bashTerminalGuidance = `

Command and process discipline:
- Use bash for every terminal operation: tests, lint, type checks, builds, git operations, package manager commands, and arbitrary scripts. There is no separate test-runner, git, or background-process tool on this surface; do not invent one.
- For JavaScript projects, prefer package scripts such as npm test or npm run typecheck. If you only know the runner command such as npx vitest, still use bash and let the harness approval policy decide whether it may run.
- For repository history work, inspect git status, git diff, git diff --cached when staged files exist, and recent git log before committing. Stage only intended files with explicit paths, unstage mistakes with explicit paths, create commits with git commit -m, and push only when the user explicitly requested a remote write.
- Never use broad staging, sensitive credential paths, destructive git commands, force push, git config mutation, hook-skipping flags, or interactive/editor-driven git flows unless the user explicitly requested that exact action and the runtime permits it.
- For long-lived dev servers, watchers, and background processes, use bash with an explicit timeout when you need bounded logs or readiness output. Do not background commands with "&". After a managed process reports a localhost port, use report_listening_ports so the desktop can preview it.`

func addOpenAICodexPrompt(b *surfaceBuilder) {
	b.surface.SystemFragment = strings.TrimSpace(`
[Tool surface: openai_codex]
You are running under the OpenAI / Codex harness. Your editing primitive is apply_patch. Use it for every file change — create new files, update existing files, and remove files via *** Add File / *** Update File / *** Delete File blocks inside a single *** Begin Patch / *** End Patch envelope.

All terminal work is unified under the bash tool. The internal capability is command.bash, and the runtime routes permission checks against it.

Use read_file before editing a file so the patch's context anchors match the on-disk content. Use grep / glob / ast_search / semantic_search to find the code you need to change.
` + bashTerminalGuidance + sharedTail)
}

func addOpenAIGPTPrompt(b *surfaceBuilder) {
	b.surface.SystemFragment = strings.TrimSpace(`
[Tool surface: openai_gpt]
You are running under the OpenAI GPT harness. Your editing primitive is apply_patch. Use it for every file change — create new files, update existing files, and remove files via *** Add File / *** Update File / *** Delete File blocks inside a single *** Begin Patch / *** End Patch envelope.

Terminal work is unified under the bash tool.
` + bashTerminalGuidance + sharedTail)
}

func addClaudePrompt(b *surfaceBuilder) {
	b.surface.SystemFragment = strings.TrimSpace(`
[Tool surface: anthropic_claude]
You are running under the Anthropic Claude harness. Your file editing primitives are read_file, edit_file, and write_file. Call read_file first to anchor the old_string in edit_file, and use write_file only for whole-file replacement (e.g. newly created files or generated outputs).

Terminal work goes through the bash tool.
` + bashTerminalGuidance + sharedTail)
}

func addGenericPrompt(b *surfaceBuilder, p Profile) {
	if p.Family == FamilyLocal || !p.Workflow.AllowDirectShell {
		b.surface.SystemFragment = strings.TrimSpace(`
[Tool surface: generic (no command execution)]
You are running under a generic BYOK profile. File work uses read_file, edit_file (with exact old_string match — call read_file first to anchor it), and write_file for whole-file replacement.

This profile does not expose command execution. If a task requires command-only work, explain that the active model profile cannot run those operations and recommend switching to a profile that allows command execution.
` + sharedTail)
		return
	}
	b.surface.SystemFragment = strings.TrimSpace(`
[Tool surface: generic]
You are running under a generic BYOK profile. File work uses read_file, edit_file (exact old_string match — call read_file first), and write_file (whole-file replacement).

Terminal work goes through the bash tool.
` + bashTerminalGuidance + sharedTail)
}
