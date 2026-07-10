package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/blueberrycongee/wuu/internal/capability"
)

// ServerConfig describes one MCP server connection.
type ServerConfig struct {
	Name    string   `json:"name"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"`
	// Transport selects the wire protocol for URL servers: "http" (streamable
	// HTTP, aliases "streamable-http"/"streamable_http") or "sse" (legacy
	// HTTP+SSE). Empty means auto: try streamable HTTP first and fall back to
	// SSE when the endpoint rejects the initialize POST (see ConnectRemote).
	// The names follow Claude Code's .mcp.json convention. Ignored for stdio
	// (command) servers.
	Transport     string                  `json:"transport,omitempty"`
	Env           map[string]string       `json:"env,omitempty"`
	Headers       map[string]string       `json:"headers,omitempty"`
	OAuth         *OAuthConfig            `json:"oauth,omitempty"`
	Enabled       *bool                   `json:"enabled,omitempty"`
	ToolOverrides map[string]ToolOverride `json:"tool_overrides,omitempty"`
}

// Canonical ServerConfig.Transport values. "http" (not "streamable-http") is
// canonical because that is the name Claude Code's .mcp.json uses for its
// StreamableHTTPClientTransport.
const (
	TransportAuto           = ""
	TransportSSE            = "sse"
	TransportStreamableHTTP = "http"
)

// normalizeTransport canonicalizes a user-supplied transport name.
func normalizeTransport(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return TransportAuto, nil
	case "sse":
		return TransportSSE, nil
	case "http", "streamable-http", "streamable_http", "streamablehttp":
		return TransportStreamableHTTP, nil
	default:
		return "", fmt.Errorf("unsupported transport %q (want \"http\"/\"streamable-http\", \"sse\", or empty for auto)", v)
	}
}

type OAuthConfig struct {
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
	RedirectURI  string   `json:"redirect_uri,omitempty"`
}

func (c ServerConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// ToolOverride lets local config correct or supplement MCP tool metadata.
type ToolOverride struct {
	ReadOnly        *bool                 `json:"read_only,omitempty"`
	ConcurrencySafe *bool                 `json:"concurrency_safe,omitempty"`
	Capability      capability.Capability `json:"capability,omitempty"`
}

// Client is a connected MCP server session.
type Client struct {
	name      string
	transport Transport
	inFlight  *inFlight
	readLoop  *readLoop
	mu        sync.RWMutex
	tools     []Tool
	overrides map[string]ToolOverride
	closed    bool
}

// Connect establishes an MCP session with the given transport.
func Connect(name string, t Transport) (*Client, error) {
	c := &Client{
		name:      name,
		transport: t,
		inFlight:  newInFlight(),
	}
	c.readLoop = newReadLoop(t, c.inFlight, c.handleNotification, c.handleRequest)
	c.readLoop.Start()

	// Initialize handshake.
	params := InitializeParams{ProtocolVersion: PreferredProtocolVersion}
	params.ClientInfo.Name = "wuu"
	params.ClientInfo.Version = "0.1.0"
	resultBytes, err := call(context.Background(), t, c.inFlight, "initialize", params)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp initialize: %w", err)
	}
	var result InitializeResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp initialize decode: %w", err)
	}
	if err := validateProtocolVersion(result.ProtocolVersion); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp initialize compatibility: %w", err)
	}

	// Send initialized notification.
	_ = t.Send(context.Background(), Request{JSONRPC: "2.0", Method: "notifications/initialized"})

	return c, nil
}

// ConnectStdio starts a local command as an MCP stdio server and connects.
func ConnectStdio(cfg ServerConfig) (*Client, error) {
	cmd := cfg.Command
	args := cfg.Args
	if cmd == "" {
		return nil, fmt.Errorf("mcp server %q: command is required for stdio transport", cfg.Name)
	}
	t, err := NewStdioTransportWithEnv(cmd, args, cfg.Env)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q: %w", cfg.Name, err)
	}
	c, err := Connect(cfg.Name, t)
	if err != nil {
		_ = t.Close()
		return nil, err
	}
	c.SetToolOverrides(cfg.ToolOverrides)
	return c, nil
}

// ConnectSSE connects to a remote MCP server over SSE.
func ConnectSSE(cfg ServerConfig) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("mcp server %q: url is required for sse transport", cfg.Name)
	}
	t, err := NewSSETransportWithHeaders(cfg.URL, cfg.Headers)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q: %w", cfg.Name, err)
	}
	c, err := Connect(cfg.Name, t)
	if err != nil {
		_ = t.Close()
		return nil, fmt.Errorf("mcp server %q: SSE transport at %s: %w", cfg.Name, cfg.URL, err)
	}
	c.SetToolOverrides(cfg.ToolOverrides)
	return c, nil
}

// ConnectStreamableHTTP connects to a remote MCP server over the streamable
// HTTP transport (MCP spec revision 2025-03-26+).
func ConnectStreamableHTTP(cfg ServerConfig) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("mcp server %q: url is required for streamable HTTP transport", cfg.Name)
	}
	t := NewStreamableHTTPTransport(cfg.URL, cfg.Headers)
	c, err := Connect(cfg.Name, t)
	if err != nil {
		// Connect already closed the transport via c.Close().
		return nil, fmt.Errorf("mcp server %q: streamable HTTP transport at %s: %w", cfg.Name, cfg.URL, err)
	}
	c.SetToolOverrides(cfg.ToolOverrides)
	return c, nil
}

// ConnectRemote connects to a URL-based MCP server, honoring
// cfg.Transport:
//
//   - "http" / "streamable-http": streamable HTTP only; a failure is
//     reported as-is with no SSE fallback (the user pinned the transport).
//   - "sse": legacy HTTP+SSE only, exactly the pre-transport-field behavior.
//   - empty (auto): POST an InitializeRequest as streamable HTTP first; if
//     the endpoint rejects it with an HTTP 4xx (e.g. 405/404) or a
//     non-JSON-RPC reply, fall back to legacy SSE. This is the client
//     backwards-compatibility strategy from the MCP spec's transports
//     chapter ("Backwards Compatibility"), so existing SSE-only server
//     configs keep working without changes. Network-level failures do not
//     fall back — SSE against the same unreachable endpoint would fail too.
func ConnectRemote(cfg ServerConfig) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("mcp server %q: url is required for a remote transport", cfg.Name)
	}
	transport, err := normalizeTransport(cfg.Transport)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q: %w", cfg.Name, err)
	}
	switch transport {
	case TransportSSE:
		return ConnectSSE(cfg)
	case TransportStreamableHTTP:
		c, err := ConnectStreamableHTTP(cfg)
		if err != nil {
			return nil, fmt.Errorf("%w (transport %q explicitly configured; not falling back to SSE)", err, strings.TrimSpace(cfg.Transport))
		}
		return c, nil
	}
	// Auto: streamable HTTP first, SSE fallback for "not a streamable HTTP
	// endpoint" failures.
	c, httpErr := ConnectStreamableHTTP(cfg)
	if httpErr == nil {
		return c, nil
	}
	var serr *streamableHTTPError
	if !errors.As(httpErr, &serr) || !serr.fallbackToSSE() {
		return nil, httpErr
	}
	c, sseErr := ConnectSSE(cfg)
	if sseErr != nil {
		return nil, fmt.Errorf("%v; SSE fallback also failed: %w", httpErr, sseErr)
	}
	return c, nil
}

// Name returns the server name.
func (c *Client) Name() string { return c.name }

// DiscoverTools fetches the tool list from the server and caches it.
func (c *Client) DiscoverTools(ctx context.Context) ([]Tool, error) {
	resultBytes, err := call(ctx, c.transport, c.inFlight, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var result ListToolsResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("decode tools/list: %w", err)
	}
	c.mu.Lock()
	c.tools = result.Tools
	c.mu.Unlock()
	return result.Tools, nil
}

func (c *Client) handleNotification(method string, _ json.RawMessage) {
	switch method {
	case "notifications/tools/list_changed", "tools/list_changed":
		go func() {
			_, _ = c.DiscoverTools(context.Background())
		}()
	}
}

func (c *Client) handleRequest(method string, _ json.RawMessage) (json.RawMessage, *RPCError) {
	switch method {
	case "elicitation/create", "elicitation/request":
		return nil, &RPCError{Code: -32000, Message: "MCP elicitation is not supported yet"}
	default:
		return nil, &RPCError{Code: -32601, Message: "MCP client request is not supported"}
	}
}

// Tools returns the cached tool list.
func (c *Client) Tools() []Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Tool, len(c.tools))
	copy(out, c.tools)
	return out
}

// SetToolOverrides replaces local metadata overrides for this server.
func (c *Client) SetToolOverrides(overrides map[string]ToolOverride) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.overrides = cloneToolOverrides(overrides)
}

// ToolOverride returns the local metadata override for a tool, when present.
func (c *Client) ToolOverride(toolName string) (ToolOverride, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	override, ok := c.overrides[toolName]
	return override, ok
}

func cloneToolOverrides(in map[string]ToolOverride) map[string]ToolOverride {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]ToolOverride, len(in))
	for name, override := range in {
		out[name] = override
	}
	return out
}

// CallTool invokes a tool on the server.
func (c *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*CallToolResult, error) {
	params := CallToolParams{Name: name, Arguments: arguments}
	resultBytes, err := call(ctx, c.transport, c.inFlight, "tools/call", params)
	if err != nil {
		return nil, err
	}
	var result CallToolResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("decode tools/call: %w", err)
	}
	return &result, nil
}

// Close shuts down the client.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	stopped := make(chan struct{})
	go func() {
		c.readLoop.Stop()
		close(stopped)
	}()
	closeErr := c.transport.Close()
	<-stopped
	c.inFlight.closeAll()
	return closeErr
}
