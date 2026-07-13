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
	if _, err := session.SetGroupThread(srv.rt.SessionDir, groupID, true); err != nil {
		t.Fatalf("SetGroupThread: %v", err)
	}
	if err := session.AddThreadMember(srv.rt.SessionDir, groupID, participantID); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}

	kit := residentToolkitForTest(t, srv, dmID)
	_, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"brief","text":"Found the bug; Iris owns the fix.","thread_id":%q}`, groupID),
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
	if posted.ParticipantID != participantID || posted.PostKind != "brief" || posted.Content != "Found the bug; Iris owns the fix." {
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

func TestResidentPostMessageLimitsOnlyGroupMainStream(t *testing.T) {
	srv, rt := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Iris", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)
	groupID := startGroupThreadForTest(t, srv)
	if _, err := session.SetGroupThread(srv.rt.SessionDir, groupID, true); err != nil {
		t.Fatalf("SetGroupThread: %v", err)
	}
	if err := session.AddThreadMember(srv.rt.SessionDir, groupID, participantID); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}
	cth := createStoredOpenThreadForTest(t, srv, groupID, participantID, "seq-3", 3)
	longText := strings.Repeat("界", maxGroupBriefRunes+1)
	kit := residentToolkitForTest(t, srv, dmID)

	_, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"brief","text":%q,"thread_id":%q}`, longText, groupID),
	})
	if err == nil || !strings.Contains(err.Error(), "coordination briefs") || !strings.Contains(err.Error(), "got 281") {
		t.Fatalf("long group main-stream post should be rejected with repair guidance, got %v", err)
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"result","text":%q}`, longText),
	}); err != nil {
		t.Fatalf("long DM result should remain allowed: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"update","text":%q,"thread_id":%q}`, longText, cth.ID),
	}); err != nil {
		t.Fatalf("long reply/task detail should remain allowed: %v", err)
	}

	history, err := session.LoadHistoryRecords(rt.sessionDir, groupID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	for _, record := range history {
		if record.Role == "participant" && record.ThreadID == "" && record.Content == longText {
			t.Fatal("rejected long group brief was persisted")
		}
	}
}

func TestResidentPostMessageToSubthreadFoldsIntoReplyThread(t *testing.T) {
	srv, rt := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Iris", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(srv.rt.SessionDir, groupID, participantID); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}
	cth := createStoredOpenThreadForTest(t, srv, groupID, participantID, "seq-3", 3)

	group := srv.thread(groupID)
	group.mu.Lock()
	turnsBefore := len(group.Turns)
	group.mu.Unlock()

	kit := residentToolkitForTest(t, srv, dmID)
	out, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"update","text":"Looking into it.","thread_id":%q}`, cth.ID),
	})
	if err != nil {
		t.Fatalf("post_message to subthread: %v", err)
	}
	var posted struct {
		ThreadID string `json:"thread_id"`
	}
	if err := json.Unmarshal([]byte(out), &posted); err != nil {
		t.Fatalf("unmarshal post_message result: %v", err)
	}
	if posted.ThreadID != cth.ID {
		t.Fatalf("post_message result thread_id = %q, want subthread %q", posted.ThreadID, cth.ID)
	}

	history, err := session.LoadHistoryRecords(rt.sessionDir, groupID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	var tagged *session.HistoryRecord
	for i := range history {
		if history[i].Role == "participant" && strings.TrimSpace(history[i].ThreadID) == cth.ID {
			tagged = &history[i]
			break
		}
	}
	if tagged == nil {
		t.Fatalf("subthread message not persisted under parent history tagged with cth: %+v", history)
	}
	if tagged.Content != "Looking into it." || tagged.ParticipantID != participantID {
		t.Fatalf("unexpected subthread record: %+v", *tagged)
	}

	// Weak isolation: the subthread post must not append a visible item to the
	// main group stream (publishParticipantMessage short-circuits on thread_id).
	group.mu.Lock()
	turnsAfter := len(group.Turns)
	group.mu.Unlock()
	if turnsAfter != turnsBefore {
		t.Fatalf("subthread post leaked into main stream: turns %d -> %d", turnsBefore, turnsAfter)
	}
}

func TestResidentTaskTurnRewritesParentPostIntoTaskThread(t *testing.T) {
	srv, rt := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "IrisTask", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(srv.rt.SessionDir, groupID, participantID); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}
	cth := createStoredOpenThreadForTest(t, srv, groupID, participantID, "seq-task", 7)

	envs := []MessageEnvelope{{
		SourceThreadID:    groupID,
		SourceSubthreadID: cth.ID,
		SenderKind:        "system",
		SenderName:        "task plan",
		Text:              "Continue the assigned task.",
	}}
	speech := srv.residentParticipantSpeechForTurn(
		participantID,
		residentSpeechHopsByThread(envs),
		residentSpeechEngagedThreads(envs),
		nil,
		residentSpeechReplySubthreads(envs),
	)
	posted, err := speech.PostMessage(context.Background(), "update", "Targeted progress.", groupID, 0, false)
	if err != nil {
		t.Fatalf("post task update through parent id: %v", err)
	}
	if posted.ThreadID != cth.ID {
		t.Fatalf("posted thread_id = %q, want task thread %q", posted.ThreadID, cth.ID)
	}

	history, err := session.LoadHistoryRecords(rt.sessionDir, groupID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	for _, record := range history {
		if record.Role == "participant" && record.Content == "Targeted progress." {
			if record.ThreadID != cth.ID {
				t.Fatalf("task update leaked to main stream: %+v", record)
			}
			return
		}
	}
	t.Fatal("task update was not persisted")
}

func TestResidentSpeechReplySubthreadsRejectsAmbiguousParent(t *testing.T) {
	got := residentSpeechReplySubthreads([]MessageEnvelope{
		{SourceThreadID: "group-1", SourceSubthreadID: "cth-a"},
		{SourceThreadID: "group-1", SourceSubthreadID: "cth-b"},
	})
	if _, ok := got["group-1"]; ok {
		t.Fatalf("ambiguous reply target should not be inferred: %+v", got)
	}
}

func TestResidentPostMessageToSubthreadRejectsNonMember(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Jules", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)
	groupID := startGroupThreadForTest(t, srv)
	ownerID := saveNamedParticipant(t, srv.rt, "ThreadOwner", "lead", "")
	if err := session.AddThreadMember(srv.rt.SessionDir, groupID, ownerID); err != nil {
		t.Fatalf("AddThreadMember(owner): %v", err)
	}
	cth := createStoredOpenThreadForTest(t, srv, groupID, ownerID, "seq-3", 3)

	kit := residentToolkitForTest(t, srv, dmID)
	_, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"update","text":"nope","thread_id":%q}`, cth.ID),
	})
	if err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("post_message to subthread should reject non-member of parent group, got %v", err)
	}
}

func TestResidentPostMessageToUnknownSubthreadFails(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Kira", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)

	kit := residentToolkitForTest(t, srv, dmID)
	_, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: `{"kind":"update","text":"nope","thread_id":"cth-does-not-exist"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "conversation thread not found") {
		t.Fatalf("post_message to unknown subthread should fail, got %v", err)
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
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"brief","text":"one useful update","thread_id":%q}`, groupID),
	}); err != nil {
		t.Fatalf("first post_message: %v", err)
	}
	_, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"brief","text":"a redundant follow-up","thread_id":%q}`, groupID),
	})
	if err == nil || !strings.Contains(err.Error(), "already posted to thread") {
		t.Fatalf("second post_message should hit per-thread turn limit, got %v", err)
	}
}

func TestResidentDeclineDefaultsToOwnDMThread(t *testing.T) {
	srv, rt := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Kira", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)

	kit := residentToolkitForTest(t, srv, dmID)
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: `{"kind":"decline","text":"No useful update."}`,
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
