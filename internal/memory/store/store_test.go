package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFileProvider_New_RejectsEmptyDir(t *testing.T) {
	if _, err := NewFileProvider(""); err == nil {
		t.Fatal("expected error for empty dir, got nil")
	}
}

func TestFileProvider_New_AcceptsDir(t *testing.T) {
	fp, err := NewFileProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	if fp.Dir() == "" {
		t.Fatal("Dir() should be non-empty after New")
	}
	if fp.Name() != "file" {
		t.Errorf("Name() = %q, want %q", fp.Name(), "file")
	}
}

func TestFileProvider_Available_OK(t *testing.T) {
	fp, err := NewFileProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := fp.Available(context.Background()); err != nil {
		t.Errorf("Available: %v", err)
	}
}

func TestEntryAndSourceConstants(t *testing.T) {
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

func TestFileProvider_Store_AssignsIDAndPersists(t *testing.T) {
	dir := t.TempDir()
	fp, err := NewFileProvider(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	id, err := fp.Store(ctx, Entry{Content: "remember this", Source: SourceUser})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if id == "" {
		t.Fatal("Store returned empty ID")
	}
	// Duplicate explicit ID must fail.
	if _, err := fp.Store(ctx, Entry{ID: id, Content: "again"}); !errors.Is(err, ErrDuplicateID) {
		t.Errorf("explicit duplicate ID: got %v, want ErrDuplicateID", err)
	}
	// Empty content must fail.
	if _, err := fp.Store(ctx, Entry{Content: "   "}); err == nil {
		t.Error("empty content should fail")
	}
}

func TestFileProvider_Recall_NewestFirst_AndTags(t *testing.T) {
	fp, err := NewFileProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Insert in known time order so UpdatedAt ordering is deterministic.
	now := time.Now().UTC()
	e1 := Entry{Content: "a", Source: SourceUser, Tags: []string{"x"}, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)}
	e2 := Entry{Content: "b", Source: SourceUser, Tags: []string{"y"}, CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour)}
	e3 := Entry{Content: "c", Source: SourceUser, Tags: []string{"x", "y"}, CreatedAt: now, UpdatedAt: now}
	for _, e := range []Entry{e1, e2, e3} {
		if _, err := fp.Store(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := fp.Recall(ctx, RecallQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("Recall: got %d entries, want 3", len(got))
	}
	if got[0].Content != "c" || got[1].Content != "b" || got[2].Content != "a" {
		t.Errorf("Recall ordering wrong: %q,%q,%q", got[0].Content, got[1].Content, got[2].Content)
	}
	// Tag filter: AND semantics.
	got, err = fp.Recall(ctx, RecallQuery{Tags: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("tag=x: got %d entries, want 2", len(got))
	}
	got, err = fp.Recall(ctx, RecallQuery{Tags: []string{"x", "y"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "c" {
		t.Errorf("tag=x,y: got %+v, want only c", got)
	}
	// Limit clamps result count.
	got, err = fp.Recall(ctx, RecallQuery{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "c" {
		t.Errorf("limit=1: got %+v, want only c", got)
	}
}

func TestFileProvider_Search_SubstringCaseInsensitive(t *testing.T) {
	fp, err := NewFileProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, c := range []string{"Project Atlas: API design", "totally unrelated", "atlas runway", "Project Atlas: UI"} {
		if _, err := fp.Store(ctx, Entry{Content: c, Source: SourceUser}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := fp.Search(ctx, SearchQuery{Text: "atlas"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("search 'atlas': got %d entries, want 3", len(got))
	}
	got, err = fp.Search(ctx, SearchQuery{Text: "UNRELATED"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("case-insensitive search: got %d entries, want 1", len(got))
	}
}

func TestFileProvider_Delete_HidesButPersists(t *testing.T) {
	dir := t.TempDir()
	fp, err := NewFileProvider(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, err := fp.Store(ctx, Entry{Content: "to forget", Source: SourceUser})
	if err != nil {
		t.Fatal(err)
	}
	if err := fp.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Re-delete is NotFound.
	if err := fp.Delete(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("re-delete: got %v, want ErrNotFound", err)
	}
	// Search and Recall must not surface the deleted entry.
	got, _ := fp.Search(ctx, SearchQuery{Text: "forget"})
	if len(got) != 0 {
		t.Errorf("deleted entry leaked into Search: %+v", got)
	}
	got, _ = fp.Recall(ctx, RecallQuery{})
	if len(got) != 0 {
		t.Errorf("deleted entry leaked into Recall: %+v", got)
	}
	// Reopen to confirm the tombstone survives a reload.
	fp2, err := NewFileProvider(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := fp2.Delete(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("after reload, delete: got %v, want ErrNotFound", err)
	}
}

func TestFileProvider_ReloadReplaysLog(t *testing.T) {
	dir := t.TempDir()
	fp, err := NewFileProvider(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	want := []string{"alpha", "beta", "gamma"}
	for _, w := range want {
		if _, err := fp.Store(ctx, Entry{Content: w, Source: SourceUser}); err != nil {
			t.Fatal(err)
		}
	}
	// Reopen from the same dir; we must observe the same entries.
	fp2, err := NewFileProvider(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fp2.Recall(ctx, RecallQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("after reload: got %d entries, want %d", len(got), len(want))
	}
	// Ordering check: must contain all three values regardless of order.
	seen := map[string]bool{}
	for _, e := range got {
		seen[e.Content] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("missing %q after reload", w)
		}
	}
}

func TestFileProvider_StoreConcurrent_NoDataRace(t *testing.T) {
	fp, err := NewFileProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if _, err := fp.Store(ctx, Entry{Content: "msg", Source: SourceUser, Tags: []string{"worker"}}); err != nil {
				t.Errorf("worker %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	got, err := fp.Recall(ctx, RecallQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Errorf("concurrent store: got %d entries, want %d", len(got), n)
	}
	// Tags filter still works after concurrent writes.
	got, err = fp.Recall(ctx, RecallQuery{Tags: []string{"worker"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Errorf("concurrent store w/ tag: got %d entries, want %d", len(got), n)
	}
	// Order must be deterministic (newest first, ID tiebreaker).
	sorted := sort.SliceIsSorted(got, func(i, j int) bool {
		if got[i].UpdatedAt.Equal(got[j].UpdatedAt) {
			return got[i].ID < got[j].ID
		}
		return got[i].UpdatedAt.After(got[j].UpdatedAt)
	})
	if !sorted {
		t.Error("query result not sorted by UpdatedAt desc, ID asc")
	}
}
