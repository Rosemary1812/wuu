//go:build windows

package process

import (
	"os"
	"strings"
	"testing"
)

func TestVerifyProcessGroupRejectsMismatchedRecords(t *testing.T) {
	if _, err := verifyProcessGroup(1234, 5678); err == nil || !strings.Contains(err.Error(), "refusing to signal") {
		t.Fatalf("want refusal for pgid != pid, got %v", err)
	}
	ok, err := verifyProcessGroup(1234, 1234)
	if err != nil || !ok {
		t.Fatalf("matching pgid rejected: ok=%t err=%v", ok, err)
	}
}

func TestLookupProcessGroupIsPid(t *testing.T) {
	if got := lookupProcessGroup(4321); got != 4321 {
		t.Fatalf("lookupProcessGroup = %d, want the pid back", got)
	}
}

func TestPTYUnsupported(t *testing.T) {
	if ptySupported() {
		t.Fatal("ptySupported must be false on windows")
	}
}

func TestTaskkillTreeIgnoresInvalidPids(t *testing.T) {
	for _, pid := range []int{0, 1, -5} {
		if err := taskkillTree(pid, true); err != nil {
			t.Fatalf("taskkillTree(%d) = %v, want nil", pid, err)
		}
	}
}

func TestProcessExistsSelf(t *testing.T) {
	if !processExists(os.Getpid()) {
		t.Fatal("current process reported dead")
	}
}
