package agenttemplate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSourceDirsParsesTemplatesAndProjectOverridesUser(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "project-agents")
	userDir := filepath.Join(t.TempDir(), "user-agents")

	writeTemplate(t, filepath.Join(userDir, "reviewer.md"), `---
name: reviewer
description: User reviewer
model: claude-sonnet
permissionMode: acceptEdits
effort: high
tools: Read, Grep
---
Review from the user template.
`)
	writeTemplate(t, filepath.Join(projectDir, "reviewer.md"), `---
name: reviewer
description: Project reviewer
model: claude-opus
permissionMode: plan
effort: medium
tools: Read, Grep, Bash
---
Review from the project template.
`)
	writeTemplate(t, filepath.Join(userDir, "researcher.md"), `---
name: researcher
description: Research a topic
---
Gather evidence before reporting.
`)

	discovery := DiscoverSourceDirs(
		[]SourceDir{{Path: projectDir, Source: "project"}},
		[]SourceDir{{Path: userDir, Source: "user"}},
	)
	if len(discovery.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %+v", discovery.Diagnostics)
	}
	if len(discovery.Templates) != 2 {
		t.Fatalf("Templates = %+v", discovery.Templates)
	}

	reviewer, ok := Find(discovery.Templates, "reviewer")
	if !ok {
		t.Fatal("reviewer template not found")
	}
	if reviewer.Source != "project" || reviewer.Description != "Project reviewer" {
		t.Fatalf("project template did not override user template: %+v", reviewer)
	}
	if reviewer.Instructions != "Review from the project template." {
		t.Fatalf("Instructions = %q", reviewer.Instructions)
	}
	if reviewer.Model != "claude-opus" || reviewer.PermissionMode != "plan" || reviewer.Effort != "medium" {
		t.Fatalf("frontmatter metadata not preserved: %+v", reviewer)
	}
	if reviewer.Metadata["tools"] != "Read, Grep, Bash" {
		t.Fatalf("Metadata = %+v", reviewer.Metadata)
	}

	researcher, ok := Find(discovery.Templates, "researcher")
	if !ok || researcher.Source != "user" {
		t.Fatalf("researcher = %+v, ok=%v", researcher, ok)
	}
}

func TestDiscoverSourceDirsReportsInvalidTemplates(t *testing.T) {
	dir := t.TempDir()
	writeTemplate(t, filepath.Join(dir, "missing-body.md"), `---
name: missing-body
description: Missing body
---
`)
	writeTemplate(t, filepath.Join(dir, "missing-name.md"), `---
description: Missing name
---
Body.
`)
	writeTemplate(t, filepath.Join(dir, "broken.md"), `---
name: [broken
---
Body.
`)
	writeTemplate(t, filepath.Join(dir, "README.md"), "ignored")

	discovery := DiscoverSourceDirs(
		[]SourceDir{{Path: dir, Source: "project"}},
		nil,
	)
	if len(discovery.Templates) != 0 {
		t.Fatalf("Templates = %+v", discovery.Templates)
	}
	if len(discovery.Diagnostics) != 3 {
		t.Fatalf("Diagnostics = %+v", discovery.Diagnostics)
	}
	for _, diagnostic := range discovery.Diagnostics {
		if diagnostic.Path == "" || diagnostic.Message == "" {
			t.Fatalf("invalid diagnostic: %+v", diagnostic)
		}
	}
}

func writeTemplate(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
