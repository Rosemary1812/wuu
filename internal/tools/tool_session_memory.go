package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/sessionmemory"
	"github.com/blueberrycongee/wuu/internal/stringutil"
)

const (
	sessionMemoryName             = "session_memory"
	sessionMemoryActionStatus     = "status"
	sessionMemoryActionRead       = "read"
	sessionMemoryActionAppend     = "append"
	sessionMemoryActionReplace    = "replace"
	sessionMemoryToolReadMaxBytes = 64 * 1024
)

var allowedSessionMemoryActions = []string{
	sessionMemoryActionStatus,
	sessionMemoryActionRead,
	sessionMemoryActionAppend,
	sessionMemoryActionReplace,
}

var allowedSessionMemoryTargets = []string{
	sessionmemory.TargetProjectMemory,
	sessionmemory.TargetSummary,
	sessionmemory.TargetCheckpoint,
	sessionmemory.TargetNotes,
}

type SessionMemoryTool struct {
	env *Env
}

type sessionMemoryArgs struct {
	Action  string `json:"action"`
	Target  string `json:"target,omitempty"`
	Content string `json:"content,omitempty"`
	Source  string `json:"source,omitempty"`
}

func NewSessionMemoryTool(env *Env) *SessionMemoryTool {
	return &SessionMemoryTool{env: env}
}

func (t *SessionMemoryTool) Name() string { return sessionMemoryName }

func (t *SessionMemoryTool) IsReadOnly() bool        { return false }
func (t *SessionMemoryTool) IsConcurrencySafe() bool { return false }

func (t *SessionMemoryTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: sessionMemoryName,
		Description: "Read or update durable workspace/session memory files that are injected as per-turn context. " +
			"Use target=\"summary\" for compact recoverable state of the active task, target=\"checkpoint\" only for legacy checkpoint compatibility, target=\"notes\" for session scratch notes, " +
			"and target=\"project_memory\" only for stable workspace facts that should survive across future sessions. " +
			"Do not store raw transcripts, secrets, temporary task progress, PR numbers, commit SHAs, or facts likely to go stale within a week. " +
			"Prefer append for incremental notes; use replace when consolidating a summary or project memory after review.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"action"},
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        allowedSessionMemoryActions,
					"description": "status lists memory files; read returns one target; append adds an entry; replace overwrites one target with consolidated markdown.",
				},
				"target": map[string]any{
					"type":        "string",
					"enum":        allowedSessionMemoryTargets,
					"description": "Required for read, append, and replace. project_memory is workspace-level; summary, checkpoint, and notes are session-level.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Markdown content for append or replace. Write compact durable facts, not instructions to yourself.",
				},
				"source": map[string]any{
					"type":        "string",
					"description": "Optional source label for append entries, for example agent, dream, workflow, or user.",
				},
			},
		},
	}
}

func (t *SessionMemoryTool) Classify(argsJSON string) ToolClassification {
	var args sessionMemoryArgs
	_ = json.Unmarshal([]byte(argsJSON), &args)
	switch strings.TrimSpace(args.Action) {
	case sessionMemoryActionStatus, sessionMemoryActionRead:
		return ToolClassification{
			ReadOnly:        true,
			ConcurrencySafe: true,
			Risk:            ToolRiskLow,
			Reason:          "session memory read",
		}
	case sessionMemoryActionAppend, sessionMemoryActionReplace:
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskMedium,
			Reason:          "session memory write affects future context",
		}
	default:
		return ToolClassification{
			ReadOnly:        false,
			ConcurrencySafe: false,
			Risk:            ToolRiskMedium,
			Reason:          "session memory action not recognized",
		}
	}
}

func (t *SessionMemoryTool) Execute(ctx context.Context, args string) (string, error) {
	_ = ctx
	if t == nil || t.env == nil {
		return "", fmt.Errorf("session_memory: env is not configured")
	}
	var a sessionMemoryArgs
	if err := decodeArgs(args, &a); err != nil {
		return "", fmt.Errorf("session_memory: %w", err)
	}
	action, err := normalizeSessionMemoryAction(a.Action)
	if err != nil {
		return "", fmt.Errorf("session_memory: %w", err)
	}
	stateDir, err := t.env.WorkspaceStateDir()
	if err != nil {
		return "", fmt.Errorf("session_memory: %w", err)
	}
	sessionDir := strings.TrimSpace(t.env.SessionDir)

	switch action {
	case sessionMemoryActionStatus:
		status, err := sessionmemory.Status(stateDir, sessionDir)
		if err != nil {
			return "", fmt.Errorf("session_memory: %w", err)
		}
		dream, err := sessionmemory.LoadDreamState(stateDir)
		if err != nil {
			return "", fmt.Errorf("session_memory: %w", err)
		}
		return mustJSON(map[string]any{
			"action": sessionMemoryActionStatus,
			"dream":  dream,
			"files":  status,
		})
	case sessionMemoryActionRead:
		target, err := normalizeSessionMemoryTarget(a.Target)
		if err != nil {
			return "", fmt.Errorf("session_memory: %w", err)
		}
		path, content, exists, err := sessionmemory.ReadTarget(stateDir, sessionDir, target)
		if err != nil {
			return "", fmt.Errorf("session_memory: %w", err)
		}
		truncated := false
		if len(content) > sessionMemoryToolReadMaxBytes {
			content = stringutil.HeadTail(content, sessionMemoryToolReadMaxBytes/2, sessionMemoryToolReadMaxBytes/2, "\n\n[trimmed session memory]\n\n")
			truncated = true
		}
		return mustJSON(map[string]any{
			"action":    sessionMemoryActionRead,
			"target":    target,
			"path":      path,
			"exists":    exists,
			"content":   content,
			"truncated": truncated,
		})
	case sessionMemoryActionAppend:
		target, err := normalizeSessionMemoryTarget(a.Target)
		if err != nil {
			return "", fmt.Errorf("session_memory: %w", err)
		}
		content := strings.TrimSpace(a.Content)
		if content == "" {
			return "", fmt.Errorf("session_memory: content is required for append")
		}
		if err := scanMemoryContent(content); err != nil {
			return "", fmt.Errorf("session_memory: %w", err)
		}
		path, length, err := sessionmemory.AppendTarget(stateDir, sessionDir, target, content, a.Source, time.Now().UTC())
		if err != nil {
			return "", fmt.Errorf("session_memory: %w", err)
		}
		return mustJSON(map[string]any{
			"action":  sessionMemoryActionAppend,
			"target":  target,
			"path":    path,
			"written": true,
			"length":  length,
		})
	case sessionMemoryActionReplace:
		target, err := normalizeSessionMemoryTarget(a.Target)
		if err != nil {
			return "", fmt.Errorf("session_memory: %w", err)
		}
		content := strings.TrimSpace(a.Content)
		if content == "" {
			return "", fmt.Errorf("session_memory: content is required for replace")
		}
		if err := scanMemoryContent(content); err != nil {
			return "", fmt.Errorf("session_memory: %w", err)
		}
		path, length, err := sessionmemory.ReplaceTarget(stateDir, sessionDir, target, content)
		if err != nil {
			return "", fmt.Errorf("session_memory: %w", err)
		}
		return mustJSON(map[string]any{
			"action":  sessionMemoryActionReplace,
			"target":  target,
			"path":    path,
			"written": true,
			"length":  length,
		})
	default:
		return "", fmt.Errorf("session_memory: unsupported action %q", action)
	}
}

func normalizeSessionMemoryAction(action string) (string, error) {
	action = strings.TrimSpace(action)
	for _, allowed := range allowedSessionMemoryActions {
		if action == allowed {
			return action, nil
		}
	}
	return "", fmt.Errorf("action %q is not one of %v", action, allowedSessionMemoryActions)
}

func normalizeSessionMemoryTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	for _, allowed := range allowedSessionMemoryTargets {
		if target == allowed {
			return target, nil
		}
	}
	return "", fmt.Errorf("target %q is not one of %v", target, allowedSessionMemoryTargets)
}
