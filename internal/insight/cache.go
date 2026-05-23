package insight

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/blueberrycongee/wuu/internal/statepath"
)

const usageDataSubDir = "usage-data"
const cacheSubDir = "cache"

// UsageDataDir returns the user-level directory for usage reports and caches.
func UsageDataDir(homeDir string) string {
	home, err := statepath.Home(homeDir)
	if err != nil {
		return ""
	}
	return statepath.UsageDataDir(home)
}

// CacheDir returns the cache directory under usage-data.
func CacheDir(usageDataDir string) string {
	return filepath.Join(usageDataDir, cacheSubDir)
}

// LoadCachedMeta loads cached session metadata, returns nil if missing.
func LoadCachedMeta(cacheDir, sessionID string) *SessionMeta {
	path := filepath.Join(cacheDir, sessionID+".meta.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var meta SessionMeta
	if json.Unmarshal(data, &meta) != nil {
		return nil
	}
	return &meta
}

// SaveCachedMeta persists session metadata to the cache.
func SaveCachedMeta(cacheDir string, meta SessionMeta) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cacheDir, meta.ID+".meta.json"), data, 0o600)
}

// LoadCachedFacet loads a cached facet, returns nil if missing.
func LoadCachedFacet(cacheDir, sessionID string) *Facet {
	path := filepath.Join(cacheDir, sessionID+".facet.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var facet Facet
	if json.Unmarshal(data, &facet) != nil {
		return nil
	}
	return &facet
}

// SaveCachedFacet persists a facet to the cache.
func SaveCachedFacet(cacheDir string, facet Facet) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(facet)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cacheDir, facet.SessionID+".facet.json"), data, 0o600)
}
