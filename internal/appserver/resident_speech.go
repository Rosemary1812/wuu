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
	// seenSeqByThread records, per source thread, the highest message seq this
	// turn's envelope batch carried — i.e. the version of the room the agent
	// read before composing. The freshness check (held draft) compares it to
	// the thread's current tail so a reply written against a now-stale room is
	// held instead of posted blind.
	seenSeqByThread map[string]int
}

type residentSpeechLimiter struct {
	mu            sync.Mutex
	postsByThread map[string]int
}

func (s *Server) residentParticipantSpeech(participantID string) tools.ParticipantSpeech {
	return s.residentParticipantSpeechForTurn(participantID, nil, nil)
}

func (s *Server) residentParticipantSpeechForTurn(participantID string, hopByThread, seenSeqByThread map[string]int) tools.ParticipantSpeech {
	hops := make(map[string]int, len(hopByThread))
	for threadID, hop := range hopByThread {
		threadID = strings.TrimSpace(threadID)
		if threadID != "" && hop > 0 {
			hops[threadID] = hop
		}
	}
	seen := make(map[string]int, len(seenSeqByThread))
	for threadID, seq := range seenSeqByThread {
		threadID = strings.TrimSpace(threadID)
		if threadID != "" && seq > 0 {
			seen[threadID] = seq
		}
	}
	return residentParticipantSpeech{
		server:          s,
		participantID:   strings.TrimSpace(participantID),
		limiter:         &residentSpeechLimiter{},
		hopByThread:     hops,
		seenSeqByThread: seen,
	}
}

func (r residentParticipantSpeech) PostMessage(ctx context.Context, kind, text, targetThreadID string, force bool) (tools.PostedMessage, error) {
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
	// Freshness check (held draft): if the agent read this thread this turn
	// (we have a seen-seq for it) and it has moved since — a teammate or the
	// user posted while the agent was composing — hold the draft instead of
	// posting it blind. A decline needs no check (it is already the silent
	// choice), and force skips it (the agent has decided to send anyway).
	if kind != "decline" && !force {
		if note, held := r.roomMovedSince(targetThreadID, subthreadID); held {
			return tools.PostedMessage{Held: true, HeldNote: note}, nil
		}
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

// roomMovedSince reports whether the target scope gained new visible messages
// (from someone other than this agent) since the agent read it this turn. It
// keys on the seen-seq the turn's envelope batch carried: no seen-seq for the
// thread means the agent is initiating rather than replying, so a fresh post is
// never held. Scope matters — a main-stream post is only stale if new main
// messages arrived; a reply-subthread post only if new messages in that same
// cth arrived. The returned note lists what arrived, for the agent to weigh.
func (r residentParticipantSpeech) roomMovedSince(parentThreadID, subthreadID string) (string, bool) {
	seen, ok := r.seenSeqByThread[strings.TrimSpace(parentThreadID)]
	if !ok || seen <= 0 {
		return "", false
	}
	records, err := session.LoadHistoryRecords(r.server.rt.SessionDir, parentThreadID, false)
	if err != nil {
		return "", false
	}
	var arrived []string
	for _, rec := range records {
		if rec.Seq <= seen {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(rec.Role))
		if role != "participant" && role != "user" {
			continue
		}
		// A participant record with no post kind is a non-message row (e.g.
		// telemetry meta) — it is not the room speaking.
		if role == "participant" && strings.TrimSpace(rec.PostKind) == "" {
			continue
		}
		if strings.TrimSpace(rec.ThreadID) != strings.TrimSpace(subthreadID) {
			continue
		}
		if strings.TrimSpace(rec.ParticipantID) == r.participantID {
			continue
		}
		who := strings.TrimSpace(rec.ParticipantID)
		if role == "user" || who == "" {
			who = "the user"
		} else if summary, ok := r.server.resolveParticipantSummary(who); ok && strings.TrimSpace(summary.Name) != "" {
			who = summary.Name
		}
		body := firstNonEmpty(strings.TrimSpace(rec.DisplayContent), strings.TrimSpace(rec.Content))
		body = strings.Join(strings.Fields(body), " ")
		if len(body) > 160 {
			body = body[:160] + "…"
		}
		arrived = append(arrived, fmt.Sprintf("- %s: %s", who, body))
	}
	if len(arrived) == 0 {
		return "", false
	}
	return strings.Join(arrived, "\n"), true
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
