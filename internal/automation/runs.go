package automation

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Run struct {
	ID          string    `json:"id"`
	TaskID      string    `json:"task_id"`
	Task        Task      `json:"task"`
	Mode        string    `json:"mode"`
	Status      RunStatus `json:"status"`
	TriggeredAt time.Time `json:"triggered_at"`
	CompletedAt time.Time `json:"completed_at,omitzero"`
	ThreadID    string    `json:"thread_id,omitempty"`
	TurnID      string    `json:"turn_id,omitempty"`
	QueueID     string    `json:"queue_id,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// maxStoredRuns bounds the run-history file. Every fire of a recurring task
// appends a record and every mutation rewrites the whole file, so an
// unbounded history degrades quadratically; oldest terminal runs are dropped
// first, non-terminal runs are always kept.
const maxStoredRuns = 500

type RunStore struct {
	path    string
	mu      sync.Mutex
	onError func(error)
}

func NewRunStore(path string) *RunStore {
	return &RunStore{path: strings.TrimSpace(path)}
}

func (s *RunStore) List() ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *RunStore) Add(run Run) error {
	if strings.TrimSpace(run.ID) == "" {
		return errors.New("automation run id is required")
	}
	return s.update(func(runs []Run) ([]Run, error) {
		for _, existing := range runs {
			if existing.ID == run.ID {
				return nil, fmt.Errorf("automation run %q already exists", run.ID)
			}
		}
		return pruneRuns(append(runs, run)), nil
	})
}

func (s *RunStore) UpdateAdmission(id string, status RunStatus, threadID, turnID, queueID string) error {
	return s.mutate(id, func(run *Run) {
		if run.Status == RunStatusCompleted || run.Status == RunStatusFailed {
			return
		}
		run.Status = status
		run.ThreadID = strings.TrimSpace(threadID)
		run.TurnID = strings.TrimSpace(turnID)
		run.QueueID = strings.TrimSpace(queueID)
	})
}

func (s *RunStore) Finish(id string, status RunStatus, threadID, turnID, errorText string) error {
	return s.mutate(id, func(run *Run) {
		run.Status = status
		run.CompletedAt = time.Now().UTC()
		if threadID != "" {
			run.ThreadID = threadID
		}
		if turnID != "" {
			run.TurnID = turnID
		}
		run.Error = strings.TrimSpace(errorText)
	})
}

// FailIfPending is also invoked for queue ids that never belonged to an
// automation run (any queued user turn being discarded), so it only writes
// when a matching non-terminal run exists.
func (s *RunStore) FailIfPending(id, errorText string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, err := s.load()
	if err != nil {
		return err
	}
	for index := range runs {
		if runs[index].ID != id || runs[index].Status == RunStatusCompleted || runs[index].Status == RunStatusFailed {
			continue
		}
		runs[index].Status = RunStatusFailed
		runs[index].CompletedAt = time.Now().UTC()
		runs[index].Error = strings.TrimSpace(errorText)
		return s.save(runs)
	}
	return nil
}

func (s *RunStore) mutate(id string, mutation func(*Run)) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("automation run id is required")
	}
	return s.update(func(runs []Run) ([]Run, error) {
		for index := range runs {
			if runs[index].ID == id {
				mutation(&runs[index])
				return runs, nil
			}
		}
		return nil, fmt.Errorf("automation run %q not found", id)
	})
}

func (s *RunStore) update(change func([]Run) ([]Run, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, err := s.load()
	if err != nil {
		return err
	}
	runs, err = change(runs)
	if err != nil {
		return err
	}
	return s.save(runs)
}

func (s *RunStore) load() ([]Run, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var wrapper struct {
		Runs []Run `json:"runs"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		// Run history is informational; an unreadable file must never take
		// the workspace down (Manager.Start loads it on the app-server
		// startup path). Quarantine the file and continue from empty.
		backup := fmt.Sprintf("%s.corrupt-%d", s.path, time.Now().UnixMilli())
		if renameErr := os.Rename(s.path, backup); renameErr != nil {
			return nil, fmt.Errorf("parse automation runs: %w", err)
		}
		if s.onError != nil {
			s.onError(fmt.Errorf("parse automation runs: %w; corrupt history moved to %s", err, backup))
		}
		return nil, nil
	}
	return wrapper.Runs, nil
}

func (s *RunStore) save(runs []Run) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Runs []Run `json:"runs"`
	}{Runs: runs}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".automation-runs-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

func pruneRuns(runs []Run) []Run {
	if len(runs) <= maxStoredRuns {
		return runs
	}
	overflow := len(runs) - maxStoredRuns
	pruned := make([]Run, 0, maxStoredRuns)
	for _, run := range runs {
		if overflow > 0 && (run.Status == RunStatusCompleted || run.Status == RunStatusFailed) {
			overflow--
			continue
		}
		pruned = append(pruned, run)
	}
	return pruned
}

func newRunID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}
