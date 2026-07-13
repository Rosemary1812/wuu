package session

import (
	"errors"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
)

func attemptTaskForTest(t *testing.T, dir, groupID, ownerID, workerID string) ConversationThread {
	t.Helper()
	if _, err := CreateWithMetadata(dir, groupID, "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []participant.Participant{
		{ID: ownerID, Kind: participant.KindNamed, Name: "Owner " + ownerID},
		{ID: workerID, Kind: participant.KindNamed, Name: "Worker " + workerID},
	} {
		if err := UpsertParticipant(dir, p); err != nil {
			t.Fatal(err)
		}
		if err := AddThreadMember(dir, groupID, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	open := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID: groupID, AnchorItemID: "anchor-" + groupID,
		ThreadOwnerParticipantID: ownerID, ParentAuthorParticipantID: ownerID,
	})
	task, err := EscalateConversationThread(dir, open.ID, ownerID, "attempt task")
	if err != nil {
		t.Fatal(err)
	}
	task, err = SetConversationThreadPlan(dir, task.ID, []TaskPiece{{
		ID: "node-1", Title: "worker node", Assignee: workerID, Status: TaskPiecePending,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := SetConversationThreadExecState(dir, task.ID, ExecStateExecuting); err != nil {
		t.Fatal(err)
	}
	task.ExecState = ExecStateExecuting
	return task
}

func TestTaskAttemptLifecycleBindsExactPlanNode(t *testing.T) {
	dir := t.TempDir()
	task := attemptTaskForTest(t, dir, "group-a", "owner-a", "worker-a")
	attempt, reserved, err := ReserveTaskAttempt(dir, task.ID, "node-1", "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	piece := taskPieceByID(reserved.Plan, "node-1")
	if attempt.Status != TaskAttemptQueued || attempt.Ordinal != 1 || piece == nil || piece.CurrentAttemptID != attempt.ID || piece.Status != TaskPieceActive || piece.Attempts != 1 {
		t.Fatalf("reservation mismatch: attempt=%+v piece=%+v", attempt, piece)
	}
	started, err := StartTaskAttempt(dir, attempt.ID, time.Time{})
	if err != nil || started.Status != TaskAttemptRunning || started.StartedAt.IsZero() {
		t.Fatalf("start = %+v, %v", started, err)
	}
	finished, updated, err := FinishTaskAttempt(dir, attempt.ID, TaskAttemptSucceeded, TaskPieceDone, "", "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	piece = taskPieceByID(updated.Plan, "node-1")
	if finished.Status != TaskAttemptSucceeded || finished.FinishedAt.IsZero() || piece == nil || piece.Status != TaskPieceDone || piece.CurrentAttemptID != "" {
		t.Fatalf("finish mismatch: attempt=%+v piece=%+v", finished, piece)
	}
	if _, _, err := FinishTaskAttempt(dir, attempt.ID, TaskAttemptSucceeded, TaskPieceDone, "", "", time.Time{}); err == nil {
		t.Fatal("duplicate completion must not mutate the node twice")
	}
}

func TestTaskAttemptActiveAssigneeReservationIsGlobal(t *testing.T) {
	dir := t.TempDir()
	first := attemptTaskForTest(t, dir, "group-a", "owner-a", "worker")
	second := attemptTaskForTest(t, dir, "group-b", "owner-b", "worker")
	attempt, _, err := ReserveTaskAttempt(dir, first.ID, "node-1", "worker")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReserveTaskAttempt(dir, second.ID, "node-1", "worker"); !errors.Is(err, ErrTaskAssigneeBusy) {
		t.Fatalf("second reservation = %v, want ErrTaskAssigneeBusy", err)
	}
	if _, _, err := FinishTaskAttempt(dir, attempt.ID, TaskAttemptSucceeded, TaskPieceDone, "", "", time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReserveTaskAttempt(dir, second.ID, "node-1", "worker"); err != nil {
		t.Fatalf("reservation after release: %v", err)
	}
}

func TestActiveTaskAttemptForAssigneeReturnsUniqueAssignment(t *testing.T) {
	dir := t.TempDir()
	task := attemptTaskForTest(t, dir, "group-a", "owner-a", "worker-a")
	want, _, err := ReserveTaskAttempt(dir, task.ID, "node-1", "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ActiveTaskAttemptForAssignee(dir, "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.TaskID != task.ID || got.NodeID != "node-1" {
		t.Fatalf("active attempt = %+v, want %+v", got, want)
	}
}

func TestSettleActiveTaskAttemptsPausesWithoutReplay(t *testing.T) {
	dir := t.TempDir()
	task := attemptTaskForTest(t, dir, "group-a", "owner-a", "worker-a")
	attempt, _, err := ReserveTaskAttempt(dir, task.ID, "node-1", "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartTaskAttempt(dir, attempt.ID, time.Time{}); err != nil {
		t.Fatal(err)
	}
	settled, err := SettleActiveTaskAttempts(dir, time.Now().UTC())
	if err != nil || len(settled) != 1 || settled[0].ID != attempt.ID || settled[0].Status != TaskAttemptInterrupted {
		t.Fatalf("settled = %+v, %v", settled, err)
	}
	updated, err := FindConversationThreadByID(dir, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	piece := taskPieceByID(updated.Plan, "node-1")
	if updated.ExecState != ExecStateBlocked || piece == nil || piece.Status != TaskPieceBlocked || piece.CurrentAttemptID != "" {
		t.Fatalf("settled task = exec %q piece %+v", updated.ExecState, piece)
	}
	if again, err := SettleActiveTaskAttempts(dir, time.Now().UTC()); err != nil || len(again) != 0 {
		t.Fatalf("repeat settle = %+v, %v", again, err)
	}
}
