package pluginhost

import (
	"encoding/json"
	"time"
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
