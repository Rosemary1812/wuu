package plugin

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blueberrycongee/wuu/internal/extensions"
	"github.com/blueberrycongee/wuu/internal/pluginhost"
)

func TestPackageContractTracksEntryContentButNotEnvironmentValues(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "bin", "plugin")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("version-one"), 0o755); err != nil {
		t.Fatal(err)
	}
	item := Plugin{
		Manifest: Manifest{
			ID: "runtime-kit",
			Runtime: &RuntimeSpec{
				Protocol: pluginhost.ProtocolName,
				Command:  entry,
				Env:      map[string]string{"PLUGIN_TOKEN": "secret-one"},
			},
			RuntimePath: "bin/plugin",
		},
		Source: "project",
		Root:   root,
	}
	first, err := item.PackageContract()
	if err != nil {
		t.Fatal(err)
	}
	if first.SubjectID != "plugin:project:runtime-kit" || !reflect.DeepEqual(first.Permissions, []string{extensions.PermProcessSpawn}) {
		t.Fatalf("package contract = %+v", first)
	}

	item.Runtime.Env["PLUGIN_TOKEN"] = "secret-two"
	secretChanged, err := item.PackageContract()
	if err != nil {
		t.Fatal(err)
	}
	if secretChanged.Fingerprint != first.Fingerprint {
		t.Fatal("secret value unexpectedly changed package fingerprint")
	}

	if err := os.WriteFile(entry, []byte("version-two"), 0o755); err != nil {
		t.Fatal(err)
	}
	entryChanged, err := item.PackageContract()
	if err != nil {
		t.Fatal(err)
	}
	if entryChanged.Fingerprint == first.Fingerprint {
		t.Fatal("runtime entry content did not change package fingerprint")
	}
}

func TestPackageContractTracksSkillTreeContent(t *testing.T) {
	root := t.TempDir()
	skillPath := filepath.Join(root, "skills", "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("review version one"), 0o644); err != nil {
		t.Fatal(err)
	}
	item := Plugin{
		Manifest: Manifest{ID: "skill-kit", Skills: []string{"skills"}},
		Source:   "project",
		Root:     root,
	}
	first, err := item.PackageContract()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("review version two"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := item.PackageContract()
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("skill content did not change package fingerprint")
	}
}
