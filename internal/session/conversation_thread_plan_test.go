package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTaskPlanNodeFieldsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	task := seedTaskThread(t, dir)

	activityAt := time.Date(2026, 7, 7, 10, 0, 0, 123456789, time.UTC)
	progressAt := time.Date(2026, 7, 7, 10, 5, 0, 987654321, time.UTC)
	full := TaskPiece{
		ID:             "p1",
		Title:          "collect requirements",
		Assignee:       "prt-bella",
		DependsOn:      []string{"p0"},
		Status:         TaskPieceActive,
		Prompt:         "interview the reporter and write down repro steps",
		Handoff:        `{"summary":"upstream notes"}`,
		RetryBudget:    5,
		Attempts:       2,
		FailureReason:  "first attempt timed out",
		LastActivityAt: activityAt,
		LastProgressAt: progressAt,
	}
	minimal := TaskPiece{ID: "p2", Title: "write the fix", Assignee: "prt-carl"}

	if _, err := SetConversationThreadPlan(dir, task.ID, []TaskPiece{full, minimal}); err != nil {
		t.Fatalf("set plan: %v", err)
	}
	got, err := FindConversationThreadByID(dir, task.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(got.Plan) != 2 {
		t.Fatalf("plan length = %d, want 2", len(got.Plan))
	}

	p1 := got.Plan[0]
	if p1.ID != "p1" || p1.Title != full.Title || p1.Assignee != full.Assignee {
		t.Fatalf("piece identity fields lost: %+v", p1)
	}
	if len(p1.DependsOn) != 1 || p1.DependsOn[0] != "p0" {
		t.Fatalf("depends_on = %v, want [p0]", p1.DependsOn)
	}
	if p1.Status != TaskPieceActive {
		t.Fatalf("status = %q, want active", p1.Status)
	}
	if p1.Prompt != full.Prompt {
		t.Fatalf("prompt = %q, want %q", p1.Prompt, full.Prompt)
	}
	if p1.Handoff != full.Handoff {
		t.Fatalf("handoff = %q, want %q", p1.Handoff, full.Handoff)
	}
	if p1.RetryBudget != 5 || p1.Attempts != 2 {
		t.Fatalf("retry_budget/attempts = %d/%d, want 5/2", p1.RetryBudget, p1.Attempts)
	}
	if p1.FailureReason != full.FailureReason {
		t.Fatalf("failure_reason = %q, want %q", p1.FailureReason, full.FailureReason)
	}
	if !p1.LastActivityAt.Equal(activityAt) {
		t.Fatalf("last_activity_at = %v, want %v", p1.LastActivityAt, activityAt)
	}
	if !p1.LastProgressAt.Equal(progressAt) {
		t.Fatalf("last_progress_at = %v, want %v", p1.LastProgressAt, progressAt)
	}

	// A piece the lead left bare is normalized on write: pending status and
	// the default retry budget, both stored explicitly.
	p2 := got.Plan[1]
	if p2.Status != TaskPiecePending {
		t.Fatalf("bare piece status = %q, want pending", p2.Status)
	}
	if p2.RetryBudget != TaskPieceDefaultRetryBudget {
		t.Fatalf("bare piece retry_budget = %d, want %d", p2.RetryBudget, TaskPieceDefaultRetryBudget)
	}
	if !p2.LastActivityAt.IsZero() || !p2.LastProgressAt.IsZero() {
		t.Fatalf("bare piece timestamps should stay zero: %+v", p2)
	}

	// Prove the write path stored the defaults explicitly, not just that the
	// read path backfilled them.
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var planJSON string
	if err := db.QueryRow(`SELECT plan FROM conversation_threads WHERE id = ?`, task.ID).Scan(&planJSON); err != nil {
		t.Fatalf("read stored plan column: %v", err)
	}
	if !strings.Contains(planJSON, `"retry_budget":3`) {
		t.Fatalf("stored plan should carry the explicit default budget, got %s", planJSON)
	}
	if !strings.Contains(planJSON, `"status":"pending"`) {
		t.Fatalf("stored plan should carry the explicit pending status, got %s", planJSON)
	}
}

func TestTaskPlanLegacyRowReadsBackWithDefaults(t *testing.T) {
	dir := t.TempDir()
	task := seedTaskThread(t, dir)

	// A plan stored before pieces became executable nodes carried only the
	// original five fields. Marshal exactly that shape and write it straight
	// into the plan column, bypassing today's write-path normalization.
	type legacyPiece struct {
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Assignee  string   `json:"assignee"`
		DependsOn []string `json:"depends_on,omitempty"`
		Status    string   `json:"status"`
	}
	raw, err := json.Marshal([]legacyPiece{
		{ID: "p1", Title: "triage", Assignee: "prt-bella", Status: TaskPieceActive},
		{ID: "p2", Title: "fix", Assignee: "prt-carl", DependsOn: []string{"p1"}, Status: TaskPiecePending},
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE conversation_threads SET plan = ? WHERE id = ?`, string(raw), task.ID); err != nil {
		db.Close()
		t.Fatalf("write legacy plan: %v", err)
	}
	db.Close()

	got, err := FindConversationThreadByID(dir, task.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(got.Plan) != 2 {
		t.Fatalf("plan length = %d, want 2", len(got.Plan))
	}
	for _, piece := range got.Plan {
		if piece.RetryBudget != TaskPieceDefaultRetryBudget {
			t.Fatalf("piece %q retry_budget = %d, want default %d", piece.ID, piece.RetryBudget, TaskPieceDefaultRetryBudget)
		}
		if piece.Attempts != 0 {
			t.Fatalf("piece %q attempts = %d, want 0", piece.ID, piece.Attempts)
		}
		if piece.Prompt != "" || piece.Handoff != "" || piece.FailureReason != "" {
			t.Fatalf("piece %q node strings should be empty: %+v", piece.ID, piece)
		}
		if !piece.LastActivityAt.IsZero() || !piece.LastProgressAt.IsZero() {
			t.Fatalf("piece %q timestamps should be zero: %+v", piece.ID, piece)
		}
	}
	if got.Plan[0].Status != TaskPieceActive || got.Plan[1].Status != TaskPiecePending {
		t.Fatalf("legacy statuses must be preserved: %+v", got.Plan)
	}
}

func TestUpdateTaskPiece(t *testing.T) {
	dir := t.TempDir()
	task := seedTaskThread(t, dir)
	if _, err := SetConversationThreadPlan(dir, task.ID, []TaskPiece{
		{ID: "p1", Title: "triage", Assignee: "prt-bella", Status: TaskPieceActive},
		{ID: "p2", Title: "fix", Assignee: "prt-carl", DependsOn: []string{"p1"}},
	}); err != nil {
		t.Fatalf("set plan: %v", err)
	}

	activityAt := time.Date(2026, 7, 7, 11, 0, 0, 0, time.UTC)
	updated, err := UpdateTaskPiece(dir, task.ID, "p1", func(piece *TaskPiece) {
		piece.Attempts++
		piece.Status = TaskPieceRetrying
		piece.FailureReason = "assignee turn crashed"
		piece.LastActivityAt = activityAt
	})
	if err != nil {
		t.Fatalf("update piece: %v", err)
	}
	if updated.Plan[0].Attempts != 1 || updated.Plan[0].Status != TaskPieceRetrying {
		t.Fatalf("returned piece = %+v, want attempts 1 / retrying", updated.Plan[0])
	}

	got, err := FindConversationThreadByID(dir, task.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	p1 := got.Plan[0]
	if p1.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", p1.Attempts)
	}
	if p1.Status != TaskPieceRetrying {
		t.Fatalf("status = %q, want retrying", p1.Status)
	}
	if p1.FailureReason != "assignee turn crashed" {
		t.Fatalf("failure_reason = %q", p1.FailureReason)
	}
	if !p1.LastActivityAt.Equal(activityAt) {
		t.Fatalf("last_activity_at = %v, want %v", p1.LastActivityAt, activityAt)
	}
	// The sibling piece is untouched.
	if got.Plan[1].Status != TaskPiecePending || got.Plan[1].Attempts != 0 {
		t.Fatalf("sibling piece must be untouched: %+v", got.Plan[1])
	}

	// MarkTaskPieceStatus rides the same path and must not drop node fields.
	if _, err := MarkTaskPieceStatus(dir, task.ID, "p1", TaskPieceDone); err != nil {
		t.Fatalf("mark status: %v", err)
	}
	got, err = FindConversationThreadByID(dir, task.ID)
	if err != nil {
		t.Fatalf("find after mark: %v", err)
	}
	if got.Plan[0].Status != TaskPieceDone {
		t.Fatalf("status = %q, want done", got.Plan[0].Status)
	}
	if got.Plan[0].FailureReason != "assignee turn crashed" || got.Plan[0].Attempts != 1 {
		t.Fatalf("marking status must preserve node fields: %+v", got.Plan[0])
	}

	// Unknown piece id fails loudly.
	if _, err := UpdateTaskPiece(dir, task.ID, "p9", func(piece *TaskPiece) {
		piece.Attempts++
	}); err == nil || !strings.Contains(err.Error(), `has no piece "p9"`) {
		t.Fatalf("unknown piece should error loudly, got %v", err)
	}
}
