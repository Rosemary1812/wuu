package host

import (
	"encoding/json"
	"testing"

	"github.com/blueberrycongee/wuu/internal/appserver"
)

func TestFilterMobileChatLineDropsHiddenStreamNotifications(t *testing.T) {
	line := []byte(`{"method":"item/agentMessage/delta","params":{"thread_id":"t","turn_id":"turn","item_id":"i","delta":"hidden"}}`)
	if got, keep := filterMobileChatLine(line); keep || got != nil {
		t.Fatalf("hidden delta should be dropped, got keep=%v line=%s", keep, got)
	}
}

func TestFilterMobileChatLineKeepsServerRequests(t *testing.T) {
	line := []byte(`{"id":"server-1","method":"tool/approval/request","params":{"tool":"bash"}}`)
	got, keep := filterMobileChatLine(line)
	if !keep || string(got) != string(line) {
		t.Fatalf("server request should pass through unchanged, keep=%v got=%s", keep, got)
	}
}

func TestFilterMobileChatLineSlimsThreadListResponse(t *testing.T) {
	line := []byte(`{
		"id":"list-1",
		"result":{
			"threads":[
				{
					"id":"collaboration",
					"source":"collaboration",
					"turns":[{
						"id":"turn-1",
						"items":[
							{"id":"u","type":"user_message","text":"hi"},
							{"id":"a","type":"agent_message","text":"working"},
							{"id":"tool","type":"tool_call","name":"bash"}
						],
						"items_view":"full",
						"status":"completed"
					}],
					"child_agents":[{"id":"agent-1"}],
					"browser_state":{"current_url":"https://example.com"}
				},
				{"id":"project","workspace_kind":"project","turns":[{"id":"p","items":[]}]}
			]
		}
	}`)
	got, keep := filterMobileChatLine(line)
	if !keep {
		t.Fatal("thread list response should be kept")
	}
	var env struct {
		Result struct {
			Threads []struct {
				ID          string           `json:"id"`
				ChildAgents json.RawMessage  `json:"child_agents"`
				Browser     json.RawMessage  `json:"browser_state"`
				Turns       []appserver.Turn `json:"turns"`
			} `json:"threads"`
		} `json:"result"`
	}
	if err := json.Unmarshal(got, &env); err != nil {
		t.Fatalf("decode filtered line: %v\n%s", err, got)
	}
	if len(env.Result.Threads) != 1 {
		t.Fatalf("expected only collaboration threads, got %+v", env.Result.Threads)
	}
	thread := env.Result.Threads[0]
	if thread.ID != "collaboration" {
		t.Fatalf("first thread = %q, want collaboration", thread.ID)
	}
	if len(thread.ChildAgents) != 0 || len(thread.Browser) != 0 {
		t.Fatalf("desktop-only thread fields were not removed: child=%s browser=%s", thread.ChildAgents, thread.Browser)
	}
	items := thread.Turns[0].Items
	if gotTypes := itemTypes(items); gotTypes != "user_message,agent_message" {
		t.Fatalf("unexpected visible item types: %s", gotTypes)
	}
	if len(items) != 2 {
		t.Fatalf("expected collaboration intake messages only: %+v", items)
	}
}

func TestFilterMobileChatLineKeepsOnlyVisibleItemNotifications(t *testing.T) {
	hidden := []byte(`{"method":"item/completed","params":{"thread_id":"t","turn_id":"turn","item":{"id":"tool","type":"tool_call","name":"bash"}}}`)
	if got, keep := filterMobileChatLine(hidden); keep || got != nil {
		t.Fatalf("tool item notification should be dropped, keep=%v got=%s", keep, got)
	}
	visible := []byte(`{"method":"item/completed","params":{"thread_id":"t","turn_id":"turn","item":{"id":"msg","type":"agent_message","text":"done"}}}`)
	got, keep := filterMobileChatLine(visible)
	if !keep {
		t.Fatal("agent message notification should be kept")
	}
	var env struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(got, &env); err != nil || env.Method != appserver.NotificationItemCompleted {
		t.Fatalf("visible notification corrupted: method=%q err=%v line=%s", env.Method, err, got)
	}
}

func itemTypes(items []appserver.ThreadItem) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += ","
		}
		out += string(item.Type)
	}
	return out
}
