package process

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStartListAndPersist(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "state", "runtime")
	m, err := NewManager(root, runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Start(context.Background(), StartOptions{Command: "echo hello; sleep 1", OwnerKind: OwnerMainAgent, OwnerID: "main", Lifecycle: LifecycleManaged})
	if err != nil {
		t.Fatal(err)
	}
	if p.OwnerKind != OwnerMainAgent || p.Lifecycle != LifecycleManaged || p.Status != StatusRunning {
		t.Fatalf("unexpected process: %+v", p)
	}
	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 process, got %d", len(list))
	}
	if _, err := os.Stat(filepath.Join(runtimeDir, "processes", p.ID+".json")); err != nil {
		t.Fatal(err)
	}
}

func TestStartDetachesProcessLifecycleFromStartContext(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, filepath.Join(root, "state", "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p, err := m.Start(ctx, StartOptions{Command: "sleep 30", OwnerKind: OwnerMainAgent, OwnerID: "main", Lifecycle: LifecycleManaged})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	time.Sleep(200 * time.Millisecond)

	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	var status Status
	for _, proc := range list {
		if proc.ID == p.ID {
			status = proc.Status
			break
		}
	}
	if status != StatusRunning {
		t.Fatalf("expected process to keep running after start context cancel, got %s", status)
	}

	stopped, err := m.Stop(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped == nil || (stopped.Status != StatusStopped && stopped.Status != StatusFailed) {
		t.Fatalf("expected manager stop to end process, got %+v", stopped)
	}
}

func TestStopStopsProcessGroup(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root, filepath.Join(root, "state", "runtime"))
	p, err := m.Start(context.Background(), StartOptions{Command: "sleep 30 & wait", OwnerKind: OwnerSubagent, OwnerID: "worker-1", Lifecycle: LifecycleSession})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Stop(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	proc, _ := os.FindProcess(p.PID)
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		// allow a brief settle, then fail if still alive
		time.Sleep(200 * time.Millisecond)
		if err := proc.Signal(syscall.Signal(0)); err == nil {
			t.Fatal("process still alive after stop")
		}
	}
}

func TestCleanupSessionOnlyStopsSessionLifecycle(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root, filepath.Join(root, "state", "runtime"))
	sessionProc, err := m.Start(context.Background(), StartOptions{Command: "sleep 30", OwnerKind: OwnerMainAgent, OwnerID: "main", Lifecycle: LifecycleSession})
	if err != nil {
		t.Fatal(err)
	}
	managedProc, err := m.Start(context.Background(), StartOptions{Command: "sleep 30", OwnerKind: OwnerMainAgent, OwnerID: "main", Lifecycle: LifecycleManaged})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.CleanupSession(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	list, _ := m.List()
	var gotSession, gotManaged Status
	for _, p := range list {
		if p.ID == sessionProc.ID {
			gotSession = p.Status
		}
		if p.ID == managedProc.ID {
			gotManaged = p.Status
		}
	}
	if gotSession != StatusStopped && gotSession != StatusFailed {
		t.Fatalf("session process not stopped: %s", gotSession)
	}
	if gotManaged != StatusRunning {
		t.Fatalf("managed process should keep running, got %s", gotManaged)
	}
	_, _ = m.Stop(managedProc.ID)
}

func TestReadOutput(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root, filepath.Join(root, "state", "runtime"))
	p, err := m.Start(context.Background(), StartOptions{Command: "echo ready; sleep 1", OwnerKind: OwnerMainAgent, OwnerID: "main", Lifecycle: LifecycleSession})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	out, _, err := m.ReadOutput(p.ID, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ready") {
		t.Fatalf("unexpected output: %q", out)
	}
	_, _ = m.Stop(p.ID)
}

func TestUpdatePreviewPersistsLocalPreviewURLs(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root, filepath.Join(root, "state", "runtime"))
	p, err := m.Start(context.Background(), StartOptions{Command: "sleep 1", OwnerKind: OwnerMainAgent, OwnerID: "thread-1", Lifecycle: LifecycleSession})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop(p.ID)

	updated, err := m.UpdatePreview(p.ID, []string{
		"http://127.0.0.1:5173/",
		"https://example.com/not-local",
		"http://localhost:3000/app",
	}, "http://localhost:3000/app")
	if err != nil {
		t.Fatal(err)
	}
	if updated.PrimaryPreviewURL != "http://localhost:3000/app" {
		t.Fatalf("PrimaryPreviewURL = %q", updated.PrimaryPreviewURL)
	}
	if len(updated.PreviewURLs) != 2 || updated.PreviewURLs[0] != "http://localhost:5173/" || updated.PreviewURLs[1] != "http://localhost:3000/app" {
		t.Fatalf("unexpected preview urls: %+v", updated.PreviewURLs)
	}

	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].PrimaryPreviewURL != "http://localhost:3000/app" {
		t.Fatalf("preview metadata not persisted: %+v", list)
	}
}

func TestPreviewURLsFromTextOnlyAcceptsLocalhost(t *testing.T) {
	got := PreviewURLsFromText(`ready at http://localhost:5173/ and http://127.0.0.1:3000/docs but not https://example.com`)
	want := []string{"http://localhost:5173/", "http://localhost:3000/docs"}
	if len(got) != len(want) {
		t.Fatalf("PreviewURLsFromText = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("PreviewURLsFromText = %+v, want %+v", got, want)
		}
	}
}

func TestStartTTYProvidesTerminalSemantics(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root, filepath.Join(root, "state", "runtime"))
	ttyProc, err := m.Start(context.Background(), StartOptions{
		Command:   "if test -t 1; then echo MODE_TTY; else echo MODE_PIPE; fi; sleep 1",
		OwnerKind: OwnerMainAgent,
		OwnerID:   "main",
		Lifecycle: LifecycleSession,
		TTY:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop(ttyProc.ID)
	pipeProc, err := m.Start(context.Background(), StartOptions{
		Command:   "if test -t 1; then echo MODE_TTY; else echo MODE_PIPE; fi; sleep 1",
		OwnerKind: OwnerMainAgent,
		OwnerID:   "main",
		Lifecycle: LifecycleSession,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop(pipeProc.ID)

	ttyOffset := int64(0)
	ttyOut, err := m.ReadOutputSnapshot(context.Background(), ttyProc.ID, OutputReadOptions{OffsetBytes: &ttyOffset, Wait: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !ttyOut.Process.TTY || !strings.Contains(ttyOut.Output, "MODE_TTY") {
		t.Fatalf("expected tty process output, got %+v output=%q", ttyOut.Process, ttyOut.Output)
	}
	pipeOffset := int64(0)
	pipeOut, err := m.ReadOutputSnapshot(context.Background(), pipeProc.ID, OutputReadOptions{OffsetBytes: &pipeOffset, Wait: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if pipeOut.Process.TTY || !strings.Contains(pipeOut.Output, "MODE_PIPE") {
		t.Fatalf("expected pipe process output, got %+v output=%q", pipeOut.Process, pipeOut.Output)
	}
}

func TestWriteStdinToTTY(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root, filepath.Join(root, "state", "runtime"))
	p, err := m.Start(context.Background(), StartOptions{
		Command:   "read line; echo GOT_TTY:$line; sleep 1",
		OwnerKind: OwnerMainAgent,
		OwnerID:   "main",
		Lifecycle: LifecycleSession,
		TTY:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop(p.ID)
	if _, err := m.WriteStdin(p.ID, "hello\n"); err != nil {
		t.Fatalf("WriteStdin: %v", err)
	}
	offset := int64(0)
	deadline := time.Now().Add(2 * time.Second)
	var output strings.Builder
	for time.Now().Before(deadline) {
		snapshot, err := m.ReadOutputSnapshot(context.Background(), p.ID, OutputReadOptions{
			OffsetBytes: &offset,
			Wait:        200 * time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}
		output.WriteString(snapshot.Output)
		offset = snapshot.EndOffset
		if strings.Contains(output.String(), "GOT_TTY:hello") {
			return
		}
	}
	if !strings.Contains(output.String(), "GOT_TTY:hello") {
		t.Fatalf("unexpected tty stdin output: %q", output.String())
	}
}

func TestReadOutputSnapshotWaitsForNewOutputAfterOffset(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root, filepath.Join(root, "state", "runtime"))
	p, err := m.Start(context.Background(), StartOptions{Command: "sleep 0.2; printf ready; sleep 1", OwnerKind: OwnerMainAgent, OwnerID: "main", Lifecycle: LifecycleSession})
	if err != nil {
		t.Fatal(err)
	}
	offset := int64(0)
	snapshot, err := m.ReadOutputSnapshot(context.Background(), p.ID, OutputReadOptions{
		MaxBytes:    4096,
		OffsetBytes: &offset,
		Wait:        2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TimedOut {
		t.Fatalf("expected wait to receive output, got timeout: %+v", snapshot)
	}
	if snapshot.StartOffset != 0 || snapshot.EndOffset <= 0 || snapshot.TotalBytes != snapshot.EndOffset {
		t.Fatalf("unexpected offsets: %+v", snapshot)
	}
	if !strings.Contains(snapshot.Output, "ready") {
		t.Fatalf("unexpected output: %q", snapshot.Output)
	}
	_, _ = m.Stop(p.ID)
}

func TestReadOutputSnapshotTimesOutWhenNoNewOutput(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root, filepath.Join(root, "state", "runtime"))
	p, err := m.Start(context.Background(), StartOptions{Command: "sleep 1", OwnerKind: OwnerMainAgent, OwnerID: "main", Lifecycle: LifecycleSession})
	if err != nil {
		t.Fatal(err)
	}
	offset := int64(0)
	snapshot, err := m.ReadOutputSnapshot(context.Background(), p.ID, OutputReadOptions{
		MaxBytes:    4096,
		OffsetBytes: &offset,
		Wait:        100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.TimedOut {
		t.Fatalf("expected timeout waiting for new output: %+v", snapshot)
	}
	if snapshot.Output != "" || snapshot.StartOffset != 0 || snapshot.EndOffset != 0 {
		t.Fatalf("unexpected output after timeout: %+v", snapshot)
	}
	_, _ = m.Stop(p.ID)
}

func TestWriteStdin(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root, filepath.Join(root, "state", "runtime"))
	p, err := m.Start(context.Background(), StartOptions{Command: "read line; echo got:$line; sleep 1", OwnerKind: OwnerMainAgent, OwnerID: "main", Lifecycle: LifecycleSession})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.WriteStdin(p.ID, "hello\n"); err != nil {
		t.Fatalf("WriteStdin: %v", err)
	}
	deadline := time.After(2 * time.Second)
	for {
		out, _, err := m.ReadOutput(p.ID, 4096)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "got:hello") {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for stdin echo; output=%q", out)
		case <-time.After(50 * time.Millisecond):
		}
	}
	_, _ = m.Stop(p.ID)
}

func TestManagerPublishesLifecycleEventsAndCleanupSkipsManaged(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, filepath.Join(root, "state", "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan Event, 16)
	m.Subscribe(events)

	sessionProc, err := m.Start(context.Background(), StartOptions{Command: "sleep 5", OwnerKind: OwnerMainAgent, OwnerID: "main", Lifecycle: LifecycleSession})
	if err != nil {
		t.Fatal(err)
	}
	managedProc, err := m.Start(context.Background(), StartOptions{Command: "sleep 5", OwnerKind: OwnerMainAgent, OwnerID: "main", Lifecycle: LifecycleManaged})
	if err != nil {
		t.Fatal(err)
	}

	if got := (<-events); got.Type != EventStarted || got.Process.ID != sessionProc.ID {
		t.Fatalf("unexpected first event: %+v", got)
	}
	if got := (<-events); got.Type != EventStarted || got.Process.ID != managedProc.ID {
		t.Fatalf("unexpected second event: %+v", got)
	}

	result, err := m.CleanupSessionWithResult()
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Cleaned) != 1 || result.Cleaned[0].ID != sessionProc.ID {
		t.Fatalf("unexpected cleanup result: %+v", result)
	}

	gotCleanup := false
	gotStopped := false
	deadline := time.After(5 * time.Second)
	for !(gotCleanup && gotStopped) {
		select {
		case ev := <-events:
			switch {
			case ev.Type == EventCleanedUp && ev.Process.ID == sessionProc.ID:
				gotCleanup = true
			case ev.Type == EventStopped && ev.Process.ID == sessionProc.ID:
				gotStopped = true
			case ev.Process.ID == managedProc.ID && ev.Type == EventCleanedUp:
				t.Fatalf("managed process should not be cleaned up: %+v", ev)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for cleanup events; cleanup=%v stopped=%v", gotCleanup, gotStopped)
		}
	}

	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]Status{}
	for _, proc := range list {
		statuses[proc.ID] = proc.Status
	}
	if statuses[sessionProc.ID] != StatusStopped {
		t.Fatalf("expected session process stopped, got %s", statuses[sessionProc.ID])
	}
	if statuses[managedProc.ID] != StatusRunning {
		t.Fatalf("expected managed process still running, got %s", statuses[managedProc.ID])
	}
	_, _ = m.Stop(managedProc.ID)
}
