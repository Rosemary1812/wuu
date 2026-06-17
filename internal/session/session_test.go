package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
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
	setSessionUpdatedAt(t, dir, s.ID, time.Time{})
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
	if sessions[0].UpdatedAt.IsZero() {
		t.Fatalf("expected updated timestamp: %+v", sessions[0])
	}
}

func TestUpdateGeneratedTitle(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := UpdateGeneratedTitle(dir, s.ID, "Short title")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Short title" {
		t.Fatalf("title not returned: %+v", updated)
	}

	if _, err := UpdateGeneratedTitle(dir, s.ID, "Replacement"); err != nil {
		t.Fatal(err)
	}
	sessions, err := List(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Title != "Short title" {
		t.Fatalf("title should persist once: %+v", sessions)
	}
}

func TestListOrdersPinnedGroupsByActivity(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	older, err := CreateWithMetadata(dir, "older", "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := CreateWithMetadata(dir, "newer", "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	setSessionUpdatedAt(t, dir, older.ID, base)
	setSessionUpdatedAt(t, dir, newer.ID, base.Add(time.Hour))

	sessions, err := List(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != newer.ID || sessions[1].ID != older.ID {
		t.Fatalf("expected active sessions by updated_at desc, got %+v", sessions)
	}

	if _, err := UpdatePinned(dir, older.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdatePinned(dir, newer.ID, true); err != nil {
		t.Fatal(err)
	}
	sessions, err = List(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != newer.ID || sessions[1].ID != older.ID {
		t.Fatalf("expected pinned sessions by updated_at desc, got %+v", sessions)
	}
}

func TestListBackfillsActivityFromSessionFileModTime(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	cwd := "/tmp/project"
	writeLegacyIndex(t, dir, []Session{
		{ID: "older", CreatedAt: base.Add(-time.Hour), CWD: cwd},
		{ID: "newer", CreatedAt: base.Add(-time.Hour), CWD: cwd},
	})
	if err := os.WriteFile(FilePath(dir, "older"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(FilePath(dir, "newer"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(FilePath(dir, "older"), base, base); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(FilePath(dir, "newer"), base.Add(time.Hour), base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	sessions, err := List(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != "newer" || sessions[1].ID != "older" {
		t.Fatalf("expected sessions by file activity fallback, got %+v", sessions)
	}
	if sessions[0].UpdatedAt.IsZero() || sessions[1].UpdatedAt.IsZero() {
		t.Fatalf("expected updated_at fallback to be populated, got %+v", sessions)
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

func TestHistoryRecordsPersistInSQLite(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}

	if err := AppendHistoryRecord(dir, "thread-1", HistoryRecord{
		Role:      "assistant",
		Content:   "done",
		Phase:     "final_answer",
		ToolCalls: json.RawMessage(`[{"id":"call_1","name":"read_file","arguments":"{}"}]`),
		At:        time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendHistoryRecord(dir, "thread-1", HistoryRecord{
		Role:         "meta",
		Content:      "token_usage",
		InputTokens:  12,
		OutputTokens: 4,
	}); err != nil {
		t.Fatal(err)
	}

	visible, err := LoadHistoryRecords(dir, "thread-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].Role != "assistant" || visible[0].Phase != "final_answer" || string(visible[0].ToolCalls) == "" {
		t.Fatalf("unexpected visible history: %+v", visible)
	}

	all, err := LoadHistoryRecords(dir, "thread-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[1].Role != "meta" || all[1].InputTokens != 12 || all[1].OutputTokens != 4 {
		t.Fatalf("unexpected full history: %+v", all)
	}
}

func TestHistoryRecordOperationsRequireExistingSession(t *testing.T) {
	dir := t.TempDir()
	rec := HistoryRecord{Role: "user", Content: "hello"}

	if err := AppendHistoryRecord(dir, "missing-thread", rec); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("AppendHistoryRecord() error = %v, want ErrSessionNotFound", err)
	}
	if err := RewriteHistoryRecords(dir, "missing-thread", []HistoryRecord{rec}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("RewriteHistoryRecords() error = %v, want ErrSessionNotFound", err)
	}
	if _, err := LoadHistoryRecords(dir, "missing-thread", false); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("LoadHistoryRecords() error = %v, want ErrSessionNotFound", err)
	}
}

func TestRewriteHistoryRecordsReplacesExistingMessages(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	if err := AppendHistoryRecord(dir, "thread-1", HistoryRecord{Role: "user", Content: "old user"}); err != nil {
		t.Fatal(err)
	}
	if err := AppendHistoryRecord(dir, "thread-1", HistoryRecord{Role: "assistant", Content: "old assistant"}); err != nil {
		t.Fatal(err)
	}

	if err := RewriteHistoryRecords(dir, "thread-1", []HistoryRecord{{Role: "assistant", Content: "new assistant"}}); err != nil {
		t.Fatal(err)
	}

	history, err := LoadHistoryRecords(dir, "thread-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Role != "assistant" || history[0].Content != "new assistant" {
		t.Fatalf("rewrite should replace old history, got %+v", history)
	}
}

func TestLegacyHistoryImportsIntoSQLite(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	writeLegacyIndex(t, dir, []Session{{
		ID:        "legacy-thread",
		CreatedAt: start,
		UpdatedAt: start,
		CWD:       "/tmp/project",
	}})
	writeLegacyHistory(t, FilePath(dir, "legacy-thread"), []HistoryRecord{
		{Role: "user", Content: "hello", At: start},
		{Role: "assistant", Content: "done", At: start.Add(time.Minute)},
	})

	sessions, err := List(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "legacy-thread" {
		t.Fatalf("legacy index not imported: %+v", sessions)
	}
	history, err := LoadHistoryRecords(dir, "legacy-thread", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Content != "hello" || history[1].Content != "done" {
		t.Fatalf("legacy history not imported: %+v", history)
	}
}

func TestLegacyHistoryImportSkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	writeLegacyIndex(t, dir, []Session{{
		ID:        "legacy-thread",
		CreatedAt: start,
		UpdatedAt: start,
		CWD:       "/tmp/project",
	}})
	first := mustMarshalJSONLine(t, HistoryRecord{Role: "user", Content: "hello", At: start})
	second := mustMarshalJSONLine(t, HistoryRecord{Role: "assistant", Content: "done", At: start.Add(time.Minute)})
	raw := append(append(append([]byte{}, first...), []byte("\n{not-json}\n")...), second...)
	raw = append(raw, '\n')
	if err := os.WriteFile(FilePath(dir, "legacy-thread"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	history, err := LoadHistoryRecords(dir, "legacy-thread", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Content != "hello" || history[1].Content != "done" {
		t.Fatalf("legacy import should skip corrupt lines and keep valid records, got %+v", history)
	}
}

func TestLegacyHistoryImportRunsOnce(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	writeLegacyIndex(t, dir, []Session{{
		ID:        "legacy-thread",
		CreatedAt: start,
		UpdatedAt: start,
		CWD:       "/tmp/project",
	}})
	writeLegacyHistory(t, FilePath(dir, "legacy-thread"), []HistoryRecord{
		{Role: "user", Content: "original", At: start},
	})
	history, err := LoadHistoryRecords(dir, "legacy-thread", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Content != "original" {
		t.Fatalf("unexpected initial import: %+v", history)
	}

	writeLegacyHistory(t, FilePath(dir, "legacy-thread"), []HistoryRecord{
		{Role: "user", Content: "changed after import", At: start.Add(time.Minute)},
	})
	history, err = LoadHistoryRecords(dir, "legacy-thread", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Content != "original" {
		t.Fatalf("legacy import should be idempotent after marker is written, got %+v", history)
	}
}

// TestConcurrentCreateAndUpdate exercises concurrent short writes against the
// SQLite store. Creates and metadata updates must not lose sessions.
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

func TestConcurrentHistoryAppendAndLoad(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}

	const appends = 64
	const readers = 4
	start := make(chan struct{})
	done := make(chan struct{})
	errs := make(chan error, appends+readers)

	var readerWG sync.WaitGroup
	readerWG.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer readerWG.Done()
			<-start
			for {
				select {
				case <-done:
					return
				default:
				}
				if _, err := LoadHistoryRecords(dir, "thread-1", false); err != nil {
					errs <- err
					return
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	var writerWG sync.WaitGroup
	writerWG.Add(appends)
	for i := 0; i < appends; i++ {
		i := i
		go func() {
			defer writerWG.Done()
			<-start
			if err := AppendHistoryRecord(dir, "thread-1", HistoryRecord{
				Role:    "user",
				Content: "message-" + strconv.Itoa(i),
			}); err != nil {
				errs <- err
			}
		}()
	}

	close(start)
	writerWG.Wait()
	close(done)
	readerWG.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	history, err := LoadHistoryRecords(dir, "thread-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != appends {
		t.Fatalf("expected %d appended records, got %d: %+v", appends, len(history), history)
	}
	seen := make(map[string]bool, appends)
	for _, rec := range history {
		if rec.Role != "user" {
			t.Fatalf("unexpected record role after concurrent append: %+v", rec)
		}
		if seen[rec.Content] {
			t.Fatalf("duplicate history content after concurrent append: %q", rec.Content)
		}
		seen[rec.Content] = true
	}
	for i := 0; i < appends; i++ {
		content := "message-" + strconv.Itoa(i)
		if !seen[content] {
			t.Fatalf("missing history content after concurrent append: %q", content)
		}
	}
}

func TestConcurrentHistoryRewriteAndAppend(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	if err := AppendHistoryRecord(dir, "thread-1", HistoryRecord{Role: "user", Content: "seed"}); err != nil {
		t.Fatal(err)
	}

	const appends = 32
	start := make(chan struct{})
	errs := make(chan error, appends+1)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		if err := RewriteHistoryRecords(dir, "thread-1", []HistoryRecord{{
			Role:    "assistant",
			Content: "rewritten",
		}}); err != nil {
			errs <- err
		}
	}()

	wg.Add(appends)
	for i := 0; i < appends; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			if err := AppendHistoryRecord(dir, "thread-1", HistoryRecord{
				Role:    "user",
				Content: "append-" + strconv.Itoa(i),
			}); err != nil {
				errs <- err
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	history, err := LoadHistoryRecords(dir, "thread-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) < 1 || len(history) > appends+1 {
		t.Fatalf("unexpected history length after concurrent rewrite/append: %d %+v", len(history), history)
	}
	var rewritten int
	seen := make(map[string]bool, len(history))
	for _, rec := range history {
		if rec.Content == "seed" {
			t.Fatalf("seed record should not survive rewrite: %+v", history)
		}
		if rec.Content == "rewritten" {
			rewritten++
			continue
		}
		if seen[rec.Content] {
			t.Fatalf("duplicate appended content after concurrent rewrite/append: %q", rec.Content)
		}
		seen[rec.Content] = true
	}
	if rewritten != 1 {
		t.Fatalf("expected one rewritten record, got %d in %+v", rewritten, history)
	}
}

func TestSQLiteDatabaseIsCreated(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(DBPath(dir)); err != nil {
		t.Fatalf("expected sqlite database to exist: %v", err)
	}
}

func setSessionUpdatedAt(t *testing.T, dir, id string, at time.Time) {
	t.Helper()
	if _, err := updateMetadata(dir, id, false, func(s *Session) {
		s.UpdatedAt = at
	}); err != nil {
		t.Fatal(err)
	}
}

func writeLegacyIndex(t *testing.T, dir string, sessions []Session) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(IndexPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, sess := range sessions {
		if err := enc.Encode(sess); err != nil {
			t.Fatal(err)
		}
	}
}

func mustMarshalJSONLine(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeLegacyHistory(t *testing.T, path string, records []HistoryRecord) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			t.Fatal(err)
		}
	}
}
