package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/hooks"
	"github.com/blueberrycongee/wuu/internal/modelroles"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

// participantPinRequest records one ChatRequest received by a stream-client
// stub and lets tests assert which client/model fired a given request.
type participantPinRequest struct {
	Client string
	Model  string
}

// recordingClient is a StreamClient stub that records every ChatRequest.
type recordingClient struct {
	id      string
	mu      sync.Mutex
	request providers.ChatRequest
	got     bool
}

func (c *recordingClient) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.request = req
	c.got = true
	return providers.ChatResponse{Content: "ok from " + c.id}, nil
}

func (c *recordingClient) StreamChat(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	resp, err := c.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan providers.StreamEvent, 2)
	ch <- providers.StreamEvent{Type: providers.EventContentDelta, Content: resp.Content}
	ch <- providers.StreamEvent{Type: providers.EventDone}
	close(ch)
	return ch, nil
}

func (c *recordingClient) LastRequest() (providers.ChatRequest, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.request, c.got
}

// buildParticipantPinRuntime wires up a Session that points at a generated
// config file with the requested providers configured. The returned Session
// uses the caller-supplied client as the worker default. Workers can target
// either the same provider or a different one through the participant pin.
func buildParticipantPinRuntime(t *testing.T, workerClient providers.StreamClient, workerProviderName string, extraProviders map[string]config.ProviderConfig) *runtime.Session {
	t.Helper()
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := config.Config{
		DefaultProvider: workerProviderName,
		Providers: map[string]config.ProviderConfig{
			workerProviderName: {
				Type:    "anthropic",
				BaseURL: "https://fake.example.test",
				Model:   "default-worker-model",
			},
		},
	}
	for name, p := range extraProviders {
		cfg.Providers[name] = p
	}
	cfgPath := filepath.Join(root, ".wuu.json")
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}

	resolvedName := workerProviderName
	providerCfg := cfg.Providers[workerProviderName]
	roleSelections, err := modelroles.Resolve(cfg, modelroles.ResolveOptions{
		ProviderName:   resolvedName,
		ProviderConfig: providerCfg,
		Model:          providerCfg.Model,
	})
	if err != nil {
		t.Fatalf("modelroles.Resolve: %v", err)
	}

	rt := &runtime.Session{
		ProviderName: workerProviderName,
		Model:        providerCfg.Model,
		RootDir:      root,
		ConfigPath:   cfgPath,
		SessionDir:   filepath.Join(root, ".wuu-state", "sessions"),
		StreamRunner: &agent.StreamRunner{Client: providers.AdaptStreamClient(&recordingClient{id: "main"}), Model: providerCfg.Model},
		HookDispatcher: hooks.NewDispatcher(nil),
		WorkerClient: workerClient,
		ModelRoles:   roleSelections,
	}
	return rt
}

// saveNamedParticipant pins a named participant (KindNamed) with the given
// Model field into the session dir used by the supplied runtime. Returns the
// generated participant ID.
func saveNamedParticipant(t *testing.T, rt *runtime.Session, name, role, model string) string {
	t.Helper()
	p := participant.Participant{
		ID:        participant.NewID(),
		Kind:      participant.KindNamed,
		Name:      name,
		Role:      role,
		Model:     model,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := session.UpsertParticipant(rt.SessionDir, p); err != nil {
		t.Fatalf("upsert participant: %v", err)
	}
	return p.ID
}

// buildParticipantPinCoord wires up an AgentControl that uses workerClient as
// its default worker stream client. Workers can override per spawn via
// SpawnRequest.ClientOverride / ModelOverride.
func buildParticipantPinCoord(t *testing.T, rt *runtime.Session, workerClient providers.StreamClient) *agentcontrol.AgentControl {
	t.Helper()
	threadDir := filepath.Join(rt.RootDir, ".wuu-state", "threads")
	harnessDir := filepath.Join(rt.RootDir, ".wuu-state", "harness")
	historyDir := filepath.Join(rt.RootDir, ".wuu-state", "history")
	c, err := agentcontrol.New(agentcontrol.Config{
		Client:       workerClient,
		DefaultModel: "default-worker-model",
		ParentRepo:   rt.RootDir,
		WorktreeRoot: filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:    "sess-pin",
		ThreadDir:    threadDir,
		HarnessDir:   harnessDir,
		HistoryDir:   historyDir,
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return noopToolExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("agentcontrol.New: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestParticipantStartHonorsModelPinOnConfiguredProvider(t *testing.T) {
	workerDefault := &recordingClient{id: "default"}

	// Stand up an httptest server that pretends to be Anthropic. The
	// per-participant pin resolves alt-provider through
	// providerfactory.BuildStreamClientWithRetry, which builds a real
	// anthropic client pointed at the configured BaseURL. Pointing it at
	// our test server lets the worker actually complete a request and
	// confirms the override client was selected.
	overrideReqCh := make(chan providers.ChatRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req providers.ChatRequest
		_ = json.Unmarshal(body, &req)
		select {
		case overrideReqCh <- req:
		default:
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
		fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0}\n\n")
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok from override\"}}\n\n")
		fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		fmt.Fprintf(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}\n\n")
		fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	t.Cleanup(server.Close)

	rt := buildParticipantPinRuntime(t, providers.AdaptStreamClient(workerDefault), "fake-provider", map[string]config.ProviderConfig{
		"alt-provider": {
			Type:    "anthropic",
			BaseURL: server.URL,
			Model:   "alt-default",
		},
	})
	coord := buildParticipantPinCoord(t, rt, providers.AdaptStreamClient(workerDefault))

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"thread","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "thread")["result"]).Thread.ID

	participantID := saveNamedParticipant(t, rt, "andy", "reviewer", "alt-provider:pinned-model")

	threadRuntime := &runtime.ThreadRuntime{
		StreamRunner: rt.StreamRunner,
		AgentControl: coord,
	}
	rootThread := newThreadState(threadID, nil, rt.ProviderName, rt.Model, rt.RootDir, false, time.Now().UTC())
	rootThread.execRuntime = threadRuntime
	srv.mu.Lock()
	srv.threads[threadID] = rootThread
	srv.mu.Unlock()
	srv.subscribeThreadRuntime(threadID, threadRuntime)

	raw := fmt.Sprintf(`{"id":"participant","method":"participant/start","params":{"thread_id":%q,"participant_id":%q,"prompt":"do review"}}`, threadID, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("participant/start: %v", err)
	}
	msgs := parseOutput(t, out.String())
	resp := responseByID(t, msgs, "participant")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("participant/start returned error: %v", errMsg)
	}
	agentID := remarshal[ParticipantStartResult](t, resp["result"]).Agent.ID
	if agentID == "" {
		t.Fatalf("expected agent id on participant/start; output:\n%s", out.String())
	}

	// The override provider (the test server) must receive the first
	// provider request, and it must carry the per-participant model
	// pin.
	var overrideReq providers.ChatRequest
	select {
	case overrideReq = <-overrideReqCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("alt-provider (override) never received a chat request; output:\n%s", out.String())
	}
	if overrideReq.Model != "pinned-model" {
		t.Fatalf("override client received model %q, want %q", overrideReq.Model, "pinned-model")
	}
	if req, ok := workerDefault.LastRequest(); ok {
		t.Fatalf("default worker client should not have received a request when override is set; got model=%q", req.Model)
	}
	waitForAgentStatus(t, coord, agentID, subagent.StatusCompleted)
}

func TestParticipantStartModelPinBareModelNameOverridesWorkerDefault(t *testing.T) {
	workerDefault := &recordingClient{id: "default"}

	rt := buildParticipantPinRuntime(t, providers.AdaptStreamClient(workerDefault), "fake-provider", nil)
	coord := buildParticipantPinCoord(t, rt, providers.AdaptStreamClient(workerDefault))

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"thread","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "thread")["result"]).Thread.ID

	participantID := saveNamedParticipant(t, rt, "andy", "reviewer", "bare-pinned-model")

	threadRuntime := &runtime.ThreadRuntime{
		StreamRunner: rt.StreamRunner,
		AgentControl: coord,
	}
	rootThread := newThreadState(threadID, nil, rt.ProviderName, rt.Model, rt.RootDir, false, time.Now().UTC())
	rootThread.execRuntime = threadRuntime
	srv.mu.Lock()
	srv.threads[threadID] = rootThread
	srv.mu.Unlock()
	srv.subscribeThreadRuntime(threadID, threadRuntime)

	raw := fmt.Sprintf(`{"id":"participant","method":"participant/start","params":{"thread_id":%q,"participant_id":%q,"prompt":"do review"}}`, threadID, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("participant/start: %v", err)
	}
	msgs := parseOutput(t, out.String())
	resp := responseByID(t, msgs, "participant")
	if errPayload, ok := resp["error"]; ok {
		t.Fatalf("participant/start returned error: %v", errPayload)
	}
	agentID := remarshal[ParticipantStartResult](t, resp["result"]).Agent.ID
	if agentID == "" {
		t.Fatalf("expected agent id on participant/start; output:\n%s", out.String())
	}

	waitForAgentStatus(t, coord, agentID, subagent.StatusCompleted)
	if req, ok := workerDefault.LastRequest(); !ok {
		t.Fatalf("worker client never received a chat request; output:\n%s", out.String())
	} else if req.Model != "bare-pinned-model" {
		t.Fatalf("worker client received model %q, want %q", req.Model, "bare-pinned-model")
	}
}

func TestParticipantStartModelPinRejectsUnconfiguredProvider(t *testing.T) {
	workerDefault := &recordingClient{id: "default"}

	rt := buildParticipantPinRuntime(t, providers.AdaptStreamClient(workerDefault), "fake-provider", nil)
	coord := buildParticipantPinCoord(t, rt, providers.AdaptStreamClient(workerDefault))

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"thread","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "thread")["result"]).Thread.ID

	participantID := saveNamedParticipant(t, rt, "andy", "reviewer", "missing-provider:some-model")

	threadRuntime := &runtime.ThreadRuntime{
		StreamRunner: rt.StreamRunner,
		AgentControl: coord,
	}
	rootThread := newThreadState(threadID, nil, rt.ProviderName, rt.Model, rt.RootDir, false, time.Now().UTC())
	rootThread.execRuntime = threadRuntime
	srv.mu.Lock()
	srv.threads[threadID] = rootThread
	srv.mu.Unlock()
	srv.subscribeThreadRuntime(threadID, threadRuntime)

	raw := fmt.Sprintf(`{"id":"participant","method":"participant/start","params":{"thread_id":%q,"participant_id":%q,"prompt":"do review"}}`, threadID, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("participant/start: %v", err)
	}

	msgs := parseOutput(t, out.String())
	resp := responseByID(t, msgs, "participant")
	errPayload, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error response, got %+v; messages: %s", resp, out.String())
	}
	errMsg, _ := errPayload["message"].(string)
	if !strings.Contains(errMsg, "missing-provider") {
		t.Fatalf("error message should mention missing-provider, got %q", errMsg)
	}
}

func TestParseParticipantModelPin(t *testing.T) {
	cases := []struct {
		raw       string
		provider  string
		modelName string
	}{
		{"anthropic:claude-sonnet", "anthropic", "claude-sonnet"},
		{" openai : gpt-4o ", "openai", "gpt-4o"},
		{"openrouter/openai/gpt-4o-mini", "", "openrouter/openai/gpt-4o-mini"},
		{"provider-with-colons:v1:model", "provider-with-colons", "v1:model"},
		{"", "", ""},
		{"  ", "", ""},
	}
	for _, c := range cases {
		provider, model := parseParticipantModelPin(c.raw)
		if provider != c.provider || model != c.modelName {
			t.Errorf("parseParticipantModelPin(%q) = (%q,%q), want (%q,%q)", c.raw, provider, model, c.provider, c.modelName)
		}
	}
}
