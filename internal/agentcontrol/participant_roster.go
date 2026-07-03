package agentcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ParticipantRoster persists the named-participant registry used by the
// conversation-native manage_participant tool. The owner of the
// session/workspace state (typically the app-server) supplies the
// implementation; AgentControl enforces that only agents with the
// participant speech capability can call into it.
type ParticipantRoster interface {
	// List returns the currently active named participants in a stable
	// order. The agent_id field is the active run that owns the
	// participant identity, or empty for an idle roster entry.
	List(ctx context.Context, agentID string) ([]RosterEntry, error)
	// Save upserts one named participant by name. The request's
	// MemorySeed is consumed by the roster only when the named
	// participant does not already exist (newly created) — never as
	// an overwrite of an existing MEMORY.md.
	Save(ctx context.Context, agentID string, req RosterSaveRequest) (RosterEntry, error)
	// Retire marks one named participant as retired. The roster
	// returns a non-nil error when the caller targets its own
	// participant identity.
	Retire(ctx context.Context, agentID string, name string) (RosterEntry, error)
	// ResolveAgentName returns the display name of the participant
	// identity bound to the given agent_id, or "" if the agent has
	// no participant identity (e.g. an ephemeral worker).
	ResolveAgentName(agentID string) string
}

// RosterEntry is one named participant in a roster response.
type RosterEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role,omitempty"`
	Avatar  string `json:"avatar,omitempty"`
	Tagline string `json:"tagline,omitempty"`
	Model   string `json:"model,omitempty"`
}

// RosterSaveRequest carries a save call from the manage_participant tool.
// Empty fields are treated as "do not change" when the named
// participant already exists.
type RosterSaveRequest struct {
	Name       string `json:"name"`
	Role       string `json:"role,omitempty"`
	Avatar     string `json:"avatar,omitempty"`
	Tagline    string `json:"tagline,omitempty"`
	Model      string `json:"model,omitempty"`
	MemorySeed string `json:"memory_seed,omitempty"`
}

// SetParticipantRoster installs the persistence layer for the
// manage_participant tool. Passing nil removes any previously
// installed roster. The agent control never persists participant
// state itself; it always delegates to the supplied callback.
func (c *AgentControl) SetParticipantRoster(r ParticipantRoster) {
	if c == nil {
		return
	}
	c.participantRosterMu.Lock()
	defer c.participantRosterMu.Unlock()
	c.participantRoster = r
}

func (c *AgentControl) currentParticipantRoster() ParticipantRoster {
	if c == nil {
		return nil
	}
	c.participantRosterMu.Lock()
	defer c.participantRosterMu.Unlock()
	return c.participantRoster
}

// SetAgentParticipantID records the participant id associated with an
// authorized speech run, so the roster can resolve the agent's own
// participant name for retire-self enforcement. The mapping is
// best-effort: AgentControl does not own participant lifecycle, so an
// out-of-band call is the simplest way to opt in. Passing an empty
// participantID removes any prior binding.
func (c *AgentControl) SetAgentParticipantID(agentID, participantID string) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return
	}
	participantID = strings.TrimSpace(participantID)
	c.participantRosterMu.Lock()
	defer c.participantRosterMu.Unlock()
	if participantID == "" {
		delete(c.participantRosterBindings, agentID)
		return
	}
	if c.participantRosterBindings == nil {
		c.participantRosterBindings = make(map[string]string)
	}
	c.participantRosterBindings[agentID] = participantID
}

func (c *AgentControl) boundParticipantID(agentID string) string {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return ""
	}
	c.participantRosterMu.Lock()
	defer c.participantRosterMu.Unlock()
	return c.participantRosterBindings[agentID]
}

// BoundParticipantID returns the participant id bound to agentID by a
// prior SetAgentParticipantID call, or "" when the agent has no
// explicit binding.
func (c *AgentControl) BoundParticipantID(agentID string) string {
	return c.boundParticipantID(agentID)
}

// ManageParticipantAction is the verb portion of a manage_participant
// tool call.
type ManageParticipantAction string

const (
	ManageParticipantList   ManageParticipantAction = "list"
	ManageParticipantSave   ManageParticipantAction = "save"
	ManageParticipantRetire ManageParticipantAction = "retire"
)

// ManageParticipant dispatches a single manage_participant tool
// invocation. The agent must already have the participant speech
// capability enabled; otherwise the call is rejected without reaching
// the roster.
func (c *AgentControl) ManageParticipant(ctx context.Context, agentID, args string) (ManageParticipantResult, error) {
	if c == nil {
		return ManageParticipantResult{}, errors.New("manage_participant: agent control not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return ManageParticipantResult{}, errors.New("manage_participant: agent_id is required")
	}
	if !c.ParticipantSpeechEnabled(agentID) {
		return ManageParticipantResult{}, fmt.Errorf("manage_participant: agent %q does not have participant speech capability", agentID)
	}
	roster := c.currentParticipantRoster()
	if roster == nil {
		return ManageParticipantResult{}, errors.New("manage_participant: no participant roster is attached")
	}

	var payload struct {
		Action     string `json:"action"`
		Name       string `json:"name,omitempty"`
		Role       string `json:"role,omitempty"`
		Avatar     string `json:"avatar,omitempty"`
		Tagline    string `json:"tagline,omitempty"`
		Model      string `json:"model,omitempty"`
		MemorySeed string `json:"memory_seed,omitempty"`
	}
	if err := json.Unmarshal([]byte(args), &payload); err != nil {
		return ManageParticipantResult{}, fmt.Errorf("manage_participant: parse args: %w", err)
	}
	action := ManageParticipantAction(strings.ToLower(strings.TrimSpace(payload.Action)))
	switch action {
	case ManageParticipantList:
		entries, err := roster.List(ctx, agentID)
		if err != nil {
			return ManageParticipantResult{}, err
		}
		return ManageParticipantResult{Action: action, Entries: entries}, nil
	case ManageParticipantSave:
		name := strings.TrimSpace(payload.Name)
		if name == "" {
			return ManageParticipantResult{}, errors.New("manage_participant: name is required for save")
		}
		entry, err := roster.Save(ctx, agentID, RosterSaveRequest{
			Name:       name,
			Role:       strings.TrimSpace(payload.Role),
			Avatar:     strings.TrimSpace(payload.Avatar),
			Tagline:    strings.TrimSpace(payload.Tagline),
			Model:      strings.TrimSpace(payload.Model),
			MemorySeed: payload.MemorySeed,
		})
		if err != nil {
			return ManageParticipantResult{}, err
		}
		return ManageParticipantResult{Action: action, Entries: []RosterEntry{entry}}, nil
	case ManageParticipantRetire:
		name := strings.TrimSpace(payload.Name)
		if name == "" {
			return ManageParticipantResult{}, errors.New("manage_participant: name is required for retire")
		}
		entry, err := roster.Retire(ctx, agentID, name)
		if err != nil {
			return ManageParticipantResult{}, err
		}
		return ManageParticipantResult{Action: action, Entries: []RosterEntry{entry}}, nil
	default:
		return ManageParticipantResult{}, fmt.Errorf("manage_participant: action %q is not supported", payload.Action)
	}
}

// ManageParticipantResult is the structured outcome of a
// manage_participant tool call. The tool marshals it as JSON for the
// model.
type ManageParticipantResult struct {
	Action  ManageParticipantAction `json:"action"`
	Entries []RosterEntry           `json:"participants,omitempty"`
}
