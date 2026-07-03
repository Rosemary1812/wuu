package appserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

type residentParticipantSpeech struct {
	server        *Server
	participantID string
	limiter       *residentSpeechLimiter
}

type residentSpeechLimiter struct {
	mu            sync.Mutex
	postsByThread map[string]int
}

func (s *Server) residentParticipantSpeech(participantID string) tools.ParticipantSpeech {
	return residentParticipantSpeech{
		server:        s,
		participantID: strings.TrimSpace(participantID),
		limiter:       &residentSpeechLimiter{},
	}
}

func (r residentParticipantSpeech) PostMessage(ctx context.Context, kind, text, targetThreadID string) (tools.PostedMessage, error) {
	_ = ctx
	if r.server == nil {
		return tools.PostedMessage{}, errors.New("post_message: app server not configured")
	}
	participantID := strings.TrimSpace(r.participantID)
	if participantID == "" {
		return tools.PostedMessage{}, errors.New("post_message: participant_id is required")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "result"
	}
	switch kind {
	case "result", "question", "update":
	default:
		return tools.PostedMessage{}, fmt.Errorf("post_message: kind %q is not supported", kind)
	}
	if kind == "update" && strings.TrimSpace(targetThreadID) == "" {
		return tools.PostedMessage{}, errors.New("post_message: thread_id is required for update messages")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return tools.PostedMessage{}, errors.New("post_message: text is required")
	}
	targetThreadID, err := r.resolveTargetThread(strings.TrimSpace(targetThreadID))
	if err != nil {
		return tools.PostedMessage{}, err
	}
	if err := r.reservePost(targetThreadID); err != nil {
		return tools.PostedMessage{}, err
	}
	now := time.Now().UTC()
	msg := agentcontrol.ParticipantMessage{
		AgentID:       participantID,
		ParticipantID: participantID,
		Kind:          kind,
		Text:          text,
		CreatedAt:     now,
	}
	if err := r.server.publishParticipantMessage(targetThreadID, msg); err != nil {
		return tools.PostedMessage{}, err
	}
	return tools.PostedMessage{
		AgentID:       participantID,
		ParticipantID: participantID,
		Kind:          kind,
		ThreadID:      targetThreadID,
		Text:          text,
		CreatedAt:     now,
	}, nil
}

func (r residentParticipantSpeech) Decline(ctx context.Context, reason, targetThreadID string) error {
	_ = ctx
	if r.server == nil {
		return errors.New("decline: app server not configured")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("decline: reason is required")
	}
	targetThreadID, err := r.resolveTargetThread(strings.TrimSpace(targetThreadID))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	msg := agentcontrol.ParticipantMessage{
		AgentID:       strings.TrimSpace(r.participantID),
		ParticipantID: strings.TrimSpace(r.participantID),
		Kind:          "decline",
		Text:          reason,
		CreatedAt:     now,
	}
	return r.server.publishParticipantMessage(targetThreadID, msg)
}

func (r residentParticipantSpeech) resolveTargetThread(targetThreadID string) (string, error) {
	if r.server == nil {
		return "", errors.New("post_message: app server not configured")
	}
	participantID := strings.TrimSpace(r.participantID)
	if participantID == "" {
		return "", errors.New("participant_id is required")
	}
	if targetThreadID == "" {
		th, err := r.server.ensureResidentDMThread(participantID)
		if err != nil {
			return "", err
		}
		return th.ID, nil
	}
	th, err := r.server.ensureResidentThread(targetThreadID)
	if err != nil {
		return "", err
	}
	th.mu.Lock()
	isOwnDM := strings.TrimSpace(th.DMParticipantID) == participantID
	th.mu.Unlock()
	if isOwnDM {
		return th.ID, nil
	}
	members, err := session.ListThreadMembers(r.server.rt.SessionDir, th.ID)
	if err != nil {
		return "", err
	}
	for _, memberID := range members {
		if strings.TrimSpace(memberID) == participantID {
			return th.ID, nil
		}
	}
	return "", fmt.Errorf("post_message: participant %q is not a member of thread %q; ask the user to add you to the group first", participantID, th.ID)
}

func (r residentParticipantSpeech) reservePost(targetThreadID string) error {
	if r.limiter == nil {
		return nil
	}
	const maxPostsPerThread = 2
	targetThreadID = strings.TrimSpace(targetThreadID)
	if targetThreadID == "" {
		return errors.New("post_message: thread_id is required")
	}
	r.limiter.mu.Lock()
	defer r.limiter.mu.Unlock()
	if r.limiter.postsByThread == nil {
		r.limiter.postsByThread = make(map[string]int)
	}
	if r.limiter.postsByThread[targetThreadID] >= maxPostsPerThread {
		return fmt.Errorf("post_message: participant %q already posted %d messages to thread %q this turn", strings.TrimSpace(r.participantID), maxPostsPerThread, targetThreadID)
	}
	r.limiter.postsByThread[targetThreadID]++
	return nil
}
