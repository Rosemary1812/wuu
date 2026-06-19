package mcp

import (
	"context"
	"testing"
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

func TestManagerRefreshRequiresConfiguredServer(t *testing.T) {
	manager := NewManager()
	err := manager.Refresh(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected missing refresh to fail")
	}
}
