package session

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
)

const testConversationThreadOwnerID = "prt-thread-owner"

func createOpenConversationThreadForTest(t *testing.T, dir string, thread ConversationThread) ConversationThread {
	t.Helper()
	owner := strings.TrimSpace(thread.ThreadOwnerParticipantID)
	if owner == "" {
		owner = testConversationThreadOwnerID
		thread.ThreadOwnerParticipantID = owner
	}
	if err := UpsertParticipant(dir, participant.Participant{
		ID: owner, Kind: participant.KindNamed, Name: "Thread Owner " + owner,
	}); err != nil {
		t.Fatalf("upsert thread owner: %v", err)
	}
	if err := AddThreadMember(dir, thread.SessionID, owner); err != nil {
		t.Fatalf("add thread owner to parent: %v", err)
	}
	if thread.ParentSeq <= 0 {
		thread.ParentSeq = 1
	}
	if strings.TrimSpace(thread.ParentAuthorParticipantID) == "" {
		thread.ParentAuthorParticipantID = owner
	}
	created, err := CreateConversationThread(dir, thread)
	if err != nil {
		t.Fatalf("create open conversation thread: %v", err)
	}
	return created
}

func TestConversationThreadCRUD(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}

	first := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID:    " thread-1 ",
		AnchorItemID: " item-1 ",
		Title:        " Review auth flow ",
		CreatedBy:    " prt-reviewer ",
		CreatedAt:    time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC),
	})
	if first.ID == "" || !strings.HasPrefix(first.ID, "cth-") {
		t.Fatalf("expected generated cth id, got %+v", first)
	}
	if first.SessionID != "thread-1" || first.AnchorItemID != "item-1" || first.Title != "Review auth flow" || first.CreatedBy != "prt-reviewer" {
		t.Fatalf("thread fields were not normalized: %+v", first)
	}
	if first.Status != ConversationThreadOpen {
		t.Fatalf("default status = %q, want open", first.Status)
	}

	second := createOpenConversationThreadForTest(t, dir, ConversationThread{
		ID:           "cth-custom",
		SessionID:    "thread-1",
		AnchorItemID: "item-2",
		ParentSeq:    2,
		CreatedAt:    time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC),
	})
	if err := UpdateConversationThreadStatus(dir, second.ID, ConversationThreadResolved); err != nil {
		t.Fatal(err)
	}
	second.Status = ConversationThreadResolved
	if second.ID != "cth-custom" || second.Status != ConversationThreadResolved {
		t.Fatalf("custom thread not persisted: %+v", second)
	}

	if err := UpdateConversationThreadStatus(dir, first.ID, ConversationThreadResolved); err != nil {
		t.Fatal(err)
	}
	list, err := ListConversationThreads(dir, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 conversation threads, got %+v", list)
	}
	if list[0].ID != first.ID || list[0].Status != ConversationThreadResolved || list[1].ID != second.ID {
		t.Fatalf("unexpected listed threads: %+v", list)
	}
}

// A reply subthread created from a parent message persists that binding: its
// seq and author survive a create -> find -> list round trip (T3).
func TestConversationThreadParentBindingPersists(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "grp", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	created := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID:                 "grp",
		AnchorItemID:              "grp-turn-0001-item-1",
		Title:                     "converge",
		ParentSeq:                 7,
		ThreadOwnerParticipantID:  " prt-ada ",
		ParentAuthorParticipantID: " prt-ada ",
	})
	if created.ParentSeq != 7 || created.ParentAuthorParticipantID != "prt-ada" || created.ThreadOwnerParticipantID != "prt-ada" {
		t.Fatalf("create returned parent binding %d/%q, want 7/prt-ada", created.ParentSeq, created.ParentAuthorParticipantID)
	}
	got, err := FindConversationThreadByID(dir, created.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.ParentSeq != 7 || got.ParentAuthorParticipantID != "prt-ada" {
		t.Fatalf("persisted parent binding = %d/%q, want 7/prt-ada", got.ParentSeq, got.ParentAuthorParticipantID)
	}
	list, err := ListConversationThreads(dir, "grp")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ParentSeq != 7 || list[0].ParentAuthorParticipantID != "prt-ada" {
		t.Fatalf("listed parent binding = %+v", list)
	}
}

func TestCreateConversationThreadRejectsDuplicateAnchor(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "group", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	first := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID: "group", AnchorItemID: "message-1", ParentSeq: 1,
	})
	_, err := CreateConversationThread(dir, ConversationThread{
		SessionID: "group", AnchorItemID: first.AnchorItemID, ParentSeq: 1,
		ParentAuthorParticipantID: first.ParentAuthorParticipantID,
		ThreadOwnerParticipantID:  first.ThreadOwnerParticipantID,
	})
	if !errors.Is(err, ErrConversationThreadAnchorUsed) {
		t.Fatalf("duplicate anchor error = %v, want ErrConversationThreadAnchorUsed", err)
	}
}

func TestConversationThreadAnchorMigrationMergesReachableState(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "group", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	first := createOpenConversationThreadForTest(t, dir, ConversationThread{
		ID: "cth-first", SessionID: "group", AnchorItemID: "message-1", ParentSeq: 1,
	})
	secondMember := participant.Participant{ID: "prt-second", Kind: participant.KindNamed, Name: "Second"}
	if err := UpsertParticipant(dir, secondMember); err != nil {
		t.Fatal(err)
	}
	if err := AddThreadMember(dir, "group", secondMember.ID); err != nil {
		t.Fatal(err)
	}

	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP INDEX idx_conversation_threads_anchor`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO conversation_threads (
	id, session_id, anchor_item_id, title, status, created_by, created_at,
	thread_owner_participant_id, parent_seq, parent_author_participant_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"cth-second", "group", first.AnchorItemID, "duplicate title", string(ConversationThreadOpen),
		"human", timeText(first.CreatedAt.Add(time.Second)), secondMember.ID, 1, secondMember.ID); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO conversation_thread_members (conversation_thread_id, participant_id, joined_at)
VALUES (?, ?, ?)`, "cth-second", secondMember.ID, first.CreatedAt.Add(time.Second).UnixMilli()); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO session_messages (session_id, seq, role, content, thread_id)
VALUES (?, ?, ?, ?, ?)`, "group", 1, "assistant", "duplicate reply", "cth-second"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO task_events (id, session_id, task_id, node_id, seq, kind, actor, summary, payload, at)
VALUES (?, ?, ?, '', 1, 'note', '', '', '', ?)`, "evt-duplicate", "group", "cth-second", time.Now().UnixMilli()); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	threads, err := ListConversationThreads(dir, "group")
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0].ID != first.ID {
		t.Fatalf("anchor migration threads = %+v, want only %q", threads, first.ID)
	}
	members, err := ListConversationThreadMembers(dir, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(members, secondMember.ID) {
		t.Fatalf("merged members = %v, want %q", members, secondMember.ID)
	}
	db, err = openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var messageThreadID, eventTaskID string
	if err := db.QueryRow(`SELECT thread_id FROM session_messages WHERE session_id = ? AND seq = 1`, "group").Scan(&messageThreadID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT task_id FROM task_events WHERE id = ?`, "evt-duplicate").Scan(&eventTaskID); err != nil {
		t.Fatal(err)
	}
	if messageThreadID != first.ID || eventTaskID != first.ID {
		t.Fatalf("merged references = message %q event %q, want %q", messageThreadID, eventTaskID, first.ID)
	}
}

func TestFindConversationThreadByID(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	created := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID:    "thread-1",
		AnchorItemID: "seq-7",
		Title:        "Discuss retry",
		CreatedBy:    "prt-author",
	})

	got, err := FindConversationThreadByID(dir, " "+created.ID+" ")
	if err != nil {
		t.Fatalf("FindConversationThreadByID() error = %v", err)
	}
	if got.ID != created.ID || got.SessionID != "thread-1" || got.AnchorItemID != "seq-7" {
		t.Fatalf("unexpected thread: %+v", got)
	}
	if got.Title != "Discuss retry" || got.CreatedBy != "prt-author" || got.Status != ConversationThreadOpen {
		t.Fatalf("thread fields not loaded: %+v", got)
	}

	if _, err := FindConversationThreadByID(dir, "cth-missing"); !errors.Is(err, ErrConversationThreadNotFound) {
		t.Fatalf("FindConversationThreadByID(missing) error = %v, want ErrConversationThreadNotFound", err)
	}
	if _, err := FindConversationThreadByID(dir, "   "); !errors.Is(err, ErrConversationThreadNotFound) {
		t.Fatalf("FindConversationThreadByID(blank) error = %v, want ErrConversationThreadNotFound", err)
	}
}

func TestEscalateConversationThread(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	created := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID:                "thread-1",
		AnchorItemID:             "seq-3",
		Title:                    "discuss",
		CreatedBy:                "prt-opener",
		ThreadOwnerParticipantID: "prt-lead",
	})

	escalated, err := EscalateConversationThread(dir, created.ID, " human ", " Ship the fix ")
	if err != nil {
		t.Fatalf("EscalateConversationThread: %v", err)
	}
	if escalated.Status != ConversationThreadTask {
		t.Fatalf("status = %q, want task", escalated.Status)
	}
	if escalated.EscalatedBy != "human" {
		t.Fatalf("escalated_by = %q, want human", escalated.EscalatedBy)
	}
	if escalated.LeadParticipantID != "prt-lead" {
		t.Fatalf("lead_participant_id = %q, want prt-lead", escalated.LeadParticipantID)
	}
	if escalated.Title != "Ship the fix" {
		t.Fatalf("title = %q, want Ship the fix", escalated.Title)
	}
	if escalated.EscalatedAt.IsZero() {
		t.Fatal("escalated_at must be set on escalation")
	}
	if escalated.ExecState != ExecStatePlanning {
		t.Fatalf("exec_state = %q, want planning", escalated.ExecState)
	}
	if escalated.ID != created.ID || escalated.ThreadOwnerParticipantID != created.ThreadOwnerParticipantID {
		t.Fatalf("promotion must preserve the same cth and owner: before=%+v after=%+v", created, escalated)
	}
	if _, err := EscalateConversationThread(dir, created.ID, "human", "again"); err == nil || !strings.Contains(err.Error(), `status is "task"`) {
		t.Fatalf("an already-promoted task must not be promoted again, got %v", err)
	}

	// Generic status mutation cannot resolve an active task.
	if err := UpdateConversationThreadStatus(dir, created.ID, ConversationThreadResolved); err == nil || !strings.Contains(err.Error(), "active task") {
		t.Fatalf("generic status update must reject an active task, got %v", err)
	}
	if _, err := ConcludeConversationThread(dir, created.ID, "prt-lead", "done"); err != nil {
		t.Fatalf("lead conclude: %v", err)
	}
	got, err := FindConversationThreadByID(dir, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ConversationThreadResolved {
		t.Fatalf("status = %q, want resolved", got.Status)
	}
	if got.EscalatedAt.IsZero() {
		t.Fatal("escalated_at must survive resolve")
	}
	// The lead survives resolve too — reclaim of lead authority is via status ->
	// resolved (the workflow gate requires status == task), not by nulling the field.
	if got.LeadParticipantID != "prt-lead" {
		t.Fatalf("lead_participant_id = %q, want prt-lead to survive resolve", got.LeadParticipantID)
	}
	if got.Summary != "done" {
		t.Fatalf("summary = %q, want done", got.Summary)
	}
	if err := UpdateConversationThreadStatus(dir, created.ID, ConversationThreadOpen); err == nil || !strings.Contains(err.Error(), "cannot be reopened") {
		t.Fatalf("resolved task must not reopen as a Thread, got %v", err)
	}
}

func TestEscalateConversationThreadRejectsDepartedOwner(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	created := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID: "thread-1", AnchorItemID: "seq-1", ThreadOwnerParticipantID: "prt-departed",
	})
	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM thread_members WHERE session_id = ? AND participant_id = ?`, "thread-1", "prt-departed"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	if _, err := EscalateConversationThread(dir, created.ID, "human", ""); err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("departed owner promotion must be rejected, got %v", err)
	}
	got, err := FindConversationThreadByID(dir, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ConversationThreadOpen || got.LeadParticipantID != "" {
		t.Fatalf("rejected promotion mutated thread: %+v", got)
	}
}

func TestConversationThreadOwnerMigrationUsesOnlyValidCurrentAuthority(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "group", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []participant.Participant{
		{ID: "prt-valid-lead", Kind: participant.KindNamed, Name: "Valid Lead"},
		{ID: "prt-valid-parent", Kind: participant.KindNamed, Name: "Valid Parent"},
		{ID: "prt-departed-parent", Kind: participant.KindNamed, Name: "Departed Parent"},
	} {
		if err := UpsertParticipant(dir, p); err != nil {
			t.Fatal(err)
		}
		if err := AddThreadMember(dir, "group", p.ID); err != nil {
			t.Fatal(err)
		}
	}
	createOpenConversationThreadForTest(t, dir, ConversationThread{
		ID: "cth-open-valid", SessionID: "group", AnchorItemID: "open-valid",
		ThreadOwnerParticipantID: "prt-valid-parent", ParentAuthorParticipantID: "prt-valid-parent",
	})
	createOpenConversationThreadForTest(t, dir, ConversationThread{
		ID: "cth-open-departed", SessionID: "group", AnchorItemID: "open-departed",
		ThreadOwnerParticipantID: "prt-departed-parent", ParentAuthorParticipantID: "prt-departed-parent",
	})

	db, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range []ConversationThread{
		{ID: "cth-task-valid", SessionID: "group", AnchorItemID: "task-valid", Status: ConversationThreadTask, LeadParticipantID: "prt-valid-lead", ParentAuthorParticipantID: "prt-valid-parent"},
		{ID: "cth-task-invalid", SessionID: "group", AnchorItemID: "task-invalid", Status: ConversationThreadTask, LeadParticipantID: "prt-ghost", ParentAuthorParticipantID: "prt-valid-parent"},
	} {
		if _, err := db.Exec(`
INSERT INTO conversation_threads (
	id, session_id, anchor_item_id, title, status, created_by, created_at,
	escalated_at, lead_participant_id, parent_seq, parent_author_participant_id
) VALUES (?, ?, ?, '', ?, '', ?, ?, ?, 1, ?)`, task.ID, task.SessionID,
			task.AnchorItemID, string(task.Status), timeText(time.Now().UTC()),
			timeText(time.Now().UTC()), task.LeadParticipantID,
			task.ParentAuthorParticipantID); err != nil {
			db.Close()
			t.Fatalf("insert legacy task %s: %v", task.ID, err)
		}
	}
	if _, err := db.Exec(`UPDATE conversation_threads SET thread_owner_participant_id = ''`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM thread_members WHERE session_id = ? AND participant_id = ?`, "group", "prt-departed-parent"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	threads, err := ListConversationThreads(dir, "group")
	if err != nil {
		t.Fatal(err)
	}
	owners := make(map[string]string, len(threads))
	for _, thread := range threads {
		owners[thread.ID] = thread.ThreadOwnerParticipantID
	}
	if owners["cth-task-valid"] != "prt-valid-lead" {
		t.Fatalf("valid task lead was not migrated: %v", owners)
	}
	if owners["cth-task-invalid"] != "" {
		t.Fatalf("invalid task lead fell back to parent author: %v", owners)
	}
	if owners["cth-open-valid"] != "prt-valid-parent" {
		t.Fatalf("valid open parent author was not migrated: %v", owners)
	}
	if owners["cth-open-departed"] != "" {
		t.Fatalf("departed open parent author was migrated: %v", owners)
	}
}

func TestEscalateConversationThreadMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := EscalateConversationThread(dir, "cth-missing", "", ""); !errors.Is(err, ErrConversationThreadNotFound) {
		t.Fatalf("EscalateConversationThread(missing) = %v, want ErrConversationThreadNotFound", err)
	}
	if err := SetConversationThreadSummary(dir, "cth-missing", "x"); !errors.Is(err, ErrConversationThreadNotFound) {
		t.Fatalf("SetConversationThreadSummary(missing) = %v, want ErrConversationThreadNotFound", err)
	}
}

func TestLeadTaskThreads(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "grp-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}

	// A task cth led by prt-lead.
	led := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID: "grp-1", AnchorItemID: "item-1", ParentSeq: 1,
		ThreadOwnerParticipantID: "prt-lead", CreatedAt: time.Date(2026, 7, 3, 8, 0, 0, 0, time.UTC),
	})
	if _, err := EscalateConversationThread(dir, led.ID, "human", "Ship it"); err != nil {
		t.Fatal(err)
	}
	// A task cth led by a different agent — must not leak to prt-lead.
	other := createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID: "grp-1", AnchorItemID: "item-2", ParentSeq: 2,
		ThreadOwnerParticipantID: "prt-other", CreatedAt: time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC),
	})
	if _, err := EscalateConversationThread(dir, other.ID, "human", ""); err != nil {
		t.Fatal(err)
	}
	// An open (never escalated) cth also created by prt-lead is not a task.
	createOpenConversationThreadForTest(t, dir, ConversationThread{
		SessionID: "grp-1", AnchorItemID: "item-3", CreatedBy: "prt-lead",
		ParentSeq: 3, ThreadOwnerParticipantID: "prt-lead",
	})

	leadTasks, err := LeadTaskThreads(dir, " prt-lead ")
	if err != nil {
		t.Fatalf("LeadTaskThreads: %v", err)
	}
	if len(leadTasks) != 1 || leadTasks[0].ID != led.ID {
		t.Fatalf("LeadTaskThreads(prt-lead) = %+v, want only %q", leadTasks, led.ID)
	}
	if leadTasks[0].Status != ConversationThreadTask || leadTasks[0].LeadParticipantID != "prt-lead" {
		t.Fatalf("unexpected lead task fields: %+v", leadTasks[0])
	}

	// Resolving the task reclaims authority: it no longer shows up as a lead task.
	if _, err := ConcludeConversationThread(dir, led.ID, "prt-lead", "done"); err != nil {
		t.Fatal(err)
	}
	after, err := LeadTaskThreads(dir, "prt-lead")
	if err != nil {
		t.Fatalf("LeadTaskThreads after resolve: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("resolved task still returned as lead task: %+v", after)
	}

	// Empty lead id yields nothing without touching the store.
	if got, err := LeadTaskThreads(dir, "  "); err != nil || got != nil {
		t.Fatalf("LeadTaskThreads(empty) = (%+v, %v), want (nil, nil)", got, err)
	}
}

func TestCreateConversationThreadRequiresExistingSession(t *testing.T) {
	dir := t.TempDir()
	if err := UpsertParticipant(dir, participant.Participant{ID: testConversationThreadOwnerID, Kind: participant.KindNamed, Name: "Missing Session Owner"}); err != nil {
		t.Fatal(err)
	}
	_, err := CreateConversationThread(dir, ConversationThread{
		SessionID: "missing", AnchorItemID: "item-1", ParentSeq: 1,
		ParentAuthorParticipantID: testConversationThreadOwnerID, ThreadOwnerParticipantID: testConversationThreadOwnerID,
	})
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("CreateConversationThread() error = %v, want ErrSessionNotFound", err)
	}
}

func TestConversationThreadRejectsInvalidStatus(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	_, err := CreateConversationThread(dir, ConversationThread{
		SessionID:    "thread-1",
		AnchorItemID: "item-1",
		Status:       "paused",
	})
	if err == nil {
		t.Fatal("expected invalid status error")
	}
}

func TestCreateConversationThreadRequiresOpenAnchoredOwnedThread(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateWithMetadata(dir, "thread-1", "/tmp/project"); err != nil {
		t.Fatal(err)
	}
	owner := participant.Participant{ID: "prt-owner", Kind: participant.KindNamed, Name: "Owner"}
	if err := UpsertParticipant(dir, owner); err != nil {
		t.Fatal(err)
	}
	if err := AddThreadMember(dir, "thread-1", owner.ID); err != nil {
		t.Fatal(err)
	}
	valid := ConversationThread{
		SessionID: "thread-1", AnchorItemID: "item-1", ParentSeq: 1,
		ParentAuthorParticipantID: owner.ID, ThreadOwnerParticipantID: owner.ID,
	}

	task := valid
	task.Status = ConversationThreadTask
	if _, err := CreateConversationThread(dir, task); err == nil || !strings.Contains(err.Error(), "must be created open") {
		t.Fatalf("direct task create must be rejected, got %v", err)
	}
	missingAnchor := valid
	missingAnchor.ParentSeq = 0
	if _, err := CreateConversationThread(dir, missingAnchor); err == nil || !strings.Contains(err.Error(), "resolved parent message anchor") {
		t.Fatalf("unresolved anchor must be rejected, got %v", err)
	}
	missingOwner := valid
	missingOwner.ThreadOwnerParticipantID = ""
	if _, err := CreateConversationThread(dir, missingOwner); err == nil || !strings.Contains(err.Error(), "owner is required") {
		t.Fatalf("missing owner must be rejected, got %v", err)
	}
	inactiveOwner := valid
	inactiveOwner.ThreadOwnerParticipantID = "prt-ghost"
	if _, err := CreateConversationThread(dir, inactiveOwner); !errors.Is(err, ErrParticipantNotFound) {
		t.Fatalf("inactive owner must be rejected, got %v", err)
	}
}

func TestDeleteSessionRemovesConversationThreads(t *testing.T) {
	dir := t.TempDir()
	sess, err := CreateWithMetadata(dir, "thread-1", "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	createOpenConversationThreadForTest(t, dir, ConversationThread{SessionID: sess.ID, AnchorItemID: "item-1"})
	if _, err := Delete(dir, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ListConversationThreads(dir, sess.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ListConversationThreads() after delete = %v, want ErrSessionNotFound", err)
	}
}
