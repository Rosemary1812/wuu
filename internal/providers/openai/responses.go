package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/blueberrycongee/wuu/internal/provideroptions"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func (c *Client) responsesChat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	payload, err := c.buildResponsesRequest(req, false)
	if err != nil {
		return providers.ChatResponse{}, err
	}

	body, err := marshalResponsesRequest(payload)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpResp, err := c.doResponsesRequest(ctx, c.httpClient, body, false)
	if err != nil {
		return providers.ChatResponse{}, err
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("read response body: %w", err)
	}

	var parsed responsesResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return providers.ChatResponse{}, fmt.Errorf("parse response JSON: %w", err)
	}
	if parsed.Error != nil {
		return providers.ChatResponse{}, parsed.Error.asError()
	}
	return parsed.asChatResponse()
}

func (c *Client) responsesStreamChat(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	payload, err := c.buildResponsesRequest(req, true)
	if err != nil {
		return nil, err
	}

	body, err := marshalResponsesRequest(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	streamClient := newStreamingHTTPClient(c.httpClient, c.streamConfig)
	resp, err := c.doSingleResponsesRequest(ctx, streamClient, body, true)
	if err != nil {
		return nil, err
	}

	ch := make(chan providers.StreamEvent, 64)
	go c.readResponsesSSE(resp, ch)
	return ch, nil
}

func (c *Client) buildResponsesRequest(req providers.ChatRequest, stream bool) (responsesRequest, error) {
	if strings.TrimSpace(req.Model) == "" {
		return responsesRequest{}, errors.New("model is required")
	}
	if len(req.Messages) == 0 {
		return responsesRequest{}, errors.New("messages is required")
	}

	normalized, err := providers.NormalizeAndValidateMessagesForModel(req.Model, req.Messages)
	if err != nil {
		return responsesRequest{}, err
	}

	instructions, messages := splitResponsesInstructions(normalized)
	input := make([]responsesInputItem, 0, len(messages))
	if tools := responsesCompactedDiscoveredTools(req.Model, normalized); len(tools) > 0 {
		input = append(input, responsesInputItem{
			Type:      "tool_search_output",
			CallID:    "wuu-compact-discovered-tools",
			Status:    "completed",
			Execution: "server",
			Tools:     tools,
		})
	}
	for _, msg := range messages {
		input = appendResponsesInputItem(input, msg, req.Model)
	}

	payload := responsesRequest{
		Model:           req.Model,
		Instructions:    instructions,
		Input:           input,
		Temperature:     req.Temperature,
		MaxOutputTokens: req.MaxTokens,
		Stream:          stream,
		Options:         provideroptions.Clone(req.ProviderOptions),
	}
	if c.responsesStore != nil {
		payload.Store = c.responsesStore
	}
	if key := responsePromptCacheKey(req.CacheHint); key != "" {
		payload.PromptCacheKey = key
	}
	if strings.TrimSpace(req.Effort) != "" {
		payload.Reasoning = &responsesReasoning{Effort: req.Effort}
	}
	if len(req.Tools) > 0 {
		discoveredToolNames := providers.DiscoveredToolNamesFromMessages(normalized)
		tools := make([]responsesToolDefinition, 0, len(req.Tools))
		for _, tool := range req.Tools {
			if shouldOmitResponsesTopLevelTool(tool, discoveredToolNames) {
				continue
			}
			tools = append(tools, responsesToolDefinitionFromProvider(req.Model, tool))
		}
		if len(tools) > 0 {
			payload.ToolChoice = "auto"
			payload.Tools = tools
		}
	}

	return payload, nil
}

func marshalResponsesRequest(payload responsesRequest) ([]byte, error) {
	if len(payload.Options) == 0 {
		return json.Marshal(payload)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	mergeResponsesProviderOptions(object, payload.Options)
	return json.Marshal(object)
}

func mergeResponsesProviderOptions(object map[string]any, options map[string]any) {
	if len(object) == 0 || len(options) == 0 {
		return
	}
	for key, value := range options {
		switch key {
		case "reasoningEffort":
			if effort, ok := value.(string); ok && strings.TrimSpace(effort) != "" {
				reasoning := ensureResponseObject(object, "reasoning")
				reasoning["effort"] = strings.TrimSpace(effort)
			}
		case "reasoningSummary":
			reasoning := ensureResponseObject(object, "reasoning")
			reasoning["summary"] = value
		case "textVerbosity":
			text := ensureResponseObject(object, "text")
			text["verbosity"] = value
		case "serviceTier":
			object["service_tier"] = value
		case "maxOutputTokens":
			object["max_output_tokens"] = value
		case "promptCacheKey":
			object["prompt_cache_key"] = value
		case "temperature":
			if _, exists := object["temperature"]; !exists {
				object["temperature"] = value
			}
		case "topP":
			object["top_p"] = value
		case "topK":
			object["top_k"] = value
		default:
			if responsesProviderOptionUnsupported(key) {
				continue
			}
			mergeProviderOptionValue(object, key, value)
		}
	}
}

func responsesProviderOptionUnsupported(key string) bool {
	switch key {
	case "toolStreaming", "thinkingConfig", "reasoningConfig", "modelParams", "gateway",
		"usage", "chat_template_args", "enable_thinking", "thinking":
		return true
	default:
		return false
	}
}

func ensureResponseObject(object map[string]any, key string) map[string]any {
	if existing, ok := object[key].(map[string]any); ok {
		return existing
	}
	next := make(map[string]any)
	object[key] = next
	return next
}

func splitResponsesInstructions(messages []providers.ChatMessage) (string, []providers.ChatMessage) {
	instructions := make([]string, 0, 1)
	input := make([]providers.ChatMessage, 0, len(messages))
	for _, msg := range messages {
		if strings.EqualFold(msg.Role, "system") {
			if text := strings.TrimSpace(msg.Content); text != "" {
				instructions = append(instructions, text)
			}
			continue
		}
		input = append(input, msg)
	}
	return strings.Join(instructions, "\n\n"), input
}

func appendResponsesInputItem(input []responsesInputItem, msg providers.ChatMessage, model string) []responsesInputItem {
	if msg.Role == "tool" {
		if isResponsesToolSearchResult(msg) {
			return append(input, responsesInputItem{
				Type:      "tool_search_output",
				CallID:    msg.ToolCallID,
				Status:    "completed",
				Execution: "client",
				Tools:     responsesToolSearchOutputTools(model, msg.Content),
			})
		}
		return append(input, responsesInputItem{
			Type:   "function_call_output",
			CallID: msg.ToolCallID,
			Output: msg.Content,
		})
	}

	if msg.Role == "assistant" {
		if strings.TrimSpace(msg.Content) != "" {
			input = append(input, responsesInputItem{
				Role:    "assistant",
				Content: msg.Content,
			})
		}
		for _, call := range msg.ToolCalls {
			if isResponsesToolSearchCall(call) {
				input = append(input, responsesInputItem{
					Type:      "tool_search_call",
					CallID:    call.ID,
					Status:    "completed",
					Execution: "client",
					Arguments: responsesToolSearchArguments(call.Arguments),
				})
				continue
			}
			input = append(input, responsesInputItem{
				Type:      "function_call",
				CallID:    call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			})
		}
		if strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
			input = append(input, responsesInputItem{Role: "assistant", Content: ""})
		}
		return input
	}

	return append(input, responsesInputItem{
		Role:    msg.Role,
		Content: responsesMessageContent(msg),
	})
}

func responsesMessageContent(msg providers.ChatMessage) any {
	if (len(msg.Images) == 0 && len(msg.Files) == 0) || !strings.EqualFold(msg.Role, "user") {
		return msg.Content
	}

	parts := make([]responsesInputContentPart, 0, len(msg.Images)+len(msg.Files)+1)
	if strings.TrimSpace(msg.Content) != "" {
		parts = append(parts, responsesInputContentPart{
			Type: "input_text",
			Text: msg.Content,
		})
	}
	for _, image := range msg.Images {
		data := strings.TrimSpace(image.Data)
		if data == "" {
			continue
		}
		mediaType := strings.TrimSpace(image.MediaType)
		if mediaType == "" {
			mediaType = "image/png"
		}
		parts = append(parts, responsesInputContentPart{
			Type:     "input_image",
			ImageURL: "data:" + mediaType + ";base64," + data,
		})
	}
	for _, file := range msg.Files {
		data := strings.TrimSpace(file.Data)
		if data == "" {
			continue
		}
		mediaType := strings.TrimSpace(file.MediaType)
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		filename := strings.TrimSpace(file.Filename)
		if filename == "" {
			filename = "attachment"
		}
		parts = append(parts, responsesInputContentPart{
			Type:     "input_file",
			Filename: filename,
			FileData: "data:" + mediaType + ";base64," + data,
		})
	}
	return parts
}

func responsePromptCacheKey(hint *providers.CacheHint) string {
	if hint == nil {
		return ""
	}
	return strings.TrimSpace(hint.PromptCacheKey)
}

func responsesToolParameters(schema map[string]any) map[string]any {
	if schema == nil {
		return nil
	}
	out := make(map[string]any, len(schema)+1)
	for k, v := range schema {
		out[k] = v
	}
	if typ, ok := out["type"].(string); ok && typ == "object" {
		if _, exists := out["properties"]; !exists {
			out["properties"] = map[string]any{}
		}
	}
	return out
}

func responsesToolDefinitionFromProvider(model string, tool providers.ToolDefinition) responsesToolDefinition {
	if strings.EqualFold(tool.Name, "tool_search") {
		return responsesToolDefinition{
			Type:        "tool_search",
			Description: tool.Description,
			Execution:   "client",
			Parameters:  responsesToolParameters(providers.ToolInputSchemaForModel(model, tool.InputSchema)),
		}
	}
	strict := false
	return responsesToolDefinition{
		Type:         "function",
		Name:         tool.Name,
		Description:  tool.Description,
		Strict:       &strict,
		DeferLoading: tool.DeferLoading,
		Parameters:   responsesToolParameters(providers.ToolInputSchemaForModel(model, tool.InputSchema)),
	}
}

func responsesToolDefinitionFromLoadable(model string, tool providers.LoadableToolDefinition) responsesToolDefinition {
	strict := false
	typ := strings.TrimSpace(tool.Type)
	if typ == "" {
		typ = "function"
	}
	return responsesToolDefinition{
		Type:         typ,
		Name:         tool.Name,
		Description:  tool.Description,
		Strict:       &strict,
		DeferLoading: true,
		Parameters:   responsesToolParameters(providers.ToolInputSchemaForModel(model, tool.InputSchema)),
	}
}

func responsesToolSearchOutputTools(model string, content string) []responsesToolDefinition {
	return responsesToolDefinitionsFromLoadable(model, providers.LoadableToolsFromToolSearchResult(content))
}

func responsesCompactedDiscoveredTools(model string, messages []providers.ChatMessage) []responsesToolDefinition {
	attached := providers.AttachedDiscoveredToolsFromMessages(messages)
	if len(attached) == 0 {
		return nil
	}
	rawNames := providers.ToolSearchResultToolNamesFromMessages(messages)
	filtered := make([]providers.LoadableToolDefinition, 0, len(attached))
	for _, tool := range attached {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		if _, exists := rawNames[name]; exists {
			continue
		}
		filtered = append(filtered, tool)
	}
	return responsesToolDefinitionsFromLoadable(model, filtered)
}

func responsesToolDefinitionsFromLoadable(model string, loadable []providers.LoadableToolDefinition) []responsesToolDefinition {
	tools := make([]responsesToolDefinition, 0, len(loadable))
	for _, tool := range loadable {
		if strings.TrimSpace(tool.Name) == "" {
			continue
		}
		tools = append(tools, responsesToolDefinitionFromLoadable(model, tool))
	}
	return tools
}

func shouldOmitResponsesTopLevelTool(tool providers.ToolDefinition, discovered map[string]struct{}) bool {
	if len(discovered) == 0 {
		return false
	}
	if strings.EqualFold(tool.Name, "tool_search") {
		return false
	}
	_, ok := discovered[tool.Name]
	return ok
}

func isResponsesToolSearchCall(call providers.ToolCall) bool {
	return call.Kind == providers.ToolCallKindToolSearch || strings.EqualFold(call.Name, "tool_search")
}

func isResponsesToolSearchResult(msg providers.ChatMessage) bool {
	return msg.ToolResultKind == providers.ToolCallKindToolSearch || strings.EqualFold(msg.Name, "tool_search")
}

func responsesToolSearchArguments(arguments string) any {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return trimmed
	}
	return decoded
}

func (c *Client) doSingleResponsesRequest(
	ctx context.Context,
	httpClient *http.Client,
	body []byte,
	acceptStream bool,
) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if acceptStream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
		_ = resp.Body.Close()
		body := fmt.Sprintf("%s: %s", resp.Status, string(snippet))
		return nil, &providers.HTTPError{
			StatusCode:      resp.StatusCode,
			Body:            body,
			RetryAfter:      providers.ParseRetryAfter(resp),
			ContextOverflow: providers.DetectContextOverflow(body),
		}
	}
	return resp, nil
}

func (c *Client) doResponsesRequest(
	ctx context.Context,
	httpClient *http.Client,
	body []byte,
	acceptStream bool,
) (*http.Response, error) {
	var httpResp *http.Response
	err := providers.WithRetry(ctx, c.retryConfig, func() error {
		resp, err := c.doSingleResponsesRequest(ctx, httpClient, body, acceptStream)
		if err != nil {
			return err
		}
		httpResp = resp
		return nil
	})
	if err != nil {
		return nil, err
	}
	return httpResp, nil
}

func (c *Client) readResponsesSSE(resp *http.Response, ch chan<- providers.StreamEvent) {
	defer close(ch)
	defer resp.Body.Close()

	idleTimeout := c.streamConfig.IdleTimeout
	var idleFired atomic.Bool
	idleTimer := time.AfterFunc(idleTimeout, func() {
		idleFired.Store(true)
		_ = resp.Body.Close()
	})
	defer idleTimer.Stop()
	resetIdle := func() { idleTimer.Reset(idleTimeout) }

	pending := newResponsesPendingTools()
	var sawToolCall bool
	var currentTextPhase providers.MessagePhase

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		resetIdle()
		line := scanner.Text()
		providers.DebugLogf("Responses SSE raw: %s", line)
		if line == "" || strings.HasPrefix(line, "event:") {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			pending.emitEnds(ch)
			ch <- providers.StreamEvent{Type: providers.EventDone}
			return
		}

		var event responsesStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			providers.DebugLogf("Responses SSE parse error: %v, data: %s", err, data)
			ch <- providers.StreamEvent{Type: providers.EventError, Error: fmt.Errorf("parse chunk: %w", err)}
			return
		}

		switch event.Type {
		case "response.output_text.delta":
			if event.Delta != "" {
				ch <- providers.StreamEvent{Type: providers.EventContentDelta, Content: event.Delta, Phase: currentTextPhase}
			}

		case "response.output_item.added":
			switch event.Item.Type {
			case "message":
				if phase := providers.NormalizeMessagePhase(event.Item.Phase); phase != "" {
					currentTextPhase = phase
					ch <- providers.StreamEvent{Type: providers.EventContentDelta, Phase: currentTextPhase}
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
			switch event.Item.Type {
			case "message":
				if phase := providers.NormalizeMessagePhase(event.Item.Phase); phase != "" {
					currentTextPhase = phase
					ch <- providers.StreamEvent{Type: providers.EventContentDelta, Phase: currentTextPhase}
				}
			case "function_call", "tool_search_call":
				sawToolCall = true
				pt := pending.start(event.Item, event.outputIndex(), ch)
				pending.emitEnd(pt, event.Item.argumentsString(), ch)
			}

		case "response.completed":
			pending.emitEnds(ch)
			usage, stopReason, truncated := responsesDoneMetadata(event.Response, sawToolCall)
			ch <- providers.StreamEvent{
				Type:       providers.EventDone,
				Usage:      usage,
				StopReason: stopReason,
				Truncated:  truncated,
			}
			return

		case "response.failed":
			if event.Response != nil && event.Response.Error != nil {
				ch <- providers.StreamEvent{Type: providers.EventError, Error: event.Response.Error.asError()}
				return
			}
			ch <- providers.StreamEvent{Type: providers.EventError, Error: errors.New("response failed")}
			return

		case "error":
			if event.Error != nil {
				ch <- providers.StreamEvent{Type: providers.EventError, Error: event.Error.asError()}
				return
			}
			ch <- providers.StreamEvent{Type: providers.EventError, Error: errors.New("response stream error")}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		if idleFired.Load() {
			ch <- providers.StreamEvent{
				Type:  providers.EventError,
				Error: fmt.Errorf("stream idle timeout after %s: %w", idleTimeout, context.DeadlineExceeded),
			}
			return
		}
		ch <- providers.StreamEvent{Type: providers.EventError, Error: fmt.Errorf("read stream: %w", err)}
		return
	}
	if idleFired.Load() {
		ch <- providers.StreamEvent{
			Type:  providers.EventError,
			Error: fmt.Errorf("stream idle timeout after %s: %w", idleTimeout, context.DeadlineExceeded),
		}
		return
	}
	ch <- providers.StreamEvent{
		Type:  providers.EventError,
		Error: providers.NewIncompleteStreamError("stream closed before response.completed"),
	}
}

func responsesDoneMetadata(resp *responsesResponse, sawToolCall bool) (*providers.TokenUsage, string, bool) {
	if resp == nil {
		if sawToolCall {
			return nil, "tool_calls", false
		}
		return nil, "", false
	}

	usage := resp.Usage.asTokenUsage()
	stopReason := strings.ToLower(strings.TrimSpace(resp.Status))
	truncated := false
	if resp.IncompleteDetails != nil && strings.TrimSpace(resp.IncompleteDetails.Reason) != "" {
		stopReason = strings.ToLower(strings.TrimSpace(resp.IncompleteDetails.Reason))
		truncated = stopReason == "max_output_tokens"
	}
	if sawToolCall && !truncated {
		stopReason = "tool_calls"
	}
	return usage, stopReason, truncated
}

type responsesPendingTool struct {
	itemID      string
	outputIndex int
	id          string
	name        string
	kind        providers.ToolCallKind
	args        strings.Builder
	ended       bool
}

type responsesPendingTools struct {
	items    []*responsesPendingTool
	byItemID map[string]*responsesPendingTool
	byIndex  map[int]*responsesPendingTool
}

func newResponsesPendingTools() *responsesPendingTools {
	return &responsesPendingTools{
		byItemID: make(map[string]*responsesPendingTool),
		byIndex:  make(map[int]*responsesPendingTool),
	}
}

func (p *responsesPendingTools) start(item responsesOutputItem, outputIndex int, ch chan<- providers.StreamEvent) *responsesPendingTool {
	if item.ID != "" {
		if existing := p.byItemID[item.ID]; existing != nil {
			existing.update(item, outputIndex)
			return existing
		}
	}
	if outputIndex >= 0 {
		if existing := p.byIndex[outputIndex]; existing != nil {
			existing.update(item, outputIndex)
			return existing
		}
	}

	pt := &responsesPendingTool{
		itemID:      item.ID,
		outputIndex: outputIndex,
		id:          item.CallID,
		name:        item.toolCallName(),
		kind:        item.toolCallKind(),
	}
	if args := item.argumentsString(); args != "" {
		pt.args.WriteString(args)
	}
	p.items = append(p.items, pt)
	if item.ID != "" {
		p.byItemID[item.ID] = pt
	}
	if outputIndex >= 0 {
		p.byIndex[outputIndex] = pt
	}
	ch <- providers.StreamEvent{
		Type: providers.EventToolUseStart,
		ToolCall: &providers.ToolCall{
			ID:   pt.id,
			Name: pt.name,
			Kind: pt.kind,
		},
	}
	return pt
}

func (p *responsesPendingTools) appendDelta(event responsesStreamEvent, ch chan<- providers.StreamEvent) {
	pt := p.find(event)
	if pt != nil {
		pt.args.WriteString(event.Delta)
	}
	ch <- providers.StreamEvent{Type: providers.EventToolUseDelta, Content: event.Delta}
}

func (p *responsesPendingTools) setArguments(event responsesStreamEvent) {
	pt := p.find(event)
	args := event.argumentsString()
	if pt == nil || args == "" {
		return
	}
	pt.args.Reset()
	pt.args.WriteString(args)
}

func (p *responsesPendingTools) emitEnd(pt *responsesPendingTool, arguments string, ch chan<- providers.StreamEvent) {
	if pt == nil || pt.ended {
		return
	}
	if arguments != "" {
		pt.args.Reset()
		pt.args.WriteString(arguments)
	}
	pt.ended = true
	ch <- providers.StreamEvent{
		Type: providers.EventToolUseEnd,
		ToolCall: &providers.ToolCall{
			ID:        pt.id,
			Name:      pt.name,
			Kind:      pt.kind,
			Arguments: pt.args.String(),
		},
	}
}

func (p *responsesPendingTools) emitEnds(ch chan<- providers.StreamEvent) {
	for _, pt := range p.items {
		p.emitEnd(pt, "", ch)
	}
}

func (p *responsesPendingTools) find(event responsesStreamEvent) *responsesPendingTool {
	if event.ItemID != "" {
		if pt := p.byItemID[event.ItemID]; pt != nil {
			return pt
		}
	}
	if idx := event.outputIndex(); idx >= 0 {
		if pt := p.byIndex[idx]; pt != nil {
			return pt
		}
	}
	if len(p.items) == 0 {
		return nil
	}
	return p.items[len(p.items)-1]
}

func (p *responsesPendingTool) update(item responsesOutputItem, outputIndex int) {
	if p.itemID == "" {
		p.itemID = item.ID
	}
	if p.outputIndex < 0 {
		p.outputIndex = outputIndex
	}
	if p.id == "" {
		p.id = item.CallID
	}
	if p.name == "" {
		p.name = item.toolCallName()
	}
	if p.kind == "" {
		p.kind = item.toolCallKind()
	}
}

type responsesRequest struct {
	Model           string                    `json:"model"`
	Instructions    string                    `json:"instructions,omitempty"`
	Input           []responsesInputItem      `json:"input"`
	Tools           []responsesToolDefinition `json:"tools,omitempty"`
	ToolChoice      string                    `json:"tool_choice,omitempty"`
	Temperature     float64                   `json:"temperature,omitempty"`
	MaxOutputTokens int                       `json:"max_output_tokens,omitempty"`
	Stream          bool                      `json:"stream,omitempty"`
	Store           *bool                     `json:"store,omitempty"`
	Reasoning       *responsesReasoning       `json:"reasoning,omitempty"`
	PromptCacheKey  string                    `json:"prompt_cache_key,omitempty"`
	Options         map[string]any            `json:"-"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type responsesInputItem struct {
	Type      string `json:"type,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   any    `json:"content,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Status    string `json:"status,omitempty"`
	Execution string `json:"execution,omitempty"`
	Arguments any    `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
	Tools     any    `json:"tools,omitempty"`
}

type responsesInputContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Filename string `json:"filename,omitempty"`
	FileData string `json:"file_data,omitempty"`
}

type responsesToolDefinition struct {
	Type         string         `json:"type"`
	Name         string         `json:"name,omitempty"`
	Description  string         `json:"description,omitempty"`
	Strict       *bool          `json:"strict,omitempty"`
	Execution    string         `json:"execution,omitempty"`
	DeferLoading bool           `json:"defer_loading,omitempty"`
	Parameters   map[string]any `json:"parameters"`
}

type responsesResponse struct {
	Status            string                      `json:"status"`
	Output            []responsesOutputItem       `json:"output"`
	Usage             *responsesUsage             `json:"usage,omitempty"`
	Error             *responsesError             `json:"error,omitempty"`
	IncompleteDetails *responsesIncompleteDetails `json:"incomplete_details,omitempty"`
}

func (r responsesResponse) asChatResponse() (providers.ChatResponse, error) {
	if r.Error != nil {
		return providers.ChatResponse{}, r.Error.asError()
	}

	var contentParts []string
	calls := make([]providers.ToolCall, 0)
	var phase providers.MessagePhase
	for _, item := range r.Output {
		switch item.Type {
		case "message":
			if itemPhase := providers.NormalizeMessagePhase(item.Phase); itemPhase != "" {
				phase = itemPhase
			}
			content, err := parseResponsesContent(item.Content)
			if err != nil {
				return providers.ChatResponse{}, err
			}
			if content != "" {
				contentParts = append(contentParts, content)
			}
		case "function_call":
			calls = append(calls, providers.ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: item.argumentsString(),
			})
		case "tool_search_call":
			calls = append(calls, providers.ToolCall{
				ID:        item.CallID,
				Name:      "tool_search",
				Kind:      providers.ToolCallKindToolSearch,
				Arguments: item.argumentsString(),
			})
		}
	}

	stopReason := strings.ToLower(strings.TrimSpace(r.Status))
	truncated := false
	if r.IncompleteDetails != nil && strings.TrimSpace(r.IncompleteDetails.Reason) != "" {
		stopReason = strings.ToLower(strings.TrimSpace(r.IncompleteDetails.Reason))
		truncated = stopReason == "max_output_tokens"
	}
	if len(calls) > 0 && !truncated {
		stopReason = "tool_calls"
	}

	return providers.ChatResponse{
		Content:    strings.Join(contentParts, "\n"),
		Phase:      phase,
		ToolCalls:  calls,
		Usage:      r.Usage.asTokenUsage(),
		StopReason: stopReason,
		Truncated:  truncated,
	}, nil
}

type responsesOutputItem struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Role      string          `json:"role,omitempty"`
	Phase     string          `json:"phase,omitempty"`
	Status    string          `json:"status,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Execution string          `json:"execution,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

func (i responsesOutputItem) toolCallName() string {
	if i.Type == "tool_search_call" {
		return "tool_search"
	}
	return i.Name
}

func (i responsesOutputItem) toolCallKind() providers.ToolCallKind {
	if i.Type == "tool_search_call" {
		return providers.ToolCallKindToolSearch
	}
	return ""
}

func (i responsesOutputItem) argumentsString() string {
	return rawResponseArgumentsString(i.Arguments)
}

func (e responsesStreamEvent) argumentsString() string {
	return rawResponseArgumentsString(e.Arguments)
}

func rawResponseArgumentsString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, raw); err == nil {
		return compacted.String()
	}
	return string(raw)
}

type responsesContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func parseResponsesContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}

	var parts []responsesContentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			switch part.Type {
			case "output_text", "text", "input_text":
				if part.Text != "" {
					out = append(out, part.Text)
				}
			}
		}
		return strings.Join(out, "\n"), nil
	}

	return "", fmt.Errorf("unsupported response content: %s", string(raw))
}

type responsesIncompleteDetails struct {
	Reason string `json:"reason,omitempty"`
}

type responsesUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	InputTokensDetails *struct {
		CachedTokens int `json:"cached_tokens,omitempty"`
	} `json:"input_tokens_details,omitempty"`
}

func (u *responsesUsage) asTokenUsage() *providers.TokenUsage {
	if u == nil {
		return nil
	}
	cached := 0
	if u.InputTokensDetails != nil {
		cached = u.InputTokensDetails.CachedTokens
	}
	freshInput := u.InputTokens - cached
	if freshInput < 0 {
		freshInput = 0
	}
	return &providers.TokenUsage{
		InputTokens:     freshInput,
		OutputTokens:    u.OutputTokens,
		CacheReadTokens: cached,
	}
}

type responsesError struct {
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func (e *responsesError) asError() error {
	if e == nil {
		return errors.New("response error")
	}
	if e.Code != "" || e.Message != "" {
		return providers.NewProviderStreamError(e.Code, e.Message)
	}
	if e.Type != "" {
		return fmt.Errorf("response error: %s", e.Type)
	}
	return errors.New("response error")
}

type responsesStreamEvent struct {
	Type        string              `json:"type"`
	Delta       string              `json:"delta,omitempty"`
	Arguments   json.RawMessage     `json:"arguments,omitempty"`
	ItemID      string              `json:"item_id,omitempty"`
	OutputIndex *int                `json:"output_index,omitempty"`
	Item        responsesOutputItem `json:"item,omitempty"`
	Response    *responsesResponse  `json:"response,omitempty"`
	Error       *responsesError     `json:"error,omitempty"`
}

func (e responsesStreamEvent) outputIndex() int {
	if e.OutputIndex == nil {
		return -1
	}
	return *e.OutputIndex
}
