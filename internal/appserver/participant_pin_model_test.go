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
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/hooks"
	"github.com/blueberrycongee/wuu/internal/modelroles"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
)

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

func buildParticipantPinRuntime(t *testing.T, currentClient providers.StreamClient, providerName string, extraProviders map[string]config.ProviderConfig) *runtime.Session {
	t.Helper()
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := config.Config{
		DefaultProvider: providerName,
		Providers: map[string]config.ProviderConfig{
			providerName: {
				Type:    "anthropic",
				BaseURL: "https://fake.example.test",
				Model:   "default-model",
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

	providerCfg := cfg.Providers[providerName]
	roleSelections, err := modelroles.Resolve(cfg, modelroles.ResolveOptions{
		ProviderName:   providerName,
		ProviderConfig: providerCfg,
		Model:          providerCfg.Model,
	})
	if err != nil {
		t.Fatalf("modelroles.Resolve: %v", err)
	}

	return &runtime.Session{
		ProviderName:   providerName,
		Model:          providerCfg.Model,
		RootDir:        root,
		ConfigPath:     cfgPath,
		SessionDir:     filepath.Join(root, ".wuu-state", "sessions"),
		StreamRunner:   &agent.StreamRunner{Client: currentClient, Model: providerCfg.Model, APIModel: providerCfg.Model},
		HookDispatcher: hooks.NewDispatcher(nil),
		WorkerClient:   currentClient,
		ModelRoles:     roleSelections,
	}
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

func TestResidentDMHonorsModelPinOnConfiguredProvider(t *testing.T) {
	currentClient := &recordingClient{id: "current"}
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

	rt := buildParticipantPinRuntime(t, providers.AdaptStreamClient(currentClient), "fake-provider", map[string]config.ProviderConfig{
		"alt-provider": {
			Type:    "anthropic",
			BaseURL: server.URL,
			APIKey:  "test-key",
			Model:   "alt-default",
		},
	})
	out := &lockedBuffer{}
	srv := New(rt, out)
	participantID := saveNamedParticipant(t, rt, "andy", "reviewer", "alt-provider:pinned-model")
	threadID := startDMThreadForPinTest(t, srv, out, participantID)

	raw := fmt.Sprintf(`{"id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"do review"}}`, threadID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	waitForTurnCompletedForThread(t, out, threadID)

	var overrideReq providers.ChatRequest
	select {
	case overrideReq = <-overrideReqCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("alt-provider override never received a chat request; output:\n%s", out.String())
	}
	if overrideReq.Model != "pinned-model" {
		t.Fatalf("override client received model %q, want %q", overrideReq.Model, "pinned-model")
	}
	if req, ok := currentClient.LastRequest(); ok {
		t.Fatalf("current provider client should not receive request when override is set; got model=%q", req.Model)
	}
}

func TestResidentDMBareModelPinOverridesCurrentProviderModel(t *testing.T) {
	currentClient := &recordingClient{id: "current"}
	rt := buildParticipantPinRuntime(t, providers.AdaptStreamClient(currentClient), "fake-provider", nil)
	out := &lockedBuffer{}
	srv := New(rt, out)
	participantID := saveNamedParticipant(t, rt, "andy", "reviewer", "bare-pinned-model")
	threadID := startDMThreadForPinTest(t, srv, out, participantID)

	raw := fmt.Sprintf(`{"id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"do review"}}`, threadID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	waitForTurnCompletedForThread(t, out, threadID)

	if req, ok := currentClient.LastRequest(); !ok {
		t.Fatalf("current provider client never received a chat request; output:\n%s", out.String())
	} else if req.Model != "bare-pinned-model" {
		t.Fatalf("current provider client received model %q, want %q", req.Model, "bare-pinned-model")
	}
}

func TestResidentDMModelPinRejectsUnconfiguredProvider(t *testing.T) {
	currentClient := &recordingClient{id: "current"}
	rt := buildParticipantPinRuntime(t, providers.AdaptStreamClient(currentClient), "fake-provider", nil)
	out := &lockedBuffer{}
	srv := New(rt, out)
	participantID := saveNamedParticipant(t, rt, "andy", "reviewer", "missing-provider:some-model")
	threadID := startDMThreadForPinTest(t, srv, out, participantID)

	raw := fmt.Sprintf(`{"id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"do review"}}`, threadID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	msgs := parseOutput(t, out.String())
	resp := responseByID(t, msgs, "turn")
	errPayload, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error response, got %+v; messages: %s", resp, out.String())
	}
	errMsg, _ := errPayload["message"].(string)
	if !strings.Contains(errMsg, "missing-provider") {
		t.Fatalf("error message should mention missing-provider, got %q", errMsg)
	}
}

func startDMThreadForPinTest(t *testing.T, srv *Server, out *lockedBuffer, participantID string) string {
	t.Helper()
	raw := fmt.Sprintf(`{"id":"thread","method":"thread/start","params":{"dm_participant_id":%q}}`, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "thread")
	if errPayload, ok := resp["error"]; ok {
		t.Fatalf("thread/start returned error: %v", errPayload)
	}
	return remarshal[ThreadStartResult](t, resp["result"]).Thread.ID
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
