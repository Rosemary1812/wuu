package appserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/session"
)

const (
	// maxCreateGroupPerTurn / maxAddGroupMemberPerTurn are the runtime
	// budgets for the resident group-management tools, enforced by the
	// same limiter family as post_message
	// (2026-07-03-sidebar-groups-andy-workspaces.md §4).
	maxCreateGroupPerTurn    = 1
	maxAddGroupMemberPerTurn = 8
	// maxGroupMembers caps explicit members of one group thread
	// (resident-named-agents.md §6).
	maxGroupMembers = 8
)

// residentGroupManager implements tools.GroupManager for one resident
// participant. Like residentSpeechLimiter, a fresh instance is wired per
// turn configuration, so the budgets are per turn.
type residentGroupManager struct {
	server        *Server
	participantID string
	limiter       *residentGroupLimiter
}

type residentGroupLimiter struct {
	mu      sync.Mutex
	creates int
	adds    int
}

func (s *Server) residentGroupManager(participantID string) *residentGroupManager {
	return &residentGroupManager{
		server:        s,
		participantID: strings.TrimSpace(participantID),
		limiter:       &residentGroupLimiter{},
	}
}

func (m *residentGroupManager) CreateGroup(ctx context.Context, title string) (string, error) {
	_ = ctx
	if m == nil || m.server == nil || m.server.rt == nil {
		return "", errors.New("create_group: app server not configured")
	}
	participantID := strings.TrimSpace(m.participantID)
	if participantID == "" {
		return "", errors.New("create_group: participant_id is required")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("create_group: title is required")
	}
	// The "all" channel is a system-guaranteed singleton: neither agents
	// nor users may create, rename, or dissolve it.
	if strings.EqualFold(title, allChannelTitle) {
		return "", fmt.Errorf("create_group: title %q is reserved for the built-in all channel", allChannelTitle)
	}
	if err := m.limiter.reserveCreate(); err != nil {
		return "", err
	}
	th, err := m.server.createGroupThreadState(session.NewID(), title, time.Now().UTC())
	if err != nil {
		return "", err
	}
	th = m.server.addResidentThread(th)
	if err := session.AddThreadMember(m.server.rt.SessionDir, th.ID, participantID); err != nil {
		return "", fmt.Errorf("create_group: join created group: %w", err)
	}
	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	if err := m.server.notifyThreadStarted(thread); err != nil {
		return "", err
	}
	return th.ID, nil
}

func (m *residentGroupManager) AddGroupMember(ctx context.Context, threadID, participantID string) error {
	_ = ctx
	if m == nil || m.server == nil || m.server.rt == nil {
		return errors.New("add_group_member: app server not configured")
	}
	callerID := strings.TrimSpace(m.participantID)
	if callerID == "" {
		return errors.New("add_group_member: participant_id is required")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return errors.New("add_group_member: thread_id is required")
	}
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return errors.New("add_group_member: participant_id is required")
	}
	sessionDir := m.server.rt.SessionDir
	meta, ok, err := session.Find(sessionDir, threadID)
	if err != nil {
		return fmt.Errorf("add_group_member: lookup thread: %w", err)
	}
	if !ok {
		return fmt.Errorf("add_group_member: %w: %q", session.ErrSessionNotFound, threadID)
	}
	if !meta.Group {
		return fmt.Errorf("add_group_member: thread %q is not a group thread", threadID)
	}
	if isAllChannelThread(meta.Group, meta.Title) {
		return errors.New("add_group_member: the all channel already includes every named agent; no members can be added")
	}
	members, err := session.ListThreadMembers(sessionDir, threadID)
	if err != nil {
		return fmt.Errorf("add_group_member: list thread members: %w", err)
	}
	callerIsMember := false
	targetIsMember := false
	for _, memberID := range members {
		memberID = strings.TrimSpace(memberID)
		if memberID == callerID {
			callerIsMember = true
		}
		if memberID == participantID {
			targetIsMember = true
		}
	}
	if !callerIsMember {
		return fmt.Errorf("add_group_member: participant %q is not a member of thread %q; ask the user to add you to the group first", callerID, threadID)
	}
	if targetIsMember {
		// Idempotent: re-adding an existing member is a no-op success and
		// does not consume budget.
		return nil
	}
	if len(members) >= maxGroupMembers {
		return fmt.Errorf("add_group_member: thread %q already has %d members (max %d)", threadID, len(members), maxGroupMembers)
	}
	if err := m.limiter.reserveAdd(); err != nil {
		return err
	}
	if err := session.AddThreadMember(sessionDir, threadID, participantID); err != nil {
		return fmt.Errorf("add_group_member: %w", err)
	}
	return nil
}

func (l *residentGroupLimiter) reserveCreate() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.creates >= maxCreateGroupPerTurn {
		return fmt.Errorf("create_group: already created %d group this turn (max %d)", l.creates, maxCreateGroupPerTurn)
	}
	l.creates++
	return nil
}

func (l *residentGroupLimiter) reserveAdd() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.adds >= maxAddGroupMemberPerTurn {
		return fmt.Errorf("add_group_member: already added %d members this turn (max %d)", l.adds, maxAddGroupMemberPerTurn)
	}
	l.adds++
	return nil
}
