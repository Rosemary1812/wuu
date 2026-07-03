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
