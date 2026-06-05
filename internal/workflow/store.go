package workflow

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Run struct {
	ID              string    `json:"id"`
	DefinitionName  string    `json:"definition_name,omitempty"`
	DefinitionPath  string    `json:"definition_path,omitempty"`
	Arguments       string    `json:"arguments,omitempty"`
	Status          RunState  `json:"status"`
	Phases          []Phase   `json:"phases,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	PlanPath        string    `json:"plan_path,omitempty"`
	FinalReportPath string    `json:"final_report_path,omitempty"`
	Error           string    `json:"error,omitempty"`
}

type Phase struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Status      PhaseState `json:"status"`
	AgentRunIDs []string   `json:"agent_run_ids,omitempty"`
	StartedAt   time.Time  `json:"started_at,omitempty"`
	CompletedAt time.Time  `json:"completed_at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type AgentRun struct {
	ID            string        `json:"id"`
	WorkflowRunID string        `json:"workflow_run_id"`
	PhaseID       string        `json:"phase_id,omitempty"`
	AgentID       string        `json:"agent_id,omitempty"`
	TaskName      string        `json:"task_name,omitempty"`
	AgentProfile  string        `json:"agent_profile,omitempty"`
	Status        AgentRunState `json:"status"`
	Prompt        string        `json:"prompt,omitempty"`
	ReportPath    string        `json:"report_path,omitempty"`
	ChangedFiles  []string      `json:"changed_files,omitempty"`
	Artifacts     []string      `json:"artifacts,omitempty"`
	StartedAt     time.Time     `json:"started_at,omitempty"`
	CompletedAt   time.Time     `json:"completed_at,omitempty"`
	Error         string        `json:"error,omitempty"`
}

type EventType string

const (
	EventRunCreated            EventType = "run_created"
	EventRunStatusChanged      EventType = "run_status_changed"
	EventPhaseStatusChanged    EventType = "phase_status_changed"
	EventAgentRunUpserted      EventType = "agent_run_upserted"
	EventAgentRunStatusChanged EventType = "agent_run_status_changed"
	EventPlanWritten           EventType = "plan_written"
	EventFinalReportWritten    EventType = "final_report_written"
)

type Event struct {
	Type       EventType `json:"type"`
	RunID      string    `json:"run_id"`
	PhaseID    string    `json:"phase_id,omitempty"`
	AgentRunID string    `json:"agent_run_id,omitempty"`
	Status     string    `json:"status,omitempty"`
	Message    string    `json:"message,omitempty"`
	Artifact   string    `json:"artifact,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dir string) *Store {
	return &Store{dir: strings.TrimSpace(dir)}
}

func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

func (s *Store) CreateRun(run Run) (Run, error) {
	runID, err := validateStoreID("workflow run", run.ID)
	if err != nil {
		return Run{}, err
	}
	run.ID = runID
	if run.Status == "" {
		run.Status = RunStateDraft
	}
	if err := ValidateRunTransition("", run.Status); err != nil {
		return Run{}, err
	}
	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = now
	}
	if run.Status == RunStateRunning && run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	if IsTerminalRunState(run.Status) && run.CompletedAt.IsZero() {
		run.CompletedAt = now
	}
	if s != nil && s.dir != "" {
		runPath := filepath.Join(s.runDir(run.ID), "run.json")
		if _, err := os.Stat(runPath); err == nil {
			return Run{}, fmt.Errorf("workflow run %q already exists", run.ID)
		} else if !os.IsNotExist(err) {
			return Run{}, err
		}
	}
	if err := s.SaveRun(run); err != nil {
		return Run{}, err
	}
	if err := s.AppendEvent(Event{Type: EventRunCreated, RunID: run.ID, Status: string(run.Status)}); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *Store) SaveRun(run Run) error {
	if s == nil || s.dir == "" {
		return nil
	}
	runID, err := validateStoreID("workflow run", run.ID)
	if err != nil {
		return err
	}
	run.ID = runID
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.runDir(run.ID), 0755); err != nil {
		return err
	}
	run.UpdatedAt = time.Now().UTC()
	return writeJSONFile(filepath.Join(s.runDir(run.ID), "run.json"), run)
}

func (s *Store) LoadRun(runID string) (Run, error) {
	if s == nil || s.dir == "" {
		return Run{}, fmt.Errorf("workflow store not configured")
	}
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return Run{}, err
	}
	var run Run
	if err := readJSONFile(filepath.Join(s.runDir(runID), "run.json"), &run); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *Store) ListRuns() ([]Run, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}
	root := filepath.Join(s.dir, "workflows")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	runs := make([]Run, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		run, err := s.LoadRun(entry.Name())
		if err != nil {
			continue
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].CreatedAt.Before(runs[j].CreatedAt)
	})
	return runs, nil
}

func (s *Store) UpdateRunStatus(runID string, status RunState, message string) (Run, error) {
	run, err := s.LoadRun(runID)
	if err != nil {
		return Run{}, err
	}
	if err := ValidateRunTransition(run.Status, status); err != nil {
		return Run{}, err
	}
	now := time.Now().UTC()
	run.Status = status
	run.UpdatedAt = now
	if status == RunStateRunning && run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	if IsTerminalRunState(status) && run.CompletedAt.IsZero() {
		run.CompletedAt = now
	}
	if status == RunStateFailed {
		run.Error = strings.TrimSpace(message)
	}
	if err := s.SaveRun(run); err != nil {
		return Run{}, err
	}
	if err := s.AppendEvent(Event{Type: EventRunStatusChanged, RunID: run.ID, Status: string(status), Message: message}); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *Store) UpdatePhaseStatus(runID, phaseID string, status PhaseState, message string) (Run, error) {
	run, err := s.LoadRun(runID)
	if err != nil {
		return Run{}, err
	}
	idx := -1
	for i := range run.Phases {
		if run.Phases[i].ID == phaseID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Run{}, fmt.Errorf("workflow phase %q not found", phaseID)
	}
	if err := ValidatePhaseTransition(run.Phases[idx].Status, status); err != nil {
		return Run{}, err
	}
	now := time.Now().UTC()
	run.Phases[idx].Status = status
	if status == PhaseStateRunning && run.Phases[idx].StartedAt.IsZero() {
		run.Phases[idx].StartedAt = now
	}
	if IsTerminalPhaseState(status) && run.Phases[idx].CompletedAt.IsZero() {
		run.Phases[idx].CompletedAt = now
	}
	if status == PhaseStateFailed || status == PhaseStateBlocked {
		run.Phases[idx].Error = strings.TrimSpace(message)
	}
	if err := s.SaveRun(run); err != nil {
		return Run{}, err
	}
	if err := s.AppendEvent(Event{Type: EventPhaseStatusChanged, RunID: run.ID, PhaseID: phaseID, Status: string(status), Message: message}); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *Store) UpsertAgentRun(agent AgentRun) error {
	if s == nil || s.dir == "" {
		return nil
	}
	runID, err := validateStoreID("workflow run", agent.WorkflowRunID)
	if err != nil {
		return err
	}
	agentID, err := validateStoreID("workflow agent run", agent.ID)
	if err != nil {
		return err
	}
	agent.WorkflowRunID = runID
	agent.ID = agentID
	if agent.Status == "" {
		agent.Status = AgentRunStateQueued
	}
	if err := ValidateAgentRunTransition("", agent.Status); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.agentDir(agent.WorkflowRunID), 0755); err != nil {
		return err
	}
	if agent.Status == AgentRunStateRunning && agent.StartedAt.IsZero() {
		agent.StartedAt = time.Now().UTC()
	}
	if IsTerminalAgentRunState(agent.Status) && agent.CompletedAt.IsZero() {
		agent.CompletedAt = time.Now().UTC()
	}
	if err := writeJSONFile(filepath.Join(s.agentDir(agent.WorkflowRunID), agent.ID+".json"), agent); err != nil {
		return err
	}
	return s.appendEventLocked(Event{Type: EventAgentRunUpserted, RunID: agent.WorkflowRunID, PhaseID: agent.PhaseID, AgentRunID: agent.ID, Status: string(agent.Status)})
}

func (s *Store) LoadAgentRun(runID, agentRunID string) (AgentRun, error) {
	if s == nil || s.dir == "" {
		return AgentRun{}, fmt.Errorf("workflow store not configured")
	}
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return AgentRun{}, err
	}
	agentRunID, err = validateStoreID("workflow agent run", agentRunID)
	if err != nil {
		return AgentRun{}, err
	}
	var agent AgentRun
	if err := readJSONFile(filepath.Join(s.agentDir(runID), agentRunID+".json"), &agent); err != nil {
		return AgentRun{}, err
	}
	return agent, nil
}

func (s *Store) ListAgentRuns(runID string) ([]AgentRun, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return nil, err
	}
	dir := s.agentDir(runID)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	agents := make([]AgentRun, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		var agent AgentRun
		if err := readJSONFile(filepath.Join(dir, entry.Name()), &agent); err != nil {
			continue
		}
		agents = append(agents, agent)
	}
	sort.Slice(agents, func(i, j int) bool {
		if agents[i].PhaseID == agents[j].PhaseID {
			if agents[i].TaskName == agents[j].TaskName {
				return agents[i].ID < agents[j].ID
			}
			return agents[i].TaskName < agents[j].TaskName
		}
		return agents[i].PhaseID < agents[j].PhaseID
	})
	return agents, nil
}

func (s *Store) UpdateAgentRunStatus(runID, agentRunID string, status AgentRunState, message string) (AgentRun, error) {
	agent, err := s.LoadAgentRun(runID, agentRunID)
	if err != nil {
		return AgentRun{}, err
	}
	if err := ValidateAgentRunTransition(agent.Status, status); err != nil {
		return AgentRun{}, err
	}
	now := time.Now().UTC()
	agent.Status = status
	if status == AgentRunStateRunning && agent.StartedAt.IsZero() {
		agent.StartedAt = now
	}
	if IsTerminalAgentRunState(status) && agent.CompletedAt.IsZero() {
		agent.CompletedAt = now
	}
	if status == AgentRunStateFailed {
		agent.Error = strings.TrimSpace(message)
	}
	if err := s.UpsertAgentRun(agent); err != nil {
		return AgentRun{}, err
	}
	if err := s.AppendEvent(Event{Type: EventAgentRunStatusChanged, RunID: runID, PhaseID: agent.PhaseID, AgentRunID: agentRunID, Status: string(status), Message: message}); err != nil {
		return AgentRun{}, err
	}
	return agent, nil
}

func (s *Store) WritePlan(runID, content string) (string, error) {
	if s == nil || s.dir == "" {
		return "", nil
	}
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.runDir(runID), "plan.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	run, err := s.LoadRun(runID)
	if err == nil {
		run.PlanPath = path
		_ = s.SaveRun(run)
	}
	return path, s.AppendEvent(Event{Type: EventPlanWritten, RunID: runID, Artifact: path})
}

func (s *Store) WriteFinalReport(runID, content string) (string, error) {
	if s == nil || s.dir == "" {
		return "", nil
	}
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.runDir(runID), "final-report.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	run, err := s.LoadRun(runID)
	if err == nil {
		run.FinalReportPath = path
		_ = s.SaveRun(run)
	}
	return path, s.AppendEvent(Event{Type: EventFinalReportWritten, RunID: runID, Artifact: path})
}

func (s *Store) AppendEvent(event Event) error {
	if s == nil || s.dir == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendEventLocked(event)
}

func (s *Store) ListEvents(runID string) ([]Event, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.runDir(runID), "events.jsonl")
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var events []Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

func (s *Store) appendEventLocked(event Event) error {
	runID, err := validateStoreID("workflow event run", event.RunID)
	if err != nil {
		return err
	}
	event.RunID = runID
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(s.runDir(event.RunID), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	path := filepath.Join(s.runDir(event.RunID), "events.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *Store) runDir(runID string) string {
	return filepath.Join(s.dir, "workflows", strings.TrimSpace(runID))
}

func (s *Store) agentDir(runID string) string {
	return filepath.Join(s.runDir(runID), "agents")
}

func validateStoreID(kind, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("%s id is required", kind)
	}
	if id == "." || id == ".." {
		return "", fmt.Errorf("%s id %q is invalid", kind, id)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return "", fmt.Errorf("%s id %q contains invalid character %q", kind, id, r)
		}
	}
	return id, nil
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSONFile(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
