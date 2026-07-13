package appserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

type residentParticipantSpeech struct {
	server        *Server
	participantID string
	limiter       *residentSpeechLimiter
	hopByThread   map[string]int
	// engagedThreads is the set of source threads this turn's envelope batch
	// woke the agent from — the threads it is actively replying in. The
	// held-draft freshness check applies only to these (an initiating post in
	// a thread the agent was not woken from is never held). The seq it compares
	// against is the durable read cursor (session.ThreadReadWatermark), not a
	// value carried here — read receipts are the single source of truth for how
	// far the agent has read a thread.
	engagedThreads map[string]bool
}

// A main group post is a coordination signal, not the work product. Details
// belong in the anchored reply Thread, Task, or a linked artifact. The limit is
// deliberately scoped to named-agent posts in the group main stream: DMs and
// reply/task threads remain able to carry complete answers and evidence.
const maxGroupBriefRunes = 280

type residentSpeechLimiter struct {
	mu            sync.Mutex
	postsByThread map[string]int
	// spoke is set once the resident publishes any post_message/decline this
	// turn (to any thread). afterResidentTurn reads it to decide whether the
	// plain-text fallback should fire: a resident that already spoke through the
	// tool channel must not have its trailing assistant text ("no reply needed",
	// wrap-up notes) republished as a second, spam post. The DM turn's own Items
	// cannot answer this — a group post lives in the group thread, not the DM.
	spoke bool
	// askedQuestion is set when the resident posts a kind=question this turn —
	// its explicit "I am blocked, waiting on an answer" signal. Turn-end
	// completion capture reads it: a plan-dispatch turn that asked a question is
	// waiting, not finished, so its node is NOT auto-completed (it stays open and
	// is re-woken when the answer arrives). It is turn-wide on purpose — a
	// question about one node suppresses auto-complete of every node this turn
	// dispatched, the conservative choice (a running node is left running, never
	// frozen).
	askedQuestion bool
	// dispatchedNodes is the per-turn snapshot of the pieces this turn was
	// dispatched to run: for each task the consumed batch dispatched, the
	// assignee's Active/Retrying piece ids, captured before the turn ran and only
	// for tasks in ExecState executing. Turn-end completion capture completes only
	// these (still-unfinished) pieces, so a node a mid-turn action activated for a
	// LATER turn (a piece_done downstream, a need_upstream upstream, a set_plan
	// re-dispatch) is never mistaken for one this turn ran.
	dispatchedNodes map[string]map[string]string // taskID -> pieceID -> attemptID
}

// markSpoke records that the resident published a post this turn (any kind), so
// the plain-text fallback is suppressed. A kind=question additionally sets
// askedQuestion — the blocked/waiting signal turn-end completion capture honors.
func (l *residentSpeechLimiter) markSpoke(kind string) {
	l.mu.Lock()
	l.spoke = true
	if strings.EqualFold(strings.TrimSpace(kind), "question") {
		l.askedQuestion = true
	}
	l.mu.Unlock()
}

func (l *residentSpeechLimiter) didSpeak() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.spoke
}

func (l *residentSpeechLimiter) askedAQuestion() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.askedQuestion
}

func (l *residentSpeechLimiter) dispatchedNodesSnapshot() map[string]map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dispatchedNodes
}

func (s *Server) residentParticipantSpeech(participantID string) tools.ParticipantSpeech {
	return s.residentParticipantSpeechForTurn(participantID, nil, nil, nil)
}

// lockThreadPost acquires the per-thread post-serialization mutex and returns
// its unlock. It makes the held-draft freshness check and the subsequent
// publish one atomic critical section per target thread (see Server.threadPostMu).
func (s *Server) lockThreadPost(threadID string) func() {
	threadID = strings.TrimSpace(threadID)
	if s == nil || threadID == "" {
		return func() {}
	}
	v, _ := s.threadPostMu.LoadOrStore(threadID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *Server) residentParticipantSpeechForTurn(participantID string, hopByThread map[string]int, engagedThreads map[string]bool, dispatchedNodes map[string]map[string]string) tools.ParticipantSpeech {
	hops := make(map[string]int, len(hopByThread))
	for threadID, hop := range hopByThread {
		threadID = strings.TrimSpace(threadID)
		if threadID != "" && hop > 0 {
			hops[threadID] = hop
		}
	}
	engaged := make(map[string]bool, len(engagedThreads))
	for threadID := range engagedThreads {
		if threadID = strings.TrimSpace(threadID); threadID != "" {
			engaged[threadID] = true
		}
	}
	limiter := &residentSpeechLimiter{dispatchedNodes: dispatchedNodes}
	pid := strings.TrimSpace(participantID)
	if s != nil && pid != "" {
		// Stash this turn's limiter so afterResidentTurn can tell whether the
		// resident spoke through the tool channel before deciding the fallback.
		s.residentTurnSpeech.Store(pid, limiter)
	}
	return residentParticipantSpeech{
		server:         s,
		participantID:  pid,
		limiter:        limiter,
		hopByThread:    hops,
		engagedThreads: engaged,
	}
}

func (r residentParticipantSpeech) PostMessage(ctx context.Context, kind, text, targetThreadID string, basisSeq int, force bool) (tools.PostedMessage, error) {
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
	case "brief", "result", "question", "update", "decline":
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
	if subthreadID == "" && utf8.RuneCountInString(text) > maxGroupBriefRunes {
		if meta, found, findErr := session.Find(r.server.rt.SessionDir, targetThreadID); findErr == nil && found && meta.Group {
			return tools.PostedMessage{}, fmt.Errorf(
				"post_message: group main-stream posts are coordination briefs and must be at most %d characters (got %d); keep only the decision, why it matters, and next owner/action, then put details in a reply Thread, Task, or linked artifact",
				maxGroupBriefRunes,
				utf8.RuneCountInString(text),
			)
		}
	}
	// Serialize the freshness-check-then-publish critical section per target
	// thread. The staleness read and the seq-assigning append are two steps;
	// without this lock, residents racing to post the same thing all pass the
	// read before any commits and then all publish duplicates. Under the lock,
	// the first racer commits and every later racer's roomMovedSince sees it and
	// holds. Scoped to the parent thread (targetThreadID), which owns the seq
	// space for both main-stream and cth posts.
	unlockPost := r.server.lockThreadPost(targetThreadID)
	defer unlockPost()
	// Basis: the seq the agent generated this post against. Explicit basis_seq
	// wins; 0 falls back to the agent's durable read cursor (its "latest seen").
	// The cursor is scoped to where the post lands: a cth post falls back to the
	// cth's own read watermark, a main-stream post to the main watermark, so a
	// cth draft is measured for staleness only against new cth messages and a
	// main draft only against new main messages (T3 per-cth basis isolation).
	basis := basisSeq
	if basis <= 0 {
		var seen int
		var err error
		if subthreadID != "" {
			seen, err = session.ThreadReadWatermarkScoped(r.server.rt.SessionDir, targetThreadID, participantID, subthreadID)
		} else {
			seen, err = session.ThreadReadWatermark(r.server.rt.SessionDir, targetThreadID, participantID)
		}
		if err == nil {
			basis = seen
		}
	}
	// Freshness check (held draft / rebase trigger): if the thread has moved
	// past this basis — a teammate or the user posted while the agent was
	// composing — hold the draft instead of posting it blind (core constraint:
	// a message generated against an old environment must not land). The agent
	// then re-reasons against what arrived (rebase, folded via inception) and
	// posts again with an updated basis. A decline needs no check (it is already
	// the silent choice); force skips it (the agent decided to send anyway).
	if kind != "decline" && !force {
		if note, held := r.roomMovedSince(targetThreadID, subthreadID, basis); held {
			return tools.PostedMessage{Held: true, HeldNote: note, BasisSeq: basis}, nil
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
		BasisSeq:  basis,
		CreatedAt: now,
	}
	if err := r.server.publishParticipantMessage(targetThreadID, msg); err != nil {
		return tools.PostedMessage{}, err
	}
	// The resident has now spoken through the tool channel this turn — suppress
	// the plain-text fallback so trailing wrap-up text is not republished. A
	// kind=question also records the blocked/waiting signal (askedQuestion) that
	// turn-end completion capture reads.
	if r.limiter != nil {
		r.limiter.markSpoke(kind)
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
		BasisSeq:      basis,
		CreatedAt:     now,
	}, nil
}

// roomMovedSince reports whether the target scope gained new visible messages
// (from someone other than this agent) after the given basis seq — the seq the
// agent generated its post against. basis<=0 means the agent has no basis in
// this thread (it is initiating, not replying to a room it read), so a fresh
// post is never held. Scope matters — a main-stream post is only stale if new
// main messages arrived; a reply-subthread post only if new messages in that
// same cth arrived. The returned note lists what arrived, for the agent to
// re-reason against (rebase).
func (r residentParticipantSpeech) roomMovedSince(parentThreadID, subthreadID string, basis int) (string, bool) {
	parentThreadID = strings.TrimSpace(parentThreadID)
	if !r.engagedThreads[parentThreadID] {
		// The agent is initiating in a thread it was not woken from this turn,
		// not replying to a room it read — nothing to be stale against.
		return "", false
	}
	if basis <= 0 {
		return "", false
	}
	// The chat messages past the agent's basis (same read as the pull inbox).
	records, err := session.ChatMessagesSince(r.server.rt.SessionDir, parentThreadID, basis)
	if err != nil {
		return "", false
	}
	subthreadID = strings.TrimSpace(subthreadID)
	var arrived []string
	for _, rec := range records {
		// Scope: a main-stream draft is stale only against new main messages; a
		// reply-subthread draft only against new messages in that same cth.
		if strings.TrimSpace(rec.ThreadID) != subthreadID {
			continue
		}
		if strings.TrimSpace(rec.ParticipantID) == r.participantID {
			continue
		}
		who := strings.TrimSpace(rec.ParticipantID)
		if strings.EqualFold(strings.TrimSpace(rec.Role), "user") || who == "" {
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
	const maxPostsPerThread = 1
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
		return fmt.Errorf("post_message: participant %q already posted to thread %q this turn; continue with non-message coordination tools or wait for the next turn", strings.TrimSpace(r.participantID), targetThreadID)
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
