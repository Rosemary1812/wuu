package cron

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	MaxJobs            = 50
	RecurringMaxAge    = 7 * 24 * time.Hour
	RecurringJitterCap = 15 * time.Minute
)

type Task struct {
	ID                string            `json:"id"`
	Title             string            `json:"title,omitempty"`
	Cron              string            `json:"cron"`
	Timezone          string            `json:"timezone,omitempty"`
	Prompt            string            `json:"prompt,omitempty"`
	Mode              string            `json:"mode,omitempty"`
	CreatorThreadID   string            `json:"creatorThreadId,omitempty"`
	HeartbeatThreadID string            `json:"heartbeatThreadId,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	CreatedAt         int64             `json:"createdAt"`
	LastFiredAt       int64             `json:"lastFiredAt,omitempty"`
	Recurring         bool              `json:"recurring"`
	Paused            bool              `json:"paused,omitempty"`
}

func (t Task) NextFireAt() (time.Time, error) {
	ce, err := ParseCronExpression(t.Cron)
	if err != nil {
		return time.Time{}, err
	}
	location := time.Local
	if timezone := strings.TrimSpace(t.Timezone); timezone != "" {
		location, err = time.LoadLocation(timezone)
		if err != nil {
			return time.Time{}, fmt.Errorf("load timezone %q: %w", timezone, err)
		}
	}
	anchor := time.Now().In(location)
	if t.LastFiredAt > 0 {
		anchor = time.UnixMilli(t.LastFiredAt).In(location)
	} else if t.CreatedAt > 0 {
		anchor = time.UnixMilli(t.CreatedAt).In(location)
	}
	return JitteredNextRun(ce, t.ID, anchor, t.Recurring)
}

type TaskStore struct {
	path string
}

type SessionTaskStore struct {
	namespace string
}

var sessionTaskState = struct {
	mu    sync.Mutex
	tasks map[string][]Task
}{
	tasks: make(map[string][]Task),
}

func NewTaskStore(path string) *TaskStore {
	return &TaskStore{path: path}
}

func NewSessionTaskStore(namespace string) *SessionTaskStore {
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		ns = "default"
	}
	return &SessionTaskStore{namespace: ns}
}

func (s *TaskStore) load() ([]Task, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var wrapper struct {
		Tasks []Task `json:"tasks"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parse tasks file: %w", err)
	}
	return wrapper.Tasks, nil
}

func (s *TaskStore) save(tasks []Task) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	wrapper := struct {
		Tasks []Task `json:"tasks"`
	}{Tasks: tasks}
	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".scheduled-tasks-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

func (s *TaskStore) List() ([]Task, error) {
	return s.load()
}

func (s *TaskStore) Add(task Task) error {
	return s.update(func(tasks []Task) ([]Task, error) {
		if len(tasks) >= MaxJobs {
			return nil, fmt.Errorf("maximum number of scheduled tasks reached (%d)", MaxJobs)
		}
		for _, existing := range tasks {
			if existing.ID == task.ID {
				return nil, fmt.Errorf("scheduled task %q already exists", task.ID)
			}
		}
		return append(tasks, task), nil
	})
}

func (s *TaskStore) Remove(ids ...string) error {
	return s.update(func(tasks []Task) ([]Task, error) {
		idSet := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			idSet[id] = struct{}{}
		}
		filtered := make([]Task, 0, len(tasks))
		for _, t := range tasks {
			if _, ok := idSet[t.ID]; !ok {
				filtered = append(filtered, t)
			}
		}
		return filtered, nil
	})
}

func (s *TaskStore) SetPaused(id string, paused bool) (Task, bool, error) {
	var updated Task
	found := false
	err := s.updateIfChanged(func(tasks []Task) ([]Task, bool, error) {
		for index := range tasks {
			if tasks[index].ID != id {
				continue
			}
			found = true
			if tasks[index].Paused == paused {
				updated = tasks[index]
				return tasks, false, nil
			}
			tasks[index].Paused = paused
			updated = tasks[index]
			return tasks, true, nil
		}
		return tasks, false, nil
	})
	return updated, found, err
}

// ClaimForDispatch atomically consumes one due task occurrence if the stored
// task still matches the scheduler snapshot. Recurring tasks advance their
// LastFiredAt timestamp; one-shot tasks are removed. A successful claim is an
// at-most-once dispatch attempt: callback failures do not roll it back.
func (s *TaskStore) ClaimForDispatch(expected Task, firedAt int64) (bool, error) {
	claimed := false
	err := s.updateIfChanged(func(tasks []Task) ([]Task, bool, error) {
		index, err := matchingTaskIndex(tasks, expected)
		if err != nil || index < 0 {
			return tasks, false, err
		}
		if expected.Recurring {
			tasks[index].LastFiredAt = firedAt
		} else {
			tasks = append(tasks[:index], tasks[index+1:]...)
		}
		claimed = true
		return tasks, true, nil
	})
	if err != nil {
		return false, err
	}
	return claimed, nil
}

// RemoveIfUnchanged removes an expired task only if it still matches the
// scheduler snapshot. A concurrent edit therefore cannot be discarded.
func (s *TaskStore) RemoveIfUnchanged(expected Task) (bool, error) {
	removed := false
	err := s.updateIfChanged(func(tasks []Task) ([]Task, bool, error) {
		index, err := matchingTaskIndex(tasks, expected)
		if err != nil || index < 0 {
			return tasks, false, err
		}
		tasks = append(tasks[:index], tasks[index+1:]...)
		removed = true
		return tasks, true, nil
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

// update serializes the read-modify-write transaction across app-server
// processes. The lock file is intentionally kept after unlock: removing it
// could let a new caller lock a different inode while another caller is still
// waiting on the old one.
func (s *TaskStore) update(apply func([]Task) ([]Task, error)) error {
	return s.updateIfChanged(func(tasks []Task) ([]Task, bool, error) {
		next, err := apply(tasks)
		return next, err == nil, err
	})
}

func (s *TaskStore) updateIfChanged(apply func([]Task) ([]Task, bool, error)) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := flockExclusive(lock); err != nil {
		return err
	}
	defer flockUnlock(lock)

	tasks, err := s.load()
	if err != nil {
		return err
	}
	next, changed, err := apply(tasks)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return s.save(next)
}

func (s *SessionTaskStore) List() ([]Task, error) {
	sessionTaskState.mu.Lock()
	defer sessionTaskState.mu.Unlock()

	tasks := sessionTaskState.tasks[s.namespace]
	out := make([]Task, len(tasks))
	copy(out, tasks)
	return out, nil
}

func (s *SessionTaskStore) Add(task Task) error {
	sessionTaskState.mu.Lock()
	defer sessionTaskState.mu.Unlock()

	tasks := sessionTaskState.tasks[s.namespace]
	if len(tasks) >= MaxJobs {
		return fmt.Errorf("maximum number of scheduled tasks reached (%d)", MaxJobs)
	}
	for _, existing := range tasks {
		if existing.ID == task.ID {
			return fmt.Errorf("scheduled task %q already exists", task.ID)
		}
	}
	tasks = append(tasks, task)
	sessionTaskState.tasks[s.namespace] = tasks
	return nil
}

func (s *SessionTaskStore) Remove(ids ...string) error {
	sessionTaskState.mu.Lock()
	defer sessionTaskState.mu.Unlock()

	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	tasks := sessionTaskState.tasks[s.namespace]
	filtered := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		if _, ok := idSet[t.ID]; !ok {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		delete(sessionTaskState.tasks, s.namespace)
		return nil
	}
	sessionTaskState.tasks[s.namespace] = filtered
	return nil
}

func (s *SessionTaskStore) SetPaused(id string, paused bool) (Task, bool, error) {
	sessionTaskState.mu.Lock()
	defer sessionTaskState.mu.Unlock()

	tasks := sessionTaskState.tasks[s.namespace]
	for index := range tasks {
		if tasks[index].ID != id {
			continue
		}
		tasks[index].Paused = paused
		sessionTaskState.tasks[s.namespace] = tasks
		return tasks[index], true, nil
	}
	return Task{}, false, nil
}

func (s *SessionTaskStore) ClaimForDispatch(expected Task, firedAt int64) (bool, error) {
	sessionTaskState.mu.Lock()
	defer sessionTaskState.mu.Unlock()

	tasks := sessionTaskState.tasks[s.namespace]
	index, err := matchingTaskIndex(tasks, expected)
	if err != nil || index < 0 {
		return false, err
	}
	if expected.Recurring {
		tasks[index].LastFiredAt = firedAt
		sessionTaskState.tasks[s.namespace] = tasks
	} else {
		tasks = append(tasks[:index], tasks[index+1:]...)
		storeSessionTasks(s.namespace, tasks)
	}
	return true, nil
}

func (s *SessionTaskStore) RemoveIfUnchanged(expected Task) (bool, error) {
	sessionTaskState.mu.Lock()
	defer sessionTaskState.mu.Unlock()

	tasks := sessionTaskState.tasks[s.namespace]
	index, err := matchingTaskIndex(tasks, expected)
	if err != nil || index < 0 {
		return false, err
	}
	tasks = append(tasks[:index], tasks[index+1:]...)
	storeSessionTasks(s.namespace, tasks)
	return true, nil
}

func storeSessionTasks(namespace string, tasks []Task) {
	if len(tasks) == 0 {
		delete(sessionTaskState.tasks, namespace)
		return
	}
	sessionTaskState.tasks[namespace] = tasks
}

func matchingTaskIndex(tasks []Task, expected Task) (int, error) {
	index := -1
	for i, task := range tasks {
		if task.ID != expected.ID {
			continue
		}
		if index >= 0 {
			return -1, fmt.Errorf("duplicate scheduled task id %q", expected.ID)
		}
		index = i
	}
	if index < 0 || !sameTask(tasks[index], expected) {
		return -1, nil
	}
	return index, nil
}

func sameTask(left, right Task) bool {
	return left.ID == right.ID &&
		left.Title == right.Title &&
		left.Cron == right.Cron &&
		left.Timezone == right.Timezone &&
		left.Prompt == right.Prompt &&
		left.Mode == right.Mode &&
		left.CreatorThreadID == right.CreatorThreadID &&
		left.HeartbeatThreadID == right.HeartbeatThreadID &&
		maps.Equal(left.Metadata, right.Metadata) &&
		left.CreatedAt == right.CreatedAt &&
		left.LastFiredAt == right.LastFiredAt &&
		left.Recurring == right.Recurring &&
		left.Paused == right.Paused
}

func GenerateTaskID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func JitteredNextRun(ce CronExpression, taskID string, anchor time.Time, recurring bool) (time.Time, error) {
	next, err := ce.NextRun(anchor)
	if err != nil {
		return time.Time{}, err
	}
	if recurring {
		interval := next.Sub(anchor)
		if interval <= 0 {
			interval = time.Minute
		}
		maxJitter := interval / 10
		if maxJitter > RecurringJitterCap {
			maxJitter = RecurringJitterCap
		}
		jitter := deterministicJitter(taskID, maxJitter)
		next = next.Add(jitter)
	} else {
		if next.Minute()%30 == 0 && next.Second() == 0 {
			jitter := deterministicJitter(taskID, 90*time.Second)
			next = next.Add(-jitter)
			if next.Before(anchor) {
				next = anchor.Add(time.Second)
			}
		}
	}
	return next, nil
}

func deterministicJitter(seed string, max time.Duration) time.Duration {
	h := 0
	for i := 0; i < len(seed); i++ {
		h = h*31 + int(seed[i])
		if h < 0 {
			h = -h
		}
	}
	if max <= 0 {
		return 0
	}
	return time.Duration(h) % max
}

func IsExpired(task Task, nowMillis int64) bool {
	if !task.Recurring {
		return false
	}
	age := time.Duration(nowMillis-task.CreatedAt) * time.Millisecond
	return age > RecurringMaxAge
}

// FindMissedOneShots returns the one-shot tasks whose scheduled time already
// passed — they were due while no scheduler was running (workspace closed,
// process dead) and would otherwise sit silently in the store. The scheduler
// fires them once at startup (Scheduler.catchUpMissedOneShots). Recurring
// tasks are deliberately excluded: they carry no backfill semantics — missed
// occurrences collapse into the single next due fire (see Scheduler.Start).
func FindMissedOneShots(tasks []Task, now time.Time) []Task {
	var missed []Task
	for _, t := range tasks {
		if t.Recurring {
			continue
		}
		ce, err := ParseCronExpression(t.Cron)
		if err != nil {
			continue
		}
		anchor := time.UnixMilli(t.CreatedAt)
		next, err := ce.NextRun(anchor)
		if err != nil {
			continue
		}
		if next.Before(now) {
			missed = append(missed, t)
		}
	}
	return missed
}
