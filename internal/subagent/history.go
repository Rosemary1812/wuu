package subagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// ResumeSnapshotVersion is the schema version of the persisted run record.
// Snapshots written at this version (or newer) carry every field needed to
// rebuild the runtime for a cross-restart resume. Records with a lower
// version (including 0 / missing, i.e. written before resume support) load
// fine but cannot be resumed — the resume path rejects them explicitly.
const ResumeSnapshotVersion = 1

// historyRecord is the JSON shape we write per sub-agent. It captures the
// metadata and conversation needed to render a worker session and, from
// ResumeSnapshotVersion onward, to rebuild the runtime for a follow-up
// after a process restart.
type historyRecord struct {
	Version       int                     `json:"version"`
	ID            string                  `json:"id"`
	Type          string                  `json:"type"`
	TaskName      string                  `json:"task_name,omitempty"`
	AgentProfile  string                  `json:"agent_profile,omitempty"`
	AgentPath     string                  `json:"agent_path,omitempty"`
	ParentID      string                  `json:"parent_id,omitempty"`
	ParticipantID string                  `json:"participant_id,omitempty"`
	Description   string                  `json:"description"`
	Status        string                  `json:"status"`
	StartedAt     time.Time               `json:"started_at"`
	CompletedAt   time.Time               `json:"completed_at"`
	CWD           string                  `json:"cwd,omitempty"`
	Model         string                  `json:"model"`
	ModelPin      string                  `json:"model_pin,omitempty"`
	Prompt        string                  `json:"prompt"`
	Result        string                  `json:"result,omitempty"`
	Error         string                  `json:"error,omitempty"`
	Messages      []providers.ChatMessage `json:"messages,omitempty"`
}

// PersistedRun is a decoded sub-agent snapshot. The agentcontrol layer
// loads it to validate and rebuild a resumable runtime; the subagent
// package uses it to seed Manager.Restore.
type PersistedRun struct {
	Version       int
	ID            string
	Type          string
	TaskName      string
	AgentProfile  string
	AgentPath     string
	ParentID      string
	ParticipantID string
	Description   string
	Status        Status
	StartedAt     time.Time
	CompletedAt   time.Time
	CWD           string
	Model         string
	ModelPin      string
	Prompt        string
	Result        string
	Error         string
	Messages      []providers.ChatMessage
}

// LoadPersistedRun reads and decodes a sub-agent snapshot from disk. Old
// snapshots that predate the resume fields decode without error, leaving
// the new fields at their zero values (Version 0).
func LoadPersistedRun(path string) (PersistedRun, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PersistedRun{}, err
	}
	var rec historyRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return PersistedRun{}, err
	}
	return PersistedRun{
		Version:       rec.Version,
		ID:            rec.ID,
		Type:          rec.Type,
		TaskName:      rec.TaskName,
		AgentProfile:  rec.AgentProfile,
		AgentPath:     rec.AgentPath,
		ParentID:      rec.ParentID,
		ParticipantID: rec.ParticipantID,
		Description:   rec.Description,
		Status:        Status(rec.Status),
		StartedAt:     rec.StartedAt,
		CompletedAt:   rec.CompletedAt,
		CWD:           rec.CWD,
		Model:         rec.Model,
		ModelPin:      rec.ModelPin,
		Prompt:        rec.Prompt,
		Result:        rec.Result,
		Error:         rec.Error,
		Messages:      rec.Messages,
	}, nil
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
		Version:       ResumeSnapshotVersion,
		ID:            sa.ID,
		Type:          sa.Type,
		TaskName:      sa.TaskName,
		AgentProfile:  sa.AgentProfile,
		AgentPath:     sa.AgentPath,
		ParentID:      sa.ParentID,
		ParticipantID: sa.ParticipantID,
		Description:   sa.Description,
		Status:        string(sa.Status),
		StartedAt:     sa.StartedAt,
		CompletedAt:   sa.CompletedAt,
		CWD:           sa.workerRoot,
		Model:         sa.model,
		ModelPin:      sa.modelPin,
		Prompt:        sa.prompt,
		Result:        sa.Result,
		Messages:      providers.CloneChatMessages(sa.history),
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
