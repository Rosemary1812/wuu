package appserver

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

func (s *Server) handleThreadStart(req Request) error {
	var params ThreadStartParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := session.NewID()
	memoryPath := ""
	if !params.Ephemeral {
		sess, err := session.CreateWithMetadata(s.rt.SessionDir, id, s.rt.RootDir)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		memoryPath = session.FilePath(s.rt.SessionDir, sess.ID)
	} else {
		id = "ephemeral-" + id
	}
	history := make([]providers.ChatMessage, 0, 1)
	if prompt := strings.TrimSpace(s.rt.StreamRunner.SystemPrompt); prompt != "" {
		history = append(history, providers.ChatMessage{Role: "system", Content: prompt})
	}
	th := newThreadState(id, history, s.rt.ProviderName, s.rt.Model, s.rt.RootDir, memoryPath, time.Now().UTC())
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
	return s.writeNotification(NotificationThreadStarted, ThreadStartedNotification{
		Thread: thread,
	})
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
		result := ThreadResumeResult{Thread: thread}
		if err := s.writeResponse(req.ID, result, nil); err != nil {
			return err
		}
		return s.writeNotification(NotificationThreadResumed, ThreadResumedNotification{
			Thread: thread,
		})
	}
	path, err := session.Load(s.rt.SessionDir, id)
	if err != nil {
		thread, ok, agentErr := s.agentSessionThread(id)
		if agentErr != nil {
			return s.writeResponse(req.ID, nil, agentErr)
		}
		if !ok {
			return s.writeResponse(req.ID, nil, err)
		}
		result := ThreadResumeResult{Thread: thread}
		if err := s.writeResponse(req.ID, result, nil); err != nil {
			return err
		}
		return s.writeNotification(NotificationThreadResumed, ThreadResumedNotification{
			Thread: thread,
		})
	}
	history, err := loadChatMessages(path)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	repaired, err := providers.RepairAndValidateToolCallHistory(history)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if !reflect.DeepEqual(repaired, history) {
		if err := rewriteChatHistory(path, repaired); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	history = repaired
	history = ensureBaseSystemPrompt(history, s.rt.StreamRunner.SystemPrompt)
	th := newThreadState(id, history, s.rt.ProviderName, s.rt.Model, s.rt.RootDir, path, time.Now().UTC())
	if metadata, ok, err := session.Find(s.rt.SessionDir, id); err != nil {
		return s.writeResponse(req.ID, nil, err)
	} else if ok {
		applySessionMetadata(th, metadata)
	}
	s.mu.Lock()
	s.threads[id] = th
	s.mu.Unlock()

	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	thread, err = s.threadWithChildAgents(thread)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	result := ThreadResumeResult{Thread: thread}
	if err := s.writeResponse(req.ID, result, nil); err != nil {
		return err
	}
	return s.writeNotification(NotificationThreadResumed, ThreadResumedNotification{
		Thread: thread,
	})
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
	sess, err := session.CreateForkWithMetadata(s.rt.SessionDir, id, source.cwd, fork)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	path := session.FilePath(s.rt.SessionDir, sess.ID)
	if err := rewriteChatHistory(path, history); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if err := session.UpdateIndex(s.rt.SessionDir, sess.ID, persistableMessageCount(history), threadPreview(history)); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	th := newThreadState(sess.ID, history, source.modelProvider, source.model, source.cwd, path, now)
	applySessionMetadata(th, *sess)
	s.mu.Lock()
	s.threads[th.ID] = th
	s.mu.Unlock()

	th.mu.Lock()
	thread := th.snapshotLocked()
	th.mu.Unlock()
	if err := s.writeResponse(req.ID, ThreadForkResult{Thread: thread}, nil); err != nil {
		return err
	}
	return s.writeNotification(NotificationThreadStarted, ThreadStartedNotification{
		Thread: thread,
	})
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

	path, err := session.Load(s.rt.SessionDir, id)
	if err != nil {
		return forkSourceThread{}, err
	}
	history, err := loadChatMessages(path)
	if err != nil {
		return forkSourceThread{}, err
	}
	repaired, err := providers.RepairAndValidateToolCallHistory(history)
	if err != nil {
		return forkSourceThread{}, err
	}
	if !reflect.DeepEqual(repaired, history) {
		if err := rewriteChatHistory(path, repaired); err != nil {
			return forkSourceThread{}, err
		}
	}
	history = ensureBaseSystemPrompt(repaired, s.rt.StreamRunner.SystemPrompt)
	th := newThreadState(id, history, s.rt.ProviderName, s.rt.Model, s.rt.RootDir, path, now)
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
	sessions, err := session.ListForCWD(s.rt.SessionDir, s.rt.RootDir, 0)
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
		if thread.CWD == s.rt.RootDir {
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
	metadata, err := session.UpdateTitle(s.rt.SessionDir, id, params.Title)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	thread, err := s.threadAfterMetadataUpdate(metadata)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, ThreadRenameResult{Thread: thread}, nil)
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
	th.PinnedAt = metadata.PinnedAt
	th.ArchivedAt = metadata.ArchivedAt
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
			CreatedAt:        sess.CreatedAt,
			UpdatedAt:        updatedAt,
			Turns:            []Turn{},
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
	return thread, nil
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
		childIndexByPath[meta.Path] = len(children)
		children = append(children, agentFromThreadMetadata(meta))
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

	sessions, err := session.ListForCWD(s.rt.SessionDir, s.rt.RootDir, 0)
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

func agentFromThreadMetadata(meta agentthread.Metadata) Agent {
	startedAt := meta.CreatedAt
	if startedAt.IsZero() {
		startedAt = meta.UpdatedAt
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
	sessions, err := session.ListForCWD(s.rt.SessionDir, s.rt.RootDir, 0)
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
