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
	if andy.Avatar != "🦉" {
		t.Errorf("avatar = %q, want owl", andy.Avatar)
	}
	if andy.Tagline != "随时开工的常驻搭档，可以帮你搭建团队" {
		t.Errorf("tagline = %q, want constant tagline", andy.Tagline)
	}
	if andy.Model != "" {
		t.Errorf("model = %q, want empty", andy.Model)
	}
	if !strings.Contains(andy.Workspace, filepath.Join("participants", andy.ID)) {
		t.Errorf("workspace should contain participants/<id>, got %q", andy.Workspace)
	}
	if andy.Memory != "" {
		t.Errorf("memory = %q, want empty", andy.Memory)
	}

	memPath := filepath.Join(andy.Workspace, participantMemoryFileName)
	info, err := os.Stat(memPath)
	if err != nil {
		t.Fatalf("MEMORY.md should exist: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("MEMORY.md size = %d, want 0", info.Size())
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
