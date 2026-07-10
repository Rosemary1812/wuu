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

func TestResidentFetchThreadMessagesSubthreadPullAndMainStreamExclusion(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	mina := saveNamedParticipant(t, srv.rt, "Mina", "reviewer", "")
	nico := saveNamedParticipant(t, srv.rt, "Nico", "reviewer", "")
	outsider := saveNamedParticipant(t, srv.rt, "Odis", "reviewer", "")
	minaDM := startResidentDMForTest(t, srv, mina)
	groupID := startGroupThreadForTest(t, srv)
	// Mina and Nico are group members; Nico is deliberately NOT a participant of
	// the reply subthread (pull is open to the whole group, push is not).
	for _, participantID := range []string{mina, nico} {
		if err := session.AddThreadMember(srv.rt.SessionDir, groupID, participantID); err != nil {
			t.Fatalf("AddThreadMember %s: %v", participantID, err)
		}
	}
	cth := createStoredOpenThreadForTest(t, srv, groupID, mina, "seq-2", 2)
	for _, rec := range []session.HistoryRecord{
		{Role: "participant", Content: "main stream note", Name: "Mina", ParticipantID: mina, PostKind: "result"},
		{Role: "participant", Content: "reply-only detail", Name: "Mina", ParticipantID: mina, PostKind: "update", ThreadID: cth.ID},
	} {
		if err := session.AppendHistoryRecord(srv.rt.SessionDir, groupID, rec); err != nil {
			t.Fatalf("AppendHistoryRecord: %v", err)
		}
	}

	decode := func(t *testing.T, out string) []string {
		t.Helper()
		var result struct {
			Messages []struct {
				Text string `json:"text"`
			} `json:"messages"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		texts := make([]string, 0, len(result.Messages))
		for _, m := range result.Messages {
			texts = append(texts, m.Text)
		}
		return texts
	}

	// Plain top-level fetch of the group must exclude the reply-subthread record
	// so a reply line never leaks into an ambient main-stream pull.
	minaKit := residentToolkitForTest(t, srv, minaDM)
	out, err := minaKit.Execute(context.Background(), providers.ToolCall{
		Name:      "fetch_thread_messages",
		Arguments: fmt.Sprintf(`{"thread_id":%q}`, groupID),
	})
	if err != nil {
		t.Fatalf("fetch group main stream: %v", err)
	}
	mainTexts := decode(t, out)
	if len(mainTexts) != 1 || mainTexts[0] != "main stream note" {
		t.Fatalf("main-stream fetch should exclude reply records, got %v", mainTexts)
	}

	// Nico is a group member but not a reply participant; it can still PULL the
	// subthread by its cth id, and gets only that reply's records.
	nicoDM := startResidentDMForTest(t, srv, nico)
	nicoKit := residentToolkitForTest(t, srv, nicoDM)
	out, err = nicoKit.Execute(context.Background(), providers.ToolCall{
		Name:      "fetch_thread_messages",
		Arguments: fmt.Sprintf(`{"thread_id":%q}`, cth.ID),
	})
	if err != nil {
		t.Fatalf("non-participant group member pull of cth: %v", err)
	}
	cthTexts := decode(t, out)
	if len(cthTexts) != 1 || cthTexts[0] != "reply-only detail" {
		t.Fatalf("cth pull should return only the reply record, got %v", cthTexts)
	}

	// A non-member of the parent group cannot pull the reply either.
	outsiderThread, err := srv.ensureResidentDMThread(outsider)
	if err != nil {
		t.Fatalf("ensureResidentDMThread outsider: %v", err)
	}
	outsiderKit := residentToolkitForTest(t, srv, outsiderThread.ID)
	if _, err := outsiderKit.Execute(context.Background(), providers.ToolCall{
		Name:      "fetch_thread_messages",
		Arguments: fmt.Sprintf(`{"thread_id":%q}`, cth.ID),
	}); err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("non-group-member cth pull should fail with membership error, got %v", err)
	}
}
