package skills

import (
	"embed"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed bundled/*.md bundled/*/SKILL.md
var bundledFS embed.FS

// BundledSkills returns skills compiled into the binary. These are
// parsed at call time from the embedded filesystem. Discovered skills
// with the same name take precedence (project customization wins).
func BundledSkills() []Skill {
	var out []Skill
	_ = fs.WalkDir(bundledFS, "bundled", func(filePath string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		base := path.Base(filePath)
		if !strings.EqualFold(base, "SKILL.md") && !strings.HasSuffix(strings.ToLower(base), ".md") {
			return nil
		}
		data, err := bundledFS.ReadFile(filePath)
		if err != nil {
			return nil
		}
		dir := path.Dir(filePath)
		s := parseSkillContent(string(data), base, "bundled", dir)
		if s.Name == "" {
			return nil
		}
		if strings.EqualFold(base, "SKILL.md") {
			s.Name = canonicalName(firstNonEmpty(s.Name, path.Base(dir)))
		}
		s.Path = filePath
		s.Dir = dir
		out = append(out, s)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

// MergeWithBundled merges discovered skills with bundled ones.
// Discovered skills override bundled skills with the same name.
func MergeWithBundled(discovered []Skill) []Skill {
	bundled := BundledSkills()
	if len(bundled) == 0 {
		return discovered
	}

	// Index discovered names for dedup.
	seen := make(map[string]bool, len(discovered))
	for _, s := range discovered {
		seen[s.Name] = true
	}

	// Append bundled skills not overridden by discovered ones.
	merged := make([]Skill, len(discovered), len(discovered)+len(bundled))
	copy(merged, discovered)
	for _, s := range bundled {
		if !seen[s.Name] {
			merged = append(merged, s)
		}
	}
	return merged
}
