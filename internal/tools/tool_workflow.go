package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
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
			"to choose a saved workflow before creating workflow run state.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *ListWorkflowsTool) Execute(_ context.Context, _ string) (string, error) {
	visible := t.env.VisibleWorkflows()
	items := make([]map[string]any, 0, len(visible))
	for _, wf := range visible {
		item := map[string]any{
			"name":                   wf.Name,
			"description":            wf.Description,
			"when_to_use":            wf.WhenToUse,
			"kind":                   workflowDefinitionKind(wf),
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
			"next_steps":             workflowDefinitionNextSteps(wf),
		}
		items = append(items, item)
	}
	return mustJSON(map[string]any{
		"action":    "list_workflows",
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
			"instructions for creating durable workflow run state. Markdown " +
			"workflow bodies may contain ${ARGUMENTS}, ${WUU_WORKFLOW_DIR}, and ${WUU_SESSION_ID} substitutions. " +
			"Legacy ${CLAUDE_WORKFLOW_DIR} and ${CLAUDE_SESSION_ID} aliases are also supported; " +
			"script workflows are returned as raw JavaScript and receive arguments through the args global when run.",
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
	body := wf.Content
	suggestedPhaseNames := []string(nil)
	if workflowDefinitionKind(wf) != workflow.DefinitionKindScript {
		body = t.env.ProcessWorkflowBody(wf, args.Arguments)
		suggestedPhaseNames = extractWorkflowPhaseNames(body)
	}
	return mustJSON(map[string]any{
		"action":                 "load_workflow",
		"name":                   wf.Name,
		"description":            wf.Description,
		"when_to_use":            wf.WhenToUse,
		"kind":                   workflowDefinitionKind(wf),
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
		"suggested_phase_names":  suggestedPhaseNames,
		"content":                body,
		"next_steps":             workflowDefinitionNextSteps(wf),
	})
}

// ---------------------------------------------------------------------------
// save_workflow
// ---------------------------------------------------------------------------

type SaveWorkflowTool struct{ env *Env }

func NewSaveWorkflowTool(env *Env) *SaveWorkflowTool { return &SaveWorkflowTool{env: env} }

func (t *SaveWorkflowTool) Name() string            { return "save_workflow" }
func (t *SaveWorkflowTool) IsReadOnly() bool        { return false }
func (t *SaveWorkflowTool) IsConcurrencySafe() bool { return false }

func (t *SaveWorkflowTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "save_workflow",
		Description: "Save an ad hoc or reusable workflow definition to a portable WORKFLOW.md or WORKFLOW.js file. " +
			"Use this when the user wants to reuse, share, or schedule a workflow that was created during the current session.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Stable workflow name. A project WORKFLOW.md or WORKFLOW.js is written under .wuu/workflows/<name>/ by default.",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "Short workflow description for discovery.",
				},
				"when_to_use": map[string]any{
					"type":        "string",
					"description": "When the workflow should be selected.",
				},
				"argument_hint": map[string]any{
					"type":        "string",
					"description": "Optional slash-command style argument hint.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Markdown workflow body or JavaScript workflow script. Required unless run_id is provided.",
				},
				"kind": map[string]any{
					"type":        "string",
					"enum":        []string{"markdown", "script"},
					"description": "Definition kind. Defaults to markdown. Use script to save a script-driven workflow.",
				},
				"run_id": map[string]any{
					"type":        "string",
					"description": "Optional Workflow Run id to turn into a reusable definition when content is omitted.",
				},
				"scope": map[string]any{
					"type":        "string",
					"enum":        []string{"project", "user"},
					"description": "Where to save the workflow. Defaults to project.",
				},
				"overwrite": map[string]any{
					"type":        "boolean",
					"description": "Whether to replace an existing WORKFLOW.md. Defaults false.",
				},
				"version": map[string]any{"type": "string"},
				"max_agents": map[string]any{
					"type":        "integer",
					"description": "Optional workflow agent cap.",
				},
				"max_concurrency": map[string]any{
					"type":        "integer",
					"description": "Optional workflow concurrency cap.",
				},
				"profiles": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name":     map[string]any{"type": "string"},
							"required": map[string]any{"type": "boolean"},
						},
						"required": []string{"name"},
					},
				},
				"allow_profile_creation": map[string]any{
					"type":        "string",
					"description": "Profile creation policy, for example ask or auto.",
				},
				"memory_policy": map[string]any{
					"type":        "string",
					"description": "Workflow memory policy, for example report-candidates-only.",
				},
			},
			"required": []string{"name"},
		},
	}
}

type saveWorkflowArgs struct {
	Name                 string                `json:"name"`
	Description          string                `json:"description"`
	WhenToUse            string                `json:"when_to_use"`
	ArgumentHint         string                `json:"argument_hint"`
	Content              string                `json:"content"`
	Kind                 string                `json:"kind"`
	RunID                string                `json:"run_id"`
	Scope                string                `json:"scope"`
	Overwrite            bool                  `json:"overwrite"`
	Version              string                `json:"version"`
	MaxAgents            int                   `json:"max_agents"`
	MaxConcurrency       int                   `json:"max_concurrency"`
	Profiles             []workflow.ProfileRef `json:"profiles"`
	AllowProfileCreation string                `json:"allow_profile_creation"`
	MemoryPolicy         string                `json:"memory_policy"`
}

func (t *SaveWorkflowTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args saveWorkflowArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	name := strings.TrimSpace(strings.TrimPrefix(args.Name, "/"))
	if name == "" {
		return "", errors.New("save_workflow requires name")
	}
	kind, err := normalizeWorkflowDefinitionKind(args.Kind)
	if err != nil {
		return "", err
	}
	body := strings.TrimSpace(args.Content)
	if body == "" && strings.TrimSpace(args.RunID) != "" {
		store, err := t.env.WorkflowStore()
		if err != nil {
			return "", fmt.Errorf("resolve workflow store: %w", err)
		}
		run, err := store.LoadRun(args.RunID)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(run.ScriptPath) != "" {
			data, readErr := os.ReadFile(run.ScriptPath)
			if readErr == nil {
				body = strings.TrimSpace(string(data))
				kind = workflow.DefinitionKindScript
			}
		}
		if body == "" {
			body = reusableWorkflowBodyFromRun(run)
		}
	}
	if body == "" {
		return "", errors.New("save_workflow requires content or run_id")
	}
	if activeSurfaceLacksTerminalExecution(t.env) && mentionsTerminalOnlyPath(body) {
		return "", errors.New("save_workflow cannot save terminal-dependent workflow content under the active model surface")
	}
	path, source, err := workflowDefinitionPath(t.env.RootDir, args.Scope, name, kind)
	if err != nil {
		return "", err
	}
	if !args.Overwrite {
		if _, statErr := os.Stat(path); statErr == nil {
			return "", fmt.Errorf("workflow definition already exists at %s; set overwrite=true to replace it", path)
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		}
	}
	content := renderWorkflowDefinitionMarkdown(args, name, body)
	if kind == workflow.DefinitionKindScript {
		content = renderWorkflowDefinitionScript(args, name, body)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	def, err := workflow.LoadDefinitionFile(path, source)
	if err != nil {
		return "", err
	}
	t.env.Workflows = upsertWorkflowDefinition(t.env.Workflows, def)
	return mustJSON(map[string]any{
		"action":   "save_workflow",
		"name":     def.Name,
		"path":     def.Path,
		"source":   def.Source,
		"kind":     workflowDefinitionKind(def),
		"workflow": def,
		"next_steps": []string{
			"Use list_workflows or load_workflow to inspect the saved definition.",
			"Use schedule_cron with workflow_name when this saved workflow should run on a schedule.",
		},
	})
}

// ---------------------------------------------------------------------------
// start_workflow
// ---------------------------------------------------------------------------

type StartWorkflowTool struct{ env *Env }

func NewStartWorkflowTool(env *Env) *StartWorkflowTool { return &StartWorkflowTool{env: env} }

func (t *StartWorkflowTool) Name() string            { return "start_workflow" }
func (t *StartWorkflowTool) IsReadOnly() bool        { return false }
func (t *StartWorkflowTool) IsConcurrencySafe() bool { return false }

func (t *StartWorkflowTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "start_workflow",
		Description: "Start a workflow run. Prefer this over choosing run_workflow/create_workflow yourself: driver=auto sends saved script definitions or ad hoc scripts to the script path, and markdown definitions or ad hoc plans to the agent-managed path. " +
			"The returned driver field tells you whether the script or the agent controls phases, worker spawns, awaits, and synthesis. " +
			"Omit goal_id and goal_dir for a self-contained workflow; start_workflow creates a Goal binding for that run. Pass goal_id and goal_dir only when binding the run to a broader user-visible Goal. " +
			"Use run_workflow or create_workflow directly only when the task explicitly requires that path.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"definition_name": map[string]any{
					"type":        "string",
					"description": "Optional saved workflow definition name.",
				},
				"arguments": map[string]any{
					"type":        "string",
					"description": "Arguments for the saved workflow or script.",
				},
				"driver": map[string]any{
					"type":        "string",
					"enum":        []string{"auto", "script", "agent_managed"},
					"description": "Driver override. Defaults to auto, which uses the saved workflow kind or script field.",
				},
				"run_id": map[string]any{
					"type":        "string",
					"description": "Optional caller-provided run id. Omit for an auto-generated id.",
				},
				"goal_id": map[string]any{
					"type":        "string",
					"description": "Optional existing Goal id from start_goal. Pass with goal_dir to bind this workflow to the broader user-visible Goal.",
				},
				"goal_dir": map[string]any{
					"type":        "string",
					"description": "Optional existing Goal directory from start_goal.",
				},
				"script": map[string]any{
					"type":        "string",
					"description": "Ad hoc WORKFLOW.js script. When present, auto uses the script driver.",
				},
				"background": map[string]any{
					"type":        "boolean",
					"description": "Script driver only. Defaults to true; set false to run synchronously.",
				},
				"max_agents": map[string]any{
					"type":        "integer",
					"description": "Script driver cap for total worker spawns.",
				},
				"max_concurrency": map[string]any{
					"type":        "integer",
					"description": "Script driver cap for agents in one spawnBatch/spawnAgents call.",
				},
				"plan": map[string]any{
					"type":        "string",
					"description": "Agent-managed driver plan. If omitted with definition_name, the workflow definition body is used.",
				},
				"phases": map[string]any{
					"type":        "array",
					"description": "Agent-managed driver phase plan.",
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
					"description": "Agent-managed driver initial run state. Defaults to running.",
				},
			},
		},
	}
}

func (t *StartWorkflowTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		DefinitionName string `json:"definition_name"`
		Driver         string `json:"driver"`
		Script         string `json:"script"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	driver := strings.TrimSpace(args.Driver)
	if driver == "" {
		driver = "auto"
	}
	switch driver {
	case "auto":
		if strings.TrimSpace(args.Script) != "" {
			return NewRunWorkflowTool(t.env).Execute(ctx, argsJSON)
		}
		if strings.TrimSpace(args.DefinitionName) != "" {
			found, ok := t.env.FindWorkflow(args.DefinitionName)
			if !ok {
				return "", fmt.Errorf("workflow %q not found. available: %s", args.DefinitionName, strings.Join(t.env.WorkflowNames(), ", "))
			}
			if workflowDefinitionKind(found) == workflow.DefinitionKindScript {
				return NewRunWorkflowTool(t.env).Execute(ctx, argsJSON)
			}
		}
		return NewCreateWorkflowTool(t.env).Execute(ctx, argsJSON)
	case "script":
		return NewRunWorkflowTool(t.env).Execute(ctx, argsJSON)
	case "agent_managed":
		return NewCreateWorkflowTool(t.env).Execute(ctx, argsJSON)
	default:
		return "", fmt.Errorf("start_workflow driver must be auto, script, or agent_managed")
	}
}

// ---------------------------------------------------------------------------
// run_workflow
// ---------------------------------------------------------------------------

type RunWorkflowTool struct{ env *Env }

func NewRunWorkflowTool(env *Env) *RunWorkflowTool { return &RunWorkflowTool{env: env} }

func (t *RunWorkflowTool) Name() string            { return "run_workflow" }
func (t *RunWorkflowTool) IsReadOnly() bool        { return false }
func (t *RunWorkflowTool) IsConcurrencySafe() bool { return false }

func (t *RunWorkflowTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "run_workflow",
		Description: "Run a workflow through the script path. " +
			"Prefer start_workflow driver=auto for new workflow work and use this only when the task explicitly requires a saved WORKFLOW.js definition or ad hoc script to control the run. " +
			"Use this when a saved WORKFLOW.js definition or ad hoc script should own repeatable phase/spawn/await/synthesis control. " +
			"The script creates the same durable Workflow Run, Workflow Team, Agent Run, and final-report state as agent-managed workflows. " +
			"Inside the script, use phase(), spawn(), spawnBatch([...]), awaitAgents(), and synthesize(). Workers use their compiled tool surface for task work; " +
			"the script only coordinates and persists state. " +
			"Default caps are 1000 total worker spawns and 16 agents per spawnBatch/spawnAgents batch unless a lower definition or caller cap is set.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"definition_name": map[string]any{
					"type":        "string",
					"description": "Optional saved script workflow name. The definition must be kind=script unless script is also supplied.",
				},
				"arguments": map[string]any{
					"type":        "string",
					"description": "Arguments exposed to the script as args. JSON strings become objects; other strings stay strings.",
				},
				"script": map[string]any{
					"type":        "string",
					"description": "Ad hoc JavaScript workflow script. Omit when running a saved .js workflow definition.",
				},
				"run_id": map[string]any{
					"type":        "string",
					"description": "Optional caller-provided run id. Omit for an auto-generated id.",
				},
				"goal_id": map[string]any{
					"type":        "string",
					"description": "Optional existing Goal id from start_goal. Pass with goal_dir to bind this workflow to the broader user-visible Goal.",
				},
				"goal_dir": map[string]any{
					"type":        "string",
					"description": "Optional existing Goal directory from start_goal.",
				},
				"background": map[string]any{
					"type":        "boolean",
					"description": "Whether to start the script in the background. Defaults true.",
				},
				"max_agents": map[string]any{
					"type":        "integer",
					"description": "Optional override for this run's total agent cap.",
				},
				"max_concurrency": map[string]any{
					"type":        "integer",
					"description": "Optional override for this run's spawnBatch/spawnAgents batch size cap.",
				},
			},
		},
	}
}

func (t *RunWorkflowTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		DefinitionName string `json:"definition_name"`
		Arguments      string `json:"arguments"`
		Script         string `json:"script"`
		RunID          string `json:"run_id"`
		GoalID         string `json:"goal_id"`
		GoalDir        string `json:"goal_dir"`
		Background     *bool  `json:"background"`
		MaxAgents      int    `json:"max_agents"`
		MaxConcurrency int    `json:"max_concurrency"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}

	var def workflow.Definition
	if strings.TrimSpace(args.DefinitionName) != "" {
		found, ok := t.env.FindWorkflow(args.DefinitionName)
		if !ok {
			return "", fmt.Errorf("workflow %q not found. available: %s", args.DefinitionName, strings.Join(t.env.WorkflowNames(), ", "))
		}
		def = found
	}

	script := args.Script
	if strings.TrimSpace(script) == "" {
		if def.Name == "" {
			return "", errors.New("run_workflow requires definition_name or script")
		}
		if workflowDefinitionKind(def) != workflow.DefinitionKindScript {
			return "", fmt.Errorf("workflow %q is kind=%s; run_workflow requires a script workflow", def.Name, workflowDefinitionKind(def))
		}
		script = def.Content
	}
	if strings.TrimSpace(script) == "" {
		return "", errors.New("run_workflow requires a non-empty script")
	}
	if activeSurfaceLacksTerminalExecution(t.env) && mentionsTerminalOnlyPath(script) {
		return "", errors.New("run_workflow cannot run terminal-dependent script content under the active model surface")
	}

	status := workflow.RunStateRunning
	profileResolution := []workflow.ProfileResolution(nil)
	pauseReason := ""
	resumeHint := ""
	var err error
	if def.Name != "" {
		profileResolution, err = resolveWorkflowProfilesForDefinition(def, workflow.AutoCreateProfiles(def.AllowProfileCreation))
		if err != nil {
			return "", err
		}
		missingRequired := workflow.MissingRequiredProfiles(profileResolution)
		if len(missingRequired) > 0 {
			status = workflow.RunStatePaused
			pauseReason = "Missing required Agent Profiles: " + strings.Join(workflowProfileResolutionNames(missingRequired), ", ")
			resumeHint = "Create or choose the required Agent Profiles, then resume the workflow run."
		}
	}

	runID := strings.TrimSpace(args.RunID)
	if runID == "" {
		runID = "workflow-" + session.NewID()
	}
	store, err := t.env.WorkflowStore()
	if err != nil {
		return "", fmt.Errorf("resolve workflow store: %w", err)
	}
	runInput, err := bindWorkflowRunToGoal(t.env, workflow.Run{
		ID:             runID,
		DefinitionName: def.Name,
		DefinitionPath: def.Path,
		Arguments:      strings.TrimSpace(args.Arguments),
		Driver:         workflow.RunDriverScript,
		Entrypoint:     workflow.RunEntrypointNaturalLanguageAgent,
		Status:         status,
		PauseReason:    pauseReason,
		ResumeHint:     resumeHint,
	}, args.GoalID, args.GoalDir)
	if err != nil {
		return "", err
	}
	run, err := store.CreateRun(runInput)
	if err != nil {
		return "", err
	}
	run, goalState, err := attachWorkflowGoal(t.env, store, run)
	if err != nil {
		return "", err
	}
	scriptPath, err := store.WriteScript(run.ID, script)
	if err != nil {
		return "", err
	}
	run.ScriptPath = scriptPath

	background := true
	if args.Background != nil {
		background = *args.Background
	}
	maxAgents := args.MaxAgents
	if maxAgents <= 0 {
		maxAgents = def.MaxAgents
	}
	maxConcurrency := args.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = def.MaxConcurrency
	}
	runtime := newWorkflowScriptRuntime(t.env, store, run, script, maxAgents, maxConcurrency)

	if status == workflow.RunStateRunning {
		if background {
			go func() {
				finished, _ := runtime.Run(context.Background())
				_, _ = syncWorkflowGoalStatus(finished)
			}()
		} else {
			run, err = runtime.Run(ctx)
			if _, syncErr := syncWorkflowGoalStatus(run); syncErr != nil {
				return "", syncErr
			}
			if err != nil {
				return "", err
			}
		}
	}

	return mustJSON(map[string]any{
		"action":             "run_workflow",
		"driver":             run.Driver,
		"entrypoint":         run.Entrypoint,
		"run_id":             run.ID,
		"status":             run.Status,
		"goal_id":            run.GoalID,
		"goal_dir":           run.GoalDir,
		"definition_name":    run.DefinitionName,
		"definition_path":    run.DefinitionPath,
		"script_path":        scriptPath,
		"background":         background && status == workflow.RunStateRunning,
		"profile_resolution": profileResolution,
		"next_steps":         runWorkflowNextSteps(run.Status),
		"workflow_status":    map[string]string{"run_id": run.ID},
		"goal_status":        map[string]string{"goal_id": goalState.ID, "state_path": filepath.Join(run.GoalDir, "state.json")},
	})
}

func newWorkflowScriptRuntime(env *Env, store *workflow.Store, run workflow.Run, script string, maxAgents, maxConcurrency int) *workflow.ScriptRuntime {
	return workflow.NewScriptRuntime(workflow.ScriptRuntimeOptions{
		Store:            store,
		AgentControl:     env.AgentControl,
		RootDir:          env.RootDir,
		CurrentAgentID:   env.AgentID,
		CurrentAgentPath: env.AgentPath,
		RunID:            run.ID,
		GoalID:           run.GoalID,
		GoalDir:          run.GoalDir,
		DefinitionName:   run.DefinitionName,
		DefinitionPath:   run.DefinitionPath,
		Arguments:        strings.TrimSpace(run.Arguments),
		Script:           script,
		MaxAgents:        maxAgents,
		MaxConcurrency:   maxConcurrency,
	})
}

func bindWorkflowRunToGoal(env *Env, run workflow.Run, goalID, goalDir string) (workflow.Run, error) {
	goalID = strings.TrimSpace(goalID)
	goalDir = strings.TrimSpace(goalDir)
	if goalID == "" && goalDir == "" {
		return run, nil
	}
	store, state, err := env.ResolveGoalStore(goalID, goalDir)
	if err != nil {
		return workflow.Run{}, err
	}
	run.GoalID = state.ID
	run.GoalDir = store.Dir()
	return run, nil
}

func attachWorkflowGoal(env *Env, store *workflow.Store, run workflow.Run) (workflow.Run, goalrunner.State, error) {
	if strings.TrimSpace(run.GoalID) != "" && strings.TrimSpace(run.GoalDir) != "" {
		goalStore := goalrunner.NewStore(run.GoalDir)
		state, err := goalStore.LoadState()
		if err != nil {
			return run, goalrunner.State{}, err
		}
		run.GoalID = state.ID
		run.GoalDir = goalStore.Dir()
		if err := store.SaveRun(run); err != nil {
			return run, goalrunner.State{}, err
		}
		state, err = initializeWorkflowGoalStatus(goalStore, state, run)
		if err != nil {
			return run, goalrunner.State{}, err
		}
		return run, state, nil
	}
	goalStore, err := env.WorkflowGoalStore(run.ID)
	if err != nil {
		return run, goalrunner.State{}, err
	}
	state, err := goalStore.LoadState()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return run, goalrunner.State{}, err
		}
		state, err = goalStore.Init(goalrunner.Spec{
			ID:   run.ID,
			Goal: workflowGoalTitle(run),
			Task: strings.TrimSpace(run.Arguments),
			Trigger: goalrunner.Trigger{
				Type:   "workflow",
				Source: "start_workflow",
				Payload: map[string]string{
					"workflow_run_id": run.ID,
					"definition_name": run.DefinitionName,
					"driver":          run.Driver,
				},
			},
			AssignedAgent: "workflow",
			EscalationPolicy: goalrunner.EscalationPolicy{
				EscalateOnFailure: true,
			},
		})
		if err != nil {
			return run, goalrunner.State{}, err
		}
		state, err = initializeWorkflowGoalStatus(goalStore, state, run)
		if err != nil {
			return run, goalrunner.State{}, err
		}
	}
	run.GoalID = state.ID
	run.GoalDir = goalStore.Dir()
	if err := store.SaveRun(run); err != nil {
		return run, goalrunner.State{}, err
	}
	return run, state, nil
}

func initializeWorkflowGoalStatus(store *goalrunner.Store, state goalrunner.State, run workflow.Run) (goalrunner.State, error) {
	message := "workflow run " + run.ID + " created"
	switch run.Status {
	case workflow.RunStateRunning:
		step := goalrunner.StepPlan
		if run.Driver == workflow.RunDriverScript {
			step = goalrunner.StepExecution
		}
		return store.SetStatus(goalrunner.StatusRunning, step, message)
	case workflow.RunStatePaused:
		return store.AddFailure(goalrunner.Failure{
			Step:     goalrunner.StepApproval,
			Kind:     "workflow_paused",
			Source:   "workflow",
			SourceID: run.ID,
			Message:  firstWorkflowGoalText(run.PauseReason, run.ResumeHint, "workflow run is paused"),
		})
	case workflow.RunStateFailed:
		return store.AddFailure(goalrunner.Failure{
			Step:     goalrunner.StepExecution,
			Kind:     "workflow_failed",
			Source:   "workflow",
			SourceID: run.ID,
			Message:  firstWorkflowGoalText(run.Error, "workflow run failed"),
		})
	case workflow.RunStateCompleted:
		return store.SetStatus(goalrunner.StatusCompleted, goalrunner.StepSummary, message)
	case workflow.RunStateCancelled:
		return store.SetStatus(goalrunner.StatusCancelled, state.CurrentStep, message)
	default:
		return state, nil
	}
}

func syncWorkflowGoalStatus(run workflow.Run) (goalrunner.State, error) {
	if strings.TrimSpace(run.GoalDir) == "" {
		return goalrunner.State{}, nil
	}
	store := goalrunner.NewStore(run.GoalDir)
	state, err := store.LoadState()
	if err != nil {
		return goalrunner.State{}, err
	}
	return initializeWorkflowGoalStatus(store, state, run)
}

func workflowGoalTitle(run workflow.Run) string {
	name := strings.TrimSpace(run.DefinitionName)
	if name == "" {
		name = strings.TrimSpace(run.ID)
	}
	if name == "" {
		name = "workflow run"
	}
	if args := strings.TrimSpace(run.Arguments); args != "" {
		return "Workflow " + name + ": " + args
	}
	return "Workflow " + name
}

func firstWorkflowGoalText(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func shouldStartInitialPausedScriptRun(run workflow.Run) bool {
	return run.Status == workflow.RunStatePaused &&
		run.StartedAt.IsZero() &&
		strings.TrimSpace(run.ScriptPath) != ""
}

func unresolvedRequiredProfilesForRun(env *Env, run workflow.Run) ([]workflow.ProfileResolution, error) {
	if env == nil || strings.TrimSpace(run.DefinitionName) == "" {
		return nil, nil
	}
	def, ok := env.FindWorkflow(run.DefinitionName)
	if !ok {
		return nil, nil
	}
	return resolveWorkflowProfilesForDefinition(def, false)
}

func workflowScriptLimitsForRun(env *Env, run workflow.Run) (maxAgents, maxConcurrency int) {
	if env == nil || strings.TrimSpace(run.DefinitionName) == "" {
		return 0, 0
	}
	if def, ok := env.FindWorkflow(run.DefinitionName); ok {
		return def.MaxAgents, def.MaxConcurrency
	}
	return 0, 0
}

type CreateWorkflowTool struct{ env *Env }

func NewCreateWorkflowTool(env *Env) *CreateWorkflowTool { return &CreateWorkflowTool{env: env} }

func (t *CreateWorkflowTool) Name() string            { return "create_workflow" }
func (t *CreateWorkflowTool) IsReadOnly() bool        { return false }
func (t *CreateWorkflowTool) IsConcurrencySafe() bool { return true }

func (t *CreateWorkflowTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "create_workflow",
		Description: "Create an agent-managed Workflow Run from a reusable workflow definition or an ad hoc plan. " +
			"Prefer start_workflow driver=auto for new workflow work and use this only when the task explicitly requires an agent-managed run. " +
			"This records the workflow state, phase plan, event log, and plan artifact. It does not automatically " +
			"spawn agents; after creating a running workflow, list_agent_profiles, record the Workflow Team with " +
			"workflow_control action=record_workflow_team, then use spawn_agent with agent_profile for reuse_profile/create_profile " +
			"members and without agent_profile for ephemeral workers. Require agent_report before completion, then inspect progress " +
			"with workflow_status.",
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
				"goal_id": map[string]any{
					"type":        "string",
					"description": "Optional existing Goal id from start_goal. Pass with goal_dir to bind this workflow to the broader user-visible Goal.",
				},
				"goal_dir": map[string]any{
					"type":        "string",
					"description": "Optional existing Goal directory from start_goal.",
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
		GoalID         string               `json:"goal_id"`
		GoalDir        string               `json:"goal_dir"`
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
		if workflowDefinitionKind(found) == workflow.DefinitionKindScript {
			return "", fmt.Errorf("workflow %q is kind=script; use run_workflow so the script driver owns phase/spawn/await/synthesis control", found.Name)
		}
		def = found
		body = t.env.ProcessWorkflowBody(def, args.Arguments)
	}

	var profileResolution []workflow.ProfileResolution
	pauseReason := ""
	resumeHint := ""
	if def.Name != "" {
		profileResolution, err = resolveWorkflowProfilesForDefinition(def, workflow.AutoCreateProfiles(def.AllowProfileCreation))
		if err != nil {
			return "", err
		}
		missingRequired := workflow.MissingRequiredProfiles(profileResolution)
		if status == workflow.RunStateRunning && len(missingRequired) > 0 {
			status = workflow.RunStatePaused
			pauseReason = "Missing required Agent Profiles: " + strings.Join(workflowProfileResolutionNames(missingRequired), ", ")
			resumeHint = "Create or choose the required Agent Profiles, then resume the workflow run."
		}
	}

	plan := strings.TrimSpace(args.Plan)
	if plan == "" {
		plan = strings.TrimSpace(body)
	}
	if activeSurfaceLacksTerminalExecution(t.env) && mentionsTerminalOnlyPath(plan) {
		return "", errors.New("create_workflow cannot create terminal-dependent workflow plans under the active model surface")
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
	runInput, err := bindWorkflowRunToGoal(t.env, workflow.Run{
		ID:             runID,
		DefinitionName: def.Name,
		DefinitionPath: def.Path,
		Arguments:      strings.TrimSpace(args.Arguments),
		Driver:         workflow.RunDriverAgentManaged,
		Entrypoint:     workflow.RunEntrypointNaturalLanguageAgent,
		Status:         status,
		Phases:         phases,
		PauseReason:    pauseReason,
		ResumeHint:     resumeHint,
	}, args.GoalID, args.GoalDir)
	if err != nil {
		return "", err
	}
	run, err := store.CreateRun(runInput)
	if err != nil {
		return "", err
	}
	run, goalState, err := attachWorkflowGoal(t.env, store, run)
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
		"action":             "create_workflow",
		"driver":             run.Driver,
		"entrypoint":         run.Entrypoint,
		"run_id":             run.ID,
		"status":             run.Status,
		"goal_id":            run.GoalID,
		"goal_dir":           run.GoalDir,
		"definition_name":    run.DefinitionName,
		"definition_path":    run.DefinitionPath,
		"plan_path":          planPath,
		"phases":             run.Phases,
		"profile_resolution": profileResolution,
		"next_steps":         workflowNextSteps(status),
		"workflow_status":    map[string]string{"run_id": run.ID},
		"goal_status":        map[string]string{"goal_id": goalState.ID, "state_path": filepath.Join(run.GoalDir, "state.json")},
	})
}
