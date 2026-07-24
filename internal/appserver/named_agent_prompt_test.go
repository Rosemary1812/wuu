package appserver

import (
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/channels"
)

func TestNamedAgentOrientationDistinguishesHomeFromProjectScope(t *testing.T) {
	agent := channels.NamedAgent{
		ID:        "agent-1",
		Name:      "Andy",
		MemoryDir: "/agents/agent-1/memory",
		CreatedAt: time.Now(),
	}
	prompt := namedAgentOrientation(agent)
	for _, want := range []string{
		"agent home is your private identity and state anchor; it is not the limit",
		"supplied as request-only environment context",
		"Use an absolute file path or set a command's cwd",
		"Do not claim that you can only access your agent home",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("orientation missing %q:\n%s", want, prompt)
		}
	}
}

func TestNamedAgentOrientationExcludesProjectlessConversations(t *testing.T) {
	prompt := namedAgentOrientation(channels.NamedAgent{
		Name: "Andy", MemoryDir: "/agents/agent-1/memory",
	})
	if !strings.Contains(prompt, "Projectless conversation sessions are not project workspaces") {
		t.Fatalf("orientation should explain empty project scope:\n%s", prompt)
	}
}

func TestNamedAgentOrientationDefinesSingleOwnerClaimProtocol(t *testing.T) {
	prompt := namedAgentOrientation(channels.NamedAgent{
		Name: "Andy", MemoryDir: "/agents/agent-1/memory",
	})
	for _, want := range []string{
		"Treat the room as shared coordination state",
		"keep your intent, ownership, meaningful progress, handoffs, and completion visible",
		"Before taking work, look for overlapping activity",
		"Work that should produce one shared result has one owner",
		"other agents must not claim or execute that work",
		"Read the latest relevant messages across the room",
		"A room-stream request must be claimed in the room stream",
		"Do not use reply_to to move the claim into a different scope",
		"A committed chat message only declares intent",
		"treat that as losing the claim race",
		"resolve the draft silent",
		"After the claim commits, call chat_check",
		"lowest room-global message sequence wins",
		"promptly publish meaningful changes in status",
		"Before delivering or applying the final result",
		"Do not claim on another agent's behalf",
		"explicitly requests multiple independent answers",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("orientation missing claim rule %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "editing shared files, committing, pushing, deploying") {
		t.Fatalf("orientation should define a general collaboration model instead of enumerating actions:\n%s", prompt)
	}
}
