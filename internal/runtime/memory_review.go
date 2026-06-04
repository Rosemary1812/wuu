package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/blueberrycongee/wuu/internal/agent"
	memstore "github.com/blueberrycongee/wuu/internal/memory/store"
	"github.com/blueberrycongee/wuu/internal/provideroptions"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/tools"
)

const (
	profileMemoryReviewTimeout               = 2 * time.Minute
	profileMemoryReviewMaxSteps              = 4
	profileMemoryReviewMaxTranscriptMessages = 60
	profileMemoryReviewMaxMessageBytes       = 2000
)

type profileMemoryReviewScheduler struct {
	provider        memstore.Provider
	interval        int
	memoryCharLimit int
	userCharLimit   int

	mu         sync.Mutex
	hydrated   bool
	turnsSince int
}

func newProfileMemoryReviewScheduler(provider memstore.Provider, interval, memoryCharLimit, userCharLimit int) *profileMemoryReviewScheduler {
	if provider == nil || interval <= 0 {
		return nil
	}
	return &profileMemoryReviewScheduler{
		provider:        provider,
		interval:        interval,
		memoryCharLimit: memoryCharLimit,
		userCharLimit:   userCharLimit,
	}
}

func (s *profileMemoryReviewScheduler) AfterTurn(ctx context.Context, runner *agent.StreamRunner, history []providers.ChatMessage, result agent.LoopResult) {
	if s == nil || runner == nil || !s.shouldReview(history, result) {
		return
	}
	if ctx != nil && ctx.Err() != nil {
		return
	}
	client := runner.Client
	model := strings.TrimSpace(runner.APIModel)
	if model == "" {
		model = strings.TrimSpace(runner.Model)
	}
	if client == nil || model == "" {
		return
	}
	job := profileMemoryReviewJob{
		client:          client,
		model:           model,
		systemPrompt:    runner.SystemPrompt,
		temperature:     runner.Temperature,
		effort:          runner.Effort,
		providerOptions: provideroptions.Clone(runner.ProviderOptions),
		history:         cloneChatMessages(history),
	}
	go func() {
		reviewCtx, cancel := context.WithTimeout(context.Background(), profileMemoryReviewTimeout)
		defer cancel()
		_ = s.run(reviewCtx, job)
	}()
}

func (s *profileMemoryReviewScheduler) shouldReview(history []providers.ChatMessage, result agent.LoopResult) bool {
	if strings.TrimSpace(result.Content) == "" {
		return false
	}
	if turnUsedWriteMemory(result) {
		s.reset()
		return false
	}
	userTurns := countProfileMemoryUserTurns(history)
	if userTurns == 0 {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hydrated {
		priorTurns := userTurns - 1
		if priorTurns > 0 {
			s.turnsSince = priorTurns % s.interval
		}
		s.hydrated = true
	}
	s.turnsSince++
	if s.turnsSince < s.interval {
		return false
	}
	s.turnsSince = 0
	return true
}

func (s *profileMemoryReviewScheduler) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hydrated = true
	s.turnsSince = 0
}

type profileMemoryReviewJob struct {
	client          providers.Client
	model           string
	systemPrompt    string
	temperature     float64
	effort          string
	providerOptions map[string]any
	history         []providers.ChatMessage
}

func (s *profileMemoryReviewScheduler) run(ctx context.Context, job profileMemoryReviewJob) error {
	if s == nil || s.provider == nil {
		return nil
	}
	if job.client == nil || strings.TrimSpace(job.model) == "" {
		return nil
	}
	messages := buildProfileMemoryReviewMessages(job.systemPrompt, job.history)
	if len(messages) == 0 {
		return nil
	}
	executor := newProfileMemoryOnlyExecutor(s.provider, s.memoryCharLimit, s.userCharLimit)
	_, err := agent.RunToolLoop(ctx, messages, agent.LoopConfig{
		Tools:           executor,
		Model:           job.model,
		Temperature:     job.temperature,
		MaxSteps:        profileMemoryReviewMaxSteps,
		Effort:          job.effort,
		ProviderOptions: provideroptions.Clone(job.providerOptions),
	}, profileMemoryReviewStep{client: job.client})
	return err
}

type profileMemoryReviewStep struct {
	client providers.Client
}

func (s profileMemoryReviewStep) Execute(ctx context.Context, req providers.ChatRequest) (agent.StepResult, error) {
	resp, err := s.client.Chat(ctx, req)
	if err != nil {
		return agent.StepResult{}, err
	}
	return agent.StepResult{
		Content:          resp.Content,
		ReasoningContent: resp.ReasoningContent,
		ReasoningBlocks:  append([]providers.ReasoningBlock(nil), resp.ReasoningBlocks...),
		ToolCalls:        append([]providers.ToolCall(nil), resp.ToolCalls...),
		Truncated:        resp.Truncated,
		StopReason:       resp.StopReason,
		Usage:            resp.Usage,
	}, nil
}

type profileMemoryOnlyExecutor struct {
	index map[string]tools.Tool
	defs  []providers.ToolDefinition
}

func newProfileMemoryOnlyExecutor(provider memstore.Provider, memoryCharLimit, userCharLimit int) *profileMemoryOnlyExecutor {
	env := &tools.Env{Memory: provider, MemoryCharLimit: memoryCharLimit, UserMemoryCharLimit: userCharLimit}
	memoryTools := tools.NewMemoryTools(env)
	index := make(map[string]tools.Tool, len(memoryTools))
	defs := make([]providers.ToolDefinition, 0, len(memoryTools))
	for _, tool := range memoryTools {
		index[tool.Name()] = tool
		defs = append(defs, tool.Definition())
	}
	return &profileMemoryOnlyExecutor{index: index, defs: defs}
}

func (e *profileMemoryOnlyExecutor) Definitions() []providers.ToolDefinition {
	return e.defs
}

func (e *profileMemoryOnlyExecutor) Execute(ctx context.Context, call providers.ToolCall) (string, error) {
	tool := e.index[call.Name]
	if tool == nil {
		return "", fmt.Errorf("background memory review: tool %q is not available", call.Name)
	}
	return tool.Execute(ctx, call.Arguments)
}

func (e *profileMemoryOnlyExecutor) ToolMetadata(call providers.ToolCall) (agent.ToolMetadata, bool) {
	tool := e.index[call.Name]
	if tool == nil {
		return agent.ToolMetadata{}, false
	}
	return agent.ToolMetadata{
		ReadOnly:        tool.IsReadOnly(),
		ConcurrencySafe: tool.IsConcurrencySafe(),
	}, true
}

func buildProfileMemoryReviewMessages(systemPrompt string, history []providers.ChatMessage) []providers.ChatMessage {
	out := make([]providers.ChatMessage, 0, len(history)+2)
	if strings.TrimSpace(systemPrompt) != "" {
		out = append(out, providers.ChatMessage{Role: "system", Content: systemPrompt})
	}
	transcript := selectProfileMemoryReviewTranscript(history)
	if len(transcript) == 0 {
		return nil
	}
	for _, msg := range transcript {
		out = append(out, msg)
	}
	out = append(out, providers.ChatMessage{Role: "user", Content: profileMemoryReviewPrompt})
	return out
}

func selectProfileMemoryReviewTranscript(history []providers.ChatMessage) []providers.ChatMessage {
	transcript := make([]providers.ChatMessage, 0, len(history))
	for _, msg := range history {
		role := strings.TrimSpace(msg.Role)
		if role != "user" && role != "assistant" {
			continue
		}
		content := normalizeProfileMemoryReviewContent(msg.Content)
		if content == "" || isSyntheticProfileMemoryUserMessage(role, content) {
			continue
		}
		transcript = append(transcript, providers.ChatMessage{Role: role, Content: content})
	}
	if len(transcript) > profileMemoryReviewMaxTranscriptMessages {
		return transcript[len(transcript)-profileMemoryReviewMaxTranscriptMessages:]
	}
	return transcript
}

func normalizeProfileMemoryReviewContent(content string) string {
	content = strings.TrimSpace(strings.Join(strings.Fields(content), " "))
	if content == "" {
		return ""
	}
	if len(content) <= profileMemoryReviewMaxMessageBytes {
		return content
	}
	cut := profileMemoryReviewMaxMessageBytes
	for cut > 0 && !utf8.ValidString(content[:cut]) {
		cut--
	}
	return content[:cut] + " [truncated]"
}

func isSyntheticProfileMemoryUserMessage(role, content string) bool {
	if role != "user" {
		return false
	}
	return strings.HasPrefix(content, "[Hook context for ") ||
		strings.HasPrefix(content, "Output token limit hit. Resume directly")
}

func countProfileMemoryUserTurns(history []providers.ChatMessage) int {
	count := 0
	for _, msg := range history {
		content := normalizeProfileMemoryReviewContent(msg.Content)
		if strings.TrimSpace(msg.Role) == "user" && content != "" && !isSyntheticProfileMemoryUserMessage("user", content) {
			count++
		}
	}
	return count
}

func turnUsedWriteMemory(result agent.LoopResult) bool {
	for _, msg := range result.NewMessages {
		for _, call := range msg.ToolCalls {
			if call.Name == "write_memory" {
				return true
			}
		}
		if msg.Role == "tool" && msg.Name == "write_memory" {
			return true
		}
	}
	return false
}

func cloneChatMessages(messages []providers.ChatMessage) []providers.ChatMessage {
	out := make([]providers.ChatMessage, len(messages))
	for i, msg := range messages {
		out[i] = msg
		out[i].Images = nil
		out[i].Files = nil
		out[i].ReasoningBlocks = nil
		out[i].ReasoningContent = ""
		out[i].ToolCalls = nil
	}
	return out
}

const profileMemoryReviewPrompt = `Review the conversation above and decide whether profile memory should be updated.

Save memory only when it is durable and likely to help future sessions:
1. User profile facts, preferences, communication style, expectations, or recurring corrections go to write_memory with target="user".
2. Stable project conventions, environment facts, tool quirks, or recurring workflow lessons go to write_memory with target="memory".

Do not save task progress, completed-work logs, temporary TODOs, PR numbers, issue numbers, commit SHAs, raw dumps, or facts likely to go stale within a week. Do not duplicate facts that are already present in the current profile memory snapshot.

If something is worth saving, call write_memory with action="add" and a compact declarative fact. If an existing memory is wrong, stale, or the target is near its character limit, use action="replace" or action="remove" with old_text. If nothing is worth saving, reply exactly: Nothing to save.`
