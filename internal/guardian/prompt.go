package guardian

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/blueberrycongee/wuu/internal/tools"
)

// Truncation budgets, aligned in spirit with Codex's GUARDIAN_MAX_* limits
// (see thirdparty/codex/codex-rs/core/src/guardian/mod.rs) but expressed in
// characters (≈ 4 chars per token) so we do not need to pull in a tokenizer
// just to size prompts. The numbers below are deliberately conservative;
// they can be tuned once we have real-world prompt-size telemetry.
const (
	// MaxTranscriptEntries caps the number of recent transcript entries
	// included in the prompt. Older entries are dropped first.
	MaxTranscriptEntries = 40

	// MaxEntryChars caps each transcript entry's content (≈ 2000 tokens).
	// Longer entries are truncated with a marker.
	MaxEntryChars = 8000

	// MaxActionChars caps the tool-call argument preview (≈ 4000 tokens).
	// Roughly half of Codex's GUARDIAN_MAX_ACTION_STRING_TOKENS so the
	// prompt fits comfortably inside a typical 8k-context reviewer model.
	MaxActionChars = 16000

	// MaxTranscriptChars caps the total transcript length after per-entry
	// truncation. Older entries are dropped if the budget is exceeded.
	MaxTranscriptChars = 40000
)

const truncationMarker = "\n...[truncated]"

// promptTemplate is the guardian system prompt. It is intentionally simple:
// text/template + a const string keeps the prompt close to the code that
// consumes it, which is easier to tune than a separate embedded file.
const promptTemplate = `You are a tool call reviewer for a coding agent running in "approve for me" mode.

The user has chosen to let an automatic reviewer handle routine approval prompts
instead of being interrupted, but anything dangerous or ambiguous must still be
denied so the model can pick a safer alternative or stop.

## Planned tool call

- Tool name: {{.ToolName}}
- Tool kind: {{.Kind}}
- Risk level (pre-classified): {{.Risk}}
- Read-only: {{if .ReadOnly}}yes{{else}}no{{end}}
- Destructive: {{if .Destructive}}yes{{else}}no{{end}}
- Policy reason: {{.PolicyReason}}
- Classification reason: {{.ClassificationReason}}

Arguments (truncated, render verbatim):

    {{.ArgumentsPreview}}

## Recent conversation (truncated, oldest dropped first)

{{if .TranscriptEntries}}{{range .TranscriptEntries}}- [{{.Role}}] {{.Content}}
{{end}}{{else}}(no prior conversation available; judge from the tool call alone){{end}}
## Decision framework

Approve ONLY when ALL of the following hold:
- The action matches what the user just asked for in the conversation above.
- The action is reversible or has low blast radius (workspace file edits,
  running tests, reading code, installing dev-only dependencies).
- The action does not write outside the current workspace, touch .git metadata,
  or modify user secrets / system paths.

Deny when ANY of the following hold:
- Destructive shell commands (rm -rf, dd, force-push, chmod 777, mkfs, etc.).
- Writing outside the workspace or to system paths (/etc, /usr, ~/.ssh, etc.).
- Network exfiltration, uploading data, or commands that contradict the user's
  stated intent.
- High blast radius with no recent user signal justifying it.

When signals conflict, err on the side of denying and let the agent pick a safer alternative. User confirmation must be explicit (not suggestive or implicit) to override a block; if in doubt, deny.

## Output

Respond with a single JSON object and nothing else:

    {"decision": "approved" | "denied", "risk_level": "low" | "medium" | "high" | "critical", "rationale": "<one sentence>"}

Do not include any other text, code fences, or commentary.
`

var promptTmpl = template.Must(template.New("guardian").Parse(promptTemplate))

// promptEntry is the per-line transcript shape rendered by promptTemplate.
type promptEntry struct {
	Role    string
	Content string
}

// promptData is the value passed to promptTemplate.
type promptData struct {
	ToolName             string
	Kind                 string
	Risk                 string
	ReadOnly             bool
	Destructive          bool
	PolicyReason         string
	ClassificationReason string
	ArgumentsPreview     string
	TranscriptEntries    []promptEntry
}

// BuildPrompt renders the guardian prompt for the given approval request,
// using the supplied transcript for context. The transcript is truncated per
// MaxTranscriptEntries / MaxEntryChars / MaxTranscriptChars before rendering;
// the action preview is truncated per MaxActionChars.
//
// The function only returns an error on template execution failure, which
// indicates a bug in the prompt template rather than a runtime condition.
func BuildPrompt(req tools.ToolApprovalReviewRequest, transcript Transcript) (string, error) {
	truncated := truncateTranscript(transcript)
	entries := make([]promptEntry, 0, len(truncated.Entries))
	for _, e := range truncated.Entries {
		entries = append(entries, promptEntry{
			Role:    string(e.Role),
			Content: e.Content,
		})
	}
	data := promptData{
		ToolName:             strings.TrimSpace(req.ToolName),
		Kind:                 string(req.Kind),
		Risk:                 string(req.Risk),
		ReadOnly:             req.ReadOnly,
		Destructive:          req.Destructive,
		PolicyReason:         strings.TrimSpace(req.PolicyReason),
		ClassificationReason: strings.TrimSpace(req.ClassificationReason),
		ArgumentsPreview:     truncateString(req.ArgumentsPreview, MaxActionChars),
		TranscriptEntries:    entries,
	}
	var buf strings.Builder
	if err := promptTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render guardian prompt: %w", err)
	}
	return buf.String(), nil
}

// truncateString returns s shortened to at most max chars. When truncation
// occurs and there is room, a truncation marker is appended so callers can
// tell that content was dropped. max <= 0 returns an empty string.
func truncateString(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	if max <= len(truncationMarker) {
		return s[:max]
	}
	return s[:max-len(truncationMarker)] + truncationMarker
}

// truncateTranscript drops entries from the front until the transcript fits
// within the per-entry and total character budgets. Entries are returned in
// the original order. The returned Transcript always has a non-nil Entries
// slice so callers can iterate without a nil check.
func truncateTranscript(t Transcript) Transcript {
	if len(t.Entries) == 0 {
		return Transcript{Entries: []TranscriptEntry{}}
	}
	// 1. Cap entry count: keep the last N entries.
	entries := t.Entries
	if len(entries) > MaxTranscriptEntries {
		entries = entries[len(entries)-MaxTranscriptEntries:]
	}
	// 2. Truncate each entry's content to MaxEntryChars.
	truncated := make([]TranscriptEntry, 0, len(entries))
	for _, e := range entries {
		truncated = append(truncated, TranscriptEntry{
			Role:    e.Role,
			Content: truncateString(e.Content, MaxEntryChars),
		})
	}
	// 3. If total chars exceed the budget, drop entries from the front
	//    until we fit. We always keep at least one entry so the reviewer
	//    still sees the most recent user signal.
	total := 0
	for _, e := range truncated {
		total += len(e.Content)
	}
	for total > MaxTranscriptChars && len(truncated) > 1 {
		total -= len(truncated[0].Content)
		truncated = truncated[1:]
	}
	return Transcript{Entries: truncated}
}
