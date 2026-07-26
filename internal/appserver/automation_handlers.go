package appserver

import (
	"errors"
	"strings"

	"github.com/blueberrycongee/wuu/internal/automation"
)

func (s *Server) handleAutomationList(req Request) error {
	if s == nil || s.rt == nil || s.rt.AutomationManager == nil {
		return s.writeResponse(req.ID, nil, errors.New("automation manager is unavailable"))
	}
	tasks, err := s.rt.AutomationManager.ListTasks()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, AutomationListResult{Tasks: tasks}, nil)
}

func (s *Server) handleAutomationRuns(req Request) error {
	if s == nil || s.rt == nil || s.rt.AutomationManager == nil {
		return s.writeResponse(req.ID, nil, errors.New("automation manager is unavailable"))
	}
	runs, err := s.rt.AutomationManager.ListRuns()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, AutomationRunsResult{Runs: runs}, nil)
}

func (s *Server) handleAutomationUpdate(req Request) error {
	if s == nil || s.rt == nil || s.rt.AutomationManager == nil {
		return s.writeResponse(req.ID, nil, errors.New("automation manager is unavailable"))
	}
	var params AutomationUpdateParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	params.ID = strings.TrimSpace(params.ID)
	if params.ID == "" {
		return s.writeResponse(req.ID, nil, errors.New("id is required"))
	}
	task, err := s.rt.AutomationManager.UpdateTask(params.ID, automation.UpdateTaskParams{
		Title: params.Title, Prompt: params.Prompt, Schedule: params.Schedule,
		Timezone: params.Timezone, Mode: params.Mode,
		HeartbeatThreadID: params.HeartbeatThreadID,
		Recurring:         params.Recurring, Paused: params.Paused,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, AutomationUpdateResult{Task: task}, nil)
}

func (s *Server) handleAutomationRemove(req Request) error {
	if s == nil || s.rt == nil || s.rt.AutomationManager == nil {
		return s.writeResponse(req.ID, nil, errors.New("automation manager is unavailable"))
	}
	var params AutomationRemoveParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if err := s.rt.AutomationManager.RemoveTask(strings.TrimSpace(params.ID)); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, OKResult{OK: true}, nil)
}
