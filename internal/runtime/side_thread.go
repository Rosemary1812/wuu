package runtime

import (
	"errors"
	"strings"

	"github.com/blueberrycongee/wuu/internal/agent"
)

const sideThreadSystemPrompt = `You are a read-only side conversation attached to a coding task. Answer the user's questions from the supplied snapshot of the main task and the side-conversation history.

The snapshot may have been captured while the main task was still running. Describe only what the snapshot proves, distinguish completed work from in-progress work, and say when the current outcome is not yet known. Do not claim to have inspected state newer than the snapshot.

You have no tools and must not modify files, run commands, start agents, or steer the main task. Keep the answer direct and useful.`

// NewSideThreadRunner clones the active model configuration into an isolated,
// tool-free runner. It shares the provider client but none of the main
// thread's mutable callbacks, tool ledger, request hooks, or usage tracker.
func (s *Session) NewSideThreadRunner(sideThreadID string) (*agent.StreamRunner, error) {
	if s == nil || s.StreamRunner == nil {
		return nil, errors.New("stream runner is required")
	}
	id := strings.TrimSpace(sideThreadID)
	if id == "" {
		return nil, errors.New("side thread id is required")
	}

	runner := cloneStreamRunnerForThread(s.StreamRunner, nil)
	runner.Tools = nil
	runner.ToolLedger = nil
	runner.SystemPrompt = sideThreadSystemPrompt
	runner.SystemPromptSections = nil
	runner.MaxSteps = 1
	runner.OnEvent = nil
	runner.OnUsage = nil
	runner.OnTokenUsage = nil
	runner.StreamingToolExecution = false
	runner.BeforeStep = nil
	runner.BeforeRequestContext = nil
	runner.BeforeRequest = nil
	runner.OnRequestContext = nil
	runner.OnCompactAttempt = nil
	runner.OnToolBatchRejected = nil
	runner.AfterTurn = nil
	runner.ForceToolFirstStep = ""
	runner.ForceInitialCompact = false
	runner.CompactOnly = false
	runner.PromptCacheKey = "side-thread:" + id
	runner.InferenceJournal = s.InferenceJournalForOwner("side-thread:" + id)
	return runner, nil
}
