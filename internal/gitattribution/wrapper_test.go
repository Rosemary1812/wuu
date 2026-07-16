package gitattribution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRealGitRejectsWrapperItself(t *testing.T) {
	binDir := t.TempDir()
	wrapperPath := filepath.Join(binDir, "git")
	if err := os.WriteFile(wrapperPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := validateRealGit(wrapperPath, wrapperPath); err == nil || !strings.Contains(err.Error(), "wrapper itself") {
		t.Fatalf("validateRealGit() error = %v, want self-wrapper rejection", err)
	}
}

func TestValidateRealGitAcceptsDifferentGitExecutable(t *testing.T) {
	root := t.TempDir()
	wrapperPath := filepath.Join(root, "wrapper", "git")
	realGit := filepath.Join(root, "real", "git")
	for _, path := range []string{wrapperPath, realGit} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	if err := validateRealGit(wrapperPath, realGit); err != nil {
		t.Fatalf("validateRealGit() error = %v", err)
	}
}

func TestWrapperScriptPassesItsPathToDispatcher(t *testing.T) {
	script := wrapperScript("/opt/wuu", "/state/git-wrapper/bin/git")
	if !strings.Contains(script, "__wuu_internal_git_wrapper '/state/git-wrapper/bin/git' \"$real_git\"") {
		t.Fatalf("wrapper script does not pass its own path:\n%s", script)
	}
}
