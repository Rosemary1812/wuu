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
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"crypto/rand"
	"encoding/hex"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/subagent"
	"github.com/blueberrycongee/wuu/internal/worktree"
)

// WorkerToolkitFactory builds a fresh ToolExecutor for a worker thread.
// The metadata argument contains the worker's canonical agent path, so
// orchestration tools inside that worker can resolve relative child paths.
type WorkerToolkitFactory func(rootDir string, wt WorkerType, meta agentthread.Metadata) (agent.ToolExecutor, error)

// AgentControl owns the orchestration runtime for one wuu session.
type AgentControl struct {
	manager       *subagent.Manager
	worktrees     *worktree.Manager // nil when workspace is not a git repo
	parentRepo    string            // absolute path to workspace root
	worktreeRoot  string            // .wuu/worktrees/ directory
	sessionID     string
	historyDir    string
	threadDir     string
	threads       *agentthread.Registry
	threadStore   *agentthread.Store
	rootThreadID  string
	rootThreadDir string
	workerFact    WorkerToolkitFactory
	defaultSys    string // base system prompt prefix added to every worker
	maxParallel   int
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
	WorktreeRoot    string // .wuu/worktrees/ (only used when workspace is a git repo)
	HistoryDir      string // .wuu/sessions/{session-id}/workers/
	ThreadDir       string // .wuu/sessions/{session-id}/threads/
	SessionID       string
	WorkerSysPrompt string
	WorkerFactory   WorkerToolkitFactory
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
		workerFact:   cfg.WorkerFactory,
		defaultSys:   cfg.WorkerSysPrompt,
		maxParallel:  maxP,
	}
	c.registerRootThread()
	statusCh := make(chan subagent.Notification, 64)
	mgr.Subscribe(statusCh)
	go c.consumeWorkerStatus(statusCh)
	return c, nil
}

// Manager exposes the underlying subagent.Manager for advanced use
// (Subscribe, etc.).
func (c *AgentControl) Manager() *subagent.Manager {
	return c.manager
}

// SetSessionInfo updates the coordinator's session ID and history dir
// after the TUI has generated them. Safe to call once at startup.
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
	c.registerRootThread()
}

// SessionID returns the bound session ID, or "session-pending" if
// SetSessionInfo hasn't been called yet.
func (c *AgentControl) SessionID() string {
	return c.sessionID
}

// SpawnRequest is the internal shape of a spawn_agent tool invocation
// after argument validation.
type SpawnRequest struct {
	Type        string
	TaskName    string
	Description string
	Prompt      string
	ParentID    string
	ParentPath  string
	BaseRepo    string // optional: chain off another worktree (worktree mode only)
	Synchronous bool
	Timeout     time.Duration
	// Isolation overrides the worker type's DefaultIsolation when set.
	// Empty string means "use the type default". Use this from
	// spawn_agent to opt a normally-inplace worker into a worktree
	// (e.g. an explorer that needs to run a destructive script).
	Isolation string
}

// SpawnResult is what the spawn_agent tool returns to the model.
type SpawnResult struct {
	AgentID      string `json:"agent_id"`
	TaskName     string `json:"task_name,omitempty"`
	AgentPath    string `json:"agent_path,omitempty"`
	Status       string `json:"status"`
	Isolation    string `json:"isolation"`               // "inplace" or "worktree"
	WorktreePath string `json:"worktree_path,omitempty"` // empty for inplace spawns
	Result       string `json:"result,omitempty"`
	Error        string `json:"error,omitempty"`
	DurationMS   int64  `json:"duration_ms,omitempty"`
}

// Spawn launches a sub-agent. In synchronous mode it blocks until
// the sub-agent finishes; in async mode it returns immediately with
// status "running" and the agent_id the orchestrator can poll.
func (c *AgentControl) Spawn(ctx context.Context, req SpawnRequest) (*SpawnResult, error) {
	// Concurrency cap.
	if c.manager.CountRunning() >= c.maxParallel {
		return nil, fmt.Errorf("max parallel sub-agents reached (%d). Wait for one to complete or close one with close_agent.", c.maxParallel)
	}

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

	// Resolve effective isolation: caller override > type default.
	isolation, err := NormalizeIsolation(req.Isolation, wt)
	if err != nil {
		return nil, err
	}
	// BaseRepo only makes sense for chained worktree spawns.
	if isolation == IsolationInplace && strings.TrimSpace(req.BaseRepo) != "" {
		return nil, errors.New("base_repo is only supported with isolation=worktree")
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
	threadMeta, err := c.registerChildThread(workerID, taskName, wtype, req.Prompt, agentthread.SourceThreadSpawn, "", req.ParentID, req.ParentPath)
	if err != nil {
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		return nil, err
	}

	// 3. Build worker's toolkit rooted at the chosen working directory.
	workerKit, err := c.workerFact(workerRoot, wt, threadMeta)
	if err != nil {
		if failed, ok := c.threads.UpdateStatus(workerID, agentthread.StatusFailed, time.Now().UTC()); ok {
			_ = c.threadStore.RecordStatus(failed)
		}
		if closed, ok := c.threads.UpdateEdgeStatus(workerID, agentthread.EdgeClosed, time.Now().UTC()); ok {
			_ = c.threadStore.RecordEdgeStatus(closed)
		}
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		return nil, fmt.Errorf("worker toolkit: %w", err)
	}

	// 4. Compose system prompt: type-specific role + working dir + base prompt.
	sys := composeWorkerSystemPrompt(c.defaultSys, wt, workerRoot, isolation)

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
		return nil, fmt.Errorf("spawn: %w", err)
	}

	result := &SpawnResult{
		AgentID:   sa.ID,
		TaskName:  threadMeta.TaskName,
		AgentPath: threadMeta.Path,
		Status:    string(sa.Status),
		Isolation: string(isolation),
	}
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

	return result, nil
}

// ForkRequest is the internal shape of a spawn_agent invocation with
// fork_turns enabled after argument validation. It always uses the
// default worker type, but isolation is still caller-selectable: a
// forked history and a worktree are orthogonal concerns.
type ForkRequest struct {
	TaskName    string
	Description string
	ForkMode    string
	ParentID    string
	ParentPath  string
	BaseRepo    string // optional: chain off another worktree (worktree mode only)
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
	if c.manager.CountRunning() >= c.maxParallel {
		return nil, fmt.Errorf("max parallel sub-agents reached (%d). Wait for one to complete or close one with close_agent.", c.maxParallel)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("prompt is required")
	}
	if len(parentHistory) == 0 {
		return nil, errors.New("spawn_agent fork: no parent history (only the main agent in an interactive session can fork)")
	}

	// Resolve the default worker type so the worker has the full
	// tool set. ask_user remains unavailable because workers do not
	// receive an ask bridge.
	wt, err := LookupWorkerType("worker")
	if err != nil {
		return nil, err
	}

	workerID := newAgentControlWorkerID(wt.Name)
	taskName := req.TaskName
	isolation, err := NormalizeIsolation(req.Isolation, wt)
	if err != nil {
		return nil, err
	}
	if isolation == IsolationInplace && strings.TrimSpace(req.BaseRepo) != "" {
		return nil, errors.New("base_repo is only supported with isolation=worktree")
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

	threadMeta, err := c.registerChildThread(workerID, taskName, wt.Name, forkPrompt, agentthread.SourceThreadSpawn, req.ForkMode, req.ParentID, req.ParentPath)
	if err != nil {
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		return nil, err
	}

	workerKit, err := c.workerFact(workerRoot, wt, threadMeta)
	if err != nil {
		if failed, ok := c.threads.UpdateStatus(workerID, agentthread.StatusFailed, time.Now().UTC()); ok {
			_ = c.threadStore.RecordStatus(failed)
		}
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

	// Note: we deliberately do NOT set SystemPrompt — when
	// InitialHistory is non-nil, the subagent runner uses
	// history[0] as the system message and ignores the option.
	workerCtx := ctx
	if !req.Synchronous {
		workerCtx = context.WithoutCancel(ctx)
	}

	sa, err := c.manager.Spawn(workerCtx, subagent.SpawnOptions{
		ID:             workerID,
		Type:           wt.Name,
		TaskName:       threadMeta.TaskName,
		AgentPath:      threadMeta.Path,
		ParentID:       threadMeta.ParentID,
		Description:    req.Description,
		Prompt:         forkPrompt,
		Toolkit:        workerKit,
		HistoryPath:    historyPath,
		InitialHistory: parentHistory,
	})
	if err != nil {
		if worktreeRef != nil {
			_ = c.worktrees.Cleanup(worktreeRef)
		}
		return nil, fmt.Errorf("spawn: %w", err)
	}

	result := &SpawnResult{
		AgentID:   sa.ID,
		TaskName:  threadMeta.TaskName,
		AgentPath: threadMeta.Path,
		Status:    string(sa.Status),
		Isolation: string(isolation),
	}
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
	return c.manager.List()
}

func (c *AgentControl) ListFrom(currentPath, pathPrefix string) []subagent.SubAgentSnapshot {
	list := c.manager.List()
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

func (c *AgentControl) registerChildThread(id, taskName, role, message string, source agentthread.SourceKind, forkMode, parentID, parentPath string) (agentthread.Metadata, error) {
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
		Role:            role,
		LastTaskMessage: message,
		CWD:             c.parentRepo,
		SourceKind:      source,
		ForkMode:        strings.TrimSpace(forkMode),
		Status:          agentthread.StatusRunning,
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

func (c *AgentControl) consumeWorkerStatus(ch <-chan subagent.Notification) {
	for n := range ch {
		if c == nil || c.threads == nil {
			continue
		}
		status := threadStatusFromSubAgent(n.Status)
		meta, ok := c.threads.UpdateStatus(n.AgentID, status, time.Now().UTC())
		if !ok {
			continue
		}
		_ = c.threadStore.RecordStatus(meta)
		if isFinalSubAgentStatus(n.Status) {
			if !c.deliverNestedResultToParent(context.Background(), n.Snapshot) && c.isRootChildSnapshot(n.Snapshot) {
				_ = c.threadStore.RecordCommunication(c.rootThreadID, newAgentCompletionCommunication(n.Snapshot, agentthread.RootPath))
			}
		}
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
	communication := newAgentCompletionCommunication(snap, parentPath)
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
	if strings.TrimSpace(recipientPath) == "" {
		recipientPath = agentthread.RootPath
	}
	content := agentthread.SubagentNotificationContent(snap.AgentPath, NewAgentMailboxMessage(snap))
	return agentthread.NewInterAgentCommunication(parseAgentPathOrRoot(snap.AgentPath), parseAgentPathOrRoot(recipientPath), content, false)
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

// SystemPromptPreamble returns the instructions prepended to the
// main agent's system prompt. It teaches, in order:
//
//   - Step 0: classify every task before acting (Path A / B / C and
//     the "referenced artifact" override).
//   - Path A: when the user has a specific answer in their head,
//     extract it via the ask_user tool instead of guessing.
//   - Path B: when the user hands the decision to the agent, gather
//     context, form a recommendation, and declare it before acting.
//   - The phantom-read rule: if the user references an existing
//     artifact, read_file it in full before planning.
//   - The interview loop: the default iterative rhythm for
//     non-trivial tasks.
//   - Delegation rules (spawn/fork_turns, communication planes,
//     honesty rules, failure handling) — but only AFTER alignment.
//
// There is NO separate "coordinator role" persona here. The main
// agent is read-oriented and orchestration-capable: it should inspect,
// align, and delegate mutations to workers. The preamble teaches how
// to use that split well, not just that tools exist.
func SystemPromptPreamble() string {
	return `You are an orchestration agent. Your job is to help the user achieve their goal by directing workers to research, implement, and verify code changes.

## Your Tools

- spawn_agent — start a new worker. By default it inherits your full conversation history; set fork_turns="none" for a clean slate, or a positive integer string for only the last N user turns.
- send_message — queue a message for an existing worker without triggering a new turn.
- followup_task — send a follow-up task message and trigger the target worker's next turn.
- wait_agent — wait for any mailbox update only when agent output blocks your next step.
- close_agent — stop a running worker that is stuck or off-track.
- list_agents — see active workers and their status.

## Workers

Workers have the full tool set including read_file, write_file, edit_file, run_shell, grep, glob, and git. They execute tasks autonomously.

## Task Workflow

| Phase | Who | Purpose |
|-------|-----|---------|
| Research | Workers (parallel) | Investigate codebase, find files, understand problem |
| Synthesis | You | Read findings, understand the problem, craft implementation specs |
| Implementation | Workers | Make targeted changes per spec |
| Verification | Workers | Test changes work |

## Delegation Discipline

Do not spawn workers for trivial tasks you can handle yourself — reading a specific file, running a quick grep, or reporting a command output. Spawn agents for higher-level work: multi-file refactors, parallel research across different areas, verification that requires running the full test suite, or tasks that benefit from isolated context.

Do not delegate work that blocks your immediate next step. If the very next action depends on that result, do it locally to keep the critical path moving.

Good worker prompts are self-contained: specific file paths, line numbers, exactly what to change, and what counts as done. For code-edit subtasks, split work so each worker has a disjoint write set.

## Concurrency

Launch independent workers in parallel whenever possible. Research tasks can run freely in parallel. Write-heavy tasks should run one at a time per file set to avoid conflicts.

After spawning async workers, keep doing meaningful non-overlapping work when it exists. If there is no useful local work left, end your turn and let mailbox notifications resume you. Do not repeatedly wait by reflex.

## Working with Worker Results

Agent messages arrive as structured inter-agent notifications with author, recipient, content, and trigger_turn fields. Treat content as the actual instruction or result. When a worker finishes, its result arrives as a notification in your next turn.

Before launching follow-up work, read the returned content yourself and do your own synthesis. Never chain workers by implication with phrases like "based on your findings" or "based on the research".

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
	b.WriteString("You CANNOT spawn further sub-agents.\n")
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
