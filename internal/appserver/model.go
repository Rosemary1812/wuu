package appserver

import (
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

type outboundNotification struct {
	method string
	params any
}

func newThreadState(id string, history []providers.ChatMessage, rtProvider, model, cwd, memoryPath string, now time.Time) *threadState {
	return &threadState{
		ID:            id,
		History:       append([]providers.ChatMessage(nil), history...),
		CreatedAt:     now,
		UpdatedAt:     now,
		ModelProvider: rtProvider,
		Model:         model,
		CWD:           cwd,
		Turns:         turnsFromHistory(id, history, now),
		MemoryPath:    memoryPath,
		toolItems:     make(map[string]string),
	}
}

func (th *threadState) snapshotLocked() Thread {
	status := ThreadStatusIdle
	if th.running {
		status = ThreadStatusInProgress
	}
	return Thread{
		ID:               th.ID,
		Preview:          threadPreview(th.History),
		ModelProvider:    th.ModelProvider,
		Model:            th.Model,
		CWD:              th.CWD,
		Status:           status,
		Pinned:           th.PinnedAt != nil,
		Archived:         th.ArchivedAt != nil,
		ForkedFromID:     th.ForkedFromID,
		ForkedFromTurnID: th.ForkedFromTurnID,
		ForkedFromItemID: th.ForkedFromItemID,
		CreatedAt:        th.CreatedAt,
		UpdatedAt:        th.UpdatedAt,
		Turns:            cloneTurns(th.Turns),
	}
}

func (th *threadState) startTurnLocked(turnID string, userMsg providers.ChatMessage, now time.Time) Turn {
	th.currentTurn = turnID
	th.running = true
	th.UpdatedAt = now
	th.nextItemIndex = 0
	th.activeAgentItemID = ""
	th.activeReasoningItemID = ""
	th.toolItems = make(map[string]string)

	userItem := chatMessageItem(th.nextItemIDLocked(turnID), userMsg)
	turn := Turn{
		ID:        turnID,
		Items:     []ThreadItem{userItem},
		ItemsView: TurnItemsViewFull,
		Status:    TurnStatusInProgress,
		StartedAt: &now,
	}
	th.Turns = append(th.Turns, turn)
	return turn
}

func (th *threadState) completeTurnLocked(turnID string, status TurnStatus, err error, now time.Time) Turn {
	th.running = false
	th.currentTurn = ""
	th.cancel = nil
	th.UpdatedAt = now
	th.activeAgentItemID = ""
	th.activeReasoningItemID = ""
	th.toolItems = make(map[string]string)

	turn := th.ensureTurnLocked(turnID, now)
	turn.Status = status
	if err != nil {
		turn.Error = &TurnError{Message: err.Error()}
	}
	turn.CompletedAt = &now
	if turn.StartedAt != nil {
		duration := now.Sub(*turn.StartedAt).Milliseconds()
		turn.DurationMS = &duration
	}
	th.replaceTurnLocked(turn)
	return turn
}

func (th *threadState) applyStreamEventLocked(turnID string, ev providers.StreamEvent, now time.Time) []outboundNotification {
	var out []outboundNotification
	switch ev.Type {
	case providers.EventContentDelta:
		if ev.Content == "" {
			return nil
		}
		item, started := th.ensureActiveAgentItemLocked(turnID, now)
		if started {
			out = append(out, itemStarted(th.ID, turnID, item, now))
		}
		item.Text += ev.Content
		th.upsertItemLocked(turnID, item, now)
		out = append(out, outboundNotification{
			method: NotificationAgentMessageDelta,
			params: AgentMessageDeltaNotification{
				ThreadID: th.ID,
				TurnID:   turnID,
				ItemID:   item.ID,
				Delta:    ev.Content,
			},
		})
	case providers.EventThinkingDelta:
		if ev.Content == "" {
			return nil
		}
		item, started := th.ensureActiveReasoningItemLocked(turnID, now)
		if started {
			out = append(out, itemStarted(th.ID, turnID, item, now))
		}
		item.Text += ev.Content
		th.upsertItemLocked(turnID, item, now)
		out = append(out, outboundNotification{
			method: NotificationReasoningDelta,
			params: ReasoningDeltaNotification{
				ThreadID: th.ID,
				TurnID:   turnID,
				ItemID:   item.ID,
				Delta:    ev.Content,
			},
		})
	case providers.EventThinkingDone:
		if th.activeReasoningItemID == "" {
			return nil
		}
		item, ok := th.itemLocked(turnID, th.activeReasoningItemID)
		if !ok {
			return nil
		}
		item.Status = ThreadItemStatusCompleted
		th.upsertItemLocked(turnID, item, now)
		th.activeReasoningItemID = ""
		out = append(out, itemCompleted(th.ID, turnID, item, now))
	case providers.EventToolUseStart:
		if ev.ToolCall == nil {
			return nil
		}
		item := th.toolItemFromCallLocked(turnID, *ev.ToolCall, now)
		out = append(out, itemStarted(th.ID, turnID, item, now))
	case providers.EventToolUseDelta:
		if ev.Content == "" {
			return nil
		}
		item, ok := th.latestToolItemLocked(turnID)
		if !ok {
			return nil
		}
		item.Arguments += ev.Content
		th.upsertItemLocked(turnID, item, now)
		out = append(out, outboundNotification{
			method: NotificationToolCallDelta,
			params: ToolCallDeltaNotification{
				ThreadID: th.ID,
				TurnID:   turnID,
				ItemID:   item.ID,
				Delta:    ev.Content,
			},
		})
	case providers.EventToolUseEnd:
		if ev.ToolCall == nil {
			return nil
		}
		item := th.toolItemFromCallLocked(turnID, *ev.ToolCall, now)
		if ev.ToolResult != "" {
			item.Result += ev.ToolResult
			item.Status = ThreadItemStatusCompleted
			th.upsertItemLocked(turnID, item, now)
			out = append(out, outboundNotification{
				method: NotificationToolCallOutput,
				params: ToolCallOutputNotification{
					ThreadID: th.ID,
					TurnID:   turnID,
					ItemID:   item.ID,
					Delta:    ev.ToolResult,
				},
			})
			out = append(out, itemCompleted(th.ID, turnID, item, now))
		}
	case providers.EventMessage:
		if ev.Message == nil {
			return nil
		}
		out = append(out, th.applyMessageItemLocked(turnID, *ev.Message, now)...)
	case providers.EventCompact:
		item := ThreadItem{
			ID:     th.nextItemIDLocked(turnID),
			Type:   ThreadItemContextCompaction,
			Status: ThreadItemStatusCompleted,
			Text:   ev.Content,
		}
		th.upsertItemLocked(turnID, item, now)
		out = append(out, itemStarted(th.ID, turnID, item, now), itemCompleted(th.ID, turnID, item, now))
	case providers.EventError:
		msg := "stream error"
		if ev.Error != nil {
			msg = ev.Error.Error()
		}
		item := ThreadItem{
			ID:     th.nextItemIDLocked(turnID),
			Type:   ThreadItemError,
			Status: ThreadItemStatusFailed,
			Error:  msg,
		}
		th.upsertItemLocked(turnID, item, now)
		out = append(out, itemStarted(th.ID, turnID, item, now), itemCompleted(th.ID, turnID, item, now))
	}
	return out
}

func (th *threadState) applyMessageItemLocked(turnID string, msg providers.ChatMessage, now time.Time) []outboundNotification {
	switch msg.Role {
	case "assistant":
		if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.ReasoningContent) == "" {
			return nil
		}
		var out []outboundNotification
		if strings.TrimSpace(msg.ReasoningContent) != "" && th.activeReasoningItemID == "" && !th.hasReasoningTextLocked(turnID, msg.ReasoningContent) {
			item := ThreadItem{
				ID:     th.nextItemIDLocked(turnID),
				Type:   ThreadItemReasoning,
				Status: ThreadItemStatusCompleted,
				Text:   msg.ReasoningContent,
			}
			th.upsertItemLocked(turnID, item, now)
			out = append(out, itemStarted(th.ID, turnID, item, now), itemCompleted(th.ID, turnID, item, now))
		}
		if strings.TrimSpace(msg.Content) == "" {
			return out
		}
		item, started := th.ensureActiveAgentItemLocked(turnID, now)
		if started {
			out = append(out, itemStarted(th.ID, turnID, item, now))
		}
		item.Text = msg.Content
		item.Status = ThreadItemStatusCompleted
		th.upsertItemLocked(turnID, item, now)
		th.activeAgentItemID = ""
		out = append(out, itemCompleted(th.ID, turnID, item, now))
		return out
	case "tool":
		if msg.ToolCallID == "" {
			return nil
		}
		item, ok := th.itemLocked(turnID, th.toolItems[msg.ToolCallID])
		if !ok {
			item = ThreadItem{
				ID:     th.nextItemIDLocked(turnID),
				Type:   ThreadItemToolCall,
				Status: ThreadItemStatusInProgress,
			}
			th.toolItems[msg.ToolCallID] = item.ID
		}
		item.Result += msg.Content
		item.Status = ThreadItemStatusCompleted
		th.upsertItemLocked(turnID, item, now)
		return []outboundNotification{itemCompleted(th.ID, turnID, item, now)}
	default:
		return nil
	}
}

func (th *threadState) ensureActiveAgentItemLocked(turnID string, now time.Time) (ThreadItem, bool) {
	if th.activeAgentItemID != "" {
		if item, ok := th.itemLocked(turnID, th.activeAgentItemID); ok {
			return item, false
		}
	}
	item := ThreadItem{
		ID:     th.nextItemIDLocked(turnID),
		Type:   ThreadItemAgentMessage,
		Status: ThreadItemStatusInProgress,
		Role:   "assistant",
	}
	th.activeAgentItemID = item.ID
	th.upsertItemLocked(turnID, item, now)
	return item, true
}

func (th *threadState) ensureActiveReasoningItemLocked(turnID string, now time.Time) (ThreadItem, bool) {
	if th.activeReasoningItemID != "" {
		if item, ok := th.itemLocked(turnID, th.activeReasoningItemID); ok {
			return item, false
		}
	}
	item := ThreadItem{
		ID:     th.nextItemIDLocked(turnID),
		Type:   ThreadItemReasoning,
		Status: ThreadItemStatusInProgress,
	}
	th.activeReasoningItemID = item.ID
	th.upsertItemLocked(turnID, item, now)
	return item, true
}

func (th *threadState) toolItemFromCallLocked(turnID string, call providers.ToolCall, now time.Time) ThreadItem {
	id := strings.TrimSpace(call.ID)
	if id == "" {
		id = fmt.Sprintf("tool-%d", len(th.toolItems)+1)
	}
	if itemID := th.toolItems[id]; itemID != "" {
		if item, ok := th.itemLocked(turnID, itemID); ok {
			if strings.TrimSpace(call.Name) != "" {
				item.Name = call.Name
			}
			if call.Arguments != "" {
				item.Arguments = call.Arguments
			}
			th.upsertItemLocked(turnID, item, now)
			return item
		}
	}
	item := ThreadItem{
		ID:        th.nextItemIDLocked(turnID),
		Type:      threadItemTypeForTool(call.Name),
		Status:    ThreadItemStatusInProgress,
		Name:      call.Name,
		Arguments: call.Arguments,
	}
	th.toolItems[id] = item.ID
	th.upsertItemLocked(turnID, item, now)
	return item
}

func (th *threadState) latestToolItemLocked(turnID string) (ThreadItem, bool) {
	turn := th.ensureTurnLocked(turnID, time.Now())
	for i := len(turn.Items) - 1; i >= 0; i-- {
		if turn.Items[i].Type == ThreadItemToolCall && turn.Items[i].Status == ThreadItemStatusInProgress {
			return turn.Items[i], true
		}
	}
	return ThreadItem{}, false
}

func (th *threadState) hasReasoningTextLocked(turnID, text string) bool {
	turn := th.ensureTurnLocked(turnID, time.Now())
	for _, item := range turn.Items {
		if item.Type == ThreadItemReasoning && item.Text == text {
			return true
		}
	}
	return false
}

func (th *threadState) ensureTurnLocked(turnID string, now time.Time) Turn {
	for _, turn := range th.Turns {
		if turn.ID == turnID {
			return turn
		}
	}
	turn := Turn{
		ID:        turnID,
		ItemsView: TurnItemsViewFull,
		Status:    TurnStatusInProgress,
		StartedAt: &now,
	}
	th.Turns = append(th.Turns, turn)
	return turn
}

func (th *threadState) replaceTurnLocked(turn Turn) {
	for i := range th.Turns {
		if th.Turns[i].ID == turn.ID {
			th.Turns[i] = turn
			return
		}
	}
	th.Turns = append(th.Turns, turn)
}

func (th *threadState) itemLocked(turnID, itemID string) (ThreadItem, bool) {
	if itemID == "" {
		return ThreadItem{}, false
	}
	turn := th.ensureTurnLocked(turnID, time.Now())
	for _, item := range turn.Items {
		if item.ID == itemID {
			return item, true
		}
	}
	return ThreadItem{}, false
}

func (th *threadState) upsertItemLocked(turnID string, item ThreadItem, now time.Time) {
	turn := th.ensureTurnLocked(turnID, now)
	for i := range turn.Items {
		if turn.Items[i].ID == item.ID {
			turn.Items[i] = item
			th.replaceTurnLocked(turn)
			th.UpdatedAt = now
			return
		}
	}
	turn.Items = append(turn.Items, item)
	th.replaceTurnLocked(turn)
	th.UpdatedAt = now
}

func (th *threadState) nextItemIDLocked(turnID string) string {
	th.nextItemIndex++
	return fmt.Sprintf("%s-item-%d", turnID, th.nextItemIndex)
}

func itemStarted(threadID, turnID string, item ThreadItem, at time.Time) outboundNotification {
	return outboundNotification{
		method: NotificationItemStarted,
		params: ItemStartedNotification{
			ThreadID:    threadID,
			TurnID:      turnID,
			Item:        item,
			StartedAtMS: at.UnixMilli(),
		},
	}
}

func itemCompleted(threadID, turnID string, item ThreadItem, at time.Time) outboundNotification {
	return outboundNotification{
		method: NotificationItemCompleted,
		params: ItemCompletedNotification{
			ThreadID:      threadID,
			TurnID:        turnID,
			Item:          item,
			CompletedAtMS: at.UnixMilli(),
		},
	}
}

func turnsFromHistory(threadID string, history []providers.ChatMessage, now time.Time) []Turn {
	var turns []Turn
	var current *Turn
	itemIndex := 0
	toolItems := make(map[string]int)
	nextItemID := func(turnID string) string {
		itemIndex++
		return fmt.Sprintf("%s-item-%d", turnID, itemIndex)
	}
	appendItem := func(item ThreadItem) {
		if current == nil || item.ID == "" {
			return
		}
		current.Items = append(current.Items, item)
	}
	for _, msg := range history {
		if msg.Role == "system" {
			continue
		}
		if msg.Role == "user" && !isToolResultMessage(msg) {
			turnID := fmt.Sprintf("%s-turn-%04d", threadID, len(turns)+1)
			itemIndex = 0
			toolItems = make(map[string]int)
			turn := Turn{
				ID:        turnID,
				ItemsView: TurnItemsViewFull,
				Status:    TurnStatusCompleted,
			}
			turn.Items = append(turn.Items, chatMessageItem(nextItemID(turnID), msg))
			turns = append(turns, turn)
			current = &turns[len(turns)-1]
			continue
		}
		if current == nil {
			continue
		}
		switch msg.Role {
		case "assistant":
			if strings.TrimSpace(msg.ReasoningContent) != "" {
				appendItem(ThreadItem{
					ID:     nextItemID(current.ID),
					Type:   ThreadItemReasoning,
					Status: ThreadItemStatusCompleted,
					Text:   msg.ReasoningContent,
				})
			}
			if strings.TrimSpace(msg.Content) != "" {
				appendItem(ThreadItem{
					ID:     nextItemID(current.ID),
					Type:   ThreadItemAgentMessage,
					Status: ThreadItemStatusCompleted,
					Role:   "assistant",
					Text:   msg.Content,
				})
			}
			for _, call := range msg.ToolCalls {
				item := ThreadItem{
					ID:        nextItemID(current.ID),
					Type:      threadItemTypeForTool(call.Name),
					Status:    ThreadItemStatusCompleted,
					Name:      call.Name,
					Arguments: call.Arguments,
				}
				if strings.TrimSpace(call.ID) != "" {
					toolItems[call.ID] = len(current.Items)
				}
				appendItem(item)
			}
		case "tool":
			if idx, ok := toolItems[msg.ToolCallID]; ok && idx >= 0 && idx < len(current.Items) {
				current.Items[idx].Result += msg.Content
				current.Items[idx].Status = ThreadItemStatusCompleted
				continue
			}
			appendItem(ThreadItem{
				ID:     nextItemID(current.ID),
				Type:   ThreadItemToolCall,
				Status: ThreadItemStatusCompleted,
				Result: msg.Content,
			})
		default:
			item := chatMessageItem(nextItemID(current.ID), msg)
			appendItem(item)
		}
	}
	return turns
}

func chatMessageItem(id string, msg providers.ChatMessage) ThreadItem {
	switch msg.Role {
	case "user":
		return ThreadItem{
			ID:     id,
			Type:   ThreadItemUserMessage,
			Status: ThreadItemStatusCompleted,
			Role:   "user",
			Text:   msg.Content,
			Images: threadItemImages(msg.Images),
		}
	case "assistant":
		if strings.TrimSpace(msg.Content) != "" {
			return ThreadItem{
				ID:     id,
				Type:   ThreadItemAgentMessage,
				Status: ThreadItemStatusCompleted,
				Role:   "assistant",
				Text:   msg.Content,
			}
		}
		if strings.TrimSpace(msg.ReasoningContent) != "" {
			return ThreadItem{
				ID:     id,
				Type:   ThreadItemReasoning,
				Status: ThreadItemStatusCompleted,
				Text:   msg.ReasoningContent,
			}
		}
	case "tool":
		return ThreadItem{
			ID:     id,
			Type:   ThreadItemToolCall,
			Status: ThreadItemStatusCompleted,
			Result: msg.Content,
		}
	}
	return ThreadItem{}
}

func isToolResultMessage(msg providers.ChatMessage) bool {
	return msg.Role == "tool" || msg.ToolCallID != ""
}

func threadItemTypeForTool(name string) ThreadItemType {
	switch strings.TrimSpace(name) {
	case "spawn_agent", "send_message", "followup_task", "wait_agent", "close_agent", "list_agents":
		return ThreadItemCollabAgentTool
	default:
		return ThreadItemToolCall
	}
}

func threadPreview(history []providers.ChatMessage) string {
	for _, msg := range history {
		if msg.Role == "user" && !isToolResultMessage(msg) && strings.TrimSpace(msg.Content) != "" {
			return strings.TrimSpace(msg.Content)
		}
		if msg.Role == "user" && !isToolResultMessage(msg) && len(msg.Images) > 0 {
			if len(msg.Images) == 1 {
				return "[Image #1]"
			}
			return fmt.Sprintf("[%d images]", len(msg.Images))
		}
	}
	return ""
}

func threadItemImages(images []providers.InputImage) []ThreadItemImage {
	if len(images) == 0 {
		return nil
	}
	out := make([]ThreadItemImage, 0, len(images))
	for _, image := range images {
		data := strings.TrimSpace(image.Data)
		if data == "" {
			continue
		}
		mediaType := strings.TrimSpace(image.MediaType)
		if mediaType == "" {
			mediaType = "image/png"
		}
		out = append(out, ThreadItemImage{
			MediaType: mediaType,
			Data:      data,
		})
	}
	return out
}

func cloneTurns(turns []Turn) []Turn {
	out := make([]Turn, len(turns))
	for i, turn := range turns {
		out[i] = turn
		out[i].Items = append([]ThreadItem(nil), turn.Items...)
	}
	return out
}
