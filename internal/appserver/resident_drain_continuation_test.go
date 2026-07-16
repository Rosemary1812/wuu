package appserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/session"
)

func TestResidentTurnCompletionDrainsMessageArrivingWhileTurnRuns(t *testing.T) {
	blocking := newBlockingStreamClient("resident reply")
	rt := newTestRuntime(t, &fakeClient{})
	rt.StreamRunner.Client = blocking
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() {
		unblockResidentDrainClient(blocking)
		srv.Close()
	})
	residentID := saveNamedParticipant(t, rt, "Running Reader", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(rt.SessionDir, groupID, residentID); err != nil {
		t.Fatal(err)
	}

	base := time.Now().UTC().Add(-time.Minute)
	enqueueResidentDrainEnvelope(t, rt.SessionDir, residentID, groupID, "running-first", base)
	srv.kickResidentAgent(residentID)
	select {
	case <-blocking.started:
	case <-time.After(3 * time.Second):
		t.Fatal("first resident turn did not reach the provider")
	}

	// This kick must observe the resident DM as running and leave the envelope
	// pending. The terminal kick is what admits it after the first turn releases
	// its execution lease.
	enqueueResidentDrainEnvelope(t, rt.SessionDir, residentID, groupID, "running-second", base.Add(time.Second))
	srv.kickResidentAgent(residentID)
	waitForResidentDrainIdle(t, srv, residentID)
	if pending, err := session.PendingResidentEnvelopes(rt.SessionDir, residentID, 0); err != nil || len(pending) != 1 {
		t.Fatalf("inbox while first turn runs = %+v, %v; want second envelope pending", pending, err)
	}

	unblockResidentDrainClient(blocking)
	history := waitForResidentDrainHistory(t, srv, residentID, 2)
	assertResidentDrainMessagesOnceInOrder(t, history, "running-first", "running-second")
	waitForResidentInboxEmpty(t, rt.SessionDir, residentID)
}

func TestResidentDrainPreflightSkipsEmptyDirectDM(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("ok")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(srv.Close)
	residentID := saveNamedParticipant(t, rt, "Quiet Reader", "reviewer", "")
	dm, err := srv.ensureResidentDMThread(residentID)
	if err != nil {
		t.Fatalf("ensure resident DM: %v", err)
	}
	if srv.residentDrainHasPendingWork(residentID, dm.ID) {
		t.Fatal("empty direct DM unexpectedly requires a resident drain")
	}

	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(rt.SessionDir, groupID, residentID); err != nil {
		t.Fatal(err)
	}
	enqueueResidentDrainEnvelope(t, rt.SessionDir, residentID, groupID, "pending", time.Now().UTC())
	if !srv.residentDrainHasPendingWork(residentID, dm.ID) {
		t.Fatal("pending resident envelope was missed by drain preflight")
	}
}

func TestResidentTurnCompletionDrainsBeyondBatchLimit(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("batch reply")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(srv.Close)
	residentID := saveNamedParticipant(t, rt, "Batch Reader", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(rt.SessionDir, groupID, residentID); err != nil {
		t.Fatal(err)
	}

	base := time.Now().UTC().Add(-time.Minute)
	wants := make([]string, 0, residentEnvelopeBatchLimit+1)
	for i := 0; i < residentEnvelopeBatchLimit+1; i++ {
		text := fmt.Sprintf("batch-message-%02d", i)
		wants = append(wants, text)
		enqueueResidentDrainEnvelope(t, rt.SessionDir, residentID, groupID, text, base.Add(time.Duration(i)*time.Millisecond))
	}
	srv.kickResidentAgent(residentID)

	history := waitForResidentDrainHistory(t, srv, residentID, 2)
	assertResidentDrainMessagesOnceInOrder(t, history, wants...)
	waitForResidentInboxEmpty(t, rt.SessionDir, residentID)

	var batches []session.HistoryRecord
	for _, rec := range history {
		if rec.Role == "user" && len(rec.EnvelopeMeta) > 0 {
			batches = append(batches, rec)
		}
	}
	if len(batches) != 2 {
		t.Fatalf("resident batches = %d, want 2", len(batches))
	}
	if strings.Contains(batches[0].Content, wants[residentEnvelopeBatchLimit]) || !strings.Contains(batches[1].Content, wants[residentEnvelopeBatchLimit]) {
		t.Fatalf("batch boundary did not split at %d messages", residentEnvelopeBatchLimit)
	}
}

func enqueueResidentDrainEnvelope(t *testing.T, dir, participantID, sourceThreadID, text string, createdAt time.Time) {
	t.Helper()
	id := "env-" + strings.ReplaceAll(text, " ", "-")
	payload, err := json.Marshal(MessageEnvelope{
		ID: id, SourceThreadID: sourceThreadID, SourceTitle: "resident drain test",
		SenderKind: "user", SenderName: "User", Text: text, CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.EnqueueResidentEnvelope(dir, session.ResidentEnvelope{
		ID: id, ParticipantID: participantID, EnvelopeJSON: payload, CreatedAt: createdAt,
	}); err != nil {
		t.Fatal(err)
	}
}

func unblockResidentDrainClient(client *blockingStreamClient) {
	if client == nil {
		return
	}
	select {
	case <-client.release:
	default:
		close(client.release)
	}
}

func waitForResidentDrainIdle(t *testing.T, srv *Server, participantID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	idleSince := time.Time{}
	for time.Now().Before(deadline) {
		if srv.residentDraining(participantID) {
			idleSince = time.Time{}
		} else if idleSince.IsZero() {
			idleSince = time.Now()
		} else if time.Since(idleSince) >= 100*time.Millisecond {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("resident drain did not become idle while the turn was running")
}

func waitForResidentInboxEmpty(t *testing.T, dir, participantID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pending, err := session.PendingResidentEnvelopes(dir, participantID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("resident inbox did not drain")
}

func waitForResidentDrainHistory(t *testing.T, srv *Server, participantID string, wantUserRecords int) []session.HistoryRecord {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, history := findDMHistoryIfExists(t, srv, participantID)
		users := 0
		for _, rec := range history {
			if rec.Role == "user" && len(rec.EnvelopeMeta) > 0 {
				users++
			}
		}
		if users >= wantUserRecords {
			return history
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d resident drain batches for %s", wantUserRecords, participantID)
	return nil
}

func assertResidentDrainMessagesOnceInOrder(t *testing.T, history []session.HistoryRecord, wants ...string) {
	t.Helper()
	var batches []string
	for _, rec := range history {
		if rec.Role == "user" && len(rec.EnvelopeMeta) > 0 {
			batches = append(batches, rec.Content)
		}
	}
	combined := strings.Join(batches, "\n")
	previous := -1
	for _, want := range wants {
		if count := strings.Count(combined, want); count != 1 {
			t.Fatalf("resident history contains %q %d times, want once", want, count)
		}
		index := strings.Index(combined, want)
		if index <= previous {
			t.Fatalf("resident history order is wrong for %q", wants)
		}
		previous = index
	}
}
