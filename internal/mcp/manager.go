package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Manager holds all active MCP client connections and exposes their tools.
type Manager struct {
	mu       sync.RWMutex
	configs  map[string]ServerConfig
	clients  map[string]*Client
	statuses map[string]ServerStatus
}

// NewManager creates an empty MCP manager.
func NewManager() *Manager {
	return &Manager{
		configs:  make(map[string]ServerConfig),
		clients:  make(map[string]*Client),
		statuses: make(map[string]ServerStatus),
	}
}

func (m *Manager) Configure(configs map[string]ServerConfig) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.configs == nil {
		m.configs = make(map[string]ServerConfig)
	}
	if m.statuses == nil {
		m.statuses = make(map[string]ServerStatus)
	}
	for name, cfg := range configs {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		cfg.Name = name
		m.configs[name] = cloneServerConfig(cfg)
		if _, connected := m.clients[name]; connected {
			continue
		}
		state := MCPServerStateConfigured
		if !cfg.IsEnabled() {
			state = MCPServerStateDisabled
		}
		if current, ok := m.statuses[name]; !ok || current.State == "" {
			m.statuses[name] = ServerStatus{Name: name, State: state, AuthStatus: authStatusForConfig(cfg)}
		}
	}
}

// Add connects and registers an MCP server. If the connection fails, the
// error is returned and no client is kept.
func (m *Manager) Add(ctx context.Context, cfg ServerConfig) error {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		return fmt.Errorf("mcp server name is required")
	}
	m.rememberConfig(cfg)
	if !cfg.IsEnabled() {
		m.recordStatus(ServerStatus{Name: cfg.Name, State: MCPServerStateDisabled, AuthStatus: authStatusForConfig(cfg)})
		return nil
	}
	m.recordStatus(ServerStatus{Name: cfg.Name, State: MCPServerStateConnecting, AuthStatus: authStatusForConfig(cfg)})
	var client *Client
	var err error
	if cfg.URL != "" {
		client, err = ConnectRemote(cfg)
	} else {
		client, err = ConnectStdio(cfg)
	}
	if err != nil {
		m.recordStatus(ServerStatus{Name: cfg.Name, State: classifyConnectError(err), AuthStatus: authStatusAfterConnectError(cfg, err), Connected: false, Error: err.Error()})
		return err
	}
	// Eagerly discover tools so the toolkit can include them.
	if _, derr := client.DiscoverTools(ctx); derr != nil {
		_ = client.Close()
		err := fmt.Errorf("discover tools for %q: %w", cfg.Name, derr)
		m.recordStatus(ServerStatus{Name: cfg.Name, State: MCPServerStateFailed, AuthStatus: authStatusForConfig(cfg), Connected: false, Error: err.Error()})
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.clients[cfg.Name]; ok {
		_ = old.Close()
	}
	m.clients[cfg.Name] = client
	m.statuses[cfg.Name] = ServerStatus{
		Name:       cfg.Name,
		State:      MCPServerStateConnected,
		AuthStatus: authStatusForConfig(cfg),
		Connected:  true,
		ToolCount:  len(client.Tools()),
	}
	return nil
}

func (m *Manager) Connect(ctx context.Context, name string) error {
	cfg, ok := m.Config(name)
	if !ok {
		return fmt.Errorf("mcp server %q not configured", name)
	}
	return m.Add(ctx, cfg)
}

func (m *Manager) Refresh(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		for _, cfg := range m.Configs() {
			if err := m.Add(ctx, cfg); err != nil {
				return err
			}
		}
		return nil
	}
	cfg, ok := m.Config(name)
	if !ok {
		return fmt.Errorf("mcp server %q not configured", name)
	}
	m.closeClient(name)
	return m.Add(ctx, cfg)
}

// Remove disconnects an MCP server by name.
func (m *Manager) Remove(name string) error {
	return m.Disconnect(name)
}

func (m *Manager) Disconnect(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.clients[name]
	if !ok {
		if _, configured := m.configs[name]; configured {
			m.statuses[name] = ServerStatus{Name: name, State: MCPServerStateDisabled, AuthStatus: authStatusForConfig(m.configs[name])}
			return nil
		}
		return fmt.Errorf("mcp server %q not found", name)
	}
	delete(m.clients, name)
	m.statuses[name] = ServerStatus{Name: name, State: MCPServerStateDisabled, AuthStatus: authStatusForConfig(m.configs[name])}
	return c.Close()
}

// AllTools returns every tool from every connected MCP server, wrapped as
// wuu-compatible *MCPTool instances. The caller owns the slice.
func (m *Manager) AllTools() []*MCPTool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*MCPTool
	for _, c := range m.clients {
		for _, t := range c.Tools() {
			out = append(out, NewMCPTool(c, t))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}

func (m *Manager) Config(name string) (ServerConfig, bool) {
	if m == nil {
		return ServerConfig{}, false
	}
	name = strings.TrimSpace(name)
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg, ok := m.configs[name]
	return cloneServerConfig(cfg), ok
}

func (m *Manager) Configs() []ServerConfig {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ServerConfig, 0, len(m.configs))
	for _, cfg := range m.configs {
		out = append(out, cloneServerConfig(cfg))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// Status returns a human-readable status summary for each configured server.
func (m *Manager) Status() map[string]ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]ServerStatus, len(m.statuses))
	for name, status := range m.statuses {
		if c, ok := m.clients[name]; ok {
			status.State = MCPServerStateConnected
			if status.AuthStatus == "" {
				status.AuthStatus = authStatusForConfig(m.configs[name])
			}
			status.Connected = true
			status.ToolCount = len(c.Tools())
			status.Error = ""
		}
		out[name] = status
	}
	return out
}

// Close shuts down all connections.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var firstErr error
	for _, c := range m.clients {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	m.clients = make(map[string]*Client)
	for name := range m.statuses {
		m.statuses[name] = ServerStatus{Name: name, State: MCPServerStateDisabled, AuthStatus: authStatusForConfig(m.configs[name])}
	}
	return firstErr
}

func (m *Manager) recordStatus(status ServerStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(status.Name) == "" {
		return
	}
	if m.statuses == nil {
		m.statuses = make(map[string]ServerStatus)
	}
	m.statuses[status.Name] = status
}

func (m *Manager) rememberConfig(cfg ServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.configs == nil {
		m.configs = make(map[string]ServerConfig)
	}
	m.configs[cfg.Name] = cloneServerConfig(cfg)
}

func (m *Manager) closeClient(name string) {
	m.mu.Lock()
	c := m.clients[name]
	if c != nil {
		delete(m.clients, name)
	}
	m.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

func cloneServerConfig(cfg ServerConfig) ServerConfig {
	out := cfg
	if cfg.Args != nil {
		out.Args = append([]string(nil), cfg.Args...)
	}
	if cfg.Env != nil {
		out.Env = make(map[string]string, len(cfg.Env))
		for k, v := range cfg.Env {
			out.Env[k] = v
		}
	}
	if cfg.Headers != nil {
		out.Headers = make(map[string]string, len(cfg.Headers))
		for k, v := range cfg.Headers {
			out.Headers[k] = v
		}
	}
	if cfg.OAuth != nil {
		out.OAuth = &OAuthConfig{
			ClientID:     cfg.OAuth.ClientID,
			ClientSecret: cfg.OAuth.ClientSecret,
			Scopes:       append([]string(nil), cfg.OAuth.Scopes...),
			RedirectURI:  cfg.OAuth.RedirectURI,
		}
	}
	if cfg.ToolOverrides != nil {
		out.ToolOverrides = cloneToolOverrides(cfg.ToolOverrides)
	}
	return out
}

type MCPServerState string

const (
	MCPServerStateConfigured              MCPServerState = "configured"
	MCPServerStateConnecting              MCPServerState = "connecting"
	MCPServerStateConnected               MCPServerState = "connected"
	MCPServerStateFailed                  MCPServerState = "failed"
	MCPServerStateDisabled                MCPServerState = "disabled"
	MCPServerStateNeedsAuth               MCPServerState = "needs_auth"
	MCPServerStateNeedsClientRegistration MCPServerState = "needs_client_registration"
)

type MCPAuthStatus string

const (
	MCPAuthStatusUnsupported MCPAuthStatus = "unsupported"
	MCPAuthStatusNotLoggedIn MCPAuthStatus = "not_logged_in"
	MCPAuthStatusBearerToken MCPAuthStatus = "bearer_token"
	MCPAuthStatusOAuth       MCPAuthStatus = "oauth"
)

func classifyConnectError(err error) MCPServerState {
	if err == nil {
		return MCPServerStateConnected
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized"):
		return MCPServerStateNeedsAuth
	case strings.Contains(msg, "client registration"):
		return MCPServerStateNeedsClientRegistration
	default:
		return MCPServerStateFailed
	}
}

func authStatusForConfig(cfg ServerConfig) MCPAuthStatus {
	if strings.TrimSpace(cfg.URL) == "" {
		return MCPAuthStatusUnsupported
	}
	if cfg.OAuth != nil {
		return MCPAuthStatusNotLoggedIn
	}
	if hasAuthHeaders(cfg.Headers) {
		return MCPAuthStatusBearerToken
	}
	return MCPAuthStatusUnsupported
}

func authStatusAfterConnectError(cfg ServerConfig, err error) MCPAuthStatus {
	if classifyConnectError(err) == MCPServerStateNeedsAuth {
		return MCPAuthStatusNotLoggedIn
	}
	return authStatusForConfig(cfg)
}

func hasAuthHeaders(headers map[string]string) bool {
	for key, value := range headers {
		key = strings.ToLower(strings.TrimSpace(key))
		if strings.TrimSpace(value) == "" {
			continue
		}
		if key == "authorization" || strings.Contains(key, "api-key") || strings.Contains(key, "apikey") {
			return true
		}
	}
	return false
}

// ServerStatus describes one MCP server's runtime state.
type ServerStatus struct {
	Name       string         `json:"name"`
	State      MCPServerState `json:"state"`
	AuthStatus MCPAuthStatus  `json:"auth_status,omitempty"`
	Connected  bool           `json:"connected"`
	ToolCount  int            `json:"tool_count"`
	Error      string         `json:"error,omitempty"`
}

// PromptCacheStablePrefix returns the number of built-in tools that should
// be kept before MCP tools in the tool list. This preserves prompt cache
// stability: built-in tools form a stable prefix; MCP tools (which may
// change across sessions) are appended so they don't invalidate cache
// anchors inside the built-in block.
func PromptCacheStablePrefix() int {
	// wuu currently registers ~20 built-in tools. We return a generous
	// upper bound so the toolkit can sort MCP tools after all built-ins.
	return 50
}
