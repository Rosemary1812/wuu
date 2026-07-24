package channels

import (
	"context"
	"testing"
)

func TestHumanMessageAttachmentsPersistWithoutText(t *testing.T) {
	ctx := context.Background()
	service := openTestService(t, nil)
	agent := createTestAgent(t, service, "Alpha")
	room := createTestRoom(t, service, agent)

	sent, err := service.SendHuman(ctx, HumanSendParams{
		RoomID:  room.ID,
		HumanID: "human-1",
		Images: []MessageImage{{
			MediaType: "image/png",
			Data:      "aW1hZ2U=",
			Width:     32,
			Height:    24,
		}},
		Files: []MessageFile{{
			MediaType: "application/pdf",
			Data:      "cGRm",
			Filename:  "brief.pdf",
		}},
	})
	if err != nil {
		t.Fatalf("SendHuman() error = %v", err)
	}
	if sent.Message.Body != "" || len(sent.Message.Images) != 1 || len(sent.Message.Files) != 1 {
		t.Fatalf("sent message = %#v", sent.Message)
	}

	messages, err := service.ListMessages(ctx, room.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	message := messages[0]
	if message.Images[0].Data != "aW1hZ2U=" || message.Images[0].Width != 32 || message.Images[0].Height != 24 {
		t.Fatalf("image = %#v", message.Images)
	}
	if message.Files[0].Data != "cGRm" || message.Files[0].Filename != "brief.pdf" {
		t.Fatalf("file = %#v", message.Files)
	}
}
