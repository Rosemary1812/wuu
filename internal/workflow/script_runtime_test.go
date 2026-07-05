package workflow

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResolveNamedParticipantMatchesPool(t *testing.T) {
	r := &ScriptRuntime{opts: ScriptRuntimeOptions{
		AllowedParticipants: []ParticipantRef{
			{ID: "prt-1", Name: "Rina"},
			{ID: "prt-2", Name: "Kenta"},
		},
	}}

	got, err := r.resolveNamedParticipant("prt-2")
	if err != nil || got.ID != "prt-2" {
		t.Fatalf("resolve by id: got=%+v err=%v", got, err)
	}
	got, err = r.resolveNamedParticipant("rina") // case-insensitive name
	if err != nil || got.ID != "prt-1" {
		t.Fatalf("resolve by name: got=%+v err=%v", got, err)
	}
	if _, err := r.resolveNamedParticipant("stranger"); err == nil {
		t.Fatal("expected non-member reference to be rejected")
	}

	empty := &ScriptRuntime{opts: ScriptRuntimeOptions{}}
	if _, err := empty.resolveNamedParticipant("Rina"); err == nil {
		t.Fatal("expected empty pool to reject any named participant")
	}
}

// TestResolveNamedParticipantRejectsBusyMember proves the decision-five
// concurrency lock: a pool member already executing another task/workflow run
// is refused (told busy) instead of being enlisted into a second workflow.
func TestResolveNamedParticipantRejectsBusyMember(t *testing.T) {
	r := &ScriptRuntime{opts: ScriptRuntimeOptions{
		AllowedParticipants: []ParticipantRef{
			{ID: "prt-1", Name: "Rina", Busy: true},
			{ID: "prt-2", Name: "Kenta"},
		},
	}}
	if _, err := r.resolveNamedParticipant("Rina"); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("expected busy member rejection, got %v", err)
	}
	if _, err := r.resolveNamedParticipant("prt-1"); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("expected busy member rejection by id, got %v", err)
	}
	// An idle member in the same pool still resolves.
	if got, err := r.resolveNamedParticipant("Kenta"); err != nil || got.ID != "prt-2" {
		t.Fatalf("idle member should resolve: got=%+v err=%v", got, err)
	}
}

func TestScriptRuntimeRejectsOutOfGroupParticipant(t *testing.T) {
	store := NewStore(t.TempDir())
	run, err := store.CreateRun(Run{ID: "grp-guard", Status: RunStateRunning})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	runtime := NewScriptRuntime(ScriptRuntimeOptions{
		Store: store,
		RunID: run.ID,
		AllowedParticipants: []ParticipantRef{
			{ID: "prt-1", Name: "Rina"},
		},
		Script: `spawn({ participant: "Stranger", prompt: "do work" });`,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := runtime.Run(ctx); err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("expected out-of-group participant rejection, got %v", err)
	}
}

func TestScriptRuntimeNamedParticipantPassesPoolBeforeDispatch(t *testing.T) {
	store := NewStore(t.TempDir())
	run, err := store.CreateRun(Run{ID: "grp-ok", Status: RunStateRunning})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	// A pool member resolves; the run then fails at dispatch because no
	// AgentControl is wired. Reaching the "agent control not configured" error
	// proves the group-pool gate accepted the member (resolution succeeded).
	runtime := NewScriptRuntime(ScriptRuntimeOptions{
		Store: store,
		RunID: run.ID,
		AllowedParticipants: []ParticipantRef{
			{ID: "prt-1", Name: "Rina"},
		},
		Script: `spawn({ participant: "Rina", prompt: "do work" });`,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := runtime.Run(ctx); err == nil || !strings.Contains(err.Error(), "agent control not configured") {
		t.Fatalf("expected pool-accepted member to fail at dispatch, got %v", err)
	}
}

func TestScriptRuntimeRejectsParticipantWithAgentProfile(t *testing.T) {
	store := NewStore(t.TempDir())
	run, err := store.CreateRun(Run{ID: "grp-conflict", Status: RunStateRunning})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	runtime := NewScriptRuntime(ScriptRuntimeOptions{
		Store: store,
		RunID: run.ID,
		AllowedParticipants: []ParticipantRef{
			{ID: "prt-1", Name: "Rina"},
		},
		Script: `spawn({ participant: "Rina", agentProfile: "qa", prompt: "do work" });`,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := runtime.Run(ctx); err == nil || !strings.Contains(err.Error(), "must not also set agentProfile") {
		t.Fatalf("expected participant+agentProfile conflict, got %v", err)
	}
}

func TestScriptRuntimeWaitsWhileRunPaused(t *testing.T) {
	store := NewStore(t.TempDir())
	run, err := store.CreateRun(Run{
		ID:          "paused-script",
		Status:      RunStatePaused,
		PauseReason: "manual pause",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	runtime := NewScriptRuntime(ScriptRuntimeOptions{
		Store: store,
		RunID: run.ID,
		Script: `
phase("Resume gate", () => {
  synthesize("# Final\n\nresumed");
});
`,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type runtimeResult struct {
		run Run
		err error
	}
	done := make(chan runtimeResult, 1)
	go func() {
		completed, runErr := runtime.Run(ctx)
		done <- runtimeResult{run: completed, err: runErr}
	}()

	select {
	case result := <-done:
		t.Fatalf("runtime completed while paused: run=%+v err=%v", result.run, result.err)
	case <-time.After(150 * time.Millisecond):
	}

	if _, err := store.UpdateRunStatus(run.ID, RunStateRunning, "resume"); err != nil {
		t.Fatalf("UpdateRunStatus resume: %v", err)
	}

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("runtime failed after resume: %v", result.err)
		}
		if result.run.Status != RunStateCompleted {
			t.Fatalf("runtime should complete after resume: %+v", result.run)
		}
		data, err := os.ReadFile(result.run.FinalReportPath)
		if err != nil {
			t.Fatalf("read final report: %v", err)
		}
		if !strings.Contains(string(data), "resumed") {
			t.Fatalf("final report mismatch:\n%s", string(data))
		}
	case <-ctx.Done():
		t.Fatalf("runtime did not complete after resume: %v", ctx.Err())
	}
}
