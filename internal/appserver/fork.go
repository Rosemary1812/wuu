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
		if msg.Hidden {
			continue
		}
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

func editHistoryBeforeUserMessage(history []providers.ChatMessage, sourceThreadID string, turns []Turn, targetTurnID, targetItemID string) ([]providers.ChatMessage, ThreadEditDraft, error) {
	targetTurnID = strings.TrimSpace(targetTurnID)
	targetItemID = strings.TrimSpace(targetItemID)
	if targetTurnID == "" {
		return nil, ThreadEditDraft{}, fmt.Errorf("turn_id is required")
	}
	if targetItemID == "" {
		return nil, ThreadEditDraft{}, fmt.Errorf("item_id is required")
	}

	var currentTurnID string
	turnIndex := 0
	currentTurnIndex := -1
	itemIndex := 0

	nextItemID := func() string {
		itemIndex++
		if currentTurnIndex >= 0 && currentTurnIndex < len(turns) && itemIndex-1 < len(turns[currentTurnIndex].Items) {
			return turns[currentTurnIndex].Items[itemIndex-1].ID
		}
		return fmt.Sprintf("%s-item-%d", currentTurnID, itemIndex)
	}
	turnMatches := func() bool {
		return currentTurnID != "" && currentTurnID == targetTurnID
	}
	itemMatches := func(itemID string) bool {
		return turnMatches() && itemID == targetItemID
	}

	for i, msg := range history {
		if msg.Hidden {
			continue
		}
		if msg.Role == "system" {
			continue
		}
		if msg.Role != "user" || isToolResultMessage(msg) {
			continue
		}
		if msg.Steered {
			if currentTurnID != "" && itemMatches(nextItemID()) {
				return nil, ThreadEditDraft{}, fmt.Errorf("only regular user messages can be edited")
			}
			continue
		}

		currentTurnIndex = turnIndex
		turnIndex++
		if currentTurnIndex < len(turns) && strings.TrimSpace(turns[currentTurnIndex].ID) != "" {
			currentTurnID = turns[currentTurnIndex].ID
		} else {
			currentTurnID = fmt.Sprintf("%s-turn-%04d", sourceThreadID, turnIndex)
		}
		itemIndex = 0
		if itemMatches(nextItemID()) {
			if compactedAttachmentOmission(msg) {
				return nil, ThreadEditDraft{}, fmt.Errorf("this message contains compacted attachment placeholders and cannot be restored for editing")
			}
			return cloneHistory(history[:i]), editDraftFromChatMessage(msg), nil
		}
	}

	return nil, ThreadEditDraft{}, fmt.Errorf("editable user message not found")
}

func editDraftFromChatMessage(msg providers.ChatMessage) ThreadEditDraft {
	draft := ThreadEditDraft{
		Prompt: chatMessageDisplayContent(msg),
		Images: make([]TurnStartImage, 0, len(msg.Images)),
		Files:  make([]TurnStartFile, 0, len(msg.Files)),
	}
	for _, image := range msg.Images {
		data := strings.TrimSpace(image.Data)
		if data == "" {
			continue
		}
		draft.Images = append(draft.Images, TurnStartImage{
			MediaType: strings.TrimSpace(image.MediaType),
			Data:      data,
		})
	}
	for _, file := range msg.Files {
		data := strings.TrimSpace(file.Data)
		if data == "" {
			continue
		}
		draft.Files = append(draft.Files, TurnStartFile{
			MediaType: strings.TrimSpace(file.MediaType),
			Data:      data,
			Filename:  strings.TrimSpace(file.Filename),
		})
	}
	return draft
}

func compactedAttachmentOmission(msg providers.ChatMessage) bool {
	if len(msg.Images) > 0 || len(msg.Files) > 0 {
		return false
	}
	content := strings.ToLower(msg.Content)
	return strings.Contains(content, "attachment omitted from compacted history")
}

func cloneHistory(history []providers.ChatMessage) []providers.ChatMessage {
	return providers.CloneChatMessages(history)
}
