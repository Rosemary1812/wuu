package toolctx

import (
	"context"
	"strings"
)

type stepIndexKey struct{}

type worktreePathKey struct{}

// WithStepIndex annotates tool execution context with the model step that
// requested the tool. The value is telemetry-only; tools must not branch on it.
func WithStepIndex(ctx context.Context, stepIndex int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, stepIndexKey{}, stepIndex)
}

func StepIndex(ctx context.Context) (int, bool) {
	if ctx == nil {
		return 0, false
	}
	stepIndex, ok := ctx.Value(stepIndexKey{}).(int)
	return stepIndex, ok
}

// WithWorktreePath binds the isolated git-worktree checkout a
// worktree-forked thread must execute in (fork-to-worktree step 5). The
// turn entry injects the session's persisted worktree path here; file and
// shell tools switch their execution CWD to this checkout only AFTER their
// ordinary sandbox / whitelist checks pass, so the binding never widens
// what a tool may touch — it only relocates approved workspace paths into
// the isolated copy.
func WithWorktreePath(ctx context.Context, path string) context.Context {
	path = strings.TrimSpace(path)
	if path == "" {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, worktreePathKey{}, path)
}

// WorktreePath reports the worktree checkout bound to this tool execution,
// if any. Tools that do not touch the filesystem can ignore it.
func WorktreePath(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	path, ok := ctx.Value(worktreePathKey{}).(string)
	if !ok || strings.TrimSpace(path) == "" {
		return "", false
	}
	return path, true
}
