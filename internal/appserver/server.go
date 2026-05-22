package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
)

const ProtocolVersion = "wuu-app-server/v0.1"

var errShutdown = errors.New("app-server shutdown requested")

type request struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type response struct {
	Type   string          `json:"type"`
	ID     json.RawMessage `json:"id,omitempty"`
	Result any             `json:"result,omitempty"`
	Error  *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type notification struct {
	Type   string `json:"type"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type threadState struct {
	ID      string
	History []providers.ChatMessage

	mu          sync.Mutex
	running     bool
	currentTurn string
	cancel      context.CancelFunc
}

type Server struct {
	rt      *runtime.Session
	out     io.Writer
	writeMu sync.Mutex

	mu      sync.Mutex
	threads map[string]*threadState
}

func New(rt *runtime.Session, out io.Writer) *Server {
	return &Server{
		rt:      rt,
		out:     out,
		threads: make(map[string]*threadState),
	}
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
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return s.writeResponse(nil, nil, fmt.Errorf("parse request: %w", err))
	}
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "config/read":
		return s.handleConfigRead(req)
	case "thread/start":
		return s.handleThreadStart(req)
	case "thread/list":
		return s.handleThreadList(req)
	case "turn/start":
		return s.handleTurnStart(ctx, req)
	case "turn/interrupt":
		return s.handleTurnInterrupt(req)
	case "shutdown":
		if err := s.writeResponse(req.ID, map[string]any{"ok": true}, nil); err != nil {
			return err
		}
		return errShutdown
	default:
		return s.writeResponse(req.ID, nil, fmt.Errorf("unknown method %q", req.Method))
	}
}

func (s *Server) handleInitialize(req request) error {
	return s.writeResponse(req.ID, map[string]any{
		"protocol_version": ProtocolVersion,
		"provider":         s.rt.ProviderName,
		"model":            s.rt.Model,
		"workspace_root":   s.rt.RootDir,
	}, nil)
}

func (s *Server) handleConfigRead(req request) error {
	return s.writeResponse(req.ID, map[string]any{
		"provider":       s.rt.ProviderName,
		"model":          s.rt.Model,
		"config_path":    s.rt.ConfigPath,
		"workspace_root": s.rt.RootDir,
		"session_dir":    s.rt.SessionDir,
	}, nil)
}

func (s *Server) handleThreadStart(req request) error {
	id := session.NewID()
	history := make([]providers.ChatMessage, 0, 1)
	if prompt := strings.TrimSpace(s.rt.StreamRunner.SystemPrompt); prompt != "" {
		history = append(history, providers.ChatMessage{Role: "system", Content: prompt})
	}
	th := &threadState{ID: id, History: history}

	s.mu.Lock()
	s.threads[id] = th
	s.mu.Unlock()

	s.rt.SetSessionID(id)
	if err := s.writeResponse(req.ID, map[string]any{"thread_id": id}, nil); err != nil {
		return err
	}
	return s.writeNotification("thread/started", map[string]any{
		"thread_id": id,
	})
}

func (s *Server) handleThreadList(req request) error {
	s.mu.Lock()
	threads := make([]map[string]any, 0, len(s.threads))
	for _, th := range s.threads {
		th.mu.Lock()
		threads = append(threads, map[string]any{
			"thread_id":     th.ID,
			"message_count": len(th.History),
			"running":       th.running,
			"current_turn":  th.currentTurn,
		})
		th.mu.Unlock()
	}
	s.mu.Unlock()
	return s.writeResponse(req.ID, map[string]any{"threads": threads}, nil)
}

type turnStartParams struct {
	ThreadID string `json:"thread_id"`
	Prompt   string `json:"prompt"`
}

func (s *Server) handleTurnStart(ctx context.Context, req request) error {
	var params turnStartParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	params.ThreadID = strings.TrimSpace(params.ThreadID)
	params.Prompt = strings.TrimSpace(params.Prompt)
	if params.ThreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if params.Prompt == "" {
		return s.writeResponse(req.ID, nil, errors.New("prompt is required"))
	}
	th := s.thread(params.ThreadID)
	if th == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q not found", params.ThreadID))
	}

	turnID := session.NewID()
	turnCtx, cancel := context.WithCancel(ctx)
	userMsg := providers.ChatMessage{Role: "user", Content: params.Prompt}

	th.mu.Lock()
	if th.running {
		th.mu.Unlock()
		cancel()
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q already has a running turn", params.ThreadID))
	}
	history := append([]providers.ChatMessage(nil), th.History...)
	history = append(history, userMsg)
	th.History = history
	th.running = true
	th.currentTurn = turnID
	th.cancel = cancel
	th.mu.Unlock()

	if err := s.writeResponse(req.ID, map[string]any{"turn_id": turnID}, nil); err != nil {
		cancel()
		return err
	}
	if err := s.writeNotification("turn/started", map[string]any{
		"thread_id": params.ThreadID,
		"turn_id":   turnID,
	}); err != nil {
		cancel()
		return err
	}

	go s.runTurn(turnCtx, th, turnID, history)
	return nil
}

type turnInterruptParams struct {
	ThreadID string `json:"thread_id"`
}

func (s *Server) handleTurnInterrupt(req request) error {
	var params turnInterruptParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	th := s.thread(strings.TrimSpace(params.ThreadID))
	if th == nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q not found", params.ThreadID))
	}
	th.mu.Lock()
	cancel := th.cancel
	th.mu.Unlock()
	if cancel == nil {
		return s.writeResponse(req.ID, nil, errors.New("thread has no running turn"))
	}
	cancel()
	return s.writeResponse(req.ID, map[string]any{"ok": true}, nil)
}

func (s *Server) runTurn(ctx context.Context, th *threadState, turnID string, history []providers.ChatMessage) {
	notify := func(method string, params any) {
		_ = s.writeNotification(method, params)
	}
	res, err := s.rt.StreamRunner.RunWithCallback(ctx, history, func(ev providers.StreamEvent) {
		notify("turn/event", map[string]any{
			"thread_id": th.ID,
			"turn_id":   turnID,
			"event":     sanitizeStreamEvent(ev),
		})
	})

	th.mu.Lock()
	if res.HistoryRewritten {
		th.History = append([]providers.ChatMessage(nil), res.NewMessages...)
	} else {
		th.History = append(th.History, res.NewMessages...)
	}
	th.running = false
	th.currentTurn = ""
	th.cancel = nil
	th.mu.Unlock()

	if err != nil {
		notify("turn/error", map[string]any{
			"thread_id": th.ID,
			"turn_id":   turnID,
			"error":     err.Error(),
		})
		return
	}
	notify("turn/completed", map[string]any{
		"thread_id":     th.ID,
		"turn_id":       turnID,
		"content":       res.Content,
		"input_tokens":  res.InputTokens,
		"output_tokens": res.OutputTokens,
	})
}

func (s *Server) thread(id string) *threadState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threads[id]
}

func sanitizeStreamEvent(ev providers.StreamEvent) map[string]any {
	out := map[string]any{
		"type":      ev.Type,
		"content":   ev.Content,
		"truncated": ev.Truncated,
	}
	if ev.Message != nil {
		out["message"] = ev.Message
	}
	if ev.ToolCall != nil {
		out["tool_call"] = ev.ToolCall
	}
	if ev.ToolResult != "" {
		out["tool_result"] = ev.ToolResult
	}
	if ev.Usage != nil {
		out["usage"] = ev.Usage
	}
	if ev.StopReason != "" {
		out["stop_reason"] = ev.StopReason
	}
	if ev.Error != nil {
		out["error"] = ev.Error.Error()
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
	resp := response{Type: "response", ID: id, Result: result}
	if err != nil {
		resp.Result = nil
		resp.Error = &responseError{
			Code:    "error",
			Message: err.Error(),
		}
	}
	return s.writeJSON(resp)
}

func (s *Server) writeNotification(method string, params any) error {
	return s.writeJSON(notification{
		Type:   "notification",
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
