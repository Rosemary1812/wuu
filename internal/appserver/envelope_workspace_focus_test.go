package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// enqueueTestEnvelope enqueues a hand-built MessageEnvelope straight into a
// participant's resident inbox, bypassing routeEnvelopes entirely. Tests use
// this to control exactly which Workspace values land in one drain batch
// (2026-07-03-workspace-focus.md "carry source-thread workspace focus on
// envelopes" §"drainResidentAgent 起 turn 前的 cwd").
func enqueueTestEnvelope(t *testing.T, sessDir, participantID string, env MessageEnvelope) {
	t.Helper()
	if strings.TrimSpace(env.ID) == "" {
		env.ID = "env-" + session.NewID()
	}
	if env.CreatedAt.IsZero() {
		env.CreatedAt = time.Now().UTC()
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if _, err := session.EnqueueResidentEnvelope(sessDir, session.ResidentEnvelope{
		ID:            env.ID,
		ParticipantID: participantID,
		EnvelopeJSON:  data,
		CreatedAt:     env.CreatedAt,
	}); err != nil {
		t.Fatalf("EnqueueResidentEnvelope: %v", err)
	}
}

// waitForResidentDMHistoryContains polls a resident's DM history until some
// record's raw (model-visible) Content contains substr, or fails the test.
func waitForResidentDMHistoryContains(t *testing.T, srv *Server, participantID, substr string) []session.HistoryRecord {
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
			for _, rec := range history {
				if strings.Contains(rec.Content, substr) {
					return history
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for resident DM history containing %q", substr)
	return nil
}

// TestGroupSourceFocusPropagatesIntoRoutedEnvelope covers the routing half
// of "carry source-thread workspace focus on envelopes": a group thread's
// declared focus rides along on every envelope routeUserMessageToResidents
// builds from it, landing in the resident's raw (model-visible) history
// content as the <incoming_message workspace="..."> attribute.
func TestGroupSourceFocusPropagatesIntoRoutedEnvelope(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{{Content: "ok"}, {Content: "ok"}}}
	srv, _, _, _ := newFocusTestServer(t, client)
	participantID := saveNamedParticipant(t, srv.rt, "Priya", "general-purpose", "")
	group := startNamedGroupThreadForTest(t, srv, "release")
	if err := session.AddThreadMember(srv.rt.SessionDir, group.ID, participantID); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}

	// Declare the group's focus before the addressed message that should
	// carry it into the routed envelope.
	if resp := startFocusTurn(t, srv, "focus", group.ID, "setting focus", strPtr("acme")); resp["error"] != nil {
		t.Fatalf("focus turn/start: %v", resp["error"])
	}

	raw := fmt.Sprintf(`{"id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"ship it, @Priya","mentions":[%q]}}`, group.ID, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	history := waitForResidentDMHistoryContains(t, srv, participantID, "ship it")
	found := false
	for _, rec := range history {
		if rec.Role == "user" && strings.Contains(rec.Content, "ship it") {
			found = true
			if !strings.Contains(rec.Content, `workspace="acme"`) {
				t.Fatalf("routed envelope missing workspace attribute: %q", rec.Content)
			}
		}
	}
	if !found {
		t.Fatalf("expected routed envelope containing %q in resident history; history=%+v", "ship it", history)
	}
}

// residentTurnRootCapture records the resident thread's toolkit root at the
// moment the provider is called, from a fakeClient.onChat hook — this is the
// only point mid-turn where we can safely observe the in-flight cwd without
// racing the turn's own goroutine (once the turn completes,
// ensureThreadRuntime reapplies the thread's own persisted focus, which is
// deliberately unaffected by the envelope batch, masking the override we
// want to observe). t.Fatal must not be called from onChat: it runs on the
// turn's background goroutine, not the test goroutine, so failures are
// recorded into the capture instead.
type residentTurnRootCapture struct {
	mu   sync.Mutex
	root string
	ok   bool
}

func (c *residentTurnRootCapture) hook(srv *Server, threadID string) func(int, providers.ChatRequest) {
	return func(int, providers.ChatRequest) {
		th := srv.thread(threadID)
		if th == nil {
			return
		}
		th.mu.Lock()
		rt := th.execRuntime
		th.mu.Unlock()
		if rt == nil || rt.Toolkit == nil {
			return
		}
		c.mu.Lock()
		c.root = rt.Toolkit.RootDir()
		c.ok = true
		c.mu.Unlock()
	}
}

func (c *residentTurnRootCapture) result(t *testing.T) string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ok {
		t.Fatalf("provider was never called; no root captured")
	}
	return c.root
}

// TestDrainResidentAgentSingleSourceFocusRootsToolkit covers the cwd half of
// "carry source-thread workspace focus on envelopes": when every envelope in
// a drain batch names the same workspace, tools for that turn run rooted
// there.
func TestDrainResidentAgentSingleSourceFocusRootsToolkit(t *testing.T) {
	capture := &residentTurnRootCapture{}
	client := &fakeClient{responses: []providers.ChatResponse{{Content: "ok"}}}
	srv, out, rt, workspaceRoot := newFocusTestServer(t, client)
	kit, err := tools.New(rt.RootDir)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	rt.Toolkit = kit
	participantID := saveNamedParticipant(t, srv.rt, "Nia", "general-purpose", "")
	thread := startFocusDMThread(t, srv, participantID)
	client.onChat = capture.hook(srv, thread.ID)

	enqueueTestEnvelope(t, srv.rt.SessionDir, participantID, MessageEnvelope{
		SourceThreadID: "group-a",
		SourceTitle:    "release",
		SenderKind:     "user",
		SenderName:     "User",
		Workspace:      "acme",
		Text:           "ship it",
	})

	srv.drainResidentAgent(participantID)
	waitForTurnCompletedCountForThread(t, out, thread.ID, 1)

	wantRoot := workspaceRoot
	if resolved, err := filepath.EvalSymlinks(workspaceRoot); err == nil {
		wantRoot = resolved
	}
	if got := capture.result(t); got != wantRoot {
		t.Fatalf("toolkit root during single-source envelope focus = %q, want %q", got, wantRoot)
	}

	// This is scoped to the envelope-driven turn only: the resident's own
	// DM thread must not pick up a persisted focus, nor a declaration item
	// (those are the semantics of the user directly declaring focus in a
	// conversation, not of inbound routed messages).
	sess, _, err := session.Find(srv.rt.SessionDir, thread.ID)
	if err != nil {
		t.Fatalf("session.Find: %v", err)
	}
	if sess.FocusWorkspace != "" {
		t.Fatalf("resident DM's own focus polluted by envelope batch: %q", sess.FocusWorkspace)
	}
	if got := len(focusDeclarationRecords(t, srv.rt.SessionDir, thread.ID)); got != 0 {
		t.Fatalf("envelope-driven turn must not inject a focus declaration, got %d", got)
	}
}

// TestDrainResidentAgentAmbiguousFocusFallsBackToHome covers the fallback
// half: when a batch's envelopes disagree on workspace, the turn's cwd
// falls back to the resident's agent home rather than guessing.
func TestDrainResidentAgentAmbiguousFocusFallsBackToHome(t *testing.T) {
	capture := &residentTurnRootCapture{}
	client := &fakeClient{responses: []providers.ChatResponse{{Content: "ok"}}}
	srv, out, rt, _ := newFocusTestServer(t, client)
	kit, err := tools.New(rt.RootDir)
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	rt.Toolkit = kit
	participantID := saveNamedParticipant(t, srv.rt, "Omar", "general-purpose", "")
	thread := startFocusDMThread(t, srv, participantID)
	client.onChat = capture.hook(srv, thread.ID)

	enqueueTestEnvelope(t, srv.rt.SessionDir, participantID, MessageEnvelope{
		SourceThreadID: "group-a", SourceTitle: "a", SenderKind: "user", SenderName: "User",
		Workspace: "acme", Text: "hi from a",
	})
	enqueueTestEnvelope(t, srv.rt.SessionDir, participantID, MessageEnvelope{
		SourceThreadID: "group-b", SourceTitle: "b", SenderKind: "user", SenderName: "User",
		Workspace: "beta", Text: "hi from b",
	})

	srv.drainResidentAgent(participantID)
	waitForTurnCompletedCountForThread(t, out, thread.ID, 1)

	wantHome := thread.CWD
	if resolved, err := filepath.EvalSymlinks(thread.CWD); err == nil {
		wantHome = resolved
	}
	if got := capture.result(t); got != wantHome {
		t.Fatalf("ambiguous envelope batch focus should fall back to agent home, got %q want %q", got, wantHome)
	}
}
