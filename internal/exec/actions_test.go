package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/appserver"
	"github.com/blueberrycongee/wuu/internal/providers"
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
				Params: map[string]any{"thread_id": "$group", "anchor_item_id": "seq-3", "thread_owner_participant_id": "ada"},
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
				Params: map[string]any{"thread_id": "$group", "subthread_id": "$cth"},
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
	if openParams["thread_owner_participant_id"] != "ada" {
		t.Fatalf("open_reply owner lost: %+v", openParams)
	}
	postParams := decodeCallParams(t, controller.calls[2].params)
	if postParams["thread_id"] != "grp-1" || postParams["subthread_id"] != "cth-1" {
		t.Fatalf("post_subthread ids not threaded: %+v", postParams)
	}
	escParams := decodeCallParams(t, controller.calls[3].params)
	if escParams["subthread_id"] != "cth-1" {
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

func TestRunActionSequenceCreatesDMThroughThreadStart(t *testing.T) {
	controller := newFakeController()
	controller.callResults = map[string]json.RawMessage{
		"thread/start": json.RawMessage(`{"thread":{"id":"dm-1","dm_participant_id":"prt-ada"}}`),
	}

	err := Run(context.Background(), Options{
		Controller: controller,
		Actions: []GroupAction{{
			Action: "create_dm",
			Params: map[string]any{"dm_participant_id": "prt-ada"},
			Expect: map[string]any{"thread.dm_participant_id": "prt-ada"},
		}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(controller.calls) != 1 || controller.calls[0].method != "thread/start" {
		t.Fatalf("calls = %+v, want one thread/start", controller.calls)
	}
	params := decodeCallParams(t, controller.calls[0].params)
	if params["dm_participant_id"] != "prt-ada" {
		t.Fatalf("create_dm participant lost: %+v", params)
	}
	if _, ok := params["group"]; ok {
		t.Fatalf("create_dm must not inject group:true: %+v", params)
	}
}

func TestRunActionSequenceSendsDesktopEquivalentUserTurn(t *testing.T) {
	controller := newFakeController(notification(appserver.NotificationTurnCompleted, appserver.TurnCompletedNotification{
		ThreadID: "dm-1",
		Turn:     appserver.Turn{ID: "turn-1", Status: appserver.TurnStatusCompleted},
		Content:  "DM reply",
	}))

	err := Run(context.Background(), Options{
		Controller: controller,
		Actions: []GroupAction{{
			Action: "send_user_message",
			Params: map[string]any{"thread_id": "dm-1", "prompt": "please investigate"},
			Expect: map[string]any{"status": "completed", "text": "DM reply"},
		}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if controller.startedTurnThread != "dm-1" || controller.startedPrompt != "please investigate" {
		t.Fatalf("turn/start = thread %q prompt %q", controller.startedTurnThread, controller.startedPrompt)
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
