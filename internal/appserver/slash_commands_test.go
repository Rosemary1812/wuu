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
