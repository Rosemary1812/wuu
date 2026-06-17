package guardian

import (
	"context"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestWithTranscriptStoresEntries(t *testing.T) {
	ctx := WithTranscript(context.Background(), Transcript{
		Entries: []TranscriptEntry{
			{Role: TranscriptRoleUser, Content: "hi"},
			{Role: TranscriptRoleAssistant, Content: "hello"},
		},
	})
	got, ok := TranscriptFromContext(ctx)
	if !ok {
		t.Fatal("expected transcript to be present")
	}
	if len(got.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(got.Entries))
	}
	if got.Entries[0].Role != TranscriptRoleUser || got.Entries[0].Content != "hi" {
		t.Fatalf("first entry = %+v", got.Entries[0])
	}
	if got.Entries[1].Role != TranscriptRoleAssistant || got.Entries[1].Content != "hello" {
		t.Fatalf("second entry = %+v", got.Entries[1])
	}
}

func TestTranscriptFromContextMissing(t *testing.T) {
	if _, ok := TranscriptFromContext(context.Background()); ok {
		t.Fatal("expected missing transcript to return ok=false")
	}
}

func TestTranscriptFromContextNilContext(t *testing.T) {
	//nolint:staticcheck // explicit nil-context test
	if _, ok := TranscriptFromContext(nil); ok {
		t.Fatal("expected nil ctx to return ok=false")
	}
}

func TestWithTranscriptNormalisesNilEntries(t *testing.T) {
	ctx := WithTranscript(context.Background(), Transcript{})
	got, ok := TranscriptFromContext(ctx)
	if !ok {
		t.Fatal("expected transcript to be present")
	}
	if got.Entries == nil {
		t.Fatal("Entries should be normalised to a non-nil slice")
	}
	if len(got.Entries) != 0 {
		t.Fatalf("Entries len = %d, want 0", len(got.Entries))
	}
}

func TestWithTranscriptNilContext(t *testing.T) {
	//nolint:staticcheck // explicit nil-context test
	ctx := WithTranscript(nil, Transcript{Entries: []TranscriptEntry{{Role: TranscriptRoleUser}}})
	if ctx == nil {
		t.Fatal("WithTranscript(nil,...) should return a usable context")
	}
	got, ok := TranscriptFromContext(ctx)
	if !ok {
		t.Fatal("expected transcript to be present")
	}
	if len(got.Entries) != 1 {
		t.Fatalf("Entries len = %d, want 1", len(got.Entries))
	}
}

func TestTranscriptFromChatMessages(t *testing.T) {
	cases := []struct {
		name string
		in   []providers.ChatMessage
		want []TranscriptEntry
	}{
		{
			name: "empty input",
			in:   nil,
			want: []TranscriptEntry{},
		},
		{
			name: "user and assistant kept",
			in: []providers.ChatMessage{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "world"},
			},
			want: []TranscriptEntry{
				{Role: TranscriptRoleUser, Content: "hello"},
				{Role: TranscriptRoleAssistant, Content: "world"},
			},
		},
		{
			name: "system role dropped",
			in: []providers.ChatMessage{
				{Role: "system", Content: "you are a coding agent"},
				{Role: "user", Content: "hi"},
			},
			want: []TranscriptEntry{
				{Role: TranscriptRoleUser, Content: "hi"},
			},
		},
		{
			name: "tool-call-only assistant dropped",
			in: []providers.ChatMessage{
				{Role: "user", Content: "do it"},
				{Role: "assistant", Content: ""},
			},
			want: []TranscriptEntry{
				{Role: TranscriptRoleUser, Content: "do it"},
			},
		},
		{
			name: "tool result kept",
			in: []providers.ChatMessage{
				{Role: "user", Content: "do it"},
				{Role: "tool", Content: "ok done"},
			},
			want: []TranscriptEntry{
				{Role: TranscriptRoleUser, Content: "do it"},
				{Role: TranscriptRoleTool, Content: "ok done"},
			},
		},
		{
			name: "whitespace content dropped",
			in: []providers.ChatMessage{
				{Role: "user", Content: "   "},
				{Role: "assistant", Content: "\n\n"},
			},
			want: []TranscriptEntry{},
		},
		{
			name: "leading and trailing whitespace trimmed",
			in: []providers.ChatMessage{
				{Role: "user", Content: "  hello  "},
			},
			want: []TranscriptEntry{
				{Role: TranscriptRoleUser, Content: "hello"},
			},
		},
		{
			name: "case-insensitive role matching",
			in: []providers.ChatMessage{
				{Role: "USER", Content: "hi"},
				{Role: "Assistant", Content: "hello"},
			},
			want: []TranscriptEntry{
				{Role: TranscriptRoleUser, Content: "hi"},
				{Role: TranscriptRoleAssistant, Content: "hello"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TranscriptFromChatMessages(tc.in)
			if got.Entries == nil {
				t.Fatal("Entries should be non-nil even for empty input")
			}
			if len(got.Entries) != len(tc.want) {
				t.Fatalf("len = %d, want %d (%+v)", len(got.Entries), len(tc.want), got.Entries)
			}
			for i, w := range tc.want {
				if got.Entries[i].Role != w.Role || got.Entries[i].Content != w.Content {
					t.Fatalf("entry %d = %+v, want %+v", i, got.Entries[i], w)
				}
			}
		})
	}
}
