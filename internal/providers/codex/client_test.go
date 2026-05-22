package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

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
