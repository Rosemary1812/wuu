package plugin

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

//go:embed all:bundled
var bundledFS embed.FS

const bundledFingerprintFile = ".fingerprint"

// EnableCUAMacEnv gates the bundled Computer Use plugin. It remains off in
// normal releases while the native integration is under internal evaluation.
const EnableCUAMacEnv = "WUU_ENABLE_CUA_MAC"

type DiscoverOptions struct {
	GOOS      string
	LookupEnv func(string) (string, bool)
}

func defaultDiscoverOptions() DiscoverOptions {
	return DiscoverOptions{GOOS: runtime.GOOS, LookupEnv: os.LookupEnv}
}

func discoverBundled(wuuHome string, options DiscoverOptions) []Plugin {
	if strings.TrimSpace(options.GOOS) == "" {
		options.GOOS = runtime.GOOS
	}
	if options.LookupEnv == nil {
		options.LookupEnv = os.LookupEnv
	}
	root, err := materializeBundled(wuuHome)
	if err != nil || root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []Plugin
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		item, err := loadOfficialPluginDir(filepath.Join(root, entry.Name()))
		if err != nil || !supportsPlatform(item.Platforms, options.GOOS) || !bundledPluginEnabled(item, options.LookupEnv) {
			continue
		}
		resolveOfficialHelper(&item, options.LookupEnv)
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func bundledPluginEnabled(item Plugin, lookupEnv func(string) (string, bool)) bool {
	if item.ID != "cua-mac" {
		return true
	}
	if lookupEnv == nil {
		return false
	}
	value, ok := lookupEnv(EnableCUAMacEnv)
	return ok && strings.TrimSpace(value) == "1"
}

func loadOfficialPluginDir(dir string) (Plugin, error) {
	for _, rel := range []string{ManifestFilename, CodexManifestFilename, ClaudeManifestFilename} {
		path := filepath.Join(dir, rel)
		if item, err := LoadManifestWithOptions(path, LoadOptions{Source: "bundled", Official: true}); err == nil {
			return item, nil
		}
	}
	return Plugin{}, os.ErrNotExist
}

func supportsPlatform(platforms []string, goos string) bool {
	if len(platforms) == 0 {
		return true
	}
	for _, platform := range platforms {
		if strings.EqualFold(strings.TrimSpace(platform), strings.TrimSpace(goos)) {
			return true
		}
	}
	return false
}

type officialHelperSpec struct {
	ID          string `json:"id"`
	Environment string `json:"environment"`
	Executable  string `json:"executable"`
}

func resolveOfficialHelper(item *Plugin, lookupEnv func(string) (string, bool)) {
	if item == nil || !item.Official || len(item.OfficialNativeHelper) == 0 {
		return
	}
	var spec officialHelperSpec
	if json.Unmarshal(item.OfficialNativeHelper, &spec) != nil || strings.TrimSpace(spec.ID) == "" {
		return
	}
	command := resolveHelperCommand(spec, lookupEnv)
	placeholder := "@wuu/official-helper:" + strings.TrimSpace(spec.ID)
	for name, server := range item.MCPServers {
		if strings.TrimSpace(server.Command) == placeholder {
			server.Command = command
			item.MCPServers[name] = server
		}
	}
}

func resolveHelperCommand(spec officialHelperSpec, lookupEnv func(string) (string, bool)) string {
	if key := strings.TrimSpace(spec.Environment); key != "" && lookupEnv != nil {
		if value, ok := lookupEnv(key); ok && isExecutableFile(value) {
			return value
		}
	}
	name := strings.TrimSpace(spec.Executable)
	if name == "" {
		return "@wuu/official-helper:" + strings.TrimSpace(spec.ID)
	}
	if current, err := os.Executable(); err == nil {
		if sibling := filepath.Join(filepath.Dir(current), name); isExecutableFile(sibling) {
			return sibling
		}
	}
	if found, err := exec.LookPath(name); err == nil {
		return found
	}
	return name
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func materializeBundled(wuuHome string) (string, error) {
	if strings.TrimSpace(wuuHome) == "" {
		return "", nil
	}
	dest := filepath.Join(wuuHome, "cache", "plugins", ".bundled")
	want := bundledFingerprint()
	if got, err := os.ReadFile(filepath.Join(dest, bundledFingerprintFile)); err == nil && string(got) == want {
		return dest, nil
	}
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	err := fs.WalkDir(bundledFS, "bundled", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(path, "bundled"), "/")
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := bundledFS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if mkdirErr := os.MkdirAll(filepath.Dir(target), 0o755); mkdirErr != nil {
			return mkdirErr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dest, bundledFingerprintFile), []byte(want), 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

func bundledFingerprint() string {
	var paths []string
	_ = fs.WalkDir(bundledFS, "bundled", func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		data, err := bundledFS.ReadFile(path)
		if err != nil {
			continue
		}
		hash.Write([]byte(path))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
