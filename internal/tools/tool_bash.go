package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// BashTool is the bash-first command tool exposed to the model on
// the Codex / GPT / Claude / generic surfaces. It wraps the same
// shell-execution path as the legacy run_shell tool but advertises
// the new "bash" name and a description that calls out bash as
// the unified terminal entry point.
//
// Implementation note: the underlying executor is executeShellCommand
// (the same package-level function ShellTool uses), so all of the
// existing bash-mode policy checks (sensitive paths, destructive
// commands, package-mutation, env-dump) apply unchanged. The legacy
// run_shell name stays registered as an internal / advanced tool so
// progressive disclosure and replay paths still work.
type BashTool struct{ env *Env }

func NewBashTool(env *Env) *BashTool { return &BashTool{env: env} }

func (t *BashTool) Name() string            { return "bash" }
func (t *BashTool) IsReadOnly() bool        { return false }
func (t *BashTool) IsConcurrencySafe() bool { return false }

func (t *BashTool) Classify(argsJSON string) ToolClassification {
	var args struct {
		Command string `json:"command"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskHigh,
			Reason:          "invalid bash invocation",
		}
	}
	return classifyShellCommand(args.Command)
}

func (t *BashTool) ValidateInput(argsJSON string) error {
	var args struct {
		Command string `json:"command"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	if strings.TrimSpace(args.Command) == "" {
		return errors.New("bash requires command")
	}
	return nil
}

func (t *BashTool) PermissionRequests(argsJSON string) []ToolPermissionRequest {
	var args struct {
		Command string `json:"command"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return nil
	}
	return []ToolPermissionRequest{shellPermissionRequest("bash", args.Command)}
}

func (t *BashTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "bash",
		Description: "Executes a bash command in the workspace and returns its output. " +
			"This is the unified command entry point for the Wuu harness.\n\n" +
			"Use bash for every terminal operation: tests (npx vitest, pytest, go test, " +
			"cargo test, pnpm test, bun test, …), lint, type checks, build commands, " +
			"git operations (git status / diff / log / add / commit / push), package " +
			"manager invocations (npm / pnpm / yarn / bun / pip / uv / cargo / go), " +
			"docker, scripts, and any other shell command. There is no separate " +
			"\"run test\" or \"git\" tool on the bash-first surfaces — bash covers " +
			"all of them.\n\n" +
			"The working directory is the workspace root. Shell state does not persist " +
			"between calls.\n\n" +
			"IMPORTANT: Avoid using bash to cat, head, tail, grep, find, sed, awk, or " +
			"echo when a dedicated tool exists. Use read_file instead of cat, the " +
			"search tools (grep / glob / ast_search / semantic_search) instead of " +
			"grep / rg / find, and apply_patch / edit_file / write_file instead of " +
			"sed.\n\n" +
			"Instructions:\n" +
			"- Commands must be non-interactive; never rely on editors, pagers, or terminal prompts\n" +
			"- Default timeout is 300s, max 3600s\n" +
			"- Results include exit_code, duration_ms, workspace_revision, compact combined output, stdout/stderr tails, and full_log_ref when session artifacts are available\n" +
			"- If commands are independent, make multiple tool calls in parallel\n" +
			"- If commands depend on each other, chain them with '&&'\n" +
			"- Git commands are supported for normal non-interactive workflows: inspect with git status/diff/log, stage explicit paths, commit with git commit -m, and push only when the user explicitly requested a remote write. Unsafe git forms (broad staging, config mutation, force push, hook skipping, destructive reset/clean/checkout, interactive git) are rejected by the bash policy; the structured git tool is reserved for advanced restricted operations.",
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

func (t *BashTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
		Purpose        string `json:"purpose"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if len(args.Command) == 0 || len(bytes.TrimSpace([]byte(args.Command))) == 0 {
		return "", errors.New("bash requires command")
	}
	if reason, ok := blockedShellGitCommandReason(args.Command); ok {
		return "", fmt.Errorf("bash refuses unsafe git command (%s). Use safe shell git commands such as git status/diff/log, explicit-path git add, or git commit -m; use the structured git tool for advanced restricted git operations: error_kind=unsupported_git_shell model_next_action=%q", reason, "retry with a safe git shell command or use the structured git tool")
	}
	if shellCommandDumpsEnvironment(args.Command) {
		return "", errors.New("bash refuses to print process environment variables because they may contain secrets")
	}
	if reason, ok := shellCommandSensitivePathReason(args.Command); ok {
		return "", fmt.Errorf("bash refuses to access sensitive paths (%s). Use dedicated metadata-safe tools or ask the user for explicit secret handling", reason)
	}
	if shellCommandInvokesDestructiveCommand(args.Command) {
		return "", errors.New("bash refuses to execute destructive shell commands; use apply_patch, edit_file, write_file, or another restricted tool so changes remain auditable")
	}
	if shellCommandInvokesPackageOrNetworkMutation(args.Command) {
		return "", errors.New("bash refuses to execute package, network, or external mutation commands; use dedicated web tools or ask the user for explicit approval")
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
	result.Purpose = redactToolOutput(args.Purpose)
	fullLogRef, fullLogBytes, fullLogErr := persistShellLog(t.env.SessionDir, result)
	if fullLogRef != "" {
		result.FullLogRef = fullLogRef
		result.FullLogBytes = fullLogBytes
	} else if fullLogErr != "" {
		result.FullLogError = fullLogErr
	}
	return mustJSON(result)
}
