package appserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
)

func TestServerStartupFailsClosedWhenPresenceCannotBeAcquired(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = t.TempDir()
	journal, err := session.NewInferenceJournalRuntime(rt.SessionDir, "presence-failure-test")
	if err != nil {
		t.Fatalf("create inference journal runtime: %v", err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	rt.InferenceJournalRuntime = journal

	const participantID = "prt-presence-failure"
	if err := session.UpsertParticipant(rt.SessionDir, participant.Participant{
		ID: participantID, Kind: participant.KindNamed, Name: "Presence failure",
	}); err != nil {
		t.Fatalf("create participant: %v", err)
	}
	if _, err := session.EnqueueResidentEnvelope(rt.SessionDir, session.ResidentEnvelope{
		ID: "env-presence-failure", ParticipantID: participantID, EnvelopeJSON: []byte(`{"text":"pending"}`),
	}); err != nil {
		t.Fatalf("enqueue resident envelope: %v", err)
	}
	if err := os.Mkdir(filepath.Join(rt.SessionDir, ".appserver-startup.lock"), 0o700); err != nil {
		t.Fatalf("create invalid startup gate: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	defer srv.Close()
	if srv.startupErr == nil || !strings.Contains(srv.startupErr.Error(), "app-server presence") {
		t.Fatalf("startup error = %v, want presence acquisition failure", srv.startupErr)
	}
	if srv.sideThreadStore != nil {
		t.Fatal("failed startup initialized the side-thread store")
	}
	if srv.inferenceMaintenanceDone != nil {
		t.Fatal("failed startup launched inference maintenance")
	}
	if err := srv.handleLine(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)); err == nil || !strings.Contains(err.Error(), "app-server presence") {
		t.Fatalf("handleLine error = %v, want startup rejection", err)
	}
	if got := out.String(); got != "" {
		t.Fatalf("failed server served a request: %s", got)
	}

	pending, err := session.PendingResidentEnvelopes(rt.SessionDir, participantID, 0)
	if err != nil {
		t.Fatalf("load resident envelope: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != "env-presence-failure" {
		t.Fatalf("failed startup settled pending work: %+v", pending)
	}
	namedCount, err := session.CountParticipantsByKind(rt.SessionDir, participant.KindNamed)
	if err != nil {
		t.Fatalf("count named participants: %v", err)
	}
	if namedCount != 1 {
		t.Fatalf("failed startup changed named participant count to %d", namedCount)
	}
	if _, err := os.Stat(filepath.Join(rt.WuuHome, defaultAgentSeededMarkerName)); !os.IsNotExist(err) {
		t.Fatalf("failed startup wrote the default participant marker: %v", err)
	}

	runOut := &lockedBuffer{}
	err = RunStdio(context.Background(), rt, strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n"), runOut)
	if err == nil || !strings.Contains(err.Error(), "app-server presence") {
		t.Fatalf("RunStdio error = %v, want startup rejection", err)
	}
	if got := runOut.String(); got != "" {
		t.Fatalf("RunStdio served a request after failed startup: %s", got)
	}
}

func TestSecondLiveServerSkipsBootSettlement(t *testing.T) {
	ownerRuntime := newTestRuntime(t, &fakeClient{})
	owner := New(ownerRuntime, &lockedBuffer{})

	const (
		sourceThread  = "live-source-thread"
		participantID = "prt-live-owner"
	)
	if _, err := session.CreateWithMetadata(ownerRuntime.SessionDir, sourceThread, ownerRuntime.RootDir); err != nil {
		t.Fatalf("create source thread: %v", err)
	}
	if err := session.UpsertParticipant(ownerRuntime.SessionDir, participant.Participant{
		ID: participantID, Kind: participant.KindNamed, Name: "Live owner",
	}); err != nil {
		t.Fatalf("create resident participant: %v", err)
	}
	seq, err := session.AppendHistoryRecordReturningSeq(ownerRuntime.SessionDir, sourceThread, session.HistoryRecord{
		Role: "user", Content: "live source message",
	})
	if err != nil {
		t.Fatalf("append source message: %v", err)
	}
	envelopeJSON, err := json.Marshal(MessageEnvelope{
		ID: "env-live", SourceThreadID: sourceThread, SourceSeq: seq, Text: "still being processed",
	})
	if err != nil {
		t.Fatalf("marshal resident envelope: %v", err)
	}
	if _, err := session.EnqueueResidentEnvelope(ownerRuntime.SessionDir, session.ResidentEnvelope{
		ID: "env-live", ParticipantID: participantID, EnvelopeJSON: envelopeJSON,
	}); err != nil {
		t.Fatalf("enqueue live resident envelope: %v", err)
	}
	if err := session.UpsertMessageMark(ownerRuntime.SessionDir, session.MessageMark{
		SessionID: sourceThread, Seq: seq, ParticipantID: participantID,
		Kind: session.MessageMarkKindSeen, Status: session.SeenStatusInProgress, At: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("mark live receipt in progress: %v", err)
	}

	peerRuntime := newTestRuntime(t, &fakeClient{})
	peerRuntime.SessionDir = ownerRuntime.SessionDir
	peer := New(peerRuntime, &lockedBuffer{})

	pending, err := session.PendingResidentEnvelopes(ownerRuntime.SessionDir, participantID, 0)
	if err != nil {
		t.Fatalf("load envelope after peer startup: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != "env-live" {
		t.Fatalf("peer startup settled the live envelope: %+v", pending)
	}
	marks, err := session.ListMessageMarks(ownerRuntime.SessionDir, sourceThread)
	if err != nil {
		t.Fatalf("load receipt after peer startup: %v", err)
	}
	if len(marks) != 1 || marks[0].Status != session.SeenStatusInProgress {
		t.Fatalf("peer startup settled the live receipt: %+v", marks)
	}

	peer.Close()
	owner.Close()

	recoveryRuntime := newTestRuntime(t, &fakeClient{})
	recoveryRuntime.SessionDir = ownerRuntime.SessionDir
	recovery := New(recoveryRuntime, &lockedBuffer{})
	defer recovery.Close()
	pending, err = session.PendingResidentEnvelopes(ownerRuntime.SessionDir, participantID, 0)
	if err != nil {
		t.Fatalf("load envelope after real restart: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("first server after shutdown did not expire the orphan envelope: %+v", pending)
	}
	marks, err = session.ListMessageMarks(ownerRuntime.SessionDir, sourceThread)
	if err != nil {
		t.Fatalf("load receipt after real restart: %v", err)
	}
	if len(marks) != 1 || marks[0].Status != session.SeenStatusExpiredUnprocessed {
		t.Fatalf("first server after shutdown did not settle the orphan receipt: %+v", marks)
	}
}
