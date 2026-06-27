package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
)

const requestShapeHashBytes = 16

func requestContextInfo(stepIndex int, assembly RequestAssembly, tools []providers.ToolDefinition, hint *providers.CacheHint) RequestContextInfo {
	messages := assembly.Messages
	systemMessages := systemMessageCount(messages)
	stablePrefix := 0
	promptCacheKey := ""
	if hint != nil {
		stablePrefix = hint.StablePrefixMessages
		promptCacheKey = strings.TrimSpace(hint.PromptCacheKey)
	}
	if stablePrefix < 0 {
		stablePrefix = 0
	}
	if maxStable := len(messages) - systemMessages; stablePrefix > maxStable {
		stablePrefix = maxStable
	}
	stableEnd := systemMessages + stablePrefix
	if stableEnd > len(messages) {
		stableEnd = len(messages)
	}

	info := RequestContextInfo{
		StepIndex:         stepIndex,
		MessageCount:      len(messages),
		SystemMessages:    systemMessages,
		ToolCount:         len(tools),
		StablePrefix:      stablePrefix,
		SystemBytes:       messageBytesForRequestShape(messages[:systemMessages]),
		StablePrefixBytes: messageBytesForRequestShape(messages[:stableEnd]),
		MessageBytes:      messageBytesForRequestShape(messages),
		ToolSchemaBytes:   toolSchemaBytesForRequestShape(tools),
		SystemHash:        hashMessagesForRequestShape(messages[:systemMessages]),
		StablePrefixHash:  hashMessagesForRequestShape(messages[:stableEnd]),
		ToolSurfaceHash:   hashToolsForRequestShape(tools),
		PromptCacheKey:    promptCacheKey,
	}

	seenKinds := make(map[string]struct{})
	for _, msg := range messages {
		if msg.Hidden {
			info.HiddenMessages++
		}
	}
	for _, msg := range assembly.RequestOnlyMessages {
		info.DynamicBytes += len([]byte(msg.Content))
		if !wuucontext.IsSystemReminder(msg.Name, msg.Content) {
			continue
		}
		info.TransientMessages++
		info.ContentBytes += len([]byte(msg.Content))
		for _, kind := range systemReminderBlockKinds(msg.Content) {
			if _, ok := seenKinds[kind]; ok {
				continue
			}
			seenKinds[kind] = struct{}{}
			info.BlockKinds = append(info.BlockKinds, kind)
		}
	}
	for _, segment := range assembly.Segments {
		for _, block := range segment.Blocks {
			if strings.TrimSpace(wuucontext.CompileBlocks([]wuucontext.Block{block})) == "" {
				continue
			}
			kind := strings.TrimSpace(string(block.Kind))
			if kind == "" {
				kind = string(wuucontext.BlockAdditionalContext)
			}
			if _, ok := seenKinds[kind]; ok {
				continue
			}
			seenKinds[kind] = struct{}{}
			info.BlockKinds = append(info.BlockKinds, kind)
		}
	}
	return info
}

func messageBytesForRequestShape(messages []providers.ChatMessage) int {
	total := 0
	for _, msg := range messages {
		total += len([]byte(msg.Name))
		total += len([]byte(msg.Content))
		total += len([]byte(msg.ReasoningContent))
		total += len([]byte(msg.ToolCallID))
		for _, block := range msg.ReasoningBlocks {
			total += len([]byte(block.Type))
			total += len([]byte(block.Thinking))
			total += len([]byte(block.Signature))
			total += len([]byte(block.Data))
		}
		for _, image := range msg.Images {
			total += len([]byte(image.MediaType))
			total += len([]byte(image.Data))
		}
		for _, file := range msg.Files {
			total += len([]byte(file.MediaType))
			total += len([]byte(file.Data))
			total += len([]byte(file.Filename))
		}
		for _, call := range msg.ToolCalls {
			total += len([]byte(call.ID))
			total += len([]byte(call.ProviderItemID))
			total += len([]byte(call.ProviderItemModel))
			total += len([]byte(call.Name))
			total += len([]byte(call.Arguments))
			total += len([]byte(string(call.Kind)))
		}
		for _, tool := range msg.DiscoveredTools {
			if raw, err := json.Marshal(tool); err == nil {
				total += len(raw)
			}
		}
	}
	return total
}

func hashMessagesForRequestShape(messages []providers.ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(strings.ToLower(strings.TrimSpace(msg.Role)))
		b.WriteByte('\n')
		b.WriteString(strings.TrimSpace(msg.Name))
		b.WriteByte('\n')
		if msg.Hidden {
			b.WriteString("hidden\n")
		}
		b.WriteString(msg.Content)
		b.WriteByte('\n')
		if msg.ToolCallID != "" {
			b.WriteString("tool_call_id:")
			b.WriteString(msg.ToolCallID)
			b.WriteByte('\n')
		}
		for _, call := range msg.ToolCalls {
			b.WriteString("tool_call:")
			b.WriteString(call.ID)
			b.WriteByte('\n')
			b.WriteString(call.Name)
			b.WriteByte('\n')
			b.WriteString(call.Arguments)
			b.WriteByte('\n')
		}
	}
	return shortRequestShapeHash(b.String())
}

func toolSchemaBytesForRequestShape(tools []providers.ToolDefinition) int {
	if len(tools) == 0 {
		return 0
	}
	type toolSchema struct {
		Name         string         `json:"name"`
		Description  string         `json:"description,omitempty"`
		InputSchema  map[string]any `json:"input_schema,omitempty"`
		DeferLoading bool           `json:"defer_loading,omitempty"`
		CacheStable  bool           `json:"cache_stable,omitempty"`
	}
	schemas := make([]toolSchema, 0, len(tools))
	for _, tool := range tools {
		schemas = append(schemas, toolSchema{
			Name:         tool.Name,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			DeferLoading: tool.DeferLoading,
			CacheStable:  tool.CacheStable,
		})
	}
	raw, err := json.Marshal(schemas)
	if err != nil {
		return 0
	}
	return len(raw)
}

func hashToolsForRequestShape(tools []providers.ToolDefinition) string {
	if len(tools) == 0 {
		return ""
	}
	var b strings.Builder
	for _, tool := range tools {
		b.WriteString(tool.Name)
		b.WriteByte('\n')
		b.WriteString(tool.Description)
		b.WriteByte('\n')
		b.WriteString(strconv.FormatBool(tool.DeferLoading))
		b.WriteByte('\n')
		b.WriteString(strconv.FormatBool(tool.CacheStable))
		b.WriteByte('\n')
		if tool.InputSchema != nil {
			if raw, err := json.Marshal(tool.InputSchema); err == nil {
				b.Write(raw)
			}
		}
		b.WriteByte('\n')
	}
	return shortRequestShapeHash(b.String())
}

func shortRequestShapeHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:requestShapeHashBytes])
}
