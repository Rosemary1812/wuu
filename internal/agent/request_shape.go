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
		StepIndex:        stepIndex,
		MessageCount:     len(messages),
		SystemMessages:   systemMessages,
		ToolCount:        len(tools),
		StablePrefix:     stablePrefix,
		SystemHash:       hashMessagesForRequestShape(messages[:systemMessages]),
		StablePrefixHash: hashMessagesForRequestShape(messages[:stableEnd]),
		ToolSurfaceHash:  hashToolsForRequestShape(tools),
		PromptCacheKey:   promptCacheKey,
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
