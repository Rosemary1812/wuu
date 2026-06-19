package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientRefreshesToolsOnListChangedNotification(t *testing.T) {
	transport := newScriptedTransport()
	client := &Client{
		name:      "server",
		transport: transport,
		inFlight:  newInFlight(),
	}
	client.readLoop = newReadLoop(transport, client.inFlight, client.handleNotification, client.handleRequest)
	client.readLoop.Start()
	t.Cleanup(func() { _ = client.Close() })

	tools, err := client.DiscoverTools(context.Background())
	if err != nil {
		t.Fatalf("initial DiscoverTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "initial" {
		t.Fatalf("initial tools = %+v", tools)
	}

	transport.notify(Response{JSONRPC: "2.0", Method: "notifications/tools/list_changed"})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tools = client.Tools()
		if len(tools) == 2 && tools[1].Name == "refreshed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("tools did not refresh after notification: %+v", client.Tools())
}

func TestReadLoopRejectsUnsupportedServerRequest(t *testing.T) {
	transport := newScriptedTransport()
	client := &Client{
		name:      "server",
		transport: transport,
		inFlight:  newInFlight(),
	}
	client.readLoop = newReadLoop(transport, client.inFlight, client.handleNotification, client.handleRequest)
	client.readLoop.Start()
	t.Cleanup(func() { _ = client.Close() })

	transport.notify(Response{JSONRPC: "2.0", ID: 99, Method: "elicitation/create"})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sent, ok := transport.sentResponse(99); ok {
			if sent.Error == nil || !strings.Contains(sent.Error.Message, "elicitation") {
				t.Fatalf("unexpected elicitation response: %+v", sent)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("read loop did not reject server request")
}

func TestStdioEnvOverlay(t *testing.T) {
	got := mergeProcessEnv([]string{"A=1", "B=2"}, map[string]string{
		"B": "override",
		"C": "3",
	})
	values := map[string]string{}
	for _, item := range got {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	if values["A"] != "1" || values["B"] != "override" || values["C"] != "3" {
		t.Fatalf("unexpected env overlay: %+v", values)
	}
}

func TestManagerStatusIncludesConnectionFailures(t *testing.T) {
	manager := NewManager()
	err := manager.Add(context.Background(), ServerConfig{Name: "broken", Command: ""})
	if err == nil {
		t.Fatal("expected Add to fail")
	}

	status := manager.Status()["broken"]
	if status.Name != "broken" || status.Connected {
		t.Fatalf("unexpected failed status: %+v", status)
	}
	if status.Error == "" {
		t.Fatalf("failed status should include error: %+v", status)
	}
}

type scriptedTransport struct {
	mu        sync.Mutex
	listCalls int
	inbox     chan Response
	closed    chan struct{}
	sent      []Request
}

func newScriptedTransport() *scriptedTransport {
	return &scriptedTransport{
		inbox:  make(chan Response, 8),
		closed: make(chan struct{}),
	}
}

func (t *scriptedTransport) Send(_ context.Context, req Request) error {
	t.mu.Lock()
	t.sent = append(t.sent, req)
	t.mu.Unlock()
	if req.Method != "tools/list" {
		return nil
	}
	t.mu.Lock()
	t.listCalls++
	call := t.listCalls
	t.mu.Unlock()

	tools := []Tool{{Name: "initial"}}
	if call > 1 {
		tools = []Tool{{Name: "initial"}, {Name: "refreshed"}}
	}
	result, _ := json.Marshal(ListToolsResult{Tools: tools})
	t.inbox <- Response{JSONRPC: "2.0", ID: req.ID, Result: result}
	return nil
}

func (t *scriptedTransport) Receive(ctx context.Context) (Response, error) {
	select {
	case <-ctx.Done():
		return Response{}, ctx.Err()
	case <-t.closed:
		return Response{}, context.Canceled
	case resp := <-t.inbox:
		return resp, nil
	}
}

func (t *scriptedTransport) Close() error {
	select {
	case <-t.closed:
	default:
		close(t.closed)
	}
	return nil
}

func (t *scriptedTransport) notify(resp Response) {
	t.inbox <- resp
}

func (t *scriptedTransport) sentResponse(id int64) (Request, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, req := range t.sent {
		if req.ID == id {
			return req, true
		}
	}
	return Request{}, false
}
