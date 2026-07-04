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
	for _, want := range []string{"HelpMe recovery", "helpme tool", "fresh general-purpose helper", "background", "await_agents", "inception", "bounded recovery summary", "raw parent/helper transcripts", "arrays of short strings", "still not fixed"} {
		if !strings.Contains(content, want) {
			t.Fatalf("rendered prompt missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "automatically compact") {
		t.Fatalf("rendered prompt should not promise automatic compact:\n%s", content)
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

func TestRenderCompactSlashCommandPrompt(t *testing.T) {
	content, display, ok := renderLightweightSlashCommandPrompt("/compact")
	if !ok {
		t.Fatal("expected /compact to render")
	}
	if display != "/compact" {
		t.Fatalf("display = %q", display)
	}
	for _, want := range []string{"compacted at the user's request", "Briefly confirm", "Do not start new work"} {
		if !strings.Contains(content, want) {
			t.Fatalf("rendered prompt missing %q:\n%s", want, content)
		}
	}

	withArgs, _, ok := renderLightweightSlashCommandPrompt("/compact 后续只关注登录模块")
	if !ok {
		t.Fatal("expected /compact with args to render")
	}
	if !strings.Contains(withArgs, "后续只关注登录模块") {
		t.Fatalf("rendered prompt missing user notes:\n%s", withArgs)
	}
}

func TestIsManualCompactPrompt(t *testing.T) {
	cases := []struct {
		prompt string
		want   bool
	}{
		{"/compact", true},
		{"/compact 后续只保留结论", true},
		{"/COMPACT", true},
		{"/compress", true},
		{"//compact", false},
		{"/compaction", false},
		{"compact", false},
		{"/debug compact the logs", false},
		{"  /compact  ", true},
	}
	for _, c := range cases {
		if got := isManualCompactPrompt(c.prompt); got != c.want {
			t.Errorf("isManualCompactPrompt(%q) = %v, want %v", c.prompt, got, c.want)
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
