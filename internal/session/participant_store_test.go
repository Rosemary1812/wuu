package session

import (
	"testing"

	"github.com/blueberrycongee/wuu/internal/participant"
)

func TestParticipantCRUD(t *testing.T) {
	dir := t.TempDir()
	p := participant.Participant{
		ID: participant.NewID(), Kind: participant.KindEphemeral,
		Name: "Reviewer·auth", Role: "reviewer", Avatar: "🧐",
	}
	if err := UpsertParticipant(dir, p); err != nil {
		t.Fatal(err)
	}
	got, err := GetParticipant(dir, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != p.Name || got.Kind != p.Kind || got.Role != p.Role {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	list, err := ListParticipants(dir, participant.KindEphemeral)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
}

func TestNamedParticipantUniqueName(t *testing.T) {
	dir := t.TempDir()
	a := participant.Participant{ID: participant.NewID(), Kind: participant.KindNamed, Name: "Noel", Role: "reviewer"}
	b := participant.Participant{ID: participant.NewID(), Kind: participant.KindNamed, Name: "Noel", Role: "qa"}
	if err := UpsertParticipant(dir, a); err != nil {
		t.Fatal(err)
	}
	if err := UpsertParticipant(dir, b); err == nil {
		t.Fatal("expected unique-name violation for active named participants")
	}
}
