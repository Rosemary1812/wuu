package codex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/config"
)

const (
	tokenURL    = "https://auth.openai.com/oauth/token"
	clientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	refreshSkew = 120 * time.Second
)

// OAuthConfig configures the local ChatGPT/Codex OAuth credential source.
type OAuthConfig struct {
	BaseURL    string
	APIKey     string
	Home       string
	HTTPClient *http.Client
}

// OAuthSource resolves the bearer credentials used by the ChatGPT-backed
// Codex endpoint. It is intentionally separate from the wire client so wuu can
// keep its own agent loop while reusing a local Codex subscription login.
type OAuthSource struct {
	baseURL    string
	apiKey     string
	home       string
	httpClient *http.Client
}

type credentials struct {
	accessToken  string
	refreshToken string
	accountID    string
	source       string
	refreshable  bool
}

// NewOAuthSource creates a Codex OAuth credential source.
func NewOAuthSource(cfg OAuthConfig) *OAuthSource {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	home := strings.TrimSpace(cfg.Home)
	if home == "" {
		home = os.Getenv("HOME")
	}
	return &OAuthSource{
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(cfg.APIKey),
		home:       home,
		httpClient: cfg.HTTPClient,
	}
}

func (s *OAuthSource) Credentials(ctx context.Context, forceRefresh bool) (credentials, error) {
	if s.apiKey != "" {
		return credentials{
			accessToken: s.apiKey,
			accountID:   accountIDFromToken(s.apiKey),
			source:      "explicit",
		}, nil
	}
	if strings.TrimSpace(s.home) == "" {
		return credentials{}, errors.New("home directory is required for Codex OAuth")
	}

	state, err := config.LoadCodexOAuth(s.home)
	if err == nil {
		creds := credentials{
			accessToken:  strings.TrimSpace(state.Tokens.AccessToken),
			refreshToken: strings.TrimSpace(state.Tokens.RefreshToken),
			accountID:    firstNonEmpty(state.Tokens.AccountID, accountIDFromToken(state.Tokens.AccessToken)),
			source:       "wuu-auth-store",
			refreshable:  strings.TrimSpace(state.Tokens.RefreshToken) != "",
		}
		if forceRefresh || tokenExpiring(creds.accessToken, refreshSkew) {
			refreshed, refreshErr := refresh(ctx, s.httpClient, creds.refreshToken)
			if refreshErr != nil {
				return credentials{}, refreshErr
			}
			state.Tokens.AccessToken = refreshed.accessToken
			state.Tokens.RefreshToken = firstNonEmpty(refreshed.refreshToken, state.Tokens.RefreshToken)
			state.Tokens.AccountID = firstNonEmpty(accountIDFromToken(refreshed.accessToken), state.Tokens.AccountID)
			state.LastRefresh = time.Now().UTC().Format(time.RFC3339)
			state.AuthMode = "chatgpt"
			if state.Source == "" {
				state.Source = "wuu"
			}
			state.BaseURL = s.baseURL
			if saveErr := config.SaveCodexOAuth(s.home, state); saveErr != nil {
				return credentials{}, saveErr
			}
			creds.accessToken = state.Tokens.AccessToken
			creds.refreshToken = state.Tokens.RefreshToken
			creds.accountID = firstNonEmpty(state.Tokens.AccountID, accountIDFromToken(state.Tokens.AccessToken))
		}
		return creds, nil
	}

	cliState, cliErr := loadCodexCLIAuth(s.home)
	if cliErr != nil {
		return credentials{}, fmt.Errorf("no Codex OAuth credentials found; run `codex` to sign in or import credentials into wuu: %w", cliErr)
	}
	if tokenExpiring(cliState.Tokens.AccessToken, 0) {
		return credentials{}, errors.New("Codex CLI credentials are expired; run `codex` to refresh them before using openai-codex")
	}
	return credentials{
		accessToken:  strings.TrimSpace(cliState.Tokens.AccessToken),
		refreshToken: strings.TrimSpace(cliState.Tokens.RefreshToken),
		accountID:    firstNonEmpty(cliState.Tokens.AccountID, accountIDFromToken(cliState.Tokens.AccessToken)),
		source:       "codex-cli-readonly",
		refreshable:  false,
	}, nil
}

// LocalOAuthStatus reports whether wuu can use local ChatGPT/Codex OAuth
// credentials without performing a network refresh.
func LocalOAuthStatus(home string) (string, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		home = os.Getenv("HOME")
	}
	if home == "" {
		return "", errors.New("home directory is required for Codex OAuth")
	}
	if state, err := config.LoadCodexOAuth(home); err == nil {
		if strings.TrimSpace(state.Tokens.AccessToken) != "" &&
			(!tokenExpiring(state.Tokens.AccessToken, 0) || strings.TrimSpace(state.Tokens.RefreshToken) != "") {
			return "wuu-auth-store", nil
		}
	}
	cliState, err := loadCodexCLIAuth(home)
	if err != nil {
		return "", fmt.Errorf("Codex CLI OAuth credentials not found: %w", err)
	}
	if tokenExpiring(cliState.Tokens.AccessToken, 0) {
		return "", errors.New("Codex CLI OAuth credentials are expired; run `codex` to refresh them")
	}
	return "codex-cli", nil
}

type refreshResult struct {
	accessToken  string
	refreshToken string
}

func refresh(ctx context.Context, httpClient *http.Client, refreshToken string) (refreshResult, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return refreshResult{}, errors.New("Codex OAuth refresh token is missing")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshTokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return refreshResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return refreshResult{}, fmt.Errorf("refresh Codex OAuth token: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return refreshResult{}, fmt.Errorf("refresh Codex OAuth token: %s: %s", resp.Status, string(body))
	}

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return refreshResult{}, fmt.Errorf("parse Codex OAuth refresh response: %w", err)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return refreshResult{}, errors.New("Codex OAuth refresh response missing access_token")
	}
	return refreshResult{
		accessToken:  strings.TrimSpace(payload.AccessToken),
		refreshToken: strings.TrimSpace(payload.RefreshToken),
	}, nil
}

func refreshTokenURL() string {
	if v := strings.TrimSpace(os.Getenv("CODEX_REFRESH_TOKEN_URL_OVERRIDE")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("WUU_CODEX_REFRESH_TOKEN_URL")); v != "" {
		return v
	}
	return tokenURL
}

func loadCodexCLIAuth(home string) (config.CodexOAuthState, error) {
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	path := filepath.Join(codexHome, "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return config.CodexOAuthState{}, err
	}
	var raw struct {
		Tokens config.CodexOAuthTokens `json:"tokens"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return config.CodexOAuthState{}, err
	}
	if strings.TrimSpace(raw.Tokens.AccessToken) == "" {
		return config.CodexOAuthState{}, errors.New("Codex CLI auth is missing access_token")
	}
	return config.CodexOAuthState{
		Tokens:   raw.Tokens,
		AuthMode: "chatgpt",
		Source:   "codex-cli",
	}, nil
}

// codexHeaders returns the static headers the ChatGPT-backed Codex backend
// expects on every Responses API call. They mirror the OAuth-Responses client
// contract used by the Codex CLI / pi: a stable originator + User-Agent so
// the backend can attribute traffic, the lowercase chatgpt-account-id it
// uses for subscription routing, and the OpenAI-Beta tag that gates the
// experimental Responses surface.
func codexHeaders(accessToken, accountID string) map[string]string {
	headers := map[string]string{
		"User-Agent": "codex_cli_rs/0.0.0 (wuu)",
		"originator": "codex_cli_rs",
		// The ChatGPT backend rejects requests without this beta tag. The
		// Responses-over-WebSocket uses a different value; the WebSocket
		// path replaces this header during the upgrade handshake.
		"OpenAI-Beta": "responses=experimental",
	}
	if accountID == "" {
		accountID = accountIDFromToken(accessToken)
	}
	if accountID != "" {
		// Codex backend matches on the lowercase header. The legacy capitalized
		// ChatGPT-Account-ID was a wuu-only spelling; the canonical lowercase
		// form is what the Codex CLI / pi send and what the backend routes on.
		headers["chatgpt-account-id"] = accountID
	}
	return headers
}

func accountIDFromToken(token string) string {
	claims := jwtClaims(token)
	authClaims, ok := claims["https://api.openai.com/auth"].(map[string]any)
	if !ok {
		return ""
	}
	accountID, _ := authClaims["chatgpt_account_id"].(string)
	return strings.TrimSpace(accountID)
}

func tokenExpiring(token string, skew time.Duration) bool {
	claims := jwtClaims(token)
	exp, ok := claims["exp"].(float64)
	if !ok || exp <= 0 {
		return false
	}
	return time.Unix(int64(exp), 0).Before(time.Now().Add(skew))
}

func jwtClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil
		}
	}
	var claims map[string]any
	dec := json.NewDecoder(bytes.NewReader(payload))
	if err := dec.Decode(&claims); err != nil {
		return nil
	}
	return claims
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
