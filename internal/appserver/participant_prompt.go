package appserver

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func residentParticipantSystemPrompt(p participant.Participant, memory string) string {
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

func namedParticipantPrompt(p participant.Participant, memory, prompt string) string {
	var b strings.Builder
	b.WriteString(residentParticipantSystemPrompt(p, memory))
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
	return residentParticipantSystemPrompt(p, memory), nil
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
