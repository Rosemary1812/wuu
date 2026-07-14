package appserver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/worktree"
)

// workspaceKindForCWD classifies a thread by where its working directory lives.
// Threads whose cwd sits under <wuuHome>/scratch belong to the scratch
// (no-project) workspace; threads whose cwd sits under <wuuHome>/agents/ belong
// to a per-agent direct-message home; everything else is treated as a project
// thread. The scratch root mirrors the layout produced by the desktop
// ProjectManager.selectNoProject helper, so threads created from a scratch
// chat round-trip with the correct kind after a desktop restart.
func workspaceKindForCWD(wuuHome, cwd string) WorkspaceKind {
	if wuuHome == "" || cwd == "" {
		return WorkspaceKindProject
	}
	scratchRoot := filepath.Clean(filepath.Join(wuuHome, "scratch"))
	agentRoot := filepath.Clean(filepath.Join(wuuHome, "agents"))
	cleanCWD := filepath.Clean(cwd)
	if cleanCWD == scratchRoot {
		return WorkspaceKindScratch
	}
	if strings.HasPrefix(cleanCWD, scratchRoot+string(filepath.Separator)) {
		return WorkspaceKindScratch
	}
	if cleanCWD == agentRoot {
		return WorkspaceKindDM
	}
	if strings.HasPrefix(cleanCWD, agentRoot+string(filepath.Separator)) {
		return WorkspaceKindDM
	}
	return WorkspaceKindProject
}

func (s *Server) handleThreadStart(req Request) error {
	var params ThreadStartParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	dmParticipantID := strings.TrimSpace(params.DMParticipantID)
	if dmParticipantID != "" && params.Ephemeral {
		return s.writeResponse(req.ID, nil, errors.New("dm threads cannot be ephemeral"))
	}
	if params.Group && dmParticipantID != "" {
		return s.writeResponse(req.ID, nil, errors.New("group threads cannot also be dm threads"))
	}
	if params.Group && params.Ephemeral {
		return s.writeResponse(req.ID, nil, errors.New("group threads cannot be ephemeral"))
	}
	if params.Group {
		th, err := s.createGroupThreadState(session.NewID(), params.Title, time.Now().UTC())
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		s.mu.Lock()
		s.threads[th.ID] = th
		s.mu.Unlock()

		th.mu.Lock()
		thread := th.snapshotLocked()
		th.mu.Unlock()
		thread = s.threadWithGroupMembers(thread)
		if err := s.writeResponse(req.ID, ThreadStartResult{Thread: thread}, nil); err != nil {
			return err
		}
		if err := s.notifyThreadStarted(thread); err != nil {
			return err
		}
		s.pruneResidentThreads(thread.ID)
		return nil
	}
	if dmParticipantID != "" {
		p, err := session.GetParticipant(s.rt.SessionDir, dmParticipantID)
		if err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("dm participant %q: %w", dmParticipantID, err))
		}
		if p.Kind != participant.KindNamed {
			return s.writeResponse(req.ID, nil, fmt.Errorf("dm participant %q is not a named agent", dmParticipantID))
		}
		// Find-or-create: a resident DM thread for this participant may
		// already exist in memory or on disk (e.g. created silently by a
		// background resident reply). Reusing ensureResidentDMThread here
		// keeps thread/start idempotent so a stale frontend cache can never
		// spawn a second, indistinguishable "Andy" tab. See issue #3.
		th, err := s.ensureResidentDMThread(dmParticipantID)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}

		th.mu.Lock()
		thread := th.snapshotLocked()
		th.mu.Unlock()
		if err := s.writeResponse(req.ID, ThreadStartResult{Thread: thread}, nil); err != nil {
			return err
		}
		if err := s.notifyThreadStarted(thread); err != nil {
			return err
		}
		s.pruneResidentThreads(thread.ID)
		return nil
	}

	id := session.NewID()
	persistHistory := !params.Ephemeral
	threadCWD := s.rt.RootDir
	workspaceKind := workspaceKindForCWD(s.rt.WuuHome, s.rt.RootDir)
	if !params.Ephemeral {
		if _, err := session.CreateWithMetadata(s.rt.SessionDir, id, threadCWD); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		// Bind the thread to the active workspace's stable id (registered
		// projects) so its state and listing survive the project moving; DM
		// and scratch runtimes carry no id and stay matched by cwd.
		if wsID := strings.TrimSpace(s.rt.WorkspaceID); wsID != "" {
			if _, err := session.SetWorkspaceID(s.rt.SessionDir, id, wsID); err != nil {
				return s.writeResponse(req.ID, nil, err)
			}
		}
	} else {
		id = "ephemeral-" + id
	}
	history := make([]providers.ChatMessage, 0, 1)
	if prompt := strings.TrimSpace(s.rt.StreamRunner.SystemPrompt); prompt != "" {
		history = append(history, providers.ChatMessage{Role: "system", Content: prompt})
	}
	th := newThreadState(id, history, s.rt.ProviderName, s.rt.Model, threadCWD, persistHistory, time.Now().UTC())
	th.WorkspaceKind = workspaceKind
	th.Ephemeral = params.Ephemeral

	s.mu.Lock()
	s.threads[id] = th
	s.mu.Unlock()

	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	if err := s.writeResponse(req.ID, ThreadStartResult{Thread: thread}, nil); err != nil {
		return err
	}
	if err := s.notifyThreadStarted(thread); err != nil {
		return err
	}
	s.pruneResidentThreads(thread.ID)
	return nil
}

func (s *Server) createResidentDMThreadState(p participant.Participant, id string, now time.Time) (*threadState, error) {
	if p.Kind != participant.KindNamed {
		return nil, fmt.Errorf("dm participant %q is not a named agent", p.ID)
	}
	wuuHome := strings.TrimSpace(s.rt.WuuHome)
	if wuuHome == "" {
		home, err := statepath.Home("")
		if err != nil {
			return nil, fmt.Errorf("dm participant %q: resolve wuu home: %w", p.ID, err)
		}
		wuuHome = home
	}
	threadCWD := statepath.AgentHomeDir(wuuHome, p.ID)
	if err := os.MkdirAll(threadCWD, 0o755); err != nil {
		return nil, fmt.Errorf("dm participant %q: create agent home: %w", p.ID, err)
	}
	prompt, err := s.residentPromptForParticipant(p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		id = session.NewID()
	}
	// DMs are matched by the DM bypass, never by workspace id, so they carry
	// no workspace id.
	if _, err := session.CreateWithMetadata(s.rt.SessionDir, id, threadCWD); err != nil {
		return nil, err
	}
	if _, err := session.BindDMParticipant(s.rt.SessionDir, id, p.ID); err != nil {
		return nil, err
	}
	if _, err := session.UpdateTitle(s.rt.SessionDir, id, p.Name); err != nil {
		return nil, err
	}
	history := []providers.ChatMessage{{Role: "system", Content: prompt}}
	th := newThreadState(id, history, s.rt.ProviderName, s.rt.Model, threadCWD, true, now)
	th.WorkspaceKind = WorkspaceKindDM
	th.DMParticipantID = p.ID
	th.Title = p.Name
	return th, nil
}

func (s *Server) handleThreadResume(req Request) error {
	var params ThreadResumeParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := strings.TrimSpace(params.SessionID)
	var err error
	if id == "" {
		id, err = s.mostRecentVisibleThreadID()
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		if id == "" {
			return s.writeResponse(req.ID, nil, errors.New("no sessions found"))
		}
	}
	if th := s.thread(id); th != nil {
		th.mu.Lock()
		thread := th.snapshotLocked()
		th.mu.Unlock()
		thread, err = s.threadWithChildAgents(thread)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		return s.writeThreadResumeResult(req, thread)
	}
	th, err := s.loadPersistedThreadState(id, time.Now().UTC())
	if err != nil {
		if !errors.Is(err, session.ErrSessionNotFound) {
			return s.writeResponse(req.ID, nil, err)
		}
		thread, ok, agentErr := s.agentSessionThread(id)
		if agentErr != nil {
			return s.writeResponse(req.ID, nil, agentErr)
		}
		if !ok {
			return s.writeResponse(req.ID, nil, session.ErrSessionNotFound)
		}
		return s.writeThreadResumeResult(req, thread)
	}
	th = s.addResidentThread(th)
	if th == nil {
		return s.writeResponse(req.ID, nil, errServerClosed)
	}

	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	thread, err = s.threadWithChildAgents(thread)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeThreadResumeResult(req, thread)
}

func (s *Server) writeThreadResumeResult(req Request, thread Thread) error {
	result := ThreadResumeResult{Thread: thread}
	if err := s.writeResponse(req.ID, result, nil); err != nil {
		return err
	}
	if err := s.writeNotification(NotificationThreadResumed, ThreadResumedNotification{
		Thread: thread,
	}); err != nil {
		return err
	}
	if s.thread(thread.ID) != nil {
		s.kickGoalContinuation(thread.ID)
	}
	s.pruneResidentThreads(thread.ID)
	return nil
}

func (s *Server) ensureResidentThread(id string) (*threadState, error) {
	if s == nil || s.closed.Load() {
		return nil, errServerClosed
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("thread_id is required")
	}
	if th := s.thread(id); th != nil {
		return th, nil
	}
	th, err := s.loadPersistedThreadState(id, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	th = s.addResidentThread(th)
	if th == nil {
		return nil, errServerClosed
	}
	s.pruneResidentThreads(id)
	return th, nil
}

func (s *Server) ensureResidentDMThread(participantID string) (*threadState, error) {
	if s == nil || s.closed.Load() {
		return nil, errServerClosed
	}
	participantID = strings.TrimSpace(participantID)
	if participantID == "" {
		return nil, errors.New("participant_id is required")
	}
	s.mu.Lock()
	threads := make([]*threadState, 0, len(s.threads))
	for _, th := range s.threads {
		threads = append(threads, th)
	}
	s.mu.Unlock()
	for _, th := range threads {
		if th == nil {
			continue
		}
		th.mu.Lock()
		matches := strings.TrimSpace(th.DMParticipantID) == participantID
		th.mu.Unlock()
		if matches {
			return th, nil
		}
	}

	sessions, err := session.List(s.rt.SessionDir, 0)
	if err != nil {
		return nil, err
	}
	for _, sess := range sessions {
		if strings.TrimSpace(sess.DMParticipantID) != participantID {
			continue
		}
		return s.ensureResidentThread(sess.ID)
	}

	p, err := session.GetParticipant(s.rt.SessionDir, participantID)
	if err != nil {
		return nil, err
	}
	// Existing DM threads for a retired participant remain loadable above
	// (frozen read-only at load); creating a brand-new DM for one is a
	// contradiction — the identity is archived.
	if p.RetiredAt != nil {
		return nil, fmt.Errorf("participant %q is retired; no new DM thread can be created", firstNonEmpty(p.Name, participantID))
	}
	th, err := s.createResidentDMThreadState(p, session.NewID(), time.Now().UTC())
	if err != nil {
		return nil, err
	}
	th = s.addResidentThread(th)
	if th == nil {
		return nil, errServerClosed
	}
	s.pruneResidentThreads(th.ID)
	return th, nil
}

func (s *Server) addResidentThread(th *threadState) *threadState {
	if s == nil || th == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return nil
	}
	if existing := s.threads[th.ID]; existing != nil {
		return existing
	}
	s.threads[th.ID] = th
	return th
}

func (s *Server) loadPersistedThreadState(id string, now time.Time) (*threadState, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("thread_id is required")
	}
	metadata, ok, err := session.Find(s.rt.SessionDir, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, session.ErrSessionNotFound
	}
	history, err := loadChatMessages(s.rt.SessionDir, id)
	if err != nil {
		return nil, err
	}
	repaired, err := providers.RepairAndValidateToolCallHistory(history)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(repaired, history) {
		if err := rewriteChatHistory(s.rt.SessionDir, id, repaired); err != nil {
			return nil, err
		}
	}
	systemPrompt := s.rt.StreamRunner.SystemPrompt
	dmRetired := false
	if participantID := strings.TrimSpace(metadata.DMParticipantID); participantID != "" {
		p, err := session.GetParticipant(s.rt.SessionDir, participantID)
		if err != nil {
			return nil, err
		}
		dmRetired = p.RetiredAt != nil
		systemPrompt, err = s.residentPromptForParticipant(p)
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(metadata.DMParticipantID) != "" {
		history = ensureResidentSystemPrompt(repaired, systemPrompt)
	} else {
		history = ensureBaseSystemPrompt(repaired, systemPrompt)
	}
	threadCWD := firstNonEmpty(metadata.CWD, s.rt.RootDir)
	th := newThreadState(id, history, s.rt.ProviderName, s.rt.Model, threadCWD, true, now)
	displayHistory, err := loadPersistedMessages(s.rt.SessionDir, id, false)
	if err != nil {
		return nil, err
	}
	th.Turns = turnsFromPersistedHistory(id, displayHistory, now, s.resolveParticipantSummary)
	if metas, err := loadMetaMessages(s.rt.SessionDir, id); err != nil {
		return nil, err
	} else {
		th.Turns = applyTokenUsageMetasToTurns(th.Turns, metas)
	}
	th.WorkspaceKind = workspaceKindForCWD(s.rt.WuuHome, threadCWD)
	applySessionMetadata(th, metadata)
	// Retire cleanup protocol: a retired participant's DM is frozen
	// read-only. ReadOnly is not persisted per-session, so the freeze is
	// derived from the participant row every time the thread loads.
	if dmRetired {
		th.ReadOnly = true
	}
	return th, nil
}

type forkSourceThread struct {
	history       []providers.ChatMessage
	modelProvider string
	model         string
	cwd           string
	thread        Thread
}

func (s *Server) handleThreadFork(req Request) error {
	var params ThreadForkParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	sourceID := strings.TrimSpace(params.ThreadID)
	if sourceID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	mode := strings.TrimSpace(params.Mode)
	if mode == "" {
		mode = "local"
	}
	if mode != "local" && mode != "worktree" {
		return s.writeResponse(req.ID, nil, fmt.Errorf("unsupported fork mode %q", mode))
	}

	now := time.Now().UTC()
	source, err := s.loadForkSourceThread(sourceID, now)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	history, err := forkHistoryAtTarget(source.history, source.thread.ID, source.thread.Turns, params.TurnID, params.ItemID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	id := session.NewID()
	fork := session.ForkMetadata{
		ForkedFromID:     source.thread.ID,
		ForkedFromTurnID: strings.TrimSpace(params.TurnID),
		ForkedFromItemID: strings.TrimSpace(params.ItemID),
	}
	forkCWD := source.cwd
	var createdWorktree *worktree.Worktree
	var forkWorktree session.WorktreeInfo
	if mode == "worktree" {
		manager, mgrErr := s.worktreeManager(source.cwd)
		if mgrErr != nil {
			return s.writeResponse(req.ID, nil, mgrErr)
		}
		createdWorktree, err = manager.Create(id, "fork", "")
		if err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("worktree create: %w", err))
		}
		forkCWD = createdWorktree.Path
		forkWorktree = session.WorktreeInfo{
			Path:     createdWorktree.Path,
			BaseHEAD: createdWorktree.HEAD,
			BaseRepo: firstNonEmpty(worktreeBaseRepo(source.thread.Worktree), source.cwd),
		}
	}
	cleanupWorktree := func() {
		if createdWorktree == nil {
			return
		}
		if manager, mgrErr := s.worktreeManager(source.cwd); mgrErr == nil {
			_ = manager.Cleanup(createdWorktree)
		}
	}

	var sess *session.Session
	if mode == "worktree" {
		sess, err = session.CreateWithWorktree(s.rt.SessionDir, id, forkCWD, fork, forkWorktree)
	} else {
		sess, err = session.CreateForkWithMetadata(s.rt.SessionDir, id, forkCWD, fork)
	}
	if err != nil {
		cleanupWorktree()
		return s.writeResponse(req.ID, nil, err)
	}
	// A fork belongs to the same workspace as its source, so it inherits the
	// active workspace's stable id (empty for scratch/DM/group runtimes).
	if wsID := strings.TrimSpace(s.rt.WorkspaceID); wsID != "" {
		if _, err := session.SetWorkspaceID(s.rt.SessionDir, id, wsID); err != nil {
			cleanupWorktree()
			return s.writeResponse(req.ID, nil, err)
		}
	}
	if err := rewriteChatHistory(s.rt.SessionDir, sess.ID, history); err != nil {
		_, _ = session.Delete(s.rt.SessionDir, sess.ID)
		cleanupWorktree()
		return s.writeResponse(req.ID, nil, err)
	}
	if err := session.UpdateIndex(s.rt.SessionDir, sess.ID, persistableMessageCount(history), threadPreview(history)); err != nil {
		_, _ = session.Delete(s.rt.SessionDir, sess.ID)
		cleanupWorktree()
		return s.writeResponse(req.ID, nil, err)
	}

	th := newThreadState(sess.ID, history, source.modelProvider, source.model, forkCWD, true, now)
	applySessionMetadata(th, *sess)
	s.mu.Lock()
	s.threads[th.ID] = th
	s.mu.Unlock()

	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	thread = s.threadWithWorktreeStatus(thread)
	if err := s.writeResponse(req.ID, ThreadForkResult{Thread: thread, Worktree: thread.Worktree}, nil); err != nil {
		return err
	}
	if err := s.notifyThreadStarted(thread); err != nil {
		return err
	}
	s.pruneResidentThreads(thread.ID, source.thread.ID)
	return nil
}

func (s *Server) handleThreadEditMessage(req Request) error {
	var params ThreadEditMessageParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	th, err := s.ensureResidentThread(threadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if s.hasQueuedUserTurns(threadID) {
		return s.writeResponse(req.ID, nil, errors.New("queued messages must be sent or removed before editing history"))
	}

	th.mu.Lock()
	if th.ReadOnly {
		th.mu.Unlock()
		return s.writeResponse(req.ID, nil, errors.New("thread is read-only"))
	}
	if th.running {
		th.mu.Unlock()
		return s.writeResponse(req.ID, nil, errors.New("thread is running"))
	}
	current := th.snapshotLocked()
	nextHistory, draft, err := editHistoryBeforeUserMessage(th.History, th.ID, current.Turns, params.TurnID, params.ItemID)
	if err != nil {
		th.mu.Unlock()
		return s.writeResponse(req.ID, nil, err)
	}
	if th.PersistHistory {
		if err := rewriteChatHistory(s.rt.SessionDir, th.ID, nextHistory); err != nil {
			th.mu.Unlock()
			return s.writeResponse(req.ID, nil, err)
		}
		if err := session.UpdateIndex(s.rt.SessionDir, th.ID, persistableMessageCount(nextHistory), threadPreview(nextHistory)); err != nil {
			th.mu.Unlock()
			return s.writeResponse(req.ID, nil, err)
		}
	}
	now := time.Now().UTC()
	th.History = nextHistory
	th.Turns = turnsFromHistory(th.ID, nextHistory, now)
	th.UpdatedAt = now
	th.currentTurn = ""
	th.nextItemIndex = 0
	th.activeAgentItemID = ""
	th.activeReasoningItemID = ""
	th.toolItems = make(map[string]string)
	thread := th.snapshotLocked()
	th.mu.Unlock()

	if err := s.writeResponse(req.ID, ThreadEditMessageResult{Thread: thread, Draft: draft}, nil); err != nil {
		return err
	}
	return s.notifyThreadUpdated(thread)
}

func (s *Server) loadForkSourceThread(id string, now time.Time) (forkSourceThread, error) {
	if th := s.thread(id); th != nil {
		th.mu.Lock()
		defer th.mu.Unlock()
		return forkSourceThread{
			history:       cloneHistory(th.History),
			modelProvider: th.ModelProvider,
			model:         th.Model,
			cwd:           th.CWD,
			thread:        th.snapshotLocked(),
		}, nil
	}

	if _, ok, err := session.Find(s.rt.SessionDir, id); err != nil {
		return forkSourceThread{}, err
	} else if !ok {
		return forkSourceThread{}, session.ErrSessionNotFound
	}
	history, err := loadChatMessages(s.rt.SessionDir, id)
	if err != nil {
		return forkSourceThread{}, err
	}
	repaired, err := providers.RepairAndValidateToolCallHistory(history)
	if err != nil {
		return forkSourceThread{}, err
	}
	if !reflect.DeepEqual(repaired, history) {
		if err := rewriteChatHistory(s.rt.SessionDir, id, repaired); err != nil {
			return forkSourceThread{}, err
		}
	}
	history = ensureBaseSystemPrompt(repaired, s.rt.StreamRunner.SystemPrompt)
	th := newThreadState(id, history, s.rt.ProviderName, s.rt.Model, s.rt.RootDir, true, now)
	if metas, err := loadMetaMessages(s.rt.SessionDir, id); err != nil {
		return forkSourceThread{}, err
	} else {
		th.Turns = applyTokenUsageMetasToTurns(th.Turns, metas)
	}
	if metadata, ok, err := session.Find(s.rt.SessionDir, id); err != nil {
		return forkSourceThread{}, err
	} else if ok {
		applySessionMetadata(th, metadata)
	}
	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	return forkSourceThread{
		history:       cloneHistory(th.History),
		modelProvider: th.ModelProvider,
		model:         th.Model,
		cwd:           th.CWD,
		thread:        thread,
	}, nil
}

func (s *Server) handleThreadList(req Request) error {
	var params ThreadListParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if _, err := s.ensureAllChannel(); err != nil {
		providers.DebugLogf("ensure #all group channel: %v", err)
	}
	targetCWD := firstNonEmpty(params.CWD, s.rt.RootDir)
	// Trust the active workspace's stable id only when the caller didn't pin a
	// specific cwd; an explicit params.CWD falls back to pure cwd matching.
	targetWorkspaceID := ""
	if strings.TrimSpace(params.CWD) == "" {
		targetWorkspaceID = s.rt.WorkspaceID
	}
	sessions, err := session.ListForCWDWithDMs(s.rt.SessionDir, targetCWD, targetWorkspaceID, 0)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	entries := make(map[string]threadListEntry, len(sessions))
	for _, sess := range sessions {
		if sess.ArchivedAt != nil {
			continue
		}
		entries[sess.ID] = threadEntryFromSession(sess, s.rt.ProviderName, s.rt.Model)
	}

	s.mu.Lock()
	for _, th := range s.threads {
		th.mu.Lock()
		thread := th.snapshotLocked()
		entry := threadListEntry{thread: thread, pinnedAt: th.PinnedAt}
		th.mu.Unlock()
		if thread.Ephemeral {
			continue
		}
		if thread.ReadOnly {
			continue
		}
		if thread.Archived {
			delete(entries, thread.ID)
			continue
		}
		if sameThreadListCWD(thread.CWD, targetCWD) || sameThreadListCWD(worktreeBaseRepo(thread.Worktree), targetCWD) || thread.DMParticipantID != "" || thread.Group {
			entries[thread.ID] = entry
		}
	}
	s.mu.Unlock()

	threads := make([]threadListEntry, 0, len(entries))
	for _, entry := range entries {
		threads = append(threads, entry)
	}
	sortThreadListEntries(threads)
	result := make([]Thread, 0, len(threads))
	for _, entry := range threads {
		thread, err := s.threadWithChildAgents(entry.thread)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		result = append(result, thread)
	}
	return s.writeResponse(req.ID, ThreadListResult{Threads: result}, nil)
}

// handleThreadListArchived returns every archived session the running server
// knows about, regardless of cwd. The Settings → Archive panel is global —
// it must surface threads the user has archived across every workspace and
// scratch directory, not just the active one. After an app restart the
// archive page reconstructs itself exclusively from this handler, so we
// merge disk-backed sessions with in-memory threads (in-memory wins when
// both exist, mirroring handleThreadList's policy) and ignore the caller's
// cwd argument.
func (s *Server) handleThreadListArchived(req Request) error {
	var params ThreadListParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	sessions, err := session.List(s.rt.SessionDir, 0)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	entries := make(map[string]threadListEntry, len(sessions))
	for _, sess := range sessions {
		if sess.ArchivedAt == nil {
			continue
		}
		entries[sess.ID] = threadEntryFromSession(sess, s.rt.ProviderName, s.rt.Model)
	}

	s.mu.Lock()
	for _, th := range s.threads {
		th.mu.Lock()
		thread := th.snapshotLocked()
		entry := threadListEntry{thread: thread, pinnedAt: th.PinnedAt}
		th.mu.Unlock()
		if thread.Ephemeral {
			continue
		}
		if thread.ReadOnly {
			continue
		}
		if !thread.Archived {
			continue
		}
		entries[thread.ID] = entry
	}
	s.mu.Unlock()

	threads := make([]threadListEntry, 0, len(entries))
	for _, entry := range entries {
		threads = append(threads, entry)
	}
	sortThreadListEntries(threads)
	result := make([]Thread, 0, len(threads))
	for _, entry := range threads {
		thread, err := s.threadWithChildAgents(entry.thread)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		result = append(result, thread)
	}
	return s.writeResponse(req.ID, ThreadListResult{Threads: result}, nil)
}

func sameThreadListCWD(left, right string) bool {
	return cleanThreadListCWD(left) == cleanThreadListCWD(right)
}

func cleanThreadListCWD(cwd string) string {
	if cwd == "" {
		return ""
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(cwd)
}

func (s *Server) handleThreadPin(req Request) error {
	var params ThreadPinParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := strings.TrimSpace(params.ThreadID)
	if id == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	metadata, err := session.UpdatePinned(s.rt.SessionDir, id, params.Pinned)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	thread, err := s.threadAfterMetadataUpdate(metadata)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadPinResult{Thread: thread}, nil)
}

func (s *Server) handleThreadArchive(req Request) error {
	var params ThreadArchiveParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := strings.TrimSpace(params.ThreadID)
	if id == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if params.Archived {
		if th := s.thread(id); th != nil {
			th.mu.Lock()
			running := th.running
			th.mu.Unlock()
			if running {
				return s.writeResponse(req.ID, nil, errors.New("cannot archive a running thread"))
			}
		}
		if err := s.rejectAllChannelMutation(id, "archive"); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	metadata, err := session.UpdateArchived(s.rt.SessionDir, id, params.Archived)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	thread, err := s.threadAfterMetadataUpdate(metadata)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadArchiveResult{Thread: thread}, nil)
}

func (s *Server) handleThreadRename(req Request) error {
	var params ThreadRenameParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := strings.TrimSpace(params.ThreadID)
	if id == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	if err := s.rejectAllChannelMutation(id, "rename"); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	metadata, err := session.UpdateTitle(s.rt.SessionDir, id, params.Title)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	thread, err := s.threadAfterMetadataUpdate(metadata)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if err := s.writeResponse(req.ID, ThreadRenameResult{Thread: thread}, nil); err != nil {
		return err
	}
	return s.notifyThreadUpdated(thread)
}

type threadListEntry struct {
	thread   Thread
	pinnedAt *time.Time
}

func applySessionMetadata(th *threadState, metadata session.Session) {
	if !metadata.CreatedAt.IsZero() {
		th.CreatedAt = metadata.CreatedAt
	}
	if !metadata.UpdatedAt.IsZero() {
		th.UpdatedAt = metadata.UpdatedAt
	} else if !metadata.CreatedAt.IsZero() {
		th.UpdatedAt = metadata.CreatedAt
	}
	if strings.TrimSpace(metadata.CWD) != "" {
		th.CWD = metadata.CWD
	}
	th.Title = metadata.Title
	th.ForkedFromID = metadata.ForkedFromID
	th.ForkedFromTurnID = metadata.ForkedFromTurnID
	th.ForkedFromItemID = metadata.ForkedFromItemID
	th.WorktreePath = metadata.WorktreePath
	th.WorktreeBaseHEAD = metadata.WorktreeBaseHEAD
	th.WorktreeBaseRepo = metadata.WorktreeBaseRepo
	th.PinnedAt = metadata.PinnedAt
	th.ArchivedAt = metadata.ArchivedAt
	th.DMParticipantID = metadata.DMParticipantID
	th.Group = metadata.Group
	th.FocusWorkspace = metadata.FocusWorkspace
}

func threadEntryFromSession(sess session.Session, provider, model string) threadListEntry {
	updatedAt := sess.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = sess.CreatedAt
	}
	return threadListEntry{
		thread: Thread{
			ID:               sess.ID,
			Preview:          firstNonEmpty(sess.Title, sess.Summary),
			Title:            sess.Title,
			ModelProvider:    provider,
			Model:            model,
			CWD:              sess.CWD,
			Status:           ThreadStatusIdle,
			Pinned:           sess.PinnedAt != nil,
			Archived:         sess.ArchivedAt != nil,
			ForkedFromID:     sess.ForkedFromID,
			ForkedFromTurnID: sess.ForkedFromTurnID,
			ForkedFromItemID: sess.ForkedFromItemID,
			Worktree:         threadWorktreeInfo(sess.WorktreePath, sess.WorktreeBaseHEAD, sess.WorktreeBaseRepo),
			CreatedAt:        sess.CreatedAt,
			UpdatedAt:        updatedAt,
			Turns:            []Turn{},
			DMParticipantID:  sess.DMParticipantID,
			Group:            sess.Group,
			FocusWorkspace:   sess.FocusWorkspace,
		},
		pinnedAt: sess.PinnedAt,
	}
}

func (s *Server) threadWithChildAgents(thread Thread) (Thread, error) {
	agents, err := s.childAgentsForThread(thread.ID)
	if err != nil {
		return thread, err
	}
	thread.ChildAgents = agents
	thread = s.threadWithGroupMembers(thread)
	return s.threadWithWorktreeStatus(thread), nil
}

func (s *Server) threadWithWorktreeStatus(thread Thread) Thread {
	if thread.Worktree == nil || strings.TrimSpace(thread.Worktree.Path) == "" {
		return thread
	}
	info := *thread.Worktree
	manager, err := s.worktreeManager(firstNonEmpty(info.BaseRepo, thread.CWD, s.rt.RootDir))
	if err == nil {
		if status, statusErr := manager.Status(info.Path); statusErr == nil {
			info.Dirty = status.Dirty
			info.ChangedFiles = append([]string(nil), status.ChangedFiles...)
		}
	}
	thread.Worktree = &info
	return thread
}

func (s *Server) worktreeManager(parentRepo string) (*worktree.Manager, error) {
	if s == nil || s.rt == nil {
		return nil, errors.New("runtime session is required")
	}
	parentRepo = firstNonEmpty(parentRepo, s.rt.RootDir)
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return nil, err
	}
	return worktree.NewManager(parentRepo, statepath.WorktreeRoot(stateDir))
}

func worktreeBaseRepo(info *WorktreeInfo) string {
	if info == nil {
		return ""
	}
	return strings.TrimSpace(info.BaseRepo)
}

func (s *Server) childAgentsForThread(threadID string) ([]Agent, error) {
	store := s.agentThreadStore(threadID)
	if store == nil {
		return nil, nil
	}
	threads, err := store.ListThreads()
	if err != nil {
		return nil, err
	}

	children := make([]Agent, 0)
	childIndexByPath := make(map[string]int)
	for _, meta := range threads {
		if !isDirectChildAgentThread(threadID, meta) {
			continue
		}
		pinned, archived := s.childAgentPinArchive(meta.ID)
		childIndexByPath[meta.Path] = len(children)
		children = append(children, agentFromThreadMetadata(meta, pinned, archived))
	}
	if len(children) == 0 {
		return nil, nil
	}

	for _, meta := range threads {
		if meta.Source.Kind != agentthread.SourceThreadSpawn {
			continue
		}
		for path, index := range childIndexByPath {
			if meta.ID == children[index].ID || !strings.HasPrefix(meta.Path, path+"/") {
				continue
			}
			children[index].NestedCount++
			if isRunningAgentStatus(string(meta.Status)) {
				children[index].NestedRunningCount++
			}
			break
		}
	}

	sort.Slice(children, func(i, j int) bool {
		left := children[i].StartedAt
		right := children[j].StartedAt
		if !left.Equal(right) {
			return left.Before(right)
		}
		return children[i].AgentPath < children[j].AgentPath
	})
	return children, nil
}

// childAgentPinArchive fetches the pinned/archived flags for a child agent's
// own session from the same session store keyed by the agent ID. A missing
// session or lookup error collapses to (false, false) so the renderer still
// gets a usable Agent record; the per-agent row is only meaningful for
// completed sessions that have been written to disk, so silent fallback here
// is safe.
func (s *Server) childAgentPinArchive(agentID string) (pinned, archived bool) {
	if s == nil || s.rt == nil || strings.TrimSpace(agentID) == "" {
		return false, false
	}
	sess, ok, err := session.Find(s.rt.SessionDir, agentID)
	if err != nil || !ok {
		return false, false
	}
	return sess.PinnedAt != nil, sess.ArchivedAt != nil
}

func (s *Server) agentSessionThread(agentID string) (Thread, bool, error) {
	rootID, meta, ok, err := s.agentThreadMetadata(agentID)
	if err != nil || !ok {
		return Thread{}, ok, err
	}

	var rec persistedAgentHistory
	if control := s.liveAgentControl(rootID); control != nil && control.Manager() != nil {
		if sa := control.Manager().Get(meta.ID); sa != nil {
			snap := sa.Snapshot()
			var th *threadState
			if isRunningSubAgentStatus(snap.Status) {
				th, _, _ = s.ensureLiveAgentThread(rootID, control, snap, time.Now().UTC())
			} else {
				th, _, _, _ = s.syncFinalAgentThread(rootID, control, snap, time.Now().UTC())
			}
			if th != nil {
				th.mu.Lock()
				thread := th.snapshotLocked()
				th.mu.Unlock()
				return thread, true, nil
			}
		}
	}
	history, hasHistory := s.liveAgentHistory(rootID, meta.ID)
	if !hasHistory {
		path, err := s.agentHistoryPath(rootID, meta.ID)
		if err != nil {
			return Thread{}, true, err
		}
		if path != "" {
			loaded, err := loadAgentHistory(path)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return Thread{}, true, err
				}
			} else {
				rec = loaded
				history = cloneHistory(rec.Messages)
			}
		}
	}
	if len(history) == 0 {
		history = fallbackAgentHistory(meta, rec)
	}

	now := time.Now().UTC()
	createdAt := meta.CreatedAt
	if createdAt.IsZero() {
		createdAt = rec.StartedAt
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := meta.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = rec.CompletedAt
	}
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	model := firstNonEmpty(rec.Model, meta.Model, s.rt.Model)
	cwd := firstNonEmpty(meta.CWD, s.rt.RootDir)

	return Thread{
		ID:            meta.ID,
		ParentID:      rootID,
		AgentPath:     meta.Path,
		Preview:       agentSessionPreview(meta, rec, history),
		ModelProvider: s.rt.ProviderName,
		Model:         model,
		CWD:           cwd,
		Status:        ThreadStatusIdle,
		ReadOnly:      true,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		Turns:         turnsFromHistory(meta.ID, history, now),
	}, true, nil
}

func (s *Server) agentThreadMetadata(agentID string) (string, agentthread.Metadata, bool, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", agentthread.Metadata{}, false, nil
	}
	rootIDs, err := s.rootThreadIDs()
	if err != nil {
		return "", agentthread.Metadata{}, false, err
	}
	for _, rootID := range rootIDs {
		store := s.agentThreadStore(rootID)
		if store == nil {
			continue
		}
		threads, err := store.ListThreads()
		if err != nil {
			return "", agentthread.Metadata{}, false, err
		}
		for _, meta := range threads {
			if meta.ID == agentID && meta.Source.Kind == agentthread.SourceThreadSpawn {
				return rootID, meta, true, nil
			}
		}
	}
	return "", agentthread.Metadata{}, false, nil
}

func (s *Server) rootThreadIDs() ([]string, error) {
	if s == nil || s.rt == nil {
		return nil, nil
	}
	seen := make(map[string]bool)
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}

	sessions, err := session.ListForCWDWithDMs(s.rt.SessionDir, s.rt.RootDir, s.rt.WorkspaceID, 0)
	if err != nil {
		return nil, err
	}
	for _, sess := range sessions {
		add(sess.ID)
	}

	s.mu.Lock()
	for id, th := range s.threads {
		if th == nil {
			continue
		}
		th.mu.Lock()
		cwd := th.CWD
		readOnly := th.ReadOnly
		th.mu.Unlock()
		if !readOnly && cwd == s.rt.RootDir {
			add(id)
		}
	}
	s.mu.Unlock()
	return ids, nil
}

func (s *Server) liveAgentHistory(rootID, agentID string) ([]providers.ChatMessage, bool) {
	control := s.liveAgentControl(rootID)
	if control == nil || control.Manager() == nil {
		return nil, false
	}
	return control.Manager().History(agentID)
}

func (s *Server) liveAgentControl(rootID string) *agentcontrol.AgentControl {
	th := s.thread(rootID)
	if th == nil {
		return nil
	}
	th.mu.Lock()
	threadRuntime := th.execRuntime
	th.mu.Unlock()
	if threadRuntime == nil {
		return nil
	}
	return threadRuntime.AgentControl
}

func (s *Server) agentHistoryPath(rootID, agentID string) (string, error) {
	rootID = strings.TrimSpace(rootID)
	agentID = strings.TrimSpace(agentID)
	if s == nil || s.rt == nil || rootID == "" || agentID == "" {
		return "", nil
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statepath.SessionArtifactDir(stateDir, rootID), "workers", agentID+".json"), nil
}

func (s *Server) agentThreadStore(threadID string) *agentthread.Store {
	if s == nil || s.rt == nil || strings.TrimSpace(threadID) == "" {
		return nil
	}
	stateDir, err := s.workspaceStateDir()
	if err != nil {
		return nil
	}
	return agentthread.NewStore(filepath.Join(statepath.SessionArtifactDir(stateDir, threadID), "threads"))
}

func (s *Server) workspaceStateDir() (string, error) {
	if s == nil || s.rt == nil {
		return "", errors.New("runtime session is required")
	}
	if stateDir := strings.TrimSpace(s.rt.StateDir); stateDir != "" {
		return stateDir, nil
	}
	home, err := statepath.Home("")
	if err != nil {
		return "", err
	}
	if id := strings.TrimSpace(s.rt.WorkspaceID); id != "" {
		return statepath.WorkspaceDirByID(home, id)
	}
	return statepath.WorkspaceDir(home, s.rt.RootDir)
}

func fallbackAgentHistory(meta agentthread.Metadata, rec persistedAgentHistory) []providers.ChatMessage {
	prompt := firstNonEmpty(rec.Prompt, meta.LastTaskMessage)
	result := strings.TrimSpace(rec.Result)
	errorText := strings.TrimSpace(rec.Error)
	history := make([]providers.ChatMessage, 0, 2)
	if prompt != "" {
		history = append(history, providers.ChatMessage{Role: "user", Content: prompt})
	}
	if result != "" {
		history = append(history, providers.ChatMessage{Role: "assistant", Content: result})
	} else if errorText != "" {
		history = append(history, providers.ChatMessage{Role: "assistant", Content: "Worker failed: " + errorText})
	}
	return history
}

func agentSessionPreview(meta agentthread.Metadata, rec persistedAgentHistory, history []providers.ChatMessage) string {
	if preview := firstNonEmpty(rec.Description, meta.TaskName, threadPreview(history), rec.Prompt, meta.LastTaskMessage, meta.ID); preview != "" {
		return preview
	}
	return "子任务"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isDirectChildAgentThread(threadID string, meta agentthread.Metadata) bool {
	if meta.Source.Kind != agentthread.SourceThreadSpawn || meta.ID == threadID {
		return false
	}
	if meta.Source.ParentPath == agentthread.RootPath {
		return true
	}
	return strings.TrimSpace(meta.ParentID) == strings.TrimSpace(threadID) && agentPathDepth(meta.Path) == 2
}

func agentFromThreadMetadata(meta agentthread.Metadata, pinArchive ...bool) Agent {
	startedAt := meta.CreatedAt
	if startedAt.IsZero() {
		startedAt = meta.UpdatedAt
	}
	var pinned, archived bool
	if len(pinArchive) > 0 {
		pinned = pinArchive[0]
	}
	if len(pinArchive) > 1 {
		archived = pinArchive[1]
	}
	return Agent{
		ID:           meta.ID,
		Type:         meta.Role,
		TaskName:     meta.TaskName,
		AgentProfile: meta.AgentProfile,
		AgentPath:    meta.Path,
		ParentID:     meta.ParentID,
		Description:  meta.TaskName,
		Status:       string(meta.Status),
		Pinned:       pinned,
		Archived:     archived,
		StartedAt:    startedAt,
	}
}

func isRunningAgentStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case string(agentthread.StatusPending), string(agentthread.StatusRunning):
		return true
	default:
		return false
	}
}

func agentPathDepth(path string) int {
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "/") + 1
}

func sortThreadListEntries(entries []threadListEntry) {
	sort.Slice(entries, func(i, j int) bool {
		leftPinned := entries[i].pinnedAt != nil
		rightPinned := entries[j].pinnedAt != nil
		if leftPinned != rightPinned {
			return leftPinned
		}
		leftTime := entries[i].thread.UpdatedAt
		if leftTime.IsZero() {
			leftTime = entries[i].thread.CreatedAt
		}
		rightTime := entries[j].thread.UpdatedAt
		if rightTime.IsZero() {
			rightTime = entries[j].thread.CreatedAt
		}
		if !leftTime.Equal(rightTime) {
			return leftTime.After(rightTime)
		}
		return entries[i].thread.ID > entries[j].thread.ID
	})
}

func (s *Server) mostRecentVisibleThreadID() (string, error) {
	sessions, err := session.ListForCWDWithDMs(s.rt.SessionDir, s.rt.RootDir, s.rt.WorkspaceID, 0)
	if err != nil {
		return "", err
	}
	entries := make([]threadListEntry, 0, len(sessions))
	for _, sess := range sessions {
		if sess.ArchivedAt != nil {
			continue
		}
		entries = append(entries, threadEntryFromSession(sess, s.rt.ProviderName, s.rt.Model))
	}
	sortThreadListEntries(entries)
	if len(entries) == 0 {
		return "", nil
	}
	return entries[0].thread.ID, nil
}

func (s *Server) threadAfterMetadataUpdate(metadata session.Session) (Thread, error) {
	if th := s.thread(metadata.ID); th != nil {
		th.mu.Lock()
		applySessionMetadata(th, metadata)
		thread := th.snapshotLocked()
		th.mu.Unlock()
		return s.threadWithChildAgents(thread)
	}
	return s.threadWithChildAgents(threadEntryFromSession(metadata, s.rt.ProviderName, s.rt.Model).thread)
}

func (s *Server) hasRunningThread() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, th := range s.threads {
		th.mu.Lock()
		running := th.running
		th.mu.Unlock()
		if running {
			return true
		}
	}
	return false
}
