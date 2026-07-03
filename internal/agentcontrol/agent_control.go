// Package agentcontrol wires the orchestration tools (spawn_agent,
// send_message, close_agent, list_agents) to the underlying subagent
// and worktree subsystems.
//
// AgentControl is the shared control plane for one root agent tree. It
// owns the SubAgent Manager, Worktree Manager, thread registry, and
// event store, and exposes the API the toolkit uses to implement the
// orchestration tools.
package agentcontrol

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"crypto/rand"
	"encoding/hex"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/harness"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/subagent"
	"github.com/blueberrycongee/wuu/internal/worktree"
)

// WorkerToolkitFactory builds a fresh ToolExecutor for a worker thread.
// The metadata argument contains the worker's canonical agent path, so
// orchestration tools inside that worker can resolve relative child paths.
type WorkerToolkitFactory func(rootDir string, wt WorkerType, meta agentthread.Metadata) (agent.ToolExecutor, error)

// WorkerSystemPromptFactory builds the base prompt for a registered worker.
// AgentControl still wraps it with the worker role and working-directory
// instructions.
type WorkerSystemPromptFactory func(rootDir string, wt WorkerType, meta agentthread.Metadata, isolation IsolationMode) (string, error)

// ParticipantStore persists conversation participant identities. It is
// defined here (instead of importing internal/session) so agentcontrol
// stays decoupled from the session storage layer.
type ParticipantStore interface {
	Upsert(participant.Participant) error
}

// AgentControl owns the orchestration runtime for one wuu session.
type AgentControl struct {
	manager       *subagent.Manager
	worktrees     *worktree.Manager // nil when workspace is not a git repo
	parentRepo    string            // absolute path to workspace root
	worktreeRoot  string            // workspace-state worktrees directory
	sessionID     string
	historyDir    string
	threadDir     string
	threads       *agentthread.Registry
	threadStore   *agentthread.Store
	harnessDir    string
	harnessStore  *harness.Store
	failureSink   FailureSink
	reportSink    ReportSink
	rootThreadID  string
	rootThreadDir string
	workerFact    WorkerToolkitFactory
	workerPrompt  WorkerSystemPromptFactory
	defaultSys    string           // base system prompt prefix added to every worker
	participants  ParticipantStore // optional; nil disables participant persistence
	maxParallel   int
	queueMu       sync.Mutex
	queued        []preparedSpawn
	statusCh      chan subagent.Notification
	statusStop    chan struct{}
	statusDone    chan struct{}
	closeOnce     sync.Once

	resultDeliveriesMu sync.Mutex
	resultDeliveries   map[string]agentResultDelivery

	participantMessagesMu  sync.Mutex
	participantMessages    []chan<- ParticipantMessage
	participantSpeech      map[string]struct{}
	participantResultPosts map[string]struct{}
	participantResponses   map[string]struct{}

	// participantRosterMu guards participantRoster and
	// participantRosterBindings, the dispatch table for the
	// manage_participant tool.
	participantRosterMu       sync.Mutex
	participantRoster         ParticipantRoster
	participantRosterBindings map[string]string

	// workerProviderName is the provider name the AgentControl's worker
	// runtime is currently configured for. The model-pin resolver
	// (installed via SetModelPinClientResolver) uses it to decide
	// whether a queued spawn's pin targets the same provider (no fresh
	// client needed) or a different one (resolver MUST yield a fresh
	// client or fail).
	workerProviderNameMu sync.Mutex
	workerProviderName   string

	// modelPinResolver rebuilds the stream client for a queued spawn
	// whose raw pin targets a provider different from the worker
	// default. nil means no resolver is installed; in that case a
	// cross-provider pin restored from disk fails the spawn
	// explicitly instead of silently falling back to the default
	// client (which would route the request to the wrong provider).
	modelPinResolverMu sync.Mutex
	modelPinResolver   ModelPinClientResolver
}

// ModelPinClientResolver rebuilds the (model, client) pair for a queued
// spawn whose raw participant pin survived a process restart. The
// resolver owns the policy: bare-model / same-provider pins return
// (model, nil, nil), cross-provider pins return (model, freshClient, nil),
// and any error fails the queued spawn explicitly. Appserver
// (internal/appserver) is the typical owner of this callback because
// it already holds the runtime config + provider factory used to build
// the worker client.
type ModelPinClientResolver func(rawPin string) (modelOverride string, clientOverride providers.StreamClient, err error)

// Config holds the dependencies needed to build an AgentControl.
type Config struct {
	// Client is the streaming LLM client every worker spawned by this
	// agent control runtime will share. It must be a StreamClient (not just a
	// Client) so workers run through the same streaming transport as
	// the interactive main agent.
	Client                         providers.StreamClient
	DefaultModel                   string
	DefaultEffort                  string
	DefaultOptions                 map[string]any
	DefaultContextWindow           int
	DefaultMaxInputTokens          int
	DefaultOutputReserveTokens     int
	DefaultCompactThresholdPct     float64
	DefaultCompactKeepRecentTokens int
	DefaultDisableAutoCompact      bool
	ParentRepo                     string // absolute path to the user's workspace
	WorktreeRoot                   string // workspace-state worktrees directory (only used when workspace is a git repo)
	HistoryDir                     string // session artifact workers directory
	ThreadDir                      string // session artifact threads directory
	HarnessDir                     string // session artifact harness directory
	FailureSink                    FailureSink
	ReportSink                     ReportSink
	SessionID                      string
	WorkerSysPrompt                string
	WorkerFactory                  WorkerToolkitFactory
	WorkerPrompt                   WorkerSystemPromptFactory
	// ParticipantStore, when set, persists the ephemeral participant
	// identity created for each spawned worker. Optional: when nil,
	// participant IDs are still generated in-memory but not persisted.
	ParticipantStore ParticipantStore
	MaxParallel      int
}

// New constructs an AgentControl. Worktree isolation is only available
// when the workspace is a git repository; inplace spawns and forks
// work regardless.
func New(cfg Config) (*AgentControl, error) {
	if cfg.Client == nil {
		return nil, errors.New("Client required")
	}
	if cfg.WorkerFactory == nil {
		return nil, errors.New("WorkerFactory required")
	}

	// Worktree manager is optional — only created when the workspace
	// is a git repo. Non-git workspaces can still spawn inplace
	// workers and fork agents; only isolation=worktree is unavailable.
	var wt *worktree.Manager
	if worktree.IsGitRepo(cfg.ParentRepo) {
		var err error
		wt, err = worktree.NewManager(cfg.ParentRepo, cfg.WorktreeRoot)
		if err != nil {
			return nil, fmt.Errorf("worktree manager: %w", err)
		}
	}

	mgr := subagent.NewManagerWithOptions(cfg.Client, cfg.DefaultModel, subagent.ManagerOptions{
		DefaultEffort:           cfg.DefaultEffort,
		DefaultProviderOptions:  cfg.DefaultOptions,
		ContextWindowOverride:   cfg.DefaultContextWindow,
		MaxInputTokens:          cfg.DefaultMaxInputTokens,
		OutputReserveTokens:     cfg.DefaultOutputReserveTokens,
		CompactThresholdPct:     cfg.DefaultCompactThresholdPct,
		CompactKeepRecentTokens: cfg.DefaultCompactKeepRecentTokens,
		DisableAutoCompact:      cfg.DefaultDisableAutoCompact,
	})
	threadRegistry := agentthread.NewRegistry()

	maxP := cfg.MaxParallel
	if maxP <= 0 {
		maxP = 5
	}
	harnessDir := strings.TrimSpace(cfg.HarnessDir)
	if harnessDir == "" && strings.TrimSpace(cfg.ThreadDir) != "" {
		harnessDir = filepath.Join(filepath.Dir(cfg.ThreadDir), "harness")
	}
	c := &AgentControl{
		manager:      mgr,
		worktrees:    wt,
		parentRepo:   cfg.ParentRepo,
		worktreeRoot: cfg.WorktreeRoot,
		sessionID:    cfg.SessionID,
		historyDir:   cfg.HistoryDir,
		threadDir:    cfg.ThreadDir,
		threads:      threadRegistry,
		threadStore:  agentthread.NewStore(cfg.ThreadDir),
		harnessDir:   harnessDir,
		harnessStore: harness.NewStore(harnessDir),
		failureSink:  cfg.FailureSink,
		reportSink:   cfg.ReportSink,
		workerFact:   cfg.WorkerFactory,
		workerPrompt: cfg.WorkerPrompt,
		defaultSys:   cfg.WorkerSysPrompt,
		participants: cfg.ParticipantStore,
		maxParallel:  maxP,
	}
	c.restoreAgentResultDeliveries()
	c.registerRootThread()
	statusCh := make(chan subagent.Notification, 64)
	mgr.Subscribe(statusCh)
	c.statusCh = statusCh
	c.statusStop = make(chan struct{})
	c.statusDone = make(chan struct{})
	go func() {
		defer close(c.statusDone)
		c.consumeWorkerStatus(statusCh)
	}()
	_ = c.restoreQueuedSpawns()
	go c.maybeStartQueued(context.Background())
	return c, nil
}

// Manager exposes the underlying subagent.Manager for advanced use
// (Subscribe, etc.).
func (c *AgentControl) Manager() *subagent.Manager {
	return c.manager
}

// UpdateWorkerDefaults changes the runtime defaults used by future worker
// spawns. Running workers keep their existing runners.
func (c *AgentControl) UpdateWorkerDefaults(client providers.StreamClient, defaultModel string, opts subagent.ManagerOptions) {
	if c == nil || c.manager == nil {
		return
	}
	c.manager.UpdateDefaults(client, defaultModel, opts)
}

// Close stops AgentControl-owned background consumers. It does not cancel
// running workers; callers should StopAll first when they need cancellation.
func (c *AgentControl) Close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(func() {
		if c.manager != nil && c.statusCh != nil {
			c.manager.Unsubscribe(c.statusCh)
		}
		if c.statusStop != nil {
			close(c.statusStop)
		}
		if c.statusDone != nil {
			<-c.statusDone
		}
	})
}

// HarnessStore exposes the durable task graph store for tests and UI adapters.
func (c *AgentControl) HarnessStore() *harness.Store {
	if c == nil {
		return nil
	}
	return c.harnessStore
}

// Threads exposes the in-memory thread registry. Tests use it to assert
// thread state directly without round-tripping through the persisted store.
func (c *AgentControl) Threads() *agentthread.Registry {
	if c == nil {
		return nil
	}
	return c.threads
}

// SetSessionInfo updates the coordinator's session ID and history dir
// after the session runtime has assigned them. Safe to call once at startup.
func (c *AgentControl) SetSessionInfo(sessionID, historyDir string, threadDir ...string) {
	c.sessionID = sessionID
	c.historyDir = historyDir
	if len(threadDir) > 0 && strings.TrimSpace(threadDir[0]) != "" {
		c.threadDir = strings.TrimSpace(threadDir[0])
		c.threadStore = agentthread.NewStore(c.threadDir)
	} else if strings.TrimSpace(historyDir) != "" {
		c.threadDir = filepath.Join(filepath.Dir(historyDir), "threads")
		c.threadStore = agentthread.NewStore(c.threadDir)
	}
	c.restoreAgentResultDeliveries()
	if c.threadDir != "" {
		c.setHarnessDir(filepath.Join(filepath.Dir(c.threadDir), "harness"))
	}
	c.registerRootThread()
}

func (c *AgentControl) setHarnessDir(dir string) {
	c.harnessDir = strings.TrimSpace(dir)
	c.harnessStore = harness.NewStore(c.harnessDir)
}

// SessionID returns the bound session ID, or "session-pending" if
// SetSessionInfo hasn't been called yet.
func (c *AgentControl) SessionID() string {
	return c.sessionID
}

// SetWorkerProviderName records the name of the provider the
// AgentControl's worker runtime is currently configured for. The
// model-pin resolver (installed via SetModelPinClientResolver) consults
// it to decide whether a queued spawn's pin targets the same provider
// or a different one. Passing an empty string clears the binding.
func (c *AgentControl) SetWorkerProviderName(name string) {
	if c == nil {
		return
	}
	c.workerProviderNameMu.Lock()
	defer c.workerProviderNameMu.Unlock()
	c.workerProviderName = strings.TrimSpace(name)
}

// WorkerProviderName returns the provider name installed via
// SetWorkerProviderName, or "" when none is set.
func (c *AgentControl) WorkerProviderName() string {
	if c == nil {
		return ""
	}
	c.workerProviderNameMu.Lock()
	defer c.workerProviderNameMu.Unlock()
	return c.workerProviderName
}

// SetModelPinClientResolver installs the callback that rebuilds the
// stream client for a queued spawn whose raw pin targets a different
// provider. Appserver (internal/appserver) owns the resolver because it
// holds the runtime config + provider factory used to build the worker
// client. Passing nil removes any previously installed resolver; in
// that case a queued spawn restored with a cross-provider pin fails
// explicitly instead of silently using the worker default client.
//
// The resolver signature:
//   - bare-model / same-provider pin → (model, nil, nil)
//   - cross-provider pin with a working provider → (model, freshClient, nil)
//   - any error → fail the queued spawn with that error visible on the
//     thread + harness task (never silently fall back).
func (c *AgentControl) SetModelPinClientResolver(resolver ModelPinClientResolver) {
	if c == nil {
		return
	}
	c.modelPinResolverMu.Lock()
	defer c.modelPinResolverMu.Unlock()
	c.modelPinResolver = resolver
}

func (c *AgentControl) currentModelPinResolver() ModelPinClientResolver {
	if c == nil {
		return nil
	}
	c.modelPinResolverMu.Lock()
	defer c.modelPinResolverMu.Unlock()
	return c.modelPinResolver
}

// pinTargetsDifferentProvider reports whether the raw participant pin
// (e.g. "alt:model") names a provider that is not the worker's current
// default provider. A bare model (no colon) and a same-provider pin
// both return false. Mirrors appserver.parseParticipantModelPin so
// agentcontrol does not depend on the appserver package.
func pinTargetsDifferentProvider(rawPin, workerProvider string) bool {
	value := strings.TrimSpace(rawPin)
	if value == "" {
		return false
	}
	idx := strings.Index(value, ":")
	if idx < 0 {
		return false
	}
	pinProvider := strings.TrimSpace(value[:idx])
	if pinProvider == "" {
		return false
	}
	workerProvider = strings.TrimSpace(workerProvider)
	if workerProvider == "" {
		return true
	}
	return pinProvider != workerProvider
}

// SpawnRequest is the internal shape of a spawn_agent tool invocation
// after argument validation.
type SpawnRequest struct {
	Type          string
	TaskName      string
	ParticipantID string
	AgentProfile  string // optional durable memory profile to wake for this worker
	Description   string
	Prompt        string
	ParentID      string
	ParentPath    string
	GoalID        string
	GoalDir       string
	BaseRepo      string // optional: chain off another worktree (worktree mode only)
	Synchronous   bool
	Timeout       time.Duration
	// SpeechCapability is internal-only. It is set by conversation-native
	// app-server paths, never by the LLM-facing spawn_agent tool.
	SpeechCapability bool
	// Isolation overrides the worker type's DefaultIsolation when set.
	// Empty string means "use the type default". Use this from
	// spawn_agent to opt a normally-inplace worker into a worktree
	// (e.g. an explorer that needs to run a destructive script).
	Isolation string
	// ModelOverride and ClientOverride are internal-only spawn hooks used
	// when a per-participant model pin diverges from the runtime
	// worker default. ModelOverride replaces the model string for this
	// single run; ClientOverride, when non-nil, replaces the stream
	// client the runner would otherwise inherit from the worker
	// defaults. Both fields are set together when a named participant
	// pins a different provider, and ModelOverride alone when it pins
	// a model on the worker's current provider. The LLM-facing
	// spawn_agent tool MUST NOT expose either field.
	ModelOverride  string
	ClientOverride providers.StreamClient
	// ModelPin is the raw participant pin (e.g. "alt-provider:model" or
	// "bare-model"). It is persisted with queued spawns so the restore
	// path can rebuild ClientOverride via the registered
	// ModelPinClientResolver when the pin targets a different
	// provider. Empty pin means the spawn honors only ModelOverride
	// (or the worker default when ModelOverride is also empty). Like
	// ModelOverride and ClientOverride, this field is internal-only
	// and must not be exposed through the LLM-facing spawn_agent
	// tool.
	ModelPin string
}

// SpawnResult is what the spawn_agent tool returns to the model.
type SpawnResult struct {
	Action          string   `json:"action"`
	ResultID        string   `json:"result_id,omitempty"`
	AgentID         string   `json:"agent_id"`
	TaskName        string   `json:"task_name,omitempty"`
	AgentProfile    string   `json:"agent_profile,omitempty"`
	AgentPath       string   `json:"agent_path,omitempty"`
	Status          string   `json:"status"`
	Isolation       string   `json:"isolation"`               // "inplace" or "worktree"
	WorktreePath    string   `json:"worktree_path,omitempty"` // empty for inplace spawns
	Result          string   `json:"result,omitempty"`
	ResultPath      string   `json:"result_path,omitempty"`
	ResultBytes     int      `json:"result_bytes,omitempty"`
	ResultTruncated bool     `json:"result_truncated,omitempty"`
	Error           string   `json:"error,omitempty"`
	DurationMS      int64    `json:"duration_ms,omitempty"`
	ResultConsumed  bool     `json:"result_consumed,omitempty"`
	ConsumedBy      string   `json:"consumed_by,omitempty"`
	NextSteps       []string `json:"next_steps,omitempty"`
}

type preparedSpawn struct {
	WorkerID         string
	ParticipantID    string
	WorkerType       WorkerType
	ThreadMeta       agentthread.Metadata
	Description      string
	Prompt           string
	GoalID           string
	GoalDir          string
	Isolation        IsolationMode
	SpeechCapability bool
	BaseRepo         string
	IsFork           bool
	ForkMode         string
	ParentHistory    []providers.ChatMessage
	// ModelOverride and ClientOverride carry a per-participant model
	// pin across the queue boundary so that even queued spawns honor
	// the pin once they dequeue.
	ModelOverride  string
	ClientOverride providers.StreamClient
	// ModelPin is the raw participant Model pin (e.g. "p:m" or "m").
	// It is persisted alongside ModelOverride so a queued spawn can
	// rebuild the ClientOverride on restart when the pin targets a
	// provider different from the worker default. The resolver
	// callback installed via SetModelPinClientResolver owns the
	// policy: bare-model / same-provider pins just change the model,
	// cross-provider pins MUST resolve to a fresh client — a nil
	// client or resolver error fails the spawn explicitly so a wrong
	// provider is never used.
	ModelPin string
}

type queuedSpawnPayload struct {
	WorkerID         string                  `json:"worker_id"`
	ParticipantID    string                  `json:"participant_id,omitempty"`
	WorkerType       string                  `json:"worker_type"`
	ThreadMeta       agentthread.Metadata    `json:"thread_meta"`
	Description      string                  `json:"description,omitempty"`
	Prompt           string                  `json:"prompt"`
	GoalID           string                  `json:"goal_id,omitempty"`
	GoalDir          string                  `json:"goal_dir,omitempty"`
	Isolation        string                  `json:"isolation"`
	SpeechCapability bool                    `json:"speech_capability,omitempty"`
	BaseRepo         string                  `json:"base_repo,omitempty"`
	IsFork           bool                    `json:"is_fork,omitempty"`
	ForkMode         string                  `json:"fork_mode,omitempty"`
	ParentHistory    []providers.ChatMessage `json:"parent_history,omitempty"`
	// ModelOverride is persisted with the queued payload so the
	// per-participant model pin survives session restart. The
	// ClientOverride is intentionally NOT persisted — reconstructing a
	// provider client from the queue on its own is not safe, and a
	// restart that drops a per-participant pin falls back to the
	// worker default (matching the empty pin semantics).
	//
	// ModelPin is the raw participant pin (e.g. "p:m" or "m"). When
	// present, the restore path asks the registered
	// ModelPinClientResolver to rebuild the client: a cross-provider
	// pin MUST return a non-nil client or an error. A missing
	// resolver is treated as an error rather than a silent fallback,
	// because a queued spawn restored with no resolver and a
	// cross-provider pin would otherwise route to the wrong provider.
	ModelOverride string `json:"model_override,omitempty"`
	ModelPin      string `json:"model_pin,omitempty"`
}

// Spawn launches a sub-agent. In synchronous mode it waits until the child
// finishes or the caller's context is cancelled; in async mode it returns
// immediately with status "running" and the agent_id the orchestrator can join
// later or receive via completion notification.
func (c *AgentControl) Spawn(ctx context.Context, req SpawnRequest) (*SpawnResult, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("prompt is required")
	}

	// Resolve worker type (validates the name).
	wt, err := LookupWorkerType(req.Type)
	if err != nil {
		return nil, err
	}
	wtype := wt.Name

	workerID := newAgentControlWorkerID(wtype)
	taskName := req.TaskName
	agentProfile := strings.TrimSpace(req.AgentProfile)

	// Resolve effective isolation: caller override > type default.
	isolation, err := NormalizeIsolation(req.Isolation, wt)
	if err != nil {
		return nil, err
	}
	// BaseRepo only makes sense for chained worktree spawns.
	if isolation == IsolationInplace && strings.TrimSpace(req.BaseRepo) != "" {
		return nil, errors.New("base_repo is only supported with isolation=worktree")
	}

	if c.manager.CountRunning() >= c.maxParallel {
		if req.Synchronous {
			return nil, fmt.Errorf("max parallel sub-agents reached (%d). Wait for one to complete or use async spawn so the task can queue.", c.maxParallel)
		}
		threadMeta, err := c.registerChildThreadWithStatus(workerID, taskName, agentProfile, wtype, req.Prompt, agentthread.SourceThreadSpawn, "", req.ParentID, req.ParentPath, agentthread.StatusPending)
		if err != nil {
			return nil, err
		}
		participantID := strings.TrimSpace(req.ParticipantID)
		if participantID == "" {
			participantID = c.newEphemeralParticipant(threadMeta.TaskName, wt).ID
		}
		prepared := preparedSpawn{
			WorkerID:         workerID,
			ParticipantID:    participantID,
			WorkerType:       wt,
			ThreadMeta:       threadMeta,
			Description:      req.Description,
			Prompt:           req.Prompt,
			GoalID:           strings.TrimSpace(req.GoalID),
			GoalDir:          strings.TrimSpace(req.GoalDir),
			Isolation:        isolation,
			SpeechCapability: req.SpeechCapability,
			BaseRepo:         req.BaseRepo,
			ModelOverride:    strings.TrimSpace(req.ModelOverride),
			ClientOverride:   req.ClientOverride,
			ModelPin:         strings.TrimSpace(req.ModelPin),
		}
		c.recordHarnessTaskQueued(threadMeta, wtype, req.Prompt, isolation, req.BaseRepo, req.GoalID, req.GoalDir)
		if err := c.enqueuePreparedSpawn(prepared); err != nil {
			c.recordHarnessTaskFailure(workerID, err)
			return nil, err
		}
		return &SpawnResult{
			Action:       "spawn_agent",
			AgentID:      workerID,
			TaskName:     threadMeta.TaskName,
			AgentProfile: threadMeta.AgentProfile,
			AgentPath:    threadMeta.Path,
			Status:       "queued",
			Isolation:    string(isolation),
			NextSteps:    spawnResultNextSteps("queued", false, string(isolation), threadMeta.Path),
		}, nil
	}

	// 1. Determine the worker's working directory.
	//    - inplace: share the parent repo (no checkout cost)
	//    - worktree: `git worktree add --detach` based on parent HEAD
	var (
		workerRoot  string
		worktreeRef *worktree.Worktree
	)
	if isolation == IsolationWorktree {
		if c.worktrees == nil {
			return nil, errors.New("isolation=worktree requires repository worktree support (this workspace does not support isolated worktrees)")
		}
		worktreeRef, err = c.worktrees.Create(c.sessionID, workerID, req.BaseRepo)
		if err != nil {
			return nil, fmt.Errorf("worktree create: %w", err)
		}
		workerRoot = worktreeRef.Path
	} else {
		workerRoot = c.parentRepo
	}

	// 2. Register the child thread before launch so the visible worker
	// ID, worktree ID, and thread path all point at the same task.
	threadMeta, err := c.registerChildThread(workerID, taskName, agentProfile, wtype, req.Prompt, agentthread.SourceThreadSpawn, "", req.ParentID, req.ParentPath)
	if err != nil {
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		return nil, err
	}
	c.recordHarnessTaskStart(threadMeta, wtype, req.Prompt, workerRoot, isolation, req.BaseRepo, req.GoalID, req.GoalDir)

	// Create the worker's conversation participant identity. Failure to
	// persist never blocks the spawn.
	participantID := strings.TrimSpace(req.ParticipantID)
	if participantID == "" {
		participantID = c.newEphemeralParticipant(threadMeta.TaskName, wt).ID
	}
	if req.SpeechCapability {
		c.EnableParticipantSpeech(workerID)
	}

	// 3. Build worker's toolkit rooted at the chosen working directory.
	workerKit, err := c.workerFact(workerRoot, wt, threadMeta)
	if err != nil {
		if failed, ok := c.threads.UpdateStatus(workerID, agentthread.StatusFailed, time.Now().UTC()); ok {
			_ = c.threadStore.RecordStatus(failed)
		}
		c.recordHarnessTaskFailure(workerID, fmt.Errorf("worker toolkit: %w", err))
		if closed, ok := c.threads.UpdateEdgeStatus(workerID, agentthread.EdgeClosed, time.Now().UTC()); ok {
			_ = c.threadStore.RecordEdgeStatus(closed)
		}
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		return nil, fmt.Errorf("worker toolkit: %w", err)
	}

	// 4. Compose system prompt: type-specific role + working dir + base prompt.
	sys, err := c.workerSystemPrompt(workerRoot, wt, threadMeta, isolation)
	if err != nil {
		if failed, ok := c.threads.UpdateStatus(workerID, agentthread.StatusFailed, time.Now().UTC()); ok {
			_ = c.threadStore.RecordStatus(failed)
		}
		c.recordHarnessTaskFailure(workerID, fmt.Errorf("worker system prompt: %w", err))
		if closed, ok := c.threads.UpdateEdgeStatus(workerID, agentthread.EdgeClosed, time.Now().UTC()); ok {
			_ = c.threadStore.RecordEdgeStatus(closed)
		}
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		return nil, fmt.Errorf("worker system prompt: %w", err)
	}

	// 5. History path.
	historyPath := ""
	if c.historyDir != "" {
		historyPath = filepath.Join(c.historyDir, workerID+".json")
	}

	// 6. Spawn via manager using the ID already allocated by the
	// AgentControl. That keeps worktree paths, persisted thread
	// metadata, and visible agent IDs aligned.
	workerCtx := ctx
	if !req.Synchronous {
		workerCtx = context.WithoutCancel(ctx)
	}

	sa, err := c.manager.Spawn(workerCtx, subagent.SpawnOptions{
		ID:            workerID,
		ParticipantID: participantID,
		Type:          wtype,
		TaskName:      threadMeta.TaskName,
		AgentProfile:  threadMeta.AgentProfile,
		AgentPath:     threadMeta.Path,
		ParentID:      threadMeta.ParentID,
		Description:   req.Description,
		Prompt:        req.Prompt,
		SystemPrompt:  sys,
		Toolkit:       workerKit,
		HistoryPath:   historyPath,
		WorkerRoot:    workerRoot,
		Model:         strings.TrimSpace(req.ModelOverride),
		ModelPin:      strings.TrimSpace(req.ModelPin),
		Client:        req.ClientOverride,
	})
	if err != nil {
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		c.recordHarnessTaskFailure(workerID, fmt.Errorf("spawn: %w", err))
		return nil, fmt.Errorf("spawn: %w", err)
	}

	result := &SpawnResult{
		Action:       "spawn_agent",
		AgentID:      sa.ID,
		TaskName:     threadMeta.TaskName,
		AgentProfile: threadMeta.AgentProfile,
		AgentPath:    threadMeta.Path,
		Status:       string(sa.Status),
		Isolation:    string(isolation),
	}
	result.NextSteps = spawnResultNextSteps(result.Status, req.Synchronous, result.Isolation, result.AgentPath)
	if worktreeRef != nil {
		result.WorktreePath = worktreeRef.Path
	}

	if !req.Synchronous {
		return result, nil
	}

	// Synchronous mode: wait for completion. There is no hidden default
	// lifetime here; user stop, session shutdown, CLI/app timeout, or an
	// explicit request timeout owns cancellation.
	waitCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	snap, err := c.manager.Wait(waitCtx, sa.ID)
	if err != nil {
		return nil, fmt.Errorf("wait: %w", err)
	}
	result.Status = string(snap.Status)
	resultID, claimed, consumedBy := c.claimAgentResultDelivery(snap, agentResultConsumerSpawnAgent)
	result.ResultID = resultID
	ref := c.AgentResultReference(snap)
	result.Result = ref.Preview
	result.ResultPath = ref.Path
	result.ResultBytes = ref.Bytes
	result.ResultTruncated = ref.Truncated
	if resultID != "" && !claimed {
		result.ResultConsumed = true
		result.ConsumedBy = consumedBy
		result.Result = ""
	}
	if snap.Error != nil {
		result.Error = snap.Error.Error()
	}
	if !snap.CompletedAt.IsZero() && !snap.StartedAt.IsZero() {
		result.DurationMS = snap.CompletedAt.Sub(snap.StartedAt).Milliseconds()
	}
	result.NextSteps = spawnResultNextSteps(result.Status, true, result.Isolation, result.AgentPath)

	return result, nil
}

// newEphemeralParticipant creates the participant identity for a
// freshly spawned worker and persists it through the configured
// ParticipantStore. Persistence failures are logged but never block a
// spawn; the in-memory identity is returned regardless.
func (c *AgentControl) newEphemeralParticipant(taskName string, wt WorkerType) participant.Participant {
	now := time.Now().UTC()
	p := participant.Participant{
		ID:        participant.NewID(),
		Kind:      participant.KindEphemeral,
		Name:      participant.DeriveEphemeralName(taskName, wt.Name),
		Role:      wt.Name,
		Avatar:    participant.DefaultAvatar(wt.Name),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if c != nil && c.participants != nil {
		if err := c.participants.Upsert(p); err != nil {
			providers.DebugLogf("agentcontrol: persist participant %s: %v", p.ID, err)
		}
	}
	return p
}

func spawnResultNextSteps(status string, synchronous bool, isolation string, agentPath string) []string {
	pathHint := strings.TrimSpace(agentPath)
	if pathHint == "" {
		pathHint = "this agent"
	}
	worktreeHint := ""
	if IsolationMode(isolation) == IsolationWorktree {
		worktreeHint = " Inspect worktree_path and the worker's patch artifacts before merging or relying on file changes."
	}
	switch subagent.Status(strings.TrimSpace(status)) {
	case subagent.StatusQueued:
		return []string{
			"The worker is queued; do not spawn a duplicate for the same task unless requirements change.",
			"Continue non-overlapping local work when available, or use await_agents with " + pathHint + " only when synthesis depends on this output.",
		}
	case subagent.StatusRunning:
		return []string{
			"Continue non-overlapping local work when available; the worker will send a background completion notification when it finishes.",
			"Use await_agents with " + pathHint + " only when the next step depends on this worker's output." + worktreeHint,
		}
	case subagent.StatusCompleted:
		if synchronous {
			return []string{
				"Inspect the worker result and any agent_report artifacts before relying on the handoff.",
				"Use workflow_control when this result must be bound into a larger workflow record." + worktreeHint,
			}
		}
		return []string{
			"Inspect the worker's completion notification and agent_report artifacts before relying on the handoff." + worktreeHint,
		}
	case subagent.StatusFailed:
		return []string{
			"Inspect error and any partial artifacts, then decide whether to retry with a narrower brief, rollback, or ask the user.",
		}
	default:
		return []string{
			"Inspect the worker status before deciding whether to continue local work, await the worker, retry, or close the task.",
		}
	}
}

// ForkRequest is the internal shape of a spawn_agent invocation where
// subagent_type is omitted. It always uses the default general-purpose
// agent definition, but isolation is still caller-selectable: a forked
// history and a worktree are orthogonal concerns.
type ForkRequest struct {
	TaskName     string
	AgentProfile string // optional durable memory profile to wake for this worker
	Description  string
	ForkMode     string
	ParentID     string
	ParentPath   string
	BaseRepo     string // optional: chain off another worktree (worktree mode only)
	// Isolation overrides the default agent type's DefaultIsolation
	// when set. Empty string means "use the type default".
	Isolation string
	// Prompt is what the worker sees as its FINAL user message,
	// appended to the inherited history. Callers should wrap any
	// role-override instructions in <system-reminder> tags so the
	// model treats them as authoritative over anything in the
	// inherited parent system prompt.
	Prompt      string
	Synchronous bool
	Timeout     time.Duration
}

// Fork launches a sub-agent that inherits a snapshot of the parent
// agent's conversation history. The worker's first request to the
// LLM provider replays the parent's history verbatim and adds the
// fork prompt as the final user message — preserving prompt-cache
// hits across the fork boundary.
//
// `parentHistory` MUST be a complete history with no dangling
// tool_use blocks: the caller is expected to have already stripped the
// in-flight spawn_agent
// assistant turn before passing it through.
func (c *AgentControl) Fork(ctx context.Context, req ForkRequest, parentHistory []providers.ChatMessage) (*SpawnResult, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("prompt is required")
	}
	if len(parentHistory) == 0 {
		return nil, errors.New("spawn_agent fork: no parent history (only the main agent in an interactive session can fork)")
	}

	// Resolve the default agent type so the fork has the full tool set.
	wt, err := LookupWorkerType(DefaultSubagentType)
	if err != nil {
		return nil, err
	}

	workerID := newAgentControlWorkerID(wt.Name)
	taskName := req.TaskName
	agentProfile := strings.TrimSpace(req.AgentProfile)
	isolation, err := NormalizeIsolation(req.Isolation, wt)
	if err != nil {
		return nil, err
	}
	if isolation == IsolationInplace && strings.TrimSpace(req.BaseRepo) != "" {
		return nil, errors.New("base_repo is only supported with isolation=worktree")
	}

	if c.manager.CountRunning() >= c.maxParallel {
		if req.Synchronous {
			return nil, fmt.Errorf("max parallel sub-agents reached (%d). Wait for one to complete or use async spawn so the task can queue.", c.maxParallel)
		}
		threadMeta, err := c.registerChildThreadWithStatus(workerID, taskName, agentProfile, wt.Name, req.Prompt, agentthread.SourceThreadSpawn, req.ForkMode, req.ParentID, req.ParentPath, agentthread.StatusPending)
		if err != nil {
			return nil, err
		}
		prt := c.newEphemeralParticipant(threadMeta.TaskName, wt)
		prepared := preparedSpawn{
			WorkerID:      workerID,
			ParticipantID: prt.ID,
			WorkerType:    wt,
			ThreadMeta:    threadMeta,
			Description:   req.Description,
			Prompt:        req.Prompt,
			Isolation:     isolation,
			BaseRepo:      req.BaseRepo,
			IsFork:        true,
			ForkMode:      req.ForkMode,
			ParentHistory: providers.CloneChatMessages(parentHistory),
		}
		c.recordHarnessTaskQueued(threadMeta, wt.Name, req.Prompt, isolation, req.BaseRepo, "", "")
		if err := c.enqueuePreparedSpawn(prepared); err != nil {
			c.recordHarnessTaskFailure(workerID, err)
			return nil, err
		}
		return &SpawnResult{
			Action:       "spawn_agent",
			AgentID:      workerID,
			TaskName:     threadMeta.TaskName,
			AgentProfile: threadMeta.AgentProfile,
			AgentPath:    threadMeta.Path,
			Status:       "queued",
			Isolation:    string(isolation),
			NextSteps:    spawnResultNextSteps("queued", false, string(isolation), threadMeta.Path),
		}, nil
	}

	var (
		workerRoot  string
		worktreeRef *worktree.Worktree
	)
	if isolation == IsolationWorktree {
		if c.worktrees == nil {
			return nil, errors.New("isolation=worktree requires repository worktree support (this workspace does not support isolated worktrees)")
		}
		worktreeRef, err = c.worktrees.Create(c.sessionID, workerID, req.BaseRepo)
		if err != nil {
			return nil, fmt.Errorf("worktree create: %w", err)
		}
		workerRoot = worktreeRef.Path
	} else {
		workerRoot = c.parentRepo
	}

	forkPrompt := req.Prompt
	if isolation == IsolationWorktree {
		forkPrompt = appendForkWorktreeReminder(forkPrompt, workerRoot, isolation)
	}

	threadMeta, err := c.registerChildThread(workerID, taskName, agentProfile, wt.Name, forkPrompt, agentthread.SourceThreadSpawn, req.ForkMode, req.ParentID, req.ParentPath)
	if err != nil {
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		return nil, err
	}
	c.recordHarnessTaskStart(threadMeta, wt.Name, req.Prompt, workerRoot, isolation, req.BaseRepo, "", "")

	// Create the worker's conversation participant identity. Failure to
	// persist never blocks the spawn.
	prt := c.newEphemeralParticipant(threadMeta.TaskName, wt)

	workerKit, err := c.workerFact(workerRoot, wt, threadMeta)
	if err != nil {
		if failed, ok := c.threads.UpdateStatus(workerID, agentthread.StatusFailed, time.Now().UTC()); ok {
			_ = c.threadStore.RecordStatus(failed)
		}
		c.recordHarnessTaskFailure(workerID, fmt.Errorf("worker toolkit: %w", err))
		if closed, ok := c.threads.UpdateEdgeStatus(workerID, agentthread.EdgeClosed, time.Now().UTC()); ok {
			_ = c.threadStore.RecordEdgeStatus(closed)
		}
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		return nil, fmt.Errorf("worker toolkit: %w", err)
	}

	historyPath := ""
	if c.historyDir != "" {
		historyPath = filepath.Join(c.historyDir, workerID+".json")
	}

	initialHistory := providers.CloneChatMessages(parentHistory)
	sys, sysErr := c.workerSystemPrompt(workerRoot, wt, threadMeta, isolation)
	if sysErr != nil {
		if failed, ok := c.threads.UpdateStatus(workerID, agentthread.StatusFailed, time.Now().UTC()); ok {
			_ = c.threadStore.RecordStatus(failed)
		}
		if closed, ok := c.threads.UpdateEdgeStatus(workerID, agentthread.EdgeClosed, time.Now().UTC()); ok {
			_ = c.threadStore.RecordEdgeStatus(closed)
		}
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		c.recordHarnessTaskFailure(workerID, fmt.Errorf("worker system prompt: %w", sysErr))
		return nil, fmt.Errorf("worker system prompt: %w", sysErr)
	}
	initialHistory = withInitialSystemPrompt(initialHistory, sys)

	// Forks keep the parent's conversation body but always replace the
	// system prompt so the worker prompt and worker toolkit come from
	// the same compiled model surface.
	workerCtx := ctx
	if !req.Synchronous {
		workerCtx = context.WithoutCancel(ctx)
	}

	sa, err := c.manager.Spawn(workerCtx, subagent.SpawnOptions{
		ID:             workerID,
		ParticipantID:  prt.ID,
		Type:           wt.Name,
		TaskName:       threadMeta.TaskName,
		AgentProfile:   threadMeta.AgentProfile,
		AgentPath:      threadMeta.Path,
		ParentID:       threadMeta.ParentID,
		Description:    req.Description,
		Prompt:         forkPrompt,
		Toolkit:        workerKit,
		HistoryPath:    historyPath,
		WorkerRoot:     workerRoot,
		InitialHistory: initialHistory,
	})
	if err != nil {
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		c.recordHarnessTaskFailure(workerID, fmt.Errorf("spawn: %w", err))
		return nil, fmt.Errorf("spawn: %w", err)
	}

	result := &SpawnResult{
		Action:       "spawn_agent",
		AgentID:      sa.ID,
		TaskName:     threadMeta.TaskName,
		AgentProfile: threadMeta.AgentProfile,
		AgentPath:    threadMeta.Path,
		Status:       string(sa.Status),
		Isolation:    string(isolation),
	}
	result.NextSteps = spawnResultNextSteps(result.Status, req.Synchronous, result.Isolation, result.AgentPath)
	if worktreeRef != nil {
		result.WorktreePath = worktreeRef.Path
	}

	if !req.Synchronous {
		return result, nil
	}

	waitCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	snap, err := c.manager.Wait(waitCtx, sa.ID)
	if err != nil {
		return nil, fmt.Errorf("wait: %w", err)
	}
	result.Status = string(snap.Status)
	ref := c.AgentResultReference(snap)
	result.Result = ref.Preview
	result.ResultPath = ref.Path
	result.ResultBytes = ref.Bytes
	result.ResultTruncated = ref.Truncated
	if snap.Error != nil {
		result.Error = snap.Error.Error()
	}
	if !snap.CompletedAt.IsZero() && !snap.StartedAt.IsZero() {
		result.DurationMS = snap.CompletedAt.Sub(snap.StartedAt).Milliseconds()
	}
	result.NextSteps = spawnResultNextSteps(result.Status, true, result.Isolation, result.AgentPath)
	return result, nil
}

// StopAll cancels every running worker. Used for Ctrl+C handling.
func (c *AgentControl) StopAll() {
	c.manager.StopAll()
}

// Stop cancels a specific worker by ID, path, or task name. Returns false if not found.
func (c *AgentControl) Stop(target string) bool {
	return c.StopFrom(agentthread.RootPath, target)
}

func (c *AgentControl) StopFrom(currentPath, target string) bool {
	meta, ok := c.threads.ResolveFrom(currentPath, target)
	if !ok || meta.Path == agentthread.RootPath {
		return false
	}
	subtree := c.threads.Subtree(meta.ID)
	if len(subtree) == 0 {
		subtree = []agentthread.Metadata{meta}
	}
	stopped := false
	now := time.Now().UTC()
	for _, node := range subtree {
		if node.Path == agentthread.RootPath {
			continue
		}
		if c.manager.Stop(node.ID) {
			stopped = true
		}
		if closed, found := c.threads.UpdateEdgeStatus(node.ID, agentthread.EdgeClosed, now); found {
			_ = c.threadStore.RecordEdgeStatus(closed)
		}
	}
	return stopped
}

// List returns snapshots of all sub-agents in this session.
func (c *AgentControl) List() []subagent.SubAgentSnapshot {
	return c.ListFrom(agentthread.RootPath, "")
}

func (c *AgentControl) ListFrom(currentPath, pathPrefix string) []subagent.SubAgentSnapshot {
	list := c.manager.List()
	known := make(map[string]struct{}, len(list))
	for _, snap := range list {
		known[snap.ID] = struct{}{}
	}
	for _, snap := range c.queuedSnapshots() {
		if _, ok := known[snap.ID]; ok {
			continue
		}
		list = append(list, snap)
	}
	prefix := strings.TrimSpace(pathPrefix)
	if prefix == "" {
		return list
	}
	resolved, err := agentthread.ResolveAgentPath(agentthread.AgentPath(currentPath), prefix)
	if err != nil {
		if parsed, parseErr := agentthread.ParseAgentPath(prefix); parseErr == nil {
			resolved = parsed
		} else {
			return nil
		}
	}
	want := string(resolved)
	out := make([]subagent.SubAgentSnapshot, 0, len(list))
	for _, snap := range list {
		if snap.AgentPath == want || strings.HasPrefix(snap.AgentPath, want+"/") {
			out = append(out, snap)
		}
	}
	return out
}

func (c *AgentControl) queuedSnapshots() []subagent.SubAgentSnapshot {
	if c == nil || c.harnessStore == nil {
		return nil
	}
	tasks, err := c.harnessStore.ListTasks()
	if err != nil {
		return nil
	}
	out := make([]subagent.SubAgentSnapshot, 0)
	for _, task := range tasks {
		if task.Status != harness.TaskStatusQueued {
			continue
		}
		out = append(out, subagent.SubAgentSnapshot{
			ID:          task.ID,
			Type:        task.Role,
			TaskName:    task.Name,
			AgentPath:   task.Path,
			ParentID:    task.ParentID,
			Description: task.Name,
			Status:      subagent.StatusQueued,
			StartedAt:   task.StartedAt,
		})
	}
	return out
}

// SendMessage delivers a follow-up message to a specific sub-agent.
// Messages are queued while the worker is running and injected as
// user-role turns before the next model round.
func (c *AgentControl) SendMessage(target, message string) error {
	return c.SendMessageFrom(agentthread.RootPath, target, message)
}

func (c *AgentControl) SendMessageFrom(currentPath, target, message string) error {
	id := c.resolveAgentIDFrom(currentPath, target)
	if id == "" {
		return errors.New("target is required")
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return errors.New("message is required")
	}
	sa := c.manager.Get(id)
	if sa == nil {
		return fmt.Errorf("agent %q not found", id)
	}
	snap := sa.Snapshot()
	// Queue-or-resume, identical to followup_task: a running target keeps
	// the message in its mailbox for its next model round without being
	// interrupted, while a terminal target (completed, failed, cancelled)
	// is revived in place with its full context plus this message.
	// send_message carries trigger_turn=false so the child reads it as
	// interim communication rather than a task hand-off.
	communication := newInterAgentCommunication(currentPath, snap.AgentPath, msg, false)
	if _, err := c.manager.Followup(context.Background(), id, communication.String()); err != nil {
		return err
	}
	_ = c.threadStore.RecordCommunication(id, communication)
	if meta, ok := c.threads.UpdateLastTaskMessage(id, msg, time.Now().UTC()); ok {
		_ = c.threadStore.UpsertThread(meta)
	}
	return nil
}

func (c *AgentControl) FollowupTask(ctx context.Context, target, message string) (subagent.SubAgentSnapshot, error) {
	return c.FollowupTaskFrom(agentthread.RootPath, ctx, target, message)
}

func (c *AgentControl) FollowupTaskFrom(currentPath string, ctx context.Context, target, message string) (subagent.SubAgentSnapshot, error) {
	id := c.resolveAgentIDFrom(currentPath, target)
	if id == "" {
		return subagent.SubAgentSnapshot{}, errors.New("target is required")
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return subagent.SubAgentSnapshot{}, errors.New("message is required")
	}
	sa := c.manager.Get(id)
	if sa == nil {
		return subagent.SubAgentSnapshot{}, fmt.Errorf("agent %q not found", id)
	}
	current := sa.Snapshot()
	communication := newInterAgentCommunication(currentPath, current.AgentPath, msg, true)
	snap, err := c.manager.Followup(ctx, id, communication.String())
	if err != nil {
		return snap, err
	}
	_ = c.threadStore.RecordCommunication(id, communication)
	if meta, ok := c.threads.UpdateLastTaskMessage(id, msg, time.Now().UTC()); ok {
		_ = c.threadStore.UpsertThread(meta)
	}
	return snap, nil
}

func (c *AgentControl) Wait(ctx context.Context, target string) (subagent.SubAgentSnapshot, error) {
	return c.WaitFrom(agentthread.RootPath, ctx, target)
}

func (c *AgentControl) WaitFrom(currentPath string, ctx context.Context, target string) (subagent.SubAgentSnapshot, error) {
	id := c.resolveAgentIDFrom(currentPath, target)
	if id == "" {
		return subagent.SubAgentSnapshot{}, errors.New("target is required")
	}
	return c.manager.Wait(ctx, id)
}

const (
	WaitAgentSignalTimeout       = "timeout"
	WaitAgentSignalQueuedMessage = "queued_message"
	WaitAgentSignalCompleted     = "agent_completed"
	WaitAgentSignalFailed        = "agent_failed"
	WaitAgentSignalCancelled     = "agent_cancelled"
)

type WaitAgentSignal struct {
	Received            bool
	SignalType          string
	AgentID             string
	AgentPath           string
	TaskName            string
	ParentID            string
	Status              string
	Description         string
	PendingMessageCount int
}

func (c *AgentControl) WaitForAgentNotificationFrom(currentPath string, ctx context.Context) (WaitAgentSignal, error) {
	if c == nil || c.manager == nil {
		return WaitAgentSignal{}, errors.New("agent control not configured")
	}
	currentID := c.agentIDForPath(currentPath)
	if signal, ok := c.queuedMessageSignal(currentID); ok {
		return signal, nil
	}
	ch := make(chan subagent.Notification, 16)
	c.manager.Subscribe(ch)
	defer c.manager.Unsubscribe(ch)
	if signal, ok := c.queuedMessageSignal(currentID); ok {
		return signal, nil
	}
	for {
		select {
		case n := <-ch:
			if signal, ok := c.agentNotificationSignal(currentID, n); ok {
				return signal, nil
			}
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return WaitAgentSignal{SignalType: WaitAgentSignalTimeout}, nil
			}
			return WaitAgentSignal{}, ctx.Err()
		}
	}
}

func (c *AgentControl) WaitForMailboxUpdateFrom(currentPath string, ctx context.Context) (bool, error) {
	signal, err := c.WaitForAgentNotificationFrom(currentPath, ctx)
	if err != nil {
		return false, err
	}
	return signal.Received, nil
}

func (c *AgentControl) agentIDForPath(currentPath string) string {
	path := strings.TrimSpace(currentPath)
	if path == "" || path == agentthread.RootPath {
		return ""
	}
	if meta, ok := c.threads.ResolveFrom(path, path); ok && meta.Path != agentthread.RootPath {
		return meta.ID
	}
	return ""
}

func (c *AgentControl) isMailboxNotificationFor(currentID string, n subagent.Notification) bool {
	_, ok := c.agentNotificationSignal(currentID, n)
	return ok
}

func (c *AgentControl) queuedMessageSignal(currentID string) (WaitAgentSignal, bool) {
	if currentID == "" {
		return WaitAgentSignal{}, false
	}
	count := c.manager.PendingMessageCount(currentID)
	if count <= 0 {
		return WaitAgentSignal{}, false
	}
	signal := WaitAgentSignal{
		Received:            true,
		SignalType:          WaitAgentSignalQueuedMessage,
		AgentID:             currentID,
		PendingMessageCount: count,
	}
	if snap := c.snapshotByID(currentID); snap != nil {
		signal = waitAgentSignalFromSnapshot(WaitAgentSignalQueuedMessage, *snap)
		signal.PendingMessageCount = count
	}
	return signal, true
}

func (c *AgentControl) agentNotificationSignal(currentID string, n subagent.Notification) (WaitAgentSignal, bool) {
	if currentID == "" {
		if !isFinalSubAgentStatus(n.Status) {
			return WaitAgentSignal{}, false
		}
		parentID := strings.TrimSpace(n.Snapshot.ParentID)
		if parentID == "" || parentID == c.sessionID || parentID == c.rootThreadID {
			return waitAgentSignalFromSnapshot(waitAgentSignalTypeForStatus(n.Status), n.Snapshot), true
		}
		return WaitAgentSignal{}, false
	}
	if n.Snapshot.ID == currentID {
		if n.Snapshot.PendingMessageCount > 0 {
			signal := waitAgentSignalFromSnapshot(WaitAgentSignalQueuedMessage, n.Snapshot)
			signal.PendingMessageCount = n.Snapshot.PendingMessageCount
			return signal, true
		}
		if signal, ok := c.queuedMessageSignal(currentID); ok {
			return signal, true
		}
	}
	if strings.TrimSpace(n.Snapshot.ParentID) == currentID && isFinalSubAgentStatus(n.Status) {
		return waitAgentSignalFromSnapshot(waitAgentSignalTypeForStatus(n.Status), n.Snapshot), true
	}
	return WaitAgentSignal{}, false
}

func waitAgentSignalTypeForStatus(status subagent.Status) string {
	switch status {
	case subagent.StatusCompleted:
		return WaitAgentSignalCompleted
	case subagent.StatusFailed:
		return WaitAgentSignalFailed
	case subagent.StatusCancelled:
		return WaitAgentSignalCancelled
	default:
		return string(status)
	}
}

func waitAgentSignalFromSnapshot(signalType string, snap subagent.SubAgentSnapshot) WaitAgentSignal {
	return WaitAgentSignal{
		Received:    true,
		SignalType:  signalType,
		AgentID:     snap.ID,
		AgentPath:   snap.AgentPath,
		TaskName:    snap.TaskName,
		ParentID:    snap.ParentID,
		Status:      string(snap.Status),
		Description: snap.Description,
	}
}

// Subscribe forwards to the underlying manager so the UI can receive
// status notifications and publish mailbox messages.
func (c *AgentControl) Subscribe(ch chan<- subagent.Notification) {
	if c == nil || c.manager == nil {
		return
	}
	c.manager.Subscribe(ch)
}

func (c *AgentControl) Unsubscribe(ch chan<- subagent.Notification) {
	if c == nil || c.manager == nil {
		return
	}
	c.manager.Unsubscribe(ch)
}

func (c *AgentControl) SubscribeStream(ch chan<- subagent.StreamNotification) {
	if c == nil || c.manager == nil {
		return
	}
	c.manager.SubscribeStream(ch)
}

func (c *AgentControl) UnsubscribeStream(ch chan<- subagent.StreamNotification) {
	if c == nil || c.manager == nil {
		return
	}
	c.manager.UnsubscribeStream(ch)
}

func (c *AgentControl) registerRootThread() {
	if c == nil || c.threads == nil {
		return
	}
	sessionID := strings.TrimSpace(c.sessionID)
	if sessionID == "" || sessionID == "session-pending" {
		return
	}
	if c.rootThreadID == sessionID && c.rootThreadDir == c.threadDir {
		return
	}
	meta := c.threads.RegisterRoot(sessionID, sessionID, c.parentRepo, "", time.Now().UTC())
	_ = c.threadStore.UpsertThread(meta)
	c.rootThreadID = sessionID
	c.rootThreadDir = c.threadDir
}

func (c *AgentControl) workerSystemPrompt(rootDir string, wt WorkerType, meta agentthread.Metadata, isolation IsolationMode) (string, error) {
	base := ""
	if c != nil {
		base = c.defaultSys
	}
	if c != nil && c.workerPrompt != nil {
		customBase, err := c.workerPrompt(rootDir, wt, meta, isolation)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(customBase) != "" {
			base = customBase
		}
	}
	return composeWorkerSystemPrompt(base, wt, rootDir, isolation), nil
}

func withInitialSystemPrompt(history []providers.ChatMessage, systemPrompt string) []providers.ChatMessage {
	sys := strings.TrimSpace(systemPrompt)
	if sys == "" {
		return providers.CloneChatMessages(history)
	}
	out := make([]providers.ChatMessage, 0, len(history)+1)
	out = append(out, providers.ChatMessage{Role: "system", Content: sys})
	start := 0
	for start < len(history) && strings.TrimSpace(history[start].Role) == "system" {
		start++
	}
	out = append(out, providers.CloneChatMessages(history[start:])...)
	return out
}

func (c *AgentControl) registerChildThread(id, taskName, agentProfile, role, message string, source agentthread.SourceKind, forkMode, parentID, parentPath string) (agentthread.Metadata, error) {
	return c.registerChildThreadWithStatus(id, taskName, agentProfile, role, message, source, forkMode, parentID, parentPath, agentthread.StatusRunning)
}

func (c *AgentControl) registerChildThreadWithStatus(id, taskName, agentProfile, role, message string, source agentthread.SourceKind, forkMode, parentID, parentPath string, status agentthread.Status) (agentthread.Metadata, error) {
	if c == nil || c.threads == nil {
		return agentthread.Metadata{}, errors.New("thread registry is not configured")
	}
	c.registerRootThread()
	parentPath = strings.TrimSpace(parentPath)
	if parentPath == "" {
		parentPath = agentthread.RootPath
	}
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		parentID = c.sessionID
	}
	meta, err := c.threads.RegisterSpawn(agentthread.SpawnSpec{
		ID:              id,
		SessionID:       c.sessionID,
		ParentID:        parentID,
		ParentPath:      parentPath,
		TaskName:        taskName,
		AgentProfile:    strings.TrimSpace(agentProfile),
		Role:            role,
		LastTaskMessage: message,
		CWD:             c.parentRepo,
		SourceKind:      source,
		ForkMode:        strings.TrimSpace(forkMode),
		Status:          status,
		Now:             time.Now().UTC(),
	})
	if err != nil {
		return agentthread.Metadata{}, err
	}
	if err := c.threadStore.UpsertThread(meta); err != nil {
		return agentthread.Metadata{}, err
	}
	return meta, nil
}

func (c *AgentControl) recordHarnessTaskStart(meta agentthread.Metadata, role, intent, workerRoot string, isolation IsolationMode, baseRepo, goalID, goalDir string) {
	if c == nil || c.harnessStore == nil {
		return
	}
	now := time.Now().UTC()
	_, existed := c.harnessTask(meta.ID)
	workspaceMode := harness.WorkspaceShared
	if isolation == IsolationWorktree {
		workspaceMode = harness.WorkspaceWorktree
	}
	runID := harnessRunID(meta.ID)
	task := harness.Task{
		ID:         meta.ID,
		SessionID:  c.sessionID,
		ParentID:   meta.ParentID,
		ParentPath: meta.Source.ParentPath,
		Path:       meta.Path,
		Name:       meta.TaskName,
		Role:       role,
		Intent:     intent,
		GoalID:     strings.TrimSpace(goalID),
		GoalDir:    strings.TrimSpace(goalDir),
		Workspace: harness.WorkspaceLease{
			Mode:      workspaceMode,
			Root:      workerRoot,
			BaseRepo:  strings.TrimSpace(baseRepo),
			CreatedAt: now,
		},
		Status:     harness.TaskStatusRunning,
		LastRunID:  runID,
		CardItemID: taskCardItemID(meta.ID),
		CreatedAt:  meta.CreatedAt,
		UpdatedAt:  now,
		StartedAt:  now,
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	run := harness.AgentRun{
		ID:        runID,
		TaskID:    meta.ID,
		AgentID:   meta.ID,
		Role:      role,
		Status:    harness.TaskStatusRunning,
		StartedAt: now,
	}
	_ = c.harnessStore.UpsertTask(task)
	_ = c.harnessStore.UpsertRun(run)
	eventType := harness.EventTaskCreated
	if existed {
		eventType = harness.EventTaskStatusChanged
	}
	_ = c.harnessStore.AppendEvent(harness.Event{
		Type:      eventType,
		TaskID:    meta.ID,
		RunID:     runID,
		AgentID:   meta.ID,
		Path:      meta.Path,
		Status:    string(harness.TaskStatusRunning),
		CreatedAt: now,
	})
	_ = c.harnessStore.AppendEvent(harness.Event{
		Type:      harness.EventWorkspaceAssigned,
		TaskID:    meta.ID,
		RunID:     runID,
		AgentID:   meta.ID,
		Path:      workerRoot,
		Status:    string(workspaceMode),
		CreatedAt: now,
	})
	_ = c.harnessStore.AppendEvent(harness.Event{
		Type:      harness.EventRunStarted,
		TaskID:    meta.ID,
		RunID:     runID,
		AgentID:   meta.ID,
		Path:      meta.Path,
		Status:    string(harness.TaskStatusRunning),
		CreatedAt: now,
	})
}

func (c *AgentControl) recordHarnessTaskQueued(meta agentthread.Metadata, role, intent string, isolation IsolationMode, baseRepo, goalID, goalDir string) {
	if c == nil || c.harnessStore == nil {
		return
	}
	now := time.Now().UTC()
	workspaceMode := harness.WorkspaceShared
	if isolation == IsolationWorktree {
		workspaceMode = harness.WorkspaceWorktree
	}
	runID := harnessRunID(meta.ID)
	task := harness.Task{
		ID:         meta.ID,
		SessionID:  c.sessionID,
		ParentID:   meta.ParentID,
		ParentPath: meta.Source.ParentPath,
		Path:       meta.Path,
		Name:       meta.TaskName,
		Role:       role,
		Intent:     intent,
		GoalID:     strings.TrimSpace(goalID),
		GoalDir:    strings.TrimSpace(goalDir),
		Workspace: harness.WorkspaceLease{
			Mode:     workspaceMode,
			BaseRepo: strings.TrimSpace(baseRepo),
		},
		Status:     harness.TaskStatusQueued,
		LastRunID:  runID,
		CardItemID: taskCardItemID(meta.ID),
		CreatedAt:  meta.CreatedAt,
		UpdatedAt:  now,
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	_ = c.harnessStore.UpsertTask(task)
	_ = c.harnessStore.AppendEvent(harness.Event{
		Type:      harness.EventTaskCreated,
		TaskID:    meta.ID,
		RunID:     runID,
		AgentID:   meta.ID,
		Path:      meta.Path,
		Status:    string(harness.TaskStatusQueued),
		CreatedAt: now,
	})
	_ = c.harnessStore.AppendEvent(harness.Event{
		Type:      harness.EventTaskStatusChanged,
		TaskID:    meta.ID,
		RunID:     runID,
		AgentID:   meta.ID,
		Path:      meta.Path,
		Status:    string(harness.TaskStatusQueued),
		CreatedAt: now,
	})
}

func taskCardItemID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	return "task-card-" + taskID
}

func (c *AgentControl) enqueuePreparedSpawn(prepared preparedSpawn) error {
	if c != nil && c.harnessStore != nil && c.harnessStore.Dir() != "" {
		payload, err := json.Marshal(queuedSpawnPayloadFromPrepared(prepared))
		if err != nil {
			return fmt.Errorf("persist queued spawn: %w", err)
		}
		if err := c.harnessStore.UpsertQueueItem(harness.QueueItem{
			ID:      prepared.WorkerID,
			TaskID:  prepared.WorkerID,
			Kind:    "agent_spawn",
			Payload: payload,
		}); err != nil {
			return fmt.Errorf("persist queued spawn: %w", err)
		}
	}
	c.queueMu.Lock()
	defer c.queueMu.Unlock()
	c.queued = append(c.queued, prepared)
	return nil
}

func (c *AgentControl) maybeStartQueued(ctx context.Context) {
	if c == nil {
		return
	}
	for {
		if c.manager.CountRunning() >= c.maxParallel {
			return
		}
		prepared, ok := c.popQueuedSpawn()
		if !ok {
			return
		}
		if err := c.startQueuedSpawn(ctx, prepared); err != nil {
			c.recordHarnessTaskFailure(prepared.WorkerID, err)
			c.deleteQueuedSpawn(prepared.WorkerID)
			if failed, ok := c.threads.UpdateStatus(prepared.WorkerID, agentthread.StatusFailed, time.Now().UTC()); ok {
				_ = c.threadStore.RecordStatus(failed)
			}
			if closed, ok := c.threads.UpdateEdgeStatus(prepared.WorkerID, agentthread.EdgeClosed, time.Now().UTC()); ok {
				_ = c.threadStore.RecordEdgeStatus(closed)
			}
			continue
		}
		c.deleteQueuedSpawn(prepared.WorkerID)
	}
}

func (c *AgentControl) popQueuedSpawn() (preparedSpawn, bool) {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()
	if len(c.queued) == 0 {
		return preparedSpawn{}, false
	}
	prepared := c.queued[0]
	c.queued[0] = preparedSpawn{}
	c.queued = c.queued[1:]
	return prepared, true
}

func (c *AgentControl) deleteQueuedSpawn(workerID string) {
	if c == nil || c.harnessStore == nil || c.harnessStore.Dir() == "" {
		return
	}
	_ = c.harnessStore.DeleteQueueItem(workerID)
}

func queuedSpawnPayloadFromPrepared(prepared preparedSpawn) queuedSpawnPayload {
	return queuedSpawnPayload{
		WorkerID:         prepared.WorkerID,
		ParticipantID:    prepared.ParticipantID,
		WorkerType:       prepared.WorkerType.Name,
		ThreadMeta:       prepared.ThreadMeta,
		Description:      prepared.Description,
		Prompt:           prepared.Prompt,
		GoalID:           prepared.GoalID,
		GoalDir:          prepared.GoalDir,
		Isolation:        string(prepared.Isolation),
		SpeechCapability: prepared.SpeechCapability,
		BaseRepo:         prepared.BaseRepo,
		IsFork:           prepared.IsFork,
		ForkMode:         prepared.ForkMode,
		ParentHistory:    providers.CloneChatMessages(prepared.ParentHistory),
		ModelOverride:    prepared.ModelOverride,
		ModelPin:         prepared.ModelPin,
	}
}

func preparedSpawnFromQueuedPayload(payload queuedSpawnPayload) (preparedSpawn, error) {
	wt, err := LookupWorkerType(payload.WorkerType)
	if err != nil {
		return preparedSpawn{}, err
	}
	isolation, err := NormalizeIsolation(payload.Isolation, wt)
	if err != nil {
		return preparedSpawn{}, err
	}
	workerID := strings.TrimSpace(payload.WorkerID)
	if workerID == "" {
		workerID = payload.ThreadMeta.ID
	}
	if workerID == "" {
		return preparedSpawn{}, errors.New("queued spawn worker_id is required")
	}
	if payload.ThreadMeta.ID == "" {
		payload.ThreadMeta.ID = workerID
	}
	return preparedSpawn{
		WorkerID:         workerID,
		ParticipantID:    payload.ParticipantID,
		WorkerType:       wt,
		ThreadMeta:       payload.ThreadMeta,
		Description:      payload.Description,
		Prompt:           payload.Prompt,
		GoalID:           payload.GoalID,
		GoalDir:          payload.GoalDir,
		Isolation:        isolation,
		SpeechCapability: payload.SpeechCapability,
		BaseRepo:         payload.BaseRepo,
		IsFork:           payload.IsFork,
		ForkMode:         payload.ForkMode,
		ParentHistory:    providers.CloneChatMessages(payload.ParentHistory),
		ModelOverride:    payload.ModelOverride,
		ModelPin:         payload.ModelPin,
	}, nil
}

func (c *AgentControl) restoreQueuedSpawns() error {
	if c == nil || c.harnessStore == nil || c.harnessStore.Dir() == "" {
		return nil
	}
	items, err := c.harnessStore.ListQueueItems()
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Kind != "agent_spawn" {
			continue
		}
		var payload queuedSpawnPayload
		if err := json.Unmarshal(item.Payload, &payload); err != nil {
			_ = c.harnessStore.DeleteQueueItem(item.ID)
			continue
		}
		prepared, err := preparedSpawnFromQueuedPayload(payload)
		if err != nil {
			_ = c.harnessStore.DeleteQueueItem(item.ID)
			continue
		}
		if err := c.threads.Restore(prepared.ThreadMeta); err != nil {
			return err
		}
		c.queueMu.Lock()
		c.queued = append(c.queued, prepared)
		c.queueMu.Unlock()
	}
	return nil
}

func (c *AgentControl) startQueuedSpawn(ctx context.Context, prepared preparedSpawn) error {
	workerRoot := c.parentRepo
	var worktreeRef *worktree.Worktree
	var err error
	if prepared.Isolation == IsolationWorktree {
		if c.worktrees == nil {
			return errors.New("isolation=worktree requires repository worktree support (this workspace does not support isolated worktrees)")
		}
		worktreeRef, err = c.worktrees.Create(c.sessionID, prepared.WorkerID, prepared.BaseRepo)
		if err != nil {
			return fmt.Errorf("worktree create: %w", err)
		}
		workerRoot = worktreeRef.Path
	}
	if running, ok := c.threads.UpdateStatus(prepared.WorkerID, agentthread.StatusRunning, time.Now().UTC()); ok {
		_ = c.threadStore.RecordStatus(running)
	}
	c.recordHarnessTaskStart(prepared.ThreadMeta, prepared.WorkerType.Name, prepared.Prompt, workerRoot, prepared.Isolation, prepared.BaseRepo, prepared.GoalID, prepared.GoalDir)
	if prepared.SpeechCapability {
		c.EnableParticipantSpeech(prepared.WorkerID)
	}
	workerKit, err := c.workerFact(workerRoot, prepared.WorkerType, prepared.ThreadMeta)
	if err != nil {
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		return fmt.Errorf("worker toolkit: %w", err)
	}
	prompt := prepared.Prompt
	systemPrompt, err := c.workerSystemPrompt(workerRoot, prepared.WorkerType, prepared.ThreadMeta, prepared.Isolation)
	if err != nil {
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		return fmt.Errorf("worker system prompt: %w", err)
	}
	var initialHistory []providers.ChatMessage
	if prepared.IsFork {
		initialHistory = providers.CloneChatMessages(prepared.ParentHistory)
		initialHistory = withInitialSystemPrompt(initialHistory, systemPrompt)
		systemPrompt = ""
		if prepared.Isolation == IsolationWorktree {
			prompt = appendForkWorktreeReminder(prompt, workerRoot, prepared.Isolation)
		}
	}
	historyPath := ""
	if c.historyDir != "" {
		historyPath = filepath.Join(c.historyDir, prepared.WorkerID+".json")
	}
	participantID := prepared.ParticipantID
	if strings.TrimSpace(participantID) == "" {
		// Legacy queued payloads (persisted before participant identity
		// existed) get a fresh participant at start time.
		participantID = c.newEphemeralParticipant(prepared.ThreadMeta.TaskName, prepared.WorkerType).ID
	}
	// Per-participant model pin restore. Queued spawns persist the raw
	// pin (e.g. "alt-provider:model") but not the stream client. Before
	// the runner can pick a client we MUST rebuild it for any pin whose
	// provider differs from the current worker default; otherwise the
	// subagent.Manager would silently fall back to defaults.client and
	// route the request to the wrong provider.
	//
	// Behavior matrix (raw pin → resolver action):
	//   ""                                 → no-op, keep ModelOverride
	//   "model"                            → bare model, keep ModelOverride
	//   "p:model" p==workerProviderName    → keep ModelOverride
	//   "p:model" p!=workerProviderName    → resolver must yield a fresh
	//                                       client; nil client or any
	//                                       error fails this run
	//                                       explicitly (never silent).
	modelOverride := prepared.ModelOverride
	clientOverride := prepared.ClientOverride
	if rawPin := strings.TrimSpace(prepared.ModelPin); rawPin != "" {
		resolver := c.currentModelPinResolver()
		if resolver == nil {
			return fmt.Errorf("queued spawn %s has model pin %q but no model-pin resolver is installed; refusing to fall back to the worker default client", prepared.WorkerID, rawPin)
		}
		resolvedModel, resolvedClient, resolveErr := resolver(rawPin)
		if resolveErr != nil {
			return fmt.Errorf("queued spawn %s model pin %q could not be resolved: %w", prepared.WorkerID, rawPin, resolveErr)
		}
		if strings.TrimSpace(resolvedModel) == "" {
			return fmt.Errorf("queued spawn %s model pin %q resolved to an empty model", prepared.WorkerID, rawPin)
		}
		if pinTargetsDifferentProvider(rawPin, c.WorkerProviderName()) {
			if resolvedClient == nil {
				return fmt.Errorf("queued spawn %s model pin %q targets a different provider but resolver returned no client; refusing to use the worker default client", prepared.WorkerID, rawPin)
			}
			clientOverride = resolvedClient
		}
		modelOverride = resolvedModel
	}
	_, err = c.manager.Spawn(context.WithoutCancel(ctx), subagent.SpawnOptions{
		ID:             prepared.WorkerID,
		ParticipantID:  participantID,
		Type:           prepared.WorkerType.Name,
		TaskName:       prepared.ThreadMeta.TaskName,
		AgentProfile:   prepared.ThreadMeta.AgentProfile,
		AgentPath:      prepared.ThreadMeta.Path,
		ParentID:       prepared.ThreadMeta.ParentID,
		Description:    prepared.Description,
		Prompt:         prompt,
		SystemPrompt:   systemPrompt,
		Toolkit:        workerKit,
		HistoryPath:    historyPath,
		WorkerRoot:     workerRoot,
		InitialHistory: initialHistory,
		Model:          modelOverride,
		ModelPin:       prepared.ModelPin,
		Client:         clientOverride,
	})
	if err != nil {
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		return fmt.Errorf("spawn: %w", err)
	}
	return nil
}

func (c *AgentControl) recordHarnessTaskFailure(taskID string, err error) {
	if c == nil || c.harnessStore == nil {
		return
	}
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	now := time.Now().UTC()
	runID := harnessRunID(taskID)
	task, _ := c.harnessTask(taskID)
	_, _ = c.harnessStore.UpdateTaskStatus(taskID, harness.TaskStatusFailed, now, 0, 0, errText)
	_, _ = c.harnessStore.UpdateRunStatus(runID, harness.TaskStatusFailed, now, 0, 0, errText)
	_ = c.harnessStore.AppendEvent(harness.Event{
		Type:      harness.EventTaskStatusChanged,
		TaskID:    taskID,
		RunID:     runID,
		AgentID:   taskID,
		Status:    string(harness.TaskStatusFailed),
		Message:   errText,
		CreatedAt: now,
	})
	_ = c.harnessStore.AppendEvent(harness.Event{
		Type:      harness.EventRunCompleted,
		TaskID:    taskID,
		RunID:     runID,
		AgentID:   taskID,
		Status:    string(harness.TaskStatusFailed),
		Message:   errText,
		CreatedAt: now,
	})
	_ = c.recordAgentFailure(AgentFailure{
		Source:    "harness_task",
		TaskID:    taskID,
		RunID:     runID,
		AgentID:   taskID,
		GoalID:    task.GoalID,
		GoalDir:   task.GoalDir,
		Outcome:   string(harness.TaskStatusFailed),
		Message:   errText,
		CreatedAt: now,
	})
}

func (c *AgentControl) recordHarnessStatus(n subagent.Notification) {
	if c == nil || c.harnessStore == nil {
		return
	}
	status := harnessStatusFromSubAgent(n.Status)
	if n.Status == subagent.StatusCompleted {
		if report, ok, err := c.harnessStore.ReportForTask(n.AgentID); err == nil && ok {
			if reportStatus := harnessStatusFromReportOutcome(report.Outcome); reportStatus != "" {
				status = reportStatus
			}
		} else if err == nil && !ok {
			status = harness.TaskStatusAwaitingReport
		}
	}
	errText := ""
	if n.Snapshot.Error != nil {
		errText = n.Snapshot.Error.Error()
	}
	if task, ok := c.harnessTask(n.AgentID); ok {
		if isActiveHarnessStatus(status) && (task.Status == harness.TaskStatusAwaitingReport || isTerminalHarnessStatus(task.Status)) {
			return
		}
		if isTerminalHarnessStatus(status) && task.Status == status && task.InputTokens == n.Snapshot.InputTokens && task.OutputTokens == n.Snapshot.OutputTokens && strings.TrimSpace(task.Error) == strings.TrimSpace(errText) {
			return
		}
	}
	completedAt := n.Snapshot.CompletedAt
	if isFinalSubAgentStatus(n.Status) && completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	runID := harnessRunID(n.AgentID)
	if _, err := c.harnessStore.UpdateTaskStatus(n.AgentID, status, completedAt, n.Snapshot.InputTokens, n.Snapshot.OutputTokens, errText); err == nil {
		_ = c.harnessStore.AppendEvent(harness.Event{
			Type:      harness.EventTaskStatusChanged,
			TaskID:    n.AgentID,
			RunID:     runID,
			AgentID:   n.AgentID,
			Path:      n.Snapshot.AgentPath,
			Status:    string(status),
			Message:   errText,
			CreatedAt: time.Now().UTC(),
		})
	}
	if _, err := c.harnessStore.UpdateRunStatus(runID, status, completedAt, n.Snapshot.InputTokens, n.Snapshot.OutputTokens, errText); err == nil && isFinalSubAgentStatus(n.Status) {
		_ = c.harnessStore.AppendEvent(harness.Event{
			Type:      harness.EventRunCompleted,
			TaskID:    n.AgentID,
			RunID:     runID,
			AgentID:   n.AgentID,
			Path:      n.Snapshot.AgentPath,
			Status:    string(status),
			Message:   errText,
			CreatedAt: time.Now().UTC(),
		})
	}
	if isFinalSubAgentStatus(n.Status) {
		c.recordAgentResultArtifact(n.Snapshot)
		c.recordWorktreeArtifacts(n.Snapshot)
	}
}

func (c *AgentControl) harnessReportForTask(taskID string) (string, []string) {
	if c == nil || c.harnessStore == nil {
		return "", nil
	}
	var taskArtifactPaths []string
	tasks, err := c.harnessStore.ListTasks()
	if err == nil {
		for _, task := range tasks {
			if task.ID == taskID {
				taskArtifactPaths = append(taskArtifactPaths, task.ArtifactPaths...)
				break
			}
		}
	}
	report, ok, err := c.harnessStore.ReportForTask(taskID)
	if err == nil && ok {
		paths := append([]string(nil), report.Artifacts...)
		if report.ReportPath != "" && !stringSliceContains(paths, report.ReportPath) {
			paths = append(paths, report.ReportPath)
		}
		for _, path := range taskArtifactPaths {
			if path != "" && !stringSliceContains(paths, path) {
				paths = append(paths, path)
			}
		}
		return report.ReportPath, paths
	}
	return "", taskArtifactPaths
}

func (c *AgentControl) recordWorktreeArtifacts(snap subagent.SubAgentSnapshot) {
	if c == nil || c.harnessStore == nil {
		return
	}
	task, ok := c.harnessTask(snap.ID)
	if !ok || task.Workspace.Mode != harness.WorkspaceWorktree || strings.TrimSpace(task.Workspace.Root) == "" {
		return
	}
	root := task.Workspace.Root
	statusOut, err := gitOutput(root, "status", "--porcelain")
	if err != nil || strings.TrimSpace(statusOut) == "" {
		return
	}
	artifactDir := filepath.Join(c.harnessDir, "artifacts", snap.ID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return
	}
	statusPath := filepath.Join(artifactDir, "worktree-status.txt")
	if err := os.WriteFile(statusPath, []byte(statusOut), 0o644); err == nil {
		_ = c.harnessStore.AddArtifact(harness.Artifact{
			ID:        snap.ID + "-worktree-status",
			TaskID:    snap.ID,
			RunID:     harnessRunID(snap.ID),
			Kind:      harness.ArtifactEvidence,
			Path:      statusPath,
			Summary:   "worktree status",
			CreatedAt: time.Now().UTC(),
		})
	}
	patchOut, err := gitOutput(root, "diff", "--binary", "HEAD", "--")
	if err == nil && strings.TrimSpace(patchOut) != "" {
		patchPath := filepath.Join(artifactDir, "changes.patch")
		if err := os.WriteFile(patchPath, []byte(patchOut), 0o644); err == nil {
			_ = c.harnessStore.AddArtifact(harness.Artifact{
				ID:        snap.ID + "-patch",
				TaskID:    snap.ID,
				RunID:     harnessRunID(snap.ID),
				Kind:      harness.ArtifactPatch,
				Path:      patchPath,
				Summary:   "worktree diff against base HEAD",
				CreatedAt: time.Now().UTC(),
			})
		}
	}
	untracked, err := gitUntrackedFiles(root)
	if err != nil || len(untracked) == 0 {
		return
	}
	manifestPath := filepath.Join(artifactDir, "untracked-files.txt")
	if err := os.WriteFile(manifestPath, []byte(strings.Join(untracked, "\n")+"\n"), 0o644); err == nil {
		_ = c.harnessStore.AddArtifact(harness.Artifact{
			ID:        snap.ID + "-untracked-manifest",
			TaskID:    snap.ID,
			RunID:     harnessRunID(snap.ID),
			Kind:      harness.ArtifactManifest,
			Path:      manifestPath,
			Summary:   "untracked files created by worktree task",
			CreatedAt: time.Now().UTC(),
		})
	}
	archivePath := filepath.Join(artifactDir, "untracked-files.tar")
	if err := writeUntrackedArchive(root, archivePath, untracked); err == nil {
		_ = c.harnessStore.AddArtifact(harness.Artifact{
			ID:        snap.ID + "-untracked-archive",
			TaskID:    snap.ID,
			RunID:     harnessRunID(snap.ID),
			Kind:      harness.ArtifactArchive,
			Path:      archivePath,
			Summary:   "archive of untracked files created by worktree task",
			CreatedAt: time.Now().UTC(),
		})
	}
}

func (c *AgentControl) harnessTask(taskID string) (harness.Task, bool) {
	if c == nil || c.harnessStore == nil {
		return harness.Task{}, false
	}
	tasks, err := c.harnessStore.ListTasks()
	if err != nil {
		return harness.Task{}, false
	}
	for _, task := range tasks {
		if task.ID == taskID {
			return task, true
		}
	}
	return harness.Task{}, false
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func gitUntrackedFiles(dir string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard", "-z")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	parts := bytes.Split(out, []byte{0})
	files := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(string(part))
		if name == "" {
			continue
		}
		if filepath.IsAbs(name) || name == "." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
			continue
		}
		files = append(files, filepath.ToSlash(name))
	}
	return files, nil
}

func writeUntrackedArchive(root, archivePath string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		return err
	}
	out, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer out.Close()
	tw := tar.NewWriter(out)
	defer tw.Close()
	for _, rel := range files {
		cleanRel := filepath.Clean(rel)
		if cleanRel == "." || filepath.IsAbs(cleanRel) || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
			continue
		}
		absPath := filepath.Join(root, cleanRel)
		info, err := os.Lstat(absPath)
		if err != nil || info.IsDir() {
			continue
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, _ = os.Readlink(absPath)
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(cleanRel)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		in, err := os.Open(absPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, in); err != nil {
			in.Close()
			return err
		}
		if err := in.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (c *AgentControl) consumeWorkerStatus(ch <-chan subagent.Notification) {
	for {
		select {
		case n := <-ch:
			c.consumeWorkerNotification(n)
		case <-c.statusStop:
			return
		}
	}
}

func (c *AgentControl) consumeWorkerNotification(n subagent.Notification) {
	if c == nil || c.threads == nil {
		return
	}
	status := threadStatusFromSubAgent(n.Status)
	if current, ok := c.threads.Resolve(n.AgentID); ok {
		if isActiveAgentThreadStatus(status) && isFinalAgentThreadStatus(current.Status) {
			return
		}
		if isFinalAgentThreadStatus(status) && current.Status == status {
			return
		}
	}
	c.recordHarnessStatus(n)
	meta, ok := c.threads.UpdateStatus(n.AgentID, status, time.Now().UTC())
	if !ok {
		return
	}
	_ = c.threadStore.RecordStatus(meta)
	if isFinalSubAgentStatus(n.Status) {
		c.ensureAgentResultDelivery(n.Snapshot)
		if !c.deliverNestedResultToParent(context.Background(), n.Snapshot) && c.isRootChildSnapshot(n.Snapshot) {
			_ = c.threadStore.RecordCommunication(c.rootThreadID, c.newAgentCompletionCommunication(n.Snapshot, agentthread.RootPath))
		}
		go c.maybeStartQueued(context.Background())
	}
}

func (c *AgentControl) deliverNestedResultToParent(ctx context.Context, snap subagent.SubAgentSnapshot) bool {
	if c == nil || c.manager == nil {
		return false
	}
	parentID := strings.TrimSpace(snap.ParentID)
	if parentID == "" || parentID == c.sessionID || parentID == c.rootThreadID {
		return false
	}
	if c.manager.Get(parentID) == nil {
		return false
	}
	resultID, claimed, _ := c.claimAgentResultDelivery(snap, agentResultConsumerNestedFollowup)
	if !claimed {
		return true
	}
	parentPath := parentPathForSnapshot(snap)
	if meta, ok := c.threads.Resolve(parentID); ok && strings.TrimSpace(meta.Path) != "" {
		parentPath = meta.Path
	}
	communication := c.newAgentCompletionCommunication(snap, parentPath)
	_, err := c.manager.Followup(ctx, parentID, communication.String())
	if err != nil {
		c.ReleaseAgentResultDeliveryClaim(resultID, agentResultConsumerNestedFollowup)
	}
	if err == nil {
		_ = c.threadStore.RecordCommunication(parentID, communication)
	}
	return err == nil
}

func (c *AgentControl) isRootChildSnapshot(snap subagent.SubAgentSnapshot) bool {
	parentID := strings.TrimSpace(snap.ParentID)
	return parentID == "" || parentID == c.sessionID || parentID == c.rootThreadID
}

func formatAgentCompletionCommunication(snap subagent.SubAgentSnapshot, recipientPath string) string {
	return newAgentCompletionCommunication(snap, recipientPath).String()
}

func newAgentCompletionCommunication(snap subagent.SubAgentSnapshot, recipientPath string) agentthread.InterAgentCommunication {
	return newAgentCompletionCommunicationWithMessage(snap, recipientPath, NewAgentMailboxMessage(snap))
}

func (c *AgentControl) newAgentCompletionCommunication(snap subagent.SubAgentSnapshot, recipientPath string) agentthread.InterAgentCommunication {
	reportPath, artifacts := c.harnessReportForTask(snap.ID)
	return newAgentCompletionCommunicationWithMessage(snap, recipientPath, c.agentMailboxMessageWithRefs(snap, reportPath, artifacts))
}

// AgentCompletionChatMessage returns the user-role handoff that should resume
// the recipient agent after a child agent finishes.
func (c *AgentControl) AgentCompletionChatMessage(snap subagent.SubAgentSnapshot, recipientPath string) providers.ChatMessage {
	c.ensureAgentResultDelivery(snap)
	reportPath, artifacts := c.harnessReportForTask(snap.ID)
	communication := newAgentCompletionCommunicationWithMessageAndTrigger(
		snap,
		recipientPath,
		c.agentMailboxMessageWithRefs(snap, reportPath, artifacts),
		true,
	)
	return providers.ChatMessage{
		Role:    "user",
		Name:    wuucontext.AgentNotificationMessageName,
		Content: communication.String(),
	}
}

func (c *AgentControl) AgentMailboxMessage(snap subagent.SubAgentSnapshot) AgentMailboxMessage {
	reportPath, artifacts := c.harnessReportForTask(snap.ID)
	return c.agentMailboxMessageWithRefs(snap, reportPath, artifacts)
}

func (c *AgentControl) agentMailboxMessageWithRefs(snap subagent.SubAgentSnapshot, reportPath string, artifacts []string) AgentMailboxMessage {
	ref := c.AgentResultReference(snap)
	return NewAgentMailboxMessageWithReportAndResult(
		snap,
		c.sessionArtifactRef(reportPath),
		c.sessionArtifactRefs(artifacts),
		ref,
	)
}

func newAgentCompletionCommunicationWithMessage(snap subagent.SubAgentSnapshot, recipientPath string, message AgentMailboxMessage) agentthread.InterAgentCommunication {
	return newAgentCompletionCommunicationWithMessageAndTrigger(snap, recipientPath, message, false)
}

func newAgentCompletionCommunicationWithMessageAndTrigger(snap subagent.SubAgentSnapshot, recipientPath string, message AgentMailboxMessage, triggerTurn bool) agentthread.InterAgentCommunication {
	if strings.TrimSpace(recipientPath) == "" {
		recipientPath = agentthread.RootPath
	}
	content := agentthread.SubagentNotificationContent(snap.AgentPath, message)
	return agentthread.NewInterAgentCommunication(parseAgentPathOrRoot(snap.AgentPath), parseAgentPathOrRoot(recipientPath), content, triggerTurn)
}

func formatInterAgentCommunication(authorPath, recipientPath, content string, triggerTurn bool) string {
	return newInterAgentCommunication(authorPath, recipientPath, content, triggerTurn).String()
}

func newInterAgentCommunication(authorPath, recipientPath, content string, triggerTurn bool) agentthread.InterAgentCommunication {
	return agentthread.NewInterAgentCommunication(
		parseAgentPathOrRoot(authorPath),
		parseAgentPathOrRoot(recipientPath),
		content,
		triggerTurn,
	)
}

func parseAgentPathOrRoot(path string) agentthread.AgentPath {
	parsed, err := agentthread.ParseAgentPath(path)
	if err != nil {
		return agentthread.RootAgentPath()
	}
	return parsed
}

func parentPathForSnapshot(snap subagent.SubAgentSnapshot) string {
	path := strings.TrimSpace(snap.AgentPath)
	if path == "" || path == agentthread.RootPath {
		return agentthread.RootPath
	}
	if idx := strings.LastIndex(path, "/"); idx > len("/root") {
		return path[:idx]
	}
	return agentthread.RootPath
}

func isFinalSubAgentStatus(status subagent.Status) bool {
	switch status {
	case subagent.StatusCompleted, subagent.StatusFailed, subagent.StatusCancelled:
		return true
	default:
		return false
	}
}

func isActiveAgentThreadStatus(status agentthread.Status) bool {
	switch status {
	case agentthread.StatusPending, agentthread.StatusRunning:
		return true
	default:
		return false
	}
}

func isFinalAgentThreadStatus(status agentthread.Status) bool {
	switch status {
	case agentthread.StatusCompleted, agentthread.StatusFailed, agentthread.StatusCancelled:
		return true
	default:
		return false
	}
}

func (c *AgentControl) resolveAgentID(target string) string {
	return c.resolveAgentIDFrom(agentthread.RootPath, target)
}

func (c *AgentControl) resolveAgentIDFrom(currentPath, target string) string {
	id := strings.TrimSpace(target)
	if id == "" {
		return ""
	}
	if c.manager.Get(id) != nil {
		return id
	}
	if c.threads != nil {
		if meta, ok := c.threads.ResolveFrom(currentPath, id); ok {
			return meta.ID
		}
	}
	return id
}

func threadStatusFromSubAgent(status subagent.Status) agentthread.Status {
	switch status {
	case subagent.StatusPending:
		return agentthread.StatusPending
	case subagent.StatusQueued:
		return agentthread.StatusPending
	case subagent.StatusRunning:
		return agentthread.StatusRunning
	case subagent.StatusCompleted:
		return agentthread.StatusCompleted
	case subagent.StatusFailed:
		return agentthread.StatusFailed
	case subagent.StatusCancelled:
		return agentthread.StatusCancelled
	default:
		return agentthread.Status(status)
	}
}

func harnessStatusFromSubAgent(status subagent.Status) harness.TaskStatus {
	switch status {
	case subagent.StatusPending:
		return harness.TaskStatusPending
	case subagent.StatusQueued:
		return harness.TaskStatusQueued
	case subagent.StatusRunning:
		return harness.TaskStatusRunning
	case subagent.StatusCompleted:
		return harness.TaskStatusCompleted
	case subagent.StatusFailed:
		return harness.TaskStatusFailed
	case subagent.StatusCancelled:
		return harness.TaskStatusCancelled
	default:
		return harness.TaskStatus(status)
	}
}

func harnessRunID(taskID string) string {
	return strings.TrimSpace(taskID) + "-run-1"
}

func stringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

// SystemPromptPreamble returns the instructions prepended to the
// main agent's system prompt. It teaches, in order:
//
//   - When the user's intent is unclear or the request depends on
//     information only they have, ask a clarifying question in your
//     reply before acting. Do not guess.
//   - Delegation rules (fresh subagents, fork spawns, communication planes,
//     honesty rules, failure handling) — but only AFTER alignment.
//
// There is NO separate "coordinator role" persona here. The main
// agent remains the user's coding agent; sub-agents are optional task
// tools for cases where delegation is worth the overhead.
func SystemPromptPreamble() string {
	return `You are wuu's main coding agent with access to an optional Agent tool. The main agent owns the user conversation, the final synthesis, and the decision about whether delegation is worth the overhead.

## Clarifying the request

When the user's intent is unclear, the task depends on requirements or tradeoffs only they can answer, or you would otherwise have to guess at something material, ask a clarifying question in your assistant reply before acting. Do not invent answers the user has not given you, and do not invoke a tool to surface the question — write it as plain text and let the user respond.

## Agent Tool

- spawn_agent — launch a child agent. Pass description and prompt. Specify subagent_type for a fresh specialized agent, or omit subagent_type to fork yourself with full conversation context.
- send_message — queue a message for an existing background agent without triggering a new turn.
- followup_task — send a follow-up task message and trigger the target background agent's next turn.
- await_agents — explicitly join specific child agents, or all active descendant agents, and return structured per-agent results.
- close_agent — stop a running agent that is stuck or off-track.
- list_agents — see active agents and their status.

## Available Subagents

- general-purpose: broad code research, search, implementation, and multi-step tasks. Use this when you want a fresh agent with no inherited conversation context.
- verification: independent post-change verifier. Use after meaningful implementation work; it runs in the background and returns PASS, FAIL, or PARTIAL with evidence.
- planner: lead/planning role for phase plans, verification gates, and escalation points.
- researcher: read-only codebase research and constraint discovery.
- worker: scoped implementation role; defaults to worktree isolation for edits.
- reviewer: independent diff review for bugs, regressions, and missing verification.
- qa: product-facing verifier for tests, smoke checks, and UI/browser evidence.
- debugger: failure triage and root-cause analysis from logs and failing commands.
- integrator: final synthesis across worker, reviewer, and verifier evidence.
- fork-self: omit subagent_type to fork yourself. Use when the child needs the current conversation context and you do not want intermediate tool output in your own context.

Agents execute tasks autonomously and return a structured handoff. The agent result is input for your own synthesis; do not forward it blindly.

## When to Use Agents

Do not spawn agents for trivial tasks you can handle yourself — reading a specific file, running a quick grep, or reporting a command output. Keep work local when the task is tightly coupled, small, or on the critical path. Spawn agents only when delegation materially improves the work: multi-file refactors, independent research across different areas, verification that benefits from a separate context, or work that can run in parallel.

Do not delegate work that blocks your immediate next step. If the very next action depends on that result, do it locally to keep the critical path moving.

Do not delegate understanding. Never hand off vague prompts like "based on your findings, fix the bug" or "based on the research, implement it." Read the findings yourself, decide what should happen, then give the agent a concrete brief.

## Concurrency

Launch independent agents in parallel whenever possible. Read-only or verification tasks can run freely in parallel. Write-heavy tasks should run one at a time per file set to avoid conflicts. When you split code-edit work, assign each agent clear files or modules and avoid overlapping ownership.

Fresh subagents run in the foreground by default so you can use their result immediately. Foreground child execution has no model-selected wait duration; it continues until the child finishes or the user/runtime cancels the turn. Set run_in_background=true only when you have genuinely independent or long-running work to do in parallel. Forks and verification agents run in the background. After spawning background agents, keep doing meaningful non-overlapping work when it exists. If there is no useful local work left, end your turn and let background completion notifications automatically resume you. Do not sleep, poll, or loop checking status.

Use await_agents when synthesis or integration depends on child outputs. Prefer explicit targets. Omit targets only when you intentionally want to join all active descendant tasks. If await_agents returns awaiting_report, the worker finished without a durable handoff; follow up or verify before relying on the result.

## Working with Agent Results

Background completion notifications are internal agent handoffs, not new user requests. They may be encoded as structured inter-agent notifications with author, recipient, content, and trigger_turn fields. Treat content as the handoff payload, then synthesize and verify it yourself. When a background agent finishes, its result automatically arrives as a notification in your next turn.

Before launching follow-up work, read the returned content yourself and do your own synthesis. Agent output is not a substitute for your judgment.

## Writing Agent Prompts

Fresh subagent prompts must be self-contained. Include the task, background, role, identity or memory status, scope, non-goals, starting points, acceptance criteria, deliverables, reporting expectations, and constraints. For code-edit subtasks, explicitly name owned files or modules and nearby files or modules that are out of scope; split work so each agent has a disjoint write set.

Fork prompts can be shorter because the child inherits your context, but they still need a specific directive and scope. Do not re-explain all background in a fork; state what to do, what is out of scope, and what to report.

## Handling Worker Failures

When a worker reports failure, continue the same worker with followup_task — it has the full error context. If correction still fails, try a different approach or report to the user.

If a worker seems stuck, close it with close_agent and respawn with clearer instructions.
`
}

// CleanupSession removes all worktrees belonging to this session.
func (c *AgentControl) CleanupSession() error {
	if c.worktrees == nil {
		return nil // non-git workspace, no worktrees to clean
	}
	return c.worktrees.CleanupSession(c.sessionID)
}

func appendForkWorktreeReminder(prompt, workerRoot string, isolation IsolationMode) string {
	var b strings.Builder
	b.WriteString(prompt)
	b.WriteString("\n\n<system-reminder>\n")
	fmt.Fprintf(&b, "Your active working directory for this child task is: %s\n", workerRoot)
	fmt.Fprintf(&b, "Isolation mode: %s\n", isolation)
	b.WriteString("This overrides any inherited working-directory assumptions from the parent history.\n")
	b.WriteString("</system-reminder>")
	return b.String()
}

// composeWorkerSystemPrompt builds the system prompt for a worker.
// It prepends the worker type's role-specific prompt + a description
// of the working directory and isolation mode, then appends the base
// prompt (typically the main agent's project memory and skills, not
// the optional coordinator-mode instructions).
func composeWorkerSystemPrompt(base string, wt WorkerType, workerRoot string, isolation IsolationMode) string {
	var b strings.Builder
	b.WriteString(wt.SystemPrompt)
	b.WriteString("\n\n")
	if wt.Role != "" || wt.ContextScope != "" || wt.OutputSchema != "" || len(wt.SuccessCriteria) > 0 {
		b.WriteString("## Role Contract\n")
		if wt.Role != "" {
			fmt.Fprintf(&b, "Role: %s\n", wt.Role)
		}
		if wt.ContextScope != "" {
			fmt.Fprintf(&b, "Context scope: %s\n", wt.ContextScope)
		}
		if wt.OutputSchema != "" {
			fmt.Fprintf(&b, "Output schema: %s\n", wt.OutputSchema)
		}
		if len(wt.SuccessCriteria) > 0 {
			b.WriteString("Success criteria:\n")
			for _, item := range wt.SuccessCriteria {
				fmt.Fprintf(&b, "- %s\n", item)
			}
		}
		b.WriteString("\n")
	}
	switch isolation {
	case IsolationWorktree:
		fmt.Fprintf(&b, "Your working directory is %s — an isolated worktree for this worker. ", workerRoot)
		b.WriteString("Edits you make stay sandboxed; the orchestrator will inspect the worktree after you finish. ")
	default: // inplace
		fmt.Fprintf(&b, "Your working directory is %s — the SHARED parent repository. ", workerRoot)
		b.WriteString("You are running inplace (no worktree isolation), so be especially careful: ")
		b.WriteString("read-only operations are safe, but any file you modify is visible to the orchestrator and other workers immediately. ")
	}
	b.WriteString("All file paths in your tools resolve relative to this directory. ")
	b.WriteString("You cannot spawn or manage other agents from this worker. If the task seems to require additional delegation, report that need in your final handoff so the parent can decide and coordinate.\n")
	if base != "" {
		b.WriteString("\n---\n\n")
		b.WriteString(base)
		b.WriteString("\n\n---\n\n")
		b.WriteString("Worker override: if any inherited text above describes the MAIN interactive agent as read-only, or says file changes / command execution must be delegated, ignore that text. It applies to the parent, not to you. If a tool is in your tool list, you may use it unless your task prompt explicitly forbids it.")
	}
	return b.String()
}

// newAgentControlWorkerID generates a worker ID. Mirrors subagent's
// scheme but is generated by AgentControl since worktree creation
// happens before subagent.Manager.Spawn.
func newAgentControlWorkerID(typ string) string {
	if typ == "" {
		typ = "agent"
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s", typ, hex.EncodeToString(b))
}
