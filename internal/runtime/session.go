package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"

	"github.com/blueberrycongee/wuu/internal/activity"
	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agenttemplate"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/config"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/cron"
	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
	"github.com/blueberrycongee/wuu/internal/goalruntime"
	"github.com/blueberrycongee/wuu/internal/hooks"
	"github.com/blueberrycongee/wuu/internal/mcp"
	"github.com/blueberrycongee/wuu/internal/memdir"
	"github.com/blueberrycongee/wuu/internal/memory"
	"github.com/blueberrycongee/wuu/internal/modelbudget"
	"github.com/blueberrycongee/wuu/internal/modelcatalog"
	"github.com/blueberrycongee/wuu/internal/modelprofile"
	"github.com/blueberrycongee/wuu/internal/modelroles"
	"github.com/blueberrycongee/wuu/internal/modelvariant"
	"github.com/blueberrycongee/wuu/internal/participant"
	pluginpkg "github.com/blueberrycongee/wuu/internal/plugin"
	"github.com/blueberrycongee/wuu/internal/pluginhost"
	"github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/prompt"
	"github.com/blueberrycongee/wuu/internal/providerfactory"
	"github.com/blueberrycongee/wuu/internal/provideroptions"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/skills"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/toolledger"
	"github.com/blueberrycongee/wuu/internal/tools"
	"github.com/blueberrycongee/wuu/internal/workspaces"
)

// Options describes the shared agent runtime needed by interactive clients.
// The shape is intentionally UI-neutral so desktop and protocol clients can
// attach without rebuilding the agent.
type Options struct {
	RootDir string
	// WorkspaceID is the stable, location-independent identity of the
	// workspace (the desktop's registered-project id). When set, the workspace
	// state directory is keyed by this id so it survives the project being
	// moved or renamed on disk. Empty for location-anchored roots (the shared
	// 对话 scratch dir, agent homes), which stay keyed by their path.
	WorkspaceID string
	HomeDir     string
	ConfigPath  string
	// ConfigLoadMode records the source model that produced Config so later
	// app-server reloads use the same inputs. The zero value is normal user
	// config plus restricted project overlays.
	ConfigLoadMode ConfigLoadMode
	Config         config.Config
	ProviderName   string
	ModelOverride  string
	NoTools        bool
}

// ConfigLoadMode identifies the three supported config source models. It is
// intentionally not a boolean: an explicitly trusted project config includes
// its settings layers, while an explicit file is loaded alone.
type ConfigLoadMode uint8

const (
	ConfigLoadNormal ConfigLoadMode = iota
	ConfigLoadFile
	ConfigLoadProject
)

// Session owns one initialized local agent runtime: provider client, tool
// executor, hooks, MCP, skills, memory, coordinator, process manager, and the
// stream runner. UI surfaces should depend on this instead of reassembling the
// pieces themselves.
type Session struct {
	ProviderName             string
	Model                    string
	RootDir                  string
	WorkspaceID              string
	StateDir                 string
	ConfigPath               string
	HomeDir                  string
	ConfigLoadMode           ConfigLoadMode
	SessionDir               string
	StreamRunner             *agent.StreamRunner
	TitleClient              providers.Client
	HookDispatcher           *hooks.Dispatcher
	Skills                   []skills.Skill
	AgentTemplates           []agenttemplate.Template
	AgentTemplateDiagnostics []agenttemplate.Diagnostic
	Plugins                  []pluginpkg.Plugin
	PluginHost               *pluginhost.Host
	Memory                   []memory.File
	// MemdirEnabled reports whether the file-directory memory (user
	// notebook teaching + index injection and file-scope whitelist) is
	// active for this session. False when Memory.Disable is set.
	MemdirEnabled            bool
	DreamIntervalDays        int
	AgentControl             *agentcontrol.AgentControl
	ProcessManager           *process.Manager
	Toolkit                  *tools.Toolkit
	ActivityRegistry         *activity.Registry
	WorkerClient             providers.StreamClient
	ModelRoles               modelroles.Set
	ModelBudget              modelbudget.Budget
	WorkerModelBudget        modelbudget.Budget
	BaseSystemPrompt         string
	BaseSystemPromptSections []prompt.SectionInfo
	UserSystemPrompt         string
	// SessionDate is the session-start frozen calendar date (YYYY-MM-DD)
	// stamped into the "# Environment" system section. Freezing it here keeps
	// the cached system prefix byte-stable across turns and thread rebuilds:
	// a long-lived session that crosses a day boundary keeps its start date
	// instead of churning the prompt cache. Real-time clock reads belong in the
	// per-turn message stream, never in this cached prefix.
	SessionDate                 string
	WuuHome                     string
	Permissions                 config.ResolvedPermissions
	CoordinatorPreamble         string
	ExperimentalCoordinatorMode bool
	ToolLoadingPreference       config.ToolLoadingMode
	ToolLoadingMode             config.ToolLoadingMode
	ToolSearchEnabled           bool
	NativeDeferredToolDiscovery bool
	ExperimentalDeferredBundles bool
	DeferredToolCatalogPrompt   string
	CronScheduler               *cron.Scheduler
	CronLock                    *cron.Lock
	ReadinessIssues             []ReadinessIssue
	InferenceJournalRuntime     *session.InferenceJournalRuntime
}

type ReadinessIssue struct {
	Code     string
	Provider string
	Message  string
}

// ThreadRuntime owns the mutable execution state for one app-server
// conversation. The desktop app can run multiple ThreadRuntimes at once; each
// one has its own StreamRunner, Toolkit Env, usage tracker, and AgentControl.
type ThreadRuntime struct {
	StreamRunner      *agent.StreamRunner
	Toolkit           *tools.Toolkit
	AgentControl      *agentcontrol.AgentControl
	GoalRuntime       *goalruntime.Runtime
	ActivityRegistry  *activity.Registry
	ModelBudget       modelbudget.Budget
	WorkerModelBudget modelbudget.Budget
}

// resolveWorkspaceStateDir returns the workspace state directory, keyed by the
// stable workspace id when present (so it survives the project moving on disk)
// and otherwise by the filesystem path (location-anchored roots that never
// move: the shared 对话 scratch dir, agent homes, worktree checkouts).
func resolveWorkspaceStateDir(wuuHome, workspaceID, rootDir string) (string, error) {
	if id := strings.TrimSpace(workspaceID); id != "" {
		return statepath.WorkspaceDirByID(wuuHome, id)
	}
	return statepath.WorkspaceDir(wuuHome, rootDir)
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
	workspaceID := strings.TrimSpace(opts.WorkspaceID)
	workspaceStateDir, err := resolveWorkspaceStateDir(wuuHome, workspaceID, rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace state directory: %w", err)
	}
	sessionDir := statepath.SessionsDir(wuuHome)
	journalRuntime, err := session.NewInferenceJournalRuntime(sessionDir, inferenceJournalWorkspaceScope(workspaceID, rootDir))
	if err != nil {
		return nil, fmt.Errorf("initialize inference journal: %w", err)
	}
	journalOwned := true
	defer func() {
		if journalOwned {
			_ = journalRuntime.Close()
		}
	}()
	workspaceJournal := journalRuntime.ForOwner("workspace-runtime")
	userSystemPrompt := cfg.Agent.UserSystemPrompt()
	permissions := config.ResolveAgentPermissions(cfg.Agent)

	providerCfg, resolvedName, err := cfg.ResolveProvider(opts.ProviderName)
	if err != nil {
		return nil, err
	}
	if opts.ModelOverride != "" {
		providerCfg.Model = opts.ModelOverride
	}
	roleSelections, err := modelroles.Resolve(cfg, modelroles.ResolveOptions{
		ProviderName:   resolvedName,
		ProviderConfig: providerCfg,
		Model:          providerCfg.Model,
		Effort:         cfg.Agent.Effort,
		Variant:        cfg.Agent.Variant,
	})
	if err != nil {
		return nil, err
	}
	mainRole := roleSelections.Main
	ruleProviderName, ruleProviderCfg := mainRole.RuleProvider, mainRole.RuleProviderConfig
	toolModeModel := mainRole.APIModel

	client, clientErr := providerfactory.BuildRuntimeStreamClient(ruleProviderCfg, resolvedName)
	if client == nil {
		return nil, clientErr
	}
	var readinessIssues []ReadinessIssue
	if clientErr != nil {
		readinessIssues = append(readinessIssues, ReadinessIssue{Code: "credential_missing", Provider: resolvedName, Message: clientErr.Error()})
	}
	titleClient := providers.Client(client)
	if !roleSelections.Title.Inherited {
		roleClient, roleErr := providerfactory.BuildRuntimeStreamClient(roleSelections.Title.RuleProviderConfig, roleSelections.Title.Provider)
		if roleClient == nil {
			return nil, fmt.Errorf("build title client: %w", roleErr)
		}
		if roleErr != nil {
			readinessIssues = append(readinessIssues, ReadinessIssue{Code: "credential_missing", Provider: roleSelections.Title.Provider, Message: roleErr.Error()})
		}
		titleClient = roleClient
	}

	providers.InitDebugLog(statepath.LogDir(wuuHome))
	setupCatwalk(cfg)

	discoveredPlugins := discoverPlugins(rootDir, wuuHome)
	pluginHost := startPluginHost(discoveredPlugins, rootDir, wuuHome)
	hookDispatcher := buildHookDispatcher(cfg, discoveredPlugins, providers.Client(client), toolModeModel, workspaceJournal)
	discoveredSkills := discoverSkills(rootDir, opts.HomeDir, wuuHome, discoveredPlugins)
	discoveredAgentTemplates := discoverAgentTemplates(rootDir, opts.HomeDir)

	processMgr, err := process.NewManager(rootDir, statepath.RuntimeDir(workspaceStateDir))
	if err != nil {
		return nil, err
	}
	activityRegistry := activity.NewRegistry()

	// File-directory memory (memory-redesign M1): the user notebook index is
	// read once here — session creation is one of the two allowed
	// prompt-prefix change points (contract §4) — and the teaching + index
	// are injected into the base prompt below. Memory.Disable is the sole
	// configuration gate for this durable, global per-user memory.
	memdirEnabled := !cfg.Memory.Disable
	var memdirTeaching, memdirWorkerTeaching, memdirIndex string
	if memdirEnabled {
		// One-time lazy migration of the retired memory surfaces
		// (entries.jsonl, flat participant MEMORY.md, agent-home orphans)
		// into notebook topic files + index lines (contract §7). After the
		// first successful run the marker file makes this a single stat().
		if err := memdir.Migrate(wuuHome); err != nil {
			providers.DebugLogf("memdir migration: %v", err)
		}
		userNotebook := memdir.UserMemdir(wuuHome)
		if err := memdir.EnsureDir(userNotebook); err != nil {
			providers.DebugLogf("ensure user memory notebook: %v", err)
		}
		memdirTeaching = memdir.SessionTeaching(userNotebook)
		memdirWorkerTeaching = memdir.WorkerTeaching(userNotebook)
		if snap, err := memdir.ReadIndex(userNotebook); err == nil {
			memdirIndex = snap.Content
		} else {
			providers.DebugLogf("read user memory index: %v", err)
		}
	}

	var toolExecutor agent.ToolExecutor
	var toolkit *tools.Toolkit
	toolLoadingPreference := cfg.Agent.ToolLoadingPreference()
	toolLoadingMode, toolSearchEnabled, nativeDeferredDiscovery := resolveToolLoadingModeForProvider(toolLoadingPreference, ruleProviderCfg, toolModeModel, mainRole.ProviderOptions)
	experimentalDeferredBundles := cfg.Agent.ExperimentalDeferredToolBundles
	if !opts.NoTools {
		kit, newErr := tools.New(rootDir)
		if newErr != nil {
			return nil, newErr
		}
		kit.SetStateDir(workspaceStateDir)
		kit.SetProcessManager(processMgr)
		kit.SetSkills(discoveredSkills)
		ConfigureToolkitPermissions(kit, permissions)
		kit.ConfigureSurfaceForProviderModel(ruleProviderName, toolModeModel, true)
		kit.SetToolSearchEnabled(toolSearchEnabled)
		kit.SetExperimentalDeferredToolBundles(experimentalDeferredBundles)
		kit.SetNativeDeferredToolDiscovery(nativeDeferredDiscovery)
		kit.SetActivityRegistry(activityRegistry)
		var fileScopeExtras []string
		// User memory is a file-directory notebook injected into the prompt
		// and written with ordinary file tools inside the boundary roots below.
		if memdirEnabled {
			fileScopeExtras = append(fileScopeExtras, memdir.UserMemdir(wuuHome))
		}
		kit.SetFileScopeRoots(workspaces.BoundaryRoots(kit.RootDir(), wuuHome, fileScopeExtras...))
		kit.SetOnFileChanged(func(absPath string) {
			_, _ = hookDispatcher.Dispatch(context.Background(), hooks.FileChanged, &hooks.Input{
				CWD:      rootDir,
				FilePath: absPath,
			})
		})
		toolkit = kit
		toolExecutor = hooks.NewHookedExecutor(kit, hookDispatcher, "", rootDir)
		toolExecutor = newPluginToolExecutor(toolExecutor, pluginHost, "", rootDir)
		connectMCPServers(cfg, discoveredPlugins, toolkit)
	}

	memoryFiles := discoverMemory(rootDir, opts.HomeDir, cfg.Memory)
	// Freeze the environment date once at session start. Every system-prompt
	// build in this session (base, worker, per-thread rebuild) reuses this
	// frozen value so the cached system prefix does not drift when threads
	// rebuild or the wall clock crosses midnight during a long session.
	sessionDate := wuucontext.Snapshot(rootDir).Date
	mainSurface := activeSurface(toolkit)
	if toolkit != nil {
		if err := toolkit.ValidateActiveToolSurfaceForProvider(providers.ToolSurfaceValidationTarget{
			ProviderKind: ruleProviderCfg.Type,
			ProviderName: resolvedName,
			Model:        toolModeModel,
		}); err != nil {
			return nil, err
		}
	}
	deferredToolCatalogPrompt, err := deferredToolCatalogPromptForToolkit(toolkit)
	if err != nil {
		return nil, err
	}
	mainSurface.DeferredToolCatalog = deferredToolCatalogPrompt
	baseSystemPromptResult := buildBaseSystemPromptResult(rootDir, sessionDate, config.DefaultSystemPrompt(), userSystemPrompt, resolvedName, toolModeModel, mainSurface, memoryFiles, memdirTeaching, memdirIndex, discoveredSkills)
	baseSystemPrompt := baseSystemPromptResult.Content
	baseSystemPromptSections := agentPromptSections(baseSystemPromptResult.Sections)

	if toolkit != nil {
		if err := agentcontrol.EnsureSharedDir(workspaceStateDir); err != nil {
			return nil, fmt.Errorf("ensure shared dir: %w", err)
		}
	}

	var agentControl *agentcontrol.AgentControl
	var coordinatorPreamble string
	var workerClient providers.StreamClient
	workerModelBudget := ResolveModelBudget(
		roleSelections.Worker.Model,
		roleSelections.Worker.RuleProviderConfig,
		cfg.Agent.MaxContextTokens,
	)
	if toolkit != nil {
		workerToolProviderName := roleSelections.Worker.RuleProvider
		workerToolModeModel := roleSelections.Worker.APIModel
		_, workerToolSearchEnabled, workerNativeDeferredDiscovery := resolveToolLoadingForProvider(cfg.Agent, roleSelections.Worker.RuleProviderConfig, workerToolModeModel, roleSelections.Worker.ProviderOptions)
		workerToolSurface := compiledSurfaceForProviderModel(workerToolProviderName, workerToolModeModel)
		// Fill the worker deferred-tool catalog the same way mainSurface is
		// filled above (consistency-repair #13: this was left empty while the
		// worker prompt taught catalog lookups through tool_search).
		workerDeferredCatalog, catErr := workerDeferredToolCatalogPromptForToolkit(toolkit, workerToolProviderName, workerToolModeModel, workerToolSearchEnabled, experimentalDeferredBundles)
		if catErr != nil {
			return nil, catErr
		}
		workerToolSurface.DeferredToolCatalog = workerDeferredCatalog
		workerBaseSystemPrompt := buildBaseSystemPromptContent(rootDir, sessionDate, config.WorkerSystemPrompt(), userSystemPrompt, workerToolProviderName, workerToolModeModel, workerToolSurface, memoryFiles, memdirWorkerTeaching, memdirIndex, discoveredSkills)
		var werr error
		workerClient, werr = providerfactory.BuildStreamClient(roleSelections.Worker.RuleProviderConfig, roleSelections.Worker.Provider)
		if werr != nil {
			if providerfactory.IsCredentialError(werr) {
				workerClient = &providers.UnavailableClient{Reason: werr.Error()}
				readinessIssues = append(readinessIssues, ReadinessIssue{Code: "credential_missing", Provider: roleSelections.Worker.Provider, Message: werr.Error()})
			} else {
				return nil, fmt.Errorf("build worker client: %w", werr)
			}
		}

		c, cerr := agentcontrol.New(agentcontrol.Config{
			Client:                         workerClient,
			DefaultModel:                   roleSelections.Worker.APIModel,
			DefaultEffort:                  roleSelections.Worker.LegacyEffort,
			DefaultOptions:                 modelvariant.CloneOptions(roleSelections.Worker.ProviderOptions),
			DefaultContextWindow:           workerModelBudget.ContextWindowTokens,
			DefaultMaxInputTokens:          workerModelBudget.InputLimitTokens,
			DefaultOutputReserveTokens:     workerModelBudget.OutputReserveTokens,
			DefaultCompactThresholdTokens:  workerModelBudget.CompactThresholdTokens,
			DefaultTemperature:             cfg.Agent.Temperature,
			DefaultCompactThresholdPct:     cfg.Agent.CompactThresholdPct,
			DefaultCompactKeepRecentTokens: cfg.Agent.CompactKeepRecentTokens,
			DefaultDisableAutoCompact:      cfg.Agent.DisableAutoCompact,
			ParentRepo:                     rootDir,
			WorktreeRoot:                   statepath.WorktreeRoot(workspaceStateDir),
			SessionID:                      "session-pending",
			HistoryDir:                     "",
			WorkerSysPrompt:                workerBaseSystemPrompt,
			WorkerPrompt: func(workerRoot string, wt agentcontrol.WorkerType, meta agentthread.Metadata, isolation agentcontrol.IsolationMode) (string, error) {
				return buildWorkerBasePrompt(workerRoot, sessionDate, wuuHome, userSystemPrompt, workerToolProviderName, workerToolModeModel, workerToolSurface, memoryFiles, memdirEnabled, discoveredSkills), nil
			},
			WorkerFactory: func(workerRoot string, wt agentcontrol.WorkerType, meta agentthread.Metadata) (agent.ToolExecutor, error) {
				wkit, werr := toolkit.CloneForRoot(workerRoot)
				if werr != nil {
					return nil, werr
				}
				// Reset the inherited file-scope whitelist: a worker gets the
				// standard boundary roots, but not the user notebook extra.
				wkit.SetFileScopeRoots(workspaces.BoundaryRoots(workerRoot, wuuHome))
				workerStateDir := workspaceStateDir
				if workerRoot != rootDir {
					if dir, err := statepath.WorkspaceDir(wuuHome, workerRoot); err == nil {
						workerStateDir = dir
					}
				}
				wkit.SetStateDir(workerStateDir)
				wkit.SetProcessManager(processMgr)
				wkit.SetSkills(discoveredSkills)
				wkit.SetAgentControl(agentControl)
				wkit.ConfigureSurfaceForProviderModel(workerToolProviderName, workerToolModeModel, false)
				wkit.SetToolSearchEnabled(workerToolSearchEnabled)
				wkit.SetExperimentalDeferredToolBundles(experimentalDeferredBundles)
				wkit.SetNativeDeferredToolDiscovery(workerNativeDeferredDiscovery)
				wkit.SetAgentIdentity(meta.ID, meta.Path)
				participantSpeech := agentControl != nil && agentControl.ParticipantSpeechEnabled(meta.ID)
				applyWorkerToolFilter(wkit, wt, participantSpeech)
				// Group management is resident-only; spawned runs never
				// receive the backend.
				wkit.SetGroupManager(nil)
				if participantSpeech {
					wkit.SetParticipantSpeechEnabled(true)
					// Participant task runs share the resident file scope:
					// worker home + registered workspaces + system temp.
					wkit.SetFileScopeRoots(workspaces.BoundaryRoots(workerRoot, wuuHome))
				}
				return wkit, nil
			},
			ParticipantStore: sessionParticipantStore{sessDir: statepath.SessionsDir(wuuHome)},
			MaxParallel:      5,
			InferenceJournal: workspaceJournal,
			ToolLedgerFactory: func(ownerID string) (*toolledger.Ledger, error) {
				return toolledger.New(sessionDir, ownerID)
			},
		})
		if cerr == nil {
			agentControl = c
			toolkit.SetAgentControl(agentControl)
			coordinatorPreamble = agentcontrol.SystemPromptPreamble()
		}
	}

	modelBudget := ResolveModelBudget(
		providerCfg.Model,
		ruleProviderCfg,
		cfg.Agent.MaxContextTokens,
	)
	dreamIntervalDays := cfg.Memory.DreamIntervalDaysValue()
	if cfg.Memory.Disable {
		dreamIntervalDays = 0
	}
	var afterTurnHooks []func(context.Context, *agent.StreamRunner, []providers.ChatMessage, agent.LoopResult)
	if toolkit != nil {
		if dreamScheduler := newSessionDreamScheduler(rootDir, workspaceStateDir, func() string { return toolkit.SessionDir() }, dreamIntervalDays); dreamScheduler != nil {
			afterTurnHooks = append(afterTurnHooks, dreamScheduler.AfterTurn)
		}
	}
	afterTurn := chainAfterTurn(afterTurnHooks...)

	streamRunner := &agent.StreamRunner{
		Client:                      client,
		Tools:                       toolExecutor,
		Model:                       providerCfg.Model,
		APIModel:                    modelcatalog.APIModel(ruleProviderCfg, providerCfg.Model),
		SystemPrompt:                baseSystemPrompt,
		SystemPromptSections:        baseSystemPromptSections,
		MaxSteps:                    cfg.Agent.MaxSteps,
		Temperature:                 cfg.Agent.Temperature,
		Effort:                      mainRole.LegacyEffort,
		Variant:                     mainRole.Variant,
		ProviderOptions:             modelvariant.CloneOptions(mainRole.ProviderOptions),
		NativeDeferredToolDiscovery: nativeDeferredDiscovery,
		ContextWindowOverride:       modelBudget.ContextWindowTokens,
		MaxInputTokens:              modelBudget.InputLimitTokens,
		OutputReserveTokens:         modelBudget.OutputReserveTokens,
		CompactThresholdTokens:      modelBudget.CompactThresholdTokens,
		CompactThresholdPct:         cfg.Agent.CompactThresholdPct,
		CompactKeepRecentTokens:     cfg.Agent.CompactKeepRecentTokens,
		DisableAutoCompact:          cfg.Agent.DisableAutoCompact,
		BeforeRequestContext:        RuntimeContextInjector(agentControl, agentthread.RootPath, toolkitContextBlockProvider(toolkit)),
		BeforeRequest:               pluginRequestInterceptor(pluginHost, resolvedName, "", rootDir),
		AfterTurn:                   afterTurn,
		InferenceOperationKind:      providers.InferenceOperationAgentRound,
		InferenceWorkloadProfile:    providers.InferenceProfileInteractive,
		InferenceJournal:            workspaceJournal,
	}

	configLoadMode := opts.ConfigLoadMode
	// Keep direct runtime callers that pass a non-user ConfigPath pinned to
	// that file. Normal product paths set the mode explicitly when they trust
	// a complete project config rather than one standalone file.
	if configLoadMode == ConfigLoadNormal && strings.TrimSpace(opts.ConfigPath) != "" && !isUserConfigPath(opts.ConfigPath, opts.HomeDir) {
		configLoadMode = ConfigLoadFile
	}

	runtimeSession := &Session{
		ProviderName:                resolvedName,
		Model:                       providerCfg.Model,
		RootDir:                     rootDir,
		WorkspaceID:                 workspaceID,
		StateDir:                    workspaceStateDir,
		ConfigPath:                  opts.ConfigPath,
		HomeDir:                     opts.HomeDir,
		ConfigLoadMode:              configLoadMode,
		SessionDir:                  sessionDir,
		StreamRunner:                streamRunner,
		TitleClient:                 titleClient,
		HookDispatcher:              hookDispatcher,
		Skills:                      discoveredSkills,
		AgentTemplates:              discoveredAgentTemplates.Templates,
		AgentTemplateDiagnostics:    discoveredAgentTemplates.Diagnostics,
		Plugins:                     discoveredPlugins,
		PluginHost:                  pluginHost,
		Memory:                      memoryFiles,
		MemdirEnabled:               memdirEnabled,
		DreamIntervalDays:           dreamIntervalDays,
		AgentControl:                agentControl,
		ProcessManager:              processMgr,
		Toolkit:                     toolkit,
		ActivityRegistry:            activityRegistry,
		WorkerClient:                workerClient,
		ModelRoles:                  roleSelections,
		ModelBudget:                 modelBudget,
		WorkerModelBudget:           workerModelBudget,
		BaseSystemPrompt:            baseSystemPrompt,
		BaseSystemPromptSections:    baseSystemPromptResult.Sections,
		UserSystemPrompt:            userSystemPrompt,
		SessionDate:                 sessionDate,
		WuuHome:                     wuuHome,
		Permissions:                 permissions,
		CoordinatorPreamble:         coordinatorPreamble,
		ExperimentalCoordinatorMode: cfg.Agent.ExperimentalCoordinatorMode,
		ToolLoadingPreference:       toolLoadingPreference,
		ToolLoadingMode:             toolLoadingMode,
		ToolSearchEnabled:           toolSearchEnabled,
		NativeDeferredToolDiscovery: nativeDeferredDiscovery,
		ExperimentalDeferredBundles: experimentalDeferredBundles,
		DeferredToolCatalogPrompt:   deferredToolCatalogPrompt,
		ReadinessIssues:             readinessIssues,
		InferenceJournalRuntime:     journalRuntime,
	}
	journalOwned = false
	return runtimeSession, nil
}

func inferenceJournalWorkspaceScope(workspaceID, rootDir string) string {
	if id := strings.TrimSpace(workspaceID); id != "" {
		return "workspace-id:" + id
	}
	root := cleanRuntimeRoot(rootDir)
	sum := sha256.Sum256([]byte(root))
	return "workspace-path:" + hex.EncodeToString(sum[:16])
}

func (s *Session) InferenceJournalForOwner(ownerID string) providers.InferenceJournal {
	if s == nil || s.InferenceJournalRuntime == nil {
		return nil
	}
	return s.InferenceJournalRuntime.ForOwner(ownerID)
}

// LoadEffectiveConfig reloads the same source model that created the session.
// Normal sessions rebuild the user base plus restricted project overlays,
// explicit files remain pinned to one file, and explicitly trusted project
// configs continue to include their settings layers.
func (s *Session) LoadEffectiveConfig() (config.Config, string, error) {
	if s == nil {
		return config.Config{}, "", errors.New("runtime session is nil")
	}
	switch s.ConfigLoadMode {
	case ConfigLoadNormal:
		return config.LoadFrom(s.RootDir, s.HomeDir)
	case ConfigLoadFile:
		return config.LoadPath(s.ConfigPath)
	case ConfigLoadProject:
		return config.LoadProjectConfig(s.RootDir)
	default:
		return config.Config{}, "", fmt.Errorf("unknown config load mode %d", s.ConfigLoadMode)
	}
}

func isUserConfigPath(path, home string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	want := []string{statepath.LegacyConfigPath(home)}
	if canonical, err := statepath.ConfigPath(home); err == nil {
		want = append(want, canonical)
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, candidate := range want {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		cleanCandidate, err := filepath.Abs(candidate)
		if err == nil && cleanPath == cleanCandidate {
			return true
		}
	}
	return false
}

func resolveToolLoadingForProvider(agentCfg config.AgentConfig, providerCfg config.ProviderConfig, model string, providerOptions map[string]any) (config.ToolLoadingMode, bool, bool) {
	return resolveToolLoadingModeForProvider(agentCfg.ToolLoadingPreference(), providerCfg, model, providerOptions)
}

func resolveToolLoadingModeForProvider(mode config.ToolLoadingMode, providerCfg config.ProviderConfig, model string, providerOptions map[string]any) (config.ToolLoadingMode, bool, bool) {
	switch mode {
	case config.ToolLoadingFlat:
		return mode, false, false
	case config.ToolLoadingNative:
		if providerfactory.SupportsNativeToolDiscovery(providerCfg, model, providerOptions) {
			return mode, true, true
		}
		return config.ToolLoadingWuuToolSearch, true, false
	case config.ToolLoadingWuuToolSearch:
		return mode, true, false
	default:
		if providerfactory.SupportsNativeToolDiscoveryByDefault(providerCfg, model, providerOptions) {
			return config.ToolLoadingNative, true, true
		}
		if providerfactory.ShouldFallbackToWuuToolSearchByDefault(providerCfg, model, providerOptions) {
			return config.ToolLoadingWuuToolSearch, true, false
		}
		return config.ToolLoadingFlat, false, false
	}
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

	lock := cron.NewLock(statepath.ScheduledTasksLockPath(stateDir))
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
	goalStore := s.startScheduledGoal(task, prompt)
	runner := s.StreamRunner
	if threadRT, err := s.NewThreadRuntime(scheduledCronSessionID("cron-task", task.ID)); err == nil && threadRT.StreamRunner != nil {
		runner = threadRT.StreamRunner
	} else if err != nil {
		providers.DebugLogf("cron prompt task %q using shared runner after thread runtime error: %v", task.ID, err)
	}
	if _, err := runner.Run(context.Background(), prompt); err != nil {
		providers.DebugLogf("cron prompt task %q failed: %v", task.ID, err)
		if goalStore != nil {
			_, _ = goalStore.AddFailure(goalrunner.Failure{
				Step:     goalrunner.StepExecution,
				Kind:     "scheduled_task_failed",
				Source:   "cron",
				SourceID: task.ID,
				Message:  err.Error(),
			})
		}
		return
	}
	if goalStore != nil {
		_, _ = goalStore.MarkStepCompleted(goalrunner.StepExecution)
		_, _ = goalStore.AddProgress(goalrunner.StepSummary, "Scheduled task execution completed.")
		_, _ = goalStore.SetStatus(goalrunner.StatusCompleted, goalrunner.StepSummary, "scheduled task execution completed")
	}
}

func (s *Session) startScheduledGoal(task cron.Task, prompt string) *goalrunner.Store {
	stateDir := strings.TrimSpace(s.StateDir)
	if stateDir == "" {
		return nil
	}
	goalID := scheduledCronSessionID("cron-goal", task.ID)
	store := goalrunner.NewStore(filepath.Join(stateDir, "goals", goalID))
	triggerPayload := map[string]string{
		"task_id":   strings.TrimSpace(task.ID),
		"cron":      strings.TrimSpace(task.Cron),
		"kind":      "prompt",
		"recurring": fmt.Sprintf("%t", task.Recurring),
	}
	runner := goalrunner.Runner{Store: store}
	if _, err := runner.Init(context.Background(), goalrunner.Spec{
		ID:            goalID,
		Goal:          "Scheduled prompt task",
		Task:          strings.TrimSpace(prompt),
		AssignedAgent: "cron-scheduler",
		Trigger: goalrunner.Trigger{
			Type:    "scheduled",
			Source:  "cron",
			Payload: triggerPayload,
		},
	}); err != nil {
		providers.DebugLogf("cron prompt task %q failed to initialize goal: %v", task.ID, err)
		return nil
	}
	if _, err := store.SetStatus(goalrunner.StatusRunning, goalrunner.StepExecution, "scheduled task fired"); err != nil {
		providers.DebugLogf("cron prompt task %q failed to mark goal running: %v", task.ID, err)
		return store
	}
	if _, _, err := store.AddArtifact("trigger.md", "trigger", renderScheduledGoalTrigger(task, prompt)); err != nil {
		providers.DebugLogf("cron prompt task %q failed to write goal trigger artifact: %v", task.ID, err)
	}
	return store
}

func renderScheduledGoalTrigger(task cron.Task, prompt string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Scheduled Trigger\n\n")
	fmt.Fprintf(&b, "- Task: %s\n", strings.TrimSpace(task.ID))
	fmt.Fprintf(&b, "- Cron: %s\n", strings.TrimSpace(task.Cron))
	fmt.Fprintf(&b, "- Recurring: %t\n", task.Recurring)
	fmt.Fprintf(&b, "- Kind: prompt\n")
	fmt.Fprintf(&b, "\n## Prompt\n\n%s\n", strings.TrimSpace(prompt))
	return b.String()
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
	return s.NewThreadRuntimeForRoot(sessionID, s.RootDir)
}

// NewThreadRuntimeForRoot creates a per-conversation execution runtime whose
// tools are rooted at rootDir while durable artifacts stay in the parent
// workspace state directory.
func (s *Session) NewThreadRuntimeForRoot(sessionID, rootDir string) (*ThreadRuntime, error) {
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
	threadRoot := strings.TrimSpace(rootDir)
	if threadRoot == "" {
		threadRoot = s.RootDir
	}
	if abs, err := filepath.Abs(threadRoot); err == nil {
		threadRoot = abs
	}
	if ev, err := filepath.EvalSymlinks(threadRoot); err == nil {
		threadRoot = ev
	}

	stateDir := strings.TrimSpace(s.StateDir)
	if stateDir == "" {
		home, err := statepath.Home("")
		if err != nil {
			return nil, fmt.Errorf("resolve wuu home: %w", err)
		}
		stateDir, err = resolveWorkspaceStateDir(home, s.WorkspaceID, s.RootDir)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace state directory: %w", err)
		}
	}
	artifactDir := statepath.SessionArtifactDir(stateDir, id)
	goalRuntime := goalruntime.NewRuntime(goalruntime.NewStore(statepath.ThreadGoalRuntimePath(stateDir, id)))
	wuuHome := strings.TrimSpace(s.WuuHome)
	if wuuHome == "" {
		if home, err := statepath.Home(""); err == nil {
			wuuHome = home
		}
	}

	var (
		kit          *tools.Toolkit
		agentControl *agentcontrol.AgentControl
		toolExecutor = s.StreamRunner.Tools
	)
	threadProcessManager := s.ProcessManager
	if !sameRuntimeRoot(threadRoot, s.RootDir) && threadProcessManager != nil {
		manager, err := process.NewManager(threadRoot, statepath.RuntimeDir(stateDir))
		if err != nil {
			return nil, fmt.Errorf("thread process manager: %w", err)
		}
		threadProcessManager = manager
	}

	if s.Toolkit != nil {
		workerClient := s.WorkerClient
		if workerClient == nil {
			workerClient = s.StreamRunner.Client
		}
		if workerClient != nil {
			var control *agentcontrol.AgentControl
			workerModel := s.Model
			if roleModel := strings.TrimSpace(s.ModelRoles.Worker.APIModel); roleModel != "" {
				workerModel = roleModel
			}
			workerModelBudget := s.WorkerModelBudget
			workerToolProviderName := s.ModelRoles.Worker.RuleProvider
			workerToolModeModel := workerModel
			_, workerToolSearchEnabled, workerNativeDeferredDiscovery := resolveToolLoadingModeForProvider(s.ToolLoadingPreference, s.ModelRoles.Worker.RuleProviderConfig, workerToolModeModel, s.ModelRoles.Worker.ProviderOptions)
			workerToolSurface := compiledSurfaceForProviderModel(workerToolProviderName, workerToolModeModel)
			// Fill the worker deferred-tool catalog like the session build
			// path does (consistency-repair #13).
			workerDeferredCatalog, catErr := workerDeferredToolCatalogPromptForToolkit(s.Toolkit, workerToolProviderName, workerToolModeModel, workerToolSearchEnabled, s.ExperimentalDeferredBundles)
			if catErr != nil {
				return nil, catErr
			}
			workerToolSurface.DeferredToolCatalog = workerDeferredCatalog
			// Thread creation is an allowed prompt-prefix change point, so
			// the worker base prompt reads the user notebook index fresh.
			workerBaseSystemPrompt := buildWorkerBasePrompt(
				threadRoot,
				s.SessionDate,
				wuuHome,
				s.UserSystemPrompt,
				workerToolProviderName,
				workerToolModeModel,
				workerToolSurface,
				s.Memory,
				s.MemdirEnabled,
				s.Skills,
			)
			control, _ = agentcontrol.New(agentcontrol.Config{
				Client:                         workerClient,
				DefaultModel:                   workerModel,
				DefaultEffort:                  s.ModelRoles.Worker.LegacyEffort,
				DefaultOptions:                 modelvariant.CloneOptions(s.ModelRoles.Worker.ProviderOptions),
				DefaultContextWindow:           workerModelBudget.ContextWindowTokens,
				DefaultMaxInputTokens:          workerModelBudget.InputLimitTokens,
				DefaultOutputReserveTokens:     workerModelBudget.OutputReserveTokens,
				DefaultCompactThresholdTokens:  workerModelBudget.CompactThresholdTokens,
				DefaultCompactThresholdPct:     s.StreamRunner.CompactThresholdPct,
				DefaultCompactKeepRecentTokens: s.StreamRunner.CompactKeepRecentTokens,
				DefaultDisableAutoCompact:      s.StreamRunner.DisableAutoCompact,
				ParentRepo:                     threadRoot,
				WorktreeRoot:                   statepath.WorktreeRoot(stateDir),
				SessionID:                      id,
				HistoryDir:                     filepath.Join(artifactDir, "workers"),
				ThreadDir:                      filepath.Join(artifactDir, "threads"),
				HarnessDir:                     filepath.Join(artifactDir, "harness"),
				WorkerSysPrompt:                workerBaseSystemPrompt,
				WorkerPrompt: func(workerRoot string, wt agentcontrol.WorkerType, meta agentthread.Metadata, isolation agentcontrol.IsolationMode) (string, error) {
					return buildWorkerBasePrompt(workerRoot, s.SessionDate, wuuHome, s.UserSystemPrompt, workerToolProviderName, workerToolModeModel, workerToolSurface, s.Memory, s.MemdirEnabled, s.Skills), nil
				},
				WorkerFactory: func(workerRoot string, wt agentcontrol.WorkerType, meta agentthread.Metadata) (agent.ToolExecutor, error) {
					parentKit := kit
					if parentKit == nil {
						parentKit = s.Toolkit
					}
					workerKit, err := parentKit.CloneForRoot(workerRoot)
					if err != nil {
						return nil, err
					}
					// Reset the inherited file-scope whitelist: a worker gets
					// the standard boundary roots, but not the user notebook
					// extra.
					workerKit.SetFileScopeRoots(workspaces.BoundaryRoots(workerRoot, wuuHome))
					workerKit.ConfigureSurfaceForProviderModel(workerToolProviderName, workerToolModeModel, false)
					workerStateDir := stateDir
					if !sameRuntimeRoot(workerRoot, threadRoot) {
						if home, err := statepath.Home(""); err == nil {
							if dir, err := statepath.WorkspaceDir(home, workerRoot); err == nil {
								workerStateDir = dir
							}
						}
					}
					workerKit.SetStateDir(workerStateDir)
					workerKit.SetProcessManager(threadProcessManager)
					workerKit.SetSkills(s.Skills)
					workerKit.SetAgentControl(control)
					workerKit.SetSessionID(id)
					workerKit.SetSessionDir(artifactDir)
					workerKit.SetToolSearchEnabled(workerToolSearchEnabled)
					workerKit.SetExperimentalDeferredToolBundles(s.ExperimentalDeferredBundles)
					workerKit.SetNativeDeferredToolDiscovery(workerNativeDeferredDiscovery)
					workerKit.SetAgentIdentity(meta.ID, meta.Path)
					participantSpeech := control != nil && control.ParticipantSpeechEnabled(meta.ID)
					applyWorkerToolFilter(workerKit, wt, participantSpeech)
					// Group management is resident-only; spawned runs (task
					// runs and ordinary subagents) never receive the backend.
					workerKit.SetGroupManager(nil)
					if participantSpeech {
						workerKit.SetParticipantSpeechEnabled(true)
						// Participant task runs share the resident file scope:
						// worker home + registered workspaces + system temp
						// (sidebar-groups-andy-workspaces.md §5.2).
						workerKit.SetFileScopeRoots(workspaces.BoundaryRoots(workerRoot, wuuHome))
					} else {
						workerKit.SetParticipantIdentity("")
						workerKit.SetParticipantSpeech(nil)
						workerKit.SetParticipantSpeechEnabled(false)
						workerKit.SetResidentParticipantEnabled(false)
					}
					return workerKit, nil
				},
				ParticipantStore: sessionParticipantStore{sessDir: statepath.SessionsDir(wuuHome)},
				MaxParallel:      5,
				InferenceJournal: s.InferenceJournalForOwner(id),
				ToolLedgerFactory: func(ownerID string) (*toolledger.Ledger, error) {
					return toolledger.New(s.SessionDir, ownerID)
				},
			})
			agentControl = control
		}

		var err error
		kit, err = s.Toolkit.CloneForRoot(threadRoot)
		if err != nil {
			return nil, err
		}
		kit.SetStateDir(stateDir)
		kit.SetProcessManager(threadProcessManager)
		kit.SetSkills(s.Skills)
		kit.SetAgentControl(agentControl)
		ConfigureToolkitPermissions(kit, s.Permissions)
		kit.SetSessionID(id)
		kit.SetSessionDir(artifactDir)
		kit.SetConversationSessionDir(s.SessionDir)
		kit.SetGoalRuntime(goalRuntime)
		kit.SetAgentIdentity(id, agentthread.RootPath)
		fileScopeExtras := []string{artifactDir}
		if s.MemdirEnabled {
			fileScopeExtras = append(fileScopeExtras, memdir.UserMemdir(wuuHome))
		}
		// Rebase the file-scope whitelist on the thread root (the clone
		// inherited the parent session's roots): thread root + registered
		// workspaces + temp + artifact/memory extras.
		kit.SetFileScopeRoots(workspaces.BoundaryRoots(kit.RootDir(), wuuHome, fileScopeExtras...))
		toolExecutor = hooks.NewHookedExecutor(kit, s.HookDispatcher, "", threadRoot)
		toolExecutor = newPluginToolExecutor(toolExecutor, s.PluginHost, id, threadRoot)
	}

	runner := cloneStreamRunnerForThread(s.StreamRunner, toolExecutor)
	toolLedger, err := toolledger.New(s.SessionDir, id)
	if err != nil {
		return nil, fmt.Errorf("open tool ledger: %w", err)
	}
	runner.ToolLedger = toolLedger
	runner.SystemPrompt, runner.SystemPromptSections = systemPromptForThreadRoot(runner.SystemPrompt, runner.SystemPromptSections, threadRoot, s.SessionDate)
	if s.MemdirEnabled {
		runner.SystemPrompt, runner.SystemPromptSections = systemPromptWithFreshMemdirIndex(runner.SystemPrompt, runner.SystemPromptSections, wuuHome)
	}
	runner.PromptCacheKey = strings.TrimSpace(id)
	runner.InferenceJournal = s.InferenceJournalForOwner(id)
	runner.BeforeRequestContext = RuntimeContextInjector(agentControl, agentthread.RootPath, toolkitContextBlockProvider(kit))
	runner.BeforeRequest = pluginRequestInterceptor(s.PluginHost, s.ProviderName, id, threadRoot)
	var afterTurnHooks []func(context.Context, *agent.StreamRunner, []providers.ChatMessage, agent.LoopResult)
	if kit != nil {
		if dreamScheduler := newSessionDreamScheduler(threadRoot, stateDir, func() string { return artifactDir }, s.DreamIntervalDays); dreamScheduler != nil {
			afterTurnHooks = append(afterTurnHooks, dreamScheduler.AfterTurn)
		}
	}
	runner.AfterTurn = chainAfterTurn(afterTurnHooks...)

	return &ThreadRuntime{
		StreamRunner:      runner,
		Toolkit:           kit,
		AgentControl:      agentControl,
		GoalRuntime:       goalRuntime,
		ActivityRegistry:  s.ActivityRegistry,
		ModelBudget:       s.ModelBudget,
		WorkerModelBudget: s.WorkerModelBudget,
	}, nil
}

func cloneStreamRunnerForThread(base *agent.StreamRunner, toolExecutor agent.ToolExecutor) *agent.StreamRunner {
	if base == nil {
		return nil
	}
	return &agent.StreamRunner{
		Client:                      base.Client,
		Tools:                       toolExecutor,
		ToolLedger:                  base.ToolLedger,
		Model:                       base.Model,
		APIModel:                    base.APIModel,
		SystemPrompt:                base.SystemPrompt,
		SystemPromptSections:        append([]agent.SystemPromptSectionInfo(nil), base.SystemPromptSections...),
		MaxSteps:                    base.MaxSteps,
		Temperature:                 base.Temperature,
		OnEvent:                     base.OnEvent,
		Bus:                         base.Bus,
		OnUsage:                     base.OnUsage,
		OnTokenUsage:                base.OnTokenUsage,
		ContextWindowOverride:       base.ContextWindowOverride,
		MaxInputTokens:              base.MaxInputTokens,
		OutputReserveTokens:         base.OutputReserveTokens,
		CompactThresholdTokens:      base.CompactThresholdTokens,
		CompactThresholdPct:         base.CompactThresholdPct,
		CompactKeepRecentTokens:     base.CompactKeepRecentTokens,
		DisableAutoCompact:          base.DisableAutoCompact,
		StreamingToolExecution:      base.StreamingToolExecution,
		BeforeStep:                  base.BeforeStep,
		BeforeRequestContext:        base.BeforeRequestContext,
		BeforeRequest:               base.BeforeRequest,
		AfterTurn:                   base.AfterTurn,
		Effort:                      base.Effort,
		Variant:                     base.Variant,
		ProviderOptions:             provideroptions.Clone(base.ProviderOptions),
		NativeDeferredToolDiscovery: base.NativeDeferredToolDiscovery,
		PromptCacheKey:              base.PromptCacheKey,
		InferenceOperationKind:      base.InferenceOperationKind,
		InferenceWorkloadProfile:    base.InferenceWorkloadProfile,
		InferenceJournal:            base.InferenceJournal,
	}
}

func sameRuntimeRoot(left, right string) bool {
	left = cleanRuntimeRoot(left)
	right = cleanRuntimeRoot(right)
	return left != "" && left == right
}

// sessionParticipantStore adapts the session store to
// agentcontrol.ParticipantStore so spawned workers get durable
// participant identities.
type sessionParticipantStore struct {
	sessDir string
}

func (s sessionParticipantStore) Upsert(p participant.Participant) error {
	return session.UpsertParticipant(s.sessDir, p)
}

func cleanRuntimeRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if ev, err := filepath.EvalSymlinks(root); err == nil {
		root = ev
	}
	return filepath.Clean(root)
}

func systemPromptForThreadRoot(promptText string, sections []agent.SystemPromptSectionInfo, rootDir, sessionDate string) (string, []agent.SystemPromptSectionInfo) {
	envSection := environmentSystemPromptSection(rootDir, sessionDate)
	if strings.TrimSpace(promptText) == "" || strings.TrimSpace(envSection) == "" {
		return promptText, append([]agent.SystemPromptSectionInfo(nil), sections...)
	}
	const marker = "# Environment"
	start := strings.Index(promptText, marker)
	if start < 0 {
		return promptText, append([]agent.SystemPromptSectionInfo(nil), sections...)
	}
	end := len(promptText)
	if next := strings.Index(promptText[start+len(marker):], "\n\n# "); next >= 0 {
		end = start + len(marker) + next
	}
	updated := promptText[:start] + envSection + promptText[end:]
	return updated, updateEnvironmentSectionInfo(sections, envSection)
}

func updateEnvironmentSectionInfo(sections []agent.SystemPromptSectionInfo, envSection string) []agent.SystemPromptSectionInfo {
	return updateSectionInfo(sections, "environment", envSection)
}

func updateSectionInfo(sections []agent.SystemPromptSectionInfo, key, content string) []agent.SystemPromptSectionInfo {
	out := append([]agent.SystemPromptSectionInfo(nil), sections...)
	sum := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(sum[:16])
	for i := range out {
		if out[i].Key == key {
			out[i].Bytes = len([]byte(content))
			out[i].Hash = hash
			return out
		}
	}
	return out
}

// systemPromptWithFreshMemdirIndex re-reads the user notebook index and
// splices an up-to-date memdir section into a thread's system prompt.
// Thread creation is one of the two allowed prompt-prefix change points
// (memory-redesign §4), so a conversation started today sees memories saved
// in earlier conversations without restarting the app. Within the thread
// the section stays frozen.
func systemPromptWithFreshMemdirIndex(promptText string, sections []agent.SystemPromptSectionInfo, wuuHome string) (string, []agent.SystemPromptSectionInfo) {
	userNotebook := memdir.UserMemdir(wuuHome)
	snap, err := memdir.ReadIndex(userNotebook)
	if err != nil {
		return promptText, sections
	}
	section := prompt.MemdirSection(memdir.SessionTeaching(userNotebook), snap.Content)
	const marker = "# Memory directory"
	start := strings.Index(promptText, marker)
	if start < 0 || section == "" {
		return promptText, append([]agent.SystemPromptSectionInfo(nil), sections...)
	}
	end := len(promptText)
	if next := strings.Index(promptText[start+len(marker):], "\n\n# "); next >= 0 {
		end = start + len(marker) + next
	}
	updated := promptText[:start] + section + promptText[end:]
	return updated, updateSectionInfo(sections, "memdir", section)
}

func chainAfterTurn(hooks ...func(context.Context, *agent.StreamRunner, []providers.ChatMessage, agent.LoopResult)) func(context.Context, *agent.StreamRunner, []providers.ChatMessage, agent.LoopResult) {
	filtered := make([]func(context.Context, *agent.StreamRunner, []providers.ChatMessage, agent.LoopResult), 0, len(hooks))
	for _, hook := range hooks {
		if hook != nil {
			filtered = append(filtered, hook)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return func(ctx context.Context, runner *agent.StreamRunner, history []providers.ChatMessage, result agent.LoopResult) {
		for _, hook := range filtered {
			hook(ctx, runner, history, result)
		}
	}
}

func applyWorkerToolFilter(kit *tools.Toolkit, wt agentcontrol.WorkerType, participantSpeech bool) {
	if kit == nil {
		return
	}
	fullNames := kit.SurfaceToolNames()

	allowed := agentcontrol.FilterToolsForWorker(wt, fullNames, participantSpeech)
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
	if s.StreamRunner != nil {
		s.StreamRunner.PromptCacheKey = strings.TrimSpace(id)
	}
	if s.Toolkit != nil {
		s.Toolkit.SetSessionID(id)
		s.Toolkit.SetAgentIdentity(id, agentthread.RootPath)
		stateDir := strings.TrimSpace(s.StateDir)
		if stateDir == "" {
			if home, err := statepath.Home(""); err == nil {
				if dir, err := resolveWorkspaceStateDir(home, s.WorkspaceID, s.RootDir); err == nil {
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
	var cleanupErr error
	if s.Toolkit != nil {
		if manager := s.Toolkit.MCPManager(); manager != nil {
			cleanupErr = errors.Join(cleanupErr, manager.Close())
		}
	}
	if s.InferenceJournalRuntime != nil {
		cleanupErr = errors.Join(cleanupErr, s.InferenceJournalRuntime.Close())
		s.InferenceJournalRuntime = nil
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
	if s.PluginHost != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = s.PluginHost.Close(ctx)
		cancel()
		s.PluginHost = nil
	}
	if s.ProcessManager == nil {
		return process.CleanupResult{}, cleanupErr
	}
	result, err := s.ProcessManager.CleanupSessionWithResult()
	return result, errors.Join(cleanupErr, err)
}

func ResolveModelBudget(model string, provider config.ProviderConfig, agentOverride int) modelbudget.Budget {
	return modelbudget.Resolve(model, provider, agentOverride)
}

// ResolveContextWindow resolves the trusted model context size used for
// proactive auto-compact. A zero return means the model limit is unknown; the
// runtime should skip proactive compaction and rely on provider overflow errors.
func ResolveContextWindow(model string, provider config.ProviderConfig, agentOverride int) int {
	return ResolveModelBudget(model, provider, agentOverride).ContextWindowTokens
}

// ResolveInputWindow resolves the effective prompt/input budget when the
// provider publishes a separate input cap. It intentionally does not synthesize
// an input cap from context-output; proactive compaction handles output reserve
// separately.
func ResolveInputWindow(model string, provider config.ProviderConfig) int {
	return ResolveModelBudget(model, provider, 0).InputLimitTokens
}

const codexSubscriptionGPT5InputCap = modelbudget.CodexSubscriptionGPT5InputCap

func codexSubscriptionInputCap(model, providerType string) int {
	return modelbudget.CodexSubscriptionInputCap(model, providerType)
}

// RuntimeContextInjector returns volatile request-only runtime context injected
// into model requests without appending it to live or durable history. Stable
// session environment belongs in the system prompt, not here.
func RuntimeContextInjector(control *agentcontrol.AgentControl, currentPath string, blockProviders ...func() []wuucontext.Block) func() []agent.ContextSegment {
	return func() []agent.ContextSegment {
		var blocks []wuucontext.Block
		for _, provider := range blockProviders {
			if provider == nil {
				continue
			}
			blocks = append(blocks, provider()...)
		}
		if control != nil {
			if agentReminder := control.ActiveTaskReminder(currentPath); agentReminder != "" {
				blocks = append(blocks, wuucontext.Block{
					Kind:    wuucontext.BlockTaskState,
					Title:   "Active child-agent status",
					Source:  "runtime.subagent_status",
					Content: agentReminder,
				})
			}
		}
		return agent.RequestOnlyContextBlocks(blocks)
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

func buildHookDispatcher(cfg config.Config, plugins []pluginpkg.Plugin, client providers.Client, defaultModel string, defaultJournal providers.InferenceJournal) *hooks.Dispatcher {
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
	for _, item := range plugins {
		for evName, entries := range item.Hooks {
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
	}
	hookRegistry := hooks.NewRegistry(hookEntries)
	if client != nil {
		// Wire the prompt-hook model client so type=prompt hooks actually
		// run. Without this, PromptHook.Execute short-circuits with a
		// nil client and the hook silently passes through. Pass the
		// configured tool-mode model as the default; individual hook
		// entries can still override via their own `model` field.
		hookRegistry.SetModelClient(hooks.NewProviderModelClient(client, defaultModel, defaultJournal))
	}
	return hooks.NewDispatcher(hookRegistry)
}

func discoverPlugins(rootDir, wuuHome string) []pluginpkg.Plugin {
	return pluginpkg.Discover(rootDir, wuuHome)
}

func discoverSkills(rootDir, homeDir, wuuHome string, plugins []pluginpkg.Plugin) []skills.Skill {
	var projectDirs []skills.SourceDir
	var userDirs []skills.SourceDir
	for _, item := range plugins {
		source := item.SourceLabel()
		for _, dir := range item.SkillDirs() {
			switch item.Source {
			case "project":
				projectDirs = append(projectDirs, skills.SourceDir{Path: dir, Source: source})
			default:
				userDirs = append(userDirs, skills.SourceDir{Path: dir, Source: source})
			}
		}
	}
	if home := skillUserHome(homeDir); home != "" {
		userDirs = append(userDirs,
			skills.SourceDir{Path: filepath.Join(home, ".codex", "skills"), Source: "user"},
			skills.SourceDir{Path: filepath.Join(home, ".claude", "skills"), Source: "user"},
			skills.SourceDir{Path: filepath.Join(home, ".agents", "skills"), Source: "user"},
			skills.SourceDir{Path: filepath.Join(home, ".config", "opencode", "skills"), Source: "user"},
		)
	}
	if strings.TrimSpace(wuuHome) != "" {
		userDirs = append(userDirs, skills.SourceDir{Path: filepath.Join(wuuHome, "skills"), Source: "user"})
	}
	projectDirs = append(projectDirs, skillProjectDirs(rootDir)...)
	discovered := skills.DiscoverSourceDirs(projectDirs, userDirs)
	// Claude Code-style command templates (.claude/commands/*.md) are read as
	// pure content and adapted into lightweight skill entries. Native skills
	// take precedence over commands with the same name.
	commands := skills.DiscoverCommandsSourceDirs(commandProjectDirs(rootDir), commandUserDirs(wuuHome))
	return skills.MergeWithBundled(skills.MergeCommands(discovered, commands), wuuHome)
}

func discoverAgentTemplates(rootDir, homeDir string) agenttemplate.Discovery {
	var projectDirs []agenttemplate.SourceDir
	if strings.TrimSpace(rootDir) != "" {
		if absRoot, err := filepath.Abs(rootDir); err == nil {
			projectRoot := findSkillProjectRoot(absRoot)
			for _, dir := range skillDirChain(projectRoot, absRoot) {
				projectDirs = append(projectDirs, agenttemplate.SourceDir{
					Path:   filepath.Join(dir, ".claude", "agents"),
					Source: "project",
				})
			}
		}
	}
	var userDirs []agenttemplate.SourceDir
	if home := skillUserHome(homeDir); home != "" {
		userDirs = append(userDirs, agenttemplate.SourceDir{
			Path:   filepath.Join(home, ".claude", "agents"),
			Source: "user",
		})
	}
	return agenttemplate.DiscoverSourceDirs(projectDirs, userDirs)
}

// commandProjectDirs returns the project-chain command directories, mirroring
// skillProjectDirs but scoped to Claude Code / wuu command template folders.
func commandProjectDirs(rootDir string) []skills.SourceDir {
	if strings.TrimSpace(rootDir) == "" {
		return nil
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil
	}
	projectRoot := findSkillProjectRoot(absRoot)
	chain := skillDirChain(projectRoot, absRoot)
	out := make([]skills.SourceDir, 0, len(chain)*2)
	for _, dir := range chain {
		out = append(out,
			skills.SourceDir{Path: filepath.Join(dir, ".claude", "commands"), Source: "project"},
			skills.SourceDir{Path: filepath.Join(dir, ".wuu", "commands"), Source: "project"},
		)
	}
	return out
}

// commandUserDirs returns the user-level command directory (~/.wuu/commands),
// resolved from wuuHome exactly like the ~/.wuu/skills user directory.
func commandUserDirs(wuuHome string) []skills.SourceDir {
	if strings.TrimSpace(wuuHome) == "" {
		return nil
	}
	return []skills.SourceDir{{Path: filepath.Join(wuuHome, "commands"), Source: "user"}}
}

func skillUserHome(homeDir string) string {
	home := strings.TrimSpace(homeDir)
	if home != "" {
		return home
	}
	if resolved, err := os.UserHomeDir(); err == nil {
		return resolved
	}
	return ""
}

func skillProjectDirs(rootDir string) []skills.SourceDir {
	if strings.TrimSpace(rootDir) == "" {
		return nil
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil
	}
	projectRoot := findSkillProjectRoot(absRoot)
	chain := skillDirChain(projectRoot, absRoot)
	out := make([]skills.SourceDir, 0, len(chain)*5)
	for _, dir := range chain {
		// External ecosystem directories are lower precedence than native wuu
		// skills at the same level. More specific child directories still override
		// ancestors because the chain is ordered root -> current directory.
		out = append(out,
			skills.SourceDir{Path: filepath.Join(dir, ".claude", "skills"), Source: "project"},
			skills.SourceDir{Path: filepath.Join(dir, ".agents", "skills"), Source: "project"},
			skills.SourceDir{Path: filepath.Join(dir, ".opencode", "skill"), Source: "project"},
			skills.SourceDir{Path: filepath.Join(dir, ".opencode", "skills"), Source: "project"},
			skills.SourceDir{Path: filepath.Join(dir, ".wuu", "skills"), Source: "project"},
		)
	}
	return out
}

func findSkillProjectRoot(start string) string {
	cur := start
	for {
		for _, marker := range []string{".git", ".hg", ".jj", ".svn"} {
			if _, err := os.Lstat(filepath.Join(cur, marker)); err == nil {
				return cur
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

func skillDirChain(root, leaf string) []string {
	if root == "" {
		return []string{leaf}
	}
	rel, err := filepath.Rel(root, leaf)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return []string{leaf}
	}
	chain := []string{root}
	if rel == "." {
		return chain
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		chain = append(chain, cur)
	}
	return chain
}

func connectMCPServers(cfg config.Config, plugins []pluginpkg.Plugin, toolkit *tools.Toolkit) {
	servers := mcpServersFromConfigAndPlugins(cfg, plugins)
	if toolkit == nil || len(servers) == 0 {
		return
	}
	toolkit.SetMCPActivityBindings(mcpActivityBindingsFromPlugins(plugins))
	mcpMgr := mcp.NewManager()
	toolkit.SetMCPManager(mcpMgr)
	serverConfigs := make(map[string]mcp.ServerConfig, len(servers))
	for name, mcpCfg := range servers {
		serverConfigs[name] = mcp.ServerConfig{
			Name:          name,
			Command:       mcpCfg.Command,
			Args:          mcpCfg.Args,
			URL:           mcpCfg.URL,
			Transport:     mcpCfg.Transport,
			Env:           mcpCfg.Env,
			Headers:       mcpCfg.Headers,
			OAuth:         mcpOAuthConfig(mcpCfg.OAuth),
			Enabled:       mcpCfg.Enabled,
			ToolOverrides: mcpToolOverrides(mcpCfg.ToolOverrides),
		}
	}
	mcpMgr.Configure(serverConfigs)
	go func() {
		ctx := context.Background()
		for name, serverCfg := range serverConfigs {
			if !serverCfg.IsEnabled() {
				providers.DebugLogf("mcp server %q disabled", name)
				continue
			}
			if err := mcpMgr.Add(ctx, serverCfg); err != nil {
				providers.DebugLogf("mcp server %q failed to connect: %v", name, err)
			} else {
				providers.DebugLogf("mcp server %q connected (%d tools)", name, mcpMgr.Status()[name].ToolCount)
			}
		}
	}()
}

func mcpActivityBindingsFromPlugins(plugins []pluginpkg.Plugin) map[string]tools.MCPActivityBinding {
	out := make(map[string]tools.MCPActivityBinding)
	for _, item := range plugins {
		if len(item.ActivityKinds) != 1 {
			continue
		}
		kind := activity.Kind(strings.TrimSpace(item.ActivityKinds[0]))
		if kind != activity.KindBrowser && kind != activity.KindCUA {
			continue
		}
		pluginID := strings.TrimSpace(item.ID)
		if pluginID == "" {
			continue
		}
		for name := range item.MCPServers {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			out["plugin."+pluginID+"."+name] = tools.MCPActivityBinding{Kind: kind, PluginID: pluginID}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mcpServersFromConfigAndPlugins(cfg config.Config, plugins []pluginpkg.Plugin) map[string]config.MCPServerConfig {
	out := make(map[string]config.MCPServerConfig)
	for _, item := range plugins {
		for name, server := range item.MCPServers {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			out["plugin."+item.ID+"."+name] = server
		}
	}
	for name, server := range cfg.MCPServers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = server
	}
	return out
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
			Capability:      override.Capability,
		}
	}
	return out
}

func mcpOAuthConfig(in *config.MCPOAuthConfig) *mcp.OAuthConfig {
	if in == nil {
		return nil
	}
	return &mcp.OAuthConfig{
		ClientID:     in.ClientID,
		ClientSecret: in.ClientSecret,
		Scopes:       append([]string(nil), in.Scopes...),
		RedirectURI:  in.RedirectURI,
	}
}

func ConfigureToolkitPermissions(kit *tools.Toolkit, permissions config.ResolvedPermissions) {
	if kit == nil {
		return
	}
	kit.SetBoundary(BoundaryForMode(permissions.Mode))
}

func BoundaryForMode(mode string) tools.WorkspaceBoundary {
	switch config.NormalizePermissionMode(mode) {
	case config.PermissionModeReadOnly:
		return tools.ReadOnlyBoundary()
	case config.PermissionModeUnconfined:
		return tools.UnconfinedBoundary()
	default:
		return tools.StandardBoundary()
	}
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
	} else if dirs := statepath.UserInstructionDirs(homeDir); len(dirs) > 0 {
		// Resolve the canonical user instruction dirs (unified wuu home +
		// legacy ~/.config/wuu) through statepath so WUU_HOME relocates the
		// user AGENTS.md scan along with the rest of the directory.
		memOpts.UserDirs = dirs
	}
	if cfg.IncludeLegacyMemory != nil {
		memOpts.IncludeLegacyMemory = cfg.IncludeLegacyMemory
	}
	return memory.Discover(rootDir, homeDir, memOpts)
}

// buildWorkerBasePrompt assembles a worker subagent's base system prompt.
// Workers get the READ-ONLY user-notebook variant (memory-redesign §3:
// 临时 worker 只读注入): the user index is injected as orientation — read
// fresh here because worker/thread creation is a prompt-prefix creation
// moment under the cache red lines — while the notebook directory stays
// outside the worker's writable file scope.
func buildWorkerBasePrompt(rootDir, sessionDate, wuuHome, userPrompt, providerName, model string, toolSurface capability.Surface, memoryFiles []memory.File, memdirEnabled bool, discoveredSkills []skills.Skill) string {
	var teaching, index string
	if memdirEnabled && strings.TrimSpace(wuuHome) != "" {
		userNotebook := memdir.UserMemdir(wuuHome)
		teaching = memdir.WorkerTeaching(userNotebook)
		if snap, err := memdir.ReadIndex(userNotebook); err == nil {
			index = snap.Content
		} else {
			providers.DebugLogf("read user memory index for worker prompt: %v", err)
		}
	}
	return buildBaseSystemPromptContent(rootDir, sessionDate, config.WorkerSystemPrompt(), userPrompt, providerName, model, toolSurface, memoryFiles, teaching, index, discoveredSkills)
}

func (s *Session) RefreshSystemPrompt(providerName, model string) string {
	if s == nil {
		return ""
	}
	// Settings changes rebuild the whole prompt anyway, so the user notebook
	// index is re-read here (an accepted one-time prompt-cache invalidation).
	var memdirTeaching, memdirIndex string
	if s.MemdirEnabled {
		userNotebook := memdir.UserMemdir(s.WuuHome)
		memdirTeaching = memdir.SessionTeaching(userNotebook)
		if snap, err := memdir.ReadIndex(userNotebook); err == nil {
			memdirIndex = snap.Content
		} else {
			providers.DebugLogf("read user memory index: %v", err)
		}
	}
	baseSystemPromptResult := buildBaseSystemPromptResult(
		s.RootDir,
		s.SessionDate,
		config.DefaultSystemPrompt(),
		s.UserSystemPrompt,
		providerName,
		model,
		activeSurfaceWithDeferredToolCatalog(s.Toolkit, s.DeferredToolCatalogPrompt),
		s.Memory,
		memdirTeaching,
		memdirIndex,
		s.Skills,
	)
	baseSystemPrompt := baseSystemPromptResult.Content
	s.BaseSystemPrompt = baseSystemPrompt
	s.BaseSystemPromptSections = baseSystemPromptResult.Sections
	if s.StreamRunner != nil {
		s.StreamRunner.UpdateSystemPromptWithSections(baseSystemPrompt, agentPromptSections(baseSystemPromptResult.Sections))
	}
	return baseSystemPrompt
}

// ApplyGeneralConfig refreshes user-owned prompt and memory settings on the
// shared session runtime without changing provider or model selection.
func (s *Session) ApplyGeneralConfig(cfg config.Config, homeDir string) string {
	if s == nil {
		return ""
	}
	if strings.TrimSpace(homeDir) == "" {
		homeDir = os.Getenv("HOME")
	}
	s.UserSystemPrompt = cfg.Agent.UserSystemPrompt()
	s.Memory = discoverMemory(s.RootDir, homeDir, cfg.Memory)
	s.MemdirEnabled = !cfg.Memory.Disable
	s.DreamIntervalDays = cfg.Memory.DreamIntervalDaysValue()
	if cfg.Memory.Disable {
		s.DreamIntervalDays = 0
	}
	if s.Toolkit != nil {
		fileScopeExtras := []string{}
		if s.MemdirEnabled {
			userNotebook := memdir.UserMemdir(s.WuuHome)
			if err := memdir.EnsureDir(userNotebook); err != nil {
				providers.DebugLogf("ensure user memory notebook: %v", err)
			}
			fileScopeExtras = append(fileScopeExtras, userNotebook)
		}
		s.Toolkit.SetFileScopeRoots(workspaces.BoundaryRoots(s.Toolkit.RootDir(), s.WuuHome, fileScopeExtras...))
	}
	apiModel := s.Model
	if s.StreamRunner != nil && strings.TrimSpace(s.StreamRunner.APIModel) != "" {
		apiModel = s.StreamRunner.APIModel
	}
	return s.RefreshSystemPrompt(s.ProviderName, apiModel)
}

// buildBaseSystemPrompt keeps the frozen-date-agnostic signature used by tests
// and standalone callers; it passes an empty sessionDate so the environment
// section falls back to the current date.
func buildBaseSystemPrompt(rootDir, basePrompt, userPrompt, providerName, model string, toolSurface capability.Surface, memoryFiles []memory.File, memdirTeaching, memdirIndex string, discoveredSkills []skills.Skill) string {
	return buildBaseSystemPromptContent(rootDir, "", basePrompt, userPrompt, providerName, model, toolSurface, memoryFiles, memdirTeaching, memdirIndex, discoveredSkills)
}

func buildBaseSystemPromptContent(rootDir, sessionDate, basePrompt, userPrompt, providerName, model string, toolSurface capability.Surface, memoryFiles []memory.File, memdirTeaching, memdirIndex string, discoveredSkills []skills.Skill) string {
	return buildBaseSystemPromptResult(rootDir, sessionDate, basePrompt, userPrompt, providerName, model, toolSurface, memoryFiles, memdirTeaching, memdirIndex, discoveredSkills).Content
}

func buildBaseSystemPromptResult(rootDir, sessionDate, basePrompt, userPrompt, providerName, model string, toolSurface capability.Surface, memoryFiles []memory.File, memdirTeaching, memdirIndex string, discoveredSkills []skills.Skill) prompt.BuildResult {
	var pb prompt.Builder
	pb.AddSection("base", basePrompt, true)
	pb.AddSection("tool_surface", toolSurface.SystemFragment, true)
	if _, ok := toolSurface.Tools["tool_search"]; ok {
		pb.AddSection("deferred_tool_catalog", toolSurface.DeferredToolCatalog, true)
	}
	// The spawn_agent worker-type roster is session-static (compile-time
	// constant) but must not live in the spawn_agent tool schema, where a future
	// dynamic change would churn the cached tool-schema prefix. Mirror the
	// deferred_tool_catalog pattern: emit it as a static system-prompt section
	// only for surfaces that actually expose spawn_agent, so the tool schema
	// stays generic and stable.
	if _, ok := toolSurface.Tools["spawn_agent"]; ok {
		pb.AddSection("subagent_types", subagentTypesSystemSection(), true)
	}
	pb.AddSection("environment", environmentSystemPromptSection(rootDir, sessionDate), true)
	if strings.TrimSpace(userPrompt) != "" {
		pb.AddSection("user_custom_prompt", "# User Custom Instructions\n\nFollow these user-defined instructions unless they conflict with wuu's built-in behavior, safety, or tool-use discipline above.\n\n"+userPrompt, true)
	}
	pb.AddMemory(memoryFiles)
	if strings.TrimSpace(memdirTeaching) != "" {
		pb.AddMemdir(memdirTeaching, memdirIndex)
	}
	if toolSurface.ProfileName != "" {
		pb.AddSkills(tools.FilterSkillsForSurface(discoveredSkills, toolSurface))
	}
	return pb.BuildWithInfo()
}

// subagentTypesSystemSection renders the static Subagent Types roster injected
// into the base system prompt for spawn_agent-capable surfaces. It replaces the
// worker-type list that used to be baked into the spawn_agent tool description,
// keeping that dynamic list out of the cached tool-schema prefix while staying
// cache-stable itself (built from the compile-time worker registry).
func subagentTypesSystemSection() string {
	types := agentcontrol.AvailableWorkerTypes()
	if len(types) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Subagent Types\n\n")
	b.WriteString("These are the spawn_agent subagent_type values available this session. ")
	b.WriteString("Set subagent_type to launch a fresh specialized agent; omit it to fork yourself with full conversation context.\n\n")
	for _, wt := range types {
		fmt.Fprintf(&b, "- %s: %s\n", wt.Name, wt.Description)
	}
	return strings.TrimRight(b.String(), "\n")
}

// environmentSystemPromptSection renders the static "# Environment" system
// section. The date is the session-start frozen value (sessionDate) so the
// cached system prefix stays byte-stable across turns and thread rebuilds; a
// long-lived session that crosses a day boundary keeps its start date instead
// of busting the prompt cache. Real-time clock reads belong in the per-turn
// message stream, not this cached prefix. When sessionDate is empty (callers
// without a frozen session, e.g. standalone worker-prompt helpers) it falls
// back to the current date.
func environmentSystemPromptSection(rootDir, sessionDate string) string {
	env := wuucontext.Snapshot(rootDir)
	if frozen := strings.TrimSpace(sessionDate); frozen != "" {
		env.Date = frozen
	}
	var b strings.Builder
	b.WriteString("# Environment\n\n")
	if cwd := strings.TrimSpace(env.CWD); cwd != "" {
		fmt.Fprintf(&b, "- Current working directory: %s\n", cwd)
	}
	if date := strings.TrimSpace(env.Date); date != "" {
		fmt.Fprintf(&b, "- Current date: %s\n", date)
	}
	return strings.TrimRight(b.String(), "\n")
}

func agentPromptSections(sections []prompt.SectionInfo) []agent.SystemPromptSectionInfo {
	if len(sections) == 0 {
		return nil
	}
	out := make([]agent.SystemPromptSectionInfo, 0, len(sections))
	for _, section := range sections {
		out = append(out, agent.SystemPromptSectionInfo{
			Key:    section.Key,
			Static: section.Static,
			Bytes:  section.Bytes,
			Hash:   section.Hash,
		})
	}
	return out
}

func activeSurface(kit *tools.Toolkit) capability.Surface {
	if kit == nil {
		return capability.Surface{}
	}
	return kit.ActiveSurface()
}

func activeSurfaceWithDeferredToolCatalog(kit *tools.Toolkit, catalogPrompt string) capability.Surface {
	surface := activeSurface(kit)
	surface.DeferredToolCatalog = catalogPrompt
	return surface
}

func deferredToolCatalogPromptForToolkit(kit *tools.Toolkit) (string, error) {
	if kit == nil {
		return "", nil
	}
	return kit.DeferredToolCatalogSystemSection()
}

// workerDeferredToolCatalogPromptForToolkit computes the deferred-tool
// catalog section for the worker surface (consistency-repair #13: worker
// prompts taught tool_search catalog lookups while their catalog stayed
// empty). It reuses the exact generator that fills
// mainSurface.DeferredToolCatalog, but on a throwaway in-memory clone of the
// session toolkit configured with the worker-compiled surface, so entries are
// filtered by the worker's own exposure buckets (no orchestration suite,
// worker tool-search setting).
func workerDeferredToolCatalogPromptForToolkit(kit *tools.Toolkit, providerName, model string, toolSearchEnabled, experimentalDeferredBundles bool) (string, error) {
	if kit == nil {
		return "", nil
	}
	wkit, err := kit.CloneForRoot("")
	if err != nil {
		return "", err
	}
	wkit.ConfigureSurfaceForProviderModel(providerName, model, false)
	wkit.SetToolSearchEnabled(toolSearchEnabled)
	wkit.SetExperimentalDeferredToolBundles(experimentalDeferredBundles)
	return wkit.DeferredToolCatalogSystemSection()
}

// compiledSurfaceForProviderModel is the worker-only entry point in
// production: every caller in internal/runtime/session.go that uses
// it is configuring a worker's tool surface, not the main agent's.
// The main agent's surface is installed through
// internal/tools/edit_mode.go::ConfigureSurfaceForProviderModel on
// the toolkit itself. Worker surfaces intentionally omit the
// main-agent-only helpme recovery tool; the runtime still enforces
// the same boundary via DisallowedTools and the helpme tool's
// Execute path check.
func compiledSurfaceForProviderModel(providerName, model string) capability.Surface {
	return modelprofile.DefaultCompiler{}.Compile(modelprofile.Resolve(providerName, model), modelprofile.SurfaceWorker)
}
