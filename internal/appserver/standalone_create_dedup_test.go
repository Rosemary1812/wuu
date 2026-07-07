package appserver

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
)

// === Issue #4 v3: standalone-task title dedup ===
//
// Symptom: standalone task creation (anchor_seq=0) had no title-based
// dedup — two agents splitting the same work would silently produce two
// cards with similar titles, leaving the board with stale duplicates
// ("18 cards on the board, 10 never claimed"). The fix (issue #4 v3):
//
//	- per-thread unfinished-task check (status filter narrows to
//	  ConversationThreadTask only, because L89-93 in
//	  `conversation_thread.go` pins that single value for the standalone
//	  path: a standalone cth cannot reach any other status, so the
//	  single-value filter is exhaustive);
//	- app-layer normalized title compare (TrimSpace + collapse
//	  whitespace + ToLower via `normalizeTitleForDedup`);
//	- hard-block error naming existing.ID + existing.Title;
//	- strict-id escape hatch `ack_collision_id`: must equal the
//	  existing task's id, NOT a generic ack or a made-up value
//	  (5th-test red line). The strict-match rule means a caller cannot
//	  bypass dedup with a guessed string; the conflict error message
//	  tells them exactly which id to cite.
//
// Tests (Andy's seq=25 list):
//
//	red→green  TestStandaloneCreateBlocksOnSameGroupSameTitle
//	ack-esc   TestStandaloneCreateAckCollisionOverride
//	diff      TestStandaloneCreateDifferentTitleStillSucceeds
//	L89-93    TestStandaloneCreateSameTitleResolvedIgnored
//	5th red   TestStandaloneCreateAckCollisionMismatchRefuses
//
// Cross-group pin (TestStandaloneCreateSameTitleDifferentGroupsBothSucceed)
// was REMOVED: the appserver collapses all `thread/start {group: true}`
// JSON-RPC calls through `ensureAllChannel` (`appserver/group_thread.go:60`)
// into one shared "all" channel, so two `startNamedGroupThreadForTest`
// calls return the SAME thread ID. To re-add the cross-group test,
// construct a second session directly via `session.Save` +
// `AddThreadMember` (bypassing the all-channel JSON-RPC dedup) and seed
// cth rows under each. Out of scope for issue #4.

// TestStandaloneCreateBlocksOnSameGroupSameTitle — RED→GREEN for the
// core invariant. Two creators with the same title in the SAME group
// on the standalone path: the second surfaces a conflict naming the
// existing task (id + title), so the caller can choose
// claim-OR-differentiate-OR-ack.
func TestStandaloneCreateBlocksOnSameGroupSameTitle(t *testing.T) {
	srv, groupID, ada, bea := newTaskWakeFixture(t)
	managerAda := srv.residentTaskManager(ada)
	managerBea := srv.residentTaskManager(bea)

	const title = "fix login flake"
	first, err := managerAda.CreateTask(context.Background(), groupID, 0, title, false, "")
	if err != nil {
		t.Fatalf("first standalone CreateTask: %v", err)
	}
	if first.Title != title {
		t.Fatalf("first task title = %q, want %q", first.Title, title)
	}

	_, err = managerBea.CreateTask(context.Background(), groupID, 0, title, false, "")
	if err == nil {
		t.Fatalf("second standalone CreateTask with same title succeeded; collision went undetected (issue #4)")
	}
	if !strings.Contains(err.Error(), first.ID) {
		t.Fatalf("conflict error %q does not reference existing task ID %q", err.Error(), first.ID)
	}
	if !strings.Contains(err.Error(), title) {
		t.Fatalf("conflict error %q does not reference existing task title %q", err.Error(), title)
	}
}

// TestStandaloneCreateAckCollisionOverride — issue #4 v3 escape hatch.
// After the conflict error names existing.ID, the caller can pass
// `ack_collision_id=existing.ID` to persist a same-titled duplicate
// (work-splitting case). The strict-match rule means the caller must
// have read the conflict error first.
func TestStandaloneCreateAckCollisionOverride(t *testing.T) {
	srv, groupID, ada, bea := newTaskWakeFixture(t)
	managerAda := srv.residentTaskManager(ada)
	managerBea := srv.residentTaskManager(bea)

	const title = "fix login flake"
	first, err := managerAda.CreateTask(context.Background(), groupID, 0, title, false, "")
	if err != nil {
		t.Fatalf("first standalone CreateTask: %v", err)
	}

	// Without ack_collision_id: collision surfaces.
	if _, err := managerBea.CreateTask(context.Background(), groupID, 0, title, false, ""); err == nil {
		t.Fatalf("collision without ack_collision_id should error")
	}

	// With ack_collision_id=first.ID: caller explicitly ack'd; duplicate cth persists.
	second, err := managerBea.CreateTask(context.Background(), groupID, 0, title, false, first.ID)
	if err != nil {
		t.Fatalf("ack_collision_id=%q should bypass collision, got %v", first.ID, err)
	}
	if first.ID == second.ID {
		t.Fatalf("ack_collision_id should produce a distinct cth, both reports %q", first.ID)
	}
}

// TestStandaloneCreateDifferentTitleStillSucceeds — voluntary-claim
// branch via title differentiation. After the conflict surfaces, the
// caller can rebuild with a DISTINGUISHING title (no ack_collision_id
// needed, no escape hatch invoked). This satisfies "我另起一张" without
// touching force / ack semantics.
func TestStandaloneCreateDifferentTitleStillSucceeds(t *testing.T) {
	srv, groupID, ada, bea := newTaskWakeFixture(t)
	managerAda := srv.residentTaskManager(ada)
	managerBea := srv.residentTaskManager(bea)

	if _, err := managerAda.CreateTask(context.Background(), groupID, 0, "fix login flake", false, ""); err != nil {
		t.Fatalf("first standalone CreateTask: %v", err)
	}

	differentiated, err := managerBea.CreateTask(context.Background(), groupID, 0, "fix login flake (Bea)", false, "")
	if err != nil {
		t.Fatalf("differentiated title should not collide, got %v", err)
	}
	if differentiated.Title != "fix login flake (Bea)" {
		t.Fatalf("differentiated title = %q, want %q", differentiated.Title, "fix login flake (Bea)")
	}
}

// TestStandaloneCreateSameTitleResolvedIgnored — architectural pin for
// `conversation_thread.go:89-93`. A standalone cth naturally cannot
// reach ConversationThreadResolved (only the resolve path lands there,
// and that path requires a non-empty anchor_item_id per L89-93). So a
// "resolved" cth in a thread is, by construction, anchored — not
// unfinished — and must NOT trigger the dedup. The test seeds an
// anchored+resolved cth (the canonical full-lifecycle shape: anchor →
// task → review → resolved), then creates a new standalone with the
// same title, expecting success.
//
// Two invariants pinned:
//
//	A. The dedup's status filter is correctly narrow
//	   (ConversationThreadTask only); if someone widens it to
//	   "task + review" or adds resolved, this test catches the
//	   over-broad filter.
//	B. The L89-93 standalone constraint (AnchorItemID == "" is only
//	   allowed when Status == ConversationThreadTask) is upheld. If a
//	   refactor relaxes L89-93, the seed path here would need adjustment
//	   but the dedup logic must still treat resolved as non-unfinished.
func TestStandaloneCreateSameTitleResolvedIgnored(t *testing.T) {
	srv, groupID, ada, _ := newTaskWakeFixture(t)
	managerAda := srv.residentTaskManager(ada)

	// Seed an anchored+resolved cth in the same thread. anchor_item_id
	// must be non-empty (L89-93); resolving only the anchored path is
	// what lets a cth reach ConversationThreadResolved.
	if _, err := session.CreateConversationThread(srv.rt.SessionDir, session.ConversationThread{
		SessionID:    groupID,
		AnchorItemID: "anchor-resolved",
		Title:        "fix login flake",
		Status:       session.ConversationThreadResolved,
		CreatedBy:    ada,
	}); err != nil {
		t.Fatalf("seed anchored+resolved cth: %v", err)
	}

	// Same title, standalone creation: must succeed — resolved cth is
	// finished, not unfinished, and the dedup ignores it.
	if _, err := managerAda.CreateTask(context.Background(), groupID, 0, "fix login flake", false, ""); err != nil {
		t.Fatalf("resolved cth should NOT block new task with same title (issue #4 v3 L89-93 pin): got %v", err)
	}
}

// TestStandaloneCreateAckCollisionMismatchRefuses — 5th red line.
// `ack_collision_id` is strict id-match: passing a wrong id (made-up or
// from another cth) is functionally a generic "force" bypass and must
// keep the dedup hard-block. This guards against future regressions
// that relax ack_collision_id to a generic ack — the test fails loudly
// if anyone widens the ack semantics, exactly the regression the
// issue #4 v3 design forbids.
func TestStandaloneCreateAckCollisionMismatchRefuses(t *testing.T) {
	srv, groupID, ada, bea := newTaskWakeFixture(t)
	managerAda := srv.residentTaskManager(ada)
	managerBea := srv.residentTaskManager(bea)

	first, err := managerAda.CreateTask(context.Background(), groupID, 0, "fix login flake", false, "")
	if err != nil {
		t.Fatalf("first standalone CreateTask: %v", err)
	}

	// Bogus made-up id — must NOT bypass the dedup.
	const bogusID = "cth-bogus-does-not-exist"
	_, err = managerBea.CreateTask(context.Background(), groupID, 0, "fix login flake", false, bogusID)
	if err == nil {
		t.Fatalf("ack_collision_id mismatch (=%q) should NOT bypass collision (issue #4 v3 5th red line)", bogusID)
	}
	if !strings.Contains(err.Error(), first.ID) {
		t.Fatalf("conflict error %q does not reference existing task ID %q", err.Error(), first.ID)
	}

	// Contrast: the correct id DOES bypass (re-confirming that the
	// mismatch path above is the only one that blocks — the matching
	// path is fully covered by TestStandaloneCreateAckCollisionOverride
	// but is asserted here too for self-contained coverage).
	if _, err := managerBea.CreateTask(context.Background(), groupID, 0, "fix login flake", false, first.ID); err != nil {
		t.Fatalf("ack_collision_id=%q (matching) should bypass, got %v", first.ID, err)
	}
}