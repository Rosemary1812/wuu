package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

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
			"- Results include exit_code, duration_ms, combined output, and stdout/stderr tails\n" +
			"- If commands are independent, make multiple tool calls in parallel\n" +
			"- If commands depend on each other, chain them with '&&'\n" +
			"- For git operations, prefer the git tool over run_shell",
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
			},
			"required": []string{"command"},
		},
	}
}

func (t *ShellTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if len(args.Command) == 0 || len(bytes.TrimSpace([]byte(args.Command))) == 0 {
		return "", errors.New("run_shell requires command")
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
	return mustJSON(result)
}

type shellExecutionResult struct {
	Command             string             `json:"command"`
	Classification      ToolClassification `json:"classification"`
	ExitCode            int                `json:"exit_code"`
	DurationMS          int64              `json:"duration_ms"`
	TimedOut            bool               `json:"timed_out"`
	Truncated           bool               `json:"truncated"`
	Output              string             `json:"output"`
	StdoutTail          string             `json:"stdout_tail"`
	StderrTail          string             `json:"stderr_tail"`
	StdoutBytes         int                `json:"stdout_bytes"`
	StderrBytes         int                `json:"stderr_bytes"`
	StdoutTailTruncated bool               `json:"stdout_tail_truncated"`
	StderrTailTruncated bool               `json:"stderr_tail_truncated"`
	NextSuggestions     []string           `json:"next_suggestions,omitempty"`
}

func executeShellCommand(ctx context.Context, env *Env, command string, timeoutSeconds int) (shellExecutionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if env == nil {
		env = &Env{}
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	cmd.Dir = env.RootDir
	cmd.Env = mergeEnv(os.Environ(), nonInteractiveShellEnv())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startedAt := time.Now()
	err := cmd.Run()
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
	redactedStdout := redactToolOutput(stdoutText)
	redactedStderr := redactToolOutput(stderrText)
	redactedCommand := redactToolOutput(command)
	output := redactedStdout + redactedStderr
	trimmed, truncated := truncate(output, maxShellOutputBytes)
	stdoutTail, stdoutTailTruncated := tailString(redactedStdout, maxShellTailBytes)
	stderrTail, stderrTailTruncated := tailString(redactedStderr, maxShellTailBytes)
	classification := classifyShellCommand(command)
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)

	return shellExecutionResult{
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
		StdoutTailTruncated: stdoutTailTruncated,
		StderrTailTruncated: stderrTailTruncated,
		NextSuggestions:     shellNextSuggestions(exitCode, timedOut, classification),
	}, nil
}

func shellNextSuggestions(exitCode int, timedOut bool, classification ToolClassification) []string {
	if timedOut {
		return []string{"narrow the command scope or increase timeout only if the long-running command is necessary"}
	}
	if exitCode != 0 {
		return []string{"inspect the redacted stdout/stderr tails, then retry with corrected inputs or use run_test for verification commands"}
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
	if strings.ContainsAny(command, "\n;&|><`$()") {
		return highRiskShellClassification("shell command uses shell metacharacters", false)
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return highRiskShellClassification("empty shell command", false)
	}
	for len(fields) > 0 && looksLikeEnvAssignment(fields[0]) {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return highRiskShellClassification("environment-only shell command", false)
	}
	if shellFieldsTouchSensitivePath(fields) {
		return highRiskShellClassification("shell command may read secrets", false)
	}
	if shellFieldsLookDestructive(fields) {
		return highRiskShellClassification("destructive shell command", true)
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
		return highRiskShellClassification("package, network, or external mutation command", true)
	}
	return highRiskShellClassification("shell command is not proven read-only or verification-only", false)
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

func shellFieldsTouchSensitivePath(fields []string) bool {
	for _, field := range fields[1:] {
		lower := strings.ToLower(strings.Trim(field, `"'`))
		if strings.Contains(lower, ".env") ||
			strings.Contains(lower, "credential") ||
			strings.Contains(lower, "credentials") ||
			strings.Contains(lower, "secret") ||
			strings.Contains(lower, ".netrc") ||
			strings.Contains(lower, ".npmrc") ||
			strings.Contains(lower, ".pypirc") ||
			strings.Contains(lower, ".pgpass") {
			return true
		}
	}
	return false
}

func shellFieldsLookDestructive(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "rm", "rmdir", "unlink", "mv", "chmod", "chown", "sudo", "dd", "truncate":
		return true
	case "git":
		if len(fields) > 1 {
			switch fields[1] {
			case "reset", "clean", "push", "tag", "checkout", "switch", "merge", "rebase", "commit":
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
	switch fields[0] {
	case "go":
		return len(fields) > 1 && oneOf(fields[1], "test", "vet", "build", "list")
	case "cargo":
		return len(fields) > 1 && oneOf(fields[1], "test", "check", "build", "clippy")
	case "pytest", "ruff", "mypy", "tsc":
		return true
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
	switch fields[0] {
	case "npm", "pnpm", "yarn", "bun":
		if len(fields) > 1 {
			return oneOf(fields[1], "install", "i", "add", "remove", "update", "upgrade", "publish", "exec", "dlx")
		}
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
