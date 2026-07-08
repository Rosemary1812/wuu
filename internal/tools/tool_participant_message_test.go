package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type heldParticipantSpeech struct{}

func (heldParticipantSpeech) PostMessage(context.Context, string, string, string, int, bool) (PostedMessage, error) {
	return PostedMessage{
		Held:     true,
		HeldNote: "Ada: already answered",
		BasisSeq: 7,
	}, nil
}

func TestPostMessageHeldResultSuggestsConsideringInception(t *testing.T) {
	tool := NewPostMessageTool(&Env{ParticipantSpeech: heldParticipantSpeech{}})
	raw, err := tool.Execute(context.Background(), `{"kind":"result","text":"draft","thread_id":"thr-1","basis_seq":7}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	reason, _ := result["reason"].(string)
	if !strings.Contains(reason, "consider inception before deciding whether to revise, resend with force=true, or stay silent") {
		t.Fatalf("held reason missing inception guidance:\n%s", reason)
	}
}

func TestPostMessageToolDescriptionTeachesGroupThreadID(t *testing.T) {
	def := NewPostMessageTool(&Env{}).Definition()
	for _, want := range []string{
		"For group chat replies, set thread_id to the source thread_id from the incoming_message.",
		"Plain assistant text is private and is not posted to the group.",
		"call post_message alone first and wait for status=\"posted\"",
		"never call post_message and piece_done in the same assistant tool-call batch",
	} {
		if !strings.Contains(def.Description, want) {
			t.Fatalf("post_message description missing %q:\n%s", want, def.Description)
		}
	}

	properties, ok := def.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("post_message schema missing properties: %+v", def.InputSchema)
	}
	threadID, ok := properties["thread_id"].(map[string]any)
	if !ok {
		t.Fatalf("post_message schema missing thread_id property: %+v", properties)
	}
	description, _ := threadID["description"].(string)
	for _, want := range []string{
		"For group replies, use the incoming_message thread_id.",
		"Leaving thread_id empty posts to your own DM.",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("thread_id description missing %q:\n%s", want, description)
		}
	}
}
