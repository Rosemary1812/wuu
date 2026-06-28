package agent

import (
	"context"
	"strings"
	"sync"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolctx"
)

type toolRunState int

const (
	toolRunQueued toolRunState = iota
	toolRunRunning
	toolRunDone
)

type toolRun struct {
	call            providers.ToolCall
	order           int
	finalized       bool
	concurrencySafe bool
	streamSafe      bool
	streamStarted   bool

	mu     sync.Mutex
	state  toolRunState
	done   chan struct{}
	cancel context.CancelFunc
	result string
	err    error
}

// TurnToolRuntime owns tool executions for one model turn. Streaming can
// enqueue read-only tools early, and the loop later waits for those same runs
// while executing any remaining final tool calls.
type TurnToolRuntime struct {
	executor ToolExecutor
	sem      chan struct{}

	mu       sync.Mutex
	runs     []*toolRun
	byID     map[string]*toolRun
	canceled bool

	requestContext []ContextSegment
	stepIndex      *int
}

func NewTurnToolRuntime(executor ToolExecutor) *TurnToolRuntime {
	return &TurnToolRuntime{
		executor: executor,
		sem:      make(chan struct{}, maxToolConcurrency),
		byID:     map[string]*toolRun{},
	}
}

func (r *TurnToolRuntime) SetStepIndex(stepIndex int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	value := stepIndex
	r.stepIndex = &value
}

// ObserveStreamEvent records streamed tool blocks and starts safe prefix tools
// as soon as their arguments are complete.
func (r *TurnToolRuntime) ObserveStreamEvent(ctx context.Context, event providers.StreamEvent) {
	if r == nil || r.executor == nil {
		return
	}
	switch event.Type {
	case providers.EventToolUseStart:
		if event.ToolCall == nil || strings.TrimSpace(event.ToolCall.ID) == "" {
			return
		}
		r.addStreamToolStart(event.ToolCall)
	case providers.EventToolUseDelta:
		r.appendStreamToolDelta(event.Content)
	case providers.EventToolUseEnd:
		if event.ToolCall == nil || strings.TrimSpace(event.ToolCall.ID) == "" {
			return
		}
		r.finalizeStreamTool(ctx, event.ToolCall)
	}
}

// Cancel stops any in-flight streaming-started work and prevents additional
// stream-prefix starts for this runtime.
func (r *TurnToolRuntime) Cancel() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.canceled = true
	runs := append([]*toolRun(nil), r.runs...)
	r.mu.Unlock()

	for _, run := range runs {
		run.mu.Lock()
		cancel := run.cancel
		run.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

func (r *TurnToolRuntime) addStreamToolStart(call *providers.ToolCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.canceled {
		return
	}
	if existing := r.byID[call.ID]; existing != nil {
		existing.call.ProviderItemID = call.ProviderItemID
		existing.call.ProviderItemModel = call.ProviderItemModel
		existing.call.Name = call.Name
		existing.call.Kind = call.Kind
		existing.concurrencySafe = toolCanRunConcurrently(r.executor, existing.call)
		existing.streamSafe = toolCanStartDuringStreaming(r.executor, existing.call)
		return
	}
	run := &toolRun{
		call: providers.ToolCall{
			ID:                call.ID,
			ProviderItemID:    call.ProviderItemID,
			ProviderItemModel: call.ProviderItemModel,
			Name:              call.Name,
			Kind:              call.Kind,
		},
		order: len(r.runs),
		done:  make(chan struct{}),
	}
	run.concurrencySafe = toolCanRunConcurrently(r.executor, run.call)
	run.streamSafe = toolCanStartDuringStreaming(r.executor, run.call)
	r.runs = append(r.runs, run)
	r.byID[call.ID] = run
}

func (r *TurnToolRuntime) appendStreamToolDelta(delta string) {
	if delta == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.canceled || len(r.runs) == 0 {
		return
	}
	r.runs[len(r.runs)-1].call.Arguments += delta
}

func (r *TurnToolRuntime) finalizeStreamTool(ctx context.Context, call *providers.ToolCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.canceled {
		return
	}
	run := r.byID[call.ID]
	if run == nil {
		run = &toolRun{
			call: providers.ToolCall{
				ID:                call.ID,
				ProviderItemID:    call.ProviderItemID,
				ProviderItemModel: call.ProviderItemModel,
			},
			order: len(r.runs),
			done:  make(chan struct{}),
		}
		r.runs = append(r.runs, run)
		r.byID[call.ID] = run
	}
	if call.ProviderItemID != "" {
		run.call.ProviderItemID = call.ProviderItemID
		run.call.ProviderItemModel = call.ProviderItemModel
	}
	run.call.Name = call.Name
	run.call.Kind = call.Kind
	if call.Arguments != "" {
		run.call.Arguments = call.Arguments
	}
	run.finalized = run.call.Arguments != ""
	run.concurrencySafe = toolCanRunConcurrently(r.executor, run.call)
	run.streamSafe = toolCanStartDuringStreaming(r.executor, run.call)
	r.startReadyStreamPrefixLocked(ctx)
}

func (r *TurnToolRuntime) startReadyStreamPrefixLocked(ctx context.Context) {
	for _, run := range r.runs {
		if !run.finalized {
			return
		}
		if !run.streamSafe {
			return
		}
		r.startRunLocked(ctx, run, true)
	}
}

func (r *TurnToolRuntime) startRunLocked(ctx context.Context, run *toolRun, streamStarted bool) {
	run.mu.Lock()
	if run.state != toolRunQueued {
		run.mu.Unlock()
		return
	}
	run.state = toolRunRunning
	run.streamStarted = run.streamStarted || streamStarted
	runCtx, cancel := context.WithCancel(ctx)
	if r.stepIndex != nil {
		runCtx = toolctx.WithStepIndex(runCtx, *r.stepIndex)
	}
	run.cancel = cancel
	call := run.call
	run.mu.Unlock()

	go func() {
		select {
		case <-runCtx.Done():
			run.complete("", runCtx.Err())
			return
		default:
		}
		select {
		case r.sem <- struct{}{}:
			defer func() { <-r.sem }()
		case <-runCtx.Done():
			run.complete("", runCtx.Err())
			return
		}
		select {
		case <-runCtx.Done():
			run.complete("", runCtx.Err())
			return
		default:
		}
		result, err := r.executor.Execute(runCtx, call)
		run.complete(result, err)
	}()
}

func (run *toolRun) complete(result string, err error) {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.state == toolRunDone {
		return
	}
	run.result = result
	run.err = err
	run.state = toolRunDone
	close(run.done)
}

func (run *toolRun) wait(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-run.done:
		run.mu.Lock()
		defer run.mu.Unlock()
		return run.result, run.err
	}
}

// ExecuteFinalCalls returns tool messages in the model-requested order. Runs
// that were already started during streaming are awaited; all other calls are
// executed through the same runtime.
func (r *TurnToolRuntime) ExecuteFinalCalls(
	ctx context.Context,
	calls []providers.ToolCall,
	onResult func(providers.ToolCall, string),
) []providers.ChatMessage {
	if r == nil {
		r = NewTurnToolRuntime(nil)
	}
	r.registerFinalCalls(calls)
	batches := partitionToolCalls(r.executor, calls)
	var toolMessages []providers.ChatMessage
	for _, batch := range batches {
		batchResult := r.executeBatch(ctx, batch, onResult)
		toolMessages = append(toolMessages, batchResult.messages...)
		r.requestContext = append(r.requestContext, batchResult.requestContext...)
	}
	return toolMessages
}

// TakeRequestContextSegments returns request-only context produced by tool
// execution and clears it from this turn runtime.
func (r *TurnToolRuntime) TakeRequestContextSegments() []ContextSegment {
	if r == nil || len(r.requestContext) == 0 {
		return nil
	}
	out := append([]ContextSegment(nil), r.requestContext...)
	r.requestContext = nil
	return out
}

func (r *TurnToolRuntime) registerFinalCalls(calls []providers.ToolCall) {
	r.mu.Lock()
	if r.byID == nil {
		r.byID = map[string]*toolRun{}
	}
	finalIDs := map[string]bool{}
	for _, call := range calls {
		if call.ID != "" {
			finalIDs[call.ID] = true
		}
	}
	var orphanCancels []context.CancelFunc
	for id, run := range r.byID {
		if finalIDs[id] {
			continue
		}
		delete(r.byID, id)
		run.mu.Lock()
		if run.streamStarted && run.cancel != nil {
			orphanCancels = append(orphanCancels, run.cancel)
		}
		run.mu.Unlock()
	}
	ordered := make([]*toolRun, 0, len(calls))
	seen := map[string]bool{}
	for i, call := range calls {
		var run *toolRun
		if call.ID != "" && !seen[call.ID] {
			run = r.byID[call.ID]
			seen[call.ID] = true
		}
		if run == nil {
			run = &toolRun{done: make(chan struct{})}
		}
		run.call = call
		run.order = i
		run.finalized = true
		run.concurrencySafe = toolCanRunConcurrently(r.executor, call)
		run.streamSafe = toolCanStartDuringStreaming(r.executor, call)
		ordered = append(ordered, run)
		if call.ID != "" {
			r.byID[call.ID] = run
		}
	}
	r.runs = ordered
	r.mu.Unlock()

	for _, cancel := range orphanCancels {
		cancel()
	}
}

func (r *TurnToolRuntime) executeBatch(
	ctx context.Context,
	batch toolBatch,
	onResult func(providers.ToolCall, string),
) toolBatchResult {
	ctxProvider, hasCtxProvider := r.executor.(ToolContextProvider)

	if !batch.concurrent || len(batch.calls) == 1 {
		msgs := make([]providers.ChatMessage, 0, len(batch.calls))
		requestContext := make([]ContextSegment, 0, len(batch.calls))
		for _, call := range batch.calls {
			result := r.executeOrAwaitRun(ctx, call)
			if onResult != nil {
				onResult(call, result)
			}
			msgs = append(msgs, providers.ChatMessage{
				Role:           "tool",
				Name:           call.Name,
				ToolCallID:     call.ID,
				ToolResultKind: call.Kind,
				Content:        result,
			})
			if hasCtxProvider {
				if extra := ctxProvider.LastAdditionalContext(); extra != "" {
					segment := postToolAdditionalContextSegment(call.Name, extra)
					if len(segment.Messages) > 0 {
						requestContext = append(requestContext, segment)
					}
				}
			}
		}
		return toolBatchResult{messages: msgs, requestContext: requestContext}
	}

	runs := make([]*toolRun, len(batch.calls))
	r.mu.Lock()
	for i, call := range batch.calls {
		run := r.runForCallLocked(call)
		r.startRunLocked(ctx, run, false)
		runs[i] = run
	}
	r.mu.Unlock()

	msgs := make([]providers.ChatMessage, len(batch.calls))
	for i, call := range batch.calls {
		result := r.awaitRunResult(ctx, runs[i], call)
		if onResult != nil {
			onResult(call, result)
		}
		msgs[i] = providers.ChatMessage{
			Role:           "tool",
			Name:           call.Name,
			ToolCallID:     call.ID,
			ToolResultKind: call.Kind,
			Content:        result,
		}
	}
	return toolBatchResult{messages: msgs}
}

type toolBatchResult struct {
	messages       []providers.ChatMessage
	requestContext []ContextSegment
}

func postToolAdditionalContextSegment(toolName, content string) ContextSegment {
	content = strings.TrimSpace(content)
	if content == "" {
		return ContextSegment{}
	}
	title := "Hook context"
	if name := strings.TrimSpace(toolName); name != "" {
		title = "Hook context for " + name
	}
	return RequestOnlyContextBlockSegment([]wuucontext.Block{{
		Kind:    wuucontext.BlockAdditionalContext,
		Title:   title,
		Source:  "hooks.post_tool_use",
		Content: content,
	}})
}

func (r *TurnToolRuntime) executeOrAwaitRun(ctx context.Context, call providers.ToolCall) string {
	r.mu.Lock()
	run := r.runForCallLocked(call)
	r.startRunLocked(ctx, run, false)
	r.mu.Unlock()
	return r.awaitRunResult(ctx, run, call)
}

func (r *TurnToolRuntime) runForCallLocked(call providers.ToolCall) *toolRun {
	if r.byID == nil {
		r.byID = map[string]*toolRun{}
	}
	if call.ID != "" {
		if run := r.byID[call.ID]; run != nil {
			return run
		}
	}
	run := &toolRun{
		call:      call,
		order:     len(r.runs),
		finalized: true,
		done:      make(chan struct{}),
	}
	run.concurrencySafe = toolCanRunConcurrently(r.executor, call)
	run.streamSafe = toolCanStartDuringStreaming(r.executor, call)
	r.runs = append(r.runs, run)
	if call.ID != "" {
		r.byID[call.ID] = run
	}
	return run
}

func (r *TurnToolRuntime) awaitRunResult(ctx context.Context, run *toolRun, call providers.ToolCall) string {
	result, err := run.wait(ctx)
	if err == nil {
		return result
	}
	run.mu.Lock()
	streamStarted := run.streamStarted
	run.mu.Unlock()
	if streamStarted && ctx.Err() == nil {
		result, err = r.executeDirect(ctx, call)
		if err == nil {
			return result
		}
	}
	return errorJSON(err)
}

func (r *TurnToolRuntime) executeDirect(ctx context.Context, call providers.ToolCall) (string, error) {
	if r.executor == nil {
		return "", context.Canceled
	}
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return r.executor.Execute(r.executionContext(ctx), call)
}

func (r *TurnToolRuntime) executionContext(ctx context.Context) context.Context {
	if r == nil {
		return ctx
	}
	r.mu.Lock()
	stepIndex := r.stepIndex
	r.mu.Unlock()
	if stepIndex == nil {
		return ctx
	}
	return toolctx.WithStepIndex(ctx, *stepIndex)
}

func toolCanRunConcurrently(executor ToolExecutor, call providers.ToolCall) bool {
	if executor == nil {
		return false
	}
	mp, ok := executor.(ToolMetadataProvider)
	if !ok {
		return false
	}
	meta, found := mp.ToolMetadata(call)
	return found && meta.ConcurrencySafe
}

func toolCanStartDuringStreaming(executor ToolExecutor, call providers.ToolCall) bool {
	if executor == nil {
		return false
	}
	mp, ok := executor.(ToolMetadataProvider)
	if !ok {
		return false
	}
	meta, found := mp.ToolMetadata(call)
	return found && meta.ReadOnly && meta.ConcurrencySafe
}
