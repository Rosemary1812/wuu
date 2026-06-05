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
	AgentPath     string        `json:"agent_path,omitempty"`
	TaskName      string        `json:"task_name,omitempty"`
	AgentProfile  string        `json:"agent_profile,omitempty"`
	Status        AgentRunState `json:"status"`
	Prompt        string        `json:"prompt,omitempty"`
	Result        string        `json:"result,omitempty"`
	ReportPath    string        `json:"report_path,omitempty"`
	ReportMissing bool          `json:"report_missing,omitempty"`
	ChangedFiles  []string      `json:"changed_files,omitempty"`
	Artifacts     []string      `json:"artifacts,omitempty"`
	WorktreePath  string        `json:"worktree_path,omitempty"`
	InputTokens   int           `json:"input_tokens,omitempty"`
	OutputTokens  int           `json:"output_tokens,omitempty"`
	DurationMS    int64         `json:"duration_ms,omitempty"`
	StartedAt     time.Time     `json:"started_at,omitempty"`
	CompletedAt   time.Time     `json:"completed_at,omitempty"`
	Error         string        `json:"error,omitempty"`
}

type EventType string

const (
	EventRunCreated              EventType = "run_created"
	EventRunStatusChanged        EventType = "run_status_changed"
	EventPhaseStatusChanged      EventType = "phase_status_changed"
	EventAgentRunUpserted        EventType = "agent_run_upserted"
	EventAgentRunStatusChanged   EventType = "agent_run_status_changed"
	EventPlanWritten             EventType = "plan_written"
	EventFinalReportWritten      EventType = "final_report_written"
	EventMemoryCandidateAdded    EventType = "memory_candidate_added"
	EventMemoryCandidateReviewed EventType = "memory_candidate_reviewed"
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.agentDir(agent.WorkflowRunID), 0755); err != nil {
		return err
	}
	path := filepath.Join(s.agentDir(agent.WorkflowRunID), agent.ID+".json")
	var existing AgentRun
	if err := readJSONFile(path, &existing); err == nil {
		if agent.Status == "" {
			agent.Status = existing.Status
		}
		if err := ValidateAgentRunTransition(existing.Status, agent.Status); err != nil {
			return err
		}
		agent = mergeAgentRun(existing, agent)
	} else if os.IsNotExist(err) {
		if agent.Status == "" {
			agent.Status = AgentRunStateQueued
		}
		if err := ValidateAgentRunTransition("", agent.Status); err != nil {
			return err
		}
	} else {
		return err
	}
	if agent.Status == AgentRunStateRunning && agent.StartedAt.IsZero() {
		agent.StartedAt = time.Now().UTC()
	}
	if IsTerminalAgentRunState(agent.Status) && agent.CompletedAt.IsZero() {
		agent.CompletedAt = time.Now().UTC()
	}
	if err := writeJSONFile(path, agent); err != nil {
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

func (s *Store) AttachAgentRunToPhase(runID, phaseID, agentRunID string) (Run, error) {
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return Run{}, err
	}
	agentRunID, err = validateStoreID("workflow agent run", agentRunID)
	if err != nil {
		return Run{}, err
	}
	run, err := s.LoadRun(runID)
	if err != nil {
		return Run{}, err
	}
	for i := range run.Phases {
		if run.Phases[i].ID != phaseID {
			continue
		}
		if !containsString(run.Phases[i].AgentRunIDs, agentRunID) {
			run.Phases[i].AgentRunIDs = append(run.Phases[i].AgentRunIDs, agentRunID)
		}
		if err := s.SaveRun(run); err != nil {
			return Run{}, err
		}
		return run, nil
	}
	return Run{}, fmt.Errorf("workflow phase %q not found", phaseID)
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

func (s *Store) AddMemoryCandidate(candidate MemoryCandidate) (MemoryCandidate, error) {
	if s == nil || s.dir == "" {
		return candidate, nil
	}
	runID, err := validateStoreID("workflow run", candidate.RunID)
	if err != nil {
		return MemoryCandidate{}, err
	}
	if _, err := s.LoadRun(runID); err != nil {
		return MemoryCandidate{}, err
	}
	content := strings.TrimSpace(candidate.Content)
	if content == "" {
		return MemoryCandidate{}, fmt.Errorf("workflow memory candidate content is required")
	}
	target := strings.TrimSpace(candidate.Target)
	if target == "" {
		target = "memory"
	}
	if target != "memory" && target != "user" {
		return MemoryCandidate{}, fmt.Errorf("workflow memory candidate target must be memory or user")
	}
	status := candidate.Status
	if status == "" {
		status = MemoryCandidatePending
	}
	if err := ValidateMemoryCandidateStatus(status); err != nil {
		return MemoryCandidate{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	candidates, err := s.loadMemoryCandidatesLocked(runID)
	if err != nil {
		return MemoryCandidate{}, err
	}
	id := strings.TrimSpace(candidate.ID)
	if id == "" {
		id = nextMemoryCandidateID(candidates)
	}
	id, err = validateStoreID("workflow memory candidate", id)
	if err != nil {
		return MemoryCandidate{}, err
	}
	for _, existing := range candidates {
		if existing.ID == id {
			return MemoryCandidate{}, fmt.Errorf("workflow memory candidate %q already exists", id)
		}
	}
	now := time.Now().UTC()
	candidate.ID = id
	candidate.RunID = runID
	candidate.AgentRunID = strings.TrimSpace(candidate.AgentRunID)
	candidate.AgentProfile = strings.TrimSpace(candidate.AgentProfile)
	candidate.Target = target
	candidate.Content = content
	candidate.Tags = trimStrings(candidate.Tags)
	candidate.Source = strings.TrimSpace(candidate.Source)
	candidate.Status = status
	candidate.Reason = strings.TrimSpace(candidate.Reason)
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = now
	}
	if candidate.Status != MemoryCandidatePending && candidate.ReviewedAt.IsZero() {
		candidate.ReviewedAt = now
	}
	candidates = append(candidates, candidate)
	if err := writeJSONFile(s.memoryCandidatesPath(runID), candidates); err != nil {
		return MemoryCandidate{}, err
	}
	if err := s.appendEventLocked(Event{
		Type:       EventMemoryCandidateAdded,
		RunID:      runID,
		AgentRunID: candidate.AgentRunID,
		Status:     string(candidate.Status),
		Message:    candidate.ID,
	}); err != nil {
		return MemoryCandidate{}, err
	}
	return candidate, nil
}

func (s *Store) ListMemoryCandidates(runID string) ([]MemoryCandidate, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadMemoryCandidatesLocked(runID)
}

func (s *Store) UpdateMemoryCandidateStatus(runID, candidateID string, status MemoryCandidateStatus, reason string) (MemoryCandidate, error) {
	if s == nil || s.dir == "" {
		return MemoryCandidate{}, nil
	}
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return MemoryCandidate{}, err
	}
	candidateID, err = validateStoreID("workflow memory candidate", candidateID)
	if err != nil {
		return MemoryCandidate{}, err
	}
	if err := ValidateMemoryCandidateStatus(status); err != nil {
		return MemoryCandidate{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	candidates, err := s.loadMemoryCandidatesLocked(runID)
	if err != nil {
		return MemoryCandidate{}, err
	}
	for i := range candidates {
		if candidates[i].ID != candidateID {
			continue
		}
		candidates[i].Status = status
		candidates[i].Reason = strings.TrimSpace(reason)
		candidates[i].ReviewedAt = time.Now().UTC()
		if err := writeJSONFile(s.memoryCandidatesPath(runID), candidates); err != nil {
			return MemoryCandidate{}, err
		}
		if err := s.appendEventLocked(Event{
			Type:    EventMemoryCandidateReviewed,
			RunID:   runID,
			Status:  string(status),
			Message: candidateID,
		}); err != nil {
			return MemoryCandidate{}, err
		}
		return candidates[i], nil
	}
	return MemoryCandidate{}, fmt.Errorf("workflow memory candidate %q not found", candidateID)
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

func (s *Store) memoryCandidatesPath(runID string) string {
	return filepath.Join(s.runDir(runID), "memory-candidates.json")
}

func (s *Store) loadMemoryCandidatesLocked(runID string) ([]MemoryCandidate, error) {
	var candidates []MemoryCandidate
	path := s.memoryCandidatesPath(runID)
	if err := readJSONFile(path, &candidates); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return candidates, nil
}

func nextMemoryCandidateID(candidates []MemoryCandidate) string {
	used := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		used[candidate.ID] = struct{}{}
	}
	for i := len(candidates) + 1; ; i++ {
		id := fmt.Sprintf("candidate-%d", i)
		if _, ok := used[id]; !ok {
			return id
		}
	}
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

func trimStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func mergeAgentRun(existing, next AgentRun) AgentRun {
	if next.WorkflowRunID == "" {
		next.WorkflowRunID = existing.WorkflowRunID
	}
	if next.PhaseID == "" {
		next.PhaseID = existing.PhaseID
	}
	if next.AgentID == "" {
		next.AgentID = existing.AgentID
	}
	if next.AgentPath == "" {
		next.AgentPath = existing.AgentPath
	}
	if next.TaskName == "" {
		next.TaskName = existing.TaskName
	}
	if next.AgentProfile == "" {
		next.AgentProfile = existing.AgentProfile
	}
	if next.Prompt == "" {
		next.Prompt = existing.Prompt
	}
	if next.Result == "" {
		next.Result = existing.Result
	}
	if next.ReportPath == "" {
		next.ReportPath = existing.ReportPath
	}
	if next.ReportPath != "" || next.Status == AgentRunStateCompleted {
		next.ReportMissing = false
	} else if !next.ReportMissing {
		next.ReportMissing = existing.ReportMissing
	}
	if next.ChangedFiles == nil {
		next.ChangedFiles = existing.ChangedFiles
	}
	if next.Artifacts == nil {
		next.Artifacts = existing.Artifacts
	}
	if next.WorktreePath == "" {
		next.WorktreePath = existing.WorktreePath
	}
	if next.InputTokens == 0 {
		next.InputTokens = existing.InputTokens
	}
	if next.OutputTokens == 0 {
		next.OutputTokens = existing.OutputTokens
	}
	if next.DurationMS == 0 {
		next.DurationMS = existing.DurationMS
	}
	if next.StartedAt.IsZero() {
		next.StartedAt = existing.StartedAt
	}
	if next.CompletedAt.IsZero() {
		next.CompletedAt = existing.CompletedAt
	}
	if next.Error == "" {
		next.Error = existing.Error
	}
	return next
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
