package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	osexec "os/exec"
	"strings"

	"github.com/blueberrycongee/wuu/internal/appserver"
)

func newServerRequestHandler(opts Options) (ServerRequestHandler, error) {
	handler := strings.TrimSpace(opts.ApprovalHandler)
	socket := strings.TrimSpace(opts.ApprovalSocket)
	if handler != "" && socket != "" {
		return nil, fmt.Errorf("--approval-handler and --approval-socket cannot be used together")
	}
	switch {
	case handler != "":
		return func(ctx context.Context, req ServerRequest) ServerRequestResult {
			return runApprovalHandlerCommand(ctx, handler, req)
		}, nil
	case socket != "":
		return func(ctx context.Context, req ServerRequest) ServerRequestResult {
			return runApprovalSocketRequest(ctx, socket, req)
		}, nil
	default:
		return nil, nil
	}
}

func runApprovalHandlerCommand(ctx context.Context, command string, req ServerRequest) ServerRequestResult {
	input, err := marshalApprovalHandlerInput(req)
	if err != nil {
		return approvalHandlerError("approval_handler_input_error", err)
	}
	cmd := osexec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(input)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return approvalHandlerError("approval_handler_failed", fmt.Errorf("approval handler failed: %w", err))
	}
	result, err := normalizeApprovalHandlerOutput(stdout.Bytes())
	if err != nil {
		return approvalHandlerError("approval_handler_output_error", err)
	}
	return ServerRequestResult{Result: result}
}

func runApprovalSocketRequest(ctx context.Context, socketPath string, req ServerRequest) ServerRequestResult {
	input, err := marshalApprovalHandlerInput(req)
	if err != nil {
		return approvalHandlerError("approval_socket_input_error", err)
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return approvalHandlerError("approval_socket_connect_failed", err)
	}
	defer conn.Close()
	if _, err := conn.Write(append(input, '\n')); err != nil {
		return approvalHandlerError("approval_socket_write_failed", err)
	}
	var response json.RawMessage
	dec := json.NewDecoder(conn)
	if err := dec.Decode(&response); err != nil {
		return approvalHandlerError("approval_socket_read_failed", err)
	}
	result, err := normalizeApprovalHandlerOutput(response)
	if err != nil {
		return approvalHandlerError("approval_socket_output_error", err)
	}
	return ServerRequestResult{Result: result}
}

func marshalApprovalHandlerInput(req ServerRequest) ([]byte, error) {
	var params any
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("decode app-server request params: %w", err)
		}
	}
	data, err := json.Marshal(map[string]any{
		"id":     rawIDString(req.ID),
		"method": req.Method,
		"params": params,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal approval request: %w", err)
	}
	return data, nil
}

func normalizeApprovalHandlerOutput(data []byte) (json.RawMessage, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("approval handler returned empty output")
	}
	var envelope struct {
		Result json.RawMessage          `json:"result,omitempty"`
		Error  *appserver.ResponseError `json:"error,omitempty"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil {
		if envelope.Error != nil {
			return nil, errors.New(envelope.Error.Message)
		}
		if len(envelope.Result) > 0 {
			return append(json.RawMessage(nil), envelope.Result...), nil
		}
	}
	var response struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode approval handler output: %w", err)
	}
	if strings.TrimSpace(response.Decision) == "" {
		return nil, fmt.Errorf("approval handler output requires decision")
	}
	return append(json.RawMessage(nil), data...), nil
}

func approvalHandlerError(code string, err error) ServerRequestResult {
	msg := "approval handler failed"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		msg = err.Error()
	}
	return ServerRequestResult{Error: &appserver.ResponseError{Code: code, Message: msg}}
}
