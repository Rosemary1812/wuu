package hooks

import (
	"encoding/json"
	"testing"
)

func TestInputMarshal(t *testing.T) {
	in := Input{
		Event:     PreToolUse,
		SessionID: "sess-1",
		CWD:       "/tmp",
		ToolName:  "run_shell",
		ToolInput: json.RawMessage(`{"command":"ls"}`),
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["hook_event_name"] != "PreToolUse" {
		t.Fatalf("expected PreToolUse, got %v", decoded["hook_event_name"])
	}
	if decoded["tool_name"] != "run_shell" {
		t.Fatalf("expected run_shell, got %v", decoded["tool_name"])
	}
}

func TestInputOmitsEmptyFields(t *testing.T) {
	in := Input{
		Event:     SessionStart,
		SessionID: "sess-1",
		CWD:       "/tmp",
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["tool_name"]; ok {
		t.Fatal("tool_name should be omitted for SessionStart")
	}
	if _, ok := decoded["prompt"]; ok {
		t.Fatal("prompt should be omitted")
	}
}

func TestOutputParseJSON(t *testing.T) {
	raw := `{"decision":"block","reason":"dangerous"}`
	out, err := ParseOutput([]byte(raw), 0)
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected block, got %s", out.Decision)
	}
	if !out.IsBlocked() {
		t.Fatal("expected IsBlocked true")
	}
}

func TestOutputFallbackExitCodeZero(t *testing.T) {
	out, err := ParseOutput([]byte("some text\n"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != "" {
		t.Fatal("expected empty decision for non-JSON output with exit 0")
	}
	if out.IsBlocked() {
		t.Fatal("should not be blocked")
	}
}

func TestOutputFallbackExitCodeBlock(t *testing.T) {
	out, err := ParseOutput([]byte("blocked reason\n"), 2)
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != "block" {
		t.Fatalf("expected block for exit 2, got %s", out.Decision)
	}
	if !out.IsBlocked() {
		t.Fatal("expected blocked")
	}
}

func TestOutputContinueFalse(t *testing.T) {
	raw := `{"continue":false,"reason":"stop here"}`
	out, err := ParseOutput([]byte(raw), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsBlocked() {
		t.Fatal("continue=false should block")
	}
}
