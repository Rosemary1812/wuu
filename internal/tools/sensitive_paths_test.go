package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/statepath"
)

// runtimeHome returns the canonical agent home directory for tests, or skips
// the test if the host has no resolvable home (e.g. some CI environments).
func runtimeHome(t *testing.T) string {
	t.Helper()
	home, err := statepath.Home("")
	if err != nil {
		t.Skipf("statepath.Home unavailable: %v", err)
	}
	return filepath.ToSlash(filepath.Clean(home))
}

func TestIsAgentRuntimeMetadataPath(t *testing.T) {
	runtimeDir := runtimeHome(t)

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"exact home", runtimeDir, true},
		{"memory subdir", runtimeDir + "/memory/test.md", true},
		{"auth file", runtimeDir + "/auth.json", true},
		{"nested session artifact", runtimeDir + "/sessions/abc/msg.jsonl", true},
		{"workspace root", "/Users/somebody/work/foo", false},
		{"unrelated dot wuu sibling", "/Users/somebody/.wuuish/foo", false},
		{"partial suffix only", runtimeDir + "ish/foo", false},
		{"empty path", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAgentRuntimeMetadataPath(tc.path); got != tc.want {
				t.Fatalf("isAgentRuntimeMetadataPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestRejectSensitiveToolPath_AllowsAgentRuntimeWhenMutationsAllowed(t *testing.T) {
	target := runtimeHome(t) + "/memory/test.md"

	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.env.AllowMutations = true

	if err := rejectSensitiveToolPath(kit.env, "write_file", "write", target); err != nil {
		t.Fatalf("agent runtime metadata should be allowed when AllowMutations=true: %v", err)
	}
}

func TestRejectSensitiveToolPath_BlocksAgentRuntimeInReadOnly(t *testing.T) {
	target := runtimeHome(t) + "/memory/test.md"

	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.env.AllowMutations = false

	err = rejectSensitiveToolPath(kit.env, "write_file", "write", target)
	if err == nil {
		t.Fatalf("agent runtime metadata should still be blocked when AllowMutations=false")
	}
	if !strings.Contains(err.Error(), "sensitive path") {
		t.Fatalf("expected sensitive-path error, got: %v", err)
	}
}

func TestRejectSensitiveToolPath_NonAgentSensitivePathStillBlocked(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.env.AllowMutations = true

	// A filename containing both "private" and "key" hits the credential
	// gate. The agent-metadata exemption must not let unrelated sensitive
	// paths through just because AllowMutations is on.
	target := filepath.Join(t.TempDir(), "private_key.pem")
	err = rejectSensitiveToolPath(kit.env, "write_file", "write", target)
	if err == nil {
		t.Fatalf("non-agent-runtime sensitive path should remain blocked even with AllowMutations=true")
	}
}

func TestSetBoundaryPropagatesAllowMutations(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	kit.SetBoundary(StandardBoundary())
	if !kit.env.AllowMutations {
		t.Fatalf("StandardBoundary should propagate AllowMutations=true; env=%+v", kit.env)
	}

	kit.SetBoundary(ReadOnlyBoundary())
	if kit.env.AllowMutations {
		t.Fatalf("ReadOnlyBoundary should propagate AllowMutations=false; env=%+v", kit.env)
	}

	kit.SetBoundary(UnconfinedBoundary())
	if !kit.env.AllowMutations {
		t.Fatalf("UnconfinedBoundary should propagate AllowMutations=true; env=%+v", kit.env)
	}
}

func TestResolvePath_AllowsAgentRuntimeInStandardMode(t *testing.T) {
	target := runtimeHome(t) + "/memory/test.md"

	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetBoundary(StandardBoundary())

	resolved, err := kit.env.ResolvePath(target)
	if err != nil {
		t.Fatalf("StandardBoundary should allow agent runtime metadata: %v", err)
	}
	if resolved != target {
		t.Fatalf("resolved = %q, want %q", resolved, target)
	}
}

func TestResolvePath_BlocksAgentRuntimeInReadOnly(t *testing.T) {
	target := runtimeHome(t) + "/memory/test.md"

	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetBoundary(ReadOnlyBoundary())

	if _, err := kit.env.ResolvePath(target); err == nil {
		t.Fatalf("ReadOnlyBoundary should still block agent runtime metadata path resolution")
	}
}