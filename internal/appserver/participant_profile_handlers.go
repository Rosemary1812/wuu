package appserver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
)

const participantMemoryFileName = "MEMORY.md"

func (s *Server) handleParticipantList(req Request) error {
	participants, err := session.ListParticipants(s.rt.SessionDir, participant.KindNamed)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	profiles := make([]ParticipantProfile, 0, len(participants))
	for _, p := range participants {
		profile, err := s.participantProfile(p)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		profiles = append(profiles, profile)
	}
	return s.writeResponse(req.ID, ParticipantListResult{Participants: profiles}, nil)
}

func (s *Server) handleParticipantSave(req Request) error {
	var params ParticipantSaveParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return s.writeResponse(req.ID, nil, errors.New("participant name is required"))
	}
	now := time.Now().UTC()
	p := participant.Participant{
		ID:        strings.TrimSpace(params.ID),
		Kind:      participant.KindNamed,
		Name:      name,
		Role:      strings.TrimSpace(params.Role),
		Avatar:    strings.TrimSpace(params.Avatar),
		Tagline:   strings.TrimSpace(params.Tagline),
		Model:     strings.TrimSpace(params.Model),
		UpdatedAt: now,
	}
	if p.ID == "" {
		p.ID = participant.NewID()
		p.CreatedAt = now
	} else if existing, err := session.GetParticipant(s.rt.SessionDir, p.ID); err == nil {
		p.CreatedAt = existing.CreatedAt
		p.Workspace = existing.Workspace
	} else if !errors.Is(err, session.ErrParticipantNotFound) {
		return s.writeResponse(req.ID, nil, err)
	}
	if p.Avatar == "" {
		p.Avatar = participant.DefaultAvatar(p.Role)
	}
	if p.Workspace == "" {
		workspace, err := s.participantWorkspace(p.ID)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		p.Workspace = workspace
	}
	if err := os.MkdirAll(p.Workspace, 0o755); err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("create participant workspace: %w", err))
	}
	if err := os.WriteFile(participantMemoryPath(p.Workspace), []byte(params.Memory), 0o644); err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("write participant memory: %w", err))
	}
	if err := session.UpsertParticipant(s.rt.SessionDir, p); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	s.invalidateParticipantSummary(p.ID)
	profile, err := s.participantProfile(p)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ParticipantSaveResult{Participant: profile}, nil)
}

func (s *Server) handleParticipantFeedback(req Request) error {
	var params ParticipantFeedbackParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := strings.TrimSpace(params.ParticipantID)
	text := strings.TrimSpace(params.Text)
	if id == "" {
		return s.writeResponse(req.ID, nil, errors.New("participant_id is required"))
	}
	if text == "" {
		return s.writeResponse(req.ID, nil, errors.New("feedback text is required"))
	}
	p, err := session.GetParticipant(s.rt.SessionDir, id)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if err := os.MkdirAll(p.Workspace, 0o755); err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("create participant workspace: %w", err))
	}
	var b strings.Builder
	b.WriteString("\n\n## Feedback ")
	b.WriteString(time.Now().UTC().Format("2006-01-02"))
	b.WriteString("\n")
	if taskID := strings.TrimSpace(params.TaskID); taskID != "" {
		b.WriteString("- Task: ")
		b.WriteString(taskID)
		b.WriteString("\n")
	}
	if messageID := strings.TrimSpace(params.MessageID); messageID != "" {
		b.WriteString("- Message: ")
		b.WriteString(messageID)
		b.WriteString("\n")
	}
	b.WriteString("- Note: ")
	b.WriteString(text)
	b.WriteString("\n")
	if err := appendParticipantMemory(p.Workspace, b.String()); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	p.UpdatedAt = time.Now().UTC()
	if err := session.UpsertParticipant(s.rt.SessionDir, p); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	profile, err := s.participantProfile(p)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ParticipantFeedbackResult{Participant: profile}, nil)
}

func (s *Server) handleParticipantReset(req Request) error {
	var params ParticipantResetParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := strings.TrimSpace(params.ParticipantID)
	if id == "" {
		return s.writeResponse(req.ID, nil, errors.New("participant_id is required"))
	}
	scope := strings.ToLower(strings.TrimSpace(params.Scope))
	if scope == "" {
		scope = "restart"
	}
	p, err := session.GetParticipant(s.rt.SessionDir, id)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	switch scope {
	case "restart":
	case "session":
		if err := appendParticipantMemory(p.Workspace, "\n\n## Session reset\n- Runtime state reset.\n"); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	case "full":
		if err := os.MkdirAll(p.Workspace, 0o755); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("create participant workspace: %w", err))
		}
		if err := os.WriteFile(participantMemoryPath(p.Workspace), nil, 0o644); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("reset participant memory: %w", err))
		}
	default:
		return s.writeResponse(req.ID, nil, fmt.Errorf("unsupported reset scope %q", scope))
	}
	p.UpdatedAt = time.Now().UTC()
	if err := session.UpsertParticipant(s.rt.SessionDir, p); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	s.invalidateParticipantSummary(p.ID)
	profile, err := s.participantProfile(p)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ParticipantResetResult{Participant: profile}, nil)
}

func (s *Server) participantProfile(p participant.Participant) (ParticipantProfile, error) {
	memory := ""
	if path := participantMemoryPath(p.Workspace); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			memory = string(data)
		} else if !errors.Is(err, os.ErrNotExist) {
			return ParticipantProfile{}, fmt.Errorf("read participant memory: %w", err)
		}
	}
	runs, err := session.ListParticipantRuns(s.rt.SessionDir, p.ID, 10)
	if err != nil {
		return ParticipantProfile{}, err
	}
	trackRecord := make([]ParticipantRunEntry, 0, len(runs))
	for _, run := range runs {
		trackRecord = append(trackRecord, ParticipantRunEntry{
			TaskID:    run.TaskID,
			Summary:   run.Summary,
			Outcome:   run.Outcome,
			CreatedAt: run.CreatedAt,
		})
	}
	return ParticipantProfile{
		ID:          p.ID,
		Kind:        string(p.Kind),
		Name:        p.Name,
		Role:        p.Role,
		Avatar:      p.Avatar,
		Tagline:     p.Tagline,
		Workspace:   p.Workspace,
		Model:       p.Model,
		Memory:      memory,
		TrackRecord: trackRecord,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}, nil
}

func (s *Server) participantWorkspace(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("participant id is required")
	}
	root := strings.TrimSpace(s.rt.WuuHome)
	if root == "" {
		stateDir, err := s.workspaceStateDir()
		if err != nil {
			return "", err
		}
		root = stateDir
	}
	return filepath.Join(root, "participants", id), nil
}

func participantMemoryPath(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	return filepath.Join(workspace, participantMemoryFileName)
}

func appendParticipantMemory(workspace, text string) error {
	if strings.TrimSpace(workspace) == "" {
		return errors.New("participant workspace is required")
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return fmt.Errorf("create participant workspace: %w", err)
	}
	file, err := os.OpenFile(participantMemoryPath(workspace), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("append participant memory: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		return fmt.Errorf("append participant memory: %w", err)
	}
	return nil
}

func (s *Server) invalidateParticipantSummary(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	s.participantMu.Lock()
	delete(s.participantSummaryCache, id)
	s.participantMu.Unlock()
}
