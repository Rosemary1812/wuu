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
	if shellCommandLooksReadOnly(args.Command) {
		return ToolClassification{
			ReadOnly:        true,
			ConcurrencySafe: true,
			Risk:            ToolRiskLow,
			Reason:          "simple read-only shell command",
		}
	}
	return ToolClassification{
		ReadOnly:        false,
		ConcurrencySafe: false,
		Risk:            ToolRiskHigh,
		Reason:          "shell command is not proven read-only",
	}
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

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-lc", args.Command)
	cmd.Dir = t.env.RootDir
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
			return "", fmt.Errorf("run command: %w", err)
		}
	}

	stdoutText := stdout.String()
	stderrText := stderr.String()
	output := stdoutText + stderrText
	trimmed, truncated := truncate(output, maxShellOutputBytes)
	stdoutTail, stdoutTailTruncated := tailString(stdoutText, maxShellTailBytes)
	stderrTail, stderrTailTruncated := tailString(stderrText, maxShellTailBytes)

	result := map[string]any{
		"command":               args.Command,
		"exit_code":             exitCode,
		"duration_ms":           durationMS,
		"timed_out":             errors.Is(runCtx.Err(), context.DeadlineExceeded),
		"truncated":             truncated,
		"output":                trimmed,
		"stdout_tail":           stdoutTail,
		"stderr_tail":           stderrTail,
		"stdout_bytes":          len(stdoutText),
		"stderr_bytes":          len(stderrText),
		"stdout_tail_truncated": stdoutTailTruncated,
		"stderr_tail_truncated": stderrTailTruncated,
	}
	return mustJSON(result)
}

func tailString(value string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value, false
	}
	return stringutil.HeadTail(value, 0, maxBytes, ""), true
}

func shellCommandLooksReadOnly(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	if strings.ContainsAny(command, "\n;&|><`$()") {
		return false
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	for len(fields) > 0 && looksLikeEnvAssignment(fields[0]) {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "pwd", "ls", "cat", "head", "tail", "wc", "file", "stat", "du",
		"rg", "grep":
		return true
	default:
		return false
	}
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
