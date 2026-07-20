package appserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
)

const andyName = "Andy"

func ensureDefaultParticipantForTest(t *testing.T, srv *Server) {
	t.Helper()
	if err := srv.ensureDefaultParticipant(); err != nil {
		t.Fatalf("ensureDefaultParticipant: %v", err)
	}
}

func listParticipantsForTest(t *testing.T, srv *Server) ParticipantListResult {
	t.Helper()
	out := &lockedBuffer{}
	prev := srv.out
	srv.out = out
	t.Cleanup(func() { srv.out = prev })
	raw := []byte(fmt.Sprintf(`{"id":"list","method":%q}`, MethodParticipantList))
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("participant/list: %v", err)
	}
	return remarshal[ParticipantListResult](t, responseByID(t, parseOutput(t, out.String()), "list")["result"])
}

func saveNamedParticipant(t *testing.T, rt *runtime.Session, name, role, model string) string {
	t.Helper()
	now := time.Now().UTC()
	p := participant.Participant{
		ID:        participant.NewID(),
		Kind:      participant.KindNamed,
		Name:      name,
		Role:      role,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := session.UpsertParticipant(rt.SessionDir, p); err != nil {
		t.Fatalf("upsert participant: %v", err)
	}
	return p.ID
}

func TestEnsureDefaultParticipantSkipsWhenMarkerPresent(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	if err := os.MkdirAll(rt.WuuHome, 0o755); err != nil {
		t.Fatalf("mkdir wuu home: %v", err)
	}
	markerPath := filepath.Join(rt.WuuHome, defaultAgentSeededMarkerName)
	if err := os.WriteFile(markerPath, []byte("seeded earlier\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	srv := New(rt, &lockedBuffer{})

	ensureDefaultParticipantForTest(t, srv)
	list := listParticipantsForTest(t, srv)
	if len(list.Participants) != 0 {
		t.Fatalf("marker must block seeding even with an empty roster, got %+v", list.Participants)
	}
}

func TestEnsureDefaultParticipantIsIdempotent(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	srv := New(rt, &lockedBuffer{})

	ensureDefaultParticipantForTest(t, srv)
	firstList := listParticipantsForTest(t, srv)
	if len(firstList.Participants) != 1 {
		t.Fatalf("first ensure should produce 1 participant, got %d", len(firstList.Participants))
	}
	firstID := firstList.Participants[0].ID

	ensureDefaultParticipantForTest(t, srv)
	secondList := listParticipantsForTest(t, srv)
	if len(secondList.Participants) != 1 {
		t.Fatalf("second ensure should still produce 1 participant, got %d", len(secondList.Participants))
	}
	if secondList.Participants[0].ID != firstID {
		t.Errorf("second ensure must not create a duplicate: firstID=%q secondID=%q", firstID, secondList.Participants[0].ID)
	}
}

func TestEnsureDefaultParticipantSkipsWhenRetired(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")

	now := time.Now().UTC()
	ghost := participant.Participant{
		ID:        participant.NewID(),
		Kind:      participant.KindNamed,
		Name:      andyName,
		Role:      "reviewer",
		Avatar:    "👻",
		Tagline:   "retired ghost",
		Workspace: filepath.Join(rt.WuuHome, "participants", "ghost"),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := session.UpsertParticipant(rt.SessionDir, ghost); err != nil {
		t.Fatalf("upsert ghost: %v", err)
	}
	if err := session.RetireParticipant(rt.SessionDir, ghost.ID); err != nil {
		t.Fatalf("retire ghost: %v", err)
	}

	srv := New(rt, &lockedBuffer{})

	all, err := session.ListParticipants(rt.SessionDir, participant.KindNamed)
	if err != nil {
		t.Fatalf("list named: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("active named list should be empty (ghost retired, no resurrection), got %+v", all)
	}
	if err := srv.ensureDefaultParticipant(); err != nil {
		t.Fatalf("ensureDefaultParticipant (re-run) should still skip: %v", err)
	}

	after := listParticipantsForTest(t, srv)
	for _, p := range after.Participants {
		if p.Name == andyName && string(p.Kind) == string(participant.KindNamed) {
			t.Errorf("retired named participant must block Andy seed, got %+v", p)
		}
	}
}
