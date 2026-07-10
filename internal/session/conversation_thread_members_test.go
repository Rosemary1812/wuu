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
	for _, participantID := range []string{noel.ID, reviewer.ID} {
		if err := AddThreadMember(dir, "thread-1", participantID); err != nil {
			t.Fatal(err)
		}
	}

	cth, err := CreateConversationThread(dir, ConversationThread{
		SessionID: "thread-1", AnchorItemID: "seq-3", CreatedBy: noel.ID,
		ParentSeq: 3, ParentAuthorParticipantID: noel.ID, ThreadOwnerParticipantID: noel.ID,
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

	if err := RemoveConversationThreadMember(dir, cth.ID, reviewer.ID); err != nil {
		t.Fatal(err)
	}
	members, err = ListConversationThreadMembers(dir, cth.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0] != noel.ID {
		t.Fatalf("members after remove = %v, want only Noel", members)
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
		if err := AddThreadMember(dir, "thread-1", p.ID); err != nil {
			t.Fatal(err)
		}
	}
	cth, err := CreateConversationThread(dir, ConversationThread{
		SessionID: "thread-1", AnchorItemID: "seq-1",
		ParentSeq: 1, ParentAuthorParticipantID: reviewer.ID, ThreadOwnerParticipantID: reviewer.ID,
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

func TestConversationThreadMembersStayInsideParentGroup(t *testing.T) {
	dir := t.TempDir()
	for _, groupID := range []string{"group-a", "group-b"} {
		if _, err := CreateWithMetadata(dir, groupID, t.TempDir()); err != nil {
			t.Fatal(err)
		}
		if _, err := SetGroupThread(dir, groupID, true); err != nil {
			t.Fatal(err)
		}
	}
	owner := participant.Participant{ID: "prt-owner", Kind: participant.KindNamed, Name: "Owner"}
	member := participant.Participant{ID: "prt-member", Kind: participant.KindNamed, Name: "Member"}
	outsider := participant.Participant{ID: "prt-outsider", Kind: participant.KindNamed, Name: "Outsider"}
	for _, p := range []participant.Participant{owner, member, outsider} {
		if err := UpsertParticipant(dir, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{owner.ID, member.ID} {
		if err := AddThreadMember(dir, "group-a", id); err != nil {
			t.Fatal(err)
		}
	}
	if err := AddThreadMember(dir, "group-b", outsider.ID); err != nil {
		t.Fatal(err)
	}
	cth, err := CreateConversationThread(dir, ConversationThread{
		SessionID: "group-a", AnchorItemID: "message-1", ParentSeq: 1,
		ParentAuthorParticipantID: owner.ID, ThreadOwnerParticipantID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := AddConversationThreadMember(dir, cth.ID, outsider.ID); err == nil {
		t.Fatal("a named agent from another group must not join this Thread")
	}
	if err := AddConversationThreadMember(dir, cth.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if err := RemoveThreadMember(dir, "group-a", member.ID); err != nil {
		t.Fatal(err)
	}
	members, err := ListConversationThreadMembers(dir, cth.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0] != owner.ID {
		t.Fatalf("members after parent-group removal = %v, want owner only", members)
	}
}
