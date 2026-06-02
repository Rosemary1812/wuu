package appserver

import (
	"context"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

// threadTitleSystemPrompt is copied from
// thirdparty/opencode/packages/opencode/src/agent/prompt/title.txt so our
// generated titles match opencode's quality and behavior (single-line,
// language-preserving, no refusals, ≤100 characters).
//
// Keep this in sync with opencode when bumping the upstream prompt.
const threadTitleSystemPrompt = `You are a title generator. You output ONLY a thread title. Nothing else.

<task>
Generate a brief title that would help the user find this conversation later.

Follow all rules in <rules>
Use the <examples> so you know what a good title looks like.
Your output must be:
- A single line
- ≤50 characters
- No explanations
</task>

<rules>
- you MUST use the same language as the user message you are summarizing
- Title must be grammatically correct and read naturally - no word salad
- Never include tool names in the title (e.g. "read tool", "bash tool", "edit tool")
- Focus on the main topic or question the user needs to retrieve
- Vary your phrasing - avoid repetitive patterns like always starting with "Analyzing"
- When a file is mentioned, focus on WHAT the user wants to do WITH the file, not just that they shared it
- Keep exact: technical terms, numbers, filenames, HTTP codes
- Remove: the, this, my, a, an
- Never assume tech stack
- Never use tools
- NEVER respond to questions, just generate a title for the conversation
- The title should NEVER include "summarizing" or "generating" when generating a title
- DO NOT SAY YOU CANNOT GENERATE A TITLE OR COMPLAIN ABOUT THE INPUT
- Always output something meaningful, even if the input is minimal.
- If the user message is short or conversational (e.g. "hello", "lol", "what's up", "hey"):
  → create a title that reflects the user's tone or intent (such as Greeting, Quick check-in, Light chat, Intro message, etc.)
</rules>

<examples>
"debug 500 errors in production" → Debugging production 500 errors
"refactor user service" → Refactoring user service
"why is app.js failing" → app.js failure investigation
"implement rate limiting" → Rate limiting implementation
"how do I connect postgres to my API" → Postgres API connection
"best practices for React hooks" → React hooks best practices
"@src/auth.ts can you add refresh token support" → Auth refresh token support
"@utils/parser.ts this is broken" → Parser bug fix
"look at @config.json" → Config review
"@App.tsx add dark mode toggle" → Dark mode toggle in App
</examples>`

// titleGenerationTimeout caps the streaming call for a single title attempt.
// Long enough for a thinking model to reason briefly, short enough that a
// stalled provider does not strand the goroutine.
const titleGenerationTimeout = 60 * time.Second

// titleMaxRuneLength matches opencode's 100-character cap. Keep this aligned
// with thirdparty/opencode/packages/opencode/src/session/prompt.ts so we don't
// silently diverge.
const titleMaxRuneLength = 100

// recommendedTitleTemperature mirrors opencode's per-model temperature mapping
// in thirdparty/opencode/packages/opencode/src/provider/transform.ts
// (`temperature()`). The second return value reports whether a temperature
// should be sent at all; when false the caller MUST omit the field so models
// that pin their temperature (e.g. kimi-k2.6) accept the request.
func recommendedTitleTemperature(modelID string) (float64, bool) {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if id == "" {
		return 0, false
	}
	if strings.Contains(id, "qwen") {
		return 0.55, true
	}
	if strings.Contains(id, "claude") {
		// Claude rejects temperature when reasoning is enabled and tolerates
		// the default otherwise; opencode opts to never send one.
		return 0, false
	}
	if strings.Contains(id, "gemini") {
		return 1.0, true
	}
	if strings.Contains(id, "glm-4.6") || strings.Contains(id, "glm-4.7") {
		return 1.0, true
	}
	if strings.Contains(id, "minimax-m2") {
		return 1.0, true
	}
	if strings.Contains(id, "kimi-k2") {
		// kimi-k2-thinking, kimi-k2.5/.6/.7, kimi-k2p5, kimi-k2-5 all want 1.0.
		for _, marker := range []string{"thinking", "k2.", "k2p", "k2-5"} {
			if strings.Contains(id, marker) {
				return 1.0, true
			}
		}
		return 0.6, true
	}
	return 0, false
}

// generateThreadTitle asks the title model for a short, sidebar-friendly title
// for the freshly started conversation.
//
// Implementation notes (aligned with opencode's SessionPrompt.ensureTitle):
//   - We stream the response so providers that reject non-streaming requests
//     (kimi-k2.6 returns "Stream must be set to true", and other reasoning
//     models exhibit similar behavior) still work.
//   - Temperature follows opencode's recommendedTitleTemperature mapping
//     instead of a hardcoded value, so pinned-temperature models accept the
//     request rather than 400-ing.
//   - We do not constrain max_tokens: the system prompt already bounds the
//     output, and capping output blocks thinking models from ever emitting a
//     final answer (reasoning consumes the entire budget).
//   - We aggregate text deltas only; thinking deltas (<think>…</think>) are
//     stripped again post-hoc in cleanGeneratedThreadTitle.
func (s *Server) generateThreadTitle(threadID string, history []providers.ChatMessage) {
	if s == nil || s.rt == nil {
		return
	}
	client := s.titleStreamClient()
	if client == nil {
		return
	}
	th := s.thread(threadID)
	if th == nil {
		return
	}

	th.mu.Lock()
	if th.ReadOnly || th.ParentID != "" || strings.TrimSpace(th.Title) != "" {
		th.mu.Unlock()
		return
	}
	th.mu.Unlock()

	firstUser, ok := firstUserMessageForTitle(history)
	if !ok {
		return
	}
	model := ""
	if s.rt.StreamRunner != nil {
		model = firstNonEmpty(s.rt.StreamRunner.APIModel, s.rt.StreamRunner.Model)
	}
	model = firstNonEmpty(model, s.rt.Model)
	if model == "" {
		return
	}

	temp, sendTemp := recommendedTitleTemperature(model)
	req := providers.ChatRequest{
		Model: model,
		Messages: []providers.ChatMessage{
			{Role: "system", Content: threadTitleSystemPrompt},
			{Role: "user", Content: "Generate a title for this conversation:\n" + firstUser},
		},
	}
	if sendTemp {
		req.Temperature = temp
	}

	ctx, cancel := context.WithTimeout(context.Background(), titleGenerationTimeout)
	defer cancel()

	text, err := streamTitleText(ctx, client, req)
	if err != nil {
		providers.DebugLogf("generate title for thread %q: %v", threadID, err)
		return
	}
	title := cleanGeneratedThreadTitle(text)
	if title == "" {
		return
	}
	metadata, err := session.UpdateGeneratedTitle(s.rt.SessionDir, threadID, title)
	if err != nil {
		providers.DebugLogf("persist generated title for thread %q: %v", threadID, err)
		return
	}
	thread, err := s.threadAfterMetadataUpdate(metadata)
	if err != nil {
		providers.DebugLogf("load generated title for thread %q: %v", threadID, err)
		return
	}
	_ = s.writeNotification(NotificationThreadUpdated, ThreadUpdatedNotification{Thread: thread})
}

// titleStreamClient resolves the streaming client used for title generation.
// We require an explicitly configured TitleClient (production wires this to
// the main provider client; tests inject their own) and wrap it with
// providers.AdaptStreamClient so non-streaming fakes still work via the
// synthetic stream adapter.
//
// We intentionally do NOT fall back to StreamRunner.Client when TitleClient is
// nil. Falling back would inject a parallel request into the main provider
// during the first turn (consuming prepared test responses, costing real
// users an extra call, and competing for rate limit headroom). The
// production runtime always populates TitleClient, so a nil here means "no
// title client configured" and we should silently skip.
func (s *Server) titleStreamClient() providers.StreamClient {
	if s.rt.TitleClient == nil {
		return nil
	}
	return providers.AdaptStreamClient(s.rt.TitleClient)
}

// streamTitleText opens a streaming chat and aggregates content deltas until
// the model finishes. Thinking deltas are intentionally ignored: opencode does
// the same (Stream.filter(textDelta)) and we additionally strip <think> blocks
// from the final text as belt-and-suspenders.
func streamTitleText(ctx context.Context, client providers.StreamClient, req providers.ChatRequest) (string, error) {
	events, err := client.StreamChat(ctx, req)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for ev := range events {
		switch ev.Type {
		case providers.EventContentDelta:
			b.WriteString(ev.Content)
		case providers.EventContentReplace:
			b.Reset()
			b.WriteString(ev.Content)
		case providers.EventMessage:
			if ev.Message != nil && ev.Message.Content != "" {
				b.Reset()
				b.WriteString(ev.Message.Content)
			}
		case providers.EventError:
			if ev.Error != nil {
				return "", ev.Error
			}
		case providers.EventDone:
			// keep draining to let the producer close cleanly
		}
	}
	return b.String(), ctx.Err()
}

// firstUserMessageForTitle returns the user prompt to summarize. opencode only
// generates a title for the very first user turn (`history.filter(real).length
// !== 1` returns); we mirror that constraint with the count == 1 check.
func firstUserMessageForTitle(history []providers.ChatMessage) (string, bool) {
	var first string
	count := 0
	for _, msg := range history {
		if msg.Role != "user" || isToolResultMessage(msg) {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		count++
		if count == 1 {
			first = content
		}
	}
	return first, count == 1 && first != ""
}

// cleanGeneratedThreadTitle strips reasoning fences, optional list/title
// prefixes, and surrounding quotes before truncating to the opencode-aligned
// 100-rune cap.
func cleanGeneratedThreadTitle(text string) string {
	text = stripThinkBlocks(text)
	for _, line := range strings.Split(text, "\n") {
		title := strings.TrimSpace(line)
		// Bullet prefix.
		title = strings.TrimPrefix(title, "- ")
		title = strings.TrimPrefix(title, "* ")
		// English "Title:" prefix (model habit).
		for _, prefix := range []string{"Title:", "title:", "TITLE:"} {
			if strings.HasPrefix(title, prefix) {
				title = strings.TrimSpace(strings.TrimPrefix(title, prefix))
			}
		}
		// Chinese 标题: / 标题: prefixes (half-width + full-width colon).
		for _, prefix := range []string{"标题:", "标题:", "题目:", "题目:"} {
			if strings.HasPrefix(title, prefix) {
				title = strings.TrimSpace(strings.TrimPrefix(title, prefix))
			}
		}
		title = strings.Trim(strings.TrimSpace(title), "\"'`“”‘’《》「」『』")
		if title == "" {
			continue
		}
		runes := []rune(title)
		if len(runes) > titleMaxRuneLength {
			title = string(runes[:titleMaxRuneLength])
		}
		return strings.TrimSpace(title)
	}
	return ""
}

// stripThinkBlocks removes inline <think>…</think> reasoning fences emitted by
// models that surface chain-of-thought as plain text instead of a structured
// reasoning channel.
func stripThinkBlocks(text string) string {
	for {
		lower := strings.ToLower(text)
		start := strings.Index(lower, "<think>")
		if start < 0 {
			return text
		}
		endRel := strings.Index(lower[start:], "</think>")
		if endRel < 0 {
			return text[:start]
		}
		end := start + endRel + len("</think>")
		text = text[:start] + text[end:]
	}
}
