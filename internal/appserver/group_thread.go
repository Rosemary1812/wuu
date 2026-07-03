package appserver

import (
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
	if err := s.writeNotification(NotificationThreadStarted, ThreadStartedNotification{
		Thread: thread,
	}); err != nil {
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
func (s *Server) handleGroupTurnStart(req Request, th *threadState, params TurnStartParams, images []providers.InputImage, files []providers.InputFile) error {
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
	s.routeUserMessageToResidents(th, userMsg, mentioned)
	return nil
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
