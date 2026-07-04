package appserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

func timeNow() time.Time { return time.Now().UTC() }

// lookupWorkerTypeName validates that role matches a built-in
// agentcontrol worker type. The check is intentionally local so
// participant_roster does not have to import agentcontrol's full
// worker type registry.
func lookupWorkerTypeName(role string) (string, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		return "", nil
	}
	if _, err := agentcontrol.LookupWorkerType(role); err != nil {
		return "", err
	}
	return role, nil
}

// participantRosterForTool builds the manage_participant roster backed
// by the app-server's session store. It is installed on every
// AgentControl the server owns, including the throwaway test instances
// created by direct test wiring.
func (s *Server) participantRosterForTool() agentcontrol.ParticipantRoster {
	return &serverParticipantRoster{server: s}
}

type serverParticipantRoster struct {
	server *Server
}

func (r *serverParticipantRoster) sessionDir() string {
	if r == nil || r.server == nil || r.server.rt == nil {
		return ""
	}
	return r.server.rt.SessionDir
}

func (r *serverParticipantRoster) List(ctx context.Context, agentID string) ([]agentcontrol.RosterEntry, error) {
	if err := r.requireSessionDir(); err != nil {
		return nil, err
	}
	all, err := session.ListParticipants(r.sessionDir(), participant.KindNamed)
	if err != nil {
		return nil, fmt.Errorf("manage_participant: list: %w", err)
	}
	out := make([]agentcontrol.RosterEntry, 0, len(all))
	for _, p := range all {
		out = append(out, agentcontrol.RosterEntry{
			ID:      p.ID,
			Name:    p.Name,
			Role:    p.Role,
			Tagline: p.Tagline,
			Model:   p.Model,
		})
	}
	return out, nil
}

func (r *serverParticipantRoster) Save(ctx context.Context, agentID string, req agentcontrol.RosterSaveRequest) (agentcontrol.RosterEntry, error) {
	if err := r.requireSessionDir(); err != nil {
		return agentcontrol.RosterEntry{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return agentcontrol.RosterEntry{}, errors.New("manage_participant: name is required")
	}
	role := strings.TrimSpace(req.Role)
	if role != "" {
		if _, err := lookupWorkerTypeName(role); err != nil {
			return agentcontrol.RosterEntry{}, fmt.Errorf("manage_participant: %w", err)
		}
	}
	tagline := strings.TrimSpace(req.Tagline)
	model := strings.TrimSpace(req.Model)

	existing, err := r.findActiveByName(name)
	if err != nil {
		return agentcontrol.RosterEntry{}, err
	}

	now := timeNow()
	entry := participant.Participant{
		Kind:      participant.KindNamed,
		Name:      name,
		Role:      role,
		Tagline:   tagline,
		Model:     model,
		UpdatedAt: now,
	}
	if existing == nil {
		entry.ID = participant.NewID()
		entry.CreatedAt = now
		workspace, werr := r.server.participantWorkspace(entry.ID)
		if werr != nil {
			return agentcontrol.RosterEntry{}, werr
		}
		entry.Workspace = workspace
	} else {
		entry.ID = existing.ID
		entry.CreatedAt = existing.CreatedAt
		entry.Workspace = existing.Workspace
		// Legacy emoji avatars are preserved in the store but no longer
		// settable through this tool.
		entry.Avatar = existing.Avatar
		if entry.Role == "" {
			entry.Role = existing.Role
		}
		if entry.Tagline == "" {
			entry.Tagline = existing.Tagline
		}
		if entry.Model == "" {
			entry.Model = existing.Model
		}
	}
	// Persist the participant row first so a failed workspace creation
	// cannot leave a real participant row missing from the store. The
	// resulting "row without workspace" state is acceptable: the next
	// save/list will lazily fix the directory the same way the
	// participant/save RPC tolerates the same partial state on retry.
	if err := session.UpsertParticipant(r.sessionDir(), entry); err != nil {
		return agentcontrol.RosterEntry{}, err
	}
	r.server.invalidateParticipantSummary(entry.ID)
	if existing == nil {
		if err := os.MkdirAll(entry.Workspace, 0o755); err != nil {
			return agentcontrol.RosterEntry{}, fmt.Errorf("create participant workspace: %w", err)
		}
		// Only consume MemorySeed on initial creation. A blank/whitespace
		// seed writes a 0-byte MEMORY.md so the workspace has a real
		// file (matching the reset/save semantics); non-blank seeds are
		// written verbatim, no internal whitespace trimming.
		memPath := participantMemoryPath(entry.Workspace)
		if memPath != "" {
			seed := []byte(req.MemorySeed)
			if strings.TrimSpace(req.MemorySeed) == "" {
				seed = []byte{}
			}
			if err := os.WriteFile(memPath, seed, 0o644); err != nil {
				return agentcontrol.RosterEntry{}, fmt.Errorf("write participant memory: %w", err)
			}
		}
	}
	result := agentcontrol.RosterEntry{
		ID:      entry.ID,
		Name:    entry.Name,
		Role:    entry.Role,
		Tagline: entry.Tagline,
		Model:   entry.Model,
	}
	if existing == nil {
		// Same-name rebuild guard: surface a retired predecessor's ID so
		// the caller knows archived state exists. Advisory only — the new
		// participant starts fresh.
		result.ArchivedPredecessorID = r.server.archivedPredecessorID(entry.Name, entry.ID)
	}
	if profile, err := r.server.participantProfile(entry); err == nil {
		if err := r.server.notifyParticipantUpdated(profile); err != nil {
			providers.DebugLogf("notify participant update for %q: %v", entry.ID, err)
		}
	} else {
		providers.DebugLogf("load participant profile for %q after roster save: %v", entry.ID, err)
	}
	return result, nil
}

func (r *serverParticipantRoster) Retire(ctx context.Context, agentID, name string) (agentcontrol.RosterEntry, error) {
	if err := r.requireSessionDir(); err != nil {
		return agentcontrol.RosterEntry{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return agentcontrol.RosterEntry{}, errors.New("manage_participant: name is required")
	}
	agentName := r.ResolveAgentName(agentID)
	if agentName != "" && strings.EqualFold(agentName, name) {
		return agentcontrol.RosterEntry{}, errors.New("cannot retire yourself")
	}
	target, err := r.findActiveByName(name)
	if err != nil {
		return agentcontrol.RosterEntry{}, err
	}
	if target == nil {
		return agentcontrol.RosterEntry{}, fmt.Errorf("manage_participant: participant %q is not active", name)
	}
	// Full cleanup protocol (store transaction + directory archival + DM
	// freeze), shared with the participant/retire RPC.
	if err := r.server.retireNamedParticipant(*target); err != nil {
		return agentcontrol.RosterEntry{}, err
	}
	if updated, err := session.GetParticipant(r.sessionDir(), target.ID); err == nil {
		if profile, err := r.server.participantProfile(updated); err == nil {
			profile.AvatarImage = ""
			if err := r.server.notifyParticipantUpdated(profile); err != nil {
				providers.DebugLogf("notify participant retire for %q: %v", target.ID, err)
			}
		} else {
			providers.DebugLogf("load participant profile for %q after roster retire: %v", target.ID, err)
		}
	} else {
		providers.DebugLogf("load participant %q after roster retire: %v", target.ID, err)
	}
	return agentcontrol.RosterEntry{
		ID:      target.ID,
		Name:    target.Name,
		Role:    target.Role,
		Tagline: target.Tagline,
		Model:   target.Model,
	}, nil
}

func (r *serverParticipantRoster) ResolveAgentName(agentID string) string {
	if r == nil || r.server == nil || r.server.rt == nil {
		return ""
	}
	participantID := r.findAgentParticipantID(agentID)
	if participantID == "" {
		return ""
	}
	p, err := session.GetParticipant(r.sessionDir(), participantID)
	if err != nil {
		return ""
	}
	return p.Name
}

func (r *serverParticipantRoster) findActiveByName(name string) (*participant.Participant, error) {
	all, err := session.ListParticipants(r.sessionDir(), participant.KindNamed)
	if err != nil {
		return nil, fmt.Errorf("manage_participant: list: %w", err)
	}
	for i, p := range all {
		if strings.EqualFold(p.Name, name) {
			return &all[i], nil
		}
	}
	return nil, nil
}

func (r *serverParticipantRoster) requireSessionDir() error {
	if strings.TrimSpace(r.sessionDir()) == "" {
		return errors.New("manage_participant: session dir is not configured")
	}
	return nil
}

// findAgentParticipantID resolves the participant id associated with a
// running agent. It checks the explicit roster binding first
// (SetAgentParticipantID), then falls back to any thread's snapshot
// ParticipantID. Returns "" if no mapping is known.
func (r *serverParticipantRoster) findAgentParticipantID(agentID string) string {
	if r == nil || r.server == nil {
		return ""
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return ""
	}
	r.server.mu.Lock()
	defer r.server.mu.Unlock()
	for _, th := range r.server.threads {
		if th == nil || th.execRuntime == nil || th.execRuntime.AgentControl == nil {
			continue
		}
		if pid := th.execRuntime.AgentControl.BoundParticipantID(agentID); pid != "" {
			return pid
		}
		for _, snap := range th.execRuntime.AgentControl.List() {
			if snap.ID == agentID {
				return snap.ParticipantID
			}
		}
	}
	return ""
}
