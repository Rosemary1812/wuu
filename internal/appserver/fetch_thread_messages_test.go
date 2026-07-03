package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

func TestResidentFetchThreadMessagesRequiresMembershipAndLimits(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Mina", "reviewer", "")
	otherID := saveNamedParticipant(t, srv.rt, "Nico", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(srv.rt.SessionDir, groupID, participantID); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}
	for _, rec := range []session.HistoryRecord{
		{Role: "user", Content: "first user message"},
		{Role: "participant", Content: "participant result", Name: "Mina", ParticipantID: participantID, PostKind: "result"},
		{Role: "assistant", Content: strings.Repeat("x", 520)},
	} {
		if err := session.AppendHistoryRecord(srv.rt.SessionDir, groupID, rec); err != nil {
			t.Fatalf("AppendHistoryRecord: %v", err)
		}
	}

	kit := residentToolkitForTest(t, srv, dmID)
	out, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "fetch_thread_messages",
		Arguments: fmt.Sprintf(`{"thread_id":%q,"limit":2}`, groupID),
	})
	if err != nil {
		t.Fatalf("fetch_thread_messages: %v", err)
	}
	var result struct {
		Truncated bool `json:"truncated"`
		Messages  []struct {
			Role string `json:"role"`
			Text string `json:"text"`
		} `json:"messages"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !result.Truncated || len(result.Messages) != 2 {
		t.Fatalf("result should be truncated to two messages: %+v", result)
	}
	if result.Messages[0].Role != "participant" || !strings.Contains(result.Text, "[Mina] participant result") {
		t.Fatalf("first returned message should be participant result: %+v", result)
	}
	if len([]rune(result.Messages[1].Text)) != 500 || !strings.HasSuffix(result.Messages[1].Text, "...") {
		t.Fatalf("assistant message should be truncated to 500 runes, got len=%d text suffix=%q", len([]rune(result.Messages[1].Text)), result.Messages[1].Text[len(result.Messages[1].Text)-3:])
	}

	otherThread, err := srv.ensureResidentDMThread(otherID)
	if err != nil {
		t.Fatalf("ensureResidentDMThread other: %v", err)
	}
	otherKit := residentToolkitForTest(t, srv, otherThread.ID)
	_, err = otherKit.Execute(context.Background(), providers.ToolCall{
		Name:      "fetch_thread_messages",
		Arguments: fmt.Sprintf(`{"thread_id":%q}`, groupID),
	})
	if err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("non-member fetch should fail with membership error, got %v", err)
	}
}
