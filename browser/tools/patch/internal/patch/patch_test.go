package patch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDiffOutputDetectsRenameAndDeleteSignatures(t *testing.T) {
	renameDiff := `diff --git a/chrome/old.cc b/chrome/new.cc
similarity index 100%
rename from chrome/old.cc
rename to chrome/new.cc
`
	deleteDiff := `diff --git a/chrome/dead.cc b/chrome/dead.cc
deleted file mode 100644
index 123..000 100644
--- a/chrome/dead.cc
+++ /dev/null
@@ -1 +0,0 @@
-gone
`
	renameSet, err := ParseDiffOutput(renameDiff)
	if err != nil {
		t.Fatalf("ParseDiffOutput rename: %v", err)
	}
	deleteSet, err := ParseDiffOutput(deleteDiff)
	if err != nil {
		t.Fatalf("ParseDiffOutput delete: %v", err)
	}
	renamePatch := renameSet["chrome/new.cc"]
	if !renamePatch.IsPureRename() {
		t.Fatalf("expected pure rename patch")
	}
	if deletePatch := deleteSet["chrome/dead.cc"]; signature(deletePatch) != "delete:chrome/dead.cc" {
		t.Fatalf("unexpected delete signature: %s", signature(deletePatch))
	}
}

func TestWriteRepoPatchSetWritesMarkersAndReloads(t *testing.T) {
	patchesDir := t.TempDir()
	set := PatchSet{
		"chrome/dead.cc": {
			Path: "chrome/dead.cc",
			Op:   OpDelete,
		},
		"chrome/new.cc": {
			Path:       "chrome/new.cc",
			Op:         OpRename,
			OldPath:    "chrome/old.cc",
			Similarity: 100,
			Content: []byte(`diff --git a/chrome/old.cc b/chrome/new.cc
similarity index 100%
rename from chrome/old.cc
rename to chrome/new.cc
`),
		},
	}
	if _, _, err := WriteRepoPatchSet(patchesDir, set, nil); err != nil {
		t.Fatalf("WriteRepoPatchSet: %v", err)
	}
	if _, err := filepath.Abs(filepath.Join(patchesDir, "chrome", "dead.cc.deleted")); err != nil {
		t.Fatalf("abs: %v", err)
	}
	loaded, err := LoadRepoPatchSet(patchesDir, nil)
	if err != nil {
		t.Fatalf("LoadRepoPatchSet: %v", err)
	}
	if loaded["chrome/dead.cc"].Op != OpDelete {
		t.Fatalf("expected delete marker to round-trip")
	}
	if !loaded["chrome/new.cc"].IsPureRename() {
		t.Fatalf("expected rename marker to round-trip")
	}
}

func TestWriteRepoPatchSetNormalizesBlankContextLines(t *testing.T) {
	patchesDir := t.TempDir()
	set := PatchSet{
		"chrome/browser.cc": {
			Path: "chrome/browser.cc",
			Op:   OpModify,
			Content: []byte("diff --git a/chrome/browser.cc b/chrome/browser.cc\n" +
				"index 111..222 100644\n" +
				"--- a/chrome/browser.cc\n" +
				"+++ b/chrome/browser.cc\n" +
				"@@ -1,3 +1,4 @@\n" +
				" context\n" +
				" \n" +
				"+change\n" +
				" context\n"),
		},
	}

	if _, _, err := WriteRepoPatchSet(patchesDir, set, nil); err != nil {
		t.Fatalf("WriteRepoPatchSet: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(patchesDir, "chrome", "browser.cc"))
	if err != nil {
		t.Fatalf("read patch: %v", err)
	}
	if strings.Contains(string(body), "\n \n") {
		t.Fatalf("expected blank context line to be stored without trailing whitespace:\n%s", body)
	}
}

func TestPathMatchesSkipsInternalState(t *testing.T) {
	if PathMatches(".browseros-patch/state.yaml", nil) {
		t.Fatalf("expected internal state path to be ignored")
	}
}

func TestIsGitlinkPatchDetectsSubprojectDiff(t *testing.T) {
	patchFile := FilePatch{
		Path: "third_party/depot_tools",
		Op:   OpModify,
		Content: []byte(`diff --git a/third_party/depot_tools b/third_party/depot_tools
index 42786f6e46c25c30dd58f69283ab6fcd0c959f58..b40fa9470ee3eb0cb32307834fc5d80181f3ccfb 160000
--- a/third_party/depot_tools
+++ b/third_party/depot_tools
@@ -1 +1 @@
-Subproject commit 42786f6e46c25c30dd58f69283ab6fcd0c959f58
+Subproject commit b40fa9470ee3eb0cb32307834fc5d80181f3ccfb
`),
	}

	if !IsGitlinkPatch(patchFile) {
		t.Fatalf("expected gitlink patch to be detected")
	}
}

func TestCompareIgnoresHunkOffsetDrift(t *testing.T) {
	repo := PatchSet{
		"chrome/browser.cc": {
			Path: "chrome/browser.cc",
			Op:   OpModify,
			Content: []byte(`diff --git a/chrome/browser.cc b/chrome/browser.cc
index 111..222 100644
--- a/chrome/browser.cc
+++ b/chrome/browser.cc
@@ -10,6 +10,7 @@ void Start()
 context
+change
`),
		},
	}
	local := PatchSet{
		"chrome/browser.cc": {
			Path: "chrome/browser.cc",
			Op:   OpModify,
			Content: []byte(`diff --git a/chrome/browser.cc b/chrome/browser.cc
index aaa..bbb 100644
--- a/chrome/browser.cc
+++ b/chrome/browser.cc
@@ -25,6 +25,7 @@ void Start()
 context
+change
`),
		},
	}

	deltas := Compare(repo, local)
	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(deltas))
	}
	if deltas[0].Kind != UpToDate {
		t.Fatalf("expected up-to-date delta, got %s", deltas[0].Kind)
	}
}

func TestBuildRangePatchSetUsesLatestBaseScopedPatch(t *testing.T) {
	ctx := context.Background()
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.name", "Test User")
	runGit(t, repoDir, "config", "user.email", "test@example.com")

	writeRepoFile(t, filepath.Join(repoDir, "chrome", "foo.txt"), "one\n")
	runGit(t, repoDir, "add", "chrome/foo.txt")
	runGit(t, repoDir, "commit", "-m", "base")
	base := gitOutput(t, repoDir, "rev-parse", "HEAD")

	writeRepoFile(t, filepath.Join(repoDir, "chrome", "foo.txt"), "two\n")
	runGit(t, repoDir, "commit", "-am", "step one")
	writeRepoFile(t, filepath.Join(repoDir, "chrome", "foo.txt"), "three\n")
	runGit(t, repoDir, "commit", "-am", "step two")
	end := gitOutput(t, repoDir, "rev-parse", "HEAD")

	set, err := BuildRangePatchSet(ctx, repoDir, base, end, base, false, nil)
	if err != nil {
		t.Fatalf("BuildRangePatchSet: %v", err)
	}
	content := string(set["chrome/foo.txt"].Content)
	if !strings.Contains(content, "+three") {
		t.Fatalf("expected final patch content, got %q", content)
	}
	if strings.Contains(content, "+two") {
		t.Fatalf("expected latest base-scoped patch, got %q", content)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func writeRepoFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
