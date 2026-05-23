// Package worktree wraps git worktree operations for subagent isolation.
//
// Each subagent in coordinator mode runs inside its own git worktree,
// rooted under the workspace state directory at
// worktrees/{session-id}/{worker-id}/. The worktree is
// created in detached HEAD mode based on the parent repository's current
// HEAD, so workers see a snapshot of the project at spawn time without
// polluting the parent's branch state.
//
// Worktrees persist after the worker completes so the user can inspect
// the changes (cd into the path, git diff, cherry-pick, etc.). Cleanup
// is explicit via Cleanup() or CleanupSession().
package worktree

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree represents one isolated git worktree for a subagent.
type Worktree struct {
	Path      string // absolute path to the worktree directory
	SessionID string // owning session
	WorkerID  string // worker that owns it
	HEAD      string // the commit it was created from (full sha)
}

// Manager creates and tracks worktrees rooted at a parent repository.
type Manager struct {
	parentRepo string // absolute path to the source git repo (parent cwd)
	rootDir    string // workspace-state worktrees directory
}

// NewManager constructs a Manager. parentRepo must be inside a git
// repository (or be one). rootDir is where worktrees are stored,
// typically under the user-level workspace state directory.
func NewManager(parentRepo, rootDir string) (*Manager, error) {
	abs, err := filepath.Abs(parentRepo)
	if err != nil {
		return nil, fmt.Errorf("resolve parent repo: %w", err)
	}
	if !isGitRepo(abs) {
		return nil, fmt.Errorf("not a git repository: %s (run 'git init' first)", abs)
	}
	return &Manager{
		parentRepo: abs,
		rootDir:    rootDir,
	}, nil
}

// Create allocates a new worktree for the given session/worker.
// If baseRepo is empty, the parent repo's current HEAD is used as the
// source. If baseRepo points to another existing worktree, the new
// worktree is based on that worktree's HEAD instead — useful for
// chaining workers.
func (m *Manager) Create(sessionID, workerID, baseRepo string) (*Worktree, error) {
	if sessionID == "" || workerID == "" {
		return nil, errors.New("sessionID and workerID required")
	}

	source := m.parentRepo
	if strings.TrimSpace(baseRepo) != "" {
		abs, err := filepath.Abs(baseRepo)
		if err != nil {
			return nil, fmt.Errorf("resolve base repo: %w", err)
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("base repo does not exist: %s", abs)
		}
		source = abs
	}

	// Resolve the source's current HEAD so the worktree is reproducible.
	head, err := resolveHead(source)
	if err != nil {
		return nil, fmt.Errorf("resolve HEAD of %s: %w", source, err)
	}

	target := filepath.Join(m.rootDir, sessionID, workerID)
	if _, err := os.Stat(target); err == nil {
		return nil, fmt.Errorf("worktree path already exists: %s", target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, fmt.Errorf("create worktree parent dir: %w", err)
	}

	// `git worktree add --detach <path> <commit>` creates a worktree at
	// the given commit with no branch attached.
	cmd := exec.Command("git", "worktree", "add", "--detach", target, head)
	cmd.Dir = source
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git worktree add failed: %w\n%s", err, out)
	}

	return &Worktree{
		Path:      target,
		SessionID: sessionID,
		WorkerID:  workerID,
		HEAD:      head,
	}, nil
}

// Cleanup removes a worktree from disk and unregisters it from git.
// Safe to call on a worktree that's already been removed.
func (m *Manager) Cleanup(wt *Worktree) error {
	if wt == nil || wt.Path == "" {
		return nil
	}
	if _, err := os.Stat(wt.Path); errors.Is(err, os.ErrNotExist) {
		// Already gone — still tell git to forget it.
		_ = m.pruneFromGit(wt.Path)
		return nil
	}
	cmd := exec.Command("git", "worktree", "remove", "--force", wt.Path)
	cmd.Dir = m.parentRepo
	if out, err := cmd.CombinedOutput(); err != nil {
		// Try a manual removal as a fallback (e.g., the worktree was
		// already detached from git's metadata for some reason).
		if rmErr := os.RemoveAll(wt.Path); rmErr != nil {
			return fmt.Errorf("git worktree remove: %w\n%s\n(rmAll: %v)", err, out, rmErr)
		}
		_ = m.pruneFromGit(wt.Path)
	}
	return nil
}

// HasChanges reports whether the worktree contains any uncommitted
// modifications relative to its base HEAD. Used by the coordinator to
// decide whether a finished worker's worktree can be auto-pruned.
//
// Detects: tracked-file edits, staged changes, and untracked files.
// Returns false on a pristine worktree (read-only worker did nothing).
func (m *Manager) HasChanges(wt *Worktree) (bool, error) {
	if wt == nil || wt.Path == "" {
		return false, errors.New("worktree is nil")
	}
	if _, err := os.Stat(wt.Path); err != nil {
		return false, fmt.Errorf("stat worktree: %w", err)
	}
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = wt.Path
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// CleanupIfClean removes the worktree only when it has no uncommitted
// changes. Returns kept=true (with no error) if the worktree was dirty
// and was therefore preserved for the user to inspect.
//
// Mirrors Claude Code's behavior: ephemeral read-only sub-agents
// shouldn't leave detritus on disk, but anything the worker actually
// modified must survive so the orchestrator (or user) can review and
// merge it.
func (m *Manager) CleanupIfClean(wt *Worktree) (kept bool, err error) {
	if wt == nil || wt.Path == "" {
		return false, nil
	}
	dirty, err := m.HasChanges(wt)
	if err != nil {
		return false, err
	}
	if dirty {
		return true, nil
	}
	if cleanupErr := m.Cleanup(wt); cleanupErr != nil {
		return false, cleanupErr
	}
	return false, nil
}

// CleanupSession removes all worktrees belonging to a session.
func (m *Manager) CleanupSession(sessionID string) error {
	dir := filepath.Join(m.rootDir, sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var firstErr error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		wt := &Worktree{
			Path:      filepath.Join(dir, e.Name()),
			SessionID: sessionID,
			WorkerID:  e.Name(),
		}
		if err := m.Cleanup(wt); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// Try to remove the now-empty session directory.
	_ = os.Remove(dir)
	return firstErr
}

// List returns all worktrees currently on disk for the given session.
// Note: this scans the filesystem, not git's worktree registry.
func (m *Manager) List(sessionID string) ([]*Worktree, error) {
	dir := filepath.Join(m.rootDir, sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Worktree
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, &Worktree{
			Path:      filepath.Join(dir, e.Name()),
			SessionID: sessionID,
			WorkerID:  e.Name(),
		})
	}
	return out, nil
}

// pruneFromGit asks git to forget worktrees that no longer exist on disk.
// Used as a best-effort cleanup; errors are silently swallowed because
// pruning is non-critical.
func (m *Manager) pruneFromGit(_ string) error {
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = m.parentRepo
	return cmd.Run()
}

// ParentRepo returns the absolute path of the source git repository
// this manager was created against.
func (m *Manager) ParentRepo() string {
	return m.parentRepo
}

// IsGitRepo reports whether the given directory is inside a git repo.
func IsGitRepo(dir string) bool {
	return isGitRepo(dir)
}

func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// resolveHead returns the full sha of the given repo's current HEAD.
func resolveHead(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
