package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
)

var errShutdown = errors.New("app-server shutdown requested")

type threadState struct {
	ID               string
	ParentID         string
	AgentPath        string
	History          []providers.ChatMessage
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Title            string
	ModelProvider    string
	Model            string
	CWD              string
	ForkedFromID     string
	ForkedFromTurnID string
	ForkedFromItemID string
	PinnedAt         *time.Time
	ArchivedAt       *time.Time
	Turns            []Turn
	MemoryPath       string
	ReadOnly         bool
	// ListeningPorts is the deduped, sorted list of localhost ports the
	// agent has surfaced via the report_listening_ports tool. It is
	// reset whenever a fresh tool call reports an explicit (possibly
	// empty) list, and carried over across turns so the in-app browser
	// preview survives between tool calls.
	ListeningPorts []int
	BrowserState   ThreadBrowserState

	execRuntime *runtime.ThreadRuntime

	mu            sync.Mutex
	running       bool
	currentTurn   string
	cancel        context.CancelFunc
	pendingSteers []providers.ChatMessage

	nextItemIndex         int
	activeAgentItemID     string
	activeReasoningItemID string
	toolItems             map[string]string
}

type Server struct {
	rt      *runtime.Session
	out     io.Writer
	writeMu sync.Mutex

	mu      sync.Mutex
	threads map[string]*threadState

	pendingMu       sync.Mutex
	nextServerReqID int64
	pendingRequests map[string]chan clientResponse

	agentCompletionMu            sync.Mutex
	pendingAgentCompletionTurns  map[string][]providers.ChatMessage
	drainingAgentCompletionTurns map[string]bool

	queuedTurnMu        sync.Mutex
	pendingQueuedTurns  map[string][]queuedTurn
	drainingQueuedTurns map[string]bool
}

func New(rt *runtime.Session, out io.Writer) *Server {
	s := &Server{
		rt:              rt,
		out:             out,
		threads:         make(map[string]*threadState),
		pendingRequests: make(map[string]chan clientResponse),

		pendingAgentCompletionTurns:  make(map[string][]providers.ChatMessage),
		drainingAgentCompletionTurns: make(map[string]bool),
		pendingQueuedTurns:           make(map[string][]queuedTurn),
		drainingQueuedTurns:          make(map[string]bool),
	}
	if rt != nil {
		s.installToolApprovalReviewer(rt.Toolkit)
	}
	_ = rt
	return s
}

func RunStdio(ctx context.Context, rt *runtime.Session, in io.Reader, out io.Writer) error {
	if rt == nil {
		return errors.New("runtime session is required")
	}
	s := New(rt, out)
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := s.handleLine(ctx, []byte(line)); err != nil {
			if errors.Is(err, errShutdown) {
				return nil
			}
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read app-server input: %w", err)
	}
	return nil
}

func (s *Server) handleLine(ctx context.Context, raw []byte) error {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return s.writeResponse(nil, nil, fmt.Errorf("parse request: %w", err))
	}
	if strings.TrimSpace(req.Method) == "" {
		return s.handleClientResponse(raw)
	}
	switch req.Method {
	case MethodInitialize:
		return s.handleInitialize(req)
	case MethodConfigRead:
		return s.handleConfigRead(req)
	case MethodConfigModelUpdate:
		return s.handleConfigModelUpdate(req)
	case MethodConfigCodexModels:
		return s.handleConfigCodexModels(ctx, req)
	case MethodSkillList:
		return s.handleSkillList(req)
	case MethodGoalSnapshot:
		return s.handleGoalSnapshot(req)
	case MethodGoalWorktreeReview:
		return s.handleGoalWorktreeReview(req)
	case MethodGoalWorktreeCleanup:
		return s.handleGoalWorktreeCleanup(req)
	case MethodGoalWorktreeRollback:
		return s.handleGoalWorktreeRollback(req)
	case MethodGoalWorktreeMerge:
		return s.handleGoalWorktreeMerge(req)
	case MethodGoalApprovalResolve:
		return s.handleGoalApprovalResolve(req)
	case MethodThreadStart:
		return s.handleThreadStart(req)
	case MethodThreadResume:
		return s.handleThreadResume(req)
	case MethodThreadFork:
		return s.handleThreadFork(req)
	case MethodThreadList:
		return s.handleThreadList(req)
	case MethodThreadSearch:
		return s.handleThreadSearch(req)
	case MethodThreadPin:
		return s.handleThreadPin(req)
	case MethodThreadArchive:
		return s.handleThreadArchive(req)
	case MethodThreadRegenerateTitle:
		return s.handleThreadRegenerateTitle(ctx, req)
	case MethodTurnStart:
		return s.handleTurnStart(ctx, req)
	case MethodTurnQueue:
		return s.handleTurnQueue(req)
	case MethodTurnDequeue:
		return s.handleTurnDequeue(req)
	case MethodTurnSteer:
		return s.handleTurnSteer(req)
	case MethodTurnUnsteer:
		return s.handleTurnUnsteer(req)
	case MethodTurnInterrupt:
		return s.handleTurnInterrupt(req)
	case MethodProcessList:
		return s.handleProcessList(req)
	case MethodProcessStop:
		return s.handleProcessStop(req)
	case MethodShutdown:
		if err := s.writeResponse(req.ID, OKResult{OK: true}, nil); err != nil {
			return err
		}
		return errShutdown
	default:
		return s.writeResponse(req.ID, nil, fmt.Errorf("unknown method %q", req.Method))
	}
}

func (s *Server) handleClientResponse(raw []byte) error {
	var resp ClientResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return s.writeResponse(nil, nil, fmt.Errorf("parse response: %w", err))
	}
	key := requestIDKey(resp.ID)
	if key == "" {
		return s.writeResponse(nil, nil, errors.New("response id is required"))
	}
	s.pendingMu.Lock()
	ch := s.pendingRequests[key]
	if ch != nil {
		delete(s.pendingRequests, key)
	}
	s.pendingMu.Unlock()
	if ch == nil {
		return nil
	}
	ch <- clientResponse{result: resp.Result, err: resp.Error}
	return nil
}

func (s *Server) thread(id string) *threadState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threads[id]
}

type clientResponse struct {
	result json.RawMessage
	err    *ResponseError
}

func (s *Server) requestClient(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := s.nextServerRequestID()
	rawID := json.RawMessage(strconv.Quote(id))
	key := requestIDKey(rawID)
	ch := make(chan clientResponse, 1)

	s.pendingMu.Lock()
	s.pendingRequests[key] = ch
	s.pendingMu.Unlock()

	if err := s.writeJSON(ServerRequest{ID: rawID, Method: method, Params: params}); err != nil {
		s.pendingMu.Lock()
		delete(s.pendingRequests, key)
		s.pendingMu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.err != nil {
			return nil, errors.New(resp.err.Message)
		}
		return resp.result, nil
	case <-ctx.Done():
		s.pendingMu.Lock()
		delete(s.pendingRequests, key)
		s.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

func (s *Server) nextServerRequestID() string {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	s.nextServerReqID++
	return fmt.Sprintf("server-%d", s.nextServerReqID)
}

func requestIDKey(raw json.RawMessage) string {
	return strings.TrimSpace(string(raw))
}

func sanitizeStreamEvent(ev providers.StreamEvent) StreamEventPayload {
	out := StreamEventPayload{
		Type:      ev.Type,
		Content:   ev.Content,
		Truncated: ev.Truncated,
	}
	if ev.Message != nil {
		out.Message = ev.Message
	}
	if ev.ToolCall != nil {
		out.ToolCall = ev.ToolCall
	}
	if ev.ToolResult != "" {
		out.ToolResult = ev.ToolResult
	}
	if ev.PlanUpdate != nil {
		out.PlanUpdate = ev.PlanUpdate
	}
	if ev.RequestContext != nil {
		out.RequestContext = ev.RequestContext
	}
	if ev.Usage != nil {
		out.Usage = ev.Usage
	}
	if ev.StopReason != "" {
		out.StopReason = ev.StopReason
	}
	if ev.Error != nil {
		out.Error = ev.Error.Error()
	}
	return out
}

func decodeParams(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

func (s *Server) writeResponse(id json.RawMessage, result any, err error) error {
	resp := Response{ID: id, Result: result}
	if err != nil {
		resp.Result = nil
		resp.Error = &ResponseError{
			Code:    "error",
			Message: err.Error(),
		}
	}
	return s.writeJSON(resp)
}

func (s *Server) writeNotification(method string, params any) error {
	return s.writeJSON(Notification{
		Method: method,
		Params: params,
	})
}

func (s *Server) writeJSON(v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	enc := json.NewEncoder(s.out)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("write app-server message: %w", err)
	}
	return nil
}
