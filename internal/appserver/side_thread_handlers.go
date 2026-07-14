package appserver

import (
	"errors"
	"fmt"
	"strings"
	"time"

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

// handleSideThreadSendMessage implements sideThread/sendMessage. It
// lazily creates the on-disk side-thread record on the first send,
// assigns a fresh side_thread_id, persists the user message, and
// marks Status=running. The actual agent turn driver and the
// SideThreadEvent broadcast are wired in a later commit; once they
// land they will read this same record.
func (s *Server) handleSideThreadSendMessage(req Request) error {
	var params SideThreadSendParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	mainID := strings.TrimSpace(params.MainThreadID)
	if mainID == "" {
		return s.writeResponse(req.ID, nil, errors.New("main_thread_id is required"))
	}
	prompt := strings.TrimSpace(params.Prompt)
	if prompt == "" {
		return s.writeResponse(req.ID, nil, errors.New("prompt is required"))
	}
	res, err := s.sendSideThreadMessage(mainID, prompt)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, res, nil)
}

// sendSideThreadMessage is the testable inner half of
// handleSideThreadSendMessage. It performs the lazy-create +
// append + status-flip, then re-reads the canonical record to
// build the wire summary.
func (s *Server) sendSideThreadMessage(mainID, prompt string) (*SideThreadSendResult, error) {
	if strings.TrimSpace(mainID) == "" {
		return nil, errors.New("main_thread_id is required")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("prompt is required")
	}
	if s.sideThreadStore == nil {
		return nil, errors.New("side-thread feature unavailable")
	}
	userMsgID := generateSideMessageID()
	now := time.Now().UTC()

	mutateErr := s.sideThreadStore.Mutate(mainID, func(st *sidethread.SideThread) error {
		if st.SideThreadID == "" {
			sid, err := sidethread.NewSideThreadID()
			if err != nil {
				return err
			}
			st.SideThreadID = sid
		}
		st.Status = sidethread.StatusRunning
		st.Messages = append(st.Messages, sidethread.Message{
			ID:           userMsgID,
			SideThreadID: st.SideThreadID,
			Role:         sidethread.RoleUser,
			Text:         prompt,
			CreatedAt:    now,
		})
		return nil
	})
	if mutateErr == nil {
		st, err := s.sideThreadStore.Load(mainID)
		if err != nil {
			return nil, err
		}
		return &SideThreadSendResult{
			UserMessageID: userMsgID,
			Summary:       *sideThreadWireSummary(st, s.mainTaskSnapshot(mainID)),
		}, nil
	}
	if !errors.Is(mutateErr, sidethread.ErrNotFound) {
		return nil, mutateErr
	}
	// First send for this main thread — materialize a fresh record.
	sid, idErr := sidethread.NewSideThreadID()
	if idErr != nil {
		return nil, idErr
	}
	st := &sidethread.SideThread{
		SideThreadID: sid,
		MainThreadID: mainID,
		Status:       sidethread.StatusRunning,
		CreatedAt:    now,
		UpdatedAt:    now,
		Messages: []sidethread.Message{
			{
				ID:           userMsgID,
				SideThreadID: sid,
				Role:         sidethread.RoleUser,
				Text:         prompt,
				CreatedAt:    now,
			},
		},
	}
	if err := s.sideThreadStore.Save(st); err != nil {
		return nil, err
	}
	return &SideThreadSendResult{
		UserMessageID: userMsgID,
		Summary:       *sideThreadWireSummary(st, s.mainTaskSnapshot(mainID)),
	}, nil
}

// handleSideThreadInterrupt implements sideThread/interrupt. The
// pure-state path is the only thing scoped here: flipping a
// running side-thread's Status to StatusInterrupted. Any actual
// agent turn driver in a later commit will observe the status and
// cancel its in-flight request.
func (s *Server) handleSideThreadInterrupt(req Request) error {
	var params SideThreadInterruptParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	mainID := strings.TrimSpace(params.MainThreadID)
	if mainID == "" {
		return s.writeResponse(req.ID, nil, errors.New("main_thread_id is required"))
	}
	res, err := s.interruptSideThread(mainID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, res, nil)
}

func (s *Server) interruptSideThread(mainID string) (*SideThreadInterruptResult, error) {
	if s.sideThreadStore == nil {
		return &SideThreadInterruptResult{Ok: false}, nil
	}
	if strings.TrimSpace(mainID) == "" {
		return &SideThreadInterruptResult{Ok: false}, nil
	}
	mutateErr := s.sideThreadStore.Mutate(mainID, func(st *sidethread.SideThread) error {
		if st.Status == sidethread.StatusRunning {
			st.Status = sidethread.StatusInterrupted
		}
		return nil
	})
	if errors.Is(mutateErr, sidethread.ErrNotFound) {
		return &SideThreadInterruptResult{Ok: false}, nil
	}
	if mutateErr != nil {
		return nil, mutateErr
	}
	return &SideThreadInterruptResult{Ok: true}, nil
}

// generateSideMessageID produces a deterministic-enough id for V1.
// Real cryptographically-strong entropy can replace this once the
// renderer needs ids that survive across cold starts.
func generateSideMessageID() string {
	return fmt.Sprintf("sm_%d", time.Now().UTC().UnixNano())
}

// cascadeSideThreadForMain removes the on-disk side thread bound to
// mainID. Invoked from handleThreadDelete after the parent session
// row has been dropped (design §8: "删除 Main Thread 时,同时删除
// 其 Side Thread"). Best-effort by design — a failed delete only
// leaves recoverable disk state behind, never resurrects the
// session. No-op when the store is unconfigured or the record is
// absent.
func (s *Server) cascadeSideThreadForMain(mainID string) {
	if s == nil || s.sideThreadStore == nil {
		return
	}
	if strings.TrimSpace(mainID) == "" {
		return
	}
	_ = s.sideThreadStore.Delete(mainID)
}
