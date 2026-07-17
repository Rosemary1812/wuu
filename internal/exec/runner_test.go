package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/appserver"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type fakeController struct {
	initResult appserver.InitializeResult
	thread     appserver.Thread
	turn       appserver.Turn
	events     []Notification
	batches    [][]Notification

	startedThread     bool
	startEphemeral    bool
	resumedThread     string
	forkedThread      string
	startedPrompt     string
	startedTurnThread string
	startedPrompts    []string
	startedImages     []appserver.TurnStartImage
	startedFiles      []appserver.TurnStartFile
	interrupted       string
	shutdown          bool
	startCount        int

	participantStart  appserver.ParticipantStartParams
	participantAgent  appserver.Agent
	participantCalled bool

	calls       []recordedCall
	callResults map[string]json.RawMessage
}

type recordedCall struct {
	method string
	params json.RawMessage
}

func newFakeController(events ...Notification) *fakeController {
	return newFakeControllerBatches(events)
}

func newFakeControllerBatches(batches ...[]Notification) *fakeController {
	return &fakeController{
		initResult: appserver.InitializeResult{
			ProtocolVersion: appserver.ProtocolVersion,
			Provider:        "test-provider",
			Model:           "test-model",
			WorkspaceRoot:   "/repo",
			Permissions: appserver.PermissionSummary{
				Mode: "standard",
			},
		},
		thread:  appserver.Thread{ID: "thread-1", ModelProvider: "test-provider", Model: "test-model", CWD: "/repo"},
		turn:    appserver.Turn{ID: "turn-1"},
		batches: batches,
	}
}

func (f *fakeController) Initialize(context.Context) (appserver.InitializeResult, error) {
	return f.initResult, nil
}

func (f *fakeController) StartThread(_ context.Context, ephemeral bool) (appserver.Thread, error) {
	f.startedThread = true
	f.startEphemeral = ephemeral
	f.thread.Ephemeral = ephemeral
	return f.thread, nil
}

func (f *fakeController) ResumeThread(_ context.Context, id string) (appserver.Thread, error) {
	f.resumedThread = id
	return f.thread, nil
}

func (f *fakeController) ForkThread(_ context.Context, id string) (appserver.Thread, error) {
	f.forkedThread = id
	f.thread.ID = "fork-thread-1"
	f.thread.ForkedFromID = id
	return f.thread, nil
}

func (f *fakeController) StartTurn(_ context.Context, threadID string, input TurnInput) (appserver.Turn, error) {
	f.startCount++
	f.startedTurnThread = threadID
	f.startedPrompt = input.Prompt
	f.startedPrompts = append(f.startedPrompts, input.Prompt)
	f.startedImages = append([]appserver.TurnStartImage(nil), input.Images...)
	f.startedFiles = append([]appserver.TurnStartFile(nil), input.Files...)
	turn := f.turn
	turn.ID = fmt.Sprintf("turn-%d", f.startCount)
	return turn, nil
}

func (f *fakeController) StartParticipantTurn(_ context.Context, params appserver.ParticipantStartParams) (appserver.Agent, error) {
	f.startCount++
	f.participantCalled = true
	f.participantStart = params
	agent := f.participantAgent
	if strings.TrimSpace(agent.ID) == "" {
		agent.ID = "named-agent-1"
	}
	if strings.TrimSpace(agent.Status) == "" {
		agent.Status = "running"
	}
	return agent, nil
}

func (f *fakeController) Call(_ context.Context, method string, params any, result any) error {
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = append(json.RawMessage(nil), data...)
	}
	f.calls = append(f.calls, recordedCall{method: method, params: raw})
	if result == nil {
		return nil
	}
	data := f.callResults[method]
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	return json.Unmarshal(data, result)
}

func (f *fakeController) Interrupt(_ context.Context, threadID string) error {
	f.interrupted = threadID
	return nil
}

func (f *fakeController) Shutdown(context.Context) error {
	f.shutdown = true
	return nil
}

func (f *fakeController) Notifications() <-chan Notification {
	idx := f.startCount - 1
	var events []Notification
	if idx >= 0 && idx < len(f.batches) {
		events = f.batches[idx]
	} else {
		events = f.events
	}
	ch := make(chan Notification, len(events))
	for _, ev := range events {
		ch <- ev
	}
	return ch
}

func TestRunDefaultStdoutOnlyFinalMessage(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationAgentMessageDelta, appserver.AgentMessageDeltaNotification{ThreadID: "thread-1", TurnID: "turn-1", Delta: "partial"}),
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "final answer", TracePath: "/trace.jsonl"}),
	)
	var stdout, stderr bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:     "do work",
		Stdout:     &stdout,
		Stderr:     &stderr,
		Controller: controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := stdout.String(); got != "final answer\n" {
		t.Fatalf("stdout = %q", got)
	}
	if strings.Contains(stdout.String(), "provider") || strings.Contains(stdout.String(), "trace_path") {
		t.Fatalf("stdout should contain only final answer, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "provider: test-provider") || !strings.Contains(stderr.String(), "trace_path: /trace.jsonl") {
		t.Fatalf("stderr missing run metadata: %q", stderr.String())
	}
	if !controller.startedThread || controller.startedPrompt != "do work" || !controller.shutdown {
		t.Fatalf("controller did not run expected app-server path: %+v", controller)
	}
}

func TestRunWaitsForAutomaticContinuation(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{
			ThreadID:                 "thread-1",
			Turn:                     appserver.Turn{ID: "turn-1"},
			Content:                  "waiting for the background process",
			AwaitingAutoContinuation: true,
		}),
		notification(appserver.NotificationTurnStarted, appserver.TurnStartedNotification{
			ThreadID: "thread-1",
			Turn:     appserver.Turn{ID: "turn-2"},
		}),
		notification(appserver.NotificationAgentMessageDelta, appserver.AgentMessageDeltaNotification{
			ThreadID: "thread-1",
			TurnID:   "turn-2",
			Delta:    "continued",
		}),
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{
			ThreadID:  "thread-1",
			Turn:      appserver.Turn{ID: "turn-2"},
			Content:   "background work verified",
			TracePath: "/trace-2.jsonl",
		}),
	)
	var stdout, stderr bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:     "run background work",
		Stdout:     &stdout,
		Stderr:     &stderr,
		Controller: controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := stdout.String(); got != "background work verified\n" {
		t.Fatalf("stdout = %q", got)
	}
	if strings.Contains(stdout.String(), "waiting for the background process") {
		t.Fatalf("stdout exposed intermediate completion: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "trace_path: /trace-2.jsonl") {
		t.Fatalf("stderr missing continuation trace path: %q", stderr.String())
	}
}

func TestRunJSONLEmitsStableEvents(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnEvent, appserver.TurnEventNotification{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Event: appserver.StreamEventPayload{
				Type: providers.EventRequestContext,
				RequestContext: &providers.RequestContextSummary{
					StepIndex:                1,
					TransientMessages:        2,
					ContentBytes:             128,
					BlockKinds:               []string{"ENVIRONMENT", "TOOL_POLICY"},
					BlockKindCounts:          map[string]int{"ENVIRONMENT": 1, "TOOL_POLICY": 1},
					BlockKindBytes:           map[string]int{"ENVIRONMENT": 80, "TOOL_POLICY": 48},
					SegmentLifecycleCounts:   map[string]int{"request_only": 1},
					SegmentPlacementCounts:   map[string]int{"after_history": 1},
					SegmentCachePolicyCounts: map[string]int{"volatile": 1},
					MessageCount:             4,
					SystemMessages:           1,
					HiddenMessages:           2,
					ToolCount:                9,
					StablePrefix:             0,
					TurnPrefix:               1,
					DynamicBytes:             128,
					SystemBytes:              2048,
					StablePrefixBytes:        2048,
					TurnPrefixBytes:          2176,
					MessageBytes:             2304,
					ToolSchemaBytes:          4096,
					LoadableToolCount:        1,
					LoadableToolSchemaBytes:  512,
					LoadableToolSurfaceHash:  "loadable-hash",
					SystemHash:               "system-hash",
					StablePrefixHash:         "stable-hash",
					TurnPrefixHash:           "turn-hash",
					ToolSurfaceHash:          "tools-hash",
					PromptCacheKey:           "thread-1",
					SystemSections: []providers.SystemPromptSectionSummary{{
						Key:    "base",
						Static: true,
						Bytes:  512,
						Hash:   "base-hash",
					}},
				},
			},
		}),
		notification(appserver.NotificationTurnEvent, appserver.TurnEventNotification{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Event: appserver.StreamEventPayload{
				Type: providers.EventProviderState,
				ProviderState: &providers.ProviderStateSummary{
					StepIndex:              1,
					Provider:               "openai",
					Protocol:               "responses_websocket",
					Transport:              "websocket",
					ReplayMode:             "previous_response_id",
					PreviousResponseIDUsed: true,
					ConnectionReused:       true,
					FallbackActive:         true,
					FallbackReason:         "websocket_failed_before_first_event",
					FallbackPinStatus:      "created",
					FallbackRetryAfterMS:   29950,
					FallbackTTLMS:          30000,
					InputItems:             1,
					FullInputItems:         3,
					DeltaInputItems:        1,
				},
			},
		}),
		notification(appserver.NotificationAgentMessageDelta, appserver.AgentMessageDeltaNotification{ThreadID: "thread-1", TurnID: "turn-1", Delta: "hello"}),
		notification(appserver.NotificationTurnUsage, appserver.TurnUsageNotification{ThreadID: "thread-1", TurnID: "turn-1", InputTokens: 3, OutputTokens: 4}),
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "hello", TracePath: "/trace.jsonl"}),
	)
	controller.initResult.Ultra = true
	controller.initResult.MaxParallel = 7
	var stdout, stderr bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:     "do work",
		JSON:       true,
		Stdout:     &stdout,
		Stderr:     &stderr,
		Controller: controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("json mode should not write diagnostics to stderr in fake run, got %q", stderr.String())
	}
	events := parseJSONLines(t, stdout.String())
	gotTypes := make([]string, 0, len(events))
	for _, event := range events {
		gotTypes = append(gotTypes, event["type"].(string))
	}
	wantTypes := []string{"session_configured", "thread_started", "turn_started", "request_context", "provider_state", "agent_message_delta", "usage_updated", "turn_completed", "result"}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("event types:\n got: %#v\nwant: %#v\njsonl:\n%s", gotTypes, wantTypes, stdout.String())
	}
	sessionConfigured := events[0]
	if sessionConfigured["ultra"] != true || sessionConfigured["max_parallel"] != float64(7) {
		t.Fatalf("session_configured missing Ultra mode readback: %+v", sessionConfigured)
	}
	requestContext := events[3]
	if requestContext["tool_count"] != float64(9) ||
		requestContext["turn_prefix"] != float64(1) ||
		requestContext["dynamic_context_bytes"] != float64(128) ||
		requestContext["system_bytes"] != float64(2048) ||
		requestContext["stable_prefix_bytes"] != float64(2048) ||
		requestContext["turn_prefix_bytes"] != float64(2176) ||
		requestContext["message_bytes"] != float64(2304) ||
		requestContext["tool_schema_bytes"] != float64(4096) ||
		requestContext["loadable_tool_count"] != float64(1) ||
		requestContext["loadable_tool_schema_bytes"] != float64(512) ||
		requestContext["loadable_tool_surface_hash"] != "loadable-hash" ||
		requestContext["system_hash"] != "system-hash" ||
		requestContext["stable_prefix_hash"] != "stable-hash" ||
		requestContext["turn_prefix_hash"] != "turn-hash" ||
		requestContext["tool_surface_hash"] != "tools-hash" ||
		requestContext["prompt_cache_key"] != "thread-1" {
		t.Fatalf("unexpected request_context event: %+v", requestContext)
	}
	if !reflect.DeepEqual(requestContext["block_kind_counts"], map[string]any{"ENVIRONMENT": float64(1), "TOOL_POLICY": float64(1)}) ||
		!reflect.DeepEqual(requestContext["block_kind_bytes"], map[string]any{"ENVIRONMENT": float64(80), "TOOL_POLICY": float64(48)}) {
		t.Fatalf("unexpected request_context block metrics: %+v", requestContext)
	}
	if !reflect.DeepEqual(requestContext["segment_lifecycle_counts"], map[string]any{"request_only": float64(1)}) ||
		!reflect.DeepEqual(requestContext["segment_placement_counts"], map[string]any{"after_history": float64(1)}) ||
		!reflect.DeepEqual(requestContext["segment_cache_policy_counts"], map[string]any{"volatile": float64(1)}) {
		t.Fatalf("unexpected request_context segment policy metrics: %+v", requestContext)
	}
	sections, ok := requestContext["system_sections"].([]any)
	if !ok || len(sections) != 1 {
		t.Fatalf("request_context missing system_sections: %+v", requestContext)
	}
	section, ok := sections[0].(map[string]any)
	if !ok || section["key"] != "base" || section["static"] != true || section["bytes"] != float64(512) || section["hash"] != "base-hash" {
		t.Fatalf("unexpected system section: %+v", sections[0])
	}
	providerState := events[4]
	if providerState["step_index"] != float64(1) ||
		providerState["provider"] != "openai" ||
		providerState["protocol"] != "responses_websocket" ||
		providerState["transport"] != "websocket" ||
		providerState["replay_mode"] != "previous_response_id" ||
		providerState["previous_response_id_used"] != true ||
		providerState["connection_reused"] != true ||
		providerState["fallback_active"] != true ||
		providerState["fallback_reason"] != "websocket_failed_before_first_event" ||
		providerState["fallback_pin_status"] != "created" ||
		providerState["fallback_retry_after_ms"] != float64(29950) ||
		providerState["fallback_ttl_ms"] != float64(30000) ||
		providerState["input_items"] != float64(1) ||
		providerState["full_input_items"] != float64(3) ||
		providerState["delta_input_items"] != float64(1) {
		t.Fatalf("unexpected provider_state event: %+v", providerState)
	}
	result := events[len(events)-1]
	if result["status"] != "completed" || result["thread_id"] != "thread-1" || result["turn_id"] != "turn-1" || result["final_message"] != "hello" || result["trace_path"] != "/trace.jsonl" {
		t.Fatalf("unexpected result event: %+v", result)
	}
}

func TestRunEphemeralStartsEphemeralThread(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "done"}),
	)

	if err := Run(context.Background(), Options{
		Prompt:     "scratch task",
		Ephemeral:  true,
		Controller: controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !controller.startedThread || !controller.startEphemeral {
		t.Fatalf("expected ephemeral start, got %+v", controller)
	}
}

func TestRunResumeLastUsesResumePath(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "continued"}),
	)
	var stdout, stderr bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:     "continue",
		ResumeLast: true,
		Stdout:     &stdout,
		Stderr:     &stderr,
		Controller: controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if controller.startedThread {
		t.Fatal("resume should not start a new thread")
	}
	if controller.resumedThread != "" {
		t.Fatalf("resume last should pass empty thread id, got %q", controller.resumedThread)
	}
}

func TestRunForkUsesForkPath(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "fork-thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "forked"}),
	)
	var stdout bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:     "continue from fork",
		ForkID:     "source-thread",
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if controller.startedThread || controller.resumedThread != "" {
		t.Fatalf("fork should not start or resume: started=%v resumed=%q", controller.startedThread, controller.resumedThread)
	}
	if controller.forkedThread != "source-thread" {
		t.Fatalf("forkedThread = %q", controller.forkedThread)
	}
	events := parseJSONLines(t, stdout.String())
	if got := events[1]["type"]; got != "thread_forked" {
		t.Fatalf("second event = %v, want thread_forked\n%s", got, stdout.String())
	}
}

func TestRunPassesAttachmentsToTurnStart(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "done"}),
	)
	var stdout bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt: "inspect",
		Attachments: Attachments{
			Images: []appserver.TurnStartImage{{MediaType: "image/png", Data: "image-data"}},
			Files:  []appserver.TurnStartFile{{MediaType: "application/pdf", Data: "file-data", Filename: "report.pdf"}},
		},
		Stdout:     &stdout,
		Controller: controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(controller.startedImages) != 1 || controller.startedImages[0].MediaType != "image/png" || controller.startedImages[0].Data != "image-data" {
		t.Fatalf("images not passed to StartTurn: %+v", controller.startedImages)
	}
	if len(controller.startedFiles) != 1 || controller.startedFiles[0].MediaType != "application/pdf" || controller.startedFiles[0].Filename != "report.pdf" {
		t.Fatalf("files not passed to StartTurn: %+v", controller.startedFiles)
	}
}

func TestRunTurnErrorReturnsExitCodeOne(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnError, appserver.TurnErrorNotification{ThreadID: "thread-1", TurnID: "turn-1", Error: "model failed"}),
	)
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		Prompt:     "do work",
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
	})
	if ExitCode(err) != ExitTurnFailed {
		t.Fatalf("ExitCode = %d, err=%v", ExitCode(err), err)
	}
	events := parseJSONLines(t, stdout.String())
	result := events[len(events)-1]
	if result["status"] != "failed" || result["error"] != "model failed" {
		t.Fatalf("unexpected failure result: %+v", result)
	}
}

func TestRunTurnErrorWithInterruptedStatusEmitsTurnInterrupted(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnError, appserver.TurnErrorNotification{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Error:    "interrupted",
			Turn:     appserver.Turn{ID: "turn-1", Status: appserver.TurnStatusInterrupted},
		}),
	)
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		Prompt:     "do work",
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
	})
	if ExitCode(err) != ExitInterrupted {
		t.Fatalf("ExitCode = %d, err=%v", ExitCode(err), err)
	}
	events := parseJSONLines(t, stdout.String())
	var sawInterrupted bool
	for _, ev := range events {
		if ev["type"] == "turn_failed" {
			t.Fatalf("interrupted turn must not emit turn_failed: %+v", ev)
		}
		if ev["type"] == "turn_interrupted" {
			sawInterrupted = true
		}
	}
	if !sawInterrupted {
		t.Fatalf("expected a turn_interrupted event, got %+v", events)
	}
	result := events[len(events)-1]
	if result["status"] != "interrupted" {
		t.Fatalf("unexpected result status: %+v", result)
	}
}

func TestRunTurnErrorClassifiesProviderModelError(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnError, appserver.TurnErrorNotification{ThreadID: "thread-1", TurnID: "turn-1", Error: "provider returned an error"}),
	)
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		Prompt:     "do work",
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
	})
	if ExitCode(err) != ExitProviderModelError {
		t.Fatalf("ExitCode = %d, err=%v", ExitCode(err), err)
	}
}

func TestRunTurnErrorClassifiesToolFailure(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnError, appserver.TurnErrorNotification{ThreadID: "thread-1", TurnID: "turn-1", Error: "tool execution failed: run_shell failed"}),
	)
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		Prompt:     "do work",
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
	})
	if ExitCode(err) != ExitToolFailed {
		t.Fatalf("ExitCode = %d, err=%v", ExitCode(err), err)
	}
}

func TestRunTimeoutInterruptsAndReturnsExitCodeFour(t *testing.T) {
	controller := newFakeController()
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		Prompt:     "do work",
		JSON:       true,
		Timeout:    20 * time.Millisecond,
		Stdout:     &stdout,
		Controller: controller,
	})
	if ExitCode(err) != ExitTimeout {
		t.Fatalf("ExitCode = %d, err=%v", ExitCode(err), err)
	}
	if controller.interrupted != "thread-1" {
		t.Fatalf("interrupted thread = %q", controller.interrupted)
	}
	if !controller.shutdown {
		t.Fatal("controller was not shutdown")
	}
	events := parseJSONLines(t, stdout.String())
	types := eventTypes(events)
	for _, want := range []string{"turn_interrupted", "result"} {
		if !containsString(types, want) {
			t.Fatalf("missing %s in events %#v\n%s", want, types, stdout.String())
		}
	}
	result := events[len(events)-1]
	if result["status"] != "timeout" || result["error"] != "timeout" {
		t.Fatalf("unexpected timeout result: %+v", result)
	}
}

func TestClassifySetupErrorReturnsProviderModelExit(t *testing.T) {
	err := classifySetupError(fmt.Errorf("no API key found for provider %q", "test"))
	if ExitCode(err) != ExitProviderModelError {
		t.Fatalf("ExitCode = %d, err=%v", ExitCode(err), err)
	}
}

func TestRunJSONLEmitsWorkEventFamilies(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationItemStarted, appserver.ItemStartedNotification{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Item: appserver.ThreadItem{
				ID:        "item-bash",
				Type:      appserver.ThreadItemToolCall,
				Name:      "bash",
				Arguments: `{"command":"go test ./..."}`,
			},
		}),
		notification(appserver.NotificationToolCallOutput, appserver.ToolCallOutputNotification{ThreadID: "thread-1", TurnID: "turn-1", ItemID: "item-bash", Delta: "ok\n"}),
		notification(appserver.NotificationItemCompleted, appserver.ItemCompletedNotification{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Item: appserver.ThreadItem{
				ID:     "item-bash",
				Type:   appserver.ThreadItemToolCall,
				Name:   "bash",
				Status: appserver.ThreadItemStatusCompleted,
			},
		}),
		notification(appserver.NotificationItemCompleted, appserver.ItemCompletedNotification{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Item: appserver.ThreadItem{
				ID:     "item-write",
				Type:   appserver.ThreadItemToolCall,
				Name:   "write_file",
				Status: appserver.ThreadItemStatusCompleted,
				Result: `{"action":"create","path":"notes.txt","new_file_sha":"sha256:abc","workspace_revision":"fs:worktree:1"}`,
			},
		}),
		notification(appserver.NotificationAgentUpdated, appserver.AgentUpdatedNotification{
			ThreadID: "thread-1",
			Agent:    appserver.Agent{ID: "agent-1", Type: "subagent", TaskName: "worker", Status: "running"},
		}),
		notification(appserver.NotificationAgentUpdated, appserver.AgentUpdatedNotification{
			ThreadID: "thread-1",
			Agent:    appserver.Agent{ID: "agent-1", Type: "subagent", TaskName: "worker", Status: "completed", Result: "done"},
		}),
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "done"}),
	)
	var stdout bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:     "do work",
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := parseJSONLines(t, stdout.String())
	types := eventTypes(events)
	for _, want := range []string{"command_started", "command_output_delta", "command_completed", "file_changed", "subagent_started", "subagent_completed"} {
		if !containsString(types, want) {
			t.Fatalf("missing %s in events %#v\n%s", want, types, stdout.String())
		}
	}
	commandStarted := firstEventOfType(t, events, "command_started")
	if commandStarted["command"] != "go test ./..." {
		t.Fatalf("command_started missing command: %+v", commandStarted)
	}
	fileChanged := firstEventOfType(t, events, "file_changed")
	if fileChanged["path"] != "notes.txt" || fileChanged["action"] != "create" || fileChanged["new_file_sha"] != "sha256:abc" {
		t.Fatalf("unexpected file_changed: %+v", fileChanged)
	}
}

func TestRunIgnoresChildTurnCompletionUntilRootCompletes(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationAgentUpdated, appserver.AgentUpdatedNotification{
			ThreadID: "thread-1",
			Agent:    appserver.Agent{ID: "agent-1", Type: "general-purpose", TaskName: "worker", Status: "running"},
		}),
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{
			ThreadID: "agent-1",
			Turn:     appserver.Turn{ID: "agent-turn-1"},
			Content:  "child result",
		}),
		notification(appserver.NotificationAgentUpdated, appserver.AgentUpdatedNotification{
			ThreadID: "thread-1",
			Agent:    appserver.Agent{ID: "agent-1", Type: "general-purpose", TaskName: "worker", Status: "completed", Result: "child result"},
		}),
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{
			ThreadID:  "thread-1",
			Turn:      appserver.Turn{ID: "turn-1"},
			Content:   "parent result",
			TracePath: "/trace.jsonl",
		}),
	)
	var stdout bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:     "spawn worker",
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := parseJSONLines(t, stdout.String())
	types := eventTypes(events)
	if !containsString(types, "subagent_completed") {
		t.Fatalf("missing subagent_completed in %#v\n%s", types, stdout.String())
	}
	result := events[len(events)-1]
	if result["type"] != "result" || result["thread_id"] != "thread-1" || result["turn_id"] != "turn-1" || result["final_message"] != "parent result" {
		t.Fatalf("exec should finish on root turn, got %+v\n%s", result, stdout.String())
	}
}

func TestRunIgnoresChildTurnErrorUntilRootCompletes(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnError, appserver.TurnErrorNotification{
			ThreadID: "agent-1",
			TurnID:   "agent-turn-1",
			Error:    "child failed",
			Turn:     appserver.Turn{ID: "agent-turn-1"},
		}),
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{
			ThreadID:  "thread-1",
			Turn:      appserver.Turn{ID: "turn-1"},
			Content:   "parent handled child failure",
			TracePath: "/trace.jsonl",
		}),
	)
	var stdout bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:     "await worker",
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	events := parseJSONLines(t, stdout.String())
	types := eventTypes(events)
	if !containsString(types, "turn_failed") {
		t.Fatalf("missing child turn_failed in %#v\n%s", types, stdout.String())
	}
	result := events[len(events)-1]
	if result["type"] != "result" || result["status"] != "completed" || result["final_message"] != "parent handled child failure" {
		t.Fatalf("exec should let root turn decide after child error, got %+v\n%s", result, stdout.String())
	}
}

func TestRunOutputSchemaEmitsStructuredResult(t *testing.T) {
	schemaPath := writeExecSchema(t, `{
		"type": "object",
		"required": ["summary"],
		"properties": {
			"summary": {"type": "string"}
		},
		"additionalProperties": false
	}`)
	controller := newFakeController(
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: `{"summary":"done"}`, TracePath: "/trace.jsonl"}),
	)
	var stdout bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:           "summarize",
		OutputSchemaPath: schemaPath,
		JSON:             true,
		Stdout:           &stdout,
		Controller:       controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(controller.startedPrompt, "Return only JSON") || !strings.Contains(controller.startedPrompt, `"summary"`) {
		t.Fatalf("prompt missing schema instructions: %q", controller.startedPrompt)
	}
	events := parseJSONLines(t, stdout.String())
	result := events[len(events)-1]
	structured, ok := result["structured_result"].(map[string]any)
	if !ok || structured["summary"] != "done" {
		t.Fatalf("structured_result = %+v", result["structured_result"])
	}
}

func TestRunOutputSchemaRetriesInvalidFinalMessage(t *testing.T) {
	schemaPath := writeExecSchema(t, `{
		"type": "object",
		"required": ["summary"],
		"properties": {
			"summary": {"type": "string"}
		}
	}`)
	controller := newFakeControllerBatches(
		[]Notification{
			notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-1"}, Content: "not json"}),
		},
		[]Notification{
			notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{ThreadID: "thread-1", Turn: appserver.Turn{ID: "turn-2"}, Content: `{"summary":"fixed"}`}),
		},
	)
	var stdout bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:           "summarize",
		OutputSchemaPath: schemaPath,
		JSON:             true,
		Stdout:           &stdout,
		Controller:       controller,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if controller.startCount != 2 {
		t.Fatalf("startCount = %d, want 2", controller.startCount)
	}
	if len(controller.startedPrompts) != 2 || !strings.Contains(controller.startedPrompts[1], "previous final answer did not validate") {
		t.Fatalf("retry prompt missing validation context: %+v", controller.startedPrompts)
	}
	events := parseJSONLines(t, stdout.String())
	gotTypes := make([]string, 0, len(events))
	for _, event := range events {
		gotTypes = append(gotTypes, event["type"].(string))
	}
	wantTypes := []string{"session_configured", "thread_started", "turn_started", "turn_completed", "error", "turn_started", "turn_completed", "result"}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("event types:\n got: %#v\nwant: %#v\njsonl:\n%s", gotTypes, wantTypes, stdout.String())
	}
	result := events[len(events)-1]
	if result["status"] != "completed" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunParticipantTurnStartsNamedRunAndCompletesOnAgentTerminal(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationItemCompleted, appserver.ItemCompletedNotification{
			ThreadID: "named-agent-1",
			TurnID:   "named-turn-1",
			Item: appserver.ThreadItem{
				ID:       "msg-1",
				Type:     appserver.ThreadItemParticipantMsg,
				AgentID:  "named-agent-1",
				PostKind: "update",
				Text:     "hello group",
			},
		}),
		// A child turn/completed on the named agent's own thread must NOT end
		// the participant run — only the agent/updated terminal status does.
		notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{
			ThreadID: "named-agent-1",
			Turn:     appserver.Turn{ID: "named-turn-1"},
			Content:  "child content",
		}),
		notification(appserver.NotificationAgentUpdated, appserver.AgentUpdatedNotification{
			ThreadID: "group-1",
			Agent:    appserver.Agent{ID: "named-agent-1", Type: "resident", TaskName: "Ada", Status: "running"},
		}),
		notification(appserver.NotificationAgentUpdated, appserver.AgentUpdatedNotification{
			ThreadID: "group-1",
			Agent:    appserver.Agent{ID: "named-agent-1", Type: "resident", TaskName: "Ada", Status: "completed", Result: "posted to group"},
		}),
	)
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		Prompt:     "greet the group",
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
		Participant: ParticipantRun{
			ParticipantID: "ada",
			ThreadID:      "group-1",
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !controller.participantCalled {
		t.Fatal("StartParticipantTurn was not called")
	}
	if controller.participantStart.ParticipantID != "ada" || controller.participantStart.ThreadID != "group-1" || controller.participantStart.Prompt != "greet the group" {
		t.Fatalf("unexpected participant/start params: %+v", controller.participantStart)
	}
	if controller.startedThread {
		t.Fatal("an explicit thread id should not start a new thread")
	}
	events := parseJSONLines(t, stdout.String())
	types := eventTypes(events)
	for _, want := range []string{"participant_turn_started", "participant_message", "subagent_started", "subagent_completed", "result"} {
		if !containsString(types, want) {
			t.Fatalf("missing %s in events %#v\n%s", want, types, stdout.String())
		}
	}
	started := firstEventOfType(t, events, "participant_turn_started")
	if started["agent_id"] != "named-agent-1" || started["participant_id"] != "ada" {
		t.Fatalf("unexpected participant_turn_started: %+v", started)
	}
	msg := firstEventOfType(t, events, "participant_message")
	if msg["text"] != "hello group" || msg["post_kind"] != "update" {
		t.Fatalf("unexpected participant_message: %+v", msg)
	}
	result := events[len(events)-1]
	if result["status"] != "completed" || result["thread_id"] != "group-1" || result["final_message"] != "posted to group" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRunParticipantTurnStartsThreadWhenUnset(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationAgentUpdated, appserver.AgentUpdatedNotification{
			ThreadID: "thread-1",
			Agent:    appserver.Agent{ID: "named-agent-1", Status: "completed", Result: "done"},
		}),
	)
	var stdout bytes.Buffer

	if err := Run(context.Background(), Options{
		Prompt:     "introduce yourself",
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
		Participant: ParticipantRun{
			ParticipantID: "ada",
		},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !controller.startedThread {
		t.Fatal("participant run without a thread id should start a fresh thread")
	}
	if controller.participantStart.ThreadID != "thread-1" {
		t.Fatalf("participant/start should target the freshly started thread, got %q", controller.participantStart.ThreadID)
	}
	events := parseJSONLines(t, stdout.String())
	types := eventTypes(events)
	if !containsString(types, "thread_started") {
		t.Fatalf("missing thread_started in %#v", types)
	}
}

func TestRunParticipantTurnFailsOnAgentFailure(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationAgentUpdated, appserver.AgentUpdatedNotification{
			ThreadID: "group-1",
			Agent:    appserver.Agent{ID: "named-agent-1", Status: "failed", Error: "resident run failed"},
		}),
	)
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		Prompt:     "do work",
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
		Participant: ParticipantRun{
			ParticipantID: "ada",
			ThreadID:      "group-1",
		},
	})
	if ExitCode(err) != ExitTurnFailed {
		t.Fatalf("ExitCode = %d, err=%v", ExitCode(err), err)
	}
	events := parseJSONLines(t, stdout.String())
	result := events[len(events)-1]
	if result["status"] != "failed" || result["error"] != "resident run failed" {
		t.Fatalf("unexpected failure result: %+v", result)
	}
}

func notification(method string, params any) Notification {
	data, err := json.Marshal(params)
	if err != nil {
		panic(err)
	}
	return Notification{Method: method, Params: data}
}

func parseJSONLines(t *testing.T, text string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(text), "\n")
	var events []map[string]any
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid JSONL line %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

func eventTypes(events []map[string]any) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		if typ, _ := event["type"].(string); typ != "" {
			out = append(out, typ)
		}
	}
	return out
}

func firstEventOfType(t *testing.T, events []map[string]any, typ string) map[string]any {
	t.Helper()
	for _, event := range events {
		if event["type"] == typ {
			return event
		}
	}
	t.Fatalf("event %s not found in %+v", typ, events)
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func writeExecSchema(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	return path
}
