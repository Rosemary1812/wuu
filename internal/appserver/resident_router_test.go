package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

func TestIdleUnreadWakeCandidatePrefersMostUnreadAndSkipsIneligible(t *testing.T) {
	candidates := []idleUnreadCandidate{
		{ParticipantID: "ada", UnreadCount: 2},
		{ParticipantID: "bea", UnreadCount: 5},
		{ParticipantID: "cyd", UnreadCount: 9, Busy: true},
		{ParticipantID: "dan", UnreadCount: 7, LastSpeaker: true},
	}
	got, ok := chooseIdleUnreadCandidate(candidates, rand.New(rand.NewSource(1)))
	if !ok || got.ParticipantID != "bea" {
		t.Fatalf("candidate = %+v, %v; want bea", got, ok)
	}
}

func TestIdleUnreadWakeCandidateRandomizesTies(t *testing.T) {
	candidates := []idleUnreadCandidate{
		{ParticipantID: "ada", UnreadCount: 3},
		{ParticipantID: "bea", UnreadCount: 3},
	}
	seen := map[string]bool{}
	for seed := int64(1); seed <= 20; seed++ {
		got, ok := chooseIdleUnreadCandidate(candidates, rand.New(rand.NewSource(seed)))
		if !ok {
			t.Fatal("expected a candidate")
		}
		seen[got.ParticipantID] = true
	}
	if !seen["ada"] || !seen["bea"] {
		t.Fatalf("tie did not randomize across seeds: %v", seen)
	}
}

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
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
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
	// The envelope carries the source user message's seq (its stable address)
	// so read receipts and reactions can point back at it. Both members' copies
	// of the same source message must agree on the seq, and it must be real.
	if ivyMeta.SourceSeq <= 0 || ivyMeta.SourceSeq != beaMeta.SourceSeq {
		t.Fatalf("envelope SourceSeq: Ivy=%d Bea=%d, want equal and > 0", ivyMeta.SourceSeq, beaMeta.SourceSeq)
	}
}

func TestResidentRouterParticipantMessageHonorsMentionsAndRoutesDeepRelays(t *testing.T) {
	// Wake gating (讨论层): an @mention wakes the addressed member now; an
	// ambient agent post does NOT wake other members — it becomes unread in the
	// ledger and is pulled the next time that member is woken for a real reason.
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	for _, participantID := range []string{ada, bea} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, participantID); err != nil {
			t.Fatalf("AddThreadMember %s: %v", participantID, err)
		}
	}

	// Ada @-mentions Bea: Bea is pushed and woken (addressed must-answer).
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
	if beaMeta.SourceThreadID != groupID || !beaMeta.Addressed || beaMeta.SenderParticipantID != ada {
		t.Fatalf("Bea envelope meta = %+v", beaMeta)
	}

	// Bea replies without @mentioning anyone: an ambient agent post. It must NOT
	// wake Ada — Ada gets no DM turn. The message still lives in the group
	// ledger; Ada would pull it when next woken for a real reason. Asserting the
	// absence: Ada's DM history stays empty after the quiesce settles.
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
	waitForResidentQuiesce(t, srv)
	if _, adaHistory := findDMHistoryIfExists(t, srv, ada); len(adaHistory) != 0 {
		t.Fatalf("ambient agent post must not wake Ada, got DM history %+v", adaHistory)
	}
	// The ledger still holds Bea's post as unread for Ada (pullable on next wake).
	if pending, err := session.ChatMessagesSince(rt.SessionDir, groupID, 0); err != nil {
		t.Fatalf("ChatMessagesSince: %v", err)
	} else if len(pending) < 2 {
		t.Fatalf("group ledger should hold both posts, got %d", len(pending))
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

func TestGroupTurnStartRoutesThroughExplicitAllChannelMembership(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")

	allID, err := srv.ensureAllChannel()
	if err != nil {
		t.Fatalf("ensureAllChannel: %v", err)
	}
	if err := session.AddThreadMember(rt.SessionDir, allID, ivy); err != nil {
		t.Fatalf("AddThreadMember Ivy: %v", err)
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

	_, ivyHistory := waitForResidentDMHistory(t, srv, ivy, 1)
	ivyMeta := findEnvelopeMetaRecord(t, ivyHistory)
	if ivyMeta.SourceThreadID != allID {
		t.Fatalf("expected envelope sourced from #all, got %+v", ivyMeta)
	}
	if _, beaHistory := findDMHistoryIfExists(t, srv, bea); len(beaHistory) != 0 {
		t.Fatalf("non-member Bea must not receive #all traffic, got %+v", beaHistory)
	}
}

func TestAllChannelMentionDoesNotAutoAddMember(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ivy := saveNamedParticipant(t, rt, "Ivy", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")

	allID, err := srv.ensureAllChannel()
	if err != nil {
		t.Fatalf("ensureAllChannel: %v", err)
	}
	if err := session.AddThreadMember(rt.SessionDir, allID, ivy); err != nil {
		t.Fatalf("AddThreadMember Ivy: %v", err)
	}

	raw := fmt.Sprintf(`{"id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"Heads up, @Bea.","mentions":[%q]}}`, allID, bea)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "turn")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("turn/start returned error: %v", errMsg)
	}

	members, err := session.ListThreadMembers(rt.SessionDir, allID)
	if err != nil {
		t.Fatalf("ListThreadMembers: %v", err)
	}
	if len(members) != 1 || members[0] != ivy {
		t.Fatalf("#all mention must not auto-add Bea; members=%+v", members)
	}
	if _, beaHistory := findDMHistoryIfExists(t, srv, bea); len(beaHistory) != 0 {
		t.Fatalf("non-member Bea must not receive #all mention traffic, got %+v", beaHistory)
	}
}

func TestResidentPostMessageToAllChannelRequiresExplicitMembership(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("post_message to #all without explicit membership should fail, got %v", err)
	}

	if err := session.AddThreadMember(srv.rt.SessionDir, allID, participantID); err != nil {
		t.Fatalf("AddThreadMember Iris: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"result","text":"Heads up.","thread_id":%q}`, allID),
	})
	if err != nil {
		t.Fatalf("post_message to #all with explicit membership should succeed: %v", err)
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

func TestResidentEnvelopeDisplayContentIsRawText(t *testing.T) {
	// Display content is the message text only. The source ("from all") is
	// context, carried in EnvelopeMeta and rendered separately by the chat
	// view — it must not leak into the transcript as an "Incoming message
	// from …:" prefix (the DM bug).
	got := residentEnvelopeDisplayContent([]MessageEnvelope{
		{SourceTitle: "all", Text: "你们好"},
	})
	if got != "你们好" {
		t.Fatalf("display content = %q, want raw text %q", got, "你们好")
	}
	if strings.Contains(got, "Incoming message from") {
		t.Fatalf("display content leaked source framing: %q", got)
	}
	// Multiple envelopes join their raw texts, still with no framing.
	multi := residentEnvelopeDisplayContent([]MessageEnvelope{
		{SourceTitle: "all", Text: "hi"},
		{SourceTitle: "all", Text: "there"},
	})
	if multi != "hi\n\nthere" {
		t.Fatalf("multi display content = %q, want %q", multi, "hi\n\nthere")
	}
}

func TestChatMessageItemCarriesEnvelopeMeta(t *testing.T) {
	// A user ThreadItem must carry envelope_meta to the wire, so the chat view
	// renders an envelope notice instead of a plain user bubble that reads as
	// if the user typed the routed-in message.
	meta := json.RawMessage(`[{"id":"e1","source_thread_title":"all"}]`)
	item := chatMessageItem("item-1", providers.ChatMessage{
		Role:           "user",
		DisplayContent: "你们好",
		EnvelopeMeta:   meta,
	})
	if len(item.EnvelopeMeta) == 0 {
		t.Fatalf("chatMessageItem dropped EnvelopeMeta")
	}
	if string(item.EnvelopeMeta) != string(meta) {
		t.Fatalf("EnvelopeMeta = %s, want %s", item.EnvelopeMeta, meta)
	}
	if item.Text != "你们好" {
		t.Fatalf("item.Text = %q, want %q", item.Text, "你们好")
	}
}

func TestFallbackReplyTargetThreadIDHonorsSilenceForUnaddressed(t *testing.T) {
	// Plain user DM turn (no envelopes): a plain-text answer belongs in the
	// resident's own DM.
	if id, sub, ok := fallbackReplyTargetThreadID("dm-1", nil); !ok || id != "dm-1" || sub != "" {
		t.Fatalf("plain DM turn: got (%q,%q,%v), want (dm-1,\"\",true)", id, sub, ok)
	}

	// UN-addressed group message: silence is valid and plain text is private, so
	// the fallback must NOT republish it (this is the "(staying silent)" leak).
	unaddressed := []MessageEnvelope{
		{SourceThreadID: "group-all", Addressed: false, Text: "你们好"},
	}
	if id, _, ok := fallbackReplyTargetThreadID("dm-1", unaddressed); ok {
		t.Fatalf("un-addressed batch should send nothing; got target %q", id)
	}

	// ADDRESSED (@mention/DM) still gets the plain-text fallback, routed to its
	// source thread (rule 1: addressed MUST be answered). Ambient un-addressed
	// envelopes in the same batch don't change the target.
	addressed := []MessageEnvelope{
		{SourceThreadID: "group-all", Addressed: true, Text: "@Bea 在吗"},
		{SourceThreadID: "group-all", Addressed: false, Text: "ambient"},
	}
	if id, sub, ok := fallbackReplyTargetThreadID("dm-1", addressed); !ok || id != "group-all" || sub != "" {
		t.Fatalf("addressed batch: got (%q,%q,%v), want (group-all,\"\",true)", id, sub, ok)
	}

	// Addressed reply-subthread (cth) envelope: the fallback folds back into the
	// same subthread (weak isolation), returning the parent thread + cth id.
	subthreaded := []MessageEnvelope{
		{SourceThreadID: "group-all", SourceSubthreadID: "cth-1", Addressed: true, Text: "@Bea 在吗"},
	}
	if id, sub, ok := fallbackReplyTargetThreadID("dm-1", subthreaded); !ok || id != "group-all" || sub != "cth-1" {
		t.Fatalf("subthread batch: got (%q,%q,%v), want (group-all,cth-1,true)", id, sub, ok)
	}

	// Addressed envelopes disagreeing on source thread → ambiguous → nothing.
	ambiguous := []MessageEnvelope{
		{SourceThreadID: "group-a", Addressed: true, Text: "@X"},
		{SourceThreadID: "group-b", Addressed: true, Text: "@X"},
	}
	if _, _, ok := fallbackReplyTargetThreadID("dm-1", ambiguous); ok {
		t.Fatalf("ambiguous addressed sources should send nothing")
	}

	// Addressed envelopes disagreeing on subthread (one main-stream, one cth) →
	// ambiguous → nothing (must not leak a reply into the main stream).
	mixedSub := []MessageEnvelope{
		{SourceThreadID: "group-all", SourceSubthreadID: "", Addressed: true, Text: "@X"},
		{SourceThreadID: "group-all", SourceSubthreadID: "cth-1", Addressed: true, Text: "@X"},
	}
	if _, _, ok := fallbackReplyTargetThreadID("dm-1", mixedSub); ok {
		t.Fatalf("mixed main-stream/subthread addressed sources should send nothing")
	}
}

func TestRouteSubthreadFansOutToParticipantSubsetOnly(t *testing.T) {
	// Empty provider response: a woken resident produces no final answer, so no
	// fallback reply cascades and the test observes only the routing it triggers.
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	cid := saveNamedParticipant(t, rt, "Cid", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	for _, participantID := range []string{ada, bea, cid} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, participantID); err != nil {
			t.Fatalf("AddThreadMember %s: %v", participantID, err)
		}
	}
	// Reply subthread whose weak-isolation subset is only Bea (Cid is a group
	// member but deliberately NOT a participant of this reply).
	cth, err := session.CreateConversationThread(rt.SessionDir, session.ConversationThread{
		SessionID:    groupID,
		AnchorItemID: "seq-3",
		CreatedBy:    ada,
	})
	if err != nil {
		t.Fatalf("CreateConversationThread: %v", err)
	}
	if err := session.AddConversationThreadMember(rt.SessionDir, cth.ID, bea); err != nil {
		t.Fatalf("seed cth member Bea: %v", err)
	}

	// Ada posts into the reply subthread (thread_id=cth). This hits the
	// publishParticipantMessage short-circuit and routes only to the cth subset.
	if err := srv.publishParticipantMessage(groupID, agentcontrol.ParticipantMessage{
		AgentID:       ada,
		ParticipantID: ada,
		Kind:          "update",
		Hop:           1,
		Text:          "Digging into the failing test.",
		ThreadID:      cth.ID,
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("publish Ada subthread message: %v", err)
	}

	// Bea (a reply participant) is pushed the message, tagged with the subthread.
	_, beaHistory := waitForResidentDMHistory(t, srv, bea, 1)
	beaMeta := findEnvelopeMetaRecord(t, beaHistory)
	if beaMeta.SourceThreadID != groupID || beaMeta.SourceSubthreadID != cth.ID {
		t.Fatalf("Bea envelope meta = %+v, want source=%s subthread=%s", beaMeta, groupID, cth.ID)
	}
	if beaMeta.SenderParticipantID != ada || beaMeta.Addressed {
		t.Fatalf("Bea envelope meta sender/addressed = %+v", beaMeta)
	}

	// Cid (a group member but NOT a reply participant) is never woken: routing
	// enqueues synchronously inside publishParticipantMessage, so by now Cid's
	// inbox is provably empty and no DM turn was ever started for it.
	pending, err := session.PendingResidentEnvelopes(rt.SessionDir, cid, 0)
	if err != nil {
		t.Fatalf("PendingResidentEnvelopes Cid: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("non-participant Cid was woken by a reply message: %d pending envelopes", len(pending))
	}
	if _, cidHistory := findDMHistoryIfExists(t, srv, cid); len(cidHistory) != 0 {
		t.Fatalf("non-participant Cid got a DM turn from a reply message: %+v", cidHistory)
	}

	// The sender auto-joins the reply subthread; the non-participant does not.
	members, err := session.ListConversationThreadMembers(rt.SessionDir, cth.ID)
	if err != nil {
		t.Fatalf("ListConversationThreadMembers: %v", err)
	}
	if len(members) != 2 || !containsMember(members, ada) || !containsMember(members, bea) {
		t.Fatalf("cth members = %v, want Ada (auto-joined) and Bea", members)
	}
	if containsMember(members, cid) {
		t.Fatalf("non-participant Cid must not be in cth members: %v", members)
	}
}

func TestRouteSubthreadMentionPullsGroupMemberIntoReply(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	cid := saveNamedParticipant(t, rt, "Cid", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	for _, participantID := range []string{ada, bea, cid} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, participantID); err != nil {
			t.Fatalf("AddThreadMember %s: %v", participantID, err)
		}
	}
	cth, err := session.CreateConversationThread(rt.SessionDir, session.ConversationThread{
		SessionID:    groupID,
		AnchorItemID: "seq-5",
		CreatedBy:    ada,
	})
	if err != nil {
		t.Fatalf("CreateConversationThread: %v", err)
	}
	if err := session.AddConversationThreadMember(rt.SessionDir, cth.ID, bea); err != nil {
		t.Fatalf("seed cth member Bea: %v", err)
	}

	// Ada @mentions Cid inside the reply: being @'d in a reply joins the reply
	// (被@者加入), not the room. Cid is pulled into the cth subset and addressed.
	if err := srv.publishParticipantMessage(groupID, agentcontrol.ParticipantMessage{
		AgentID:       ada,
		ParticipantID: ada,
		Kind:          "update",
		Hop:           1,
		Text:          "@Cid can you confirm the fix?",
		ThreadID:      cth.ID,
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("publish Ada subthread mention: %v", err)
	}

	_, cidHistory := waitForResidentDMHistory(t, srv, cid, 1)
	cidMeta := findEnvelopeMetaRecord(t, cidHistory)
	if cidMeta.SourceSubthreadID != cth.ID || !cidMeta.Addressed {
		t.Fatalf("mentioned Cid envelope meta = %+v, want subthread=%s addressed=true", cidMeta, cth.ID)
	}

	members, err := session.ListConversationThreadMembers(rt.SessionDir, cth.ID)
	if err != nil {
		t.Fatalf("ListConversationThreadMembers: %v", err)
	}
	if !containsMember(members, cid) {
		t.Fatalf("mentioned Cid should have joined the cth: %v", members)
	}
}

// TestSubthreadMessageEmitsSubthreadUpdatedNotification locks in the W1
// realtime-refresh contract: a cth (reply-subthread) message — which is
// short-circuited off the main stream and emits no turn/item/thread
// notification — must still push a thread/subUpdated notification carrying the
// parent thread id, the subthread id, and the refreshed view (turns +
// reply_count) so the split reply panel streams and the reply badge stays fresh.
func TestSubthreadMessageEmitsSubthreadUpdatedNotification(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	out := &lockedBuffer{}
	srv := New(rt, out)
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	for _, participantID := range []string{ada, bea} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, participantID); err != nil {
			t.Fatalf("AddThreadMember %s: %v", participantID, err)
		}
	}
	cth, err := session.CreateConversationThread(rt.SessionDir, session.ConversationThread{
		SessionID:    groupID,
		AnchorItemID: "seq-7",
		CreatedBy:    ada,
	})
	if err != nil {
		t.Fatalf("CreateConversationThread: %v", err)
	}
	if err := session.AddConversationThreadMember(rt.SessionDir, cth.ID, bea); err != nil {
		t.Fatalf("seed cth member Bea: %v", err)
	}

	if err := srv.publishParticipantMessage(groupID, agentcontrol.ParticipantMessage{
		AgentID:       ada,
		ParticipantID: ada,
		Kind:          "update",
		Hop:           1,
		Text:          "Streaming an update into the reply.",
		ThreadID:      cth.ID,
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("publish Ada subthread message: %v", err)
	}

	msgs := parseOutput(t, out.String())
	var note map[string]any
	for _, msg := range msgs {
		if msg["method"] == NotificationSubthreadUpdated {
			note = msg
			break
		}
	}
	if note == nil {
		t.Fatalf("expected %s notification; output:\n%s", NotificationSubthreadUpdated, out.String())
	}
	// A cth message must NOT leak the main-stream turn/thread notifications the
	// short-circuit deliberately suppresses.
	for _, msg := range msgs {
		if m, _ := msg["method"].(string); m == NotificationTurnStarted || m == NotificationThreadUpdated {
			t.Fatalf("cth message leaked main-stream notification %q; output:\n%s", m, out.String())
		}
	}
	params, ok := note["params"].(map[string]any)
	if !ok {
		t.Fatalf("notification params not an object: %+v", note)
	}
	if params["thread_id"] != groupID {
		t.Fatalf("notification thread_id = %v, want parent %s", params["thread_id"], groupID)
	}
	if params["subthread_id"] != cth.ID {
		t.Fatalf("notification subthread_id = %v, want %s", params["subthread_id"], cth.ID)
	}
	sub, ok := params["subthread"].(map[string]any)
	if !ok {
		t.Fatalf("notification subthread not an object: %+v", params["subthread"])
	}
	if replyCount, _ := sub["reply_count"].(float64); replyCount < 1 {
		t.Fatalf("notification subthread reply_count = %v, want >= 1", sub["reply_count"])
	}
	if turns, _ := sub["turns"].([]any); len(turns) == 0 {
		t.Fatalf("notification subthread carried no turns for the panel to patch: %+v", sub)
	}
}

func containsMember(members []string, want string) bool {
	for _, m := range members {
		if strings.TrimSpace(m) == want {
			return true
		}
	}
	return false
}
