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

func TestProfileDirIsStableAndUnderProfiles(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".wuu")
	name := "Mia Agent"

	got, err := ProfileDir(home, name)
	if err != nil {
		t.Fatalf("ProfileDir: %v", err)
	}
	if !strings.HasPrefix(got, filepath.Join(home, "profiles")+string(filepath.Separator)) {
		t.Fatalf("ProfileDir() = %q, want under %q", got, filepath.Join(home, "profiles"))
	}
	if strings.Contains(got, "Mia Agent") {
		t.Fatalf("ProfileDir() should sanitize spaces, got %q", got)
	}

	gotAgain, err := ProfileDir(home, name)
	if err != nil {
		t.Fatalf("ProfileDir second call: %v", err)
	}
	if gotAgain != got {
		t.Fatalf("ProfileDir not stable: %q then %q", got, gotAgain)
	}
}

func TestProfileDirDefaultsEmptyName(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".wuu")

	got, err := ProfileDir(home, "")
	if err != nil {
		t.Fatalf("ProfileDir empty: %v", err)
	}
	want, err := ProfileDir(home, "default")
	if err != nil {
		t.Fatalf("ProfileDir default: %v", err)
	}
	if got != want {
		t.Fatalf("empty profile dir = %q, want default profile dir %q", got, want)
	}
	if ProfileMemoryDir(got) != filepath.Join(got, "memory") {
		t.Fatalf("ProfileMemoryDir(%q) = %q", got, ProfileMemoryDir(got))
	}
}
