package appserver

import (
	"strings"
	"testing"
)

func TestRenderLightweightSlashCommandPrompt(t *testing.T) {
	content, display, ok := renderLightweightSlashCommandPrompt("/debug login failure")
	if !ok {
		t.Fatal("expected /debug to render")
	}
	if display != "/debug login failure" {
		t.Fatalf("display = %q", display)
	}
	for _, want := range []string{"Investigate this problem", "login failure", "root cause"} {
		if !strings.Contains(content, want) {
			t.Fatalf("rendered prompt missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "/debug") {
		t.Fatalf("rendered prompt should not include raw slash command:\n%s", content)
	}
}

func TestRenderHelpMeSlashCommandPrompt(t *testing.T) {
	content, display, ok := renderLightweightSlashCommandPrompt("/helpme still not fixed after three tries")
	if !ok {
		t.Fatal("expected /helpme to render")
	}
	if display != "/helpme still not fixed after three tries" {
		t.Fatalf("display = %q", display)
	}
	for _, want := range []string{"HelpMe recovery", "helpme tool", "fresh general-purpose helper", "automatically compact", "arrays of short strings", "still not fixed"} {
		if !strings.Contains(content, want) {
			t.Fatalf("rendered prompt missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "/helpme") {
		t.Fatalf("rendered prompt should not include raw slash command:\n%s", content)
	}
}

func TestRenderCommitSlashCommandPromptIsSurfaceNeutral(t *testing.T) {
	content, _, ok := renderLightweightSlashCommandPrompt("/commit polish summary")
	if !ok {
		t.Fatal("expected /commit to render")
	}
	for _, want := range []string{"repository commit", "active model surface", "prepared message"} {
		if !strings.Contains(content, want) {
			t.Fatalf("commit slash prompt missing %q:\n%s", want, content)
		}
	}
	for _, banned := range []string{"git", "bash", "run_shell", "run_test", "start_process"} {
		if strings.Contains(content, banned) {
			t.Fatalf("commit slash prompt must not teach command path %q:\n%s", banned, content)
		}
	}
}

func TestRenderLightweightSlashCommandPromptKeepsSkillSlashRaw(t *testing.T) {
	content, display, ok := renderLightweightSlashCommandPrompt("/slides quarterly roadmap")
	if ok || display != "" || content != "/slides quarterly roadmap" {
		t.Fatalf("skill slash should remain raw, got content=%q display=%q ok=%v", content, display, ok)
	}
}

func TestRenderLightweightSlashCommandPromptIgnoresEscapedSlash(t *testing.T) {
	content, display, ok := renderLightweightSlashCommandPrompt("//debug login failure")
	if ok || display != "" || content != "//debug login failure" {
		t.Fatalf("escaped slash should remain raw, got content=%q display=%q ok=%v", content, display, ok)
	}
}
