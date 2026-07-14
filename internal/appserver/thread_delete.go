package appserver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/worktree"
)

// handleThreadDelete permanently removes a conversation. Unlike archive
// (which only hides the thread), delete is the storage-hygiene path: it
// removes the session row (chat history, conversation threads, and group
// membership cascade via foreign keys), the workspace-scoped session
// artifact directory (workers/threads/harness/goal_runtime.json), and any
// fork worktree still bound to the thread. Only archived or otherwise idle
// (not running) threads are eligible; the built-in #all channel is never
// deletable.
func (s *Server) handleThreadDelete(req Request) error {
	var params ThreadDeleteParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := strings.TrimSpace(params.ThreadID)
	if id == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if th := s.thread(id); th != nil {
		th.mu.Lock()
		running := th.running
		th.mu.Unlock()
		if running {
			return s.writeResponse(req.ID, nil, errors.New("cannot delete a running thread"))
		}
		if threadHasActiveAgents(th) {
			return s.writeResponse(req.ID, nil, errors.New("cannot delete a thread with active agents"))
		}
	}
	if err := s.rejectAllChannelMutation(id, "delete"); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	sideThreadLease, err := s.acquireSideThreadExecutionLease(id)
	if errors.Is(err, errSideThreadExecutionBusy) {
		return s.writeResponse(req.ID, nil, errors.New("cannot delete a thread while its side thread is running"))
	}
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	defer releaseSideThreadExecutionLease(id, sideThreadLease)

	lifecycleLease, err := s.acquireThreadLifecycleWriteLease(id)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	defer releaseThreadLifecycleWriteLease(id, lifecycleLease)

	if s.sideThreadStore != nil {
		if err := s.sideThreadStore.Delete(id); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("delete side thread for %q: %w", id, err))
		}
	}

	deleted, err := session.Delete(s.rt.SessionDir, id)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	// Remove the in-memory owner and stop its subscriptions before deleting
	// runtime artifacts. Otherwise an idle thread leaves the AgentControl and
	// app-server forwarding goroutines alive after its durable state is gone.
	s.mu.Lock()
	removed := s.threads[id]
	delete(s.threads, id)
	s.mu.Unlock()
	if removed != nil {
		removed.mu.Lock()
		threadRuntime := removed.execRuntime
		removed.mu.Unlock()
		if threadRuntime != nil && threadRuntime.AgentControl != nil {
			// The active-agent check and durable delete are not one lock scope.
			// Cancel a worker that happened to start between them rather than
			// leaving it attached to a thread whose durable owner is gone.
			threadRuntime.AgentControl.StopAll()
		}
	}
	releaseThreadRuntime(removed)

	// Everything past this point is best-effort cleanup: the session row
	// (and its cascaded history) is already gone, so a failing worktree or
	// artifact removal must not resurrect the thread — it only leaves disk
	// garbage that a later cleanup pass can reclaim.
	stateDir, stateDirErr := s.workspaceStateDir()
	if info, bound := deleted.WorktreeInfo(); bound && stateDirErr == nil {
		// Only remove worktrees that live under this workspace's managed
		// worktree root. A stale or foreign path in the session metadata
		// must never turn into an os.RemoveAll of arbitrary directories.
		if pathWithinRoot(info.Path, statepath.WorktreeRoot(stateDir)) {
			if manager, mgrErr := s.worktreeManager(firstNonEmpty(info.BaseRepo, s.rt.RootDir)); mgrErr == nil {
				_ = manager.Cleanup(&worktree.Worktree{Path: info.Path, SessionID: id})
			}
		}
	}
	if stateDirErr == nil {
		_ = os.RemoveAll(statepath.SessionArtifactDir(stateDir, id))
	}

	return s.writeResponse(req.ID, ThreadDeleteResult{ThreadID: id}, nil)
}

func threadHasActiveAgents(th *threadState) bool {
	if th == nil {
		return false
	}
	th.mu.Lock()
	threadRuntime := th.execRuntime
	th.mu.Unlock()
	if threadRuntime == nil || threadRuntime.AgentControl == nil {
		return false
	}
	for _, snapshot := range threadRuntime.AgentControl.List() {
		if isRunningSubAgentStatus(snapshot.Status) {
			return true
		}
	}
	return false
}

func pathWithinRoot(path, root string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	root = filepath.Clean(strings.TrimSpace(root))
	if path == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "."
}
