package appserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/jsonl"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type persistedToolCall struct {
	ID        string                     `json:"id"`
	Name      string                     `json:"name"`
	Arguments string                     `json:"arguments"`
	Display   *providers.ToolCallDisplay `json:"display,omitempty"`
}

type persistedImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type persistedMessage struct {
	Role             string                     `json:"role"`
	Content          string                     `json:"content"`
	ClientID         string                     `json:"client_id,omitempty"`
	Steered          bool                       `json:"steered,omitempty"`
	ReasoningContent string                     `json:"reasoning_content,omitempty"`
	ReasoningBlocks  []providers.ReasoningBlock `json:"reasoning_blocks,omitempty"`
	Images           []persistedImage           `json:"images,omitempty"`
	ToolCalls        []persistedToolCall        `json:"tool_calls,omitempty"`
	ToolCallID       string                     `json:"tool_call_id,omitempty"`
	Name             string                     `json:"name,omitempty"`
	At               time.Time                  `json:"at,omitempty"`
	InputTokens      int                        `json:"input_tokens,omitempty"`
	OutputTokens     int                        `json:"output_tokens,omitempty"`
}

type persistedAgentHistory struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	TaskName     string                  `json:"task_name,omitempty"`
	AgentProfile string                  `json:"agent_profile,omitempty"`
	AgentPath    string                  `json:"agent_path,omitempty"`
	ParentID     string                  `json:"parent_id,omitempty"`
	Description  string                  `json:"description"`
	Status       string                  `json:"status"`
	StartedAt    time.Time               `json:"started_at"`
	CompletedAt  time.Time               `json:"completed_at"`
	Model        string                  `json:"model"`
	Prompt       string                  `json:"prompt"`
	Result       string                  `json:"result,omitempty"`
	Error        string                  `json:"error,omitempty"`
	Messages     []providers.ChatMessage `json:"messages,omitempty"`
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
			ClientID:         rec.ClientID,
			Content:          rec.Content,
			Steered:          rec.Steered,
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
				Display:   cloneToolCallDisplay(tc.Display),
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

func loadAgentHistory(path string) (persistedAgentHistory, error) {
	if strings.TrimSpace(path) == "" {
		return persistedAgentHistory{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return persistedAgentHistory{}, fmt.Errorf("read worker history: %w", err)
	}
	var rec persistedAgentHistory
	if err := json.Unmarshal(data, &rec); err != nil {
		return persistedAgentHistory{}, fmt.Errorf("decode worker history: %w", err)
	}
	return rec, nil
}

func appendChatMessage(path string, msg providers.ChatMessage) error {
	if strings.TrimSpace(path) == "" || !shouldPersistMessage(msg) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create session directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open session history for append: %w", err)
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(persistedMessageFromChatMessage(msg)); err != nil {
		return fmt.Errorf("write session message: %w", err)
	}
	return nil
}

func rewriteChatHistory(path string, msgs []providers.ChatMessage) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	metas, err := loadMetaMessages(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("load session metadata: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create session directory: %w", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("rewrite session history: %w", err)
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	for _, msg := range msgs {
		if !shouldPersistMessage(msg) {
			continue
		}
		if err := enc.Encode(persistedMessageFromChatMessage(msg)); err != nil {
			return fmt.Errorf("write session history: %w", err)
		}
	}
	for _, rec := range metas {
		if err := enc.Encode(rec); err != nil {
			return fmt.Errorf("write session metadata: %w", err)
		}
	}
	return nil
}

func appendTokenUsage(path string, inputTokens, outputTokens int) error {
	if strings.TrimSpace(path) == "" || (inputTokens == 0 && outputTokens == 0) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create session directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open session history for append: %w", err)
	}
	defer file.Close()
	rec := persistedMessage{
		Role:         "meta",
		Content:      "token_usage",
		At:           time.Now().UTC(),
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}
	if err := json.NewEncoder(file).Encode(rec); err != nil {
		return fmt.Errorf("write token usage: %w", err)
	}
	return nil
}

func persistedMessageFromChatMessage(msg providers.ChatMessage) persistedMessage {
	out := persistedMessage{
		Role:             strings.ToLower(msg.Role),
		Content:          msg.Content,
		ClientID:         msg.ClientID,
		Steered:          msg.Steered,
		ReasoningContent: msg.ReasoningContent,
		ReasoningBlocks:  append([]providers.ReasoningBlock(nil), msg.ReasoningBlocks...),
		ToolCallID:       msg.ToolCallID,
		Name:             msg.Name,
		At:               time.Now().UTC(),
	}
	for _, image := range msg.Images {
		data := strings.TrimSpace(image.Data)
		if data == "" {
			continue
		}
		out.Images = append(out.Images, persistedImage{
			MediaType: image.MediaType,
			Data:      data,
		})
	}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, persistedToolCall{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
			Display:   cloneToolCallDisplay(tc.Display),
		})
	}
	return out
}

func loadMetaMessages(path string) ([]persistedMessage, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var metas []persistedMessage
	err = jsonl.ForEachLine(file, func(raw []byte) error {
		payload := bytes.TrimSpace(raw)
		if len(payload) == 0 {
			return nil
		}
		var rec persistedMessage
		if err := json.Unmarshal(payload, &rec); err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(rec.Role), "meta") {
			metas = append(metas, rec)
		}
		return nil
	})
	return metas, err
}

func shouldPersistMessage(msg providers.ChatMessage) bool {
	role := strings.ToLower(strings.TrimSpace(msg.Role))
	switch role {
	case "user", "assistant", "tool":
		return true
	case "system":
		return strings.HasPrefix(strings.TrimSpace(msg.Content), compact.ConversationSummaryPrefix)
	default:
		return false
	}
}

func persistableMessageCount(msgs []providers.ChatMessage) int {
	var count int
	for _, msg := range msgs {
		if shouldPersistMessage(msg) {
			count++
		}
	}
	return count
}

func ensureBaseSystemPrompt(history []providers.ChatMessage, prompt string) []providers.ChatMessage {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return history
	}
	if len(history) > 0 && strings.EqualFold(history[0].Role, "system") && history[0].Content == prompt {
		return history
	}
	out := make([]providers.ChatMessage, 0, len(history)+1)
	out = append(out, providers.ChatMessage{Role: "system", Content: prompt})
	out = append(out, history...)
	return out
}
