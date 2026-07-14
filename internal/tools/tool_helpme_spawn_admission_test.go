package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/subagent"
)

func TestHelpMeRecoveryPrepareFailureDoesNotCreateWorkerOrCompletion(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "session")
	control := newHelpMeTestControl(t, dir, sessionDir)
	harnessDir := filepath.Join(sessionDir, "harness")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatalf("create harness dir: %v", err)
	}
	// Block the directory that atomic recovery persistence must create while
	// leaving the thread and queue stores writable.
	if err := os.WriteFile(filepath.Join(harnessDir, "helpme-recovery"), []byte("blocked"), 0o600); err != nil {
		t.Fatalf("block recovery dir: %v", err)
	}

	notifications := make(chan subagent.Notification, 4)
	control.Subscribe(notifications)
	defer control.Unsubscribe(notifications)
	tool := NewHelpMeTool(&Env{AgentControl: control, SessionDir: sessionDir})
	result, err := tool.Execute(context.Background(), `{"reason":"test persistence failure","ask":"do not start a helper"}`)
	if err == nil || !strings.Contains(err.Error(), "persist helpme recovery") {
		t.Fatalf("Execute error = %v, want recovery persistence failure", err)
	}
	if result != "" {
		t.Fatalf("Execute result = %q, want empty", result)
	}
	if workers := control.Manager().List(); len(workers) != 0 {
		t.Fatalf("recovery prepare failure created workers: %+v", workers)
	}
	if threads := control.Threads().List(); len(threads) != 1 {
		t.Fatalf("recovery prepare failure created child threads: %+v", threads)
	}
	if tasks, listErr := control.HarnessStore().ListTasks(); listErr != nil || len(tasks) != 0 {
		t.Fatalf("recovery prepare failure harness tasks = %+v, err=%v", tasks, listErr)
	}
	if queue, listErr := control.HarnessStore().ListQueueItems(); listErr != nil || len(queue) != 0 {
		t.Fatalf("recovery prepare failure queue = %+v, err=%v", queue, listErr)
	}
	if pending, pendingErr := control.PendingRootAgentCompletions(); pendingErr != nil || len(pending) != 0 {
		t.Fatalf("recovery prepare failure completions = %+v, err=%v", pending, pendingErr)
	}
	select {
	case notification := <-notifications:
		t.Fatalf("recovery prepare failure emitted worker notification: %+v", notification)
	case <-time.After(100 * time.Millisecond):
	}
}
