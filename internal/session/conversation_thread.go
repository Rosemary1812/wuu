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
// enters executing, the engine lands completed when every piece is done.
// blocked / needs_human / failed are the exception states. The empty string
// is the pre-execution zero value and is never explicitly set: a thread that
// never entered execution (a plain reply) simply stays empty.
const (
	ExecStatePlanning   = "planning"
	ExecStateExecuting  = "executing"
	ExecStateBlocked    = "blocked"
	ExecStateNeedsHuman = "needs_human"
	ExecStateCompleted  = "completed"
	ExecStateFailed     = "failed"
)

var ErrConversationThreadNotFound = errors.New("conversation thread not found")

type ConversationThread struct {
	ID           string                   `json:"id"`
	SessionID    string                   `json:"session_id"`
	AnchorItemID string                   `json:"anchor_item_id"`
	Title        string                   `json:"title,omitempty"`
	Status       ConversationThreadStatus `json:"status"`
	CreatedBy    string                   `json:"created_by,omitempty"`
	CreatedAt    time.Time                `json:"created_at"`
	// EscalatedAt / EscalatedBy mark that this reply subthread was promoted to a
	// task by a human (open -> task). EscalatedAt stays set even after the task
	// resolves (status -> resolved), so a resolved cth still carries "this was a
	// task" — a plain reply resolve leaves EscalatedAt zero. Summary is the
	// one-line conclusion bubbled back to the main stream when the task wraps up.
	EscalatedAt time.Time `json:"escalated_at,omitempty"`
	EscalatedBy string    `json:"escalated_by,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	// LeadParticipantID is the single named agent granted task-lead authority when
	// this reply was escalated to a task: the workflow orchestration gate keys on
	// (caller == LeadParticipantID && status == task). It is distinct from
	// EscalatedBy (the human-click provenance, which is never a named agent): the
	// lead is either the named escalator or the named member the human picked. The
	// field survives the resolve transition — reclaim of lead authority happens via
	// status -> resolved (the gate requires status == task), not by nulling it —
	// and a re-escalation (resolved -> task) can reassign it.
	LeadParticipantID string `json:"lead_participant_id,omitempty"`
	// OwnerParticipantID is the named agent currently owning this task's work:
	// mutual exclusion (one owner at a time, taken via ClaimConversationThread's
	// CAS), reporting duty, and the right to conclude the task. Ownership is NOT
	// lead authority — claiming never touches LeadParticipantID and grants no
	// workflow orchestration (2026-07-06 agent-task-rail design, user-adjudicated).
	// Empty means unclaimed.
	OwnerParticipantID string `json:"owner_participant_id,omitempty"`
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
	// the approval axis (Status): planning -> executing -> completed on the
	// normal path, with blocked / needs_human / failed as exception states
	// (see the ExecState* constants). Empty is the pre-execution zero value —
	// a thread that never entered execution. Written only through
	// SetConversationThreadExecState.
	ExecState string `json:"exec_state,omitempty"`
	// ParentSeq / ParentAuthorParticipantID bind a reply subthread to the exact
	// main-stream message it was opened from (T3 Thread-first-class): the
	// message's seq (its stable per-thread address in the parent's seq space)
	// and its author's participant id ("human" for a user / thread-owner
	// message). Distinct from AnchorItemID (a rendered GUI item id): these are
	// the durable binding the escalation lead default (lead = parent author) and
	// the reply-to-your-own-message refusal (initiator == parent author) key on.
	// Zero/empty for a standalone task (no anchor) or an anchor that resolved to
	// no message. Persisted at create time by CreateConversationThread.
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
	// A discussion reply must anchor on a message; a task may be standalone
	// (created from scratch, e.g. splitting work into several tasks — one
	// anchor can host at most one cth, so splits cannot all anchor the same
	// kickoff message).
	if thread.AnchorItemID == "" && thread.Status != ConversationThreadTask {
		return ConversationThread{}, fmt.Errorf("conversation thread anchor item is required")
	}
	if err := validateConversationThreadStatus(thread.Status); err != nil {
		return ConversationThread{}, err
	}
	exists, err := sessionExistsTx(tx, thread.SessionID)
	if err != nil {
		return ConversationThread{}, err
	}
	if !exists {
		return ConversationThread{}, fmt.Errorf("%w: %q", ErrSessionNotFound, thread.SessionID)
	}
	if thread.ID == "" {
		thread.ID = NewConversationThreadID()
	}
	if thread.CreatedAt.IsZero() {
		thread.CreatedAt = time.Now().UTC()
	}

	if _, err := tx.Exec(`
INSERT INTO conversation_threads (
	id, session_id, anchor_item_id, title, status, created_by, created_at, parent_seq, parent_author_participant_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		thread.ID, thread.SessionID, thread.AnchorItemID, thread.Title, string(thread.Status), thread.CreatedBy, timeText(thread.CreatedAt), thread.ParentSeq, thread.ParentAuthorParticipantID,
	); err != nil {
		return ConversationThread{}, fmt.Errorf("create conversation thread: %w", err)
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
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at, escalated_at, escalated_by, summary, lead_participant_id, owner_participant_id, plan, exec_state, parent_seq, parent_author_participant_id
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
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at, escalated_at, escalated_by, summary, lead_participant_id, owner_participant_id, plan, exec_state, parent_seq, parent_author_participant_id
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
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at, escalated_at, escalated_by, summary, lead_participant_id, owner_participant_id, plan, exec_state, parent_seq, parent_author_participant_id
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

	db, err := openStore(sessDir)
	if err != nil {
		return err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

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
	thread.LeadParticipantID = strings.TrimSpace(thread.LeadParticipantID)
	thread.OwnerParticipantID = strings.TrimSpace(thread.OwnerParticipantID)
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

// EscalateConversationThread promotes a reply subthread to a task: it advances
// the status from the discussion state (open) to the execution state (task) and
// records who escalated it, who leads it, plus the escalation time. It is
// idempotent — calling it on an already-escalated (task) thread just refreshes
// escalated_by/lead/title (each with overwrite-if-non-empty semantics) and leaves
// escalated_at pinned to the first escalation. A resolved thread is re-opened into
// the task state so the human can re-run it, and the lead can be reassigned then.
// Reached by the human-click RPC (which may grant a lead) and by manage_task
// action=escalate (agent path, always empty lead). escalatedBy records
// provenance; leadParticipantID is the single named agent recorded as the
// task's lead.
func EscalateConversationThread(sessDir, id, escalatedBy, leadParticipantID, title string) (ConversationThread, error) {
	id = strings.TrimSpace(id)
	escalatedBy = strings.TrimSpace(escalatedBy)
	leadParticipantID = strings.TrimSpace(leadParticipantID)
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
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at, escalated_at, escalated_by, summary, lead_participant_id, owner_participant_id, plan, exec_state, parent_seq, parent_author_participant_id
FROM conversation_threads
WHERE id = ?`, id)
	thread, err := scanConversationThread(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
		}
		return ConversationThread{}, fmt.Errorf("find conversation thread: %w", err)
	}

	thread.Status = ConversationThreadTask
	if thread.EscalatedAt.IsZero() {
		thread.EscalatedAt = time.Now().UTC()
	}
	if escalatedBy != "" {
		thread.EscalatedBy = escalatedBy
	}
	if leadParticipantID != "" {
		thread.LeadParticipantID = leadParticipantID
	}
	if title != "" {
		thread.Title = title
	}

	if _, err := tx.Exec(`
UPDATE conversation_threads
SET status = ?, title = ?, escalated_at = ?, escalated_by = ?, lead_participant_id = ?
WHERE id = ?`,
		string(thread.Status), thread.Title, timeText(thread.EscalatedAt), thread.EscalatedBy, thread.LeadParticipantID, id,
	); err != nil {
		return ConversationThread{}, fmt.Errorf("escalate conversation thread: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ConversationThread{}, fmt.Errorf("commit conversation thread escalate: %w", err)
	}
	return thread, nil
}

// ClaimConversationThread atomically takes work ownership of a task subthread:
// a single CAS that sets owner_participant_id to participantID only when the
// task is unowned. Losing the race is a normal outcome, not an error — it
// returns claimed=false with the current row so the caller can report who owns
// it. Claiming is refused (as an error) when the subthread does not exist, is
// not in the task status (discussion replies stay discussion until a human
// escalates), or is already owned by the caller (claim is not a refresh).
// Ownership grants no lead/workflow authority (agent-task-rail design).
func ClaimConversationThread(sessDir, id, participantID string) (ConversationThread, bool, error) {
	id = strings.TrimSpace(id)
	participantID = strings.TrimSpace(participantID)
	if id == "" {
		return ConversationThread{}, false, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	if participantID == "" {
		return ConversationThread{}, false, errors.New("claim conversation thread: participant id is required")
	}

	db, err := openStore(sessDir)
	if err != nil {
		return ConversationThread{}, false, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	res, err := db.Exec(`
UPDATE conversation_threads
SET owner_participant_id = ?
WHERE id = ? AND status = ? AND owner_participant_id = ''`,
		participantID, id, string(ConversationThreadTask))
	if err != nil {
		return ConversationThread{}, false, fmt.Errorf("claim conversation thread: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return ConversationThread{}, false, fmt.Errorf("claim conversation thread: %w", err)
	}

	thread, findErr := findConversationThreadByIDDB(db, id)
	if findErr != nil {
		return ConversationThread{}, false, findErr
	}
	if affected == 1 {
		return thread, true, nil
	}
	// CAS lost: explain exactly why, loudly, instead of guessing.
	switch {
	case thread.Status != ConversationThreadTask:
		return thread, false, fmt.Errorf("claim conversation thread %q: status is %q, only a task can be claimed", id, thread.Status)
	case thread.OwnerParticipantID == participantID:
		return thread, false, fmt.Errorf("claim conversation thread %q: already owned by you", id)
	default:
		return thread, false, nil
	}
}

// SetConversationThreadLeadIfEmpty atomically claims task-lead (orchestration)
// authority on a task subthread: a single CAS that sets lead_participant_id to
// participantID only when the task currently has no lead. becameLead reports
// whether this call won the claim (rows affected == 1). Losing the CAS is a
// normal outcome, not an error — it returns the current thread with
// becameLead=false so the caller can read who already leads and defer to them.
// It is the atomic counterpart to a human-granted lead: an agent-created
// standalone task is born leadless, and the first board member to plan it takes
// the lead here. Claiming is refused (as a loud error) only when the subthread
// does not exist or is not in the task status — only a live task has an
// orchestration lead to hold. Lead authority, unlike ownership, is not released
// while the task runs.
func SetConversationThreadLeadIfEmpty(sessDir, id, participantID string) (ConversationThread, bool, error) {
	id = strings.TrimSpace(id)
	participantID = strings.TrimSpace(participantID)
	if id == "" {
		return ConversationThread{}, false, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	if participantID == "" {
		return ConversationThread{}, false, errors.New("set conversation thread lead: participant id is required")
	}

	db, err := openStore(sessDir)
	if err != nil {
		return ConversationThread{}, false, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	res, err := db.Exec(`
UPDATE conversation_threads
SET lead_participant_id = ?
WHERE id = ? AND status = ? AND lead_participant_id = ''`,
		participantID, id, string(ConversationThreadTask))
	if err != nil {
		return ConversationThread{}, false, fmt.Errorf("set conversation thread lead: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return ConversationThread{}, false, fmt.Errorf("set conversation thread lead: %w", err)
	}

	thread, findErr := findConversationThreadByIDDB(db, id)
	if findErr != nil {
		return ConversationThread{}, false, findErr
	}
	if affected == 1 {
		return thread, true, nil
	}
	// CAS lost. A non-task status is a loud error (a discussion reply or a
	// resolved task has no orchestration lead to claim); a task that already
	// carries a lead is a normal race loss — return it so the caller reads who
	// leads and refuses accordingly.
	if thread.Status != ConversationThreadTask {
		return thread, false, fmt.Errorf("set conversation thread lead %q: status is %q, only a task has an orchestration lead", id, thread.Status)
	}
	return thread, false, nil
}

// UnclaimConversationThread releases work ownership. Only the current owner
// can release; the status stays task so the work is immediately claimable by
// someone else.
func UnclaimConversationThread(sessDir, id, participantID string) (ConversationThread, error) {
	id = strings.TrimSpace(id)
	participantID = strings.TrimSpace(participantID)
	if id == "" {
		return ConversationThread{}, fmt.Errorf("%w: %q", ErrConversationThreadNotFound, id)
	}
	if participantID == "" {
		return ConversationThread{}, errors.New("unclaim conversation thread: participant id is required")
	}

	db, err := openStore(sessDir)
	if err != nil {
		return ConversationThread{}, err
	}
	defer db.Close()

	storeWriteMu.Lock()
	defer storeWriteMu.Unlock()

	res, err := db.Exec(`
UPDATE conversation_threads
SET owner_participant_id = ''
WHERE id = ? AND status = ? AND owner_participant_id = ?`,
		id, string(ConversationThreadTask), participantID)
	if err != nil {
		return ConversationThread{}, fmt.Errorf("unclaim conversation thread: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return ConversationThread{}, fmt.Errorf("unclaim conversation thread: %w", err)
	}
	thread, findErr := findConversationThreadByIDDB(db, id)
	if findErr != nil {
		return ConversationThread{}, findErr
	}
	if affected == 0 {
		switch {
		case thread.Status != ConversationThreadTask:
			return thread, fmt.Errorf("unclaim conversation thread %q: status is %q, not task", id, thread.Status)
		case thread.OwnerParticipantID != participantID:
			return thread, fmt.Errorf("unclaim conversation thread %q: owned by %q, not you", id, thread.OwnerParticipantID)
		default:
			return thread, fmt.Errorf("unclaim conversation thread %q: not owned", id)
		}
	}
	return thread, nil
}

// ConcludeConversationThread files a task's conclusion and resolves it in one
// CAS: status task -> resolved with the summary stored. Filing the conclusion
// IS the completion — there is no review gate waiting for a human click. The
// owner OR the lead may conclude (an unclaimed plan-task is concluded by its
// lead; a plain owned task by its owner). CAS loss is diagnosed loudly: the
// status was not task, or the caller is neither owner nor lead.
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

	res, err := db.Exec(`
UPDATE conversation_threads
SET status = ?, summary = ?
WHERE id = ? AND status = ? AND (owner_participant_id = ? OR lead_participant_id = ?)`,
		string(ConversationThreadResolved), summary, id, string(ConversationThreadTask), participantID, participantID)
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
		case thread.OwnerParticipantID != participantID && thread.LeadParticipantID != participantID:
			return thread, fmt.Errorf("conclude conversation thread %q: owner is %q and lead is %q — only the owner or the lead may file the conclusion", id, thread.OwnerParticipantID, thread.LeadParticipantID)
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
	switch state {
	case ExecStatePlanning, ExecStateExecuting, ExecStateBlocked, ExecStateNeedsHuman, ExecStateCompleted, ExecStateFailed:
	default:
		return fmt.Errorf("invalid conversation thread exec state %q (valid: %s, %s, %s, %s, %s, %s)",
			state, ExecStatePlanning, ExecStateExecuting, ExecStateBlocked, ExecStateNeedsHuman, ExecStateCompleted, ExecStateFailed)
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

// findConversationThreadByIDDB is FindConversationThreadByID against an
// already-open handle (for use under storeWriteMu).
func findConversationThreadByIDDB(db *sql.DB, id string) (ConversationThread, error) {
	row := db.QueryRow(`
SELECT id, session_id, anchor_item_id, title, status, created_by, created_at, escalated_at, escalated_by, summary, lead_participant_id, owner_participant_id, plan, exec_state, parent_seq, parent_author_participant_id
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
		&escalatedAt, &thread.EscalatedBy, &thread.Summary, &thread.LeadParticipantID, &thread.OwnerParticipantID, &planJSON,
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
