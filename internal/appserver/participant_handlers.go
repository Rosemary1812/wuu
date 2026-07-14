package appserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providerfactory"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/subagent"
)

// workerProviderName returns the provider name the AgentControl's worker
// runtime is currently configured for. It prefers the explicit Worker role
// selection and falls back to the main provider when Worker inherited. A
// participant pin compared against this name decides whether the override
// needs a fresh stream client (different provider) or just a model swap
// (same provider).
func workerProviderName(rt *runtime.Session) string {
	if rt == nil {
		return ""
	}
	if name := strings.TrimSpace(rt.ModelRoles.Worker.Provider); name != "" {
		return name
	}
	return strings.TrimSpace(rt.ProviderName)
}

// parseParticipantModelPin splits a participant Model field of the form
// "<provider>:<model>" into its provider and model parts. Splitting on the
// FIRST colon means model names containing further ':' or '/' characters
// (e.g. openrouter-style "openrouter/openai/gpt-4o-mini" or versioned model
// IDs like "v1:model") round-trip intact. A value with no colon is treated
// as a bare model name on the worker's current provider; providerName comes
// back empty in that case.
func parseParticipantModelPin(value string) (providerName, modelName string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	idx := strings.Index(value, ":")
	if idx < 0 {
		return "", value
	}
	return strings.TrimSpace(value[:idx]), strings.TrimSpace(value[idx+1:])
}

// participantModelOverride resolves a participant Model pin to a model name
// and an optional stream client override to apply to the next spawn. The
// helper looks at runtime.Session's worker provider selection rather than
// reach down into agentcontrol, so this file stays the single place that
// applies per-participant model pins.
//
// behavior:
//   - empty pin  → no override (follows global worker default)
//   - "model"    → override Model on the worker's current provider
//   - "p:model" where p == workerProviderName → override Model only
//   - "p:model" where p != workerProviderName → fresh stream client + Model
//   - missing p in config, malformed pin, or provider build failure → error
//
// Errors never fall back silently: the caller surfaces them on participant/start.
func resolveParticipantModelOverride(rt *runtimeSessionReference, participantName, rawPin, workerProviderName string) (modelOverride string, clientOverride providers.StreamClient, err error) {
	providerName, modelName := parseParticipantModelPin(rawPin)
	if providerName == "" && modelName == "" {
		return "", nil, nil
	}
	if modelName == "" {
		return "", nil, fmt.Errorf("participant %q pins model %q but model name is empty", participantName, rawPin)
	}
	if providerName == "" || providerName == workerProviderName {
		return modelName, nil, nil
	}
	if rt == nil || strings.TrimSpace(rt.configPath) == "" {
		return "", nil, fmt.Errorf("participant %q pins model %q but no runtime config is available", participantName, rawPin)
	}
	cfg, loadErr := rt.loadEffectiveConfig()
	if loadErr != nil {
		return "", nil, fmt.Errorf("participant %q pins model %q but config could not be loaded: %w", participantName, rawPin, loadErr)
	}
	providerCfg, found := cfg.Providers[providerName]
	if !found {
		return "", nil, fmt.Errorf("participant %q pins model %q but provider %q is not configured", participantName, rawPin, providerName)
	}
	client, buildErr := providerfactory.BuildStreamClient(providerCfg, providerName)
	if buildErr != nil {
		return "", nil, fmt.Errorf("participant %q pins model %q but provider %q failed to build: %w", participantName, rawPin, providerName, buildErr)
	}
	return modelName, client, nil
}

// runtimeSessionReference is the minimal shape resolveParticipantModelOverride
// needs from a runtime.Session. It exists so callers can pass either a real
// runtime.Session (production) or a constructed shape (tests) without a
// circular dependency on the runtime package.
type runtimeSessionReference struct {
	configPath string
	loadConfig func() (config.Config, string, error)
}

func newRuntimeSessionReference(rt *runtime.Session) *runtimeSessionReference {
	if rt == nil {
		return nil
	}
	return &runtimeSessionReference{
		configPath: rt.ConfigPath,
		loadConfig: rt.LoadEffectiveConfig,
	}
}

func (rt *runtimeSessionReference) loadEffectiveConfig() (config.Config, error) {
	if rt == nil {
		return config.Config{}, errors.New("runtime config is unavailable")
	}
	if rt.loadConfig != nil {
		cfg, _, err := rt.loadConfig()
		return cfg, err
	}
	cfg, _, err := config.LoadPath(rt.configPath)
	return cfg, err
}

func (s *Server) handleParticipantStart(ctx context.Context, req Request) error {
	var params ParticipantStartParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	threadID := strings.TrimSpace(params.ThreadID)
	if threadID == "" {
		return s.writeResponse(req.ID, nil, errors.New("thread_id is required"))
	}
	prompt := strings.TrimSpace(params.Prompt)
	if prompt == "" {
		return s.writeResponse(req.ID, nil, errors.New("prompt is required"))
	}
	displayPrompt := prompt

	th, err := s.ensureResidentThread(threadID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	var threadRuntime *runtime.ThreadRuntime

	participantID := strings.TrimSpace(params.ParticipantID)
	subagentType := strings.TrimSpace(params.SubagentType)
	agentProfile := strings.TrimSpace(params.AgentProfile)
	description := strings.TrimSpace(params.Description)
	taskName := strings.TrimSpace(params.TaskName)
	if participantID != "" {
		p, err := session.GetParticipant(s.rt.SessionDir, participantID)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		if p.Kind != participant.KindNamed {
			return s.writeResponse(req.ID, nil, fmt.Errorf("participant %q is not a named agent", participantID))
		}
		// GetParticipant intentionally returns retired rows (audit/history);
		// starting a run for one is rejected here (retire cleanup protocol).
		if p.RetiredAt != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("participant %q is retired and cannot be started", firstNonEmpty(p.Name, participantID)))
		}
		// Decision-five concurrency lock: a named agent runs one task at a
		// time. Refuse a second pull while it is already executing a
		// task run or mid chat-reply, instead of spawning a
		// concurrent run against the same resident identity. The caller is
		// told to wait or fork a copy (decision six) when more hands are
		// needed. This is the fast read-only reject; the lock is reserved
		// atomically right before Spawn to close the check-then-spawn window.
		if s.residentDraining(participantID) {
			return s.writeResponse(req.ID, nil, fmt.Errorf("participant %q is busy replying in chat; wait for it to finish or fork a copy", firstNonEmpty(p.Name, participantID)))
		}
		if s.participantIsBusy(participantID) {
			return s.writeResponse(req.ID, nil, fmt.Errorf("participant %q is busy running another task; wait for it to finish or fork a copy", firstNonEmpty(p.Name, participantID)))
		}
		// Role is a free-form persona/职责说明 note, NOT the task-dispatch
		// worker type: named agents all run on one deck, so role never selects
		// a tool surface. Name is the identity axis — task_name, agent_profile,
		// and the persona prompt all key off p.Name / the persona text.
		subagentType = resolveParticipantSubagentType(subagentType, p)
		if taskName == "" {
			taskName = p.Name
		}
		if description == "" {
			description = p.Tagline
		}
		if agentProfile == "" {
			agentProfile = p.Name
		}
		memory, err := s.readParticipantMemory(p)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		prompt = namedParticipantPrompt(p, memory, prompt, s.registeredWorkspaces())
	}
	if subagentType == "" {
		subagentType = agentcontrol.DefaultSubagentType
	}
	if taskName == "" {
		taskName = deriveParticipantTaskName(description, prompt, subagentType)
	}

	// A participant spawn becomes durable child work for this root thread. Hold
	// the same mutation lease used by delete until Spawn has recorded the child,
	// so delete observes either no child before the spawn or the active child
	// after it, never an in-between state. RecordUserMessage shares the lease
	// because it is model-visible history and must not interleave with a turn
	// compaction/rewrite.
	var mutationLease *session.ThreadExecutionLease
	if th.PersistHistory {
		mutationLease, err = s.tryAcquireThreadMutationLease(th.ID)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		defer releaseThreadMutationLease(th.ID, mutationLease)
		th.mu.Lock()
		if th.running || th.executionLease != nil {
			th.mu.Unlock()
			return s.writeResponse(req.ID, nil, threadExecutionBusyError(th.ID))
		}
		if err := s.refreshDurableThreadHistoryLocked(th); err != nil {
			th.mu.Unlock()
			return s.writeResponse(req.ID, nil, err)
		}
		th.mu.Unlock()
	}
	threadRuntime, err = s.ensureThreadRuntimeAfterAdmission(th)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if threadRuntime == nil || threadRuntime.AgentControl == nil {
		return s.writeResponse(req.ID, nil, errors.New("participant/start requires agent control"))
	}

	if params.RecordUserMessage {
		userMsg, err := userMessageFromPrompt(displayPrompt, nil, nil, s.rt.ExperimentalHelpMe)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		now := time.Now().UTC()
		th.mu.Lock()
		if th.ReadOnly {
			th.mu.Unlock()
			return s.writeResponse(req.ID, nil, errors.New("thread is read-only"))
		}
		if th.PersistHistory {
			if _, err := appendChatMessage(s.rt.SessionDir, th.ID, userMsg); err != nil {
				th.mu.Unlock()
				return s.writeResponse(req.ID, nil, err)
			}
		}
		history := cloneHistory(th.History)
		history = append(history, userMsg)
		th.History = history
		turn := th.appendUserMessageTurnLocked(session.NewID(), userMsg, now)
		th.mu.Unlock()
		_ = s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
			ThreadID: threadID,
			Turn:     turn,
		})
	}

	// Reserve the concurrency lock atomically just before the spawn so two
	// near-simultaneous starts can't both pass the read-only check above.
	// The reservation is bound to the run id on success and rolled back on
	// spawn failure; the run's Running notification and completion drive the
	// authoritative acquire/release (forwardAgentNotifications).
	if participantID != "" {
		if !s.tryAcquireParticipantBusy(participantID, "task") {
			return s.writeResponse(req.ID, nil, fmt.Errorf("participant %q is busy running another task; wait for it to finish or fork a copy", firstNonEmpty(taskName, participantID)))
		}
	}
	spawned, err := threadRuntime.AgentControl.Spawn(ctx, agentcontrol.SpawnRequest{
		Type:             subagentType,
		TaskName:         taskName,
		ParticipantID:    participantID,
		AgentProfile:     agentProfile,
		Description:      description,
		Prompt:           prompt,
		ParentID:         threadID,
		ParentPath:       agentthread.RootPath,
		SpeechCapability: true,
		Isolation:        strings.TrimSpace(params.Isolation),
		Synchronous:      false,
	})
	if err != nil {
		if participantID != "" {
			s.releaseParticipantBusy(participantID, "")
		}
		return s.writeResponse(req.ID, nil, err)
	}
	if participantID != "" {
		s.bindParticipantBusyAgent(participantID, spawned.AgentID)
		_ = session.UpsertParticipantRun(s.rt.SessionDir, session.ParticipantRun{
			ID:            spawned.AgentID,
			ParticipantID: participantID,
			AgentID:       spawned.AgentID,
			TaskID:        spawned.TaskName,
			SessionID:     threadID,
			Summary:       participantRunSummary(description, displayPrompt),
			Outcome:       "running",
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		})
		// Bind the freshly-spawned agent to its participant identity so
		// the manage_participant tool can resolve "this agent's own
		// name" for retire-self enforcement. The handleParticipantStart
		// path is the only way a speech-capable run receives its
		// participant identity, so this is where the binding has to be
		// installed.
		threadRuntime.AgentControl.SetAgentParticipantID(spawned.AgentID, participantID)
	}

	for _, snap := range threadRuntime.AgentControl.List() {
		if snap.ID == spawned.AgentID {
			return s.writeResponse(req.ID, ParticipantStartResult{
				Agent: s.agentFromSnapshot(threadRuntime.AgentControl, snap),
			}, nil)
		}
	}

	return s.writeResponse(req.ID, ParticipantStartResult{
		Agent: Agent{
			ID:           spawned.AgentID,
			Type:         subagentType,
			TaskName:     spawned.TaskName,
			AgentProfile: spawned.AgentProfile,
			AgentPath:    spawned.AgentPath,
			ParentID:     threadID,
			Description:  description,
			Status:       spawned.Status,
			StartedAt:    time.Now().UTC(),
			Participant: s.participantSummaryForSnapshot(subagent.SubAgentSnapshot{
				ParticipantID: spawned.ParticipantID,
				TaskName:      spawned.TaskName,
				Type:          subagentType,
			}),
		},
	}, nil)
}

// resolveParticipantSubagentType picks the worker type for a participant/start
// spawn. Decision-four (name-centric identity): a named agent's Role is a
// persona/职责说明 note, never a worker type, so it is deliberately NOT
// consulted here — every named agent runs on the shared default deck. Only an
// explicit caller-supplied subagent_type overrides the default; the
// participant is accepted so the "role is ignored" contract is explicit and
// test-guardable at this single derivation point.
func resolveParticipantSubagentType(explicitSubagentType string, _ participant.Participant) string {
	if s := strings.TrimSpace(explicitSubagentType); s != "" {
		return s
	}
	return agentcontrol.DefaultSubagentType
}

func participantRunSummary(values ...string) string {
	for _, value := range values {
		summary := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		if summary == "" {
			continue
		}
		runes := []rune(summary)
		if len(runes) > 180 {
			return string(runes[:180])
		}
		return summary
	}
	return ""
}

func deriveParticipantTaskName(description, prompt, subagentType string) string {
	for _, value := range []string{description, prompt, subagentType} {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		fields := strings.Fields(name)
		if len(fields) > 6 {
			fields = fields[:6]
		}
		name = strings.Join(fields, "_")
		name = strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z':
				return r
			case r >= 'A' && r <= 'Z':
				return r
			case r >= '0' && r <= '9':
				return r
			case r == '_' || r == '-':
				return r
			default:
				return '_'
			}
		}, name)
		name = strings.Trim(name, "_-")
		if name != "" {
			return name
		}
	}
	return "participant_task"
}
