package appserver

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
)

func (s *Server) handleParticipantStart(ctx context.Context, req Request) error {
	var params ParticipantStartParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	prompt := strings.TrimSpace(params.Prompt)
	if prompt == "" {
		return s.writeResponse(req.ID, nil, errors.New("prompt is required"))
	}

	th, err := s.ensureResidentThread(threadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadRuntime, err := s.ensureThreadRuntime(th)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if threadRuntime == nil || threadRuntime.AgentControl == nil {
		return s.writeResponse(req.ID, nil, errors.New("participant/start requires agent control"))
	}

	subagentType := strings.TrimSpace(params.SubagentType)
	if subagentType == "" {
		subagentType = agentcontrol.DefaultSubagentType
	}
	agentProfile := strings.TrimSpace(params.AgentProfile)
	description := strings.TrimSpace(params.Description)
	taskName := strings.TrimSpace(params.TaskName)
	if taskName == "" {
		taskName = deriveParticipantTaskName(description, prompt, subagentType)
	}

	spawned, err := threadRuntime.AgentControl.Spawn(ctx, agentcontrol.SpawnRequest{
		Type:             subagentType,
		TaskName:         taskName,
		AgentProfile:     agentProfile,
		Description:      description,
		Prompt:           prompt,
		ParentID:         threadID,
		ParentPath:       agentthread.RootPath,
		SpeechCapability: true,
		Isolation:        strings.TrimSpace(params.Isolation),
		Synchronous:      false,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	for _, snap := range threadRuntime.AgentControl.List() {
		if snap.ID == spawned.AgentID {
			return s.writeResponse(req.ID, ParticipantStartResult{
				Agent: s.agentFromSnapshot(threadRuntime.AgentControl, snap),
			}, nil)
		}
	}

	return s.writeResponse(req.ID, ParticipantStartResult{
		Agent: Agent{
			ID:           spawned.AgentID,
			Type:         subagentType,
			TaskName:     spawned.TaskName,
			AgentProfile: spawned.AgentProfile,
			AgentPath:    spawned.AgentPath,
			ParentID:     threadID,
			Description:  description,
			Status:       spawned.Status,
			StartedAt:    time.Now().UTC(),
		},
	}, nil)
}

func deriveParticipantTaskName(description, prompt, subagentType string) string {
	for _, value := range []string{description, prompt, subagentType} {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		fields := strings.Fields(name)
		if len(fields) > 6 {
			fields = fields[:6]
		}
		name = strings.Join(fields, "_")
		name = strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z':
				return r
			case r >= 'A' && r <= 'Z':
				return r
			case r >= '0' && r <= '9':
				return r
			case r == '_' || r == '-':
				return r
			default:
				return '_'
			}
		}, name)
		name = strings.Trim(name, "_-")
		if name != "" {
			return name
		}
	}
	return "participant_task"
}
