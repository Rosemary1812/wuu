package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/provideroptions"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/sessionmemory"
	"github.com/blueberrycongee/wuu/internal/tools"
)

const (
	sessionDreamTimeout  = 3 * time.Minute
	sessionDreamMaxSteps = 6
)

type sessionDreamScheduler struct {
	workspaceStateDir  string
	sessionArtifactDir func() string
	interval           time.Duration

	mu      sync.Mutex
	running bool
}

func newSessionDreamScheduler(workspaceStateDir string, sessionArtifactDir func() string, intervalDays int) *sessionDreamScheduler {
	if strings.TrimSpace(workspaceStateDir) == "" || sessionArtifactDir == nil || intervalDays <= 0 {
		return nil
	}
	return &sessionDreamScheduler{
		workspaceStateDir:  workspaceStateDir,
		sessionArtifactDir: sessionArtifactDir,
		interval:           time.Duration(intervalDays) * 24 * time.Hour,
	}
}

func (s *sessionDreamScheduler) AfterTurn(ctx context.Context, runner *agent.StreamRunner, history []providers.ChatMessage, result agent.LoopResult) {
	if s == nil || runner == nil || !s.shouldStart(history, result, time.Now().UTC()) {
		return
	}
	if ctx != nil && ctx.Err() != nil {
		s.finish()
		return
	}
	sessionArtifactDir := strings.TrimSpace(s.sessionArtifactDir())
	if sessionArtifactDir == "" {
		s.finish()
		return
	}
	client := runner.Client
	model := strings.TrimSpace(runner.APIModel)
	if model == "" {
		model = strings.TrimSpace(runner.Model)
	}
	if client == nil || model == "" {
		s.finish()
		return
	}
	job := sessionDreamJob{
		client:             client,
		model:              model,
		systemPrompt:       runner.SystemPrompt,
		temperature:        runner.Temperature,
		effort:             runner.Effort,
		providerOptions:    provideroptions.Clone(runner.ProviderOptions),
		workspaceStateDir:  s.workspaceStateDir,
		sessionArtifactDir: sessionArtifactDir,
		history:            cloneChatMessages(history),
		now:                time.Now().UTC(),
	}
	go func() {
		defer s.finish()
		dreamCtx, cancel := context.WithTimeout(context.Background(), sessionDreamTimeout)
		defer cancel()
		_ = s.run(dreamCtx, job)
	}()
}

func (s *sessionDreamScheduler) shouldStart(history []providers.ChatMessage, result agent.LoopResult, now time.Time) bool {
	if strings.TrimSpace(result.Content) == "" || countProfileMemoryUserTurns(history) == 0 {
		return false
	}
	if !s.tryStart() {
		return false
	}
	state, err := sessionmemory.LoadDreamState(s.workspaceStateDir)
	if err != nil {
		s.finish()
		return false
	}
	if !state.LastRunAt.IsZero() && now.Sub(state.LastRunAt) < s.interval {
		s.finish()
		return false
	}
	return true
}

func (s *sessionDreamScheduler) tryStart() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	return true
}

func (s *sessionDreamScheduler) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
}

type sessionDreamJob struct {
	client             providers.Client
	model              string
	systemPrompt       string
	temperature        float64
	effort             string
	providerOptions    map[string]any
	workspaceStateDir  string
	sessionArtifactDir string
	history            []providers.ChatMessage
	now                time.Time
}

func (s *sessionDreamScheduler) run(ctx context.Context, job sessionDreamJob) error {
	if job.client == nil || strings.TrimSpace(job.model) == "" {
		return nil
	}
	messages := buildSessionDreamMessages(job.systemPrompt, job.history)
	if len(messages) == 0 {
		return nil
	}
	executor := newSessionMemoryOnlyExecutor(job.workspaceStateDir, job.sessionArtifactDir)
	_, err := agent.RunToolLoop(ctx, messages, agent.LoopConfig{
		Tools:           executor,
		Model:           job.model,
		Temperature:     job.temperature,
		MaxSteps:        sessionDreamMaxSteps,
		Effort:          job.effort,
		ProviderOptions: provideroptions.Clone(job.providerOptions),
	}, profileMemoryReviewStep{client: job.client})
	if err != nil {
		return err
	}
	now := job.now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return sessionmemory.SaveDreamState(job.workspaceStateDir, sessionmemory.DreamState{LastRunAt: now})
}

type sessionMemoryOnlyExecutor struct {
	tool *tools.SessionMemoryTool
	defs []providers.ToolDefinition
}

func newSessionMemoryOnlyExecutor(workspaceStateDir, sessionArtifactDir string) *sessionMemoryOnlyExecutor {
	env := &tools.Env{
		StateDir:   workspaceStateDir,
		SessionDir: sessionArtifactDir,
	}
	tool := tools.NewSessionMemoryTool(env)
	return &sessionMemoryOnlyExecutor{
		tool: tool,
		defs: []providers.ToolDefinition{tool.Definition()},
	}
}

func (e *sessionMemoryOnlyExecutor) Definitions() []providers.ToolDefinition {
	return e.defs
}

func (e *sessionMemoryOnlyExecutor) Execute(ctx context.Context, call providers.ToolCall) (string, error) {
	if e == nil || e.tool == nil || call.Name != e.tool.Name() {
		return "", fmt.Errorf("background dream: tool %q is not available", call.Name)
	}
	return e.tool.Execute(ctx, call.Arguments)
}

func (e *sessionMemoryOnlyExecutor) ToolMetadata(call providers.ToolCall) (agent.ToolMetadata, bool) {
	if e == nil || e.tool == nil || call.Name != e.tool.Name() {
		return agent.ToolMetadata{}, false
	}
	info := e.tool.Classify(call.Arguments)
	return agent.ToolMetadata{
		ReadOnly:        info.ReadOnly,
		ConcurrencySafe: info.ConcurrencySafe,
		Destructive:     info.Destructive,
		Risk:            string(info.Risk),
		Reason:          info.Reason,
	}, true
}

func buildSessionDreamMessages(systemPrompt string, history []providers.ChatMessage) []providers.ChatMessage {
	out := make([]providers.ChatMessage, 0, len(history)+2)
	if strings.TrimSpace(systemPrompt) != "" {
		out = append(out, providers.ChatMessage{Role: "system", Content: systemPrompt})
	}
	transcript := selectProfileMemoryReviewTranscript(history)
	if len(transcript) == 0 {
		return nil
	}
	out = append(out, transcript...)
	out = append(out, providers.ChatMessage{Role: "user", Content: sessionDreamPrompt})
	return out
}

const sessionDreamPrompt = `Review the recent conversation and consolidate durable workspace/session memory.

Use only session_memory:
1. Read existing project_memory, checkpoint, or notes when needed before editing them.
2. Write project_memory only for stable workspace facts, architecture decisions, durable conventions, tool quirks, or recurring workflow lessons that should survive future sessions.
3. Write checkpoint for compact recoverable state of the active task: active intent, next concrete action, current work, decisions, and open questions.
4. Write notes for useful session scratch details that are not durable enough for project_memory.

Do not store raw transcripts, secrets, temporary task progress, completed-work logs, PR numbers, commit SHAs, raw command output, or facts likely to go stale within a week. Do not modify source files.

Prefer session_memory action="append" for new durable facts or notes. Use action="replace" only when consolidating an existing checkpoint or project memory into cleaner markdown. If nothing should change, reply exactly: Nothing to dream.`
