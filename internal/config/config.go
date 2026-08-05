package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/extensions"
	"github.com/blueberrycongee/wuu/internal/securefs"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/internal/storelock"
	"github.com/blueberrycongee/wuu/prompts"
)

const (
	localPrimaryConfig  = ".wuu.json"
	localFallbackConfig = "wuu.json"

	DefaultAgentName = "default"
	// DefaultAgentMaxParallel is the shared worker concurrency limit used when
	// agent.max_parallel is omitted or set to zero.
	DefaultAgentMaxParallel = 5

	// DefaultDreamIntervalDays is the minimum elapsed-time gate for background
	// consolidation. A dream also requires the runtime's minimum number of
	// completed sessions, so reaching this interval alone never starts a pass.
	DefaultDreamIntervalDays = 1

	defaultCodexSubscriptionBaseURL = "https://chatgpt.com/backend-api/codex"
)

type ToolLoadingMode string

const (
	ToolLoadingAuto   ToolLoadingMode = "auto"
	ToolLoadingFlat   ToolLoadingMode = "flat"
	ToolLoadingNative ToolLoadingMode = "native"
)

// ErrConfigNotFound is returned by LoadFrom when none of the candidate
// config files exist on disk. Callers should use errors.Is to
// distinguish a missing config (where initializing defaults is the right
// recovery) from a present-but-broken config (where overwriting it
// would silently destroy the user's work).
var ErrConfigNotFound = errors.New("config not found")

// HookEntry defines a single hook command bound to a lifecycle event.
type HookEntry struct {
	Matcher string `json:"matcher,omitempty"` // tool name pattern, "*" or empty = match all
	Type    string `json:"type,omitempty"`    // "command" (default) or "prompt"
	Command string `json:"command,omitempty"` // for type=command — shell command
	Prompt  string `json:"prompt,omitempty"`  // for type=prompt — evaluation prompt
	Model   string `json:"model,omitempty"`   // for type=prompt — model to use
	Timeout int    `json:"timeout,omitempty"` // seconds, default 30
}

// MCPServerConfig configures one MCP server connection.
type MCPServerConfig struct {
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"`
	// Transport selects the wire protocol for URL servers: "http" (streamable
	// HTTP per the MCP spec, alias "streamable-http") or "sse" (legacy
	// HTTP+SSE). Empty means auto: try streamable HTTP first, fall back to SSE
	// when the endpoint rejects the initialize POST with an HTTP 4xx. The
	// names follow Claude Code's .mcp.json `type` convention. Ignored for
	// stdio (command) servers.
	Transport     string                     `json:"transport,omitempty"`
	Env           map[string]string          `json:"env,omitempty"`
	Headers       map[string]string          `json:"headers,omitempty"`
	OAuth         *MCPOAuthConfig            `json:"oauth,omitempty"`
	Enabled       *bool                      `json:"enabled,omitempty"`
	ToolOverrides map[string]MCPToolOverride `json:"tool_overrides,omitempty"`
}

type MCPOAuthConfig struct {
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
	RedirectURI  string   `json:"redirect_uri,omitempty"`
}

// MCPToolOverride corrects or supplements server-provided MCP tool metadata.
type MCPToolOverride struct {
	ReadOnly        *bool                 `json:"read_only,omitempty"`
	ConcurrencySafe *bool                 `json:"concurrency_safe,omitempty"`
	Capability      capability.Capability `json:"capability,omitempty"`
}

// Config holds CLI runtime settings.
type Config struct {
	DefaultProvider string                    `json:"default_provider"`
	Providers       map[string]ProviderConfig `json:"providers"`
	Agent           AgentConfig               `json:"agent"`
	Hooks           map[string][]HookEntry    `json:"hooks,omitempty"`
	Memory          MemoryConfig              `json:"memory,omitempty"`
	// MCPServers maps server name to connection config. When present, wuu
	// connects to each server at startup (in the background) and exposes
	// its tools to the agent.
	MCPServers map[string]MCPServerConfig `json:"mcp_servers,omitempty"`
	// MCPJson gates and approves Claude Code project-level `.mcp.json` servers.
	// `.mcp.json` entries are never loaded until approved here (recommended in
	// `.wuu/settings.local.json`). See mcpjson.go. It is a pointer so an unset
	// section stays out of serialized templates.
	MCPJson *MCPJsonTrust `json:"mcp_json,omitempty"`
	// Extensions carries machine-local trust grants for executable extension
	// configurations. Shared project settings are stripped before merge; UI
	// writers must persist grants in .wuu/settings.local.json.
	Extensions *extensions.Settings `json:"extensions,omitempty"`
}

// MemoryConfig overrides the defaults for memory file discovery. All fields
// are optional; empty values fall back to memory.DefaultOptions().
type MemoryConfig struct {
	// Filenames to look for in priority order.
	Filenames []string `json:"filenames,omitempty"`
	// ProjectRootMarkers stop the upward walk through ancestors.
	// Default: [".git", ".hg", ".jj", ".svn"].
	ProjectRootMarkers []string `json:"project_root_markers,omitempty"`
	// UserDirs are scanned for user-level memory. Tilde-expanded.
	UserDirs []string `json:"user_dirs,omitempty"`
	// IncludeLegacyMemory imports Claude-style rules, local files, and
	// auto-memory paths. It is off by default and intended for explicit
	// migration, not normal request context.
	IncludeLegacyMemory *bool `json:"include_legacy_memory,omitempty"`
	// Disable turns off durable memory discovery and user-notebook injection.
	Disable bool `json:"disable,omitempty"`
	// NudgeInterval is retained for compatibility with configs written by the
	// retired profile-memory reviewer. Current runtimes ignore it.
	NudgeInterval *int `json:"nudge_interval,omitempty"`
	// MemoryCharLimit is retained for compatibility with the retired indexed
	// memory store. Current notebook memory does not apply this limit.
	MemoryCharLimit int `json:"memory_char_limit,omitempty"`
	// UserCharLimit is retained for compatibility with the retired indexed
	// memory store. Current notebook memory does not apply this limit.
	UserCharLimit int `json:"user_char_limit,omitempty"`
	// DreamIntervalDays is the minimum elapsed-time gate for the background
	// dream pass. The pass also requires five completed sessions since the last
	// successful consolidation. nil means the default interval; 0 disables it.
	//
	// Deprecated: use Dream.IntervalDays instead. This field is retained so
	// existing configs keep working.
	DreamIntervalDays *int `json:"dream_interval_days,omitempty"`
	// Dream configures the background session-dream memory consolidation pass.
	Dream *DreamConfig `json:"dream,omitempty"`
}

// DreamConfig configures the background session-dream memory consolidation
// pass. It is intentionally scoped: an enable switch, interval, and optional
// dedicated provider/model. Empty provider/model means the dream uses the
// currently selected main provider/model.
type DreamConfig struct {
	Enabled      bool   `json:"enabled,omitempty"`
	IntervalDays int    `json:"interval_days,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
}

func (m MemoryConfig) DreamEnabled() bool {
	if m.Dream != nil {
		return m.Dream.Enabled
	}
	// Backward compatibility: the legacy dream_interval_days field toggled the
	// pass by value. A positive interval meant enabled; zero meant disabled.
	if m.DreamIntervalDays != nil {
		return *m.DreamIntervalDays > 0
	}
	return false
}

func (m MemoryConfig) DreamIntervalDaysValue() int {
	if m.Dream != nil && m.Dream.IntervalDays > 0 {
		return m.Dream.IntervalDays
	}
	if m.DreamIntervalDays != nil {
		return *m.DreamIntervalDays
	}
	return DefaultDreamIntervalDays
}

func (m MemoryConfig) DreamProvider() string {
	if m.Dream != nil {
		return strings.TrimSpace(m.Dream.Provider)
	}
	return ""
}

func (m MemoryConfig) DreamModel() string {
	if m.Dream != nil {
		return strings.TrimSpace(m.Dream.Model)
	}
	return ""
}

// ProviderConfig configures one model gateway.
type ProviderConfig struct {
	Type         string                         `json:"type"`
	BaseURL      string                         `json:"base_url"`
	API          string                         `json:"api,omitempty"`
	NPM          string                         `json:"npm,omitempty"`
	WireAPI      string                         `json:"wire_api,omitempty"`
	APIKey       string                         `json:"api_key,omitempty"`
	APIKeyEnv    string                         `json:"api_key_env,omitempty"`
	AuthToken    string                         `json:"auth_token,omitempty"`
	AuthTokenEnv string                         `json:"auth_token_env,omitempty"`
	Model        string                         `json:"model"`
	Models       map[string]ProviderModelConfig `json:"models,omitempty"`
	Headers      map[string]string              `json:"headers,omitempty"`
	// ReuseCodexCredentials lets Codex subscription providers read the local
	// Codex CLI auth store (CODEX_HOME/auth.json or ~/.codex/auth.json) as a
	// read-only fallback when wuu does not have its own OAuth session.
	ReuseCodexCredentials bool `json:"reuse_codex_credentials,omitempty"`
	// StreamConnectTimeoutMS bounds dial and TLS handshake for one streaming
	// connection attempt. It does not cap the whole turn.
	StreamConnectTimeoutMS int `json:"stream_connect_timeout_ms,omitempty"`
	// StreamHeaderTimeoutMS bounds the wait from request sent to first
	// response headers, which includes server-side queueing and prompt
	// prefill. Large-context requests need a far looser deadline here than
	// the connect stage.
	StreamHeaderTimeoutMS int `json:"stream_header_timeout_ms,omitempty"`
	// StreamIdleTimeoutMS bounds silence after the streaming response has
	// started. It does not affect the initial connect stage.
	StreamIdleTimeoutMS int `json:"stream_idle_timeout_ms,omitempty"`
	// StreamTransport selects the preferred streaming transport for providers
	// that support more than one path: auto, sse, websocket, or websocket-cached.
	StreamTransport string `json:"stream_transport,omitempty"`
	// ContextWindow optionally overrides wuu's built-in registry for
	// this provider's model. Use it for new models wuu doesn't know
	// about yet, custom finetunes, private deployments, or proxies
	// that rename the upstream model. Zero means "use the registry".
	ContextWindow int `json:"context_window,omitempty"`
	// CacheCreationInputTokensOmitted marks Anthropic-compatible endpoints
	// that omit cache_creation_input_tokens from usage payloads. When set,
	// Wuu reports cache creation as unknown instead of showing a misleading
	// literal zero.
	CacheCreationInputTokensOmitted bool `json:"cache_creation_input_tokens_omitted,omitempty"`
	// InputTokensIncludeCacheRead marks Anthropic-compatible endpoints whose
	// input_tokens are inclusive of cache_read_input_tokens, unlike native
	// Anthropic where input_tokens exclude cached tokens. When set, Wuu
	// subtracts cache_read from input so context occupancy is not
	// double-counted. Orthogonal to CacheCreationInputTokensOmitted. Never
	// auto-detected: verify the endpoint's semantics empirically (two
	// identical cache_control requests; inclusive implies input >= cache_read
	// on the warm request) before enabling. MiniMax's anthropic endpoint was
	// live-probed EXCLUSIVE on 2026-07-06 and must not set this. Defaults to
	// false.
	InputTokensIncludeCacheRead bool `json:"input_tokens_include_cache_read,omitempty"`
}

// ProviderModelProviderConfig mirrors OpenCode's per-model provider override
// shape. It lets users pin the upstream AI SDK package and API endpoint for
// custom model aliases.
type ProviderModelProviderConfig struct {
	API string `json:"api,omitempty"`
	NPM string `json:"npm,omitempty"`
}

// ProviderModelLimitConfig carries model token limits used by provider-specific
// option generation.
type ProviderModelLimitConfig struct {
	Context int `json:"context,omitempty"`
	Input   int `json:"input,omitempty"`
	Output  int `json:"output,omitempty"`
}

// ProviderModelConfig lets a provider expose a small model catalog without
// forcing users to duplicate full provider definitions. The OpenCode-compatible
// metadata fields are intentionally accepted so wuu can derive the same
// model-specific variants when a config was copied from OpenCode/models.dev.
type ProviderModelConfig struct {
	ID               string                         `json:"id,omitempty"`
	Name             string                         `json:"name,omitempty"`
	Family           string                         `json:"family,omitempty"`
	Status           string                         `json:"status,omitempty"`
	ReleaseDate      string                         `json:"release_date,omitempty"`
	Reasoning        *bool                          `json:"reasoning,omitempty"`
	ReasoningOptions []map[string]any               `json:"reasoning_options,omitempty"`
	Attachment       *bool                          `json:"attachment,omitempty"`
	ToolCall         *bool                          `json:"tool_call,omitempty"`
	StructuredOutput *bool                          `json:"structured_output,omitempty"`
	Temperature      *bool                          `json:"temperature,omitempty"`
	Interleaved      any                            `json:"interleaved,omitempty"`
	Modalities       *ProviderModelModalitiesConfig `json:"modalities,omitempty"`
	Cost             map[string]any                 `json:"cost,omitempty"`
	Provider         *ProviderModelProviderConfig   `json:"provider,omitempty"`
	Limit            *ProviderModelLimitConfig      `json:"limit,omitempty"`
	Options          map[string]any                 `json:"options,omitempty"`
	Headers          map[string]string              `json:"headers,omitempty"`
	SupportedEfforts []string                       `json:"supported_efforts,omitempty"`
	DefaultEffort    string                         `json:"default_effort,omitempty"`
	DefaultVariant   string                         `json:"default_variant,omitempty"`
	Variants         map[string]map[string]any      `json:"variants,omitempty"`
	Disabled         bool                           `json:"disabled,omitempty"`
	ContextWindow    int                            `json:"context_window,omitempty"`
}

type ProviderModelModalitiesConfig struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

// AgentConfig controls behavior of the local tool loop.
type AgentConfig struct {
	// Name identifies the configured agent profile. Memory is global per user
	// and is not scoped by this value.
	Name             string `json:"name,omitempty"`
	MaxSteps         int    `json:"max_steps"`
	MaxContextTokens int    `json:"max_context_tokens"`
	// MaxParallel limits concurrently executing anonymous workers. Queued
	// workers do not count toward the limit. Zero selects the default.
	MaxParallel int `json:"max_parallel,omitempty"`
	// UltraMode enables proactive multi-agent delegation for top-level turns.
	// The runtime snapshots this value at the turn boundary.
	UltraMode bool `json:"ultra_mode,omitempty"`
	// Temperature overrides model/provider sampling when greater than zero.
	// Zero means Auto: omit the request field and let the provider or model
	// compatibility layer choose.
	Temperature float64 `json:"temperature,omitempty"`
	// CompactThresholdPct overrides the default usable-window trigger for
	// proactive compaction. Zero means auto; custom values are fractions in
	// (0,1), for example 0.5 for 50%.
	CompactThresholdPct float64 `json:"compact_threshold_pct,omitempty"`
	// CompactKeepRecentTokens is the recent raw-history budget kept after
	// compaction. Zero means use the default 20K tokens.
	CompactKeepRecentTokens int `json:"compact_keep_recent_tokens,omitempty"`
	// SystemPrompt is a legacy user-customized prompt field. It is appended
	// after wuu's built-in base prompt instead of replacing it.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// AppendSystemPrompt is the preferred field for user or project-specific
	// instructions that should customize, not replace, wuu's base behavior.
	AppendSystemPrompt string `json:"append_system_prompt,omitempty"`
	// GitAttributionEnabled controls whether commits created through WUU's
	// agent tool surface receive the WUU Agent co-author trailer. Nil defaults
	// to enabled; an explicit false is the user opt-out.
	GitAttributionEnabled *bool `json:"git_attribution_enabled,omitempty"`
	// PermissionMode selects the authority boundary. Empty resolves to standard.
	PermissionMode string `json:"permission_mode,omitempty"`
	// Effort controls reasoning depth. Valid: "low", "medium", "high",
	// "max" (Anthropic only). Empty = API default. Aligned with Claude
	// Code's /effort command and Codex's reasoning_effort setting.
	Effort string `json:"effort,omitempty"`
	// Variant selects a model-scoped provider option bundle. It supersedes
	// Effort when the selected provider/model exposes OpenCode-style variants.
	Variant string `json:"variant,omitempty"`
	// ModelRoles lets role-specific runtime work use a different model while
	// preserving the main model as the default. Empty role entries inherit the
	// active provider/model/effort/variant selected above.
	ModelRoles ModelRolesConfig `json:"model_roles,omitempty"`
	// ModelAliases are stable labels that skills can pass as spawn_agent.model
	// values. Each alias must explicitly name a configured provider and model;
	// unlike model_roles entries, aliases never inherit from the active main
	// selection. Project layers cannot define aliases.
	ModelAliases map[string]ModelRoleConfig `json:"model_aliases,omitempty"`
	// DisableAutoCompact turns off the proactive auto-compact pass
	// that fires when the conversation reaches the model's usable input
	// window after reserving output headroom. The reactive overflow
	// recovery (compact triggered by an actual context_length_exceeded
	// error) still runs. Use this when you want full control over compact
	// via the slash command, or when you're debugging compact behavior
	// itself.
	DisableAutoCompact bool `json:"disable_auto_compact,omitempty"`
	// CatwalkAutoupdate enables the background fetch from charm.land's
	// catwalk service to refresh the model→context-window registry
	// between wuu builds. Disabled by default — wuu's embedded
	// snapshot is already curated and the remote fetch isn't needed
	// unless the user is on the bleeding edge of new models. When
	// disabled, only the embedded data ships with each wuu binary
	// is used.
	CatwalkAutoupdate bool `json:"catwalk_autoupdate,omitempty"`
	// ToolLoading controls how Wuu exposes large/deferred tool surfaces.
	// Empty means "auto": first-party native provider paths use provider
	// deferred-loading protocol; every other path uses a flat tool list.
	// Valid: auto, flat, native.
	//
	// The retired "wuu_tool_search" / "tool_search" values still parse, but
	// resolve to auto and print a one-time deprecation notice. Wuu's own
	// progressive loading rewrote the top-level tools array mid-conversation,
	// which invalidated the provider prompt cache after the insertion point.
	ToolLoading ToolLoadingMode `json:"tool_loading,omitempty"`
	// ToolSearch is a legacy alias kept for older config files. New configs
	// should use tool_loading. true now means auto, not Wuu progressive
	// loading, which no longer exists.
	ToolSearch *bool `json:"tool_search,omitempty"`
	// ExperimentalDeferredToolBundles enables the provider-native bundle
	// activation path where successful tools can attach follow-on schemas.
	// It is off by default; the main path is tool_search or flat tools.
	ExperimentalDeferredToolBundles bool `json:"experimental_deferred_tool_bundles,omitempty"`
	// ExperimentalHelpMe exposes the unfinished HelpMe recovery flow. Keep it
	// disabled by default until its user-facing contract is ready to ship.
	ExperimentalHelpMe bool `json:"experimental_helpme,omitempty"`
	// ExperimentalCoordinatorMode exposes the old coordinator slash mode
	// for local experimentation. Disabled by default because the mode's
	// user-facing contract is still unclear: the main agent loses some
	// direct write tools but not every mutating capability.
	ExperimentalCoordinatorMode bool `json:"experimental_coordinator_mode,omitempty"`
}

// MaxParallelValue resolves the configured worker concurrency limit.
func (a AgentConfig) MaxParallelValue() int {
	if a.MaxParallel <= 0 {
		return DefaultAgentMaxParallel
	}
	return a.MaxParallel
}

// ModelRolesConfig configures provider/model choices for non-main runtime
// roles. The runtime resolves empty role fields by inheriting from the active
// main model, so adding this shape is backwards-compatible with existing
// configs.
type ModelRolesConfig struct {
	Review   ModelRoleConfig `json:"review,omitempty"`
	Compact  ModelRoleConfig `json:"compact,omitempty"`
	Title    ModelRoleConfig `json:"title,omitempty"`
	Memory   ModelRoleConfig `json:"memory,omitempty"`
	Worker   ModelRoleConfig `json:"worker,omitempty"`
	Fallback ModelRoleConfig `json:"fallback,omitempty"`
}

// ModelRoleConfig pins a role to a provider/model/variant selection. Provider
// defaults to the main provider. Model defaults to the selected provider's
// configured model. Variant supersedes Effort when the provider exposes
// model-scoped variants, matching AgentConfig's main model semantics.
type ModelRoleConfig struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
	Variant  string `json:"variant,omitempty"`
}

type AdvancedRuntimeUpdate struct {
	MaxSteps                *int
	MaxContextTokens        *int
	Temperature             *float64
	CompactThresholdPct     *float64
	CompactKeepRecentTokens *int
	DisableAutoCompact      *bool
	ProviderContextWindow   *int
	// ModelAliases replaces the entire agent.model_aliases map. A non-nil map
	// clears any existing aliases and writes only the entries with non-nil
	// values. Settings uses this to add, edit, and delete aliases in one call.
	ModelAliases map[string]*ModelRoleConfig
}

type GeneralSettingsUpdate struct {
	AppendSystemPrompt    *string
	GitAttributionEnabled *bool
	MemoryDisable         *bool
	MCPEnabledToggles     map[string]*bool // server name → enabled; nil = skip
	DreamEnabled          *bool
	DreamIntervalDays     *int
	DreamProvider         *string
	DreamModel            *string
}

func (a AgentConfig) GitAttributionEnabledValue() bool {
	return a.GitAttributionEnabled == nil || *a.GitAttributionEnabled
}

// LoadFrom reads config from deterministic directories (test-friendly).
//
// The user config is the trusted base: the unified ~/.wuu/config.json (or
// WUU_HOME/config.json) first, then the legacy ~/.config/wuu/config.json.
// statepath resolves WUU_HOME even when the HOME environment variable is
// absent, and otherwise falls back to the operating-system user home. Project
// files are overlays in this order:
// .wuu.json (or wuu.json), .wuu/settings.json, then
// .wuu/settings.local.json. Project overlays cannot define provider
// connections or credential sources, expand memory discovery outside the
// workspace, or loosen the user's permission mode. This prevents a repository
// from choosing where user credentials are sent merely by being opened.
//
// Callers that deliberately trust a project config must opt into
// LoadProjectConfig or LoadPath; an empty home argument is never a hidden
// switch that grants project files this authority.
func LoadFrom(workdir, home string) (Config, string, error) {
	migrateLegacyGlobalStore(home)
	var userCandidates []string
	newPath, err := statepath.ConfigPath(home)
	if err != nil {
		return Config{}, "", fmt.Errorf("resolve user config: %w", err)
	}
	if newPath != "" {
		userCandidates = append(userCandidates, newPath)
	}
	if legacyPath := statepath.LegacyConfigPath(home); legacyPath != "" {
		userCandidates = append(userCandidates, legacyPath)
	}

	for _, candidate := range userCandidates {
		cfg, readErr := readConfig(candidate)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			return Config{}, "", readErr
		}
		layered, appliedLayers, layerErr := applyRestrictedProjectLayers(candidate, workdir, cfg)
		if layerErr != nil {
			return Config{}, "", layerErr
		}
		cfg = layered
		// Read and translate Claude Code's project-level `.mcp.json`, if
		// present. Approved servers (per the mcp_json trust section, now
		// fully merged into cfg above) are merged into cfg.MCPServers. This
		// is a no-op when the file is absent. workdir is the project root,
		// the same directory used for the settings layers above.
		applyMCPJsonServers(&cfg, workdir)
		applyDefaults(&cfg)
		if validateErr := cfg.Validate(); validateErr != nil {
			return Config{}, "", validateErr
		}
		logSettingsLayerDebug(candidate, appliedLayers)
		return cfg, candidate, nil
	}

	configPath := "~/.wuu/config.json"
	if strings.TrimSpace(newPath) != "" {
		configPath = newPath
	}
	return Config{}, "", fmt.Errorf("%w, run `wuu init` to create user config %s", ErrConfigNotFound, configPath)
}

// LoadProjectConfig explicitly trusts the first project config (.wuu.json,
// then wuu.json) as a complete standalone config and applies both project
// settings layers. It is reserved for explicit CLI choices such as
// --ignore-user-config; normal desktop and CLI startup must use LoadFrom.
func LoadProjectConfig(workdir string) (Config, string, error) {
	candidates := []string{
		filepath.Join(workdir, localPrimaryConfig),
		filepath.Join(workdir, localFallbackConfig),
	}
	for _, candidate := range candidates {
		cfg, err := readConfig(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Config{}, "", err
		}
		layered, appliedLayers, err := applyProjectSettingsLayers(candidate, workdir, cfg)
		if err != nil {
			return Config{}, "", err
		}
		cfg = layered
		applyMCPJsonServers(&cfg, workdir)
		applyDefaults(&cfg)
		if err := cfg.Validate(); err != nil {
			return Config{}, "", err
		}
		logSettingsLayerDebug(candidate, appliedLayers)
		return cfg, candidate, nil
	}
	return Config{}, "", fmt.Errorf("%w, pass --config or run `wuu init` to create a user config", ErrConfigNotFound)
}

func LoadPath(path string) (Config, string, error) {
	resolved := strings.TrimSpace(path)
	if resolved == "" {
		return Config{}, "", errors.New("config path is required")
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return Config{}, "", fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := readConfig(abs)
	if err != nil {
		return Config{}, "", err
	}
	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, "", err
	}
	return cfg, abs, nil
}

func readConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	return decodeConfig(data, path)
}

// decodeConfig parses config bytes with the same strict semantics as the
// on-disk reader: unknown fields are rejected (DisallowUnknownFields) and the
// legacy Codex credential-reuse defaults are applied from the raw provider
// objects. sourcePath only labels parse errors. It is shared by readConfig and
// the settings-layer merger (settings_layer.go) so both honor identical schema
// strictness.
func decodeConfig(data []byte, sourcePath string) (Config, error) {
	var cfg Config
	sanitized := stripLegacyPermissionKeys(data)
	dec := json.NewDecoder(bytes.NewReader(sanitized))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", sourcePath, err)
	}
	var raw struct {
		Providers map[string]map[string]json.RawMessage `json:"providers"`
	}
	_ = json.Unmarshal(data, &raw)
	applyLegacyCodexCredentialReuseDefaults(&cfg, raw.Providers)

	return cfg, nil
}

func stripLegacyPermissionKeys(data []byte) []byte {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return data
	}
	agent, _ := raw["agent"].(map[string]any)
	if agent == nil {
		return data
	}
	for _, key := range []string{
		"tool_policy",
		"permission_rules",
		"permission_profile",
		"approval_policy",
		"approvals_reviewer",
	} {
		delete(agent, key)
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return data
	}
	return out
}

func applyLegacyCodexCredentialReuseDefaults(cfg *Config, rawProviders map[string]map[string]json.RawMessage) {
	if cfg == nil || len(rawProviders) == 0 || len(cfg.Providers) == 0 {
		return
	}
	for name, rawProvider := range rawProviders {
		if rawProvider == nil {
			continue
		}
		if _, explicit := rawProvider["reuse_codex_credentials"]; explicit {
			continue
		}
		provider, ok := cfg.Providers[name]
		if !ok || !isCodexSubscriptionProvider(provider.Type) {
			continue
		}
		if !usesDefaultCodexSubscriptionBaseURL(provider.BaseURL) {
			continue
		}
		provider.ReuseCodexCredentials = true
		cfg.Providers[name] = provider
	}
}

func usesDefaultCodexSubscriptionBaseURL(baseURL string) bool {
	normalized := strings.TrimRight(strings.ToLower(strings.TrimSpace(baseURL)), "/")
	return normalized == "" || normalized == defaultCodexSubscriptionBaseURL
}

// ResolveProvider returns explicit provider or default one.
func (c Config) ResolveProvider(name string) (ProviderConfig, string, error) {
	if len(c.Providers) == 0 {
		return ProviderConfig{}, "", errors.New("providers is empty")
	}

	if name != "" {
		p, ok := c.Providers[name]
		if !ok {
			return ProviderConfig{}, "", fmt.Errorf("provider %q not found", name)
		}
		return p, name, nil
	}

	p, ok := c.Providers[c.DefaultProvider]
	if !ok {
		return ProviderConfig{}, "", fmt.Errorf("default provider %q not found", c.DefaultProvider)
	}
	return p, c.DefaultProvider, nil
}

// Validate performs semantic checks.
func (c Config) Validate() error {
	if len(c.Providers) == 0 {
		return errors.New("providers is required")
	}
	if c.DefaultProvider == "" {
		return errors.New("default_provider is required")
	}
	if _, ok := c.Providers[c.DefaultProvider]; !ok {
		return fmt.Errorf("default_provider %q not found in providers", c.DefaultProvider)
	}

	for name, provider := range c.Providers {
		if provider.Type == "" {
			return fmt.Errorf("providers.%s.type is required", name)
		}
		if provider.BaseURL == "" && !isCodexSubscriptionProvider(provider.Type) {
			return fmt.Errorf("providers.%s.base_url is required", name)
		}
		if provider.Model == "" {
			return fmt.Errorf("providers.%s.model is required", name)
		}
		if provider.ContextWindow < 0 {
			return fmt.Errorf("providers.%s.context_window cannot be negative", name)
		}
		for modelID, model := range provider.Models {
			if strings.TrimSpace(modelID) == "" {
				return fmt.Errorf("providers.%s.models contains an empty model id", name)
			}
			if model.ContextWindow < 0 {
				return fmt.Errorf("providers.%s.models.%s.context_window cannot be negative", name, modelID)
			}
			if model.Limit != nil {
				if model.Limit.Context < 0 {
					return fmt.Errorf("providers.%s.models.%s.limit.context cannot be negative", name, modelID)
				}
				if model.Limit.Input < 0 {
					return fmt.Errorf("providers.%s.models.%s.limit.input cannot be negative", name, modelID)
				}
				if model.Limit.Output < 0 {
					return fmt.Errorf("providers.%s.models.%s.limit.output cannot be negative", name, modelID)
				}
			}
			for _, effort := range model.SupportedEfforts {
				if strings.TrimSpace(effort) == "" {
					return fmt.Errorf("providers.%s.models.%s.supported_efforts contains an empty value", name, modelID)
				}
			}
			for variantID, variant := range model.Variants {
				if strings.TrimSpace(variantID) == "" {
					return fmt.Errorf("providers.%s.models.%s.variants contains an empty variant id", name, modelID)
				}
				if disabled, ok := variant["disabled"]; ok {
					if _, valid := disabled.(bool); !valid {
						return fmt.Errorf("providers.%s.models.%s.variants.%s.disabled must be a boolean", name, modelID, variantID)
					}
				}
			}
		}
		switch provider.WireAPI {
		case "", "chat", "responses":
		default:
			return fmt.Errorf("providers.%s.wire_api must be \"chat\" or \"responses\"", name)
		}
		if isCodexSubscriptionProvider(provider.Type) && provider.WireAPI == "chat" {
			return fmt.Errorf("providers.%s.wire_api must be \"responses\" for %s", name, provider.Type)
		}
		if provider.StreamConnectTimeoutMS < 0 {
			return fmt.Errorf("providers.%s.stream_connect_timeout_ms cannot be negative", name)
		}
		if provider.StreamHeaderTimeoutMS < 0 {
			return fmt.Errorf("providers.%s.stream_header_timeout_ms cannot be negative", name)
		}
		if provider.StreamIdleTimeoutMS < 0 {
			return fmt.Errorf("providers.%s.stream_idle_timeout_ms cannot be negative", name)
		}
		switch strings.ToLower(strings.TrimSpace(provider.StreamTransport)) {
		case "", "auto", "sse", "websocket", "websocket-cached", "websocket_cached":
		default:
			return fmt.Errorf("providers.%s.stream_transport must be \"auto\", \"sse\", \"websocket\", or \"websocket-cached\"", name)
		}
	}

	if c.Agent.MaxSteps < 0 {
		return errors.New("agent.max_steps cannot be negative (use 0 for unlimited)")
	}
	if c.Agent.MaxParallel < 0 {
		return errors.New("agent.max_parallel cannot be negative (use 0 for default)")
	}
	if c.Agent.MaxContextTokens < 0 {
		return errors.New("agent.max_context_tokens cannot be negative (use 0 for auto)")
	}
	if c.Agent.Temperature < 0 || c.Agent.Temperature > 2 {
		return errors.New("agent.temperature must be in [0,2] (0 means Auto)")
	}
	if c.Agent.CompactThresholdPct < 0 || c.Agent.CompactThresholdPct >= 1 {
		return errors.New("agent.compact_threshold_pct must be in [0,1)")
	}
	if c.Agent.CompactKeepRecentTokens < 0 {
		return errors.New("agent.compact_keep_recent_tokens cannot be negative (use 0 for default)")
	}
	if strings.TrimSpace(string(c.Agent.ToolLoading)) != "" && NormalizeToolLoadingMode(c.Agent.ToolLoading) == "" {
		return errors.New("agent.tool_loading must be one of auto, flat, or native")
	}
	if c.Memory.DreamIntervalDays != nil && *c.Memory.DreamIntervalDays < 0 {
		return errors.New("memory.dream_interval_days cannot be negative")
	}
	if c.Memory.Dream != nil && c.Memory.Dream.IntervalDays < 0 {
		return errors.New("memory.dream.interval_days cannot be negative")
	}
	if c.Memory.Dream != nil && strings.TrimSpace(c.Memory.Dream.Provider) != "" {
		if _, ok := c.Providers[c.Memory.Dream.Provider]; !ok {
			return fmt.Errorf("memory.dream.provider %q not found in providers", c.Memory.Dream.Provider)
		}
	}
	if err := validatePermissionConfig(c.Agent); err != nil {
		return err
	}
	if err := validateModelRolesConfig(c); err != nil {
		return err
	}
	if err := validateModelAliasesConfig(c); err != nil {
		return err
	}

	return nil
}

func validateModelRolesConfig(c Config) error {
	roles := map[string]ModelRoleConfig{
		"review":   c.Agent.ModelRoles.Review,
		"compact":  c.Agent.ModelRoles.Compact,
		"title":    c.Agent.ModelRoles.Title,
		"memory":   c.Agent.ModelRoles.Memory,
		"worker":   c.Agent.ModelRoles.Worker,
		"fallback": c.Agent.ModelRoles.Fallback,
	}
	for role, cfg := range roles {
		provider := strings.TrimSpace(cfg.Provider)
		if provider == "" {
			continue
		}
		if _, ok := c.Providers[provider]; !ok {
			return fmt.Errorf("agent.model_roles.%s.provider %q not found in providers", role, provider)
		}
	}
	return nil
}

var modelAliasNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func normalizeModelAliasName(name string) string {
	return strings.TrimSpace(name)
}

func validateModelAliasesConfig(c Config) error {
	seen := make(map[string]string, len(c.Agent.ModelAliases))
	for rawName, alias := range c.Agent.ModelAliases {
		name := normalizeModelAliasName(rawName)
		if name == "" {
			return errors.New("agent.model_aliases contains an alias with an empty normalized name")
		}
		if !modelAliasNamePattern.MatchString(name) {
			return fmt.Errorf("agent.model_aliases alias %q is invalid: alias names must start with a lowercase letter and contain only lowercase letters, digits, underscores, and hyphens", name)
		}
		if previousRaw, ok := seen[name]; ok {
			return fmt.Errorf("agent.model_aliases aliases %q and %q normalize to the same name", previousRaw, rawName)
		}
		seen[name] = rawName

		providerName := strings.TrimSpace(alias.Provider)
		if providerName == "" {
			return fmt.Errorf("agent.model_aliases.%s.provider is required", name)
		}
		providerCfg, ok := c.Providers[providerName]
		if !ok {
			return fmt.Errorf("agent.model_aliases.%s.provider %q not found in providers", name, providerName)
		}
		model := strings.TrimSpace(alias.Model)
		if model == "" {
			return fmt.Errorf("agent.model_aliases.%s.model is required", name)
		}
		if err := validateAliasEffortVariant(name, alias, providerCfg, model); err != nil {
			return err
		}
	}
	return nil
}

func validateAliasEffortVariant(aliasName string, alias ModelRoleConfig, providerCfg ProviderConfig, model string) error {
	modelCfg, ok := providerCfg.Models[model]
	if !ok {
		return nil
	}
	valid := make(map[string]struct{})
	for _, effort := range modelCfg.SupportedEfforts {
		if e := strings.TrimSpace(effort); e != "" {
			valid[e] = struct{}{}
		}
	}
	for variantID := range modelCfg.Variants {
		if id := strings.TrimSpace(variantID); id != "" {
			valid[id] = struct{}{}
		}
	}
	if len(valid) == 0 {
		return nil
	}
	for _, field := range []struct {
		name, value string
	}{{"effort", alias.Effort}, {"variant", alias.Variant}} {
		value := strings.TrimSpace(field.value)
		if value == "" {
			continue
		}
		if _, ok := valid[value]; !ok {
			return fmt.Errorf("agent.model_aliases.%s.%s %q is not supported by model %q", aliasName, field.name, value, model)
		}
	}
	return nil
}

func validatePermissionConfig(agent AgentConfig) error {
	if err := validatePermissionMode(agent.PermissionMode); err != nil {
		return err
	}
	return nil
}

func validatePermissionMode(mode string) error {
	switch NormalizePermissionMode(mode) {
	case PermissionModeStandard, PermissionModeReadOnly, PermissionModeUnconfined:
		return nil
	default:
		return nil
	}
}

// Default returns a practical starter config.
func Default() Config {
	return Config{
		DefaultProvider: "openai",
		Providers: map[string]ProviderConfig{
			"openai": {
				Type:      "openai-compatible",
				BaseURL:   "https://api.openai.com/v1",
				APIKeyEnv: "OPENAI_API_KEY",
				Model:     "gpt-4.1",
			},
			"codex": {
				Type:      "codex",
				BaseURL:   "https://api.openai.com/v1",
				APIKeyEnv: "OPENAI_API_KEY",
				Model:     "gpt-5-codex",
			},
			"openai-codex": {
				Type:                  "openai-codex",
				BaseURL:               defaultCodexSubscriptionBaseURL,
				WireAPI:               "responses",
				Model:                 "gpt-5.5",
				ReuseCodexCredentials: true,
			},
			"anthropic": {
				Type:      "anthropic",
				BaseURL:   "https://api.anthropic.com",
				APIKeyEnv: "ANTHROPIC_API_KEY",
				Model:     "claude-3-5-sonnet-latest",
			},
			"openrouter": {
				Type:      "openai-compatible",
				BaseURL:   "https://openrouter.ai/api/v1",
				APIKeyEnv: "OPENROUTER_API_KEY",
				Model:     "openai/gpt-4.1-mini",
				Headers: map[string]string{
					"HTTP-Referer": "https://github.com/blueberrycongee/wuu",
					"X-Title":      "wuu",
				},
			},
		},
		Agent: AgentConfig{
			Name:           DefaultAgentName,
			PermissionMode: PermissionModeStandard,
			MaxParallel:    DefaultAgentMaxParallel,
			// 0 = unlimited; the model decides when to stop. Users who
			// want a runaway safety net can set this explicitly.
			MaxSteps: 0,
		},
	}
}

// DefaultSystemPrompt returns wuu's built-in base behavior prompt for the
// main agent. It combines the universal base sections with the main-only
// orchestration path-selection map. It is not serialized into config files;
// user config is appended separately.
func DefaultSystemPrompt() string {
	return prompts.System() + "\n\n" + prompts.SystemMain()
}

// WorkerSystemPrompt returns the system prompt used to seed spawned
// subagents. It contains only the universal base sections; the main-only
// orchestration map is excluded because orchestration belongs to the brain:
// spawn_agent, helpme, and the subagent management suite (send_message,
// close_agent) are compiled out of worker surfaces entirely
// (internal/modelprofile/compiler.go). Of the
// other tools the map mentions, update_plan stays visible on worker surfaces
// and goal stays deferred behind tool_search — their
// worker-facing guidance lives in the tool descriptions and the worker's
// deferred-tool catalog, not in the orchestration map.
func WorkerSystemPrompt() string {
	return prompts.System()
}

// UserSystemPrompt returns user-controlled prompt additions. The legacy
// system_prompt field is preserved as an append-only customization.
func (a AgentConfig) UserSystemPrompt() string {
	var parts []string
	if s := strings.TrimSpace(a.SystemPrompt); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(a.AppendSystemPrompt); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n\n")
}

func (a AgentConfig) ToolSearchEnabled() bool {
	return a.ToolLoadingPreference() != ToolLoadingFlat
}

func (a AgentConfig) ToolLoadingPreference() ToolLoadingMode {
	if raw := strings.TrimSpace(string(a.ToolLoading)); raw != "" {
		if mode := NormalizeToolLoadingMode(a.ToolLoading); mode != "" {
			if isRetiredToolLoadingMode(a.ToolLoading) {
				warnRetiredToolLoadingOnce(raw, fmt.Sprintf(
					"wuu: agent.tool_loading = %q was removed and now behaves as %q. Wuu's own progressive tool loading rewrote the tools array mid-conversation and invalidated the provider prompt cache. Set agent.tool_loading to auto, flat, or native to silence this notice.",
					raw, ToolLoadingAuto))
			}
			return mode
		}
	}
	if a.ToolSearch != nil {
		if *a.ToolSearch {
			return ToolLoadingAuto
		}
		return ToolLoadingFlat
	}
	return ToolLoadingAuto
}

// isRetiredToolLoadingMode reports whether a configured value names the removed
// Wuu progressive-loading mode. Those spellings still parse so an existing
// config keeps starting; they resolve to auto.
func isRetiredToolLoadingMode(mode ToolLoadingMode) bool {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case "wuu_tool_search", "tool_search":
		return true
	default:
		return false
	}
}

func NormalizeToolLoadingMode(mode ToolLoadingMode) ToolLoadingMode {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case "", string(ToolLoadingAuto):
		return ToolLoadingAuto
	case string(ToolLoadingFlat):
		return ToolLoadingFlat
	case string(ToolLoadingNative):
		return ToolLoadingNative
	case "wuu_tool_search", "tool_search":
		return ToolLoadingAuto
	default:
		return ""
	}
}

func (a AgentConfig) ProfileName() string {
	if name := strings.TrimSpace(a.Name); name != "" {
		return name
	}
	return DefaultAgentName
}

func isCodexSubscriptionProvider(providerType string) bool {
	s := strings.ToLower(strings.TrimSpace(providerType))
	s = strings.ReplaceAll(s, "_", "-")
	switch s {
	case "openai-codex", "codex-subscription", "chatgpt-codex":
		return true
	default:
		return false
	}
}

// TemplateJSON returns a formatted starter config file.
func TemplateJSON() (string, error) {
	cfg := Default()
	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(buf) + "\n", nil
}

// UpdateProviderModel changes the model field for a named provider in
// the config file at configPath and writes it back. It operates on the
// raw JSON to preserve unknown fields and formatting.
func UpdateProviderModel(configPath, providerName, newModel string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	providers, ok := raw["providers"].(map[string]any)
	if !ok {
		return fmt.Errorf("providers section not found")
	}
	provider, ok := providers[providerName].(map[string]any)
	if !ok {
		return fmt.Errorf("provider %q not found", providerName)
	}
	provider["model"] = newModel

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return securefs.WriteFileAtomic(configPath, append(out, '\n'))
}

// UpdateProviderSelection changes the default provider and the selected
// provider's model in the config file at configPath.
func UpdateProviderSelection(configPath, providerName, newModel string) error {
	return updateProviderSelection(configPath, providerName, newModel, nil, nil, nil, nil, nil, nil, nil, false, nil)
}

// UpdateProviderRuntime changes the default provider and editable connection
// fields for that provider. A nil apiKey keeps the existing key configuration.
func UpdateProviderRuntime(configPath, providerName, newModel string, baseURL, apiKey, authToken, effort, variant, permissionMode *string, ultraMode *bool) error {
	return updateProviderSelection(configPath, providerName, newModel, baseURL, apiKey, authToken, effort, variant, permissionMode, ultraMode, false, nil)
}

// CreateProviderRuntime creates a new provider with the requested type
// (e.g. "openai-compatible", "anthropic"), selects it, and persists its
// editable runtime fields. A nil or empty providerType defaults to
// "openai-compatible". The caller is responsible for whitelisting allowed
// type values before invocation; this function writes the type verbatim.
func CreateProviderRuntime(configPath, providerName string, providerType *string, newModel string, baseURL, apiKey, authToken, effort, variant, permissionMode *string, ultraMode *bool) error {
	return updateProviderSelection(configPath, providerName, newModel, baseURL, apiKey, authToken, effort, variant, permissionMode, ultraMode, true, providerType)
}

// UpdateAgentUltraMode atomically persists an Ultra-only runtime update while
// preserving provider selection and every unrelated config field.
func UpdateAgentUltraMode(configPath string, ultraMode *bool) error {
	if ultraMode == nil {
		return nil
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	agent, _ := raw["agent"].(map[string]any)
	if agent == nil {
		agent = make(map[string]any)
		raw["agent"] = agent
	}
	setOptionalBool(agent, "ultra_mode", ultraMode)
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return securefs.WriteFileAtomic(configPath, append(out, '\n'))
}

// RemoveProvider deletes a configured provider from the config file and,
// when the removed provider was the active default, atomically promotes
// fallbackName to default_provider with fallbackModel. fallbackName must
// exist in the providers map after deletion; passing "" skips the swap
// when the removed provider was not the active default.
//
// The function preserves unknown top-level fields and formatting by
// editing the raw JSON map directly, mirroring UpdateProviderModel and
// UpdateAdvancedRuntime. Callers are responsible for refusing the
// removal before this point (last provider, OAuth-locked, provider used
// by a running turn, etc.) — this function only handles the on-disk
// mutation.
func RemoveProvider(configPath, providerName, fallbackName, fallbackModel string) (newDefault string, err error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("read config: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("parse config: %w", err)
	}

	providers, ok := raw["providers"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("providers section not found")
	}
	if _, exists := providers[providerName]; !exists {
		return "", fmt.Errorf("provider %q not found", providerName)
	}

	currentDefault, _ := raw["default_provider"].(string)
	removedWasDefault := currentDefault == providerName

	delete(providers, providerName)

	if removedWasDefault {
		if strings.TrimSpace(fallbackName) == "" {
			// No fallback supplied — pick any remaining provider to keep
			// the runtime consistent. Callers should normally pick a
			// sensible default, but this fallback keeps the config
			// usable even if the caller skips that step.
			for name := range providers {
				fallbackName = name
				break
			}
		}
		if strings.TrimSpace(fallbackName) == "" {
			// No providers left to fall back to; refuse the deletion so
			// the runtime never ends up with an empty default_provider.
			return "", errors.New("cannot remove the last configured provider")
		}
		if _, ok := providers[fallbackName].(map[string]any); !ok {
			return "", fmt.Errorf("fallback provider %q not found", fallbackName)
		}
		raw["default_provider"] = fallbackName
		if strings.TrimSpace(fallbackModel) != "" {
			fb, _ := providers[fallbackName].(map[string]any)
			if fb != nil {
				fb["model"] = strings.TrimSpace(fallbackModel)
			}
		}
		newDefault = fallbackName
	}

	// Clean up any model_role entries that pointed at the removed
	// provider. Empty roles fall back to the main selection per the
	// existing resolver, so dropping the name is the right default
	// rather than erroring.
	if agent, ok := raw["agent"].(map[string]any); ok {
		if roles, ok := agent["model_roles"].(map[string]any); ok {
			for roleKey, raw := range roles {
				role, _ := raw.(map[string]any)
				if role == nil {
					continue
				}
				if name, _ := role["provider"].(string); name == providerName {
					delete(role, "provider")
				}
				_ = roleKey
			}
		}
		// Aliases require an explicit provider and model, so an alias that
		// pointed at the removed provider is no longer valid. Delete the
		// whole entry to keep the config valid.
		if aliases, ok := agent["model_aliases"].(map[string]any); ok {
			for aliasKey, raw := range aliases {
				alias, _ := raw.(map[string]any)
				if alias == nil {
					continue
				}
				if name, _ := alias["provider"].(string); name == providerName {
					delete(aliases, aliasKey)
				}
			}
		}
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}
	if err := securefs.WriteFileAtomic(configPath, append(out, '\n')); err != nil {
		return "", err
	}
	return newDefault, nil
}

// UpdateAdvancedRuntime changes low-level runtime knobs without switching the
// selected provider/model. Zero values mean "auto/default" for numeric knobs.
func UpdateAdvancedRuntime(configPath, providerName string, update AdvancedRuntimeUpdate) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	agent, _ := raw["agent"].(map[string]any)
	if agent == nil {
		agent = make(map[string]any)
		raw["agent"] = agent
	}
	setOptionalInt(agent, "max_steps", update.MaxSteps)
	setOptionalInt(agent, "max_context_tokens", update.MaxContextTokens)
	setOptionalFloat(agent, "temperature", update.Temperature, 0)
	setOptionalFloat(agent, "compact_threshold_pct", update.CompactThresholdPct, 0)
	setOptionalInt(agent, "compact_keep_recent_tokens", update.CompactKeepRecentTokens)
	setOptionalBool(agent, "disable_auto_compact", update.DisableAutoCompact)
	if update.ModelAliases != nil {
		aliases := make(map[string]any, len(update.ModelAliases))
		for rawName, alias := range update.ModelAliases {
			name := normalizeModelAliasName(rawName)
			if name == "" || alias == nil {
				continue
			}
			entry := map[string]any{
				"provider": strings.TrimSpace(alias.Provider),
				"model":    strings.TrimSpace(alias.Model),
			}
			if effort := strings.TrimSpace(alias.Effort); effort != "" {
				entry["effort"] = effort
			}
			if variant := strings.TrimSpace(alias.Variant); variant != "" {
				entry["variant"] = variant
			}
			aliases[name] = entry
		}
		if len(aliases) == 0 {
			delete(agent, "model_aliases")
		} else {
			agent["model_aliases"] = aliases
		}
	}
	if len(agent) == 0 {
		delete(raw, "agent")
	}

	if update.ProviderContextWindow != nil {
		providers, _ := raw["providers"].(map[string]any)
		if providers == nil {
			return errors.New("providers is required")
		}
		name := strings.TrimSpace(providerName)
		if name == "" {
			name, _ = raw["default_provider"].(string)
			name = strings.TrimSpace(name)
		}
		if name == "" {
			return errors.New("provider is required")
		}
		provider, _ := providers[name].(map[string]any)
		if provider == nil {
			return fmt.Errorf("provider %q not found", name)
		}
		setOptionalInt(provider, "context_window", update.ProviderContextWindow)
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(out, &cfg); err != nil {
		return err
	}
	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return err
	}
	return securefs.WriteFileAtomic(configPath, append(out, '\n'))
}

func UpdateGeneralSettings(configPath string, update GeneralSettingsUpdate) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if update.AppendSystemPrompt != nil {
		agent, _ := raw["agent"].(map[string]any)
		if agent == nil {
			agent = make(map[string]any)
			raw["agent"] = agent
		}
		setOptionalString(agent, "append_system_prompt", update.AppendSystemPrompt)
		if len(agent) == 0 {
			delete(raw, "agent")
		}
	}

	if update.GitAttributionEnabled != nil {
		agent, _ := raw["agent"].(map[string]any)
		if agent == nil {
			agent = make(map[string]any)
			raw["agent"] = agent
		}
		if *update.GitAttributionEnabled {
			delete(agent, "git_attribution_enabled")
		} else {
			agent["git_attribution_enabled"] = false
		}
		if len(agent) == 0 {
			delete(raw, "agent")
		}
	}

	if update.MemoryDisable != nil {
		memory, _ := raw["memory"].(map[string]any)
		if memory == nil {
			memory = make(map[string]any)
			raw["memory"] = memory
		}
		if *update.MemoryDisable {
			memory["disable"] = true
		} else {
			delete(memory, "disable")
		}
		if len(memory) == 0 {
			delete(raw, "memory")
		}
	}

	if update.DreamEnabled != nil || update.DreamIntervalDays != nil || update.DreamProvider != nil || update.DreamModel != nil {
		memory, _ := raw["memory"].(map[string]any)
		if memory == nil {
			memory = make(map[string]any)
			raw["memory"] = memory
		}
		dream, _ := memory["dream"].(map[string]any)
		if dream == nil {
			dream = make(map[string]any)
			memory["dream"] = dream
		}
		if update.DreamEnabled != nil {
			dream["enabled"] = *update.DreamEnabled
		}
		if update.DreamIntervalDays != nil {
			if *update.DreamIntervalDays <= 0 {
				delete(dream, "interval_days")
			} else {
				dream["interval_days"] = *update.DreamIntervalDays
			}
		}
		if update.DreamProvider != nil {
			setOptionalString(dream, "provider", update.DreamProvider)
		}
		if update.DreamModel != nil {
			setOptionalString(dream, "model", update.DreamModel)
		}
		// Migrate away from the legacy flat field so the new section is the
		// single source of truth.
		delete(memory, "dream_interval_days")
		if len(dream) == 0 {
			delete(memory, "dream")
		}
		if len(memory) == 0 {
			delete(raw, "memory")
		}
	}

	if len(update.MCPEnabledToggles) > 0 {
		mcpServers, _ := raw["mcp_servers"].(map[string]any)
		if mcpServers == nil {
			mcpServers = make(map[string]any)
			raw["mcp_servers"] = mcpServers
		}
		for name, enabled := range update.MCPEnabledToggles {
			if enabled == nil {
				continue
			}
			server, _ := mcpServers[name].(map[string]any)
			if server == nil {
				server = make(map[string]any)
				mcpServers[name] = server
			}
			if *enabled {
				delete(server, "enabled")
			} else {
				server["enabled"] = false
			}
		}
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(out, &cfg); err != nil {
		return err
	}
	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return err
	}
	return securefs.WriteFileAtomic(configPath, append(out, '\n'))
}

// UpdateExtensionSettings atomically mutates user-owned extension grants and
// package decisions without rewriting unrelated configuration fields. The
// file lock keeps app-server instances from replacing each other's decisions.
func UpdateExtensionSettings(configPath string, update func(*extensions.Settings) error) (extensions.Settings, error) {
	lock, err := storelock.Acquire(filepath.Dir(configPath))
	if err != nil {
		return extensions.Settings{}, err
	}
	defer func() { _ = lock.Release() }()

	data, err := os.ReadFile(configPath)
	if err != nil {
		return extensions.Settings{}, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return extensions.Settings{}, err
	}
	settings := extensions.Settings{}
	if extensionRaw, ok := raw["extensions"]; ok {
		encoded, err := json.Marshal(extensionRaw)
		if err != nil {
			return extensions.Settings{}, fmt.Errorf("marshal current extension settings: %w", err)
		}
		if err := json.Unmarshal(encoded, &settings); err != nil {
			return extensions.Settings{}, fmt.Errorf("decode current extension settings: %w", err)
		}
	}
	if update == nil {
		return extensions.Settings{}, errors.New("extension settings update is required")
	}
	if err := update(&settings); err != nil {
		return extensions.Settings{}, err
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return extensions.Settings{}, fmt.Errorf("marshal extension settings: %w", err)
	}
	var extensionRaw map[string]any
	if err := json.Unmarshal(encoded, &extensionRaw); err != nil {
		return extensions.Settings{}, fmt.Errorf("decode extension settings: %w", err)
	}
	if len(extensionRaw) == 0 {
		delete(raw, "extensions")
	} else {
		raw["extensions"] = extensionRaw
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return extensions.Settings{}, fmt.Errorf("marshal config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(out, &cfg); err != nil {
		return extensions.Settings{}, err
	}
	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return extensions.Settings{}, err
	}
	if err := securefs.WriteFileAtomic(configPath, append(out, '\n')); err != nil {
		return extensions.Settings{}, err
	}
	return settings, nil
}

func setOptionalString(target map[string]any, key string, value *string) {
	if value == nil {
		return
	}
	if *value == "" {
		delete(target, key)
		return
	}
	target[key] = *value
}

func setOptionalInt(target map[string]any, key string, value *int) {
	if value == nil {
		return
	}
	if *value <= 0 {
		delete(target, key)
		return
	}
	target[key] = *value
}

func setOptionalFloat(target map[string]any, key string, value *float64, defaultValue float64) {
	if value == nil {
		return
	}
	if *value <= 0 || *value == defaultValue {
		delete(target, key)
		return
	}
	target[key] = *value
}

func setOptionalBool(target map[string]any, key string, value *bool) {
	if value == nil {
		return
	}
	if !*value {
		delete(target, key)
		return
	}
	target[key] = true
}

func updateProviderSelection(configPath, providerName, newModel string, baseURL, apiKey, authToken, effort, variant, permissionMode *string, ultraMode *bool, createProvider bool, providerType *string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	providers, ok := raw["providers"].(map[string]any)
	if !ok {
		return fmt.Errorf("providers section not found")
	}
	provider, ok := providers[providerName].(map[string]any)
	if createProvider {
		if ok {
			return fmt.Errorf("provider %q already exists", providerName)
		}
		if strings.TrimSpace(providerName) == "" {
			return fmt.Errorf("provider name is required")
		}
		if baseURL == nil || strings.TrimSpace(*baseURL) == "" {
			return fmt.Errorf("base_url is required")
		}
		// Resolve the requested type. Nil or empty defaults to
		// "openai-compatible" to preserve the legacy behavior for
		// callers that have not been updated to send a type yet.
		providerTypeValue := "openai-compatible"
		if providerType != nil {
			if requested := strings.ToLower(strings.TrimSpace(*providerType)); requested != "" {
				providerTypeValue = requested
			}
		}
		provider = map[string]any{
			"type":     providerTypeValue,
			"base_url": strings.TrimSpace(*baseURL),
		}
		if apiKey != nil && strings.TrimSpace(*apiKey) != "" {
			provider["api_key"] = strings.TrimSpace(*apiKey)
		}
		if authToken != nil && strings.TrimSpace(*authToken) != "" {
			provider["auth_token"] = strings.TrimSpace(*authToken)
		}
		providers[providerName] = provider
	} else if !ok {
		return fmt.Errorf("provider %q not found", providerName)
	}
	raw["default_provider"] = providerName
	provider["model"] = newModel
	if baseURL != nil {
		provider["base_url"] = strings.TrimSpace(*baseURL)
	}
	if apiKey != nil {
		if key := strings.TrimSpace(*apiKey); key != "" {
			provider["api_key"] = key
		}
		if strings.TrimSpace(*apiKey) == "" {
			delete(provider, "api_key")
		}
		delete(provider, "api_key_env")
	}
	if authToken != nil {
		if token := strings.TrimSpace(*authToken); token != "" {
			provider["auth_token"] = token
		}
		if strings.TrimSpace(*authToken) == "" {
			delete(provider, "auth_token")
		}
		delete(provider, "auth_token_env")
	}
	if effort != nil {
		agent, _ := raw["agent"].(map[string]any)
		if agent == nil {
			agent = make(map[string]any)
			raw["agent"] = agent
		}
		if strings.TrimSpace(*effort) == "" {
			delete(agent, "effort")
		} else {
			agent["effort"] = strings.TrimSpace(*effort)
		}
	}
	if variant != nil {
		agent, _ := raw["agent"].(map[string]any)
		if agent == nil {
			agent = make(map[string]any)
			raw["agent"] = agent
		}
		if strings.TrimSpace(*variant) == "" {
			delete(agent, "variant")
		} else {
			agent["variant"] = strings.TrimSpace(*variant)
		}
	}
	if permissionMode != nil {
		mode := NormalizePermissionMode(*permissionMode)
		if err := validatePermissionMode(mode); err != nil {
			return err
		}
		agent, _ := raw["agent"].(map[string]any)
		if agent == nil {
			agent = make(map[string]any)
			raw["agent"] = agent
		}
		agent["permission_mode"] = mode
		delete(agent, "permission_profile")
		delete(agent, "approval_policy")
		delete(agent, "approvals_reviewer")
		delete(agent, "tool_policy")
		delete(agent, "permission_rules")
	}
	if ultraMode != nil {
		agent, _ := raw["agent"].(map[string]any)
		if agent == nil {
			agent = make(map[string]any)
			raw["agent"] = agent
		}
		setOptionalBool(agent, "ultra_mode", ultraMode)
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return securefs.WriteFileAtomic(configPath, append(out, '\n'))
}

func applyDefaults(cfg *Config) {
	if strings.TrimSpace(cfg.Agent.Name) == "" {
		cfg.Agent.Name = DefaultAgentName
	}
	if cfg.Agent.MaxParallel == 0 {
		cfg.Agent.MaxParallel = DefaultAgentMaxParallel
	}
	permissions := ResolveAgentPermissions(cfg.Agent)
	cfg.Agent.PermissionMode = permissions.Mode
}
