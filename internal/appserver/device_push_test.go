package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/runtime"
)

func TestDevicePushRegisterRequiresRemoteSessionRegistrar(t *testing.T) {
	var out bytes.Buffer
	srv := New(&runtime.Session{
		SessionDir: t.TempDir(),
		WuuHome:    t.TempDir(),
	}, &out)

	err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"device/push_register","params":{"token":"abc","platform":"ios"}}`))
	if err != nil {
		t.Fatalf("handleLine: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "only available from a remote device session") {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
