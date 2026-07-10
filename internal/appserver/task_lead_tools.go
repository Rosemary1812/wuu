package appserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

var taskLeadManagementTools = map[string]bool{
	"manage_task":           true,
	"manage_participant":    true,
	"fetch_thread_messages": true,
	"post_message":          true,
	"react":                 true,
	"inception":             true,
}

// taskLeadManagementTurn identifies a system wake that asks the immutable lead
// to plan, recover, or conclude. Worker dispatches always carry an attempt id
// and retain the full execution surface.
func (s *Server) taskLeadManagementTurn(participantID string, envs []MessageEnvelope) bool {
	participantID = strings.TrimSpace(participantID)
	if s == nil || s.rt == nil || participantID == "" || len(planAttemptDispatches(envs)) > 0 {
		return false
	}
	for _, env := range envs {
		if !strings.EqualFold(strings.TrimSpace(env.SenderKind), "system") || strings.TrimSpace(env.SenderName) != "task plan" {
			continue
		}
		taskID := strings.TrimSpace(env.SourceSubthreadID)
		if taskID == "" {
			continue
		}
		task, err := session.FindConversationThreadByID(s.rt.SessionDir, taskID)
		if err == nil && strings.TrimSpace(task.LeadParticipantID) == participantID {
			return true
		}
	}
	return false
}

type allowlistedToolExecutor struct {
	base    agent.ToolExecutor
	allowed map[string]bool
}

func (e allowlistedToolExecutor) Definitions() []providers.ToolDefinition {
	if e.base == nil {
		return nil
	}
	defs := e.base.Definitions()
	out := make([]providers.ToolDefinition, 0, len(defs))
	for _, def := range defs {
		if e.allowed[strings.TrimSpace(def.Name)] {
			out = append(out, def)
		}
	}
	return out
}

func (e allowlistedToolExecutor) Execute(ctx context.Context, call providers.ToolCall) (string, error) {
	if e.base == nil || !e.allowed[strings.TrimSpace(call.Name)] {
		return "", fmt.Errorf("tool %q is unavailable during a Task lead management turn; delegate execution to a worker node", call.Name)
	}
	return e.base.Execute(ctx, call)
}

func (e allowlistedToolExecutor) SupportsTool(name string) bool {
	if !e.allowed[strings.TrimSpace(name)] {
		return false
	}
	if provider, ok := e.base.(agent.ToolSupportProvider); ok {
		return provider.SupportsTool(name)
	}
	for _, def := range e.Definitions() {
		if def.Name == name {
			return true
		}
	}
	return false
}

func (e allowlistedToolExecutor) ToolMetadata(call providers.ToolCall) (agent.ToolMetadata, bool) {
	if !e.allowed[strings.TrimSpace(call.Name)] {
		return agent.ToolMetadata{}, false
	}
	if provider, ok := e.base.(agent.ToolMetadataProvider); ok {
		return provider.ToolMetadata(call)
	}
	return agent.ToolMetadata{}, false
}

func (e allowlistedToolExecutor) ToolDisplay(call providers.ToolCall) (providers.ToolCallDisplay, bool) {
	if !e.allowed[strings.TrimSpace(call.Name)] {
		return providers.ToolCallDisplay{}, false
	}
	if provider, ok := e.base.(agent.ToolDisplayProvider); ok {
		return provider.ToolDisplay(call)
	}
	return providers.ToolCallDisplay{}, false
}

func (e allowlistedToolExecutor) LastAdditionalContext() string {
	if provider, ok := e.base.(agent.ToolContextProvider); ok {
		return provider.LastAdditionalContext()
	}
	return ""
}

func (e allowlistedToolExecutor) DiscoveredTools(call providers.ToolCall) []providers.LoadableToolDefinition {
	provider, ok := e.base.(agent.ToolDiscoveryProvider)
	if !ok {
		return nil
	}
	defs := provider.DiscoveredTools(call)
	out := defs[:0]
	for _, def := range defs {
		if e.allowed[strings.TrimSpace(def.Name)] {
			out = append(out, def)
		}
	}
	return out
}
