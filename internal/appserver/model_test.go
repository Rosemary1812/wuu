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
