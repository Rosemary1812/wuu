package subagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// historyRecord is the JSON shape we write per sub-agent. It captures
// the metadata and conversation needed to render a worker session.
type historyRecord struct {
	ID          string                  `json:"id"`
	Type        string                  `json:"type"`
	TaskName    string                  `json:"task_name,omitempty"`
	AgentPath   string                  `json:"agent_path,omitempty"`
	ParentID    string                  `json:"parent_id,omitempty"`
	Description string                  `json:"description"`
	Status      string                  `json:"status"`
	StartedAt   time.Time               `json:"started_at"`
	CompletedAt time.Time               `json:"completed_at"`
	Model       string                  `json:"model"`
	Prompt      string                  `json:"prompt"`
	Result      string                  `json:"result,omitempty"`
	Error       string                  `json:"error,omitempty"`
	Messages    []providers.ChatMessage `json:"messages,omitempty"`
}

// persistHistory writes the sub-agent's final state to its configured
// HistoryPath. Errors are returned but typically ignored — persistence
// is best-effort.
func persistHistory(sa *SubAgent) error {
	if sa.historyPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(sa.historyPath), 0o755); err != nil {
		return err
	}

	sa.mu.Lock()
	rec := historyRecord{
		ID:          sa.ID,
		Type:        sa.Type,
		TaskName:    sa.TaskName,
		AgentPath:   sa.AgentPath,
		ParentID:    sa.ParentID,
		Description: sa.Description,
		Status:      string(sa.Status),
		StartedAt:   sa.StartedAt,
		CompletedAt: sa.CompletedAt,
		Model:       sa.model,
		Prompt:      sa.prompt,
		Result:      sa.Result,
		Messages:    append([]providers.ChatMessage(nil), sa.history...),
	}
	if sa.Error != nil {
		rec.Error = sa.Error.Error()
	}
	sa.mu.Unlock()

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sa.historyPath, data, 0o644)
}
