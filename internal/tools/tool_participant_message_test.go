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
