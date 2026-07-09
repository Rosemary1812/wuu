package authstorage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func skipIfNotUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("mode bits not honored on windows")
	}
}

func TestStore_LoadNotFound(t *testing.T) {
	dir := t.TempDir()
	store := New(filepath.Join(dir, "auth.json"))
	creds, err := store.Load()
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load() err = %v, want ErrNotFound", err)
	}
	if credds, ok := any(creds).(AuthFile); ok && credds != nil {
		// We accept both nil and a freshly-allocated empty map —
		// either is fine semantically. Catch the wrong case here.
		if len(creds) != 0 {
			t.Fatalf("Load() creds = %+v, want empty/nil on ErrNotFound", credds)
		}
	}
}

func TestStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := New(filepath.Join(dir, "auth.json"))
	want := AuthFile{
		"openai": {
			APIKey:       "sk-test-1234",
			RefreshToken: "rt-5678",
			ExpiresAt:    time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
		},
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	openai, ok := got["openai"]
	if !ok {
		t.Fatalf("Load: openai missing; got keys = %v", keysOf(got))
	}
	if openai.APIKey != "sk-test-1234" {
		t.Errorf("openai.APIKey = %q, want sk-test-1234", openai.APIKey)
	}
	if openai.RefreshToken != "rt-5678" {
		t.Errorf("openai.RefreshToken = %q, want rt-5678", openai.RefreshToken)
	}
	if !openai.ExpiresAt.Equal(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("openai.ExpiresAt = %v, want 2030-01-02T03:04:05Z", openai.ExpiresAt)
	}
}

func TestStore_Save0o600(t *testing.T) {
	skipIfNotUnix(t)
	dir := t.TempDir()
	store := New(filepath.Join(dir, "auth.json"))
	if err := store.Save(AuthFile{"openai": {APIKey: "k"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 0o600", got)
	}
}

func TestStore_DeleteIdempotent(t *testing.T) {
	dir := t.TempDir()
	store := New(filepath.Join(dir, "auth.json"))
	// Delete on a missing file should not error.
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete on missing: %v", err)
	}
	if err := store.Save(AuthFile{"p": {APIKey: "k"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete on existing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("after Delete, stat err = %v, want ErrNotExist", err)
	}
}

func TestStore_DeleteProvider(t *testing.T) {
	dir := t.TempDir()
	store := New(filepath.Join(dir, "auth.json"))
	if err := store.Save(AuthFile{
		"openai":    {APIKey: "sk-openai"},
		"anthropic": {AuthToken: "tok-anthropic"},
	}); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	if err := store.DeleteProvider("openai"); err != nil {
		t.Fatalf("DeleteProvider openai: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load after DeleteProvider: %v", err)
	}
	if _, present := got["openai"]; present {
		t.Errorf("openai still present after DeleteProvider")
	}
	if _, present := got["anthropic"]; !present {
		t.Errorf("anthropic disappeared after DeleteProvider(openai)")
	}
}

func TestStore_DeleteProviderLastOneRemovesFile(t *testing.T) {
	dir := t.TempDir()
	store := New(filepath.Join(dir, "auth.json"))
	if err := store.Save(AuthFile{"only": {APIKey: "k"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.DeleteProvider("only"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file still exists after removing last provider: err=%v", err)
	}
}

func TestStore_SetProviderPreservesOthers(t *testing.T) {
	dir := t.TempDir()
	store := New(filepath.Join(dir, "auth.json"))
	if _, err := store.SetProvider("openai", AuthCredentials{APIKey: "k1"}); err != nil {
		t.Fatalf("SetProvider openai: %v", err)
	}
	if _, err := store.SetProvider("anthropic", AuthCredentials{AuthToken: "t1"}); err != nil {
		t.Fatalf("SetProvider anthropic: %v", err)
	}
	if _, err := store.SetProvider("openai", AuthCredentials{APIKey: "k2"}); err != nil {
		t.Fatalf("SetProvider openai overwrite: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got["openai"].APIKey != "k2" {
		t.Errorf("openai.APIKey = %q, want k2 (overwrite)", got["openai"].APIKey)
	}
	if got["anthropic"].AuthToken != "t1" {
		t.Errorf("anthropic.AuthToken = %q, want t1 (preserved)", got["anthropic"].AuthToken)
	}
}

func TestStore_ConcurrentSaves(t *testing.T) {
	// Hammer SetProvider from many goroutines. The mutex must
	// serialize writers; securefs.WriteFileAtomic must never produce a
	// half-written file. After the dust settles, every provider that
	// any writer attempted to set must be present, with coherent (non-
	// empty) credentials.
	dir := t.TempDir()
	store := New(filepath.Join(dir, "auth.json"))
	const N = 16
	providers := []string{"a", "b", "c", "d"}
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			id := providers[i%len(providers)]
			if _, err := store.SetProvider(id, AuthCredentials{
				APIKey: "k-" + id,
			}); err != nil {
				t.Errorf("SetProvider %s: %v", id, err)
			}
		}()
	}
	wg.Wait()
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load after concurrent: %v", err)
	}
	for _, id := range providers {
		c, ok := got[id]
		if !ok {
			t.Errorf("provider %s missing after concurrent SetProvider", id)
			continue
		}
		if c.APIKey != "k-"+id {
			t.Errorf("provider %s APIKey = %q, want k-%s", id, c.APIKey, id)
		}
	}
}

func TestStore_LoadMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte("not json {"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	store := New(path)
	creds, err := store.Load()
	if err == nil {
		t.Fatalf("Load on malformed = nil err, want error")
	}
	if creds != nil {
		t.Fatalf("Load on malformed = %+v, want nil creds", creds)
	}
}

func TestMigrate_NoCredentials(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	authPath := filepath.Join(dir, "auth.json")
	original := `{
  "default_provider": "openai",
  "providers": {
    "openai": {"model": "gpt-4o"},
    "anthropic": {"model": "claude-opus"}
  }
}`
	if err := os.WriteFile(cfgPath, []byte(original), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	res, err := MigrateFromConfig(cfgPath, authPath)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if res.Migrated {
		t.Fatalf("Migrated = true, want false on a clean config")
	}
	if _, err := os.Stat(authPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("auth.json exists after no-op migration: err=%v", err)
	}
}

func TestMigrate_ExtractsInlineCredentials(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	authPath := filepath.Join(dir, "auth.json")
	original := `{
  "default_provider": "openai",
  "providers": {
    "openai": {"model": "gpt-4o", "api_key": "sk-old-inline"},
    "anthropic": {"model": "claude-opus", "auth_token": "tok-old-inline"},
    "noop": {"model": "noop-1"}
  }
}`
	if err := os.WriteFile(cfgPath, []byte(original), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := MigrateFromConfig(cfgPath, authPath)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !res.Migrated {
		t.Fatalf("Migrated = false, want true")
	}
	// Both providers should be in the migrated list.
	if len(res.Providers) != 2 {
		t.Errorf("len(Providers) = %d, want 2 (openai + anthropic); got %v", len(res.Providers), res.Providers)
	}
	// Each migrated provider must end up in auth.json with the original
	// credential — neither one should overwrite the other.
	got, err := New(authPath).Load()
	if err != nil {
		t.Fatalf("Load auth: %v", err)
	}
	if got["openai"].APIKey != "sk-old-inline" {
		t.Errorf("openai.APIKey = %q, want sk-old-inline", got["openai"].APIKey)
	}
	if got["anthropic"].AuthToken != "tok-old-inline" {
		t.Errorf("anthropic.AuthToken = %q, want tok-old-inline", got["anthropic"].AuthToken)
	}
	if _, present := got["noop"]; present {
		t.Errorf("noop provider unexpectedly migrated (no creds in config)")
	}
}

func TestMigrate_PreservesExistingCredentials(t *testing.T) {
	// If auth.json already has credentials for one provider and the
	// config.json has inline creds for another, the migration must
	// not clobber the existing entry.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	authPath := filepath.Join(dir, "auth.json")
	// Pre-seed auth.json with a credentials for anthropic that
	// shouldn't be touched.
	pre := AuthFile{"anthropic": {AuthToken: "tok-pre-existing"}}
	if err := New(authPath).Save(pre); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	cfg := `{
  "providers": {
    "openai": {"model": "gpt-4o", "api_key": "sk-fresh-migration"}
  }
}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	res, err := MigrateFromConfig(cfgPath, authPath)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !res.Migrated {
		t.Fatalf("Migrated = false, want true")
	}
	got, err := New(authPath).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got["openai"].APIKey != "sk-fresh-migration" {
		t.Errorf("openai.APIKey = %q, want sk-fresh-migration", got["openai"].APIKey)
	}
	if got["anthropic"].AuthToken != "tok-pre-existing" {
		t.Errorf("anthropic.AuthToken = %q, want tok-pre-existing (preserved)", got["anthropic"].AuthToken)
	}
}

func TestMigrate_NoConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	authPath := filepath.Join(dir, "auth.json")
	res, err := MigrateFromConfig(cfgPath, authPath)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if res.Migrated {
		t.Fatalf("Migrated = true on missing config, want false")
	}
	if _, err := os.Stat(authPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("auth.json created on missing config: err=%v", err)
	}
}

func TestMigrate_Auth0o600(t *testing.T) {
	skipIfNotUnix(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	authPath := filepath.Join(dir, "auth.json")
	cfg := `{"providers": {"openai": {"api_key": "k"}}}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := MigrateFromConfig(cfgPath, authPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	info, err := os.Stat(authPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("auth.json mode = %o, want 0o600", got)
	}
}

// keysOf is a tiny helper for failure messages that need to print the
// provider IDs in a saved auth.json.
func keysOf(m AuthFile) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// jsonBytesAreValid makes a test failure message clearer when the
// file is expected to be parseable JSON.
func jsonBytesAreValid(data []byte) bool {
	var v any
	return json.Unmarshal(data, &v) == nil
}