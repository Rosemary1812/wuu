package appserver

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
)

func TestCollaborationThreadUsesGlobalScratchWorkspace(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = retryingTempDir(t)
	rt.WorkspaceID = "project-1"
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start","params":{"collaboration":true}}`)); err != nil {
		t.Fatalf("thread/start collaboration: %v", err)
	}
	response := responseByID(t, parseOutput(t, out.String()), "1")
	if response["error"] != nil {
		t.Fatalf("thread/start collaboration error: %+v", response["error"])
	}
	thread := remarshal[ThreadStartResult](t, response["result"]).Thread
	wantCWD := filepath.Join(rt.WuuHome, "scratch", ThreadSourceCollaboration)
	if thread.Source != ThreadSourceCollaboration || thread.CWD != wantCWD || thread.WorkspaceKind != WorkspaceKindScratch {
		t.Fatalf("collaboration thread = %+v, want source=%q cwd=%q scratch workspace", thread, ThreadSourceCollaboration, wantCWD)
	}
	stored, ok, err := session.Find(rt.SessionDir, thread.ID)
	if err != nil || !ok {
		t.Fatalf("find collaboration session: ok=%v err=%v", ok, err)
	}
	if stored.Source != ThreadSourceCollaboration || stored.WorkspaceID != "" {
		t.Fatalf("stored collaboration metadata = %+v", stored)
	}

	if err := srv.handleLine(context.Background(), []byte(`{"id":"2","method":"thread/list","params":{"cwd":"/unrelated/project"}}`)); err != nil {
		t.Fatalf("thread/list from unrelated workspace: %v", err)
	}
	listed := remarshal[ThreadListResult](t, responseByID(t, parseOutput(t, out.String()), "2")["result"])
	if len(listed.Threads) != 1 || listed.Threads[0].ID != thread.ID || listed.Threads[0].Source != ThreadSourceCollaboration {
		t.Fatalf("global collaboration thread missing from unrelated workspace list: %+v", listed.Threads)
	}

	if err := srv.handleLine(context.Background(), []byte(`{"id":"3","method":"thread/start","params":{"collaboration":true}}`)); err != nil {
		t.Fatalf("second collaboration thread/start: %v", err)
	}
	reused := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "3")["result"]).Thread
	if reused.ID != thread.ID {
		t.Fatalf("second collaboration start created %q, want existing %q", reused.ID, thread.ID)
	}
	sessions, err := session.List(rt.SessionDir, 0)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("collaboration start must remain idempotent, sessions = %+v", sessions)
	}
}

func TestCollaborationThreadCannotBeEphemeral(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start","params":{"collaboration":true,"ephemeral":true}}`)); err != nil {
		t.Fatalf("thread/start transport: %v", err)
	}
	response := responseByID(t, parseOutput(t, out.String()), "1")
	if response["error"] == nil {
		t.Fatalf("collaboration + ephemeral unexpectedly succeeded: %+v", response)
	}
}

func TestCollaborationThreadStartsFreshAfterArchive(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = retryingTempDir(t)
	out := &lockedBuffer{}
	srv := New(rt, out)

	dispatchPayload(t, srv, "1", "thread/start", ThreadStartParams{Collaboration: true})
	first := responseByID(t, parseOutput(t, out.String()), "1")
	if first["error"] != nil {
		t.Fatalf("first collaboration start rejected: %+v", first["error"])
	}
	firstThread := remarshal[ThreadStartResult](t, first["result"]).Thread
	dispatchPayload(t, srv, "2", "thread/archive", ThreadArchiveParams{ThreadID: firstThread.ID, Archived: true})
	archived := responseByID(t, parseOutput(t, out.String()), "2")
	if archived["error"] != nil {
		t.Fatalf("archive collaboration thread: %+v", archived["error"])
	}
	dispatchPayload(t, srv, "3", "thread/start", ThreadStartParams{Collaboration: true})
	second := responseByID(t, parseOutput(t, out.String()), "3")
	if second["error"] != nil {
		t.Fatalf("second collaboration start rejected: %+v", second["error"])
	}
	secondThread := remarshal[ThreadStartResult](t, second["result"]).Thread
	if secondThread.ID == firstThread.ID {
		t.Fatalf("archived collaboration thread %q was reused", firstThread.ID)
	}
	if secondThread.Source != ThreadSourceCollaboration {
		t.Fatalf("new collaboration thread source = %q", secondThread.Source)
	}
}
