package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

func newResidentSpeechTestServer(t *testing.T) (*Server, *runtimeForResidentSpeechTest) {
	t.Helper()
	rt := newTestRuntime(t, &fakeClient{})
	kit, err := tools.New(rt.RootDir)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	rt.Toolkit = kit
	out := &lockedBuffer{}
	return New(rt, out), &runtimeForResidentSpeechTest{sessionDir: rt.SessionDir}
}

type runtimeForResidentSpeechTest struct {
	sessionDir string
}

func startResidentDMForTest(t *testing.T, srv *Server, participantID string) string {
	t.Helper()
	raw := fmt.Sprintf(`{"id":"dm","method":"thread/start","params":{"dm_participant_id":%q}}`, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start dm: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	resp := responseByID(t, msgs, "dm")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/start dm returned error: %v", errMsg)
	}
	return remarshal[ThreadStartResult](t, resp["result"]).Thread.ID
}

func startGroupThreadForTest(t *testing.T, srv *Server) string {
	t.Helper()
	raw := `{"id":"group","method":"thread/start","params":{}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start group: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	resp := responseByID(t, msgs, "group")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/start group returned error: %v", errMsg)
	}
	return remarshal[ThreadStartResult](t, resp["result"]).Thread.ID
}

func residentToolkitForTest(t *testing.T, srv *Server, dmThreadID string) *tools.Toolkit {
	t.Helper()
	th := srv.thread(dmThreadID)
	if th == nil {
		t.Fatalf("dm thread %q not found", dmThreadID)
	}
	threadRuntime, err := srv.ensureThreadRuntime(th)
	if err != nil {
		t.Fatalf("ensureThreadRuntime: %v", err)
	}
	if threadRuntime.Toolkit == nil {
		t.Fatal("resident thread runtime did not receive a toolkit")
	}
	return threadRuntime.Toolkit
}

func TestResidentPostMessageToMemberThreadPersistsSignedCard(t *testing.T) {
	srv, rt := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Iris", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(srv.rt.SessionDir, groupID, participantID); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}

	kit := residentToolkitForTest(t, srv, dmID)
	_, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"result","text":"Found the bug.","thread_id":%q}`, groupID),
	})
	if err != nil {
		t.Fatalf("post_message: %v", err)
	}

	history, err := session.LoadHistoryRecords(rt.sessionDir, groupID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	var posted *session.HistoryRecord
	for i := range history {
		if history[i].Role == "participant" {
			posted = &history[i]
			break
		}
	}
	if posted == nil {
		t.Fatalf("participant message not persisted: %+v", history)
	}
	if posted.ParticipantID != participantID || posted.PostKind != "result" || posted.Content != "Found the bug." {
		t.Fatalf("unexpected persisted participant message: %+v", *posted)
	}

	group := srv.thread(groupID)
	group.mu.Lock()
	defer group.mu.Unlock()
	if len(group.Turns) == 0 || len(group.Turns[len(group.Turns)-1].Items) == 0 {
		t.Fatalf("participant message did not create a visible thread item: %+v", group.Turns)
	}
	item := group.Turns[len(group.Turns)-1].Items[len(group.Turns[len(group.Turns)-1].Items)-1]
	if item.Type != ThreadItemParticipantMsg || item.Participant == nil || item.Participant.ID != participantID {
		t.Fatalf("unexpected participant thread item: %+v", item)
	}
}

func TestResidentPostMessageRejectsNonMemberThread(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Jules", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)
	groupID := startGroupThreadForTest(t, srv)

	kit := residentToolkitForTest(t, srv, dmID)
	_, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"result","text":"I should not be here.","thread_id":%q}`, groupID),
	})
	if err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("post_message should reject non-member thread, got %v", err)
	}
}

func TestResidentPostMessageLimitsPerThreadPerTurn(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Jules", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(srv.rt.SessionDir, groupID, participantID); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}

	kit := residentToolkitForTest(t, srv, dmID)
	for i := 1; i <= 2; i++ {
		if _, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "post_message",
			Arguments: fmt.Sprintf(`{"kind":"update","text":"update %d","thread_id":%q}`, i, groupID),
		}); err != nil {
			t.Fatalf("post_message %d: %v", i, err)
		}
	}
	_, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"update","text":"update 3","thread_id":%q}`, groupID),
	})
	if err == nil || !strings.Contains(err.Error(), "already posted 2 messages") {
		t.Fatalf("third post_message should hit per-thread turn limit, got %v", err)
	}
}

func TestResidentDeclineDefaultsToOwnDMThread(t *testing.T) {
	srv, rt := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Kira", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)

	kit := residentToolkitForTest(t, srv, dmID)
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "decline",
		Arguments: `{"reason":"No useful update."}`,
	}); err != nil {
		t.Fatalf("decline: %v", err)
	}

	history, err := session.LoadHistoryRecords(rt.sessionDir, dmID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	if len(history) == 0 {
		t.Fatal("decline did not persist a participant message")
	}
	got := history[len(history)-1]
	if got.Role != "participant" || got.ParticipantID != participantID || got.PostKind != "decline" || got.Content != "No useful update." {
		t.Fatalf("unexpected decline record: %+v", got)
	}
}

func TestResidentManageParticipantUsesParticipantIdentity(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Lena", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)

	kit := residentToolkitForTest(t, srv, dmID)
	out, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "manage_participant",
		Arguments: `{"action":"list"}`,
	})
	if err != nil {
		t.Fatalf("manage_participant list: %v", err)
	}
	var result struct {
		Action       string `json:"action"`
		Participants []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"participants"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal manage_participant result: %v", err)
	}
	if result.Action != "list" {
		t.Fatalf("action = %q, want list", result.Action)
	}
	foundSelf := false
	for _, p := range result.Participants {
		if p.ID == participantID && p.Name == "Lena" {
			foundSelf = true
		}
	}
	if !foundSelf {
		t.Fatalf("manage_participant list omitted resident participant: %+v", result.Participants)
	}
}
