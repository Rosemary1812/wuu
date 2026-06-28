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
		Description: "Rewind the conversation context to an earlier Wuu checkpoint and replace the discarded messages with a single message you compose. Use it when recent context has too much noise and a concise summary can stand in for it. " +
			"You will see checkpoint IDs in `user` messages with content like <system>CHECKPOINT {id}</system>. Pass the desired checkpoint id as anchor_id to rewind to that point. " +
			"The new message you compose is appended to the end of the rewound context, so the next request sees everything before the checkpoint plus your message. " +
			"Default to the closest checkpoint before the noisy content; rewind further only when the summary fully bridges the gap to current external state. " +
			"Typical scenarios: You read a file, ran a command, or fetched a web page and only a small part of the result is useful; rewind to the checkpoint just before that read and give your past self only the useful part. " +
			"You searched the web and the result is large. If you got what you need, rewind and include only the relevant findings; if not, rewind and tell your past self a better query to try. " +
			"You wrote code that did not work, spent many steps fixing it, and the struggle is not relevant to the goal. Rewind to before you wrote the code, give your past self the fixed version, and remind them the fix is already on the filesystem so they do not redo the work. " +
			"The message you send must give your past self everything they need to continue without repeating the work you already did: what you did and what changed, what you learned, and any concrete results worth keeping. " +
			"This tool only rewrites conversation history. It does not roll back files, processes, browser state, remote systems, or any other external state. " +
			"Call it only after a complete assistant/tool turn, when the working state is stable enough to continue from the summary. Do not wait until only the final answer is left — by then the intermediate noise is already mixed in. " +
			"Do not explain this to the user. Do not present it as a final answer, a user-facing feature, or a manual rollback command.",
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
