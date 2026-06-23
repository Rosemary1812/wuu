package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/stringutil"
)

const maxShellTailBytes = 8 * 1024

// ShellTool executes non-interactive shell commands.
type ShellTool struct{ env *Env }

func NewShellTool(env *Env) *ShellTool { return &ShellTool{env: env} }

func (t *ShellTool) Name() string            { return "run_shell" }
func (t *ShellTool) IsReadOnly() bool        { return false }
func (t *ShellTool) IsConcurrencySafe() bool { return false }

func (t *ShellTool) Classify(argsJSON string) ToolClassification {
	var args struct {
		Command string `json:"command"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskHigh,
			Reason:          "invalid shell invocation",
		}
	}
	return classifyShellCommand(args.Command)
}

func (t *ShellTool) ValidateInput(argsJSON string) error {
	var args struct {
		Command string `json:"command"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	if strings.TrimSpace(args.Command) == "" {
		return errors.New("run_shell requires command")
	}
	return nil
}

func (t *ShellTool) PermissionRequests(argsJSON string) []ToolPermissionRequest {
	var args struct {
		Command string `json:"command"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return nil
	}
	return []ToolPermissionRequest{shellPermissionRequest("run_shell", args.Command)}
}

func (t *ShellTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "run_shell",
		Description: "Executes a bash command in the workspace and returns its output.\n\n" +
			"The working directory is the workspace root. Shell state does not persist between calls.\n\n" +
			"IMPORTANT: Avoid using this tool to run `cat`, `head`, `tail`, `grep`, `find`, " +
			"`sed`, `awk`, or `echo` when a dedicated tool exists. Use read_file instead of cat, " +
			"grep tool instead of grep/rg, glob instead of find, edit_file instead of sed.\n\n" +
			"Instructions:\n" +
			"- Commands must be non-interactive; never rely on editors, pagers, or terminal prompts\n" +
			"- Default timeout is 300s, max 3600s\n" +
			"- Results include exit_code, duration_ms, workspace_revision, compact combined output, stdout/stderr tails, and full_log_ref when session artifacts are available\n" +
			"- If commands are independent, make multiple tool calls in parallel\n" +
			"- If commands depend on each other, chain them with '&&'\n" +
			"- Git commands are supported for normal non-interactive workflows: inspect with git status/diff/log, stage explicit paths, commit with explicit non-interactive messages (-m/--message or -F/--file), and push only when the user explicitly requested a remote write. Unsafe git forms such as broad staging, config mutation, force push, hook skipping, destructive reset/clean/checkout, and interactive git are rejected by command policy unless full access is active.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command to execute. Must be non-interactive; never rely on editors, pagers, or terminal prompts.",
				},
				"timeout_seconds": map[string]any{
					"type":        "integer",
					"description": "Max runtime in seconds (1-3600).",
				},
				"purpose": map[string]any{
					"type":        "string",
					"description": "Why this command is needed. Stored in redacted logs for replay and audit.",
				},
			},
			"required": []string{"command"},
		},
	}
}

func (t *ShellTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
		Purpose        string `json:"purpose"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if len(args.Command) == 0 || len(bytes.TrimSpace([]byte(args.Command))) == 0 {
		return "", errors.New("run_shell requires command")
	}
	if !t.env.BypassToolHardProtections() {
		if reason, ok := blockedShellGitCommandReason(args.Command); ok {
			return "", fmt.Errorf("run_shell refuses unsafe git command (%s). Use safe non-interactive git commands such as git status/diff/log, explicit-path git add, or git commit with an explicit non-interactive message: error_kind=unsupported_git_shell model_next_action=%q", reason, "retry with a safe bash git command")
		}
		if shellCommandUsesUnsupportedWrapper(args.Command) {
			return "", errors.New("run_shell refuses unsupported shell wrapper syntax because it cannot prove which command will execute; retry with a plain command or a supported timeout/time/nice/nohup/stdbuf form")
		}
		if shellCommandDumpsEnvironment(args.Command) {
			return "", errors.New("run_shell refuses to print process environment variables because they may contain secrets")
		}
		if reason, ok := shellCommandSensitivePathReason(args.Command); ok {
			return "", fmt.Errorf("run_shell refuses to access sensitive paths (%s). Use dedicated metadata-safe tools or ask the user for explicit secret handling", reason)
		}
		if shellCommandInvokesDestructiveCommand(args.Command) {
			return "", errors.New("run_shell refuses to execute destructive shell commands; use the file editing tool exposed in this session or another restricted audited tool so changes remain reviewable")
		}
		if testCommandLooksLikeLocalRunnerVerification(args.Command) {
			return "", errors.New("run_shell refuses to execute package-runner verification commands directly because they can install packages when the runner is missing; use bash action=run for local verification so project-local test runners are resolved without downloads: error_kind=wrong_tool_for_verification model_next_action=\"retry with bash action=run using the same command\"")
		}
		if shellCommandInvokesPackageOrNetworkMutation(args.Command) {
			return "", errors.New("run_shell refuses to execute package, network, or external mutation commands; use dedicated web tools, project-approved verification commands, or ask the user for explicit approval")
		}
	}

	timeout := args.TimeoutSeconds
	if timeout <= 0 {
		timeout = defaultShellTimeoutSeconds
	}
	if timeout > maxShellTimeoutSeconds {
		timeout = maxShellTimeoutSeconds
	}

	result, err := executeShellCommand(ctx, t.env, args.Command, timeout)
	if err != nil {
		return "", err
	}
	result.Purpose = t.env.RedactToolOutput(args.Purpose)
	fullLogRef, fullLogBytes, fullLogErr := persistShellLog(t.env.SessionDir, result)
	if fullLogRef != "" {
		result.FullLogRef = fullLogRef
		result.FullLogBytes = fullLogBytes
	} else if fullLogErr != "" {
		result.FullLogError = fullLogErr
	}
	return mustJSON(result)
}

type shellExecutionResult struct {
	Action              string                  `json:"action"`
	Command             string                  `json:"command"`
	RequestedCommand    string                  `json:"requested_command,omitempty"`
	ResolvedCommand     string                  `json:"resolved_command,omitempty"`
	Purpose             string                  `json:"purpose,omitempty"`
	Classification      ToolClassification      `json:"classification"`
	ExitCode            int                     `json:"exit_code"`
	DurationMS          int64                   `json:"duration_ms"`
	TimedOut            bool                    `json:"timed_out"`
	Truncated           bool                    `json:"truncated"`
	Output              string                  `json:"output"`
	StdoutTail          string                  `json:"stdout_tail"`
	StderrTail          string                  `json:"stderr_tail"`
	StdoutBytes         int                     `json:"stdout_bytes"`
	StderrBytes         int                     `json:"stderr_bytes"`
	WorkspaceRevision   string                  `json:"workspace_revision"`
	StdoutTailTruncated bool                    `json:"stdout_tail_truncated"`
	StderrTailTruncated bool                    `json:"stderr_tail_truncated"`
	FullLogRef          string                  `json:"full_log_ref,omitempty"`
	FullLogBytes        int                     `json:"full_log_bytes,omitempty"`
	FullLogError        string                  `json:"full_log_error,omitempty"`
	NextSuggestions     []string                `json:"next_suggestions,omitempty"`
	Verification        *bashVerificationResult `json:"verification,omitempty"`
	redactedStdout      string
	redactedStderr      string
}

func persistShellLog(sessionDir string, shellResult shellExecutionResult) (path string, bytes int, errSummary string) {
	sessionDir = strings.TrimSpace(sessionDir)
	if sessionDir == "" {
		return "", 0, ""
	}
	dir := filepath.Join(sessionDir, "tool-results", "shell-logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, redactToolOutput(err.Error())
	}
	commandHash := sha256Hex([]byte(shellResult.Command))
	name := fmt.Sprintf("%s-%s.log", time.Now().UTC().Format("20060102T150405.000000000Z"), commandHashPrefix(commandHash))
	path = filepath.Join(dir, name)
	content := buildShellLog(shellResult)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", 0, redactToolOutput(err.Error())
	}
	return path, len(content), ""
}

func buildShellLog(shellResult shellExecutionResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "command: %s\n", shellResult.Command)
	if strings.TrimSpace(shellResult.Purpose) != "" {
		fmt.Fprintf(&b, "purpose: %s\n", shellResult.Purpose)
	}
	fmt.Fprintf(&b, "exit_code: %d\n", shellResult.ExitCode)
	fmt.Fprintf(&b, "duration_ms: %d\n", shellResult.DurationMS)
	fmt.Fprintf(&b, "timed_out: %t\n", shellResult.TimedOut)
	fmt.Fprintf(&b, "stdout_bytes: %d\n", shellResult.StdoutBytes)
	fmt.Fprintf(&b, "stderr_bytes: %d\n", shellResult.StderrBytes)
	fmt.Fprintf(&b, "workspace_revision: %s\n\n", shellResult.WorkspaceRevision)
	b.WriteString("--- stdout (redacted) ---\n")
	b.WriteString(shellResult.redactedStdout)
	if !strings.HasSuffix(shellResult.redactedStdout, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n--- stderr (redacted) ---\n")
	b.WriteString(shellResult.redactedStderr)
	if !strings.HasSuffix(shellResult.redactedStderr, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

func executeShellCommand(ctx context.Context, env *Env, command string, timeoutSeconds int) (shellExecutionResult, error) {
	return executeShellCommandWithCWD(ctx, env, command, timeoutSeconds, "")
}

func executeShellCommandWithCWD(ctx context.Context, env *Env, command string, timeoutSeconds int, cwd string) (shellExecutionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if env == nil {
		env = &Env{}
	}
	workDir, err := resolveShellWorkingDir(env, cwd)
	if err != nil {
		return shellExecutionResult{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	cmd.Dir = workDir
	cmd.Env = mergeEnv(os.Environ(), nonInteractiveShellEnv())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startedAt := time.Now()
	err = cmd.Run()
	durationMS := time.Since(startedAt).Milliseconds()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			exitCode = 124
		} else {
			return shellExecutionResult{}, fmt.Errorf("run command: %w", err)
		}
	}

	stdoutText := stdout.String()
	stderrText := stderr.String()
	redactedStdout := env.RedactToolOutput(stdoutText)
	redactedStderr := env.RedactToolOutput(stderrText)
	redactedCommand := env.RedactToolOutput(command)
	output := redactedStdout + redactedStderr
	trimmed, truncated := truncate(output, maxShellOutputBytes)
	stdoutTail, stdoutTailTruncated := tailString(redactedStdout, maxShellTailBytes)
	stderrTail, stderrTailTruncated := tailString(redactedStderr, maxShellTailBytes)
	classification := classifyShellCommand(command)
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)

	return shellExecutionResult{
		Action:              "run",
		Command:             redactedCommand,
		Classification:      classification,
		ExitCode:            exitCode,
		DurationMS:          durationMS,
		TimedOut:            timedOut,
		Truncated:           truncated,
		Output:              trimmed,
		StdoutTail:          stdoutTail,
		StderrTail:          stderrTail,
		StdoutBytes:         len(stdoutText),
		StderrBytes:         len(stderrText),
		WorkspaceRevision:   workspaceRevision(ctx, env.RootDir),
		StdoutTailTruncated: stdoutTailTruncated,
		StderrTailTruncated: stderrTailTruncated,
		NextSuggestions:     shellNextSuggestions(exitCode, timedOut, classification),
		redactedStdout:      redactedStdout,
		redactedStderr:      redactedStderr,
	}, nil
}

func resolveShellWorkingDir(env *Env, cwd string) (string, error) {
	if env == nil {
		env = &Env{}
	}
	if strings.TrimSpace(env.RootDir) == "" {
		if strings.TrimSpace(cwd) == "" {
			return "", nil
		}
		return "", fmt.Errorf("working directory %q requires workspace root", cwd)
	}
	workDir, err := env.ResolvePath(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve working directory %q: %w", cwd, err)
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("resolve working directory %q: %w", workDir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("working directory %q does not exist", abs)
		}
		return "", fmt.Errorf("inspect working directory %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory %q is not a directory", abs)
	}
	return abs, nil
}

func shellNextSuggestions(exitCode int, timedOut bool, classification ToolClassification) []string {
	if timedOut {
		return []string{"if this was a dev server, watch mode, or other long-lived command, rerun it with bash action=start_background and inspect it with bash action=read_background; otherwise narrow the command scope or set a bounded timeout only when necessary"}
	}
	if exitCode != 0 {
		return []string{"inspect the redacted stdout/stderr tails and full_log_ref when present, then retry the corrected command with bash action=run"}
	}
	if classification.ReadOnly {
		return []string{"use the returned observation as evidence for the next action"}
	}
	return []string{"inspect git diff or relevant artifacts before continuing"}
}

func tailString(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value, false
	}
	return stringutil.HeadTail(value, 0, maxBytes, ""), true
}

func shellCommandLooksReadOnly(command string) bool {
	return classifyShellCommand(command).ReadOnly
}

func classifyShellCommand(command string) ToolClassification {
	command = strings.TrimSpace(command)
	if command == "" {
		return highRiskShellClassification("empty shell command", false)
	}
	hasShellMetacharacters := strings.ContainsAny(command, "\n;&|><`$()")
	rawFields := strings.Fields(command)
	if len(rawFields) == 0 {
		return highRiskShellClassification("empty shell command", false)
	}
	fields := stripLeadingShellEnvAssignments(rawFields)
	if len(fields) == 0 {
		return highRiskShellClassification("environment-only shell command", false)
	}
	if shellFieldsDumpEnvironment(command, fields) {
		return highRiskShellClassification("shell command may expose environment secrets", false)
	}
	if shellFieldsTouchSensitivePath(fields) {
		return highRiskShellClassification("shell command may read secrets", false)
	}
	if shellCommandUsesUnsupportedWrapper(command) {
		return highRiskShellClassification("unsupported shell wrapper command", false)
	}
	envFields := normalizeShellCommandFieldsPreservingEnv(fields)
	if shellFieldsDumpEnvironment(command, envFields) {
		return highRiskShellClassification("shell command may expose environment secrets", false)
	}
	fields = normalizeShellCommandFields(fields)
	if len(fields) == 0 {
		return highRiskShellClassification("wrapper-only shell command", false)
	}
	if shellFieldsDumpEnvironment(command, fields) {
		return highRiskShellClassification("shell command may expose environment secrets", false)
	}
	if shellFieldsTouchSensitivePath(fields) {
		return highRiskShellClassification("shell command may read secrets", false)
	}
	if classification, ok := classifyShellGitCommand(command); ok {
		return classification
	}
	if shellCommandInvokesDestructiveCommand(command) {
		return highRiskShellClassification("destructive shell command", true)
	}
	if hasShellMetacharacters {
		if shellCommandLooksLikeDirectoryScopedVerification(command) {
			return ToolClassification{
				ReadOnly:        false,
				ConcurrencySafe: false,
				Risk:            ToolRiskMedium,
				Reason:          "local verification command",
			}
		}
		return highRiskShellClassification("shell command uses shell metacharacters", false)
	}
	switch fields[0] {
	case "pwd", "ls", "cat", "head", "tail", "wc", "file", "stat", "du",
		"rg", "grep":
		return ToolClassification{
			ReadOnly:        true,
			ConcurrencySafe: true,
			Risk:            ToolRiskLow,
			Reason:          "simple read-only shell command",
		}
	}
	if shellFieldsLookLikeVerification(fields) {
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskMedium,
			Reason:          "local verification command",
		}
	}
	if shellFieldsLookLikePackageOrNetworkMutation(fields) {
		return highRiskShellClassification("package, network, or external mutation command", false)
	}
	return highRiskShellClassification("shell command is not proven read-only or verification-only", false)
}

func shellCommandInvokesGit(command string) bool {
	for _, segment := range splitShellCommandSegments(command) {
		fields := normalizeShellCommandFields(strings.Fields(segment))
		if len(fields) > 0 && shellCommandBaseName(fields[0]) == "git" {
			return true
		}
	}
	return false
}

func blockedShellGitCommandReason(command string) (string, bool) {
	if !shellCommandInvokesGit(command) {
		return "", false
	}
	classification, ok := classifyShellGitCommand(command)
	if !ok {
		return "unsupported git shell shape", true
	}
	if classification.Destructive {
		return classification.Reason, true
	}
	if classification.Risk == ToolRiskHigh && classification.Reason == "unsupported git shell command" {
		return classification.Reason, true
	}
	return "", false
}

func classifyShellGitCommand(command string) (ToolClassification, bool) {
	if !shellCommandInvokesGit(command) {
		return ToolClassification{}, false
	}
	segments, ok := splitShellCommandSegmentsQuoted(command)
	if !ok {
		return highRiskShellClassification("unsupported git shell command", false), true
	}
	sawGit := false
	readOnly := true
	concurrencySafe := true
	risk := ToolRiskLow
	reason := "git shell read-only command"
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		fields, ok := splitShellFields(segment)
		if !ok {
			return highRiskShellClassification("unsupported git shell command", false), true
		}
		fields = normalizeShellCommandFields(fields)
		if len(fields) == 0 {
			continue
		}
		if shellCommandBaseName(fields[0]) == "cd" {
			if len(fields) == 2 && safeRelativeShellDir(fields[1]) {
				continue
			}
			return highRiskShellClassification("unsupported git shell command", false), true
		}
		if shellCommandBaseName(fields[0]) != "git" {
			return highRiskShellClassification("unsupported git shell command", false), true
		}
		fields[0] = "git"
		sawGit = true
		segmentClass := classifyShellGitFields(fields)
		if segmentClass.Destructive {
			return segmentClass, true
		}
		if !segmentClass.ReadOnly {
			readOnly = false
			concurrencySafe = false
		}
		if riskRank(segmentClass.Risk) > riskRank(risk) {
			risk = segmentClass.Risk
			reason = segmentClass.Reason
		}
	}
	if !sawGit {
		return highRiskShellClassification("unsupported git shell command", false), true
	}
	return ToolClassification{
		ReadOnly:        readOnly,
		ConcurrencySafe: concurrencySafe,
		Destructive:     false,
		Risk:            risk,
		Reason:          reason,
	}, true
}

func classifyShellGitFields(fields []string) ToolClassification {
	if len(fields) > 1 && oneOf(fields[1], "reset", "clean", "checkout", "switch", "merge", "rebase") {
		return highRiskShellClassification("destructive git shell command", true)
	}
	subcmd, args, err := parseShellGitSubcommand(fields)
	if err != nil {
		return highRiskShellClassification("unsupported git shell command", false)
	}
	switch subcmd {
	case "add":
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskMedium,
			Reason:          "git shell add writes the repository index",
		}
	case "restore --staged":
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskMedium,
			Reason:          "git shell restore --staged writes the repository index",
		}
	case "commit":
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskMedium,
			Reason:          "git shell commit writes local repository history",
		}
	case "push":
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskHigh,
			Reason:          "git shell push writes remote branch state",
		}
	case "ls-remote":
		return ToolClassification{
			ReadOnly:        true,
			ConcurrencySafe: true,
			Risk:            ToolRiskMedium,
			Reason:          "git shell ls-remote may contact a remote",
		}
	default:
		_ = args
		return ToolClassification{
			ReadOnly:        true,
			ConcurrencySafe: true,
			Risk:            ToolRiskLow,
			Reason:          "git shell read-only command",
		}
	}
}

func parseShellGitSubcommand(fields []string) (string, []string, error) {
	if len(fields) < 2 || fields[0] != "git" {
		return "", nil, errors.New("not a git command")
	}
	subcmd := strings.TrimSpace(fields[1])
	remainingArgs := append([]string(nil), fields[2:]...)
	if subcmd == "" || strings.HasPrefix(subcmd, "-") {
		return "", nil, fmt.Errorf("git shell global options are not supported")
	}
	if !allowedGitSubcommands[subcmd] && policiedSubcommands[subcmd] == nil && len(remainingArgs) > 0 {
		combined := subcmd + " " + remainingArgs[0]
		if allowedGitSubcommands[combined] || policiedSubcommands[combined] != nil {
			subcmd = combined
			remainingArgs = remainingArgs[1:]
		}
	}
	if err := validateGlobalGitArgs(subcmd, remainingArgs); err != nil {
		return "", nil, err
	}
	if policy := policiedSubcommands[subcmd]; policy != nil {
		if err := validateSubcommandFlags(subcmd, policy, remainingArgs); err != nil {
			return "", nil, err
		}
	} else if allowedGitSubcommands[subcmd] {
		if err := validateGitArgs(subcmd, remainingArgs, false); err != nil {
			return "", nil, err
		}
	} else {
		return "", nil, fmt.Errorf("git shell subcommand %q is not allowed", subcmd)
	}
	if err := validateSensitiveGitContentArgs(subcmd, remainingArgs, false); err != nil {
		return "", nil, err
	}
	return subcmd, remainingArgs, nil
}

func riskRank(risk ToolRisk) int {
	switch risk {
	case ToolRiskHigh:
		return 3
	case ToolRiskMedium:
		return 2
	case ToolRiskLow:
		return 1
	default:
		return 0
	}
}

func splitShellCommandSegmentsQuoted(command string) ([]string, bool) {
	var segments []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\\' && !inSingle:
			if i+1 >= len(runes) {
				return nil, false
			}
			b.WriteRune(r)
			i++
			b.WriteRune(runes[i])
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			b.WriteRune(r)
		case r == '"' && !inSingle:
			inDouble = !inDouble
			b.WriteRune(r)
		case !inSingle && !inDouble && strings.ContainsRune("\n;&|()`", r):
			segments = append(segments, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	if inSingle || inDouble {
		return nil, false
	}
	segments = append(segments, b.String())
	return segments, true
}

func splitShellFields(segment string) ([]string, bool) {
	var fields []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	haveField := false
	flush := func() {
		if !haveField {
			return
		}
		fields = append(fields, b.String())
		b.Reset()
		haveField = false
	}
	runes := []rune(segment)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\\' && !inSingle:
			if i+1 >= len(runes) {
				return nil, false
			}
			i++
			b.WriteRune(runes[i])
			haveField = true
		case r == '\'' && !inDouble:
			inSingle = !inSingle
			haveField = true
		case r == '"' && !inSingle:
			inDouble = !inDouble
			haveField = true
		case unicode.IsSpace(r) && !inSingle && !inDouble:
			flush()
		default:
			b.WriteRune(r)
			haveField = true
		}
	}
	if inSingle || inDouble {
		return nil, false
	}
	flush()
	return fields, true
}

func shellCommandInvokesDestructiveCommand(command string) bool {
	for _, segment := range splitShellCommandSegments(command) {
		fields := normalizeShellCommandFields(strings.Fields(segment))
		if shellFieldsLookDestructive(fields) {
			return true
		}
	}
	return false
}

func shellCommandInvokesPackageOrNetworkMutation(command string) bool {
	for _, segment := range splitShellCommandSegments(command) {
		fields := normalizeShellCommandFields(strings.Fields(segment))
		if shellFieldsLookLikePackageOrNetworkMutation(fields) {
			return true
		}
	}
	return false
}

func shellCommandUsesUnsupportedWrapper(command string) bool {
	segments, ok := splitShellCommandSegmentsQuoted(command)
	if !ok {
		segments = splitShellCommandSegments(command)
	}
	if len(segments) == 0 {
		segments = []string{command}
	}
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		fields, ok := splitShellFields(segment)
		if !ok {
			fields = strings.Fields(segment)
		}
		if shellFieldsUseUnsupportedWrapper(fields) {
			return true
		}
	}
	return false
}

func shellFieldsUseUnsupportedWrapper(fields []string) bool {
	fields = stripLeadingShellEnvAssignments(fields)
	for len(fields) > 0 {
		switch shellCommandBaseName(fields[0]) {
		case "command", "exec":
			fields = stripLeadingShellEnvAssignments(fields[1:])
		case "env":
			fields = stripLeadingShellEnvAssignments(stripEnvCommandPrefix(fields[1:]))
		default:
			if !shellWrapperCommandName(fields[0]) {
				return false
			}
			stripped, ok := stripSafeShellWrapperPrefix(fields)
			if !ok {
				return true
			}
			fields = stripLeadingShellEnvAssignments(stripped)
		}
	}
	return false
}

func normalizeShellCommandFields(fields []string) []string {
	fields = stripLeadingShellEnvAssignments(fields)
	for len(fields) > 0 {
		switch shellCommandBaseName(fields[0]) {
		case "command", "exec":
			fields = fields[1:]
		case "env":
			fields = stripEnvCommandPrefix(fields[1:])
		default:
			stripped, ok := stripSafeShellWrapperPrefix(fields)
			if !ok {
				return fields
			}
			fields = stripped
		}
		fields = stripLeadingShellEnvAssignments(fields)
	}
	return fields
}

func normalizeShellCommandFieldsPreservingEnv(fields []string) []string {
	fields = stripLeadingShellEnvAssignments(fields)
	for len(fields) > 0 {
		switch shellCommandBaseName(fields[0]) {
		case "command", "exec":
			fields = fields[1:]
		case "env":
			return fields
		default:
			stripped, ok := stripSafeShellWrapperPrefix(fields)
			if !ok {
				return fields
			}
			fields = stripped
		}
		fields = stripLeadingShellEnvAssignments(fields)
	}
	return fields
}

func stripLeadingShellEnvAssignments(fields []string) []string {
	for len(fields) > 0 && looksLikeEnvAssignment(fields[0]) {
		fields = fields[1:]
	}
	return fields
}

func stripSafeShellWrapperPrefix(fields []string) ([]string, bool) {
	if len(fields) == 0 {
		return fields, false
	}
	switch shellCommandBaseName(fields[0]) {
	case "timeout":
		i := timeoutWrappedCommandIndex(fields)
		if i < 0 {
			return fields, false
		}
		return fields[i:], true
	case "time":
		i := timeWrappedCommandIndex(fields)
		if i < 0 {
			return fields, false
		}
		return fields[i:], true
	case "nice":
		i := niceWrappedCommandIndex(fields)
		if i < 0 {
			return fields, false
		}
		return fields[i:], true
	case "nohup":
		i := 1
		if len(fields) > i && fields[i] == "--" {
			i++
		}
		if i >= len(fields) || strings.HasPrefix(fields[i], "-") {
			return fields, false
		}
		return fields[i:], true
	case "stdbuf":
		i := stdbufWrappedCommandIndex(fields)
		if i < 0 {
			return fields, false
		}
		return fields[i:], true
	default:
		return fields, false
	}
}

func shellWrapperCommandName(name string) bool {
	switch shellCommandBaseName(name) {
	case "timeout", "time", "nice", "nohup", "stdbuf":
		return true
	default:
		return false
	}
}

func shellCommandBaseName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.Contains(name, "/") {
		return filepath.Base(name)
	}
	return name
}

func timeoutWrappedCommandIndex(fields []string) int {
	i := 1
	for i < len(fields) {
		arg := fields[i]
		switch {
		case arg == "--foreground" || arg == "--preserve-status" || arg == "--verbose" || arg == "-v":
			i++
		case strings.HasPrefix(arg, "--kill-after=") || strings.HasPrefix(arg, "--signal="):
			value := arg[strings.IndexByte(arg, '=')+1:]
			if !shellWrapperValueLooksSafe(value) {
				return -1
			}
			i++
		case arg == "--kill-after" || arg == "--signal" || arg == "-k" || arg == "-s":
			if i+1 >= len(fields) || !shellWrapperValueLooksSafe(fields[i+1]) {
				return -1
			}
			i += 2
		case strings.HasPrefix(arg, "-k") || strings.HasPrefix(arg, "-s"):
			if len(arg) <= 2 || !shellWrapperValueLooksSafe(arg[2:]) {
				return -1
			}
			i++
		case arg == "--":
			i++
			goto duration
		case strings.HasPrefix(arg, "-"):
			return -1
		default:
			goto duration
		}
	}

duration:
	if i >= len(fields) || i+1 >= len(fields) || !shellTimeoutDurationLooksSafe(fields[i]) {
		return -1
	}
	return i + 1
}

func timeWrappedCommandIndex(fields []string) int {
	i := 1
	if i < len(fields) && fields[i] == "-p" {
		i++
	}
	if i < len(fields) && fields[i] == "--" {
		i++
	}
	if i >= len(fields) || strings.HasPrefix(fields[i], "-") {
		return -1
	}
	return i
}

func niceWrappedCommandIndex(fields []string) int {
	i := 1
	if i >= len(fields) {
		return -1
	}
	switch {
	case fields[i] == "--":
		i++
	case fields[i] == "-n":
		if i+1 >= len(fields) || !shellNiceAdjustmentLooksSafe(fields[i+1]) {
			return -1
		}
		i += 2
		if i < len(fields) && fields[i] == "--" {
			i++
		}
	case shellLegacyNiceAdjustmentLooksSafe(fields[i]):
		i++
		if i < len(fields) && fields[i] == "--" {
			i++
		}
	case strings.HasPrefix(fields[i], "-"):
		return -1
	}
	if i >= len(fields) {
		return -1
	}
	return i
}

func stdbufWrappedCommandIndex(fields []string) int {
	i := 1
	consumedFlag := false
	for i < len(fields) {
		arg := fields[i]
		switch {
		case arg == "-i" || arg == "-o" || arg == "-e":
			if i+1 >= len(fields) || !shellWrapperValueLooksSafe(fields[i+1]) {
				return -1
			}
			i += 2
			consumedFlag = true
		case len(arg) > 2 && arg[0] == '-' && (arg[1] == 'i' || arg[1] == 'o' || arg[1] == 'e'):
			if !shellWrapperValueLooksSafe(arg[2:]) {
				return -1
			}
			i++
			consumedFlag = true
		case strings.HasPrefix(arg, "--input=") || strings.HasPrefix(arg, "--output=") || strings.HasPrefix(arg, "--error="):
			value := arg[strings.IndexByte(arg, '=')+1:]
			if !shellWrapperValueLooksSafe(value) {
				return -1
			}
			i++
			consumedFlag = true
		case strings.HasPrefix(arg, "-"):
			return -1
		default:
			if !consumedFlag {
				return -1
			}
			return i
		}
	}
	return -1
}

func shellWrapperValueLooksSafe(value string) bool {
	if value == "" || strings.ContainsAny(value, "\n;&|><`$()") {
		return false
	}
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		if r == '_' || r == '.' || r == '+' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func shellTimeoutDurationLooksSafe(value string) bool {
	if value == "" || strings.ContainsAny(value, "\n;&|><`$()+-") {
		return false
	}
	if last := value[len(value)-1]; last == 's' || last == 'm' || last == 'h' || last == 'd' {
		value = value[:len(value)-1]
	}
	if value == "" {
		return false
	}
	sawDigit := false
	sawDot := false
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			sawDigit = true
		case r == '.':
			if sawDot {
				return false
			}
			sawDot = true
		default:
			return false
		}
	}
	return sawDigit && value[0] != '.' && value[len(value)-1] != '.'
}

func shellNiceAdjustmentLooksSafe(value string) bool {
	if value == "" || strings.ContainsAny(value, "\n;&|><`$()+.") {
		return false
	}
	if value[0] == '-' {
		value = value[1:]
	}
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func shellLegacyNiceAdjustmentLooksSafe(value string) bool {
	return strings.HasPrefix(value, "-") && shellNiceAdjustmentLooksSafe(value)
}

func splitShellCommandSegments(command string) []string {
	return strings.FieldsFunc(command, func(r rune) bool {
		switch r {
		case '\n', ';', '&', '|', '(', ')', '`':
			return true
		default:
			return false
		}
	})
}

func stripEnvCommandPrefix(fields []string) []string {
	for len(fields) > 0 {
		field := fields[0]
		if looksLikeEnvAssignment(field) {
			fields = fields[1:]
			continue
		}
		if strings.HasPrefix(field, "-") {
			fields = fields[1:]
			if field == "-u" || field == "--unset" || field == "-C" || field == "--chdir" {
				if len(fields) > 0 {
					fields = fields[1:]
				}
			}
			continue
		}
		break
	}
	return fields
}

func shellCommandLooksLikeDirectoryScopedVerification(command string) bool {
	parts := strings.Split(command, "&&")
	if len(parts) != 2 {
		return false
	}
	leftFields := strings.Fields(strings.TrimSpace(parts[0]))
	if len(leftFields) != 2 || leftFields[0] != "cd" || !safeRelativeShellDir(leftFields[1]) {
		return false
	}
	right := strings.TrimSpace(parts[1])
	if right == "" || strings.ContainsAny(right, "\n;&|><`$()") {
		return false
	}
	rightFields := strings.Fields(right)
	rightFields = normalizeShellCommandFields(rightFields)
	return shellFieldsLookLikeVerification(rightFields)
}

func safeRelativeShellDir(path string) bool {
	path = shellPathToken(path)
	if path == "" || strings.HasPrefix(path, "-") || strings.HasPrefix(path, "~") || filepath.IsAbs(path) {
		return false
	}
	cleaned := filepath.Clean(path)
	return cleaned == "." || (cleaned != ".." && !strings.HasPrefix(cleaned, "../"))
}

func highRiskShellClassification(reason string, destructive bool) ToolClassification {
	return ToolClassification{
		ReadOnly:        false,
		ConcurrencySafe: false,
		Destructive:     destructive,
		Risk:            ToolRiskHigh,
		Reason:          reason,
	}
}

func shellCommandDumpsEnvironment(command string) bool {
	for _, segment := range splitShellCommandSegments(command) {
		fields := stripLeadingShellEnvAssignments(strings.Fields(strings.TrimSpace(segment)))
		if shellFieldsDumpEnvironment(command, fields) {
			return true
		}
		envFields := normalizeShellCommandFieldsPreservingEnv(fields)
		if shellFieldsDumpEnvironment(command, envFields) {
			return true
		}
		fields = normalizeShellCommandFields(fields)
		if shellFieldsDumpEnvironment(command, fields) {
			return true
		}
	}
	return false
}

func shellFieldsDumpEnvironment(command string, fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "printenv", "set", "export":
		return true
	case "env":
		if strings.ContainsAny(command, "\n;&|><`$()") {
			return true
		}
		for _, field := range fields[1:] {
			if !strings.HasPrefix(field, "-") && !looksLikeEnvAssignment(field) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func shellFieldsTouchSensitivePath(fields []string) bool {
	for _, field := range fields[1:] {
		if _, ok := shellSensitivePathTokenReason(field); ok {
			return true
		}
	}
	return false
}

func shellCommandSensitivePathReason(command string) (string, bool) {
	for _, field := range strings.Fields(command) {
		if reason, ok := shellSensitivePathTokenReason(field); ok {
			return reason, true
		}
	}
	return "", false
}

func shellSensitivePathTokenReason(field string) (string, bool) {
	token := shellPathToken(field)
	if token == "" || strings.HasPrefix(token, "-") {
		return "", false
	}
	lower := strings.ToLower(token)
	switch {
	case strings.Contains(lower, ".env"), strings.Contains(lower, ".netrc"), strings.Contains(lower, ".npmrc"), strings.Contains(lower, ".pypirc"), strings.Contains(lower, ".pgpass"):
		return sensitivePathReason(token)
	case strings.Contains(lower, "/") || strings.Contains(lower, "."):
		return sensitivePathReason(token)
	case lower == "secret" || lower == "secrets" || lower == "credential" || lower == "credentials" || lower == "private_key":
		return sensitivePathReason(token)
	default:
		return "", false
	}
}

func shellPathToken(field string) string {
	return strings.Trim(field, " \t\r\n\"'`(){}[]<>|&;,:")
}

func shellFieldsLookDestructive(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	switch shellCommandBaseName(fields[0]) {
	case "rm", "rmdir", "unlink", "mv", "chmod", "chown", "sudo", "dd", "truncate":
		return true
	case "git":
		if len(fields) > 1 {
			switch fields[1] {
			case "reset", "clean", "checkout", "switch", "merge", "rebase":
				return true
			}
		}
	case "docker", "kubectl", "terraform":
		return true
	}
	return false
}

func shellFieldsLookLikeVerification(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	switch shellCommandBaseName(fields[0]) {
	case "go":
		return len(fields) > 1 && oneOf(fields[1], "test", "vet", "build", "list")
	case "cargo":
		return len(fields) > 1 && oneOf(fields[1], "test", "check", "build", "clippy")
	case "pytest", "ruff", "mypy":
		return true
	case "tsc":
		return tscCommandLooksLikeTypecheck(fields[1:])
	case "python", "python3":
		return len(fields) > 2 && fields[1] == "-m" && oneOf(fields[2], "pytest", "mypy")
	case "npm", "pnpm", "yarn", "bun":
		return packageManagerCommandLooksLikeVerification(fields)
	case "make":
		return len(fields) > 1 && makeTargetLooksLikeVerification(fields[1])
	case "uv":
		return len(fields) > 2 && fields[1] == "run" && shellFieldsLookLikeVerification(fields[2:])
	}
	return false
}

func packageManagerCommandLooksLikeVerification(fields []string) bool {
	if len(fields) < 2 {
		return false
	}
	switch fields[1] {
	case "test", "lint", "build", "check", "typecheck":
		return true
	case "run":
		return len(fields) > 2 && makeTargetLooksLikeVerification(fields[2])
	}
	return false
}

func makeTargetLooksLikeVerification(target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	return oneOf(target, "test", "tests", "check", "lint", "typecheck", "build", "verify", "ci")
}

func shellFieldsLookLikePackageOrNetworkMutation(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	switch shellCommandBaseName(fields[0]) {
	case "npm", "pnpm", "yarn", "bun":
		if len(fields) > 1 {
			return oneOf(fields[1], "install", "i", "add", "remove", "update", "upgrade", "publish", "exec", "dlx")
		}
	case "npx":
		return true
	case "pip", "pip3", "uv":
		if len(fields) > 1 {
			return oneOf(fields[1], "install", "add", "remove", "sync", "publish")
		}
	case "curl", "wget", "ssh", "scp", "rsync", "gh":
		return true
	}
	return false
}

func oneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}

func looksLikeEnvAssignment(value string) bool {
	idx := strings.IndexByte(value, '=')
	if idx <= 0 {
		return false
	}
	name := value[:idx]
	for i, r := range name {
		if r == '_' {
			continue
		}
		if i == 0 {
			if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
				return false
			}
			continue
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}
