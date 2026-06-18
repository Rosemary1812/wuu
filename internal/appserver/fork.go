package appserver

import (
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func forkHistoryAtTarget(history []providers.ChatMessage, sourceThreadID string, turns []Turn, targetTurnID, targetItemID string) ([]providers.ChatMessage, error) {
	targetTurnID = strings.TrimSpace(targetTurnID)
	targetItemID = strings.TrimSpace(targetItemID)
	if targetTurnID == "" && targetItemID == "" {
		return cloneHistory(history), nil
	}

	var currentTurnID string
	turnIndex := 0
	currentTurnIndex := -1
	itemIndex := 0
	toolItems := make(map[string]string)
	targetTurnSeen := false
	targetTurnLastIndex := -1
	matchedToolCallID := ""
	matchedToolCutoffIndex := -1

	nextItemID := func() string {
		itemIndex++
		if currentTurnIndex >= 0 && currentTurnIndex < len(turns) && itemIndex-1 < len(turns[currentTurnIndex].Items) {
			return turns[currentTurnIndex].Items[itemIndex-1].ID
		}
		return fmt.Sprintf("%s-item-%d", currentTurnID, itemIndex)
	}
	turnMatches := func() bool {
		return currentTurnID != "" && (targetTurnID == "" || currentTurnID == targetTurnID)
	}
	itemMatches := func(itemID string) bool {
		return targetItemID != "" && turnMatches() && itemID == targetItemID
	}
	markTargetTurnMessage := func(index int) {
		if turnMatches() {
			targetTurnSeen = true
			targetTurnLastIndex = index
		}
	}
	returnPrefix := func(index int) []providers.ChatMessage {
		if index < 0 {
			return nil
		}
		if index >= len(history) {
			index = len(history) - 1
		}
		return cloneHistory(history[:index+1])
	}

	for i, msg := range history {
		if matchedToolCallID != "" && !(msg.Role == "tool" && msg.ToolCallID == matchedToolCallID) {
			return returnPrefix(matchedToolCutoffIndex), nil
		}
		if msg.Role == "system" {
			continue
		}
		if msg.Role == "user" && !isToolResultMessage(msg) {
			if targetTurnSeen && targetItemID == "" {
				return returnPrefix(targetTurnLastIndex), nil
			}
			currentTurnIndex = turnIndex
			turnIndex++
			if currentTurnIndex < len(turns) && strings.TrimSpace(turns[currentTurnIndex].ID) != "" {
				currentTurnID = turns[currentTurnIndex].ID
			} else {
				currentTurnID = fmt.Sprintf("%s-turn-%04d", sourceThreadID, turnIndex)
			}
			itemIndex = 0
			toolItems = make(map[string]string)
			markTargetTurnMessage(i)
			if itemMatches(nextItemID()) {
				return returnPrefix(i), nil
			}
			continue
		}
		if currentTurnID == "" {
			continue
		}

		markTargetTurnMessage(i)
		switch msg.Role {
		case "assistant":
			if strings.TrimSpace(msg.ReasoningContent) != "" && itemMatches(nextItemID()) {
				return returnPrefix(i), nil
			}
			if strings.TrimSpace(msg.Content) != "" && itemMatches(nextItemID()) {
				return returnPrefix(i), nil
			}
			for _, call := range msg.ToolCalls {
				itemID := nextItemID()
				if strings.TrimSpace(call.ID) != "" {
					toolItems[call.ID] = itemID
				}
				if itemMatches(itemID) {
					if strings.TrimSpace(call.ID) == "" {
						return returnPrefix(i), nil
					}
					matchedToolCallID = call.ID
					matchedToolCutoffIndex = i
				}
			}
		case "tool":
			itemID := toolItems[msg.ToolCallID]
			if itemID == "" {
				itemID = nextItemID()
				if strings.TrimSpace(msg.ToolCallID) != "" {
					toolItems[msg.ToolCallID] = itemID
				}
			}
			if matchedToolCallID != "" && msg.ToolCallID == matchedToolCallID {
				matchedToolCutoffIndex = i
			}
			if itemMatches(itemID) {
				return returnPrefix(i), nil
			}
		default:
			if itemMatches(nextItemID()) {
				return returnPrefix(i), nil
			}
		}
	}

	if matchedToolCallID != "" {
		return returnPrefix(matchedToolCutoffIndex), nil
	}
	if targetTurnSeen && targetItemID == "" {
		return returnPrefix(targetTurnLastIndex), nil
	}
	return nil, fmt.Errorf("fork target not found")
}

func cloneHistory(history []providers.ChatMessage) []providers.ChatMessage {
	return providers.CloneChatMessages(history)
}
