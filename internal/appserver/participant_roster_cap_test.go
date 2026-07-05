package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
)

// TestParticipantSaveEnforcesRosterCap covers the active named-agent roster
// cap. #all no longer mirrors this roster, but the product still keeps the
// named roster bounded while the team UI and resident runtime are tuned
// around small groups. Retired participants free a slot.
func TestParticipantSaveEnforcesRosterCap(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})

	// Fill the roster to capacity through the direct store helper, which
	// bypasses the RPC cap, so the entry point under test starts exactly at
	// the limit.
	for i := 0; i < maxNamedParticipants; i++ {
		saveNamedParticipant(t, rt, fmt.Sprintf("Agent%d", i), "reviewer", "")
	}

	saveOver := func(reqID string) map[string]any {
		raw, err := json.Marshal(map[string]any{
			"id":     reqID,
			"method": MethodParticipantSave,
			"params": ParticipantSaveParams{Name: "OneTooMany", Role: "reviewer"},
		})
		if err != nil {
			t.Fatalf("marshal save: %v", err)
		}
		if err := srv.handleLine(context.Background(), raw); err != nil {
			t.Fatalf("participant/save: %v", err)
		}
		return responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), reqID)
	}

	// At capacity: a new named agent is rejected with a clear roster-full error.
	resp := saveOver("over-cap")
	if resp["error"] == nil {
		t.Fatalf("expected roster-full error at capacity, got result %v", resp["result"])
	}
	if msg := fmt.Sprint(resp["error"]); !strings.Contains(msg, "roster is full") {
		t.Fatalf("expected roster-full error, got %q", msg)
	}

	// Retiring one frees a slot: the same creation then succeeds, proving
	// retired participants do not count toward the cap.
	all, err := session.ListParticipants(rt.SessionDir, participant.KindNamed)
	if err != nil || len(all) == 0 {
		t.Fatalf("list participants: %v (n=%d)", err, len(all))
	}
	if err := session.RetireParticipant(rt.SessionDir, all[0].ID); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if resp := saveOver("after-retire"); resp["error"] != nil {
		t.Fatalf("expected success after freeing a slot, got error %v", resp["error"])
	}
}
