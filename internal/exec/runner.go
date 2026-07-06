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
	commandItems        map[string]appserver.ThreadItem
	toolOutputs         map[string]string
	seenSubagents       map[string]bool
	permissionDenied    bool
	permissionError     string
	structuredResult    any
	structuredResultSet bool
}

func Run(ctx context.Context, opts Options) error {
	if err := validateRunOptions(opts); err != nil {
		return WithExitCode(ExitInvalidInput, err)
	}
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
		state.commandItems = nil
		state.toolOutputs = nil
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
				emitTurnInterrupted(opts, state, "timeout")
				emitResult(opts, state, "timeout", "timeout")
				return WithExitCode(ExitTimeout, err)
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				_ = interruptBestEffort(controller, state.threadID)
				emitTurnInterrupted(opts, state, "interrupted")
				emitResult(opts, state, "interrupted", "interrupted")
				return WithExitCode(ExitInterrupted, err)
			}
			return err
		}
		if state.permissionDenied {
			errorText := state.permissionError
			if errorText == "" {
				errorText = "permission denied"
			}
			state.status = "permission_denied"
			emitResult(opts, state, "permission_denied", errorText)
			return WithExitCode(ExitPermissionDenied, errors.New(errorText))
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

func validateRunOptions(opts Options) error {
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
		if isCurrentTurn(state, params.ThreadID, params.TurnID) {
			state.finalMessage += params.Delta
			emitJSON(opts, map[string]any{"type": "agent_message_delta", "thread_id": params.ThreadID, "turn_id": params.TurnID, "delta": params.Delta})
		}
	case appserver.NotificationAgentMessageReplace:
		var params appserver.AgentMessageReplaceNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		if isCurrentTurn(state, params.ThreadID, params.TurnID) {
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
		emitItemStarted(opts, params, state)
	case appserver.NotificationItemCompleted:
		var params appserver.ItemCompletedNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		emitItemCompleted(opts, params, state)
	case appserver.NotificationToolCallOutput:
		var params appserver.ToolCallOutputNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		state.appendToolOutput(params.ItemID, params.Delta)
		emitJSON(opts, map[string]any{"type": "tool_output_delta", "thread_id": params.ThreadID, "turn_id": params.TurnID, "item_id": params.ItemID, "delta": params.Delta})
		emitCommandOutputDelta(opts, params, state)
	case appserver.NotificationAgentUpdated:
		var params appserver.AgentUpdatedNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		emitSubagentUpdated(opts, params, state)
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
		emitJSON(opts, map[string]any{"type": "turn_completed", "thread_id": params.ThreadID, "turn_id": params.Turn.ID, "input_tokens": params.InputTokens, "output_tokens": params.OutputTokens, "trace_path": params.TracePath})
		if !isCurrentTurn(state, params.ThreadID, params.Turn.ID) {
			return false, nil
		}
		if params.Content != "" {
			state.finalMessage = params.Content
		}
		state.threadID = params.ThreadID
		state.turnID = params.Turn.ID
		state.tracePath = params.TracePath
		state.status = "completed"
		return true, nil
	case appserver.NotificationTurnError:
		var params appserver.TurnErrorNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		emitJSON(opts, map[string]any{"type": "turn_failed", "thread_id": params.ThreadID, "turn_id": params.TurnID, "error": params.Error})
		if !isCurrentTurn(state, params.ThreadID, params.TurnID) {
			return false, nil
		}
		state.threadID = params.ThreadID
		state.turnID = params.TurnID
		state.status = "failed"
		status := "failed"
		code := ExitTurnFailed
		switch {
		case state.permissionDenied || isPermissionFailure(params.Error):
			status = "permission_denied"
			code = ExitPermissionDenied
			state.permissionDenied = true
			if state.permissionError == "" {
				state.permissionError = params.Error
			}
		case isProviderModelFailure(params.Error):
			code = ExitProviderModelError
		case isToolFailure(params.Error):
			code = ExitToolFailed
		}
		emitResult(opts, *state, status, params.Error)
		return false, WithExitCode(code, errors.New(params.Error))
	}
	return false, nil
}

func isCurrentTurn(state *runState, threadID, turnID string) bool {
	if state == nil {
		return false
	}
	if state.threadID != "" && threadID != state.threadID {
		return false
	}
	if state.turnID != "" && turnID != state.turnID {
		return false
	}
	return true
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

func emitItemStarted(opts Options, params appserver.ItemStartedNotification, state *runState) {
	switch params.Item.Type {
	case appserver.ThreadItemToolCall, appserver.ThreadItemCollabAgentTool:
		emitJSON(opts, map[string]any{"type": "tool_started", "thread_id": params.ThreadID, "turn_id": params.TurnID, "item_id": params.Item.ID, "name": params.Item.Name, "arguments": params.Item.Arguments})
		if isCommandTool(params.Item.Name) {
			state.rememberCommandItem(params.Item)
			emitJSON(opts, commandEventPayload("command_started", params.ThreadID, params.TurnID, params.Item))
		}
		if !opts.JSON && params.Item.Name != "" {
			fmt.Fprintf(opts.Stderr, "tool_started: %s\n", params.Item.Name)
		}
	}
}

func emitItemCompleted(opts Options, params appserver.ItemCompletedNotification, state *runState) {
	switch params.Item.Type {
	case appserver.ThreadItemToolCall, appserver.ThreadItemCollabAgentTool:
		item := params.Item
		if item.Result == "" {
			item.Result = state.toolOutput(item.ID)
		}
		payload := map[string]any{"type": "tool_completed", "thread_id": params.ThreadID, "turn_id": params.TurnID, "item_id": item.ID, "name": item.Name, "status": item.Status, "error": item.Error}
		emitJSON(opts, payload)
		if isCommandTool(item.Name) {
			commandPayload := commandEventPayload("command_completed", params.ThreadID, params.TurnID, item)
			commandPayload["status"] = item.Status
			commandPayload["error"] = item.Error
			emitJSON(opts, commandPayload)
			state.forgetCommandItem(item.ID)
		}
		for _, event := range fileChangeEventsFromToolResult(params.ThreadID, params.TurnID, item) {
			emitJSON(opts, event)
		}
		if !opts.JSON && params.Item.Name != "" {
			fmt.Fprintf(opts.Stderr, "tool_completed: %s\n", params.Item.Name)
		}
	}
}

func emitCommandOutputDelta(opts Options, params appserver.ToolCallOutputNotification, state *runState) {
	item, ok := state.commandItem(params.ItemID)
	if !ok {
		return
	}
	payload := commandEventPayload("command_output_delta", params.ThreadID, params.TurnID, item)
	payload["delta"] = params.Delta
	emitJSON(opts, payload)
}

func emitSubagentUpdated(opts Options, params appserver.AgentUpdatedNotification, state *runState) {
	agent := params.Agent
	if strings.TrimSpace(agent.ID) == "" {
		return
	}
	state.ensureMaps()
	eventType := "subagent_updated"
	if !state.seenSubagents[agent.ID] {
		eventType = "subagent_started"
		state.seenSubagents[agent.ID] = true
	}
	if isTerminalAgentStatus(agent.Status) {
		eventType = "subagent_completed"
	}
	payload := map[string]any{
		"type":                  eventType,
		"thread_id":             params.ThreadID,
		"agent_id":              agent.ID,
		"agent_type":            agent.Type,
		"status":                agent.Status,
		"task_name":             agent.TaskName,
		"agent_profile":         agent.AgentProfile,
		"agent_path":            agent.AgentPath,
		"parent_id":             agent.ParentID,
		"description":           agent.Description,
		"result":                agent.Result,
		"result_path":           agent.ResultPath,
		"result_bytes":          agent.ResultBytes,
		"result_truncated":      agent.ResultTruncated,
		"error":                 agent.Error,
		"input_tokens":          agent.InputTokens,
		"output_tokens":         agent.OutputTokens,
		"cache_creation_tokens": agent.CacheCreationTokens,
		"cache_read_tokens":     agent.CacheReadTokens,
	}
	emitJSON(opts, payload)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func emitTurnInterrupted(opts Options, state runState, reason string) {
	emitJSON(opts, map[string]any{
		"type":      "turn_interrupted",
		"thread_id": state.threadID,
		"turn_id":   state.turnID,
		"reason":    reason,
	})
}

func (s *runState) ensureMaps() {
	if s.commandItems == nil {
		s.commandItems = make(map[string]appserver.ThreadItem)
	}
	if s.toolOutputs == nil {
		s.toolOutputs = make(map[string]string)
	}
	if s.seenSubagents == nil {
		s.seenSubagents = make(map[string]bool)
	}
}

func (s *runState) rememberCommandItem(item appserver.ThreadItem) {
	if strings.TrimSpace(item.ID) == "" {
		return
	}
	s.ensureMaps()
	s.commandItems[item.ID] = item
}

func (s *runState) commandItem(itemID string) (appserver.ThreadItem, bool) {
	if s == nil || s.commandItems == nil {
		return appserver.ThreadItem{}, false
	}
	item, ok := s.commandItems[itemID]
	return item, ok
}

func (s *runState) forgetCommandItem(itemID string) {
	if s == nil || s.commandItems == nil {
		return
	}
	delete(s.commandItems, itemID)
}

func (s *runState) appendToolOutput(itemID, delta string) {
	if strings.TrimSpace(itemID) == "" || delta == "" {
		return
	}
	s.ensureMaps()
	s.toolOutputs[itemID] += delta
}

func (s *runState) toolOutput(itemID string) string {
	if s == nil || s.toolOutputs == nil {
		return ""
	}
	return s.toolOutputs[itemID]
}

func isCommandTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "bash":
		return true
	default:
		return false
	}
}

func commandEventPayload(eventType, threadID, turnID string, item appserver.ThreadItem) map[string]any {
	payload := map[string]any{
		"type":      eventType,
		"thread_id": threadID,
		"turn_id":   turnID,
		"item_id":   item.ID,
		"name":      item.Name,
		"arguments": item.Arguments,
	}
	for _, key := range []string{"command", "process_id"} {
		if value := stringArgument(item.Arguments, key); value != "" {
			payload[key] = value
		}
	}
	return payload
}

func stringArgument(args, key string) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(args), &payload); err != nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func isTerminalAgentStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "failed", "cancelled", "canceled":
		return true
	default:
		return false
	}
}

func fileChangeEventsFromToolResult(threadID, turnID string, item appserver.ThreadItem) []map[string]any {
	if strings.TrimSpace(item.Result) == "" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(item.Result), &payload); err != nil {
		return nil
	}
	switch strings.TrimSpace(item.Name) {
	case "write_file", "edit_file":
		event := fileChangeEventBase(threadID, turnID, item, payload)
		if path, _ := payload["path"].(string); strings.TrimSpace(path) != "" {
			event["path"] = strings.TrimSpace(path)
			return []map[string]any{event}
		}
	case "apply_patch":
		return applyPatchFileChangeEvents(threadID, turnID, item, payload)
	case "checkpoint":
		return checkpointFileChangeEvents(threadID, turnID, item, payload)
	}
	return nil
}

func fileChangeEventBase(threadID, turnID string, item appserver.ThreadItem, result map[string]any) map[string]any {
	event := map[string]any{
		"type":      "file_changed",
		"thread_id": threadID,
		"turn_id":   turnID,
		"item_id":   item.ID,
		"tool_name": item.Name,
	}
	for _, key := range []string{"action", "old_file_sha", "new_file_sha", "workspace_revision", "patch_journal_path", "manifest_path"} {
		if value, _ := result[key].(string); strings.TrimSpace(value) != "" {
			event[key] = strings.TrimSpace(value)
		}
	}
	return event
}

func applyPatchFileChangeEvents(threadID, turnID string, item appserver.ThreadItem, result map[string]any) []map[string]any {
	files, ok := result["files"].([]any)
	if !ok {
		return fileChangeEventsFromChangedFiles(threadID, turnID, item, result)
	}
	events := make([]map[string]any, 0, len(files))
	for _, raw := range files {
		file, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		event := fileChangeEventBase(threadID, turnID, item, result)
		copyStringField(event, file, "path")
		copyStringField(event, file, "move_path")
		copyStringField(event, file, "action")
		copyStringField(event, file, "old_file_sha")
		copyStringField(event, file, "new_file_sha")
		if _, ok := event["path"]; !ok {
			continue
		}
		events = append(events, event)
	}
	return events
}

func fileChangeEventsFromChangedFiles(threadID, turnID string, item appserver.ThreadItem, result map[string]any) []map[string]any {
	changed, ok := result["changed_files"].([]any)
	if !ok {
		return nil
	}
	events := make([]map[string]any, 0, len(changed))
	for _, raw := range changed {
		path, _ := raw.(string)
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		event := fileChangeEventBase(threadID, turnID, item, result)
		event["path"] = path
		events = append(events, event)
	}
	return events
}

func checkpointFileChangeEvents(threadID, turnID string, item appserver.ThreadItem, result map[string]any) []map[string]any {
	restored, ok := result["restored_files"].([]any)
	if !ok {
		return nil
	}
	events := make([]map[string]any, 0, len(restored))
	for _, raw := range restored {
		file, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		path, _ := file["path"].(string)
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		event := fileChangeEventBase(threadID, turnID, item, result)
		event["path"] = path
		copyStringField(event, file, "action")
		events = append(events, event)
	}
	return events
}

func copyStringField(dst map[string]any, src map[string]any, key string) {
	value, _ := src[key].(string)
	if strings.TrimSpace(value) != "" {
		dst[key] = strings.TrimSpace(value)
	}
}

func isPermissionFailure(text string) bool {
	text = strings.ToLower(text)
	for _, marker := range []string{
		"error_kind=boundary_denied",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func isProviderModelFailure(text string) bool {
	text = strings.ToLower(text)
	for _, marker := range []string{
		"provider",
		"model not found",
		"unsupported model",
		"unknown model",
		"no model",
		"api key",
		"auth token",
		"unsupported wire",
		"unsupported api",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func isToolFailure(text string) bool {
	text = strings.ToLower(text)
	for _, marker := range []string{
		"tool execution failed",
		"tool call failed",
		"tool failed",
		"error_kind=tool",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func emitTurnStreamEvent(opts Options, params appserver.TurnEventNotification) {
	switch params.Event.Type {
	case "plan_update":
		emitJSON(opts, map[string]any{"type": "plan_updated", "thread_id": params.ThreadID, "turn_id": params.TurnID, "plan": params.Event.PlanUpdate})
	case "request_context":
		rc := params.Event.RequestContext
		if rc == nil {
			return
		}
		payload := map[string]any{
			"type":                        "request_context",
			"thread_id":                   params.ThreadID,
			"turn_id":                     params.TurnID,
			"step_index":                  rc.StepIndex,
			"transient_messages":          rc.TransientMessages,
			"content_bytes":               rc.ContentBytes,
			"block_kinds":                 rc.BlockKinds,
			"block_kind_counts":           rc.BlockKindCounts,
			"block_kind_bytes":            rc.BlockKindBytes,
			"segment_lifecycle_counts":    rc.SegmentLifecycleCounts,
			"segment_placement_counts":    rc.SegmentPlacementCounts,
			"segment_cache_policy_counts": rc.SegmentCachePolicyCounts,
			"message_count":               rc.MessageCount,
			"system_messages":             rc.SystemMessages,
			"hidden_messages":             rc.HiddenMessages,
			"tool_count":                  rc.ToolCount,
			"stable_prefix":               rc.StablePrefix,
			"turn_prefix":                 rc.TurnPrefix,
			"dynamic_context_bytes":       rc.DynamicBytes,
			"system_bytes":                rc.SystemBytes,
			"stable_prefix_bytes":         rc.StablePrefixBytes,
			"turn_prefix_bytes":           rc.TurnPrefixBytes,
			"message_bytes":               rc.MessageBytes,
			"tool_schema_bytes":           rc.ToolSchemaBytes,
			"loadable_tool_count":         rc.LoadableToolCount,
			"loadable_tool_schema_bytes":  rc.LoadableToolSchemaBytes,
			"loadable_tool_surface_hash":  rc.LoadableToolSurfaceHash,
			"system_hash":                 rc.SystemHash,
			"stable_prefix_hash":          rc.StablePrefixHash,
			"turn_prefix_hash":            rc.TurnPrefixHash,
			"tool_surface_hash":           rc.ToolSurfaceHash,
			"prompt_cache_key":            rc.PromptCacheKey,
		}
		if len(rc.SystemSections) > 0 {
			payload["system_sections"] = rc.SystemSections
		}
		emitJSON(opts, payload)
	case "provider_state":
		state := params.Event.ProviderState
		if state == nil {
			return
		}
		emitJSON(opts, map[string]any{
			"type":                      "provider_state",
			"thread_id":                 params.ThreadID,
			"turn_id":                   params.TurnID,
			"step_index":                state.StepIndex,
			"provider":                  state.Provider,
			"protocol":                  state.Protocol,
			"transport":                 state.Transport,
			"replay_mode":               state.ReplayMode,
			"previous_response_id_used": state.PreviousResponseIDUsed,
			"connection_reused":         state.ConnectionReused,
			"diagnostic":                state.Diagnostic,
			"transport_failure_phase":   state.TransportFailurePhase,
			"fallback_transport":        state.FallbackTransport,
			"events_emitted":            state.EventsEmitted,
			"fallback_active":           state.FallbackActive,
			"fallback_reason":           state.FallbackReason,
			"input_items":               state.InputItems,
			"full_input_items":          state.FullInputItems,
			"delta_input_items":         state.DeltaInputItems,
		})
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
	if isProviderModelFailure(err.Error()) {
		return WithExitCode(ExitProviderModelError, err)
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
