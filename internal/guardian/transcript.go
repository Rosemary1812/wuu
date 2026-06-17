// Package guardian implements an LLM-driven tool approval reviewer that
// mirrors Codex's "auto_review" mode (see thirdparty/codex/codex-rs/core/src/guardian).
//
// The reviewer reuses the wuu providers package to ask the model whether a
// pending tool call should be approved, given the recent transcript context.
// This file owns the transcript plumbing only; the prompt template, the
// circuit breaker, and the provider-backed reviewer live in sibling files.
package guardian

import (
	"context"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// TranscriptRole tags the source of a transcript entry passed to the reviewer.
// It mirrors the role taxonomy used by providers.ChatMessage so callers can
// construct entries from an existing conversation history without remapping.
type TranscriptRole string

const (
	TranscriptRoleUser      TranscriptRole = "user"
	TranscriptRoleAssistant TranscriptRole = "assistant"
	TranscriptRoleTool      TranscriptRole = "tool"
)

// TranscriptEntry is a single pre-truncated, redacted conversation fragment
// that the reviewer uses to understand user intent. Callers are expected to
// have already truncated and redacted sensitive data before handing the entry
// over; the reviewer will not run additional sanitisation.
type TranscriptEntry struct {
	Role    TranscriptRole
	Content string
}

// Transcript is the recent conversation snapshot hung off the request context.
// Callers populate it before invoking a tool that may require approval so the
// reviewer can judge intent in context.
type Transcript struct {
	Entries []TranscriptEntry
}

// transcriptKey is unexported so callers cannot fabricate or read it via
// context.WithValue collisions with foreign packages.
type transcriptKey struct{}

// WithTranscript returns a copy of ctx that carries the supplied transcript.
// A nil Entries slice is normalised to an empty slice so downstream consumers
// can iterate without nil checks.
func WithTranscript(ctx context.Context, t Transcript) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if t.Entries == nil {
		t.Entries = []TranscriptEntry{}
	}
	return context.WithValue(ctx, transcriptKey{}, t)
}

// TranscriptFromContext returns the transcript attached to ctx, if any.
// The bool reports whether a transcript was present so callers can decide
// whether to proceed without one (e.g. fall back to a more conservative
// default behaviour).
func TranscriptFromContext(ctx context.Context) (Transcript, bool) {
	if ctx == nil {
		return Transcript{}, false
	}
	t, ok := ctx.Value(transcriptKey{}).(Transcript)
	if !ok {
		return Transcript{}, false
	}
	if t.Entries == nil {
		t.Entries = []TranscriptEntry{}
	}
	return t, true
}

// TranscriptFromChatMessages converts a chat history into a Transcript
// suitable for the guardian reviewer. The function drops messages whose
// role is unknown to the reviewer (system, custom agent roles, ...) and
// skips assistant tool-call messages whose Content is empty — those
// carry the action metadata in providers.ToolCall, which the reviewer
// already receives via the request side. Tool result messages with
// non-empty Content are kept so the reviewer can see what the model
// has already learned.
//
// The returned Transcript is then passed through truncateTranscript in
// BuildPrompt, so per-entry and total length caps still apply.
func TranscriptFromChatMessages(messages []providers.ChatMessage) Transcript {
	if len(messages) == 0 {
		return Transcript{Entries: []TranscriptEntry{}}
	}
	entries := make([]TranscriptEntry, 0, len(messages))
	for _, msg := range messages {
		role := transcriptRoleForMessage(msg.Role)
		if role == "" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		entries = append(entries, TranscriptEntry{
			Role:    role,
			Content: content,
		})
	}
	return Transcript{Entries: entries}
}

func transcriptRoleForMessage(role string) TranscriptRole {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		return TranscriptRoleUser
	case "assistant":
		return TranscriptRoleAssistant
	case "tool":
		return TranscriptRoleTool
	default:
		return ""
	}
}
