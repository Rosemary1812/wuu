package appserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
)

// A user turn admitted under Ultra injects the root delegation policy as
// request-only TOOL_POLICY context and publishes the snapshot to the thread's
// orchestration control; the next turn re-reads the session setting
// (docs/app-server-protocol.md, Ultra Mode Configuration).
func TestUltraTurnInjectsRootPolicyAndSnapshotsPerTurn(t *testing.T) {
	client := &fakeClient{
		responses: []providers.ChatResponse{
			{Content: "ultra turn done"},
			{Content: "plain turn done"},
		},
	}
	rt := newTestRuntime(t, client)
	// The bare test session builds thread runtimes without their own
	// orchestration control; install a session-level one so the turn's
	// snapshot publication is observable.
	control, err := agentcontrol.New(agentcontrol.Config{
		Client:       providers.AdaptStreamClient(&fakeClient{}),
		DefaultModel: "fake-model",
		ParentRepo:   rt.RootDir,
		WorktreeRoot: filepath.Join(rt.RootDir, ".wuu", "worktrees"),
		SessionID:    "ultra-turn-session",
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return noopToolExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("agentcontrol.New: %v", err)
	}
	t.Cleanup(func() {
		control.StopAll()
		control.Close()
	})
	rt.AgentControl = control
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"1","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "1")["result"]).Thread.ID

	startTurn := func(id, prompt string) {
		payload := map[string]any{
			"id":     id,
			"method": MethodTurnStart,
			"params": TurnStartParams{ThreadID: threadID, Prompt: prompt},
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal turn request: %v", err)
		}
		if err := srv.handleLine(context.Background(), raw); err != nil {
			t.Fatalf("turn/start: %v", err)
		}
	}

	rt.SetUltraMode(true)
	startTurn("2", "do ultra work")
	waitForTurnCompletedCountForThread(t, out, threadID, 1)

	// The turn snapshot reached the orchestration control, so spawns during
	// this turn would inherit Ultra.
	if !control.TurnUltra() {
		t.Fatalf("ultra turn must publish its snapshot to the agent control")
	}

	// Flip the session off: the next user turn snapshots the new value.
	rt.SetUltraMode(false)
	startTurn("3", "do plain work")
	waitForTurnCompletedCountForThread(t, out, threadID, 2)
	if control.TurnUltra() {
		t.Fatalf("non-ultra turn must reset the control's turn snapshot")
	}

	client.mu.Lock()
	requests := append([]providers.ChatRequest(nil), client.requests...)
	client.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("expected two provider requests, got %d", len(requests))
	}

	ultraPolicy := func(req providers.ChatRequest) *providers.ChatMessage {
		for i := range req.Messages {
			msg := req.Messages[i]
			if msg.Role == "user" && strings.Contains(msg.Content, "[TOOL_POLICY]") {
				return &req.Messages[i]
			}
		}
		return nil
	}
	policy := ultraPolicy(requests[0])
	if policy == nil {
		t.Fatalf("ultra turn request missing TOOL_POLICY context:\n%+v", requests[0].Messages)
	}
	if !strings.Contains(policy.Content, "Ultra mode is active") {
		t.Fatalf("unexpected ultra policy content:\n%s", policy.Content)
	}
	if !policy.Hidden || !wuucontext.IsSystemReminder(policy.Name, policy.Content) {
		t.Fatalf("ultra policy should be request-only hidden context: %+v", policy)
	}
	if leaked := ultraPolicy(requests[1]); leaked != nil {
		t.Fatalf("non-ultra turn must not carry the ultra policy:\n%s", leaked.Content)
	}
}
