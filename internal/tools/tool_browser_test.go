package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestToolkit_BrowserToolHiddenWithoutBridge(t *testing.T) {
	t.Setenv("WUU_BROWSER_BRIDGE_URL", "")

	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New toolkit: %v", err)
	}
	if _, ok := kit.ToolInfo("browser"); ok {
		t.Fatal("browser tool should be hidden without a browser bridge URL")
	}
}

func TestToolkit_BrowserToolCallsBridge(t *testing.T) {
	var gotOrigin string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOrigin = r.Header.Get("Origin")
		if r.URL.Path != "/browser-bridge/tabs/target-1/navigate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tab":{"targetId":"target-1","url":"http://localhost:5173"}}`))
	}))
	defer server.Close()
	t.Setenv("WUU_BROWSER_BRIDGE_URL", server.URL+"/browser-bridge")

	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New toolkit: %v", err)
	}

	result, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "browser",
		Arguments: `{"action":"navigate","target_id":"target-1","url":"http://localhost:5173"}`,
	})
	if err != nil {
		t.Fatalf("browser execute: %v", err)
	}
	if gotOrigin != server.URL {
		t.Fatalf("Origin = %q, want %q", gotOrigin, server.URL)
	}
	if gotBody["url"] != "http://localhost:5173" {
		t.Fatalf("body url = %#v", gotBody["url"])
	}
	if !strings.Contains(result, `"action":"navigate"`) {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestToolkit_BrowserToolRejectsRemoteBridgeURL(t *testing.T) {
	t.Setenv("WUU_BROWSER_BRIDGE_URL", "https://example.com/browser-bridge")

	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New toolkit: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "browser",
		Arguments: `{"action":"list_tabs"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "must be loopback") {
		t.Fatalf("expected loopback error, got %v", err)
	}
}

func TestToolkit_BrowserToolClassifiesReadAndWriteActions(t *testing.T) {
	t.Setenv("WUU_BROWSER_BRIDGE_URL", "http://127.0.0.1:9105/browser-bridge")

	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New toolkit: %v", err)
	}

	readInfo, ok := kit.ToolMetadata(providers.ToolCall{
		Name:      "browser",
		Arguments: `{"action":"screenshot","target_id":"target-1"}`,
	})
	if !ok {
		t.Fatal("missing browser metadata")
	}
	if !readInfo.ReadOnly || readInfo.Risk != string(ToolRiskMedium) {
		t.Fatalf("unexpected read metadata: %+v", readInfo)
	}

	writeInfo, ok := kit.ToolMetadata(providers.ToolCall{
		Name:      "browser",
		Arguments: `{"action":"click","target_id":"target-1","x":1,"y":2}`,
	})
	if !ok {
		t.Fatal("missing browser metadata")
	}
	if writeInfo.ReadOnly || writeInfo.Risk != string(ToolRiskHigh) {
		t.Fatalf("unexpected write metadata: %+v", writeInfo)
	}
}
