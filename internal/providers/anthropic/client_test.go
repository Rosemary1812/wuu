package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestChat_TextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Fatalf("missing api key header")
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hello"}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	resp, err := client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("unexpected tool calls: %+v", resp.ToolCalls)
	}
}

func TestChat_ToolSearchNativeAddsBetaHeaderWhenExplicitlyEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("anthropic-beta"); !strings.Contains(got, toolSearchBetaHeader1P) {
			t.Fatalf("expected tool search beta header, got %q", got)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
		Tools: []providers.ToolDefinition{
			{Name: "tool_search", InputSchema: map[string]any{"type": "object"}},
		},
		ProviderOptions: map[string]any{"anthropicToolSearch": true},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_ToolSearchNativeMergesConfiguredBetaHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		got := r.Header.Get("anthropic-beta")
		if !strings.Contains(got, toolSearchBetaHeader1P) || !strings.Contains(got, "fast-mode-2026-02-01") {
			t.Fatalf("expected merged beta header, got %q", got)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Headers: map[string]string{"anthropic-beta": "fast-mode-2026-02-01"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
		Tools: []providers.ToolDefinition{
			{Name: "tool_search", InputSchema: map[string]any{"type": "object"}},
		},
		ProviderOptions: map[string]any{"anthropicToolSearch": true},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_AnthropicReplaysReasoningBlocksForAssistantToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) != 3 {
			t.Fatalf("expected 3 messages, got %#v", body["messages"])
		}
		assistant, ok := msgs[1].(map[string]any)
		if !ok {
			t.Fatalf("unexpected assistant message: %#v", msgs[1])
		}
		content, ok := assistant["content"].([]any)
		if !ok || len(content) != 2 {
			t.Fatalf("expected thinking + tool_use content, got %#v", assistant["content"])
		}
		thinking, ok := content[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected thinking block: %#v", content[0])
		}
		if thinking["type"] != "thinking" || thinking["thinking"] != "inspect repo before tool use" || thinking["signature"] != "sig_1" {
			t.Fatalf("unexpected thinking block payload: %#v", thinking)
		}
		toolUse, ok := content[1].(map[string]any)
		if !ok {
			t.Fatalf("unexpected tool_use block: %#v", content[1])
		}
		if toolUse["type"] != "tool_use" || toolUse["id"] != "call_1" || toolUse["name"] != "list_files" {
			t.Fatalf("unexpected tool_use payload: %#v", toolUse)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "inspect repo"},
			{
				Role:             "assistant",
				ReasoningContent: "inspect repo before tool use",
				ReasoningBlocks: []providers.ReasoningBlock{
					{Type: "thinking", Thinking: "inspect repo before tool use", Signature: "sig_1"},
				},
				ToolCalls: []providers.ToolCall{
					{ID: "call_1", Name: "list_files", Arguments: `{}`},
				},
			},
			{Role: "tool", ToolCallID: "call_1", Name: "list_files", Content: "[]"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_AnthropicFallsBackToReasoningContentReplay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) != 3 {
			t.Fatalf("expected 3 messages, got %#v", body["messages"])
		}
		assistant, ok := msgs[1].(map[string]any)
		if !ok {
			t.Fatalf("unexpected assistant message: %#v", msgs[1])
		}
		content, ok := assistant["content"].([]any)
		if !ok || len(content) != 2 {
			t.Fatalf("expected synthetic thinking + tool_use content, got %#v", assistant["content"])
		}
		thinking, ok := content[0].(map[string]any)
		if !ok || thinking["type"] != "thinking" || thinking["thinking"] != "inspect repo before tool use" {
			t.Fatalf("unexpected synthetic thinking block payload: %#v", content[0])
		}
		if _, ok := thinking["signature"]; ok {
			t.Fatalf("did not expect synthetic signature on legacy replay: %#v", thinking)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "inspect repo"},
			{
				Role:             "assistant",
				ReasoningContent: "inspect repo before tool use",
				ToolCalls: []providers.ToolCall{
					{ID: "call_1", Name: "list_files", Arguments: `{}`},
				},
			},
			{Role: "tool", ToolCallID: "call_1", Name: "list_files", Content: "[]"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_ParsesReasoningBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
  "content": [
    {"type":"thinking","thinking":"inspect repo before tool use","signature":"sig_1"},
    {"type":"tool_use","id":"call_1","name":"list_files","input":{}}
  ],
  "stop_reason":"tool_use"
}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := client.Chat(context.Background(), providers.ChatRequest{
		Model:    "claude-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "inspect repo"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.ReasoningContent != "inspect repo before tool use" {
		t.Fatalf("unexpected reasoning content: %q", resp.ReasoningContent)
	}
	if len(resp.ReasoningBlocks) != 1 {
		t.Fatalf("expected 1 reasoning block, got %+v", resp.ReasoningBlocks)
	}
	if resp.ReasoningBlocks[0].Signature != "sig_1" {
		t.Fatalf("unexpected reasoning block: %+v", resp.ReasoningBlocks[0])
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_1" {
		t.Fatalf("unexpected tool calls: %+v", resp.ToolCalls)
	}
}

func TestChat_AnthropicAddsCacheControlFromHint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		system, ok := body["system"].([]any)
		if !ok || len(system) != 1 {
			t.Fatalf("expected system blocks, got %#v", body["system"])
		}
		sysBlock, ok := system[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected system block: %#v", system[0])
		}
		cacheCtl, ok := sysBlock["cache_control"].(map[string]any)
		if !ok || cacheCtl["type"] != "ephemeral" {
			t.Fatalf("expected system cache_control, got %#v", sysBlock["cache_control"])
		}
		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) < 2 {
			t.Fatalf("expected messages, got %#v", body["messages"])
		}
		second, ok := msgs[1].(map[string]any)
		if !ok {
			t.Fatalf("unexpected message payload: %#v", msgs[1])
		}
		content, ok := second["content"].([]any)
		if !ok || len(content) == 0 {
			t.Fatalf("unexpected content blocks: %#v", second["content"])
		}
		lastBlock, ok := content[len(content)-1].(map[string]any)
		if !ok {
			t.Fatalf("unexpected content block: %#v", content[len(content)-1])
		}
		cacheCtl, ok = lastBlock["cache_control"].(map[string]any)
		if !ok || cacheCtl["type"] != "ephemeral" {
			t.Fatalf("expected message cache_control, got %#v", lastBlock["cache_control"])
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "stable reply"},
			{Role: "user", Content: "latest"},
		},
		CacheHint: &providers.CacheHint{StableSystem: true, StablePrefixMessages: 2},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
}

func TestBuildAnthropicRequest_SmooshesSystemReminderIntoToolResult(t *testing.T) {
	reminder := wuucontext.FormatSystemReminder(wuucontext.EnvInfo{
		CWD:       "/tmp/project",
		Date:      "2026-04-21",
		GitBranch: "main",
		GitStatus: "clean",
	})

	payload, err := buildAnthropicRequest(providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "check repo"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{
				{ID: "tool_1", Name: "git", Arguments: `{"subcommand":"status"}`},
			}},
			{Role: "tool", ToolCallID: "tool_1", Name: "git", Content: `{"exit_code":0}`},
			{Role: "user", Name: wuucontext.SystemReminderMessageName, Content: reminder},
		},
	}, 1024, false)
	if err != nil {
		t.Fatalf("buildAnthropicRequest: %v", err)
	}

	if len(payload.Messages) != 3 {
		t.Fatalf("expected 3 messages after merge, got %d", len(payload.Messages))
	}
	last := payload.Messages[2]
	if last.Role != "user" {
		t.Fatalf("expected final message to be user, got %q", last.Role)
	}
	if len(last.Content) != 1 {
		t.Fatalf("expected tool_result-only user content, got %+v", last.Content)
	}
	if last.Content[0].Type != "tool_result" {
		t.Fatalf("expected tool_result block, got %+v", last.Content[0])
	}
	content, ok := last.Content[0].Content.(string)
	if !ok {
		t.Fatalf("expected string tool_result content, got %#v", last.Content[0].Content)
	}
	if !strings.Contains(content, `{"exit_code":0}`) {
		t.Fatalf("expected tool output to be preserved, got %q", content)
	}
	if !strings.Contains(content, "<system-reminder>") {
		t.Fatalf("expected system reminder to be folded into tool_result, got %q", content)
	}
}

func TestBuildAnthropicRequest_ReplaysInvalidToolArgumentsWithErrorResult(t *testing.T) {
	payload, err := buildAnthropicRequest(providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "update plan"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{
				{ID: "call_plan", Name: "update_plan", Arguments: `{"plan": `},
			}},
			{Role: "tool", ToolCallID: "call_plan", Name: "update_plan", Content: `{"error":"invalid tool arguments: unexpected EOF","ok":false}`},
		},
	}, 1024, false)
	if err != nil {
		t.Fatalf("buildAnthropicRequest: %v", err)
	}
	if len(payload.Messages) != 3 {
		t.Fatalf("expected user, assistant, tool result messages, got %+v", payload.Messages)
	}
	assistant := payload.Messages[1]
	if assistant.Role != "assistant" || len(assistant.Content) != 1 {
		t.Fatalf("expected assistant tool_use, got %+v", assistant)
	}
	toolUse := assistant.Content[0]
	if toolUse.Type != "tool_use" || toolUse.ID != "call_plan" || toolUse.Name != "update_plan" {
		t.Fatalf("unexpected tool_use block: %+v", toolUse)
	}
	input, ok := toolUse.Input.(map[string]any)
	if !ok || len(input) != 0 {
		t.Fatalf("expected invalid arguments to replay as empty Anthropic input object, got %#v", toolUse.Input)
	}
	result := payload.Messages[2]
	if result.Role != "user" || len(result.Content) != 1 {
		t.Fatalf("expected tool result message, got %+v", result)
	}
	if result.Content[0].Type != "tool_result" || result.Content[0].ToolUseID != "call_plan" {
		t.Fatalf("unexpected tool_result block: %+v", result.Content[0])
	}
	content, ok := result.Content[0].Content.(string)
	if !ok || !strings.Contains(content, "invalid tool arguments") {
		t.Fatalf("expected invalid tool arguments result, got %#v", result.Content[0].Content)
	}
}

func TestBuildAnthropicRequest_CacheBoundaryKeepsSystemReminderAfterToolResult(t *testing.T) {
	reminder := wuucontext.FormatSystemReminder(wuucontext.EnvInfo{
		CWD:       "/tmp/project",
		Date:      "2026-04-21",
		GitBranch: "main",
		GitStatus: "clean",
	})

	payload, err := buildAnthropicRequest(providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "check repo"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{
				{ID: "tool_1", Name: "git", Arguments: `{"subcommand":"status"}`},
			}},
			{Role: "tool", ToolCallID: "tool_1", Name: "git", Content: `{"exit_code":0}`},
			{Role: "user", Name: wuucontext.SystemReminderMessageName, Content: reminder},
		},
		CacheHint: &providers.CacheHint{StablePrefixMessages: 3},
	}, 1024, false)
	if err != nil {
		t.Fatalf("buildAnthropicRequest: %v", err)
	}

	if len(payload.Messages) != 3 {
		t.Fatalf("expected 3 messages after merge, got %d", len(payload.Messages))
	}
	if payload.Messages[1].Content[0].CacheControl != nil {
		t.Fatalf("did not expect cache marker to stop at tool_use, got %+v", payload.Messages[1].Content[0].CacheControl)
	}
	last := payload.Messages[2]
	if len(last.Content) != 2 {
		t.Fatalf("expected tool_result plus volatile reminder, got %+v", last.Content)
	}
	if last.Content[0].Type != "tool_result" {
		t.Fatalf("expected tool_result block, got %+v", last.Content[0])
	}
	if last.Content[0].CacheControl == nil || last.Content[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("expected cache marker on stable tool_result block, got %+v", last.Content[0].CacheControl)
	}
	content, ok := last.Content[0].Content.(string)
	if !ok {
		t.Fatalf("expected string tool_result content, got %#v", last.Content[0].Content)
	}
	if strings.Contains(content, "<system-reminder>") {
		t.Fatalf("did not expect volatile reminder in cached tool_result, got %q", content)
	}
	if last.Content[1].Type != "text" || !strings.Contains(last.Content[1].Text, "<system-reminder>") {
		t.Fatalf("expected volatile system reminder after cache boundary, got %+v", last.Content[1])
	}
	if last.Content[1].CacheControl != nil {
		t.Fatalf("did not expect cache marker on volatile reminder, got %+v", last.Content[1].CacheControl)
	}
}

func TestBuildAnthropicRequest_TurnPrefixCachesLatestUserBeforeDynamicContext(t *testing.T) {
	reminder := wuucontext.FormatSystemReminder(wuucontext.EnvInfo{
		CWD:  "/tmp/project",
		Date: "2026-04-21",
	})

	payload, err := buildAnthropicRequest(providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "check repo"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{
				ID: "tool_1", Name: "read_file", Arguments: `{"path":"README.md"}`,
			}}},
			{Role: "tool", ToolCallID: "tool_1", Name: "read_file", Content: `{"content":"hello"}`},
			{Role: "user", Name: wuucontext.SystemReminderMessageName, Content: reminder, Hidden: true},
		},
		CacheHint: &providers.CacheHint{StableSystem: true, TurnPrefixMessages: 1},
	}, 1024, false)
	if err != nil {
		t.Fatalf("buildAnthropicRequest: %v", err)
	}
	if len(payload.Messages) != 3 {
		t.Fatalf("expected 3 messages after merge, got %d", len(payload.Messages))
	}
	first := payload.Messages[0]
	if first.Role != "user" || len(first.Content) != 1 {
		t.Fatalf("unexpected first message: %+v", first)
	}
	if first.Content[0].CacheControl == nil || first.Content[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("expected turn prefix cache marker on latest user, got %+v", first.Content[0].CacheControl)
	}
	last := payload.Messages[2]
	if len(last.Content) != 1 || last.Content[0].Type != "tool_result" {
		t.Fatalf("expected tool_result carrying volatile reminder, got %+v", last.Content)
	}
	if last.Content[0].CacheControl != nil {
		t.Fatalf("did not expect cache marker on dynamic tool result context, got %+v", last.Content[0].CacheControl)
	}
	content, ok := last.Content[0].Content.(string)
	if !ok || !strings.Contains(content, "<system-reminder>") {
		t.Fatalf("expected volatile reminder in uncached tool_result content, got %#v", last.Content[0].Content)
	}
}

func TestBuildAnthropicRequest_ClampsOverlargeStablePrefixCacheHint(t *testing.T) {
	payload, err := buildAnthropicRequest(providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "stable reply"},
		},
		CacheHint: &providers.CacheHint{StablePrefixMessages: 99},
	}, 1024, false)
	if err != nil {
		t.Fatalf("buildAnthropicRequest: %v", err)
	}

	if len(payload.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(payload.Messages))
	}
	last := payload.Messages[1]
	if len(last.Content) != 1 || last.Content[0].Type != "text" {
		t.Fatalf("expected assistant text block, got %+v", last.Content)
	}
	if last.Content[0].CacheControl == nil || last.Content[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("expected cache marker on clamped stable prefix, got %+v", last.Content[0].CacheControl)
	}
}

func TestBuildAnthropicRequest_ScrubsClaudeToolCallIDs(t *testing.T) {
	payload, err := buildAnthropicRequest(providers.ChatRequest{
		Model: "claude-opus-4.7",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "check repo"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{
				{ID: "tool.1/2", Name: "git", Arguments: `{"subcommand":"status"}`},
			}},
			{Role: "tool", ToolCallID: "tool.1/2", Name: "git", Content: `{"exit_code":0}`},
		},
	}, 1024, false)
	if err != nil {
		t.Fatalf("buildAnthropicRequest: %v", err)
	}

	if got := payload.Messages[1].Content[0].ID; got != "tool_1_2" {
		t.Fatalf("tool_use id = %q", got)
	}
	if got := payload.Messages[2].Content[0].ToolUseID; got != "tool_1_2" {
		t.Fatalf("tool_result id = %q", got)
	}
}

func TestBuildAnthropicRequest_LeavesRegularUserTextOutsideToolResult(t *testing.T) {
	payload, err := buildAnthropicRequest(providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "check repo"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{
				{ID: "tool_1", Name: "git", Arguments: `{"subcommand":"status"}`},
			}},
			{Role: "tool", ToolCallID: "tool_1", Name: "git", Content: `{"exit_code":0}`},
			{Role: "user", Content: "real follow-up"},
		},
	}, 1024, false)
	if err != nil {
		t.Fatalf("buildAnthropicRequest: %v", err)
	}

	if len(payload.Messages) != 3 {
		t.Fatalf("expected 3 messages after merge, got %d", len(payload.Messages))
	}
	last := payload.Messages[2]
	if len(last.Content) != 2 {
		t.Fatalf("expected tool_result + text siblings for real user input, got %+v", last.Content)
	}
	if last.Content[0].Type != "tool_result" || last.Content[1].Type != "text" {
		t.Fatalf("unexpected block order: %+v", last.Content)
	}
	if got := last.Content[1].Text; got != "real follow-up" {
		t.Fatalf("unexpected trailing user text: %q", got)
	}
}

func TestBuildAnthropicRequest_CachesStableToolPrefix(t *testing.T) {
	payload, err := buildAnthropicRequest(providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
		},
		Tools: []providers.ToolDefinition{
			{Name: "read_file", InputSchema: map[string]any{"type": "object"}, CacheStable: true},
			{Name: "tool_search", InputSchema: map[string]any{"type": "object"}, CacheStable: true},
			{Name: "mcp_docs_search", InputSchema: map[string]any{"type": "object"}},
		},
	}, 1024, false)
	if err != nil {
		t.Fatalf("buildAnthropicRequest: %v", err)
	}
	if len(payload.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %+v", payload.Tools)
	}
	if payload.Tools[0].CacheControl != nil {
		t.Fatalf("did not expect cache_control on first stable tool: %+v", payload.Tools[0].CacheControl)
	}
	if payload.Tools[1].CacheControl == nil || payload.Tools[1].CacheControl.Type != "ephemeral" {
		t.Fatalf("expected cache_control on stable tool prefix boundary, got %+v", payload.Tools[1].CacheControl)
	}
	if payload.Tools[2].CacheControl != nil {
		t.Fatalf("did not expect cache_control on dynamic tool: %+v", payload.Tools[2].CacheControl)
	}
}

func TestBuildAnthropicRequest_ToolSearchNativeEnabledUsesToolReferences(t *testing.T) {
	payload, err := buildAnthropicRequestWithSupport(providers.ChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "find docs"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{
				ID:        "search_1",
				Name:      "tool_search",
				Kind:      providers.ToolCallKindToolSearch,
				Arguments: `{"query":"docs"}`,
			}}},
			{
				Role:           "tool",
				Name:           "tool_search",
				ToolCallID:     "search_1",
				ToolResultKind: providers.ToolCallKindToolSearch,
				Content: `{
  "loadable_tools": [
    {
      "type": "function",
      "name": "mcp_docs_search",
      "description": "Search docs through MCP",
      "input_schema": {"type":"object","properties":{"query":{"type":"string"}}},
      "defer_loading": true
    }
  ]
}`,
			},
		},
		Tools: []providers.ToolDefinition{
			{Name: "tool_search", InputSchema: map[string]any{"type": "object"}, CacheStable: true},
			{Name: "mcp_docs_search", InputSchema: map[string]any{"type": "object"}},
		},
	}, 1024, false, anthropicToolSearchSupport{BaseURL: "https://api.anthropic.com"})
	if err != nil {
		t.Fatalf("buildAnthropicRequestWithSupport: %v", err)
	}
	if len(payload.Betas) != 1 || payload.Betas[0] != toolSearchBetaHeader1P {
		t.Fatalf("expected tool search beta, got %+v", payload.Betas)
	}
	if len(payload.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %+v", payload.Tools)
	}
	if payload.Tools[0].DeferLoading {
		t.Fatalf("tool_search itself should not be defer_loading: %+v", payload.Tools[0])
	}
	if !payload.Tools[1].DeferLoading {
		t.Fatalf("discovered tool should be defer_loading: %+v", payload.Tools[1])
	}
	last := payload.Messages[len(payload.Messages)-1]
	if len(last.Content) != 1 || last.Content[0].Type != "tool_result" {
		t.Fatalf("unexpected final message: %+v", last)
	}
	refs, ok := last.Content[0].Content.([]anthropicBlock)
	if !ok || len(refs) != 1 {
		t.Fatalf("expected tool_reference content, got %#v", last.Content[0].Content)
	}
	if refs[0].Type != "tool_reference" || refs[0].ToolName != "mcp_docs_search" {
		t.Fatalf("unexpected tool_reference: %+v", refs[0])
	}
}

func TestBuildAnthropicRequest_RegularToolResultCanDiscoverDeferredTools(t *testing.T) {
	payload, err := buildAnthropicRequestWithSupport(providers.ChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "start a reviewer"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{
				ID:        "spawn_1",
				Name:      "spawn_agent",
				Arguments: `{"description":"Review","prompt":"Review."}`,
			}}},
			{
				Role:       "tool",
				Name:       "spawn_agent",
				ToolCallID: "spawn_1",
				Content:    `{"action":"spawn_agent","agent_id":"agent_1"}`,
				DiscoveredTools: []providers.LoadableToolDefinition{{
					Type:        "function",
					Name:        "await_agents",
					Description: "Wait for subagents",
					InputSchema: map[string]any{"type": "object"},
				}},
			},
		},
		Tools: []providers.ToolDefinition{
			{Name: "tool_search", InputSchema: map[string]any{"type": "object"}, CacheStable: true},
			{Name: "spawn_agent", InputSchema: map[string]any{"type": "object"}, CacheStable: true},
			{Name: "await_agents", InputSchema: map[string]any{"type": "object"}, DeferLoading: true},
		},
	}, 1024, false, anthropicToolSearchSupport{BaseURL: "https://api.anthropic.com"})
	if err != nil {
		t.Fatalf("buildAnthropicRequestWithSupport: %v", err)
	}
	if len(payload.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %+v", payload.Tools)
	}
	if payload.Tools[1].Name != "spawn_agent" || payload.Tools[1].DeferLoading {
		t.Fatalf("spawn_agent should remain directly visible: %+v", payload.Tools[1])
	}
	if payload.Tools[2].Name != "await_agents" || !payload.Tools[2].DeferLoading {
		t.Fatalf("discovered management tool should stay defer_loading: %+v", payload.Tools[2])
	}

	last := payload.Messages[len(payload.Messages)-1]
	if len(last.Content) != 1 || last.Content[0].Type != "tool_result" {
		t.Fatalf("unexpected final message: %+v", last)
	}
	blocks, ok := last.Content[0].Content.([]anthropicBlock)
	if !ok || len(blocks) != 2 {
		t.Fatalf("expected text plus tool_reference content, got %#v", last.Content[0].Content)
	}
	if blocks[0].Type != "text" || blocks[0].Text == "" {
		t.Fatalf("expected original tool result text first, got %+v", blocks[0])
	}
	if blocks[1].Type != "tool_reference" || blocks[1].ToolName != "await_agents" {
		t.Fatalf("unexpected discovered tool_reference: %+v", blocks[1])
	}
}

func TestBuildAnthropicRequest_CompactedDiscoveredToolsRestoreAsVisibleTools(t *testing.T) {
	payload, err := buildAnthropicRequestWithSupport(providers.ChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []providers.ChatMessage{
			{
				Role:    "system",
				Content: "[Conversation summary]\nSummary:\nOlder turns discovered the docs search tool.",
				DiscoveredTools: []providers.LoadableToolDefinition{{
					Type:        "function",
					Name:        "mcp_docs_search",
					Description: "Search docs through MCP",
					InputSchema: map[string]any{"type": "object"},
				}},
			},
			{Role: "user", Content: "continue"},
		},
		Tools: []providers.ToolDefinition{
			{Name: "tool_search", InputSchema: map[string]any{"type": "object"}, CacheStable: true},
			{Name: "mcp_docs_search", InputSchema: map[string]any{"type": "object"}, DeferLoading: true},
		},
	}, 1024, false, anthropicToolSearchSupport{BaseURL: "https://api.anthropic.com"})
	if err != nil {
		t.Fatalf("buildAnthropicRequestWithSupport: %v", err)
	}
	if len(payload.Betas) != 1 || payload.Betas[0] != toolSearchBetaHeader1P {
		t.Fatalf("expected tool search beta, got %+v", payload.Betas)
	}
	if len(payload.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %+v", payload.Tools)
	}
	if payload.Tools[1].Name != "mcp_docs_search" || payload.Tools[1].DeferLoading {
		t.Fatalf("compacted discovered tool should be restored as visible schema, got %+v", payload.Tools[1])
	}
}

func TestBuildAnthropicRequest_ToolSearchDisabledForProxyByDefault(t *testing.T) {
	payload, err := buildAnthropicRequestWithSupport(providers.ChatRequest{
		Model: "claude-sonnet-4-5",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "find docs"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{
				ID:        "search_1",
				Name:      "tool_search",
				Kind:      providers.ToolCallKindToolSearch,
				Arguments: `{"query":"docs"}`,
			}}},
			{
				Role:           "tool",
				Name:           "tool_search",
				ToolCallID:     "search_1",
				ToolResultKind: providers.ToolCallKindToolSearch,
				Content:        `{"loadable_tools":[{"type":"function","name":"mcp_docs_search","input_schema":{"type":"object"}}]}`,
			},
		},
		Tools: []providers.ToolDefinition{
			{Name: "tool_search", InputSchema: map[string]any{"type": "object"}},
			{Name: "mcp_docs_search", InputSchema: map[string]any{"type": "object"}},
		},
	}, 1024, false, anthropicToolSearchSupport{BaseURL: "https://anthropic-proxy.example.com"})
	if err != nil {
		t.Fatalf("buildAnthropicRequestWithSupport: %v", err)
	}
	if len(payload.Betas) != 0 {
		t.Fatalf("proxy default should not enable tool search beta, got %+v", payload.Betas)
	}
	if payload.Tools[1].DeferLoading {
		t.Fatalf("proxy default should not send defer_loading: %+v", payload.Tools[1])
	}
	last := payload.Messages[len(payload.Messages)-1]
	if _, ok := last.Content[0].Content.(string); !ok {
		t.Fatalf("proxy default should keep string tool_result content, got %#v", last.Content[0].Content)
	}
}

func TestBuildAnthropicRequest_ToolSearchDisabledForHaiku(t *testing.T) {
	payload, err := buildAnthropicRequestWithSupport(providers.ChatRequest{
		Model: "claude-3-5-haiku-latest",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "find docs"},
		},
		Tools: []providers.ToolDefinition{
			{Name: "tool_search", InputSchema: map[string]any{"type": "object"}},
			{Name: "mcp_docs_search", InputSchema: map[string]any{"type": "object"}, DeferLoading: true},
		},
	}, 1024, false, anthropicToolSearchSupport{BaseURL: "https://api.anthropic.com"})
	if err != nil {
		t.Fatalf("buildAnthropicRequestWithSupport: %v", err)
	}
	if len(payload.Betas) != 0 {
		t.Fatalf("haiku should not enable tool search beta, got %+v", payload.Betas)
	}
	if payload.Tools[1].DeferLoading {
		t.Fatalf("haiku should not send defer_loading: %+v", payload.Tools[1])
	}
}

func TestBuildAnthropicRequest_SplitsStableSystemBlocks(t *testing.T) {
	payload, err := buildAnthropicRequest(providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "system", Content: "base prompt"},
			{Role: "system", Content: "conversation summary"},
			{Role: "user", Content: "hello"},
		},
		CacheHint: &providers.CacheHint{StableSystem: true},
	}, 1024, false)
	if err != nil {
		t.Fatalf("buildAnthropicRequest: %v", err)
	}
	blocks, ok := payload.System.([]anthropicSystemBlock)
	if !ok {
		t.Fatalf("expected system blocks, got %#v", payload.System)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected two system blocks, got %+v", blocks)
	}
	if blocks[0].Text != "base prompt" || blocks[0].CacheControl != nil {
		t.Fatalf("unexpected first system block: %+v", blocks[0])
	}
	if blocks[1].Text != "conversation summary" || blocks[1].CacheControl == nil || blocks[1].CacheControl.Type != "ephemeral" {
		t.Fatalf("unexpected cached system boundary: %+v", blocks[1])
	}
}

func TestBuildAnthropicRequest_SendsProviderOptions(t *testing.T) {
	payload, err := buildAnthropicRequest(providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
		},
		ProviderOptions: map[string]any{
			"effort":      "high",
			"speed":       "fast",
			"temperature": 1.0,
			"topP":        0.95,
			"topK":        40,
			"thinking": map[string]any{
				"type":         "enabled",
				"budgetTokens": 4096,
				"display":      "none",
			},
		},
	}, 1024, false)
	if err != nil {
		t.Fatalf("buildAnthropicRequest: %v", err)
	}
	if payload.OutputConfig == nil || payload.OutputConfig.Effort != "high" {
		t.Fatalf("unexpected output_config: %+v", payload.OutputConfig)
	}
	if payload.Thinking == nil {
		t.Fatal("expected thinking payload")
	}
	if payload.Thinking.Type != "enabled" || payload.Thinking.BudgetTokens != 4096 || payload.Thinking.Display != "none" {
		t.Fatalf("unexpected thinking payload: %+v", payload.Thinking)
	}
	if payload.Speed != "fast" {
		t.Fatalf("unexpected speed: %q", payload.Speed)
	}
	if payload.Temperature == nil || *payload.Temperature != 1.0 {
		t.Fatalf("unexpected temperature: %+v", payload.Temperature)
	}
	if payload.TopP == nil || *payload.TopP != 0.95 {
		t.Fatalf("unexpected top_p: %+v", payload.TopP)
	}
	if payload.TopK == nil || *payload.TopK != 40 {
		t.Fatalf("unexpected top_k: %+v", payload.TopK)
	}
}

func TestBuildAnthropicRequest_DoesNotOverrideExplicitTemperatureWithProviderOption(t *testing.T) {
	payload, err := buildAnthropicRequest(providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
		},
		Temperature: 0.2,
		ProviderOptions: map[string]any{
			"temperature": 1.0,
		},
	}, 1024, false)
	if err != nil {
		t.Fatalf("buildAnthropicRequest: %v", err)
	}
	if payload.Temperature == nil || *payload.Temperature != 0.2 {
		t.Fatalf("unexpected temperature: %+v", payload.Temperature)
	}
}

func TestChat_AnthropicPrefersCompactSummaryAsCacheAnchor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) != 3 {
			t.Fatalf("expected three non-system messages, got %#v", body["messages"])
		}
		first, ok := msgs[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected first message: %#v", msgs[0])
		}
		firstContent, ok := first["content"].([]any)
		if !ok || len(firstContent) != 1 {
			t.Fatalf("unexpected first content payload: %#v", first["content"])
		}
		firstBlock, ok := firstContent[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected first block: %#v", firstContent[0])
		}
		if firstBlock["text"] != "stable summary payload" {
			t.Fatalf("unexpected summary payload: %#v", firstBlock)
		}
		cacheControl, ok := firstBlock["cache_control"].(map[string]any)
		if !ok || cacheControl["type"] != "ephemeral" {
			t.Fatalf("expected cache_control on compact summary anchor, got %#v", firstBlock["cache_control"])
		}

		second, ok := msgs[1].(map[string]any)
		if !ok {
			t.Fatalf("unexpected second message: %#v", msgs[1])
		}
		secondContent, ok := second["content"].([]any)
		if !ok || len(secondContent) != 1 {
			t.Fatalf("unexpected second content payload: %#v", second["content"])
		}
		secondBlock, ok := secondContent[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected second block: %#v", secondContent[0])
		}
		if _, exists := secondBlock["cache_control"]; exists {
			t.Fatalf("did not expect cache_control on latest stable message when compact summary is present: %#v", secondBlock)
		}

		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "system", Content: "[Conversation summary]\nrewritten history"},
			{Role: "user", Content: "stable summary payload"},
			{Role: "assistant", Content: "older stable answer"},
			{Role: "user", Content: "latest ask"},
		},
		CacheHint: &providers.CacheHint{
			StableSystem:         true,
			StablePrefixMessages: 2,
			HasCompactSummary:    true,
		},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
}

func TestChat_AnthropicOmitsCacheControlWithoutHint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if _, ok := body["system"].([]any); ok {
			t.Fatalf("did not expect structured system blocks: %#v", body["system"])
		}
		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) == 0 {
			t.Fatalf("expected messages, got %#v", body["messages"])
		}
		first, ok := msgs[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected message payload: %#v", msgs[0])
		}
		content, ok := first["content"].([]any)
		if !ok || len(content) == 0 {
			t.Fatalf("unexpected content blocks: %#v", first["content"])
		}
		block, ok := content[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected content block: %#v", content[0])
		}
		if _, ok := block["cache_control"]; ok {
			t.Fatalf("did not expect cache_control: %#v", block["cache_control"])
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
}

func TestChat_AddsCacheControlToStableAnthropicPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		system, ok := body["system"].([]any)
		if !ok || len(system) != 1 {
			t.Fatalf("unexpected system payload: %#v", body["system"])
		}
		sysBlock, ok := system[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected system block: %#v", system[0])
		}
		if sysBlock["text"] != "sys" {
			t.Fatalf("unexpected system text: %#v", sysBlock["text"])
		}
		cacheControl, ok := sysBlock["cache_control"].(map[string]any)
		if !ok || cacheControl["type"] != "ephemeral" {
			t.Fatalf("unexpected system cache_control: %#v", sysBlock["cache_control"])
		}

		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) != 3 {
			t.Fatalf("unexpected messages payload: %#v", body["messages"])
		}

		// First message (user "stable context") — no cache_control.
		firstMsg, ok := msgs[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected first message: %#v", msgs[0])
		}
		content, ok := firstMsg["content"].([]any)
		if !ok || len(content) != 1 {
			t.Fatalf("unexpected first content payload: %#v", firstMsg["content"])
		}
		textBlock, ok := content[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected first text block: %#v", content[0])
		}
		if _, exists := textBlock["cache_control"]; exists {
			t.Fatalf("did not expect cache_control on first stable message: %#v", textBlock)
		}

		// Second message (assistant "stable reply") — cache_control on stable prefix boundary.
		secondMsg, ok := msgs[1].(map[string]any)
		if !ok {
			t.Fatalf("unexpected second message: %#v", msgs[1])
		}
		content, ok = secondMsg["content"].([]any)
		if !ok || len(content) != 1 {
			t.Fatalf("unexpected second content payload: %#v", secondMsg["content"])
		}
		textBlock, ok = content[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected second text block: %#v", content[0])
		}
		cacheControl, ok = textBlock["cache_control"].(map[string]any)
		if !ok || cacheControl["type"] != "ephemeral" {
			t.Fatalf("unexpected message cache_control: %#v", textBlock["cache_control"])
		}

		// Third message (user "volatile ask") — no cache_control.
		thirdMsg, ok := msgs[2].(map[string]any)
		if !ok {
			t.Fatalf("unexpected third message: %#v", msgs[2])
		}
		content, ok = thirdMsg["content"].([]any)
		if !ok || len(content) != 1 {
			t.Fatalf("unexpected third content payload: %#v", thirdMsg["content"])
		}
		textBlock, ok = content[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected third text block: %#v", content[0])
		}
		if _, exists := textBlock["cache_control"]; exists {
			t.Fatalf("did not expect cache_control on volatile message: %#v", textBlock)
		}

		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "stable context"},
			{Role: "assistant", Content: "stable reply"},
			{Role: "user", Content: "volatile ask"},
		},
		CacheHint: &providers.CacheHint{
			StableSystem:         true,
			StablePrefixMessages: 2,
		},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
}

func TestChat_OmitsCacheControlWithoutHint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if system, exists := body["system"]; exists {
			t.Fatalf("did not expect structured system payload: %#v", system)
		}
		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) != 1 {
			t.Fatalf("unexpected messages payload: %#v", body["messages"])
		}
		msg, ok := msgs[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected message: %#v", msgs[0])
		}
		content, ok := msg["content"].([]any)
		if !ok || len(content) != 1 {
			t.Fatalf("unexpected content payload: %#v", msg["content"])
		}
		textBlock, ok := content[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected text block: %#v", content[0])
		}
		if _, exists := textBlock["cache_control"]; exists {
			t.Fatalf("did not expect cache_control without hint: %#v", textBlock)
		}

		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
}

func TestStreamIdleTimeout_DefaultMatchesCodex(t *testing.T) {
	t.Setenv("WUU_STREAM_IDLE_TIMEOUT_MS", "")
	if got := streamIdleTimeout(); got != 300*time.Second {
		t.Fatalf("expected 300s default stream idle timeout, got %s", got)
	}
}

func TestChat_ToolUseResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"tool_use","id":"call-1","name":"read_file","input":{"path":"README.md"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	resp, err := client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "read readme"},
		},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	call := resp.ToolCalls[0]
	if call.ID != "call-1" || call.Name != "read_file" {
		t.Fatalf("unexpected tool call: %+v", call)
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		t.Fatalf("parse arguments: %v", err)
	}
	if args["path"] != "README.md" {
		t.Fatalf("unexpected arguments: %+v", args)
	}
}

func TestChat_SendsImageBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) != 1 {
			t.Fatalf("unexpected messages payload: %#v", body["messages"])
		}

		msg, ok := msgs[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected message type: %#v", msgs[0])
		}
		content, ok := msg["content"].([]any)
		if !ok || len(content) != 2 {
			t.Fatalf("unexpected content payload: %#v", msg["content"])
		}

		textBlock, ok := content[0].(map[string]any)
		if !ok || textBlock["type"] != "text" || textBlock["text"] != "describe this" {
			t.Fatalf("unexpected text block: %#v", content[0])
		}

		imageBlock, ok := content[1].(map[string]any)
		if !ok || imageBlock["type"] != "image" {
			t.Fatalf("unexpected image block: %#v", content[1])
		}
		source, ok := imageBlock["source"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected source payload: %#v", imageBlock["source"])
		}
		if source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != "AAA" {
			t.Fatalf("unexpected image source: %#v", source)
		}

		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{
				Role:    "user",
				Content: "describe this",
				Images: []providers.InputImage{
					{MediaType: "image/png", Data: "AAA"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
}

func TestChat_SendsDocumentBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) != 1 {
			t.Fatalf("unexpected messages payload: %#v", body["messages"])
		}

		msg, ok := msgs[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected message type: %#v", msgs[0])
		}
		content, ok := msg["content"].([]any)
		if !ok || len(content) != 2 {
			t.Fatalf("unexpected content payload: %#v", msg["content"])
		}

		documentBlock, ok := content[1].(map[string]any)
		if !ok || documentBlock["type"] != "document" {
			t.Fatalf("unexpected document block: %#v", content[1])
		}
		source, ok := documentBlock["source"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected source payload: %#v", documentBlock["source"])
		}
		if source["type"] != "base64" || source["media_type"] != "application/pdf" || source["data"] != "JVBERi0xLjQ=" {
			t.Fatalf("unexpected document source: %#v", source)
		}

		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{
				Role:    "user",
				Content: "read this",
				Files: []providers.InputFile{
					{MediaType: "application/pdf", Data: "JVBERi0xLjQ=", Filename: "brief.pdf"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
}

func TestChat_AppliesCacheControlToStablePrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			System []struct {
				Type         string `json:"type"`
				Text         string `json:"text"`
				CacheControl *struct {
					Type string `json:"type"`
				} `json:"cache_control,omitempty"`
			} `json:"system"`
			Messages []struct {
				Role    string `json:"role"`
				Content []struct {
					Type         string `json:"type"`
					Text         string `json:"text,omitempty"`
					CacheControl *struct {
						Type string `json:"type"`
					} `json:"cache_control,omitempty"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if len(body.System) != 1 {
			t.Fatalf("expected one system block, got %#v", body.System)
		}
		if body.System[0].Text != "sys" {
			t.Fatalf("unexpected system text: %q", body.System[0].Text)
		}
		if body.System[0].CacheControl == nil || body.System[0].CacheControl.Type != "ephemeral" {
			t.Fatalf("expected cache_control on system block, got %#v", body.System[0].CacheControl)
		}
		if len(body.Messages) != 2 {
			t.Fatalf("expected two non-system messages, got %d", len(body.Messages))
		}
		if body.Messages[0].Role != "user" {
			t.Fatalf("unexpected first role: %q", body.Messages[0].Role)
		}
		lastBlock := body.Messages[0].Content[len(body.Messages[0].Content)-1]
		if lastBlock.CacheControl == nil || lastBlock.CacheControl.Type != "ephemeral" {
			t.Fatalf("expected cache_control on stable prefix boundary, got %#v", lastBlock.CacheControl)
		}
		if len(body.Messages[1].Content) == 0 {
			t.Fatal("expected follow-up content")
		}
		followUpLast := body.Messages[1].Content[len(body.Messages[1].Content)-1]
		if followUpLast.CacheControl != nil {
			t.Fatalf("did not expect cache_control on volatile message, got %#v", followUpLast.CacheControl)
		}

		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "second"},
		},
		CacheHint: &providers.CacheHint{
			StableSystem:         true,
			StablePrefixMessages: 1,
		},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
}

func TestChat_RetriesTransientServerError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	rc := providers.RetryConfig{
		MaxRetries:   2,
		InitialDelay: time.Millisecond,
		MaxDelay:     2 * time.Millisecond,
	}
	client, err := New(ClientConfig{
		BaseURL:     server.URL,
		APIKey:      "test-key",
		RetryConfig: &rc,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	resp, err := client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("chat error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestChat_DoesNotRetryAuthError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	rc := providers.RetryConfig{
		MaxRetries:   2,
		InitialDelay: time.Millisecond,
		MaxDelay:     2 * time.Millisecond,
	}
	client, err := New(ClientConfig{
		BaseURL:     server.URL,
		APIKey:      "test-key",
		RetryConfig: &rc,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hi"},
		},
	})
	if err == nil {
		t.Fatal("expected auth error")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected 1 attempt for auth failure, got %d", got)
	}
}

func TestStreamChat_SSE(t *testing.T) {
	ssePayload := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ssePayload))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:    "claude-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var events []providers.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Verify content deltas arrive in order.
	var contentParts []string
	var usageEvents []providers.StreamEvent
	for _, ev := range events {
		if ev.Type == providers.EventContentDelta {
			contentParts = append(contentParts, ev.Content)
		}
		if ev.Type == providers.EventUsage {
			usageEvents = append(usageEvents, ev)
		}
	}
	if len(contentParts) != 2 || contentParts[0] != "Hello" || contentParts[1] != " world" {
		t.Fatalf("unexpected content deltas: %v", contentParts)
	}
	if len(usageEvents) != 1 || usageEvents[0].Usage == nil || usageEvents[0].Usage.OutputTokens != 5 {
		t.Fatalf("unexpected usage events: %+v", usageEvents)
	}

	// Verify EventDone is the last event.
	last := events[len(events)-1]
	if last.Type != providers.EventDone {
		t.Fatalf("expected last event to be EventDone, got %s", last.Type)
	}

	// Verify usage in done event.
	if last.Usage == nil {
		t.Fatal("expected usage in done event")
	}
	if last.Usage.InputTokens != 10 {
		t.Fatalf("expected 10 input tokens, got %d", last.Usage.InputTokens)
	}
	if last.Usage.OutputTokens != 5 {
		t.Fatalf("expected 5 output tokens, got %d", last.Usage.OutputTokens)
	}
}

func TestStreamChat_ToolUse(t *testing.T) {
	ssePayload := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":15}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"tu_1\",\"name\":\"read_file\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"\\\"test.go\\\"}\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":8}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ssePayload))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:    "claude-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "read file"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var events []providers.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	// Verify tool use start.
	var toolStarts, toolEnds int
	var endToolCall *providers.ToolCall
	for _, ev := range events {
		switch ev.Type {
		case providers.EventToolUseStart:
			toolStarts++
			if ev.ToolCall == nil || ev.ToolCall.Name != "read_file" || ev.ToolCall.ID != "tu_1" {
				t.Fatalf("unexpected tool start: %+v", ev.ToolCall)
			}
		case providers.EventToolUseEnd:
			toolEnds++
			endToolCall = ev.ToolCall
		}
	}
	if toolStarts != 1 {
		t.Fatalf("expected 1 tool start, got %d", toolStarts)
	}
	if toolEnds != 1 {
		t.Fatalf("expected 1 tool end, got %d", toolEnds)
	}
	if endToolCall == nil || endToolCall.ID != "tu_1" {
		t.Fatalf("unexpected tool end: %+v", endToolCall)
	}
	if endToolCall.Arguments != `{"path":"test.go"}` {
		t.Fatalf("unexpected tool arguments: %q", endToolCall.Arguments)
	}

	// Verify done is last.
	last := events[len(events)-1]
	if last.Type != providers.EventDone {
		t.Fatalf("expected EventDone last, got %s", last.Type)
	}
}

func TestStreamChat_ThinkingDoneIncludesReasoningBlock(t *testing.T) {
	ssePayload := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":15}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"inspect repo\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig_1\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":8}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ssePayload))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:    "claude-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var events []providers.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 4 {
		t.Fatalf("expected thinking delta + thinking done + usage + done, got %d events", len(events))
	}
	if events[0].Type != providers.EventThinkingDelta || events[0].Content != "inspect repo" {
		t.Fatalf("unexpected first event: %+v", events[0])
	}
	if events[1].Type != providers.EventThinkingDone || events[1].ReasoningBlock == nil {
		t.Fatalf("expected thinking_done with reasoning block, got %+v", events[1])
	}
	if events[1].ReasoningBlock.Signature != "sig_1" || events[1].ReasoningBlock.Thinking != "inspect repo" {
		t.Fatalf("unexpected reasoning block: %+v", events[1].ReasoningBlock)
	}
	if events[2].Type != providers.EventUsage || events[2].Usage == nil || events[2].Usage.OutputTokens != 8 {
		t.Fatalf("expected usage event with output tokens, got %+v", events[2])
	}
	if events[3].Type != providers.EventDone {
		t.Fatalf("expected done event, got %+v", events[3])
	}
}

func TestStreamChat_ErrorEventSurfacesProviderError(t *testing.T) {
	ssePayload := "event: error\n" +
		"data: {\"error\":{\"code\":\"1305\",\"message\":\"该模型当前访问量过大，请您稍后再试\"}}\n\n" +
		"data: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ssePayload))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:    "claude-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var events []providers.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 stream event, got %d", len(events))
	}
	if events[0].Type != providers.EventError {
		t.Fatalf("expected error event, got %+v", events[0])
	}
	if events[0].Error == nil || !providers.IsRetryable(events[0].Error) {
		t.Fatalf("expected retryable provider stream error, got %v", events[0].Error)
	}
}

func TestStreamChat_MissingMessageStopYieldsIncompleteError(t *testing.T) {
	ssePayload := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ssePayload))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:    "claude-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var events []providers.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	if len(events) != 2 {
		t.Fatalf("expected content delta + terminal error, got %d events", len(events))
	}
	if events[0].Type != providers.EventContentDelta || events[0].Content != "Hello" {
		t.Fatalf("unexpected first event: %+v", events[0])
	}
	if events[1].Type != providers.EventError {
		t.Fatalf("expected terminal error, got %+v", events[1])
	}
	if events[1].Error == nil || !providers.IsRetryable(events[1].Error) {
		t.Fatalf("expected retryable incomplete stream error, got %v", events[1].Error)
	}
}

func TestStreamChat_MessageDeltaCanBackfillInputTokens(t *testing.T) {
	ssePayload := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"测试\"}}\n\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"cache_read_input_tokens\":0}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ssePayload))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:    "claude-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var events []providers.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	last := events[len(events)-1]
	if last.Type != providers.EventDone {
		t.Fatalf("expected EventDone last, got %s", last.Type)
	}
	if last.Usage == nil {
		t.Fatal("expected usage in done event")
	}
	if last.Usage.InputTokens != 10 {
		t.Fatalf("expected backfilled input tokens 10, got %d", last.Usage.InputTokens)
	}
	if last.Usage.OutputTokens != 2 {
		t.Fatalf("expected output tokens 2, got %d", last.Usage.OutputTokens)
	}
}

func TestStreamChat_ValidationErrors(t *testing.T) {
	client, _ := New(ClientConfig{BaseURL: "http://localhost", APIKey: "k"})

	_, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model: "", Messages: []providers.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty model")
	}

	_, err = client.StreamChat(context.Background(), providers.ChatRequest{
		Model: "m", Messages: nil,
	})
	if err == nil {
		t.Fatal("expected error for empty messages")
	}
}

func TestStreamChat_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client, _ := New(ClientConfig{BaseURL: server.URL, APIKey: "k"})
	_, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model: "m", Messages: []providers.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	var httpErr *providers.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T (%v)", err, err)
	}
	if httpErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: %d", httpErr.StatusCode)
	}
}

func TestStreamChat_RejectsInvalidMessageSequenceBeforeRequest(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		t.Fatal("request should not reach server for invalid local history")
	}))
	defer server.Close()

	client, _ := New(ClientConfig{BaseURL: server.URL, APIKey: "k"})
	_, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model: "m",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
			{Role: "system", Content: "late system"},
		},
	})
	if err == nil {
		t.Fatal("expected invalid sequence error")
	}
	if hits.Load() != 0 {
		t.Fatalf("expected zero requests, got %d", hits.Load())
	}
	if !strings.Contains(err.Error(), "invalid message sequence after tool-call history repair") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsCacheCreationOmittingEndpoint(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{"minimax production endpoint", "https://api.minimaxi.com/anthropic", true},
		{"minimax with trailing slash", "https://api.minimaxi.com/anthropic/", true},
		{"minimax case insensitive", "https://API.MINIMAXI.COM/anthropic", true},
		{"anthropic native", "https://api.anthropic.com", false},
		{"anthropic staging", "https://api-staging.anthropic.com", false},
		{"empty", "", false},
		{"localhost", "http://localhost:8080", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCacheCreationOmittingEndpoint(tc.baseURL); got != tc.want {
				t.Errorf("isCacheCreationOmittingEndpoint(%q) = %v, want %v", tc.baseURL, got, tc.want)
			}
		})
	}
}

func TestStampCacheCreationFlag(t *testing.T) {
	t.Run("minimax endpoint forces unknown", func(t *testing.T) {
		client := &Client{baseURL: "https://api.minimaxi.com/anthropic"}
		usage := &providers.TokenUsage{CacheCreationTokens: 12345}
		client.stampCacheCreationFlag(usage)
		if !usage.CacheCreationUnknown {
			t.Errorf("expected CacheCreationUnknown=true for minimax endpoint, got false (CacheCreationTokens=%d)", usage.CacheCreationTokens)
		}
	})
	t.Run("minimax forces unknown even when field present in payload", func(t *testing.T) {
		client := &Client{baseURL: "https://api.minimaxi.com/anthropic"}
		usage := &providers.TokenUsage{CacheCreationTokens: 0}
		usage.CacheCreationUnknown = false
		client.stampCacheCreationFlag(usage)
		if !usage.CacheCreationUnknown {
			t.Error("expected stamp to override false to true for minimax endpoint")
		}
	})
	t.Run("anthropic native leaves flag at default", func(t *testing.T) {
		client := &Client{baseURL: "https://api.anthropic.com"}
		usage := &providers.TokenUsage{CacheCreationTokens: 12345}
		client.stampCacheCreationFlag(usage)
		if usage.CacheCreationUnknown {
			t.Error("expected CacheCreationUnknown=false for native anthropic endpoint")
		}
	})
	t.Run("nil usage does not panic", func(t *testing.T) {
		client := &Client{baseURL: "https://api.minimaxi.com/anthropic"}
		client.stampCacheCreationFlag(nil)
	})
	t.Run("repeated stamps are idempotent", func(t *testing.T) {
		client := &Client{baseURL: "https://api.minimaxi.com/anthropic"}
		usage := &providers.TokenUsage{}
		client.stampCacheCreationFlag(usage)
		client.stampCacheCreationFlag(usage)
		client.stampCacheCreationFlag(usage)
		if !usage.CacheCreationUnknown {
			t.Error("expected flag to remain true after repeated stamps")
		}
	})
}

func TestLastCacheableBlockIndex(t *testing.T) {
	t.Run("empty blocks returns -1", func(t *testing.T) {
		if got := lastCacheableBlockIndex(nil); got != -1 {
			t.Errorf("lastCacheableBlockIndex(nil) = %d, want -1", got)
		}
		if got := lastCacheableBlockIndex([]anthropicBlock{}); got != -1 {
			t.Errorf("lastCacheableBlockIndex([]) = %d, want -1", got)
		}
	})
	t.Run("all cacheable returns last index", func(t *testing.T) {
		blocks := []anthropicBlock{
			{Type: "text", Text: "a"},
			{Type: "text", Text: "b"},
			{Type: "text", Text: "c"},
		}
		if got := lastCacheableBlockIndex(blocks); got != 2 {
			t.Errorf("got %d, want 2", got)
		}
	})
	t.Run("text after image returns text index", func(t *testing.T) {
		blocks := []anthropicBlock{
			{Type: "image", Source: &anthropicImageSource{Type: "base64", MediaType: "image/png", Data: "..."}},
			{Type: "text", Text: "tail"},
		}
		if got := lastCacheableBlockIndex(blocks); got != 1 {
			t.Errorf("got %d, want 1", got)
		}
	})
	t.Run("all uncacheable returns -1", func(t *testing.T) {
		blocks := []anthropicBlock{
			{Type: "image"},
			{Type: "audio"},
			{Type: "thinking"},
		}
		if got := lastCacheableBlockIndex(blocks); got != -1 {
			t.Errorf("got %d, want -1", got)
		}
	})
	t.Run("mixed with tool_use picks tool_use index", func(t *testing.T) {
		blocks := []anthropicBlock{
			{Type: "image"},
			{Type: "tool_use", ID: "x", Name: "Read"},
		}
		if got := lastCacheableBlockIndex(blocks); got != 1 {
			t.Errorf("got %d, want 1", got)
		}
	})
}

func TestMarkAnthropicBoundaryForCache_UsesPreComputedSourceIndex(t *testing.T) {
	// Source scenario:
	//   s0: user "hi"                        (merged into messages[0])
	//   s1: assistant tool_call              (messages[1])
	//   s2: user tool_result                 (merged into messages[2])
	//   s3: user mailbox_text                (merged into messages[2])
	//   s4: user [text_a, image_b]           (merged into messages[2])
	//
	// After merge, messages[2].Content holds all of s2+s3+s4 blocks. The
	// boundary is at s4 (nonSystemSeen hits StablePrefixMessages=5).
	// SourceLastCacheablePayloadIdx is pre-computed to point at s4's
	// text_a (the last cacheable block within s4). Without the fix, the
	// walk-backward would land on s3's mailbox_text, leaving s4's image_b
	// permanently uncached.
	messages := []anthropicMessage{
		{Role: "user", Content: []anthropicBlock{{Type: "text", Text: "hi"}}},
		{Role: "assistant", Content: []anthropicBlock{{Type: "tool_use", ID: "call-1", Name: "Read"}}},
		{Role: "user", Content: []anthropicBlock{
			{Type: "tool_result", ToolUseID: "call-1", Content: "result-1"},
			{Type: "text", Text: "mailbox"},
			{Type: "text", Text: "text_a"},
			{Type: "image", Source: &anthropicImageSource{Type: "base64", MediaType: "image/png", Data: "x"}},
		}},
	}
	// s4 occupies indices [2, 4) in messages[2].Content (text_a at 2,
	// image at 3). Last cacheable in s4 is text_a at payload index 2.
	boundary := anthropicCacheBoundary{
		MessageIndex:                  2,
		BlockEnd:                      4,
		SourceLastCacheablePayloadIdx: 2,
	}
	if !markAnthropicBoundaryForCache(messages, boundary) {
		t.Fatal("expected markAnthropicBoundaryForCache to return true")
	}
	marked := messages[2].Content[2]
	if marked.CacheControl == nil || marked.CacheControl.Type != "ephemeral" {
		t.Errorf("expected CacheControl=ephemeral on s4 text_a, got %+v", marked.CacheControl)
	}
	// mailbox_text (s3) must NOT be marked.
	if messages[2].Content[1].CacheControl != nil {
		t.Errorf("expected s3 mailbox_text to be untouched, got %+v", messages[2].Content[1].CacheControl)
	}
}

func TestMarkAnthropicBoundaryForCache_LegacyFallbackWhenSourceEmpty(t *testing.T) {
	// Boundary source message has no cacheable blocks. The pre-computed
	// index is -1, so we fall back to the legacy walk-backward which
	// marks the nearest preceding cacheable block (mailbox_text in this
	// case, since s4 contributes only an image).
	messages := []anthropicMessage{
		{Role: "user", Content: []anthropicBlock{{Type: "text", Text: "hi"}}},
		{Role: "assistant", Content: []anthropicBlock{{Type: "tool_use", ID: "c1", Name: "Read"}}},
		{Role: "user", Content: []anthropicBlock{
			{Type: "tool_result", ToolUseID: "c1", Content: "r"},
			{Type: "text", Text: "mailbox"},
			{Type: "image", Source: &anthropicImageSource{Type: "base64", MediaType: "image/png", Data: "x"}},
		}},
	}
	boundary := anthropicCacheBoundary{
		MessageIndex:                  2,
		BlockEnd:                      3,
		SourceLastCacheablePayloadIdx: -1,
	}
	if !markAnthropicBoundaryForCache(messages, boundary) {
		t.Fatal("expected legacy fallback to find a cacheable block")
	}
	marked := messages[2].Content[1]
	if marked.CacheControl == nil || marked.CacheControl.Type != "ephemeral" {
		t.Errorf("expected CacheControl=ephemeral on mailbox_text, got %+v", marked.CacheControl)
	}
}
