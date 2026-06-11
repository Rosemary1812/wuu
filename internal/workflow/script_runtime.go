package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/dop251/goja"
)

const (
	DefaultScriptMaxAgents      = 1000
	DefaultScriptMaxConcurrency = 16
	scriptPausePollInterval     = 100 * time.Millisecond
)

type ScriptRuntimeOptions struct {
	Store            *Store
	AgentControl     *agentcontrol.AgentControl
	RootDir          string
	CurrentAgentID   string
	CurrentAgentPath string
	RunID            string
	DefinitionName   string
	DefinitionPath   string
	Arguments        string
	Script           string
	MaxAgents        int
	MaxConcurrency   int
}

type ScriptRuntime struct {
	opts            ScriptRuntimeOptions
	currentPhaseID  string
	spawnSeq        int
	spawnedAgentIDs []string
	maxAgents       int
	maxConcurrency  int
	completedByUser bool
}

type ScriptSpawnSpec struct {
	TaskName        string `json:"name"`
	Description     string `json:"description"`
	Prompt          string `json:"prompt"`
	SubagentType    string `json:"subagentType"`
	AgentProfile    string `json:"agentProfile"`
	Isolation       string `json:"isolation"`
	RunInBackground bool   `json:"runInBackground"`
	TimeoutMS       int64  `json:"timeoutMs"`
}

type ScriptSpawnResult struct {
	AgentID      string `json:"agentId"`
	TaskName     string `json:"taskName,omitempty"`
	AgentProfile string `json:"agentProfile,omitempty"`
	AgentPath    string `json:"agentPath,omitempty"`
	Status       string `json:"status"`
	Isolation    string `json:"isolation,omitempty"`
	WorktreePath string `json:"worktreePath,omitempty"`
	Result       string `json:"result,omitempty"`
	Error        string `json:"error,omitempty"`
	DurationMS   int64  `json:"durationMs,omitempty"`
}

type ScriptAwaitArgs struct {
	Targets   []string `json:"targets"`
	TimeoutMS int64    `json:"timeoutMs"`
}

func NewScriptRuntime(opts ScriptRuntimeOptions) *ScriptRuntime {
	maxAgents := opts.MaxAgents
	if maxAgents <= 0 {
		maxAgents = DefaultScriptMaxAgents
	}
	maxConcurrency := opts.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = DefaultScriptMaxConcurrency
	}
	return &ScriptRuntime{
		opts:           opts,
		maxAgents:      maxAgents,
		maxConcurrency: maxConcurrency,
	}
}

func (r *ScriptRuntime) Run(ctx context.Context) (Run, error) {
	if r == nil {
		return Run{}, errors.New("workflow script runtime is nil")
	}
	if r.opts.Store == nil {
		return Run{}, errors.New("workflow store is required")
	}
	if strings.TrimSpace(r.opts.RunID) == "" {
		return Run{}, errors.New("workflow run id is required")
	}
	if strings.TrimSpace(r.opts.Script) == "" {
		return Run{}, errors.New("workflow script is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	vm := goja.New()
	r.installGlobals(ctx, vm)
	_, err := vm.RunString(r.opts.Script)
	if err != nil {
		message := scriptErrorMessage(err)
		_ = r.failRun(message)
		run, loadErr := r.opts.Store.LoadRun(r.opts.RunID)
		if loadErr != nil {
			return Run{}, loadErr
		}
		return run, err
	}
	run, loadErr := r.opts.Store.LoadRun(r.opts.RunID)
	if loadErr != nil {
		return Run{}, loadErr
	}
	if !r.completedByUser && run.Status == RunStateRunning {
		run, err = r.opts.Store.UpdateRunStatus(run.ID, RunStateCompleted, "workflow script completed")
		if err != nil {
			return Run{}, err
		}
	}
	return run, nil
}

func (r *ScriptRuntime) installGlobals(ctx context.Context, vm *goja.Runtime) {
	_ = vm.Set("args", parseScriptArguments(r.opts.Arguments))
	_ = vm.Set("workflow", map[string]any{
		"runId":          r.opts.RunID,
		"definitionName": r.opts.DefinitionName,
		"definitionPath": r.opts.DefinitionPath,
		"rootDir":        r.opts.RootDir,
		"maxAgents":      r.maxAgents,
		"maxConcurrency": r.maxConcurrency,
	})
	_ = vm.Set("console", map[string]any{
		"log": func(goja.FunctionCall) goja.Value { return goja.Undefined() },
	})
	_ = vm.Set("phase", func(call goja.FunctionCall) goja.Value {
		value, err := r.phase(ctx, vm, call)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return value
	})
	_ = vm.Set("spawnAgent", func(call goja.FunctionCall) goja.Value {
		value, err := r.spawnAgent(ctx, vm, call.Argument(0))
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(scriptObject(value))
	})
	_ = vm.Set("spawn", func(call goja.FunctionCall) goja.Value {
		value, err := r.spawn(ctx, vm, call)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(scriptObject(value))
	})
	spawnBatch := func(call goja.FunctionCall) goja.Value {
		value, err := r.spawnAgents(ctx, vm, call.Argument(0))
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(scriptObject(value))
	}
	_ = vm.Set("spawnBatch", spawnBatch)
	_ = vm.Set("spawnAgents", spawnBatch)
	_ = vm.Set("awaitAgents", func(call goja.FunctionCall) goja.Value {
		value, err := r.awaitAgents(ctx, vm, call.Argument(0))
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(scriptObject(value))
	})
	_ = vm.Set("synthesize", func(call goja.FunctionCall) goja.Value {
		value, err := r.synthesize(ctx, vm, call.Argument(0))
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(scriptObject(value))
	})
	_ = vm.Set("status", func(goja.FunctionCall) goja.Value {
		value, err := r.status()
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(value)
	})
}

func (r *ScriptRuntime) phase(ctx context.Context, vm *goja.Runtime, call goja.FunctionCall) (goja.Value, error) {
	if err := r.ensureRunnable(ctx); err != nil {
		return nil, err
	}
	phaseID, phaseName, err := parseScriptPhase(vm, call.Argument(0))
	if err != nil {
		return nil, err
	}
	if _, err := r.opts.Store.EnsurePhase(r.opts.RunID, Phase{ID: phaseID, Name: phaseName, Status: PhaseStateRunnable}); err != nil {
		return nil, err
	}
	if _, err := r.opts.Store.UpdatePhaseStatus(r.opts.RunID, phaseID, PhaseStateRunning, "workflow script entered phase"); err != nil {
		return nil, err
	}
	previousPhaseID := r.currentPhaseID
	r.currentPhaseID = phaseID
	defer func() {
		r.currentPhaseID = previousPhaseID
	}()

	fn, ok := goja.AssertFunction(call.Argument(1))
	if !ok {
		if _, err := r.opts.Store.UpdatePhaseStatus(r.opts.RunID, phaseID, PhaseStateCompleted, "workflow script phase completed"); err != nil {
			return nil, err
		}
		return goja.Undefined(), nil
	}
	value, err := fn(goja.Undefined())
	if err != nil {
		message := scriptErrorMessage(err)
		_, _ = r.opts.Store.UpdatePhaseStatus(r.opts.RunID, phaseID, PhaseStateFailed, message)
		_ = r.failRun(message)
		return nil, err
	}
	if _, err := r.opts.Store.UpdatePhaseStatus(r.opts.RunID, phaseID, PhaseStateCompleted, "workflow script phase completed"); err != nil {
		return nil, err
	}
	return value, nil
}

func (r *ScriptRuntime) spawnAgents(ctx context.Context, vm *goja.Runtime, value goja.Value) ([]ScriptSpawnResult, error) {
	if goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, errors.New("spawnAgents requires an array of spawn specs")
	}
	object := value.ToObject(vm)
	if object == nil {
		return nil, errors.New("spawnAgents requires an array of spawn specs")
	}
	lengthValue := object.Get("length")
	if goja.IsUndefined(lengthValue) || goja.IsNull(lengthValue) {
		return nil, errors.New("spawnAgents requires an array of spawn specs")
	}
	length := int(lengthValue.ToInteger())
	if length < 0 {
		return nil, errors.New("spawnAgents requires a valid array length")
	}
	specs := make([]ScriptSpawnSpec, 0, length)
	for i := 0; i < length; i++ {
		spec, err := parseScriptSpawnSpec(vm, object.Get(strconv.Itoa(i)))
		if err != nil {
			return nil, fmt.Errorf("parse spawnAgents spec %d: %w", i, err)
		}
		specs = append(specs, spec)
	}
	if len(specs) > r.maxConcurrency {
		return nil, fmt.Errorf("spawnAgents requested %d agents; max_concurrency is %d", len(specs), r.maxConcurrency)
	}
	results := make([]ScriptSpawnResult, 0, len(specs))
	for _, spec := range specs {
		result, err := r.spawnAgentSpec(ctx, spec)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (r *ScriptRuntime) spawnAgent(ctx context.Context, vm *goja.Runtime, value goja.Value) (ScriptSpawnResult, error) {
	spec, err := parseScriptSpawnSpec(vm, value)
	if err != nil {
		return ScriptSpawnResult{}, err
	}
	return r.spawnAgentSpec(ctx, spec)
}

func (r *ScriptRuntime) spawn(ctx context.Context, vm *goja.Runtime, call goja.FunctionCall) (ScriptSpawnResult, error) {
	spec, err := r.parseSpawnPrimitiveSpec(vm, call)
	if err != nil {
		return ScriptSpawnResult{}, err
	}
	return r.spawnAgentSpec(ctx, spec)
}

func (r *ScriptRuntime) parseSpawnPrimitiveSpec(vm *goja.Runtime, call goja.FunctionCall) (ScriptSpawnSpec, error) {
	if len(call.Arguments) == 0 || goja.IsUndefined(call.Argument(0)) || goja.IsNull(call.Argument(0)) {
		return ScriptSpawnSpec{}, errors.New("spawn requires a prompt string or spawn spec")
	}
	first := call.Argument(0)
	if isScriptObject(first) {
		spec, err := parseScriptSpawnSpec(vm, first)
		if err != nil {
			return ScriptSpawnSpec{}, err
		}
		if spec.TaskName == "" {
			spec.TaskName = r.nextSpawnTaskName(firstNonEmpty(spec.AgentProfile, spec.Prompt, spec.Description))
		}
		return spec, nil
	}

	firstText := strings.TrimSpace(first.String())
	if firstText == "" {
		return ScriptSpawnSpec{}, errors.New("spawn requires a non-empty prompt or role")
	}

	var spec ScriptSpawnSpec
	if len(call.Arguments) > 1 && !goja.IsUndefined(call.Argument(1)) && !goja.IsNull(call.Argument(1)) {
		var err error
		spec, err = parseScriptSpawnSpec(vm, call.Argument(1))
		if err != nil {
			return ScriptSpawnSpec{}, err
		}
	}
	if spec.Prompt != "" {
		if spec.TaskName == "" {
			spec.TaskName = r.nextSpawnTaskName(firstText)
		}
		spec = addSpawnRoleToPrompt(spec, firstText)
		return spec, nil
	}

	spec.Prompt = firstText
	if spec.TaskName == "" {
		spec.TaskName = r.nextSpawnTaskName(firstText)
	}
	return spec, nil
}

func (r *ScriptRuntime) spawnAgentSpec(ctx context.Context, spec ScriptSpawnSpec) (ScriptSpawnResult, error) {
	if err := r.ensureRunnable(ctx); err != nil {
		return ScriptSpawnResult{}, err
	}
	if r.opts.AgentControl == nil {
		return ScriptSpawnResult{}, errors.New("agent control not configured")
	}
	prompt := strings.TrimSpace(spec.Prompt)
	if prompt == "" {
		return ScriptSpawnResult{}, errors.New("spawnAgent requires prompt")
	}
	taskName := strings.TrimSpace(spec.TaskName)
	if taskName == "" {
		taskName = r.nextSpawnTaskName(firstNonEmpty(spec.Description, prompt, spec.AgentProfile))
	}
	description := strings.TrimSpace(spec.Description)
	if description == "" {
		description = taskName
	}
	subagentType := strings.TrimSpace(spec.SubagentType)
	if subagentType == "" {
		subagentType = agentcontrol.DefaultSubagentType
	}
	wt, err := agentcontrol.LookupWorkerType(subagentType)
	if err != nil {
		return ScriptSpawnResult{}, err
	}
	foreground := !spec.RunInBackground && !wt.Background
	if err := r.ensureAgentCapacity(1); err != nil {
		return ScriptSpawnResult{}, err
	}
	timeout := time.Duration(0)
	if spec.TimeoutMS > 0 {
		timeout = time.Duration(spec.TimeoutMS) * time.Millisecond
	}
	result, err := r.opts.AgentControl.Spawn(ctx, agentcontrol.SpawnRequest{
		Type:         subagentType,
		TaskName:     taskName,
		AgentProfile: strings.TrimSpace(spec.AgentProfile),
		Description:  description,
		Prompt:       prompt,
		ParentID:     strings.TrimSpace(r.opts.CurrentAgentID),
		ParentPath:   r.currentAgentPath(),
		Synchronous:  foreground,
		Timeout:      timeout,
		Isolation:    strings.TrimSpace(spec.Isolation),
	})
	if err != nil {
		return ScriptSpawnResult{}, err
	}
	out := ScriptSpawnResult{
		AgentID:      result.AgentID,
		TaskName:     result.TaskName,
		AgentProfile: result.AgentProfile,
		AgentPath:    result.AgentPath,
		Status:       result.Status,
		Isolation:    result.Isolation,
		WorktreePath: result.WorktreePath,
		Result:       result.Result,
		Error:        result.Error,
		DurationMS:   result.DurationMS,
	}
	if err := r.recordSpawnResult(out, prompt); err != nil {
		return out, err
	}
	if err := r.recordWorkflowTeamMember(out, prompt); err != nil {
		return out, err
	}
	r.rememberSpawnedAgent(out.AgentID)
	if foreground && strings.TrimSpace(out.AgentID) != "" {
		refreshed, err := r.awaitAgentTargets(ctx, []string{out.AgentID})
		if err != nil {
			return out, err
		}
		if err := r.recordAwaitResult(refreshed); err != nil {
			return out, err
		}
		out = refreshSpawnResultFromAwait(out, refreshed)
	}
	return out, nil
}

func (r *ScriptRuntime) awaitAgents(ctx context.Context, vm *goja.Runtime, value goja.Value) (agentcontrol.AwaitAgentsResult, error) {
	if err := r.ensureRunnable(ctx); err != nil {
		return agentcontrol.AwaitAgentsResult{}, err
	}
	if r.opts.AgentControl == nil {
		return agentcontrol.AwaitAgentsResult{}, errors.New("agent control not configured")
	}
	args, err := parseScriptAwaitArgs(vm, value)
	if err != nil {
		return agentcontrol.AwaitAgentsResult{}, err
	}
	waitCtx := ctx
	var cancel context.CancelFunc
	if args.TimeoutMS > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, time.Duration(args.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	targets := args.Targets
	if len(targets) == 0 {
		targets = r.defaultAwaitTargets()
	}
	result, err := r.awaitAgentTargets(waitCtx, targets)
	if err != nil {
		return result, err
	}
	if err := r.recordAwaitResult(result); err != nil {
		return result, err
	}
	return result, nil
}

func (r *ScriptRuntime) awaitAgentTargets(ctx context.Context, targets []string) (agentcontrol.AwaitAgentsResult, error) {
	return r.opts.AgentControl.AwaitFrom(r.currentAgentPath(), ctx, targets)
}

func (r *ScriptRuntime) synthesize(ctx context.Context, vm *goja.Runtime, value goja.Value) (map[string]any, error) {
	if err := r.ensureRunnable(ctx); err != nil {
		return nil, err
	}
	if goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, errors.New("synthesize requires final report content")
	}
	content := ""
	switch exported := value.Export().(type) {
	case string:
		content = strings.TrimSpace(exported)
	case map[string]any:
		var payload struct {
			Content string `json:"content"`
		}
		if err := vm.ExportTo(value, &payload); err == nil && strings.TrimSpace(payload.Content) != "" {
			content = strings.TrimSpace(payload.Content)
		}
	default:
		return nil, errors.New("synthesize requires a string or {content}")
	}
	if content == "" {
		return nil, errors.New("synthesize requires final report content")
	}
	path, err := r.opts.Store.WriteFinalReport(r.opts.RunID, content)
	if err != nil {
		return nil, err
	}
	run, err := r.opts.Store.LoadRun(r.opts.RunID)
	if err != nil {
		return nil, err
	}
	if run.Status == RunStateRunning {
		run, err = r.opts.Store.UpdateRunStatus(run.ID, RunStateCompleted, "workflow script synthesized final report")
		if err != nil {
			return nil, err
		}
		r.completedByUser = true
	}
	return map[string]any{"finalReportPath": path, "status": run.Status}, nil
}

func (r *ScriptRuntime) status() (map[string]any, error) {
	run, err := r.opts.Store.LoadRun(r.opts.RunID)
	if err != nil {
		return nil, err
	}
	agents, err := r.opts.Store.ListAgentRuns(r.opts.RunID)
	if err != nil {
		return nil, err
	}
	team, err := r.opts.Store.LoadTeamPlan(r.opts.RunID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"run":          scriptObject(run),
		"agentRuns":    scriptObject(agents),
		"workflowTeam": scriptObject(team),
		"teamPlan":     scriptObject(team),
	}, nil
}

func (r *ScriptRuntime) recordSpawnResult(result ScriptSpawnResult, prompt string) error {
	now := time.Now().UTC()
	reportMissing := strings.EqualFold(strings.TrimSpace(result.Status), "completed")
	agent := AgentRun{
		ID:            result.AgentID,
		WorkflowRunID: r.opts.RunID,
		PhaseID:       r.currentPhaseID,
		AgentID:       result.AgentID,
		AgentPath:     result.AgentPath,
		TaskName:      result.TaskName,
		AgentProfile:  result.AgentProfile,
		Status:        agentRunStateFromExternal(result.Status, reportMissing),
		Prompt:        prompt,
		Result:        result.Result,
		ReportMissing: reportMissing,
		WorktreePath:  result.WorktreePath,
		DurationMS:    result.DurationMS,
		Error:         result.Error,
		StartedAt:     now,
	}
	if IsTerminalAgentRunState(agent.Status) {
		agent.CompletedAt = now
	}
	if err := r.opts.Store.UpsertAgentRun(agent); err != nil {
		return err
	}
	if r.currentPhaseID != "" {
		_, err := r.opts.Store.AttachAgentRunToPhase(r.opts.RunID, r.currentPhaseID, agent.ID)
		return err
	}
	return nil
}

func (r *ScriptRuntime) recordAwaitResult(result agentcontrol.AwaitAgentsResult) error {
	for _, item := range result.Results {
		id := strings.TrimSpace(item.AgentID)
		if id == "" {
			id = strings.TrimSpace(item.AgentPath)
		}
		if id == "" {
			continue
		}
		existing, _ := r.opts.Store.LoadAgentRun(r.opts.RunID, id)
		agent := AgentRun{
			ID:            id,
			WorkflowRunID: r.opts.RunID,
			PhaseID:       firstNonEmpty(existing.PhaseID, r.currentPhaseID),
			AgentID:       strings.TrimSpace(item.AgentID),
			AgentPath:     strings.TrimSpace(item.AgentPath),
			TaskName:      strings.TrimSpace(item.TaskName),
			AgentProfile:  strings.TrimSpace(item.AgentProfile),
			Status:        agentRunStateFromExternal(item.Status, item.ReportMissing),
			Prompt:        existing.Prompt,
			Result:        item.Result,
			ReportPath:    item.ReportPath,
			ReportMissing: item.ReportMissing,
			ChangedFiles:  item.ChangedFiles,
			Artifacts:     item.Artifacts,
			WorktreePath:  item.WorktreePath,
			InputTokens:   item.InputTokens,
			OutputTokens:  item.OutputTokens,
			DurationMS:    item.DurationMS,
			RetryCount:    existing.RetryCount,
			MaxRetries:    existing.MaxRetries,
			RetryReason:   existing.RetryReason,
			RollbackHint:  existing.RollbackHint,
			StartedAt:     existing.StartedAt,
			Error:         item.Error,
		}
		if agent.StartedAt.IsZero() {
			agent.StartedAt = time.Now().UTC()
		}
		if IsTerminalAgentRunState(agent.Status) || agent.Status == AgentRunStateAwaitingReport {
			agent.CompletedAt = time.Now().UTC()
		} else {
			agent.CompletedAt = existing.CompletedAt
		}
		if err := r.opts.Store.UpsertAgentRun(agent); err != nil {
			return err
		}
		if agent.PhaseID != "" {
			if _, err := r.opts.Store.AttachAgentRunToPhase(r.opts.RunID, agent.PhaseID, agent.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *ScriptRuntime) recordWorkflowTeamMember(result ScriptSpawnResult, prompt string) error {
	id := scriptWorkflowTeamMemberID(result)
	if id == "" {
		return nil
	}
	mode := TeamMemberEphemeral
	agentProfile := strings.TrimSpace(result.AgentProfile)
	if agentProfile != "" {
		mode = TeamMemberReuseProfile
	}
	taskName := strings.TrimSpace(result.TaskName)
	if taskName == "" {
		taskName = id
	}
	member := TeamMember{
		ID:           id,
		Role:         "Workflow script worker",
		Mode:         mode,
		AgentProfile: agentProfile,
		TaskName:     taskName,
		PhaseID:      strings.TrimSpace(r.currentPhaseID),
		Prompt:       strings.TrimSpace(prompt),
		Reason:       "Spawned by workflow script driver.",
	}

	plan, err := r.opts.Store.LoadTeamPlan(r.opts.RunID)
	if err != nil {
		return err
	}
	plan.RunID = r.opts.RunID
	for i := range plan.Members {
		if plan.Members[i].ID != member.ID {
			continue
		}
		member.CreatedProfile = plan.Members[i].CreatedProfile
		plan.Members[i] = member
		_, err := r.opts.Store.SaveTeamPlan(plan)
		return err
	}
	plan.Members = append(plan.Members, member)
	_, err = r.opts.Store.SaveTeamPlan(plan)
	return err
}

func scriptWorkflowTeamMemberID(result ScriptSpawnResult) string {
	for _, raw := range []string{result.TaskName, result.AgentID, result.AgentPath} {
		if id := sanitizeScriptWorkflowTeamMemberID(raw); id != "" {
			return id
		}
	}
	return ""
}

func sanitizeScriptWorkflowTeamMemberID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	lastSep := false
	for _, r := range raw {
		ok := r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.'
		if ok {
			b.WriteRune(r)
			lastSep = false
			continue
		}
		if !lastSep {
			b.WriteByte('_')
			lastSep = true
		}
	}
	return strings.Trim(b.String(), "._-")
}

func (r *ScriptRuntime) rememberSpawnedAgent(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	for _, existing := range r.spawnedAgentIDs {
		if existing == id {
			return
		}
	}
	r.spawnedAgentIDs = append(r.spawnedAgentIDs, id)
}

func (r *ScriptRuntime) defaultAwaitTargets() []string {
	seen := map[string]struct{}{}
	targets := make([]string, 0, len(r.spawnedAgentIDs))
	for _, id := range r.spawnedAgentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		targets = append(targets, id)
	}
	if r.opts.Store == nil {
		return targets
	}
	agents, err := r.opts.Store.ListAgentRuns(r.opts.RunID)
	if err != nil {
		return targets
	}
	for _, agent := range agents {
		id := strings.TrimSpace(agent.ID)
		if id == "" {
			id = strings.TrimSpace(agent.AgentID)
		}
		if id == "" {
			id = strings.TrimSpace(agent.AgentPath)
		}
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		targets = append(targets, id)
	}
	return targets
}

func refreshSpawnResultFromAwait(spawned ScriptSpawnResult, result agentcontrol.AwaitAgentsResult) ScriptSpawnResult {
	for _, item := range result.Results {
		if strings.TrimSpace(item.AgentID) != strings.TrimSpace(spawned.AgentID) {
			continue
		}
		if item.ReportMissing {
			return spawned
		}
		if strings.TrimSpace(item.Status) != "" {
			spawned.Status = item.Status
		}
		if strings.TrimSpace(item.Result) != "" {
			spawned.Result = item.Result
		}
		if strings.TrimSpace(item.Error) != "" {
			spawned.Error = item.Error
		}
		if strings.TrimSpace(item.AgentPath) != "" {
			spawned.AgentPath = item.AgentPath
		}
		if strings.TrimSpace(item.TaskName) != "" {
			spawned.TaskName = item.TaskName
		}
		if strings.TrimSpace(item.AgentProfile) != "" {
			spawned.AgentProfile = item.AgentProfile
		}
		if strings.TrimSpace(item.WorktreePath) != "" {
			spawned.WorktreePath = item.WorktreePath
		}
		if item.DurationMS > 0 {
			spawned.DurationMS = item.DurationMS
		}
		return spawned
	}
	return spawned
}

func (r *ScriptRuntime) ensureAgentCapacity(count int) error {
	agents, err := r.opts.Store.ListAgentRuns(r.opts.RunID)
	if err != nil {
		return err
	}
	if len(agents)+count > r.maxAgents {
		return fmt.Errorf("workflow would exceed max_agents (%d)", r.maxAgents)
	}
	return nil
}

func (r *ScriptRuntime) ensureRunnable(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		run, err := r.opts.Store.LoadRun(r.opts.RunID)
		if err != nil {
			return err
		}
		switch run.Status {
		case RunStateRunning:
			return nil
		case RunStatePaused:
			timer := time.NewTimer(scriptPausePollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
				continue
			}
		case RunStateCompleted, RunStateFailed, RunStateCancelled:
			return fmt.Errorf("workflow run %s is already %s", run.ID, run.Status)
		default:
			return fmt.Errorf("workflow run %s is %s, not running", run.ID, run.Status)
		}
	}
}

func (r *ScriptRuntime) failRun(message string) error {
	run, err := r.opts.Store.LoadRun(r.opts.RunID)
	if err != nil {
		return err
	}
	if IsTerminalRunState(run.Status) {
		return nil
	}
	_, err = r.opts.Store.UpdateRunStatus(run.ID, RunStateFailed, message)
	return err
}

func (r *ScriptRuntime) currentAgentPath() string {
	path := strings.TrimSpace(r.opts.CurrentAgentPath)
	if path == "" {
		return agentthread.RootPath
	}
	return path
}

func (r *ScriptRuntime) nextSpawnTaskName(seed string) string {
	r.spawnSeq++
	base := slugID(seed)
	if base == "" || base == "phase" {
		base = "spawn"
	}
	if len(base) > 40 {
		base = strings.Trim(base[:40], "_")
	}
	return fmt.Sprintf("%s_%d", base, r.spawnSeq)
}

func addSpawnRoleToPrompt(spec ScriptSpawnSpec, role string) ScriptSpawnSpec {
	role = strings.TrimSpace(role)
	if role == "" {
		return spec
	}
	prefix := "Role: " + role + "\n\n"
	if strings.TrimSpace(spec.Prompt) != "" && !strings.HasPrefix(strings.TrimSpace(spec.Prompt), "Role: ") {
		spec.Prompt = prefix + strings.TrimSpace(spec.Prompt)
	}
	return spec
}

func parseScriptPhase(vm *goja.Runtime, value goja.Value) (id, name string, err error) {
	if goja.IsUndefined(value) || goja.IsNull(value) {
		return "", "", errors.New("phase requires a name or {id, name}")
	}
	switch exported := value.Export().(type) {
	case string:
		name = strings.TrimSpace(exported)
	case map[string]any:
		name = strings.TrimSpace(stringFromMap(exported, "name"))
		id = strings.TrimSpace(stringFromMap(exported, "id"))
	default:
		var payload struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := vm.ExportTo(value, &payload); err == nil {
			name = strings.TrimSpace(payload.Name)
			id = strings.TrimSpace(payload.ID)
		}
	}
	if name == "" {
		return "", "", errors.New("phase name is required")
	}
	if id == "" {
		id = slugID(name)
	}
	if id == "" {
		return "", "", fmt.Errorf("phase %q does not produce a valid id", name)
	}
	return id, name, nil
}

func parseScriptAwaitArgs(vm *goja.Runtime, value goja.Value) (ScriptAwaitArgs, error) {
	if goja.IsUndefined(value) || goja.IsNull(value) {
		return ScriptAwaitArgs{}, nil
	}
	if object := value.ToObject(vm); object != nil {
		if object.Get("targets") != nil && !goja.IsUndefined(object.Get("targets")) {
			var args ScriptAwaitArgs
			if err := vm.ExportTo(value, &args); err != nil {
				return ScriptAwaitArgs{}, fmt.Errorf("parse awaitAgents args: %w", err)
			}
			args.TimeoutMS = firstScriptInt64(object, args.TimeoutMS, "timeoutMs", "timeout_ms", "TimeoutMS")
			return args, nil
		}
	}
	targets, err := targetsFromValue(vm, value)
	if err != nil {
		return ScriptAwaitArgs{}, err
	}
	return ScriptAwaitArgs{Targets: targets}, nil
}

func parseScriptSpawnSpec(vm *goja.Runtime, value goja.Value) (ScriptSpawnSpec, error) {
	if goja.IsUndefined(value) || goja.IsNull(value) {
		return ScriptSpawnSpec{}, errors.New("spawnAgent requires a spawn spec")
	}
	var spec ScriptSpawnSpec
	if err := vm.ExportTo(value, &spec); err != nil {
		return ScriptSpawnSpec{}, fmt.Errorf("parse spawnAgent spec: %w", err)
	}
	if object := value.ToObject(vm); object != nil {
		spec.TaskName = firstScriptString(object, spec.TaskName, "name", "Name")
		spec.Description = firstScriptString(object, spec.Description, "description", "Description")
		spec.Prompt = firstScriptString(object, spec.Prompt, "prompt", "Prompt")
		spec.SubagentType = firstScriptString(object, spec.SubagentType, "subagentType", "subagent_type", "SubagentType")
		spec.AgentProfile = firstScriptString(object, spec.AgentProfile, "agentProfile", "agent_profile", "AgentProfile")
		spec.Isolation = firstScriptString(object, spec.Isolation, "isolation", "Isolation")
		spec.RunInBackground = firstScriptBool(object, spec.RunInBackground, "runInBackground", "run_in_background", "RunInBackground")
		spec.TimeoutMS = firstScriptInt64(object, spec.TimeoutMS, "timeoutMs", "timeout_ms", "TimeoutMS")
	}
	return spec, nil
}

func isScriptObject(value goja.Value) bool {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return false
	}
	if _, ok := value.Export().(map[string]any); ok {
		return true
	}
	return false
}

func targetsFromValue(vm *goja.Runtime, value goja.Value) ([]string, error) {
	exported := value.Export()
	switch typed := exported.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, nil
		}
		return []string{strings.TrimSpace(typed)}, nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = appendTarget(out, item)
		}
		return out, nil
	default:
		var targets []string
		if err := vm.ExportTo(value, &targets); err == nil {
			return trimStringSlice(targets), nil
		}
		return nil, fmt.Errorf("awaitAgents target must be a string, array, or {targets, timeoutMs}")
	}
}

func appendTarget(out []string, item any) []string {
	switch typed := item.(type) {
	case string:
		if strings.TrimSpace(typed) != "" {
			return append(out, strings.TrimSpace(typed))
		}
	case map[string]any:
		for _, key := range []string{"agentId", "agent_id", "agentPath", "agent_path"} {
			if value := strings.TrimSpace(fmt.Sprint(typed[key])); value != "" && value != "<nil>" {
				return append(out, value)
			}
		}
	}
	return out
}

func firstScriptString(object *goja.Object, current string, keys ...string) string {
	if strings.TrimSpace(current) != "" {
		return current
	}
	for _, key := range keys {
		value := object.Get(key)
		if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
			continue
		}
		text := strings.TrimSpace(value.String())
		if text != "" {
			return text
		}
	}
	return current
}

func firstScriptBool(object *goja.Object, current bool, keys ...string) bool {
	if current {
		return current
	}
	for _, key := range keys {
		value := object.Get(key)
		if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
			continue
		}
		if exported, ok := value.Export().(bool); ok {
			return exported
		}
	}
	return current
}

func firstScriptInt64(object *goja.Object, current int64, keys ...string) int64 {
	if current != 0 {
		return current
	}
	for _, key := range keys {
		value := object.Get(key)
		if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
			continue
		}
		switch exported := value.Export().(type) {
		case int64:
			return exported
		case int:
			return int64(exported)
		case float64:
			return int64(exported)
		}
	}
	return current
}

func agentRunStateFromExternal(status string, reportMissing bool) AgentRunState {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued", "pending":
		return AgentRunStateQueued
	case "starting":
		return AgentRunStateStarting
	case "running":
		return AgentRunStateRunning
	case "completed":
		if reportMissing {
			return AgentRunStateAwaitingReport
		}
		return AgentRunStateCompleted
	case "failed", "not_found":
		return AgentRunStateFailed
	case "cancelled", "canceled":
		return AgentRunStateCancelled
	default:
		if reportMissing {
			return AgentRunStateAwaitingReport
		}
		return AgentRunStateRunning
	}
}

func parseScriptArguments(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var out any
	if json.Unmarshal([]byte(raw), &out) == nil {
		return out
	}
	return raw
}

func scriptObject(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return value
	}
	return out
}

func scriptErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var exception *goja.Exception
	if errors.As(err, &exception) {
		return exception.String()
	}
	return err.Error()
}

func slugID(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	var b strings.Builder
	lastSep := false
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastSep = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastSep = false
		default:
			if !lastSep && b.Len() > 0 {
				b.WriteByte('_')
				lastSep = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "phase"
	}
	return out
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func trimStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}
