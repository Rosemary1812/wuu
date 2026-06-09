package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

func (t *RunTestTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "run_test",
		Description: "Run one local verification command and return structured test/debug feedback.\n\n" +
			"Usage:\n" +
			"- Use for targeted, affected, or full test/build/typecheck/lint verification\n" +
			"- The command must be a single local verification command such as go test, pytest, npm test, npm run lint, cargo test, or make test\n" +
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
	classification := classifyTestCommand(args.Command)
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
		return "", fmt.Errorf("run_test refuses to rerun the same command after %d failed attempts at unchanged workspace revision. Change code, narrow the command, or inspect failure_summary before rerunning", previousFailures)
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
	t.env.RecordTestRun(commandHash, revision, failed)
	fullLogRef, fullLogBytes, fullLogErr := persistRunTestLog(t.env.SessionDir, commandHash, scope, args.Purpose, shellResult)
	result := map[string]any{
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
	Failed       bool     `json:"failed"`
	FailingTests []string `json:"failing_tests,omitempty"`
	Indicators   []string `json:"indicators,omitempty"`
	Snippets     []string `json:"snippets,omitempty"`
}

var (
	goFailLine     = regexp.MustCompile(`^--- FAIL:\s+([^\s(]+)`)
	pytestFailLine = regexp.MustCompile(`^FAILED\s+([^\s]+)`)
	jsFailLine     = regexp.MustCompile(`^\s*FAIL\s+(.+)$`)
)

func summarizeTestFailure(output string) testFailureSummary {
	var summary testFailureSummary
	if strings.TrimSpace(output) == "" {
		return summary
	}
	lines := strings.Split(output, "\n")
	seenTests := map[string]struct{}{}
	seenIndicators := map[string]struct{}{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
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
