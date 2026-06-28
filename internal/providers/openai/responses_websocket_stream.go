package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/coder/websocket"
)

// ResponsesWebSocketCache stores per-session Codex Responses WebSocket state.
// It mirrors pi's session cache: a reusable connection plus the last full
// request/response pair needed to build previous_response_id deltas.
type ResponsesWebSocketCache struct {
	mu       sync.Mutex
	sessions map[string]*responsesWebSocketSession
}

func NewResponsesWebSocketCache() *ResponsesWebSocketCache {
	return &ResponsesWebSocketCache{sessions: make(map[string]*responsesWebSocketSession)}
}

type responsesWebSocketSession struct {
	mu           sync.Mutex
	conn         *websocket.Conn
	wsURL        string
	continuation *responsesWebSocketContinuation
}

type responsesWebSocketContinuation struct {
	lastRequestRest   []byte
	lastRequestInput  []responsesInputItem
	lastResponseID    string
	lastResponseItems []responsesInputItem
}

func (c *Client) responsesStreamChatWebSocket(ctx context.Context, payload responsesRequest) (<-chan providers.StreamEvent, error) {
	if c.responsesWSCache == nil {
		return nil, errors.New("responses websocket cache is nil")
	}
	sessionID := responsesWebSocketSessionID(payload)
	if sessionID == "" {
		return nil, errors.New("responses websocket session id is empty")
	}
	wsURL, err := resolveCodexWebSocketURL(c.baseURL)
	if err != nil {
		return nil, err
	}
	session := c.responsesWSCache.session(sessionID)
	session.mu.Lock()

	conn, err := c.responsesWebSocketConnectionLocked(ctx, session, wsURL, payload.ExtraHeaders)
	if err != nil {
		session.mu.Unlock()
		return nil, err
	}

	fullPayload := payload
	requestPayload := responsesCachedWebSocketRequest(session, fullPayload)
	providers.DebugLogf("Responses websocket request: session=%q previous_response_id=%v input_items=%d full_input_items=%d",
		sessionID, strings.TrimSpace(requestPayload.PreviousResponseID) != "", len(requestPayload.Input), len(fullPayload.Input))
	body, err := marshalResponsesWebSocketCreate(requestPayload)
	if err != nil {
		session.mu.Unlock()
		return nil, err
	}
	if err := conn.Write(ctx, websocket.MessageText, body); err != nil {
		c.responsesWebSocketDropLocked(session)
		session.mu.Unlock()
		return nil, err
	}

	ch := make(chan providers.StreamEvent, 64)
	go c.readResponsesWebSocket(ctx, session, fullPayload, requestPayload, conn, ch)
	return ch, nil
}

func (c *Client) responsesWebSocketConnectionLocked(ctx context.Context, session *responsesWebSocketSession, wsURL string, extraHeaders map[string]string) (*websocket.Conn, error) {
	if session.conn != nil && session.wsURL == wsURL {
		return session.conn, nil
	}
	if session.conn != nil {
		_ = session.conn.Close(websocket.StatusNormalClosure, "reconnect")
		session.conn = nil
		session.continuation = nil
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.apiKey)
	for k, v := range c.headers {
		headers.Set(k, v)
	}
	for k, v := range extraHeaders {
		headers.Set(k, v)
	}
	headers.Set("OpenAI-Beta", CodexWebSocketBetaTag)
	conn, err := (CodexWebSocketDialer{ConnectTimeout: c.streamConfig.ConnectTimeout}).dialCodexWebSocket(ctx, wsURL, headers)
	if err != nil {
		return nil, err
	}
	session.conn = conn
	session.wsURL = wsURL
	return conn, nil
}

func (c *Client) readResponsesWebSocket(ctx context.Context, session *responsesWebSocketSession, fullPayload, requestPayload responsesRequest, conn *websocket.Conn, ch chan<- providers.StreamEvent) {
	defer close(ch)
	defer session.mu.Unlock()

	ch <- providers.StreamEvent{
		Type:          providers.EventProviderState,
		ProviderState: responsesWebSocketProviderState(fullPayload, requestPayload),
	}

	pending := newResponsesPendingTools()
	pendingReasoning := newResponsesPendingReasoning()
	var sawToolCall bool
	var currentTextPhase providers.MessagePhase
	var currentTextItemID string
	var responseID string
	var responseItems []responsesInputItem

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			c.responsesWebSocketDropLocked(session)
			ch <- providers.StreamEvent{Type: providers.EventError, Error: fmt.Errorf("read websocket stream: %w", err)}
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		providers.DebugLogf("Responses websocket raw: %s", string(data))
		var event responsesStreamEvent
		if err := json.Unmarshal(data, &event); err != nil {
			c.responsesWebSocketDropLocked(session)
			ch <- providers.StreamEvent{Type: providers.EventError, Error: fmt.Errorf("parse websocket event: %w", err)}
			return
		}

		switch event.Type {
		case "response.created":
			if event.Response != nil {
				responseID = event.Response.ID
			}

		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			pendingReasoning.appendDelta(event, event.Delta, ch)

		case "response.reasoning_summary_part.done":
			pendingReasoning.appendDelta(event, "\n\n", ch)

		case "response.output_text.delta":
			if event.Delta != "" {
				if event.ItemID != "" {
					currentTextItemID = event.ItemID
				}
				ch <- providers.StreamEvent{Type: providers.EventContentDelta, Content: event.Delta, Phase: currentTextPhase, ProviderItemID: currentTextItemID}
			}

		case "response.output_item.added":
			switch event.Item.Type {
			case "reasoning":
				pendingReasoning.start(event.Item, event.outputIndex())
			case "message":
				if event.Item.ID != "" {
					currentTextItemID = event.Item.ID
				}
				if phase := providers.NormalizeMessagePhase(event.Item.Phase); phase != "" {
					currentTextPhase = phase
					ch <- providers.StreamEvent{Type: providers.EventContentDelta, Phase: currentTextPhase, ProviderItemID: currentTextItemID}
				}
			case "function_call", "tool_search_call":
				sawToolCall = true
				pending.start(event.Item, event.outputIndex(), ch)
			}

		case "response.function_call_arguments.delta", "response.tool_search_call.arguments.delta":
			if event.Delta != "" {
				pending.appendDelta(event, ch)
			}

		case "response.function_call_arguments.done", "response.tool_search_call.arguments.done":
			pending.setArguments(event)

		case "response.output_item.done":
			if item, ok := responsesOutputItemReplayInput(event.Item); ok {
				responseItems = append(responseItems, item)
			}
			switch event.Item.Type {
			case "reasoning":
				pendingReasoning.emitDone(event, ch)
			case "message":
				if event.Item.ID != "" {
					currentTextItemID = event.Item.ID
				}
				if phase := providers.NormalizeMessagePhase(event.Item.Phase); phase != "" {
					currentTextPhase = phase
					ch <- providers.StreamEvent{Type: providers.EventContentDelta, Phase: currentTextPhase, ProviderItemID: currentTextItemID}
				}
			case "function_call", "tool_search_call":
				sawToolCall = true
				pt := pending.start(event.Item, event.outputIndex(), ch)
				pending.emitEnd(pt, event.Item.argumentsString(), ch)
			}

		case "response.completed", "response.done", "response.incomplete":
			if event.Response != nil && event.Response.ID != "" {
				responseID = event.Response.ID
			}
			pending.emitEnds(ch)
			usage, stopReason, finishReason, truncated := responsesDoneMetadata(event.Response, sawToolCall)
			responsesWebSocketStoreContinuation(session, fullPayload, responseID, responseItems)
			ch <- providers.StreamEvent{
				Type:         providers.EventDone,
				Usage:        usage,
				StopReason:   stopReason,
				FinishReason: finishReason,
				Truncated:    truncated,
			}
			return

		case "response.failed":
			c.responsesWebSocketDropLocked(session)
			if event.Response != nil && event.Response.Error != nil {
				ch <- providers.StreamEvent{Type: providers.EventError, Error: event.Response.Error.asError()}
				return
			}
			ch <- providers.StreamEvent{Type: providers.EventError, Error: errors.New("response failed")}
			return

		case "error":
			c.responsesWebSocketDropLocked(session)
			if event.Error != nil {
				ch <- providers.StreamEvent{Type: providers.EventError, Error: event.Error.asError()}
				return
			}
			ch <- providers.StreamEvent{Type: providers.EventError, Error: errors.New("response websocket error")}
			return
		}
	}
}

func responsesWebSocketProviderState(fullPayload, requestPayload responsesRequest) *providers.ProviderStateSummary {
	previous := strings.TrimSpace(requestPayload.PreviousResponseID) != ""
	replayMode := "full_request"
	deltaItems := 0
	if previous {
		replayMode = "previous_response_id"
		deltaItems = len(requestPayload.Input)
	}
	return &providers.ProviderStateSummary{
		Provider:               "openai",
		Protocol:               "responses_websocket",
		ReplayMode:             replayMode,
		PreviousResponseIDUsed: previous,
		InputItems:             len(requestPayload.Input),
		FullInputItems:         len(fullPayload.Input),
		DeltaInputItems:        deltaItems,
	}
}

func (c *Client) responsesWebSocketDropLocked(session *responsesWebSocketSession) {
	if session.conn != nil {
		_ = session.conn.Close(websocket.StatusInternalError, "stream_error")
	}
	session.conn = nil
	session.continuation = nil
}

func (c *ResponsesWebSocketCache) session(sessionID string) *responsesWebSocketSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions == nil {
		c.sessions = make(map[string]*responsesWebSocketSession)
	}
	session := c.sessions[sessionID]
	if session == nil {
		session = &responsesWebSocketSession{}
		c.sessions[sessionID] = session
	}
	return session
}

func responsesWebSocketSessionID(payload responsesRequest) string {
	if payload.ExtraHeaders != nil {
		if id := strings.TrimSpace(payload.ExtraHeaders["session-id"]); id != "" {
			return id
		}
	}
	return strings.TrimSpace(payload.PromptCacheKey)
}

func responsesCachedWebSocketRequest(session *responsesWebSocketSession, payload responsesRequest) responsesRequest {
	continuation := session.continuation
	if continuation == nil || continuation.lastResponseID == "" {
		return payload
	}
	rest, err := responsesRequestRestJSON(payload)
	if err != nil || string(rest) != string(continuation.lastRequestRest) {
		session.continuation = nil
		return payload
	}
	delta, ok := responsesCachedInputDelta(payload.Input, continuation)
	if !ok {
		session.continuation = nil
		return payload
	}
	payload.PreviousResponseID = continuation.lastResponseID
	payload.Input = delta
	return payload
}

func responsesCachedInputDelta(current []responsesInputItem, continuation *responsesWebSocketContinuation) ([]responsesInputItem, bool) {
	baseline := make([]responsesInputItem, 0, len(continuation.lastRequestInput)+len(continuation.lastResponseItems))
	baseline = append(baseline, continuation.lastRequestInput...)
	baseline = append(baseline, continuation.lastResponseItems...)
	return responsesCachedInputDeltaFromBaseline(current, baseline)
}

func responsesCachedInputDeltaFromBaseline(current, baseline []responsesInputItem) ([]responsesInputItem, bool) {
	if len(current) < len(baseline) {
		if !responsesInputCanMatchBaselineWithRefreshableContext(current, baseline) {
			return nil, false
		}
	}
	var delta []responsesInputItem
	i := 0
	for j := 0; j < len(baseline); j++ {
		base := baseline[j]
		if isRefreshableResponsesInputItem(base) {
			if i < len(current) && responsesInputItemEqual(current[i], base) {
				i++
			}
			continue
		}
		for i < len(current) && isRefreshableResponsesInputItem(current[i]) {
			delta = append(delta, current[i])
			i++
		}
		if i >= len(current) || !responsesInputItemEqual(current[i], base) {
			return nil, false
		}
		i++
	}
	if i < len(current) {
		delta = append(delta, current[i:]...)
	}
	if delta == nil {
		delta = make([]responsesInputItem, 0)
	}
	return delta, true
}

func responsesWebSocketStoreContinuation(session *responsesWebSocketSession, payload responsesRequest, responseID string, responseItems []responsesInputItem) {
	if strings.TrimSpace(responseID) == "" {
		session.continuation = nil
		return
	}
	rest, err := responsesRequestRestJSON(payload)
	if err != nil {
		session.continuation = nil
		return
	}
	session.continuation = &responsesWebSocketContinuation{
		lastRequestRest:   rest,
		lastRequestInput:  append([]responsesInputItem(nil), payload.Input...),
		lastResponseID:    responseID,
		lastResponseItems: append([]responsesInputItem(nil), responseItems...),
	}
}

func responsesRequestRestJSON(payload responsesRequest) ([]byte, error) {
	body, err := marshalResponsesRequest(payload)
	if err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return nil, err
	}
	delete(object, "input")
	delete(object, "previous_response_id")
	return json.Marshal(object)
}

func responsesInputItemsEqual(a, b []responsesInputItem) bool {
	aj, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bj, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return jsonBytesEqual(aj, bj)
}

func responsesInputItemEqual(a, b responsesInputItem) bool {
	return responsesInputItemsEqual([]responsesInputItem{a}, []responsesInputItem{b})
}

func jsonBytesEqual(a, b []byte) bool {
	var av any
	var bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return string(a) == string(b)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return string(a) == string(b)
	}
	return reflect.DeepEqual(av, bv)
}

func responsesInputCanMatchBaselineWithRefreshableContext(current, baseline []responsesInputItem) bool {
	nonRefreshable := 0
	for _, item := range baseline {
		if !isRefreshableResponsesInputItem(item) {
			nonRefreshable++
		}
	}
	return len(current) >= nonRefreshable
}

func isRefreshableResponsesInputItem(item responsesInputItem) bool {
	if !strings.EqualFold(strings.TrimSpace(item.Role), "user") {
		return false
	}
	text := strings.TrimSpace(responsesInputItemText(item))
	return wuucontext.IsSystemReminder("", text) || wuucontext.IsGoalContinuation("", text)
}

func responsesInputItemText(item responsesInputItem) string {
	switch content := item.Content.(type) {
	case string:
		return content
	case []responsesInputContentPart:
		parts := make([]string, 0, len(content))
		for _, part := range content {
			if part.Text != "" {
				parts = append(parts, part.Text)
			}
		}
		return strings.Join(parts, "\n")
	case []any:
		parts := make([]string, 0, len(content))
		for _, raw := range content {
			if part, ok := raw.(map[string]any); ok {
				if text, ok := part["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func marshalResponsesWebSocketCreate(payload responsesRequest) ([]byte, error) {
	body, err := marshalResponsesRequest(payload)
	if err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return nil, err
	}
	object["type"] = json.RawMessage(`"response.create"`)
	return json.Marshal(object)
}

func responsesOutputItemReplayInput(item responsesOutputItem) (responsesInputItem, bool) {
	switch item.Type {
	case "reasoning":
		if len(item.Raw) == 0 {
			return responsesInputItem{}, false
		}
		return responsesInputItem{Raw: append(json.RawMessage(nil), item.Raw...)}, true
	case "message":
		content, err := parseResponsesContent(item.Content)
		if err != nil || content == "" {
			return responsesInputItem{}, false
		}
		return responsesInputItem{
			Type:    "message",
			ID:      item.ID,
			Role:    "assistant",
			Status:  "completed",
			Phase:   string(providers.NormalizeMessagePhase(item.Phase)),
			Content: []responsesInputContentPart{{Type: "output_text", Text: content}},
		}, true
	case "function_call":
		return responsesInputItem{
			Type:      "function_call",
			ID:        item.ID,
			CallID:    item.CallID,
			Name:      item.Name,
			Arguments: item.argumentsString(),
		}, true
	case "tool_search_call":
		return responsesInputItem{
			Type:      "tool_search_call",
			ID:        item.ID,
			CallID:    item.CallID,
			Status:    "completed",
			Execution: "client",
			Arguments: responsesToolSearchArguments(item.argumentsString()),
		}, true
	default:
		return responsesInputItem{}, false
	}
}
