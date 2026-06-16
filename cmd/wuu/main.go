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

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/appserver"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/providers/codex"
	"github.com/blueberrycongee/wuu/internal/runtime"
	sessionid "github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/sessiontrace"
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
		printUsage()
		return nil
	}

	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "models":
		return runModels(args[1:])
	case "run":
		return runTask(args[1:])
	case "probe-title":
		return runProbeTitle(args[1:])
	case "eval":
		return runEval(args[1:])
	case "goal":
		return runGoal(args[1:])
	case "loop":
		return runLoop(args[1:])
	case "tui":
		return errors.New("the TUI has been removed; use the desktop GUI or `wuu run` for one-shot CLI tasks")
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
		return fmt.Errorf("unknown command %q", args[0])
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

	cfg := config.Default()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Printf("created %s\n", configPath)
	return nil
}

func runProbeTitle(args []string) error {
	fs := flag.NewFlagSet("probe-title", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	workdir := fs.String("workdir", "", "workspace directory (default: cwd)")
	threadID := fs.String("thread", "", "thread id to regenerate title for (default: most recent)")
	userPrompt := fs.String("user-prompt", "", "synthetic first user message; probe runs in dry-run mode")
	providerName := fs.String("provider", "", "override provider name from config")
	modelOverride := fs.String("model", "", "override model from config")
	dryRun := fs.Bool("dry-run", false, "do not persist the title")
	verbose := fs.Bool("verbose", true, "print every step in human-readable mode")
	jsonOut := fs.Bool("json", false, "emit TitleGenerationResult as JSON")
	quiet := fs.Bool("quiet", false, "suppress human-readable summary; implies --json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *quiet {
		*jsonOut = true
	}

	if *userPrompt == "" && *threadID == "" && *dryRun == false {
		// Default to dry-run for the implicit "use most recent thread" path
		// so an accidental invocation cannot overwrite a real title without
		// intent. Pass --dry-run=false to persist.
		*dryRun = true
	}

	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}
	homeDir := os.Getenv("HOME")

	opts := appserver.ProbeTitleOptions{
		WorkDir:       rootDir,
		HomeDir:       homeDir,
		ProviderName:  *providerName,
		ModelOverride: *modelOverride,
		ThreadID:      *threadID,
		UserPrompt:    *userPrompt,
		DryRun:        *dryRun,
		Verbose:       *verbose,
		JSON:          *jsonOut,
	}
	_, err = appserver.ProbeTitle(context.Background(), opts)
	if err != nil {
		// TitleGenerationResult is already pretty-printed or JSON-encoded by
		// ProbeTitle itself. We only need to surface the error to the shell.
		return err
	}
	return nil
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

	rt, err := runtime.NewSession(runtime.Options{
		RootDir:       rootDir,
		HomeDir:       os.Getenv("HOME"),
		ConfigPath:    configPath,
		Config:        cfg,
		ProviderName:  *providerName,
		ModelOverride: *modelOverride,
		NoTools:       *noTools,
	})
	if err != nil {
		return err
	}
	defer rt.Cleanup()
	cliSessionID := "cli-" + sessionid.NewID()
	rt.SetSessionID(cliSessionID)

	runner := rt.StreamRunner
	if runner == nil {
		return errors.New("stream runner is not configured")
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

	toolRecordStart := 0
	if rt.Toolkit != nil {
		toolRecordStart = len(rt.Toolkit.ToolTelemetry())
	}
	var contextRequests []sessiontrace.RequestContextRecord
	baseOnRequestContext := runner.OnRequestContext
	runner.OnRequestContext = func(info agent.RequestContextInfo) {
		if baseOnRequestContext != nil {
			baseOnRequestContext(info)
		}
		contextRequests = append(contextRequests, sessionTraceRequestContext(info))
	}
	defer func() {
		runner.OnRequestContext = baseOnRequestContext
	}()
	startedAt := time.Now().UTC()
	history := cliRunInitialHistory(runner, prompt)
	res, err := runner.RunWithCallback(ctx, history, runner.OnEvent)
	completedAt := time.Now().UTC()
	tracePath, traceErr := persistCLIRunTrace(rt, runner, cliSessionID, startedAt, completedAt, res, err, toolRecordStart, contextRequests)
	if traceErr != nil {
		fmt.Fprintf(os.Stderr, "warning: write session trace: %v\n", traceErr)
	}
	if err != nil {
		return err
	}

	fmt.Printf("provider: %s\nmodel: %s\nconfig: %s\n", rt.ProviderName, rt.Model, configPath)
	if tracePath != "" {
		fmt.Printf("trace_path: %s\n", tracePath)
	}
	fmt.Println()
	fmt.Println(res.Content)
	return nil
}

func cliRunInitialHistory(runner *agent.StreamRunner, prompt string) []providers.ChatMessage {
	var history []providers.ChatMessage
	if runner != nil && strings.TrimSpace(runner.SystemPrompt) != "" {
		history = append(history, providers.ChatMessage{Role: "system", Content: runner.SystemPrompt})
	}
	return append(history, providers.ChatMessage{Role: "user", Content: prompt})
}

func persistCLIRunTrace(rt *runtime.Session, runner *agent.StreamRunner, sessionID string, startedAt, completedAt time.Time, res agent.LoopResult, runErr error, toolRecordStart int, contextRequests []sessiontrace.RequestContextRecord) (string, error) {
	if rt == nil || rt.Toolkit == nil {
		return "", nil
	}
	tracePath := sessiontrace.Path(rt.Toolkit.SessionDir())
	if strings.TrimSpace(tracePath) == "" {
		return "", nil
	}
	status := "completed"
	errorText := ""
	if runErr != nil {
		status = "failed"
		errorText = runErr.Error()
	}
	durationMS := completedAt.Sub(startedAt).Milliseconds()
	model := ""
	apiModel := ""
	if runner != nil {
		model = runner.Model
		apiModel = runner.APIModel
	}
	turn := sessiontrace.TurnRecord{
		ThreadID:         sessionID,
		TurnID:           sessionID + "-turn-1",
		Status:           status,
		ProviderName:     rt.ProviderName,
		Model:            model,
		APIModel:         apiModel,
		ModelProfile:     sessiontrace.NewModelProfileRecord(rt.ProviderName, model, apiModel),
		StartedAt:        &startedAt,
		CompletedAt:      &completedAt,
		DurationMS:       &durationMS,
		InputTokens:      res.InputTokens,
		OutputTokens:     res.OutputTokens,
		HistoryRewritten: res.HistoryRewritten,
		Error:            errorText,
	}
	final := sessiontrace.FinalRecord{
		Status:             status,
		InputTokens:        res.InputTokens,
		OutputTokens:       res.OutputTokens,
		FinalAnswerPreview: res.Content,
		Error:              errorText,
	}
	records := rt.Toolkit.ToolTelemetry()
	if toolRecordStart > 0 && toolRecordStart < len(records) {
		records = records[toolRecordStart:]
	} else if toolRecordStart >= len(records) {
		records = nil
	}
	if err := sessiontrace.AppendTurn(tracePath, turn, final, rt.Toolkit.ToolInfos(), records, contextRequests); err != nil {
		return "", err
	}
	return tracePath, nil
}

func sessionTraceRequestContext(info agent.RequestContextInfo) sessiontrace.RequestContextRecord {
	return sessiontrace.RequestContextRecord{
		StepIndex:         info.StepIndex,
		TransientMessages: info.TransientMessages,
		ContentBytes:      info.ContentBytes,
		BlockKinds:        append([]string(nil), info.BlockKinds...),
	}
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
	cfg, configPath, err := loadOrCreateAppServerConfig(rootDir, homeDir)
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
	if err := rt.StartCronScheduler(); err != nil {
		_, _ = rt.Cleanup()
		return err
	}
	defer func() {
		_, _ = rt.Cleanup()
	}()

	return appserver.RunStdio(context.Background(), rt, os.Stdin, os.Stdout)
}

func loadOrCreateAppServerConfig(rootDir, homeDir string) (config.Config, string, error) {
	cfg, configPath, err := config.LoadFrom(rootDir, homeDir)
	if err == nil {
		return cfg, configPath, nil
	}
	if !errors.Is(err, config.ErrConfigNotFound) {
		return config.Config{}, "", err
	}

	configPath = filepath.Join(rootDir, ".wuu.json")
	if strings.TrimSpace(homeDir) != "" {
		configPath = filepath.Join(homeDir, ".config", "wuu", "config.json")
	}
	cfg = appServerStarterConfig()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return config.Config{}, "", err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return config.Config{}, "", fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(configPath, append(data, '\n'), 0o600); err != nil {
		return config.Config{}, "", fmt.Errorf("write starter config: %w", err)
	}
	return cfg, configPath, nil
}

func appServerStarterConfig() config.Config {
	cfg := config.Default()
	if provider, ok := cfg.Providers["openai-codex"]; ok {
		cfg.DefaultProvider = "openai-codex"
		cfg.Providers = map[string]config.ProviderConfig{
			"openai-codex": provider,
		}
	}
	return cfg
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

func printUsage() {
	fmt.Println(`wuu - GUI-first coding agent backend and CLI tools

Usage:
  wuu init [--force]
  wuu models [flags]
  wuu run [flags] "your coding task"
  wuu eval [flags]
  wuu goal demo [flags]
  wuu goal status [flags]
  wuu app-server [flags]
  wuu probe-title [flags]   run the LLM title pipeline against a real provider
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
  --replay-trace    replay an eval trace JSONL without calling a model or tools
  --live-codex-oauth
                   run live MCP E2E with local Codex OAuth

Goal flags:
  demo --workdir DIR --goal TEXT [--id ID] [--verify-command CMD]
                   write a durable demo goal under Wuu workspace state
  status --workdir DIR [--id ID] [--json]
                   read goal state from Wuu workspace state

App server flags:
  --provider        provider name from config
  --model           model override
  --workdir         workspace directory
  --no-tools        disable local tools

Probe-title flags:
  --workdir         workspace directory (default: cwd)
  --thread          thread id to regenerate title for (default: most recent)
  --user-prompt     synthetic first user message; auto dry-run
  --provider        override provider from config
  --model           override model from config
  --dry-run         do not persist the title
  --verbose         print every step in human-readable mode
  --json            emit the result struct as JSON
  --quiet           suppress human-readable summary`)
}
