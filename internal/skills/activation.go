package skills

import (
	"path/filepath"
	"regexp"
	"strings"
)

// FilterActiveSkills returns skills whose Paths globs match at least
// one entry in touchedPaths, plus any skills without Paths constraints
// (unconditional). This enables lazy activation: skills with narrow
// paths only appear in the prompt when the user is working with
// matching files.
func FilterActiveSkills(all []Skill, touchedPaths []string) []Skill {
	if len(all) == 0 {
		return nil
	}
	out := make([]Skill, 0, len(all))
	for _, s := range all {
		if len(s.Paths) == 0 {
			out = append(out, s)
			continue
		}
		if matchesAnyPath(s.Paths, touchedPaths) {
			out = append(out, s)
		}
	}
	return out
}

// matchesAnyPath reports whether any of the globs match any of the paths.
func matchesAnyPath(globs, paths []string) bool {
	for _, glob := range globs {
		re, err := compileGlob(glob)
		if err != nil {
			continue
		}
		for _, p := range paths {
			if re.MatchString(filepath.ToSlash(p)) {
				return true
			}
		}
	}
	return false
}

// compileGlob converts a glob pattern to a regex. Supports:
//   - ** → matches any path segment(s)
//   - * → matches anything except /
//   - ? → matches any single non-/ char
func compileGlob(pattern string) (*regexp.Regexp, error) {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	var b strings.Builder
	b.WriteString("(?:^|/)")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					b.WriteString("(?:.*/)?")
					i += 2
				} else {
					b.WriteString(".*")
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(pattern[i])
		default:
			b.WriteByte(pattern[i])
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
