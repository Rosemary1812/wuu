// Package participant defines conversation participants: humans, the
// primary agent, persistent named agents, and ephemeral task workers.
package participant

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Kind classifies how a participant joins the conversation.
type Kind string

const (
	KindHuman     Kind = "human"
	KindPrimary   Kind = "primary"
	KindNamed     Kind = "named"
	KindEphemeral Kind = "ephemeral"
)

// Participant is one conversation identity.
type Participant struct {
	ID        string
	Kind      Kind
	Name      string
	Role      string // WorkerType name; empty for human/primary
	Avatar    string // legacy emoji glyph; no longer written or rendered
	Tagline   string
	Workspace string // persistent dir for named agents; empty otherwise
	Model     string // pinned model; empty = follow global
	CreatedAt time.Time
	UpdatedAt time.Time
	RetiredAt *time.Time
}

// Summary is the wire shape embedded in notifications and thread items.
type Summary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Role string `json:"role,omitempty"`
}

// Summary returns the wire shape for a participant.
func (p Participant) Summary() Summary {
	return Summary{
		ID:   p.ID,
		Name: p.Name,
		Kind: string(p.Kind),
		Role: p.Role,
	}
}

// NewID generates a participant ID: "prt-" + 16 hex chars.
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "prt-" + hex.EncodeToString(b)
}

// DeriveEphemeralName builds a display name for an ephemeral task worker:
// capitalized worker type (or "Agent" when empty), joined to the task name
// with "·" when the task name is non-empty.
func DeriveEphemeralName(taskName, workerType string) string {
	if workerType == "" {
		workerType = "agent"
	}
	name := capitalize(workerType)
	if taskName != "" {
		name += "·" + taskName
	}
	return name
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-'a'+'A') + s[1:]
	}
	return s
}
