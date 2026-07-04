package appserver

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/session"
)

func startNamedGroupThreadForTest(t *testing.T, srv *Server, title string) Thread {
	t.Helper()
	raw := fmt.Sprintf(`{"id":"group-start","method":"thread/start","params":{"group":true,"title":%q}}`, title)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start group: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	resp := responseByID(t, msgs, "group-start")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/start group returned error: %v", errMsg)
	}
	return remarshal[ThreadStartResult](t, resp["result"]).Thread
}

func TestThreadStartGroupCreatesGroupThread(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})

	thread := startNamedGroupThreadForTest(t, srv, "launch-planning")
	if !thread.Group {
		t.Fatalf("expected Group=true, got %+v", thread)
	}
	if thread.Title != "launch-planning" {
		t.Fatalf("expected title %q, got %q", "launch-planning", thread.Title)
	}
	if thread.DMParticipantID != "" {
		t.Fatalf("group thread should not carry a dm_participant_id, got %q", thread.DMParticipantID)
	}

	sess, ok, err := session.Find(rt.SessionDir, thread.ID)
	if err != nil {
		t.Fatalf("session.Find: %v", err)
	}
	if !ok || !sess.Group {
		t.Fatalf("expected persisted session to be marked group, got %+v (ok=%v)", sess, ok)
	}
}

func TestThreadStartGroupRejectsDMParticipant(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	participantID := saveNamedParticipant(t, rt, "Nell", "reviewer", "")

	raw := fmt.Sprintf(`{"id":"bad","method":"thread/start","params":{"group":true,"dm_participant_id":%q}}`, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("handleLine: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	resp := responseByID(t, msgs, "bad")
	if _, ok := resp["error"]; !ok {
		t.Fatalf("expected error response, got %+v", resp)
	}
}

func TestEnsureAllChannelIdempotentAndMirrorsRoster(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")

	firstID, err := srv.ensureAllChannel()
	if err != nil {
		t.Fatalf("ensureAllChannel: %v", err)
	}
	secondID, err := srv.ensureAllChannel()
	if err != nil {
		t.Fatalf("ensureAllChannel (second call): %v", err)
	}
	if firstID != secondID {
		t.Fatalf("ensureAllChannel is not idempotent: %q != %q", firstID, secondID)
	}

	sessions, err := session.List(rt.SessionDir, 0)
	if err != nil {
		t.Fatalf("session.List: %v", err)
	}
	allCount := 0
	for _, sess := range sessions {
		if sess.Group && sess.Title == allChannelTitle {
			allCount++
		}
	}
	if allCount != 1 {
		t.Fatalf("expected exactly one #all channel, found %d", allCount)
	}

	th := srv.thread(firstID)
	if th == nil {
		t.Fatalf("#all thread %q not resident", firstID)
	}
	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	thread = srv.threadWithGroupMembers(thread)
	memberIDs := make(map[string]bool, len(thread.Members))
	for _, m := range thread.Members {
		memberIDs[m.ID] = true
	}
	if !memberIDs[ivy] || !memberIDs[bea] {
		t.Fatalf("expected #all members to mirror the named roster, got %+v", thread.Members)
	}
	// No explicit thread_members rows were ever written for #all.
	members, err := session.ListThreadMembers(rt.SessionDir, firstID)
	if err != nil {
		t.Fatalf("ListThreadMembers: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("expected no explicit thread_members rows for #all, got %+v", members)
	}
}

func TestGroupTurnStartCompletesWithoutProviderCallAndMarksMentionedAddressed(t *testing.T) {
	client := &fakeClient{response: providersResponse("ok")}
	rt := newTestRuntime(t, client)
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	group := startNamedGroupThreadForTest(t, srv, "release")
	if err := session.AddThreadMember(rt.SessionDir, group.ID, bea); err != nil {
		t.Fatalf("AddThreadMember Bea: %v", err)
	}

	raw := fmt.Sprintf(`{"id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"Ship it, @Ivy.","mentions":[%q]}}`, group.ID, ivy)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	resp := responseByID(t, msgs, "turn")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("turn/start returned error: %v", errMsg)
	}
	turn := remarshal[TurnStartResult](t, resp["result"]).Turn
	if turn.Status != TurnStatusCompleted {
		t.Fatalf("expected group turn to complete synchronously, got status %q", turn.Status)
	}
	if turn.CompletedAt == nil {
		t.Fatalf("expected CompletedAt to be set on a synchronously completed group turn")
	}
	if len(turn.Items) != 1 || turn.Items[0].Type != ThreadItemUserMessage {
		t.Fatalf("expected exactly one user_message item, got %+v", turn.Items)
	}
	completed := remarshal[TurnCompletedNotification](t, notificationByMethodForThread(t, msgs, NotificationTurnCompleted, group.ID)["params"])
	if completed.Turn.ID != turn.ID || completed.Turn.Status != TurnStatusCompleted {
		t.Fatalf("expected completed notification for group turn %q, got %+v", turn.ID, completed.Turn)
	}

	th := srv.thread(group.ID)
	if th == nil {
		t.Fatalf("group thread %q not resident", group.ID)
	}
	th.mu.Lock()
	running := th.running
	th.mu.Unlock()
	if running {
		t.Fatalf("group thread must never run a provider turn")
	}

	_, ivyHistory := waitForResidentDMHistory(t, srv, ivy, 1)
	_, beaHistory := waitForResidentDMHistory(t, srv, bea, 1)
	ivyMeta := findEnvelopeMetaRecord(t, ivyHistory)
	beaMeta := findEnvelopeMetaRecord(t, beaHistory)
	if !ivyMeta.Addressed {
		t.Fatalf("mentioned member Ivy should be addressed, got %+v", ivyMeta)
	}
	if beaMeta.Addressed {
		t.Fatalf("unmentioned member Bea should not be addressed, got %+v", beaMeta)
	}
	if ivyMeta.SourceThreadTitle != group.Title {
		t.Fatalf("expected envelope meta source_thread_title %q, got %q", group.Title, ivyMeta.SourceThreadTitle)
	}

	// The group thread's own turn never invoked the provider: the only
	// Chat calls recorded belong to the resident agents it woke up.
	client.mu.Lock()
	callCount := len(client.requests)
	client.mu.Unlock()
	if callCount == 0 {
		t.Fatalf("expected the mentioned resident agent to run its own provider turn")
	}
}

func TestGroupTurnQueueRejected(t *testing.T) {
	client := &fakeClient{response: providersResponse("should not run")}
	rt := newTestRuntime(t, client)
	srv := New(rt, &lockedBuffer{})
	group := startNamedGroupThreadForTest(t, srv, "release")

	raw := fmt.Sprintf(`{"id":"queue","method":"turn/queue","params":{"thread_id":%q,"prompt":"queued group message"}}`, group.ID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/queue: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	resp := responseByID(t, msgs, "queue")
	errMsg := fmt.Sprint(resp["error"])
	if !strings.Contains(errMsg, "group threads do not support queued turns") {
		t.Fatalf("expected group queue rejection, got %+v", resp)
	}
	history, err := session.LoadHistoryRecords(rt.SessionDir, group.ID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("rejected group queue should not append history, got %+v", history)
	}
	client.mu.Lock()
	callCount := len(client.requests)
	client.mu.Unlock()
	if callCount != 0 {
		t.Fatalf("rejected group queue should not call provider, got %d calls", callCount)
	}
}

// TestGroupTurnStartRoutesThroughAllChannelWithoutExplicitMembership lives in
// resident_router_test.go: it exercises routeEnvelopes' #all fan-out, which
// is implemented there alongside the rest of the envelope routing machinery.

func removeThreadMemberForTest(t *testing.T, srv *Server, reqID, threadID, participantID string) map[string]any {
	t.Helper()
	raw := fmt.Sprintf(`{"id":%q,"method":"thread/members/remove","params":{"thread_id":%q,"participant_id":%q}}`, reqID, threadID, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/members/remove: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	return responseByID(t, msgs, reqID)
}

func addThreadMemberForTest(t *testing.T, srv *Server, reqID, threadID, participantID string) map[string]any {
	t.Helper()
	raw := fmt.Sprintf(`{"id":%q,"method":"thread/members/add","params":{"thread_id":%q,"participant_id":%q}}`, reqID, threadID, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/members/add: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	return responseByID(t, msgs, reqID)
}

func TestThreadMembersAddAddsGroupMember(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	group := startNamedGroupThreadForTest(t, srv, "release")
	if err := session.AddThreadMember(rt.SessionDir, group.ID, ivy); err != nil {
		t.Fatalf("AddThreadMember Ivy: %v", err)
	}

	resp := addThreadMemberForTest(t, srv, "add", group.ID, bea)
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/members/add returned error: %v", errMsg)
	}
	thread := remarshal[ThreadMembersAddResult](t, resp["result"]).Thread
	if len(thread.Members) != 2 || thread.Members[0].ID != ivy || thread.Members[1].ID != bea {
		t.Fatalf("expected result thread members to include Ivy and Bea, got %+v", thread.Members)
	}
	members, err := session.ListThreadMembers(rt.SessionDir, group.ID)
	if err != nil {
		t.Fatalf("ListThreadMembers: %v", err)
	}
	if len(members) != 2 || members[0] != ivy || members[1] != bea {
		t.Fatalf("expected persisted members to include Ivy and Bea, got %+v", members)
	}
}

func TestThreadMembersAddIsIdempotent(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")
	group := startNamedGroupThreadForTest(t, srv, "release")
	if err := session.AddThreadMember(rt.SessionDir, group.ID, ivy); err != nil {
		t.Fatalf("AddThreadMember Ivy: %v", err)
	}

	resp := addThreadMemberForTest(t, srv, "add-existing", group.ID, ivy)
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/members/add existing member returned error: %v", errMsg)
	}
	thread := remarshal[ThreadMembersAddResult](t, resp["result"]).Thread
	if len(thread.Members) != 1 || thread.Members[0].ID != ivy {
		t.Fatalf("expected result thread members to stay as Ivy, got %+v", thread.Members)
	}
}

func TestThreadMembersAddRejectsAllChannel(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")

	allID, err := srv.ensureAllChannel()
	if err != nil {
		t.Fatalf("ensureAllChannel: %v", err)
	}

	resp := addThreadMemberForTest(t, srv, "add-all", allID, ivy)
	errMsg, ok := resp["error"]
	if !ok {
		t.Fatalf("expected error adding a member to #all, got: %+v", resp)
	}
	errStr := fmt.Sprint(errMsg)
	if !strings.Contains(errStr, "all channel") {
		t.Fatalf("error should mention the all channel, got %q", errStr)
	}
}

func TestThreadMembersAddRejectsMemberCap(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	group := startNamedGroupThreadForTest(t, srv, "release")
	for i := 0; i < maxGroupMembers; i++ {
		id := saveNamedParticipant(t, rt, fmt.Sprintf("Member %d", i), "reviewer", "")
		if err := session.AddThreadMember(rt.SessionDir, group.ID, id); err != nil {
			t.Fatalf("AddThreadMember %d: %v", i, err)
		}
	}
	overflow := saveNamedParticipant(t, rt, "Overflow", "reviewer", "")

	resp := addThreadMemberForTest(t, srv, "add-overflow", group.ID, overflow)
	errMsg, ok := resp["error"]
	if !ok {
		t.Fatalf("expected error adding beyond member cap, got: %+v", resp)
	}
	errStr := fmt.Sprint(errMsg)
	if !strings.Contains(errStr, "max 8") {
		t.Fatalf("error should mention the member cap, got %q", errStr)
	}
}

func TestThreadMembersRemoveRemovesGroupMember(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	group := startNamedGroupThreadForTest(t, srv, "release")
	if err := session.AddThreadMember(rt.SessionDir, group.ID, ivy); err != nil {
		t.Fatalf("AddThreadMember Ivy: %v", err)
	}
	if err := session.AddThreadMember(rt.SessionDir, group.ID, bea); err != nil {
		t.Fatalf("AddThreadMember Bea: %v", err)
	}

	resp := removeThreadMemberForTest(t, srv, "remove", group.ID, bea)
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/members/remove returned error: %v", errMsg)
	}
	thread := remarshal[ThreadMembersRemoveResult](t, resp["result"]).Thread
	if len(thread.Members) != 1 || thread.Members[0].ID != ivy {
		t.Fatalf("expected result thread members to be just Ivy after removal, got %+v", thread.Members)
	}
	members, err := session.ListThreadMembers(rt.SessionDir, group.ID)
	if err != nil {
		t.Fatalf("ListThreadMembers: %v", err)
	}
	if len(members) != 1 || members[0] != ivy {
		t.Fatalf("expected persisted members to be just Ivy after removal, got %+v", members)
	}
}

func TestThreadMembersRemoveRejectsAllChannel(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")

	allID, err := srv.ensureAllChannel()
	if err != nil {
		t.Fatalf("ensureAllChannel: %v", err)
	}

	resp := removeThreadMemberForTest(t, srv, "remove-all", allID, ivy)
	errMsg, ok := resp["error"]
	if !ok {
		t.Fatalf("expected error removing a member from #all, got: %+v", resp)
	}
	errStr := fmt.Sprint(errMsg)
	if !strings.Contains(errStr, "all channel") {
		t.Fatalf("error should mention the all channel, got %q", errStr)
	}
}

func TestThreadMembersRemoveRejectsNonGroupThread(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")

	raw := `{"id":"start","method":"thread/start","params":{}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	startResp := responseByID(t, msgs, "start")
	if errMsg, ok := startResp["error"]; ok {
		t.Fatalf("thread/start returned error: %v", errMsg)
	}
	thread := remarshal[ThreadStartResult](t, startResp["result"]).Thread

	resp := removeThreadMemberForTest(t, srv, "remove", thread.ID, ivy)
	errMsg, ok := resp["error"]
	if !ok {
		t.Fatalf("expected error removing a member from a non-group thread, got: %+v", resp)
	}
	errStr := fmt.Sprint(errMsg)
	if !strings.Contains(errStr, "not a group thread") {
		t.Fatalf("error should say the thread is not a group thread, got %q", errStr)
	}
}

func TestThreadMembersRemoveRejectsNonMember(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	group := startNamedGroupThreadForTest(t, srv, "release")
	if err := session.AddThreadMember(rt.SessionDir, group.ID, ivy); err != nil {
		t.Fatalf("AddThreadMember Ivy: %v", err)
	}

	resp := removeThreadMemberForTest(t, srv, "remove", group.ID, bea)
	errMsg, ok := resp["error"]
	if !ok {
		t.Fatalf("expected error removing a non-member, got: %+v", resp)
	}
	errStr := fmt.Sprint(errMsg)
	if !strings.Contains(errStr, "not a member") {
		t.Fatalf("error should say the participant is not a member, got %q", errStr)
	}
	members, err := session.ListThreadMembers(rt.SessionDir, group.ID)
	if err != nil {
		t.Fatalf("ListThreadMembers: %v", err)
	}
	if len(members) != 1 || members[0] != ivy {
		t.Fatalf("membership should be untouched by a failed removal, got %+v", members)
	}
}

// The #all channel's first broadcast must already carry the implicit
// roster as Members: ensureAllChannel used to send a bare snapshot, so the
// channel appeared with no member chips until some other wrapped payload
// arrived (consistency plan 2026-07-04 §1 #15).
func TestEnsureAllChannelBroadcastCarriesMembers(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")

	if _, err := srv.ensureAllChannel(); err != nil {
		t.Fatalf("ensureAllChannel: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	started := remarshal[ThreadStartedNotification](t, notificationByMethod(t, msgs, NotificationThreadStarted)["params"])
	memberIDs := make(map[string]bool, len(started.Thread.Members))
	for _, m := range started.Thread.Members {
		memberIDs[m.ID] = true
	}
	if !memberIDs[ivy] || !memberIDs[bea] {
		t.Fatalf("thread/started for #all must carry the named roster as members, got %+v", started.Thread.Members)
	}
}

// A participant message landing in a group thread rebroadcasts the thread
// via thread/updated. The frontend replaces its cached thread wholesale on
// that notification, so the snapshot must carry Members — a bare snapshot
// would wipe the member chips the UI already had (§1 #15).
func TestGroupParticipantMessageThreadUpdatedCarriesMembers(t *testing.T) {
	client := &fakeClient{response: providersResponse("ok")}
	rt := newTestRuntime(t, client)
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	group := startNamedGroupThreadForTest(t, srv, "release")
	if err := session.AddThreadMember(rt.SessionDir, group.ID, ivy); err != nil {
		t.Fatalf("AddThreadMember Ivy: %v", err)
	}
	if err := session.AddThreadMember(rt.SessionDir, group.ID, bea); err != nil {
		t.Fatalf("AddThreadMember Bea: %v", err)
	}

	if err := srv.publishParticipantMessage(group.ID, agentcontrol.ParticipantMessage{
		ParticipantID: ivy,
		Kind:          "result",
		Text:          "shipping update",
	}); err != nil {
		t.Fatalf("publishParticipantMessage: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	var updated *ThreadUpdatedNotification
	for _, raw := range notificationsByMethod(msgs, NotificationThreadUpdated) {
		params := remarshal[ThreadUpdatedNotification](t, raw["params"])
		if params.Thread.ID == group.ID {
			updated = &params
			break
		}
	}
	if updated == nil {
		t.Fatalf("no thread/updated notification for group thread %q in %+v", group.ID, msgs)
	}
	memberIDs := make(map[string]bool, len(updated.Thread.Members))
	for _, m := range updated.Thread.Members {
		memberIDs[m.ID] = true
	}
	if !memberIDs[ivy] || !memberIDs[bea] {
		t.Fatalf("thread/updated for a group thread must carry members, got %+v", updated.Thread.Members)
	}
}
