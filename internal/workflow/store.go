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
	Driver          string    `json:"driver,omitempty"`
	Entrypoint      string    `json:"entrypoint,omitempty"`
	Status          RunState  `json:"status"`
	Phases          []Phase   `json:"phases,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	PlanPath        string    `json:"plan_path,omitempty"`
	ScriptPath      string    `json:"script_path,omitempty"`
	FinalReportPath string    `json:"final_report_path,omitempty"`
	PauseReason     string    `json:"pause_reason,omitempty"`
	ResumeHint      string    `json:"resume_hint,omitempty"`
	RollbackHint    string    `json:"rollback_hint,omitempty"`
	Error           string    `json:"error,omitempty"`
}

const (
	RunDriverAgentManaged             = "agent_managed"
	RunDriverScript                   = "script"
	RunEntrypointNaturalLanguageAgent = "natural_language_agent"
)

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
	RetryCount    int           `json:"retry_count,omitempty"`
	MaxRetries    int           `json:"max_retries,omitempty"`
	RetryReason   string        `json:"retry_reason,omitempty"`
	RollbackHint  string        `json:"rollback_hint,omitempty"`
	StartedAt     time.Time     `json:"started_at,omitempty"`
	CompletedAt   time.Time     `json:"completed_at,omitempty"`
	Error         string        `json:"error,omitempty"`
}

type TeamMemberMode string

const (
	TeamMemberReuseProfile  TeamMemberMode = "reuse_profile"
	TeamMemberCreateProfile TeamMemberMode = "create_profile"
	TeamMemberEphemeral     TeamMemberMode = "ephemeral"
)

type TeamPlan struct {
	RunID     string       `json:"run_id"`
	Members   []TeamMember `json:"members,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type TeamMember struct {
	ID             string         `json:"id"`
	Role           string         `json:"role"`
	Mode           TeamMemberMode `json:"mode"`
	AgentProfile   string         `json:"agent_profile,omitempty"`
	TaskName       string         `json:"task_name,omitempty"`
	PhaseID        string         `json:"phase_id,omitempty"`
	Prompt         string         `json:"prompt,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	CreatedProfile bool           `json:"created_profile,omitempty"`
}

type RunStatusUpdate struct {
	Status       RunState
	Message      string
	PauseReason  string
	ResumeHint   string
	RollbackHint string
}

type AgentRetryRequest struct {
	Reason       string
	MaxRetries   int
	RollbackHint string
}

type FileCheckpoint struct {
	ID         string               `json:"id"`
	RunID      string               `json:"run_id"`
	Reason     string               `json:"reason,omitempty"`
	Files      []FileCheckpointFile `json:"files,omitempty"`
	CreatedAt  time.Time            `json:"created_at"`
	RestoredAt time.Time            `json:"restored_at,omitempty"`
}

type FileCheckpointFile struct {
	Path         string `json:"path"`
	Existed      bool   `json:"existed"`
	SnapshotPath string `json:"snapshot_path,omitempty"`
	Size         int64  `json:"size,omitempty"`
}

type EventType string

const (
	EventRunCreated              EventType = "run_created"
	EventRunStatusChanged        EventType = "run_status_changed"
	EventPhaseStatusChanged      EventType = "phase_status_changed"
	EventAgentRunUpserted        EventType = "agent_run_upserted"
	EventAgentRunStatusChanged   EventType = "agent_run_status_changed"
	EventAgentRunRetryRequested  EventType = "agent_run_retry_requested"
	EventPlanWritten             EventType = "plan_written"
	EventScriptWritten           EventType = "script_written"
	EventFinalReportWritten      EventType = "final_report_written"
	EventMemoryCandidateAdded    EventType = "memory_candidate_added"
	EventMemoryCandidateReviewed EventType = "memory_candidate_reviewed"
	EventFileCheckpointCreated   EventType = "file_checkpoint_created"
	EventFileCheckpointRestored  EventType = "file_checkpoint_restored"
	EventTeamPlanRecorded        EventType = "team_plan_recorded"
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
	if strings.TrimSpace(run.Entrypoint) == "" && strings.TrimSpace(run.Driver) != "" {
		run.Entrypoint = RunEntrypointNaturalLanguageAgent
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
	return s.UpdateRunStatusWithDetails(runID, RunStatusUpdate{Status: status, Message: message})
}

func (s *Store) UpdateRunStatusWithDetails(runID string, update RunStatusUpdate) (Run, error) {
	run, err := s.LoadRun(runID)
	if err != nil {
		return Run{}, err
	}
	if err := ValidateRunTransition(run.Status, update.Status); err != nil {
		return Run{}, err
	}
	now := time.Now().UTC()
	run.Status = update.Status
	run.UpdatedAt = now
	if update.Status == RunStateRunning && run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	if IsTerminalRunState(update.Status) && run.CompletedAt.IsZero() {
		run.CompletedAt = now
	}
	switch update.Status {
	case RunStatePaused:
		run.PauseReason = firstNonEmpty(update.PauseReason, update.Message)
		run.ResumeHint = strings.TrimSpace(update.ResumeHint)
		run.RollbackHint = strings.TrimSpace(update.RollbackHint)
	case RunStateRunning, RunStateCompleted, RunStateCancelled:
		run.PauseReason = ""
		run.ResumeHint = ""
		run.RollbackHint = ""
	}
	if update.Status == RunStateFailed {
		run.Error = strings.TrimSpace(update.Message)
	}
	if err := s.SaveRun(run); err != nil {
		return Run{}, err
	}
	eventMessage := strings.TrimSpace(update.Message)
	if eventMessage == "" {
		eventMessage = strings.TrimSpace(update.PauseReason)
	}
	if err := s.AppendEvent(Event{Type: EventRunStatusChanged, RunID: run.ID, Status: string(update.Status), Message: eventMessage}); err != nil {
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
	if IsTerminalPhaseState(status) && run.Phases[idx].StartedAt.IsZero() {
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

func (s *Store) EnsurePhase(runID string, phase Phase) (Run, error) {
	run, err := s.LoadRun(runID)
	if err != nil {
		return Run{}, err
	}
	phaseID, err := validateStoreID("workflow phase", phase.ID)
	if err != nil {
		return Run{}, err
	}
	phase.ID = phaseID
	if strings.TrimSpace(phase.Name) == "" {
		phase.Name = phase.ID
	}
	if phase.Status == "" {
		phase.Status = PhaseStateRunnable
	}
	if err := ValidatePhaseTransition("", phase.Status); err != nil {
		return Run{}, err
	}
	for i := range run.Phases {
		if run.Phases[i].ID == phase.ID {
			return run, nil
		}
	}
	run.Phases = append(run.Phases, phase)
	if err := s.SaveRun(run); err != nil {
		return Run{}, err
	}
	if err := s.AppendEvent(Event{Type: EventPhaseStatusChanged, RunID: run.ID, PhaseID: phase.ID, Status: string(phase.Status), Message: "phase created"}); err != nil {
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
	if err := validateAgentRetryBounds(agent); err != nil {
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

func (s *Store) SaveTeamPlan(plan TeamPlan) (TeamPlan, error) {
	if s == nil || s.dir == "" {
		return plan, nil
	}
	runID, err := validateStoreID("workflow run", plan.RunID)
	if err != nil {
		return TeamPlan{}, err
	}
	plan.RunID = runID
	now := time.Now().UTC()
	var existing TeamPlan
	path := s.teamPlanPath(runID)
	if err := readJSONFile(path, &existing); err == nil {
		if !existing.CreatedAt.IsZero() {
			plan.CreatedAt = existing.CreatedAt
		}
	} else if !os.IsNotExist(err) {
		return TeamPlan{}, err
	}
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = now
	}
	plan.UpdatedAt = now
	if err := validateTeamPlan(plan); err != nil {
		return TeamPlan{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeJSONFile(path, plan); err != nil {
		return TeamPlan{}, err
	}
	if err := s.appendEventLocked(Event{Type: EventTeamPlanRecorded, RunID: plan.RunID, Message: fmt.Sprintf("%d members", len(plan.Members))}); err != nil {
		return TeamPlan{}, err
	}
	return plan, nil
}

func (s *Store) LoadTeamPlan(runID string) (TeamPlan, error) {
	if s == nil || s.dir == "" {
		return TeamPlan{}, nil
	}
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return TeamPlan{}, err
	}
	var plan TeamPlan
	if err := readJSONFile(s.teamPlanPath(runID), &plan); err != nil {
		if os.IsNotExist(err) {
			return TeamPlan{RunID: runID}, nil
		}
		return TeamPlan{}, err
	}
	return plan, nil
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

func (s *Store) RequestAgentRunRetry(runID, agentRunID string, request AgentRetryRequest) (AgentRun, error) {
	agent, err := s.LoadAgentRun(runID, agentRunID)
	if err != nil {
		return AgentRun{}, err
	}
	if err := ValidateAgentRunTransition(agent.Status, AgentRunStateRetrying); err != nil {
		return AgentRun{}, err
	}
	maxRetries := request.MaxRetries
	if maxRetries <= 0 {
		maxRetries = agent.MaxRetries
	}
	if maxRetries <= 0 {
		maxRetries = 2
	}
	if agent.RetryCount >= maxRetries {
		return AgentRun{}, fmt.Errorf("workflow agent run %q retry limit reached (%d/%d)", agent.ID, agent.RetryCount, maxRetries)
	}
	agent.Status = AgentRunStateRetrying
	agent.RetryCount++
	agent.MaxRetries = maxRetries
	agent.RetryReason = firstNonEmpty(request.Reason, agent.Error)
	agent.RollbackHint = strings.TrimSpace(request.RollbackHint)
	if err := s.UpsertAgentRun(agent); err != nil {
		return AgentRun{}, err
	}
	if err := s.AppendEvent(Event{
		Type:       EventAgentRunRetryRequested,
		RunID:      runID,
		PhaseID:    agent.PhaseID,
		AgentRunID: agent.ID,
		Status:     string(AgentRunStateRetrying),
		Message:    agent.RetryReason,
	}); err != nil {
		return AgentRun{}, err
	}
	return s.LoadAgentRun(runID, agentRunID)
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
		if strings.TrimSpace(run.Driver) == "" {
			run.Driver = RunDriverAgentManaged
		}
		if strings.TrimSpace(run.Entrypoint) == "" {
			run.Entrypoint = RunEntrypointNaturalLanguageAgent
		}
		_ = s.SaveRun(run)
	}
	return path, s.AppendEvent(Event{Type: EventPlanWritten, RunID: runID, Artifact: path})
}

func (s *Store) WriteScript(runID, content string) (string, error) {
	if s == nil || s.dir == "" {
		return "", nil
	}
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.runDir(runID), "workflow.js")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	run, err := s.LoadRun(runID)
	if err == nil {
		run.ScriptPath = path
		if strings.TrimSpace(run.Driver) == "" {
			run.Driver = RunDriverScript
		}
		if strings.TrimSpace(run.Entrypoint) == "" {
			run.Entrypoint = RunEntrypointNaturalLanguageAgent
		}
		_ = s.SaveRun(run)
	}
	return path, s.AppendEvent(Event{Type: EventScriptWritten, RunID: runID, Artifact: path})
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

func (s *Store) CheckpointDir(runID, checkpointID string) (string, error) {
	if s == nil || s.dir == "" {
		return "", fmt.Errorf("workflow store not configured")
	}
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return "", err
	}
	checkpointID, err = validateStoreID("workflow file checkpoint", checkpointID)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.runDir(runID), "checkpoints", checkpointID), nil
}

func (s *Store) SaveFileCheckpoint(checkpoint FileCheckpoint) (FileCheckpoint, error) {
	if s == nil || s.dir == "" {
		return checkpoint, nil
	}
	runID, err := validateStoreID("workflow run", checkpoint.RunID)
	if err != nil {
		return FileCheckpoint{}, err
	}
	if _, err := s.LoadRun(runID); err != nil {
		return FileCheckpoint{}, err
	}
	checkpointID, err := validateStoreID("workflow file checkpoint", checkpoint.ID)
	if err != nil {
		return FileCheckpoint{}, err
	}
	checkpoint.RunID = runID
	checkpoint.ID = checkpointID
	checkpoint.Reason = strings.TrimSpace(checkpoint.Reason)
	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = time.Now().UTC()
	}
	for i := range checkpoint.Files {
		checkpoint.Files[i].Path = strings.TrimSpace(checkpoint.Files[i].Path)
		checkpoint.Files[i].SnapshotPath = strings.TrimSpace(checkpoint.Files[i].SnapshotPath)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.runDir(runID), "checkpoints", checkpointID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return FileCheckpoint{}, err
	}
	path := filepath.Join(dir, "checkpoint.json")
	if _, err := os.Stat(path); err == nil {
		return FileCheckpoint{}, fmt.Errorf("workflow file checkpoint %q already exists", checkpointID)
	} else if !os.IsNotExist(err) {
		return FileCheckpoint{}, err
	}
	if err := writeJSONFile(path, checkpoint); err != nil {
		return FileCheckpoint{}, err
	}
	if err := s.appendEventLocked(Event{
		Type:     EventFileCheckpointCreated,
		RunID:    runID,
		Status:   checkpointID,
		Message:  checkpoint.Reason,
		Artifact: path,
	}); err != nil {
		return FileCheckpoint{}, err
	}
	return checkpoint, nil
}

func (s *Store) LoadFileCheckpoint(runID, checkpointID string) (FileCheckpoint, error) {
	if s == nil || s.dir == "" {
		return FileCheckpoint{}, fmt.Errorf("workflow store not configured")
	}
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return FileCheckpoint{}, err
	}
	checkpointID, err = validateStoreID("workflow file checkpoint", checkpointID)
	if err != nil {
		return FileCheckpoint{}, err
	}
	var checkpoint FileCheckpoint
	if err := readJSONFile(filepath.Join(s.runDir(runID), "checkpoints", checkpointID, "checkpoint.json"), &checkpoint); err != nil {
		return FileCheckpoint{}, err
	}
	return checkpoint, nil
}

func (s *Store) ListFileCheckpoints(runID string) ([]FileCheckpoint, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}
	runID, err := validateStoreID("workflow run", runID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(s.runDir(runID), "checkpoints")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	checkpoints := make([]FileCheckpoint, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		checkpoint, err := s.LoadFileCheckpoint(runID, entry.Name())
		if err != nil {
			continue
		}
		checkpoints = append(checkpoints, checkpoint)
	}
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].CreatedAt.Before(checkpoints[j].CreatedAt)
	})
	return checkpoints, nil
}

func (s *Store) MarkFileCheckpointRestored(runID, checkpointID, message string) (FileCheckpoint, error) {
	checkpoint, err := s.LoadFileCheckpoint(runID, checkpointID)
	if err != nil {
		return FileCheckpoint{}, err
	}
	checkpoint.RestoredAt = time.Now().UTC()
	dir, err := s.CheckpointDir(runID, checkpointID)
	if err != nil {
		return FileCheckpoint{}, err
	}
	if err := writeJSONFile(filepath.Join(dir, "checkpoint.json"), checkpoint); err != nil {
		return FileCheckpoint{}, err
	}
	if err := s.AppendEvent(Event{
		Type:     EventFileCheckpointRestored,
		RunID:    checkpoint.RunID,
		Status:   checkpoint.ID,
		Message:  strings.TrimSpace(message),
		Artifact: filepath.Join(dir, "checkpoint.json"),
	}); err != nil {
		return FileCheckpoint{}, err
	}
	return checkpoint, nil
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

func (s *Store) teamPlanPath(runID string) string {
	return filepath.Join(s.runDir(runID), "team-plan.json")
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

func validateTeamPlan(plan TeamPlan) error {
	if _, err := validateStoreID("workflow run", plan.RunID); err != nil {
		return err
	}
	ids := make(map[string]struct{}, len(plan.Members))
	for _, member := range plan.Members {
		id, err := validateStoreID("workflow team member", member.ID)
		if err != nil {
			return err
		}
		if _, exists := ids[id]; exists {
			return fmt.Errorf("workflow team member id %q is duplicated", id)
		}
		ids[id] = struct{}{}
		if strings.TrimSpace(member.Role) == "" {
			return fmt.Errorf("workflow team member %q requires role", id)
		}
		switch member.Mode {
		case TeamMemberReuseProfile, TeamMemberCreateProfile:
			if strings.TrimSpace(member.AgentProfile) == "" {
				return fmt.Errorf("workflow team member %q mode %q requires agent_profile", id, member.Mode)
			}
		case TeamMemberEphemeral:
			if strings.TrimSpace(member.AgentProfile) != "" {
				return fmt.Errorf("workflow team member %q mode ephemeral must not set agent_profile", id)
			}
		default:
			return fmt.Errorf("workflow team member %q has unsupported mode %q", id, member.Mode)
		}
	}
	return nil
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
	if next.MaxRetries == 0 {
		next.MaxRetries = existing.MaxRetries
	}
	if next.Status == AgentRunStateRetrying && next.RetryCount == 0 {
		next.RetryCount = existing.RetryCount + 1
	} else if next.RetryCount == 0 {
		next.RetryCount = existing.RetryCount
	}
	if next.RetryReason == "" {
		next.RetryReason = existing.RetryReason
	}
	if next.RollbackHint == "" {
		next.RollbackHint = existing.RollbackHint
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

func validateAgentRetryBounds(agent AgentRun) error {
	if agent.Status != AgentRunStateRetrying || agent.MaxRetries <= 0 {
		return nil
	}
	if agent.RetryCount > agent.MaxRetries {
		return fmt.Errorf("workflow agent run %q retry limit exceeded (%d/%d)", agent.ID, agent.RetryCount, agent.MaxRetries)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
