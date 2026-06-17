package guardian

import (
	"context"
	"testing"
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
