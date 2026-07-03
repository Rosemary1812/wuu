package appserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
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
	displayPrompt := prompt

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

	participantID := strings.TrimSpace(params.ParticipantID)
	subagentType := strings.TrimSpace(params.SubagentType)
	agentProfile := strings.TrimSpace(params.AgentProfile)
	description := strings.TrimSpace(params.Description)
	taskName := strings.TrimSpace(params.TaskName)
	if participantID != "" {
		p, err := session.GetParticipant(s.rt.SessionDir, participantID)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		if p.Kind != participant.KindNamed {
			return s.writeResponse(req.ID, nil, fmt.Errorf("participant %q is not a named agent", participantID))
		}
		if subagentType == "" {
			subagentType = p.Role
		}
		if taskName == "" {
			taskName = p.Name
		}
		if description == "" {
			description = p.Tagline
		}
		if agentProfile == "" {
			agentProfile = p.Name
		}
		memory := ""
		if path := participantMemoryPath(p.Workspace); path != "" {
			if data, err := os.ReadFile(path); err == nil {
				memory = string(data)
			} else if !errors.Is(err, os.ErrNotExist) {
				return s.writeResponse(req.ID, nil, fmt.Errorf("read participant memory: %w", err))
			}
		}
		prompt = namedParticipantPrompt(p, memory, prompt)
	}
	if subagentType == "" {
		subagentType = agentcontrol.DefaultSubagentType
	}
	if taskName == "" {
		taskName = deriveParticipantTaskName(description, prompt, subagentType)
	}

	if params.RecordUserMessage {
		userMsg := userMessageFromPrompt(displayPrompt, nil, nil)
		now := time.Now().UTC()
		th.mu.Lock()
		if th.ReadOnly {
			th.mu.Unlock()
			return s.writeResponse(req.ID, nil, errors.New("thread is read-only"))
		}
		if th.PersistHistory {
			if err := appendChatMessage(s.rt.SessionDir, th.ID, userMsg); err != nil {
				th.mu.Unlock()
				return s.writeResponse(req.ID, nil, err)
			}
		}
		history := cloneHistory(th.History)
		history = append(history, userMsg)
		th.History = history
		turn := th.appendUserMessageTurnLocked(session.NewID(), userMsg, now)
		th.mu.Unlock()
		_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
			ThreadID: threadID,
			Turn:     turn,
		})
	}

	spawned, err := threadRuntime.AgentControl.Spawn(ctx, agentcontrol.SpawnRequest{
		Type:             subagentType,
		TaskName:         taskName,
		ParticipantID:    participantID,
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
	if participantID != "" {
		_ = session.UpsertParticipantRun(s.rt.SessionDir, session.ParticipantRun{
			ID:            spawned.AgentID,
			ParticipantID: participantID,
			AgentID:       spawned.AgentID,
			TaskID:        spawned.TaskName,
			SessionID:     threadID,
			Summary:       participantRunSummary(description, displayPrompt),
			Outcome:       "running",
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		})
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

func participantRunSummary(values ...string) string {
	for _, value := range values {
		summary := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		if summary == "" {
			continue
		}
		runes := []rune(summary)
		if len(runes) > 180 {
			return string(runes[:180])
		}
		return summary
	}
	return ""
}

func namedParticipantPrompt(p participant.Participant, memory, prompt string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s, a long-running named agent in this workspace.", p.Name)
	if role := strings.TrimSpace(p.Role); role != "" {
		fmt.Fprintf(&b, " Your role is %s.", role)
	}
	b.WriteString("\n")
	if tagline := strings.TrimSpace(p.Tagline); tagline != "" {
		fmt.Fprintf(&b, "How your teammates describe you: %s\n", tagline)
	}
	if memory = strings.TrimSpace(memory); memory != "" {
		b.WriteString("\n## Your memory\nNotes you kept from previous work. Trust them, but verify anything that may have gone stale:\n\n")
		b.WriteString(memory)
		b.WriteString("\n")
	}
	b.WriteString("\n## Request\n")
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString("\n\nWhen you are done, post your conclusion with post_message (kind=result). If you are blocked on the user, ask with post_message (kind=question). If no response is actually needed, call decline with a one-line reason. Never end the turn silently.")
	return b.String()
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
