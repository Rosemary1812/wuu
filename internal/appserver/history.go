package appserver

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/blueberrycongee/wuu/internal/jsonl"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type persistedToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type persistedImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type persistedMessage struct {
	Role             string                     `json:"role"`
	Content          string                     `json:"content"`
	ReasoningContent string                     `json:"reasoning_content,omitempty"`
	ReasoningBlocks  []providers.ReasoningBlock `json:"reasoning_blocks,omitempty"`
	Images           []persistedImage           `json:"images,omitempty"`
	ToolCalls        []persistedToolCall        `json:"tool_calls,omitempty"`
	ToolCallID       string                     `json:"tool_call_id,omitempty"`
	Name             string                     `json:"name,omitempty"`
}

func loadChatMessages(path string) ([]providers.ChatMessage, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open session history: %w", err)
	}
	defer file.Close()

	var messages []providers.ChatMessage
	line := 0
	err = jsonl.ForEachLine(file, func(raw []byte) error {
		line++
		if len(raw) == 0 {
			return nil
		}
		var rec persistedMessage
		if err := json.Unmarshal(raw, &rec); err != nil {
			return fmt.Errorf("parse session line %d: %w", line, err)
		}
		role := strings.ToLower(strings.TrimSpace(rec.Role))
		if role == "" || role == "meta" {
			return nil
		}
		msg := providers.ChatMessage{
			Role:             role,
			Name:             rec.Name,
			Content:          rec.Content,
			ReasoningContent: rec.ReasoningContent,
			ReasoningBlocks:  append([]providers.ReasoningBlock(nil), rec.ReasoningBlocks...),
			ToolCallID:       rec.ToolCallID,
		}
		for _, image := range rec.Images {
			if strings.TrimSpace(image.Data) == "" {
				continue
			}
			msg.Images = append(msg.Images, providers.InputImage{
				MediaType: image.MediaType,
				Data:      image.Data,
			})
		}
		for _, tc := range rec.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, providers.ToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			})
		}
		messages = append(messages, msg)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan session history: %w", err)
	}
	return messages, nil
}
