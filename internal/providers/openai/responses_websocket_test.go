package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestResolveCodexWebSocketURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"https://chatgpt.com/backend-api/codex", "wss://chatgpt.com/backend-api/codex", false},
		{"https://chatgpt.com/backend-api/codex/", "wss://chatgpt.com/backend-api/codex", false},
		{"http://localhost:8080/codex", "ws://localhost:8080/codex", false},
		{"wss://chatgpt.com/x", "wss://chatgpt.com/x", false},
		{"ws://localhost:8080/x", "ws://localhost:8080/x", false},
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