package session

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/securefs"
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
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	Title     string    `json:"title,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	Entries   int       `json:"entries"`
	CWD       string    `json:"cwd,omitempty"`
	// WorkspaceID is the stable, location-independent identity of the workspace
	// this session belongs to (the desktop's registered-project id). Sessions
	// of a workspace with an id are listed by that id, so they follow the
	// project across moves/renames even though CWD still records the old path.
	// Empty for location-anchored threads (对话 scratch, DMs, groups), which
	// are matched by cwd / the DM-group bypass instead.
	WorkspaceID      string     `json:"workspace_id,omitempty"`
	ForkedFromID     string     `json:"forked_from_id,omitempty"`
	ForkedFromTurnID string     `json:"forked_from_turn_id,omitempty"`
	ForkedFromItemID string     `json:"forked_from_item_id,omitempty"`
	PinnedAt         *time.Time `json:"pinned_at,omitempty"`
	ArchivedAt       *time.Time `json:"archived_at,omitempty"`
	WorktreePath     string     `json:"worktree_path,omitempty"`
	WorktreeBaseHEAD string     `json:"worktree_base_head,omitempty"`
	WorktreeBaseRepo string     `json:"worktree_base_repo,omitempty"`
	// DMParticipantID pins the named participant this thread is a direct-message
	// conversation with. Set once at thread creation; never mutated afterward.
	DMParticipantID string `json:"dm_participant_id,omitempty"`
	// Group marks this session as a chat-style group channel with no primary
	// agent (chat-style-threads-design.md §3). Set once at thread creation.
	Group bool `json:"group,omitempty"`
	// FocusWorkspace pins the workspace-focus a chat-style thread has most
	// recently declared (2026-07-03-workspace-focus.md §1): "" means every
	// registered workspace (default, unchanged behavior), "~" means the
	// agent's home directory only, any other value names a registered
	// workspace (internal/workspaces.Workspace.Name). Unlike DMParticipantID
	// this is re-settable across the thread's lifetime via
	// SetFocusWorkspace — focus is expected to change as the user or the
	// resident agent switches between projects.
	FocusWorkspace string `json:"focus_workspace,omitempty"`
}

type ForkMetadata struct {
	ForkedFromID     string
	ForkedFromTurnID string
	ForkedFromItemID string
}

type WorktreeInfo struct {
	Path     string
	BaseHEAD string
	BaseRepo string
}

// HistoryRecord is the durable session message shape. App-server remains
// responsible for provider-specific ChatMessage conversion.
type HistoryRecord struct {
	// Seq is the record's per-session sequence (session_messages.seq), its
	// stable address within the thread. Ordinary appends ignore it and allocate
	// the next physical sequence. History checkpoints retain positive addresses
	// and allocate a new physical sequence for records whose Seq is zero.
	Seq               int             `json:"seq,omitempty"`
	Role              string          `json:"role"`
	Content           string          `json:"content"`
	DisplayContent    string          `json:"display_content,omitempty"`
	Phase             string          `json:"phase,omitempty"`
	ProviderItemID    string          `json:"provider_item_id,omitempty"`
	ProviderItemModel string          `json:"provider_item_model,omitempty"`
	ClientID          string          `json:"client_id,omitempty"`
	Hidden            bool            `json:"hidden,omitempty"`
	Steered           bool            `json:"steered,omitempty"`
	ReasoningContent  string          `json:"reasoning_content,omitempty"`
	ReasoningBlocks   json.RawMessage `json:"reasoning_blocks,omitempty"`
	Images            json.RawMessage `json:"images,omitempty"`
	Files             json.RawMessage `json:"files,omitempty"`
	ToolCalls         json.RawMessage `json:"tool_calls,omitempty"`
	DiscoveredTools   json.RawMessage `json:"discovered_tools,omitempty"`
	ToolCallID        string          `json:"tool_call_id,omitempty"`
	ToolInvocationID  string          `json:"tool_invocation_id,omitempty"`
	ToolResultKind    string          `json:"tool_result_kind,omitempty"`
	FinishReason      string          `json:"finish_reason,omitempty"`
	StopReason        string          `json:"stop_reason,omitempty"`
	Truncated         bool            `json:"truncated,omitempty"`
	Name              string          `json:"name,omitempty"`
	ParticipantID     string          `json:"participant_id,omitempty"`
	PostKind          string          `json:"post_kind,omitempty"`
	ThreadID          string          `json:"thread_id,omitempty"`
	// BasisSeq is the message seq a participant post declared it was generated
	// against (0 = the poster's read cursor was used / not applicable). The
	// freshness/rebase check holds a post whose basis is stale; persisted here
	// for audit and task trace.
	BasisSeq     int             `json:"basis_seq,omitempty"`
	EnvelopeMeta json.RawMessage `json:"envelope_meta,omitempty"`
	// FocusMeta is the structured {kind,name,root} metadata for a
	// workspace-focus declaration item (2026-07-03-workspace-focus.md §3.1).
	FocusMeta           json.RawMessage `json:"focus_meta,omitempty"`
	At                  time.Time       `json:"at,omitempty"`
	InputTokens         int             `json:"input_tokens,omitempty"`
	OutputTokens        int             `json:"output_tokens,omitempty"`
	ContextTokens       int             `json:"context_tokens,omitempty"`
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
// The workspace identity is bound separately via SetWorkspaceID (mirroring
// BindDMParticipant / SetGroupThread), so this signature stays stable.
func CreateWithMetadata(sessDir, id, cwd string) (*Session, error) {
	return createWithMetadata(sessDir, id, cwd, ForkMetadata{})
}

// CreateForkWithMetadata initializes a forked session with source metadata.
func CreateForkWithMetadata(sessDir, id, cwd string, fork ForkMetadata) (*Session, error) {
	return createWithMetadata(sessDir, id, cwd, fork)
}

// CreateWithWorktree initializes a forked session bound to an isolated git worktree.
func CreateWithWorktree(sessDir, id, cwd string, fork ForkMetadata, worktree WorktreeInfo) (*Session, error) {
	return createWithMetadataAndWorktree(sessDir, id, cwd, fork, worktree)
}

func createWithMetadata(sessDir, id, cwd string, fork ForkMetadata) (*Session, error) {
	return createWithMetadataAndWorktree(sessDir, id, cwd, fork, WorktreeInfo{})
}

func createWithMetadataAndWorktree(sessDir, id, cwd string, fork ForkMetadata, worktree WorktreeInfo) (*Session, error) {
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
		WorktreePath:     normalizeCWD(worktree.Path),
		WorktreeBaseHEAD: strings.TrimSpace(worktree.BaseHEAD),
		WorktreeBaseRepo: normalizeCWD(worktree.BaseRepo),
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
       pinned_at, archived_at,
       worktree_path, worktree_base_head, worktree_base_repo,
       dm_participant_id, is_group, focus_workspace, workspace_id
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

// ListForCWD reads sessions scoped to a workspace. When workspaceID is set
// (a registered project) sessions are matched by that stable id so they follow
// the project across moves; otherwise they are matched by cwd.
func ListForCWD(sessDir, cwd, workspaceID string, limit int) ([]Session, error) {
	return listForCWD(sessDir, cwd, workspaceID, limit, false)
}

// ListForCWDWithDMs reads sessions scoped to a workspace (by stable id when
// workspaceID is set, else by cwd), plus all direct-message sessions
// (DMParticipantID != "") and group channels (Group == true) regardless of
// their workspace, so DM and group threads surface in every app server's
// thread list and search results. Callers that need strict scoping
// (MostRecentForCWD, CLI listings, the insight scanner) should use ListForCWD.
func ListForCWDWithDMs(sessDir, cwd, workspaceID string, limit int) ([]Session, error) {
	return listForCWD(sessDir, cwd, workspaceID, limit, true)
}

func listForCWD(sessDir, cwd, workspaceID string, limit int, includeDMs bool) ([]Session, error) {
	target := normalizeCWD(cwd)
	wsID := strings.TrimSpace(workspaceID)
	if target == "" && wsID == "" {
		return List(sessDir, limit)
	}
	sessions, err := List(sessDir, 0)
	if err != nil {
		return nil, err
	}
	matchesCWD := func(s Session) bool {
		return target != "" &&
			(normalizeCWD(s.CWD) == target || normalizeCWD(s.WorktreeBaseRepo) == target)
	}
	filtered := make([]Session, 0, len(sessions))
	for _, s := range sessions {
		if includeDMs && (strings.TrimSpace(s.DMParticipantID) != "" || s.Group) {
			filtered = append(filtered, s)
			continue
		}
		if wsID != "" {
			// A workspace with a stable id (a registered project) matches by
			// that id, so its sessions follow the project across moves even
			// though CWD still records the old path. As a graceful transition,
			// sessions predating the id still match by path while they live at
			// the workspace's current location.
			if strings.TrimSpace(s.WorkspaceID) == wsID ||
				(strings.TrimSpace(s.WorkspaceID) == "" && matchesCWD(s)) {
				filtered = append(filtered, s)
			}
			continue
		}
		if matchesCWD(s) {
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

// BindDMParticipant permanently tags a session as the direct-message
// conversation with a named participant. The tag is set once at creation;
// callers should not invoke this for arbitrary session IDs in normal flows.
// Empty participant IDs are stored as empty strings; callers must validate
// the participant before calling.
func BindDMParticipant(sessDir, id, participantID string) (Session, error) {
	return updateMetadata(sessDir, id, false, func(s *Session) {
		s.DMParticipantID = strings.TrimSpace(participantID)
	})
}

// SetGroupThread permanently tags a session as a chat-style group channel.
// Like BindDMParticipant, this is set once at creation; callers should not
// invoke this for arbitrary session IDs in normal flows.
func SetGroupThread(sessDir, id string, group bool) (Session, error) {
	return updateMetadata(sessDir, id, false, func(s *Session) {
		s.Group = group
	})
}

// SetFocusWorkspace updates the workspace-focus declared for a chat-style
// thread (2026-07-03-workspace-focus.md §4). Unlike BindDMParticipant this
// is freely re-settable across the thread's lifetime — focus is expected
// to change repeatedly as the user or the resident agent switches between
// projects. Callers are responsible for validating focus against the
// registered workspace roster before calling; this setter stores whatever
// it is given (trimmed).
func SetFocusWorkspace(sessDir, id, focus string) (Session, error) {
	return updateMetadata(sessDir, id, false, func(s *Session) {
		s.FocusWorkspace = strings.TrimSpace(focus)
	})
}

// SetWorkspaceID binds a session to a stable, location-independent workspace
// identity (the desktop's registered-project id), so the session's state and
// thread listing follow the workspace across moves/renames. Set once at
// creation for registered-project threads; left empty for location-anchored
// threads (对话 scratch, DMs, groups), which match by cwd / the DM-group bypass.
func SetWorkspaceID(sessDir, id, workspaceID string) (Session, error) {
	return updateMetadata(sessDir, id, false, func(s *Session) {
		s.WorkspaceID = strings.TrimSpace(workspaceID)
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

func BindWorktree(sessDir, id string, worktree WorktreeInfo) (Session, error) {
	return updateMetadata(sessDir, id, false, func(s *Session) {
		s.WorktreePath = normalizeCWD(worktree.Path)
		s.WorktreeBaseHEAD = strings.TrimSpace(worktree.BaseHEAD)
		s.WorktreeBaseRepo = normalizeCWD(worktree.BaseRepo)
		if s.CWD == "" {
			s.CWD = s.WorktreePath
		}
	})
}

func WorktreeInfoForSession(sessDir, id string) (WorktreeInfo, bool, error) {
	sess, ok, err := Find(sessDir, id)
	if err != nil || !ok {
		return WorktreeInfo{}, false, err
	}
	info, bound := sess.WorktreeInfo()
	return info, bound, nil
}

func (s Session) WorktreeInfo() (WorktreeInfo, bool) {
	path := normalizeCWD(s.WorktreePath)
	if path == "" {
		return WorktreeInfo{}, false
	}
	return WorktreeInfo{
		Path:     path,
		BaseHEAD: strings.TrimSpace(s.WorktreeBaseHEAD),
		BaseRepo: normalizeCWD(s.WorktreeBaseRepo),
	}, true
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
	var pendingCompensations int
	if err := tx.QueryRow(`SELECT COUNT(1) FROM resident_admission_compensations WHERE thread_id = ?`, id).Scan(&pendingCompensations); err != nil {
		return Session{}, fmt.Errorf("inspect pending resident admission compensation: %w", err)
	}
	if pendingCompensations > 0 {
		return Session{}, fmt.Errorf("session %q has pending resident admission compensation", id)
	}
	if err := deletePendingResidentEnvelopesFromSourceThreadTx(tx, id); err != nil {
		return Session{}, fmt.Errorf("clean pending resident envelopes for deleted session: %w", err)
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

// MostRecentForCWD returns the most recent session for a workspace (by stable
// id when workspaceID is set, else by cwd).
func MostRecentForCWD(sessDir, cwd, workspaceID string) (string, error) {
	sessions, err := ListForCWD(sessDir, cwd, workspaceID, 1)
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
	_, err := AppendHistoryRecordReturningSeq(sessDir, id, rec)
	return err
}

// AppendHistoryRecordReturningSeq is AppendHistoryRecord but also returns the
// seq assigned to the new record — its stable address within the thread, which
// callers stamp onto routed envelopes so read receipts and reactions can point
// back at this exact message.
func AppendHistoryRecordReturningSeq(sessDir, id string, rec HistoryRecord) (int, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin history append: %w", err)
	}
	defer tx.Rollback()
	if ok, err := sessionExistsTx(tx, id); err != nil {
		return 0, err
	} else if !ok {
		return 0, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	seq, err := appendHistoryRecordTx(tx, id, rec)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit history append: %w", err)
	}
	return seq, nil
}

// AppendHistoryRecords appends a related history segment in one transaction.
// Tool-call declarations and their results must use this path so a crash can
// never expose a partially projected provider message sequence.
func AppendHistoryRecords(sessDir, id string, records []HistoryRecord) error {
	if len(records) == 0 {
		return nil
	}
	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin history batch append: %w", err)
	}
	defer tx.Rollback()
	if ok, err := sessionExistsTx(tx, id); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	for _, rec := range records {
		if _, err := appendHistoryRecordTx(tx, id, rec); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit history batch append: %w", err)
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

// ChatMessagesSince returns the visible chat messages in a thread with seq
// strictly greater than afterSeq, oldest first: user messages, and participant
// posts (a participant record carries a post kind; participant records with no
// post kind are telemetry/meta, not speech). It is the pull-inbox read — "the
// messages in this thread I have not read yet" — and the shared body of the
// held-draft freshness check. Non-chat roles (system/assistant/tool/meta) are
// excluded.
func ChatMessagesSince(sessDir, id string, afterSeq int) ([]HistoryRecord, error) {
	all, err := LoadHistoryRecords(sessDir, id, false)
	if err != nil {
		return nil, err
	}
	var out []HistoryRecord
	for _, rec := range all {
		if rec.Seq <= afterSeq {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(rec.Role))
		if role == "user" {
			out = append(out, rec)
			continue
		}
		if role == "participant" && strings.TrimSpace(rec.PostKind) != "" {
			out = append(out, rec)
		}
	}
	return out, nil
}

func openStore(sessDir string) (*sql.DB, error) {
	// Directory at 0o700, file at 0o600. modernc.org/sqlite ignores
	// mode arguments in its DSN parser and falls back to the umask when
	// creating the DB file, so we pre-create the file at the right mode
	// before handing it to the driver. The driver re-uses an existing
	// file instead of recreating it, so the mode set here is what the
	// file ends up with on disk.
	if err := securefs.Mkdir(sessDir); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}
	dbPath := DBPath(sessDir)
	if err := securefs.PreCreateFile(dbPath); err != nil {
		return nil, fmt.Errorf("precreate sessions db: %w", err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
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

// OpenStore exposes the shared sessions database to sibling persistence
// components that own independent tables, such as the durable tool ledger.
func OpenStore(sessDir string) (*sql.DB, error) {
	return openStore(sessDir)
}

// openStoreForScan opens the sessions database for a pure read-only scan.
// Unlike openStore it never creates the directory, database file, or schema:
// recovery scans run on their own cadence after arbitrary external cleanup,
// and a reader that resurrects the store it is probing would fight that
// cleanup forever. A missing store reports ok=false and means there is
// nothing to scan.
func openStoreForScan(sessDir string) (*sql.DB, bool, error) {
	dbPath := DBPath(sessDir)
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("probe sessions database: %w", err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return nil, false, fmt.Errorf("open sessions database: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, true, nil
}

// storeTableExists reports whether a table is present without running the
// schema migration, so scan paths can treat a store from before the table's
// introduction as having no rows instead of creating the table.
func storeTableExists(db *sql.DB, name string) (bool, error) {
	var found string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("probe table %q: %w", name, err)
	}
	return true, nil
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
			archived_at TEXT,
			worktree_path TEXT NOT NULL DEFAULT '',
			worktree_base_head TEXT NOT NULL DEFAULT '',
			worktree_base_repo TEXT NOT NULL DEFAULT ''
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
			tool_invocation_id TEXT NOT NULL DEFAULT '',
			tool_result_kind TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			at TEXT,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			context_tokens INTEGER NOT NULL DEFAULT 0,
			cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			provider TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			participant_id TEXT NOT NULL DEFAULT '',
			post_kind TEXT NOT NULL DEFAULT '',
			thread_id TEXT NOT NULL DEFAULT '',
			envelope_meta TEXT NOT NULL DEFAULT '',
			focus_meta TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(session_id, seq),
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_session_messages_role ON session_messages(session_id, role, seq)`,
		`CREATE TABLE IF NOT EXISTS session_history_checkpoints (
			session_id      TEXT NOT NULL,
			version         INTEGER NOT NULL,
			kind            TEXT NOT NULL,
			through_seq     INTEGER NOT NULL,
			replacement_json TEXT NOT NULL,
			created_at      TEXT NOT NULL,
			PRIMARY KEY(session_id, version),
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS conversation_threads (
			id             TEXT PRIMARY KEY,
			session_id     TEXT NOT NULL,
			anchor_item_id TEXT NOT NULL,
			title          TEXT NOT NULL DEFAULT '',
			status         TEXT NOT NULL,
			created_by     TEXT NOT NULL DEFAULT '',
			created_at     TEXT NOT NULL,
			thread_owner_participant_id TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_threads_session ON conversation_threads(session_id, created_at, id)`,
		`CREATE TABLE IF NOT EXISTS participants (
			id         TEXT PRIMARY KEY,
			kind       TEXT NOT NULL,
			name       TEXT NOT NULL,
			role       TEXT NOT NULL DEFAULT '',
			avatar     TEXT NOT NULL DEFAULT '',
			tagline    TEXT NOT NULL DEFAULT '',
			workspace  TEXT NOT NULL DEFAULT '',
			model      TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			retired_at TEXT
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_participants_named_name ON participants(name) WHERE retired_at IS NULL AND kind = 'named'`,
		`CREATE TABLE IF NOT EXISTS participant_runs (
			id             TEXT PRIMARY KEY,
			participant_id TEXT NOT NULL,
			agent_id       TEXT NOT NULL DEFAULT '',
			task_id        TEXT NOT NULL DEFAULT '',
			session_id      TEXT NOT NULL DEFAULT '',
			summary        TEXT NOT NULL DEFAULT '',
			outcome        TEXT NOT NULL DEFAULT '',
			created_at     TEXT NOT NULL,
			updated_at     TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_participant_runs_participant ON participant_runs(participant_id, created_at, id)`,
		`CREATE TABLE IF NOT EXISTS thread_members (
			session_id     TEXT NOT NULL,
			participant_id TEXT NOT NULL,
			joined_at      INTEGER NOT NULL,
			PRIMARY KEY (session_id, participant_id),
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE,
			FOREIGN KEY(participant_id) REFERENCES participants(id) ON DELETE CASCADE
		)`,
		// conversation_thread_members is the weak-isolation participant subset of
		// a reply subthread (cth-*). Keyed on conversation_thread_id (not
		// session_id) because a cth is a conversation_threads row, not a session;
		// the FK cascades membership away when the subthread or participant is
		// deleted. Retire is an UPDATE, not a DELETE, so RetireParticipant clears
		// these rows explicitly (mirrors thread_members).
		`CREATE TABLE IF NOT EXISTS conversation_thread_members (
			conversation_thread_id TEXT NOT NULL,
			participant_id         TEXT NOT NULL,
			joined_at              INTEGER NOT NULL,
			PRIMARY KEY (conversation_thread_id, participant_id),
			FOREIGN KEY(conversation_thread_id) REFERENCES conversation_threads(id) ON DELETE CASCADE,
			FOREIGN KEY(participant_id) REFERENCES participants(id) ON DELETE CASCADE
		)`,
		// resident_inbox pivots for issue #3 — replay → settle/expire (user
		// spec: "桌面应用关着即世界暂停，重启不替过去补课"). Three-state
		// isolation is enforced at the index level: a row is in the
		// `pending` set iff BOTH `consumed_at IS NULL` AND `expired_at IS
		// NULL`; `processed` is `consumed_at IS NOT NULL`; `expired` is
		// `consumed_at IS NULL AND expired_at IS NOT NULL`. The
		// idx_resident_inbox_pending partial index MUST exclude expired
		// rows, otherwise the existing PendingResidentEnvelopes query
		// would surface settled-expired envelopes as "pending" and the
		// pivot invariant "boot can't burn tokens" silently fails.
		`CREATE TABLE IF NOT EXISTS resident_inbox (
			id             TEXT PRIMARY KEY,
			participant_id TEXT NOT NULL,
			envelope_json  TEXT NOT NULL,
			created_at     INTEGER NOT NULL,
			consumed_at    INTEGER,
			expired_at     INTEGER,
			FOREIGN KEY(participant_id) REFERENCES participants(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_resident_inbox_pending ON resident_inbox(participant_id, created_at) WHERE consumed_at IS NULL AND expired_at IS NULL`,
		// resident_admission_compensations is a short-lived recovery journal for
		// resident turns that committed admission state but failed before their
		// model runner launched. The rollback and journal deletion happen in one
		// transaction, so a crash either leaves a replayable intent or a fully
		// compensated admission.
		`CREATE TABLE IF NOT EXISTS resident_admission_compensations (
			id           TEXT PRIMARY KEY,
			thread_id    TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			created_at   INTEGER NOT NULL
		)`,
		// resident_wake_intents is the durable handoff from a resolved admission
		// rollback to a live app-server. The rollback transaction restores the
		// inbox/read state and publishes this wake atomically; a resident drain
		// acknowledges it only after launch ownership is registered (or after it
		// proves there is no work). Cold-boot settlement discards these intents so
		// reopening the desktop never spends tokens replaying work from downtime.
		`CREATE TABLE IF NOT EXISTS resident_wake_intents (
			id             TEXT PRIMARY KEY,
			participant_id TEXT NOT NULL,
			thread_id      TEXT NOT NULL,
			created_at     INTEGER NOT NULL,
			FOREIGN KEY(participant_id) REFERENCES participants(id) ON DELETE CASCADE,
			FOREIGN KEY(thread_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_resident_wake_intents_participant
			ON resident_wake_intents(participant_id, created_at, id)`,
		// message_marks backs read receipts + message reactions
		// (2026-07-04-read-receipts-and-reactions.md §4): one row per
		// (message, participant, kind). kind='seen' carries a lifecycle
		// status (in_progress|completed|failed); kind='reaction' carries a
		// reaction key in payload. A message is addressed by (session_id, seq).
		`CREATE TABLE IF NOT EXISTS message_marks (
			session_id     TEXT NOT NULL,
			seq            INTEGER NOT NULL,
			participant_id TEXT NOT NULL,
			kind           TEXT NOT NULL,
			status         TEXT NOT NULL DEFAULT '',
			payload        TEXT NOT NULL DEFAULT '',
			at             INTEGER NOT NULL,
			PRIMARY KEY (session_id, seq, participant_id, kind),
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE,
			FOREIGN KEY(participant_id) REFERENCES participants(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_message_marks_message ON message_marks(session_id, seq)`,
		// task_attempts is the durable scheduler truth: one row per concrete
		// execution of one plan node. queued/running attempts hold an assignee
		// reservation, enforced globally by the partial unique index.
		`CREATE TABLE IF NOT EXISTS task_attempts (
			id               TEXT PRIMARY KEY,
			session_id       TEXT NOT NULL,
			task_id          TEXT NOT NULL,
			node_id          TEXT NOT NULL,
			assignee_id      TEXT NOT NULL,
			ordinal          INTEGER NOT NULL,
			status           TEXT NOT NULL,
			failure_category TEXT NOT NULL DEFAULT '',
			failure_message  TEXT NOT NULL DEFAULT '',
			created_at       INTEGER NOT NULL,
			started_at       INTEGER NOT NULL DEFAULT 0,
			finished_at      INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(task_id) REFERENCES conversation_threads(id) ON DELETE CASCADE,
			FOREIGN KEY(assignee_id) REFERENCES participants(id) ON DELETE CASCADE,
			UNIQUE(task_id, node_id, ordinal)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_attempts_task ON task_attempts(task_id, node_id, ordinal)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_task_attempts_active_assignee
		 ON task_attempts(assignee_id) WHERE status IN ('queued', 'running')`,
		// task_events is the durable, append-only execution trace for a task
		// (one conversation_thread upgraded to a task). It is the record the
		// system, the lead/supervisor, and humans read to understand what
		// happened — especially on failure. task_id is the cth id; node_id is a
		// plan piece id (empty for task-level events). seq is a per-task
		// monotonic order key. Never overwritten: state transitions are new
		// rows, so history is preserved (unlike the plan JSON column).
		`CREATE TABLE IF NOT EXISTS task_events (
			id         TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			task_id    TEXT NOT NULL,
			node_id    TEXT NOT NULL DEFAULT '',
			seq        INTEGER NOT NULL,
			kind       TEXT NOT NULL,
			actor      TEXT NOT NULL DEFAULT '',
			attempt_id TEXT NOT NULL DEFAULT '',
			summary    TEXT NOT NULL DEFAULT '',
			payload    TEXT NOT NULL DEFAULT '',
			at         INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_events_task ON task_events(task_id, seq)`,
		// inference_journal_runtimes is the cross-process liveness lease used to
		// distinguish a crashed writer from another live app-server or CLI.
		`CREATE TABLE IF NOT EXISTS inference_journal_runtimes (
			id                 TEXT PRIMARY KEY,
			workspace_scope    TEXT NOT NULL,
			pid                INTEGER NOT NULL,
			started_at         INTEGER NOT NULL,
			heartbeat_at       INTEGER NOT NULL,
			closed_at          INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_inference_journal_runtimes_scope
		 ON inference_journal_runtimes(workspace_scope, heartbeat_at, closed_at)`,
		`CREATE TABLE IF NOT EXISTS inference_workflows (
			id                         TEXT PRIMARY KEY,
			runtime_id                 TEXT NOT NULL,
			workspace_scope            TEXT NOT NULL,
			owner_id                   TEXT NOT NULL,
			workload_profile           TEXT NOT NULL,
			max_operations             INTEGER NOT NULL DEFAULT -1,
			max_attempts               INTEGER NOT NULL DEFAULT -1,
			max_submissions            INTEGER NOT NULL DEFAULT -1,
			max_replays                INTEGER NOT NULL DEFAULT -1,
			max_transport_switches     INTEGER NOT NULL DEFAULT -1,
			max_credential_refreshes   INTEGER NOT NULL DEFAULT -1,
			max_payload_transforms     INTEGER NOT NULL DEFAULT -1,
			max_child_operations       INTEGER NOT NULL DEFAULT -1,
			max_recovery_wait_ms       INTEGER NOT NULL DEFAULT -1,
			max_unknown_billable       INTEGER NOT NULL DEFAULT -1,
			max_usage_tokens           INTEGER NOT NULL DEFAULT -1,
			used_operations            INTEGER NOT NULL DEFAULT 0,
			used_attempts              INTEGER NOT NULL DEFAULT 0,
			used_submissions           INTEGER NOT NULL DEFAULT 0,
			used_replays               INTEGER NOT NULL DEFAULT 0,
			used_transport_switches    INTEGER NOT NULL DEFAULT 0,
			used_credential_refreshes  INTEGER NOT NULL DEFAULT 0,
			used_payload_transforms    INTEGER NOT NULL DEFAULT 0,
			used_child_operations      INTEGER NOT NULL DEFAULT 0,
			used_recovery_wait_ms      INTEGER NOT NULL DEFAULT 0,
			known_submissions          INTEGER NOT NULL DEFAULT 0,
			estimated_submissions      INTEGER NOT NULL DEFAULT 0,
			unknown_billable           INTEGER NOT NULL DEFAULT 0,
			known_usage_tokens         INTEGER NOT NULL DEFAULT 0,
			estimated_usage_tokens     INTEGER NOT NULL DEFAULT 0,
			status                     TEXT NOT NULL DEFAULT 'active',
			created_at                 INTEGER NOT NULL,
			updated_at                 INTEGER NOT NULL,
			terminal_at                INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(runtime_id) REFERENCES inference_journal_runtimes(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_inference_workflows_owner
		 ON inference_workflows(owner_id, created_at, id)`,
		// inference_operations / attempts / submissions form the metadata-only
		// write-ahead journal for provider requests. They intentionally store a
		// SHA-256 request identity instead of prompt or wire payload content.
		`CREATE TABLE IF NOT EXISTS inference_operations (
			id                 TEXT PRIMARY KEY,
			workflow_id        TEXT NOT NULL,
			parent_operation_id TEXT NOT NULL DEFAULT '',
			attempt_limit      INTEGER NOT NULL,
			runtime_id         TEXT NOT NULL,
			workspace_scope    TEXT NOT NULL,
			owner_id           TEXT NOT NULL,
			kind               TEXT NOT NULL,
			workload_profile   TEXT NOT NULL,
			payload_version    INTEGER NOT NULL,
			request_hash       TEXT NOT NULL,
			status             TEXT NOT NULL,
			terminal_outcome   TEXT NOT NULL DEFAULT '',
			recovery_action    TEXT NOT NULL DEFAULT '',
			failure_origin     TEXT NOT NULL DEFAULT '',
			failure_category   TEXT NOT NULL DEFAULT '',
			provider_family    TEXT NOT NULL DEFAULT '',
			provider_code      TEXT NOT NULL DEFAULT '',
			http_status        INTEGER NOT NULL DEFAULT 0,
			confidence         TEXT NOT NULL DEFAULT '',
			created_at         INTEGER NOT NULL,
			updated_at         INTEGER NOT NULL,
			terminal_at        INTEGER NOT NULL DEFAULT 0
			,FOREIGN KEY(runtime_id) REFERENCES inference_journal_runtimes(id)
			,FOREIGN KEY(workflow_id) REFERENCES inference_workflows(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_inference_operations_recovery
		 ON inference_operations(workspace_scope, status, runtime_id, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_inference_operations_owner
		 ON inference_operations(owner_id, created_at, id)`,
		`CREATE TABLE IF NOT EXISTS inference_attempts (
			id                 TEXT PRIMARY KEY,
			operation_id       TEXT NOT NULL,
			ordinal            INTEGER NOT NULL,
			request_hash       TEXT NOT NULL,
			phase              TEXT NOT NULL,
			terminal_outcome   TEXT NOT NULL DEFAULT '',
			recovery_action    TEXT NOT NULL DEFAULT '',
			retry_at           INTEGER NOT NULL DEFAULT 0,
			failure_origin     TEXT NOT NULL DEFAULT '',
			failure_category   TEXT NOT NULL DEFAULT '',
			provider_family    TEXT NOT NULL DEFAULT '',
			provider_code      TEXT NOT NULL DEFAULT '',
			http_status        INTEGER NOT NULL DEFAULT 0,
			confidence         TEXT NOT NULL DEFAULT '',
			prepared_at        INTEGER NOT NULL,
			dispatching_at     INTEGER NOT NULL DEFAULT 0,
			sent_at            INTEGER NOT NULL DEFAULT 0,
			first_event_at     INTEGER NOT NULL DEFAULT 0,
			terminal_at        INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(operation_id) REFERENCES inference_operations(id) ON DELETE CASCADE,
			UNIQUE(operation_id, ordinal),
			UNIQUE(id, operation_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_inference_attempts_operation
		 ON inference_attempts(operation_id, ordinal)`,
		`CREATE TABLE IF NOT EXISTS inference_submissions (
			id                         TEXT PRIMARY KEY,
			operation_id               TEXT NOT NULL,
			attempt_id                 TEXT NOT NULL,
			ordinal                    INTEGER NOT NULL,
			attempt_ordinal            INTEGER NOT NULL,
			provider                   TEXT NOT NULL DEFAULT '',
			protocol                   TEXT NOT NULL DEFAULT '',
			transport                  TEXT NOT NULL DEFAULT '',
			mode                       TEXT NOT NULL DEFAULT '',
			reason                     TEXT NOT NULL DEFAULT '',
			outcome                    TEXT NOT NULL,
			failure_category           TEXT NOT NULL DEFAULT '',
			cost_state                 TEXT NOT NULL,
			reported_input_tokens      INTEGER NOT NULL DEFAULT 0,
			reported_output_tokens     INTEGER NOT NULL DEFAULT 0,
			reported_cache_creation    INTEGER NOT NULL DEFAULT 0,
			reported_cache_read        INTEGER NOT NULL DEFAULT 0,
			reported_cache_unknown     INTEGER NOT NULL DEFAULT 0,
			has_reported_usage         INTEGER NOT NULL DEFAULT 0,
			estimated_input_tokens     INTEGER NOT NULL DEFAULT 0,
			estimated_output_tokens    INTEGER NOT NULL DEFAULT 0,
			estimated_cache_creation   INTEGER NOT NULL DEFAULT 0,
			estimated_cache_read       INTEGER NOT NULL DEFAULT 0,
			estimated_cache_unknown    INTEGER NOT NULL DEFAULT 0,
			has_estimated_usage        INTEGER NOT NULL DEFAULT 0,
			output_bytes               INTEGER NOT NULL DEFAULT 0,
			started_at                 INTEGER NOT NULL,
			completed_at               INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(operation_id) REFERENCES inference_operations(id) ON DELETE CASCADE,
			FOREIGN KEY(attempt_id, operation_id) REFERENCES inference_attempts(id, operation_id) ON DELETE CASCADE,
			UNIQUE(operation_id, ordinal)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_inference_attempts_id_operation
		 ON inference_attempts(id, operation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_inference_submissions_attempt
		 ON inference_submissions(attempt_id, ordinal)`,
		`CREATE TRIGGER IF NOT EXISTS trg_inference_submission_attempt_operation_insert
		 BEFORE INSERT ON inference_submissions
		 WHEN NOT EXISTS (
			SELECT 1 FROM inference_attempts a
			WHERE a.id = NEW.attempt_id AND a.operation_id = NEW.operation_id
		 )
		 BEGIN
			SELECT RAISE(ABORT, 'inference submission attempt/operation mismatch');
		 END`,
		`CREATE TABLE IF NOT EXISTS tool_batches (
			id              TEXT PRIMARY KEY,
			owner_id        TEXT NOT NULL,
			operation_id    TEXT NOT NULL,
			step_index      INTEGER NOT NULL,
			status          TEXT NOT NULL,
			created_at      INTEGER NOT NULL,
			updated_at      INTEGER NOT NULL,
			terminal_at     INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tool_batches_owner
		 ON tool_batches(owner_id, created_at, id)`,
		`CREATE TABLE IF NOT EXISTS tool_invocations (
			id                TEXT PRIMARY KEY,
			batch_id          TEXT NOT NULL,
			provider_call_id  TEXT NOT NULL,
			tool_name         TEXT NOT NULL,
			tool_kind         TEXT NOT NULL DEFAULT '',
			arguments_json    TEXT NOT NULL,
			replay_policy     TEXT NOT NULL,
			state             TEXT NOT NULL,
			result_json       TEXT NOT NULL DEFAULT '',
			prepared_at       INTEGER NOT NULL,
			running_at        INTEGER NOT NULL DEFAULT 0,
			settled_at        INTEGER NOT NULL DEFAULT 0,
			projected_at      INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(batch_id) REFERENCES tool_batches(id) ON DELETE CASCADE,
			UNIQUE(batch_id, provider_call_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tool_invocations_batch
		 ON tool_invocations(batch_id, prepared_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_tool_invocations_projection
		 ON tool_invocations(state, projected_at, settled_at)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate sessions database: %w", err)
		}
	}
	if err := addColumnIfMissing(db, "inference_operations", "workflow_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "inference_operations", "parent_operation_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "inference_operations", "attempt_limit", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	// Existing journal rows predate workflow identity. Give each historical
	// operation a conservative synthetic workflow and rebuild exact counters
	// from its durable attempts/submissions without inventing hard limits.
	if _, err := db.Exec(`
INSERT OR IGNORE INTO inference_workflows (
    id, runtime_id, workspace_scope, owner_id, workload_profile,
    used_operations, used_attempts, used_submissions, used_replays,
    known_submissions, estimated_submissions, unknown_billable,
    known_usage_tokens, estimated_usage_tokens, status, created_at, updated_at, terminal_at
)
SELECT 'iwf-legacy-' || o.id, o.runtime_id, o.workspace_scope, o.owner_id, o.workload_profile,
       1,
       (SELECT COUNT(*) FROM inference_attempts a WHERE a.operation_id = o.id),
       (SELECT COUNT(*) FROM inference_submissions s WHERE s.operation_id = o.id),
       MAX(0, (SELECT COUNT(*) FROM inference_attempts a WHERE a.operation_id = o.id) - 1),
       (SELECT COUNT(*) FROM inference_submissions s WHERE s.operation_id = o.id AND s.cost_state = 'known'),
       (SELECT COUNT(*) FROM inference_submissions s WHERE s.operation_id = o.id AND s.cost_state = 'estimated'),
       (SELECT COUNT(*) FROM inference_submissions s WHERE s.operation_id = o.id AND s.cost_state = 'unknown_but_billable'),
       COALESCE((SELECT SUM(reported_input_tokens + reported_output_tokens + reported_cache_creation + reported_cache_read)
                 FROM inference_submissions s WHERE s.operation_id = o.id AND s.cost_state = 'known'), 0),
       COALESCE((SELECT SUM(estimated_input_tokens + estimated_output_tokens + estimated_cache_creation + estimated_cache_read)
                 FROM inference_submissions s WHERE s.operation_id = o.id AND s.cost_state = 'estimated'), 0),
       o.status, o.created_at, o.updated_at, o.terminal_at
FROM inference_operations o
WHERE o.workflow_id = ''`); err != nil {
		return fmt.Errorf("migrate inference workflows: %w", err)
	}
	if _, err := db.Exec(`
UPDATE inference_operations
SET workflow_id = 'iwf-legacy-' || id,
    attempt_limit = MAX(attempt_limit, (SELECT COUNT(*) FROM inference_attempts a WHERE a.operation_id = inference_operations.id), 1)
WHERE workflow_id = ''`); err != nil {
		return fmt.Errorf("backfill inference workflow identity: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_inference_operations_workflow
		ON inference_operations(workflow_id, created_at, id)`); err != nil {
		return fmt.Errorf("migrate inference workflow index: %w", err)
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
	if err := addColumnIfMissing(db, "session_messages", "context_tokens", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "tool_result_kind", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "tool_invocation_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
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
	if err := addColumnIfMissing(db, "session_messages", "participant_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "post_kind", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "thread_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "basis_seq", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "envelope_meta", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "session_messages", "focus_meta", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_session_messages_thread ON session_messages(session_id, thread_id, seq)`); err != nil {
		return fmt.Errorf("migrate sessions database: %w", err)
	}
	// forked_from marks a named participant as a temporary分身 forked from
	// another named agent (decision six): it holds the母体's participant id.
	// Empty for ordinary participants.
	if err := addColumnIfMissing(db, "participants", "forked_from", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "sessions", "worktree_path", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "sessions", "worktree_base_head", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "sessions", "worktree_base_repo", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "sessions", "dm_participant_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "sessions", "is_group", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "sessions", "focus_workspace", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "sessions", "workspace_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_sessions_workspace_id ON sessions(workspace_id)`); err != nil {
		return fmt.Errorf("migrate sessions database: %w", err)
	}
	// thread_id tags a read receipt / mention mark with the reply subthread the
	// marked message belongs to ('' = main stream, cth-* = a reply subthread).
	// It is a redundant descriptor of the message that seq addresses (a given seq
	// carries one consistent tag), added so per-cth read cursors can be computed
	// without joining session_messages: ThreadReadWatermarkScoped filters seen
	// rows by this tag so a cth read never advances the main-stream cursor and
	// vice versa (T3 independent per-cth cursor). Legacy rows default '' — they
	// are historical main-stream marks, no data migration needed.
	if err := addColumnIfMissing(db, "message_marks", "thread_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// Task-escalation columns on conversation_threads: escalated_at/escalated_by
	// mark a reply promoted to a task (survives resolve); summary holds the
	// one-line conclusion bubbled back to the main stream on wrap-up.
	if err := addColumnIfMissing(db, "conversation_threads", "escalated_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "conversation_threads", "escalated_by", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "conversation_threads", "summary", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// lead_participant_id records the single named agent assigned as the task's
	// lead on promotion. Distinct from escalated_by provenance; it survives
	// resolve for history, and resolved Tasks cannot be reopened.
	if err := addColumnIfMissing(db, "conversation_threads", "lead_participant_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// thread_owner_participant_id is the durable owner of the discussion Thread.
	// On promotion this identity becomes lead_participant_id. Existing active tasks
	// preserve their active named lead as owner; open Threads use their active named
	// parent author. Legacy Task rows without a valid lead remain ownerless; they
	// never fall back to their parent author.
	if err := addColumnIfMissing(db, "conversation_threads", "thread_owner_participant_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// plan holds the lead's declared work breakdown (JSON array of TaskPiece)
	// for a team task (task-rail design §8). Empty string for a plain task.
	if err := addColumnIfMissing(db, "conversation_threads", "plan", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// exec_state is the execution axis of a task cth (planning/executing/
	// blocked/needs_human/completed/failed), deliberately separate from the
	// approval status column. Empty = never entered execution.
	if err := addColumnIfMissing(db, "conversation_threads", "exec_state", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	// parent_seq / parent_author_participant_id bind a reply subthread to the
	// exact main-stream message it was opened from (T3 Thread-first-class): the
	// message's seq and its author's participant id ("human" for a user/thread-
	// owner message). AnchorItemID stays for GUI rendering; these two are the
	// durable, seq-addressable parent binding used to select the Thread owner.
	// Legacy rows created before anchored Threads may leave them empty.
	if err := addColumnIfMissing(db, "conversation_threads", "parent_seq", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "conversation_threads", "parent_author_participant_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "task_events", "attempt_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "inference_journal_runtimes", "pid", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "resident_admission_compensations", "thread_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_resident_admission_compensations_thread
		ON resident_admission_compensations(thread_id, created_at, id)`); err != nil {
		return fmt.Errorf("migrate resident admission compensation index: %w", err)
	}
	if _, err := db.Exec(`
UPDATE conversation_threads
SET thread_owner_participant_id = CASE
	WHEN status = 'task'
	 AND lead_participant_id <> ''
	 AND EXISTS (
		SELECT 1 FROM participants p
		WHERE p.id = conversation_threads.lead_participant_id
		  AND p.kind = 'named' AND p.retired_at IS NULL
	 )
	 AND EXISTS (
		SELECT 1 FROM thread_members tm
		WHERE tm.session_id = conversation_threads.session_id
		  AND tm.participant_id = conversation_threads.lead_participant_id
	 )
	THEN lead_participant_id
	WHEN status = 'open'
	 AND parent_author_participant_id <> ''
	 AND EXISTS (
		SELECT 1 FROM participants p
		WHERE p.id = conversation_threads.parent_author_participant_id
		  AND p.kind = 'named' AND p.retired_at IS NULL
	 )
	 AND EXISTS (
		SELECT 1 FROM thread_members tm
		WHERE tm.session_id = conversation_threads.session_id
		  AND tm.participant_id = conversation_threads.parent_author_participant_id
	 )
	THEN parent_author_participant_id
	ELSE ''
END
WHERE thread_owner_participant_id = ''
  AND status IN ('open', 'task')`); err != nil {
		return fmt.Errorf("migrate conversation thread owners: %w", err)
	}
	if err := ensureConversationThreadAnchorUniqueness(db); err != nil {
		return err
	}
	if err := ensureConversationThreadParentUniqueness(db); err != nil {
		return err
	}
	return nil
}

// ensureConversationThreadAnchorUniqueness repairs the only state that could
// predate the durable one-message/one-Thread invariant: two rows created for
// the same rendered parent message by concurrent desktop windows. References
// and memberships are folded into the most advanced row before the partial
// unique index is installed, so migration never fixes startup by discarding
// reachable discussion history.
func ensureConversationThreadAnchorUniqueness(db *sql.DB) error {
	var installed int
	if err := db.QueryRow(`
SELECT EXISTS(
	SELECT 1 FROM sqlite_master
	WHERE type = 'index' AND name = 'idx_conversation_threads_anchor'
)`).Scan(&installed); err != nil {
		return fmt.Errorf("check conversation thread anchor index: %w", err)
	}
	if installed != 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin conversation thread anchor migration: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
SELECT session_id, anchor_item_id
FROM conversation_threads
WHERE anchor_item_id <> ''
GROUP BY session_id, anchor_item_id
HAVING COUNT(*) > 1`)
	if err != nil {
		return fmt.Errorf("list duplicate conversation thread anchors: %w", err)
	}
	type anchorKey struct{ sessionID, anchorItemID string }
	var keys []anchorKey
	for rows.Next() {
		var key anchorKey
		if err := rows.Scan(&key.sessionID, &key.anchorItemID); err != nil {
			rows.Close()
			return fmt.Errorf("scan duplicate conversation thread anchor: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close duplicate conversation thread anchors: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan duplicate conversation thread anchors: %w", err)
	}

	for _, key := range keys {
		duplicates, err := loadConversationThreadDuplicates(tx,
			"session_id = ? AND anchor_item_id = ?", key.sessionID, key.anchorItemID)
		if err != nil {
			return fmt.Errorf("load duplicate conversation threads: %w", err)
		}
		if err := mergeConversationThreadDuplicates(tx, key.sessionID, duplicates); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`
CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_threads_anchor
ON conversation_threads(session_id, anchor_item_id)
WHERE anchor_item_id <> ''`); err != nil {
		return fmt.Errorf("create conversation thread anchor index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit conversation thread anchor migration: %w", err)
	}
	return nil
}

// ensureConversationThreadParentUniqueness makes the durable parent message
// address, not a transient rendered item id, the one-message/one-Thread key.
// Rows created before parent_seq existed remain untouched at zero.
func ensureConversationThreadParentUniqueness(db *sql.DB) error {
	var installed int
	if err := db.QueryRow(`
SELECT EXISTS(
	SELECT 1 FROM sqlite_master
	WHERE type = 'index' AND name = 'idx_conversation_threads_parent_seq'
)`).Scan(&installed); err != nil {
		return fmt.Errorf("check conversation thread parent index: %w", err)
	}
	if installed != 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin conversation thread parent migration: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
SELECT session_id, parent_seq
FROM conversation_threads
WHERE parent_seq > 0
GROUP BY session_id, parent_seq
HAVING COUNT(*) > 1`)
	if err != nil {
		return fmt.Errorf("list duplicate conversation thread parents: %w", err)
	}
	type parentKey struct {
		sessionID string
		parentSeq int
	}
	var keys []parentKey
	for rows.Next() {
		var key parentKey
		if err := rows.Scan(&key.sessionID, &key.parentSeq); err != nil {
			rows.Close()
			return fmt.Errorf("scan duplicate conversation thread parent: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close duplicate conversation thread parents: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan duplicate conversation thread parents: %w", err)
	}

	for _, key := range keys {
		duplicates, err := loadConversationThreadDuplicates(tx,
			"session_id = ? AND parent_seq = ?", key.sessionID, key.parentSeq)
		if err != nil {
			return fmt.Errorf("load duplicate conversation thread parents: %w", err)
		}
		if err := mergeConversationThreadDuplicates(tx, key.sessionID, duplicates); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`
CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_threads_parent_seq
ON conversation_threads(session_id, parent_seq)
WHERE parent_seq > 0`); err != nil {
		return fmt.Errorf("create conversation thread parent index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit conversation thread parent migration: %w", err)
	}
	return nil
}

func loadConversationThreadDuplicates(tx *sql.Tx, where string, args ...any) ([]ConversationThread, error) {
	threadRows, err := tx.Query(`
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at,
       thread_owner_participant_id, escalated_at, escalated_by, summary,
       lead_participant_id, plan, exec_state, parent_seq,
       parent_author_participant_id
FROM conversation_threads
WHERE `+where+`
ORDER BY
  CASE
    WHEN status = 'resolved' AND escalated_at <> '' THEN 5
    WHEN status = 'task' THEN 4
    WHEN status = 'open' THEN 3
    WHEN status = 'resolved' THEN 2
    ELSE 1
  END DESC,
  CASE WHEN summary <> '' THEN 1 ELSE 0 END DESC,
  created_at ASC,
  id ASC`, args...)
	if err != nil {
		return nil, err
	}
	var duplicates []ConversationThread
	for threadRows.Next() {
		thread, err := scanConversationThread(threadRows)
		if err != nil {
			threadRows.Close()
			return nil, fmt.Errorf("scan duplicate conversation thread: %w", err)
		}
		duplicates = append(duplicates, thread)
	}
	if err := threadRows.Close(); err != nil {
		return nil, fmt.Errorf("close duplicate conversation threads: %w", err)
	}
	if err := threadRows.Err(); err != nil {
		return nil, fmt.Errorf("scan duplicate conversation threads: %w", err)
	}
	return duplicates, nil
}

func mergeConversationThreadDuplicates(tx *sql.Tx, sessionID string, duplicates []ConversationThread) error {
	if len(duplicates) < 2 {
		return nil
	}
	keeper := duplicates[0]
	for _, duplicate := range duplicates[1:] {
		mergeConversationThreadMigrationFields(&keeper, duplicate)
		if _, err := tx.Exec(`
INSERT INTO conversation_thread_members (conversation_thread_id, participant_id, joined_at)
SELECT ?, participant_id, joined_at
FROM conversation_thread_members
WHERE conversation_thread_id = ?
ON CONFLICT(conversation_thread_id, participant_id) DO NOTHING`, keeper.ID, duplicate.ID); err != nil {
			return fmt.Errorf("merge conversation thread members: %w", err)
		}
		if _, err := tx.Exec(`
UPDATE session_messages
SET thread_id = ?
WHERE session_id = ? AND thread_id = ?`, keeper.ID, sessionID, duplicate.ID); err != nil {
			return fmt.Errorf("merge conversation thread messages: %w", err)
		}
		if _, err := tx.Exec(`
UPDATE message_marks
SET thread_id = ?
WHERE session_id = ? AND thread_id = ?`, keeper.ID, sessionID, duplicate.ID); err != nil {
			return fmt.Errorf("merge conversation thread marks: %w", err)
		}
		if _, err := tx.Exec(`
UPDATE participant_runs
SET task_id = ?
WHERE task_id = ?`, keeper.ID, duplicate.ID); err != nil {
			return fmt.Errorf("merge conversation thread participant runs: %w", err)
		}
		if err := rewriteResidentInboxSubthread(tx, duplicate.ID, keeper.ID); err != nil {
			return err
		}
		var eventOffset int
		if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM task_events WHERE task_id = ?`, keeper.ID).Scan(&eventOffset); err != nil {
			return fmt.Errorf("load conversation thread event offset: %w", err)
		}
		if _, err := tx.Exec(`
UPDATE task_events
SET task_id = ?, seq = seq + ?
WHERE task_id = ?`, keeper.ID, eventOffset, duplicate.ID); err != nil {
			return fmt.Errorf("merge conversation thread events: %w", err)
		}
		if _, err := tx.Exec(`
UPDATE task_attempts
SET ordinal = ordinal + COALESCE((
	SELECT MAX(existing.ordinal)
	FROM task_attempts AS existing
	WHERE existing.task_id = ?
	  AND existing.node_id = task_attempts.node_id
), 0)
WHERE task_id = ?`, keeper.ID, duplicate.ID); err != nil {
			return fmt.Errorf("resequence conversation thread attempts: %w", err)
		}
		if _, err := tx.Exec(`
UPDATE task_attempts
SET task_id = ?
WHERE task_id = ?`, keeper.ID, duplicate.ID); err != nil {
			return fmt.Errorf("merge conversation thread attempts: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM conversation_threads WHERE id = ?`, duplicate.ID); err != nil {
			return fmt.Errorf("remove duplicate conversation thread: %w", err)
		}
	}
	if err := reconcileMergedConversationThreadAttempts(tx, &keeper); err != nil {
		return err
	}
	planJSON, err := json.Marshal(keeper.Plan)
	if err != nil {
		return fmt.Errorf("marshal merged conversation thread plan: %w", err)
	}
	if len(keeper.Plan) == 0 {
		planJSON = nil
	}
	if _, err := tx.Exec(`
UPDATE conversation_threads
SET title = ?, created_by = ?, thread_owner_participant_id = ?,
    escalated_at = ?, escalated_by = ?, summary = ?, lead_participant_id = ?,
    plan = ?, exec_state = ?, parent_seq = ?,
    parent_author_participant_id = ?
WHERE id = ?`, keeper.Title, keeper.CreatedBy, keeper.ThreadOwnerParticipantID,
		timeText(keeper.EscalatedAt), keeper.EscalatedBy, keeper.Summary,
		keeper.LeadParticipantID, string(planJSON),
		keeper.ExecState, keeper.ParentSeq, keeper.ParentAuthorParticipantID,
		keeper.ID); err != nil {
		return fmt.Errorf("persist merged conversation thread: %w", err)
	}
	return nil
}

func reconcileMergedConversationThreadAttempts(tx *sql.Tx, thread *ConversationThread) error {
	if thread == nil || len(thread.Plan) == 0 {
		return nil
	}
	for i := range thread.Plan {
		piece := &thread.Plan[i]
		var maxOrdinal int
		if err := tx.QueryRow(`
SELECT COALESCE(MAX(ordinal), 0)
FROM task_attempts
WHERE task_id = ? AND node_id = ?`, thread.ID, piece.ID).Scan(&maxOrdinal); err != nil {
			return fmt.Errorf("load merged task attempt count for node %q: %w", piece.ID, err)
		}
		if maxOrdinal > piece.Attempts {
			piece.Attempts = maxOrdinal
		}

		var activeAttemptID string
		err := tx.QueryRow(`
SELECT id
FROM task_attempts
WHERE task_id = ? AND node_id = ? AND status IN (?, ?)
ORDER BY ordinal DESC, created_at DESC, id DESC
LIMIT 1`, thread.ID, piece.ID, TaskAttemptQueued, TaskAttemptRunning).Scan(&activeAttemptID)
		switch {
		case err == nil:
			piece.CurrentAttemptID = activeAttemptID
			piece.Status = TaskPieceActive
		case errors.Is(err, sql.ErrNoRows):
			piece.CurrentAttemptID = ""
			if piece.Status == TaskPieceActive {
				piece.Status = TaskPieceBlocked
				piece.FailureReason = "attempt history repaired while merging duplicate Thread records"
				if thread.Status == ConversationThreadTask {
					thread.ExecState = ExecStateBlocked
				}
			}
		default:
			return fmt.Errorf("load merged active task attempt for node %q: %w", piece.ID, err)
		}
	}
	return nil
}

func rewriteResidentInboxSubthread(tx *sql.Tx, fromID, toID string) error {
	rows, err := tx.Query(`SELECT id, envelope_json FROM resident_inbox`)
	if err != nil {
		return fmt.Errorf("load resident inbox for conversation thread merge: %w", err)
	}
	type inboxUpdate struct{ id, payload string }
	var updates []inboxUpdate
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return fmt.Errorf("scan resident inbox for conversation thread merge: %w", err)
		}
		var envelope map[string]any
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			continue
		}
		if value, _ := envelope["source_subthread_id"].(string); value != fromID {
			continue
		}
		envelope["source_subthread_id"] = toID
		payload, err := json.Marshal(envelope)
		if err != nil {
			rows.Close()
			return fmt.Errorf("encode resident inbox %q for conversation thread merge: %w", id, err)
		}
		updates = append(updates, inboxUpdate{id: id, payload: string(payload)})
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close resident inbox for conversation thread merge: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan resident inbox for conversation thread merge: %w", err)
	}
	for _, update := range updates {
		if _, err := tx.Exec(`UPDATE resident_inbox SET envelope_json = ? WHERE id = ?`, update.payload, update.id); err != nil {
			return fmt.Errorf("rewrite resident inbox %q for conversation thread merge: %w", update.id, err)
		}
	}
	return nil
}

func mergeConversationThreadMigrationFields(dst *ConversationThread, src ConversationThread) {
	if dst.Title == "" {
		dst.Title = src.Title
	}
	if dst.CreatedBy == "" {
		dst.CreatedBy = src.CreatedBy
	}
	if dst.ThreadOwnerParticipantID == "" {
		dst.ThreadOwnerParticipantID = src.ThreadOwnerParticipantID
	}
	if dst.EscalatedAt.IsZero() {
		dst.EscalatedAt = src.EscalatedAt
	}
	if dst.EscalatedBy == "" {
		dst.EscalatedBy = src.EscalatedBy
	}
	if dst.Summary == "" {
		dst.Summary = src.Summary
	}
	if dst.LeadParticipantID == "" {
		dst.LeadParticipantID = src.LeadParticipantID
	}
	if len(dst.Plan) == 0 {
		dst.Plan = src.Plan
	}
	if dst.ExecState == "" {
		dst.ExecState = src.ExecState
	}
	if dst.ParentSeq <= 0 {
		dst.ParentSeq = src.ParentSeq
	}
	if dst.ParentAuthorParticipantID == "" {
		dst.ParentAuthorParticipantID = src.ParentAuthorParticipantID
	}
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
	_, err := db.Exec(insertSessionSQL(), sessionArgs(sess)...)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func insertSessionSQL() string {
	return `INSERT INTO sessions (
		id, created_at, updated_at, title, summary, entries, cwd,
		forked_from_id, forked_from_turn_id, forked_from_item_id,
		pinned_at, archived_at, worktree_path, worktree_base_head, worktree_base_repo,
		dm_participant_id, is_group, focus_workspace, workspace_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
}

func updateSessionTx(tx *sql.Tx, sess Session) error {
	_, err := tx.Exec(`
UPDATE sessions
SET created_at = ?, updated_at = ?, title = ?, summary = ?, entries = ?, cwd = ?,
    forked_from_id = ?, forked_from_turn_id = ?, forked_from_item_id = ?,
    pinned_at = ?, archived_at = ?, worktree_path = ?, worktree_base_head = ?, worktree_base_repo = ?,
    dm_participant_id = ?, is_group = ?, focus_workspace = ?, workspace_id = ?
WHERE id = ?`,
		timeText(sess.CreatedAt), timeText(sess.UpdatedAt), sess.Title, sess.Summary, sess.Entries, normalizeCWD(sess.CWD),
		sess.ForkedFromID, sess.ForkedFromTurnID, sess.ForkedFromItemID,
		nullableTimeText(sess.PinnedAt), nullableTimeText(sess.ArchivedAt),
		normalizeCWD(sess.WorktreePath), sess.WorktreeBaseHEAD, normalizeCWD(sess.WorktreeBaseRepo),
		strings.TrimSpace(sess.DMParticipantID), boolToInt(sess.Group), strings.TrimSpace(sess.FocusWorkspace), strings.TrimSpace(sess.WorkspaceID),
		sess.ID,
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
		normalizeCWD(sess.WorktreePath),
		sess.WorktreeBaseHEAD,
		normalizeCWD(sess.WorktreeBaseRepo),
		strings.TrimSpace(sess.DMParticipantID),
		boolToInt(sess.Group),
		strings.TrimSpace(sess.FocusWorkspace),
		strings.TrimSpace(sess.WorkspaceID),
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func findSessionDB(db *sql.DB, id string) (Session, bool, error) {
	row := db.QueryRow(`
SELECT id, created_at, updated_at, title, summary, entries, cwd,
       forked_from_id, forked_from_turn_id, forked_from_item_id,
       pinned_at, archived_at,
       worktree_path, worktree_base_head, worktree_base_repo,
       dm_participant_id, is_group, focus_workspace, workspace_id
FROM sessions
WHERE id = ?`, id)
	return scanSessionRow(row)
}

func findSessionTx(tx *sql.Tx, id string) (Session, bool, error) {
	row := tx.QueryRow(`
SELECT id, created_at, updated_at, title, summary, entries, cwd,
       forked_from_id, forked_from_turn_id, forked_from_item_id,
       pinned_at, archived_at,
       worktree_path, worktree_base_head, worktree_base_repo,
       dm_participant_id, is_group, focus_workspace, workspace_id
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
	var isGroup int
	if err := scanner.Scan(
		&s.ID, &createdAt, &updatedAt, &s.Title, &s.Summary, &s.Entries, &s.CWD,
		&s.ForkedFromID, &s.ForkedFromTurnID, &s.ForkedFromItemID,
		&pinnedAt, &archivedAt,
		&s.WorktreePath, &s.WorktreeBaseHEAD, &s.WorktreeBaseRepo,
		&s.DMParticipantID, &isGroup, &s.FocusWorkspace, &s.WorkspaceID,
	); err != nil {
		return Session{}, err
	}
	s.Group = isGroup != 0
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

// appendHistoryRecordTx inserts a record with the next per-session seq and
// returns that seq. seq is the message's stable address within the thread
// (used by message_marks for read receipts and reactions).
func appendHistoryRecordTx(tx *sql.Tx, id string, rec HistoryRecord) (int, error) {
	var nextSeq int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM session_messages WHERE session_id = ?`, id).Scan(&nextSeq); err != nil {
		return 0, fmt.Errorf("next history sequence: %w", err)
	}
	if err := insertHistoryRecordTx(tx, id, nextSeq, rec); err != nil {
		return 0, err
	}
	return nextSeq, nil
}

func insertHistoryRecordTx(tx *sql.Tx, id string, seq int, rec HistoryRecord) error {
	_, err := tx.Exec(`
			INSERT INTO session_messages (
				session_id, seq, role, content, display_content, phase, provider_item_id, provider_item_model, client_id, hidden, steered, reasoning_content,
				reasoning_blocks_json, images_json, files_json, tool_calls_json, discovered_tools_json,
				tool_call_id, tool_invocation_id, tool_result_kind, name, at, input_tokens, output_tokens, context_tokens, cache_creation_tokens, cache_read_tokens,
				provider, model, participant_id, post_kind, thread_id, basis_seq, envelope_meta, focus_meta
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, seq, strings.ToLower(strings.TrimSpace(rec.Role)), rec.Content, rec.DisplayContent, strings.TrimSpace(rec.Phase), strings.TrimSpace(rec.ProviderItemID), strings.TrimSpace(rec.ProviderItemModel), rec.ClientID, boolInt(rec.Hidden), boolInt(rec.Steered), rec.ReasoningContent,
		rawJSONText(rec.ReasoningBlocks), rawJSONText(rec.Images), rawJSONText(rec.Files), rawJSONText(rec.ToolCalls), rawJSONText(rec.DiscoveredTools),
		rec.ToolCallID, rec.ToolInvocationID, rec.ToolResultKind, rec.Name, nullableValueTimeText(rec.At), rec.InputTokens, rec.OutputTokens, rec.ContextTokens, rec.CacheCreationTokens, rec.CacheReadTokens,
		strings.TrimSpace(rec.Provider), strings.TrimSpace(rec.Model), strings.TrimSpace(rec.ParticipantID), strings.TrimSpace(rec.PostKind), strings.TrimSpace(rec.ThreadID), rec.BasisSeq, rawJSONText(rec.EnvelopeMeta), rawJSONText(rec.FocusMeta),
	)
	if err != nil {
		return fmt.Errorf("insert history record: %w", err)
	}
	if err := projectToolInvocationTx(tx, id, rec.ToolInvocationID); err != nil {
		return err
	}
	return nil
}

func projectToolInvocationTx(tx *sql.Tx, ownerID, invocationID string) error {
	invocationID = strings.TrimSpace(invocationID)
	if invocationID == "" {
		return nil
	}
	now := time.Now().UTC().UnixMilli()
	result, err := tx.Exec(`
UPDATE tool_invocations SET projected_at = MAX(projected_at, ?)
WHERE id = ? AND state IN ('succeeded', 'failed')
  AND batch_id IN (SELECT id FROM tool_batches WHERE owner_id = ?)`, now, invocationID, ownerID)
	if err != nil {
		return fmt.Errorf("project tool invocation: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("tool invocation %q is not settled for owner %q", invocationID, ownerID)
	}
	_, err = tx.Exec(`
UPDATE tool_batches SET status = 'projected', updated_at = ?, terminal_at = MAX(terminal_at, ?)
WHERE owner_id = ? AND status = 'settled'
  AND id = (SELECT batch_id FROM tool_invocations WHERE id = ?)
  AND NOT EXISTS (
    SELECT 1 FROM tool_invocations
    WHERE batch_id = tool_batches.id AND state IN ('succeeded', 'failed') AND projected_at = 0
  )`, now, now, ownerID, invocationID)
	if err != nil {
		return fmt.Errorf("project tool batch: %w", err)
	}
	return nil
}

const historyRecordsSelect = `
	SELECT seq, role, content, display_content, phase, client_id, hidden, steered, reasoning_content,
	       provider_item_id, provider_item_model,
	       reasoning_blocks_json, images_json, files_json, tool_calls_json, discovered_tools_json,
	       tool_call_id, tool_invocation_id, tool_result_kind, name, at, input_tokens, output_tokens, context_tokens, cache_creation_tokens, cache_read_tokens,
	       provider, model, participant_id, post_kind, thread_id, basis_seq, envelope_meta, focus_meta
	FROM session_messages`

func loadHistoryRecordsDB(db *sql.DB, id string, includeMeta bool) ([]HistoryRecord, error) {
	query := historyRecordsSelect + ` WHERE session_id = ?`
	args := []any{id}
	if !includeMeta {
		query += ` AND lower(role) <> 'meta'`
	}
	query += ` ORDER BY seq ASC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("load session history: %w", err)
	}
	return scanHistoryRecords(rows)
}

func scanHistoryRecords(rows *sql.Rows) ([]HistoryRecord, error) {
	defer rows.Close()

	var records []HistoryRecord
	for rows.Next() {
		var rec HistoryRecord
		var hidden, steered int
		var reasoningBlocks, images, files, toolCalls, discoveredTools, envelopeMeta, focusMeta string
		var at sql.NullString
		if err := rows.Scan(
			&rec.Seq,
			&rec.Role, &rec.Content, &rec.DisplayContent, &rec.Phase, &rec.ClientID, &hidden, &steered, &rec.ReasoningContent,
			&rec.ProviderItemID, &rec.ProviderItemModel,
			&reasoningBlocks, &images, &files, &toolCalls, &discoveredTools,
			&rec.ToolCallID, &rec.ToolInvocationID, &rec.ToolResultKind, &rec.Name, &at, &rec.InputTokens, &rec.OutputTokens, &rec.ContextTokens, &rec.CacheCreationTokens, &rec.CacheReadTokens,
			&rec.Provider, &rec.Model, &rec.ParticipantID, &rec.PostKind, &rec.ThreadID, &rec.BasisSeq, &envelopeMeta, &focusMeta,
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
		rec.EnvelopeMeta = rawMessage(envelopeMeta)
		rec.FocusMeta = rawMessage(focusMeta)
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
