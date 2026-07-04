package appserver

import (
	"errors"
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
	}
	if err := s.rejectAllChannelMutation(id, "delete"); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	deleted, err := session.Delete(s.rt.SessionDir, id)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

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

	s.mu.Lock()
	delete(s.threads, id)
	s.mu.Unlock()

	return s.writeResponse(req.ID, ThreadDeleteResult{ThreadID: id}, nil)
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
