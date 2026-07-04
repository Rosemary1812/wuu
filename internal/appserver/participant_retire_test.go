package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

// retireTestSaveParticipant creates a named participant through the
// participant/save RPC so the workspace directory and MEMORY.md exist on
// disk, and returns the saved profile.
func retireTestSaveParticipant(t *testing.T, srv *Server, id, name, memory string) ParticipantProfile {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"id":     id,
		"method": MethodParticipantSave,
		"params": ParticipantSaveParams{Name: name, Role: "reviewer", Memory: memory},
	})
	if err != nil {
		t.Fatalf("marshal save: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("participant/save: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), id)
	if resp["error"] != nil {
		t.Fatalf("participant/save error: %v", resp["error"])
	}
	return remarshal[ParticipantSaveResult](t, resp["result"]).Participant
}

func retireTestStartDM(t *testing.T, srv *Server, reqID, participantID string) Thread {
	t.Helper()
	raw := fmt.Sprintf(`{"id":%q,"method":"thread/start","params":{"dm_participant_id":%q}}`, reqID, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), reqID)
	if resp["error"] != nil {
		t.Fatalf("thread/start error: %v", resp["error"])
	}
	return remarshal[ThreadStartResult](t, resp["result"]).Thread
}

func retireTestRetire(t *testing.T, srv *Server, reqID, participantID string) map[string]any {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"id":     reqID,
		"method": MethodParticipantRetire,
		"params": ParticipantRetireParams{ParticipantID: participantID},
	})
	if err != nil {
		t.Fatalf("marshal retire: %v", err)
	}
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("participant/retire: %v", err)
	}
	return responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), reqID)
}

// TestParticipantRetireArchivesDirectories asserts the disk half of the
// retire cleanup protocol: participants/<id>/ (including the memory/
// notebook) moves to participants/.archived/<id>/ and agents/<id>/home moves
// to agents/.archived/<id>-home/. Nothing is deleted — 归档不硬删 is a red
// line (memory-redesign §9).
func TestParticipantRetireArchivesDirectories(t *testing.T) {
	_, srv, wuuHome := withDMRuntime(t)

	saved := retireTestSaveParticipant(t, srv, "save-gale", "Gale", "important lore")
	if saved.Workspace != filepath.Join(wuuHome, "participants", saved.ID) {
		t.Fatalf("workspace = %q, want under %q", saved.Workspace, wuuHome)
	}
	// Seed an identity notebook inside the workspace.
	notebookDir := filepath.Join(saved.Workspace, "memory")
	if err := os.MkdirAll(notebookDir, 0o755); err != nil {
		t.Fatalf("mkdir notebook: %v", err)
	}
	if err := os.WriteFile(filepath.Join(notebookDir, "MEMORY.md"), []byte("- [Lesson](lesson.md)"), 0o644); err != nil {
		t.Fatalf("write notebook index: %v", err)
	}
	// Create the DM thread so the agent home exists on disk.
	retireTestStartDM(t, srv, "dm-gale", saved.ID)
	agentHome := statepath.AgentHomeDir(wuuHome, saved.ID)
	if _, err := os.Stat(agentHome); err != nil {
		t.Fatalf("agent home should exist before retire: %v", err)
	}

	resp := retireTestRetire(t, srv, "retire-gale", saved.ID)
	if resp["error"] != nil {
		t.Fatalf("retire error: %v", resp["error"])
	}

	if _, err := os.Stat(saved.Workspace); !os.IsNotExist(err) {
		t.Errorf("original workspace should be moved away, stat err = %v", err)
	}
	archivedWorkspace := filepath.Join(wuuHome, "participants", ".archived", saved.ID)
	memory, err := os.ReadFile(filepath.Join(archivedWorkspace, participantMemoryFileName))
	if err != nil {
		t.Fatalf("archived MEMORY.md should exist: %v", err)
	}
	if string(memory) != "important lore" {
		t.Errorf("archived MEMORY.md = %q, want %q", memory, "important lore")
	}
	notebook, err := os.ReadFile(filepath.Join(archivedWorkspace, "memory", "MEMORY.md"))
	if err != nil {
		t.Fatalf("archived memory notebook must survive retire (red line): %v", err)
	}
	if !strings.Contains(string(notebook), "Lesson") {
		t.Errorf("archived notebook index = %q, want Lesson entry", notebook)
	}

	if _, err := os.Stat(agentHome); !os.IsNotExist(err) {
		t.Errorf("agent home should be moved away, stat err = %v", err)
	}
	archivedHome := filepath.Join(wuuHome, "agents", ".archived", saved.ID+"-home")
	if info, err := os.Stat(archivedHome); err != nil || !info.IsDir() {
		t.Errorf("archived agent home missing: %v", err)
	}

	// The store row must point at the archived workspace so profile reads
	// (and a future rehire) see the real on-disk location.
	p, err := session.GetParticipant(srv.rt.SessionDir, saved.ID)
	if err != nil {
		t.Fatalf("GetParticipant after retire: %v", err)
	}
	if p.RetiredAt == nil {
		t.Errorf("RetiredAt = nil after retire")
	}
	if p.Workspace != archivedWorkspace {
		t.Errorf("workspace after retire = %q, want %q", p.Workspace, archivedWorkspace)
	}
	retired := remarshal[ParticipantRetireResult](t, resp["result"])
	if retired.Participant.Memory != "important lore" {
		t.Errorf("retire response memory = %q, want archived content readable", retired.Participant.Memory)
	}
}

// TestParticipantRetireFreezesDMThread asserts the runtime half of the
// cleanup: the loaded DM thread flips read-only, turn/start rejects with an
// explicit "retired" error, and turn/queue rejects via the read-only flag.
func TestParticipantRetireFreezesDMThread(t *testing.T) {
	_, srv, _ := withDMRuntime(t)

	saved := retireTestSaveParticipant(t, srv, "save-hope", "Hope", "")
	dm := retireTestStartDM(t, srv, "dm-hope", saved.ID)

	if resp := retireTestRetire(t, srv, "retire-hope", saved.ID); resp["error"] != nil {
		t.Fatalf("retire error: %v", resp["error"])
	}

	turnRaw := fmt.Sprintf(`{"id":"turn-frozen","method":"turn/start","params":{"thread_id":%q,"prompt":"hello?"}}`, dm.ID)
	if err := srv.handleLine(context.Background(), []byte(turnRaw)); err != nil {
		t.Fatalf("turn/start transport: %v", err)
	}
	turnResp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "turn-frozen")
	if turnResp["error"] == nil {
		t.Fatalf("turn/start on retired DM should fail, got %+v", turnResp["result"])
	}
	msg, _ := turnResp["error"].(map[string]any)["message"].(string)
	if !strings.Contains(strings.ToLower(msg), "retired") {
		t.Errorf("turn/start error should say retired, got %q", msg)
	}

	queueRaw := fmt.Sprintf(`{"id":"queue-frozen","method":"turn/queue","params":{"thread_id":%q,"prompt":"later?"}}`, dm.ID)
	if err := srv.handleLine(context.Background(), []byte(queueRaw)); err != nil {
		t.Fatalf("turn/queue transport: %v", err)
	}
	queueResp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "queue-frozen")
	if queueResp["error"] == nil {
		t.Fatalf("turn/queue on retired DM should fail, got %+v", queueResp["result"])
	}
	qmsg, _ := queueResp["error"].(map[string]any)["message"].(string)
	if !strings.Contains(strings.ToLower(qmsg), "read-only") {
		t.Errorf("turn/queue error should say read-only, got %q", qmsg)
	}
}

// TestRetiredDMThreadLoadsReadOnly asserts the freeze survives a server
// restart: ReadOnly is not persisted, so it must be re-derived from the
// participant row when the DM thread loads from disk.
func TestRetiredDMThreadLoadsReadOnly(t *testing.T) {
	rt, srv, wuuHome := withDMRuntime(t)

	saved := retireTestSaveParticipant(t, srv, "save-iris", "Iris", "")
	dm := retireTestStartDM(t, srv, "dm-iris", saved.ID)
	if resp := retireTestRetire(t, srv, "retire-iris", saved.ID); resp["error"] != nil {
		t.Fatalf("retire error: %v", resp["error"])
	}

	// Fresh server over the same state — nothing in memory.
	rt2 := newTestRuntime(t, &fakeClient{})
	rt2.WuuHome = wuuHome
	rt2.SessionDir = rt.SessionDir
	srv2 := New(rt2, &lockedBuffer{})

	resumeRaw := fmt.Sprintf(`{"id":"resume","method":"thread/resume","params":{"session_id":%q}}`, dm.ID)
	if err := srv2.handleLine(context.Background(), []byte(resumeRaw)); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}
	resumeResp := responseByID(t, parseOutput(t, srv2.out.(*lockedBuffer).String()), "resume")
	if resumeResp["error"] != nil {
		t.Fatalf("thread/resume error: %v", resumeResp["error"])
	}
	resumed := remarshal[ThreadResumeResult](t, resumeResp["result"]).Thread
	if !resumed.ReadOnly {
		t.Errorf("retired DM thread should load read-only after restart")
	}
}

// TestThreadStartRejectsNewDMForRetiredParticipant: with no pre-existing DM
// thread, thread/start with a retired dm_participant_id must fail clearly
// instead of minting a fresh DM for an archived identity.
func TestThreadStartRejectsNewDMForRetiredParticipant(t *testing.T) {
	_, srv, _ := withDMRuntime(t)

	saved := retireTestSaveParticipant(t, srv, "save-june", "June", "")
	if resp := retireTestRetire(t, srv, "retire-june", saved.ID); resp["error"] != nil {
		t.Fatalf("retire error: %v", resp["error"])
	}

	raw := fmt.Sprintf(`{"id":"dm-after","method":"thread/start","params":{"dm_participant_id":%q}}`, saved.ID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start transport: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "dm-after")
	if resp["error"] == nil {
		t.Fatalf("thread/start for retired participant should fail, got %+v", resp["result"])
	}
	msg, _ := resp["error"].(map[string]any)["message"].(string)
	if !strings.Contains(strings.ToLower(msg), "retired") {
		t.Errorf("error should say retired, got %q", msg)
	}
}

// TestParticipantSaveDetectsArchivedPredecessor: creating a named
// participant whose name matches a retired predecessor surfaces the
// predecessor's ID (same-name rebuild guard). The new identity still starts
// fresh — rehire UI is a later phase.
func TestParticipantSaveDetectsArchivedPredecessor(t *testing.T) {
	_, srv, _ := withDMRuntime(t)

	first := retireTestSaveParticipant(t, srv, "save-noel-1", "Noel", "generation one lore")
	if resp := retireTestRetire(t, srv, "retire-noel-1", first.ID); resp["error"] != nil {
		t.Fatalf("retire error: %v", resp["error"])
	}

	raw, _ := json.Marshal(map[string]any{
		"id":     "save-noel-2",
		"method": MethodParticipantSave,
		"params": ParticipantSaveParams{Name: "noel", Role: "qa"},
	})
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("second save: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "save-noel-2")
	if resp["error"] != nil {
		t.Fatalf("second save error: %v", resp["error"])
	}
	second := remarshal[ParticipantSaveResult](t, resp["result"])
	if second.Participant.ID == first.ID {
		t.Fatalf("rebuild must mint a fresh ID, got predecessor's %q", first.ID)
	}
	if second.ArchivedPredecessorID != first.ID {
		t.Errorf("ArchivedPredecessorID = %q, want %q", second.ArchivedPredecessorID, first.ID)
	}
	if second.Participant.Memory != "" {
		t.Errorf("rebuilt participant must start fresh, got memory %q", second.Participant.Memory)
	}

	// Updating the existing rebuilt participant is NOT a creation: the
	// predecessor hint must not reappear.
	updateRaw, _ := json.Marshal(map[string]any{
		"id":     "save-noel-3",
		"method": MethodParticipantSave,
		"params": ParticipantSaveParams{ID: second.Participant.ID, Name: "Noel", Role: "qa"},
	})
	if err := srv.handleLine(context.Background(), updateRaw); err != nil {
		t.Fatalf("update save: %v", err)
	}
	updateResp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "save-noel-3")
	if updateResp["error"] != nil {
		t.Fatalf("update save error: %v", updateResp["error"])
	}
	update := remarshal[ParticipantSaveResult](t, updateResp["result"])
	if update.ArchivedPredecessorID != "" {
		t.Errorf("update should not carry ArchivedPredecessorID, got %q", update.ArchivedPredecessorID)
	}
}

// TestRosterSaveDetectsArchivedPredecessor mirrors the guard on the
// manage_participant roster path.
func TestRosterSaveDetectsArchivedPredecessor(t *testing.T) {
	_, srv, _ := withDMRuntime(t)
	roster := srv.participantRosterForTool()

	first, err := roster.Save(context.Background(), "agent-1", agentcontrol.RosterSaveRequest{Name: "Mars", Role: "reviewer"})
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if first.ArchivedPredecessorID != "" {
		t.Errorf("first creation has no predecessor, got %q", first.ArchivedPredecessorID)
	}
	if _, err := roster.Retire(context.Background(), "agent-1", "Mars"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	second, err := roster.Save(context.Background(), "agent-1", agentcontrol.RosterSaveRequest{Name: "mars", Role: "qa"})
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if second.ID == first.ID {
		t.Fatalf("rebuild must mint a fresh ID")
	}
	if second.ArchivedPredecessorID != first.ID {
		t.Errorf("ArchivedPredecessorID = %q, want %q", second.ArchivedPredecessorID, first.ID)
	}

	// A plain update of the active rebuilt entry must not re-surface it.
	third, err := roster.Save(context.Background(), "agent-1", agentcontrol.RosterSaveRequest{Name: "Mars", Tagline: "still here"})
	if err != nil {
		t.Fatalf("third save: %v", err)
	}
	if third.ID != second.ID {
		t.Fatalf("update should keep the active entry's ID")
	}
	if third.ArchivedPredecessorID != "" {
		t.Errorf("update should not carry ArchivedPredecessorID, got %q", third.ArchivedPredecessorID)
	}
}

// TestRosterRetireRunsFullCleanup drives the manage_participant tool's
// roster path and asserts it shares the same cleanup protocol as the RPC:
// derived rows removed, workspace archived.
func TestRosterRetireRunsFullCleanup(t *testing.T) {
	_, srv, wuuHome := withDMRuntime(t)
	roster := srv.participantRosterForTool()

	entry, err := roster.Save(context.Background(), "agent-1", agentcontrol.RosterSaveRequest{
		Name:       "Kira",
		Role:       "reviewer",
		MemorySeed: "seeded memory",
	})
	if err != nil {
		t.Fatalf("roster save: %v", err)
	}

	// Give the participant derived state: group membership + pending envelope.
	if _, err := session.CreateWithMetadata(srv.rt.SessionDir, "sess-roster-group", t.TempDir()); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := session.AddThreadMember(srv.rt.SessionDir, "sess-roster-group", entry.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if _, err := session.EnqueueResidentEnvelope(srv.rt.SessionDir, session.ResidentEnvelope{
		ParticipantID: entry.ID,
		EnvelopeJSON:  []byte(`{"text":"pending"}`),
	}); err != nil {
		t.Fatalf("enqueue envelope: %v", err)
	}

	if _, err := roster.Retire(context.Background(), "agent-1", "Kira"); err != nil {
		t.Fatalf("roster retire: %v", err)
	}

	members, err := session.ListThreadMembers(srv.rt.SessionDir, "sess-roster-group")
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("thread members after retire = %v, want empty", members)
	}
	archived := filepath.Join(wuuHome, "participants", ".archived", entry.ID)
	data, err := os.ReadFile(filepath.Join(archived, participantMemoryFileName))
	if err != nil {
		t.Fatalf("archived MEMORY.md should exist after roster retire: %v", err)
	}
	if !strings.Contains(string(data), "seeded memory") {
		t.Errorf("archived MEMORY.md = %q, want seeded content", data)
	}
	if _, err := session.PendingResidentEnvelopes(srv.rt.SessionDir, entry.ID, 0); err == nil {
		t.Errorf("pending envelope reads for a retired participant should be rejected")
	}
}
