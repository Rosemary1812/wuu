package appserver

// Kanban-OS orchestration: RPC handlers for the agent-neutral task model and
// the dispatch path that binds a task to a named-agent execution. Dispatch is
// "create run with target" (the store action) plus spawning the execution
// site; terminal outcomes flow back through the agent notification hook in
// agent_threads.go.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/kanban"
	"github.com/blueberrycongee/wuu/internal/memdir"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/subagent"
	"github.com/blueberrycongee/wuu/internal/workspaces"
)

// ---- wire shapes ----

type kanbanTaskWire struct {
	ID             string `json:"id"`
	SpaceID        string `json:"space_id"`
	WorkspaceID    string `json:"workspace_id,omitempty"`
	ParentID       string `json:"parent_id,omitempty"`
	Title          string `json:"title"`
	Brief          string `json:"brief,omitempty"`
	Status         string `json:"status"`
	SourceThreadID string `json:"source_thread_id,omitempty"`
	CreatedBy      string `json:"created_by,omitempty"`
	SortIndex      int    `json:"sort_index"`
	LatestRunID    string `json:"latest_run_id,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

type kanbanRunWire struct {
	ID           string `json:"id"`
	TaskID       string `json:"task_id"`
	SpaceID      string `json:"space_id"`
	WorkspaceID  string `json:"workspace_id,omitempty"`
	Kind         string `json:"kind"`
	TargetID     string `json:"target_id"`
	ThreadID     string `json:"thread_id,omitempty"`
	Status       string `json:"status"`
	Summary      string `json:"summary,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	CreatedBy    string `json:"created_by,omitempty"`
	CreatedAt    int64  `json:"created_at"`
	StartedAt    int64  `json:"started_at,omitempty"`
	FinishedAt   int64  `json:"finished_at,omitempty"`
}

type kanbanArtifactWire struct {
	ID          string `json:"id"`
	RunID       string `json:"run_id"`
	TaskID      string `json:"task_id"`
	SpaceID     string `json:"space_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Path        string `json:"path"`
	DisplayName string `json:"display_name,omitempty"`
	MediaType   string `json:"media_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
	CreatedAt   int64  `json:"created_at"`
}

type kanbanTaskWithLatestRunWire struct {
	kanbanTaskWire
	LatestRun *kanbanRunWire `json:"latest_run,omitempty"`
}

type KanbanUpdatedNotification struct {
	SpaceID string `json:"space_id"`
	TaskID  string `json:"task_id,omitempty"`
}

func kanbanTaskToWire(t kanban.Task) kanbanTaskWire {
	return kanbanTaskWire{
		ID: t.ID, SpaceID: t.SpaceID, WorkspaceID: t.WorkspaceID, ParentID: t.ParentID, Title: t.Title,
		Brief: t.Brief, Status: t.Status, SourceThreadID: t.SourceThreadID,
		CreatedBy: t.CreatedBy, SortIndex: t.SortIndex, LatestRunID: t.LatestRunID,
		CreatedAt: t.CreatedAt.UnixMilli(), UpdatedAt: t.UpdatedAt.UnixMilli(),
	}
}

func kanbanRunToWire(r kanban.Run) kanbanRunWire {
	return kanbanRunWire{
		ID: r.ID, TaskID: r.TaskID, SpaceID: r.SpaceID, WorkspaceID: r.WorkspaceID, Kind: r.Kind,
		TargetID: r.TargetID, ThreadID: r.ThreadID, Status: r.Status,
		Summary: r.Summary, ErrorMessage: r.ErrorMessage, CreatedBy: r.CreatedBy,
		CreatedAt: r.CreatedAt.UnixMilli(), StartedAt: r.StartedAt.UnixMilli(),
		FinishedAt: r.FinishedAt.UnixMilli(),
	}
}

func kanbanArtifactToWire(a kanban.Artifact) kanbanArtifactWire {
	return kanbanArtifactWire{
		ID: a.ID, RunID: a.RunID, TaskID: a.TaskID, SpaceID: a.SpaceID, WorkspaceID: a.WorkspaceID, Path: a.Path,
		DisplayName: a.DisplayName, MediaType: a.MediaType, SizeBytes: a.SizeBytes,
		CreatedAt: a.CreatedAt.UnixMilli(),
	}
}

func (s *Server) notifyKanbanUpdated(spaceID, taskID string) {
	_ = s.writeNotification(NotificationKanbanUpdated, KanbanUpdatedNotification{
		SpaceID: spaceID, TaskID: taskID,
	})
}

// ---- task CRUD ----

type KanbanCreateTaskParams struct {
	SpaceID        string `json:"space_id"`
	WorkspaceID    string `json:"workspace_id,omitempty"`
	ParentID       string `json:"parent_id,omitempty"`
	Title          string `json:"title"`
	Brief          string `json:"brief,omitempty"`
	SourceThreadID string `json:"source_thread_id,omitempty"`
	Status         string `json:"status,omitempty"`
	CreatedBy      string `json:"created_by,omitempty"`
	SortIndex      int    `json:"sort_index,omitempty"`
}

func (s *Server) handleKanbanCreateTask(req Request) error {
	var params KanbanCreateTaskParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	spaceID := strings.TrimSpace(params.SpaceID)
	if spaceID == "" {
		spaceID = "global"
	}
	task, err := session.CreateKanbanTask(s.rt.SessionDir, kanban.Task{
		SpaceID:        spaceID,
		WorkspaceID:    strings.TrimSpace(params.WorkspaceID),
		ParentID:       strings.TrimSpace(params.ParentID),
		Title:          params.Title,
		Brief:          params.Brief,
		SourceThreadID: strings.TrimSpace(params.SourceThreadID),
		Status:         strings.TrimSpace(params.Status),
		CreatedBy:      strings.TrimSpace(params.CreatedBy),
		SortIndex:      params.SortIndex,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	s.notifyKanbanUpdated(task.SpaceID, task.ID)
	return s.writeResponse(req.ID, kanbanTaskToWire(task), nil)
}

type KanbanListTasksParams struct {
	SpaceID   string `json:"space_id"`
	ParentID  string `json:"parent_id,omitempty"`
}

func (s *Server) handleKanbanListTasks(req Request) error {
	var params KanbanListTasksParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	spaceID := strings.TrimSpace(params.SpaceID)
	if spaceID == "" {
		spaceID = "global"
	}
	tasks, err := session.ListKanbanTasks(s.rt.SessionDir, spaceID, strings.TrimSpace(params.ParentID))
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	out := make([]kanbanTaskWithLatestRunWire, 0, len(tasks))
	for _, t := range tasks {
		w := kanbanTaskWithLatestRunWire{kanbanTaskWire: kanbanTaskToWire(t)}
		if t.LatestRunID != "" {
			if run, err := session.GetKanbanRun(s.rt.SessionDir, t.LatestRunID); err == nil {
				rw := kanbanRunToWire(run)
				w.LatestRun = &rw
			}
		}
		out = append(out, w)
	}
	return s.writeResponse(req.ID, out, nil)
}

type KanbanTransitionTaskParams struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

func (s *Server) handleKanbanTransitionTask(req Request) error {
	var params KanbanTransitionTaskParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	task, err := session.TransitionKanbanTaskStatus(s.rt.SessionDir, strings.TrimSpace(params.TaskID), strings.TrimSpace(params.Status))
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	s.notifyKanbanUpdated(task.SpaceID, task.ID)
	return s.writeResponse(req.ID, kanbanTaskToWire(task), nil)
}

type KanbanListRunsParams struct {
	TaskID string `json:"task_id"`
}

func (s *Server) handleKanbanListRuns(req Request) error {
	var params KanbanListRunsParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	runs, err := session.ListKanbanRuns(s.rt.SessionDir, strings.TrimSpace(params.TaskID))
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	out := make([]kanbanRunWire, 0, len(runs))
	for _, r := range runs {
		out = append(out, kanbanRunToWire(r))
	}
	return s.writeResponse(req.ID, out, nil)
}

type KanbanListArtifactsParams struct {
	TaskID string `json:"task_id"`
}

func (s *Server) handleKanbanListArtifacts(req Request) error {
	var params KanbanListArtifactsParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	artifacts, err := session.ListKanbanArtifacts(s.rt.SessionDir, strings.TrimSpace(params.TaskID))
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	out := make([]kanbanArtifactWire, 0, len(artifacts))
	for _, a := range artifacts {
		out = append(out, kanbanArtifactToWire(a))
	}
	return s.writeResponse(req.ID, out, nil)
}

// ---- dispatch ----

type KanbanDispatchRunParams struct {
	ThreadID  string `json:"thread_id"` // host conversation thread that owns the spawned execution
	TaskID    string `json:"task_id"`
	TargetID  string `json:"target_id"`
	Kind      string `json:"kind,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
}

func (s *Server) handleKanbanDispatchRun(ctx context.Context, req Request) error {
	var params KanbanDispatchRunParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	run, err := s.dispatchKanbanRun(ctx, threadID, strings.TrimSpace(params.TaskID),
		strings.TrimSpace(params.TargetID), strings.TrimSpace(params.Kind), strings.TrimSpace(params.CreatedBy))
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, kanbanRunToWire(run), nil)
}

// dispatchKanbanRun is the full dispatch path: record the run (the dispatch
// action itself), then spawn the target's execution site. Any failure after
// the run is recorded completes the run as failed so the task never parks in
// running with no execution behind it.
func (s *Server) dispatchKanbanRun(ctx context.Context, hostThreadID, taskID, targetID, kind, createdBy string) (kanban.Run, error) {
	task, err := session.GetKanbanTask(s.rt.SessionDir, taskID)
	if err != nil {
		return kanban.Run{}, err
	}
	run, err := session.CreateKanbanRun(s.rt.SessionDir, kanban.Run{
		TaskID: taskID, TargetID: targetID, Kind: kind, CreatedBy: createdBy,
	})
	if err != nil {
		return kanban.Run{}, err
	}
	fail := func(cause error) (kanban.Run, error) {
		if _, cerr := session.CompleteKanbanRun(s.rt.SessionDir, run.ID, kanban.RunStatusFailed, "", cause.Error()); cerr != nil {
			providers.DebugLogf("complete dispatch-failed kanban run %s: %v", run.ID, cerr)
		}
		s.notifyKanbanUpdated(task.SpaceID, task.ID)
		return kanban.Run{}, cause
	}

	p, err := session.GetParticipant(s.rt.SessionDir, targetID)
	if err != nil {
		return fail(err)
	}
	if p.Kind != participant.KindNamed {
		return fail(fmt.Errorf("participant %q is not a named agent", targetID))
	}
	if s.participantIsBusy(targetID) {
		return fail(fmt.Errorf("participant %q is busy; wait for it to finish", firstNonEmpty(p.Name, targetID)))
	}

	workspace, err := s.resolvedParticipantWorkspace(p)
	if err != nil {
		return fail(err)
	}
	outputDir := filepath.Join(workspace, "kanban-runs", run.ID)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fail(fmt.Errorf("create run output dir: %w", err))
	}
	memory, err := s.readParticipantMemory(p)
	if err != nil {
		return fail(err)
	}
	priorArtifacts, err := session.ListKanbanArtifacts(s.rt.SessionDir, task.ID)
	if err != nil {
		return fail(err)
	}
	manifest, err := participant.LoadManifest(workspace)
	if err != nil {
		return fail(err)
	}
	overlay, err := participant.LoadPromptOverlay(workspace)
	if err != nil {
		return fail(err)
	}
	requestPrompt := kanbanRunRequestPrompt(task, run, priorArtifacts, outputDir)
	if overlay != "" || len(manifest.Skills) > 0 {
		requestPrompt = kanbanStandingInstructionsPrompt(overlay, manifest.Skills) + requestPrompt
	}
	prompt := namedParticipantPrompt(p, memory, requestPrompt, s.registeredWorkspaces())
	modelOverride, clientOverride, err := resolveParticipantModelOverride(
		newRuntimeSessionReference(s.rt), p.Name, strings.TrimSpace(p.Model), workerProviderName(s.rt))
	if err != nil {
		return fail(err)
	}

	th, err := s.ensureThreadLoaded(hostThreadID)
	if err != nil {
		return fail(err)
	}
	threadRuntime, err := s.ensureThreadRuntimeAfterAdmission(th)
	if err != nil {
		return fail(err)
	}
	if threadRuntime == nil || threadRuntime.AgentControl == nil {
		return fail(errors.New("kanban dispatch requires agent control"))
	}
	if !s.tryAcquireParticipantBusy(targetID, "task") {
		return fail(fmt.Errorf("participant %q is busy running another task", firstNonEmpty(p.Name, targetID)))
	}
	spawnReq := agentcontrol.SpawnRequest{
		Type:          resolveParticipantSubagentType("", p),
		TaskName:      p.Name,
		ParticipantID: targetID,
		AgentProfile:  p.Name,
		Description:   firstNonEmpty(p.Tagline, task.Title),
		Prompt:        prompt,
		ParentID:      hostThreadID,
		ParentPath:    agentthread.RootPath,
		Synchronous:   false,
	}
	if manifest.NormalizedPermissionTier() == participant.PermissionTierUnrestricted {
		// Non-nil empty slice: clear the file-scope whitelist for this run.
		spawnReq.FileScopeRoots = []string{}
	} else {
		// Tier workspace: the host thread's working roots plus the whole wuu
		// home, so the run can write its own output dir and read prior runs'
		// artifacts under the participant homes.
		spawnReq.FileScopeRoots = workspaces.BoundaryRoots(
			th.CWD, s.rt.WuuHome, memdir.ParticipantMemdir(s.rt.WuuHome, targetID))
	}
	if modelOverride != "" {
		spawnReq.ModelOverride = modelOverride
		spawnReq.ModelPin = strings.TrimSpace(p.Model)
	}
	if clientOverride != nil {
		spawnReq.ClientOverride = clientOverride
	}
	spawned, err := threadRuntime.AgentControl.Spawn(ctx, spawnReq)
	if err != nil {
		s.releaseParticipantBusy(targetID, "")
		return fail(err)
	}
	s.bindParticipantBusyAgent(targetID, spawned.AgentID)
	threadRuntime.AgentControl.SetAgentParticipantID(spawned.AgentID, targetID)
	_ = session.UpsertParticipantRun(s.rt.SessionDir, session.ParticipantRun{
		ID:            spawned.AgentID,
		ParticipantID: targetID,
		AgentID:       spawned.AgentID,
		TaskID:        task.Title,
		SessionID:     hostThreadID,
		Summary:       task.Title,
		Outcome:       "running",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	})
	run, err = session.StartKanbanRun(s.rt.SessionDir, run.ID, spawned.AgentID)
	if err != nil {
		providers.DebugLogf("bind kanban run %s to agent %s: %v", run.ID, spawned.AgentID, err)
	}
	s.notifyKanbanUpdated(task.SpaceID, task.ID)
	return run, nil
}

// kanbanRunRequestPrompt renders the per-run request: the distilled brief is
// the authority, prior artifacts and the source conversation are lazy
// references, and the output contract points at the run's output dir.
func kanbanRunRequestPrompt(task kanban.Task, run kanban.Run, priorArtifacts []kanban.Artifact, outputDir string) string {
	var b strings.Builder
	if run.Kind == kanban.RunKindReview {
		fmt.Fprintf(&b, "# Verification task: %s\n\n", strings.TrimSpace(task.Title))
		b.WriteString("You are the second pair of eyes on this task. You did not do the\n")
		b.WriteString("work and share none of the author's assumptions — that is the point.\n")
		b.WriteString("Verify the deliverables against the brief below and report concrete\n")
		b.WriteString("problems (missing requirements, defects, inconsistencies). Do not\n")
		b.WriteString("rewrite the deliverables yourself.\n\n")
	} else {
		fmt.Fprintf(&b, "# Task: %s\n\n", strings.TrimSpace(task.Title))
	}
	if brief := strings.TrimSpace(task.Brief); brief != "" {
		b.WriteString("## Brief\n\n")
		b.WriteString(brief)
		b.WriteString("\n\n")
	}
	b.WriteString("## References\n\n")
	if len(priorArtifacts) == 0 {
		b.WriteString("No prior artifacts from earlier runs of this task.\n")
	} else {
		b.WriteString("Artifacts from earlier runs of this task (read them only if useful):\n")
		for _, a := range priorArtifacts {
			fmt.Fprintf(&b, "- %s\n", a.Path)
		}
	}
	if threadID := strings.TrimSpace(task.SourceThreadID); threadID != "" {
		fmt.Fprintf(&b, "\nThis task crystallized from conversation thread %s; the brief above is the\n", threadID)
		b.WriteString("settled conclusion and normally all you need.\n")
	}
	b.WriteString("\n## Output contract\n\n")
	fmt.Fprintf(&b, "Write every deliverable file into this directory (it already exists):\n`%s`\n\n", outputDir)
	b.WriteString("Your final message must be a short summary (a few sentences) of what you\n")
	b.WriteString("did and the key decisions; it becomes the run record the human reviews.\n")
	return b.String()
}

// kanbanStandingInstructionsPrompt renders the manifest's standing layer
// (prompt overlay + designated skills) ahead of the per-run request.
func kanbanStandingInstructionsPrompt(overlay string, skills []string) string {
	var b strings.Builder
	b.WriteString("## Standing instructions\n\n")
	if strings.TrimSpace(overlay) != "" {
		b.WriteString(strings.TrimSpace(overlay))
		b.WriteString("\n\n")
	}
	if len(skills) > 0 {
		b.WriteString("Your designated skills (load them with load_skill when the task matches): ")
		b.WriteString(strings.Join(skills, ", "))
		b.WriteString("\n\n")
	}
	return b.String()
}

// ---- crystallize ----

// KanbanCrystallizeParams converts one intake conversation into draft tasks.
type KanbanCrystallizeParams struct {
	ThreadID  string `json:"thread_id"`
	CreatedBy string `json:"created_by,omitempty"`
}

type kanbanCrystallizedSubtask struct {
	Title           string `json:"title"`
	Brief           string `json:"brief"`
	SuggestedTarget string `json:"suggested_target"`
}

type kanbanCrystallizedPlan struct {
	Title    string                      `json:"title"`
	Brief    string                      `json:"brief"`
	Subtasks []kanbanCrystallizedSubtask `json:"subtasks"`
}

type kanbanCrystallizeSubtaskWire struct {
	kanbanTaskWire
	SuggestedTargetID   string `json:"suggested_target_id,omitempty"`
	SuggestedTargetName string `json:"suggested_target_name,omitempty"`
}

type kanbanCrystallizeResultWire struct {
	Task     kanbanTaskWire                 `json:"task"`
	Subtasks []kanbanCrystallizeSubtaskWire `json:"subtasks"`
}

// handleKanbanCrystallize is the phase transition out of conversation: a
// synchronous distiller worker reads the transcript and produces a settled
// brief plus an optional decomposition, which lands as draft tasks for the
// human checkpoint. Nothing here is dispatched — confirmation and dispatch
// are separate, explicit calls.
func (s *Server) handleKanbanCrystallize(ctx context.Context, req Request) error {
	var params KanbanCrystallizeParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	th, err := s.ensureThreadLoaded(threadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	th.mu.Lock()
	transcript := kanbanTranscript(cloneHistory(th.History))
	th.mu.Unlock()
	if transcript == "" {
		return s.writeResponse(req.ID, nil, errors.New("conversation is empty; nothing to crystallize"))
	}

	roster, err := session.ListParticipants(s.rt.SessionDir, participant.KindNamed)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadRuntime, err := s.ensureThreadRuntimeAfterAdmission(th)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if threadRuntime == nil || threadRuntime.AgentControl == nil {
		return s.writeResponse(req.ID, nil, errors.New("kanban crystallize requires agent control"))
	}
	spawned, err := threadRuntime.AgentControl.Spawn(ctx, agentcontrol.SpawnRequest{
		Type:        agentcontrol.DefaultSubagentType,
		TaskName:    "kanban-crystallize",
		Description: "Distill a conversation into a settled task brief",
		Prompt:      kanbanCrystallizePrompt(transcript, roster),
		ParentID:    threadID,
		ParentPath:  agentthread.RootPath,
		Synchronous: true,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	plan, err := parseKanbanCrystallizedPlan(spawned.Result)
	if err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("distillation did not produce a usable plan: %w", err))
	}

	createdBy := strings.TrimSpace(params.CreatedBy)
	workspaceID := strings.TrimSpace(th.WorkspaceID)
	parent, err := session.CreateKanbanTask(s.rt.SessionDir, kanban.Task{
		SpaceID: "global", WorkspaceID: workspaceID, Title: plan.Title, Brief: plan.Brief,
		Status: kanban.TaskStatusDraft, SourceThreadID: threadID, CreatedBy: createdBy,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	targetIDByName := map[string]string{}
	for _, p := range roster {
		targetIDByName[strings.ToLower(strings.TrimSpace(p.Name))] = p.ID
	}
	result := kanbanCrystallizeResultWire{Task: kanbanTaskToWire(parent)}
	for i, sub := range plan.Subtasks {
		if strings.TrimSpace(sub.Title) == "" {
			continue
		}
		child, err := session.CreateKanbanTask(s.rt.SessionDir, kanban.Task{
			SpaceID: "global", WorkspaceID: workspaceID, ParentID: parent.ID, Title: sub.Title, Brief: sub.Brief,
			Status: kanban.TaskStatusDraft, SourceThreadID: threadID, CreatedBy: createdBy,
			SortIndex: i + 1,
		})
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		w := kanbanCrystallizeSubtaskWire{kanbanTaskWire: kanbanTaskToWire(child)}
		if name := strings.TrimSpace(sub.SuggestedTarget); name != "" {
			w.SuggestedTargetName = name
			w.SuggestedTargetID = targetIDByName[strings.ToLower(name)]
		}
		result.Subtasks = append(result.Subtasks, w)
	}
	s.notifyKanbanUpdated("global", parent.ID)
	return s.writeResponse(req.ID, result, nil)
}

// kanbanTranscript flattens conversation history for the distiller, keeping
// the tail (where convergence lives) and marking any front truncation.
func kanbanTranscript(history []providers.ChatMessage) string {
	const (
		perMessageCap = 2000
		totalCap      = 60000
	)
	var parts []string
	for _, msg := range history {
		role := strings.TrimSpace(msg.Role)
		if role == "" || role == "system" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if len([]rune(content)) > perMessageCap {
			content = string([]rune(content)[:perMessageCap]) + " …"
		}
		parts = append(parts, role+": "+content)
	}
	for len(parts) > 0 && len(strings.Join(parts, "\n\n")) > totalCap {
		parts = parts[1:]
	}
	return strings.Join(parts, "\n\n")
}

// kanbanCrystallizePrompt instructs the distiller. The brief it produces is
// the authority executors will read; the conversation itself stays behind as
// a lazy reference, so the brief must stand on its own.
func kanbanCrystallizePrompt(transcript string, roster []participant.Participant) string {
	var b strings.Builder
	b.WriteString("You distill a conversation into task records for a kanban board.\n\n")
	b.WriteString("Read the transcript and produce ONLY a JSON object (no prose, no fences):\n")
	b.WriteString(`{"title": "short task title", "brief": "markdown brief", "subtasks": [{"title": "...", "brief": "...", "suggested_target": "agent name"}]}`)
	b.WriteString("\n\nThe brief is the ONLY context the executing agent is guaranteed to read.\n")
	b.WriteString("It must stand alone: the settled goal, the done criteria, every hard\n")
	b.WriteString("constraint, and alternatives that were rejected (with why), each in one\n")
	b.WriteString("line. Drop the meandering, the dead ends, and anything superseded.\n\n")
	b.WriteString("Split into subtasks only when the work naturally parcels out to separate\n")
	b.WriteString("deliverables; otherwise return an empty subtasks array.\n")
	if len(roster) > 0 {
		b.WriteString("\nNamed agents available for suggested_target (use exact names, only when a\n")
		b.WriteString("subtask clearly fits one):\n")
		for _, p := range roster {
			fmt.Fprintf(&b, "- %s (%s)\n", strings.TrimSpace(p.Name), strings.TrimSpace(p.Tagline))
		}
	}
	b.WriteString("\n## Transcript\n\n")
	b.WriteString(transcript)
	return b.String()
}

// parseKanbanCrystallizedPlan extracts the JSON plan from the distiller's
// result, tolerating surrounding prose.
func parseKanbanCrystallizedPlan(result string) (kanbanCrystallizedPlan, error) {
	start := strings.Index(result, "{")
	end := strings.LastIndex(result, "}")
	if start < 0 || end <= start {
		return kanbanCrystallizedPlan{}, errors.New("no JSON object in distiller output")
	}
	var plan kanbanCrystallizedPlan
	if err := json.Unmarshal([]byte(result[start:end+1]), &plan); err != nil {
		return kanbanCrystallizedPlan{}, fmt.Errorf("parse distiller JSON: %w", err)
	}
	if strings.TrimSpace(plan.Title) == "" {
		return kanbanCrystallizedPlan{}, errors.New("distiller returned an empty title")
	}
	return plan, nil
}

// ---- participant manifest ----

type ParticipantManifestWire struct {
	Skills         []string `json:"skills"`
	PermissionTier string   `json:"permission_tier"`
	PromptOverlay  string   `json:"prompt_overlay"`
}

type ParticipantManifestParams struct {
	ParticipantID  string   `json:"participant_id"`
	Skills         []string `json:"skills,omitempty"`
	PermissionTier string   `json:"permission_tier,omitempty"`
	PromptOverlay  *string  `json:"prompt_overlay,omitempty"`
}

func (s *Server) participantManifestForWire(participantID string) (ParticipantManifestWire, string, error) {
	p, err := session.GetParticipant(s.rt.SessionDir, strings.TrimSpace(participantID))
	if err != nil {
		return ParticipantManifestWire{}, "", err
	}
	workspace, err := s.resolvedParticipantWorkspace(p)
	if err != nil {
		return ParticipantManifestWire{}, "", err
	}
	manifest, err := participant.LoadManifest(workspace)
	if err != nil {
		return ParticipantManifestWire{}, "", err
	}
	overlay, err := participant.LoadPromptOverlay(workspace)
	if err != nil {
		return ParticipantManifestWire{}, "", err
	}
	skills := manifest.Skills
	if skills == nil {
		skills = []string{}
	}
	return ParticipantManifestWire{
		Skills:         skills,
		PermissionTier: manifest.NormalizedPermissionTier(),
		PromptOverlay:  overlay,
	}, workspace, nil
}

func (s *Server) handleParticipantGetManifest(req Request) error {
	var params ParticipantManifestParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if strings.TrimSpace(params.ParticipantID) == "" {
		return s.writeResponse(req.ID, nil, errors.New("participant_id is required"))
	}
	wire, _, err := s.participantManifestForWire(params.ParticipantID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, wire, nil)
}

func (s *Server) handleParticipantSaveManifest(req Request) error {
	var params ParticipantManifestParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if strings.TrimSpace(params.ParticipantID) == "" {
		return s.writeResponse(req.ID, nil, errors.New("participant_id is required"))
	}
	_, workspace, err := s.participantManifestForWire(params.ParticipantID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if err := participant.SaveManifest(workspace, participant.Manifest{
		Skills:         params.Skills,
		PermissionTier: params.PermissionTier,
	}); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if params.PromptOverlay != nil {
		if err := participant.SavePromptOverlay(workspace, *params.PromptOverlay); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	wire, _, err := s.participantManifestForWire(params.ParticipantID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, wire, nil)
}

// ---- auto dispatch ----

// kanbanAutoDispatchActor marks runs created by the auto-dispatcher, so the
// completion hook can drain the next ready task only for autopilot batches —
// manual dispatches never cascade.
const kanbanAutoDispatchActor = "auto"

type KanbanAutoDispatchParams struct {
	SpaceID    string `json:"space_id"`
	ThreadID   string `json:"thread_id"`
	TargetID   string `json:"target_id,omitempty"`
}

type kanbanAutoDispatchSkippedWire struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
}

type kanbanAutoDispatchResultWire struct {
	Dispatched []kanbanRunWire                 `json:"dispatched"`
	Skipped    []kanbanAutoDispatchSkippedWire `json:"skipped"`
}

// resolveDefaultDispatchTarget names the autopilot's fallback owner: Andy
// (the seeded generalist), else the first active named agent.
func (s *Server) resolveDefaultDispatchTarget() (string, error) {
	roster, err := session.ListParticipants(s.rt.SessionDir, participant.KindNamed)
	if err != nil {
		return "", err
	}
	for _, p := range roster {
		if strings.EqualFold(strings.TrimSpace(p.Name), defaultSeedParticipantName) {
			return p.ID, nil
		}
	}
	for _, p := range roster {
		return p.ID, nil
	}
	return "", errors.New("no named agent available; create one first")
}

// kanbanAutoDispatchEligible filters a space's full task list to
// dispatchable leaves, preserving scan order (sort_index, created_at). A task
// is eligible when it is ready, has no children (parents are containers), and
// its parent is past the draft stage.
func kanbanAutoDispatchEligible(tasks []kanban.Task) []kanban.Task {
	childrenByParent := map[string]int{}
	byID := map[string]kanban.Task{}
	for _, t := range tasks {
		byID[t.ID] = t
		if t.ParentID != "" {
			childrenByParent[t.ParentID]++
		}
	}
	var out []kanban.Task
	for _, t := range tasks {
		if t.Status != kanban.TaskStatusReady || childrenByParent[t.ID] > 0 {
			continue
		}
		if t.ParentID != "" {
			parent, ok := byID[t.ParentID]
			if !ok || parent.Status == kanban.TaskStatusDraft || parent.Status == kanban.TaskStatusCancelled {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// autoDispatchKanbanTasks drains the space's eligible tasks toward one
// target, serially: the busy lock admits one run, the rest are reported as
// skipped and drained later by the autopilot completion hook.
func (s *Server) autoDispatchKanbanTasks(ctx context.Context, spaceID, hostThreadID, targetID string) (kanbanAutoDispatchResultWire, error) {
	tasks, err := session.ListAllKanbanTasks(s.rt.SessionDir, spaceID)
	if err != nil {
		return kanbanAutoDispatchResultWire{}, err
	}
	eligible := kanbanAutoDispatchEligible(tasks)
	result := kanbanAutoDispatchResultWire{
		Dispatched: []kanbanRunWire{},
		Skipped:    []kanbanAutoDispatchSkippedWire{},
	}
	busy := false
	for _, task := range eligible {
		if busy {
			result.Skipped = append(result.Skipped, kanbanAutoDispatchSkippedWire{TaskID: task.ID, Reason: "target busy; drains after the active run"})
			continue
		}
		run, err := s.dispatchKanbanRun(ctx, hostThreadID, task.ID, targetID, kanban.RunKindExecute, kanbanAutoDispatchActor)
		if err != nil {
			if errors.Is(err, kanban.ErrTargetBusy) {
				busy = true
				result.Skipped = append(result.Skipped, kanbanAutoDispatchSkippedWire{TaskID: task.ID, Reason: "target busy; drains after the active run"})
				continue
			}
			result.Skipped = append(result.Skipped, kanbanAutoDispatchSkippedWire{TaskID: task.ID, Reason: err.Error()})
			continue
		}
		busy = true
		result.Dispatched = append(result.Dispatched, kanbanRunToWire(run))
	}
	return result, nil
}

func (s *Server) handleKanbanAutoDispatch(ctx context.Context, req Request) error {
	var params KanbanAutoDispatchParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	spaceID := strings.TrimSpace(params.SpaceID)
	if spaceID == "" {
		spaceID = "global"
	}
	hostThreadID := strings.TrimSpace(params.ThreadID)
	if hostThreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	targetID := strings.TrimSpace(params.TargetID)
	if targetID == "" {
		var err error
		targetID, err = s.resolveDefaultDispatchTarget()
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	result, err := s.autoDispatchKanbanTasks(ctx, spaceID, hostThreadID, targetID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, result, nil)
}

// maybeAutoDispatchNextKanbanRun is the autopilot drain: after an auto-batch
// run reaches a terminal state, offer the space's remaining ready tasks to
// the same target. Errors only log — the board already reflects the finished
// run, and any successful follow-up dispatch emits its own kanban/updated.
func (s *Server) maybeAutoDispatchNextKanbanRun(run kanban.Run) {
	if run.CreatedBy != kanbanAutoDispatchActor || strings.TrimSpace(run.HostThreadID) == "" {
		return
	}
	if _, err := s.autoDispatchKanbanTasks(context.Background(), run.SpaceID, run.HostThreadID, run.TargetID); err != nil {
		providers.DebugLogf("auto-dispatch drain for space %s: %v", run.SpaceID, err)
	}
}

// rollupKanbanParent parks a container parent in human review once every
// child has reached review or done.
func (s *Server) rollupKanbanParent(taskID string) {
	task, err := session.GetKanbanTask(s.rt.SessionDir, taskID)
	if err != nil || task.ParentID == "" {
		return
	}
	siblings, err := session.ListKanbanTasks(s.rt.SessionDir, task.SpaceID, task.ParentID)
	if err != nil || len(siblings) == 0 {
		return
	}
	for _, sibling := range siblings {
		if sibling.Status != kanban.TaskStatusReview && sibling.Status != kanban.TaskStatusDone {
			return
		}
	}
	parent, err := session.GetKanbanTask(s.rt.SessionDir, task.ParentID)
	if err != nil {
		return
	}
	if parent.Status != kanban.TaskStatusReady && parent.Status != kanban.TaskStatusRunning {
		return
	}
	if _, err := session.TransitionKanbanTaskStatus(s.rt.SessionDir, parent.ID, kanban.TaskStatusReview); err != nil {
		providers.DebugLogf("rollup kanban parent %s to review: %v", parent.ID, err)
		return
	}
	s.notifyKanbanUpdated(parent.SpaceID, parent.ID)
}

// completeKanbanRunForAgent folds a spawned execution's terminal outcome back
// into its kanban run, collects produced artifacts, and notifies the board.
// It no-ops for participant runs that are not kanban executions.
func (s *Server) completeKanbanRunForAgent(participantID, agentID string, status subagent.Status, summary string) {
	if s.rt == nil || strings.TrimSpace(s.rt.SessionDir) == "" {
		return
	}
	run, err := session.GetActiveKanbanRunByThreadID(s.rt.SessionDir, agentID)
	if errors.Is(err, kanban.ErrRunNotFound) {
		return
	}
	if err != nil {
		providers.DebugLogf("lookup kanban run for agent %s: %v", agentID, err)
		return
	}
	runStatus := kanban.RunStatusInterrupted
	errorMessage := ""
	switch status {
	case subagent.StatusCompleted:
		runStatus = kanban.RunStatusSucceeded
	case subagent.StatusFailed:
		runStatus = kanban.RunStatusFailed
		errorMessage = summary
	}
	s.collectKanbanRunArtifacts(participantID, run)
	completedRun, err := session.CompleteKanbanRun(s.rt.SessionDir, run.ID, runStatus, summary, errorMessage)
	if err != nil {
		providers.DebugLogf("complete kanban run %s for agent %s: %v", run.ID, agentID, err)
		return
	}
	s.notifyKanbanUpdated(run.SpaceID, run.TaskID)
	s.rollupKanbanParent(run.TaskID)
	s.maybeAutoDispatchNextKanbanRun(completedRun)
}

// collectKanbanRunArtifacts attributes every file the run left in its output
// dir to the run and its task. Best-effort: scan failures only log.
func (s *Server) collectKanbanRunArtifacts(participantID string, run kanban.Run) {
	p, err := session.GetParticipant(s.rt.SessionDir, participantID)
	if err != nil {
		providers.DebugLogf("load participant %s for artifact scan: %v", participantID, err)
		return
	}
	workspace, err := s.resolvedParticipantWorkspace(p)
	if err != nil {
		providers.DebugLogf("resolve participant %s workspace for artifact scan: %v", participantID, err)
		return
	}
	outputDir := filepath.Join(workspace, "kanban-runs", run.ID)
	_ = filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if strings.HasPrefix(name, ".") {
			return nil
		}
		rel, err := filepath.Rel(outputDir, path)
		if err != nil {
			return nil
		}
		if _, err := session.AddKanbanArtifact(s.rt.SessionDir, kanban.Artifact{
			RunID: run.ID, TaskID: run.TaskID, SpaceID: run.SpaceID, WorkspaceID: run.WorkspaceID,
			Path:        filepath.ToSlash(rel),
			DisplayName: name,
			MediaType:   mime.TypeByExtension(filepath.Ext(name)),
			SizeBytes:   info.Size(),
		}); err != nil {
			providers.DebugLogf("record kanban artifact %s for run %s: %v", rel, run.ID, err)
		}
		return nil
	})
}
