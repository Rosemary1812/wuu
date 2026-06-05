package workflow

import (
	"fmt"
	"time"
)

type MemoryCandidateStatus string

const (
	MemoryCandidatePending  MemoryCandidateStatus = "pending"
	MemoryCandidateAccepted MemoryCandidateStatus = "accepted"
	MemoryCandidateRejected MemoryCandidateStatus = "rejected"
)

type MemoryCandidate struct {
	ID           string                `json:"id"`
	RunID        string                `json:"run_id"`
	AgentRunID   string                `json:"agent_run_id,omitempty"`
	AgentProfile string                `json:"agent_profile,omitempty"`
	Target       string                `json:"target"`
	Content      string                `json:"content"`
	Tags         []string              `json:"tags,omitempty"`
	Source       string                `json:"source,omitempty"`
	Status       MemoryCandidateStatus `json:"status"`
	Reason       string                `json:"reason,omitempty"`
	CreatedAt    time.Time             `json:"created_at"`
	ReviewedAt   time.Time             `json:"reviewed_at,omitempty"`
}

func ValidateMemoryCandidateStatus(status MemoryCandidateStatus) error {
	switch status {
	case MemoryCandidatePending, MemoryCandidateAccepted, MemoryCandidateRejected:
		return nil
	default:
		return fmt.Errorf("unknown workflow memory candidate status: %s", status)
	}
}
