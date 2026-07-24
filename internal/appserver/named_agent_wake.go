package appserver

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/channels"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
)

const (
	namedAgentSessionSource = "named-agent:"
	namedAgentWakePrompt    = "你有新消息，用 chat_check 查收"
)

func (s *Server) Deliver(agentID string) {
	if s == nil || s.closed.Load() || strings.TrimSpace(agentID) == "" {
		return
	}
	s.startBackground(func() {
		if err := s.deliverNamedAgentWake(context.Background(), agentID); err != nil {
			providers.DebugLogf("deliver named agent wake %q: %v", agentID, err)
		}
	})
}

func (s *Server) deliverNamedAgentWake(ctx context.Context, agentID string) error {
	s.namedAgentMu.Lock()
	defer s.namedAgentMu.Unlock()
	if s.channelService == nil {
		return errors.New("channels service is unavailable")
	}
	if s.rt == nil {
		return errors.New("runtime session is unavailable")
	}
	agent, err := s.channelService.GetNamedAgent(ctx, agentID)
	if err != nil {
		return err
	}
	threadID := namedAgentSessionID(agent)
	th := s.thread(threadID)
	if th == nil && !agent.Autostart {
		_, found, err := session.Find(s.rt.SessionDir, threadID)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
	}
	th, err = s.ensureNamedAgentThreadLocked(agent)
	if err != nil {
		return err
	}
	if threadIsRunning(th) {
		if err := s.channelService.MarkWakePending(ctx, agent.ID); err != nil {
			return err
		}
		return s.holdNamedAgentWake(th.ID, agent.ID)
	}
	_, _, _, _ = s.removeHeldUserTurn(th.ID, namedAgentWakeID(agent.ID))
	return s.startNamedAgentWakeLocked(agent, th)
}

func (s *Server) ensureNamedAgentThreadLocked(agent channels.NamedAgent) (*threadState, error) {
	if s.rt == nil {
		return nil, errors.New("runtime session is unavailable")
	}
	threadID := namedAgentSessionID(agent)
	if th := s.thread(threadID); th != nil {
		th.mu.Lock()
		defer th.mu.Unlock()
		if th.NamedAgentID != "" && th.NamedAgentID != agent.ID {
			return nil, fmt.Errorf("session %q is owned by named agent %q", threadID, th.NamedAgentID)
		}
		if th.Source != "" && th.Source != namedAgentSessionSource+agent.ID {
			return nil, fmt.Errorf("session %q is not owned by named agent %q", threadID, agent.ID)
		}
		th.NamedAgentID = agent.ID
		th.Source = namedAgentSessionSource + agent.ID
		th.CWD = filepath.Dir(agent.MemoryDir)
		if th.execRuntime == nil {
			selection := runtime.ThreadModelSelection{
				Provider: th.ModelProvider, Model: th.Model, Variant: th.ModelVariant,
				Effort: th.ModelEffort, PermissionMode: th.PermissionMode,
			}
			if model := strings.TrimSpace(agent.ModelOverride); model != "" {
				selection.Provider = strings.TrimSpace(agent.ProviderOverride)
				selection.Model = model
			}
			threadRuntime, err := s.newNamedAgentRuntime(threadID, agent, selection)
			if err != nil {
				return nil, err
			}
			th.execRuntime = threadRuntime
			if len(th.History) > 0 && strings.EqualFold(strings.TrimSpace(th.History[0].Role), "system") {
				th.History[0].Content = threadRuntime.StreamRunner.SystemPrompt
			}
		}
		return th, nil
	}
	agentHome := filepath.Dir(agent.MemoryDir)
	selection := s.currentSessionRuntimeSelection()
	if model := strings.TrimSpace(agent.ModelOverride); model != "" {
		selection.Provider = strings.TrimSpace(agent.ProviderOverride)
		selection.Model = model
	}
	threadRuntime, err := s.newNamedAgentRuntime(threadID, agent, runtime.ThreadModelSelection{
		Provider: selection.Provider, Model: selection.Model, Variant: selection.Variant,
		Effort: selection.Effort, PermissionMode: selection.PermissionMode,
	})
	if err != nil {
		return nil, err
	}

	metadata, found, err := session.Find(s.rt.SessionDir, threadID)
	if err != nil {
		releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: threadRuntime})
		return nil, err
	}
	var th *threadState
	if found {
		if metadata.Source != namedAgentSessionSource+agent.ID {
			releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: threadRuntime})
			return nil, fmt.Errorf("session %q is not owned by named agent %q", threadID, agent.ID)
		}
		th, err = s.loadPersistedThreadState(threadID, time.Now().UTC())
		if err != nil {
			releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: threadRuntime})
			return nil, err
		}
	} else {
		if _, err := session.CreateWithMetadata(s.rt.SessionDir, threadID, agentHome); err != nil {
			releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: threadRuntime})
			return nil, err
		}
		if _, err := session.SetSource(s.rt.SessionDir, threadID, namedAgentSessionSource+agent.ID); err != nil {
			releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: threadRuntime})
			return nil, err
		}
		if _, err := session.UpdateTitle(s.rt.SessionDir, threadID, agent.Name); err != nil {
			releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: threadRuntime})
			return nil, err
		}
		if _, err := session.SetRuntimeSelection(s.rt.SessionDir, threadID, selection); err != nil {
			releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: threadRuntime})
			return nil, err
		}
		history := []providers.ChatMessage{{Role: "system", Content: threadRuntime.StreamRunner.SystemPrompt}}
		th = newThreadState(threadID, history, selection.Provider, selection.Model, agentHome, true, time.Now().UTC())
		th.Source = namedAgentSessionSource + agent.ID
		th.Title = agent.Name
		applyThreadRuntimeSelection(th, selection)
	}
	th.NamedAgentID = agent.ID
	if len(th.History) > 0 && strings.EqualFold(strings.TrimSpace(th.History[0].Role), "system") {
		th.History[0].Content = threadRuntime.StreamRunner.SystemPrompt
	}
	th.execRuntime = threadRuntime
	s.mu.Lock()
	existing := s.threads[threadID]
	if existing == nil {
		s.threads[threadID] = th
	}
	s.mu.Unlock()
	if existing != nil {
		releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: threadRuntime})
		return existing, nil
	}
	return th, nil
}

func (s *Server) newNamedAgentRuntime(threadID string, agent channels.NamedAgent, selection runtime.ThreadModelSelection) (*runtime.ThreadRuntime, error) {
	agentHome := filepath.Dir(agent.MemoryDir)
	threadRuntime, err := s.rt.NewNamedAgentThreadRuntime(
		threadID, agentHome, agent.MemoryDir, namedAgentOrientation(agent), selection,
	)
	if err != nil {
		return nil, err
	}
	chatAgent, err := s.channelService.BindAgent(context.Background(), agent.ID)
	if err != nil {
		releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: threadRuntime})
		return nil, err
	}
	if threadRuntime.Toolkit == nil {
		releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: threadRuntime})
		return nil, errors.New("named agent toolkit is unavailable")
	}
	threadRuntime.Toolkit.SetChatAgent(chatAgent)
	if err := s.rt.ConfigureNamedAgentThreadRuntime(
		threadRuntime, agentHome, agent.MemoryDir,
		namedAgentOrientation(agent),
	); err != nil {
		releaseDetachedThreadRuntime(detachedThreadRuntime{runtime: threadRuntime})
		return nil, err
	}
	return threadRuntime, nil
}

func (s *Server) startNamedAgentWakeLocked(agent channels.NamedAgent, th *threadState) error {
	permissions, err := s.resolveThreadTurnPermissions(th, nil)
	if err != nil {
		return err
	}
	message := providers.ChatMessage{
		Role: "user", Content: namedAgentWakePrompt,
		ClientID: namedAgentWakeTurnID(agent.ID), Hidden: true, Phase: "channel_wake",
	}
	var threadRuntime *runtime.ThreadRuntime
	started, ok, err := s.startThreadUserTurnWithAdmission(
		context.Background(), th, message, turnRuntimeSnapshot{}.withPermissions(permissions), false,
		turnReadOnlyFail, turnAdmissionHooks{afterLease: func(admitted *threadState, _ *providers.ChatMessage) error {
			threadRuntime, err = s.ensureThreadRuntimeAfterAdmission(admitted)
			return err
		}},
	)
	if err != nil {
		return err
	}
	if !ok {
		if err := s.channelService.MarkWakePending(context.Background(), agent.ID); err != nil {
			return err
		}
		return s.holdNamedAgentWake(th.ID, agent.ID)
	}
	launch, accepted := s.reserveBackground(func() {
		s.runTurn(started.ctx, th, threadRuntime, started.turnID, started.runtime, started.history)
		s.completeNamedAgentTurn(agent.ID)
	})
	if !accepted {
		return errors.Join(errServerClosed, s.abortStartedThreadTurnDurably(th, started, errServerClosed))
	}
	defer launch.Cancel()
	launch.Commit()
	return nil
}

func (s *Server) completeNamedAgentTurn(agentID string) {
	s.namedAgentMu.Lock()
	defer s.namedAgentMu.Unlock()
	if s.closed.Load() || s.channelService == nil {
		return
	}
	pending, err := s.channelService.TakePendingWake(context.Background(), agentID)
	if err != nil {
		providers.DebugLogf("take pending named agent wake %q: %v", agentID, err)
		return
	}
	threadID := ""
	if agent, getErr := s.channelService.GetNamedAgent(context.Background(), agentID); getErr == nil {
		threadID = namedAgentSessionID(agent)
		_, _, _, _ = s.removeHeldUserTurn(threadID, namedAgentWakeID(agentID))
		if pending {
			th, ensureErr := s.ensureNamedAgentThreadLocked(agent)
			if ensureErr == nil {
				ensureErr = s.startNamedAgentWakeLocked(agent, th)
			}
			if ensureErr != nil {
				providers.DebugLogf("inject pending named agent wake %q: %v", agentID, ensureErr)
			}
		}
		return
	}
	if threadID != "" {
		_, _, _, _ = s.removeHeldUserTurn(threadID, namedAgentWakeID(agentID))
	}
}

func (s *Server) holdNamedAgentWake(threadID, agentID string) error {
	id := namedAgentWakeID(agentID)
	if _, found, err := s.findHeldUserTurn(threadID, id); err != nil {
		return err
	} else if found {
		return nil
	}
	_, err := s.appendHeldUserTurns(threadID, []queuedTurn{{
		id: id,
		msg: providers.ChatMessage{
			Role: "user", Content: namedAgentWakePrompt, ClientID: id, Hidden: true, Phase: "channel_wake",
		},
		origin: session.HeldUserWorkOriginQueue,
	}})
	return err
}

func (s *Server) restoreNamedAgentWakes() {
	if s == nil || s.channelService == nil || s.closed.Load() {
		return
	}
	agents, err := s.channelService.ListNamedAgents(context.Background())
	if err != nil {
		providers.DebugLogf("restore named agent wakes: %v", err)
		return
	}
	for _, agent := range agents {
		state, err := s.channelService.WakeState(context.Background(), agent.ID)
		if err == nil && state.Outstanding {
			if err := s.deliverNamedAgentWake(context.Background(), agent.ID); err != nil {
				providers.DebugLogf("restore named agent wake %q: %v", agent.ID, err)
			}
		}
	}
}

func namedAgentSessionID(agent channels.NamedAgent) string {
	sum := sha256.Sum256([]byte(agent.ID))
	return agent.CreatedAt.UTC().Format("20060102-150405") + fmt.Sprintf("-%x", sum[:8])
}

func namedAgentWakeID(agentID string) string {
	return "channel-wake:" + strings.TrimSpace(agentID)
}

func namedAgentWakeTurnID(agentID string) string {
	return namedAgentWakeID(agentID) + ":" + session.NewID()
}

func namedAgentOrientation(agent channels.NamedAgent) string {
	agentHome := filepath.Dir(agent.MemoryDir)
	return fmt.Sprintf(`# Named agent

You are %s, a persistent named agent in Wuu group chat. Your agent home and default working directory is %s, and your durable memory directory is %s. The agent home is your private identity and state anchor; it is not the limit of your project activity scope. Use only your own memory directory for long-term memory; do not treat another agent or the user's memory as yours.

## Project activity scope

Your current registered project workspaces are supplied as request-only environment context. You may read, search, edit, and run commands in any listed project workspace, subject to the current permission mode. Use an absolute file path or set a command's cwd to the relevant project root. Do not claim that you can only access your agent home, and do not rebind the persistent session workspace merely to perform work in another listed project. Projectless conversation sessions are not project workspaces. The system temp directory may also be available for transient files, but it is not a project workspace.

Wake notifications contain no chat content. On wake, call chat_check to inspect the queryable inbox, choose which signals need full context with chat_read, and use chat_send only when you have a useful contribution. If chat_check returns has_more, check again. Every committed channel message is visible to all room members and produces an inbox signal for the other agents; @mention is not a visibility or delivery gate, but it does request the named agent's immediate attention and creates a response obligation. Human messages wake room agents whether or not they contain @mentions. Ordinary agent messages do not wake other agents; an agent @mention wakes only the named agent. Agent-only @mention handoffs are bounded per thread, and after the budget is exhausted further messages remain in inbox until human participation resets it. Keep chat messages short, do not repeat others, and use @ only when immediate attention from a specific agent is useful. Silence is valid when you have no useful response and no direct obligation.

Use chat_task to create, list, or update lightweight room tasks. If you own a task, keep progress in that task's thread and move its state from open to doing to done; task ownership is responsibility metadata, not an execution orchestrator. Use chat_remind when you need to wake yourself at least one minute later, optionally with room or thread context. Mention a human room member by their member ID when they must see a message.

chat_send requires the current basis sequence for the target room main stream or thread. If that scope moved, your text becomes a held draft instead of posting. Resolve every held draft explicitly with one of four paths:
- revise: read the delta, then chat_send replacement text with a fresh basis;
- as_is: chat_draft resolve as_is with a fresh basis when the independent point still stands (it may be held again if the scope moved);
- silent: chat_draft resolve silent when others covered it or silence is better;
- anyway: chat_draft resolve anyway only after hold_count reaches 2 and the unchanged text remains important.
The server never rewrites or automatically resends a held draft.`, agent.Name, agentHome, agent.MemoryDir)
}
