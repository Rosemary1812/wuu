package hooks

import "encoding/json"

// Output is the parsed response from a hook process.
// All fields are optional; a hook that simply exits 0 produces a zero Output.
type Output struct {
	Continue           *bool               `json:"continue,omitempty"`
	Decision           string              `json:"decision,omitempty"`
	Reason             string              `json:"reason,omitempty"`
	UpdatedInput       json.RawMessage     `json:"updated_input,omitempty"`
	Context            string              `json:"additional_context,omitempty"`
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// HookSpecificOutput mirrors the Codex/Claude hook result shape while keeping
// Wuu's existing top-level fields working.
type HookSpecificOutput struct {
	HookEventName            string          `json:"hookEventName,omitempty"`
	PermissionDecision       string          `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string          `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             json.RawMessage `json:"updatedInput,omitempty"`
	AdditionalContext        string          `json:"additionalContext,omitempty"`
}

// IsBlocked returns true when the hook wants to block the operation.
func (o *Output) IsBlocked() bool {
	if o == nil {
		return false
	}
	if o.Decision == "block" {
		return true
	}
	if o.Continue != nil && !*o.Continue {
		return true
	}
	return false
}

// ParseOutput interprets hook stdout. If stdout is valid JSON, it is decoded
// into Output. Otherwise, the exit code is used as the sole signal:
// 0 means continue, 2 means block. All other exit codes are treated as
// execution failures by the caller, not handled here.
func ParseOutput(stdout []byte, exitCode int) (*Output, error) {
	out := &Output{}
	if len(stdout) > 0 && json.Valid(stdout) {
		if err := json.Unmarshal(stdout, out); err == nil {
			out.Normalize()
			return out, nil
		}
	}
	if exitCode == 2 {
		out.Decision = "block"
	}
	return out, nil
}

// Normalize promotes Codex/Claude-compatible hookSpecificOutput fields into
// Wuu's canonical Output fields.
func (o *Output) Normalize() {
	if o == nil || o.HookSpecificOutput == nil {
		return
	}
	hookOut := o.HookSpecificOutput
	if len(o.UpdatedInput) == 0 && len(hookOut.UpdatedInput) > 0 {
		o.UpdatedInput = hookOut.UpdatedInput
	}
	if o.Context == "" && hookOut.AdditionalContext != "" {
		o.Context = hookOut.AdditionalContext
	}
	if hookOut.PermissionDecision == "deny" {
		o.Decision = "block"
		if o.Reason == "" {
			o.Reason = hookOut.PermissionDecisionReason
		}
	}
}
