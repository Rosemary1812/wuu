package appserver

import (
	"context"
	"fmt"
	"testing"

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

// TestGroupTurnStartRoutesThroughAllChannelWithoutExplicitMembership lives in
// resident_router_test.go: it exercises routeEnvelopes' #all fan-out, which
// is implemented there alongside the rest of the envelope routing machinery.
