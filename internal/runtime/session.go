package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/config"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/hooks"
	"github.com/blueberrycongee/wuu/internal/mcp"
	"github.com/blueberrycongee/wuu/internal/memory"
	"github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/prompt"
	"github.com/blueberrycongee/wuu/internal/providerfactory"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/skills"
	"github.com/blueberrycongee/wuu/internal/tools"
	"github.com/blueberrycongee/wuu/internal/worktree"
)

// Options describes the shared agent runtime needed by interactive clients.
// The TUI is the first client, but the shape is intentionally UI-neutral so a
// future desktop app can attach a protocol bridge without rebuilding the agent.
type Options struct {
	RootDir       string
	HomeDir       string
	ConfigPath    string
	Config        config.Config
	ProviderName  string
	ModelOverride string
	NoTools       bool
	AskBridge     tools.AskUserBridge
}

// Session owns one initialized local agent runtime: provider client, tool
// executor, hooks, MCP, skills, memory, coordinator, process manager, and the
// stream runner. UI surfaces should depend on this instead of reassembling the
// pieces themselves.
type Session struct {
	ProviderName                string
	Model                       string
	RootDir                     string
	ConfigPath                  string
	SessionDir                  string
	StreamRunner                *agent.StreamRunner
	HookDispatcher              *hooks.Dispatcher
	Skills                      []skills.Skill
	Memory                      []memory.File
	AgentControl                *agentcontrol.AgentControl
	AskBridge                   tools.AskUserBridge
	ProcessManager              *process.Manager
	Toolkit                     *tools.Toolkit
	BaseSystemPrompt            string
	CoordinatorPreamble         string
	ExperimentalCoordinatorMode bool
}

// NewSession builds the shared runtime for an interactive agent surface.
func NewSession(opts Options) (*Session, error) {
	rootDir := strings.TrimSpace(opts.RootDir)
	if rootDir == "" {
		return nil, fmt.Errorf("root dir is required")
	}
	cfg := opts.Config

	providerCfg, resolvedName, err := cfg.ResolveProvider(opts.ProviderName)
	if err != nil {
		return nil, err
	}
	if opts.ModelOverride != "" {
		providerCfg.Model = opts.ModelOverride
	}

	client, err := providerfactory.BuildStreamClient(providerCfg, resolvedName)
	if err != nil {
		return nil, err
	}

	providers.InitDebugLog(rootDir)
	setupCatwalk(cfg)

	hookDispatcher := buildHookDispatcher(cfg)
	discoveredSkills := discoverSkills(rootDir, opts.HomeDir)

	processMgr, err := process.NewManager(rootDir)
	if err != nil {
		return nil, err
	}

	var toolExecutor agent.ToolExecutor
	var toolkit *tools.Toolkit
	if !opts.NoTools {
		kit, newErr := tools.New(rootDir)
		if newErr != nil {
			return nil, newErr
		}
		kit.SetProcessManager(processMgr)
		kit.SetSkills(discoveredSkills)
		kit.SetAskUserBridge(opts.AskBridge)
		kit.SetOnFileChanged(func(absPath string) {
			_, _ = hookDispatcher.Dispatch(context.Background(), hooks.FileChanged, &hooks.Input{
				CWD:      rootDir,
				FilePath: absPath,
			})
		})
		toolkit = kit
		toolExecutor = hooks.NewHookedExecutor(kit, hookDispatcher, "", rootDir)
		connectMCPServers(cfg, toolkit)
	}

	memoryFiles := discoverMemory(rootDir, opts.HomeDir, cfg.Memory)
	baseSystemPrompt := buildBaseSystemPrompt(rootDir, cfg.Agent.SystemPrompt, memoryFiles, discoveredSkills)

	if toolkit != nil {
		if err := agentcontrol.EnsureSharedDir(rootDir); err != nil {
			return nil, fmt.Errorf("ensure shared dir: %w", err)
		}
	}

	var agentControl *agentcontrol.AgentControl
	var coordinatorPreamble string
	if toolkit != nil {
		workerRetry := providerfactory.SubAgentRetryConfig()
		workerClient, werr := providerfactory.BuildStreamClientWithRetry(providerCfg, resolvedName, &workerRetry)
		if werr != nil {
			return nil, fmt.Errorf("build worker client: %w", werr)
		}

		c, cerr := agentcontrol.New(agentcontrol.Config{
			Client:          workerClient,
			DefaultModel:    providerCfg.Model,
			ParentRepo:      rootDir,
			WorktreeRoot:    filepath.Join(rootDir, ".wuu", "worktrees"),
			SessionID:       "session-pending",
			HistoryDir:      "",
			WorkerSysPrompt: baseSystemPrompt,
			WorkerFactory: func(workerRoot string, wt agentcontrol.WorkerType, meta agentthread.Metadata) (agent.ToolExecutor, error) {
				wkit, werr := tools.New(workerRoot)
				if werr != nil {
					return nil, werr
				}
				wkit.SetProcessManager(processMgr)
				wkit.SetSkills(discoveredSkills)
				wkit.SetAgentControl(agentControl)
				wkit.SetAgentIdentity(meta.ID, meta.Path)
				applyWorkerToolFilter(wkit, wt)
				return wkit, nil
			},
			MaxParallel: 5,
		})
		if cerr == nil {
			agentControl = c
			toolkit.SetAgentControl(agentControl)
			coordinatorPreamble = agentcontrol.SystemPromptPreamble()
		}
	}

	sessionDir := session.Dir(opts.HomeDir)
	if sessionDir == "" {
		sessionDir = filepath.Join(rootDir, ".wuu", "sessions")
	}

	streamRunner := &agent.StreamRunner{
		Client:       client,
		Tools:        toolExecutor,
		Model:        providerCfg.Model,
		SystemPrompt: baseSystemPrompt,
		MaxSteps:     cfg.Agent.MaxSteps,
		Temperature:  cfg.Agent.Temperature,
		Effort:       cfg.Agent.Effort,
		ContextWindowOverride: ResolveContextWindow(
			providerCfg.Model,
			providerCfg.ContextWindow,
			cfg.Agent.MaxContextTokens,
		),
		DisableAutoCompact: cfg.Agent.DisableAutoCompact,
		BeforeStep:         EnvContextInjector(rootDir),
	}

	return &Session{
		ProviderName:                resolvedName,
		Model:                       providerCfg.Model,
		RootDir:                     rootDir,
		ConfigPath:                  opts.ConfigPath,
		SessionDir:                  sessionDir,
		StreamRunner:                streamRunner,
		HookDispatcher:              hookDispatcher,
		Skills:                      discoveredSkills,
		Memory:                      memoryFiles,
		AgentControl:                agentControl,
		AskBridge:                   opts.AskBridge,
		ProcessManager:              processMgr,
		Toolkit:                     toolkit,
		BaseSystemPrompt:            baseSystemPrompt,
		CoordinatorPreamble:         coordinatorPreamble,
		ExperimentalCoordinatorMode: cfg.Agent.ExperimentalCoordinatorMode,
	}, nil
}

func applyWorkerToolFilter(kit *tools.Toolkit, wt agentcontrol.WorkerType) {
	if kit == nil {
		return
	}
	full := kit.Definitions()
	fullNames := make([]string, 0, len(full))
	for _, def := range full {
		fullNames = append(fullNames, def.Name)
	}

	allowed := agentcontrol.FilterToolsForWorker(wt, fullNames)
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}

	disabled := make([]string, 0, len(fullNames)-len(allowedSet))
	for _, name := range fullNames {
		if _, ok := allowedSet[name]; !ok {
			disabled = append(disabled, name)
		}
	}
	kit.DisableTools(disabled...)
}

// SetSessionID binds workspace-scoped runtime artifact paths after the UI has
// created or resumed a session. Conversation logs live in SessionDir.
func (s *Session) SetSessionID(id string) {
	if s == nil || strings.TrimSpace(id) == "" {
		return
	}
	if s.Toolkit != nil {
		s.Toolkit.SetSessionID(id)
		s.Toolkit.SetAgentIdentity(id, agentthread.RootPath)
		artifactDir := filepath.Join(s.RootDir, ".wuu", "sessions", id)
		s.Toolkit.SetSessionDir(artifactDir)
		if s.AgentControl != nil {
			s.AgentControl.SetSessionInfo(
				id,
				filepath.Join(artifactDir, "workers"),
				filepath.Join(s.SessionDir, id+".threads"),
			)
		}
	}
}

// Cleanup stops session-scoped background work owned by the runtime.
func (s *Session) Cleanup() (process.CleanupResult, error) {
	if s == nil {
		return process.CleanupResult{}, nil
	}
	if s.AgentControl != nil {
		_ = s.AgentControl.CleanupSession()
	}
	if s.ProcessManager == nil {
		return process.CleanupResult{}, nil
	}
	return s.ProcessManager.CleanupSessionWithResult()
}

// ResolveContextWindow resolves the effective model context size.
func ResolveContextWindow(model string, providerOverride, agentOverride int) int {
	if providerOverride > 0 {
		return providerOverride
	}
	if agentOverride > 0 {
		return agentOverride
	}
	return providers.ContextWindowFor(model)
}

// EnvContextInjector returns dynamic environment context injected before each
// model round.
func EnvContextInjector(rootDir string) func() []providers.ChatMessage {
	return func() []providers.ChatMessage {
		env := wuucontext.Snapshot(rootDir)
		reminder := wuucontext.FormatSystemReminder(env)
		return []providers.ChatMessage{{
			Role:    "user",
			Name:    wuucontext.SystemReminderMessageName,
			Content: reminder,
		}}
	}
}

func setupCatwalk(cfg config.Config) {
	catwalkCfg := providers.CatwalkSyncConfig{
		CachePath: providers.DefaultCatwalkCachePath(),
	}
	if cfg.Agent.CatwalkAutoupdate {
		catwalkCfg.Client = catwalk.NewWithURL(providers.DefaultCatwalkURL)
	}
	catwalkSync := providers.NewCatwalkSync(catwalkCfg)
	providers.SetCatwalkSync(catwalkSync)
	if cfg.Agent.CatwalkAutoupdate {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = providers.RefreshCatwalkIndex(ctx)
		}()
	}
}

func buildHookDispatcher(cfg config.Config) *hooks.Dispatcher {
	hookEntries := make(map[hooks.Event][]hooks.HookConfig)
	for evName, entries := range cfg.Hooks {
		ev := hooks.Event(evName)
		for _, e := range entries {
			hookEntries[ev] = append(hookEntries[ev], hooks.HookConfig{
				Matcher: e.Matcher,
				Type:    e.Type,
				Command: e.Command,
				Prompt:  e.Prompt,
				Model:   e.Model,
				Timeout: e.Timeout,
			})
		}
	}
	hookRegistry := hooks.NewRegistry(hookEntries)
	return hooks.NewDispatcher(hookRegistry)
}

func discoverSkills(rootDir, homeDir string) []skills.Skill {
	projectSkillsDir := filepath.Join(rootDir, ".claude", "skills")
	userSkillsDir := ""
	if homeDir != "" {
		userSkillsDir = filepath.Join(homeDir, ".claude", "skills")
	}
	return skills.Discover(projectSkillsDir, userSkillsDir)
}

func connectMCPServers(cfg config.Config, toolkit *tools.Toolkit) {
	if toolkit == nil || len(cfg.MCPServers) == 0 {
		return
	}
	mcpMgr := mcp.NewManager()
	toolkit.SetMCPManager(mcpMgr)
	go func() {
		ctx := context.Background()
		for name, mcpCfg := range cfg.MCPServers {
			serverCfg := mcp.ServerConfig{
				Name:    name,
				Command: mcpCfg.Command,
				Args:    mcpCfg.Args,
				URL:     mcpCfg.URL,
				Env:     mcpCfg.Env,
			}
			if err := mcpMgr.Add(ctx, serverCfg); err != nil {
				providers.DebugLogf("mcp server %q failed to connect: %v", name, err)
			} else {
				providers.DebugLogf("mcp server %q connected (%d tools)", name, mcpMgr.Status()[name].ToolCount)
			}
		}
	}()
}

func discoverMemory(rootDir, homeDir string, cfg config.MemoryConfig) []memory.File {
	if cfg.Disable {
		return nil
	}
	memOpts := memory.DefaultOptions()
	if len(cfg.Filenames) > 0 {
		memOpts.Filenames = cfg.Filenames
	}
	if len(cfg.ProjectRootMarkers) > 0 {
		memOpts.ProjectRootMarkers = cfg.ProjectRootMarkers
	}
	if len(cfg.UserDirs) > 0 {
		memOpts.UserDirs = cfg.UserDirs
	}
	return memory.Discover(rootDir, homeDir, memOpts)
}

func buildBaseSystemPrompt(rootDir, systemPrompt string, memoryFiles []memory.File, discoveredSkills []skills.Skill) string {
	var pb prompt.Builder
	pb.AddSection("base", systemPrompt, true)
	pb.AddMemory(memoryFiles)
	pb.AddSkills(discoveredSkills)
	if worktree.IsGitRepo(rootDir) {
		gitCtx := prompt.NewGitContext(rootDir)
		pb.AddGitContext(gitCtx.Collect())
	}
	return pb.Build()
}
