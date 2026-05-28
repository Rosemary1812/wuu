package session

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/blueberrycongee/wuu/internal/statepath"
)

// withIndexLock serializes access to the session index file across
// processes. Two concurrent wuu sessions in the same workspace can
// otherwise race between appendIndex and UpdateIndex's truncate-rewrite,
// losing session entries. Blocking is fine — index operations are brief.
func withIndexLock(sessDir string, exclusive bool, fn func() error) error {
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		return fmt.Errorf("create sessions dir: %w", err)
	}
	lockPath := filepath.Join(sessDir, ".index.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open index lock: %w", err)
	}
	defer f.Close()
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(f.Fd()), mode); err != nil {
		return fmt.Errorf("acquire index lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

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

// NewID generates a human-readable, sortable session ID: YYYYMMDD-HHMMSS-xxxxxxxxxxxxxxxx.
func NewID() string {
	b := make([]byte, 8)
	rand.Read(b)
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

// FilePath returns the data file path for a session ID.
func FilePath(sessDir, id string) string {
	return filepath.Join(sessDir, id+".jsonl")
}

// IndexPath returns the index file path.
func IndexPath(sessDir string) string {
	return filepath.Join(sessDir, "index.jsonl")
}

// Create initializes a new session: creates the directory, data file, and index entry.
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
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}

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

	// Hold the index lock for both the data-file create and the index
	// append so a concurrent UpdateIndex cannot snapshot the index
	// between the two and rewrite it without this session's entry.
	if err := withIndexLock(sessDir, true, func() error {
		dataPath := FilePath(sessDir, sess.ID)
		f, err := os.Create(dataPath)
		if err != nil {
			return fmt.Errorf("create session file: %w", err)
		}
		f.Close()
		return appendIndexLocked(sessDir, sess)
	}); err != nil {
		return nil, err
	}
	return sess, nil
}

// List reads the index and returns the most recent sessions (up to limit).
func List(sessDir string, limit int) ([]Session, error) {
	var sessions []Session
	err := withIndexLock(sessDir, false, func() error {
		var err error
		sessions, err = listLocked(sessDir, limit)
		return err
	})
	return sessions, err
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

// listLocked reads the index assuming the caller already holds the lock.
func listLocked(sessDir string, limit int) ([]Session, error) {
	sessions, err := readIndexEntriesLocked(sessDir, true)
	if err != nil {
		return nil, err
	}

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

	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}
	return sessions, nil
}

func readIndexEntriesLocked(sessDir string, backfillUpdatedAt bool) ([]Session, error) {
	indexPath := IndexPath(sessDir)
	f, err := os.Open(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open index: %w", err)
	}
	defer f.Close()

	var sessions []Session
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var s Session
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue // skip corrupt lines
		}
		if backfillUpdatedAt && s.UpdatedAt.IsZero() {
			if info, err := os.Stat(FilePath(sessDir, s.ID)); err == nil {
				s.UpdatedAt = info.ModTime().UTC()
			}
		}
		sessions = append(sessions, s)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan index: %w", err)
	}
	return sessions, nil
}

// Load returns the data file path for a session ID, verifying it exists.
func Load(sessDir, id string) (string, error) {
	path := FilePath(sessDir, id)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("session %q not found", id)
	}
	return path, nil
}

// Find returns metadata for a session ID from the index.
func Find(sessDir, id string) (Session, bool, error) {
	var found Session
	ok := false
	err := withIndexLock(sessDir, false, func() error {
		sessions, err := listLocked(sessDir, 0)
		if err != nil {
			return err
		}
		for _, s := range sessions {
			if s.ID == id {
				found = s
				ok = true
				return nil
			}
		}
		return nil
	})
	return found, ok, err
}

// UpdateIndex updates the entries count and summary for a session in the index.
func UpdateIndex(sessDir string, id string, entries int, summary string) error {
	now := time.Now().UTC()
	_, err := updateMetadata(sessDir, id, true, func(s *Session) {
		s.Entries = entries
		s.UpdatedAt = now
		if summary != "" && s.Summary == "" {
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

// UpdatePinned marks a session as pinned or unpinned in the index.
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

// UpdateArchived marks a session as archived or active in the index.
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

func updateMetadata(sessDir, id string, missingOK bool, update func(*Session)) (Session, error) {
	var updated Session
	err := withIndexLock(sessDir, true, func() error {
		sessions, err := readIndexEntriesLocked(sessDir, false)
		if err != nil {
			return err
		}

		found := false
		for i := range sessions {
			if sessions[i].ID == id {
				update(&sessions[i])
				updated = sessions[i]
				found = true
				break
			}
		}
		if !found {
			if missingOK {
				return nil
			}
			return fmt.Errorf("session %q not found", id)
		}

		// Sort chronologically for stable output, then write to a
		// temp file + rename so a crash mid-write can't leave a
		// truncated index.
		sort.Slice(sessions, func(i, j int) bool {
			return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
		})
		indexPath := IndexPath(sessDir)
		tmp, err := os.CreateTemp(sessDir, ".index.*.tmp")
		if err != nil {
			return fmt.Errorf("rewrite index: %w", err)
		}
		tmpName := tmp.Name()
		enc := json.NewEncoder(tmp)
		for _, s := range sessions {
			if err := enc.Encode(s); err != nil {
				tmp.Close()
				os.Remove(tmpName)
				return fmt.Errorf("write index: %w", err)
			}
		}
		if err := tmp.Close(); err != nil {
			os.Remove(tmpName)
			return fmt.Errorf("close index tmp: %w", err)
		}
		if err := os.Rename(tmpName, indexPath); err != nil {
			os.Remove(tmpName)
			return fmt.Errorf("rename index: %w", err)
		}
		return nil
	})
	if err != nil {
		return Session{}, err
	}
	return updated, nil
}

func sessionActivityAt(s Session) time.Time {
	if !s.UpdatedAt.IsZero() {
		return s.UpdatedAt
	}
	return s.CreatedAt
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

// appendIndexLocked appends to the index assuming the caller already holds
// the exclusive lock (via withIndexLock). O_APPEND is atomic for small
// writes, but the Create→append sequence in Create must be atomic as a
// whole against UpdateIndex, which is why the lock is required.
func appendIndexLocked(sessDir string, sess *Session) error {
	indexPath := IndexPath(sessDir)
	f, err := os.OpenFile(indexPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open index for append: %w", err)
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(sess)
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
