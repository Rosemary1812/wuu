package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/channels"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/tools"
)

func TestServerOpensIndependentChannelsStore(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	server := NewWithCredentialStore(rt, &lockedBuffer{}, nil, nil)
	if server.startupErr != nil {
		t.Fatalf("NewWithCredentialStore() startup error = %v", server.startupErr)
	}
	if server.channelService == nil {
		t.Fatal("server channel service is nil")
	}
	if got, want := server.channelService.Dir(), statepath.ChannelsDir(rt.WuuHome); got != want {
		t.Fatalf("channels dir = %q, want %q", got, want)
	}
	credential, err := server.channelService.CreateNamedAgent(context.Background(), channels.CreateNamedAgentParams{Name: "Alpha"})
	if err != nil {
		t.Fatalf("CreateNamedAgent() error = %v", err)
	}
	if credential.Agent.ID == "" || credential.Token == "" {
		t.Fatalf("created credential = %#v", credential)
	}

	service := server.channelService
	server.Close()
	if _, err := service.ListNamedAgents(context.Background()); err == nil {
		t.Fatal("channels store remained usable after Server.Close")
	}
}

func TestChannelAgentRPCPersistsEffortOverride(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	out := &lockedBuffer{}
	server := NewWithCredentialStore(rt, out, nil, nil)
	t.Cleanup(server.Close)

	var created ChannelAgentCreateResult
	callChannelRPC(t, server, out, MethodChannelAgentCreate, ChannelAgentCreateParams{
		Name: "Reasoner", ProviderOverride: "openai", ModelOverride: "gpt-reasoner", EffortOverride: "high",
	}, &created)
	if created.Agent.EffortOverride != "high" {
		t.Fatalf("created agent = %#v, want effort override high", created.Agent)
	}

	var updated ChannelAgentUpdateResult
	callChannelRPC(t, server, out, MethodChannelAgentUpdate, ChannelAgentUpdateParams{
		AgentID: created.Agent.ID, Name: created.Agent.Name, ProviderOverride: "openai", ModelOverride: "gpt-reasoner", EffortOverride: "low",
	}, &updated)
	if updated.Agent.EffortOverride != "low" {
		t.Fatalf("updated agent = %#v, want effort override low", updated.Agent)
	}
}

func TestNamedAgentRuntimeSelectionAppliesModelAndEffortOverrides(t *testing.T) {
	agent := channels.NamedAgent{ProviderOverride: "openai", ModelOverride: "gpt-reasoner", EffortOverride: "high"}
	provider, model, effort := namedAgentModelSelection("default-provider", "default-model", "medium", agent)
	if provider != "openai" || model != "gpt-reasoner" || effort != "high" {
		t.Fatalf("named agent runtime selection = (%q, %q, %q)", provider, model, effort)
	}
	provider, model, effort = namedAgentModelSelection("default-provider", "default-model", "medium", channels.NamedAgent{})
	if provider != "default-provider" || model != "default-model" || effort != "medium" {
		t.Fatalf("inherited runtime selection = (%q, %q, %q)", provider, model, effort)
	}
}

func TestChannelHumanRPCsCreateRoomAndSendMessage(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	attachNamedAgentTestToolkit(t, rt)
	out := &lockedBuffer{}
	server := NewWithCredentialStore(rt, out, nil, nil)
	t.Cleanup(server.Close)

	var createdAgent ChannelAgentCreateResult
	callChannelRPC(t, server, out, MethodChannelAgentCreate, ChannelAgentCreateParams{Name: "Alpha"}, &createdAgent)
	if createdAgent.Agent.ID == "" || createdAgent.Agent.Name != "Alpha" {
		t.Fatalf("created agent = %#v", createdAgent.Agent)
	}
	if strings.Contains(out.String(), `"token"`) {
		t.Fatal("human agent-create RPC exposed the internal agent token")
	}

	var createdRoom ChannelRoomCreateResult
	const roomAvatar = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	callChannelRPC(t, server, out, MethodChannelRoomCreate, ChannelRoomCreateParams{
		Name: "Review", AvatarImage: roomAvatar, AgentIDs: []string{createdAgent.Agent.ID},
	}, &createdRoom)
	if len(createdRoom.Room.Members) != 2 || createdRoom.Room.AvatarImage != roomAvatar {
		t.Fatalf("created room members = %#v", createdRoom.Room.Members)
	}
	var updatedRoom ChannelRoomUpdateResult
	emptyAvatar := ""
	callChannelRPC(t, server, out, MethodChannelRoomUpdate, ChannelRoomUpdateParams{
		RoomID: createdRoom.Room.ID, AvatarImage: &emptyAvatar,
	}, &updatedRoom)
	if updatedRoom.Room.AvatarImage != "" {
		t.Fatalf("updated room avatar = %q", updatedRoom.Room.AvatarImage)
	}

	var sent ChannelMessageSendResult
	callChannelRPC(t, server, out, MethodChannelMessageSend, ChannelMessageSendParams{
		RoomID: createdRoom.Room.ID, Body: "@Alpha review this",
		Images: []TurnStartImage{{
			MediaType: "image/png",
			Data:      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
		}},
	}, &sent)
	if sent.Message.Seq != 1 || sent.Message.AuthorID != localChannelHumanID || len(sent.Message.Images) != 1 {
		t.Fatalf("sent message = %#v", sent.Message)
	}

	var listed ChannelMessageListResult
	callChannelRPC(t, server, out, MethodChannelMessageList, ChannelMessageListParams{RoomID: createdRoom.Room.ID}, &listed)
	if len(listed.Messages) != 1 || listed.Messages[0].Body != "@Alpha review this" || len(listed.Messages[0].Images) != 1 {
		t.Fatalf("listed messages = %#v", listed.Messages)
	}
	state, err := server.channelService.WakeState(context.Background(), createdAgent.Agent.ID)
	if err != nil || !state.Outstanding {
		t.Fatalf("agent wake state = %#v, err %v", state, err)
	}
	var createdTask ChannelTaskCreateResult
	callChannelRPC(t, server, out, MethodChannelTaskCreate, ChannelTaskCreateParams{
		RoomID: createdRoom.Room.ID, Title: "Review patch", OwnerID: createdAgent.Agent.ID,
	}, &createdTask)
	if createdTask.Task.Kind != channels.MessageTask || createdTask.Task.TaskState != string(channels.TaskStateOpen) {
		t.Fatalf("created task = %#v", createdTask.Task)
	}
	var updatedTask ChannelTaskUpdateResult
	callChannelRPC(t, server, out, MethodChannelTaskUpdate, ChannelTaskUpdateParams{
		TaskID: createdTask.Task.ID, State: string(channels.TaskStateDone),
	}, &updatedTask)
	if updatedTask.Task.TaskState != string(channels.TaskStateDone) {
		t.Fatalf("updated task = %#v", updatedTask.Task)
	}
	client, err := server.channelService.BindAgent(context.Background(), createdAgent.Agent.ID)
	if err != nil {
		t.Fatalf("BindAgent() error = %v", err)
	}
	if _, err := client.Send(context.Background(), channels.AgentSendParams{
		RoomID: createdRoom.Room.ID, Body: "@local-user finished", BasisSeq: createdTask.Task.Seq,
	}); err != nil {
		t.Fatalf("agent human mention send error = %v", err)
	}
	var mentionStatus ChannelHumanMentionStatusResult
	callChannelRPC(t, server, out, MethodChannelMentionStatus, struct{}{}, &mentionStatus)
	if mentionStatus.Count != 1 {
		t.Fatalf("human mention count = %d", mentionStatus.Count)
	}
	var mentionAck ChannelHumanMentionAckResult
	callChannelRPC(t, server, out, MethodChannelMentionAck, struct{}{}, &mentionAck)
	if mentionAck.Acknowledged != 1 {
		t.Fatalf("human mention ack = %d", mentionAck.Acknowledged)
	}
	var started ChannelAgentStartResult
	callChannelRPC(t, server, out, MethodChannelAgentStart, ChannelAgentStartParams{AgentID: createdAgent.Agent.ID}, &started)
	if started.Agent.ID != createdAgent.Agent.ID || started.WakeState.AgentID != createdAgent.Agent.ID || started.ThreadID != namedAgentSessionID(createdAgent.Agent) || server.thread(started.ThreadID) == nil {
		t.Fatalf("started named agent = %#v", started)
	}
}

func TestNamedAgentWakeAutostartCreatesIsolatedSession(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	attachNamedAgentTestToolkit(t, rt)
	server := NewWithCredentialStore(rt, &lockedBuffer{}, nil, nil)
	t.Cleanup(server.Close)
	credential, err := server.channelService.CreateNamedAgent(context.Background(), channels.CreateNamedAgentParams{
		Name: "Alpha", Autostart: true,
	})
	if err != nil {
		t.Fatalf("CreateNamedAgent() error = %v", err)
	}
	server.channelService.SetWakeSink(nil)
	room := createAppserverTestRoom(t, server.channelService, credential.Agent)
	if _, err := server.channelService.SendHuman(context.Background(), channels.HumanSendParams{
		RoomID: room.ID, HumanID: "human-1", Body: "@Alpha review",
	}); err != nil {
		t.Fatalf("SendHuman() error = %v", err)
	}
	if err := server.deliverNamedAgentWake(context.Background(), credential.Agent.ID); err != nil {
		t.Fatalf("deliverNamedAgentWake() error = %v", err)
	}

	threadID := namedAgentSessionID(credential.Agent)
	th := server.thread(threadID)
	if th == nil {
		t.Fatal("named agent thread was not created")
	}
	if th.NamedAgentID != credential.Agent.ID || th.CWD != filepath.Dir(credential.Agent.MemoryDir) {
		t.Fatalf("named agent thread identity = %#v", th)
	}
	if th.execRuntime == nil || th.execRuntime.StreamRunner == nil {
		t.Fatal("named agent execution runtime is nil")
	}
	prompt := th.execRuntime.StreamRunner.SystemPrompt
	if !strings.Contains(prompt, credential.Agent.MemoryDir) || !strings.Contains(prompt, "You are Alpha") {
		t.Fatalf("named agent prompt does not carry isolated identity:\n%s", prompt)
	}
	metadata, found, err := session.Find(rt.SessionDir, threadID)
	if err != nil || !found {
		t.Fatalf("Find(named agent session) = found %v, err %v", found, err)
	}
	if metadata.Source != namedAgentSessionSource+credential.Agent.ID {
		t.Fatalf("named agent session source = %q", metadata.Source)
	}

	offline, err := server.channelService.CreateNamedAgent(context.Background(), channels.CreateNamedAgentParams{Name: "Offline"})
	if err != nil {
		t.Fatalf("CreateNamedAgent(offline) error = %v", err)
	}
	offlineRoom := createAppserverTestRoom(t, server.channelService, offline.Agent)
	if _, err := server.channelService.SendHuman(context.Background(), channels.HumanSendParams{
		RoomID: offlineRoom.ID, HumanID: "human-1", Body: "@Offline review",
	}); err != nil {
		t.Fatalf("SendHuman(offline) error = %v", err)
	}
	if err := server.deliverNamedAgentWake(context.Background(), offline.Agent.ID); err != nil {
		t.Fatalf("deliverNamedAgentWake(offline) error = %v", err)
	}
	if server.thread(namedAgentSessionID(offline.Agent)) != nil {
		t.Fatal("non-autostart agent was started while offline")
	}
	state, err := server.channelService.WakeState(context.Background(), offline.Agent.ID)
	if err != nil || !state.Outstanding {
		t.Fatalf("offline wake state = %#v, err %v", state, err)
	}
}

func TestEnsureNamedAgentThreadRebuildsNamedRuntimeAfterLoad(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	attachNamedAgentTestToolkit(t, rt)
	server := NewWithCredentialStore(rt, &lockedBuffer{}, nil, nil)
	t.Cleanup(server.Close)
	credential, err := server.channelService.CreateNamedAgent(context.Background(), channels.CreateNamedAgentParams{Name: "Alpha"})
	if err != nil {
		t.Fatalf("CreateNamedAgent() error = %v", err)
	}
	th, err := server.ensureNamedAgentThreadLocked(credential.Agent)
	if err != nil {
		t.Fatalf("ensureNamedAgentThreadLocked() error = %v", err)
	}
	staleRuntime := th.execRuntime
	th.execRuntime = nil
	th.NamedAgentID = ""
	releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: staleRuntime})

	loaded, err := server.ensureNamedAgentThreadLocked(credential.Agent)
	if err != nil {
		t.Fatalf("ensureNamedAgentThreadLocked(loaded) error = %v", err)
	}
	if loaded != th || loaded.execRuntime == nil || loaded.NamedAgentID != credential.Agent.ID {
		t.Fatalf("rebuilt named agent thread = %#v", loaded)
	}
	wantTools := map[string]bool{
		"chat_check": false, "chat_read": false, "chat_send": false,
		"chat_draft": false, "chat_task": false, "chat_remind": false,
		"read_file": false, "list_files": false, "bash": false, "spawn_agent": false,
	}
	for _, definition := range loaded.execRuntime.Toolkit.Definitions() {
		if _, ok := wantTools[definition.Name]; ok {
			wantTools[definition.Name] = true
		}
	}
	for name, found := range wantTools {
		if !found {
			t.Fatalf("named-agent runtime omitted main/chat tool %q", name)
		}
	}
	if !strings.Contains(loaded.execRuntime.StreamRunner.SystemPrompt, credential.Agent.MemoryDir) {
		t.Fatal("loaded named-agent thread was rebuilt without chat tools or private memory prompt")
	}
}

func TestNonAutostartNamedAgentWakeLoadsExistingPersistedSession(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	attachNamedAgentTestToolkit(t, rt)
	server := NewWithCredentialStore(rt, &lockedBuffer{}, nil, nil)
	t.Cleanup(server.Close)
	credential, err := server.channelService.CreateNamedAgent(context.Background(), channels.CreateNamedAgentParams{Name: "Alpha"})
	if err != nil {
		t.Fatalf("CreateNamedAgent() error = %v", err)
	}
	thread, err := server.ensureNamedAgentThreadLocked(credential.Agent)
	if err != nil {
		t.Fatalf("ensureNamedAgentThreadLocked() error = %v", err)
	}
	server.mu.Lock()
	delete(server.threads, thread.ID)
	server.mu.Unlock()
	releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: thread.execRuntime})
	thread.execRuntime = nil

	server.channelService.SetWakeSink(nil)
	room := createAppserverTestRoom(t, server.channelService, credential.Agent)
	if _, err := server.channelService.SendHuman(context.Background(), channels.HumanSendParams{
		RoomID: room.ID, HumanID: "human-1", Body: "@Alpha resume",
	}); err != nil {
		t.Fatalf("SendHuman() error = %v", err)
	}
	if err := server.deliverNamedAgentWake(context.Background(), credential.Agent.ID); err != nil {
		t.Fatalf("deliverNamedAgentWake() error = %v", err)
	}
	loaded := server.thread(thread.ID)
	if loaded == nil || loaded.NamedAgentID != credential.Agent.ID || loaded.Source != namedAgentSessionSource+credential.Agent.ID {
		t.Fatalf("loaded persisted non-autostart thread = %#v", loaded)
	}
}

func TestChannelAgentResetRequestsTheCurrentThreadOwner(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	attachNamedAgentTestToolkit(t, rt)
	out := &lockedBuffer{}
	server := NewWithCredentialStore(rt, out, nil, nil)
	t.Cleanup(server.Close)
	credential, err := server.channelService.CreateNamedAgent(context.Background(), channels.CreateNamedAgentParams{
		Name: "Alpha", Autostart: true,
	})
	if err != nil {
		t.Fatalf("CreateNamedAgent() error = %v", err)
	}
	threadID := namedAgentSessionID(credential.Agent)
	owner, acquired, err := session.TryAcquireThreadExecutionLease(rt.SessionDir, threadID)
	if err != nil || !acquired || owner == nil {
		t.Fatalf("owner lease = %v, acquired %v, err %v", owner, acquired, err)
	}
	defer owner.Release()

	var reset ChannelAgentResetResult
	callChannelRPC(t, server, out, MethodChannelAgentReset, ChannelAgentResetParams{AgentID: credential.Agent.ID}, &reset)
	if !reset.Requested || reset.ThreadID != threadID || reset.Agent.ID != credential.Agent.ID {
		t.Fatalf("reset result = %#v", reset)
	}
	requested, err := owner.ResetRequested()
	if err != nil || !requested {
		t.Fatalf("owner reset requested = %v, err %v", requested, err)
	}
}

func TestChannelAgentListSeesAnOwnerInAnotherAppServer(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	attachNamedAgentTestToolkit(t, rt)
	out := &lockedBuffer{}
	server := NewWithCredentialStore(rt, out, nil, nil)
	t.Cleanup(server.Close)
	credential, err := server.channelService.CreateNamedAgent(context.Background(), channels.CreateNamedAgentParams{
		Name: "Alpha", Autostart: true,
	})
	if err != nil {
		t.Fatalf("CreateNamedAgent() error = %v", err)
	}
	owner, acquired, err := session.TryAcquireThreadExecutionLease(rt.SessionDir, namedAgentSessionID(credential.Agent))
	if err != nil || !acquired || owner == nil {
		t.Fatalf("owner lease = %v, acquired %v, err %v", owner, acquired, err)
	}
	defer owner.Release()

	var listed ChannelAgentListResult
	callChannelRPC(t, server, out, MethodChannelAgentList, nil, &listed)
	if len(listed.Agents) != 1 || listed.Agents[0].ActivityStatus != "thinking" {
		t.Fatalf("cross-process agent activity = %#v", listed.Agents)
	}
}

func TestChannelAgentResetClearsStaleWakeWithoutDroppingInbox(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	attachNamedAgentTestToolkit(t, rt)
	out := &lockedBuffer{}
	server := NewWithCredentialStore(rt, out, nil, nil)
	t.Cleanup(server.Close)
	credential, err := server.channelService.CreateNamedAgent(context.Background(), channels.CreateNamedAgentParams{
		Name: "Alpha", Autostart: true,
	})
	if err != nil {
		t.Fatalf("CreateNamedAgent() error = %v", err)
	}
	server.channelService.SetWakeSink(nil)
	room := createAppserverTestRoom(t, server.channelService, credential.Agent)
	if _, err := server.channelService.SendHuman(context.Background(), channels.HumanSendParams{
		RoomID: room.ID, HumanID: "human-1", Body: "@Alpha queued",
	}); err != nil {
		t.Fatalf("SendHuman() error = %v", err)
	}

	var reset ChannelAgentResetResult
	callChannelRPC(t, server, out, MethodChannelAgentReset, ChannelAgentResetParams{AgentID: credential.Agent.ID}, &reset)
	if reset.Requested || reset.WakeState.Outstanding || reset.WakeState.Pending {
		t.Fatalf("idle reset result = %#v", reset)
	}
	inbox, err := server.channelService.ListInbox(context.Background(), credential.Agent.ID, true)
	if err != nil || len(inbox) != 1 {
		t.Fatalf("preserved inbox = %#v, err %v", inbox, err)
	}
}

func TestChannelAgentResetInterruptsCurrentWakeAndDrainsFollowup(t *testing.T) {
	client := newBlockingStreamClient("followup done")
	rt := newTestRuntime(t, &fakeClient{})
	rt.StreamRunner.Client = client
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	attachNamedAgentTestToolkit(t, rt)
	out := &lockedBuffer{}
	server := NewWithCredentialStore(rt, out, nil, nil)
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		select {
		case <-client.release:
		default:
			close(client.release)
		}
	})
	credential, err := server.channelService.CreateNamedAgent(context.Background(), channels.CreateNamedAgentParams{
		Name: "Alpha", Autostart: true,
	})
	if err != nil {
		t.Fatalf("CreateNamedAgent() error = %v", err)
	}
	server.channelService.SetWakeSink(nil)
	room := createAppserverTestRoom(t, server.channelService, credential.Agent)
	if _, err := server.channelService.SendHuman(context.Background(), channels.HumanSendParams{
		RoomID: room.ID, HumanID: "human-1", Body: "@Alpha first",
	}); err != nil {
		t.Fatalf("first SendHuman() error = %v", err)
	}
	if err := server.deliverNamedAgentWake(context.Background(), credential.Agent.ID); err != nil {
		t.Fatalf("first deliverNamedAgentWake() error = %v", err)
	}
	select {
	case <-client.started:
	case <-time.After(2 * time.Second):
		t.Fatal("named agent turn did not start")
	}
	if _, err := server.channelService.SendHuman(context.Background(), channels.HumanSendParams{
		RoomID: room.ID, HumanID: "human-1", Body: "@Alpha second",
	}); err != nil {
		t.Fatalf("second SendHuman() error = %v", err)
	}
	if err := server.deliverNamedAgentWake(context.Background(), credential.Agent.ID); err != nil {
		t.Fatalf("second deliverNamedAgentWake() error = %v", err)
	}
	thread := server.thread(namedAgentSessionID(credential.Agent))
	thread.mu.Lock()
	firstTurnID := thread.currentTurn
	thread.mu.Unlock()

	var reset ChannelAgentResetResult
	callChannelRPC(t, server, out, MethodChannelAgentReset, ChannelAgentResetParams{AgentID: credential.Agent.ID}, &reset)
	if !reset.Requested {
		t.Fatalf("running reset result = %#v", reset)
	}
	transitionDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(transitionDeadline) {
		thread.mu.Lock()
		currentTurnID := thread.currentTurn
		thread.mu.Unlock()
		if currentTurnID != firstTurnID {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	thread.mu.Lock()
	currentTurnID := thread.currentTurn
	thread.mu.Unlock()
	if currentTurnID == firstTurnID {
		t.Fatal("reset did not interrupt the current named-agent turn")
	}
	close(client.release)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, stateErr := server.channelService.WakeState(context.Background(), credential.Agent.ID)
		if stateErr == nil && !state.Outstanding && !state.Pending {
			inbox, inboxErr := server.channelService.ListInbox(context.Background(), credential.Agent.ID, true)
			if inboxErr == nil && len(inbox) == 2 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, _ := server.channelService.WakeState(context.Background(), credential.Agent.ID)
	inbox, _ := server.channelService.ListInbox(context.Background(), credential.Agent.ID, true)
	t.Fatalf("reset followup did not drain: state %#v, inbox %#v", state, inbox)
}

func TestNamedAgentRunningWakeUsesPendingHeldTurn(t *testing.T) {
	client := newBlockingStreamClient("done")
	rt := newTestRuntime(t, &fakeClient{})
	rt.StreamRunner.Client = client
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	attachNamedAgentTestToolkit(t, rt)
	server := NewWithCredentialStore(rt, &lockedBuffer{}, nil, nil)
	t.Cleanup(server.Close)
	credential, err := server.channelService.CreateNamedAgent(context.Background(), channels.CreateNamedAgentParams{
		Name: "Alpha", Autostart: true,
	})
	if err != nil {
		t.Fatalf("CreateNamedAgent() error = %v", err)
	}
	server.channelService.SetWakeSink(nil)
	room := createAppserverTestRoom(t, server.channelService, credential.Agent)
	if _, err := server.channelService.SendHuman(context.Background(), channels.HumanSendParams{
		RoomID: room.ID, HumanID: "human-1", Body: "@Alpha first",
	}); err != nil {
		t.Fatalf("first SendHuman() error = %v", err)
	}
	if err := server.deliverNamedAgentWake(context.Background(), credential.Agent.ID); err != nil {
		t.Fatalf("first deliverNamedAgentWake() error = %v", err)
	}
	select {
	case <-client.started:
	case <-time.After(5 * time.Second):
		t.Fatal("named agent turn did not start")
	}
	var listed ChannelAgentListResult
	callChannelRPC(t, server, server.out.(*lockedBuffer), MethodChannelAgentList, nil, &listed)
	if len(listed.Agents) != 1 || listed.Agents[0].ActivityStatus != "thinking" ||
		len(listed.Agents[0].ActivityRoomIDs) != 1 || listed.Agents[0].ActivityRoomIDs[0] != room.ID {
		t.Fatalf("running named agent activity = %#v, want room %q", listed.Agents, room.ID)
	}
	if err := server.channelService.ClearWakeOnCheck(context.Background(), credential.Agent.ID); err != nil {
		t.Fatalf("ClearWakeOnCheck() error = %v", err)
	}
	if _, err := server.channelService.SendHuman(context.Background(), channels.HumanSendParams{
		RoomID: room.ID, HumanID: "human-1", Body: "@Alpha second",
	}); err != nil {
		t.Fatalf("second SendHuman() error = %v", err)
	}
	if err := server.deliverNamedAgentWake(context.Background(), credential.Agent.ID); err != nil {
		t.Fatalf("second deliverNamedAgentWake() error = %v", err)
	}
	state, err := server.channelService.WakeState(context.Background(), credential.Agent.ID)
	if err != nil || !state.Outstanding || !state.Pending {
		t.Fatalf("running wake state = %#v, err %v", state, err)
	}
	held, err := server.loadHeldUserTurns(namedAgentSessionID(credential.Agent))
	if err != nil || len(held) != 1 || held[0].id != namedAgentWakeID(credential.Agent.ID) {
		t.Fatalf("held named agent wake = %#v, err %v", held, err)
	}

	close(client.release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err = server.channelService.WakeState(context.Background(), credential.Agent.ID)
		held, _ = server.loadHeldUserTurns(namedAgentSessionID(credential.Agent))
		if err == nil && !state.Pending && len(held) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pending wake did not drain: state %#v, held %#v", state, held)
}

func TestNamedAgentSequentialWakesPersistDistinctUserTurns(t *testing.T) {
	client := newBlockingStreamClient("done")
	close(client.release)
	rt := newTestRuntime(t, &fakeClient{})
	rt.StreamRunner.Client = client
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	attachNamedAgentTestToolkit(t, rt)
	server := NewWithCredentialStore(rt, &lockedBuffer{}, nil, nil)
	t.Cleanup(server.Close)
	credential, err := server.channelService.CreateNamedAgent(context.Background(), channels.CreateNamedAgentParams{
		Name: "Alpha", Autostart: true,
	})
	if err != nil {
		t.Fatalf("CreateNamedAgent() error = %v", err)
	}
	server.channelService.SetWakeSink(nil)
	room := createAppserverTestRoom(t, server.channelService, credential.Agent)

	for index, body := range []string{"first", "second"} {
		if _, err := server.channelService.SendHuman(context.Background(), channels.HumanSendParams{
			RoomID: room.ID, HumanID: "human-1", Body: body,
		}); err != nil {
			t.Fatalf("SendHuman(%d) error = %v", index, err)
		}
		if err := server.deliverNamedAgentWake(context.Background(), credential.Agent.ID); err != nil {
			t.Fatalf("deliverNamedAgentWake(%d) error = %v", index, err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for threadIsRunning(server.thread(namedAgentSessionID(credential.Agent))) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if threadIsRunning(server.thread(namedAgentSessionID(credential.Agent))) {
			t.Fatalf("named agent wake %d did not complete", index)
		}
		completionDeadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(completionDeadline) {
			state, stateErr := server.channelService.WakeState(context.Background(), credential.Agent.ID)
			if stateErr == nil && !state.Outstanding && !state.Pending {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		state, stateErr := server.channelService.WakeState(context.Background(), credential.Agent.ID)
		if stateErr != nil || state.Outstanding || state.Pending {
			t.Fatalf("named agent wake %d was not released after unchecked completion: state %#v, err %v", index, state, stateErr)
		}
	}

	records, err := session.LoadHistoryRecords(rt.SessionDir, namedAgentSessionID(credential.Agent), false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords() error = %v", err)
	}
	var wakeIDs []string
	for _, record := range records {
		if record.Role == "user" && record.Phase == "channel_wake" {
			wakeIDs = append(wakeIDs, record.ClientID)
		}
	}
	if len(wakeIDs) != 2 || wakeIDs[0] == wakeIDs[1] {
		t.Fatalf("persisted named agent wake IDs = %#v, want two unique turns", wakeIDs)
	}
}

func TestChannelRoomUpdateAndDeleteRPCs(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	out := &lockedBuffer{}
	server := NewWithCredentialStore(rt, out, nil, nil)
	t.Cleanup(server.Close)

	var createdAgent ChannelAgentCreateResult
	callChannelRPC(t, server, out, MethodChannelAgentCreate, ChannelAgentCreateParams{Name: "Alpha"}, &createdAgent)
	var createdRoom ChannelRoomCreateResult
	callChannelRPC(t, server, out, MethodChannelRoomCreate, ChannelRoomCreateParams{
		Name: "Review", AgentIDs: []string{createdAgent.Agent.ID},
	}, &createdRoom)

	var updated ChannelRoomUpdateResult
	updatedName := "  Delivery  "
	callChannelRPC(t, server, out, MethodChannelRoomUpdate, ChannelRoomUpdateParams{
		RoomID: createdRoom.Room.ID,
		Name:   &updatedName,
	}, &updated)
	if updated.Room.ID != createdRoom.Room.ID || updated.Room.Name != "Delivery" || len(updated.Room.Members) != 2 {
		t.Fatalf("updated room = %#v", updated.Room)
	}
	var createdAgentTwo ChannelAgentCreateResult
	callChannelRPC(t, server, out, MethodChannelAgentCreate, ChannelAgentCreateParams{Name: "Beta"}, &createdAgentTwo)
	agentIDs := []string{createdAgent.Agent.ID, createdAgentTwo.Agent.ID}
	callChannelRPC(t, server, out, MethodChannelRoomUpdate, ChannelRoomUpdateParams{
		RoomID:   createdRoom.Room.ID,
		AgentIDs: &agentIDs,
	}, &updated)
	if len(updated.Room.Members) != 3 {
		t.Fatalf("updated room members = %#v, want local human plus two agents", updated.Room.Members)
	}
	loaded, err := server.channelService.GetRoom(context.Background(), createdRoom.Room.ID)
	if err != nil || loaded.Name != "Delivery" {
		t.Fatalf("persisted room = %#v, err = %v", loaded, err)
	}

	var deleted ChannelRoomDeleteResult
	callChannelRPC(t, server, out, MethodChannelRoomDelete, ChannelRoomDeleteParams{RoomID: createdRoom.Room.ID}, &deleted)
	if !deleted.Deleted {
		t.Fatal("delete RPC returned deleted=false")
	}
	if _, err := server.channelService.GetRoom(context.Background(), createdRoom.Room.ID); !errors.Is(err, channels.ErrNotFound) {
		t.Fatalf("GetRoom(deleted) error = %v, want ErrNotFound", err)
	}
	if kept, err := server.channelService.GetNamedAgent(context.Background(), createdAgent.Agent.ID); err != nil || kept.ID != createdAgent.Agent.ID {
		t.Fatalf("named agent after delete RPC = %#v, err = %v", kept, err)
	}
}

func createAppserverTestRoom(t *testing.T, service *channels.Service, agents ...channels.NamedAgent) channels.Room {
	t.Helper()
	members := make([]channels.RoomMember, 0, len(agents))
	for _, agent := range agents {
		members = append(members, channels.RoomMember{MemberType: channels.MemberAgent, MemberID: agent.ID})
	}
	room, err := service.CreateRoom(context.Background(), channels.CreateRoomParams{
		Kind: channels.RoomChannel, Name: "test-room", CreatedBy: "human-1", Members: members,
	})
	if err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	return room
}

func TestChannelRoomListReportsUnreadAndReadRPCClearsIt(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(t.TempDir(), ".wuu")
	out := &lockedBuffer{}
	server := NewWithCredentialStore(rt, out, nil, nil)
	t.Cleanup(server.Close)

	credential, err := server.channelService.CreateNamedAgent(context.Background(), channels.CreateNamedAgentParams{Name: "Alpha"})
	if err != nil {
		t.Fatalf("CreateNamedAgent() error = %v", err)
	}
	var createdRoom ChannelRoomCreateResult
	callChannelRPC(t, server, out, MethodChannelRoomCreate, ChannelRoomCreateParams{
		Name: "Unread", AgentIDs: []string{credential.Agent.ID},
	}, &createdRoom)
	if _, err := server.channelService.SendAgent(context.Background(), channels.AgentSendParams{
		RoomID: createdRoom.Room.ID, AgentID: credential.Agent.ID, Token: credential.Token, Body: "new message", BasisSeq: 0,
	}); err != nil {
		t.Fatalf("SendAgent() error = %v", err)
	}

	var listed ChannelRoomListResult
	callChannelRPC(t, server, out, MethodChannelRoomList, nil, &listed)
	if len(listed.Rooms) != 1 || listed.Rooms[0].UnreadCount != 1 {
		t.Fatalf("listed rooms = %#v, want one unread message", listed.Rooms)
	}
	var read ChannelRoomReadResult
	callChannelRPC(t, server, out, MethodChannelRoomRead, ChannelRoomReadParams{RoomID: createdRoom.Room.ID}, &read)
	if !read.Read {
		t.Fatal("room read result is false")
	}
	callChannelRPC(t, server, out, MethodChannelRoomList, nil, &listed)
	if len(listed.Rooms) != 1 || listed.Rooms[0].UnreadCount != 0 {
		t.Fatalf("listed rooms after read = %#v, want zero unread", listed.Rooms)
	}
}

func attachNamedAgentTestToolkit(t *testing.T, rt *runtime.Session) {
	t.Helper()
	kit, err := tools.New(rt.RootDir)
	if err != nil {
		t.Fatalf("tools.New() error = %v", err)
	}
	rt.Toolkit = kit
	rt.StreamRunner.Tools = kit
}

func callChannelRPC(t *testing.T, server *Server, out *lockedBuffer, method string, params any, target any) {
	t.Helper()
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal %s params: %v", method, err)
	}
	requestJSON, err := json.Marshal(Request{ID: json.RawMessage(`1`), Method: method, Params: paramsJSON})
	if err != nil {
		t.Fatalf("marshal %s request: %v", method, err)
	}
	if err := server.handleLine(context.Background(), requestJSON); err != nil {
		t.Fatalf("%s handler error = %v", method, err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *ResponseError  `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &envelope); err != nil {
		t.Fatalf("decode %s response: %v", method, err)
	}
	if envelope.Error != nil {
		t.Fatalf("%s response error = %#v", method, envelope.Error)
	}
	if err := json.Unmarshal(envelope.Result, target); err != nil {
		t.Fatalf("decode %s result: %v", method, err)
	}
}
