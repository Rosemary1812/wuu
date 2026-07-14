package appserver

import (
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

// residentCompensationRecoveryInterval keeps live peers responsive without
// turning the durable compensation journal into a hot polling path.
const residentCompensationRecoveryInterval = threadExecutionLeaseRetryDelay

// startResidentCompensationRecovery starts the lightweight live-peer recovery
// watcher. New calls this after startup recovery and presence registration are
// complete. The watcher is owned by backgroundWG through startBackground, so
// Close waits for it to observe closed and exit.
func (s *Server) startResidentCompensationRecovery() {
	if s == nil || s.rt == nil || strings.TrimSpace(s.rt.SessionDir) == "" || s.closed.Load() {
		return
	}
	s.residentCompensationOnce.Do(func() {
		done := make(chan struct{})
		s.residentCompensationDone = done
		if !s.startBackground(func() {
			defer close(done)
			s.watchResidentCompensationRecovery()
		}) {
			close(done)
		}
	})
}

func (s *Server) watchResidentCompensationRecovery() {
	// Probe once immediately so a peer that starts after a shutdown handoff
	// does not have to wait for the first ticker edge.
	s.recoverPendingResidentCompensations()
	ticker := time.NewTicker(residentCompensationRecoveryInterval)
	defer ticker.Stop()
	for {
		if s.closed.Load() {
			return
		}
		<-ticker.C
		if s.closed.Load() {
			return
		}
		s.recoverPendingResidentCompensations()
	}
}

func (s *Server) recoverPendingResidentCompensations() {
	if s == nil || s.rt == nil || s.closed.Load() {
		return
	}
	pending, err := session.PendingResidentAdmissionCompensations(s.rt.SessionDir)
	if err != nil {
		providers.DebugLogf("list live resident admission compensations: %v", err)
	} else {
		for _, item := range pending {
			if s.closed.Load() {
				return
			}
			s.recoverPendingResidentCompensation(item)
		}
	}
	s.recoverPendingResidentWakeIntents()
}

func (s *Server) recoverPendingResidentCompensation(pending session.ResidentAdmissionCompensation) {
	lease, acquired, err := session.TryAcquireThreadExecutionLease(s.rt.SessionDir, pending.ThreadID)
	if err != nil {
		providers.DebugLogf("acquire live resident compensation lease for thread %q: %v", pending.ThreadID, err)
		return
	}
	if !acquired {
		// The admission owner is still live. It remains solely responsible for
		// either resolving the compensation or handing the journal off on Close.
		return
	}

	resolveErr := session.ResolveResidentAdmissionCompensation(s.rt.SessionDir, pending)
	if err := lease.Release(); err != nil {
		providers.DebugLogf("release live resident compensation lease for thread %q: %v", pending.ThreadID, err)
	}
	if resolveErr != nil {
		providers.DebugLogf("resolve live resident admission compensation %q: %v", pending.ID, resolveErr)
		return
	}

	// Resolve publishes a durable wake intent in the same transaction that
	// restores admission state. The wake scanner below therefore remains able
	// to hand work to a live peer even if this process closes or crashes here.
}

func (s *Server) recoverPendingResidentWakeIntents() {
	pending, err := session.PendingResidentWakeIntents(s.rt.SessionDir)
	if err != nil {
		providers.DebugLogf("list live resident wake intents: %v", err)
		return
	}
	for _, item := range pending {
		if s.closed.Load() {
			return
		}
		s.kickResidentAgent(item.ParticipantID)
	}
}
