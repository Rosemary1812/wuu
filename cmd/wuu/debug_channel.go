package main

import (
	"context"
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

func runDebugChannel(args []string) error {
	if len(args) == 0 {
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, errors.New("debug channel subcommand is required"))
	}
	switch args[0] {
	case "inspect":
		return runDebugChannelInspect(args[1:])
	case "send":
		return runDebugChannelSend(args[1:])
	default:
		return wuuexec.WithExitCode(wuuexec.ExitInvalidInput, fmt.Errorf("unknown debug channel subcommand %q", args[0]))
	}
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
