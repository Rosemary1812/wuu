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
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/appserver"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/evalharness"
	processruntime "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providerfactory"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/providers/codex"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/tools"
	"github.com/blueberrycongee/wuu/internal/tui"
	"github.com/blueberrycongee/wuu/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return runTUI(nil)
	}

	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "models":
		return runModels(args[1:])
	case "run":
		return runTask(args[1:])
	case "eval":
		return runEval(args[1:])
	case "tui":
		return runTUI(args[1:])
	case "app-server":
		return runAppServer(args[1:])
	case "version", "-v", "--version":
		if args[0] == "version" {
			return runVersion(args[1:])
		}
		return runVersion(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		// No subcommand → default to TUI.
		return runTUI(args)
	}
}

func runVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	long := fs.Bool("long", false, "show detailed version info")
	jsonOutput := fs.Bool("json", false, "output version as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	info := version.Info()
	if *jsonOutput {
		data, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal version info: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}
	if *long {
		fmt.Println(info.LongString())
		return nil
	}

	fmt.Println(info.String())
	return nil
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "overwrite existing .wuu.json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	workdir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get current directory: %w", err)
	}
	configPath := filepath.Join(workdir, ".wuu.json")

	if !*force {
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", configPath)
		}
	}

	result, err := runOnboarding()
	if err != nil {
		return err
	}
	if !result.Completed {
		fmt.Println("setup cancelled")
		return nil
	}

	return writeOnboardingResult(workdir, os.Getenv("HOME"), result)
}

func runModels(args []string) error {
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	providerName := fs.String("provider", "", "provider name in config")
	workdir := fs.String("workdir", "", "workspace directory")
	jsonOutput := fs.Bool("json", false, "output model metadata as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}
	cfg, configPath, err := config.LoadFrom(rootDir, os.Getenv("HOME"))
	if err != nil {
		return err
	}
	providerCfg, resolvedName, err := cfg.ResolveProvider(*providerName)
	if err != nil {
		return err
	}
	if !isCodexModelsProvider(providerCfg.Type) {
		return fmt.Errorf("provider %q uses type %q; live model lookup currently supports openai-codex providers only", resolvedName, providerCfg.Type)
	}

	client, err := codex.New(codex.ClientConfig{
		BaseURL: providerCfg.BaseURL,
		APIKey:  explicitProviderAPIKey(providerCfg),
		Headers: providerCfg.Headers,
	})
	if err != nil {
		return err
	}
	models, err := client.Models(context.Background())
	if err != nil {
		return err
	}
	if *jsonOutput {
		data, err := json.MarshalIndent(models, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal models: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("provider: %s\nconfig: %s\n\n", resolvedName, configPath)
	for _, model := range models {
		fmt.Println(model.Slug)
	}
	return nil
}

func runTask(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	providerName := fs.String("provider", "", "provider name in config")
	modelOverride := fs.String("model", "", "model override")
	maxSteps := fs.Int("max-steps", 0, "max tool loop steps")
	temperature := fs.Float64("temperature", -1, "sampling temperature override")
	systemPrompt := fs.String("system-prompt", "", "system prompt override")
	workdir := fs.String("workdir", "", "workspace directory")
	noTools := fs.Bool("no-tools", false, "disable local tools")
	timeout := fs.Duration("timeout", 10*time.Minute, "request timeout (e.g. 5m)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}

	cfg, configPath, err := config.LoadFrom(rootDir, os.Getenv("HOME"))
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

	client, err := providerfactory.BuildStreamClient(providerCfg, resolvedName)
	if err != nil {
		return err
	}

	var toolExecutor agent.ToolExecutor
	var processMgr *processruntime.Manager
	wuuHome, err := statepath.Home(os.Getenv("HOME"))
	if err != nil {
		return err
	}
	workspaceStateDir, err := statepath.WorkspaceDir(wuuHome, rootDir)
	if err != nil {
		return err
	}
	processMgr, err = processruntime.NewManager(rootDir, statepath.RuntimeDir(workspaceStateDir))
	if err != nil {
		return err
	}
	if !*noTools {
		kit, newErr := tools.New(rootDir)
		if newErr != nil {
			return newErr
		}
		// Default normal mode: main agent retains all tools including write_file,
		// edit_file, and run_shell. Coordinator mode can be entered at runtime via
		// the /coordinator slash command.
		kit.SetStateDir(workspaceStateDir)
		kit.SetProcessManager(processMgr)
		kit.SetToolPolicy(runtime.ToolPolicyFromConfig(cfg.Agent.ToolPolicy))
		toolExecutor = kit
	}

	runner := agent.StreamRunner{
		Client:       client,
		Tools:        toolExecutor,
		Model:        providerCfg.Model,
		SystemPrompt: cfg.Agent.SystemPrompt,
		MaxSteps:     cfg.Agent.MaxSteps,
		Temperature:  cfg.Agent.Temperature,
		Effort:       cfg.Agent.Effort,
		ContextWindowOverride: runtime.ResolveContextWindow(
			providerCfg.Model,
			providerCfg.ContextWindow,
			cfg.Agent.MaxContextTokens,
		),
	}

	if *maxSteps > 0 {
		runner.MaxSteps = *maxSteps
	}
	if *temperature >= 0 {
		runner.Temperature = *temperature
	}
	if strings.TrimSpace(*systemPrompt) != "" {
		runner.SystemPrompt = *systemPrompt
	}

	prompt, err := resolvePrompt(fs.Args())
	if err != nil {
		return err
	}

	ctx := context.Background()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	answer, err := runner.Run(ctx, prompt)
	if err != nil {
		return err
	}

	fmt.Printf("provider: %s\nmodel: %s\nconfig: %s\n\n", resolvedName, providerCfg.Model, configPath)
	fmt.Println(answer)
	return nil
}

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
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *list {
		for _, task := range evalharness.Catalog() {
			fmt.Printf("%s\t%s\t%s\n", task.ID, task.Name, task.Description)
		}
		return nil
	}

	configRoot, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}
	homeDir := os.Getenv("HOME")
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
	}
	result.MissingTools = missingRequiredTools(cfg.Task.RequiredTools, result.ToolNames)

	verification, verifyErr := evalharness.VerifyTask(ctx, cfg.Task, taskRoot, runResult.Content)
	if verifyErr != nil {
		result.Error = verifyErr.Error()
	} else {
		result.Success = runErr == nil && verification.Passed && len(result.MissingTools) == 0
		result.VerificationReason = verification.Reason
	}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	result.DurationMS = time.Since(started).Milliseconds()
	return result
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
		if len(result.MissingTools) > 0 {
			fmt.Printf("  missing_tools: %s\n", strings.Join(result.MissingTools, ","))
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

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		return value[:idx]
	}
	return value
}

func runTUI(args []string) error {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	providerName := fs.String("provider", "", "provider name in config")
	modelOverride := fs.String("model", "", "model override")
	maxSteps := fs.Int("max-steps", 0, "max tool loop steps")
	temperature := fs.Float64("temperature", -1, "sampling temperature override")
	systemPrompt := fs.String("system-prompt", "", "system prompt override")
	themeMode := fs.String("theme", "", "theme override: auto|dark|light")
	workdir := fs.String("workdir", "", "workspace directory")
	noTools := fs.Bool("no-tools", false, "disable local tools")
	requestTimeout := fs.Duration("request-timeout", 0, "turn timeout (e.g. 2m, 0 disables)")
	memoryFile := fs.String("memory-file", "", "session memory file path (deprecated, use sessions)")
	resumeID := fs.String("resume", "", "resume session by ID (empty with flag = most recent)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}

	homeDir := os.Getenv("HOME")

	resolvedTheme, err := resolveTUIThemeMode(homeDir, strings.TrimSpace(*themeMode))
	if err != nil {
		return err
	}
	if err := tui.SetThemeMode(resolvedTheme); err != nil {
		if strings.TrimSpace(*themeMode) != "" {
			return err
		}
		// Invalid persisted preference should never block startup.
		if fallbackErr := tui.SetThemeMode("auto"); fallbackErr != nil {
			return fallbackErr
		}
	}

	cfg, configPath, err := config.LoadFrom(rootDir, homeDir)
	if err != nil {
		// Only enter onboarding when the config genuinely does not
		// exist. A present-but-broken config (parse error, failed
		// validation, etc.) must surface so the user can fix it —
		// otherwise onboarding would silently overwrite their
		// existing .wuu.json with a fresh template.
		if !errors.Is(err, config.ErrConfigNotFound) {
			return err
		}
		result, onboardErr := runOnboarding()
		if onboardErr != nil {
			return onboardErr
		}
		if !result.Completed {
			return nil // user cancelled
		}

		if writeErr := writeOnboardingResult(rootDir, homeDir, result); writeErr != nil {
			return writeErr
		}

		// Reload config.
		cfg, configPath, err = config.LoadFrom(rootDir, homeDir)
		if err != nil {
			return fmt.Errorf("config still invalid after onboarding: %w", err)
		}
	}

	askBridge := tui.NewAskUserBridge()
	rt, err := runtime.NewSession(runtime.Options{
		RootDir:       rootDir,
		HomeDir:       homeDir,
		ConfigPath:    configPath,
		Config:        cfg,
		ProviderName:  *providerName,
		ModelOverride: *modelOverride,
		NoTools:       *noTools,
		AskBridge:     askBridge,
	})
	if err != nil {
		return err
	}
	if *maxSteps > 0 {
		rt.StreamRunner.MaxSteps = *maxSteps
	}
	if *temperature >= 0 {
		rt.StreamRunner.Temperature = *temperature
	}
	if strings.TrimSpace(*systemPrompt) != "" {
		// CLI --system-prompt overrides everything, including base and
		// coordinator preamble. It becomes the new base for both modes.
		rt.StreamRunner.SystemPrompt = *systemPrompt
		rt.BaseSystemPrompt = *systemPrompt
	}

	resolvedMemoryPath, err := resolveRuntimePath(rootDir, *memoryFile)
	if err != nil {
		return err
	}

	sessDir := rt.SessionDir

	// Handle --resume flag.
	resolvedResumeID := strings.TrimSpace(*resumeID)
	// Check if --resume was passed without a value (flag present but empty).
	for _, a := range args {
		if a == "--resume" && resolvedResumeID == "" {
			// Resume most recent session.
			recent, err := session.MostRecentForCWD(sessDir, rootDir)
			if err == nil && recent != "" {
				resolvedResumeID = recent
			}
			break
		}
	}

	cfgUI := tui.Config{
		Provider:            rt.ProviderName,
		Model:               rt.Model,
		WorkspaceRoot:       rootDir,
		ConfigPath:          configPath,
		MemoryPath:          resolvedMemoryPath,
		StateDir:            rt.StateDir,
		SessionDir:          sessDir,
		ResumeID:            resolvedResumeID,
		RequestTimeout:      *requestTimeout,
		StreamRunner:        rt.StreamRunner,
		HookDispatcher:      rt.HookDispatcher,
		Skills:              rt.Skills,
		Memory:              rt.Memory,
		AgentControl:        rt.AgentControl,
		AskUserBridge:       askBridge,
		ProcessManager:      rt.ProcessManager,
		Toolkit:             rt.Toolkit,
		BaseSystemPrompt:    rt.BaseSystemPrompt,
		CoordinatorPreamble: rt.CoordinatorPreamble,
	}
	if rt.Toolkit != nil {
		cfgUI.OnSessionID = rt.SetSessionID
	}
	var cleanupSummary processruntime.CleanupResult
	defer func() {
		if rt.AgentControl != nil {
			_ = rt.AgentControl.CleanupSession()
		}
	}()
	if err := tui.Run(cfgUI); err != nil {
		return err
	}
	if rt.ProcessManager != nil {
		result, err := rt.ProcessManager.CleanupSessionWithResult()
		if err != nil {
			return err
		}
		cleanupSummary = result
	}
	if len(cleanupSummary.Cleaned) > 0 {
		fmt.Println()
		fmt.Printf("Cleaned up %d session process(es):\n", len(cleanupSummary.Cleaned))
		for _, proc := range cleanupSummary.Cleaned {
			fmt.Printf("  - %s (%s)\n", proc.Command, proc.ID)
		}
	}
	return nil
}

func runAppServer(args []string) error {
	fs := flag.NewFlagSet("app-server", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	providerName := fs.String("provider", "", "provider name in config")
	modelOverride := fs.String("model", "", "model override")
	workdir := fs.String("workdir", "", "workspace directory")
	noTools := fs.Bool("no-tools", false, "disable local tools")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}
	homeDir := os.Getenv("HOME")
	cfg, configPath, err := config.LoadFrom(rootDir, homeDir)
	if err != nil {
		return err
	}

	rt, err := runtime.NewSession(runtime.Options{
		RootDir:       rootDir,
		HomeDir:       homeDir,
		ConfigPath:    configPath,
		Config:        cfg,
		ProviderName:  *providerName,
		ModelOverride: *modelOverride,
		NoTools:       *noTools,
	})
	if err != nil {
		return err
	}
	defer func() {
		_, _ = rt.Cleanup()
	}()

	return appserver.RunStdio(context.Background(), rt, os.Stdin, os.Stdout)
}

func resolveTUIThemeMode(homeDir, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if strings.TrimSpace(homeDir) == "" {
		return "auto", nil
	}
	globalCfg, err := config.LoadGlobalConfig(homeDir)
	if err != nil {
		return "", fmt.Errorf("load global preferences: %w", err)
	}
	resolvedTheme := strings.TrimSpace(globalCfg.Theme)
	if resolvedTheme == "" {
		return "auto", nil
	}
	return resolvedTheme, nil
}

func resolveWorkdir(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get current directory: %w", err)
		}
		return cwd, nil
	}

	abs, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("resolve workdir: %w", err)
	}
	return abs, nil
}

func isCodexModelsProvider(providerType string) bool {
	s := strings.ToLower(strings.TrimSpace(providerType))
	s = strings.ReplaceAll(s, "_", "-")
	return s == "openai-codex" || s == "codex-subscription" || s == "chatgpt-codex"
}

func explicitProviderAPIKey(provider config.ProviderConfig) string {
	if key := strings.TrimSpace(provider.APIKey); key != "" {
		return key
	}
	if envKey := strings.TrimSpace(provider.APIKeyEnv); envKey != "" {
		return strings.TrimSpace(os.Getenv(envKey))
	}
	return ""
}

func resolveRuntimePath(rootDir, input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}
	if filepath.IsAbs(value) {
		return value, nil
	}
	return filepath.Join(rootDir, value), nil
}

func resolvePrompt(args []string) (string, error) {
	if len(args) > 0 {
		prompt := strings.TrimSpace(strings.Join(args, " "))
		if prompt != "" {
			return prompt, nil
		}
	}

	if !stdinHasInput() {
		return "", errors.New("prompt is required (pass text or pipe stdin)")
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	prompt := strings.TrimSpace(string(data))
	if prompt == "" {
		return "", errors.New("prompt is empty")
	}
	return prompt, nil
}

func stdinHasInput() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

func runOnboarding() (tui.OnboardingResult, error) {
	m := tui.NewOnboardingModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return tui.OnboardingResult{}, fmt.Errorf("onboarding: %w", err)
	}
	om, ok := finalModel.(tui.OnboardingModel)
	if !ok {
		return tui.OnboardingResult{}, fmt.Errorf("unexpected model type")
	}
	return om.Result(), nil
}

func writeOnboardingResult(rootDir, home string, r tui.OnboardingResult) error {
	// 1. Save API key to global auth store.
	providerName := r.ProviderType
	if providerName == "openai-compatible" {
		providerName = "custom"
	}
	if strings.TrimSpace(r.APIKey) != "" {
		if err := config.SaveAuthKey(home, providerName, r.APIKey); err != nil {
			return fmt.Errorf("save auth key: %w", err)
		}
	}

	// 2. Write .wuu.json (no API key stored in project config).
	cfg := config.Default()
	cfg.DefaultProvider = providerName
	cfg.Providers = map[string]config.ProviderConfig{
		providerName: {
			Type:    r.ProviderType,
			BaseURL: r.BaseURL,
			Model:   r.Model,
		},
	}
	if r.ProviderType == "openai-codex" {
		p := cfg.Providers[providerName]
		p.WireAPI = "responses"
		cfg.Providers[providerName] = p
	}
	configPath := filepath.Join(rootDir, ".wuu.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// 3. Save global preferences.
	gc := config.GlobalConfig{
		Theme:                  r.Theme,
		HasCompletedOnboarding: true,
	}
	return config.SaveGlobalConfig(home, gc)
}

func printUsage() {
	fmt.Println(`wuu - coding agent CLI (MVP)

Usage:
  wuu init [--force]
  wuu models [flags]
  wuu run [flags] "your coding task"
  wuu eval [flags]
  wuu tui [flags]
  wuu app-server [flags]
  wuu version [--long|--json]

Models flags:
  --provider        provider name from config
  --workdir         workspace directory
  --json            output model metadata as JSON

Run flags:
  --provider        provider name from config
  --model           model override
  --max-steps       max tool loop steps
  --temperature     temperature override
  --system-prompt   system prompt override
  --workdir         workspace directory
  --no-tools        disable local tools
  --timeout         total timeout (default 10m)

Eval flags:
  --provider        provider name from config
  --model           model override
  --workdir         workspace directory containing wuu config
  --task            task id, comma-separated ids, or all
  --list            list built-in eval tasks
  --json            output eval report as JSON
  --output          write eval report JSON to path
  --max-steps       max tool loop steps per task
  --timeout         timeout per task (default 10m)
  --keep-workdirs   keep temporary task workdirs

TUI flags:
  --provider        provider name from config
  --model           model override
  --theme           theme override: auto|dark|light
  --max-steps       max tool loop steps
  --temperature     temperature override
  --system-prompt   system prompt override
  --workdir         workspace directory
  --no-tools        disable local tools
  --memory-file     session memory file path
  --request-timeout turn timeout (default disabled)

App server flags:
  --provider        provider name from config
  --model           model override
  --workdir         workspace directory
  --no-tools        disable local tools`)
}
