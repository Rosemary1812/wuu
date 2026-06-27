package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	proc "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"
)

const (
	bashActionRun             = "run"
	bashActionStartBackground = "start_background"
	bashActionListBackground  = "list_background"
	bashActionReadBackground  = "read_background"
	bashActionWriteBackground = "write_background"
	bashActionStopBackground  = "stop_background"
)

// BashTool is the bash-first command tool exposed to the model on
// the Codex / GPT / Claude / generic surfaces. It exposes one
// unified terminal entry point for bounded commands, verification,
// and managed background processes.
//
// Implementation note: short-lived commands use executeShellCommand,
// verification commands get result enrichment as a post-processor,
// and long-running commands use the managed process backend.
// Package/network commands that are covered by the default command
// policy reach approval before this executor runs; unrelated
// package/network commands still fail closed.
type BashTool struct{ env *Env }

func NewBashTool(env *Env) *BashTool { return &BashTool{env: env} }

func (t *BashTool) Name() string            { return "bash" }
func (t *BashTool) IsReadOnly() bool        { return false }
func (t *BashTool) IsConcurrencySafe() bool { return false }

func (t *BashTool) Classify(argsJSON string) ToolClassification {
	var args bashArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskHigh,
			Reason:          "invalid bash invocation",
		}
	}
	switch normalizeBashAction(args) {
	case bashActionRun:
		return classifyShellCommand(args.Command)
	case bashActionStartBackground:
		classification := classifyShellCommand(args.Command)
		reason := "managed background process"
		if classification.Reason != "" {
			reason = "background command: " + classification.Reason
		}
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Destructive:     classification.Destructive,
			Risk:            ToolRiskHigh,
			Reason:          reason,
		}
	case bashActionListBackground, bashActionReadBackground:
		return ToolClassification{
			ReadOnly:        true,
			ConcurrencySafe: true,
			Risk:            ToolRiskMedium,
			Reason:          "managed background process observation",
		}
	case bashActionWriteBackground, bashActionStopBackground:
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: true,
			Risk:            ToolRiskHigh,
			Reason:          "managed background process control",
		}
	default:
		return highRiskShellClassification("invalid bash action", false)
	}
}

func (t *BashTool) ValidateInput(argsJSON string) error {
	var args bashArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	switch normalizeBashAction(args) {
	case bashActionRun, bashActionStartBackground:
		if strings.TrimSpace(args.Command) == "" {
			return errors.New("bash requires command")
		}
	case bashActionListBackground:
		return nil
	case bashActionReadBackground, bashActionWriteBackground, bashActionStopBackground:
		if strings.TrimSpace(args.ProcessID) == "" {
			return errors.New("bash background action requires process_id")
		}
	default:
		return errors.New("bash action must be one of run, start_background, list_background, read_background, write_background, stop_background")
	}
	return nil
}

func (t *BashTool) PermissionRequests(argsJSON string) []ToolPermissionRequest {
	var args bashArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return nil
	}
	switch normalizeBashAction(args) {
	case bashActionStartBackground:
		return []ToolPermissionRequest{backgroundPermissionRequest(args.Command)}
	case bashActionListBackground:
		return []ToolPermissionRequest{backgroundProcessPermissionRequest("*")}
	case bashActionReadBackground, bashActionWriteBackground, bashActionStopBackground:
		return []ToolPermissionRequest{backgroundProcessPermissionRequest(args.ProcessID)}
	default:
		return []ToolPermissionRequest{shellPermissionRequest("bash", args.Command)}
	}
}

func (t *BashTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "bash",
		Description: "Run non-interactive bash operations in the workspace. Use for terminal work: tests, lint, builds, git, package managers, scripts, docker, and managed background processes.\n\n" +
			"Prefer dedicated file/search/edit tools for reading, searching, or changing files. Default action=run returns exit_code, duration_ms, workspace_revision, output tails, and full_log_ref when available; verification commands add verification metadata. Use start_background/list_background/read_background/write_background/stop_background for long-lived processes. cwd defaults to the workspace root. Shell state does not persist between run calls.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{bashActionRun, bashActionStartBackground, bashActionListBackground, bashActionReadBackground, bashActionWriteBackground, bashActionStopBackground},
					"description": "Operation. Defaults to run; background actions manage long-lived processes.",
				},
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command to execute or start in the background. Must be non-interactive; never rely on editors, pagers, or terminal prompts.",
				},
				"timeout_seconds": map[string]any{
					"type":        "integer",
					"description": "Max runtime in seconds for action=run (1-3600).",
				},
				"purpose": map[string]any{
					"type":        "string",
					"description": "Why this command is needed. Stored in redacted logs for replay and audit.",
				},
				"scope": map[string]any{
					"type":        "string",
					"enum":        []string{"targeted", "affected", "full"},
					"description": "Verification scope for test/build/lint/typecheck commands. Defaults to targeted.",
				},
				"cwd": map[string]any{
					"type":        "string",
					"description": "Working directory for run/start_background. Defaults to workspace root.",
				},
				"lifecycle": map[string]any{
					"type":        "string",
					"enum":        []string{"session", "managed"},
					"description": "Background process lifecycle. Defaults to session.",
				},
				"tty": map[string]any{
					"type":        "boolean",
					"description": "Run the background command in a pseudo-terminal.",
				},
				"wait_ms": map[string]any{
					"type":        "integer",
					"description": "For background start/read: wait this many milliseconds for output. Maximum 60000 for start.",
				},
				"max_bytes": map[string]any{
					"type":        "integer",
					"description": "Maximum background output bytes to return. Default 32768.",
				},
				"offset_bytes": map[string]any{
					"type":        "integer",
					"description": "For read_background: byte offset to read from. Use the previous end_offset for incremental logs.",
				},
				"process_id": map[string]any{
					"type":        "string",
					"description": "Managed background process id for read_background/write_background/stop_background.",
				},
				"input": map[string]any{
					"type":        "string",
					"description": "Text to write to background process input for action=write_background. Include a trailing newline when needed.",
				},
			},
			"required": []string{},
		},
	}
}

func (t *BashTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args bashArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	switch normalizeBashAction(args) {
	case bashActionRun:
		return t.executeRun(ctx, args)
	case bashActionStartBackground:
		return t.executeStartBackground(ctx, args)
	case bashActionListBackground:
		return t.executeListBackground()
	case bashActionReadBackground:
		return t.executeReadBackground(ctx, args)
	case bashActionWriteBackground:
		return t.executeWriteStdin(args)
	case bashActionStopBackground:
		return t.executeStopBackground(args)
	default:
		return "", errors.New("bash action must be one of run, start_background, list_background, read_background, write_background, stop_background")
	}
}

type bashArgs struct {
	Action         string `json:"action"`
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Purpose        string `json:"purpose"`
	Scope          string `json:"scope"`
	CWD            string `json:"cwd"`
	OwnerKind      string `json:"owner_kind"`
	OwnerID        string `json:"owner_id"`
	Lifecycle      string `json:"lifecycle"`
	TTY            bool   `json:"tty"`
	WaitMS         int    `json:"wait_ms"`
	MaxBytes       int    `json:"max_bytes"`
	OffsetBytes    *int64 `json:"offset_bytes"`
	ProcessID      string `json:"process_id"`
	Input          string `json:"input"`
	Background     bool   `json:"background"`
}

func normalizeBashAction(args bashArgs) string {
	action := strings.TrimSpace(args.Action)
	switch action {
	case "":
		if args.Background {
			return bashActionStartBackground
		}
		return bashActionRun
	default:
		return action
	}
}

type bashVerificationResult struct {
	Kind              string             `json:"kind"`
	Scope             string             `json:"scope"`
	Passed            bool               `json:"passed"`
	FailureSummary    testFailureSummary `json:"failure_summary"`
	WorkspaceRevision string             `json:"workspace_revision,omitempty"`
	RepeatGuard       map[string]any     `json:"repeat_guard,omitempty"`
	CommandHash       string             `json:"command_hash,omitempty"`
	NextSuggestions   []string           `json:"next_suggestions,omitempty"`
}

func (t *BashTool) executeRun(ctx context.Context, args bashArgs) (string, error) {
	if len(args.Command) == 0 || len(bytes.TrimSpace([]byte(args.Command))) == 0 {
		return "", errors.New("bash requires command")
	}
	if !t.env.BypassToolHardProtections() {
		if reason, ok := blockedShellGitCommandReason(args.Command); ok {
			return "", fmt.Errorf("bash refuses unsafe git command (%s). Use safe shell git commands such as git status/diff/log, explicit-path git add, or git commit with an explicit non-interactive message: error_kind=unsupported_git_shell model_next_action=%q", reason, "retry with a safe git shell command")
		}
		if shellCommandUsesUnsupportedWrapper(args.Command) {
			return "", errors.New("bash refuses unsupported shell wrapper syntax because it cannot prove which command will execute; retry with a plain command or a supported timeout/time/nice/nohup/stdbuf form")
		}
		if shellCommandDumpsEnvironment(args.Command) {
			return "", errors.New("bash refuses to print process environment variables because they may contain secrets")
		}
		if reason, ok := shellCommandSensitivePathReason(args.Command); ok {
			return "", fmt.Errorf("bash refuses to access sensitive paths (%s). Use dedicated metadata-safe tools or ask the user for explicit secret handling", reason)
		}
		if shellCommandInvokesDestructiveCommand(args.Command) {
			return "", errors.New("bash refuses to execute destructive shell commands; use the file editing tool exposed in this session or another restricted audited tool so changes remain reviewable")
		}
		if shellCommandInvokesPackageOrNetworkMutation(args.Command) && !shellCommandPackageOrNetworkMutationCoveredByCommandPolicy(args.Command) {
			return "", errors.New("bash refuses to execute package, network, or external mutation commands; use dedicated web tools or ask the user for explicit approval")
		}
	}

	command := strings.TrimSpace(args.Command)
	runCWD, err := resolveShellWorkingDir(t.env, args.CWD)
	if err != nil {
		return "", err
	}
	verification := bashCommandLooksLikeVerification(command)
	resolved := resolvedRunTestCommand{Requested: command, Command: command}
	if verification {
		resolved, err = resolveRunTestCommand(runCWD, command)
		if err != nil {
			return "", err
		}
		command = resolved.Command
	}

	timeout := args.TimeoutSeconds
	if timeout <= 0 {
		timeout = defaultShellTimeoutSeconds
	}
	if timeout > maxShellTimeoutSeconds {
		timeout = maxShellTimeoutSeconds
	}

	revision := workspaceRevision(ctx, t.env.RootDir)
	commandHash := sha256Hex([]byte(command))
	if verification && revision != "" {
		previousFailures := t.env.ConsecutiveTestFailures(commandHash, revision)
		if previousFailures >= maxRepeatedRunTestFailures {
			return "", repeatedBashVerificationFailureError{
				PreviousFailures: previousFailures,
				MaxFailures:      maxRepeatedRunTestFailures,
				Revision:         revision,
				CommandHash:      commandHash,
			}
		}
	}

	result, err := executeShellCommandWithCWD(ctx, t.env, command, timeout, runCWD)
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
	if verification {
		result.Verification = t.enrichVerificationResult(command, commandHash, revision, args, result)
		result.NextSuggestions = result.Verification.NextSuggestions
		if resolved.Changed {
			result.RequestedCommand = t.env.RedactToolOutput(resolved.Requested)
			result.ResolvedCommand = result.Command
		}
	}
	return mustJSON(result)
}

func bashCommandLooksLikeVerification(command string) bool {
	if testCommandLooksLikeLocalRunnerVerification(command) {
		return true
	}
	classification := classifyShellCommand(command)
	return classification.Risk == ToolRiskMedium && classification.Reason == "local verification command"
}

func (t *BashTool) enrichVerificationResult(command, commandHash, revision string, args bashArgs, shellResult shellExecutionResult) *bashVerificationResult {
	scope := strings.TrimSpace(args.Scope)
	if scope == "" {
		scope = "targeted"
	}
	failureSummary := summarizeTestFailure(shellResult.Output)
	if shellResult.ExitCode != 0 {
		failureSummary.Failed = true
	}
	failed := shellResult.ExitCode != 0 || shellResult.TimedOut || failureSummary.Failed
	previousFailures := 0
	if revision != "" {
		previousFailures = t.env.ConsecutiveTestFailures(commandHash, revision)
	}
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
		FullLogRef:     shellResult.FullLogRef,
	})
	return &bashVerificationResult{
		Kind:              "verification",
		Scope:             scope,
		Passed:            shellResult.ExitCode == 0 && !shellResult.TimedOut,
		FailureSummary:    failureSummary,
		WorkspaceRevision: revision,
		CommandHash:       commandHashPrefix(commandHash),
		RepeatGuard: map[string]any{
			"previous_failed_runs":                    previousFailures,
			"max_failed_runs_without_revision_change": maxRepeatedRunTestFailures,
		},
		NextSuggestions: runTestNextSuggestions(shellResult, failureSummary),
	}
}

type repeatedBashVerificationFailureError struct {
	PreviousFailures int
	MaxFailures      int
	Revision         string
	CommandHash      string
}

func (e repeatedBashVerificationFailureError) Error() string {
	return fmt.Sprintf(
		"bash blocked repeated failing verification command: error_kind=repeated_failure_same_revision previous_failed_runs=%d max_failed_runs_without_revision_change=%d workspace_revision=%s command_hash=%s safe_retry=%q model_next_action=%q",
		e.PreviousFailures,
		e.MaxFailures,
		e.Revision,
		commandHashPrefix(e.CommandHash),
		"change code, narrow the command, or inspect verification.failure_summary/full_log_ref before rerunning",
		"read the latest failure evidence, form a new hypothesis, patch minimally, then rerun targeted verification after the workspace revision changes",
	)
}

func (t *BashTool) executeStartBackground(ctx context.Context, args bashArgs) (string, error) {
	if strings.TrimSpace(args.Command) == "" {
		return "", errors.New("bash requires command")
	}
	if !t.env.BypassToolHardProtections() {
		if shellCommandInvokesGit(args.Command) {
			return "", errors.New("bash background mode refuses to execute git commands because git operations should be short-lived and non-interactive. Use action=run with a safe git shell command")
		}
		if shellCommandUsesUnsupportedWrapper(args.Command) {
			return "", errors.New("bash background mode refuses unsupported shell wrapper syntax because it cannot prove which command will execute; retry with a plain command or a supported timeout/time/nice/nohup/stdbuf form")
		}
		if shellCommandDumpsEnvironment(args.Command) {
			return "", errors.New("bash background mode refuses to print process environment variables because they may contain secrets")
		}
		if reason, ok := shellCommandSensitivePathReason(args.Command); ok {
			return "", errors.New("bash background mode refuses to access sensitive paths (" + reason + "). Use dedicated metadata-safe tools or ask the user for explicit secret handling")
		}
		if shellCommandInvokesDestructiveCommand(args.Command) {
			return "", errors.New("bash background mode refuses to execute destructive shell commands; use the file editing tool exposed in this session or another restricted audited tool so changes remain reviewable")
		}
		if shellCommandInvokesPackageOrNetworkMutation(args.Command) {
			return "", errors.New("bash background mode refuses to execute package, network, or external mutation commands; use a bounded bash run or ask the user for explicit approval")
		}
	}
	args.OwnerKind = defaultProcessOwnerKind(t.env, args.OwnerKind)
	if strings.TrimSpace(args.OwnerID) == "" {
		args.OwnerID = defaultProcessOwnerID(t.env)
	}
	m, err := t.env.ProcessManager()
	if err != nil {
		return "", err
	}
	p, startErr := m.Start(context.WithoutCancel(ctx), proc.StartOptions{Command: args.Command, CWD: args.CWD, OwnerKind: proc.OwnerKind(args.OwnerKind), OwnerID: args.OwnerID, Lifecycle: proc.Lifecycle(args.Lifecycle), TTY: args.TTY, AllowOutsideWorkspace: t.env.BypassToolHardProtections()})
	response := startProcessResponse{}
	if p != nil {
		response.Process = redactProcess(t.env, *p)
		response.Action = bashActionStartBackground
		response.NextSuggestions = bashBackgroundNextSuggestions(args.WaitMS)
		if startErr == nil && args.WaitMS > 0 {
			wait := time.Duration(args.WaitMS) * time.Millisecond
			if wait > maxStartProcessInitialWait {
				wait = maxStartProcessInitialWait
			}
			offset := int64(0)
			snapshot, readErr := m.ReadOutputSnapshot(ctx, p.ID, proc.OutputReadOptions{
				MaxBytes:    args.MaxBytes,
				OffsetBytes: &offset,
				Wait:        wait,
			})
			if readErr != nil {
				response.LastError = t.env.RedactToolOutput(readErr.Error())
			} else {
				process := snapshot.Process
				detectedPreviewURLs := proc.PreviewURLsFromText(snapshot.Output)
				if len(detectedPreviewURLs) > 0 {
					if updated, updateErr := m.UpdatePreview(p.ID, detectedPreviewURLs, detectedPreviewURLs[0]); updateErr == nil {
						process = *updated
						response.DetectedPreviewURLs = append([]string(nil), updated.PreviewURLs...)
					}
				}
				response.Process = redactProcess(t.env, process)
				response.Action = bashActionStartBackground
				response.InitialOutput = t.env.RedactToolOutput(snapshot.Output)
				response.InitialTruncated = snapshot.Truncated
				response.InitialStartOffset = snapshot.StartOffset
				response.InitialEndOffset = snapshot.EndOffset
				response.InitialTotalBytes = snapshot.TotalBytes
				response.InitialTimedOut = snapshot.TimedOut
				response.InitialDurationMS = snapshot.Duration.Milliseconds()
			}
		}
	}
	out, _ := mustJSON(response)
	if startErr != nil {
		return out, startErr
	}
	return out, nil
}

func bashBackgroundNextSuggestions(waitMS int) []string {
	if waitMS <= 0 {
		return []string{"use bash action=read_background with offset_bytes=0 and wait_ms to wait for readiness; after a dev server prints its localhost URL, use tool_search select:report_listening_ports if needed, then report the port"}
	}
	return []string{"pass initial_end_offset as offset_bytes to bash action=read_background for incremental logs; after a dev server prints its localhost URL, use tool_search select:report_listening_ports if needed, then report the port"}
}

func (t *BashTool) executeListBackground() (string, error) {
	m, err := t.env.ProcessManager()
	if err != nil {
		return "", err
	}
	ps, err := m.List()
	if err != nil {
		return "", err
	}
	redacted := make([]proc.Process, 0, len(ps))
	for _, p := range ps {
		redacted = append(redacted, redactProcess(t.env, p))
	}
	return mustJSON(map[string]any{
		"action":    bashActionListBackground,
		"processes": redacted,
	})
}

func (t *BashTool) executeReadBackground(ctx context.Context, args bashArgs) (string, error) {
	m, err := t.env.ProcessManager()
	if err != nil {
		return "", err
	}
	snapshot, err := m.ReadOutputSnapshot(ctx, args.ProcessID, proc.OutputReadOptions{
		MaxBytes:    args.MaxBytes,
		OffsetBytes: args.OffsetBytes,
		Wait:        time.Duration(args.WaitMS) * time.Millisecond,
	})
	if err != nil {
		return "", err
	}
	process := snapshot.Process
	detectedPreviewURLs := proc.PreviewURLsFromText(snapshot.Output)
	if len(detectedPreviewURLs) > 0 {
		if updated, updateErr := m.UpdatePreview(args.ProcessID, detectedPreviewURLs, detectedPreviewURLs[0]); updateErr == nil {
			process = *updated
		}
	}
	return mustJSON(map[string]any{
		"action":                bashActionReadBackground,
		"process_id":            args.ProcessID,
		"output":                t.env.RedactToolOutput(snapshot.Output),
		"detected_preview_urls": detectedPreviewURLs,
		"truncated":             snapshot.Truncated,
		"start_offset":          snapshot.StartOffset,
		"end_offset":            snapshot.EndOffset,
		"total_bytes":           snapshot.TotalBytes,
		"timed_out":             snapshot.TimedOut,
		"duration_ms":           snapshot.Duration.Milliseconds(),
		"status":                process.Status,
		"exit_code":             process.ExitCode,
		"process":               redactProcess(t.env, process),
	})
}

func (t *BashTool) executeWriteStdin(args bashArgs) (string, error) {
	m, err := t.env.ProcessManager()
	if err != nil {
		return "", err
	}
	p, err := m.WriteStdin(args.ProcessID, args.Input)
	if err != nil {
		return "", err
	}
	return mustJSON(map[string]any{
		"action":        bashActionWriteBackground,
		"process_id":    args.ProcessID,
		"bytes_written": len(args.Input),
		"process":       redactProcessPtr(t.env, p),
	})
}

func (t *BashTool) executeStopBackground(args bashArgs) (string, error) {
	m, err := t.env.ProcessManager()
	if err != nil {
		return "", err
	}
	p, err := m.Stop(args.ProcessID)
	if err != nil {
		return "", err
	}
	redacted := redactProcessPtr(t.env, p)
	if redacted != nil {
		redacted.Action = bashActionStopBackground
	}
	return mustJSON(redacted)
}
