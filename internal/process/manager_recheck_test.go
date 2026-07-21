package process

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func waitForProcessStatus(t *testing.T, m *Manager, id string, want Status, timeout time.Duration) Process {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		p, err := m.Get(id)
		if err == nil && p.Status == want {
			return *p
		}
		if time.Now().After(deadline) {
			current, _ := m.Get(id)
			t.Fatalf("process %q did not reach status %q within %s (current: %+v)", id, want, timeout, current)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestReadOutputSnapshotMinDwellPacesOutputReturn(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Start(context.Background(), StartOptions{Command: "printf 'tick\\n'; sleep 30", OwnerKind: OwnerMainAgent, OwnerID: "main"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = m.Stop(p.ID) }()

	zero := int64(0)
	paced, err := m.ReadOutputSnapshot(context.Background(), p.ID, OutputReadOptions{
		MaxBytes:    4096,
		OffsetBytes: &zero,
		Wait:        3 * time.Second,
		MinDwell:    time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(paced.Output, "tick") {
		t.Fatalf("paced wait should return the new output: %+v", paced)
	}
	if paced.Duration < 900*time.Millisecond {
		t.Fatalf("min dwell should pace the output-driven return, returned after %s", paced.Duration)
	}
	if paced.TimedOut {
		t.Fatal("output arrived before the deadline; the wait should not time out")
	}

	// Without a minimum dwell the same read releases as soon as output is
	// visible.
	unpaced, err := m.ReadOutputSnapshot(context.Background(), p.ID, OutputReadOptions{
		MaxBytes:    4096,
		OffsetBytes: &zero,
		Wait:        3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if unpaced.Duration > 900*time.Millisecond {
		t.Fatalf("zero min dwell should return on first output, took %s", unpaced.Duration)
	}
}

func TestReadOutputSnapshotExitReleasesImmediatelyWithMinDwell(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Start(context.Background(), StartOptions{Command: "exit 0", OwnerKind: OwnerMainAgent, OwnerID: "main"})
	if err != nil {
		t.Fatal(err)
	}
	waitForProcessStatus(t, m, p.ID, StatusStopped, 5*time.Second)

	zero := int64(0)
	snapshot, err := m.ReadOutputSnapshot(context.Background(), p.ID, OutputReadOptions{
		MaxBytes:    4096,
		OffsetBytes: &zero,
		Wait:        5 * time.Second,
		MinDwell:    30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Duration > time.Second {
		t.Fatalf("process exit must release the wait immediately regardless of min dwell, took %s", snapshot.Duration)
	}
}

func TestSetRecheckValidation(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Start(context.Background(), StartOptions{Command: "sleep 30", OwnerKind: OwnerMainAgent, OwnerID: "main"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = m.Stop(p.ID) }()

	if _, err := m.SetRecheck(p.ID, -1); err == nil {
		t.Fatal("negative recheck_minutes should fail")
	}
	if _, err := m.SetRecheck(p.ID, MaxRecheckMinutes+1); err == nil {
		t.Fatal("recheck_minutes above the cap should fail")
	}
	updated, err := m.SetRecheck(p.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if updated.RecheckMinutes != 5 || updated.NextRecheckAt.IsZero() {
		t.Fatalf("recheck schedule not applied: %+v", updated)
	}
	cancelled, err := m.SetRecheck(p.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.RecheckMinutes != 0 || !cancelled.NextRecheckAt.IsZero() {
		t.Fatalf("recheck cancellation not applied: %+v", cancelled)
	}

	stopped, err := m.Start(context.Background(), StartOptions{Command: "exit 0", OwnerKind: OwnerMainAgent, OwnerID: "main"})
	if err != nil {
		t.Fatal(err)
	}
	waitForProcessStatus(t, m, stopped.ID, StatusStopped, 5*time.Second)
	if _, err := m.SetRecheck(stopped.ID, 5); err == nil {
		t.Fatal("setting a recheck on a terminal process should fail")
	}
}

func TestRecheckSchedulerFiresAdvancesAndDelivers(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Start(context.Background(), StartOptions{Command: "sleep 60", OwnerKind: OwnerMainAgent, OwnerID: "main", Lifecycle: LifecycleManaged, RecheckMinutes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = m.Stop(p.ID) }()

	// Pull the deadline into the past so the next scheduler pass fires.
	m.mu.Lock()
	rec, err := m.load(p.ID)
	if err != nil {
		m.mu.Unlock()
		t.Fatal(err)
	}
	rec.NextRecheckAt = time.Now().Add(-time.Minute)
	if err := m.save(rec); err != nil {
		m.mu.Unlock()
		t.Fatal(err)
	}
	m.mu.Unlock()
	m.signalRecheckScheduler()

	deadline := time.Now().Add(5 * time.Second)
	var pending []Process
	for {
		pending, err = m.PendingRechecks()
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scheduler did not fire the due recheck within 5s (pending: %+v)", pending)
		}
		time.Sleep(20 * time.Millisecond)
	}
	fired := pending[0]
	if fired.ID != p.ID || fired.PendingRecheckAt.IsZero() {
		t.Fatalf("unexpected pending recheck: %+v", fired)
	}
	if !fired.NextRecheckAt.After(time.Now()) {
		t.Fatalf("firing should arm the next interval in the future: %+v", fired)
	}

	if _, err := m.MarkRecheckDelivered(p.ID); err != nil {
		t.Fatal(err)
	}
	pending, err = m.PendingRechecks()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("delivery should clear the pending obligation: %+v", pending)
	}
}

func TestScanRechecksClearsTerminalSchedule(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Start(context.Background(), StartOptions{Command: "sleep 60", OwnerKind: OwnerMainAgent, OwnerID: "main", RecheckMinutes: 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Stop(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, due := m.scanRechecks(); len(due) != 0 {
		t.Fatalf("terminal process should never be due: %+v", due)
	}
	current, err := m.Get(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.RecheckMinutes != 0 || !current.NextRecheckAt.IsZero() || !current.PendingRecheckAt.IsZero() {
		t.Fatalf("terminal process should lose its recheck schedule: %+v", current)
	}
}

func TestAdoptTracksNaturalExitAndCompletion(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	id, logPath := m.ReserveProcessLog()
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := managedCommand("printf 'adopted-output\\n'; exit 0", root, "")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = logf
	cmd.Stderr = logf
	handle, err := StartCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Adopt(id, cmd, handle, logf, AdoptOptions{Command: "printf ...; exit 0", CWD: root, OwnerKind: OwnerMainAgent, OwnerID: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusRunning || p.PGID <= 1 || p.ProcessStartTime == "" {
		t.Fatalf("adopted process missing identity: %+v", p)
	}

	terminal := waitForProcessStatus(t, m, p.ID, StatusStopped, 5*time.Second)
	if terminal.ExitCode != 0 || terminal.TerminalCause != EventCauseNaturalExit {
		t.Fatalf("unexpected terminal state: %+v", terminal)
	}
	pending, err := m.PendingCompletions()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range pending {
		if candidate.ID == p.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("adopted process should carry a completion obligation: %+v", pending)
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "adopted-output") {
		t.Fatalf("adopted log should keep the process output: %q", content)
	}
}

func TestAdoptStopTerminatesProcess(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	id, logPath := m.ReserveProcessLog()
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := managedCommand("sleep 60", root, "")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = logf
	cmd.Stderr = logf
	handle, err := StartCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Adopt(id, cmd, handle, logf, AdoptOptions{Command: "sleep 60", CWD: root, OwnerKind: OwnerMainAgent, OwnerID: "main"})
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := m.Stop(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != StatusStopped {
		t.Fatalf("stop should terminate the adopted process: %+v", stopped)
	}
}
