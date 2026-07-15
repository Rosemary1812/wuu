package appserver

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/tools"
)

const processCompletionOutputBytes = 8 * 1024

type processCompletionPayload struct {
	ProcessID         string         `json:"process_id"`
	Status            process.Status `json:"status"`
	ExitCode          int            `json:"exit_code"`
	Command           string         `json:"command,omitempty"`
	OutputTail        string         `json:"output_tail,omitempty"`
	OutputTruncated   bool           `json:"output_truncated,omitempty"`
	OutputStartOffset int64          `json:"output_start_offset,omitempty"`
	OutputEndOffset   int64          `json:"output_end_offset,omitempty"`
	OutputTotalBytes  int64          `json:"output_total_bytes,omitempty"`
	Instruction       string         `json:"instruction"`
}

func (s *Server) forwardProcessNotifications(threadID string, control *agentcontrol.AgentControl, manager *process.Manager, ch <-chan process.Event, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if manager != nil {
				// The channel is only a low-latency hint. Pull every persisted
				// obligation so a terminal event dropped behind a full channel is
				// recovered when any retained event is consumed.
				s.replayPendingProcessCompletions(threadID, control, manager)
				continue
			}
			if event.Cause != process.EventCauseNaturalExit || !processEventBelongsToThread(threadID, control, event) {
				continue
			}
			s.enqueueProcessCompletionTurn(threadID, event.Process.ID, processCompletionChatMessage(manager, event))
		}
	}
}

func processEventBelongsToThread(threadID string, control *agentcontrol.AgentControl, event process.Event) bool {
	ownerID := strings.TrimSpace(event.Process.OwnerID)
	switch event.Process.OwnerKind {
	case process.OwnerMainAgent:
		return ownerID != "" && ownerID == strings.TrimSpace(threadID)
	case process.OwnerSubagent:
		if ownerID == "" || control == nil {
			return false
		}
		for _, snapshot := range control.List() {
			if strings.TrimSpace(snapshot.ID) == ownerID {
				return true
			}
		}
	}
	return false
}

func processCompletionChatMessage(manager *process.Manager, event process.Event) providers.ChatMessage {
	payload := processCompletionPayload{
		ProcessID:   event.Process.ID,
		Status:      event.Process.Status,
		ExitCode:    event.Process.ExitCode,
		Command:     tools.RedactToolOutput(event.Process.Command),
		Instruction: "This background command has finished. Continue from this result; do not poll it again. Use bash action=read_background only if the omitted output matters.",
	}
	if manager != nil {
		snapshot, err := manager.ReadOutputSnapshot(context.Background(), event.Process.ID, process.OutputReadOptions{MaxBytes: processCompletionOutputBytes})
		if err == nil {
			payload.OutputTail = tools.RedactToolOutput(snapshot.Output)
			payload.OutputTruncated = snapshot.Truncated
			payload.OutputStartOffset = snapshot.StartOffset
			payload.OutputEndOffset = snapshot.EndOffset
			payload.OutputTotalBytes = snapshot.TotalBytes
		}
	}
	encoded, _ := json.Marshal(payload)
	return providers.ChatMessage{
		Role:     "user",
		Name:     wuucontext.ProcessNotificationMessageName,
		ClientID: processCompletionClientID([]string{event.Process.ID}),
		Content:  "<process_notification>" + string(encoded) + "</process_notification>",
	}
}

func (s *Server) replayPendingProcessCompletions(threadID string, control *agentcontrol.AgentControl, manager *process.Manager) {
	if s == nil || manager == nil {
		return
	}
	pending, err := manager.PendingCompletions()
	if err != nil {
		providers.DebugLogf("restore pending process completions for thread %q: %v", threadID, err)
		return
	}
	for _, p := range pending {
		event := process.Event{Type: process.EventStopped, Cause: process.EventCauseNaturalExit, Process: p}
		if p.Status == process.StatusFailed {
			event.Type = process.EventFailed
		}
		if processEventBelongsToThread(threadID, control, event) {
			s.enqueueProcessCompletionTurn(threadID, p.ID, processCompletionChatMessage(manager, event))
		}
	}
}

func (s *Server) restorePendingProcessCompletionsOnThreadResume(threadID string) {
	if s == nil || s.rt == nil || s.rt.ProcessManager == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	pending, err := s.rt.ProcessManager.PendingCompletions()
	if err != nil {
		providers.DebugLogf("inspect pending process completions while resuming thread %q: %v", threadID, err)
		return
	}
	needsRuntime := false
	for _, p := range pending {
		if (p.OwnerKind == process.OwnerMainAgent && strings.TrimSpace(p.OwnerID) == threadID) || p.OwnerKind == process.OwnerSubagent {
			needsRuntime = true
			break
		}
	}
	if !needsRuntime {
		return
	}
	th := s.thread(threadID)
	if th == nil || !canResumeAgentCompletionThread(th) {
		return
	}
	if _, err := s.ensureThreadRuntime(th); err != nil {
		providers.DebugLogf("restore pending process completions for resumed thread %q: %v", threadID, err)
	}
}

const (
	processCompletionClientIDPrefix       = "wuu-process-completion:"
	processCompletionAnswerClientIDPrefix = "wuu-process-completion-answer:"
)

func processCompletionClientID(processIDs []string) string {
	ids := uniqueSortedCompletionIDs(processIDs)
	if len(ids) == 0 {
		return ""
	}
	return processCompletionClientIDPrefix + strings.Join(ids, ",")
}

func processCompletionIDs(clientID string) []string {
	clientID = strings.TrimSpace(clientID)
	if !strings.HasPrefix(clientID, processCompletionClientIDPrefix) {
		return nil
	}
	return splitAgentCompletionResultIDs(strings.TrimPrefix(clientID, processCompletionClientIDPrefix))
}

func processCompletionAnswerIDs(clientID string) []string {
	clientID = strings.TrimSpace(clientID)
	if !strings.HasPrefix(clientID, processCompletionAnswerClientIDPrefix) {
		return nil
	}
	return splitAgentCompletionResultIDs(strings.TrimPrefix(clientID, processCompletionAnswerClientIDPrefix))
}

func uniqueSortedCompletionIDs(ids []string) []string {
	clean := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		clean = append(clean, id)
	}
	sort.Strings(clean)
	return clean
}

func markProcessCompletionAnswer(res *agent.LoopResult, processIDs []string) bool {
	if res == nil || len(res.NewMessages) == 0 {
		return false
	}
	ids := uniqueSortedCompletionIDs(processIDs)
	if len(ids) == 0 {
		return false
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	markerIndex := -1
	for i, msg := range res.NewMessages {
		for _, id := range processCompletionIDs(msg.ClientID) {
			if wanted[id] {
				markerIndex = i
				break
			}
		}
	}
	for i := len(res.NewMessages) - 1; i > markerIndex; i-- {
		msg := &res.NewMessages[i]
		if !strings.EqualFold(strings.TrimSpace(msg.Role), "assistant") || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		msg.ClientID = processCompletionAnswerClientIDPrefix + strings.Join(ids, ",")
		return true
	}
	return false
}

func processCompletionMarkerAnswered(history []providers.ChatMessage, processID string) bool {
	processID = strings.TrimSpace(processID)
	if processID == "" {
		return false
	}
	markerIndex := -1
	for i, msg := range history {
		for _, id := range processCompletionIDs(msg.ClientID) {
			if id == processID {
				markerIndex = i
				break
			}
		}
	}
	if markerIndex < 0 {
		return false
	}
	for _, msg := range history[markerIndex+1:] {
		for _, id := range processCompletionAnswerIDs(msg.ClientID) {
			if id == processID {
				return true
			}
		}
	}
	return false
}

func (s *Server) enqueueProcessCompletionTurn(threadID, processID string, msg providers.ChatMessage) {
	if s == nil || s.closed.Load() {
		return
	}
	threadID = strings.TrimSpace(threadID)
	processID = strings.TrimSpace(processID)
	if threadID == "" || processID == "" || !chatMessageHasUserPayload(msg) {
		return
	}
	th := s.thread(threadID)
	if th == nil || !canResumeAgentCompletionThread(th) {
		return
	}

	s.agentCompletionMu.Lock()
	if s.closed.Load() {
		s.agentCompletionMu.Unlock()
		return
	}
	for _, pending := range s.pendingAgentCompletionTurns[threadID] {
		if pending.processID == processID {
			s.agentCompletionMu.Unlock()
			return
		}
	}
	s.pendingAgentCompletionTurns[threadID] = append(s.pendingAgentCompletionTurns[threadID], agentCompletionTurn{
		processID: processID,
		msg:       msg,
	})
	s.agentCompletionMu.Unlock()

	s.kickAgentCompletionDrain(threadID)
}
