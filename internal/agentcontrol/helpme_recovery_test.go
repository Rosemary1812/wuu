package agentcontrol

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentthread"
)

func newHelpMeRecoveryControl(t *testing.T, dir, harnessDir string) *AgentControl {
	t.Helper()
	c, err := New(Config{
		Client:        &fakeClient{},
		DefaultModel:  "fake-model",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, "wt"),
		SessionID:     "sess-helpme-recovery",
		HarnessDir:    harnessDir,
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestHelpMeRecoveryRegisterQueryAndOneShotApplied(t *testing.T) {
	// No harness dir: the recovery must still work purely in memory.
	c := newHelpMeRecoveryControl(t, t.TempDir(), "")

	if _, ok := c.HelpMeRecoveryForHelper("helper-unknown"); ok {
		t.Fatal("unknown helper must not resolve a recovery")
	}
	if applied, err := c.MarkHelpMeRecoveryApplied("helper-unknown"); err != nil || applied {
		t.Fatal("marking an unknown helper applied must return false")
	}

	if err := c.RegisterHelpMeRecovery(HelpMeRecovery{
		HelperID:   "helper-1",
		ParentPath: "/root",
		Brief: HelpMeRecoveryBrief{
			OriginalGoal:   "fix the login test",
			Ask:            "find the real cause",
			Reason:         "two failed auth attempts",
			FailedAttempts: []string{"changed the router guard"},
		},
	}); err != nil {
		t.Fatalf("register recovery: %v", err)
	}

	rec, ok := c.HelpMeRecoveryForHelper("helper-1")
	if !ok {
		t.Fatal("registered recovery not found")
	}
	if rec.Applied {
		t.Fatal("fresh recovery must not be applied")
	}
	if rec.Brief.OriginalGoal != "fix the login test" || rec.Brief.Ask != "find the real cause" || rec.ParentPath != "/root" {
		t.Fatalf("recovery lost registered state: %+v", rec)
	}
	if rec.CreatedAt.IsZero() {
		t.Fatal("register must stamp CreatedAt")
	}

	if applied, err := c.MarkHelpMeRecoveryApplied("helper-1"); err != nil || !applied {
		t.Fatal("first apply must succeed")
	}
	if applied, err := c.MarkHelpMeRecoveryApplied("helper-1"); err != nil || applied {
		t.Fatal("second apply must fail: recovery is one-shot")
	}
	rec, ok = c.HelpMeRecoveryForHelper("helper-1")
	if !ok || !rec.Applied {
		t.Fatalf("applied recovery must remain queryable as applied, got %+v ok=%v", rec, ok)
	}
}

func TestHelpMeRecoveryLazyLoadsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	harnessDir := filepath.Join(dir, "session", "harness")

	first := newHelpMeRecoveryControl(t, dir, harnessDir)
	if err := first.RegisterHelpMeRecovery(HelpMeRecovery{
		HelperID:               "helper-restart",
		ParentExecutionJournal: "### Paths taken\n- guard patch - ruled out",
		Brief:                  HelpMeRecoveryBrief{OriginalGoal: "survive the restart", Ask: "keep the resolved goal"},
	}); err != nil {
		t.Fatalf("register recovery: %v", err)
	}
	path := filepath.Join(harnessDir, helpMeRecoveryDirName, "helper-restart.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("recovery snapshot not persisted: %v", err)
	}

	// A fresh instance on the same session directory simulates a process
	// restart; the recovery must rehydrate lazily on first query.
	second := newHelpMeRecoveryControl(t, dir, harnessDir)
	rec, ok := second.HelpMeRecoveryForHelper("helper-restart")
	if !ok {
		t.Fatal("recovery must lazy-load from disk after restart")
	}
	if rec.Brief.OriginalGoal != "survive the restart" || rec.Applied {
		t.Fatalf("rehydrated recovery lost state: %+v", rec)
	}
	if rec.ParentExecutionJournal != "### Paths taken\n- guard patch - ruled out" {
		t.Fatalf("rehydrated recovery lost the parent journal: %+v", rec)
	}
	if applied, err := second.MarkHelpMeRecoveryApplied("helper-restart"); err != nil || !applied {
		t.Fatal("first apply after restart must succeed")
	}

	// The applied flag is persisted too: another restart still refuses a
	// second application.
	third := newHelpMeRecoveryControl(t, dir, harnessDir)
	if applied, err := third.MarkHelpMeRecoveryApplied("helper-restart"); err != nil || applied {
		t.Fatal("recovery must stay one-shot across restarts")
	}
	rec, ok = third.HelpMeRecoveryForHelper("helper-restart")
	if !ok || !rec.Applied {
		t.Fatalf("expected applied recovery after restart, got %+v ok=%v", rec, ok)
	}
}
