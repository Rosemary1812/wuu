package pluginhost

import (
	"encoding/json"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

// Hook identifies a typed interception point exposed by the Wuu runtime.
type Hook string

const (
	HookSessionStart      Hook = "session.start"
	HookSessionStop       Hook = "session.stop"
	HookChatMessage       Hook = "chat.message"
	HookChatRequest       Hook = "chat.request"
	HookToolDefinition    Hook = "tool.definition"
	HookToolExecuteBefore Hook = "tool.execute.before"
	HookToolExecuteAfter  Hook = "tool.execute.after"
	HookShellEnv          Hook = "shell.env"
)

var validHooks = map[Hook]struct{}{
	HookSessionStart:      {},
	HookSessionStop:       {},
	HookChatMessage:       {},
	HookChatRequest:       {},
	HookToolDefinition:    {},
	HookToolExecuteBefore: {},
	HookToolExecuteAfter:  {},
	HookShellEnv:          {},
}

// IsValidHook reports whether name is part of the public plugin protocol.
func IsValidHook(name Hook) bool {
	_, ok := validHooks[name]
	return ok
}

// InitializeParams describes the runtime instance offered to a plugin.
type InitializeParams struct {
	ProtocolVersion int    `json:"protocol_version"`
	PluginID        string `json:"plugin_id"`
	PluginRoot      string `json:"plugin_root"`
	ProjectRoot     string `json:"project_root"`
	WuuHome         string `json:"wuu_home"`
}

// InitializeResult declares the interception points implemented by a plugin.
type InitializeResult struct {
	Hooks []Hook `json:"hooks"`
}

// InvokeParams carries one interception through the external plugin protocol.
// Input is immutable event context. Output is the mutable value produced by
// earlier plugins in the chain.
type InvokeParams struct {
	Hook   Hook            `json:"hook"`
	Input  json.RawMessage `json:"input"`
	Output json.RawMessage `json:"output"`
}

// InvokeResult returns the next output value in the deterministic plugin chain.
type InvokeResult struct {
	Output json.RawMessage `json:"output"`
}

// ChatRequestInput identifies one provider-neutral model request without
// duplicating the mutable request payload carried in InvokeParams.Output.
type ChatRequestInput struct {
	SessionID string `json:"session_id,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
	CWD       string `json:"cwd"`
	Provider  string `json:"provider"`
	StepIndex int    `json:"step_index"`
}

// ChatRequestOutput is the public, provider-neutral model request surface.
// Transport-owned cache and correlation fields stay under Wuu's control.
type ChatRequestOutput struct {
	Model                       string                     `json:"model"`
	Messages                    []providers.ChatMessage    `json:"messages"`
	Tools                       []providers.ToolDefinition `json:"tools,omitempty"`
	Temperature                 float64                    `json:"temperature,omitempty"`
	MaxTokens                   int                        `json:"max_tokens,omitempty"`
	Effort                      string                     `json:"effort,omitempty"`
	ProviderOptions             map[string]any             `json:"provider_options,omitempty"`
	NativeDeferredToolDiscovery bool                       `json:"native_deferred_tool_discovery,omitempty"`
	ForceToolName               string                     `json:"force_tool_name,omitempty"`
}

type ChatMessageInput struct {
	SessionID string `json:"session_id,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
	CWD       string `json:"cwd"`
}

type ChatMessageOutput struct {
	Content        string                 `json:"content"`
	DisplayContent string                 `json:"display_content,omitempty"`
	Images         []providers.InputImage `json:"images,omitempty"`
	Files          []providers.InputFile  `json:"files,omitempty"`
}

type ToolExecuteInput struct {
	SessionID string          `json:"session_id,omitempty"`
	ThreadID  string          `json:"thread_id,omitempty"`
	CWD       string          `json:"cwd"`
	StepIndex int             `json:"step_index,omitempty"`
	CallID    string          `json:"call_id"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolExecuteBeforeOutput struct {
	Arguments json.RawMessage `json:"arguments"`
}

type ToolExecuteAfterOutput struct {
	Result toolresult.Result `json:"result"`
	Error  string            `json:"error,omitempty"`
}

type State string

const (
	StateStarting State = "starting"
	StateActive   State = "active"
	StateFailed   State = "failed"
	StateStopped  State = "stopped"
)

// Status is a user-safe snapshot of one plugin runtime.
type Status struct {
	ID        string    `json:"id"`
	State     State     `json:"state"`
	Hooks     []Hook    `json:"hooks,omitempty"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
}
