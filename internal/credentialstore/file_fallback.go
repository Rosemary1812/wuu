package credentialstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/blueberrycongee/wuu/internal/securefs"
)

type FileStore struct {
	path string
	mu   *sync.Mutex
}

var fileStoreLocks sync.Map

func NewFileStore(path string) *FileStore {
	lock, _ := fileStoreLocks.LoadOrStore(path, &sync.Mutex{})
	return &FileStore{path: path, mu: lock.(*sync.Mutex)}
}

func (s *FileStore) Get(_ context.Context, namespace, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.load()
	if err != nil {
		return nil, err
	}
	encoded, ok := values[credentialAccount(namespace, key)]
	if !ok {
		return nil, ErrNotFound
	}
	value, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("credentialstore file decode: %w", err)
	}
	return value, nil
}

func (s *FileStore) Set(_ context.Context, namespace, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.load()
	if errors.Is(err, ErrNotFound) {
		values = map[string]string{}
	} else if err != nil {
		return err
	}
	values[credentialAccount(namespace, key)] = base64.StdEncoding.EncodeToString(value)
	return s.write(values)
}

func (s *FileStore) Delete(_ context.Context, namespace, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.load()
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	delete(values, credentialAccount(namespace, key))
	return s.write(values)
}

func (s *FileStore) load() (map[string]string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("credentialstore file read: %w", err)
	}
	values := map[string]string{}
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("credentialstore file parse: %w", err)
	}
	return values, nil
}

func (s *FileStore) write(values map[string]string) error {
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return fmt.Errorf("credentialstore file marshal: %w", err)
	}
	if err := securefs.WriteFileAtomic(s.path, append(data, '\n')); err != nil {
		return fmt.Errorf("credentialstore file write: %w", err)
	}
	return nil
}
