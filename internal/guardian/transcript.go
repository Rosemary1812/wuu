// Package guardian implements an LLM-driven tool approval reviewer that
// mirrors Codex's "auto_review" mode (see thirdparty/codex/codex-rs/core/src/guardian).
//
// The reviewer reuses the wuu providers package to ask the model whether a
// pending tool call should be approved, given the recent transcript context.
// This file owns the transcript plumbing only; the prompt template, the
// circuit breaker, and the provider-backed reviewer live in sibling files.
package guardian

import "context"

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
