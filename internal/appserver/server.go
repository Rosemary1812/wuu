package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/providerfactory"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/providers/codex"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
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
	Turns         []Turn
	MemoryPath    string

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
	if rt != nil && rt.AgentControl != nil {
		ch := make(chan subagent.Notification, 64)
		rt.AgentControl.Subscribe(ch)
		go s.forwardAgentNotifications(ch)
	}
	return s
}

func (s *Server) forwardAgentNotifications(ch <-chan subagent.Notification) {
	for n := range ch {
		_ = s.writeNotification(NotificationAgentUpdated, AgentUpdatedNotification{
			Agent: agentFromSnapshot(n.Snapshot),
		})
		switch n.Status {
		case subagent.StatusCompleted, subagent.StatusFailed, subagent.StatusCancelled:
			if s.isRootAgentSnapshot(n.Snapshot) {
				_ = s.writeNotification(NotificationAgentMailbox, AgentMailboxNotification{
					Message: agentcontrol.NewAgentMailboxMessage(n.Snapshot),
				})
			}
		}
	}
}

func (s *Server) isRootAgentSnapshot(snap subagent.SubAgentSnapshot) bool {
	parentID := strings.TrimSpace(snap.ParentID)
	if parentID == "" {
		return true
	}
	if s == nil || s.rt == nil || s.rt.AgentControl == nil {
		return false
	}
	return parentID == s.rt.AgentControl.SessionID()
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

	s.rt.SetSessionID(id)
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
		id, err = session.MostRecentForCWD(s.rt.SessionDir, s.rt.RootDir)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		if id == "" {
			return s.writeResponse(req.ID, nil, errors.New("no sessions found"))
		}
	}
	path, err := session.Load(s.rt.SessionDir, id)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
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
	s.mu.Lock()
	s.threads[id] = th
	s.mu.Unlock()

	s.rt.SetSessionID(id)
	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	result := ThreadResumeResult{Thread: thread}
	if err := s.writeResponse(req.ID, result, nil); err != nil {
		return err
	}
	return s.writeNotification(NotificationThreadResumed, ThreadResumedNotification{
		Thread: thread,
	})
}

func (s *Server) handleThreadList(req Request) error {
	s.mu.Lock()
	threads := make([]Thread, 0, len(s.threads))
	for _, th := range s.threads {
		th.mu.Lock()
		threads = append(threads, th.snapshotLocked())
		th.mu.Unlock()
	}
	s.mu.Unlock()
	return s.writeResponse(req.ID, ThreadListResult{Threads: threads}, nil)
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
	if params.Prompt == "" {
		return s.writeResponse(req.ID, nil, errors.New("prompt is required"))
	}
	th := s.thread(params.ThreadID)
	if th == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q not found", params.ThreadID))
	}

	turnID := session.NewID()
	turnCtx, cancel := context.WithCancel(ctx)
	userMsg := providers.ChatMessage{Role: "user", Content: params.Prompt}
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
	turn := th.startTurnLocked(turnID, params.Prompt, now)
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

	go s.runTurn(turnCtx, th, turnID, history)
	return nil
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

func (s *Server) runTurn(ctx context.Context, th *threadState, turnID string, history []providers.ChatMessage) {
	notify := func(method string, params any) {
		_ = s.writeNotification(method, params)
	}
	notifyBatch := func(batch []outboundNotification) {
		for _, item := range batch {
			notify(item.method, item.params)
		}
	}
	res, err := s.rt.StreamRunner.RunWithCallback(ctx, history, func(ev providers.StreamEvent) {
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
