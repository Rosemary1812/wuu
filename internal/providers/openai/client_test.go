package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestChat_SendsRequestAndParsesToolCall(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if got := r.Header.Get("X-Test"); got != "ok" {
			t.Fatalf("missing custom header, got %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["model"] != "gpt-test" {
			t.Fatalf("unexpected model: %v", body["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "choices": [
    {
      "message": {
        "content": "",
        "phase": "commentary",
        "tool_calls": [
          {
            "id": "call_1",
            "type": "function",
            "function": {
              "name": "run_shell",
              "arguments": "{\"command\":\"ls\"}"
            }
          }
        ]
      }
    }
  ]
}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Headers: map[string]string{"X-Test": "ok"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := client.Chat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
		},
		Tools: []providers.ToolDefinition{
			{Name: "run_shell", Description: "run shell", InputSchema: map[string]any{"type": "object"}},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.Phase != providers.MessagePhaseCommentary {
		t.Fatalf("unexpected phase: %q", resp.Phase)
	}
	if resp.ToolCalls[0].Name != "run_shell" {
		t.Fatalf("unexpected tool name: %s", resp.ToolCalls[0].Name)
	}
}

func TestChat_SendsMaxTokensAndReasoningEffort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["max_tokens"] != float64(321) {
			t.Fatalf("expected max_tokens=321, got %#v", body["max_tokens"])
		}
		if body["reasoning_effort"] != "high" {
			t.Fatalf("expected reasoning_effort=high, got %#v", body["reasoning_effort"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:     "gpt-test",
		Messages:  []providers.ChatMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 321,
		Effort:    "high",
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_SendsOpenRouterReasoningEffortShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if _, ok := body["reasoning_effort"]; ok {
			t.Fatalf("did not expect reasoning_effort for OpenRouter payload: %#v", body)
		}
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok || reasoning["effort"] != "high" {
			t.Fatalf("expected reasoning.effort=high, got %#v", body["reasoning"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL + "/openrouter.ai/v1", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:    "openai/gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
		Effort:   "high",
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_SendsProviderOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["reasoning_effort"] != "medium" {
			t.Fatalf("expected reasoning_effort=medium, got %#v", body["reasoning_effort"])
		}
		if body["verbosity"] != "low" {
			t.Fatalf("expected verbosity=low, got %#v", body["verbosity"])
		}
		if body["service_tier"] != "priority" {
			t.Fatalf("expected service_tier=priority, got %#v", body["service_tier"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:    "gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
		ProviderOptions: map[string]any{
			"reasoningEffort": "medium",
			"textVerbosity":   "low",
			"serviceTier":     "priority",
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_SendsOpenRouterProviderOptionsShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if _, ok := body["reasoning_effort"]; ok {
			t.Fatalf("did not expect reasoning_effort for OpenRouter payload: %#v", body)
		}
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok || reasoning["effort"] != "high" {
			t.Fatalf("expected reasoning.effort=high, got %#v", body["reasoning"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL + "/openrouter.ai/v1", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:    "openai/gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
		ProviderOptions: map[string]any{
			"reasoningEffort": "high",
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_SendsPromptCacheKeyForOpenAICompatible(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["promptCacheKey"] != "cache-key-1" {
			t.Fatalf("expected promptCacheKey, got %#v", body["promptCacheKey"])
		}
		if _, exists := body["prompt_cache_key"]; exists {
			t.Fatalf("did not expect prompt_cache_key on standard OpenAI payload: %#v", body["prompt_cache_key"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
		},
		CacheHint: &providers.CacheHint{PromptCacheKey: "cache-key-1"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_FiltersUnsupportedProviderOptions(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		for _, key := range []string{"include", "toolStreaming", "thinkingConfig", "reasoningConfig", "modelParams", "gateway"} {
			if _, exists := body[key]; exists {
				t.Fatalf("chat payload should filter %s: %#v", key, body)
			}
		}
		if body["metadata"] == nil {
			t.Fatalf("chat payload should keep ordinary provider options: %#v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:    "gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
		ProviderOptions: map[string]any{
			"include":         []any{"reasoning.encrypted_content"},
			"toolStreaming":   false,
			"thinkingConfig":  map[string]any{"includeThoughts": true},
			"reasoningConfig": map[string]any{"type": "enabled"},
			"modelParams":     map[string]any{"reasoning_effort": "high"},
			"gateway":         map[string]any{"caching": "auto"},
			"metadata":        map[string]any{"eval": "provider-options"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_SendsSamplingProviderOptions(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["temperature"] != float64(1) {
			t.Fatalf("expected temperature=1, got %#v", body["temperature"])
		}
		if body["top_p"] != 0.95 {
			t.Fatalf("expected top_p=0.95, got %#v", body["top_p"])
		}
		if body["top_k"] != float64(40) {
			t.Fatalf("expected top_k=40, got %#v", body["top_k"])
		}
		if _, exists := body["topP"]; exists {
			t.Fatalf("did not expect camel-case topP on wire: %#v", body)
		}
		if _, exists := body["topK"]; exists {
			t.Fatalf("did not expect camel-case topK on wire: %#v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:    "minimax-m2.1",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
		ProviderOptions: map[string]any{
			"temperature": 1.0,
			"topP":        0.95,
			"topK":        40,
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_DoesNotOverrideExplicitTemperatureWithProviderOption(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["temperature"] != 0.2 {
			t.Fatalf("expected explicit temperature=0.2, got %#v", body["temperature"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:       "minimax-m2.1",
		Messages:    []providers.ChatMessage{{Role: "user", Content: "hello"}},
		Temperature: 0.2,
		ProviderOptions: map[string]any{
			"temperature": 1.0,
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_SendsSnakeCasePromptCacheKeyForOpenRouter(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["prompt_cache_key"] != "cache-key-2" {
			t.Fatalf("expected prompt_cache_key, got %#v", body["prompt_cache_key"])
		}
		if _, exists := body["promptCacheKey"]; exists {
			t.Fatalf("did not expect promptCacheKey on OpenRouter payload: %#v", body["promptCacheKey"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key", Headers: map[string]string{"HTTP-Referer": "https://openrouter.ai/app"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "openrouter-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
		},
		CacheHint: &providers.CacheHint{PromptCacheKey: "cache-key-2"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_OmitsPromptCacheKeyWithoutHint(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if _, exists := body["promptCacheKey"]; exists {
			t.Fatalf("did not expect promptCacheKey without hint: %#v", body["promptCacheKey"])
		}
		if _, exists := body["prompt_cache_key"]; exists {
			t.Fatalf("did not expect prompt_cache_key without hint: %#v", body["prompt_cache_key"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestStreamIdleTimeout_DefaultMatchesCodex(t *testing.T) {
	t.Setenv("WUU_STREAM_IDLE_TIMEOUT_MS", "")
	if got := streamIdleTimeout(); got != 300*time.Second {
		t.Fatalf("expected 300s default stream idle timeout, got %s", got)
	}
}

func TestStreamConnectTimeout_DefaultAccommodatesRelay(t *testing.T) {
	t.Setenv("WUU_STREAM_CONNECT_TIMEOUT_MS", "")
	if got := streamConnectTimeout(); got != 600*time.Second {
		t.Fatalf("expected 600s default stream connect timeout, got %s", got)
	}
}

func TestChat_SendsImageContentParts(t *testing.T) {
	t.Helper()

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

		textPart, ok := content[0].(map[string]any)
		if !ok || textPart["type"] != "text" || textPart["text"] != "look at this" {
			t.Fatalf("unexpected text part: %#v", content[0])
		}

		imagePart, ok := content[1].(map[string]any)
		if !ok || imagePart["type"] != "image_url" {
			t.Fatalf("unexpected image part: %#v", content[1])
		}
		imageURL, ok := imagePart["image_url"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected image_url payload: %#v", imagePart["image_url"])
		}
		if imageURL["url"] != "data:image/png;base64,AAA" {
			t.Fatalf("unexpected image data url: %#v", imageURL["url"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
		Messages: []providers.ChatMessage{
			{
				Role:    "user",
				Content: "look at this",
				Images: []providers.InputImage{
					{MediaType: "image/png", Data: "AAA"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_SendsFileContentParts(t *testing.T) {
	t.Helper()

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
		filePart, ok := content[1].(map[string]any)
		if !ok || filePart["type"] != "file" {
			t.Fatalf("unexpected file part: %#v", content[1])
		}
		filePayload, ok := filePart["file"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected file payload: %#v", filePart["file"])
		}
		if filePayload["filename"] != "brief.pdf" || filePayload["file_data"] != "data:application/pdf;base64,JVBERi0xLjQ=" {
			t.Fatalf("unexpected file payload: %#v", filePayload)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
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
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_SendsPromptCacheKeyAliases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PromptCacheKey string `json:"promptCacheKey"`
			AltCacheKey    string `json:"prompt_cache_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body.PromptCacheKey != "cache-key-1" {
			t.Fatalf("unexpected promptCacheKey: %q", body.PromptCacheKey)
		}
		if body.AltCacheKey != "" {
			t.Fatalf("unexpected prompt_cache_key on OpenAI-compatible payload: %q", body.AltCacheKey)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
		},
		CacheHint: &providers.CacheHint{PromptCacheKey: "cache-key-1"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_SendsReasoningContentInAssistantToolCallMessage(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role             string `json:"role"`
				Content          any    `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []any  `json:"tool_calls"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if len(body.Messages) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(body.Messages))
		}
		assistant := body.Messages[1]
		if assistant.Role != "assistant" {
			t.Fatalf("expected assistant role, got %q", assistant.Role)
		}
		if assistant.ReasoningContent != "inspect repo before tool use" {
			t.Fatalf("unexpected reasoning_content: %q", assistant.ReasoningContent)
		}
		if len(assistant.ToolCalls) != 1 {
			t.Fatalf("expected tool_calls to be present, got %#v", assistant.ToolCalls)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "review this repo"},
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

func TestChat_SendsEmptyReasoningContentForDeepSeekAssistantReplay(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if len(body.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(body.Messages))
		}
		assistant := body.Messages[1]
		value, exists := assistant["reasoning_content"]
		if !exists || value != "" {
			t.Fatalf("expected empty reasoning_content key, got exists=%v value=%#v body=%#v", exists, value, assistant)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "deepseek-v4",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "previous answer"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_AppliesMistralMessageCompatibility(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role       string `json:"role"`
				Content    string `json:"content"`
				ToolCallID string `json:"tool_call_id"`
				ToolCalls  []struct {
					ID string `json:"id"`
				} `json:"tool_calls"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if len(body.Messages) != 5 {
			t.Fatalf("expected assistant separator message, got %#v", body.Messages)
		}
		if body.Messages[1].ToolCalls[0].ID != "call12345" || body.Messages[2].ToolCallID != "call12345" {
			t.Fatalf("expected scrubbed Mistral tool call IDs, got %#v", body.Messages)
		}
		if body.Messages[3].Role != "assistant" || body.Messages[3].Content != "Done." || body.Messages[4].Role != "user" {
			t.Fatalf("expected Done separator before user, got %#v", body.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "mistral-large-latest",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_123456789_extra", Name: "read", Arguments: `{}`}}},
			{Role: "tool", ToolCallID: "call_123456789_extra", Content: "ok"},
			{Role: "user", Content: "next"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestChat_ParsesReasoningContent(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "choices": [
    {
      "message": {
        "content": "",
        "reasoning_content": "inspect repo before tool use",
        "tool_calls": [
          {
            "id": "call_1",
            "type": "function",
            "function": {
              "name": "list_files",
              "arguments": "{}"
            }
          }
        ]
      }
    }
  ]
}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := client.Chat(context.Background(), providers.ChatRequest{
		Model:    "gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.ReasoningContent != "inspect repo before tool use" {
		t.Fatalf("unexpected reasoning content: %q", resp.ReasoningContent)
	}
}

func TestChat_HandlesProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:    "gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected provider error")
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
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
		t.Fatalf("New: %v", err)
	}

	resp, err := client.Chat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("unexpected response content: %q", resp.Content)
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
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "hello"},
		},
	})
	if err == nil {
		t.Fatal("expected auth error")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected 1 attempt for auth failure, got %d", got)
	}
}

func TestNewStreamingHTTPClient_DisablesOverallTimeout(t *testing.T) {
	base := &http.Client{
		Timeout:       5 * time.Second,
		Transport:     http.DefaultTransport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	streamClient := newStreamingHTTPClient(base, providers.StreamTransportConfig{
		ConnectTimeout: time.Second,
		IdleTimeout:    5 * time.Second,
	})

	if streamClient == base {
		t.Fatal("expected streaming client to clone the base client")
	}
	if streamClient.Timeout != 0 {
		t.Fatalf("expected streaming client timeout disabled, got %s", streamClient.Timeout)
	}
	if streamClient.Transport == base.Transport {
		t.Fatal("expected streaming client transport to be cloned")
	}
	if streamClient.CheckRedirect == nil {
		t.Fatal("expected streaming client to preserve redirect policy")
	}
	if base.Timeout != 5*time.Second {
		t.Fatalf("expected base client timeout unchanged, got %s", base.Timeout)
	}
}

func TestStreamChat_ConnectTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(ClientConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		RetryConfig: &providers.RetryConfig{
			MaxRetries:   0,
			InitialDelay: time.Millisecond,
			MaxDelay:     time.Millisecond,
		},
		StreamConfig: &providers.StreamTransportConfig{
			ConnectTimeout: 50 * time.Millisecond,
			IdleTimeout:    time.Second,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start := time.Now()
	_, err = client.StreamChat(context.Background(), providers.ChatRequest{
		Model:    "gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected connect timeout error")
	}
	if elapsed >= 250*time.Millisecond {
		t.Fatalf("expected connect timeout to fail quickly, took %s", elapsed)
	}
}

func TestStreamChat_SSE(t *testing.T) {
	ssePayload := "data: {\"choices\":[{\"delta\":{\"phase\":\"final_answer\",\"content\":\"Hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"path\\\":\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"test.go\\\"}\"}}]}},{\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
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
		Model:    "gpt-test",
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
	var phases []providers.MessagePhase
	for _, ev := range events {
		if ev.Type == providers.EventContentDelta {
			contentParts = append(contentParts, ev.Content)
			phases = append(phases, ev.Phase)
		}
	}
	if len(contentParts) != 2 || contentParts[0] != "Hello" || contentParts[1] != " world" {
		t.Fatalf("unexpected content deltas: %v", contentParts)
	}
	if len(phases) != 2 || phases[0] != providers.MessagePhaseFinalAnswer || phases[1] != providers.MessagePhaseFinalAnswer {
		t.Fatalf("unexpected content phases: %v", phases)
	}

	// Verify tool call events.
	var toolStarts, toolEnds int
	var endToolCall *providers.ToolCall
	for _, ev := range events {
		switch ev.Type {
		case providers.EventToolUseStart:
			toolStarts++
			if ev.ToolCall == nil || ev.ToolCall.Name != "read_file" {
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
	if endToolCall == nil || endToolCall.ID != "call_1" {
		t.Fatalf("unexpected tool end call: %+v", endToolCall)
	}
	if endToolCall.Arguments != `{"path":"test.go"}` {
		t.Fatalf("unexpected tool arguments: %q", endToolCall.Arguments)
	}

	// Verify EventDone is the last event.
	last := events[len(events)-1]
	if last.Type != providers.EventDone {
		t.Fatalf("expected last event to be EventDone, got %s", last.Type)
	}
}

func TestStreamChat_EmitsThinkingEventsForReasoningContent(t *testing.T) {
	ssePayload := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"inspect \"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"repo\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"list_files\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
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
		Model:    "gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var events []providers.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	if len(events) < 5 {
		t.Fatalf("expected thinking/tool events, got %v", events)
	}
	if events[0].Type != providers.EventThinkingDelta || events[0].Content != "inspect " {
		t.Fatalf("unexpected first event: %+v", events[0])
	}
	if events[1].Type != providers.EventThinkingDelta || events[1].Content != "repo" {
		t.Fatalf("unexpected second event: %+v", events[1])
	}
	if events[2].Type != providers.EventThinkingDone {
		t.Fatalf("expected thinking done before tool call, got %+v", events[2])
	}
	if events[3].Type != providers.EventToolUseStart {
		t.Fatalf("expected tool start after thinking, got %+v", events[3])
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

func TestStreamChat_MissingDoneYieldsIncompleteError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
	}))
	defer server.Close()

	client, _ := New(ClientConfig{BaseURL: server.URL, APIKey: "k"})
	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:    "m",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hi"}},
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
	if events[0].Type != providers.EventContentDelta || events[0].Content != "hi" {
		t.Fatalf("unexpected first event: %+v", events[0])
	}
	if events[1].Type != providers.EventError {
		t.Fatalf("expected terminal error, got %+v", events[1])
	}
	if events[1].Error == nil || !providers.IsRetryable(events[1].Error) {
		t.Fatalf("expected retryable incomplete stream error, got %v", events[1].Error)
	}
}

func TestStreamChat_IdleWatchdogFires(t *testing.T) {
	// Set a very short idle timeout for the test.
	t.Setenv("WUU_STREAM_IDLE_TIMEOUT_MS", "100")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Write one chunk then hang forever — the watchdog should fire.
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Block until the client disconnects.
		<-r.Context().Done()
	}))
	defer server.Close()

	client, _ := New(ClientConfig{BaseURL: server.URL, APIKey: "k"})
	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:    "m",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var gotContent bool
	var gotError bool
	var errMsg string
	for ev := range ch {
		switch ev.Type {
		case providers.EventContentDelta:
			gotContent = true
		case providers.EventError:
			gotError = true
			if ev.Error != nil {
				errMsg = ev.Error.Error()
			}
		}
	}
	if !gotContent {
		t.Fatal("expected at least one content delta before timeout")
	}
	if !gotError {
		t.Fatal("expected error event from idle watchdog")
	}
	if !errors.Is(fmt.Errorf("wrap: %w", context.DeadlineExceeded), context.DeadlineExceeded) {
		t.Fatal("sanity check failed")
	}
	if errMsg == "" || !strings.Contains(errMsg, "idle timeout") {
		t.Fatalf("expected idle timeout error, got: %q", errMsg)
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
	if !strings.Contains(err.Error(), "invalid message sequence after normalization") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResponsesChat_SendsResponsesPayloadAndParsesToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if _, exists := body["messages"]; exists {
			t.Fatalf("responses payload must not include chat messages: %#v", body["messages"])
		}
		if body["model"] != "gpt-test" {
			t.Fatalf("unexpected model: %#v", body["model"])
		}
		if body["instructions"] != "sys" {
			t.Fatalf("unexpected instructions: %#v", body["instructions"])
		}
		if body["max_output_tokens"] != float64(123) {
			t.Fatalf("expected max_output_tokens=123, got %#v", body["max_output_tokens"])
		}

		tools, ok := body["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("unexpected tools payload: %#v", body["tools"])
		}
		tool, ok := tools[0].(map[string]any)
		if !ok || tool["type"] != "function" || tool["name"] != "read_file" {
			t.Fatalf("unexpected responses tool: %#v", tools[0])
		}
		if tool["strict"] != false {
			t.Fatalf("expected strict=false like Codex Responses tools, got %#v", tool["strict"])
		}
		parameters, ok := tool["parameters"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected parameters payload: %#v", tool["parameters"])
		}
		if properties, ok := parameters["properties"].(map[string]any); !ok || len(properties) != 0 {
			t.Fatalf("expected empty object properties for responses schema, got %#v", parameters["properties"])
		}
		if _, exists := tool["function"]; exists {
			t.Fatalf("responses tool must not use chat-completions function wrapper: %#v", tool)
		}

		input, ok := body["input"].([]any)
		if !ok || len(input) != 3 {
			t.Fatalf("unexpected input payload: %#v", body["input"])
		}
		callItem := input[1].(map[string]any)
		if callItem["type"] != "function_call" || callItem["call_id"] != "call_1" || callItem["name"] != "read_file" {
			t.Fatalf("unexpected function_call input: %#v", callItem)
		}
		outputItem := input[2].(map[string]any)
		if outputItem["type"] != "function_call_output" || outputItem["call_id"] != "call_1" || outputItem["output"] != "file contents" {
			t.Fatalf("unexpected function_call_output input: %#v", outputItem)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "status": "completed",
  "output": [
    {
      "type": "function_call",
      "call_id": "call_2",
      "name": "read_file",
      "arguments": "{\"path\":\"README.md\"}"
    }
  ],
  "usage": {
    "input_tokens": 10,
    "input_tokens_details": {"cached_tokens": 3},
    "output_tokens": 4
  }
}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, WireAPI: "responses", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := client.Chat(context.Background(), providers.ChatRequest{
		Model:     "gpt-test",
		MaxTokens: 123,
		Messages: []providers.ChatMessage{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "read README"},
			{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "read_file", Arguments: `{"path":"README.md"}`}}},
			{Role: "tool", ToolCallID: "call_1", Content: "file contents"},
		},
		Tools: []providers.ToolDefinition{
			{Name: "read_file", Description: "read file", InputSchema: map[string]any{"type": "object"}},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_2" || resp.ToolCalls[0].Name != "read_file" {
		t.Fatalf("unexpected tool calls: %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Arguments != `{"path":"README.md"}` {
		t.Fatalf("unexpected tool args: %q", resp.ToolCalls[0].Arguments)
	}
	if resp.StopReason != "tool_calls" {
		t.Fatalf("expected tool_calls stop reason, got %q", resp.StopReason)
	}
	wantUsage := &providers.TokenUsage{InputTokens: 7, OutputTokens: 4, CacheReadTokens: 3}
	if !reflect.DeepEqual(resp.Usage, wantUsage) {
		t.Fatalf("got usage %+v, want %+v", resp.Usage, wantUsage)
	}
}

func TestResponsesChat_FiltersUnsupportedProviderOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		for _, key := range []string{"toolStreaming", "thinkingConfig", "reasoningConfig", "modelParams", "gateway", "usage", "chat_template_args", "enable_thinking", "thinking"} {
			if _, exists := body[key]; exists {
				t.Fatalf("responses payload should filter %s: %#v", key, body)
			}
		}
		if _, exists := body["include"]; !exists {
			t.Fatalf("responses payload should keep include: %#v", body)
		}
		if body["metadata"] == nil {
			t.Fatalf("responses payload should keep ordinary provider options: %#v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, WireAPI: "responses", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:    "gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
		ProviderOptions: map[string]any{
			"include":            []any{"reasoning.encrypted_content"},
			"toolStreaming":      false,
			"thinkingConfig":     map[string]any{"includeThoughts": true},
			"reasoningConfig":    map[string]any{"type": "enabled"},
			"modelParams":        map[string]any{"reasoning_effort": "high"},
			"gateway":            map[string]any{"caching": "auto"},
			"usage":              map[string]any{"include": true},
			"chat_template_args": map[string]any{"enable_thinking": true},
			"enable_thinking":    true,
			"thinking":           map[string]any{"type": "enabled"},
			"metadata":           map[string]any{"eval": "provider-options"},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestResponsesChat_SendsSamplingProviderOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["temperature"] != float64(1) {
			t.Fatalf("expected temperature=1, got %#v", body["temperature"])
		}
		if body["top_p"] != 0.95 {
			t.Fatalf("expected top_p=0.95, got %#v", body["top_p"])
		}
		if body["top_k"] != float64(20) {
			t.Fatalf("expected top_k=20, got %#v", body["top_k"])
		}
		if _, exists := body["topP"]; exists {
			t.Fatalf("did not expect camel-case topP on wire: %#v", body)
		}
		if _, exists := body["topK"]; exists {
			t.Fatalf("did not expect camel-case topK on wire: %#v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, WireAPI: "responses", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:    "minimax-m2",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
		ProviderOptions: map[string]any{
			"temperature": 1.0,
			"topP":        0.95,
			"topK":        20,
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestResponsesChat_SendsFileContentParts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		input, ok := body["input"].([]any)
		if !ok || len(input) != 1 {
			t.Fatalf("unexpected input payload: %#v", body["input"])
		}
		item, ok := input[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected input item: %#v", input[0])
		}
		content, ok := item["content"].([]any)
		if !ok || len(content) != 2 {
			t.Fatalf("unexpected content payload: %#v", item["content"])
		}
		filePart, ok := content[1].(map[string]any)
		if !ok || filePart["type"] != "input_file" {
			t.Fatalf("unexpected file part: %#v", content[1])
		}
		if filePart["filename"] != "brief.pdf" || filePart["file_data"] != "data:application/pdf;base64,JVBERi0xLjQ=" {
			t.Fatalf("unexpected file part: %#v", filePart)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "status": "completed",
  "output": [{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]
}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, WireAPI: "responses", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
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
		t.Fatalf("Chat: %v", err)
	}
}

func TestResponsesChat_SendsProviderOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok || reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
			t.Fatalf("unexpected reasoning payload: %#v", body["reasoning"])
		}
		text, ok := body["text"].(map[string]any)
		if !ok || text["verbosity"] != "low" {
			t.Fatalf("unexpected text payload: %#v", body["text"])
		}
		if body["max_output_tokens"] != float64(777) {
			t.Fatalf("expected max_output_tokens=777, got %#v", body["max_output_tokens"])
		}
		if body["service_tier"] != "priority" {
			t.Fatalf("expected service_tier=priority, got %#v", body["service_tier"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "status": "completed",
  "output": [{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]
}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, WireAPI: "responses", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:    "gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
		ProviderOptions: map[string]any{
			"reasoningEffort":  "high",
			"reasoningSummary": "auto",
			"textVerbosity":    "low",
			"maxOutputTokens":  777,
			"serviceTier":      "priority",
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestResponsesChat_ParsesMessageContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "status": "completed",
  "output": [
    {
      "type": "message",
      "role": "assistant",
      "phase": "final_answer",
      "content": [
        {"type": "output_text", "text": "hello"},
        {"type": "output_text", "text": "world"}
      ]
    }
  ]
}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, WireAPI: "responses", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := client.Chat(context.Background(), providers.ChatRequest{
		Model:    "gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "hello\nworld" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if resp.Phase != providers.MessagePhaseFinalAnswer {
		t.Fatalf("unexpected phase: %q", resp.Phase)
	}
}

func TestResponsesChat_ContextLengthExceededClassified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "status": "failed",
  "error": {
    "code": "context_length_exceeded",
    "message": "Your input exceeds the context window of this model. Please adjust your input and try again."
  }
}`))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, WireAPI: "responses", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Chat(context.Background(), providers.ChatRequest{
		Model:    "gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if !providers.IsContextOverflow(err) {
		t.Fatalf("expected context overflow error, got %T (%v)", err, err)
	}
}

func TestResponsesStreamChat_SSE(t *testing.T) {
	ssePayload := "event: response.output_item.added\n" +
		"data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final_answer\",\"status\":\"in_progress\"},\"output_index\":0}\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\n" +
		"event: response.output_item.added\n" +
		"data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"status\":\"in_progress\",\"arguments\":\"\",\"call_id\":\"call_1\",\"name\":\"read_file\"},\"output_index\":0}\n\n" +
		"event: response.function_call_arguments.delta\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"{\\\"path\\\":\",\"item_id\":\"fc_1\",\"output_index\":0}\n\n" +
		"event: response.function_call_arguments.delta\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"\\\"README.md\\\"}\",\"item_id\":\"fc_1\",\"output_index\":0}\n\n" +
		"event: response.function_call_arguments.done\n" +
		"data: {\"type\":\"response.function_call_arguments.done\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\",\"item_id\":\"fc_1\",\"output_index\":0}\n\n" +
		"event: response.output_item.done\n" +
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"status\":\"completed\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\",\"call_id\":\"call_1\",\"name\":\"read_file\"},\"output_index\":0}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":5,\"output_tokens\":2}}}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ssePayload))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, WireAPI: "responses", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:    "gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
		Tools: []providers.ToolDefinition{
			{Name: "read_file", Description: "read file", InputSchema: map[string]any{"type": "object"}},
		},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var events []providers.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	var content string
	var contentPhase providers.MessagePhase
	var toolStarts, toolEnds int
	var endToolCall *providers.ToolCall
	var done *providers.StreamEvent
	for i := range events {
		ev := events[i]
		switch ev.Type {
		case providers.EventContentDelta:
			content += ev.Content
			if ev.Content != "" {
				contentPhase = ev.Phase
			}
		case providers.EventToolUseStart:
			toolStarts++
			if ev.ToolCall == nil || ev.ToolCall.ID != "call_1" || ev.ToolCall.Name != "read_file" {
				t.Fatalf("unexpected tool start: %+v", ev.ToolCall)
			}
		case providers.EventToolUseEnd:
			toolEnds++
			endToolCall = ev.ToolCall
		case providers.EventDone:
			done = &events[i]
		}
	}
	if content != "Hello" {
		t.Fatalf("unexpected content: %q", content)
	}
	if contentPhase != providers.MessagePhaseFinalAnswer {
		t.Fatalf("unexpected content phase: %q", contentPhase)
	}
	if toolStarts != 1 || toolEnds != 1 {
		t.Fatalf("expected one tool start/end, got starts=%d ends=%d events=%+v", toolStarts, toolEnds, events)
	}
	if endToolCall == nil || endToolCall.Arguments != `{"path":"README.md"}` {
		t.Fatalf("unexpected tool end: %+v", endToolCall)
	}
	if done == nil || done.StopReason != "tool_calls" {
		t.Fatalf("unexpected done event: %+v", done)
	}
	if done.Usage == nil || done.Usage.InputTokens != 5 || done.Usage.OutputTokens != 2 {
		t.Fatalf("unexpected usage: %+v", done.Usage)
	}
}

func TestResponsesStreamChat_ContextLengthExceededClassified(t *testing.T) {
	rawError := `{"type":"response.failed","response":{"status":"failed","error":{"code":"context_length_exceeded","message":"Your input exceeds the context window of this model. Please adjust your input and try again."}}}`
	ssePayload := "event: response.failed\n" +
		"data: " + rawError + "\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ssePayload))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, WireAPI: "responses", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:    "gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var got error
	for ev := range ch {
		if ev.Type == providers.EventError {
			got = ev.Error
			break
		}
	}
	if !providers.IsContextOverflow(got) {
		t.Fatalf("expected context overflow stream error, got %T (%v)", got, got)
	}
}

func TestResponsesStreamChat_TopLevelContextLengthErrorClassified(t *testing.T) {
	rawError := `{"type":"error","error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"Your input exceeds the context window of this model. Please adjust your input and try again.","param":"input"},"sequence_number":2}`
	ssePayload := "event: error\n" +
		"data: " + rawError + "\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(ssePayload))
	}))
	defer server.Close()

	client, err := New(ClientConfig{BaseURL: server.URL, WireAPI: "responses", APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:    "gpt-test",
		Messages: []providers.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var got error
	for ev := range ch {
		if ev.Type == providers.EventError {
			got = ev.Error
			break
		}
	}
	if !providers.IsContextOverflow(got) {
		t.Fatalf("expected top-level context overflow stream error, got %T (%v)", got, got)
	}
}

func TestNew_RejectsUnknownWireAPI(t *testing.T) {
	_, err := New(ClientConfig{BaseURL: "https://example.com", WireAPI: "legacy", APIKey: "test-key"})
	if err == nil {
		t.Fatal("expected unknown wire API error")
	}
}

func TestChunkUsage_AsTokenUsage_Cached(t *testing.T) {
	// gpt-4o reports cached_tokens as a SUBSET of prompt_tokens. The
	// helper has to split it out so wuu's auto-compact accounts for
	// the cache portion explicitly.
	u := &chunkUsage{
		PromptTokens:     5000,
		CompletionTokens: 200,
		PromptTokensDetails: &struct {
			CachedTokens int `json:"cached_tokens,omitempty"`
		}{CachedTokens: 4500},
	}
	got := u.asTokenUsage()
	want := &providers.TokenUsage{
		InputTokens:     500, // 5000 - 4500
		OutputTokens:    200,
		CacheReadTokens: 4500,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// And TotalContextTokens should still equal the original
	// prompt_tokens + completion_tokens.
	if total := got.TotalContextTokens(); total != 5200 {
		t.Fatalf("expected total 5200, got %d", total)
	}
}

func TestChunkUsage_AsTokenUsage_NoCacheDetails(t *testing.T) {
	// Older OpenAI / OpenRouter / proxy responses without
	// prompt_tokens_details should still parse cleanly.
	u := &chunkUsage{PromptTokens: 1000, CompletionTokens: 300}
	got := u.asTokenUsage()
	want := &providers.TokenUsage{InputTokens: 1000, OutputTokens: 300}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestChunkUsage_AsTokenUsage_Nil(t *testing.T) {
	var u *chunkUsage
	if got := u.asTokenUsage(); got != nil {
		t.Fatalf("expected nil for nil receiver, got %+v", got)
	}
}
