package cron

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type SchedulerConfig struct {
	Store        *TaskStore
	SessionStore *SessionTaskStore
	OnFire       func(context.Context, Task) error
	OnError      func(error)
	IsOwner      func() bool
	IsKilled     func() bool
}

type schedulerStore interface {
	List() ([]Task, error)
	ClaimForDispatch(Task, int64) (bool, error)
	RemoveIfUnchanged(Task) (bool, error)
}

type Scheduler struct {
	cfg SchedulerConfig

	callbackCtx     context.Context
	cancelCallbacks context.CancelFunc
	loopStop        chan struct{}
	stopDone        chan struct{}

	stateMu  sync.Mutex
	started  bool
	stopping bool

	loopWG     sync.WaitGroup
	callbackWG sync.WaitGroup
	checkMu    sync.Mutex

	inFlightMu sync.Mutex
	inFlight   map[string]struct{}
}

func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.IsKilled == nil {
		cfg.IsKilled = func() bool { return false }
	}
	callbackCtx, cancelCallbacks := context.WithCancel(context.Background())
	return &Scheduler{
		cfg:             cfg,
		callbackCtx:     callbackCtx,
		cancelCallbacks: cancelCallbacks,
		loopStop:        make(chan struct{}),
		stopDone:        make(chan struct{}),
		inFlight:        make(map[string]struct{}),
	}
}

// Start launches the scheduler loop. Before the first tick it catches up
// one-shot tasks whose fire time passed while no scheduler was running.
//
// Each occurrence is claimed in its store before its callback starts. The
// scheduler therefore provides at-most-once attempts, not exactly-once
// execution: a callback failure or a process crash after the claim is not
// retried automatically.
func (s *Scheduler) Start() {
	s.stateMu.Lock()
	if s.started || s.stopping {
		s.stateMu.Unlock()
		return
	}
	s.started = true
	s.loopWG.Add(1)
	s.stateMu.Unlock()

	go s.run()
}

func (s *Scheduler) run() {
	defer s.loopWG.Done()
	s.catchUpMissedOneShots(time.Now())

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.check()
		case <-s.loopStop:
			return
		}
	}
}

// catchUpMissedOneShots uses the same claim path as regular ticks. Recurring
// tasks are not backfilled; the first regular due evaluation collapses missed
// occurrences into one attempt.
func (s *Scheduler) catchUpMissedOneShots(now time.Time) {
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	if s.shouldStopWork() {
		return
	}

	if s.ownsDurableTasks() && s.cfg.Store != nil {
		s.catchUpStore("durable", s.cfg.Store, false, now)
	}
	if s.cfg.SessionStore != nil {
		s.catchUpStore("session", s.cfg.SessionStore, true, now)
	}
}

func (s *Scheduler) catchUpStore(kind string, store schedulerStore, sessionOnly bool, now time.Time) {
	tasks, err := store.List()
	if err != nil {
		s.reportError(fmt.Errorf("list %s scheduled tasks for catch-up: %w", kind, err))
		return
	}
	for _, task := range FindMissedOneShots(tasks, now) {
		if task.Paused {
			continue
		}
		if s.shouldStopWork() {
			return
		}
		s.claimAndDispatch(store, task, sessionOnly, now)
	}
}

// Stop is idempotent. It prevents new claims, stops the polling loop, cancels
// active callbacks, and waits for them before returning. Callers may release
// scheduler ownership only after Stop returns.
func (s *Scheduler) Stop() {
	s.stateMu.Lock()
	if s.stopping {
		done := s.stopDone
		s.stateMu.Unlock()
		<-done
		return
	}
	s.stopping = true
	started := s.started
	close(s.loopStop)
	s.stateMu.Unlock()

	if started {
		s.loopWG.Wait()
	}
	// Package tests call check directly. Joining the check lock also makes
	// callbackWG.Wait safe from a concurrent Add outside the polling loop.
	s.checkMu.Lock()
	s.checkMu.Unlock()
	s.cancelCallbacks()
	s.callbackWG.Wait()

	s.stateMu.Lock()
	close(s.stopDone)
	s.stateMu.Unlock()
}

func (s *Scheduler) check() {
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	if s.shouldStopWork() {
		return
	}

	now := time.Now()
	if s.ownsDurableTasks() && s.cfg.Store != nil {
		s.checkStore("durable", s.cfg.Store, false, now)
	}
	if s.cfg.SessionStore != nil {
		s.checkStore("session", s.cfg.SessionStore, true, now)
	}
}

func (s *Scheduler) checkStore(kind string, store schedulerStore, sessionOnly bool, now time.Time) {
	tasks, err := store.List()
	if err != nil {
		s.reportError(fmt.Errorf("list %s scheduled tasks: %w", kind, err))
		return
	}
	for _, task := range tasks {
		if s.shouldStopWork() {
			return
		}
		// Expiry cleanup runs before the paused check so pausing a task
		// cannot keep an expired entry in the store forever.
		if task.Recurring && IsExpired(task, now.UnixMilli()) {
			if _, err := store.RemoveIfUnchanged(task); err != nil {
				s.reportError(fmt.Errorf("remove expired %s scheduled task %q: %w", kind, task.ID, err))
			}
			continue
		}
		if task.Paused {
			continue
		}

		next, err := task.NextFireAt()
		if err != nil {
			s.reportError(fmt.Errorf("calculate next fire for %s scheduled task %q: %w", kind, task.ID, err))
			continue
		}
		if now.Before(next) {
			continue
		}
		s.claimAndDispatch(store, task, sessionOnly, now)
	}
}

func (s *Scheduler) claimAndDispatch(store schedulerStore, task Task, sessionOnly bool, firedAt time.Time) {
	if s.cfg.OnFire == nil || s.shouldStopWork() {
		return
	}
	key := task.ID
	if sessionOnly {
		key = "session:" + task.ID
	}

	s.inFlightMu.Lock()
	if _, busy := s.inFlight[key]; busy {
		s.inFlightMu.Unlock()
		return
	}
	s.inFlight[key] = struct{}{}
	s.inFlightMu.Unlock()

	claimed, err := store.ClaimForDispatch(task, firedAt.UnixMilli())
	if err != nil {
		s.clearInFlight(key)
		s.reportError(fmt.Errorf("claim scheduled task %q: %w", task.ID, err))
		return
	}
	if !claimed {
		s.clearInFlight(key)
		return
	}

	s.callbackWG.Add(1)
	go func() {
		defer s.callbackWG.Done()
		defer s.clearInFlight(key)
		if err := s.cfg.OnFire(s.callbackCtx, task); err != nil {
			s.reportError(fmt.Errorf("run scheduled task %q: %w", task.ID, err))
		}
	}()
}

func (s *Scheduler) clearInFlight(key string) {
	s.inFlightMu.Lock()
	delete(s.inFlight, key)
	s.inFlightMu.Unlock()
}

func (s *Scheduler) ownsDurableTasks() bool {
	return s.cfg.IsOwner == nil || s.cfg.IsOwner()
}

func (s *Scheduler) shouldStopWork() bool {
	if s.cfg.IsKilled() {
		return true
	}
	s.stateMu.Lock()
	stopping := s.stopping
	s.stateMu.Unlock()
	return stopping
}

func (s *Scheduler) reportError(err error) {
	if err != nil && s.cfg.OnError != nil {
		s.cfg.OnError(err)
	}
}

func (s *Scheduler) GetNextFireTime() time.Time {
	var earliest time.Time
	appendEarliest := func(tasks []Task) {
		for _, task := range tasks {
			next, err := task.NextFireAt()
			if err != nil {
				continue
			}
			if earliest.IsZero() || next.Before(earliest) {
				earliest = next
			}
		}
	}
	if s.cfg.Store != nil {
		tasks, err := s.cfg.Store.List()
		if err == nil {
			appendEarliest(tasks)
		}
	}
	if s.cfg.SessionStore != nil {
		tasks, err := s.cfg.SessionStore.List()
		if err == nil {
			appendEarliest(tasks)
		}
	}
	return earliest
}
