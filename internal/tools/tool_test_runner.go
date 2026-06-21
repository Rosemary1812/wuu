package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

const maxRepeatedRunTestFailures = 2

type RunTestTool struct{ env *Env }

func NewRunTestTool(env *Env) *RunTestTool { return &RunTestTool{env: env} }

func (t *RunTestTool) Name() string            { return "run_test" }
func (t *RunTestTool) IsReadOnly() bool        { return false }
func (t *RunTestTool) IsConcurrencySafe() bool { return false }

func (t *RunTestTool) Classify(argsJSON string) ToolClassification {
	var args struct {
		Command string `json:"command"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskHigh,
			Reason:          "invalid test invocation",
		}
	}
	return classifyTestCommand(args.Command)
}

func (t *RunTestTool) ValidateInput(argsJSON string) error {
	var args struct {
		Command string `json:"command"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	if strings.TrimSpace(args.Command) == "" {
		return errors.New("run_test requires command")
	}
	return nil
}

func (t *RunTestTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "run_test",
		Description: "Run one local verification command and return structured test/debug feedback.\n\n" +
			"Usage:\n" +
			"- Use for targeted, affected, or full test/build/typecheck/lint verification\n" +
			"- The command must be a single local verification command such as go test, pytest, npm test, npm run lint, cargo test, or make test\n" +
			"- JavaScript test-runner shims such as npx vitest are resolved to the project-local node_modules/.bin runner when it exists; run_test will not download packages\n" +
			"- Do not use for package installation, network calls, deploys, git mutations, or arbitrary shell exploration\n" +
			"- Commands that dump environment variables or touch sensitive credential paths are rejected\n" +
			"- Results include exit code, duration, compact output, and failure_summary with likely failing tests or error snippets",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Single non-interactive local verification command to run.",
				},
				"scope": map[string]any{
					"type":        "string",
					"enum":        []string{"targeted", "affected", "full"},
					"description": "Verification scope. Defaults to targeted.",
				},
				"purpose": map[string]any{
					"type":        "string",
					"description": "Why this verification is needed.",
				},
				"timeout_seconds": map[string]any{
					"type":        "integer",
					"description": "Max runtime in seconds (1-3600).",
				},
			},
			"required": []string{"command"},
		},
	}
}

func (t *RunTestTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Command        string `json:"command"`
		Scope          string `json:"scope"`
		Purpose        string `json:"purpose"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Command) == "" {
		return "", errors.New("run_test requires command")
	}
	command := strings.TrimSpace(args.Command)
	if shellCommandDumpsEnvironment(args.Command) {
		return "", errors.New("run_test refuses to print process environment variables because they may contain secrets")
	}
	if reason, ok := shellCommandSensitivePathReason(args.Command); ok {
		return "", errors.New("run_test refuses to access sensitive paths (" + reason + "). Use dedicated metadata-safe tools or ask the user for explicit secret handling")
	}
	resolved, err := resolveRunTestCommand(t.env.RootDir, args.Command)
	if err != nil {
		return "", err
	}
	command = resolved.Command
	classification := classifyTestCommand(command)
	if classification.Risk != ToolRiskMedium || classification.Reason != "local verification command" {
		return "", errors.New("run_test only accepts local verification commands; use run_shell for other shell commands")
	}
	timeout := args.TimeoutSeconds
	if timeout <= 0 {
		timeout = defaultShellTimeoutSeconds
	}
	if timeout > maxShellTimeoutSeconds {
		timeout = maxShellTimeoutSeconds
	}
	scope := strings.TrimSpace(args.Scope)
	if scope == "" {
		scope = "targeted"
	}

	revision := workspaceRevision(ctx, t.env.RootDir)
	commandHash := sha256Hex([]byte(command))
	previousFailures := t.env.ConsecutiveTestFailures(commandHash, revision)
	if revision != "" && previousFailures >= maxRepeatedRunTestFailures {
		return "", repeatedRunTestFailureError{
			PreviousFailures: previousFailures,
			MaxFailures:      maxRepeatedRunTestFailures,
			Revision:         revision,
			CommandHash:      commandHash,
		}
	}

	shellResult, err := executeShellCommand(ctx, t.env, command, timeout)
	if err != nil {
		return "", err
	}
	failureSummary := summarizeTestFailure(shellResult.Output)
	if shellResult.ExitCode != 0 {
		failureSummary.Failed = true
	}
	failed := shellResult.ExitCode != 0 || shellResult.TimedOut || failureSummary.Failed
	fullLogRef, fullLogBytes, fullLogErr := persistRunTestLog(t.env.SessionDir, commandHash, scope, args.Purpose, shellResult)
	t.env.RecordTestRunResult(testRunEntry{
		CommandHash:    commandHash,
		Revision:       revision,
		Failed:         failed,
		Command:        command,
		Scope:          scope,
		Purpose:        args.Purpose,
		ExitCode:       shellResult.ExitCode,
		TimedOut:       shellResult.TimedOut,
		DurationMS:     shellResult.DurationMS,
		FailureSummary: failureSummary,
		FullLogRef:     fullLogRef,
	})
	result := map[string]any{
		"action":             "run",
		"command":            shellResult.Command,
		"scope":              scope,
		"purpose":            redactToolOutput(args.Purpose),
		"classification":     classification,
		"passed":             shellResult.ExitCode == 0 && !shellResult.TimedOut,
		"exit_code":          shellResult.ExitCode,
		"duration_ms":        shellResult.DurationMS,
		"timed_out":          shellResult.TimedOut,
		"truncated":          shellResult.Truncated,
		"output":             shellResult.Output,
		"stdout_tail":        shellResult.StdoutTail,
		"stderr_tail":        shellResult.StderrTail,
		"stdout_bytes":       shellResult.StdoutBytes,
		"stderr_bytes":       shellResult.StderrBytes,
		"failure_summary":    failureSummary,
		"workspace_revision": revision,
		"repeat_guard": map[string]any{
			"previous_failed_runs":                    previousFailures,
			"max_failed_runs_without_revision_change": maxRepeatedRunTestFailures,
		},
		"next_suggestions": runTestNextSuggestions(shellResult, failureSummary),
	}
	if resolved.Changed {
		result["requested_command"] = redactToolOutput(resolved.Requested)
		result["resolved_command"] = shellResult.Command
	}
	if fullLogRef != "" {
		result["full_log_ref"] = fullLogRef
		result["full_log_bytes"] = fullLogBytes
	} else if fullLogErr != "" {
		result["full_log_error"] = fullLogErr
	}
	return mustJSON(result)
}

func persistRunTestLog(sessionDir, commandHash, scope, purpose string, shellResult shellExecutionResult) (path string, bytes int, errSummary string) {
	sessionDir = strings.TrimSpace(sessionDir)
	if sessionDir == "" {
		return "", 0, ""
	}
	dir := filepath.Join(sessionDir, "tool-results", "run-test-logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, evalSafeToolError(err)
	}
	name := fmt.Sprintf("%s-%s.log", time.Now().UTC().Format("20060102T150405.000000000Z"), commandHashPrefix(commandHash))
	path = filepath.Join(dir, name)
	content := buildRunTestLog(scope, purpose, shellResult)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", 0, evalSafeToolError(err)
	}
	return path, len(content), ""
}

type repeatedRunTestFailureError struct {
	PreviousFailures int
	MaxFailures      int
	Revision         string
	CommandHash      string
}

func (e repeatedRunTestFailureError) Error() string {
	return fmt.Sprintf(
		"run_test blocked repeated failing command: error_kind=repeated_failure_same_revision previous_failed_runs=%d max_failed_runs_without_revision_change=%d workspace_revision=%s command_hash=%s safe_retry=%q model_next_action=%q",
		e.PreviousFailures,
		e.MaxFailures,
		e.Revision,
		commandHashPrefix(e.CommandHash),
		"change code, narrow the command, or inspect failure_summary/full_log_ref before rerunning",
		"read the latest failure evidence, form a new hypothesis, patch minimally, then rerun targeted verification after the workspace revision changes",
	)
}

func commandHashPrefix(commandHash string) string {
	commandHash = strings.TrimSpace(commandHash)
	if len(commandHash) > 12 {
		return commandHash[:12]
	}
	if commandHash == "" {
		return "unknown"
	}
	return commandHash
}

func buildRunTestLog(scope, purpose string, shellResult shellExecutionResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "command: %s\n", shellResult.Command)
	fmt.Fprintf(&b, "scope: %s\n", strings.TrimSpace(scope))
	if strings.TrimSpace(purpose) != "" {
		fmt.Fprintf(&b, "purpose: %s\n", redactToolOutput(purpose))
	}
	fmt.Fprintf(&b, "exit_code: %d\n", shellResult.ExitCode)
	fmt.Fprintf(&b, "duration_ms: %d\n", shellResult.DurationMS)
	fmt.Fprintf(&b, "timed_out: %t\n", shellResult.TimedOut)
	fmt.Fprintf(&b, "stdout_bytes: %d\n", shellResult.StdoutBytes)
	fmt.Fprintf(&b, "stderr_bytes: %d\n\n", shellResult.StderrBytes)
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

func evalSafeToolError(err error) string {
	if err == nil {
		return ""
	}
	return redactToolOutput(err.Error())
}

func runTestNextSuggestions(shellResult shellExecutionResult, failureSummary testFailureSummary) []string {
	if shellResult.TimedOut {
		return []string{"narrow the verification scope before rerunning, or raise timeout only if the broad test is required"}
	}
	if shellResult.ExitCode != 0 || failureSummary.Failed {
		return []string{"inspect failure_summary, form a concrete hypothesis, read implicated files, patch minimally, then rerun targeted verification"}
	}
	return []string{"record this verification in the final response and inspect the final diff before finishing"}
}

type testFailureSummary struct {
	Failed       bool                  `json:"failed"`
	FailingTests []string              `json:"failing_tests,omitempty"`
	Indicators   []string              `json:"indicators,omitempty"`
	Locations    []testFailureLocation `json:"locations,omitempty"`
	Snippets     []string              `json:"snippets,omitempty"`
}

type testFailureLocation struct {
	Path   string `json:"path"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
	Text   string `json:"text,omitempty"`
}

var (
	goFailLine       = regexp.MustCompile(`^--- FAIL:\s+([^\s(]+)`)
	pytestFailLine   = regexp.MustCompile(`^FAILED\s+([^\s]+)`)
	jsFailLine       = regexp.MustCompile(`^\s*FAIL\s+(.+)$`)
	fileLocationLine = regexp.MustCompile(`([A-Za-z0-9_./\\-]+\.(?:go|py|js|jsx|ts|tsx|rs|java|kt|kts|c|cc|cpp|cxx|h|hpp|cs|rb|php|swift|m|mm)):(\d+)(?::(\d+))?`)
)

func summarizeTestFailure(output string) testFailureSummary {
	var summary testFailureSummary
	if strings.TrimSpace(output) == "" {
		return summary
	}
	lines := strings.Split(output, "\n")
	seenTests := map[string]struct{}{}
	seenIndicators := map[string]struct{}{}
	seenLocations := map[string]struct{}{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		addFailureLocations(&summary.Locations, seenLocations, trimmed)
		if match := goFailLine.FindStringSubmatch(trimmed); len(match) == 2 {
			addUnique(&summary.FailingTests, seenTests, match[1])
			addTestSnippet(&summary.Snippets, lines, i)
			summary.Failed = true
			continue
		}
		if match := pytestFailLine.FindStringSubmatch(trimmed); len(match) == 2 {
			addUnique(&summary.FailingTests, seenTests, match[1])
			addTestSnippet(&summary.Snippets, lines, i)
			summary.Failed = true
			continue
		}
		if match := jsFailLine.FindStringSubmatch(trimmed); len(match) == 2 {
			addUnique(&summary.FailingTests, seenTests, strings.TrimSpace(match[1]))
			addTestSnippet(&summary.Snippets, lines, i)
			summary.Failed = true
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "assertion") ||
			strings.Contains(lower, "expected") ||
			strings.Contains(lower, "panic:") ||
			strings.Contains(lower, "error:") ||
			strings.Contains(lower, "failed") {
			addUnique(&summary.Indicators, seenIndicators, trimmed)
			addTestSnippet(&summary.Snippets, lines, i)
			summary.Failed = true
		}
	}
	if len(summary.Indicators) > 8 {
		summary.Indicators = summary.Indicators[:8]
	}
	if len(summary.Locations) > 8 {
		summary.Locations = summary.Locations[:8]
	}
	if len(summary.Snippets) > 8 {
		summary.Snippets = summary.Snippets[:8]
	}
	return summary
}

func addUnique(values *[]string, seen map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if _, ok := seen[value]; ok {
		return
	}
	seen[value] = struct{}{}
	*values = append(*values, value)
}

func addFailureLocations(values *[]testFailureLocation, seen map[string]struct{}, line string) {
	for _, match := range fileLocationLine.FindAllStringSubmatch(line, -1) {
		if len(match) < 3 {
			continue
		}
		path := strings.TrimSpace(strings.ReplaceAll(match[1], "\\", "/"))
		lineNumber, err := strconv.Atoi(match[2])
		if path == "" || err != nil || lineNumber <= 0 {
			continue
		}
		columnNumber := 0
		if len(match) > 3 && strings.TrimSpace(match[3]) != "" {
			columnNumber, _ = strconv.Atoi(match[3])
		}
		key := fmt.Sprintf("%s:%d:%d", path, lineNumber, columnNumber)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		*values = append(*values, testFailureLocation{
			Path:   path,
			Line:   lineNumber,
			Column: columnNumber,
			Text:   truncateFailureLocationText(line),
		})
	}
}

func truncateFailureLocationText(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 240 {
		return value
	}
	return value[:240] + "..."
}

func addTestSnippet(snippets *[]string, lines []string, idx int) {
	start := idx - 1
	if start < 0 {
		start = 0
	}
	end := idx + 2
	if end > len(lines) {
		end = len(lines)
	}
	snippet := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	if snippet != "" {
		*snippets = append(*snippets, snippet)
	}
}

func classifyTestCommand(command string) ToolClassification {
	if testCommandLooksLikeLocalRunnerVerification(command) {
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskMedium,
			Reason:          "local verification command",
		}
	}
	classification := classifyShellCommand(command)
	if classification.Risk == ToolRiskMedium && classification.Reason == "local verification command" {
		return classification
	}
	classification.ReadOnly = false
	classification.ConcurrencySafe = false
	classification.Risk = ToolRiskHigh
	if classification.Reason == "" || classification.Reason == "simple read-only shell command" {
		classification.Reason = "run_test requires a local verification command"
	}
	return classification
}

type resolvedRunTestCommand struct {
	Requested string
	Command   string
	Changed   bool
}

func resolveRunTestCommand(rootDir, command string) (resolvedRunTestCommand, error) {
	requested := strings.TrimSpace(command)
	resolved := resolvedRunTestCommand{Requested: requested, Command: requested}
	if rewritten, changed, err := resolveLocalNpxTestRunner(rootDir, requested); err != nil {
		return resolved, err
	} else if changed {
		resolved.Command = rewritten
		resolved.Changed = true
	}
	return resolved, nil
}

func testCommandLooksLikeLocalRunnerVerification(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	if left, dir, right, ok := splitDirectoryScopedTestCommand(command); ok {
		_ = left
		_ = dir
		return testCommandLooksLikeLocalRunnerVerification(right)
	}
	if strings.ContainsAny(command, "\n;&|><`$()") {
		return false
	}
	fields := normalizeShellCommandFields(strings.Fields(command))
	if len(fields) == 0 {
		return false
	}
	if fields[0] == "npx" {
		runnerIdx, ok := npxVerificationRunnerIndex(fields, 0)
		return ok && jsVerificationRunnerName(fields[runnerIdx])
	}
	return jsVerificationRunnerName(fields[0])
}

func resolveLocalNpxTestRunner(rootDir, command string) (string, bool, error) {
	if left, dir, right, ok := splitDirectoryScopedTestCommand(command); ok {
		workDir := filepath.Join(rootDir, dir)
		rewritten, changed, err := resolveSimpleLocalNpxTestRunner(workDir, right)
		if err != nil || !changed {
			return "", false, err
		}
		return left + " && " + rewritten, true, nil
	}
	if strings.ContainsAny(command, "\n;&|><`$()") {
		return "", false, nil
	}
	return resolveSimpleLocalNpxTestRunner(rootDir, command)
}

func resolveSimpleLocalNpxTestRunner(workDir, command string) (string, bool, error) {
	fields := strings.Fields(command)
	exeIdx := shellExecutableFieldIndex(fields)
	if exeIdx < 0 || exeIdx >= len(fields) || fields[exeIdx] != "npx" {
		return "", false, nil
	}
	runnerIdx, ok := npxVerificationRunnerIndex(fields, exeIdx)
	if !ok || !jsVerificationRunnerName(fields[runnerIdx]) {
		return "", false, nil
	}
	runner := jsRunnerBaseName(fields[runnerIdx])
	localRunner := filepath.Join(workDir, "node_modules", ".bin", runner)
	if _, err := os.Stat(localRunner); err != nil {
		if os.IsNotExist(err) {
			return "", false, localTestRunnerMissingError{
				Runner:   runner,
				Expected: filepath.ToSlash(filepath.Join("node_modules", ".bin", runner)),
			}
		}
		return "", false, fmt.Errorf("inspect local test runner %q: %w", localRunner, err)
	}
	rewritten := make([]string, 0, len(fields)-runnerIdx+exeIdx+1)
	rewritten = append(rewritten, fields[:exeIdx]...)
	rewritten = append(rewritten, "./node_modules/.bin/"+runner)
	rewritten = append(rewritten, fields[runnerIdx+1:]...)
	return strings.Join(rewritten, " "), true, nil
}

func splitDirectoryScopedTestCommand(command string) (left, dir, right string, ok bool) {
	parts := strings.Split(command, "&&")
	if len(parts) != 2 {
		return "", "", "", false
	}
	left = strings.TrimSpace(parts[0])
	right = strings.TrimSpace(parts[1])
	leftFields := strings.Fields(left)
	if len(leftFields) != 2 || leftFields[0] != "cd" || !safeRelativeShellDir(leftFields[1]) || right == "" {
		return "", "", "", false
	}
	if strings.ContainsAny(right, "\n;&|><`$()") {
		return "", "", "", false
	}
	return left, shellPathToken(leftFields[1]), right, true
}

func shellExecutableFieldIndex(fields []string) int {
	for i := 0; i < len(fields); {
		for i < len(fields) && looksLikeEnvAssignment(fields[i]) {
			i++
		}
		if i >= len(fields) {
			return -1
		}
		switch shellCommandBaseName(fields[i]) {
		case "command", "exec":
			i++
			continue
		case "env":
			i++
			for i < len(fields) {
				field := fields[i]
				if looksLikeEnvAssignment(field) {
					i++
					continue
				}
				if strings.HasPrefix(field, "-") {
					i++
					if field == "-u" || field == "--unset" || field == "-C" || field == "--chdir" {
						if i < len(fields) {
							i++
						}
					}
					continue
				}
				break
			}
			continue
		default:
			stripped, ok := stripSafeShellWrapperPrefix(fields[i:])
			if ok {
				i += len(fields[i:]) - len(stripped)
				continue
			}
			if shellWrapperCommandName(fields[i]) {
				return -1
			}
			return i
		}
	}
	return -1
}

func npxVerificationRunnerIndex(fields []string, npxIdx int) (int, bool) {
	if npxIdx < 0 || npxIdx >= len(fields) || fields[npxIdx] != "npx" {
		return -1, false
	}
	i := npxIdx + 1
	for i < len(fields) {
		switch fields[i] {
		case "--no-install", "--":
			i++
			continue
		default:
			if strings.HasPrefix(fields[i], "-") {
				return -1, false
			}
			return i, true
		}
	}
	return -1, false
}

func jsVerificationRunnerName(name string) bool {
	switch jsRunnerBaseName(name) {
	case "vitest", "jest", "mocha", "ava", "tap":
		return true
	default:
		return false
	}
}

func jsRunnerBaseName(name string) string {
	name = shellPathToken(name)
	name = strings.TrimSuffix(name, ".cmd")
	name = strings.TrimSuffix(name, ".ps1")
	return filepath.Base(name)
}

type localTestRunnerMissingError struct {
	Runner   string
	Expected string
}

func (e localTestRunnerMissingError) Error() string {
	return fmt.Sprintf(
		"run_test refuses to invoke npx for local test runner %q because npx can download packages when the runner is missing: error_kind=local_test_runner_missing expected=%q model_next_action=%q",
		e.Runner,
		e.Expected,
		"install project dependencies with explicit approval, use an existing package script, or run a project-local test binary",
	)
}
