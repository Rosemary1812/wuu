package channels

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

type recordingWakeSink struct {
	mu        sync.Mutex
	delivered []string
}

func (s *recordingWakeSink) Deliver(agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delivered = append(s.delivered, agentID)
}

func (s *recordingWakeSink) take() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := append([]string(nil), s.delivered...)
	s.delivered = nil
	return result
}

type recordingTelemetrySink struct {
	mu     sync.Mutex
	events []TelemetryEvent
}

func (s *recordingTelemetrySink) RecordChannelEvent(event TelemetryEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *recordingTelemetrySink) snapshot() []TelemetryEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]TelemetryEvent(nil), s.events...)
}

func openTestService(t *testing.T, wake WakeSink) *Service {
	t.Helper()
	service, err := Open(t.TempDir(), wake)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return service
}

func createTestAgent(t *testing.T, service *Service, name string) AgentCredential {
	t.Helper()
	credential, err := service.CreateNamedAgent(context.Background(), CreateNamedAgentParams{
		Name:      name,
		Autostart: true,
	})
	if err != nil {
		t.Fatalf("CreateNamedAgent(%q) error = %v", name, err)
	}
	return credential
}

func createTestRoom(t *testing.T, service *Service, agents ...AgentCredential) Room {
	t.Helper()
	members := make([]RoomMember, 0, len(agents))
	for _, credential := range agents {
		members = append(members, RoomMember{MemberType: MemberAgent, MemberID: credential.Agent.ID})
	}
	room, err := service.CreateRoom(context.Background(), CreateRoomParams{
		Kind:      RoomChannel,
		Name:      "test-room",
		CreatedBy: "human-1",
		Members:   members,
	})
	if err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	return room
}

func TestOpenCreatesIndependentChannelsSchema(t *testing.T) {
	service := openTestService(t, nil)
	rows, err := service.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		tables = append(tables, name)
	}
	want := []string{
		"agent_wake_state",
		"drafts",
		"inbox_items",
		"named_agents",
		"reminders",
		"room_cursors",
		"room_members",
		"room_messages",
		"rooms",
	}
	for _, table := range want {
		if !containsString(tables, table) {
			t.Errorf("channels schema missing table %q; tables = %v", table, tables)
		}
	}
	if containsString(tables, "participants") || containsString(tables, "sessions") {
		t.Fatalf("channels database leaked legacy/session tables: %v", tables)
	}
	info, err := os.Stat(filepath.Join(service.Dir(), databaseFileName))
	if err != nil {
		t.Fatalf("stat channels database: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("channels database mode = %o, want 600", info.Mode().Perm())
	}
}

func TestNamedAgentIdentityIsIndependentAndTokenIsHashed(t *testing.T) {
	service := openTestService(t, nil)
	credential, err := service.CreateNamedAgent(context.Background(), CreateNamedAgentParams{
		Name:          "Alpha",
		ModelOverride: "provider:model",
		Autostart:     true,
	})
	if err != nil {
		t.Fatalf("CreateNamedAgent() error = %v", err)
	}
	if credential.Agent.ID == "" || credential.Token == "" {
		t.Fatalf("credential = %#v, want generated id and token", credential)
	}
	if credential.Agent.MemoryDir != filepath.Join(service.Dir(), "agents", credential.Agent.ID, "memory") {
		t.Errorf("memory dir = %q", credential.Agent.MemoryDir)
	}
	if info, err := os.Stat(credential.Agent.MemoryDir); err != nil || !info.IsDir() {
		t.Fatalf("memory directory not created: info=%v err=%v", info, err)
	}
	var storedHash string
	if err := service.db.QueryRow(`SELECT token_hash FROM named_agents WHERE id = ?`, credential.Agent.ID).Scan(&storedHash); err != nil {
		t.Fatalf("read token hash: %v", err)
	}
	if storedHash == credential.Token || storedHash != tokenHash(credential.Token) {
		t.Fatalf("stored token hash = %q, raw token = %q", storedHash, credential.Token)
	}
	tokenPath := filepath.Join(filepath.Dir(credential.Agent.MemoryDir), agentTokenFile)
	if raw, err := os.ReadFile(tokenPath); err != nil || strings.TrimSpace(string(raw)) != credential.Token {
		t.Fatalf("persisted private token = %q, err %v", raw, err)
	}
	if info, err := os.Stat(tokenPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private token mode = %v, err %v", info, err)
	}
	got, err := service.AuthenticateAgent(context.Background(), credential.Agent.ID, credential.Token)
	if err != nil {
		t.Fatalf("AuthenticateAgent() error = %v", err)
	}
	if got != credential.Agent {
		t.Errorf("AuthenticateAgent() = %#v, want %#v", got, credential.Agent)
	}
	if _, err := service.AuthenticateAgent(context.Background(), credential.Agent.ID, "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong token error = %v, want ErrUnauthorized", err)
	}
	client, err := service.BindAgent(context.Background(), credential.Agent.ID)
	if err != nil {
		t.Fatalf("BindAgent() error = %v", err)
	}
	client.token = "wrong"
	if _, err := client.Check(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("bound client did not authenticate each call: %v", err)
	}
	if _, err := service.CreateNamedAgent(context.Background(), CreateNamedAgentParams{Name: "alpha"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("case-insensitive duplicate name error = %v, want ErrConflict", err)
	}
	state, err := service.WakeState(context.Background(), credential.Agent.ID)
	if err != nil {
		t.Fatalf("WakeState() error = %v", err)
	}
	if state.Outstanding || state.Pending {
		t.Errorf("initial wake state = %#v, want idle flags", state)
	}
}

func TestRoomMembershipAndDMConstraints(t *testing.T) {
	service := openTestService(t, nil)
	agent := createTestAgent(t, service, "Alpha")
	room := createTestRoom(t, service, agent)
	if len(room.Members) != 2 {
		t.Fatalf("room members = %#v, want creator plus agent", room.Members)
	}
	loaded, err := service.GetRoom(context.Background(), room.ID)
	if err != nil {
		t.Fatalf("GetRoom() error = %v", err)
	}
	if len(loaded.Members) != 2 {
		t.Errorf("loaded members = %#v", loaded.Members)
	}
	if _, err := service.CreateRoom(context.Background(), CreateRoomParams{
		Kind:      RoomDM,
		Name:      "invalid-dm",
		CreatedBy: "human-1",
	}); err == nil {
		t.Fatal("CreateRoom(dm with one member) succeeded")
	}
	if _, err := service.CreateRoom(context.Background(), CreateRoomParams{
		Kind:      RoomChannel,
		Name:      "missing-agent",
		CreatedBy: "human-1",
		Members:   []RoomMember{{MemberType: MemberAgent, MemberID: "agent-missing"}},
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing agent member error = %v, want ErrNotFound", err)
	}
}

func TestMessageTriggersCoalesceWakeAndPersistInbox(t *testing.T) {
	ctx := context.Background()
	wake := &recordingWakeSink{}
	service := openTestService(t, wake)
	alpha := createTestAgent(t, service, "Alpha")
	alphaBeta := createTestAgent(t, service, "AlphaBeta")
	beta := createTestAgent(t, service, "Beta")
	room := createTestRoom(t, service, alpha, alphaBeta, beta)

	plain, err := service.SendHuman(ctx, HumanSendParams{RoomID: room.ID, HumanID: "human-1", Body: "hello everyone"})
	if err != nil {
		t.Fatalf("plain SendHuman() error = %v", err)
	}
	if plain.Message.Seq != 1 || len(plain.WakeAgentIDs) != 0 || len(wake.take()) != 0 {
		t.Fatalf("plain send = %#v", plain)
	}
	mentioned, err := service.SendHuman(ctx, HumanSendParams{RoomID: room.ID, HumanID: "human-1", Body: "@Alpha, please review"})
	if err != nil {
		t.Fatalf("mention SendHuman() error = %v", err)
	}
	if got, want := mentioned.Message.Mentions, []string{alpha.Agent.ID}; !equalStrings(got, want) {
		t.Fatalf("mentions = %v, want %v", got, want)
	}
	if got, want := mentioned.WakeAgentIDs, []string{alpha.Agent.ID}; !equalStrings(got, want) {
		t.Fatalf("wake targets = %v, want %v", got, want)
	}
	if got := wake.take(); !equalStrings(got, []string{alpha.Agent.ID}) {
		t.Fatalf("delivered wake = %v", got)
	}

	coalesced, err := service.SendHuman(ctx, HumanSendParams{RoomID: room.ID, HumanID: "human-1", Body: "@Alpha another detail"})
	if err != nil {
		t.Fatalf("coalesced SendHuman() error = %v", err)
	}
	if len(coalesced.WakeAgentIDs) != 0 || len(wake.take()) != 0 {
		t.Fatalf("duplicate outstanding wake was delivered: %#v", coalesced)
	}
	items, err := service.ListInbox(ctx, alpha.Agent.ID, true)
	if err != nil {
		t.Fatalf("ListInbox() error = %v", err)
	}
	if len(items) != 2 || items[0].Kind != InboxMention || items[1].Kind != InboxMention {
		t.Fatalf("alpha inbox = %#v", items)
	}

	self, err := service.SendAgent(ctx, AgentSendParams{
		RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token,
		Body: "@Alpha self note", BasisSeq: coalesced.Message.Seq,
	})
	if err != nil {
		t.Fatalf("self mention SendAgent() error = %v", err)
	}
	if got := wake.take(); len(got) != 0 {
		t.Fatalf("agent woke itself: %v", got)
	}

	if _, err := service.db.Exec(`UPDATE agent_wake_state SET outstanding = 0 WHERE agent_id = ?`, beta.Agent.ID); err != nil {
		t.Fatalf("clear beta wake: %v", err)
	}
	root, err := service.SendAgent(ctx, AgentSendParams{
		RoomID: room.ID, AgentID: beta.Agent.ID, Token: beta.Token,
		Body: "proposal", BasisSeq: self.Message.Seq,
	})
	if err != nil {
		t.Fatalf("root SendAgent() error = %v", err)
	}
	reply, err := service.SendHuman(ctx, HumanSendParams{
		RoomID: room.ID, HumanID: "human-1", ReplyTo: root.Message.ID, Body: "please expand",
	})
	if err != nil {
		t.Fatalf("reply SendHuman() error = %v", err)
	}
	if reply.Message.ThreadID != root.Message.ID {
		t.Fatalf("reply thread = %q, want root %q", reply.Message.ThreadID, root.Message.ID)
	}
	if got, want := reply.WakeAgentIDs, []string{beta.Agent.ID}; !equalStrings(got, want) {
		t.Fatalf("human reply wake = %v, want %v", got, want)
	}
	if got := wake.take(); !equalStrings(got, []string{beta.Agent.ID}) {
		t.Fatalf("human reply delivered wake = %v", got)
	}
	betaInbox, err := service.ListInbox(ctx, beta.Agent.ID, true)
	if err != nil {
		t.Fatalf("ListInbox(beta) error = %v", err)
	}
	kinds := make([]InboxKind, 0, len(betaInbox))
	for _, item := range betaInbox {
		if item.MessageID == reply.Message.ID {
			kinds = append(kinds, item.Kind)
		}
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	if len(kinds) != 2 || kinds[0] != InboxReply || kinds[1] != InboxThreadUpdate {
		t.Fatalf("reply inbox kinds = %v, want reply + thread_update", kinds)
	}
}

func TestM2SameBasisHoldsCollidingAnswer(t *testing.T) {
	ctx := context.Background()
	service := openTestService(t, nil)
	alpha := createTestAgent(t, service, "Alpha")
	beta := createTestAgent(t, service, "Beta")
	room := createTestRoom(t, service, alpha, beta)

	first, err := service.SendAgent(ctx, AgentSendParams{
		RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token, Body: "1", BasisSeq: 0,
	})
	if err != nil {
		t.Fatalf("first SendAgent() error = %v", err)
	}
	second, err := service.SendAgent(ctx, AgentSendParams{
		RoomID: room.ID, AgentID: beta.Agent.ID, Token: beta.Token, Body: "1", BasisSeq: 0,
	})
	if err != nil {
		t.Fatalf("second SendAgent() error = %v", err)
	}
	if first.Status != SendCommitted || first.Message.Seq != 1 {
		t.Fatalf("first send = %#v, want committed seq 1", first)
	}
	if second.Status != SendHeld || second.Draft == nil || second.Draft.HoldCount != 1 {
		t.Fatalf("second same-basis send = %#v, want held draft", second)
	}
	if second.Delta == nil || second.Delta.Count != 1 || len(second.Delta.Items) != 1 || second.Delta.Items[0].Preview != "1" {
		t.Fatalf("held delta = %#v, want first answer summary", second.Delta)
	}
	messages, err := service.ListMessages(ctx, room.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 1 || messages[0].Body != "1" {
		t.Fatalf("same-basis M2 messages = %#v", messages)
	}
}

func TestMentionPresentRequiresTokenBoundaries(t *testing.T) {
	tests := []struct {
		body string
		want bool
	}{
		{body: "@Alpha", want: true},
		{body: "please ask (@Alpha), thanks", want: true},
		{body: "Hello@Alpha", want: false},
		{body: "@Alphabet", want: false},
	}
	for _, test := range tests {
		if got := mentionPresent(test.body, "Alpha"); got != test.want {
			t.Errorf("mentionPresent(%q, Alpha) = %v, want %v", test.body, got, test.want)
		}
	}
}

func TestWakeStateBusyCheckAndCompletionTransitions(t *testing.T) {
	ctx := context.Background()
	service := openTestService(t, nil)
	agent := createTestAgent(t, service, "Alpha")
	room := createTestRoom(t, service, agent)

	if _, err := service.SendHuman(ctx, HumanSendParams{
		RoomID: room.ID, HumanID: "human-1", Body: "@Alpha review",
	}); err != nil {
		t.Fatalf("SendHuman() error = %v", err)
	}
	if err := service.MarkWakePending(ctx, agent.Agent.ID); err != nil {
		t.Fatalf("MarkWakePending() error = %v", err)
	}
	state, err := service.WakeState(ctx, agent.Agent.ID)
	if err != nil {
		t.Fatalf("WakeState() error = %v", err)
	}
	if !state.Outstanding || !state.Pending {
		t.Fatalf("busy wake state = %#v, want outstanding and pending", state)
	}

	taken, err := service.TakePendingWake(ctx, agent.Agent.ID)
	if err != nil {
		t.Fatalf("TakePendingWake() error = %v", err)
	}
	if !taken {
		t.Fatal("TakePendingWake() = false, want true")
	}
	state, err = service.WakeState(ctx, agent.Agent.ID)
	if err != nil {
		t.Fatalf("WakeState() after take error = %v", err)
	}
	if !state.Outstanding || state.Pending {
		t.Fatalf("taken wake state = %#v, want only outstanding", state)
	}

	if err := service.MarkWakePending(ctx, agent.Agent.ID); err != nil {
		t.Fatalf("second MarkWakePending() error = %v", err)
	}
	if err := service.ClearWakeOnCheck(ctx, agent.Agent.ID); err != nil {
		t.Fatalf("ClearWakeOnCheck() error = %v", err)
	}
	taken, err = service.TakePendingWake(ctx, agent.Agent.ID)
	if err != nil {
		t.Fatalf("TakePendingWake() after check error = %v", err)
	}
	if taken {
		t.Fatal("checked pending wake was reinjected")
	}
	state, err = service.WakeState(ctx, agent.Agent.ID)
	if err != nil {
		t.Fatalf("final WakeState() error = %v", err)
	}
	if state.Outstanding || state.Pending {
		t.Fatalf("checked wake state = %#v, want clear", state)
	}
}

func TestCheckAndReadAuthenticateAdvanceAndClearWake(t *testing.T) {
	ctx := context.Background()
	service := openTestService(t, nil)
	alpha := createTestAgent(t, service, "Alpha")
	outsider := createTestAgent(t, service, "Outsider")
	room := createTestRoom(t, service, alpha)
	body := "@Alpha " + strings.Repeat("细节", 50)
	sent, err := service.SendHuman(ctx, HumanSendParams{RoomID: room.ID, HumanID: "human-1", Body: body})
	if err != nil {
		t.Fatalf("SendHuman() error = %v", err)
	}
	if _, err := service.Check(ctx, alpha.Agent.ID, "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Check(wrong token) error = %v, want ErrUnauthorized", err)
	}
	checked, err := service.Check(ctx, alpha.Agent.ID, alpha.Token)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(checked.Items) != 1 || checked.Items[0].MessageID != sent.Message.ID || checked.HasMore {
		t.Fatalf("Check() = %#v", checked)
	}
	if got := utf8.RuneCountInString(checked.Items[0].Preview); got != checkPreviewRunes+1 {
		t.Fatalf("preview runes = %d, want %d including ellipsis", got, checkPreviewRunes+1)
	}
	if len(checked.Scopes) != 1 || checked.Scopes[0].RoomID != room.ID || checked.Scopes[0].Seq != sent.Message.Seq {
		t.Fatalf("check scopes = %#v", checked.Scopes)
	}
	state, err := service.WakeState(ctx, alpha.Agent.ID)
	if err != nil || state.Outstanding || state.Pending {
		t.Fatalf("checked wake state = %#v, err %v", state, err)
	}
	unpulled, err := service.ListInbox(ctx, alpha.Agent.ID, true)
	if err != nil || len(unpulled) != 0 {
		t.Fatalf("unpulled inbox = %#v, err %v", unpulled, err)
	}
	messages, err := service.ReadInboxMessages(ctx, alpha.Agent.ID, alpha.Token, []string{checked.Items[0].ID})
	if err != nil || len(messages) != 1 || messages[0].Body != body {
		t.Fatalf("ReadInboxMessages() = %#v, err %v", messages, err)
	}
	if _, err := service.ReadInboxMessages(ctx, outsider.Agent.ID, outsider.Token, []string{checked.Items[0].ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider ReadInboxMessages() error = %v, want ErrNotFound", err)
	}
	if _, err := service.ReadAgentMessages(ctx, outsider.Agent.ID, outsider.Token, room.ID, 0, 10); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("outsider ReadAgentMessages() error = %v, want ErrUnauthorized", err)
	}
}

func TestConcurrentMessageSequencesAreContiguous(t *testing.T) {
	ctx := context.Background()
	service := openTestService(t, nil)
	agent := createTestAgent(t, service, "Alpha")
	room := createTestRoom(t, service, agent)

	const count = 20
	var wg sync.WaitGroup
	errorsCh := make(chan error, count)
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.SendHuman(ctx, HumanSendParams{
				RoomID: room.ID, HumanID: "human-1", Body: "message",
			})
			errorsCh <- err
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent SendHuman() error = %v", err)
		}
	}
	messages, err := service.ListMessages(ctx, room.ID, 0, count)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != count {
		t.Fatalf("message count = %d, want %d", len(messages), count)
	}
	for index, message := range messages {
		if message.Seq != int64(index+1) {
			t.Fatalf("message[%d].Seq = %d, want %d", index, message.Seq, index+1)
		}
	}
}

func TestCheckPaginationDoesNotConsumeOverflowItem(t *testing.T) {
	ctx := context.Background()
	service := openTestService(t, nil)
	alpha := createTestAgent(t, service, "Alpha")
	room := createTestRoom(t, service, alpha)
	for index := 0; index < checkLimit+1; index++ {
		if _, err := service.SendHuman(ctx, HumanSendParams{
			RoomID: room.ID, HumanID: "human-1", Body: fmt.Sprintf("@Alpha item %d", index+1),
		}); err != nil {
			t.Fatalf("SendHuman(%d) error = %v", index+1, err)
		}
	}
	first, err := service.Check(ctx, alpha.Agent.ID, alpha.Token)
	if err != nil {
		t.Fatalf("first Check() error = %v", err)
	}
	if len(first.Items) != checkLimit || !first.HasMore {
		t.Fatalf("first Check() items = %d, has_more = %v", len(first.Items), first.HasMore)
	}
	second, err := service.Check(ctx, alpha.Agent.ID, alpha.Token)
	if err != nil {
		t.Fatalf("second Check() error = %v", err)
	}
	if len(second.Items) != 1 || second.HasMore || second.Items[0].Preview != "@Alpha item 51" {
		t.Fatalf("second Check() = %#v", second)
	}
}

func TestOpenMigratesLegacyAgentInboxAndPreservesItems(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	service, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open(current) error = %v", err)
	}
	alpha := createTestAgent(t, service, "Alpha")
	room := createTestRoom(t, service, alpha)
	if _, err := service.SendHuman(ctx, HumanSendParams{
		RoomID: room.ID, HumanID: "human-1", Body: "@Alpha legacy",
	}); err != nil {
		t.Fatalf("SendHuman() error = %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("Close(current) error = %v", err)
	}

	db, err := sql.Open("sqlite", sqliteDSN(filepath.Join(dir, databaseFileName)))
	if err != nil {
		t.Fatalf("open legacy fixture database: %v", err)
	}
	for _, statement := range []string{
		`PRAGMA foreign_keys = OFF`,
		`DROP INDEX idx_inbox_items_agent_pull`,
		`DROP INDEX idx_inbox_items_unique`,
		`ALTER TABLE inbox_items RENAME TO inbox_items_current`,
		`CREATE TABLE inbox_items (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			room_id TEXT NOT NULL,
			message_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			pulled_at INTEGER
		)`,
		`INSERT INTO inbox_items(id, agent_id, room_id, message_id, kind, created_at, pulled_at)
			SELECT id, member_id, room_id, message_id, kind, created_at, pulled_at FROM inbox_items_current`,
		`DROP TABLE inbox_items_current`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatalf("prepare legacy inbox with %q: %v", statement, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy fixture database: %v", err)
	}

	upgraded, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open(legacy) error = %v", err)
	}
	t.Cleanup(func() { _ = upgraded.Close() })
	check, err := upgraded.Check(ctx, alpha.Agent.ID, alpha.Token)
	if err != nil {
		t.Fatalf("Check(upgraded) error = %v", err)
	}
	if len(check.Items) != 1 || check.Items[0].Preview != "@Alpha legacy" {
		t.Fatalf("upgraded inbox = %#v", check)
	}
	reminder, err := upgraded.SetReminderAfter(ctx, ReminderSetParams{
		AgentID: alpha.Agent.ID, Token: alpha.Token, Note: "works after migration",
	}, MinReminderDur)
	if err != nil || reminder.RoomID != "" {
		t.Fatalf("SetReminderAfter(upgraded) = %#v, %v", reminder, err)
	}
}

func TestChannelTelemetrySinkRecordsCommittedAndHeldSends(t *testing.T) {
	ctx := context.Background()
	service := openTestService(t, nil)
	telemetry := &recordingTelemetrySink{}
	service.SetTelemetrySink(telemetry)
	alpha := createTestAgent(t, service, "Alpha")
	room := createTestRoom(t, service, alpha)
	if _, err := service.SendHuman(ctx, HumanSendParams{
		RoomID: room.ID, HumanID: "human-1", Body: "first",
	}); err != nil {
		t.Fatalf("SendHuman() error = %v", err)
	}
	result, err := service.SendAgent(ctx, AgentSendParams{
		RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token,
		Body: "stale", BasisSeq: 0,
	})
	if err != nil || result.Status != SendHeld {
		t.Fatalf("SendAgent(stale) = %#v, %v", result, err)
	}
	events := telemetry.snapshot()
	if len(events) != 2 || events[0].Name != "message_committed" || events[0].MemberType != MemberHuman {
		t.Fatalf("committed telemetry = %#v", events)
	}
	if events[1].Name != "draft_held" || events[1].MemberID != alpha.Agent.ID || events[1].HoldCount != 1 {
		t.Fatalf("held telemetry = %#v", events[1])
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
