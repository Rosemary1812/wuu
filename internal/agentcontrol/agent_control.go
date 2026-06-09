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
	"github.com/blueberrycongee/wuu/internal/harness"
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
	rootThreadID  string
	rootThreadDir string
	workerFact    WorkerToolkitFactory
	workerPrompt  WorkerSystemPromptFactory
	defaultSys    string // base system prompt prefix added to every worker
	maxParallel   int
	queueMu       sync.Mutex
	queued        []preparedSpawn
	statusCh      chan subagent.Notification
	statusStop    chan struct{}
	statusDone    chan struct{}
	closeOnce     sync.Once
}

// Config holds the dependencies needed to build an AgentControl.
type Config struct {
	// Client is the streaming LLM client every worker spawned by this
	// agent control runtime will share. It must be a StreamClient (not just a
	// Client) so workers run through the same streaming transport as
	// the interactive main agent.
	Client          providers.StreamClient
	DefaultModel    string
	ParentRepo      string // absolute path to the user's workspace
	WorktreeRoot    string // workspace-state worktrees directory (only used when workspace is a git repo)
	HistoryDir      string // session artifact workers directory
	ThreadDir       string // session artifact threads directory
	HarnessDir      string // session artifact harness directory
	SessionID       string
	WorkerSysPrompt string
	WorkerFactory   WorkerToolkitFactory
	WorkerPrompt    WorkerSystemPromptFactory
	MaxParallel     int
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

	mgr := subagent.NewManager(cfg.Client, cfg.DefaultModel)
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
		workerFact:   cfg.WorkerFactory,
		workerPrompt: cfg.WorkerPrompt,
		defaultSys:   cfg.WorkerSysPrompt,
		maxParallel:  maxP,
	}
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

// SpawnRequest is the internal shape of a spawn_agent tool invocation
// after argument validation.
type SpawnRequest struct {
	Type         string
	TaskName     string
	AgentProfile string // optional durable memory profile to wake for this worker
	Description  string
	Prompt       string
	ParentID     string
	ParentPath   string
	BaseRepo     string // optional: chain off another worktree (worktree mode only)
	Synchronous  bool
	Timeout      time.Duration
	// Isolation overrides the worker type's DefaultIsolation when set.
	// Empty string means "use the type default". Use this from
	// spawn_agent to opt a normally-inplace worker into a worktree
	// (e.g. an explorer that needs to run a destructive script).
	Isolation string
}

// SpawnResult is what the spawn_agent tool returns to the model.
type SpawnResult struct {
	Action       string   `json:"action"`
	AgentID      string   `json:"agent_id"`
	TaskName     string   `json:"task_name,omitempty"`
	AgentProfile string   `json:"agent_profile,omitempty"`
	AgentPath    string   `json:"agent_path,omitempty"`
	Status       string   `json:"status"`
	Isolation    string   `json:"isolation"`               // "inplace" or "worktree"
	WorktreePath string   `json:"worktree_path,omitempty"` // empty for inplace spawns
	Result       string   `json:"result,omitempty"`
	Error        string   `json:"error,omitempty"`
	DurationMS   int64    `json:"duration_ms,omitempty"`
	NextSteps    []string `json:"next_steps,omitempty"`
}

type preparedSpawn struct {
	WorkerID      string
	WorkerType    WorkerType
	ThreadMeta    agentthread.Metadata
	Description   string
	Prompt        string
	Isolation     IsolationMode
	BaseRepo      string
	IsFork        bool
	ForkMode      string
	ParentHistory []providers.ChatMessage
}

type queuedSpawnPayload struct {
	WorkerID      string                  `json:"worker_id"`
	WorkerType    string                  `json:"worker_type"`
	ThreadMeta    agentthread.Metadata    `json:"thread_meta"`
	Description   string                  `json:"description,omitempty"`
	Prompt        string                  `json:"prompt"`
	Isolation     string                  `json:"isolation"`
	BaseRepo      string                  `json:"base_repo,omitempty"`
	IsFork        bool                    `json:"is_fork,omitempty"`
	ForkMode      string                  `json:"fork_mode,omitempty"`
	ParentHistory []providers.ChatMessage `json:"parent_history,omitempty"`
}

// Spawn launches a sub-agent. In synchronous mode it blocks until
// the sub-agent finishes; in async mode it returns immediately with
// status "running" and the agent_id the orchestrator can poll.
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
		prepared := preparedSpawn{
			WorkerID:    workerID,
			WorkerType:  wt,
			ThreadMeta:  threadMeta,
			Description: req.Description,
			Prompt:      req.Prompt,
			Isolation:   isolation,
			BaseRepo:    req.BaseRepo,
		}
		c.recordHarnessTaskQueued(threadMeta, wtype, req.Prompt, isolation, req.BaseRepo)
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
			return nil, errors.New("isolation=worktree requires a git repository (this workspace is not a git repo)")
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
	c.recordHarnessTaskStart(threadMeta, wtype, req.Prompt, workerRoot, isolation, req.BaseRepo)

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
		ID:           workerID,
		Type:         wtype,
		TaskName:     threadMeta.TaskName,
		AgentProfile: threadMeta.AgentProfile,
		AgentPath:    threadMeta.Path,
		ParentID:     threadMeta.ParentID,
		Description:  req.Description,
		Prompt:       req.Prompt,
		SystemPrompt: sys,
		Toolkit:      workerKit,
		HistoryPath:  historyPath,
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

	// Synchronous mode: wait for completion.
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	snap, err := c.manager.Wait(waitCtx, sa.ID)
	if err != nil {
		return nil, fmt.Errorf("wait: %w", err)
	}
	result.Status = string(snap.Status)
	result.Result = snap.Result
	if snap.Error != nil {
		result.Error = snap.Error.Error()
	}
	if !snap.CompletedAt.IsZero() && !snap.StartedAt.IsZero() {
		result.DurationMS = snap.CompletedAt.Sub(snap.StartedAt).Milliseconds()
	}
	result.NextSteps = spawnResultNextSteps(result.Status, true, result.Isolation, result.AgentPath)

	return result, nil
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
			"Continue non-overlapping local work when available; the worker will report through the mailbox when it finishes.",
			"Use await_agents with " + pathHint + " only when the next step depends on this worker's output." + worktreeHint,
		}
	case subagent.StatusCompleted:
		if synchronous {
			return []string{
				"Inspect the worker result and any agent_report artifacts before relying on the handoff.",
				"Use await_agents or workflow_control only if this result must be joined into a larger agent team or workflow record." + worktreeHint,
			}
		}
		return []string{
			"Inspect the worker's mailbox result and agent_report artifacts before relying on the handoff." + worktreeHint,
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

// ForkRequest is the internal shape of a spawn_agent invocation with
// fork_turns enabled after argument validation. It always uses the
// default worker type, but isolation is still caller-selectable: a
// forked history and a worktree are orthogonal concerns.
type ForkRequest struct {
	TaskName     string
	AgentProfile string // optional durable memory profile to wake for this worker
	Description  string
	ForkMode     string
	ParentID     string
	ParentPath   string
	BaseRepo     string // optional: chain off another worktree (worktree mode only)
	// Isolation overrides the default worker type's DefaultIsolation
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

	// Resolve the default worker type so the worker has the full
	// tool set.
	wt, err := LookupWorkerType("worker")
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
		prepared := preparedSpawn{
			WorkerID:      workerID,
			WorkerType:    wt,
			ThreadMeta:    threadMeta,
			Description:   req.Description,
			Prompt:        req.Prompt,
			Isolation:     isolation,
			BaseRepo:      req.BaseRepo,
			IsFork:        true,
			ForkMode:      req.ForkMode,
			ParentHistory: append([]providers.ChatMessage(nil), parentHistory...),
		}
		c.recordHarnessTaskQueued(threadMeta, wt.Name, req.Prompt, isolation, req.BaseRepo)
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
			return nil, errors.New("isolation=worktree requires a git repository (this workspace is not a git repo)")
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
	c.recordHarnessTaskStart(threadMeta, wt.Name, req.Prompt, workerRoot, isolation, req.BaseRepo)

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

	initialHistory := append([]providers.ChatMessage(nil), parentHistory...)
	if threadMeta.AgentProfile != "" {
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
	}

	// Note: for ordinary forks we deliberately do NOT set SystemPrompt — when
	// InitialHistory is non-nil, the subagent runner uses the inherited system
	// message. Profile-backed workers replace that inherited system message
	// above so their durable identity and memory rules are active.
	workerCtx := ctx
	if !req.Synchronous {
		workerCtx = context.WithoutCancel(ctx)
	}

	sa, err := c.manager.Spawn(workerCtx, subagent.SpawnOptions{
		ID:             workerID,
		Type:           wt.Name,
		TaskName:       threadMeta.TaskName,
		AgentProfile:   threadMeta.AgentProfile,
		AgentPath:      threadMeta.Path,
		ParentID:       threadMeta.ParentID,
		Description:    req.Description,
		Prompt:         forkPrompt,
		Toolkit:        workerKit,
		HistoryPath:    historyPath,
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

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	snap, err := c.manager.Wait(waitCtx, sa.ID)
	if err != nil {
		return nil, fmt.Errorf("wait: %w", err)
	}
	result.Status = string(snap.Status)
	result.Result = snap.Result
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
	switch snap.Status {
	case subagent.StatusCancelled:
		return fmt.Errorf("agent %q is %s and cannot receive messages", id, snap.Status)
	}
	communication := newInterAgentCommunication(currentPath, snap.AgentPath, msg, false)
	if ok := c.manager.QueueMessage(id, communication.String()); !ok {
		return fmt.Errorf("agent %q not found", id)
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

func (c *AgentControl) WaitForMailboxUpdateFrom(currentPath string, ctx context.Context) (bool, error) {
	if c == nil || c.manager == nil {
		return false, errors.New("agent control not configured")
	}
	currentID := c.agentIDForPath(currentPath)
	if currentID != "" && c.manager.PendingMessageCount(currentID) > 0 {
		return true, nil
	}
	ch := make(chan subagent.Notification, 16)
	c.manager.Subscribe(ch)
	defer c.manager.Unsubscribe(ch)
	if currentID != "" && c.manager.PendingMessageCount(currentID) > 0 {
		return true, nil
	}
	for {
		select {
		case n := <-ch:
			if c.isMailboxNotificationFor(currentID, n) {
				return true, nil
			}
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return false, nil
			}
			return false, ctx.Err()
		}
	}
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
	if currentID == "" {
		if !isFinalSubAgentStatus(n.Status) {
			return false
		}
		parentID := strings.TrimSpace(n.Snapshot.ParentID)
		return parentID == "" || parentID == c.sessionID || parentID == c.rootThreadID
	}
	if n.Snapshot.ID == currentID && c.manager.PendingMessageCount(currentID) > 0 {
		return true
	}
	if strings.TrimSpace(n.Snapshot.ParentID) == currentID && isFinalSubAgentStatus(n.Status) {
		return true
	}
	return false
}

// Subscribe forwards to the underlying manager so the UI can receive
// status notifications and publish mailbox messages.
func (c *AgentControl) Subscribe(ch chan<- subagent.Notification) {
	c.manager.Subscribe(ch)
}

func (c *AgentControl) SubscribeStream(ch chan<- subagent.StreamNotification) {
	c.manager.SubscribeStream(ch)
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
		return append([]providers.ChatMessage(nil), history...)
	}
	out := make([]providers.ChatMessage, 0, len(history)+1)
	out = append(out, providers.ChatMessage{Role: "system", Content: sys})
	start := 0
	for start < len(history) && strings.TrimSpace(history[start].Role) == "system" {
		start++
	}
	out = append(out, history[start:]...)
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

func (c *AgentControl) recordHarnessTaskStart(meta agentthread.Metadata, role, intent, workerRoot string, isolation IsolationMode, baseRepo string) {
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
		Workspace: harness.WorkspaceLease{
			Mode:      workspaceMode,
			Root:      workerRoot,
			BaseRepo:  strings.TrimSpace(baseRepo),
			CreatedAt: now,
		},
		Status:    harness.TaskStatusRunning,
		LastRunID: runID,
		CreatedAt: meta.CreatedAt,
		UpdatedAt: now,
		StartedAt: now,
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

func (c *AgentControl) recordHarnessTaskQueued(meta agentthread.Metadata, role, intent string, isolation IsolationMode, baseRepo string) {
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
		Workspace: harness.WorkspaceLease{
			Mode:     workspaceMode,
			BaseRepo: strings.TrimSpace(baseRepo),
		},
		Status:    harness.TaskStatusQueued,
		LastRunID: runID,
		CreatedAt: meta.CreatedAt,
		UpdatedAt: now,
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
		WorkerID:      prepared.WorkerID,
		WorkerType:    prepared.WorkerType.Name,
		ThreadMeta:    prepared.ThreadMeta,
		Description:   prepared.Description,
		Prompt:        prepared.Prompt,
		Isolation:     string(prepared.Isolation),
		BaseRepo:      prepared.BaseRepo,
		IsFork:        prepared.IsFork,
		ForkMode:      prepared.ForkMode,
		ParentHistory: append([]providers.ChatMessage(nil), prepared.ParentHistory...),
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
		WorkerID:      workerID,
		WorkerType:    wt,
		ThreadMeta:    payload.ThreadMeta,
		Description:   payload.Description,
		Prompt:        payload.Prompt,
		Isolation:     isolation,
		BaseRepo:      payload.BaseRepo,
		IsFork:        payload.IsFork,
		ForkMode:      payload.ForkMode,
		ParentHistory: append([]providers.ChatMessage(nil), payload.ParentHistory...),
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
			return errors.New("isolation=worktree requires a git repository (this workspace is not a git repo)")
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
	c.recordHarnessTaskStart(prepared.ThreadMeta, prepared.WorkerType.Name, prepared.Prompt, workerRoot, prepared.Isolation, prepared.BaseRepo)
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
		initialHistory = append([]providers.ChatMessage(nil), prepared.ParentHistory...)
		if prepared.ThreadMeta.AgentProfile != "" {
			initialHistory = withInitialSystemPrompt(initialHistory, systemPrompt)
		} else {
			systemPrompt = ""
		}
		if prepared.Isolation == IsolationWorktree {
			prompt = appendForkWorktreeReminder(prompt, workerRoot, prepared.Isolation)
		}
	}
	historyPath := ""
	if c.historyDir != "" {
		historyPath = filepath.Join(c.historyDir, prepared.WorkerID+".json")
	}
	_, err = c.manager.Spawn(context.WithoutCancel(ctx), subagent.SpawnOptions{
		ID:             prepared.WorkerID,
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
		InitialHistory: initialHistory,
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
	statusPath := filepath.Join(artifactDir, "git-status.txt")
	if err := os.WriteFile(statusPath, []byte(statusOut), 0o644); err == nil {
		_ = c.harnessStore.AddArtifact(harness.Artifact{
			ID:        snap.ID + "-git-status",
			TaskID:    snap.ID,
			RunID:     harnessRunID(snap.ID),
			Kind:      harness.ArtifactEvidence,
			Path:      statusPath,
			Summary:   "worktree git status",
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
	parentPath := parentPathForSnapshot(snap)
	if meta, ok := c.threads.Resolve(parentID); ok && strings.TrimSpace(meta.Path) != "" {
		parentPath = meta.Path
	}
	communication := c.newAgentCompletionCommunication(snap, parentPath)
	_, err := c.manager.Followup(ctx, parentID, communication.String())
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
	return newAgentCompletionCommunicationWithMessage(snap, recipientPath, NewAgentMailboxMessageWithReport(snap, reportPath, artifacts))
}

// AgentCompletionChatMessage returns the user-role handoff that should resume
// the recipient agent after a child agent finishes.
func (c *AgentControl) AgentCompletionChatMessage(snap subagent.SubAgentSnapshot, recipientPath string) providers.ChatMessage {
	reportPath, artifacts := c.harnessReportForTask(snap.ID)
	communication := newAgentCompletionCommunicationWithMessageAndTrigger(
		snap,
		recipientPath,
		NewAgentMailboxMessageWithReport(snap, reportPath, artifacts),
		true,
	)
	return providers.ChatMessage{
		Role:    "user",
		Content: communication.String(),
	}
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
//   - Delegation rules (spawn/fork_turns, communication planes,
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

- spawn_agent — start a new worker. By default it inherits your full conversation history; set fork_turns="none" for a clean slate, or a positive integer string for only the last N user turns.
- send_message — queue a message for an existing worker without triggering a new turn.
- followup_task — send a follow-up task message and trigger the target worker's next turn.
- wait_agent — wait for any mailbox update only when an agent notification blocks your next step.
- await_agents — explicitly join specific child agents, or all active descendant agents, and return structured per-agent results.
- close_agent — stop a running worker that is stuck or off-track.
- list_agents — see active workers and their status.

## Agent Types

- worker: general-purpose implementation, testing, and exploration.
- research: read-only codebase investigation. Use for open-ended questions where you want evidence without edits.
- verification: read-only adversarial review. Use after meaningful changes or when you need an independent check.

Workers execute tasks autonomously and return a structured handoff. The worker result is input for your own synthesis; do not forward it blindly.

## When to Use Agents

Do not spawn workers for trivial tasks you can handle yourself — reading a specific file, running a quick grep, or reporting a command output. Keep work local when the task is tightly coupled, small, or on the critical path. Spawn agents only when delegation materially improves the work: multi-file refactors, independent research across different areas, verification that benefits from a separate context, or work that can run in parallel.

Do not delegate work that blocks your immediate next step. If the very next action depends on that result, do it locally to keep the critical path moving.

Do not delegate understanding. Never hand off vague prompts like "based on your findings, fix the bug" or "based on the research, implement it." Read the findings yourself, decide what should happen, then give the worker a concrete brief.

## Concurrency

Launch independent workers in parallel whenever possible. Research tasks can run freely in parallel. Write-heavy tasks should run one at a time per file set to avoid conflicts.

After spawning async workers, keep doing meaningful non-overlapping work when it exists. If there is no useful local work left, end your turn and let mailbox notifications automatically resume you. Do not repeatedly wait by reflex.

Use await_agents when synthesis or integration depends on child outputs. Prefer explicit targets. Omit targets only when you intentionally want to join all active descendant tasks. If await_agents returns awaiting_report, the worker finished without a durable handoff; follow up or verify before relying on the result.

## Working with Worker Results

Agent messages arrive as structured inter-agent notifications with author, recipient, content, and trigger_turn fields. Treat content as the actual instruction or result. When a worker finishes, its result automatically arrives as a notification in your next turn.

Before launching follow-up work, read the returned content yourself and do your own synthesis. Worker output is not a substitute for your judgment.

## Writing Worker Prompts

Good worker prompts are self-contained unless you deliberately use fork_turns to inherit context. Include the task, background, role, identity or memory status, scope, non-goals, starting points, acceptance criteria, deliverables, reporting expectations, and constraints. For code-edit subtasks, split work so each worker has a disjoint write set.

Use fork_turns="none" only when the prompt is fully self-contained. Preserve fork_turns="all" when the child needs the user's intent, prior analysis, or repo findings already in this conversation.

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
	switch isolation {
	case IsolationWorktree:
		fmt.Fprintf(&b, "Your working directory is %s — a git worktree isolated from other workers. ", workerRoot)
		b.WriteString("Edits you make stay sandboxed; the orchestrator will inspect the worktree after you finish. ")
	default: // inplace
		fmt.Fprintf(&b, "Your working directory is %s — the SHARED parent repository. ", workerRoot)
		b.WriteString("You are running inplace (no worktree isolation), so be especially careful: ")
		b.WriteString("read-only operations are safe, but any file you modify is visible to the orchestrator and other workers immediately. ")
	}
	b.WriteString("All file paths in your tools resolve relative to this directory. ")
	b.WriteString("You may spawn further sub-agents when a task is genuinely independent or needs isolated verification, but you remain responsible for synthesizing their reports before you finish.\n")
	if base != "" {
		b.WriteString("\n---\n\n")
		b.WriteString(base)
		b.WriteString("\n\n---\n\n")
		b.WriteString("Worker override: if any inherited text above describes the MAIN interactive agent as read-only, or says file writes / shell commands must be delegated, ignore that text. It applies to the parent, not to you. If a tool is in your tool list, you may use it unless your task prompt explicitly forbids it.")
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
