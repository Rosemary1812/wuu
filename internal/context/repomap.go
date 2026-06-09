package context

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultRepoMapMaxFiles       = 4000
	defaultRepoMapMaxListedFiles = 120
)

var repoMapSkipDirs = map[string]bool{
	".git":         true,
	".wuu-home":    true,
	".wuu":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"coverage":     true,
	".next":        true,
	".turbo":       true,
	".cache":       true,
	"tmp":          true,
	"target":       true,
	"__pycache__":  true,
}

var repoMapPriorityFiles = map[string]bool{
	"AGENTS.md":          true,
	"README.md":          true,
	"go.mod":             true,
	"go.work":            true,
	"package.json":       true,
	"pnpm-lock.yaml":     true,
	"package-lock.json":  true,
	"yarn.lock":          true,
	"Cargo.toml":         true,
	"pyproject.toml":     true,
	"requirements.txt":   true,
	"Makefile":           true,
	"Taskfile.yml":       true,
	"justfile":           true,
	"docker-compose.yml": true,
	"Dockerfile":         true,
}

type RepoMapOptions struct {
	MaxFiles       int
	MaxListedFiles int
}

type repoMapFile struct {
	Path     string
	Ext      string
	Priority int
}

// RepoMapBlock returns a compact repository map for per-turn typed context.
func RepoMapBlock(root string, opts RepoMapOptions) (Block, bool) {
	root = strings.TrimSpace(root)
	if root == "" {
		return Block{}, false
	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = defaultRepoMapMaxFiles
	}
	if opts.MaxListedFiles <= 0 {
		opts.MaxListedFiles = defaultRepoMapMaxListedFiles
	}

	files, truncated, err := collectRepoMapFiles(root, opts.MaxFiles)
	if err != nil || len(files) == 0 {
		return Block{}, false
	}
	sortRepoMapFiles(files)
	languageCounts := repoMapLanguageCounts(files)
	testFiles := repoMapTestFiles(files, 20)
	testMappings := repoMapTestMappings(files, 20)

	listed := files
	if len(listed) > opts.MaxListedFiles {
		listed = listed[:opts.MaxListedFiles]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "files_scanned: %d\n", len(files))
	if truncated {
		b.WriteString("truncated: true\n")
	}
	if len(languageCounts) > 0 {
		b.WriteString("languages:\n")
		for _, item := range languageCounts {
			fmt.Fprintf(&b, "- %s: %d\n", item.Name, item.Count)
		}
	}
	if len(testFiles) > 0 {
		b.WriteString("test_files:\n")
		for _, path := range testFiles {
			fmt.Fprintf(&b, "- %s\n", path)
		}
	}
	if len(testMappings) > 0 {
		b.WriteString("test_mappings:\n")
		for _, mapping := range testMappings {
			fmt.Fprintf(&b, "- %s -> %s\n", mapping.Source, mapping.Test)
		}
	}
	b.WriteString("representative_files:\n")
	for _, file := range listed {
		fmt.Fprintf(&b, "- %s\n", file.Path)
	}
	if omitted := len(files) - len(listed); omitted > 0 {
		fmt.Fprintf(&b, "omitted_files: %d\n", omitted)
	}

	return Block{
		Kind:        BlockRepoMap,
		Title:       "Compact repository map",
		Source:      "runtime.repo_map",
		TokenBudget: 1200,
		Content:     strings.TrimRight(b.String(), "\n"),
	}, true
}

func collectRepoMapFiles(root string, maxFiles int) ([]repoMapFile, bool, error) {
	files := make([]repoMapFile, 0, min(maxFiles, 256))
	truncated := false
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != root && repoMapSkipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= maxFiles {
			truncated = true
			return filepath.SkipAll
		}
		if strings.HasPrefix(name, ".") && !repoMapPriorityFiles[name] {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > 2*1024*1024 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if isRepoMapGeneratedPath(rel) {
			return nil
		}
		files = append(files, repoMapFile{
			Path:     rel,
			Ext:      strings.ToLower(filepath.Ext(rel)),
			Priority: repoMapPriority(rel),
		})
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return files, truncated, nil
}

func sortRepoMapFiles(files []repoMapFile) {
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Priority != files[j].Priority {
			return files[i].Priority < files[j].Priority
		}
		return files[i].Path < files[j].Path
	})
}

func repoMapPriority(path string) int {
	base := filepath.Base(path)
	if repoMapPriorityFiles[base] || strings.HasSuffix(path, "/AGENTS.md") {
		return 0
	}
	if isRepoMapTestPath(path) {
		return 2
	}
	if strings.Count(path, "/") <= 1 {
		return 1
	}
	return 3
}

type repoMapLanguageCount struct {
	Name  string
	Count int
}

type repoMapTestMapping struct {
	Source string
	Test   string
}

func repoMapLanguageCounts(files []repoMapFile) []repoMapLanguageCount {
	counts := map[string]int{}
	for _, file := range files {
		if name := repoMapLanguage(file.Ext, filepath.Base(file.Path)); name != "" {
			counts[name]++
		}
	}
	out := make([]repoMapLanguageCount, 0, len(counts))
	for name, count := range counts {
		out = append(out, repoMapLanguageCount{Name: name, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func repoMapLanguage(ext, base string) string {
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java", ".kt", ".kts":
		return "jvm"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".sh", ".bash", ".zsh":
		return "shell"
	}
	switch base {
	case "Makefile", "Dockerfile", "justfile":
		return strings.ToLower(base)
	default:
		return ""
	}
}

func repoMapTestFiles(files []repoMapFile, limit int) []string {
	var out []string
	for _, file := range files {
		if isRepoMapTestPath(file.Path) {
			out = append(out, file.Path)
			if len(out) >= limit {
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

func repoMapTestMappings(files []repoMapFile, limit int) []repoMapTestMapping {
	if limit <= 0 {
		return nil
	}
	fileSet := make(map[string]struct{}, len(files))
	for _, file := range files {
		fileSet[file.Path] = struct{}{}
	}
	var out []repoMapTestMapping
	for _, file := range files {
		if !isRepoMapTestPath(file.Path) {
			continue
		}
		if source := repoMapSourceForTest(file.Path, fileSet); source != "" {
			out = append(out, repoMapTestMapping{Source: source, Test: file.Path})
			if len(out) >= limit {
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Test < out[j].Test
	})
	return out
}

func repoMapSourceForTest(testPath string, fileSet map[string]struct{}) string {
	dir := filepath.Dir(testPath)
	if dir == "." {
		dir = ""
	}
	base := filepath.Base(testPath)
	lowerBase := strings.ToLower(base)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	candidates := []string(nil)
	switch {
	case strings.HasSuffix(lowerBase, "_test.go"):
		candidates = append(candidates, strings.TrimSuffix(base, "_test.go")+".go")
	case strings.HasSuffix(lowerBase, ".test.tsx"):
		candidates = append(candidates, strings.TrimSuffix(base, ".test.tsx")+".tsx")
	case strings.HasSuffix(lowerBase, ".test.ts"):
		candidates = append(candidates, strings.TrimSuffix(base, ".test.ts")+".ts")
	case strings.HasSuffix(lowerBase, ".spec.tsx"):
		candidates = append(candidates, strings.TrimSuffix(base, ".spec.tsx")+".tsx")
	case strings.HasSuffix(lowerBase, ".spec.ts"):
		candidates = append(candidates, strings.TrimSuffix(base, ".spec.ts")+".ts")
	case strings.HasPrefix(lowerBase, "test_") && ext == ".py":
		candidates = append(candidates, strings.TrimPrefix(base, "test_"))
	case strings.HasSuffix(lowerBase, "_test.py"):
		candidates = append(candidates, strings.TrimSuffix(stem, "_test")+".py")
	}
	for _, candidate := range candidates {
		source := candidate
		if dir != "" {
			source = filepath.ToSlash(filepath.Join(dir, candidate))
		}
		if _, ok := fileSet[source]; ok {
			return source
		}
	}
	return ""
}

func isRepoMapTestPath(path string) bool {
	base := filepath.Base(path)
	lower := strings.ToLower(path)
	return strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "/tests/") ||
		strings.HasSuffix(base, "_test.go") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.tsx") ||
		strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, ".spec.tsx") ||
		strings.HasPrefix(base, "test_")
}

func isRepoMapGeneratedPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".min.js") ||
		strings.HasSuffix(lower, ".map") ||
		strings.Contains(lower, "/generated/") ||
		strings.Contains(lower, "/.generated/")
}
