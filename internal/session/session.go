package session

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/statepath"
	_ "modernc.org/sqlite"
)

const (
	dbFileName = "sessions.sqlite3"
)

var ErrSessionNotFound = errors.New("session not found")

var (
	storeInitMu  sync.Mutex
	storeWriteMu sync.Mutex
)

// Session represents one conversation session.
type Session struct {
	ID               string     `json:"id"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at,omitempty"`
	Title            string     `json:"title,omitempty"`
	Summary          string     `json:"summary,omitempty"`
	Entries          int        `json:"entries"`
	CWD              string     `json:"cwd,omitempty"`
	ForkedFromID     string     `json:"forked_from_id,omitempty"`
	ForkedFromTurnID string     `json:"forked_from_turn_id,omitempty"`
	ForkedFromItemID string     `json:"forked_from_item_id,omitempty"`
	PinnedAt         *time.Time `json:"pinned_at,omitempty"`
	ArchivedAt       *time.Time `json:"archived_at,omitempty"`
}

type ForkMetadata struct {
	ForkedFromID     string
	ForkedFromTurnID string
	ForkedFromItemID string
}

// HistoryRecord is the durable session message shape. App-server remains
// responsible for provider-specific ChatMessage conversion.
type HistoryRecord struct {
	Role                string          `json:"role"`
	Content             string          `json:"content"`
	DisplayContent      string          `json:"display_content,omitempty"`
	Phase               string          `json:"phase,omitempty"`
	ProviderItemID      string          `json:"provider_item_id,omitempty"`
	ProviderItemModel   string          `json:"provider_item_model,omitempty"`
	ClientID            string          `json:"client_id,omitempty"`
	Hidden              bool            `json:"hidden,omitempty"`
	Steered             bool            `json:"steered,omitempty"`
	ReasoningContent    string          `json:"reasoning_content,omitempty"`
	ReasoningBlocks     json.RawMessage `json:"reasoning_blocks,omitempty"`
	Images              json.RawMessage `json:"images,omitempty"`
	Files               json.RawMessage `json:"files,omitempty"`
	ToolCalls           json.RawMessage `json:"tool_calls,omitempty"`
	DiscoveredTools     json.RawMessage `json:"discovered_tools,omitempty"`
	ToolCallID          string          `json:"tool_call_id,omitempty"`
	ToolResultKind      string          `json:"tool_result_kind,omitempty"`
	FinishReason        string          `json:"finish_reason,omitempty"`
	StopReason          string          `json:"stop_reason,omitempty"`
	Truncated           bool            `json:"truncated,omitempty"`
	Name                string          `json:"name,omitempty"`
	At                  time.Time       `json:"at,omitempty"`
	InputTokens         int             `json:"input_tokens,omitempty"`
	OutputTokens        int             `json:"output_tokens,omitempty"`
	CacheCreationTokens int             `json:"cache_creation_tokens,omitempty"`
	CacheReadTokens     int             `json:"cache_read_tokens,omitempty"`
	// Provider and Model carry which provider/model produced this token_usage
	// row. Empty for chat records and for legacy token_usage rows written
	// before this field existed; readers should treat empty as "unknown".
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// NewID generates a human-readable, sortable session ID: YYYYMMDD-HHMMSS-xxxxxxxxxxxxxxxx.
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// Dir returns the user-level sessions directory.
func Dir(homeDir string) string {
	home, err := statepath.Home(homeDir)
	if err != nil {
		return ""
	}
	return statepath.SessionsDir(home)
}

// DBPath returns the SQLite database path for session state.
func DBPath(sessDir string) string {
	return filepath.Join(sessDir, dbFileName)
}

// Create initializes a new session.
// If id is non-empty, it is used as the session ID; otherwise a new one is generated.
func Create(sessDir string, id ...string) (*Session, error) {
	sessID := ""
	if len(id) > 0 {
		sessID = id[0]
	}
	return CreateWithMetadata(sessDir, sessID, "")
}

// CreateWithMetadata initializes a new session with thread-level metadata.
func CreateWithMetadata(sessDir, id, cwd string) (*Session, error) {
	return createWithMetadata(sessDir, id, cwd, ForkMetadata{})
}

// CreateForkWithMetadata initializes a forked session with source metadata.
func CreateForkWithMetadata(sessDir, id, cwd string, fork ForkMetadata) (*Session, error) {
	return createWithMetadata(sessDir, id, cwd, fork)
}

func createWithMetadata(sessDir, id, cwd string, fork ForkMetadata) (*Session, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	sessID := NewID()
	if strings.TrimSpace(id) != "" {
		sessID = strings.TrimSpace(id)
	}

	now := time.Now().UTC()
	sess := &Session{
		ID:               sessID,
		CreatedAt:        now,
		UpdatedAt:        now,
		CWD:              normalizeCWD(cwd),
		ForkedFromID:     strings.TrimSpace(fork.ForkedFromID),
		ForkedFromTurnID: strings.TrimSpace(fork.ForkedFromTurnID),
		ForkedFromItemID: strings.TrimSpace(fork.ForkedFromItemID),
	}
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	if err := insertSession(db, *sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// List reads sessions and returns the most recent sessions (up to limit).
func List(sessDir string, limit int) ([]Session, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
SELECT id, created_at, updated_at, title, summary, entries, cwd,
       forked_from_id, forked_from_turn_id, forked_from_item_id,
       pinned_at, archived_at
FROM sessions`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan sessions: %w", err)
	}

	sortSessions(sessions)
	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}
	return sessions, nil
}

// ListForCWD reads sessions scoped to a workspace cwd.
func ListForCWD(sessDir, cwd string, limit int) ([]Session, error) {
	target := normalizeCWD(cwd)
	if target == "" {
		return List(sessDir, limit)
	}
	sessions, err := List(sessDir, 0)
	if err != nil {
		return nil, err
	}
	filtered := make([]Session, 0, len(sessions))
	for _, s := range sessions {
		if normalizeCWD(s.CWD) == target {
			filtered = append(filtered, s)
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

// Find returns metadata for a session ID.
func Find(sessDir, id string) (Session, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Session{}, false, nil
	}
	db, err := openStore(sessDir)
	if err != nil {
		return Session{}, false, err
	}
	defer db.Close()
	return findSessionDB(db, id)
}

// UpdateIndex updates the entries count and summary for a session.
func UpdateIndex(sessDir string, id string, entries int, summary string) error {
	now := time.Now().UTC()
	_, err := updateMetadata(sessDir, id, true, func(s *Session) {
		s.Entries = entries
		s.UpdatedAt = now
		if strings.TrimSpace(summary) != "" && strings.TrimSpace(s.Summary) == "" {
			s.Summary = summary
		}
	})
	return err
}

// UpdateGeneratedTitle sets a title only when the session does not already have one.
func UpdateGeneratedTitle(sessDir, id string, title string) (Session, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Session{}, fmt.Errorf("title is required")
	}
	return updateMetadata(sessDir, id, false, func(s *Session) {
		if strings.TrimSpace(s.Title) == "" {
			s.Title = title
		}
	})
}

// UpdateTitle overwrites a session title unconditionally. Differs from
// UpdateGeneratedTitle, which only fills an empty title. The right-click
// Rename menu uses UpdateTitle to overwrite both the auto-generated
// preview and any prior user-edited title.
func UpdateTitle(sessDir, id string, title string) (Session, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Session{}, fmt.Errorf("title is required")
	}
	return updateMetadata(sessDir, id, false, func(s *Session) {
		s.Title = title
	})
}

// UpdatePinned marks a session as pinned or unpinned.
func UpdatePinned(sessDir, id string, pinned bool) (Session, error) {
	now := time.Now().UTC()
	return updateMetadata(sessDir, id, false, func(s *Session) {
		if pinned {
			s.PinnedAt = &now
		} else {
			s.PinnedAt = nil
		}
	})
}

// UpdateArchived marks a session as archived or active.
func UpdateArchived(sessDir, id string, archived bool) (Session, error) {
	now := time.Now().UTC()
	return updateMetadata(sessDir, id, false, func(s *Session) {
		if archived {
			s.ArchivedAt = &now
			s.PinnedAt = nil
		} else {
			s.ArchivedAt = nil
		}
	})
}

// Delete removes a session and its durable history records.
func Delete(sessDir, id string) (Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Session{}, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	db, err := openStore(sessDir)
	if err != nil {
		return Session{}, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return Session{}, fmt.Errorf("begin session delete: %w", err)
	}
	defer tx.Rollback()

	deleted, ok, err := findSessionTx(tx, id)
	if err != nil {
		return Session{}, err
	}
	if !ok {
		return Session{}, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return Session{}, fmt.Errorf("delete session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Session{}, fmt.Errorf("commit session delete: %w", err)
	}
	return deleted, nil
}

func updateMetadata(sessDir, id string, missingOK bool, update func(*Session)) (Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		if missingOK {
			return Session{}, nil
		}
		return Session{}, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	db, err := openStore(sessDir)
	if err != nil {
		return Session{}, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return Session{}, fmt.Errorf("begin session update: %w", err)
	}
	defer tx.Rollback()

	s, ok, err := findSessionTx(tx, id)
	if err != nil {
		return Session{}, err
	}
	if !ok {
		if missingOK {
			return Session{}, nil
		}
		return Session{}, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	update(&s)
	if err := updateSessionTx(tx, s); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return Session{}, fmt.Errorf("commit session update: %w", err)
	}
	return s, nil
}

// MostRecent returns the most recent session ID, or empty string if none.
func MostRecent(sessDir string) (string, error) {
	sessions, err := List(sessDir, 1)
	if err != nil {
		return "", err
	}
	if len(sessions) == 0 {
		return "", nil
	}
	return sessions[0].ID, nil
}

// MostRecentForCWD returns the most recent session for a workspace cwd.
func MostRecentForCWD(sessDir, cwd string) (string, error) {
	sessions, err := ListForCWD(sessDir, cwd, 1)
	if err != nil {
		return "", err
	}
	if len(sessions) == 0 {
		return "", nil
	}
	return sessions[0].ID, nil
}

// AppendHistoryRecord appends one durable history record to a session.
func AppendHistoryRecord(sessDir, id string, rec HistoryRecord) error {
	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin history append: %w", err)
	}
	defer tx.Rollback()
	if ok, err := sessionExistsTx(tx, id); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	if err := appendHistoryRecordTx(tx, id, rec); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit history append: %w", err)
	}
	return nil
}

// RewriteHistoryRecords replaces a session's durable history records.
func RewriteHistoryRecords(sessDir, id string, records []HistoryRecord) error {
	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin history rewrite: %w", err)
	}
	defer tx.Rollback()
	if ok, err := sessionExistsTx(tx, id); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	if _, err := tx.Exec(`DELETE FROM session_messages WHERE session_id = ?`, id); err != nil {
		return fmt.Errorf("clear session history: %w", err)
	}
	for i, rec := range records {
		if err := insertHistoryRecordTx(tx, id, i+1, rec); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit history rewrite: %w", err)
	}
	return nil
}

// LoadHistoryRecords returns history records in write order.
func LoadHistoryRecords(sessDir, id string, includeMeta bool) ([]HistoryRecord, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if ok, err := sessionExistsDB(db, id); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	return loadHistoryRecordsDB(db, id, includeMeta)
}

func openStore(sessDir string) (*sql.DB, error) {
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(DBPath(sessDir)))
	if err != nil {
		return nil, fmt.Errorf("open sessions database: %w", err)
	}
	db.SetMaxOpenConns(1)
	storeInitMu.Lock()
	defer storeInitMu.Unlock()
	if err := configureDB(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func sqliteDSN(path string) string {
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	return u.String()
}

func configureDB(db *sql.DB) error {
	pragmas := []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = NORMAL`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
	}
	for _, stmt := range pragmas {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("configure sessions database: %w", err)
		}
	}
	return nil
}

func migrateSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			entries INTEGER NOT NULL DEFAULT 0,
			cwd TEXT NOT NULL DEFAULT '',
			forked_from_id TEXT NOT NULL DEFAULT '',
			forked_from_turn_id TEXT NOT NULL DEFAULT '',
			forked_from_item_id TEXT NOT NULL DEFAULT '',
			pinned_at TEXT,
			archived_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_cwd ON sessions(cwd)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at, id)`,
		`CREATE TABLE IF NOT EXISTS session_messages (
				session_id TEXT NOT NULL,
				seq INTEGER NOT NULL,
				role TEXT NOT NULL,
				content TEXT NOT NULL DEFAULT '',
				display_content TEXT NOT NULL DEFAULT '',
				phase TEXT NOT NULL DEFAULT '',
				provider_item_id TEXT NOT NULL DEFAULT '',
				provider_item_model TEXT NOT NULL DEFAULT '',
				client_id TEXT NOT NULL DEFAULT '',
				hidden INTEGER NOT NULL DEFAULT 0,
				steered INTEGER NOT NULL DEFAULT 0,
				reasoning_content TEXT NOT NULL DEFAULT '',
			reasoning_blocks_json TEXT NOT NULL DEFAULT '',
			images_json TEXT NOT NULL DEFAULT '',
			files_json TEXT NOT NULL DEFAULT '',
			tool_calls_json TEXT NOT NULL DEFAULT '',
			discovered_tools_json TEXT NOT NULL DEFAULT '',
			tool_call_id TEXT NOT NULL DEFAULT '',
			tool_result_kind TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			at TEXT,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			provider TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(session_id, seq),
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_session_messages_role ON session_messages(session_id, role, seq)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate sessions database: %w", err)
		}
	}
	if err := addColumnIfMissing(db, "session_messages", "phase", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "display_content", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "provider_item_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "provider_item_model", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "hidden", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "cache_creation_tokens", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "cache_read_tokens", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "tool_result_kind", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "discovered_tools_json", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "provider", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "model", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return nil
}

func addColumnIfMissing(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan %s columns: %w", table, err)
		}
		if strings.EqualFold(name, column) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan %s columns: %w", table, err)
	}
	if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, definition)); err != nil {
		return fmt.Errorf("add %s.%s column: %w", table, column, err)
	}
	return nil
}

func insertSession(db *sql.DB, sess Session) error {
	_, err := db.Exec(insertSessionSQL(""), sessionArgs(sess)...)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func insertSessionTx(tx *sql.Tx, sess Session, conflict string) error {
	_, err := tx.Exec(insertSessionSQL(conflict), sessionArgs(sess)...)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func insertSessionSQL(conflict string) string {
	conflict = strings.TrimSpace(conflict)
	if conflict != "" {
		conflict = " " + conflict
	}
	return `INSERT` + conflict + ` INTO sessions (
		id, created_at, updated_at, title, summary, entries, cwd,
		forked_from_id, forked_from_turn_id, forked_from_item_id,
		pinned_at, archived_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
}

func updateSessionTx(tx *sql.Tx, sess Session) error {
	_, err := tx.Exec(`
UPDATE sessions
SET created_at = ?, updated_at = ?, title = ?, summary = ?, entries = ?, cwd = ?,
    forked_from_id = ?, forked_from_turn_id = ?, forked_from_item_id = ?,
    pinned_at = ?, archived_at = ?
WHERE id = ?`,
		timeText(sess.CreatedAt), timeText(sess.UpdatedAt), sess.Title, sess.Summary, sess.Entries, normalizeCWD(sess.CWD),
		sess.ForkedFromID, sess.ForkedFromTurnID, sess.ForkedFromItemID,
		nullableTimeText(sess.PinnedAt), nullableTimeText(sess.ArchivedAt), sess.ID,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return nil
}

func sessionArgs(sess Session) []any {
	return []any{
		sess.ID,
		timeText(sess.CreatedAt),
		timeText(sess.UpdatedAt),
		sess.Title,
		sess.Summary,
		sess.Entries,
		normalizeCWD(sess.CWD),
		sess.ForkedFromID,
		sess.ForkedFromTurnID,
		sess.ForkedFromItemID,
		nullableTimeText(sess.PinnedAt),
		nullableTimeText(sess.ArchivedAt),
	}
}

func findSessionDB(db *sql.DB, id string) (Session, bool, error) {
	row := db.QueryRow(`
SELECT id, created_at, updated_at, title, summary, entries, cwd,
       forked_from_id, forked_from_turn_id, forked_from_item_id,
       pinned_at, archived_at
FROM sessions
WHERE id = ?`, id)
	return scanSessionRow(row)
}

func findSessionTx(tx *sql.Tx, id string) (Session, bool, error) {
	row := tx.QueryRow(`
SELECT id, created_at, updated_at, title, summary, entries, cwd,
       forked_from_id, forked_from_turn_id, forked_from_item_id,
       pinned_at, archived_at
FROM sessions
WHERE id = ?`, id)
	return scanSessionRow(row)
}

func scanSessionRow(scanner interface {
	Scan(dest ...any) error
}) (Session, bool, error) {
	s, err := scanSession(scanner)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	return s, true, nil
}

func scanSession(scanner interface {
	Scan(dest ...any) error
}) (Session, error) {
	var s Session
	var createdAt, updatedAt string
	var pinnedAt, archivedAt sql.NullString
	if err := scanner.Scan(
		&s.ID, &createdAt, &updatedAt, &s.Title, &s.Summary, &s.Entries, &s.CWD,
		&s.ForkedFromID, &s.ForkedFromTurnID, &s.ForkedFromItemID,
		&pinnedAt, &archivedAt,
	); err != nil {
		return Session{}, err
	}
	s.CreatedAt = parseTime(createdAt)
	s.UpdatedAt = parseTime(updatedAt)
	if pinnedAt.Valid {
		if t := parseTime(pinnedAt.String); !t.IsZero() {
			s.PinnedAt = &t
		}
	}
	if archivedAt.Valid {
		if t := parseTime(archivedAt.String); !t.IsZero() {
			s.ArchivedAt = &t
		}
	}
	return s, nil
}

func sessionExistsDB(db *sql.DB, id string) (bool, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ?`, strings.TrimSpace(id)).Scan(&count); err != nil {
		return false, fmt.Errorf("check session exists: %w", err)
	}
	return count > 0, nil
}

func sessionExistsTx(tx *sql.Tx, id string) (bool, error) {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ?`, strings.TrimSpace(id)).Scan(&count); err != nil {
		return false, fmt.Errorf("check session exists: %w", err)
	}
	return count > 0, nil
}

func appendHistoryRecordTx(tx *sql.Tx, id string, rec HistoryRecord) error {
	var nextSeq int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM session_messages WHERE session_id = ?`, id).Scan(&nextSeq); err != nil {
		return fmt.Errorf("next history sequence: %w", err)
	}
	return insertHistoryRecordTx(tx, id, nextSeq, rec)
}

func insertHistoryRecordTx(tx *sql.Tx, id string, seq int, rec HistoryRecord) error {
	_, err := tx.Exec(`
			INSERT INTO session_messages (
				session_id, seq, role, content, display_content, phase, provider_item_id, provider_item_model, client_id, hidden, steered, reasoning_content,
				reasoning_blocks_json, images_json, files_json, tool_calls_json, discovered_tools_json,
				tool_call_id, tool_result_kind, name, at, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
				provider, model
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, seq, strings.ToLower(strings.TrimSpace(rec.Role)), rec.Content, rec.DisplayContent, strings.TrimSpace(rec.Phase), strings.TrimSpace(rec.ProviderItemID), strings.TrimSpace(rec.ProviderItemModel), rec.ClientID, boolInt(rec.Hidden), boolInt(rec.Steered), rec.ReasoningContent,
		rawJSONText(rec.ReasoningBlocks), rawJSONText(rec.Images), rawJSONText(rec.Files), rawJSONText(rec.ToolCalls), rawJSONText(rec.DiscoveredTools),
		rec.ToolCallID, rec.ToolResultKind, rec.Name, nullableValueTimeText(rec.At), rec.InputTokens, rec.OutputTokens, rec.CacheCreationTokens, rec.CacheReadTokens,
		strings.TrimSpace(rec.Provider), strings.TrimSpace(rec.Model),
	)
	if err != nil {
		return fmt.Errorf("insert history record: %w", err)
	}
	return nil
}

func loadHistoryRecordsDB(db *sql.DB, id string, includeMeta bool) ([]HistoryRecord, error) {
	query := `
		SELECT role, content, display_content, phase, client_id, hidden, steered, reasoning_content,
		       provider_item_id, provider_item_model,
		       reasoning_blocks_json, images_json, files_json, tool_calls_json, discovered_tools_json,
	       tool_call_id, tool_result_kind, name, at, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
	       provider, model
	FROM session_messages
WHERE session_id = ?`
	args := []any{id}
	if !includeMeta {
		query += ` AND lower(role) <> 'meta'`
	}
	query += ` ORDER BY seq ASC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("load session history: %w", err)
	}
	defer rows.Close()

	var records []HistoryRecord
	for rows.Next() {
		var rec HistoryRecord
		var hidden, steered int
		var reasoningBlocks, images, files, toolCalls, discoveredTools string
		var at sql.NullString
		if err := rows.Scan(
			&rec.Role, &rec.Content, &rec.DisplayContent, &rec.Phase, &rec.ClientID, &hidden, &steered, &rec.ReasoningContent,
			&rec.ProviderItemID, &rec.ProviderItemModel,
			&reasoningBlocks, &images, &files, &toolCalls, &discoveredTools,
			&rec.ToolCallID, &rec.ToolResultKind, &rec.Name, &at, &rec.InputTokens, &rec.OutputTokens, &rec.CacheCreationTokens, &rec.CacheReadTokens,
			&rec.Provider, &rec.Model,
		); err != nil {
			return nil, fmt.Errorf("scan session history: %w", err)
		}
		rec.Hidden = hidden != 0
		rec.Steered = steered != 0
		rec.ReasoningBlocks = rawMessage(reasoningBlocks)
		rec.Images = rawMessage(images)
		rec.Files = rawMessage(files)
		rec.ToolCalls = rawMessage(toolCalls)
		rec.DiscoveredTools = rawMessage(discoveredTools)
		if at.Valid {
			rec.At = parseTime(at.String)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan session history: %w", err)
	}
	return records, nil
}

func sortSessions(sessions []Session) {
	sort.Slice(sessions, func(i, j int) bool {
		leftPinned := sessions[i].PinnedAt != nil
		rightPinned := sessions[j].PinnedAt != nil
		if leftPinned != rightPinned {
			return leftPinned
		}
		leftTime := sessionActivityAt(sessions[i])
		rightTime := sessionActivityAt(sessions[j])
		if !leftTime.Equal(rightTime) {
			return leftTime.After(rightTime)
		}
		return sessions[i].ID > sessions[j].ID
	})
}

func sessionActivityAt(s Session) time.Time {
	if !s.UpdatedAt.IsZero() {
		return s.UpdatedAt
	}
	return s.CreatedAt
}

func normalizeCWD(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return filepath.Clean(cwd)
	}
	return filepath.Clean(abs)
}

func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func nullableTimeText(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return timeText(*t)
}

func nullableValueTimeText(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return timeText(t)
}

func parseTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func rawJSONText(raw json.RawMessage) string {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	return string(raw)
}

func rawMessage(raw string) json.RawMessage {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil
	}
	return json.RawMessage(raw)
}

func bytesTrimSpace(raw []byte) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}
