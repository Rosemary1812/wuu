package participant

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.NormalizedPermissionTier() != PermissionTierWorkspace {
		t.Fatalf("default tier = %q, want workspace", m.NormalizedPermissionTier())
	}
	if err := SaveManifest(dir, Manifest{
		Skills:         []string{"commit", "review-pr"},
		PermissionTier: PermissionTierUnrestricted,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 2 || got.Skills[0] != "commit" {
		t.Fatalf("skills = %+v", got.Skills)
	}
	if got.NormalizedPermissionTier() != PermissionTierUnrestricted {
		t.Fatalf("tier = %q, want unrestricted", got.NormalizedPermissionTier())
	}
}

func TestManifestUnknownTierFallsBackToWorkspace(t *testing.T) {
	m := Manifest{PermissionTier: "everything"}
	if got := m.NormalizedPermissionTier(); got != PermissionTierWorkspace {
		t.Fatalf("unknown tier normalized to %q, want workspace", got)
	}
}

func TestPromptOverlayRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if overlay, err := LoadPromptOverlay(dir); err != nil || overlay != "" {
		t.Fatalf("missing overlay = %q, %v", overlay, err)
	}
	if err := SavePromptOverlay(filepath.Join(dir, "nested", "home"), "  You write terse, dry release notes.  "); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPromptOverlay(filepath.Join(dir, "nested", "home"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "You write terse, dry release notes." {
		t.Fatalf("overlay = %q", got)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "nested", "home", PromptOverlayName)); err != nil || len(raw) == 0 {
		t.Fatalf("overlay file = %v", err)
	}
}
