package tools

import (
	"errors"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

// seedTaskThread creates a group session, opens a reply subthread on it, and
// escalates that reply to a task with leadID as its recorded lead. It returns
// the session dir and the escalated task cth id — the exact store shape the
// workflow-lead gate keys on (status == task, lead_participant_id == lead).
func seedTaskThread(t *testing.T, leadID string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if _, err := session.CreateWithMetadata(dir, "grp-1", "/tmp/project"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	cth, err := session.CreateConversationThread(dir, session.ConversationThread{
		SessionID:    "grp-1",
		AnchorItemID: "seq-1",
		Title:        "Ship the fix",
		CreatedBy:    "user",
	})
	if err != nil {
		t.Fatalf("create conversation thread: %v", err)
	}
	if _, err := session.EscalateConversationThread(dir, cth.ID, "user", leadID, "Ship the fix"); err != nil {
		t.Fatalf("escalate to task: %v", err)
	}
	return dir, cth.ID
}

// The workflow orchestration authority gate (requireWorkflowLead) is what
// enforces ownership at start_workflow / run_workflow / create_workflow execute
// time. It has to (a) allow the recorded task lead, returning the bound task
// cth; (b) reject any other named agent with errWorkflowLeadRequired; and
// (c) let self-contained callers (no participant identity — ordinary exec/CI)
// straight through with no binding.
func TestRequireWorkflowLeadGrantsLeadRejectsNonLead(t *testing.T) {
	const leadID = "prt-ada"
	const otherID = "prt-ben"
	sessDir, cthID := seedTaskThread(t, leadID)

	// (a) The recorded lead is granted and receives the bound task cth.
	leadEnv := &Env{ParticipantID: leadID, ConversationSessionDir: sessDir}
	cth, err := requireWorkflowLead(leadEnv)
	if err != nil {
		t.Fatalf("lead should be authorized, got %v", err)
	}
	if cth == nil || cth.ID != cthID {
		t.Fatalf("lead should bind to task cth %q, got %+v", cthID, cth)
	}
	if cth.Status != session.ConversationThreadTask || cth.LeadParticipantID != leadID {
		t.Fatalf("bound cth is not the escalated task led by %q: %+v", leadID, cth)
	}

	// (b) A different named agent leads no task and is refused.
	otherEnv := &Env{ParticipantID: otherID, ConversationSessionDir: sessDir}
	if _, err := requireWorkflowLead(otherEnv); !errors.Is(err, errWorkflowLeadRequired) {
		t.Fatalf("non-lead named agent should be rejected with errWorkflowLeadRequired, got %v", err)
	}

	// (c) A self-contained caller (no participant identity) passes through with
	// no bound task cth.
	selfEnv := &Env{ConversationSessionDir: sessDir}
	cth, err = requireWorkflowLead(selfEnv)
	if err != nil {
		t.Fatalf("self-contained caller should pass through, got %v", err)
	}
	if cth != nil {
		t.Fatalf("self-contained caller must not bind a task cth, got %+v", cth)
	}

	// A nil env is treated as self-contained (never panics).
	if cth, err := requireWorkflowLead(nil); err != nil || cth != nil {
		t.Fatalf("nil env should be a no-op pass-through, got cth=%+v err=%v", cth, err)
	}
}

// Once a task resolves (status flips away from task) the lead's orchestration
// authority is reclaimed: even the former lead can no longer start a run.
func TestRequireWorkflowLeadRejectsResolvedTaskLead(t *testing.T) {
	const leadID = "prt-ada"
	sessDir, cthID := seedTaskThread(t, leadID)
	if err := session.UpdateConversationThreadStatus(sessDir, cthID, session.ConversationThreadResolved); err != nil {
		t.Fatalf("resolve task: %v", err)
	}
	env := &Env{ParticipantID: leadID, ConversationSessionDir: sessDir}
	if _, err := requireWorkflowLead(env); !errors.Is(err, errWorkflowLeadRequired) {
		t.Fatalf("lead of a resolved (non-task) cth should be rejected, got %v", err)
	}
}

// requireWorkflowControlLead gates operations on an existing run against that
// run's own cth binding: only the current lead of the exact bound task cth may
// drive it, a stranger or an unbound run is refused, and a self-contained
// caller passes through.
func TestRequireWorkflowControlLeadValidatesRunBinding(t *testing.T) {
	const leadID = "prt-ada"
	const otherID = "prt-ben"
	sessDir, cthID := seedTaskThread(t, leadID)

	// The lead of the bound task cth may drive its run.
	leadEnv := &Env{ParticipantID: leadID, ConversationSessionDir: sessDir}
	if err := requireWorkflowControlLead(leadEnv, workflow.Run{ThreadID: cthID}); err != nil {
		t.Fatalf("lead should be allowed to control its bound run, got %v", err)
	}

	// A non-lead named agent is refused.
	otherEnv := &Env{ParticipantID: otherID, ConversationSessionDir: sessDir}
	if err := requireWorkflowControlLead(otherEnv, workflow.Run{ThreadID: cthID}); !errors.Is(err, errWorkflowLeadRequired) {
		t.Fatalf("non-lead should be refused control, got %v", err)
	}

	// A run with no cth binding cannot be lead-driven.
	if err := requireWorkflowControlLead(leadEnv, workflow.Run{}); !errors.Is(err, errWorkflowLeadRequired) {
		t.Fatalf("unbound run should be refused for a named caller, got %v", err)
	}

	// A self-contained caller passes through regardless of binding.
	selfEnv := &Env{ConversationSessionDir: sessDir}
	if err := requireWorkflowControlLead(selfEnv, workflow.Run{ThreadID: cthID}); err != nil {
		t.Fatalf("self-contained caller should pass through control gate, got %v", err)
	}
}
