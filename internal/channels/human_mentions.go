package channels

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func (s *Service) HumanMentionStatus(ctx context.Context, humanID string) ([]HumanMentionCount, error) {
	humanID = strings.TrimSpace(humanID)
	if humanID == "" {
		return nil, fmt.Errorf("human id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT room_id, COUNT(*)
		FROM inbox_items
		WHERE member_type = 'human' AND member_id = ? AND kind = 'mention' AND pulled_at IS NULL
		GROUP BY room_id
		ORDER BY room_id`, humanID)
	if err != nil {
		return nil, fmt.Errorf("query human mention status: %w", err)
	}
	defer rows.Close()
	counts := make([]HumanMentionCount, 0)
	for rows.Next() {
		var count HumanMentionCount
		if err := rows.Scan(&count.RoomID, &count.UnreadCount); err != nil {
			return nil, fmt.Errorf("scan human mention status: %w", err)
		}
		counts = append(counts, count)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate human mention status: %w", err)
	}
	return counts, nil
}

func (s *Service) ListHumanMentions(ctx context.Context, humanID, roomID string) ([]HumanMentionItem, error) {
	humanID = strings.TrimSpace(humanID)
	roomID = strings.TrimSpace(roomID)
	if humanID == "" {
		return nil, fmt.Errorf("human id is required")
	}
	query := `
		SELECT inbox.id, inbox.member_id, inbox.room_id, inbox.message_id,
			message.author_type, message.author_id, message.body, inbox.created_at
		FROM inbox_items inbox
		JOIN room_messages message ON message.id = inbox.message_id
		WHERE inbox.member_type = 'human' AND inbox.member_id = ? AND inbox.kind = 'mention'`
	args := []any{humanID}
	if roomID != "" {
		query += ` AND inbox.room_id = ?`
		args = append(args, roomID)
	}
	query += ` AND inbox.pulled_at IS NULL ORDER BY inbox.created_at, inbox.id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list human mentions: %w", err)
	}
	defer rows.Close()
	items := make([]HumanMentionItem, 0)
	for rows.Next() {
		var item HumanMentionItem
		var body string
		var createdAt int64
		if err := rows.Scan(
			&item.ID, &item.HumanID, &item.RoomID, &item.MessageID,
			&item.AuthorType, &item.AuthorID, &body, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan human mention: %w", err)
		}
		item.Preview = preview(body, checkPreviewRunes)
		item.CreatedAt = fromMillis(createdAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate human mentions: %w", err)
	}
	return items, nil
}

func (s *Service) AckHumanMentions(ctx context.Context, roomID, humanID string) error {
	roomID = strings.TrimSpace(roomID)
	humanID = strings.TrimSpace(humanID)
	if roomID == "" || humanID == "" {
		return fmt.Errorf("room and human id are required")
	}
	if err := s.requireHumanMember(ctx, roomID, humanID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin ack human mentions: %w", err)
	}
	defer tx.Rollback()
	now := toMillis(s.now())
	if _, err := tx.ExecContext(ctx, `
		UPDATE inbox_items SET pulled_at = ?
		WHERE member_type = 'human' AND member_id = ? AND room_id = ? AND kind = 'mention' AND pulled_at IS NULL`,
		now, humanID, roomID); err != nil {
		return fmt.Errorf("mark human mentions pulled: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO room_cursors (room_id, member_type, member_id, last_read_seq)
		VALUES (?, 'human', ?, COALESCE((SELECT MAX(seq) FROM room_messages WHERE room_id = ?), 0))
		ON CONFLICT(room_id, member_type, member_id) DO UPDATE SET
			last_read_seq = COALESCE((SELECT MAX(seq) FROM room_messages WHERE room_id = ?), 0)`,
		roomID, humanID, roomID, roomID); err != nil {
		return fmt.Errorf("advance human cursor: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ack human mentions: %w", err)
	}
	return nil
}

func (s *Service) humanMentionRoomIDs(ctx context.Context, humanID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT room_id FROM inbox_items
		WHERE member_type = 'human' AND member_id = ? AND kind = 'mention' AND pulled_at IS NULL`, humanID)
	if err != nil {
		return nil, fmt.Errorf("query human mention rooms: %w", err)
	}
	defer rows.Close()
	rooms := make([]string, 0)
	for rows.Next() {
		var roomID string
		if err := rows.Scan(&roomID); err != nil {
			return nil, fmt.Errorf("scan human mention room: %w", err)
		}
		rooms = append(rooms, roomID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate human mention rooms: %w", err)
	}
	sort.Strings(rooms)
	return rooms, nil
}
