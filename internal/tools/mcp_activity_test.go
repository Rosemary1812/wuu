package tools

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/blueberrycongee/wuu/internal/activity"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

func TestCropTransparentPNGRemovesWindowShadowPadding(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 8, 10))
	for y := 2; y < 9; y++ {
		for x := 1; x < 8; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 48})
		}
	}
	for y := 3; y < 8; y++ {
		for x := 2; x < 7; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 20, G: 30, B: 40, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	cropped, err := png.Decode(bytes.NewReader(cropTransparentPNG(encoded.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	if got := cropped.Bounds().Size(); got.X != 5 || got.Y != 5 {
		t.Fatalf("cropped size = %v, want 5x5", got)
	}
}

type activityTestTool struct {
	calls int
}

func (t *activityTestTool) Name() string { return "mcp_plugin_cua_mac_computer_computer" }
func (t *activityTestTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{Name: t.Name(), InputSchema: map[string]any{"type": "object"}}
}
func (t *activityTestTool) Execute(context.Context, string) (string, error) {
	panic("rich path required")
}
func (t *activityTestTool) ExecuteResult(context.Context, string) (toolresult.Result, error) {
	t.calls++
	return toolresult.Result{Content: []toolresult.ContentPart{
		{Type: toolresult.ContentTypeText, Text: "observed"},
		{Type: toolresult.ContentTypeImage, Data: "cG5n", MIMEType: "image/png"},
	}}, nil
}
func (t *activityTestTool) IsReadOnly() bool        { return false }
func (t *activityTestTool) IsConcurrencySafe() bool { return false }

func TestExecuteActivityBoundToolCreatesRefAndHonorsTakeover(t *testing.T) {
	registry := activity.NewRegistry()
	tool := &activityTestTool{}
	artifacts := t.TempDir()
	kit := &Toolkit{
		env:              &Env{RootDir: "/repo", SessionID: "thread-1", SessionDir: artifacts},
		registry:         NewRegistry(tool),
		boundary:         StandardBoundary(),
		activityRegistry: registry,
		mcpActivityBindings: map[string]MCPActivityBinding{
			"plugin.cua-mac.computer": {Kind: activity.KindCUA, PluginID: "cua-mac"},
		},
	}
	call := providers.ToolCall{ID: "call-1", Name: tool.Name(), Arguments: `{"action":"observe","app":"com.apple.TextEdit"}`}

	result, err := kit.executeActivityBoundToolResult(context.Background(), call, tool, "plugin.cua-mac.computer")
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	if result.Activity == nil || result.Activity.Kind != string(activity.KindCUA) || result.Activity.ThreadID != "thread-1" {
		t.Fatalf("activity result = %+v", result.Activity)
	}
	if result.Activity.PreviewURI == "" {
		t.Fatalf("activity preview missing: %+v", result.Activity)
	}
	previewPath := filepath.Join(artifacts, "activities", result.Activity.ID, "preview.png")
	if data, readErr := os.ReadFile(previewPath); readErr != nil || string(data) != "png" {
		t.Fatalf("preview artifact = %q, %v", data, readErr)
	}
	sessions := registry.List("thread-1")
	if len(sessions) != 1 || sessions[0].PluginID != "cua-mac" || sessions[0].Target != "com.apple.TextEdit" || sessions[0].State != activity.StateActive || sessions[0].Preview != result.Activity.PreviewURI {
		t.Fatalf("activity sessions = %+v", sessions)
	}
	if tool.calls != 1 {
		t.Fatalf("tool calls = %d", tool.calls)
	}

	if _, err := registry.Takeover("thread-1", sessions[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := kit.executeActivityBoundToolResult(context.Background(), call, tool, "plugin.cua-mac.computer"); !errors.Is(err, activity.ErrControlRevoked) {
		t.Fatalf("execute after takeover = %v, want ErrControlRevoked", err)
	}
	if tool.calls != 1 {
		t.Fatalf("revoked call reached helper: calls=%d", tool.calls)
	}

	if _, _, err := registry.Release("thread-1", sessions[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := kit.executeActivityBoundToolResult(context.Background(), call, tool, "plugin.cua-mac.computer"); err != nil {
		t.Fatalf("execute after release: %v", err)
	}
	if tool.calls != 2 {
		t.Fatalf("released call count = %d", tool.calls)
	}

	if _, err := registry.Stop("thread-1", sessions[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := kit.executeActivityBoundToolResult(context.Background(), call, tool, "plugin.cua-mac.computer"); !errors.Is(err, activity.ErrStopped) {
		t.Fatalf("execute after stop = %v, want ErrStopped", err)
	}
	if tool.calls != 2 {
		t.Fatalf("stopped call reached helper: calls=%d", tool.calls)
	}
}

func TestExecuteActivityBoundToolRequiresThreadContext(t *testing.T) {
	tool := &activityTestTool{}
	kit := &Toolkit{
		env:              &Env{RootDir: "/repo"},
		registry:         NewRegistry(tool),
		boundary:         StandardBoundary(),
		activityRegistry: activity.NewRegistry(),
		mcpActivityBindings: map[string]MCPActivityBinding{
			"plugin.cua-mac.computer": {Kind: activity.KindCUA, PluginID: "cua-mac"},
		},
	}
	call := providers.ToolCall{ID: "call-1", Name: tool.Name(), Arguments: `{"action":"observe"}`}
	if _, err := kit.executeActivityBoundToolResult(context.Background(), call, tool, "plugin.cua-mac.computer"); err == nil {
		t.Fatal("expected missing thread context error")
	}
	if tool.calls != 0 {
		t.Fatalf("missing-thread call reached helper: calls=%d", tool.calls)
	}
}
