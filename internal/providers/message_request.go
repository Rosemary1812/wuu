package providers

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// PrepareMessagesForModelRequest applies shared tool-call history repair plus
// request-only model/provider compatibility transforms. It deliberately does
// not mutate stored history; clients call this just before lowering to wire
// format.
func PrepareMessagesForModelRequest(model string, msgs []ChatMessage) ([]ChatMessage, error) {
	repaired, err := RepairAndValidateToolCallHistory(msgs)
	if err != nil {
		return nil, err
	}
	projected := ApplyToolResultProjections(repaired)
	compatible := ApplyModelMessageCompatibility(model, projected)
	if err := ValidateToolCallHistory(compatible); err != nil {
		return nil, fmt.Errorf("invalid message sequence after model compatibility: %w", err)
	}
	return compatible, nil
}

// ApplyModelMessageCompatibility mirrors the request-only message transforms
// in OpenCode's ProviderTransform.message that are relevant to wuu's Go
// clients.
func ApplyModelMessageCompatibility(model string, msgs []ChatMessage) []ChatMessage {
	if len(msgs) == 0 {
		return nil
	}
	out := cloneMessagesForRequest(msgs)
	out = sanitizeMessageText(out)

	lower := strings.ToLower(strings.TrimSpace(model))
	switch {
	case isMistralModel(lower):
		out = rewriteToolCallIDs(out, scrubMistralToolCallID)
		out = insertMistralToolUserSeparator(out)
	case isClaudeModel(lower):
		out = rewriteToolCallIDs(out, scrubClaudeToolCallID)
	}
	return out
}

func cloneMessagesForRequest(msgs []ChatMessage) []ChatMessage {
	out := make([]ChatMessage, len(msgs))
	for i, msg := range msgs {
		out[i] = msg
		if len(msg.Images) > 0 {
			out[i].Images = append([]InputImage(nil), msg.Images...)
		}
		if len(msg.Files) > 0 {
			out[i].Files = append([]InputFile(nil), msg.Files...)
		}
		if len(msg.ToolCalls) > 0 {
			out[i].ToolCalls = append([]ToolCall(nil), msg.ToolCalls...)
		}
		if msg.ToolResult != nil {
			result := msg.ToolResult.Clone()
			out[i].ToolResult = &result
		}
		if len(msg.ReasoningBlocks) > 0 {
			out[i].ReasoningBlocks = append([]ReasoningBlock(nil), msg.ReasoningBlocks...)
		}
	}
	return out
}

func sanitizeMessageText(msgs []ChatMessage) []ChatMessage {
	for i := range msgs {
		msgs[i].Content = SanitizeSurrogates(msgs[i].Content)
		msgs[i].ReasoningContent = SanitizeSurrogates(msgs[i].ReasoningContent)
		for j := range msgs[i].ReasoningBlocks {
			msgs[i].ReasoningBlocks[j].Thinking = SanitizeSurrogates(msgs[i].ReasoningBlocks[j].Thinking)
		}
		for j := range msgs[i].Files {
			msgs[i].Files[j].Filename = SanitizeSurrogates(msgs[i].Files[j].Filename)
		}
	}
	return msgs
}

// SanitizeSurrogates replaces UTF-16 surrogate code points with U+FFFD.
// Several provider frontends reject lone surrogates even when Go can encode
// them into JSON strings.
func SanitizeSurrogates(content string) string {
	return strings.Map(func(r rune) rune {
		if r >= 0xD800 && r <= 0xDFFF {
			return '\uFFFD'
		}
		return r
	}, content)
}

func rewriteToolCallIDs(msgs []ChatMessage, scrub func(string) string) []ChatMessage {
	for i := range msgs {
		if msgs[i].ToolCallID != "" {
			msgs[i].ToolCallID = scrub(msgs[i].ToolCallID)
		}
		for j := range msgs[i].ToolCalls {
			msgs[i].ToolCalls[j].ID = scrub(msgs[i].ToolCalls[j].ID)
		}
	}
	return msgs
}

func scrubClaudeToolCallID(id string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, id)
}

// scrubMistralToolCallID maps an arbitrary tool-call ID onto Mistral's
// required 9-alphanumeric form. An ID that is already valid passes through;
// everything else is hashed rather than prefix-truncated, because prefix
// truncation collapses IDs sharing their first nine alphanumerics — e.g.
// Moonshot-style "functions.name:0" and "functions.name:1" both truncated to
// "functions" — into duplicates that fail history validation on every retry,
// permanently fail-closing the conversation on Mistral models.
func scrubMistralToolCallID(id string) string {
	if len(id) == 9 && isMistralToolCallID(id) {
		return id
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	sum := h.Sum64()
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var b [9]byte
	for i := len(b) - 1; i >= 0; i-- {
		b[i] = digits[sum%36]
		sum /= 36
	}
	return string(b[:])
}

func isMistralToolCallID(id string) bool {
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func insertMistralToolUserSeparator(msgs []ChatMessage) []ChatMessage {
	out := make([]ChatMessage, 0, len(msgs)+1)
	for i, msg := range msgs {
		out = append(out, msg)
		if msg.Role == "tool" && i+1 < len(msgs) && msgs[i+1].Role == "user" {
			out = append(out, ChatMessage{Role: "assistant", Content: "Done."})
		}
	}
	return out
}

func isClaudeModel(model string) bool {
	return strings.Contains(model, "claude") || strings.Contains(model, "anthropic")
}

func isMistralModel(model string) bool {
	return strings.Contains(model, "mistral") || strings.Contains(model, "devstral")
}

func IsDeepSeekModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(model, "deepseek")
}
