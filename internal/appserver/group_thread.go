package appserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

// allChannelTitle is the reserved title of the default group channel that
// the server keeps present at all times (chat-style-threads-design.md §3.2).
// Its membership is implicit: every named participant in the roster, no
// thread_members rows required.
const allChannelTitle = "all"

// isAllChannelThread reports whether a group thread is the #all channel by
// its group flag and title. Title matching is case-insensitive so a thread
// created as "All" or "ALL" still resolves to the singleton channel.
func isAllChannelThread(group bool, title string) bool {
	return group && strings.EqualFold(strings.TrimSpace(title), allChannelTitle)
}

// allNamedParticipantIDs returns every active named participant's ID — the
// implicit membership roster for the #all channel.
func (s *Server) allNamedParticipantIDs() ([]string, error) {
	if s == nil || s.rt == nil {
		return nil, nil
	}
	people, err := session.ListParticipants(s.rt.SessionDir, participant.KindNamed)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(people))
	for _, p := range people {
		ids = append(ids, p.ID)
	}
	return ids, nil
}

// ensureAllChannel idempotently creates the "#all" group thread on demand.
// Safe to call repeatedly and concurrently: allChannelMu serializes creation
// so two callers cannot race into creating duplicate channels. Returns the
// existing channel's thread ID if one is already present. A workspace with
// no named participants yet has nobody to populate the channel with, so
// creation is deferred until the roster is non-empty — this keeps
// thread/list's result set stable for workspaces that never seed any named
// agents, and matches the design's "created on demand" phrasing.
func (s *Server) ensureAllChannel() (string, error) {
	if s == nil || s.rt == nil {
		return "", errors.New("runtime session is required")
	}
	s.allChannelMu.Lock()
	defer s.allChannelMu.Unlock()

	sessions, err := session.List(s.rt.SessionDir, 0)
	if err != nil {
		return "", err
	}
	for _, sess := range sessions {
		if sess.ArchivedAt != nil {
			continue
		}
		if isAllChannelThread(sess.Group, sess.Title) {
			return sess.ID, nil
		}
	}

	roster, err := session.ListParticipants(s.rt.SessionDir, participant.KindNamed)
	if err != nil {
		return "", err
	}
	if len(roster) == 0 {
		return "", nil
	}

	th, err := s.createGroupThreadState(session.NewID(), allChannelTitle, time.Now().UTC())
	if err != nil {
		return "", err
	}
	th = s.addResidentThread(th)

	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	if err := s.notifyThreadStarted(thread); err != nil {
		return "", err
	}
	return th.ID, nil
}

// createGroupThreadState persists a new group-thread session and builds its
// in-memory threadState. Mirrors createResidentDMThreadState's shape for a
// group channel instead of a resident DM.
func (s *Server) createGroupThreadState(id, title string, now time.Time) (*threadState, error) {
	if strings.TrimSpace(id) == "" {
		id = session.NewID()
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Group"
	}
	// Group channels are matched by the group bypass, never by workspace id.
	if _, err := session.CreateWithMetadata(s.rt.SessionDir, id, s.rt.RootDir); err != nil {
		return nil, err
	}
	if _, err := session.SetGroupThread(s.rt.SessionDir, id, true); err != nil {
		return nil, err
	}
	if _, err := session.UpdateTitle(s.rt.SessionDir, id, title); err != nil {
		return nil, err
	}
	history := make([]providers.ChatMessage, 0, 1)
	if prompt := strings.TrimSpace(s.rt.StreamRunner.SystemPrompt); prompt != "" {
		history = append(history, providers.ChatMessage{Role: "system", Content: prompt})
	}
	th := newThreadState(id, history, s.rt.ProviderName, s.rt.Model, s.rt.RootDir, true, now)
	th.Group = true
	th.Title = title
	return th, nil
}

// handleGroupTurnStart is turn/start's terminal branch for group threads
// (chat-style-threads-design.md §3.3): group threads have no primary agent.
// The user message is recorded as a completed turn (no provider call) and
// immediately fanned out to members as envelopes; mentioned members are
// marked addressed. Reuses the existing envelope routing machinery
// (routeUserMessageToResidents / routeEnvelopes) so member resident agents
// wake up exactly as they do for any other thread's user turn.
//
// Workspace focus (2026-07-03-workspace-focus.md §3, §6) is handled here
// too: group threads share the DM branch's idempotent-declare helper
// (applyTurnWorkspaceFocus is deliberately thread-kind agnostic). A group
// thread never runs a model turn, but the declaration item is still
// persisted into its history as a completed synthetic turn ahead of the
// caller's message, exactly like a DM — the chat view renders it as a
// divider for every member reading the group transcript.
func (s *Server) handleGroupTurnStart(req Request, th *threadState, params TurnStartParams, images []providers.InputImage, files []providers.InputFile) error {
	th.mu.Lock()
	turnRunning := th.running
	th.mu.Unlock()
	if params.FocusWorkspace != nil {
		if turnRunning {
			return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q already has a running turn", th.ID))
		}
		if err := s.applyTurnWorkspaceFocus(th, params.FocusWorkspace); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}

	mentioned, err := s.prepareThreadMentions(th, params.Mentions)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	userMsg := userMessageFromPrompt(params.Prompt, images, files)
	if strings.TrimSpace(userMsg.Role) == "" {
		userMsg.Role = "user"
	}
	if !chatMessageHasUserPayload(userMsg) {
		return s.writeResponse(req.ID, nil, errors.New("prompt or attachment is required"))
	}

	now := time.Now().UTC()
	th.mu.Lock()
	if th.ReadOnly {
		th.mu.Unlock()
		return s.writeResponse(req.ID, nil, errors.New("thread is read-only"))
	}
	if th.running {
		th.mu.Unlock()
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q already has a running turn", th.ID))
	}
	if th.PersistHistory {
		if err := appendChatMessage(s.rt.SessionDir, th.ID, userMsg); err != nil {
			th.mu.Unlock()
			return s.writeResponse(req.ID, nil, err)
		}
	}
	history := cloneHistory(th.History)
	history = append(history, userMsg)
	th.History = history
	turn := th.appendUserMessageTurnLocked(session.NewID(), userMsg, now)
	th.mu.Unlock()

	if err := s.writeResponse(req.ID, TurnStartResult{Turn: turn}, nil); err != nil {
		return err
	}
	if err := s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
		ThreadID: th.ID,
		Turn:     turn,
	}); err != nil {
		return err
	}
	if err := s.writeNotification(NotificationTurnCompleted, TurnCompletedNotification{
		ThreadID: th.ID,
		Turn:     turn,
	}); err != nil {
		return err
	}
	s.routeUserMessageToResidents(th, userMsg, mentioned)
	return nil
}

// notifyThreadStarted is the single outlet for thread/started broadcasts.
// Every send point must route through it instead of calling
// writeNotification directly: the snapshot is wrapped through
// threadWithGroupMembers first, so a group thread's first broadcast always
// carries Members. A bare snapshotLocked() value would render the channel
// with no member chips until some other wrapped payload happened to arrive
// (consistency plan 2026-07-04 §1 #15).
func (s *Server) notifyThreadStarted(thread Thread) error {
	return s.writeNotification(NotificationThreadStarted, ThreadStartedNotification{
		Thread: s.threadWithGroupMembers(thread),
	})
}

// notifyThreadUpdated mirrors notifyThreadStarted for thread/updated. This
// one guards against the inverse failure: the frontend replaces its cached
// thread wholesale on thread/updated, so a bare group snapshot would wipe
// Members the UI already had.
func (s *Server) notifyThreadUpdated(thread Thread) error {
	return s.writeNotification(NotificationThreadUpdated, ThreadUpdatedNotification{
		Thread: s.threadWithGroupMembers(thread),
	})
}

func (s *Server) notifyGroupMemberAdded(thread Thread, participantID string) error {
	if err := s.notifyThreadUpdated(thread); err != nil {
		return err
	}
	if err := s.enqueueGroupMemberWelcomeEnvelope(thread, participantID); err != nil {
		providers.DebugLogf("enqueue group welcome for %q in %q: %v", participantID, thread.ID, err)
	}
	return nil
}

func (s *Server) enqueueGroupMemberWelcomeEnvelope(thread Thread, participantID string) error {
	participantID = strings.TrimSpace(participantID)
	if s == nil || s.rt == nil || participantID == "" {
		return nil
	}
	if s.participantRetired(participantID) {
		return nil
	}
	title := groupWelcomeTitle(thread)
	now := time.Now().UTC()
	env := MessageEnvelope{
		ID:             "env-" + session.NewID(),
		SourceThreadID: thread.ID,
		SourceTitle:    title,
		SenderKind:     "system",
		SenderName:     "Wuu",
		Addressed:      true,
		Hop:            0,
		Text:           fmt.Sprintf("你已加入「%s」群聊。请在群里发一条简短招呼，告诉大家你能帮什么。", title),
		CreatedAt:      now,
		Workspace:      thread.FocusWorkspace,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal group welcome envelope: %w", err)
	}
	if _, err := session.EnqueueResidentEnvelope(s.rt.SessionDir, session.ResidentEnvelope{
		ID:            env.ID,
		ParticipantID: participantID,
		EnvelopeJSON:  data,
		CreatedAt:     now,
	}); err != nil {
		return err
	}
	s.kickResidentAgent(participantID)
	return nil
}

func groupWelcomeTitle(thread Thread) string {
	title := strings.TrimSpace(thread.Title)
	if title == "" {
		title = strings.TrimSpace(thread.Preview)
	}
	if title == "" {
		title = "群聊"
	}
	return strings.TrimPrefix(title, "#")
}

// notifyOutboundBatch flushes notifications that were built while holding a
// threadState lock (applyStreamEventLocked / takePendingSteersLocked).
// Those builders only have the bare snapshot, so any thread/updated in the
// batch is re-routed through notifyThreadUpdated here — outside the lock —
// to keep the "group broadcasts always carry Members" invariant in one
// place.
func (s *Server) notifyOutboundBatch(batch []outboundNotification) {
	for _, item := range batch {
		if params, ok := item.params.(ThreadUpdatedNotification); ok {
			_ = s.notifyThreadUpdated(params.Thread)
			continue
		}
		_ = s.writeNotification(item.method, item.params)
	}
}

// threadWithGroupMembers populates Thread.Members for group threads (the
// frontend chips UI and chat avatars). For the #all channel, members mirror
// the entire named-participant roster rather than explicit thread_members
// rows. Non-group threads are returned unchanged.
func (s *Server) threadWithGroupMembers(thread Thread) Thread {
	if !thread.Group {
		return thread
	}
	ids, err := s.threadMemberIDsForGroup(thread.ID, thread.Group, thread.Title)
	if err != nil {
		providers.DebugLogf("resolve group members for %q: %v", thread.ID, err)
		return thread
	}
	members := make([]participant.Summary, 0, len(ids))
	for _, id := range ids {
		if summary, ok := s.resolveParticipantSummary(id); ok {
			members = append(members, summary)
		}
	}
	thread.Members = members
	return thread
}

// threadMemberIDsForGroup resolves the member participant IDs for a group
// thread: the full named roster for #all, explicit thread_members rows
// otherwise.
func (s *Server) threadMemberIDsForGroup(threadID string, group bool, title string) ([]string, error) {
	if isAllChannelThread(group, title) {
		return s.allNamedParticipantIDs()
	}
	return session.ListThreadMembers(s.rt.SessionDir, threadID)
}

// handleThreadMembersAdd implements `thread/members/add`: the user adds a
// named participant to a group thread's explicit roster. The #all channel is
// rejected because its membership is implicit — the entire named roster,
// with no thread_members rows to add. The result thread is wrapped through
// threadWithGroupMembers (via threadAfterMetadataUpdate →
// threadWithChildAgents) so the frontend member list updates from the
// response instead of going stale.
func (s *Server) handleThreadMembersAdd(req Request) error {
	var params ThreadMembersMutationParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	participantID := strings.TrimSpace(params.ParticipantID)
	if participantID == "" {
		return s.writeResponse(req.ID, nil, errors.New("participant_id is required"))
	}
	meta, ok, err := session.Find(s.rt.SessionDir, threadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if !ok {
		return s.writeResponse(req.ID, nil, fmt.Errorf("%w: %q", session.ErrSessionNotFound, threadID))
	}
	if !meta.Group {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q is not a group thread", threadID))
	}
	if isAllChannelThread(meta.Group, meta.Title) {
		return s.writeResponse(req.ID, nil, errors.New("the all channel includes every named agent; members cannot be added"))
	}
	members, err := session.ListThreadMembers(s.rt.SessionDir, threadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	for _, memberID := range members {
		if strings.TrimSpace(memberID) == participantID {
			thread, err := s.threadAfterMetadataUpdate(meta)
			if err != nil {
				return s.writeResponse(req.ID, nil, err)
			}
			return s.writeResponse(req.ID, ThreadMembersAddResult{Thread: thread}, nil)
		}
	}
	if len(members) >= maxGroupMembers {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q already has %d members (max %d)", threadID, len(members), maxGroupMembers))
	}
	if err := session.AddThreadMember(s.rt.SessionDir, threadID, participantID); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	thread, err := s.threadAfterMetadataUpdate(meta)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if err := s.writeResponse(req.ID, ThreadMembersAddResult{Thread: thread}, nil); err != nil {
		return err
	}
	return s.notifyGroupMemberAdded(thread, participantID)
}

// handleThreadMembersRemove implements `thread/members/remove`: the user
// removes a named participant from a group thread's explicit roster. The
// #all channel is rejected because its membership is implicit — the entire
// named roster, with no thread_members rows to delete. The result thread is
// wrapped through threadWithGroupMembers (via threadAfterMetadataUpdate →
// threadWithChildAgents) so the frontend member chips update from the
// response instead of going stale.
func (s *Server) handleThreadMembersRemove(req Request) error {
	var params ThreadMembersMutationParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	participantID := strings.TrimSpace(params.ParticipantID)
	if participantID == "" {
		return s.writeResponse(req.ID, nil, errors.New("participant_id is required"))
	}
	meta, ok, err := session.Find(s.rt.SessionDir, threadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if !ok {
		return s.writeResponse(req.ID, nil, fmt.Errorf("%w: %q", session.ErrSessionNotFound, threadID))
	}
	if !meta.Group {
		return s.writeResponse(req.ID, nil, fmt.Errorf("thread %q is not a group thread", threadID))
	}
	if isAllChannelThread(meta.Group, meta.Title) {
		return s.writeResponse(req.ID, nil, errors.New("the all channel includes every named agent; members cannot be removed"))
	}
	members, err := session.ListThreadMembers(s.rt.SessionDir, threadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	isMember := false
	for _, memberID := range members {
		if strings.TrimSpace(memberID) == participantID {
			isMember = true
			break
		}
	}
	if !isMember {
		return s.writeResponse(req.ID, nil, fmt.Errorf("participant %q is not a member of thread %q", participantID, threadID))
	}
	if err := session.RemoveThreadMember(s.rt.SessionDir, threadID, participantID); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	thread, err := s.threadAfterMetadataUpdate(meta)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadMembersRemoveResult{Thread: thread}, nil)
}

// rejectAllChannelMutation guards the system-guaranteed #all channel:
// neither agents nor users may rename, archive, or dissolve it
// (2026-07-03-sidebar-groups-andy-workspaces.md §4).
func (s *Server) rejectAllChannelMutation(threadID, action string) error {
	if s == nil || s.rt == nil {
		return nil
	}
	meta, ok, err := session.Find(s.rt.SessionDir, strings.TrimSpace(threadID))
	if err != nil || !ok {
		// Missing metadata falls through to the handler's own lookup error.
		return nil
	}
	if isAllChannelThread(meta.Group, meta.Title) {
		return fmt.Errorf("cannot %s the built-in all channel", action)
	}
	return nil
}
