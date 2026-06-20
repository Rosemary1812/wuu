package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentthread"
	proc "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"
)

const maxStartProcessInitialWait = 60 * time.Second

// ---------------------------------------------------------------------------
// start_process
// ---------------------------------------------------------------------------

type StartProcessTool struct{ env *Env }

type startProcessResponse struct {
	proc.Process
	InitialOutput       string   `json:"initial_output,omitempty"`
	InitialTruncated    bool     `json:"initial_truncated,omitempty"`
	InitialStartOffset  int64    `json:"initial_start_offset,omitempty"`
	InitialEndOffset    int64    `json:"initial_end_offset,omitempty"`
	InitialTotalBytes   int64    `json:"initial_total_bytes,omitempty"`
	InitialTimedOut     bool     `json:"initial_timed_out,omitempty"`
	InitialDurationMS   int64    `json:"initial_duration_ms,omitempty"`
	DetectedPreviewURLs []string `json:"detected_preview_urls,omitempty"`
	NextSuggestions     []string `json:"next_suggestions,omitempty"`
}

func NewStartProcessTool(env *Env) *StartProcessTool { return &StartProcessTool{env: env} }

func (t *StartProcessTool) Name() string            { return "start_process" }
func (t *StartProcessTool) IsReadOnly() bool        { return false }
func (t *StartProcessTool) IsConcurrencySafe() bool { return false }

func (t *StartProcessTool) Classify(argsJSON string) ToolClassification {
	var args struct {
		Command string `json:"command"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return highRiskShellClassification("invalid process invocation", false)
	}
	shellClass := classifyShellCommand(args.Command)
	reason := "managed background process"
	if shellClass.Reason != "" {
		reason = "process command: " + shellClass.Reason
	}
	return ToolClassification{
		ReadOnly:        false,
		ConcurrencySafe: false,
		Destructive:     shellClass.Destructive,
		Risk:            ToolRiskHigh,
		Reason:          reason,
	}
}

func (t *StartProcessTool) ValidateInput(argsJSON string) error {
	var args struct {
		Command string `json:"command"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return err
	}
	if strings.TrimSpace(args.Command) == "" {
		return errors.New("start_process requires command")
	}
	return nil
}

func (t *StartProcessTool) PermissionRequests(argsJSON string) []ToolPermissionRequest {
	var args struct {
		Command string `json:"command"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return nil
	}
	return []ToolPermissionRequest{shellPermissionRequest("start_process", args.Command)}
}

func (t *StartProcessTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "start_process", Description: "Start a managed background OS process in the workspace. Use this instead of run_shell or shell '&' for dev servers, watch modes, and other long-lived commands. Commands that dump environment variables or touch sensitive credential paths are rejected.",
		InputSchema: objectSchema(
			map[string]any{
				"command":    stringSchema("Command to run. Do not append '&'; this tool already keeps the process in the background."),
				"cwd":        stringSchema("Working directory. Defaults to the workspace root."),
				"owner_kind": stringEnumSchema("main_agent", "subagent"),
				"owner_id":   stringSchema("Optional owner id. Defaults to the current agent/session id."),
				"lifecycle":  stringEnumSchema("session", "managed"),
				"tty":        booleanSchema("Run the command inside a pseudo-terminal. Use for commands that require terminal semantics."),
				"wait_ms":    integerSchema("Optional initial wait for output after starting, in milliseconds. Use 10000-30000 for dev servers while waiting for a ready line or localhost URL. Maximum 60000."),
				"max_bytes":  integerSchema("Maximum initial output bytes to return when wait_ms is set. Default 32768."),
			},
			"command",
		),
	}
}

func (t *StartProcessTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Command   string `json:"command"`
		CWD       string `json:"cwd"`
		OwnerKind string `json:"owner_kind"`
		OwnerID   string `json:"owner_id"`
		Lifecycle string `json:"lifecycle"`
		TTY       bool   `json:"tty"`
		WaitMS    int    `json:"wait_ms"`
		MaxBytes  int    `json:"max_bytes"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if args.Command == "" {
		return "", errors.New("start_process requires command")
	}
	if shellCommandInvokesGit(args.Command) {
		return "", errors.New("start_process refuses to execute git commands because git operations should be short-lived and non-interactive. Use bash action=run for normal git status/diff/log/add/commit workflows: error_kind=unsupported_tool_path model_next_action=\"retry with bash action=run for short-lived git\"")
	}
	if shellCommandDumpsEnvironment(args.Command) {
		return "", errors.New("start_process refuses to print process environment variables because they may contain secrets")
	}
	if reason, ok := shellCommandSensitivePathReason(args.Command); ok {
		return "", errors.New("start_process refuses to access sensitive paths (" + reason + "). Use dedicated metadata-safe tools or ask the user for explicit secret handling")
	}
	if shellCommandInvokesDestructiveCommand(args.Command) {
		return "", errors.New("start_process refuses to execute destructive shell commands; use the file editing tool exposed in this session or another restricted audited tool so changes remain reviewable")
	}
	if shellCommandInvokesPackageOrNetworkMutation(args.Command) {
		return "", errors.New("start_process refuses to execute package, network, or external mutation commands; use dedicated web tools, project-approved verification commands, or ask the user for explicit approval")
	}
	args.OwnerKind = defaultProcessOwnerKind(t.env, args.OwnerKind)
	if strings.TrimSpace(args.OwnerID) == "" {
		args.OwnerID = defaultProcessOwnerID(t.env)
	}
	m, err := t.env.ProcessManager()
	if err != nil {
		return "", err
	}
	p, startErr := m.Start(context.WithoutCancel(ctx), proc.StartOptions{Command: args.Command, CWD: args.CWD, OwnerKind: proc.OwnerKind(args.OwnerKind), OwnerID: args.OwnerID, Lifecycle: proc.Lifecycle(args.Lifecycle), TTY: args.TTY})
	response := startProcessResponse{}
	if p != nil {
		response.Process = redactProcess(*p)
		response.Action = "start_process"
		response.NextSuggestions = startProcessNextSuggestions(args.WaitMS)
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
				response.LastError = redactToolOutput(readErr.Error())
			} else {
				process := snapshot.Process
				detectedPreviewURLs := proc.PreviewURLsFromText(snapshot.Output)
				if len(detectedPreviewURLs) > 0 {
					if updated, updateErr := m.UpdatePreview(p.ID, detectedPreviewURLs, detectedPreviewURLs[0]); updateErr == nil {
						process = *updated
						response.DetectedPreviewURLs = append([]string(nil), updated.PreviewURLs...)
					}
				}
				response.Process = redactProcess(process)
				response.Action = "start_process"
				response.InitialOutput = redactToolOutput(snapshot.Output)
				response.InitialTruncated = snapshot.Truncated
				response.InitialStartOffset = snapshot.StartOffset
				response.InitialEndOffset = snapshot.EndOffset
				response.InitialTotalBytes = snapshot.TotalBytes
				response.InitialTimedOut = snapshot.TimedOut
				response.InitialDurationMS = snapshot.Duration.Milliseconds()
			}
		}
	}
	out, _ := json.Marshal(response)
	if startErr != nil {
		return string(out), startErr
	}
	return string(out), nil
}

func defaultProcessOwnerKind(env *Env, ownerKind string) string {
	ownerKind = strings.TrimSpace(ownerKind)
	if ownerKind != "" {
		return ownerKind
	}
	if currentAgentPath(env) != agentthread.RootPath {
		return string(proc.OwnerSubagent)
	}
	return string(proc.OwnerMainAgent)
}

func defaultProcessOwnerID(env *Env) string {
	if env == nil {
		return "main"
	}
	if id := strings.TrimSpace(env.AgentID); id != "" {
		return id
	}
	if id := strings.TrimSpace(env.SessionID); id != "" {
		return id
	}
	return "main"
}

func startProcessNextSuggestions(waitMS int) []string {
	if waitMS <= 0 {
		return []string{"use read_process_output with offset_bytes=0 and wait_ms to wait for readiness; call report_listening_ports after a dev server prints its localhost port"}
	}
	return []string{"pass initial_end_offset as offset_bytes to read_process_output for incremental logs; call report_listening_ports after a dev server prints its localhost port"}
}

// ---------------------------------------------------------------------------
// list_processes
// ---------------------------------------------------------------------------

type ListProcessesTool struct{ env *Env }

func NewListProcessesTool(env *Env) *ListProcessesTool { return &ListProcessesTool{env: env} }

func (t *ListProcessesTool) Name() string            { return "list_processes" }
func (t *ListProcessesTool) IsReadOnly() bool        { return true }
func (t *ListProcessesTool) IsConcurrencySafe() bool { return true }

func (t *ListProcessesTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "list_processes", Description: "List wuu-managed background OS processes.",
		InputSchema: objectSchema(nil),
	}
}

func (t *ListProcessesTool) Execute(_ context.Context, _ string) (string, error) {
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
		redacted = append(redacted, redactProcess(p))
	}
	return mustJSON(map[string]any{
		"action":    "list_processes",
		"processes": redacted,
	})
}

// ---------------------------------------------------------------------------
// stop_process
// ---------------------------------------------------------------------------

type StopProcessTool struct{ env *Env }

func NewStopProcessTool(env *Env) *StopProcessTool { return &StopProcessTool{env: env} }

func (t *StopProcessTool) Name() string            { return "stop_process" }
func (t *StopProcessTool) IsReadOnly() bool        { return false }
func (t *StopProcessTool) IsConcurrencySafe() bool { return true }

func (t *StopProcessTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "stop_process", Description: "Stop a background process by process group, graceful then kill.",
		InputSchema: objectSchema(
			map[string]any{
				"process_id": stringSchema(""),
			},
			"process_id",
		),
	}
}

func (t *StopProcessTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		ProcessID string `json:"process_id"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	m, err := t.env.ProcessManager()
	if err != nil {
		return "", err
	}
	p, err := m.Stop(args.ProcessID)
	if err != nil {
		return "", err
	}
	redacted := redactProcessPtr(p)
	if redacted != nil {
		redacted.Action = "stop_process"
	}
	return mustJSON(redacted)
}

// ---------------------------------------------------------------------------
// read_process_output
// ---------------------------------------------------------------------------

type ReadProcessOutputTool struct{ env *Env }

func NewReadProcessOutputTool(env *Env) *ReadProcessOutputTool {
	return &ReadProcessOutputTool{env: env}
}

func (t *ReadProcessOutputTool) Name() string            { return "read_process_output" }
func (t *ReadProcessOutputTool) IsReadOnly() bool        { return true }
func (t *ReadProcessOutputTool) IsConcurrencySafe() bool { return true }

func (t *ReadProcessOutputTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "read_process_output",
		Description: "Read output from a managed background process log. Use offset_bytes plus wait_ms to yield until new output is available, then pass the returned end_offset in the next call for incremental polling.",
		InputSchema: objectSchema(
			map[string]any{
				"process_id":   stringSchema("Managed process id returned by start_process."),
				"max_bytes":    integerSchema("Maximum bytes to return. Default 32768."),
				"offset_bytes": integerSchema("Optional byte offset to read from. Use the previous end_offset to read only new output."),
				"wait_ms":      integerSchema("Optional maximum time to wait for output beyond offset_bytes before returning."),
			},
			"process_id",
		),
	}
}

func (t *ReadProcessOutputTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		ProcessID   string `json:"process_id"`
		MaxBytes    int    `json:"max_bytes"`
		OffsetBytes *int64 `json:"offset_bytes"`
		WaitMS      int    `json:"wait_ms"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
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
		"action":                "read_process_output",
		"process_id":            args.ProcessID,
		"output":                redactToolOutput(snapshot.Output),
		"detected_preview_urls": detectedPreviewURLs,
		"truncated":             snapshot.Truncated,
		"start_offset":          snapshot.StartOffset,
		"end_offset":            snapshot.EndOffset,
		"total_bytes":           snapshot.TotalBytes,
		"timed_out":             snapshot.TimedOut,
		"duration_ms":           snapshot.Duration.Milliseconds(),
		"status":                process.Status,
		"exit_code":             process.ExitCode,
		"process":               redactProcess(process),
	})
}

// ---------------------------------------------------------------------------
// write_stdin
// ---------------------------------------------------------------------------

type WriteStdinTool struct{ env *Env }

func NewWriteStdinTool(env *Env) *WriteStdinTool { return &WriteStdinTool{env: env} }

func (t *WriteStdinTool) Name() string            { return "write_stdin" }
func (t *WriteStdinTool) IsReadOnly() bool        { return false }
func (t *WriteStdinTool) IsConcurrencySafe() bool { return true }

func (t *WriteStdinTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name:        "write_stdin",
		Description: "Write text to stdin for a running managed background process.",
		InputSchema: objectSchema(
			map[string]any{
				"process_id": stringSchema("Managed process id returned by start_process."),
				"input":      stringSchema("Text to write to stdin. Include a trailing newline when the process is waiting for a line."),
			},
			"process_id",
			"input",
		),
	}
}

func (t *WriteStdinTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		ProcessID string `json:"process_id"`
		Input     string `json:"input"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	m, err := t.env.ProcessManager()
	if err != nil {
		return "", err
	}
	p, err := m.WriteStdin(args.ProcessID, args.Input)
	if err != nil {
		return "", err
	}
	return mustJSON(map[string]any{
		"action":        "write_stdin",
		"process_id":    args.ProcessID,
		"bytes_written": len(args.Input),
		"process":       redactProcessPtr(p),
	})
}

func redactProcessPtr(p *proc.Process) *proc.Process {
	if p == nil {
		return nil
	}
	redacted := redactProcess(*p)
	return &redacted
}

func redactProcess(p proc.Process) proc.Process {
	p.Command = redactToolOutput(p.Command)
	p.LastError = redactToolOutput(p.LastError)
	return p
}
