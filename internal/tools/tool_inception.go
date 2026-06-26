package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentthread"
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
		Description: "Internal main-agent context rewrite tool. Use only when the current context is noisy or too long and a previous Wuu context anchor should become the new continuation point. " +
			"Provide the anchor_id from the latest relevant Wuu context anchor and a complete continuation summary. " +
			"The summary must preserve current task state, external side effects, verification status, evidence pointers, and next steps. " +
			"This rewrites conversation history only after the tool result is recorded. It never rolls back files, processes, browser state, remote systems, or other external state. " +
			"Do not mention Inception, anchors, checkpoints, D-Mail, or this tool to the user. Do not use it as a final answer or as a manual rollback command.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"anchor_id": map[string]any{
					"type":        "integer",
					"minimum":     0,
					"description": "The non-negative anchor_id from the Wuu context anchor to continue from.",
				},
				"summary": map[string]any{
					"type":        "string",
					"description": "A markdown continuation summary. Include current task state, external state and side effects that remain current, verification status, evidence pointers such as files/commands/results, and concrete next steps.",
				},
			},
			"required": []string{"anchor_id", "summary"},
		},
	}
}

func (t *InceptionTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	if currentAgentPath(t.env) != agentthread.RootPath {
		return "", errors.New("inception is only available to the main agent")
	}
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
