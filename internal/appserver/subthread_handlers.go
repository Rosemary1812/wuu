package appserver

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/session"
)

func (s *Server) handleThreadListSub(req Request) error {
	var params ThreadListSubParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	threads, err := session.ListConversationThreads(s.rt.SessionDir, threadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	views, err := s.conversationSubthreadViews(threadID, threads, false)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadListSubResult{Subthreads: views}, nil)
}

func (s *Server) handleThreadOpenSub(req Request) error {
	var params ThreadOpenSubParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}

	thread, err := s.openConversationSubthread(params)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	view, err := s.conversationSubthreadView(threadID, thread, true)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadOpenSubResult{Subthread: view}, nil)
}

func (s *Server) handleThreadResolveSub(req Request) error {
	var params ThreadResolveSubParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	subthreadID := strings.TrimSpace(params.SubthreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if subthreadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("subthread_id is required"))
	}

	thread, err := s.findConversationSubthread(threadID, subthreadID, "")
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	status := session.ConversationThreadOpen
	if params.Resolved {
		status = session.ConversationThreadResolved
	}
	if err := session.UpdateConversationThreadStatus(s.rt.SessionDir, thread.ID, status); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	thread.Status = status
	view, err := s.conversationSubthreadView(threadID, thread, true)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadResolveSubResult{Subthread: view}, nil)
}

func (s *Server) openConversationSubthread(params ThreadOpenSubParams) (session.ConversationThread, error) {
	threadID := strings.TrimSpace(params.ThreadID)
	subthreadID := strings.TrimSpace(params.SubthreadID)
	anchorItemID := strings.TrimSpace(params.AnchorItemID)
	if subthreadID != "" || anchorItemID != "" {
		if thread, err := s.findConversationSubthread(threadID, subthreadID, anchorItemID); err == nil {
			return thread, nil
		} else if subthreadID != "" || !errors.Is(err, session.ErrConversationThreadNotFound) {
			return session.ConversationThread{}, err
		}
	}
	if anchorItemID == "" {
		return session.ConversationThread{}, errors.New("anchor_item_id is required")
	}
	return session.CreateConversationThread(s.rt.SessionDir, session.ConversationThread{
		SessionID:    threadID,
		AnchorItemID: anchorItemID,
		Title:        params.Title,
		CreatedBy:    params.CreatedBy,
	})
}

func (s *Server) findConversationSubthread(threadID, subthreadID, anchorItemID string) (session.ConversationThread, error) {
	threadID = strings.TrimSpace(threadID)
	subthreadID = strings.TrimSpace(subthreadID)
	anchorItemID = strings.TrimSpace(anchorItemID)
	threads, err := session.ListConversationThreads(s.rt.SessionDir, threadID)
	if err != nil {
		return session.ConversationThread{}, err
	}
	for _, thread := range threads {
		if subthreadID != "" && thread.ID == subthreadID {
			return thread, nil
		}
		if subthreadID == "" && anchorItemID != "" && thread.AnchorItemID == anchorItemID {
			return thread, nil
		}
	}
	key := firstNonEmpty(subthreadID, anchorItemID)
	return session.ConversationThread{}, fmt.Errorf("%w: %q", session.ErrConversationThreadNotFound, key)
}

func (s *Server) conversationSubthreadViews(threadID string, threads []session.ConversationThread, includeTurns bool) ([]ConversationSubthread, error) {
	records, err := loadPersistedMessages(s.rt.SessionDir, threadID, false)
	if err != nil {
		return nil, err
	}
	views := make([]ConversationSubthread, 0, len(threads))
	for _, thread := range threads {
		views = append(views, conversationSubthreadViewFromRecords(threadID, thread, records, includeTurns, s.resolveParticipantSummary))
	}
	return views, nil
}

func (s *Server) conversationSubthreadView(threadID string, thread session.ConversationThread, includeTurns bool) (ConversationSubthread, error) {
	records, err := loadPersistedMessages(s.rt.SessionDir, threadID, false)
	if err != nil {
		return ConversationSubthread{}, err
	}
	return conversationSubthreadViewFromRecords(threadID, thread, records, includeTurns, s.resolveParticipantSummary), nil
}

func conversationSubthreadViewFromRecords(threadID string, thread session.ConversationThread, records []persistedMessage, includeTurns bool, resolve participantSummaryResolver) ConversationSubthread {
	view := ConversationSubthread{
		ID:           thread.ID,
		ThreadID:     thread.SessionID,
		AnchorItemID: thread.AnchorItemID,
		Title:        thread.Title,
		Status:       string(thread.Status),
		CreatedBy:    thread.CreatedBy,
		CreatedAt:    thread.CreatedAt,
	}
	subthreadID := strings.TrimSpace(thread.ID)
	for _, rec := range records {
		if strings.TrimSpace(rec.ThreadID) == subthreadID && !rec.Hidden {
			view.ReplyCount++
		}
	}
	if includeTurns {
		view.Turns = turnsFromConversationSubthreadHistory(threadID, thread.ID, records, time.Now().UTC(), resolve)
	}
	return view
}
