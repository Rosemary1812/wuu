package compact

import (
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestBuildHelpMeJointCompactContentIncludesTraceAndEvidence(t *testing.T) {
	content := BuildHelpMeJointCompactContent(HelpMeJointCompactInput{
		OriginalGoal:     "fix the broken login test",
		Reason:           "main agent tried the wrong auth path twice",
		FailedAttempts:   []string{"changed router guard; test still failed"},
		HelperStatus:     "completed",
		HelperAgentPath:  "/root/helpme_recovery",
		HelperResultPath: "$SESSION_DIR/agent-results/helpme.txt",
		HelperReportPath: "$SESSION_DIR/harness/reports/helpme.json",
		ReportSummary:    "real bug was token refresh ordering",
		ChangedFiles:     []string{"internal/auth/session.go"},
		Verification:     []string{"go test ./internal/auth"},
		ReportEvidence:   []string{"internal/auth/session.go:42 - refresh happens before validation"},
	})
	for _, want := range []string{
		HelpMeJointCompactPrefix,
		"fix the broken login test",
		"changed router guard",
		"$SESSION_DIR/agent-results/helpme.txt",
		"$SESSION_DIR/harness/reports/helpme.json",
		"internal/auth/session.go:42",
		"Correct continuation path",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("HelpMe compact missing %q:\n%s", want, content)
		}
	}
}

func TestIsHelpMeJointCompactContent(t *testing.T) {
	if !IsHelpMeJointCompactContent("  " + HelpMeJointCompactPrefix + "\nRecovered") {
		t.Fatal("expected HelpMe joint compact prefix to be recognized")
	}
	if IsHelpMeJointCompactContent(ConversationSummaryPrefix + "\nRecovered") {
		t.Fatal("conversation summary must not be classified as HelpMe joint compact")
	}
}

func TestRewriteHistoryFromHelpMeToolMessagesDropsToolChain(t *testing.T) {
	toolResult := providers.ChatMessage{
		Role:       "tool",
		Name:       HelpMeToolName,
		ToolCallID: "call_help",
		Content:    `{"action":"helpme","status":"completed","history_rewrite":{"kind":"helpme_joint_compact","content":"[HelpMe joint compact]\nRecovered"}}`,
	}
	history := []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "old ask"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_help", Name: HelpMeToolName}}},
		toolResult,
	}

	rewritten, ok, err := RewriteHistoryFromHelpMeToolMessages(history, []providers.ChatMessage{toolResult})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected rewrite")
	}
	if len(rewritten) != 4 {
		t.Fatalf("expected system prefix, HelpMe compact, inherited checkpoint, and unanswered user ask, got %+v", rewritten)
	}
	if rewritten[1].Role != "system" || !strings.Contains(rewritten[1].Content, "Recovered") {
		t.Fatalf("unexpected rewritten summary: %+v", rewritten[1])
	}
	if id, ok := ContextAnchorIDFromMessage(rewritten[2]); !ok || id != 0 {
		t.Fatalf("expected inherited checkpoint 0, got %+v", rewritten[2])
	}
	// "old ask" never got a visible assistant reply, so it must survive the
	// wholesale rewrite as the preserved unanswered user suffix.
	if rewritten[3].Role != "user" || rewritten[3].Content != "old ask" {
		t.Fatalf("expected unanswered user ask preserved after checkpoint, got %+v", rewritten[3])
	}
}

func TestRewriteHistoryWithHelpMeCompactCarriesMonotonicCheckpoint(t *testing.T) {
	history := []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		BuildContextAnchorMessage(0),
		{Role: "assistant", Content: "explored old path"},
		BuildContextAnchorMessage(4),
		{Role: "user", Content: "latest ask"},
	}
	oldNext := NextContextAnchorID(history)

	rewritten := RewriteHistoryWithHelpMeCompact(history, HelpMeJointCompactPrefix+"\nRecovered")
	if len(rewritten) != 4 {
		t.Fatalf("expected system prefix, HelpMe compact, inherited checkpoint, unanswered user ask; got %+v", rewritten)
	}
	if _, ok := FindContextAnchorIndex(rewritten, 4); ok {
		t.Fatalf("old checkpoint id must not survive HelpMe rewrite: %+v", rewritten)
	}
	id, ok := ContextAnchorIDFromMessage(rewritten[2])
	if !ok || id != oldNext || !rewritten[2].Hidden {
		t.Fatalf("expected inherited hidden checkpoint %d, got %+v", oldNext, rewritten[2])
	}
	if rewritten[3].Role != "user" || rewritten[3].Content != "latest ask" {
		t.Fatalf("expected unanswered user ask preserved after checkpoint, got %+v", rewritten[3])
	}
	if next := NextContextAnchorID(rewritten); next != oldNext+1 {
		t.Fatalf("next anchor id should remain monotonic after HelpMe rewrite, got %d want %d", next, oldNext+1)
	}
}

// TestRewriteHistoryWithHelpMeCompactPreservesUnansweredUserMessages locks
// the inception-aligned window semantics: user messages already answered by
// a visible assistant reply are discarded with the polluted context, while
// user messages that arrived afterwards (for example during await_agents)
// are re-appended after the inherited checkpoint.
func TestRewriteHistoryWithHelpMeCompactPreservesUnansweredUserMessages(t *testing.T) {
	history := []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "answered ask"},
		{Role: "assistant", Content: "tried the wrong path"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_await", Name: AwaitAgentsToolName}}},
		{Role: "user", Content: "interjection during await"},
		{Role: "user", Name: "hidden_note", Content: "hidden", Hidden: true},
	}

	rewritten := RewriteHistoryWithHelpMeCompact(history, HelpMeJointCompactPrefix+"\nRecovered")
	var preserved []string
	for _, msg := range rewritten {
		if msg.Role == "user" && !msg.Hidden {
			preserved = append(preserved, msg.Content)
		}
	}
	if len(preserved) != 1 || preserved[0] != "interjection during await" {
		t.Fatalf("expected only the unanswered interjection to survive, got %+v", rewritten)
	}
	anchorIndex := -1
	interjectionIndex := -1
	for i, msg := range rewritten {
		if _, ok := ContextAnchorIDFromMessage(msg); ok {
			anchorIndex = i
		}
		if msg.Content == "interjection during await" {
			interjectionIndex = i
		}
	}
	if anchorIndex == -1 || interjectionIndex == -1 || interjectionIndex < anchorIndex {
		t.Fatalf("expected preserved user message after the checkpoint (anchor=%d, user=%d): %+v", anchorIndex, interjectionIndex, rewritten)
	}
}

func TestRewriteHistoryFromAwaitAgentsToolMessagesAcceptsHelpMeRewrite(t *testing.T) {
	toolResult := providers.ChatMessage{
		Role:       "tool",
		Name:       AwaitAgentsToolName,
		ToolCallID: "call_await",
		Content:    `{"action":"await_agents","results":[{"agent_id":"agent-1","status":"completed"}],"history_rewrite":{"kind":"helpme_joint_compact","content":"[HelpMe joint compact]\nRecovered from awaited helper"}}`,
	}
	history := []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_await", Name: AwaitAgentsToolName}}},
		toolResult,
	}

	rewritten, ok, err := RewriteHistoryFromHelpMeToolMessages(history, []providers.ChatMessage{toolResult})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected await_agents HelpMe rewrite")
	}
	if len(rewritten) != 3 || rewritten[1].Role != "system" || !strings.Contains(rewritten[1].Content, "awaited helper") {
		t.Fatalf("unexpected rewritten history: %+v", rewritten)
	}
	if id, ok := ContextAnchorIDFromMessage(rewritten[2]); !ok || id != 0 {
		t.Fatalf("expected inherited checkpoint 0, got %+v", rewritten[2])
	}
}

func TestAwaitAgentsWithoutHelpMeRewriteIsIgnoredEvenWhenNonJSON(t *testing.T) {
	toolResult := providers.ChatMessage{
		Role:       "tool",
		Name:       AwaitAgentsToolName,
		ToolCallID: "call_await",
		Content:    "await_agents: agent control not configured",
	}

	_, ok, err := HelpMeRewriteFromToolMessages([]providers.ChatMessage{toolResult})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected await_agents result without history_rewrite to be ignored")
	}
}

// TestHelpMeWithoutRewriteIsIgnoredEvenWhenNonJSON locks the same guard for
// the helpme carrier itself: a plain-text helpme tool error must be skipped
// instead of failing the whole turn on json.Unmarshal.
func TestHelpMeWithoutRewriteIsIgnoredEvenWhenNonJSON(t *testing.T) {
	toolResult := providers.ChatMessage{
		Role:       "tool",
		Name:       HelpMeToolName,
		ToolCallID: "call_help",
		Content:    "helpme is only available to the main agent",
	}

	_, ok, err := HelpMeRewriteFromToolMessages([]providers.ChatMessage{toolResult})
	if err != nil {
		t.Fatalf("non-JSON helpme tool message must not error: %v", err)
	}
	if ok {
		t.Fatal("expected helpme result without history_rewrite to be ignored")
	}
}
