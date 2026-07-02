package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type InceptionTool struct{ env *Env }

const (
	maxConsecutiveInceptionFailures = 3
	inceptionTemporarilyDisabled    = "inception is temporarily disabled for this session due to repeated failures; continue without compressing"
)

type inceptionFailureState struct {
	mu          sync.Mutex
	consecutive int
	disabled    bool
}

func NewInceptionTool(env *Env) *InceptionTool { return &InceptionTool{env: env} }

func (t *InceptionTool) Name() string            { return compact.InceptionToolName }
func (t *InceptionTool) IsReadOnly() bool        { return true }
func (t *InceptionTool) IsConcurrencySafe() bool { return false }

func (t *InceptionTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: compact.InceptionToolName,
		Description: "Compress the useful semantics from a noisy suffix of conversation history and replace that suffix with a concise continuation summary. Use it when recent reads, searches, command output, failed attempts, or debugging chatter have become low-value context, but the durable facts extracted from them are still useful. " +
			"You will see checkpoint IDs in `user` messages with content like <system>CHECKPOINT {id}</system>. Pass the checkpoint just before the low-value suffix as anchor_id. The system keeps all history before that checkpoint and replaces everything after it with your summary. " +
			"Default to the closest checkpoint before the noise. Choose an earlier checkpoint only when your summary preserves every durable fact needed to continue from the current external state. " +
			"The summary is a structured object, not a transcript and not a user-facing report. Fill delivered_to_user, progress, state, next_action, and open_questions so the next step can distinguish work already shown to the user from work still pending. Preserve uncertainty, open questions, and pending user confirmation as pending. Do not claim the user approved, authorized, or requested execution unless visible user history explicitly says so. Omit raw logs, tool-call IDs, detours, commentary about compression, and anything that does not help future reasoning. " +
			"This tool only rewrites conversation history. It does not roll back files, processes, browser state, remote systems, or any other external state. Use it only after a complete assistant/tool turn, when the working state is stable enough to continue from the summary. Do not explain this to the user or present it as a final answer.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"anchor_id": map[string]any{
					"type":        "integer",
					"minimum":     0,
					"description": "The non-negative anchor_id from the Wuu context checkpoint to continue from.",
				},
				"summary": map[string]any{
					"type":        "object",
					"description": "A structured semantic-compression summary that replaces the removed suffix. Do not mention inception, compression, rewritten history, checkpoints, tool-call IDs, or that history was removed. Do not write a user-facing explanation or final answer.",
					"properties": map[string]any{
						"delivered_to_user": map[string]any{
							"type":        "string",
							"description": "Conclusions or answers already shown to the user, so they are not delivered again.",
						},
						"progress": map[string]any{
							"type":        "array",
							"description": "Completed work and external side effects that remain true, including files changed, commands run, and results.",
							"items":       map[string]any{"type": "string"},
						},
						"state": map[string]any{
							"type":        "array",
							"description": "Current task state and concrete evidence pointers such as files, commands, results, and rejected paths worth avoiding.",
							"items":       map[string]any{"type": "string"},
						},
						"next_action": map[string]any{
							"type":        "string",
							"description": "The next concrete action to take after the rewrite.",
						},
						"open_questions": map[string]any{
							"type":        "array",
							"description": "Unresolved questions or pending user confirmations. Keep them pending; do not upgrade them into approval.",
							"items":       map[string]any{"type": "string"},
						},
					},
					"required":             []string{"delivered_to_user", "progress", "state", "next_action", "open_questions"},
					"additionalProperties": false,
				},
			},
			"required":             []string{"anchor_id", "summary"},
			"additionalProperties": false,
		},
	}
}

func (t *InceptionTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if t.isTemporarilyDisabled() {
		return "", errors.New(inceptionTemporarilyDisabled)
	}
	var args inceptionArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return t.fail(err)
	}
	if args.AnchorID == nil {
		return t.fail(errors.New("inception: anchor_id is required"))
	}
	anchorID := *args.AnchorID
	if anchorID < 0 {
		return t.fail(errors.New("inception: anchor_id must be non-negative"))
	}
	summary, err := parseInceptionSummary(args.Summary)
	if err != nil {
		return t.fail(err)
	}
	history := agent.HistoryFromContext(ctx)
	if len(history) == 0 {
		return t.fail(errors.New("inception: parent history is unavailable"))
	}
	if _, ok := compact.FindContextAnchorIndex(history, anchorID); !ok {
		return t.fail(fmt.Errorf("inception: anchor %d not found", anchorID))
	}

	content := compact.BuildInceptionContinuationContent(anchorID, summary)
	out, err := json.Marshal(inceptionResponse{
		Action: "inception",
		Status: "completed",
		HistoryRewrite: &compact.InceptionHistoryRewrite{
			Kind:     compact.InceptionHistoryRewriteKind,
			AnchorID: anchorID,
			Content:  content,
		},
	})
	if err != nil {
		return t.fail(err)
	}
	t.recordSuccess()
	return string(out), nil
}

func (t *InceptionTool) Classify(string) ToolClassification {
	return ToolClassification{
		ReadOnly:        true,
		ConcurrencySafe: false,
		Risk:            ToolRiskLow,
		Reason:          "rewrites conversation context only after the tool result is recorded",
	}
}

type inceptionArgs struct {
	AnchorID *int            `json:"anchor_id"`
	Summary  json.RawMessage `json:"summary"`
}

type inceptionSummary struct {
	DeliveredToUser string   `json:"delivered_to_user"`
	Progress        []string `json:"progress"`
	State           []string `json:"state"`
	NextAction      string   `json:"next_action"`
	OpenQuestions   []string `json:"open_questions"`
}

type inceptionResponse struct {
	Action         string                           `json:"action"`
	Status         string                           `json:"status"`
	HistoryRewrite *compact.InceptionHistoryRewrite `json:"history_rewrite,omitempty"`
}

func parseInceptionSummary(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", errors.New("inception: summary is required")
	}
	if !strings.HasPrefix(trimmed, "{") {
		return "", errors.New("inception: summary must be a structured object")
	}
	var summary inceptionSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		return "", fmt.Errorf("inception: parse structured summary: %w", err)
	}
	summary.Progress = cleanInceptionSummaryList(summary.Progress)
	summary.State = cleanInceptionSummaryList(summary.State)
	summary.OpenQuestions = cleanInceptionSummaryList(summary.OpenQuestions)
	summary.DeliveredToUser = strings.TrimSpace(summary.DeliveredToUser)
	summary.NextAction = strings.TrimSpace(summary.NextAction)
	if len(summary.State) == 0 && summary.NextAction == "" {
		return "", errors.New("inception: summary state or next_action is required")
	}
	return renderInceptionSummary(summary), nil
}

func cleanInceptionSummaryList(values []string) []string {
	out := values[:0]
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func renderInceptionSummary(summary inceptionSummary) string {
	var b strings.Builder
	writeInceptionSummaryTextSection(&b, "Delivered to user", summary.DeliveredToUser)
	writeInceptionSummaryListSection(&b, "Progress", summary.Progress)
	writeInceptionSummaryListSection(&b, "State", summary.State)
	writeInceptionSummaryTextSection(&b, "Next action", summary.NextAction)
	writeInceptionSummaryListSection(&b, "Open questions", summary.OpenQuestions)
	return strings.TrimSpace(b.String())
}

func writeInceptionSummaryTextSection(b *strings.Builder, title, value string) {
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	fmt.Fprintf(b, "## %s\n", title)
	if value == "" {
		b.WriteString("None.")
		return
	}
	b.WriteString(value)
}

func writeInceptionSummaryListSection(b *strings.Builder, title string, values []string) {
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	fmt.Fprintf(b, "## %s\n", title)
	if len(values) == 0 {
		b.WriteString("None.")
		return
	}
	for _, value := range values {
		fmt.Fprintf(b, "- %s\n", value)
	}
}

func (t *InceptionTool) isTemporarilyDisabled() bool {
	if t == nil || t.env == nil {
		return false
	}
	return t.env.inceptionState.isDisabled()
}

func (t *InceptionTool) fail(err error) (string, error) {
	if t != nil && t.env != nil {
		t.env.inceptionState.recordFailure()
	}
	return "", err
}

func (t *InceptionTool) recordSuccess() {
	if t != nil && t.env != nil {
		t.env.inceptionState.recordSuccess()
	}
}

func (s *inceptionFailureState) isDisabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.disabled
}

func (s *inceptionFailureState) recordFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disabled {
		return
	}
	s.consecutive++
	if s.consecutive >= maxConsecutiveInceptionFailures {
		s.disabled = true
	}
}

func (s *inceptionFailureState) recordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecutive = 0
}
