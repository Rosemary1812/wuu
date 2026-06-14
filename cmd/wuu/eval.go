package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/config"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/evalharness"
	looprunner "github.com/blueberrycongee/wuu/internal/loop"
	"github.com/blueberrycongee/wuu/internal/modelprofile"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/providers/codex"
	"github.com/blueberrycongee/wuu/internal/runtime"
	sessionid "github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/sessiontrace"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/tools"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

type evalReport struct {
	StartedAt  time.Time            `json:"started_at"`
	DurationMS int64                `json:"duration_ms"`
	Provider   string               `json:"provider,omitempty"`
	Model      string               `json:"model,omitempty"`
	Config     string               `json:"config,omitempty"`
	Summary    evalSummary          `json:"summary"`
	Results    []evalharness.Result `json:"results"`
}

type evalSummary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

func runEval(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	providerName := fs.String("provider", "", "provider name in config")
	modelOverride := fs.String("model", "", "model override")
	workdir := fs.String("workdir", "", "workspace directory containing wuu config")
	taskFilter := fs.String("task", "all", "task id or comma-separated task ids")
	maxSteps := fs.Int("max-steps", 0, "max tool loop steps per task")
	timeout := fs.Duration("timeout", 10*time.Minute, "timeout per task")
	list := fs.Bool("list", false, "list built-in eval tasks")
	jsonOutput := fs.Bool("json", false, "output eval report as JSON")
	outputPath := fs.String("output", "", "write eval report JSON to this path")
	keepWorkdirs := fs.Bool("keep-workdirs", false, "keep temporary task workdirs")
	replayTrace := fs.String("replay-trace", "", "replay an eval trace JSONL file without calling a model or executing tools")
	liveCodexOAuth := fs.Bool("live-codex-oauth", false, "run live Codex OAuth E2E eval using local Codex CLI or wuu OAuth credentials")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *list {
		for _, task := range evalharness.Catalog() {
			fmt.Printf("%s\t%s\t%s\n", task.ID, task.Name, task.Description)
		}
		return nil
	}
	if strings.TrimSpace(*replayTrace) != "" {
		return runEvalTraceReplay(strings.TrimSpace(*replayTrace), *jsonOutput, *outputPath)
	}

	homeDir := os.Getenv("HOME")
	if *liveCodexOAuth {
		return runLiveCodexOAuthEval(liveCodexOAuthEvalConfig{
			ModelOverride: *modelOverride,
			TaskFilter:    *taskFilter,
			MaxSteps:      *maxSteps,
			Timeout:       *timeout,
			JSONOutput:    *jsonOutput,
			OutputPath:    *outputPath,
			KeepWorkdirs:  *keepWorkdirs,
			HomeDir:       homeDir,
		})
	}

	configRoot, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}
	cfg, configPath, err := config.LoadFrom(configRoot, homeDir)
	if err != nil {
		return err
	}
	providerCfg, resolvedName, err := cfg.ResolveProvider(*providerName)
	if err != nil {
		return err
	}
	if *modelOverride != "" {
		providerCfg.Model = *modelOverride
	}

	tasks, err := resolveEvalTasks(*taskFilter)
	if err != nil {
		return err
	}

	reportStarted := time.Now()
	results := make([]evalharness.Result, 0, len(tasks))
	for _, task := range tasks {
		result := runEvalTask(evalTaskRunConfig{
			Task:          task,
			Config:        cfg,
			ConfigPath:    configPath,
			ProviderName:  resolvedName,
			ModelOverride: *modelOverride,
			MaxSteps:      *maxSteps,
			Timeout:       *timeout,
			HomeDir:       homeDir,
			KeepWorkdir:   *keepWorkdirs,
		})
		results = append(results, result)
	}

	report := evalReport{
		StartedAt:  reportStarted,
		DurationMS: time.Since(reportStarted).Milliseconds(),
		Provider:   resolvedName,
		Model:      providerCfg.Model,
		Config:     configPath,
		Summary:    summarizeEvalResults(results),
		Results:    results,
	}
	if *outputPath != "" {
		if err := writeEvalReport(*outputPath, report); err != nil {
			return err
		}
	}
	if *jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		printEvalReport(report)
	}
	if report.Summary.Failed > 0 {
		return fmt.Errorf("eval failed: %d/%d passed", report.Summary.Passed, report.Summary.Total)
	}
	return nil
}

func runEvalTraceReplay(tracePath string, jsonOutput bool, outputPath string) error {
	summary, err := evalharness.ReplayTrace(tracePath)
	if err != nil {
		sessionSummary, sessionErr := sessiontrace.ReplayTrace(tracePath)
		if sessionErr != nil {
			return fmt.Errorf("replay eval trace: %w; replay session trace: %v", err, sessionErr)
		}
		if outputPath != "" {
			if err := writeSessionTraceReplaySummary(outputPath, sessionSummary); err != nil {
				return err
			}
		}
		if jsonOutput {
			data, err := json.MarshalIndent(sessionSummary, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}
		printSessionTraceReplay(sessionSummary)
		return nil
	}
	if outputPath != "" {
		if err := writeEvalReplaySummary(outputPath, summary); err != nil {
			return err
		}
	}
	if jsonOutput {
		data, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	printEvalTraceReplay(summary)
	if summary.Final != nil && !summary.Final.Success {
		return fmt.Errorf("eval trace replay failed: task %s did not pass", summary.Task.ID)
	}
	return nil
}

type liveCodexOAuthEvalConfig struct {
	ModelOverride string
	TaskFilter    string
	MaxSteps      int
	Timeout       time.Duration
	JSONOutput    bool
	OutputPath    string
	KeepWorkdirs  bool
	HomeDir       string
}

func runLiveCodexOAuthEval(cfg liveCodexOAuthEvalConfig) error {
	homeDir := strings.TrimSpace(cfg.HomeDir)
	if homeDir == "" {
		homeDir = os.Getenv("HOME")
	}
	source, authErr := codex.LocalOAuthStatus(homeDir)
	if authErr != nil {
		msg := fmt.Sprintf("SKIP live Codex OAuth eval: %v", authErr)
		if cfg.JSONOutput {
			report := evalReport{
				StartedAt:  time.Now(),
				DurationMS: 0,
				Provider:   "openai-codex",
				Model:      cfg.ModelOverride,
				Config:     "live-codex-oauth",
				Summary:    evalSummary{Total: 0},
			}
			if cfg.OutputPath != "" {
				if err := writeEvalReport(cfg.OutputPath, report); err != nil {
					return err
				}
			}
			data, err := json.MarshalIndent(map[string]any{
				"skipped": true,
				"reason":  msg,
				"report":  report,
			}, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}
		fmt.Println(msg)
		return nil
	}

	taskFilter := strings.TrimSpace(cfg.TaskFilter)
	if taskFilter == "" || taskFilter == "all" {
		taskFilter = "mcp_live_discovery"
	}
	tasks, err := resolveEvalTasks(taskFilter)
	if err != nil {
		return err
	}

	runtimeConfig := config.Default()
	runtimeConfig.DefaultProvider = "openai-codex"
	providerCfg := runtimeConfig.Providers["openai-codex"]
	if cfg.ModelOverride != "" {
		providerCfg.Model = cfg.ModelOverride
		runtimeConfig.Providers["openai-codex"] = providerCfg
	}

	reportStarted := time.Now()
	results := make([]evalharness.Result, 0, len(tasks))
	for _, task := range tasks {
		result := runEvalTask(evalTaskRunConfig{
			Task:          task,
			Config:        runtimeConfig,
			ConfigPath:    "live-codex-oauth",
			ProviderName:  "openai-codex",
			ModelOverride: cfg.ModelOverride,
			MaxSteps:      cfg.MaxSteps,
			Timeout:       cfg.Timeout,
			HomeDir:       homeDir,
			KeepWorkdir:   cfg.KeepWorkdirs,
		})
		results = append(results, result)
	}

	report := evalReport{
		StartedAt:  reportStarted,
		DurationMS: time.Since(reportStarted).Milliseconds(),
		Provider:   "openai-codex",
		Model:      providerCfg.Model,
		Config:     "live-codex-oauth:" + source,
		Summary:    summarizeEvalResults(results),
		Results:    results,
	}
	if cfg.OutputPath != "" {
		if err := writeEvalReport(cfg.OutputPath, report); err != nil {
			return err
		}
	}
	if cfg.JSONOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("live Codex OAuth source: %s\n", source)
		printEvalReport(report)
	}
	if report.Summary.Failed > 0 {
		return fmt.Errorf("live Codex OAuth eval failed: %d/%d passed", report.Summary.Passed, report.Summary.Total)
	}
	return nil
}

type evalTaskRunConfig struct {
	Task          evalharness.Task
	Config        config.Config
	ConfigPath    string
	ProviderName  string
	ModelOverride string
	MaxSteps      int
	Timeout       time.Duration
	HomeDir       string
	KeepWorkdir   bool
}

func runEvalTask(cfg evalTaskRunConfig) evalharness.Result {
	started := time.Now()
	result := evalharness.Result{
		TaskID:   cfg.Task.ID,
		TaskName: cfg.Task.Name,
	}

	taskRoot, err := os.MkdirTemp("", "wuu-eval-"+cfg.Task.ID+"-*")
	if err != nil {
		result.Error = err.Error()
		result.DurationMS = time.Since(started).Milliseconds()
		return result
	}
	if cfg.KeepWorkdir {
		result.Workdir = taskRoot
	} else {
		defer os.RemoveAll(taskRoot)
	}
	if cfg.Task.IsolateWuuHome {
		defer setTemporaryEnv("WUU_HOME", filepath.Join(taskRoot, ".wuu-home"))()
	}

	ctx := context.Background()
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	if err := evalharness.SetupTask(cfg.Task, taskRoot); err != nil {
		result.Error = err.Error()
		result.DurationMS = time.Since(started).Milliseconds()
		return result
	}

	runtimeConfig := cfg.Config
	if cfg.Task.Configure != nil {
		runtimeConfig = cfg.Task.Configure(taskRoot, runtimeConfig)
	}

	rt, err := runtime.NewSession(runtime.Options{
		RootDir:       taskRoot,
		HomeDir:       cfg.HomeDir,
		ConfigPath:    cfg.ConfigPath,
		Config:        runtimeConfig,
		ProviderName:  cfg.ProviderName,
		ModelOverride: cfg.ModelOverride,
	})
	if err != nil {
		result.Error = err.Error()
		result.DurationMS = time.Since(started).Milliseconds()
		return result
	}
	evalSessionID := "eval-" + cfg.Task.ID + "-" + sessionid.NewID()
	rt.SetSessionID(evalSessionID)
	defer func() {
		_, _ = rt.Cleanup()
	}()
	if cfg.MaxSteps > 0 {
		rt.StreamRunner.MaxSteps = cfg.MaxSteps
	}
	if rt.Toolkit != nil {
		if err := waitForMCPRequiredTools(ctx, rt.Toolkit, cfg.Task.RequiredTools, 5*time.Second); err != nil {
			result.Error = err.Error()
			result.DurationMS = time.Since(started).Milliseconds()
			return result
		}
	}

	var contextRequests []agent.RequestContextInfo
	if rt.StreamRunner != nil {
		previous := rt.StreamRunner.OnRequestContext
		rt.StreamRunner.OnRequestContext = func(info agent.RequestContextInfo) {
			if previous != nil {
				previous(info)
			}
			contextRequests = append(contextRequests, agent.RequestContextInfo{
				StepIndex:         info.StepIndex,
				TransientMessages: info.TransientMessages,
				ContentBytes:      info.ContentBytes,
				BlockKinds:        append([]string(nil), info.BlockKinds...),
			})
		}
	}

	history := []providers.ChatMessage{}
	if strings.TrimSpace(rt.StreamRunner.SystemPrompt) != "" {
		history = append(history, providers.ChatMessage{Role: "system", Content: rt.StreamRunner.SystemPrompt})
	}
	history = append(history, providers.ChatMessage{Role: "user", Content: cfg.Task.Prompt})

	runResult, runErr := rt.StreamRunner.RunWithCallback(ctx, history, nil)
	result.InputTokens = runResult.InputTokens
	result.OutputTokens = runResult.OutputTokens
	result.Turns = countAssistantTurns(runResult.NewMessages)
	if rt.Toolkit != nil {
		records := rt.Toolkit.ToolTelemetry()
		result.ToolCalls = len(records)
		result.ToolNames = uniqueToolNames(records)
		result.ToolSequence = toolNameSequence(records)
		result.MissingErrors = missingRequiredToolErrors(cfg.Task.RequiredErrors, records)
	}
	result.MissingTools = missingRequiredTools(cfg.Task.RequiredTools, result.ToolNames)
	result.ForbiddenToolsUsed = forbiddenToolsUsed(cfg.Task.ForbiddenTools, result.ToolNames)
	result.MissingToolCalls = missingRequiredToolCalls(cfg.Task.RequiredToolCalls, runResult.NewMessages)
	result.MissingToolSeq = missingRequiredToolSequence(cfg.Task.RequiredToolSequence, runResult.NewMessages)

	verification, verifyErr := evalharness.VerifyTask(ctx, cfg.Task, taskRoot, runResult.Content)
	if verifyErr != nil {
		result.Error = verifyErr.Error()
	} else {
		result.Success = runErr == nil && verification.Passed && len(result.MissingTools) == 0 && len(result.ForbiddenToolsUsed) == 0 && len(result.MissingToolCalls) == 0 && len(result.MissingToolSeq) == 0 && len(result.MissingErrors) == 0
		result.VerificationReason = verification.Reason
		result.VerificationEvidence = verification.Evidence
	}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	result.Observability = collectEvalObservability(rt, evalSessionID, taskRoot, cfg.KeepWorkdir, runResult.Content, contextRequests)
	applyEvalWorkflowIssues(&result)
	result.Validation = evalharness.BuildValidationSummary(result)
	result.DurationMS = time.Since(started).Milliseconds()
	persistEvalTrace(&result)
	return result
}

func applyEvalWorkflowIssues(result *evalharness.Result) {
	if result == nil || result.Observability == nil {
		return
	}
	result.WorkflowIssues = evalharness.LoopValidationIssues(result.Observability.LoopAttention, result.Observability.WorkflowRuns)
	if len(result.WorkflowIssues) > 0 {
		result.Success = false
	}
}

func setTemporaryEnv(key, value string) func() {
	previous, existed := os.LookupEnv(key)
	_ = os.Setenv(key, value)
	return func() {
		if existed {
			_ = os.Setenv(key, previous)
			return
		}
		_ = os.Unsetenv(key)
	}
}

const (
	evalAnswerPreviewLimit       = 4000
	evalTextPreviewLimit         = 1000
	evalContextBlockPreviewLimit = 800
)

var evalSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/\-=]+`),
	regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|authorization|password|secret)\s*[:=]\s*["']?[^"'\s,;]+`),
}

func collectEvalObservability(rt *runtime.Session, sessionID, taskRoot string, keepWorkdir bool, finalAnswer string, contextRequests []agent.RequestContextInfo) *evalharness.Observability {
	obs := &evalharness.Observability{
		SessionID:          strings.TrimSpace(sessionID),
		FinalAnswerPreview: evalSafePreview(finalAnswer, evalAnswerPreviewLimit),
		TaskWorkdirKept:    keepWorkdir,
		ContextRequests:    evalContextRequestObservations(contextRequests),
	}
	if keepWorkdir {
		obs.TaskWorkdir = taskRoot
	} else {
		obs.Warnings = append(obs.Warnings, "task workdir is removed after eval; pass --keep-workdirs to inspect it")
	}
	if rt == nil {
		obs.Warnings = append(obs.Warnings, "runtime session is unavailable")
		return obs
	}
	obs.StateDir = strings.TrimSpace(rt.StateDir)
	obs.ModelProfile = evalModelProfileObservation(rt)
	obs.ContextBlocks = evalContextBlockObservations(rt)
	if obs.StateDir != "" {
		obs.SessionDir = statepath.SessionArtifactDir(obs.StateDir, sessionID)
		obs.WorkflowDir = filepath.Join(obs.StateDir, "workflows")
	}
	if rt.Toolkit != nil {
		obs.ToolInventory = evalToolInventoryObservations(rt.Toolkit.ToolInfos())
		obs.ToolRecords = evalToolObservations(rt.Toolkit.ToolTelemetry())
	}
	var workflowStore *workflow.Store
	if obs.StateDir != "" {
		workflowStore = workflow.NewStore(obs.StateDir)
	}
	snapshotOpts := looprunner.SnapshotOptions{
		WorkflowStore: workflowStore,
	}
	if rt.AgentControl != nil {
		snapshotOpts.HarnessStore = rt.AgentControl.HarnessStore()
	}
	snapshot := looprunner.SnapshotSystem(snapshotOpts)
	obs.HarnessDir = snapshot.HarnessDir
	obs.LoopAttention = evalLoopAttentionObservations(snapshot.Attention)
	obs.WorkflowRuns = evalWorkflowObservations(snapshot.Workflows)
	obs.HarnessTasks = evalHarnessTaskObservations(snapshot.Harness.Tasks)
	obs.HarnessReports = evalHarnessReportObservations(snapshot.Harness.Reports)
	obs.Warnings = append(obs.Warnings, evalSafeStringSlice(snapshot.Warnings, evalTextPreviewLimit)...)
	return obs
}

func persistEvalTrace(result *evalharness.Result) {
	if result == nil || result.Observability == nil {
		return
	}
	obs := result.Observability
	if strings.TrimSpace(obs.SessionDir) == "" {
		obs.Warnings = append(obs.Warnings, "eval trace unavailable: session dir is empty")
		return
	}
	tracePath := filepath.Join(obs.SessionDir, "eval-trace.jsonl")
	obs.TracePath = tracePath
	if err := evalharness.WriteTrace(tracePath, *result); err != nil {
		obs.Warnings = append(obs.Warnings, "write eval trace: "+evalSafePreview(err.Error(), evalTextPreviewLimit))
		obs.TracePath = ""
	}
}

func evalModelProfileObservation(rt *runtime.Session) *evalharness.ModelProfileObservation {
	if rt == nil {
		return nil
	}
	apiModel := ""
	if rt.StreamRunner != nil {
		apiModel = strings.TrimSpace(rt.StreamRunner.APIModel)
	}
	modelForProfile := apiModel
	if modelForProfile == "" {
		modelForProfile = strings.TrimSpace(rt.Model)
	}
	profile := modelprofile.Resolve(rt.ProviderName, modelForProfile)
	return &evalharness.ModelProfileObservation{
		ProviderName:              rt.ProviderName,
		Model:                     rt.Model,
		APIModel:                  apiModel,
		Family:                    string(profile.Family),
		ToolCalling:               string(profile.APIShape.ToolCalling),
		FreeformTool:              profile.APIShape.FreeformTool,
		ParallelToolCalls:         profile.APIShape.ParallelToolCalls,
		ContextWindowTokens:       profile.Context.WindowTokens,
		DefaultWriteMode:          string(profile.Workflow.DefaultWriteMode),
		DefaultSearchBudget:       profile.Workflow.DefaultSearchBudget,
		DefaultMaxAutonomousSteps: profile.Workflow.DefaultMaxAutonomousSteps,
		NeedsReadBeforeWrite:      profile.Workflow.NeedsReadBeforeWrite,
		AllowParallelReadOnly:     profile.Workflow.AllowParallelReadOnly,
		AllowDirectShell:          profile.Workflow.AllowDirectShell,
	}
}

func evalContextBlockObservations(rt *runtime.Session) []evalharness.ContextBlockObservation {
	if rt == nil {
		return nil
	}
	rootDir := strings.TrimSpace(rt.RootDir)
	if rootDir == "" {
		return nil
	}
	blocks := []wuucontext.Block{
		wuucontext.EnvironmentBlock(wuucontext.Snapshot(rootDir)),
	}
	if block, ok := wuucontext.RepoMapBlock(rootDir, wuucontext.RepoMapOptions{}); ok {
		blocks = append(blocks, block)
	}
	if block, ok := wuucontext.RecentDiffBlock(rootDir, wuucontext.RecentDiffOptions{}); ok {
		blocks = append(blocks, block)
	}
	if rt.Toolkit != nil {
		blocks = append(blocks, rt.Toolkit.ContextBlocks()...)
	}
	out := make([]evalharness.ContextBlockObservation, 0, len(blocks))
	for _, block := range blocks {
		content := strings.TrimSpace(block.Content)
		if strings.TrimSpace(string(block.Kind)) == "" || content == "" {
			continue
		}
		out = append(out, evalharness.ContextBlockObservation{
			Kind:           string(block.Kind),
			Title:          strings.TrimSpace(block.Title),
			Source:         strings.TrimSpace(block.Source),
			TokenBudget:    block.TokenBudget,
			ContentBytes:   len([]byte(content)),
			ContentPreview: evalSafePreview(content, evalContextBlockPreviewLimit),
		})
	}
	return out
}

func evalContextRequestObservations(infos []agent.RequestContextInfo) []evalharness.ContextRequestObservation {
	if len(infos) == 0 {
		return nil
	}
	out := make([]evalharness.ContextRequestObservation, 0, len(infos))
	for _, info := range infos {
		if info.TransientMessages <= 0 && info.ContentBytes <= 0 && len(info.BlockKinds) == 0 {
			continue
		}
		out = append(out, evalharness.ContextRequestObservation{
			StepIndex:         info.StepIndex,
			TransientMessages: info.TransientMessages,
			ContentBytes:      info.ContentBytes,
			BlockKinds:        append([]string(nil), info.BlockKinds...),
		})
	}
	return out
}

func evalToolObservations(records []tools.ToolExecutionRecord) []evalharness.ToolObservation {
	out := make([]evalharness.ToolObservation, 0, len(records))
	for _, record := range records {
		envelope := record.ResultEnvelope()
		out = append(out, evalharness.ToolObservation{
			Name:                 record.Name,
			CallID:               record.CallID,
			ArgumentsSHA256:      record.ArgumentsSHA256,
			ResultAction:         record.ResultAction,
			Kind:                 string(record.Kind),
			Exposure:             string(record.Exposure),
			Risk:                 string(record.Risk),
			ClassificationReason: record.ClassificationReason,
			PolicyAction:         string(record.PolicyAction),
			PolicyReason:         record.PolicyReason,
			ReadOnly:             record.ReadOnly,
			ConcurrencySafe:      record.ConcurrencySafe,
			StartedAt:            record.StartedAt,
			DurationMS:           record.DurationMS,
			RevisionBefore:       record.RevisionBefore,
			RevisionAfter:        record.RevisionAfter,
			Success:              record.Success,
			Error:                evalSafePreview(record.Error, evalTextPreviewLimit),
			ErrorKind:            record.ErrorKind,
			RawOutputBytes:       record.RawOutputBytes,
			ReturnedOutputBytes:  record.ReturnedOutputBytes,
			ResultBudgeted:       record.ResultBudgeted,
			ResultRef:            record.ResultRef,
			ArtifactRefs:         append([]string(nil), record.ArtifactRefs...),
			ApprovalRef:          record.ApprovalRef,
			PatchRiskSummary:     evalPatchRiskObservation(record.PatchRiskSummary),
			ResultEnvelope:       &envelope,
		})
	}
	return out
}

func evalPatchRiskObservation(risk *tools.ToolPatchRisk) *evalharness.PatchRiskObservation {
	if risk == nil {
		return nil
	}
	actions := map[string]int(nil)
	if len(risk.Actions) > 0 {
		actions = make(map[string]int, len(risk.Actions))
		for action, count := range risk.Actions {
			actions[action] = count
		}
	}
	return &evalharness.PatchRiskObservation{
		FileCount:      risk.FileCount,
		HunkCount:      risk.HunkCount,
		AddedLines:     risk.AddedLines,
		DeletedLines:   risk.DeletedLines,
		Actions:        actions,
		MultiFile:      risk.MultiFile,
		ContainsDelete: risk.ContainsDelete,
		ContainsMove:   risk.ContainsMove,
		RiskLevel:      risk.RiskLevel,
		ReviewHint:     risk.ReviewHint,
	}
}

func evalToolInventoryObservations(infos []tools.ToolInfo) []evalharness.ToolInventoryObservation {
	out := make([]evalharness.ToolInventoryObservation, 0, len(infos))
	for _, info := range infos {
		out = append(out, evalharness.ToolInventoryObservation{
			Name:            info.Name,
			Kind:            string(info.Kind),
			Exposure:        string(info.Exposure),
			Risk:            string(info.Risk),
			ReadOnly:        info.ReadOnly,
			ConcurrencySafe: info.ConcurrencySafe,
			Destructive:     info.Destructive,
			Reason:          evalSafePreview(info.Reason, evalTextPreviewLimit),
		})
	}
	return out
}

func evalLoopAttentionObservations(items []looprunner.AttentionItem) []evalharness.LoopAttentionObservation {
	out := make([]evalharness.LoopAttentionObservation, 0, len(items))
	for _, item := range items {
		out = append(out, evalharness.LoopAttentionObservation{
			Source:  item.Source,
			ID:      item.ID,
			Status:  item.Status,
			Message: evalSafePreview(item.Message, evalTextPreviewLimit),
			Path:    item.Path,
		})
	}
	return out
}

func evalWorkflowObservations(runs []looprunner.WorkflowSnapshot) []evalharness.WorkflowRunObservation {
	out := make([]evalharness.WorkflowRunObservation, 0, len(runs))
	for _, run := range runs {
		item := evalharness.WorkflowRunObservation{
			ID:              run.ID,
			RunDir:          run.RunDir,
			EventLogPath:    run.EventLogPath,
			DefinitionName:  run.DefinitionName,
			Driver:          run.Driver,
			Entrypoint:      run.Entrypoint,
			Status:          run.Status,
			Error:           evalSafePreview(run.Error, evalTextPreviewLimit),
			ScriptPath:      run.ScriptPath,
			FinalReportPath: run.FinalReportPath,
			LoopID:          run.LoopID,
			LoopDir:         run.LoopDir,
			Phases:          evalWorkflowPhaseObservations(run.Phases),
			AgentRuns:       evalWorkflowAgentRunObservations(run.AgentRuns),
			TeamArbitration: evalWorkflowTeamArbitration(run.Arbitration),
			EventCount:      run.EventCount,
		}
		if run.Team != nil && len(run.Team.Members) > 0 {
			item.WorkflowTeam = evalWorkflowTeamObservation(*run.Team)
		}
		out = append(out, item)
	}
	return out
}

func evalWorkflowTeamArbitration(in looprunner.WorkflowArbitration) evalharness.WorkflowTeamArbitration {
	overlaps := make([]evalharness.WorkflowChangedFileOverlapObservation, 0, len(in.ChangedFileOverlaps))
	for _, overlap := range in.ChangedFileOverlaps {
		overlaps = append(overlaps, evalharness.WorkflowChangedFileOverlapObservation{
			File:        overlap.File,
			AgentRunIDs: append([]string(nil), overlap.AgentRunIDs...),
		})
	}
	return evalharness.WorkflowTeamArbitration{
		Status:              in.Status,
		OpenAgentRuns:       append([]string(nil), in.OpenAgentRuns...),
		MissingReports:      append([]string(nil), in.MissingReports...),
		FailedAgentRuns:     append([]string(nil), in.FailedAgentRuns...),
		ChangedFileOverlaps: overlaps,
		NextActions:         append([]string(nil), in.NextActions...),
	}
}

func evalWorkflowTeamObservation(team looprunner.WorkflowTeamSnapshot) *evalharness.WorkflowTeamObservation {
	out := &evalharness.WorkflowTeamObservation{
		CreatedAt: team.CreatedAt,
		UpdatedAt: team.UpdatedAt,
		Members:   make([]evalharness.WorkflowTeamMemberObservation, 0, len(team.Members)),
	}
	for _, member := range team.Members {
		out.Members = append(out.Members, evalharness.WorkflowTeamMemberObservation{
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

func evalWorkflowPhaseObservations(phases []looprunner.WorkflowPhaseSnapshot) []evalharness.WorkflowPhaseObservation {
	out := make([]evalharness.WorkflowPhaseObservation, 0, len(phases))
	for _, phase := range phases {
		out = append(out, evalharness.WorkflowPhaseObservation{
			ID:          phase.ID,
			Name:        phase.Name,
			Status:      phase.Status,
			Error:       evalSafePreview(phase.Error, evalTextPreviewLimit),
			AgentRunIDs: append([]string(nil), phase.AgentRunIDs...),
		})
	}
	return out
}

func evalWorkflowAgentRunObservations(agents []looprunner.WorkflowAgentSnapshot) []evalharness.WorkflowAgentRunObservation {
	out := make([]evalharness.WorkflowAgentRunObservation, 0, len(agents))
	for _, agent := range agents {
		out = append(out, evalharness.WorkflowAgentRunObservation{
			ID:            agent.ID,
			PhaseID:       agent.PhaseID,
			AgentID:       agent.AgentID,
			AgentPath:     agent.AgentPath,
			TaskName:      agent.TaskName,
			AgentProfile:  agent.AgentProfile,
			Status:        agent.Status,
			ReportPath:    agent.ReportPath,
			ReportMissing: agent.ReportMissing,
			ChangedFiles:  append([]string(nil), agent.ChangedFiles...),
			Artifacts:     append([]string(nil), agent.Artifacts...),
			WorktreePath:  agent.WorktreePath,
			InputTokens:   agent.InputTokens,
			OutputTokens:  agent.OutputTokens,
			DurationMS:    agent.DurationMS,
			Error:         evalSafePreview(agent.Error, evalTextPreviewLimit),
		})
	}
	return out
}

func evalHarnessTaskObservations(tasks []looprunner.HarnessTaskSnapshot) []evalharness.HarnessTaskObservation {
	out := make([]evalharness.HarnessTaskObservation, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, evalharness.HarnessTaskObservation{
			ID:            task.ID,
			ParentID:      task.ParentID,
			Path:          task.Path,
			Name:          task.Name,
			Role:          task.Role,
			LoopID:        task.LoopID,
			LoopDir:       task.LoopDir,
			Status:        task.Status,
			ReportPath:    task.ReportPath,
			ArtifactPaths: append([]string(nil), task.ArtifactPaths...),
			InputTokens:   task.InputTokens,
			OutputTokens:  task.OutputTokens,
			Error:         evalSafePreview(task.Error, evalTextPreviewLimit),
		})
	}
	return out
}

func evalHarnessReportObservations(reports []looprunner.HarnessReportSnapshot) []evalharness.HarnessReportObservation {
	out := make([]evalharness.HarnessReportObservation, 0, len(reports))
	for _, report := range reports {
		out = append(out, evalharness.HarnessReportObservation{
			ID:           report.ID,
			TaskID:       report.TaskID,
			RunID:        report.RunID,
			AgentID:      report.AgentID,
			AgentPath:    report.AgentPath,
			Outcome:      report.Outcome,
			Summary:      evalSafePreview(report.Summary, evalTextPreviewLimit),
			ChangedFiles: append([]string(nil), report.ChangedFiles...),
			Verification: evalSafeStringSlice(report.Verification, evalTextPreviewLimit),
			Artifacts:    append([]string(nil), report.Artifacts...),
			ReportPath:   report.ReportPath,
		})
	}
	return out
}

func evalSafeStringSlice(values []string, limit int) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, evalSafePreview(value, limit))
	}
	return out
}

func evalSafePreview(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, pattern := range evalSecretPatterns {
		value = pattern.ReplaceAllStringFunc(value, func(match string) string {
			idx := strings.IndexAny(match, ":=")
			if idx >= 0 {
				return strings.TrimSpace(match[:idx]) + ": [REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	if limit > 0 && len(value) > limit {
		value = value[:limit] + "... [truncated]"
	}
	return value
}

func resolveEvalTasks(input string) ([]evalharness.Task, error) {
	input = strings.TrimSpace(input)
	if input == "" || input == "all" {
		return evalharness.Catalog(), nil
	}
	parts := strings.Split(input, ",")
	tasks := make([]evalharness.Task, 0, len(parts))
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		task, ok := evalharness.ByID(id)
		if !ok {
			return nil, fmt.Errorf("unknown eval task %q", id)
		}
		tasks = append(tasks, task)
	}
	if len(tasks) == 0 {
		return nil, errors.New("no eval tasks selected")
	}
	return tasks, nil
}

func summarizeEvalResults(results []evalharness.Result) evalSummary {
	summary := evalSummary{Total: len(results)}
	for _, result := range results {
		if result.Success {
			summary.Passed++
		}
	}
	summary.Failed = summary.Total - summary.Passed
	return summary
}

func countAssistantTurns(messages []providers.ChatMessage) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == "assistant" {
			count++
		}
	}
	return count
}

func uniqueToolNames(records []tools.ToolExecutionRecord) []string {
	seen := map[string]bool{}
	names := make([]string, 0, len(records))
	for _, record := range records {
		if record.Name == "" || seen[record.Name] {
			continue
		}
		seen[record.Name] = true
		names = append(names, record.Name)
	}
	return names
}

func toolNameSequence(records []tools.ToolExecutionRecord) []string {
	if len(records) == 0 {
		return nil
	}
	names := make([]string, 0, len(records))
	for _, record := range records {
		if record.Name != "" {
			names = append(names, record.Name)
		}
	}
	return names
}

func missingRequiredTools(required []string, used []string) []string {
	if len(required) == 0 {
		return nil
	}
	usedSet := map[string]bool{}
	for _, name := range used {
		usedSet[name] = true
	}
	missing := make([]string, 0, len(required))
	for _, name := range required {
		if !usedSet[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func forbiddenToolsUsed(forbidden []string, used []string) []string {
	if len(forbidden) == 0 || len(used) == 0 {
		return nil
	}
	forbiddenSet := map[string]bool{}
	for _, name := range forbidden {
		if name != "" {
			forbiddenSet[name] = true
		}
	}
	out := make([]string, 0, len(forbidden))
	seen := map[string]bool{}
	for _, name := range used {
		if forbiddenSet[name] && !seen[name] {
			out = append(out, name)
			seen[name] = true
		}
	}
	return out
}

func missingRequiredToolCalls(required []evalharness.ToolCallRequirement, messages []providers.ChatMessage) []string {
	if len(required) == 0 {
		return nil
	}
	missing := make([]string, 0, len(required))
	for _, req := range required {
		found := false
		for _, msg := range messages {
			if msg.Role != "assistant" {
				continue
			}
			for _, call := range msg.ToolCalls {
				if call.Name != req.ToolName {
					continue
				}
				if toolCallMatchesRequirement(req, call.Arguments) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			missing = append(missing, formatToolCallRequirement(req))
		}
	}
	return missing
}

func missingRequiredToolSequence(required []evalharness.ToolCallRequirement, messages []providers.ChatMessage) []string {
	if len(required) == 0 {
		return nil
	}
	calls := assistantToolCalls(messages)
	next := 0
	for i, req := range required {
		found := false
		for next < len(calls) {
			call := calls[next]
			next++
			if call.Name == req.ToolName && toolCallMatchesRequirement(req, call.Arguments) {
				found = true
				break
			}
		}
		if !found {
			missing := make([]string, 0, len(required)-i)
			for _, remaining := range required[i:] {
				missing = append(missing, formatToolCallRequirement(remaining))
			}
			return missing
		}
	}
	return nil
}

func assistantToolCalls(messages []providers.ChatMessage) []providers.ToolCall {
	var calls []providers.ToolCall
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		calls = append(calls, msg.ToolCalls...)
	}
	return calls
}

func toolCallMatchesRequirement(req evalharness.ToolCallRequirement, argsJSON string) bool {
	for _, needle := range req.ArgsContains {
		if needle != "" && !strings.Contains(argsJSON, needle) {
			return false
		}
	}
	if len(req.ArgumentEquals) == 0 {
		return true
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return false
	}
	for key, want := range req.ArgumentEquals {
		got, ok := args[key]
		if !ok || fmt.Sprint(got) != want {
			return false
		}
	}
	return true
}

func formatToolCallRequirement(req evalharness.ToolCallRequirement) string {
	parts := []string{req.ToolName}
	if len(req.ArgumentEquals) > 0 {
		keys := make([]string, 0, len(req.ArgumentEquals))
		for key := range req.ArgumentEquals {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, key+"="+req.ArgumentEquals[key])
		}
	}
	for _, needle := range req.ArgsContains {
		if needle != "" {
			parts = append(parts, "contains="+needle)
		}
	}
	return strings.Join(parts, " ")
}

func missingRequiredToolErrors(required []evalharness.ToolErrorRequirement, records []tools.ToolExecutionRecord) []string {
	if len(required) == 0 {
		return nil
	}
	missing := make([]string, 0, len(required))
	for _, req := range required {
		found := false
		for _, record := range records {
			if record.Name != req.ToolName || record.Success {
				continue
			}
			if req.ErrorContains == "" || strings.Contains(record.Error, req.ErrorContains) {
				found = true
				break
			}
		}
		if !found {
			if req.ErrorContains == "" {
				missing = append(missing, req.ToolName)
			} else {
				missing = append(missing, req.ToolName+":"+req.ErrorContains)
			}
		}
	}
	return missing
}

func waitForMCPRequiredTools(ctx context.Context, toolkit *tools.Toolkit, required []string, timeout time.Duration) error {
	var mcpNames []string
	for _, name := range required {
		if strings.HasPrefix(name, "mcp_") {
			mcpNames = append(mcpNames, name)
		}
	}
	if len(mcpNames) == 0 || toolkit == nil {
		return nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		allFound := true
		for _, name := range mcpNames {
			if _, ok := toolkit.ToolInfo(name); !ok {
				allFound = false
				break
			}
		}
		if allFound {
			return nil
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for required MCP tools: %s", strings.Join(mcpNames, ","))
		case <-ticker.C:
		}
	}
}

func writeEvalReport(path string, report evalReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	resolved, err := resolveRuntimePath("", path)
	if err != nil {
		return err
	}
	return os.WriteFile(resolved, append(data, '\n'), 0o644)
}

func writeEvalReplaySummary(path string, summary evalharness.TraceReplaySummary) error {
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	resolved, err := resolveRuntimePath("", path)
	if err != nil {
		return err
	}
	return os.WriteFile(resolved, append(data, '\n'), 0o644)
}

func writeSessionTraceReplaySummary(path string, summary sessiontrace.ReplaySummary) error {
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	resolved, err := resolveRuntimePath("", path)
	if err != nil {
		return err
	}
	return os.WriteFile(resolved, append(data, '\n'), 0o644)
}

func printEvalReport(report evalReport) {
	fmt.Printf("eval: %d/%d passed in %dms\n", report.Summary.Passed, report.Summary.Total, report.DurationMS)
	for _, result := range report.Results {
		status := "FAIL"
		if result.Success {
			status = "PASS"
		}
		fmt.Printf("%s %s turns=%d tools=%d input=%d output=%d duration=%dms\n",
			status, result.TaskID, result.Turns, result.ToolCalls, result.InputTokens, result.OutputTokens, result.DurationMS)
		if len(result.ToolNames) > 0 {
			fmt.Printf("  tool_names: %s\n", strings.Join(result.ToolNames, ","))
		}
		if len(result.ToolSequence) > 0 {
			fmt.Printf("  tool_sequence: %s\n", strings.Join(result.ToolSequence, ","))
		}
		if len(result.MissingTools) > 0 {
			fmt.Printf("  missing_tools: %s\n", strings.Join(result.MissingTools, ","))
		}
		if len(result.ForbiddenToolsUsed) > 0 {
			fmt.Printf("  forbidden_tools: %s\n", strings.Join(result.ForbiddenToolsUsed, ","))
		}
		if len(result.MissingToolCalls) > 0 {
			fmt.Printf("  missing_tool_calls: %s\n", strings.Join(result.MissingToolCalls, ","))
		}
		if len(result.MissingToolSeq) > 0 {
			fmt.Printf("  missing_tool_sequence: %s\n", strings.Join(result.MissingToolSeq, ","))
		}
		if len(result.MissingErrors) > 0 {
			fmt.Printf("  missing_errors: %s\n", strings.Join(result.MissingErrors, ","))
		}
		if len(result.WorkflowIssues) > 0 {
			fmt.Printf("  workflow_issues: %s\n", strings.Join(result.WorkflowIssues, ","))
		}
		if result.Validation != nil {
			fmt.Printf("  validation: status=%s tools=%d evidence=%d missing=%d failures=%d\n",
				result.Validation.Status, len(result.Validation.ToolCalls), len(result.Validation.Evidence), len(result.Validation.Missing), len(result.Validation.Failures))
		}
		if result.Error != "" {
			fmt.Printf("  error: %s\n", result.Error)
		} else if !result.Success && result.VerificationReason != "" {
			fmt.Printf("  verify: %s\n", firstLine(result.VerificationReason))
		}
		if result.Workdir != "" {
			fmt.Printf("  workdir: %s\n", result.Workdir)
		}
	}
}

func printEvalTraceReplay(summary evalharness.TraceReplaySummary) {
	status := "FAIL"
	if summary.Final != nil && summary.Final.Success {
		status = "PASS"
	}
	taskID := ""
	taskName := ""
	if summary.Task != nil {
		taskID = summary.Task.ID
		taskName = summary.Task.Name
	}
	fmt.Printf("eval trace replay: %s %s events=%d complete=%t mode=%s\n", status, taskID, summary.EventCount, summary.Complete, summary.Mode)
	if taskName != "" {
		fmt.Printf("  task_name: %s\n", taskName)
	}
	if summary.ModelProfile != nil {
		fmt.Printf("  model_profile: %s/%s family=%s write_mode=%s\n", summary.ModelProfile.ProviderName, summary.ModelProfile.Model, summary.ModelProfile.Family, summary.ModelProfile.DefaultWriteMode)
	}
	if len(summary.ToolInventory) > 0 {
		fmt.Printf("  tool_inventory: %d tools\n", len(summary.ToolInventory))
	}
	if len(summary.ContextBlockKinds) > 0 {
		fmt.Printf("  context_blocks: %s\n", strings.Join(summary.ContextBlockKinds, ","))
	}
	if len(summary.ToolNames) > 0 {
		fmt.Printf("  tool_records: %s\n", strings.Join(summary.ToolNames, ","))
	}
	if summary.Task != nil && len(summary.Task.ForbiddenToolsUsed) > 0 {
		fmt.Printf("  forbidden_tools: %s\n", strings.Join(summary.Task.ForbiddenToolsUsed, ","))
	}
	if summary.Task != nil && len(summary.Task.WorkflowIssues) > 0 {
		fmt.Printf("  workflow_issues: %s\n", strings.Join(summary.Task.WorkflowIssues, ","))
	}
	if attention := formatEvalLoopAttention(summary.LoopAttention); attention != "" {
		fmt.Printf("  loop_attention: %s\n", attention)
	}
	if summary.ToolSummary != nil {
		fmt.Printf("  tool_summary: total=%d succeeded=%d failed=%d\n", summary.ToolSummary.Total, summary.ToolSummary.Succeeded, summary.ToolSummary.Failed)
		if len(summary.ToolSummary.ByResultAction) > 0 {
			fmt.Printf("  result_actions: %s\n", formatCountMap(summary.ToolSummary.ByResultAction))
		}
		if blocks := formatEvalPolicyBlocks(summary.ToolSummary.PolicyBlocks); blocks != "" {
			fmt.Printf("  policy_blocks: %s\n", blocks)
		}
		if repeated := formatEvalRepeatedArguments(summary.ToolSummary.RepeatedArguments); repeated != "" {
			fmt.Printf("  repeated_arguments: %s\n", repeated)
		}
		if summary.ToolSummary.PatchRisk != nil {
			risk := summary.ToolSummary.PatchRisk
			fmt.Printf("  patch_risk: total=%d levels=%s files=%d hunks=%d +%d -%d\n",
				risk.Total, formatCountMap(risk.ByLevel), risk.FileCount, risk.HunkCount, risk.AddedLines, risk.DeletedLines)
		}
	}
	if summary.Validation != nil {
		fmt.Printf("  validation: status=%s tools=%d evidence=%d missing=%d failures=%d\n",
			summary.Validation.Status, len(summary.Validation.ToolCalls), len(summary.Validation.Evidence), len(summary.Validation.Missing), len(summary.Validation.Failures))
		if evidence := formatEvalValidationEvidence(summary.Validation.Evidence); evidence != "" {
			fmt.Printf("  validation_evidence: %s\n", evidence)
		}
		if missing := formatDelimitedValues(summary.Validation.Missing, ","); missing != "" {
			fmt.Printf("  validation_missing: %s\n", missing)
		}
		if failures := formatDelimitedValues(summary.Validation.Failures, ","); failures != "" {
			fmt.Printf("  validation_failures: %s\n", failures)
		}
	}
	if runs := formatEvalWorkflowRuns(summary.WorkflowRuns, summary.WorkflowRunIDs); runs != "" {
		fmt.Printf("  workflow_runs: %s\n", runs)
	}
	if agents := formatEvalWorkflowAgents(summary.WorkflowRuns); agents != "" {
		fmt.Printf("  workflow_agents: %s\n", agents)
	}
	if arbitration := formatEvalWorkflowArbitration(summary.WorkflowRuns); arbitration != "" {
		fmt.Printf("  workflow_arbitration: %s\n", arbitration)
	}
	if summary.Observability != nil {
		if summary.Observability.SessionID != "" {
			fmt.Printf("  session_id: %s\n", summary.Observability.SessionID)
		}
		if summary.Observability.TracePath != "" {
			fmt.Printf("  trace_path: %s\n", summary.Observability.TracePath)
		}
	}
	if summary.Final != nil {
		if summary.Final.VerificationReason != "" {
			fmt.Printf("  verify: %s\n", firstLine(summary.Final.VerificationReason))
		}
		if summary.Final.Error != "" {
			fmt.Printf("  error: %s\n", firstLine(summary.Final.Error))
		}
	}
	for _, warning := range summary.Warnings {
		fmt.Printf("  warning: %s\n", warning)
	}
}

func formatCountMap(values map[string]int) string {
	if len(values) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, values[key]))
	}
	return strings.Join(parts, ",")
}

func printSessionTraceReplay(summary sessiontrace.ReplaySummary) {
	status := ""
	if summary.Final != nil {
		status = summary.Final.Status
	}
	threadID := ""
	turnID := ""
	if summary.LatestTurn != nil {
		threadID = summary.LatestTurn.ThreadID
		turnID = summary.LatestTurn.TurnID
	}
	fmt.Printf("session trace replay: status=%s thread=%s turn=%s events=%d complete=%t mode=%s\n", status, threadID, turnID, summary.EventCount, summary.Complete, summary.Mode)
	if summary.LatestTurn != nil {
		if summary.LatestTurn.ModelProfile != nil {
			profile := summary.LatestTurn.ModelProfile
			fmt.Printf("  model_profile: %s/%s api_model=%s family=%s write_mode=%s\n", profile.ProviderName, profile.Model, profile.APIModel, profile.Family, profile.DefaultWriteMode)
		} else if summary.LatestTurn.ProviderName != "" || summary.LatestTurn.Model != "" {
			fmt.Printf("  model_profile: %s/%s api_model=%s\n", summary.LatestTurn.ProviderName, summary.LatestTurn.Model, summary.LatestTurn.APIModel)
		}
		if summary.LatestTurn.InputTokens > 0 || summary.LatestTurn.OutputTokens > 0 {
			fmt.Printf("  tokens: input=%d output=%d\n", summary.LatestTurn.InputTokens, summary.LatestTurn.OutputTokens)
		}
	}
	if len(summary.ContextRequests) > 0 {
		fmt.Printf("  context_requests: %d\n", len(summary.ContextRequests))
	}
	if len(summary.ContextBlockKinds) > 0 {
		fmt.Printf("  context_blocks: %s\n", strings.Join(summary.ContextBlockKinds, ","))
	}
	if len(summary.ToolInventory) > 0 {
		fmt.Printf("  tool_inventory: %d tools\n", len(summary.ToolInventory))
	}
	if len(summary.ToolNames) > 0 {
		fmt.Printf("  tool_records: %s\n", strings.Join(summary.ToolNames, ","))
	}
	if summary.ToolSummary != nil {
		fmt.Printf("  tool_summary: total=%d succeeded=%d failed=%d\n", summary.ToolSummary.Total, summary.ToolSummary.Succeeded, summary.ToolSummary.Failed)
		if len(summary.ToolSummary.ByResultAction) > 0 {
			fmt.Printf("  result_actions: %s\n", formatCountMap(summary.ToolSummary.ByResultAction))
		}
		if blocks := formatSessionPolicyBlocks(summary.ToolSummary.PolicyBlocks); blocks != "" {
			fmt.Printf("  policy_blocks: %s\n", blocks)
		}
		if repeated := formatSessionRepeatedArguments(summary.ToolSummary.RepeatedArguments); repeated != "" {
			fmt.Printf("  repeated_arguments: %s\n", repeated)
		}
	}
	if summary.Final != nil {
		if summary.Final.Error != "" {
			fmt.Printf("  error: %s\n", firstLine(summary.Final.Error))
		}
		if summary.Final.FinalAnswerPreview != "" {
			fmt.Printf("  final: %s\n", firstLine(summary.Final.FinalAnswerPreview))
		}
	}
	for _, warning := range summary.Warnings {
		fmt.Printf("  warning: %s\n", warning)
	}
}

func formatEvalWorkflowRuns(runs []evalharness.WorkflowRunObservation, fallbackIDs []string) string {
	parts := make([]string, 0, len(runs))
	for _, run := range runs {
		id := strings.TrimSpace(run.ID)
		if id == "" {
			continue
		}
		labelParts := []string{id}
		if driver := strings.TrimSpace(run.Driver); driver != "" {
			labelParts = append(labelParts, "driver="+driver)
		}
		if status := strings.TrimSpace(run.Status); status != "" {
			labelParts = append(labelParts, "status="+status)
		}
		if eventLog := strings.TrimSpace(run.EventLogPath); eventLog != "" {
			labelParts = append(labelParts, "event_log="+eventLog)
		}
		if runDir := strings.TrimSpace(run.RunDir); runDir != "" {
			labelParts = append(labelParts, "run_dir="+runDir)
		}
		parts = append(parts, strings.Join(labelParts, ":"))
	}
	if len(parts) > 0 {
		return strings.Join(parts, ",")
	}
	return strings.Join(fallbackIDs, ",")
}

func formatEvalLoopAttention(items []evalharness.LoopAttentionObservation) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		source := strings.TrimSpace(item.Source)
		if source == "" {
			source = "loop"
		}
		labelParts := []string{source}
		if id := strings.TrimSpace(item.ID); id != "" {
			labelParts = append(labelParts, "id="+id)
		}
		if status := strings.TrimSpace(item.Status); status != "" {
			labelParts = append(labelParts, "status="+status)
		}
		if path := strings.TrimSpace(item.Path); path != "" {
			labelParts = append(labelParts, "path="+path)
		}
		if message := strings.TrimSpace(item.Message); message != "" {
			labelParts = append(labelParts, "message="+firstLine(message))
		}
		parts = append(parts, strings.Join(labelParts, ":"))
	}
	return strings.Join(parts, ",")
}

func formatEvalWorkflowAgents(runs []evalharness.WorkflowRunObservation) string {
	parts := []string(nil)
	for _, run := range runs {
		runID := strings.TrimSpace(run.ID)
		for _, agent := range run.AgentRuns {
			agentID := strings.TrimSpace(agent.ID)
			if agentID == "" {
				continue
			}
			if runID != "" {
				agentID = runID + "/" + agentID
			}
			labelParts := []string{agentID}
			if task := strings.TrimSpace(agent.TaskName); task != "" {
				labelParts = append(labelParts, "task="+task)
			}
			if profile := strings.TrimSpace(agent.AgentProfile); profile != "" {
				labelParts = append(labelParts, "profile="+profile)
			}
			if status := strings.TrimSpace(agent.Status); status != "" {
				labelParts = append(labelParts, "status="+status)
			}
			if report := strings.TrimSpace(agent.ReportPath); report != "" {
				labelParts = append(labelParts, "report="+report)
			}
			if agent.ReportMissing {
				labelParts = append(labelParts, "report_missing=true")
			}
			if worktree := strings.TrimSpace(agent.WorktreePath); worktree != "" {
				labelParts = append(labelParts, "worktree="+worktree)
			}
			if changed := formatDelimitedValues(agent.ChangedFiles, "|"); changed != "" {
				labelParts = append(labelParts, "changed="+changed)
			}
			if artifacts := formatDelimitedValues(agent.Artifacts, "|"); artifacts != "" {
				labelParts = append(labelParts, "artifacts="+artifacts)
			}
			if errText := strings.TrimSpace(agent.Error); errText != "" {
				labelParts = append(labelParts, "error="+firstLine(errText))
			}
			parts = append(parts, strings.Join(labelParts, ":"))
		}
	}
	return strings.Join(parts, ",")
}

func formatEvalWorkflowArbitration(runs []evalharness.WorkflowRunObservation) string {
	parts := []string(nil)
	for _, run := range runs {
		arbitration := run.TeamArbitration
		if arbitration.Status == "" &&
			len(arbitration.OpenAgentRuns) == 0 &&
			len(arbitration.MissingReports) == 0 &&
			len(arbitration.FailedAgentRuns) == 0 &&
			len(arbitration.ChangedFileOverlaps) == 0 &&
			len(arbitration.NextActions) == 0 {
			continue
		}
		id := strings.TrimSpace(run.ID)
		if id == "" {
			id = "workflow"
		}
		labelParts := []string{id}
		if status := strings.TrimSpace(arbitration.Status); status != "" {
			labelParts = append(labelParts, "status="+status)
		}
		if open := formatDelimitedValues(arbitration.OpenAgentRuns, "|"); open != "" {
			labelParts = append(labelParts, "open="+open)
		}
		if missing := formatDelimitedValues(arbitration.MissingReports, "|"); missing != "" {
			labelParts = append(labelParts, "missing_reports="+missing)
		}
		if failed := formatDelimitedValues(arbitration.FailedAgentRuns, "|"); failed != "" {
			labelParts = append(labelParts, "failed="+failed)
		}
		if overlaps := formatEvalWorkflowChangedFileOverlaps(arbitration.ChangedFileOverlaps); overlaps != "" {
			labelParts = append(labelParts, "overlaps="+overlaps)
		}
		if next := formatDelimitedValues(arbitration.NextActions, "|"); next != "" {
			labelParts = append(labelParts, "next="+next)
		}
		parts = append(parts, strings.Join(labelParts, ":"))
	}
	return strings.Join(parts, ",")
}

func formatEvalWorkflowChangedFileOverlaps(overlaps []evalharness.WorkflowChangedFileOverlapObservation) string {
	parts := make([]string, 0, len(overlaps))
	for _, overlap := range overlaps {
		file := strings.TrimSpace(overlap.File)
		if file == "" {
			continue
		}
		agents := formatDelimitedValues(overlap.AgentRunIDs, "+")
		if agents != "" {
			parts = append(parts, file+"="+agents)
		} else {
			parts = append(parts, file)
		}
	}
	return strings.Join(parts, "|")
}

func formatEvalValidationEvidence(values []evalharness.VerificationEvidence) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		label := strings.TrimSpace(value.Check)
		if label == "" {
			label = "verification"
		}
		status := "failed"
		if value.Passed {
			status = "passed"
		}
		piece := label + ":" + status
		if command := strings.TrimSpace(value.Command); command != "" {
			piece += ":command=" + firstLine(command)
		}
		if path := strings.TrimSpace(value.Path); path != "" {
			piece += ":path=" + path
		}
		parts = append(parts, piece)
	}
	return strings.Join(parts, ",")
}

func formatDelimitedValues(values []string, separator string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, separator)
}

func formatEvalPolicyBlocks(values []evalharness.ToolPolicyBlockSummary) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if part := formatPolicyBlockLabel(value.ToolName, value.CallID, value.PolicyAction, value.ErrorKind, value.ApprovalRef); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ",")
}

func formatSessionPolicyBlocks(values []sessiontrace.ToolPolicyBlockSummary) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if part := formatPolicyBlockLabel(value.ToolName, value.CallID, value.PolicyAction, value.ErrorKind, value.ApprovalRef); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ",")
}

func formatPolicyBlockLabel(toolName, callID, action, errorKind, approvalRef string) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return ""
	}
	parts := []string{toolName}
	if action = strings.TrimSpace(action); action != "" {
		parts = append(parts, action)
	}
	if errorKind = strings.TrimSpace(errorKind); errorKind != "" {
		parts = append(parts, errorKind)
	}
	label := strings.Join(parts, ":")
	if callID = strings.TrimSpace(callID); callID != "" {
		label += ":call_id=" + callID
	}
	if approvalRef = strings.TrimSpace(approvalRef); approvalRef != "" {
		label += ":approval_ref=" + approvalRef
	}
	return label
}

func formatEvalRepeatedArguments(values []evalharness.ToolRepeatedArgumentSummary) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value.ToolName == "" || value.ArgumentsSHA256 == "" || value.Count < 2 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%s=%d", value.ToolName, value.ArgumentsSHA256, value.Count))
	}
	return strings.Join(parts, ",")
}

func formatSessionRepeatedArguments(values []sessiontrace.ToolRepeatedArgumentSummary) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value.ToolName == "" || value.ArgumentsSHA256 == "" || value.Count < 2 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%s=%d", value.ToolName, value.ArgumentsSHA256, value.Count))
	}
	return strings.Join(parts, ",")
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		return value[:idx]
	}
	return value
}
