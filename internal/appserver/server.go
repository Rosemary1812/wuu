package appserver

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/providerfactory"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/providers/codex"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/subagent"
	"github.com/blueberrycongee/wuu/internal/tools"
)

var errShutdown = errors.New("app-server shutdown requested")

type threadState struct {
	ID            string
	History       []providers.ChatMessage
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ModelProvider string
	Model         string
	CWD           string
	PinnedAt      *time.Time
	ArchivedAt    *time.Time
	Turns         []Turn
	MemoryPath    string

	execRuntime *runtime.ThreadRuntime

	mu          sync.Mutex
	running     bool
	currentTurn string
	cancel      context.CancelFunc

	nextItemIndex         int
	activeAgentItemID     string
	activeReasoningItemID string
	toolItems             map[string]string
}

type Server struct {
	rt      *runtime.Session
	out     io.Writer
	writeMu sync.Mutex

	mu      sync.Mutex
	threads map[string]*threadState

	pendingMu       sync.Mutex
	nextServerReqID int64
	pendingRequests map[string]chan clientResponse
}

func New(rt *runtime.Session, out io.Writer) *Server {
	s := &Server{
		rt:              rt,
		out:             out,
		threads:         make(map[string]*threadState),
		pendingRequests: make(map[string]chan clientResponse),
	}
	if rt != nil && rt.Toolkit != nil {
		rt.Toolkit.SetAskUserBridge(s)
		rt.AskBridge = s
	}
	return s
}

func (s *Server) forwardAgentNotifications(threadID string, control *agentcontrol.AgentControl, ch <-chan subagent.Notification) {
	for n := range ch {
		_ = s.writeNotification(NotificationAgentUpdated, AgentUpdatedNotification{
			ThreadID: threadID,
			Agent:    agentFromSnapshot(n.Snapshot),
		})
		switch n.Status {
		case subagent.StatusCompleted, subagent.StatusFailed, subagent.StatusCancelled:
			if s.isRootAgentSnapshot(control, threadID, n.Snapshot) {
				_ = s.writeNotification(NotificationAgentMailbox, AgentMailboxNotification{
					ThreadID: threadID,
					Message:  agentcontrol.NewAgentMailboxMessage(n.Snapshot),
				})
			}
		}
	}
}

func (s *Server) isRootAgentSnapshot(control *agentcontrol.AgentControl, threadID string, snap subagent.SubAgentSnapshot) bool {
	parentID := strings.TrimSpace(snap.ParentID)
	if parentID == "" {
		return true
	}
	if control != nil && parentID == control.SessionID() {
		return true
	}
	return parentID == strings.TrimSpace(threadID)
}

func agentFromSnapshot(snap subagent.SubAgentSnapshot) Agent {
	out := Agent{
		ID:           snap.ID,
		Type:         snap.Type,
		TaskName:     snap.TaskName,
		AgentPath:    snap.AgentPath,
		ParentID:     snap.ParentID,
		Description:  snap.Description,
		Status:       string(snap.Status),
		Result:       snap.Result,
		InputTokens:  snap.InputTokens,
		OutputTokens: snap.OutputTokens,
		StartedAt:    snap.StartedAt,
		CompletedAt:  snap.CompletedAt,
	}
	if snap.Error != nil {
		out.Error = snap.Error.Error()
	}
	return out
}

func RunStdio(ctx context.Context, rt *runtime.Session, in io.Reader, out io.Writer) error {
	if rt == nil {
		return errors.New("runtime session is required")
	}
	s := New(rt, out)
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := s.handleLine(ctx, []byte(line)); err != nil {
			if errors.Is(err, errShutdown) {
				return nil
			}
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read app-server input: %w", err)
	}
	return nil
}

func (s *Server) handleLine(ctx context.Context, raw []byte) error {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return s.writeResponse(nil, nil, fmt.Errorf("parse request: %w", err))
	}
	if strings.TrimSpace(req.Method) == "" {
		return s.handleClientResponse(raw)
	}
	switch req.Method {
	case MethodInitialize:
		return s.handleInitialize(req)
	case MethodConfigRead:
		return s.handleConfigRead(req)
	case MethodConfigModelUpdate:
		return s.handleConfigModelUpdate(req)
	case MethodConfigCodexModels:
		return s.handleConfigCodexModels(ctx, req)
	case MethodThreadStart:
		return s.handleThreadStart(req)
	case MethodThreadResume:
		return s.handleThreadResume(req)
	case MethodThreadList:
		return s.handleThreadList(req)
	case MethodThreadPin:
		return s.handleThreadPin(req)
	case MethodThreadArchive:
		return s.handleThreadArchive(req)
	case MethodTurnStart:
		return s.handleTurnStart(ctx, req)
	case MethodTurnInterrupt:
		return s.handleTurnInterrupt(req)
	case MethodShutdown:
		if err := s.writeResponse(req.ID, OKResult{OK: true}, nil); err != nil {
			return err
		}
		return errShutdown
	default:
		return s.writeResponse(req.ID, nil, fmt.Errorf("unknown method %q", req.Method))
	}
}

func (s *Server) handleClientResponse(raw []byte) error {
	var resp ClientResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return s.writeResponse(nil, nil, fmt.Errorf("parse response: %w", err))
	}
	key := requestIDKey(resp.ID)
	if key == "" {
		return s.writeResponse(nil, nil, errors.New("response id is required"))
	}
	s.pendingMu.Lock()
	ch := s.pendingRequests[key]
	if ch != nil {
		delete(s.pendingRequests, key)
	}
	s.pendingMu.Unlock()
	if ch == nil {
		return nil
	}
	ch <- clientResponse{result: resp.Result, err: resp.Error}
	return nil
}

func (s *Server) handleInitialize(req Request) error {
	return s.writeResponse(req.ID, InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Provider:        s.rt.ProviderName,
		Model:           s.rt.Model,
		Effort:          s.currentEffort(),
		WorkspaceRoot:   s.rt.RootDir,
		Providers:       s.providerSummaries(),
	}, nil)
}

func (s *Server) handleConfigRead(req Request) error {
	return s.writeResponse(req.ID, ConfigReadResult{
		Provider:      s.rt.ProviderName,
		Model:         s.rt.Model,
		Effort:        s.currentEffort(),
		ConfigPath:    s.rt.ConfigPath,
		WorkspaceRoot: s.rt.RootDir,
		SessionDir:    s.rt.SessionDir,
		Providers:     s.providerSummaries(),
	}, nil)
}

func (s *Server) handleConfigModelUpdate(req Request) error {
	var params ConfigModelUpdateParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	providerName := strings.TrimSpace(params.Provider)
	if providerName == "" {
		providerName = s.rt.ProviderName
	}
	model := strings.TrimSpace(params.Model)
	if model == "" {
		return s.writeResponse(req.ID, nil, errors.New("model is required"))
	}
	if s.hasRunningThread() {
		return s.writeResponse(req.ID, nil, errors.New("cannot change model while a turn is running"))
	}
	cfg, _, err := config.LoadFrom(s.rt.RootDir, os.Getenv("HOME"))
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	providerCfg, resolvedName, err := cfg.ResolveProvider(providerName)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	providerCfg.Model = model
	effort := s.currentEffort()
	if params.Effort != nil {
		effort = strings.TrimSpace(*params.Effort)
	}

	var client providers.StreamClient
	if resolvedName != s.rt.ProviderName {
		client, err = providerfactory.BuildStreamClient(providerCfg, resolvedName)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	if params.Effort != nil {
		if err := config.UpdateProviderSelectionAndEffort(s.rt.ConfigPath, resolvedName, model, effort); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	} else if err := config.UpdateProviderSelection(s.rt.ConfigPath, resolvedName, model); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	s.rt.ProviderName = resolvedName
	s.rt.Model = model
	if s.rt.StreamRunner != nil {
		if client != nil {
			s.rt.StreamRunner.Client = client
		}
		s.rt.StreamRunner.Model = model
		if params.Effort != nil {
			s.rt.StreamRunner.Effort = effort
		}
		s.rt.StreamRunner.ContextWindowOverride = runtime.ResolveContextWindow(
			model,
			providerCfg.ContextWindow,
			cfg.Agent.MaxContextTokens,
		)
	}
	s.updateIdleThreadRuntime(resolvedName, model)

	return s.writeResponse(req.ID, ConfigModelUpdateResult{
		Provider:  resolvedName,
		Model:     model,
		Effort:    effort,
		Providers: s.providerSummaries(),
	}, nil)
}

func (s *Server) handleConfigCodexModels(ctx context.Context, req Request) error {
	var params ConfigCodexModelsParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	providerName := strings.TrimSpace(params.Provider)
	if providerName == "" {
		providerName = s.rt.ProviderName
	}
	cfg, _, err := config.LoadFrom(s.rt.RootDir, os.Getenv("HOME"))
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	providerCfg, resolvedName, err := cfg.ResolveProvider(providerName)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if !isCodexProviderType(providerCfg.Type) {
		return s.writeResponse(req.ID, nil, fmt.Errorf("provider %s uses type %s; Codex models require openai-codex", resolvedName, providerCfg.Type))
	}
	client, err := codex.New(codex.ClientConfig{
		BaseURL: providerCfg.BaseURL,
		APIKey:  explicitProviderAPIKey(providerCfg),
		Headers: providerCfg.Headers,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	models, err := client.Models(ctx)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	out := make([]CodexModelSummary, 0, len(models))
	for _, model := range models {
		out = append(out, CodexModelSummary{
			Slug:                  model.Slug,
			DisplayName:           model.DisplayName,
			DefaultReasoningLevel: model.DefaultReasoningLevel,
			SupportedReasoning:    append([]string(nil), model.SupportedReasoning...),
			SupportedInAPI:        model.SupportedInAPI,
		})
	}
	effort := strings.TrimSpace(cfg.Agent.Effort)
	if resolvedName == s.rt.ProviderName {
		effort = s.currentEffort()
	}
	return s.writeResponse(req.ID, ConfigCodexModelsResult{
		Provider: resolvedName,
		Model:    providerCfg.Model,
		Effort:   effort,
		Models:   out,
	}, nil)
}

func (s *Server) handleThreadStart(req Request) error {
	id := session.NewID()
	sess, err := session.CreateWithMetadata(s.rt.SessionDir, id, s.rt.RootDir)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	history := make([]providers.ChatMessage, 0, 1)
	if prompt := strings.TrimSpace(s.rt.StreamRunner.SystemPrompt); prompt != "" {
		history = append(history, providers.ChatMessage{Role: "system", Content: prompt})
	}
	th := newThreadState(id, history, s.rt.ProviderName, s.rt.Model, s.rt.RootDir, session.FilePath(s.rt.SessionDir, sess.ID), time.Now().UTC())

	s.mu.Lock()
	s.threads[id] = th
	s.mu.Unlock()

	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	if err := s.writeResponse(req.ID, ThreadStartResult{Thread: thread}, nil); err != nil {
		return err
	}
	return s.writeNotification(NotificationThreadStarted, ThreadStartedNotification{
		Thread: thread,
	})
}

func (s *Server) handleThreadResume(req Request) error {
	var params ThreadResumeParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := strings.TrimSpace(params.SessionID)
	var err error
	if id == "" {
		id, err = s.mostRecentVisibleThreadID()
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		if id == "" {
			return s.writeResponse(req.ID, nil, errors.New("no sessions found"))
		}
	}
	if th := s.thread(id); th != nil {
		th.mu.Lock()
		thread := th.snapshotLocked()
		th.mu.Unlock()
		thread, err = s.threadWithChildAgents(thread)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		result := ThreadResumeResult{Thread: thread}
		if err := s.writeResponse(req.ID, result, nil); err != nil {
			return err
		}
		return s.writeNotification(NotificationThreadResumed, ThreadResumedNotification{
			Thread: thread,
		})
	}
	path, err := session.Load(s.rt.SessionDir, id)
	if err != nil {
		thread, ok, agentErr := s.agentSessionThread(id)
		if agentErr != nil {
			return s.writeResponse(req.ID, nil, agentErr)
		}
		if !ok {
			return s.writeResponse(req.ID, nil, err)
		}
		result := ThreadResumeResult{Thread: thread}
		if err := s.writeResponse(req.ID, result, nil); err != nil {
			return err
		}
		return s.writeNotification(NotificationThreadResumed, ThreadResumedNotification{
			Thread: thread,
		})
	}
	history, err := loadChatMessages(path)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	normalized, err := providers.NormalizeAndValidateMessages(history)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if !reflect.DeepEqual(normalized, history) {
		if err := rewriteChatHistory(path, normalized); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	history = normalized
	history = ensureBaseSystemPrompt(history, s.rt.StreamRunner.SystemPrompt)
	th := newThreadState(id, history, s.rt.ProviderName, s.rt.Model, s.rt.RootDir, path, time.Now().UTC())
	if metadata, ok, err := session.Find(s.rt.SessionDir, id); err != nil {
		return s.writeResponse(req.ID, nil, err)
	} else if ok {
		applySessionMetadata(th, metadata)
	}
	s.mu.Lock()
	s.threads[id] = th
	s.mu.Unlock()

	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	thread, err = s.threadWithChildAgents(thread)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	result := ThreadResumeResult{Thread: thread}
	if err := s.writeResponse(req.ID, result, nil); err != nil {
		return err
	}
	return s.writeNotification(NotificationThreadResumed, ThreadResumedNotification{
		Thread: thread,
	})
}

func (s *Server) handleThreadList(req Request) error {
	sessions, err := session.ListForCWD(s.rt.SessionDir, s.rt.RootDir, 0)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	entries := make(map[string]threadListEntry, len(sessions))
	for _, sess := range sessions {
		if sess.ArchivedAt != nil {
			continue
		}
		entries[sess.ID] = threadEntryFromSession(sess, s.rt.ProviderName, s.rt.Model)
	}

	s.mu.Lock()
	for _, th := range s.threads {
		th.mu.Lock()
		thread := th.snapshotLocked()
		entry := threadListEntry{thread: thread, pinnedAt: th.PinnedAt}
		th.mu.Unlock()
		if thread.Archived {
			delete(entries, thread.ID)
			continue
		}
		if thread.CWD == s.rt.RootDir {
			entries[thread.ID] = entry
		}
	}
	s.mu.Unlock()

	threads := make([]threadListEntry, 0, len(entries))
	for _, entry := range entries {
		threads = append(threads, entry)
	}
	sortThreadListEntries(threads)
	result := make([]Thread, 0, len(threads))
	for _, entry := range threads {
		thread, err := s.threadWithChildAgents(entry.thread)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		result = append(result, thread)
	}
	return s.writeResponse(req.ID, ThreadListResult{Threads: result}, nil)
}

func (s *Server) handleThreadPin(req Request) error {
	var params ThreadPinParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := strings.TrimSpace(params.ThreadID)
	if id == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	metadata, err := session.UpdatePinned(s.rt.SessionDir, id, params.Pinned)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	thread, err := s.threadAfterMetadataUpdate(metadata)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadPinResult{Thread: thread}, nil)
}

func (s *Server) handleThreadArchive(req Request) error {
	var params ThreadArchiveParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := strings.TrimSpace(params.ThreadID)
	if id == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if params.Archived {
		if th := s.thread(id); th != nil {
			th.mu.Lock()
			running := th.running
			th.mu.Unlock()
			if running {
				return s.writeResponse(req.ID, nil, errors.New("cannot archive a running thread"))
			}
		}
	}
	metadata, err := session.UpdateArchived(s.rt.SessionDir, id, params.Archived)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	thread, err := s.threadAfterMetadataUpdate(metadata)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadArchiveResult{Thread: thread}, nil)
}

type threadListEntry struct {
	thread   Thread
	pinnedAt *time.Time
}

func applySessionMetadata(th *threadState, metadata session.Session) {
	if !metadata.CreatedAt.IsZero() {
		th.CreatedAt = metadata.CreatedAt
	}
	if strings.TrimSpace(metadata.CWD) != "" {
		th.CWD = metadata.CWD
	}
	th.PinnedAt = metadata.PinnedAt
	th.ArchivedAt = metadata.ArchivedAt
}

func threadEntryFromSession(sess session.Session, provider, model string) threadListEntry {
	updatedAt := sess.CreatedAt
	return threadListEntry{
		thread: Thread{
			ID:            sess.ID,
			Preview:       sess.Summary,
			ModelProvider: provider,
			Model:         model,
			CWD:           sess.CWD,
			Status:        ThreadStatusIdle,
			Pinned:        sess.PinnedAt != nil,
			Archived:      sess.ArchivedAt != nil,
			CreatedAt:     sess.CreatedAt,
			UpdatedAt:     updatedAt,
			Turns:         []Turn{},
		},
		pinnedAt: sess.PinnedAt,
	}
}

func (s *Server) threadWithChildAgents(thread Thread) (Thread, error) {
	agents, err := s.childAgentsForThread(thread.ID)
	if err != nil {
		return thread, err
	}
	thread.ChildAgents = agents
	return thread, nil
}

func (s *Server) childAgentsForThread(threadID string) ([]Agent, error) {
	store := s.agentThreadStore(threadID)
	if store == nil {
		return nil, nil
	}
	threads, err := store.ListThreads()
	if err != nil {
		return nil, err
	}

	children := make([]Agent, 0)
	childIndexByPath := make(map[string]int)
	for _, meta := range threads {
		if !isDirectChildAgentThread(threadID, meta) {
			continue
		}
		childIndexByPath[meta.Path] = len(children)
		children = append(children, agentFromThreadMetadata(meta))
	}
	if len(children) == 0 {
		return nil, nil
	}

	for _, meta := range threads {
		if meta.Source.Kind != agentthread.SourceThreadSpawn {
			continue
		}
		for path, index := range childIndexByPath {
			if meta.ID == children[index].ID || !strings.HasPrefix(meta.Path, path+"/") {
				continue
			}
			children[index].NestedCount++
			if isRunningAgentStatus(string(meta.Status)) {
				children[index].NestedRunningCount++
			}
			break
		}
	}

	sort.Slice(children, func(i, j int) bool {
		left := children[i].StartedAt
		right := children[j].StartedAt
		if !left.Equal(right) {
			return left.Before(right)
		}
		return children[i].AgentPath < children[j].AgentPath
	})
	return children, nil
}

func (s *Server) agentSessionThread(agentID string) (Thread, bool, error) {
	rootID, meta, ok, err := s.agentThreadMetadata(agentID)
	if err != nil || !ok {
		return Thread{}, ok, err
	}

	var rec persistedAgentHistory
	history, hasHistory := s.liveAgentHistory(rootID, meta.ID)
	if !hasHistory {
		path, err := s.agentHistoryPath(rootID, meta.ID)
		if err != nil {
			return Thread{}, true, err
		}
		if path != "" {
			loaded, err := loadAgentHistory(path)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return Thread{}, true, err
				}
			} else {
				rec = loaded
				history = append([]providers.ChatMessage(nil), rec.Messages...)
			}
		}
	}
	if len(history) == 0 {
		history = fallbackAgentHistory(meta, rec)
	}

	now := time.Now().UTC()
	createdAt := meta.CreatedAt
	if createdAt.IsZero() {
		createdAt = rec.StartedAt
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := meta.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = rec.CompletedAt
	}
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	model := firstNonEmpty(rec.Model, meta.Model, s.rt.Model)
	cwd := firstNonEmpty(meta.CWD, s.rt.RootDir)

	return Thread{
		ID:            meta.ID,
		ParentID:      rootID,
		AgentPath:     meta.Path,
		Preview:       agentSessionPreview(meta, rec, history),
		ModelProvider: s.rt.ProviderName,
		Model:         model,
		CWD:           cwd,
		Status:        ThreadStatusIdle,
		ReadOnly:      true,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		Turns:         turnsFromHistory(meta.ID, history, now),
	}, true, nil
}

func (s *Server) agentThreadMetadata(agentID string) (string, agentthread.Metadata, bool, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", agentthread.Metadata{}, false, nil
	}
	rootIDs, err := s.rootThreadIDs()
	if err != nil {
		return "", agentthread.Metadata{}, false, err
	}
	for _, rootID := range rootIDs {
		store := s.agentThreadStore(rootID)
		if store == nil {
			continue
		}
		threads, err := store.ListThreads()
		if err != nil {
			return "", agentthread.Metadata{}, false, err
		}
		for _, meta := range threads {
			if meta.ID == agentID && meta.Source.Kind == agentthread.SourceThreadSpawn {
				return rootID, meta, true, nil
			}
		}
	}
	return "", agentthread.Metadata{}, false, nil
}

func (s *Server) rootThreadIDs() ([]string, error) {
	if s == nil || s.rt == nil {
		return nil, nil
	}
	seen := make(map[string]bool)
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}

	sessions, err := session.ListForCWD(s.rt.SessionDir, s.rt.RootDir, 0)
	if err != nil {
		return nil, err
	}
	for _, sess := range sessions {
		add(sess.ID)
	}

	s.mu.Lock()
	for id, th := range s.threads {
		if th == nil {
			continue
		}
		th.mu.Lock()
		cwd := th.CWD
		th.mu.Unlock()
		if cwd == s.rt.RootDir {
			add(id)
		}
	}
	s.mu.Unlock()
	return ids, nil
}

func (s *Server) liveAgentHistory(rootID, agentID string) ([]providers.ChatMessage, bool) {
	th := s.thread(rootID)
	if th == nil {
		return nil, false
	}
	th.mu.Lock()
	threadRuntime := th.execRuntime
	th.mu.Unlock()
	if threadRuntime == nil || threadRuntime.AgentControl == nil || threadRuntime.AgentControl.Manager() == nil {
		return nil, false
	}
	return threadRuntime.AgentControl.Manager().History(agentID)
}

func (s *Server) agentHistoryPath(rootID, agentID string) (string, error) {
	rootID = strings.TrimSpace(rootID)
	agentID = strings.TrimSpace(agentID)
	if s == nil || s.rt == nil || rootID == "" || agentID == "" {
		return "", nil
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statepath.SessionArtifactDir(stateDir, rootID), "workers", agentID+".json"), nil
}

func (s *Server) agentThreadStore(threadID string) *agentthread.Store {
	if s == nil || s.rt == nil || strings.TrimSpace(threadID) == "" {
		return nil
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return nil
	}
	return agentthread.NewStore(filepath.Join(statepath.SessionArtifactDir(stateDir, threadID), "threads"))
}

func (s *Server) workspaceStateDir() (string, error) {
	if s == nil || s.rt == nil {
		return "", errors.New("runtime session is required")
	}
	if stateDir := strings.TrimSpace(s.rt.StateDir); stateDir != "" {
		return stateDir, nil
	}
	home, err := statepath.Home("")
	if err != nil {
		return "", err
	}
	return statepath.WorkspaceDir(home, s.rt.RootDir)
}

func fallbackAgentHistory(meta agentthread.Metadata, rec persistedAgentHistory) []providers.ChatMessage {
	prompt := firstNonEmpty(rec.Prompt, meta.LastTaskMessage)
	result := strings.TrimSpace(rec.Result)
	errorText := strings.TrimSpace(rec.Error)
	history := make([]providers.ChatMessage, 0, 2)
	if prompt != "" {
		history = append(history, providers.ChatMessage{Role: "user", Content: prompt})
	}
	if result != "" {
		history = append(history, providers.ChatMessage{Role: "assistant", Content: result})
	} else if errorText != "" {
		history = append(history, providers.ChatMessage{Role: "assistant", Content: "Worker failed: " + errorText})
	}
	return history
}

func agentSessionPreview(meta agentthread.Metadata, rec persistedAgentHistory, history []providers.ChatMessage) string {
	if preview := firstNonEmpty(rec.Description, meta.TaskName, threadPreview(history), rec.Prompt, meta.LastTaskMessage, meta.ID); preview != "" {
		return preview
	}
	return "子任务"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isDirectChildAgentThread(threadID string, meta agentthread.Metadata) bool {
	if meta.Source.Kind != agentthread.SourceThreadSpawn || meta.ID == threadID {
		return false
	}
	if meta.Source.ParentPath == agentthread.RootPath {
		return true
	}
	return strings.TrimSpace(meta.ParentID) == strings.TrimSpace(threadID) && agentPathDepth(meta.Path) == 2
}

func agentFromThreadMetadata(meta agentthread.Metadata) Agent {
	startedAt := meta.CreatedAt
	if startedAt.IsZero() {
		startedAt = meta.UpdatedAt
	}
	return Agent{
		ID:          meta.ID,
		Type:        meta.Role,
		TaskName:    meta.TaskName,
		AgentPath:   meta.Path,
		ParentID:    meta.ParentID,
		Description: meta.TaskName,
		Status:      string(meta.Status),
		StartedAt:   startedAt,
	}
}

func isRunningAgentStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case string(agentthread.StatusPending), string(agentthread.StatusRunning):
		return true
	default:
		return false
	}
}

func agentPathDepth(path string) int {
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "/") + 1
}

func sortThreadListEntries(entries []threadListEntry) {
	sort.Slice(entries, func(i, j int) bool {
		leftPinned := entries[i].pinnedAt != nil
		rightPinned := entries[j].pinnedAt != nil
		if leftPinned != rightPinned {
			return leftPinned
		}
		if leftPinned && rightPinned && !entries[i].pinnedAt.Equal(*entries[j].pinnedAt) {
			return entries[i].pinnedAt.After(*entries[j].pinnedAt)
		}
		leftTime := entries[i].thread.UpdatedAt
		if leftTime.IsZero() {
			leftTime = entries[i].thread.CreatedAt
		}
		rightTime := entries[j].thread.UpdatedAt
		if rightTime.IsZero() {
			rightTime = entries[j].thread.CreatedAt
		}
		return leftTime.After(rightTime)
	})
}

func (s *Server) mostRecentVisibleThreadID() (string, error) {
	sessions, err := session.ListForCWD(s.rt.SessionDir, s.rt.RootDir, 0)
	if err != nil {
		return "", err
	}
	entries := make([]threadListEntry, 0, len(sessions))
	for _, sess := range sessions {
		if sess.ArchivedAt != nil {
			continue
		}
		entries = append(entries, threadEntryFromSession(sess, s.rt.ProviderName, s.rt.Model))
	}
	sortThreadListEntries(entries)
	if len(entries) == 0 {
		return "", nil
	}
	return entries[0].thread.ID, nil
}

func (s *Server) threadAfterMetadataUpdate(metadata session.Session) (Thread, error) {
	if th := s.thread(metadata.ID); th != nil {
		th.mu.Lock()
		applySessionMetadata(th, metadata)
		thread := th.snapshotLocked()
		th.mu.Unlock()
		return s.threadWithChildAgents(thread)
	}
	return s.threadWithChildAgents(threadEntryFromSession(metadata, s.rt.ProviderName, s.rt.Model).thread)
}

func (s *Server) hasRunningThread() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, th := range s.threads {
		th.mu.Lock()
		running := th.running
		th.mu.Unlock()
		if running {
			return true
		}
	}
	return false
}

func (s *Server) updateIdleThreadRuntime(providerName, model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, th := range s.threads {
		th.mu.Lock()
		if !th.running {
			th.ModelProvider = providerName
			th.Model = model
			if th.execRuntime != nil && th.execRuntime.StreamRunner != nil && s.rt != nil && s.rt.StreamRunner != nil {
				th.execRuntime.StreamRunner.Client = s.rt.StreamRunner.Client
				th.execRuntime.StreamRunner.Model = model
				th.execRuntime.StreamRunner.Effort = s.currentEffort()
				th.execRuntime.StreamRunner.ContextWindowOverride = s.rt.StreamRunner.ContextWindowOverride
			}
		}
		th.mu.Unlock()
	}
}

func (s *Server) currentEffort() string {
	if s == nil || s.rt == nil || s.rt.StreamRunner == nil {
		return ""
	}
	return strings.TrimSpace(s.rt.StreamRunner.Effort)
}

func (s *Server) providerSummaries() []ProviderSummary {
	cfg, _, err := config.LoadFrom(s.rt.RootDir, os.Getenv("HOME"))
	if err != nil {
		return nil
	}
	return providerSummariesFromConfig(cfg)
}

func providerSummariesFromConfig(cfg config.Config) []ProviderSummary {
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ProviderSummary, 0, len(names))
	for _, name := range names {
		provider := cfg.Providers[name]
		out = append(out, ProviderSummary{
			Name:  name,
			Type:  provider.Type,
			Model: provider.Model,
		})
	}
	return out
}

func isCodexProviderType(providerType string) bool {
	s := strings.ToLower(strings.TrimSpace(providerType))
	s = strings.ReplaceAll(s, "_", "-")
	return s == "openai-codex" || s == "codex-subscription" || s == "chatgpt-codex"
}

func explicitProviderAPIKey(provider config.ProviderConfig) string {
	if key := strings.TrimSpace(provider.APIKey); key != "" {
		return key
	}
	if envKey := strings.TrimSpace(provider.APIKeyEnv); envKey != "" {
		return strings.TrimSpace(os.Getenv(envKey))
	}
	return ""
}

func (s *Server) handleTurnStart(ctx context.Context, req Request) error {
	var params TurnStartParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	params.ThreadID = strings.TrimSpace(params.ThreadID)
	params.Prompt = strings.TrimSpace(params.Prompt)
	if params.ThreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	images, err := normalizeTurnStartImages(params.Images)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if params.Prompt == "" && len(images) == 0 {
		return s.writeResponse(req.ID, nil, errors.New("prompt or image is required"))
	}
	th := s.thread(params.ThreadID)
	if th == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q not found", params.ThreadID))
	}
	threadRuntime, err := s.ensureThreadRuntime(th)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	turnID := session.NewID()
	turnCtx, cancel := context.WithCancel(ctx)
	turnCtx = withAskUserThreadID(turnCtx, th.ID)
	userMsg := providers.ChatMessage{Role: "user", Content: params.Prompt, Images: images}
	now := time.Now().UTC()

	th.mu.Lock()
	if th.running {
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q already has a running turn", params.ThreadID))
	}
	if err := appendChatMessage(th.MemoryPath, userMsg); err != nil {
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, err)
	}
	history := append([]providers.ChatMessage(nil), th.History...)
	history = append(history, userMsg)
	th.History = history
	th.cancel = cancel
	turn := th.startTurnLocked(turnID, userMsg, now)
	th.mu.Unlock()

	if err := s.writeResponse(req.ID, TurnStartResult{Turn: turn}, nil); err != nil {
		cancel()
		return err
	}
	if err := s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
		ThreadID: params.ThreadID,
		Turn:     turn,
	}); err != nil {
		cancel()
		return err
	}

	go s.runTurn(turnCtx, th, threadRuntime, turnID, history)
	return nil
}

func (s *Server) ensureThreadRuntime(th *threadState) (*runtime.ThreadRuntime, error) {
	if th == nil {
		return nil, errors.New("thread is required")
	}
	th.mu.Lock()
	existing := th.execRuntime
	th.mu.Unlock()
	if existing != nil {
		return existing, nil
	}
	if s.rt == nil {
		return nil, errors.New("runtime session is required")
	}
	threadRuntime, err := s.rt.NewThreadRuntime(th.ID, s)
	if err != nil {
		return nil, err
	}
	th.mu.Lock()
	if th.execRuntime == nil {
		th.execRuntime = threadRuntime
		th.mu.Unlock()
		s.subscribeThreadRuntime(th.ID, threadRuntime)
		return threadRuntime, nil
	}
	existing = th.execRuntime
	th.mu.Unlock()
	return existing, nil
}

func (s *Server) subscribeThreadRuntime(threadID string, threadRuntime *runtime.ThreadRuntime) {
	if threadRuntime == nil || threadRuntime.AgentControl == nil {
		return
	}
	ch := make(chan subagent.Notification, 64)
	threadRuntime.AgentControl.Subscribe(ch)
	go s.forwardAgentNotifications(threadID, threadRuntime.AgentControl, ch)
}

func normalizeTurnStartImages(images []TurnStartImage) ([]providers.InputImage, error) {
	if len(images) == 0 {
		return nil, nil
	}
	out := make([]providers.InputImage, 0, len(images))
	for index, image := range images {
		mediaType := strings.TrimSpace(image.MediaType)
		data := strings.TrimSpace(image.Data)
		if data == "" {
			return nil, fmt.Errorf("image %d data is required", index+1)
		}
		var err error
		mediaType, data, err = normalizeImagePayload(mediaType, data)
		if err != nil {
			return nil, fmt.Errorf("image %d: %w", index+1, err)
		}
		out = append(out, providers.InputImage{MediaType: mediaType, Data: data})
	}
	return out, nil
}

func normalizeImagePayload(mediaType, data string) (string, string, error) {
	if strings.HasPrefix(strings.ToLower(data), "data:") {
		header, payload, ok := strings.Cut(data, ",")
		if !ok {
			return "", "", errors.New("invalid data URL")
		}
		if !strings.Contains(strings.ToLower(header), ";base64") {
			return "", "", errors.New("image data URL must be base64")
		}
		if mediaType == "" {
			mediaType = strings.TrimPrefix(strings.Split(header, ";")[0], "data:")
		}
		data = strings.TrimSpace(payload)
	}
	if mediaType == "" {
		mediaType = "image/png"
	}
	if !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return "", "", fmt.Errorf("unsupported media type %q", mediaType)
	}
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return "", "", fmt.Errorf("invalid base64 data: %w", err)
	}
	return mediaType, data, nil
}

func (s *Server) handleTurnInterrupt(req Request) error {
	var params TurnInterruptParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	th := s.thread(strings.TrimSpace(params.ThreadID))
	if th == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q not found", params.ThreadID))
	}
	th.mu.Lock()
	cancel := th.cancel
	th.mu.Unlock()
	if cancel == nil {
		return s.writeResponse(req.ID, nil, errors.New("thread has no running turn"))
	}
	cancel()
	return s.writeResponse(req.ID, OKResult{OK: true}, nil)
}

func (s *Server) runTurn(ctx context.Context, th *threadState, threadRuntime *runtime.ThreadRuntime, turnID string, history []providers.ChatMessage) {
	notify := func(method string, params any) {
		_ = s.writeNotification(method, params)
	}
	notifyBatch := func(batch []outboundNotification) {
		for _, item := range batch {
			notify(item.method, item.params)
		}
	}
	runner := s.rt.StreamRunner
	if threadRuntime != nil && threadRuntime.StreamRunner != nil {
		runner = threadRuntime.StreamRunner
	}
	res, err := runner.RunWithCallback(ctx, history, func(ev providers.StreamEvent) {
		th.mu.Lock()
		batch := th.applyStreamEventLocked(turnID, ev, time.Now().UTC())
		th.mu.Unlock()
		notifyBatch(batch)
		notify(NotificationTurnEvent, TurnEventNotification{
			ThreadID: th.ID,
			TurnID:   turnID,
			Event:    sanitizeStreamEvent(ev),
		})
	})

	now := time.Now().UTC()
	th.mu.Lock()
	rewriteHistory := res.HistoryRewritten
	if res.HistoryRewritten {
		th.History = append([]providers.ChatMessage(nil), res.NewMessages...)
	} else {
		th.History = append(th.History, res.NewMessages...)
	}
	var historyErr error
	if normalized, nerr := providers.NormalizeAndValidateMessages(th.History); nerr != nil {
		historyErr = nerr
	} else if !reflect.DeepEqual(normalized, th.History) {
		th.History = normalized
		rewriteHistory = true
	}
	var persistErr error
	if historyErr != nil {
		persistErr = historyErr
	} else {
		persistErr = s.persistTurnResultLocked(th, res, rewriteHistory)
	}
	status := TurnStatusCompleted
	if err != nil {
		status = TurnStatusFailed
		if errors.Is(err, context.Canceled) {
			status = TurnStatusInterrupted
		}
	}
	if err == nil && persistErr != nil {
		err = persistErr
		status = TurnStatusFailed
	}
	turn := th.completeTurnLocked(turnID, status, err, now)
	th.mu.Unlock()

	if err != nil {
		notify(NotificationTurnError, TurnErrorNotification{
			ThreadID: th.ID,
			TurnID:   turnID,
			Error:    err.Error(),
			Turn:     turn,
		})
		return
	}
	notify(NotificationTurnCompleted, TurnCompletedNotification{
		ThreadID:     th.ID,
		Turn:         turn,
		Content:      res.Content,
		InputTokens:  res.InputTokens,
		OutputTokens: res.OutputTokens,
	})
}

func (s *Server) thread(id string) *threadState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threads[id]
}

func (s *Server) persistTurnResultLocked(th *threadState, res agent.LoopResult, rewriteHistory bool) error {
	if strings.TrimSpace(th.MemoryPath) == "" {
		return nil
	}
	if rewriteHistory {
		if err := rewriteChatHistory(th.MemoryPath, th.History); err != nil {
			return err
		}
	} else {
		for _, msg := range res.NewMessages {
			if err := appendChatMessage(th.MemoryPath, msg); err != nil {
				return err
			}
		}
	}
	if err := appendTokenUsage(th.MemoryPath, res.InputTokens, res.OutputTokens); err != nil {
		return err
	}
	return session.UpdateIndex(s.rt.SessionDir, th.ID, persistableMessageCount(th.History), threadPreview(th.History))
}

func (s *Server) AskUser(ctx context.Context, req tools.AskUserRequest) (tools.AskUserResponse, error) {
	if err := req.Validate(); err != nil {
		return tools.AskUserResponse{}, err
	}
	result, err := s.requestClient(ctx, MethodToolRequestUserInput, ToolRequestUserInputParams{
		ThreadID:  askUserThreadID(ctx),
		Questions: req.Questions,
	})
	if err != nil {
		return tools.AskUserResponse{}, err
	}
	var resp tools.AskUserResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return tools.AskUserResponse{}, fmt.Errorf("decode ask_user response: %w", err)
	}
	return resp, nil
}

type clientResponse struct {
	result json.RawMessage
	err    *ResponseError
}

type askUserThreadIDContextKey struct{}

func withAskUserThreadID(ctx context.Context, threadID string) context.Context {
	return context.WithValue(ctx, askUserThreadIDContextKey{}, strings.TrimSpace(threadID))
}

func askUserThreadID(ctx context.Context) string {
	threadID, _ := ctx.Value(askUserThreadIDContextKey{}).(string)
	return strings.TrimSpace(threadID)
}

func (s *Server) requestClient(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := s.nextServerRequestID()
	rawID := json.RawMessage(strconv.Quote(id))
	key := requestIDKey(rawID)
	ch := make(chan clientResponse, 1)

	s.pendingMu.Lock()
	s.pendingRequests[key] = ch
	s.pendingMu.Unlock()

	if err := s.writeJSON(ServerRequest{ID: rawID, Method: method, Params: params}); err != nil {
		s.pendingMu.Lock()
		delete(s.pendingRequests, key)
		s.pendingMu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.err != nil {
			return nil, errors.New(resp.err.Message)
		}
		return resp.result, nil
	case <-ctx.Done():
		s.pendingMu.Lock()
		delete(s.pendingRequests, key)
		s.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

func (s *Server) nextServerRequestID() string {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	s.nextServerReqID++
	return fmt.Sprintf("server-%d", s.nextServerReqID)
}

func requestIDKey(raw json.RawMessage) string {
	return strings.TrimSpace(string(raw))
}

func sanitizeStreamEvent(ev providers.StreamEvent) StreamEventPayload {
	out := StreamEventPayload{
		Type:      ev.Type,
		Content:   ev.Content,
		Truncated: ev.Truncated,
	}
	if ev.Message != nil {
		out.Message = ev.Message
	}
	if ev.ToolCall != nil {
		out.ToolCall = ev.ToolCall
	}
	if ev.ToolResult != "" {
		out.ToolResult = ev.ToolResult
	}
	if ev.Usage != nil {
		out.Usage = ev.Usage
	}
	if ev.StopReason != "" {
		out.StopReason = ev.StopReason
	}
	if ev.Error != nil {
		out.Error = ev.Error.Error()
	}
	return out
}

func decodeParams(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

func (s *Server) writeResponse(id json.RawMessage, result any, err error) error {
	resp := Response{ID: id, Result: result}
	if err != nil {
		resp.Result = nil
		resp.Error = &ResponseError{
			Code:    "error",
			Message: err.Error(),
		}
	}
	return s.writeJSON(resp)
}

func (s *Server) writeNotification(method string, params any) error {
	return s.writeJSON(Notification{
		Method: method,
		Params: params,
	})
}

func (s *Server) writeJSON(v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	enc := json.NewEncoder(s.out)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("write app-server message: %w", err)
	}
	return nil
}
