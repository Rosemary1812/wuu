package mcp

import (
	"encoding/json"
	"testing"
)

func TestToolAnnotationsDecodeReadOnlyHint(t *testing.T) {
	var result ListToolsResult
	if err := json.Unmarshal([]byte(`{
  "tools": [
    {
      "name": "inspect",
      "description": "Inspect state",
      "inputSchema": {},
      "annotations": {
        "readOnlyHint": true
      }
    }
  ]
}`), &result); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("decoded %d tools, want 1", len(result.Tools))
	}
	hint := result.Tools[0].Annotations.ReadOnlyHint
	if hint == nil || *hint != true {
		t.Fatalf("readOnlyHint = %v, want true", hint)
	}
}

func TestMCPTool_MetadataDefaultsConservative(t *testing.T) {
	tool := NewMCPTool(&Client{name: "server"}, Tool{Name: "inspect"})

	if tool.IsReadOnly() {
		t.Fatal("tool without readOnlyHint should not be read-only")
	}
	if tool.IsConcurrencySafe() {
		t.Fatal("tool without readOnlyHint should not be concurrency-safe")
	}
}

func TestMCPTool_ReadOnlyHintEnablesReadOnlyAndConcurrency(t *testing.T) {
	tool := NewMCPTool(&Client{name: "server"}, Tool{
		Name:        "inspect",
		Annotations: &ToolAnnotations{ReadOnlyHint: boolPtr(true)},
	})

	if !tool.IsReadOnly() {
		t.Fatal("readOnlyHint=true should make tool read-only")
	}
	if !tool.IsConcurrencySafe() {
		t.Fatal("readOnlyHint=true should make tool concurrency-safe")
	}
}

func TestMCPTool_WritableHintStaysSerial(t *testing.T) {
	tool := NewMCPTool(&Client{name: "server"}, Tool{
		Name:        "mutate",
		Annotations: &ToolAnnotations{ReadOnlyHint: boolPtr(false)},
	})

	if tool.IsReadOnly() {
		t.Fatal("readOnlyHint=false should not make tool read-only")
	}
	if tool.IsConcurrencySafe() {
		t.Fatal("readOnlyHint=false should not make tool concurrency-safe")
	}
}

func TestMCPTool_ConfigOverrideWinsOverAnnotation(t *testing.T) {
	tests := []struct {
		name                string
		annotationReadOnly  *bool
		override            ToolOverride
		wantReadOnly        bool
		wantConcurrencySafe bool
	}{
		{
			name:                "read-only override beats writable hint",
			annotationReadOnly:  boolPtr(false),
			override:            ToolOverride{ReadOnly: boolPtr(true)},
			wantReadOnly:        true,
			wantConcurrencySafe: true,
		},
		{
			name:                "writable override beats read-only hint",
			annotationReadOnly:  boolPtr(true),
			override:            ToolOverride{ReadOnly: boolPtr(false)},
			wantReadOnly:        false,
			wantConcurrencySafe: false,
		},
		{
			name:                "concurrency override can narrow read-only hint",
			annotationReadOnly:  boolPtr(true),
			override:            ToolOverride{ConcurrencySafe: boolPtr(false)},
			wantReadOnly:        true,
			wantConcurrencySafe: false,
		},
		{
			name:                "concurrency override can opt in without read-only",
			override:            ToolOverride{ConcurrencySafe: boolPtr(true)},
			wantReadOnly:        false,
			wantConcurrencySafe: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{name: "server"}
			client.SetToolOverrides(map[string]ToolOverride{
				"tool": tt.override,
			})
			toolDef := Tool{Name: "tool"}
			if tt.annotationReadOnly != nil {
				toolDef.Annotations = &ToolAnnotations{ReadOnlyHint: tt.annotationReadOnly}
			}
			tool := NewMCPTool(client, toolDef)

			if tool.IsReadOnly() != tt.wantReadOnly {
				t.Fatalf("IsReadOnly() = %t, want %t", tool.IsReadOnly(), tt.wantReadOnly)
			}
			if tool.IsConcurrencySafe() != tt.wantConcurrencySafe {
				t.Fatalf("IsConcurrencySafe() = %t, want %t", tool.IsConcurrencySafe(), tt.wantConcurrencySafe)
			}
		})
	}
}

func TestManagerAllToolsUsesClientOverrides(t *testing.T) {
	manager := NewManager()
	client := &Client{
		name:  "server",
		tools: []Tool{{Name: "tool"}},
	}
	client.SetToolOverrides(map[string]ToolOverride{
		"tool": {ReadOnly: boolPtr(true)},
	})

	manager.mu.Lock()
	manager.clients["server"] = client
	manager.mu.Unlock()

	tools := manager.AllTools()
	if len(tools) != 1 {
		t.Fatalf("AllTools() returned %d tools, want 1", len(tools))
	}
	if !tools[0].IsReadOnly() || !tools[0].IsConcurrencySafe() {
		t.Fatalf("override metadata not applied: readOnly=%t concurrencySafe=%t", tools[0].IsReadOnly(), tools[0].IsConcurrencySafe())
	}
}

func boolPtr(v bool) *bool {
	return &v
}
