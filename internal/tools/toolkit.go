package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/goalruntime"
	"github.com/blueberrycongee/wuu/internal/mcp"
	"github.com/blueberrycongee/wuu/internal/memory/store"
	"github.com/blueberrycongee/wuu/internal/modelprofile"
	proc "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/skills"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/stringutil"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

const (
	defaultShellTimeoutSeconds = 300
	maxShellTimeoutSeconds     = 3600
	defaultMaxFileBytes        = 256 * 1024
	defaultMaxEntries          = 1000
	// Per-tool output size limits (in bytes). Shell/grep produce verbose,
	// low-density output and get a tighter cap; other tools use a generous
	// default.
	maxShellOutputBytes = 30 * 1024  // 30 KB
	maxGrepOutputBytes  = 20 * 1024  // 20 KB
	maxToolOutputBytes  = 100 * 1024 // 100 KB — general cap for other tools
)

// Toolkit executes local coding tools for the agent. It satisfies
// agent.ToolExecutor via Definitions() + Execute().
//
// Internally it holds an Env (shared runtime state) and a Registry
// (all registered tools). The old switch-case dispatch is replaced by
// registry lookup.
type Toolkit struct {
	env                    *Env
	registry               *Registry
	disabledTools          map[string]struct{}
	exposureMu             sync.RWMutex
	activatedDeferredTools map[string]struct{}
	toolPolicy             ToolPolicy
	permissionBoundary     PermissionBoundary
	extensionSurfacePolicy ExtensionSurfacePolicy
	autoModeClassifier     AutoModeClassifier
	approvalReviewer       ToolApprovalReviewer
	approvalStore          *ToolApprovalStore
	permissionRulesMu      sync.RWMutex
	permissionRules        ToolPermissionRuleSet
	permissionSessionRules ToolPermissionRuleSet
	// mcpManager, when set, exposes MCP server tools alongside built-in
	// tools. MCP tools are appended after built-ins to preserve prompt
	// cache stability (the built-in prefix stays constant).
	mcpManager *mcp.Manager

	// activeProfileMu guards activeProfile and activeSurface. Reads
	// from Definitions() and Execute() take the RLock; SetActiveProfile
	// takes the Lock.
	activeProfileMu sync.RWMutex
	// activeProfile is the most recently installed ModelProfile. The
	// zero value means "no profile compiled yet" — Definitions() falls
	// back to the legacy direct-tool surface in that case.
	activeProfile modelprofile.Profile
	// activeSurface is the compiled surface for activeProfile. It is
	// the authoritative source for which tool names are visible to
	// the model under the current profile.
	activeSurface capability.Surface
}

// New creates a tool executor rooted in a workspace.
func New(rootDir string) (*Toolkit, error) {
	if strings.TrimSpace(rootDir) == "" {
		return nil, errors.New("root directory is required")
	}
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root directory: %w", err)
	}
	if ev, err := filepath.EvalSymlinks(abs); err == nil {
		abs = ev
	}
	wuuHome, err := statepath.Home("")
	if err != nil {
		return nil, fmt.Errorf("resolve wuu home: %w", err)
	}
	stateDir, err := statepath.WorkspaceDir(wuuHome, abs)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace state directory: %w", err)
	}

	env := &Env{
		RootDir:  abs,
		StateDir: stateDir,
	}
	t := &Toolkit{env: env, autoModeClassifier: DefaultAutoModeClassifier{}, approvalStore: NewToolApprovalStore()}
	t.rebuildRegistry()
	t.SetEditToolMode(EditToolModeText)
	return t, nil
}

// CloneForRoot returns a toolkit with the same configured dependencies but a
// fresh per-session Env state. It is used by desktop app-server threads so
// concurrent conversations do not share mutable tool state such as SessionID,
// read tracking, plans, or telemetry.
func (t *Toolkit) CloneForRoot(rootDir string) (*Toolkit, error) {
	if t == nil || t.env == nil {
		return nil, errors.New("toolkit is not initialized")
	}
	cloneRoot := strings.TrimSpace(rootDir)
	if cloneRoot == "" {
		cloneRoot = t.env.RootDir
	}
	abs, err := filepath.Abs(cloneRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve root directory: %w", err)
	}
	if ev, err := filepath.EvalSymlinks(abs); err == nil {
		abs = ev
	}
	// Build a fresh Env from the source toolkit's configured dependencies.
	// We must NOT do `env := *t.env`: Env embeds testRunState, which holds a
	// sync.RWMutex, and the sync package contract forbids copying a Mutex or
	// RWMutex after first use (go vet's copylocks analyzer enforces this).
	// The lock-bearing per-session state fields (readState, testState,
	// planState, webState, toolTelemetry) stay zero so each cloned session
	// owns independent mutable state, matching the original intent.
	env := Env{
		RootDir:             abs,
		StateDir:            t.env.StateDir,
		PermissionProfile:   t.env.PermissionProfile,
		SessionID:           t.env.SessionID,
		SessionDir:          t.env.SessionDir,
		GoalRuntime:         t.env.GoalRuntime,
		AgentID:             t.env.AgentID,
		AgentPath:           t.env.AgentPath,
		ProcessMgr:          t.env.ProcessMgr,
		AgentControl:        t.env.AgentControl,
		Skills:              t.env.Skills,
		Workflows:           t.env.Workflows,
		OnFileChanged:       t.env.OnFileChanged,
		OnPlanUpdated:       t.env.OnPlanUpdated,
		OnPortsReported:     t.env.OnPortsReported,
		Memory:              t.env.Memory,
		MemoryCharLimit:     t.env.MemoryCharLimit,
		UserMemoryCharLimit: t.env.UserMemoryCharLimit,
	}

	clone := &Toolkit{
		env:                    &env,
		toolPolicy:             t.toolPolicy,
		permissionBoundary:     t.permissionBoundary,
		extensionSurfacePolicy: t.extensionSurfacePolicy,
		autoModeClassifier:     t.autoModeClassifier,
		approvalReviewer:       t.approvalReviewer,
		approvalStore:          NewToolApprovalStore(),
		permissionRules:        t.PermissionRules(),
		mcpManager:             t.mcpManager,
	}
	t.activeProfileMu.RLock()
	clone.activeProfile = t.activeProfile
	clone.activeSurface = cloneSurface(t.activeSurface)
	t.activeProfileMu.RUnlock()
	clone.env.ActiveSurface = cloneSurface(clone.activeSurface)
	if len(t.disabledTools) > 0 {
		clone.disabledTools = make(map[string]struct{}, len(t.disabledTools))
		for name := range t.disabledTools {
			clone.disabledTools[name] = struct{}{}
		}
	}
	clone.activatedDeferredTools = t.cloneActivatedDeferredTools()
	clone.rebuildRegistry()
	return clone, nil
}

// rebuildRegistry constructs the tool registry from the current Env.
// Called at construction and whenever dependencies change.
func (t *Toolkit) rebuildRegistry() {
	e := t.env
	registered := []Tool{
		// File operations
		NewReadFileTool(e),
		NewWriteFileTool(e),
		NewListFilesTool(e),
		NewEditFileTool(e),
		NewApplyPatchTool(e),
		NewCheckpointTool(e),
		// Search
		NewRepoMapTool(e),
		NewGrepTool(e),
		NewGlobTool(e),
		NewASTSearchTool(e),
		NewSemanticSearchTool(e),
		// Shell
		NewShellTool(e),
		// Bash is the new unified command entry point emitted by the
		// model profile compiler. The legacy run_shell tool stays
		// registered as an internal / advanced implementation that
		// the surface compiler can keep hidden.
		NewBashTool(e),
		NewRunTestTool(e),
		// Git
		NewGitTool(e),
		// Web
		NewWebSearchTool(e),
		NewWebFetchTool(e),
		// Skills
		NewLoadSkillTool(e),
		// Durable session/workspace memory
		NewSessionMemoryTool(e),
		// Session/thread lookup (used after right-click "copy ID" on the
		// desktop session tree — agents receive a thread ID and resolve it
		// back to the full conversation via this tool).
		NewThreadGetTool(e),
		// Goals
		NewStartGoalTool(e),
		NewUpdateGoalTool(e),
		NewCompleteGoalTool(e),
		NewGoalStatusTool(e),
		// Workflows
		NewListWorkflowsTool(e),
		NewLoadWorkflowTool(e),
		NewSaveWorkflowTool(e),
		NewListAgentProfilesTool(e),
		NewCreateAgentProfileTool(e),
		NewStartWorkflowTool(e),
		NewRunWorkflowTool(e),
		NewCreateWorkflowTool(e),
		NewWorkflowControlTool(e),
		NewWorkflowStatusTool(e),
		// Planning
		NewUpdatePlanTool(e),
		// Agent orchestration
		NewSpawnAgentTool(e),
		NewSendAgentMessageTool(e),
		NewFollowupTaskTool(e),
		NewWaitAgentTool(e),
		NewAwaitAgentsTool(e),
		NewCloseAgentTool(e),
		NewListAgentsTool(e),
		NewAgentReportTool(e),
		// Process management
		NewStartProcessTool(e),
		NewListProcessesTool(e),
		NewStopProcessTool(e),
		NewReadProcessOutputTool(e),
		NewWriteStdinTool(e),
		// Cron scheduling
		NewScheduleCronTool(e),
		NewCancelCronTool(e),
		NewListCronTool(e),
		// Dev-server surface for the desktop preview
		NewReportListeningPortsTool(e),
		// Deferred tool discovery
		NewToolSearchTool(t),
	}
	// Memory tools stay in the internal registry, but Definitions hides them
	// unless a memory provider is attached for this agent profile.
	registered = append(registered, NewMemoryTools(e)...)
	t.registry = NewRegistry(registered...)
}

// ── Dependency setters ─────────────────────────────────────────────

// SetAgentControl attaches the shared agent control runtime.
func (t *Toolkit) SetAgentControl(c *agentcontrol.AgentControl) {
	t.env.AgentControl = c
}

// SetProcessManager attaches the process manager.
func (t *Toolkit) SetProcessManager(m *proc.Manager) {
	t.env.ProcessMgr = m
}

// SetStateDir sets the workspace-scoped runtime state directory.
func (t *Toolkit) SetStateDir(dir string) {
	t.env.StateDir = strings.TrimSpace(dir)
}

// SetSkills attaches the discovered skills.
func (t *Toolkit) SetSkills(s []skills.Skill) {
	t.env.Skills = s
}

// SetWorkflows attaches the discovered workflow definitions.
func (t *Toolkit) SetWorkflows(w []workflow.Definition) {
	t.env.Workflows = w
}

// Skills returns the currently registered skills (read-only).
func (t *Toolkit) Skills() []skills.Skill {
	return t.env.Skills
}

// Workflows returns the currently registered workflow definitions (read-only).
func (t *Toolkit) Workflows() []workflow.Definition {
	return t.env.Workflows
}

// SetMemory attaches the memory store provider. Definitions exposes the
// memory tools only while a provider is attached.
func (t *Toolkit) SetMemory(p store.Provider) {
	t.env.Memory = p
}

func (t *Toolkit) SetMemoryLimits(memoryLimit, userLimit int) {
	if t == nil || t.env == nil {
		return
	}
	t.env.MemoryCharLimit = memoryLimit
	t.env.UserMemoryCharLimit = userLimit
}

func (t *Toolkit) MemoryLimits() (memoryLimit, userLimit int) {
	if t == nil || t.env == nil {
		return 0, 0
	}
	return t.env.MemoryCharLimit, t.env.UserMemoryCharLimit
}

// Memory returns the currently attached memory store provider, if any.
func (t *Toolkit) Memory() store.Provider {
	if t == nil || t.env == nil {
		return nil
	}
	return t.env.Memory
}

// SetSessionID sets the current session ID.
func (t *Toolkit) SetSessionID(id string) {
	t.env.SessionID = id
}

// SessionID returns the session currently bound to this toolkit.
func (t *Toolkit) SessionID() string {
	if t == nil || t.env == nil {
		return ""
	}
	return t.env.SessionID
}

// SetSessionDir sets the session directory for result budgeting.
func (t *Toolkit) SetSessionDir(dir string) {
	t.env.SessionDir = dir
}

// SessionDir returns the session artifact directory currently bound to this toolkit.
func (t *Toolkit) SessionDir() string {
	if t == nil || t.env == nil {
		return ""
	}
	return t.env.SessionDir
}

// SetGoalRuntime attaches the thread-scoped active Goal runtime.
func (t *Toolkit) SetGoalRuntime(runtime *goalruntime.Runtime) {
	t.env.GoalRuntime = runtime
}

// GoalRuntime returns the thread-scoped active Goal runtime, if any.
func (t *Toolkit) GoalRuntime() *goalruntime.Runtime {
	if t == nil || t.env == nil {
		return nil
	}
	return t.env.GoalRuntime
}

// SetAgentIdentity sets the current agent identity for relative agent-path
// resolution inside orchestration tools.
func (t *Toolkit) SetAgentIdentity(id, path string) {
	t.env.AgentID = strings.TrimSpace(id)
	t.env.AgentPath = strings.TrimSpace(path)
}

// SetOnFileChanged sets the callback fired after write_file/edit_file
// successfully modifies a file. Used to dispatch FileChanged hooks.
func (t *Toolkit) SetOnFileChanged(fn func(absPath string)) {
	t.env.OnFileChanged = fn
}

// SetOnPlanUpdated sets the callback fired after update_plan successfully
// stores a new snapshot.
func (t *Toolkit) SetOnPlanUpdated(fn func(snapshot PlanSnapshot)) {
	t.env.OnPlanUpdated = fn
}

// SetOnPortsReported sets the callback fired after report_listening_ports
// successfully validates the agent's port list. The desktop wires this to
// push thread state into the GUI's in-app browser preview.
func (t *Toolkit) SetOnPortsReported(fn func(ports []int)) {
	t.env.OnPortsReported = fn
}

// SetMCPManager attaches the MCP manager so its tools are exposed to the agent.
func (t *Toolkit) SetMCPManager(m *mcp.Manager) {
	t.mcpManager = m
}

func (t *Toolkit) MCPManager() *mcp.Manager {
	if t == nil {
		return nil
	}
	return t.mcpManager
}

// SetToolPolicy installs the runtime policy used before executing known tools.
func (t *Toolkit) SetToolPolicy(policy ToolPolicy) {
	t.toolPolicy = policy
}

// SetPermissionBoundary installs the hard runtime boundary below tool policy.
// The zero value leaves the legacy unrestricted behavior in place.
func (t *Toolkit) SetPermissionBoundary(boundary PermissionBoundary) {
	t.permissionBoundary = boundary
	if t != nil && t.env != nil {
		t.env.PermissionProfile = normalizePermissionBoundaryProfile(boundary.Profile)
	}
}

func (t *Toolkit) bypassToolHardProtections() bool {
	return t != nil && t.env != nil && t.env.BypassToolHardProtections()
}

// SetExtensionSurfacePolicy installs the runtime trust policy for extension
// backed tools such as MCP, skills, and workflows. The zero value allows all.
func (t *Toolkit) SetExtensionSurfacePolicy(policy ExtensionSurfacePolicy) {
	t.extensionSurfacePolicy = policy
}

// SetAutoModeClassifier installs the reviewer used by explicit auto-classify
// policy actions. Passing nil makes those actions fail closed for non-low-risk calls.
func (t *Toolkit) SetAutoModeClassifier(classifier AutoModeClassifier) {
	t.autoModeClassifier = classifier
}

// SetToolApprovalReviewer installs the reviewer used when policy requires
// approval. A nil reviewer preserves the legacy fail-closed behavior: the
// request is written as an approval artifact and returned to the model.
func (t *Toolkit) SetToolApprovalReviewer(reviewer ToolApprovalReviewer) {
	t.approvalReviewer = reviewer
	if t.approvalStore == nil {
		t.approvalStore = NewToolApprovalStore()
	}
}

// ApprovalStore returns the session-wide approval cache the toolkit uses
// to skip the reviewer for repeated identical tool calls. Returns nil
// only when the toolkit itself is nil. Callers (e.g. the LLM-driven
// guardian reviewer) can pass the result through to keep their own
// short-circuit on top of the toolkit's already-failed attempts.
func (t *Toolkit) ApprovalStore() *ToolApprovalStore {
	if t == nil {
		return nil
	}
	return t.approvalStore
}

// AgentControl returns the attached agent control runtime, or nil.
func (t *Toolkit) AgentControl() *agentcontrol.AgentControl {
	return t.env.AgentControl
}

// CurrentPlan returns the latest update_plan snapshot stored by this toolkit.
func (t *Toolkit) CurrentPlan() (PlanSnapshot, bool) {
	if t == nil || t.env == nil {
		return PlanSnapshot{}, false
	}
	return t.env.planState.get()
}

// ── Tool disabling ─────────────────────────────────────────────────

// DisableTools removes specific tools from this toolkit instance.
func (t *Toolkit) DisableTools(names ...string) {
	if t.disabledTools == nil {
		t.disabledTools = make(map[string]struct{}, len(names))
	}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		t.disabledTools[n] = struct{}{}
	}
}

// EnableTools re-enables specific tools that were previously disabled.
func (t *Toolkit) EnableTools(names ...string) {
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		delete(t.disabledTools, n)
	}
}

func (t *Toolkit) isToolDisabled(name string) bool {
	if len(t.disabledTools) == 0 {
		return false
	}
	_, ok := t.disabledTools[name]
	return ok
}

// ── ToolExecutor interface ─────────────────────────────────────────

// Definitions returns JSON-schema tool definitions for every enabled
// tool the agent can call.
//
// When an active profile has been installed via SetActiveProfile,
// the visible set is the per-profile compiled surface. Without an
// active profile the toolkit falls back to the legacy direct-tool
// surface: every direct tool the registry knows about, in the same
// order the legacy code returned them. The legacy fallback exists so
// callers that have not migrated yet (replay, debugging, internal
// admin tools) keep working.
func (t *Toolkit) Definitions() []providers.ToolDefinition {
	all := t.registry.Definitions()
	surface := t.activeCompiledSurface()
	hasSurface := surface.ProfileName != ""
	stable := make([]providers.ToolDefinition, 0, len(all))
	dynamic := make([]providers.ToolDefinition, 0)
	for _, d := range all {
		if isMemoryToolName(d.Name) && memoryProvider(t.env) == nil {
			continue
		}
		if hasSurface && t.isToolDisabled(d.Name) {
			continue
		}
		if hasSurface {
			// Surface is the authoritative whitelist: tools the
			// surface includes are visible regardless of the legacy
			// toolExposure state. Explicit session disables still
			// win, so worker filters and edit-mode switches cannot
			// leak tools back into the model-visible list. The
			// compiler can promote a Hidden-by-default tool (e.g.
			// apply_patch on Codex) into a model-visible entry by
			// listing it in surface.Tools.
			if !activeSurfaceAllowsKnownTool(surface, t.registry.Lookup(d.Name)) {
				continue
			}
			if isDeferredByDefault(d.Name) {
				if !t.isDeferredToolActive(d.Name) {
					continue
				}
				d.CacheStable = false
				dynamic = append(dynamic, d)
				continue
			}
			d.CacheStable = true
			stable = append(stable, d)
			continue
		}
		if t.toolExposure(d.Name) == ToolExposureDirect {
			if isDeferredByDefault(d.Name) {
				d.CacheStable = false
				dynamic = append(dynamic, d)
				continue
			}
			d.CacheStable = true
			stable = append(stable, d)
		}
	}
	out := make([]providers.ToolDefinition, 0, len(stable)+len(dynamic))
	out = append(out, stable...)
	out = append(out, dynamic...)
	// Append direct MCP tools after built-ins to preserve prompt cache stability.
	if t.mcpManager != nil {
		for _, tool := range t.mcpManager.AllTools() {
			if hasSurface {
				if t.isToolDisabled(tool.Name()) {
					continue
				}
				if !activeSurfaceAllowsKnownTool(surface, tool) {
					continue
				}
				if isDeferredByDefault(tool.Name()) && !t.isDeferredToolActive(tool.Name()) {
					continue
				}
				d := tool.Definition()
				d.CacheStable = false
				out = append(out, d)
				continue
			}
			if t.toolExposure(tool.Name()) == ToolExposureDirect {
				d := tool.Definition()
				d.CacheStable = false
				out = append(out, d)
			}
		}
	}
	return out
}

// SetActiveProfile installs the model profile that drives
// Definitions(). The toolkit compiles the profile into a Surface and
// uses it as the whitelist for visible tool names.
//
// Passing the zero value clears the active profile and restores the
// legacy direct-tool surface. This is the right behavior for the
// CLI's `wuu debug tools` path and for any admin caller that wants
// to inspect every tool the registry knows about, regardless of
// model context.
func (t *Toolkit) SetActiveProfile(p modelprofile.Profile) {
	if t == nil {
		return
	}
	if (p == modelprofile.Profile{}) {
		t.SetEditToolMode(EditToolModeText)
	} else {
		t.SetEditToolMode(EditToolModeForProfile(p))
	}

	t.activeProfileMu.Lock()
	defer t.activeProfileMu.Unlock()
	t.activeProfile = p
	if (p == modelprofile.Profile{}) {
		t.activeSurface = capability.Surface{}
		t.env.ActiveSurface = capability.Surface{}
		return
	}
	t.activeSurface = modelprofile.DefaultCompiler{}.Compile(p)
	t.env.ActiveSurface = cloneSurface(t.activeSurface)
}

// ActiveProfile returns the currently installed model profile, or the
// zero value if none is installed.
func (t *Toolkit) ActiveProfile() modelprofile.Profile {
	if t == nil {
		return modelprofile.Profile{}
	}
	t.activeProfileMu.RLock()
	defer t.activeProfileMu.RUnlock()
	return t.activeProfile
}

// ActiveSurface returns the compiled surface for the active profile.
// The returned value is a copy; callers can inspect it freely.
func (t *Toolkit) ActiveSurface() capability.Surface {
	if t == nil {
		return capability.Surface{}
	}
	t.activeProfileMu.RLock()
	defer t.activeProfileMu.RUnlock()
	return cloneSurface(t.activeSurface)
}

// activeCompiledSurface is the internal helper Definitions() uses
// to read the active surface under the read lock. Kept package-private
// so the public ActiveSurface() getter can hand callers a copy
// without contention on the hot Definitions() path.
func (t *Toolkit) activeCompiledSurface() capability.Surface {
	if t == nil {
		return capability.Surface{}
	}
	t.activeProfileMu.RLock()
	defer t.activeProfileMu.RUnlock()
	return t.activeSurface
}

func cloneSurface(surface capability.Surface) capability.Surface {
	out := surface
	if len(surface.Tools) > 0 {
		out.Tools = make(map[string]capability.Capability, len(surface.Tools))
		for name, cap := range surface.Tools {
			out.Tools[name] = cap
		}
	}
	if len(surface.HiddenTools) > 0 {
		out.HiddenTools = make(map[string]capability.Capability, len(surface.HiddenTools))
		for name, cap := range surface.HiddenTools {
			out.HiddenTools[name] = cap
		}
	}
	out.Capabilities = append([]capability.Capability(nil), surface.Capabilities...)
	out.HiddenCapabilities = append([]capability.Capability(nil), surface.HiddenCapabilities...)
	return out
}

// Execute runs one tool call and returns JSON result. This is the
// registry-based dispatch that replaces the old switch-case.
//
// Large results are automatically persisted to disk when a SessionDir
// is configured, so the model receives a compact reference instead of a
// truncated blob.
func (t *Toolkit) Execute(ctx context.Context, call providers.ToolCall) (string, error) {
	if t.isToolDisabled(call.Name) {
		return "", fmt.Errorf("tool %q is disabled in this session", call.Name)
	}
	if err := t.ensureToolAvailableForExecution(call.Name); err != nil {
		return "", err
	}
	tool := t.registry.Lookup(call.Name)
	if tool == nil {
		// Fallback to MCP manager.
		if t.mcpManager != nil {
			for _, mcpTool := range t.mcpManager.AllTools() {
				if mcpTool.Name() == call.Name {
					return t.executeKnownTool(ctx, call, mcpTool)
				}
			}
		}
		return "", fmt.Errorf("unknown tool %q", call.Name)
	}
	return t.executeKnownTool(ctx, call, tool)
}

func (t *Toolkit) ensureToolAvailableForExecution(name string) error {
	surface := t.activeCompiledSurface()
	if surface.ProfileName != "" {
		if !activeSurfaceAllowsKnownTool(surface, t.LookupTool(name)) {
			return fmt.Errorf("tool %q is not available in the active model surface", name)
		}
		if !t.extensionSurfacePolicy.allowsKind(classifyToolKind(name)) {
			return nil
		}
		if isDeferredByDefault(name) && !t.isDeferredToolActive(name) {
			return fmt.Errorf("tool %q is deferred; call tool_search first to expose it", name)
		}
		return nil
	}
	if t.toolExposure(name) == ToolExposureDeferred {
		return fmt.Errorf("tool %q is deferred; call tool_search first to expose it", name)
	}
	return nil
}

func activeSurfaceAllowsDynamicTool(surface capability.Surface, name string) bool {
	if surface.ProfileName == "" {
		return true
	}
	if _, ok := surface.Tools[name]; ok {
		return true
	}
	if classifyToolKind(name) == ToolKindMCP && surfaceHasVisibleCapability(surface, capability.CapabilityMCP) {
		return true
	}
	return false
}

func activeSurfaceAllowsKnownTool(surface capability.Surface, tool Tool) bool {
	if tool == nil {
		return false
	}
	if !activeSurfaceAllowsDynamicTool(surface, tool.Name()) {
		return false
	}
	if classifyToolKind(tool.Name()) == ToolKindMCP &&
		surfaceLacksTerminalExecution(surface) {
		return mcpToolAllowedWithoutTerminalExecution(surface, tool)
	}
	return true
}

func mcpToolAllowedWithoutTerminalExecution(surface capability.Surface, tool Tool) bool {
	if mcpToolMentionsTerminalOnlyPath(tool) {
		return false
	}
	capName, ok := mcpToolCapability(tool)
	if !ok {
		return false
	}
	if !surfaceHasVisibleCapability(surface, capName) {
		return false
	}
	if !isReadOnlyMCPProfileCapability(capName) {
		return false
	}
	return tool.IsReadOnly()
}

func isReadOnlyMCPProfileCapability(capName capability.Capability) bool {
	switch capName {
	case capability.CapabilityFileRead,
		capability.CapabilityFileList,
		capability.CapabilitySearchGrep,
		capability.CapabilitySearchGlob,
		capability.CapabilitySearchAST,
		capability.CapabilitySearchSemantic,
		capability.CapabilityWebFetch,
		capability.CapabilityWebSearch,
		capability.CapabilityMemorySession,
		capability.CapabilityMemoryProject:
		return true
	default:
		return false
	}
}

func surfaceHasVisibleCapability(surface capability.Surface, capName capability.Capability) bool {
	for _, existing := range surface.Capabilities {
		if existing == capName {
			return true
		}
	}
	return false
}

// LookupTool returns the Tool with the given name, or nil. This
// allows callers (e.g. the agent loop) to inspect tool metadata
// like IsReadOnly() and IsConcurrencySafe() for scheduling.
func (t *Toolkit) LookupTool(name string) Tool {
	if tool := t.registry.Lookup(name); tool != nil {
		return tool
	}
	if t.mcpManager != nil {
		for _, tool := range t.mcpManager.AllTools() {
			if tool.Name() == name {
				return tool
			}
		}
	}
	return nil
}

// ToolMetadata implements agent.ToolMetadataProvider so the loop can
// partition tool calls into concurrent (read-only) and serial (write)
// batches without importing the tools package.
func (t *Toolkit) ToolMetadata(call providers.ToolCall) (agent.ToolMetadata, bool) {
	tool := t.LookupTool(call.Name)
	if tool == nil {
		return agent.ToolMetadata{}, false
	}
	info := buildToolInfoForArgs(tool, t.toolExposure(call.Name), call.Arguments)
	return agent.ToolMetadata{
		ReadOnly:        info.ReadOnly,
		ConcurrencySafe: info.ConcurrencySafe,
		Destructive:     info.Destructive,
		Risk:            string(info.Risk),
		Reason:          info.Reason,
	}, true
}

// ── Shared utilities (used by individual tool files) ───────────────

func nonInteractiveShellEnv() map[string]string {
	return map[string]string{
		"EDITOR":              "true",
		"GIT_EDITOR":          "true",
		"GIT_PAGER":           "cat",
		"GIT_SEQUENCE_EDITOR": "true",
		"GIT_TERMINAL_PROMPT": "0",
		"GH_PAGER":            "cat",
		"NO_COLOR":            "1",
		"PAGER":               "cat",
		"VISUAL":              "true",
	}
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	merged := make(map[string]string, len(base)+len(overrides))
	order := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, exists := merged[key]; !exists {
			order = append(order, key)
		}
		merged[key] = value
	}
	for key, value := range overrides {
		if _, exists := merged[key]; !exists {
			order = append(order, key)
		}
		merged[key] = value
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, key+"="+merged[key])
	}
	return out
}

func decodeArgs(raw string, target any) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = "{}"
	}
	if err := json.Unmarshal([]byte(trimmed), target); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	return nil
}

func mustJSON(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func truncate(value string, maxBytes int) (string, bool) {
	if len(value) <= maxBytes {
		return value, false
	}
	return stringutil.Truncate(value, maxBytes, ""), true
}

func normalizeDisplayPath(rootDir, absPath string) string {
	rel, err := filepath.Rel(rootDir, absPath)
	if err != nil {
		return absPath
	}
	if rel == "." {
		return "."
	}
	return rel
}

func isSkippedDir(name string) bool {
	switch name {
	case ".git", ".wuu", ".hg", ".svn", "node_modules", "vendor", "__pycache__", ".tox", ".venv":
		return true
	}
	return false
}

func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	const binaryCheckSize = 8192
	buf := make([]byte, binaryCheckSize)
	n, _ := f.Read(buf)
	checkBuf := buf[:n]

	nonPrintable := 0
	for _, b := range checkBuf {
		if b == 0 {
			return true
		}
		if b < 32 && b != 9 && b != 10 && b != 13 {
			nonPrintable++
		}
	}

	if len(checkBuf) > 0 && float64(nonPrintable)/float64(len(checkBuf)) > 0.1 {
		return true
	}
	return false
}

// levenshtein returns the edit distance between two strings.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr := make([]int, lb+1)
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev = curr
	}
	return prev[lb]
}

// suggestSimilarFile looks in the same directory for a filename within
// Levenshtein distance 3 of the requested file. Returns "" if none found.
func suggestSimilarFile(absPath string) string {
	dir := filepath.Dir(absPath)
	target := filepath.Base(absPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	bestDist := 4 // threshold + 1
	bestName := ""
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		d := levenshtein(strings.ToLower(target), strings.ToLower(e.Name()))
		if d < bestDist {
			bestDist = d
			bestName = e.Name()
		}
	}
	return bestName
}

func matchGlob(pattern, path string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	path = filepath.ToSlash(path)
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "/") {
		matched, _ := filepath.Match(pattern, filepath.Base(path))
		return matched
	}
	re, err := regexp.Compile(globToRegexp(pattern))
	if err != nil {
		return false
	}
	return re.MatchString(path)
}

func globToRegexp(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					b.WriteString("(?:.*/)?")
					i += 2
					continue
				}
				if i+2 >= len(pattern) && i > 0 && pattern[i-1] == '/' {
					b.WriteString(".*")
					i++
					continue
				}
				b.WriteString("[^/]*")
				i++
				continue
			}
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(pattern[i])
		default:
			b.WriteByte(pattern[i])
		}
	}
	b.WriteString("$")
	return b.String()
}

// ── Ripgrep helpers ────────────────────────────────────────────────

type grepMatch struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

type grepOptions struct {
	outputMode string
	context    int
	before     int
	after      int
	ignoreCase bool
}

type grepCountResult struct {
	File  string `json:"file"`
	Count int    `json:"count"`
}

type rgJSONEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
	} `json:"data"`
}

var (
	rgLookupPath = exec.LookPath
	rgCommand    = exec.CommandContext
	rgPathOnce   sync.Once
	rgPath       string
)

func lookupRG() string {
	rgPathOnce.Do(func() {
		path, err := rgLookupPath("rg")
		if err == nil {
			rgPath = path
		}
	})
	return rgPath
}

func resetRGForTests() {
	rgPathOnce = sync.Once{}
	rgPath = ""
}

func buildRGFilesCommand(ctx context.Context, pattern string) *exec.Cmd {
	name := lookupRG()
	if name == "" {
		return nil
	}
	args := []string{"--files", "--hidden", "-0", "--glob", pattern}
	return rgCommand(ctx, name, args...)
}

func buildRGGrepCommand(ctx context.Context, pattern, searchRoot, include string, opts grepOptions) *exec.Cmd {
	name := lookupRG()
	if name == "" {
		return nil
	}
	args := []string{"--json", "--hidden", "-H", "-n"}
	if opts.ignoreCase {
		args = append(args, "-i")
	}
	if opts.context > 0 {
		args = append(args, "-C", fmt.Sprintf("%d", opts.context))
	}
	if opts.before > 0 {
		args = append(args, "-B", fmt.Sprintf("%d", opts.before))
	}
	if opts.after > 0 {
		args = append(args, "-A", fmt.Sprintf("%d", opts.after))
	}
	args = append(args, pattern)
	if include != "" {
		args = append(args, "--glob", include)
	}
	if strings.TrimSpace(searchRoot) != "" {
		args = append(args, searchRoot)
	}
	return rgCommand(ctx, name, args...)
}
