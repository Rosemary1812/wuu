package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	prompttext "github.com/blueberrycongee/wuu/internal/prompt"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
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
	items := make([]map[string]any, 0, len(t.env.Workflows))
	for _, wf := range t.env.Workflows {
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
			"orchestration assets, similar to skills, but they are used to create durable workflow run state. Markdown " +
			"workflow bodies may contain ${ARGUMENTS}, ${CLAUDE_WORKFLOW_DIR}, and ${CLAUDE_SESSION_ID} substitutions; " +
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
					"description": "Stable workflow name. A project WORKFLOW.md is written under .claude/workflows/<name>/ by default.",
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
// create_workflow
// ---------------------------------------------------------------------------

type RunWorkflowTool struct{ env *Env }

func NewRunWorkflowTool(env *Env) *RunWorkflowTool { return &RunWorkflowTool{env: env} }

func (t *RunWorkflowTool) Name() string            { return "run_workflow" }
func (t *RunWorkflowTool) IsReadOnly() bool        { return false }
func (t *RunWorkflowTool) IsConcurrencySafe() bool { return false }

func (t *RunWorkflowTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "run_workflow",
		Description: "Run a workflow through the script driver. The natural-language agent remains the entry point; " +
			"use this when a saved WORKFLOW.js definition or ad hoc script should own repeatable phase/spawn/await/synthesis control. " +
			"The script creates the same durable Workflow Run, Workflow Team, Agent Run, and final-report state as agent-managed workflows. " +
			"Inside the script, use phase(), spawn(), spawnBatch([...]), awaitAgents(), and synthesize(). Workers do shell/file work; " +
			"the script only coordinates and persists state. " +
			"Default caps are 1000 total worker spawns and 16 agents per spawnBatch/spawnAgents batch unless a lower definition or caller cap is set. " +
			"Use create_workflow when the agent should manage the run directly.",
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
	run, err := store.CreateRun(workflow.Run{
		ID:             runID,
		DefinitionName: def.Name,
		DefinitionPath: def.Path,
		Arguments:      strings.TrimSpace(args.Arguments),
		Driver:         workflow.RunDriverScript,
		Entrypoint:     workflow.RunEntrypointNaturalLanguageAgent,
		Status:         status,
		PauseReason:    pauseReason,
		ResumeHint:     resumeHint,
	})
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
				_, _ = runtime.Run(context.Background())
			}()
		} else {
			run, err = runtime.Run(ctx)
			if err != nil {
				return "", err
			}
		}
	}

	return mustJSON(map[string]any{
		"driver":             run.Driver,
		"entrypoint":         run.Entrypoint,
		"run_id":             run.ID,
		"status":             run.Status,
		"definition_name":    run.DefinitionName,
		"definition_path":    run.DefinitionPath,
		"script_path":        scriptPath,
		"background":         background && status == workflow.RunStateRunning,
		"profile_resolution": profileResolution,
		"next_steps":         runWorkflowNextSteps(run.Status),
		"workflow_status":    map[string]string{"run_id": run.ID},
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
		DefinitionName:   run.DefinitionName,
		DefinitionPath:   run.DefinitionPath,
		Arguments:        strings.TrimSpace(run.Arguments),
		Script:           script,
		MaxAgents:        maxAgents,
		MaxConcurrency:   maxConcurrency,
	})
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
			"This records the workflow state, phase plan, event log, and plan artifact. It does not automatically " +
			"spawn agents; after creating a running workflow, list_agent_profiles, record the Workflow Team with " +
			"workflow_control action=record_workflow_team, then use spawn_agent with agent_profile for reuse_profile/create_profile " +
			"members and without agent_profile for ephemeral workers. Require agent_report before completion, then inspect progress " +
			"with workflow_status. This is the direct agent-managed driver under the same natural-language workflow entry point.",
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
		Driver:         workflow.RunDriverAgentManaged,
		Entrypoint:     workflow.RunEntrypointNaturalLanguageAgent,
		Status:         status,
		Phases:         phases,
		PauseReason:    pauseReason,
		ResumeHint:     resumeHint,
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
		"driver":             run.Driver,
		"entrypoint":         run.Entrypoint,
		"run_id":             run.ID,
		"status":             run.Status,
		"definition_name":    run.DefinitionName,
		"definition_path":    run.DefinitionPath,
		"plan_path":          planPath,
		"phases":             run.Phases,
		"profile_resolution": profileResolution,
		"next_steps":         workflowNextSteps(status),
		"workflow_status":    map[string]string{"run_id": run.ID},
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
						"record_await_results",
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
				"team":      workflowTeamInputSchema("Workflow Team members chosen by the agent before spawning workflow workers. Use mode reuse_profile for existing durable profiles, create_profile for new recurring durable identities, and ephemeral for one-off memoryless workers."),
				"team_plan": workflowTeamInputSchema("Compatibility alias for team. Prefer team with action=record_workflow_team."),
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
				"mode":          map[string]any{"type": "string", "enum": []string{"reuse_profile", "create_profile", "ephemeral"}},
				"agent_profile": map[string]any{"type": "string", "description": "Profile name for reuse_profile/create_profile; omit for ephemeral."},
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
	ReportPath   string                    `json:"report_path"`
	ChangedFiles []string                  `json:"changed_files"`
	Artifacts    []string                  `json:"artifacts"`
	Paths        []string                  `json:"paths"`
	CheckpointID string                    `json:"checkpoint_id"`
	RetryCount   int                       `json:"retry_count"`
	MaxRetries   int                       `json:"max_retries"`
	RetryReason  string                    `json:"retry_reason"`
	AwaitResults []workflowAwaitResult     `json:"await_results"`
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
	TaskName     string `json:"task_name"`
	PhaseID      string `json:"phase_id"`
	Prompt       string `json:"prompt"`
	Reason       string `json:"reason"`
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
			go func() {
				_, _ = runtime.Run(context.Background())
			}()
			background = true
		}
		return mustJSON(map[string]any{
			"action":             action,
			"run":                run,
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
			"team_plan":     plan,
			"next_steps": []string{
				"Write each spawn_agent.message from the Base Agent Brief Contract, adding only the small context extension that applies.",
				"Add the Workflow Context Extension for workflow team members.",
				"Spawn reuse_profile and create_profile members with spawn_agent.agent_profile set to the recorded profile.",
				"Spawn ephemeral members without agent_profile.",
				"After spawning, bind each result back with workflow_control action=record_agent_run or record_await_results.",
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

	case "record_await_results":
		if len(args.AwaitResults) == 0 {
			return "", errors.New("workflow_control record_await_results requires await_results")
		}
		run, err := store.LoadRun(args.RunID)
		if err != nil {
			return "", err
		}
		teamPlan, err := store.LoadTeamPlan(args.RunID)
		if err != nil {
			return "", err
		}
		agentsToRecord := make([]workflow.AgentRun, 0, len(args.AwaitResults))
		for _, result := range args.AwaitResults {
			phaseID := strings.TrimSpace(args.PhaseID)
			if phaseID == "" {
				phaseID = workflowTeamPhaseForAwaitResult(teamPlan, result)
			}
			agent, err := workflowAgentRunFromAwaitResult(args.RunID, phaseID, result)
			if err != nil {
				return "", err
			}
			agentsToRecord = append(agentsToRecord, agent)
		}
		candidateIDs := make([]string, 0, len(agentsToRecord))
		for _, agent := range agentsToRecord {
			candidateIDs = append(candidateIDs, agent.ID)
		}
		if err := enforceWorkflowAgentCap(t.env, store, run, candidateIDs); err != nil {
			return "", err
		}

		agents := make([]workflow.AgentRun, 0, len(agentsToRecord))
		for _, agent := range agentsToRecord {
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

	used := make(map[string]int, len(inputs))
	members := make([]workflow.TeamMember, 0, len(inputs))
	for _, input := range inputs {
		role := strings.TrimSpace(input.Role)
		if role == "" {
			return workflow.TeamPlan{}, errors.New("workflow_control record_workflow_team requires every team member role")
		}
		mode, err := parseWorkflowTeamMemberMode(input.Mode, input.AgentProfile)
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

func workflowTeamPhaseForAwaitResult(plan workflow.TeamPlan, result workflowAwaitResult) string {
	candidates := map[string]struct{}{}
	for _, value := range []string{
		result.TaskName,
		result.AgentPath,
		workflowTaskNameFromAgentPath(result.AgentPath),
		result.AgentID,
	} {
		if value = strings.TrimSpace(value); value != "" {
			candidates[value] = struct{}{}
		}
	}
	for _, member := range plan.Members {
		phaseID := strings.TrimSpace(member.PhaseID)
		if phaseID == "" {
			continue
		}
		for _, value := range []string{member.TaskName, member.ID} {
			if _, ok := candidates[strings.TrimSpace(value)]; ok {
				return phaseID
			}
		}
	}
	return ""
}

func workflowTaskNameFromAgentPath(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	return strings.TrimSpace(parts[len(parts)-1])
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
			"runs":  reverseWorkflowRunsWithDriverDefaults(runs),
			"count": len(runs),
			"next_steps": []string{
				"Pass run_id to workflow_status to inspect a specific Workflow Run.",
				"Use create_workflow for agent-managed runs or run_workflow for script-driven definitions when starting new workflow work.",
			},
		})
	}

	run, err := store.LoadRun(runID)
	if err != nil {
		return "", err
	}
	run = workflowRunWithDriverDefaults(run)
	agents, err := store.ListAgentRuns(runID)
	if err != nil {
		return "", err
	}
	memoryCandidates, err := store.ListMemoryCandidates(runID)
	if err != nil {
		return "", err
	}
	fileCheckpoints, err := store.ListFileCheckpoints(runID)
	if err != nil {
		return "", err
	}
	teamPlan, err := store.LoadTeamPlan(runID)
	if err != nil {
		return "", err
	}
	arbitration := workflow.AnalyzeTeamArbitration(agents)
	result := map[string]any{
		"run":               run,
		"agent_runs":        agents,
		"workflow_team":     teamPlan,
		"team_plan":         teamPlan,
		"team_arbitration":  arbitration,
		"memory_candidates": memoryCandidates,
		"file_checkpoints":  fileCheckpoints,
		"next_steps":        workflowStatusNextSteps(run, agents, teamPlan, arbitration),
	}
	if run.DefinitionName != "" {
		if def, ok := t.env.FindWorkflow(run.DefinitionName); ok {
			profileResolution, err := resolveWorkflowProfilesForDefinition(def, false)
			if err != nil {
				return "", err
			}
			result["profile_resolution"] = profileResolution
		}
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

func resolveWorkflowProfilesForDefinition(def workflow.Definition, createMissing bool) ([]workflow.ProfileResolution, error) {
	if len(def.Profiles) == 0 {
		return nil, nil
	}
	wuuHome, err := statepath.Home("")
	if err != nil {
		return nil, err
	}
	return workflow.ResolveProfiles(workflow.ProfileResolutionOptions{
		WuuHome:       wuuHome,
		Definition:    def,
		CreateMissing: createMissing,
	})
}

func workflowProfileResolutionNames(resolutions []workflow.ProfileResolution) []string {
	names := make([]string, 0, len(resolutions))
	for _, resolution := range resolutions {
		if strings.TrimSpace(resolution.Name) != "" {
			names = append(names, resolution.Name)
		}
	}
	return names
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

func workflowDefinitionPath(rootDir, scope, name, kind string) (string, string, error) {
	dirName := slugWorkflowID(name)
	if dirName == "" {
		return "", "", fmt.Errorf("invalid workflow name %q", name)
	}
	filename := "WORKFLOW.md"
	if kind == workflow.DefinitionKindScript {
		filename = "WORKFLOW.js"
	}
	switch strings.TrimSpace(scope) {
	case "", "project":
		return filepath.Join(rootDir, ".claude", "workflows", dirName, filename), "project", nil
	case "user":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		return filepath.Join(home, ".claude", "workflows", dirName, filename), "user", nil
	default:
		return "", "", fmt.Errorf("invalid workflow scope %q", scope)
	}
}

func workflowDefinitionKind(def workflow.Definition) string {
	if def.Kind == workflow.DefinitionKindScript {
		return workflow.DefinitionKindScript
	}
	return workflow.DefinitionKindMarkdown
}

func workflowDefinitionNextSteps(def workflow.Definition) []string {
	if workflowDefinitionKind(def) == workflow.DefinitionKindScript {
		return []string{
			"Use run_workflow with definition_name to start this script-driven workflow.",
			"Let the script driver own phase/spawn/await/synthesis control; inspect workflow_status for durable run state.",
		}
	}
	return []string{
		"Use create_workflow with definition_name to start an agent-managed Workflow Run.",
		"After create_workflow, list Agent Profiles, record the Workflow Team, spawn agents, await results, and bind them back with workflow_control.",
	}
}

func normalizeWorkflowDefinitionKind(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", workflow.DefinitionKindMarkdown:
		return workflow.DefinitionKindMarkdown, nil
	case workflow.DefinitionKindScript:
		return workflow.DefinitionKindScript, nil
	default:
		return "", fmt.Errorf("invalid workflow kind %q", raw)
	}
}

func renderWorkflowDefinitionMarkdown(args saveWorkflowArgs, name, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", workflowFrontmatterScalar(name))
	if strings.TrimSpace(args.Description) != "" {
		fmt.Fprintf(&b, "description: %s\n", workflowFrontmatterScalar(args.Description))
	}
	if strings.TrimSpace(args.WhenToUse) != "" {
		fmt.Fprintf(&b, "when-to-use: %s\n", workflowFrontmatterScalar(args.WhenToUse))
	}
	if strings.TrimSpace(args.ArgumentHint) != "" {
		fmt.Fprintf(&b, "argument-hint: %s\n", workflowFrontmatterScalar(args.ArgumentHint))
	}
	if strings.TrimSpace(args.Version) != "" {
		fmt.Fprintf(&b, "version: %s\n", workflowFrontmatterScalar(args.Version))
	}
	if args.MaxAgents > 0 {
		fmt.Fprintf(&b, "max-agents: %d\n", args.MaxAgents)
	}
	if args.MaxConcurrency > 0 {
		fmt.Fprintf(&b, "max-concurrency: %d\n", args.MaxConcurrency)
	}
	if len(args.Profiles) > 0 {
		b.WriteString("profiles:\n")
		for _, profile := range args.Profiles {
			profileName := strings.TrimSpace(profile.Name)
			if profileName == "" {
				continue
			}
			fmt.Fprintf(&b, "  - name: %s\n", workflowFrontmatterScalar(profileName))
			if profile.Required {
				b.WriteString("    required: true\n")
			}
		}
	}
	if strings.TrimSpace(args.AllowProfileCreation) != "" {
		fmt.Fprintf(&b, "allow-profile-creation: %s\n", workflowFrontmatterScalar(args.AllowProfileCreation))
	}
	if strings.TrimSpace(args.MemoryPolicy) != "" {
		fmt.Fprintf(&b, "memory-policy: %s\n", workflowFrontmatterScalar(args.MemoryPolicy))
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")
	return b.String()
}

func renderWorkflowDefinitionScript(args saveWorkflowArgs, name, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// name: %s\n", workflowFrontmatterScalar(name))
	if strings.TrimSpace(args.Description) != "" {
		fmt.Fprintf(&b, "// description: %s\n", workflowFrontmatterScalar(args.Description))
	}
	if strings.TrimSpace(args.WhenToUse) != "" {
		fmt.Fprintf(&b, "// when-to-use: %s\n", workflowFrontmatterScalar(args.WhenToUse))
	}
	if strings.TrimSpace(args.ArgumentHint) != "" {
		fmt.Fprintf(&b, "// argument-hint: %s\n", workflowFrontmatterScalar(args.ArgumentHint))
	}
	if strings.TrimSpace(args.Version) != "" {
		fmt.Fprintf(&b, "// version: %s\n", workflowFrontmatterScalar(args.Version))
	}
	if args.MaxAgents > 0 {
		fmt.Fprintf(&b, "// max-agents: %d\n", args.MaxAgents)
	}
	if args.MaxConcurrency > 0 {
		fmt.Fprintf(&b, "// max-concurrency: %d\n", args.MaxConcurrency)
	}
	if len(args.Profiles) > 0 {
		names := make([]string, 0, len(args.Profiles))
		for _, profile := range args.Profiles {
			if strings.TrimSpace(profile.Name) != "" {
				names = append(names, strings.TrimSpace(profile.Name))
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(&b, "// profiles: %s\n", strings.Join(names, ", "))
		}
	}
	if strings.TrimSpace(args.AllowProfileCreation) != "" {
		fmt.Fprintf(&b, "// allow-profile-creation: %s\n", workflowFrontmatterScalar(args.AllowProfileCreation))
	}
	if strings.TrimSpace(args.MemoryPolicy) != "" {
		fmt.Fprintf(&b, "// memory-policy: %s\n", workflowFrontmatterScalar(args.MemoryPolicy))
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")
	return b.String()
}

func workflowFrontmatterScalar(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return `""`
	}
	return value
}

func reusableWorkflowBodyFromRun(run workflow.Run) string {
	var b strings.Builder
	b.WriteString("## Intent\n\n")
	if strings.TrimSpace(run.Arguments) != "" {
		fmt.Fprintf(&b, "Repeat the process used for: %s\n", strings.TrimSpace(run.Arguments))
	} else {
		b.WriteString("Repeat this workflow for similar future work.\n")
	}
	if len(run.Phases) > 0 {
		b.WriteString("\n## Phases\n\n")
		for i, phase := range run.Phases {
			name := strings.TrimSpace(phase.Name)
			if name == "" {
				name = phase.ID
			}
			fmt.Fprintf(&b, "%d. %s\n", i+1, name)
		}
	}
	b.WriteString("\n## Output\n\n")
	b.WriteString("The final report must include shipped behavior, changed files, verification, open risks, and memory candidates.\n")
	return b.String()
}

func upsertWorkflowDefinition(existing []workflow.Definition, def workflow.Definition) []workflow.Definition {
	for i := range existing {
		if existing[i].Name == def.Name {
			next := make([]workflow.Definition, len(existing))
			copy(next, existing)
			next[i] = def
			return next
		}
	}
	next := make([]workflow.Definition, 0, len(existing)+1)
	next = append(next, existing...)
	next = append(next, def)
	return next
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
			"Call list_agent_profiles, choose reuse_profile/create_profile/ephemeral members for this run, then record the Workflow Team with workflow_control action=record_workflow_team.",
			"Write each team member prompt from the Base Agent Brief Contract and add the Workflow Context Extension.",
			"Spawn reuse_profile/create_profile members with spawn_agent.agent_profile set to the recorded profile; spawn ephemeral members without agent_profile.",
			"Require each workflow agent to call agent_report before treating its work as complete.",
			"Create file checkpoints before risky direct edits that may need rollback.",
			"Use await_agents when synthesis depends on agent output, then workflow_control action=record_await_results to bind results to the workflow run.",
			"Use workflow_control action=generate_final_report after all blocking phases and agent runs are complete.",
		}
	}
}

func runWorkflowNextSteps(status workflow.RunState) []string {
	switch status {
	case workflow.RunStatePaused:
		return []string{
			"Inspect workflow_status for pause_reason, resume_hint, and profile_resolution.",
			"Create or choose the required Agent Profiles, then use workflow_control action=resume_run.",
		}
	case workflow.RunStateCompleted:
		return []string{
			"Inspect workflow_status for the final report, phases, Agent Runs, and event log.",
			"Use save_workflow with run_id if this ad hoc script should become a reusable workflow.",
		}
	case workflow.RunStateFailed:
		return []string{
			"Inspect workflow_status and the saved workflow.js artifact for the script failure.",
			"Fix the script or blocked agent output, then start a new workflow run.",
		}
	case workflow.RunStateCancelled:
		return []string{"Inspect workflow_status for cancellation context before starting a new workflow run."}
	default:
		return []string{
			"Use workflow_status to monitor phases, Agent Runs, and the event log while the script runs.",
			"Use workflow_control action=pause_run/resume_run if the script needs to pause between orchestration steps.",
			"Use awaitAgents in the script for fan-in and synthesize for the final report.",
		}
	}
}

func workflowStatusNextSteps(run workflow.Run, agents []workflow.AgentRun, teamPlan workflow.TeamPlan, arbitration workflow.TeamArbitration) []string {
	switch run.Status {
	case workflow.RunStateDraft, workflow.RunStateApprovalPending, workflow.RunStatePaused:
		return workflowNextSteps(run.Status)
	case workflow.RunStateCompleted:
		return []string{
			"Inspect final_report_path, agent reports, and events as the durable record of the completed workflow.",
			"Use save_workflow with run_id only if this run should become a reusable workflow definition.",
		}
	case workflow.RunStateFailed:
		return []string{
			"Inspect run error, failed Agent Runs, file checkpoints, and events before deciding whether to retry, rollback, or start a new run.",
		}
	case workflow.RunStateCancelled:
		return []string{
			"Treat this run as closed unless the user asks to start a replacement Workflow Run.",
		}
	}

	if strings.TrimSpace(run.ScriptPath) != "" {
		if arbitration.Status == "attention_required" {
			return append(copyWorkflowSteps(arbitration.NextActions), "After resolving script worker issues, inspect workflow_status again for final_report_path or remaining events.")
		}
		return []string{
			"The script driver owns phase/spawn/await/synthesis control; inspect events and final_report_path, and pause the run if it appears stuck.",
		}
	}

	if len(agents) > 0 && arbitration.Status == "attention_required" {
		return append(copyWorkflowSteps(arbitration.NextActions), "After resolving arbitration issues, update phase state and inspect workflow_status again before synthesis.")
	}
	if len(teamPlan.Members) == 0 {
		return []string{
			"Call list_agent_profiles, choose reuse_profile/create_profile/ephemeral members for this run, then record the Workflow Team with workflow_control action=record_workflow_team.",
		}
	}
	if len(agents) == 0 {
		return []string{
			"Spawn the recorded Workflow Team members with spawn_agent, setting agent_profile only for reuse_profile/create_profile members.",
			"After workers finish, bind outputs back with workflow_control action=record_await_results.",
		}
	}
	if err := validateWorkflowReadyToComplete(run, agents); err == nil {
		return []string{
			"Use workflow_control action=generate_final_report or write_final_report with complete_run=true.",
		}
	}
	return []string{
		"Continue runnable or active phases, await or record worker results, then inspect workflow_status again before synthesis.",
	}
}

func copyWorkflowSteps(steps []string) []string {
	out := make([]string, len(steps))
	copy(out, steps)
	return out
}

func reverseWorkflowRuns(runs []workflow.Run) []workflow.Run {
	out := make([]workflow.Run, len(runs))
	for i := range runs {
		out[i] = runs[len(runs)-1-i]
	}
	return out
}

func reverseWorkflowRunsWithDriverDefaults(runs []workflow.Run) []workflow.Run {
	out := reverseWorkflowRuns(runs)
	for i := range out {
		out[i] = workflowRunWithDriverDefaults(out[i])
	}
	return out
}

func workflowRunWithDriverDefaults(run workflow.Run) workflow.Run {
	if strings.TrimSpace(run.Driver) == "" {
		if strings.TrimSpace(run.ScriptPath) != "" {
			run.Driver = workflow.RunDriverScript
		} else {
			run.Driver = workflow.RunDriverAgentManaged
		}
	}
	if strings.TrimSpace(run.Entrypoint) == "" {
		run.Entrypoint = workflow.RunEntrypointNaturalLanguageAgent
	}
	return run
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
