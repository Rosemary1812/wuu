package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

func waitForResidentDMHistory(t *testing.T, srv *Server, participantID string, wantUserRecords int) (string, []session.HistoryRecord) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sessions, err := session.List(srv.rt.SessionDir, 0)
		if err != nil {
			t.Fatalf("session.List: %v", err)
		}
		for _, sess := range sessions {
			if sess.DMParticipantID != participantID {
				continue
			}
			history, err := session.LoadHistoryRecords(srv.rt.SessionDir, sess.ID, false)
			if err != nil {
				t.Fatalf("LoadHistoryRecords: %v", err)
			}
			users := 0
			for _, rec := range history {
				if rec.Role == "user" {
					users++
				}
			}
			if users >= wantUserRecords {
				return sess.ID, history
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for resident DM history for %s", participantID)
	return "", nil
}

func findEnvelopeMetaRecord(t *testing.T, history []session.HistoryRecord) envelopeMetaRecord {
	t.Helper()
	for _, rec := range history {
		if rec.Role != "user" || len(rec.EnvelopeMeta) == 0 {
			continue
		}
		var metas []envelopeMetaRecord
		if err := json.Unmarshal(rec.EnvelopeMeta, &metas); err != nil {
			t.Fatalf("unmarshal envelope meta: %v", err)
		}
		if len(metas) > 0 {
			return metas[0]
		}
	}
	t.Fatalf("no envelope meta in history: %+v", history)
	return envelopeMetaRecord{}
}

func TestResidentRouterUserMessageFansOutToThreadMembers(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	out := &lockedBuffer{}
	srv := New(rt, out)
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(rt.SessionDir, groupID, bea); err != nil {
		t.Fatalf("AddThreadMember Bea: %v", err)
	}

	raw := fmt.Sprintf(`{"id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"Please look, @Ivy.","mentions":[%q]}}`, groupID, ivy)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	_, ivyHistory := waitForResidentDMHistory(t, srv, ivy, 1)
	_, beaHistory := waitForResidentDMHistory(t, srv, bea, 1)
	ivyMeta := findEnvelopeMetaRecord(t, ivyHistory)
	beaMeta := findEnvelopeMetaRecord(t, beaHistory)
	if ivyMeta.SourceThreadID != groupID || !ivyMeta.Addressed || ivyMeta.Hop != 0 {
		t.Fatalf("Ivy envelope meta = %+v", ivyMeta)
	}
	if beaMeta.SourceThreadID != groupID || beaMeta.Addressed || beaMeta.Hop != 0 {
		t.Fatalf("Bea envelope meta = %+v", beaMeta)
	}
}

func TestResidentRouterParticipantMessageHonorsMentionsAndHopBudget(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	for _, participantID := range []string{ada, bea} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, participantID); err != nil {
			t.Fatalf("AddThreadMember %s: %v", participantID, err)
		}
	}

	err := srv.publishParticipantMessage(groupID, agentcontrol.ParticipantMessage{
		AgentID:       ada,
		ParticipantID: ada,
		Kind:          "result",
		Hop:           1,
		Text:          "@Bea please verify this.",
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("publish Ada message: %v", err)
	}
	_, beaHistory := waitForResidentDMHistory(t, srv, bea, 1)
	beaMeta := findEnvelopeMetaRecord(t, beaHistory)
	if beaMeta.SourceThreadID != groupID || !beaMeta.Addressed || beaMeta.Hop != 1 || beaMeta.SenderParticipantID != ada {
		t.Fatalf("Bea envelope meta = %+v", beaMeta)
	}

	err = srv.publishParticipantMessage(groupID, agentcontrol.ParticipantMessage{
		AgentID:       bea,
		ParticipantID: bea,
		Kind:          "result",
		Hop:           2,
		Text:          "Verified.",
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("publish Bea message: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, history := findDMHistoryIfExists(t, srv, ada); len(history) > 0 {
		t.Fatalf("Ada should not receive unmentioned hop=2 reply, history=%+v", history)
	}
}

func TestResidentRouterRecordsUnansweredAddressedTelemetry(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	out := &lockedBuffer{}
	srv := New(rt, out)
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)

	raw := fmt.Sprintf(`{"id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"@Ivy please answer.","mentions":[%q]}}`, groupID, ivy)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	dmID, _ := waitForResidentDMHistory(t, srv, ivy, 1)

	history := waitForResidentTelemetry(t, rt.SessionDir, dmID)
	found := false
	for _, rec := range history {
		if rec.Role == "meta" && rec.Content == "resident_unanswered_addressed" && strings.TrimSpace(rec.ParticipantID) == ivy {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing unanswered addressed telemetry: %+v", history)
	}
}

func waitForResidentTelemetry(t *testing.T, sessionDir, dmID string) []session.HistoryRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		history, err := session.LoadHistoryRecords(sessionDir, dmID, true)
		if err != nil {
			t.Fatalf("LoadHistoryRecords include meta: %v", err)
		}
		for _, rec := range history {
			if rec.Role == "meta" && rec.Content == "resident_unanswered_addressed" {
				return history
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for resident telemetry in %s", dmID)
	return nil
}

func findDMHistoryIfExists(t *testing.T, srv *Server, participantID string) (string, []session.HistoryRecord) {
	t.Helper()
	sessions, err := session.List(srv.rt.SessionDir, 0)
	if err != nil {
		t.Fatalf("session.List: %v", err)
	}
	for _, sess := range sessions {
		if sess.DMParticipantID != participantID {
			continue
		}
		history, err := session.LoadHistoryRecords(srv.rt.SessionDir, sess.ID, false)
		if err != nil {
			t.Fatalf("LoadHistoryRecords: %v", err)
		}
		return sess.ID, history
	}
	return "", nil
}

func providersResponse(content string) providers.ChatResponse {
	return providers.ChatResponse{Content: content}
}

func TestGroupTurnStartRoutesThroughAllChannelWithoutExplicitMembership(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")

	allID, err := srv.ensureAllChannel()
	if err != nil {
		t.Fatalf("ensureAllChannel: %v", err)
	}

	raw := fmt.Sprintf(`{"id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"Heads up, everyone."}}`, allID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	resp := responseByID(t, msgs, "turn")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("turn/start returned error: %v", errMsg)
	}

	// Ivy was never added to thread_members, yet #all's implicit roster
	// membership still routes her an envelope.
	_, ivyHistory := waitForResidentDMHistory(t, srv, ivy, 1)
	ivyMeta := findEnvelopeMetaRecord(t, ivyHistory)
	if ivyMeta.SourceThreadID != allID {
		t.Fatalf("expected envelope sourced from #all, got %+v", ivyMeta)
	}
}

func TestResidentPostMessageToAllChannelWithoutExplicitMembership(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Iris", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)
	allID, err := srv.ensureAllChannel()
	if err != nil {
		t.Fatalf("ensureAllChannel: %v", err)
	}

	kit := residentToolkitForTest(t, srv, dmID)
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"result","text":"Heads up.","thread_id":%q}`, allID),
	})
	if err != nil {
		t.Fatalf("post_message to #all without explicit membership: %v", err)
	}
}

func TestFallbackDMReplyPublishesFinalAnswerWhenNoToolPost(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	th, err := srv.ensureResidentDMThread(ada)
	if err != nil {
		t.Fatalf("ensureResidentDMThread: %v", err)
	}
	turn := Turn{
		ID:     "turn-1",
		Status: TurnStatusCompleted,
		Items: []ThreadItem{
			{ID: "i1", Type: ThreadItemUserMessage, Text: "hi"},
			{ID: "i2", Type: ThreadItemAgentMessage, Text: "你好，我是 Ada。"},
		},
	}
	srv.fallbackDMReplyFromFinalAnswer(th, ada, nil, turn, time.Now().UTC())

	th.mu.Lock()
	snapshot := th.snapshotLocked()
	th.mu.Unlock()
	found := false
	for _, tn := range snapshot.Turns {
		for _, item := range tn.Items {
			if item.Type == ThreadItemParticipantMsg && item.Text == "你好，我是 Ada。" && item.PostKind == "result" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected fallback participant_message in DM thread, turns=%+v", snapshot.Turns)
	}
}

func TestFallbackDMReplySkipsWhenResidentUsedTools(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	th, err := srv.ensureResidentDMThread(ada)
	if err != nil {
		t.Fatalf("ensureResidentDMThread: %v", err)
	}
	for _, tc := range []struct {
		name string
		turn Turn
	}{
		{"already posted", Turn{ID: "t1", Status: TurnStatusCompleted, Items: []ThreadItem{
			{ID: "i1", Type: ThreadItemParticipantMsg, Text: "posted"},
			{ID: "i2", Type: ThreadItemAgentMessage, Text: "final"},
		}}},
		{"failed turn", Turn{ID: "t2", Status: TurnStatusFailed, Items: []ThreadItem{
			{ID: "i1", Type: ThreadItemAgentMessage, Text: "final"},
		}}},
		{"no final answer", Turn{ID: "t3", Status: TurnStatusCompleted, Items: []ThreadItem{
			{ID: "i1", Type: ThreadItemUserMessage, Text: "hi"},
		}}},
	} {
		srv.fallbackDMReplyFromFinalAnswer(th, ada, nil, tc.turn, time.Now().UTC())
		th.mu.Lock()
		snapshot := th.snapshotLocked()
		th.mu.Unlock()
		for _, tn := range snapshot.Turns {
			for _, item := range tn.Items {
				if item.Type == ThreadItemParticipantMsg {
					t.Fatalf("%s: unexpected fallback message published", tc.name)
				}
			}
		}
	}
}

// TestFallbackDMReplyRoutesToGroupEnvelopeSourceThread guards against the
// regression where a resident's fallback reply (final answer text with no
// post_message tool call) to a group-chat-triggered turn silently landed in
// the resident's own DM thread instead of the group thread that sent the
// MessageEnvelope. The resident's DM thread is its only "brain" (turns for
// group envelopes still run there), but replies must be routed back to the
// envelope's SourceThreadID, not hardcoded to th.ID.
func TestFallbackDMReplyRoutesToGroupEnvelopeSourceThread(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	dmTh, err := srv.ensureResidentDMThread(ada)
	if err != nil {
		t.Fatalf("ensureResidentDMThread: %v", err)
	}
	groupID := startGroupThreadForTest(t, srv)

	turn := Turn{
		ID:     "turn-1",
		Status: TurnStatusCompleted,
		Items: []ThreadItem{
			{ID: "i1", Type: ThreadItemUserMessage, Text: "envelope from group"},
			{ID: "i2", Type: ThreadItemAgentMessage, Text: "群里的回复"},
		},
	}
	envs := []MessageEnvelope{
		{ID: "env-1", SourceThreadID: groupID, Addressed: true, SenderKind: "user", Text: "hi Ada"},
	}
	srv.fallbackDMReplyFromFinalAnswer(dmTh, ada, envs, turn, time.Now().UTC())

	groupTh, err := srv.ensureResidentThread(groupID)
	if err != nil {
		t.Fatalf("ensureResidentThread group: %v", err)
	}
	groupTh.mu.Lock()
	groupSnapshot := groupTh.snapshotLocked()
	groupTh.mu.Unlock()
	found := false
	for _, tn := range groupSnapshot.Turns {
		for _, item := range tn.Items {
			if item.Type == ThreadItemParticipantMsg && item.Text == "群里的回复" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected fallback reply in group thread, turns=%+v", groupSnapshot.Turns)
	}

	dmTh.mu.Lock()
	dmSnapshot := dmTh.snapshotLocked()
	dmTh.mu.Unlock()
	for _, tn := range dmSnapshot.Turns {
		for _, item := range tn.Items {
			if item.Type == ThreadItemParticipantMsg {
				t.Fatalf("unexpected fallback reply leaked into own DM thread: %+v", item)
			}
		}
	}
}

// TestFallbackDMReplySkipsAmbiguousMultipleEnvelopeSources guards the
// ambiguous case: when a batch of envelopes triggering one turn names more
// than one distinct source thread, the fallback must not guess and must not
// send anywhere. recordUnansweredAddressedEnvelopes telemetry is the
// existing safety net for that case.
func TestFallbackDMReplySkipsAmbiguousMultipleEnvelopeSources(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	dmTh, err := srv.ensureResidentDMThread(ada)
	if err != nil {
		t.Fatalf("ensureResidentDMThread: %v", err)
	}
	groupA := startGroupThreadWithReqIDForTest(t, srv, "group-a")
	groupB := startGroupThreadWithReqIDForTest(t, srv, "group-b")
	if groupA == groupB {
		t.Fatalf("expected two distinct group threads, got the same id %q twice", groupA)
	}

	turn := Turn{
		ID:     "turn-1",
		Status: TurnStatusCompleted,
		Items: []ThreadItem{
			{ID: "i1", Type: ThreadItemAgentMessage, Text: "ambiguous reply"},
		},
	}
	envs := []MessageEnvelope{
		{ID: "env-1", SourceThreadID: groupA, Addressed: true, SenderKind: "user", Text: "hi"},
		{ID: "env-2", SourceThreadID: groupB, Addressed: true, SenderKind: "user", Text: "hi"},
	}
	srv.fallbackDMReplyFromFinalAnswer(dmTh, ada, envs, turn, time.Now().UTC())

	for _, id := range []string{groupA, groupB} {
		th, err := srv.ensureResidentThread(id)
		if err != nil {
			t.Fatalf("ensureResidentThread %s: %v", id, err)
		}
		th.mu.Lock()
		snapshot := th.snapshotLocked()
		th.mu.Unlock()
		for _, tn := range snapshot.Turns {
			for _, item := range tn.Items {
				if item.Type == ThreadItemParticipantMsg {
					t.Fatalf("unexpected fallback reply in thread %s: %+v", id, item)
				}
			}
		}
	}

	dmTh.mu.Lock()
	dmSnapshot := dmTh.snapshotLocked()
	dmTh.mu.Unlock()
	for _, tn := range dmSnapshot.Turns {
		for _, item := range tn.Items {
			if item.Type == ThreadItemParticipantMsg {
				t.Fatalf("unexpected fallback reply in own DM thread: %+v", item)
			}
		}
	}
}

// startGroupThreadWithReqIDForTest is like startGroupThreadForTest but takes
// an explicit request id so two group threads can be created against the
// same server: responseByID matches the first message with the given id in
// the accumulated output buffer, so reusing startGroupThreadForTest's fixed
// "group" id for a second call would resolve back to the first thread.
func startGroupThreadWithReqIDForTest(t *testing.T, srv *Server, reqID string) string {
	t.Helper()
	raw := fmt.Sprintf(`{"id":%q,"method":"thread/start","params":{}}`, reqID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start group: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	resp := responseByID(t, msgs, reqID)
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/start group returned error: %v", errMsg)
	}
	return remarshal[ThreadStartResult](t, resp["result"]).Thread.ID
}
