package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// fakeStep is a programmable Step implementation for loop tests.
// Each Execute call pops the next entry from results / errs.
type fakeStep struct {
	results []StepResult
	errs    []error // optional, indexed parallel to results
	calls   []providers.ChatRequest
	idx     int
}

func (f *fakeStep) Execute(_ context.Context, req providers.ChatRequest) (StepResult, error) {
	f.calls = append(f.calls, req)
	if f.idx >= len(f.results) {
		return StepResult{}, errors.New("fakeStep: unexpected extra call")
	}
	r := f.results[f.idx]
	var err error
	if f.idx < len(f.errs) {
		err = f.errs[f.idx]
	}
	f.idx++
	if err != nil {
		return StepResult{}, err
	}
	return r, nil
}

// fakeLoopTools is a no-op ToolExecutor that records every call.
type fakeLoopTools struct {
	mu      sync.Mutex
	defs    []providers.ToolDefinition
	results map[string]string // call.ID → JSON result
	calls   []providers.ToolCall
	err     error
}

func (f *fakeLoopTools) Definitions() []providers.ToolDefinition { return f.defs }
func (f *fakeLoopTools) Execute(_ context.Context, call providers.ToolCall) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	if f.err != nil {
		return "", f.err
	}
	if r, ok := f.results[call.ID]; ok {
		return r, nil
	}
	return `{"ok":true}`, nil
}

func (f *fakeLoopTools) recordedCalls() []providers.ToolCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]providers.ToolCall, len(f.calls))
	copy(out, f.calls)
	return out
}

type contextLoopTools struct {
	defs []providers.ToolDefinition

	calls []providers.ToolCall
	last  string
}

func (f *contextLoopTools) Definitions() []providers.ToolDefinition { return f.defs }
func (f *contextLoopTools) Execute(_ context.Context, call providers.ToolCall) (string, error) {
	f.calls = append(f.calls, call)
	f.last = "context for " + call.ID
	return `{"ok":"` + call.ID + `"}`, nil
}
func (f *contextLoopTools) LastAdditionalContext() string { return f.last }

type delayedConcurrentTools struct {
	call2Done chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	completed []string
}

func newDelayedConcurrentTools() *delayedConcurrentTools {
	return &delayedConcurrentTools{call2Done: make(chan struct{})}
}

func (f *delayedConcurrentTools) Definitions() []providers.ToolDefinition {
	return []providers.ToolDefinition{{Name: "read_file"}}
}

func (f *delayedConcurrentTools) ToolMetadata(_ providers.ToolCall) (ToolMetadata, bool) {
	return ToolMetadata{ReadOnly: true, ConcurrencySafe: true}, true
}

func (f *delayedConcurrentTools) Execute(_ context.Context, call providers.ToolCall) (string, error) {
	switch call.ID {
	case "call_1":
		select {
		case <-f.call2Done:
		case <-time.After(time.Second):
			return "", errors.New("call_1 timed out waiting for call_2")
		}
		f.recordCompleted(call.ID)
	case "call_2":
		f.recordCompleted(call.ID)
		f.closeOnce.Do(func() { close(f.call2Done) })
	default:
		f.recordCompleted(call.ID)
	}
	return `{"ok":"` + call.ID + `"}`, nil
}

func (f *delayedConcurrentTools) recordCompleted(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed = append(f.completed, id)
}

func (f *delayedConcurrentTools) completedOrder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.completed))
	copy(out, f.completed)
	return out
}

type argumentAwareMetadataTools struct{}

func (argumentAwareMetadataTools) Definitions() []providers.ToolDefinition { return nil }
func (argumentAwareMetadataTools) Execute(context.Context, providers.ToolCall) (string, error) {
	return `{"ok":true}`, nil
}
func (argumentAwareMetadataTools) ToolMetadata(call providers.ToolCall) (ToolMetadata, bool) {
	if strings.Contains(call.Arguments, "safe") {
		return ToolMetadata{ReadOnly: true, ConcurrencySafe: true}, true
	}
	return ToolMetadata{ReadOnly: false, ConcurrencySafe: false}, true
}

func userMsg(content string) providers.ChatMessage {
	return providers.ChatMessage{Role: "user", Content: content}
}

func TestPartitionToolCallsUsesCallArguments(t *testing.T) {
	calls := []providers.ToolCall{
		{ID: "safe_1", Name: "run_shell", Arguments: `{"kind":"safe"}`},
		{ID: "unsafe", Name: "run_shell", Arguments: `{"kind":"write"}`},
		{ID: "safe_2", Name: "run_shell", Arguments: `{"kind":"safe"}`},
	}

	batches := partitionToolCalls(argumentAwareMetadataTools{}, calls)
	if len(batches) != 3 {
		t.Fatalf("expected 3 batches, got %+v", batches)
	}
	if !batches[0].concurrent || batches[0].calls[0].ID != "safe_1" {
		t.Fatalf("first call should be concurrent based on arguments, got %+v", batches[0])
	}
	if batches[1].concurrent || batches[1].calls[0].ID != "unsafe" {
		t.Fatalf("second call should be serial based on arguments, got %+v", batches[1])
	}
	if !batches[2].concurrent || batches[2].calls[0].ID != "safe_2" {
		t.Fatalf("third call should be concurrent based on arguments, got %+v", batches[2])
	}
}

func TestRunToolLoop_SimpleAnswer(t *testing.T) {
	step := &fakeStep{results: []StepResult{{Content: "hello back"}}}
	res, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("hi")}, LoopConfig{Model: "m"}, step)
	if err != nil {
		t.Fatalf("loop error: %v", err)
	}
	if res.Content != "hello back" {
		t.Fatalf("got content %q", res.Content)
	}
	if len(res.NewMessages) != 1 || res.NewMessages[0].Role != "assistant" {
		t.Fatalf("unexpected new messages: %+v", res.NewMessages)
	}
}

func TestRunToolLoop_BuildsCacheHintFromHistory(t *testing.T) {
	step := &fakeStep{results: []StepResult{{Content: "ok"}}}
	history := []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "latest"},
	}
	_, err := RunToolLoop(context.Background(), history, LoopConfig{Model: "m"}, step)
	if err != nil {
		t.Fatal(err)
	}
	if len(step.calls) != 1 {
		t.Fatalf("expected one call, got %d", len(step.calls))
	}
	hint := step.calls[0].CacheHint
	if hint == nil {
		t.Fatal("expected cache hint")
	}
	if !hint.StableSystem {
		t.Fatal("expected StableSystem=true")
	}
	if hint.StablePrefixMessages != 2 {
		t.Fatalf("expected stable prefix size 2, got %d", hint.StablePrefixMessages)
	}
	if hint.PromptCacheKey == "" {
		t.Fatal("expected prompt cache key")
	}
}

func TestRunToolLoop_CompactRewritePromotesSummaryIntoCacheHint(t *testing.T) {
	step := &fakeStep{results: []StepResult{
		{ToolCalls: []providers.ToolCall{{ID: "c1", Name: "t", Arguments: `{}`}}, Usage: &providers.TokenUsage{InputTokens: 950}},
		{Content: "done"},
	}}
	cfg := LoopConfig{
		Model: "m",
		Tools: &fakeLoopTools{defs: []providers.ToolDefinition{{Name: "t"}}},
		Compact: func(_ context.Context, _ []providers.ChatMessage) ([]providers.ChatMessage, error) {
			return []providers.ChatMessage{
				{Role: "system", Content: "[Conversation summary]\nOlder turns were compacted."},
				{Role: "user", Content: "latest ask"},
			}, nil
		},
		MaxContextTokens: 1000,
	}

	_, err := RunToolLoop(context.Background(), []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "old ask"},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "latest ask"},
	}, cfg, step)
	if err != nil {
		t.Fatal(err)
	}
	if len(step.calls) != 2 {
		t.Fatalf("expected 2 step calls, got %d", len(step.calls))
	}
	secondHint := step.calls[1].CacheHint
	if secondHint == nil {
		t.Fatal("expected cache hint after compact")
	}
	if !secondHint.HasCompactSummary {
		t.Fatal("expected compact summary flag after rewrite")
	}
	if secondHint.StablePrefixMessages != 0 {
		t.Fatalf("expected current turn to remain volatile after rewrite, got %d", secondHint.StablePrefixMessages)
	}
	if !secondHint.StableSystem {
		t.Fatal("expected summary system message to stay cacheable")
	}
	if secondHint.PromptCacheKey == "" {
		t.Fatal("expected prompt cache key after compact")
	}
	if step.calls[1].Messages[0].Content != "[Conversation summary]\nOlder turns were compacted." {
		t.Fatalf("expected compact summary at request root, got %+v", step.calls[1].Messages[0])
	}
}

func TestRunToolLoop_ToolCallThenAnswer(t *testing.T) {
	step := &fakeStep{results: []StepResult{
		{ToolCalls: []providers.ToolCall{{ID: "c1", Name: "run_shell", Arguments: `{}`}}},
		{Content: "tool said ok, here is your answer"},
	}}
	tools := &fakeLoopTools{
		defs:    []providers.ToolDefinition{{Name: "run_shell"}},
		results: map[string]string{"c1": `{"ok":true}`},
	}
	cfg := LoopConfig{Model: "m", Tools: tools}

	var seenCalls []providers.ToolCall
	cfg.OnToolResult = func(call providers.ToolCall, _ string) {
		seenCalls = append(seenCalls, call)
	}

	res, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("do thing")}, cfg, step)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "tool said ok, here is your answer" {
		t.Fatalf("got %q", res.Content)
	}
	if len(tools.calls) != 1 || tools.calls[0].ID != "c1" {
		t.Fatalf("unexpected tool calls: %+v", tools.calls)
	}
	if len(seenCalls) != 1 {
		t.Fatalf("expected OnToolResult to fire once, got %d", len(seenCalls))
	}
	roles := []string{}
	for _, m := range res.NewMessages {
		roles = append(roles, m.Role)
	}
	if strings.Join(roles, ",") != "assistant,tool,assistant" {
		t.Fatalf("unexpected message order: %v", roles)
	}
}

func TestRunToolLoop_AppendsAllToolResultsBeforeFollowupContext(t *testing.T) {
	step := &fakeStep{results: []StepResult{
		{ToolCalls: []providers.ToolCall{
			{ID: "call_1", Name: "read_file", Arguments: `{}`},
			{ID: "call_2", Name: "grep", Arguments: `{}`},
			{ID: "call_3", Name: "read_file", Arguments: `{}`},
		}},
		{Content: "done"},
	}}
	tools := &contextLoopTools{defs: []providers.ToolDefinition{
		{Name: "read_file"},
		{Name: "grep"},
	}}

	res, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("inspect")}, LoopConfig{
		Model: "m",
		Tools: tools,
	}, step)
	if err != nil {
		t.Fatal(err)
	}
	if err := providers.ValidateMessageSequence(res.NewMessages); err != nil {
		t.Fatalf("expected valid returned message sequence, got %v: %+v", err, res.NewMessages)
	}
	roles := make([]string, 0, len(res.NewMessages))
	for _, msg := range res.NewMessages {
		roles = append(roles, msg.Role)
	}
	if got, want := strings.Join(roles, ","), "assistant,tool,tool,tool,user,user,user,assistant"; got != want {
		t.Fatalf("unexpected returned message order: got %s want %s", got, want)
	}
	if len(step.calls) != 2 {
		t.Fatalf("expected two provider requests, got %d", len(step.calls))
	}
	requestRoles := make([]string, 0, len(step.calls[1].Messages))
	for _, msg := range step.calls[1].Messages {
		requestRoles = append(requestRoles, msg.Role)
	}
	if got, want := strings.Join(requestRoles, ","), "user,assistant,tool,tool,tool,user,user,user"; got != want {
		t.Fatalf("unexpected second request order: got %s want %s", got, want)
	}
	for i, wantID := range []string{"call_1", "call_2", "call_3"} {
		msg := step.calls[1].Messages[2+i]
		if msg.Role != "tool" || msg.ToolCallID != wantID {
			t.Fatalf("tool result %d: got %+v want call_id %s", i, msg, wantID)
		}
	}
}

func TestRunToolLoop_ConcurrentToolCompletionDoesNotReorderProviderMessages(t *testing.T) {
	step := &fakeStep{results: []StepResult{
		{ToolCalls: []providers.ToolCall{
			{ID: "call_1", Name: "read_file", Arguments: `{}`},
			{ID: "call_2", Name: "read_file", Arguments: `{}`},
		}},
		{Content: "done"},
	}}
	tools := newDelayedConcurrentTools()

	res, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("inspect")}, LoopConfig{
		Model: "m",
		Tools: tools,
	}, step)
	if err != nil {
		t.Fatal(err)
	}
	if err := providers.ValidateMessageSequence(res.NewMessages); err != nil {
		t.Fatalf("expected valid returned message sequence, got %v: %+v", err, res.NewMessages)
	}
	if got := strings.Join(tools.completedOrder(), ","); got != "call_2,call_1" {
		t.Fatalf("test did not simulate out-of-order completion: got %s", got)
	}
	if len(step.calls) != 2 {
		t.Fatalf("expected two provider requests, got %d", len(step.calls))
	}
	var gotToolIDs []string
	for _, msg := range step.calls[1].Messages {
		if msg.Role == "tool" {
			gotToolIDs = append(gotToolIDs, msg.ToolCallID)
		}
	}
	if got, want := strings.Join(gotToolIDs, ","), "call_1,call_2"; got != want {
		t.Fatalf("provider request tool order changed with completion order: got %s want %s", got, want)
	}
}

func TestRunToolLoop_TruncationRecovery(t *testing.T) {
	step := &fakeStep{results: []StepResult{
		{Content: "part1 ", Truncated: true, StopReason: "length"},
		{Content: "part2 ", Truncated: true, StopReason: "max_tokens"},
		{Content: "done."},
	}}
	res, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("write story")}, LoopConfig{Model: "m"}, step)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "part1 part2 done." {
		t.Fatalf("expected concatenated content, got %q", res.Content)
	}
	if len(step.calls) != 3 {
		t.Fatalf("expected 3 step calls, got %d", len(step.calls))
	}
	final := step.calls[2].Messages
	continues := 0
	for _, m := range final {
		if m.Role == "user" && m.Content == truncationContinuePrompt {
			continues++
		}
	}
	if continues != 2 {
		t.Fatalf("expected 2 continue prompts in final request, got %d", continues)
	}
}

func TestRunToolLoop_TruncationCappedReturnsPartial(t *testing.T) {
	results := make([]StepResult, 0, maxTruncationRecoveries+1)
	for i := 0; i <= maxTruncationRecoveries; i++ {
		results = append(results, StepResult{Content: "x", Truncated: true, StopReason: "length"})
	}
	step := &fakeStep{results: results}
	res, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("loop")}, LoopConfig{Model: "m"}, step)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "xxxx" {
		t.Fatalf("got %q", res.Content)
	}
}

func TestRunToolLoop_ContextOverflowAutoCompact(t *testing.T) {
	overflow := &providers.HTTPError{StatusCode: 400, Body: "context_length_exceeded", ContextOverflow: true}
	step := &fakeStep{results: []StepResult{{}, {Content: "ok"}}, errs: []error{overflow, nil}}
	compactCalled := 0
	compactFn := func(_ context.Context, msgs []providers.ChatMessage) ([]providers.ChatMessage, error) {
		compactCalled++
		return msgs[len(msgs)-1:], nil
	}
	cfg := LoopConfig{Model: "m", Compact: compactFn}

	res, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("big")}, cfg, step)
	if err != nil {
		t.Fatalf("loop error: %v", err)
	}
	if res.Content != "ok" {
		t.Fatalf("expected ok, got %q", res.Content)
	}
	if compactCalled != 1 {
		t.Fatalf("expected compact called once, got %d", compactCalled)
	}
}

func TestRunToolLoop_ContextOverflowOnlyRetriesOnce(t *testing.T) {
	overflow := &providers.HTTPError{StatusCode: 400, Body: "context_length_exceeded", ContextOverflow: true}
	step := &fakeStep{results: []StepResult{{}, {}}, errs: []error{overflow, overflow}}
	cfg := LoopConfig{Model: "m", Compact: func(_ context.Context, m []providers.ChatMessage) ([]providers.ChatMessage, error) { return m, nil }}

	_, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("big")}, cfg, step)
	if err == nil {
		t.Fatal("expected second overflow to surface")
	}
	if !providers.IsContextOverflow(err) {
		t.Fatalf("expected context-overflow error, got %v", err)
	}
}

func TestRunToolLoop_MaxStepsExceeded(t *testing.T) {
	step := &fakeStep{results: []StepResult{{ToolCalls: []providers.ToolCall{{ID: "a", Name: "t", Arguments: `{}`}}}}}
	cfg := LoopConfig{Model: "m", Tools: &fakeLoopTools{defs: []providers.ToolDefinition{{Name: "t"}}}, MaxSteps: 1}
	_, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("loop")}, cfg, step)
	if err == nil {
		t.Fatal("expected max-steps error")
	}
	if !strings.Contains(err.Error(), "max steps exceeded") {
		t.Fatalf("got %v", err)
	}
}

func TestRunToolLoop_ZeroMaxStepsIsUnlimited(t *testing.T) {
	const rounds = 12
	results := make([]StepResult, 0, rounds+1)
	for i := 0; i < rounds; i++ {
		results = append(results, StepResult{ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("c%d", i), Name: "t", Arguments: `{}`}}})
	}
	results = append(results, StepResult{Content: "all done"})

	step := &fakeStep{results: results}
	cfg := LoopConfig{Model: "m", Tools: &fakeLoopTools{defs: []providers.ToolDefinition{{Name: "t"}}}}
	res, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("long")}, cfg, step)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "all done" {
		t.Fatalf("got %q", res.Content)
	}
}

func TestRunToolLoop_OnUsageReceivesPerCall(t *testing.T) {
	step := &fakeStep{results: []StepResult{{Content: "done", Usage: &providers.TokenUsage{InputTokens: 10, OutputTokens: 5}}}}
	var seenIn, seenOut int
	cfg := LoopConfig{Model: "m", OnUsage: func(in, out int) { seenIn += in; seenOut += out }}
	res, _ := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("hi")}, cfg, step)
	if seenIn != 10 || seenOut != 5 {
		t.Fatalf("OnUsage missed: in=%d out=%d", seenIn, seenOut)
	}
	if res.InputTokens != 10 || res.OutputTokens != 5 {
		t.Fatalf("LoopResult totals wrong: %+v", res)
	}
}

func TestRunToolLoop_ProactiveCompactTriggers(t *testing.T) {
	step := &fakeStep{results: []StepResult{{ToolCalls: []providers.ToolCall{{ID: "c1", Name: "t", Arguments: `{}`}}, Usage: &providers.TokenUsage{InputTokens: 950, OutputTokens: 0}}, {Content: "compacted answer"}}}
	tools := &fakeLoopTools{defs: []providers.ToolDefinition{{Name: "t"}}}

	compactCalled := 0
	compactFn := func(_ context.Context, msgs []providers.ChatMessage) ([]providers.ChatMessage, error) {
		compactCalled++
		return []providers.ChatMessage{{Role: "user", Content: "summary"}}, nil
	}
	var compactInfos []CompactInfo
	cfg := LoopConfig{Model: "m", Tools: tools, Compact: compactFn, MaxContextTokens: 1000, OnCompact: func(info CompactInfo) { compactInfos = append(compactInfos, info) }}

	res, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("hi")}, cfg, step)
	if err != nil {
		t.Fatalf("loop error: %v", err)
	}
	if compactCalled != 1 {
		t.Fatalf("expected 1 proactive compact, got %d", compactCalled)
	}
	if len(compactInfos) != 1 {
		t.Fatalf("expected 1 OnCompact callback, got %d", len(compactInfos))
	}
	if compactInfos[0].Reason != CompactReasonProactive {
		t.Fatalf("expected proactive reason, got %q", compactInfos[0].Reason)
	}
	if compactInfos[0].MessagesAfter >= compactInfos[0].MessagesBefore {
		t.Fatalf("expected MessagesAfter < MessagesBefore, got %+v", compactInfos[0])
	}
	if res.Content != "compacted answer" {
		t.Fatalf("expected compacted answer, got %q", res.Content)
	}
	if !res.HistoryRewritten {
		t.Fatal("expected history rewritten after proactive compact")
	}
	if len(res.NewMessages) != 2 {
		t.Fatalf("expected full compacted history snapshot, got %d messages", len(res.NewMessages))
	}
	if res.NewMessages[0].Role != "user" || res.NewMessages[0].Content != "summary" {
		t.Fatalf("expected compacted snapshot to start with summary message, got %+v", res.NewMessages[0])
	}
	if res.NewMessages[1].Role != "assistant" || res.NewMessages[1].Content != "compacted answer" {
		t.Fatalf("expected compacted answer in snapshot, got %+v", res.NewMessages[1])
	}
}

func TestRunToolLoop_PreRequestCompactRequiresGroundTruthUsage(t *testing.T) {
	history := []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: strings.Repeat("seed ", 80)},
		{Role: "assistant", Content: strings.Repeat("seed ", 80)},
		{Role: "user", Content: strings.Repeat("seed ", 80)},
		{Role: "assistant", Content: strings.Repeat("seed ", 80)},
	}
	step := &fakeStep{results: []StepResult{{Content: "ok"}}}
	compactCalled := 0
	cfg := LoopConfig{
		Model: "m",
		Compact: func(_ context.Context, msgs []providers.ChatMessage) ([]providers.ChatMessage, error) {
			compactCalled++
			return msgs[:2], nil
		},
		MaxContextTokens: 10,
	}

	res, err := RunToolLoop(context.Background(), history, cfg, step)
	if err != nil {
		t.Fatal(err)
	}
	if compactCalled != 0 {
		t.Fatalf("expected no pre-request compact without ground-truth usage, got %d", compactCalled)
	}
	if len(step.calls) != 1 {
		t.Fatalf("expected one provider call, got %d", len(step.calls))
	}
	if len(step.calls[0].Messages) != len(history) {
		t.Fatalf("expected original history to be sent unchanged, got %d messages", len(step.calls[0].Messages))
	}
	if res.Content != "ok" {
		t.Fatalf("unexpected content %q", res.Content)
	}
}

func TestRunToolLoop_PreRequestCompactUsesSharedUsageTracker(t *testing.T) {
	history := []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "follow up"},
	}
	step := &fakeStep{results: []StepResult{{Content: "ok"}}}
	tracker := NewUsageTracker()
	tracker.RecordResponse(&providers.TokenUsage{InputTokens: 950})
	tracker.RecordPendingMessages(history[len(history)-1:])

	compactCalled := 0
	cfg := LoopConfig{
		Model: "m",
		Compact: func(_ context.Context, _ []providers.ChatMessage) ([]providers.ChatMessage, error) {
			compactCalled++
			return []providers.ChatMessage{
				{Role: "system", Content: "[Conversation summary]\nOlder turns"},
				{Role: "user", Content: "follow up"},
			}, nil
		},
		MaxContextTokens: 1000,
		UsageTracker:     tracker,
	}

	res, err := RunToolLoop(context.Background(), history, cfg, step)
	if err != nil {
		t.Fatal(err)
	}
	if compactCalled != 1 {
		t.Fatalf("expected one pre-request compact, got %d", compactCalled)
	}
	if len(step.calls) != 1 {
		t.Fatalf("expected one provider call, got %d", len(step.calls))
	}
	if got := step.calls[0].Messages[0].Content; got != "[Conversation summary]\nOlder turns" {
		t.Fatalf("expected compacted request root, got %q", got)
	}
	if !res.HistoryRewritten {
		t.Fatal("expected history rewrite after pre-request compact")
	}
}

func TestRunToolLoop_ProactiveCompactDisabledWhenNoWindow(t *testing.T) {
	step := &fakeStep{results: []StepResult{{Content: "done", Usage: &providers.TokenUsage{InputTokens: 1_000_000, OutputTokens: 0}}}}
	compactCalled := 0
	cfg := LoopConfig{Model: "m", Compact: func(_ context.Context, m []providers.ChatMessage) ([]providers.ChatMessage, error) {
		compactCalled++
		return m, nil
	}}
	_, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("hi")}, cfg, step)
	if err != nil {
		t.Fatal(err)
	}
	if compactCalled != 0 {
		t.Fatalf("proactive compact should be disabled, but ran %d times", compactCalled)
	}
}

func TestRunToolLoop_ProactiveCompactRespectsCustomThreshold(t *testing.T) {
	step := &fakeStep{results: []StepResult{{ToolCalls: []providers.ToolCall{{ID: "c", Name: "t", Arguments: `{}`}}, Usage: &providers.TokenUsage{InputTokens: 600}}, {Content: "ok"}}}
	compactCalled := 0
	cfg := LoopConfig{Model: "m", Tools: &fakeLoopTools{defs: []providers.ToolDefinition{{Name: "t"}}}, Compact: func(_ context.Context, m []providers.ChatMessage) ([]providers.ChatMessage, error) {
		compactCalled++
		return []providers.ChatMessage{{Role: "user", Content: "sum"}}, nil
	}, MaxContextTokens: 1000, CompactThresholdPct: 0.5}
	_, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("hi")}, cfg, step)
	if err != nil {
		t.Fatal(err)
	}
	if compactCalled != 1 {
		t.Fatalf("expected proactive compact at 50%% threshold, got %d", compactCalled)
	}
}

func TestRunToolLoop_ProactiveCompactDoesNotLoopOnNoOpCompact(t *testing.T) {
	step := &fakeStep{results: []StepResult{{ToolCalls: []providers.ToolCall{{ID: "c1", Name: "t", Arguments: `{}`}}, Usage: &providers.TokenUsage{InputTokens: 950}}, {ToolCalls: []providers.ToolCall{{ID: "c2", Name: "t", Arguments: `{}`}}, Usage: &providers.TokenUsage{InputTokens: 950}}, {Content: "done"}}}
	compactCalled := 0
	cfg := LoopConfig{Model: "m", Tools: &fakeLoopTools{defs: []providers.ToolDefinition{{Name: "t"}}}, Compact: func(_ context.Context, m []providers.ChatMessage) ([]providers.ChatMessage, error) {
		compactCalled++
		return m, nil
	}, MaxContextTokens: 1000}
	_, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("hi")}, cfg, step)
	if err != nil {
		t.Fatal(err)
	}
	if compactCalled < 1 {
		t.Fatalf("expected at least one compact attempt, got %d", compactCalled)
	}
}

func TestRunToolLoop_OverflowCompactFiresOnCompactCallback(t *testing.T) {
	overflow := &providers.HTTPError{StatusCode: 400, Body: "context_length_exceeded", ContextOverflow: true}
	step := &fakeStep{results: []StepResult{{}, {Content: "ok"}}, errs: []error{overflow, nil}}
	var infos []CompactInfo
	cfg := LoopConfig{Model: "m", Compact: func(_ context.Context, m []providers.ChatMessage) ([]providers.ChatMessage, error) {
		return m[len(m)-1:], nil
	}, OnCompact: func(info CompactInfo) { infos = append(infos, info) }}
	_, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("big")}, cfg, step)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Reason != CompactReasonOverflow {
		t.Fatalf("expected one overflow OnCompact, got %+v", infos)
	}
}

func TestRunToolLoop_BeforeStepInjectsMessages(t *testing.T) {
	step := &fakeStep{results: []StepResult{{Content: "ok"}}}
	injected := false
	cfg := LoopConfig{Model: "m", BeforeStep: func() []providers.ChatMessage {
		if injected {
			return nil
		}
		injected = true
		return []providers.ChatMessage{{Role: "user", Content: "follow-up"}}
	}}
	_, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("hi")}, cfg, step)
	if err != nil {
		t.Fatal(err)
	}
	if len(step.calls) != 1 {
		t.Fatalf("expected one step call, got %d", len(step.calls))
	}
	msgs := step.calls[0].Messages
	if len(msgs) != 2 {
		t.Fatalf("expected injected message in request, got %d messages", len(msgs))
	}
	if msgs[1].Role != "user" || msgs[1].Content != "follow-up" {
		t.Fatalf("unexpected injected message: %+v", msgs[1])
	}
}

func TestRunToolLoop_EmptyAnswerWithoutStopReasonIsError(t *testing.T) {
	step := &fakeStep{results: []StepResult{{Content: "  "}}}
	_, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("hi")}, LoopConfig{Model: "m"}, step)
	if err == nil || !IsEmptyAnswer(err) {
		t.Fatalf("expected EmptyAnswerError, got %v", err)
	}
}

func TestRunToolLoop_EmptyAnswerCarriesStopReason(t *testing.T) {
	step := &fakeStep{results: []StepResult{{Content: "", StopReason: "stop"}}}
	_, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("hi")}, LoopConfig{Model: "m"}, step)
	if err == nil || !IsEmptyAnswer(err) {
		t.Fatalf("expected EmptyAnswerError, got %v", err)
	}
	var emptyErr *EmptyAnswerError
	if !errors.As(err, &emptyErr) || emptyErr.StopReason != "stop" {
		t.Fatalf("expected StopReason=stop, got %+v", emptyErr)
	}
}

func TestRunToolLoop_EmptyAnswerWithNaturalStopReasonSucceeds(t *testing.T) {
	step := &fakeStep{results: []StepResult{{Content: "  ", StopReason: "end_turn"}}}
	res, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("hi")}, LoopConfig{Model: "m"}, step)
	if err != nil {
		t.Fatalf("expected empty completion to succeed, got %v", err)
	}
	if res.Content != "" {
		t.Fatalf("expected empty final content, got %q", res.Content)
	}
	if len(res.NewMessages) != 0 {
		t.Fatalf("expected no persisted empty assistant message, got %+v", res.NewMessages)
	}
}

func TestRunToolLoop_ReasoningOnlyAnswerStillPersistsAssistantMessage(t *testing.T) {
	step := &fakeStep{results: []StepResult{{
		Content:          " ",
		ReasoningContent: "inspect repo before reply",
		StopReason:       "end_turn",
	}}}
	res, err := RunToolLoop(context.Background(), []providers.ChatMessage{userMsg("hi")}, LoopConfig{Model: "m"}, step)
	if err != nil {
		t.Fatalf("expected reasoning-only completion to succeed, got %v", err)
	}
	if len(res.NewMessages) != 1 {
		t.Fatalf("expected reasoning-only assistant message to persist, got %+v", res.NewMessages)
	}
	if got := res.NewMessages[0].ReasoningContent; got != "inspect repo before reply" {
		t.Fatalf("unexpected reasoning content: %q", got)
	}
}

func TestRunToolLoop_RejectsProviderToolCallWithoutID(t *testing.T) {
	step := &fakeStep{results: []StepResult{{ToolCalls: []providers.ToolCall{{ID: "", Name: "t", Arguments: `{}`}}}}}
	_, err := RunToolLoop(context.Background(), nil, LoopConfig{Model: "m"}, step)
	if err == nil {
		t.Fatal("expected invalid tool_call error")
	}
	if !strings.Contains(err.Error(), "provider returned invalid tool_calls") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunToolLoop_RejectsDuplicateProviderToolCallIDs(t *testing.T) {
	step := &fakeStep{results: []StepResult{{ToolCalls: []providers.ToolCall{
		{ID: "call_1", Name: "a", Arguments: `{}`},
		{ID: "call_1", Name: "b", Arguments: `{}`},
	}}}}
	_, err := RunToolLoop(context.Background(), nil, LoopConfig{Model: "m"}, step)
	if err == nil {
		t.Fatal("expected duplicate tool_call id error")
	}
	if !strings.Contains(err.Error(), "provider returned invalid tool_calls") {
		t.Fatalf("unexpected error: %v", err)
	}
}
