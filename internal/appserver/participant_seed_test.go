package appserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
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

func TestEnsureDefaultParticipantSeedsAndy(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	srv := New(rt, &lockedBuffer{})

	ensureDefaultParticipantForTest(t, srv)

	list := listParticipantsForTest(t, srv)
	if len(list.Participants) != 1 {
		t.Fatalf("participant list = %d, want 1: %+v", len(list.Participants), list.Participants)
	}
	andy := list.Participants[0]
	if andy.Name != andyName {
		t.Errorf("name = %q, want %q", andy.Name, andyName)
	}
	if andy.Kind != string(participant.KindNamed) {
		t.Errorf("kind = %q, want %q", andy.Kind, participant.KindNamed)
	}
	if andy.Role != "general-purpose" {
		t.Errorf("role = %q, want %q", andy.Role, "general-purpose")
	}
	if andy.Tagline != defaultSeedParticipantTagline {
		t.Errorf("tagline = %q, want %q", andy.Tagline, defaultSeedParticipantTagline)
	}
	if andy.Model != "" {
		t.Errorf("model = %q, want empty", andy.Model)
	}
	if !strings.Contains(andy.Workspace, filepath.Join("participants", andy.ID)) {
		t.Errorf("workspace should contain participants/<id>, got %q", andy.Workspace)
	}
	if !strings.Contains(andy.Memory, "团队组建者") {
		t.Errorf("memory should carry the preset persona, got %q", andy.Memory)
	}

	memPath := filepath.Join(andy.Workspace, participantMemoryFileName)
	data, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatalf("MEMORY.md should exist: %v", err)
	}
	if !strings.Contains(string(data), "团队组建者") || !strings.Contains(string(data), "create_group") {
		t.Errorf("MEMORY.md should contain the preset persona, got %q", string(data))
	}

	markerPath := filepath.Join(rt.WuuHome, defaultAgentSeededMarkerName)
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("seed marker should exist after seeding: %v", err)
	}
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
