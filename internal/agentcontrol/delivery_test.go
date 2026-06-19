package agentcontrol

import (
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

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
