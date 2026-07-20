package appserver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
)

// archivedDirName is the sibling directory that receives a retired
// participant's on-disk profile state under participants/.archived/<id>/.
//
// Archival is a MOVE, never a delete. Memory directories must survive
// retirement so a future 复职 (rehire) can restore them — memory-redesign
// §9 marks "cleanup must not delete memory directories" as a red line.
const archivedDirName = ".archived"

// retireNamedParticipant retires the profile in storage, archives its profile
// directory without deleting memory, and repoints the stored workspace path.
func (s *Server) retireNamedParticipant(p participant.Participant) error {
	if s == nil || s.rt == nil {
		return errors.New("retire participant: server runtime is not configured")
	}
	if err := session.RetireParticipant(s.rt.SessionDir, p.ID); err != nil {
		return err
	}
	s.invalidateParticipantSummary(p.ID)

	var errs []error
	archivedWorkspace, err := archiveParticipantWorkspace(p.Workspace)
	if err != nil {
		errs = append(errs, err)
	}
	if archivedWorkspace != "" {
		if refreshed, err := session.GetParticipant(s.rt.SessionDir, p.ID); err != nil {
			errs = append(errs, err)
		} else {
			refreshed.Workspace = archivedWorkspace
			refreshed.UpdatedAt = time.Now().UTC()
			if err := session.UpsertParticipant(s.rt.SessionDir, refreshed); err != nil {
				errs = append(errs, fmt.Errorf("repoint retired workspace: %w", err))
			}
		}
	}
	return errors.Join(errs...)
}

// archiveParticipantWorkspace moves participants/<id>/ into
// participants/.archived/<id>/ and returns the archived path ("" when there
// was nothing on disk to archive).
func archiveParticipantWorkspace(workspace string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", nil
	}
	dst := filepath.Join(filepath.Dir(workspace), archivedDirName, filepath.Base(workspace))
	moved, finalDst, err := archiveDir(workspace, dst)
	if err != nil {
		return "", fmt.Errorf("archive participant workspace: %w", err)
	}
	if !moved {
		return "", nil
	}
	return finalDst, nil
}

// archiveDir moves src to dst, creating dst's parent directory. A missing
// src is not an error — there is nothing to archive (reported via the bool).
// If dst already exists (a previous partial cleanup already archived
// something under this name) the new archive gets a timestamp suffix rather
// than overwriting: archived state is never deleted or replaced.
func archiveDir(src, dst string) (bool, string, error) {
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return false, "", nil
	} else if err != nil {
		return false, "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, "", err
	}
	if _, err := os.Stat(dst); err == nil {
		dst += "-" + strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	}
	if err := os.Rename(src, dst); err != nil {
		return false, "", err
	}
	return true, dst, nil
}
