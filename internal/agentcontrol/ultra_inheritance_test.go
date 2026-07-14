package agentcontrol

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

func newUltraTestControl(t *testing.T) *AgentControl {
	t.Helper()
	dir := t.TempDir()
	initRepo(t, dir)
	c, err := New(Config{
		Client:        &fakeClient{resp: providers.ChatResponse{Content: "done"}},
		DefaultModel:  "fake-model",
		ParentRepo:    dir,
		WorktreeRoot:  filepath.Join(dir, ".wuu", "worktrees"),
		SessionID:     "sess-ultra",
		HistoryDir:    filepath.Join(dir, ".wuu-state", "sessions", "sess-ultra", "workers"),
		WorkerFactory: func(string, WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) { return fakeToolkit{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopAndCloseAgentControlForTest(t, c) })
	return c
}

func spawnUltraTestWorker(t *testing.T, c *AgentControl, task, parentID, parentPath string) *SpawnResult {
	t.Helper()
	res, err := c.Spawn(context.Background(), SpawnRequest{
		Type:        DefaultSubagentType,
		TaskName:    task,
		Description: "test",
		Prompt:      "do something",
		ParentID:    parentID,
		ParentPath:  parentPath,
		Synchronous: true,
	})
	if err != nil {
		t.Fatalf("Spawn %s: %v", task, err)
	}
	return res
}

func workerSystemMessage(t *testing.T, c *AgentControl, agentID string) string {
	t.Helper()
	sa := c.manager.Get(agentID)
	if sa == nil {
		t.Fatalf("worker %s not found in manager", agentID)
	}
	history := sa.HistorySnapshot()
	if len(history) == 0 || history[0].Role != "system" {
		t.Fatalf("worker %s history has no system message", agentID)
	}
	return history[0].Content
}

// The turn snapshot decides a root spawn's Ultra value, the parent's stored
// value decides a nested spawn's, and later session flips never touch an
// already-spawned subtree (docs/app-server-protocol.md, turn boundary and
// inheritance).
func TestSpawnInheritsUltraFromTurnAndParent(t *testing.T) {
	c := newUltraTestControl(t)

	c.SetTurnUltra(true)
	parent := spawnUltraTestWorker(t, c, "ultra_parent", "", "")
	if got := c.manager.Get(parent.AgentID).Snapshot(); !got.Ultra {
		t.Fatalf("root spawn under an Ultra turn must inherit Ultra, snapshot=%+v", got)
	}
	if sys := workerSystemMessage(t, c, parent.AgentID); !strings.Contains(sys, "You may spawn subagents") {
		t.Fatalf("Ultra worker system prompt missing the Ultra worker policy:\n%s", sys)
	}

	// Flipping the session mid-tree must not change the subtree: a child of
	// the Ultra parent stays Ultra even though the turn snapshot is now off.
	c.SetTurnUltra(false)
	child := spawnUltraTestWorker(t, c, "ultra_child", parent.AgentID, parent.AgentPath)
	if got := c.manager.Get(child.AgentID).Snapshot(); !got.Ultra {
		t.Fatalf("nested spawn must inherit the parent worker's Ultra value, snapshot=%+v", got)
	}

	// A fresh root spawn reads the new turn snapshot.
	plain := spawnUltraTestWorker(t, c, "plain_root", "", "")
	if got := c.manager.Get(plain.AgentID).Snapshot(); got.Ultra {
		t.Fatalf("root spawn under a non-Ultra turn must not inherit Ultra, snapshot=%+v", got)
	}
	if sys := workerSystemMessage(t, c, plain.AgentID); strings.Contains(sys, "You may spawn subagents") {
		t.Fatalf("non-Ultra worker system prompt must not contain the Ultra worker policy:\n%s", sys)
	}
}

// Ultra must survive the durable run snapshot so a resumed worker keeps its
// capability after a restart.
func TestPersistedRunKeepsUltra(t *testing.T) {
	c := newUltraTestControl(t)
	c.SetTurnUltra(true)
	parent := spawnUltraTestWorker(t, c, "ultra_persist", "", "")

	meta, ok := c.threads.Resolve(parent.AgentID)
	if !ok || !meta.Ultra {
		t.Fatalf("thread registry must record the inherited Ultra value, meta=%+v ok=%v", meta, ok)
	}
	persisted, err := subagent.LoadPersistedRun(filepath.Join(c.historyDir, parent.AgentID+".json"))
	if err != nil {
		t.Fatalf("LoadPersistedRun: %v", err)
	}
	if !persisted.Ultra {
		t.Fatalf("persisted run must keep Ultra, run=%+v", persisted)
	}
	if rehydrated := rehydratedThreadMeta(persisted); !rehydrated.Ultra {
		t.Fatalf("rehydrated thread metadata must keep Ultra, meta=%+v", rehydrated)
	}
}
