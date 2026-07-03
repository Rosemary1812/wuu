package agentcontrol

import (
	"errors"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

// TestResumeProducesNewDeliveryID proves the delivery ledger treats a
// fail->resume->complete cycle as two distinct results: the failed
// snapshot and the later completed snapshot each mint their own delivery
// id, and each is claimable exactly once. The resume delivers a second
// time without special-casing because completedAt/result/status changed.
func TestResumeProducesNewDeliveryID(t *testing.T) {
	threadDir := t.TempDir()
	c := &AgentControl{
		rootThreadID: "root-thread",
		threadStore:  agentthread.NewStore(threadDir),
	}
	base := time.Now().UTC()

	failed := subagent.SubAgentSnapshot{
		ID:          "worker-resume",
		AgentPath:   "/root/resume",
		ParentID:    "root-thread",
		Status:      subagent.StatusFailed,
		Error:       errors.New("api terminal error"),
		CompletedAt: base,
	}
	failedID := c.ensureAgentResultDelivery(failed).ResultID
	if failedID == "" {
		t.Fatal("expected a delivery id for the failed run")
	}
	if ok, _ := c.ClaimAgentResultDeliveryID(failedID, agentResultConsumerNestedFollowup); !ok {
		t.Fatal("failed delivery should be claimable exactly once")
	}
	if ok, _ := c.ClaimAgentResultDeliveryID(failedID, agentResultConsumerNestedFollowup); ok {
		t.Fatal("failed delivery must not be claimable twice")
	}

	// The resume run reaches a new terminal snapshot: new status, new
	// completion time, new result -> a distinct delivery id.
	completed := subagent.SubAgentSnapshot{
		ID:          "worker-resume",
		AgentPath:   "/root/resume",
		ParentID:    "root-thread",
		Status:      subagent.StatusCompleted,
		Result:      "resumed done",
		CompletedAt: base.Add(time.Minute),
	}
	completedID := c.ensureAgentResultDelivery(completed).ResultID
	if completedID == "" || completedID == failedID {
		t.Fatalf("resume should mint a new delivery id, failed=%q completed=%q", failedID, completedID)
	}
	if ok, _ := c.ClaimAgentResultDeliveryID(completedID, agentResultConsumerNestedFollowup); !ok {
		t.Fatal("completed delivery should be claimable exactly once")
	}
	if ok, _ := c.ClaimAgentResultDeliveryID(completedID, agentResultConsumerNestedFollowup); ok {
		t.Fatal("completed delivery must not be claimable twice")
	}
}

func TestAgentResultDeliveryClaimRestoresFromThreadEvents(t *testing.T) {
	threadDir := t.TempDir()
	snap := subagent.SubAgentSnapshot{
		ID:          "worker-1",
		AgentPath:   "/root/review",
		ParentID:    "root-thread",
		Status:      subagent.StatusCompleted,
		Result:      "done",
		CompletedAt: time.Now().UTC(),
	}

	first := &AgentControl{
		rootThreadID: "root-thread",
		threadStore:  agentthread.NewStore(threadDir),
	}
	delivery := first.ensureAgentResultDelivery(snap)
	if delivery.ResultID == "" {
		t.Fatal("expected delivery result id")
	}
	if ok, consumedBy := first.ClaimAgentResultDeliveryID(delivery.ResultID, agentResultConsumerAwaitAgents); !ok || consumedBy != "" {
		t.Fatalf("first claim = %v consumedBy=%q, want claimed", ok, consumedBy)
	}

	restored := &AgentControl{
		rootThreadID: "root-thread",
		threadStore:  agentthread.NewStore(threadDir),
	}
	restored.restoreAgentResultDeliveries()
	if ok, consumedBy := restored.ClaimAgentResultDeliveryID(delivery.ResultID, agentResultConsumerAutoCompletion); ok || consumedBy != agentResultConsumerAwaitAgents {
		t.Fatalf("restored claim = %v consumedBy=%q, want already consumed by await_agents", ok, consumedBy)
	}
}
