package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

func ptrToolResult(text string) *toolresult.Result {
	r := toolresult.FromText(text)
	return &r
}

type fakeGrepTool struct{ text string }

func (f fakeGrepTool) Name() string { return "grep" }
func (f fakeGrepTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{Name: "grep"}
}
func (f fakeGrepTool) Execute(context.Context, string) (string, error) { return f.text, nil }
func (f fakeGrepTool) IsReadOnly() bool                                { return true }
func (f fakeGrepTool) IsConcurrencySafe() bool                         { return true }

type fakeRichMediaTool struct{ text string }

func (f fakeRichMediaTool) Name() string { return "mcp_rich_media" }
func (f fakeRichMediaTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{Name: f.Name()}
}
func (f fakeRichMediaTool) Execute(context.Context, string) (string, error) {
	return f.text, nil
}
func (f fakeRichMediaTool) ExecuteResult(context.Context, string) (toolresult.Result, error) {
	return toolresult.Result{
		Content: []toolresult.ContentPart{
			{Type: toolresult.ContentTypeText, Text: f.text},
			{Type: toolresult.ContentTypeImage, Data: "aW1hZ2U=", MIMEType: "image/png", Name: "screen.png"},
		},
		StructuredContent: json.RawMessage(`{"caption":"structured metadata"}`),
		Meta:              json.RawMessage(`{"source":"mcp"}`),
	}, nil
}
func (f fakeRichMediaTool) IsReadOnly() bool        { return true }
func (f fakeRichMediaTool) IsConcurrencySafe() bool { return true }

func runFakeGrep(t *testing.T, mode string) (providers.ToolCall, string, []ToolExecutionRecord) {
	t.Helper()
	t.Setenv(projectionModeEnvVar, "") // isolate from any ambient override
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New toolkit: %v", err)
	}
	kit.env.SessionDir = t.TempDir()
	kit.env.ToolResultProjectionMode = mode

	call := providers.ToolCall{ID: "call-int", Name: "grep", Arguments: "{}"}
	returned, err := kit.executeKnownToolResultWithRepeatPolicy(
		context.Background(), call, fakeGrepTool{text: grepContentEnvelope(3000)}, true)
	if err != nil {
		t.Fatalf("execute (mode=%s): %v", mode, err)
	}
	return call, returned.TextProjection(), kit.ToolTelemetry()
}

func recordFor(records []ToolExecutionRecord, callID string) *ToolExecutionRecord {
	for i := range records {
		if records[i].CallID == callID {
			return &records[i]
		}
	}
	return nil
}

func assertGenericContinuation(t *testing.T, text string) {
	t.Helper()
	m := parseOut(t, text)
	if m["kind"] != "archived_tool_result" || m["artifact_ref"] == "" {
		t.Fatalf("generic budget omitted artifact identity: %s", snip(text, 200))
	}
	continuation, _ := m["continuation"].(map[string]any)
	if continuation == nil || continuation["next"] == nil {
		t.Fatalf("generic budget omitted stable continuation: %s", snip(text, 300))
	}
}

func TestChokePoint_ModeOff_UsesGenericBudgetNoDiagnostics(t *testing.T) {
	call, text, records := runFakeGrep(t, "off")
	assertGenericContinuation(t, text)
	rec := recordFor(records, call.ID)
	if rec == nil || rec.Projection != nil {
		t.Fatalf("off mode must not compute projection diagnostics: %+v", rec)
	}
}

func TestChokePoint_ModeShadow_MeasuresButDoesNotApply(t *testing.T) {
	call, text, records := runFakeGrep(t, "shadow")
	assertGenericContinuation(t, text)
	rec := recordFor(records, call.ID)
	if rec == nil || rec.Projection == nil {
		t.Fatalf("shadow mode must record projection diagnostics")
	}
	if !rec.Projection.Applied {
		t.Fatalf("shadow diagnostics should show the projection would apply: %+v", rec.Projection)
	}
	if rec.Projection.ProjectionHash == "" || rec.Projection.OriginalHash == "" {
		t.Fatalf("shadow diagnostics must carry content hashes for stability tracking")
	}
}

func TestChokePoint_ModeActive_AppliesBoundedProjection(t *testing.T) {
	call, text, records := runFakeGrep(t, "active")
	if !strings.HasPrefix(strings.TrimSpace(text), "{") {
		t.Fatalf("active mode must return the projected JSON envelope, got: %s", snip(text, 80))
	}
	if got := estimateResultTokens(text); got > defaultProjectionTokenBudget {
		t.Fatalf("active projection over budget: %d tokens", got)
	}
	if !strings.Contains(text, "projection") || !strings.Contains(text, "artifact_ref") {
		t.Fatalf("active projection must reference its artifact")
	}
	rec := recordFor(records, call.ID)
	if rec == nil || rec.Projection == nil || !rec.Projection.Applied {
		t.Fatalf("active mode must record an applied projection: %+v", rec)
	}
	if rec.Projection.ProjectedTokens >= rec.Projection.OriginalTokens {
		t.Fatalf("active projection must reduce tokens: %+v", rec.Projection)
	}
	if rec.ResultRef == "" {
		t.Fatalf("active projection must record a recovery ref")
	}
}

func TestChokePoint_EnvOverrideBeatsConfiguredMode(t *testing.T) {
	t.Setenv(projectionModeEnvVar, "active")
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.env.SessionDir = t.TempDir()
	kit.env.ToolResultProjectionMode = "off" // env override should win
	call := providers.ToolCall{ID: "c", Name: "grep", Arguments: "{}"}
	returned, err := kit.executeKnownToolResultWithRepeatPolicy(
		context.Background(), call, fakeGrepTool{text: grepContentEnvelope(3000)}, true)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(returned.TextProjection()), "{") {
		t.Fatalf("env override to active did not take effect: %s", snip(returned.TextProjection(), 80))
	}
}

// TestActiveProjection_IsStableThroughWireProjection proves the fix closes the
// bypass: once the result is finalized (bounded, text-only), the provider
// projection cannot restore a larger result, and repeated preparation is
// byte-identical (cache-safe within an epoch).
func TestActiveProjection_IsStableThroughWireProjection(t *testing.T) {
	call, projectedText, _ := runFakeGrep(t, "active")

	stable := providers.ChatMessage{
		Role:       "tool",
		Name:       "grep",
		ToolCallID: call.ID,
		Content:    projectedText,
		ToolResult: ptrToolResult(projectedText),
	}
	msgs := []providers.ChatMessage{
		{Role: "user", Content: "search"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: call.ID, Name: "grep"}}},
		stable,
	}

	first, err := providers.PrepareMessagesForModelRequest("gpt-5", msgs)
	if err != nil {
		t.Fatalf("prepare#1: %v", err)
	}
	second, err := providers.PrepareMessagesForModelRequest("gpt-5", first)
	if err != nil {
		t.Fatalf("prepare#2: %v", err)
	}
	c1 := toolContent(first, call.ID)
	c2 := toolContent(second, call.ID)
	if c1 != projectedText {
		t.Fatalf("wire content diverged from the finalized projection")
	}
	if c1 != c2 {
		t.Fatalf("wire content not stable across repeated preparation")
	}
	if got := estimateResultTokens(c1); got > defaultProjectionTokenBudget {
		t.Fatalf("wire content exceeded budget: %d tokens", got)
	}
}

func TestRichMediaSettlement_IsStableAndKeepsNativeObservation(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.env.SessionDir = t.TempDir()
	tool := fakeRichMediaTool{text: strings.Repeat("semantic evidence line\n", 3_000)}
	call := providers.ToolCall{ID: "call-rich", Name: tool.Name(), Arguments: "{}"}
	returned, err := kit.executeKnownToolResultWithRepeatPolicy(context.Background(), call, tool, true)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	record := recordFor(kit.ToolTelemetry(), call.ID)
	if record == nil || !record.ResultBudgeted || record.ResultRef == "" {
		t.Fatalf("rich result did not cross settlement boundary: %+v", record)
	}
	if len(returned.Content) != 2 || returned.Content[1].Type != toolresult.ContentTypeImage {
		t.Fatalf("native media was not retained: %+v", returned.Content)
	}
	if !strings.Contains(returned.TextProjection(), record.ResultRef) || strings.Contains(returned.TextProjection(), "structured metadata") {
		t.Fatalf("model projection lacks recovery index or leaks duplicate metadata: %.300q", returned.TextProjection())
	}

	messages := []providers.ChatMessage{
		{Role: "user", Content: "inspect"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: call.ID, Name: call.Name}}},
		{Role: "tool", Name: call.Name, ToolCallID: call.ID, Content: returned.TextProjection(), ToolResult: &returned},
	}
	first, err := providers.PrepareMessagesForModelRequest("gpt-5", messages)
	if err != nil {
		t.Fatalf("prepare#1: %v", err)
	}
	second, err := providers.PrepareMessagesForModelRequest("gpt-5", messages)
	if err != nil {
		t.Fatalf("prepare#2: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("rich result request projection is not byte-stable")
	}
	if got := toolContent(first, call.ID); !strings.Contains(got, record.ResultRef) || !strings.Contains(got, "structured metadata") || strings.Contains(got, `"source":"mcp"`) {
		t.Fatalf("wire tool text lacks bounded structured semantics or leaks private meta: %.500q", got)
	}
	if len(first) != 4 || len(first[3].Images) != 1 || first[3].Images[0].Data != "aW1hZ2U=" {
		t.Fatalf("native image observation missing: %+v", first)
	}
}

func toolContent(msgs []providers.ChatMessage, callID string) string {
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == callID {
			return m.Content
		}
	}
	return ""
}
