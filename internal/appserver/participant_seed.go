package appserver

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
)

const (
	defaultSeedParticipantName    = "Andy"
	defaultSeedParticipantRole    = "general-purpose"
	defaultSeedParticipantAvatar  = "🦉"
	defaultSeedParticipantTagline = "随时开工的常驻搭档，可以帮你搭建团队"
)

// ensureDefaultParticipant seeds a single named agent "Andy" on first launch.
// It runs once per install: if any named participant has ever existed (active
// or retired), Andy is not recreated, so retiring him cannot cause the next
// launch to resurrect him.
func (s *Server) ensureDefaultParticipant() error {
	if s == nil || s.rt == nil {
		return fmt.Errorf("runtime session is required")
	}
	sessionDir := strings.TrimSpace(s.rt.SessionDir)
	if sessionDir == "" {
		return fmt.Errorf("runtime session dir is required")
	}

	count, err := session.CountParticipantsByKind(sessionDir, participant.KindNamed)
	if err != nil {
		return fmt.Errorf("count named participants: %w", err)
	}
	if count > 0 {
		return nil
	}

	now := time.Now().UTC()
	seed := participant.Participant{
		ID:        participant.NewID(),
		Kind:      participant.KindNamed,
		Name:      defaultSeedParticipantName,
		Role:      defaultSeedParticipantRole,
		Avatar:    defaultSeedParticipantAvatar,
		Tagline:   defaultSeedParticipantTagline,
		CreatedAt: now,
		UpdatedAt: now,
	}
	workspace, err := s.participantWorkspace(seed.ID)
	if err != nil {
		return fmt.Errorf("resolve default participant workspace: %w", err)
	}
	seed.Workspace = workspace

	// Only materialize the workspace directory and empty MEMORY.md when the
	// runtime has a real WuuHome. Test environments often fall through to a
	// home-relative path, and writing under it risks racing with TempDir
	// cleanup on platforms where SQLite WAL files don't unlink cleanly.
	if strings.TrimSpace(s.rt.WuuHome) != "" {
		if err := os.MkdirAll(seed.Workspace, 0o755); err != nil {
			return fmt.Errorf("create default participant workspace: %w", err)
		}
		memPath := participantMemoryPath(seed.Workspace)
		if memPath == "" {
			return fmt.Errorf("default participant memory path is empty")
		}
		if _, err := os.Stat(memPath); os.IsNotExist(err) {
			if err := os.WriteFile(memPath, nil, 0o644); err != nil {
				return fmt.Errorf("write default participant memory: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("stat default participant memory: %w", err)
		}
	}

	if err := session.UpsertParticipant(sessionDir, seed); err != nil {
		return fmt.Errorf("upsert default participant: %w", err)
	}
	return nil
}

func logDefaultParticipantSeedError(err error) {
	if err == nil {
		return
	}
	log.Printf("wuu: default participant seed failed: %v", err)
}
