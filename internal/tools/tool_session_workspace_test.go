package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetSessionWorkspaceUpdatesSubsequentToolRoot(t *testing.T) {
	root := t.TempDir()
	oldRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "after.txt"), []byte("linked workspace\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var reboundRoot string
	env := &Env{
		RootDir:        oldRoot,
		FileScopeRoots: []string{oldRoot},
		OnSessionWorkspaceChanged: func(got string) error {
			reboundRoot = got
			return nil
		},
	}

	result, err := NewSetSessionWorkspaceTool(env).Execute(
		context.Background(),
		`{"root":`+quoteJSONForTest(root)+`}`,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := filepath.Clean(root)
	if reboundRoot != want {
		t.Fatalf("callback root = %q, want %q", reboundRoot, want)
	}
	if env.RootDir != want {
		t.Fatalf("subsequent tool root = %q, want %q", env.RootDir, want)
	}
	if len(env.FileScopeRoots) != 1 || env.FileScopeRoots[0] != want {
		t.Fatalf("file scope roots = %q, want [%q]", env.FileScopeRoots, want)
	}
	readResult, err := NewReadFileTool(env).Execute(context.Background(), `{"path":"after.txt"}`)
	if err != nil {
		t.Fatalf("read_file after workspace change: %v", err)
	}
	if !strings.Contains(readResult, "linked workspace") {
		t.Fatalf("read_file result = %s", readResult)
	}
	if result != `{"root":`+quoteJSONForTest(want)+`}` {
		t.Fatalf("result = %s", result)
	}
}

func TestSetSessionWorkspaceDoesNotMoveRootWhenPersistenceFails(t *testing.T) {
	oldRoot := t.TempDir()
	env := &Env{
		RootDir:        oldRoot,
		FileScopeRoots: []string{oldRoot},
		OnSessionWorkspaceChanged: func(string) error {
			return errors.New("rejected")
		},
	}

	_, err := NewSetSessionWorkspaceTool(env).Execute(
		context.Background(),
		`{"root":`+quoteJSONForTest(t.TempDir())+`}`,
	)
	if err == nil {
		t.Fatal("Execute unexpectedly succeeded")
	}
	if env.RootDir != oldRoot {
		t.Fatalf("tool root changed to %q after failure", env.RootDir)
	}
	if len(env.FileScopeRoots) != 1 || env.FileScopeRoots[0] != oldRoot {
		t.Fatalf("file scope changed after failure: %q", env.FileScopeRoots)
	}
}

func TestCloneForRootPreservesSessionWorkspaceCallback(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var got string
	kit.SetOnSessionWorkspaceChanged(func(root string) error {
		got = root
		return nil
	})
	clone, err := kit.CloneForRoot(t.TempDir())
	if err != nil {
		t.Fatalf("CloneForRoot: %v", err)
	}
	if clone.env.OnSessionWorkspaceChanged == nil {
		t.Fatal("clone dropped session workspace callback")
	}
	if err := clone.env.OnSessionWorkspaceChanged("linked"); err != nil {
		t.Fatal(err)
	}
	if got != "linked" {
		t.Fatalf("callback root = %q, want linked", got)
	}
}

func quoteJSONForTest(value string) string {
	quoted := "\""
	for _, r := range value {
		switch r {
		case '\\', '"':
			quoted += "\\" + string(r)
		default:
			quoted += string(r)
		}
	}
	return quoted + "\""
}
