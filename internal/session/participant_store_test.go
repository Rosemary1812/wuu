package session

import (
	"errors"
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

func TestRetireParticipant(t *testing.T) {
	dir := t.TempDir()
	p := participant.Participant{
		ID: participant.NewID(), Kind: participant.KindEphemeral,
		Name: "Reviewer·auth", Role: "reviewer",
	}
	if err := UpsertParticipant(dir, p); err != nil {
		t.Fatal(err)
	}
	if err := RetireParticipant(dir, p.ID); err != nil {
		t.Fatal(err)
	}
	got, err := GetParticipant(dir, p.ID)
	if err != nil {
		t.Fatalf("retired participant should still be readable by ID: %v", err)
	}
	if got.RetiredAt == nil {
		t.Error("RetiredAt = nil, want non-nil after retire")
	}
	list, err := ListParticipants(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("ListParticipants after retire = %v, want empty", list)
	}
}

func TestRetireParticipantNotFound(t *testing.T) {
	dir := t.TempDir()
	err := RetireParticipant(dir, "prt-doesnotexist0000")
	if !errors.Is(err, ErrParticipantNotFound) {
		t.Errorf("RetireParticipant unknown ID = %v, want ErrParticipantNotFound", err)
	}
}

func TestRetiredNamedParticipantFreesName(t *testing.T) {
	dir := t.TempDir()
	a := participant.Participant{ID: participant.NewID(), Kind: participant.KindNamed, Name: "Noel", Role: "reviewer"}
	if err := UpsertParticipant(dir, a); err != nil {
		t.Fatal(err)
	}
	if err := RetireParticipant(dir, a.ID); err != nil {
		t.Fatal(err)
	}
	b := participant.Participant{ID: participant.NewID(), Kind: participant.KindNamed, Name: "Noel", Role: "qa"}
	if err := UpsertParticipant(dir, b); err != nil {
		t.Errorf("upsert named participant with retired name = %v, want nil", err)
	}
}
