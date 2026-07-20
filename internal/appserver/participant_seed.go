package appserver

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
)

const (
	defaultSeedParticipantName = "Andy"
	// defaultSeedParticipantRole is a free-form persona note, not a worker
	// type: role no longer selects a task-dispatch surface (named agents all
	// run on one deck), so the product-facing 团队组建者 identity is the role
	// itself. Name (Andy) is the identity axis users see.
	defaultSeedParticipantRole    = "团队组建者"
	defaultSeedParticipantTagline = "帮你把目标拆成清晰的看板任务"
	// defaultAgentSeededMarkerName is the once-ever install marker written
	// under the wuu home. Its presence means the default agent was already
	// seeded (or intentionally skipped); deleting Andy must never resurrect
	// him on the next launch.
	defaultAgentSeededMarkerName = "default-agent-seeded"
)

// defaultSeedParticipantMemory is Andy's preset persona
// It ships as ordinary MEMORY.md content: fully editable, fully deletable,
// zero mechanical privilege.
const defaultSeedParticipantMemory = `# 角色设定（预置人设，可自由修改）

- 名字：Andy
- 角色：团队组建者
- 定位：帮你把目标拆成清晰的看板任务

你擅长理解用户目标，并把它整理成可以在看板中执行的任务。先问清目标、
约束和完成标准，再提出一个最小可用的任务拆分。优先复用现有 named agent，
只在职责长期稳定且确有需要时建议创建新 agent。不要为一次性问题扩充团队。
`

// ensureDefaultParticipant seeds a single named agent "Andy" on first launch.
// It runs once per install, guarded twice: a default-agent-seeded marker in
// the wuu home, and the participant store itself — if any named participant
// has ever existed (active or retired), Andy is not recreated, so retiring
// him cannot cause the next launch to resurrect him.
func (s *Server) ensureDefaultParticipant() error {
	if s == nil || s.rt == nil {
		return fmt.Errorf("runtime session is required")
	}
	sessionDir := strings.TrimSpace(s.rt.SessionDir)
	if sessionDir == "" {
		return fmt.Errorf("runtime session dir is required")
	}
	markerPath := s.defaultAgentSeededMarkerPath()
	if markerPath != "" {
		if _, err := os.Stat(markerPath); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat default agent seed marker: %w", err)
		}
	}

	count, err := session.CountParticipantsByKind(sessionDir, participant.KindNamed)
	if err != nil {
		return fmt.Errorf("count named participants: %w", err)
	}
	if count > 0 {
		// Named participants already exist (Andy may have been retired):
		// record the marker so this install never seeds again.
		return s.writeDefaultAgentSeededMarker(markerPath)
	}

	now := time.Now().UTC()
	seed := participant.Participant{
		ID:        participant.NewID(),
		Kind:      participant.KindNamed,
		Name:      defaultSeedParticipantName,
		Role:      defaultSeedParticipantRole,
		Tagline:   defaultSeedParticipantTagline,
		CreatedAt: now,
		UpdatedAt: now,
	}
	workspace, err := s.participantWorkspace(seed.ID)
	if err != nil {
		return fmt.Errorf("resolve default participant workspace: %w", err)
	}
	seed.Workspace = workspace

	if err := os.MkdirAll(seed.Workspace, 0o755); err != nil {
		return fmt.Errorf("create default participant workspace: %w", err)
	}
	memPath := participantMemoryPath(seed.Workspace)
	if memPath == "" {
		return fmt.Errorf("default participant memory path is empty")
	}
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		if err := os.WriteFile(memPath, []byte(defaultSeedParticipantMemory), 0o644); err != nil {
			return fmt.Errorf("write default participant memory: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("stat default participant memory: %w", err)
	}

	if err := session.UpsertParticipant(sessionDir, seed); err != nil {
		return fmt.Errorf("upsert default participant: %w", err)
	}
	return s.writeDefaultAgentSeededMarker(markerPath)
}

// defaultAgentSeededMarkerPath returns the install-level marker path, or ""
// when the runtime has no wuu home configured (test-only sessions).
func (s *Server) defaultAgentSeededMarkerPath() string {
	if s == nil || s.rt == nil {
		return ""
	}
	wuuHome := strings.TrimSpace(s.rt.WuuHome)
	if wuuHome == "" {
		return ""
	}
	return filepath.Join(wuuHome, defaultAgentSeededMarkerName)
}

func (s *Server) writeDefaultAgentSeededMarker(markerPath string) error {
	if strings.TrimSpace(markerPath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		return fmt.Errorf("create wuu home for seed marker: %w", err)
	}
	content := fmt.Sprintf("seeded at %s\n", time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(markerPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write default agent seed marker: %w", err)
	}
	return nil
}

func logDefaultParticipantSeedError(err error) {
	if err == nil {
		return
	}
	log.Printf("wuu: default participant seed failed: %v", err)
}
