package sessiontrace

import (
	"bufio"
	"encoding/json"
	"fmt"
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

type ReplaySummary struct {
	Path          string           `json:"path,omitempty"`
	Mode          string           `json:"mode"`
	EventCount    int              `json:"event_count"`
	EventTypes    map[string]int   `json:"event_types,omitempty"`
	Turns         []TurnRecord     `json:"turns,omitempty"`
	LatestTurn    *TurnRecord      `json:"latest_turn,omitempty"`
	ToolInventory []tools.ToolInfo `json:"tool_inventory,omitempty"`
	ToolNames     []string         `json:"tool_names,omitempty"`
	Final         *FinalRecord     `json:"final,omitempty"`
	Complete      bool             `json:"complete"`
	Warnings      []string         `json:"warnings,omitempty"`
}

func Path(sessionDir string) string {
	sessionDir = strings.TrimSpace(sessionDir)
	if sessionDir == "" {
		return ""
	}
	return filepath.Join(sessionDir, "session-trace.jsonl")
}

func ReplayTrace(path string) (ReplaySummary, error) {
	file, err := os.Open(path)
	if err != nil {
		return ReplaySummary{}, err
	}
	defer file.Close()

	summary := ReplaySummary{
		Path:       path,
		Mode:       "session_trace_replay",
		EventTypes: make(map[string]int),
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var event struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data,omitempty"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return ReplaySummary{}, fmt.Errorf("decode trace line %d: %w", line, err)
		}
		summary.EventCount++
		summary.EventTypes[event.Type]++
		if err := replayEvent(&summary, event.Type, event.Data); err != nil {
			return ReplaySummary{}, fmt.Errorf("replay trace line %d type %q: %w", line, event.Type, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return ReplaySummary{}, err
	}
	if len(summary.Turns) == 0 {
		return ReplaySummary{}, fmt.Errorf("trace %s has no turn event", path)
	}
	if summary.Final == nil {
		summary.Warnings = append(summary.Warnings, "trace has no final event")
	}
	summary.Complete = summary.EventTypes["turn"] > 0 && summary.EventTypes["final"] > 0
	return summary, nil
}

func replayEvent(summary *ReplaySummary, eventType string, data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	switch eventType {
	case "turn":
		var turn TurnRecord
		if err := json.Unmarshal(data, &turn); err != nil {
			return err
		}
		summary.Turns = append(summary.Turns, turn)
		summary.LatestTurn = &summary.Turns[len(summary.Turns)-1]
	case "tool_inventory":
		var inventory []tools.ToolInfo
		if err := json.Unmarshal(data, &inventory); err != nil {
			return err
		}
		summary.ToolInventory = inventory
	case "tool_records":
		var records []tools.ToolExecutionRecord
		if err := json.Unmarshal(data, &records); err != nil {
			return err
		}
		for _, record := range records {
			if strings.TrimSpace(record.Name) != "" {
				summary.ToolNames = append(summary.ToolNames, record.Name)
			}
		}
	case "final":
		var final FinalRecord
		if err := json.Unmarshal(data, &final); err != nil {
			return err
		}
		summary.Final = &final
	}
	return nil
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
