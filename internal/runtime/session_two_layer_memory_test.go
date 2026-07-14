package runtime

// These tests cover the file-directory memory replacement: legacy memstore
// directories stay unused, the disable switch controls prompt injection, and
// startup migration preserves old on-disk memory.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/memdir"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

func twoLayerTestConfig() config.Config {
	return config.Config{
		DefaultProvider: "test",
		Providers: map[string]config.ProviderConfig{
			"test": {
				Type:      "openai-compatible",
				BaseURL:   "https://example.test/v1",
				APIKeyEnv: "TEST_WUU_KEY",
				Model:     "gpt-test",
			},
		},
	}
}

func TestNewSessionDoesNotCreateLegacyWorkspaceMemoryStore(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	wuuHome := filepath.Join(home, "state")
	t.Setenv("WUU_HOME", wuuHome)
	t.Setenv("TEST_WUU_KEY", "abc")

	_, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config:     twoLayerTestConfig(),
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	workspaceStateDir, err := statepath.WorkspaceDir(wuuHome, root)
	if err != nil {
		t.Fatalf("WorkspaceDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspaceStateDir, "memory-store")); !os.IsNotExist(err) {
		t.Fatalf("<ws-state>/memory-store must no longer be created (stat err = %v)", err)
	}
}

func TestNewSessionMemoryDisableDisablesMemdir(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	wuuHome := filepath.Join(home, "state")
	t.Setenv("WUU_HOME", wuuHome)
	t.Setenv("TEST_WUU_KEY", "abc")

	cfg := twoLayerTestConfig()
	cfg.Memory = config.MemoryConfig{Disable: true}

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config:     cfg,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if rt.MemdirEnabled {
		t.Fatal("Memory.Disable must disable the memdir injection")
	}
	if strings.Contains(rt.BaseSystemPrompt, "# Memory directory") {
		t.Fatalf("disabled memory must not inject the memdir section:\n%s", rt.BaseSystemPrompt)
	}
	if _, err := os.Stat(memdir.UserMemdir(wuuHome)); !os.IsNotExist(err) {
		t.Fatalf("disabled memory must not create the user notebook (stat err = %v)", err)
	}
}

func TestNewSessionMigratesLegacyMemoryOnStartup(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	wuuHome := filepath.Join(home, "state")
	t.Setenv("WUU_HOME", wuuHome)
	t.Setenv("TEST_WUU_KEY", "abc")

	userNotebook := memdir.UserMemdir(wuuHome)
	if err := os.MkdirAll(userNotebook, 0o755); err != nil {
		t.Fatalf("mkdir notebook: %v", err)
	}
	entry := `{"id":"e1","content":"User prefers concise Chinese replies","tags":["target:user"],"source":"user","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(userNotebook, "entries.jsonl"), []byte(entry), 0o644); err != nil {
		t.Fatalf("write legacy log: %v", err)
	}

	rt, err := NewSession(Options{
		RootDir:    root,
		HomeDir:    home,
		ConfigPath: filepath.Join(root, ".wuu.json"),
		Config:     twoLayerTestConfig(),
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	// The migrated index line must reach the very first post-upgrade prompt.
	if !strings.Contains(rt.BaseSystemPrompt, "User prefers concise Chinese replies") {
		t.Fatalf("migrated index line missing from prompt:\n%s", rt.BaseSystemPrompt)
	}
	if _, err := os.Stat(filepath.Join(userNotebook, "entries.jsonl.migrated")); err != nil {
		t.Fatalf("legacy log not retired: %v", err)
	}
	if _, err := os.Stat(filepath.Join(userNotebook, ".memdir-migrated")); err != nil {
		t.Fatalf("migration marker missing: %v", err)
	}
}

func TestNewSessionBaseSystemPromptMemdirByteIdentical(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	wuuHome := filepath.Join(home, "state")
	t.Setenv("WUU_HOME", wuuHome)
	t.Setenv("TEST_WUU_KEY", "abc")

	userNotebook := memdir.UserMemdir(wuuHome)
	if err := os.MkdirAll(userNotebook, 0o755); err != nil {
		t.Fatalf("mkdir notebook: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userNotebook, "MEMORY.md"), []byte("- [Global fact](g.md) — global fact\n"), 0o644); err != nil {
		t.Fatalf("seed notebook: %v", err)
	}

	build := func() string {
		rt, err := NewSession(Options{
			RootDir:    root,
			HomeDir:    home,
			ConfigPath: filepath.Join(root, ".wuu.json"),
			Config:     twoLayerTestConfig(),
		})
		if err != nil {
			t.Fatalf("NewSession: %v", err)
		}
		return rt.BaseSystemPrompt
	}

	first := build()
	second := build()
	if first != second {
		t.Fatalf("memdir BaseSystemPrompt not byte-identical across rebuilds:\n#1:\n%s\n#2:\n%s", first, second)
	}
	if !strings.Contains(first, "- [Global fact](g.md) — global fact") {
		t.Fatalf("notebook index line missing from prompt:\n%s", first)
	}
}
