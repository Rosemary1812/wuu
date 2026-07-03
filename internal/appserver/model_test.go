package appserver

import (
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestThreadStateCompletesPreambleBeforeToolStart(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
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

func TestThreadStateLeavesUnresolvedTextPhaseUnknown(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)

	out := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:    providers.EventContentDelta,
		Content: "I will inspect the current prompt path.",
	}, now)

	turn := th.ensureTurnLocked("turn", now)
	if len(turn.Items) != 2 {
		t.Fatalf("expected user and live assistant items, got %+v", turn.Items)
	}
	live := turn.Items[1]
	if live.Type != ThreadItemAgentMessage || live.Status != ThreadItemStatusInProgress {
		t.Fatalf("expected live assistant item, got %+v", live)
	}
	if live.Phase != "" {
		t.Fatalf("unresolved assistant text should have unknown phase, got %+v", live)
	}
	started, ok := out[0].params.(ItemStartedNotification)
	if !ok || started.Item.Phase != "" {
		t.Fatalf("started notification should leave phase unknown, got %#v", out[0].params)
	}
}

func TestThreadStateUsesProviderPhaseOnStreamingText(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)

	out := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:    providers.EventContentDelta,
		Content: "The result is clear.",
		Phase:   providers.MessagePhaseFinalAnswer,
	}, now)

	turn := th.ensureTurnLocked("turn", now)
	if len(turn.Items) != 2 {
		t.Fatalf("expected user and live assistant items, got %+v", turn.Items)
	}
	live := turn.Items[1]
	if live.Phase != ThreadItemPhaseFinalAnswer {
		t.Fatalf("streaming text should preserve provider phase, got %+v", live)
	}
	started, ok := out[0].params.(ItemStartedNotification)
	if !ok || started.Item.Phase != ThreadItemPhaseFinalAnswer {
		t.Fatalf("started notification should carry provider phase, got %#v", out[0].params)
	}
}

func TestThreadStateToolResultMessageDoesNotDuplicateCompletion(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)

	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:     providers.EventToolUseStart,
		ToolCall: &providers.ToolCall{ID: "call_1", Name: "read_file"},
	}, now)
	// The agent loop first streams the (display-truncated) tool result...
	out := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:       providers.EventToolUseEnd,
		ToolCall:   &providers.ToolCall{ID: "call_1", Name: "read_file"},
		ToolResult: "package appserver",
	}, now)
	if got := countNotifications(out, NotificationItemCompleted); got != 1 {
		t.Fatalf("tool-use end should complete the item once, got %d", got)
	}
	// ...then forwards the recorded history message for the same call.
	out = th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:    providers.EventMessage,
		Message: &providers.ChatMessage{Role: "tool", ToolCallID: "call_1", Content: "package appserver\n\nfunc more() {}"},
	}, now)
	if got := countNotifications(out, NotificationItemCompleted); got != 0 {
		t.Fatalf("tool history message must not re-announce completion, got %d notifications: %#v", got, out)
	}

	turn := th.ensureTurnLocked("turn", now)
	if len(turn.Items) != 2 {
		t.Fatalf("expected user and tool items, got %+v", turn.Items)
	}
	toolItem := turn.Items[1]
	if toolItem.Result != "package appserver\n\nfunc more() {}" {
		t.Fatalf("tool result should be upgraded to full message content without doubling, got %q", toolItem.Result)
	}
	if toolItem.Status != ThreadItemStatusCompleted {
		t.Fatalf("tool item should stay completed, got %+v", toolItem)
	}
}

func TestThreadStateToolResultMessageAloneCompletesItem(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)

	// No streamed tool-result event (e.g. replayed or non-streaming path):
	// the history message must still create and complete the item.
	out := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:    providers.EventMessage,
		Message: &providers.ChatMessage{Role: "tool", ToolCallID: "call_9", Content: "done"},
	}, now)
	if got := countNotifications(out, NotificationItemCompleted); got != 1 {
		t.Fatalf("tool message without prior stream event should complete the item once, got %d", got)
	}
	turn := th.ensureTurnLocked("turn", now)
	toolItem := turn.Items[len(turn.Items)-1]
	if toolItem.Result != "done" || toolItem.Status != ThreadItemStatusCompleted {
		t.Fatalf("unexpected tool item %+v", toolItem)
	}
}

func countNotifications(out []outboundNotification, method string) int {
	count := 0
	for _, n := range out {
		if n.method == method {
			count++
		}
	}
	return count
}

func TestThreadStateCarriesToolCallDisplay(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
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

func TestThreadStateCarriesProviderToolCallIDAsSourceID(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)

	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventToolUseStart,
		ToolCall: &providers.ToolCall{
			ID:   "call_provider_1",
			Name: "run_shell",
		},
	}, now)

	turn := th.ensureTurnLocked("turn", now)
	if len(turn.Items) != 2 {
		t.Fatalf("expected user and tool items, got %+v", turn.Items)
	}
	toolItem := turn.Items[1]
	if toolItem.ID == "call_provider_1" {
		t.Fatalf("tool item should keep UI item id separate from provider call id: %+v", toolItem)
	}
	if toolItem.SourceID != "call_provider_1" {
		t.Fatalf("tool item SourceID = %q, want provider call id", toolItem.SourceID)
	}
}

func TestChatMessageItemUsesDisplayContentForUserMessage(t *testing.T) {
	item := chatMessageItem("item-1", providers.ChatMessage{
		Role:           "user",
		Content:        "expanded model prompt",
		DisplayContent: "/debug login failure",
	})

	if item.Text != "/debug login failure" {
		t.Fatalf("item.Text = %q, want display content", item.Text)
	}
}

func TestThreadPreviewUsesDisplayContent(t *testing.T) {
	preview := threadPreview([]providers.ChatMessage{{
		Role:           "user",
		Content:        "expanded model prompt",
		DisplayContent: "/debug login failure",
	}})

	if preview != "/debug login failure" {
		t.Fatalf("preview = %q, want display content", preview)
	}
}

func TestThreadPreviewSkipsInternalContextMessages(t *testing.T) {
	preview := threadPreview([]providers.ChatMessage{
		compact.BuildContextAnchorMessage(0),
		{Role: "user", Name: compact.ContextContinuationName, Content: compact.BuildInceptionContinuationContent(0, "## Task state\nContinue.")},
		{Role: "user", Name: "wuu_system_reminder", Content: "hidden environment", Hidden: true},
		{Role: "user", Content: "visible request"},
	})

	if preview != "visible request" {
		t.Fatalf("preview = %q, want visible request", preview)
	}
}

func TestTurnsFromHistorySkipsHiddenMessages(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	turns := turnsFromHistory("thread", []providers.ChatMessage{
		{Role: "user", Name: "wuu_system_reminder", Content: "hidden environment", Hidden: true},
		{Role: "user", Content: "visible request"},
		{Role: "user", Name: "wuu_task_contract", Content: "hidden task contract", Hidden: true},
		{Role: "assistant", Content: "done"},
	}, now)

	if len(turns) != 1 {
		t.Fatalf("expected one visible turn, got %+v", turns)
	}
	items := turns[0].Items
	if len(items) != 2 {
		t.Fatalf("expected only visible user and assistant items, got %+v", items)
	}
	if items[0].Type != ThreadItemUserMessage || items[0].Text != "visible request" {
		t.Fatalf("expected visible user item, got %+v", items[0])
	}
	if items[1].Type != ThreadItemAgentMessage || items[1].Text != "done" {
		t.Fatalf("expected visible assistant item, got %+v", items[1])
	}
}

func TestTurnsFromPersistedHistoryRestoresParticipantMessage(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	turns := turnsFromPersistedHistory("thread", []persistedMessage{
		{Role: "user", Content: "review this diff"},
		{
			Role:          "participant",
			Content:       "Found one regression in the reconnect loop.",
			ParticipantID: "prt-reviewer",
			PostKind:      "result",
		},
		{Role: "assistant", Content: "I will use Noel's result."},
	}, now, func(id string) (participant.Summary, bool) {
		if id != "prt-reviewer" {
			return participant.Summary{}, false
		}
		return participant.Summary{
			ID:     id,
			Name:   "Noel",
			Kind:   string(participant.KindEphemeral),
			Role:   "reviewer",
			Avatar: participant.DefaultAvatar("reviewer"),
		}, true
	})

	if len(turns) != 1 {
		t.Fatalf("expected one visible turn, got %+v", turns)
	}
	items := turns[0].Items
	if len(items) != 3 {
		t.Fatalf("expected user, participant result, and assistant items, got %+v", items)
	}
	result := items[1]
	if result.Type != ThreadItemParticipantMsg || result.PostKind != "result" || result.Text != "Found one regression in the reconnect loop." {
		t.Fatalf("expected participant result item, got %+v", result)
	}
	if result.Participant == nil || result.Participant.Name != "Noel" || result.Participant.Role != "reviewer" {
		t.Fatalf("expected participant summary on result item, got %+v", result.Participant)
	}
}

func TestTurnsFromPersistedHistorySkipsConversationThreadRows(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	turns := turnsFromPersistedHistory("thread", []persistedMessage{
		{Role: "user", Content: "main request"},
		{Role: "assistant", Content: "private investigation", ThreadID: "cth-review"},
		{Role: "participant", Content: "private result", ParticipantID: "prt-reviewer", PostKind: "result", ThreadID: "cth-review"},
		{Role: "assistant", Content: "main response"},
	}, now, nil)

	if len(turns) != 1 {
		t.Fatalf("expected one main turn, got %+v", turns)
	}
	items := turns[0].Items
	if len(items) != 2 {
		t.Fatalf("subthread rows should not be visible in main turn list: %+v", items)
	}
	if items[0].Text != "main request" || items[1].Text != "main response" {
		t.Fatalf("unexpected main items: %+v", items)
	}
}

func TestAppendParticipantMessageLockedAddsCurrentTurnItem(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "review"}, now)

	turn, item, created := th.appendParticipantMessageLocked(persistedMessage{
		Role:          "participant",
		Content:       "Found a missing nil check.",
		ClientID:      "agent-1-result",
		Name:          "reviewer",
		ParticipantID: "prt-reviewer",
		PostKind:      "result",
	}, now, func(id string) (participant.Summary, bool) {
		if id != "prt-reviewer" {
			return participant.Summary{}, false
		}
		return participant.Summary{
			ID:     id,
			Name:   "Noel",
			Kind:   string(participant.KindEphemeral),
			Role:   "reviewer",
			Avatar: participant.DefaultAvatar("reviewer"),
		}, true
	})

	if created {
		t.Fatal("participant message should use the current turn")
	}
	if turn.ID != "turn" || len(turn.Items) != 2 {
		t.Fatalf("expected participant item appended to current turn, got %+v", turn)
	}
	if len(th.History) != 1 {
		t.Fatalf("expected participant context appended to model history, got %+v", th.History)
	}
	ctx := th.History[0]
	if !ctx.Hidden || ctx.Name != participantModelContextMessageName || ctx.ParticipantID != "prt-reviewer" || ctx.ParticipantName != "reviewer" || ctx.PostKind != "result" {
		t.Fatalf("unexpected participant context metadata: %+v", ctx)
	}
	if !strings.Contains(ctx.Content, "Found a missing nil check.") {
		t.Fatalf("participant context missing result text: %q", ctx.Content)
	}
	if item.Type != ThreadItemParticipantMsg || item.Status != ThreadItemStatusCompleted || item.PostKind != "result" {
		t.Fatalf("unexpected participant item: %+v", item)
	}
	if item.Text != "Found a missing nil check." || item.SourceID != "agent-1-result" || item.AgentID != "agent-1-result" {
		t.Fatalf("unexpected participant text/source: %+v", item)
	}
	if item.Participant == nil || item.Participant.Name != "Noel" || item.Participant.Role != "reviewer" {
		t.Fatalf("expected participant summary on item, got %+v", item.Participant)
	}
}

func TestParticipantMessageItemDoesNotInferLegacyTimestampSourceAsAgentID(t *testing.T) {
	item := participantMessageItem("item", persistedMessage{
		Role:          "participant",
		Content:       "legacy result",
		ClientID:      "reviewer-1a2b3c4d-1783000000000000000",
		ParticipantID: "prt-reviewer",
		PostKind:      "result",
	}, nil)

	if item.SourceID == "" {
		t.Fatal("expected legacy source_id to be preserved")
	}
	if item.AgentID != "" {
		t.Fatalf("legacy timestamp source should not become agent_id, got %+v", item)
	}
}

func TestTurnsFromHistorySurfacesInceptionArtifact(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	turns := turnsFromHistory("thread", []providers.ChatMessage{
		{Role: "user", Content: "start"},
		compact.BuildContextAnchorMessage(0),
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{{
				ID:        "call_inception",
				Name:      compact.InceptionToolName,
				Arguments: `{"anchor_id":0,"summary":"state"}`,
			}},
		},
		{Role: "tool", Name: compact.InceptionToolName, ToolCallID: "call_inception", Content: `{"action":"inception","status":"completed"}`},
		{Role: "user", Name: compact.ContextContinuationName, Content: compact.BuildInceptionContinuationContent(0, "## Task state\nContinue.")},
		{Role: "assistant", Content: "done"},
	}, now)

	if len(turns) != 1 {
		t.Fatalf("expected one visible turn, got %+v", turns)
	}
	items := turns[0].Items
	if len(items) != 3 {
		t.Fatalf("expected user, inception tool call, and assistant items, got %+v", items)
	}
	if items[0].Type != ThreadItemUserMessage || items[0].Text != "start" {
		t.Fatalf("expected visible user item, got %+v", items[0])
	}
	if items[1].Name != compact.InceptionToolName {
		t.Fatalf("expected inception tool call as second item, got %+v", items[1])
	}
	if items[2].Type != ThreadItemAgentMessage || items[2].Text != "done" {
		t.Fatalf("expected visible assistant item, got %+v", items[2])
	}
}

func TestThreadStateSurfacesLiveInceptionEvents(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "start"}, now)

	for _, ev := range []providers.StreamEvent{
		{Type: providers.EventToolUseStart, ToolCall: &providers.ToolCall{ID: "call_inception", Name: compact.InceptionToolName}},
		{Type: providers.EventToolUseDelta, Content: `{"anchor_id":0`},
		{Type: providers.EventToolUseEnd, ToolCall: &providers.ToolCall{ID: "call_inception", Name: compact.InceptionToolName}, ToolResult: `{"action":"inception"}`},
	} {
		th.applyStreamEventLocked("turn", ev, now)
	}
	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:     providers.EventToolUseStart,
		ToolCall: &providers.ToolCall{ID: "call_read", Name: "read_file", Arguments: `{"path":"README.md"}`},
	}, now)

	turn := th.ensureTurnLocked("turn", now)
	if len(turn.Items) != 3 {
		t.Fatalf("expected user, inception, and read_file, got %+v", turn.Items)
	}
	if turn.Items[1].Name != compact.InceptionToolName {
		t.Fatalf("expected inception tool call as second item, got %+v", turn.Items[1])
	}
	if turn.Items[2].Name != "read_file" || turn.Items[2].Arguments != `{"path":"README.md"}` {
		t.Fatalf("expected read_file as third item, got %+v", turn.Items[2])
	}
}

func TestThreadStateLabelsInceptionCompactEvent(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "start"}, now)

	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:          providers.EventCompact,
		Content:       "✦ Inception rewrote history: 9 → 3 messages",
		CompactReason: "inception",
	}, now)

	turn := th.ensureTurnLocked("turn", now)
	if len(turn.Items) != 2 {
		t.Fatalf("expected user and compact item, got %+v", turn.Items)
	}
	if turn.Items[1].Type != ThreadItemContextCompaction || turn.Items[1].Reason != "inception" {
		t.Fatalf("expected inception compact reason on item, got %+v", turn.Items[1])
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
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
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

func TestThreadStateLeavesPostToolStreamingTextPhaseUnknownOnFirstDelta(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
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
		t.Fatalf("expected user, tool, and live agent items, got %+v", turn.Items)
	}
	streamed := turn.Items[2]
	if streamed.Type != ThreadItemAgentMessage || streamed.Status != ThreadItemStatusInProgress {
		t.Fatalf("post-tool text should start a live assistant item, got %+v", streamed)
	}
	if streamed.Phase != "" {
		t.Fatalf("post-tool streaming text should have unknown phase, got %+v", streamed)
	}
	if len(out) == 0 {
		t.Fatal("expected notifications for first text delta")
	}
	started, ok := out[0].params.(ItemStartedNotification)
	if !ok || started.Item.Phase != "" {
		t.Fatalf("started notification should leave phase unknown, got %#v", out[0].params)
	}
}

func TestThreadStateMovesStreamingTextToFinalAnswerOnAssistantMessage(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)

	// 1. Stream preamble + tool_use + tool_result + streamed "final" text.
	//    While streaming, the assistant item phase is unknown.
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
	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:    providers.EventContentDelta,
		Content: "The result is clear.",
	}, now)

	// 2. The complete assistant message arrives (no ToolCalls → final_answer).
	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventMessage,
		Message: &providers.ChatMessage{
			Role:    "assistant",
			Content: "The result is clear.",
		},
	}, now)

	turn := th.ensureTurnLocked("turn", now)
	var agentItems []ThreadItem
	for _, item := range turn.Items {
		if item.Type == ThreadItemAgentMessage {
			agentItems = append(agentItems, item)
		}
	}
	if len(agentItems) != 1 {
		t.Fatalf("expected exactly one agent item, got %+v", agentItems)
	}
	if agentItems[0].Phase != ThreadItemPhaseFinalAnswer {
		t.Fatalf("EventAssistantMessage should promote streaming commentary to final_answer, got %+v", agentItems[0])
	}
	if agentItems[0].Status != ThreadItemStatusCompleted {
		t.Fatalf("EventAssistantMessage should mark the agent item completed, got %+v", agentItems[0])
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

func TestTurnsFromHistoryPreservesProviderAssistantPhase(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	turns := turnsFromHistory("thread", []providers.ChatMessage{
		{Role: "user", Content: "inspect"},
		{
			Role:    "assistant",
			Content: "I checked the files.",
			Phase:   providers.MessagePhaseCommentary,
		},
	}, now)

	if len(turns) != 1 || len(turns[0].Items) != 2 {
		t.Fatalf("expected one turn with user and assistant, got %+v", turns)
	}
	item := turns[0].Items[1]
	if item.Type != ThreadItemAgentMessage || item.Phase != ThreadItemPhaseCommentary {
		t.Fatalf("history should preserve provider assistant phase, got %+v", item)
	}
}

func TestApplyTokenUsageMetasToTurnsAlignsFromNewestTurn(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	turns := turnsFromHistory("thread", []providers.ChatMessage{
		{Role: "user", Content: "old"},
		{Role: "assistant", Content: "old done"},
		{Role: "user", Content: "new"},
		{Role: "assistant", Content: "new done"},
	}, now)
	turns = applyTokenUsageMetasToTurns(turns, []persistedMessage{{
		Role:            "meta",
		Content:         "token_usage",
		Model:           "minimax-m3",
		InputTokens:     19_600,
		ContextTokens:   88_000,
		CacheReadTokens: 113_000,
	}})

	if len(turns) != 2 {
		t.Fatalf("expected two turns, got %+v", turns)
	}
	if turns[0].InputTokens != 0 || turns[0].CacheReadTokens != 0 {
		t.Fatalf("usage should not attach to legacy first turn: %+v", turns[0])
	}
	if turns[1].InputTokens != 19_600 || turns[1].CacheReadTokens != 113_000 || turns[1].ContextTokens != 88_000 || turns[1].UsageModel != "minimax-m3" {
		t.Fatalf("usage should attach to newest turn: %+v", turns[1])
	}
}

func TestThreadStateCapturesListeningPortsFromReport(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
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
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
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
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
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
