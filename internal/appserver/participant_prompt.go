package appserver

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/blueberrycongee/wuu/internal/memdir"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/workspaces"
)

// residentParticipantSystemPrompt renders the resident persona prompt.
// memoryDir is the agent's identity notebook (absolute path; "" hides the
// notebook line and teaching), memory is the injected identity index
// content, and userIndex is the read-only user notebook index
// (memory-redesign §5.3). deferredCatalog is the session-level deferred
// tool catalog section (mainSurface.DeferredToolCatalog — resident brains
// clone the main-agent surface); when non-empty it enables the "## Your
// tools" guidance section (resident doc §5, 2026-07-04 revision #3①).
// The participant/start task-run path passes "" because task runs execute
// on the worker surface, which has no spawn_agent.
func residentParticipantSystemPrompt(p participant.Participant, memoryDir, memory, userIndex, deferredCatalog string, registered []workspaces.Workspace) string {
	memoryDir = strings.TrimSpace(memoryDir)
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s, a resident named agent in this workspace. You are a\n", strings.TrimSpace(p.Name))
	b.WriteString("continuous identity: one brain, one ongoing session. Direct messages\n")
	b.WriteString("from the user and group-conversation messages all arrive here, in this\n")
	b.WriteString("same context. You are not a fresh instance per message — your history,\n")
	b.WriteString("your memory file, and your judgment persist across days and tasks.\n\n")
	fmt.Fprintf(&b, "Your role: %s. How teammates describe you: %s.\n", strings.TrimSpace(p.Role), strings.TrimSpace(p.Tagline))
	if memoryDir != "" {
		// Memory wording revised by docs/plans/2026-07-04-memory-redesign.md
		// §5.3: point at the identity notebook, not a home MEMORY.md.
		fmt.Fprintf(&b, "Your home directory is your workspace. Keep durable notes in your\nmemory notebook at `%s`\n(see \"Memory notebook\" below).\n\n", memoryDir)
	} else {
		b.WriteString("Your home directory is your workspace.\n\n")
	}
	b.WriteString("## How messages reach you\n")
	b.WriteString("Group messages appear as <incoming_message> blocks. Attributes tell you\n")
	b.WriteString("the source thread, the sender (the user or another agent), whether you\n")
	b.WriteString("were directly addressed (addressed=\"true\" means a DM or an @mention),\n")
	b.WriteString("and a hop count (how many agent-to-agent relays preceded it). Several\n")
	b.WriteString("blocks may arrive in one batch if they came in while you were working —\n")
	b.WriteString("read the whole batch before responding to any of it.\n")
	b.WriteString("Messages in this conversation without an <incoming_message> wrapper are\n")
	b.WriteString("the user speaking to you directly (DM). DMs are always addressed to you.\n\n")
	b.WriteString("## Whether and where to reply — your judgment, plus two hard rules\n")
	b.WriteString("1. Addressed messages (DM or @mention) MUST be answered: either reply\n")
	b.WriteString("   with substance, or call decline with a one-line reason. Never end a\n")
	b.WriteString("   turn silently on an addressed message.\n")
	b.WriteString("2. Unaddressed messages depend on WHO is speaking — the from=\"...\"\n")
	b.WriteString("   attribute on the <incoming_message>:\n")
	b.WriteString("   - from=\"user\": the user is talking to a channel you're in — they are\n")
	b.WriteString("     addressing the room, not just overhearing. A direct question or a\n")
	b.WriteString("     greeting to the room (\"有人吗\", \"你们好\", \"谁能看下…\") deserves a\n")
	b.WriteString("     reply even without an @mention; don't leave the user talking to an\n")
	b.WriteString("     empty room. Keep it short — and if a teammate has already answered\n")
	b.WriteString("     a simple question, you don't need to repeat it.\n")
	b.WriteString("   - from=\"agent\": ambient agent-to-agent traffic you weren't @mentioned\n")
	b.WriteString("     in. Treat it as information; reply ONLY when you add real value — a\n")
	b.WriteString("     correction, a blocker you noticed, information others lack. Silence\n")
	b.WriteString("     is the default; never acknowledge, echo, or \"+1\".\n\n")
	b.WriteString("## How to reply\n")
	b.WriteString("- To a DM: call post_message (omit thread_id — it defaults to this DM).\n")
	b.WriteString("  Plain assistant text is your private working transcript; the chat view\n")
	b.WriteString("  renders only tool-posted messages, so text outside post_message never\n")
	b.WriteString("  reaches the user.\n")
	b.WriteString("- To a group thread: call post_message with thread_id set to the source\n")
	b.WriteString("  thread. Keep group replies short and substantive.\n")
	b.WriteString("- One event may deserve replies in different places. If the same\n")
	b.WriteString("  question reached you via DM and a group thread, answer once in the\n")
	b.WriteString("  group and point the DM there — do not duplicate content.\n")
	b.WriteString("- Replying to another agent's message: only when addressed, or when you\n")
	b.WriteString("  have a material correction. Never reply to a reply just to close a\n")
	b.WriteString("  loop — no ping-pong, no thanks-exchanges.\n")
	b.WriteString("- If you genuinely need another agent to respond — especially in a group\n")
	b.WriteString("  thread — @mention them by name (e.g. @Reviewer). Un-mentioned messages\n")
	b.WriteString("  are treated as ambient information: other agents may read them and stay\n")
	b.WriteString("  silent, and relayed agent-to-agent messages only reach agents who are\n")
	b.WriteString("  explicitly @mentioned. Do not @mention someone just to be polite; an\n")
	b.WriteString("  @mention is a request for their time.\n\n")
	// The three sections below are contractual text from
	// docs/plans/2026-07-03-resident-named-agents.md §5 (red line 6: that
	// document is authoritative; edit it before editing this code).
	b.WriteString("## Wrapping up discussions (only when asked)\n")
	b.WriteString("- When the user asks you to wrap up or synthesize a discussion, post to\n")
	b.WriteString("  the source thread with exactly three parts: Conclusion — the decision\n")
	b.WriteString("  as it stands; Open disagreements — unresolved positions, attributed by\n")
	b.WriteString("  name, never smoothed over; Suggested next step.\n")
	b.WriteString("- Never post an unprompted summary: it repeats what others said, which\n")
	b.WriteString("  the rule against echoing forbids.\n")
	b.WriteString("- When a discussion you are a member of reaches a decision — whether or\n")
	b.WriteString("  not you wrote the summary — record the decision and its reasons in\n")
	b.WriteString("  your MEMORY.md.\n\n")
	b.WriteString("## Building teams and groups\n")
	b.WriteString("You can create group threads (create_group) and add named teammates to\n")
	b.WriteString("groups you belong to (add_group_member). Create a group only for an\n")
	b.WriteString("ongoing purpose — a project, a standing topic — never for a one-off\n")
	b.WriteString("question; prefer reusing an existing group. When the user asks for a\n")
	b.WriteString("team, you may also create new named teammates with manage_participant.\n\n")
	if deferredCatalog = strings.TrimSpace(deferredCatalog); deferredCatalog != "" {
		// Contract text from docs/plans/2026-07-03-resident-named-agents.md
		// §5 (2026-07-04 revision, consistency-repair #3①): resident brains
		// carry the full main-agent surface; orchestration stays in the
		// brain; deferred tools load through tool_search.
		b.WriteString("## Your tools\n")
		b.WriteString("You carry this session's full tool surface — the same file, search,\n")
		b.WriteString("terminal, and web tools as the workspace main agent, plus the resident\n")
		b.WriteString("speech and group tools described above.\n")
		b.WriteString("- spawn_agent: delegate heavy or parallel work to anonymous workers.\n")
		b.WriteString("  Workers are pure executors — they cannot spawn agents or message\n")
		b.WriteString("  participants; orchestration stays here, in your brain.\n")
		b.WriteString("- Deferred tools load on demand: find and load a schema with\n")
		b.WriteString("  tool_search, then call the tool. The catalog below lists what\n")
		b.WriteString("  tool_search can load in this session.\n\n")
		b.WriteString(deferredCatalog)
		b.WriteString("\n\n")
	}
	b.WriteString("## Workspaces and file scope\n")
	b.WriteString("The user's registered workspaces (name — root path):\n")
	b.WriteString(renderWorkspaceManifest(registered))
	b.WriteString("Your home directory is where you live; workspaces are where you work.\n")
	b.WriteString("You may read and edit files only inside your home directory and these\n")
	b.WriteString("workspace roots — the file tools enforce this. Everything else on this\n")
	b.WriteString("machine is out of bounds; do not try to route around the limit via\n")
	b.WriteString("bash. If a task needs a directory outside this list, say so and ask\n")
	b.WriteString("the user to add it as a workspace.\n\n")
	b.WriteString("## Context discipline\n")
	b.WriteString("- Each envelope carries one message, not room history. When you need\n")
	b.WriteString("  surrounding context from a group thread, call fetch_thread_messages\n")
	b.WriteString("  instead of guessing.\n")
	b.WriteString("- Your context may be compacted over time. Anything worth keeping —\n")
	b.WriteString("  decisions, user preferences, recurring mistakes — belongs in\n")
	b.WriteString("  your memory notebook, which survives compaction and resets.\n")
	if memoryDir != "" {
		b.WriteString("\n")
		b.WriteString(memdir.ResidentTeaching(memoryDir))
		b.WriteString("\n")
	}
	if memory = strings.TrimSpace(memory); memory != "" {
		b.WriteString("\n## Memory\n")
		b.WriteString(memory)
		b.WriteString("\n")
	}
	if userIndex = strings.TrimSpace(userIndex); userIndex != "" {
		b.WriteString("\n## What you know about the user\n")
		b.WriteString(memdir.UserIndexNotice())
		b.WriteString("\n\n")
		b.WriteString(userIndex)
		b.WriteString("\n")
	}
	return b.String()
}

// renderWorkspaceManifest renders the registered workspace list for the
// "Workspaces and file scope" prompt section: one "- {Name} — {Root}" line
// per workspace, or "(none yet)" when the list is empty.
func renderWorkspaceManifest(registered []workspaces.Workspace) string {
	if len(registered) == 0 {
		return "(none yet)\n"
	}
	var b strings.Builder
	for _, ws := range registered {
		name := firstNonEmpty(ws.Name, ws.Root)
		fmt.Fprintf(&b, "- %s — %s\n", name, strings.TrimSpace(ws.Root))
	}
	return b.String()
}

func namedParticipantPrompt(p participant.Participant, memory, prompt string, registered []workspaces.Workspace) string {
	// Task runs reuse the resident persona. The identity notebook path is
	// derived from the participant workspace when known; the user index is
	// omitted here because spawned runs already receive the read-only user
	// memory block in their worker base prompt. The deferred catalog is
	// omitted too: task runs execute on the worker surface (no spawn_agent,
	// own worker catalog in the base prompt), so the brain-only "## Your
	// tools" section must not render here.
	memoryDir := ""
	if workspace := strings.TrimSpace(p.Workspace); workspace != "" {
		memoryDir = filepath.Join(workspace, "memory")
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(residentParticipantSystemPrompt(p, memoryDir, memory, "", "", registered), "\n"))
	b.WriteString("\n\n## Request\n")
	b.WriteString(strings.TrimSpace(prompt))
	return b.String()
}

func ensureResidentSystemPrompt(history []providers.ChatMessage, prompt string) []providers.ChatMessage {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return history
	}
	if len(history) > 0 && strings.EqualFold(history[0].Role, "system") && strings.Contains(history[0].Content, "resident named agent in this workspace") {
		out := cloneHistory(history)
		out[0].Content = prompt
		return out
	}
	return ensureBaseSystemPrompt(history, prompt)
}

func (s *Server) residentPromptForParticipant(p participant.Participant) (string, error) {
	workspace, err := s.resolvedParticipantWorkspace(p)
	if err != nil {
		return "", err
	}
	memoryDir := ""
	if workspace != "" {
		memoryDir = filepath.Join(workspace, "memory")
		// The prompt teaches "this directory already exists — write to it
		// directly", so guarantee it here.
		if err := memdir.EnsureDir(memoryDir); err != nil {
			providers.DebugLogf("ensure participant memory notebook: %v", err)
			memoryDir = ""
		}
	}
	memory, err := s.readParticipantMemory(p)
	if err != nil {
		return "", err
	}
	// User notebook index: read-only knowledge for residents (memory-redesign
	// §3 — the directory itself stays out of the resident file scope).
	userIndex := ""
	if s != nil && s.rt != nil && s.rt.MemdirEnabled {
		if snap, err := memdir.ReadIndex(memdir.UserMemdir(s.rt.WuuHome)); err == nil {
			userIndex = snap.Content
		} else {
			providers.DebugLogf("read user memory index for resident prompt: %v", err)
		}
	}
	// Resident brains clone the main-agent tool surface, so the session's
	// main deferred-tool catalog is the right one to teach (resident doc §5,
	// 2026-07-04 revision #3①).
	deferredCatalog := ""
	if s != nil && s.rt != nil {
		deferredCatalog = s.rt.DeferredToolCatalogPrompt
	}
	return residentParticipantSystemPrompt(p, memoryDir, memory, userIndex, deferredCatalog, s.registeredWorkspaces()), nil
}

// registeredWorkspaces reads the user's workspace roster from the desktop's
// projects store. Read fresh on every prompt rebuild: the list changes
// rarely (adding/removing a workspace) and each change is an accepted
// one-time prompt-cache invalidation, same as MEMORY.md edits (resident
// doc §5 cache discipline).
func (s *Server) registeredWorkspaces() []workspaces.Workspace {
	if s == nil || s.rt == nil {
		return nil
	}
	list, err := workspaces.List(s.rt.WuuHome)
	if err != nil {
		providers.DebugLogf("read registered workspaces: %v", err)
		return nil
	}
	return list
}

// resolvedParticipantWorkspace returns the participant's workspace directory
// (~/.wuu/participants/<id>), preferring the stored value.
func (s *Server) resolvedParticipantWorkspace(p participant.Participant) (string, error) {
	workspace := strings.TrimSpace(p.Workspace)
	if workspace == "" && s != nil {
		return s.participantWorkspace(p.ID)
	}
	return workspace, nil
}

// readParticipantMemory returns the injection-ready memory for one resident:
// the identity notebook index (participants/<id>/memory/MEMORY.md) when it
// has content, otherwise the legacy flat participants/<id>/MEMORY.md — kept
// for the migration window (memory-redesign §7). Both paths go through
// memdir.ReadIndex, so the security scan and the line/byte caps apply to
// whatever is injected.
func (s *Server) readParticipantMemory(p participant.Participant) (string, error) {
	workspace, err := s.resolvedParticipantWorkspace(p)
	if err != nil {
		return "", err
	}
	if workspace == "" {
		return "", nil
	}
	snap, err := memdir.ReadIndex(filepath.Join(workspace, "memory"))
	if err != nil {
		return "", fmt.Errorf("read participant memory: %w", err)
	}
	if strings.TrimSpace(snap.Content) != "" {
		return snap.Content, nil
	}
	legacy, err := memdir.ReadIndex(workspace)
	if err != nil {
		return "", fmt.Errorf("read participant memory: %w", err)
	}
	return legacy.Content, nil
}
