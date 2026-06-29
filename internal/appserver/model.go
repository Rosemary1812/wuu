package appserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/compact"
	proc "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type outboundNotification struct {
	method string
	params any
}

func newThreadState(id string, history []providers.ChatMessage, rtProvider, model, cwd string, persistHistory bool, now time.Time) *threadState {
	return &threadState{
		ID:             id,
		History:        cloneHistory(history),
		CreatedAt:      now,
		UpdatedAt:      now,
		ModelProvider:  rtProvider,
		Model:          model,
		CWD:            cwd,
		Turns:          turnsFromHistory(id, history, now),
		PersistHistory: persistHistory,
		toolItems:      make(map[string]string),
	}
}

func (th *threadState) snapshotLocked() Thread {
	status := ThreadStatusIdle
	if th.running {
		status = ThreadStatusInProgress
	}
	return Thread{
		ID:               th.ID,
		ParentID:         th.ParentID,
		AgentPath:        th.AgentPath,
		Preview:          firstNonEmpty(th.Title, threadPreview(th.History)),
		Title:            th.Title,
		ModelProvider:    th.ModelProvider,
		Model:            th.Model,
		CWD:              th.CWD,
		WorkspaceKind:    th.WorkspaceKind,
		Status:           status,
		ReadOnly:         th.ReadOnly,
		Ephemeral:        th.Ephemeral,
		Pinned:           th.PinnedAt != nil,
		Archived:         th.ArchivedAt != nil,
		ForkedFromID:     th.ForkedFromID,
		ForkedFromTurnID: th.ForkedFromTurnID,
		ForkedFromItemID: th.ForkedFromItemID,
		Worktree:         threadWorktreeInfo(th.WorktreePath, th.WorktreeBaseHEAD, th.WorktreeBaseRepo),
		CreatedAt:        th.CreatedAt,
		UpdatedAt:        th.UpdatedAt,
		Turns:            cloneTurns(th.Turns),
		ListeningPorts:   cloneListeningPorts(th.ListeningPorts),
		BrowserState:     cloneThreadBrowserState(th.BrowserState),
	}
}

func threadWorktreeInfo(path, baseHEAD, baseRepo string) *WorktreeInfo {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	return &WorktreeInfo{
		Path:     path,
		BaseHEAD: strings.TrimSpace(baseHEAD),
		BaseRepo: strings.TrimSpace(baseRepo),
	}
}

func (th *threadState) startTurnLocked(turnID string, userMsg providers.ChatMessage, now time.Time) Turn {
	th.currentTurn = turnID
	th.running = true
	th.UpdatedAt = now
	th.pendingSteers = nil
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

func (th *threadState) startInternalTurnLocked(turnID string, now time.Time) Turn {
	th.currentTurn = turnID
	th.running = true
	th.UpdatedAt = now
	th.pendingSteers = nil
	th.nextItemIndex = 0
	th.activeAgentItemID = ""
	th.activeReasoningItemID = ""
	th.toolItems = make(map[string]string)

	turn := Turn{
		ID:        turnID,
		ItemsView: TurnItemsViewFull,
		Status:    TurnStatusInProgress,
		StartedAt: &now,
	}
	th.Turns = append(th.Turns, turn)
	return turn
}

func (th *threadState) startAgentTurnLocked(now time.Time) (Turn, bool) {
	if th.running && th.currentTurn != "" {
		turn := th.ensureTurnLocked(th.currentTurn, now)
		th.nextItemIndex = max(th.nextItemIndex, maxTurnItemIndex(turn))
		return turn, false
	}

	if len(th.Turns) == 0 {
		turnID := fmt.Sprintf("%s-turn-%04d", th.ID, 1)
		turn := Turn{
			ID:        turnID,
			ItemsView: TurnItemsViewFull,
			Status:    TurnStatusInProgress,
			StartedAt: &now,
		}
		th.Turns = append(th.Turns, turn)
		th.currentTurn = turnID
		th.running = true
		th.pendingSteers = nil
		th.nextItemIndex = 0
		return turn, true
	}

	index := len(th.Turns) - 1
	turn := th.Turns[index]
	started := turn.Status != TurnStatusInProgress
	turn.Status = TurnStatusInProgress
	if turn.StartedAt == nil {
		startedAt := th.CreatedAt
		if startedAt.IsZero() {
			startedAt = now
		}
		turn.StartedAt = &startedAt
	}
	turn.CompletedAt = nil
	turn.Error = nil
	th.Turns[index] = turn
	th.currentTurn = turn.ID
	th.running = true
	th.pendingSteers = nil
	th.nextItemIndex = max(th.nextItemIndex, maxTurnItemIndex(turn))
	if th.toolItems == nil {
		th.toolItems = make(map[string]string)
	}
	return turn, started
}

func (th *threadState) completeTurnLocked(turnID string, status TurnStatus, err error, now time.Time, finishReason, stopReason string, truncated bool) Turn {
	th.running = false
	th.currentTurn = ""
	th.cancel = nil
	th.UpdatedAt = now
	if th.activeAgentItemID != "" {
		if item, ok := th.itemLocked(turnID, th.activeAgentItemID); ok && status == TurnStatusCompleted {
			item.Status = ThreadItemStatusCompleted
			item.FinishReason = finishReason
			item.StopReason = stopReason
			item.Truncated = truncated
			th.upsertItemLocked(turnID, item, now)
		}
		th.activeAgentItemID = ""
	}
	th.activeReasoningItemID = ""
	th.toolItems = make(map[string]string)

	turn := th.ensureTurnLocked(turnID, now)
	turn.Status = status
	if err != nil {
		turn.Error = &TurnError{Message: err.Error()}
	}
	turn.FinishReason = finishReason
	turn.StopReason = stopReason
	turn.Truncated = truncated
	turn.CompletedAt = &now
	if turn.StartedAt != nil {
		duration := now.Sub(*turn.StartedAt).Milliseconds()
		turn.DurationMS = &duration
	}
	th.replaceTurnLocked(turn)
	return turn
}

func applyTokenUsageToTurn(turn *Turn, usage providers.TokenUsage, contextTokens, requestContextTokens int, model string) {
	if turn == nil {
		return
	}
	turn.InputTokens = usage.InputTokens
	turn.OutputTokens = usage.OutputTokens
	turn.ContextTokens = contextTokens
	turn.RequestContextTokens = requestContextTokens
	turn.CacheCreationTokens = usage.CacheCreationTokens
	turn.CacheReadTokens = usage.CacheReadTokens
	turn.UsageModel = strings.TrimSpace(model)
}

func applyTokenUsageMetasToTurns(turns []Turn, metas []persistedMessage) []Turn {
	if len(turns) == 0 || len(metas) == 0 {
		return turns
	}
	usages := make([]persistedMessage, 0, len(metas))
	for _, meta := range metas {
		if !strings.EqualFold(strings.TrimSpace(meta.Role), "meta") ||
			strings.TrimSpace(meta.Content) != "token_usage" {
			continue
		}
		if meta.InputTokens == 0 &&
			meta.OutputTokens == 0 &&
			meta.ContextTokens == 0 &&
			meta.RequestContextTokens == 0 &&
			meta.CacheCreationTokens == 0 &&
			meta.CacheReadTokens == 0 {
			continue
		}
		usages = append(usages, meta)
	}
	if len(usages) == 0 {
		return turns
	}
	turnIndex := len(turns) - len(usages)
	usageIndex := 0
	if turnIndex < 0 {
		usageIndex = -turnIndex
		turnIndex = 0
	}
	for turnIndex < len(turns) && usageIndex < len(usages) {
		meta := usages[usageIndex]
		applyTokenUsageToTurn(&turns[turnIndex], providers.TokenUsage{
			InputTokens:         meta.InputTokens,
			OutputTokens:        meta.OutputTokens,
			CacheCreationTokens: meta.CacheCreationTokens,
			CacheReadTokens:     meta.CacheReadTokens,
		}, meta.ContextTokens, meta.RequestContextTokens, meta.Model)
		turnIndex++
		usageIndex++
	}
	return turns
}

func (th *threadState) takePendingSteersLocked(turnID string, now time.Time) ([]providers.ChatMessage, []outboundNotification) {
	if len(th.pendingSteers) == 0 || turnID == "" || turnID != th.currentTurn {
		return nil, nil
	}
	steers := cloneHistory(th.pendingSteers)
	th.pendingSteers = nil
	var out []outboundNotification
	for i := range steers {
		if strings.TrimSpace(steers[i].Role) == "" {
			steers[i].Role = "user"
		}
		steers[i].Steered = true
		item := chatMessageItem(th.nextItemIDLocked(turnID), steers[i])
		if item.ID == "" {
			continue
		}
		th.upsertItemLocked(turnID, item, now)
		out = append(out, itemStarted(th.ID, turnID, item, now), itemCompleted(th.ID, turnID, item, now))
	}
	return steers, out
}

func (th *threadState) drainPendingSteersLocked() []providers.ChatMessage {
	if len(th.pendingSteers) == 0 {
		return nil
	}
	steers := cloneHistory(th.pendingSteers)
	th.pendingSteers = nil
	return steers
}

func (th *threadState) removePendingSteerLocked(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(th.pendingSteers) == 0 {
		return false
	}
	next := th.pendingSteers[:0]
	removed := false
	for _, msg := range th.pendingSteers {
		if !removed && msg.ClientID == id {
			removed = true
			continue
		}
		next = append(next, msg)
	}
	th.pendingSteers = next
	return removed
}

func maxTurnItemIndex(turn Turn) int {
	maxIndex := len(turn.Items)
	for _, item := range turn.Items {
		_, suffix, ok := strings.Cut(item.ID, "-item-")
		if !ok {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(suffix, "%d", &n); err == nil && n > maxIndex {
			maxIndex = n
		}
	}
	return maxIndex
}

func (th *threadState) applyStreamEventLocked(turnID string, ev providers.StreamEvent, now time.Time) []outboundNotification {
	var out []outboundNotification
	switch ev.Type {
	case providers.EventContentDelta:
		if ev.Content == "" {
			return nil
		}
		// Streaming text usually has unknown phase. If the provider exposes
		// Codex-style phase metadata on the active output item, keep it.
		item, started := th.ensureActiveAgentItemLocked(turnID, now, threadItemPhaseFromProvider(ev.Phase))
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
	case providers.EventContentReplace:
		if th.activeAgentItemID == "" && ev.Content == "" {
			return nil
		}
		item, started := th.ensureActiveAgentItemLocked(turnID, now, threadItemPhaseFromProvider(ev.Phase))
		if started {
			out = append(out, itemStarted(th.ID, turnID, item, now))
		}
		item.Text = ev.Content
		th.upsertItemLocked(turnID, item, now)
		out = append(out, outboundNotification{
			method: NotificationAgentMessageReplace,
			params: AgentMessageReplaceNotification{
				ThreadID: th.ID,
				TurnID:   turnID,
				ItemID:   item.ID,
				Text:     ev.Content,
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
	case providers.EventThinkingReplace:
		if th.activeReasoningItemID == "" && ev.Content == "" {
			return nil
		}
		item, started := th.ensureActiveReasoningItemLocked(turnID, now)
		if started {
			out = append(out, itemStarted(th.ID, turnID, item, now))
		}
		item.Text = ev.Content
		th.upsertItemLocked(turnID, item, now)
		out = append(out, outboundNotification{
			method: NotificationReasoningReplace,
			params: ReasoningReplaceNotification{
				ThreadID: th.ID,
				TurnID:   turnID,
				ItemID:   item.ID,
				Text:     ev.Content,
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
		out = append(out, th.completeActiveAgentItemLocked(turnID, now, ThreadItemPhaseCommentary)...)
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
		if th.applyBrowserPreviewFromToolResultLocked(ev.ToolCall.Name, ev.ToolResult, now) {
			out = append(out, outboundNotification{
				method: NotificationThreadUpdated,
				params: ThreadUpdatedNotification{Thread: th.snapshotLocked()},
			})
		}
	case providers.EventMessage:
		if ev.Message == nil {
			return nil
		}
		if ev.Message.Hidden || compact.IsInternalContextMessage(*ev.Message) {
			return nil
		}
		out = append(out, th.applyMessageItemLocked(turnID, *ev.Message, now)...)
	case providers.EventCompact:
		item := ThreadItem{
			ID:     th.nextItemIDLocked(turnID),
			Type:   ThreadItemContextCompaction,
			Status: ThreadItemStatusCompleted,
			Text:   ev.Content,
			Reason: ev.CompactReason,
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
		if th.activeAgentItemID == "" && th.hasAgentTextLocked(turnID, msg.Content) {
			return out
		}
		phase := assistantMessagePhase(msg)
		item, started := th.ensureActiveAgentItemLocked(turnID, now, phase)
		if started {
			out = append(out, itemStarted(th.ID, turnID, item, now))
		}
		item.Text = msg.Content
		item.Phase = phase
		item.Status = ThreadItemStatusCompleted
		item.FinishReason = string(msg.FinishReason)
		item.StopReason = msg.StopReason
		item.Truncated = msg.Truncated
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

func (th *threadState) ensureActiveAgentItemLocked(turnID string, now time.Time, phase ThreadItemPhase) (ThreadItem, bool) {
	if th.activeAgentItemID != "" {
		if item, ok := th.itemLocked(turnID, th.activeAgentItemID); ok {
			if shouldUpdateAgentPhase(item.Phase, phase) {
				item.Phase = phase
				th.upsertItemLocked(turnID, item, now)
			}
			return item, false
		}
	}
	item := ThreadItem{
		ID:     th.nextItemIDLocked(turnID),
		Type:   ThreadItemAgentMessage,
		Status: ThreadItemStatusInProgress,
		Phase:  phase,
		Role:   "assistant",
	}
	th.activeAgentItemID = item.ID
	th.upsertItemLocked(turnID, item, now)
	return item, true
}

func shouldUpdateAgentPhase(current, next ThreadItemPhase) bool {
	if next == "" || current == next {
		return false
	}
	return current == ""
}

func (th *threadState) completeActiveAgentItemLocked(turnID string, now time.Time, phase ThreadItemPhase) []outboundNotification {
	if th.activeAgentItemID == "" {
		return nil
	}
	item, ok := th.itemLocked(turnID, th.activeAgentItemID)
	th.activeAgentItemID = ""
	if !ok {
		return nil
	}
	if phase != "" {
		item.Phase = phase
	}
	item.Status = ThreadItemStatusCompleted
	th.upsertItemLocked(turnID, item, now)
	return []outboundNotification{itemCompleted(th.ID, turnID, item, now)}
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
			if call.Display != nil {
				item.Display = cloneToolCallDisplay(call.Display)
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
		Display:   cloneToolCallDisplay(call.Display),
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

func (th *threadState) hasAgentTextLocked(turnID, text string) bool {
	turn := th.ensureTurnLocked(turnID, time.Now())
	for _, item := range turn.Items {
		if item.Type == ThreadItemAgentMessage && item.Text == text {
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
	var pendingCompactions []ThreadItem
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
		if msg.Hidden {
			continue
		}
		if compact.IsInternalContextMessage(msg) {
			continue
		}
		if msg.Role == "system" {
			if item, ok := contextCompactionItemFromSystemMessage(msg); ok {
				pendingCompactions = append(pendingCompactions, item)
			}
			continue
		}
		if msg.Role == "user" && !isToolResultMessage(msg) {
			if msg.Steered && current != nil {
				appendItem(chatMessageItem(nextItemID(current.ID), msg))
				continue
			}
			turnID := fmt.Sprintf("%s-turn-%04d", threadID, len(turns)+1)
			itemIndex = 0
			toolItems = make(map[string]int)
			turn := Turn{
				ID:        turnID,
				ItemsView: TurnItemsViewFull,
				Status:    TurnStatusCompleted,
			}
			turn.Items = append(turn.Items, chatMessageItem(nextItemID(turnID), msg))
			for _, item := range pendingCompactions {
				item.ID = nextItemID(turnID)
				turn.Items = append(turn.Items, item)
			}
			pendingCompactions = nil
			turns = append(turns, turn)
			current = &turns[len(turns)-1]
			continue
		}
		if current == nil && msg.Role == "assistant" && len(pendingCompactions) > 0 {
			turnID := fmt.Sprintf("%s-turn-%04d", threadID, len(turns)+1)
			itemIndex = 0
			toolItems = make(map[string]int)
			turn := Turn{
				ID:        turnID,
				ItemsView: TurnItemsViewFull,
				Status:    TurnStatusCompleted,
			}
			for _, item := range pendingCompactions {
				item.ID = nextItemID(turnID)
				turn.Items = append(turn.Items, item)
			}
			pendingCompactions = nil
			turns = append(turns, turn)
			current = &turns[len(turns)-1]
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
					ID:           nextItemID(current.ID),
					Type:         ThreadItemAgentMessage,
					Status:       ThreadItemStatusCompleted,
					Phase:        assistantMessagePhase(msg),
					Role:         "assistant",
					Text:         msg.Content,
					FinishReason: string(msg.FinishReason),
					StopReason:   msg.StopReason,
					Truncated:    msg.Truncated,
				})
			}
			if msg.FinishReason != "" || msg.StopReason != "" || msg.Truncated {
				current.FinishReason = string(msg.FinishReason)
				current.StopReason = msg.StopReason
				current.Truncated = msg.Truncated
			}
			for _, call := range msg.ToolCalls {
				item := ThreadItem{
					ID:        nextItemID(current.ID),
					Type:      threadItemTypeForTool(call.Name),
					Status:    ThreadItemStatusCompleted,
					Name:      call.Name,
					Arguments: call.Arguments,
					Display:   cloneToolCallDisplay(call.Display),
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

func contextCompactionItemFromSystemMessage(msg providers.ChatMessage) (ThreadItem, bool) {
	content := strings.TrimSpace(msg.Content)
	switch {
	case compact.IsConversationSummaryContent(content):
		return ThreadItem{
			Type:   ThreadItemContextCompaction,
			Status: ThreadItemStatusCompleted,
			Text:   "Compacted history",
		}, true
	case compact.IsHelpMeJointCompactContent(content):
		return ThreadItem{
			Type:   ThreadItemContextCompaction,
			Status: ThreadItemStatusCompleted,
			Text:   "HelpMe recovered and compacted history",
			Reason: compact.HelpMeToolName,
		}, true
	default:
		return ThreadItem{}, false
	}
}

func chatMessageItem(id string, msg providers.ChatMessage) ThreadItem {
	switch msg.Role {
	case "user":
		return ThreadItem{
			ID:       id,
			SourceID: msg.ClientID,
			Type:     ThreadItemUserMessage,
			Status:   ThreadItemStatusCompleted,
			Role:     "user",
			Text:     chatMessageDisplayContent(msg),
			Images:   threadItemImages(msg.Images),
			Files:    threadItemFiles(msg.Files),
		}
	case "assistant":
		if strings.TrimSpace(msg.Content) != "" {
			return ThreadItem{
				ID:           id,
				Type:         ThreadItemAgentMessage,
				Status:       ThreadItemStatusCompleted,
				Phase:        assistantMessagePhase(msg),
				Role:         "assistant",
				Text:         msg.Content,
				FinishReason: string(msg.FinishReason),
				StopReason:   msg.StopReason,
				Truncated:    msg.Truncated,
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

func isInternalContextToolName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), compact.InceptionToolName)
}

func threadItemTypeForTool(name string) ThreadItemType {
	switch strings.TrimSpace(name) {
	case "spawn_agent", "helpme", "send_message", "followup_task", "await_agents", "close_agent", "list_agents", "agent_report":
		return ThreadItemCollabAgentTool
	default:
		return ThreadItemToolCall
	}
}

func assistantMessagePhase(msg providers.ChatMessage) ThreadItemPhase {
	if phase := threadItemPhaseFromProvider(msg.Phase); phase != "" {
		return phase
	}
	if len(msg.ToolCalls) > 0 {
		return ThreadItemPhaseCommentary
	}
	return ThreadItemPhaseFinalAnswer
}

func threadItemPhaseFromProvider(phase providers.MessagePhase) ThreadItemPhase {
	switch phase {
	case providers.MessagePhaseCommentary:
		return ThreadItemPhaseCommentary
	case providers.MessagePhaseFinalAnswer:
		return ThreadItemPhaseFinalAnswer
	default:
		return ""
	}
}

func threadPreview(history []providers.ChatMessage) string {
	for _, msg := range history {
		if msg.Hidden {
			continue
		}
		if compact.IsInternalContextMessage(msg) {
			continue
		}
		if msg.Role == "user" && !isToolResultMessage(msg) && strings.TrimSpace(chatMessageDisplayContent(msg)) != "" {
			return strings.TrimSpace(chatMessageDisplayContent(msg))
		}
		if msg.Role == "user" && !isToolResultMessage(msg) && len(msg.Images) > 0 {
			if len(msg.Images) == 1 {
				return "[Image #1]"
			}
			return fmt.Sprintf("[%d images]", len(msg.Images))
		}
		if msg.Role == "user" && !isToolResultMessage(msg) && len(msg.Files) > 0 {
			if len(msg.Files) == 1 {
				return filePreview(msg.Files[0], 1)
			}
			return fmt.Sprintf("[%d files]", len(msg.Files))
		}
	}
	return ""
}

func chatMessageDisplayContent(msg providers.ChatMessage) string {
	if strings.TrimSpace(msg.DisplayContent) != "" {
		return msg.DisplayContent
	}
	return msg.Content
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
		out[i].Items = make([]ThreadItem, len(turn.Items))
		for j, item := range turn.Items {
			out[i].Items[j] = cloneThreadItem(item)
		}
	}
	return out
}

func threadItemFiles(files []providers.InputFile) []ThreadItemFile {
	if len(files) == 0 {
		return nil
	}
	out := make([]ThreadItemFile, 0, len(files))
	for _, file := range files {
		data := strings.TrimSpace(file.Data)
		if data == "" {
			continue
		}
		mediaType := strings.TrimSpace(file.MediaType)
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		out = append(out, ThreadItemFile{
			MediaType: mediaType,
			Data:      data,
			Filename:  strings.TrimSpace(file.Filename),
		})
	}
	return out
}

func filePreview(file providers.InputFile, index int) string {
	name := strings.TrimSpace(file.Filename)
	if name != "" {
		return "[" + name + "]"
	}
	mediaType := strings.TrimSpace(file.MediaType)
	if mediaType == "" {
		mediaType = "file"
	}
	return fmt.Sprintf("[File #%d: %s]", index, mediaType)
}

func cloneThreadItem(item ThreadItem) ThreadItem {
	item.Images = append([]ThreadItemImage(nil), item.Images...)
	item.Files = append([]ThreadItemFile(nil), item.Files...)
	item.Display = cloneToolCallDisplay(item.Display)
	return item
}

func cloneToolCallDisplay(display *providers.ToolCallDisplay) *providers.ToolCallDisplay {
	if display == nil {
		return nil
	}
	clone := *display
	return &clone
}

// cloneListeningPorts returns a defensive copy so the snapshot is safe to
// hand off to the renderer without aliasing the live threadState.
func cloneListeningPorts(ports []int) []int {
	if len(ports) == 0 {
		return nil
	}
	out := make([]int, len(ports))
	copy(out, ports)
	return out
}

func cloneThreadBrowserState(state ThreadBrowserState) *ThreadBrowserState {
	if state.CurrentURL == "" && state.PrimaryPreviewURL == "" && state.LinkedProcessID == "" {
		return nil
	}
	clone := state
	return &clone
}

type browserPreviewUpdate struct {
	PortsSet          bool
	Ports             []int
	PreviewURLs       []string
	PrimaryPreviewURL string
	ProcessID         string
}

func (th *threadState) applyBrowserPreviewFromToolResultLocked(name, raw string, now time.Time) bool {
	update, ok := decodeBrowserPreviewResult(name, raw)
	if !ok {
		return false
	}
	changed := false
	if update.PortsSet && !intSliceEqual(th.ListeningPorts, update.Ports) {
		th.ListeningPorts = update.Ports
		changed = true
	}
	if update.PortsSet && len(update.Ports) == 0 {
		if th.BrowserState.PrimaryPreviewURL != "" || th.BrowserState.LinkedProcessID != "" {
			th.BrowserState.PrimaryPreviewURL = ""
			th.BrowserState.LinkedProcessID = ""
			changed = true
		}
	} else if update.PrimaryPreviewURL != "" {
		if th.BrowserState.PrimaryPreviewURL != update.PrimaryPreviewURL {
			th.BrowserState.PrimaryPreviewURL = update.PrimaryPreviewURL
			th.BrowserState.CurrentURL = update.PrimaryPreviewURL
			changed = true
		}
		if update.ProcessID != "" && th.BrowserState.LinkedProcessID != update.ProcessID {
			th.BrowserState.LinkedProcessID = update.ProcessID
			changed = true
		}
	}
	if changed {
		th.UpdatedAt = now
	}
	return changed
}

// decodeListeningPortsResult parses a report_listening_ports tool result
// (a compact JSON object) into a normalized, deduped, ordered port
// list. Returns ok=false if the result is not a recognizable payload.
func decodeListeningPortsResult(raw string) ([]int, bool) {
	update, ok := decodeBrowserPreviewResult("report_listening_ports", raw)
	if !ok || !update.PortsSet {
		return nil, false
	}
	return update.Ports, true
}

func decodeBrowserPreviewResult(name, raw string) (browserPreviewUpdate, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return browserPreviewUpdate{}, false
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return browserPreviewUpdate{}, false
	}
	update := browserPreviewUpdate{}
	if rawPorts, ok := payload["ports"]; ok {
		var ports []int
		if err := json.Unmarshal(rawPorts, &ports); err == nil {
			update.PortsSet = true
			update.Ports = normalizeListeningPortsForServer(ports)
		}
	}
	if rawURLs, ok := payload["preview_urls"]; ok {
		var urls []string
		if err := json.Unmarshal(rawURLs, &urls); err == nil {
			update.PreviewURLs = proc.NormalizePreviewURLs(urls)
		}
	}
	if rawPrimary, ok := payload["primary_preview_url"]; ok {
		var primary string
		if err := json.Unmarshal(rawPrimary, &primary); err == nil {
			update.PrimaryPreviewURL = proc.NormalizePreviewURL(primary)
		}
	}
	if rawProcessID, ok := payload["process_id"]; ok {
		var processID string
		if err := json.Unmarshal(rawProcessID, &processID); err == nil {
			update.ProcessID = strings.TrimSpace(processID)
		}
	}
	if rawProcess, ok := payload["process"]; ok {
		applyProcessPreviewPayload(rawProcess, &update)
	} else {
		applyProcessPreviewPayload([]byte(trimmed), &update)
	}
	if len(update.PreviewURLs) == 0 && len(update.Ports) > 0 {
		update.PreviewURLs = proc.PreviewURLsFromPorts(update.Ports)
	}
	if update.PrimaryPreviewURL == "" && len(update.PreviewURLs) > 0 {
		update.PrimaryPreviewURL = update.PreviewURLs[0]
	}
	if name == "report_listening_ports" && update.PortsSet {
		return update, true
	}
	if update.PrimaryPreviewURL != "" {
		return update, true
	}
	return browserPreviewUpdate{}, false
}

func applyProcessPreviewPayload(raw json.RawMessage, update *browserPreviewUpdate) {
	var process struct {
		ID                string   `json:"id"`
		PreviewURLs       []string `json:"preview_urls"`
		PrimaryPreviewURL string   `json:"primary_preview_url"`
	}
	if err := json.Unmarshal(raw, &process); err != nil {
		return
	}
	if update.ProcessID == "" {
		update.ProcessID = strings.TrimSpace(process.ID)
	}
	if len(update.PreviewURLs) == 0 {
		update.PreviewURLs = proc.NormalizePreviewURLs(process.PreviewURLs)
	}
	if update.PrimaryPreviewURL == "" {
		update.PrimaryPreviewURL = proc.NormalizePreviewURL(process.PrimaryPreviewURL)
	}
}

// normalizeListeningPortsForServer mirrors the tool's own normalization:
// drop out-of-range and dedupe while preserving order. Returning nil for an
// empty input keeps the thread-level field unset so the JSON omits it.
func normalizeListeningPortsForServer(in []int) []int {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, p := range in {
		if p < 1 || p > 65535 {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
