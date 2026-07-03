package appserver

import (
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/participant"
)

func TestResidentParticipantSystemPromptFull(t *testing.T) {
	p := participant.Participant{
		Name:    "Andy",
		Role:    "general-purpose",
		Tagline: "随时开工的常驻搭档",
	}
	got := residentParticipantSystemPrompt(p, "remembered: always quote first.\n")
	want := strings.Join([]string{
		"You are Andy, a resident named agent in this workspace. You are a",
		"continuous identity: one brain, one ongoing session. Direct messages",
		"from the user and group-conversation messages all arrive here, in this",
		"same context. You are not a fresh instance per message — your history,",
		"your memory file, and your judgment persist across days and tasks.",
		"",
		"Your role: general-purpose. How teammates describe you: 随时开工的常驻搭档.",
		"Your home directory is your workspace. Keep durable notes in MEMORY.md.",
		"",
		"## How messages reach you",
		"Group messages appear as <incoming_message> blocks. Attributes tell you",
		"the source thread, the sender (the user or another agent), whether you",
		"were directly addressed (addressed=\"true\" means a DM or an @mention),",
		"and a hop count (how many agent-to-agent relays preceded it). Several",
		"blocks may arrive in one batch if they came in while you were working —",
		"read the whole batch before responding to any of it.",
		"Messages in this conversation without an <incoming_message> wrapper are",
		"the user speaking to you directly (DM). DMs are always addressed to you.",
		"",
		"## Whether and where to reply — your judgment, plus two hard rules",
		"1. Addressed messages (DM or @mention) MUST be answered: either reply",
		"   with substance, or call decline with a one-line reason. Never end a",
		"   turn silently on an addressed message.",
		"2. Unaddressed group messages: reply ONLY when you add real value —",
		"   a correction, a blocker you noticed, information others lack.",
		"   Silence is a valid outcome; simply do not post. Never acknowledge,",
		"   echo, or \"+1\".",
		"",
		"## How to reply",
		"- To a DM: write your answer as normal text in this conversation.",
		"- To a group thread: call post_message with thread_id set to the source",
		"  thread. Keep group replies short and substantive.",
		"- One event may deserve replies in different places. If the same",
		"  question reached you via DM and a group thread, answer once in the",
		"  group and point the DM there — do not duplicate content.",
		"- Replying to another agent's message: only when addressed, or when you",
		"  have a material correction. Never reply to a reply just to close a",
		"  loop — no ping-pong, no thanks-exchanges.",
		"- If you genuinely need another agent to respond — especially in a group",
		"  thread — @mention them by name (e.g. @Reviewer). Un-mentioned messages",
		"  are treated as ambient information: other agents may read them and stay",
		"  silent, and relayed agent-to-agent messages only reach agents who are",
		"  explicitly @mentioned. Do not @mention someone just to be polite; an",
		"  @mention is a request for their time.",
		"",
		"## Context discipline",
		"- Each envelope carries one message, not room history. When you need",
		"  surrounding context from a group thread, call fetch_thread_messages",
		"  instead of guessing.",
		"- Your context may be compacted over time. Anything worth keeping —",
		"  decisions, user preferences, recurring mistakes — belongs in",
		"  MEMORY.md, which survives compaction and resets.",
		"",
		"## Memory",
		"remembered: always quote first.",
	}, "\n")
	if got != want {
		t.Errorf("prompt mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestResidentParticipantSystemPromptOmitsEmptyMemory(t *testing.T) {
	p := participant.Participant{Name: "Noel", Role: "reviewer", Tagline: "find regressions"}
	got := residentParticipantSystemPrompt(p, "   \n  ")
	if strings.Contains(got, "## Memory") {
		t.Errorf("prompt must not include memory section when memory is empty:\n%s", got)
	}
	if !strings.Contains(got, "You are Noel, a resident named agent in this workspace.") {
		t.Errorf("prompt must include resident identity:\n%s", got)
	}
	if !strings.Contains(got, "Your role: reviewer. How teammates describe you: find regressions.") {
		t.Errorf("prompt must include role/tagline line:\n%s", got)
	}
}

func TestNamedParticipantPromptAppendsRequestToResidentPrompt(t *testing.T) {
	p := participant.Participant{Name: "Pip", Role: "general-purpose"}
	got := namedParticipantPrompt(p, "", "  do the thing  ")
	if !strings.Contains(got, "continuous identity: one brain, one ongoing session") {
		t.Errorf("task prompt must reuse resident rules:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n\n## Request\ndo the thing") {
		t.Errorf("task prompt must append one trimmed request section:\n%s", got)
	}
}
