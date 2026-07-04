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

func TestConfigAndAuthPathsLiveInUnifiedHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUU_HOME", "")
	t.Setenv("HOME", home)

	cfgPath, err := ConfigPath(home)
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if want := filepath.Join(home, ".wuu", "config.json"); cfgPath != want {
		t.Fatalf("ConfigPath() = %q, want %q", cfgPath, want)
	}
	authPath, err := AuthPath(home)
	if err != nil {
		t.Fatalf("AuthPath: %v", err)
	}
	if want := filepath.Join(home, ".wuu", "auth.json"); authPath != want {
		t.Fatalf("AuthPath() = %q, want %q", authPath, want)
	}
	instructions, err := UserInstructionsDir(home)
	if err != nil {
		t.Fatalf("UserInstructionsDir: %v", err)
	}
	if want := filepath.Join(home, ".wuu"); instructions != want {
		t.Fatalf("UserInstructionsDir() = %q, want %q", instructions, want)
	}
}

func TestConfigAndAuthPathsFollowWuuHomeOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "state")
	t.Setenv("WUU_HOME", override)

	cfgPath, err := ConfigPath(t.TempDir())
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if want := filepath.Join(override, "config.json"); cfgPath != want {
		t.Fatalf("ConfigPath() = %q, want %q", cfgPath, want)
	}
	authPath, err := AuthPath(t.TempDir())
	if err != nil {
		t.Fatalf("AuthPath: %v", err)
	}
	if want := filepath.Join(override, "auth.json"); authPath != want {
		t.Fatalf("AuthPath() = %q, want %q", authPath, want)
	}
}

func TestLegacyPathsIgnoreWuuHomeOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUU_HOME", filepath.Join(t.TempDir(), "state"))

	if got, want := LegacyConfigPath(home), filepath.Join(home, ".config", "wuu", "config.json"); got != want {
		t.Fatalf("LegacyConfigPath() = %q, want %q", got, want)
	}
	if got, want := LegacyAuthPath(home), filepath.Join(home, ".config", "wuu", "auth.json"); got != want {
		t.Fatalf("LegacyAuthPath() = %q, want %q", got, want)
	}
	if got, want := LegacyGlobalDir(home), filepath.Join(home, ".config", "wuu"); got != want {
		t.Fatalf("LegacyGlobalDir() = %q, want %q", got, want)
	}
	if LegacyConfigPath("") != "" || LegacyAuthPath("") != "" || LegacyGlobalDir("") != "" {
		t.Fatal("legacy paths must be empty when home is empty")
	}
}

func TestUserInstructionDirsOrderCanonicalThenLegacy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUU_HOME", "")
	t.Setenv("HOME", home)

	dirs := UserInstructionDirs(home)
	want := []string{filepath.Join(home, ".wuu"), filepath.Join(home, ".config", "wuu")}
	if len(dirs) != len(want) || dirs[0] != want[0] || dirs[1] != want[1] {
		t.Fatalf("UserInstructionDirs() = %v, want %v", dirs, want)
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

func TestWorkspaceDirByIDIsStableAndPathIndependent(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".wuu")
	id := "proj 1234abcd"

	got, err := WorkspaceDirByID(home, id)
	if err != nil {
		t.Fatalf("WorkspaceDirByID: %v", err)
	}
	if !strings.HasPrefix(got, filepath.Join(home, "workspaces")+string(filepath.Separator)) {
		t.Fatalf("WorkspaceDirByID() = %q, want under %q", got, filepath.Join(home, "workspaces"))
	}
	if strings.Contains(got, "proj 1234abcd") {
		t.Fatalf("WorkspaceDirByID() should sanitize spaces, got %q", got)
	}

	gotAgain, err := WorkspaceDirByID(home, id)
	if err != nil {
		t.Fatalf("WorkspaceDirByID second call: %v", err)
	}
	if gotAgain != got {
		t.Fatalf("WorkspaceDirByID not stable: %q then %q", got, gotAgain)
	}

	// The whole point: the same id maps to the same dir regardless of any
	// filesystem path, and differs from the path-keyed WorkspaceDir.
	pathKeyed, err := WorkspaceDir(home, filepath.Join(t.TempDir(), "proj"))
	if err != nil {
		t.Fatalf("WorkspaceDir: %v", err)
	}
	if got == pathKeyed {
		t.Fatalf("WorkspaceDirByID collided with path-keyed WorkspaceDir: %q", got)
	}

	if _, err := WorkspaceDirByID(home, "  "); err == nil {
		t.Fatalf("WorkspaceDirByID should reject a blank id")
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

func TestAgentHomeDirLivesUnderWuuHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".wuu")
	got := AgentHomeDir(home, "prt-andy")
	want := filepath.Join(home, "agents", "prt-andy", "home")
	if got != want {
		t.Fatalf("AgentHomeDir = %q, want %q", got, want)
	}
}

func TestAgentHomeDirStableAcrossCalls(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".wuu")
	first := AgentHomeDir(home, "prt-bea")
	second := AgentHomeDir(home, "prt-bea")
	if first != second {
		t.Fatalf("AgentHomeDir not stable: %q then %q", first, second)
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

func TestWorkspaceMemoryDirDoesNotCollideWithDreamMemory(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "workspaces", "proj-abc123")

	got := WorkspaceMemoryDir(stateDir)
	want := filepath.Join(stateDir, "memory-store")
	if got != want {
		t.Fatalf("WorkspaceMemoryDir(%q) = %q, want %q", stateDir, got, want)
	}
	if got != WorkspaceMemoryDir(stateDir) {
		t.Fatal("WorkspaceMemoryDir should be stable across calls")
	}

	// The per-workspace dream memory (internal/sessionmemory) owns the
	// "memory" subdirectory and its files: memory/MEMORY.md,
	// memory/dream_state.json, memory/dream.lock. The workspace memory store
	// (a FileProvider writing entries.jsonl + MEMORY.md) must not share that
	// namespace, or its MEMORY.md would clobber the dream project memory.
	dreamMemoryDir := filepath.Join(stateDir, "memory")
	if got == dreamMemoryDir {
		t.Fatalf("workspace memory store dir %q collides with dream memory dir %q", got, dreamMemoryDir)
	}
	sep := string(filepath.Separator)
	if strings.HasPrefix(got+sep, dreamMemoryDir+sep) {
		t.Fatalf("workspace memory store dir %q is nested inside dream memory dir %q", got, dreamMemoryDir)
	}
	for _, dreamFile := range []string{"MEMORY.md", "dream_state.json", "dream.lock"} {
		dreamPath := filepath.Join(dreamMemoryDir, dreamFile)
		for _, storeFile := range []string{"MEMORY.md", "entries.jsonl"} {
			if filepath.Join(got, storeFile) == dreamPath {
				t.Fatalf("workspace store file %q collides with dream file %q", filepath.Join(got, storeFile), dreamPath)
			}
		}
	}
}
