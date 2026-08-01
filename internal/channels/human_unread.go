package channels

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) HumanRoomUnreadStatus(ctx context.Context, humanID string) ([]HumanRoomUnreadCount, error) {
	humanID = strings.TrimSpace(humanID)
	if humanID == "" {
		return nil, fmt.Errorf("human id is required")
	}
	if err := s.ensureHumanRoomCursors(ctx, humanID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT member.room_id, COUNT(message.id)
		FROM room_members member
		JOIN room_cursors cursor
			ON cursor.room_id = member.room_id
			AND cursor.member_type = 'human'
			AND cursor.member_id = member.member_id
		LEFT JOIN room_messages message
			ON message.room_id = member.room_id
			AND message.seq > cursor.last_read_seq
			AND NOT (message.author_type = 'human' AND message.author_id = member.member_id)
		WHERE member.member_type = 'human' AND member.member_id = ?
		GROUP BY member.room_id
		ORDER BY member.room_id`, humanID)
	if err != nil {
		return nil, fmt.Errorf("query human room unread status: %w", err)
	}
	defer rows.Close()
	counts := make([]HumanRoomUnreadCount, 0)
	for rows.Next() {
		var count HumanRoomUnreadCount
		if err := rows.Scan(&count.RoomID, &count.UnreadCount); err != nil {
			return nil, fmt.Errorf("scan human room unread status: %w", err)
		}
		counts = append(counts, count)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate human room unread status: %w", err)
	}
	return counts, nil
}

func (s *Service) MarkHumanRoomRead(ctx context.Context, roomID, humanID string) error {
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
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO room_cursors (room_id, member_type, member_id, last_read_seq)
		VALUES (?, 'human', ?, COALESCE((SELECT MAX(seq) FROM room_messages WHERE room_id = ?), 0))
		ON CONFLICT(room_id, member_type, member_id) DO UPDATE SET
			last_read_seq = COALESCE((SELECT MAX(seq) FROM room_messages WHERE room_id = ?), 0)`,
		roomID, humanID, roomID, roomID); err != nil {
		return fmt.Errorf("mark human room read: %w", err)
	}
	return nil
}

func (s *Service) ensureHumanRoomCursors(ctx context.Context, humanID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO room_cursors (room_id, member_type, member_id, last_read_seq)
		SELECT member.room_id, 'human', member.member_id, COALESCE(MAX(message.seq), 0)
		FROM room_members member
		LEFT JOIN room_messages message ON message.room_id = member.room_id
		WHERE member.member_type = 'human' AND member.member_id = ?
		GROUP BY member.room_id, member.member_id
		ON CONFLICT(room_id, member_type, member_id) DO NOTHING`, humanID); err != nil {
		return fmt.Errorf("initialize human room cursors: %w", err)
	}
	return nil
}
