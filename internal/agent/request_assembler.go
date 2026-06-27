package agent

import (
	"strings"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
)

// ContextSegmentLifecycle describes how long a context segment lives.
type ContextSegmentLifecycle string

const (
	// ContextSegmentRequestOnly is visible to the model for the current
	// provider request but is never appended to live or durable history.
	ContextSegmentRequestOnly ContextSegmentLifecycle = "request_only"
)

// ContextSegmentPlacement describes where a segment is placed in the provider
// request. The first supported placement keeps dynamic context after the live
// conversation so it cannot become part of the stable prompt prefix.
type ContextSegmentPlacement string

const (
	ContextSegmentAfterHistory ContextSegmentPlacement = "after_history"
)

// ContextSegmentCachePolicy records the cache intent of a segment. The
// provider adapters still receive plain ChatMessages; this metadata exists so
// request assembly, telemetry, and future provider-specific cache handling have
// one owner instead of inferring intent from ChatMessage.Hidden.
type ContextSegmentCachePolicy string

const (
	ContextSegmentVolatile ContextSegmentCachePolicy = "volatile"
)

// ContextSegment is model-visible context assembled for a provider request.
// It makes the request lifecycle explicit instead of overloading
// providers.ChatMessage.Hidden with durability, UI visibility, and cache intent.
type ContextSegment struct {
	Messages    []providers.ChatMessage
	Blocks      []wuucontext.Block
	Lifecycle   ContextSegmentLifecycle
	Placement   ContextSegmentPlacement
	CachePolicy ContextSegmentCachePolicy
	Durable     bool
	VisibleInUI bool
}

// RequestOnlyContextMessages wraps messages as volatile request-only context.
func RequestOnlyContextMessages(messages []providers.ChatMessage) []ContextSegment {
	if len(messages) == 0 {
		return nil
	}
	return []ContextSegment{RequestOnlyContextSegment(messages)}
}

// RequestOnlyContextSegment builds a request-only segment from ChatMessages
// while marking them hidden from user-facing transcript surfaces.
func RequestOnlyContextSegment(messages []providers.ChatMessage) ContextSegment {
	return ContextSegment{
		Messages:    normalizeRequestOnlyMessages(messages),
		Lifecycle:   ContextSegmentRequestOnly,
		Placement:   ContextSegmentAfterHistory,
		CachePolicy: ContextSegmentVolatile,
		Durable:     false,
		VisibleInUI: false,
	}
}

// RequestOnlyContextBlocks wraps typed runtime context blocks as volatile
// request-only context. The segment keeps the typed blocks as the source of
// truth; ChatMessages are only the provider request projection.
func RequestOnlyContextBlocks(blocks []wuucontext.Block) []ContextSegment {
	if len(blocks) == 0 {
		return nil
	}
	return []ContextSegment{RequestOnlyContextBlockSegment(blocks)}
}

// RequestOnlyContextBlockSegment builds a request-only segment from typed
// context blocks.
func RequestOnlyContextBlockSegment(blocks []wuucontext.Block) ContextSegment {
	copied := append([]wuucontext.Block(nil), blocks...)
	return ContextSegment{
		Messages:    requestOnlyMessagesFromBlocks(copied),
		Blocks:      copied,
		Lifecycle:   ContextSegmentRequestOnly,
		Placement:   ContextSegmentAfterHistory,
		CachePolicy: ContextSegmentVolatile,
		Durable:     false,
		VisibleInUI: false,
	}
}

// RequestAssembly is the provider request view produced from durable live
// history plus request-only context segments.
type RequestAssembly struct {
	Messages            []providers.ChatMessage
	RequestOnlyMessages []providers.ChatMessage
	Segments            []ContextSegment
}

func assembleModelRequest(history []providers.ChatMessage, segments []ContextSegment) RequestAssembly {
	total := len(history)
	for _, segment := range segments {
		if !isRequestOnlyAfterHistorySegment(segment) {
			continue
		}
		total += len(segment.Messages)
	}
	messages := make([]providers.ChatMessage, 0, total)
	messages = append(messages, history...)
	requestOnly := make([]providers.ChatMessage, 0, total-len(history))
	usedSegments := make([]ContextSegment, 0, len(segments))
	for _, segment := range segments {
		if !isRequestOnlyAfterHistorySegment(segment) {
			continue
		}
		normalized := normalizeRequestOnlyMessages(segment.Messages)
		if len(normalized) == 0 {
			continue
		}
		segment.Messages = normalized
		messages = append(messages, normalized...)
		requestOnly = append(requestOnly, normalized...)
		usedSegments = append(usedSegments, segment)
	}
	return RequestAssembly{
		Messages:            messages,
		RequestOnlyMessages: requestOnly,
		Segments:            usedSegments,
	}
}

func requestContextSegments(provider func() []ContextSegment) []ContextSegment {
	if provider == nil {
		return nil
	}
	return provider()
}

func normalizeRequestOnlyMessages(messages []providers.ChatMessage) []providers.ChatMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]providers.ChatMessage, 0, len(messages))
	for _, msg := range messages {
		if strings.TrimSpace(msg.Role) == "" {
			msg.Role = "user"
		}
		msg.Hidden = true
		out = append(out, msg)
	}
	return out
}

func requestOnlyMessagesFromBlocks(blocks []wuucontext.Block) []providers.ChatMessage {
	if len(blocks) == 0 {
		return nil
	}
	counts := make(map[string]int, len(blocks))
	messages := make([]providers.ChatMessage, 0, len(blocks))
	for _, block := range blocks {
		rendered := wuucontext.CompileBlocks([]wuucontext.Block{block})
		if strings.TrimSpace(rendered) == "" {
			continue
		}
		name := wuucontext.SystemReminderBlockMessageName(block, 0)
		ordinal := counts[name]
		counts[name] = ordinal + 1
		if ordinal > 0 {
			name = wuucontext.SystemReminderBlockMessageName(block, ordinal)
		}
		messages = append(messages, providers.ChatMessage{
			Role:    "user",
			Name:    name,
			Content: "<system-reminder>\n" + rendered + "\n</system-reminder>",
			Hidden:  true,
		})
	}
	return messages
}

func isRequestOnlyAfterHistorySegment(segment ContextSegment) bool {
	return segment.Lifecycle == ContextSegmentRequestOnly &&
		segment.Placement == ContextSegmentAfterHistory &&
		segment.CachePolicy == ContextSegmentVolatile &&
		!segment.Durable &&
		!segment.VisibleInUI
}
