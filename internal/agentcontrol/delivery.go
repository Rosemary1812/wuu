package agentcontrol

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

const (
	agentResultConsumerAwaitAgents    = "await_agents"
	agentResultConsumerAutoCompletion = "auto_completion"
	agentResultConsumerNestedFollowup = "nested_followup"
	agentResultConsumerSpawnAgent     = "spawn_agent"
)

type agentResultDelivery struct {
	ResultID    string
	AgentID     string
	ParentID    string
	ThreadID    string
	AgentPath   string
	Status      string
	CompletedAt time.Time
	CreatedAt   time.Time
	ConsumedBy  string
	ConsumedAt  time.Time
}

func agentResultDeliveryID(snap subagent.SubAgentSnapshot) string {
	agentID := strings.TrimSpace(snap.ID)
	if agentID == "" || !isFinalSubAgentStatus(snap.Status) {
		return ""
	}
	completedAt := ""
	if !snap.CompletedAt.IsZero() {
		completedAt = snap.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	errText := ""
	if snap.Error != nil {
		errText = snap.Error.Error()
	}
	h := sha256.New()
	h.Write([]byte(agentID))
	h.Write([]byte{0})
	h.Write([]byte(string(snap.Status)))
	h.Write([]byte{0})
	h.Write([]byte(completedAt))
	h.Write([]byte{0})
	h.Write([]byte(snap.Result))
	h.Write([]byte{0})
	h.Write([]byte(errText))
	sum := h.Sum(nil)
	return "agent_result_" + hex.EncodeToString(sum[:12])
}

// AgentResultDeliveryID returns the stable delivery id for a terminal worker
// result. Non-terminal snapshots do not have a consumable delivery id.
func (c *AgentControl) AgentResultDeliveryID(snap subagent.SubAgentSnapshot) string {
	return agentResultDeliveryID(snap)
}

func (c *AgentControl) ensureAgentResultDelivery(snap subagent.SubAgentSnapshot) agentResultDelivery {
	resultID := agentResultDeliveryID(snap)
	if c == nil || resultID == "" {
		return agentResultDelivery{}
	}
	now := time.Now().UTC()
	delivery := agentResultDelivery{
		ResultID:    resultID,
		AgentID:     strings.TrimSpace(snap.ID),
		ParentID:    strings.TrimSpace(snap.ParentID),
		ThreadID:    strings.TrimSpace(c.rootThreadID),
		AgentPath:   strings.TrimSpace(snap.AgentPath),
		Status:      strings.TrimSpace(string(snap.Status)),
		CompletedAt: snap.CompletedAt,
		CreatedAt:   now,
	}
	c.resultDeliveriesMu.Lock()
	if c.resultDeliveries == nil {
		c.resultDeliveries = make(map[string]agentResultDelivery)
	}
	if existing, ok := c.resultDeliveries[resultID]; ok {
		c.resultDeliveriesMu.Unlock()
		return existing
	}
	c.resultDeliveries[resultID] = delivery
	c.resultDeliveriesMu.Unlock()
	c.recordAgentResultDeliveryEvent(agentthread.Event{
		Type:        agentthread.EventResultReady,
		ThreadID:    delivery.ThreadID,
		AgentID:     delivery.AgentID,
		ParentID:    delivery.ParentID,
		ResultID:    delivery.ResultID,
		Path:        delivery.AgentPath,
		Status:      agentThreadStatusForResultDelivery(delivery.Status),
		CompletedAt: delivery.CompletedAt,
		CreatedAt:   now,
	})
	return delivery
}

func agentThreadStatusForResultDelivery(status string) agentthread.Status {
	switch subagent.Status(strings.TrimSpace(status)) {
	case subagent.StatusCompleted:
		return agentthread.StatusCompleted
	case subagent.StatusFailed:
		return agentthread.StatusFailed
	case subagent.StatusCancelled:
		return agentthread.StatusCancelled
	default:
		return agentthread.Status(strings.TrimSpace(status))
	}
}

func (c *AgentControl) claimAgentResultDelivery(snap subagent.SubAgentSnapshot, consumer string) (string, bool, string) {
	delivery := c.ensureAgentResultDelivery(snap)
	if delivery.ResultID == "" {
		return "", false, ""
	}
	claimed, consumedBy := c.ClaimAgentResultDeliveryID(delivery.ResultID, consumer)
	return delivery.ResultID, claimed, consumedBy
}

// ClaimAgentResultDeliveryID marks a terminal result as consumed by one model
// delivery path. It returns false when another path has already consumed it.
func (c *AgentControl) ClaimAgentResultDeliveryID(resultID, consumer string) (bool, string) {
	if c == nil {
		return false, ""
	}
	resultID = strings.TrimSpace(resultID)
	consumer = strings.TrimSpace(consumer)
	if resultID == "" {
		return false, ""
	}
	if consumer == "" {
		consumer = "unknown"
	}
	now := time.Now().UTC()
	c.resultDeliveriesMu.Lock()
	delivery, ok := c.resultDeliveries[resultID]
	if !ok {
		c.resultDeliveriesMu.Unlock()
		return false, ""
	}
	if delivery.ConsumedBy != "" {
		consumedBy := delivery.ConsumedBy
		c.resultDeliveriesMu.Unlock()
		return false, consumedBy
	}
	delivery.ConsumedBy = consumer
	delivery.ConsumedAt = now
	c.resultDeliveries[resultID] = delivery
	c.resultDeliveriesMu.Unlock()
	c.recordAgentResultDeliveryEvent(agentthread.Event{
		Type:        agentthread.EventResultClaim,
		ThreadID:    delivery.ThreadID,
		AgentID:     delivery.AgentID,
		ParentID:    delivery.ParentID,
		ResultID:    delivery.ResultID,
		Consumer:    consumer,
		Path:        delivery.AgentPath,
		Status:      agentThreadStatusForResultDelivery(delivery.Status),
		CompletedAt: delivery.CompletedAt,
		CreatedAt:   now,
	})
	return true, ""
}

func (c *AgentControl) ReleaseAgentResultDeliveryClaim(resultID, consumer string) {
	if c == nil {
		return
	}
	resultID = strings.TrimSpace(resultID)
	consumer = strings.TrimSpace(consumer)
	if resultID == "" || consumer == "" {
		return
	}
	now := time.Now().UTC()
	c.resultDeliveriesMu.Lock()
	delivery, ok := c.resultDeliveries[resultID]
	if !ok || delivery.ConsumedBy != consumer {
		c.resultDeliveriesMu.Unlock()
		return
	}
	delivery.ConsumedBy = ""
	delivery.ConsumedAt = time.Time{}
	c.resultDeliveries[resultID] = delivery
	c.resultDeliveriesMu.Unlock()
	c.recordAgentResultDeliveryEvent(agentthread.Event{
		Type:        agentthread.EventResultRelease,
		ThreadID:    delivery.ThreadID,
		AgentID:     delivery.AgentID,
		ParentID:    delivery.ParentID,
		ResultID:    delivery.ResultID,
		Consumer:    consumer,
		Path:        delivery.AgentPath,
		Status:      agentThreadStatusForResultDelivery(delivery.Status),
		CompletedAt: delivery.CompletedAt,
		CreatedAt:   now,
	})
}

func (c *AgentControl) recordAgentResultDeliveryEvent(event agentthread.Event) {
	if c == nil || c.threadStore == nil {
		return
	}
	_ = c.threadStore.AppendEvent(event)
}

func (c *AgentControl) restoreAgentResultDeliveries() {
	if c == nil || c.threadStore == nil {
		return
	}
	c.resultDeliveriesMu.Lock()
	deliveries := make(map[string]agentResultDelivery, len(c.resultDeliveries))
	for resultID, delivery := range c.resultDeliveries {
		deliveries[resultID] = delivery
	}
	c.resultDeliveriesMu.Unlock()
	events, err := c.threadStore.ReadEvents()
	if err != nil {
		return
	}
	for _, event := range events {
		resultID := strings.TrimSpace(event.ResultID)
		if resultID == "" {
			continue
		}
		switch event.Type {
		case agentthread.EventResultReady:
			delivery := deliveries[resultID]
			delivery.ResultID = resultID
			delivery.AgentID = strings.TrimSpace(event.AgentID)
			delivery.ParentID = strings.TrimSpace(event.ParentID)
			delivery.ThreadID = strings.TrimSpace(event.ThreadID)
			delivery.AgentPath = strings.TrimSpace(event.Path)
			delivery.Status = strings.TrimSpace(string(event.Status))
			delivery.CompletedAt = event.CompletedAt
			delivery.CreatedAt = event.CreatedAt
			deliveries[resultID] = delivery
		case agentthread.EventResultClaim:
			delivery := deliveries[resultID]
			delivery.ResultID = resultID
			delivery.AgentID = firstNonEmptyString(delivery.AgentID, strings.TrimSpace(event.AgentID))
			delivery.ParentID = firstNonEmptyString(delivery.ParentID, strings.TrimSpace(event.ParentID))
			delivery.ThreadID = firstNonEmptyString(delivery.ThreadID, strings.TrimSpace(event.ThreadID))
			delivery.AgentPath = firstNonEmptyString(delivery.AgentPath, strings.TrimSpace(event.Path))
			delivery.Status = firstNonEmptyString(delivery.Status, strings.TrimSpace(string(event.Status)))
			if delivery.CompletedAt.IsZero() {
				delivery.CompletedAt = event.CompletedAt
			}
			delivery.ConsumedBy = strings.TrimSpace(event.Consumer)
			delivery.ConsumedAt = event.CreatedAt
			deliveries[resultID] = delivery
		case agentthread.EventResultRelease:
			delivery := deliveries[resultID]
			if delivery.ConsumedBy == strings.TrimSpace(event.Consumer) {
				delivery.ConsumedBy = ""
				delivery.ConsumedAt = time.Time{}
				deliveries[resultID] = delivery
			}
		}
	}
	c.resultDeliveriesMu.Lock()
	c.resultDeliveries = deliveries
	c.resultDeliveriesMu.Unlock()
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
