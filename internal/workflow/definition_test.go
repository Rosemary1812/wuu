package workflow

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseDefinitionFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	content := `---
name: feature-delivery
description: Deliver a feature end to end
when-to-use: Use for product changes that need planning, implementation, review, and QA.
argument-hint: "<feature request>"
user-invocable: false
disable-model-invocation: true
version: 0.1.0
max-agents: 12
max-concurrency: 4
profiles:
  - name: product_planner
    required: true
  - name: frontend_owner
    required: false
  - qa_reviewer
allow-profile-creation: ask
memory-policy: report-candidates-only
---

## Intent

Turn a feature request into shipped behavior.
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	wf, err := parseDefinitionFile(path, "project")
	if err != nil {
		t.Fatalf("parseDefinitionFile: %v", err)
	}
	if wf.Name != "feature-delivery" {
		t.Fatalf("Name = %q", wf.Name)
	}
	if wf.Description != "Deliver a feature end to end" {
		t.Fatalf("Description = %q", wf.Description)
	}
	if wf.WhenToUse == "" || wf.ArgumentHint != "<feature request>" {
		t.Fatalf("usage fields not parsed: %+v", wf)
	}
	if wf.UserInvocable {
		t.Fatal("UserInvocable should be false")
	}
	if !wf.DisableModelInvoke {
		t.Fatal("DisableModelInvoke should be true")
	}
	if wf.Version != "0.1.0" || wf.MaxAgents != 12 || wf.MaxConcurrency != 4 {
		t.Fatalf("version/limits not parsed: %+v", wf)
	}
	wantProfiles := []ProfileRef{
		{Name: "product_planner", Required: true},
		{Name: "frontend_owner"},
		{Name: "qa_reviewer"},
	}
	if !reflect.DeepEqual(wf.Profiles, wantProfiles) {
		t.Fatalf("Profiles = %+v, want %+v", wf.Profiles, wantProfiles)
	}
	if wf.AllowProfileCreation != "ask" || wf.MemoryPolicy != "report-candidates-only" {
		t.Fatalf("policy fields not parsed: %+v", wf)
	}
	if wf.Source != "project" || wf.Path != path || wf.Dir != dir {
		t.Fatalf("source/path fields not parsed: %+v", wf)
	}
	if wf.Content == "" || wf.Content[0] != '\n' {
		t.Fatalf("Content not preserved: %q", wf.Content)
	}
}

func TestParseDefinitionFileDerivesDescriptionAndFallbackName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "release-check")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "WORKFLOW.md")
	content := `---
max-agents: bad
profiles: [release_manager, qa_reviewer]
---

# Release Check

Run release readiness checks.
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	wf, err := parseDefinitionFile(path, "user")
	if err != nil {
		t.Fatalf("parseDefinitionFile: %v", err)
	}
	if wf.Name != "release-check" {
		t.Fatalf("Name = %q", wf.Name)
	}
	if wf.Description != "Release Check" {
		t.Fatalf("Description = %q", wf.Description)
	}
	if wf.MaxAgents != 0 {
		t.Fatalf("MaxAgents = %d, want 0 for invalid value", wf.MaxAgents)
	}
	wantProfiles := []ProfileRef{{Name: "release_manager"}, {Name: "qa_reviewer"}}
	if !reflect.DeepEqual(wf.Profiles, wantProfiles) {
		t.Fatalf("Profiles = %+v, want %+v", wf.Profiles, wantProfiles)
	}
}

func TestParseScriptDefinitionFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dynamic-review")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "WORKFLOW.js")
	content := `// name: dynamic-review
// description: Run a dynamic review team
// when-to-use: Use when multiple reviewers should work in parallel.
// argument-hint: <review scope>
// max-agents: 8
// max-concurrency: 3
// profiles: qa_reviewer, code_reviewer
// allow-profile-creation: ask
// memory-policy: report-candidates-only

phase("Plan", () => {});
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	wf, err := LoadDefinitionFile(path, "project")
	if err != nil {
		t.Fatalf("LoadDefinitionFile: %v", err)
	}
	if wf.Kind != DefinitionKindScript {
		t.Fatalf("Kind = %q", wf.Kind)
	}
	if wf.Name != "dynamic-review" || wf.Description != "Run a dynamic review team" {
		t.Fatalf("metadata not parsed: %+v", wf)
	}
	if wf.WhenToUse == "" || wf.ArgumentHint != "<review scope>" {
		t.Fatalf("usage fields not parsed: %+v", wf)
	}
	if wf.MaxAgents != 8 || wf.MaxConcurrency != 3 {
		t.Fatalf("limits not parsed: %+v", wf)
	}
	wantProfiles := []ProfileRef{{Name: "qa_reviewer"}, {Name: "code_reviewer"}}
	if !reflect.DeepEqual(wf.Profiles, wantProfiles) {
		t.Fatalf("Profiles = %+v, want %+v", wf.Profiles, wantProfiles)
	}
	if wf.AllowProfileCreation != "ask" || wf.MemoryPolicy != "report-candidates-only" {
		t.Fatalf("policy fields not parsed: %+v", wf)
	}
	if wf.Content != content {
		t.Fatalf("script content should be preserved")
	}
}

func TestParseScriptDefinitionFileSupportsFrontmatter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bugfix-smoke")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "WORKFLOW.js")
	content := `---
name: bugfix-smoke
description: Fix a scoped bug through a dynamic workflow.
when_to_use: Use for small implementation bugs.
argument_hint: <bug summary>
max_agents: 3
max_concurrency: 2
profiles: [go_bugfix_worker]
---

phase("Investigate", () => {});
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	wf, err := LoadDefinitionFile(path, "project")
	if err != nil {
		t.Fatalf("LoadDefinitionFile: %v", err)
	}
	if wf.Kind != DefinitionKindScript {
		t.Fatalf("Kind = %q", wf.Kind)
	}
	if wf.Name != "bugfix-smoke" || wf.Description != "Fix a scoped bug through a dynamic workflow." {
		t.Fatalf("frontmatter metadata not parsed: %+v", wf)
	}
	if wf.ArgumentHint != "<bug summary>" || wf.MaxAgents != 3 || wf.MaxConcurrency != 2 {
		t.Fatalf("frontmatter fields not parsed: %+v", wf)
	}
	if !reflect.DeepEqual(wf.Profiles, []ProfileRef{{Name: "go_bugfix_worker"}}) {
		t.Fatalf("Profiles = %+v", wf.Profiles)
	}
	if wf.Content != "\nphase(\"Investigate\", () => {});\n" {
		t.Fatalf("script frontmatter should be stripped from executable content: %q", wf.Content)
	}
}

func TestDiscoverProjectOverridesUserAndFindAllowsSlash(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	userDir := filepath.Join(root, "user")
	if err := os.MkdirAll(filepath.Join(projectDir, "feature"), 0755); err != nil {
		t.Fatalf("MkdirAll project feature: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(userDir, "feature"), 0755); err != nil {
		t.Fatalf("MkdirAll user feature: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "feature", "WORKFLOW.md"), []byte(`---
name: feature
description: user version
---
User body
`), 0644); err != nil {
		t.Fatalf("WriteFile user feature: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "feature", "WORKFLOW.md"), []byte(`---
name: feature
description: project version
---
Project body
`), 0644); err != nil {
		t.Fatalf("WriteFile project feature: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "audit.md"), []byte(`---
description: audit workflow
---
Audit body
`), 0644); err != nil {
		t.Fatalf("WriteFile audit: %v", err)
	}

	workflows := Discover(projectDir, userDir)
	if len(workflows) != 2 {
		t.Fatalf("Discover returned %d workflows: %+v", len(workflows), workflows)
	}
	if workflows[0].Name != "audit" || workflows[1].Name != "feature" {
		t.Fatalf("workflows not sorted by name: %+v", workflows)
	}
	feature, ok := Find(workflows, "/feature")
	if !ok {
		t.Fatal("Find(/feature) failed")
	}
	if feature.Description != "project version" || feature.Source != "project" {
		t.Fatalf("project workflow did not override user workflow: %+v", feature)
	}
}

func TestParseDefinitionFileRequiresFrontmatter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.md")
	if err := os.WriteFile(path, []byte("no frontmatter"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := parseDefinitionFile(path, "project"); err == nil {
		t.Fatal("expected missing frontmatter error")
	}
}
