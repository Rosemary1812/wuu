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
	"github.com/blueberrycongee/wuu/internal/statepath"
)

// archivedDirName is the sibling directory that receives a retired
// participant's on-disk state: participants/.archived/<id>/ for the
// workspace (including the memory/ identity notebook) and
// agents/.archived/<id>-home/ for the DM agent home.
//
// Archival is a MOVE, never a delete. Memory directories must survive
// retirement so a future 复职 (rehire) can restore them — memory-redesign
// §9 marks "cleanup must not delete memory directories" as a red line.
const archivedDirName = ".archived"

// retireNamedParticipant runs the full retire cleanup protocol for one named
// participant (consistency repair plan §1 #7). Steps, in order:
//
//  1. Storage: session.RetireParticipant stamps retired_at and, in the same
//     transaction, removes the participant's thread_members rows and drops
//     its unconsumed resident_inbox envelopes.
//  2. Disk: the participant workspace (participants/<id>/, containing
//     MEMORY.md, avatar, and the memory/ notebook) moves to
//     participants/.archived/<id>/; the DM agent home (agents/<id>/home)
//     moves to agents/.archived/<id>-home/. Nothing is deleted.
//  3. Storage again: the participants row's workspace column is repointed at
//     the archived location so profile reads and a future rehire see the
//     real on-disk path.
//  4. Runtime: any loaded DM thread bound to the participant is frozen
//     read-only. Persisted-but-unloaded DM threads get the same freeze at
//     load time (loadPersistedThreadState checks retired_at), so the freeze
//     survives restarts without a schema change.
//
// The storage retire is the source of truth and runs first; the follow-up
// steps are collected with errors.Join so a disk failure never leaves the
// participant half-active. Re-running the protocol is safe: the store call
// is idempotent, and archiving skips sources that are already gone.
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
	if err := s.archiveAgentHome(p.ID); err != nil {
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
	s.freezeParticipantDMThreads(p.ID)
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

// archiveAgentHome moves agents/<id>/home into agents/.archived/<id>-home/.
// The wuu home is resolved the same way createResidentDMThreadState resolves
// it when creating the home, so the archive looks where the home was made.
func (s *Server) archiveAgentHome(participantID string) error {
	wuuHome := strings.TrimSpace(s.rt.WuuHome)
	if wuuHome == "" {
		home, err := statepath.Home("")
		if err != nil {
			// No resolvable wuu home means no agent home was ever created.
			return nil
		}
		wuuHome = home
	}
	src := statepath.AgentHomeDir(wuuHome, participantID)
	dst := filepath.Join(wuuHome, "agents", archivedDirName, participantID+"-home")
	moved, _, err := archiveDir(src, dst)
	if err != nil {
		return fmt.Errorf("archive agent home: %w", err)
	}
	if moved {
		// The agents/<id>/ container only held home/. os.Remove refuses
		// non-empty directories, so this can never destroy data.
		_ = os.Remove(filepath.Dir(src))
	}
	return nil
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

// freezeParticipantDMThreads marks every loaded DM thread bound to the
// participant read-only, reusing Thread.ReadOnly's existing semantics
// (turn/queue and queued-turn drains reject read-only threads; the history
// stays browsable). turn/start additionally rejects retired DM participants
// with an explicit error so the user learns why the thread is frozen.
func (s *Server) freezeParticipantDMThreads(participantID string) {
	participantID = strings.TrimSpace(participantID)
	if s == nil || participantID == "" {
		return
	}
	s.mu.Lock()
	threads := make([]*threadState, 0, len(s.threads))
	for _, th := range s.threads {
		if th != nil {
			threads = append(threads, th)
		}
	}
	s.mu.Unlock()
	for _, th := range threads {
		th.mu.Lock()
		if strings.TrimSpace(th.DMParticipantID) == participantID {
			th.ReadOnly = true
		}
		th.mu.Unlock()
	}
}

// participantRetired reports whether the participant exists in the store and
// carries a retired_at stamp.
func (s *Server) participantRetired(participantID string) bool {
	if s == nil || s.rt == nil {
		return false
	}
	p, err := session.GetParticipant(s.rt.SessionDir, strings.TrimSpace(participantID))
	return err == nil && p.RetiredAt != nil
}
