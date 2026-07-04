package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/statepath"
	"github.com/blueberrycongee/wuu/prompts"
)

const (
	localPrimaryConfig  = ".wuu.json"
	localFallbackConfig = "wuu.json"

	DefaultAgentName = "default"

	DefaultMemoryCharLimit     = 2200
	DefaultUserMemoryCharLimit = 1375
	DefaultDreamIntervalDays   = 7

	defaultCodexSubscriptionBaseURL = "https://chatgpt.com/backend-api/codex"
)

type ToolLoadingMode string

const (
	ToolLoadingAuto          ToolLoadingMode = "auto"
	ToolLoadingFlat          ToolLoadingMode = "flat"
	ToolLoadingNative        ToolLoadingMode = "native"
	ToolLoadingWuuToolSearch ToolLoadingMode = "wuu_tool_search"
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
	// Disable turns off memory loading entirely.
	Disable bool `json:"disable,omitempty"`
	// NudgeInterval controls how many successful user turns pass before
	// the background memory reviewer checks whether durable facts should be
	// saved. nil means the default interval; 0 disables the reviewer.
	NudgeInterval *int `json:"nudge_interval,omitempty"`
	// MemoryCharLimit caps target="memory" persistent entries by character
	// count. Zero uses DefaultMemoryCharLimit.
	MemoryCharLimit int `json:"memory_char_limit,omitempty"`
	// UserCharLimit caps target="user" persistent entries by character
	// count. Zero uses DefaultUserMemoryCharLimit.
	UserCharLimit int `json:"user_char_limit,omitempty"`
	// DreamIntervalDays controls how often the background dream pass
	// consolidates recent session history into workspace memory. nil means
	// the default interval; 0 disables the dream pass.
	DreamIntervalDays *int `json:"dream_interval_days,omitempty"`
}

// ProfileMemoryNudgeInterval returns the configured background review
// cadence. The default mirrors the memory-review cadence used by comparable
// agent runtimes.
func (m MemoryConfig) ProfileMemoryNudgeInterval() int {
	if m.NudgeInterval == nil {
		return 10
	}
	return *m.NudgeInterval
}

func (m MemoryConfig) ProfileMemoryCharLimit() int {
	if m.MemoryCharLimit <= 0 {
		return DefaultMemoryCharLimit
	}
	return m.MemoryCharLimit
}

func (m MemoryConfig) ProfileUserCharLimit() int {
	if m.UserCharLimit <= 0 {
		return DefaultUserMemoryCharLimit
	}
	return m.UserCharLimit
}

func (m MemoryConfig) DreamIntervalDaysValue() int {
	if m.DreamIntervalDays == nil {
		return DefaultDreamIntervalDays
	}
	return *m.DreamIntervalDays
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
	// StreamConnectTimeoutMS bounds dial/TLS/response-header wait for one
	// streaming connection attempt. It does not cap the whole turn.
	StreamConnectTimeoutMS int `json:"stream_connect_timeout_ms,omitempty"`
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
	// Name identifies a durable agent profile. The default profile is a
	// temporary session; non-default names opt into profile-scoped memory shared
	// across workspaces.
	Name             string `json:"name,omitempty"`
	MaxSteps         int    `json:"max_steps"`
	MaxContextTokens int    `json:"max_context_tokens"`
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
	AppendSystemPrompt string           `json:"append_system_prompt,omitempty"`
	ToolPolicy         ToolPolicyConfig `json:"tool_policy,omitempty"`
	// PermissionRules are granular OpenCode-style permission rules:
	// permission -> pattern -> allow/deny/ask. They refine the broad permission
	// mode without changing the hard permission boundary.
	PermissionRules PermissionRulesConfig `json:"permission_rules,omitempty"`
	// PermissionMode is the user-facing Codex-style permission preset.
	// Empty resolves to the Default preset.
	PermissionMode string `json:"permission_mode,omitempty"`
	// PermissionProfile, ApprovalPolicy, and ApprovalsReviewer are the
	// normalized runtime fields behind PermissionMode. Advanced configs can set
	// them directly; preset selection rewrites them together.
	PermissionProfile string `json:"permission_profile,omitempty"`
	ApprovalPolicy    string `json:"approval_policy,omitempty"`
	ApprovalsReviewer string `json:"approvals_reviewer,omitempty"`
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
	// deferred-loading protocol; compatible endpoints use a flat tool list.
	// Valid: auto, flat, native, wuu_tool_search.
	ToolLoading ToolLoadingMode `json:"tool_loading,omitempty"`
	// ToolSearch is a legacy alias kept for older config files. New configs
	// should use tool_loading.
	ToolSearch *bool `json:"tool_search,omitempty"`
	// ExperimentalDeferredToolBundles enables the provider-native bundle
	// activation path where successful tools can attach follow-on schemas.
	// It is off by default; the main path is tool_search or flat tools.
	ExperimentalDeferredToolBundles bool `json:"experimental_deferred_tool_bundles,omitempty"`
	// ExperimentalCoordinatorMode exposes the old coordinator slash mode
	// for local experimentation. Disabled by default because the mode's
	// user-facing contract is still unclear: the main agent loses some
	// direct write tools but not every mutating capability.
	ExperimentalCoordinatorMode bool `json:"experimental_coordinator_mode,omitempty"`
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

// ToolPolicyConfig configures explicit capability-policy overrides. The broad
// runtime profile comes from PermissionMode instead of a second preset system.
type ToolPolicyConfig struct {
	DefaultAction string            `json:"default_action,omitempty"`
	Tools         map[string]string `json:"tools,omitempty"`
	Kinds         map[string]string `json:"kinds,omitempty"`
	Risks         map[string]string `json:"risks,omitempty"`
}

type AdvancedRuntimeUpdate struct {
	MaxSteps                *int
	MaxContextTokens        *int
	Temperature             *float64
	CompactThresholdPct     *float64
	CompactKeepRecentTokens *int
	DisableAutoCompact      *bool
	ProviderContextWindow   *int
}

type GeneralSettingsUpdate struct {
	AppendSystemPrompt *string
	MemoryDisable      *bool
	MCPEnabledToggles  map[string]*bool // server name → enabled; nil = skip
}

// Load reads config with priority: .wuu.json, wuu.json, then the unified user
// config (~/.wuu/config.json, or WUU_HOME/config.json), then the legacy
// ~/.config/wuu/config.json for backward compatibility.
func Load() (Config, string, error) {
	workdir, err := os.Getwd()
	if err != nil {
		return Config{}, "", fmt.Errorf("get cwd: %w", err)
	}
	return LoadFrom(workdir, os.Getenv("HOME"))
}

// LoadFrom reads config from deterministic directories (test-friendly). The
// global lookup order is the unified user config first (~/.wuu/config.json, or
// WUU_HOME/config.json) then the legacy ~/.config/wuu/config.json. Before the
// global read it performs a one-time, non-destructive migration of any legacy
// config.json/auth.json into the unified home. Passing an empty home (for
// example via --ignore-user-config) skips the global lookup and migration
// entirely.
func LoadFrom(workdir, home string) (Config, string, error) {
	candidates := []string{
		filepath.Join(workdir, localPrimaryConfig),
		filepath.Join(workdir, localFallbackConfig),
	}
	if home != "" {
		migrateLegacyGlobalStore(home)
		if newPath, err := statepath.ConfigPath(home); err == nil && newPath != "" {
			candidates = append(candidates, newPath)
		}
		if legacyPath := statepath.LegacyConfigPath(home); legacyPath != "" {
			candidates = append(candidates, legacyPath)
		}
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			cfg, readErr := readConfig(candidate)
			if readErr != nil {
				return Config{}, "", readErr
			}
			// Overlay the project-scoped settings layers (if any) on top of
			// the selected base config. projectRoot is workdir, the same
			// directory used to look up the project config above. When no
			// layer files exist this is a no-op and cfg is returned unchanged,
			// preserving the pre-layering pick-first behavior exactly.
			layered, appliedLayers, layerErr := applyProjectSettingsLayers(candidate, workdir, cfg)
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
	}

	return Config{}, "", fmt.Errorf("%w, run `wuu init` to create %s", ErrConfigNotFound, localPrimaryConfig)
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
	dec := json.NewDecoder(bytes.NewReader(data))
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
		return errors.New("agent.tool_loading must be one of auto, flat, native, or wuu_tool_search")
	}
	if c.Memory.DreamIntervalDays != nil && *c.Memory.DreamIntervalDays < 0 {
		return errors.New("memory.dream_interval_days cannot be negative")
	}
	if err := validateToolPolicyConfig(c.Agent.ToolPolicy); err != nil {
		return err
	}
	if err := validatePermissionConfig(c.Agent); err != nil {
		return err
	}
	if err := validateModelRolesConfig(c); err != nil {
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

func validatePermissionConfig(agent AgentConfig) error {
	if err := validatePermissionMode(agent.PermissionMode); err != nil {
		return err
	}
	if err := validatePermissionProfile(agent.PermissionProfile); err != nil {
		return err
	}
	if err := validateApprovalPolicy(agent.ApprovalPolicy); err != nil {
		return err
	}
	if err := validateApprovalsReviewer(agent.ApprovalsReviewer); err != nil {
		return err
	}
	if err := validatePermissionRulesConfig(agent.PermissionRules); err != nil {
		return err
	}
	return nil
}

func validateToolPolicyConfig(policy ToolPolicyConfig) error {
	if err := validateToolPolicyAction("agent.tool_policy.default_action", policy.DefaultAction); err != nil {
		return err
	}
	for name, action := range policy.Tools {
		if strings.TrimSpace(name) == "" {
			return errors.New("agent.tool_policy.tools contains an empty tool name")
		}
		if err := validateToolPolicyAction(fmt.Sprintf("agent.tool_policy.tools.%s", name), action); err != nil {
			return err
		}
	}
	for kind, action := range policy.Kinds {
		if strings.TrimSpace(kind) == "" {
			return errors.New("agent.tool_policy.kinds contains an empty kind")
		}
		if err := validateToolPolicyAction(fmt.Sprintf("agent.tool_policy.kinds.%s", kind), action); err != nil {
			return err
		}
	}
	for risk, action := range policy.Risks {
		risk = strings.TrimSpace(risk)
		if risk == "" {
			return errors.New("agent.tool_policy.risks contains an empty risk")
		}
		if err := validateToolPolicyRisk(risk); err != nil {
			return err
		}
		if err := validateToolPolicyAction(fmt.Sprintf("agent.tool_policy.risks.%s", risk), action); err != nil {
			return err
		}
	}
	return nil
}

func validatePermissionMode(mode string) error {
	switch normalizePermissionMode(mode) {
	case "", PermissionModeReadOnly, PermissionModeAgent, PermissionModeAutoReview, PermissionModeFullAccess:
		return nil
	default:
		return fmt.Errorf("agent.permission_mode must be one of read_only, agent, auto_review, full_access")
	}
}

func validatePermissionProfile(profile string) error {
	switch normalizePermissionProfile(profile) {
	case "", PermissionProfileReadOnly, PermissionProfileWorkspaceWrite, PermissionProfileDangerFullAccess:
		return nil
	default:
		return fmt.Errorf("agent.permission_profile must be one of read_only, workspace_write, danger_full_access")
	}
}

func validateApprovalPolicy(policy string) error {
	switch normalizeApprovalPolicy(policy) {
	case "", ApprovalPolicyOnRequest, ApprovalPolicyNever:
		return nil
	default:
		return fmt.Errorf("agent.approval_policy must be one of on_request, never")
	}
}

func validateApprovalsReviewer(reviewer string) error {
	switch normalizeApprovalsReviewer(reviewer) {
	case "", ApprovalsReviewerUser, ApprovalsReviewerAutoReview:
		return nil
	default:
		return fmt.Errorf("agent.approvals_reviewer must be one of user, auto_review")
	}
}

func validateToolPolicyAction(path, action string) error {
	switch strings.TrimSpace(action) {
	case "", "allow", "deny", "require_approval", "auto_classify":
		return nil
	default:
		return fmt.Errorf("%s must be one of allow, deny, require_approval, auto_classify", path)
	}
}

func validateToolPolicyRisk(risk string) error {
	switch risk {
	case "low", "medium", "high":
		return nil
	default:
		return fmt.Errorf("agent.tool_policy.risks.%s is not a known risk", risk)
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
			Name:              DefaultAgentName,
			PermissionMode:    PermissionModeAgent,
			PermissionProfile: PermissionProfileWorkspaceWrite,
			ApprovalPolicy:    ApprovalPolicyOnRequest,
			ApprovalsReviewer: ApprovalsReviewerUser,
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
// followup_task, await_agents, close_agent, list_agents) are compiled out
// of worker surfaces entirely (internal/modelprofile/compiler.go). Of the
// other tools the map mentions, update_plan and inception stay visible on
// worker surfaces and create_goal/start_workflow stay deferred behind
// tool_search — their worker-facing guidance lives in the tool descriptions
// and the worker's deferred-tool catalog, not in the orchestration map.
// read_memory/write_memory are retired on every surface (memory-redesign
// §6: durable memory is a file directory written with plain file tools).
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
	if strings.TrimSpace(string(a.ToolLoading)) != "" {
		if mode := NormalizeToolLoadingMode(a.ToolLoading); mode != "" {
			return mode
		}
	}
	if a.ToolSearch != nil {
		if *a.ToolSearch {
			return ToolLoadingWuuToolSearch
		}
		return ToolLoadingFlat
	}
	return ToolLoadingAuto
}

func NormalizeToolLoadingMode(mode ToolLoadingMode) ToolLoadingMode {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case "", string(ToolLoadingAuto):
		return ToolLoadingAuto
	case string(ToolLoadingFlat):
		return ToolLoadingFlat
	case string(ToolLoadingNative):
		return ToolLoadingNative
	case string(ToolLoadingWuuToolSearch), "tool_search":
		return ToolLoadingWuuToolSearch
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

// ProfileMemoryEnabled reports whether the durable long-term memory store is
// attached to a session. Memory is now a single global store under
// statepath.GlobalMemoryDir(wuuHome) and is enabled for every session by
// default, matching the Claude Code convention of one durable memory
// document per user regardless of the agent profile name. The flag is kept
// for downstream code that still wants to gate memory injection on a
// feature toggle; the function returns true unconditionally until a
// future config knob is introduced.
func (a AgentConfig) ProfileMemoryEnabled() bool {
	return true
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
	return os.WriteFile(configPath, append(out, '\n'), 0644)
}

// UpdateProviderSelection changes the default provider and the selected
// provider's model in the config file at configPath.
func UpdateProviderSelection(configPath, providerName, newModel string) error {
	return updateProviderSelection(configPath, providerName, newModel, nil, nil, nil, nil, nil, nil, false, nil)
}

// UpdateProviderSelectionAndEffort changes the default provider, selected
// provider's model, and global reasoning effort in the config file at configPath.
func UpdateProviderSelectionAndEffort(configPath, providerName, newModel, effort string) error {
	return updateProviderSelection(configPath, providerName, newModel, nil, nil, nil, &effort, nil, nil, false, nil)
}

// UpdateProviderRuntime changes the default provider and editable connection
// fields for that provider. A nil apiKey keeps the existing key configuration.
func UpdateProviderRuntime(configPath, providerName, newModel string, baseURL, apiKey, authToken, effort, variant, permissionMode *string) error {
	return updateProviderSelection(configPath, providerName, newModel, baseURL, apiKey, authToken, effort, variant, permissionMode, false, nil)
}

// CreateProviderRuntime creates a new provider with the requested type
// (e.g. "openai-compatible", "anthropic"), selects it, and persists its
// editable runtime fields. A nil or empty providerType defaults to
// "openai-compatible". The caller is responsible for whitelisting allowed
// type values before invocation; this function writes the type verbatim.
func CreateProviderRuntime(configPath, providerName string, providerType *string, newModel string, baseURL, apiKey, authToken, effort, variant, permissionMode *string) error {
	return updateProviderSelection(configPath, providerName, newModel, baseURL, apiKey, authToken, effort, variant, permissionMode, true, providerType)
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
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(configPath, append(out, '\n'), 0644); err != nil {
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
	return os.WriteFile(configPath, append(out, '\n'), 0644)
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
	return os.WriteFile(configPath, append(out, '\n'), 0644)
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

func updateProviderSelection(configPath, providerName, newModel string, baseURL, apiKey, authToken, effort, variant, permissionMode *string, createProvider bool, providerType *string) error {
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
		mode := normalizePermissionMode(*permissionMode)
		if err := validatePermissionMode(mode); err != nil {
			return err
		}
		permissions, err := ResolvePermissionModePreset(mode)
		if err != nil {
			return err
		}
		agent, _ := raw["agent"].(map[string]any)
		if agent == nil {
			agent = make(map[string]any)
			raw["agent"] = agent
		}
		agent["permission_mode"] = permissions.Mode
		agent["permission_profile"] = permissions.PermissionProfile
		agent["approval_policy"] = permissions.ApprovalPolicy
		agent["approvals_reviewer"] = permissions.ApprovalsReviewer
		delete(agent, "tool_policy")
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(configPath, append(out, '\n'), 0644)
}

func applyDefaults(cfg *Config) {
	if strings.TrimSpace(cfg.Agent.Name) == "" {
		cfg.Agent.Name = DefaultAgentName
	}
	permissions := ResolveAgentPermissions(cfg.Agent)
	if strings.TrimSpace(cfg.Agent.PermissionMode) == "" {
		cfg.Agent.PermissionMode = permissions.Mode
	}
	if strings.TrimSpace(cfg.Agent.PermissionProfile) == "" {
		cfg.Agent.PermissionProfile = permissions.PermissionProfile
	}
	if strings.TrimSpace(cfg.Agent.ApprovalPolicy) == "" {
		cfg.Agent.ApprovalPolicy = permissions.ApprovalPolicy
	}
	if strings.TrimSpace(cfg.Agent.ApprovalsReviewer) == "" {
		cfg.Agent.ApprovalsReviewer = permissions.ApprovalsReviewer
	}
}
