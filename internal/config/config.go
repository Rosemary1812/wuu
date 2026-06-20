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
)

const (
	localPrimaryConfig   = ".wuu.json"
	localFallbackConfig  = "wuu.json"
	globalConfigRelative = ".config/wuu/config.json"

	DefaultAgentName = "default"

	DefaultMemoryCharLimit     = 2200
	DefaultUserMemoryCharLimit = 1375
	DefaultDreamIntervalDays   = 7
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
	Command       string                     `json:"command,omitempty"`
	Args          []string                   `json:"args,omitempty"`
	URL           string                     `json:"url,omitempty"`
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
	// MCPServers maps server name → connection config. When present, wuu
	// connects to each server at startup (in the background) and exposes
	// its tools to the agent. Aligned with Claude Code's mcpServers config
	// and Codex CLI's mcp.servers field.
	MCPServers map[string]MCPServerConfig `json:"mcp_servers,omitempty"`
}

// MemoryConfig overrides the defaults for memory file discovery
// (CLAUDE.md / AGENTS.md auto-loading). All fields are optional;
// empty values fall back to memory.DefaultOptions().
type MemoryConfig struct {
	// Filenames to look for in priority order. Default:
	// ["AGENTS.md", "AGENTS.override.md", "CLAUDE.md"].
	Filenames []string `json:"filenames,omitempty"`
	// ProjectRootMarkers stop the upward walk through ancestors.
	// Default: [".git", ".hg", ".jj", ".svn"].
	ProjectRootMarkers []string `json:"project_root_markers,omitempty"`
	// UserDirs are scanned for user-level memory. Tilde-expanded.
	// Default: ["~/.config/wuu", "~/.claude", "~/.codex"].
	UserDirs []string `json:"user_dirs,omitempty"`
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
	// StreamConnectTimeoutMS bounds dial/TLS/response-header wait for one
	// streaming connection attempt. It does not cap the whole turn.
	StreamConnectTimeoutMS int `json:"stream_connect_timeout_ms,omitempty"`
	// StreamIdleTimeoutMS bounds silence after the streaming response has
	// started. It does not affect the initial connect stage.
	StreamIdleTimeoutMS int `json:"stream_idle_timeout_ms,omitempty"`
	// ContextWindow optionally overrides wuu's built-in registry for
	// this provider's model. Use it for new models wuu doesn't know
	// about yet, custom finetunes, private deployments, or proxies
	// that rename the upstream model. Zero means "use the registry".
	ContextWindow int `json:"context_window,omitempty"`
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
	Name             string  `json:"name,omitempty"`
	MaxSteps         int     `json:"max_steps"`
	MaxContextTokens int     `json:"max_context_tokens"`
	Temperature      float64 `json:"temperature"`
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

// ToolPolicyConfig configures the runtime policy layer that runs before tool
// execution. Empty means local high-trust mode: allow all tools.
type ToolPolicyConfig struct {
	Profile       string            `json:"profile,omitempty"`
	DefaultAction string            `json:"default_action,omitempty"`
	Tools         map[string]string `json:"tools,omitempty"`
	Kinds         map[string]string `json:"kinds,omitempty"`
	Risks         map[string]string `json:"risks,omitempty"`
}

// Load reads config with priority: .wuu.json, wuu.json, ~/.config/wuu/config.json.
func Load() (Config, string, error) {
	workdir, err := os.Getwd()
	if err != nil {
		return Config{}, "", fmt.Errorf("get cwd: %w", err)
	}
	return LoadFrom(workdir, os.Getenv("HOME"))
}

// LoadFrom reads config from deterministic directories (test-friendly).
func LoadFrom(workdir, home string) (Config, string, error) {
	candidates := []string{
		filepath.Join(workdir, localPrimaryConfig),
		filepath.Join(workdir, localFallbackConfig),
	}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, globalConfigRelative))
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			cfg, readErr := readConfig(candidate)
			if readErr != nil {
				return Config{}, "", readErr
			}
			applyDefaults(&cfg)
			if validateErr := cfg.Validate(); validateErr != nil {
				return Config{}, "", validateErr
			}
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

	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}

	return cfg, nil
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
	}

	if c.Agent.MaxSteps < 0 {
		return errors.New("agent.max_steps cannot be negative (use 0 for unlimited)")
	}
	if c.Agent.Temperature < 0 || c.Agent.Temperature > 2 {
		return errors.New("agent.temperature must be in [0,2]")
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
	if err := validateToolPolicyProfile(policy.Profile); err != nil {
		return err
	}
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

func validateToolPolicyProfile(profile string) error {
	switch strings.TrimSpace(profile) {
	case "", "safe", "balanced", "auto", "autonomous", "enterprise_restricted":
		return nil
	default:
		return fmt.Errorf("agent.tool_policy.profile must be one of safe, balanced, auto, autonomous, enterprise_restricted")
	}
}

func validatePermissionMode(mode string) error {
	switch normalizePermissionMode(mode) {
	case "", PermissionModeReadOnly, PermissionModeDefault, PermissionModeApproveForMe, PermissionModeFullAccess:
		return nil
	default:
		return fmt.Errorf("agent.permission_mode must be one of read_only, default, approve_for_me, full_access")
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
				Type:    "openai-codex",
				BaseURL: "https://chatgpt.com/backend-api/codex",
				WireAPI: "responses",
				Model:   "gpt-5.5",
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
			PermissionMode:    PermissionModeDefault,
			PermissionProfile: PermissionProfileWorkspaceWrite,
			ApprovalPolicy:    ApprovalPolicyOnRequest,
			ApprovalsReviewer: ApprovalsReviewerUser,
			// 0 = unlimited. Aligned with Claude Code, which has no
			// default step cap; the model decides when to stop. Users
			// who want a runaway safety net can set this explicitly.
			MaxSteps:    0,
			Temperature: 0.2,
		},
	}
}

const defaultSystemPrompt = `You are wuu, a pragmatic local coding agent in a GUI-first development environment.

Use Compose as your default way to handle coding work: classify the task, choose the lightest suitable path, and move between direct implementation, read-only planning, skill use, dynamic workflows, sub-agents, and memory as needed. Do not ask the user to pick a work style first; choose it from the task.

At the start of each task, classify the work before choosing a path:
- Fast path: for a simple, specific, low-risk change with clear requirements, inspect the relevant code, make the minimal edit, and verify it. Do not force brainstorms, workflows, or sub-agents onto straightforward work.
- Planning path: when requirements are ambiguous, the design space is open, or the change affects architecture, security, data safety, or product behavior, create a short plan and ask the user only for choices that are irreversible or materially affect scope.
- Skill path: when an available skill clearly matches the task or the user invokes one, load it with load_skill, follow its instructions, and keep the work scoped to the user's request.
- Workflow path: when the task is repeatable, scheduled, long-running, multi-phase, or benefits from durable run state, use load_workflow and start_workflow instead of hand-managing the whole process in chat.
- Delegation path: when independent investigation, parallel implementation, risky verification, or separate context would materially improve the result, use spawn_agent with a focused prompt.
- Memory path: when you learn durable project facts, recurring workflow lessons, or session state that should survive context pruning, use session_memory with the narrowest appropriate target. Use summary for recoverable session state, notes for scratch details, and project_memory only for stable workspace facts.

Use tools to make real changes on the user's system. Do not just describe solutions in text when the user asked you to inspect, change, test, or verify something.

Make minimal changes to achieve the goal. Follow the existing coding style of the project. Test what you build and verify what you change. Always explain what changed or what decision you made.

If multiple tool calls are independent, make them in parallel.

For manual code edits, use the editing tool exposed in this session. If apply_patch is available, use it for hand-written file changes. If apply_patch is not available, use edit_file for targeted modifications and write_file only for new files or full rewrites. Do not edit files through heredocs, redirected command output, or file-printing commands when a dedicated edit tool fits the job.
Use command execution only when the active tool surface exposes that capability. If it is not exposed, report that command execution and command-based verification are unavailable under the current profile instead of inventing another path. Profile-specific command instructions live in the tool_surface section.

Before a final response after code or workflow changes, inspect the final diff or durable run state and report a compact verification ledger: what changed, which validation commands or workflow reports passed, and any unverified scope with the reason. If no validation was run, say so explicitly instead of implying success.

For multi-step work, maintain a visible checklist with update_plan. Create or update the plan before substantive edits, keep exactly one item in_progress until all plan items are completed, update it after meaningful milestones, and mark every item completed before the final response. Do not use update_plan for trivial one-step tasks.

When a task has explicit constraints, acceptance criteria, non-goals, or risky assumptions, maintain them in update_plan's constraint ledger fields. Keep constraints concise and active, and set the pre_write_check / pre_finish_check fields in update_plan's constraint ledger before mutating files and before claiming completion. Treat the injected [CONSTRAINT_LEDGER] context block as the current source of truth for these checks.

# Long-lived processes and dev servers

When a command may keep running and the active tool surface exposes command execution, use the exposed tool only when you need bounded logs, readiness output, or validation evidence, and give it an explicit timeout when appropriate. If command execution is unavailable, say that plainly instead of inventing another path.

When a managed process opens a localhost port the user would want to see, call report_listening_ports with the port numbers once it is ready. The desktop uses the first port to auto-open the in-app browser preview, and shows the full list as clickable chips in the workspace sidebar. Skip this for short-lived one-shot commands and for ports that are not intended for browser preview.

Do not claim a dev server is still running after your reply unless a tool result explicitly says a managed process remains active. Stop temporary commands when they are no longer needed or when the user asks you to stop them. If the active surface cannot keep a process alive after the turn, say that plainly.

# Sub-agents

You may use spawn_agent to spawn sub-agents only when delegation materially improves the task: independent investigation, parallel implementation slices, risky verification, or work that benefits from a separate context. Keep work local when the next step is tightly coupled, on the critical path, or simpler to do directly.

Before spawning, make these choices explicitly:
- Agent shape: specify subagent_type='general-purpose' or subagent_type='verification' for a fresh specialized agent. Omit subagent_type only when you intentionally want to fork yourself with full conversation context.
- Workspace: omit isolation so the child works in the current repo. Use isolation='worktree' only for destructive or broad experiments, overlapping or uncertain concurrent writes, generated outputs/formatters that may touch many files, or when the user explicitly asks for isolation.
- Waiting: fresh specialized agents run in the foreground by default. Use run_in_background=true only when you have genuinely independent work to do in parallel. Forks and verification agents run in the background. Do not call wait_agent after spawn_agent just to poll. Call wait_agent only when the current turn is blocked on a background completion signal; use await_agents when you need child output for synthesis.

Background completion notifications are internal agent handoffs, not new user requests. Read their content as worker output and do your own synthesis before acting on it.

For spawn_agent, always provide description and prompt. Use name only when you need a stable addressable task name; otherwise wuu derives one. The prompt must follow the Base Agent Brief Contract: task, background, role, identity/memory, scope, non-goals, starting points, acceptance criteria, deliverables, reporting, and constraints. For code-edit work, name the files or modules the child owns and any nearby files or modules it should avoid; split parallel edits so ownership does not overlap. Address child tasks later with agent_id, agent_path, or task_name.

Treat commands as non-interactive when the active tool surface exposes command execution. If command execution is not exposed, say that command execution is unavailable under the current profile.

# Assistant message phases

Use progress commentary only while work is still underway: brief status, what you are about to do, what changed direction, or what evidence you just found. Keep it useful and concise. Do not put final conclusions, complete answers, verification ledgers, or handoff summaries in progress commentary.

Use the final response only when the turn is complete or genuinely blocked. The final response should answer the user's request and, when work was performed, report the user-visible change, validation performed, and any unverified scope. Do not write visible labels such as "commentary" or "final_answer"; those are runtime metadata, not user-facing text.

# Communicating with the user

All text you output outside tool calls is displayed to the user. Use it to keep the user oriented, not to narrate every routine step.

Before your first tool call, give one short sentence so the user knows what you are about to do. While working, send short updates at meaningful moments: when you find a bug or root cause, when you change direction, before editing files, or when you have made progress without an update. Keep text between tool calls concise and useful. No fluff.

# Code comments

Think in three comment buckets: 'what', 'why', and future-intent/status comments. Do not write 'what' comments that merely restate the code. Write 'why' comments only when they preserve a non-obvious rationale or tradeoff, and keep them sparse, factual, and up to the standard of top-tier open-source projects. Do not leave future-intent/status comments such as 'I will do it later' or other speculative notes. Treat every comment as long-lived documentation that future agents will read, so avoid anything misleading or not true at the time it is written.`

// DefaultSystemPrompt returns wuu's built-in base behavior prompt. It is not
// serialized into config files; user config is appended separately.
func DefaultSystemPrompt() string {
	return defaultSystemPrompt
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

func (a AgentConfig) ProfileName() string {
	if name := strings.TrimSpace(a.Name); name != "" {
		return name
	}
	return DefaultAgentName
}

func (a AgentConfig) ProfileMemoryEnabled() bool {
	return a.ProfileName() != DefaultAgentName
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
	return updateProviderSelection(configPath, providerName, newModel, nil, nil, nil, nil, nil, nil, false)
}

// UpdateProviderSelectionAndEffort changes the default provider, selected
// provider's model, and global reasoning effort in the config file at configPath.
func UpdateProviderSelectionAndEffort(configPath, providerName, newModel, effort string) error {
	return updateProviderSelection(configPath, providerName, newModel, nil, nil, &effort, nil, nil, nil, false)
}

// UpdateProviderRuntime changes the default provider and editable connection
// fields for that provider. A nil apiKey keeps the existing key configuration.
func UpdateProviderRuntime(configPath, providerName, newModel string, baseURL, apiKey, effort, variant, toolPolicyProfile, permissionMode *string) error {
	return updateProviderSelection(configPath, providerName, newModel, baseURL, apiKey, effort, variant, toolPolicyProfile, permissionMode, false)
}

// CreateProviderRuntime creates a new OpenAI-compatible provider, selects it,
// and persists its editable runtime fields.
func CreateProviderRuntime(configPath, providerName, newModel string, baseURL, apiKey, effort, variant, toolPolicyProfile, permissionMode *string) error {
	return updateProviderSelection(configPath, providerName, newModel, baseURL, apiKey, effort, variant, toolPolicyProfile, permissionMode, true)
}

func updateProviderSelection(configPath, providerName, newModel string, baseURL, apiKey, effort, variant, toolPolicyProfile, permissionMode *string, createProvider bool) error {
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
		provider = map[string]any{
			"type":     "openai-compatible",
			"base_url": strings.TrimSpace(*baseURL),
		}
		if apiKey != nil && strings.TrimSpace(*apiKey) != "" {
			provider["api_key"] = strings.TrimSpace(*apiKey)
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
		permissions, _ := PermissionPresetForMode(mode)
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
	} else if toolPolicyProfile != nil {
		profile := strings.TrimSpace(*toolPolicyProfile)
		if err := validateToolPolicyProfile(profile); err != nil {
			return err
		}
		agent, _ := raw["agent"].(map[string]any)
		if agent == nil {
			agent = make(map[string]any)
			raw["agent"] = agent
		}
		if profile == "" {
			delete(agent, "tool_policy")
		} else {
			agent["tool_policy"] = map[string]any{
				"profile": profile,
			}
		}
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
	// max_steps = 0 means unlimited (no step cap, the model decides
	// when to stop). Aligned with Claude Code's default behavior.
	// Users who set an explicit positive value get a hard cap.
	if cfg.Agent.Temperature == 0 {
		cfg.Agent.Temperature = 0.2
	}
}
