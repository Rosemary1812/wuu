package toolctx

import (
	"context"
	"testing"
)

func TestWorktreePathRoundTrip(t *testing.T) {
	ctx := WithWorktreePath(context.Background(), "/tmp/worktrees/session/fork")
	got, ok := WorktreePath(ctx)
	if !ok || got != "/tmp/worktrees/session/fork" {
		t.Fatalf("WorktreePath = %q, %v; want bound path", got, ok)
	}
}

func TestWorktreePathTrimsWhitespace(t *testing.T) {
	ctx := WithWorktreePath(context.Background(), "  /tmp/wt  ")
	got, ok := WorktreePath(ctx)
	if !ok || got != "/tmp/wt" {
		t.Fatalf("WorktreePath = %q, %v; want trimmed path", got, ok)
	}
}

func TestWorktreePathEmptyBindingIsNoop(t *testing.T) {
	base := context.Background()
	ctx := WithWorktreePath(base, "   ")
	if ctx != base {
		t.Fatalf("empty worktree binding should return the original context")
	}
	if got, ok := WorktreePath(ctx); ok || got != "" {
		t.Fatalf("WorktreePath on unbound ctx = %q, %v; want none", got, ok)
	}
}

func TestWorktreePathNilContext(t *testing.T) {
	if got, ok := WorktreePath(nil); ok || got != "" {
		t.Fatalf("WorktreePath(nil) = %q, %v; want none", got, ok)
	}
	ctx := WithWorktreePath(nil, "/tmp/wt")
	if got, ok := WorktreePath(ctx); !ok || got != "/tmp/wt" {
		t.Fatalf("WithWorktreePath(nil, ...) should still bind, got %q, %v", got, ok)
	}
}

func TestWaitInterruptRoundTrip(t *testing.T) {
	interrupt := make(chan struct{})
	ctx := WithWaitInterrupt(context.Background(), interrupt)
	if got := WaitInterrupt(ctx); got != interrupt {
		t.Fatal("WaitInterrupt did not return the bound signal")
	}
	close(interrupt)
	select {
	case <-WaitInterrupt(ctx):
	default:
		t.Fatal("bound wait interrupt did not preserve closure")
	}
}

func TestWaitInterruptNilBindingIsNoop(t *testing.T) {
	base := context.Background()
	if ctx := WithWaitInterrupt(base, nil); ctx != base {
		t.Fatal("nil wait interrupt should return the original context")
	}
	if got := WaitInterrupt(nil); got != nil {
		t.Fatal("WaitInterrupt(nil) should return nil")
	}
}
