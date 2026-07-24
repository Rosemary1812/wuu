package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/providers"
	sessionstore "github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/sessionmemory"
)

type dreamRecordingJournal struct {
	operations chan providers.InferenceOperation
	workflows  chan providers.InferenceWorkflowTerminalRecord
}

type sessionDreamFakeClient struct {
	responses  []providers.ChatResponse
	errors     []error
	requests   []providers.ChatRequest
	beforeChat func()
}

type dreamStreamFirstClient struct {
	chatCalls   int
	streamCalls int
}

func (c *dreamStreamFirstClient) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	c.chatCalls++
	return providers.ChatResponse{}, errors.New("unary Chat must not be called")
}

func (c *dreamStreamFirstClient) StreamChat(_ context.Context, _ providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	c.streamCalls++
	events := make(chan providers.StreamEvent, 2)
	events <- providers.StreamEvent{Type: providers.EventContentDelta, Content: "Nothing to dream."}
	events <- providers.StreamEvent{Type: providers.EventDone, StopReason: "end_turn"}
	close(events)
	return events, nil
}

func (c *sessionDreamFakeClient) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	if c.beforeChat != nil {
		c.beforeChat()
	}
	c.requests = append(c.requests, req)
	idx := len(c.requests) - 1
	if idx < len(c.errors) && c.errors[idx] != nil {
		return providers.ChatResponse{}, c.errors[idx]
	}
	if idx < len(c.responses) {
		return c.responses[idx], nil
	}
	return providers.ChatResponse{Content: "Nothing to dream."}, nil
}

func makeSessionDreamHistory(userTurns int) []providers.ChatMessage {
	history := make([]providers.ChatMessage, 0, userTurns*2)
	for i := 0; i < userTurns; i++ {
		history = append(history,
			providers.ChatMessage{Role: "user", Content: "user turn"},
			providers.ChatMessage{Role: "assistant", Content: "assistant turn"},
		)
	}
	return history
}

func newSessionDreamScheduler(root, workspaceState string, sessionArtifactDir func() string, intervalDays int, client providers.Client, model string) *sessionDreamScheduler {
	sessionsDir := filepath.Join(workspaceState, "global-sessions")
	for i := 0; i < sessionDreamMinSessions; i++ {
		id := fmt.Sprintf("dream-history-%d", i)
		if _, err := sessionstore.CreateWithMetadata(sessionsDir, id, root); err != nil {
			panic(err)
		}
		if err := sessionstore.AppendHistoryRecords(sessionsDir, id, []sessionstore.HistoryRecord{
			{Role: "user", Content: fmt.Sprintf("durable lesson %d", i)},
			{Role: "assistant", Content: "noted"},
		}); err != nil {
			panic(err)
		}
		if err := sessionstore.UpdateIndex(sessionsDir, id, 2, ""); err != nil {
			panic(err)
		}
	}
	return newSessionDreamSchedulerWithSessions(root, workspaceState, sessionsDir, "", sessionArtifactDir, intervalDays, sessionDreamMinSessions, client, model)
}

func TestBuildSessionDreamMessagesSkipsToolProtocolAndSyntheticUserMessages(t *testing.T) {
	messages := buildSessionDreamMessages([]sessionDreamCandidate{{ID: "session-1"}})

	for _, msg := range messages {
		if msg.Role == "tool" {
			t.Fatalf("tool protocol message should not be included: %+v", messages)
		}
		if !strings.Contains(msg.Content, "session-1") && msg.Role == "user" {
			t.Fatalf("session ID missing from dream request: %+v", messages)
		}
	}
	last := messages[len(messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "Nothing to dream") {
		t.Fatalf("dream prompt missing: %+v", last)
	}
}

func (j *dreamRecordingJournal) PrepareOperation(record providers.InferenceOperationJournalRecord) error {
	select {
	case j.operations <- record.Operation:
	default:
	}
	return nil
}
func (*dreamRecordingJournal) PrepareAttempt(providers.InferenceAttemptJournalRecord) error {
	return nil
}
func (*dreamRecordingJournal) UpsertSubmission(providers.InferenceSubmissionJournalRecord) error {
	return nil
}
func (*dreamRecordingJournal) MarkAttemptFirstEvent(string, string, string, time.Time) error {
	return nil
}
func (*dreamRecordingJournal) CompleteAttempt(providers.InferenceAttemptTerminalRecord) error {
	return nil
}
func (*dreamRecordingJournal) PrepareRecoveryAttempt(context.Context, providers.InferenceRecoveryAttemptJournalRecord) error {
	return nil
}
func (*dreamRecordingJournal) CompleteOperation(providers.InferenceOperationTerminalRecord) error {
	return nil
}
func (j *dreamRecordingJournal) CompleteWorkflow(record providers.InferenceWorkflowTerminalRecord) error {
	select {
	case j.workflows <- record:
	default:
	}
	return nil
}

func TestSessionDreamScheduler_BackgroundRunKeepsInferenceJournal(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string { return sessionArtifact }, 7, nil, "")
	journal := &dreamRecordingJournal{
		operations: make(chan providers.InferenceOperation, 1),
		workflows:  make(chan providers.InferenceWorkflowTerminalRecord, 1),
	}
	runner := &agent.StreamRunner{
		Client: providers.AdaptStreamClient(&sessionDreamFakeClient{
			responses: []providers.ChatResponse{{Content: "Nothing to dream."}},
		}),
		Model:            "test-model",
		InferenceJournal: journal,
	}

	scheduler.AfterTurn(context.Background(), runner, makeSessionDreamHistory(1), agent.LoopResult{Content: "done"})

	select {
	case operation := <-journal.operations:
		if operation.Kind != providers.InferenceOperationMemory || operation.WorkloadProfile != providers.InferenceProfileBestEffort {
			t.Fatalf("dream operation = %+v, want memory/best_effort", operation)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("background dream did not prepare an inference operation in the runner journal")
	}
	select {
	case workflow := <-journal.workflows:
		if workflow.Outcome != providers.InferenceOutcomeSucceeded {
			t.Fatalf("dream workflow completion = %+v", workflow)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("background dream did not complete its inference workflow")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		scheduler.mu.Lock()
		running := scheduler.running
		scheduler.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background dream did not finish after workflow completion")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSessionDreamPrefersNativeStreaming(t *testing.T) {
	client := &dreamStreamFirstClient{}
	workspaceState := t.TempDir()
	job := sessionDreamJob{
		client:             client,
		model:              "test-model",
		rootDir:            t.TempDir(),
		workspaceStateDir:  workspaceState,
		sessionArtifactDir: filepath.Join(workspaceState, "sessions", "session-1"),
		sessions:           []sessionDreamCandidate{{ID: "session-1"}},
		now:                time.Now().UTC(),
	}

	if err := (&sessionDreamScheduler{}).run(context.Background(), job); err != nil {
		t.Fatalf("run dream: %v", err)
	}
	if client.streamCalls != 1 || client.chatCalls != 0 {
		t.Fatalf("dream calls: stream=%d chat=%d, want stream=1 chat=0", client.streamCalls, client.chatCalls)
	}
}

func TestSessionDreamScheduler_BackgroundReportsRunFailure(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string {
		return filepath.Join(workspaceState, "sessions", "session-1")
	}, 7, nil, "")
	reported := make(chan error, 1)
	scheduler.reportError = func(err error) { reported <- err }
	runner := &agent.StreamRunner{
		Client: providers.AdaptStreamClient(&sessionDreamFakeClient{
			errors: []error{errors.New("provider unavailable")},
		}),
		Model: "test-model",
	}

	scheduler.AfterTurn(context.Background(), runner, makeSessionDreamHistory(1), agent.LoopResult{Content: "done"})

	select {
	case err := <-reported:
		if !strings.Contains(err.Error(), "provider unavailable") {
			t.Fatalf("reported error = %v, want provider unavailable", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("background dream failure was not reported")
	}
}

func TestSessionDreamScheduler_UsesDedicatedClientAndModel(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	dedicatedClient := &sessionDreamFakeClient{
		responses: []providers.ChatResponse{{Content: "Nothing to dream."}},
	}
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string {
		return filepath.Join(workspaceState, "sessions", "session-1")
	}, 7, dedicatedClient, "dedicated-model")
	journal := &dreamRecordingJournal{
		operations: make(chan providers.InferenceOperation, 1),
		workflows:  make(chan providers.InferenceWorkflowTerminalRecord, 1),
	}
	runner := &agent.StreamRunner{
		Client: providers.AdaptStreamClient(&sessionDreamFakeClient{
			responses: []providers.ChatResponse{{Content: "Nothing to dream."}},
		}),
		Model:            "runner-model",
		InferenceJournal: journal,
	}

	scheduler.AfterTurn(context.Background(), runner, makeSessionDreamHistory(1), agent.LoopResult{Content: "done"})

	select {
	case <-journal.operations:
	case <-time.After(5 * time.Second):
		t.Fatal("background dream did not prepare an inference operation")
	}
	select {
	case <-journal.workflows:
	case <-time.After(5 * time.Second):
		t.Fatal("background dream did not complete its inference workflow")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		scheduler.mu.Lock()
		running := scheduler.running
		scheduler.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background dream did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(dedicatedClient.requests) == 0 {
		t.Fatal("dedicated client was not used")
	}
	if dedicatedClient.requests[0].Model != "dedicated-model" {
		t.Fatalf("dedicated request model = %q, want dedicated-model", dedicatedClient.requests[0].Model)
	}
}

func TestSessionDreamScheduler_OverridesModelOnRunnerClient(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	runnerClient := &sessionDreamFakeClient{
		responses: []providers.ChatResponse{{Content: "Nothing to dream."}},
	}
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string {
		return filepath.Join(workspaceState, "sessions", "session-1")
	}, 7, nil, "lower-cost-model")
	journal := &dreamRecordingJournal{
		operations: make(chan providers.InferenceOperation, 1),
		workflows:  make(chan providers.InferenceWorkflowTerminalRecord, 1),
	}
	runner := &agent.StreamRunner{
		Client:           providers.AdaptStreamClient(runnerClient),
		Model:            "runner-model",
		InferenceJournal: journal,
	}

	scheduler.AfterTurn(context.Background(), runner, makeSessionDreamHistory(1), agent.LoopResult{Content: "done"})

	select {
	case <-journal.workflows:
	case <-time.After(5 * time.Second):
		t.Fatal("background dream did not complete its inference workflow")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		scheduler.mu.Lock()
		running := scheduler.running
		scheduler.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background dream did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(runnerClient.requests) == 0 {
		t.Fatal("runner client was not used")
	}
	if runnerClient.requests[0].Model != "lower-cost-model" {
		t.Fatalf("dream request model = %q, want lower-cost-model", runnerClient.requests[0].Model)
	}
}

func TestIsPureSessionDreamCancellation(t *testing.T) {
	if !isPureSessionDreamCancellation(fmt.Errorf("stop dream: %w", context.Canceled)) {
		t.Fatal("wrapped context cancellation should be treated as a normal cancellation")
	}
	if isPureSessionDreamCancellation(context.DeadlineExceeded) {
		t.Fatal("dream deadline should be reported as a failed attempt")
	}
	if isPureSessionDreamCancellation(errors.Join(context.Canceled, errors.New("persist failure state"))) {
		t.Fatal("persistence failure joined to cancellation must still be reported")
	}
}

func TestSessionDreamScheduler_ShouldStartRespectsInterval(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string { return sessionArtifact }, 7, nil, "")
	history := makeSessionDreamHistory(1)
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	if !scheduler.shouldStart(history, agent.LoopResult{Content: "done"}, now) {
		t.Fatal("missing dream state should start first dream")
	}
	scheduler.finish()

	if err := sessionmemory.SaveDreamState(workspaceState, sessionmemory.DreamState{LastRunAt: now.Add(-24 * time.Hour)}); err != nil {
		t.Fatalf("SaveDreamState recent: %v", err)
	}
	if scheduler.shouldStart(history, agent.LoopResult{Content: "done"}, now) {
		t.Fatal("recent dream state should not start")
	}

	if err := sessionmemory.SaveDreamState(workspaceState, sessionmemory.DreamState{LastRunAt: now.Add(-8 * 24 * time.Hour)}); err != nil {
		t.Fatalf("SaveDreamState old: %v", err)
	}
	if !scheduler.shouldStart(history, agent.LoopResult{Content: "done"}, now) {
		t.Fatal("old dream state should start")
	}
	scheduler.finish()
}

func TestSessionDreamScheduler_PrepareRequiresTimeAndSessionThresholds(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "active-session")
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string { return sessionArtifact }, 1, nil, "")
	history := makeSessionDreamHistory(1)
	now := time.Now().UTC()

	if err := sessionmemory.SaveDreamState(workspaceState, sessionmemory.DreamState{LastRunAt: now.Add(-23 * time.Hour)}); err != nil {
		t.Fatalf("save recent dream state: %v", err)
	}
	if _, ok, err := scheduler.prepare(history, agent.LoopResult{Content: "done"}, "active-session", now); err != nil || ok {
		t.Fatalf("time gate: ok=%t err=%v, want not eligible", ok, err)
	}

	if err := sessionmemory.SaveDreamState(workspaceState, sessionmemory.DreamState{LastRunAt: now.Add(-25 * time.Hour)}); err != nil {
		t.Fatalf("save elapsed dream state: %v", err)
	}
	if sessions, ok, err := scheduler.prepare(history, agent.LoopResult{Content: "done"}, "dream-history-0", now); err != nil || ok || len(sessions) != 0 {
		t.Fatalf("session gate after excluding current: sessions=%v ok=%t err=%v", sessions, ok, err)
	}

	sessions, ok, err := scheduler.prepare(history, agent.LoopResult{Content: "done"}, "active-session", now)
	if err != nil || !ok || len(sessions) != sessionDreamMinSessions {
		t.Fatalf("combined gates: sessions=%v ok=%t err=%v", sessions, ok, err)
	}
	scheduler.finish()
}

func TestSessionDreamExecutorRestrictsThreadGetToBatch(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionsDir := filepath.Join(workspaceState, "global-sessions")
	for _, id := range []string{"allowed-session", "other-session"} {
		if _, err := sessionstore.CreateWithMetadata(sessionsDir, id, root); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		if err := sessionstore.AppendHistoryRecord(sessionsDir, id, sessionstore.HistoryRecord{Role: "user", Content: id + " content"}); err != nil {
			t.Fatalf("append %s: %v", id, err)
		}
	}
	executor := newSessionDreamExecutor(root, workspaceState, filepath.Join(workspaceState, "sessions", "active"), sessionsDir, []sessionDreamCandidate{{ID: "allowed-session"}})

	result, err := executor.Execute(context.Background(), providers.ToolCall{Name: "thread_get", Arguments: `{"thread_id":"allowed-session"}`})
	if err != nil || !strings.Contains(result, "allowed-session content") {
		t.Fatalf("allowed thread_get: result=%q err=%v", result, err)
	}
	if _, err := executor.Execute(context.Background(), providers.ToolCall{Name: "thread_get", Arguments: `{"thread_id":"other-session"}`}); err == nil || !strings.Contains(err.Error(), "outside this consolidation batch") {
		t.Fatalf("out-of-batch thread_get error = %v", err)
	}
}

func TestSessionDreamScheduler_ShouldStartBacksOffRecentFailure(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string { return sessionArtifact }, 7, nil, "")
	history := makeSessionDreamHistory(1)
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	if err := sessionmemory.SaveDreamState(workspaceState, sessionmemory.DreamState{
		LastStatus:     sessionmemory.DreamStatusFailed,
		LastFinishedAt: now.Add(-30 * time.Minute),
	}); err != nil {
		t.Fatalf("SaveDreamState recent failure: %v", err)
	}
	if scheduler.shouldStart(history, agent.LoopResult{Content: "done"}, now) {
		t.Fatal("recent failed dream should back off")
	}

	if err := sessionmemory.SaveDreamState(workspaceState, sessionmemory.DreamState{
		LastStatus:     sessionmemory.DreamStatusFailed,
		LastFinishedAt: now.Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("SaveDreamState old failure: %v", err)
	}
	if !scheduler.shouldStart(history, agent.LoopResult{Content: "done"}, now) {
		t.Fatal("old failed dream should be eligible again")
	}
	scheduler.finish()
}

// TestSessionDreamScheduler_ShouldStartReconcilesStaleRunningState is the
// crash story for repair item #9: a dream that died with its process leaves
// LastStatus=running. Once that state is older than 2× the dream timeout it
// must be reconciled to failed and retried immediately — not blocked by the
// interval gate of an earlier completed dream, and not left lying forever.
func TestSessionDreamScheduler_ShouldStartReconcilesStaleRunningState(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string { return sessionArtifact }, 7, nil, "")
	history := makeSessionDreamHistory(1)
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	// Crashed mid-dream: started well past the stale window, never finished.
	// LastRunAt is recent enough that the interval gate alone would block —
	// the stale-running reconciliation must win.
	if err := sessionmemory.SaveDreamState(workspaceState, sessionmemory.DreamState{
		LastRunAt:     now.Add(-24 * time.Hour),
		LastStartedAt: now.Add(-time.Hour),
		LastStatus:    sessionmemory.DreamStatusRunning,
	}); err != nil {
		t.Fatalf("SaveDreamState stale running: %v", err)
	}
	if !scheduler.shouldStart(history, agent.LoopResult{Content: "done"}, now) {
		t.Fatal("stale running dream state must be reconciled and retried immediately")
	}
	scheduler.finish()

	state, err := sessionmemory.LoadDreamState(workspaceState)
	if err != nil {
		t.Fatalf("LoadDreamState: %v", err)
	}
	if state.LastStatus != sessionmemory.DreamStatusFailed {
		t.Fatalf("stale running state should settle as failed, got %q", state.LastStatus)
	}
	if !strings.Contains(state.LastError, "interrupted") {
		t.Fatalf("reconciled state should carry the interruption reason, got %q", state.LastError)
	}
	if !state.LastFinishedAt.Equal(now) {
		t.Fatalf("LastFinishedAt = %v, want %v", state.LastFinishedAt, now)
	}
}

// TestSessionDreamScheduler_ShouldStartLeavesFreshRunningState asserts a
// running state inside the stale window (a dream may genuinely be live in
// another process) is left alone: the cross-process lock is the arbiter.
func TestSessionDreamScheduler_ShouldStartLeavesFreshRunningState(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string { return sessionArtifact }, 7, nil, "")
	history := makeSessionDreamHistory(1)
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	if err := sessionmemory.SaveDreamState(workspaceState, sessionmemory.DreamState{
		LastStartedAt: now.Add(-time.Minute),
		LastStatus:    sessionmemory.DreamStatusRunning,
	}); err != nil {
		t.Fatalf("SaveDreamState fresh running: %v", err)
	}
	if scheduler.shouldStart(history, agent.LoopResult{Content: "done"}, now) {
		t.Fatal("a possibly-live running dream must not be reaped inside the stale window")
	}
	state, err := sessionmemory.LoadDreamState(workspaceState)
	if err != nil {
		t.Fatalf("LoadDreamState: %v", err)
	}
	if state.LastStatus != sessionmemory.DreamStatusRunning {
		t.Fatalf("fresh running state must stay untouched, got %q", state.LastStatus)
	}
}

func TestSessionDreamScheduler_LiveOwnerPreventsStaleStateRepair(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string {
		return filepath.Join(workspaceState, "sessions", "session-1")
	}, 7, nil, "")
	now := time.Now().UTC()
	want := sessionmemory.DreamState{
		LastRunAt:     now.Add(-24 * time.Hour),
		LastStartedAt: now.Add(-time.Hour),
		LastStatus:    sessionmemory.DreamStatusRunning,
	}
	if err := sessionmemory.SaveDreamState(workspaceState, want); err != nil {
		t.Fatalf("SaveDreamState: %v", err)
	}
	owner, acquired, err := sessionmemory.TryAcquireDreamLock(workspaceState)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireDreamLock: acquired=%t err=%v", acquired, err)
	}
	defer owner.Release()

	scheduler.AfterTurn(
		context.Background(),
		&agent.StreamRunner{},
		makeSessionDreamHistory(1),
		agent.LoopResult{Content: "done"},
	)
	got, err := sessionmemory.LoadDreamState(workspaceState)
	if err != nil {
		t.Fatalf("LoadDreamState: %v", err)
	}
	if got.LastStatus != want.LastStatus || !got.LastStartedAt.Equal(want.LastStartedAt) || got.LastError != "" {
		t.Fatalf("live owner's state was rewritten: got=%+v want=%+v", got, want)
	}
}

func TestSessionDream_RunWritesProjectMemoryWithAlignedToolSet(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	client := &sessionDreamFakeClient{
		responses: []providers.ChatResponse{
			{
				ToolCalls: []providers.ToolCall{{
					ID:   "call_1",
					Name: "session_memory",
					Arguments: `{
						"action":"replace",
						"target":"project_memory",
						"content":"# Project Memory\n\n## Discovered Durable Knowledge\n\n- The release workflow requires visual QA before tagging."
					}`,
				}},
			},
			{Content: "Saved."},
		},
	}
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string { return sessionArtifact }, 7, nil, "")
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	err := scheduler.run(context.Background(), sessionDreamJob{
		client:             client,
		model:              "test-model",
		systemPrompt:       "Use apply_patch for file edits. Use bash for terminal work.",
		rootDir:            root,
		workspaceStateDir:  workspaceState,
		sessionArtifactDir: sessionArtifact,
		now:                now,
		sessions:           []sessionDreamCandidate{{ID: "session-1"}},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	_, content, exists, err := sessionmemory.ReadTarget(workspaceState, sessionArtifact, sessionmemory.TargetProjectMemory)
	if err != nil {
		t.Fatalf("ReadTarget: %v", err)
	}
	if !exists || !strings.Contains(content, "visual QA before tagging") {
		t.Fatalf("project memory not written: exists=%t content=%q", exists, content)
	}
	eventPath := filepath.Join(workspaceState, "memory", "events.jsonl")
	eventData, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatalf("read background memory events: %v", err)
	}
	eventText := string(eventData)
	for _, want := range []string{
		`"source":"session_dream"`,
		`"tool":"session_memory"`,
		`"target":"project_memory"`,
		`"path":"` + filepath.Join(workspaceState, "memory", "MEMORY.md") + `"`,
		`"written":true`,
	} {
		if !strings.Contains(eventText, want) {
			t.Fatalf("background memory event missing %q:\n%s", want, eventText)
		}
	}
	state, err := sessionmemory.LoadDreamState(workspaceState)
	if err != nil {
		t.Fatalf("LoadDreamState: %v", err)
	}
	if !state.LastRunAt.Equal(now) {
		t.Fatalf("dream state LastRunAt = %v, want %v", state.LastRunAt, now)
	}
	if state.LastStatus != sessionmemory.DreamStatusCompleted || !state.LastFinishedAt.Equal(now) || state.LastError != "" {
		t.Fatalf("dream completion state = %+v", state)
	}
	if len(client.requests) != 2 {
		t.Fatalf("chat calls = %d, want 2", len(client.requests))
	}
	for index, req := range client.requests {
		if req.Operation.Kind != providers.InferenceOperationMemory || req.Operation.WorkloadProfile != providers.InferenceProfileBestEffort {
			t.Fatalf("dream request %d operation = %+v, want memory/best_effort", index+1, req.Operation)
		}
	}
	toolNames := make(map[string]bool)
	for _, def := range client.requests[0].Tools {
		toolNames[def.Name] = true
	}
	wantTools := []string{"read_file", "list_files", "grep", "glob", "thread_get", "session_memory"}
	if len(toolNames) != len(wantTools) {
		t.Fatalf("dream tools = %+v, want %v", toolNames, wantTools)
	}
	for _, name := range wantTools {
		if !toolNames[name] {
			t.Fatalf("dream tools = %+v, missing %s", toolNames, name)
		}
	}
	for _, blocked := range []string{"apply_patch", "edit_file", "write_file", "bash"} {
		if toolNames[blocked] {
			t.Fatalf("dream tools must not expose profile-specific or command tools, got %+v", toolNames)
		}
	}
	firstSystem := client.requests[0].Messages[0]
	if firstSystem.Role != "system" || !strings.Contains(firstSystem.Content, "background memory consolidation worker") {
		t.Fatalf("dream must use a profile-neutral memory system prompt, got %+v", firstSystem)
	}
	for _, blocked := range []string{"apply_patch", "edit_file", "write_file", "bash", "terminal", "shell", "git"} {
		if strings.Contains(firstSystem.Content, blocked) {
			t.Fatalf("dream system prompt must not inherit profile-specific tool names %q:\n%s", blocked, firstSystem.Content)
		}
	}
	var last providers.ChatMessage
	for i := len(client.requests[0].Messages) - 1; i >= 0; i-- {
		if client.requests[0].Messages[i].Hidden {
			continue
		}
		last = client.requests[0].Messages[i]
		break
	}
	if last.Role != "user" || !strings.Contains(last.Content, "read_file") || !strings.Contains(last.Content, "Nothing to dream") {
		t.Fatalf("missing dream prompt in request: %+v", last)
	}
	for _, old := range []string{
		"Available tools are read_file, list_files, glob, grep, run_shell",
		"Use run_shell only",
		"write_file",
		"edit_file",
		"apply_patch",
		"bash",
		"terminal",
		"shell",
		"git",
		"package manager",
		"long-running",
	} {
		if strings.Contains(last.Content, old) {
			t.Fatalf("dream prompt must not teach profile-specific or legacy tool path %q:\n%s", old, last.Content)
		}
	}
}

func TestSessionDream_RunRecordsFailureState(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	client := &sessionDreamFakeClient{
		errors: []error{errors.New("provider unavailable")},
	}
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string { return sessionArtifact }, 7, nil, "")
	started := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	err := scheduler.run(context.Background(), sessionDreamJob{
		client:             client,
		model:              "test-model",
		rootDir:            root,
		workspaceStateDir:  workspaceState,
		sessionArtifactDir: sessionArtifact,
		now:                started,
		sessions:           []sessionDreamCandidate{{ID: "session-1"}},
	})
	if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("run error = %v, want provider unavailable", err)
	}

	state, loadErr := sessionmemory.LoadDreamState(workspaceState)
	if loadErr != nil {
		t.Fatalf("LoadDreamState: %v", loadErr)
	}
	if state.LastStatus != sessionmemory.DreamStatusFailed || !strings.Contains(state.LastError, "provider unavailable") {
		t.Fatalf("dream failure state = %+v", state)
	}
	if !state.LastRunAt.IsZero() {
		t.Fatalf("failed dream should not update LastRunAt: %+v", state)
	}
	if !state.LastStartedAt.Equal(started) {
		t.Fatalf("LastStartedAt = %v, want %v", state.LastStartedAt, started)
	}
}

func TestSessionDream_RunReturnsFailureStatePersistenceError(t *testing.T) {
	root := t.TempDir()
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	providerErr := errors.New("provider unavailable")
	statePath := sessionmemory.DreamStatePath(workspaceState)
	client := &sessionDreamFakeClient{
		errors: []error{providerErr},
		beforeChat: func() {
			if err := os.Remove(statePath); err != nil {
				t.Fatalf("remove dream state: %v", err)
			}
			if err := os.Mkdir(statePath, 0o755); err != nil {
				t.Fatalf("replace dream state with directory: %v", err)
			}
		},
	}
	scheduler := newSessionDreamScheduler(root, workspaceState, func() string { return sessionArtifact }, 7, nil, "")

	err := scheduler.run(context.Background(), sessionDreamJob{
		client:             client,
		model:              "test-model",
		rootDir:            root,
		workspaceStateDir:  workspaceState,
		sessionArtifactDir: sessionArtifact,
		sessions:           []sessionDreamCandidate{{ID: "session-1"}},
	})

	if !errors.Is(err, providerErr) {
		t.Fatalf("run error = %v, want original provider error", err)
	}
	if !strings.Contains(err.Error(), "record dream failure") || !strings.Contains(err.Error(), "read dream state") {
		t.Fatalf("run error = %v, want failure-state persistence error", err)
	}
}
