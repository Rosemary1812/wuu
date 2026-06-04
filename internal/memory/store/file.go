package store

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// entriesFile is the on-disk file name for the append-only JSONL log.
// It is package-private; production code must not parse this file
// directly. Use a FileProvider (or another Provider) to read state.
const entriesFile = "entries.jsonl"

// logRecord is the on-disk wrapper around Entry. The Deleted field
// marks a tombstone line in the log; otherwise the line is a normal
// entry. We do not expose Deleted on the public Entry type because
// "deleted" is a Provider-level concern, not a property of the
// record itself.
type logRecord struct {
	Entry
	Deleted bool `json:"_deleted,omitempty"`
}

// NewFileProvider creates a FileProvider rooted at dir. The directory
// is created if missing, and any existing entries.jsonl is replayed
// into memory.
//
// Passing an empty dir is an error; the caller is expected to compute
// a path (typically under the workspace state directory) before
// calling.
//
// A single process-wide FileProvider is the intended use. Two wuu
// processes sharing a directory will race on the log; the mutex here
// serializes only within one process.
func NewFileProvider(dir string) (*FileProvider, error) {
	if dir == "" {
		return nil, errors.New("memory store: dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("memory store: create dir: %w", err)
	}
	fp := &FileProvider{
		dir:        dir,
		entries:    make(map[ID]*Entry),
		tombstones: make(map[ID]struct{}),
	}
	if err := fp.load(); err != nil {
		return nil, fmt.Errorf("memory store: load existing entries: %w", err)
	}
	return fp, nil
}

// logPath returns the full path to the JSONL log file.
func (f *FileProvider) logPath() string { return filepath.Join(f.dir, entriesFile) }

// load replays the on-disk log into the in-memory index. It locks
// the Provider for the duration of the read; subsequent calls are
// no-ops once loaded is true.
func (f *FileProvider) load() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loadLocked()
}

// loadLocked reads the log and rebuilds the in-memory index. It
// must be called with f.mu held. Malformed lines are skipped (not
// fatal) so a partial corruption does not brick the whole store.
func (f *FileProvider) loadLocked() error {
	file, err := os.Open(f.logPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			f.loaded = true
			return nil
		}
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	// Allow long lines for entries with verbose content. 16 MiB is
	// far above any reasonable single memory entry; the cap exists
	// to bound memory under hostile input.
	scanner.Buffer(make([]byte, 0, 1<<16), 1<<24)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec logRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			// Skip malformed lines; do not fail the whole load.
			continue
		}
		if rec.ID == "" {
			continue
		}
		// Copy out of the scanner buffer; it is reused on the next
		// iteration and any pointer we hand out would otherwise be
		// clobbered.
		cp := rec.Entry
		f.entries[cp.ID] = &cp
		if rec.Deleted {
			f.tombstones[cp.ID] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	f.loaded = true
	return nil
}

// Available reports whether the backend is ready to serve. It is
// cheap on a warm cache (just a mutex check) and triggers a load
// on first call after construction. We do not probe-write to verify
// writability here; the next Store will fail with a clear error if
// the dir is read-only, and probe-writing on every health check would
// be noisy and slow.
func (f *FileProvider) Available(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loaded {
		return nil
	}
	return f.loadLocked()
}

// Store inserts a new entry. If entry.ID is empty, the Provider
// assigns one. If entry.ID is non-empty and already exists,
// ErrDuplicateID is returned. CreatedAt and UpdatedAt are filled in
// if zero; Source defaults to SourceSystem if empty.
func (f *FileProvider) Store(_ context.Context, entry Entry) (ID, error) {
	if strings.TrimSpace(entry.Content) == "" {
		return "", errors.New("memory store: entry content is empty")
	}
	if entry.Source == "" {
		entry.Source = SourceSystem
	}
	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now

	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.loaded {
		if err := f.loadLocked(); err != nil {
			return "", err
		}
	}
	if entry.ID == "" {
		entry.ID = f.newID(now)
	} else {
		if _, exists := f.entries[entry.ID]; exists {
			return "", ErrDuplicateID
		}
	}
	cp := entry
	f.entries[entry.ID] = &cp
	if err := f.appendLocked(&logRecord{Entry: cp}); err != nil {
		// Roll back the in-memory insert so state stays consistent.
		delete(f.entries, entry.ID)
		return "", err
	}
	return entry.ID, nil
}

// Recall returns entries matching the index-only query, newest first.
// Tombstoned entries are filtered out. If the caller supplied a
// non-empty Tags list, only entries that carry ALL those tags are
// returned (logical AND).
func (f *FileProvider) Recall(_ context.Context, q RecallQuery) ([]Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.loaded {
		if err := f.loadLocked(); err != nil {
			return nil, err
		}
	}
	return f.queryLocked(func(e *Entry) bool {
		if e.ID == "" {
			return false
		}
		if _, gone := f.tombstones[e.ID]; gone {
			return false
		}
		if !q.Since.IsZero() && e.CreatedAt.Before(q.Since) {
			return false
		}
		if !tagsMatch(e.Tags, q.Tags) {
			return false
		}
		return true
	}, q.Limit), nil
}

// Search returns entries whose Content contains the query text (and
// that match the tag filter, if any). Matching is case-insensitive
// substring. Results are returned newest-first; ranking by relevance
// is out of scope for the in-tree backend.
func (f *FileProvider) Search(_ context.Context, q SearchQuery) ([]Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.loaded {
		if err := f.loadLocked(); err != nil {
			return nil, err
		}
	}
	needle := strings.ToLower(strings.TrimSpace(q.Text))
	return f.queryLocked(func(e *Entry) bool {
		if e.ID == "" {
			return false
		}
		if _, gone := f.tombstones[e.ID]; gone {
			return false
		}
		if !tagsMatch(e.Tags, q.Tags) {
			return false
		}
		if !q.Since.IsZero() && e.CreatedAt.Before(q.Since) {
			return false
		}
		if needle != "" && !strings.Contains(strings.ToLower(e.Content), needle) {
			return false
		}
		return true
	}, q.Limit), nil
}

// Delete removes the entry with the given ID. Returns ErrNotFound if
// no such entry exists or if the entry was already deleted. The
// entry's history is preserved in the log as a tombstone line so a
// future provider can reconstruct it (or audit deletes).
func (f *FileProvider) Delete(_ context.Context, id ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.loaded {
		if err := f.loadLocked(); err != nil {
			return err
		}
	}
	e, ok := f.entries[id]
	if !ok {
		return ErrNotFound
	}
	if _, gone := f.tombstones[id]; gone {
		return ErrNotFound
	}
	f.tombstones[id] = struct{}{}
	rec := &logRecord{Entry: *e, Deleted: true}
	if err := f.appendLocked(rec); err != nil {
		// Roll back so the in-memory and on-disk states agree.
		delete(f.tombstones, id)
		return err
	}
	return nil
}

// queryLocked applies keep to the index, sorts the survivors by
// UpdatedAt desc (ID asc as a tiebreaker), and applies the optional
// limit. Caller must hold f.mu.
func (f *FileProvider) queryLocked(keep func(*Entry) bool, limit int) []Entry {
	out := make([]Entry, 0, len(f.entries))
	for _, e := range f.entries {
		if keep(e) {
			out = append(out, *e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// appendLocked serializes rec as a single JSONL line, appends it to
// the log, and fsyncs the file so a kernel crash cannot lose the
// write. Caller must hold f.mu. We deliberately do not use
// temp-file + rename for appends because the file is not being
// replaced — durability is the only invariant we need.
func (f *FileProvider) appendLocked(rec *logRecord) error {
	path := f.logPath()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

// tagsMatch reports whether the entry's tags satisfy the filter.
// An empty filter matches everything; a non-empty filter requires
// every wanted tag to be present (logical AND).
func tagsMatch(have, want []string) bool {
	if len(want) == 0 {
		return true
	}
	idx := make(map[string]struct{}, len(have))
	for _, t := range have {
		idx[t] = struct{}{}
	}
	for _, t := range want {
		if _, ok := idx[t]; !ok {
			return false
		}
	}
	return true
}

// newID returns a new entry ID. The prefix "mem_" disambiguates
// memory IDs from tool result IDs in logs. The timestamp prefix
// keeps IDs roughly sortable by creation order; the random suffix
// prevents collision under bursty concurrent stores.
func (f *FileProvider) newID(now time.Time) ID {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fall back to a time-only ID. Collision is possible under
		// a worst-case rand failure, but the worst outcome is a
		// duplicate-ID error the caller can retry around.
		return ID(fmt.Sprintf("mem_%x", now.UnixNano()))
	}
	return ID(fmt.Sprintf("mem_%x_%s", now.UnixNano(), hex.EncodeToString(buf[:])))
}

// Compile-time check that FileProvider satisfies Provider. If the
// interface drifts, this file will fail to build, which is the
// correct signal. It lives next to the methods so reviewers see the
// contract binding at a glance.
var _ Provider = (*FileProvider)(nil)
