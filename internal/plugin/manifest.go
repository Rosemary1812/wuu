package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blueberrycongee/wuu/internal/config"
)

const (
	ManifestFilename       = "plugin.json"
	CodexManifestFilename  = ".codex-plugin/plugin.json"
	ClaudeManifestFilename = ".claude-plugin/plugin.json"
)

type Manifest struct {
	ID                   string                            `json:"id"`
	Name                 string                            `json:"name,omitempty"`
	Description          string                            `json:"description,omitempty"`
	Version              string                            `json:"version,omitempty"`
	Author               json.RawMessage                   `json:"author,omitempty"`
	Homepage             string                            `json:"homepage,omitempty"`
	Repository           string                            `json:"repository,omitempty"`
	License              string                            `json:"license,omitempty"`
	Keywords             []string                          `json:"keywords,omitempty"`
	Skills               []string                          `json:"skills,omitempty"`
	Hooks                map[string][]config.HookEntry     `json:"hooks,omitempty"`
	MCPServers           map[string]config.MCPServerConfig `json:"mcp_servers,omitempty"`
	Interface            json.RawMessage                   `json:"interface,omitempty"`
	Platforms            []string                          `json:"platforms,omitempty"`
	RequestedPermissions []string                          `json:"requested_permissions,omitempty"`
	ActivityKinds        []string                          `json:"activity_kinds,omitempty"`
	OfficialNativeHelper json.RawMessage                   `json:"official_native_helper,omitempty"`
	MinimumWuuVersion    string                            `json:"minimum_wuu_version,omitempty"`
	UnsupportedFields    []string                          `json:"unsupported_fields,omitempty"`
}

type LoadOptions struct {
	Source   string
	Official bool
}

type rawManifest struct {
	ID                     string          `json:"id"`
	Name                   string          `json:"name"`
	Description            string          `json:"description"`
	Version                string          `json:"version"`
	Author                 json.RawMessage `json:"author"`
	Homepage               string          `json:"homepage"`
	Repository             string          `json:"repository"`
	License                string          `json:"license"`
	Keywords               []string        `json:"keywords"`
	Skills                 json.RawMessage `json:"skills"`
	Hooks                  json.RawMessage `json:"hooks"`
	MCPServers             json.RawMessage `json:"mcpServers"`
	MCPServersAlias        json.RawMessage `json:"mcp_servers"`
	Interface              json.RawMessage `json:"interface"`
	Platforms              []string        `json:"platforms"`
	RequestedPermissions   []string        `json:"requestedPermissions"`
	RequestedPermsAlias    []string        `json:"requested_permissions"`
	ActivityKinds          []string        `json:"activityKinds"`
	ActivityKindsAlias     []string        `json:"activity_kinds"`
	OfficialNativeHelper   json.RawMessage `json:"officialNativeHelper"`
	OfficialHelperAlias    json.RawMessage `json:"official_native_helper"`
	MinimumWuuVersion      string          `json:"minimumWuuVersion"`
	MinimumWuuVersionAlias string          `json:"minimum_wuu_version"`
}

var supportedManifestFields = map[string]struct{}{
	"id": {}, "name": {}, "description": {}, "version": {}, "author": {},
	"homepage": {}, "repository": {}, "license": {}, "keywords": {},
	"skills": {}, "hooks": {}, "mcpServers": {}, "mcp_servers": {},
	"interface": {}, "platforms": {},
	"requestedPermissions": {}, "requested_permissions": {},
	"activityKinds": {}, "activity_kinds": {},
	"officialNativeHelper": {}, "official_native_helper": {},
	"minimumWuuVersion": {}, "minimum_wuu_version": {},
}

func LoadManifest(path, source string) (Plugin, error) {
	return LoadManifestWithOptions(path, LoadOptions{Source: source})
}

func LoadManifestWithOptions(path string, options LoadOptions) (Plugin, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Plugin{}, fmt.Errorf("read plugin manifest: %w", err)
	}
	root := manifestRoot(path)
	manifest, err := normalizeManifest(data, root, options.Official)
	if err != nil {
		return Plugin{}, fmt.Errorf("parse plugin manifest %s: %w", path, err)
	}
	return Plugin{
		Manifest:     manifest,
		Source:       strings.TrimSpace(options.Source),
		Root:         root,
		ManifestPath: path,
		Official:     options.Official,
	}, nil
}

func normalizeManifest(data []byte, root string, official bool) (Manifest, error) {
	var raw rawManifest
	if err := json.Unmarshal(data, &raw); err != nil {
		return Manifest{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Manifest{}, err
	}

	id := strings.TrimSpace(raw.ID)
	if id == "" {
		id = strings.TrimSpace(raw.Name)
	}
	if id == "" {
		return Manifest{}, fmt.Errorf("requires id or name")
	}

	skills, err := normalizePathList(root, "skills", raw.Skills)
	if err != nil {
		return Manifest{}, err
	}
	hooks, err := normalizeHooks(root, raw.Hooks)
	if err != nil {
		return Manifest{}, err
	}
	mcpServers, err := normalizeMCPServers(root, firstRaw(raw.MCPServers, raw.MCPServersAlias))
	if err != nil {
		return Manifest{}, err
	}
	helper := firstRaw(raw.OfficialNativeHelper, raw.OfficialHelperAlias)
	if hasDeclaredValue(helper) && !official {
		return Manifest{}, fmt.Errorf("official_native_helper is reserved for official bundled plugins")
	}

	unsupported := make([]string, 0)
	for field := range fields {
		if _, ok := supportedManifestFields[field]; !ok {
			unsupported = append(unsupported, field)
		}
	}
	sort.Strings(unsupported)

	return Manifest{
		ID:                   id,
		Name:                 strings.TrimSpace(raw.Name),
		Description:          strings.TrimSpace(raw.Description),
		Version:              strings.TrimSpace(raw.Version),
		Author:               cloneRaw(raw.Author),
		Homepage:             strings.TrimSpace(raw.Homepage),
		Repository:           strings.TrimSpace(raw.Repository),
		License:              strings.TrimSpace(raw.License),
		Keywords:             normalizeStrings(raw.Keywords),
		Skills:               skills,
		Hooks:                hooks,
		MCPServers:           mcpServers,
		Interface:            cloneRaw(raw.Interface),
		Platforms:            normalizeStrings(raw.Platforms),
		RequestedPermissions: normalizeStrings(append(raw.RequestedPermsAlias, raw.RequestedPermissions...)),
		ActivityKinds:        normalizeStrings(append(raw.ActivityKindsAlias, raw.ActivityKinds...)),
		OfficialNativeHelper: cloneRaw(helper),
		MinimumWuuVersion:    firstString(raw.MinimumWuuVersion, raw.MinimumWuuVersionAlias),
		UnsupportedFields:    unsupported,
	}, nil
}

func normalizePathList(root, field string, raw json.RawMessage) ([]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		path, err := normalizePluginPath(root, field, one)
		if err != nil {
			return nil, err
		}
		return []string{path}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, fmt.Errorf("%s must be a string or string array", field)
	}
	out := make([]string, 0, len(many))
	for _, value := range many {
		path, err := normalizePluginPath(root, field, value)
		if err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return normalizeStrings(out), nil
}

func normalizeHooks(root string, raw json.RawMessage) (map[string][]config.HookEntry, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var out map[string][]config.HookEntry
		if err := json.Unmarshal(trimmed, &out); err != nil {
			return nil, fmt.Errorf("hooks: %w", err)
		}
		return out, nil
	}
	paths, err := normalizePathList(root, "hooks", raw)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]config.HookEntry)
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return nil, fmt.Errorf("read hooks %s: %w", path, err)
		}
		var wrapper struct {
			Hooks map[string][]config.HookEntry `json:"hooks"`
		}
		if err := json.Unmarshal(data, &wrapper); err != nil {
			return nil, fmt.Errorf("parse hooks %s: %w", path, err)
		}
		for event, entries := range wrapper.Hooks {
			out[event] = append(out[event], entries...)
		}
	}
	return out, nil
}

func normalizeMCPServers(root string, raw json.RawMessage) (map[string]config.MCPServerConfig, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var out map[string]config.MCPServerConfig
		if err := json.Unmarshal(trimmed, &out); err != nil {
			return nil, fmt.Errorf("mcpServers: %w", err)
		}
		return out, nil
	}
	var ref string
	if err := json.Unmarshal(trimmed, &ref); err != nil {
		return nil, fmt.Errorf("mcpServers must be an object or path")
	}
	path, err := normalizePluginPath(root, "mcpServers", ref)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return nil, fmt.Errorf("read mcpServers %s: %w", path, err)
	}
	var wrapper struct {
		MCPServers map[string]config.MCPServerConfig `json:"mcpServers"`
		Alias      map[string]config.MCPServerConfig `json:"mcp_servers"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parse mcpServers %s: %w", path, err)
	}
	if len(wrapper.MCPServers) > 0 {
		return wrapper.MCPServers, nil
	}
	return wrapper.Alias, nil
}

func normalizePluginPath(root, field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) {
		return "", fmt.Errorf("%s path %q must remain within plugin root %s", field, value, root)
	}
	cleaned := filepath.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s path %q must remain within plugin root %s", field, value, root)
	}
	return cleaned, nil
}

func manifestRoot(path string) string {
	dir := filepath.Dir(path)
	base := filepath.Base(dir)
	if base == ".codex-plugin" || base == ".claude-plugin" {
		return filepath.Dir(dir)
	}
	return dir
}

func firstRaw(values ...json.RawMessage) json.RawMessage {
	for _, value := range values {
		if len(bytes.TrimSpace(value)) > 0 {
			return value
		}
	}
	return nil
}

func firstString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func hasDeclaredValue(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte("false"))
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}
