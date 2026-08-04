package appserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

func TestThreadSnapshotExposesInterruptedOrchestration(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/tmp", false, now)
	th.workerTreeFrozen = true

	snapshot := th.snapshotLocked()
	if !snapshot.TreeInterrupted {
		t.Fatal("snapshot must expose the frozen worker tree as an interrupted orchestration")
	}
}

func TestEmptyTurnsKeepItemsAsAnArray(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	tests := []struct {
		name  string
		start func(*threadState) Turn
	}{
		{
			name: "internal turn",
			start: func(th *threadState) Turn {
				return th.startInternalTurnLocked("turn", now)
			},
		},
		{
			name: "agent turn",
			start: func(th *threadState) Turn {
				turn, _ := th.startAgentTurnLocked(now)
				return turn
			},
		},
		{
			name: "ensured turn",
			start: func(th *threadState) Turn {
				return th.ensureTurnLocked("turn", now)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			turn := test.start(newThreadState("thread", nil, "provider", "model", "/repo", false, now))
			if turn.Items == nil {
				t.Fatal("empty turn items must be an initialized slice")
			}
		})
	}
}

func TestTurnJSONEncodesNilItemsAsEmptyArray(t *testing.T) {
	encoded, err := json.Marshal(Turn{ID: "turn"})
	if err != nil {
		t.Fatalf("marshal turn: %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal turn JSON: %v", err)
	}
	if got := string(payload["items"]); got != "[]" {
		t.Fatalf("items JSON = %s, want []", got)
	}
}

func TestThreadStateRetainsExecutionLeaseUntilRelease(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)
	th.cancel = func() {}

	turn := th.finishTurnLocked("turn", TurnStatusCompleted, nil, now.Add(time.Second), "stop", "", false)
	if turn.Status != TurnStatusCompleted {
		t.Fatalf("turn status = %q, want completed", turn.Status)
	}
	if !th.running || th.currentTurn != "turn" || th.cancel == nil {
		t.Fatalf("finishing should retain execution lease: running=%v current=%q cancel_nil=%v", th.running, th.currentTurn, th.cancel == nil)
	}

	th.releaseTurnExecutionLocked("turn")
	if th.running || th.currentTurn != "" || th.cancel != nil {
		t.Fatalf("release should make thread idle: running=%v current=%q cancel_nil=%v", th.running, th.currentTurn, th.cancel == nil)
	}
}

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

func TestThreadStatePreservesRichToolResultDetail(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)
	call := providers.ToolCall{ID: "call-1", Name: "browser_observe"}
	th.applyStreamEventLocked("turn", providers.StreamEvent{Type: providers.EventToolUseStart, ToolCall: &call}, now)
	detail := toolresult.Result{
		Content:           []toolresult.ContentPart{{Type: "image", Data: "aW1hZ2U=", MIMEType: "image/png"}},
		StructuredContent: []byte(`{"url":"https://example.test"}`),
		Meta:              []byte(`{"private":true}`),
	}
	th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:             providers.EventToolUseEnd,
		ToolCall:         &call,
		ToolResult:       detail.TextProjection(),
		ToolResultDetail: &detail,
	}, now)

	turn := th.ensureTurnLocked("turn", now)
	item := turn.Items[len(turn.Items)-1]
	if item.ResultDetail == nil || item.ResultDetail.JSONProjection() != detail.JSONProjection() {
		t.Fatalf("rich tool detail missing from item: %+v", item)
	}
}

func TestThreadStateSupersedesToolDraftOnInferenceReplay(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "inspect"}, now)
	call := providers.ToolCall{ID: "call-1", Name: "read_file"}
	th.applyStreamEventLocked("turn", providers.StreamEvent{Type: providers.EventToolUseStart, ToolCall: &call}, now)
	out := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type: providers.EventLifecycle,
		Lifecycle: &providers.StreamLifecycle{
			Phase: providers.StreamPhaseReconnecting, ResetPartial: true,
		},
	}, now.Add(time.Second))

	turn := th.ensureTurnLocked("turn", now)
	item := turn.Items[len(turn.Items)-1]
	if item.Status != ThreadItemStatusFailed || !strings.Contains(item.Error, "Superseded") {
		t.Fatalf("superseded tool draft = %+v", item)
	}
	if len(out) != 1 || out[0].method != NotificationItemCompleted {
		t.Fatalf("supersede notifications = %+v", out)
	}
	if _, exists := th.toolItems[call.ID]; exists {
		t.Fatal("superseded provider call id remained registered")
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

// TestChatMessageItemProjectsAgentNotificationName verifies that the
// user-message projection surfaces msg.Name onto ThreadItem.Name so the
// renderer (and any other downstream consumer) can gate on the reliable
// server-set field rather than sniffing the JSON payload.
func TestChatMessageItemProjectsAgentNotificationName(t *testing.T) {
	item := chatMessageItem("item-handoff", providers.ChatMessage{
		Role:    "user",
		Name:    "wuu_agent_notification",
		Content: `{"author":"/root/worker","recipient":"/root","content":"<subagent_notification>\n{\"agent_path\":\"/root/worker\"}\n</subagent_notification>","trigger_turn":true}`,
	})
	if item.Name != "wuu_agent_notification" {
		t.Fatalf("item.Name = %q, want %q", item.Name, "wuu_agent_notification")
	}
}

// TestChatMessageItemPreservesCombinedAgentCompletionName exercises the
// drain-combine scenario: combineAgentCompletionMessages joins ≥2 envelopes
// with "\n\n" and forwards the first non-empty Name onto the synthesized
// turn. The projection must keep that Name intact so the renderer's name
// gate still fires even when Content is no longer a single JSON object.
func TestChatMessageItemPreservesCombinedAgentCompletionName(t *testing.T) {
	envelope1 := `{"author":"/root/a","recipient":"/root","content":"<subagent_notification>1</subagent_notification>","trigger_turn":true}`
	envelope2 := `{"author":"/root/b","recipient":"/root","content":"<subagent_notification>2</subagent_notification>","trigger_turn":true}`
	item := chatMessageItem("item-combined", providers.ChatMessage{
		Role:    "user",
		Name:    "wuu_agent_notification",
		Content: envelope1 + "\n\n" + envelope2,
	})
	if item.Name != "wuu_agent_notification" {
		t.Fatalf("item.Name = %q, want %q (combined turn lost the agent-notification name)", item.Name, "wuu_agent_notification")
	}
}

// TestChatMessageItemLeavesNameEmptyForPlainUserMessage guards normal user
// input: msg.Name == "" must project to ThreadItem.Name == "" so the wire
// stays free of an empty "name" key (omitempty on the struct field).
func TestChatMessageItemLeavesNameEmptyForPlainUserMessage(t *testing.T) {
	item := chatMessageItem("item-plain", providers.ChatMessage{
		Role:    "user",
		Content: "do the thing",
	})
	if item.Name != "" {
		t.Fatalf("item.Name = %q, want empty for plain user message", item.Name)
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
		{Role: "user", Name: "wuu_context_anchor", Content: "<system>CHECKPOINT 0</system>", Hidden: true},
		{Role: "user", Name: "wuu_context_continuation", Content: "<system-reminder>\n[Wuu context continuation]\nContinue.\n</system-reminder>", Hidden: true},
		{Role: "user", Name: "wuu_system_reminder", Content: "hidden environment", Hidden: true},
		{Role: "user", Content: "visible request"},
	})

	if preview != "visible request" {
		t.Fatalf("preview = %q, want visible request", preview)
	}
}

func TestThreadPreviewSkipsAgentNotificationEnvelope(t *testing.T) {
	preview := threadPreview([]providers.ChatMessage{
		{
			Role:    "user",
			Name:    "wuu_agent_notification",
			Content: `{"author":"/root/reviewer","recipient":"/root","content":"<subagent_notification>done</subagent_notification>","trigger_turn":true}`,
		},
		{Role: "assistant", Content: "acknowledged"},
		{Role: "user", Content: "检查分叉后的标题"},
	})

	if preview != "检查分叉后的标题" {
		t.Fatalf("preview = %q, want visible user request", preview)
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

func TestTurnsFromHistoryLoadsRetiredContextRewriteArtifact(t *testing.T) {
	const retiredToolName = "inception"
	now := time.Unix(0, 0).UTC()
	turns := turnsFromHistory("thread", []providers.ChatMessage{
		{Role: "user", Content: "start"},
		{Role: "user", Name: "wuu_context_anchor", Content: "<system>CHECKPOINT 0</system>", Hidden: true},
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{{
				ID:        "call_retired",
				Name:      retiredToolName,
				Arguments: `{"anchor_id":0,"summary":"state"}`,
			}},
		},
		{Role: "tool", Name: retiredToolName, ToolCallID: "call_retired", Content: `{"action":"inception","status":"completed"}`},
		{Role: "user", Name: "wuu_context_continuation", Content: "<system-reminder>\n[Wuu context continuation]\nContinue.\n</system-reminder>", Hidden: true},
		{Role: "assistant", Content: "done"},
	}, now)

	if len(turns) != 1 {
		t.Fatalf("expected one visible turn, got %+v", turns)
	}
	items := turns[0].Items
	if len(items) != 3 {
		t.Fatalf("expected user, retired tool call, and assistant items, got %+v", items)
	}
	if items[0].Type != ThreadItemUserMessage || items[0].Text != "start" {
		t.Fatalf("expected visible user item, got %+v", items[0])
	}
	if items[1].Name != retiredToolName {
		t.Fatalf("expected retired tool call as second item, got %+v", items[1])
	}
	if items[2].Type != ThreadItemAgentMessage || items[2].Text != "done" {
		t.Fatalf("expected visible assistant item, got %+v", items[2])
	}
}

func TestThreadStateUpdatesContextCompactionProgressItem(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	th := newThreadState("thread", nil, "provider", "model", "/repo", false, now)
	th.startTurnLocked("turn", providers.ChatMessage{Role: "user", Content: "continue"}, now)

	started := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:          providers.EventCompact,
		CompactReason: "proactive",
		CompactPhase:  providers.CompactPhaseStarted,
	}, now)
	turn := th.ensureTurnLocked("turn", now)
	if len(turn.Items) != 2 || turn.Items[1].Type != ThreadItemContextCompaction || turn.Items[1].Status != ThreadItemStatusInProgress {
		t.Fatalf("compact start should add one in-progress item, got %+v", turn.Items)
	}
	if len(started) != 1 || started[0].method != NotificationItemStarted {
		t.Fatalf("compact start notifications = %+v, want one item/started", started)
	}
	itemID := turn.Items[1].ID

	completed := th.applyStreamEventLocked("turn", providers.StreamEvent{
		Type:          providers.EventCompact,
		Content:       "✦ Compacted history: 8 → 3 messages",
		CompactReason: "proactive",
		CompactPhase:  providers.CompactPhaseCompleted,
	}, now.Add(time.Second))
	turn = th.ensureTurnLocked("turn", now)
	if len(turn.Items) != 2 || turn.Items[1].ID != itemID || turn.Items[1].Status != ThreadItemStatusCompleted {
		t.Fatalf("compact completion should update the progress item in place, got %+v", turn.Items)
	}
	if len(completed) != 1 || completed[0].method != NotificationItemCompleted {
		t.Fatalf("compact completion notifications = %+v, want one item/completed", completed)
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
