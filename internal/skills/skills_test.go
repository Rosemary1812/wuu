package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commit.md")

	content := "---\nname: /commit\ndescription: Create a git commit\n---\nThis skill creates commits.\nWith multiple lines."
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	skill, err := parseSkillFile(path, "project")
	if err != nil {
		t.Fatalf("parseSkillFile: %v", err)
	}
	if canonicalName(skill.Name) != "commit" {
		t.Fatalf("unexpected name: %q", skill.Name)
	}
	if skill.Description != "Create a git commit" {
		t.Fatalf("unexpected description: %q", skill.Description)
	}
	if skill.Source != "project" {
		t.Fatalf("unexpected source: %q", skill.Source)
	}
	if skill.Content != "This skill creates commits.\nWith multiple lines." {
		t.Fatalf("unexpected content: %q", skill.Content)
	}
}

func TestParseSkillFile_LoopMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "loop.md")

	content := strings.Join([]string{
		"---",
		"name: loop",
		"description: Run a durable loop",
		"trigger-condition: long-running task",
		"allowed-tools: [read_file, run_test]",
		"required-context: [state, failures]",
		"examples: [continue loop, recover failed task]",
		"verification-checklist: [state persisted, verifier passed]",
		"progressive-disclosure: load state first",
		"---",
		"Body.",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	skill, err := parseSkillFile(path, "project")
	if err != nil {
		t.Fatalf("parseSkillFile: %v", err)
	}
	if skill.TriggerCondition != "long-running task" {
		t.Fatalf("TriggerCondition = %q", skill.TriggerCondition)
	}
	if got := strings.Join(skill.RequiredContext, ","); got != "state,failures" {
		t.Fatalf("RequiredContext = %q", got)
	}
	if got := strings.Join(skill.Examples, ","); got != "continue loop,recover failed task" {
		t.Fatalf("Examples = %q", got)
	}
	if got := strings.Join(skill.VerificationChecklist, ","); got != "state persisted,verifier passed" {
		t.Fatalf("VerificationChecklist = %q", got)
	}
	if skill.ProgressiveDisclosure != "load state first" {
		t.Fatalf("ProgressiveDisclosure = %q", skill.ProgressiveDisclosure)
	}
}

func TestParseSkillFile_NoName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.md")

	content := "---\ndescription: Review code\n---\nBody here."
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	skill, err := parseSkillFile(path, "user")
	if err != nil {
		t.Fatalf("parseSkillFile: %v", err)
	}
	// Should fall back to filename.
	if canonicalName(skill.Name) != "review" {
		t.Fatalf("expected review, got %q", skill.Name)
	}
}

func TestParseSkillFile_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.md")

	if err := os.WriteFile(path, []byte("no frontmatter here"), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	_, err := parseSkillFile(path, "project")
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestDiscover(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()

	// Create project skill.
	if err := os.WriteFile(
		filepath.Join(projectDir, "build.md"),
		[]byte("---\nname: /build\ndescription: Build project\n---\nBuild body."),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	// Create user skill that overrides a project skill.
	if err := os.WriteFile(
		filepath.Join(userDir, "build.md"),
		[]byte("---\nname: /build\ndescription: User build override\n---\nUser body."),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	// Create another user skill.
	if err := os.WriteFile(
		filepath.Join(userDir, "deploy.md"),
		[]byte("---\nname: /deploy\ndescription: Deploy\n---\nDeploy body."),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	skills := Discover(projectDir, userDir)

	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	// Skills should be sorted by name.
	if skills[0].Name != "build" || skills[1].Name != "deploy" {
		t.Fatalf("unexpected skill order: %v, %v", skills[0].Name, skills[1].Name)
	}

	// build should be the project version (project overrides user).
	if skills[0].Description != "Build project" {
		t.Fatalf("expected project description for build, got %q", skills[0].Description)
	}
}

func TestDiscover_EmptyDirs(t *testing.T) {
	skills := Discover("", "")
	if len(skills) != 0 {
		t.Fatalf("expected 0 skills for empty dirs, got %d", len(skills))
	}
}

func TestDiscoverSourceDirsPreservesSourceLabels(t *testing.T) {
	userPluginDir := t.TempDir()
	projectDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(userPluginDir, "compose.md"),
		[]byte("---\nname: compose\ndescription: Plugin compose\n---\nPlugin body."),
		0644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(projectDir, "local.md"),
		[]byte("---\nname: local\ndescription: Local skill\n---\nLocal body."),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	got := DiscoverSourceDirs(
		[]SourceDir{{Path: projectDir, Source: "project"}},
		[]SourceDir{{Path: userPluginDir, Source: "plugin:compose"}},
	)
	if len(got) != 2 {
		t.Fatalf("skills = %+v", got)
	}
	compose, ok := Find(got, "compose")
	if !ok {
		t.Fatal("compose skill not found")
	}
	if compose.Source != "plugin:compose" {
		t.Fatalf("compose.Source = %q", compose.Source)
	}
}

func TestRegistryRouteUsesLoopMetadata(t *testing.T) {
	registry := NewRegistry([]Skill{
		{
			Name:             "codebase-research",
			Description:      "Read code and summarize constraints",
			TriggerCondition: "Use when asked to inspect architecture or find relevant files",
		},
		{
			Name:             "browser-task",
			Description:      "Verify browser behavior",
			TriggerCondition: "Use for browser smoke tests and DOM checks",
			Paths:            []string{"desktop/**/*.tsx"},
		},
		{
			Name:             "release-check",
			Description:      "Prepare a release gate",
			TriggerCondition: "Use before publishing builds",
		},
	})

	routed := registry.Route("please inspect the architecture and find relevant files", []string{"internal/agent/run.go"})
	if len(routed) == 0 || routed[0].Name != "codebase-research" {
		t.Fatalf("unexpected route result: %+v", routed)
	}

	routed = registry.Route("run a browser smoke test and DOM check", []string{"desktop/src/renderer/App.tsx"})
	if len(routed) == 0 || routed[0].Name != "browser-task" {
		t.Fatalf("unexpected browser route result: %+v", routed)
	}
}

func TestBundledSkillsIncludesLoopEngineeringSkills(t *testing.T) {
	items := BundledSkills()
	for _, name := range []string{"codebase-research", "long-running-workflow", "diff-review"} {
		skill, ok := Find(items, name)
		if !ok {
			t.Fatalf("bundled skill %q not found in %+v", name, items)
		}
		if skill.Source != "bundled" {
			t.Fatalf("%s Source = %q", name, skill.Source)
		}
		if len(skill.VerificationChecklist) == 0 {
			t.Fatalf("%s missing verification checklist", name)
		}
	}
}
