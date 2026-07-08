package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// These tests consolidate the "a stale artifact cannot be blindly overwritten"
// guarantee. The file layer already enforces read-before-write SHA content
// addressing (stale_file_baseline) for write_file/edit_file, and apply_patch
// guards via anchor matching against the current file. There is no separate
// durable version table; the per-Env in-memory read ledger is the whole
// mechanism. Each case asserts BOTH the refusal (with the right error_kind)
// AND that the target file on disk is byte-for-byte unchanged.

// Scenario 1: write_file catches an out-of-band change even when the attacker
// preserves size and mtime. Same-length replacement + Chtimes restores mtime,
// so the ONLY differing signal is the recorded content SHA. This exercises
// write_file's SHA branch (validateExistingWrite); previously only edit_file
// had an equivalent Chtimes-preserved test.
func TestToolkit_WriteFileRejectsStaleReadWithPreservedMtime(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(root, "a.txt")
	mustWriteFile(t, path, "alpha\n")

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"a.txt"}`,
	}); err != nil {
		t.Fatalf("read_file: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	originalModTime := info.ModTime()

	// Out-of-band change: same length ("bravo\n" == 6 bytes == "alpha\n"),
	// mtime restored, so size and mtime match the baseline exactly. Only the
	// SHA differs.
	mustWriteFile(t, path, "bravo\n")
	if err := os.Chtimes(path, originalModTime, originalModTime); err != nil {
		t.Fatalf("preserve modtime: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"a.txt","content":"gamma\n"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=stale_file_baseline") {
		t.Fatalf("expected stale-baseline refusal from SHA branch, got: %v", err)
	}
	if got := mustReadFile(t, path); got != "bravo\n" {
		t.Fatalf("stale write must not mutate file, got: %q", got)
	}
}

// Scenario 2: edit_file's size-mismatch branch (validateEditBaseline's
// readEntry.Size != info.Size() check) fires when the out-of-band change
// alters the file length. This branch is checked before the SHA compare, so a
// different-length external write is rejected regardless of content hash.
func TestToolkit_EditFileRejectsStaleReadOnSizeMismatch(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(root, "a.txt")
	mustWriteFile(t, path, "alpha\n")

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"a.txt"}`,
	}); err != nil {
		t.Fatalf("read_file: %v", err)
	}

	// External change to a DIFFERENT length (still contains the anchor so the
	// only reason to reject is the baseline, not a missing anchor).
	mustWriteFile(t, path, "alpha extended body line\n")

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"a.txt","old_text":"alpha","new_text":"ALPHA"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=stale_file_baseline") {
		t.Fatalf("expected stale-baseline refusal from size branch, got: %v", err)
	}
	if got := mustReadFile(t, path); got != "alpha extended body line\n" {
		t.Fatalf("stale edit must not mutate file, got: %q", got)
	}
}

// Scenario 3: staleness detected against a write-recorded baseline (not a
// read). A create via write_file records a BaselineOnly ledger entry; an
// out-of-band change afterwards must make the next write/edit stale even
// though read_file was never called for the current on-disk bytes.
func TestToolkit_WriteFileRejectsStaleWriteBaseline(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(root, "new.txt")

	// Create the file through the tool: this records a BaselineOnly entry, no
	// read_file involved.
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"new.txt","content":"alpha\n"}`,
	}); err != nil {
		t.Fatalf("write_file create: %v", err)
	}

	// Out-of-band change after the write baseline was recorded.
	mustWriteFile(t, path, "bravo\n")

	// A second write without re-reading must be rejected as stale: the write
	// baseline is now out of date.
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"new.txt","content":"charlie\n"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=stale_file_baseline") {
		t.Fatalf("expected stale-baseline refusal against write baseline, got: %v", err)
	}
	if got := mustReadFile(t, path); got != "bravo\n" {
		t.Fatalf("stale write against write baseline must not mutate file, got: %q", got)
	}

	// An edit against the same stale write baseline is likewise refused.
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"new.txt","old_text":"bravo","new_text":"BRAVO"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=stale_file_baseline") {
		t.Fatalf("expected stale-baseline edit refusal against write baseline, got: %v", err)
	}
	if got := mustReadFile(t, path); got != "bravo\n" {
		t.Fatalf("stale edit against write baseline must not mutate file, got: %q", got)
	}
}

// Scenario 4: an edit_file -> edit_file chain refreshes the baseline after each
// successful edit, so a second edit needs no re-read on the now-current
// content. The refresh path still guards: an out-of-band change after the
// chain makes the next edit stale.
func TestToolkit_EditFileChainRefreshesBaselineAndStillGuards(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(root, "a.txt")
	mustWriteFile(t, path, "alpha\nbravo\ncharlie\n")

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"a.txt"}`,
	}); err != nil {
		t.Fatalf("read_file: %v", err)
	}

	// First edit succeeds and refreshes the write baseline.
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"a.txt","old_text":"alpha","new_text":"ALPHA"}`,
	}); err != nil {
		t.Fatalf("first edit: %v", err)
	}

	// Second edit WITHOUT a re-read: succeeds only because the first edit
	// refreshed the baseline to the current content.
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"a.txt","old_text":"bravo","new_text":"BRAVO"}`,
	}); err != nil {
		t.Fatalf("chained edit should succeed against refreshed baseline: %v", err)
	}
	if got := mustReadFile(t, path); got != "ALPHA\nBRAVO\ncharlie\n" {
		t.Fatalf("unexpected content after edit chain: %q", got)
	}

	// The refresh path still guards: an out-of-band change makes the next edit
	// stale even though the baseline was just refreshed by the chain.
	mustWriteFile(t, path, "externally rewritten content\n")
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"a.txt","old_text":"charlie","new_text":"CHARLIE"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=stale_file_baseline") {
		t.Fatalf("expected stale-baseline refusal after external change post-refresh, got: %v", err)
	}
	if got := mustReadFile(t, path); got != "externally rewritten content\n" {
		t.Fatalf("stale edit after refresh must not mutate file, got: %q", got)
	}
}

// Scenario 5: apply_patch has NO read baseline. Its conflict protection is
// pure anchor matching against the current file. This test documents both
// sides of that design:
//   - if the out-of-band change removes the anchor lines, the patch is refused
//     with anchor_not_found and the file is unchanged;
//   - if the out-of-band change touches only unrelated lines and the anchor
//     still matches, the patch applies against the current file by design
//     (last-writer-wins on non-anchor regions), preserving the external edit.
func TestToolkit_ApplyPatchAnchorConflictAndTOCTOU(t *testing.T) {
	patchFor := func(t *testing.T, filename string) string {
		t.Helper()
		patchText := "*** Begin Patch\n*** Update File: " + filename + "\n@@\n-beta\n+BETA\n*** End Patch"
		args, err := json.Marshal(map[string]string{"patchText": patchText})
		if err != nil {
			t.Fatalf("marshal patch args: %v", err)
		}
		return string(args)
	}

	t.Run("anchor removed by external change is rejected", func(t *testing.T) {
		root := t.TempDir()
		kit, err := New(root)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		kit.SetEditToolMode(EditToolModePatch)
		path := filepath.Join(root, "a.txt")
		mustWriteFile(t, path, "alpha\nbeta\ngamma\n")

		// The patch is generated against a read snapshot; apply_patch does not
		// consult that read baseline, so this read only mirrors a realistic
		// flow.
		if _, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "read_file",
			Arguments: `{"path":"a.txt"}`,
		}); err != nil {
			t.Fatalf("read_file: %v", err)
		}

		// Out-of-band change removes the anchor line before the patch lands.
		mustWriteFile(t, path, "alpha\nCHANGED\ngamma\n")

		_, err = kit.Execute(context.Background(), providers.ToolCall{
			Name:      "apply_patch",
			Arguments: patchFor(t, "a.txt"),
		})
		if err == nil || !strings.Contains(err.Error(), "anchor_not_found") {
			t.Fatalf("expected anchor_not_found after external anchor removal, got: %v", err)
		}
		if got := mustReadFile(t, path); got != "alpha\nCHANGED\ngamma\n" {
			t.Fatalf("failed patch must not mutate file, got: %q", got)
		}
	})

	t.Run("anchor still present applies against current content by design", func(t *testing.T) {
		root := t.TempDir()
		kit, err := New(root)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		kit.SetEditToolMode(EditToolModePatch)
		path := filepath.Join(root, "a.txt")
		mustWriteFile(t, path, "alpha\nbeta\ngamma\n")

		// Out-of-band change touches an unrelated line; the "beta" anchor is
		// untouched, so apply_patch applies against the CURRENT file. There is
		// no whole-file baseline, so the external "gamma" -> "GAMMA" edit is
		// preserved rather than rejected. This is documented, intentional
		// last-writer-wins behavior for non-anchor regions, not a bug.
		mustWriteFile(t, path, "alpha\nbeta\nGAMMA\n")

		if _, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "apply_patch",
			Arguments: patchFor(t, "a.txt"),
		}); err != nil {
			t.Fatalf("apply_patch with matching anchor should apply against current content: %v", err)
		}
		if got := mustReadFile(t, path); got != "alpha\nBETA\nGAMMA\n" {
			t.Fatalf("apply_patch should patch the anchor and keep the external edit, got: %q", got)
		}
	})
}

// Scenario 6: a ranged read (offset/limit) records the WHOLE-file SHA, so it
// still authorizes and guards a full overwrite. With no intervening change a
// full write succeeds; after an out-of-band change the same write is rejected
// as stale.
func TestToolkit_WriteFileRangedReadBaselineGuardsWholeFile(t *testing.T) {
	original := "line1\nline2\nline3\nline4\n"

	t.Run("ranged read authorizes unchanged full overwrite", func(t *testing.T) {
		root := t.TempDir()
		kit, err := New(root)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		path := filepath.Join(root, "a.txt")
		mustWriteFile(t, path, original)

		// Read only lines 2-3, but the ledger records the whole-file SHA.
		if _, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "read_file",
			Arguments: `{"path":"a.txt","offset":2,"limit":2}`,
		}); err != nil {
			t.Fatalf("ranged read_file: %v", err)
		}

		// No intervening change: full overwrite succeeds because the whole-file
		// SHA still matches.
		if _, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "write_file",
			Arguments: `{"path":"a.txt","content":"rewritten\n"}`,
		}); err != nil {
			t.Fatalf("full write after unchanged ranged read should succeed: %v", err)
		}
		if got := mustReadFile(t, path); got != "rewritten\n" {
			t.Fatalf("unexpected content after full overwrite: %q", got)
		}
	})

	t.Run("ranged read still guards on whole-file SHA", func(t *testing.T) {
		root := t.TempDir()
		kit, err := New(root)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		path := filepath.Join(root, "a.txt")
		mustWriteFile(t, path, original)

		if _, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "read_file",
			Arguments: `{"path":"a.txt","offset":2,"limit":2}`,
		}); err != nil {
			t.Fatalf("ranged read_file: %v", err)
		}

		// Out-of-band change to a line OUTSIDE the read range still changes the
		// whole-file SHA, so the full overwrite is rejected as stale.
		mustWriteFile(t, path, "line1\nline2\nline3\nEXTERNAL\n")
		_, err = kit.Execute(context.Background(), providers.ToolCall{
			Name:      "write_file",
			Arguments: `{"path":"a.txt","content":"rewritten\n"}`,
		})
		if err == nil || !strings.Contains(err.Error(), "error_kind=stale_file_baseline") {
			t.Fatalf("expected stale-baseline refusal on whole-file SHA, got: %v", err)
		}
		if got := mustReadFile(t, path); got != "line1\nline2\nline3\nEXTERNAL\n" {
			t.Fatalf("stale full write must not mutate file, got: %q", got)
		}
	})
}
