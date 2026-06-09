package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

type ListAgentProfilesTool struct{ env *Env }

func NewListAgentProfilesTool(env *Env) *ListAgentProfilesTool {
	return &ListAgentProfilesTool{env: env}
}

func (t *ListAgentProfilesTool) Name() string            { return "list_agent_profiles" }
func (t *ListAgentProfilesTool) IsReadOnly() bool        { return true }
func (t *ListAgentProfilesTool) IsConcurrencySafe() bool { return true }

func (t *ListAgentProfilesTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "list_agent_profiles",
		Description: "List durable named Agent Profiles that can be reused by subagents, workflow teams, or other recurring delegated roles. " +
			"Use this before deciding whether a role should reuse an existing memory-bearing profile, create a new profile, or use an ephemeral memoryless worker.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *ListAgentProfilesTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	wuuHome, err := statepath.Home("")
	if err != nil {
		return "", err
	}
	profiles, err := workflow.ListProfiles(wuuHome)
	if err != nil {
		return "", err
	}
	return mustJSON(map[string]any{"action": "list_agent_profiles", "profiles": profiles, "count": len(profiles)})
}

type CreateAgentProfileTool struct{ env *Env }

func NewCreateAgentProfileTool(env *Env) *CreateAgentProfileTool {
	return &CreateAgentProfileTool{env: env}
}

func (t *CreateAgentProfileTool) Name() string            { return "create_agent_profile" }
func (t *CreateAgentProfileTool) IsReadOnly() bool        { return false }
func (t *CreateAgentProfileTool) IsConcurrencySafe() bool { return true }

func (t *CreateAgentProfileTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "create_agent_profile",
		Description: "Create or update a durable named Agent Profile identity for recurring subagent or workflow roles. " +
			"Use only when the role is likely to recur or the user, workflow, or agent policy asks for a named memory-bearing agent; use ephemeral spawn_agent without agent_profile for one-off workers.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Stable profile name to pass later as spawn_agent.agent_profile.",
				},
				"role": map[string]any{
					"type":        "string",
					"description": "Short role label, for example QA reviewer or release coordinator.",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "Why this profile exists and what recurring work it should remember.",
				},
				"workflow_name": map[string]any{
					"type":        "string",
					"description": "Optional workflow definition name that motivated this profile. Leave empty for general subagent identities.",
				},
			},
			"required": []string{"name"},
		},
	}
}

func (t *CreateAgentProfileTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Name         string `json:"name"`
		Role         string `json:"role"`
		Description  string `json:"description"`
		WorkflowName string `json:"workflow_name"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return "", fmt.Errorf("create_agent_profile requires name")
	}
	wuuHome, err := statepath.Home("")
	if err != nil {
		return "", err
	}
	profile, created, err := workflow.EnsureProfile(workflow.ProfileEnsureOptions{
		WuuHome:      wuuHome,
		Name:         name,
		Source:       "agent",
		WorkflowName: args.WorkflowName,
		Role:         args.Role,
		Description:  args.Description,
	})
	if err != nil {
		return "", err
	}
	return mustJSON(map[string]any{
		"action":  "create_agent_profile",
		"profile": profile,
		"created": created,
		"next_steps": []string{
			"Use spawn_agent with agent_profile set to this profile name when this durable identity should perform work.",
			"Use a memoryless worker without agent_profile for one-off tasks that should not reuse or write profile memory.",
		},
	})
}
