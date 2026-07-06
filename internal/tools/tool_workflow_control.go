package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	memstore "github.com/blueberrycongee/wuu/internal/memory/store"
	prompttext "github.com/blueberrycongee/wuu/internal/prompt"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

// ---------------------------------------------------------------------------
// workflow_control
// ---------------------------------------------------------------------------

type WorkflowControlTool struct{ env *Env }

func NewWorkflowControlTool(env *Env) *WorkflowControlTool { return &WorkflowControlTool{env: env} }

func (t *WorkflowControlTool) Name() string            { return "workflow_control" }
func (t *WorkflowControlTool) IsReadOnly() bool        { return false }
func (t *WorkflowControlTool) IsConcurrencySafe() bool { return false }

func (t *WorkflowControlTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "workflow_control",
		Description: "Update durable Workflow Run state after planning, spawning agents, or synthesizing results. " +
			"Use this to bind spawn_agent outputs back to a workflow run. The runtime enforces valid run, phase, " +
			"and agent-run state transitions; do not invent states. Use pause_run / resume_run for blocked workflow recovery and " +
			"retry_agent_run for bounded Agent Run retries. Before complete_run=true, mark every phase completed, failed, or skipped. " +
			"Use create_file_checkpoint / restore_file_checkpoint for scoped file rollback.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type": "string",
					"enum": []string{
						"set_run_status",
						"pause_run",
						"resume_run",
						"set_phase_status",
						"record_workflow_team",
						"record_agent_run",
						"retry_agent_run",
						"write_final_report",
						"generate_final_report",
						"record_memory_candidate",
						"review_memory_candidate",
						"create_file_checkpoint",
						"restore_file_checkpoint",
					},
					"description": "Control action to perform.",
				},
				"run_id": map[string]any{
					"type":        "string",
					"description": "Workflow run id.",
				},
				"status": map[string]any{
					"type":        "string",
					"description": "New status for set_run_status, set_phase_status, or record_agent_run.",
				},
				"message": map[string]any{
					"type":        "string",
					"description": "Optional reason, error, blocker, or status note.",
				},
				"pause_reason": map[string]any{
					"type":        "string",
					"description": "Structured active pause reason for pause_run or set_run_status status=paused.",
				},
				"resume_hint": map[string]any{
					"type":        "string",
					"description": "What must happen before a paused workflow can resume.",
				},
				"rollback_hint": map[string]any{
					"type":        "string",
					"description": "Optional runtime rollback or checkpoint guidance for a paused workflow or retried agent run.",
				},
				"phase_id": map[string]any{
					"type":        "string",
					"description": "Phase id for set_phase_status or record_agent_run.",
				},
				"agent_run_id": map[string]any{
					"type":        "string",
					"description": "Workflow-local Agent Run id. Defaults to agent_id for record_agent_run.",
				},
				"agent_id": map[string]any{
					"type":        "string",
					"description": "Agent id returned by spawn_agent.",
				},
				"task_name": map[string]any{
					"type":        "string",
					"description": "Task name returned by spawn_agent.",
				},
				"agent_profile": map[string]any{
					"type":        "string",
					"description": "Durable Agent Profile used for this run, if any.",
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "Task prompt or brief used for the agent run.",
				},
				"result": map[string]any{
					"type":        "string",
					"description": "Agent result summary or final output to bind to the run for record_agent_run.",
				},
				"report_path": map[string]any{
					"type":        "string",
					"description": "Structured agent_report path filed by the spawned agent.",
				},
				"changed_files": map[string]any{
					"type":        "array",
					"description": "Changed files from the spawned agent's agent_report.",
					"items":       map[string]any{"type": "string"},
				},
				"artifacts": map[string]any{
					"type":        "array",
					"description": "Artifact paths from the spawned agent's agent_report.",
					"items":       map[string]any{"type": "string"},
				},
				"paths": map[string]any{
					"type":        "array",
					"description": "Workspace-relative file paths for create_file_checkpoint.",
					"items":       map[string]any{"type": "string"},
				},
				"checkpoint_id": map[string]any{
					"type":        "string",
					"description": "File checkpoint id for create_file_checkpoint or restore_file_checkpoint.",
				},
				"retry_count": map[string]any{
					"type":        "integer",
					"description": "Recorded retry count for record_agent_run. retry_agent_run increments this automatically.",
				},
				"max_retries": map[string]any{
					"type":        "integer",
					"description": "Bounded retry limit for retry_agent_run. Defaults to 2 when unset.",
				},
				"retry_reason": map[string]any{
					"type":        "string",
					"description": "Structured reason for retrying an Agent Run.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Final report markdown for write_final_report, or memory candidate content for record_memory_candidate.",
				},
				"candidate_id": map[string]any{
					"type":        "string",
					"description": "Memory candidate id for review_memory_candidate.",
				},
				"target": map[string]any{
					"type":        "string",
					"enum":        []string{"memory", "user"},
					"description": "Memory target for record_memory_candidate. Defaults to memory.",
				},
				"team": workflowTeamInputSchema("Workflow Team members chosen by the agent before spawning workflow workers. Use mode reuse_profile for existing profiles with saved memory, create_profile for new recurring profiles, and ephemeral for one-off workers without profile memory."),
				"tags": map[string]any{
					"type":        "array",
					"description": "Tags for record_memory_candidate.",
					"items":       map[string]any{"type": "string"},
				},
				"source": map[string]any{
					"type":        "string",
					"description": "Source label for record_memory_candidate, for example agent_report or workflow.",
				},
				"complete_run": map[string]any{
					"type":        "boolean",
					"description": "For write_final_report, also transition the run to completed.",
				},
			},
			"required": []string{"action", "run_id"},
		},
	}
}

func workflowTeamInputSchema(description string) map[string]any {
	return map[string]any{
		"type":        "array",
		"description": description,
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":            map[string]any{"type": "string", "description": "Optional stable team member id."},
				"role":          map[string]any{"type": "string", "description": "Role needed for this workflow run."},
				"mode":          map[string]any{"type": "string", "enum": []string{"reuse_profile", "create_profile", "ephemeral", "named_participant"}},
				"agent_profile": map[string]any{"type": "string", "description": "Profile name for reuse_profile/create_profile; omit for ephemeral and named_participant."},
				"participant":   map[string]any{"type": "string", "description": "Named group member (name or participant id) for mode named_participant. Must be a member of this workflow's group thread; the named agent runs under its own durable identity and memory."},
				"task_name":     map[string]any{"type": "string", "description": "Suggested spawn_agent task_name."},
				"phase_id":      map[string]any{"type": "string", "description": "Optional planned phase id for this member."},
				"prompt":        map[string]any{"type": "string", "description": "Optional task brief to use when spawning this member. " + prompttext.AgentBriefContractSummary() + " " + prompttext.WorkflowBriefExtensionSummary()},
				"reason":        map[string]any{"type": "string", "description": "Why this member should reuse/create a profile or stay ephemeral."},
			},
			"required": []string{"role", "mode"},
		},
	}
}

type workflowControlArgs struct {
	Action       string                    `json:"action"`
	RunID        string                    `json:"run_id"`
	Status       string                    `json:"status"`
	Message      string                    `json:"message"`
	PauseReason  string                    `json:"pause_reason"`
	ResumeHint   string                    `json:"resume_hint"`
	RollbackHint string                    `json:"rollback_hint"`
	PhaseID      string                    `json:"phase_id"`
	AgentRunID   string                    `json:"agent_run_id"`
	AgentID      string                    `json:"agent_id"`
	TaskName     string                    `json:"task_name"`
	AgentProfile string                    `json:"agent_profile"`
	Prompt       string                    `json:"prompt"`
	Result       string                    `json:"result"`
	ReportPath   string                    `json:"report_path"`
	ChangedFiles []string                  `json:"changed_files"`
	Artifacts    []string                  `json:"artifacts"`
	Paths        []string                  `json:"paths"`
	CheckpointID string                    `json:"checkpoint_id"`
	RetryCount   int                       `json:"retry_count"`
	MaxRetries   int                       `json:"max_retries"`
	RetryReason  string                    `json:"retry_reason"`
	Content      string                    `json:"content"`
	CandidateID  string                    `json:"candidate_id"`
	Target       string                    `json:"target"`
	Team         []workflowTeamMemberInput `json:"team"`
	TeamPlan     []workflowTeamMemberInput `json:"team_plan"`
	Tags         []string                  `json:"tags"`
	Source       string                    `json:"source"`
	CompleteRun  bool                      `json:"complete_run"`
}

type workflowTeamMemberInput struct {
	ID           string `json:"id"`
	Role         string `json:"role"`
	Mode         string `json:"mode"`
	AgentProfile string `json:"agent_profile"`
	Participant  string `json:"participant"`
	TaskName     string `json:"task_name"`
	PhaseID      string `json:"phase_id"`
	Prompt       string `json:"prompt"`
	Reason       string `json:"reason"`
}

func (t *WorkflowControlTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args workflowControlArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	action := strings.TrimSpace(args.Action)
	if action == "" {
		return "", errors.New("workflow_control requires action")
	}
	if strings.TrimSpace(args.RunID) == "" {
		return "", errors.New("workflow_control requires run_id")
	}
	store, err := workflowStoreForEnv(t.env)
	if err != nil {
		return "", fmt.Errorf("resolve workflow store: %w", err)
	}

	// Execute-time task-lead gate: a conversation-native named agent may drive a
	// run only while it still leads the task cth that run is bound to. Validated
	// against the run's own binding (not merely the caller's active tasks) so a
	// lead cannot use one task's identity to manipulate another task's run.
	// Self-contained callers (no participant identity) skip the LoadRun and pass
	// through unchanged.
	if strings.TrimSpace(t.env.ParticipantID) != "" {
		gateRun, err := store.LoadRun(args.RunID)
		if err != nil {
			return "", err
		}
		if err := requireWorkflowControlLead(t.env, gateRun); err != nil {
			return "", err
		}
	}

	switch action {
	case "set_run_status":
		status, err := parseWorkflowRunState(args.Status)
		if err != nil {
			return "", err
		}
		run, err := store.UpdateRunStatusWithDetails(args.RunID, workflow.RunStatusUpdate{
			Status:       status,
			Message:      args.Message,
			PauseReason:  args.PauseReason,
			ResumeHint:   args.ResumeHint,
			RollbackHint: args.RollbackHint,
		})
		if err != nil {
			return "", err
		}
		return workflowControlRunResult(action, run, nil)

	case "pause_run":
		run, err := store.UpdateRunStatusWithDetails(args.RunID, workflow.RunStatusUpdate{
			Status:       workflow.RunStatePaused,
			Message:      args.Message,
			PauseReason:  args.PauseReason,
			ResumeHint:   args.ResumeHint,
			RollbackHint: args.RollbackHint,
		})
		if err != nil {
			return "", err
		}
		return workflowControlRunResult(action, run, nil)

	case "resume_run":
		previous, err := store.LoadRun(args.RunID)
		if err != nil {
			return "", err
		}
		startInitialScriptRun := shouldStartInitialPausedScriptRun(previous)
		var profileResolution []workflow.ProfileResolution
		resumeScript := ""
		resumeMaxAgents := 0
		resumeMaxConcurrency := 0
		if startInitialScriptRun {
			profileResolution, err = unresolvedRequiredProfilesForRun(t.env, previous)
			if err != nil {
				return "", err
			}
			if missing := workflow.MissingRequiredProfiles(profileResolution); len(missing) > 0 {
				return "", fmt.Errorf("cannot resume workflow run %q: missing required Agent Profiles: %s", previous.ID, strings.Join(workflowProfileResolutionNames(missing), ", "))
			}
			scriptPath := strings.TrimSpace(previous.ScriptPath)
			if scriptPath == "" {
				return "", errors.New("workflow script path is empty")
			}
			data, err := os.ReadFile(scriptPath)
			if err != nil {
				return "", fmt.Errorf("read workflow script: %w", err)
			}
			resumeScript = string(data)
			resumeMaxAgents, resumeMaxConcurrency = workflowScriptLimitsForRun(t.env, previous)
		}
		run, err := store.UpdateRunStatusWithDetails(args.RunID, workflow.RunStatusUpdate{
			Status:  workflow.RunStateRunning,
			Message: args.Message,
		})
		if err != nil {
			return "", err
		}
		background := false
		if startInitialScriptRun {
			runtime := newWorkflowScriptRuntime(t.env, store, run, resumeScript, resumeMaxAgents, resumeMaxConcurrency)
			// UpdateRunStatusWithDetails above already persisted the
			// durable started marker (Status=running); the background
			// helper adds the live-registry entry, goal sync, and error
			// logging so a failure is never silently swallowed.
			runWorkflowScriptInBackground(runtime, run.ID, "workflow_control resume_run")
			background = true
		}
		return workflowControlRunResult(action, run, map[string]any{
			"background":         background,
			"profile_resolution": profileResolution,
		})

	case "set_phase_status":
		if strings.TrimSpace(args.PhaseID) == "" {
			return "", errors.New("workflow_control set_phase_status requires phase_id")
		}
		status, err := parseWorkflowPhaseState(args.Status)
		if err != nil {
			return "", err
		}
		run, err := store.UpdatePhaseStatus(args.RunID, args.PhaseID, status, args.Message)
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"action": action, "run": run})

	case "record_workflow_team", "record_team_plan":
		plan, err := workflowTeamPlanFromControlArgs(t.env, store, args)
		if err != nil {
			return "", err
		}
		plan, err = store.SaveTeamPlan(plan)
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{
			"action":        "record_workflow_team",
			"workflow_team": plan,
			"next_steps": []string{
				"Write each spawn_agent.prompt from the Base Agent Brief Contract, adding only the small context extension that applies.",
				"Include workflow run, phase, team member, and result-binding context for workflow team members.",
				"Spawn reuse_profile and create_profile members with spawn_agent.agent_profile plus the workflow goal_id and goal_dir.",
				"Spawn ephemeral members without agent_profile, but still pass the workflow goal_id and goal_dir.",
				"After each worker finishes, bind its result back with workflow_control action=record_agent_run.",
			},
		})

	case "record_agent_run":
		agent, err := workflowAgentRunFromControlArgs(args)
		if err != nil {
			return "", err
		}
		existingRun, err := store.LoadRun(agent.WorkflowRunID)
		if err != nil {
			return "", err
		}
		if err := enforceWorkflowAgentCap(t.env, store, existingRun, []string{agent.ID}); err != nil {
			return "", err
		}
		if err := store.UpsertAgentRun(agent); err != nil {
			return "", err
		}
		var run workflow.Run
		if strings.TrimSpace(agent.PhaseID) != "" {
			run, err = store.AttachAgentRunToPhase(agent.WorkflowRunID, agent.PhaseID, agent.ID)
			if err != nil {
				return "", err
			}
		} else {
			run, _ = store.LoadRun(agent.WorkflowRunID)
		}
		loaded, err := store.LoadAgentRun(agent.WorkflowRunID, agent.ID)
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"action": action, "run": run, "agent_run": loaded})

	case "retry_agent_run":
		agentRunID := strings.TrimSpace(args.AgentRunID)
		if agentRunID == "" {
			agentRunID = strings.TrimSpace(args.AgentID)
		}
		if agentRunID == "" {
			return "", errors.New("workflow_control retry_agent_run requires agent_run_id or agent_id")
		}
		reason := firstWorkflowText(args.RetryReason, args.Message)
		agent, err := store.RequestAgentRunRetry(args.RunID, agentRunID, workflow.AgentRetryRequest{
			Reason:       reason,
			MaxRetries:   args.MaxRetries,
			RollbackHint: args.RollbackHint,
		})
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{
			"action":    action,
			"agent_run": agent,
			"next_steps": []string{
				"Spawn or message the same Agent Profile with the retry context.",
				"After the retry finishes, bind its result back with workflow_control action=record_agent_run.",
			},
		})

	case "write_final_report":
		content := strings.TrimSpace(args.Content)
		if content == "" {
			return "", errors.New("workflow_control write_final_report requires content")
		}
		if args.CompleteRun {
			run, err := store.LoadRun(args.RunID)
			if err != nil {
				return "", err
			}
			agents, err := store.ListAgentRuns(args.RunID)
			if err != nil {
				return "", err
			}
			if err := validateWorkflowReadyToComplete(run, agents); err != nil {
				return "", err
			}
		}
		path, err := store.WriteFinalReport(args.RunID, content)
		if err != nil {
			return "", err
		}
		run, err := store.LoadRun(args.RunID)
		if err != nil {
			return "", err
		}
		if args.CompleteRun {
			run, err = store.UpdateRunStatus(args.RunID, workflow.RunStateCompleted, args.Message)
			if err != nil {
				return "", err
			}
			return workflowControlRunResult(action, run, map[string]any{"final_report_path": path})
		}
		return mustJSON(map[string]any{"action": action, "run": run, "final_report_path": path})

	case "generate_final_report":
		run, err := store.LoadRun(args.RunID)
		if err != nil {
			return "", err
		}
		agents, err := store.ListAgentRuns(args.RunID)
		if err != nil {
			return "", err
		}
		candidates, err := store.ListMemoryCandidates(args.RunID)
		if err != nil {
			return "", err
		}
		checkpoints, err := store.ListFileCheckpoints(args.RunID)
		if err != nil {
			return "", err
		}
		if args.CompleteRun {
			if err := validateWorkflowReadyToComplete(run, agents); err != nil {
				return "", err
			}
		}
		report := renderGeneratedWorkflowFinalReport(run, agents, candidates, checkpoints)
		path, err := store.WriteFinalReport(args.RunID, report)
		if err != nil {
			return "", err
		}
		run, err = store.LoadRun(args.RunID)
		if err != nil {
			return "", err
		}
		if args.CompleteRun {
			run, err = store.UpdateRunStatus(args.RunID, workflow.RunStateCompleted, args.Message)
			if err != nil {
				return "", err
			}
			return workflowControlRunResult(action, run, map[string]any{"final_report_path": path, "content": report})
		}
		return mustJSON(map[string]any{"action": action, "run": run, "final_report_path": path, "content": report})

	case "record_memory_candidate":
		content := strings.TrimSpace(args.Content)
		if content == "" {
			return "", errors.New("workflow_control record_memory_candidate requires content")
		}
		candidate, err := store.AddMemoryCandidate(workflow.MemoryCandidate{
			ID:           strings.TrimSpace(args.CandidateID),
			RunID:        strings.TrimSpace(args.RunID),
			AgentRunID:   strings.TrimSpace(args.AgentRunID),
			AgentProfile: strings.TrimSpace(args.AgentProfile),
			Target:       strings.TrimSpace(args.Target),
			Content:      content,
			Tags:         trimWorkflowStringSlice(args.Tags),
			Source:       strings.TrimSpace(args.Source),
			Reason:       strings.TrimSpace(args.Message),
		})
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"action": action, "memory_candidate": candidate})

	case "review_memory_candidate":
		candidateID := strings.TrimSpace(args.CandidateID)
		if candidateID == "" {
			return "", errors.New("workflow_control review_memory_candidate requires candidate_id")
		}
		status, err := parseWorkflowMemoryCandidateStatus(args.Status)
		if err != nil {
			return "", err
		}
		if status == workflow.MemoryCandidatePending {
			return "", errors.New("workflow_control review_memory_candidate requires accepted or rejected status")
		}
		candidate, err := store.UpdateMemoryCandidateStatus(args.RunID, candidateID, status, args.Message)
		if err != nil {
			return "", err
		}
		result := map[string]any{"action": action, "memory_candidate": candidate}
		if status == workflow.MemoryCandidateAccepted {
			result["memory_write"] = persistAcceptedWorkflowMemoryCandidate(ctx, t.env, candidate)
		}
		return mustJSON(result)

	case "create_file_checkpoint":
		checkpoint, err := createWorkflowFileCheckpoint(t.env, store, args)
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"action": action, "file_checkpoint": checkpoint})

	case "restore_file_checkpoint":
		checkpoint, restored, err := restoreWorkflowFileCheckpoint(t.env, store, args)
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"action": action, "file_checkpoint": checkpoint, "restored_files": restored})

	default:
		return "", fmt.Errorf("unsupported workflow_control action %q", action)
	}
}

func workflowControlRunResult(action string, run workflow.Run, extra map[string]any) (string, error) {
	result := map[string]any{
		"action": action,
		"run":    run,
	}
	for key, value := range extra {
		result[key] = value
	}
	if strings.TrimSpace(run.GoalDir) != "" {
		state, err := syncWorkflowGoalStatus(run)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(state.ID) != "" {
			result["goal_status"] = map[string]any{
				"goal_id":    state.ID,
				"goal_dir":   run.GoalDir,
				"state_path": filepath.Join(run.GoalDir, "state.json"),
				"status":     state.Status,
				"step":       state.CurrentStep,
			}
		}
	}
	return mustJSON(result)
}

func persistAcceptedWorkflowMemoryCandidate(ctx context.Context, env *Env, candidate workflow.MemoryCandidate) map[string]any {
	if env == nil || env.Memory == nil {
		return map[string]any{
			"persisted": false,
			"reason":    "profile_memory_not_configured",
		}
	}
	target, err := normalizeMemoryTarget(candidate.Target)
	if err != nil {
		return map[string]any{
			"persisted": false,
			"reason":    err.Error(),
		}
	}
	// Accepted workflow memory candidates are durable, cross-project workflow
	// facts, and this path is gated on the global store (env.Memory) above, so
	// persist them to the global layer explicitly rather than the default
	// "workspace" write scope.
	args := writeMemoryArgs{
		Action:  memoryActionAdd,
		Scope:   memoryScopeGlobal,
		Target:  target,
		Content: candidate.Content,
		Tags:    workflowMemoryCandidateTags(candidate),
		Source:  string(memstore.SourceTool),
	}
	payload, err := json.Marshal(args)
	if err != nil {
		return map[string]any{
			"persisted": false,
			"reason":    "encode_memory_write_failed",
			"error":     err.Error(),
		}
	}
	result, err := NewWriteMemoryTool(env).Execute(ctx, string(payload))
	if err != nil {
		return map[string]any{
			"persisted": false,
			"target":    target,
			"reason":    "write_memory_failed",
			"error":     err.Error(),
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		return map[string]any{
			"persisted": true,
			"target":    target,
			"raw":       result,
		}
	}
	return map[string]any{
		"persisted": true,
		"target":    target,
		"result":    decoded,
	}
}

func workflowMemoryCandidateTags(candidate workflow.MemoryCandidate) []string {
	tags := memoryUserTags(candidate.Tags)
	tags = append(tags, "workflow")
	if runID := strings.TrimSpace(candidate.RunID); runID != "" {
		tags = append(tags, "workflow_run:"+runID)
	}
	if candidateID := strings.TrimSpace(candidate.ID); candidateID != "" {
		tags = append(tags, "workflow_candidate:"+candidateID)
	}
	if source := strings.TrimSpace(candidate.Source); source != "" {
		tags = append(tags, "source:"+source)
	}
	if profile := strings.TrimSpace(candidate.AgentProfile); profile != "" {
		tags = append(tags, "agent_profile:"+profile)
	}
	return tags
}

func workflowAgentRunFromControlArgs(args workflowControlArgs) (workflow.AgentRun, error) {
	agentRunID := strings.TrimSpace(args.AgentRunID)
	if agentRunID == "" {
		agentRunID = strings.TrimSpace(args.AgentID)
	}
	if agentRunID == "" {
		return workflow.AgentRun{}, errors.New("workflow_control record_agent_run requires agent_run_id or agent_id")
	}
	var status workflow.AgentRunState
	if strings.TrimSpace(args.Status) != "" {
		parsed, err := parseWorkflowAgentRunState(args.Status)
		if err != nil {
			return workflow.AgentRun{}, err
		}
		status = parsed
	}
	agent := workflow.AgentRun{
		ID:            agentRunID,
		WorkflowRunID: strings.TrimSpace(args.RunID),
		PhaseID:       strings.TrimSpace(args.PhaseID),
		AgentID:       strings.TrimSpace(args.AgentID),
		TaskName:      strings.TrimSpace(args.TaskName),
		AgentProfile:  strings.TrimSpace(args.AgentProfile),
		Status:        status,
		Prompt:        strings.TrimSpace(args.Prompt),
		Result:        strings.TrimSpace(args.Result),
		ReportPath:    strings.TrimSpace(args.ReportPath),
		ChangedFiles:  trimWorkflowStringSlice(args.ChangedFiles),
		Artifacts:     trimWorkflowStringSlice(args.Artifacts),
		RetryCount:    args.RetryCount,
		MaxRetries:    args.MaxRetries,
		RetryReason:   strings.TrimSpace(args.RetryReason),
		RollbackHint:  strings.TrimSpace(args.RollbackHint),
		Error:         strings.TrimSpace(args.Message),
	}
	if agent.AgentID == "" {
		agent.AgentID = agent.ID
	}
	return agent, nil
}

func workflowTeamPlanFromControlArgs(env *Env, store *workflow.Store, args workflowControlArgs) (workflow.TeamPlan, error) {
	inputs := workflowTeamInputs(args)
	if len(inputs) == 0 {
		return workflow.TeamPlan{}, errors.New("workflow_control record_workflow_team requires team")
	}
	run, err := store.LoadRun(args.RunID)
	if err != nil {
		return workflow.TeamPlan{}, err
	}
	if run.DefinitionName != "" {
		if def, ok := env.FindWorkflow(run.DefinitionName); ok && def.MaxAgents > 0 && len(inputs) > def.MaxAgents {
			return workflow.TeamPlan{}, fmt.Errorf("workflow %q supports at most %d planned agents", def.Name, def.MaxAgents)
		}
	}
	wuuHome, err := statepath.Home("")
	if err != nil {
		return workflow.TeamPlan{}, err
	}

	// groupPool is the run's named-participant membership pool, resolved lazily
	// (and once) the first time a named_participant member is seen. Nil until
	// then; an explicit empty resolution is still enforced (no members = every
	// named_participant reference rejected).
	var groupPool []workflow.ParticipantRef
	groupPoolResolved := false

	used := make(map[string]int, len(inputs))
	members := make([]workflow.TeamMember, 0, len(inputs))
	for _, input := range inputs {
		role := strings.TrimSpace(input.Role)
		if role == "" {
			return workflow.TeamPlan{}, errors.New("workflow_control record_workflow_team requires every team member role")
		}
		modeRaw := input.Mode
		if strings.TrimSpace(modeRaw) == "" && strings.TrimSpace(input.Participant) != "" {
			modeRaw = string(workflow.TeamMemberNamedParticipant)
		}
		mode, err := parseWorkflowTeamMemberMode(modeRaw, input.AgentProfile)
		if err != nil {
			return workflow.TeamPlan{}, err
		}
		memberID := workflowTeamMemberID(input.ID, role, input.AgentProfile, used)
		member := workflow.TeamMember{
			ID:           memberID,
			Role:         role,
			Mode:         mode,
			AgentProfile: strings.TrimSpace(input.AgentProfile),
			TaskName:     strings.TrimSpace(input.TaskName),
			PhaseID:      strings.TrimSpace(input.PhaseID),
			Prompt:       strings.TrimSpace(input.Prompt),
			Reason:       strings.TrimSpace(input.Reason),
		}
		if member.TaskName == "" {
			member.TaskName = member.ID
		}
		switch member.Mode {
		case workflow.TeamMemberNamedParticipant:
			if !groupPoolResolved {
				groupPool = workflowRunGroupMembers(env, run)
				groupPoolResolved = true
			}
			ref, err := resolveWorkflowNamedParticipant(input.Participant, groupPool)
			if err != nil {
				return workflow.TeamPlan{}, fmt.Errorf("team member %q: %w", member.ID, err)
			}
			member.AgentProfile = ""
			member.ParticipantID = ref.ID
			member.ParticipantName = ref.Name
		case workflow.TeamMemberReuseProfile:
			if member.AgentProfile == "" {
				return workflow.TeamPlan{}, fmt.Errorf("team member %q mode reuse_profile requires agent_profile", member.ID)
			}
			if _, ok, err := workflow.LoadProfile(wuuHome, member.AgentProfile); err != nil {
				return workflow.TeamPlan{}, err
			} else if !ok {
				return workflow.TeamPlan{}, fmt.Errorf("team member %q reuses missing profile %q; choose create_profile or ephemeral", member.ID, member.AgentProfile)
			}
		case workflow.TeamMemberCreateProfile:
			if member.AgentProfile == "" {
				return workflow.TeamPlan{}, fmt.Errorf("team member %q mode create_profile requires agent_profile", member.ID)
			}
			profile, created, err := workflow.EnsureProfile(workflow.ProfileEnsureOptions{
				WuuHome:      wuuHome,
				Name:         member.AgentProfile,
				Source:       "workflow",
				WorkflowName: run.DefinitionName,
				Role:         member.Role,
				Description:  member.Reason,
			})
			if err != nil {
				return workflow.TeamPlan{}, err
			}
			member.AgentProfile = profile.Name
			member.CreatedProfile = created
		case workflow.TeamMemberEphemeral:
			if member.AgentProfile != "" {
				return workflow.TeamPlan{}, fmt.Errorf("team member %q mode ephemeral must not set agent_profile", member.ID)
			}
		}
		members = append(members, member)
	}
	return workflow.TeamPlan{RunID: strings.TrimSpace(args.RunID), Members: members}, nil
}

// resolveWorkflowNamedParticipant matches a team member's participant
// reference (name or id) against the run's group membership pool. Only members
// of the bound group thread may fill named_participant slots; an empty pool or
// an unknown reference is rejected so a workflow can never orchestrate a named
// agent outside its own group.
func resolveWorkflowNamedParticipant(ref string, pool []workflow.ParticipantRef) (workflow.ParticipantRef, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return workflow.ParticipantRef{}, errors.New("mode named_participant requires participant (name or id)")
	}
	if len(pool) == 0 {
		return workflow.ParticipantRef{}, fmt.Errorf("this workflow is not bound to a group thread; cannot add named participant %q", ref)
	}
	for _, member := range pool {
		if strings.TrimSpace(member.ID) == ref {
			return acceptWorkflowNamedParticipant(member)
		}
	}
	for _, member := range pool {
		if strings.EqualFold(strings.TrimSpace(member.Name), ref) {
			return acceptWorkflowNamedParticipant(member)
		}
	}
	return workflow.ParticipantRef{}, fmt.Errorf("named participant %q is not a member of this workflow's group", ref)
}

// acceptWorkflowNamedParticipant refuses a resolved member that is already
// executing another task/workflow run (decision-five concurrency lock): a
// group's named agent can't be enlisted into a second workflow while busy, so
// the agent is told to wait or fork a copy (decision six) instead of two
// callers racing the same resident identity.
func acceptWorkflowNamedParticipant(member workflow.ParticipantRef) (workflow.ParticipantRef, error) {
	if member.Busy {
		name := strings.TrimSpace(member.Name)
		if name == "" {
			name = strings.TrimSpace(member.ID)
		}
		return workflow.ParticipantRef{}, fmt.Errorf("named participant %q is busy running another task; wait for it to finish or fork a copy", name)
	}
	return member, nil
}

func workflowTeamInputs(args workflowControlArgs) []workflowTeamMemberInput {
	if len(args.Team) > 0 {
		return args.Team
	}
	return args.TeamPlan
}

func parseWorkflowTeamMemberMode(raw, agentProfile string) (workflow.TeamMemberMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		if strings.TrimSpace(agentProfile) != "" {
			return workflow.TeamMemberReuseProfile, nil
		}
		return workflow.TeamMemberEphemeral, nil
	case "reuse", "reuse_profile":
		return workflow.TeamMemberReuseProfile, nil
	case "create", "create_profile":
		return workflow.TeamMemberCreateProfile, nil
	case "ephemeral", "memoryless":
		return workflow.TeamMemberEphemeral, nil
	case "named", "named_participant", "participant":
		return workflow.TeamMemberNamedParticipant, nil
	default:
		return "", fmt.Errorf("unsupported workflow team member mode %q", raw)
	}
}

func workflowTeamMemberID(rawID, role, profile string, used map[string]int) string {
	base := slugWorkflowID(rawID)
	if base == "" {
		base = slugWorkflowID(role)
	}
	if base == "" {
		base = slugWorkflowID(profile)
	}
	if base == "" {
		base = "member"
	}
	count := used[base]
	used[base] = count + 1
	if count == 0 {
		return base
	}
	return fmt.Sprintf("%s_%d", base, count+1)
}

func trimWorkflowStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstWorkflowText(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func enforceWorkflowAgentCap(env *Env, store *workflow.Store, run workflow.Run, candidateIDs []string) error {
	capacity := workflowAgentCap(env, run)
	existing, err := store.ListAgentRuns(run.ID)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(existing)+len(candidateIDs))
	for _, agent := range existing {
		seen[agent.ID] = struct{}{}
	}
	newCount := 0
	for _, id := range candidateIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		newCount++
	}
	if len(existing)+newCount > capacity {
		return fmt.Errorf("workflow run %q would exceed max agent runs (%d)", run.ID, capacity)
	}
	return nil
}

func workflowAgentCap(env *Env, run workflow.Run) int {
	const hardCap = workflow.DefaultScriptMaxAgents
	capacity := hardCap
	if env != nil && strings.TrimSpace(run.DefinitionName) != "" {
		if def, ok := env.FindWorkflow(run.DefinitionName); ok && def.MaxAgents > 0 && def.MaxAgents < capacity {
			capacity = def.MaxAgents
		}
	}
	return capacity
}

func createWorkflowFileCheckpoint(env *Env, store *workflow.Store, args workflowControlArgs) (workflow.FileCheckpoint, error) {
	if len(args.Paths) == 0 {
		return workflow.FileCheckpoint{}, errors.New("workflow_control create_file_checkpoint requires paths")
	}
	if _, err := store.LoadRun(args.RunID); err != nil {
		return workflow.FileCheckpoint{}, err
	}
	checkpointID := strings.TrimSpace(args.CheckpointID)
	if checkpointID == "" {
		checkpointID = "checkpoint-" + session.NewID()
	}
	checkpointDir, err := store.CheckpointDir(args.RunID, checkpointID)
	if err != nil {
		return workflow.FileCheckpoint{}, err
	}
	filesDir := filepath.Join(checkpointDir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		return workflow.FileCheckpoint{}, err
	}

	seen := map[string]struct{}{}
	files := make([]workflow.FileCheckpointFile, 0, len(args.Paths))
	for _, rawPath := range args.Paths {
		absPath, err := env.ResolvePath(rawPath)
		if err != nil {
			return workflow.FileCheckpoint{}, err
		}
		relPath, err := filepath.Rel(env.RootDir, absPath)
		if err != nil {
			return workflow.FileCheckpoint{}, err
		}
		relPath = filepath.ToSlash(relPath)
		if _, ok := seen[relPath]; ok {
			continue
		}
		seen[relPath] = struct{}{}

		entry := workflow.FileCheckpointFile{Path: relPath}
		data, readErr := os.ReadFile(absPath)
		if readErr == nil {
			entry.Existed = true
			entry.Size = int64(len(data))
			entry.SnapshotPath = filepath.ToSlash(filepath.Join("files", workflowCheckpointSnapshotName(len(files), relPath)))
			if err := os.WriteFile(filepath.Join(checkpointDir, filepath.FromSlash(entry.SnapshotPath)), data, 0o644); err != nil {
				return workflow.FileCheckpoint{}, err
			}
		} else if !os.IsNotExist(readErr) {
			return workflow.FileCheckpoint{}, readErr
		}
		files = append(files, entry)
	}
	if len(files) == 0 {
		return workflow.FileCheckpoint{}, errors.New("workflow_control create_file_checkpoint requires at least one valid path")
	}
	return store.SaveFileCheckpoint(workflow.FileCheckpoint{
		ID:     checkpointID,
		RunID:  strings.TrimSpace(args.RunID),
		Reason: firstWorkflowText(args.Message, args.RollbackHint),
		Files:  files,
	})
}

func restoreWorkflowFileCheckpoint(env *Env, store *workflow.Store, args workflowControlArgs) (workflow.FileCheckpoint, []string, error) {
	checkpointID := strings.TrimSpace(args.CheckpointID)
	if checkpointID == "" {
		return workflow.FileCheckpoint{}, nil, errors.New("workflow_control restore_file_checkpoint requires checkpoint_id")
	}
	checkpoint, err := store.LoadFileCheckpoint(args.RunID, checkpointID)
	if err != nil {
		return workflow.FileCheckpoint{}, nil, err
	}
	checkpointDir, err := store.CheckpointDir(args.RunID, checkpointID)
	if err != nil {
		return workflow.FileCheckpoint{}, nil, err
	}
	restored := make([]string, 0, len(checkpoint.Files))
	for _, file := range checkpoint.Files {
		if strings.TrimSpace(file.Path) == "" {
			continue
		}
		absPath, err := env.ResolvePath(file.Path)
		if err != nil {
			return workflow.FileCheckpoint{}, nil, err
		}
		if !file.Existed {
			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				return workflow.FileCheckpoint{}, nil, err
			}
			restored = append(restored, file.Path)
			continue
		}
		snapshotAbs, err := checkpointSnapshotAbsPath(checkpointDir, file.SnapshotPath)
		if err != nil {
			return workflow.FileCheckpoint{}, nil, err
		}
		data, err := os.ReadFile(snapshotAbs)
		if err != nil {
			return workflow.FileCheckpoint{}, nil, err
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return workflow.FileCheckpoint{}, nil, err
		}
		if err := os.WriteFile(absPath, data, 0o644); err != nil {
			return workflow.FileCheckpoint{}, nil, err
		}
		restored = append(restored, file.Path)
	}
	checkpoint, err = store.MarkFileCheckpointRestored(args.RunID, checkpointID, args.Message)
	if err != nil {
		return workflow.FileCheckpoint{}, nil, err
	}
	return checkpoint, restored, nil
}

func workflowCheckpointSnapshotName(index int, relPath string) string {
	slug := slugWorkflowID(relPath)
	if slug == "" {
		slug = "file"
	}
	return fmt.Sprintf("%03d-%s.snapshot", index+1, slug)
}

func checkpointSnapshotAbsPath(checkpointDir, snapshotPath string) (string, error) {
	snapshotPath = filepath.Clean(filepath.FromSlash(strings.TrimSpace(snapshotPath)))
	if snapshotPath == "" || filepath.IsAbs(snapshotPath) || snapshotPath == "." || strings.HasPrefix(snapshotPath, ".."+string(filepath.Separator)) || snapshotPath == ".." {
		return "", fmt.Errorf("invalid checkpoint snapshot path %q", snapshotPath)
	}
	return filepath.Join(checkpointDir, snapshotPath), nil
}

func validateWorkflowReadyToComplete(run workflow.Run, agents []workflow.AgentRun) error {
	for _, phase := range run.Phases {
		if !workflow.IsTerminalPhaseState(phase.Status) {
			return fmt.Errorf("workflow phase %q is %s; cannot complete run", phase.ID, phase.Status)
		}
	}
	for _, agent := range agents {
		if !workflow.IsTerminalAgentRunState(agent.Status) {
			return fmt.Errorf("workflow agent run %q is %s; cannot complete run", agent.ID, agent.Status)
		}
		if agent.Status == workflow.AgentRunStateCompleted && strings.TrimSpace(agent.ReportPath) == "" {
			return fmt.Errorf("workflow agent run %q completed without agent_report; cannot complete run", agent.ID)
		}
	}
	return nil
}

func renderGeneratedWorkflowFinalReport(run workflow.Run, agents []workflow.AgentRun, candidates []workflow.MemoryCandidate, checkpoints []workflow.FileCheckpoint) string {
	var b strings.Builder
	b.WriteString("# Workflow Final Report\n\n")
	fmt.Fprintf(&b, "- Run: `%s`\n", run.ID)
	if run.DefinitionName != "" {
		fmt.Fprintf(&b, "- Workflow: `%s`\n", run.DefinitionName)
	}
	if strings.TrimSpace(run.Arguments) != "" {
		fmt.Fprintf(&b, "- Arguments: %s\n", strings.TrimSpace(run.Arguments))
	}
	fmt.Fprintf(&b, "- Status: `%s`\n", run.Status)
	if run.PauseReason != "" {
		fmt.Fprintf(&b, "- Pause reason: %s\n", run.PauseReason)
	}
	if run.ResumeHint != "" {
		fmt.Fprintf(&b, "- Resume hint: %s\n", run.ResumeHint)
	}
	if run.RollbackHint != "" {
		fmt.Fprintf(&b, "- Rollback hint: %s\n", run.RollbackHint)
	}
	if !run.StartedAt.IsZero() {
		fmt.Fprintf(&b, "- Started: %s\n", run.StartedAt.Format(time.RFC3339))
	}
	if !run.CompletedAt.IsZero() {
		fmt.Fprintf(&b, "- Completed: %s\n", run.CompletedAt.Format(time.RFC3339))
	}

	if len(run.Phases) > 0 {
		b.WriteString("\n## Phases\n\n")
		for _, phase := range run.Phases {
			fmt.Fprintf(&b, "- `%s`: %s", phase.ID, phase.Status)
			if phase.Name != "" {
				fmt.Fprintf(&b, " - %s", phase.Name)
			}
			if len(phase.AgentRunIDs) > 0 {
				fmt.Fprintf(&b, " (agents: %s)", strings.Join(phase.AgentRunIDs, ", "))
			}
			if phase.Error != "" {
				fmt.Fprintf(&b, " - %s", phase.Error)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n## Agent Runs\n\n")
	if len(agents) == 0 {
		b.WriteString("No Agent Runs recorded.\n")
	} else {
		for _, agent := range agents {
			label := agent.TaskName
			if label == "" {
				label = agent.ID
			}
			fmt.Fprintf(&b, "- `%s`: %s", agent.ID, agent.Status)
			if label != agent.ID {
				fmt.Fprintf(&b, " - %s", label)
			}
			if agent.AgentProfile != "" {
				fmt.Fprintf(&b, " [profile: `%s`]", agent.AgentProfile)
			}
			if agent.ReportMissing {
				b.WriteString(" [missing agent_report]")
			}
			if agent.RetryCount > 0 {
				if agent.MaxRetries > 0 {
					fmt.Fprintf(&b, " [retries: %d/%d]", agent.RetryCount, agent.MaxRetries)
				} else {
					fmt.Fprintf(&b, " [retries: %d]", agent.RetryCount)
				}
			}
			b.WriteString("\n")
			if agent.ReportPath != "" {
				fmt.Fprintf(&b, "  - Report: %s\n", agent.ReportPath)
			}
			if agent.RetryReason != "" {
				fmt.Fprintf(&b, "  - Retry reason: %s\n", agent.RetryReason)
			}
			if agent.RollbackHint != "" {
				fmt.Fprintf(&b, "  - Rollback hint: %s\n", agent.RollbackHint)
			}
			if agent.Error != "" {
				fmt.Fprintf(&b, "  - Error: %s\n", agent.Error)
			}
			if excerpt := trimReportExcerpt(agent.Result, 240); excerpt != "" {
				fmt.Fprintf(&b, "  - Result: %s\n", excerpt)
			}
			if len(agent.ChangedFiles) > 0 {
				fmt.Fprintf(&b, "  - Changed files: %s\n", strings.Join(agent.ChangedFiles, ", "))
			}
			if len(agent.Artifacts) > 0 {
				fmt.Fprintf(&b, "  - Artifacts: %s\n", strings.Join(agent.Artifacts, ", "))
			}
		}
	}

	changed := uniqueWorkflowStringsFromAgents(agents, func(agent workflow.AgentRun) []string {
		return agent.ChangedFiles
	})
	if len(changed) > 0 {
		b.WriteString("\n## Changed Files\n\n")
		for _, path := range changed {
			fmt.Fprintf(&b, "- %s\n", path)
		}
	}

	b.WriteString("\n## Memory Candidates\n\n")
	if len(candidates) == 0 {
		b.WriteString("No memory candidates recorded.\n")
	} else {
		for _, candidate := range candidates {
			fmt.Fprintf(&b, "- `%s` [%s/%s]: %s", candidate.ID, candidate.Target, candidate.Status, candidate.Content)
			if candidate.AgentProfile != "" {
				fmt.Fprintf(&b, " (profile: `%s`)", candidate.AgentProfile)
			}
			if candidate.Reason != "" {
				fmt.Fprintf(&b, " - %s", candidate.Reason)
			}
			b.WriteString("\n")
		}
	}

	if len(checkpoints) > 0 {
		b.WriteString("\n## File Checkpoints\n\n")
		for _, checkpoint := range checkpoints {
			fmt.Fprintf(&b, "- `%s`: %d file(s)", checkpoint.ID, len(checkpoint.Files))
			if checkpoint.Reason != "" {
				fmt.Fprintf(&b, " - %s", checkpoint.Reason)
			}
			if !checkpoint.RestoredAt.IsZero() {
				fmt.Fprintf(&b, " [restored: %s]", checkpoint.RestoredAt.Format(time.RFC3339))
			}
			b.WriteString("\n")
		}
	}

	open := workflowOpenItems(run, agents)
	if len(open) > 0 {
		b.WriteString("\n## Open Items\n\n")
		for _, item := range open {
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}
	return b.String()
}

func trimReportExcerpt(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" || maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func uniqueWorkflowStringsFromAgents(agents []workflow.AgentRun, collect func(workflow.AgentRun) []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, agent := range agents {
		for _, value := range collect(agent) {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func workflowOpenItems(run workflow.Run, agents []workflow.AgentRun) []string {
	var out []string
	if run.Status == workflow.RunStatePaused {
		if run.PauseReason != "" {
			out = append(out, fmt.Sprintf("Workflow is paused: %s", run.PauseReason))
		} else {
			out = append(out, "Workflow is paused.")
		}
	}
	for _, phase := range run.Phases {
		if !workflow.IsTerminalPhaseState(phase.Status) {
			out = append(out, fmt.Sprintf("Phase `%s` is `%s`.", phase.ID, phase.Status))
		}
	}
	for _, agent := range agents {
		if !workflow.IsTerminalAgentRunState(agent.Status) {
			out = append(out, fmt.Sprintf("Agent Run `%s` is `%s`.", agent.ID, agent.Status))
		}
	}
	return out
}
