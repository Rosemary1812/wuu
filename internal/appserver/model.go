package appserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/participant"
	proc "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type outboundNotification struct {
	method string
	params any
}

const (
	manualContextCompactionReason         = "manual"
	manualContextCompactionInProgressText = "Manual context compaction in progress."
	manualContextCompactionFailedText     = "Manual context compaction failed; history is unchanged."
)

func newThreadState(id string, history []providers.ChatMessage, rtProvider, model, cwd string, persistHistory bool, now time.Time) *threadState {
	return &threadState{
		ID:             id,
		History:        cloneHistory(history),
		CreatedAt:      now,
		UpdatedAt:      now,
		LastAccessedAt: now,
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
		BrowserState:     cloneThreadBrowserState(th.BrowserState),
		DMParticipantID:  th.DMParticipantID,
		Group:            th.Group,
		FocusWorkspace:   th.FocusWorkspace,
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
	th.currentTurnKind = TurnKindUser
	th.running = true
	th.runningProviderName = th.ModelProvider
	th.runningModel = th.Model
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

func (th *threadState) appendUserMessageTurnLocked(turnID string, userMsg providers.ChatMessage, now time.Time) Turn {
	th.currentTurn = turnID
	th.UpdatedAt = now
	th.nextItemIndex = 0
	th.activeAgentItemID = ""
	th.activeReasoningItemID = ""
	th.toolItems = make(map[string]string)

	userItem := chatMessageItem(th.nextItemIDLocked(turnID), userMsg)
	turn := Turn{
		ID:          turnID,
		Items:       []ThreadItem{userItem},
		ItemsView:   TurnItemsViewFull,
		Status:      TurnStatusCompleted,
		StartedAt:   &now,
		CompletedAt: &now,
	}
	th.Turns = append(th.Turns, turn)
	return turn
}

func (th *threadState) startInternalTurnLocked(turnID string, now time.Time) Turn {
	return th.startInternalTurnWithKindLocked(turnID, TurnKindInternal, now)
}

func (th *threadState) startCompactTurnLocked(turnID string, displayMsg providers.ChatMessage, now time.Time) Turn {
	turn := th.startInternalTurnWithKindLocked(turnID, TurnKindCompact, now)
	if strings.EqualFold(displayMsg.Role, "user") && strings.TrimSpace(chatMessageDisplayContent(displayMsg)) != "" {
		item := chatMessageItem(th.nextItemIDLocked(turnID), displayMsg)
		turn.Items = append(turn.Items, item)
	}
	turn.Items = append(turn.Items, ThreadItem{
		ID:     th.nextItemIDLocked(turnID),
		Type:   ThreadItemContextCompaction,
		Status: ThreadItemStatusInProgress,
		Text:   manualContextCompactionInProgressText,
		Reason: manualContextCompactionReason,
	})
	th.replaceTurnLocked(turn)
	return turn
}

func (th *threadState) startInternalTurnWithKindLocked(turnID string, kind TurnKind, now time.Time) Turn {
	th.currentTurn = turnID
	th.currentTurnKind = kind
	th.running = true
	th.runningProviderName = th.ModelProvider
	th.runningModel = th.Model
	th.UpdatedAt = now
	th.pendingSteers = nil
	th.nextItemIndex = 0
	th.activeAgentItemID = ""
	th.activeReasoningItemID = ""
	th.toolItems = make(map[string]string)

	turn := Turn{
		ID:        turnID,
		Kind:      kind,
		ItemsView: TurnItemsViewFull,
		Status:    TurnStatusInProgress,
		StartedAt: &now,
	}
	th.Turns = append(th.Turns, turn)
	return turn
}

func (th *threadState) startAgentTurnLocked(now time.Time) (Turn, bool) {
	if th.running && th.currentTurn != "" {
		if strings.TrimSpace(th.runningProviderName) == "" {
			th.runningProviderName = th.ModelProvider
		}
		if strings.TrimSpace(th.runningModel) == "" {
			th.runningModel = th.Model
		}
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
		th.currentTurnKind = TurnKindInternal
		th.running = true
		th.runningProviderName = th.ModelProvider
		th.runningModel = th.Model
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
	th.currentTurnKind = TurnKindInternal
	th.running = true
	th.runningProviderName = th.ModelProvider
	th.runningModel = th.Model
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
	th.currentTurnKind = ""
	th.runningProviderName = ""
	th.runningModel = ""
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
	if turn.Kind == TurnKindCompact && status == TurnStatusFailed {
		th.finishPendingContextCompactionLocked(turnID, ThreadItemStatusFailed, manualContextCompactionFailedText, now)
		turn = th.ensureTurnLocked(turnID, now)
	}
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

func applyTokenUsageToTurn(turn *Turn, usage providers.TokenUsage, contextTokens int, model string) {
	if turn == nil {
		return
	}
	turn.InputTokens = usage.InputTokens
	turn.OutputTokens = usage.OutputTokens
	turn.ContextTokens = contextTokens
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
		}, meta.ContextTokens, meta.Model)
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
		if th.applyBrowserPreviewFromToolResultLocked(ev.ToolResult, now) {
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
		// A compaction pass just folded this thread's older history into a
		// summary that may not have preserved the workspace-focus
		// declaration item (2026-07-03-workspace-focus.md §7). Mark the
		// declaration stale so the next applyTurnWorkspaceFocus call
		// re-declares the focus even if the requested value still matches
		// the stored one — otherwise the idempotent no-op path would leave
		// the agent with no focus reminder anywhere in its live context.
		th.focusDeclarationStale = true
		item, replacesPending := th.pendingContextCompactionItemLocked(turnID, now)
		if !replacesPending {
			item.ID = th.nextItemIDLocked(turnID)
			item.Type = ThreadItemContextCompaction
		}
		item.Status = contextCompactionStatusForContent(ev.Content)
		item.Text = ev.Content
		item.Reason = ev.CompactReason
		th.upsertItemLocked(turnID, item, now)
		if replacesPending {
			out = append(out, itemCompleted(th.ID, turnID, item, now))
		} else {
			out = append(out, itemStarted(th.ID, turnID, item, now), itemCompleted(th.ID, turnID, item, now))
		}
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

func (th *threadState) pendingContextCompactionItemLocked(turnID string, now time.Time) (ThreadItem, bool) {
	turn := th.ensureTurnLocked(turnID, now)
	for i := len(turn.Items) - 1; i >= 0; i-- {
		item := turn.Items[i]
		if item.Type == ThreadItemContextCompaction && item.Status == ThreadItemStatusInProgress {
			return item, true
		}
	}
	return ThreadItem{}, false
}

func (th *threadState) finishPendingContextCompactionLocked(turnID string, status ThreadItemStatus, text string, now time.Time) {
	item, ok := th.pendingContextCompactionItemLocked(turnID, now)
	if !ok {
		item = ThreadItem{
			ID:   th.nextItemIDLocked(turnID),
			Type: ThreadItemContextCompaction,
		}
	}
	item.Status = status
	item.Text = text
	if strings.TrimSpace(item.Reason) == "" {
		item.Reason = manualContextCompactionReason
	}
	th.upsertItemLocked(turnID, item, now)
}

func contextCompactionStatusForContent(content string) ThreadItemStatus {
	if isCompactFailureNoticeContent(content) {
		return ThreadItemStatusFailed
	}
	return ThreadItemStatusCompleted
}

func isCompactFailureNoticeContent(content string) bool {
	normalized := strings.TrimSpace(strings.TrimLeft(content, "✦*• \t"))
	lower := strings.ToLower(normalized)
	return strings.HasPrefix(lower, "manual context compaction failed") ||
		strings.HasPrefix(lower, "context compaction failed") ||
		strings.HasPrefix(lower, "proactive compact failed") ||
		strings.HasPrefix(lower, "context-overflow compact failed") ||
		strings.HasPrefix(lower, "compact failed")
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
		} else if item.Status == ThreadItemStatusCompleted {
			// EventToolUseEnd already finalized this item with a
			// truncated copy of the same result. Upgrade to the
			// full-fidelity message content, but do not append or
			// announce a second completion.
			if len(msg.Content) > len(item.Result) {
				item.Result = msg.Content
				th.upsertItemLocked(turnID, item, now)
			}
			return nil
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
			if id != "" {
				item.SourceID = id
			}
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
		SourceID:  id,
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

func (th *threadState) appendParticipantMessageLocked(rec persistedMessage, now time.Time, resolve participantSummaryResolver) (Turn, ThreadItem, bool) {
	turnID := strings.TrimSpace(th.currentTurn)
	createdTurn := false
	if turnID == "" {
		if len(th.Turns) > 0 {
			turnID = th.Turns[len(th.Turns)-1].ID
		} else {
			turnID = fmt.Sprintf("%s-turn-%04d", th.ID, 1)
			createdTurn = true
		}
	}
	turn := th.ensureTurnLocked(turnID, now)
	th.nextItemIndex = max(th.nextItemIndex, maxTurnItemIndex(turn))
	item := participantMessageItem(th.nextItemIDLocked(turnID), rec, resolve)
	if msg, ok := participantModelContextMessage(rec); ok {
		th.History = append(th.History, msg)
	}
	turn.Items = append(turn.Items, item)
	if createdTurn && !th.running {
		turn.Status = TurnStatusCompleted
	}
	th.replaceTurnLocked(turn)
	th.UpdatedAt = now
	return turn, item, createdTurn
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
	persisted := make([]persistedMessage, 0, len(history))
	for _, msg := range history {
		persisted = append(persisted, persistedMessageFromChatMessage(msg))
	}
	return turnsFromPersistedHistory(threadID, persisted, now, nil)
}

type participantSummaryResolver func(string) (participant.Summary, bool)

func turnsFromPersistedHistory(threadID string, history []persistedMessage, now time.Time, resolve participantSummaryResolver) []Turn {
	return turnsFromPersistedHistoryInScope(threadID, "", history, now, resolve)
}

func turnsFromConversationSubthreadHistory(parentThreadID, subthreadID string, history []persistedMessage, now time.Time, resolve participantSummaryResolver) []Turn {
	scopeID := strings.TrimSpace(parentThreadID)
	if id := strings.TrimSpace(subthreadID); id != "" {
		scopeID = scopeID + "-" + id
	}
	return turnsFromPersistedHistoryInScope(scopeID, subthreadID, history, now, resolve)
}

func turnsFromPersistedHistoryInScope(threadID, subthreadID string, history []persistedMessage, now time.Time, resolve participantSummaryResolver) []Turn {
	var turns []Turn
	var current *Turn
	itemIndex := 0
	toolItems := make(map[string]int)
	var pendingCompactions []ThreadItem
	subthreadID = strings.TrimSpace(subthreadID)
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
	startSyntheticTurn := func() {
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
	for _, rec := range history {
		if strings.TrimSpace(rec.ThreadID) != subthreadID {
			continue
		}
		if rec.Hidden {
			continue
		}
		if isParticipantPersistedMessage(rec) {
			if current == nil {
				startSyntheticTurn()
			}
			appendItem(participantMessageItem(nextItemID(current.ID), rec, resolve))
			continue
		}
		msg := chatMessageFromPersistedMessage(rec)
		if compact.IsInternalContextMessage(msg) {
			continue
		}
		if msg.Role == "system" {
			if compact.IsConversationSummaryContent(msg.Content) {
				pendingCompactions = append(pendingCompactions, ThreadItem{
					Type:   ThreadItemContextCompaction,
					Status: ThreadItemStatusCompleted,
					Text:   "Compacted history",
				})
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
		if current == nil && msg.Role == "assistant" && pendingCompactionsContainReason(pendingCompactions, compact.HelpMeToolName) {
			startSyntheticTurn()
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
					SourceID:  call.ID,
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

func pendingCompactionsContainReason(items []ThreadItem, reason string) bool {
	for _, item := range items {
		if item.Reason == reason {
			return true
		}
	}
	return false
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
			ID:           id,
			Seq:          msg.Seq,
			SourceID:     msg.ClientID,
			Type:         ThreadItemUserMessage,
			Status:       ThreadItemStatusCompleted,
			Role:         "user",
			Text:         chatMessageDisplayContent(msg),
			Images:       threadItemImages(msg.Images),
			Files:        threadItemFiles(msg.Files),
			EnvelopeMeta: append(json.RawMessage(nil), msg.EnvelopeMeta...),
			FocusMeta:    append(json.RawMessage(nil), msg.FocusMeta...),
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
			ID:       id,
			SourceID: msg.ToolCallID,
			Type:     ThreadItemToolCall,
			Status:   ThreadItemStatusCompleted,
			Result:   msg.Content,
		}
	}
	return ThreadItem{}
}

func chatMessageFromPersistedMessage(rec persistedMessage) providers.ChatMessage {
	msg := providers.ChatMessage{
		Seq:               rec.Seq,
		Role:              strings.ToLower(strings.TrimSpace(rec.Role)),
		Name:              rec.Name,
		ClientID:          rec.ClientID,
		Content:           rec.Content,
		DisplayContent:    rec.DisplayContent,
		Phase:             providers.NormalizeMessagePhase(rec.Phase),
		Hidden:            rec.Hidden,
		ProviderItemID:    rec.ProviderItemID,
		ProviderItemModel: rec.ProviderItemModel,
		Steered:           rec.Steered,
		ReasoningContent:  rec.ReasoningContent,
		ReasoningBlocks:   append([]providers.ReasoningBlock(nil), rec.ReasoningBlocks...),
		ToolCallID:        rec.ToolCallID,
		ToolResultKind:    providers.NormalizeToolCallKind(rec.ToolResultKind),
		FinishReason:      providers.FinishReason(strings.TrimSpace(rec.FinishReason)),
		StopReason:        strings.ToLower(strings.TrimSpace(rec.StopReason)),
		Truncated:         rec.Truncated,
		DiscoveredTools:   providers.CloneLoadableToolDefinitions(rec.DiscoveredTools),
		EnvelopeMeta:      append(json.RawMessage(nil), rec.EnvelopeMeta...),
		FocusMeta:         append(json.RawMessage(nil), rec.FocusMeta...),
	}
	for _, image := range rec.Images {
		if strings.TrimSpace(image.Data) == "" {
			continue
		}
		msg.Images = append(msg.Images, providers.InputImage{
			MediaType: image.MediaType,
			Data:      image.Data,
			Width:     image.Width,
			Height:    image.Height,
		})
	}
	for _, file := range rec.Files {
		if strings.TrimSpace(file.Data) == "" {
			continue
		}
		msg.Files = append(msg.Files, providers.InputFile{
			MediaType: file.MediaType,
			Data:      file.Data,
			Filename:  file.Filename,
		})
	}
	for _, tc := range rec.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, providers.ToolCall{
			ID:                tc.ID,
			ProviderItemID:    tc.ProviderItemID,
			ProviderItemModel: tc.ProviderItemModel,
			Name:              tc.Name,
			Arguments:         tc.Arguments,
			Kind:              providers.NormalizeToolCallKind(tc.Kind),
			Display:           cloneToolCallDisplay(tc.Display),
		})
	}
	return msg
}

func isParticipantPersistedMessage(rec persistedMessage) bool {
	role := strings.ToLower(strings.TrimSpace(rec.Role))
	return role == "participant" || (strings.TrimSpace(rec.ParticipantID) != "" && strings.TrimSpace(rec.PostKind) != "")
}

func participantMessageItem(id string, rec persistedMessage, resolve participantSummaryResolver) ThreadItem {
	text := rec.DisplayContent
	if strings.TrimSpace(text) == "" {
		text = rec.Content
	}
	postKind := strings.ToLower(strings.TrimSpace(rec.PostKind))
	if postKind == "" {
		postKind = "result"
	}
	item := ThreadItem{
		ID:       id,
		Seq:      rec.Seq,
		SourceID: rec.ClientID,
		AgentID:  participantMessageAgentID(rec),
		Type:     ThreadItemParticipantMsg,
		Status:   ThreadItemStatusCompleted,
		Role:     "participant",
		Text:     text,
		PostKind: postKind,
	}
	if id := strings.TrimSpace(rec.ParticipantID); id != "" {
		if resolve != nil {
			if summary, ok := resolve(id); ok {
				item.Participant = &summary
				return item
			}
		}
		summary := participant.Summary{
			ID:   id,
			Name: firstNonEmpty(strings.TrimSpace(rec.Name), "Participant"),
			Kind: string(participant.KindEphemeral),
		}
		item.Participant = &summary
	}
	return item
}

func participantMessageAgentID(rec persistedMessage) string {
	id := strings.TrimSpace(rec.ClientID)
	if id == "" {
		return ""
	}
	if dash := strings.LastIndex(id, "-"); dash >= 0 {
		suffix := id[dash+1:]
		if len(suffix) >= 12 && allASCIIDigits(suffix) {
			return ""
		}
	}
	return id
}

func allASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isToolResultMessage(msg providers.ChatMessage) bool {
	return msg.Role == "tool" || msg.ToolCallID != ""
}

func isInternalContextToolName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), compact.InceptionToolName)
}

func threadItemTypeForTool(name string) ThreadItemType {
	switch strings.TrimSpace(name) {
	case "spawn_agent", "helpme", "send_message", "close_agent", "agent_report":
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

func cloneThreadBrowserState(state ThreadBrowserState) *ThreadBrowserState {
	if state.CurrentURL == "" && state.PrimaryPreviewURL == "" && state.LinkedProcessID == "" {
		return nil
	}
	clone := state
	return &clone
}

type browserPreviewUpdate struct {
	PreviewURLs       []string
	PrimaryPreviewURL string
	ProcessID         string
}

func (th *threadState) applyBrowserPreviewFromToolResultLocked(raw string, now time.Time) bool {
	update, ok := decodeBrowserPreviewResult(raw)
	if !ok {
		return false
	}
	changed := false
	if update.PrimaryPreviewURL != "" {
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

func decodeBrowserPreviewResult(raw string) (browserPreviewUpdate, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return browserPreviewUpdate{}, false
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return browserPreviewUpdate{}, false
	}
	update := browserPreviewUpdate{}
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
	if update.PrimaryPreviewURL == "" && len(update.PreviewURLs) > 0 {
		update.PrimaryPreviewURL = update.PreviewURLs[0]
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
