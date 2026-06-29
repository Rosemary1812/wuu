package appserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/goalruntime"
	"github.com/blueberrycongee/wuu/internal/guardian"
	"github.com/blueberrycongee/wuu/internal/imageproc"
	"github.com/blueberrycongee/wuu/internal/insight"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/sessiontrace"
	"github.com/blueberrycongee/wuu/internal/stringutil"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

type queuedTurn struct {
	id  string
	msg providers.ChatMessage
}

type agentCompletionTurn struct {
	agentID  string
	resultID string
	msg      providers.ChatMessage
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
	userMsg := userMessageFromPrompt(params.Prompt, images, files)
	now := time.Now().UTC()

	th.mu.Lock()
	if th.running {
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q already has a running turn", params.ThreadID))
	}
	if th.PersistHistory {
		if err := appendChatMessage(s.rt.SessionDir, th.ID, userMsg); err != nil {
			th.mu.Unlock()
			cancel()
			return s.writeResponse(req.ID, nil, err)
		}
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
	msg := userMessageFromPrompt(params.Prompt, images, files)
	msg.ClientID = queueID
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
	steerMsg := userMessageFromPrompt(params.Prompt, images, files)
	steerMsg.ClientID = clientID
	steerMsg.Steered = true
	th.pendingSteers = append(th.pendingSteers, steerMsg)
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
	rootDir := th.CWD
	th.mu.Unlock()
	if existing != nil {
		return existing, nil
	}
	if s.rt == nil {
		return nil, errors.New("runtime session is required")
	}
	threadRuntime, err := s.rt.NewThreadRuntimeForRoot(th.ID, firstNonEmpty(rootDir, s.rt.RootDir))
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
		rawBytes, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("image %d: base64 decode: %w", index+1, err)
		}
		mode := imageproc.ModeDefault
		if image.Original {
			mode = imageproc.ModeOriginal
		}
		result, err := imageproc.Encode("", rawBytes, imageproc.Options{Mode: mode})
		if err != nil {
			return nil, fmt.Errorf("image %d: %w", index+1, err)
		}
		out = append(out, providers.InputImage{
			MediaType: result.MediaType,
			Data:      base64.StdEncoding.EncodeToString(result.Bytes),
		})
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

func userMessageFromPrompt(prompt string, images []providers.InputImage, files []providers.InputFile) providers.ChatMessage {
	content, display, ok := renderLightweightSlashCommandPrompt(prompt)
	msg := providers.ChatMessage{
		Role:    "user",
		Content: content,
		Images:  images,
		Files:   files,
	}
	if ok {
		msg.DisplayContent = display
	}
	return msg
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
	s.runTurnWithRequestContext(ctx, th, threadRuntime, turnID, history, nil)
}

func usageContextWindowTokens(runner *agent.StreamRunner) int {
	if runner == nil {
		return 0
	}
	if runner.ContextWindowOverride > 0 {
		return runner.ContextWindowOverride
	}
	return 0
}

func (s *Server) runTurnWithRequestContext(ctx context.Context, th *threadState, threadRuntime *runtime.ThreadRuntime, turnID string, history []providers.ChatMessage, requestContext []agent.ContextSegment) {
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
	// Resolve the real runtime context-window size for the active model so
	// turn/usage notifications can drive a "已用 / 总数" meter in the UI.
	// Captured once per turn: the runner is per-thread for its lifetime,
	// so the model identity does not change between usage samples.
	contextWindowTokens := usageContextWindowTokens(runner)
	toolRecordStart := 0
	if threadRuntime != nil && threadRuntime.Toolkit != nil {
		toolRecordStart = len(threadRuntime.Toolkit.ToolTelemetry())
	}
	baseBeforeStep := runner.BeforeStep
	baseBeforeRequestContext := runner.BeforeRequestContext
	baseOnRequestContext := runner.OnRequestContext
	baseOnCompactAttempt := runner.OnCompactAttempt
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
			Model:               runner.Model,
			InputTokens:         snapshot.InputTokens,
			OutputTokens:        snapshot.OutputTokens,
			CacheCreationTokens: snapshot.CacheCreationTokens,
			CacheReadTokens:     snapshot.CacheReadTokens,
			ContextWindowTokens: contextWindowTokens,
		})
	}
	runner.OnUsage = func(inputTokens, outputTokens int) {
		if baseOnUsage != nil {
			baseOnUsage(inputTokens, outputTokens)
		}
	}
	var contextRequests []sessiontrace.RequestContextRecord
	var providerStates []sessiontrace.ProviderStateRecord
	var compactAttempts []sessiontrace.CompactRecord
	runner.OnTokenUsage = func(usage providers.TokenUsage) {
		if baseOnTokenUsage != nil {
			baseOnTokenUsage(usage)
		}
		attachUsageToLatestRequestContext(contextRequests, usage)
		usagePushMu.Lock()
		completedUsage = addUsage(completedUsage, usage)
		liveUsage = providers.TokenUsage{}
		usageSnapshot := completedUsage
		usagePushMu.Unlock()
		notifyUsage(usageSnapshot, true)
	}
	runner.OnRequestContext = func(info agent.RequestContextInfo) {
		if baseOnRequestContext != nil {
			baseOnRequestContext(info)
		}
		contextRequests = append(contextRequests, sessiontrace.RequestContextRecord{
			StepIndex:                info.StepIndex,
			TransientMessages:        info.TransientMessages,
			ContentBytes:             info.ContentBytes,
			BlockKinds:               append([]string(nil), info.BlockKinds...),
			BlockKindCounts:          cloneStringIntMap(info.BlockKindCounts),
			BlockKindBytes:           cloneStringIntMap(info.BlockKindBytes),
			SegmentLifecycleCounts:   cloneStringIntMap(info.SegmentLifecycleCounts),
			SegmentPlacementCounts:   cloneStringIntMap(info.SegmentPlacementCounts),
			SegmentCachePolicyCounts: cloneStringIntMap(info.SegmentCachePolicyCounts),
			MessageCount:             info.MessageCount,
			SystemMessages:           info.SystemMessages,
			HiddenMessages:           info.HiddenMessages,
			ToolCount:                info.ToolCount,
			StablePrefix:             info.StablePrefix,
			TurnPrefix:               info.TurnPrefix,
			DynamicBytes:             info.DynamicBytes,
			SystemBytes:              info.SystemBytes,
			StablePrefixBytes:        info.StablePrefixBytes,
			TurnPrefixBytes:          info.TurnPrefixBytes,
			MessageBytes:             info.MessageBytes,
			ToolSchemaBytes:          info.ToolSchemaBytes,
			LoadableToolCount:        info.LoadableToolCount,
			LoadableToolSchemaBytes:  info.LoadableToolSchemaBytes,
			LoadableToolSurfaceHash:  info.LoadableToolSurfaceHash,
			SystemHash:               info.SystemHash,
			StablePrefixHash:         info.StablePrefixHash,
			TurnPrefixHash:           info.TurnPrefixHash,
			ToolSurfaceHash:          info.ToolSurfaceHash,
			PromptCacheKey:           info.PromptCacheKey,
			InputTokens:              info.InputTokens,
			OutputTokens:             info.OutputTokens,
			CacheCreationTokens:      info.CacheCreationTokens,
			CacheReadTokens:          info.CacheReadTokens,
			SystemSections:           requestContextSystemSections(info.SystemSections),
		})
	}
	runner.OnCompactAttempt = func(info agent.CompactAttemptInfo) {
		if baseOnCompactAttempt != nil {
			baseOnCompactAttempt(info)
		}
		compactAttempts = append(compactAttempts, compactRecord(info))
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
	turnRequestContext := cloneContextSegments(requestContext)
	runner.BeforeRequestContext = func() []agent.ContextSegment {
		var segments []agent.ContextSegment
		if baseBeforeRequestContext != nil {
			segments = append(segments, baseBeforeRequestContext()...)
		}
		if len(turnRequestContext) > 0 {
			segments = append(segments, cloneContextSegments(turnRequestContext)...)
		}
		return segments
	}
	defer func() {
		runner.BeforeStep = baseBeforeStep
		runner.BeforeRequestContext = baseBeforeRequestContext
		runner.OnRequestContext = baseOnRequestContext
		runner.OnCompactAttempt = baseOnCompactAttempt
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
		if ev.Type == providers.EventProviderState && ev.ProviderState != nil {
			providerStates = append(providerStates, providerStateRecord(ev.ProviderState))
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
	if repaired, nerr := providers.RepairAndValidateToolCallHistory(th.History); nerr != nil {
		historyErr = nerr
	} else if !reflect.DeepEqual(repaired, th.History) {
		th.History = repaired
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
	accountingTurn := th.ensureTurnLocked(turnID, now)
	goalUsageDelta := goalUsageDeltaForTurn(accountingTurn, now, res)
	th.mu.Unlock()

	if accountErr := accountActiveGoalTurn(threadRuntime, goalUsageDelta, now); accountErr != nil {
		if err != nil {
			err = errors.Join(err, accountErr)
		} else {
			err = accountErr
			status = TurnStatusFailed
			titleHistory = nil
		}
	}
	if stopErr := stopActiveGoalAfterTurnError(threadRuntime, err, now); stopErr != nil {
		if err != nil {
			err = errors.Join(err, stopErr)
		} else {
			err = stopErr
			status = TurnStatusFailed
			titleHistory = nil
		}
	}

	th.mu.Lock()
	turn := th.completeTurnLocked(turnID, status, err, now, string(res.FinishReason), res.StopReason, res.Truncated)
	applyTokenUsageToTurn(&turn, providers.TokenUsage{
		InputTokens:         res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CacheCreationTokens: res.CacheCreationTokens,
		CacheReadTokens:     res.CacheReadTokens,
	}, res.ContextTokens, th.Model)
	th.replaceTurnLocked(turn)
	unconsumedSteers := th.drainPendingSteersLocked()
	th.mu.Unlock()

	tracePath, traceErr := s.persistTurnTrace(threadRuntime, runner, th.ID, turn, res, err, toolRecordStart, contextRequests, providerStates, compactAttempts)
	if traceErr != nil {
		tracePath = ""
	}

	if len(unconsumedSteers) > 0 {
		s.prependQueuedUserTurns(th.ID, queuedTurnsFromSteers(unconsumedSteers))
	}
	if err != nil {
		// Surface the Go core's typed error classification to the front-end
		// instead of dropping it on the floor. BuildTurnError pulls the
		// provider-specific code, status code, canonical category, and a
		// structured next-step action from the typed errors
		// (HTTPError, StreamError) and the agentcontrol classifier. The
		// raw `Error` string is preserved for backward compatibility and
		// for the "copy debug info" payload.
		structured := BuildTurnError(err, th.ModelProvider)
		turn.Error = &structured
		th.mu.Lock()
		th.replaceTurnLocked(turn)
		th.mu.Unlock()
		notify(NotificationTurnError, TurnErrorNotification{
			ThreadID:   th.ID,
			TurnID:     turnID,
			Error:      structured.Message,
			Code:       structured.Code,
			Category:   string(structured.Category),
			Provider:   structured.Provider,
			StatusCode: structured.StatusCode,
			Action:     structured.Action,
			Turn:       turn,
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
		ContextTokens:       res.ContextTokens,
		CacheCreationTokens: res.CacheCreationTokens,
		CacheReadTokens:     res.CacheReadTokens,
		FinishReason:        string(res.FinishReason),
		StopReason:          res.StopReason,
		Truncated:           res.Truncated,
		TracePath:           tracePath,
	})
	go s.generateThreadTitle(th.ID, titleHistory)
	s.kickAgentCompletionDrain(th.ID)
	s.kickQueuedTurnDrain(th.ID)
	s.kickGoalContinuation(th.ID)
}

func goalUsageDeltaForTurn(turn Turn, completedAt time.Time, res agent.LoopResult) goalruntime.UsageDelta {
	elapsed := time.Duration(0)
	if turn.StartedAt != nil && completedAt.After(*turn.StartedAt) {
		elapsed = completedAt.Sub(*turn.StartedAt)
	}
	return goalruntime.UsageDelta{
		// Goal budgets track fresh model work, not prompt-cache reads. Cache
		// reads still count toward context-window pressure in the agent loop.
		Tokens:  res.InputTokens + res.OutputTokens,
		Elapsed: elapsed,
		Turns:   1,
	}
}

func accountActiveGoalTurn(threadRuntime *runtime.ThreadRuntime, delta goalruntime.UsageDelta, completedAt time.Time) error {
	if threadRuntime == nil || threadRuntime.GoalRuntime == nil {
		return nil
	}
	if _, _, err := threadRuntime.GoalRuntime.AccountActiveUsage(delta, completedAt); err != nil {
		return fmt.Errorf("account active goal usage: %w", err)
	}
	return nil
}

func stopActiveGoalAfterTurnError(threadRuntime *runtime.ThreadRuntime, turnErr error, completedAt time.Time) error {
	if threadRuntime == nil || threadRuntime.GoalRuntime == nil || turnErr == nil {
		return nil
	}
	if errors.Is(turnErr, context.Canceled) {
		return nil
	}
	goal, err := threadRuntime.GoalRuntime.CurrentGoal()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load active goal after turn error: %w", err)
	}
	if goal.Status != goalruntime.StatusActive {
		return nil
	}
	status := goalruntime.StatusBlocked
	if providers.IsContextOverflow(turnErr) {
		status = goalruntime.StatusUsageLimited
	}
	if _, err := threadRuntime.GoalRuntime.SetSystemStatus(status, completedAt); err != nil {
		return fmt.Errorf("stop active goal after turn error: %w", err)
	}
	return nil
}

func (s *Server) persistTurnTrace(threadRuntime *runtime.ThreadRuntime, runner *agent.StreamRunner, threadID string, turn Turn, res agent.LoopResult, runErr error, toolRecordStart int, contextRequests []sessiontrace.RequestContextRecord, providerStates []sessiontrace.ProviderStateRecord, compactAttempts []sessiontrace.CompactRecord) (string, error) {
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
		FinishReason:        string(res.FinishReason),
		StopReason:          res.StopReason,
		Truncated:           res.Truncated,
		HistoryRewritten:    res.HistoryRewritten,
		Error:               errorText,
	}
	finalRecord := sessiontrace.FinalRecord{
		Status:              string(turn.Status),
		InputTokens:         res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CacheCreationTokens: res.CacheCreationTokens,
		CacheReadTokens:     res.CacheReadTokens,
		FinishReason:        string(res.FinishReason),
		StopReason:          res.StopReason,
		Truncated:           res.Truncated,
		FinalAnswerPreview:  res.Content,
		Error:               errorText,
	}
	records := threadRuntime.Toolkit.ToolTelemetry()
	if toolRecordStart > 0 && toolRecordStart < len(records) {
		records = records[toolRecordStart:]
	} else if toolRecordStart >= len(records) {
		records = nil
	}
	if err := sessiontrace.AppendTurn(tracePath, turnRecord, finalRecord, threadRuntime.Toolkit.ToolInfos(), records, contextRequests, providerStates, compactAttempts); err != nil {
		return "", err
	}
	return tracePath, nil
}

func compactRecord(info agent.CompactAttemptInfo) sessiontrace.CompactRecord {
	return sessiontrace.CompactRecord{
		Reason:         string(info.Reason),
		Status:         string(info.Status),
		TokensBefore:   info.TokensBefore,
		MessagesBefore: info.MessagesBefore,
		MessagesAfter:  info.MessagesAfter,
		Error:          info.Error,
	}
}

func providerStateRecord(state *providers.ProviderStateSummary) sessiontrace.ProviderStateRecord {
	if state == nil {
		return sessiontrace.ProviderStateRecord{}
	}
	return sessiontrace.ProviderStateRecord{
		StepIndex:              state.StepIndex,
		Provider:               state.Provider,
		Protocol:               state.Protocol,
		Transport:              state.Transport,
		ReplayMode:             state.ReplayMode,
		PreviousResponseIDUsed: state.PreviousResponseIDUsed,
		ConnectionReused:       state.ConnectionReused,
		FallbackActive:         state.FallbackActive,
		FallbackReason:         state.FallbackReason,
		InputItems:             state.InputItems,
		FullInputItems:         state.FullInputItems,
		DeltaInputItems:        state.DeltaInputItems,
	}
}

func attachUsageToLatestRequestContext(records []sessiontrace.RequestContextRecord, usage providers.TokenUsage) {
	if len(records) == 0 {
		return
	}
	record := &records[len(records)-1]
	record.InputTokens = usage.InputTokens
	record.OutputTokens = usage.OutputTokens
	record.CacheCreationTokens = usage.CacheCreationTokens
	record.CacheReadTokens = usage.CacheReadTokens
}

func (s *Server) enqueueAgentCompletionTurn(threadID, agentID, resultID string, msg providers.ChatMessage) {
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
		s.pendingAgentCompletionTurns = make(map[string][]agentCompletionTurn)
	}
	s.pendingAgentCompletionTurns[threadID] = append(s.pendingAgentCompletionTurns[threadID], agentCompletionTurn{
		agentID:  strings.TrimSpace(agentID),
		resultID: strings.TrimSpace(resultID),
		msg:      msg,
	})
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
	if th.PersistHistory {
		if err := appendChatMessage(s.rt.SessionDir, th.ID, entry.msg); err != nil {
			th.mu.Unlock()
			cancel()
			return false, err
		}
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

func (s *Server) hasQueuedUserWork(threadID string) bool {
	s.queuedTurnMu.Lock()
	defer s.queuedTurnMu.Unlock()
	return len(s.pendingQueuedTurns[threadID]) > 0 || s.drainingQueuedTurns[threadID]
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
	pending, claimed := s.claimAgentCompletionTurns(threadID, pending)
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
		s.releaseAgentCompletionClaims(threadID, claimed)
		s.prependPendingAgentCompletionTurns(threadID, pending)
		requeued = true
	} else if !started || err != nil {
		s.releaseAgentCompletionClaims(threadID, claimed)
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
	if th.PersistHistory {
		if err := appendChatMessage(s.rt.SessionDir, th.ID, userMsg); err != nil {
			th.mu.Unlock()
			cancel()
			return false, err
		}
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

func (s *Server) takePendingAgentCompletionTurns(threadID string) []agentCompletionTurn {
	s.agentCompletionMu.Lock()
	defer s.agentCompletionMu.Unlock()
	pending := cloneAgentCompletionTurns(s.pendingAgentCompletionTurns[threadID])
	delete(s.pendingAgentCompletionTurns, threadID)
	return pending
}

func (s *Server) prependPendingAgentCompletionTurns(threadID string, turns []agentCompletionTurn) {
	if len(turns) == 0 {
		return
	}
	s.agentCompletionMu.Lock()
	defer s.agentCompletionMu.Unlock()
	if s.pendingAgentCompletionTurns == nil {
		s.pendingAgentCompletionTurns = make(map[string][]agentCompletionTurn)
	}
	existing := cloneAgentCompletionTurns(s.pendingAgentCompletionTurns[threadID])
	s.pendingAgentCompletionTurns[threadID] = append(cloneAgentCompletionTurns(turns), existing...)
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

func (s *Server) hasQueuedAgentCompletionWork(threadID string) bool {
	s.agentCompletionMu.Lock()
	defer s.agentCompletionMu.Unlock()
	return len(s.pendingAgentCompletionTurns[threadID]) > 0 || s.drainingAgentCompletionTurns[threadID]
}

func (s *Server) kickGoalContinuation(threadID string) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}

	s.goalContinuationMu.Lock()
	if s.drainingGoalContinuation[threadID] {
		s.goalContinuationMu.Unlock()
		return
	}
	if s.drainingGoalContinuation == nil {
		s.drainingGoalContinuation = make(map[string]bool)
	}
	s.drainingGoalContinuation[threadID] = true
	s.goalContinuationMu.Unlock()

	go s.drainGoalContinuation(threadID)
}

func (s *Server) drainGoalContinuation(threadID string) {
	started, err := s.startGoalContinuationTurn(context.Background(), threadID)
	if err != nil {
		providers.DebugLogf("start goal continuation turn for thread %q: %v", threadID, err)
	}
	s.clearGoalContinuationDrain(threadID)
	if started {
		return
	}
}

func (s *Server) clearGoalContinuationDrain(threadID string) {
	s.goalContinuationMu.Lock()
	defer s.goalContinuationMu.Unlock()
	delete(s.drainingGoalContinuation, threadID)
}

func (s *Server) startGoalContinuationTurn(ctx context.Context, threadID string) (bool, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return false, errors.New("thread_id is required")
	}
	th := s.thread(threadID)
	if th == nil {
		return false, fmt.Errorf("thread %q not found", threadID)
	}
	threadRuntime, err := s.ensureThreadRuntime(th)
	if err != nil {
		return false, err
	}
	if threadRuntime == nil || threadRuntime.GoalRuntime == nil {
		return false, nil
	}

	input := s.goalContinuationInput(th, threadID)
	decision, err := threadRuntime.GoalRuntime.DecideContinuation(input)
	if err != nil {
		return false, err
	}
	if !decision.Allowed {
		return false, nil
	}
	goal, err := threadRuntime.GoalRuntime.CurrentGoal()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !goal.CanAutoContinue() {
		return false, nil
	}

	turnID := session.NewID()
	turnCtx, cancel := context.WithCancel(ctx)
	now := time.Now().UTC()

	th.mu.Lock()
	if th.running || th.ReadOnly {
		th.mu.Unlock()
		cancel()
		return false, nil
	}
	if s.hasQueuedUserWork(threadID) || s.hasQueuedAgentCompletionWork(threadID) {
		th.mu.Unlock()
		cancel()
		return false, nil
	}
	history := cloneHistory(th.History)
	th.cancel = cancel
	turn := th.startInternalTurnLocked(turnID, now)
	th.mu.Unlock()

	_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
		ThreadID: threadID,
		Turn:     turn,
	})
	go s.runTurnWithRequestContext(turnCtx, th, threadRuntime, turnID, history, goalContinuationContextSegments(goal))
	return true, nil
}

func (s *Server) goalContinuationInput(th *threadState, threadID string) goalruntime.ContinuationInput {
	var running, readOnly bool
	if th != nil {
		th.mu.Lock()
		running = th.running
		readOnly = th.ReadOnly
		th.mu.Unlock()
	}
	return goalruntime.ContinuationInput{
		ThreadIdle:      !running,
		ActiveTurn:      running,
		QueuedUserWork:  s.hasQueuedUserWork(threadID),
		QueuedAgentWork: s.hasQueuedAgentCompletionWork(threadID),
		ReadOnly:        readOnly,
	}
}

const (
	// Goal continuations are hidden request-only reminders that can repeat
	// across many automatic turns. Keep the inline objective below a small
	// prompt budget and require get_goal when the full objective matters.
	goalContinuationObjectiveHeadBytes = 1200
	goalContinuationObjectiveTailBytes = 600
)

func goalContinuationContextSegments(goal goalruntime.Goal) []agent.ContextSegment {
	return agent.RequestOnlyContextBlocks([]wuucontext.Block{goalContinuationBlock(goal)})
}

func goalContinuationBlock(goal goalruntime.Goal) wuucontext.Block {
	objective, objectiveNote := goalContinuationObjectivePreview(goal.Objective)
	if objectiveNote != "" {
		objectiveNote = "\n" + objectiveNote
	}
	content := fmt.Sprintf(strings.Join([]string{
		"<goal_continuation>",
		"Continue working toward the active thread goal.",
		"Objective: %s%s",
		"Status: %s",
		"Tokens used: %d",
		"Time used seconds: %d",
		"Goal turns: %d",
		"",
		"Make concrete progress toward the objective. Do not mark the goal complete unless the objective is actually achieved. If the same blocker prevents progress for multiple continuation turns, use the goal status rules instead of pretending the work is complete.",
		"</goal_continuation>",
	}, "\n"), objective, objectiveNote, goal.Status, goal.TokensUsed, goal.TimeUsedSeconds, goal.GoalTurns)
	return wuucontext.Block{
		Kind:    wuucontext.BlockGoalContinuation,
		Title:   "Active goal continuation",
		Source:  "runtime.goal_continuation",
		Content: content,
	}
}

func goalContinuationObjectivePreview(objective string) (string, string) {
	objective = strings.TrimSpace(objective)
	if len([]byte(objective)) <= goalContinuationObjectiveHeadBytes+goalContinuationObjectiveTailBytes {
		return objective, ""
	}
	preview := stringutil.HeadTail(
		objective,
		goalContinuationObjectiveHeadBytes,
		goalContinuationObjectiveTailBytes,
		"\n\n[objective trimmed; call get_goal for the full objective]\n\n",
	)
	return preview, "Full objective omitted from this continuation reminder; call get_goal if the missing details matter."
}

func combineAgentCompletionMessages(turns []agentCompletionTurn) providers.ChatMessage {
	if len(turns) == 0 {
		return providers.ChatMessage{Role: "user"}
	}
	if len(turns) == 1 {
		return turns[0].msg
	}
	contents := make([]string, 0, len(turns))
	name := ""
	for _, turn := range turns {
		msg := turn.msg
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

func cloneAgentCompletionTurns(turns []agentCompletionTurn) []agentCompletionTurn {
	if len(turns) == 0 {
		return nil
	}
	msgs := make([]providers.ChatMessage, 0, len(turns))
	for _, turn := range turns {
		msgs = append(msgs, turn.msg)
	}
	msgs = cloneHistory(msgs)
	out := make([]agentCompletionTurn, 0, len(turns))
	for i, turn := range turns {
		out = append(out, agentCompletionTurn{
			agentID:  turn.agentID,
			resultID: turn.resultID,
			msg:      msgs[i],
		})
	}
	return out
}

func (s *Server) claimAgentCompletionTurns(threadID string, turns []agentCompletionTurn) ([]agentCompletionTurn, []agentCompletionTurn) {
	if len(turns) == 0 {
		return nil, nil
	}
	th := s.thread(threadID)
	if th == nil {
		return turns, nil
	}
	th.mu.Lock()
	threadRuntime := th.execRuntime
	th.mu.Unlock()
	if threadRuntime == nil || threadRuntime.AgentControl == nil {
		return turns, nil
	}
	out := turns[:0]
	claimed := make([]agentCompletionTurn, 0, len(turns))
	for _, turn := range turns {
		if strings.TrimSpace(turn.resultID) != "" {
			ok, _ := threadRuntime.AgentControl.ClaimAgentResultDeliveryID(turn.resultID, "auto_completion")
			if !ok {
				continue
			}
			claimed = append(claimed, turn)
		}
		out = append(out, turn)
	}
	return out, claimed
}

func (s *Server) releaseAgentCompletionClaims(threadID string, turns []agentCompletionTurn) {
	if len(turns) == 0 {
		return
	}
	th := s.thread(threadID)
	if th == nil {
		return
	}
	th.mu.Lock()
	threadRuntime := th.execRuntime
	th.mu.Unlock()
	if threadRuntime == nil || threadRuntime.AgentControl == nil {
		return
	}
	for _, turn := range turns {
		if strings.TrimSpace(turn.resultID) == "" {
			continue
		}
		threadRuntime.AgentControl.ReleaseAgentResultDeliveryClaim(turn.resultID, "auto_completion")
	}
}

func queuedTurnSummary(threadID string, entry queuedTurn) QueuedTurn {
	preview := strings.TrimSpace(chatMessageDisplayContent(entry.msg))
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
	if !th.PersistHistory {
		return nil
	}
	if rewriteHistory {
		if err := rewriteChatHistory(s.rt.SessionDir, th.ID, th.History); err != nil {
			return err
		}
	} else {
		for _, msg := range res.NewMessages {
			if err := appendChatMessage(s.rt.SessionDir, th.ID, msg); err != nil {
				return err
			}
		}
	}
	if err := appendTokenUsage(s.rt.SessionDir, th.ID, th.ModelProvider, th.Model, providers.TokenUsage{
		InputTokens:         res.InputTokens,
		OutputTokens:        res.OutputTokens,
		CacheCreationTokens: res.CacheCreationTokens,
		CacheReadTokens:     res.CacheReadTokens,
	}, res.ContextTokens); err != nil {
		return err
	}
	return session.UpdateIndex(s.rt.SessionDir, th.ID, persistableMessageCount(th.History), threadPreview(th.History))
}

// handleSettingsUsage returns the aggregated token usage snapshot for
// the desktop settings page. Range selects a time window ("all" / "7d"
// / "30d" / "90d"); empty defaults to "all". Range filtering is applied
// to each token_usage row's At timestamp (UTC), not to the session
// CreatedAt, so a long-running session that crosses a range boundary
// contributes only the rows inside the window. Rows with a zero At
// (legacy imports written before the timestamp was added) are kept
// under "all" and dropped from any time-windowed query so they cannot
// masquerade as fresh activity.
func (s *Server) handleSettingsUsage(req Request) error {
	var params SettingsUsageQuery
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	rangeFilter := params.Range
	if rangeFilter == "" {
		rangeFilter = SettingsUsageRangeAll
	}
	if err := validateSettingsUsageRange(rangeFilter); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	sessDir := s.rt.SessionDir
	now := time.Now().UTC()
	cutoff := settingsUsageRangeCutoff(rangeFilter, now)

	metas, err := insight.ScanSessions(sessDir, 0)
	if err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("scan sessions: %w", err))
	}
	rows, err := insight.CollectTokenUsageRows(sessDir)
	if err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("collect usage rows: %w", err))
	}
	filteredRows := filterUsageRowsByCutoff(rows, cutoff)

	titleByID := make(map[string]string, len(metas))
	for _, m := range metas {
		if title := strings.TrimSpace(m.FirstUserMsg); title != "" {
			titleByID[m.ID] = truncateUsageTitle(title)
		}
	}
	metrics, days, entries := aggregateUsageRows(filteredRows, titleByID)
	totalSessions := countSessionsInRange(rows, cutoff)

	return s.writeResponse(req.ID, SettingsUsageResponse{
		Range:           rangeFilter,
		TotalSessions:   totalSessions,
		GeneratedAt:     now.Format(time.RFC3339Nano),
		Metrics:         metrics,
		ModelBreakdowns: buildUsageModelBreakdowns(filteredRows),
		Days:            days,
		Entries:         entries,
	}, nil)
}

// filterUsageRowsByCutoff returns rows that fall inside the requested
// range. "all" (cutoff == nil) keeps every row, including zero-At
// legacy imports; time-windowed queries drop zero-At rows so they
// cannot be pinned to "today" or "this week" by accident.
func filterUsageRowsByCutoff(rows []insight.TokenUsageRow, cutoff *time.Time) []insight.TokenUsageRow {
	if cutoff == nil {
		return rows
	}
	out := make([]insight.TokenUsageRow, 0, len(rows))
	for _, r := range rows {
		if r.At.IsZero() {
			continue
		}
		if r.At.Before(*cutoff) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// countSessionsInRange returns the number of distinct session IDs that
// have at least one token_usage row inside the cutoff. Sessions with
// only zero-At legacy rows are excluded from time-windowed counts but
// still counted under "all".
func countSessionsInRange(rows []insight.TokenUsageRow, cutoff *time.Time) int {
	seen := make(map[string]struct{})
	for _, r := range rows {
		if cutoff != nil && r.At.IsZero() {
			continue
		}
		if cutoff != nil && r.At.Before(*cutoff) {
			continue
		}
		seen[r.SessionID] = struct{}{}
	}
	return len(seen)
}

func buildUsageModelBreakdowns(rows []insight.TokenUsageRow) []insight.ModelUsage {
	buckets := make(map[string]*insight.ModelUsage)
	sessionsByBucket := make(map[string]map[string]struct{})
	for _, r := range rows {
		key := r.Provider + "|" + r.Model
		bucket, ok := buckets[key]
		if !ok {
			bucket = &insight.ModelUsage{Provider: r.Provider, Model: r.Model}
			buckets[key] = bucket
		}
		bucket.InputTokens += r.InputTokens
		bucket.OutputTokens += r.OutputTokens
		bucket.CacheCreationTokens += r.CacheCreationTokens
		bucket.CacheReadTokens += r.CacheReadTokens
		if r.SessionID != "" {
			seen := sessionsByBucket[key]
			if seen == nil {
				seen = make(map[string]struct{})
				sessionsByBucket[key] = seen
			}
			seen[r.SessionID] = struct{}{}
		}
	}

	breakdowns := make([]insight.ModelUsage, 0, len(buckets))
	for key, bucket := range buckets {
		bucket.Sessions = len(sessionsByBucket[key])
		breakdowns = append(breakdowns, *bucket)
	}
	sort.Slice(breakdowns, func(i, j int) bool {
		return breakdowns[i].TotalContextTokens() > breakdowns[j].TotalContextTokens()
	})
	return breakdowns
}

// aggregateUsageRows is the single source of truth for the desktop
// usage page's metrics, daily series, and recent-entries list. It
// never reads from session metadata — only the per-row token_usage
// trail — so the headline numbers, the heatmap, and the "最近记录"
// list all stay numerically consistent.
func aggregateUsageRows(rows []insight.TokenUsageRow, titleByID map[string]string) (SettingsUsageMetrics, []SettingsUsageDay, []SettingsUsageEntry) {
	metrics := SettingsUsageMetrics{}
	type dayBucket struct {
		input, output, cacheRead, cacheCreation int
		turns                                   int
	}
	daysByDate := make(map[string]*dayBucket)

	var minAt, maxAt time.Time
	for _, r := range rows {
		metrics.InputTokens += r.InputTokens
		metrics.OutputTokens += r.OutputTokens
		metrics.CacheReadTokens += r.CacheReadTokens
		metrics.CacheCreationTokens += r.CacheCreationTokens
		metrics.Turns++
		if !r.At.IsZero() {
			if minAt.IsZero() || r.At.Before(minAt) {
				minAt = r.At
			}
			if r.At.After(maxAt) {
				maxAt = r.At
			}
			date := r.At.UTC().Format("2006-01-02")
			bucket, ok := daysByDate[date]
			if !ok {
				bucket = &dayBucket{}
				daysByDate[date] = bucket
			}
			bucket.input += r.InputTokens
			bucket.output += r.OutputTokens
			bucket.cacheRead += r.CacheReadTokens
			bucket.cacheCreation += r.CacheCreationTokens
			bucket.turns++
		}
	}

	metrics.PromptTokens = metrics.InputTokens + metrics.CacheReadTokens
	metrics.ContextTokens = metrics.InputTokens + metrics.CacheReadTokens + metrics.OutputTokens
	if metrics.PromptTokens > 0 {
		metrics.CacheHitRate = float64(metrics.CacheReadTokens) / float64(metrics.PromptTokens)
	}
	if !minAt.IsZero() {
		metrics.DateRange = [2]string{minAt.UTC().Format("2006-01-02"), maxAt.UTC().Format("2006-01-02")}
		metrics.ActiveDays = len(daysByDate)
	}

	days := make([]SettingsUsageDay, 0, len(daysByDate))
	for date, b := range daysByDate {
		prompt := b.input + b.cacheRead
		var rate float64
		if prompt > 0 {
			rate = float64(b.cacheRead) / float64(prompt)
		}
		days = append(days, SettingsUsageDay{
			Date:                date,
			InputTokens:         b.input,
			OutputTokens:        b.output,
			CacheCreationTokens: b.cacheCreation,
			CacheReadTokens:     b.cacheRead,
			CacheHitRate:        rate,
			Turns:               b.turns,
		})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })

	entries := buildUsageEntries(rows, titleByID, 8)
	return metrics, days, entries
}

// buildUsageEntries picks the most recent N token_usage rows (by At)
// and renders them as SettingsUsageEntry rows. Rows with a zero At are
// sorted last so legacy imports never steal the top slots in the
// "最近记录" list.
func buildUsageEntries(rows []insight.TokenUsageRow, titleByID map[string]string, limit int) []SettingsUsageEntry {
	type sortable struct {
		row insight.TokenUsageRow
		ts  time.Time
	}
	items := make([]sortable, 0, len(rows))
	for _, r := range rows {
		items = append(items, sortable{row: r, ts: r.At})
	}
	sort.Slice(items, func(i, j int) bool {
		iZero, jZero := items[i].ts.IsZero(), items[j].ts.IsZero()
		if iZero != jZero {
			return !iZero
		}
		if items[i].ts.Equal(items[j].ts) {
			return items[i].row.SessionID < items[j].row.SessionID
		}
		return items[i].ts.After(items[j].ts)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	entries := make([]SettingsUsageEntry, 0, len(items))
	for _, it := range items {
		r := it.row
		title := titleByID[r.SessionID]
		if title == "" {
			title = r.SessionID
		}
		entries = append(entries, SettingsUsageEntry{
			ID:                  "turn:" + r.SessionID + "@" + r.At.Format(time.RFC3339Nano),
			Source:              "turn",
			Title:               title,
			Provider:            r.Provider,
			Model:               r.Model,
			At:                  r.At.UTC().Format(time.RFC3339Nano),
			InputTokens:         r.InputTokens,
			OutputTokens:        r.OutputTokens,
			CacheCreationTokens: r.CacheCreationTokens,
			CacheReadTokens:     r.CacheReadTokens,
		})
	}
	return entries
}

// truncateUsageTitle shortens a session's first user message down to a
// reasonable card headline; the desktop may trim further before display.
func truncateUsageTitle(s string) string {
	const max = 60
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func requestContextSystemSections(sections []agent.SystemPromptSectionInfo) []sessiontrace.SystemSectionRecord {
	if len(sections) == 0 {
		return nil
	}
	out := make([]sessiontrace.SystemSectionRecord, 0, len(sections))
	for _, section := range sections {
		out = append(out, sessiontrace.SystemSectionRecord{
			Key:    section.Key,
			Static: section.Static,
			Bytes:  section.Bytes,
			Hash:   section.Hash,
		})
	}
	return out
}

func cloneStringIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// validateSettingsUsageRange rejects unknown range strings so the
// desktop gets a clear error instead of a silently empty snapshot.
func validateSettingsUsageRange(r SettingsUsageRange) error {
	switch r {
	case SettingsUsageRangeAll, SettingsUsageRange7d, SettingsUsageRange30d, SettingsUsageRange90d:
		return nil
	}
	return fmt.Errorf("invalid settings/usage range %q", r)
}

// settingsUsageRangeCutoff returns the exclusive lower-bound timestamp
// for the requested range. "all" (and the empty string) returns nil,
// meaning no time filter applies.
func settingsUsageRangeCutoff(r SettingsUsageRange, now time.Time) *time.Time {
	switch r {
	case SettingsUsageRangeAll, "":
		return nil
	case SettingsUsageRange7d:
		c := now.AddDate(0, 0, -7)
		return &c
	case SettingsUsageRange30d:
		c := now.AddDate(0, 0, -30)
		return &c
	case SettingsUsageRange90d:
		c := now.AddDate(0, 0, -90)
		return &c
	}
	return nil
}
