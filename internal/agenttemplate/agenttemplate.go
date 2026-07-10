// Package agenttemplate discovers reusable Claude Code-style subagent
// templates without turning them into persistent Wuu participants.
package agenttemplate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type SourceDir struct {
	Path   string
	Source string
}

type Template struct {
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Instructions   string            `json:"instructions"`
	Source         string            `json:"source"`
	Path           string            `json:"path"`
	Model          string            `json:"model,omitempty"`
	PermissionMode string            `json:"permission_mode,omitempty"`
	Effort         string            `json:"effort,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type Diagnostic struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type Discovery struct {
	Templates   []Template   `json:"templates"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

func DiscoverSourceDirs(projectDirs, userDirs []SourceDir) Discovery {
	byName := make(map[string]Template)
	var diagnostics []Diagnostic
	for _, sourceDir := range append(append([]SourceDir(nil), userDirs...), projectDirs...) {
		templates, diags := discoverDir(sourceDir)
		diagnostics = append(diagnostics, diags...)
		for _, template := range templates {
			byName[template.Name] = template
		}
	}

	templates := make([]Template, 0, len(byName))
	for _, template := range byName {
		templates = append(templates, template)
	}
	sort.Slice(templates, func(i, j int) bool {
		if templates[i].Name == templates[j].Name {
			return templates[i].Path < templates[j].Path
		}
		return templates[i].Name < templates[j].Name
	})
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Path == diagnostics[j].Path {
			return diagnostics[i].Message < diagnostics[j].Message
		}
		return diagnostics[i].Path < diagnostics[j].Path
	})
	return Discovery{Templates: templates, Diagnostics: diagnostics}
}

func Find(templates []Template, name string) (Template, bool) {
	name = strings.TrimSpace(name)
	for _, template := range templates {
		if template.Name == name {
			return template, true
		}
	}
	return Template{}, false
}

func discoverDir(sourceDir SourceDir) ([]Template, []Diagnostic) {
	dir := strings.TrimSpace(sourceDir.Path)
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []Diagnostic{{Path: dir, Message: err.Error()}}
	}

	var templates []Template
	var diagnostics []Diagnostic
	for _, entry := range entries {
		if entry.IsDir() || strings.EqualFold(entry.Name(), "README.md") || strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		template, err := parseFile(path, sourceDir.Source)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		templates = append(templates, template)
	}
	return templates, diagnostics
}

func parseFile(path, source string) (Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Template{}, err
	}
	frontmatter, body, err := splitFrontmatter(string(data))
	if err != nil {
		return Template{}, err
	}
	if strings.TrimSpace(body) == "" {
		return Template{}, fmt.Errorf("template instructions are required")
	}

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(frontmatter), &raw); err != nil {
		return Template{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	metadata := scalarMetadata(raw)
	name := strings.TrimSpace(metadata["name"])
	if name == "" {
		return Template{}, fmt.Errorf("frontmatter name is required")
	}
	description := strings.TrimSpace(metadata["description"])
	if description == "" {
		return Template{}, fmt.Errorf("frontmatter description is required")
	}
	delete(metadata, "name")
	delete(metadata, "description")

	return Template{
		Name:           name,
		Description:    description,
		Instructions:   strings.TrimSpace(body),
		Source:         normalizedSource(source),
		Path:           path,
		Model:          strings.TrimSpace(metadata["model"]),
		PermissionMode: strings.TrimSpace(metadata["permissionMode"]),
		Effort:         strings.TrimSpace(metadata["effort"]),
		Metadata:       metadata,
	}, nil
}

func splitFrontmatter(content string) (string, string, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", "", fmt.Errorf("YAML frontmatter is required")
	}
	rest := normalized[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", fmt.Errorf("unterminated YAML frontmatter")
	}
	after := rest[end+len("\n---"):]
	if after != "" && !strings.HasPrefix(after, "\n") {
		return "", "", fmt.Errorf("invalid YAML frontmatter closing delimiter")
	}
	return rest[:end], strings.TrimPrefix(after, "\n"), nil
}

func scalarMetadata(raw map[string]any) map[string]string {
	out := make(map[string]string)
	for key, value := range raw {
		switch typed := value.(type) {
		case string:
			out[key] = strings.TrimSpace(typed)
		case bool, int, int64, uint64, float64:
			out[key] = fmt.Sprint(typed)
		}
	}
	return out
}

func normalizedSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "user"
	}
	return source
}
