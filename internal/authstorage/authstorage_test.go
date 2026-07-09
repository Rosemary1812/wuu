package authstorage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestStoreRoundTripAllCredentialKinds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	store := New(path)
	if err := store.Set("api", Credentials{Type: "api_key", APIKey: "sk-test"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("bearer", Credentials{Type: "auth_token", AuthToken: "token"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("openai-codex", Credentials{Type: "oauth", AccessToken: "access", RefreshToken: "refresh", AccountID: "account"}); err != nil {
		t.Fatal(err)
	}
	file, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if file.Version != CurrentVersion || file.Providers["api"].APIKey != "sk-test" || file.Providers["bearer"].AuthToken != "token" || file.Providers["openai-codex"].RefreshToken != "refresh" {
		t.Fatalf("unexpected file: %+v", file)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", info, err)
	}
}

func TestStoreInstancesSerializeUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('a' + i))
			if err := New(path).Set(id, Credentials{Type: "api_key", APIKey: id}); err != nil {
				t.Errorf("set: %v", err)
			}
		}(i)
	}
	wg.Wait()
	file, err := New(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Providers) != 20 {
		t.Fatalf("providers = %d", len(file.Providers))
	}
}

func TestStoreRejectsUnversionedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"provider":{"api_key":"x"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path).Load(); err == nil {
		t.Fatal("expected unsupported version error")
	}
}
