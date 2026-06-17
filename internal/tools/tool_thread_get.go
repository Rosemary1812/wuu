package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

const threadGetName = "thread_get"

// ThreadGetTool looks up a session (thread) by ID and returns its full
// data: metadata + history records. The thread ID is the value copied
// from the desktop session tree's right-click menu; this tool is what
// makes "copy an ID, paste it to an agent" actually useful — the agent
// resolves the pasted ID back to the full conversation.
//
// Source of truth is the user-level SQLite sessions database
// (statepath.SessionsDir(statepath.Home(""))). Root thread IDs are
// equal to session IDs because handleThreadStart writes both with the
// same session.NewID() value.
type ThreadGetTool struct {
	env *Env
}

func NewThreadGetTool(env *Env) *ThreadGetTool {
	return &ThreadGetTool{env: env}
}

func (t *ThreadGetTool) Name() string { return threadGetName }

func (t *ThreadGetTool) IsReadOnly() bool        { return true }
func (t *ThreadGetTool) IsConcurrencySafe() bool { return true }

func (t *ThreadGetTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: threadGetName,
		Description: "Look up a session (thread) by ID and return its full data: metadata + history records. " +
			"Use this when a user pastes a thread ID (e.g. from the desktop session tree right-click menu) " +
			"and you need to inspect the conversation.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"thread_id"},
			"properties": map[string]any{
				"thread_id": map[string]any{
					"type":        "string",
					"description": "Thread (session) ID, e.g. 20260618-045400-3a8e9b1c0d2f",
				},
			},
		},
	}
}

func (t *ThreadGetTool) Classify(argsJSON string) ToolClassification {
	var args struct {
		ThreadID string `json:"thread_id"`
	}
	_ = json.Unmarshal([]byte(argsJSON), &args)
	trimmed := strings.TrimSpace(args.ThreadID)
	reason := "thread_get is read-only"
	if trimmed == "" {
		reason = "thread_get with empty thread_id returns ErrSessionNotFound before any IO"
	}
	return ToolClassification{
		ReadOnly:        true,
		ConcurrencySafe: true,
		Risk:            ToolRiskLow,
		Reason:          reason,
	}
}

func (t *ThreadGetTool) Execute(ctx context.Context, args string) (string, error) {
	_ = ctx
	if t == nil || t.env == nil {
		return "", fmt.Errorf("thread_get: env is not configured")
	}
	var a struct {
		ThreadID string `json:"thread_id"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return "", fmt.Errorf("thread_get: %w", err)
	}
	id := strings.TrimSpace(a.ThreadID)
	if id == "" {
		return "", fmt.Errorf("thread_get: thread_id is required")
	}

	home, err := statepath.Home("")
	if err != nil {
		return "", fmt.Errorf("thread_get: resolve wuu home: %w", err)
	}
	sessDir := statepath.SessionsDir(home)
	if sessDir == "" {
		return "", errors.New("thread_get: sessions dir is empty")
	}

	meta, ok, err := session.Find(sessDir, id)
	if err != nil {
		return "", fmt.Errorf("thread_get: lookup %q: %w", id, err)
	}
	if !ok {
		return "", fmt.Errorf("%w: %q", session.ErrSessionNotFound, id)
	}

	records, err := session.LoadHistoryRecords(sessDir, id, true)
	if err != nil {
		return "", fmt.Errorf("thread_get: load history %q: %w", id, err)
	}

	data, err := json.Marshal(map[string]any{
		"thread_id": id,
		"session":   meta,
		"history":   records,
	})
	if err != nil {
		return "", fmt.Errorf("thread_get: marshal result: %w", err)
	}
	return string(data), nil
}
