// Package prompt implements a section-based system prompt builder.
//
// Static sections (base prompt, coordinator preamble) are placed first
// so the prompt prefix stays stable across turns — maximising provider
// prompt-cache hit rates. Dynamic sections (memory, skills, git
// context) follow.
//
// Memory files are truncated to MaxMemoryLines / MaxMemoryBytes to
// prevent prompt explosion from large AGENTS.md or CLAUDE.md files.
// Aligned with Claude Code's 200-line / 25 KB caps.
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
	Static  bool // true = part of the stable cache prefix
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
	sb.WriteString("# Memory\n\n")
	sb.WriteString("The following memory files contain project- and user-defined conventions, ")
	sb.WriteString("style guides, and constraints. Treat them as binding instructions for this session.\n\n")
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
	sb.WriteString("You have bounded, profile-scoped persistent memory across sessions for this named agent. ")
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
		sb.WriteString("The following entries were captured at session start. Mid-session writes update disk but do not change this snapshot until the next session.\n\n")
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
	sb.WriteString("The following skills are available in this session. Each skill is a reusable, ")
	sb.WriteString("project- or user-defined instruction set that encodes conventions, recipes, or workflows.\n\n")
	sb.WriteString("**How to use skills:**\n")
	sb.WriteString("1. Read the skill catalog below — match the user's intent against each skill's description and \"when to use\" guidance.\n")
	sb.WriteString("2. When a skill applies, call the `load_skill` tool with the skill's name to retrieve the full body. ")
	sb.WriteString("Pass any user-supplied arguments via the `arguments` parameter.\n")
	sb.WriteString("3. Follow the loaded skill's instructions exactly. If the skill body contains tool restrictions or step orderings, respect them.\n")
	sb.WriteString("4. Users can also invoke skills directly by typing `/<skill-name>` (e.g. `/commit`). When that happens, the skill body is injected as a user message — no need to call `load_skill` separately.\n\n")
	sb.WriteString("**Skill catalog:**\n\n")
	for _, s := range visible {
		desc := s.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&sb, "- **%s**: %s", s.Name, desc)
		if s.WhenToUse != "" {
			fmt.Fprintf(&sb, "\n  _When to use:_ %s", s.WhenToUse)
		}
		if s.ArgumentHint != "" {
			fmt.Fprintf(&sb, "\n  _Arguments:_ `%s`", s.ArgumentHint)
		}
		sb.WriteString("\n")
	}
	b.AddSection("skills", strings.TrimRight(sb.String(), "\n"), false)
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
	sb.WriteString("The following workflows are available in this session. A workflow is a reusable, durable, multi-agent process. ")
	sb.WriteString("Use workflows for long-running, scheduled, repeatable, or multi-phase work where chat context should not be the only state store.\n\n")
	sb.WriteString("**How to use workflows:**\n")
	sb.WriteString("1. Match the user's intent against the workflow catalog below.\n")
	sb.WriteString("2. When a workflow applies, call `load_workflow` with its name and user arguments to inspect the full definition.\n")
	sb.WriteString("3. Call `create_workflow` to create a durable Workflow Run with phases and a plan.\n")
	sb.WriteString("4. Use `spawn_agent` for actual work. Set `agent_profile` only for durable named profiles; omit it for temporary workers.\n")
	sb.WriteString("5. Use `workflow_control` to record phase and Agent Run status, and `workflow_status` to inspect progress before synthesis.\n")
	sb.WriteString("6. Use `workflow_control` pause_run, resume_run, and retry_agent_run for blocked recovery instead of hiding recovery state in prose.\n")
	sb.WriteString("7. Use workflow file checkpoints before risky direct edits and restore_file_checkpoint for scoped rollback.\n")
	sb.WriteString("8. Use `save_workflow` when an ad hoc plan should become a reusable project or user workflow definition.\n")
	sb.WriteString("9. For scheduled workflow-shaped tasks, use `schedule_cron` with workflow_name so the scheduler can start a Workflow Run.\n")
	sb.WriteString("10. Record long-term memory candidates through `workflow_control`; do not write profile memory directly from ordinary workflow progress.\n\n")
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
		if len(wf.Profiles) > 0 {
			fmt.Fprintf(&sb, "\n  _Profiles:_ %s", workflowProfileNames(wf.Profiles))
		}
		sb.WriteString("\n")
	}
	b.AddSection("workflows", strings.TrimRight(sb.String(), "\n"), false)
}

// AddGitContext adds git status information as a dynamic section.
func (b *Builder) AddGitContext(gitInfo string) {
	if strings.TrimSpace(gitInfo) == "" {
		return
	}
	b.AddSection("git_context", "# Git Context\n\n"+gitInfo, false)
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
