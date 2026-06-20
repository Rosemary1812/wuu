package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/modelprofile"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestExtensionSurfacePolicyHidesAndBlocksSkillTools(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetExtensionSurfacePolicy(RestrictedExtensionSurfacePolicy())

	if definitionNames(kit.Definitions())["load_skill"] {
		t.Fatal("restricted extension policy should hide load_skill")
	}
	info, ok := kit.ToolInfo("load_skill")
	if !ok {
		t.Fatal("load_skill should remain a known tool")
	}
	if info.Exposure != ToolExposureHidden {
		t.Fatalf("load_skill exposure = %s, want %s", info.Exposure, ToolExposureHidden)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "load_skill",
		Arguments: `{"name":"docs"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=extension_surface_denied") {
		t.Fatalf("expected extension surface denial, got %v", err)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || records[0].ErrorKind != "extension_surface_denied" || records[0].PolicyAction != ToolPolicyDeny {
		t.Fatalf("unexpected telemetry: %+v", records)
	}
}

func TestExtensionSurfacePolicyHidesAndBlocksDeferredMCPTools(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.registry = NewRegistry(
		NewToolSearchTool(kit),
		&stubTool{
			name:   "mcp_docs_search",
			def:    providers.ToolDefinition{Name: "mcp_docs_search", Description: "Search docs through MCP"},
			result: `{"action":"mcp_docs_search"}`,
		},
	)
	kit.SetExtensionSurfacePolicy(RestrictedExtensionSurfacePolicy())

	if definitionNames(kit.Definitions())["mcp_docs_search"] {
		t.Fatal("restricted extension policy should hide MCP tools")
	}
	info, ok := kit.ToolInfo("mcp_docs_search")
	if !ok {
		t.Fatal("mcp_docs_search should remain a known tool")
	}
	if info.Kind != ToolKindMCP || info.Exposure != ToolExposureHidden {
		t.Fatalf("unexpected MCP info: %+v", info)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "tool_search",
		Arguments: `{"query":"docs search"}`,
	})
	if err != nil {
		t.Fatalf("tool_search: %v", err)
	}
	var parsed struct {
		Matched      int      `json:"matched"`
		ExposedTools []string `json:"exposed_tools"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse tool_search: %v", err)
	}
	if parsed.Matched != 0 || len(parsed.ExposedTools) != 0 {
		t.Fatalf("tool_search should not expose disabled extension tools: %+v", parsed)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{Name: "mcp_docs_search", Arguments: `{}`})
	if err == nil || !strings.Contains(err.Error(), "error_kind=extension_surface_denied") {
		t.Fatalf("expected direct MCP execution denial, got %v", err)
	}
}

func TestExtensionSurfacePolicyBlocksMCPEvenWhenProfileAllowsCapability(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.registry = NewRegistry(
		NewToolSearchTool(kit),
		&stubTool{
			name:   "mcp_docs_search",
			def:    providers.ToolDefinition{Name: "mcp_docs_search", Description: "Search docs through MCP"},
			result: `{"action":"mcp_docs_search"}`,
		},
	)
	kit.SetActiveProfile(modelprofile.Resolve("openai", "gpt-5-codex"))
	kit.SetExtensionSurfacePolicy(RestrictedExtensionSurfacePolicy())

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "tool_search",
		Arguments: `{"query":"docs search"}`,
	})
	if err != nil {
		t.Fatalf("tool_search: %v", err)
	}
	var parsed struct {
		Matched      int      `json:"matched"`
		ExposedTools []string `json:"exposed_tools"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse tool_search: %v", err)
	}
	if parsed.Matched != 0 || len(parsed.ExposedTools) != 0 {
		t.Fatalf("restricted extension policy should block MCP discovery: %+v", parsed)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{Name: "mcp_docs_search", Arguments: `{}`})
	if err == nil || !strings.Contains(err.Error(), "error_kind=extension_surface_denied") {
		t.Fatalf("expected extension surface denial, got %v", err)
	}
}

func TestExtensionSurfacePolicyHidesWorkflowToolsAndClones(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetExtensionSurfacePolicy(RestrictedExtensionSurfacePolicy())

	if definitionNames(kit.Definitions())["list_workflows"] {
		t.Fatal("restricted extension policy should hide workflow tools")
	}
	states := kit.ExtensionToolSurfaceStates()
	if states.Workflows.Allowed || states.Workflows.KnownTools == 0 || states.Workflows.VisibleTools != 0 {
		t.Fatalf("unexpected workflow surface state: %+v", states.Workflows)
	}

	clone, err := kit.CloneForRoot(t.TempDir())
	if err != nil {
		t.Fatalf("CloneForRoot: %v", err)
	}
	if definitionNames(clone.Definitions())["list_workflows"] {
		t.Fatal("clone should keep restricted extension policy")
	}
}
