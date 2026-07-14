package runtime

import (
	"context"
	"errors"
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
	sessionDreamTimeout        = 3 * time.Minute
	sessionDreamLockStaleAfter = 2 * sessionDreamTimeout
	sessionDreamFailureBackoff = time.Hour
	sessionDreamMaxSteps       = 6
)

type sessionDreamScheduler struct {
	rootDir            string
	workspaceStateDir  string
	sessionArtifactDir func() string
	interval           time.Duration

	mu      sync.Mutex
	running bool
}

func newSessionDreamScheduler(rootDir, workspaceStateDir string, sessionArtifactDir func() string, intervalDays int) *sessionDreamScheduler {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" || strings.TrimSpace(workspaceStateDir) == "" || sessionArtifactDir == nil || intervalDays <= 0 {
		return nil
	}
	return &sessionDreamScheduler{
		rootDir:            rootDir,
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
	lock, acquired, err := sessionmemory.TryAcquireDreamLock(s.workspaceStateDir, sessionDreamLockStaleAfter, time.Now().UTC())
	if err != nil || !acquired {
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
		rootDir:            s.rootDir,
		workspaceStateDir:  s.workspaceStateDir,
		sessionArtifactDir: sessionArtifactDir,
		history:            cloneMemoryReviewHistory(history),
		now:                time.Now().UTC(),
	}
	journal := runner.InferenceJournal
	go func() {
		defer s.finish()
		defer lock.Release()
		dreamCtx := providers.WithInferenceJournal(context.Background(), journal)
		dreamCtx, cancel := context.WithTimeout(dreamCtx, sessionDreamTimeout)
		defer cancel()
		_ = s.run(dreamCtx, job)
	}()
}

func (s *sessionDreamScheduler) shouldStart(history []providers.ChatMessage, result agent.LoopResult, now time.Time) bool {
	if strings.TrimSpace(result.Content) == "" || countMemoryReviewUserTurns(history) == 0 {
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
	// Crash self-heal (repair plan 2026-07-04 item #9): a dream that died
	// with its process leaves LastStatus=running forever — the state file
	// lies and, after an earlier completed dream, the interval gate would
	// sit on the retry. A running state older than the dream lock's stale
	// window (2× the dream timeout, so no live dream can still hold it) is
	// reconciled to failed and retried immediately, bypassing both the
	// failure backoff and the interval gate: the interval had already
	// elapsed when the interrupted dream was allowed to start. A running
	// state still inside the stale window may belong to a live dream in
	// another process, so back off and let the cross-process lock arbitrate.
	if state.LastStatus == sessionmemory.DreamStatusRunning {
		if !state.LastStartedAt.IsZero() && now.Sub(state.LastStartedAt) < sessionDreamLockStaleAfter {
			s.finish()
			return false
		}
		if err := sessionmemory.RecordDreamFailed(s.workspaceStateDir, now, errors.New("interrupted: process exited while a dream was running (stale running state reconciled)")); err != nil {
			s.finish()
			return false
		}
		return true
	}
	if state.LastStatus == sessionmemory.DreamStatusFailed &&
		!state.LastFinishedAt.IsZero() &&
		now.Sub(state.LastFinishedAt) < sessionDreamFailureBackoff {
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
	rootDir            string
	workspaceStateDir  string
	sessionArtifactDir string
	history            []providers.ChatMessage
	now                time.Time
}

func (s *sessionDreamScheduler) run(ctx context.Context, job sessionDreamJob) error {
	if job.client == nil || strings.TrimSpace(job.model) == "" {
		return nil
	}
	messages := buildSessionDreamMessages(job.history)
	if len(messages) == 0 {
		return nil
	}
	started := job.now
	if started.IsZero() {
		started = time.Now().UTC()
	}
	if err := sessionmemory.RecordDreamStarted(job.workspaceStateDir, started); err != nil {
		return err
	}
	executor := newSessionDreamExecutor(job.rootDir, job.workspaceStateDir, job.sessionArtifactDir)
	_, err := agent.RunToolLoop(ctx, messages, agent.LoopConfig{
		Tools:                    executor,
		Model:                    job.model,
		InferenceOperationKind:   providers.InferenceOperationMemory,
		InferenceWorkloadProfile: providers.InferenceProfileBestEffort,
		Temperature:              job.temperature,
		MaxSteps:                 sessionDreamMaxSteps,
		Effort:                   job.effort,
		ProviderOptions:          provideroptions.Clone(job.providerOptions),
	}, backgroundMemoryStep{client: job.client})
	if err != nil {
		_ = sessionmemory.RecordDreamFailed(job.workspaceStateDir, time.Now().UTC(), err)
		return err
	}
	return sessionmemory.RecordDreamCompleted(job.workspaceStateDir, started)
}

type sessionDreamExecutor struct {
	registry *tools.Registry
	defs     []providers.ToolDefinition
}

func newSessionDreamExecutor(rootDir, workspaceStateDir, sessionArtifactDir string) *sessionDreamExecutor {
	env := &tools.Env{
		RootDir:    rootDir,
		StateDir:   workspaceStateDir,
		SessionDir: sessionArtifactDir,
	}
	registry := tools.NewRegistry(
		tools.NewReadFileTool(env),
		tools.NewListFilesTool(env),
		tools.NewGrepTool(env),
		tools.NewGlobTool(env),
		tools.NewSessionMemoryTool(env),
	)
	return &sessionDreamExecutor{registry: registry, defs: registry.Definitions()}
}

func (e *sessionDreamExecutor) Definitions() []providers.ToolDefinition {
	if e == nil || e.defs == nil {
		return nil
	}
	return e.defs
}

func (e *sessionDreamExecutor) Execute(ctx context.Context, call providers.ToolCall) (string, error) {
	if e == nil || e.registry == nil {
		return "", fmt.Errorf("background dream: tool %q is not available", call.Name)
	}
	tool := e.registry.Lookup(call.Name)
	if tool == nil {
		return "", fmt.Errorf("background dream: tool %q is not available", call.Name)
	}
	result, err := tool.Execute(ctx, call.Arguments)
	if err == nil && call.Name == "session_memory" {
		_ = recordBackgroundMemoryEvent("session_dream", call.Name, result)
	}
	return result, err
}

func (e *sessionDreamExecutor) ToolMetadata(call providers.ToolCall) (agent.ToolMetadata, bool) {
	if e == nil || e.registry == nil {
		return agent.ToolMetadata{}, false
	}
	tool := e.registry.Lookup(call.Name)
	if tool == nil {
		return agent.ToolMetadata{}, false
	}
	info := tools.ToolClassification{
		ReadOnly:        tool.IsReadOnly(),
		ConcurrencySafe: tool.IsConcurrencySafe(),
	}
	if classifier, ok := tool.(tools.InputClassifyingTool); ok {
		info = classifier.Classify(call.Arguments)
	}
	return agent.ToolMetadata{
		ReadOnly:        info.ReadOnly,
		ConcurrencySafe: info.ConcurrencySafe,
		Destructive:     info.Destructive,
		Risk:            string(info.Risk),
		Reason:          info.Reason,
	}, true
}

func buildSessionDreamMessages(history []providers.ChatMessage) []providers.ChatMessage {
	out := make([]providers.ChatMessage, 0, len(history)+2)
	out = append(out, providers.ChatMessage{Role: "system", Content: sessionDreamSystemPrompt})
	transcript := selectMemoryReviewTranscript(history)
	if len(transcript) == 0 {
		return nil
	}
	out = append(out, transcript...)
	out = append(out, providers.ChatMessage{Role: "user", Content: sessionDreamPrompt})
	return out
}

const sessionDreamSystemPrompt = `You are a background memory review worker. Your only job is to inspect the recent conversation and maintain durable memory. Do not modify workspace files or use capabilities outside the listed memory-review tools.`

const sessionDreamPrompt = `Review the recent conversation and consolidate durable workspace/session memory.

Available tools are read_file, list_files, glob, grep, and session_memory.

Use read_file, list_files, glob, and grep only when workspace inspection is required to confirm what should be remembered. Stay within the listed memory-review tools; do not modify source files directly, access network resources, or invoke external programs.

Use session_memory for durable writes:
1. Read existing project_memory, summary, checkpoint, or notes when needed before editing them.
2. Write project_memory only for stable workspace facts, architecture decisions, durable conventions, tool quirks, or recurring workflow lessons that should survive future sessions.
3. Write summary for compact recoverable state of the active task: active intent, next concrete action, current work, decisions, and open questions. Use checkpoint only when maintaining older checkpoint.md content.
4. Write notes for useful session scratch details that are not durable enough for project_memory.

If session_memory is insufficient for a memory artifact, reply with what is missing instead of writing files directly. Do not modify source files.

Do not store raw transcripts, secrets, temporary task progress, completed-work logs, PR numbers, commit SHAs, raw command output, or facts likely to go stale within a week.

Prefer session_memory action="append" for new durable facts or notes. Use action="replace" only when consolidating an existing summary or project memory into cleaner markdown. If nothing should change, reply exactly: Nothing to dream.`
