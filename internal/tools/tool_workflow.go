package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

// ---------------------------------------------------------------------------
// list_workflows
// ---------------------------------------------------------------------------

type ListWorkflowsTool struct{ env *Env }

func NewListWorkflowsTool(env *Env) *ListWorkflowsTool { return &ListWorkflowsTool{env: env} }

func (t *ListWorkflowsTool) Name() string            { return "list_workflows" }
func (t *ListWorkflowsTool) IsReadOnly() bool        { return true }
func (t *ListWorkflowsTool) IsConcurrencySafe() bool { return true }

func (t *ListWorkflowsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "list_workflows",
		Description: "List reusable workflow definitions discovered from project and user workflow directories. " +
			"Use this when the user asks for a repeatable, long-running, scheduled, or multi-agent task and you need " +
			"to choose a saved workflow before creating a durable workflow run.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *ListWorkflowsTool) Execute(_ context.Context, _ string) (string, error) {
	items := make([]map[string]any, 0, len(t.env.Workflows))
	for _, wf := range t.env.Workflows {
		item := map[string]any{
			"name":                   wf.Name,
			"description":            wf.Description,
			"when_to_use":            wf.WhenToUse,
			"source":                 wf.Source,
			"path":                   wf.Path,
			"argument_hint":          wf.ArgumentHint,
			"user_invocable":         wf.UserInvocable,
			"disable_model_invoke":   wf.DisableModelInvoke,
			"version":                wf.Version,
			"max_agents":             wf.MaxAgents,
			"max_concurrency":        wf.MaxConcurrency,
			"profiles":               wf.Profiles,
			"allow_profile_creation": wf.AllowProfileCreation,
			"memory_policy":          wf.MemoryPolicy,
		}
		items = append(items, item)
	}
	return mustJSON(map[string]any{
		"workflows": items,
		"count":     len(items),
	})
}

// ---------------------------------------------------------------------------
// load_workflow
// ---------------------------------------------------------------------------

type LoadWorkflowTool struct{ env *Env }

func NewLoadWorkflowTool(env *Env) *LoadWorkflowTool { return &LoadWorkflowTool{env: env} }

func (t *LoadWorkflowTool) Name() string            { return "load_workflow" }
func (t *LoadWorkflowTool) IsReadOnly() bool        { return true }
func (t *LoadWorkflowTool) IsConcurrencySafe() bool { return true }

func (t *LoadWorkflowTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "load_workflow",
		Description: "Load the full body of a reusable workflow definition. Workflow definitions are portable " +
			"orchestration assets, similar to skills, but they are used to create durable workflow runs. The returned " +
			"body may contain ${ARGUMENTS}, ${CLAUDE_WORKFLOW_DIR}, and ${CLAUDE_SESSION_ID} substitutions.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Workflow name. Leading slash is optional.",
				},
				"arguments": map[string]any{
					"type":        "string",
					"description": "Optional argument string substituted into ${ARGUMENTS}.",
				},
			},
			"required": []string{"name"},
		},
	}
}

func (t *LoadWorkflowTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Name) == "" {
		return "", errors.New("load_workflow requires name")
	}
	wf, ok := t.env.FindWorkflow(args.Name)
	if !ok {
		return "", fmt.Errorf("workflow %q not found. available: %s", args.Name, strings.Join(t.env.WorkflowNames(), ", "))
	}
	body := t.env.ProcessWorkflowBody(wf, args.Arguments)
	return mustJSON(map[string]any{
		"name":                   wf.Name,
		"description":            wf.Description,
		"when_to_use":            wf.WhenToUse,
		"source":                 wf.Source,
		"path":                   wf.Path,
		"dir":                    wf.Dir,
		"argument_hint":          wf.ArgumentHint,
		"version":                wf.Version,
		"max_agents":             wf.MaxAgents,
		"max_concurrency":        wf.MaxConcurrency,
		"profiles":               wf.Profiles,
		"allow_profile_creation": wf.AllowProfileCreation,
		"memory_policy":          wf.MemoryPolicy,
		"suggested_phase_names":  extractWorkflowPhaseNames(body),
		"content":                body,
	})
}

// ---------------------------------------------------------------------------
// create_workflow
// ---------------------------------------------------------------------------

type CreateWorkflowTool struct{ env *Env }

func NewCreateWorkflowTool(env *Env) *CreateWorkflowTool { return &CreateWorkflowTool{env: env} }

func (t *CreateWorkflowTool) Name() string            { return "create_workflow" }
func (t *CreateWorkflowTool) IsReadOnly() bool        { return false }
func (t *CreateWorkflowTool) IsConcurrencySafe() bool { return true }

func (t *CreateWorkflowTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "create_workflow",
		Description: "Create a durable Workflow Run from a reusable workflow definition or an ad hoc plan. " +
			"This records the workflow state, phase plan, event log, and plan artifact. It does not automatically " +
			"spawn agents; after creating a running workflow, use spawn_agent with agent_profile when named durable " +
			"profiles should work on phases, require agent_report before completion, then inspect progress with " +
			"workflow_status. Use this instead of manually carrying long-running workflow state in chat context.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"definition_name": map[string]any{
					"type":        "string",
					"description": "Optional saved workflow definition name. Omit for an ad hoc workflow.",
				},
				"arguments": map[string]any{
					"type":        "string",
					"description": "Arguments for the saved workflow, also stored on the run.",
				},
				"plan": map[string]any{
					"type":        "string",
					"description": "Markdown plan for this run. If omitted with definition_name, the workflow definition body is used.",
				},
				"phases": map[string]any{
					"type":        "array",
					"description": "Simple ordered phase plan. If omitted, phases are extracted from the plan or workflow body.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":   map[string]any{"type": "string", "description": "Optional stable phase id."},
							"name": map[string]any{"type": "string", "description": "Human-readable phase name."},
						},
						"required": []string{"name"},
					},
				},
				"initial_status": map[string]any{
					"type":        "string",
					"enum":        []string{"draft", "approval_pending", "running"},
					"description": "Initial run state. Defaults to running.",
				},
				"run_id": map[string]any{
					"type":        "string",
					"description": "Optional caller-provided run id. Omit for an auto-generated id.",
				},
			},
		},
	}
}

type workflowPhaseInput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (t *CreateWorkflowTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		DefinitionName string               `json:"definition_name"`
		Arguments      string               `json:"arguments"`
		Plan           string               `json:"plan"`
		Phases         []workflowPhaseInput `json:"phases"`
		InitialStatus  string               `json:"initial_status"`
		RunID          string               `json:"run_id"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	status, err := parseInitialWorkflowStatus(args.InitialStatus)
	if err != nil {
		return "", err
	}

	var def workflow.Definition
	var body string
	if strings.TrimSpace(args.DefinitionName) != "" {
		found, ok := t.env.FindWorkflow(args.DefinitionName)
		if !ok {
			return "", fmt.Errorf("workflow %q not found. available: %s", args.DefinitionName, strings.Join(t.env.WorkflowNames(), ", "))
		}
		def = found
		body = t.env.ProcessWorkflowBody(def, args.Arguments)
	}

	plan := strings.TrimSpace(args.Plan)
	if plan == "" {
		plan = strings.TrimSpace(body)
	}
	phases := buildWorkflowPhases(args.Phases, plan, status)
	if len(phases) == 0 {
		return "", errors.New("create_workflow requires at least one phase or a plan with extractable phases")
	}
	if len(phases) > 100 {
		return "", errors.New("create_workflow supports at most 100 phases")
	}

	runID := strings.TrimSpace(args.RunID)
	if runID == "" {
		runID = "workflow-" + session.NewID()
	}
	store, err := t.env.WorkflowStore()
	if err != nil {
		return "", fmt.Errorf("resolve workflow store: %w", err)
	}
	run, err := store.CreateRun(workflow.Run{
		ID:             runID,
		DefinitionName: def.Name,
		DefinitionPath: def.Path,
		Arguments:      strings.TrimSpace(args.Arguments),
		Status:         status,
		Phases:         phases,
	})
	if err != nil {
		return "", err
	}
	planPath := ""
	if plan != "" {
		planPath, err = store.WritePlan(run.ID, renderWorkflowPlan(def, args.Arguments, plan, phases))
		if err != nil {
			return "", err
		}
		run.PlanPath = planPath
	}

	return mustJSON(map[string]any{
		"run_id":          run.ID,
		"status":          run.Status,
		"definition_name": run.DefinitionName,
		"definition_path": run.DefinitionPath,
		"plan_path":       planPath,
		"phases":          run.Phases,
		"next_steps":      workflowNextSteps(status),
		"workflow_status": map[string]string{"run_id": run.ID},
	})
}

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
		Description: "Update durable Workflow Run state after planning, spawning agents, awaiting agents, or synthesizing results. " +
			"Use this to bind spawn_agent / await_agents outputs back to a workflow run. The runtime enforces valid run, phase, " +
			"and agent-run state transitions; do not invent states. If await_agents reports report_missing=true, record the " +
			"agent run as awaiting_report instead of completed. Use pause_run / resume_run for blocked workflow recovery and " +
			"retry_agent_run for bounded Agent Run retries.",
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
						"record_agent_run",
						"record_await_results",
						"retry_agent_run",
						"write_final_report",
						"generate_final_report",
						"record_memory_candidate",
						"review_memory_candidate",
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
					"description": "Agent id returned by spawn_agent / await_agents.",
				},
				"task_name": map[string]any{
					"type":        "string",
					"description": "Task name returned by spawn_agent / await_agents.",
				},
				"agent_profile": map[string]any{
					"type":        "string",
					"description": "Durable Agent Profile used for this run, if any.",
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "Task prompt or brief used for the agent run.",
				},
				"report_path": map[string]any{
					"type":        "string",
					"description": "Structured agent_report path returned by await_agents.",
				},
				"changed_files": map[string]any{
					"type":        "array",
					"description": "Changed files from await_agents / agent_report.",
					"items":       map[string]any{"type": "string"},
				},
				"artifacts": map[string]any{
					"type":        "array",
					"description": "Artifact paths from await_agents / agent_report.",
					"items":       map[string]any{"type": "string"},
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
				"await_results": map[string]any{
					"type":        "array",
					"description": "Raw results array returned by await_agents. Used by record_await_results to import multiple Agent Runs at once.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"agent_id":       map[string]any{"type": "string"},
							"task_name":      map[string]any{"type": "string"},
							"agent_profile":  map[string]any{"type": "string"},
							"agent_path":     map[string]any{"type": "string"},
							"status":         map[string]any{"type": "string"},
							"result":         map[string]any{"type": "string"},
							"error":          map[string]any{"type": "string"},
							"changed_files":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"report_path":    map[string]any{"type": "string"},
							"report_missing": map[string]any{"type": "boolean"},
							"artifacts":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"worktree_path":  map[string]any{"type": "string"},
							"input_tokens":   map[string]any{"type": "integer"},
							"output_tokens":  map[string]any{"type": "integer"},
							"duration_ms":    map[string]any{"type": "integer"},
						},
					},
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

type workflowControlArgs struct {
	Action       string                `json:"action"`
	RunID        string                `json:"run_id"`
	Status       string                `json:"status"`
	Message      string                `json:"message"`
	PauseReason  string                `json:"pause_reason"`
	ResumeHint   string                `json:"resume_hint"`
	RollbackHint string                `json:"rollback_hint"`
	PhaseID      string                `json:"phase_id"`
	AgentRunID   string                `json:"agent_run_id"`
	AgentID      string                `json:"agent_id"`
	TaskName     string                `json:"task_name"`
	AgentProfile string                `json:"agent_profile"`
	Prompt       string                `json:"prompt"`
	ReportPath   string                `json:"report_path"`
	ChangedFiles []string              `json:"changed_files"`
	Artifacts    []string              `json:"artifacts"`
	RetryCount   int                   `json:"retry_count"`
	MaxRetries   int                   `json:"max_retries"`
	RetryReason  string                `json:"retry_reason"`
	AwaitResults []workflowAwaitResult `json:"await_results"`
	Content      string                `json:"content"`
	CandidateID  string                `json:"candidate_id"`
	Target       string                `json:"target"`
	Tags         []string              `json:"tags"`
	Source       string                `json:"source"`
	CompleteRun  bool                  `json:"complete_run"`
}

type workflowAwaitResult struct {
	AgentID       string   `json:"agent_id"`
	TaskName      string   `json:"task_name"`
	AgentProfile  string   `json:"agent_profile"`
	AgentPath     string   `json:"agent_path"`
	Status        string   `json:"status"`
	Result        string   `json:"result"`
	Error         string   `json:"error"`
	ChangedFiles  []string `json:"changed_files"`
	ReportPath    string   `json:"report_path"`
	ReportMissing bool     `json:"report_missing"`
	Artifacts     []string `json:"artifacts"`
	WorktreePath  string   `json:"worktree_path"`
	InputTokens   int      `json:"input_tokens"`
	OutputTokens  int      `json:"output_tokens"`
	DurationMS    int64    `json:"duration_ms"`
}

func (t *WorkflowControlTool) Execute(_ context.Context, argsJSON string) (string, error) {
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
	store, err := t.env.WorkflowStore()
	if err != nil {
		return "", fmt.Errorf("resolve workflow store: %w", err)
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
		return mustJSON(map[string]any{"action": action, "run": run})

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
		return mustJSON(map[string]any{"action": action, "run": run})

	case "resume_run":
		run, err := store.UpdateRunStatusWithDetails(args.RunID, workflow.RunStatusUpdate{
			Status:  workflow.RunStateRunning,
			Message: args.Message,
		})
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{"action": action, "run": run})

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

	case "record_agent_run":
		agent, err := workflowAgentRunFromControlArgs(args)
		if err != nil {
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

	case "record_await_results":
		if len(args.AwaitResults) == 0 {
			return "", errors.New("workflow_control record_await_results requires await_results")
		}
		agents := make([]workflow.AgentRun, 0, len(args.AwaitResults))
		var run workflow.Run
		for _, result := range args.AwaitResults {
			agent, err := workflowAgentRunFromAwaitResult(args.RunID, args.PhaseID, result)
			if err != nil {
				return "", err
			}
			if err := store.UpsertAgentRun(agent); err != nil {
				return "", err
			}
			if strings.TrimSpace(agent.PhaseID) != "" {
				run, err = store.AttachAgentRunToPhase(agent.WorkflowRunID, agent.PhaseID, agent.ID)
				if err != nil {
					return "", err
				}
			}
			loaded, err := store.LoadAgentRun(agent.WorkflowRunID, agent.ID)
			if err != nil {
				return "", err
			}
			agents = append(agents, loaded)
		}
		if run.ID == "" {
			run, _ = store.LoadRun(args.RunID)
		}
		return mustJSON(map[string]any{"action": action, "run": run, "agent_runs": agents})

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
				"After the retry finishes, use await_agents and record_await_results again.",
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
		if args.CompleteRun {
			if err := validateWorkflowReadyToComplete(run, agents); err != nil {
				return "", err
			}
		}
		report := renderGeneratedWorkflowFinalReport(run, agents, candidates)
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
		return mustJSON(map[string]any{"action": action, "memory_candidate": candidate})

	default:
		return "", fmt.Errorf("unsupported workflow_control action %q", action)
	}
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

func workflowAgentRunFromAwaitResult(runID, phaseID string, result workflowAwaitResult) (workflow.AgentRun, error) {
	agentRunID := strings.TrimSpace(result.AgentID)
	if agentRunID == "" {
		agentRunID = strings.TrimSpace(result.TaskName)
	}
	if agentRunID == "" {
		agentRunID = slugWorkflowID(result.AgentPath)
	}
	if agentRunID == "" {
		return workflow.AgentRun{}, errors.New("workflow_control record_await_results requires each result to include agent_id, task_name, or agent_path")
	}
	status, err := workflowStatusFromAwaitResult(result)
	if err != nil {
		return workflow.AgentRun{}, err
	}
	agent := workflow.AgentRun{
		ID:            agentRunID,
		WorkflowRunID: strings.TrimSpace(runID),
		PhaseID:       strings.TrimSpace(phaseID),
		AgentID:       strings.TrimSpace(result.AgentID),
		AgentPath:     strings.TrimSpace(result.AgentPath),
		TaskName:      strings.TrimSpace(result.TaskName),
		AgentProfile:  strings.TrimSpace(result.AgentProfile),
		Status:        status,
		Result:        strings.TrimSpace(result.Result),
		ReportPath:    strings.TrimSpace(result.ReportPath),
		ReportMissing: result.ReportMissing,
		ChangedFiles:  trimWorkflowStringSlice(result.ChangedFiles),
		Artifacts:     trimWorkflowStringSlice(result.Artifacts),
		WorktreePath:  strings.TrimSpace(result.WorktreePath),
		InputTokens:   result.InputTokens,
		OutputTokens:  result.OutputTokens,
		DurationMS:    result.DurationMS,
		Error:         strings.TrimSpace(result.Error),
	}
	if agent.AgentID == "" {
		agent.AgentID = agent.ID
	}
	return agent, nil
}

func workflowStatusFromAwaitResult(result workflowAwaitResult) (workflow.AgentRunState, error) {
	if result.ReportMissing {
		return workflow.AgentRunStateAwaitingReport, nil
	}
	switch strings.ToLower(strings.TrimSpace(result.Status)) {
	case "", "pending", "queued":
		return workflow.AgentRunStateQueued, nil
	case "starting":
		return workflow.AgentRunStateStarting, nil
	case "running":
		return workflow.AgentRunStateRunning, nil
	case "awaiting_report":
		return workflow.AgentRunStateAwaitingReport, nil
	case "completed", "complete":
		return workflow.AgentRunStateCompleted, nil
	case "failed", "failure", "error", "stuck", "not_found":
		return workflow.AgentRunStateFailed, nil
	case "cancelled", "canceled":
		return workflow.AgentRunStateCancelled, nil
	default:
		return "", fmt.Errorf("unsupported await agent status %q", result.Status)
	}
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

func renderGeneratedWorkflowFinalReport(run workflow.Run, agents []workflow.AgentRun, candidates []workflow.MemoryCandidate) string {
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

// ---------------------------------------------------------------------------
// workflow_status
// ---------------------------------------------------------------------------

type WorkflowStatusTool struct{ env *Env }

func NewWorkflowStatusTool(env *Env) *WorkflowStatusTool { return &WorkflowStatusTool{env: env} }

func (t *WorkflowStatusTool) Name() string            { return "workflow_status" }
func (t *WorkflowStatusTool) IsReadOnly() bool        { return true }
func (t *WorkflowStatusTool) IsConcurrencySafe() bool { return true }

func (t *WorkflowStatusTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "workflow_status",
		Description: "Inspect durable Workflow Run state. Pass run_id to inspect one run, or omit run_id to list recent runs. " +
			"Use this after create_workflow, after awaiting workflow agents, before resuming a paused run, and before final synthesis.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"run_id": map[string]any{
					"type":        "string",
					"description": "Workflow run id. Omit to list recent runs.",
				},
				"include_events": map[string]any{
					"type":        "boolean",
					"description": "Whether to include recent event log entries for a specific run. Defaults false.",
				},
				"event_limit": map[string]any{
					"type":        "integer",
					"description": "Maximum event entries to return when include_events is true. Defaults 20.",
				},
			},
		},
	}
}

func (t *WorkflowStatusTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		RunID         string `json:"run_id"`
		IncludeEvents bool   `json:"include_events"`
		EventLimit    int    `json:"event_limit"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	store, err := t.env.WorkflowStore()
	if err != nil {
		return "", fmt.Errorf("resolve workflow store: %w", err)
	}
	runID := strings.TrimSpace(args.RunID)
	if runID == "" {
		runs, err := store.ListRuns()
		if err != nil {
			return "", err
		}
		return mustJSON(map[string]any{
			"runs":  reverseWorkflowRuns(runs),
			"count": len(runs),
		})
	}

	run, err := store.LoadRun(runID)
	if err != nil {
		return "", err
	}
	agents, err := store.ListAgentRuns(runID)
	if err != nil {
		return "", err
	}
	memoryCandidates, err := store.ListMemoryCandidates(runID)
	if err != nil {
		return "", err
	}
	result := map[string]any{
		"run":               run,
		"agent_runs":        agents,
		"memory_candidates": memoryCandidates,
	}
	if args.IncludeEvents {
		events, err := store.ListEvents(runID)
		if err != nil {
			return "", err
		}
		result["events"] = tailWorkflowEvents(events, args.EventLimit)
	}
	return mustJSON(result)
}

func parseInitialWorkflowStatus(raw string) (workflow.RunState, error) {
	switch strings.TrimSpace(raw) {
	case "", string(workflow.RunStateRunning):
		return workflow.RunStateRunning, nil
	case string(workflow.RunStateDraft):
		return workflow.RunStateDraft, nil
	case string(workflow.RunStateApprovalPending):
		return workflow.RunStateApprovalPending, nil
	default:
		return "", fmt.Errorf("invalid initial_status %q", raw)
	}
}

func parseWorkflowRunState(raw string) (workflow.RunState, error) {
	switch strings.TrimSpace(raw) {
	case string(workflow.RunStateDraft):
		return workflow.RunStateDraft, nil
	case string(workflow.RunStateApprovalPending):
		return workflow.RunStateApprovalPending, nil
	case string(workflow.RunStateRunning):
		return workflow.RunStateRunning, nil
	case string(workflow.RunStatePaused):
		return workflow.RunStatePaused, nil
	case string(workflow.RunStateCompleted):
		return workflow.RunStateCompleted, nil
	case string(workflow.RunStateFailed):
		return workflow.RunStateFailed, nil
	case string(workflow.RunStateCancelled):
		return workflow.RunStateCancelled, nil
	default:
		return "", fmt.Errorf("invalid workflow run status %q", raw)
	}
}

func parseWorkflowPhaseState(raw string) (workflow.PhaseState, error) {
	switch strings.TrimSpace(raw) {
	case string(workflow.PhaseStatePending):
		return workflow.PhaseStatePending, nil
	case string(workflow.PhaseStateRunnable):
		return workflow.PhaseStateRunnable, nil
	case string(workflow.PhaseStateRunning):
		return workflow.PhaseStateRunning, nil
	case string(workflow.PhaseStateCompleted):
		return workflow.PhaseStateCompleted, nil
	case string(workflow.PhaseStateBlocked):
		return workflow.PhaseStateBlocked, nil
	case string(workflow.PhaseStateFailed):
		return workflow.PhaseStateFailed, nil
	case string(workflow.PhaseStateSkipped):
		return workflow.PhaseStateSkipped, nil
	default:
		return "", fmt.Errorf("invalid workflow phase status %q", raw)
	}
}

func parseWorkflowAgentRunState(raw string) (workflow.AgentRunState, error) {
	switch strings.TrimSpace(raw) {
	case string(workflow.AgentRunStateQueued):
		return workflow.AgentRunStateQueued, nil
	case string(workflow.AgentRunStateStarting):
		return workflow.AgentRunStateStarting, nil
	case string(workflow.AgentRunStateRunning):
		return workflow.AgentRunStateRunning, nil
	case string(workflow.AgentRunStateAwaitingReport):
		return workflow.AgentRunStateAwaitingReport, nil
	case string(workflow.AgentRunStateCompleted):
		return workflow.AgentRunStateCompleted, nil
	case string(workflow.AgentRunStateFailed):
		return workflow.AgentRunStateFailed, nil
	case string(workflow.AgentRunStateRetrying):
		return workflow.AgentRunStateRetrying, nil
	case string(workflow.AgentRunStateCancelled):
		return workflow.AgentRunStateCancelled, nil
	default:
		return "", fmt.Errorf("invalid workflow agent run status %q", raw)
	}
}

func parseWorkflowMemoryCandidateStatus(raw string) (workflow.MemoryCandidateStatus, error) {
	switch strings.TrimSpace(raw) {
	case string(workflow.MemoryCandidatePending):
		return workflow.MemoryCandidatePending, nil
	case string(workflow.MemoryCandidateAccepted):
		return workflow.MemoryCandidateAccepted, nil
	case string(workflow.MemoryCandidateRejected):
		return workflow.MemoryCandidateRejected, nil
	default:
		return "", fmt.Errorf("invalid workflow memory candidate status %q", raw)
	}
}

func buildWorkflowPhases(inputs []workflowPhaseInput, plan string, runStatus workflow.RunState) []workflow.Phase {
	names := make([]workflowPhaseInput, 0, len(inputs))
	for _, input := range inputs {
		if strings.TrimSpace(input.Name) == "" {
			continue
		}
		names = append(names, input)
	}
	if len(names) == 0 {
		for _, name := range extractWorkflowPhaseNames(plan) {
			names = append(names, workflowPhaseInput{Name: name})
		}
	}
	if len(names) == 0 && strings.TrimSpace(plan) != "" {
		names = append(names, workflowPhaseInput{ID: "workflow", Name: "Workflow"})
	}

	used := make(map[string]int, len(names))
	phases := make([]workflow.Phase, 0, len(names))
	for i, input := range names {
		id := workflowPhaseID(input.ID, input.Name, used)
		status := workflow.PhaseStatePending
		if runStatus == workflow.RunStateRunning && i == 0 {
			status = workflow.PhaseStateRunnable
		}
		phases = append(phases, workflow.Phase{
			ID:     id,
			Name:   strings.TrimSpace(input.Name),
			Status: status,
		})
	}
	return phases
}

func workflowPhaseID(rawID, name string, used map[string]int) string {
	base := slugWorkflowID(rawID)
	if base == "" {
		base = slugWorkflowID(name)
	}
	if base == "" {
		base = "phase"
	}
	count := used[base]
	used[base] = count + 1
	if count == 0 {
		return base
	}
	return fmt.Sprintf("%s_%d", base, count+1)
}

func slugWorkflowID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func extractWorkflowPhaseNames(body string) []string {
	lines := strings.Split(body, "\n")
	inPhases := false
	phaseHeadingLevel := 0
	var names []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			level := leadingHashCount(trimmed)
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if strings.EqualFold(heading, "Phases") {
				inPhases = true
				phaseHeadingLevel = level
				continue
			}
			if inPhases && level <= phaseHeadingLevel {
				break
			}
		}
		if !inPhases {
			continue
		}
		if name := markdownListItemText(trimmed); name != "" {
			names = append(names, name)
			if len(names) >= 100 {
				break
			}
		}
	}
	return names
}

func leadingHashCount(value string) int {
	count := 0
	for _, r := range value {
		if r != '#' {
			break
		}
		count++
	}
	return count
}

func markdownListItemText(value string) string {
	if strings.HasPrefix(value, "- ") || strings.HasPrefix(value, "* ") {
		return strings.TrimSpace(value[2:])
	}
	idx := 0
	for idx < len(value) && value[idx] >= '0' && value[idx] <= '9' {
		idx++
	}
	if idx == 0 || idx >= len(value) {
		return ""
	}
	if value[idx] != '.' && value[idx] != ')' {
		return ""
	}
	return strings.TrimSpace(value[idx+1:])
}

func renderWorkflowPlan(def workflow.Definition, arguments, plan string, phases []workflow.Phase) string {
	var b strings.Builder
	b.WriteString("# Workflow Run Plan\n\n")
	if def.Name != "" {
		fmt.Fprintf(&b, "- Definition: %s\n", def.Name)
	}
	if strings.TrimSpace(arguments) != "" {
		fmt.Fprintf(&b, "- Arguments: %s\n", strings.TrimSpace(arguments))
	}
	if len(phases) > 0 {
		b.WriteString("\n## Phases\n\n")
		for i, phase := range phases {
			fmt.Fprintf(&b, "%d. %s (`%s`)\n", i+1, phase.Name, phase.ID)
		}
	}
	if strings.TrimSpace(plan) != "" {
		b.WriteString("\n## Plan Body\n\n")
		b.WriteString(strings.TrimSpace(plan))
		b.WriteString("\n")
	}
	return b.String()
}

func workflowNextSteps(status workflow.RunState) []string {
	switch status {
	case workflow.RunStateDraft:
		return []string{"Review or refine the plan, then transition the run to running before spawning agents."}
	case workflow.RunStateApprovalPending:
		return []string{"Ask for the required approval, then resume the workflow run when approved."}
	case workflow.RunStatePaused:
		return []string{
			"Inspect workflow_status for pause_reason, resume_hint, blocked phases, and Agent Runs.",
			"Resolve the blocker or rollback concern, then use workflow_control action=resume_run.",
			"If a specific Agent Run should be retried, use workflow_control action=retry_agent_run before spawning or messaging it again.",
		}
	default:
		return []string{
			"Spawn workflow agents with spawn_agent, setting agent_profile for durable named profiles.",
			"Require each workflow agent to call agent_report before treating its work as complete.",
			"Use await_agents when synthesis depends on agent output, then workflow_control action=record_await_results to bind results to the workflow run.",
			"Use workflow_control action=generate_final_report after all blocking phases and agent runs are complete.",
		}
	}
}

func reverseWorkflowRuns(runs []workflow.Run) []workflow.Run {
	out := make([]workflow.Run, len(runs))
	for i := range runs {
		out[i] = runs[len(runs)-1-i]
	}
	return out
}

func tailWorkflowEvents(events []workflow.Event, limit int) []workflow.Event {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if len(events) <= limit {
		return events
	}
	return events[len(events)-limit:]
}
