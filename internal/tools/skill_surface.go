package tools

import (
	"strings"

	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/skills"
)

// FilterSkillsForSurface returns only skills whose declared tool needs
// are compatible with the active compiled model surface. Without this
// guard, load_skill can reintroduce terminal or edit-tool instructions
// that the profile compiler intentionally removed from the visible tool
// list.
func FilterSkillsForSurface(all []skills.Skill, surface capability.Surface) []skills.Skill {
	if surface.ProfileName == "" {
		out := make([]skills.Skill, len(all))
		copy(out, all)
		return out
	}
	out := make([]skills.Skill, 0, len(all))
	for _, skill := range all {
		if skillAllowedBySurface(skill, surface) {
			out = append(out, skill)
		}
	}
	return out
}

func skillAllowedBySurface(skill skills.Skill, surface capability.Surface) bool {
	if surface.ProfileName == "" {
		return true
	}
	if len(skill.AllowedTools) == 0 {
		if surfaceHasVisibleCapability(surface, capability.CapabilityCommandBash) {
			return true
		}
		return !skillMentionsTerminalOnlyPath(skill)
	}
	for _, raw := range skill.AllowedTools {
		name := strings.TrimSpace(raw)
		if name == "" || !isKnownSurfaceSkillTool(name) {
			continue
		}
		if !surfaceAllowsSkillTool(surface, name) {
			return false
		}
	}
	return true
}

func surfaceAllowsSkillTool(surface capability.Surface, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	if _, ok := surface.Tools[name]; ok {
		return true
	}
	switch name {
	case "bash":
		return surfaceHasVisibleCapability(surface, capability.CapabilityCommandBash)
	case "run_shell", "run_test", "git", "start_process", "list_processes", "read_process_output", "write_stdin", "stop_process":
		return false
	default:
		return false
	}
}

func isKnownSurfaceSkillTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "read_file", "list_files", "write_file", "edit_file", "apply_patch",
		"grep", "glob", "ast_search", "semantic_search",
		"bash", "run_shell", "run_test", "git", "start_process", "list_processes", "read_process_output", "write_stdin", "stop_process",
		"spawn_agent", "send_message", "followup_task", "wait_agent", "await_agents", "close_agent", "list_agents", "agent_report",
		"tool_search", "load_skill",
		"web_fetch", "web_search",
		"read_memory", "write_memory", "session_memory",
		"update_plan", "start_goal", "update_goal", "complete_goal", "goal_status",
		"report_listening_ports",
		"schedule_cron", "cancel_cron", "list_cron",
		"list_workflows", "load_workflow", "save_workflow", "start_workflow", "run_workflow", "create_workflow", "workflow_control", "workflow_status",
		"list_agent_profiles", "create_agent_profile":
		return true
	default:
		return false
	}
}

func skillMentionsTerminalOnlyPath(skill skills.Skill) bool {
	text := strings.ToLower(strings.Join([]string{
		skill.Name,
		skill.Description,
		skill.WhenToUse,
		skill.TriggerCondition,
		strings.Join(skill.RequiredContext, " "),
		strings.Join(skill.VerificationChecklist, " "),
		skill.ProgressiveDisclosure,
		skill.Content,
	}, "\n"))
	for _, marker := range []string{
		"bash",
		"run_shell",
		"run_test",
		"start_process",
		"git ",
		"`git",
		" shell ",
		"terminal",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
