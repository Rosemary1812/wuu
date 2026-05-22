package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestClientUsesCodexCLIAuthReadOnly(t *testing.T) {
	home := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	token := fakeJWT(t, time.Now().Add(time.Hour), "acct_123")
	writeCodexCLIAuth(t, codexHome, token, "refresh-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("path = %q, want /responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("originator"); got != "codex_cli_rs" {
			t.Fatalf("originator = %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-ID"); got != "acct_123" {
			t.Fatalf("ChatGPT-Account-ID = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if _, exists := body["temperature"]; exists {
			t.Fatalf("Codex request must omit unsupported temperature, got %#v", body["temperature"])
		}
		if body["instructions"] == "" {
			t.Fatalf("Codex request must include instructions: %#v", body)
		}
		if body["store"] != false {
			t.Fatalf("Codex request must set store=false, got %#v", body["store"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, Home: home, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := client.Chat(context.Background(), providers.ChatRequest{
		Model:       "gpt-5-codex",
		Temperature: 0.2,
		Messages: []providers.ChatMessage{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q", resp.Content)
	}
	if _, err := config.LoadCodexOAuth(home); err == nil {
		t.Fatal("Codex CLI read-only fallback should not write wuu auth state")
	}
}

func TestClientRefreshesStoredWuuCodexOAuth(t *testing.T) {
	home := t.TempDir()
	stale := fakeJWT(t, time.Now().Add(-time.Hour), "acct_old")
	fresh := fakeJWT(t, time.Now().Add(time.Hour), "acct_new")
	if err := config.SaveCodexOAuth(home, config.CodexOAuthState{
		Tokens: config.CodexOAuthTokens{
			AccessToken:  stale,
			RefreshToken: "refresh-old",
		},
		AuthMode: "chatgpt",
		Source:   "test",
	}); err != nil {
		t.Fatalf("SaveCodexOAuth: %v", err)
	}

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Fatalf("grant_type = %q", got)
		}
		if got := r.Form.Get("refresh_token"); got != "refresh-old" {
			t.Fatalf("refresh_token = %q", got)
		}
		if got := r.Form.Get("client_id"); got != clientID {
			t.Fatalf("client_id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":` + jsonString(fresh) + `,"refresh_token":"refresh-new"}`))
	}))
	defer tokenServer.Close()
	t.Setenv("CODEX_REFRESH_TOKEN_URL_OVERRIDE", tokenServer.URL)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+fresh {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-ID"); got != "acct_new" {
			t.Fatalf("ChatGPT-Account-ID = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"refreshed"}]}]}`))
	}))
	defer apiServer.Close()

	client, err := New(ClientConfig{BaseURL: apiServer.URL, Home: home, HTTPClient: apiServer.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := client.Chat(context.Background(), providers.ChatRequest{
		Model:    "gpt-5-codex",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "refreshed" {
		t.Fatalf("content = %q", resp.Content)
	}
	state, err := config.LoadCodexOAuth(home)
	if err != nil {
		t.Fatalf("LoadCodexOAuth: %v", err)
	}
	if state.Tokens.AccessToken != fresh || state.Tokens.RefreshToken != "refresh-new" {
		t.Fatalf("unexpected stored tokens: %#v", state.Tokens)
	}
}

func TestClientModelsUsesCodexOAuth(t *testing.T) {
	home := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	token := fakeJWT(t, time.Now().Add(time.Hour), "acct_models")
	writeCodexCLIAuth(t, codexHome, token, "refresh-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("path = %q, want /models", r.URL.Path)
		}
		if got := r.URL.Query().Get("client_version"); got != "1.0.0" {
			t.Fatalf("client_version = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("originator"); got != "codex_cli_rs" {
			t.Fatalf("originator = %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-ID"); got != "acct_models" {
			t.Fatalf("ChatGPT-Account-ID = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "models": [
		    {"slug":"hidden-model","visibility":"hide","priority":1},
		    {"slug":"gpt-5.4","display_name":"GPT-5.4","priority":20,"supported_in_api":true},
		    {"slug":"gpt-5.5","display_name":"GPT-5.5","priority":9,"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"}],"supported_in_api":true}
		  ]
		}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, Home: home, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	models, err := client.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models len = %d, want 2: %#v", len(models), models)
	}
	if models[0].Slug != "gpt-5.5" || models[1].Slug != "gpt-5.4" {
		t.Fatalf("unexpected model order: %#v", models)
	}
	if got := models[0].SupportedReasoning; len(got) != 2 || got[0] != "low" || got[1] != "medium" {
		t.Fatalf("unexpected reasoning levels: %#v", got)
	}
}

func TestCompactWithCodexClientUsesNormalResponsesEndpoint(t *testing.T) {
	home := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	token := fakeJWT(t, time.Now().Add(time.Hour), "acct_compact")
	writeCodexCLIAuth(t, codexHome, token, "refresh-token")

	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/responses/compact" {
			t.Fatalf("wuu compact must not use Codex remote compact endpoint")
		}
		if r.URL.Path != "/responses" {
			t.Fatalf("path = %q, want /responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("Authorization = %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if _, exists := body["tools"]; exists {
			t.Fatalf("compact summary request must not include tools: %#v", body["tools"])
		}
		input, ok := body["input"].([]any)
		if !ok || len(input) != 1 {
			t.Fatalf("expected one normal Responses input item, got %#v", body["input"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"<analysis>draft</analysis><summary>summary via normal responses</summary>"}]}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, Home: home, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	messages := []providers.ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "second reply"},
		{Role: "user", Content: "third"},
		{Role: "assistant", Content: "third reply"},
		{Role: "user", Content: "fourth"},
		{Role: "assistant", Content: "fourth reply"},
	}

	result, err := compact.Compact(context.Background(), messages, client, "gpt-5-codex")
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(paths) != 1 || paths[0] != "/responses" {
		t.Fatalf("unexpected request paths: %#v", paths)
	}
	if len(result) == 0 || result[0].Role != "system" || !compact.IsConversationSummaryContent(result[0].Content) {
		t.Fatalf("expected compacted summary at history root, got %#v", result)
	}
	if !strings.Contains(result[0].Content, "summary via normal responses") {
		t.Fatalf("expected cleaned summary from normal Responses call, got %q", result[0].Content)
	}
}

func writeCodexCLIAuth(t *testing.T, codexHome, accessToken, refreshToken string) {
	t.Helper()
	payload := map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]string{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), data, 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
}

func fakeJWT(t *testing.T, exp time.Time, accountID string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	claims := map[string]any{
		"exp": exp.Unix(),
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": accountID,
		},
	}
	data, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("Marshal claims: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(data)
	return header + "." + payload + ".sig"
}

func jsonString(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
