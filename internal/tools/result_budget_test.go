package tools

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/toolresult"
)

func TestFinalizeGenericToolResultBoundsTextAndPreservesRichMedia(t *testing.T) {
	sessionDir := t.TempDir()
	text := strings.Repeat("head evidence\n", 600) + strings.Repeat("tail evidence\n", 600)
	raw := toolresult.Result{
		Content: []toolresult.ContentPart{
			{Type: toolresult.ContentTypeText, Text: text},
			{Type: toolresult.ContentTypeImage, Data: "aW1hZ2U=", MIMEType: "image/png", Name: "screen.png"},
		},
		StructuredContent: json.RawMessage(`{"caption":"screen","private_payload":"kept"}`),
		Meta:              json.RawMessage(`{"source":"mcp"}`),
		Activity:          &toolresult.ActivityRef{ID: "activity-1", Kind: "computer"},
	}

	got, ref, bounded := finalizeGenericToolResult(sessionDir, "call-rich", raw, 1_000)
	if !bounded || ref == "" {
		t.Fatalf("expected bounded rich result, bounded=%v ref=%q", bounded, ref)
	}
	if len(got.Content) != 2 || got.Content[0].Type != toolresult.ContentTypeText || !reflect.DeepEqual(got.Content[1], raw.Content[1]) {
		t.Fatalf("rich content was not preserved: %+v", got.Content)
	}
	if !strings.Contains(got.Content[0].Text, ref) || !strings.Contains(got.Content[0].Text, "head evidence") || !strings.Contains(got.Content[0].Text, "tail evidence") {
		t.Fatalf("bounded preview lost evidence or recovery index: %.300q", got.Content[0].Text)
	}
	if string(got.StructuredContent) != string(raw.StructuredContent) || string(got.Meta) != string(raw.Meta) || got.Activity == nil || got.Activity.ID != raw.Activity.ID {
		t.Fatalf("rich metadata changed: %+v", got)
	}
	if strings.Contains(got.TextProjection(), "private_payload") {
		t.Fatalf("structured metadata was duplicated into model text: %q", got.TextProjection())
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if string(data) != raw.TextProjection() {
		t.Fatal("artifact does not contain the exact original model-visible context")
	}
}

func TestFinalizeGenericToolResultBoundsStructuredOnlyResult(t *testing.T) {
	sessionDir := t.TempDir()
	raw := toolresult.Result{
		StructuredContent: json.RawMessage(`{"payload":"` + strings.Repeat("x", 4_000) + `"}`),
		Meta:              json.RawMessage(`{"private":true}`),
	}

	got, ref, bounded := finalizeGenericToolResult(sessionDir, "call-structured", raw, 1_000)
	if !bounded || ref == "" || len(got.Content) != 1 || got.Content[0].Type != toolresult.ContentTypeText {
		t.Fatalf("structured-only result was not bounded: bounded=%v ref=%q result=%+v", bounded, ref, got)
	}
	if !strings.Contains(got.TextProjection(), ref) || strings.Contains(got.TextProjection(), strings.Repeat("x", 2_000)) {
		t.Fatalf("structured-only provider projection is not bounded: %.300q", got.TextProjection())
	}
	var index map[string]any
	if err := json.Unmarshal([]byte(got.TextProjection()), &index); err != nil {
		t.Fatalf("structured projection must remain valid JSON: %v", err)
	}
	if index["kind"] != "archived_structured_tool_result" || index["shape"] != "object" {
		t.Fatalf("structured projection lacks a meaningful index: %+v", index)
	}
	if string(got.StructuredContent) != string(raw.StructuredContent) || string(got.Meta) != string(raw.Meta) {
		t.Fatal("structured-only metadata was not retained")
	}
}

func TestFinalizeGenericToolResultUsesLineLimit(t *testing.T) {
	raw := toolresult.FromText(strings.Repeat("x\n", defaultResultMaxLines+1))
	got, ref, bounded := finalizeGenericToolResult(t.TempDir(), "call-lines", raw, defaultResultBudget)
	if !bounded || ref == "" || got.TextProjection() == raw.TextProjection() {
		t.Fatal("line-heavy result should cross the generic settlement boundary")
	}
}

func TestFinalizeGenericToolResultFailsOpenWithoutArtifactStorage(t *testing.T) {
	raw := toolresult.Result{
		Content: []toolresult.ContentPart{
			{Type: toolresult.ContentTypeText, Text: strings.Repeat("x", 2_000)},
			{Type: toolresult.ContentTypeImage, Data: "aW1hZ2U=", MIMEType: "image/png"},
		},
	}
	got, ref, bounded := finalizeGenericToolResult("", "call-no-store", raw, 1_000)
	if bounded || ref != "" || got.JSONProjection() != raw.JSONProjection() {
		t.Fatal("settlement without recoverable storage must fail open")
	}
}
