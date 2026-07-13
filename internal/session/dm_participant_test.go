package session

import "testing"

func TestBindDMParticipantSetsAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir, "dm-sess")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := BindDMParticipant(dir, s.ID, "prt-andy")
	if err != nil {
		t.Fatalf("BindDMParticipant: %v", err)
	}
	if updated.DMParticipantID != "prt-andy" {
		t.Fatalf("DMParticipantID = %q, want %q", updated.DMParticipantID, "prt-andy")
	}

	found, ok, err := Find(dir, s.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if !ok {
		t.Fatalf("session not found after BindDMParticipant")
	}
	if found.DMParticipantID != "prt-andy" {
		t.Fatalf("Find DMParticipantID = %q, want %q", found.DMParticipantID, "prt-andy")
	}

	listed, err := List(dir, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 session, got %d", len(listed))
	}
	if listed[0].DMParticipantID != "prt-andy" {
		t.Fatalf("List DMParticipantID = %q, want %q", listed[0].DMParticipantID, "prt-andy")
	}
}

func TestBindDMParticipantTrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := BindDMParticipant(dir, s.ID, "  prt-bea  ")
	if err != nil {
		t.Fatalf("BindDMParticipant: %v", err)
	}
	if updated.DMParticipantID != "prt-bea" {
		t.Fatalf("DMParticipantID = %q, want %q", updated.DMParticipantID, "prt-bea")
	}
}
