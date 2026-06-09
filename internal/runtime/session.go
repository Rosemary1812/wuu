package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/config"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/cron"
	"github.com/blueberrycongee/wuu/internal/hooks"
	"github.com/blueberrycongee/wuu/internal/mcp"
	"github.com/blueberrycongee/wuu/internal/memory"
	memstore "github.com/blueberrycongee/wuu/internal/memory/store"
	"github.com/blueberrycongee/wuu/internal/modelcatalog"
	"github.com/blueberrycongee/wuu/internal/modelvariant"
	"github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/prompt"
	"github.com/blueberrycongee/wuu/internal/providerfactory"
	"github.com/blueberrycongee/wuu/internal/provideroptions"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/skills"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/tools"
	"github.com/blueberrycongee/wuu/internal/workflow"
	"github.com/blueberrycongee/wuu/internal/worktree"
)

// Options describes the shared agent runtime needed by interactive clients.
// The shape is intentionally UI-neutral so desktop and protocol clients can
// attach without rebuilding the agent.
type Options struct {
	RootDir       string
	HomeDir       string
	ConfigPath    string
	Config        config.Config
	ProviderName  string
	ModelOverride string
	NoTools       bool
}

// Session owns one initialized local agent runtime: provider client, tool
// executor, hooks, MCP, skills, memory, coordinator, process manager, and the
// stream runner. UI surfaces should depend on this instead of reassembling the
// pieces themselves.
type Session struct {
	ProviderName                string
	Model                       string
	RootDir                     string
	StateDir                    string
	ConfigPath                  string
	SessionDir                  string
	StreamRunner                *agent.StreamRunner
	TitleClient                 providers.Client
	HookDispatcher              *hooks.Dispatcher
	Skills                      []skills.Skill
	Workflows                   []workflow.Definition
	Memory                      []memory.File
	ProfileMemoryNudgeInterval  int
	ProfileMemoryCharLimit      int
	ProfileUserMemoryCharLimit  int
	AgentControl                *agentcontrol.AgentControl
	ProcessManager              *process.Manager
	Toolkit                     *tools.Toolkit
	WorkerClient                providers.StreamClient
	BaseSystemPrompt            string
	UserSystemPrompt            string
	WuuHome                     string
	ToolPolicy                  config.ToolPolicyConfig
	CoordinatorPreamble         string
	ExperimentalCoordinatorMode bool
	CronScheduler               *cron.Scheduler
	CronLock                    *cron.Lock
}

// ThreadRuntime owns the mutable execution state for one app-server
// conversation. The desktop app can run multiple ThreadRuntimes at once; each
// one has its own StreamRunner, Toolkit Env, usage tracker, and AgentControl.
type ThreadRuntime struct {
	StreamRunner *agent.StreamRunner
	Toolkit      *tools.Toolkit
	AgentControl *agentcontrol.AgentControl
}

// NewSession builds the shared runtime for an interactive agent surface.
func NewSession(opts Options) (*Session, error) {
	rootDir := strings.TrimSpace(opts.RootDir)
	if rootDir == "" {
		return nil, fmt.Errorf("root dir is required")
	}
	cfg := opts.Config

	wuuHome, err := statepath.Home(opts.HomeDir)
	if err != nil {
		return nil, fmt.Errorf("resolve wuu home: %w", err)
	}
	workspaceStateDir, err := statepath.WorkspaceDir(wuuHome, rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace state directory: %w", err)
	}
	profileName := cfg.Agent.ProfileName()
	profileMemoryEnabled := cfg.Agent.ProfileMemoryEnabled()
	userSystemPrompt := cfg.Agent.UserSystemPrompt()

	providerCfg, resolvedName, err := cfg.ResolveProvider(opts.ProviderName)
	if err != nil {
		return nil, err
	}
	if opts.ModelOverride != "" {
		providerCfg.Model = opts.ModelOverride
	}
	ruleProviderName, ruleProviderCfg := modelcatalog.EnrichProvider(resolvedName, providerCfg, providerCfg.Model)
	toolModeModel := modelcatalog.APIModel(ruleProviderCfg, providerCfg.Model)

	client, err := providerfactory.BuildStreamClient(ruleProviderCfg, resolvedName)
	if err != nil {
		return nil, err
	}

	providers.InitDebugLog(statepath.LogDir(wuuHome))
	setupCatwalk(cfg)

	hookDispatcher := buildHookDispatcher(cfg)
	discoveredSkills := discoverSkills(rootDir, opts.HomeDir)
	discoveredWorkflows := discoverWorkflows(rootDir, opts.HomeDir)

	processMgr, err := process.NewManager(rootDir, statepath.RuntimeDir(workspaceStateDir))
	if err != nil {
		return nil, err
	}

	var toolExecutor agent.ToolExecutor
	var toolkit *tools.Toolkit
	var profileMemoryProvider memstore.Provider
	profileMemoryCharLimit := cfg.Memory.ProfileMemoryCharLimit()
	profileUserMemoryCharLimit := cfg.Memory.ProfileUserCharLimit()
	if !opts.NoTools {
		kit, newErr := tools.New(rootDir)
		if newErr != nil {
			return nil, newErr
		}
		kit.SetStateDir(workspaceStateDir)
		kit.SetProcessManager(processMgr)
		kit.SetSkills(discoveredSkills)
		kit.SetWorkflows(discoveredWorkflows)
		kit.SetToolPolicy(ToolPolicyFromConfig(cfg.Agent.ToolPolicy))
		kit.ConfigureEditToolsForProviderModel(ruleProviderName, toolModeModel)
		kit.SetMemoryLimits(profileMemoryCharLimit, profileUserMemoryCharLimit)
		if profileMemoryEnabled {
			// Attach the durable profile memory store for named agents. Ordinary
			// default sessions stay memoryless so they can act as transient
			// orchestration workspaces.
			if memProvider, memErr := newProfileMemoryProvider(wuuHome, profileName); memErr == nil {
				kit.SetMemory(memProvider)
				profileMemoryProvider = memProvider
			}
		}
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
	profileMemoryEntries := recallProfileMemory(context.Background(), profileMemoryProvider)
	baseSystemPrompt := buildBaseSystemPrompt(rootDir, config.DefaultSystemPrompt(), userSystemPrompt, resolvedName, toolModeModel, memoryFiles, profileMemoryEntries, profileMemoryProvider != nil, profileMemoryCharLimit, profileUserMemoryCharLimit, discoveredSkills, discoveredWorkflows)

	if toolkit != nil {
		if err := agentcontrol.EnsureSharedDir(workspaceStateDir); err != nil {
			return nil, fmt.Errorf("ensure shared dir: %w", err)
		}
	}

	var agentControl *agentcontrol.AgentControl
	var coordinatorPreamble string
	var workerClient providers.StreamClient
	if toolkit != nil {
		workerRetry := providerfactory.SubAgentRetryConfig()
		var werr error
		workerClient, werr = providerfactory.BuildStreamClientWithRetry(providerCfg, resolvedName, &workerRetry)
		if werr != nil {
			return nil, fmt.Errorf("build worker client: %w", werr)
		}

		c, cerr := agentcontrol.New(agentcontrol.Config{
			Client:          workerClient,
			DefaultModel:    providerCfg.Model,
			ParentRepo:      rootDir,
			WorktreeRoot:    statepath.WorktreeRoot(workspaceStateDir),
			SessionID:       "session-pending",
			HistoryDir:      "",
			WorkerSysPrompt: baseSystemPrompt,
			WorkerPrompt: func(workerRoot string, wt agentcontrol.WorkerType, meta agentthread.Metadata, isolation agentcontrol.IsolationMode) (string, error) {
				return buildProfileWorkerBasePrompt(workerRoot, wuuHome, meta.AgentProfile, userSystemPrompt, resolvedName, toolModeModel, memoryFiles, profileMemoryCharLimit, profileUserMemoryCharLimit, discoveredSkills, discoveredWorkflows)
			},
			WorkerFactory: func(workerRoot string, wt agentcontrol.WorkerType, meta agentthread.Metadata) (agent.ToolExecutor, error) {
				wkit, werr := tools.New(workerRoot)
				if werr != nil {
					return nil, werr
				}
				workerStateDir := workspaceStateDir
				if workerRoot != rootDir {
					if dir, err := statepath.WorkspaceDir(wuuHome, workerRoot); err == nil {
						workerStateDir = dir
					}
				}
				wkit.SetStateDir(workerStateDir)
				wkit.SetProcessManager(processMgr)
				wkit.SetSkills(discoveredSkills)
				wkit.SetWorkflows(discoveredWorkflows)
				wkit.SetAgentControl(agentControl)
				wkit.SetToolPolicy(ToolPolicyFromConfig(cfg.Agent.ToolPolicy))
				wkit.ConfigureEditToolsForProviderModel(ruleProviderName, toolModeModel)
				wkit.SetMemoryLimits(profileMemoryCharLimit, profileUserMemoryCharLimit)
				if strings.TrimSpace(meta.AgentProfile) != "" {
					memProvider, memErr := newProfileMemoryProvider(wuuHome, meta.AgentProfile)
					if memErr != nil {
						return nil, memErr
					}
					wkit.SetMemory(memProvider)
				} else if profileMemoryProvider != nil {
					wkit.SetMemory(profileMemoryProvider)
				}
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

	sessionDir := statepath.SessionsDir(wuuHome)
	modelSelection := modelvariant.ResolveForProvider(ruleProviderName, ruleProviderCfg, providerCfg.Model, cfg.Agent.Variant, cfg.Agent.Effort)
	contextWindow := ResolveContextWindow(
		providerCfg.Model,
		ruleProviderCfg.ContextWindow,
		cfg.Agent.MaxContextTokens,
	)
	profileMemoryNudgeInterval := cfg.Memory.ProfileMemoryNudgeInterval()
	var afterTurn func(context.Context, *agent.StreamRunner, []providers.ChatMessage, agent.LoopResult)
	if memoryReviewer := newProfileMemoryReviewScheduler(profileMemoryProvider, profileMemoryNudgeInterval, profileMemoryCharLimit, profileUserMemoryCharLimit); memoryReviewer != nil {
		afterTurn = memoryReviewer.AfterTurn
	}

	streamRunner := &agent.StreamRunner{
		Client:                client,
		Tools:                 toolExecutor,
		Model:                 providerCfg.Model,
		APIModel:              modelcatalog.APIModel(ruleProviderCfg, providerCfg.Model),
		SystemPrompt:          baseSystemPrompt,
		MaxSteps:              cfg.Agent.MaxSteps,
		Temperature:           cfg.Agent.Temperature,
		Effort:                modelSelection.LegacyEffort,
		Variant:               modelSelection.Variant,
		ProviderOptions:       modelSelection.ProviderOptions,
		ContextWindowOverride: contextWindow,
		MaxInputTokens:        ResolveInputWindow(providerCfg.Model, ruleProviderCfg, contextWindow),
		DisableAutoCompact:    cfg.Agent.DisableAutoCompact,
		BeforeRequest:         EnvContextInjector(rootDir, agentControl, agentthread.RootPath, toolkitContextBlockProvider(toolkit)),
		AfterTurn:             afterTurn,
	}

	return &Session{
		ProviderName:                resolvedName,
		Model:                       providerCfg.Model,
		RootDir:                     rootDir,
		StateDir:                    workspaceStateDir,
		ConfigPath:                  opts.ConfigPath,
		SessionDir:                  sessionDir,
		StreamRunner:                streamRunner,
		TitleClient:                 client,
		HookDispatcher:              hookDispatcher,
		Skills:                      discoveredSkills,
		Workflows:                   discoveredWorkflows,
		Memory:                      memoryFiles,
		ProfileMemoryNudgeInterval:  profileMemoryNudgeInterval,
		ProfileMemoryCharLimit:      profileMemoryCharLimit,
		ProfileUserMemoryCharLimit:  profileUserMemoryCharLimit,
		AgentControl:                agentControl,
		ProcessManager:              processMgr,
		Toolkit:                     toolkit,
		WorkerClient:                workerClient,
		BaseSystemPrompt:            baseSystemPrompt,
		UserSystemPrompt:            userSystemPrompt,
		WuuHome:                     wuuHome,
		ToolPolicy:                  cfg.Agent.ToolPolicy,
		CoordinatorPreamble:         coordinatorPreamble,
		ExperimentalCoordinatorMode: cfg.Agent.ExperimentalCoordinatorMode,
	}, nil
}

func (s *Session) StartCronScheduler() error {
	if s == nil || s.StreamRunner == nil {
		return nil
	}
	if s.CronScheduler != nil {
		return nil
	}
	stateDir := strings.TrimSpace(s.StateDir)
	if stateDir == "" {
		return fmt.Errorf("workspace state directory is required for cron scheduler")
	}

	lockID := fmt.Sprintf("runtime-%d-%d", os.Getpid(), time.Now().UnixNano())
	lock := cron.NewLock(statepath.ScheduledTasksLockPath(stateDir), lockID)
	ownsDurableTasks := false
	scheduler := cron.NewScheduler(cron.SchedulerConfig{
		Store:        cron.NewTaskStore(statepath.ScheduledTasksPath(stateDir)),
		SessionStore: cron.NewSessionTaskStore(stateDir),
		OnFire: func(task cron.Task) {
			s.runScheduledPrompt(task)
		},
		IsOwner: func() bool {
			if ownsDurableTasks {
				return true
			}
			ok, err := lock.TryAcquire()
			if err != nil {
				providers.DebugLogf("cron scheduler lock acquire failed: %v", err)
				return false
			}
			ownsDurableTasks = ok
			return ok
		},
	})
	s.CronLock = lock
	s.CronScheduler = scheduler
	scheduler.Start()
	return nil
}

func (s *Session) runScheduledPrompt(task cron.Task) {
	prompt := strings.TrimSpace(task.Prompt)
	if prompt == "" || s.StreamRunner == nil {
		return
	}
	runner := s.StreamRunner
	if threadRT, err := s.NewThreadRuntime(scheduledCronSessionID("cron-task", task.ID)); err == nil && threadRT.StreamRunner != nil {
		runner = threadRT.StreamRunner
	} else if err != nil {
		providers.DebugLogf("cron prompt task %q using shared runner after thread runtime error: %v", task.ID, err)
	}
	if _, err := runner.Run(context.Background(), prompt); err != nil {
		providers.DebugLogf("cron prompt task %q failed: %v", task.ID, err)
	}
}

func scheduledCronSessionID(prefix, taskID string) string {
	id := strings.TrimSpace(taskID)
	var safe strings.Builder
	for i := 0; i < len(id); i++ {
		ch := id[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			safe.WriteByte(ch)
		}
	}
	if safe.Len() == 0 {
		safe.WriteString("task")
	}
	return fmt.Sprintf("%s-%s-%d", prefix, safe.String(), time.Now().UnixNano())
}

// NewThreadRuntime creates a per-conversation execution runtime from the
// shared workspace runtime. It intentionally does not mutate Session.Toolkit or
// Session.AgentControl; those remain the legacy single-session runtime used by
// CLI and older call sites.
func (s *Session) NewThreadRuntime(sessionID string) (*ThreadRuntime, error) {
	if s == nil {
		return nil, fmt.Errorf("runtime session is required")
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if s.StreamRunner == nil {
		return nil, fmt.Errorf("stream runner is required")
	}

	stateDir := strings.TrimSpace(s.StateDir)
	if stateDir == "" {
		home, err := statepath.Home("")
		if err != nil {
			return nil, fmt.Errorf("resolve wuu home: %w", err)
		}
		stateDir, err = statepath.WorkspaceDir(home, s.RootDir)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace state directory: %w", err)
		}
	}
	artifactDir := statepath.SessionArtifactDir(stateDir, id)

	var (
		kit          *tools.Toolkit
		agentControl *agentcontrol.AgentControl
		toolExecutor = s.StreamRunner.Tools
	)

	if s.Toolkit != nil {
		workerClient := s.WorkerClient
		if workerClient == nil {
			workerClient = s.StreamRunner.Client
		}
		if workerClient != nil {
			wuuHome := strings.TrimSpace(s.WuuHome)
			if wuuHome == "" {
				if home, err := statepath.Home(""); err == nil {
					wuuHome = home
				}
			}
			var control *agentcontrol.AgentControl
			control, _ = agentcontrol.New(agentcontrol.Config{
				Client:          workerClient,
				DefaultModel:    s.Model,
				ParentRepo:      s.RootDir,
				WorktreeRoot:    statepath.WorktreeRoot(stateDir),
				SessionID:       id,
				HistoryDir:      filepath.Join(artifactDir, "workers"),
				ThreadDir:       filepath.Join(artifactDir, "threads"),
				HarnessDir:      filepath.Join(artifactDir, "harness"),
				WorkerSysPrompt: s.BaseSystemPrompt,
				WorkerPrompt: func(workerRoot string, wt agentcontrol.WorkerType, meta agentthread.Metadata, isolation agentcontrol.IsolationMode) (string, error) {
					model := s.Model
					if s.StreamRunner != nil && strings.TrimSpace(s.StreamRunner.APIModel) != "" {
						model = s.StreamRunner.APIModel
					}
					return buildProfileWorkerBasePrompt(workerRoot, wuuHome, meta.AgentProfile, s.UserSystemPrompt, s.ProviderName, model, s.Memory, s.ProfileMemoryCharLimit, s.ProfileUserMemoryCharLimit, s.Skills, s.Workflows)
				},
				WorkerFactory: func(workerRoot string, wt agentcontrol.WorkerType, meta agentthread.Metadata) (agent.ToolExecutor, error) {
					workerKit, err := s.Toolkit.CloneForRoot(workerRoot)
					if err != nil {
						return nil, err
					}
					workerStateDir := stateDir
					if workerRoot != s.RootDir {
						if home, err := statepath.Home(""); err == nil {
							if dir, err := statepath.WorkspaceDir(home, workerRoot); err == nil {
								workerStateDir = dir
							}
						}
					}
					workerKit.SetStateDir(workerStateDir)
					workerKit.SetProcessManager(s.ProcessManager)
					workerKit.SetSkills(s.Skills)
					workerKit.SetWorkflows(s.Workflows)
					workerKit.SetAgentControl(control)
					if strings.TrimSpace(meta.AgentProfile) != "" {
						memProvider, memErr := newProfileMemoryProvider(wuuHome, meta.AgentProfile)
						if memErr != nil {
							return nil, memErr
						}
						workerKit.SetMemory(memProvider)
					}
					workerKit.SetAgentIdentity(meta.ID, meta.Path)
					applyWorkerToolFilter(workerKit, wt)
					return workerKit, nil
				},
				MaxParallel: 5,
			})
			agentControl = control
		}

		var err error
		kit, err = s.Toolkit.CloneForRoot(s.RootDir)
		if err != nil {
			return nil, err
		}
		kit.SetStateDir(stateDir)
		kit.SetProcessManager(s.ProcessManager)
		kit.SetSkills(s.Skills)
		kit.SetWorkflows(s.Workflows)
		kit.SetAgentControl(agentControl)
		kit.SetSessionID(id)
		kit.SetSessionDir(artifactDir)
		kit.SetAgentIdentity(id, agentthread.RootPath)
		toolExecutor = hooks.NewHookedExecutor(kit, s.HookDispatcher, "", s.RootDir)
	}

	runner := cloneStreamRunnerForThread(s.StreamRunner, toolExecutor)
	runner.BeforeRequest = EnvContextInjector(s.RootDir, agentControl, agentthread.RootPath, toolkitContextBlockProvider(kit))
	if kit != nil {
		memoryLimit, userLimit := kit.MemoryLimits()
		if memoryReviewer := newProfileMemoryReviewScheduler(kit.Memory(), s.ProfileMemoryNudgeInterval, memoryLimit, userLimit); memoryReviewer != nil {
			runner.AfterTurn = memoryReviewer.AfterTurn
		}
	}

	return &ThreadRuntime{
		StreamRunner: runner,
		Toolkit:      kit,
		AgentControl: agentControl,
	}, nil
}

func cloneStreamRunnerForThread(base *agent.StreamRunner, toolExecutor agent.ToolExecutor) *agent.StreamRunner {
	if base == nil {
		return nil
	}
	return &agent.StreamRunner{
		Client:                  base.Client,
		Tools:                   toolExecutor,
		Model:                   base.Model,
		APIModel:                base.APIModel,
		SystemPrompt:            base.SystemPrompt,
		MaxSteps:                base.MaxSteps,
		Temperature:             base.Temperature,
		OnEvent:                 base.OnEvent,
		Bus:                     base.Bus,
		OnUsage:                 base.OnUsage,
		ContextWindowOverride:   base.ContextWindowOverride,
		MaxInputTokens:          base.MaxInputTokens,
		DisableAutoCompact:      base.DisableAutoCompact,
		StreamingToolExecution:  base.StreamingToolExecution,
		BeforeStep:              base.BeforeStep,
		BeforeRequest:           base.BeforeRequest,
		AfterTurn:               base.AfterTurn,
		Effort:                  base.Effort,
		Variant:                 base.Variant,
		ProviderOptions:         provideroptions.Clone(base.ProviderOptions),
		StreamReconnectBudget:   base.StreamReconnectBudget,
		StreamRetryInitialDelay: base.StreamRetryInitialDelay,
		StreamRetryMaxDelay:     base.StreamRetryMaxDelay,
	}
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

// SetSessionID binds user-level runtime artifact paths after the UI has
// created or resumed a session. Conversation logs live in SessionDir.
func (s *Session) SetSessionID(id string) {
	if s == nil || strings.TrimSpace(id) == "" {
		return
	}
	if s.Toolkit != nil {
		s.Toolkit.SetSessionID(id)
		s.Toolkit.SetAgentIdentity(id, agentthread.RootPath)
		stateDir := strings.TrimSpace(s.StateDir)
		if stateDir == "" {
			if home, err := statepath.Home(""); err == nil {
				if dir, err := statepath.WorkspaceDir(home, s.RootDir); err == nil {
					stateDir = dir
				}
			}
		}
		if stateDir == "" {
			return
		}
		artifactDir := statepath.SessionArtifactDir(stateDir, id)
		s.Toolkit.SetSessionDir(artifactDir)
		if s.AgentControl != nil {
			s.AgentControl.SetSessionInfo(
				id,
				filepath.Join(artifactDir, "workers"),
				filepath.Join(artifactDir, "threads"),
			)
		}
	}
}

// Cleanup stops session-scoped background work owned by the runtime.
func (s *Session) Cleanup() (process.CleanupResult, error) {
	if s == nil {
		return process.CleanupResult{}, nil
	}
	if s.CronScheduler != nil {
		s.CronScheduler.Stop()
		s.CronScheduler = nil
	}
	if s.CronLock != nil {
		s.CronLock.Release()
		s.CronLock = nil
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

// ResolveInputWindow resolves the effective prompt/input budget. It is often
// lower than the total context window because providers reserve output tokens
// server-side before validating the request.
func ResolveInputWindow(model string, provider config.ProviderConfig, contextWindow int) int {
	if limit := configuredModelInputLimit(model, provider); limit > 0 {
		if cap := codexSubscriptionInputCap(model, provider.Type); cap > 0 && cap < limit {
			return cap
		}
		return limit
	}
	if cap := codexSubscriptionInputCap(model, provider.Type); cap > 0 {
		return cap
	}
	if contextWindow <= 0 {
		return 0
	}
	reserve := providers.MaxOutputTokensFor(model)
	if reserve <= 0 || reserve >= contextWindow {
		return 0
	}
	return contextWindow - reserve
}

func configuredModelInputLimit(model string, provider config.ProviderConfig) int {
	model = strings.TrimSpace(model)
	if model == "" || len(provider.Models) == 0 {
		return 0
	}
	if cfg, ok := provider.Models[model]; ok && cfg.Limit != nil && cfg.Limit.Input > 0 {
		return cfg.Limit.Input
	}
	apiModel := modelcatalog.APIModel(provider, model)
	if apiModel != "" && apiModel != model {
		if cfg, ok := provider.Models[apiModel]; ok && cfg.Limit != nil && cfg.Limit.Input > 0 {
			return cfg.Limit.Input
		}
	}
	return 0
}

const codexSubscriptionGPT5InputCap = 272_000

func codexSubscriptionInputCap(model, providerType string) int {
	if !isCodexSubscriptionProviderType(providerType) {
		return 0
	}
	id := strings.ToLower(strings.TrimSpace(model))
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		id = id[idx+1:]
	}
	if strings.Contains(id, "gpt-5") {
		return codexSubscriptionGPT5InputCap
	}
	return 0
}

func isCodexSubscriptionProviderType(providerType string) bool {
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case "openai-codex", "codex-subscription", "chatgpt-codex":
		return true
	default:
		return false
	}
}

// EnvContextInjector returns dynamic runtime context injected into each model
// request without persisting it in conversation history.
func EnvContextInjector(rootDir string, control *agentcontrol.AgentControl, currentPath string, blockProviders ...func() []wuucontext.Block) func() []providers.ChatMessage {
	return func() []providers.ChatMessage {
		env := wuucontext.Snapshot(rootDir)
		blocks := []wuucontext.Block{wuucontext.EnvironmentBlock(env)}
		if repoMap, ok := wuucontext.RepoMapBlock(rootDir, wuucontext.RepoMapOptions{}); ok {
			blocks = append(blocks, repoMap)
		}
		if recentDiff, ok := wuucontext.RecentDiffBlock(rootDir, wuucontext.RecentDiffOptions{}); ok {
			blocks = append(blocks, recentDiff)
		}
		for _, provider := range blockProviders {
			if provider == nil {
				continue
			}
			blocks = append(blocks, provider()...)
		}
		reminder := wuucontext.FormatSystemReminderBlocks(blocks...)
		if control != nil {
			if agentReminder := control.ActiveTaskReminder(currentPath); agentReminder != "" {
				reminder += "\n\n" + agentReminder
			}
		}
		return []providers.ChatMessage{{
			Role:    "user",
			Name:    wuucontext.SystemReminderMessageName,
			Content: reminder,
		}}
	}
}

func toolkitContextBlockProvider(toolkit *tools.Toolkit) func() []wuucontext.Block {
	if toolkit == nil {
		return nil
	}
	return toolkit.ContextBlocks
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
	return skills.MergeWithBundled(skills.Discover(projectSkillsDir, userSkillsDir))
}

func discoverWorkflows(rootDir, homeDir string) []workflow.Definition {
	projectWorkflowsDir := filepath.Join(rootDir, ".claude", "workflows")
	userWorkflowsDir := ""
	if homeDir != "" {
		userWorkflowsDir = filepath.Join(homeDir, ".claude", "workflows")
	}
	return workflow.Discover(projectWorkflowsDir, userWorkflowsDir)
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
				Name:          name,
				Command:       mcpCfg.Command,
				Args:          mcpCfg.Args,
				URL:           mcpCfg.URL,
				Env:           mcpCfg.Env,
				ToolOverrides: mcpToolOverrides(mcpCfg.ToolOverrides),
			}
			if err := mcpMgr.Add(ctx, serverCfg); err != nil {
				providers.DebugLogf("mcp server %q failed to connect: %v", name, err)
			} else {
				providers.DebugLogf("mcp server %q connected (%d tools)", name, mcpMgr.Status()[name].ToolCount)
			}
		}
	}()
}

func mcpToolOverrides(in map[string]config.MCPToolOverride) map[string]mcp.ToolOverride {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]mcp.ToolOverride, len(in))
	for name, override := range in {
		out[name] = mcp.ToolOverride{
			ReadOnly:        override.ReadOnly,
			ConcurrencySafe: override.ConcurrencySafe,
		}
	}
	return out
}

func ToolPolicyFromConfig(in config.ToolPolicyConfig) tools.ToolPolicy {
	policy, ok := tools.PolicyForProfile(tools.ToolPolicyProfile(strings.TrimSpace(in.Profile)))
	if !ok {
		policy = tools.ToolPolicy{}
	}
	if profile := strings.TrimSpace(in.Profile); profile != "" {
		policy.Profile = tools.ToolPolicyProfile(profile)
	}
	if action := toolPolicyAction(in.DefaultAction); action != "" {
		policy.DefaultAction = action
	}
	if actions := toolPolicyToolActions(in.Tools); len(actions) > 0 {
		policy.ToolActions = mergeToolPolicyToolActions(policy.ToolActions, actions)
	}
	if actions := toolPolicyKindActions(in.Kinds); len(actions) > 0 {
		policy.KindActions = mergeToolPolicyKindActions(policy.KindActions, actions)
	}
	if actions := toolPolicyRiskActions(in.Risks); len(actions) > 0 {
		policy.RiskActions = mergeToolPolicyRiskActions(policy.RiskActions, actions)
	}
	return policy
}

func toolPolicyToolActions(in map[string]string) map[string]tools.ToolPolicyAction {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]tools.ToolPolicyAction, len(in))
	for name, action := range in {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = toolPolicyAction(action)
		}
	}
	return out
}

func toolPolicyKindActions(in map[string]string) map[tools.ToolKind]tools.ToolPolicyAction {
	if len(in) == 0 {
		return nil
	}
	out := make(map[tools.ToolKind]tools.ToolPolicyAction, len(in))
	for kind, action := range in {
		kind = strings.TrimSpace(kind)
		if kind != "" {
			out[tools.ToolKind(kind)] = toolPolicyAction(action)
		}
	}
	return out
}

func toolPolicyRiskActions(in map[string]string) map[tools.ToolRisk]tools.ToolPolicyAction {
	if len(in) == 0 {
		return nil
	}
	out := make(map[tools.ToolRisk]tools.ToolPolicyAction, len(in))
	for risk, action := range in {
		risk = strings.TrimSpace(risk)
		if risk != "" {
			out[tools.ToolRisk(risk)] = toolPolicyAction(action)
		}
	}
	return out
}

func mergeToolPolicyToolActions(base, override map[string]tools.ToolPolicyAction) map[string]tools.ToolPolicyAction {
	out := make(map[string]tools.ToolPolicyAction, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

func mergeToolPolicyKindActions(base, override map[tools.ToolKind]tools.ToolPolicyAction) map[tools.ToolKind]tools.ToolPolicyAction {
	out := make(map[tools.ToolKind]tools.ToolPolicyAction, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

func mergeToolPolicyRiskActions(base, override map[tools.ToolRisk]tools.ToolPolicyAction) map[tools.ToolRisk]tools.ToolPolicyAction {
	out := make(map[tools.ToolRisk]tools.ToolPolicyAction, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		out[k] = v
	}
	return out
}

func toolPolicyAction(action string) tools.ToolPolicyAction {
	return tools.ToolPolicyAction(strings.TrimSpace(action))
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

func newProfileMemoryProvider(wuuHome, profileName string) (*memstore.FileProvider, error) {
	name := strings.TrimSpace(profileName)
	if name == "" || strings.EqualFold(name, config.DefaultAgentName) {
		return nil, fmt.Errorf("profile memory requires a named agent profile")
	}
	if _, _, err := workflow.EnsureProfile(workflow.ProfileEnsureOptions{
		WuuHome: wuuHome,
		Name:    name,
		Source:  "agent",
	}); err != nil {
		return nil, err
	}
	profileStateDir, err := statepath.ProfileDir(wuuHome, name)
	if err != nil {
		return nil, fmt.Errorf("resolve profile state directory: %w", err)
	}
	return memstore.NewFileProvider(statepath.ProfileMemoryDir(profileStateDir))
}

func buildProfileWorkerBasePrompt(rootDir, wuuHome, profileName, userPrompt, providerName, model string, memoryFiles []memory.File, profileMemoryCharLimit, profileUserMemoryCharLimit int, discoveredSkills []skills.Skill, discoveredWorkflows []workflow.Definition) (string, error) {
	name := strings.TrimSpace(profileName)
	if name == "" || strings.EqualFold(name, config.DefaultAgentName) {
		return "", nil
	}
	provider, err := newProfileMemoryProvider(wuuHome, name)
	if err != nil {
		return "", err
	}
	entries := recallProfileMemory(context.Background(), provider)
	return buildBaseSystemPrompt(rootDir, config.DefaultSystemPrompt(), userPrompt, providerName, model, memoryFiles, entries, true, profileMemoryCharLimit, profileUserMemoryCharLimit, discoveredSkills, discoveredWorkflows), nil
}

func (s *Session) RefreshSystemPrompt(providerName, model string) string {
	if s == nil {
		return ""
	}
	var profileMemoryEntries []memstore.Entry
	profileMemoryEnabled := false
	if s.Toolkit != nil && s.Toolkit.Memory() != nil {
		profileMemoryEnabled = true
		profileMemoryEntries = recallProfileMemory(context.Background(), s.Toolkit.Memory())
	}
	baseSystemPrompt := buildBaseSystemPrompt(
		s.RootDir,
		config.DefaultSystemPrompt(),
		s.UserSystemPrompt,
		providerName,
		model,
		s.Memory,
		profileMemoryEntries,
		profileMemoryEnabled,
		s.ProfileMemoryCharLimit,
		s.ProfileUserMemoryCharLimit,
		s.Skills,
		s.Workflows,
	)
	s.BaseSystemPrompt = baseSystemPrompt
	if s.StreamRunner != nil {
		s.StreamRunner.SystemPrompt = baseSystemPrompt
	}
	return baseSystemPrompt
}

func buildBaseSystemPrompt(rootDir, basePrompt, userPrompt, providerName, model string, memoryFiles []memory.File, profileMemoryEntries []memstore.Entry, profileMemoryEnabled bool, profileMemoryCharLimit, profileUserMemoryCharLimit int, discoveredSkills []skills.Skill, discoveredWorkflows []workflow.Definition) string {
	var pb prompt.Builder
	pb.AddSection("base", basePrompt, true)
	pb.AddHarnessAdapter(providerName, model)
	if strings.TrimSpace(userPrompt) != "" {
		pb.AddSection("user_custom_prompt", "# User Custom Instructions\n\nFollow these user-defined instructions unless they conflict with wuu's built-in behavior, safety, or tool-use discipline above.\n\n"+userPrompt, true)
	}
	pb.AddMemory(memoryFiles)
	if profileMemoryEnabled {
		pb.AddProfileMemoryWithLimits(profileMemoryEntries, profileMemoryCharLimit, profileUserMemoryCharLimit)
	}
	pb.AddSkills(discoveredSkills)
	pb.AddWorkflows(discoveredWorkflows)
	if worktree.IsGitRepo(rootDir) {
		gitCtx := prompt.NewGitContext(rootDir)
		pb.AddGitContext(gitCtx.Collect())
	}
	return pb.Build()
}

func recallProfileMemory(ctx context.Context, provider memstore.Provider) []memstore.Entry {
	if provider == nil {
		return nil
	}
	entries, err := provider.Recall(ctx, memstore.RecallQuery{})
	if err != nil {
		return nil
	}
	return entries
}
