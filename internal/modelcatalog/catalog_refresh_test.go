package modelcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const testModelsDevCatalog = `{
  "fresh": {
    "id": "fresh",
    "name": "Fresh Provider",
    "api": "https://example.test/v1",
    "npm": "@ai-sdk/openai-compatible",
    "models": {
      "fresh-model": {"id": "fresh-model", "name": "Fresh Model", "tool_call": true}
    }
  }
}`

func TestRefreshWritesNormalizedCacheAndPublishesCatalog(t *testing.T) {
	t.Cleanup(func() { _ = UseEmbedded() })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testModelsDevCatalog))
	}))
	t.Cleanup(server.Close)

	cachePath := filepath.Join(t.TempDir(), "modelcatalog.json")
	counts, err := Refresh(context.Background(), RefreshOptions{
		URL:       server.URL,
		CachePath: cachePath,
		Client:    server.Client(),
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if counts.Providers != 1 || counts.Models != 1 {
		t.Fatalf("Refresh() counts = %+v, want 1 provider and 1 model", counts)
	}
	provider, ok := ProviderByID("fresh")
	if !ok || len(provider.Models) != 1 || provider.Models[0].ID != "fresh-model" {
		t.Fatalf("published provider = %+v, ok=%v", provider, ok)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("ReadFile(cache) error = %v", err)
	}
	parsed, err := decodeCatalog(data)
	if err != nil {
		t.Fatalf("cached catalog is invalid: %v", err)
	}
	if got := countCatalog(parsed); got != counts {
		t.Fatalf("cached counts = %+v, want %+v", got, counts)
	}
}

func TestRefreshFailureKeepsPreviousCacheAndPublishedCatalog(t *testing.T) {
	t.Cleanup(func() { _ = UseEmbedded() })

	cachePath := filepath.Join(t.TempDir(), "modelcatalog.json")
	validServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(testModelsDevCatalog))
	}))
	_, err := Refresh(context.Background(), RefreshOptions{
		URL:       validServer.URL,
		CachePath: cachePath,
		Client:    validServer.Client(),
	})
	validServer.Close()
	if err != nil {
		t.Fatalf("initial Refresh() error = %v", err)
	}
	before, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}

	invalidServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"fresh":{"id":"fresh","models":{}}}`))
	}))
	t.Cleanup(invalidServer.Close)
	_, err = Refresh(context.Background(), RefreshOptions{
		URL:       invalidServer.URL,
		CachePath: cachePath,
		Client:    invalidServer.Client(),
	})
	if err == nil {
		t.Fatal("Refresh() error = nil for an empty normalized catalog")
	}
	after, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed refresh changed the valid cache")
	}
	if _, ok := ProviderByID("fresh"); !ok {
		t.Fatal("failed refresh replaced the published catalog")
	}
}

func TestLoadCacheRejectsInvalidDataWithoutReplacingCatalog(t *testing.T) {
	t.Cleanup(func() { _ = UseEmbedded() })

	publishCatalog(catalogData{Providers: []Provider{{
		ID: "existing", Name: "Existing", Models: []Model{{ID: "existing-model"}},
	}}})
	path := filepath.Join(t.TempDir(), "modelcatalog.json")
	if err := os.WriteFile(path, []byte(`{"providers":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadCache(path); err == nil {
		t.Fatal("LoadCache() error = nil for empty cache")
	}
	if _, ok := ProviderByID("existing"); !ok {
		t.Fatal("invalid cache replaced the previously loaded catalog")
	}
}
