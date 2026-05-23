package agentthread

import (
	"strings"
	"testing"
	"time"
)

func TestRegistryRegistersRootAndChildPath(t *testing.T) {
	reg := NewRegistry()
	now := time.Date(2026, 5, 23, 1, 2, 3, 0, time.UTC)
	root := reg.RegisterRoot("root-thread", "sess-1", "/repo", "gpt-5", now)
	if root.Path != RootPath || root.Source.Kind != SourceRoot {
		t.Fatalf("unexpected root metadata: %+v", root)
	}

	child, err := reg.RegisterSpawn(SpawnSpec{
		ID:              "worker-1",
		SessionID:       "sess-1",
		ParentPath:      RootPath,
		TaskName:        "Fix Login Bug",
		Role:            "worker",
		LastTaskMessage: "fix it",
		Status:          StatusRunning,
		Now:             now,
	})
	if err != nil {
		t.Fatalf("RegisterSpawn: %v", err)
	}
	if child.Path != "/root/fix-login-bug" {
		t.Fatalf("unexpected child path %q", child.Path)
	}
	if child.ParentID != "root-thread" || child.Source.ParentThreadID != "root-thread" {
		t.Fatalf("child did not link to root: %+v", child)
	}
	if got, ok := reg.Resolve("Fix Login Bug"); !ok || got.ID != "worker-1" {
		t.Fatalf("resolve by task name failed: %+v ok=%v", got, ok)
	}
	if got, ok := reg.Resolve("/root/fix-login-bug"); !ok || got.ID != "worker-1" {
		t.Fatalf("resolve by path failed: %+v ok=%v", got, ok)
	}
}

func TestRegistryRejectsDuplicateTaskPath(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterRoot("root-thread", "sess-1", "", "", time.Now())
	if _, err := reg.RegisterSpawn(SpawnSpec{ID: "a", TaskName: "same task"}); err != nil {
		t.Fatalf("first spawn should succeed: %v", err)
	}
	if _, err := reg.RegisterSpawn(SpawnSpec{ID: "b", TaskName: "same_task"}); err == nil {
		t.Fatal("expected duplicate path error")
	}
}

func TestRegistryGeneratesTaskNames(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterRoot("root-thread", "sess-1", "", "", time.Now())
	first, err := reg.RegisterSpawn(SpawnSpec{ID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := reg.RegisterSpawn(SpawnSpec{ID: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if first.TaskName != "task-1" || second.TaskName != "task-2" {
		t.Fatalf("unexpected generated names: %q %q", first.TaskName, second.TaskName)
	}
	if !strings.HasSuffix(second.Path, "/task-2") {
		t.Fatalf("unexpected path: %s", second.Path)
	}
}
