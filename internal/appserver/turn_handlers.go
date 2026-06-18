package appserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/guardian"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/sessiontrace"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

type queuedTurn struct {
	id  string
	msg providers.ChatMessage
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
	files, err := normalizeTurnStartFiles(params.Files)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if params.Prompt == "" && len(images) == 0 && len(files) == 0 {
		return s.writeResponse(req.ID, nil, errors.New("prompt or attachment is required"))
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
	userMsg := providers.ChatMessage{Role: "user", Content: params.Prompt, Images: images, Files: files}
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
	history := cloneHistory(th.History)
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

func (s *Server) handleTurnQueue(req Request) error {
	var params TurnQueueParams
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
	files, err := normalizeTurnStartFiles(params.Files)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if params.Prompt == "" && len(images) == 0 && len(files) == 0 {
		return s.writeResponse(req.ID, nil, errors.New("prompt or attachment is required"))
	}
	th := s.thread(params.ThreadID)
	if th == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q not found", params.ThreadID))
	}
	th.mu.Lock()
	readOnly := th.ReadOnly
	th.mu.Unlock()
	if readOnly {
		return s.writeResponse(req.ID, nil, errors.New("thread is read-only"))
	}

	queueID := strings.TrimSpace(params.ClientID)
	if queueID == "" {
		queueID = session.NewID()
	}
	msg := providers.ChatMessage{
		Role:     "user",
		ClientID: queueID,
		Content:  params.Prompt,
		Images:   images,
		Files:    files,
	}
	queued := queuedTurnSummary(params.ThreadID, queuedTurn{id: queueID, msg: msg})
	s.enqueueQueuedUserTurn(params.ThreadID, queuedTurn{id: queueID, msg: msg})
	if err := s.writeResponse(req.ID, TurnQueueResult{Queued: queued}, nil); err != nil {
		return err
	}
	_ = s.writeNotification(NotificationTurnQueued, TurnQueuedNotification{Queued: queued})
	s.kickQueuedTurnDrain(params.ThreadID)
	return nil
}

func (s *Server) handleTurnDequeue(req Request) error {
	var params TurnDequeueParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	queueID := strings.TrimSpace(params.QueueID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if queueID == "" {
		return s.writeResponse(req.ID, nil, errors.New("queue_id is required"))
	}
	removed := s.removeQueuedUserTurn(threadID, queueID)
	if err := s.writeResponse(req.ID, OKResult{OK: removed}, nil); err != nil {
		return err
	}
	if removed {
		_ = s.writeNotification(NotificationTurnDequeued, TurnDequeuedNotification{
			ThreadID: threadID,
			QueueID:  queueID,
		})
	}
	return nil
}

func (s *Server) handleTurnSteer(req Request) error {
	var params TurnSteerParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	params.ThreadID = strings.TrimSpace(params.ThreadID)
	params.Prompt = strings.TrimSpace(params.Prompt)
	params.ExpectedTurnID = strings.TrimSpace(params.ExpectedTurnID)
	if params.ThreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if params.ExpectedTurnID == "" {
		return s.writeResponse(req.ID, nil, errors.New("expected_turn_id is required"))
	}
	images, err := normalizeTurnStartImages(params.Images)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	files, err := normalizeTurnStartFiles(params.Files)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if params.Prompt == "" && len(images) == 0 && len(files) == 0 {
		return s.writeResponse(req.ID, nil, errors.New("prompt or attachment is required"))
	}
	th := s.thread(params.ThreadID)
	if th == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q not found", params.ThreadID))
	}
	clientID := strings.TrimSpace(params.ClientID)
	if clientID == "" {
		clientID = session.NewID()
	}

	th.mu.Lock()
	if !th.running || th.currentTurn == "" {
		th.mu.Unlock()
		return s.writeResponse(req.ID, nil, errors.New("no active turn to steer"))
	}
	if params.ExpectedTurnID != th.currentTurn {
		actual := th.currentTurn
		th.mu.Unlock()
		return s.writeResponse(req.ID, nil, fmt.Errorf("expected active turn id `%s` but found `%s`", params.ExpectedTurnID, actual))
	}
	turnID := th.currentTurn
	th.pendingSteers = append(th.pendingSteers, providers.ChatMessage{
		Role:     "user",
		ClientID: clientID,
		Content:  params.Prompt,
		Images:   images,
		Files:    files,
		Steered:  true,
	})
	th.mu.Unlock()
	if s.removeQueuedUserTurn(params.ThreadID, clientID) {
		_ = s.writeNotification(NotificationTurnDequeued, TurnDequeuedNotification{
			ThreadID: params.ThreadID,
			QueueID:  clientID,
		})
	}
	return s.writeResponse(req.ID, TurnSteerResult{TurnID: turnID}, nil)
}

func (s *Server) handleTurnUnsteer(req Request) error {
	var params TurnUnsteerParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	steerID := strings.TrimSpace(params.SteerID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if steerID == "" {
		return s.writeResponse(req.ID, nil, errors.New("steer_id is required"))
	}
	th := s.thread(threadID)
	if th == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q not found", threadID))
	}
	th.mu.Lock()
	removed := th.removePendingSteerLocked(steerID)
	th.mu.Unlock()
	return s.writeResponse(req.ID, OKResult{OK: removed}, nil)
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
	threadRuntime, err := s.rt.NewThreadRuntime(th.ID)
	if err != nil {
		return nil, err
	}
	if threadRuntime.Toolkit != nil {
		s.installToolApprovalReviewer(threadRuntime.Toolkit)
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

func normalizeTurnStartFiles(files []TurnStartFile) ([]providers.InputFile, error) {
	if len(files) == 0 {
		return nil, nil
	}
	out := make([]providers.InputFile, 0, len(files))
	for index, file := range files {
		mediaType := strings.TrimSpace(file.MediaType)
		data := strings.TrimSpace(file.Data)
		if data == "" {
			return nil, fmt.Errorf("file %d data is required", index+1)
		}
		var err error
		mediaType, data, err = normalizeFilePayload(mediaType, data)
		if err != nil {
			return nil, fmt.Errorf("file %d: %w", index+1, err)
		}
		out = append(out, providers.InputFile{
			MediaType: mediaType,
			Data:      data,
			Filename:  strings.TrimSpace(file.Filename),
		})
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

func normalizeFilePayload(mediaType, data string) (string, string, error) {
	if strings.HasPrefix(strings.ToLower(data), "data:") {
		header, payload, ok := strings.Cut(data, ",")
		if !ok {
			return "", "", errors.New("invalid data URL")
		}
		if !strings.Contains(strings.ToLower(header), ";base64") {
			return "", "", errors.New("file data URL must be base64")
		}
		if mediaType == "" {
			mediaType = strings.TrimPrefix(strings.Split(header, ";")[0], "data:")
		}
		data = strings.TrimSpace(payload)
	}
	mediaType = strings.TrimSpace(strings.ToLower(mediaType))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	if mediaType != "application/pdf" {
		return "", "", fmt.Errorf("unsupported file media type %q", mediaType)
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
	toolRecordStart := 0
	if threadRuntime != nil && threadRuntime.Toolkit != nil {
		toolRecordStart = len(threadRuntime.Toolkit.ToolTelemetry())
	}
	baseBeforeStep := runner.BeforeStep
	baseOnRequestContext := runner.OnRequestContext
	// Forward provider-reported token usage into throttled "turn/usage"
	// notifications so live UIs can render a real token-speed gauge when the
	// provider exposes stream-time cumulative usage. We keep completed calls
	// separate from the in-flight call because stream usage snapshots are
	// cumulative for the current provider request, not deltas.
	const usageNotifyInterval = 100 * time.Millisecond
	var usagePushMu sync.Mutex
	var lastUsagePushAt time.Time
	var completedUsage providers.TokenUsage
	var liveUsage providers.TokenUsage
	baseOnUsage := runner.OnUsage
	baseOnTokenUsage := runner.OnTokenUsage
	addUsage := func(a, b providers.TokenUsage) providers.TokenUsage {
		return providers.TokenUsage{
			InputTokens:         a.InputTokens + b.InputTokens,
			OutputTokens:        a.OutputTokens + b.OutputTokens,
			CacheCreationTokens: a.CacheCreationTokens + b.CacheCreationTokens,
			CacheReadTokens:     a.CacheReadTokens + b.CacheReadTokens,
		}
	}
	notifyUsage := func(snapshot providers.TokenUsage, force bool) {
		now := time.Now()
		usagePushMu.Lock()
		shouldPush := force || lastUsagePushAt.IsZero() || now.Sub(lastUsagePushAt) >= usageNotifyInterval
		if shouldPush {
			lastUsagePushAt = now
		}
		usagePushMu.Unlock()
		if !shouldPush {
			return
		}
		notify(NotificationTurnUsage, TurnUsageNotification{
			ThreadID:            th.ID,
			TurnID:              turnID,
			InputTokens:         snapshot.InputTokens,
			OutputTokens:        snapshot.OutputTokens,
			CacheCreationTokens: snapshot.CacheCreationTokens,
			CacheReadTokens:     snapshot.CacheReadTokens,
		})
	}
	runner.OnUsage = func(inputTokens, outputTokens int) {
		if baseOnUsage != nil {
			baseOnUsage(inputTokens, outputTokens)
		}
	}
	runner.OnTokenUsage = func(usage providers.TokenUsage) {
		if baseOnTokenUsage != nil {
			baseOnTokenUsage(usage)
		}
		usagePushMu.Lock()
		completedUsage = addUsage(completedUsage, usage)
		liveUsage = providers.TokenUsage{}
		usageSnapshot := completedUsage
		usagePushMu.Unlock()
		notifyUsage(usageSnapshot, true)
	}
	var contextRequests []sessiontrace.RequestContextRecord
	runner.OnRequestContext = func(info agent.RequestContextInfo) {
		if baseOnRequestContext != nil {
			baseOnRequestContext(info)
		}
		contextRequests = append(contextRequests, sessiontrace.RequestContextRecord{
			StepIndex:         info.StepIndex,
			TransientMessages: info.TransientMessages,
			ContentBytes:      info.ContentBytes,
			BlockKinds:        append([]string(nil), info.BlockKinds...),
		})
	}
	runner.BeforeStep = func() []providers.ChatMessage {
		var messages []providers.ChatMessage
		if baseBeforeStep != nil {
			messages = append(messages, baseBeforeStep()...)
		}
		th.mu.Lock()
		steers, batch := th.takePendingSteersLocked(turnID, time.Now().UTC())
		th.mu.Unlock()
		notifyBatch(batch)
		if len(steers) > 0 {
			messages = append(messages, steers...)
		}
		return messages
	}
	defer func() {
		runner.BeforeStep = baseBeforeStep
		runner.OnRequestContext = baseOnRequestContext
		runner.OnUsage = baseOnUsage
		runner.OnTokenUsage = baseOnTokenUsage
	}()
	// Hang the recent transcript off the request context so the LLM-driven
	// guardian reviewer can judge pending tool calls in light of user intent.
	// The snapshot is taken at turn entry; per-step updates would require
	// plumbing through the streaming callback and are deferred for v1.
	ctx = guardian.WithTranscript(ctx, guardian.TranscriptFromChatMessages(history))
	res, err := runner.RunWithCallback(ctx, history, func(ev providers.StreamEvent) {
		if ev.Type == providers.EventUsage && ev.Usage != nil {
			usagePushMu.Lock()
			liveUsage = *ev.Usage
			usageSnapshot := addUsage(completedUsage, liveUsage)
			usagePushMu.Unlock()
			notifyUsage(usageSnapshot, false)
		}
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
		th.History = cloneHistory(res.NewMessages)
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
	var titleHistory []providers.ChatMessage
	if err == nil {
		titleHistory = cloneHistory(th.History)
	}
	turn := th.completeTurnLocked(turnID, status, err, now)
	unconsumedSteers := th.drainPendingSteersLocked()
	th.mu.Unlock()

	tracePath, traceErr := s.persistTurnTrace(threadRuntime, runner, th.ID, turn, res, err, toolRecordStart, contextRequests)
	if traceErr != nil {
		tracePath = ""
	}

	if len(unconsumedSteers) > 0 {
		s.prependQueuedUserTurns(th.ID, queuedTurnsFromSteers(unconsumedSteers))
	}
	if err != nil {
		notify(NotificationTurnError, TurnErrorNotification{
			ThreadID: th.ID,
			TurnID:   turnID,
			Error:    err.Error(),
			Turn:     turn,
		})
		s.kickAgentCompletionDrain(th.ID)
		s.kickQueuedTurnDrain(th.ID)
		return
	}
	notify(NotificationTurnCompleted, TurnCompletedNotification{
		ThreadID:            th.ID,
		Turn:                turn,
		Content:             res.Content,
		InputTokens:         res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CacheCreationTokens: res.CacheCreationTokens,
		CacheReadTokens:     res.CacheReadTokens,
		TracePath:           tracePath,
	})
	go s.generateThreadTitle(th.ID, titleHistory)
	s.kickAgentCompletionDrain(th.ID)
	s.kickQueuedTurnDrain(th.ID)
}

func (s *Server) persistTurnTrace(threadRuntime *runtime.ThreadRuntime, runner *agent.StreamRunner, threadID string, turn Turn, res agent.LoopResult, runErr error, toolRecordStart int, contextRequests []sessiontrace.RequestContextRecord) (string, error) {
	if threadRuntime == nil || threadRuntime.Toolkit == nil {
		return "", nil
	}
	tracePath := sessiontrace.Path(threadRuntime.Toolkit.SessionDir())
	if strings.TrimSpace(tracePath) == "" {
		return "", nil
	}
	providerName := ""
	if s != nil && s.rt != nil {
		providerName = s.rt.ProviderName
	}
	model := ""
	apiModel := ""
	if runner != nil {
		model = runner.Model
		apiModel = runner.APIModel
	}
	modelBudget := threadRuntime.ModelBudget
	errorText := ""
	if runErr != nil {
		errorText = runErr.Error()
	}
	turnRecord := sessiontrace.TurnRecord{
		ThreadID:            threadID,
		TurnID:              turn.ID,
		Status:              string(turn.Status),
		ProviderName:        providerName,
		Model:               model,
		APIModel:            apiModel,
		ModelProfile:        sessiontrace.NewModelProfileRecordWithBudget(providerName, model, apiModel, modelBudget),
		StartedAt:           turn.StartedAt,
		CompletedAt:         turn.CompletedAt,
		DurationMS:          turn.DurationMS,
		InputTokens:         res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CacheCreationTokens: res.CacheCreationTokens,
		CacheReadTokens:     res.CacheReadTokens,
		HistoryRewritten:    res.HistoryRewritten,
		Error:               errorText,
	}
	finalRecord := sessiontrace.FinalRecord{
		Status:              string(turn.Status),
		InputTokens:         res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CacheCreationTokens: res.CacheCreationTokens,
		CacheReadTokens:     res.CacheReadTokens,
		FinalAnswerPreview:  res.Content,
		Error:               errorText,
	}
	records := threadRuntime.Toolkit.ToolTelemetry()
	if toolRecordStart > 0 && toolRecordStart < len(records) {
		records = records[toolRecordStart:]
	} else if toolRecordStart >= len(records) {
		records = nil
	}
	if err := sessiontrace.AppendTurn(tracePath, turnRecord, finalRecord, threadRuntime.Toolkit.ToolInfos(), records, contextRequests); err != nil {
		return "", err
	}
	return tracePath, nil
}

func (s *Server) enqueueAgentCompletionTurn(threadID string, msg providers.ChatMessage) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || !chatMessageHasUserPayload(msg) {
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

func (s *Server) enqueueQueuedUserTurn(threadID string, entry queuedTurn) {
	threadID = strings.TrimSpace(threadID)
	entry.id = strings.TrimSpace(entry.id)
	if threadID == "" || entry.id == "" || !chatMessageHasUserPayload(entry.msg) {
		return
	}
	if strings.TrimSpace(entry.msg.Role) == "" {
		entry.msg.Role = "user"
	}
	entry.msg.ClientID = entry.id
	entry.msg.Steered = false

	s.queuedTurnMu.Lock()
	if s.pendingQueuedTurns == nil {
		s.pendingQueuedTurns = make(map[string][]queuedTurn)
	}
	s.pendingQueuedTurns[threadID] = append(s.pendingQueuedTurns[threadID], entry)
	s.queuedTurnMu.Unlock()
}

func (s *Server) removeQueuedUserTurn(threadID, queueID string) bool {
	threadID = strings.TrimSpace(threadID)
	queueID = strings.TrimSpace(queueID)
	if threadID == "" || queueID == "" {
		return false
	}
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	pending := s.pendingQueuedTurns[threadID]
	next := pending[:0]
	removed := false
	for _, entry := range pending {
		if !removed && entry.id == queueID {
			removed = true
			continue
		}
		next = append(next, entry)
	}
	if len(next) == 0 {
		delete(s.pendingQueuedTurns, threadID)
	} else {
		s.pendingQueuedTurns[threadID] = next
	}
	return removed
}

func (s *Server) kickQueuedTurnDrain(threadID string) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}

	s.queuedTurnMu.Lock()
	if len(s.pendingQueuedTurns[threadID]) == 0 || s.drainingQueuedTurns[threadID] {
		s.queuedTurnMu.Unlock()
		return
	}
	if s.drainingQueuedTurns == nil {
		s.drainingQueuedTurns = make(map[string]bool)
	}
	s.drainingQueuedTurns[threadID] = true
	s.queuedTurnMu.Unlock()

	go s.drainQueuedTurns(threadID)
}

func (s *Server) drainQueuedTurns(threadID string) {
	th := s.thread(threadID)
	if th == nil {
		s.discardQueuedUserTurns(threadID)
		s.clearQueuedTurnDrain(threadID)
		return
	}
	if threadIsRunning(th) {
		s.clearQueuedTurnDrain(threadID)
		return
	}

	entry, ok := s.takeNextQueuedUserTurn(threadID)
	if !ok {
		s.clearQueuedTurnDrain(threadID)
		return
	}
	started, err := s.startQueuedTurn(context.Background(), threadID, entry)
	if err != nil {
		providers.DebugLogf("start queued turn for thread %q: %v", threadID, err)
	}
	requeued := false
	if !started && err == nil {
		s.prependQueuedUserTurns(threadID, []queuedTurn{entry})
		requeued = true
	}
	s.clearQueuedTurnDrain(threadID)
	if requeued || s.hasQueuedUserTurns(threadID) {
		s.kickQueuedTurnDrain(threadID)
	}
}

func (s *Server) startQueuedTurn(ctx context.Context, threadID string, entry queuedTurn) (bool, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return false, errors.New("thread_id is required")
	}
	if strings.TrimSpace(entry.msg.Role) == "" {
		entry.msg.Role = "user"
	}
	if !chatMessageHasUserPayload(entry.msg) {
		return false, nil
	}
	entry.id = strings.TrimSpace(entry.id)
	if entry.id == "" {
		entry.id = session.NewID()
	}
	entry.msg.ClientID = entry.id
	entry.msg.Steered = false

	th := s.thread(threadID)
	if th == nil {
		return false, fmt.Errorf("thread %q not found", threadID)
	}
	threadRuntime, err := s.ensureThreadRuntime(th)
	if err != nil {
		return false, err
	}

	turnID := session.NewID()
	turnCtx, cancel := context.WithCancel(ctx)
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
		return false, errors.New("thread is read-only")
	}
	if err := appendChatMessage(th.MemoryPath, entry.msg); err != nil {
		th.mu.Unlock()
		cancel()
		return false, err
	}
	history := cloneHistory(th.History)
	history = append(history, entry.msg)
	th.History = history
	th.cancel = cancel
	turn := th.startTurnLocked(turnID, entry.msg, now)
	th.mu.Unlock()

	_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
		ThreadID: threadID,
		Turn:     turn,
		QueueID:  entry.id,
	})
	go s.runTurn(turnCtx, th, threadRuntime, turnID, history)
	return true, nil
}

func (s *Server) takeNextQueuedUserTurn(threadID string) (queuedTurn, bool) {
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	pending := s.pendingQueuedTurns[threadID]
	if len(pending) == 0 {
		return queuedTurn{}, false
	}
	entry := pending[0]
	if len(pending) == 1 {
		delete(s.pendingQueuedTurns, threadID)
	} else {
		s.pendingQueuedTurns[threadID] = append([]queuedTurn(nil), pending[1:]...)
	}
	return entry, true
}

func (s *Server) prependQueuedUserTurns(threadID string, entries []queuedTurn) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || len(entries) == 0 {
		return
	}
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	if s.pendingQueuedTurns == nil {
		s.pendingQueuedTurns = make(map[string][]queuedTurn)
	}
	existing := append([]queuedTurn(nil), s.pendingQueuedTurns[threadID]...)
	s.pendingQueuedTurns[threadID] = append(append([]queuedTurn(nil), entries...), existing...)
}

func (s *Server) hasQueuedUserTurns(threadID string) bool {
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	return len(s.pendingQueuedTurns[threadID]) > 0
}

func (s *Server) discardQueuedUserTurns(threadID string) {
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	delete(s.pendingQueuedTurns, threadID)
}

func (s *Server) clearQueuedTurnDrain(threadID string) {
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	delete(s.drainingQueuedTurns, threadID)
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
	if !chatMessageHasUserPayload(userMsg) {
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
	history := cloneHistory(th.History)
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
	pending := cloneHistory(s.pendingAgentCompletionTurns[threadID])
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
	existing := cloneHistory(s.pendingAgentCompletionTurns[threadID])
	s.pendingAgentCompletionTurns[threadID] = append(cloneHistory(msgs), existing...)
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
	name := ""
	for _, msg := range msgs {
		if name == "" {
			name = strings.TrimSpace(msg.Name)
		}
		if content := strings.TrimSpace(msg.Content); content != "" {
			contents = append(contents, content)
		}
	}
	return providers.ChatMessage{
		Role:    "user",
		Name:    name,
		Content: strings.Join(contents, "\n\n"),
	}
}

func queuedTurnSummary(threadID string, entry queuedTurn) QueuedTurn {
	preview := strings.TrimSpace(entry.msg.Content)
	if preview == "" && len(entry.msg.Images) > 0 {
		if len(entry.msg.Images) == 1 {
			preview = "[Image #1]"
		} else {
			preview = fmt.Sprintf("[%d images]", len(entry.msg.Images))
		}
	}
	if preview == "" && len(entry.msg.Files) > 0 {
		if len(entry.msg.Files) == 1 {
			preview = filePreview(entry.msg.Files[0], 1)
		} else {
			preview = fmt.Sprintf("[%d files]", len(entry.msg.Files))
		}
	}
	return QueuedTurn{
		ID:         entry.id,
		ThreadID:   threadID,
		Preview:    preview,
		ImageCount: len(entry.msg.Images),
		FileCount:  len(entry.msg.Files),
	}
}

func chatMessageHasUserPayload(msg providers.ChatMessage) bool {
	return strings.TrimSpace(msg.Content) != "" || len(msg.Images) > 0 || len(msg.Files) > 0
}

func queuedTurnsFromSteers(msgs []providers.ChatMessage) []queuedTurn {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]queuedTurn, 0, len(msgs))
	for _, msg := range msgs {
		id := strings.TrimSpace(msg.ClientID)
		if id == "" {
			id = session.NewID()
		}
		msg.ClientID = id
		msg.Steered = false
		out = append(out, queuedTurn{id: id, msg: msg})
	}
	return out
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
	if err := appendTokenUsage(th.MemoryPath, th.ModelProvider, th.Model, providers.TokenUsage{
		InputTokens:         res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CacheCreationTokens: res.CacheCreationTokens,
		CacheReadTokens:     res.CacheReadTokens,
	}); err != nil {
		return err
	}
	return session.UpdateIndex(s.rt.SessionDir, th.ID, persistableMessageCount(th.History), threadPreview(th.History))
}
