package appserver

import (
	"fmt"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

// Issue #3: resident inbox auto-replay on restart.
//
// Symptom: when the app shuts down while envelopes are pending in a
// resident's inbox (e.g. an @-mention arrived while the agent was busy
// and the user closed the laptop), restart leaves those envelopes
// sitting in resident_inbox until the NEXT fresh message happens to
// land in the same group — which is unreliable enough to be "forever"
// for low-traffic participants. The user-visible symptom is "the model
// didn't respond after I restarted"; the technical symptom is that the
// pending envelopes are never re-fed to the resident.
//
// This test asserts the FIX END-TO-END, not just the storage-side drain.
// "Drain" alone would be satisfiable by a malicious implementation that
// just marks pending envelopes consumed without ever feeding them to a
// model turn. The user's spec — 「积压消息会被自动补投」 — is drain + replay.
// Two assertions enforce both:
//
//	A. Inbox drains to 0 (the kickResidentAgent → drainResidentAgent path
//	   started; the stored consumed_at markers were written).
//	B. Resident DM history contains at least one of the previously pending
//	   envelope texts (the drain turn rendered the envelopes into the
//	   resident's `<incoming_message>` block, so the model saw them and
//	   could react).
//
// Test fixture:
//  1. Build a runtime with a temp session dir and save a named
//     participant.
//  2. Inject N envelopes directly into the resident_inbox table
//     (bypassing routeUserMessageToResidents) — simulating "envelopes
//     arrived while the app was off, sitting in persistence", i.e. the
//     user's actual scenario: a previous process held envelopes but
//     didn't get to process them.
//  3. Construct a Server via New(rt, out) on the same rt — the
//     "restart": same SessionDir, fresh Server,
//     drainPendingResidentEnvelopesOnBoot() fires on boot entry.
//  4. Without any new group message arriving, the inbox must drain to 0
//     AND the resident DM history must contain one of the injected
//     envelope texts.
//
// N=5 stays well under residentEnvelopeBatchLimit=20 so a single drain
// pass consumes the whole batch — the assertions only need "0 pending"
// + "DM history contains pending message 0" within the polling window,
// not multi-batch behavior.
//
// The test FAILS (red) before the fix because New() never iterates
// pending inbox participants; PASSES (green) once
// drainPendingResidentEnvelopesOnBoot is wired in. Note that assertion B
// in particular would catch any "drain but don't replay" implementation,
// e.g. one that just bulk-stamps consumed_at on boot without running a
// model turn.
func TestResidentInboxAutoReplayedOnRestart(t *testing.T) {
	// Plenty of dummy responses for the drain turn: one batch with N=5
	// triggers one model call, but the helper also re-kicks on edge
	// errors so we keep a few in reserve.
	client := &fakeClient{responses: []providers.ChatResponse{
		{Content: "ok"}, {Content: "ok"}, {Content: "ok"}, {Content: "ok"},
	}}
	rt := newTestRuntime(t, client)
	rt.WuuHome = t.TempDir()

	participantID := saveNamedParticipant(t, rt, "Issue3RestartAlice", "general-purpose", "")

	const N = 5
	for i := 0; i < N; i++ {
		env := MessageEnvelope{
			SourceThreadID: "fake-group",
			SourceTitle:    "fake-group",
			SenderKind:     "user",
			SenderName:     "User",
			Addressed:      true,
			Hop:            1,
			Text:           fmt.Sprintf("pending message %d", i),
		}
		enqueueTestEnvelope(t, rt.SessionDir, participantID, env)
	}

	// Sanity: confirm exactly N envelopes are pending before "restart".
	pending, err := session.PendingResidentEnvelopes(rt.SessionDir, participantID, 0)
	if err != nil {
		t.Fatalf("PendingResidentEnvelopes setup: %v", err)
	}
	if len(pending) != N {
		t.Fatalf("setup: expected %d pending envelopes, got %d", N, len(pending))
	}

	// Simulate "app restart" by constructing a fresh Server with the same
	// rt (same SessionDir = same SQLite store). Without the fix, New()
	// simply constructs the Server and returns; the inbox stays at N and
	// the test fails with the clear "issue #3 fix missing" message.
	out := &lockedBuffer{}
	srv := New(rt, out)
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })

	// Assertion A — drain: each envelope becomes consumed_at != NULL
	// after the model returns from the drain turn + afterResidentTurn +
	// recordEnvelopeReadReceipts. The polling loop catches the case
	// where the inbox merely flips over time without a model turn
	// (e.g. a future caller that bulk-stamps consumed_at on boot).
	drainDeadline := time.Now().Add(5 * time.Second)
	var lastPending []session.ResidentEnvelope
	for time.Now().Before(drainDeadline) {
		p, err := session.PendingResidentEnvelopes(rt.SessionDir, participantID, 0)
		if err != nil {
			t.Fatalf("PendingResidentEnvelopes: %v", err)
		}
		if len(p) == 0 {
			lastPending = nil
			break
		}
		lastPending = p
		time.Sleep(50 * time.Millisecond)
	}
	if lastPending != nil {
		t.Fatalf("inbox still has %d pending envelopes after Server.New() (expected 0): issue #3 drain fix missing or incomplete", len(lastPending))
	}

	// Assertion B — replay: the drain turn must have rendered at least
	// one of the envelopes into the resident's DM history as a
	// `<incoming_message>` block (the model actually saw the content,
	// not just had it marked consumed). waitForResidentDMHistoryContains
	// polls the per-resident DM history until a record contains the
	// substring; it fails the test on timeout, which is exactly the
	// "drained but never replayed" failure mode.
	waitForResidentDMHistoryContains(t, srv, participantID, "pending message 0")
}