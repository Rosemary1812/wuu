package toolledger

import (
	"context"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

func TestLedgerPersistsLifecycleAndProjection(t *testing.T) {
	dir := t.TempDir()
	ledger, err := New(dir, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	batchID, err := ledger.BeginBatch(ctx, "operation-1", 2)
	if err != nil {
		t.Fatal(err)
	}
	call := providers.ToolCall{ID: "provider-call-1", Name: "run_shell", Arguments: `{"command":"pwd"}`}
	invocation, err := ledger.Prepare(ctx, batchID, call, "")
	if err != nil {
		t.Fatal(err)
	}
	if invocation.ID == call.ID || invocation.ReplayPolicy != ReplayAtMostOnce || invocation.State != InvocationPrepared {
		t.Fatalf("prepared invocation = %+v", invocation)
	}
	if err := ledger.FinalizeBatch(ctx, batchID); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Start(ctx, invocation.ID); err != nil {
		t.Fatal(err)
	}
	result := toolresult.FromText("/workspace")
	if err := ledger.Settle(ctx, invocation.ID, result); err != nil {
		t.Fatal(err)
	}
	pending, err := ledger.PendingProjection(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != invocation.ID || pending[0].Result.TextProjection() != "/workspace" {
		t.Fatalf("pending projection = %+v", pending)
	}
	decision, err := ledger.DecideReplay(ctx, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ReplayBlock || decision.Reason != ReplayReasonInvocationSettled || len(decision.BlockingInvocationIDs) != 1 {
		t.Fatalf("settled replay decision = %+v", decision)
	}
	if err := ledger.MarkProjected(ctx, []string{invocation.ID}); err != nil {
		t.Fatal(err)
	}
	if pending, err := ledger.PendingProjection(ctx); err != nil || len(pending) != 0 {
		t.Fatalf("pending after projection = %+v, %v", pending, err)
	}

	db, err := session.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var batchState string
	if err := db.QueryRow(`SELECT status FROM tool_batches WHERE id = ?`, batchID).Scan(&batchState); err != nil {
		t.Fatal(err)
	}
	if batchState != string(BatchProjected) {
		t.Fatalf("batch state = %q, want projected", batchState)
	}
}

func TestLedgerReconcileBlocksUnknownRunningInvocation(t *testing.T) {
	dir := t.TempDir()
	ledger, err := New(dir, "thread-crash")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	batchID, err := ledger.BeginBatch(ctx, "operation-crash", 1)
	if err != nil {
		t.Fatal(err)
	}
	running, err := ledger.Prepare(ctx, batchID, providers.ToolCall{ID: "call-running", Name: "write_file", Arguments: `{}`}, ReplayAtMostOnce)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Start(ctx, running.ID); err != nil {
		t.Fatal(err)
	}
	prepared, err := ledger.Prepare(ctx, batchID, providers.ToolCall{ID: "call-prepared", Name: "read_file", Arguments: `{}`}, ReplayAtMostOnce)
	if err != nil {
		t.Fatal(err)
	}

	recovered, err := New(dir, "thread-crash")
	if err != nil {
		t.Fatal(err)
	}
	decision, err := recovered.DecideReplay(ctx, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ReplayBlock || decision.Reason != ReplayReasonInvocationUnknown ||
		len(decision.BlockingInvocationIDs) != 1 || decision.BlockingInvocationIDs[0] != running.ID {
		t.Fatalf("crash replay decision = %+v", decision)
	}
	db, err := session.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var runningState, preparedState, batchState string
	if err := db.QueryRow(`SELECT state FROM tool_invocations WHERE id = ?`, running.ID).Scan(&runningState); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state FROM tool_invocations WHERE id = ?`, prepared.ID).Scan(&preparedState); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM tool_batches WHERE id = ?`, batchID).Scan(&batchState); err != nil {
		t.Fatal(err)
	}
	if runningState != string(InvocationInterruptedUnknown) || preparedState != string(InvocationAbandoned) || batchState != string(BatchInterrupted) {
		t.Fatalf("reconciled states = %q/%q/%q", runningState, preparedState, batchState)
	}
}

func TestLedgerScopesProviderCallIDsToBatchAndFreezesMetadata(t *testing.T) {
	dir := t.TempDir()
	ledger, err := New(dir, "thread-scope")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	call := providers.ToolCall{ID: "provider-reused", Name: "read_file", Arguments: `{"path":"a"}`}
	firstBatch, err := ledger.BeginBatch(ctx, "operation-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	first, err := ledger.Prepare(ctx, firstBatch, call, ReplayAtMostOnce)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := ledger.Prepare(ctx, firstBatch, call, ReplayAtMostOnce)
	if err != nil {
		t.Fatal(err)
	}
	if repeat.ID != first.ID {
		t.Fatalf("idempotent prepare returned %q, want %q", repeat.ID, first.ID)
	}
	changed := call
	changed.Arguments = `{"path":"b"}`
	if _, err := ledger.Prepare(ctx, firstBatch, changed, ReplayAtMostOnce); err == nil {
		t.Fatal("provider call metadata mutation was accepted")
	}
	secondBatch, err := ledger.BeginBatch(ctx, "operation-2", 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ledger.Prepare(ctx, secondBatch, call, ReplayAtMostOnce)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatal("provider call id was treated as global invocation identity")
	}
}

func TestLedgerFinalizationClosesCollectionAndSettlesCompletedBatch(t *testing.T) {
	dir := t.TempDir()
	ledger, err := New(dir, "thread-finalize")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	batchID, err := ledger.BeginBatch(ctx, "operation-finalize", 1)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := ledger.Prepare(ctx, batchID, providers.ToolCall{ID: "call-1", Name: "write_file", Arguments: `{}`}, ReplayAtMostOnce)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Start(ctx, invocation.ID); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Settle(ctx, invocation.ID, toolresult.FromText("done")); err != nil {
		t.Fatal(err)
	}
	if err := ledger.FinalizeBatch(ctx, batchID); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Prepare(ctx, batchID, providers.ToolCall{ID: "late", Name: "read_file", Arguments: `{}`}, ReplayAtMostOnce); err == nil {
		t.Fatal("finalized batch accepted a new invocation")
	}

	db, err := session.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var batchState string
	if err := db.QueryRow(`SELECT status FROM tool_batches WHERE id = ?`, batchID).Scan(&batchState); err != nil {
		t.Fatal(err)
	}
	if batchState != string(BatchSettled) {
		t.Fatalf("batch state = %q, want settled", batchState)
	}
}

func TestLedgerReplayDecisionUsesHighestRiskState(t *testing.T) {
	dir := t.TempDir()
	ledger, err := New(dir, "thread-risk")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	batchID, err := ledger.BeginBatch(ctx, "operation-risk", 1)
	if err != nil {
		t.Fatal(err)
	}
	settled, err := ledger.Prepare(ctx, batchID, providers.ToolCall{ID: "call-settled", Name: "write_file", Arguments: `{}`}, ReplayAtMostOnce)
	if err != nil {
		t.Fatal(err)
	}
	running, err := ledger.Prepare(ctx, batchID, providers.ToolCall{ID: "call-running", Name: "run_shell", Arguments: `{}`}, ReplayAtMostOnce)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Start(ctx, settled.ID); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Settle(ctx, settled.ID, toolresult.FromText("done")); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Start(ctx, running.ID); err != nil {
		t.Fatal(err)
	}
	decision, err := ledger.DecideReplay(ctx, batchID)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ReplayBlock || decision.Reason != ReplayReasonInvocationRunning {
		t.Fatalf("mixed-state replay decision = %+v", decision)
	}
}
