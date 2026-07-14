package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/extensions"
)

func TestManagerConfigureRecordsConfiguredAndDisabledStatuses(t *testing.T) {
	manager := NewManager()
	disabled := false
	manager.Configure(map[string]ServerConfig{
		"docs":     {Name: "docs", Command: "mcp-docs"},
		"disabled": {Name: "disabled", Command: "mcp-disabled", Enabled: &disabled},
	})

	status := manager.Status()
	if status["docs"].State != MCPServerStateConfigured || status["docs"].Connected {
		t.Fatalf("docs status = %+v, want configured", status["docs"])
	}
	if status["disabled"].State != MCPServerStateDisabled || status["disabled"].Connected {
		t.Fatalf("disabled status = %+v, want disabled", status["disabled"])
	}
}

func TestManagerNativeToolsAndGenerationTrackCatalogChanges(t *testing.T) {
	manager := NewManager()
	client := &Client{name: "docs", tools: []Tool{{Name: "search", InputSchema: []byte(`{"type":"object"}`)}}}
	manager.mu.Lock()
	manager.clients["docs"] = client
	manager.statuses["docs"] = ServerStatus{Name: "docs", State: MCPServerStateReady, Connected: true}
	manager.generation++
	manager.mu.Unlock()

	firstGeneration := manager.Generation()
	native := manager.NativeTools()
	if len(native) != 1 || native[0].Definition.Name != "search" || native[0].Client != client {
		t.Fatalf("NativeTools = %+v", native)
	}
	if native[0].Timeout <= 0 || native[0].Provenance.Kind != extensions.KindMCP {
		t.Fatalf("native metadata = %+v", native[0])
	}

	client.mu.Lock()
	client.tools = []Tool{{Name: "fetch"}}
	client.mu.Unlock()
	manager.catalogChanged("docs")
	if manager.Generation() <= firstGeneration {
		t.Fatalf("generation did not advance: %d -> %d", firstGeneration, manager.Generation())
	}
	native = manager.NativeTools()
	if len(native) != 1 || native[0].Definition.Name != "fetch" {
		t.Fatalf("stale native tools: %+v", native)
	}
}

func TestManagerConfigureRecordsAuthStatus(t *testing.T) {
	manager := NewManager()
	manager.Configure(map[string]ServerConfig{
		"headers": {Name: "headers", URL: "https://example.test/sse", Headers: map[string]string{"Authorization": "Bearer token"}},
		"oauth":   {Name: "oauth", URL: "https://example.test/sse", OAuth: &OAuthConfig{ClientID: "client"}},
		"stdio":   {Name: "stdio", Command: "mcp-docs"},
	})

	status := manager.Status()
	if status["headers"].AuthStatus != MCPAuthStatusBearerToken {
		t.Fatalf("headers auth status = %s, want bearer_token", status["headers"].AuthStatus)
	}
	if status["oauth"].AuthStatus != MCPAuthStatusNotLoggedIn {
		t.Fatalf("oauth auth status = %s, want not_logged_in", status["oauth"].AuthStatus)
	}
	if status["stdio"].AuthStatus != MCPAuthStatusUnsupported {
		t.Fatalf("stdio auth status = %s, want unsupported", status["stdio"].AuthStatus)
	}
}

func TestManagerFailedConnectRecordsFailedState(t *testing.T) {
	manager := NewManager()
	err := manager.Add(context.Background(), ServerConfig{Name: "broken", Command: ""})
	if err == nil {
		t.Fatal("expected Add to fail")
	}

	status := manager.Status()["broken"]
	if status.State != MCPServerStateFailed || status.Connected {
		t.Fatalf("unexpected failed status: %+v", status)
	}
	if status.Error == "" {
		t.Fatalf("failed status should include error: %+v", status)
	}
}

func TestManagerMarksUnexpectedClientFailure(t *testing.T) {
	server := newLegacySSEServer(t)
	manager := NewManager()
	t.Cleanup(func() { _ = manager.Close() })

	err := manager.Add(context.Background(), ServerConfig{
		Name:      "legacy",
		URL:       server.srv.URL + "/sse",
		Transport: TransportSSE,
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	initialGeneration := manager.Generation()
	if len(manager.NativeTools()) != 1 {
		t.Fatalf("NativeTools before failure = %+v", manager.NativeTools())
	}

	server.srv.CloseClientConnections()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := manager.Status()["legacy"]
		if status.State == MCPServerStateFailed && !status.Connected && status.Error != "" {
			if len(manager.NativeTools()) != 0 {
				t.Fatalf("NativeTools retained tools from failed client: %+v", manager.NativeTools())
			}
			if manager.Generation() <= initialGeneration {
				t.Fatalf("generation did not advance after failure: %d -> %d", initialGeneration, manager.Generation())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("manager did not record unexpected client failure: %+v", manager.Status()["legacy"])
}

func TestClassifyConnectErrorDetectsNeedsAuth(t *testing.T) {
	err := &RPCError{Code: 401, Message: "unauthorized"}
	if got := classifyConnectError(err); got != MCPServerStateNeedsAuth {
		t.Fatalf("classifyConnectError = %s, want needs_auth", got)
	}
}

func TestManagerRefreshRequiresConfiguredServer(t *testing.T) {
	manager := NewManager()
	err := manager.Refresh(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected missing refresh to fail")
	}
}

func TestManagerDisconnectPreservesOAuthAuthenticationState(t *testing.T) {
	manager := NewManager()
	manager.Configure(map[string]ServerConfig{
		"docs": {Name: "docs", URL: "https://example.test/mcp", OAuth: &OAuthConfig{ClientID: "client"}},
	})
	manager.mu.Lock()
	manager.statuses["docs"] = ServerStatus{Name: "docs", State: MCPServerStateStopped, AuthStatus: MCPAuthStatusOAuth}
	manager.mu.Unlock()

	if err := manager.Disconnect("docs"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	status := manager.Status()["docs"]
	if status.State != MCPServerStateStopped || status.AuthStatus != MCPAuthStatusOAuth {
		t.Fatalf("status after disconnect = %+v", status)
	}
}
