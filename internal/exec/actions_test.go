package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/hooks"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// The scripted action sequence must drive the existing group/reply/task RPCs in
// order, thread generated ids across steps via save_as/$vars, inject structural
// constants (group:true), and pass per-step expectations.
func TestRunActionSequenceDrivesRPCsWithVarThreading(t *testing.T) {
	controller := newFakeController()
	controller.callResults = map[string]json.RawMessage{
		"thread/start":          json.RawMessage(`{"thread":{"id":"grp-1"}}`),
		"thread/openSub":        json.RawMessage(`{"subthread":{"id":"cth-1","reply_count":0,"participants":["ada"]}}`),
		"message/postSubthread": json.RawMessage(`{"subthread":{"id":"cth-1","reply_count":1}}`),
		"thread/escalateSub":    json.RawMessage(`{"subthread":{"id":"cth-1","status":"task","lead_participant_id":"ada"}}`),
	}
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
		Actions: []GroupAction{
			{Action: "create_group", Params: map[string]any{"title": "Bug triage"}, SaveAs: map[string]string{"group": "thread.id"}},
			{
				Action: "open_reply",
				Params: map[string]any{"thread_id": "$group", "anchor_item_id": "seq-3", "participants": []any{"ada"}},
				SaveAs: map[string]string{"cth": "subthread.id"},
				Expect: map[string]any{"subthread.reply_count": float64(0)},
			},
			{
				Action: "post_subthread",
				Params: map[string]any{"thread_id": "$group", "subthread_id": "$cth", "text": "retry path?"},
				Expect: map[string]any{"subthread.reply_count": float64(1)},
			},
			{
				Action: "escalate_task",
				Params: map[string]any{"thread_id": "$group", "subthread_id": "$cth", "lead_participant_id": "ada"},
				Expect: map[string]any{"subthread.lead_participant_id": "ada", "subthread.status": "task"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	gotMethods := make([]string, 0, len(controller.calls))
	for _, call := range controller.calls {
		gotMethods = append(gotMethods, call.method)
	}
	wantMethods := []string{"thread/start", "thread/openSub", "message/postSubthread", "thread/escalateSub"}
	if strings.Join(gotMethods, ",") != strings.Join(wantMethods, ",") {
		t.Fatalf("methods = %v, want %v", gotMethods, wantMethods)
	}

	createParams := decodeCallParams(t, controller.calls[0].params)
	if createParams["group"] != true {
		t.Fatalf("create_group must inject group:true, got %+v", createParams)
	}
	if createParams["title"] != "Bug triage" {
		t.Fatalf("create_group title lost: %+v", createParams)
	}
	openParams := decodeCallParams(t, controller.calls[1].params)
	if openParams["thread_id"] != "grp-1" {
		t.Fatalf("open_reply thread_id not substituted from $group: %+v", openParams)
	}
	if parts, _ := openParams["participants"].([]any); len(parts) != 1 || parts[0] != "ada" {
		t.Fatalf("open_reply participants subset lost: %+v", openParams)
	}
	postParams := decodeCallParams(t, controller.calls[2].params)
	if postParams["thread_id"] != "grp-1" || postParams["subthread_id"] != "cth-1" {
		t.Fatalf("post_subthread ids not threaded: %+v", postParams)
	}
	escParams := decodeCallParams(t, controller.calls[3].params)
	if escParams["subthread_id"] != "cth-1" || escParams["lead_participant_id"] != "ada" {
		t.Fatalf("escalate_task params wrong: %+v", escParams)
	}

	events := parseJSONLines(t, stdout.String())
	types := eventTypes(events)
	for _, want := range []string{"action_started", "action_completed", "result"} {
		if !containsString(types, want) {
			t.Fatalf("missing %s in %v\n%s", want, types, stdout.String())
		}
	}
	if got := countType(types, "action_completed"); got != 4 {
		t.Fatalf("action_completed count = %d, want 4", got)
	}
	result := events[len(events)-1]
	if result["type"] != "result" || result["status"] != "completed" {
		t.Fatalf("unexpected final result: %+v", result)
	}
}

// A failed per-step expectation aborts the sequence with a tool-failed exit and
// emits an action_failed event.
func TestRunActionSequenceExpectMismatchFails(t *testing.T) {
	controller := newFakeController()
	controller.callResults = map[string]json.RawMessage{
		"thread/start": json.RawMessage(`{"thread":{"id":"grp-1"}}`),
	}
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
		Actions: []GroupAction{
			{Action: "create_group", Params: map[string]any{"title": "x"}, Expect: map[string]any{"thread.id": "grp-2"}},
		},
	})
	if ExitCode(err) != ExitTurnFailed {
		t.Fatalf("ExitCode = %d, err=%v", ExitCode(err), err)
	}
	types := eventTypes(parseJSONLines(t, stdout.String()))
	if !containsString(types, "action_failed") {
		t.Fatalf("missing action_failed in %v", types)
	}
}

// An unknown action is an input error before any RPC is issued.
func TestRunActionSequenceUnknownActionFails(t *testing.T) {
	controller := newFakeController()
	err := Run(context.Background(), Options{
		Controller: controller,
		Actions:    []GroupAction{{Action: "make_coffee"}},
	})
	if ExitCode(err) != ExitInvalidInput {
		t.Fatalf("ExitCode = %d, err=%v", ExitCode(err), err)
	}
	if len(controller.calls) != 0 {
		t.Fatalf("unknown action should not issue any RPC, got %v", controller.calls)
	}
}

// End-to-end against the REAL app-server handlers (over pipes, deterministic
// fake provider): a scripted sequence builds a group, adds a named member, opens
// a reply seeded with a member subset, folds a human post into that reply, and
// escalates it to a task with a named lead. Asserts the load-bearing behaviors —
// the post folds into the cth (does not touch the main group stream, weak
// isolation) and the lead is recorded — through the exec driving surface.
func TestRunActionSequenceEndToEndFoldsReplyAndRecordsLead(t *testing.T) {
	rt := newExecGroupTestRuntime(t)
	adaID := participant.NewID()
	if err := session.UpsertParticipant(rt.SessionDir, participant.Participant{
		ID:        adaID,
		Kind:      participant.KindNamed,
		Name:      "Ada",
		Role:      "reviewer",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed named participant: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	controller := newLocalControllerForRuntime(ctx, rt)

	var stdout bytes.Buffer
	err := Run(ctx, Options{
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
		Actions: []GroupAction{
			{Action: "create_group", Params: map[string]any{"title": "Bug triage"}, SaveAs: map[string]string{"group": "thread.id"}},
			{Action: "add_group_member", Params: map[string]any{"thread_id": "$group", "participant_id": adaID}},
			{
				Action: "open_reply",
				Params: map[string]any{"thread_id": "$group", "anchor_item_id": "seq-3", "created_by": "user", "participants": []any{adaID}},
				SaveAs: map[string]string{"cth": "subthread.id"},
			},
			{
				Action: "post_subthread",
				Params: map[string]any{"thread_id": "$group", "subthread_id": "$cth", "text": "what about the retry path?"},
				Expect: map[string]any{"subthread.reply_count": float64(1)},
			},
			{
				Action: "escalate_task",
				Params: map[string]any{"thread_id": "$group", "subthread_id": "$cth", "created_by": "user", "lead_participant_id": adaID, "title": "Ship the fix"},
				Expect: map[string]any{"subthread.lead_participant_id": adaID, "subthread.status": "task"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, stdout.String())
	}

	events := parseJSONLines(t, stdout.String())
	groupID := actionResultPath(t, events, "create_group", "thread", "id")
	cthID := actionResultPath(t, events, "open_reply", "subthread", "id")
	if !strings.HasPrefix(cthID, "cth-") {
		t.Fatalf("open_reply did not return a cth id: %q", cthID)
	}

	// Weak-isolation member subset: the reply was seeded with the named member so
	// only that subset is alerted into the reply.
	openEvent := actionCompletedEvent(t, events, "open_reply")
	sub := openEvent["result"].(map[string]any)["subthread"].(map[string]any)
	parts, _ := sub["participants"].([]any)
	if !containsAny(parts, adaID) {
		t.Fatalf("reply member subset missing %q: %+v", adaID, sub["participants"])
	}

	// The human post folded into the cth: it is persisted tagged with the cth
	// thread_id and did NOT append to the main group stream.
	history, err := session.LoadHistoryRecords(rt.SessionDir, groupID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	var foldedIntoCth bool
	for i := range history {
		if history[i].Content == "what about the retry path?" {
			if strings.TrimSpace(history[i].ThreadID) != cthID {
				t.Fatalf("reply post leaked to main stream: thread_id=%q, want cth %q", history[i].ThreadID, cthID)
			}
			foldedIntoCth = true
		}
	}
	if !foldedIntoCth {
		t.Fatalf("reply post not persisted tagged with cth %q: %+v", cthID, history)
	}
}

func newExecGroupTestRuntime(t *testing.T) *runtime.Session {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	kit, err := tools.New(root)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	return &runtime.Session{
		ProviderName:   "fake-provider",
		Model:          "fake-model",
		RootDir:        root,
		ConfigPath:     root + "/.wuu.json",
		SessionDir:     root + "/.wuu-state/sessions",
		HookDispatcher: hooks.NewDispatcher(nil),
		Toolkit:        kit,
		StreamRunner: &agent.StreamRunner{
			Client:       providers.AdaptStreamClient(execGroupFakeClient{}),
			Model:        "fake-model",
			SystemPrompt: "system prompt",
		},
	}
}

type execGroupFakeClient struct{}

func (execGroupFakeClient) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{}, nil
}

func decodeCallParams(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	if len(raw) == 0 {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode call params %s: %v", raw, err)
	}
	return out
}

func countType(types []string, want string) int {
	n := 0
	for _, typ := range types {
		if typ == want {
			n++
		}
	}
	return n
}

func actionCompletedEvent(t *testing.T, events []map[string]any, action string) map[string]any {
	t.Helper()
	for _, event := range events {
		if event["type"] == "action_completed" && event["action"] == action {
			return event
		}
	}
	t.Fatalf("action_completed for %q not found", action)
	return nil
}

func actionResultPath(t *testing.T, events []map[string]any, action string, path ...string) string {
	t.Helper()
	event := actionCompletedEvent(t, events, action)
	var current any = event["result"]
	for _, segment := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("action %q result path %v not an object at %q: %+v", action, path, segment, event["result"])
		}
		current = obj[segment]
	}
	str, _ := current.(string)
	return str
}

func containsAny(values []any, want string) bool {
	for _, value := range values {
		if s, _ := value.(string); s == want {
			return true
		}
	}
	return false
}
