package workflow

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	DefinitionKindMarkdown = "markdown"
	DefinitionKindScript   = "script"

	ProjectWorkflowDir = ".wuu/workflows"
	LegacyWorkflowDir  = ".claude/workflows"
)

// Definition is a reusable workflow asset discovered from project or user
// workflow directories. It describes orchestration intent; a Definition is not
// a running workflow instance.
type Definition struct {
	Name        string
	Description string
	WhenToUse   string
	Kind        string
	Content     string
	Source      string
	Path        string
	Dir         string

	ArgumentHint         string
	UserInvocable        bool
	DisableModelInvoke   bool
	Version              string
	MaxAgents            int
	MaxConcurrency       int
	Profiles             []ProfileRef
	AllowProfileCreation string
	MemoryPolicy         string
}

// ProfileRef describes a named Agent Profile that a workflow can use.
type ProfileRef struct {
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
}

type SourceDir struct {
	Path   string
	Source string
}

// Discover scans one user and one project workflow directory. Project
// workflows override user workflows with the same name.
func Discover(projectDir, userDir string) []Definition {
	return DiscoverDirs([]string{projectDir}, []string{userDir})
}

// DiscoverDirs scans workflow directories from lowest to highest precedence:
// user dirs in order, then project dirs in order. Callers should pass legacy
// dirs before native dirs when both should be supported.
func DiscoverDirs(projectDirs, userDirs []string) []Definition {
	return DiscoverSourceDirs(sourceDirs(projectDirs, "project"), sourceDirs(userDirs, "user"))
}

// DiscoverSourceDirs is DiscoverDirs with per-directory source labels.
func DiscoverSourceDirs(projectDirs, userDirs []SourceDir) []Definition {
	byName := make(map[string]Definition)
	for _, dir := range userDirs {
		for _, wf := range scanDir(dir.Path, sourceLabel(dir.Source)) {
			byName[wf.Name] = wf
		}
	}
	for _, dir := range projectDirs {
		for _, wf := range scanDir(dir.Path, sourceLabel(dir.Source)) {
			byName[wf.Name] = wf
		}
	}

	result := make([]Definition, 0, len(byName))
	for _, wf := range byName {
		result = append(result, wf)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func sourceDirs(paths []string, source string) []SourceDir {
	out := make([]SourceDir, 0, len(paths))
	for _, path := range paths {
		out = append(out, SourceDir{Path: path, Source: source})
	}
	return out
}

func sourceLabel(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "unknown"
	}
	return source
}

func ProjectWorkflowPath(rootDir string) string {
	if strings.TrimSpace(rootDir) == "" {
		return ""
	}
	return filepath.Join(rootDir, ProjectWorkflowDir)
}

func LegacyProjectWorkflowPath(rootDir string) string {
	if strings.TrimSpace(rootDir) == "" {
		return ""
	}
	return filepath.Join(rootDir, LegacyWorkflowDir)
}

func UserWorkflowPath(wuuHome string) string {
	if strings.TrimSpace(wuuHome) == "" {
		return ""
	}
	return filepath.Join(wuuHome, "workflows")
}

func LegacyUserWorkflowPath(homeDir string) string {
	if strings.TrimSpace(homeDir) == "" {
		return ""
	}
	return filepath.Join(homeDir, LegacyWorkflowDir)
}

// Find returns a workflow by name. A leading slash is tolerated.
func Find(workflows []Definition, name string) (Definition, bool) {
	name = canonicalName(name)
	for _, wf := range workflows {
		if wf.Name == name {
			return wf, true
		}
	}
	return Definition{}, false
}

func scanDir(dir, source string) []Definition {
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var workflows []Definition
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			workflowFile := findWorkflowFile(path)
			if workflowFile == "" {
				continue
			}
			wf, parseErr := loadDefinitionFile(workflowFile, source)
			if parseErr != nil {
				continue
			}
			wf.Name = canonicalName(wf.Name)
			wf.Dir = path
			workflows = append(workflows, wf)
			continue
		}

		if !isDefinitionFile(entry.Name()) {
			continue
		}
		wf, parseErr := loadDefinitionFile(path, source)
		if parseErr != nil {
			continue
		}
		wf.Name = canonicalName(wf.Name)
		wf.Dir = filepath.Dir(path)
		workflows = append(workflows, wf)
	}
	return workflows
}

func findWorkflowFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, target := range []string{"WORKFLOW.js", "WORKFLOW.md"} {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.EqualFold(entry.Name(), target) {
				return filepath.Join(dir, entry.Name())
			}
		}
	}
	return ""
}

func isDefinitionFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".js"
}

func LoadDefinitionFile(path, source string) (Definition, error) {
	return loadDefinitionFile(path, source)
}

func loadDefinitionFile(path, source string) (Definition, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js":
		return parseScriptDefinitionFile(path, source)
	default:
		return parseDefinitionFile(path, source)
	}
}

func parseScriptDefinitionFile(path, source string) (Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, err
	}
	return parseScriptDefinitionContent(string(data), filepath.Base(path), source, filepath.Dir(path), path), nil
}

func parseScriptDefinitionContent(content, filename, source, dir, path string) Definition {
	body := content
	wf := Definition{
		Kind:          DefinitionKindScript,
		Source:        source,
		Path:          path,
		Dir:           dir,
		UserInvocable: true,
	}
	if frontmatter, stripped, ok := splitLeadingFrontmatter(content); ok {
		parseFrontmatter(&wf, frontmatter)
		body = stripped
	}
	wf.Content = body
	parseScriptMetadata(&wf, body)
	if wf.Name == "" {
		if strings.EqualFold(filename, "WORKFLOW.js") {
			wf.Name = filepath.Base(dir)
		} else {
			wf.Name = strings.TrimSuffix(filename, filepath.Ext(filename))
		}
	}
	wf.Name = canonicalName(wf.Name)
	if wf.Description == "" {
		wf.Description = firstScriptDescription(body)
	}
	return wf
}

func parseScriptMetadata(wf *Definition, content string) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "//") {
			key, value, ok := splitScriptMetadataLine(strings.TrimSpace(strings.TrimPrefix(line, "//")))
			if ok {
				applyDefinitionMetadata(wf, key, value)
			}
			continue
		}
		if strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*") || strings.HasPrefix(line, "*/") {
			continue
		}
		break
	}
}

func splitScriptMetadataLine(line string) (key, value string, ok bool) {
	key, value, ok = splitAnyYAMLLine(line)
	if !ok {
		return "", "", false
	}
	if !scriptMetadataKeys[strings.ToLower(key)] {
		return "", "", false
	}
	return key, value, true
}

var scriptMetadataKeys = map[string]bool{
	"name":                     true,
	"description":              true,
	"when-to-use":              true,
	"when_to_use":              true,
	"argument-hint":            true,
	"argument_hint":            true,
	"user-invocable":           true,
	"user_invocable":           true,
	"disable-model-invocation": true,
	"disable_model_invocation": true,
	"version":                  true,
	"max-agents":               true,
	"max_agents":               true,
	"max-concurrency":          true,
	"max_concurrency":          true,
	"profiles":                 true,
	"allow-profile-creation":   true,
	"allow_profile_creation":   true,
	"memory-policy":            true,
	"memory_policy":            true,
}

var scriptDescriptionPattern = regexp.MustCompile(`^\s*//\s*(.+?)\s*$`)

func firstScriptDescription(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		matches := scriptDescriptionPattern.FindStringSubmatch(line)
		if len(matches) != 2 {
			break
		}
		key, _, ok := splitScriptMetadataLine(matches[1])
		if ok && key != "" {
			continue
		}
		desc := strings.TrimSpace(matches[1])
		if desc == "" {
			continue
		}
		if len(desc) > 200 {
			desc = desc[:200] + "..."
		}
		return desc
	}
	return ""
}

func parseDefinitionFile(path, source string) (Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, err
	}
	return parseDefinitionContent(string(data), filepath.Base(path), source, filepath.Dir(path), path)
}

func parseDefinitionContent(content, filename, source, dir, path string) (Definition, error) {
	frontmatter, body, ok := splitLeadingFrontmatter(content)
	if !ok {
		return Definition{}, fmt.Errorf("no frontmatter")
	}

	wf := Definition{
		Kind:          DefinitionKindMarkdown,
		Source:        source,
		Path:          path,
		Dir:           dir,
		UserInvocable: true,
	}
	parseFrontmatter(&wf, frontmatter)
	if wf.Name == "" {
		if strings.EqualFold(filename, "WORKFLOW.md") {
			wf.Name = filepath.Base(dir)
		} else {
			wf.Name = strings.TrimSuffix(filename, filepath.Ext(filename))
		}
	}
	wf.Name = canonicalName(wf.Name)
	wf.Content = body
	if wf.Description == "" {
		wf.Description = firstMarkdownLine(wf.Content)
	}
	return wf, nil
}

func splitLeadingFrontmatter(content string) (frontmatter []string, body string, ok bool) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, content, false
	}
	closeIndex := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIndex = i
			break
		}
	}
	if closeIndex < 0 {
		return nil, content, false
	}
	return lines[1:closeIndex], strings.Join(lines[closeIndex+1:], "\n"), true
}

func parseFrontmatter(wf *Definition, lines []string) {
	for i := 0; i < len(lines); i++ {
		key, value, ok := splitYAMLLine(lines[i])
		if !ok {
			continue
		}
		switch applyDefinitionMetadata(wf, key, value) {
		case "profiles-block":
			profiles, next := parseProfileBlock(lines, i+1)
			wf.Profiles = profiles
			i = next - 1
		}
	}
}

func applyDefinitionMetadata(wf *Definition, key, value string) string {
	switch key {
	case "name":
		wf.Name = value
	case "description":
		wf.Description = value
	case "when-to-use", "when_to_use":
		wf.WhenToUse = value
	case "argument-hint", "argument_hint":
		wf.ArgumentHint = value
	case "user-invocable", "user_invocable":
		wf.UserInvocable = parseBool(value, true)
	case "disable-model-invocation", "disable_model_invocation":
		wf.DisableModelInvoke = parseBool(value, false)
	case "version":
		wf.Version = value
	case "max-agents", "max_agents":
		wf.MaxAgents = parseInt(value)
	case "max-concurrency", "max_concurrency":
		wf.MaxConcurrency = parseInt(value)
	case "profiles":
		if value != "" {
			wf.Profiles = profileRefsFromNames(parseList(value))
			return ""
		}
		return "profiles-block"
	case "allow-profile-creation", "allow_profile_creation":
		wf.AllowProfileCreation = value
	case "memory-policy", "memory_policy":
		wf.MemoryPolicy = value
	}
	return ""
}

func parseProfileBlock(lines []string, start int) ([]ProfileRef, int) {
	var out []ProfileRef
	var current *ProfileRef
	i := start
	for i < len(lines) {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			i++
			continue
		}
		if !isIndented(raw) && !strings.HasPrefix(trimmed, "- ") {
			break
		}
		if strings.HasPrefix(trimmed, "- ") {
			if current != nil && current.Name != "" {
				out = append(out, *current)
			}
			current = &ProfileRef{}
			item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if item != "" {
				applyProfileItem(current, item)
			}
			i++
			continue
		}
		if current != nil {
			applyProfileItem(current, trimmed)
		}
		i++
	}
	if current != nil && current.Name != "" {
		out = append(out, *current)
	}
	return out, i
}

func applyProfileItem(profile *ProfileRef, item string) {
	key, value, ok := splitAnyYAMLLine(item)
	if !ok {
		profile.Name = trimQuotes(item)
		return
	}
	switch key {
	case "name":
		profile.Name = value
	case "required":
		profile.Required = parseBool(value, false)
	}
}

func profileRefsFromNames(names []string) []ProfileRef {
	out := make([]ProfileRef, 0, len(names))
	for _, name := range names {
		name = trimQuotes(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		out = append(out, ProfileRef{Name: name})
	}
	return out
}

func isIndented(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
}

func canonicalName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "/")
	return name
}

func splitYAMLLine(line string) (key, value string, ok bool) {
	if isIndented(line) {
		return "", "", false
	}
	return splitAnyYAMLLine(line)
}

func splitAnyYAMLLine(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = trimQuotes(strings.TrimSpace(line[idx+1:]))
	return key, value, key != ""
}

func trimQuotes(value string) string {
	return strings.Trim(value, `"'`)
}

func parseList(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = trimQuotes(strings.TrimSpace(part))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseBool(value string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "1", "on":
		return true
	case "false", "no", "0", "off":
		return false
	}
	return def
}

func parseInt(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func firstMarkdownLine(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		line = strings.TrimLeft(line, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 200 {
			line = line[:200] + "..."
		}
		return line
	}
	return ""
}
