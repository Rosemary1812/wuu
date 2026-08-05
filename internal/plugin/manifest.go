package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/extensions"
	"github.com/blueberrycongee/wuu/internal/pluginhost"
)

const (
	ManifestFilename       = "plugin.json"
	CodexManifestFilename  = ".codex-plugin/plugin.json"
	ClaudeManifestFilename = ".claude-plugin/plugin.json"
)

// maxPromptFileSize limits prompt_template files to 1 MiB. Larger files are
// rejected during discovery so a package cannot load unbounded host memory.
const maxPromptFileSize = 1 << 20

// CommandKind enumerates the supported command contribution kinds.
type CommandKind string

const (
	// CommandKindPromptTemplate loads a bounded UTF-8 file inside the plugin
	// root and produces a host-owned composer action.
	CommandKindPromptTemplate CommandKind = "prompt_template"
	// CommandKindRuntimeAction is reserved in the schema but not executable
	// until its request/response contract, permissions, and audit behavior are
	// defined.
	CommandKindRuntimeAction CommandKind = "runtime_action"
)

// CommandSpec is the declarative command descriptor contributed by a manifest.
type CommandSpec struct {
	ID          string      `json:"id"`
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Kind        CommandKind `json:"kind"`
	Prompt      string      `json:"prompt,omitempty"`
	Contexts    []string    `json:"contexts,omitempty"`
	Aliases     []string    `json:"aliases,omitempty"`
	Keywords    []string    `json:"keywords,omitempty"`
}

// ResolvedPrompt holds the loaded, validated prompt content and metadata.
type ResolvedPrompt struct {
	Path    string `json:"path"`
	RelPath string `json:"rel_path"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Text    string `json:"text"`
}

// ResolvedCommand is a manifest command with its public id and resolved prompt.
type ResolvedCommand struct {
	CommandSpec
	PublicID       string          `json:"public_id"`
	ResolvedPrompt *ResolvedPrompt `json:"resolved_prompt,omitempty"`
}

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
	Runtime              *RuntimeSpec                      `json:"runtime,omitempty"`
	RuntimePath          string                            `json:"-"`
	Hooks                map[string][]config.HookEntry     `json:"hooks,omitempty"`
	HookPaths            []string                          `json:"-"`
	MCPServers           map[string]config.MCPServerConfig `json:"mcp_servers,omitempty"`
	MCPPaths             []string                          `json:"-"`
	Commands             []ResolvedCommand                 `json:"commands,omitempty"`
	CommandPaths         []string                          `json:"-"`
	Interface            json.RawMessage                   `json:"interface,omitempty"`
	Platforms            []string                          `json:"platforms,omitempty"`
	RequestedPermissions []string                          `json:"requested_permissions,omitempty"`
	ActivityKinds        []string                          `json:"activity_kinds,omitempty"`
	OfficialNativeHelper json.RawMessage                   `json:"official_native_helper,omitempty"`
	MinimumWuuVersion    string                            `json:"minimum_wuu_version,omitempty"`
	UnsupportedFields    []string                          `json:"unsupported_fields,omitempty"`
}

// RuntimeSpec declares a long-lived external plugin process. Installing or
// enabling the plugin grants this process the same user authority as Wuu.
type RuntimeSpec struct {
	Protocol string            `json:"protocol"`
	Command  string            `json:"command"`
	Args     []string          `json:"args,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Timeout  int               `json:"timeout,omitempty"`
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
	Runtime                json.RawMessage `json:"runtime"`
	Hooks                  json.RawMessage `json:"hooks"`
	MCPServers             json.RawMessage `json:"mcpServers"`
	MCPServersAlias        json.RawMessage `json:"mcp_servers"`
	Commands               json.RawMessage `json:"commands"`
	Contributes            rawContributes  `json:"contributes"`
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

type rawContributes struct {
	Commands json.RawMessage `json:"commands"`
}

var supportedManifestFields = map[string]struct{}{
	"id": {}, "name": {}, "description": {}, "version": {}, "author": {},
	"homepage": {}, "repository": {}, "license": {}, "keywords": {},
	"skills": {}, "runtime": {}, "hooks": {}, "mcpServers": {}, "mcp_servers": {},
	"contributes": {}, "interface": {}, "platforms": {},
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
	item := Plugin{
		Manifest:     manifest,
		Source:       strings.TrimSpace(options.Source),
		Root:         root,
		ManifestPath: path,
		Official:     options.Official,
	}
	contract, err := item.PackageContract()
	if err != nil {
		return Plugin{}, fmt.Errorf("fingerprint plugin manifest %s: %w", path, err)
	}
	item.SubjectID = contract.SubjectID
	item.Fingerprint = contract.Fingerprint
	item.EffectivePermissions = contract.Permissions
	return item, nil
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
	runtimeSpec, runtimePath, err := normalizeRuntime(root, raw.Runtime)
	if err != nil {
		return Manifest{}, err
	}
	hooks, hookPaths, err := normalizeHooks(root, raw.Hooks)
	if err != nil {
		return Manifest{}, err
	}
	mcpServers, mcpPaths, err := normalizeMCPServers(root, firstRaw(raw.MCPServers, raw.MCPServersAlias))
	if err != nil {
		return Manifest{}, err
	}
	commands, commandPaths, err := normalizeCommands(root, id, raw.Contributes.Commands)
	if err != nil {
		return Manifest{}, err
	}
	helper := firstRaw(raw.OfficialNativeHelper, raw.OfficialHelperAlias)
	if hasDeclaredValue(helper) && !official {
		return Manifest{}, fmt.Errorf("official_native_helper is reserved for official bundled plugins")
	}

	requested, err := extensions.NormalizePermissions(append(raw.RequestedPermsAlias, raw.RequestedPermissions...))
	if err != nil {
		return Manifest{}, err
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
		Runtime:              runtimeSpec,
		RuntimePath:          runtimePath,
		Hooks:                hooks,
		HookPaths:            hookPaths,
		MCPServers:           mcpServers,
		MCPPaths:             mcpPaths,
		Commands:             commands,
		CommandPaths:         commandPaths,
		Interface:            cloneRaw(raw.Interface),
		Platforms:            normalizeStrings(raw.Platforms),
		RequestedPermissions: requested,
		ActivityKinds:        normalizeStrings(append(raw.ActivityKindsAlias, raw.ActivityKinds...)),
		OfficialNativeHelper: cloneRaw(helper),
		MinimumWuuVersion:    firstString(raw.MinimumWuuVersion, raw.MinimumWuuVersionAlias),
		UnsupportedFields:    unsupported,
	}, nil
}

func normalizeRuntime(root string, raw json.RawMessage) (*RuntimeSpec, string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, "", nil
	}
	var spec RuntimeSpec
	if err := json.Unmarshal(trimmed, &spec); err != nil {
		return nil, "", fmt.Errorf("runtime: %w", err)
	}
	spec.Protocol = strings.TrimSpace(spec.Protocol)
	spec.Command = strings.TrimSpace(spec.Command)
	if spec.Protocol != pluginhost.ProtocolName {
		return nil, "", fmt.Errorf("runtime protocol must be %q", pluginhost.ProtocolName)
	}
	if spec.Command == "" {
		return nil, "", fmt.Errorf("runtime command is required")
	}
	if spec.Timeout < 0 {
		return nil, "", fmt.Errorf("runtime timeout must be positive")
	}
	var runtimePath string
	if strings.ContainsRune(spec.Command, filepath.Separator) && !filepath.IsAbs(spec.Command) {
		command, err := normalizePluginPath(root, "runtime.command", spec.Command)
		if err != nil {
			return nil, "", err
		}
		runtimePath = command
		spec.Command = filepath.Join(root, command)
	}
	spec.Args = append([]string(nil), spec.Args...)
	if len(spec.Env) > 0 {
		env := make(map[string]string, len(spec.Env))
		for key, value := range spec.Env {
			key = strings.TrimSpace(key)
			if key == "" || strings.Contains(key, "=") {
				return nil, "", fmt.Errorf("runtime env contains invalid name %q", key)
			}
			env[key] = value
		}
		spec.Env = env
	}
	return &spec, runtimePath, nil
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

func normalizeHooks(root string, raw json.RawMessage) (map[string][]config.HookEntry, []string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil, nil
	}
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var out map[string][]config.HookEntry
		if err := json.Unmarshal(trimmed, &out); err != nil {
			return nil, nil, fmt.Errorf("hooks: %w", err)
		}
		return out, nil, nil
	}
	paths, err := normalizePathList(root, "hooks", raw)
	if err != nil {
		return nil, nil, err
	}
	out := make(map[string][]config.HookEntry)
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return nil, nil, fmt.Errorf("read hooks %s: %w", path, err)
		}
		var wrapper struct {
			Hooks map[string][]config.HookEntry `json:"hooks"`
		}
		if err := json.Unmarshal(data, &wrapper); err != nil {
			return nil, nil, fmt.Errorf("parse hooks %s: %w", path, err)
		}
		for event, entries := range wrapper.Hooks {
			out[event] = append(out[event], entries...)
		}
	}
	return out, paths, nil
}

func normalizeMCPServers(root string, raw json.RawMessage) (map[string]config.MCPServerConfig, []string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil, nil
	}
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var out map[string]config.MCPServerConfig
		if err := json.Unmarshal(trimmed, &out); err != nil {
			return nil, nil, fmt.Errorf("mcpServers: %w", err)
		}
		return out, nil, nil
	}
	var ref string
	if err := json.Unmarshal(trimmed, &ref); err != nil {
		return nil, nil, fmt.Errorf("mcpServers must be an object or path")
	}
	path, err := normalizePluginPath(root, "mcpServers", ref)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return nil, nil, fmt.Errorf("read mcpServers %s: %w", path, err)
	}
	var wrapper struct {
		MCPServers map[string]config.MCPServerConfig `json:"mcpServers"`
		Alias      map[string]config.MCPServerConfig `json:"mcp_servers"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, nil, fmt.Errorf("parse mcpServers %s: %w", path, err)
	}
	if len(wrapper.MCPServers) > 0 {
		return wrapper.MCPServers, []string{path}, nil
	}
	return wrapper.Alias, []string{path}, nil
}

func normalizeCommands(root, pluginID string, raw json.RawMessage) ([]ResolvedCommand, []string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil, nil
	}
	var specs []CommandSpec
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &specs); err != nil {
			return nil, nil, fmt.Errorf("commands: %w", err)
		}
	} else if len(trimmed) > 0 && trimmed[0] == '{' {
		var wrapper struct {
			Commands []CommandSpec `json:"commands"`
		}
		if err := json.Unmarshal(trimmed, &wrapper); err == nil && len(wrapper.Commands) > 0 {
			specs = wrapper.Commands
		} else {
			var single CommandSpec
			if err := json.Unmarshal(trimmed, &single); err != nil {
				return nil, nil, fmt.Errorf("commands: %w", err)
			}
			specs = []CommandSpec{single}
		}
	} else {
		var ref string
		if err := json.Unmarshal(trimmed, &ref); err != nil {
			return nil, nil, fmt.Errorf("commands must be an array, object, or path")
		}
		path, err := normalizePluginPath(root, "commands", ref)
		if err != nil {
			return nil, nil, err
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return nil, nil, fmt.Errorf("read commands %s: %w", path, err)
		}
		resolved, nestedPaths, err := normalizeCommands(root, pluginID, data)
		if err != nil {
			return nil, nil, fmt.Errorf("parse commands %s: %w", path, err)
		}
		return resolved, append([]string{path}, nestedPaths...), nil
	}

	var out []ResolvedCommand
	var paths []string
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if err := validateCommandSpec(spec); err != nil {
			return nil, nil, fmt.Errorf("commands: %w", err)
		}
		if _, ok := seen[spec.ID]; ok {
			return nil, nil, fmt.Errorf("commands: duplicate id %q", spec.ID)
		}
		seen[spec.ID] = struct{}{}

		resolved := ResolvedCommand{
			CommandSpec: CommandSpec{
				ID:          spec.ID,
				Title:       strings.TrimSpace(spec.Title),
				Description: strings.TrimSpace(spec.Description),
				Kind:        commandKindFromString(string(spec.Kind)),
				Contexts:    normalizeStrings(spec.Contexts),
				Aliases:     normalizeStrings(spec.Aliases),
				Keywords:    normalizeStrings(spec.Keywords),
			},
			PublicID: pluginID + "." + spec.ID,
		}
		switch resolved.Kind {
		case CommandKindPromptTemplate:
			if strings.TrimSpace(spec.Prompt) == "" {
				return nil, nil, fmt.Errorf("commands: prompt_template %q requires prompt", spec.ID)
			}
			promptPath, err := normalizePluginPath(root, "commands.prompt", spec.Prompt)
			if err != nil {
				return nil, nil, err
			}
			prompt, err := readPromptFile(root, promptPath)
			if err != nil {
				return nil, nil, fmt.Errorf("commands: %w", err)
			}
			resolved.Prompt = promptPath
			resolved.ResolvedPrompt = prompt
			paths = append(paths, promptPath)
		case CommandKindRuntimeAction:
			// Reserved: validate and store the descriptor, but do not resolve or
			// execute anything.
		default:
			return nil, nil, fmt.Errorf("commands: unknown kind %q for %q", spec.Kind, spec.ID)
		}
		out = append(out, resolved)
	}
	return out, paths, nil
}

func validateCommandSpec(spec CommandSpec) error {
	if !validCommandID(spec.ID) {
		return fmt.Errorf("invalid command id %q", spec.ID)
	}
	if strings.TrimSpace(spec.Title) == "" {
		return fmt.Errorf("command %q requires title", spec.ID)
	}
	if commandKindFromString(string(spec.Kind)) == "" {
		return fmt.Errorf("command %q requires kind prompt_template or runtime_action", spec.ID)
	}
	return nil
}

func commandKindFromString(value string) CommandKind {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(CommandKindPromptTemplate):
		return CommandKindPromptTemplate
	case string(CommandKindRuntimeAction):
		return CommandKindRuntimeAction
	}
	return ""
}

func validCommandID(id string) bool {
	id = strings.TrimSpace(id)
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func readPromptFile(root, rel string) (*ResolvedPrompt, error) {
	path := filepath.Join(root, rel)
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("prompt %s: %w", rel, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("prompt %s is a directory", rel)
	}
	if info.Size() > maxPromptFileSize {
		return nil, fmt.Errorf("prompt %s exceeds %d bytes", rel, maxPromptFileSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("prompt %s: %w", rel, err)
	}
	if !utf8.ValidString(string(data)) {
		return nil, fmt.Errorf("prompt %s is not valid UTF-8", rel)
	}
	sum := sha256.Sum256(data)
	return &ResolvedPrompt{
		Path:    path,
		RelPath: rel,
		Size:    info.Size(),
		SHA256:  hex.EncodeToString(sum[:]),
		Text:    string(data),
	}, nil
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
	if err := ensureResolvedPathWithinRoot(root, filepath.Join(root, cleaned)); err != nil {
		return "", fmt.Errorf("%s path %q must remain within plugin root %s: %w", field, value, root, err)
	}
	return cleaned, nil
}

func ensureResolvedPathWithinRoot(root, candidate string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	realRoot, err = filepath.Abs(realRoot)
	if err != nil {
		return err
	}
	probe := candidate
	var suffix []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(probe)
		if resolveErr == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			resolved, err = filepath.Abs(resolved)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(realRoot, resolved)
			if err != nil {
				return err
			}
			if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
				return errors.New("resolved path escapes plugin root")
			}
			return nil
		}
		if !os.IsNotExist(resolveErr) {
			return resolveErr
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return resolveErr
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = parent
	}
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
