// Package mcp implements a lightweight Model Context Protocol client.
//
// It supports stdio, streamable HTTP (MCP spec revision 2025-03-26+), and
// legacy SSE transports, plus tool discovery and invocation. The design
// keeps Wuu's transport layer small while preserving the semantics expected
// by coding-agent harnesses. See streamable_http.go for the remote transport
// selection and SSE fallback rules.
package mcp

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
)

// PreferredProtocolVersion is the newest MCP revision Wuu implements.
const PreferredProtocolVersion = "2026-06-30"

var acceptedProtocolVersions = map[string]struct{}{
	"2026-06-30": {},
	"2025-11-25": {},
	"2025-06-18": {},
	"2025-03-26": {},
	"2024-11-05": {},
}

func validateProtocolVersion(version string) error {
	if _, ok := acceptedProtocolVersions[version]; !ok {
		return fmt.Errorf("unsupported MCP protocol version %q; supported versions are 2026-06-30, 2025-11-25, 2025-06-18, 2025-03-26, and 2024-11-05", version)
	}
	return nil
}

// Request is a JSON-RPC request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Response is a JSON-RPC response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("mcp rpc error %d: %s", e.Code, e.Message)
}

// InitializeParams are sent in the initialize request.
type InitializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities    struct {
	} `json:"capabilities"`
	ClientInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
}

// InitializeResult is returned by initialize.
type InitializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities    struct {
		Tools *struct {
			ListChanged bool `json:"listChanged,omitempty"`
		} `json:"tools,omitempty"`
	} `json:"capabilities"`
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

// Tool represents an MCP tool definition.
type Tool struct {
	Name         string           `json:"name"`
	Title        string           `json:"title,omitempty"`
	Description  string           `json:"description"`
	InputSchema  json.RawMessage  `json:"inputSchema"`
	OutputSchema json.RawMessage  `json:"outputSchema,omitempty"`
	Annotations  *ToolAnnotations `json:"annotations,omitempty"`
	Meta         json.RawMessage  `json:"_meta,omitempty"`
}

// ToolAnnotations carries MCP server-provided behavioral hints for a tool.
type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// ListToolsResult is returned by tools/list.
type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

// CallToolParams are sent in tools/call.
type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// CallToolResult is returned by tools/call.
type CallToolResult struct {
	Content           []ToolContent   `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
	Meta              json.RawMessage `json:"_meta,omitempty"`
}

// ToolContent is a single content item in a tool result.
type ToolContent struct {
	Type        string          `json:"type"`
	Text        string          `json:"text,omitempty"`
	Data        string          `json:"data,omitempty"`
	MIMEType    string          `json:"mimeType,omitempty"`
	URI         string          `json:"uri,omitempty"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Resource    json.RawMessage `json:"resource,omitempty"`
	Meta        json.RawMessage `json:"_meta,omitempty"`
}

var requestIDCounter int64

func nextRequestID() int64 {
	return atomic.AddInt64(&requestIDCounter, 1)
}

// inFlight tracks pending requests.
type inFlight struct {
	mu      sync.Mutex
	pending map[int64]chan Response
	closed  bool
}

func newInFlight() *inFlight {
	return &inFlight{pending: make(map[int64]chan Response)}
}

func (f *inFlight) register(id int64) <-chan Response {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		ch := make(chan Response, 1)
		ch <- Response{Error: &RPCError{Code: -32000, Message: "client closed"}}
		return ch
	}
	ch := make(chan Response, 1)
	f.pending[id] = ch
	return ch
}

func (f *inFlight) resolve(id int64, resp Response) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch, ok := f.pending[id]
	if !ok {
		return false
	}
	delete(f.pending, id)
	ch <- resp
	return true
}

func (f *inFlight) closeAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	for id, ch := range f.pending {
		ch <- Response{Error: &RPCError{Code: -32000, Message: "client closed"}}
		delete(f.pending, id)
	}
}
