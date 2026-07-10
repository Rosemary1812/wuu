package exec

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/appserver"
)

// runParticipantTurn runs a single turn AS a named conversation participant
// inside a group/DM thread. Unlike the SurfaceMain path, the named-agent run
// is spawned asynchronously by participant/start, so completion is observed on
// the notification stream (agent/updated reaching a terminal status) rather
// than the turn/completed contract that waitForTurn keys on.
func runParticipantTurn(ctx context.Context, controller Controller, opts Options, state *runState) error {
	threadID := strings.TrimSpace(opts.Participant.ThreadID)
	if threadID == "" {
		// No explicit thread: start a fresh persistent thread to host the
		// named run. A brand-new thread still gets agent control, which is
		// all participant/start needs to mount the group-chat surface.
		thread, err := controller.StartThread(ctx, opts.Ephemeral)
		if err != nil {
			return classifyProtocolOrContextError(ctx, err)
		}
		threadID = thread.ID
		emitThreadEvent(opts, "thread_started", thread)
	}
	state.threadID = threadID

	params := appserver.ParticipantStartParams{
		ThreadID:      threadID,
		ParticipantID: strings.TrimSpace(opts.Participant.ParticipantID),
		Prompt:        opts.Prompt,
		TaskName:      strings.TrimSpace(opts.Participant.TaskName),
		Description:   strings.TrimSpace(opts.Participant.Description),
		SubagentType:  strings.TrimSpace(opts.Participant.SubagentType),
		AgentProfile:  strings.TrimSpace(opts.Participant.AgentProfile),
		Isolation:     strings.TrimSpace(opts.Participant.Isolation),
	}
	state.participantID = params.ParticipantID
	agent, err := controller.StartParticipantTurn(ctx, params)
	if err != nil {
		return classifyProtocolOrContextError(ctx, err)
	}
	agentID := strings.TrimSpace(agent.ID)
	if agentID == "" {
		return WithExitCode(ExitProtocol, errors.New("participant/start returned no agent id"))
	}
	// Address the result/notification stream to the spawned run.
	state.turnID = agentID
	emitParticipantTurnStarted(opts, threadID, params.ParticipantID, agent)

	if err := waitForParticipantRun(ctx, controller, opts, state, agentID); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			_ = interruptBestEffort(controller, threadID)
			emitTurnInterrupted(opts, *state, "timeout")
			emitResult(opts, *state, "timeout", "timeout")
			return WithExitCode(ExitTimeout, err)
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			_ = interruptBestEffort(controller, threadID)
			emitTurnInterrupted(opts, *state, "interrupted")
			emitResult(opts, *state, "interrupted", "interrupted")
			return WithExitCode(ExitInterrupted, err)
		}
		return err
	}

	if state.status == "failed" {
		errorText := state.finalMessage
		if strings.TrimSpace(state.permissionError) != "" {
			errorText = state.permissionError
		}
		if strings.TrimSpace(errorText) == "" {
			errorText = "participant run failed"
		}
		emitResult(opts, *state, "failed", errorText)
		return WithExitCode(ExitTurnFailed, errors.New(errorText))
	}

	emitResult(opts, *state, "completed", "")
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

// waitForParticipantRun blocks until the named-agent run identified by agentID
// reaches a terminal status on the agent/updated stream. Standard streaming
// events (tool calls, subagent updates, usage) are emitted via the shared
// handleNotification path; participant messages the named agent posts and the
// terminal agent result are captured into state.
func waitForParticipantRun(ctx context.Context, controller Controller, opts Options, state *runState, agentID string) error {
	notifications := controller.Notifications()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case notification, ok := <-notifications:
			if !ok {
				return WithExitCode(ExitProtocol, errors.New("app-server notification stream closed before participant run completed"))
			}
			// Reuse the shared handler for its emission side effects (tool
			// events, subagent start/completed, usage). Its turn/completed and
			// turn/error termination cannot fire here: the named run's turns
			// live on the agent's own thread id, so isCurrentTurn (keyed on the
			// parent thread) rejects them.
			if _, err := handleNotification(opts, notification, state); err != nil {
				return err
			}
			done, err := handleParticipantNotification(opts, notification, state, agentID)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

func handleParticipantNotification(opts Options, notification Notification, state *runState, agentID string) (bool, error) {
	switch notification.Method {
	case appserver.NotificationItemCompleted:
		var params appserver.ItemCompletedNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		if params.Item.Type == appserver.ThreadItemParticipantMsg {
			emitParticipantMessage(opts, params)
			if participantMessageBelongsToRun(params, *state) {
				state.lastParticipantItem = strings.TrimSpace(params.Item.ID)
				state.lastParticipantSeq = params.Item.Seq
			}
			if text := strings.TrimSpace(params.Item.Text); text != "" {
				state.finalMessage = params.Item.Text
			}
		}
	case appserver.NotificationAgentUpdated:
		var params appserver.AgentUpdatedNotification
		if err := decodeNotification(notification, &params); err != nil {
			return false, err
		}
		if strings.TrimSpace(params.Agent.ID) != agentID {
			return false, nil
		}
		if !isTerminalAgentStatus(params.Agent.Status) {
			return false, nil
		}
		if result := strings.TrimSpace(params.Agent.Result); result != "" {
			state.finalMessage = params.Agent.Result
		}
		switch strings.TrimSpace(params.Agent.Status) {
		case "failed":
			state.status = "failed"
			if strings.TrimSpace(params.Agent.Error) != "" {
				state.finalMessage = params.Agent.Error
			}
		case "cancelled", "canceled":
			state.status = "failed"
			if strings.TrimSpace(state.finalMessage) == "" {
				state.finalMessage = "participant run cancelled"
			}
		default:
			state.status = "completed"
		}
		return true, nil
	}
	return false, nil
}

func participantMessageBelongsToRun(note appserver.ItemCompletedNotification, state runState) bool {
	if strings.TrimSpace(note.ThreadID) != strings.TrimSpace(state.threadID) {
		return false
	}
	if strings.TrimSpace(state.participantID) == "" || note.Item.Participant == nil {
		return false
	}
	return strings.TrimSpace(note.Item.Participant.ID) == strings.TrimSpace(state.participantID)
}

func emitParticipantTurnStarted(opts Options, threadID, participantID string, agent appserver.Agent) {
	if opts.JSON {
		emitJSON(opts, map[string]any{
			"type":           "participant_turn_started",
			"thread_id":      threadID,
			"agent_id":       agent.ID,
			"participant_id": participantID,
			"task_name":      agent.TaskName,
			"status":         agent.Status,
		})
		return
	}
	fmt.Fprintf(opts.Stderr, "participant_turn: agent_id=%s participant=%s\n", agent.ID, participantID)
}

func emitParticipantMessage(opts Options, params appserver.ItemCompletedNotification) {
	if !opts.JSON {
		return
	}
	payload := map[string]any{
		"type":      "participant_message",
		"thread_id": params.ThreadID,
		"turn_id":   params.TurnID,
		"item_id":   params.Item.ID,
		"agent_id":  params.Item.AgentID,
		"post_kind": params.Item.PostKind,
		"text":      params.Item.Text,
	}
	if params.Item.Participant != nil {
		payload["participant_id"] = params.Item.Participant.ID
		payload["participant_name"] = params.Item.Participant.Name
	}
	emitJSON(opts, payload)
}
