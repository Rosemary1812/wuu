package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/appserver"
	"github.com/blueberrycongee/wuu/internal/channels"
	wuuexec "github.com/blueberrycongee/wuu/internal/exec"
)

const debugChannelPollInterval = 250 * time.Millisecond

type debugChannelInspectResult struct {
	Agents   []channels.NamedAgent `json:"agents"`
	Rooms    []channels.Room       `json:"rooms"`
	Room     *channels.Room        `json:"room,omitempty"`
	Messages []channels.Message    `json:"messages,omitempty"`
}

type debugChannelSendResult struct {
	Room            channels.Room      `json:"room"`
	Sent            channels.Message   `json:"sent"`
	Messages        []channels.Message `json:"messages"`
	ReplyCount      int                `json:"reply_count"`
	ExpectedReplies int                `json:"expected_replies"`
	TimedOut        bool               `json:"timed_out"`
}

type debugChannelE2EEvent struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type debugChannelE2EResult struct {
	Status        string                 `json:"status"`
	Phase         string                 `json:"phase"`
	Sandbox       bool                   `json:"sandbox"`
	SandboxDir    string                 `json:"sandbox_dir,omitempty"`
	Provider      string                 `json:"provider,omitempty"`
	Model         string                 `json:"model,omitempty"`
	WorkspaceRoot string                 `json:"workspace_root,omitempty"`
	Agent         channels.NamedAgent    `json:"agent"`
	WakeState     channels.WakeState     `json:"wake_state"`
	WakeStarted   bool                   `json:"wake_started"`
	AgentThreadID string                 `json:"agent_thread_id,omitempty"`
	AgentThread   *appserver.Thread      `json:"agent_thread,omitempty"`
	Agents        []channels.NamedAgent  `json:"agents,omitempty"`
	Room          channels.Room          `json:"room"`
	Threads       []appserver.Thread     `json:"threads,omitempty"`
	Sent          channels.Message       `json:"sent"`
	Messages      []channels.Message     `json:"messages"`
	Expected      string                 `json:"expected"`
	Matched       bool                   `json:"matched"`
	Events        []debugChannelE2EEvent `json:"events"`
	Error         string                 `json:"error,omitempty"`
	DurationMS    int64                  `json:"duration_ms"`
}

func runDebugChannel(args []string) error {
	if len(args) == 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("debug channel subcommand is required"))
	}
	switch args[0] {
	case "e2e":
		return runDebugChannelE2E(args[1:])
	case "inspect":
		return runDebugChannelInspect(args[1:])
	case "send":
		return runDebugChannelSend(args[1:])
	default:
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, fmt.Errorf("unknown debug channel subcommand %q", args[0]))
	}
}

func runDebugChannelE2E(args []string) error {
	fs := flag.NewFlagSet("debug channel e2e", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addDebugAppServerFlags(fs)
	sandbox := fs.Bool("sandbox", false, "run with isolated channel, agent, session, and trace state")
	keepSandbox := fs.Bool("keep-sandbox", false, "preserve sandbox state after the command exits")
	agentName := fs.String("agent", "E2EAgent", "named agent created for the scenario")
	messageFlag := fs.String("message", "", "human message sent to the named agent")
	expected := fs.String("expect", "E2E_OK", "substring required in the agent reply")
	timeout := fs.Duration("timeout", 2*time.Minute, "maximum time to wait for a matching reply")
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	if !*sandbox {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("debug channel e2e requires --sandbox to protect persistent channel data"))
	}
	name := strings.TrimSpace(*agentName)
	want := strings.TrimSpace(*expected)
	if name == "" || want == "" || *timeout <= 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("agent and expect must be non-empty and timeout must be positive"))
	}
	message := strings.TrimSpace(*messageFlag)
	positionalMessage := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if message != "" && positionalMessage != "" {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("message must be provided with --message or positional arguments, not both"))
	}
	if message == "" {
		message = positionalMessage
	}
	if message == "" {
		message = fmt.Sprintf("Reply with exactly %s and no other text.", want)
	}

	startedAt := time.Now()
	result := debugChannelE2EResult{
		Status: "running", Phase: "start", Sandbox: true, Expected: want,
		Messages: []channels.Message{}, Events: []debugChannelE2EEvent{},
	}
	opts := debugAppServerOptionsFromCLI(cfg)
	opts.sandbox = true
	opts.keepSandbox = *keepSandbox
	client, err := newDebugAppServerClient(context.Background(), opts)
	if err != nil {
		result.Status = "setup_failed"
		result.Error = err.Error()
		return finishDebugChannelE2E(result, startedAt, wuuexec.ExitTurnFailed, err)
	}
	defer shutdownDebugClient(client)
	result.SandboxDir = client.SandboxDir()

	result.Phase = "initialize"
	var initialized appserver.InitializeResult
	if err := client.Call(context.Background(), appserver.MethodInitialize, nil, &initialized); err != nil {
		result.Status = "setup_failed"
		result.Error = err.Error()
		return finishDebugChannelE2E(result, startedAt, wuuexec.ExitTurnFailed, err)
	}
	result.Provider = initialized.Provider
	result.Model = initialized.Model
	result.WorkspaceRoot = initialized.WorkspaceRoot

	result.Phase = "create_agent"
	var createdAgent appserver.ChannelAgentCreateResult
	if err := client.Call(context.Background(), appserver.MethodChannelAgentCreate, appserver.ChannelAgentCreateParams{Name: name}, &createdAgent); err != nil {
		result.Status = "setup_failed"
		result.Error = err.Error()
		return finishDebugChannelE2E(result, startedAt, wuuexec.ExitTurnFailed, err)
	}
	result.Agent = createdAgent.Agent

	result.Phase = "create_room"
	var createdRoom appserver.ChannelRoomCreateResult
	if err := client.Call(context.Background(), appserver.MethodChannelRoomCreate, appserver.ChannelRoomCreateParams{
		Name: "E2E", AgentIDs: []string{createdAgent.Agent.ID},
	}, &createdRoom); err != nil {
		result.Status = "setup_failed"
		result.Error = err.Error()
		return finishDebugChannelE2E(result, startedAt, wuuexec.ExitTurnFailed, err)
	}
	result.Room = createdRoom.Room

	body := message
	mention := "@" + createdAgent.Agent.Name
	if !strings.Contains(strings.ToLower(body), strings.ToLower(mention)) {
		body = mention + " " + body
	}
	result.Phase = "send"
	var sent appserver.ChannelMessageSendResult
	if err := client.Call(context.Background(), appserver.MethodChannelMessageSend, appserver.ChannelMessageSendParams{
		RoomID: createdRoom.Room.ID, Body: body,
	}, &sent); err != nil {
		result.Status = "send_failed"
		result.Error = err.Error()
		return finishDebugChannelE2E(result, startedAt, wuuexec.ExitTurnFailed, err)
	}
	result.Sent = sent.Message
	// The desktop exposes channel/agent/start as the explicit recovery/start
	// path. Calling it after the human send keeps the normal automatic wake in
	// play, while synchronously surfacing setup/admission failures that the wake
	// sink can otherwise only record in debug.log.
	result.Phase = "start_agent"
	var startedAgent appserver.ChannelAgentStartResult
	if err := client.Call(context.Background(), appserver.MethodChannelAgentStart, appserver.ChannelAgentStartParams{
		AgentID: createdAgent.Agent.ID,
	}, &startedAgent); err != nil {
		result.Status = "wake_failed"
		result.Error = err.Error()
		return finishDebugChannelE2E(result, startedAt, wuuexec.ExitTurnFailed, err)
	}
	result.WakeState = startedAgent.WakeState
	result.WakeStarted = startedAgent.Started
	result.AgentThreadID = startedAgent.ThreadID
	result.Phase = "wait"

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	turnCompleted := false
	turnTerminalProbed := false
	for {
		result.Events, turnCompleted = drainDebugChannelE2EEvents(client.Notifications(), result.Events, turnCompleted)
		if debugChannelE2EHasEvent(result.Events, appserver.NotificationTurnError) {
			result.Status = "turn_failed"
			result.Error = "named agent turn reported an error"
			return finishDebugChannelE2E(result, startedAt, wuuexec.ExitTurnFailed, errors.New(result.Error))
		}

		var listed appserver.ChannelMessageListResult
		if err := client.Call(ctx, appserver.MethodChannelMessageList, appserver.ChannelMessageListParams{
			RoomID: createdRoom.Room.ID, AfterSeq: sent.Message.Seq, Limit: 500,
		}, &listed); err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				result.Status = "read_failed"
				result.Error = err.Error()
				return finishDebugChannelE2E(result, startedAt, wuuexec.ExitTurnFailed, err)
			}
		} else {
			result.Messages = listed.Messages
			for _, reply := range listed.Messages {
				if reply.AuthorID == createdAgent.Agent.ID && strings.Contains(reply.Body, want) {
					result.Status = "passed"
					result.Phase = "complete"
					result.Matched = true
					result.Events, _ = drainDebugChannelE2EEvents(client.Notifications(), result.Events, turnCompleted)
					return finishDebugChannelE2E(result, startedAt, wuuexec.ExitOK, nil)
				}
			}
			if turnCompleted && countDebugChannelAgentMessages(listed.Messages) > 0 {
				result.Status = "expectation_failed"
				result.Phase = "validate"
				result.Error = fmt.Sprintf("agent replied without expected text %q", want)
				return finishDebugChannelE2E(result, startedAt, wuuexec.ExitTurnFailed, errors.New(result.Error))
			}
		}
		if !turnTerminalProbed {
			terminal, failed := probeDebugChannelE2ETerminalTurn(client, createdAgent.Agent.ID, &result)
			turnTerminalProbed = terminal
			if failed != nil {
				result.Status = "turn_failed"
				result.Phase = "provider"
				result.Error = failed.Message
				return finishDebugChannelE2E(result, startedAt, wuuexec.ExitTurnFailed, errors.New(failed.Message))
			}
		}
		result.AgentThread = nil

		select {
		case <-ctx.Done():
			result.Events, _ = drainDebugChannelE2EEvents(client.Notifications(), result.Events, turnCompleted)
			captureDebugChannelE2EState(client, &result)
			if countDebugChannelAgentMessages(result.Messages) > 0 {
				result.Status = "expectation_failed"
				result.Phase = "validate"
				result.Error = fmt.Sprintf("agent replied without expected text %q", want)
				return finishDebugChannelE2E(result, startedAt, wuuexec.ExitTurnFailed, errors.New(result.Error))
			}
			result.Status = "timed_out"
			result.Error = "timed out waiting for named agent reply"
			return finishDebugChannelE2E(result, startedAt, wuuexec.ExitTimeout, errors.New(result.Error))
		case <-time.After(debugChannelPollInterval):
		}
	}
}

func captureDebugChannelE2EState(client debugAppServerClient, result *debugChannelE2EResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var agents appserver.ChannelAgentListResult
	if err := client.Call(ctx, appserver.MethodChannelAgentList, nil, &agents); err == nil {
		result.Agents = agents.Agents
	}
	var threads appserver.ThreadListResult
	if err := client.Call(ctx, appserver.MethodThreadList, appserver.ThreadListParams{}, &threads); err == nil {
		result.Threads = threads.Threads
	}
	captureDebugChannelE2EAgentThreadWithContext(ctx, client, result)
}

func probeDebugChannelE2ETerminalTurn(client debugAppServerClient, agentID string, result *debugChannelE2EResult) (bool, *appserver.TurnError) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var listed appserver.ChannelAgentListResult
	if err := client.Call(ctx, appserver.MethodChannelAgentList, nil, &listed); err != nil {
		return false, nil
	}
	idle := false
	for _, agent := range listed.Agents {
		if agent.ID == agentID {
			idle = agent.ActivityStatus == "idle"
			break
		}
	}
	if !idle {
		return false, nil
	}
	captureDebugChannelE2EAgentThreadWithContext(ctx, client, result)
	if result.AgentThread == nil || len(result.AgentThread.Turns) == 0 {
		return false, nil
	}
	turn := result.AgentThread.Turns[len(result.AgentThread.Turns)-1]
	if turn.Status == appserver.TurnStatusFailed {
		if turn.Error != nil {
			return true, turn.Error
		}
		return true, &appserver.TurnError{Message: "named agent turn failed"}
	}
	return turn.Status == appserver.TurnStatusCompleted, nil
}

func captureDebugChannelE2EAgentThreadWithContext(ctx context.Context, client debugAppServerClient, result *debugChannelE2EResult) {
	if result.AgentThreadID == "" {
		return
	}
	var resumed appserver.ThreadResumeResult
	if err := client.Call(ctx, appserver.MethodThreadResume, appserver.ThreadResumeParams{SessionID: result.AgentThreadID}, &resumed); err == nil {
		result.AgentThread = &resumed.Thread
	}
}

func drainDebugChannelE2EEvents(input <-chan wuuexec.Notification, events []debugChannelE2EEvent, completed bool) ([]debugChannelE2EEvent, bool) {
	for {
		select {
		case event, ok := <-input:
			if !ok {
				return events, completed
			}
			if event.Method == appserver.NotificationTurnStarted || event.Method == appserver.NotificationTurnCompleted || event.Method == appserver.NotificationTurnError {
				events = append(events, debugChannelE2EEvent{Method: event.Method, Params: event.Params})
			}
			if event.Method == appserver.NotificationTurnCompleted {
				completed = true
			}
		default:
			return events, completed
		}
	}
}

func debugChannelE2EHasEvent(events []debugChannelE2EEvent, method string) bool {
	for _, event := range events {
		if event.Method == method {
			return true
		}
	}
	return false
}

func finishDebugChannelE2E(result debugChannelE2EResult, startedAt time.Time, exitCode int, runErr error) error {
	result.DurationMS = time.Since(startedAt).Milliseconds()
	if err := printJSON(result); err != nil {
		return err
	}
	if runErr == nil {
		return nil
	}
	return wuuexec.WithExitCode(exitCode, runErr)
}

func runDebugChannelInspect(args []string) error {
	fs := flag.NewFlagSet("debug channel inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addDebugAppServerFlags(fs)
	roomSelector := fs.String("room", "", "room id or unique room name")
	afterSeq := fs.Int64("after", 0, "list messages after this sequence")
	limit := fs.Int("limit", 100, "maximum messages to return")
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	if fs.NArg() != 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("debug channel inspect does not accept positional arguments"))
	}
	if *afterSeq < 0 || *limit <= 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("after must be non-negative and limit must be positive"))
	}

	client, err := newDebugAppServerClient(context.Background(), debugAppServerOptionsFromCLI(cfg))
	if err != nil {
		return err
	}
	defer shutdownDebugClient(client)

	bootstrap, err := debugChannelBootstrap(context.Background(), client)
	if err != nil {
		return err
	}
	result := debugChannelInspectResult{Agents: bootstrap.Agents, Rooms: bootstrap.Rooms}
	if strings.TrimSpace(*roomSelector) != "" {
		room, err := resolveDebugChannelRoom(bootstrap.Rooms, *roomSelector)
		if err != nil {
			return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
		}
		var listed appserver.ChannelMessageListResult
		if err := client.Call(context.Background(), appserver.MethodChannelMessageList, appserver.ChannelMessageListParams{
			RoomID: room.ID, AfterSeq: *afterSeq, Limit: *limit,
		}, &listed); err != nil {
			return err
		}
		result.Room = &room
		result.Messages = listed.Messages
	}
	return printJSON(result)
}

func runDebugChannelSend(args []string) error {
	fs := flag.NewFlagSet("debug channel send", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := addDebugAppServerFlags(fs)
	roomSelector := fs.String("room", "", "room id or unique room name")
	wait := fs.Duration("wait", 0, "maximum time to wait for agent replies")
	expectedReplies := fs.Int("replies", 1, "agent replies required before wait completes")
	if err := fs.Parse(args); err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	body := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if strings.TrimSpace(*roomSelector) == "" {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("room is required"))
	}
	if body == "" {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("message is required"))
	}
	if *wait < 0 || *expectedReplies <= 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("wait must be non-negative and replies must be positive"))
	}

	client, err := newDebugAppServerClient(context.Background(), debugAppServerOptionsFromCLI(cfg))
	if err != nil {
		return err
	}
	defer shutdownDebugClient(client)
	bootstrap, err := debugChannelBootstrap(context.Background(), client)
	if err != nil {
		return err
	}
	room, err := resolveDebugChannelRoom(bootstrap.Rooms, *roomSelector)
	if err != nil {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, err)
	}
	var sent appserver.ChannelMessageSendResult
	if err := client.Call(context.Background(), appserver.MethodChannelMessageSend, appserver.ChannelMessageSendParams{
		RoomID: room.ID, Body: body,
	}, &sent); err != nil {
		return err
	}

	result := debugChannelSendResult{
		Room: room, Sent: sent.Message, Messages: []channels.Message{}, ExpectedReplies: *expectedReplies,
	}
	if *wait == 0 {
		return printJSON(result)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *wait)
	defer cancel()
	for {
		var listed appserver.ChannelMessageListResult
		if err := client.Call(ctx, appserver.MethodChannelMessageList, appserver.ChannelMessageListParams{
			RoomID: room.ID, AfterSeq: sent.Message.Seq, Limit: 500,
		}, &listed); err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				return err
			}
		} else {
			result.Messages = listed.Messages
			result.ReplyCount = countDebugChannelAgentMessages(listed.Messages)
			if result.ReplyCount >= *expectedReplies {
				return printJSON(result)
			}
		}

		select {
		case <-ctx.Done():
			result.TimedOut = true
			if err := printJSON(result); err != nil {
				return err
			}
			return wuuexec.WithExitCode(wuuexec.ExitTimeout, fmt.Errorf("timed out waiting for %d agent reply/replies", *expectedReplies))
		case <-time.After(debugChannelPollInterval):
		}
	}
}

func debugChannelBootstrap(ctx context.Context, client debugAppServerClient) (appserver.ChannelBootstrapResult, error) {
	var result appserver.ChannelBootstrapResult
	err := client.Call(ctx, appserver.MethodChannelBootstrap, nil, &result)
	return result, err
}

func resolveDebugChannelRoom(rooms []channels.Room, selector string) (channels.Room, error) {
	selector = strings.TrimSpace(selector)
	for _, room := range rooms {
		if room.ID == selector {
			return room, nil
		}
	}
	matches := make([]channels.Room, 0, 1)
	for _, room := range rooms {
		if strings.EqualFold(strings.TrimSpace(room.Name), selector) {
			matches = append(matches, room)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return channels.Room{}, fmt.Errorf("room name %q is ambiguous; use a room id", selector)
	}
	return channels.Room{}, fmt.Errorf("room %q not found", selector)
}

func countDebugChannelAgentMessages(messages []channels.Message) int {
	count := 0
	for _, message := range messages {
		if message.AuthorType == channels.MemberAgent {
			count++
		}
	}
	return count
}
