package providers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/toolresult"
)

func TestProjectToolResultPreservesOrderAndHidesPrivateMetadata(t *testing.T) {
	result := toolresult.Result{
		Content: []toolresult.ContentPart{
			{Type: "text", Text: "first"},
			{Type: "image", Data: "aW1hZ2U=", MIMEType: "image/png", Name: "screenshot"},
			{Type: "resource_link", URI: "https://example.test/docs", Name: "docs"},
			{Type: "resource", Resource: json.RawMessage(`{"uri":"file:///tmp/a","text":"body"}`), Name: "embedded"},
			{Type: "audio", Data: "YXVkaW8=", MIMEType: "audio/wav", Name: "clip.wav"},
		},
		StructuredContent: json.RawMessage(`{"b":2,"a":1}`),
		Meta:              json.RawMessage(`{"secret":"must-not-reach-model"}`),
		Activity:          &toolresult.ActivityRef{ID: "activity-1", Kind: "browser"},
	}
	projected := ProjectToolResult(result)
	wantText := "first\n[resource_link: docs (https://example.test/docs)]\n[resource: embedded] {\"text\":\"body\",\"uri\":\"file:///tmp/a\"}\n{\"a\":1,\"b\":2}"
	if projected.ToolText != wantText {
		t.Fatalf("ToolText = %q, want %q", projected.ToolText, wantText)
	}
	if len(projected.ObservationImages) != 1 || projected.ObservationImages[0].MediaType != "image/png" {
		t.Fatalf("images = %+v", projected.ObservationImages)
	}
	if len(projected.ObservationFiles) != 1 || projected.ObservationFiles[0].Filename != "clip.wav" {
		t.Fatalf("files = %+v", projected.ObservationFiles)
	}
	if strings.Contains(projected.ToolText, "must-not-reach-model") || strings.Contains(projected.ToolText, "activity-1") {
		t.Fatalf("private metadata leaked: %q", projected.ToolText)
	}
}

func TestPrepareMessagesPlacesObservationsAfterContiguousToolResults(t *testing.T) {
	imageResult := toolresult.Result{Content: []toolresult.ContentPart{{Type: "image", Data: "aW1hZ2U=", MIMEType: "image/png"}}}
	fileResult := toolresult.Result{Content: []toolresult.ContentPart{{Type: "file", Data: "ZmlsZQ==", MIMEType: "application/pdf", Name: "report.pdf"}}}
	messages := []ChatMessage{
		{Role: "user", Content: "inspect"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call-1", Name: "observe"}, {ID: "call-2", Name: "download"}}},
		{Role: "tool", ToolCallID: "call-1", Name: "observe", ToolResult: &imageResult},
		{Role: "tool", ToolCallID: "call-2", Name: "download", ToolResult: &fileResult},
	}

	prepared, err := PrepareMessagesForModelRequest("gpt-5", messages)
	if err != nil {
		t.Fatalf("PrepareMessagesForModelRequest: %v", err)
	}
	if len(prepared) != 5 || prepared[2].Role != "tool" || prepared[3].Role != "tool" || prepared[4].Role != "user" {
		t.Fatalf("tool results/observation order = %+v", prepared)
	}
	if len(prepared[4].Images) != 1 || len(prepared[4].Files) != 1 || !prepared[4].Hidden {
		t.Fatalf("observation message = %+v", prepared[4])
	}
	if prepared[2].Content == "" || prepared[3].Content == "" {
		t.Fatalf("unsupported attachment notes missing: %+v", prepared[2:4])
	}
	if err := ValidateToolCallHistory(prepared); err != nil {
		t.Fatalf("projected history invalid: %v", err)
	}
}
