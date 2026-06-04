// Package store is the LLM-writable memory store for wuu.
//
// It is intentionally separate from the sibling memory package
// (internal/memory), which discovers and loads read-only project and
// user memory files (AGENTS.md, CLAUDE.md, ...) for system-prompt
// injection. The store here is the LLM-curated, append-friendly memory
// the model can read from and write to during a session.
//
// The first and only supported backend is file-backed: entries live as
// a JSONL log under a workspace-local directory, with an in-memory
// index for fast recall. New backends (e.g. a remote memory service)
// implement the Provider interface and slot in via Env.
package store

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ID uniquely identifies a memory entry within a Provider. IDs are
// stable across reads of the same entry; callers use them to update or
// delete specific records.
type ID string

// Source describes where an entry originated. The Provider does not
// interpret this — it is stored as-is and returned to the caller so
// downstream tools can filter or display accordingly.
type Source string

// Known source values. Providers are not required to use these; they
// are the conventions used by the in-tree tools.
const (
	SourceUser      Source = "user"
	SourceAssistant Source = "assistant"
	SourceTool      Source = "tool"
	SourceSystem    Source = "system"
	SourceImport    Source = "import"
)

// Entry is a single memory record. The Provider may assign its own ID
// when one is empty in Store; otherwise the caller-supplied ID is used
// (and a duplicate-ID error is returned if it already exists).
type Entry struct {
	ID        ID        `json:"id"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	Source    Source    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RecallQuery selects entries by recency and optional tag filter. It
// is the cheap, index-only path: no content scanning.
type RecallQuery struct {
	// Tags, if non-empty, restricts results to entries that carry ALL
	// listed tags (logical AND, not OR).
	Tags []string
	// Limit caps the number of returned entries. Zero or negative
	// means "use the provider default".
	Limit int
	// Since, if non-zero, restricts results to entries created at or
	// after this time.
	Since time.Time
}

// SearchQuery is the text-search path. Providers should perform
// substring or token matching over Content, in addition to any tag
// filter. Results are not required to be ranked; the in-tree File
// Provider returns matches in recency order.
type SearchQuery struct {
	// Text is matched against Content. Empty means "no text filter".
	Text string
	// Tags, if non-empty, restricts results to entries that carry ALL
	// listed tags.
	Tags []string
	// Since, if non-zero, restricts results to entries created at or
	// after this time.
	Since time.Time
	// Limit caps the number of returned entries.
	Limit int
}

// ErrNotImplemented is returned by Provider methods that are part of
// the interface but not yet implemented by a particular backend. It
// is distinct from generic errors so callers can decide whether to
// surface the gap to the user or fall back to another path.
var ErrNotImplemented = errors.New("memory store: not implemented")

// ErrNotFound is returned when an operation references an ID that
// does not exist in the Provider.
var ErrNotFound = errors.New("memory store: entry not found")

// ErrDuplicateID is returned by Store when the caller supplied an ID
// that already exists. Callers should retry without an ID to let the
// Provider assign one.
var ErrDuplicateID = errors.New("memory store: duplicate id")

// Provider is the contract every memory backend implements. The
// methods are safe for concurrent use; implementations serialize
// internal state as needed.
type Provider interface {
	// Name reports the backend identifier (e.g. "file"). Used in
	// logs and to disambiguate when multiple providers coexist.
	Name() string

	// Available reports whether the backend is ready to serve. It is
	// a cheap health check that callers may invoke before a Store or
	// Search to fail fast with a meaningful error.
	Available(ctx context.Context) error

	// Store inserts a new entry. If entry.ID is empty, the Provider
	// assigns one and returns it. If entry.ID is non-empty and already
	// exists, returns ErrDuplicateID. The Provider fills in CreatedAt
	// and UpdatedAt if they are zero.
	Store(ctx context.Context, entry Entry) (ID, error)

	// Recall returns entries matching the index-only query, in
	// recency order (newest first). An empty result is not an error.
	Recall(ctx context.Context, q RecallQuery) ([]Entry, error)

	// Search returns entries whose Content contains the query text
	// (and that match the tag filter, if any). Implementation-defined
	// ordering; the in-tree File Provider returns newest-first.
	Search(ctx context.Context, q SearchQuery) ([]Entry, error)

	// Delete removes the entry with the given ID. Returns
	// ErrNotFound if no such entry exists.
	Delete(ctx context.Context, id ID) error
}

// FileProvider is the in-tree, file-backed Provider. The real
// read/write/search/delete semantics live in file.go; this struct
// declaration is here so both files can extend it with methods.
type FileProvider struct {
	// dir is the on-disk root for memory state. It is created by
	// NewFileProvider (in file.go) if it does not yet exist.
	dir string

	// mu serializes all in-memory state and log writes. The Provider
	// is safe for concurrent use from multiple goroutines within a
	// single process. It does NOT coordinate across processes; if
	// two wuu processes share a directory they will race on the log.
	mu sync.Mutex

	// entries is the in-memory index. It includes tombstoned
	// entries; callers must filter via tombstones.
	entries map[ID]*Entry

	// tombstones tracks IDs that have been deleted. They are
	// preserved in the log for crash safety and audit.
	tombstones map[ID]struct{}

	// loaded reports whether the on-disk log has been replayed into
	// memory at least once. New writes call loadLocked on demand
	// so a fresh Provider with no prior reads still works.
	loaded bool
}

// Name reports the backend identifier.
func (f *FileProvider) Name() string { return "file" }

// Dir returns the on-disk root. It is exposed for diagnostics and
// tests; production code should not depend on the layout.
func (f *FileProvider) Dir() string { return f.dir }
