package credentialstore

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	keyring "github.com/zalando/go-keyring"
)

var errKeyringNotFound = keyring.ErrNotFound

type keyringBackend interface {
	Get(service, account string) (string, error)
	Set(service, account, value string) error
	Delete(service, account string) error
}

type systemKeyringBackend struct{}

func (systemKeyringBackend) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}
func (systemKeyringBackend) Set(service, account, value string) error {
	return keyring.Set(service, account, value)
}
func (systemKeyringBackend) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

type KeyringStore struct {
	service string
	backend keyringBackend
}

func NewKeyringStore(service string, backend keyringBackend) *KeyringStore {
	service = strings.TrimSpace(service)
	if service == "" {
		service = "wuu"
	}
	if backend == nil {
		backend = systemKeyringBackend{}
	}
	return &KeyringStore{service: service, backend: backend}
}

// NewDesktopStore deliberately has no secure-file fallback. A desktop build
// must surface Keychain/keyring failures to the user instead of silently
// writing OAuth credentials to disk.
func NewDesktopStore() (Store, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("desktop credential store requires macOS Keychain on %s", runtime.GOOS)
	}
	return NewKeyringStore("wuu", nil), nil
}

func (s *KeyringStore) Get(_ context.Context, namespace, key string) ([]byte, error) {
	value, err := s.backend.Get(s.service, credentialAccount(namespace, key))
	if errors.Is(err, errKeyringNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("credentialstore keyring get: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("credentialstore keyring decode: %w", err)
	}
	return data, nil
}

func (s *KeyringStore) Set(_ context.Context, namespace, key string, value []byte) error {
	if err := s.backend.Set(s.service, credentialAccount(namespace, key), base64.StdEncoding.EncodeToString(value)); err != nil {
		return fmt.Errorf("credentialstore keyring set: %w", err)
	}
	return nil
}

func (s *KeyringStore) Delete(_ context.Context, namespace, key string) error {
	err := s.backend.Delete(s.service, credentialAccount(namespace, key))
	if errors.Is(err, errKeyringNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("credentialstore keyring delete: %w", err)
	}
	return nil
}

func credentialAccount(namespace, key string) string {
	return strings.TrimSpace(namespace) + ":" + strings.TrimSpace(key)
}
