package appserver

import (
	"context"
	"encoding/json"
	"math/rand"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/workspaces"
)

// recordEnvelopeReadReceipts writes/advances the read-receipt status for the
// source messages a resident is processing (2026-07-04-read-receipts-and-
// reactions.md §3/§5). Each source message is marked seen against its own
// group thread (env.SourceThreadID / SourceSeq) by the resident participant:
// in_progress when the turn starts consuming the batch, then completed or
// failed by turn outcome. Envelopes with no source seq (0 — pre-spine or a
// non-chat path) carry no receipt. (thread, seq) is de-duplicated within a
// batch so one coalesced message yields one receipt.
func (s *Server) recordEnvelopeReadReceipts(participantID string, envs []MessageEnvelope, status string, at time.Time) {
	if s == nil || s.rt == nil {
		return
	}
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return
	}
	done := make(map[string]bool, len(envs))
	for _, env := range envs {
		threadID := strings.TrimSpace(env.SourceThreadID)
		if threadID == "" || env.SourceSeq <= 0 {
			continue
		}
		key := threadID + "#" + strconv.Itoa(env.SourceSeq)
		if done[key] {
			continue
		}
		done[key] = true
		// Tag the receipt with the source scope (the cth for reply-subthread
		// traffic, '' for main stream) so a cth read advances only the cth's
		// read cursor, never the main-stream one (T3 per-cth cursor).
		if err := session.MarkMessageSeen(s.rt.SessionDir, threadID, env.SourceSeq, participantID, status, strings.TrimSpace(env.SourceSubthreadID), at); err != nil {
			providers.DebugLogf("record read receipt (%s) for %q on %s#%d: %v", status, participantID, threadID, env.SourceSeq, err)
			continue
		}
		s.notifyMessageSeen(threadID, env.SourceSeq, participantID, status)
	}
}

// recordParticipantReaction persists one participant's reaction on a message
// and notifies clients. It never routes: a reaction is a terminal event, not a
// message (2026-07-04-read-receipts-and-reactions.md §3 invariant 1).
func (s *Server) recordParticipantReaction(threadID string, seq int, participantID, reaction string) error {
	if s == nil || s.rt == nil {
		return nil
	}
	threadID = strings.TrimSpace(threadID)
	participantID = strings.TrimSpace(participantID)
	if err := session.SetMessageReaction(s.rt.SessionDir, threadID, seq, participantID, reaction, time.Now().UTC()); err != nil {
		return err
	}
	s.notifyMessageReaction(threadID, seq, participantID, reaction)
	return nil
}

const (
	residentEnvelopeBatchLimit = 20
	idleUnreadWakeBaseDelay    = 30 * time.Second
	idleUnreadWakeMaxDelay     = 5 * time.Minute
)

type idleUnreadCandidate struct {
	ParticipantID string
	UnreadCount   int
	Busy          bool
	Draining      bool
	Retired       bool
	LastSpeaker   bool
}

func chooseIdleUnreadCandidate(candidates []idleUnreadCandidate, rng *rand.Rand) (idleUnreadCandidate, bool) {
	var best []idleUnreadCandidate
	maxUnread := 0
	for _, c := range candidates {
		if strings.TrimSpace(c.ParticipantID) == "" || c.UnreadCount <= 0 || c.Busy || c.Draining || c.Retired || c.LastSpeaker {
			continue
		}
		if c.UnreadCount > maxUnread {
			maxUnread = c.UnreadCount
			best = best[:0]
		}
		if c.UnreadCount == maxUnread {
			best = append(best, c)
		}
	}
	if len(best) == 0 {
		return idleUnreadCandidate{}, false
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return best[rng.Intn(len(best))], true
}

func (s *Server) idleUnreadWakeDelay(wave int) time.Duration {
	if s != nil && s.idleUnreadWakeDelayForTest != nil {
		return s.idleUnreadWakeDelayForTest(wave)
	}
	if wave < 0 {
		wave = 0
	}
	delay := idleUnreadWakeBaseDelay
	for i := 0; i < wave; i++ {
		delay *= 2
		if delay >= idleUnreadWakeMaxDelay {
			return idleUnreadWakeMaxDelay
		}
	}
	return delay
}

func (s *Server) idleUnreadCandidates(threadID, lastSpeakerID string) []idleUnreadCandidate {
	if s == nil || s.rt == nil {
		return nil
	}
	threadID = strings.TrimSpace(threadID)
	lastSpeakerID = strings.TrimSpace(lastSpeakerID)
	if threadID == "" {
		return nil
	}
	members, err := session.ListThreadMembers(s.rt.SessionDir, threadID)
	if err != nil {
		providers.DebugLogf("idle unread wake: list thread members %q: %v", threadID, err)
		return nil
	}
	candidates := make([]idleUnreadCandidate, 0, len(members))
	for _, participantID := range members {
		participantID = strings.TrimSpace(participantID)
		if participantID == "" {
			continue
		}
		candidate := idleUnreadCandidate{
			ParticipantID: participantID,
			Busy:          s.participantIsBusy(participantID),
			Draining:      s.residentDraining(participantID),
			Retired:       s.participantRetired(participantID),
			LastSpeaker:   participantID == lastSpeakerID,
		}
		if candidate.Busy || candidate.Draining || candidate.Retired || candidate.LastSpeaker {
			continue
		}
		watermark, err := session.ThreadReadWatermark(s.rt.SessionDir, threadID, participantID)
		if err != nil {
			providers.DebugLogf("idle unread wake: read watermark for %q in %q: %v", participantID, threadID, err)
			continue
		}
		records, err := session.ChatMessagesSince(s.rt.SessionDir, threadID, watermark)
		if err != nil {
			providers.DebugLogf("idle unread wake: chat messages since %d in %q: %v", watermark, threadID, err)
			continue
		}
		for _, rec := range records {
			if strings.TrimSpace(rec.ThreadID) != "" {
				continue
			}
			if strings.TrimSpace(rec.ParticipantID) == participantID {
				continue
			}
			candidate.UnreadCount++
		}
		if candidate.UnreadCount > 0 {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func (s *Server) scheduleIdleUnreadWake(threadID, lastSpeakerID string) {
	if s == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	lastSpeakerID = strings.TrimSpace(lastSpeakerID)
	if threadID == "" {
		return
	}
	s.idleUnreadWakeMu.Lock()
	defer s.idleUnreadWakeMu.Unlock()
	if s.idleUnreadWakeTimers == nil {
		s.idleUnreadWakeTimers = make(map[string]*time.Timer)
	}
	if s.idleUnreadWakeWaveByThread == nil {
		s.idleUnreadWakeWaveByThread = make(map[string]int)
	}
	if s.idleUnreadWakeLastSpeaker == nil {
		s.idleUnreadWakeLastSpeaker = make(map[string]string)
	}
	if existing := s.idleUnreadWakeTimers[threadID]; existing != nil {
		existing.Stop()
	}
	s.idleUnreadWakeLastSpeaker[threadID] = lastSpeakerID
	delay := s.idleUnreadWakeDelay(s.idleUnreadWakeWaveByThread[threadID])
	s.idleUnreadWakeTimers[threadID] = time.AfterFunc(delay, func() {
		s.runIdleUnreadWake(threadID)
	})
}

func (s *Server) resetIdleUnreadWake(threadID string) {
	if s == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	s.idleUnreadWakeMu.Lock()
	defer s.idleUnreadWakeMu.Unlock()
	if timer := s.idleUnreadWakeTimers[threadID]; timer != nil {
		timer.Stop()
	}
	delete(s.idleUnreadWakeTimers, threadID)
	delete(s.idleUnreadWakeWaveByThread, threadID)
	delete(s.idleUnreadWakeLastSpeaker, threadID)
}

func (s *Server) runIdleUnreadWake(threadID string) {
	if s == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	s.idleUnreadWakeMu.Lock()
	lastSpeakerID := s.idleUnreadWakeLastSpeaker[threadID]
	delete(s.idleUnreadWakeTimers, threadID)
	s.idleUnreadWakeMu.Unlock()

	candidates := s.idleUnreadCandidates(threadID, lastSpeakerID)

	s.idleUnreadWakeMu.Lock()
	if s.idleUnreadWakeRand == nil {
		s.idleUnreadWakeRand = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	candidate, ok := chooseIdleUnreadCandidate(candidates, s.idleUnreadWakeRand)
	if !ok {
		delete(s.idleUnreadWakeWaveByThread, threadID)
		delete(s.idleUnreadWakeLastSpeaker, threadID)
		s.idleUnreadWakeMu.Unlock()
		return
	}
	if s.idleUnreadWakeWaveByThread == nil {
		s.idleUnreadWakeWaveByThread = make(map[string]int)
	}
	s.idleUnreadWakeWaveByThread[threadID]++
	s.idleUnreadWakeMu.Unlock()

	s.kickResidentAgent(candidate.ParticipantID)
}

type envelopeMetaRecord struct {
	ID                string `json:"id,omitempty"`
	SourceThreadID    string `json:"source_thread_id"`
	SourceSubthreadID string `json:"source_subthread_id,omitempty"`
	// SourceThreadTitle snapshots the source thread's title at write time so
	// DM/group chat rows can name the source without a reverse lookup at
	// render time (the source thread may since have been renamed or
	// archived). Mirrors MessageEnvelope.SourceTitle.
	SourceThreadTitle   string    `json:"source_thread_title,omitempty"`
	Addressed           bool      `json:"addressed"`
	Hop                 int       `json:"hop"`
	SourceSeq           int       `json:"source_seq,omitempty"`
	SenderParticipantID string    `json:"sender_participant_id,omitempty"`
	CreatedAt           time.Time `json:"created_at,omitempty"`
}

func (s *Server) prepareThreadMentions(th *threadState, mentions []string) (map[string]bool, error) {
	mentioned := make(map[string]bool)
	if s == nil || s.rt == nil || th == nil {
		return mentioned, nil
	}
	th.mu.Lock()
	threadID := th.ID
	isDM := strings.TrimSpace(th.DMParticipantID) != ""
	isAllChannel := isAllChannelThread(th.Group, th.Title)
	th.mu.Unlock()
	if isDM {
		return mentioned, nil
	}
	for _, id := range mentions {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if mentioned[id] {
			continue
		}
		if isAllChannel {
			// #all membership changes only through the explicit member
			// tools. A mention can address an existing member, but it must
			// not silently subscribe a named agent to the public room.
			mentioned[id] = true
			continue
		}
		if err := session.AddThreadMember(s.rt.SessionDir, threadID, id); err != nil {
			return nil, err
		}
		mentioned[id] = true
	}
	return mentioned, nil
}

func (s *Server) routeUserMessageToResidents(source *threadState, msg providers.ChatMessage, mentioned map[string]bool, sourceSeq int) {
	if source == nil || msg.Hidden || strings.TrimSpace(chatMessageDisplayContent(msg)) == "" {
		return
	}
	source.mu.Lock()
	if strings.TrimSpace(source.DMParticipantID) != "" {
		source.mu.Unlock()
		return
	}
	env := MessageEnvelope{
		SourceThreadID: source.ID,
		SourceTitle:    residentEnvelopeSourceTitleLocked(source),
		SenderKind:     "user",
		SenderName:     "User",
		Hop:            0,
		SourceSeq:      sourceSeq,
		Text:           strings.TrimSpace(chatMessageDisplayContent(msg)),
		CreatedAt:      time.Now().UTC(),
		Workspace:      source.FocusWorkspace,
	}
	source.mu.Unlock()
	s.resetIdleUnreadWake(env.SourceThreadID)
	s.routeEnvelopes(source, env, mentioned)
}

func (s *Server) routeParticipantMessageToResidents(source *threadState, msg agentcontrol.ParticipantMessage, displayName string, sourceSeq int) {
	if source == nil || strings.TrimSpace(msg.Text) == "" || strings.EqualFold(strings.TrimSpace(msg.Kind), "decline") {
		return
	}
	source.mu.Lock()
	sourceIsDM := strings.TrimSpace(source.DMParticipantID) != ""
	env := MessageEnvelope{
		SourceThreadID:      source.ID,
		SourceTitle:         residentEnvelopeSourceTitleLocked(source),
		SenderKind:          "participant",
		SenderName:          firstNonEmpty(displayName, msg.TaskName, msg.AgentType, msg.AgentID, "Participant"),
		SenderParticipantID: strings.TrimSpace(msg.ParticipantID),
		Hop:                 msg.Hop,
		SourceSeq:           sourceSeq,
		Text:                strings.TrimSpace(msg.Text),
		CreatedAt:           firstNonZeroTime(msg.CreatedAt, time.Now().UTC()),
		Workspace:           source.FocusWorkspace,
	}
	source.mu.Unlock()
	if env.Hop <= 0 {
		env.Hop = 1
	}
	mentioned := s.mentionedMembersByName(source, env.Text)
	s.routeEnvelopes(source, env, mentioned)
	if !sourceIsDM {
		s.scheduleIdleUnreadWake(env.SourceThreadID, env.SenderParticipantID)
	}
}

func (s *Server) routeEnvelopes(source *threadState, base MessageEnvelope, mentioned map[string]bool) {
	if s == nil || s.rt == nil || source == nil {
		return
	}
	source.mu.Lock()
	sourceThreadID := strings.TrimSpace(source.ID)
	source.mu.Unlock()
	if sourceThreadID == "" {
		return
	}
	// Members come from explicit thread_members rows for every thread
	// (including #all now that its membership is explicit rather than
	// mirroring the named roster — chat-style-threads-design.md §3.2).
	members, err := session.ListThreadMembers(s.rt.SessionDir, sourceThreadID)
	if err != nil {
		providers.DebugLogf("list thread members for resident routing %q: %v", sourceThreadID, err)
		return
	}
	// Persist who this message @-addresses so the pull inbox can rebuild the
	// addressed flag (the message lives once in history; addressed is a durable
	// side record, not carried per-member).
	if base.SourceSeq > 0 {
		// routeEnvelopes fans out the main stream, so mentions are tagged with
		// the main scope (''); a reply-subthread mention would carry its cth tag.
		for pid := range mentioned {
			if err := session.MarkMessageMention(s.rt.SessionDir, sourceThreadID, base.SourceSeq, pid, strings.TrimSpace(base.SourceSubthreadID), base.CreatedAt); err != nil {
				providers.DebugLogf("record mention %q on %s#%d: %v", pid, sourceThreadID, base.SourceSeq, err)
			}
		}
	}
	// Main-stream chat is pull: the message is already in history, members are
	// only kicked and drain what is past their read cursor.
	s.deliverEnvelopeToMembers(members, base, mentioned, false)
}

// deliverEnvelopeToMembers delivers a message to a member set. It decides two
// things per member, independently: whether to PUSH a durable envelope copy,
// and whether to WAKE (kick) the member now.
//
// PUSH (enqueue a durable envelope): used when the message must reach the member
// directly rather than as ambient history — pushToInbox=true (content that is
// not plain main-stream history: reply-subthread cth traffic and system/handoff
// directives), an @mention (an addressed must-answer), or a sole-member group
// (a DM in disguise). Ambient main-stream chat is NOT pushed: it lives once in
// history and members pull what is past their read cursor.
//
// WAKE gating (讨论层的核心): a normal ambient message from another AGENT does
// NOT wake the other members immediately. Today's models answer the instant
// "new message" enters their context, so auto-waking every agent on every peer
// post produces a thundering herd (the reason group counting/debate degraded).
// Instead an ambient agent post becomes unread in the ledger; after the room is
// quiet, idle-unread wake may kick one eligible reader to pull normal history.
// A member is woken immediately only when: it is pushed to (an @mention, a
// handoff, a system directive, or a sole-member DM), OR the sender is the HUMAN
// (a person addressing the room is a genuine call to answer — all members wake
// and draft in parallel).
func (s *Server) deliverEnvelopeToMembers(members []string, base MessageEnvelope, mentioned map[string]bool, pushToInbox bool) {
	if s == nil || s.rt == nil {
		return
	}
	senderID := strings.TrimSpace(base.SenderParticipantID)
	senderIsHuman := strings.EqualFold(strings.TrimSpace(base.SenderKind), "user")
	// A group with a single member has no ambient contention: its one agent
	// gets every message pushed directly, like a DM, never through the pull
	// path (per design: 单人群不走拉/书签).
	soleMember := len(members) == 1
	for _, participantID := range members {
		participantID = strings.TrimSpace(participantID)
		if participantID == "" || participantID == senderID {
			continue
		}
		// Membership queries already exclude retired participants; this
		// guards the race where a member retires between the membership
		// read and the kick (retire cleanup protocol).
		if s.participantRetired(participantID) {
			continue
		}
		pushed := pushToInbox || mentioned[participantID] || soleMember
		if pushed {
			env := base
			env.ID = "env-" + session.NewID()
			// A sole-member group is a DM in disguise: its one agent is the
			// user's only interlocutor, so a main-stream post there is a
			// must-answer (addressed), exactly like a DM. cth/system directives
			// (pushToInbox) keep their own addressed semantics — a system wake
			// addresses nobody even in a one-member group.
			env.Addressed = mentioned[participantID] || (soleMember && !pushToInbox)
			if env.CreatedAt.IsZero() {
				env.CreatedAt = time.Now().UTC()
			}
			data, err := json.Marshal(env)
			if err != nil {
				providers.DebugLogf("marshal resident envelope for %q: %v", participantID, err)
				continue
			}
			if _, err := session.EnqueueResidentEnvelope(s.rt.SessionDir, session.ResidentEnvelope{
				ID:            env.ID,
				ParticipantID: participantID,
				EnvelopeJSON:  data,
				CreatedAt:     env.CreatedAt,
			}); err != nil {
				providers.DebugLogf("enqueue resident envelope for %q: %v", participantID, err)
				continue
			}
		}
		// Wake only on a directed push or a human addressing the room. An
		// ambient agent post leaves the member unread-but-not-woken.
		if pushed || senderIsHuman {
			s.kickResidentAgent(participantID)
		}
	}
}

// routeSubthreadParticipantMessage is the weak-isolation fan-out for a reply
// subthread (cth-*) message. Where routeParticipantMessageToResidents pushes a
// main-stream post to every thread member, this pushes a cth post ONLY to that
// subthread's participant subset (conversation_thread_members) — non-participants
// are never woken and never take the message into context. It runs off the
// publishParticipantMessage short-circuit (the message is already stored in the
// parent thread's history, tagged with thread_id=cth); this adds the routing the
// short-circuit used to drop on the floor.
//
// Two membership side effects keep the subset current: the sender auto-joins
// (an agent that speaks in a reply is a participant of it), and @mentioned group
// members are pulled in (被@者加入). Pull access (fetch_thread_messages) stays
// open to the whole group and is unaffected by this subset.
func (s *Server) routeSubthreadParticipantMessage(parentThreadID, subthreadID string, msg agentcontrol.ParticipantMessage, displayName string, sourceSeq int) {
	if s == nil || s.rt == nil {
		return
	}
	parentThreadID = strings.TrimSpace(parentThreadID)
	subthreadID = strings.TrimSpace(subthreadID)
	if parentThreadID == "" || subthreadID == "" {
		return
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" || strings.EqualFold(strings.TrimSpace(msg.Kind), "decline") {
		return
	}
	senderID := strings.TrimSpace(msg.ParticipantID)
	if senderID != "" {
		// Best-effort: a non-named sender (e.g. a worker/subagent posting a
		// task_card update) fails requireActiveNamedParticipant and is simply
		// not added — its cth then has no member subset and wakes no one, which
		// preserves the existing task_card folding behaviour.
		if err := session.AddConversationThreadMember(s.rt.SessionDir, subthreadID, senderID); err != nil {
			providers.DebugLogf("auto-join sender %q into subthread %q: %v", senderID, subthreadID, err)
		}
	}
	mentioned := s.mentionSubthreadMembers(parentThreadID, subthreadID, text)
	title, workspace := parentThreadID, ""
	if th := s.thread(parentThreadID); th != nil {
		th.mu.Lock()
		title = residentEnvelopeSourceTitleLocked(th)
		workspace = th.FocusWorkspace
		th.mu.Unlock()
	}
	hop := msg.Hop
	if hop <= 0 {
		hop = 1
	}
	base := MessageEnvelope{
		SourceThreadID:      parentThreadID,
		SourceSubthreadID:   subthreadID,
		SourceTitle:         title,
		SenderKind:          "participant",
		SenderName:          firstNonEmpty(displayName, msg.TaskName, msg.AgentType, msg.AgentID, "Participant"),
		SenderParticipantID: senderID,
		Hop:                 hop,
		SourceSeq:           sourceSeq,
		Text:                text,
		CreatedAt:           firstNonZeroTime(msg.CreatedAt, time.Now().UTC()),
		Workspace:           workspace,
	}
	members, err := session.ListConversationThreadMembers(s.rt.SessionDir, subthreadID)
	if err != nil {
		providers.DebugLogf("list subthread members for %q: %v", subthreadID, err)
		return
	}
	// Reply-subthread (cth) traffic stays on the weak-isolation push path: a
	// post pushes+wakes the reply's participant subset. Task threads are the
	// exception. Under the task rail's two-channel contract (plan §T7, red line
	// §4.5) every plain participant post inside a task — progress updates,
	// results, questions to the room — is PUBLIC PROGRESS for the human to read
	// and must NOT wake the working teammates. The only things that wake a node
	// are the engine's plan-advance wake (a node completing — via piece_done or at
	// turn end — dispatches the next), an @mention, or a system directive. A
	// public post still lands in history and refreshes the
	// task panel (notifySubthreadUpdated, fired by the caller), and an explicit
	// @mention inside it still wakes the mentioned member (the mentioned set
	// forces a push even off this path). This is a mechanical status check, not a
	// semantic judgement — a discussion reply (status open) keeps waking its
	// participants on every post, so the weak-isolation contract is unchanged
	// there.
	pushToInbox := true
	if thread, findErr := session.FindConversationThreadByID(s.rt.SessionDir, subthreadID); findErr == nil && thread.Status == session.ConversationThreadTask {
		pushToInbox = false
		// A public kind=update the poster files into a task thread is
		// user-visible progress (plan §T9) — real headway, not just activity.
		// Refresh the poster's OWN active/retrying node's LastProgressAt so the
		// panel's headway signal advances. Only the poster's own running node
		// moves (a bystander posting here holds no active piece), and it wakes no
		// teammate: pushToInbox=false above already holds the T7 invariant.
		if strings.EqualFold(strings.TrimSpace(msg.Kind), "update") && senderID != "" {
			if piece := activePieceForAssignee(thread.Plan, senderID); piece != nil {
				s.noteNodeProgress(thread, piece.ID, session.TaskEventNodeProgress,
					"public update: "+truncate(text, 80))
			}
		}
	}
	s.deliverEnvelopeToMembers(members, base, mentioned, pushToInbox)
}

// mentionSubthreadMembers resolves @mentions in a reply-subthread message to
// group members and pulls each matched member into the subthread's subset
// (被@者加入), returning the set to mark as addressed. Mentions are matched
// against the parent group's roster (a reply can @ anyone in the group), but the
// membership side effect lands on the cth, not the group — the whole point of
// weak isolation is that being @'d in a reply joins the reply, not the room.
func (s *Server) mentionSubthreadMembers(parentThreadID, subthreadID, text string) map[string]bool {
	out := make(map[string]bool)
	if s == nil || s.rt == nil || strings.TrimSpace(text) == "" {
		return out
	}
	members, err := session.ListThreadMembers(s.rt.SessionDir, parentThreadID)
	if err != nil {
		return out
	}
	for _, participantID := range members {
		participantID = strings.TrimSpace(participantID)
		if participantID == "" {
			continue
		}
		summary, ok := s.resolveParticipantSummary(participantID)
		if !ok || strings.TrimSpace(summary.Name) == "" {
			continue
		}
		if !textMentionsName(text, summary.Name) {
			continue
		}
		out[participantID] = true
		if err := session.AddConversationThreadMember(s.rt.SessionDir, subthreadID, participantID); err != nil {
			providers.DebugLogf("pull mentioned member %q into subthread %q: %v", participantID, subthreadID, err)
		}
	}
	return out
}

func (s *Server) kickResidentAgent(participantID string) {
	participantID = strings.TrimSpace(participantID)
	if s == nil || participantID == "" {
		return
	}
	if !s.beginResidentDrain(participantID) {
		return
	}
	go s.drainResidentAgent(participantID)
}

func (s *Server) beginResidentDrain(participantID string) bool {
	s.residentDrainMu.Lock()
	defer s.residentDrainMu.Unlock()
	if s.drainingResidentAgent == nil {
		s.drainingResidentAgent = make(map[string]bool)
	}
	if s.drainingResidentAgent[participantID] {
		return false
	}
	s.drainingResidentAgent[participantID] = true
	return true
}

func (s *Server) finishResidentDrain(participantID string) {
	s.residentDrainMu.Lock()
	defer s.residentDrainMu.Unlock()
	delete(s.drainingResidentAgent, participantID)
}

// residentDraining reports whether a chat drain is currently in flight for a
// participant. participant/start consults it so a named agent can't be pulled
// into a task while it is mid chat-reply (decision-five: one caller at a time).
func (s *Server) residentDraining(participantID string) bool {
	participantID = strings.TrimSpace(participantID)
	if s == nil || participantID == "" {
		return false
	}
	s.residentDrainMu.Lock()
	defer s.residentDrainMu.Unlock()
	return s.drainingResidentAgent[participantID]
}

func (s *Server) drainResidentAgent(participantID string) {
	defer s.finishResidentDrain(participantID)
	if s == nil || s.rt == nil {
		return
	}
	// A retired resident never drains again: its pending envelopes were
	// dropped by the retire transaction and its DM thread is frozen. A
	// stale kick must not resurrect a DM runtime for it.
	if s.participantRetired(participantID) {
		providers.DebugLogf("skip resident drain for retired participant %q", participantID)
		return
	}
	th, err := s.ensureResidentDMThread(participantID)
	if err != nil {
		providers.DebugLogf("ensure resident dm thread for %q: %v", participantID, err)
		return
	}
	th.mu.Lock()
	running := th.running
	th.mu.Unlock()
	if running {
		return
	}
	// Decision-five concurrency lock: a named agent busy executing a
	// task run does not drain its chat inbox concurrently. The
	// envelopes stay pending and are re-kicked when the run completes
	// (forwardAgentNotifications), so an @-mention arriving mid-task queues
	// instead of racing the same resident agent.
	if s.participantIsBusy(participantID) {
		providers.DebugLogf("defer resident drain for busy participant %q", participantID)
		return
	}
	// Push half first: content delivered as a durable per-member envelope —
	// reply-subthread (cth) traffic, system directives, @mentions, and
	// sole-member group posts. Consumed by id. Record which (thread, seq) each
	// carries so the pull half does not also surface the same message.
	pending, err := session.PendingResidentEnvelopes(s.rt.SessionDir, participantID, residentEnvelopeBatchLimit)
	if err != nil {
		providers.DebugLogf("load resident inbox for %q: %v", participantID, err)
		return
	}
	inbox := make([]MessageEnvelope, 0, len(pending))
	ids := make([]string, 0, len(pending))
	pushed := make(map[string]bool, len(pending))
	for _, raw := range pending {
		var env MessageEnvelope
		if err := json.Unmarshal(raw.EnvelopeJSON, &env); err != nil {
			providers.DebugLogf("decode resident envelope %q for %q: %v", raw.ID, participantID, err)
			continue
		}
		if strings.TrimSpace(env.ID) == "" {
			env.ID = raw.ID
		}
		inbox = append(inbox, env)
		ids = append(ids, raw.ID)
		if env.SourceSeq > 0 && strings.TrimSpace(env.SourceThreadID) != "" {
			pushed[env.SourceThreadID+"#"+strconv.Itoa(env.SourceSeq)] = true
		}
	}
	// Pull half: multi-agent ambient main-stream chat past this resident's read
	// cursor, minus anything already delivered via push this batch (an @mention
	// or sole-member post rides push; it must not double-deliver here). Pulled
	// envelopes have no inbox row — they are acked by advancing the read cursor
	// (recordEnvelopeReadReceipts below), not dequeued.
	envs := make([]MessageEnvelope, 0, len(inbox))
	for _, env := range s.pullResidentChatEnvelopes(participantID, th.ID) {
		if env.SourceSeq > 0 && pushed[env.SourceThreadID+"#"+strconv.Itoa(env.SourceSeq)] {
			continue
		}
		envs = append(envs, env)
	}
	envs = append(envs, inbox...)
	if len(envs) == 0 {
		return
	}
	// One chronological batch: pull (older backlog) and push (the message that
	// woke the agent) are interleaved by arrival time so the model reads them in
	// order rather than pull-then-push.
	sort.SliceStable(envs, func(i, j int) bool { return envs[i].CreatedAt.Before(envs[j].CreatedAt) })
	threadRuntime, err := s.ensureThreadRuntime(th)
	if err != nil {
		providers.DebugLogf("ensure resident runtime for %q: %v", participantID, err)
		return
	}
	if threadRuntime != nil && threadRuntime.Toolkit != nil {
		threadRuntime.Toolkit.SetParticipantSpeech(s.residentParticipantSpeechForTurn(
			participantID, residentSpeechHopsByThread(envs), residentSpeechEngagedThreads(envs),
			s.dispatchedNodesForTurn(participantID, envs)))
		threadRuntime.Toolkit.SetGroupManager(s.residentGroupManager(participantID))
		threadRuntime.Toolkit.SetTaskManager(s.residentTaskManager(participantID))
		s.applyEnvelopeBatchCWD(th, threadRuntime, envs)
	}
	userMsg := residentEnvelopeUserMessage(envs, ids)
	started, ok, err := s.startResidentTurn(context.Background(), th, userMsg, turnRuntimeSnapshot{}, false, turnReadOnlyIgnore)
	if err != nil {
		providers.DebugLogf("start resident envelope turn for %q: %v", participantID, err)
		return
	}
	if !ok {
		return
	}
	// The turn is now consuming this batch: mark each source message
	// in_progress for this resident. Turn outcome (completed/failed) is
	// recorded later in afterResidentTurn.
	s.recordEnvelopeReadReceipts(participantID, envs, session.SeenStatusInProgress, time.Now().UTC())
	if err := s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
		ThreadID: th.ID,
		Turn:     started.turn,
	}); err != nil {
		started.cancel()
		return
	}
	go s.runResidentEnvelopeTurn(started.ctx, th, threadRuntime, started.turnID, started.runtime, started.history, envs)
}

// pullResidentChatEnvelopes is the pull half of delivery: instead of chat being
// pushed into a per-member queue, main-stream chat lives once in each thread's
// history and each agent PULLS what is past its read cursor when it runs. For
// every group thread the participant follows (session.ThreadsForParticipant —
// the reverse thread_members query), this reads the chat past the participant's
// read watermark (session.ChatMessagesSince) and rebuilds envelopes. addressed
// is rebuilt from the durable mention marks (session.MentionedSeqs).
//
// The participant's own DM thread is deliberately NOT a pull source: direct
// user→resident DM turns are handled synchronously (turn_handlers, the
// isResidentDM branch) and never advance the read cursor, so pulling the DM
// would re-process them. dmThreadID is passed only to exclude it defensively if
// it ever appeared as a follow. Main-stream only — reply-subthread (cth) traffic
// and system directives ride the push path and arrive via the resident inbox.
func (s *Server) pullResidentChatEnvelopes(participantID, dmThreadID string) []MessageEnvelope {
	if s == nil || s.rt == nil {
		return nil
	}
	participantID = strings.TrimSpace(participantID)
	dm := strings.TrimSpace(dmThreadID)
	groups, err := session.ThreadsForParticipant(s.rt.SessionDir, participantID)
	if err != nil {
		providers.DebugLogf("pull: threads for %q: %v", participantID, err)
	}
	var out []MessageEnvelope
	for _, threadID := range groups {
		threadID = strings.TrimSpace(threadID)
		if threadID == "" || threadID == dm {
			continue
		}
		watermark, err := session.ThreadReadWatermark(s.rt.SessionDir, threadID, participantID)
		if err != nil {
			continue
		}
		recs, err := session.ChatMessagesSince(s.rt.SessionDir, threadID, watermark)
		if err != nil {
			continue
		}
		mentioned, _ := session.MentionedSeqs(s.rt.SessionDir, threadID, participantID)
		title, workspace := s.taskThreadContext(threadID)
		for _, rec := range recs {
			// Main-stream only (cth-tagged rides push), and never surface the
			// agent's own posts back to itself.
			if strings.TrimSpace(rec.ThreadID) != "" || strings.TrimSpace(rec.ParticipantID) == participantID {
				continue
			}
			isUser := strings.EqualFold(strings.TrimSpace(rec.Role), "user")
			senderKind, senderName, senderID, hop := "participant", "", strings.TrimSpace(rec.ParticipantID), 1
			if isUser {
				senderKind, senderName, senderID, hop = "user", "User", "", 0
			} else if summary, ok := s.resolveParticipantSummary(senderID); ok {
				senderName = summary.Name
			}
			out = append(out, MessageEnvelope{
				ID:                  "env-" + session.NewID(),
				SourceThreadID:      threadID,
				SourceTitle:         title,
				SenderKind:          senderKind,
				SenderName:          firstNonEmpty(senderName, "Participant"),
				SenderParticipantID: senderID,
				Addressed:           mentioned[rec.Seq],
				Hop:                 hop,
				SourceSeq:           rec.Seq,
				Text:                strings.TrimSpace(firstNonEmpty(rec.DisplayContent, rec.Content)),
				CreatedAt:           firstNonZeroTime(rec.At, time.Now().UTC()),
				Workspace:           workspace,
			})
		}
	}
	return out
}

func (s *Server) runResidentEnvelopeTurn(ctx context.Context, th *threadState, threadRuntime *runtime.ThreadRuntime, turnID string, turnRuntime turnRuntimeSnapshot, history []providers.ChatMessage, envs []MessageEnvelope) {
	s.runTurnWithRequestContext(ctx, th, threadRuntime, turnID, turnRuntime, history, nil, envs)
}

// applyEnvelopeBatchCWD sets an envelope-drain turn's tool execution root
// from the workspace focus the incoming batch carries
// (2026-07-03-workspace-focus.md "carry source-thread workspace focus on
// envelopes"): if every envelope in the batch agrees on exactly one
// non-home workspace, tools run rooted there for this turn; otherwise
// (no envelope names a workspace, several disagree, or the single shared
// value is home) tools stay at the resident's agent home.
//
// This is deliberately independent of the DM thread's own persisted focus:
// ensureThreadRuntime already applied that (via configureResidentThreadRuntime)
// before this call for the thread's *own* direct-conversation turns, and
// this override replaces it for this specific envelope-driven turn only.
// It never mutates th.FocusWorkspace or the session store, and it never
// injects a focus declaration item — the source threads' focus is not this
// thread's own declared focus, just where this batch of work happens to
// point the resident's tools for the duration of one turn.
func (s *Server) applyEnvelopeBatchCWD(th *threadState, threadRuntime *runtime.ThreadRuntime, envs []MessageEnvelope) {
	if s == nil || s.rt == nil || th == nil || threadRuntime == nil || threadRuntime.Toolkit == nil {
		return
	}
	th.mu.Lock()
	homeRoot := th.CWD
	th.mu.Unlock()
	roster, err := workspaces.List(s.rt.WuuHome)
	if err != nil {
		roster = nil
	}
	threadRuntime.Toolkit.SetRootDir(focusWorkspaceRoot(envelopeBatchWorkspace(envs), homeRoot, roster))
}

// envelopeBatchWorkspace resolves the single workspace focus shared by an
// entire batch of routed envelopes. "" (no override) is returned unless
// every envelope in the batch that names a non-home workspace names the
// same one — ambiguity resolves to "no override" rather than guessing.
func envelopeBatchWorkspace(envs []MessageEnvelope) string {
	seen := make(map[string]bool)
	for _, env := range envs {
		ws := strings.TrimSpace(env.Workspace)
		if ws == "" || ws == focusWorkspaceHome {
			continue
		}
		seen[ws] = true
	}
	if len(seen) != 1 {
		return ""
	}
	for ws := range seen {
		return ws
	}
	return ""
}

func residentEnvelopeUserMessage(envs []MessageEnvelope, consumedIDs []string) providers.ChatMessage {
	return providers.ChatMessage{
		Role:                       "user",
		Content:                    coalesceEnvelopes(envs),
		DisplayContent:             residentEnvelopeDisplayContent(envs),
		EnvelopeMeta:               envelopeMetaJSON(envs),
		ConsumeResidentEnvelopeIDs: append([]string(nil), consumedIDs...),
	}
}

// residentEnvelopeDisplayContent is the UI-facing text of an envelope user
// message: the raw message text ONLY. The source — which group/thread it was
// routed in from — is context, carried structurally in EnvelopeMeta and
// rendered separately by the chat view's envelope notice. It must never be
// baked into the text as an "Incoming message from …:" prefix, which would leak
// internal framing into the transcript (issue: group messages showed as
// "Incoming message from all: …" in a DM). The model still receives the full
// framing via MessageEnvelope.Prompt (Content), independent of this.
func residentEnvelopeDisplayContent(envs []MessageEnvelope) string {
	texts := make([]string, 0, len(envs))
	for _, env := range envs {
		if t := strings.TrimSpace(env.Text); t != "" {
			texts = append(texts, t)
		}
	}
	return strings.Join(texts, "\n\n")
}

func envelopeMetaJSON(envs []MessageEnvelope) json.RawMessage {
	records := make([]envelopeMetaRecord, 0, len(envs))
	for _, env := range envs {
		records = append(records, envelopeMetaRecord{
			ID:                  env.ID,
			SourceThreadID:      env.SourceThreadID,
			SourceSubthreadID:   env.SourceSubthreadID,
			SourceThreadTitle:   env.SourceTitle,
			Addressed:           env.Addressed,
			Hop:                 env.Hop,
			SourceSeq:           env.SourceSeq,
			SenderParticipantID: env.SenderParticipantID,
			CreatedAt:           env.CreatedAt,
		})
	}
	data, err := json.Marshal(records)
	if err != nil {
		return nil
	}
	return data
}

func residentSpeechHopsByThread(envs []MessageEnvelope) map[string]int {
	hops := make(map[string]int)
	for _, env := range envs {
		threadID := strings.TrimSpace(env.SourceThreadID)
		if threadID == "" {
			continue
		}
		nextHop := env.Hop + 1
		if nextHop <= 0 {
			nextHop = 1
		}
		if existing, ok := hops[threadID]; !ok || nextHop < existing {
			hops[threadID] = nextHop
		}
	}
	return hops
}

// residentSpeechEngagedThreads is the set of source threads this envelope batch
// woke the agent from — the threads it is actively replying in this turn. The
// held-draft check applies only to these; the seq it compares against comes
// from the durable read cursor (session.ThreadReadWatermark), not from here.
func residentSpeechEngagedThreads(envs []MessageEnvelope) map[string]bool {
	engaged := make(map[string]bool)
	for _, env := range envs {
		if threadID := strings.TrimSpace(env.SourceThreadID); threadID != "" {
			engaged[threadID] = true
		}
	}
	return engaged
}

func (s *Server) mentionedMembersByName(source *threadState, text string) map[string]bool {
	out := make(map[string]bool)
	if s == nil || s.rt == nil || source == nil || strings.TrimSpace(text) == "" {
		return out
	}
	source.mu.Lock()
	threadID := strings.TrimSpace(source.ID)
	source.mu.Unlock()
	// Members come from explicit thread_members rows for every thread
	// (including #all now that its membership is explicit rather than
	// mirroring the named roster — chat-style-threads-design.md §3.2).
	members, err := session.ListThreadMembers(s.rt.SessionDir, threadID)
	if err != nil {
		return out
	}
	for _, participantID := range members {
		summary, ok := s.resolveParticipantSummary(participantID)
		if !ok || strings.TrimSpace(summary.Name) == "" {
			continue
		}
		if textMentionsName(text, summary.Name) {
			out[participantID] = true
		}
	}
	return out
}

func textMentionsName(text, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.TrimSpace(text) == "" {
		return false
	}
	pattern := `(?i)(^|[^\p{L}\p{N}_])@` + regexp.QuoteMeta(name) + `($|[^\p{L}\p{N}_])`
	return regexp.MustCompile(pattern).FindStringIndex(text) != nil
}

func residentEnvelopeSourceTitleLocked(th *threadState) string {
	if th == nil {
		return "Conversation"
	}
	return firstNonEmpty(th.Title, th.ID, "Conversation")
}

func (s *Server) afterResidentTurn(th *threadState, participantID string, envs []MessageEnvelope, turn Turn, completedAt time.Time) {
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return
	}
	// Resolve the read receipt: the turn that consumed these envelopes either
	// finished (completed) or errored/was interrupted (failed). "Seen" in the
	// product sense is completed; failed is surfaced distinctly so a crashed
	// turn does not read as "read".
	if len(envs) > 0 {
		status := session.SeenStatusCompleted
		if turn.Status != TurnStatusCompleted {
			status = session.SeenStatusFailed
		}
		s.recordEnvelopeReadReceipts(participantID, envs, status, firstNonZeroTime(completedAt, time.Now().UTC()))
	}
	// This turn's per-turn speech record: whether the resident already spoke
	// through the tool channel (a group post lives in the group thread, not this
	// DM turn's Items, so the fallback cannot see it there — this flag is the
	// reliable signal), whether it asked a question (the blocked/waiting signal),
	// and the snapshot of which plan nodes this turn was dispatched to run.
	spoke, askedQuestion := false, false
	var dispatched map[string]map[string]bool
	if v, ok := s.residentTurnSpeech.LoadAndDelete(participantID); ok {
		if lim, ok := v.(*residentSpeechLimiter); ok {
			spoke = lim.didSpeak()
			askedQuestion = lim.askedAQuestion()
			dispatched = lim.dispatchedNodesSnapshot()
		}
	}
	if !spoke {
		s.fallbackDMReplyFromFinalAnswer(th, participantID, envs, turn, completedAt)
	}
	if len(envs) > 0 {
		s.recordUnansweredAddressedEnvelopes(th, participantID, envs, turn, completedAt)
	}
	// React to a plan node whose executing turn failed: retry against budget,
	// or fail the node and wake the lead (plan §T8). No-op unless this batch
	// was a plan dispatch and the turn is a retryable/hard failure.
	s.retryOrFailTaskNodesAfterTurn(participantID, envs, turn)
	// Turn-end completion capture (the flip side of the failure hook): a COMPLETED
	// plan-dispatch turn advances the plan even without piece_done — the node it
	// was dispatched for completes now, unless the agent gave an explicit blocked
	// signal this turn (a kind=question here, or need_human/need_upstream, which
	// move the node/task out of the completing snapshot). No-op unless this batch
	// was a plan dispatch.
	s.autoCompleteTaskNodesAfterTurn(participantID, envs, turn, askedQuestion, dispatched)
	s.kickResidentAgent(participantID)
}

// fallbackDMReplyFromFinalAnswer keeps replies alive with models that ignore
// the post_message contract. The chat view renders tool-posted messages
// only (chat-style-threads-design.md §2); a resident that answers in plain
// assistant text would be invisible. When a completed turn produced a final
// answer but no participant_message (post_message or decline), republish
// the final answer as the resident's message. Models that follow the
// contract never hit this path.
//
// A resident agent has a single "brain": its own DM thread. Turns started
// from a plain user DM carry no envelopes, so the fallback republishes into
// that same DM thread (th.ID), as before. Turns started to process routed
// MessageEnvelopes (group chat mentions, hand-offs from other residents,
// etc.) must instead land back in the envelope's SourceThreadID — otherwise
// the reply silently lands in the resident's own DM, which the group never
// sees. See fallbackReplyTargetThreadID for how the target is picked when
// envelopes are present.
func (s *Server) fallbackDMReplyFromFinalAnswer(th *threadState, participantID string, envs []MessageEnvelope, turn Turn, completedAt time.Time) {
	if s == nil || th == nil || turn.Status != TurnStatusCompleted {
		return
	}
	th.mu.Lock()
	threadID := th.ID
	isOwnDM := strings.TrimSpace(th.DMParticipantID) == participantID
	th.mu.Unlock()
	if !isOwnDM {
		return
	}
	targetThreadID, targetSubthreadID, ok := fallbackReplyTargetThreadID(threadID, envs)
	if !ok {
		return
	}
	finalAnswer := ""
	for _, item := range turn.Items {
		switch item.Type {
		case ThreadItemParticipantMsg:
			// The resident already spoke through the tool channel
			// (post_message anywhere, or decline).
			return
		case ThreadItemAgentMessage:
			if text := strings.TrimSpace(item.Text); text != "" {
				finalAnswer = text
			}
		}
	}
	if finalAnswer == "" {
		return
	}
	msg := agentcontrol.ParticipantMessage{
		AgentID:       participantID,
		ParticipantID: participantID,
		Kind:          "result",
		Hop:           residentSpeechHopsByThread(envs)[targetThreadID],
		Text:          finalAnswer,
		// ThreadID folds the fallback reply back into the same reply subthread
		// the addressed envelopes came from (weak isolation): empty for a
		// main-stream reply, so publishParticipantMessage behaves as before.
		ThreadID:  targetSubthreadID,
		CreatedAt: completedAt,
	}
	if err := s.publishParticipantMessage(targetThreadID, msg); err != nil {
		providers.DebugLogf("fallback DM reply for %s: %v", participantID, err)
	}
}

// fallbackReplyTargetThreadID picks where a fallback reply belongs. With no
// envelopes, this is a plain user DM turn and the reply stays in the
// resident's own DM thread (ownDMThreadID). With envelopes, the turn was
// triggered by routed MessageEnvelope(s), and the reply must go to the
// (deduplicated) SourceThreadID of the ADDRESSED envelopes — the ones the
// participant contract requires an answer to (rule 1). If those disagree on a
// source thread the target is ambiguous, so nothing is sent;
// recordUnansweredAddressedEnvelopes telemetry covers that case.
//
// When NO envelope in the batch is addressed, the fallback deliberately sends
// nothing. The contract makes silence a valid outcome for un-addressed group
// messages ("simply do not post") and treats plain assistant text as a private
// working transcript that "never reaches the user". A model that chose not to
// call post_message here has chosen silence; republishing its plain text would
// leak private reasoning (e.g. "(no reply needed, staying silent)") into the
// channel. So un-addressed batches get no plain-text fallback at all — only the
// tool channel (post_message) can speak for them.
//
// The returned subthreadID is the reply subthread (cth-*) the reply must fold
// back into: non-empty only when every addressed envelope shares one subthread,
// empty for a main-stream reply. If addressed envelopes disagree on subthread
// (some main-stream, some cth, or two different cths) the target is ambiguous
// and nothing is sent — same rule as a disagreeing source thread.
func fallbackReplyTargetThreadID(ownDMThreadID string, envs []MessageEnvelope) (threadID, subthreadID string, ok bool) {
	if len(envs) == 0 {
		return ownDMThreadID, "", true
	}
	candidates := make([]MessageEnvelope, 0, len(envs))
	for _, env := range envs {
		if env.Addressed {
			candidates = append(candidates, env)
		}
	}
	if len(candidates) == 0 {
		return "", "", false
	}
	sourceThreadIDs := make(map[string]bool, len(candidates))
	sourceSubthreadIDs := make(map[string]bool, len(candidates))
	for _, env := range candidates {
		id := strings.TrimSpace(env.SourceThreadID)
		if id == "" {
			continue
		}
		sourceThreadIDs[id] = true
		sourceSubthreadIDs[strings.TrimSpace(env.SourceSubthreadID)] = true
	}
	if len(sourceThreadIDs) != 1 || len(sourceSubthreadIDs) != 1 {
		return "", "", false
	}
	for id := range sourceThreadIDs {
		threadID = id
	}
	for sub := range sourceSubthreadIDs {
		subthreadID = sub
	}
	return threadID, subthreadID, true
}

func (s *Server) recordUnansweredAddressedEnvelopes(th *threadState, participantID string, envs []MessageEnvelope, turn Turn, completedAt time.Time) {
	if s == nil || s.rt == nil || th == nil {
		return
	}
	startedAt := time.Time{}
	if turn.StartedAt != nil {
		startedAt = turn.StartedAt.UTC()
	}
	for _, env := range envs {
		if !env.Addressed {
			continue
		}
		if s.residentParticipantRespondedTo(participantID, env.SourceThreadID, startedAt, completedAt) {
			continue
		}
		payload, err := json.Marshal(map[string]any{
			"participant_id":   participantID,
			"turn_id":          turn.ID,
			"source_thread_id": env.SourceThreadID,
			"envelope_id":      env.ID,
			"recorded_at":      completedAt,
		})
		if err != nil {
			continue
		}
		if err := session.AppendHistoryRecord(s.rt.SessionDir, th.ID, session.HistoryRecord{
			Role:          "meta",
			Content:       "resident_unanswered_addressed",
			ParticipantID: participantID,
			EnvelopeMeta:  payload,
			At:            completedAt,
		}); err != nil {
			providers.DebugLogf("record unanswered resident envelope for %q: %v", participantID, err)
		}
	}
}

func (s *Server) residentParticipantRespondedTo(participantID, sourceThreadID string, startedAt, completedAt time.Time) bool {
	if s == nil || s.rt == nil {
		return false
	}
	records, err := session.LoadHistoryRecords(s.rt.SessionDir, strings.TrimSpace(sourceThreadID), false)
	if err != nil {
		return false
	}
	for _, rec := range records {
		if strings.ToLower(strings.TrimSpace(rec.Role)) != "participant" {
			continue
		}
		if strings.TrimSpace(rec.ParticipantID) != strings.TrimSpace(participantID) {
			continue
		}
		if strings.TrimSpace(rec.PostKind) == "" {
			continue
		}
		if !startedAt.IsZero() && !rec.At.IsZero() && rec.At.Before(startedAt) {
			continue
		}
		if !completedAt.IsZero() && !rec.At.IsZero() && rec.At.After(completedAt.Add(time.Second)) {
			continue
		}
		return true
	}
	return false
}
