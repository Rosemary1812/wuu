package appserver

import (
	"errors"
	"strings"

	"github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/tools"
)

func (s *Server) handleProcessList(req Request) error {
	if len(req.Params) > 0 {
		var params struct{}
		if err := decodeParams(req.Params, &params); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	if s.rt == nil || s.rt.ProcessManager == nil {
		return s.writeResponse(req.ID, nil, errors.New("process manager is not available"))
	}
	processes, err := s.rt.ProcessManager.List()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	summaries := make([]ManagedProcessSummary, 0, len(processes))
	for _, p := range processes {
		summaries = append(summaries, managedProcessSummary(p))
	}
	return s.writeResponse(req.ID, ProcessListResult{Processes: summaries}, nil)
}

func (s *Server) handleProcessStop(req Request) error {
	var params ProcessStopParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	processID := strings.TrimSpace(params.ProcessID)
	if processID == "" {
		return s.writeResponse(req.ID, nil, errors.New("process_id is required"))
	}
	if s.rt == nil || s.rt.ProcessManager == nil {
		return s.writeResponse(req.ID, nil, errors.New("process manager is not available"))
	}
	stopped, err := s.rt.ProcessManager.Stop(processID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if stopped == nil {
		return s.writeResponse(req.ID, nil, errors.New("process not found"))
	}
	return s.writeResponse(req.ID, ProcessStopResult{Process: managedProcessSummary(*stopped)}, nil)
}

func managedProcessSummary(p process.Process) ManagedProcessSummary {
	return ManagedProcessSummary{
		ID:                p.ID,
		OwnerKind:         string(p.OwnerKind),
		OwnerID:           p.OwnerID,
		Lifecycle:         string(p.Lifecycle),
		Status:            string(p.Status),
		PID:               p.PID,
		TTY:               p.TTY,
		Command:           tools.RedactToolOutput(p.Command),
		CWD:               p.CWD,
		PreviewURLs:       append([]string(nil), p.PreviewURLs...),
		PrimaryPreviewURL: p.PrimaryPreviewURL,
		StartedAt:         p.StartedAt,
		UpdatedAt:         p.UpdatedAt,
		StoppedAt:         p.StoppedAt,
		ExitCode:          p.ExitCode,
		LastError:         tools.RedactToolOutput(p.LastError),
	}
}
