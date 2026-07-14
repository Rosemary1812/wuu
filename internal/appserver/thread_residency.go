package appserver

import (
	"sort"
	"strings"
	"time"
)

const residentThreadLimit = 8

type residentThreadCandidate struct {
	id             string
	thread         *threadState
	lastAccessedAt time.Time
	updatedAt      time.Time
}

func (s *Server) pruneResidentThreads(keepIDs ...string) {
	if s == nil || residentThreadLimit <= 0 {
		return
	}
	keep := make(map[string]struct{}, len(keepIDs))
	for _, id := range keepIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			keep[id] = struct{}{}
		}
	}

	s.mu.Lock()
	threadCount := len(s.threads)
	if threadCount <= residentThreadLimit {
		s.mu.Unlock()
		return
	}
	candidates := make([]residentThreadCandidate, 0, threadCount)
	for id, th := range s.threads {
		if _, ok := keep[id]; ok {
			continue
		}
		if candidate, ok := s.residentThreadCandidateLocked(id, th); ok {
			candidates = append(candidates, candidate)
		}
	}
	s.mu.Unlock()

	candidates = s.filterResidentThreadCandidates(candidates)
	if len(candidates) == 0 {
		return
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := candidateAccessTime(candidates[i])
		right := candidateAccessTime(candidates[j])
		if !left.Equal(right) {
			return left.Before(right)
		}
		return candidates[i].id < candidates[j].id
	})

	evicted := make([]*threadState, 0, len(candidates))
	s.mu.Lock()
	evictCount := len(s.threads) - residentThreadLimit
	for _, candidate := range candidates {
		if evictCount <= 0 {
			break
		}
		if _, ok := keep[candidate.id]; ok {
			continue
		}
		current := s.threads[candidate.id]
		if current == nil || current != candidate.thread {
			continue
		}
		if !residentThreadStillIdle(current) {
			continue
		}
		delete(s.threads, candidate.id)
		evicted = append(evicted, current)
		evictCount--
	}
	s.mu.Unlock()

	for _, th := range evicted {
		releaseThreadRuntime(th)
	}
}

func (s *Server) residentThreadCandidateLocked(id string, th *threadState) (residentThreadCandidate, bool) {
	if th == nil {
		return residentThreadCandidate{}, false
	}
	th.mu.Lock()
	defer th.mu.Unlock()
	if residentThreadExecutionActiveLocked(th) {
		return residentThreadCandidate{}, false
	}
	if th.Ephemeral {
		return residentThreadCandidate{}, false
	}
	if !th.PersistHistory && !th.ReadOnly {
		return residentThreadCandidate{}, false
	}
	return residentThreadCandidate{
		id:             id,
		thread:         th,
		lastAccessedAt: th.LastAccessedAt,
		updatedAt:      th.UpdatedAt,
	}, true
}

func (s *Server) filterResidentThreadCandidates(candidates []residentThreadCandidate) []residentThreadCandidate {
	if len(candidates) == 0 {
		return nil
	}
	out := candidates[:0]
	for _, candidate := range candidates {
		if s.hasQueuedUserWork(candidate.id) {
			continue
		}
		if s.hasQueuedAgentCompletionWork(candidate.id) {
			continue
		}
		if s.hasGoalContinuationWork(candidate.id) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func (s *Server) hasGoalContinuationWork(threadID string) bool {
	threadID = strings.TrimSpace(threadID)
	if s == nil || threadID == "" {
		return false
	}
	s.goalContinuationMu.Lock()
	defer s.goalContinuationMu.Unlock()
	return s.drainingGoalContinuation[threadID]
}

func candidateAccessTime(candidate residentThreadCandidate) time.Time {
	if !candidate.lastAccessedAt.IsZero() {
		return candidate.lastAccessedAt
	}
	if !candidate.updatedAt.IsZero() {
		return candidate.updatedAt
	}
	return time.Time{}
}

func residentThreadStillIdle(th *threadState) bool {
	if th == nil {
		return false
	}
	th.mu.Lock()
	defer th.mu.Unlock()
	if residentThreadExecutionActiveLocked(th) {
		return false
	}
	if th.Ephemeral {
		return false
	}
	if !th.PersistHistory && !th.ReadOnly {
		return false
	}
	return true
}

func residentThreadExecutionActiveLocked(th *threadState) bool {
	if th.running || th.executionLease != nil || th.admissionReserved {
		return true
	}
	return th.execRuntime != nil &&
		th.execRuntime.AgentControl != nil &&
		th.execRuntime.AgentControl.HasOwnedWorkerExecutions()
}
