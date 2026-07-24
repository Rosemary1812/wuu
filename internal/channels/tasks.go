package channels

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func (s *Service) CreateTask(ctx context.Context, params TaskCreateParams) (Message, error) {
	if _, err := s.AuthenticateAgent(ctx, params.AgentID, params.Token); err != nil {
		return Message{}, err
	}
	return s.createTask(ctx, params)
}

func (s *Service) CreateTaskHuman(ctx context.Context, params TaskCreateParams) (Message, error) {
	if err := s.requireHumanMember(ctx, params.RoomID, params.HumanID); err != nil {
		return Message{}, err
	}
	return s.createTask(ctx, params)
}

func (s *Service) createTask(ctx context.Context, params TaskCreateParams) (Message, error) {
	params.RoomID = strings.TrimSpace(params.RoomID)
	params.Title = strings.TrimSpace(params.Title)
	params.Body = strings.TrimSpace(params.Body)
	params.OwnerID = strings.TrimSpace(params.OwnerID)
	params.ThreadID = strings.TrimSpace(params.ThreadID)
	if params.RoomID == "" || params.Title == "" || params.OwnerID == "" {
		return Message{}, errors.New("task room, title and owner are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, fmt.Errorf("begin task create: %w", err)
	}
	defer tx.Rollback()
	if err := s.requireRoomAgentMemberTx(ctx, tx, params.RoomID, params.OwnerID); err != nil {
		return Message{}, err
	}
	if params.AgentID != "" {
		if err := s.requireRoomAgentMemberTx(ctx, tx, params.RoomID, params.AgentID); err != nil {
			return Message{}, err
		}
	}
	if params.HumanID != "" {
		if err := requireMemberTx(ctx, tx, params.RoomID, MemberHuman, params.HumanID); err != nil {
			return Message{}, err
		}
	}
	if params.ThreadID != "" {
		thread, err := loadMessageTx(ctx, tx, params.ThreadID)
		if err != nil {
			return Message{}, err
		}
		if thread.RoomID != params.RoomID {
			return Message{}, errors.New("task thread does not belong to room")
		}
		if thread.ThreadID != "" {
			return Message{}, errors.New("task thread target must be a root message")
		}
	}
	message, err := s.insertTaskMessageTx(ctx, tx, params)
	if err != nil {
		return Message{}, err
	}
	if err := s.taskEventInboxTx(ctx, tx, message, params.OwnerID, ""); err != nil {
		return Message{}, err
	}
	requested, err := s.requestWakeTx(ctx, tx, params.OwnerID, toMillis(message.CreatedAt))
	if err != nil {
		return Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return Message{}, fmt.Errorf("commit task create: %w", err)
	}
	if requested && s.wake != nil {
		s.wake.Deliver(params.OwnerID)
	}
	return message, nil
}

func (s *Service) UpdateTask(ctx context.Context, params TaskUpdateParams) (Message, error) {
	if _, err := s.AuthenticateAgent(ctx, params.AgentID, params.Token); err != nil {
		return Message{}, err
	}
	return s.updateTask(ctx, params)
}

func (s *Service) UpdateTaskHuman(ctx context.Context, params TaskUpdateParams) (Message, error) {
	params.HumanID = strings.TrimSpace(params.HumanID)
	if params.HumanID == "" {
		return Message{}, errors.New("human id is required")
	}
	return s.updateTask(ctx, params)
}

func (s *Service) updateTask(ctx context.Context, params TaskUpdateParams) (Message, error) {
	params.TaskID = strings.TrimSpace(params.TaskID)
	params.OwnerID = strings.TrimSpace(params.OwnerID)
	if params.TaskID == "" {
		return Message{}, errors.New("task id is required")
	}
	if params.State != "" && params.State != TaskStateOpen && params.State != TaskStateDoing && params.State != TaskStateDone {
		return Message{}, fmt.Errorf("invalid task state %q", params.State)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, fmt.Errorf("begin task update: %w", err)
	}
	defer tx.Rollback()
	message, err := loadMessageTx(ctx, tx, params.TaskID)
	if err != nil {
		return Message{}, err
	}
	if message.Kind != MessageTask {
		return Message{}, fmt.Errorf("%w: message %q is not a task", ErrConflict, params.TaskID)
	}
	if params.RoomID != "" && message.RoomID != params.RoomID {
		return Message{}, errors.New("task does not belong to room")
	}
	callerOk := false
	if params.AgentID != "" && message.TaskOwner == params.AgentID {
		callerOk = true
	}
	if params.HumanID != "" {
		if err := requireMemberTx(ctx, tx, message.RoomID, MemberHuman, params.HumanID); err == nil {
			callerOk = true
		}
	}
	if !callerOk {
		return Message{}, ErrUnauthorized
	}
	oldOwner := message.TaskOwner
	newOwner := message.TaskOwner
	if params.OwnerID != "" {
		if err := s.requireRoomAgentMemberTx(ctx, tx, message.RoomID, params.OwnerID); err != nil {
			return Message{}, err
		}
		newOwner = params.OwnerID
	}
	updatedAt := fromMillis(toMillis(s.now()))
	setState := message.TaskState
	if params.State != "" {
		setState = string(params.State)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE room_messages
		SET task_state = ?, task_owner = ?
		WHERE id = ?`,
		setState, newOwner, message.ID); err != nil {
		return Message{}, fmt.Errorf("update task message: %w", err)
	}
	message.TaskState = setState
	message.TaskOwner = newOwner
	if err := s.taskEventInboxTx(ctx, tx, message, newOwner, oldOwner); err != nil {
		return Message{}, err
	}
	requested, err := s.requestWakeTx(ctx, tx, newOwner, toMillis(updatedAt))
	if err != nil {
		return Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return Message{}, fmt.Errorf("commit task update: %w", err)
	}
	if requested && s.wake != nil {
		s.wake.Deliver(newOwner)
	}
	return message, nil
}

func (s *Service) ListTasks(ctx context.Context, params TaskListParams) ([]Message, error) {
	params.RoomID = strings.TrimSpace(params.RoomID)
	params.OwnerID = strings.TrimSpace(params.OwnerID)
	if params.AgentID != "" {
		if _, err := s.AuthenticateAgent(ctx, params.AgentID, params.Token); err != nil {
			return nil, err
		}
		if err := s.requireAgentMember(ctx, params.RoomID, params.AgentID); err != nil {
			return nil, err
		}
	} else if params.HumanID != "" {
		if err := s.requireHumanMember(ctx, params.RoomID, params.HumanID); err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("task list caller is required")
	}
	query := `
		SELECT id, room_id, seq, COALESCE(thread_id, ''), author_type, author_id,
			kind, body, mentions_json, COALESCE(reply_to, ''),
			COALESCE(task_title, ''), COALESCE(task_state, ''), COALESCE(task_owner, ''), created_at
		FROM room_messages WHERE room_id = ? AND kind = 'task'`
	args := []any{params.RoomID}
	if params.OwnerID != "" {
		query += ` AND task_owner = ?`
		args = append(args, params.OwnerID)
	}
	query += ` ORDER BY created_at, id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	messages := make([]Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return messages, nil
}

func (s *Service) insertTaskMessageTx(ctx context.Context, tx *sql.Tx, params TaskCreateParams) (Message, error) {
	var seq int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(seq), 0) + 1 FROM room_messages WHERE room_id = ?`, params.RoomID,
	).Scan(&seq); err != nil {
		return Message{}, fmt.Errorf("allocate task sequence: %w", err)
	}
	id, err := randomID("msg", 12)
	if err != nil {
		return Message{}, err
	}
	now := fromMillis(toMillis(s.now()))
	message := Message{
		ID:         id,
		RoomID:     params.RoomID,
		Seq:        seq,
		ThreadID:   params.ThreadID,
		AuthorType: MemberAgent,
		AuthorID:   params.AgentID,
		Kind:       MessageTask,
		Body:       params.Body,
		TaskTitle:  params.Title,
		TaskState:  string(TaskStateOpen),
		TaskOwner:  params.OwnerID,
		CreatedAt:  now,
	}
	if params.AgentID == "" && params.HumanID != "" {
		message.AuthorType = MemberHuman
		message.AuthorID = params.HumanID
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO room_messages (
			id, room_id, seq, thread_id, author_type, author_id, kind, body,
			mentions_json, reply_to, task_title, task_state, task_owner, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		message.ID, message.RoomID, message.Seq, nullableString(message.ThreadID),
		message.AuthorType, message.AuthorID, message.Kind, message.Body,
		"[]", nullableString(message.ReplyTo), nullableString(message.TaskTitle),
		nullableString(message.TaskState), nullableString(message.TaskOwner), toMillis(message.CreatedAt)); err != nil {
		return Message{}, fmt.Errorf("insert task message: %w", err)
	}
	return message, nil
}

func (s *Service) taskEventInboxTx(ctx context.Context, tx *sql.Tx, message Message, newOwner, oldOwner string) error {
	threadRoot := message.ThreadID
	if threadRoot == "" {
		threadRoot = message.ID
	}
	now := toMillis(message.CreatedAt)
	if newOwner != "" {
		if err := insertInboxTx(ctx, tx, MemberAgent, newOwner, message.RoomID, message.ID, InboxTask, now); err != nil {
			return err
		}
	}
	if oldOwner != "" && oldOwner != newOwner {
		if err := insertInboxTx(ctx, tx, MemberAgent, oldOwner, message.RoomID, message.ID, InboxTask, now); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT author_id FROM room_messages
		WHERE room_id = ? AND author_type = 'agent' AND (id = ? OR thread_id = ?)`,
		message.RoomID, threadRoot, threadRoot)
	if err != nil {
		return fmt.Errorf("list task thread participants: %w", err)
	}
	var participants []string
	for rows.Next() {
		var agentID string
		if err := rows.Scan(&agentID); err != nil {
			rows.Close()
			return fmt.Errorf("scan task thread participant: %w", err)
		}
		participants = append(participants, agentID)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close task thread participants: %w", err)
	}
	sort.Strings(participants)
	for _, agentID := range participants {
		if agentID == newOwner || agentID == oldOwner {
			continue
		}
		if err := insertInboxTx(ctx, tx, MemberAgent, agentID, message.RoomID, message.ID, InboxTask, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) requireRoomAgentMemberTx(ctx context.Context, tx *sql.Tx, roomID, agentID string) error {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1 FROM room_members WHERE room_id = ? AND member_type = 'agent' AND member_id = ?`,
		roomID, agentID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: agent %q is not a member of room %q", ErrUnauthorized, agentID, roomID)
	}
	if err != nil {
		return fmt.Errorf("validate agent room membership: %w", err)
	}
	return nil
}

func (s *Service) requireAgentMember(ctx context.Context, roomID, agentID string) error {
	var exists int
	err := s.db.QueryRowContext(ctx, `
		SELECT 1 FROM room_members WHERE room_id = ? AND member_type = 'agent' AND member_id = ?`,
		roomID, agentID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: agent %q is not a member of room %q", ErrUnauthorized, agentID, roomID)
	}
	if err != nil {
		return fmt.Errorf("validate agent room membership: %w", err)
	}
	return nil
}

func (s *Service) requireHumanMember(ctx context.Context, roomID, humanID string) error {
	if roomID == "" || humanID == "" {
		return errors.New("room and human id are required")
	}
	var exists int
	err := s.db.QueryRowContext(ctx, `
		SELECT 1 FROM room_members WHERE room_id = ? AND member_type = 'human' AND member_id = ?`,
		roomID, humanID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: human %q is not a member of room %q", ErrUnauthorized, humanID, roomID)
	}
	if err != nil {
		return fmt.Errorf("validate human room membership: %w", err)
	}
	return nil
}

func (s *Service) requestWakeTx(ctx context.Context, tx *sql.Tx, agentID string, now int64) (bool, error) {
	var requested int
	err := tx.QueryRowContext(ctx, `
		UPDATE agent_wake_state SET outstanding = 1, updated_at = ?
		WHERE agent_id = ? AND outstanding = 0
		RETURNING 1`, now, agentID).Scan(&requested)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("request agent wake: %w", err)
	}
	return true, nil
}
