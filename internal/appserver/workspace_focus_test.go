package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// newFocusTestServer builds a runtime/server pair with WuuHome pointing at
// its own temp dir and a registered workspace named "acme". Returns the
// workspace root so tests can assert declaration metadata and tool cwd.
func newFocusTestServer(t *testing.T, client *fakeClient) (*Server, *lockedBuffer, *runtime.Session, string) {
	t.Helper()
	rt := newTestRuntime(t, client)
	rt.WuuHome = t.TempDir()
	workspaceRoot := t.TempDir()
	store := fmt.Sprintf(`{"projects":[{"id":"p1","name":"acme","path":%q}]}`, workspaceRoot)
	if err := os.WriteFile(filepath.Join(rt.WuuHome, "projects.json"), []byte(store), 0o644); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}
	out := &lockedBuffer{}
	srv := New(rt, out)
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	return srv, out, rt, workspaceRoot
}

// waitForResidentQuiesce waits until the server has no resident inbox
// drain in flight and holds that state briefly. Post-turn goroutines
// (resident drain, title generation) touch the sqlite store after
// turn/completed fires; without a quiesce, t.TempDir cleanup can race a
// goroutine recreating WAL files and fail with "directory not empty".
func waitForResidentQuiesce(t *testing.T, srv *Server) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	idleSince := time.Time{}
	for time.Now().Before(deadline) {
		srv.residentDrainMu.Lock()
		busy := len(srv.drainingResidentAgent) > 0
		srv.residentDrainMu.Unlock()
		now := time.Now()
		if busy {
			idleSince = time.Time{}
		} else if idleSince.IsZero() {
			idleSince = now
		} else if now.Sub(idleSince) >= 150*time.Millisecond {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func startFocusDMThread(t *testing.T, srv *Server, participantID string) Thread {
	t.Helper()
	raw := fmt.Sprintf(`{"id":"dm-start","method":"thread/start","params":{"dm_participant_id":%q}}`, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "dm-start")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/start returned error: %v", errMsg)
	}
	return remarshal[ThreadStartResult](t, resp["result"]).Thread
}

func startFocusTurn(t *testing.T, srv *Server, reqID, threadID, prompt string, focus *string) map[string]any {
	t.Helper()
	params := map[string]any{"thread_id": threadID, "prompt": prompt}
	if focus != nil {
		params["focus_workspace"] = *focus
	}
	payload, err := json.Marshal(map[string]any{"id": reqID, "method": "turn/start", "params": params})
	if err != nil {
		t.Fatalf("marshal turn/start: %v", err)
	}
	if err := srv.handleLine(context.Background(), payload); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	return responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), reqID)
}

func focusDeclarationRecords(t *testing.T, sessDir, threadID string) []session.HistoryRecord {
	t.Helper()
	records, err := session.LoadHistoryRecords(sessDir, threadID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	var out []session.HistoryRecord
	for _, rec := range records {
		if len(rec.FocusMeta) > 0 {
			out = append(out, rec)
		}
	}
	return out
}

func strPtr(s string) *string { return &s }

func TestDMTurnFocusFirstSetInjectsDeclaration(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{{Content: "ok"}}}
	srv, out, _, workspaceRoot := newFocusTestServer(t, client)
	participantID := saveNamedParticipant(t, srv.rt, "Hana", "general-purpose", "")
	thread := startFocusDMThread(t, srv, participantID)

	resp := startFocusTurn(t, srv, "turn-1", thread.ID, "hello", strPtr("acme"))
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("turn/start returned error: %v", errMsg)
	}
	waitForTurnCompletedCountForThread(t, out, thread.ID, 1)

	// The declaration is persisted right before this turn's user message.
	records, err := session.LoadHistoryRecords(srv.rt.SessionDir, thread.ID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	declIdx, userIdx := -1, -1
	for i, rec := range records {
		if len(rec.FocusMeta) > 0 && declIdx == -1 {
			declIdx = i
		}
		if rec.Role == "user" && rec.Content == "hello" {
			userIdx = i
		}
	}
	if declIdx == -1 {
		t.Fatalf("no focus declaration persisted; records=%+v", records)
	}
	if userIdx == -1 || declIdx > userIdx {
		t.Fatalf("declaration (idx %d) must precede the user message (idx %d)", declIdx, userIdx)
	}
	decl := records[declIdx]
	wantContent := fmt.Sprintf("[focus: acme — %s]", workspaceRoot)
	if decl.Role != "user" || decl.Content != wantContent {
		t.Fatalf("declaration = %q (role %q), want %q", decl.Content, decl.Role, wantContent)
	}
	var meta focusMeta
	if err := json.Unmarshal(decl.FocusMeta, &meta); err != nil {
		t.Fatalf("decode focus meta %s: %v", decl.FocusMeta, err)
	}
	if meta.Kind != focusMetaKindWorkspace || meta.Name != "acme" || meta.Root != workspaceRoot {
		t.Fatalf("focus meta = %+v, want workspace/acme/%s", meta, workspaceRoot)
	}

	// The new focus is persisted on the session.
	sess, ok, err := session.Find(srv.rt.SessionDir, thread.ID)
	if err != nil || !ok {
		t.Fatalf("session.Find: %v (ok=%v)", err, ok)
	}
	if sess.FocusWorkspace != "acme" {
		t.Fatalf("session focus = %q, want %q", sess.FocusWorkspace, "acme")
	}

	// The synthetic declaration turn reached the wire with focus_meta on
	// its user item so the chat view can render the divider.
	foundWireDecl := false
	for _, msg := range parseOutput(t, out.String()) {
		if msg["method"] != NotificationTurnStarted || msg["id"] != nil {
			continue
		}
		params := remarshal[TurnStartedNotification](t, msg["params"])
		if params.ThreadID != thread.ID || len(params.Turn.Items) == 0 {
			continue
		}
		item := params.Turn.Items[0]
		if len(item.FocusMeta) > 0 {
			foundWireDecl = true
			if item.Type != ThreadItemUserMessage || item.Text != wantContent {
				t.Fatalf("wire declaration item = %+v, want user_message %q", item, wantContent)
			}
		}
	}
	if !foundWireDecl {
		t.Fatalf("no turn/started notification carried a focus_meta item; output:\n%s", out.String())
	}
}

func TestDMTurnFocusRepeatIsIdempotent(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{{Content: "one"}, {Content: "two"}, {Content: "three"}}}
	srv, out, _, _ := newFocusTestServer(t, client)
	participantID := saveNamedParticipant(t, srv.rt, "Bea", "general-purpose", "")
	thread := startFocusDMThread(t, srv, participantID)

	if resp := startFocusTurn(t, srv, "turn-1", thread.ID, "first", strPtr("acme")); resp["error"] != nil {
		t.Fatalf("first turn/start: %v", resp["error"])
	}
	waitForTurnCompletedCountForThread(t, out, thread.ID, 1)
	if got := len(focusDeclarationRecords(t, srv.rt.SessionDir, thread.ID)); got != 1 {
		t.Fatalf("declarations after first set = %d, want 1", got)
	}

	// Same focus again: the front-end may send it on every turn; nothing
	// new may enter history.
	if resp := startFocusTurn(t, srv, "turn-2", thread.ID, "second", strPtr("acme")); resp["error"] != nil {
		t.Fatalf("second turn/start: %v", resp["error"])
	}
	waitForTurnCompletedCountForThread(t, out, thread.ID, 2)
	if got := len(focusDeclarationRecords(t, srv.rt.SessionDir, thread.ID)); got != 1 {
		t.Fatalf("declarations after repeat = %d, want 1 (idempotent)", got)
	}

	// Omitting the field entirely also changes nothing.
	if resp := startFocusTurn(t, srv, "turn-3", thread.ID, "third", nil); resp["error"] != nil {
		t.Fatalf("third turn/start: %v", resp["error"])
	}
	waitForTurnCompletedCountForThread(t, out, thread.ID, 3)
	if got := len(focusDeclarationRecords(t, srv.rt.SessionDir, thread.ID)); got != 1 {
		t.Fatalf("declarations after omitted param = %d, want 1", got)
	}
	sess, _, err := session.Find(srv.rt.SessionDir, thread.ID)
	if err != nil {
		t.Fatalf("session.Find: %v", err)
	}
	if sess.FocusWorkspace != "acme" {
		t.Fatalf("focus after omitted param = %q, want %q", sess.FocusWorkspace, "acme")
	}
}

func TestDMTurnFocusSwitchInjectsNewDeclaration(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{{Content: "one"}, {Content: "two"}}}
	srv, out, _, _ := newFocusTestServer(t, client)
	participantID := saveNamedParticipant(t, srv.rt, "Cleo", "general-purpose", "")
	thread := startFocusDMThread(t, srv, participantID)

	if resp := startFocusTurn(t, srv, "turn-1", thread.ID, "first", strPtr("acme")); resp["error"] != nil {
		t.Fatalf("first turn/start: %v", resp["error"])
	}
	waitForTurnCompletedCountForThread(t, out, thread.ID, 1)

	if resp := startFocusTurn(t, srv, "turn-2", thread.ID, "second", strPtr("~")); resp["error"] != nil {
		t.Fatalf("second turn/start: %v", resp["error"])
	}
	waitForTurnCompletedCountForThread(t, out, thread.ID, 2)

	decls := focusDeclarationRecords(t, srv.rt.SessionDir, thread.ID)
	if len(decls) != 2 {
		t.Fatalf("declarations after switch = %d, want 2", len(decls))
	}
	if decls[1].Content != "[focus: home directory only]" {
		t.Fatalf("home declaration = %q, want %q", decls[1].Content, "[focus: home directory only]")
	}
	var meta focusMeta
	if err := json.Unmarshal(decls[1].FocusMeta, &meta); err != nil {
		t.Fatalf("decode focus meta: %v", err)
	}
	if meta.Kind != focusMetaKindHome || meta.Root != thread.CWD {
		t.Fatalf("home focus meta = %+v, want kind=home root=%s", meta, thread.CWD)
	}
	sess, _, err := session.Find(srv.rt.SessionDir, thread.ID)
	if err != nil {
		t.Fatalf("session.Find: %v", err)
	}
	if sess.FocusWorkspace != "~" {
		t.Fatalf("focus after switch = %q, want %q", sess.FocusWorkspace, "~")
	}
}

func TestDMTurnFocusUnknownWorkspaceErrors(t *testing.T) {
	client := &fakeClient{}
	srv, _, _, _ := newFocusTestServer(t, client)
	participantID := saveNamedParticipant(t, srv.rt, "Dana", "general-purpose", "")
	thread := startFocusDMThread(t, srv, participantID)

	resp := startFocusTurn(t, srv, "turn-1", thread.ID, "hello", strPtr("no-such-workspace"))
	errMsg, ok := resp["error"]
	if !ok {
		t.Fatalf("expected error for unknown workspace, got %+v", resp)
	}
	if errStr := fmt.Sprint(errMsg); !strings.Contains(errStr, "no-such-workspace") {
		t.Fatalf("error should name the rejected workspace, got %q", errStr)
	}

	// Nothing was persisted: no declaration, focus unchanged.
	if got := len(focusDeclarationRecords(t, srv.rt.SessionDir, thread.ID)); got != 0 {
		t.Fatalf("declarations after rejected focus = %d, want 0", got)
	}
	sess, _, err := session.Find(srv.rt.SessionDir, thread.ID)
	if err != nil {
		t.Fatalf("session.Find: %v", err)
	}
	if sess.FocusWorkspace != "" {
		t.Fatalf("focus after rejected switch = %q, want empty", sess.FocusWorkspace)
	}
}

func TestDMTurnFocusHomeAndAllSemantics(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{{Content: "one"}, {Content: "two"}}}
	srv, out, _, _ := newFocusTestServer(t, client)
	participantID := saveNamedParticipant(t, srv.rt, "Eli", "general-purpose", "")
	thread := startFocusDMThread(t, srv, participantID)

	// "~" pins to the agent home.
	if resp := startFocusTurn(t, srv, "turn-1", thread.ID, "first", strPtr("~")); resp["error"] != nil {
		t.Fatalf("home turn/start: %v", resp["error"])
	}
	waitForTurnCompletedCountForThread(t, out, thread.ID, 1)
	sess, _, err := session.Find(srv.rt.SessionDir, thread.ID)
	if err != nil {
		t.Fatalf("session.Find: %v", err)
	}
	if sess.FocusWorkspace != "~" {
		t.Fatalf("focus = %q, want %q", sess.FocusWorkspace, "~")
	}

	// An explicit empty string switches back to all workspaces — distinct
	// from omitting the field (which means "no change").
	if resp := startFocusTurn(t, srv, "turn-2", thread.ID, "second", strPtr("")); resp["error"] != nil {
		t.Fatalf("all turn/start: %v", resp["error"])
	}
	waitForTurnCompletedCountForThread(t, out, thread.ID, 2)
	sess, _, err = session.Find(srv.rt.SessionDir, thread.ID)
	if err != nil {
		t.Fatalf("session.Find: %v", err)
	}
	if sess.FocusWorkspace != "" {
		t.Fatalf("focus after explicit empty = %q, want empty", sess.FocusWorkspace)
	}

	decls := focusDeclarationRecords(t, srv.rt.SessionDir, thread.ID)
	if len(decls) != 2 {
		t.Fatalf("declarations = %d, want 2", len(decls))
	}
	if decls[0].Content != "[focus: home directory only]" {
		t.Fatalf("first declaration = %q", decls[0].Content)
	}
	if decls[1].Content != "[focus: all registered workspaces]" {
		t.Fatalf("second declaration = %q", decls[1].Content)
	}
	var meta focusMeta
	if err := json.Unmarshal(decls[1].FocusMeta, &meta); err != nil {
		t.Fatalf("decode focus meta: %v", err)
	}
	if meta.Kind != focusMetaKindAll || meta.Name != "" || meta.Root != "" {
		t.Fatalf("all focus meta = %+v, want bare kind=all", meta)
	}
}

func TestThreadWireCarriesFocusWorkspace(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{{Content: "ok"}}}
	srv, out, rt, _ := newFocusTestServer(t, client)
	participantID := saveNamedParticipant(t, srv.rt, "Fran", "general-purpose", "")
	thread := startFocusDMThread(t, srv, participantID)

	if resp := startFocusTurn(t, srv, "turn-1", thread.ID, "hello", strPtr("acme")); resp["error"] != nil {
		t.Fatalf("turn/start: %v", resp["error"])
	}
	waitForTurnCompletedCountForThread(t, out, thread.ID, 1)

	// Live server: thread/list surfaces the focus.
	if err := srv.handleLine(context.Background(), []byte(`{"id":"list","method":"thread/list"}`)); err != nil {
		t.Fatalf("thread/list: %v", err)
	}
	list := remarshal[ThreadListResult](t, responseByID(t, parseOutput(t, out.String()), "list")["result"])
	found := false
	for _, th := range list.Threads {
		if th.ID == thread.ID {
			found = true
			if th.FocusWorkspace != "acme" {
				t.Fatalf("thread/list focus = %q, want %q", th.FocusWorkspace, "acme")
			}
		}
	}
	if !found {
		t.Fatalf("thread/list did not include DM thread %q", thread.ID)
	}

	// Fresh server (simulated restart): thread/resume reloads the focus
	// from session metadata and the declaration item from history.
	rt2 := newTestRuntime(t, &fakeClient{})
	rt2.WuuHome = rt.WuuHome
	rt2.SessionDir = rt.SessionDir
	srv2 := New(rt2, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv2) })
	raw := fmt.Sprintf(`{"id":"resume","method":"thread/resume","params":{"session_id":%q}}`, thread.ID)
	if err := srv2.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}
	resumeResp := responseByID(t, parseOutput(t, srv2.out.(*lockedBuffer).String()), "resume")
	if errMsg, ok := resumeResp["error"]; ok {
		t.Fatalf("thread/resume returned error: %v", errMsg)
	}
	resumed := remarshal[ThreadResumeResult](t, resumeResp["result"]).Thread
	if resumed.FocusWorkspace != "acme" {
		t.Fatalf("resumed focus = %q, want %q", resumed.FocusWorkspace, "acme")
	}
	foundDecl := false
	for _, turn := range resumed.Turns {
		for _, item := range turn.Items {
			if len(item.FocusMeta) > 0 {
				foundDecl = true
				if item.Type != ThreadItemUserMessage || !strings.HasPrefix(item.Text, "[focus: acme") {
					t.Fatalf("resumed declaration item = %+v", item)
				}
			}
		}
	}
	if !foundDecl {
		t.Fatalf("resumed thread lost the focus declaration item; turns=%+v", resumed.Turns)
	}
}

// --- Group thread focus declarations (2026-07-03-workspace-focus.md §6) ---
//
// Group threads reuse applyTurnWorkspaceFocus unchanged (it is deliberately
// thread-kind agnostic); handleGroupTurnStart just calls it before recording
// the caller's own message. These tests mirror the DM focus tests above but
// exercise the group turn/start branch, which never invokes the provider.

func TestGroupTurnFocusFirstSetInjectsDeclaration(t *testing.T) {
	srv, out, _, workspaceRoot := newFocusTestServer(t, &fakeClient{})
	group := startNamedGroupThreadForTest(t, srv, "launch-planning")

	resp := startFocusTurn(t, srv, "turn-1", group.ID, "hello team", strPtr("acme"))
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("turn/start returned error: %v", errMsg)
	}
	turn := remarshal[TurnStartResult](t, resp["result"]).Turn
	if turn.Status != TurnStatusCompleted {
		t.Fatalf("expected group turn to complete synchronously, got %q", turn.Status)
	}
	if len(turn.Items) != 1 || turn.Items[0].Type != ThreadItemUserMessage || turn.Items[0].Text != "hello team" {
		t.Fatalf("expected exactly the user message item on the response turn, got %+v", turn.Items)
	}

	// The declaration is persisted right before this turn's user message, as
	// a separate synthetic turn (same shape as the DM path).
	records, err := session.LoadHistoryRecords(srv.rt.SessionDir, group.ID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	declIdx, userIdx := -1, -1
	for i, rec := range records {
		if len(rec.FocusMeta) > 0 && declIdx == -1 {
			declIdx = i
		}
		if rec.Role == "user" && rec.Content == "hello team" {
			userIdx = i
		}
	}
	if declIdx == -1 {
		t.Fatalf("no focus declaration persisted; records=%+v", records)
	}
	if userIdx == -1 || declIdx > userIdx {
		t.Fatalf("declaration (idx %d) must precede the user message (idx %d)", declIdx, userIdx)
	}
	decl := records[declIdx]
	wantContent := fmt.Sprintf("[focus: acme — %s]", workspaceRoot)
	if decl.Role != "user" || decl.Content != wantContent {
		t.Fatalf("declaration = %q (role %q), want %q", decl.Content, decl.Role, wantContent)
	}
	var meta focusMeta
	if err := json.Unmarshal(decl.FocusMeta, &meta); err != nil {
		t.Fatalf("decode focus meta %s: %v", decl.FocusMeta, err)
	}
	if meta.Kind != focusMetaKindWorkspace || meta.Name != "acme" || meta.Root != workspaceRoot {
		t.Fatalf("focus meta = %+v, want workspace/acme/%s", meta, workspaceRoot)
	}

	sess, ok, err := session.Find(srv.rt.SessionDir, group.ID)
	if err != nil || !ok {
		t.Fatalf("session.Find: %v (ok=%v)", err, ok)
	}
	if sess.FocusWorkspace != "acme" {
		t.Fatalf("session focus = %q, want %q", sess.FocusWorkspace, "acme")
	}

	// The synthetic declaration turn reached the wire with focus_meta on its
	// user item, same as the DM path, so the chat view can render a divider
	// for every member reading the group transcript.
	foundWireDecl := false
	for _, msg := range parseOutput(t, out.String()) {
		if msg["method"] != NotificationTurnStarted || msg["id"] != nil {
			continue
		}
		params := remarshal[TurnStartedNotification](t, msg["params"])
		if params.ThreadID != group.ID || len(params.Turn.Items) == 0 {
			continue
		}
		item := params.Turn.Items[0]
		if len(item.FocusMeta) > 0 {
			foundWireDecl = true
			if item.Type != ThreadItemUserMessage || item.Text != wantContent {
				t.Fatalf("wire declaration item = %+v, want user_message %q", item, wantContent)
			}
		}
	}
	if !foundWireDecl {
		t.Fatalf("no turn/started notification carried a focus_meta item; output:\n%s", out.String())
	}
}

func TestGroupTurnFocusRepeatIsIdempotent(t *testing.T) {
	srv, _, _, _ := newFocusTestServer(t, &fakeClient{})
	group := startNamedGroupThreadForTest(t, srv, "release")

	if resp := startFocusTurn(t, srv, "turn-1", group.ID, "first", strPtr("acme")); resp["error"] != nil {
		t.Fatalf("first turn/start: %v", resp["error"])
	}
	if got := len(focusDeclarationRecords(t, srv.rt.SessionDir, group.ID)); got != 1 {
		t.Fatalf("declarations after first set = %d, want 1", got)
	}

	// Same focus again: nothing new may enter history.
	if resp := startFocusTurn(t, srv, "turn-2", group.ID, "second", strPtr("acme")); resp["error"] != nil {
		t.Fatalf("second turn/start: %v", resp["error"])
	}
	if got := len(focusDeclarationRecords(t, srv.rt.SessionDir, group.ID)); got != 1 {
		t.Fatalf("declarations after repeat = %d, want 1 (idempotent)", got)
	}

	// Omitting the field entirely also changes nothing.
	if resp := startFocusTurn(t, srv, "turn-3", group.ID, "third", nil); resp["error"] != nil {
		t.Fatalf("third turn/start: %v", resp["error"])
	}
	if got := len(focusDeclarationRecords(t, srv.rt.SessionDir, group.ID)); got != 1 {
		t.Fatalf("declarations after omitted param = %d, want 1", got)
	}
	sess, _, err := session.Find(srv.rt.SessionDir, group.ID)
	if err != nil {
		t.Fatalf("session.Find: %v", err)
	}
	if sess.FocusWorkspace != "acme" {
		t.Fatalf("focus after omitted param = %q, want %q", sess.FocusWorkspace, "acme")
	}
}

func TestGroupTurnFocusSwitchInjectsNewDeclaration(t *testing.T) {
	srv, _, _, _ := newFocusTestServer(t, &fakeClient{})
	group := startNamedGroupThreadForTest(t, srv, "release")

	if resp := startFocusTurn(t, srv, "turn-1", group.ID, "first", strPtr("acme")); resp["error"] != nil {
		t.Fatalf("first turn/start: %v", resp["error"])
	}
	if resp := startFocusTurn(t, srv, "turn-2", group.ID, "second", strPtr("~")); resp["error"] != nil {
		t.Fatalf("second turn/start: %v", resp["error"])
	}

	decls := focusDeclarationRecords(t, srv.rt.SessionDir, group.ID)
	if len(decls) != 2 {
		t.Fatalf("declarations after switch = %d, want 2", len(decls))
	}
	if decls[1].Content != "[focus: home directory only]" {
		t.Fatalf("home declaration = %q, want %q", decls[1].Content, "[focus: home directory only]")
	}
	var meta focusMeta
	if err := json.Unmarshal(decls[1].FocusMeta, &meta); err != nil {
		t.Fatalf("decode focus meta: %v", err)
	}
	if meta.Kind != focusMetaKindHome || meta.Root != group.CWD {
		t.Fatalf("home focus meta = %+v, want kind=home root=%s", meta, group.CWD)
	}
	sess, _, err := session.Find(srv.rt.SessionDir, group.ID)
	if err != nil {
		t.Fatalf("session.Find: %v", err)
	}
	if sess.FocusWorkspace != "~" {
		t.Fatalf("focus after switch = %q, want %q", sess.FocusWorkspace, "~")
	}
}

func TestGroupTurnFocusUnknownWorkspaceErrors(t *testing.T) {
	srv, _, _, _ := newFocusTestServer(t, &fakeClient{})
	group := startNamedGroupThreadForTest(t, srv, "release")

	resp := startFocusTurn(t, srv, "turn-1", group.ID, "hello", strPtr("no-such-workspace"))
	errMsg, ok := resp["error"]
	if !ok {
		t.Fatalf("expected error for unknown workspace, got %+v", resp)
	}
	if errStr := fmt.Sprint(errMsg); !strings.Contains(errStr, "no-such-workspace") {
		t.Fatalf("error should name the rejected workspace, got %q", errStr)
	}

	// Nothing was persisted: no declaration, focus unchanged, and the
	// rejected turn's own user message never landed in history either.
	if got := len(focusDeclarationRecords(t, srv.rt.SessionDir, group.ID)); got != 0 {
		t.Fatalf("declarations after rejected focus = %d, want 0", got)
	}
	sess, _, err := session.Find(srv.rt.SessionDir, group.ID)
	if err != nil {
		t.Fatalf("session.Find: %v", err)
	}
	if sess.FocusWorkspace != "" {
		t.Fatalf("focus after rejected switch = %q, want empty", sess.FocusWorkspace)
	}
	records, err := session.LoadHistoryRecords(srv.rt.SessionDir, group.ID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	for _, rec := range records {
		if rec.Content == "hello" {
			t.Fatalf("rejected focus turn must not persist the user message either: %+v", records)
		}
	}
}

func TestDMTurnFocusRootsToolkitAtWorkspace(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{{Content: "one"}, {Content: "two"}}}
	srv, out, rt, workspaceRoot := newFocusTestServer(t, client)
	kit, err := tools.New(rt.RootDir)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	rt.Toolkit = kit
	participantID := saveNamedParticipant(t, srv.rt, "Gabe", "general-purpose", "")
	thread := startFocusDMThread(t, srv, participantID)

	if resp := startFocusTurn(t, srv, "turn-1", thread.ID, "hello", strPtr("acme")); resp["error"] != nil {
		t.Fatalf("turn/start: %v", resp["error"])
	}
	waitForTurnCompletedCountForThread(t, out, thread.ID, 1)

	dmKit := residentToolkitForTest(t, srv, thread.ID)
	wantRoot := workspaceRoot
	if resolved, err := filepath.EvalSymlinks(workspaceRoot); err == nil {
		wantRoot = resolved
	}
	if got := dmKit.RootDir(); got != wantRoot {
		t.Fatalf("toolkit root after workspace focus = %q, want %q", got, wantRoot)
	}

	// Switching back to home restores the agent-home root.
	if resp := startFocusTurn(t, srv, "turn-2", thread.ID, "back", strPtr("~")); resp["error"] != nil {
		t.Fatalf("turn/start (~): %v", resp["error"])
	}
	waitForTurnCompletedCountForThread(t, out, thread.ID, 2)
	dmKit = residentToolkitForTest(t, srv, thread.ID)
	wantHome := thread.CWD
	if resolved, err := filepath.EvalSymlinks(thread.CWD); err == nil {
		wantHome = resolved
	}
	if got := dmKit.RootDir(); got != wantHome {
		t.Fatalf("toolkit root after home focus = %q, want %q", got, wantHome)
	}
}
