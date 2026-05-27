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
	ID               string
	ParentID         string
	AgentPath        string
	History          []providers.ChatMessage
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ModelProvider    string
	Model            string
	CWD              string
	ForkedFromID     string
	ForkedFromTurnID string
	ForkedFromItemID string
	PinnedAt         *time.Time
	ArchivedAt       *time.Time
	Turns            []Turn
	MemoryPath       string
	ReadOnly         bool

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

	agentCompletionMu            sync.Mutex
	pendingAgentCompletionTurns  map[string][]providers.ChatMessage
	drainingAgentCompletionTurns map[string]bool
}

func New(rt *runtime.Session, out io.Writer) *Server {
	s := &Server{
		rt:              rt,
		out:             out,
		threads:         make(map[string]*threadState),
		pendingRequests: make(map[string]chan clientResponse),

		pendingAgentCompletionTurns:  make(map[string][]providers.ChatMessage),
		drainingAgentCompletionTurns: make(map[string]bool),
	}
	if rt != nil && rt.Toolkit != nil {
		rt.Toolkit.SetAskUserBridge(s)
		rt.AskBridge = s
	}
	return s
}

func (s *Server) forwardAgentNotifications(threadID string, control *agentcontrol.AgentControl, ch <-chan subagent.Notification) {
	for n := range ch {
		now := time.Now().UTC()
		if n.Status == subagent.StatusRunning {
			_, turn, started := s.ensureLiveAgentThread(threadID, control, n.Snapshot, now)
			if started {
				_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
					ThreadID: n.Snapshot.ID,
					Turn:     turn,
				})
			}
		}
		_ = s.writeNotification(NotificationAgentUpdated, AgentUpdatedNotification{
			ThreadID: threadID,
			Agent:    agentFromSnapshot(n.Snapshot),
		})
		switch n.Status {
		case subagent.StatusCompleted, subagent.StatusFailed, subagent.StatusCancelled:
			s.completeLiveAgentThread(threadID, control, n.Snapshot, now)
			if s.isRootAgentSnapshot(control, threadID, n.Snapshot) {
				_ = s.writeNotification(NotificationAgentMailbox, AgentMailboxNotification{
					ThreadID: threadID,
					Message:  agentcontrol.NewAgentMailboxMessage(n.Snapshot),
				})
				if control != nil {
					s.enqueueAgentCompletionTurn(threadID, control.AgentCompletionChatMessage(n.Snapshot, agentthread.RootPath))
				}
			}
		}
	}
}

func (s *Server) forwardAgentStreamNotifications(threadID string, control *agentcontrol.AgentControl, ch <-chan subagent.StreamNotification) {
	for n := range ch {
		now := time.Now().UTC()
		th, turn, started := s.ensureLiveAgentThread(threadID, control, n.Snapshot, now)
		if th == nil {
			continue
		}
		if started {
			_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
				ThreadID: th.ID,
				Turn:     turn,
			})
		}
		th.mu.Lock()
		batch := th.applyStreamEventLocked(turn.ID, n.Event, now)
		th.mu.Unlock()
		for _, item := range batch {
			_ = s.writeNotification(item.method, item.params)
		}
		_ = s.writeNotification(NotificationTurnEvent, TurnEventNotification{
			ThreadID: th.ID,
			TurnID:   turn.ID,
			Event:    sanitizeStreamEvent(n.Event),
		})
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

func isRunningSubAgentStatus(status subagent.Status) bool {
	switch status {
	case subagent.StatusRunning, subagent.StatusPending, subagent.StatusQueued:
		return true
	default:
		return false
	}
}

func (s *Server) ensureLiveAgentThread(rootThreadID string, control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot, now time.Time) (*threadState, Turn, bool) {
	if strings.TrimSpace(snap.ID) == "" {
		return nil, Turn{}, false
	}
	th, created := s.ensureAgentThreadState(rootThreadID, control, snap, now)
	if th == nil {
		return nil, Turn{}, false
	}
	th.mu.Lock()
	s.applyAgentSnapshotLocked(th, rootThreadID, snap, now)
	turn, started := th.startAgentTurnLocked(now)
	if created {
		started = true
	}
	th.mu.Unlock()
	return th, turn, started
}

func (s *Server) ensureAgentThreadState(rootThreadID string, control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot, now time.Time) (*threadState, bool) {
	if th := s.thread(snap.ID); th != nil {
		return th, false
	}

	history := s.agentSnapshotHistory(control, snap)
	th := newThreadState(snap.ID, history, s.rt.ProviderName, s.rt.Model, firstNonEmpty(snapCWD(rootThreadID, s), s.rt.RootDir), "", now)
	th.ParentID = strings.TrimSpace(rootThreadID)
	th.AgentPath = snap.AgentPath
	th.ReadOnly = true
	th.CreatedAt = firstNonZeroTime(snap.StartedAt, now)
	th.UpdatedAt = firstNonZeroTime(snap.ActivityAt, snap.CompletedAt, snap.StartedAt, now)

	s.mu.Lock()
	if existing := s.threads[snap.ID]; existing != nil {
		s.mu.Unlock()
		return existing, false
	}
	s.threads[snap.ID] = th
	s.mu.Unlock()
	return th, true
}

func (s *Server) agentSnapshotHistory(control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot) []providers.ChatMessage {
	if control != nil && control.Manager() != nil {
		if history, ok := control.Manager().History(snap.ID); ok && len(history) > 0 {
			return history
		}
	}
	history := make([]providers.ChatMessage, 0, 2)
	if strings.TrimSpace(snap.Description) != "" {
		history = append(history, providers.ChatMessage{Role: "user", Content: snap.Description})
	}
	if strings.TrimSpace(snap.Result) != "" {
		history = append(history, providers.ChatMessage{Role: "assistant", Content: snap.Result})
	} else if snap.Error != nil {
		history = append(history, providers.ChatMessage{Role: "assistant", Content: "Worker failed: " + snap.Error.Error()})
	}
	return history
}

func (s *Server) applyAgentSnapshotLocked(th *threadState, rootThreadID string, snap subagent.SubAgentSnapshot, now time.Time) {
	th.ParentID = strings.TrimSpace(rootThreadID)
	th.AgentPath = snap.AgentPath
	th.ReadOnly = true
	if !snap.StartedAt.IsZero() {
		th.CreatedAt = snap.StartedAt
	}
	switch snap.Status {
	case subagent.StatusRunning, subagent.StatusPending, subagent.StatusQueued:
		th.UpdatedAt = now
	case subagent.StatusCompleted, subagent.StatusFailed, subagent.StatusCancelled:
		th.UpdatedAt = firstNonZeroTime(snap.CompletedAt, now)
	default:
		th.UpdatedAt = now
	}
}

func (s *Server) completeLiveAgentThread(rootThreadID string, control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot, now time.Time) {
	th, turn, status, turnErr := s.syncFinalAgentThread(rootThreadID, control, snap, now)
	if th == nil {
		return
	}
	if status == TurnStatusFailed || status == TurnStatusInterrupted {
		message := ""
		if turnErr != nil {
			message = turnErr.Error()
		}
		_ = s.writeNotification(NotificationTurnError, TurnErrorNotification{
			ThreadID: th.ID,
			TurnID:   turn.ID,
			Error:    message,
			Turn:     turn,
		})
		return
	}
	_ = s.writeNotification(NotificationTurnCompleted, TurnCompletedNotification{
		ThreadID: th.ID,
		Turn:     turn,
		Content:  snap.Result,
	})
}

func (s *Server) syncFinalAgentThread(rootThreadID string, control *agentcontrol.AgentControl, snap subagent.SubAgentSnapshot, now time.Time) (*threadState, Turn, TurnStatus, error) {
	th, _ := s.ensureAgentThreadState(rootThreadID, control, snap, now)
	if th == nil {
		return nil, Turn{}, "", nil
	}
	history := s.agentSnapshotHistory(control, snap)
	status, turnErr := turnStatusForSubAgentSnapshot(snap)

	th.mu.Lock()
	s.applyAgentSnapshotLocked(th, rootThreadID, snap, now)
	if len(history) > 0 {
		th.History = history
	}
	turnID := th.currentTurn
	if turnID == "" && len(th.Turns) > 0 {
		turnID = th.Turns[len(th.Turns)-1].ID
	}
	if turnID == "" {
		turn, _ := th.startAgentTurnLocked(now)
		turnID = turn.ID
	}
	turn := th.completeTurnLocked(turnID, status, turnErr, now)
	th.mu.Unlock()
	return th, turn, status, turnErr
}

func turnStatusForSubAgentSnapshot(snap subagent.SubAgentSnapshot) (TurnStatus, error) {
	switch snap.Status {
	case subagent.StatusFailed:
		if snap.Error != nil {
			return TurnStatusFailed, snap.Error
		}
		return TurnStatusFailed, errors.New("worker failed")
	case subagent.StatusCancelled:
		return TurnStatusInterrupted, context.Canceled
	default:
		return TurnStatusCompleted, nil
	}
}

func snapCWD(rootThreadID string, s *Server) string {
	if s == nil {
		return ""
	}
	if root := s.thread(rootThreadID); root != nil {
		root.mu.Lock()
		defer root.mu.Unlock()
		return root.CWD
	}
	return ""
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
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
	case MethodThreadFork:
		return s.handleThreadFork(req)
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
	var providerCfg config.ProviderConfig
	var resolvedName string
	creatingProvider := params.CreateProvider
	if creatingProvider {
		if providerName == "" {
			return s.writeResponse(req.ID, nil, errors.New("provider is required"))
		}
		if _, _, err := cfg.ResolveProvider(providerName); err == nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("provider %q already exists", providerName))
		}
		baseURL := strings.TrimSpace(stringValue(params.BaseURL))
		apiKey := strings.TrimSpace(stringValue(params.APIKey))
		if baseURL == "" {
			return s.writeResponse(req.ID, nil, errors.New("base_url is required"))
		}
		if apiKey == "" {
			return s.writeResponse(req.ID, nil, errors.New("api_key is required"))
		}
		providerCfg = config.ProviderConfig{
			Type:    "openai-compatible",
			BaseURL: baseURL,
			APIKey:  apiKey,
			Model:   model,
		}
		resolvedName = providerName
	} else {
		providerCfg, resolvedName, err = cfg.ResolveProvider(providerName)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	providerCfg.Model = model
	connectionChanged := creatingProvider
	connectionLocked := isCodexProviderType(providerCfg.Type)
	if connectionLocked && (params.BaseURL != nil || strings.TrimSpace(stringValue(params.APIKey)) != "") {
		return s.writeResponse(req.ID, nil, errors.New("connection settings are managed by OpenAI OAuth for this provider"))
	}
	if params.BaseURL != nil {
		baseURL := strings.TrimSpace(*params.BaseURL)
		if baseURL == "" {
			return s.writeResponse(req.ID, nil, errors.New("base_url is required"))
		}
		if baseURL != strings.TrimSpace(providerCfg.BaseURL) {
			connectionChanged = true
		}
		providerCfg.BaseURL = baseURL
	}
	apiKeyForConfig := params.APIKey
	authKeyForStore := ""
	if params.APIKey != nil {
		apiKey := strings.TrimSpace(*params.APIKey)
		if apiKey != "" {
			connectionChanged = true
			authKeyForStore = apiKey
			providerCfg.APIKey = apiKey
			providerCfg.APIKeyEnv = ""
			empty := ""
			apiKeyForConfig = &empty
		} else {
			apiKeyForConfig = nil
		}
	}
	effort := s.currentEffort()
	if params.Effort != nil {
		effort = strings.TrimSpace(*params.Effort)
	}

	var client providers.StreamClient
	if resolvedName != s.rt.ProviderName || connectionChanged {
		client, err = providerfactory.BuildStreamClient(providerCfg, resolvedName)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	if authKeyForStore != "" {
		if err := config.SaveAuthKey(os.Getenv("HOME"), resolvedName, authKeyForStore); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	if creatingProvider {
		err = config.CreateProviderRuntime(s.rt.ConfigPath, resolvedName, model, params.BaseURL, apiKeyForConfig, params.Effort)
	} else {
		err = config.UpdateProviderRuntime(s.rt.ConfigPath, resolvedName, model, params.BaseURL, apiKeyForConfig, params.Effort)
	}
	if err != nil {
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

type forkSourceThread struct {
	history       []providers.ChatMessage
	modelProvider string
	model         string
	cwd           string
	thread        Thread
}

func (s *Server) handleThreadFork(req Request) error {
	var params ThreadForkParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	sourceID := strings.TrimSpace(params.ThreadID)
	if sourceID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}

	now := time.Now().UTC()
	source, err := s.loadForkSourceThread(sourceID, now)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	history, err := forkHistoryAtTarget(source.history, source.thread.ID, source.thread.Turns, params.TurnID, params.ItemID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	id := session.NewID()
	fork := session.ForkMetadata{
		ForkedFromID:     source.thread.ID,
		ForkedFromTurnID: strings.TrimSpace(params.TurnID),
		ForkedFromItemID: strings.TrimSpace(params.ItemID),
	}
	sess, err := session.CreateForkWithMetadata(s.rt.SessionDir, id, source.cwd, fork)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	path := session.FilePath(s.rt.SessionDir, sess.ID)
	if err := rewriteChatHistory(path, history); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if err := session.UpdateIndex(s.rt.SessionDir, sess.ID, persistableMessageCount(history), threadPreview(history)); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	th := newThreadState(sess.ID, history, source.modelProvider, source.model, source.cwd, path, now)
	applySessionMetadata(th, *sess)
	s.mu.Lock()
	s.threads[th.ID] = th
	s.mu.Unlock()

	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	if err := s.writeResponse(req.ID, ThreadForkResult{Thread: thread}, nil); err != nil {
		return err
	}
	return s.writeNotification(NotificationThreadStarted, ThreadStartedNotification{
		Thread: thread,
	})
}

func (s *Server) loadForkSourceThread(id string, now time.Time) (forkSourceThread, error) {
	if th := s.thread(id); th != nil {
		th.mu.Lock()
		defer th.mu.Unlock()
		return forkSourceThread{
			history:       cloneHistory(th.History),
			modelProvider: th.ModelProvider,
			model:         th.Model,
			cwd:           th.CWD,
			thread:        th.snapshotLocked(),
		}, nil
	}

	path, err := session.Load(s.rt.SessionDir, id)
	if err != nil {
		return forkSourceThread{}, err
	}
	history, err := loadChatMessages(path)
	if err != nil {
		return forkSourceThread{}, err
	}
	normalized, err := providers.NormalizeAndValidateMessages(history)
	if err != nil {
		return forkSourceThread{}, err
	}
	if !reflect.DeepEqual(normalized, history) {
		if err := rewriteChatHistory(path, normalized); err != nil {
			return forkSourceThread{}, err
		}
	}
	history = ensureBaseSystemPrompt(normalized, s.rt.StreamRunner.SystemPrompt)
	th := newThreadState(id, history, s.rt.ProviderName, s.rt.Model, s.rt.RootDir, path, now)
	if metadata, ok, err := session.Find(s.rt.SessionDir, id); err != nil {
		return forkSourceThread{}, err
	} else if ok {
		applySessionMetadata(th, metadata)
	}
	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	return forkSourceThread{
		history:       cloneHistory(th.History),
		modelProvider: th.ModelProvider,
		model:         th.Model,
		cwd:           th.CWD,
		thread:        thread,
	}, nil
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
		if thread.ReadOnly {
			continue
		}
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
	if !metadata.UpdatedAt.IsZero() {
		th.UpdatedAt = metadata.UpdatedAt
	} else if !metadata.CreatedAt.IsZero() {
		th.UpdatedAt = metadata.CreatedAt
	}
	if strings.TrimSpace(metadata.CWD) != "" {
		th.CWD = metadata.CWD
	}
	th.ForkedFromID = metadata.ForkedFromID
	th.ForkedFromTurnID = metadata.ForkedFromTurnID
	th.ForkedFromItemID = metadata.ForkedFromItemID
	th.PinnedAt = metadata.PinnedAt
	th.ArchivedAt = metadata.ArchivedAt
}

func threadEntryFromSession(sess session.Session, provider, model string) threadListEntry {
	updatedAt := sess.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = sess.CreatedAt
	}
	return threadListEntry{
		thread: Thread{
			ID:               sess.ID,
			Preview:          sess.Summary,
			ModelProvider:    provider,
			Model:            model,
			CWD:              sess.CWD,
			Status:           ThreadStatusIdle,
			Pinned:           sess.PinnedAt != nil,
			Archived:         sess.ArchivedAt != nil,
			ForkedFromID:     sess.ForkedFromID,
			ForkedFromTurnID: sess.ForkedFromTurnID,
			ForkedFromItemID: sess.ForkedFromItemID,
			CreatedAt:        sess.CreatedAt,
			UpdatedAt:        updatedAt,
			Turns:            []Turn{},
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
	if control := s.liveAgentControl(rootID); control != nil && control.Manager() != nil {
		if sa := control.Manager().Get(meta.ID); sa != nil {
			snap := sa.Snapshot()
			var th *threadState
			if isRunningSubAgentStatus(snap.Status) {
				th, _, _ = s.ensureLiveAgentThread(rootID, control, snap, time.Now().UTC())
			} else {
				th, _, _, _ = s.syncFinalAgentThread(rootID, control, snap, time.Now().UTC())
			}
			if th != nil {
				th.mu.Lock()
				thread := th.snapshotLocked()
				th.mu.Unlock()
				return thread, true, nil
			}
		}
	}
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
		readOnly := th.ReadOnly
		th.mu.Unlock()
		if !readOnly && cwd == s.rt.RootDir {
			add(id)
		}
	}
	s.mu.Unlock()
	return ids, nil
}

func (s *Server) liveAgentHistory(rootID, agentID string) ([]providers.ChatMessage, bool) {
	control := s.liveAgentControl(rootID)
	if control == nil || control.Manager() == nil {
		return nil, false
	}
	return control.Manager().History(agentID)
}

func (s *Server) liveAgentControl(rootID string) *agentcontrol.AgentControl {
	th := s.thread(rootID)
	if th == nil {
		return nil
	}
	th.mu.Lock()
	threadRuntime := th.execRuntime
	th.mu.Unlock()
	if threadRuntime == nil {
		return nil
	}
	return threadRuntime.AgentControl
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
		leftTime := entries[i].thread.UpdatedAt
		if leftTime.IsZero() {
			leftTime = entries[i].thread.CreatedAt
		}
		rightTime := entries[j].thread.UpdatedAt
		if rightTime.IsZero() {
			rightTime = entries[j].thread.CreatedAt
		}
		if !leftTime.Equal(rightTime) {
			return leftTime.After(rightTime)
		}
		return entries[i].thread.ID > entries[j].thread.ID
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
	return providerSummariesFromConfig(cfg, os.Getenv("HOME"))
}

func providerSummariesFromConfig(cfg config.Config, home string) []ProviderSummary {
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ProviderSummary, 0, len(names))
	for _, name := range names {
		provider := cfg.Providers[name]
		out = append(out, ProviderSummary{
			Name:             name,
			Type:             provider.Type,
			Model:            provider.Model,
			BaseURL:          provider.BaseURL,
			APIKeyConfigured: providerHasAuth(name, provider, home),
			ConnectionLocked: isCodexProviderType(provider.Type),
			Models:           providerModelSummaries(provider),
		})
	}
	return out
}

func providerHasAuth(name string, provider config.ProviderConfig, home string) bool {
	if provider.APIKey != "" || provider.APIKeyEnv != "" || provider.AuthToken != "" || provider.AuthTokenEnv != "" {
		return true
	}
	key, err := config.LoadAuthKey(home, name)
	return err == nil && strings.TrimSpace(key) != ""
}

func providerModelSummaries(provider config.ProviderConfig) []ProviderModelSummary {
	models := make(map[string]ProviderModelSummary, len(provider.Models)+1)
	for id, model := range provider.Models {
		id = strings.TrimSpace(id)
		if id == "" || model.Disabled {
			continue
		}
		models[id] = ProviderModelSummary{
			ID:               id,
			DisplayName:      strings.TrimSpace(model.Name),
			DefaultEffort:    strings.TrimSpace(model.DefaultEffort),
			DefaultVariant:   strings.TrimSpace(model.DefaultVariant),
			SupportedEfforts: normalizedEffortList(model.SupportedEfforts),
			Variants:         providerVariantSummaries(model.Variants),
			Source:           "config",
		}
	}
	current := strings.TrimSpace(provider.Model)
	if current != "" {
		if _, ok := models[current]; !ok {
			models[current] = ProviderModelSummary{
				ID:               current,
				SupportedEfforts: inferredEfforts(provider, current),
				Variants:         inferredVariantSummaries(provider, current),
				Source:           "selected",
			}
		}
	}
	out := make([]ProviderModelSummary, 0, len(models))
	for _, model := range models {
		if len(model.Variants) == 0 {
			model.Variants = inferredVariantSummaries(provider, model.ID)
		}
		if len(model.SupportedEfforts) == 0 {
			model.SupportedEfforts = effortIDsFromVariants(model.Variants)
			if len(model.SupportedEfforts) == 0 {
				model.SupportedEfforts = inferredEfforts(provider, model.ID)
			}
		}
		if model.DefaultVariant == "" && model.DefaultEffort != "" {
			model.DefaultVariant = model.DefaultEffort
		}
		out = append(out, model)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == current {
			return true
		}
		if out[j].ID == current {
			return false
		}
		return strings.ToLower(out[i].ID) < strings.ToLower(out[j].ID)
	})
	return out
}

func providerVariantSummaries(variants map[string]map[string]any) []ProviderModelVariantSummary {
	if len(variants) == 0 {
		return nil
	}
	out := make([]ProviderModelVariantSummary, 0, len(variants))
	for id, options := range variants {
		id = strings.TrimSpace(id)
		if id == "" || variantDisabled(options) {
			continue
		}
		out = append(out, ProviderModelVariantSummary{
			ID:      id,
			Options: cloneVariantOptions(options),
		})
	}
	sortVariantSummaries(out)
	return out
}

func inferredVariantSummaries(provider config.ProviderConfig, model string) []ProviderModelVariantSummary {
	return providerVariantSummaries(inferredVariantOptions(provider, model))
}

func cloneVariantOptions(options map[string]any) map[string]any {
	if len(options) == 0 {
		return nil
	}
	out := make(map[string]any, len(options))
	for key, value := range options {
		if key == "disabled" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func variantDisabled(options map[string]any) bool {
	if len(options) == 0 {
		return false
	}
	disabled, ok := options["disabled"].(bool)
	return ok && disabled
}

func sortVariantSummaries(variants []ProviderModelVariantSummary) {
	sort.Slice(variants, func(i, j int) bool {
		leftRank := variantRank(variants[i].ID)
		rightRank := variantRank(variants[j].ID)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return strings.ToLower(variants[i].ID) < strings.ToLower(variants[j].ID)
	})
}

func variantRank(id string) int {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "none":
		return 0
	case "minimal":
		return 1
	case "low":
		return 2
	case "medium":
		return 3
	case "high":
		return 4
	case "xhigh":
		return 5
	case "max":
		return 6
	default:
		return 100
	}
}

func effortIDsFromVariants(variants []ProviderModelVariantSummary) []string {
	if len(variants) == 0 {
		return nil
	}
	out := make([]string, 0, len(variants))
	for _, variant := range variants {
		id := strings.TrimSpace(variant.ID)
		if id == "" || id == "default" {
			continue
		}
		out = append(out, id)
	}
	return out
}

func normalizedEffortList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		effort := strings.TrimSpace(value)
		if effort == "" || seen[effort] {
			continue
		}
		seen[effort] = true
		out = append(out, effort)
	}
	return out
}

func inferredVariantOptions(provider config.ProviderConfig, model string) map[string]map[string]any {
	modelID := strings.ToLower(strings.TrimSpace(model))
	if modelID == "" {
		return nil
	}
	if excludedOpenCodeReasoningVariantModel(modelID) {
		return nil
	}
	providerType := strings.ToLower(strings.TrimSpace(provider.Type))
	providerType = strings.ReplaceAll(providerType, "_", "-")
	baseURL := strings.ToLower(strings.TrimSpace(provider.BaseURL))

	if isCodexProviderType(provider.Type) {
		return variantsWithReasoningEffort(openAICodexEfforts(modelID))
	}
	if providerType == "anthropic" || providerType == "claude" || providerType == "anthropic-official" {
		return anthropicVariantOptions(modelID)
	}
	if strings.Contains(baseURL, "openrouter.ai") {
		if strings.Contains(modelID, "gpt") {
			return variantsWithNestedReasoningEffort(openAICompatibleEfforts(modelID))
		}
		if strings.Contains(modelID, "gemini-3") || strings.Contains(modelID, "claude") {
			return variantsWithNestedReasoningEffort(openAIEfforts())
		}
		return nil
	}
	if providerType == "openai" || providerType == "openai-compatible" || providerType == "codex" {
		if strings.Contains(modelID, "mimo-v2.5-pro") || strings.Contains(baseURL, "xiaomimimo.com") {
			return variantsWithReasoningEffort(widelySupportedEfforts())
		}
		if strings.Contains(modelID, "deepseek-v4") {
			return variantsWithReasoningEffort(append(widelySupportedEfforts(), "max"))
		}
		if strings.Contains(modelID, "grok-3-mini") {
			return variantsWithReasoningEffort([]string{"low", "high"})
		}
		if strings.Contains(modelID, "gpt") || openAIOSeriesModel(modelID) {
			return variantsWithReasoningEffort(openAICompatibleEfforts(modelID))
		}
	}
	return nil
}

func excludedOpenCodeReasoningVariantModel(modelID string) bool {
	excluded := []string{
		"deepseek-chat",
		"deepseek-reasoner",
		"deepseek-r1",
		"deepseek-v3",
		"minimax",
		"glm",
		"kimi",
		"k2p",
		"qwen",
		"big-pickle",
	}
	for _, item := range excluded {
		if strings.Contains(modelID, item) {
			return true
		}
	}
	if strings.Contains(modelID, "grok") && !strings.Contains(modelID, "grok-3-mini") {
		return true
	}
	return false
}

func widelySupportedEfforts() []string {
	return []string{"low", "medium", "high"}
}

func openAIEfforts() []string {
	return []string{"none", "minimal", "low", "medium", "high", "xhigh"}
}

func openAICompatibleEfforts(modelID string) []string {
	if strings.Contains(modelID, "gpt-5-pro") || strings.Contains(modelID, "gpt-5pro") {
		return []string{"high"}
	}
	if strings.Contains(modelID, "gpt-5.2-pro") || strings.Contains(modelID, "gpt-5.3-pro") ||
		strings.Contains(modelID, "gpt-5.4-pro") || strings.Contains(modelID, "gpt-5.5-pro") {
		return []string{"medium", "high", "xhigh"}
	}
	if strings.Contains(modelID, "gpt-5.1-chat") || strings.Contains(modelID, "gpt-5.2-chat") ||
		strings.Contains(modelID, "gpt-5.3-chat") || strings.Contains(modelID, "gpt-5-chat") {
		return []string{"medium"}
	}
	if strings.Contains(modelID, "gpt-5.1") {
		return []string{"none", "low", "medium", "high"}
	}
	if strings.Contains(modelID, "gpt-5.2") || strings.Contains(modelID, "gpt-5.3") ||
		strings.Contains(modelID, "gpt-5.4") || strings.Contains(modelID, "gpt-5.5") {
		return []string{"none", "low", "medium", "high", "xhigh"}
	}
	return openAIEfforts()
}

func openAICodexEfforts(modelID string) []string {
	if strings.Contains(modelID, "gpt-5.3-codex") || strings.Contains(modelID, "gpt-5.4-codex") ||
		strings.Contains(modelID, "gpt-5.5-codex") {
		return []string{"none", "low", "medium", "high", "xhigh"}
	}
	if strings.Contains(modelID, "gpt-5.2-codex") || strings.Contains(modelID, "codex-max") {
		return []string{"low", "medium", "high", "xhigh"}
	}
	if strings.Contains(modelID, "gpt-5") || strings.Contains(modelID, "codex") {
		return widelySupportedEfforts()
	}
	return inferredEfforts(config.ProviderConfig{Type: "openai-codex"}, modelID)
}

func openAIOSeriesModel(modelID string) bool {
	return strings.Contains(modelID, "o1") || strings.Contains(modelID, "o3") || strings.Contains(modelID, "o4")
}

func variantsWithReasoningEffort(efforts []string) map[string]map[string]any {
	return variantsFromEfforts(efforts, func(effort string) map[string]any {
		return map[string]any{"reasoningEffort": effort}
	})
}

func variantsWithNestedReasoningEffort(efforts []string) map[string]map[string]any {
	return variantsFromEfforts(efforts, func(effort string) map[string]any {
		return map[string]any{"reasoning": map[string]any{"effort": effort}}
	})
}

func variantsFromEfforts(efforts []string, build func(string) map[string]any) map[string]map[string]any {
	if len(efforts) == 0 {
		return nil
	}
	out := make(map[string]map[string]any, len(efforts))
	for _, effort := range efforts {
		effort = strings.TrimSpace(effort)
		if effort == "" {
			continue
		}
		out[effort] = build(effort)
	}
	return out
}

func anthropicVariantOptions(modelID string) map[string]map[string]any {
	if strings.Contains(modelID, "opus-4-7") || strings.Contains(modelID, "opus-4.7") {
		return variantsFromEfforts([]string{"low", "medium", "high", "xhigh", "max"}, func(effort string) map[string]any {
			return map[string]any{
				"thinking": map[string]any{"type": "adaptive", "display": "summarized"},
				"effort":   effort,
			}
		})
	}
	if strings.Contains(modelID, "opus-4-6") || strings.Contains(modelID, "opus-4.6") ||
		strings.Contains(modelID, "sonnet-4-6") || strings.Contains(modelID, "sonnet-4.6") {
		return variantsFromEfforts([]string{"low", "medium", "high", "max"}, func(effort string) map[string]any {
			return map[string]any{
				"thinking": map[string]any{"type": "adaptive"},
				"effort":   effort,
			}
		})
	}
	if strings.Contains(modelID, "opus-4-5") || strings.Contains(modelID, "opus-4.5") {
		return variantsFromEfforts(widelySupportedEfforts(), func(effort string) map[string]any {
			return map[string]any{"effort": effort}
		})
	}
	return map[string]map[string]any{
		"high": {"thinking": map[string]any{"type": "enabled", "budgetTokens": 16000}},
		"max":  {"thinking": map[string]any{"type": "enabled", "budgetTokens": 31999}},
	}
}

func inferredEfforts(provider config.ProviderConfig, model string) []string {
	modelID := strings.ToLower(strings.TrimSpace(model))
	if modelID == "" {
		return nil
	}
	providerType := strings.ToLower(strings.TrimSpace(provider.Type))
	providerType = strings.ReplaceAll(providerType, "_", "-")
	baseURL := strings.ToLower(strings.TrimSpace(provider.BaseURL))
	if isCodexProviderType(provider.Type) {
		return []string{"low", "medium", "high", "xhigh"}
	}
	if providerType == "anthropic" || providerType == "claude" || providerType == "anthropic-official" {
		if strings.Contains(modelID, "claude") && (strings.Contains(modelID, "sonnet-4") || strings.Contains(modelID, "opus-4")) {
			return []string{"low", "medium", "high", "max"}
		}
		return nil
	}
	if strings.Contains(baseURL, "openrouter.ai") {
		if strings.Contains(modelID, "gpt") || strings.Contains(modelID, "claude") || strings.Contains(modelID, "gemini-3") {
			return []string{"low", "medium", "high"}
		}
		return nil
	}
	if providerType == "openai" || providerType == "openai-compatible" || providerType == "codex" {
		if strings.Contains(modelID, "gpt-5") || strings.Contains(modelID, "o1") || strings.Contains(modelID, "o3") || strings.Contains(modelID, "o4") {
			return []string{"low", "medium", "high"}
		}
	}
	return nil
}

func isCodexProviderType(providerType string) bool {
	s := strings.ToLower(strings.TrimSpace(providerType))
	s = strings.ReplaceAll(s, "_", "-")
	return s == "openai-codex" || s == "codex-subscription" || s == "chatgpt-codex"
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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
	history := cloneHistory(th.History)
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
	if threadRuntime.Toolkit != nil {
		if _, restoreErr := threadRuntime.Toolkit.RestorePlanFromHistory(history); restoreErr != nil {
			providers.DebugLogf("restore update_plan for thread %q: %v", th.ID, restoreErr)
		}
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

	streamCh := make(chan subagent.StreamNotification, 256)
	threadRuntime.AgentControl.SubscribeStream(streamCh)
	go s.forwardAgentStreamNotifications(threadID, threadRuntime.AgentControl, streamCh)
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
		s.kickAgentCompletionDrain(th.ID)
		return
	}
	notify(NotificationTurnCompleted, TurnCompletedNotification{
		ThreadID:     th.ID,
		Turn:         turn,
		Content:      res.Content,
		InputTokens:  res.InputTokens,
		OutputTokens: res.OutputTokens,
	})
	s.kickAgentCompletionDrain(th.ID)
}

func (s *Server) enqueueAgentCompletionTurn(threadID string, msg providers.ChatMessage) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || (strings.TrimSpace(msg.Content) == "" && len(msg.Images) == 0) {
		return
	}
	th := s.thread(threadID)
	if th == nil || !canResumeAgentCompletionThread(th) {
		return
	}
	if strings.TrimSpace(msg.Role) == "" {
		msg.Role = "user"
	}

	s.agentCompletionMu.Lock()
	if s.pendingAgentCompletionTurns == nil {
		s.pendingAgentCompletionTurns = make(map[string][]providers.ChatMessage)
	}
	s.pendingAgentCompletionTurns[threadID] = append(s.pendingAgentCompletionTurns[threadID], msg)
	s.agentCompletionMu.Unlock()

	s.kickAgentCompletionDrain(threadID)
}

func (s *Server) kickAgentCompletionDrain(threadID string) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}

	s.agentCompletionMu.Lock()
	if len(s.pendingAgentCompletionTurns[threadID]) == 0 || s.drainingAgentCompletionTurns[threadID] {
		s.agentCompletionMu.Unlock()
		return
	}
	if s.drainingAgentCompletionTurns == nil {
		s.drainingAgentCompletionTurns = make(map[string]bool)
	}
	s.drainingAgentCompletionTurns[threadID] = true
	s.agentCompletionMu.Unlock()

	go s.drainAgentCompletionTurns(threadID)
}

func (s *Server) drainAgentCompletionTurns(threadID string) {
	th := s.thread(threadID)
	if th == nil || !canResumeAgentCompletionThread(th) {
		s.discardPendingAgentCompletionTurns(threadID)
		s.clearAgentCompletionDrain(threadID)
		return
	}
	if threadIsRunning(th) {
		s.clearAgentCompletionDrain(threadID)
		return
	}

	pending := s.takePendingAgentCompletionTurns(threadID)
	if len(pending) == 0 {
		s.clearAgentCompletionDrain(threadID)
		return
	}

	started, err := s.startSyntheticTurn(context.Background(), threadID, combineAgentCompletionMessages(pending))
	if err != nil {
		providers.DebugLogf("start agent completion turn for thread %q: %v", threadID, err)
	}
	requeued := false
	if !started && err == nil {
		s.prependPendingAgentCompletionTurns(threadID, pending)
		requeued = true
	}
	s.clearAgentCompletionDrain(threadID)
	if requeued {
		s.kickAgentCompletionDrain(threadID)
	}
}

func (s *Server) startSyntheticTurn(ctx context.Context, threadID string, userMsg providers.ChatMessage) (bool, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return false, errors.New("thread_id is required")
	}
	if strings.TrimSpace(userMsg.Role) == "" {
		userMsg.Role = "user"
	}
	if strings.TrimSpace(userMsg.Content) == "" && len(userMsg.Images) == 0 {
		return false, nil
	}

	th := s.thread(threadID)
	if th == nil {
		return false, fmt.Errorf("thread %q not found", threadID)
	}
	if !canResumeAgentCompletionThread(th) {
		return false, nil
	}
	threadRuntime, err := s.ensureThreadRuntime(th)
	if err != nil {
		return false, err
	}

	turnID := session.NewID()
	turnCtx, cancel := context.WithCancel(ctx)
	turnCtx = withAskUserThreadID(turnCtx, th.ID)
	now := time.Now().UTC()

	th.mu.Lock()
	if th.running {
		th.mu.Unlock()
		cancel()
		return false, nil
	}
	if th.ReadOnly {
		th.mu.Unlock()
		cancel()
		return false, nil
	}
	if err := appendChatMessage(th.MemoryPath, userMsg); err != nil {
		th.mu.Unlock()
		cancel()
		return false, err
	}
	history := append([]providers.ChatMessage(nil), th.History...)
	history = append(history, userMsg)
	th.History = history
	th.cancel = cancel
	turn := th.startTurnLocked(turnID, userMsg, now)
	th.mu.Unlock()

	_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
		ThreadID: threadID,
		Turn:     turn,
	})
	go s.runTurn(turnCtx, th, threadRuntime, turnID, history)
	return true, nil
}

func canResumeAgentCompletionThread(th *threadState) bool {
	if th == nil {
		return false
	}
	th.mu.Lock()
	defer th.mu.Unlock()
	return !th.ReadOnly
}

func threadIsRunning(th *threadState) bool {
	if th == nil {
		return false
	}
	th.mu.Lock()
	defer th.mu.Unlock()
	return th.running
}

func (s *Server) takePendingAgentCompletionTurns(threadID string) []providers.ChatMessage {
	s.agentCompletionMu.Lock()
	defer s.agentCompletionMu.Unlock()
	pending := append([]providers.ChatMessage(nil), s.pendingAgentCompletionTurns[threadID]...)
	delete(s.pendingAgentCompletionTurns, threadID)
	return pending
}

func (s *Server) prependPendingAgentCompletionTurns(threadID string, msgs []providers.ChatMessage) {
	if len(msgs) == 0 {
		return
	}
	s.agentCompletionMu.Lock()
	defer s.agentCompletionMu.Unlock()
	if s.pendingAgentCompletionTurns == nil {
		s.pendingAgentCompletionTurns = make(map[string][]providers.ChatMessage)
	}
	existing := append([]providers.ChatMessage(nil), s.pendingAgentCompletionTurns[threadID]...)
	s.pendingAgentCompletionTurns[threadID] = append(append([]providers.ChatMessage(nil), msgs...), existing...)
}

func (s *Server) discardPendingAgentCompletionTurns(threadID string) {
	s.agentCompletionMu.Lock()
	defer s.agentCompletionMu.Unlock()
	delete(s.pendingAgentCompletionTurns, threadID)
}

func (s *Server) clearAgentCompletionDrain(threadID string) {
	s.agentCompletionMu.Lock()
	defer s.agentCompletionMu.Unlock()
	delete(s.drainingAgentCompletionTurns, threadID)
}

func combineAgentCompletionMessages(msgs []providers.ChatMessage) providers.ChatMessage {
	if len(msgs) == 0 {
		return providers.ChatMessage{Role: "user"}
	}
	if len(msgs) == 1 {
		return msgs[0]
	}
	contents := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		if content := strings.TrimSpace(msg.Content); content != "" {
			contents = append(contents, content)
		}
	}
	return providers.ChatMessage{
		Role:    "user",
		Content: strings.Join(contents, "\n\n"),
	}
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
	if ev.PlanUpdate != nil {
		out.PlanUpdate = ev.PlanUpdate
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
