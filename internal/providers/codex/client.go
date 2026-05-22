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
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/providers/openai"
)

const (
	DefaultBaseURL = "https://chatgpt.com/backend-api/codex"
	tokenURL       = "https://auth.openai.com/oauth/token"
	clientID       = "app_EMoamEEZ73f0CkXaXp7hrann"

	refreshSkew = 120 * time.Second
)

// ClientConfig configures the ChatGPT-backed Codex provider.
type ClientConfig struct {
	BaseURL      string
	APIKey       string
	Headers      map[string]string
	Home         string
	HTTPClient   *http.Client
	RetryConfig  *providers.RetryConfig
	StreamConfig *providers.StreamTransportConfig
}

// Client uses a local Codex OAuth session as an OpenAI Responses-compatible
// provider while leaving the agent loop in wuu.
type Client struct {
	baseURL      string
	apiKey       string
	headers      map[string]string
	home         string
	httpClient   *http.Client
	retryConfig  *providers.RetryConfig
	streamConfig *providers.StreamTransportConfig
}

type credentials struct {
	accessToken  string
	refreshToken string
	accountID    string
	source       string
	refreshable  bool
}

// New creates a Codex subscription-backed provider client.
func New(cfg ClientConfig) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	home := strings.TrimSpace(cfg.Home)
	if home == "" {
		home = os.Getenv("HOME")
	}
	return &Client{
		baseURL:      baseURL,
		apiKey:       strings.TrimSpace(cfg.APIKey),
		headers:      cloneHeaders(cfg.Headers),
		home:         home,
		httpClient:   cfg.HTTPClient,
		retryConfig:  cfg.RetryConfig,
		streamConfig: cfg.StreamConfig,
	}, nil
}

// Chat performs one non-streaming Responses API call.
func (c *Client) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	client, creds, err := c.openAIClient(ctx, false)
	if err != nil {
		return providers.ChatResponse{}, err
	}
	resp, err := client.Chat(ctx, req)
	if err == nil || !providers.IsAuthError(err) || !creds.refreshable {
		return resp, err
	}
	client, _, refreshErr := c.openAIClient(ctx, true)
	if refreshErr != nil {
		return providers.ChatResponse{}, fmt.Errorf("refresh Codex OAuth credentials after auth failure: %w", refreshErr)
	}
	return client.Chat(ctx, req)
}

// StreamChat opens a streaming Responses API call.
func (c *Client) StreamChat(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	client, creds, err := c.openAIClient(ctx, false)
	if err != nil {
		return nil, err
	}
	ch, err := client.StreamChat(ctx, req)
	if err == nil || !providers.IsAuthError(err) || !creds.refreshable {
		return ch, err
	}
	client, _, refreshErr := c.openAIClient(ctx, true)
	if refreshErr != nil {
		return nil, fmt.Errorf("refresh Codex OAuth credentials after auth failure: %w", refreshErr)
	}
	return client.StreamChat(ctx, req)
}

func (c *Client) openAIClient(ctx context.Context, forceRefresh bool) (*openai.Client, credentials, error) {
	creds, err := c.resolveCredentials(ctx, forceRefresh)
	if err != nil {
		return nil, credentials{}, err
	}
	headers := cloneHeaders(c.headers)
	if headers == nil {
		headers = make(map[string]string)
	}
	for k, v := range codexHeaders(creds.accessToken, creds.accountID) {
		if _, exists := headers[k]; !exists {
			headers[k] = v
		}
	}
	client, err := openai.New(openai.ClientConfig{
		BaseURL:      c.baseURL,
		WireAPI:      "responses",
		APIKey:       creds.accessToken,
		Headers:      headers,
		HTTPClient:   c.httpClient,
		RetryConfig:  c.retryConfig,
		StreamConfig: c.streamConfig,
	})
	if err != nil {
		return nil, credentials{}, err
	}
	return client, creds, nil
}

func (c *Client) resolveCredentials(ctx context.Context, forceRefresh bool) (credentials, error) {
	if c.apiKey != "" {
		return credentials{
			accessToken: c.apiKey,
			accountID:   accountIDFromToken(c.apiKey),
			source:      "explicit",
		}, nil
	}
	if strings.TrimSpace(c.home) == "" {
		return credentials{}, errors.New("home directory is required for Codex OAuth")
	}

	state, err := config.LoadCodexOAuth(c.home)
	if err == nil {
		creds := credentials{
			accessToken:  strings.TrimSpace(state.Tokens.AccessToken),
			refreshToken: strings.TrimSpace(state.Tokens.RefreshToken),
			accountID:    firstNonEmpty(state.Tokens.AccountID, accountIDFromToken(state.Tokens.AccessToken)),
			source:       "wuu-auth-store",
			refreshable:  strings.TrimSpace(state.Tokens.RefreshToken) != "",
		}
		if forceRefresh || tokenExpiring(creds.accessToken, refreshSkew) {
			refreshed, refreshErr := refresh(ctx, c.httpClient, creds.refreshToken)
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
			state.BaseURL = c.baseURL
			if saveErr := config.SaveCodexOAuth(c.home, state); saveErr != nil {
				return credentials{}, saveErr
			}
			creds.accessToken = state.Tokens.AccessToken
			creds.refreshToken = state.Tokens.RefreshToken
			creds.accountID = firstNonEmpty(state.Tokens.AccountID, accountIDFromToken(state.Tokens.AccessToken))
		}
		return creds, nil
	}

	cliState, cliErr := loadCodexCLIAuth(c.home)
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

func codexHeaders(accessToken, accountID string) map[string]string {
	headers := map[string]string{
		"User-Agent": "codex_cli_rs/0.0.0 (wuu)",
		"originator": "codex_cli_rs",
	}
	if accountID == "" {
		accountID = accountIDFromToken(accessToken)
	}
	if accountID != "" {
		headers["ChatGPT-Account-ID"] = accountID
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

func cloneHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
