package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// CodexWebSocketBetaTag is the OpenAI-Beta header value the ChatGPT backend
// expects for the Responses-over-WebSocket surface. Without it the upgrade
// request is rejected as a non-Responses endpoint.
//
// Mirrors pi's OPENAI_BETA_RESPONSES_WEBSOCKETS in
// thirdparty/pi/packages/ai/src/api/openai-codex-responses.ts and the
// corresponding beta tag in codex-rs responses_websocket.rs.
const CodexWebSocketBetaTag = "responses_websockets=2026-02-06"

// resolveCodexWebSocketURL converts an HTTP Responses base URL into its
// WebSocket-equivalent endpoint. The regular Responses client stores a base
// URL and appends /responses at dispatch time; the WebSocket path sends the
// upgrade directly, so it must resolve to the concrete /responses endpoint.
func resolveCodexWebSocketURL(baseURL string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return "", errors.New("codex websocket: empty base URL")
	}
	if !strings.HasSuffix(trimmed, "/responses") && !strings.Contains(trimmed, "/responses/") {
		trimmed += "/responses"
	}
	switch {
	case strings.HasPrefix(trimmed, "https://"):
		return "wss://" + strings.TrimPrefix(trimmed, "https://"), nil
	case strings.HasPrefix(trimmed, "http://"):
		return "ws://" + strings.TrimPrefix(trimmed, "http://"), nil
	case strings.HasPrefix(trimmed, "wss://"), strings.HasPrefix(trimmed, "ws://"):
		return trimmed, nil
	default:
		return "", fmt.Errorf("codex websocket: unrecognized scheme in %q", baseURL)
	}
}

// CodexWebSocketDialer bundles the connect-time options that are
// independent of any single request. Request-specific headers
// (Authorization, chatgpt-account-id, session-id, x-client-request-id)
// are passed per-call to dialCodexWebSocket so callers can keep them
// consistent with the existing static client.headers path.
type CodexWebSocketDialer struct {
	// ConnectTimeout bounds dial + TLS + upgrade handshake. Zero defers
	// to the caller's context deadline only.
	ConnectTimeout time.Duration
}

// dialCodexWebSocket opens a Responses-over-WebSocket connection to the
// ChatGPT Codex backend. It performs the standard WebSocket upgrade with
// the Codex-specific headers:
//
//   - Authorization: Bearer <token>
//   - chatgpt-account-id: <accountId>
//   - OpenAI-Beta: responses_websockets=2026-02-06 (auto-injected when absent)
//   - session-id, x-client-request-id: forwarded from the call site
//
// The returned connection must be closed by the caller; this function does
// not retain ownership. On upgrade failure the underlying http.Response
// status is included in the wrapped error so callers can distinguish 401 /
// 403 / 404 / 4xx from network-level failures.
func (d CodexWebSocketDialer) dialCodexWebSocket(
	ctx context.Context,
	wsURL string,
	headers http.Header,
) (*websocket.Conn, error) {
	if ctx == nil {
		return nil, errors.New("codex websocket: nil context")
	}
	if wsURL == "" {
		return nil, errors.New("codex websocket: empty URL")
	}
	if headers == nil {
		headers = http.Header{}
	}
	// The ChatGPT backend rejects upgrades without this beta tag. The SSE
	// Responses path uses a different tag; only the WS Responses path
	// requires this one. Callers can still override by setting the header
	// before passing it in.
	if headers.Get("OpenAI-Beta") == "" {
		headers.Set("OpenAI-Beta", CodexWebSocketBetaTag)
	}

	dialCtx := ctx
	if d.ConnectTimeout > 0 {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeout(ctx, d.ConnectTimeout)
		defer cancel()
	}

	conn, resp, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("codex websocket dial %q: status=%d: %w", wsURL, resp.StatusCode, err)
		}
		return nil, fmt.Errorf("codex websocket dial %q: %w", wsURL, err)
	}
	return conn, nil
}
