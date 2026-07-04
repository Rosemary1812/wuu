package appserver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/statepath"
)

// workspaceStateArchiveDirName is the folder inside a workspace state
// directory that receives archived (never hard-deleted) memory data during
// a state cleanup. It is skipped by the deletion sweep so repeated cleanups
// keep earlier archives intact.
const workspaceStateArchiveDirName = ".archived"

// workspaceMemoryDirNames are the state-dir subdirectories that hold
// memory-class data: "memory" is the per-workspace project memory
// (internal/sessionmemory), "memory-store" is the legacy workspace
// memstore layer. Both fall under self-consistency invariant 3 (memory is
// archived, never hard-deleted).
var workspaceMemoryDirNames = map[string]bool{
	"memory":       true,
	"memory-store": true,
}

// handleWorkspaceStateCleanup reclaims the local state of a workspace the
// user has removed from the desktop sidebar: everything under the workspace
// state directory is deleted except the memory directories, which move into
// .archived/. The serving instance refuses to clean its own active
// workspace; the desktop only issues this call for removed projects, whose
// state dirs are addressed by stable id (or path for legacy path-keyed
// dirs) rather than by the caller's runtime context.
func (s *Server) handleWorkspaceStateCleanup(req Request) error {
	var params WorkspaceStateCleanupParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	home, err := statepath.Home("")
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	var stateDir string
	switch {
	case strings.TrimSpace(params.WorkspaceID) != "":
		stateDir, err = statepath.WorkspaceDirByID(home, params.WorkspaceID)
	case strings.TrimSpace(params.WorkspacePath) != "":
		stateDir, err = statepath.WorkspaceDir(home, params.WorkspacePath)
	default:
		err = errors.New("workspace_id or workspace_path is required")
	}
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if own, ownErr := s.workspaceStateDir(); ownErr == nil && filepath.Clean(own) == filepath.Clean(stateDir) {
		return s.writeResponse(req.ID, nil, errors.New("cannot clean up the active workspace's state directory"))
	}
	result, err := cleanupWorkspaceStateDir(stateDir)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, result, nil)
}

// cleanupWorkspaceStateDir deletes every entry of a workspace state
// directory except the archive folder itself and the memory directories,
// which are moved into .archived/ (invariant 3: memory-class data is
// archived, never hard-deleted). A missing state directory is not an
// error — there is simply nothing to clean.
func cleanupWorkspaceStateDir(stateDir string) (WorkspaceStateCleanupResult, error) {
	result := WorkspaceStateCleanupResult{StateDir: stateDir}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, fmt.Errorf("read workspace state dir: %w", err)
	}
	archiveRoot := filepath.Join(stateDir, workspaceStateArchiveDirName)
	for _, entry := range entries {
		name := entry.Name()
		if name == workspaceStateArchiveDirName {
			continue
		}
		path := filepath.Join(stateDir, name)
		if entry.IsDir() && workspaceMemoryDirNames[name] {
			if err := archiveWorkspaceMemoryDir(archiveRoot, path, name); err != nil {
				return result, err
			}
			result.MemoryArchived = true
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return result, fmt.Errorf("remove %s: %w", name, err)
		}
	}
	result.Removed = true
	return result, nil
}

func archiveWorkspaceMemoryDir(archiveRoot, path, name string) error {
	if err := os.MkdirAll(archiveRoot, 0o755); err != nil {
		return fmt.Errorf("create archive dir: %w", err)
	}
	target := filepath.Join(archiveRoot, name)
	if _, err := os.Stat(target); err == nil {
		// A previous cleanup already archived this directory; keep both
		// snapshots by suffixing the new one with a UTC timestamp.
		target = filepath.Join(archiveRoot, name+"-"+time.Now().UTC().Format("20060102T150405Z"))
	}
	if err := os.Rename(path, target); err != nil {
		return fmt.Errorf("archive %s: %w", name, err)
	}
	return nil
}
