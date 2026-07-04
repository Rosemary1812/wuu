package appserver

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/workspaces"
)

const residentEnvelopeBatchLimit = 20

type envelopeMetaRecord struct {
	ID             string `json:"id,omitempty"`
	SourceThreadID string `json:"source_thread_id"`
	// SourceThreadTitle snapshots the source thread's title at write time so
	// DM/group chat rows can name the source without a reverse lookup at
	// render time (the source thread may since have been renamed or
	// archived). Mirrors MessageEnvelope.SourceTitle.
	SourceThreadTitle   string    `json:"source_thread_title,omitempty"`
	Addressed           bool      `json:"addressed"`
	Hop                 int       `json:"hop"`
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
		if err := session.AddThreadMember(s.rt.SessionDir, threadID, id); err != nil {
			return nil, err
		}
		mentioned[id] = true
	}
	return mentioned, nil
}

func (s *Server) routeUserMessageToResidents(source *threadState, msg providers.ChatMessage, mentioned map[string]bool) {
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
		Text:           strings.TrimSpace(chatMessageDisplayContent(msg)),
		CreatedAt:      time.Now().UTC(),
		Workspace:      source.FocusWorkspace,
	}
	source.mu.Unlock()
	s.routeEnvelopes(source, env, mentioned)
}

func (s *Server) routeParticipantMessageToResidents(source *threadState, msg agentcontrol.ParticipantMessage, displayName string) {
	if source == nil || strings.TrimSpace(msg.Text) == "" || strings.EqualFold(strings.TrimSpace(msg.Kind), "decline") {
		return
	}
	source.mu.Lock()
	env := MessageEnvelope{
		SourceThreadID:      source.ID,
		SourceTitle:         residentEnvelopeSourceTitleLocked(source),
		SenderKind:          "participant",
		SenderName:          firstNonEmpty(displayName, msg.TaskName, msg.AgentType, msg.AgentID, "Participant"),
		SenderParticipantID: strings.TrimSpace(msg.ParticipantID),
		Hop:                 msg.Hop,
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
}

func (s *Server) routeEnvelopes(source *threadState, base MessageEnvelope, mentioned map[string]bool) {
	if s == nil || s.rt == nil || source == nil {
		return
	}
	source.mu.Lock()
	sourceThreadID := strings.TrimSpace(source.ID)
	isAllChannel := isAllChannelThread(source.Group, source.Title)
	source.mu.Unlock()
	if sourceThreadID == "" {
		return
	}
	// #all's membership is implicit (the entire named-participant roster);
	// every other group/conversation thread routes to its explicit
	// thread_members rows (chat-style-threads-design.md §3.2).
	var members []string
	var err error
	if isAllChannel {
		members, err = s.allNamedParticipantIDs()
	} else {
		members, err = session.ListThreadMembers(s.rt.SessionDir, sourceThreadID)
	}
	if err != nil {
		providers.DebugLogf("list thread members for resident routing %q: %v", sourceThreadID, err)
		return
	}
	for _, participantID := range members {
		participantID = strings.TrimSpace(participantID)
		if participantID == "" || participantID == strings.TrimSpace(base.SenderParticipantID) {
			continue
		}
		// Membership queries already exclude retired participants; this
		// guards the race where a member retires between the membership
		// read and the enqueue (retire cleanup protocol).
		if s.participantRetired(participantID) {
			continue
		}
		env := base
		env.ID = "env-" + session.NewID()
		env.Addressed = mentioned[participantID]
		// No hop-based delivery cutoff (2026-07-04 group-fanout amendment,
		// resident-named-agents.md 增补四): deep agent→agent relays are
		// delivered like any other ambient message, so a turtle-soup-style
		// back-and-forth no longer dies the moment neither side @mentions.
		// Whether to engage is the model's judgment (design principle 10 +
		// the from="agent" prompt rule), not a depth counter; the only
		// structural backstop is the parallel fan-out width, bounded by the
		// group-size / roster cap (maxGroupMembers). Hop rides along as an
		// advisory signal in the envelope prompt.
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
		s.kickResidentAgent(participantID)
	}
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
	pending, err := session.PendingResidentEnvelopes(s.rt.SessionDir, participantID, residentEnvelopeBatchLimit)
	if err != nil {
		providers.DebugLogf("load resident inbox for %q: %v", participantID, err)
		return
	}
	envs := make([]MessageEnvelope, 0, len(pending))
	ids := make([]string, 0, len(pending))
	for _, raw := range pending {
		var env MessageEnvelope
		if err := json.Unmarshal(raw.EnvelopeJSON, &env); err != nil {
			providers.DebugLogf("decode resident envelope %q for %q: %v", raw.ID, participantID, err)
			continue
		}
		if strings.TrimSpace(env.ID) == "" {
			env.ID = raw.ID
		}
		envs = append(envs, env)
		ids = append(ids, raw.ID)
	}
	if len(envs) == 0 {
		return
	}
	threadRuntime, err := s.ensureThreadRuntime(th)
	if err != nil {
		providers.DebugLogf("ensure resident runtime for %q: %v", participantID, err)
		return
	}
	if threadRuntime != nil && threadRuntime.Toolkit != nil {
		threadRuntime.Toolkit.SetParticipantSpeech(s.residentParticipantSpeechWithHops(participantID, residentSpeechHopsByThread(envs)))
		threadRuntime.Toolkit.SetGroupManager(s.residentGroupManager(participantID))
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
	if err := s.writeNotification(NotificationTurnStarted, TurnStartedNotification{
		ThreadID: th.ID,
		Turn:     started.turn,
	}); err != nil {
		started.cancel()
		return
	}
	go s.runResidentEnvelopeTurn(started.ctx, th, threadRuntime, started.turnID, started.runtime, started.history, envs)
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
			SourceThreadTitle:   env.SourceTitle,
			Addressed:           env.Addressed,
			Hop:                 env.Hop,
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

func (s *Server) mentionedMembersByName(source *threadState, text string) map[string]bool {
	out := make(map[string]bool)
	if s == nil || s.rt == nil || source == nil || strings.TrimSpace(text) == "" {
		return out
	}
	source.mu.Lock()
	threadID := strings.TrimSpace(source.ID)
	isAllChannel := isAllChannelThread(source.Group, source.Title)
	source.mu.Unlock()
	var members []string
	var err error
	if isAllChannel {
		members, err = s.allNamedParticipantIDs()
	} else {
		members, err = session.ListThreadMembers(s.rt.SessionDir, threadID)
	}
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
	s.fallbackDMReplyFromFinalAnswer(th, participantID, envs, turn, completedAt)
	if len(envs) > 0 {
		s.recordUnansweredAddressedEnvelopes(th, participantID, envs, turn, completedAt)
	}
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
	targetThreadID, ok := fallbackReplyTargetThreadID(threadID, envs)
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
		CreatedAt:     completedAt,
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
func fallbackReplyTargetThreadID(ownDMThreadID string, envs []MessageEnvelope) (string, bool) {
	if len(envs) == 0 {
		return ownDMThreadID, true
	}
	candidates := make([]MessageEnvelope, 0, len(envs))
	for _, env := range envs {
		if env.Addressed {
			candidates = append(candidates, env)
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	sourceThreadIDs := make(map[string]bool, len(candidates))
	for _, env := range candidates {
		id := strings.TrimSpace(env.SourceThreadID)
		if id == "" {
			continue
		}
		sourceThreadIDs[id] = true
	}
	if len(sourceThreadIDs) != 1 {
		return "", false
	}
	for id := range sourceThreadIDs {
		return id, true
	}
	return "", false
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
