package appserver

import (
	"context"
	"encoding/json"
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
	callChannelRPC(t, server, out, MethodChannelRoomCreate, ChannelRoomCreateParams{
		Name: "Review", AgentIDs: []string{createdAgent.Agent.ID},
	}, &createdRoom)
	if len(createdRoom.Room.Members) != 2 {
		t.Fatalf("created room members = %#v", createdRoom.Room.Members)
	}

	var sent ChannelMessageSendResult
	callChannelRPC(t, server, out, MethodChannelMessageSend, ChannelMessageSendParams{
		RoomID: createdRoom.Room.ID, Body: "@Alpha review this",
	}, &sent)
	if sent.Message.Seq != 1 || sent.Message.AuthorID != localChannelHumanID {
		t.Fatalf("sent message = %#v", sent.Message)
	}

	var listed ChannelMessageListResult
	callChannelRPC(t, server, out, MethodChannelMessageList, ChannelMessageListParams{RoomID: createdRoom.Room.ID}, &listed)
	if len(listed.Messages) != 1 || listed.Messages[0].Body != "@Alpha review this" {
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
	if started.Agent.ID != createdAgent.Agent.ID || server.thread(namedAgentSessionID(createdAgent.Agent)) == nil {
		t.Fatalf("started named agent = %#v", started.Agent)
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
		if index == 0 {
			// running becomes false just before the background turn releases its
			// execution lease and runs the named-agent completion callback.
			time.Sleep(100 * time.Millisecond)
		}
		if err := server.channelService.ClearWakeOnCheck(context.Background(), credential.Agent.ID); err != nil {
			t.Fatalf("ClearWakeOnCheck(%d) error = %v", index, err)
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
