package appserver

import (
	"context"
	"encoding/json"
	"strings"

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
		Role:    "user",
		Name:    wuucontext.ProcessNotificationMessageName,
		Content: "<process_notification>" + string(encoded) + "</process_notification>",
	}
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
		if pending.snapshot == nil && pending.resultID == "" && pending.agentID == processID {
			s.agentCompletionMu.Unlock()
			return
		}
	}
	s.pendingAgentCompletionTurns[threadID] = append(s.pendingAgentCompletionTurns[threadID], agentCompletionTurn{
		agentID: processID,
		msg:     msg,
	})
	s.agentCompletionMu.Unlock()

	s.kickAgentCompletionDrain(threadID)
}
