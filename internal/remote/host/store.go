package host

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/remote/secure"
)

// StoredDevice is one paired phone as remembered by the host.
type StoredDevice struct {
	Pub     string    `json:"pub"` // base64url Ed25519 public key
	Name    string    `json:"name,omitempty"`
	AddedAt time.Time `json:"added_at"`
}

type storeFile struct {
	V        int            `json:"v"`
	HostSeed string         `json:"host_seed"` // base64url Ed25519 seed
	HostName string         `json:"host_name,omitempty"`
	RelayURL string         `json:"relay_url,omitempty"`
	Devices  []StoredDevice `json:"devices,omitempty"`
}

// Store is the host-side credential file (remote.json in the wuu home). It
// holds the host's long-term identity, the relay it reports to, and the set
// of paired phones. The file is chmod 0600 like auth.json.
type Store struct {
	mu   sync.Mutex
	path string
	data storeFile
	id   *secure.Identity
}

// LoadOrCreateStore opens the store at path, minting a fresh host identity on
// first use.
func LoadOrCreateStore(path, hostName string) (*Store, error) {
	s := &Store{path: path}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &s.data); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		seed, err := base64.RawURLEncoding.DecodeString(s.data.HostSeed)
		if err != nil {
			return nil, fmt.Errorf("decode host seed: %w", err)
		}
		s.id, err = secure.IdentityFromSeed(seed)
		if err != nil {
			return nil, err
		}
		return s, nil
	case os.IsNotExist(err):
		id, err := secure.NewIdentity()
		if err != nil {
			return nil, err
		}
		s.id = id
		s.data = storeFile{
			V:        1,
			HostSeed: base64.RawURLEncoding.EncodeToString(id.Seed()),
			HostName: hostName,
		}
		if err := s.save(); err != nil {
			return nil, err
		}
		return s, nil
	default:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
}

func (s *Store) Identity() *secure.Identity { return s.id }

func (s *Store) HostName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.HostName
}

func (s *Store) RelayURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.RelayURL
}

func (s *Store) SetRelayURL(url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.RelayURL = url
	return s.saveLocked()
}

func (s *Store) SetHostName(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.HostName = name
	return s.saveLocked()
}

// AddDevice records a paired phone.
func (s *Store) AddDevice(pub []byte, name string, now time.Time) error {
	key := secure.EncodeKey(pub)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Devices {
		if s.data.Devices[i].Pub == key {
			s.data.Devices[i].Name = name
			return s.saveLocked()
		}
	}
	s.data.Devices = append(s.data.Devices, StoredDevice{Pub: key, Name: name, AddedAt: now.UTC()})
	return s.saveLocked()
}

// RemoveDevice forgets a paired phone.
func (s *Store) RemoveDevice(pub string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.data.Devices[:0]
	for _, d := range s.data.Devices {
		if d.Pub != pub {
			kept = append(kept, d)
		}
	}
	s.data.Devices = kept
	return s.saveLocked()
}

// IsPaired reports whether a device public key belongs to a paired phone.
func (s *Store) IsPaired(pub []byte) bool {
	key := secure.EncodeKey(pub)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.data.Devices {
		if d.Pub == key {
			return true
		}
	}
	return false
}

// Devices returns a copy of the paired device list.
func (s *Store) Devices() []StoredDevice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]StoredDevice(nil), s.data.Devices...)
}

func (s *Store) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return errors.New("store path is empty")
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
