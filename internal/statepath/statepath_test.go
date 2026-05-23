package statepath

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeDefaultsToUserState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUU_HOME", "")
	t.Setenv("HOME", home)

	got, err := Home("")
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	want := filepath.Join(home, ".wuu")
	if got != want {
		t.Fatalf("Home() = %q, want %q", got, want)
	}
}

func TestHomeUsesOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "state")
	t.Setenv("WUU_HOME", override)

	got, err := Home(t.TempDir())
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if got != override {
		t.Fatalf("Home() = %q, want %q", got, override)
	}
}

func TestWorkspaceDirIsStableAndOutsideWorkspace(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".wuu")
	root := filepath.Join(t.TempDir(), "my workspace")

	got, err := WorkspaceDir(home, root)
	if err != nil {
		t.Fatalf("WorkspaceDir: %v", err)
	}
	if !strings.HasPrefix(got, filepath.Join(home, "workspaces")+string(filepath.Separator)) {
		t.Fatalf("WorkspaceDir() = %q, want under %q", got, filepath.Join(home, "workspaces"))
	}
	if strings.Contains(got, "my workspace") {
		t.Fatalf("WorkspaceDir() should sanitize spaces, got %q", got)
	}

	gotAgain, err := WorkspaceDir(home, root)
	if err != nil {
		t.Fatalf("WorkspaceDir second call: %v", err)
	}
	if gotAgain != got {
		t.Fatalf("WorkspaceDir not stable: %q then %q", got, gotAgain)
	}
}
