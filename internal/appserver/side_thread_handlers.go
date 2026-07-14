package appserver

import (
	"errors"
	"strings"

	"github.com/blueberrycongee/wuu/internal/sidethread"
)

// handleSideThreadOpen implements sideThread/open. It is lazy: a
// fresh main thread that has never sent a side-chat message returns
// {summary: nil} so the renderer can render an empty state without
// touching the disk. The first sendSideThreadMessage (committed
// later) materializes the on-disk record.
func (s *Server) handleSideThreadOpen(req Request) error {
	var params SideThreadOpenParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	mainID := strings.TrimSpace(params.MainThreadID)
	if mainID == "" {
		return s.writeResponse(req.ID, nil, errors.New("main_thread_id is required"))
	}
	res, err := s.openSideThread(mainID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, res, nil)
}

// handleSideThreadGetHistory implements sideThread/getHistory. The
// renderer interprets ErrNotFound as summary==null (the lazy-open
// contract from the design doc).
func (s *Server) handleSideThreadGetHistory(req Request) error {
	var params SideThreadGetHistoryParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	mainID := strings.TrimSpace(params.MainThreadID)
	if mainID == "" {
		return s.writeResponse(req.ID, nil, errors.New("main_thread_id is required"))
	}
	res, err := s.getSideThreadHistory(mainID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, res, nil)
}

// openSideThread is the testable inner half of handleSideThreadOpen.
// It returns summary==nil for both "no record yet" and "feature off"
// so callers can render the empty panel without distinguishing them.
func (s *Server) openSideThread(mainID string) (*SideThreadOpenResult, error) {
	if strings.TrimSpace(mainID) == "" {
		return nil, errors.New("main_thread_id is required")
	}
	store := s.sideThreadStore
	if store == nil {
		return &SideThreadOpenResult{Summary: nil}, nil
	}
	st, err := store.Load(mainID)
	if err != nil {
		if errors.Is(err, sidethread.ErrNotFound) {
			return &SideThreadOpenResult{Summary: nil}, nil
		}
		return nil, err
	}
	return &SideThreadOpenResult{
		Summary: sideThreadWireSummary(st, s.mainTaskSnapshot(mainID)),
	}, nil
}

// getSideThreadHistory is the testable inner half of handleSideThreadGetHistory.
// ErrNotFound is propagated unchanged so the IPC layer can translate it to a
// summary==null response on the renderer side.
func (s *Server) getSideThreadHistory(mainID string) (*SideThreadGetHistoryResult, error) {
	if strings.TrimSpace(mainID) == "" {
		return nil, errors.New("main_thread_id is required")
	}
	store := s.sideThreadStore
	if store == nil {
		return nil, sidethread.ErrNotFound
	}
	st, err := store.Load(mainID)
	if err != nil {
		return nil, err
	}
	summary := sideThreadWireSummary(st, s.mainTaskSnapshot(mainID))
	if summary == nil {
		return nil, sidethread.ErrNotFound
	}
	return &SideThreadGetHistoryResult{
		Summary:  *summary,
		Messages: sideThreadWireMessages(st.Messages),
	}, nil
}

// sideThreadWireSummary projects the internal SideThread onto the
// protocol wire shape. Appserver owns the wire shape because the
// runtime adds fields the storage layer does not (main_task_summary
// comes from the live parent main thread).
func sideThreadWireSummary(st *sidethread.SideThread, mainTask *MainTaskSnapshot) *SideThreadWireSummary {
	if st == nil {
		return nil
	}
	out := &SideThreadWireSummary{
		SideThreadID: st.SideThreadID,
		MainThreadID: st.MainThreadID,
		Status:       string(st.Status),
		CreatedAt:    st.CreatedAt,
		UpdatedAt:    st.UpdatedAt,
	}
	if mainTask != nil {
		snap := SideThreadMainTaskSummary{Running: mainTask.Running}
		if strings.TrimSpace(mainTask.LastUserMessage) != "" {
			snap.LastUserMessage = mainTask.LastUserMessage
		}
		out.MainTaskSummary = &snap
	}
	return out
}

func sideThreadWireMessages(in []sidethread.Message) []SideThreadWireMessage {
	if len(in) == 0 {
		return []SideThreadWireMessage{}
	}
	out := make([]SideThreadWireMessage, 0, len(in))
	for _, m := range in {
		out = append(out, SideThreadWireMessage{
			ID:           m.ID,
			SideThreadID: m.SideThreadID,
			Role:         string(m.Role),
			Text:         m.Text,
			Status:       string(m.Status),
			ErrorMessage: m.ErrorText,
			CreatedAt:    m.CreatedAt,
		})
	}
	return out
}

// MainTaskSnapshot is the lightweight view of the parent main thread
// that the side-thread summary carries for the renderer header.
type MainTaskSnapshot struct {
	Running         bool
	LastUserMessage string
}

// mainTaskSnapshot reads the parent main thread's running flag from
// the appserver's resident-thread table. It returns nil when the
// runtime cannot resolve the parent cheaply (no SessionDir, missing
// main thread, etc.); the renderer treats nil as "header without
// status", which is acceptable for V1. LastUserMessage is reserved
// for a later commit once the history-load is cheap enough to run on
// every side-thread open.
func (s *Server) mainTaskSnapshot(mainThreadID string) *MainTaskSnapshot {
	if s == nil || s.rt == nil {
		return nil
	}
	if strings.TrimSpace(mainThreadID) == "" {
		return nil
	}
	snap := &MainTaskSnapshot{}
	if s.threads != nil {
		if th, ok := s.threads[mainThreadID]; ok && th != nil {
			th.mu.Lock()
			snap.Running = th.running
			th.mu.Unlock()
		}
	}
	return snap
}
