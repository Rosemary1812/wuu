package appserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/blueberrycongee/wuu/internal/extensions"
	pluginpkg "github.com/blueberrycongee/wuu/internal/plugin"
)

func TestExtensionPackageUpdateGrantsExactFingerprintAndDisablesImmediately(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	home := retryingTempDir(t)
	wuuHome := filepath.Join(home, ".wuu")
	t.Setenv("WUU_HOME", wuuHome)
	rt.HomeDir = home
	rt.WuuHome = wuuHome
	baseConfig := []byte(`{
  "default_provider": "fake-provider",
  "providers": {"fake-provider": {"type": "openai-compatible", "base_url": "https://example.test/v1", "model": "fake-model"}}
}
`)
	if err := os.MkdirAll(wuuHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wuuHome, "config.json"), baseConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rt.ConfigPath, baseConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	rt.Plugins = []pluginpkg.Plugin{{
		Manifest: pluginpkg.Manifest{ID: "prompt-kit", Commands: []pluginpkg.ResolvedCommand{{
			CommandSpec: pluginpkg.CommandSpec{ID: "draft", Title: "Draft", Kind: pluginpkg.CommandKindPromptTemplate, Prompt: "Draft ${input}"},
			PublicID:    "prompt-kit.draft",
		}}},
		Source: "project", SubjectID: "plugin:project:prompt-kit", Fingerprint: "sha256:prompt-kit",
		EffectivePermissions: []string{extensions.PermCommandsExecute},
	}}

	out := &lockedBuffer{}
	srv := New(rt, out)
	grant := `{"id":"grant","method":"extension/package/update","params":{"id":"plugin:project:prompt-kit","fingerprint":"sha256:prompt-kit","action":"grant"}}`
	if err := srv.handleLine(context.Background(), []byte(grant)); err != nil {
		t.Fatalf("grant package: %v", err)
	}
	messages := parseOutput(t, out.String())
	result := remarshal[ExtensionPackageUpdateResult](t, responseByID(t, messages, "grant")["result"])
	record := findExtensionRecord(t, result.ExtensionInventory, "plugin:project:prompt-kit")
	if record.ApprovalState != ExtensionApprovalGranted || record.RuntimeState != ExtensionRuntimeActive || record.Enabled == nil || !*record.Enabled {
		t.Fatalf("granted package record = %+v", record)
	}
	if len(record.Contributions.Commands) != 1 || record.Contributions.Commands[0].ID != "draft" || record.Contributions.Commands[0].Template != "Draft ${input}" {
		t.Fatalf("command contributions = %+v", record.Contributions)
	}
	data, err := os.ReadFile(filepath.Join(wuuHome, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted["extensions"] == nil {
		t.Fatalf("grant was not persisted in user-owned config: %s", data)
	}
	projectData, err := os.ReadFile(rt.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(projectData) != string(baseConfig) {
		t.Fatalf("user-owned grant mutated project config:\n%s", projectData)
	}

	disable := `{"id":"disable","method":"extension/package/update","params":{"id":"plugin:project:prompt-kit","fingerprint":"sha256:prompt-kit","action":"disable"}}`
	if err := srv.handleLine(context.Background(), []byte(disable)); err != nil {
		t.Fatalf("disable package: %v", err)
	}
	messages = parseOutput(t, out.String())
	result = remarshal[ExtensionPackageUpdateResult](t, responseByID(t, messages, "disable")["result"])
	record = findExtensionRecord(t, result.ExtensionInventory, "plugin:project:prompt-kit")
	if record.Enabled == nil || *record.Enabled || record.RuntimeState != ExtensionRuntimeInactive {
		t.Fatalf("disabled package record = %+v", record)
	}
}

func TestExtensionPackageUpdateRejectsStaleFingerprint(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.Plugins = []pluginpkg.Plugin{{
		Manifest: pluginpkg.Manifest{ID: "changed"}, Source: "project",
		SubjectID: "plugin:project:changed", Fingerprint: "sha256:new",
	}}
	out := &lockedBuffer{}
	srv := New(rt, out)
	req := `{"id":"stale","method":"extension/package/update","params":{"id":"plugin:project:changed","fingerprint":"sha256:old","action":"grant"}}`
	if err := srv.handleLine(context.Background(), []byte(req)); err != nil {
		t.Fatalf("stale update dispatch: %v", err)
	}
	response := responseByID(t, parseOutput(t, out.String()), "stale")
	if response["error"] == nil {
		t.Fatalf("stale fingerprint unexpectedly succeeded: %+v", response)
	}
}

func TestExtensionCatalogRefreshRediscoversPackages(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	root := t.TempDir()
	rt.RootDir = root
	rt.WuuHome = filepath.Join(root, "wuu-home")
	manifestPath := filepath.Join(root, ".wuu", "plugins", "fresh", pluginpkg.ManifestFilename)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"id":"fresh","name":"Fresh plugin"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"refresh","method":"extension/catalog/refresh"}`)); err != nil {
		t.Fatalf("refresh extension catalog: %v", err)
	}
	result := remarshal[ExtensionCatalogRefreshResult](t, responseByID(t, parseOutput(t, out.String()), "refresh")["result"])
	record := findExtensionRecord(t, result.ExtensionInventory, "plugin:project:fresh")
	if record.Name != "fresh" || record.ApprovalState != ExtensionApprovalPending {
		t.Fatalf("refreshed package record = %+v", record)
	}
}

func findExtensionRecord(t *testing.T, records []ExtensionInventoryRecord, id string) ExtensionInventoryRecord {
	t.Helper()
	for _, record := range records {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("extension record %q not found in %+v", id, records)
	return ExtensionInventoryRecord{}
}
