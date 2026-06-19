package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// ServerConfig describes one MCP server connection.
type ServerConfig struct {
	Name          string                  `json:"name"`
	Command       string                  `json:"command,omitempty"`
	Args          []string                `json:"args,omitempty"`
	URL           string                  `json:"url,omitempty"`
	Env           map[string]string       `json:"env,omitempty"`
	Enabled       *bool                   `json:"enabled,omitempty"`
	ToolOverrides map[string]ToolOverride `json:"tool_overrides,omitempty"`
}

func (c ServerConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// ToolOverride lets local config correct or supplement MCP tool metadata.
type ToolOverride struct {
	ReadOnly        *bool `json:"read_only,omitempty"`
	ConcurrencySafe *bool `json:"concurrency_safe,omitempty"`
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
	c.readLoop = newReadLoop(t, c.inFlight, c.handleNotification)
	c.readLoop.Start()

	// Initialize handshake.
	params := InitializeParams{ProtocolVersion: protocolVersion}
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
	t, err := NewSSETransport(cfg.URL)
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
