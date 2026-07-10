package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/credentialstore"
)

type OAuthManager struct {
	store   credentialstore.Store
	client  *http.Client
	mu      sync.Mutex
	pending map[string]pendingOAuth
}

type OAuthStartOptions struct {
	ServerID     string
	ResourceURL  string
	RedirectURI  string
	Scopes       []string
	ClientID     string
	ClientSecret string
}

type OAuthStartResult struct {
	AuthorizationURL string   `json:"authorization_url"`
	State            string   `json:"state"`
	Scopes           []string `json:"scopes,omitempty"`
}

type OAuthFinishOptions struct {
	ServerID string
	State    string
	Code     string
}

type OAuthStatus struct {
	ServerID      string    `json:"server_id"`
	Authenticated bool      `json:"authenticated"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
	Scopes        []string  `json:"scopes,omitempty"`
}

type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

type authorizationServerMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint,omitempty"`
}

type pendingOAuth struct {
	State         string
	Verifier      string
	RedirectURI   string
	ResourceURL   string
	Scopes        []string
	ClientID      string
	ClientSecret  string
	TokenEndpoint string
}

type storedOAuthToken struct {
	AccessToken   string    `json:"access_token"`
	RefreshToken  string    `json:"refresh_token,omitempty"`
	TokenType     string    `json:"token_type,omitempty"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
	Scopes        []string  `json:"scopes,omitempty"`
	ClientID      string    `json:"client_id"`
	ClientSecret  string    `json:"client_secret,omitempty"`
	TokenEndpoint string    `json:"token_endpoint"`
	ResourceURL   string    `json:"resource_url,omitempty"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

func NewOAuthManager(store credentialstore.Store, client *http.Client) *OAuthManager {
	if client == nil {
		client = http.DefaultClient
	}
	return &OAuthManager{store: store, client: client, pending: map[string]pendingOAuth{}}
}

func (m *OAuthManager) Start(ctx context.Context, options OAuthStartOptions) (OAuthStartResult, error) {
	if m == nil || m.store == nil {
		return OAuthStartResult{}, errors.New("MCP OAuth credential store is not configured")
	}
	serverID := strings.TrimSpace(options.ServerID)
	resourceURL := strings.TrimSpace(options.ResourceURL)
	redirectURI := strings.TrimSpace(options.RedirectURI)
	if serverID == "" || resourceURL == "" || redirectURI == "" {
		return OAuthStartResult{}, errors.New("server_id, resource_url, and redirect_uri are required")
	}
	resourceMeta, err := m.discoverProtectedResource(ctx, resourceURL)
	if err != nil {
		return OAuthStartResult{}, err
	}
	if len(resourceMeta.AuthorizationServers) == 0 {
		return OAuthStartResult{}, errors.New("protected resource metadata has no authorization_servers")
	}
	authMeta, err := m.discoverAuthorizationServer(ctx, resourceMeta.AuthorizationServers[0])
	if err != nil {
		return OAuthStartResult{}, err
	}
	clientID := strings.TrimSpace(options.ClientID)
	clientSecret := options.ClientSecret
	if clientID == "" {
		clientID, clientSecret, err = m.registerClient(ctx, authMeta.RegistrationEndpoint, redirectURI)
		if err != nil {
			return OAuthStartResult{}, err
		}
	}
	state, err := randomURLToken(32)
	if err != nil {
		return OAuthStartResult{}, err
	}
	verifier, err := randomURLToken(48)
	if err != nil {
		return OAuthStartResult{}, err
	}
	challengeSum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeSum[:])
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"resource":              {resourceURL},
	}
	if len(options.Scopes) > 0 {
		query.Set("scope", strings.Join(options.Scopes, " "))
	}
	authorizationURL, err := url.Parse(authMeta.AuthorizationEndpoint)
	if err != nil {
		return OAuthStartResult{}, fmt.Errorf("parse authorization endpoint: %w", err)
	}
	authorizationURL.RawQuery = query.Encode()
	pending := pendingOAuth{State: state, Verifier: verifier, RedirectURI: redirectURI, ResourceURL: resourceURL, Scopes: append([]string(nil), options.Scopes...), ClientID: clientID, ClientSecret: clientSecret, TokenEndpoint: authMeta.TokenEndpoint}
	m.mu.Lock()
	m.pending[serverID+"\x00"+state] = pending
	m.mu.Unlock()
	return OAuthStartResult{AuthorizationURL: authorizationURL.String(), State: state, Scopes: append([]string(nil), options.Scopes...)}, nil
}

func (m *OAuthManager) Finish(ctx context.Context, options OAuthFinishOptions) (OAuthStatus, error) {
	key := strings.TrimSpace(options.ServerID) + "\x00" + strings.TrimSpace(options.State)
	m.mu.Lock()
	pending, ok := m.pending[key]
	if ok {
		delete(m.pending, key)
	}
	m.mu.Unlock()
	if !ok || pending.State != options.State {
		return OAuthStatus{}, errors.New("invalid or expired OAuth state")
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {options.Code},
		"client_id":     {pending.ClientID},
		"redirect_uri":  {pending.RedirectURI},
		"code_verifier": {pending.Verifier},
		"resource":      {pending.ResourceURL},
	}
	response, err := m.exchangeToken(ctx, pending.TokenEndpoint, pending.ClientSecret, form)
	if err != nil {
		return OAuthStatus{}, err
	}
	token := storedOAuthToken{AccessToken: response.AccessToken, RefreshToken: response.RefreshToken, TokenType: response.TokenType, ExpiresAt: tokenExpiry(response.ExpiresIn), Scopes: scopesFromResponse(response.Scope, pending.Scopes), ClientID: pending.ClientID, ClientSecret: pending.ClientSecret, TokenEndpoint: pending.TokenEndpoint, ResourceURL: pending.ResourceURL}
	if err := m.saveToken(ctx, options.ServerID, token); err != nil {
		return OAuthStatus{}, err
	}
	return oauthStatus(options.ServerID, token), nil
}

func (m *OAuthManager) Status(ctx context.Context, serverID string) (OAuthStatus, error) {
	token, err := m.loadToken(ctx, serverID)
	if errors.Is(err, credentialstore.ErrNotFound) {
		return OAuthStatus{ServerID: serverID}, nil
	}
	if err != nil {
		return OAuthStatus{}, err
	}
	return oauthStatus(serverID, token), nil
}

func (m *OAuthManager) AccessToken(ctx context.Context, serverID string) (string, error) {
	token, err := m.loadToken(ctx, serverID)
	if err != nil {
		return "", err
	}
	if token.ExpiresAt.IsZero() || time.Now().Before(token.ExpiresAt) {
		return token.AccessToken, nil
	}
	if strings.TrimSpace(token.RefreshToken) == "" {
		return "", errors.New("MCP OAuth token expired and has no refresh token")
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token.RefreshToken}, "client_id": {token.ClientID}}
	if token.ResourceURL != "" {
		form.Set("resource", token.ResourceURL)
	}
	response, err := m.exchangeToken(ctx, token.TokenEndpoint, token.ClientSecret, form)
	if err != nil {
		return "", err
	}
	token.AccessToken = response.AccessToken
	if response.RefreshToken != "" {
		token.RefreshToken = response.RefreshToken
	}
	token.TokenType = response.TokenType
	token.ExpiresAt = tokenExpiry(response.ExpiresIn)
	token.Scopes = scopesFromResponse(response.Scope, token.Scopes)
	if err := m.saveToken(ctx, serverID, token); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

func (m *OAuthManager) Remove(ctx context.Context, serverID string) error {
	return m.store.Delete(ctx, oauthNamespace(serverID), "oauth")
}

func (m *OAuthManager) discoverProtectedResource(ctx context.Context, resource string) (protectedResourceMetadata, error) {
	parsed, err := url.Parse(resource)
	if err != nil {
		return protectedResourceMetadata{}, err
	}
	endpoint := parsed.Scheme + "://" + parsed.Host + "/.well-known/oauth-protected-resource"
	var metadata protectedResourceMetadata
	if err := m.getJSON(ctx, endpoint, &metadata); err != nil {
		return metadata, fmt.Errorf("discover OAuth protected resource: %w", err)
	}
	return metadata, nil
}

func (m *OAuthManager) discoverAuthorizationServer(ctx context.Context, issuer string) (authorizationServerMetadata, error) {
	endpoint := strings.TrimRight(issuer, "/") + "/.well-known/oauth-authorization-server"
	var metadata authorizationServerMetadata
	if err := m.getJSON(ctx, endpoint, &metadata); err != nil {
		return metadata, fmt.Errorf("discover OAuth authorization server: %w", err)
	}
	if metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" {
		return metadata, errors.New("authorization server metadata is missing endpoints")
	}
	return metadata, nil
}

func (m *OAuthManager) registerClient(ctx context.Context, endpoint, redirectURI string) (string, string, error) {
	if strings.TrimSpace(endpoint) == "" {
		return "", "", errors.New("OAuth client_id is required because the server does not support dynamic registration")
	}
	body, _ := json.Marshal(map[string]any{"client_name": "Wuu", "redirect_uris": []string{redirectURI}, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("OAuth dynamic registration returned %s", resp.Status)
	}
	var registered struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&registered); err != nil {
		return "", "", err
	}
	if registered.ClientID == "" {
		return "", "", errors.New("OAuth dynamic registration returned no client_id")
	}
	return registered.ClientID, registered.ClientSecret, nil
}

func (m *OAuthManager) exchangeToken(ctx context.Context, endpoint, clientSecret string, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if clientSecret != "" {
		req.SetBasicAuth(form.Get("client_id"), clientSecret)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return tokenResponse{}, fmt.Errorf("OAuth token endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var token tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return token, err
	}
	if token.AccessToken == "" {
		return token, errors.New("OAuth token response has no access_token")
	}
	return token, nil
}

func (m *OAuthManager) getJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %s", endpoint, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (m *OAuthManager) saveToken(ctx context.Context, serverID string, token storedOAuthToken) error {
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return m.store.Set(ctx, oauthNamespace(serverID), "oauth", data)
}

func (m *OAuthManager) loadToken(ctx context.Context, serverID string) (storedOAuthToken, error) {
	data, err := m.store.Get(ctx, oauthNamespace(serverID), "oauth")
	if err != nil {
		return storedOAuthToken{}, err
	}
	var token storedOAuthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return token, err
	}
	return token, nil
}

func oauthNamespace(serverID string) string { return "mcp:" + strings.TrimSpace(serverID) }

func oauthStatus(serverID string, token storedOAuthToken) OAuthStatus {
	return OAuthStatus{ServerID: serverID, Authenticated: token.AccessToken != "", ExpiresAt: token.ExpiresAt, Scopes: append([]string(nil), token.Scopes...)}
}

func tokenExpiry(seconds int64) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(seconds) * time.Second)
}

func scopesFromResponse(scope string, fallback []string) []string {
	if strings.TrimSpace(scope) == "" {
		return append([]string(nil), fallback...)
	}
	return strings.Fields(scope)
}

func randomURLToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
