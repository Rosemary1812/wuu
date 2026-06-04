package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFileProvider_New_RejectsEmptyDir(t *testing.T) {
	if _, err := NewFileProvider(""); err == nil {
		t.Fatal("expected error for empty dir, got nil")
	}
}

func TestFileProvider_New_AcceptsDir(t *testing.T) {
	fp, err := NewFileProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileMemory: %v", err)
	}
	if fp.Dir() == "" {
		t.Fatal("Dir() should be non-empty after New")
	}
	if fp.Name() != "file" {
		t.Errorf("Name() = %q, want %q", fp.Name(), "file")
	}
}

func TestFileProvider_StubMethodsReturnErrNotImplemented(t *testing.T) {
	fp, err := NewFileProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := fp.Available(ctx); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Available: got %v, want ErrNotImplemented", err)
	}
	if _, err := fp.Store(ctx, Entry{Content: "x"}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Store: got %v, want ErrNotImplemented", err)
	}
	if _, err := fp.Recall(ctx, RecallQuery{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Recall: got %v, want ErrNotImplemented", err)
	}
	if _, err := fp.Search(ctx, SearchQuery{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Search: got %v, want ErrNotImplemented", err)
	}
	if err := fp.Delete(ctx, ID("nope")); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Delete: got %v, want ErrNotImplemented", err)
	}
}

func TestEntryAndSourceConstants(t *testing.T) {
	// Source values are conventions used by the in-tree tools. Lock
	// them down so a silent rename does not break stored data.
	want := map[Source]string{
		SourceUser:      "user",
		SourceAssistant: "assistant",
		SourceTool:      "tool",
		SourceSystem:    "system",
		SourceImport:    "import",
	}
	for s, v := range want {
		if string(s) != v {
			t.Errorf("Source %q != %q", string(s), v)
		}
	}
}

func TestSentinelErrors_AreDistinct(t *testing.T) {
	// Each sentinel must be a distinct value so callers can branch
	// on errors.Is without false positives.
	if errors.Is(ErrNotImplemented, ErrNotFound) {
		t.Error("ErrNotImplemented and ErrNotFound must be distinct")
	}
	if errors.Is(ErrNotImplemented, ErrDuplicateID) {
		t.Error("ErrNotImplemented and ErrDuplicateID must be distinct")
	}
	if !strings.Contains(ErrNotImplemented.Error(), "not implemented") {
		t.Errorf("ErrNotImplemented message should describe itself, got %q", ErrNotImplemented)
	}
	if !strings.Contains(ErrNotFound.Error(), "not found") {
		t.Errorf("ErrNotFound message should describe itself, got %q", ErrNotFound)
	}
	if !strings.Contains(ErrDuplicateID.Error(), "duplicate") {
		t.Errorf("ErrDuplicateID message should describe itself, got %q", ErrDuplicateID)
	}
}

// TestFileProvider_SatisfiesProvider is a static guarantee. The
// compile-time check at the bottom of store.go is the real test; this
// exists so the constraint is also visible to a reader of the test
// file.
func TestFileProvider_SatisfiesProvider(t *testing.T) {
	var _ Provider = (*FileProvider)(nil)
}
