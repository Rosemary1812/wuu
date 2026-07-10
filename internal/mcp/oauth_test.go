package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/credentialstore"
)

func TestManagerOAuthLifecycleControlsConnectionAuthState(t *testing.T) {
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": baseURL, "authorization_servers": []string{baseURL}})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": baseURL, "authorization_endpoint": baseURL + "/authorize", "token_endpoint": baseURL + "/token",
			})
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "manager-secret", "token_type": "Bearer", "expires_in": 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL = server.URL

	oauth := NewOAuthManager(credentialstore.NewFileStore(filepath.Join(t.TempDir(), "oauth.json")), server.Client())
	manager := NewManager()
	manager.SetOAuthManager(oauth)
	manager.Configure(map[string]ServerConfig{
		"docs": {
			Name: "docs",
			URL:  baseURL,
			OAuth: &OAuthConfig{
				ClientID:    "desktop-client",
				RedirectURI: "http://127.0.0.1/callback",
				Scopes:      []string{"tools"},
			},
		},
	})

	if err := manager.Connect(context.Background(), "docs"); !errors.Is(err, ErrOAuthRequired) {
		t.Fatalf("Connect without credentials = %v, want ErrOAuthRequired", err)
	}
	status := manager.Status()["docs"]
	if status.State != MCPServerStateAuthRequired || status.AuthStatus != MCPAuthStatusNotLoggedIn {
		t.Fatalf("status before auth = %+v", status)
	}

	start, err := manager.StartOAuth(context.Background(), "docs")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}
	status, err = manager.FinishOAuth(context.Background(), "docs", start.State, "code-1")
	if err != nil {
		t.Fatalf("FinishOAuth: %v", err)
	}
	if status.AuthStatus != MCPAuthStatusOAuth || status.State != MCPServerStateStopped {
		t.Fatalf("status after auth = %+v", status)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "manager-secret") {
		t.Fatalf("status leaked token: %s", encoded)
	}

	if err := manager.RemoveOAuth(context.Background(), "docs"); err != nil {
		t.Fatalf("RemoveOAuth: %v", err)
	}
	status = manager.Status()["docs"]
	if status.AuthStatus != MCPAuthStatusNotLoggedIn || status.State != MCPServerStateAuthRequired {
		t.Fatalf("status after remove = %+v", status)
	}
}

func TestOAuthPKCEDynamicRegistrationFinishRefreshAndRemove(t *testing.T) {
	var baseURL string
	var tokenCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": baseURL, "authorization_servers": []string{baseURL}})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": baseURL, "authorization_endpoint": baseURL + "/authorize", "token_endpoint": baseURL + "/token", "registration_endpoint": baseURL + "/register",
			})
		case "/register":
			_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "dynamic-client"})
		case "/token":
			tokenCalls++
			_ = r.ParseForm()
			if r.Form.Get("grant_type") == "authorization_code" && r.Form.Get("code_verifier") == "" {
				t.Fatal("authorization code exchange omitted PKCE verifier")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-" + r.Form.Get("grant_type"), "refresh_token": "refresh-rotated", "token_type": "Bearer", "expires_in": 1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL = server.URL

	store := credentialstore.NewFileStore(filepath.Join(t.TempDir(), "oauth.json"))
	oauth := NewOAuthManager(store, server.Client())
	start, err := oauth.Start(context.Background(), OAuthStartOptions{ServerID: "docs", ResourceURL: baseURL, RedirectURI: "http://127.0.0.1/callback", Scopes: []string{"tools"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	authURL, err := url.Parse(start.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	query := authURL.Query()
	if query.Get("client_id") != "dynamic-client" || query.Get("state") == "" || query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization URL missing OAuth/PKCE fields: %s", start.AuthorizationURL)
	}

	status, err := oauth.Finish(context.Background(), OAuthFinishOptions{ServerID: "docs", State: start.State, Code: "code-1"})
	if err != nil || !status.Authenticated {
		t.Fatalf("Finish status = %+v, %v", status, err)
	}
	token, err := oauth.AccessToken(context.Background(), "docs")
	if err != nil || !strings.HasPrefix(token, "access-") {
		t.Fatalf("AccessToken = %q, %v", token, err)
	}
	time.Sleep(1100 * time.Millisecond)
	token, err = oauth.AccessToken(context.Background(), "docs")
	if err != nil || token != "access-refresh_token" || tokenCalls < 2 {
		t.Fatalf("refreshed token = %q calls=%d err=%v", token, tokenCalls, err)
	}
	if err := oauth.Remove(context.Background(), "docs"); err != nil {
		t.Fatal(err)
	}
	status, err = oauth.Status(context.Background(), "docs")
	if err != nil || status.Authenticated {
		t.Fatalf("status after remove = %+v, %v", status, err)
	}
}
