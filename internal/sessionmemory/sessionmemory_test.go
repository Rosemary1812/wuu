package sessionmemory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
)

func TestAppendReplaceReadAndStatus(t *testing.T) {
	workspaceState := filepath.Join(t.TempDir(), "workspace-state")
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")
	now := time.Date(2026, 6, 12, 9, 30, 0, 0, time.UTC)

	path, n, err := AppendTarget(workspaceState, sessionArtifact, TargetProjectMemory, "Project uses make install for local CLI refresh.", "dream", now)
	if err != nil {
		t.Fatalf("AppendTarget: %v", err)
	}
	if n == 0 || path != filepath.Join(workspaceState, "memory", "MEMORY.md") {
		t.Fatalf("unexpected append result path=%q n=%d", path, n)
	}

	readPath, content, exists, err := ReadTarget(workspaceState, sessionArtifact, TargetProjectMemory)
	if err != nil {
		t.Fatalf("ReadTarget: %v", err)
	}
	if !exists || readPath != path {
		t.Fatalf("read exists/path mismatch exists=%t path=%q", exists, readPath)
	}
	for _, want := range []string{"# Project Memory", "2026-06-12T09:30:00Z (dream)", "Project uses make install"} {
		if !strings.Contains(content, want) {
			t.Fatalf("project memory missing %q:\n%s", want, content)
		}
	}

	replacePath, _, err := ReplaceTarget(workspaceState, sessionArtifact, TargetCheckpoint, "# Session Checkpoint\n\n## Active Intent\n\nShip memory support.")
	if err != nil {
		t.Fatalf("ReplaceTarget: %v", err)
	}
	if replacePath != filepath.Join(sessionArtifact, "memory", "checkpoint.md") {
		t.Fatalf("checkpoint path = %q", replacePath)
	}

	status, err := Status(workspaceState, sessionArtifact)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	byTarget := map[string]FileStatus{}
	for _, item := range status {
		byTarget[item.Target] = item
	}
	if !byTarget[TargetProjectMemory].Exists || byTarget[TargetProjectMemory].Bytes == 0 {
		t.Fatalf("project memory status missing: %+v", status)
	}
	if !byTarget[TargetCheckpoint].Exists || byTarget[TargetCheckpoint].Bytes == 0 {
		t.Fatalf("checkpoint status missing: %+v", status)
	}
	if byTarget[TargetNotes].Exists {
		t.Fatalf("notes should not exist yet: %+v", status)
	}
}

func TestContextBlocksSkipsMissingAndInjectsExistingFiles(t *testing.T) {
	workspaceState := filepath.Join(t.TempDir(), "workspace-state")
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")

	if blocks := ContextBlocks(workspaceState, sessionArtifact); len(blocks) != 0 {
		t.Fatalf("missing memory files should not inject blocks: %+v", blocks)
	}

	if _, _, err := ReplaceTarget(workspaceState, sessionArtifact, TargetProjectMemory, "# Project Memory\n\nDurable project fact."); err != nil {
		t.Fatalf("ReplaceTarget project: %v", err)
	}
	if _, _, err := ReplaceTarget(workspaceState, sessionArtifact, TargetNotes, "# Session Notes\n\nRemember to verify workflow memory."); err != nil {
		t.Fatalf("ReplaceTarget notes: %v", err)
	}

	blocks := ContextBlocks(workspaceState, sessionArtifact)
	if len(blocks) != 2 {
		t.Fatalf("blocks len = %d, want 2: %+v", len(blocks), blocks)
	}
	kinds := map[wuucontext.BlockKind]bool{}
	sources := map[string]bool{}
	for _, block := range blocks {
		kinds[block.Kind] = true
		sources[block.Source] = true
		if block.TokenBudget <= 0 {
			t.Fatalf("block missing token budget: %+v", block)
		}
	}
	if !kinds[wuucontext.BlockMemory] || !kinds[wuucontext.BlockTaskState] {
		t.Fatalf("unexpected block kinds: %+v", blocks)
	}
	if !sources["workspace.memory:project_memory"] || !sources["session.notes:notes"] {
		t.Fatalf("unexpected block sources: %+v", blocks)
	}
}

func TestRejectsUnknownTargetAndEmptyContent(t *testing.T) {
	workspaceState := t.TempDir()
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")

	if _, _, err := AppendTarget(workspaceState, sessionArtifact, "unknown", "fact", "", time.Time{}); err == nil {
		t.Fatal("expected unknown target error")
	}
	if _, _, err := AppendTarget(workspaceState, sessionArtifact, TargetNotes, "   ", "", time.Time{}); err == nil {
		t.Fatal("expected empty content error")
	}

	paths := PathsFor(workspaceState, sessionArtifact)
	if _, err := os.Stat(paths.Notes); !os.IsNotExist(err) {
		t.Fatalf("empty append should not create notes file: %v", err)
	}
}

func TestDreamStateRoundTrip(t *testing.T) {
	workspaceState := t.TempDir()
	initial, err := LoadDreamState(workspaceState)
	if err != nil {
		t.Fatalf("LoadDreamState initial: %v", err)
	}
	if !initial.LastRunAt.IsZero() {
		t.Fatalf("initial dream state = %+v", initial)
	}

	want := DreamState{LastRunAt: time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)}
	if err := SaveDreamState(workspaceState, want); err != nil {
		t.Fatalf("SaveDreamState: %v", err)
	}
	got, err := LoadDreamState(workspaceState)
	if err != nil {
		t.Fatalf("LoadDreamState: %v", err)
	}
	if !got.LastRunAt.Equal(want.LastRunAt) {
		t.Fatalf("dream state = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(DreamStatePath(workspaceState)); err != nil {
		t.Fatalf("dream state path not written: %v", err)
	}
}

func TestDreamStateRecordsRunStatus(t *testing.T) {
	workspaceState := t.TempDir()
	started := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	failed := started.Add(30 * time.Second)
	completed := started.Add(time.Minute)

	if err := RecordDreamStarted(workspaceState, started); err != nil {
		t.Fatalf("RecordDreamStarted: %v", err)
	}
	running, err := LoadDreamState(workspaceState)
	if err != nil {
		t.Fatalf("LoadDreamState running: %v", err)
	}
	if running.LastStatus != DreamStatusRunning || !running.LastStartedAt.Equal(started) || running.LastError != "" {
		t.Fatalf("running state = %+v", running)
	}

	if err := RecordDreamFailed(workspaceState, failed, errors.New("provider timeout")); err != nil {
		t.Fatalf("RecordDreamFailed: %v", err)
	}
	failure, err := LoadDreamState(workspaceState)
	if err != nil {
		t.Fatalf("LoadDreamState failure: %v", err)
	}
	if failure.LastStatus != DreamStatusFailed || !failure.LastFinishedAt.Equal(failed) || !strings.Contains(failure.LastError, "provider timeout") {
		t.Fatalf("failed state = %+v", failure)
	}
	if !failure.LastRunAt.IsZero() {
		t.Fatalf("failed dream should not update LastRunAt: %+v", failure)
	}

	if err := RecordDreamCompleted(workspaceState, completed); err != nil {
		t.Fatalf("RecordDreamCompleted: %v", err)
	}
	done, err := LoadDreamState(workspaceState)
	if err != nil {
		t.Fatalf("LoadDreamState completed: %v", err)
	}
	if done.LastStatus != DreamStatusCompleted || !done.LastRunAt.Equal(completed) || !done.LastFinishedAt.Equal(completed) || done.LastError != "" {
		t.Fatalf("completed state = %+v", done)
	}
}

func TestDreamLockExcludesConcurrentRunsAndReleases(t *testing.T) {
	workspaceState := t.TempDir()
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	first, acquired, err := TryAcquireDreamLock(workspaceState, time.Hour, now)
	if err != nil {
		t.Fatalf("TryAcquireDreamLock first: %v", err)
	}
	if !acquired || first == nil {
		t.Fatal("expected first dream lock to be acquired")
	}
	second, acquired, err := TryAcquireDreamLock(workspaceState, time.Hour, now)
	if err != nil {
		t.Fatalf("TryAcquireDreamLock second: %v", err)
	}
	if acquired || second != nil {
		t.Fatalf("second lock acquired=%t lock=%+v, want blocked", acquired, second)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release first: %v", err)
	}
	third, acquired, err := TryAcquireDreamLock(workspaceState, time.Hour, now)
	if err != nil {
		t.Fatalf("TryAcquireDreamLock third: %v", err)
	}
	if !acquired || third == nil {
		t.Fatal("expected lock after release to be acquired")
	}
	if err := third.Release(); err != nil {
		t.Fatalf("Release third: %v", err)
	}
}

func TestDreamLockReclaimsStaleLock(t *testing.T) {
	workspaceState := t.TempDir()
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	first, acquired, err := TryAcquireDreamLock(workspaceState, time.Hour, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("TryAcquireDreamLock first: %v", err)
	}
	if !acquired || first == nil {
		t.Fatal("expected first dream lock to be acquired")
	}
	old := now.Add(-2 * time.Hour)
	if err := os.Chtimes(DreamLockPath(workspaceState), old, old); err != nil {
		t.Fatalf("Chtimes lock: %v", err)
	}

	second, acquired, err := TryAcquireDreamLock(workspaceState, time.Hour, now)
	if err != nil {
		t.Fatalf("TryAcquireDreamLock stale: %v", err)
	}
	if !acquired || second == nil {
		t.Fatal("expected stale dream lock to be reclaimed")
	}
	if err := first.Release(); err != nil {
		t.Fatalf("stale holder release should be token-safe: %v", err)
	}
	if _, err := os.Stat(DreamLockPath(workspaceState)); err != nil {
		t.Fatalf("new lock should remain after stale holder release: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("Release second: %v", err)
	}
}
