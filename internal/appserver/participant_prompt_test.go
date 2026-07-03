package appserver

import (
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/participant"
)

func TestNamedParticipantPromptFull(t *testing.T) {
	p := participant.Participant{
		Name:    "Andy",
		Role:    "general-purpose",
		Tagline: "随时开工的常驻搭档",
	}
	got := namedParticipantPrompt(p, "remembered: always quote first.\n", "ship the demo")
	want := strings.Join([]string{
		"You are Andy, a long-running named agent in this workspace. Your role is general-purpose.",
		"How your teammates describe you: 随时开工的常驻搭档",
		"",
		"## Your memory",
		"Notes you kept from previous work. Trust them, but verify anything that may have gone stale:",
		"",
		"remembered: always quote first.",
		"",
		"## Request",
		"ship the demo",
		"",
		"If the user asks you to set up, adjust, or retire other named agents, use the manage_participant tool instead of describing the steps.",
		"",
		"When you are done, post your conclusion with post_message (kind=result). If you are blocked on the user, ask with post_message (kind=question). If no response is actually needed, call decline with a one-line reason. Never end the turn silently.",
	}, "\n")
	if got != want {
		t.Errorf("prompt mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestNamedParticipantPromptNoMemory(t *testing.T) {
	p := participant.Participant{
		Name:    "Noel",
		Role:    "reviewer",
		Tagline: "find regressions",
	}
	got := namedParticipantPrompt(p, "   \n  ", "audit the diff")
	if strings.Contains(got, "## Your memory") {
		t.Errorf("prompt must not include the memory block when memory is empty/whitespace:\n%s", got)
	}
	if !strings.Contains(got, "## Request") {
		t.Errorf("prompt must contain ## Request section:\n%s", got)
	}
	if !strings.Contains(got, "audit the diff") {
		t.Errorf("prompt must include the request body:\n%s", got)
	}
	if !strings.Contains(got, "You are Noel, a long-running named agent in this workspace. Your role is reviewer.") {
		t.Errorf("prompt must include role line:\n%s", got)
	}
	if !strings.Contains(got, "How your teammates describe you: find regressions") {
		t.Errorf("prompt must include tagline line:\n%s", got)
	}
}

func TestNamedParticipantPromptNoRoleNoTagline(t *testing.T) {
	p := participant.Participant{Name: "Pip"}
	got := namedParticipantPrompt(p, "", "  do the thing  ")
	if !strings.HasPrefix(got, "You are Pip, a long-running named agent in this workspace.\n") {
		t.Errorf("prompt prefix wrong:\n%s", got)
	}
	if strings.Contains(got, "Your role is") {
		t.Errorf("prompt must omit role line when role is empty:\n%s", got)
	}
	if strings.Contains(got, "How your teammates describe you") {
		t.Errorf("prompt must omit tagline line when tagline is empty:\n%s", got)
	}
	if !strings.Contains(got, "## Request\ndo the thing\n") {
		t.Errorf("prompt must contain trimmed request line:\n%s", got)
	}
}

func TestNamedParticipantPromptIdentityFirstPrefixIsStable(t *testing.T) {
	// The identity-first prefix should not include timestamps or other volatile
	// content that would defeat KV caching across runs.
	p := participant.Participant{Name: "Andy", Role: "general-purpose", Tagline: "ready"}
	got := namedParticipantPrompt(p, "", "task")
	prefix := strings.SplitN(got, "## Request", 2)[0]
	for _, banned := range []string{"2026", "2025", "UTC", "now", "today"} {
		if strings.Contains(prefix, banned) {
			t.Errorf("prefix contains volatile token %q:\n%s", banned, prefix)
		}
	}
}
