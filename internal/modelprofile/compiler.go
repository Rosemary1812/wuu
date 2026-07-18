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

// SurfaceKind identifies which runtime role a tool surface is compiled for.
// It is a closed set of four values so the compiler can key decisions off a
// single dimension instead of an open pair of booleans (a worker that is also
// "named" is not a valid combination). Ultra workers are kept distinct from
// main agents because they combine task orchestration with the worker-only
// agent_report handoff and never receive helpme.
type SurfaceKind int

const (
	// SurfaceWorker is a pure executor subagent surface: no task
	// orchestration or recovery tools, only the agent_report handoff.
	SurfaceWorker SurfaceKind = iota
	// SurfaceUltraWorker is an orchestrating subagent surface. It keeps the
	// worker-only agent_report handoff, adds the task orchestration suite, and
	// deliberately omits the main-agent-only helpme recovery tool.
	SurfaceUltraWorker
	// SurfaceMain is the ordinary project main-agent brain surface: it
	// carries the task-orchestration suite plus helpme.
	SurfaceMain
	// SurfaceNamed is a long-lived resident/named agent surface. It compiles
	// the same base orchestration surface as SurfaceMain; resident task-rail
	// tools are patched at runtime when the resident identity is enabled.
	SurfaceNamed
)

// orchestrates reports whether a surface kind gets the task orchestration
// suite. Main, named, and Ultra workers orchestrate; ordinary workers do not.
func (k SurfaceKind) orchestrates() bool {
	return k != SurfaceWorker
}

func (k SurfaceKind) isWorker() bool {
	return k == SurfaceWorker || k == SurfaceUltraWorker
}

func (k SurfaceKind) includesHelpme() bool {
	return k == SurfaceMain || k == SurfaceNamed
}

func (k SurfaceKind) includesSessionWorkspace() bool {
	return k == SurfaceMain
}

// Compiler compiles a model profile into a tool surface. Compile is given a
// SurfaceKind so it can decide whether the surface should advertise, hide, or
// omit orchestration and recovery tools (the spawn_agent suite, helpme) and
// worker-only handoff tools such as agent_report. The
// surface is therefore consistent with the runtime boundary instead of being
// filtered downstream. The named value remains distinct for resident/named
// callers while compiling the same base surface as SurfaceMain.
type Compiler interface {
	Compile(p Profile, kind SurfaceKind) capability.Surface
}

// DefaultCompiler returns the built-in compiler. The compiler is
// stateless: callers should keep a single instance and reuse it.
type DefaultCompiler struct{}

// Compile implements Compiler. The SurfaceKind controls the orchestration
// boundary: execution capability is equal on all surfaces, but orchestration
// belongs to the brain (the session main agent and resident named agents,
// which clone the main surface). Main and named
// surfaces get the task-orchestration suite plus helpme; ordinary worker
// surfaces are pure executors and keep only agent_report; Ultra workers get
// task orchestration plus agent_report without helpme.
func (DefaultCompiler) Compile(p Profile, kind SurfaceKind) capability.Surface {
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
	if kind.orchestrates() {
		addTaskTools(b)
	}
	if kind.includesHelpme() {
		addHelpmeTool(b)
	}
	if kind.includesSessionWorkspace() {
		addSessionWorkspaceTool(b)
	}
	if kind.isWorker() {
		addWorkerReportTool(b)
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
// It enforces the rule that every model-visible tool name lives in either the
// direct or deferred exposure bucket. A broad capability can appear in both
// buckets when different tools under it have different lifecycles.
type surfaceBuilder struct {
	surface  capability.Surface
	visible  map[capability.Capability]struct{}
	deferred map[capability.Capability]struct{}
}

func newSurfaceFor(p Profile, key ProfileKey) capability.Surface {
	return capability.Surface{
		ProfileName:   string(key),
		Provider:      p.ProviderName,
		Model:         p.Model,
		Tools:         map[string]capability.Capability{},
		DeferredTools: map[string]capability.Capability{},
		HiddenTools:   map[string]capability.Capability{},
	}
}

func newBuilder(p Profile, key ProfileKey) *surfaceBuilder {
	return &surfaceBuilder{
		surface:  newSurfaceFor(p, key),
		visible:  map[capability.Capability]struct{}{},
		deferred: map[capability.Capability]struct{}{},
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

// addDeferred registers a tool that is available only after
// tool_search loads its schema.
func (b *surfaceBuilder) addDeferred(tool string, c capability.Capability) {
	b.surface.DeferredTools[tool] = c
	b.addDeferredCapability(c)
}

func (b *surfaceBuilder) addDeferredCapability(c capability.Capability) {
	if _, ok := b.deferred[c]; ok {
		return
	}
	b.deferred[c] = struct{}{}
	b.surface.DeferredCapabilities = append(b.surface.DeferredCapabilities, c)
}

// sortCaps sorts the capability slices for deterministic output.
func (b *surfaceBuilder) sortCaps() {
	sort.SliceStable(b.surface.Capabilities, func(i, j int) bool {
		return string(b.surface.Capabilities[i]) < string(b.surface.Capabilities[j])
	})
	sort.SliceStable(b.surface.DeferredCapabilities, func(i, j int) bool {
		return string(b.surface.DeferredCapabilities[i]) < string(b.surface.DeferredCapabilities[j])
	})
}

// ── Per-profile compilation ───────────────────────────────────────

const openaiPatchEditTool = "apply_patch"

func compileOpenAICodex(b *surfaceBuilder, p Profile) {
	addFileReadTools(b)
	addSearchTools(b)
	addBashFirstTools(b, p)
	addWebTools(b)
	addBrowserTools(b)
	addMemoryTools(b)
	addSessionTools(b)
	addPlanningTools(b)
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
	addBrowserTools(b)
	addMemoryTools(b)
	addSessionTools(b)
	addPlanningTools(b)
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
	addBrowserTools(b)
	addMemoryTools(b)
	addSessionTools(b)
	addPlanningTools(b)
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
	addBrowserTools(b)
	addMemoryTools(b)
	addSessionTools(b)
	addPlanningTools(b)
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
	// Keep legacy search capabilities available for MCP tools without
	// registering low-use built-in AST or semantic search tools.
	b.addDeferredCapability(capability.CapabilitySearchAST)
	b.addDeferredCapability(capability.CapabilitySearchSemantic)
}

func addBashFirstTools(b *surfaceBuilder, p Profile) {
	if p.Execution.AllowDirectShell {
		b.addVisible("bash", capability.CapabilityCommandBash)
		b.addVisibleCapability(capability.CapabilityCommandBackground)
	}
}

func addWebTools(b *surfaceBuilder) {
	b.addVisible("web_search", capability.CapabilityWebSearch)
	b.addVisible("web_fetch", capability.CapabilityWebFetch)
}

// addBrowserTools defers the embedded browser tool on every profile. It is
// registered unconditionally and never reads the environment: the compiler must
// stay pure so surface tests do not drift with WUU_ENABLE_BROWSER. Runtime
// gating lives entirely in the toolkit's disabledTools (default off, flipped by
// SetBrowserEnabled), so a deferred entry here is inert until both the surface
// exposes it AND the toolkit enables it.
func addBrowserTools(b *surfaceBuilder) {
	b.addDeferred("wuu_browser", capability.CapabilityBrowser)
}

// addTaskTools registers the orchestration suite: spawn_agent as a visible
// tool plus the deferred subagent management tools. Main, named, and Ultra
// worker surfaces receive it; ordinary worker surfaces omit it. Runtime
// defense-in-depth remains in internal/agentcontrol/worker_types.go.
func addTaskTools(b *surfaceBuilder) {
	b.addVisible("spawn_agent", capability.CapabilityTaskSpawn)
	b.addDeferred("send_message", capability.CapabilityTaskCommunicate)
	b.addDeferred("close_agent", capability.CapabilityTaskManage)
}

// addHelpmeTool registers the main-agent-only HelpMe recovery tool. It
// is called from DefaultCompiler.Compile only when forMainAgent is true;
// worker surfaces intentionally omit helpme so the model never sees a
// recovery tool it cannot use. Runtime defense-in-depth (DisallowedTools
// in internal/agentcontrol/worker_types.go and the path check in
// HelpMeTool.Execute) is unchanged.
func addHelpmeTool(b *surfaceBuilder) {
	b.addVisible("helpme", capability.CapabilityTaskSpawn)
}

func addWorkerReportTool(b *surfaceBuilder) {
	b.addVisible("agent_report", capability.CapabilityTaskManage)
}

func addMemoryTools(b *surfaceBuilder) {
	b.addDeferred("session_memory", capability.CapabilityMemorySession)
	// Durable notebook memory uses file tools; the retired indexed-memory
	// tools are no longer registered or projected onto model surfaces.
}

func addSessionTools(b *surfaceBuilder) {
	b.addDeferred("thread_get", capability.CapabilitySessionLookup)
}

func addSessionWorkspaceTool(b *surfaceBuilder) {
	b.addDeferred("set_session_workspace", capability.CapabilitySessionWorkspace)
}

func addPlanningTools(b *surfaceBuilder) {
	b.addVisible("update_plan", capability.CapabilityPlan)
	b.addDeferred("goal", capability.CapabilityGoal)
}

func addScheduleTools(b *surfaceBuilder) {
	b.addDeferred("cron", capability.CapabilitySchedule)
}

func addSkillTools(b *surfaceBuilder) {
	b.addVisible("load_skill", capability.CapabilitySkill)
	b.addVisible("tool_search", capability.CapabilityDiscovery)
}

func addExtensionTools(b *surfaceBuilder) {
	// MCP has no stable built-in tool name because concrete MCP
	// tools are discovered at runtime. The deferred capability says
	// this profile may load MCP tools through tool_search; the tools
	// themselves are still deferred and guard-gated.
	b.addDeferredCapability(capability.CapabilityMCP)
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

// sharedTail is the common closer every profile fragment shares. It teaches
// the allow/deny authority model and forbids chat-side permission questions
// that waste turns.
const sharedTail = `

Agent authority:
- Runtime authority decisions are allow/deny only. Inside the reachable boundary, act without an approval step. Do not ask chat-side questions like "should I continue running tests?", "do you want me to commit?", or "may I run the build?" when the user's request already calls for the work.
- File tools enforce the workspace boundary. If a tool returns error_kind=boundary_denied, explain that the target is outside the reachable roots and ask the user to add that directory as a workspace if the task needs it.`

const bashTerminalGuidance = `

Command and process discipline:
- Use bash for every terminal operation: tests, lint, type checks, builds, git operations, package manager commands, and arbitrary scripts. There is no separate test-runner, git, or background-process tool on this surface; do not invent one.
- Bash is not a strong path sandbox. Use its configured working directory and do not use absolute paths to route around the workspace boundary.
- Never use broad staging, sensitive credential paths, destructive git commands, force push, git config mutation, hook-skipping flags, commit amends, or interactive/editor-driven git flows unless the user explicitly requested that exact action.`

func addOpenAICodexPrompt(b *surfaceBuilder) {
	b.surface.SystemFragment = strings.TrimSpace(`
[Tool surface: openai_codex]
You are running under the OpenAI / Codex harness. Your editing primitive is apply_patch. Use it for every file change — create new files, update existing files, and remove files via *** Add File / *** Update File / *** Delete File blocks inside a single *** Begin Patch / *** End Patch envelope.

All terminal work is unified under the bash tool. The internal capability is command.bash; the runtime applies non-path authority gates to the tool call while bash itself runs from its configured working directory.

Use read_file before editing a file so the patch's context anchors match the on-disk content. Use visible search tools such as grep and glob to find the code you need to change.
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
	if p.Family == FamilyLocal || !p.Execution.AllowDirectShell {
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
