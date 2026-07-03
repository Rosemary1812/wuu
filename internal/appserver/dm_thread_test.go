package appserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

// withDMRuntime builds a runtime/server with both WuuHome (for agent home
// directory) and RootDir (the active project) pointing at different temp
// dirs. The server is constructed inside the helper so the default participant
// seed runs here — callers must not call New on the runtime.
func withDMRuntime(t *testing.T) (*runtime.Session, *Server, string) {
	t.Helper()
	rt := newTestRuntime(t, &fakeClient{})
	wuuHome := t.TempDir()
	rt.WuuHome = wuuHome
	return rt, New(rt, &lockedBuffer{}), wuuHome
}

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

func TestThreadStartWithDMParticipantUsesAgentHome(t *testing.T) {
	rt, srv, wuuHome := withDMRuntime(t)

	participantID := saveNamedParticipant(t, rt, "Dana", "general-purpose", "")

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

	wantHome := statepath.AgentHomeDir(wuuHome, participantID)
	if start.Thread.CWD != wantHome {
		t.Fatalf("CWD = %q, want %q", start.Thread.CWD, wantHome)
	}
	if start.Thread.WorkspaceKind != WorkspaceKindDM {
		t.Fatalf("WorkspaceKind = %q, want %q", start.Thread.WorkspaceKind, WorkspaceKindDM)
	}
	info, err := os.Stat(wantHome)
	if err != nil {
		t.Fatalf("agent home dir not created on disk: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("agent home %q is not a directory", wantHome)
	}
	if start.Thread.CWD == rt.RootDir {
		t.Fatalf("DM thread CWD = RootDir %q; should live in per-agent home", rt.RootDir)
	}
}

func TestWorkspaceKindForCWDDetectsAgentHome(t *testing.T) {
	wuuHome := t.TempDir()
	agentHome := statepath.AgentHomeDir(wuuHome, "prt-x")
	if got := workspaceKindForCWD(wuuHome, agentHome); got != WorkspaceKindDM {
		t.Fatalf("workspaceKindForCWD(%q) = %q, want %q", agentHome, got, WorkspaceKindDM)
	}
}

func TestWorkspaceKindForCWDProjectUnchanged(t *testing.T) {
	wuuHome := t.TempDir()
	projectDir := filepath.Join(t.TempDir(), "my-project")
	if got := workspaceKindForCWD(wuuHome, projectDir); got != WorkspaceKindProject {
		t.Fatalf("workspaceKindForCWD(%q) = %q, want %q", projectDir, got, WorkspaceKindProject)
	}
}

func TestListForCWDIncludesDMSessionsRegardlessOfCWD(t *testing.T) {
	rt, srv, wuuHome := withDMRuntime(t)

	participantID := saveNamedParticipant(t, rt, "Eli", "general-purpose", "")

	// Create the DM thread on this server (RootDir = projectA).
	raw := fmt.Sprintf(`{"id":"start","method":"thread/start","params":{"dm_participant_id":%q}}`, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	startMsgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	startResp := responseByID(t, startMsgs, "start")
	if errMsg, ok := startResp["error"]; ok {
		t.Fatalf("thread/start returned error: %v", errMsg)
	}
	dmID := remarshal[ThreadStartResult](t, startResp["result"]).Thread.ID

	// Spin up a second server with a completely different RootDir (projectB)
	// but the same SessionDir/WuuHome — simulates a different project open
	// in a different desktop window.
	rt2 := newTestRuntime(t, &fakeClient{})
	rt2.WuuHome = wuuHome
	rt2.SessionDir = rt.SessionDir
	srv2 := New(rt2, &lockedBuffer{})

	listReq := `{"id":"list","method":"thread/list"}`
	if err := srv2.handleLine(context.Background(), []byte(listReq)); err != nil {
		t.Fatalf("thread/list: %v", err)
	}
	listMsgs := parseOutput(t, srv2.out.(*lockedBuffer).String())
	listResp := responseByID(t, listMsgs, "list")
	list := remarshal[ThreadListResult](t, listResp["result"])

	found := false
	for _, th := range list.Threads {
		if th.ID != dmID {
			continue
		}
		found = true
		if th.DMParticipantID != participantID {
			t.Fatalf("DM thread DMParticipantID = %q, want %q", th.DMParticipantID, participantID)
		}
		if th.CWD != statepath.AgentHomeDir(wuuHome, participantID) {
			t.Fatalf("DM thread CWD = %q, want %q", th.CWD, statepath.AgentHomeDir(wuuHome, participantID))
		}
	}
	if !found {
		t.Fatalf("DM thread %q not visible from unrelated-project server; threads=%+v", dmID, list.Threads)
	}
}

func TestThreadListNoDuplicateForDMFromOwnServer(t *testing.T) {
	rt, srv, _ := withDMRuntime(t)

	participantID := saveNamedParticipant(t, rt, "Fran", "general-purpose", "")

	raw := fmt.Sprintf(`{"id":"start","method":"thread/start","params":{"dm_participant_id":%q}}`, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	if err := srv.handleLine(context.Background(), []byte(`{"id":"list","method":"thread/list"}`)); err != nil {
		t.Fatalf("thread/list: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	list := remarshal[ThreadListResult](t, responseByID(t, msgs, "list")["result"])

	count := 0
	for _, th := range list.Threads {
		if th.DMParticipantID == participantID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("DM thread appeared %d times in own-server list, want 1", count)
	}
}

func TestListForCWDIncludesPersistedDMFromUnrelatedServer(t *testing.T) {
	// Direct session-level test: confirms ListForCWDWithDMs surfaces DMs even
	// when the queried cwd is a totally different project.
	rt, _, wuuHome := withDMRuntime(t)

	participantID := saveNamedParticipant(t, rt, "Gabe", "general-purpose", "")

	// Create a DM session directly (no server), with cwd = agent home.
	agentHome := statepath.AgentHomeDir(wuuHome, participantID)
	if err := os.MkdirAll(agentHome, 0o755); err != nil {
		t.Fatalf("mkdir agent home: %v", err)
	}
	sess, err := session.CreateWithMetadata(rt.SessionDir, "dm-direct", agentHome)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := session.BindDMParticipant(rt.SessionDir, sess.ID, participantID); err != nil {
		t.Fatalf("bind dm: %v", err)
	}
	// Sanity: a normal project cwd query on the SAME session dir but a
	// different project RootDir must still include the DM.
	unrelated := filepath.Join(t.TempDir(), "unrelated-project")
	got, err := session.ListForCWDWithDMs(rt.SessionDir, unrelated, 0)
	if err != nil {
		t.Fatalf("ListForCWDWithDMs: %v", err)
	}
	found := false
	for _, s := range got {
		if s.ID == sess.ID {
			found = true
			if s.DMParticipantID != participantID {
				t.Fatalf("DMParticipantID = %q, want %q", s.DMParticipantID, participantID)
			}
		}
	}
	if !found {
		t.Fatalf("ListForCWDWithDMs(unrelated) omitted DM session %q; got %+v", sess.ID, got)
	}
}
