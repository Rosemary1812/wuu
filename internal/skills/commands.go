package skills

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Claude Code "commands" are markdown prompt templates that live in
// .claude/commands/*.md. Wuu reads them as pure content and adapts each into a
// lightweight Skill entry so they flow through the existing skill discovery,
// catalog, and load_skill pipeline instead of a bespoke system.
//
// Only descriptive fields are honored: the markdown body, the `description`,
// and the `argument-hint`. Execution-binding frontmatter that Claude Code uses
// (`allowed-tools`, `model`, ...) is silently ignored so a command can never
// rebind wuu's tool surface or switch models. Claude Code's inline
// !`command` execution syntax is intentionally not implemented; because
// Env.ProcessSkillBody loads skill bodies with inline shell disabled, such
// syntax is preserved verbatim as text.
//
// A command's name is always derived from its file name — Claude Code addresses
// commands by file name, not a frontmatter field — so any `name:` key present
// in frontmatter is ignored along with the other execution-binding fields.

// DiscoverCommandsSourceDirs scans command directories from lowest to highest
// precedence: user dirs first, then project dirs. Later dirs override earlier
// dirs with the same command name. The returned entries are content-only Skill
// values suitable for merging into the discovered skill list via MergeCommands.
func DiscoverCommandsSourceDirs(projectDirs, userDirs []SourceDir) []Skill {
	userCommands := scanCommandSourceDirs(userDirs)
	projectCommands := scanCommandSourceDirs(projectDirs)

	// Project overrides user (project is more specific).
	byName := make(map[string]Skill, len(projectCommands)+len(userCommands))
	for _, c := range userCommands {
		byName[c.Name] = c
	}
	for _, c := range projectCommands {
		byName[c.Name] = c
	}

	result := make([]Skill, 0, len(byName))
	for _, c := range byName {
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// MergeCommands appends content-only command skills into the discovered skill
// list. Skills already present (by name) take precedence over commands, so a
// native skill is never shadowed by a command of the same name.
func MergeCommands(discovered, commands []Skill) []Skill {
	if len(commands) == 0 {
		return discovered
	}
	seen := make(map[string]bool, len(discovered))
	for _, s := range discovered {
		seen[s.Name] = true
	}
	merged := make([]Skill, len(discovered), len(discovered)+len(commands))
	copy(merged, discovered)
	for _, c := range commands {
		if seen[c.Name] {
			continue
		}
		merged = append(merged, c)
		seen[c.Name] = true
	}
	return merged
}

func scanCommandSourceDirs(dirs []SourceDir) []Skill {
	var out []Skill
	for _, dir := range dirs {
		source := strings.TrimSpace(dir.Source)
		if source == "" {
			source = "unknown"
		}
		out = append(out, scanCommandDir(dir.Path, source)...)
	}
	return out
}

// scanCommandDir reads flat .md command files directly inside dir. Claude Code
// addresses commands as `.claude/commands/*.md`, so only top-level markdown
// files are scanned; nested directories are skipped.
func scanCommandDir(dir, source string) []Skill {
	if strings.TrimSpace(dir) == "" {
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
	var out []Skill
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		cmd, parseErr := parseCommandFile(filepath.Join(dir, entry.Name()), source)
		if parseErr != nil {
			continue
		}
		out = append(out, cmd)
	}
	return out
}

// parseCommandFile reads a Claude Code command markdown file and adapts it into
// a content-only Skill. The command name is derived from the file name. Only
// `description` and `argument-hint` are read from frontmatter; every other key
// (including execution-binding `allowed-tools` and `model`) is ignored. A file
// without frontmatter is treated as a bare markdown body.
func parseCommandFile(path, source string) (Skill, error) {
	f, err := os.Open(path)
	if err != nil {
		return Skill{}, err
	}
	defer f.Close()

	skill := Skill{
		Name:          commandName(path),
		Source:        source,
		Path:          path,
		Dir:           filepath.Dir(path),
		UserInvocable: true,
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)

	// Peek the first line to decide whether frontmatter is present.
	if !scanner.Scan() {
		return skill, nil // empty file: valid but empty command
	}
	first := scanner.Text()

	var bodyLines []string
	if strings.TrimSpace(first) == "---" {
		// Consume frontmatter, reading only whitelisted descriptive keys. All
		// other keys (allowed-tools, model, name, ...) are intentionally dropped.
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "---" {
				break
			}
			k, v, ok := splitYAMLLine(line)
			if !ok {
				continue
			}
			switch k {
			case "description":
				skill.Description = v
			case "argument-hint", "argument_hint":
				skill.ArgumentHint = v
			}
		}
	} else {
		bodyLines = append(bodyLines, first)
	}

	for scanner.Scan() {
		bodyLines = append(bodyLines, scanner.Text())
	}
	skill.Content = normalizeCommandArguments(strings.Join(bodyLines, "\n"))
	return skill, nil
}

// commandName derives the canonical command name from the file name.
func commandName(path string) string {
	base := filepath.Base(path)
	return canonicalName(strings.TrimSuffix(base, filepath.Ext(base)))
}

// normalizeCommandArguments rewrites Claude Code's bare $ARGUMENTS placeholder
// to wuu's ${ARGUMENTS} form so the existing skill argument-substitution
// pipeline fills it when a command is loaded with arguments. The already-braced
// ${ARGUMENTS} form never contains the bare token, so it is left untouched.
func normalizeCommandArguments(body string) string {
	if !strings.Contains(body, "$ARGUMENTS") {
		return body
	}
	return strings.ReplaceAll(body, "$ARGUMENTS", "${ARGUMENTS}")
}
