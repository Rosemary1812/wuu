package agentthread

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dir string) *Store {
	return &Store{dir: strings.TrimSpace(dir)}
}

func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

func (s *Store) UpsertThread(meta Metadata) error {
	if s == nil || s.dir == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create thread store: %w", err)
	}
	threads, err := s.loadThreadsLocked()
	if err != nil {
		return err
	}
	replaced := false
	for i := range threads {
		if threads[i].ID == meta.ID {
			threads[i] = meta
			replaced = true
			break
		}
	}
	if !replaced {
		threads = append(threads, meta)
	}
	sort.Slice(threads, func(i, j int) bool {
		return threads[i].Path < threads[j].Path
	})
	if err := s.writeThreadsLocked(threads); err != nil {
		return err
	}
	return s.appendEventLocked(Event{
		Type:      EventThreadUpsert,
		ThreadID:  meta.ID,
		Path:      meta.Path,
		Status:    meta.Status,
		Metadata:  &meta,
		CreatedAt: time.Now().UTC(),
	})
}

func (s *Store) RecordStatus(meta Metadata) error {
	if s == nil || s.dir == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create thread store: %w", err)
	}
	threads, err := s.loadThreadsLocked()
	if err != nil {
		return err
	}
	for i := range threads {
		if threads[i].ID == meta.ID {
			threads[i] = meta
			break
		}
	}
	if err := s.writeThreadsLocked(threads); err != nil {
		return err
	}
	return s.appendEventLocked(Event{
		Type:      EventStatusChange,
		ThreadID:  meta.ID,
		Path:      meta.Path,
		Status:    meta.Status,
		CreatedAt: time.Now().UTC(),
	})
}

func (s *Store) RecordEdgeStatus(meta Metadata) error {
	if s == nil || s.dir == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create thread store: %w", err)
	}
	threads, err := s.loadThreadsLocked()
	if err != nil {
		return err
	}
	for i := range threads {
		if threads[i].ID == meta.ID {
			threads[i] = meta
			break
		}
	}
	if err := s.writeThreadsLocked(threads); err != nil {
		return err
	}
	return s.appendEventLocked(Event{
		Type:       EventEdgeChange,
		ThreadID:   meta.ID,
		Path:       meta.Path,
		EdgeStatus: meta.Source.EdgeStatus,
		CreatedAt:  time.Now().UTC(),
	})
}

func (s *Store) AppendEvent(event Event) error {
	if s == nil || s.dir == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create thread store: %w", err)
	}
	return s.appendEventLocked(event)
}

func (s *Store) ListThreads() ([]Metadata, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadThreadsLocked()
}

func (s *Store) ReadEvents() ([]Event, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, "events.jsonl")
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open thread events: %w", err)
	}
	defer file.Close()
	var events []Event
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan thread events: %w", err)
	}
	return events, nil
}

func (s *Store) loadThreadsLocked() ([]Metadata, error) {
	path := filepath.Join(s.dir, "threads.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read thread index: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var threads []Metadata
	if err := json.Unmarshal(data, &threads); err != nil {
		return nil, fmt.Errorf("decode thread index: %w", err)
	}
	return threads, nil
}

func (s *Store) writeThreadsLocked(threads []Metadata) error {
	data, err := json.MarshalIndent(threads, "", "  ")
	if err != nil {
		return fmt.Errorf("encode thread index: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, ".threads.*.tmp")
	if err != nil {
		return fmt.Errorf("create thread index tmp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write thread index tmp: %w", err)
	}
	if _, err := tmp.Write([]byte("\n")); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write thread index newline: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close thread index tmp: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(s.dir, "threads.json")); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename thread index: %w", err)
	}
	return nil
}

func (s *Store) appendEventLocked(event Event) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	path := filepath.Join(s.dir, "events.jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open thread events: %w", err)
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(event); err != nil {
		return fmt.Errorf("write thread event: %w", err)
	}
	return nil
}
