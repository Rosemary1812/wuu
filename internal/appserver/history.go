package appserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/jsonl"
	"github.com/blueberrycongee/wuu/internal/providers"
	sessionstore "github.com/blueberrycongee/wuu/internal/session"
)

type persistedToolCall struct {
	ID                string                     `json:"id"`
	ProviderItemID    string                     `json:"provider_item_id,omitempty"`
	ProviderItemModel string                     `json:"provider_item_model,omitempty"`
	Name              string                     `json:"name"`
	Arguments         string                     `json:"arguments"`
	Kind              string                     `json:"kind,omitempty"`
	Display           *providers.ToolCallDisplay `json:"display,omitempty"`
}

type persistedImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type persistedFile struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	Filename  string `json:"filename,omitempty"`
}

type persistedMessage struct {
	Role                string                             `json:"role"`
	Content             string                             `json:"content"`
	DisplayContent      string                             `json:"display_content,omitempty"`
	Phase               string                             `json:"phase,omitempty"`
	ProviderItemID      string                             `json:"provider_item_id,omitempty"`
	ProviderItemModel   string                             `json:"provider_item_model,omitempty"`
	ClientID            string                             `json:"client_id,omitempty"`
	Steered             bool                               `json:"steered,omitempty"`
	ReasoningContent    string                             `json:"reasoning_content,omitempty"`
	ReasoningBlocks     []providers.ReasoningBlock         `json:"reasoning_blocks,omitempty"`
	Images              []persistedImage                   `json:"images,omitempty"`
	Files               []persistedFile                    `json:"files,omitempty"`
	ToolCalls           []persistedToolCall                `json:"tool_calls,omitempty"`
	DiscoveredTools     []providers.LoadableToolDefinition `json:"discovered_tools,omitempty"`
	ToolCallID          string                             `json:"tool_call_id,omitempty"`
	ToolResultKind      string                             `json:"tool_result_kind,omitempty"`
	FinishReason        string                             `json:"finish_reason,omitempty"`
	StopReason          string                             `json:"stop_reason,omitempty"`
	Truncated           bool                               `json:"truncated,omitempty"`
	Name                string                             `json:"name,omitempty"`
	At                  time.Time                          `json:"at,omitempty"`
	InputTokens         int                                `json:"input_tokens,omitempty"`
	OutputTokens        int                                `json:"output_tokens,omitempty"`
	CacheCreationTokens int                                `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int                                `json:"cache_read_tokens,omitempty"`
	// Provider and Model carry which provider/model produced this row's
	// token_usage. Only populated when Role=="meta" and Content=="token_usage";
	// empty for chat records and for legacy token_usage rows written before
	// this field was added.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
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
	records, err := loadPersistedMessages(path, false)
	if err != nil {
		return nil, err
	}

	var messages []providers.ChatMessage
	for _, rec := range records {
		role := strings.ToLower(strings.TrimSpace(rec.Role))
		if role == "" || role == "meta" {
			continue
		}
		msg := providers.ChatMessage{
			Role:              role,
			Name:              rec.Name,
			ClientID:          rec.ClientID,
			Content:           rec.Content,
			DisplayContent:    rec.DisplayContent,
			Phase:             providers.NormalizeMessagePhase(rec.Phase),
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
		messages = append(messages, msg)
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
	rec := persistedMessageFromChatMessage(msg)
	if sessDir, id, ok, err := managedHistoryTarget(path); err != nil {
		return err
	} else if ok {
		return sessionstore.AppendHistoryRecord(sessDir, id, historyRecordFromPersistedMessage(rec))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create session directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open session history for append: %w", err)
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(rec); err != nil {
		return fmt.Errorf("write session message: %w", err)
	}
	return nil
}

func rewriteChatHistory(path string, msgs []providers.ChatMessage) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	metas, err := loadMetaMessages(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load session metadata: %w", err)
	}
	if sessDir, id, ok, err := managedHistoryTarget(path); err != nil {
		return err
	} else if ok {
		records := make([]sessionstore.HistoryRecord, 0, len(msgs)+len(metas))
		for _, msg := range msgs {
			if !shouldPersistMessage(msg) {
				continue
			}
			records = append(records, historyRecordFromPersistedMessage(persistedMessageFromChatMessage(msg)))
		}
		for _, rec := range metas {
			records = append(records, historyRecordFromPersistedMessage(rec))
		}
		return sessionstore.RewriteHistoryRecords(sessDir, id, records)
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

// appendTokenUsage persists one cumulative token usage snapshot to the session
// history. provider and model tag the row so the insight scanner can aggregate
// usage per provider/model across sessions. Empty values are preserved as empty
// strings, which the scanner interprets as "unknown provider/model".
func appendTokenUsage(path, provider, model string, usage providers.TokenUsage) error {
	if strings.TrimSpace(path) == "" || (usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheCreationTokens == 0 && usage.CacheReadTokens == 0) {
		return nil
	}
	rec := persistedMessage{
		Role:                "meta",
		Content:             "token_usage",
		Provider:            strings.TrimSpace(provider),
		Model:               strings.TrimSpace(model),
		At:                  time.Now().UTC(),
		InputTokens:         usage.InputTokens,
		OutputTokens:        usage.OutputTokens,
		CacheCreationTokens: usage.CacheCreationTokens,
		CacheReadTokens:     usage.CacheReadTokens,
	}
	if sessDir, id, ok, err := managedHistoryTarget(path); err != nil {
		return err
	} else if ok {
		return sessionstore.AppendHistoryRecord(sessDir, id, historyRecordFromPersistedMessage(rec))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create session directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open session history for append: %w", err)
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(rec); err != nil {
		return fmt.Errorf("write token usage: %w", err)
	}
	return nil
}

func persistedMessageFromChatMessage(msg providers.ChatMessage) persistedMessage {
	out := persistedMessage{
		Role:              strings.ToLower(msg.Role),
		Content:           msg.Content,
		DisplayContent:    msg.DisplayContent,
		Phase:             string(msg.Phase),
		ProviderItemID:    msg.ProviderItemID,
		ProviderItemModel: msg.ProviderItemModel,
		ClientID:          msg.ClientID,
		Steered:           msg.Steered,
		ReasoningContent:  msg.ReasoningContent,
		ReasoningBlocks:   append([]providers.ReasoningBlock(nil), msg.ReasoningBlocks...),
		DiscoveredTools:   providers.CloneLoadableToolDefinitions(msg.DiscoveredTools),
		ToolCallID:        msg.ToolCallID,
		ToolResultKind:    string(msg.ToolResultKind),
		FinishReason:      string(msg.FinishReason),
		StopReason:        strings.ToLower(strings.TrimSpace(msg.StopReason)),
		Truncated:         msg.Truncated,
		Name:              msg.Name,
		At:                time.Now().UTC(),
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
	for _, file := range msg.Files {
		data := strings.TrimSpace(file.Data)
		if data == "" {
			continue
		}
		out.Files = append(out.Files, persistedFile{
			MediaType: file.MediaType,
			Data:      data,
			Filename:  file.Filename,
		})
	}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, persistedToolCall{
			ID:                tc.ID,
			ProviderItemID:    tc.ProviderItemID,
			ProviderItemModel: tc.ProviderItemModel,
			Name:              tc.Name,
			Arguments:         tc.Arguments,
			Kind:              string(tc.Kind),
			Display:           cloneToolCallDisplay(tc.Display),
		})
	}
	return out
}

func loadMetaMessages(path string) ([]persistedMessage, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	if records, err := loadPersistedMessages(path, true); err != nil {
		return nil, err
	} else if records != nil {
		metas := make([]persistedMessage, 0)
		for _, rec := range records {
			if strings.EqualFold(strings.TrimSpace(rec.Role), "meta") {
				metas = append(metas, rec)
			}
		}
		return metas, nil
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

func loadPersistedMessages(path string, includeMeta bool) ([]persistedMessage, error) {
	if sessDir, id, ok, err := managedHistoryTarget(path); err != nil {
		return nil, err
	} else if ok {
		records, err := sessionstore.LoadHistoryRecords(sessDir, id, includeMeta)
		if err != nil {
			return nil, fmt.Errorf("load session history: %w", err)
		}
		out := make([]persistedMessage, 0, len(records))
		for _, rec := range records {
			msg, err := persistedMessageFromHistoryRecord(rec)
			if err != nil {
				return nil, err
			}
			out = append(out, msg)
		}
		return out, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open session history: %w", err)
	}
	defer file.Close()

	var messages []persistedMessage
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
		if !includeMeta && strings.EqualFold(strings.TrimSpace(rec.Role), "meta") {
			return nil
		}
		messages = append(messages, rec)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan session history: %w", err)
	}
	return messages, nil
}

func managedHistoryTarget(path string) (string, string, bool, error) {
	sessDir, id, ok := sessionstore.ParseHistoryPath(path)
	if !ok {
		return "", "", false, nil
	}
	if _, err := os.Stat(sessionstore.DBPath(sessDir)); err != nil {
		if !os.IsNotExist(err) {
			return "", "", false, err
		}
		if _, indexErr := os.Stat(sessionstore.IndexPath(sessDir)); indexErr != nil {
			if os.IsNotExist(indexErr) {
				return "", "", false, nil
			}
			return "", "", false, indexErr
		}
	}
	_, exists, err := sessionstore.Find(sessDir, id)
	if err != nil {
		return "", "", false, err
	}
	return sessDir, id, exists, nil
}

func historyRecordFromPersistedMessage(rec persistedMessage) sessionstore.HistoryRecord {
	return sessionstore.HistoryRecord{
		Role:                rec.Role,
		Content:             rec.Content,
		DisplayContent:      rec.DisplayContent,
		Phase:               rec.Phase,
		ProviderItemID:      rec.ProviderItemID,
		ProviderItemModel:   rec.ProviderItemModel,
		ClientID:            rec.ClientID,
		Steered:             rec.Steered,
		ReasoningContent:    rec.ReasoningContent,
		ReasoningBlocks:     mustJSON(rec.ReasoningBlocks),
		Images:              mustJSON(rec.Images),
		Files:               mustJSON(rec.Files),
		ToolCalls:           mustJSON(rec.ToolCalls),
		DiscoveredTools:     mustJSON(rec.DiscoveredTools),
		ToolCallID:          rec.ToolCallID,
		ToolResultKind:      rec.ToolResultKind,
		FinishReason:        rec.FinishReason,
		StopReason:          rec.StopReason,
		Truncated:           rec.Truncated,
		Name:                rec.Name,
		At:                  rec.At,
		InputTokens:         rec.InputTokens,
		OutputTokens:        rec.OutputTokens,
		CacheCreationTokens: rec.CacheCreationTokens,
		CacheReadTokens:     rec.CacheReadTokens,
		Provider:            rec.Provider,
		Model:               rec.Model,
	}
}

func persistedMessageFromHistoryRecord(rec sessionstore.HistoryRecord) (persistedMessage, error) {
	out := persistedMessage{
		Role:                rec.Role,
		Content:             rec.Content,
		DisplayContent:      rec.DisplayContent,
		Phase:               rec.Phase,
		ProviderItemID:      rec.ProviderItemID,
		ProviderItemModel:   rec.ProviderItemModel,
		ClientID:            rec.ClientID,
		Steered:             rec.Steered,
		ReasoningContent:    rec.ReasoningContent,
		ToolCallID:          rec.ToolCallID,
		ToolResultKind:      rec.ToolResultKind,
		FinishReason:        rec.FinishReason,
		StopReason:          rec.StopReason,
		Truncated:           rec.Truncated,
		Name:                rec.Name,
		At:                  rec.At,
		InputTokens:         rec.InputTokens,
		OutputTokens:        rec.OutputTokens,
		CacheCreationTokens: rec.CacheCreationTokens,
		CacheReadTokens:     rec.CacheReadTokens,
		Provider:            rec.Provider,
		Model:               rec.Model,
	}
	if err := unmarshalRaw(rec.ReasoningBlocks, &out.ReasoningBlocks); err != nil {
		return persistedMessage{}, err
	}
	if err := unmarshalRaw(rec.Images, &out.Images); err != nil {
		return persistedMessage{}, err
	}
	if err := unmarshalRaw(rec.Files, &out.Files); err != nil {
		return persistedMessage{}, err
	}
	if err := unmarshalRaw(rec.ToolCalls, &out.ToolCalls); err != nil {
		return persistedMessage{}, err
	}
	if err := unmarshalRaw(rec.DiscoveredTools, &out.DiscoveredTools); err != nil {
		return persistedMessage{}, err
	}
	out.DiscoveredTools = providers.CloneLoadableToolDefinitions(out.DiscoveredTools)
	return out, nil
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil || string(data) == "null" || string(data) == "[]" {
		return nil
	}
	return data
}

func unmarshalRaw(raw json.RawMessage, out any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode session history payload: %w", err)
	}
	return nil
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
		if shouldPersistMessage(msg) && !compact.IsInternalContextMessage(msg) {
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

func replaceBaseSystemPrompt(history []providers.ChatMessage, prompt string) []providers.ChatMessage {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return history
	}
	out := cloneHistory(history)
	if len(out) == 0 {
		return []providers.ChatMessage{{Role: "system", Content: prompt}}
	}
	if strings.EqualFold(out[0].Role, "system") {
		if out[0].Content == prompt {
			return history
		}
		out[0].Content = prompt
		return out
	}
	return append([]providers.ChatMessage{{Role: "system", Content: prompt}}, out...)
}
