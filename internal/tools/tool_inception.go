package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/compact"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type InceptionTool struct{ env *Env }

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
			"The summary is not a transcript and not a user-facing report. It must extract only the state worth keeping: current task state, files or external systems already changed, verification results, concrete evidence pointers, rejected paths that matter, and the next action. Omit raw logs, tool-call IDs, detours, commentary about compression, and anything that does not help future reasoning. " +
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
					"type":        "string",
					"description": "A markdown semantic-compression summary that replaces the removed suffix. Include only durable state needed to continue: current task state, external side effects that remain true, verification status, evidence pointers such as files/commands/results, rejected paths worth avoiding, and the next concrete action. Do not mention inception, compression, rewritten history, checkpoints, tool-call IDs, or that history was removed. Do not write a user-facing explanation or final answer.",
				},
			},
			"required": []string{"anchor_id", "summary"},
		},
	}
}

func (t *InceptionTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args inceptionArgs
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if args.AnchorID == nil {
		return "", errors.New("inception: anchor_id is required")
	}
	anchorID := *args.AnchorID
	if anchorID < 0 {
		return "", errors.New("inception: anchor_id must be non-negative")
	}
	summary := strings.TrimSpace(args.Summary)
	if summary == "" {
		return "", errors.New("inception: summary is required")
	}
	history := agent.HistoryFromContext(ctx)
	if len(history) == 0 {
		return "", errors.New("inception: parent history is unavailable")
	}
	if _, ok := compact.FindContextAnchorIndex(history, anchorID); !ok {
		return "", fmt.Errorf("inception: anchor %d not found", anchorID)
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
		return "", err
	}
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
	AnchorID *int   `json:"anchor_id"`
	Summary  string `json:"summary"`
}

type inceptionResponse struct {
	Action         string                           `json:"action"`
	Status         string                           `json:"status"`
	HistoryRewrite *compact.InceptionHistoryRewrite `json:"history_rewrite,omitempty"`
}
