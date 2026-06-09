package sessiontrace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/stringutil"
	"github.com/blueberrycongee/wuu/internal/tools"
)

const finalAnswerPreviewBytes = 1200

var secretLikeRE = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|auth[_-]?token|bearer|password|secret)\s*[:= ]+\s*[^ \t\r\n]+`)

type Event struct {
	Type      string    `json:"type"`
	ThreadID  string    `json:"thread_id,omitempty"`
	TurnID    string    `json:"turn_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Data      any       `json:"data,omitempty"`
}

type TurnRecord struct {
	ThreadID         string     `json:"thread_id"`
	TurnID           string     `json:"turn_id"`
	Status           string     `json:"status"`
	ProviderName     string     `json:"provider_name,omitempty"`
	Model            string     `json:"model,omitempty"`
	APIModel         string     `json:"api_model,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	DurationMS       *int64     `json:"duration_ms,omitempty"`
	InputTokens      int        `json:"input_tokens,omitempty"`
	OutputTokens     int        `json:"output_tokens,omitempty"`
	HistoryRewritten bool       `json:"history_rewritten,omitempty"`
	Error            string     `json:"error,omitempty"`
}

type FinalRecord struct {
	Status             string `json:"status"`
	InputTokens        int    `json:"input_tokens,omitempty"`
	OutputTokens       int    `json:"output_tokens,omitempty"`
	FinalAnswerPreview string `json:"final_answer_preview,omitempty"`
	Error              string `json:"error,omitempty"`
}

func Path(sessionDir string) string {
	sessionDir = strings.TrimSpace(sessionDir)
	if sessionDir == "" {
		return ""
	}
	return filepath.Join(sessionDir, "session-trace.jsonl")
}

func AppendTurn(path string, turn TurnRecord, final FinalRecord, inventory []tools.ToolInfo, records []tools.ToolExecutionRecord) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	createdAt := time.Now().UTC()
	encoder := json.NewEncoder(file)
	events := []Event{{
		Type:      "turn",
		ThreadID:  turn.ThreadID,
		TurnID:    turn.TurnID,
		CreatedAt: createdAt,
		Data:      turn,
	}}
	if len(inventory) > 0 {
		events = append(events, Event{
			Type:      "tool_inventory",
			ThreadID:  turn.ThreadID,
			TurnID:    turn.TurnID,
			CreatedAt: createdAt,
			Data:      append([]tools.ToolInfo(nil), inventory...),
		})
	}
	if len(records) > 0 {
		events = append(events, Event{
			Type:      "tool_records",
			ThreadID:  turn.ThreadID,
			TurnID:    turn.TurnID,
			CreatedAt: createdAt,
			Data:      append([]tools.ToolExecutionRecord(nil), records...),
		})
	}
	final.FinalAnswerPreview = redactAndTruncate(final.FinalAnswerPreview)
	final.Error = redactAndTruncate(final.Error)
	events = append(events, Event{
		Type:      "final",
		ThreadID:  turn.ThreadID,
		TurnID:    turn.TurnID,
		CreatedAt: createdAt,
		Data:      final,
	})
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return nil
}

func redactAndTruncate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = secretLikeRE.ReplaceAllString(value, "$1=[REDACTED]")
	if len(value) <= finalAnswerPreviewBytes {
		return value
	}
	return stringutil.Truncate(value, finalAnswerPreviewBytes, "...[truncated]")
}
