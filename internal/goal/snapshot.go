package goal

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/harness"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

// SystemSnapshot is the goal-level read model for existing workflow and
// harness stores. It is intentionally a projection: workflow and harness keep
// their current JSON schemas, while control-plane and eval callers can use one
// goal-owned view.
type SystemSnapshot struct {
	GeneratedAt time.Time           `json:"generated_at"`
	GoalRoot    string              `json:"goal_root,omitempty"`
	WorkflowDir string              `json:"workflow_dir,omitempty"`
	HarnessDir  string              `json:"harness_dir,omitempty"`
	Goals       []GoalStateSnapshot `json:"goals,omitempty"`
	Workflows   []WorkflowSnapshot  `json:"workflows,omitempty"`
	Harness     HarnessSnapshot     `json:"harness,omitempty"`
	Approvals   []ApprovalSnapshot  `json:"approvals,omitempty"`
	Attention   []AttentionItem     `json:"attention,omitempty"`
	Warnings    []string            `json:"warnings,omitempty"`
}

type GoalStateSnapshot struct {
	ID               string             `json:"id"`
	GoalDir          string             `json:"goal_dir,omitempty"`
	Goal             string             `json:"goal"`
	Task             string             `json:"task,omitempty"`
	Status           string             `json:"status"`
	CurrentStep      string             `json:"current_step,omitempty"`
	AssignedAgent    string             `json:"assigned_agent,omitempty"`
	NeedsHuman       bool               `json:"needs_human,omitempty"`
	CurrentBlocker   string             `json:"current_blocker,omitempty"`
	FinalArtifact    string             `json:"final_artifact,omitempty"`
	ModifiedFiles    []string           `json:"modified_files,omitempty"`
	RetryCount       int                `json:"retry_count,omitempty"`
	PendingApprovals []ApprovalSnapshot `json:"pending_approvals,omitempty"`
	UpdatedAt        time.Time          `json:"updated_at,omitempty"`
}

type ApprovalSnapshot struct {
	ID              string    `json:"id"`
	GoalID          string    `json:"goal_id,omitempty"`
	GoalDir         string    `json:"goal_dir,omitempty"`
	Step            string    `json:"step,omitempty"`
	Source          string    `json:"source,omitempty"`
	SourceID        string    `json:"source_id,omitempty"`
	Title           string    `json:"title"`
	Reason          string    `json:"reason,omitempty"`
	RequestedAction string    `json:"requested_action,omitempty"`
	Risk            string    `json:"risk,omitempty"`
	Artifact        string    `json:"artifact,omitempty"`
	Status          string    `json:"status"`
	RequestedBy     string    `json:"requested_by,omitempty"`
	ResolvedBy      string    `json:"resolved_by,omitempty"`
	Resolution      string    `json:"resolution,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	ResolvedAt      time.Time `json:"resolved_at,omitempty"`
}

type WorkflowSnapshot struct {
	ID              string                  `json:"id"`
	RunDir          string                  `json:"run_dir,omitempty"`
	EventLogPath    string                  `json:"event_log_path,omitempty"`
	DefinitionName  string                  `json:"definition_name,omitempty"`
	Driver          string                  `json:"driver,omitempty"`
	Entrypoint      string                  `json:"entrypoint,omitempty"`
	Status          string                  `json:"status"`
	Error           string                  `json:"error,omitempty"`
	ScriptPath      string                  `json:"script_path,omitempty"`
	FinalReportPath string                  `json:"final_report_path,omitempty"`
	GoalID          string                  `json:"goal_id,omitempty"`
	GoalDir         string                  `json:"goal_dir,omitempty"`
	Phases          []WorkflowPhaseSnapshot `json:"phases,omitempty"`
	AgentRuns       []WorkflowAgentSnapshot `json:"agent_runs,omitempty"`
	Team            *WorkflowTeamSnapshot   `json:"team,omitempty"`
	Arbitration     WorkflowArbitration     `json:"arbitration,omitempty"`
	EventCount      int                     `json:"event_count,omitempty"`
}

type WorkflowPhaseSnapshot struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Status      string   `json:"status"`
	Error       string   `json:"error,omitempty"`
	AgentRunIDs []string `json:"agent_run_ids,omitempty"`
}

type WorkflowAgentSnapshot struct {
	ID            string   `json:"id"`
	PhaseID       string   `json:"phase_id,omitempty"`
	AgentID       string   `json:"agent_id,omitempty"`
	AgentPath     string   `json:"agent_path,omitempty"`
	TaskName      string   `json:"task_name,omitempty"`
	AgentProfile  string   `json:"agent_profile,omitempty"`
	Status        string   `json:"status"`
	ReportPath    string   `json:"report_path,omitempty"`
	ReportMissing bool     `json:"report_missing,omitempty"`
	ChangedFiles  []string `json:"changed_files,omitempty"`
	Artifacts     []string `json:"artifacts,omitempty"`
	WorktreePath  string   `json:"worktree_path,omitempty"`
	InputTokens   int      `json:"input_tokens,omitempty"`
	OutputTokens  int      `json:"output_tokens,omitempty"`
	DurationMS    int64    `json:"duration_ms,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type WorkflowTeamSnapshot struct {
	Members   []WorkflowTeamMemberSnapshot `json:"members,omitempty"`
	CreatedAt time.Time                    `json:"created_at,omitempty"`
	UpdatedAt time.Time                    `json:"updated_at,omitempty"`
}

type WorkflowTeamMemberSnapshot struct {
	ID             string `json:"id,omitempty"`
	Role           string `json:"role,omitempty"`
	Mode           string `json:"mode,omitempty"`
	AgentProfile   string `json:"agent_profile,omitempty"`
	TaskName       string `json:"task_name,omitempty"`
	PhaseID        string `json:"phase_id,omitempty"`
	CreatedProfile bool   `json:"created_profile,omitempty"`
}

type WorkflowArbitration struct {
	Status              string                       `json:"status,omitempty"`
	OpenAgentRuns       []string                     `json:"open_agent_runs,omitempty"`
	MissingReports      []string                     `json:"missing_reports,omitempty"`
	FailedAgentRuns     []string                     `json:"failed_agent_runs,omitempty"`
	ChangedFileOverlaps []ChangedFileOverlapSnapshot `json:"changed_file_overlaps,omitempty"`
	NextActions         []string                     `json:"next_actions,omitempty"`
}

type ChangedFileOverlapSnapshot struct {
	File        string   `json:"file"`
	AgentRunIDs []string `json:"agent_run_ids"`
}

type HarnessSnapshot struct {
	Tasks   []HarnessTaskSnapshot   `json:"tasks,omitempty"`
	Reports []HarnessReportSnapshot `json:"reports,omitempty"`
}

type HarnessTaskSnapshot struct {
	ID            string   `json:"id"`
	ParentID      string   `json:"parent_id,omitempty"`
	Path          string   `json:"path,omitempty"`
	Name          string   `json:"name,omitempty"`
	Role          string   `json:"role,omitempty"`
	GoalID        string   `json:"goal_id,omitempty"`
	GoalDir       string   `json:"goal_dir,omitempty"`
	Status        string   `json:"status"`
	ReportPath    string   `json:"report_path,omitempty"`
	ArtifactPaths []string `json:"artifact_paths,omitempty"`
	InputTokens   int      `json:"input_tokens,omitempty"`
	OutputTokens  int      `json:"output_tokens,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type HarnessReportSnapshot struct {
	ID           string   `json:"id"`
	TaskID       string   `json:"task_id"`
	RunID        string   `json:"run_id,omitempty"`
	AgentID      string   `json:"agent_id,omitempty"`
	AgentPath    string   `json:"agent_path,omitempty"`
	Outcome      string   `json:"outcome"`
	Summary      string   `json:"summary,omitempty"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	Verification []string `json:"verification,omitempty"`
	Artifacts    []string `json:"artifacts,omitempty"`
	ReportPath   string   `json:"report_path,omitempty"`
}

type AttentionItem struct {
	Source  string `json:"source"`
	ID      string `json:"id,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
	Path    string `json:"path,omitempty"`
}

type SnapshotOptions struct {
	GoalRoot      string
	WorkflowStore *workflow.Store
	HarnessStore  *harness.Store
	Now           func() time.Time
}

func SnapshotSystem(opts SnapshotOptions) SystemSnapshot {
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	snap := SystemSnapshot{GeneratedAt: now()}
	if strings.TrimSpace(opts.GoalRoot) != "" {
		snap.GoalRoot = strings.TrimSpace(opts.GoalRoot)
		goals, approvals, attention, warnings := SnapshotGoals(opts.GoalRoot)
		snap.Goals = goals
		snap.Approvals = approvals
		snap.Attention = append(snap.Attention, attention...)
		snap.Warnings = append(snap.Warnings, warnings...)
	}
	if opts.WorkflowStore != nil {
		snap.WorkflowDir = filepath.Join(opts.WorkflowStore.Dir(), "workflows")
		workflowSnapshots, attention, warnings := SnapshotWorkflows(opts.WorkflowStore)
		snap.Workflows = workflowSnapshots
		snap.Attention = append(snap.Attention, attention...)
		snap.Warnings = append(snap.Warnings, warnings...)
	}
	if opts.HarnessStore != nil {
		snap.HarnessDir = opts.HarnessStore.Dir()
		harnessSnapshot, attention, warnings := SnapshotHarness(opts.HarnessStore)
		snap.Harness = harnessSnapshot
		snap.Attention = append(snap.Attention, attention...)
		snap.Warnings = append(snap.Warnings, warnings...)
	}
	return snap
}

func SnapshotGoals(goalRoot string) ([]GoalStateSnapshot, []ApprovalSnapshot, []AttentionItem, []string) {
	goalRoot = strings.TrimSpace(goalRoot)
	if goalRoot == "" {
		return nil, nil, nil, nil
	}
	entries, err := os.ReadDir(goalRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, []string{"list goal states: " + err.Error()}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	goals := make([]GoalStateSnapshot, 0, len(entries))
	var approvals []ApprovalSnapshot
	var attention []AttentionItem
	var warnings []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		goalDir := filepath.Join(goalRoot, entry.Name())
		state, err := NewStore(goalDir).LoadState()
		if err != nil {
			warnings = append(warnings, "load goal state "+entry.Name()+": "+err.Error())
			continue
		}
		item := goalStateSnapshot(goalDir, state)
		goals = append(goals, item)
		approvals = append(approvals, item.PendingApprovals...)
		if state.Status == StatusBlocked || state.Status == StatusFailed || state.Status == StatusNeedsHuman || state.NeedsHuman {
			attention = append(attention, AttentionItem{
				Source:  "goal",
				ID:      state.ID,
				Status:  string(state.Status),
				Message: firstSnapshotText(state.CurrentBlocker, state.Goal),
				Path:    filepath.Join(goalDir, "state.json"),
			})
		}
		for _, approval := range item.PendingApprovals {
			attention = append(attention, AttentionItem{
				Source:  "goal_approval",
				ID:      approval.ID,
				Status:  approval.Status,
				Message: firstSnapshotText(approval.Title, approval.Reason),
				Path:    approval.Artifact,
			})
		}
	}
	return goals, approvals, attention, warnings
}

func SnapshotWorkflows(store *workflow.Store) ([]WorkflowSnapshot, []AttentionItem, []string) {
	if store == nil {
		return nil, nil, nil
	}
	runs, err := store.ListRuns()
	if err != nil {
		return nil, nil, []string{"list workflow runs: " + err.Error()}
	}
	out := make([]WorkflowSnapshot, 0, len(runs))
	var attention []AttentionItem
	var warnings []string
	for _, run := range runs {
		runDir := filepath.Join(store.Dir(), "workflows", run.ID)
		item := WorkflowSnapshot{
			ID:              run.ID,
			RunDir:          runDir,
			EventLogPath:    filepath.Join(runDir, "events.jsonl"),
			DefinitionName:  run.DefinitionName,
			Driver:          run.Driver,
			Entrypoint:      run.Entrypoint,
			Status:          string(run.Status),
			Error:           strings.TrimSpace(run.Error),
			ScriptPath:      run.ScriptPath,
			FinalReportPath: run.FinalReportPath,
			GoalID:          run.GoalID,
			GoalDir:         run.GoalDir,
			Phases:          workflowPhaseSnapshots(run.Phases),
		}
		if run.Status == workflow.RunStateFailed || run.Status == workflow.RunStatePaused {
			attention = append(attention, AttentionItem{
				Source:  "workflow",
				ID:      run.ID,
				Status:  string(run.Status),
				Message: firstSnapshotText(run.Error, run.PauseReason, run.ResumeHint),
				Path:    item.EventLogPath,
			})
		}
		team, err := store.LoadTeamPlan(run.ID)
		if err != nil {
			warnings = append(warnings, "load workflow team for "+run.ID+": "+err.Error())
		} else if len(team.Members) > 0 {
			item.Team = workflowTeamSnapshot(team)
		}
		agents, err := store.ListAgentRuns(run.ID)
		if err != nil {
			warnings = append(warnings, "list workflow agent runs for "+run.ID+": "+err.Error())
		} else {
			item.AgentRuns = workflowAgentSnapshots(agents)
			item.Arbitration = workflowArbitrationSnapshot(workflow.AnalyzeTeamArbitration(agents))
			for _, attentionItem := range workflowAttentionFromArbitration(run.ID, item.Arbitration) {
				attention = append(attention, attentionItem)
			}
		}
		events, err := store.ListEvents(run.ID)
		if err != nil {
			warnings = append(warnings, "list workflow events for "+run.ID+": "+err.Error())
		} else {
			item.EventCount = len(events)
		}
		out = append(out, item)
	}
	return out, attention, warnings
}

func SnapshotHarness(store *harness.Store) (HarnessSnapshot, []AttentionItem, []string) {
	if store == nil {
		return HarnessSnapshot{}, nil, nil
	}
	var snap HarnessSnapshot
	var attention []AttentionItem
	var warnings []string
	tasks, err := store.ListTasks()
	if err != nil {
		warnings = append(warnings, "list harness tasks: "+err.Error())
	} else {
		snap.Tasks = harnessTaskSnapshots(tasks)
		for _, task := range tasks {
			if task.Status == harness.TaskStatusFailed {
				attention = append(attention, AttentionItem{
					Source:  "harness",
					ID:      task.ID,
					Status:  string(task.Status),
					Message: strings.TrimSpace(task.Error),
					Path:    task.ReportPath,
				})
			}
		}
	}
	reports, err := store.ListReports()
	if err != nil {
		warnings = append(warnings, "list harness reports: "+err.Error())
	} else {
		snap.Reports = harnessReportSnapshots(reports)
		for _, report := range reports {
			if reportHasBlocker(report) {
				attention = append(attention, AttentionItem{
					Source:  "harness_report",
					ID:      report.ID,
					Status:  report.Outcome,
					Message: strings.Join(report.Blockers, "; "),
					Path:    report.ReportPath,
				})
			}
		}
	}
	return snap, attention, warnings
}

func goalStateSnapshot(goalDir string, state State) GoalStateSnapshot {
	return GoalStateSnapshot{
		ID:               state.ID,
		GoalDir:          goalDir,
		Goal:             state.Goal,
		Task:             state.Task,
		Status:           string(state.Status),
		CurrentStep:      string(state.CurrentStep),
		AssignedAgent:    state.AssignedAgent,
		NeedsHuman:       state.NeedsHuman,
		CurrentBlocker:   state.CurrentBlocker,
		FinalArtifact:    state.FinalArtifact,
		ModifiedFiles:    append([]string(nil), state.ModifiedFiles...),
		RetryCount:       state.RetryCount,
		PendingApprovals: pendingApprovalSnapshots(goalDir, state),
		UpdatedAt:        state.UpdatedAt,
	}
}

func pendingApprovalSnapshots(goalDir string, state State) []ApprovalSnapshot {
	var out []ApprovalSnapshot
	for _, approval := range state.Approvals {
		if approval.Status != ApprovalStatusPending {
			continue
		}
		out = append(out, approvalSnapshot(goalDir, state.ID, approval))
	}
	return out
}

func approvalSnapshot(goalDir, goalID string, approval ApprovalRequest) ApprovalSnapshot {
	return ApprovalSnapshot{
		ID:              approval.ID,
		GoalID:          goalID,
		GoalDir:         goalDir,
		Step:            string(approval.Step),
		Source:          approval.Source,
		SourceID:        approval.SourceID,
		Title:           approval.Title,
		Reason:          approval.Reason,
		RequestedAction: approval.RequestedAction,
		Risk:            approval.Risk,
		Artifact:        approval.Artifact,
		Status:          string(approval.Status),
		RequestedBy:     approval.RequestedBy,
		ResolvedBy:      approval.ResolvedBy,
		Resolution:      approval.Resolution,
		CreatedAt:       approval.CreatedAt,
		ResolvedAt:      approval.ResolvedAt,
	}
}

func workflowPhaseSnapshots(phases []workflow.Phase) []WorkflowPhaseSnapshot {
	out := make([]WorkflowPhaseSnapshot, 0, len(phases))
	for _, phase := range phases {
		out = append(out, WorkflowPhaseSnapshot{
			ID:          phase.ID,
			Name:        phase.Name,
			Status:      string(phase.Status),
			Error:       strings.TrimSpace(phase.Error),
			AgentRunIDs: append([]string(nil), phase.AgentRunIDs...),
		})
	}
	return out
}

func workflowAgentSnapshots(agents []workflow.AgentRun) []WorkflowAgentSnapshot {
	out := make([]WorkflowAgentSnapshot, 0, len(agents))
	for _, agent := range agents {
		out = append(out, WorkflowAgentSnapshot{
			ID:            agent.ID,
			PhaseID:       agent.PhaseID,
			AgentID:       agent.AgentID,
			AgentPath:     agent.AgentPath,
			TaskName:      agent.TaskName,
			AgentProfile:  agent.AgentProfile,
			Status:        string(agent.Status),
			ReportPath:    agent.ReportPath,
			ReportMissing: agent.ReportMissing,
			ChangedFiles:  append([]string(nil), agent.ChangedFiles...),
			Artifacts:     append([]string(nil), agent.Artifacts...),
			WorktreePath:  agent.WorktreePath,
			InputTokens:   agent.InputTokens,
			OutputTokens:  agent.OutputTokens,
			DurationMS:    agent.DurationMS,
			Error:         strings.TrimSpace(agent.Error),
		})
	}
	return out
}

func workflowTeamSnapshot(team workflow.TeamPlan) *WorkflowTeamSnapshot {
	out := &WorkflowTeamSnapshot{
		CreatedAt: team.CreatedAt,
		UpdatedAt: team.UpdatedAt,
		Members:   make([]WorkflowTeamMemberSnapshot, 0, len(team.Members)),
	}
	for _, member := range team.Members {
		out.Members = append(out.Members, WorkflowTeamMemberSnapshot{
			ID:             member.ID,
			Role:           member.Role,
			Mode:           string(member.Mode),
			AgentProfile:   member.AgentProfile,
			TaskName:       member.TaskName,
			PhaseID:        member.PhaseID,
			CreatedProfile: member.CreatedProfile,
		})
	}
	return out
}

func workflowArbitrationSnapshot(in workflow.TeamArbitration) WorkflowArbitration {
	overlaps := make([]ChangedFileOverlapSnapshot, 0, len(in.ChangedFileOverlaps))
	for _, overlap := range in.ChangedFileOverlaps {
		overlaps = append(overlaps, ChangedFileOverlapSnapshot{
			File:        overlap.File,
			AgentRunIDs: append([]string(nil), overlap.AgentRunIDs...),
		})
	}
	return WorkflowArbitration{
		Status:              in.Status,
		OpenAgentRuns:       append([]string(nil), in.OpenAgentRuns...),
		MissingReports:      append([]string(nil), in.MissingReports...),
		FailedAgentRuns:     append([]string(nil), in.FailedAgentRuns...),
		ChangedFileOverlaps: overlaps,
		NextActions:         append([]string(nil), in.NextActions...),
	}
}

func workflowAttentionFromArbitration(runID string, arbitration WorkflowArbitration) []AttentionItem {
	var out []AttentionItem
	for _, id := range arbitration.FailedAgentRuns {
		out = append(out, AttentionItem{Source: "workflow_agent", ID: id, Status: "failed", Message: "workflow run " + runID + " has failed agent"})
	}
	for _, id := range arbitration.MissingReports {
		out = append(out, AttentionItem{Source: "workflow_agent", ID: id, Status: "missing_report", Message: "workflow run " + runID + " is missing agent report"})
	}
	for _, overlap := range arbitration.ChangedFileOverlaps {
		out = append(out, AttentionItem{Source: "workflow_conflict", ID: runID, Status: "changed_file_overlap", Message: strings.Join(overlap.AgentRunIDs, ", "), Path: overlap.File})
	}
	return out
}

func harnessTaskSnapshots(tasks []harness.Task) []HarnessTaskSnapshot {
	out := make([]HarnessTaskSnapshot, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, HarnessTaskSnapshot{
			ID:            task.ID,
			ParentID:      task.ParentID,
			Path:          task.Path,
			Name:          task.Name,
			Role:          task.Role,
			GoalID:        task.GoalID,
			GoalDir:       task.GoalDir,
			Status:        string(task.Status),
			ReportPath:    task.ReportPath,
			ArtifactPaths: append([]string(nil), task.ArtifactPaths...),
			InputTokens:   task.InputTokens,
			OutputTokens:  task.OutputTokens,
			Error:         strings.TrimSpace(task.Error),
		})
	}
	return out
}

func harnessReportSnapshots(reports []harness.Report) []HarnessReportSnapshot {
	out := make([]HarnessReportSnapshot, 0, len(reports))
	for _, report := range reports {
		out = append(out, HarnessReportSnapshot{
			ID:           report.ID,
			TaskID:       report.TaskID,
			RunID:        report.RunID,
			AgentID:      report.AgentID,
			AgentPath:    report.AgentPath,
			Outcome:      report.Outcome,
			Summary:      strings.TrimSpace(report.Summary),
			ChangedFiles: append([]string(nil), report.ChangedFiles...),
			Verification: append([]string(nil), report.Verification...),
			Artifacts:    append([]string(nil), report.Artifacts...),
			ReportPath:   report.ReportPath,
		})
	}
	return out
}

func reportHasBlocker(report harness.Report) bool {
	if len(report.Blockers) > 0 {
		return true
	}
	outcome := strings.ToLower(strings.TrimSpace(report.Outcome))
	return outcome == "failed" || outcome == "error" || outcome == "stuck" || outcome == "partial"
}

func firstSnapshotText(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
