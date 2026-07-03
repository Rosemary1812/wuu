package agent

import (
	"testing"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestBuildCacheHint_DefaultsToStableHistoryBeforeCurrentTurn(t *testing.T) {
	hint := buildCacheHint([]providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "first ask"},
		{Role: "assistant", Content: "first answer"},
		{Role: "user", Content: "current ask"},
	})
	if hint == nil {
		t.Fatal("expected cache hint")
	}
	if !hint.StableSystem {
		t.Fatal("expected stable system to be enabled")
	}
	if hint.StablePrefixMessages != 2 {
		t.Fatalf("expected 2 stable non-system messages, got %d", hint.StablePrefixMessages)
	}
	if hint.TurnPrefixMessages != 3 {
		t.Fatalf("expected turn prefix through latest user, got %d", hint.TurnPrefixMessages)
	}
	if hint.HasCompactSummary {
		t.Fatal("did not expect compact summary flag")
	}
	if hint.PromptCacheKey == "" {
		t.Fatal("expected prompt cache key")
	}
}

func TestBuildCacheHint_OnlyCurrentTurnStillGetsPromptCacheKey(t *testing.T) {
	hint := buildCacheHint([]providers.ChatMessage{
		{Role: "user", Content: "current ask"},
	})
	if hint == nil {
		t.Fatal("expected cache hint")
	}
	if hint.StableSystem {
		t.Fatal("did not expect stable system without system message")
	}
	if hint.StablePrefixMessages != 0 {
		t.Fatalf("expected no stable prefix messages, got %d", hint.StablePrefixMessages)
	}
	if hint.TurnPrefixMessages != 1 {
		t.Fatalf("expected current user in turn prefix, got %d", hint.TurnPrefixMessages)
	}
	if hint.HasCompactSummary {
		t.Fatal("did not expect compact summary flag")
	}
	if hint.PromptCacheKey == "" {
		t.Fatal("expected prompt cache key for current conversation root")
	}
}

func TestBuildCacheHint_HiddenModelContextKeepsCurrentTurnVolatile(t *testing.T) {
	hint := buildCacheHint([]providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "current ask"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "read_file", Arguments: `{}`}}},
		{Role: "tool", ToolCallID: "call_1", Content: "file contents"},
		{Role: "user", Name: wuucontext.SystemReminderMessageName, Content: "<system-reminder>\nstate changed\n</system-reminder>", Hidden: true},
	})
	if hint == nil {
		t.Fatal("expected cache hint")
	}
	if !hint.StableSystem {
		t.Fatal("expected stable system to be enabled")
	}
	if hint.StablePrefixMessages != 0 {
		t.Fatalf("expected hidden context to keep current ask volatile, got %d", hint.StablePrefixMessages)
	}
	if hint.TurnPrefixMessages != 1 {
		t.Fatalf("expected turn prefix to stop at latest visible user, got %d", hint.TurnPrefixMessages)
	}
}

func TestBuildCacheHint_CompactSummaryBecomesStableAnchor(t *testing.T) {
	messages := []providers.ChatMessage{
		{Role: "system", Content: "You are wuu."},
		{Role: "system", Content: "[Conversation summary]\nOlder turns were compacted."},
		{Role: "user", Content: "keep working"},
	}

	hint := buildCacheHint(messages)
	if hint == nil {
		t.Fatal("expected cache hint")
	}
	if !hint.StableSystem {
		t.Fatal("expected stable system")
	}
	if !hint.HasCompactSummary {
		t.Fatal("expected compact summary flag")
	}
	if hint.StablePrefixMessages != 0 {
		t.Fatalf("expected current turn to stay volatile, got %d stable prefix messages", hint.StablePrefixMessages)
	}
	if hint.TurnPrefixMessages != 1 {
		t.Fatalf("expected compacted turn prefix through current user, got %d", hint.TurnPrefixMessages)
	}

	otherCurrentTurn := []providers.ChatMessage{
		{Role: "system", Content: "You are wuu."},
		{Role: "system", Content: "[Conversation summary]\nOlder turns were compacted."},
		{Role: "user", Content: "do something else now"},
	}
	otherHint := buildCacheHint(otherCurrentTurn)
	if otherHint == nil {
		t.Fatal("expected second cache hint")
	}
	if hint.PromptCacheKey != otherHint.PromptCacheKey {
		t.Fatalf("expected compact summary to dominate cache key across turns, got %q vs %q", hint.PromptCacheKey, otherHint.PromptCacheKey)
	}
}

// TestCacheStablePrefixFingerprint verifies that identical inputs produce
// identical fingerprints, and changing any component produces a different
// fingerprint.
func TestCacheStablePrefixFingerprint(t *testing.T) {
	tools := []providers.ToolDefinition{
		{
			Name:        "tool_a",
			Description: "Tool A",
			InputSchema: map[string]any{"type": "object"},
		},
	}
	system := []string{"You are helpful"}

	fp1 := cacheStablePrefixFingerprint("claude-3-5-sonnet", tools, system)
	fp2 := cacheStablePrefixFingerprint("claude-3-5-sonnet", tools, system)

	if fp1 != fp2 {
		t.Fatalf("identical inputs produced different fingerprints: %q vs %q", fp1, fp2)
	}

	// Change model
	fp3 := cacheStablePrefixFingerprint("claude-3-opus", tools, system)
	if fp1 == fp3 {
		t.Fatal("different models produced same fingerprint")
	}

	// Change tools
	otherTools := []providers.ToolDefinition{
		{
			Name:        "tool_b",
			Description: "Tool B",
			InputSchema: map[string]any{"type": "object"},
		},
	}
	fp4 := cacheStablePrefixFingerprint("claude-3-5-sonnet", otherTools, system)
	if fp1 == fp4 {
		t.Fatal("different tools produced same fingerprint")
	}

	// Change system
	otherSystem := []string{"You are unhelpful"}
	fp5 := cacheStablePrefixFingerprint("claude-3-5-sonnet", tools, otherSystem)
	if fp1 == fp5 {
		t.Fatal("different system produced same fingerprint")
	}

	// Tool order matters
	reorderedTools := []providers.ToolDefinition{
		tools[0],
	}
	if len(tools) > 1 {
		reorderedTools = append(reorderedTools, tools[1])
	}
	fp6 := cacheStablePrefixFingerprint("claude-3-5-sonnet", reorderedTools, system)
	// If tools slice only has one element, fp6 will be same as fp1
	if len(tools) == 1 && fp1 != fp6 {
		t.Fatal("reordered single tool changed fingerprint (unexpected)")
	}
}

// TestComponentFingerprint verifies that identical component values produce
// identical hashes, and different values produce different hashes.
func TestComponentFingerprint(t *testing.T) {
	hash1 := componentFingerprint("model", "claude-3-5-sonnet")
	hash2 := componentFingerprint("model", "claude-3-5-sonnet")

	if hash1 != hash2 {
		t.Fatalf("identical component values produced different hashes: %q vs %q", hash1, hash2)
	}

	hash3 := componentFingerprint("model", "claude-3-opus")
	if hash1 == hash3 {
		t.Fatal("different model names produced same hash")
	}

	// Different component names with same value should produce different hashes
	hash4 := componentFingerprint("tools", "claude-3-5-sonnet")
	if hash1 == hash4 {
		t.Fatal("different component names produced same hash")
	}
}
