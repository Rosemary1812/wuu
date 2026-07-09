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
	// ForkedFrom is the participant ID of the母体 (mother) named agent this
	// one was forked from — a temporary "分身" (decision six: copy a named
	// agent with its memory snapshot, run it, then append-merge its memory
	// back into the mother and retire). Empty for ordinary named agents.
	// This is NOT the session/conversation fork (session.ForkedFromID) nor the
	// spawn_agent history fork; it is a distinct named-agent identity copy.
	ForkedFrom string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	RetiredAt  *time.Time
}

// Summary is the wire shape embedded in notifications and thread items.
type Summary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Role string `json:"role,omitempty"`
	// AvatarImage is the participant's uploaded avatar as a base64 data
	// URL ("data:image/png;base64,..."). The avatar bytes live in the
	// participant's workspace directory, not in the store, so
	// Participant.Summary() leaves this empty — the appserver fills it
	// (with a size cap, see appserver.participantSummaryAvatarMaxBytes)
	// because summaries are duplicated into every thread item they
	// attribute and an unbounded payload would bloat history resumes.
	AvatarImage string `json:"avatar_image,omitempty"`
	// Busy reports that this named agent is currently executing a
	// task run and cannot take a second concurrent pull
	// (decision-five concurrency lock). It is runtime state, not stored, so
	// Participant.Summary() leaves it false — the appserver overlays it on
	// group-member views (threadWithGroupMembers) so the UI can show a busy
	// badge and steer the user toward forking a copy (decision six).
	Busy bool `json:"busy,omitempty"`
	// ForkedFromID marks a temporary分身 (decision six): the participant ID of
	// the母体 this named agent was forked from. The UI uses it to badge the
	// copy as "X 的分身". Distinct from the session/conversation fork
	// (ThreadSummary.forked_from_id) — this is a named-agent identity copy.
	ForkedFromID string `json:"forked_from_id,omitempty"`
}

// Summary returns the wire shape for a participant.
func (p Participant) Summary() Summary {
	return Summary{
		ID:           p.ID,
		Name:         p.Name,
		Kind:         string(p.Kind),
		Role:         p.Role,
		ForkedFromID: p.ForkedFrom,
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
