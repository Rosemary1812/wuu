package mcp

import (
	"encoding/json"
	"testing"
)

func TestProtocolVersionNegotiation(t *testing.T) {
	if PreferredProtocolVersion != "2026-06-30" {
		t.Fatalf("preferred version = %q", PreferredProtocolVersion)
	}
	for _, version := range []string{"2026-06-30", "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"} {
		if err := validateProtocolVersion(version); err != nil {
			t.Fatalf("version %s rejected: %v", version, err)
		}
	}
	if err := validateProtocolVersion("2027-01-01"); err == nil {
		t.Fatal("unknown MCP version should be rejected")
	}
}

func TestToolProtocolTypesPreserveSchemasAnnotationsAndMetadata(t *testing.T) {
	raw := []byte(`{
  "name":"inspect",
  "title":"Inspect",
  "description":"Inspect state",
  "inputSchema":{"type":"object"},
  "outputSchema":{"type":"object","properties":{"ok":{"type":"boolean"}}},
  "annotations":{"title":"Safe inspect","readOnlyHint":true,"destructiveHint":false,"idempotentHint":true,"openWorldHint":false},
  "_meta":{"vendor":"kept"}
}`)
	var tool Tool
	if err := json.Unmarshal(raw, &tool); err != nil {
		t.Fatal(err)
	}
	if tool.Title != "Inspect" || len(tool.OutputSchema) == 0 || len(tool.Meta) == 0 || tool.Annotations.IdempotentHint == nil || !*tool.Annotations.IdempotentHint {
		t.Fatalf("tool protocol fields lost: %+v", tool)
	}
}
