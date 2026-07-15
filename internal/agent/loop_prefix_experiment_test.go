package agent

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/compact"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
)

// Prefix-continuity experiment sweep.
//
// These tests drive whole simulated sessions (multiple RunToolLoop runs, each
// with multiple tool rounds) and analyze the byte-prefix relation between
// EVERY consecutive pair of provider requests in the session — within runs
// and across run boundaries. Scenarios where the design allows a divergence
// (history rewrite, per-request transform tail, prune-set growth) assert the
// break happens exactly where allowed and that the chain resumes afterwards.

// prefixBreak records that request index pair (i, i+1) diverged, and at which
// message index the first difference appears (-1 = the later request is
// shorter than the earlier one).
type prefixBreak struct {
	pair     int
	msgIndex int
}

func analyzePrefixChain(requests [][]providers.ChatMessage) []prefixBreak {
	var breaks []prefixBreak
	for i := 0; i+1 < len(requests); i++ {
		prev, next := requests[i], requests[i+1]
		if len(next) < len(prev) {
			breaks = append(breaks, prefixBreak{pair: i, msgIndex: -1})
			continue
		}
		diverged := false
		for j := range prev {
			if !reflect.DeepEqual(prev[j], next[j]) {
				breaks = append(breaks, prefixBreak{pair: i, msgIndex: j})
				diverged = true
				break
			}
		}
		_ = diverged
	}
	return breaks
}

func formatBreaks(requests [][]providers.ChatMessage, breaks []prefixBreak) string {
	var b strings.Builder
	for _, brk := range breaks {
		fmt.Fprintf(&b, "requests %d→%d diverge at message %d", brk.pair, brk.pair+1, brk.msgIndex)
		if brk.msgIndex >= 0 && brk.msgIndex < len(requests[brk.pair]) {
			prev := requests[brk.pair][brk.msgIndex]
			fmt.Fprintf(&b, "\n  prev: role=%s name=%s content=%.80q", prev.Role, prev.Name, prev.Content)
			if brk.msgIndex < len(requests[brk.pair+1]) {
				next := requests[brk.pair+1][brk.msgIndex]
				fmt.Fprintf(&b, "\n  next: role=%s name=%s content=%.80q", next.Role, next.Name, next.Content)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// sessionSim drives a multi-run conversation the way a long-lived runner
// does: history accumulates NewMessages, retained request-context state is
// handed from each run to the next.
type sessionSim struct {
	t        *testing.T
	history  []providers.ChatMessage
	retained *RetainedRequestContextState
	requests [][]providers.ChatMessage
}

func (s *sessionSim) runTurn(prompt string, step *fakeStep, mutate func(cfg *LoopConfig)) LoopResult {
	s.t.Helper()
	s.history = append(s.history, userMsg(prompt))
	cfg := LoopConfig{Model: "m", RetainedRequestContext: s.retained}
	if mutate != nil {
		mutate(&cfg)
	}
	res, err := RunToolLoop(context.Background(), s.history, cfg, step)
	if err != nil {
		s.t.Fatalf("turn %q: %v", prompt, err)
	}
	for _, call := range step.calls {
		s.requests = append(s.requests, call.Messages)
	}
	if res.HistoryRewritten {
		s.history = append([]providers.ChatMessage(nil), res.NewMessages...)
	} else {
		s.history = append(s.history, res.NewMessages...)
	}
	s.retained = res.RetainedRequestContext
	return res
}

func experimentTools() *fakeLoopTools {
	return &fakeLoopTools{
		defs: []providers.ToolDefinition{{Name: "read_file"}},
		results: map[string]string{
			"call_1": `{"content":"one"}`,
			"call_2": `{"content":"two"}`,
			"call_3": `{"content":"three"}`,
			"call_4": `{"content":"four"}`,
			"call_5": `{"content":"five"}`,
			"call_6": `{"content":"six"}`,
		},
	}
}

func toolRoundSteps(callIDs ...string) []StepResult {
	steps := make([]StepResult, 0, len(callIDs)+1)
	for _, id := range callIDs {
		steps = append(steps, StepResult{ToolCalls: []providers.ToolCall{{ID: id, Name: "read_file", Arguments: `{"path":"x"}`}}})
	}
	return append(steps, StepResult{Content: "ok"})
}

func stableAndTurnEnvContext(turn int) func() []ContextSegment {
	return func() []ContextSegment {
		return RequestOnlyContextBlocks([]wuucontext.Block{
			{
				Kind:    wuucontext.BlockActiveFiles,
				Title:   "Active files",
				Source:  "runtime.active_files",
				Content: "files:\n- go.mod",
			},
			{
				Kind:    wuucontext.BlockEnvironment,
				Title:   "Runtime environment",
				Source:  "runtime.snapshot",
				Content: fmt.Sprintf("# Environment\n- State: turn %d", turn),
			},
		})
	}
}

// Scenario 1: steady multi-turn session — stable block plus a per-turn
// environment snapshot, several tool rounds per turn. Every request in the
// session must byte-extend its predecessor: zero divergences allowed,
// including across every run boundary.
func TestPrefixExperiment_SteadySessionNeverDiverges(t *testing.T) {
	sim := &sessionSim{t: t}
	turnCalls := [][]string{{"call_1", "call_2"}, {"call_3"}, {"call_4", "call_5"}, {"call_6"}}
	for turn := 1; turn <= 4; turn++ {
		ctx := stableAndTurnEnvContext(turn)
		sim.runTurn(fmt.Sprintf("ask %d", turn), &fakeStep{results: toolRoundSteps(turnCalls[turn-1]...)}, func(cfg *LoopConfig) {
			cfg.Tools = experimentTools()
			cfg.BeforeRequestContext = ctx
		})
	}
	if len(sim.requests) != 10 {
		t.Fatalf("expected 10 provider requests, got %d", len(sim.requests))
	}
	if breaks := analyzePrefixChain(sim.requests); len(breaks) != 0 {
		t.Fatalf("steady session must keep an unbroken prefix chain:\n%s", formatBreaks(sim.requests, breaks))
	}
	// Cross-checks: the stable block exists exactly once in the final
	// request; each turn's snapshot exactly once.
	final := sim.requests[len(sim.requests)-1]
	if got := countMessagesContaining(final, "[ACTIVE_FILES]"); got != 1 {
		t.Fatalf("stable block duplicated: %d copies in final request", got)
	}
	for turn := 1; turn <= 4; turn++ {
		if got := countMessagesContaining(final, fmt.Sprintf("State: turn %d", turn)); got != 1 {
			t.Fatalf("turn %d snapshot should appear exactly once in final request, got %d", turn, got)
		}
	}
}

// Scenario 2: context snapshot changes on every round (not just per turn).
// Changed snapshots append at the tail, so the chain must still never break.
func TestPrefixExperiment_PerRoundChangingContextNeverDiverges(t *testing.T) {
	sim := &sessionSim{t: t}
	contextCalls := 0
	perRound := func() []ContextSegment {
		contextCalls++
		return RequestOnlyContextBlocks([]wuucontext.Block{{
			Kind:    wuucontext.BlockEnvironment,
			Title:   "Runtime environment",
			Source:  "runtime.snapshot",
			Content: fmt.Sprintf("# Environment\n- State: step %d", contextCalls),
		}})
	}
	sim.runTurn("ask 1", &fakeStep{results: toolRoundSteps("call_1", "call_2")}, func(cfg *LoopConfig) {
		cfg.Tools = experimentTools()
		cfg.BeforeRequestContext = perRound
	})
	sim.runTurn("ask 2", &fakeStep{results: toolRoundSteps("call_3")}, func(cfg *LoopConfig) {
		cfg.Tools = experimentTools()
		cfg.BeforeRequestContext = perRound
	})
	if breaks := analyzePrefixChain(sim.requests); len(breaks) != 0 {
		t.Fatalf("per-round changing context must append, not rewrite:\n%s", formatBreaks(sim.requests, breaks))
	}
}

// Scenario 3: post-tool hook context (one-shot request-only segments emitted
// by tool execution) must ride the transcript append-only, within and across
// runs.
func TestPrefixExperiment_PostToolHookContextNeverDiverges(t *testing.T) {
	sim := &sessionSim{t: t}
	sim.runTurn("ask 1", &fakeStep{results: toolRoundSteps("call_1", "call_2")}, func(cfg *LoopConfig) {
		cfg.Tools = &contextLoopTools{defs: []providers.ToolDefinition{{Name: "read_file"}}}
	})
	sim.runTurn("ask 2", &fakeStep{results: toolRoundSteps("call_3")}, func(cfg *LoopConfig) {
		cfg.Tools = &contextLoopTools{defs: []providers.ToolDefinition{{Name: "read_file"}}}
	})
	if breaks := analyzePrefixChain(sim.requests); len(breaks) != 0 {
		t.Fatalf("post-tool hook context must stay append-only:\n%s", formatBreaks(sim.requests, breaks))
	}
	final := sim.requests[len(sim.requests)-1]
	if got := countMessagesContaining(final, "context for call_1"); got != 1 {
		t.Fatalf("hook context for call_1 should be retained exactly once, got %d", got)
	}
}

// Scenario 4: a per-request BeforeRequest transform that appends one tail
// message. Transforms are request-scoped, so consecutive requests may only
// diverge at the transform's own tail position — everything before it must
// stay byte-stable.
func TestPrefixExperiment_TransformTailIsOnlyDivergencePoint(t *testing.T) {
	sim := &sessionSim{t: t}
	transform := func(_ context.Context, req *providers.ChatRequest) error {
		req.Messages = append(req.Messages, providers.ChatMessage{Role: "user", Content: "per-request injection", Hidden: true})
		return nil
	}
	for turn := 1; turn <= 2; turn++ {
		calls := []string{"call_1", "call_2"}
		if turn == 2 {
			calls = []string{"call_3"}
		}
		sim.runTurn(fmt.Sprintf("ask %d", turn), &fakeStep{results: toolRoundSteps(calls...)}, func(cfg *LoopConfig) {
			cfg.Tools = experimentTools()
			cfg.BeforeRequest = transform
		})
	}
	breaks := analyzePrefixChain(sim.requests)
	if len(breaks) != len(sim.requests)-1 {
		t.Fatalf("every consecutive pair should diverge exactly at the transform tail, got %d breaks for %d pairs:\n%s",
			len(breaks), len(sim.requests)-1, formatBreaks(sim.requests, breaks))
	}
	for _, brk := range breaks {
		wantIdx := len(sim.requests[brk.pair]) - 1
		if brk.msgIndex != wantIdx {
			t.Fatalf("divergence must be confined to the transform tail (message %d), got message %d:\n%s",
				wantIdx, brk.msgIndex, formatBreaks(sim.requests, breaks))
		}
	}
}

// Scenario 5: a mid-run overflow compaction rewrites history. The chain must
// break exactly once — at the compaction retry — and resume unbroken after.
func TestPrefixExperiment_OverflowCompactBreaksOnceThenResumes(t *testing.T) {
	overflow := &providers.HTTPError{StatusCode: 400, Body: "context_length_exceeded", ContextOverflow: true}
	sim := &sessionSim{t: t}
	step := &fakeStep{
		results: []StepResult{
			{ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "read_file", Arguments: `{"path":"x"}`}}},
			{}, // round 2: overflow error
			{ToolCalls: []providers.ToolCall{{ID: "call_2", Name: "read_file", Arguments: `{"path":"x"}`}}},
			{Content: "ok"},
		},
		errs: []error{nil, overflow, nil, nil},
	}
	sim.runTurn("ask 1", step, func(cfg *LoopConfig) {
		cfg.Tools = experimentTools()
		cfg.BeforeRequestContext = stableAndTurnEnvContext(1)
		cfg.Compact = func(_ context.Context, msgs []providers.ChatMessage) ([]providers.ChatMessage, error) {
			return []providers.ChatMessage{userMsg("summary of everything so far")}, nil
		}
	})
	// Follow-up turn after the rewrite: must extend the rewritten transcript.
	sim.runTurn("ask 2", &fakeStep{results: toolRoundSteps("call_3")}, func(cfg *LoopConfig) {
		cfg.Tools = experimentTools()
		cfg.BeforeRequestContext = stableAndTurnEnvContext(2)
	})
	breaks := analyzePrefixChain(sim.requests)
	if len(breaks) != 1 {
		t.Fatalf("expected exactly one prefix break (the compaction retry), got %d:\n%s", len(breaks), formatBreaks(sim.requests, breaks))
	}
	if breaks[0].pair != 1 {
		t.Fatalf("the break must be at the overflow retry (pair 1), got pair %d:\n%s", breaks[0].pair, formatBreaks(sim.requests, breaks))
	}
	// The retried request must still carry the (re-emitted) context blocks.
	retry := sim.requests[2]
	if got := countMessagesContaining(retry, "[ACTIVE_FILES]"); got != 1 {
		t.Fatalf("retry after compaction should re-inject the stable block once, got %d", got)
	}
}

// Scenario 6: ToolPrune across a run boundary. Growing history can move old
// tool results out of the protect window, so a divergence is allowed — but
// only at tool-result positions that flipped to a pruned placeholder;
// anything else is a regression.
func TestPrefixExperiment_ToolPruneDivergesOnlyAtNewlyPrunedResults(t *testing.T) {
	bigContent := strings.Repeat("x", 400_000)
	sim := &sessionSim{t: t}
	sim.history = []providers.ChatMessage{
		userMsg("turn zero"),
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "old_call", Name: "read_file", Arguments: `{"path":"big.txt"}`}}},
		{Role: "tool", Name: "read_file", ToolCallID: "old_call", Content: bigContent},
	}
	for turn := 1; turn <= 3; turn++ {
		calls := [][]string{{"call_1"}, {"call_2"}, {"call_3"}}[turn-1]
		sim.runTurn(fmt.Sprintf("ask %d", turn), &fakeStep{results: toolRoundSteps(calls...)}, func(cfg *LoopConfig) {
			cfg.Tools = experimentTools()
			cfg.ToolPrune = true
		})
	}
	breaks := analyzePrefixChain(sim.requests)
	for _, brk := range breaks {
		next := sim.requests[brk.pair+1]
		if brk.msgIndex < 0 || brk.msgIndex >= len(next) {
			t.Fatalf("prune divergence out of range:\n%s", formatBreaks(sim.requests, breaks))
		}
		msg := next[brk.msgIndex]
		if !strings.EqualFold(msg.Role, "tool") || !strings.Contains(msg.Content, "[Pruned") {
			t.Fatalf("only newly-pruned tool results may diverge, got role=%s content=%.80q:\n%s",
				msg.Role, msg.Content, formatBreaks(sim.requests, breaks))
		}
	}
	// The placeholder metadata must always describe the original content.
	for i, req := range sim.requests {
		for _, msg := range req {
			if strings.Contains(msg.Content, "[Pruned") && !strings.Contains(msg.Content, fmt.Sprintf("Original: %d characters", len(bigContent))) {
				t.Fatalf("request %d carries a placeholder with corrupted metadata: %.120q", i, msg.Content)
			}
		}
	}
}

// Scenario 7: legacy transient junk in the middle of incoming history breaks
// the retained-state fingerprint. The run must fall back to a fresh
// transcript (single context copy), never fail or duplicate.
func TestPrefixExperiment_MidHistoryEditFallsBackSafely(t *testing.T) {
	sim := &sessionSim{t: t}
	sim.runTurn("ask 1", &fakeStep{results: toolRoundSteps("call_1")}, func(cfg *LoopConfig) {
		cfg.Tools = experimentTools()
		cfg.BeforeRequestContext = stableAndTurnEnvContext(1)
	})
	// Simulate an external edit: strip the assistant tool round out of the
	// durable history (e.g. a user deleted a message from the transcript).
	edited := make([]providers.ChatMessage, 0, len(sim.history))
	for _, msg := range sim.history {
		if msg.Role == "tool" {
			continue
		}
		edited = append(edited, msg)
	}
	sim.history = providers.CloneChatMessages(edited)
	// Drop the now-orphaned assistant tool call as a real editor would.
	filtered := sim.history[:0]
	for _, msg := range sim.history {
		if len(msg.ToolCalls) > 0 {
			continue
		}
		filtered = append(filtered, msg)
	}
	sim.history = filtered

	sim.runTurn("ask 2", &fakeStep{results: toolRoundSteps("call_2")}, func(cfg *LoopConfig) {
		cfg.Tools = experimentTools()
		cfg.BeforeRequestContext = stableAndTurnEnvContext(2)
	})
	// Turn 2's requests must carry each block exactly once (fresh emission,
	// no splice, no duplicates) and chain among themselves.
	turn2First := sim.requests[len(sim.requests)-2]
	if got := countMessagesContaining(turn2First, "[ACTIVE_FILES]"); got != 1 {
		t.Fatalf("fallback should inject the stable block exactly once, got %d in %+v", got, turn2First)
	}
	turn2 := sim.requests[len(sim.requests)-2:]
	if breaks := analyzePrefixChain(turn2); len(breaks) != 0 {
		t.Fatalf("post-fallback rounds must chain:\n%s", formatBreaks(turn2, breaks))
	}
}

// Scenario 8: compact.PruneToolResults determinism — pruning the same
// logical transcript twice yields identical bytes (the loop depends on this
// for prefix stability, since it re-prunes from originals each round).
func TestPrefixExperiment_PruneIsDeterministicOverGrowingInput(t *testing.T) {
	big := strings.Repeat("y", 300_000)
	base := []providers.ChatMessage{
		userMsg("t0"),
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c0", Name: "read_file", Arguments: `{}`}}},
		{Role: "tool", Name: "read_file", ToolCallID: "c0", Content: big},
		userMsg("t1"),
		userMsg("t2"),
	}
	first := compact.PruneToolResults(base)
	second := compact.PruneToolResults(base)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("PruneToolResults must be deterministic for identical input")
	}
	grown := append(providers.CloneChatMessages(base),
		providers.ChatMessage{Role: "assistant", Content: "more"},
	)
	regrown := compact.PruneToolResults(grown)
	for i := range first {
		if !reflect.DeepEqual(first[i], regrown[i]) {
			t.Fatalf("pruned prefix must stay byte-stable as input grows (message %d changed)", i)
		}
	}
}
