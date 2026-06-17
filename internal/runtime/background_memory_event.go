package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var backgroundMemoryEventMu sync.Mutex

type backgroundMemoryEvent struct {
	At      time.Time `json:"at"`
	Source  string    `json:"source"`
	Tool    string    `json:"tool"`
	Action  string    `json:"action,omitempty"`
	Target  string    `json:"target,omitempty"`
	Path    string    `json:"path,omitempty"`
	Written bool      `json:"written,omitempty"`
	Removed bool      `json:"removed,omitempty"`
	Length  int       `json:"length,omitempty"`
}

func recordBackgroundMemoryEvent(source, toolName, rawResult string) error {
	var parsed struct {
		Action  string `json:"action"`
		Target  string `json:"target"`
		Path    string `json:"path"`
		Written bool   `json:"written"`
		Removed bool   `json:"removed"`
		Length  int    `json:"length"`
	}
	if err := json.Unmarshal([]byte(rawResult), &parsed); err != nil {
		return nil
	}
	path := strings.TrimSpace(parsed.Path)
	if path == "" || (!parsed.Written && !parsed.Removed) {
		return nil
	}
	event := backgroundMemoryEvent{
		At:      time.Now().UTC(),
		Source:  strings.TrimSpace(source),
		Tool:    strings.TrimSpace(toolName),
		Action:  strings.TrimSpace(parsed.Action),
		Target:  strings.TrimSpace(parsed.Target),
		Path:    path,
		Written: parsed.Written,
		Removed: parsed.Removed,
		Length:  parsed.Length,
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	eventPath := filepath.Join(filepath.Dir(path), "events.jsonl")

	backgroundMemoryEventMu.Lock()
	defer backgroundMemoryEventMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(eventPath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(eventPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}
