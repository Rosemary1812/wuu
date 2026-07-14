package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SSETransport communicates with an MCP server over Server-Sent Events.
type SSETransport struct {
	endpoint  string
	client    *http.Client
	headers   map[string]string
	reader    *bufio.Reader
	resp      *http.Response
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	closeErr  error
}

func newSSETransport(ctx context.Context, endpoint string, headers map[string]string) (*SSETransport, error) {
	client := newSSEHTTPClient()
	transportCtx, cancel := context.WithCancel(context.Background())
	stopCallerCancel := context.AfterFunc(ctx, cancel)
	req, err := http.NewRequestWithContext(transportCtx, "GET", endpoint, nil)
	if err != nil {
		stopCallerCancel()
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		stopCallerCancel()
		cancel()
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, fmt.Errorf("sse connect GET %s: %w", endpoint, contextErr)
		}
		return nil, fmt.Errorf("sse connect GET %s: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		stopCallerCancel()
		cancel()
		resp.Body.Close()
		return nil, fmt.Errorf("sse connect GET %s: %s", endpoint, resp.Status)
	}
	if !stopCallerCancel() {
		cancel()
		resp.Body.Close()
		return nil, fmt.Errorf("sse connect GET %s: %w", endpoint, ctx.Err())
	}
	return &SSETransport{
		endpoint: endpoint,
		client:   client,
		headers:  cloneStringMap(headers),
		reader:   bufio.NewReader(resp.Body),
		resp:     resp,
		ctx:      transportCtx,
		cancel:   cancel,
	}, nil
}

func newSSEHTTPClient() *http.Client {
	tr, ok := http.DefaultTransport.(*http.Transport)
	if ok {
		tr = tr.Clone()
	} else {
		tr = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	// Bound the connection handshake without imposing a lifetime on the SSE
	// response body, which is expected to remain open indefinitely.
	tr.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: tr}
}

func (t *SSETransport) Send(ctx context.Context, req Request) error {
	// SSE transport typically POSTs to a message endpoint.
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	// Derive message endpoint from SSE endpoint: replace /sse with /message.
	msgURL := strings.TrimSuffix(t.endpoint, "/sse") + "/message"
	requestCtx, cancelRequest := context.WithCancel(t.ctx)
	stopCallerCancel := context.AfterFunc(ctx, cancelRequest)
	defer func() {
		stopCallerCancel()
		cancelRequest()
	}()
	hreq, err := http.NewRequestWithContext(requestCtx, "POST", msgURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/json")
	for key, value := range t.headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		hreq.Header.Set(key, value)
	}
	resp, err := t.client.Do(hreq)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sse post %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (t *SSETransport) Receive(ctx context.Context) (Response, error) {
	for {
		line, err := t.reader.ReadString('\n')
		if err != nil {
			return Response{}, err
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}
		var resp Response
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			// Some SSE endpoints wrap notifications differently.
			// Try parsing as raw JSON-RPC response.
			continue
		}
		return resp, nil
	}
}

func (t *SSETransport) Close() error {
	t.closeOnce.Do(func() {
		if t.cancel != nil {
			t.cancel()
		}
		if t.resp != nil {
			t.closeErr = t.resp.Body.Close()
		}
	})
	return t.closeErr
}
