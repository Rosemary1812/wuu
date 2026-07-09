package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverProjectOverridesUserAndResolvesAssetDirs(t *testing.T) {
	root := t.TempDir()
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	writePlugin(t, filepath.Join(wuuHome, "plugins", "compose"), `{
  "id": "compose",
  "description": "user compose",
  "skills": ["skills"]
}`)
	writePlugin(t, filepath.Join(root, ".wuu", "plugins", "compose"), `{
  "id": "compose",
  "description": "project compose",
  "skills": ["skills", "../escape", "/tmp/absolute"]
}`)

	plugins := Discover(root, wuuHome)
	if len(plugins) != 1 {
		t.Fatalf("plugins = %+v", plugins)
	}
	got := plugins[0]
	if got.ID != "compose" || got.Source != "project" || got.Description != "project compose" {
		t.Fatalf("project plugin should override user plugin: %+v", got)
	}
	if got.SourceLabel() != "plugin:compose" {
		t.Fatalf("SourceLabel = %q", got.SourceLabel())
	}
	skillDirs := got.SkillDirs()
	if len(skillDirs) != 1 || skillDirs[0] != filepath.Join(got.Root, "skills") {
		t.Fatalf("SkillDirs = %+v", skillDirs)
	}
}

func TestDiscoverUsesDefaultAssetDirsWhenManifestOmitsThem(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, ".wuu", "plugins", "default-assets")
	writePlugin(t, pluginDir, `{"id":"default-assets"}`)

	plugins := Discover(root, "")
	if len(plugins) != 1 {
		t.Fatalf("plugins = %+v", plugins)
	}
	if len(plugins[0].SkillDirs()) != 1 {
		t.Fatalf("default skill dirs not discovered: skills=%+v", plugins[0].SkillDirs())
	}
}

func TestLoadManifestRequiresID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ManifestFilename)
	if err := os.WriteFile(path, []byte(`{"description":"missing id"}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if _, err := LoadManifest(path, "project"); err == nil {
		t.Fatal("expected missing id error")
	}
}

func writePlugin(t *testing.T, dir, manifest string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFilename), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}
