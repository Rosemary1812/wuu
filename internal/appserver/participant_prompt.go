package appserver

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/workspaces"
)

func residentParticipantSystemPrompt(p participant.Participant, memory string, registered []workspaces.Workspace) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s, a resident named agent in this workspace. You are a\n", strings.TrimSpace(p.Name))
	b.WriteString("continuous identity: one brain, one ongoing session. Direct messages\n")
	b.WriteString("from the user and group-conversation messages all arrive here, in this\n")
	b.WriteString("same context. You are not a fresh instance per message — your history,\n")
	b.WriteString("your memory file, and your judgment persist across days and tasks.\n\n")
	fmt.Fprintf(&b, "Your role: %s. How teammates describe you: %s.\n", strings.TrimSpace(p.Role), strings.TrimSpace(p.Tagline))
	b.WriteString("Your home directory is your workspace. Keep durable notes in MEMORY.md.\n\n")
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
	b.WriteString("2. Unaddressed group messages: reply ONLY when you add real value —\n")
	b.WriteString("   a correction, a blocker you noticed, information others lack.\n")
	b.WriteString("   Silence is a valid outcome; simply do not post. Never acknowledge,\n")
	b.WriteString("   echo, or \"+1\".\n\n")
	b.WriteString("## How to reply\n")
	b.WriteString("- To a DM: write your answer as normal text in this conversation.\n")
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
	// The two sections below are contractual text from
	// docs/plans/2026-07-03-resident-named-agents.md §5 (red line 6: that
	// document is authoritative; edit it before editing this code).
	b.WriteString("## Building teams and groups\n")
	b.WriteString("You can create group threads (create_group) and add named teammates to\n")
	b.WriteString("groups you belong to (add_group_member). Create a group only for an\n")
	b.WriteString("ongoing purpose — a project, a standing topic — never for a one-off\n")
	b.WriteString("question; prefer reusing an existing group. When the user asks for a\n")
	b.WriteString("team, you may also create new named teammates with manage_participant.\n\n")
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
	b.WriteString("  MEMORY.md, which survives compaction and resets.\n")
	if memory = strings.TrimSpace(memory); memory != "" {
		b.WriteString("\n## Memory\n")
		b.WriteString(memory)
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
	var b strings.Builder
	b.WriteString(residentParticipantSystemPrompt(p, memory, registered))
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
	memory, err := s.readParticipantMemory(p)
	if err != nil {
		return "", err
	}
	return residentParticipantSystemPrompt(p, memory, s.registeredWorkspaces()), nil
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

func (s *Server) readParticipantMemory(p participant.Participant) (string, error) {
	workspace := strings.TrimSpace(p.Workspace)
	if workspace == "" && s != nil {
		var err error
		workspace, err = s.participantWorkspace(p.ID)
		if err != nil {
			return "", err
		}
	}
	path := participantMemoryPath(workspace)
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err == nil {
		return string(data), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return "", fmt.Errorf("read participant memory: %w", err)
}
