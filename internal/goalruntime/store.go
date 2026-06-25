package goalruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Store struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
}

func NewStore(path string) *Store {
	return &Store{
		path: strings.TrimSpace(path),
		now:  func() time.Time { return time.Now().UTC() },
	}
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) SetClock(now func() time.Time) {
	if s == nil || now == nil {
		return
	}
	s.now = now
}

func (s *Store) Create(spec Spec) (Goal, error) {
	if err := s.configured(); err != nil {
		return Goal{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.loadLocked()
	if err == nil && !IsTerminalStatus(existing.Status) {
		return Goal{}, fmt.Errorf("thread already has unfinished goal %q with status %s", existing.GoalID, existing.Status)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Goal{}, err
	}
	goal, err := NewGoal(spec, s.now())
	if err != nil {
		return Goal{}, err
	}
	if err := s.saveLocked(goal); err != nil {
		return Goal{}, err
	}
	return goal, nil
}

func (s *Store) Load() (Goal, error) {
	if err := s.configured(); err != nil {
		return Goal{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) Save(goal Goal) error {
	if err := s.configured(); err != nil {
		return err
	}
	if strings.TrimSpace(goal.ThreadID) == "" {
		return errors.New("goal thread_id is required")
	}
	if strings.TrimSpace(goal.GoalID) == "" {
		return errors.New("goal_id is required")
	}
	if strings.TrimSpace(goal.Objective) == "" {
		return errors.New("goal objective is required")
	}
	if !IsKnownStatus(goal.Status) {
		return fmt.Errorf("unknown goal runtime status: %s", goal.Status)
	}
	if goal.SchemaVersion == "" {
		goal.SchemaVersion = SchemaVersion
	}
	if goal.UpdatedAt.IsZero() {
		goal.UpdatedAt = s.now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(goal)
}

func (s *Store) Update(fn func(Goal) (Goal, error)) (Goal, error) {
	if err := s.configured(); err != nil {
		return Goal{}, err
	}
	if fn == nil {
		return Goal{}, errors.New("update function is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	goal, err := s.loadLocked()
	if err != nil {
		return Goal{}, err
	}
	goal, err = fn(goal)
	if err != nil {
		return Goal{}, err
	}
	if err := validateGoalForSave(goal); err != nil {
		return Goal{}, err
	}
	if goal.SchemaVersion == "" {
		goal.SchemaVersion = SchemaVersion
	}
	if goal.UpdatedAt.IsZero() {
		goal.UpdatedAt = s.now().UTC()
	}
	if err := s.saveLocked(goal); err != nil {
		return Goal{}, err
	}
	return goal, nil
}

func (s *Store) Clear() error {
	if err := s.configured(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) configured() error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("goal runtime store path is required")
	}
	return nil
}

func (s *Store) loadLocked() (Goal, error) {
	var goal Goal
	data, err := os.ReadFile(s.path)
	if err != nil {
		return Goal{}, err
	}
	if err := json.Unmarshal(data, &goal); err != nil {
		return Goal{}, err
	}
	if err := validateGoalForSave(goal); err != nil {
		return Goal{}, err
	}
	return goal, nil
}

func (s *Store) saveLocked(goal Goal) error {
	if err := validateGoalForSave(goal); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(goal, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".goal-runtime-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

func validateGoalForSave(goal Goal) error {
	if strings.TrimSpace(goal.ThreadID) == "" {
		return errors.New("goal thread_id is required")
	}
	if strings.TrimSpace(goal.GoalID) == "" {
		return errors.New("goal_id is required")
	}
	if strings.TrimSpace(goal.Objective) == "" {
		return errors.New("goal objective is required")
	}
	if !IsKnownStatus(goal.Status) {
		return fmt.Errorf("unknown goal runtime status: %s", goal.Status)
	}
	if goal.TokensUsed < 0 {
		return errors.New("goal tokens_used cannot be negative")
	}
	if goal.TimeUsedSeconds < 0 {
		return errors.New("goal time_used_seconds cannot be negative")
	}
	if goal.GoalTurns < 0 {
		return errors.New("goal goal_turns cannot be negative")
	}
	if goal.BlockerAudit.ConsecutiveTurns < 0 {
		return errors.New("goal blocker consecutive turns cannot be negative")
	}
	return nil
}
