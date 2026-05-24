package session

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCreateAndList(t *testing.T) {
	dir := t.TempDir()
	s1, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s1.ID == s2.ID {
		t.Fatalf("expected unique IDs, got %q twice", s1.ID)
	}

	sessions, err := List(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestDirUsesUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUU_HOME", "")
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".wuu", "sessions")
	if got := Dir(home); got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
	if got := Dir(""); got != want {
		t.Fatalf("Dir(empty) = %q, want %q", got, want)
	}
}

func TestListForCWDFiltersSessions(t *testing.T) {
	dir := t.TempDir()
	cwdA := filepath.Join(t.TempDir(), "project-a")
	cwdB := filepath.Join(t.TempDir(), "project-b")

	if _, err := CreateWithMetadata(dir, "sess-a", cwdA); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateWithMetadata(dir, "sess-b", cwdB); err != nil {
		t.Fatal(err)
	}

	sessions, err := ListForCWD(dir, cwdA, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "sess-a" {
		t.Fatalf("unexpected scoped sessions: %+v", sessions)
	}

	recent, err := MostRecentForCWD(dir, cwdB)
	if err != nil {
		t.Fatal(err)
	}
	if recent != "sess-b" {
		t.Fatalf("MostRecentForCWD() = %q, want sess-b", recent)
	}
}

func TestCreateForkWithMetadataPersistsSource(t *testing.T) {
	dir := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "project")
	fork, err := CreateForkWithMetadata(dir, "forked", cwd, ForkMetadata{
		ForkedFromID:     "source",
		ForkedFromTurnID: "source-turn-0001",
		ForkedFromItemID: "source-turn-0001-item-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fork.ForkedFromID != "source" || fork.ForkedFromTurnID != "source-turn-0001" || fork.ForkedFromItemID != "source-turn-0001-item-2" {
		t.Fatalf("fork metadata not set on create: %+v", fork)
	}

	found, ok, err := Find(dir, "forked")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected fork in index")
	}
	if found.ForkedFromID != "source" || found.ForkedFromTurnID != "source-turn-0001" || found.ForkedFromItemID != "source-turn-0001-item-2" {
		t.Fatalf("fork metadata not persisted: %+v", found)
	}
}

func TestUpdateIndex(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateIndex(dir, s.ID, 42, "hello"); err != nil {
		t.Fatal(err)
	}
	sessions, err := List(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Entries != 42 || sessions[0].Summary != "hello" {
		t.Fatalf("update not persisted: %+v", sessions)
	}
}

func TestPinAndArchiveMetadata(t *testing.T) {
	dir := t.TempDir()
	first, err := CreateWithMetadata(dir, "first", "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateWithMetadata(dir, "second", "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}

	pinned, err := UpdatePinned(dir, first.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if pinned.PinnedAt == nil {
		t.Fatalf("expected pinned timestamp: %+v", pinned)
	}
	sessions, err := List(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != first.ID || sessions[1].ID != second.ID {
		t.Fatalf("expected pinned session first, got %+v", sessions)
	}

	archived, err := UpdateArchived(dir, first.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if archived.ArchivedAt == nil || archived.PinnedAt != nil {
		t.Fatalf("expected archived session to clear pin: %+v", archived)
	}

	found, ok, err := Find(dir, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || found.ArchivedAt == nil {
		t.Fatalf("expected archived metadata from Find, got ok=%v session=%+v", ok, found)
	}
}

// TestConcurrentCreateAndUpdate exercises the race fixed by withIndexLock:
// before the fix, UpdateIndex's read-modify-rewrite could clobber a Create
// that happened between the read and the rewrite.
func TestConcurrentCreateAndUpdate(t *testing.T) {
	dir := t.TempDir()

	// Seed with one session that UpdateIndex will target.
	seed, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}

	const newSessions = 50
	const updates = 50
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < newSessions; i++ {
			if _, err := Create(dir); err != nil {
				t.Errorf("Create: %v", err)
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < updates; i++ {
			if err := UpdateIndex(dir, seed.ID, i, ""); err != nil {
				t.Errorf("UpdateIndex: %v", err)
				return
			}
		}
	}()

	wg.Wait()

	sessions, err := List(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	expected := newSessions + 1 // seed + creates
	if len(sessions) != expected {
		t.Fatalf("expected %d sessions after concurrent work, got %d", expected, len(sessions))
	}

	// Verify no duplicate IDs (a symptom of torn writes).
	seen := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		if seen[s.ID] {
			t.Errorf("duplicate session ID in index: %s", s.ID)
		}
		seen[s.ID] = true
	}
}

func TestLockFileIsCreated(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".index.lock")); err != nil {
		t.Fatalf("expected lock file to exist: %v", err)
	}
}
