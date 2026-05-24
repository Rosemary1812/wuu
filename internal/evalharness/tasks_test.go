package evalharness

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogHasStableTaskIDs(t *testing.T) {
	tasks := Catalog()
	if len(tasks) < 4 {
		t.Fatalf("expected at least 4 tasks, got %d", len(tasks))
	}
	seen := map[string]bool{}
	for _, task := range tasks {
		if task.ID == "" {
			t.Fatal("task id must not be empty")
		}
		if seen[task.ID] {
			t.Fatalf("duplicate task id %q", task.ID)
		}
		seen[task.ID] = true
		if task.Prompt == "" || task.Setup == nil || task.Verify == nil {
			t.Fatalf("task %q is incomplete: %+v", task.ID, task)
		}
		if len(task.RequiredTools) == 0 {
			t.Fatalf("task %q should declare required tools", task.ID)
		}
	}
}

func TestTestFailureFixVerification(t *testing.T) {
	task, ok := ByID("test_failure_fix")
	if !ok {
		t.Fatal("missing test_failure_fix task")
	}
	root := t.TempDir()
	if err := SetupTask(task, root); err != nil {
		t.Fatalf("SetupTask: %v", err)
	}

	failed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask failed module: %v", err)
	}
	if failed.Passed {
		t.Fatal("buggy fixture should fail verification")
	}

	fixed := `package evaltask

func Add(a, b int) int {
	return a + b
}
`
	if err := os.WriteFile(filepath.Join(root, "calc.go"), []byte(fixed), 0o644); err != nil {
		t.Fatalf("write fixed file: %v", err)
	}
	passed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask fixed module: %v", err)
	}
	if !passed.Passed {
		t.Fatalf("fixed fixture should pass verification: %s", passed.Reason)
	}
}

func TestLongProcessOutputVerification(t *testing.T) {
	task, ok := ByID("long_process_output")
	if !ok {
		t.Fatal("missing long_process_output task")
	}
	root := t.TempDir()
	if err := SetupTask(task, root); err != nil {
		t.Fatalf("SetupTask: %v", err)
	}

	failed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask missing marker: %v", err)
	}
	if failed.Passed {
		t.Fatal("missing observed file should fail verification")
	}

	if err := os.WriteFile(filepath.Join(root, "observed.txt"), []byte("READY_FOR_EVAL\n"), 0o644); err != nil {
		t.Fatalf("write observed file: %v", err)
	}
	passed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask observed marker: %v", err)
	}
	if !passed.Passed {
		t.Fatalf("observed marker should pass verification: %s", passed.Reason)
	}
}

func TestToolSearchDeferredVerification(t *testing.T) {
	task, ok := ByID("tool_search_deferred")
	if !ok {
		t.Fatal("missing tool_search_deferred task")
	}
	root := t.TempDir()
	if err := SetupTask(task, root); err != nil {
		t.Fatalf("SetupTask: %v", err)
	}

	failed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask missing marker: %v", err)
	}
	if failed.Passed {
		t.Fatal("missing deferred marker should fail verification")
	}

	if err := os.WriteFile(filepath.Join(root, "tool_search_result.txt"), []byte("DEFERRED_TOOL_FOUND\n"), 0o644); err != nil {
		t.Fatalf("write marker file: %v", err)
	}
	passed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask deferred marker: %v", err)
	}
	if !passed.Passed {
		t.Fatalf("deferred marker should pass verification: %s", passed.Reason)
	}
}
