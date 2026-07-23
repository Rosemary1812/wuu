package channels

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/securefs"
	_ "modernc.org/sqlite"
)

const (
	databaseFileName = "channels.sqlite3"
	agentTokenFile   = ".chat-token"
)

var (
	ErrNotFound     = errors.New("channels record not found")
	ErrConflict     = errors.New("channels record conflict")
	ErrUnauthorized = errors.New("channels authentication failed")
)

type Service struct {
	dir  string
	db   *sql.DB
	wake WakeSink
	now  func() time.Time
	mu   sync.Mutex

	telemetryMu sync.RWMutex
	telemetry   TelemetrySink
}

func Open(dir string, wake WakeSink) (*Service, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("channels state directory is required")
	}
	if err := securefs.Mkdir(dir); err != nil {
		return nil, fmt.Errorf("create channels state directory: %w", err)
	}
	dbPath := filepath.Join(dir, databaseFileName)
	if err := securefs.PreCreateFile(dbPath); err != nil {
		return nil, fmt.Errorf("precreate channels database: %w", err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("open channels database: %w", err)
	}
	db.SetMaxOpenConns(1)
	service := &Service{
		dir:  dir,
		db:   db,
		wake: wake,
		now:  func() time.Time { return time.Now().UTC() },
	}
	if err := service.configure(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := service.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return service, nil
}

func (s *Service) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Service) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

func (s *Service) SetWakeSink(wake WakeSink) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.wake = wake
	s.mu.Unlock()
}

func (s *Service) SetTelemetrySink(sink TelemetrySink) {
	if s == nil {
		return
	}
	s.telemetryMu.Lock()
	s.telemetry = sink
	s.telemetryMu.Unlock()
}

func (s *Service) emitTelemetry(event TelemetryEvent) {
	if s == nil {
		return
	}
	s.telemetryMu.RLock()
	sink := s.telemetry
	s.telemetryMu.RUnlock()
	if sink != nil {
		sink.RecordChannelEvent(event)
	}
}

func (s *Service) configure() error {
	for _, statement := range []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = NORMAL`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("configure channels database: %w", err)
		}
	}
	return nil
}

func (s *Service) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS named_agents (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL COLLATE NOCASE,
			memory_dir TEXT NOT NULL,
			model_override TEXT,
			token_hash TEXT NOT NULL,
			autostart INTEGER NOT NULL DEFAULT 0 CHECK (autostart IN (0, 1)),
			created_at INTEGER NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_named_agents_name ON named_agents(name)`,
		`CREATE TABLE IF NOT EXISTS rooms (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL CHECK (kind IN ('channel', 'dm')),
			name TEXT NOT NULL,
			created_by TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS room_members (
			room_id TEXT NOT NULL,
			member_type TEXT NOT NULL CHECK (member_type IN ('human', 'agent')),
			member_id TEXT NOT NULL,
			joined_at INTEGER NOT NULL,
			PRIMARY KEY (room_id, member_type, member_id),
			FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_room_members_member ON room_members(member_type, member_id, room_id)`,
		`CREATE TABLE IF NOT EXISTS room_messages (
			id TEXT PRIMARY KEY,
			room_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			thread_id TEXT,
			author_type TEXT NOT NULL CHECK (author_type IN ('human', 'agent')),
			author_id TEXT NOT NULL,
			kind TEXT NOT NULL CHECK (kind IN ('text', 'task', 'system')),
			body TEXT NOT NULL,
			mentions_json TEXT NOT NULL DEFAULT '[]',
			reply_to TEXT,
			task_title TEXT,
			task_state TEXT,
			task_owner TEXT,
			created_at INTEGER NOT NULL,
			UNIQUE (room_id, seq),
			FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE,
			FOREIGN KEY (thread_id) REFERENCES room_messages(id),
			FOREIGN KEY (reply_to) REFERENCES room_messages(id),
			FOREIGN KEY (task_owner) REFERENCES named_agents(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_room_messages_room_seq ON room_messages(room_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_room_messages_thread_seq ON room_messages(room_id, thread_id, seq)`,
		`CREATE TABLE IF NOT EXISTS room_cursors (
			room_id TEXT NOT NULL,
			member_type TEXT NOT NULL DEFAULT 'agent' CHECK (member_type IN ('human', 'agent')),
			member_id TEXT NOT NULL,
			last_read_seq INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (room_id, member_type, member_id),
			FOREIGN KEY (room_id, member_type, member_id) REFERENCES room_members(room_id, member_type, member_id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS inbox_items (
			id TEXT PRIMARY KEY,
			member_type TEXT NOT NULL DEFAULT 'agent' CHECK (member_type IN ('human', 'agent')),
			member_id TEXT NOT NULL,
			room_id TEXT,
			message_id TEXT,
			reminder_id TEXT,
			kind TEXT NOT NULL CHECK (kind IN ('mention', 'reply', 'thread_update', 'task', 'reminder')),
			created_at INTEGER NOT NULL,
			pulled_at INTEGER,
			FOREIGN KEY (room_id, member_type, member_id) REFERENCES room_members(room_id, member_type, member_id) ON DELETE CASCADE,
			FOREIGN KEY (message_id) REFERENCES room_messages(id) ON DELETE CASCADE,
			FOREIGN KEY (reminder_id) REFERENCES reminders(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS agent_wake_state (
			agent_id TEXT PRIMARY KEY,
			outstanding INTEGER NOT NULL DEFAULT 0 CHECK (outstanding IN (0, 1)),
			pending INTEGER NOT NULL DEFAULT 0 CHECK (pending IN (0, 1)),
			updated_at INTEGER NOT NULL,
			FOREIGN KEY (agent_id) REFERENCES named_agents(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS drafts (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			room_id TEXT NOT NULL,
			thread_id TEXT,
			body TEXT NOT NULL,
			basis_seq INTEGER NOT NULL,
			hold_count INTEGER NOT NULL DEFAULT 1,
			state TEXT NOT NULL CHECK (state IN ('held', 'dropped', 'committed', 'expired')),
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			FOREIGN KEY (agent_id) REFERENCES named_agents(id) ON DELETE CASCADE,
			FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE,
			FOREIGN KEY (thread_id) REFERENCES room_messages(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_drafts_agent_state ON drafts(agent_id, state, updated_at)`,
		`CREATE TABLE IF NOT EXISTS reminders (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			fire_at INTEGER NOT NULL,
			note TEXT NOT NULL,
			room_id TEXT,
			thread_id TEXT,
			state TEXT NOT NULL CHECK (state IN ('pending', 'fired', 'cancelled')),
			created_at INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (agent_id) REFERENCES named_agents(id) ON DELETE CASCADE,
			FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE,
			FOREIGN KEY (thread_id) REFERENCES room_messages(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_reminders_due ON reminders(state, fire_at, id)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("migrate channels database: %w", err)
		}
	}
	if err := s.migrateInboxItems(); err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_inbox_items_agent_pull ON inbox_items(member_type, member_id, pulled_at, created_at, id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_inbox_items_unique ON inbox_items(member_type, member_id, COALESCE(message_id,''), COALESCE(reminder_id,''), kind)`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("migrate channels inbox index: %w", err)
		}
	}
	return nil
}

func (s *Service) migrateInboxItems() error {
	rows, err := s.db.Query(`PRAGMA table_info(inbox_items)`)
	if err != nil {
		return fmt.Errorf("inspect channels inbox schema: %w", err)
	}
	columns := make(map[string]bool)
	roomIDNotNull := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("scan channels inbox schema: %w", err)
		}
		columns[name] = true
		roomIDNotNull = roomIDNotNull || name == "room_id" && notNull != 0
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close channels inbox schema: %w", err)
	}
	if len(columns) == 0 || columns["member_type"] && columns["member_id"] && columns["reminder_id"] && !roomIDNotNull {
		return nil
	}

	memberTypeExpr := "'agent'"
	if columns["member_type"] {
		memberTypeExpr = "member_type"
	}
	memberIDExpr := ""
	if columns["member_id"] {
		memberIDExpr = "member_id"
	} else if columns["agent_id"] {
		memberIDExpr = "agent_id"
	} else {
		return errors.New("legacy inbox_items has no member identity column")
	}
	expr := func(column, fallback string) string {
		if columns[column] {
			return column
		}
		return fallback
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin channels inbox migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`ALTER TABLE inbox_items RENAME TO inbox_items_legacy`); err != nil {
		return fmt.Errorf("rename legacy channels inbox: %w", err)
	}
	if _, err := tx.Exec(`
		CREATE TABLE inbox_items (
			id TEXT PRIMARY KEY,
			member_type TEXT NOT NULL DEFAULT 'agent' CHECK (member_type IN ('human', 'agent')),
			member_id TEXT NOT NULL,
			room_id TEXT,
			message_id TEXT,
			reminder_id TEXT,
			kind TEXT NOT NULL CHECK (kind IN ('mention', 'reply', 'thread_update', 'task', 'reminder')),
			created_at INTEGER NOT NULL,
			pulled_at INTEGER,
			FOREIGN KEY (room_id, member_type, member_id) REFERENCES room_members(room_id, member_type, member_id) ON DELETE CASCADE,
			FOREIGN KEY (message_id) REFERENCES room_messages(id) ON DELETE CASCADE,
			FOREIGN KEY (reminder_id) REFERENCES reminders(id) ON DELETE CASCADE
		)`); err != nil {
		return fmt.Errorf("create upgraded channels inbox: %w", err)
	}
	copySQL := fmt.Sprintf(`
		INSERT INTO inbox_items(
			id, member_type, member_id, room_id, message_id, reminder_id, kind, created_at, pulled_at
		)
		SELECT id, %s, %s, %s, %s, %s, %s, %s, %s
		FROM inbox_items_legacy`,
		memberTypeExpr, memberIDExpr, expr("room_id", "NULL"), expr("message_id", "NULL"),
		expr("reminder_id", "NULL"), expr("kind", "'mention'"), expr("created_at", "0"), expr("pulled_at", "NULL"))
	if _, err := tx.Exec(copySQL); err != nil {
		return fmt.Errorf("copy upgraded channels inbox: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE inbox_items_legacy`); err != nil {
		return fmt.Errorf("drop legacy channels inbox: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit channels inbox migration: %w", err)
	}
	return nil
}

func (s *Service) CreateNamedAgent(ctx context.Context, params CreateNamedAgentParams) (AgentCredential, error) {
	if s == nil || s.db == nil {
		return AgentCredential{}, errors.New("channels service is not configured")
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return AgentCredential{}, errors.New("named agent name is required")
	}
	if len([]rune(name)) > 64 {
		return AgentCredential{}, errors.New("named agent name exceeds 64 characters")
	}
	id, err := randomID("agent", 12)
	if err != nil {
		return AgentCredential{}, err
	}
	token, err := randomID("chat", 32)
	if err != nil {
		return AgentCredential{}, err
	}
	now := fromMillis(toMillis(s.now()))
	agent := NamedAgent{
		ID:            id,
		Name:          name,
		MemoryDir:     filepath.Join(s.dir, "agents", id, "memory"),
		ModelOverride: strings.TrimSpace(params.ModelOverride),
		Autostart:     params.Autostart,
		CreatedAt:     now,
	}
	if err := securefs.Mkdir(agent.MemoryDir); err != nil {
		return AgentCredential{}, fmt.Errorf("create named agent memory directory: %w", err)
	}
	if err := securefs.WriteFileAtomic(filepath.Join(filepath.Dir(agent.MemoryDir), agentTokenFile), []byte(token+"\n")); err != nil {
		return AgentCredential{}, fmt.Errorf("persist named agent token: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AgentCredential{}, fmt.Errorf("begin named agent create: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO named_agents (id, name, memory_dir, model_override, token_hash, autostart, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		agent.ID, agent.Name, agent.MemoryDir, nullableString(agent.ModelOverride), tokenHash(token), boolInt(agent.Autostart), toMillis(now),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return AgentCredential{}, fmt.Errorf("%w: named agent %q already exists", ErrConflict, name)
		}
		return AgentCredential{}, fmt.Errorf("insert named agent: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_wake_state (agent_id, outstanding, pending, updated_at)
		VALUES (?, 0, 0, ?)`, agent.ID, toMillis(now)); err != nil {
		return AgentCredential{}, fmt.Errorf("initialize named agent wake state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return AgentCredential{}, fmt.Errorf("commit named agent create: %w", err)
	}
	return AgentCredential{Agent: agent, Token: token}, nil
}

func (s *Service) GetNamedAgent(ctx context.Context, id string) (NamedAgent, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return NamedAgent{}, errors.New("named agent id is required")
	}
	return scanNamedAgent(s.db.QueryRowContext(ctx, `
		SELECT id, name, memory_dir, COALESCE(model_override, ''), autostart, created_at
		FROM named_agents WHERE id = ?`, id))
}

func (s *Service) ListNamedAgents(ctx context.Context) ([]NamedAgent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, memory_dir, COALESCE(model_override, ''), autostart, created_at
		FROM named_agents ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list named agents: %w", err)
	}
	defer rows.Close()
	agents := make([]NamedAgent, 0)
	for rows.Next() {
		agent, err := scanNamedAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list named agents: %w", err)
	}
	return agents, nil
}

func (s *Service) AuthenticateAgent(ctx context.Context, agentID, token string) (NamedAgent, error) {
	agentID = strings.TrimSpace(agentID)
	token = strings.TrimSpace(token)
	if agentID == "" || token == "" {
		return NamedAgent{}, ErrUnauthorized
	}
	var storedHash string
	var agent NamedAgent
	var autostart int
	var createdAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, memory_dir, COALESCE(model_override, ''), autostart, created_at, token_hash
		FROM named_agents WHERE id = ?`, agentID,
	).Scan(&agent.ID, &agent.Name, &agent.MemoryDir, &agent.ModelOverride, &autostart, &createdAt, &storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return NamedAgent{}, ErrUnauthorized
	}
	if err != nil {
		return NamedAgent{}, fmt.Errorf("authenticate named agent: %w", err)
	}
	actual := tokenHash(token)
	if len(actual) != len(storedHash) || subtle.ConstantTimeCompare([]byte(actual), []byte(storedHash)) != 1 {
		return NamedAgent{}, ErrUnauthorized
	}
	agent.Autostart = autostart != 0
	agent.CreatedAt = fromMillis(createdAt)
	return agent, nil
}

func (s *Service) loadAgentToken(ctx context.Context, agentID string) (string, error) {
	agent, err := s.GetNamedAgent(ctx, agentID)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(agent.MemoryDir), agentTokenFile))
	if err != nil {
		return "", fmt.Errorf("read named agent token: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if _, err := s.AuthenticateAgent(ctx, agent.ID, token); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Service) CreateRoom(ctx context.Context, params CreateRoomParams) (Room, error) {
	if params.Kind != RoomChannel && params.Kind != RoomDM {
		return Room{}, fmt.Errorf("invalid room kind %q", params.Kind)
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return Room{}, errors.New("room name is required")
	}
	createdBy := strings.TrimSpace(params.CreatedBy)
	if createdBy == "" {
		return Room{}, errors.New("room creator is required")
	}
	members, err := normalizeMembers(params.Members, createdBy)
	if err != nil {
		return Room{}, err
	}
	if params.Kind == RoomDM && len(members) != 2 {
		return Room{}, errors.New("dm rooms require exactly two members")
	}
	id, err := randomID("room", 12)
	if err != nil {
		return Room{}, err
	}
	now := fromMillis(toMillis(s.now()))
	room := Room{ID: id, Kind: params.Kind, Name: name, CreatedBy: createdBy, CreatedAt: now}
	for index := range members {
		members[index].RoomID = id
		members[index].JoinedAt = now
	}
	room.Members = members

	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Room{}, fmt.Errorf("begin room create: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO rooms (id, kind, name, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		room.ID, room.Kind, room.Name, room.CreatedBy, toMillis(room.CreatedAt)); err != nil {
		return Room{}, fmt.Errorf("insert room: %w", err)
	}
	for _, member := range room.Members {
		if member.MemberType == MemberAgent {
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT 1 FROM named_agents WHERE id = ?`, member.MemberID).Scan(&exists); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return Room{}, fmt.Errorf("%w: named agent %q", ErrNotFound, member.MemberID)
				}
				return Room{}, fmt.Errorf("validate room agent member: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO room_members (room_id, member_type, member_id, joined_at) VALUES (?, ?, ?, ?)`,
			room.ID, member.MemberType, member.MemberID, toMillis(member.JoinedAt)); err != nil {
			return Room{}, fmt.Errorf("insert room member: %w", err)
		}
		if member.MemberType == MemberAgent {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO room_cursors (room_id, member_type, member_id, last_read_seq) VALUES (?, ?, ?, 0)`, room.ID, string(member.MemberType), member.MemberID); err != nil {
				return Room{}, fmt.Errorf("initialize room cursor: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return Room{}, fmt.Errorf("commit room create: %w", err)
	}
	return room, nil
}

func (s *Service) GetRoom(ctx context.Context, id string) (Room, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Room{}, errors.New("room id is required")
	}
	var room Room
	var createdAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, kind, name, created_by, created_at FROM rooms WHERE id = ?`, id,
	).Scan(&room.ID, &room.Kind, &room.Name, &room.CreatedBy, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Room{}, fmt.Errorf("%w: room %q", ErrNotFound, id)
	}
	if err != nil {
		return Room{}, fmt.Errorf("get room: %w", err)
	}
	room.CreatedAt = fromMillis(createdAt)
	rows, err := s.db.QueryContext(ctx, `
		SELECT member_type, member_id, joined_at FROM room_members
		WHERE room_id = ? ORDER BY joined_at, member_type, member_id`, id)
	if err != nil {
		return Room{}, fmt.Errorf("list room members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var member RoomMember
		var joinedAt int64
		if err := rows.Scan(&member.MemberType, &member.MemberID, &joinedAt); err != nil {
			return Room{}, fmt.Errorf("scan room member: %w", err)
		}
		member.RoomID = id
		member.JoinedAt = fromMillis(joinedAt)
		room.Members = append(room.Members, member)
	}
	if err := rows.Err(); err != nil {
		return Room{}, fmt.Errorf("list room members: %w", err)
	}
	return room, nil
}

func (s *Service) ListRooms(ctx context.Context) ([]Room, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM rooms ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan room: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close room list: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	rooms := make([]Room, 0, len(ids))
	for _, id := range ids {
		room, err := s.GetRoom(ctx, id)
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, room)
	}
	return rooms, nil
}

func (s *Service) WakeState(ctx context.Context, agentID string) (WakeState, error) {
	var state WakeState
	var outstanding, pending int
	var updatedAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT agent_id, outstanding, pending, updated_at FROM agent_wake_state WHERE agent_id = ?`,
		strings.TrimSpace(agentID),
	).Scan(&state.AgentID, &outstanding, &pending, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WakeState{}, fmt.Errorf("%w: named agent %q", ErrNotFound, agentID)
	}
	if err != nil {
		return WakeState{}, fmt.Errorf("get wake state: %w", err)
	}
	state.Outstanding = outstanding != 0
	state.Pending = pending != 0
	state.UpdatedAt = fromMillis(updatedAt)
	return state, nil
}

func sqliteDSN(path string) string {
	u := url.URL{Scheme: "file", Path: path}
	query := u.Query()
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	query.Set("_txlock", "immediate")
	u.RawQuery = query.Encode()
	return u.String()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanNamedAgent(row scanner) (NamedAgent, error) {
	var agent NamedAgent
	var autostart int
	var createdAt int64
	if err := row.Scan(&agent.ID, &agent.Name, &agent.MemoryDir, &agent.ModelOverride, &autostart, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NamedAgent{}, ErrNotFound
		}
		return NamedAgent{}, fmt.Errorf("scan named agent: %w", err)
	}
	agent.Autostart = autostart != 0
	agent.CreatedAt = fromMillis(createdAt)
	return agent, nil
}

func normalizeMembers(input []RoomMember, createdBy string) ([]RoomMember, error) {
	members := make([]RoomMember, 0, len(input)+1)
	seen := make(map[string]struct{}, len(input)+1)
	add := func(memberType MemberType, memberID string) error {
		memberID = strings.TrimSpace(memberID)
		if memberType != MemberHuman && memberType != MemberAgent {
			return fmt.Errorf("invalid room member type %q", memberType)
		}
		if memberID == "" {
			return errors.New("room member id is required")
		}
		key := string(memberType) + "\x00" + memberID
		if _, ok := seen[key]; ok {
			return nil
		}
		seen[key] = struct{}{}
		members = append(members, RoomMember{MemberType: memberType, MemberID: memberID})
		return nil
	}
	if err := add(MemberHuman, createdBy); err != nil {
		return nil, err
	}
	for _, member := range input {
		if err := add(member.MemberType, member.MemberID); err != nil {
			return nil, err
		}
	}
	if len(members) > MaxRoomMembers {
		return nil, fmt.Errorf("room member limit is %d", MaxRoomMembers)
	}
	return members, nil
}

func randomID(prefix string, byteCount int) (string, error) {
	buffer := make([]byte, byteCount)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(buffer), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func toMillis(value time.Time) int64 {
	return value.UTC().UnixMilli()
}

func fromMillis(value int64) time.Time {
	return time.UnixMilli(value).UTC()
}

func isUniqueConstraint(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

func encodeMentions(mentions []string) (string, error) {
	data, err := json.Marshal(mentions)
	if err != nil {
		return "", fmt.Errorf("encode message mentions: %w", err)
	}
	return string(data), nil
}
