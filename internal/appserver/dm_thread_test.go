package appserver

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
)

func TestThreadStartWithDMParticipantTagsThread(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	srv := New(rt, &lockedBuffer{})

	participantID := saveNamedParticipant(t, rt, "Andy", "general-purpose", "")

	raw := fmt.Sprintf(`{"id":"start","method":"thread/start","params":{"dm_participant_id":%q}}`, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}

	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	resp := responseByID(t, msgs, "start")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/start returned error: %v", errMsg)
	}
	start := remarshal[ThreadStartResult](t, resp["result"])
	if start.Thread.DMParticipantID != participantID {
		t.Fatalf("DMParticipantID = %q, want %q", start.Thread.DMParticipantID, participantID)
	}
	if start.Thread.Title != "Andy" {
		t.Fatalf("Title = %q, want %q", start.Thread.Title, "Andy")
	}
	if start.Thread.Ephemeral {
		t.Fatalf("DM thread must not be ephemeral")
	}
}

func TestThreadStartWithDMParticipantUnknownIDErrors(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	srv := New(rt, &lockedBuffer{})

	raw := `{"id":"start","method":"thread/start","params":{"dm_participant_id":"prt-does-not-exist"}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start returned transport error: %v", err)
	}

	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	resp := responseByID(t, msgs, "start")
	errMsg, ok := resp["error"]
	if !ok {
		t.Fatalf("expected error response, got: %+v", resp)
	}
	errStr := fmt.Sprint(errMsg)
	if !strings.Contains(errStr, "participant") {
		t.Fatalf("error should mention participant, got %q", errStr)
	}
}

func TestThreadStartWithDMParticipantAndEphemeralErrors(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	srv := New(rt, &lockedBuffer{})

	participantID := saveNamedParticipant(t, rt, "Andy", "general-purpose", "")

	raw := fmt.Sprintf(`{"id":"start","method":"thread/start","params":{"dm_participant_id":%q,"ephemeral":true}}`, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start returned transport error: %v", err)
	}

	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	resp := responseByID(t, msgs, "start")
	errMsg, ok := resp["error"]
	if !ok {
		t.Fatalf("expected error response, got: %+v", resp)
	}
	errStr := fmt.Sprint(errMsg)
	if !strings.Contains(errStr, "ephemeral") {
		t.Fatalf("error should mention ephemeral, got %q", errStr)
	}
}

func TestThreadListCarriesDMParticipant(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	srv := New(rt, &lockedBuffer{})

	participantID := saveNamedParticipant(t, rt, "Bea", "general-purpose", "")

	raw := fmt.Sprintf(`{"id":"start","method":"thread/start","params":{"dm_participant_id":%q}}`, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}

	if err := srv.handleLine(context.Background(), []byte(`{"id":"list","method":"thread/list"}`)); err != nil {
		t.Fatalf("thread/list: %v", err)
	}

	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	listResp := responseByID(t, msgs, "list")
	list := remarshal[ThreadListResult](t, listResp["result"])

	var found *Thread
	for i := range list.Threads {
		if list.Threads[i].DMParticipantID == participantID {
			found = &list.Threads[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("thread/list did not include DM thread; threads=%+v", list.Threads)
	}
	if found.Title != "Bea" {
		t.Fatalf("Title = %q, want %q", found.Title, "Bea")
	}
}

func TestThreadResumeCarriesDMParticipant(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})

	participantID := saveNamedParticipant(t, rt, "Cleo", "general-purpose", "")

	// Persist the session metadata ahead of time so thread/resume loads it
	// via the persisted-state path.
	if _, err := session.CreateWithMetadata(rt.SessionDir, "dm-resume", rt.RootDir); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := session.BindDMParticipant(rt.SessionDir, "dm-resume", participantID); err != nil {
		t.Fatalf("BindDMParticipant: %v", err)
	}
	if _, err := session.UpdateTitle(rt.SessionDir, "dm-resume", "Cleo"); err != nil {
		t.Fatalf("UpdateTitle: %v", err)
	}

	srv := New(rt, &lockedBuffer{})
	raw := `{"id":"resume","method":"thread/resume","params":{"session_id":"dm-resume"}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	resp := responseByID(t, msgs, "resume")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/resume returned error: %v", errMsg)
	}
	resume := remarshal[ThreadResumeResult](t, resp["result"])
	if resume.Thread.DMParticipantID != participantID {
		t.Fatalf("DMParticipantID = %q, want %q", resume.Thread.DMParticipantID, participantID)
	}
	if resume.Thread.Title != "Cleo" {
		t.Fatalf("Title = %q, want %q", resume.Thread.Title, "Cleo")
	}
}