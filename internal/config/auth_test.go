package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthStore_RoundTrip(t *testing.T) {
	home := t.TempDir()
	if err := SaveAuthKey(home, "openai", "sk-test-key-123"); err != nil {
		t.Fatalf("SaveAuthKey: %v", err)
	}
	key, err := LoadAuthKey(home, "openai")
	if err != nil {
		t.Fatalf("LoadAuthKey: %v", err)
	}
	if key != "sk-test-key-123" {
		t.Fatalf("expected sk-test-key-123, got %q", key)
	}
	path := filepath.Join(home, ".config", "wuu", "auth.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat auth.json: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}

func TestAuthStore_UnknownProvider(t *testing.T) {
	home := t.TempDir()
	_, err := LoadAuthKey(home, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestAuthStore_MultipleProviders(t *testing.T) {
	home := t.TempDir()
	SaveAuthKey(home, "openai", "sk-openai")
	SaveAuthKey(home, "anthropic", "sk-ant-xxx")
	k1, _ := LoadAuthKey(home, "openai")
	k2, _ := LoadAuthKey(home, "anthropic")
	if k1 != "sk-openai" {
		t.Fatalf("openai key mismatch: %q", k1)
	}
	if k2 != "sk-ant-xxx" {
		t.Fatalf("anthropic key mismatch: %q", k2)
	}
}

func TestAuthStore_CodexOAuthState(t *testing.T) {
	home := t.TempDir()
	state := CodexOAuthState{
		Tokens: CodexOAuthTokens{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			AccountID:    "account-id",
		},
		LastRefresh: "2026-05-23T00:00:00Z",
		AuthMode:    "chatgpt",
		Source:      "codex-cli-import",
		BaseURL:     "https://chatgpt.com/backend-api/codex",
	}

	if err := SaveCodexOAuth(home, state); err != nil {
		t.Fatalf("SaveCodexOAuth: %v", err)
	}
	if err := SaveAuthKey(home, "openai", "sk-openai"); err != nil {
		t.Fatalf("SaveAuthKey: %v", err)
	}

	got, err := LoadCodexOAuth(home)
	if err != nil {
		t.Fatalf("LoadCodexOAuth: %v", err)
	}
	if got.Tokens.AccessToken != state.Tokens.AccessToken || got.Tokens.RefreshToken != state.Tokens.RefreshToken {
		t.Fatalf("unexpected codex tokens: %#v", got.Tokens)
	}
	key, err := LoadAuthKey(home, "openai")
	if err != nil {
		t.Fatalf("LoadAuthKey after SaveCodexOAuth: %v", err)
	}
	if key != "sk-openai" {
		t.Fatalf("key = %q, want sk-openai", key)
	}
}

func TestAuthStore_RequiresHomeDir(t *testing.T) {
	for _, home := range []string{"", "   \t\n  "} {
		t.Run("home="+strings.TrimSpace(home), func(t *testing.T) {
			err := SaveAuthKey(home, "openai", "sk-test")
			if err == nil || err.Error() != "home directory is required" {
				t.Fatalf("expected clear home error from SaveAuthKey, got %v", err)
			}

			_, err = LoadAuthKey(home, "openai")
			if err == nil || err.Error() != "home directory is required" {
				t.Fatalf("expected clear home error from LoadAuthKey, got %v", err)
			}
		})
	}
}
