package session

import (
	"strings"
	"testing"
)

func seedTaskThread(t *testing.T, dir string) ConversationThread {
	t.Helper()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	open := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID: "thread-1", AnchorItemID: "message-1", ParentSeq: 1,
		ThreadOwnerParticipantID:  "prt-lead",
		ParentAuthorParticipantID: "prt-lead",
		Title:                     "fix the bug",
	})
	task, err := EscalateConversationThread(dir, open.ID, "human", open.Title)
	if err != nil {
		t.Fatalf("promote Thread: %v", err)
	}
	return task
}

func TestConversationThreadCannotBeCreatedAsTaskOrStandalone(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	for name, thread := range map[string]ConversationThread{
		"born task":  {SessionID: "thread-1", Status: ConversationThreadTask},
		"standalone": {SessionID: "thread-1", Status: ConversationThreadOpen},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := CreateConversationThread(dir, thread); err == nil {
				t.Fatalf("%s must be refused", name)
			}
		})
	}
}

func TestConcludeConversationThreadOnlyByImmutableLead(t *testing.T) {
	dir := t.TempDir()
	task := seedTaskThread(t, dir)
	if task.LeadParticipantID != task.ThreadOwnerParticipantID || task.LeadParticipantID == "" {
		t.Fatalf("promoted Task authority = owner %q lead %q", task.ThreadOwnerParticipantID, task.LeadParticipantID)
	}
	if _, err := ConcludeConversationThread(dir, task.ID, "prt-worker", "done"); err == nil || !strings.Contains(err.Error(), "only the lead") {
		t.Fatalf("non-lead conclusion = %v, want lead refusal", err)
	}
	if _, err := ConcludeConversationThread(dir, task.ID, task.LeadParticipantID, "  "); err == nil {
		t.Fatal("empty conclusion must be refused")
	}
	concluded, err := ConcludeConversationThread(dir, task.ID, task.LeadParticipantID, "fixed and verified")
	if err != nil {
		t.Fatal(err)
	}
	if concluded.Status != ConversationThreadResolved || concluded.Summary != "fixed and verified" {
		t.Fatalf("concluded Task = %+v", concluded)
	}
	if _, err := ConcludeConversationThread(dir, task.ID, task.LeadParticipantID, "again"); err == nil || !strings.Contains(err.Error(), `status is "resolved"`) {
		t.Fatalf("double conclusion = %v, want terminal refusal", err)
	}
}

func TestSetConversationThreadExecState(t *testing.T) {
	dir := t.TempDir()
	task := seedTaskThread(t, dir)
	for _, state := range []string{
		ExecStatePlanning, ExecStateExecuting, ExecStateBlocked,
		ExecStateNeedsHuman, ExecStateCompleted, ExecStateFailed,
	} {
		if err := SetConversationThreadExecState(dir, task.ID, state); err != nil {
			t.Fatalf("set exec state %q: %v", state, err)
		}
		got, err := FindConversationThreadByID(dir, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ExecState != state {
			t.Fatalf("exec state = %q, want %q", got.ExecState, state)
		}
	}
	if err := SetConversationThreadExecState(dir, task.ID, "reviewing"); err == nil {
		t.Fatal("unknown exec state must be refused")
	}
	if err := SetConversationThreadExecState(dir, task.ID, ""); err == nil {
		t.Fatal("empty exec state must be refused")
	}
}
