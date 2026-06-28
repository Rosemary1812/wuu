package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/coder/websocket"
)

func TestResolveCodexWebSocketURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"https://chatgpt.com/backend-api/codex", "wss://chatgpt.com/backend-api/codex/responses", false},
		{"https://chatgpt.com/backend-api/codex/", "wss://chatgpt.com/backend-api/codex/responses", false},
		{"https://chatgpt.com/backend-api/codex/responses", "wss://chatgpt.com/backend-api/codex/responses", false},
		{"https://chatgpt.com/backend-api/codex/responses/compact", "wss://chatgpt.com/backend-api/codex/responses/compact", false},
		{"http://localhost:8080/codex", "ws://localhost:8080/codex/responses", false},
		{"wss://chatgpt.com/x", "wss://chatgpt.com/x/responses", false},
		{"ws://localhost:8080/x", "ws://localhost:8080/x/responses", false},
		{"", "", true},
		{"ftp://chatgpt.com/x", "", true},
	}
	for _, tc := range cases {
		got, err := resolveCodexWebSocketURL(tc.in)
		if tc.err {
			if err == nil {
				t.Errorf("resolveCodexWebSocketURL(%q): expected error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("resolveCodexWebSocketURL(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("resolveCodexWebSocketURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDialCodexWebSocket_HappyPath(t *testing.T) {
	var seenHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeaders = r.Header.Clone()
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("server-side accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		// Echo a single text message so the client can confirm the WS is live.
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := conn.Write(ctx, websocket.MessageText, []byte("hello")); err != nil {
			t.Errorf("server-side write: %v", err)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)
	headers := http.Header{}
	headers.Set("Authorization", "Bearer test-token")
	headers.Set("chatgpt-account-id", "acct_abc")
	headers.Set("session-id", "thread-1")
	headers.Set("x-client-request-id", "thread-1")

	conn, err := (CodexWebSocketDialer{}).dialCodexWebSocket(context.Background(), wsURL, headers)
	if err != nil {
		t.Fatalf("dialCodexWebSocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Handshake headers from the upgrade request the server received.
	if got := seenHeaders.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization = %q", got)
	}
	if got := seenHeaders.Get("chatgpt-account-id"); got != "acct_abc" {
		t.Errorf("chatgpt-account-id = %q", got)
	}
	if got := seenHeaders.Get("OpenAI-Beta"); got != CodexWebSocketBetaTag {
		t.Errorf("OpenAI-Beta = %q, want %q", got, CodexWebSocketBetaTag)
	}
	if got := seenHeaders.Get("session-id"); got != "thread-1" {
		t.Errorf("session-id = %q", got)
	}
	if got := seenHeaders.Get("x-client-request-id"); got != "thread-1" {
		t.Errorf("x-client-request-id = %q", got)
	}

	// Confirm a message roundtrip works through the upgraded connection.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Errorf("message type = %v, want MessageText", typ)
	}
	if string(data) != "hello" {
		t.Errorf("echoed message = %q, want hello", string(data))
	}
}

func TestDialCodexWebSocket_AllowsResponsesMessagesAboveDefaultReadLimit(t *testing.T) {
	largeMessage := strings.Repeat("x", 64*1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("server-side accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := conn.Write(ctx, websocket.MessageText, []byte(largeMessage)); err != nil {
			t.Errorf("server-side write: %v", err)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)
	conn, err := (CodexWebSocketDialer{}).dialCodexWebSocket(context.Background(), wsURL, http.Header{})
	if err != nil {
		t.Fatalf("dialCodexWebSocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Errorf("message type = %v, want MessageText", typ)
	}
	if string(data) != largeMessage {
		t.Fatalf("large message length = %d, want %d", len(data), len(largeMessage))
	}
}

func TestDialCodexWebSocket_InjectsBetaTagWhenAbsent(t *testing.T) {
	var seenBeta string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenBeta = r.Header.Get("OpenAI-Beta")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)
	conn, err := (CodexWebSocketDialer{}).dialCodexWebSocket(context.Background(), wsURL, http.Header{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if seenBeta != CodexWebSocketBetaTag {
		t.Errorf("OpenAI-Beta = %q, want %q", seenBeta, CodexWebSocketBetaTag)
	}
}

func TestDialCodexWebSocket_PreservesCallerBetaTag(t *testing.T) {
	var seenBeta string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenBeta = r.Header.Get("OpenAI-Beta")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)
	headers := http.Header{}
	headers.Set("OpenAI-Beta", "responses=experimental")
	conn, err := (CodexWebSocketDialer{}).dialCodexWebSocket(context.Background(), wsURL, headers)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if seenBeta != "responses=experimental" {
		t.Errorf("OpenAI-Beta = %q, want %q (caller override)", seenBeta, "responses=experimental")
	}
}

func TestDialCodexWebSocket_RejectsEmptyURL(t *testing.T) {
	if _, err := (CodexWebSocketDialer{}).dialCodexWebSocket(context.Background(), "", http.Header{}); err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestDialCodexWebSocket_RespectsContextCancel(t *testing.T) {
	// Block the upgrade response so the client cancel races.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)
	if _, err := (CodexWebSocketDialer{}).dialCodexWebSocket(ctx, wsURL, http.Header{}); err == nil {
		t.Fatal("expected dial to fail when context is already canceled")
	}
}

func TestResponsesStreamChatWebSocket_UsesPreviousResponseIDDelta(t *testing.T) {
	requests := make(chan map[string]any, 2)
	betas := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		betas <- r.Header.Get("OpenAI-Beta")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		for i := 0; i < 2; i++ {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				t.Errorf("read request %d: %v", i+1, err)
				return
			}
			if typ != websocket.MessageText {
				t.Errorf("request %d type = %v", i+1, typ)
				return
			}
			var body map[string]any
			if err := json.Unmarshal(data, &body); err != nil {
				t.Errorf("decode request %d: %v", i+1, err)
				return
			}
			requests <- body

			if i == 0 {
				writeWSEvent(t, ctx, conn, `{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","status":"in_progress","arguments":"","call_id":"call_1","name":"read_file"},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.function_call_arguments.done","arguments":"{\"path\":\"README.md\"}","item_id":"fc_1","output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.done","item":{"id":"fc_1","type":"function_call","status":"completed","arguments":"{\"path\":\"README.md\"}","call_id":"call_1","name":"read_file"},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.done","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`)
			} else {
				writeWSEvent(t, ctx, conn, `{"type":"response.created","response":{"id":"resp_2","status":"in_progress"}}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.added","item":{"id":"msg_2","type":"message","role":"assistant","phase":"final_answer","status":"in_progress"},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_text.delta","delta":"done","item_id":"msg_2","output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.done","item":{"id":"msg_2","type":"message","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"done"}]},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.completed","response":{"id":"resp_2","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`)
			}
		}
	}))
	defer server.Close()

	store := false
	client, err := New(ClientConfig{
		BaseURL:                 server.URL,
		WireAPI:                 "responses",
		APIKey:                  "test-key",
		Headers:                 map[string]string{"OpenAI-Beta": "responses=experimental"},
		ResponsesStore:          &store,
		ResponsesWebSocket:      true,
		ResponsesWebSocketCache: NewResponsesWebSocketCache(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tools := []providers.ToolDefinition{{Name: "read_file", Description: "read file", InputSchema: map[string]any{"type": "object"}}}
	cache := &providers.CacheHint{PromptCacheKey: "thread-1"}
	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:     "gpt-test",
		Messages:  []providers.ChatMessage{{Role: "user", Content: "read README"}},
		Tools:     tools,
		CacheHint: cache,
	})
	if err != nil {
		t.Fatalf("first StreamChat: %v", err)
	}
	firstStates, err := drainStreamProviderStates(ch)
	if err != nil {
		t.Fatalf("first stream: %v", err)
	}
	if len(firstStates) != 1 ||
		firstStates[0].ReplayMode != "full_request" ||
		firstStates[0].PreviousResponseIDUsed ||
		firstStates[0].InputItems != 1 ||
		firstStates[0].FullInputItems != 1 ||
		firstStates[0].DeltaInputItems != 0 {
		t.Fatalf("unexpected first provider state: %+v", firstStates)
	}

	ch, err = client.StreamChat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "read README"},
			{
				Role: "assistant",
				ToolCalls: []providers.ToolCall{{
					ID:                "call_1",
					ProviderItemID:    "fc_1",
					ProviderItemModel: "gpt-test",
					Name:              "read_file",
					Arguments:         `{"path":"README.md"}`,
				}},
			},
			{Role: "tool", ToolCallID: "call_1", Content: "contents"},
		},
		Tools:     tools,
		CacheHint: cache,
	})
	if err != nil {
		t.Fatalf("second StreamChat: %v", err)
	}
	secondStates, err := drainStreamProviderStates(ch)
	if err != nil {
		t.Fatalf("second stream: %v", err)
	}
	if len(secondStates) != 1 ||
		secondStates[0].ReplayMode != "previous_response_id" ||
		!secondStates[0].PreviousResponseIDUsed ||
		secondStates[0].InputItems != 1 ||
		secondStates[0].FullInputItems != 3 ||
		secondStates[0].DeltaInputItems != 1 {
		t.Fatalf("unexpected second provider state: %+v", secondStates)
	}

	if got := <-betas; got != CodexWebSocketBetaTag {
		t.Fatalf("OpenAI-Beta = %q, want %q", got, CodexWebSocketBetaTag)
	}
	first := <-requests
	if first["type"] != "response.create" {
		t.Fatalf("first request type = %#v", first["type"])
	}
	if _, exists := first["previous_response_id"]; exists {
		t.Fatalf("first request must be full context: %#v", first)
	}
	firstInput := first["input"].([]any)
	if len(firstInput) != 1 {
		t.Fatalf("first request input = %#v", firstInput)
	}

	second := <-requests
	if second["previous_response_id"] != "resp_1" {
		t.Fatalf("second request previous_response_id = %#v; body=%#v", second["previous_response_id"], second)
	}
	secondInput := second["input"].([]any)
	if len(secondInput) != 1 {
		t.Fatalf("second request should send only delta input, got %#v", secondInput)
	}
	output, ok := secondInput[0].(map[string]any)
	if !ok || output["type"] != "function_call_output" || output["call_id"] != "call_1" || output["output"] != "contents" {
		t.Fatalf("unexpected delta input: %#v", secondInput[0])
	}
}

func TestResponsesStreamChatWebSocket_UsesPreviousResponseIDDeltaAfterFinalAnswer(t *testing.T) {
	requests := make(chan map[string]any, 2)
	const reasoningItem = `{"id":"rs_1","type":"reasoning","content":[],"encrypted_content":"abc","summary":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		for i := 0; i < 2; i++ {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				t.Errorf("read request %d: %v", i+1, err)
				return
			}
			if typ != websocket.MessageText {
				t.Errorf("request %d type = %v", i+1, typ)
				return
			}
			var body map[string]any
			if err := json.Unmarshal(data, &body); err != nil {
				t.Errorf("decode request %d: %v", i+1, err)
				return
			}
			requests <- body

			if i == 0 {
				writeWSEvent(t, ctx, conn, `{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.done","item":`+reasoningItem+`,"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.added","item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","status":"in_progress"},"output_index":1}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_text.delta","delta":"probe-one","item_id":"msg_1","output_index":1}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.done","item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"probe-one"}]},"output_index":1}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`)
			} else {
				writeWSEvent(t, ctx, conn, `{"type":"response.created","response":{"id":"resp_2","status":"in_progress"}}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.added","item":{"id":"msg_2","type":"message","role":"assistant","phase":"final_answer","status":"in_progress"},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_text.delta","delta":"probe-two","item_id":"msg_2","output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.done","item":{"id":"msg_2","type":"message","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"probe-two"}]},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.completed","response":{"id":"resp_2","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`)
			}
		}
	}))
	defer server.Close()

	store := false
	client, err := New(ClientConfig{
		BaseURL:            server.URL,
		WireAPI:            "responses",
		APIKey:             "test-key",
		ResponsesStore:     &store,
		ResponsesWebSocket: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cache := &providers.CacheHint{PromptCacheKey: "thread-final-delta"}
	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:     "gpt-test",
		Messages:  []providers.ChatMessage{{Role: "user", Content: "first"}},
		CacheHint: cache,
	})
	if err != nil {
		t.Fatalf("first StreamChat: %v", err)
	}
	if err := drainStream(ch); err != nil {
		t.Fatalf("first stream: %v", err)
	}

	ch, err = client.StreamChat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "first"},
			{
				Role:              "assistant",
				Content:           "probe-one",
				Phase:             providers.MessagePhaseFinalAnswer,
				ProviderItemID:    "msg_1",
				ProviderItemModel: "gpt-test",
				ReasoningBlocks: []providers.ReasoningBlock{{
					Type: "reasoning",
					Data: reasoningItem,
				}},
			},
			{Role: "user", Content: "second"},
		},
		CacheHint: cache,
	})
	if err != nil {
		t.Fatalf("second StreamChat: %v", err)
	}
	secondStates, err := drainStreamProviderStates(ch)
	if err != nil {
		t.Fatalf("second stream: %v", err)
	}
	if len(secondStates) != 1 ||
		secondStates[0].ReplayMode != "previous_response_id" ||
		!secondStates[0].PreviousResponseIDUsed ||
		secondStates[0].InputItems != 1 ||
		secondStates[0].FullInputItems != 4 ||
		secondStates[0].DeltaInputItems != 1 {
		t.Fatalf("unexpected final-answer provider state: %+v", secondStates)
	}

	<-requests
	second := <-requests
	if second["previous_response_id"] != "resp_1" {
		t.Fatalf("final-answer request previous_response_id = %#v; body=%#v", second["previous_response_id"], second)
	}
	secondInput := second["input"].([]any)
	if len(secondInput) != 1 {
		t.Fatalf("final-answer request should send only delta input, got %#v", secondInput)
	}
}

func TestResponsesStreamChatWebSocket_PreservesDeltaWithHiddenModelContext(t *testing.T) {
	requests := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		for i := 0; i < 2; i++ {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				t.Errorf("read request %d: %v", i+1, err)
				return
			}
			if typ != websocket.MessageText {
				t.Errorf("request %d type = %v", i+1, typ)
				return
			}
			var body map[string]any
			if err := json.Unmarshal(data, &body); err != nil {
				t.Errorf("decode request %d: %v", i+1, err)
				return
			}
			requests <- body

			if i == 0 {
				writeWSEvent(t, ctx, conn, `{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.added","item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","status":"in_progress"},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_text.delta","delta":"first answer","item_id":"msg_1","output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.done","item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"first answer"}]},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":8,"output_tokens":2}}}`)
			} else {
				writeWSEvent(t, ctx, conn, `{"type":"response.created","response":{"id":"resp_2","status":"in_progress"}}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.added","item":{"id":"msg_2","type":"message","role":"assistant","phase":"final_answer","status":"in_progress"},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_text.delta","delta":"second answer","item_id":"msg_2","output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.done","item":{"id":"msg_2","type":"message","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"second answer"}]},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.completed","response":{"id":"resp_2","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`)
			}
		}
	}))
	defer server.Close()

	store := false
	client, err := New(ClientConfig{
		BaseURL:                 server.URL,
		WireAPI:                 "responses",
		APIKey:                  "test-key",
		ResponsesStore:          &store,
		ResponsesWebSocket:      true,
		ResponsesWebSocketCache: NewResponsesWebSocketCache(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hiddenContext := providers.ChatMessage{
		Role:    "user",
		Name:    wuucontext.SystemReminderMessageName,
		Content: "<system-reminder>\n# Environment\n- CWD: /tmp/project\n</system-reminder>",
		Hidden:  true,
	}
	cache := &providers.CacheHint{PromptCacheKey: "thread-hidden-context"}
	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:     "gpt-test",
		Messages:  []providers.ChatMessage{{Role: "user", Content: "first"}, hiddenContext},
		CacheHint: cache,
	})
	if err != nil {
		t.Fatalf("first StreamChat: %v", err)
	}
	if err := drainStream(ch); err != nil {
		t.Fatalf("first stream: %v", err)
	}

	ch, err = client.StreamChat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "first"},
			hiddenContext,
			{
				Role:              "assistant",
				Content:           "first answer",
				Phase:             providers.MessagePhaseFinalAnswer,
				ProviderItemID:    "msg_1",
				ProviderItemModel: "gpt-test",
			},
			{Role: "user", Content: "second"},
		},
		CacheHint: cache,
	})
	if err != nil {
		t.Fatalf("second StreamChat: %v", err)
	}
	if err := drainStream(ch); err != nil {
		t.Fatalf("second stream: %v", err)
	}

	first := <-requests
	firstInput := first["input"].([]any)
	if _, exists := first["previous_response_id"]; exists {
		t.Fatalf("first request must be full context: %#v", first)
	}
	if len(firstInput) != 2 {
		t.Fatalf("first request should include user input and hidden context, got %#v", firstInput)
	}
	hiddenInput, ok := firstInput[1].(map[string]any)
	if !ok || hiddenInput["role"] != "user" {
		t.Fatalf("hidden context serialized unexpectedly: %#v", firstInput[1])
	}

	second := <-requests
	if second["previous_response_id"] != "resp_1" {
		t.Fatalf("second request previous_response_id = %#v; body=%#v", second["previous_response_id"], second)
	}
	secondInput := second["input"].([]any)
	if len(secondInput) != 1 {
		t.Fatalf("second request should send only new user input, got %#v", secondInput)
	}
	delta, ok := secondInput[0].(map[string]any)
	if !ok || delta["role"] != "user" {
		t.Fatalf("unexpected delta input: %#v", secondInput[0])
	}
}

func TestResponsesStreamChatWebSocket_DeltaWithRefreshedHiddenContextAcrossTurns(t *testing.T) {
	requests := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		for i := 0; i < 2; i++ {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				t.Errorf("read request %d: %v", i+1, err)
				return
			}
			if typ != websocket.MessageText {
				t.Errorf("request %d type = %v", i+1, typ)
				return
			}
			var body map[string]any
			if err := json.Unmarshal(data, &body); err != nil {
				t.Errorf("decode request %d: %v", i+1, err)
				return
			}
			requests <- body

			if i == 0 {
				writeWSEvent(t, ctx, conn, `{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.added","item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","status":"in_progress"},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_text.delta","delta":"first answer","item_id":"msg_1","output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.done","item":{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"first answer"}]},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":8,"output_tokens":2}}}`)
			} else {
				writeWSEvent(t, ctx, conn, `{"type":"response.created","response":{"id":"resp_2","status":"in_progress"}}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.added","item":{"id":"msg_2","type":"message","role":"assistant","phase":"final_answer","status":"in_progress"},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_text.delta","delta":"second answer","item_id":"msg_2","output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.done","item":{"id":"msg_2","type":"message","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"second answer"}]},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.completed","response":{"id":"resp_2","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`)
			}
		}
	}))
	defer server.Close()

	store := false
	client, err := New(ClientConfig{
		BaseURL:                 server.URL,
		WireAPI:                 "responses",
		APIKey:                  "test-key",
		ResponsesStore:          &store,
		ResponsesWebSocket:      true,
		ResponsesWebSocketCache: NewResponsesWebSocketCache(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	oldContext := providers.ChatMessage{
		Role:    "user",
		Name:    wuucontext.SystemReminderMessageName,
		Content: "<system-reminder>\n# Environment\nState: old\n</system-reminder>",
		Hidden:  true,
	}
	newContext := providers.ChatMessage{
		Role:    "user",
		Name:    wuucontext.SystemReminderMessageName,
		Content: "<system-reminder>\n# Environment\nState: new\n</system-reminder>",
		Hidden:  true,
	}
	cache := &providers.CacheHint{PromptCacheKey: "thread-refresh-hidden-context"}
	ch, err := client.StreamChat(context.Background(), providers.ChatRequest{
		Model:     "gpt-test",
		Messages:  []providers.ChatMessage{{Role: "user", Content: "first"}, oldContext},
		CacheHint: cache,
	})
	if err != nil {
		t.Fatalf("first StreamChat: %v", err)
	}
	if err := drainStream(ch); err != nil {
		t.Fatalf("first stream: %v", err)
	}

	ch, err = client.StreamChat(context.Background(), providers.ChatRequest{
		Model: "gpt-test",
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "first"},
			{
				Role:              "assistant",
				Content:           "first answer",
				Phase:             providers.MessagePhaseFinalAnswer,
				ProviderItemID:    "msg_1",
				ProviderItemModel: "gpt-test",
			},
			{Role: "user", Content: "second"},
			newContext,
		},
		CacheHint: cache,
	})
	if err != nil {
		t.Fatalf("second StreamChat: %v", err)
	}
	if err := drainStream(ch); err != nil {
		t.Fatalf("second stream: %v", err)
	}

	first := <-requests
	if _, exists := first["previous_response_id"]; exists {
		t.Fatalf("first request must be full context: %#v", first)
	}

	second := <-requests
	if second["previous_response_id"] != "resp_1" {
		t.Fatalf("second request previous_response_id = %#v; body=%#v", second["previous_response_id"], second)
	}
	secondInput := second["input"].([]any)
	if len(secondInput) != 2 {
		t.Fatalf("second request should send only new user input and refreshed context, got %#v", secondInput)
	}
	rawSecondInput, err := json.Marshal(secondInput)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawSecondInput), "second") || !strings.Contains(string(rawSecondInput), "State: new") {
		t.Fatalf("second delta missing new turn or refreshed context: %s", rawSecondInput)
	}
	if strings.Contains(string(rawSecondInput), "State: old") || strings.Contains(string(rawSecondInput), "first answer") {
		t.Fatalf("second delta should not resend old context or assistant answer: %s", rawSecondInput)
	}
}

func TestResponsesStreamChatWebSocket_AgentLoopPreservesDeltaWithChangingHiddenContext(t *testing.T) {
	requests := make(chan map[string]any, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		for i := 0; i < 2; i++ {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				t.Errorf("read request %d: %v", i+1, err)
				return
			}
			if typ != websocket.MessageText {
				t.Errorf("request %d type = %v", i+1, typ)
				return
			}
			var body map[string]any
			if err := json.Unmarshal(data, &body); err != nil {
				t.Errorf("decode request %d: %v", i+1, err)
				return
			}
			requests <- body

			if i == 0 {
				writeWSEvent(t, ctx, conn, `{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","status":"in_progress","arguments":"","call_id":"call_1","name":"read_file"},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.function_call_arguments.done","arguments":"{\"path\":\"README.md\"}","item_id":"fc_1","output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.done","item":{"id":"fc_1","type":"function_call","status":"completed","arguments":"{\"path\":\"README.md\"}","call_id":"call_1","name":"read_file"},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.done","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2}}}`)
			} else {
				writeWSEvent(t, ctx, conn, `{"type":"response.created","response":{"id":"resp_2","status":"in_progress"}}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.added","item":{"id":"msg_2","type":"message","role":"assistant","phase":"final_answer","status":"in_progress"},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_text.delta","delta":"done","item_id":"msg_2","output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.output_item.done","item":{"id":"msg_2","type":"message","role":"assistant","phase":"final_answer","status":"completed","content":[{"type":"output_text","text":"done"}]},"output_index":0}`)
				writeWSEvent(t, ctx, conn, `{"type":"response.completed","response":{"id":"resp_2","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":1}}}`)
			}
		}
	}))
	defer server.Close()

	store := false
	client, err := New(ClientConfig{
		BaseURL:                 server.URL,
		WireAPI:                 "responses",
		APIKey:                  "test-key",
		Headers:                 map[string]string{"OpenAI-Beta": "responses=experimental"},
		ResponsesStore:          &store,
		ResponsesWebSocket:      true,
		ResponsesWebSocketCache: NewResponsesWebSocketCache(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	contextCalls := 0
	runner := agent.StreamRunner{
		Client:         client,
		Model:          "gpt-test",
		Tools:          &webSocketAgentLoopTools{},
		PromptCacheKey: "thread-agent-hidden-context",
		BeforeRequestContext: func() []agent.ContextSegment {
			contextCalls++
			env := wuucontext.Block{
				Kind:    wuucontext.BlockEnvironment,
				Title:   "Runtime environment",
				Source:  "runtime.snapshot",
				Content: "State: step " + strconv.Itoa(contextCalls),
			}
			return agent.RequestOnlyContextMessages([]providers.ChatMessage{
				hiddenReminderForWebSocketAgentTest(env),
			})
		},
	}
	res, err := runner.RunWithCallback(context.Background(), []providers.ChatMessage{{Role: "user", Content: "read README"}}, nil)
	if err != nil {
		t.Fatalf("RunWithCallback: %v", err)
	}
	if res.Content != "done" {
		t.Fatalf("unexpected final content %q", res.Content)
	}

	first := <-requests
	if _, exists := first["previous_response_id"]; exists {
		t.Fatalf("first request must be full context: %#v", first)
	}
	firstInputRaw, err := json.Marshal(first["input"])
	if err != nil {
		t.Fatal(err)
	}
	firstInput := string(firstInputRaw)
	if !strings.Contains(firstInput, "State: step 1") {
		t.Fatalf("first request missing request-only context: %s", firstInput)
	}

	second := <-requests
	if second["previous_response_id"] != "resp_1" {
		t.Fatalf("second request previous_response_id = %#v; body=%#v", second["previous_response_id"], second)
	}
	secondInputRaw, err := json.Marshal(second["input"])
	if err != nil {
		t.Fatal(err)
	}
	secondInput := string(secondInputRaw)
	if !strings.Contains(secondInput, "function_call_output") {
		t.Fatalf("second delta missing tool result: %s", secondInput)
	}
	if !strings.Contains(secondInput, "State: step 2") {
		t.Fatalf("second delta missing changed environment context: %s", secondInput)
	}
	if strings.Contains(secondInput, "State: step 1") {
		t.Fatalf("second delta re-sent stale request-only context: %s", secondInput)
	}
}

type webSocketAgentLoopTools struct{}

func (t *webSocketAgentLoopTools) Definitions() []providers.ToolDefinition {
	return []providers.ToolDefinition{{
		Name:        "read_file",
		Description: "read file",
		InputSchema: map[string]any{"type": "object"},
	}}
}

func (t *webSocketAgentLoopTools) Execute(_ context.Context, call providers.ToolCall) (string, error) {
	return "contents", nil
}

func hiddenReminderForWebSocketAgentTest(block wuucontext.Block) providers.ChatMessage {
	return providers.ChatMessage{
		Role:    "user",
		Name:    wuucontext.SystemReminderBlockMessageName(block, 0),
		Content: wuucontext.FormatSystemReminderBlocks(block),
		Hidden:  true,
	}
}

func writeWSEvent(t *testing.T, ctx context.Context, conn *websocket.Conn, data string) {
	t.Helper()
	if err := conn.Write(ctx, websocket.MessageText, []byte(data)); err != nil {
		t.Fatalf("write websocket event: %v", err)
	}
}

func drainStream(ch <-chan providers.StreamEvent) error {
	for ev := range ch {
		if ev.Type == providers.EventError {
			if ev.Error != nil {
				return ev.Error
			}
			return context.Canceled
		}
	}
	return nil
}

func drainStreamProviderStates(ch <-chan providers.StreamEvent) ([]providers.ProviderStateSummary, error) {
	var states []providers.ProviderStateSummary
	for ev := range ch {
		if ev.Type == providers.EventProviderState && ev.ProviderState != nil {
			states = append(states, *ev.ProviderState)
			continue
		}
		if ev.Type == providers.EventError {
			if ev.Error != nil {
				return states, ev.Error
			}
			return states, context.Canceled
		}
	}
	return states, nil
}
