package compact

import (
	"context"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// helpMeJournalMaxTokens caps the model output of one extraction round. The
// journal is bounded to helpMeCompactMaxSectionBytes characters by prompt and
// by truncation; this only needs enough headroom for that budget.
const helpMeJournalMaxTokens = 2048

// helpMeJournalInstructionPrompt frames the parent-journal extraction. This
// is deliberately NOT the continuation-style compact prompt: compaction
// summarizes for resuming the same line of work, while this extraction
// produces a faithful decision journal for a fresh agent taking over from a
// possibly-stuck one — failures are the most valuable content and must be
// preserved as explicit negative knowledge, not smoothed away.
const helpMeJournalInstructionPrompt = `You are extracting a decision journal from a coding agent's transcript. The agent may be stuck or working from wrong assumptions; a fresh agent will take over using your journal, so it must be a faithful record of what actually happened, not a cleaned-up story.

Rules:
- Be faithful to the transcript. Do not soften, omit, or reframe failures; failed work is the most valuable content here.
- Label every abandoned or unsuccessful path explicitly as "ruled out" (evidence shows it is wrong) or "low-confidence" (unproven or suspicious).
- Quote the user's own words for the goal wherever possible instead of paraphrasing.
- Record each decision with the reason it was made at the time, even if that reason later proved wrong.
- Respond with text only. Do not call tools, request tool use, or emit an analysis block, XML tags, preamble, or outro.
- Keep the whole journal under 6000 characters. When trimming, drop routine detail first; never drop failures, decisions, or exact errors.

Structure the journal with exactly these sections:

### User goal
The user's goal, quoting their original words where possible, plus explicit acceptance criteria if any.

### Key decisions
Each significant decision point: what was decided, why, and what evidence it rested on.

### Paths taken
Every distinct approach attempted, in order, each ending with its outcome and a "ruled out", "low-confidence", or "still open" label. Include exact errors, failing commands, and file paths where they matter.

### Current state
Where things stand now: files changed, verification results, and what remains unknown.`

// BuildHelpMeParentJournal machine-extracts a decision journal (goal, key
// decisions, paths taken with outcomes) from the parent agent's history. It
// is the primary handoff carrier for a HelpMe rescue: unlike the parent's
// self-reported brief, the extraction does not inherit the stuck agent's
// framing. Long histories run through the same chunked map-reduce shape as
// compaction — each model call sees one chunk plus the rolling journal — so
// the extraction never needs the whole history in one window. The journal is
// bounded to helpMeCompactMaxSectionBytes.
func BuildHelpMeParentJournal(ctx context.Context, client providers.Client, model string, budget Budget, history []providers.ChatMessage) (string, error) {
	return BuildHelpMeParentJournalWithOptions(ctx, client, model, budget, nil, history)
}

func BuildHelpMeParentJournalWithOptions(ctx context.Context, client providers.Client, model string, budget Budget, options map[string]any, history []providers.ChatMessage) (string, error) {
	if client == nil {
		return "", nil
	}
	_, _, _, conversation := splitLeadingSystemMessages(history)
	conversation = stripHistoricalImages(conversation)
	if len(conversation) == 0 {
		return "", nil
	}
	ctx, cancel := withCompactTimeout(ctx)
	defer cancel()

	inputBudget := compactSummaryInputBudgetForBudget(model, budget)
	journal := ""
	remaining := conversation
	for len(remaining) > 0 {
		n := helpMeJournalChunkSize(remaining, journal, inputBudget)
		if n <= 0 {
			n = 1
		}
		next, err := extractHelpMeJournalChunk(ctx, client, model, options, remaining[:n], journal)
		if err != nil {
			return "", err
		}
		journal = truncateHelpMeSection(strings.TrimSpace(next))
		remaining = remaining[n:]
	}
	return journal, nil
}

// helpMeJournalChunkSize mirrors compactSummaryChunkSize for the extraction
// prompt: it keeps whole messages while the rendered prompt (instruction +
// rolling journal + transcript chunk) fits the input budget, and always
// advances by at least one message.
func helpMeJournalChunkSize(messages []providers.ChatMessage, previousJournal string, inputBudget int) int {
	if len(messages) <= 1 {
		return len(messages)
	}
	if inputBudget <= 0 {
		inputBudget = compactSummaryInputMinTokens
	}
	total := EstimateTokens(buildHelpMeJournalPrompt(nil, previousJournal))
	keep := 0
	for keep < len(messages) {
		next := compactSummaryPromptMessageTokens(messages[keep])
		if keep > 0 && total+next > inputBudget {
			break
		}
		total += next
		keep++
	}
	if keep == 0 {
		return 1
	}
	return keep
}

func buildHelpMeJournalPrompt(chunk []providers.ChatMessage, previousJournal string) string {
	var b strings.Builder
	b.WriteString(helpMeJournalInstructionPrompt)
	previousJournal = strings.TrimSpace(previousJournal)
	if previousJournal != "" {
		b.WriteString("\n\n--- Previous journal ---\n\n")
		b.WriteString(previousJournal)
		b.WriteString("\n\nUpdate the journal above using the additional transcript below. Keep every ruled-out and low-confidence path; merge new decisions and outcomes; return one complete replacement journal with the required sections.\n")
	}
	b.WriteString("\n--- Transcript to extract from ---\n\n")
	for _, msg := range chunk {
		writeSummaryPromptMessage(&b, msg)
	}
	return b.String()
}

func extractHelpMeJournalChunk(ctx context.Context, client providers.Client, model string, options map[string]any, chunk []providers.ChatMessage, previousJournal string) (string, error) {
	toExtract := chunk
	for attempt := 0; ; attempt++ {
		req := helpMeJournalRequest(model, buildHelpMeJournalPrompt(toExtract, previousJournal), options)
		resp, err := summarizeCompact(ctx, client, req)
		if err != nil {
			// Same backstop as summarizeCompactChunk: if the extraction
			// request itself overflowed, drop the oldest message from this
			// chunk and retry a bounded number of times.
			if providers.IsContextOverflow(err) && attempt < maxCompactRetries && len(toExtract) > 1 {
				toExtract = toExtract[1:]
				continue
			}
			return "", fmt.Errorf("helpme parent journal extraction failed: %w", err)
		}
		return resp.Content, nil
	}
}

func helpMeJournalRequest(model, prompt string, options map[string]any) providers.ChatRequest {
	return providers.ChatRequest{
		Model:     model,
		Operation: providers.NewInferenceOperation(providers.InferenceOperationReview, providers.InferenceProfileContinuationCritical),
		Messages: []providers.ChatMessage{
			{Role: "system", Content: helpMeJournalSystemPrompt},
			{Role: "user", Content: prompt},
		},
		Temperature:     0.2,
		MaxTokens:       helpMeJournalMaxTokens,
		ProviderOptions: compactSummaryProviderOptions(options),
	}
}

// helpMeJournalSystemPrompt is exported to the request only; kept as its own
// constant so callers (and tests) can recognize extraction requests.
const helpMeJournalSystemPrompt = "You extract faithful decision journals from coding-agent transcripts. Follow the user's required format exactly. Do not call tools."
