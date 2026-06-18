package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/appserver"
)

type runState struct {
	threadID            string
	turnID              string
	finalMessage        string
	tracePath           string
	status              string
	structuredResult    any
	structuredResultSet bool
}

func Run(ctx context.Context, opts Options) error {
	restoreEnv, err := applyRunEnv(opts.Env)
	if err != nil {
		return WithExitCode(ExitInvalidInput, err)
	}
	defer restoreEnv()

	attachments, err := resolveRunAttachments(opts)
	if err != nil {
		return WithExitCode(ExitInvalidInput, err)
	}
	if strings.TrimSpace(opts.Prompt) == "" && attachments.Empty() {
		return WithExitCode(ExitInvalidInput, errors.New("prompt is required"))
	}
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.PermissionMode != "" {
		// The concrete validation happens during config application. Keep this
		// branch so CLI parse tests can distinguish an explicit invalid value
		// before a model/runtime is initialized.
	}

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	rootDir, err := resolveWorkdir(opts.Workdir)
	if err != nil {
		return WithExitCode(ExitInvalidInput, err)
	}
	outputSchema, err := loadOutputSchema(rootDir, opts.OutputSchemaPath)
	if err != nil {
		return WithExitCode(ExitInvalidInput, err)
	}

	controller := opts.Controller
	if controller == nil {
		controller, err = NewLocalAppServerController(ctx, opts)
		if err != nil {
			return classifySetupError(err)
		}
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = controller.Shutdown(shutdownCtx)
	}()

	state := runState{status: "running"}
	initResult, err := controller.Initialize(ctx)
	if err != nil {
		return classifyProtocolOrContextError(ctx, err)
	}
	emitSessionConfigured(opts, initResult)

	thread, err := startOrResumeThread(ctx, controller, opts)
	if err != nil {
		return classifyProtocolOrContextError(ctx, err)
	}
	state.threadID = thread.ID
	switch {
	case strings.TrimSpace(opts.ForkID) != "":
		emitThreadEvent(opts, "thread_forked", thread)
	case opts.ResumeLast || strings.TrimSpace(opts.ResumeID) != "":
		emitThreadEvent(opts, "thread_resumed", thread)
	default:
		emitThreadEvent(opts, "thread_started", thread)
	}

	prompt := opts.Prompt
	if outputSchema != nil {
		prompt = outputSchema.initialPrompt(prompt)
	}
	maxRetries := 0
	if outputSchema != nil {
		maxRetries = outputSchemaMaxRetries
	}
	for attempt := 0; ; attempt++ {
		state.finalMessage = ""
		state.turnID = ""
		state.status = "running"
		input := TurnInput{Prompt: prompt}
		if attempt == 0 {
			input.Images = attachments.Images
			input.Files = attachments.Files
		}
		turn, err := controller.StartTurn(ctx, thread.ID, input)
		if err != nil {
			return classifyProtocolOrContextError(ctx, err)
		}
		state.turnID = turn.ID
		emitTurnStarted(opts, thread.ID, turn)

		err = waitForTurn(ctx, controller, opts, &state)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				_ = interruptBestEffort(controller, state.threadID)
				emitResult(opts, state, "timeout", "timeout")
				return WithExitCode(ExitTimeout, err)
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				_ = interruptBestEffort(controller, state.threadID)
				emitResult(opts, state, "interrupted", "interrupted")
				return WithExitCode(ExitInterrupted, err)
			}
			return err
		}

		if outputSchema == nil {
			break
		}
		structuredResult, err := outputSchema.validate(state.finalMessage)
		if err == nil {
			state.structuredResult = structuredResult
			state.structuredResultSet = true
			break
		}
		retrying := attempt < maxRetries
		emitStructuredOutputValidation(opts, state, err, retrying)
		if !retrying {
			state.status = "failed"
			emitResult(opts, state, "failed", err.Error())
			return WithExitCode(ExitTurnFailed, err)
		}
		prompt = outputSchema.retryPrompt(state.finalMessage, err)
	}

	emitResult(opts, state, "completed", "")
	if opts.OutputLastMessage != "" {
		if err := writeLastMessage(opts.OutputLastMessage, state.finalMessage); err != nil {
			return WithExitCode(ExitTurnFailed, err)
		}
	}
	if !opts.JSON {
		if state.tracePath != "" {
			fmt.Fprintf(opts.Stderr, "trace_path: %s\n", state.tracePath)
		}
		if state.finalMessage != "" {
			fmt.Fprintln(opts.Stdout, state.finalMessage)
		}
	}
	return nil
}

func applyRunEnv(entries []string) (func(), error) {
	type priorValue struct {
		value string
		ok    bool
	}
	prior := make(map[string]priorValue)
	restore := func() {
		for key, old := range prior {
			if old.ok {
				_ = os.Setenv(key, old.value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			restore()
			return func() {}, fmt.Errorf("--env must be KEY=VALUE")
		}
		if _, seen := prior[key]; !seen {
			old, existed := os.LookupEnv(key)
			prior[key] = priorValue{value: old, ok: existed}
		}
		if err := os.Setenv(key, value); err != nil {
			restore()
			return func() {}, fmt.Errorf("set env %s: %w", key, err)
		}
	}
	return restore, nil
}

func startOrResumeThread(ctx context.Context, controller Controller, opts Options) (appserver.Thread, error) {
	if id := strings.TrimSpace(opts.ForkID); id != "" {
		return controller.ForkThread(ctx, id)
	}
	if opts.ResumeLast {
		return controller.ResumeThread(ctx, "")
	}
	if id := strings.TrimSpace(opts.ResumeID); id != "" {
		return controller.ResumeThread(ctx, id)
	}
	return controller.StartThread(ctx, opts.Ephemeral)
}

func waitForTurn(ctx context.Context, controller Controller, opts Options, state *runState) error {
	notifications := controller.Notifications()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case notification, ok := <-notifications:
			if !ok {
				return WithExitCode(ExitProtocol, errors.New("app-server notification stream closed before turn completed"))
			}
			done, err := handleNotification(opts, notification, state)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

func handleNotification(opts Options, notification Notification, state *runState) (bool, error) {
	switch notification.Method {
	case appserver.NotificationAgentMessageDelta:
		var params appserver.AgentMessageDeltaNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		if state.turnID == "" || params.TurnID == state.turnID {
			state.finalMessage += params.Delta
			emitJSON(opts, map[string]any{"type": "agent_message_delta", "thread_id": params.ThreadID, "turn_id": params.TurnID, "delta": params.Delta})
		}
	case appserver.NotificationAgentMessageReplace:
		var params appserver.AgentMessageReplaceNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		if state.turnID == "" || params.TurnID == state.turnID {
			state.finalMessage = params.Text
			emitJSON(opts, map[string]any{"type": "agent_message_final", "thread_id": params.ThreadID, "turn_id": params.TurnID, "message": params.Text})
		}
	case appserver.NotificationReasoningDelta:
		var params appserver.ReasoningDeltaNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		emitJSON(opts, map[string]any{"type": "reasoning_delta", "thread_id": params.ThreadID, "turn_id": params.TurnID, "delta": params.Delta})
	case appserver.NotificationReasoningReplace:
		var params appserver.ReasoningReplaceNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		emitJSON(opts, map[string]any{"type": "reasoning_final", "thread_id": params.ThreadID, "turn_id": params.TurnID, "text": params.Text})
	case appserver.NotificationItemStarted:
		var params appserver.ItemStartedNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		emitItemStarted(opts, params)
	case appserver.NotificationItemCompleted:
		var params appserver.ItemCompletedNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		emitItemCompleted(opts, params)
	case appserver.NotificationToolCallOutput:
		var params appserver.ToolCallOutputNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		emitJSON(opts, map[string]any{"type": "tool_output_delta", "thread_id": params.ThreadID, "turn_id": params.TurnID, "item_id": params.ItemID, "delta": params.Delta})
	case appserver.NotificationTurnUsage:
		var params appserver.TurnUsageNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		emitJSON(opts, map[string]any{"type": "usage_updated", "thread_id": params.ThreadID, "turn_id": params.TurnID, "input_tokens": params.InputTokens, "output_tokens": params.OutputTokens, "cache_creation_tokens": params.CacheCreationTokens, "cache_read_tokens": params.CacheReadTokens})
	case appserver.NotificationTurnEvent:
		var params appserver.TurnEventNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		emitTurnStreamEvent(opts, params)
	case appserver.NotificationTurnCompleted:
		var params appserver.TurnCompletedNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		if params.Content != "" {
			state.finalMessage = params.Content
		}
		state.threadID = params.ThreadID
		state.turnID = params.Turn.ID
		state.tracePath = params.TracePath
		state.status = "completed"
		emitJSON(opts, map[string]any{"type": "turn_completed", "thread_id": params.ThreadID, "turn_id": params.Turn.ID, "input_tokens": params.InputTokens, "output_tokens": params.OutputTokens, "trace_path": params.TracePath})
		return true, nil
	case appserver.NotificationTurnError:
		var params appserver.TurnErrorNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		state.threadID = params.ThreadID
		state.turnID = params.TurnID
		state.status = "failed"
		emitJSON(opts, map[string]any{"type": "turn_failed", "thread_id": params.ThreadID, "turn_id": params.TurnID, "error": params.Error})
		emitResult(opts, *state, "failed", params.Error)
		return false, WithExitCode(ExitTurnFailed, errors.New(params.Error))
	}
	return false, nil
}

func emitSessionConfigured(opts Options, result appserver.InitializeResult) {
	if opts.JSON {
		emitJSON(opts, map[string]any{
			"type":             "session_configured",
			"protocol_version": result.ProtocolVersion,
			"provider":         result.Provider,
			"model":            result.Model,
			"effort":           result.Effort,
			"variant":          result.Variant,
			"workspace_root":   result.WorkspaceRoot,
			"permissions":      result.Permissions,
			"tool_policy":      result.ToolPolicy,
		})
		return
	}
	fmt.Fprintf(opts.Stderr, "provider: %s\nmodel: %s\nworkspace: %s\n", result.Provider, result.Model, result.WorkspaceRoot)
	if result.Permissions.Mode != "" {
		fmt.Fprintf(opts.Stderr, "permission_mode: %s\n", result.Permissions.Mode)
	}
}

func emitThreadEvent(opts Options, eventType string, thread appserver.Thread) {
	if opts.JSON {
		emitJSON(opts, map[string]any{"type": eventType, "thread_id": thread.ID, "model": thread.Model, "provider": thread.ModelProvider, "cwd": thread.CWD})
		return
	}
	fmt.Fprintf(opts.Stderr, "thread_id: %s\n", thread.ID)
}

func emitTurnStarted(opts Options, threadID string, turn appserver.Turn) {
	if opts.JSON {
		emitJSON(opts, map[string]any{"type": "turn_started", "thread_id": threadID, "turn_id": turn.ID})
		return
	}
	fmt.Fprintf(opts.Stderr, "turn_id: %s\n", turn.ID)
}

func emitItemStarted(opts Options, params appserver.ItemStartedNotification) {
	switch params.Item.Type {
	case appserver.ThreadItemToolCall, appserver.ThreadItemCollabAgentTool:
		emitJSON(opts, map[string]any{"type": "tool_started", "thread_id": params.ThreadID, "turn_id": params.TurnID, "item_id": params.Item.ID, "name": params.Item.Name, "arguments": params.Item.Arguments})
		if !opts.JSON && params.Item.Name != "" {
			fmt.Fprintf(opts.Stderr, "tool_started: %s\n", params.Item.Name)
		}
	}
}

func emitItemCompleted(opts Options, params appserver.ItemCompletedNotification) {
	switch params.Item.Type {
	case appserver.ThreadItemToolCall, appserver.ThreadItemCollabAgentTool:
		payload := map[string]any{"type": "tool_completed", "thread_id": params.ThreadID, "turn_id": params.TurnID, "item_id": params.Item.ID, "name": params.Item.Name, "status": params.Item.Status, "error": params.Item.Error}
		emitJSON(opts, payload)
		if !opts.JSON && params.Item.Name != "" {
			fmt.Fprintf(opts.Stderr, "tool_completed: %s\n", params.Item.Name)
		}
	}
}

func emitTurnStreamEvent(opts Options, params appserver.TurnEventNotification) {
	switch params.Event.Type {
	case "plan_update":
		emitJSON(opts, map[string]any{"type": "plan_updated", "thread_id": params.ThreadID, "turn_id": params.TurnID, "plan": params.Event.PlanUpdate})
	}
}

func emitStructuredOutputValidation(opts Options, state runState, err error, retrying bool) {
	if opts.JSON {
		emitJSON(opts, map[string]any{
			"type":      "error",
			"thread_id": state.threadID,
			"turn_id":   state.turnID,
			"error":     err.Error(),
			"retrying":  retrying,
		})
		return
	}
	if retrying {
		fmt.Fprintf(opts.Stderr, "structured_output_validation_failed: %v; retrying\n", err)
		return
	}
	fmt.Fprintf(opts.Stderr, "structured_output_validation_failed: %v\n", err)
}

func emitResult(opts Options, state runState, status, errorText string) {
	if !opts.JSON {
		return
	}
	payload := map[string]any{
		"type":          "result",
		"status":        status,
		"thread_id":     state.threadID,
		"turn_id":       state.turnID,
		"final_message": state.finalMessage,
		"trace_path":    state.tracePath,
	}
	if errorText != "" {
		payload["error"] = errorText
	}
	if state.structuredResultSet {
		payload["structured_result"] = state.structuredResult
	}
	emitJSON(opts, payload)
}

func emitJSON(opts Options, payload map[string]any) {
	if !opts.JSON || opts.Stdout == nil {
		return
	}
	enc := json.NewEncoder(opts.Stdout)
	_ = enc.Encode(payload)
}

func decodeNotification(notification Notification, dst any) error {
	if len(notification.Params) == 0 {
		return nil
	}
	if err := json.Unmarshal(notification.Params, dst); err != nil {
		return WithExitCode(ExitProtocol, fmt.Errorf("decode %s notification: %w", notification.Method, err))
	}
	return nil
}

func classifySetupError(err error) error {
	if err == nil {
		return nil
	}
	return WithExitCode(ExitInvalidInput, err)
}

func classifyProtocolOrContextError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return WithExitCode(ExitTimeout, err)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return WithExitCode(ExitInterrupted, err)
	}
	return WithExitCode(ExitProtocol, err)
}

func interruptBestEffort(controller Controller, threadID string) error {
	if controller == nil || strings.TrimSpace(threadID) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return controller.Interrupt(ctx, threadID)
}

func writeLastMessage(path string, message string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	if err := os.WriteFile(path, []byte(message), 0o644); err != nil {
		return fmt.Errorf("write last message: %w", err)
	}
	return nil
}
