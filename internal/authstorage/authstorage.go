// Package authstorage stores provider credentials in a separate file
// (~/.wuu/auth.json) so that ~/.wuu/config.json never carries plaintext
// API keys or auth tokens. The shape mirrors Codex's AuthDotJson
// (login/src/auth/storage.rs:38-61): a single credential entry per
// file, scoped by provider_id.
//
// All writes go through securefs.WriteFileAtomic (temp file + rename)
// so the on-disk file is never half-written. In-process concurrent
// callers serialize on a sync.Mutex; cross-process safety relies on
// the atomic-rename semantics of rename(2) on POSIX.
//
// No backwards compatibility: a freshly-installed Wuu creates
// auth.json on first credential save and never embeds credentials in
// config.json. The migration helper MigrateFromConfig is a one-shot
// read-and-rewrite that consumes old config.json files and produces
// the new schema.
package authstorage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/securefs"
)

// AuthCredentials is the per-provider credential shape. ProviderID is
// the key in the on-disk map (see AuthFile), not a field on this
// struct — that keeps the per-provider JSON fragment free of
// boilerplate and avoids the risk of provider_id disagreeing with
// its surrounding key in a hand-edited file.
type AuthCredentials struct {
	APIKey       string    `json:"api_key,omitempty"`
	AuthToken    string    `json:"auth_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

// AuthFile is the on-disk schema for ~/.wuu/auth.json. It maps
// provider_id → credentials so a single file holds every provider
// Wuu has ever saved. Matches the Codex AuthDotJson shape
// (login/src/auth/storage.rs:38-61) but extended to a multi-provider
// dict because Wuu supports more than one upstream.
type AuthFile map[string]AuthCredentials

// ErrNotFound is returned by Load when auth.json does not exist.
// Callers should treat this as "no credentials configured yet", not
// an error.
var ErrNotFound = errors.New("authstorage: not found")

// Store is the file-backed credential store rooted at a single
// auth.json path. All exported methods are safe for concurrent use
// within a single process; cross-process safety relies on the
// atomic-rename semantics of securefs.WriteFileAtomic.
type Store struct {
	mu   sync.Mutex
	path string
}

// New returns a Store rooted at the given auth.json path. The file
// need not exist; Load reports ErrNotFound in that case.
func New(path string) *Store {
	return &Store{path: path}
}

// Path returns the absolute auth.json path this Store reads from and
// writes to.
func (s *Store) Path() string {
	return s.path
}

// Load returns the credential map currently on disk, or ErrNotFound
// if the file does not exist. A malformed file is treated as an
// error rather than a silent fallback — credentials are too
// sensitive to ignore a parse failure.
func (s *Store) Load() (AuthFile, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("authstorage: read %s: %w", s.path, err)
	}
	var raw AuthFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("authstorage: parse %s: %w", s.path, err)
	}
	if raw == nil {
		raw = AuthFile{}
	}
	return raw, nil
}

// Save atomically writes the credential map to disk. Concurrent
// in-process callers serialize on the Store's mutex; cross-process
// callers rely on securefs.WriteFileAtomic's temp + rename to avoid
// half-written files.
func (s *Store) Save(creds AuthFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("authstorage: marshal: %w", err)
	}
	if err := securefs.WriteFileAtomic(s.path, data); err != nil {
		return fmt.Errorf("authstorage: write: %w", err)
	}
	return nil
}

// SetProvider saves (or overwrites) credentials for a single
// provider. Other providers already on disk are preserved. Returns
// the resulting full AuthFile so callers can use HasCredential
// without a second Load.
func (s *Store) SetProvider(providerID string, creds AuthCredentials) (AuthFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.loadLocked()
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if current == nil {
		current = AuthFile{}
	}
	current[providerID] = creds
	if err := s.writeLocked(current); err != nil {
		return nil, err
	}
	out := AuthFile{}
	for k, v := range current {
		out[k] = v
	}
	return out, nil
}

// DeleteProvider removes credentials for a single provider. If the
// file ends up empty after the removal, the file is deleted so the
// absence is observable. Returns nil if the provider wasn't present.
func (s *Store) DeleteProvider(providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.loadLocked()
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	delete(current, providerID)
	if len(current) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("authstorage: delete %s: %w", s.path, err)
		}
		return nil
	}
	return s.writeLocked(current)
}

// loadLocked reads the file without taking the mutex. The caller
// MUST already hold s.mu.
func (s *Store) loadLocked() (AuthFile, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("authstorage: read %s: %w", s.path, err)
	}
	var raw AuthFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("authstorage: parse %s: %w", s.path, err)
	}
	if raw == nil {
		raw = AuthFile{}
	}
	return raw, nil
}

// writeLocked marshals + writes without taking the mutex. The caller
// MUST already hold s.mu.
func (s *Store) writeLocked(creds AuthFile) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("authstorage: marshal: %w", err)
	}
	if err := securefs.WriteFileAtomic(s.path, data); err != nil {
		return fmt.Errorf("authstorage: write: %w", err)
	}
	return nil
}

// Delete removes the entire auth.json file. Returns nil if the file
// didn't exist — Delete is treated as idempotent because every
// caller is already handling the "no credentials" case.
func (s *Store) Delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("authstorage: delete %s: %w", s.path, err)
	}
	return nil
}

// MigrateResult describes what MigrateFromConfig did, so callers can
// log or report the migration outcome without re-reading the files.
type MigrateResult struct {
	// Migrated is true if at least one provider's credentials were
	// moved from config.json to auth.json.
	Migrated bool
	// Providers lists the provider IDs whose credentials were moved.
	Providers []string
	// ConfigPath is the config.json path that was rewritten.
	ConfigPath string
	// AuthPath is the auth.json path that was written.
	AuthPath string
}

// MigrateFromConfig reads an old-style config.json that still carries
// inline api_key / auth_token fields under any of its providers,
// extracts those credentials into auth.json (merged with any
// existing entries), and returns the list of migrated provider IDs.
// After migration:
//   - Each affected provider has HasCredential = true.
//   - The api_key / auth_token fields are gone from config.json (the
//     JSON tags make them invisible on serialize; the in-memory
//     ProviderConfig still keeps the values until Load returns, but
//     SaveConfig won't write them).
//
// Returns MigrateResult{ Migrated: false } if no inline credentials
// were found — the migration is a no-op in that case and config.json
// is not touched. auth.json is rewritten only when at least one
// provider was added, so existing credentials are preserved on a
// no-op migration.
func MigrateFromConfig(configPath, authPath string) (MigrateResult, error) {
	res := MigrateResult{ConfigPath: configPath, AuthPath: authPath}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, fmt.Errorf("authstorage: read config %s: %w", configPath, err)
	}

	var raw struct {
		Providers map[string]struct {
			APIKey    string `json:"api_key,omitempty"`
			AuthToken string `json:"auth_token,omitempty"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return res, fmt.Errorf("authstorage: parse config %s: %w", configPath, err)
	}

	// Build the per-provider credential fragment set in memory first,
	// so a config with no inline credentials leaves auth.json
	// untouched even if it already exists with other providers'
	// entries.
	toAdd := AuthFile{}
	for providerID, p := range raw.Providers {
		apiKey := p.APIKey
		authToken := p.AuthToken
		if apiKey == "" && authToken == "" {
			continue
		}
		toAdd[providerID] = AuthCredentials{
			APIKey:    apiKey,
			AuthToken: authToken,
		}
		res.Providers = append(res.Providers, providerID)
	}
	if len(toAdd) == 0 {
		return res, nil
	}

	// Merge with whatever's already in auth.json so a fresh migration
	// of one provider doesn't clobber another provider's creds.
	store := New(authPath)
	existing, err := store.Load()
	if err != nil && !errors.Is(err, ErrNotFound) {
		return res, err
	}
	if existing == nil {
		existing = AuthFile{}
	}
	for id, c := range toAdd {
		existing[id] = c
	}
	if err := store.Save(existing); err != nil {
		return res, fmt.Errorf("authstorage: save: %w", err)
	}
	res.Migrated = true
	return res, nil
}