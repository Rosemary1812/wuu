package goal

import (
	"bufio"
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

const SchemaVersion = "wuu/goal/v0.1"

type Store struct {
	dir string
	now func() time.Time
	mu  sync.Mutex
}

func NewStore(dir string) *Store {
	return &Store{
		dir: strings.TrimSpace(dir),
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

func (s *Store) SetClock(now func() time.Time) {
	if s == nil || now == nil {
		return
	}
	s.now = now
}

func (s *Store) Init(spec Spec) (State, error) {
	if s == nil || s.dir == "" {
		return State{}, errors.New("goal store is not configured")
	}
	if strings.TrimSpace(spec.Goal) == "" {
		return State{}, errors.New("goal objective is required")
	}
	now := s.now()
	id := strings.TrimSpace(spec.ID)
	if id == "" {
		id = "goal-" + randomID()
	}
	state := State{
		SchemaVersion:      SchemaVersion,
		ID:                 id,
		Goal:               strings.TrimSpace(spec.Goal),
		Task:               strings.TrimSpace(spec.Task),
		Trigger:            spec.Trigger,
		Status:             StatusPending,
		CurrentStep:        StepInit,
		AssignedAgent:      strings.TrimSpace(spec.AssignedAgent),
		Permissions:        spec.Permissions,
		Worktree:           cloneWorktreeLease(spec.Worktree),
		VerificationPolicy: spec.VerificationPolicy,
		RetryPolicy:        spec.RetryPolicy,
		EscalationPolicy:   spec.EscalationPolicy,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if state.Worktree != nil {
		if state.Worktree.CreatedAt.IsZero() {
			state.Worktree.CreatedAt = now
		}
		state.Worktree.UpdatedAt = now
	}
	if err := s.SaveState(state); err != nil {
		return State{}, err
	}
	if err := s.AppendEvent(Event{Type: "goal_initialized", GoalID: state.ID, Step: StepInit, Message: state.Goal}); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *Store) LoadState() (State, error) {
	if s == nil || s.dir == "" {
		return State{}, errors.New("goal store is not configured")
	}
	var state State
	if err := readJSON(filepath.Join(s.dir, "state.json"), &state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *Store) SaveState(state State) error {
	if s == nil || s.dir == "" {
		return errors.New("goal store is not configured")
	}
	if strings.TrimSpace(state.ID) == "" {
		return errors.New("goal state id is required")
	}
	if strings.TrimSpace(state.Goal) == "" {
		return errors.New("goal state goal is required")
	}
	if state.SchemaVersion == "" {
		state.SchemaVersion = SchemaVersion
	}
	state.UpdatedAt = s.now()

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Join(s.dir, "artifacts"), 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(s.dir, "state.json"), state); err != nil {
		return err
	}
	return s.rewriteLedgersLocked(state)
}

func (s *Store) AppendEvent(event Event) error {
	if s == nil || s.dir == "" {
		return errors.New("goal store is not configured")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = s.now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(s.dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	return enc.Encode(event)
}

func (s *Store) Events() ([]Event, error) {
	if s == nil || s.dir == "" {
		return nil, errors.New("goal store is not configured")
	}
	file, err := os.Open(filepath.Join(s.dir, "events.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var out []Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) AddProgress(step Step, message string) (State, error) {
	state, err := s.LoadState()
	if err != nil {
		return State{}, err
	}
	now := s.now()
	state.Progress = append(state.Progress, ProgressEntry{Step: step, Message: strings.TrimSpace(message), CreatedAt: now})
	state.CurrentStep = step
	state.Status = StatusRunning
	if err := s.SaveState(state); err != nil {
		return State{}, err
	}
	return state, s.AppendEvent(Event{Type: "progress", GoalID: state.ID, Step: step, Message: message})
}

func (s *Store) AddDecision(step Step, summary, reason string) (State, error) {
	state, err := s.LoadState()
	if err != nil {
		return State{}, err
	}
	now := s.now()
	state.Decisions = append(state.Decisions, Decision{Step: step, Summary: strings.TrimSpace(summary), Reason: strings.TrimSpace(reason), CreatedAt: now})
	if err := s.SaveState(state); err != nil {
		return State{}, err
	}
	return state, s.AppendEvent(Event{Type: "decision", GoalID: state.ID, Step: step, Message: summary})
}

func (s *Store) AddFailure(failure Failure) (State, error) {
	state, err := s.LoadState()
	if err != nil {
		return State{}, err
	}
	if failure.CreatedAt.IsZero() {
		failure.CreatedAt = s.now()
	}
	failure.Message = strings.TrimSpace(failure.Message)
	if failure.Message == "" {
		failure.Message = "unknown failure"
	}
	failure.Kind = strings.TrimSpace(failure.Kind)
	failure.Source = strings.TrimSpace(failure.Source)
	failure.SourceID = strings.TrimSpace(failure.SourceID)
	failure.Artifact = strings.TrimSpace(failure.Artifact)
	if failure.Source != "" && failure.SourceID != "" {
		for _, existing := range state.Failures {
			if existing.Source == failure.Source &&
				existing.SourceID == failure.SourceID &&
				existing.Kind == failure.Kind {
				return state, nil
			}
		}
	}
	state.Failures = append(state.Failures, failure)
	state.CurrentStep = failure.Step
	state.CurrentBlocker = failure.Message
	state.Status = StatusBlocked
	if state.EscalationPolicy.EscalateOnFailure {
		state.NeedsHuman = true
		state.Status = StatusNeedsHuman
	}
	if len(state.NextSteps) == 0 {
		state.NextSteps = []string{"read views/failures.md in the goal store and fix the recorded failure before retrying"}
	}
	if err := s.SaveState(state); err != nil {
		return State{}, err
	}
	return state, s.AppendEvent(Event{
		Type:     "failure",
		GoalID:   state.ID,
		Step:     failure.Step,
		Message:  failure.Message,
		Artifact: failure.Artifact,
		Data: map[string]string{
			"kind":      failure.Kind,
			"source":    failure.Source,
			"source_id": failure.SourceID,
			"command":   failure.Command,
		},
	})
}

func (s *Store) RequestApproval(request ApprovalRequest) (State, ApprovalRequest, error) {
	state, err := s.LoadState()
	if err != nil {
		return State{}, ApprovalRequest{}, err
	}
	now := s.now()
	request.ID = strings.TrimSpace(request.ID)
	if request.ID == "" {
		request.ID = "approval-" + randomID()
	}
	request.Title = strings.TrimSpace(request.Title)
	if request.Title == "" {
		return State{}, ApprovalRequest{}, errors.New("approval title is required")
	}
	request.Reason = strings.TrimSpace(request.Reason)
	request.RequestedAction = strings.TrimSpace(request.RequestedAction)
	request.Risk = strings.TrimSpace(request.Risk)
	request.Source = strings.TrimSpace(request.Source)
	request.SourceID = strings.TrimSpace(request.SourceID)
	request.Artifact = strings.TrimSpace(request.Artifact)
	request.RequestedBy = strings.TrimSpace(request.RequestedBy)
	if request.CreatedAt.IsZero() {
		request.CreatedAt = now
	}
	if request.Status == "" {
		request.Status = ApprovalStatusPending
	}
	if request.Status != ApprovalStatusPending {
		return State{}, ApprovalRequest{}, fmt.Errorf("new approval request must be pending, got %q", request.Status)
	}
	for _, existing := range state.Approvals {
		if existing.ID == request.ID {
			return State{}, ApprovalRequest{}, fmt.Errorf("approval request %q already exists", request.ID)
		}
		if request.Source != "" && request.SourceID != "" &&
			existing.Source == request.Source &&
			existing.SourceID == request.SourceID &&
			existing.Status == ApprovalStatusPending {
			return state, existing, nil
		}
	}
	state.Approvals = append(state.Approvals, request)
	state.CurrentStep = StepApproval
	state.Status = StatusNeedsHuman
	state.NeedsHuman = true
	state.CurrentBlocker = request.Title
	state.NextSteps = appendUniqueStrings(state.NextSteps, []string{"review pending approvals in goal state before continuing"})
	if err := s.SaveState(state); err != nil {
		return State{}, ApprovalRequest{}, err
	}
	return state, request, s.AppendEvent(Event{
		Type:     "approval_requested",
		GoalID:   state.ID,
		Step:     StepApproval,
		Message:  request.Title,
		Artifact: request.Artifact,
		Data: map[string]string{
			"approval_id": request.ID,
			"source":      request.Source,
			"source_id":   request.SourceID,
			"action":      request.RequestedAction,
		},
	})
}

func (s *Store) ResolveApproval(resolution ApprovalResolution) (State, ApprovalRequest, error) {
	state, err := s.LoadState()
	if err != nil {
		return State{}, ApprovalRequest{}, err
	}
	id := strings.TrimSpace(resolution.ID)
	if id == "" {
		return State{}, ApprovalRequest{}, errors.New("approval id is required")
	}
	if resolution.Approved == resolution.Rejected {
		return State{}, ApprovalRequest{}, errors.New("approval resolution must set exactly one of approved or rejected")
	}
	now := s.now()
	var resolved ApprovalRequest
	found := false
	for i := range state.Approvals {
		if state.Approvals[i].ID != id {
			continue
		}
		found = true
		if state.Approvals[i].Status != ApprovalStatusPending {
			return State{}, ApprovalRequest{}, fmt.Errorf("approval request %q is already %s", id, state.Approvals[i].Status)
		}
		if resolution.Approved {
			state.Approvals[i].Status = ApprovalStatusApproved
		} else {
			state.Approvals[i].Status = ApprovalStatusRejected
		}
		state.Approvals[i].ResolvedBy = strings.TrimSpace(resolution.ResolvedBy)
		state.Approvals[i].Resolution = strings.TrimSpace(resolution.Resolution)
		state.Approvals[i].ResolvedAt = now
		resolved = state.Approvals[i]
		break
	}
	if !found {
		return State{}, ApprovalRequest{}, fmt.Errorf("approval request %q not found", id)
	}
	if resolution.Rejected {
		state.NeedsHuman = false
		state.Status = StatusBlocked
		state.CurrentStep = StepApproval
		state.CurrentBlocker = "approval rejected: " + resolved.Title
		state.Failures = append(state.Failures, Failure{
			Step:      StepApproval,
			Kind:      "approval_rejected",
			Source:    firstNonEmpty(resolved.Source, "approval"),
			SourceID:  firstNonEmpty(resolved.SourceID, resolved.ID),
			Message:   state.CurrentBlocker,
			Artifact:  resolved.Artifact,
			CreatedAt: now,
		})
		state.NextSteps = appendUniqueStrings(state.NextSteps, []string{"choose a lower-risk alternative or ask for a new approval"})
	} else if !hasPendingApprovals(state.Approvals) {
		state.NeedsHuman = false
		if state.Status == StatusNeedsHuman && state.CurrentStep == StepApproval {
			state.Status = StatusRunning
			state.CurrentStep = StepExecution
		}
		if state.CurrentBlocker == resolved.Title {
			state.CurrentBlocker = ""
		}
	}
	if err := s.SaveState(state); err != nil {
		return State{}, ApprovalRequest{}, err
	}
	return state, resolved, s.AppendEvent(Event{
		Type:     "approval_resolved",
		GoalID:   state.ID,
		Step:     StepApproval,
		Message:  string(resolved.Status),
		Artifact: resolved.Artifact,
		Data: map[string]string{
			"approval_id": resolved.ID,
			"source":      resolved.Source,
			"source_id":   resolved.SourceID,
			"resolved_by": resolved.ResolvedBy,
		},
	})
}

func (s *Store) AddArtifact(name, kind, content string) (State, string, error) {
	state, err := s.LoadState()
	if err != nil {
		return State{}, "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return State{}, "", errors.New("artifact name is required")
	}
	if filepath.Base(name) != name || strings.Contains(name, string(filepath.Separator)) {
		return State{}, "", fmt.Errorf("artifact name must be a file name, got %q", name)
	}
	path := filepath.Join(s.dir, "artifacts", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return State{}, "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return State{}, "", err
	}
	now := s.now()
	state.Artifacts = upsertArtifact(state.Artifacts, Artifact{Name: name, Path: path, Kind: strings.TrimSpace(kind), CreatedAt: now})
	if name == "final.md" {
		state.FinalArtifact = path
	}
	if err := s.SaveState(state); err != nil {
		return State{}, "", err
	}
	return state, path, s.AppendEvent(Event{Type: "artifact_written", GoalID: state.ID, Step: state.CurrentStep, Artifact: path, Message: name})
}

func (s *Store) MarkStepCompleted(step Step) (State, error) {
	state, err := s.LoadState()
	if err != nil {
		return State{}, err
	}
	if !containsStep(state.CompletedSteps, step) {
		state.CompletedSteps = append(state.CompletedSteps, step)
	}
	if err := s.SaveState(state); err != nil {
		return State{}, err
	}
	return state, s.AppendEvent(Event{Type: "step_completed", GoalID: state.ID, Step: step})
}

func (s *Store) SetStatus(status Status, step Step, message string) (State, error) {
	state, err := s.LoadState()
	if err != nil {
		return State{}, err
	}
	state.Status = status
	if step != "" {
		state.CurrentStep = step
	}
	if status == StatusCompleted {
		state.NeedsHuman = false
		state.CurrentBlocker = ""
	}
	if err := s.SaveState(state); err != nil {
		return State{}, err
	}
	return state, s.AppendEvent(Event{Type: "status_changed", GoalID: state.ID, Step: state.CurrentStep, Message: message, Data: map[string]string{"status": string(status)}})
}

func (s *Store) RecordTestResults(results []TestResult) (State, error) {
	state, err := s.LoadState()
	if err != nil {
		return State{}, err
	}
	state.TestResults = append(state.TestResults, results...)
	if err := s.SaveState(state); err != nil {
		return State{}, err
	}
	return state, s.AppendEvent(Event{Type: "verification_recorded", GoalID: state.ID, Step: StepVerification, Data: map[string]int{"checks": len(results)}})
}

func (s *Store) FailureContext() (string, error) {
	state, err := s.LoadState()
	if err != nil {
		return "", err
	}
	if len(state.Failures) == 0 {
		return "", nil
	}
	var b strings.Builder
	renderFailures(&b, state.Failures)
	return b.String(), nil
}

func (s *Store) rewriteLedgersLocked(state State) error {
	viewDir := filepath.Join(s.dir, "views")
	if err := writeMarkdown(filepath.Join(viewDir, "progress.md"), "# Progress\n\n", func(b *strings.Builder) {
		renderProgress(b, state.Progress)
	}); err != nil {
		return err
	}
	if err := writeMarkdown(filepath.Join(viewDir, "decisions.md"), "# Decisions\n\n", func(b *strings.Builder) {
		renderDecisions(b, state.Decisions)
	}); err != nil {
		return err
	}
	if err := writeMarkdown(filepath.Join(viewDir, "failures.md"), "# Failures\n\n", func(b *strings.Builder) {
		renderFailures(b, state.Failures)
	}); err != nil {
		return err
	}
	return writeMarkdown(filepath.Join(viewDir, "approvals.md"), "# Approvals\n\n", func(b *strings.Builder) {
		renderApprovals(b, state.Approvals)
	})
}

func writeMarkdown(path, header string, render func(*strings.Builder)) error {
	var b strings.Builder
	b.WriteString(header)
	render(&b)
	if strings.TrimSpace(b.String()) == strings.TrimSpace(header) {
		b.WriteString("_None recorded._\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func renderProgress(b *strings.Builder, entries []ProgressEntry) {
	for _, entry := range entries {
		fmt.Fprintf(b, "- %s `%s`: %s", entry.CreatedAt.Format(time.RFC3339), entry.Step, entry.Message)
		if entry.Source != "" {
			fmt.Fprintf(b, " source=%s", entry.Source)
		}
		if entry.SourceID != "" {
			fmt.Fprintf(b, " source_id=%s", entry.SourceID)
		}
		b.WriteByte('\n')
	}
}

func renderDecisions(b *strings.Builder, entries []Decision) {
	for _, entry := range entries {
		fmt.Fprintf(b, "- %s `%s`: %s", entry.CreatedAt.Format(time.RFC3339), entry.Step, entry.Summary)
		if entry.Reason != "" {
			fmt.Fprintf(b, " - %s", entry.Reason)
		}
		b.WriteByte('\n')
	}
}

func renderFailures(b *strings.Builder, entries []Failure) {
	for _, entry := range entries {
		fmt.Fprintf(b, "- %s `%s` %s: %s", entry.CreatedAt.Format(time.RFC3339), entry.Step, entry.Kind, entry.Message)
		if entry.Source != "" {
			fmt.Fprintf(b, " source=%s", entry.Source)
		}
		if entry.SourceID != "" {
			fmt.Fprintf(b, " source_id=%s", entry.SourceID)
		}
		if entry.Command != "" {
			fmt.Fprintf(b, " command=%q", entry.Command)
		}
		if entry.Artifact != "" {
			fmt.Fprintf(b, " artifact=%s", entry.Artifact)
		}
		b.WriteByte('\n')
	}
}

func renderApprovals(b *strings.Builder, entries []ApprovalRequest) {
	for _, entry := range entries {
		fmt.Fprintf(b, "- %s `%s` %s: %s", entry.CreatedAt.Format(time.RFC3339), entry.Status, entry.ID, entry.Title)
		if entry.Source != "" {
			fmt.Fprintf(b, " source=%s", entry.Source)
		}
		if entry.SourceID != "" {
			fmt.Fprintf(b, " source_id=%s", entry.SourceID)
		}
		if entry.RequestedAction != "" {
			fmt.Fprintf(b, " action=%q", entry.RequestedAction)
		}
		if entry.Risk != "" {
			fmt.Fprintf(b, " risk=%q", entry.Risk)
		}
		if entry.Artifact != "" {
			fmt.Fprintf(b, " artifact=%s", entry.Artifact)
		}
		if entry.Resolution != "" {
			fmt.Fprintf(b, " resolution=%q", entry.Resolution)
		}
		b.WriteByte('\n')
	}
}

func hasPendingApprovals(entries []ApprovalRequest) bool {
	for _, entry := range entries {
		if entry.Status == ApprovalStatusPending {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func readJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func randomID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func cloneWorktreeLease(in *WorktreeLease) *WorktreeLease {
	if in == nil {
		return nil
	}
	out := *in
	out.ChangedFiles = append([]string(nil), in.ChangedFiles...)
	return &out
}

func upsertArtifact(items []Artifact, artifact Artifact) []Artifact {
	for i := range items {
		if items[i].Name == artifact.Name {
			items[i] = artifact
			return items
		}
	}
	return append(items, artifact)
}

func containsStep(steps []Step, target Step) bool {
	for _, step := range steps {
		if step == target {
			return true
		}
	}
	return false
}
