package process

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStopMigratesLiveLegacyProcessRecord(t *testing.T) {
	for _, tc := range []struct {
		name           string
		storedIdentity func(time.Time) string
	}{
		{name: "missing identity", storedIdentity: func(time.Time) string { return "" }},
		{name: "ps start time", storedIdentity: func(startedAt time.Time) string {
			return startedAt.Local().Format("Mon Jan _2 15:04:05 2006")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			m, err := NewManager(root, filepath.Join(root, "state", "runtime"))
			if err != nil {
				t.Fatal(err)
			}

			recordedAt := time.Now()
			cmd := managedCommand("sleep 30", root)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			pgid, err := syscall.Getpgid(cmd.Process.Pid)
			if err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			defer func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) }()
			currentIdentity, processStartedAt, _, err := readProcessIdentity(cmd.Process.Pid)
			if err != nil {
				t.Fatal(err)
			}

			record := &Process{
				ID:               "proc-legacy-live",
				Lifecycle:        LifecycleSession,
				Status:           StatusRunning,
				PID:              cmd.Process.Pid,
				PGID:             pgid,
				ProcessStartTime: tc.storedIdentity(processStartedAt),
				StartedAt:        recordedAt,
				UpdatedAt:        time.Now(),
				ExitCode:         -1,
			}
			if err := m.save(record); err != nil {
				t.Fatal(err)
			}

			got, err := m.Stop(record.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != StatusStopped || got.StoppedAt.IsZero() {
				t.Fatalf("legacy process was not stopped: %+v", got)
			}
			if got.ProcessStartTime != currentIdentity {
				t.Fatalf("migrated process identity = %q, want %q", got.ProcessStartTime, currentIdentity)
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("legacy process was not reaped")
			}
		})
	}
}

func TestStopReconcilesDeadLegacyProcessRecord(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, filepath.Join(root, "state", "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	record := &Process{
		ID:        "proc-legacy-dead",
		Lifecycle: LifecycleSession,
		Status:    StatusRunning,
		PID:       99999999,
		PGID:      99999999,
		StartedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now().Add(-time.Hour),
	}
	if err := m.save(record); err != nil {
		t.Fatal(err)
	}

	got, err := m.Stop(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusStopped || got.StoppedAt.IsZero() {
		t.Fatalf("dead legacy process was not reconciled: %+v", got)
	}
}

func TestStopRefusesLegacyRecordForDifferentProcess(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, filepath.Join(root, "state", "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := managedCommand("sleep 30", root)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}()

	record := &Process{
		ID:        "proc-legacy-reused",
		Lifecycle: LifecycleSession,
		Status:    StatusRunning,
		PID:       cmd.Process.Pid,
		PGID:      pgid,
		StartedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now(),
	}
	if err := m.save(record); err != nil {
		t.Fatal(err)
	}

	got, err := m.Stop(record.ID)
	if err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("expected legacy identity rejection, got process=%+v err=%v", got, err)
	}
	if got.Status != StatusRunning {
		t.Fatalf("rejected stop changed status to %s", got.Status)
	}
	process, err := os.FindProcess(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("identity rejection signaled the unrelated process: %v", err)
	}
}
