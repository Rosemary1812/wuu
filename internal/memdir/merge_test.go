package memdir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTopic(t *testing.T, dir, file, name, description, body string) {
	t.Helper()
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("ensure %s: %v", dir, err)
	}
	if err := writeTopicFile(dir, file, name, description, "lesson", body); err != nil {
		t.Fatalf("write topic %s: %v", file, err)
	}
	if err := appendIndexLines(dir, []string{indexLine(name, file, description)}); err != nil {
		t.Fatalf("append index for %s: %v", file, err)
	}
}

func readIndex(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, EntrypointName))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read index: %v", err)
	}
	return string(raw)
}

// TestMergeNotebookUnionDedup covers the common fork case: the fork snapshot
// carries all of the母体's unchanged topics (byte-identical → skipped) plus a
// new topic the fork wrote while running (→ added as a new topic + index line).
func TestMergeNotebookUnionDedup(t *testing.T) {
	root := t.TempDir()
	mother := filepath.Join(root, "mother")
	fork := filepath.Join(root, "fork")

	// A shared, unchanged topic exists in both notebooks.
	writeTopic(t, mother, "deploy-flow.md", "Deploy flow", "how we ship", "Run the deploy script.")
	writeTopic(t, fork, "deploy-flow.md", "Deploy flow", "how we ship", "Run the deploy script.")
	// The fork learned something new.
	writeTopic(t, fork, "auth-bug.md", "Auth bug", "root cause of the auth flake", "The token TTL was too short.")

	res, err := MergeNotebook(fork, mother)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if res.TopicsAdded != 1 || res.TopicsSkipped != 1 || res.Conflicts != 0 {
		t.Fatalf("merge result = %+v, want added=1 skipped=1 conflicts=0", res)
	}
	// The new topic file landed in the母体.
	if _, err := os.Stat(filepath.Join(mother, "auth-bug.md")); err != nil {
		t.Fatalf("merged topic should exist in mother: %v", err)
	}
	// The unchanged topic was NOT duplicated.
	if _, err := os.Stat(filepath.Join(mother, "auth-bug-fork.md")); !os.IsNotExist(err) {
		t.Fatalf("no conflict copy expected, stat err=%v", err)
	}
	idx := readIndex(t, mother)
	if !strings.Contains(idx, "auth-bug.md") {
		t.Fatalf("mother index should gain the new topic line, got:\n%s", idx)
	}
	if strings.Count(idx, "deploy-flow.md") != 1 {
		t.Fatalf("shared topic must appear exactly once in index, got:\n%s", idx)
	}
}

// TestMergeNotebookSameNameConflict covers the sole real conflict: both sides
// edited a topic of the same file name to different content. The母体's version
// stays authoritative; the fork's is preserved under a "-fork" name so no
// experience is lost.
func TestMergeNotebookSameNameConflict(t *testing.T) {
	root := t.TempDir()
	mother := filepath.Join(root, "mother")
	fork := filepath.Join(root, "fork")

	writeTopic(t, mother, "policy.md", "Policy", "team policy", "Mother's authoritative policy.")
	writeTopic(t, fork, "policy.md", "Policy", "team policy", "Fork edited the policy differently.")

	res, err := MergeNotebook(fork, mother)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if res.Conflicts != 1 || res.TopicsAdded != 1 {
		t.Fatalf("merge result = %+v, want conflicts=1 added=1", res)
	}
	// Mother's version is untouched.
	got, err := os.ReadFile(filepath.Join(mother, "policy.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "authoritative") {
		t.Fatalf("mother's policy.md should be untouched, got:\n%s", got)
	}
	// The fork's divergent version is preserved under a distinct name.
	conflict, err := os.ReadFile(filepath.Join(mother, "policy-fork.md"))
	if err != nil {
		t.Fatalf("fork's divergent topic should be preserved: %v", err)
	}
	if !strings.Contains(string(conflict), "Fork edited") {
		t.Fatalf("conflict copy should hold the fork's version, got:\n%s", conflict)
	}
}

// TestMergeNotebookMissingSource is a no-op merge when the fork never wrote a
// notebook (a fork that accumulated nothing).
func TestMergeNotebookMissingSource(t *testing.T) {
	root := t.TempDir()
	mother := filepath.Join(root, "mother")
	writeTopic(t, mother, "seed.md", "Seed", "initial", "Only the母体 has memory.")

	res, err := MergeNotebook(filepath.Join(root, "does-not-exist"), mother)
	if err != nil {
		t.Fatalf("merge with missing source should not error: %v", err)
	}
	if res.TopicsAdded != 0 || res.TopicsSkipped != 0 || res.Conflicts != 0 {
		t.Fatalf("missing-source merge should be a no-op, got %+v", res)
	}
}

// TestMergeNotebookIsIdempotent proves re-running the same merge adds nothing
// the second time (content de-dup covers a retried/partial merge).
func TestMergeNotebookIsIdempotent(t *testing.T) {
	root := t.TempDir()
	mother := filepath.Join(root, "mother")
	fork := filepath.Join(root, "fork")
	writeTopic(t, fork, "lesson.md", "Lesson", "a new lesson", "Cache invalidation is hard.")

	first, err := MergeNotebook(fork, mother)
	if err != nil {
		t.Fatalf("first merge: %v", err)
	}
	if first.TopicsAdded != 1 {
		t.Fatalf("first merge added = %d, want 1", first.TopicsAdded)
	}
	second, err := MergeNotebook(fork, mother)
	if err != nil {
		t.Fatalf("second merge: %v", err)
	}
	if second.TopicsAdded != 0 || second.TopicsSkipped != 1 {
		t.Fatalf("second merge = %+v, want added=0 skipped=1 (idempotent)", second)
	}
}
