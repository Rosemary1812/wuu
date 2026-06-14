package loop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStorePersistsStateLedgersAndEvents(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".loop")
	store := NewStore(dir)
	now := fixedClock()
	store.SetClock(now)

	state, err := store.Init(Spec{
		ID:   "loop-test",
		Goal: "ship durable loop state",
		Trigger: Trigger{
			Type:   "manual",
			Source: "test",
		},
		AssignedAgent: "lead",
		EscalationPolicy: EscalationPolicy{
			EscalateOnFailure: true,
		},
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if state.Status != StatusPending || state.CurrentStep != StepInit {
		t.Fatalf("unexpected initial state: %+v", state)
	}

	if _, err := store.AddProgress(StepResearch, "read relevant code"); err != nil {
		t.Fatalf("AddProgress: %v", err)
	}
	if _, err := store.AddDecision(StepPlan, "use LoopRunner", "state must survive context loss"); err != nil {
		t.Fatalf("AddDecision: %v", err)
	}
	if _, err := store.AddFailure(Failure{Step: StepVerification, Kind: "test_failure", Message: "go test failed"}); err != nil {
		t.Fatalf("AddFailure: %v", err)
	}

	loaded, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.Status != StatusNeedsHuman || !loaded.NeedsHuman || loaded.CurrentBlocker != "go test failed" {
		t.Fatalf("failure should update blocker and escalation: %+v", loaded)
	}
	assertFileContains(t, filepath.Join(dir, "progress.md"), "read relevant code")
	assertFileContains(t, filepath.Join(dir, "decisions.md"), "use LoopRunner")
	assertFileContains(t, filepath.Join(dir, "failures.md"), "go test failed")

	events, err := store.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) < 4 {
		t.Fatalf("expected event log entries, got %+v", events)
	}
	if events[len(events)-1].Type != "failure" {
		t.Fatalf("last event should be failure, got %+v", events[len(events)-1])
	}
}

func TestRunnerDemoRecordsVerificationFailure(t *testing.T) {
	root := t.TempDir()
	store := NewStore(filepath.Join(root, ".loop"))
	now := fixedClock()
	store.SetClock(now)
	runner := &Runner{
		Store: store,
		Now:   now,
		Verifier: CommandVerifier{
			WorkDir: root,
			Now:     now,
		},
	}

	state, err := runner.RunDemo(context.Background(), Spec{
		ID:   "loop-fail",
		Goal: "record verifier failure",
		VerificationPolicy: VerificationPolicy{Commands: []CommandCheck{
			{Name: "intentional failure", Command: "echo FAILING && exit 7", TimeoutSeconds: 5, Required: true},
		}},
		EscalationPolicy: EscalationPolicy{EscalateOnFailure: true},
	})
	if err != nil {
		t.Fatalf("RunDemo: %v", err)
	}
	if state.Status != StatusNeedsHuman || len(state.Failures) != 1 {
		t.Fatalf("expected failed verifier state, got %+v", state)
	}
	assertFileContains(t, filepath.Join(root, ".loop", "artifacts", "verification.md"), "FAILING")
	assertFileContains(t, filepath.Join(root, ".loop", "failures.md"), "exit code 7")
}

func TestRunnerDemoCompletesWithPassingVerification(t *testing.T) {
	root := t.TempDir()
	store := NewStore(filepath.Join(root, ".loop"))
	now := fixedClock()
	runner := &Runner{
		Store: store,
		Now:   now,
		Verifier: CommandVerifier{
			WorkDir: root,
			Now:     now,
		},
	}

	state, err := runner.RunDemo(context.Background(), Spec{
		ID:   "loop-pass",
		Goal: "complete verifier pass",
		VerificationPolicy: VerificationPolicy{Commands: []CommandCheck{
			{Name: "marker", Command: "printf ok > marker.txt && test -f marker.txt", TimeoutSeconds: 5, Required: true},
		}},
	})
	if err != nil {
		t.Fatalf("RunDemo: %v", err)
	}
	if state.Status != StatusCompleted {
		t.Fatalf("expected completed state, got %+v", state)
	}
	if state.FinalArtifact == "" {
		t.Fatal("final artifact should be recorded")
	}
	assertFileContains(t, state.FinalArtifact, "Durable state")
	assertFileContains(t, filepath.Join(root, ".loop", "artifacts", "verification.md"), "PASS")
}

func TestStoreRecordsExternalArtifactRefs(t *testing.T) {
	root := t.TempDir()
	store := NewStore(filepath.Join(root, ".loop"))
	now := fixedClock()
	store.SetClock(now)
	if _, err := store.Init(Spec{ID: "loop-artifacts", Goal: "track external artifacts"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	path := filepath.Join(root, "state", "workflows", "run-1", "plan.md")

	if _, err := store.RecordExternalArtifact(ExternalArtifact{
		Source:   "workflow",
		SourceID: "run-1",
		Kind:     "plan",
		Path:     path,
	}); err != nil {
		t.Fatalf("RecordExternalArtifact: %v", err)
	}
	if _, err := store.RecordExternalArtifact(ExternalArtifact{
		Source:   "workflow",
		SourceID: "run-1",
		Kind:     "plan",
		Path:     path,
	}); err != nil {
		t.Fatalf("RecordExternalArtifact duplicate: %v", err)
	}

	state, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(state.Artifacts) != 1 {
		t.Fatalf("external artifact should be deduplicated, got %+v", state.Artifacts)
	}
	artifact := state.Artifacts[0]
	if artifact.Source != "workflow" || artifact.SourceID != "run-1" || artifact.Kind != "plan" || artifact.Path != path {
		t.Fatalf("unexpected external artifact: %+v", artifact)
	}
	events, err := store.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if events[len(events)-1].Type != "external_artifact_synced" || events[len(events)-1].Artifact != path {
		t.Fatalf("external artifact event not recorded: %+v", events)
	}
}

func TestBuiltinRolesIncludeMakerCheckerSeparation(t *testing.T) {
	worker, ok := FindRole("worker")
	if !ok {
		t.Fatal("worker role missing")
	}
	reviewer, ok := FindRole("reviewer")
	if !ok {
		t.Fatal("reviewer role missing")
	}
	if strings.Contains(strings.Join(reviewer.AllowedTools, ","), "apply_patch") ||
		strings.Contains(strings.Join(reviewer.AllowedTools, ","), "edit_file") {
		t.Fatalf("reviewer should not have edit tools: %+v", reviewer.AllowedTools)
	}
	if !strings.Contains(worker.OutputSchema, "changed_files") {
		t.Fatalf("worker output schema should require changed files: %+v", worker)
	}
	if _, err := MustRole("unknown"); err == nil {
		t.Fatal("MustRole should reject unknown role")
	}
}

func fixedClock() func() time.Time {
	ts := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return ts }
}

func assertFileContains(t *testing.T, path, needle string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), needle) {
		t.Fatalf("%s should contain %q, got:\n%s", path, needle, string(data))
	}
}
