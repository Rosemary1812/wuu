package session

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ConversationThreadStatus string

const (
	ConversationThreadOpen ConversationThreadStatus = "open"
	ConversationThreadTask ConversationThreadStatus = "task"
	// ConversationThreadResolved is the terminal state: a discussion reply
	// closed by a human, or a task whose conclusion was filed
	// (ConcludeConversationThread). There is no intermediate approval state
	// between task and resolved — filing the conclusion IS the completion.
	ConversationThreadResolved ConversationThreadStatus = "resolved"
)

// Execution states of a task cth (the ExecState column) — a separate axis
// from the approval Status above. Status says what the thread is (open
// discussion / board task / resolved); ExecState says where the execution
// engine stands on the work: escalation enters planning, the lead's set_plan
// enters executing, and the engine enters awaiting_lead when every piece is
// done. Only the lead's verified conclusion lands completed. blocked /
// needs_human / failed are exception states. The empty string is the
// pre-execution zero value and is never explicitly set.
const (
	ExecStatePlanning     = "planning"
	ExecStateExecuting    = "executing"
	ExecStateAwaitingLead = "awaiting_lead"
	ExecStateBlocked      = "blocked"
	ExecStateNeedsHuman   = "needs_human"
	ExecStateCompleted    = "completed"
	ExecStateFailed       = "failed"
)

var (
	ErrConversationThreadNotFound   = errors.New("conversation thread not found")
	ErrConversationThreadAnchorUsed = errors.New("conversation thread anchor already used")
)

type ConversationThread struct {
	ID           string                   `json:"id"`
	SessionID    string                   `json:"session_id"`
	AnchorItemID string                   `json:"anchor_item_id"`
	Title        string                   `json:"title,omitempty"`
	Status       ConversationThreadStatus `json:"status"`
	CreatedBy    string                   `json:"created_by,omitempty"`
	CreatedAt    time.Time                `json:"created_at"`
	// ThreadOwnerParticipantID is the named agent responsible for converging this
	// Thread. It is assigned when the Thread is opened and becomes the task lead
	// if the same cth is promoted.
	ThreadOwnerParticipantID string `json:"thread_owner_participant_id,omitempty"`
	// EscalatedAt / EscalatedBy mark that this reply subthread was promoted to a
	// task (open -> task). EscalatedAt stays set even after the task
	// resolves (status -> resolved), so a resolved cth still carries "this was a
	// task" — a plain reply resolve leaves EscalatedAt zero. Summary is the
	// one-line conclusion bubbled back to the main stream when the task wraps up.
	EscalatedAt time.Time `json:"escalated_at,omitempty"`
	EscalatedBy string    `json:"escalated_by,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	// LeadParticipantID is the single named agent granted task-lead authority when
	// this Thread is promoted. It is copied from ThreadOwnerParticipantID in the
	// same transaction; callers cannot override it. The workflow orchestration
	// gate keys on (caller == LeadParticipantID && status == task). The field
	// survives resolve for history, while resolved Tasks cannot be reopened.
	LeadParticipantID string `json:"lead_participant_id,omitempty"`
	// Plan is the lead's declared work breakdown for a team task (task-rail
	// design §8, 2026-07-07): one task, one thread, a team, executed as a small
	// dependency graph. Each piece is assigned to a member and may depend on
	// other pieces; the engine (advancePlan) dispatches pieces whose deps are
	// done by @-waking the assignee, and wakes the lead when all are done. A
	// piece is medium-agnostic — "assignee does X, reports done" — so code,
	// research, and document work all ride the same engine. Empty for a plain
	// single-owner task.
	Plan []TaskPiece `json:"plan,omitempty"`
	// ExecState is the execution axis of a task, deliberately separate from
	// the approval axis (Status): planning -> executing -> awaiting_lead ->
	// completed on the normal path, with blocked / needs_human / failed as exception states
	// (see the ExecState* constants). Empty is the pre-execution zero value —
	// a thread that never entered execution. Written only through
	// SetConversationThreadExecState.
	ExecState string `json:"exec_state,omitempty"`
	// ParentSeq / ParentAuthorParticipantID bind a reply subthread to the exact
	// main-stream message it was opened from (T3 Thread-first-class): the
	// message's seq (its stable per-thread address in the parent's seq space)
	// and its author's participant id ("human" for a user / thread-owner
	// message). Distinct from AnchorItemID (a rendered GUI item id), these fields
	// preserve the durable parent binding and determine the initial Thread owner.
	// Legacy rows created before anchored Threads may leave them empty.
	ParentSeq                 int    `json:"parent_seq,omitempty"`
	ParentAuthorParticipantID string `json:"parent_author_participant_id,omitempty"`
}

// TaskPiece is one unit of a team task's plan — an executable node in the
// lead's dependency graph. Status moves pending -> active (deps satisfied,
// assignee woken); from active it lands in done (assignee reported it
// complete), blocked (assignee cannot proceed without lead input), failed
// (the last attempt died with no retry budget left), or retrying (a failed
// attempt is being re-dispatched against the remaining budget, then back to
// active).
type TaskPiece struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Assignee  string   `json:"assignee"`
	DependsOn []string `json:"depends_on,omitempty"`
	Status    string   `json:"status"`
	// Prompt is the lead-authored first-step briefing for this node — what
	// the assignee is woken with when the engine dispatches the piece.
	Prompt string `json:"prompt,omitempty"`
	// Handoff is the structured handoff payload JSON the upstream node handed
	// to this one. The store persists it verbatim; its shape belongs to the
	// handoff writer, not the plan.
	Handoff string `json:"handoff,omitempty"`
	// RetryBudget is how many attempts this node gets before failed becomes
	// terminal. Zero means unset: reads and writes normalize it to the
	// default (TaskPieceDefaultRetryBudget), so plans stored before nodes
	// carried a budget come back with 3. Attempts counts attempts made so far.
	RetryBudget int `json:"retry_budget,omitempty"`
	Attempts    int `json:"attempts,omitempty"`
	// CurrentAttemptID binds this node to the one queued/running durable attempt
	// whose dispatch envelope may complete it. Empty means no attempt owns it.
	CurrentAttemptID string `json:"current_attempt_id,omitempty"`
	// FailureReason is the most recent failure, kept for the lead's
	// post-mortem when it is woken on failure.
	FailureReason string `json:"failure_reason,omitempty"`
	// LastActivityAt / LastProgressAt are the node's liveness signals:
	// activity is any observable action by the assignee, progress is a
	// declared step forward. Stall detection compares the two relative to
	// each other instead of a fixed lease deadline.
	LastActivityAt time.Time `json:"last_activity_at,omitzero"`
	LastProgressAt time.Time `json:"last_progress_at,omitzero"`
}

const (
	TaskPiecePending  = "pending"
	TaskPieceActive   = "active"
	TaskPieceDone     = "done"
	TaskPieceBlocked  = "blocked"
	TaskPieceFailed   = "failed"
	TaskPieceRetrying = "retrying"
)

// TaskPieceDefaultRetryBudget is the retry budget a plan node gets when the
// lead did not declare one (and what a plan stored before nodes carried a
// budget reads back with).
const TaskPieceDefaultRetryBudget = 3

// TaskHandoff is the structured result one node hands to the next — the input
// the engine carries into a downstream node's wake. It is deliberately distinct
// from a public_thread_message (a post_message kind=update posted to the task
// thread, which is user-visible progress that wakes no teammate): the handoff is
// the machine-carried input to the next node, the public update is prose for the
// human. Never mix them. The upstream assignee fills it when reporting a piece
// done; the engine writes it onto the downstream node's TaskPiece.Handoff and
// renders it into that node's wake envelope.
type TaskHandoff struct {
	Done       string   `json:"done,omitempty"`
	Findings   string   `json:"findings,omitempty"`
	Artifacts  []string `json:"artifacts,omitempty"`
	Limits     string   `json:"limits,omitempty"`
	NextGoal   string   `json:"next_goal,omitempty"`
	Acceptance string   `json:"acceptance,omitempty"`
	Notes      string   `json:"notes,omitempty"`
}

// MarshalTaskHandoff serializes a handoff to the JSON stored verbatim in the
// downstream node's TaskPiece.Handoff (and recorded as the handoff_created
// trace payload).
func MarshalTaskHandoff(h TaskHandoff) (string, error) {
	data, err := json.Marshal(h)
	if err != nil {
		return "", fmt.Errorf("marshal task handoff: %w", err)
	}
	return string(data), nil
}

// RenderHandoffForWake renders a handoff as a compact, human-readable block for
// the downstream node's wake envelope — the input handed to this node. Empty
// fields (and empty artifact entries) are skipped, so a sparse handoff produces
// a short block.
func RenderHandoffForWake(h TaskHandoff) string {
	var b strings.Builder
	writeLine := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(label)
		b.WriteString(": ")
		b.WriteString(value)
	}
	writeLine("Done", h.Done)
	writeLine("Findings", h.Findings)
	artifacts := make([]string, 0, len(h.Artifacts))
	for _, a := range h.Artifacts {
		if a = strings.TrimSpace(a); a != "" {
			artifacts = append(artifacts, a)
		}
	}
	writeLine("Artifacts", strings.Join(artifacts, ", "))
	writeLine("Limits", h.Limits)
	writeLine("Goal for you", h.NextGoal)
	writeLine("Acceptance", h.Acceptance)
	writeLine("Notes", h.Notes)
	return b.String()
}

func NewConversationThreadID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "cth-" + hex.EncodeToString(b)
}

func CreateConversationThread(sessDir string, thread ConversationThread) (ConversationThread, error) {
	db, err := openStore(sessDir)
	if err != nil {
		return ConversationThread{}, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return ConversationThread{}, fmt.Errorf("begin conversation thread create: %w", err)
	}
	defer tx.Rollback()

	thread = normalizeConversationThread(thread)
	if thread.SessionID == "" {
		return ConversationThread{}, fmt.Errorf("%w: %q", ErrSessionNotFound, thread.SessionID)
	}
	if err := validateConversationThreadStatus(thread.Status); err != nil {
		return ConversationThread{}, err
	}
	if thread.Status != ConversationThreadOpen {
		return ConversationThread{}, fmt.Errorf("conversation thread must be created open; Tasks are promoted from an existing Thread")
	}
	if thread.AnchorItemID == "" || thread.ParentSeq <= 0 || thread.ParentAuthorParticipantID == "" {
		return ConversationThread{}, fmt.Errorf("conversation thread requires a resolved parent message anchor")
	}
	if thread.ThreadOwnerParticipantID == "" {
		return ConversationThread{}, fmt.Errorf("conversation thread owner is required")
	}
	exists, err := sessionExistsTx(tx, thread.SessionID)
	if err != nil {
		return ConversationThread{}, err
	}
	if !exists {
		return ConversationThread{}, fmt.Errorf("%w: %q", ErrSessionNotFound, thread.SessionID)
	}
	if err := requireActiveNamedThreadMember(tx, thread.SessionID, thread.ThreadOwnerParticipantID); err != nil {
		return ConversationThread{}, fmt.Errorf("conversation thread owner: %w", err)
	}
	if thread.AnchorItemID != "" {
		var existingID string
		err := tx.QueryRow(`
SELECT id FROM conversation_threads
WHERE session_id = ? AND anchor_item_id = ?
LIMIT 1`, thread.SessionID, thread.AnchorItemID).Scan(&existingID)
		switch {
		case err == nil:
			return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadAnchorUsed, existingID)
		case !errors.Is(err, sql.ErrNoRows):
			return ConversationThread{}, fmt.Errorf("check conversation thread anchor: %w", err)
		}
	}
	if thread.ID == "" {
		thread.ID = NewConversationThreadID()
	}
	if thread.CreatedAt.IsZero() {
		thread.CreatedAt = time.Now().UTC()
	}

	if _, err := tx.Exec(`
INSERT INTO conversation_threads (
	id, session_id, anchor_item_id, title, status, created_by, created_at,
	thread_owner_participant_id, escalated_at, escalated_by,
	parent_seq, parent_author_participant_id, lead_participant_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		thread.ID, thread.SessionID, thread.AnchorItemID, thread.Title, string(thread.Status), thread.CreatedBy, timeText(thread.CreatedAt),
		thread.ThreadOwnerParticipantID, timeText(thread.EscalatedAt), thread.EscalatedBy,
		thread.ParentSeq, thread.ParentAuthorParticipantID, thread.LeadParticipantID,
	); err != nil {
		return ConversationThread{}, fmt.Errorf("create conversation thread: %w", err)
	}
	if _, err := tx.Exec(`
INSERT INTO conversation_thread_members (conversation_thread_id, participant_id, joined_at)
VALUES (?, ?, ?)
ON CONFLICT(conversation_thread_id, participant_id) DO NOTHING`,
		thread.ID, thread.ThreadOwnerParticipantID, thread.CreatedAt.UnixMilli(),
	); err != nil {
		return ConversationThread{}, fmt.Errorf("add conversation thread owner: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ConversationThread{}, fmt.Errorf("commit conversation thread create: %w", err)
	}
	return thread, nil
}

func ListConversationThreads(sessDir, sessionID string) ([]ConversationThread, error) {
	sessionID = strings.TrimSpace(sessionID)
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	exists, err := sessionExistsDB(db, sessionID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%w: %q", ErrSessionNotFound, sessionID)
	}

	rows, err := db.Query(`
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at, thread_owner_participant_id, escalated_at, escalated_by, summary, lead_participant_id, plan, exec_state, parent_seq, parent_author_participant_id
FROM conversation_threads
WHERE session_id = ?
ORDER BY created_at ASC, id ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list conversation threads: %w", err)
	}
	defer rows.Close()

	var threads []ConversationThread
	for rows.Next() {
		thread, err := scanConversationThread(rows)
		if err != nil {
			return nil, fmt.Errorf("scan conversation threads: %w", err)
		}
		threads = append(threads, thread)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan conversation threads: %w", err)
	}
	return threads, nil
}

// FindConversationThreadByID loads a single conversation subthread by its id
// (cth-*). It returns ErrConversationThreadNotFound when no row matches. Unlike
// findConversationSubthread (which scans a parent session's threads by anchor),
// this resolves a subthread id directly to its record — including SessionID, the
// parent (group) thread the subthread hangs off of.
func FindConversationThreadByID(sessDir, id string) (ConversationThread, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	db, err := openStore(sessDir)
	if err != nil {
		return ConversationThread{}, err
	}
	defer db.Close()

	row := db.QueryRow(`
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at, thread_owner_participant_id, escalated_at, escalated_by, summary, lead_participant_id, plan, exec_state, parent_seq, parent_author_participant_id
FROM conversation_threads
WHERE id = ?`, id)
	thread, err := scanConversationThread(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
		}
		return ConversationThread{}, fmt.Errorf("find conversation thread: %w", err)
	}
	return thread, nil
}

// LeadTaskThreads returns the active task subthreads (status == task) whose
// LeadParticipantID is the given named agent, most-recently-escalated first. It
// backs the execute-time task-orchestration gate: a named agent may drive
// task work only while it holds task-lead authority on at least one active
// task. The lookup is by lead identity alone — deliberately independent of which
// group/DM thread the caller's turn happens to run in — because a resident named
// agent drains its inbox in its own DM thread while the task it leads lives under
// the parent group. Returns an empty slice when the agent leads no active task.
func LeadTaskThreads(sessDir, leadParticipantID string) ([]ConversationThread, error) {
	leadParticipantID = strings.TrimSpace(leadParticipantID)
	if leadParticipantID == "" {
		return nil, nil
	}
	db, err := openStore(sessDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at, thread_owner_participant_id, escalated_at, escalated_by, summary, lead_participant_id, plan, exec_state, parent_seq, parent_author_participant_id
FROM conversation_threads
WHERE status = ? AND lead_participant_id = ?
ORDER BY escalated_at DESC, id ASC`, string(ConversationThreadTask), leadParticipantID)
	if err != nil {
		return nil, fmt.Errorf("list lead task threads: %w", err)
	}
	defer rows.Close()

	var threads []ConversationThread
	for rows.Next() {
		thread, err := scanConversationThread(rows)
		if err != nil {
			return nil, fmt.Errorf("scan lead task threads: %w", err)
		}
		threads = append(threads, thread)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan lead task threads: %w", err)
	}
	return threads, nil
}

func UpdateConversationThreadStatus(sessDir, id string, status ConversationThreadStatus) error {
	id = strings.TrimSpace(id)
	status = normalizeConversationThreadStatus(status)
	if err := validateConversationThreadStatus(status); err != nil {
		return err
	}
	if status == ConversationThreadTask {
		return errors.New("conversation thread must be promoted to task through EscalateConversationThread")
	}

	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	current, err := findConversationThreadByIDDB(db, id)
	if err != nil {
		return err
	}
	if current.Status == ConversationThreadTask {
		return fmt.Errorf("conversation thread %q is an active task; only its lead may conclude it", id)
	}
	if current.Status == ConversationThreadResolved {
		return fmt.Errorf("conversation thread %q is resolved and cannot be reopened", id)
	}
	res, err := db.Exec(`
UPDATE conversation_threads
SET status = ?
WHERE id = ?`, string(status), id)
	if err != nil {
		return fmt.Errorf("update conversation thread status: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update conversation thread status: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	return nil
}

func normalizeConversationThread(thread ConversationThread) ConversationThread {
	thread.ID = strings.TrimSpace(thread.ID)
	thread.SessionID = strings.TrimSpace(thread.SessionID)
	thread.AnchorItemID = strings.TrimSpace(thread.AnchorItemID)
	thread.Title = strings.TrimSpace(thread.Title)
	thread.CreatedBy = strings.TrimSpace(thread.CreatedBy)
	thread.ThreadOwnerParticipantID = strings.TrimSpace(thread.ThreadOwnerParticipantID)
	thread.LeadParticipantID = strings.TrimSpace(thread.LeadParticipantID)
	thread.ParentAuthorParticipantID = strings.TrimSpace(thread.ParentAuthorParticipantID)
	thread.Status = normalizeConversationThreadStatus(thread.Status)
	return thread
}

func normalizeConversationThreadStatus(status ConversationThreadStatus) ConversationThreadStatus {
	switch ConversationThreadStatus(strings.ToLower(strings.TrimSpace(string(status)))) {
	case "":
		return ConversationThreadOpen
	case ConversationThreadOpen:
		return ConversationThreadOpen
	case ConversationThreadTask:
		return ConversationThreadTask
	case ConversationThreadResolved:
		return ConversationThreadResolved
	default:
		return ConversationThreadStatus(strings.ToLower(strings.TrimSpace(string(status))))
	}
}

func validateConversationThreadStatus(status ConversationThreadStatus) error {
	switch status {
	case ConversationThreadOpen, ConversationThreadTask, ConversationThreadResolved:
		return nil
	default:
		return fmt.Errorf("invalid conversation thread status %q", status)
	}
}

// EscalateConversationThread atomically promotes one open Thread into a task on
// the same cth identity. The Thread owner becomes the immutable task lead and
// execution enters planning in the same transaction; callers cannot override
// the lead or reopen task/resolved rows through this path.
func EscalateConversationThread(sessDir, id, escalatedBy, title string) (ConversationThread, error) {
	id = strings.TrimSpace(id)
	escalatedBy = strings.TrimSpace(escalatedBy)
	title = strings.TrimSpace(title)
	if id == "" {
		return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}

	db, err := openStore(sessDir)
	if err != nil {
		return ConversationThread{}, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return ConversationThread{}, fmt.Errorf("begin conversation thread escalate: %w", err)
	}
	defer tx.Rollback()

	row := tx.QueryRow(`
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at, thread_owner_participant_id, escalated_at, escalated_by, summary, lead_participant_id, plan, exec_state, parent_seq, parent_author_participant_id
FROM conversation_threads
WHERE id = ?`, id)
	thread, err := scanConversationThread(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
		}
		return ConversationThread{}, fmt.Errorf("find conversation thread: %w", err)
	}

	if thread.Status != ConversationThreadOpen {
		return ConversationThread{}, fmt.Errorf("escalate conversation thread %q: status is %q; only an open thread can be promoted", id, thread.Status)
	}
	if thread.ThreadOwnerParticipantID == "" {
		return ConversationThread{}, fmt.Errorf("escalate conversation thread %q: thread owner is missing", id)
	}
	if err := requireActiveNamedThreadMember(tx, thread.SessionID, thread.ThreadOwnerParticipantID); err != nil {
		return ConversationThread{}, fmt.Errorf("escalate conversation thread %q: thread owner: %w", id, err)
	}

	thread.Status = ConversationThreadTask
	thread.EscalatedAt = time.Now().UTC()
	thread.EscalatedBy = escalatedBy
	thread.LeadParticipantID = thread.ThreadOwnerParticipantID
	thread.ExecState = ExecStatePlanning
	if title != "" {
		thread.Title = title
	}

	res, err := tx.Exec(`
UPDATE conversation_threads
SET status = ?, title = ?, escalated_at = ?, escalated_by = ?, lead_participant_id = ?, exec_state = ?
WHERE id = ? AND status = ?`,
		string(thread.Status), thread.Title, timeText(thread.EscalatedAt), thread.EscalatedBy,
		thread.LeadParticipantID, thread.ExecState, id, string(ConversationThreadOpen),
	)
	if err != nil {
		return ConversationThread{}, fmt.Errorf("escalate conversation thread: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return ConversationThread{}, fmt.Errorf("escalate conversation thread: %w", err)
	}
	if affected != 1 {
		return ConversationThread{}, fmt.Errorf("escalate conversation thread %q: open-to-task transition lost its CAS", id)
	}
	if _, err := tx.Exec(`
INSERT INTO conversation_thread_members (conversation_thread_id, participant_id, joined_at)
VALUES (?, ?, ?)
ON CONFLICT(conversation_thread_id, participant_id) DO NOTHING`,
		thread.ID, thread.LeadParticipantID, thread.EscalatedAt.UnixMilli(),
	); err != nil {
		return ConversationThread{}, fmt.Errorf("add task lead to conversation thread: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ConversationThread{}, fmt.Errorf("commit conversation thread escalate: %w", err)
	}
	return thread, nil
}

// ConcludeConversationThread files a task's conclusion and resolves it in one
// CAS: status task -> resolved with the summary stored. Filing the conclusion
// IS the completion — there is no review gate waiting for a human click. The
// task lead may conclude, and only after every planned worker node is done.
// CAS loss is diagnosed loudly: the status was not task, the caller is not the
// lead, or execution has not reached awaiting_lead.
func ConcludeConversationThread(sessDir, id, participantID, summary string) (ConversationThread, error) {
	id = strings.TrimSpace(id)
	participantID = strings.TrimSpace(participantID)
	summary = strings.TrimSpace(summary)
	if id == "" {
		return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	if participantID == "" {
		return ConversationThread{}, errors.New("conclude conversation thread: participant id is required")
	}
	if summary == "" {
		return ConversationThread{}, errors.New("conclude conversation thread: summary is required")
	}

	db, err := openStore(sessDir)
	if err != nil {
		return ConversationThread{}, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	thread, err := findConversationThreadByIDDB(db, id)
	if err != nil {
		return ConversationThread{}, err
	}
	switch {
	case thread.Status != ConversationThreadTask:
		return thread, fmt.Errorf("conclude conversation thread %q: status is %q, not task", id, thread.Status)
	case thread.LeadParticipantID != participantID:
		return thread, fmt.Errorf("conclude conversation thread %q: lead is %q — only the lead may file the conclusion", id, thread.LeadParticipantID)
	case len(thread.Plan) == 0:
		return thread, fmt.Errorf("conclude conversation thread %q: task has no worker plan", id)
	case thread.ExecState != ExecStateAwaitingLead:
		return thread, fmt.Errorf("conclude conversation thread %q: execution is %q, not %q", id, thread.ExecState, ExecStateAwaitingLead)
	}
	for _, piece := range thread.Plan {
		if piece.Status != TaskPieceDone {
			return thread, fmt.Errorf("conclude conversation thread %q: piece %q is still %q", id, piece.ID, piece.Status)
		}
	}

	res, err := db.Exec(`
UPDATE conversation_threads
SET status = ?, summary = ?, exec_state = ?
WHERE id = ? AND status = ? AND lead_participant_id = ? AND exec_state = ?`,
		string(ConversationThreadResolved), summary, ExecStateCompleted,
		id, string(ConversationThreadTask), participantID, ExecStateAwaitingLead)
	if err != nil {
		return ConversationThread{}, fmt.Errorf("conclude conversation thread: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return ConversationThread{}, fmt.Errorf("conclude conversation thread: %w", err)
	}
	thread, findErr := findConversationThreadByIDDB(db, id)
	if findErr != nil {
		return ConversationThread{}, findErr
	}
	if affected == 0 {
		switch {
		case thread.Status != ConversationThreadTask:
			return thread, fmt.Errorf("conclude conversation thread %q: status is %q, not task", id, thread.Status)
		case thread.ExecState != ExecStateAwaitingLead:
			return thread, fmt.Errorf("conclude conversation thread %q: execution changed to %q before conclusion", id, thread.ExecState)
		default:
			return thread, fmt.Errorf("conclude conversation thread %q: transition refused", id)
		}
	}
	return thread, nil
}

// SetConversationThreadExecState writes the task's execution state (the
// ExecState axis — see the ExecState* constants). The vocabulary is closed:
// an unknown state is a loud error, never silently stored. The empty string
// is not settable — it is the pre-execution zero value only.
func SetConversationThreadExecState(sessDir, id, state string) error {
	id = strings.TrimSpace(id)
	state = strings.TrimSpace(state)
	if id == "" {
		return fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	if !validConversationThreadExecState(state) {
		return fmt.Errorf("invalid conversation thread exec state %q", state)
	}

	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	res, err := db.Exec(`
UPDATE conversation_threads
SET exec_state = ?
WHERE id = ?`, state, id)
	if err != nil {
		return fmt.Errorf("set conversation thread exec state: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set conversation thread exec state: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	return nil
}

// TransitionConversationThreadExecState performs an exact state transition on
// a live Task. It returns true only for the caller that won the transition,
// which makes one-shot side effects such as waking the lead safe under races.
func TransitionConversationThreadExecState(sessDir, id, from, to string) (bool, error) {
	id = strings.TrimSpace(id)
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if id == "" {
		return false, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	if !validConversationThreadExecState(from) || !validConversationThreadExecState(to) {
		return false, fmt.Errorf("invalid conversation thread exec transition %q -> %q", from, to)
	}
	db, err := openStore(sessDir)
	if err != nil {
		return false, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	res, err := db.Exec(`
UPDATE conversation_threads
SET exec_state = ?
WHERE id = ? AND status = ? AND exec_state = ?`,
		to, id, string(ConversationThreadTask), from)
	if err != nil {
		return false, fmt.Errorf("transition conversation thread exec state: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("transition conversation thread exec state: %w", err)
	}
	if affected == 1 {
		return true, nil
	}
	if _, err := findConversationThreadByIDDB(db, id); err != nil {
		return false, err
	}
	return false, nil
}

func validConversationThreadExecState(state string) bool {
	switch state {
	case ExecStatePlanning, ExecStateExecuting, ExecStateAwaitingLead,
		ExecStateBlocked, ExecStateNeedsHuman, ExecStateCompleted, ExecStateFailed:
		return true
	default:
		return false
	}
}

// findConversationThreadByIDDB is FindConversationThreadByID against an
// already-open handle (for use under storeWriteMu).
func findConversationThreadByIDDB(db *sql.DB, id string) (ConversationThread, error) {
	row := db.QueryRow(`
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at, thread_owner_participant_id, escalated_at, escalated_by, summary, lead_participant_id, plan, exec_state, parent_seq, parent_author_participant_id
FROM conversation_threads
WHERE id = ?`, id)
	thread, err := scanConversationThread(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
		}
		return ConversationThread{}, fmt.Errorf("find conversation thread: %w", err)
	}
	return thread, nil
}

// SetConversationThreadSummary stores the one-line conclusion for a subthread
// (the same text bubbled back to the main stream). It only writes the summary
// column; status transitions go through UpdateConversationThreadStatus so the
// two concerns stay composable (resolve = set summary + set status resolved).
func SetConversationThreadSummary(sessDir, id, summary string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}

	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	res, err := db.Exec(`
UPDATE conversation_threads
SET summary = ?
WHERE id = ?`, strings.TrimSpace(summary), id)
	if err != nil {
		return fmt.Errorf("update conversation thread summary: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update conversation thread summary: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	return nil
}

func scanConversationThread(scanner interface {
	Scan(dest ...any) error
}) (ConversationThread, error) {
	var thread ConversationThread
	var status, createdAt, escalatedAt, planJSON string
	if err := scanner.Scan(
		&thread.ID, &thread.SessionID, &thread.AnchorItemID, &thread.Title, &status, &thread.CreatedBy, &createdAt,
		&thread.ThreadOwnerParticipantID, &escalatedAt, &thread.EscalatedBy, &thread.Summary, &thread.LeadParticipantID, &planJSON,
		&thread.ExecState, &thread.ParentSeq, &thread.ParentAuthorParticipantID,
	); err != nil {
		return ConversationThread{}, err
	}
	thread.Status = ConversationThreadStatus(status)
	thread.CreatedAt = parseTime(createdAt)
	thread.EscalatedAt = parseTime(escalatedAt)
	if strings.TrimSpace(planJSON) != "" {
		if err := json.Unmarshal([]byte(planJSON), &thread.Plan); err != nil {
			return ConversationThread{}, fmt.Errorf("decode task plan for %q: %w", thread.ID, err)
		}
		normalizeTaskPieces(thread.Plan)
	}
	return thread, nil
}

// normalizeTaskPieces is the one place plan-node defaults live: an empty
// Status becomes pending, and a zero RetryBudget (a plan stored before nodes
// carried a budget, or a lead that left it unset) becomes the default. It runs
// on every read (scanConversationThread) so legacy rows come back normalized,
// and on the write path so newly stored plans carry the values explicitly.
func normalizeTaskPieces(plan []TaskPiece) {
	for i := range plan {
		if strings.TrimSpace(plan[i].Status) == "" {
			plan[i].Status = TaskPiecePending
		}
		if plan[i].RetryBudget == 0 {
			plan[i].RetryBudget = TaskPieceDefaultRetryBudget
		}
	}
}

// SetConversationThreadPlan replaces the task's declared plan (task-rail
// design §8). Pieces are normalized before storage (empty status -> pending,
// zero retry budget -> default), so persisted plans carry explicit values. The
// caller (the lead's set_plan action) owns validation of assignees/deps; this
// just persists the breakdown so the engine can advance it.
func SetConversationThreadPlan(sessDir, id string, plan []TaskPiece) (ConversationThread, error) {
	id = strings.TrimSpace(id)
	normalizeTaskPieces(plan)
	data, err := json.Marshal(plan)
	if err != nil {
		return ConversationThread{}, fmt.Errorf("encode task plan: %w", err)
	}
	db, err := openStore(sessDir)
	if err != nil {
		return ConversationThread{}, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	if _, err := db.Exec(`UPDATE conversation_threads SET plan = ? WHERE id = ?`, string(data), id); err != nil {
		return ConversationThread{}, fmt.Errorf("set task plan: %w", err)
	}
	return findConversationThreadByIDDB(db, id)
}

// MarkTaskPieceStatus sets one piece's status inside the task's plan and
// returns the updated thread. Used by the engine (active on dispatch) and by
// the assignee's piece_done action. Errors if the piece id is not in the plan.
func MarkTaskPieceStatus(sessDir, id, pieceID, status string) (ConversationThread, error) {
	return UpdateTaskPiece(sessDir, id, pieceID, func(piece *TaskPiece) {
		piece.Status = status
	})
}

// UpdateTaskPiece loads the task's plan, applies mutate to the piece with the
// given id, persists the updated plan, and returns the updated thread. It is
// the engine's single write path for node execution state — status, attempts,
// handoff, failure reason, activity/progress timestamps — so every node
// mutation rides the same read-mutate-persist cycle under the store write
// lock. Errors if the piece id is not in the plan.
func UpdateTaskPiece(sessDir, id, pieceID string, mutate func(*TaskPiece)) (ConversationThread, error) {
	id = strings.TrimSpace(id)
	pieceID = strings.TrimSpace(pieceID)
	db, err := openStore(sessDir)
	if err != nil {
		return ConversationThread{}, err
	}
	defer db.Close()
	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()
	thread, err := findConversationThreadByIDDB(db, id)
	if err != nil {
		return ConversationThread{}, err
	}
	found := false
	for i := range thread.Plan {
		if thread.Plan[i].ID == pieceID {
			mutate(&thread.Plan[i])
			found = true
			break
		}
	}
	if !found {
		return ConversationThread{}, fmt.Errorf("task %q has no piece %q", id, pieceID)
	}
	data, err := json.Marshal(thread.Plan)
	if err != nil {
		return ConversationThread{}, fmt.Errorf("encode task plan: %w", err)
	}
	if _, err := db.Exec(`UPDATE conversation_threads SET plan = ? WHERE id = ?`, string(data), id); err != nil {
		return ConversationThread{}, fmt.Errorf("update task piece: %w", err)
	}
	return thread, nil
}
