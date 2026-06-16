package appserver

import (
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestThreadStateCompletesPreambleBeforeToolStart(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", "", now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)

	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:    providers.EventContentDelta,
		Content: "I will inspect the current prompt path.",
	}, now)
	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventToolUseStart,
		ToolCall: &providers.ToolCall{
			ID:   "call_1",
			Name: "read_file",
		},
	}, now)

	turn := th.ensureTurnLocked("turn", now)
	if len(turn.Items) != 3 {
		t.Fatalf("expected user, preamble, and tool items, got %+v", turn.Items)
	}
	if turn.Items[1].Type != ThreadItemAgentMessage || turn.Items[1].Status != ThreadItemStatusCompleted {
		t.Fatalf("preamble should be a completed assistant item before the tool row, got %+v", turn.Items[1])
	}
	if turn.Items[1].Phase != ThreadItemPhaseCommentary {
		t.Fatalf("preamble should be commentary, got %+v", turn.Items[1])
	}
	if turn.Items[2].Type != ThreadItemToolCall || turn.Items[2].Status != ThreadItemStatusInProgress {
		t.Fatalf("tool should follow the completed preamble, got %+v", turn.Items[2])
	}

	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventMessage,
		Message: &providers.ChatMessage{
			Role:    "assistant",
			Content: "I will inspect the current prompt path.",
			ToolCalls: []providers.ToolCall{{
				ID:   "call_1",
				Name: "read_file",
			}},
		},
	}, now)

	turn = th.ensureTurnLocked("turn", now)
	agentItems := 0
	for _, item := range turn.Items {
		if item.Type == ThreadItemAgentMessage {
			agentItems++
		}
	}
	if agentItems != 1 {
		t.Fatalf("final assistant message should not duplicate streamed preamble, got %+v", turn.Items)
	}
}

func TestThreadStateMarksUnresolvedTextAsPending(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", "", now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)

	out := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:    providers.EventContentDelta,
		Content: "I will inspect the current prompt path.",
	}, now)

	turn := th.ensureTurnLocked("turn", now)
	if len(turn.Items) != 2 {
		t.Fatalf("expected user and pending assistant items, got %+v", turn.Items)
	}
	pending := turn.Items[1]
	if pending.Type != ThreadItemAgentMessage || pending.Status != ThreadItemStatusInProgress {
		t.Fatalf("expected live assistant item, got %+v", pending)
	}
	if pending.Phase != ThreadItemPhasePending {
		t.Fatalf("unresolved assistant text should be pending, got %+v", pending)
	}
	started, ok := out[0].params.(ItemStartedNotification)
	if !ok || started.Item.Phase != ThreadItemPhasePending {
		t.Fatalf("started notification should carry pending phase, got %#v", out[0].params)
	}
}

func TestThreadStateCarriesToolCallDisplay(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", "", now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)

	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventToolUseStart,
		ToolCall: &providers.ToolCall{
			ID:      "call_1",
			Name:    "read_file",
			Display: &providers.ToolCallDisplay{Kind: "read", Text: "读取 文件"},
		},
	}, now)
	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventToolUseEnd,
		ToolCall: &providers.ToolCall{
			ID:        "call_1",
			Name:      "read_file",
			Arguments: `{"path":"internal/appserver/model.go"}`,
			Display:   &providers.ToolCallDisplay{Kind: "read", Text: "读取 model.go"},
		},
	}, now)

	turn := th.ensureTurnLocked("turn", now)
	if len(turn.Items) != 2 {
		t.Fatalf("expected user and tool items, got %+v", turn.Items)
	}
	toolItem := turn.Items[1]
	if toolItem.Display == nil || toolItem.Display.Text != "读取 model.go" || toolItem.Display.Kind != "read" {
		t.Fatalf("expected updated display metadata, got %+v", toolItem.Display)
	}
}

func TestTurnsFromHistoryKeepsSteeredUserMessageInCurrentTurn(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	turns := turnsFromHistory("thread", []providers.ChatMessage{
		{Role: "user", Content: "start"},
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{{
				ID:        "call_1",
				Name:      "read_file",
				Arguments: `{}`,
			}},
		},
		{Role: "tool", ToolCallID: "call_1", Name: "read_file", Content: "file"},
		{Role: "user", ClientID: "steer-1", Content: "steer now", Steered: true},
		{Role: "assistant", Content: "done"},
	}, now)

	if len(turns) != 1 {
		t.Fatalf("steered user message should not create a new turn, got %+v", turns)
	}
	var userItems []ThreadItem
	for _, item := range turns[0].Items {
		if item.Type == ThreadItemUserMessage {
			userItems = append(userItems, item)
		}
	}
	if len(userItems) != 2 {
		t.Fatalf("expected original and steered user items, got %+v", turns[0].Items)
	}
	if userItems[1].Text != "steer now" || userItems[1].SourceID != "steer-1" {
		t.Fatalf("unexpected steered user item: %+v", userItems[1])
	}
}

func TestThreadStateReplacesActiveAgentMessageText(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", "", now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)

	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:    providers.EventContentDelta,
		Content: "stale partial",
	}, now)
	out := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventContentReplace,
	}, now)
	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:    providers.EventContentDelta,
		Content: "fresh answer",
	}, now)

	turn := th.ensureTurnLocked("turn", now)
	if len(turn.Items) != 2 {
		t.Fatalf("expected user and active assistant item, got %+v", turn.Items)
	}
	if turn.Items[1].Text != "fresh answer" {
		t.Fatalf("expected stale text to be replaced before new deltas, got %q", turn.Items[1].Text)
	}
	if len(out) != 1 || out[0].method != NotificationAgentMessageReplace {
		t.Fatalf("expected replace notification, got %+v", out)
	}
	params, ok := out[0].params.(AgentMessageReplaceNotification)
	if !ok || params.Text != "" || params.ItemID != turn.Items[1].ID {
		t.Fatalf("unexpected replace params: %#v", out[0].params)
	}
}

func TestThreadStateMarksPostToolTextAsFinalPhaseOnFirstDelta(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", "", now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)

	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventToolUseStart,
		ToolCall: &providers.ToolCall{
			ID:   "call_1",
			Name: "read_file",
		},
	}, now)
	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:       providers.EventToolUseEnd,
		ToolCall:   &providers.ToolCall{ID: "call_1", Name: "read_file"},
		ToolResult: "file contents",
	}, now)
	out := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:    providers.EventContentDelta,
		Content: "The result is clear.",
	}, now)

	turn := th.ensureTurnLocked("turn", now)
	if len(turn.Items) != 3 {
		t.Fatalf("expected user, tool, and final assistant items, got %+v", turn.Items)
	}
	final := turn.Items[2]
	if final.Type != ThreadItemAgentMessage || final.Status != ThreadItemStatusInProgress {
		t.Fatalf("post-tool text should start a live assistant item, got %+v", final)
	}
	if final.Phase != ThreadItemPhaseFinalAnswer {
		t.Fatalf("post-tool text should be marked final as soon as it starts, got %+v", final)
	}
	if len(out) == 0 {
		t.Fatal("expected notifications for first final delta")
	}
	started, ok := out[0].params.(ItemStartedNotification)
	if !ok || started.Item.Phase != ThreadItemPhaseFinalAnswer {
		t.Fatalf("started notification should carry final phase, got %#v", out[0].params)
	}
}

func TestTurnsFromHistoryMarksAssistantMessagePhases(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	turns := turnsFromHistory("thread", []providers.ChatMessage{
		{Role: "user", Content: "inspect"},
		{
			Role:    "assistant",
			Content: "I will inspect first.",
			ToolCalls: []providers.ToolCall{{
				ID:   "call_1",
				Name: "read_file",
			}},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "file contents"},
		{Role: "assistant", Content: "The result is clear."},
	}, now)

	if len(turns) != 1 {
		t.Fatalf("expected one turn, got %+v", turns)
	}
	var phases []ThreadItemPhase
	for _, item := range turns[0].Items {
		if item.Type == ThreadItemAgentMessage {
			phases = append(phases, item.Phase)
		}
	}
	if len(phases) != 2 || phases[0] != ThreadItemPhaseCommentary || phases[1] != ThreadItemPhaseFinalAnswer {
		t.Fatalf("unexpected assistant phases: %+v", phases)
	}
}

func TestThreadStateCapturesListeningPortsFromReport(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", "", now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "start vite"}, now)

	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventToolUseStart,
		ToolCall: &providers.ToolCall{
			ID:   "call_1",
			Name: "report_listening_ports",
		},
	}, now)

	out := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventToolUseEnd,
		ToolCall: &providers.ToolCall{
			ID:        "call_1",
			Name:      "report_listening_ports",
			Arguments: `{"ports":[8080,3000,8080,70000]}`,
		},
		ToolResult: `{"status":"noted","ports":[8080,3000],"preview_urls":["http://localhost:8080","http://localhost:3000"],"process_id":"proc-dev"}`,
	}, now)

	if got, want := th.ListeningPorts, []int{8080, 3000}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected normalized ports [8080 3000], got %v", got)
	}
	if th.BrowserState.PrimaryPreviewURL != "http://localhost:8080" || th.BrowserState.CurrentURL != "http://localhost:8080" || th.BrowserState.LinkedProcessID != "proc-dev" {
		t.Fatalf("unexpected browser state: %+v", th.BrowserState)
	}
	if len(out) == 0 {
		t.Fatal("expected a thread/updated notification")
	}
	last := out[len(out)-1]
	if last.method != NotificationThreadUpdated {
		t.Fatalf("expected thread/updated notification, got %+v", last)
	}
	updated, ok := last.params.(ThreadUpdatedNotification)
	if !ok {
		t.Fatalf("unexpected params type: %T", last.params)
	}
	if len(updated.Thread.ListeningPorts) != 2 || updated.Thread.ListeningPorts[0] != 8080 {
		t.Fatalf("expected snapshot to carry normalized ports, got %+v", updated.Thread.ListeningPorts)
	}
	if updated.Thread.BrowserState == nil || updated.Thread.BrowserState.PrimaryPreviewURL != "http://localhost:8080" || updated.Thread.BrowserState.LinkedProcessID != "proc-dev" {
		t.Fatalf("expected snapshot to carry browser state, got %+v", updated.Thread.BrowserState)
	}

	// A second report that produces the same normalized list should not
	// emit a duplicate thread/updated notification (the desktop would
	// otherwise re-jump the browser preview).
	outRepeat := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventToolUseEnd,
		ToolCall: &providers.ToolCall{
			ID:        "call_2",
			Name:      "report_listening_ports",
			Arguments: `{"ports":[8080,3000]}`,
		},
		ToolResult: `{"status":"noted","ports":[8080,3000],"preview_urls":["http://localhost:8080","http://localhost:3000"],"process_id":"proc-dev"}`,
	}, now)
	for _, n := range outRepeat {
		if n.method == NotificationThreadUpdated {
			t.Fatalf("did not expect a duplicate thread/updated when ports are unchanged, got %+v", n)
		}
	}

	// Reporting an empty list clears the surfaced ports and emits a
	// fresh update so the desktop can hide the chips and stop trying
	// to auto-navigate.
	outEmpty := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventToolUseEnd,
		ToolCall: &providers.ToolCall{
			ID:        "call_3",
			Name:      "report_listening_ports",
			Arguments: `{"ports":[]}`,
		},
		ToolResult: `{"status":"noted","ports":[]}`,
	}, now)
	if len(th.ListeningPorts) != 0 {
		t.Fatalf("expected empty ports after explicit empty report, got %v", th.ListeningPorts)
	}
	if th.BrowserState.PrimaryPreviewURL != "" || th.BrowserState.LinkedProcessID != "" {
		t.Fatalf("expected empty report to clear active browser preview, got %+v", th.BrowserState)
	}
	var sawUpdate bool
	for _, n := range outEmpty {
		if n.method == NotificationThreadUpdated {
			sawUpdate = true
		}
	}
	if !sawUpdate {
		t.Fatalf("expected a thread/updated notification when ports transition to empty, got %+v", outEmpty)
	}
}

func TestThreadStateCapturesBrowserPreviewFromProcessResult(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", "", now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "start vite"}, now)

	out := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventToolUseEnd,
		ToolCall: &providers.ToolCall{
			ID:   "call_1",
			Name: "start_process",
		},
		ToolResult: `{"action":"start_process","id":"proc-dev","preview_urls":["http://localhost:5173/"],"primary_preview_url":"http://localhost:5173/","status":"running"}`,
	}, now)

	if th.BrowserState.PrimaryPreviewURL != "http://localhost:5173/" || th.BrowserState.CurrentURL != "http://localhost:5173/" || th.BrowserState.LinkedProcessID != "proc-dev" {
		t.Fatalf("unexpected browser state: %+v", th.BrowserState)
	}
	var sawUpdate bool
	for _, n := range out {
		if n.method == NotificationThreadUpdated {
			sawUpdate = true
		}
	}
	if !sawUpdate {
		t.Fatalf("expected thread update when process result contains preview, got %+v", out)
	}
}

func TestSnapshotIncludesListeningPorts(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", "", now)
	th.ListeningPorts = []int{5173, 3000}
	snap := th.snapshotLocked()
	if len(snap.ListeningPorts) != 2 || snap.ListeningPorts[0] != 5173 || snap.ListeningPorts[1] != 3000 {
		t.Fatalf("expected snapshot to carry listening ports, got %+v", snap.ListeningPorts)
	}
	// Mutating the live threadState after the snapshot must not affect
	// the snapshot we already handed out — the clone is a defensive
	// copy so the renderer never sees an aliased slice.
	th.ListeningPorts[0] = 9999
	if snap.ListeningPorts[0] != 5173 {
		t.Fatalf("snapshot ports must be detached from the live slice, got %v", snap.ListeningPorts)
	}
}
