package appserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
)

// writeTwoProviderSelectionConfig registers the workspace default provider
// (fake-provider, 400k window) plus a second provider (alt-provider, 100k
// window) so a conversation can pin a foreign provider/model whose derived
// budget and worker provider differ observably from the workspace default.
func writeTwoProviderSelectionConfig(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(`{
  "default_provider": "fake-provider",
  "providers": {
    "fake-provider": {
      "type": "openai-compatible",
      "base_url": "https://example.test/v1",
      "api_key": "test-key",
      "model": "fake-model",
      "context_window": 400000
    },
    "alt-provider": {
      "type": "openai-compatible",
      "base_url": "https://alt.test/v1",
      "api_key": "alt-key",
      "model": "alt-model",
      "context_window": 100000
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func residentForeignPinnedThread(t *testing.T, srv *Server, rt *runtime.Session, id string) *threadState {
	t.Helper()
	if _, err := session.CreateWithMetadata(rt.SessionDir, id, rt.RootDir); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := session.SetRuntimeSelection(rt.SessionDir, id, session.RuntimeSelection{
		Provider: "alt-provider",
		Model:    "alt-model",
	}); err != nil {
		t.Fatalf("pin alt provider: %v", err)
	}
	resume := []byte(`{"id":"resume","method":"thread/resume","params":{"session_id":"` + id + `"}}`)
	if err := srv.handleLine(context.Background(), resume); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}
	th := srv.thread(id)
	if th == nil {
		t.Fatal("resumed thread is not resident")
	}
	return th
}

type derivationFakeToolExecutor struct{}

func (derivationFakeToolExecutor) Definitions() []providers.ToolDefinition { return nil }
func (derivationFakeToolExecutor) Execute(context.Context, providers.ToolCall) (string, error) {
	return "", nil
}

// P3: a conversation pinned to a foreign provider builds its AgentControl with
// its own worker provider (NewThreadRuntimeForRoot). subscribeThreadRuntime must
// keep that value; it used to overwrite it with the workspace worker provider,
// which then let a pin naming the workspace provider route through this
// conversation's own connection.
func TestSubscribeThreadRuntimeKeepsForeignPinWorkerProvider(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{}) // workspace worker provider falls back to fake-provider
	srv := New(rt, &lockedBuffer{})

	dir := t.TempDir()
	control, err := agentcontrol.New(agentcontrol.Config{
		Client:       providers.AdaptStreamClient(&fakeClient{}),
		ProviderName: "alt-provider", // the conversation's own worker provider, set at construction
		DefaultModel: "alt-worker-model",
		ParentRepo:   dir,
		WorktreeRoot: filepath.Join(dir, "wt"),
		SessionID:    "foreign",
		HistoryDir:   filepath.Join(dir, "history"),
		ThreadDir:    filepath.Join(dir, "threads"),
		HarnessDir:   filepath.Join(dir, "harness"),
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return derivationFakeToolExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("agentcontrol.New: %v", err)
	}
	t.Cleanup(func() { control.StopAll(); control.Close() })
	if control.WorkerProviderName() != "alt-provider" {
		t.Fatalf("precondition: constructed worker provider = %q, want alt-provider", control.WorkerProviderName())
	}

	sub := srv.subscribeThreadRuntime("foreign-thread", &runtime.ThreadRuntime{AgentControl: control})
	if sub != nil {
		t.Cleanup(sub.stop)
	}

	if got := control.WorkerProviderName(); got != "alt-provider" {
		t.Fatalf("subscribeThreadRuntime clobbered the conversation's worker provider: got %q want alt-provider", got)
	}
}

// P1: an advanced-settings change propagates behavior knobs to idle runtimes,
// but the derived budget and worker defaults must be recomputed from each
// conversation's own pin — never copied from the workspace runtime.
func TestAdvancedUpdateReDerivesPinnedThreadBudgetAndWorker(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	writeTwoProviderSelectionConfig(t, rt.ConfigPath)
	srv := New(rt, &lockedBuffer{})

	th := residentForeignPinnedThread(t, srv, rt, "foreign-thread")
	built, err := srv.ensureThreadRuntime(th)
	if err != nil {
		t.Fatalf("ensureThreadRuntime: %v", err)
	}
	// The pinned thread derived its own 100k-window budget and worker-model
	// budget, both distinct from the workspace default's 400k window.
	pinnedWindow := built.StreamRunner.ContextWindowOverride
	pinnedWorkerWindow := built.WorkerModelBudget.ContextWindowTokens
	if pinnedWindow <= 0 || pinnedWindow >= s400k {
		t.Fatalf("pinned thread window = %d, want the alt-provider's smaller window", pinnedWindow)
	}

	// Simulate the workspace side of an advanced-settings change: behavior
	// knobs move on the workspace runner; the workspace budget is the 400k
	// default that must NOT bleed into the pinned thread.
	srv.rt.StreamRunner.Temperature = 0.42
	srv.rt.StreamRunner.MaxSteps = 99
	cfg, _, err := srv.rt.LoadEffectiveConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	srv.updateIdleThreadAdvancedRuntime(cfg)

	// Derived budgets stay the conversation's own, not the workspace 400k.
	if built.StreamRunner.ContextWindowOverride != pinnedWindow {
		t.Fatalf("advanced update clobbered pinned window: got %d want %d", built.StreamRunner.ContextWindowOverride, pinnedWindow)
	}
	if built.ModelBudget.ContextWindowTokens != pinnedWindow {
		t.Fatalf("advanced update clobbered ModelBudget: got %d want %d", built.ModelBudget.ContextWindowTokens, pinnedWindow)
	}
	if built.WorkerModelBudget.ContextWindowTokens != pinnedWorkerWindow || built.WorkerModelBudget.ContextWindowTokens >= s400k {
		t.Fatalf("advanced update clobbered WorkerModelBudget: got %d want %d", built.WorkerModelBudget.ContextWindowTokens, pinnedWorkerWindow)
	}
	// Behavior knobs did propagate.
	if built.StreamRunner.Temperature != 0.42 || built.StreamRunner.MaxSteps != 99 {
		t.Fatalf("behavior knobs not propagated: temp=%v steps=%d", built.StreamRunner.Temperature, built.StreamRunner.MaxSteps)
	}
}

const s400k = 400000

// Resume must restore a valid pinned selection's variant/effort/permission from
// disk into the rebuilt runtime — not just provider/model. The existing tests
// only cover the removed-provider heal path.
func TestEnsureThreadRuntimeRestoresPinnedVariantEffortPermission(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	writeSelectionTestConfig(t, rt.ConfigPath)
	srv := New(rt, &lockedBuffer{})

	if _, err := session.CreateWithMetadata(rt.SessionDir, "resume-thread", rt.RootDir); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := session.SetRuntimeSelection(rt.SessionDir, "resume-thread", session.RuntimeSelection{
		Provider:       "fake-provider",
		Model:          "fake-model",
		Variant:        "high",
		Effort:         "high",
		PermissionMode: config.PermissionModeReadOnly,
	}); err != nil {
		t.Fatalf("pin selection: %v", err)
	}
	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/resume","params":{"session_id":"resume-thread"}}`)); err != nil {
		t.Fatalf("thread/resume: %v", err)
	}
	th := srv.thread("resume-thread")
	if th == nil {
		t.Fatal("resumed thread is not resident")
	}
	// The pins are restored onto the thread state...
	th.mu.Lock()
	variant, effort, permission := th.ModelVariant, th.ModelEffort, th.PermissionMode
	th.mu.Unlock()
	if variant != "high" || effort != "high" || permission != config.PermissionModeReadOnly {
		t.Fatalf("resume did not restore pins: variant=%q effort=%q permission=%q", variant, effort, permission)
	}
	// ...and the rebuilt runtime is stamped with the restored pin (the raw
	// variant/effort/permission the conversation was resumed with).
	built, err := srv.ensureThreadRuntime(th)
	if err != nil {
		t.Fatalf("ensureThreadRuntime: %v", err)
	}
	if built.Selection.Variant != "high" || built.Selection.Effort != "high" {
		t.Fatalf("runtime selection stamp lost pins: %+v", built.Selection)
	}
	if built.Selection.PermissionMode != config.PermissionModeReadOnly {
		t.Fatalf("runtime selection stamp lost permission: %q", built.Selection.PermissionMode)
	}
}

// Fork must inherit the source conversation's full selection, not the workspace
// default.
func TestServerThreadForkInheritsSourceSelection(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{{Content: "answer"}}}
	rt := newTestRuntime(t, client)
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	sourceID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	// Run a turn so the source has a forkable assistant item.
	turnPayload, _ := json.Marshal(map[string]any{
		"id": "2", "method": MethodTurnStart,
		"params": TurnStartParams{ThreadID: sourceID, Prompt: "first prompt"},
	})
	if err := srv.handleLine(context.Background(), turnPayload); err != nil {
		t.Fatalf("turn/start: %v", err)
	}
	completed := notificationsByMethod(waitForNotificationCount(t, out, NotificationTurnCompleted, 1), NotificationTurnCompleted)
	firstTurn := remarshal[TurnCompletedNotification](t, completed[len(completed)-1]["params"]).Turn
	var agentItem ThreadItem
	for _, item := range firstTurn.Items {
		if item.Type == ThreadItemAgentMessage {
			agentItem = item
			break
		}
	}
	if agentItem.ID == "" {
		t.Fatalf("no assistant item to fork at: %+v", firstTurn)
	}

	// Pin the source conversation to a distinct selection (mimicking a session
	// the user tuned away from the workspace default).
	source := srv.thread(sourceID)
	source.mu.Lock()
	source.ModelVariant = "high"
	source.ModelEffort = "medium"
	source.PermissionMode = config.PermissionModeUnconfined
	source.mu.Unlock()

	forkPayload, _ := json.Marshal(map[string]any{
		"id": "3", "method": MethodThreadFork,
		"params": ThreadForkParams{ThreadID: sourceID, TurnID: firstTurn.ID, ItemID: agentItem.ID},
	})
	if err := srv.handleLine(context.Background(), forkPayload); err != nil {
		t.Fatalf("thread/fork: %v", err)
	}
	fork := remarshal[ThreadForkResult](t, responseByID(t, parseOutput(t, out.String()), "3")["result"]).Thread
	if fork.ModelVariant != "high" || fork.ModelEffort != "medium" || fork.PermissionMode != config.PermissionModeUnconfined {
		t.Fatalf("fork did not inherit source selection: variant=%q effort=%q permission=%q",
			fork.ModelVariant, fork.ModelEffort, fork.PermissionMode)
	}
	// The persisted session row carries the inherited pins too.
	sess, ok, err := session.Find(rt.SessionDir, fork.ID)
	if err != nil || !ok {
		t.Fatalf("find fork session: ok=%v err=%v", ok, err)
	}
	if sess.Variant != "high" || sess.Effort != "medium" || sess.PermissionMode != config.PermissionModeUnconfined {
		t.Fatalf("fork session row lost inherited pins: variant=%q effort=%q permission=%q",
			sess.Variant, sess.Effort, sess.PermissionMode)
	}
}
