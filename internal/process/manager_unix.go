//go:build !windows

package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// ptySupported reports whether this platform can run tty-mode processes.
func ptySupported() bool { return true }

// startPTYProcess starts cmd attached to a fresh pty in its own session,
// so the whole job is addressable as one process group.
func startPTYProcess(cmd *exec.Cmd) (*os.File, error) {
	return pty.StartWithAttrs(cmd, &pty.Winsize{Rows: 24, Cols: 80}, &syscall.SysProcAttr{Setsid: true, Setctty: true})
}

func resizePTY(file *os.File, cols, rows int) error {
	return pty.Setsize(file, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

// configureProcessGroup makes the child lead its own process group so
// group signals reach every descendant.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// lookupProcessGroup returns the child's process group, falling back to
// the pid itself when the group cannot be read.
func lookupProcessGroup(pid int) int {
	if pgid, err := syscall.Getpgid(pid); err == nil {
		return pgid
	}
	return pid
}

// terminateProcessGroup asks the whole group to exit. A group that is
// already gone is success, not an error.
func terminateProcessGroup(pgid int) error {
	return signalProcessGroup(pgid, syscall.SIGTERM)
}

// killProcessGroup force-kills the whole group. A group that is already
// gone is success, not an error.
func killProcessGroup(pgid int) error {
	return signalProcessGroup(pgid, syscall.SIGKILL)
}

func signalProcessGroup(pgid int, signal syscall.Signal) error {
	if err := syscall.Kill(-pgid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// verifyProcessGroup reports whether pid still leads the recorded group.
// A changed group means the pid was reused; it must never be signaled.
func verifyProcessGroup(pid, pgid int) (bool, error) {
	currentPGID, err := syscall.Getpgid(pid)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		return false, err
	}
	if currentPGID != pgid {
		return false, fmt.Errorf("group changed from %d to %d; refusing to signal it", pgid, currentPGID)
	}
	return true, nil
}

func processExists(pid int) bool {
	if pid <= 1 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
