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
