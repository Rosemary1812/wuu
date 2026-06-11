package workflow

import (
	"strings"
	"testing"
)

func TestMergeWithBundledAddsComposeWorkflow(t *testing.T) {
	got := MergeWithBundled(nil)
	compose, ok := Find(got, "compose")
	if !ok {
		t.Fatalf("compose workflow missing: %+v", got)
	}
	if compose.Source != "bundled" || compose.Kind != DefinitionKindMarkdown {
		t.Fatalf("unexpected compose workflow: %+v", compose)
	}
	if !strings.Contains(compose.Content, "session_memory") || !strings.Contains(compose.Content, "spawn_agent") {
		t.Fatalf("compose workflow missing orchestration guidance:\n%s", compose.Content)
	}
}

func TestMergeWithBundledKeepsDiscoveredOverride(t *testing.T) {
	got := MergeWithBundled([]Definition{{
		Name:        "compose",
		Description: "Project compose",
		Source:      "project",
	}})
	if len(got) != 1 {
		t.Fatalf("definitions = %+v", got)
	}
	if got[0].Description != "Project compose" || got[0].Source != "project" {
		t.Fatalf("discovered compose should override bundled: %+v", got[0])
	}
}
