package memdir

import (
	"fmt"
	"strings"
)

// Teaching text builders. Wording follows Claude Code's buildMemoryLines
// (thirdparty/claude-code-sourcemap/src/memdir/memdir.ts), condensed per the
// memory-redesign contract §5: four memory types, the two-step save, the
// What-NOT-to-save gate, and the directory-already-exists guidance.

// dirExistsGuidance is shipped because models otherwise burn turns on
// `ls`/`mkdir -p` before writing; the harness guarantees the directory
// exists via EnsureDir.
const dirExistsGuidance = "This directory already exists — write to it directly with the file tools (do not run mkdir or check for its existence)."

var typesSection = []string{
	"## Types of memory",
	"Each memory file declares one `type` in its frontmatter:",
	"- `user` — who the user is: role, goals, preferences, knowledge. Written to make future collaboration better, never as a judgement of the user.",
	"- `feedback` — guidance the user gave you about how to work: corrections AND confirmations. Include *why* so you can judge edge cases later.",
	"- `reference` — pointers to where information lives in external systems (issue trackers, dashboards, channels).",
	"- `lesson` — a lesson learned from your own work that will matter again. Include *why* and how to apply it.",
}

var howToSaveSection = []string{
	"## How to save a memory",
	"Saving a memory is a two-step process:",
	"**Step 1** — write the memory to its own topic file (e.g. `user_role.md`, `feedback_testing.md`) with this frontmatter:",
	"```markdown",
	"---",
	"name: <kebab-case identifier>",
	"description: <one line, specific — used to decide relevance in future conversations>",
	"type: user | feedback | reference | lesson",
	"---",
	"",
	"<memory content — for feedback/lesson types: the rule or fact, then **Why:** and **How to apply:** lines>",
	"```",
	fmt.Sprintf("**Step 2** — add a pointer line to `%s`: `- [Title](file.md) — one-line hook` (one line, under ~150 characters).", EntrypointName),
	fmt.Sprintf("`%s` is an index, not a memory — never write memory content directly into it. Only the index is loaded into your context (truncated past %d lines / %s). Organize memory by topic, not chronologically; update or remove memories that turn out wrong or stale; check for an existing file to update before writing a new one.", EntrypointName, MaxIndexLines, formatByteSize(MaxIndexBytes)),
}

var whatNotToSaveSection = []string{
	"## What NOT to save",
	"- Anything derivable from the current repo or workspace: code patterns, architecture, file paths, git history.",
	"- Task progress, PR numbers, commit SHAs, temporary TODOs, or facts likely to go stale within a week.",
	"- Raw transcripts or activity logs; anything already covered by AGENTS.md files.",
	"These exclusions apply even when the user explicitly asks you to save. If they ask you to save a task log or activity summary, ask what was *surprising* or *non-obvious* about it — that is the part worth keeping.",
}

// SessionTeaching returns the memory teaching block for the main session
// agent, which reads and writes the user notebook at dir.
func SessionTeaching(dir string) string {
	lines := []string{
		"# Memory directory",
		fmt.Sprintf("You have a persistent, file-based memory directory at `%s` — your notebook about the user and the work you do together. %s", dir, dirExistsGuidance),
		"Build it up over time so future sessions know who the user is, how they like to collaborate, what to avoid or repeat, and the context behind their work. If the user explicitly asks you to remember something, save it immediately as whichever type fits best; if they ask you to forget something, find and remove the relevant topic file AND its index line.",
	}
	lines = append(lines, typesSection...)
	lines = append(lines, howToSaveSection...)
	lines = append(lines, whatNotToSaveSection...)
	return strings.Join(lines, "\n")
}

// ResidentTeaching returns the memory teaching block for a resident named
// agent, which reads and writes its identity notebook at dir.
func ResidentTeaching(dir string) string {
	lines := []string{
		"## Memory notebook",
		fmt.Sprintf("Your durable memory is a file-based notebook at `%s` — your identity across days and tasks: lessons learned, feedback received, decisions and their reasons. %s", dir, dirExistsGuidance),
		"Anything worth keeping past this conversation belongs there. If the user asks you to remember something, save it immediately as whichever type fits best; if they ask you to forget something, find and remove the relevant topic file AND its index line.",
	}
	lines = append(lines, shiftHeadings(typesSection)...)
	lines = append(lines, shiftHeadings(howToSaveSection)...)
	lines = append(lines, shiftHeadings(whatNotToSaveSection)...)
	return strings.Join(lines, "\n")
}

// WorkerTeaching returns the read-only user-notebook block for ephemeral
// worker prompts: the index is injected as orientation, and the notebook is
// outside the worker's writable file scope.
func WorkerTeaching(dir string) string {
	return strings.Join([]string{
		"# User memory (read-only)",
		fmt.Sprintf("The `%s` index below comes from the user's memory notebook at `%s` — durable facts about who the user is and how they like to work. Use it to orient your work. The notebook is read-only for you: it is not in your writable file scope, so never create, edit, or delete memory files — leave memory maintenance to the main agent.", EntrypointName, dir),
		"If a remembered fact conflicts with what you observe in the current workspace, trust what you observe now.",
	}, "\n")
}

// UserIndexNotice returns the read-only preamble a resident named agent sees
// above the injected user-notebook index.
func UserIndexNotice() string {
	return strings.Join([]string{
		"The lines below are the index of the user's memory notebook — durable knowledge about who the user is and how they like to work. It is read-only for you: that directory is not in your writable file scope, so never try to edit it; record your own learnings in your identity notebook instead.",
		"If a remembered fact conflicts with what you observe now, trust what you observe.",
	}, "\n")
}

// shiftHeadings demotes `## ` headings one level so a teaching block can sit
// under an existing `## ` section of a larger prompt (the resident persona
// prompt uses `## ` for its own top-level sections).
func shiftHeadings(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") {
			out[i] = "#" + line
		} else {
			out[i] = line
		}
	}
	return out
}
