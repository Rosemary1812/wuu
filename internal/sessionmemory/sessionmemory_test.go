package sessionmemory

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	summaryPath, _, err := ReplaceTarget(workspaceState, sessionArtifact, TargetSummary, "# Session Summary\n\n## Active Intent\n\nShip memory support.")
	if err != nil {
		t.Fatalf("ReplaceTarget summary: %v", err)
	}
	if summaryPath != filepath.Join(sessionArtifact, "session-memory", "summary.md") {
		t.Fatalf("summary path = %q", summaryPath)
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
	if !byTarget[TargetSummary].Exists || byTarget[TargetSummary].Bytes == 0 {
		t.Fatalf("summary status missing: %+v", status)
	}
	if byTarget[TargetNotes].Exists {
		t.Fatalf("notes should not exist yet: %+v", status)
	}
}

func TestAppendTargetSerializesAcrossProcesses(t *testing.T) {
	workspaceState := filepath.Join(t.TempDir(), "workspace-state")
	const processCount = 12

	type processResult struct {
		marker string
		output []byte
		err    error
	}
	start := make(chan struct{})
	results := make(chan processResult, processCount)
	markers := make([]string, processCount)
	for i := 0; i < processCount; i++ {
		marker := fmt.Sprintf("[session-memory-marker-%02d]", i)
		markers[i] = marker
		go func() {
			<-start
			command := exec.Command(os.Args[0], "-test.run=^TestAppendTargetProcessHelper$")
			command.Env = append(
				os.Environ(),
				"WUU_SESSION_MEMORY_APPEND_HELPER=1",
				"WUU_SESSION_MEMORY_WORKSPACE="+workspaceState,
				"WUU_SESSION_MEMORY_MARKER="+marker,
			)
			output, err := command.CombinedOutput()
			results <- processResult{marker: marker, output: output, err: err}
		}()
	}
	close(start)
	for range processCount {
		result := <-results
		if result.err != nil {
			t.Errorf("append %s: %v\n%s", result.marker, result.err, result.output)
		}
	}
	if t.Failed() {
		return
	}

	path, content, exists, err := ReadTarget(workspaceState, "", TargetProjectMemory)
	if err != nil {
		t.Fatalf("ReadTarget: %v", err)
	}
	if !exists {
		t.Fatal("project memory was not written")
	}
	for _, marker := range markers {
		if count := strings.Count(content, marker); count != 1 {
			t.Fatalf("marker %s appears %d times, want exactly once\n%s", marker, count, content)
		}
	}

	lockPath := path + ".lock"
	before, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat target lock: %v", err)
	}
	if _, _, err := ReplaceTarget(workspaceState, "", TargetProjectMemory, "# Project Memory\n\nReplacement"); err != nil {
		t.Fatalf("ReplaceTarget: %v", err)
	}
	after, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat target lock after replace: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("target lock sidecar inode changed across writes")
	}
}

func TestAppendTargetProcessHelper(t *testing.T) {
	if os.Getenv("WUU_SESSION_MEMORY_APPEND_HELPER") != "1" {
		return
	}
	workspaceState := os.Getenv("WUU_SESSION_MEMORY_WORKSPACE")
	marker := os.Getenv("WUU_SESSION_MEMORY_MARKER")
	if _, _, err := AppendTarget(
		workspaceState,
		"",
		TargetProjectMemory,
		marker,
		"concurrency-test",
		time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatal(err)
	}
}

func TestRequestContextBlocksSkipsMissingAndInjectsExistingFiles(t *testing.T) {
	workspaceState := filepath.Join(t.TempDir(), "workspace-state")
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")

	if blocks := RequestContextBlocks(workspaceState, sessionArtifact); len(blocks) != 0 {
		t.Fatalf("missing memory files should not inject blocks: %+v", blocks)
	}

	if _, _, err := ReplaceTarget(workspaceState, sessionArtifact, TargetNotes, "# Session Notes\n\nRemember to verify workflow memory."); err != nil {
		t.Fatalf("ReplaceTarget notes: %v", err)
	}

	blocks := RequestContextBlocks(workspaceState, sessionArtifact)
	if len(blocks) != 1 {
		t.Fatalf("blocks len = %d, want 1: %+v", len(blocks), blocks)
	}
	if blocks[0].Kind != wuucontext.BlockTaskState {
		t.Fatalf("unexpected block kind: %+v", blocks)
	}
	if blocks[0].Source != "session.notes:notes" {
		t.Fatalf("unexpected block source: %+v", blocks)
	}
	if blocks[0].TokenBudget <= 0 {
		t.Fatalf("block missing token budget: %+v", blocks[0])
	}
}

func TestRequestContextBlocksExcludesProjectMemory(t *testing.T) {
	workspaceState := filepath.Join(t.TempDir(), "workspace-state")
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")

	if _, _, err := ReplaceTarget(workspaceState, sessionArtifact, TargetProjectMemory, "# Project Memory\n\nDurable project fact."); err != nil {
		t.Fatalf("ReplaceTarget project: %v", err)
	}
	if _, _, err := ReplaceTarget(workspaceState, sessionArtifact, TargetSummary, "# Session Summary\n\nActive task state."); err != nil {
		t.Fatalf("ReplaceTarget summary: %v", err)
	}
	if _, _, err := ReplaceTarget(workspaceState, sessionArtifact, TargetNotes, "# Session Notes\n\nScratch state."); err != nil {
		t.Fatalf("ReplaceTarget notes: %v", err)
	}

	blocks := RequestContextBlocks(workspaceState, sessionArtifact)
	if len(blocks) != 2 {
		t.Fatalf("request blocks len = %d, want 2: %+v", len(blocks), blocks)
	}
	sources := map[string]bool{}
	for _, block := range blocks {
		sources[block.Source] = true
		if block.Kind == wuucontext.BlockMemory || strings.Contains(block.Content, "Durable project fact") {
			t.Fatalf("request context should not include project memory: %+v", blocks)
		}
	}
	if !sources["session.summary:summary"] || !sources["session.notes:notes"] {
		t.Fatalf("request context missing session state sources: %+v", blocks)
	}
}

func TestRequestContextBlocksPrefersSummaryOverLegacyCheckpoint(t *testing.T) {
	workspaceState := filepath.Join(t.TempDir(), "workspace-state")
	sessionArtifact := filepath.Join(workspaceState, "sessions", "session-1")

	if _, _, err := ReplaceTarget(workspaceState, sessionArtifact, TargetCheckpoint, "# Session Checkpoint\n\nLegacy checkpoint."); err != nil {
		t.Fatalf("ReplaceTarget checkpoint: %v", err)
	}
	if _, _, err := ReplaceTarget(workspaceState, sessionArtifact, TargetSummary, "# Session Summary\n\nCurrent summary."); err != nil {
		t.Fatalf("ReplaceTarget summary: %v", err)
	}

	blocks := RequestContextBlocks(workspaceState, sessionArtifact)
	if len(blocks) != 1 {
		t.Fatalf("blocks len = %d, want 1: %+v", len(blocks), blocks)
	}
	if blocks[0].Source != "session.summary:summary" || !strings.Contains(blocks[0].Content, "Current summary") {
		t.Fatalf("summary block not injected: %+v", blocks[0])
	}
	if strings.Contains(blocks[0].Content, "Legacy checkpoint") {
		t.Fatalf("legacy checkpoint should not be injected when summary exists: %+v", blocks[0])
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

	first, acquired, err := TryAcquireDreamLock(workspaceState)
	if err != nil {
		t.Fatalf("TryAcquireDreamLock first: %v", err)
	}
	if !acquired || first == nil {
		t.Fatal("expected first dream lock to be acquired")
	}
	second, acquired, err := TryAcquireDreamLock(workspaceState)
	if err != nil {
		t.Fatalf("TryAcquireDreamLock second: %v", err)
	}
	if acquired || second != nil {
		t.Fatalf("second lock acquired=%t lock=%+v, want blocked", acquired, second)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release first: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("second Release first: %v", err)
	}
	third, acquired, err := TryAcquireDreamLock(workspaceState)
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

func TestDreamLockDoesNotExpireLiveHolderOrReplaceSidecar(t *testing.T) {
	workspaceState := t.TempDir()
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	first, acquired, err := TryAcquireDreamLock(workspaceState)
	if err != nil {
		t.Fatalf("TryAcquireDreamLock first: %v", err)
	}
	if !acquired || first == nil {
		t.Fatal("expected first dream lock to be acquired")
	}
	lockPath := DreamLockPath(workspaceState)
	before, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat dream lock: %v", err)
	}
	old := now.Add(-24 * time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("Chtimes lock: %v", err)
	}

	second, acquired, err := TryAcquireDreamLock(workspaceState)
	if err != nil {
		t.Fatalf("TryAcquireDreamLock contender: %v", err)
	}
	if acquired || second != nil {
		t.Fatal("sidecar age must not revoke a live dream lock")
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release first: %v", err)
	}
	third, acquired, err := TryAcquireDreamLock(workspaceState)
	if err != nil {
		t.Fatalf("TryAcquireDreamLock after release: %v", err)
	}
	if !acquired || third == nil {
		t.Fatal("expected lock after release to be acquired")
	}
	after, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat dream lock after reacquire: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("dream lock sidecar inode changed")
	}
	if err := third.Release(); err != nil {
		t.Fatalf("Release third: %v", err)
	}
}

func TestDreamLockReleasedWhenProcessExits(t *testing.T) {
	workspaceState := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestDreamLockProcessHelper$")
	cmd.Env = append(
		os.Environ(),
		"WUU_DREAM_LOCK_HELPER=1",
		"WUU_DREAM_LOCK_WORKSPACE="+workspaceState,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	ready := make(chan error, 1)
	go func() {
		line, readErr := bufio.NewReader(stdout).ReadString('\n')
		if readErr != nil {
			ready <- fmt.Errorf("read helper readiness: %w", readErr)
			return
		}
		if strings.TrimSpace(line) != "acquired" {
			ready <- fmt.Errorf("unexpected helper output %q", line)
			return
		}
		ready <- nil
	}()

	select {
	case err := <-ready:
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("helper readiness: %v; stderr: %s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("helper did not acquire lock; stderr: %s", stderr.String())
	}

	contender, acquired, err := TryAcquireDreamLock(workspaceState)
	if err != nil {
		t.Fatalf("TryAcquireDreamLock while helper is alive: %v", err)
	}
	if acquired || contender != nil {
		if contender != nil {
			_ = contender.Release()
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("expected helper process to retain dream lock while alive")
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected killed helper to exit unsuccessfully")
	}

	lock, acquired, err := TryAcquireDreamLock(workspaceState)
	if err != nil {
		t.Fatalf("TryAcquireDreamLock after helper exit: %v", err)
	}
	if !acquired || lock == nil {
		t.Fatal("expected lock to be released when helper process exited")
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func TestDreamLockProcessHelper(t *testing.T) {
	if os.Getenv("WUU_DREAM_LOCK_HELPER") != "1" {
		return
	}
	lock, acquired, err := TryAcquireDreamLock(os.Getenv("WUU_DREAM_LOCK_WORKSPACE"))
	if err != nil {
		t.Fatalf("TryAcquireDreamLock: %v", err)
	}
	if !acquired || lock == nil {
		t.Fatal("expected helper to acquire dream lock")
	}
	defer lock.Release()
	fmt.Println("acquired")
	for {
		time.Sleep(time.Hour)
	}
}
