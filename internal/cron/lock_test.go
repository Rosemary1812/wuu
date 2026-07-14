package cron

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLock_MutualExclusionAndRelease(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "sched.lock")

	lock := NewLock(lockPath)
	t.Cleanup(lock.Release)
	acquired, err := lock.TryAcquire()
	if err != nil {
		t.Fatalf("TryAcquire error: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock")
	}

	acquired2, err := lock.TryAcquire()
	if err != nil {
		t.Fatalf("idempotent TryAcquire error: %v", err)
	}
	if !acquired2 {
		t.Fatal("expected idempotent re-acquire to succeed")
	}

	lock2 := NewLock(lockPath)
	t.Cleanup(lock2.Release)
	acquired3, err := lock2.TryAcquire()
	if err != nil {
		t.Fatalf("second session TryAcquire error: %v", err)
	}
	if acquired3 {
		t.Fatal("expected second session to fail acquiring lock")
	}

	lock.Release()

	acquired4, err := lock2.TryAcquire()
	if err != nil {
		t.Fatalf("post-release TryAcquire error: %v", err)
	}
	if !acquired4 {
		t.Fatal("expected second session to acquire after release")
	}
}

func TestLock_ReleasedWhenProcessExits(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "sched.lock")

	cmd := exec.Command(os.Args[0], "-test.run=^TestLockProcessHelper$")
	cmd.Env = append(os.Environ(), "WUU_CRON_LOCK_HELPER=1", "WUU_CRON_LOCK_PATH="+lockPath)
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

	contender := NewLock(lockPath)
	acquired, err := contender.TryAcquire()
	if err != nil {
		t.Fatalf("TryAcquire while helper is alive: %v", err)
	}
	if acquired {
		contender.Release()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("expected helper process to retain lock while alive")
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected killed helper to exit unsuccessfully")
	}

	lock := NewLock(lockPath)
	t.Cleanup(lock.Release)
	acquired, err = lock.TryAcquire()
	if err != nil {
		t.Fatalf("TryAcquire after helper exit: %v", err)
	}
	if !acquired {
		t.Fatal("expected lock to be released when helper process exited")
	}
}

func TestLockProcessHelper(t *testing.T) {
	if os.Getenv("WUU_CRON_LOCK_HELPER") != "1" {
		return
	}

	lock := NewLock(os.Getenv("WUU_CRON_LOCK_PATH"))
	acquired, err := lock.TryAcquire()
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if !acquired {
		t.Fatal("expected helper to acquire lock")
	}
	fmt.Println("acquired")
	for {
		time.Sleep(time.Hour)
	}
}
