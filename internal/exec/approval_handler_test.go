package exec

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/blueberrycongee/wuu/internal/appserver"
)

func TestRunApprovalHandlerCommand(t *testing.T) {
	req := approvalHandlerTestRequest(t)
	result := runApprovalHandlerCommand(context.Background(), `printf '{"decision":"approved","reason":"ok"}'`, req)
	if result.Error != nil {
		t.Fatalf("handler error: %+v", result.Error)
	}
	response := decodeApprovalHandlerResult(t, result.Result)
	if response.Decision != "approved" || response.Reason != "ok" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestRunApprovalSocketRequest(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "wuu-approval-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)
	socketPath := filepath.Join(dir, "approval.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	defer listener.Close()
	defer os.Remove(socketPath)

	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		var request map[string]any
		if err := json.NewDecoder(conn).Decode(&request); err != nil {
			done <- err
			return
		}
		if request["method"] != appserver.MethodToolApprovalRequest {
			done <- errUnexpectedSocketRequest
			return
		}
		done <- json.NewEncoder(conn).Encode(map[string]any{
			"decision": "denied",
			"reason":   "test policy",
		})
	}()

	result := runApprovalSocketRequest(context.Background(), socketPath, approvalHandlerTestRequest(t))
	if result.Error != nil {
		t.Fatalf("socket handler error: %+v", result.Error)
	}
	response := decodeApprovalHandlerResult(t, result.Result)
	if response.Decision != "denied" || response.Reason != "test policy" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if err := <-done; err != nil {
		t.Fatalf("socket server: %v", err)
	}
}

var errUnexpectedSocketRequest = &approvalHandlerTestError{"unexpected socket request"}

type approvalHandlerTestError struct {
	message string
}

func (e *approvalHandlerTestError) Error() string {
	return e.message
}

func approvalHandlerTestRequest(t *testing.T) ServerRequest {
	t.Helper()
	params, err := json.Marshal(appserver.ToolApprovalRequest{ID: "approval-1", ToolName: "write_file"})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return ServerRequest{
		ID:     json.RawMessage(`"server-1"`),
		Method: appserver.MethodToolApprovalRequest,
		Params: params,
	}
}

func decodeApprovalHandlerResult(t *testing.T, result any) appserver.ToolApprovalResponse {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var response appserver.ToolApprovalResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}
