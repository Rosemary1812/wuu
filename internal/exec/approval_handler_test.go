package exec

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
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

func approvalChainTestRequest(t *testing.T) ServerRequest {
	t.Helper()
	params, err := json.Marshal(appserver.ToolApprovalRequest{
		ID:              "approval-1",
		ToolName:        "bash",
		ArgumentsSHA256: "sha-1",
		ApprovalKey:     "bash:sha-1",
		ApprovalRef:     "/sessions/s1/approvals/approval-1.json",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return ServerRequest{
		ID:     json.RawMessage(`"server-1"`),
		Method: appserver.MethodToolApprovalRequest,
		Params: params,
	}
}

func TestDefaultRequestHandlerDeniesApprovalsWithReason(t *testing.T) {
	// The prompt is opt-in, so the default handler never blocks even when
	// the test process has a controlling terminal.
	handler, err := newServerRequestHandler(Options{})
	if err != nil {
		t.Fatalf("newServerRequestHandler: %v", err)
	}
	if handler == nil {
		t.Fatal("exec without approval handler should still answer approval requests")
	}

	result := handler(context.Background(), approvalChainTestRequest(t))
	if result.Error != nil {
		t.Fatalf("approval request should get a decision, not a protocol error: %+v", result.Error)
	}
	response := decodeApprovalHandlerResult(t, result.Result)
	if response.Decision != "denied" {
		t.Fatalf("decision = %q, want denied", response.Decision)
	}
	if response.Reason == "" {
		t.Fatal("denied decision must carry a model-facing reason")
	}
	if !strings.Contains(response.Reason, "--approve bash:sha-1") {
		t.Fatalf("denied reason should carry the rerun grant recipe, got %q", response.Reason)
	}

	other := handler(context.Background(), ServerRequest{
		ID:     json.RawMessage(`"server-2"`),
		Method: "some/other/request",
	})
	if other.Error == nil || other.Error.Code != "non_interactive_unavailable" {
		t.Fatalf("non-approval requests should keep the protocol error, got %+v", other)
	}
}

func TestApprovalGrantsApproveMatchingRequestForSession(t *testing.T) {
	for name, tc := range map[string]struct {
		grants []string
		want   string
	}{
		"key":  {[]string{"bash:sha-1"}, "approved_for_session"},
		"id":   {[]string{"approval-1"}, "approved_for_session"},
		"sha":  {[]string{"sha-1"}, "approved_for_session"},
		"ref":  {[]string{"approval-1.json"}, "approved_for_session"},
		"miss": {[]string{"bash:other"}, "denied"},
	} {
		handler, err := newServerRequestHandler(Options{Approvals: tc.grants})
		if err != nil {
			t.Fatalf("%s: newServerRequestHandler: %v", name, err)
		}
		result := handler(context.Background(), approvalChainTestRequest(t))
		if result.Error != nil {
			t.Fatalf("%s: unexpected protocol error: %+v", name, result.Error)
		}
		response := decodeApprovalHandlerResult(t, result.Result)
		if response.Decision != tc.want {
			t.Fatalf("%s: decision = %q, want %q", name, response.Decision, tc.want)
		}
	}
}

func TestApprovalResponseForAnswer(t *testing.T) {
	for answer, want := range map[string]string{
		"y":      "approved",
		"YES\n":  "approved",
		"a":      "approved_for_session",
		"always": "approved_for_session",
		"n":      "denied",
		"":       "denied",
		"junk":   "denied",
	} {
		if got := approvalResponseForAnswer(answer); got.Decision != want {
			t.Fatalf("answer %q: decision = %q, want %q", answer, got.Decision, want)
		}
	}
}

func TestServerRequestHandlerRejectsPromptWithExternalHandler(t *testing.T) {
	if _, err := newServerRequestHandler(Options{ApprovalHandler: "approve.sh", ApprovalsMode: ApprovalsModePrompt}); err == nil {
		t.Fatal("prompt mode combined with an external handler should be rejected")
	}
	if _, err := newServerRequestHandler(Options{ApprovalSocket: "/tmp/a.sock", ApprovalsMode: ApprovalsModeAuto}); err == nil {
		t.Fatal("auto mode combined with an approval socket should be rejected")
	}
	if _, err := newServerRequestHandler(Options{ApprovalHandler: "approve.sh", ApprovalsMode: ApprovalsModeStrict}); err != nil {
		t.Fatalf("strict mode with a handler is coherent, got %v", err)
	}
}
