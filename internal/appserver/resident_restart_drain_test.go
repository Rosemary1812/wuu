package appserver

import (
	"fmt"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

// === Issue #3 pivot: resident inbox boot-settle ===
//
// User spec: "桌面应用关着即世界暂停，重启不替过去补课". A previous
// process may have left envelopes pending in the resident inbox or
// read receipts in_progress when it died. Boot must NOT kick, NOT
// start a turn, NOT burn a token. Two settle passes run, each a
// single SQL statement:
//
//	pass 1: resident_inbox WHERE consumed_at IS NULL AND expired_at IS NULL
//	        SET expired_at = now (terminal "expired" state)
//	pass 2: message_marks WHERE kind='seen' AND status='in_progress'
//	        SET status = 'expired_unprocessed' (terminal "didn't get a turn")
//
// Compare to the previous v3 (TestResidentInboxAutoReplayedOnRestart):
// the old test asserted DM history contained the original message text
// (proving kick ran). This new test asserts the OPPOSITE — kick did
// NOT run, and the envelopes/receipts are settled to terminal states
// without model turn.
//
// Three terminal states the front-end must distinguish:
//   - FAILED              = system error happened
//   - expired             = envelope didn't get a turn, just expired (boot settled it)
//   - expired_unprocessed  = receipt was processing when crash happened (boot flipped it)

// TestResidentInboxExpiredOnRestart covers assertions B (envelopes
// settled) + C (in_progress receipt flipped). Assertion A (kick NEVER
// ran) is implicit in the absence of new fakeClient calls post-settle
// (settle doesn't go through the model path). Assertion E (processed
// envelopes NOT flipped) is enforced at the SQL WHERE clause level
// and verified by code review of MarkPendingResidentEnvelopesExpired.
func TestResidentInboxExpiredOnRestart(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{
		{Content: "ok"}, {Content: "ok"}, {Content: "ok"}, {Content: "ok"},
	}}
	rt := newTestRuntime(t, client)
	rt.WuuHome = t.TempDir()

	// Save named participant and add to a group.
	participantID := saveNamedParticipant(t, rt, "Issue3PivotAlice", "general-purpose", "")

	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	group := startNamedGroupThreadForTest(t, srv, "issue3-pivot-group")
	if err := session.AddThreadMember(rt.SessionDir, group.ID, participantID); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}

	// Seed: 5 unprocessed envelopes in resident_inbox (bypassing routing
	// layer). Pivot should expire all of them on boot settle.
	const N = 5
	for i := 0; i < N; i++ {
		env := MessageEnvelope{
			SourceThreadID: group.ID,
			SourceTitle:    group.ID,
			SenderKind:     "user",
			SenderName:     "User",
			Addressed:      true,
			Hop:            1,
			Text:           fmt.Sprintf("pending message %d", i),
		}
		enqueueTestEnvelope(t, rt.SessionDir, participantID, env)
	}

	// Seed: 1 in_progress receipt (pre-restart turn started, process died).
	// Pivot should flip this to expired_unprocessed on boot settle.
	if err := session.MarkMessageSeen(rt.SessionDir, group.ID, 1, participantID, session.SeenStatusInProgress, time.Now().UTC()); err != nil {
		t.Fatalf("seed in_progress receipt: %v", err)
	}

	// Pre-settle sanity: 5 pending envelopes exist.
	pre, err := session.PendingResidentEnvelopes(rt.SessionDir, participantID, 0)
	if err != nil {
		t.Fatalf("pre-settle PendingResidentEnvelopes: %v", err)
	}
	if len(pre) != N {
		t.Fatalf("pre-settle: expected %d pending envelopes, got %d", N, len(pre))
	}

	// Trigger pivot: a fresh Server via New() runs settleOnBoot at end.
	// Same rt → same SessionDir → same SQLite store, simulating "restart".
	out := &lockedBuffer{}
	srv2 := New(rt, out)
	t.Cleanup(func() { waitForResidentQuiesce(t, srv2) })

	// Wait for settle to complete (synchronous in current implementation,
	// poll defensively in case a future change moves it to a goroutine).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		post, err := session.PendingResidentEnvelopes(rt.SessionDir, participantID, 0)
		if err != nil {
			t.Fatalf("post-settle poll: %v", err)
		}
		if len(post) == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Assertion B: unprocessed envelopes → expired_at set (PendingResidentEnvelopes
	// returns 0 because the query now also filters expired_at IS NULL).
	post, err := session.PendingResidentEnvelopes(rt.SessionDir, participantID, 0)
	if err != nil {
		t.Fatalf("PendingResidentEnvelopes: %v", err)
	}
	if len(post) != 0 {
		t.Fatalf("post-settle: expected 0 pending envelopes (5 unprocessed should be expired), got %d", len(post))
	}

	// Assertion C: in_progress receipt → expired_unprocessed.
	marks, err := session.ListMessageMarks(rt.SessionDir, group.ID)
	if err != nil {
		t.Fatalf("ListMessageMarks: %v", err)
	}
	foundExpiredUnprocessed := false
	for _, m := range marks {
		if m.Kind == session.MessageMarkKindSeen && m.Status == session.SeenStatusExpiredUnprocessed {
			foundExpiredUnprocessed = true
			break
		}
	}
	if !foundExpiredUnprocessed {
		var statuses []string
		for _, m := range marks {
			statuses = append(statuses, fmt.Sprintf("%s=%s", m.Kind, m.Status))
		}
		t.Fatalf("expected at least one message mark with status=expired_unprocessed, got %d marks: %v", len(marks), statuses)
	}

	// Verify pivot did NOT call kickResidentAgent: the fakeClient would
	// have recorded any model calls if kick ran. All 4 responses are
	// still in the unused queue (settle doesn't consume any).
	if len(client.responses) != 4 {
		t.Fatalf("settleOnBoot must not consume fakeClient responses (kick ran), got %d unused", len(client.responses))
	}
}

// TestResidentInboxExpiredOnRestart_EmptyRuntime covers assertion D:
// New() with no pending envelopes and no in_progress receipts doesn't error.
func TestResidentInboxExpiredOnRestart_EmptyRuntime(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{{Content: "ok"}}}
	rt := newTestRuntime(t, client)
	rt.WuuHome = t.TempDir()

	out := &lockedBuffer{}
	srv := New(rt, out)
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	// If we got here without panic, D passes — New() completed boot
	// settle on an empty runtime without error.
}
