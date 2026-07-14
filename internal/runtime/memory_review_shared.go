package runtime

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/providers"
)

const (
	memoryReviewMaxTranscriptMessages = 60
	memoryReviewMaxMessageBytes       = 2000
)

type backgroundMemoryStep struct {
	client providers.Client
}

func (s backgroundMemoryStep) Execute(ctx context.Context, req providers.ChatRequest) (agent.StepResult, error) {
	resp, err := providers.ExecuteChat(ctx, s.client, req, providers.InferenceOperationMemory, providers.InferenceProfileBestEffort)
	if err != nil {
		return agent.StepResult{}, err
	}
	return agent.StepResult{
		Content:          resp.Content,
		ReasoningContent: resp.ReasoningContent,
		ReasoningBlocks:  append([]providers.ReasoningBlock(nil), resp.ReasoningBlocks...),
		ToolCalls:        append([]providers.ToolCall(nil), resp.ToolCalls...),
		FinishReason:     resp.FinishReason,
		Truncated:        resp.Truncated,
		StopReason:       resp.StopReason,
		Usage:            resp.Usage,
	}, nil
}

func selectMemoryReviewTranscript(history []providers.ChatMessage) []providers.ChatMessage {
	transcript := make([]providers.ChatMessage, 0, len(history))
	for _, msg := range history {
		role := strings.TrimSpace(msg.Role)
		if role != "user" && role != "assistant" {
			continue
		}
		content := normalizeMemoryReviewContent(msg.Content)
		if content == "" || isSyntheticMemoryReviewUserMessage(role, content) {
			continue
		}
		transcript = append(transcript, providers.ChatMessage{Role: role, Content: content})
	}
	if len(transcript) > memoryReviewMaxTranscriptMessages {
		return transcript[len(transcript)-memoryReviewMaxTranscriptMessages:]
	}
	return transcript
}

func normalizeMemoryReviewContent(content string) string {
	content = strings.TrimSpace(strings.Join(strings.Fields(content), " "))
	if content == "" {
		return ""
	}
	if len(content) <= memoryReviewMaxMessageBytes {
		return content
	}
	cut := memoryReviewMaxMessageBytes
	for cut > 0 && !utf8.ValidString(content[:cut]) {
		cut--
	}
	return content[:cut] + " [truncated]"
}

func isSyntheticMemoryReviewUserMessage(role, content string) bool {
	if role != "user" {
		return false
	}
	return strings.HasPrefix(content, "[Hook context for ") ||
		strings.HasPrefix(content, "Output token limit hit. Resume directly")
}

func countMemoryReviewUserTurns(history []providers.ChatMessage) int {
	count := 0
	for _, msg := range history {
		content := normalizeMemoryReviewContent(msg.Content)
		if strings.TrimSpace(msg.Role) == "user" && content != "" && !isSyntheticMemoryReviewUserMessage("user", content) {
			count++
		}
	}
	return count
}

func cloneMemoryReviewHistory(messages []providers.ChatMessage) []providers.ChatMessage {
	out := make([]providers.ChatMessage, len(messages))
	for i, msg := range messages {
		out[i] = msg
		out[i].Images = nil
		out[i].Files = nil
		out[i].ReasoningBlocks = nil
		out[i].ReasoningContent = ""
		out[i].ToolCalls = nil
	}
	return out
}
