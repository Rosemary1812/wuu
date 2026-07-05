package appserver

import (
	"strings"
	"time"
)

// participantBusyEntry records that a named agent is currently executing a
// task/workflow run. Reason is a short label for display/telemetry ("task"
// today; participant/start and workflow-dispatched runs both use it). AgentID
// is the live run holding the lock — it is empty only during the brief window
// between a synchronous participant/start reservation and the spawn returning
// the run id. Since marks when the lock was taken.
type participantBusyEntry struct {
	Reason  string
	AgentID string
	Since   time.Time
}

// tryAcquireParticipantBusy atomically reserves the busy lock for a named
// agent. It returns false when the agent is already busy so the caller can
// refuse a second task-pull without racing. The reservation carries no agent
// id yet; bindParticipantBusyAgent attaches it once the spawn returns.
func (s *Server) tryAcquireParticipantBusy(participantID, reason string) bool {
	participantID = strings.TrimSpace(participantID)
	if s == nil || participantID == "" {
		return false
	}
	s.participantBusyMu.Lock()
	defer s.participantBusyMu.Unlock()
	if s.participantBusy == nil {
		s.participantBusy = make(map[string]participantBusyEntry)
	}
	if _, ok := s.participantBusy[participantID]; ok {
		return false
	}
	s.participantBusy[participantID] = participantBusyEntry{Reason: reason, Since: time.Now().UTC()}
	return true
}

// bindParticipantBusyAgent attaches the spawned run id to an existing
// reservation. It is a no-op when the agent is not currently reserved (the
// lock was released by a racing completion) so a stale spawn can never
// resurrect the lock.
func (s *Server) bindParticipantBusyAgent(participantID, agentID string) {
	participantID = strings.TrimSpace(participantID)
	agentID = strings.TrimSpace(agentID)
	if s == nil || participantID == "" || agentID == "" {
		return
	}
	s.participantBusyMu.Lock()
	defer s.participantBusyMu.Unlock()
	entry, ok := s.participantBusy[participantID]
	if !ok {
		return
	}
	if entry.AgentID == "" {
		entry.AgentID = agentID
		s.participantBusy[participantID] = entry
	}
}

// markParticipantBusy is the idempotent acquire used by the agent-notification
// pipe: a run reporting Running marks its participant busy whether or not a
// participant/start handler already reserved it. This is the delivery seam for
// workflow-dispatched named runs, which never pass through participant/start.
func (s *Server) markParticipantBusy(participantID, reason, agentID string) {
	participantID = strings.TrimSpace(participantID)
	if s == nil || participantID == "" {
		return
	}
	agentID = strings.TrimSpace(agentID)
	s.participantBusyMu.Lock()
	defer s.participantBusyMu.Unlock()
	if s.participantBusy == nil {
		s.participantBusy = make(map[string]participantBusyEntry)
	}
	entry, ok := s.participantBusy[participantID]
	if !ok {
		s.participantBusy[participantID] = participantBusyEntry{Reason: reason, AgentID: agentID, Since: time.Now().UTC()}
		return
	}
	if entry.AgentID == "" && agentID != "" {
		entry.AgentID = agentID
		s.participantBusy[participantID] = entry
	}
}

// releaseParticipantBusy clears the lock for a named agent. When agentID is
// non-empty it only releases a lock held by that same run (or a reservation
// with no bound run), so a terminated run can never clear a newer run's lock.
// An empty agentID force-releases (used when a synchronous reservation must be
// rolled back after a spawn error).
func (s *Server) releaseParticipantBusy(participantID, agentID string) {
	participantID = strings.TrimSpace(participantID)
	if s == nil || participantID == "" {
		return
	}
	agentID = strings.TrimSpace(agentID)
	s.participantBusyMu.Lock()
	defer s.participantBusyMu.Unlock()
	entry, ok := s.participantBusy[participantID]
	if !ok {
		return
	}
	if agentID == "" || entry.AgentID == "" || entry.AgentID == agentID {
		delete(s.participantBusy, participantID)
	}
}

// participantBusyInfo returns the busy entry for a named agent, if any.
func (s *Server) participantBusyInfo(participantID string) (participantBusyEntry, bool) {
	participantID = strings.TrimSpace(participantID)
	if s == nil || participantID == "" {
		return participantBusyEntry{}, false
	}
	s.participantBusyMu.Lock()
	defer s.participantBusyMu.Unlock()
	entry, ok := s.participantBusy[participantID]
	return entry, ok
}

// participantIsBusy reports whether a named agent is currently executing a
// task/workflow run.
func (s *Server) participantIsBusy(participantID string) bool {
	_, ok := s.participantBusyInfo(participantID)
	return ok
}
