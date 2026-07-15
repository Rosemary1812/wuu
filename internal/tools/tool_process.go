package tools

import (
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentthread"
	proc "github.com/blueberrycongee/wuu/internal/process"
)

const maxStartProcessInitialWait = 60 * time.Second

// The managed-process helpers below are shared infrastructure for the unified
// bash tool's background actions (see tool_bash.go). The former standalone
// process tools (start_process / list_processes / stop_process /
// read_process_output / write_stdin) were removed; bash is the only
// model-facing entry point for background processes.

type startProcessResponse struct {
	proc.Process
	InitialOutput       string   `json:"initial_output,omitempty"`
	InitialTruncated    bool     `json:"initial_truncated,omitempty"`
	InitialStartOffset  int64    `json:"initial_start_offset,omitempty"`
	InitialEndOffset    int64    `json:"initial_end_offset,omitempty"`
	InitialTotalBytes   int64    `json:"initial_total_bytes,omitempty"`
	InitialTimedOut     bool     `json:"initial_timed_out,omitempty"`
	InitialDurationMS   int64    `json:"initial_duration_ms,omitempty"`
	DetectedPreviewURLs []string `json:"detected_preview_urls,omitempty"`
	NextSuggestions     []string `json:"next_suggestions,omitempty"`
}

func defaultProcessOwnerKind(env *Env, ownerKind string) string {
	ownerKind = strings.TrimSpace(ownerKind)
	if ownerKind != "" {
		return ownerKind
	}
	if currentAgentPath(env) != agentthread.RootPath {
		return string(proc.OwnerSubagent)
	}
	return string(proc.OwnerMainAgent)
}

func defaultProcessOwnerID(env *Env) string {
	if env == nil {
		return "main"
	}
	if id := strings.TrimSpace(env.AgentID); id != "" {
		return id
	}
	if id := strings.TrimSpace(env.SessionID); id != "" {
		return id
	}
	return "main"
}

func redactProcessPtr(env *Env, p *proc.Process) *proc.Process {
	if p == nil {
		return nil
	}
	redacted := redactProcess(env, *p)
	return &redacted
}

func redactProcess(env *Env, p proc.Process) proc.Process {
	p.Command = env.RedactToolOutput(p.Command)
	p.LastError = env.RedactToolOutput(p.LastError)
	return p
}

func markProcessCompletionObserved(m *proc.Manager, p proc.Process) proc.Process {
	if m == nil || p.TerminalCause != proc.EventCauseNaturalExit ||
		(p.Status != proc.StatusStopped && p.Status != proc.StatusFailed) {
		return p
	}
	if updated, err := m.MarkCompletionDelivered(p.ID, "bash_result"); err == nil && updated != nil {
		return *updated
	}
	return p
}
