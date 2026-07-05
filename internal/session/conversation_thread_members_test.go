package session

import (
	"errors"
	"testing"

	"github.com/blueberrycongee/wuu/internal/participant"
)

func TestConversationThreadMembersCRUD(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	noel := participant.Participant{ID: "prt-noel", Kind: participant.KindNamed, Name: "Noel"}
	reviewer := participant.Participant{ID: "prt-reviewer", Kind: participant.KindNamed, Name: "Reviewer"}
	worker := participant.Participant{ID: "prt-worker", Kind: participant.KindEphemeral, Name: "Worker"}
	for _, p := range []participant.Participant{noel, reviewer, worker} {
		if err := UpsertParticipant(dir, p); err != nil {
			t.Fatal(err)
		}
	}

	cth, err := CreateConversationThread(dir, ConversationThread{
		SessionID:    "thread-1",
		AnchorItemID: "seq-3",
		CreatedBy:    noel.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := AddConversationThreadMember(dir, cth.ID, noel.ID); err != nil {
		t.Fatal(err)
	}
	if err := AddConversationThreadMember(dir, cth.ID, noel.ID); err != nil {
		t.Fatalf("duplicate add should be idempotent: %v", err)
	}
	if err := AddConversationThreadMember(dir, cth.ID, reviewer.ID); err != nil {
		t.Fatal(err)
	}
	// Weak-isolation subset mirrors thread_members: only active named
	// participants can be pushed subthread traffic.
	if err := AddConversationThreadMember(dir, cth.ID, worker.ID); err == nil {
		t.Fatal("ephemeral participants must not be subthread members")
	}
	// A missing subthread is refused so a forged cth id can't seed members.
	if err := AddConversationThreadMember(dir, "cth-missing", noel.ID); !errors.Is(err, ErrConversationThreadNotFound) {
		t.Fatalf("add to missing subthread = %v, want ErrConversationThreadNotFound", err)
	}

	members, err := ListConversationThreadMembers(dir, cth.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || !containsString(members, noel.ID) || !containsString(members, reviewer.ID) {
		t.Fatalf("members = %v, want Noel and Reviewer once", members)
	}

	if err := RemoveConversationThreadMember(dir, cth.ID, noel.ID); err != nil {
		t.Fatal(err)
	}
	members, err = ListConversationThreadMembers(dir, cth.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0] != reviewer.ID {
		t.Fatalf("members after remove = %v, want only Reviewer", members)
	}

	if _, err := ListConversationThreadMembers(dir, "cth-missing"); !errors.Is(err, ErrConversationThreadNotFound) {
		t.Fatalf("ListConversationThreadMembers missing = %v, want ErrConversationThreadNotFound", err)
	}
}

func TestConversationThreadMembersRetireCascade(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	noel := participant.Participant{ID: "prt-noel", Kind: participant.KindNamed, Name: "Noel"}
	reviewer := participant.Participant{ID: "prt-reviewer", Kind: participant.KindNamed, Name: "Reviewer"}
	for _, p := range []participant.Participant{noel, reviewer} {
		if err := UpsertParticipant(dir, p); err != nil {
			t.Fatal(err)
		}
	}
	cth, err := CreateConversationThread(dir, ConversationThread{
		SessionID:    "thread-1",
		AnchorItemID: "seq-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := AddConversationThreadMember(dir, cth.ID, noel.ID); err != nil {
		t.Fatal(err)
	}
	if err := AddConversationThreadMember(dir, cth.ID, reviewer.ID); err != nil {
		t.Fatal(err)
	}

	// Retire is an UPDATE, not a DELETE, so the FK cascade never fires — the
	// retire transaction must clear the subthread membership explicitly, and
	// the List filter must exclude the retired participant either way.
	if err := RetireParticipant(dir, noel.ID); err != nil {
		t.Fatal(err)
	}
	members, err := ListConversationThreadMembers(dir, cth.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0] != reviewer.ID {
		t.Fatalf("members after retire = %v, want only Reviewer", members)
	}
}
