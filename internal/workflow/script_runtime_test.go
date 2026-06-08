package workflow

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

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
