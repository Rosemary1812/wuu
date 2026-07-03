package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	osexec "os/exec"
	"path/filepath"
	"strings"

	"github.com/blueberrycongee/wuu/internal/appserver"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// Approvals modes for Options.ApprovalsMode / --approvals.
const (
	ApprovalsModeAuto   = "auto"
	ApprovalsModeStrict = "strict"
	ApprovalsModePrompt = "prompt"
)

// NormalizeApprovalsMode validates an --approvals value; empty means the
// default (auto).
func NormalizeApprovalsMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", ApprovalsModeAuto, ApprovalsModeStrict, ApprovalsModePrompt:
		return mode, nil
	default:
		return "", fmt.Errorf("approvals mode must be one of auto, strict, prompt")
	}
}

func newServerRequestHandler(opts Options) (ServerRequestHandler, error) {
	handler := strings.TrimSpace(opts.ApprovalHandler)
	socket := strings.TrimSpace(opts.ApprovalSocket)
	if handler != "" && socket != "" {
		return nil, fmt.Errorf("--approval-handler and --approval-socket cannot be used together")
	}
	mode, err := NormalizeApprovalsMode(opts.ApprovalsMode)
	if err != nil {
		return nil, err
	}
	if (handler != "" || socket != "") && mode != "" && mode != ApprovalsModeStrict {
		return nil, fmt.Errorf("--approvals %s cannot be combined with an approval handler or socket", mode)
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
	}

	// Without an external handler, exec still answers whatever approval
	// requests remain (in the default auto mode these only arise from
	// explicit require_approval rules in config), from most to least
	// specific:
	//  1. --approve grants: pre-approve exactly the named calls
	//     (matched by stable approval key, request id, or arguments
	//     hash), which is how a caller re-runs after a strict denial.
	//  2. --approvals prompt: ask the human on the controlling
	//     terminal. Opt-in only - exec is mostly driven by other
	//     agents, whose shells may hold a pty; an unanswered prompt
	//     would hang their run, so nothing ever blocks by default.
	//  3. otherwise deny with a decision (not a protocol error) whose
	//     reason tells the model to report the blocked call and tells
	//     the caller how to grant it on the next run.
	grants := newApprovalGrants(opts.Approvals)
	var prompter *ttyApprovalPrompter
	if mode == ApprovalsModePrompt {
		prompter = newTTYApprovalPrompter()
		if prompter == nil && opts.Stderr != nil {
			fmt.Fprintln(opts.Stderr, "warning: --approvals prompt requested but no controlling terminal; approvals without a matching --approve grant will be denied")
		}
	}
	return func(ctx context.Context, req ServerRequest) ServerRequestResult {
		if req.Method != appserver.MethodToolApprovalRequest {
			return ServerRequestResult{Error: &appserver.ResponseError{
				Code:    "non_interactive_unavailable",
				Message: fmt.Sprintf("non-interactive exec cannot handle app-server request %q", req.Method),
			}}
		}
		var request appserver.ToolApprovalRequest
		_ = json.Unmarshal(req.Params, &request)
		if grants.matches(request) {
			return ServerRequestResult{Result: appserver.ToolApprovalResponse{
				Decision: string(tools.ToolApprovalDecisionApprovedForSession),
				Reason:   "pre-approved by the caller via --approve",
			}}
		}
		if prompter != nil {
			if response, ok := prompter.prompt(request); ok {
				return ServerRequestResult{Result: response}
			}
		}
		return ServerRequestResult{Result: appserver.ToolApprovalResponse{
			Decision: string(tools.ToolApprovalDecisionDenied),
			Reason:   headlessApprovalDenyReason(request),
		}}
	}, nil
}

// headlessApprovalDenyReason is what the model reads when a tool call
// needs approval and nothing in this run can grant it. It must both stop
// the model from chasing approval mechanisms that do not exist here and
// carry the exact token the human caller needs to grant this call on a
// rerun.
func headlessApprovalDenyReason(request appserver.ToolApprovalRequest) string {
	reason := "approval prompts are unavailable in this unattended run; " +
		"do not retry this call or look for an approval mechanism - " +
		"use an alternative allowed by the current permissions, or finish and report the exact blocked action to the caller"
	if key := strings.TrimSpace(request.ApprovalKey); key != "" {
		reason += fmt.Sprintf("; the caller can grant exactly this call by rerunning with: wuu exec resume --last --approve %s", key)
	} else {
		reason += "; the caller can grant it by rerunning with --approval-handler or --allow-tool"
	}
	return reason
}

// approvalGrants holds the --approve tokens for this run. A token
// matches a request by its stable approval key (tool name + arguments
// hash), by the request id, by the bare arguments hash, or by the
// basename of the persisted approval artifact - whatever identifier the
// caller happened to copy out of the previous run.
type approvalGrants struct {
	tokens map[string]struct{}
}

func newApprovalGrants(tokens []string) approvalGrants {
	grants := approvalGrants{tokens: map[string]struct{}{}}
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		grants.tokens[token] = struct{}{}
	}
	return grants
}

func (g approvalGrants) matches(request appserver.ToolApprovalRequest) bool {
	if len(g.tokens) == 0 {
		return false
	}
	candidates := []string{
		request.ApprovalKey,
		request.ID,
		request.ArgumentsSHA256,
	}
	if ref := strings.TrimSpace(request.ApprovalRef); ref != "" {
		base := filepath.Base(ref)
		candidates = append(candidates, base, strings.TrimSuffix(base, filepath.Ext(base)))
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := g.tokens[candidate]; ok {
			return true
		}
	}
	return false
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
