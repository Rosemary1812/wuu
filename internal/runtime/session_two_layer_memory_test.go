package runtime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/config"
	memstore "github.com/blueberrycongee/wuu/internal/memory/store"
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

func TestRecallLayeredProfileMemoryGlobalFirstDeterministic(t *testing.T) {
	global, err := memstore.NewFileProvider(filepath.Join(t.TempDir(), "global"))
	if err != nil {
		t.Fatalf("global provider: %v", err)
	}
	workspace, err := memstore.NewFileProvider(filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatalf("workspace provider: %v", err)
	}
	ctx := context.Background()
	for _, c := range []string{"G1", "G2"} {
		if _, err := global.Store(ctx, memstore.Entry{Content: c, Tags: []string{"target:memory"}}); err != nil {
			t.Fatalf("seed global %q: %v", c, err)
		}
	}
	for _, c := range []string{"W1", "W2"} {
		if _, err := workspace.Store(ctx, memstore.Entry{Content: c, Tags: []string{"target:memory"}}); err != nil {
			t.Fatalf("seed workspace %q: %v", c, err)
		}
	}

	merged := recallLayeredProfileMemory(ctx, global, workspace)
	if len(merged) != 4 {
		t.Fatalf("merged len = %d, want 4", len(merged))
	}
	// Global entries must precede workspace entries; within a layer, order is
	// the provider's stable Recall order (UpdatedAt desc), so newest first.
	gotOrder := make([]string, 0, len(merged))
	for _, e := range merged {
		gotOrder = append(gotOrder, e.Content)
	}
	want := []string{"G2", "G1", "W2", "W1"}
	for i := range want {
		if gotOrder[i] != want[i] {
			t.Fatalf("merged order = %v, want %v", gotOrder, want)
		}
	}

	// Re-reading the unchanged stores must be byte-for-byte identical, which is
	// the cache-safety invariant for prompt-prefix stability.
	again := recallLayeredProfileMemory(ctx, global, workspace)
	if len(again) != len(merged) {
		t.Fatalf("second recall len = %d, want %d", len(again), len(merged))
	}
	for i := range merged {
		if again[i].ID != merged[i].ID || again[i].Content != merged[i].Content {
			t.Fatalf("recall not deterministic at %d: %+v vs %+v", i, again[i], merged[i])
		}
	}
}

func TestRecallLayeredProfileMemoryNilLayers(t *testing.T) {
	global, err := memstore.NewFileProvider(filepath.Join(t.TempDir(), "global"))
	if err != nil {
		t.Fatalf("global provider: %v", err)
	}
	if _, err := global.Store(context.Background(), memstore.Entry{Content: "only-global", Tags: []string{"target:memory"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if got := recallLayeredProfileMemory(context.Background(), global, nil); len(got) != 1 || got[0].Content != "only-global" {
		t.Fatalf("global-only merge = %+v", got)
	}
	if got := recallLayeredProfileMemory(context.Background(), nil, nil); got != nil {
		t.Fatalf("both-nil merge = %+v, want nil", got)
	}
}

func TestNewSessionInjectsWorkspaceMemoryLayer(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	wuuHome := filepath.Join(home, "state")
	t.Setenv("WUU_HOME", wuuHome)
	t.Setenv("TEST_WUU_KEY", "abc")

	workspaceStateDir, err := statepath.WorkspaceDir(wuuHome, root)
	if err != nil {
		t.Fatalf("WorkspaceDir: %v", err)
	}
	wsDir := statepath.WorkspaceMemoryDir(workspaceStateDir)
	// The workspace store must not share the dream memory directory.
	if wsDir == filepath.Join(workspaceStateDir, "memory") {
		t.Fatalf("workspace memory dir %q collides with dream memory dir", wsDir)
	}
	wsProvider, err := memstore.NewFileProvider(wsDir)
	if err != nil {
		t.Fatalf("workspace provider: %v", err)
	}
	if _, err := wsProvider.Store(context.Background(), memstore.Entry{
		Content: "Workspace builds with make wuu",
		Tags:    []string{"target:memory", "build"},
	}); err != nil {
		t.Fatalf("seed workspace memory: %v", err)
	}

	// Seed a global fact too so we can assert global-first ordering in the prompt.
	globalProvider, err := memstore.NewFileProvider(statepath.GlobalMemoryDir(wuuHome))
	if err != nil {
		t.Fatalf("global provider: %v", err)
	}
	if _, err := globalProvider.Store(context.Background(), memstore.Entry{
		Content: "User prefers concise Chinese replies",
		Tags:    []string{"target:user"},
	}); err != nil {
		t.Fatalf("seed global memory: %v", err)
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

	if rt.Toolkit.WorkspaceMemory() == nil {
		t.Fatal("expected the workspace memory layer to be attached")
	}
	wsAttached, ok := rt.Toolkit.WorkspaceMemory().(*memstore.FileProvider)
	if !ok {
		t.Fatalf("workspace memory provider = %T, want *FileProvider", rt.Toolkit.WorkspaceMemory())
	}
	if wsAttached.Dir() != wsDir {
		t.Fatalf("workspace memory dir = %q, want %q", wsAttached.Dir(), wsDir)
	}

	// Prompt injection now comes from the user notebook only (memdir M1):
	// the global store's rendered MEMORY.md is picked up as the notebook
	// index, while workspace-store content no longer reaches the prompt
	// (memory-redesign D2: no project notebook in v1).
	for _, want := range []string{
		"# Memory directory",
		"User prefers concise Chinese replies",
	} {
		if !strings.Contains(rt.BaseSystemPrompt, want) {
			t.Fatalf("BaseSystemPrompt missing %q:\n%s", want, rt.BaseSystemPrompt)
		}
	}
	if strings.Contains(rt.BaseSystemPrompt, "Workspace builds with make wuu") {
		t.Fatalf("workspace-store content must not be injected into the prompt:\n%s", rt.BaseSystemPrompt)
	}
}

func TestNewSessionMemoryDisableRemovesBothLayers(t *testing.T) {
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
	if rt.Toolkit.Memory() != nil {
		t.Fatal("Memory.Disable should detach the global memory layer")
	}
	if rt.Toolkit.WorkspaceMemory() != nil {
		t.Fatal("Memory.Disable should detach the workspace memory layer")
	}
	for _, def := range rt.Toolkit.Definitions() {
		if def.Name == "read_memory" || def.Name == "write_memory" {
			t.Fatalf("memory tool %q should be hidden when memory is disabled", def.Name)
		}
	}
}

func TestNewSessionBaseSystemPromptTwoLayerByteIdentical(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	wuuHome := filepath.Join(home, "state")
	t.Setenv("WUU_HOME", wuuHome)
	t.Setenv("TEST_WUU_KEY", "abc")

	workspaceStateDir, err := statepath.WorkspaceDir(wuuHome, root)
	if err != nil {
		t.Fatalf("WorkspaceDir: %v", err)
	}
	wsProvider, err := memstore.NewFileProvider(statepath.WorkspaceMemoryDir(workspaceStateDir))
	if err != nil {
		t.Fatalf("workspace provider: %v", err)
	}
	if _, err := wsProvider.Store(context.Background(), memstore.Entry{Content: "ws fact", Tags: []string{"target:memory"}}); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	globalProvider, err := memstore.NewFileProvider(statepath.GlobalMemoryDir(wuuHome))
	if err != nil {
		t.Fatalf("global provider: %v", err)
	}
	if _, err := globalProvider.Store(context.Background(), memstore.Entry{Content: "global fact", Tags: []string{"target:user"}}); err != nil {
		t.Fatalf("seed global: %v", err)
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
		t.Fatalf("two-layer BaseSystemPrompt not byte-identical across rebuilds:\n#1:\n%s\n#2:\n%s", first, second)
	}
}
