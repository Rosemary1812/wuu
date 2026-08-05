package plugin

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Plugin struct {
	Manifest
	Source               string
	Root                 string
	ManifestPath         string
	Official             bool
	SubjectID            string
	Fingerprint          string
	EffectivePermissions []string
}

func Discover(projectRoot, wuuHome string) []Plugin {
	return DiscoverWithOptions(projectRoot, wuuHome, defaultDiscoverOptions())
}

func DiscoverWithOptions(projectRoot, wuuHome string, options DiscoverOptions) []Plugin {
	bundledPlugins := discoverBundled(wuuHome, options)
	userPlugins := scanPluginRoots(userPluginRoots(wuuHome), "user")
	projectPlugins := scanPluginRoots(projectPluginRoots(projectRoot), "project")

	byID := make(map[string]Plugin, len(bundledPlugins)+len(userPlugins)+len(projectPlugins))
	for _, item := range userPlugins {
		byID[item.ID] = item
	}
	for _, item := range projectPlugins {
		byID[item.ID] = item
	}
	// Official bundled plugins are trusted by provenance, not by id. They win
	// collisions so a project cannot shadow a privileged helper declaration
	// with an untrusted plugin using the same name.
	for _, item := range bundledPlugins {
		byID[item.ID] = item
	}

	out := make([]Plugin, 0, len(byID))
	for _, item := range byID {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (p Plugin) SkillDirs() []string {
	return p.resolveDirs(p.Skills, "skills")
}

func (p Plugin) SourceLabel() string {
	id := strings.TrimSpace(p.ID)
	if id == "" {
		return "plugin"
	}
	return "plugin:" + id
}

func (p Plugin) resolveDirs(entries []string, fallback string) []string {
	var dirs []string
	if len(entries) == 0 {
		path := filepath.Join(p.Root, fallback)
		if isDir(path) {
			return []string{path}
		}
		return nil
	}
	for _, entry := range entries {
		rel, ok := cleanRelativePath(entry)
		if !ok {
			continue
		}
		path := filepath.Join(p.Root, rel)
		if isDir(path) {
			dirs = append(dirs, path)
		}
	}
	return dirs
}

func scanPluginRoots(roots []string, source string) []Plugin {
	var out []Plugin
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" || !isDir(root) {
			continue
		}
		if plugin, err := loadPluginDir(root, source); err == nil {
			out = append(out, plugin)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if plugin, err := loadPluginDir(filepath.Join(root, entry.Name()), source); err == nil {
				out = append(out, plugin)
			}
		}
	}
	return out
}

func loadPluginDir(dir, source string) (Plugin, error) {
	for _, rel := range []string{ManifestFilename, CodexManifestFilename, ClaudeManifestFilename} {
		path := filepath.Join(dir, rel)
		if plugin, err := LoadManifest(path, source); err == nil {
			return plugin, nil
		}
	}
	return Plugin{}, os.ErrNotExist
}

func userPluginRoots(wuuHome string) []string {
	if strings.TrimSpace(wuuHome) == "" {
		return nil
	}
	return []string{
		filepath.Join(wuuHome, "plugins"),
		filepath.Join(wuuHome, "plugin"),
	}
}

func projectPluginRoots(projectRoot string) []string {
	if strings.TrimSpace(projectRoot) == "" {
		return nil
	}
	return []string{
		filepath.Join(projectRoot, ".wuu", "plugins"),
		filepath.Join(projectRoot, ".wuu", "plugin"),
	}
}

func cleanRelativePath(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) {
		return "", false
	}
	cleaned := filepath.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	return cleaned, true
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
