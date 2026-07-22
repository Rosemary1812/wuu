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
	sessionDreamTimeout            = 3 * time.Minute
	sessionDreamStateRecoveryAfter = 2 * sessionDreamTimeout
	sessionDreamFailureBackoff     = time.Hour
	sessionDreamMaxSteps           = 6
)

type sessionDreamScheduler struct {
	rootDir            string
	workspaceStateDir  string
	sessionArtifactDir func() string
	interval           time.Duration
	reportError        func(error)

	// Optional dedicated client/model. When client is nil the scheduler falls
	// back to the runner's client and model (the current selected provider).
	client providers.Client
	model  string

	mu      sync.Mutex
	running bool
}

func newSessionDreamScheduler(rootDir, workspaceStateDir string, sessionArtifactDir func() string, intervalDays int, dedicatedClient providers.Client, dedicatedModel string) *sessionDreamScheduler {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" || strings.TrimSpace(workspaceStateDir) == "" || sessionArtifactDir == nil || intervalDays <= 0 {
		return nil
	}
	return &sessionDreamScheduler{
		rootDir:            rootDir,
		workspaceStateDir:  workspaceStateDir,
		sessionArtifactDir: sessionArtifactDir,
		interval:           time.Duration(intervalDays) * 24 * time.Hour,
		client:             dedicatedClient,
		model:              strings.TrimSpace(dedicatedModel),
		reportError: func(err error) {
			providers.DebugLogf("session dream: %v", err)
		},
	}
}

// sessionDreamAfterTurn builds a fresh dream scheduler AfterTurn hook. It is
// used to rebuild the hook after live settings changes without recreating the
// whole StreamRunner.
func sessionDreamAfterTurn(rootDir, workspaceStateDir string, sessionArtifactDir func() string, intervalDays int, dedicatedClient providers.Client, dedicatedModel string) func(context.Context, *agent.StreamRunner, []providers.ChatMessage, agent.LoopResult) {
	scheduler := newSessionDreamScheduler(rootDir, workspaceStateDir, sessionArtifactDir, intervalDays, dedicatedClient, dedicatedModel)
	if scheduler == nil {
		return nil
	}
	return scheduler.AfterTurn
}

func (s *sessionDreamScheduler) AfterTurn(ctx context.Context, runner *agent.StreamRunner, history []providers.ChatMessage, result agent.LoopResult) {
	if s == nil || runner == nil || strings.TrimSpace(result.Content) == "" || countMemoryReviewUserTurns(history) == 0 {
		return
	}
	// Lock before reading or repairing DreamState. A live owner may legitimately
	// run longer than the state-recovery grace, and its state must not be marked
	// interrupted by a contender that cannot acquire ownership.
	lock, acquired, err := sessionmemory.TryAcquireDreamLock(s.workspaceStateDir)
	if err != nil || !acquired {
		return
	}
	if !s.shouldStart(history, result, time.Now().UTC()) {
		_ = lock.Release()
		return
	}
	releaseStart := func() {
		s.finish()
		_ = lock.Release()
	}
	if ctx != nil && ctx.Err() != nil {
		releaseStart()
		return
	}
	sessionArtifactDir := strings.TrimSpace(s.sessionArtifactDir())
	if sessionArtifactDir == "" {
		releaseStart()
		return
	}
	var client providers.Client = runner.Client
	model := strings.TrimSpace(runner.APIModel)
	if model == "" {
		model = strings.TrimSpace(runner.Model)
	}
	if s.client != nil {
		client = s.client
	}
	if s.model != "" {
		model = s.model
	}
	if client == nil || model == "" {
		releaseStart()
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
		defer lock.Release()
		defer s.finish()
		dreamCtx := providers.WithInferenceJournal(context.Background(), journal)
		dreamCtx, cancel := context.WithTimeout(dreamCtx, sessionDreamTimeout)
		defer cancel()
		if err := s.run(dreamCtx, job); err != nil && !isPureSessionDreamCancellation(err) && s.reportError != nil {
			s.reportError(err)
		}
	}()
}

func isPureSessionDreamCancellation(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !isPureSessionDreamCancellation(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if child := wrapped.Unwrap(); child != nil {
			return isPureSessionDreamCancellation(child)
		}
	}
	return errors.Is(err, context.Canceled)
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
	// sit on the retry. A running state older than the recovery grace is
	// reconciled to failed and retried immediately, bypassing both the
	// failure backoff and the interval gate: the interval had already
	// elapsed when the interrupted dream was allowed to start. Lock ownership
	// is independent of this timestamp; the OS-backed lock below still rejects
	// the retry if another process is actually running the dream.
	if state.LastStatus == sessionmemory.DreamStatusRunning {
		if !state.LastStartedAt.IsZero() && now.Sub(state.LastStartedAt) < sessionDreamStateRecoveryAfter {
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
		if recordErr := sessionmemory.RecordDreamFailed(job.workspaceStateDir, time.Now().UTC(), err); recordErr != nil {
			return errors.Join(err, fmt.Errorf("record dream failure: %w", recordErr))
		}
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
