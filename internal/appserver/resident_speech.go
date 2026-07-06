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
	hopByThread   map[string]int
}

type residentSpeechLimiter struct {
	mu            sync.Mutex
	postsByThread map[string]int
}

func (s *Server) residentParticipantSpeech(participantID string) tools.ParticipantSpeech {
	return s.residentParticipantSpeechWithHops(participantID, nil)
}

func (s *Server) residentParticipantSpeechWithHops(participantID string, hopByThread map[string]int) tools.ParticipantSpeech {
	hops := make(map[string]int, len(hopByThread))
	for threadID, hop := range hopByThread {
		threadID = strings.TrimSpace(threadID)
		if threadID != "" && hop > 0 {
			hops[threadID] = hop
		}
	}
	return residentParticipantSpeech{
		server:        s,
		participantID: strings.TrimSpace(participantID),
		limiter:       &residentSpeechLimiter{},
		hopByThread:   hops,
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
	case "result", "question", "update", "decline":
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
	targetThreadID, subthreadID, err := r.resolveTargetThread(strings.TrimSpace(targetThreadID))
	if err != nil {
		return tools.PostedMessage{}, err
	}
	// A decline is the silent alternative to a reply; it is not counted against
	// the per-turn post budget (an agent must always be able to decline an
	// addressed message).
	if kind != "decline" {
		if err := r.reservePost(targetThreadID); err != nil {
			return tools.PostedMessage{}, err
		}
	}
	now := time.Now().UTC()
	msg := agentcontrol.ParticipantMessage{
		AgentID:       participantID,
		ParticipantID: participantID,
		Kind:          kind,
		Hop:           r.messageHop(targetThreadID),
		Text:          text,
		// ThreadID tags the message with the reply subthread (cth-*) so
		// publishParticipantMessage folds it into that thread instead of the main
		// stream; empty for ordinary top-level/DM posts.
		ThreadID:  subthreadID,
		CreatedAt: now,
	}
	if err := r.server.publishParticipantMessage(targetThreadID, msg); err != nil {
		return tools.PostedMessage{}, err
	}
	reportThreadID := targetThreadID
	if subthreadID != "" {
		reportThreadID = subthreadID
	}
	return tools.PostedMessage{
		AgentID:       participantID,
		ParticipantID: participantID,
		Kind:          kind,
		ThreadID:      reportThreadID,
		Text:          text,
		CreatedAt:     now,
	}, nil
}

// React stamps a reaction on a specific message (targetThreadID, seq). It is
// the terminal, non-routing half of participant speech: unlike PostMessage it
// never generates an envelope or wakes another agent (2026-07-04-read-receipts-
// and-reactions.md §3 invariant 1) — it only writes the mark and notifies
// clients.
func (r residentParticipantSpeech) React(ctx context.Context, targetThreadID string, seq int, reaction string) error {
	_ = ctx
	if r.server == nil {
		return errors.New("react: app server not configured")
	}
	participantID := strings.TrimSpace(r.participantID)
	if participantID == "" {
		return errors.New("react: participant_id is required")
	}
	if seq <= 0 {
		return errors.New("react: seq is required")
	}
	reaction = strings.TrimSpace(reaction)
	if !tools.IsReactionKey(reaction) {
		return fmt.Errorf("react: unknown reaction %q", reaction)
	}
	// A cth-* target resolves to its parent thread; the reacted-to message's seq
	// lives in that parent thread's history (subthread messages are stored there
	// tagged with thread_id=cth), so React always stamps against the parent.
	targetThreadID, _, err := r.resolveTargetThread(strings.TrimSpace(targetThreadID))
	if err != nil {
		return err
	}
	return r.server.recordParticipantReaction(targetThreadID, seq, participantID, reaction)
}

// resolveTargetThread resolves a post/decline/react target to the top-level
// thread to publish under, plus an optional reply-subthread id (cth-*) to tag
// the message with. A cth-* target resolves to its parent (group) thread for
// routing/storage while returning the subthread id so publishParticipantMessage
// folds the message into the reply thread instead of the main stream. For
// ordinary top-level/DM targets the returned subthreadID is empty.
func (r residentParticipantSpeech) resolveTargetThread(targetThreadID string) (parentThreadID, subthreadID string, err error) {
	if r.server == nil {
		return "", "", errors.New("post_message: app server not configured")
	}
	participantID := strings.TrimSpace(r.participantID)
	if participantID == "" {
		return "", "", errors.New("participant_id is required")
	}
	if targetThreadID == "" {
		th, err := r.server.ensureResidentDMThread(participantID)
		if err != nil {
			return "", "", err
		}
		return th.ID, "", nil
	}
	if strings.HasPrefix(targetThreadID, "cth-") {
		return r.resolveSubthreadTarget(participantID, targetThreadID)
	}
	th, err := r.server.ensureResidentThread(targetThreadID)
	if err != nil {
		return "", "", err
	}
	th.mu.Lock()
	isOwnDM := strings.TrimSpace(th.DMParticipantID) == participantID
	th.mu.Unlock()
	if isOwnDM {
		return th.ID, "", nil
	}
	// #all no longer bypasses the membership check — non-members must
	// add_group_member first (chat-style-threads-design.md §3.2).
	members, err := session.ListThreadMembers(r.server.rt.SessionDir, th.ID)
	if err != nil {
		return "", "", err
	}
	for _, memberID := range members {
		if strings.TrimSpace(memberID) == participantID {
			return th.ID, "", nil
		}
	}
	return "", "", fmt.Errorf("post_message: participant %q is not a member of thread %q; ask the user to add you to the group first", participantID, th.ID)
}

// resolveSubthreadTarget resolves a reply subthread id (cth-*) to its parent
// group thread. Membership is checked against the parent group thread's members
// (the weak-isolation participant subset stored per-cth is seeded/enforced by
// the subthread router in a later change; a group member may post into any of
// that group's reply subthreads today).
func (r residentParticipantSpeech) resolveSubthreadTarget(participantID, subthreadID string) (string, string, error) {
	cth, err := session.FindConversationThreadByID(r.server.rt.SessionDir, subthreadID)
	if err != nil {
		return "", "", fmt.Errorf("post_message: %w", err)
	}
	parentThreadID := strings.TrimSpace(cth.SessionID)
	if parentThreadID == "" {
		return "", "", fmt.Errorf("post_message: subthread %q has no parent thread", subthreadID)
	}
	th, err := r.server.ensureResidentThread(parentThreadID)
	if err != nil {
		return "", "", err
	}
	th.mu.Lock()
	isOwnDM := strings.TrimSpace(th.DMParticipantID) == participantID
	th.mu.Unlock()
	if isOwnDM {
		return th.ID, cth.ID, nil
	}
	members, err := session.ListThreadMembers(r.server.rt.SessionDir, th.ID)
	if err != nil {
		return "", "", err
	}
	for _, memberID := range members {
		if strings.TrimSpace(memberID) == participantID {
			return th.ID, cth.ID, nil
		}
	}
	return "", "", fmt.Errorf("post_message: participant %q is not a member of thread %q; ask the user to add you to the group first", participantID, th.ID)
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

func (r residentParticipantSpeech) messageHop(targetThreadID string) int {
	targetThreadID = strings.TrimSpace(targetThreadID)
	if targetThreadID != "" {
		if hop := r.hopByThread[targetThreadID]; hop > 0 {
			return hop
		}
	}
	return 1
}
