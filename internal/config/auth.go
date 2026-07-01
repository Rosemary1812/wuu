package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const authRelativePath = ".config/wuu/auth.json"

type authStore struct {
	Keys       map[string]string `json:"keys"`
	AuthTokens map[string]string `json:"auth_tokens,omitempty"`
	CodexOAuth *CodexOAuthState  `json:"codex_oauth,omitempty"`
}

// CodexOAuthTokens stores the token payload used by the ChatGPT-backed Codex
// endpoint. These are bearer credentials and must stay in the user auth store.
type CodexOAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
}

// CodexOAuthState stores wuu's own Codex OAuth session.
type CodexOAuthState struct {
	Tokens      CodexOAuthTokens `json:"tokens"`
	LastRefresh string           `json:"last_refresh,omitempty"`
	AuthMode    string           `json:"auth_mode,omitempty"`
	Source      string           `json:"source,omitempty"`
	BaseURL     string           `json:"base_url,omitempty"`
}

func SaveAuthKey(home, providerName, apiKey string) error {
	path, err := authPath(home)
	if err != nil {
		return err
	}
	store, _ := loadAuthStore(path)
	if store.Keys == nil {
		store.Keys = make(map[string]string)
	}
	store.Keys[providerName] = apiKey
	return writeAuthStore(path, store)
}

func LoadAuthKey(home, providerName string) (string, error) {
	path, err := authPath(home)
	if err != nil {
		return "", err
	}
	store, err := loadAuthStore(path)
	if err != nil {
		return "", err
	}
	key, ok := store.Keys[providerName]
	if !ok || key == "" {
		return "", fmt.Errorf("no auth key for provider %q", providerName)
	}
	return key, nil
}

func SaveAuthToken(home, providerName, token string) error {
	path, err := authPath(home)
	if err != nil {
		return err
	}
	store, _ := loadAuthStore(path)
	if store.Keys == nil {
		store.Keys = make(map[string]string)
	}
	if store.AuthTokens == nil {
		store.AuthTokens = make(map[string]string)
	}
	store.AuthTokens[providerName] = token
	return writeAuthStore(path, store)
}

func LoadAuthToken(home, providerName string) (string, error) {
	path, err := authPath(home)
	if err != nil {
		return "", err
	}
	store, err := loadAuthStore(path)
	if err != nil {
		return "", err
	}
	token, ok := store.AuthTokens[providerName]
	if !ok || token == "" {
		return "", fmt.Errorf("no auth token for provider %q", providerName)
	}
	return token, nil
}

// SaveCodexOAuth stores wuu's own Codex OAuth session.
func SaveCodexOAuth(home string, state CodexOAuthState) error {
	path, err := authPath(home)
	if err != nil {
		return err
	}
	store, _ := loadAuthStore(path)
	if store.Keys == nil {
		store.Keys = make(map[string]string)
	}
	store.CodexOAuth = &state
	return writeAuthStore(path, store)
}

// LoadCodexOAuth loads wuu's own Codex OAuth session.
func LoadCodexOAuth(home string) (CodexOAuthState, error) {
	path, err := authPath(home)
	if err != nil {
		return CodexOAuthState{}, err
	}
	store, err := loadAuthStore(path)
	if err != nil {
		return CodexOAuthState{}, err
	}
	if store.CodexOAuth == nil || strings.TrimSpace(store.CodexOAuth.Tokens.AccessToken) == "" {
		return CodexOAuthState{}, fmt.Errorf("no Codex OAuth credentials")
	}
	return *store.CodexOAuth, nil
}

func authPath(home string) (string, error) {
	resolvedHome, err := requireHomeDir(home)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedHome, authRelativePath), nil
}

func requireHomeDir(home string) (string, error) {
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("home directory is required")
	}
	return home, nil
}

func loadAuthStore(path string) (authStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return authStore{Keys: make(map[string]string)}, err
	}
	var store authStore
	if err := json.Unmarshal(data, &store); err != nil {
		return authStore{Keys: make(map[string]string)}, err
	}
	if store.Keys == nil {
		store.Keys = make(map[string]string)
	}
	if store.AuthTokens == nil {
		store.AuthTokens = make(map[string]string)
	}
	return store, nil
}

func writeAuthStore(path string, store authStore) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create auth dir: %w", err)
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
