package appserver

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/memdir"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

// participantMemoryNotebookDir returns the memdir identity notebook directory
// for a participant workspace (participants/<id>/memory). It is the append-merge
// unit for fork memory回流 — the same directory the resident writes durable
// notes into (its file scope root, turn_handlers SetFileScopeRoots), and the
// same directory archived intact on retire.
func participantMemoryNotebookDir(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	return filepath.Join(workspace, "memory")
}

// forkNamedParticipant creates a temporary分身 (decision six): a new KindNamed
// participant that copies the母体's memory snapshot (its whole workspace —
// MEMORY.md, the memory/ notebook, and avatar) so it starts with the母体's
// experience rather than as an empty shell. The copy is marked ForkedFrom =
// mother.ID and named "<母体> 的分身". It participates like any other named
// agent (add it to a group, dispatch it a task); on retire its accumulated
// memory is append-merged back into the母体 (see mergeForkMemoryBack).
//
// This is NOT the session/conversation fork (session.ForkedFromID) nor the
// spawn_agent history fork — it is a named-agent identity copy with a memory
// snapshot. The active-roster capacity cap (maxNamedParticipants) still applies,
// so a full roster must free a slot before forking: reuse an existing named
// agent or spawn anonymous workers for grunt work first (缺人优先级).
func (s *Server) forkNamedParticipant(mother participant.Participant) (participant.Participant, error) {
	if s == nil || s.rt == nil {
		return participant.Participant{}, errors.New("fork participant: server runtime is not configured")
	}
	if mother.Kind != participant.KindNamed {
		return participant.Participant{}, fmt.Errorf("fork participant: %q is not a named agent (kind=%s)", mother.Name, mother.Kind)
	}
	if mother.RetiredAt != nil {
		return participant.Participant{}, fmt.Errorf("fork participant: %q is retired", mother.Name)
	}
	if err := s.ensureNamedParticipantCapacity(); err != nil {
		return participant.Participant{}, fmt.Errorf("fork participant: %w", err)
	}

	name, err := s.uniqueForkName(mother.Name)
	if err != nil {
		return participant.Participant{}, err
	}
	now := time.Now().UTC()
	fork := participant.Participant{
		ID:         participant.NewID(),
		Kind:       participant.KindNamed,
		Name:       name,
		Role:       mother.Role,
		Tagline:    mother.Tagline,
		Model:      mother.Model,
		ForkedFrom: mother.ID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	workspace, werr := s.participantWorkspace(fork.ID)
	if werr != nil {
		return participant.Participant{}, werr
	}
	fork.Workspace = workspace

	// Persist the row first (same ordering rationale as Save: a failed disk
	// copy must not leave a fork identity missing from the store).
	if err := session.UpsertParticipant(s.rt.SessionDir, fork); err != nil {
		return participant.Participant{}, err
	}
	s.invalidateParticipantSummary(fork.ID)
	if err := copyParticipantWorkspaceSnapshot(mother.Workspace, fork.Workspace); err != nil {
		return participant.Participant{}, err
	}
	return fork, nil
}

// uniqueForkName returns "<motherName> 的分身", disambiguated with a numeric
// suffix when an active named agent already holds the name (the store enforces
// a unique index on active named-participant names).
func (s *Server) uniqueForkName(motherName string) (string, error) {
	motherName = strings.TrimSpace(motherName)
	if motherName == "" {
		motherName = "Agent"
	}
	active, err := session.ListParticipants(s.rt.SessionDir, participant.KindNamed)
	if err != nil {
		return "", fmt.Errorf("fork participant: list roster: %w", err)
	}
	taken := make(map[string]struct{}, len(active))
	for _, p := range active {
		taken[strings.ToLower(strings.TrimSpace(p.Name))] = struct{}{}
	}
	base := motherName + " 的分身"
	candidate := base
	for i := 2; ; i++ {
		if _, ok := taken[strings.ToLower(candidate)]; !ok {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s %d", base, i)
	}
}

// mergeForkMemoryBack append-merges a fork's identity notebook into its母体's
// (decision six memory回流) before the fork is archived on retire. It resolves
// the母体, serializes on the母体's merge lock, and unions the fork's topic
// files into the母体's notebook (de-duplicated by content). Best-effort: a
// missing/retired母体 or a merge error is logged and swallowed — retirement
// still archives the fork's workspace intact under .archived/, so no memory is
// ever destroyed. No-op for a non-fork participant.
func (s *Server) mergeForkMemoryBack(fork participant.Participant) {
	motherID := strings.TrimSpace(fork.ForkedFrom)
	if s == nil || s.rt == nil || motherID == "" {
		return
	}
	mother, err := session.GetParticipant(s.rt.SessionDir, motherID)
	if err != nil {
		providers.DebugLogf("fork merge-back: mother %q not found: %v", motherID, err)
		return
	}
	if mother.RetiredAt != nil {
		providers.DebugLogf("fork merge-back: mother %q is retired; skipping merge", motherID)
		return
	}
	src := participantMemoryNotebookDir(fork.Workspace)
	dst := participantMemoryNotebookDir(mother.Workspace)
	if src == "" || dst == "" {
		return
	}

	lock := s.lockForkMerge(mother.ID)
	lock.Lock()
	defer lock.Unlock()

	res, err := memdir.MergeNotebook(src, dst)
	if err != nil {
		providers.DebugLogf("fork merge-back: merge %q -> %q: %v", fork.ID, mother.ID, err)
		return
	}
	providers.DebugLogf("fork merge-back: %q -> %q added=%d skipped=%d conflicts=%d",
		fork.ID, mother.ID, res.TopicsAdded, res.TopicsSkipped, res.Conflicts)
	// Surface the母体's refreshed memory in any open profile/memory panel.
	if profile, perr := s.participantProfile(mother); perr == nil {
		if nerr := s.notifyParticipantUpdated(profile); nerr != nil {
			providers.DebugLogf("fork merge-back: notify mother %q: %v", mother.ID, nerr)
		}
	}
}

// lockForkMerge returns the per-母体 merge mutex, creating it on first use.
// Serializing on the母体 makes the same-topic conflict path in MergeNotebook
// deterministic when a母体 is forked more than once and its副本 retire at once.
func (s *Server) lockForkMerge(motherID string) *sync.Mutex {
	s.forkMergeMu.Lock()
	defer s.forkMergeMu.Unlock()
	if s.forkMergeLocks == nil {
		s.forkMergeLocks = make(map[string]*sync.Mutex)
	}
	mu, ok := s.forkMergeLocks[motherID]
	if !ok {
		mu = &sync.Mutex{}
		s.forkMergeLocks[motherID] = mu
	}
	return mu
}

// copyParticipantWorkspaceSnapshot recursively copies the母体's workspace into
// the fork's workspace so the fork starts with the母体's memory. A missing
// source is not an error — the fork just starts with an empty workspace.
func copyParticipantWorkspaceSnapshot(src, dst string) error {
	src = strings.TrimSpace(src)
	dst = strings.TrimSpace(dst)
	if dst == "" {
		return errors.New("fork participant: destination workspace is required")
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("fork participant: create workspace: %w", err)
	}
	if src == "" {
		return nil
	}
	info, err := os.Stat(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("fork participant: stat source workspace: %w", err)
	}
	if !info.IsDir() {
		return nil
	}
	return copyTree(src, dst)
}

// copyTree copies the directory tree rooted at src into dst (which must exist).
func copyTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("fork participant: read %s: %w", src, err)
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return fmt.Errorf("fork participant: mkdir %s: %w", dstPath, err)
			}
			if err := copyTree(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if !e.Type().IsRegular() {
			continue // skip symlinks/devices in a memory workspace
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("fork participant: open %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("fork participant: create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("fork participant: copy %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("fork participant: close %s: %w", dst, err)
	}
	return nil
}
