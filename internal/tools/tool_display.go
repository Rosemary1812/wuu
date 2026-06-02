package tools

import (
	"fmt"
	"path"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// ToolDisplay implements agent.ToolDisplayProvider for built-in tools.
// MCP tools intentionally return no display metadata so clients can show the
// original MCP call.
func (t *Toolkit) ToolDisplay(call providers.ToolCall) (providers.ToolCallDisplay, bool) {
	if t == nil || t.registry == nil || t.registry.Lookup(call.Name) == nil {
		return providers.ToolCallDisplay{}, false
	}
	display := builtInToolDisplay(call)
	if strings.TrimSpace(display.Text) == "" {
		display = providers.ToolCallDisplay{
			Kind: displayKindForTool(call.Name),
			Text: fallbackDisplayToolName(call.Name),
		}
	}
	return display, true
}

func builtInToolDisplay(call providers.ToolCall) providers.ToolCallDisplay {
	args := displayArgs(call.Arguments)
	name := strings.TrimSpace(call.Name)

	switch name {
	case "read_file":
		return toolDisplay("read", "读取 "+displayPathTarget(displayString(args, "path", "file"), "文件"))
	case "list_files":
		target := displayPathTarget(displayString(args, "path"), "项目目录")
		if target == "项目目录" {
			return toolDisplay("read", "查看项目目录")
		}
		return toolDisplay("read", "查看 "+target)
	case "write_file":
		return toolDisplay("edit", "写入 "+displayPathTarget(displayString(args, "path", "file"), "文件"))
	case "edit_file":
		return toolDisplay("edit", "编辑 "+displayPathTarget(displayString(args, "path", "file"), "文件"))
	case "apply_patch":
		return toolDisplay("edit", "应用补丁")
	case "grep", "glob":
		return toolDisplay("search", "搜索 "+displaySearchTarget(displayString(args, "pattern", "query", "q")))
	case "tool_search":
		query := displayString(args, "query", "q")
		if query == "" {
			return toolDisplay("search", "搜索工具")
		}
		return toolDisplay("search", "搜索工具 "+displayTruncate(query, 90))
	case "run_shell":
		command := displayString(args, "command")
		if command == "" {
			return toolDisplay("command", "运行命令")
		}
		return toolDisplay("command", "运行 "+displayTruncate(command, 100))
	case "git":
		return toolDisplay("command", displayGitLabel(args))
	case "web_search":
		query := displayString(args, "query", "q")
		if query == "" {
			return toolDisplay("search", "搜索网页")
		}
		return toolDisplay("search", "搜索网页 "+displayTruncate(query, 90))
	case "web_fetch":
		url := displayString(args, "url")
		if url == "" {
			return toolDisplay("read", "读取网页")
		}
		return toolDisplay("read", "读取网页 "+displayTruncate(url, 100))
	case "load_skill":
		skill := strings.TrimPrefix(displayString(args, "name"), "/")
		if skill == "" {
			return toolDisplay("skill", "加载技能")
		}
		return toolDisplay("skill", "加载技能 "+displayTruncate(skill, 70))
	case "update_plan":
		return toolDisplay("plan", "更新计划")
	case "ask_user":
		return toolDisplay("interaction", "等待用户选择")
	case "spawn_agent":
		return toolDisplay("agent", "启动子任务 "+displayTarget(displayString(args, "task_name"), ""))
	case "send_message":
		return toolDisplay("agent", "发送给子任务 "+displayTarget(displayString(args, "target", "task_name"), ""))
	case "followup_task":
		return toolDisplay("agent", "追加子任务 "+displayTarget(displayString(args, "target", "task_name"), ""))
	case "wait_agent":
		return toolDisplay("agent", "等待子任务 "+displayTarget(displayString(args, "target", "task_name"), ""))
	case "await_agents":
		return toolDisplay("agent", "等待子任务")
	case "close_agent":
		return toolDisplay("agent", "关闭子任务 "+displayTarget(displayString(args, "target", "task_name"), ""))
	case "list_agents":
		return toolDisplay("agent", "查看子任务")
	case "agent_report":
		return toolDisplay("agent", "读取子任务报告 "+displayTarget(displayString(args, "target", "task_name"), ""))
	case "start_process":
		command := displayString(args, "command")
		if command == "" {
			return toolDisplay("command", "启动后台任务")
		}
		return toolDisplay("command", "启动 "+displayTruncate(command, 100))
	case "list_processes":
		return toolDisplay("command", "查看后台任务")
	case "stop_process":
		return toolDisplay("command", "停止后台任务 "+displayTarget(displayString(args, "process_id"), ""))
	case "read_process_output":
		return toolDisplay("command", "读取后台输出 "+displayTarget(displayString(args, "process_id"), ""))
	case "write_stdin":
		return toolDisplay("command", "写入后台输入 "+displayTarget(displayString(args, "process_id"), ""))
	case "schedule_cron":
		cron := displayString(args, "cron")
		if cron == "" {
			return toolDisplay("schedule", "安排定时任务")
		}
		return toolDisplay("schedule", "安排定时任务 "+displayTruncate(cron, 60))
	case "cancel_cron":
		return toolDisplay("schedule", "取消定时任务 "+displayTarget(displayString(args, "id"), ""))
	case "list_cron":
		return toolDisplay("schedule", "查看定时任务")
	default:
		return providers.ToolCallDisplay{
			Kind: displayKindForTool(name),
			Text: fallbackDisplayToolName(name),
		}
	}
}

func toolDisplay(kind, text string) providers.ToolCallDisplay {
	return providers.ToolCallDisplay{Kind: kind, Text: strings.TrimSpace(text)}
}

func displayArgs(raw string) map[string]any {
	args := map[string]any{}
	if err := decodeArgs(raw, &args); err != nil {
		return map[string]any{}
	}
	return args
}

func displayString(args map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := args[key]
		if !ok || value == nil {
			continue
		}
		switch v := value.(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				return s
			}
		case fmt.Stringer:
			if s := strings.TrimSpace(v.String()); s != "" {
				return s
			}
		case float64, bool:
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return ""
}

func displayPathTarget(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	value = strings.ReplaceAll(value, "\\", "/")
	if value == "." || value == "./" {
		return "项目目录"
	}
	value = strings.TrimRight(value, "/")
	base := path.Base(value)
	if base == "." || base == "/" || base == "" {
		return displayTruncate(value, 90)
	}
	return displayTruncate(base, 90)
}

func displaySearchTarget(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "内容"
	}
	value = strings.TrimPrefix(value, "**/")
	value = strings.TrimPrefix(value, "./")
	return displayTruncate(value, 90)
}

func displayTarget(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return displayTruncate(value, 70)
}

func displayTruncate(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func displayGitLabel(args map[string]any) string {
	subcommand := strings.ToLower(displayString(args, "subcommand"))
	switch subcommand {
	case "status":
		return "检查 Git 状态"
	case "diff":
		return "查看代码差异"
	case "show":
		return "查看提交内容"
	case "log":
		return "查看提交历史"
	case "commit":
		return "提交代码"
	case "push":
		return "推送代码"
	case "branch":
		return "查看分支"
	case "ls-files":
		return "查看 Git 文件"
	case "remote":
		return "查看 Git 远端"
	case "":
		return "执行 Git 操作"
	default:
		return "执行 Git " + displayTruncate(subcommand, 40)
	}
}

func displayKindForTool(name string) string {
	switch classifyToolKind(name) {
	case ToolKindFile, ToolKindWeb, ToolKindSkill:
		return "read"
	case ToolKindSearch, ToolKindDiscovery:
		return "search"
	case ToolKindShell, ToolKindGit, ToolKindProcess:
		return "command"
	case ToolKindPlan:
		return "plan"
	case ToolKindUserInteraction:
		return "interaction"
	case ToolKindAgent:
		return "agent"
	case ToolKindSchedule:
		return "schedule"
	default:
		return "tool"
	}
}

func fallbackDisplayToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "调用工具"
	}
	return "调用 " + name
}
