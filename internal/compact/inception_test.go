package compact

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestContextAnchorRoundTrip(t *testing.T) {
	msg := BuildContextAnchorMessage(7)
	if msg.Role != "user" || msg.Name != ContextAnchorMessageName || !msg.Hidden {
		t.Fatalf("unexpected anchor message: %+v", msg)
	}
	if msg.Content != "<system>CHECKPOINT 7</system>" {
		t.Fatalf("unexpected anchor content: %q", msg.Content)
	}
	id, ok := ContextAnchorIDFromMessage(msg)
	if !ok || id != 7 {
		t.Fatalf("ContextAnchorIDFromMessage = %d,%v; want 7,true", id, ok)
	}
	next := NextContextAnchorID([]providers.ChatMessage{
		BuildContextAnchorMessage(2),
		BuildContextAnchorMessage(7),
	})
	if next != 8 {
		t.Fatalf("NextContextAnchorID = %d, want 8", next)
	}
}

func TestContextAnchorParsesLegacyMarker(t *testing.T) {
	msg := providers.ChatMessage{
		Role:    "user",
		Name:    ContextAnchorMessageName,
		Hidden:  true,
		Content: wrapInternalContextContent("[Wuu context checkpoint]\nanchor_id: 9\nlegacy"),
	}
	id, ok := ContextAnchorIDFromMessage(msg)
	if !ok || id != 9 {
		t.Fatalf("ContextAnchorIDFromMessage legacy = %d,%v; want 9,true", id, ok)
	}
}

func TestRewriteHistoryWithInceptionContinuationCutsAfterAnchor(t *testing.T) {
	content := BuildInceptionContinuationContent(0, "## Task state\nContinue from current files.\n\n## Next step\nRun tests.")
	history := []providers.ChatMessage{
		{Role: "system", Content: "base"},
		{Role: "user", Content: "fix it"},
		BuildContextAnchorMessage(0),
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_read", Name: "read_file"}}},
		{Role: "tool", Name: "read_file", ToolCallID: "call_read", Content: "large output"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_inception", Name: InceptionToolName}}},
		{Role: "tool", Name: InceptionToolName, ToolCallID: "call_inception", Content: `{"action":"inception","status":"completed"}`},
	}

	rewritten, changed, err := RewriteHistoryWithInceptionContinuation(history, 0, content)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected rewrite to report changed")
	}
	if len(rewritten) != 4 {
		t.Fatalf("expected base, user, anchor, continuation; got %+v", rewritten)
	}
	if rewritten[0].Content != "base" || rewritten[1].Content != "fix it" {
		t.Fatalf("anchor prefix history not preserved: %+v", rewritten)
	}
	if id, ok := ContextAnchorIDFromMessage(rewritten[2]); !ok || id != 0 {
		t.Fatalf("rewrite must retain the selected anchor before the continuation, got %+v", rewritten[2])
	}
	if next := NextContextAnchorID(rewritten); next != 1 {
		t.Fatalf("rewrite must not make the next injected anchor reuse id 0; got next=%d", next)
	}
	if rewritten[3].Role != "user" || rewritten[3].Name != ContextContinuationName || !rewritten[3].Hidden || !strings.Contains(rewritten[3].Content, InceptionContinuationPrefix) || !strings.Contains(rewritten[3].Content, "replaces the low-value conversation suffix") || !strings.Contains(rewritten[3].Content, "external state remain current") {
		t.Fatalf("missing continuation summary: %+v", rewritten[3])
	}
	if err := providers.ValidateToolCallHistory(rewritten); err != nil {
		t.Fatalf("rewritten history must be provider-valid: %v", err)
	}
}

func TestBuildInceptionContinuationContentMarksSummaryAsAssistantAuthored(t *testing.T) {
	content := BuildInceptionContinuationContent(4, "## Task state\nUser asked for a plan.\n\n## Next step\nWait for approval.")

	for _, want := range []string{
		"assistant-authored internal summary",
		"not a new user instruction",
		"not user authorization",
		"Do not upgrade a proposal, plan, or open question into user approval",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("continuation content missing %q:\n%s", want, content)
		}
	}
}

func TestRewriteHistoryFromInceptionToolMessages(t *testing.T) {
	content := BuildInceptionContinuationContent(1, "## Task state\nState capsule.")
	toolResult := providers.ChatMessage{
		Role:       "tool",
		Name:       InceptionToolName,
		ToolCallID: "call_inception",
		Content:    `{"action":"inception","status":"completed","history_rewrite":{"kind":"inception_context_rewrite","anchor_id":1,"content":` + strconvQuote(content) + `}}`,
	}
	history := []providers.ChatMessage{
		{Role: "system", Content: "base"},
		{Role: "user", Content: "fix it"},
		BuildContextAnchorMessage(0),
		{Role: "assistant", Content: "first phase"},
		BuildContextAnchorMessage(1),
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_inception", Name: InceptionToolName}}},
		toolResult,
	}

	rewritten, ok, err := RewriteHistoryFromInceptionToolMessages(history, []providers.ChatMessage{toolResult})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected rewrite")
	}
	if len(rewritten) != 6 {
		t.Fatalf("expected history through anchor 1 plus continuation, got %+v", rewritten)
	}
	if id, ok := ContextAnchorIDFromMessage(rewritten[2]); !ok || id != 0 {
		t.Fatalf("expected older anchor to remain, got %+v", rewritten[2])
	}
	if id, ok := ContextAnchorIDFromMessage(rewritten[4]); !ok || id != 1 {
		t.Fatalf("expected selected anchor to remain before continuation, got %+v", rewritten[4])
	}
	if strings.Contains(rewritten[len(rewritten)-1].Content, "call_inception") {
		t.Fatalf("continuation must not contain tool-chain details: %+v", rewritten)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
