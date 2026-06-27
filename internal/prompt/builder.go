// Package prompt implements a section-based system prompt builder.
//
// Static sections (base prompt, coordinator preamble) are placed first
// so the prompt prefix stays stable across turns, maximizing provider
// prompt-cache hit rates. Session-scoped discovered sections such as
// memory, skills, and workflows follow. Volatile environment and git
// state belong in per-turn context injection, not in this builder.
//
// Memory files are truncated to MaxMemoryLines / MaxMemoryBytes to prevent
// prompt explosion from large project instruction files.
package prompt

import (
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/memory"
	"github.com/blueberrycongee/wuu/internal/memory/store"
	"github.com/blueberrycongee/wuu/internal/skills"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

const (
	// MaxMemoryLines caps a single memory file at 200 lines.
	MaxMemoryLines = 200
	// MaxMemoryBytes caps a single memory file at 25 KB.
	MaxMemoryBytes = 25 * 1024
	// MaxProfileMemoryChars caps the agent-notes snapshot injected into
	// the system prompt.
	MaxProfileMemoryChars = 2200
	// MaxUserProfileMemoryChars caps the user-profile snapshot injected into
	// the system prompt.
	MaxUserProfileMemoryChars = 1375
)

// Section is one logical piece of the system prompt.
type Section struct {
	Key     string // unique identifier for dedup / replacement
	Content string
	Static  bool // true = part of the fixed built-in prefix
}

// Builder assembles the final system prompt from sections.
type Builder struct {
	sections []Section
}

// AddSection appends a named section. Duplicate keys overwrite.
func (b *Builder) AddSection(key, content string, static bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	for i := range b.sections {
		if b.sections[i].Key == key {
			b.sections[i] = Section{Key: key, Content: content, Static: static}
			return
		}
	}
	b.sections = append(b.sections, Section{Key: key, Content: content, Static: static})
}

// AddMemory adds a "Memory" section from discovered memory files,
// applying per-file truncation.
func (b *Builder) AddMemory(files []memory.File) {
	if len(files) == 0 {
		return
	}
	var sb strings.Builder
	sb.WriteString("# Workspace instructions and memory\n\n")
	sb.WriteString("The following markdown files were discovered for this session. ")
	sb.WriteString("Instruction files may contain conventions, style guides, and constraints; follow them unless they conflict with higher-priority system, developer, or tool rules. ")
	sb.WriteString("Durable MEMORY.md files are saved context and facts; use them to orient yourself, but verify time-sensitive or repo-specific details against the current workspace before acting. ")
	sb.WriteString("When files overlap, prefer the more specific local or project instruction for that workspace, and do not treat old memory as live evidence without checking it.\n\n")
	for _, f := range files {
		content := TruncateMemory(f.Content, MaxMemoryLines, MaxMemoryBytes)
		fmt.Fprintf(&sb, "## %s _[%s · %s]_\n\n", f.Name, f.Source, f.Path)
		sb.WriteString(strings.TrimRight(content, "\n"))
		sb.WriteString("\n\n")
	}
	b.AddSection("memory", strings.TrimRight(sb.String(), "\n"), false)
}

// AddProfileMemory adds durable, agent-profile-scoped memory guidance and a
// frozen snapshot of saved entries. Mid-session writes update the store but do
// not mutate this prompt; a new session receives a fresh snapshot.
func (b *Builder) AddProfileMemory(entries []store.Entry) {
	b.AddProfileMemoryWithLimits(entries, MaxProfileMemoryChars, MaxUserProfileMemoryChars)
}

// AddProfileMemoryWithLimits is AddProfileMemory with caller-provided target
// budgets. Zero or negative limits fall back to the built-in defaults.
func (b *Builder) AddProfileMemoryWithLimits(entries []store.Entry, memoryChars, userChars int) {
	if memoryChars <= 0 {
		memoryChars = MaxProfileMemoryChars
	}
	if userChars <= 0 {
		userChars = MaxUserProfileMemoryChars
	}
	var sb strings.Builder
	sb.WriteString("# Persistent Memory\n\n")
	sb.WriteString("You have a bounded, profile-scoped markdown memory document across sessions for this named agent. ")
	sb.WriteString("Use `write_memory` to save compact durable facts and `read_memory` to retrieve them when needed.\n\n")
	sb.WriteString("**When to save:**\n")
	sb.WriteString("- The user corrects you, says to remember or stop doing something, or shares a durable preference.\n")
	sb.WriteString("- You learn a stable environment fact, project convention, tool quirk, or recurring workflow that will matter in future sessions.\n")
	sb.WriteString("- The fact would reduce future user steering or prevent the same correction from being repeated.\n\n")
	sb.WriteString("**What not to save:**\n")
	sb.WriteString("- Task progress, completed-work logs, temporary TODOs, PR numbers, issue numbers, commit SHAs, raw dumps, or facts likely to go stale within a week.\n")
	sb.WriteString("- Information that is easy to re-read from the current repo or belongs in a reusable skill/workflow instead.\n\n")
	sb.WriteString("**Targets:**\n")
	sb.WriteString("- `target=\"user\"`: who the user is, their preferences, communication style, expectations, and recurring corrections.\n")
	sb.WriteString("- `target=\"memory\"`: this agent's notes, such as environment facts, project conventions, tool quirks, and lessons learned.\n\n")
	sb.WriteString("Write memories as declarative facts, not instructions to yourself. ")
	sb.WriteString("Use `write_memory` with `action=\"replace\"` or `action=\"remove\"` when an existing memory becomes wrong, stale, unwanted, or the target is near its character limit. ")
	sb.WriteString("For example, write \"User prefers concise Chinese replies\", not \"Always reply concisely\".\n")

	userBlock := renderProfileMemoryTarget(entries, "user", userChars)
	memoryBlock := renderProfileMemoryTarget(entries, "memory", memoryChars)
	if userBlock != "" || memoryBlock != "" {
		sb.WriteString("\n## Current Profile Memory Snapshot\n\n")
		sb.WriteString("The following entries were captured at session start. Mid-session writes update the MEMORY.md document and index but do not change this snapshot until the next session.\n\n")
		if userBlock != "" {
			sb.WriteString("### User Profile\n\n")
			sb.WriteString(userBlock)
			sb.WriteString("\n\n")
		}
		if memoryBlock != "" {
			sb.WriteString("### Agent Notes\n\n")
			sb.WriteString(memoryBlock)
			sb.WriteString("\n")
		}
	}
	b.AddSection("profile_memory", strings.TrimRight(sb.String(), "\n"), false)
}

// AddSkills adds a "Skills" section from discovered skills.
func (b *Builder) AddSkills(sks []skills.Skill) {
	visible := make([]skills.Skill, 0, len(sks))
	for _, s := range sks {
		if s.DisableModelInvoke {
			continue
		}
		visible = append(visible, s)
	}
	if len(visible) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString("# Session-specific guidance\n\n")
	sb.WriteString("## Skills\n\n")
	sb.WriteString("Skills provide specialized instructions and workflows for specific tasks.\n")
	sb.WriteString("Use the `load_skill` tool to load a skill when a task matches its description.\n")
	sb.WriteString("Users can also invoke skills directly by typing `/<skill-name>` (e.g. `/docs`). When that happens, treat the text after the command as the skill arguments and load the matching skill before acting.\n\n")
	sb.WriteString(skills.FormatAvailable(visible, true))
	b.AddSection("skills", strings.TrimRight(sb.String(), "\n"), false)
}

// AddToolDiscovery teaches the model the stable contract for deferred
// tool schemas. The section is static so progressive tool loading does
// not mutate the system prompt prefix across turns.
func (b *Builder) AddToolDiscovery() {
	b.AddSection("tool_discovery", strings.Join([]string{
		"# Tool Discovery",
		"",
		"Some less common tool schemas are deferred so the direct tool list stays small and cacheable.",
		"- Use `tool_search` when you need a capability that is not currently visible, especially MCP tools, workflows, scheduling, subagents, memory, web access, or specialized search helpers.",
		"- Search by capability words, or use `select:<tool_name>` when you already know the exact tool name.",
		"- After `tool_search` returns matching schemas, use the loaded tool normally in the next tool step if it fits the task.",
		"- Do not use `tool_search` for visible core tools already listed in this session, such as file reading, file editing, grep/glob search, patching, planning, or skill loading.",
		"- Do not call MCP list/resource tools only to discover available tools; use `tool_search` for tool discovery.",
	}, "\n"), true)
}

// AddWorkflows adds reusable workflow definitions to session guidance.
func (b *Builder) AddWorkflows(workflows []workflow.Definition) {
	visible := make([]workflow.Definition, 0, len(workflows))
	for _, wf := range workflows {
		if wf.DisableModelInvoke {
			continue
		}
		visible = append(visible, wf)
	}
	if len(visible) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString("# Workflow guidance\n\n")
	sb.WriteString("## Workflows\n\n")
	sb.WriteString("The following workflows are available in this session. A workflow records a multi-step run with durable state for phases, workers, reports, and recovery. ")
	sb.WriteString("Use workflows when durable state, scheduled execution, repeatability, or multiple phases/workers matter; otherwise work directly. Background script workflows may still need an active runtime to keep executing.\n\n")
	sb.WriteString("**Decision rules:**\n")
	sb.WriteString("- Use the lightest durable boundary. If one workflow run is the user-visible objective, call `start_workflow` directly; it creates or binds the Goal state for that run.\n")
	sb.WriteString("- Call `create_goal` when the user-visible objective needs an active runtime Goal that can keep pushing after an individual model turn stops, or when it is broader than one workflow run or child task, such as a program of work with multiple workflows, approvals, retries, or later resumption. Runtime Goals own continuation; workflow and subagent reports are evidence.\n")
	sb.WriteString("- Use `get_goal` to inspect the current Goal. Use `update_goal` only to mark the Goal `complete` or `blocked`; do not use it for progress notes, decisions, pause, cancel, edit, or limit states.\n")
	sb.WriteString("- A completed workflow is evidence for a broader Goal, not automatic completion of that Goal. Use `get_goal` before `update_goal` with status `complete`.\n")
	sb.WriteString("- For delegated or multi-agent Goals, completion needs independent workflow, subagent, or reviewer evidence. Do not self-certify completion from the lead agent's own claim.\n\n")
	sb.WriteString("**Entry point:**\n")
	sb.WriteString("- Workflow tools may be deferred to keep the default tool list small. If a workflow tool named below is not visible in the current tool list, call `tool_search` with `select:<tool_name>` first, then call the loaded tool.\n")
	sb.WriteString("- Match the user's intent against the workflow catalog below. When a workflow applies, call `load_workflow` with its name and user arguments before starting it.\n")
	sb.WriteString("- Start new workflow work with `start_workflow`. The `driver` argument defaults to `auto`; use `driver=\"auto\"` unless the user, workflow, or recovery path explicitly requires an override.\n")
	sb.WriteString("- Use the returned `driver`, tool descriptions, tool result `next_steps`, `workflow_status`, and workflow evidence `goal_id`/`goal_dir` as the source of truth for the exact next action. Use `workflow_control` only after a Workflow Run exists and the run state needs to record planning, worker output, recovery, or final synthesis.\n")
	sb.WriteString("- Use `save_workflow` when an ad hoc script or plan should become reusable. Use `schedule_cron` with workflow_name when a saved workflow should run on a schedule.\n\n")
	sb.WriteString("**Workflow catalog:**\n\n")
	for _, wf := range visible {
		desc := wf.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&sb, "- **%s**: %s", wf.Name, desc)
		if wf.WhenToUse != "" {
			fmt.Fprintf(&sb, "\n  _When to use:_ %s", wf.WhenToUse)
		}
		if wf.ArgumentHint != "" {
			fmt.Fprintf(&sb, "\n  _Arguments:_ `%s`", wf.ArgumentHint)
		}
		if wf.Kind != "" {
			fmt.Fprintf(&sb, "\n  _Kind:_ %s", wf.Kind)
		}
		if len(wf.Profiles) > 0 {
			fmt.Fprintf(&sb, "\n  _Profiles:_ %s", workflowProfileNames(wf.Profiles))
		}
		sb.WriteString("\n")
	}
	b.AddSection("workflows", strings.TrimRight(sb.String(), "\n"), false)
}

func workflowProfileNames(profiles []workflow.ProfileRef) string {
	names := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			continue
		}
		if profile.Required {
			name += " (required)"
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// Build returns the assembled system prompt. Static sections appear
// first (sorted by insertion order), then dynamic sections.
func (b *Builder) Build() string {
	var statics, dynamics []string
	for _, s := range b.sections {
		if s.Static {
			statics = append(statics, s.Content)
		} else {
			dynamics = append(dynamics, s.Content)
		}
	}
	all := append(statics, dynamics...)
	return strings.Join(all, "\n\n")
}

// TruncateMemory caps content at maxLines and maxBytes, whichever
// limit is hit first. Appends a marker if truncation occurred.
func TruncateMemory(content string, maxLines, maxBytes int) string {
	if len(content) <= maxBytes && countLines(content) <= maxLines {
		return content
	}

	lines := strings.SplitAfter(content, "\n")
	var b strings.Builder
	lineCount := 0
	for _, line := range lines {
		if lineCount >= maxLines || b.Len()+len(line) > maxBytes {
			omitted := len(lines) - lineCount
			fmt.Fprintf(&b, "\n[truncated — %d lines omitted]", omitted)
			return b.String()
		}
		b.WriteString(line)
		lineCount++
	}
	return b.String()
}

func countLines(s string) int {
	n := strings.Count(s, "\n")
	if len(s) > 0 && s[len(s)-1] != '\n' {
		n++
	}
	return n
}

func renderProfileMemoryTarget(entries []store.Entry, target string, maxChars int) string {
	var sb strings.Builder
	written := 0
	chars := 0
	for _, entry := range entries {
		if profileMemoryTarget(entry) != target {
			continue
		}
		line := renderProfileMemoryEntry(entry)
		if line == "" {
			continue
		}
		lineChars := len([]rune("- " + line + "\n"))
		if maxChars > 0 && chars+lineChars > maxChars {
			if written == 0 {
				return "[profile memory truncated]"
			}
			sb.WriteString("- [profile memory truncated]\n")
			break
		}
		sb.WriteString("- ")
		sb.WriteString(line)
		sb.WriteString("\n")
		chars += lineChars
		written++
	}
	return strings.TrimRight(sb.String(), "\n")
}

func renderProfileMemoryEntry(entry store.Entry) string {
	content := strings.TrimSpace(strings.Join(strings.Fields(entry.Content), " "))
	if content == "" {
		return ""
	}
	var meta []string
	if tags := profileMemoryTags(entry.Tags); len(tags) > 0 {
		meta = append(meta, "tags: "+strings.Join(tags, ", "))
	}
	if entry.Source != "" {
		meta = append(meta, "source: "+string(entry.Source))
	}
	if len(meta) == 0 {
		return content
	}
	return content + " _[" + strings.Join(meta, "; ") + "]_"
}

func profileMemoryTarget(entry store.Entry) string {
	for _, tag := range entry.Tags {
		switch strings.TrimSpace(tag) {
		case "target:user":
			return "user"
		case "target:memory":
			return "memory"
		}
	}
	return "memory"
}

func profileMemoryTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || strings.HasPrefix(tag, "target:") {
			continue
		}
		out = append(out, tag)
	}
	return out
}
