package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/blueberrycongee/wuu/internal/compact"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
)

const (
	volatileTurnUserRole = "user"
)

// buildCacheHint derives a light-weight, provider-agnostic cache hint
// from the current conversation state.
//
// Design goals:
//   - keep it conversation-shaped, not provider-shaped
//   - preserve a stable prefix across the tool loop of the current turn
//   - let a compacted summary become the new stable history root
//     without introducing a heavier session-part model
//
// The stable prefix is everything before the most recent user-role message in
// the request. Request-only reminders can refine that boundary: stable
// prompt-submit reminders such as slash-command context may join the cached
// prefix for the current turn, while volatile runtime reminders such as
// environment and task-contract blocks remain outside it.
//
// After compact rewrites history, the synthetic conversation summary at
// the front of the prompt becomes the best stable anchor we have. We
// treat that summary-bearing system message as part of the cache root so
// providers can keep reusing it across the remainder of the session.
//
// PromptCacheKey falls back to a hash derived from the stable history
// root. Thread-aware runners override it with the durable thread ID so
// OpenAI-compatible prompt-cache routing stays stable across compaction
// and ordinary turns, matching Codex's thread-scoped key design.
func buildCacheHint(messages []providers.ChatMessage) *providers.CacheHint {
	if len(messages) == 0 {
		return nil
	}

	systemCount := systemMessageCount(messages)
	lastUser := lastUserMessageIndex(messages)
	stablePrefixMessages := stablePrefixMessageCount(messages, systemCount, lastUser, requestOnlyReminderSuffixStart(messages))
	hasCompactSummary := leadingSystemHasCompactSummary(messages)

	hint := &providers.CacheHint{
		StableSystem:         systemCount > 0,
		StablePrefixMessages: stablePrefixMessages,
		HasCompactSummary:    hasCompactSummary,
	}
	hint.PromptCacheKey = buildPromptCacheKey(messages, systemCount, stablePrefixMessages)

	if !hint.StableSystem && hint.StablePrefixMessages == 0 && hint.PromptCacheKey == "" && !hint.HasCompactSummary {
		return nil
	}
	return hint
}

func applyPromptCacheKeyOverride(hint **providers.CacheHint, key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if *hint == nil {
		*hint = &providers.CacheHint{}
	}
	(*hint).PromptCacheKey = key
}

func systemMessageCount(messages []providers.ChatMessage) int {
	count := 0
	for _, msg := range messages {
		if !strings.EqualFold(msg.Role, "system") {
			break
		}
		count++
	}
	return count
}

func lastUserMessageIndex(messages []providers.ChatMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, volatileTurnUserRole) {
			return i
		}
	}
	return -1
}

func stablePrefixMessageCount(messages []providers.ChatMessage, systemCount, lastUser, reminderSuffixStart int) int {
	if reminderSuffixStart >= 0 {
		boundary := len(messages)
		for i := reminderSuffixStart; i < len(messages); i++ {
			if isVolatileRequestOnlyReminder(messages[i]) {
				boundary = i
				break
			}
		}
		stable := boundary - systemCount
		if stable < 0 {
			return 0
		}
		return stable
	}
	if lastUser < 0 {
		stable := len(messages) - systemCount
		if stable < 0 {
			return 0
		}
		return stable
	}
	stable := lastUser - systemCount
	if stable < 0 {
		return 0
	}
	return stable
}

func requestOnlyReminderSuffixStart(messages []providers.ChatMessage) int {
	start := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if !wuucontext.IsSystemReminder(msg.Name, msg.Content) {
			break
		}
		start = i
	}
	if start == len(messages) {
		return -1
	}
	return start
}

func isVolatileRequestOnlyReminder(msg providers.ChatMessage) bool {
	if !wuucontext.IsSystemReminder(msg.Name, msg.Content) {
		return false
	}
	for _, line := range strings.Split(msg.Content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "source: runtime.") {
			return true
		}
	}
	return false
}

func leadingSystemHasCompactSummary(messages []providers.ChatMessage) bool {
	for _, msg := range messages {
		if !strings.EqualFold(msg.Role, "system") {
			break
		}
		if compact.IsConversationSummaryContent(msg.Content) {
			return true
		}
	}
	return false
}

func buildPromptCacheKey(messages []providers.ChatMessage, systemCount, stablePrefixMessages int) string {
	var b strings.Builder
	b.WriteString("wuu:v2\n")

	added := false
	for i := 0; i < systemCount && i < len(messages); i++ {
		writeCacheKeyMessage(&b, messages[i])
		added = true
	}
	if stablePrefixMessages > 0 {
		idx := systemCount
		if idx >= 0 && idx < len(messages) {
			writeCacheKeyMessage(&b, messages[idx])
			added = true
		}
	}
	if !added && len(messages) > 0 {
		writeCacheKeyMessage(&b, messages[0])
		added = true
	}
	if !added {
		return ""
	}

	sum := sha256.Sum256([]byte(b.String()))
	return "wuu-" + hex.EncodeToString(sum[:16])
}

func writeCacheKeyMessage(b *strings.Builder, msg providers.ChatMessage) {
	b.WriteString(strings.ToLower(strings.TrimSpace(msg.Role)))
	b.WriteByte('\n')
	b.WriteString(msg.Content)
	b.WriteByte('\n')
	if len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			b.WriteString(tc.Name)
			b.WriteByte('\n')
			b.WriteString(tc.Arguments)
			b.WriteByte('\n')
		}
	}
	if msg.ToolCallID != "" {
		b.WriteString(msg.ToolCallID)
		b.WriteByte('\n')
	}
}
