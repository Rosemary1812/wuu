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
		Description: "Internal main-agent D-Mail context rewind tool. Use it during long tasks when conversation after a Wuu context checkpoint produced a small stable result from a much larger noisy suffix, and a concise continuation summary can replace that suffix. " +
			"Use the checkpoint id N from a hidden <system>CHECKPOINT N</system> marker as anchor_id. " +
			"Typical triggers: you read a large file, command output, tool result, or web/search result and only a small part is useful; a broad investigation branch produced stable conclusions or rejected paths; a coding/debugging detour changed external state and the raw struggle is no longer needed; or message size is growing and future steps need conclusions rather than raw intermediate outputs. " +
			"If the large read/search was useful, summarize only the useful facts to the checkpoint before that read/search. If it was a dead end, tell your past self the better next query/path or what to avoid. If files or processes changed, preserve the current external state because this tool never rolls it back. " +
			"Choose the closest checkpoint before the waste you want to remove, unless an older checkpoint is needed and your summary fully bridges from there to the current external state. " +
			"Provide the anchor_id from that checkpoint and a complete future-self continuation summary. The next request keeps messages before the checkpoint and appends your summary. " +
			"The summary must preserve current task state, decisions, external side effects, verification status, evidence pointers, rejected paths worth avoiding, and next steps. " +
			"This rewrites conversation history only after the tool result is recorded. It never rolls back files, processes, browser state, remote systems, or other external state. " +
			"Use it only after a complete assistant/tool turn when the useful state is stable enough to continue from the summary; do not wait until only the final answer remains. " +
			"Do not mention Inception, checkpoints, D-Mail, or this tool to the user. Do not use it as a final answer, user feature, or manual rollback command.",
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
					"description": "A markdown continuation summary with enough state to continue without reading the removed suffix. Include current task state, external state and side effects that remain current, verification status, evidence pointers such as files/commands/results, rejected paths worth avoiding, and concrete next steps.",
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
