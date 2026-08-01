package channels

import (
	"context"
	"testing"
)

func TestHumanRoomUnreadStatusAndMarkRead(t *testing.T) {
	ctx := context.Background()
	service, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	alpha, err := service.CreateNamedAgent(ctx, CreateNamedAgentParams{Name: "Alpha"})
	if err != nil {
		t.Fatalf("CreateNamedAgent() error = %v", err)
	}
	room, err := service.CreateRoom(ctx, CreateRoomParams{
		Kind: RoomChannel, Name: "Unread", CreatedBy: "human-1",
		Members: []RoomMember{{MemberType: MemberAgent, MemberID: alpha.Agent.ID}},
	})
	if err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}

	if _, err := service.SendHuman(ctx, HumanSendParams{RoomID: room.ID, HumanID: "human-1", Body: "own message"}); err != nil {
		t.Fatalf("SendHuman() error = %v", err)
	}
	if _, err := service.SendAgent(ctx, AgentSendParams{
		RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token, Body: "first reply", BasisSeq: 1,
	}); err != nil {
		t.Fatalf("SendAgent(first) error = %v", err)
	}

	counts, err := service.HumanRoomUnreadStatus(ctx, "human-1")
	if err != nil {
		t.Fatalf("HumanRoomUnreadStatus() error = %v", err)
	}
	if len(counts) != 1 || counts[0].RoomID != room.ID || counts[0].UnreadCount != 1 {
		t.Fatalf("unread counts = %#v, want one unread agent message", counts)
	}

	if err := service.MarkHumanRoomRead(ctx, room.ID, "human-1"); err != nil {
		t.Fatalf("MarkHumanRoomRead() error = %v", err)
	}
	counts, err = service.HumanRoomUnreadStatus(ctx, "human-1")
	if err != nil {
		t.Fatalf("HumanRoomUnreadStatus(after mark) error = %v", err)
	}
	if len(counts) != 1 || counts[0].UnreadCount != 0 {
		t.Fatalf("unread counts after mark = %#v, want zero", counts)
	}

	if _, err := service.SendAgent(ctx, AgentSendParams{
		RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token, Body: "second reply", BasisSeq: 2,
	}); err != nil {
		t.Fatalf("SendAgent(second) error = %v", err)
	}
	counts, err = service.HumanRoomUnreadStatus(ctx, "human-1")
	if err != nil {
		t.Fatalf("HumanRoomUnreadStatus(after reply) error = %v", err)
	}
	if len(counts) != 1 || counts[0].UnreadCount != 1 {
		t.Fatalf("unread counts after reply = %#v, want one", counts)
	}
}
