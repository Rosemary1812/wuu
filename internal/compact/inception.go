package compact

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
)

const (
	InceptionToolName           = "inception"
	InceptionHistoryRewriteKind = "inception_context_rewrite"
	ContextAnchorMessageName    = "wuu_context_anchor"
	ContextContinuationName     = "wuu_context_continuation"
	ContextAnchorPrefix         = "CHECKPOINT"
	legacyContextAnchorPrefix   = "[Wuu context checkpoint]"
	InceptionContinuationPrefix = "[D-Mail continuation]"
	systemReminderOpen          = "<system-reminder>"
	systemReminderClose         = "</system-reminder>"
	systemTagOpen               = "<system>"
	systemTagClose              = "</system>"
)

type InceptionHistoryRewrite struct {
	Kind     string `json:"kind"`
	AnchorID int    `json:"anchor_id"`
	Content  string `json:"content"`
}

type inceptionToolEnvelope struct {
	Action         string                   `json:"action"`
	Status         string                   `json:"status"`
	HistoryRewrite *InceptionHistoryRewrite `json:"history_rewrite,omitempty"`
}

func BuildContextAnchorMessage(anchorID int) providers.ChatMessage {
	return providers.ChatMessage{
		Role:    "user",
		Name:    ContextAnchorMessageName,
		Content: fmt.Sprintf("%s%s %d%s", systemTagOpen, ContextAnchorPrefix, anchorID, systemTagClose),
		Hidden:  true,
	}
}

func NextContextAnchorID(messages []providers.ChatMessage) int {
	next := 0
	for _, msg := range messages {
		if id, ok := ContextAnchorIDFromMessage(msg); ok && id >= next {
			next = id + 1
		}
	}
	return next
}

func ContextAnchorIDFromMessage(msg providers.ChatMessage) (int, bool) {
	role := strings.ToLower(strings.TrimSpace(msg.Role))
	if role != "user" && role != "system" {
		return 0, false
	}
	if strings.TrimSpace(msg.Name) != ContextAnchorMessageName && !IsContextAnchorContent(msg.Content) {
		return 0, false
	}
	return ContextAnchorIDFromContent(msg.Content)
}

func IsContextAnchorContent(content string) bool {
	_, ok := ContextAnchorIDFromContent(content)
	return ok
}

func ContextAnchorIDFromContent(content string) (int, bool) {
	if id, ok := contextAnchorIDFromKimiMarker(content); ok {
		return id, true
	}
	return contextAnchorIDFromLegacyMarker(content)
}

func contextAnchorIDFromKimiMarker(content string) (int, bool) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, systemTagOpen) || !strings.HasSuffix(content, systemTagClose) {
		return 0, false
	}
	content = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(content, systemTagOpen), systemTagClose))
	fields := strings.Fields(content)
	if len(fields) != 2 || !strings.EqualFold(fields[0], ContextAnchorPrefix) {
		return 0, false
	}
	id, err := strconv.Atoi(strings.TrimSpace(fields[1]))
	if err != nil || id < 0 {
		return 0, false
	}
	return id, true
}

func contextAnchorIDFromLegacyMarker(content string) (int, bool) {
	lines := strings.Split(unwrapInternalContextContent(content), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != legacyContextAnchorPrefix {
		return 0, false
	}
	for _, line := range lines[1:] {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || strings.TrimSpace(key) != "anchor_id" {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || id < 0 {
			return 0, false
		}
		return id, true
	}
	return 0, false
}

func FindContextAnchorIndex(messages []providers.ChatMessage, anchorID int) (int, bool) {
	if anchorID < 0 {
		return 0, false
	}
	for i, msg := range messages {
		if id, ok := ContextAnchorIDFromMessage(msg); ok && id == anchorID {
			return i, true
		}
	}
	return 0, false
}

func BuildInceptionContinuationContent(anchorID int, summary string) string {
	summary = strings.TrimSpace(summary)
	var b strings.Builder
	b.WriteString(InceptionContinuationPrefix)
	fmt.Fprintf(&b, "\nYou just received a D-Mail from your future self at Wuu context checkpoint %d. Only conversation history was rewritten; files, processes, browser state, remote systems, and other external state remain current.\n\n", anchorID)
	b.WriteString(summary)
	return wrapInternalContextContent(b.String())
}

func IsInternalContextMessage(msg providers.ChatMessage) bool {
	name := strings.TrimSpace(msg.Name)
	return name == ContextAnchorMessageName ||
		name == ContextContinuationName ||
		IsContextAnchorContent(msg.Content) ||
		IsInceptionContinuationContent(msg.Content)
}

func IsInceptionContinuationContent(content string) bool {
	return strings.HasPrefix(unwrapInternalContextContent(content), InceptionContinuationPrefix)
}

func wrapInternalContextContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	return systemReminderOpen + "\n" + content + "\n" + systemReminderClose
}

func unwrapInternalContextContent(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, systemReminderOpen) && strings.HasSuffix(content, systemReminderClose) {
		content = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(content, systemReminderOpen), systemReminderClose))
	}
	return strings.TrimSpace(content)
}

func RewriteHistoryFromInternalToolMessages(messages []providers.ChatMessage, toolMessages []providers.ChatMessage) ([]providers.ChatMessage, bool, error) {
	if rewritten, ok, err := RewriteHistoryFromHelpMeToolMessages(messages, toolMessages); err != nil || ok {
		return rewritten, ok, err
	}
	return RewriteHistoryFromInceptionToolMessages(messages, toolMessages)
}

func RewriteHistoryFromInternalToolMessagesWithContext(_ context.Context, messages []providers.ChatMessage, toolMessages []providers.ChatMessage) ([]providers.ChatMessage, bool, error) {
	return RewriteHistoryFromInternalToolMessages(messages, toolMessages)
}

func RewriteHistoryFromInceptionToolMessages(messages []providers.ChatMessage, toolMessages []providers.ChatMessage) ([]providers.ChatMessage, bool, error) {
	rewrite, ok, err := InceptionRewriteFromToolMessages(toolMessages)
	if err != nil || !ok {
		return nil, false, err
	}
	return RewriteHistoryWithInceptionContinuation(messages, rewrite.AnchorID, rewrite.Content)
}

func InceptionRewriteFromToolMessages(toolMessages []providers.ChatMessage) (InceptionHistoryRewrite, bool, error) {
	for _, msg := range toolMessages {
		if !strings.EqualFold(strings.TrimSpace(msg.Name), InceptionToolName) {
			continue
		}
		rewrite, ok, err := InceptionRewriteFromToolMessage(msg)
		if err != nil || ok {
			return rewrite, ok, err
		}
	}
	return InceptionHistoryRewrite{}, false, nil
}

func InceptionRewriteFromToolMessage(msg providers.ChatMessage) (InceptionHistoryRewrite, bool, error) {
	if !strings.EqualFold(strings.TrimSpace(msg.Name), InceptionToolName) {
		return InceptionHistoryRewrite{}, false, nil
	}
	var envelope inceptionToolEnvelope
	if err := json.Unmarshal([]byte(msg.Content), &envelope); err != nil {
		return InceptionHistoryRewrite{}, false, fmt.Errorf("inception history rewrite: parse tool result: %w", err)
	}
	if envelope.HistoryRewrite == nil {
		return InceptionHistoryRewrite{}, false, nil
	}
	rewrite := *envelope.HistoryRewrite
	if strings.TrimSpace(rewrite.Kind) != InceptionHistoryRewriteKind {
		return InceptionHistoryRewrite{}, false, nil
	}
	rewrite.Content = strings.TrimSpace(rewrite.Content)
	if rewrite.AnchorID < 0 {
		return InceptionHistoryRewrite{}, false, fmt.Errorf("inception history rewrite: anchor_id must be non-negative")
	}
	if rewrite.Content == "" {
		return InceptionHistoryRewrite{}, false, nil
	}
	return rewrite, true, nil
}

func RewriteHistoryWithInceptionContinuation(messages []providers.ChatMessage, anchorID int, content string) ([]providers.ChatMessage, bool, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return providers.CloneChatMessages(messages), false, nil
	}
	anchorIndex, ok := FindContextAnchorIndex(messages, anchorID)
	if !ok {
		return nil, false, fmt.Errorf("inception history rewrite: anchor %d not found", anchorID)
	}

	rewritten := providers.CloneChatMessages(messages[:anchorIndex])
	discovered := providers.DiscoveredToolsFromMessages(messages[anchorIndex:])
	rewritten = append(rewritten, providers.ChatMessage{
		Role:            "user",
		Name:            ContextContinuationName,
		Content:         content,
		Hidden:          true,
		DiscoveredTools: discovered,
	})
	return rewritten, true, nil
}
