package structuredoutput

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestValidatorAcceptsSingleSchemaValue(t *testing.T) {
	validator, err := New(json.RawMessage(`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"}},"additionalProperties":false}`))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := validator.Validate(`{"ok":true}`); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := validator.Validate(`{"ok":"yes"}`); err == nil {
		t.Fatal("Validate accepted a value that violates the schema")
	}
	if err := validator.Validate(`{"ok":true}{"ok":false}`); err == nil {
		t.Fatal("Validate accepted multiple JSON values")
	}
}

func TestValidatorRetryPromptIncludesFailureAndPreviousValue(t *testing.T) {
	validator, err := New(json.RawMessage(`{"type":"string","minLength":3}`))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	prompt := validator.RetryPrompt(`"no"`, errors.New("too short"))
	if !strings.Contains(prompt, "Previous final answer") || !strings.Contains(prompt, `"no"`) {
		t.Fatalf("retry prompt = %q", prompt)
	}
}
