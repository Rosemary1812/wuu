package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
)

const (
	maxEnvelopeHop             = 2
	residentEnvelopeBatchLimit = 20
)

type envelopeMetaRecord struct {
	ID                  string    `json:"id,omitempty"`
	SourceThreadID      string    `json:"source_thread_id"`
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
	}
	source.mu.Unlock()
	if env.Hop <= 0 {
		env.Hop = 1
	}
	mentioned := s.mentionedMembersByName(env.SourceThreadID, env.Text)
	s.routeEnvelopes(source, env, mentioned)
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
	members, err := session.ListThreadMembers(s.rt.SessionDir, sourceThreadID)
	if err != nil {
		providers.DebugLogf("list thread members for resident routing %q: %v", sourceThreadID, err)
		return
	}
	for _, participantID := range members {
		participantID = strings.TrimSpace(participantID)
		if participantID == "" || participantID == strings.TrimSpace(base.SenderParticipantID) {
			continue
		}
		env := base
		env.ID = "env-" + session.NewID()
		env.Addressed = mentioned[participantID]
		if env.Hop >= maxEnvelopeHop && !env.Addressed {
			continue
		}
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

func residentEnvelopeUserMessage(envs []MessageEnvelope, consumedIDs []string) providers.ChatMessage {
	return providers.ChatMessage{
		Role:                       "user",
		Content:                    coalesceEnvelopes(envs),
		DisplayContent:             residentEnvelopeDisplayContent(envs),
		EnvelopeMeta:               envelopeMetaJSON(envs),
		ConsumeResidentEnvelopeIDs: append([]string(nil), consumedIDs...),
	}
}

func residentEnvelopeDisplayContent(envs []MessageEnvelope) string {
	if len(envs) == 1 {
		env := envs[0]
		return strings.TrimSpace(fmt.Sprintf("Incoming message from %s: %s", firstNonEmpty(env.SourceTitle, env.SourceThreadID, "conversation"), env.Text))
	}
	return fmt.Sprintf("You received %d messages while busy.", len(envs))
}

func envelopeMetaJSON(envs []MessageEnvelope) json.RawMessage {
	records := make([]envelopeMetaRecord, 0, len(envs))
	for _, env := range envs {
		records = append(records, envelopeMetaRecord{
			ID:                  env.ID,
			SourceThreadID:      env.SourceThreadID,
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

func (s *Server) mentionedMembersByName(threadID, text string) map[string]bool {
	out := make(map[string]bool)
	if s == nil || s.rt == nil || strings.TrimSpace(text) == "" {
		return out
	}
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
	if len(envs) > 0 {
		s.recordUnansweredAddressedEnvelopes(th, participantID, envs, turn, completedAt)
	}
	s.kickResidentAgent(participantID)
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
