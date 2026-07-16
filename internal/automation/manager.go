package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/cron"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

type Mode string

const (
	ModeNewThread       Mode = "new_thread"
	ModeThreadHeartbeat Mode = "thread_heartbeat"
)

type RunStatus string

const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
)

type Task = cron.Task

type AddTaskParams struct {
	Title             string
	Prompt            string
	Schedule          string
	Timezone          string
	Mode              Mode
	CreatorThreadID   string
	HeartbeatThreadID string
	Recurring         bool
	Durable           bool
}

type ExecutionResult struct {
	Status   RunStatus
	ThreadID string
	TurnID   string
	QueueID  string
}

type Executor interface {
	ExecuteAutomation(context.Context, Task, Run) (ExecutionResult, error)
}

type Config struct {
	StateDir string
	OnError  func(error)
}

type Manager struct {
	stateDir     string
	durableStore *cron.TaskStore
	sessionStore *cron.SessionTaskStore
	runStore     *RunStore
	lock         *cron.Lock
	onError      func(error)

	mu        sync.Mutex
	executor  Executor
	scheduler *cron.Scheduler
	owner     bool
}

func NewManager(cfg Config) *Manager {
	stateDir := strings.TrimSpace(cfg.StateDir)
	return &Manager{
		stateDir:     stateDir,
		durableStore: cron.NewTaskStore(statepath.ScheduledTasksPath(stateDir)),
		sessionStore: cron.NewSessionTaskStore(stateDir),
		runStore:     NewRunStore(statepath.AutomationRunsPath(stateDir)),
		lock:         cron.NewLock(statepath.ScheduledTasksLockPath(stateDir)),
		onError:      cfg.OnError,
	}
}

func (m *Manager) Start(executor Executor) error {
	if m == nil {
		return errors.New("automation manager is required")
	}
	if strings.TrimSpace(m.stateDir) == "" {
		return errors.New("workspace state directory is required")
	}
	if executor == nil {
		return errors.New("automation executor is required")
	}

	m.mu.Lock()
	if m.scheduler != nil {
		m.executor = executor
		m.mu.Unlock()
		return nil
	}
	m.executor = executor
	scheduler := cron.NewScheduler(cron.SchedulerConfig{
		Store:        m.durableStore,
		SessionStore: m.sessionStore,
		OnFire:       m.Fire,
		OnError:      m.reportError,
		IsOwner:      m.ownsDurableTasks,
	})
	m.scheduler = scheduler
	m.mu.Unlock()

	if m.ownsDurableTasks() {
		if err := m.recoverRuns(context.Background(), executor); err != nil {
			m.Stop()
			return err
		}
	}
	scheduler.Start()
	return nil
}

func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	scheduler := m.scheduler
	m.scheduler = nil
	m.executor = nil
	m.mu.Unlock()
	if scheduler != nil {
		scheduler.Stop()
	}
	if m.lock != nil {
		m.lock.Release()
	}
	m.mu.Lock()
	m.owner = false
	m.mu.Unlock()
}

func (m *Manager) AddTask(params AddTaskParams) (Task, error) {
	if m == nil {
		return Task{}, errors.New("automation manager is required")
	}
	prompt := strings.TrimSpace(params.Prompt)
	if prompt == "" {
		return Task{}, errors.New("automation prompt is required")
	}
	schedule := strings.TrimSpace(params.Schedule)
	if schedule == "" {
		return Task{}, errors.New("automation schedule is required")
	}
	if _, err := cron.ParseCronExpression(schedule); err != nil {
		return Task{}, fmt.Errorf("invalid automation schedule: %w", err)
	}
	timezone := strings.TrimSpace(params.Timezone)
	if timezone == "" {
		timezone = time.Local.String()
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return Task{}, fmt.Errorf("invalid automation timezone %q: %w", timezone, err)
	}
	mode := params.Mode
	if mode == "" {
		mode = ModeNewThread
	}
	if mode != ModeNewThread && mode != ModeThreadHeartbeat {
		return Task{}, fmt.Errorf("invalid automation mode %q", mode)
	}
	creatorThreadID := strings.TrimSpace(params.CreatorThreadID)
	heartbeatThreadID := strings.TrimSpace(params.HeartbeatThreadID)
	if mode == ModeThreadHeartbeat {
		if heartbeatThreadID == "" {
			heartbeatThreadID = creatorThreadID
		}
		if heartbeatThreadID == "" {
			return Task{}, errors.New("thread heartbeat automation requires a thread id")
		}
	}
	task := Task{
		ID:                cron.GenerateTaskID(),
		Title:             strings.TrimSpace(params.Title),
		Cron:              schedule,
		Timezone:          timezone,
		Prompt:            prompt,
		Mode:              string(mode),
		CreatorThreadID:   creatorThreadID,
		HeartbeatThreadID: heartbeatThreadID,
		Metadata:          map[string]string{"kind": "prompt"},
		CreatedAt:         time.Now().UnixMilli(),
		Recurring:         params.Recurring,
	}
	if params.Durable {
		task.Metadata["durability"] = "durable"
	} else {
		task.Metadata["durability"] = "session-only"
	}
	if task.Title == "" {
		task.Title = prompt
	}
	if _, err := task.NextFireAt(); err != nil {
		return Task{}, fmt.Errorf("automation has no valid future run: %w", err)
	}
	if err := m.ensureCapacity(); err != nil {
		return Task{}, err
	}
	var store taskAdder = m.sessionStore
	if params.Durable {
		store = m.durableStore
	}
	if err := store.Add(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (m *Manager) ListTasks() ([]Task, error) {
	if m == nil {
		return nil, errors.New("automation manager is required")
	}
	durable, err := m.durableStore.List()
	if err != nil {
		return nil, err
	}
	sessionOnly, err := m.sessionStore.List()
	if err != nil {
		return nil, err
	}
	for index := range durable {
		durable[index] = withTaskDurability(durable[index], "durable")
	}
	for index := range sessionOnly {
		sessionOnly[index] = withTaskDurability(sessionOnly[index], "session-only")
	}
	return append(durable, sessionOnly...), nil
}

func (m *Manager) RemoveTask(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("automation task id is required")
	}
	if err := m.durableStore.Remove(id); err != nil {
		return err
	}
	return m.sessionStore.Remove(id)
}

func (m *Manager) ListRuns() ([]Run, error) {
	if m == nil {
		return nil, errors.New("automation manager is required")
	}
	return m.runStore.List()
}

func (m *Manager) SetPaused(id string, paused bool) (Task, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Task{}, errors.New("automation task id is required")
	}
	if task, found, err := m.durableStore.SetPaused(id, paused); err != nil {
		return Task{}, err
	} else if found {
		return withTaskDurability(task, "durable"), nil
	}
	if task, found, err := m.sessionStore.SetPaused(id, paused); err != nil {
		return Task{}, err
	} else if found {
		return withTaskDurability(task, "session-only"), nil
	}
	return Task{}, fmt.Errorf("automation task %q not found", id)
}

func (m *Manager) Fire(ctx context.Context, task Task) error {
	if m == nil {
		return errors.New("automation manager is required")
	}
	task = normalizeTask(task)
	m.mu.Lock()
	executor := m.executor
	m.mu.Unlock()
	if executor == nil {
		return errors.New("automation executor is not attached")
	}
	run := Run{
		ID:          newRunID(),
		TaskID:      task.ID,
		Task:        task,
		Mode:        task.Mode,
		Status:      RunStatusRunning,
		TriggeredAt: time.Now().UTC(),
	}
	if err := m.runStore.Add(run); err != nil {
		return fmt.Errorf("record automation run: %w", err)
	}
	result, err := executor.ExecuteAutomation(ctx, task, run)
	if err != nil {
		_ = m.runStore.Finish(run.ID, RunStatusFailed, "", "", err.Error())
		return err
	}
	status := result.Status
	if status != RunStatusQueued && status != RunStatusRunning {
		status = RunStatusRunning
	}
	return m.runStore.UpdateAdmission(run.ID, status, result.ThreadID, result.TurnID, result.QueueID)
}

func (m *Manager) CompleteRun(runID, threadID, turnID string, runErr error) error {
	status := RunStatusCompleted
	errorText := ""
	if runErr != nil {
		status = RunStatusFailed
		errorText = runErr.Error()
	}
	return m.runStore.Finish(strings.TrimSpace(runID), status, strings.TrimSpace(threadID), strings.TrimSpace(turnID), errorText)
}

func (m *Manager) MarkRunRunning(runID, threadID, turnID string) error {
	if m == nil {
		return errors.New("automation manager is required")
	}
	return m.runStore.UpdateAdmission(
		strings.TrimSpace(runID), RunStatusRunning,
		strings.TrimSpace(threadID), strings.TrimSpace(turnID), "",
	)
}

func (m *Manager) FailRunIfPending(runID, reason string) error {
	if m == nil {
		return errors.New("automation manager is required")
	}
	return m.runStore.FailIfPending(runID, reason)
}

func (m *Manager) recoverRuns(ctx context.Context, executor Executor) error {
	runs, err := m.runStore.List()
	if err != nil {
		return fmt.Errorf("load automation runs for recovery: %w", err)
	}
	for _, run := range runs {
		switch run.Status {
		case RunStatusRunning:
			if err := m.runStore.Finish(run.ID, RunStatusFailed, run.ThreadID, run.TurnID, "automation stopped before the turn completed"); err != nil {
				return err
			}
		case RunStatusQueued:
			task := normalizeTask(run.Task)
			if strings.TrimSpace(task.ID) == "" {
				task.ID = run.TaskID
			}
			if strings.TrimSpace(task.Prompt) == "" {
				if err := m.runStore.Finish(run.ID, RunStatusFailed, run.ThreadID, run.TurnID, "queued automation payload is unavailable"); err != nil {
					return err
				}
				continue
			}
			result, executeErr := executor.ExecuteAutomation(ctx, task, run)
			if executeErr != nil {
				if err := m.runStore.Finish(run.ID, RunStatusFailed, run.ThreadID, run.TurnID, executeErr.Error()); err != nil {
					return err
				}
				continue
			}
			status := result.Status
			if status != RunStatusQueued && status != RunStatusRunning {
				status = RunStatusRunning
			}
			if err := m.runStore.UpdateAdmission(run.ID, status, result.ThreadID, result.TurnID, result.QueueID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) ensureCapacity() error {
	durable, err := m.durableStore.List()
	if err != nil {
		return err
	}
	sessionOnly, err := m.sessionStore.List()
	if err != nil {
		return err
	}
	if len(durable)+len(sessionOnly) >= cron.MaxJobs {
		return fmt.Errorf("maximum number of automation tasks reached (%d)", cron.MaxJobs)
	}
	return nil
}

func (m *Manager) ownsDurableTasks() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.owner {
		return true
	}
	if m.lock == nil {
		return false
	}
	owner, err := m.lock.TryAcquire()
	if err != nil {
		if m.onError != nil {
			m.onError(fmt.Errorf("acquire automation scheduler lock: %w", err))
		}
		return false
	}
	m.owner = owner
	return owner
}

func (m *Manager) reportError(err error) {
	if err != nil && m.onError != nil {
		m.onError(err)
	}
}

func normalizeTask(task Task) Task {
	if strings.TrimSpace(task.Mode) == "" {
		task.Mode = string(ModeNewThread)
	}
	if strings.TrimSpace(task.Timezone) == "" {
		task.Timezone = time.Local.String()
	}
	return task
}

func withTaskDurability(task Task, durability string) Task {
	metadata := make(map[string]string, len(task.Metadata)+1)
	for key, value := range task.Metadata {
		metadata[key] = value
	}
	metadata["durability"] = durability
	task.Metadata = metadata
	return normalizeTask(task)
}

type taskAdder interface {
	Add(Task) error
}
