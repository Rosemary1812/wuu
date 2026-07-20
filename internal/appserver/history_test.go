package appserver

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/providers"
	sessionstore "github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

func TestChatHistoryRoundTripKeepsRichToolResult(t *testing.T) {
	sessDir := t.TempDir()
	sess, err := sessionstore.CreateWithMetadata(sessDir, "rich-tool-result", t.TempDir())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	detail := toolresult.Result{
		Content: []toolresult.ContentPart{
			{Type: toolresult.ContentTypeText, Text: "screenshot captured"},
			{Type: toolresult.ContentTypeImage, Data: "aW1hZ2U=", MIMEType: "image/png", Name: "screen.png"},
		},
		StructuredContent: json.RawMessage(`{"caption":"result"}`),
		Meta:              json.RawMessage(`{"source":"mcp"}`),
	}
	history := []providers.ChatMessage{
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call-rich", Name: "mcp_rich"}}},
		{Role: "tool", Name: "mcp_rich", ToolCallID: "call-rich", Content: detail.TextProjection(), ToolResult: &detail},
	}
	if err := rewriteChatHistory(sessDir, sess.ID, history); err != nil {
		t.Fatalf("rewrite history: %v", err)
	}
	loaded, err := loadChatMessages(sessDir, sess.ID)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(loaded) != 2 || loaded[1].ToolResult == nil || !reflect.DeepEqual(*loaded[1].ToolResult, detail) {
		t.Fatalf("rich tool result did not survive restart: %+v", loaded)
	}
	prepared, err := providers.PrepareMessagesForModelRequest("gpt-5", loaded)
	if err != nil {
		t.Fatalf("prepare resumed history: %v", err)
	}
	if len(prepared) != 3 || len(prepared[2].Images) != 1 || prepared[2].Images[0].Data != "aW1hZ2U=" {
		t.Fatalf("resumed history lost native media observation: %+v", prepared)
	}
}

func TestRewriteChatHistoryKeepsHelpMeJointCompact(t *testing.T) {
	sessDir := t.TempDir()
	sess, err := sessionstore.CreateWithMetadata(sessDir, "helpme-compact", t.TempDir())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	history := []providers.ChatMessage{
		{Role: "system", Content: compact.HelpMeJointCompactPrefix + "\nRecovered task state"},
		{Role: "assistant", Content: "continued"},
	}
	if err := rewriteChatHistory(sessDir, sess.ID, history); err != nil {
		t.Fatalf("rewrite history: %v", err)
	}
	loaded, err := loadChatMessages(sessDir, sess.ID)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected HelpMe compact and assistant to persist, got %+v", loaded)
	}
	if loaded[0].Role != "system" || !compact.IsHelpMeJointCompactContent(loaded[0].Content) || !strings.Contains(loaded[0].Content, "Recovered task state") {
		t.Fatalf("expected persisted HelpMe compact system message, got %+v", loaded[0])
	}
}
