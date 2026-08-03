package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/channels"
)

func TestShellCommandEnvDropsLauncherGOROOT(t *testing.T) {
	env := shellCommandEnv([]string{
		"PATH=/usr/bin",
		"GOROOT=/tmp/launcher-toolchain",
		"HOME=/home/test",
	})

	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	if _, ok := values["GOROOT"]; ok {
		t.Fatalf("shell environment leaked launcher GOROOT: %q", values["GOROOT"])
	}
	if values["PATH"] != "/usr/bin" || values["HOME"] != "/home/test" {
		t.Fatalf("shell environment lost inherited user values: %+v", values)
	}
	if values["GIT_TERMINAL_PROMPT"] != "0" || values["PAGER"] != "cat" {
		t.Fatalf("shell environment lost non-interactive overrides: %+v", values)
	}
}

func TestShellCommandEnvMarksNamedAgentSubprocess(t *testing.T) {
	service, err := channels.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open channels service: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	credential, err := service.CreateNamedAgent(context.Background(), channels.CreateNamedAgentParams{Name: "Andy"})
	if err != nil {
		t.Fatalf("create named agent: %v", err)
	}
	client, err := service.BindAgent(context.Background(), credential.Agent.ID)
	if err != nil {
		t.Fatalf("bind named agent: %v", err)
	}

	got := shellCommandEnvForTool([]string{channels.NamedAgentIDEnv + "=stale"}, &Env{ChatAgent: client})
	values := make(map[string]string, len(got))
	for _, entry := range got {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	if values[channels.NamedAgentIDEnv] != credential.Agent.ID {
		t.Fatalf("named agent marker = %q, want %q", values[channels.NamedAgentIDEnv], credential.Agent.ID)
	}

	got = shellCommandEnvForTool([]string{channels.NamedAgentIDEnv + "=stale"}, &Env{})
	for _, entry := range got {
		key, _, ok := strings.Cut(entry, "=")
		if ok && key == channels.NamedAgentIDEnv {
			t.Fatalf("ordinary tool environment retained named agent marker: %q", entry)
		}
	}
}
