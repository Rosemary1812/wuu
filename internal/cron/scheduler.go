package cron

import (
	"sync"
	"time"
)

type SchedulerConfig struct {
	Store        *TaskStore
	SessionStore *SessionTaskStore
	OnFire       func(task Task)
	IsOwner      func() bool
	IsKilled     func() bool
}

type Scheduler struct {
	cfg      SchedulerConfig
	ticker   *time.Ticker
	stopCh   chan struct{}
	wg       sync.WaitGroup
	inFlight map[string]struct{}
	mu       sync.Mutex
}

func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.IsKilled == nil {
		cfg.IsKilled = func() bool { return false }
	}
	return &Scheduler{
		cfg:      cfg,
		stopCh:   make(chan struct{}),
		inFlight: make(map[string]struct{}),
	}
}

func (s *Scheduler) Start() {
	s.ticker = time.NewTicker(time.Second)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-s.ticker.C:
				if s.cfg.IsKilled() {
					continue
				}
				s.check()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *Scheduler) Stop() {
	if s.ticker != nil {
		s.ticker.Stop()
	}
	close(s.stopCh)
	s.wg.Wait()
}

func (s *Scheduler) check() {
	now := time.Now()
	ownsDurableTasks := true
	if s.cfg.IsOwner != nil {
		ownsDurableTasks = s.cfg.IsOwner()
	}

	var durableTasks []Task
	if ownsDurableTasks && s.cfg.Store != nil {
		tasks, err := s.cfg.Store.List()
		if err != nil {
			return
		}
		durableTasks = tasks
	}

	var sessionTasks []Task
	if s.cfg.SessionStore != nil {
		tasks, err := s.cfg.SessionStore.List()
		if err != nil {
			return
		}
		sessionTasks = tasks
	}

	var durableToUpdate []string
	var durableToRemove []string
	var sessionToUpdate []string
	var sessionToRemove []string

	process := func(task Task, sessionOnly bool) {
		if s.cfg.IsKilled() {
			return
		}
		taskKey := task.ID
		if sessionOnly {
			taskKey = "session:" + task.ID
		}

		s.mu.Lock()
		if _, busy := s.inFlight[taskKey]; busy {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		if task.Recurring && IsExpired(task, now.UnixMilli()) {
			if sessionOnly {
				sessionToRemove = append(sessionToRemove, task.ID)
			} else {
				durableToRemove = append(durableToRemove, task.ID)
			}
			return
		}

		next, err := task.NextFireAt()
		if err != nil {
			return
		}

		if now.Before(next) {
			return
		}

		s.mu.Lock()
		s.inFlight[taskKey] = struct{}{}
		s.mu.Unlock()

		go func(t Task, key string) {
			defer func() {
				s.mu.Lock()
				delete(s.inFlight, key)
				s.mu.Unlock()
			}()
			if s.cfg.OnFire != nil {
				s.cfg.OnFire(t)
			}
		}(task, taskKey)

		if task.Recurring {
			if sessionOnly {
				sessionToUpdate = append(sessionToUpdate, task.ID)
			} else {
				durableToUpdate = append(durableToUpdate, task.ID)
			}
		} else {
			if sessionOnly {
				sessionToRemove = append(sessionToRemove, task.ID)
			} else {
				durableToRemove = append(durableToRemove, task.ID)
			}
		}
	}

	for _, task := range durableTasks {
		process(task, false)
	}
	for _, task := range sessionTasks {
		process(task, true)
	}

	if len(durableToUpdate) > 0 && s.cfg.Store != nil {
		s.cfg.Store.UpdateLastFired(durableToUpdate, now.UnixMilli())
	}
	if len(durableToRemove) > 0 && s.cfg.Store != nil {
		s.cfg.Store.Remove(durableToRemove...)
	}
	if len(sessionToUpdate) > 0 && s.cfg.SessionStore != nil {
		s.cfg.SessionStore.UpdateLastFired(sessionToUpdate, now.UnixMilli())
	}
	if len(sessionToRemove) > 0 && s.cfg.SessionStore != nil {
		s.cfg.SessionStore.Remove(sessionToRemove...)
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
