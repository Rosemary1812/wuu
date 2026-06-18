package modelroles

import (
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/modelcatalog"
	"github.com/blueberrycongee/wuu/internal/modelprofile"
	"github.com/blueberrycongee/wuu/internal/modelvariant"
)

type Role string

const (
	RoleMain     Role = "main"
	RoleReview   Role = "review"
	RoleCompact  Role = "compact"
	RoleTitle    Role = "title"
	RoleMemory   Role = "memory"
	RoleWorker   Role = "worker"
	RoleFallback Role = "fallback"
)

var orderedRoles = []Role{
	RoleMain,
	RoleReview,
	RoleCompact,
	RoleTitle,
	RoleMemory,
	RoleWorker,
	RoleFallback,
}

type ResolveOptions struct {
	ProviderName   string
	ProviderConfig config.ProviderConfig
	Model          string
	Effort         string
	Variant        string
}

type Set struct {
	Main     Selection
	Review   Selection
	Compact  Selection
	Title    Selection
	Memory   Selection
	Worker   Selection
	Fallback Selection
}

type Selection struct {
	Role               Role
	Provider           string
	Model              string
	APIModel           string
	Effort             string
	LegacyEffort       string
	Variant            string
	ProviderOptions    map[string]any
	Inherited          bool
	ProviderConfig     config.ProviderConfig
	RuleProvider       string
	RuleProviderConfig config.ProviderConfig
	Capabilities       Capabilities
	Behavior           Behavior
}

type Capabilities struct {
	Chat                     bool     `json:"chat"`
	Responses                bool     `json:"responses,omitempty"`
	Tools                    bool     `json:"tools"`
	ToolCalling              string   `json:"tool_calling,omitempty"`
	StructuredOutput         bool     `json:"structured_output"`
	Streaming                bool     `json:"streaming"`
	StreamingToolArgs        bool     `json:"streaming_tool_args,omitempty"`
	FreeformTool             bool     `json:"freeform_tool,omitempty"`
	ParallelToolCalls        bool     `json:"parallel_tool_calls,omitempty"`
	SystemRole               bool     `json:"system_role"`
	DeveloperRole            bool     `json:"developer_role,omitempty"`
	Reasoning                bool     `json:"reasoning"`
	ContextWindow            int      `json:"context_window,omitempty"`
	InputLimit               int      `json:"input_limit,omitempty"`
	OutputLimit              int      `json:"output_limit,omitempty"`
	ImageInput               bool     `json:"image_input,omitempty"`
	FileInput                bool     `json:"file_input,omitempty"`
	PromptCache              bool     `json:"prompt_cache,omitempty"`
	CacheGranularity         string   `json:"cache_granularity,omitempty"`
	ProtocolFamily           string   `json:"protocol_family,omitempty"`
	RetrySafeErrorCategories []string `json:"retry_safe_error_categories,omitempty"`
}

type Behavior struct {
	Family                    string      `json:"family,omitempty"`
	DefaultWriteMode          string      `json:"default_write_mode,omitempty"`
	PreferredEditPrimitive    string      `json:"preferred_edit_primitive,omitempty"`
	PreferredPatchGrammar     string      `json:"preferred_patch_grammar,omitempty"`
	PatchReliability          int         `json:"patch_reliability,omitempty"`
	ExactEditReliability      int         `json:"exact_edit_reliability,omitempty"`
	WholeFileReliability      int         `json:"whole_file_reliability,omitempty"`
	JSONReliability           int         `json:"json_reliability,omitempty"`
	LongHorizonScore          int         `json:"long_horizon_score,omitempty"`
	DefaultMaxAutonomousSteps int         `json:"default_max_autonomous_steps,omitempty"`
	DefaultSearchBudget       int         `json:"default_search_budget,omitempty"`
	NeedsReadBeforeWrite      bool        `json:"needs_read_before_write,omitempty"`
	AllowParallelReadOnly     bool        `json:"allow_parallel_read_only,omitempty"`
	AllowDirectShell          bool        `json:"allow_direct_shell,omitempty"`
	Latency                   string      `json:"latency,omitempty"`
	Suitable                  Suitability `json:"suitable,omitempty"`
}

type Suitability struct {
	Main     bool `json:"main,omitempty"`
	Review   bool `json:"review,omitempty"`
	Compact  bool `json:"compact,omitempty"`
	Title    bool `json:"title,omitempty"`
	Memory   bool `json:"memory,omitempty"`
	Worker   bool `json:"worker,omitempty"`
	Fallback bool `json:"fallback,omitempty"`
}

func OrderedRoles() []Role {
	return append([]Role(nil), orderedRoles...)
}

func (s Set) Empty() bool {
	return strings.TrimSpace(s.Main.Provider) == "" && strings.TrimSpace(s.Main.Model) == ""
}

func (s Set) ForRole(role Role) Selection {
	switch role {
	case RoleMain:
		return s.Main
	case RoleReview:
		return s.Review
	case RoleCompact:
		return s.Compact
	case RoleTitle:
		return s.Title
	case RoleMemory:
		return s.Memory
	case RoleWorker:
		return s.Worker
	case RoleFallback:
		return s.Fallback
	default:
		return Selection{}
	}
}

func (s Set) List() []Selection {
	if s.Empty() {
		return nil
	}
	out := make([]Selection, 0, len(orderedRoles))
	for _, role := range orderedRoles {
		if selection := s.ForRole(role); strings.TrimSpace(selection.Provider) != "" || strings.TrimSpace(selection.Model) != "" {
			out = append(out, selection)
		}
	}
	return out
}

func Resolve(cfg config.Config, opts ResolveOptions) (Set, error) {
	mainProvider := strings.TrimSpace(opts.ProviderName)
	mainProviderCfg := opts.ProviderConfig
	var err error
	if mainProvider == "" || strings.TrimSpace(mainProviderCfg.Type) == "" {
		mainProviderCfg, mainProvider, err = cfg.ResolveProvider(mainProvider)
		if err != nil {
			return Set{}, err
		}
	}
	mainModel := strings.TrimSpace(opts.Model)
	if mainModel == "" {
		mainModel = strings.TrimSpace(mainProviderCfg.Model)
	}
	if mainModel == "" {
		return Set{}, fmt.Errorf("main model is required")
	}
	mainProviderCfg.Model = mainModel

	main := buildSelection(RoleMain, mainProvider, mainProviderCfg, mainModel, opts.Variant, opts.Effort, false)
	set := Set{Main: main}
	var resolveErr error
	set.Review, resolveErr = resolveConfiguredRole(cfg, RoleReview, cfg.Agent.ModelRoles.Review, main)
	if resolveErr != nil {
		return Set{}, resolveErr
	}
	set.Compact, resolveErr = resolveConfiguredRole(cfg, RoleCompact, cfg.Agent.ModelRoles.Compact, main)
	if resolveErr != nil {
		return Set{}, resolveErr
	}
	set.Title, resolveErr = resolveConfiguredRole(cfg, RoleTitle, cfg.Agent.ModelRoles.Title, main)
	if resolveErr != nil {
		return Set{}, resolveErr
	}
	set.Memory, resolveErr = resolveConfiguredRole(cfg, RoleMemory, cfg.Agent.ModelRoles.Memory, main)
	if resolveErr != nil {
		return Set{}, resolveErr
	}
	set.Worker, resolveErr = resolveConfiguredRole(cfg, RoleWorker, cfg.Agent.ModelRoles.Worker, main)
	if resolveErr != nil {
		return Set{}, resolveErr
	}
	set.Fallback, resolveErr = resolveConfiguredRole(cfg, RoleFallback, cfg.Agent.ModelRoles.Fallback, main)
	if resolveErr != nil {
		return Set{}, resolveErr
	}
	return set, nil
}

func BuildFacts(providerName string, provider config.ProviderConfig, model string) (Capabilities, Behavior) {
	ruleProviderName, ruleProvider := modelcatalog.EnrichProvider(providerName, provider, model)
	profile := modelprofile.Resolve(ruleProviderName, model)
	modelCfg := ruleProvider.Models[strings.TrimSpace(model)]
	applyProviderLimits(&profile, ruleProvider, modelCfg)
	return capabilitiesFromProfile(ruleProviderName, ruleProvider, modelCfg, profile), behaviorFromProfile(profile)
}

func resolveConfiguredRole(cfg config.Config, role Role, roleCfg config.ModelRoleConfig, main Selection) (Selection, error) {
	if roleConfigEmpty(roleCfg) {
		return inheritSelection(role, main), nil
	}
	providerName := strings.TrimSpace(roleCfg.Provider)
	providerCfg := main.ProviderConfig
	if providerName == "" {
		providerName = main.Provider
	} else {
		var ok bool
		providerCfg, ok = cfg.Providers[providerName]
		if !ok {
			return Selection{}, fmt.Errorf("agent.model_roles.%s.provider %q not found in providers", role, providerName)
		}
	}
	model := strings.TrimSpace(roleCfg.Model)
	if model == "" {
		if providerName == main.Provider {
			model = main.Model
		} else {
			model = strings.TrimSpace(providerCfg.Model)
		}
	}
	if model == "" {
		return Selection{}, fmt.Errorf("agent.model_roles.%s.model is required", role)
	}
	providerCfg.Model = model
	return buildSelection(role, providerName, providerCfg, model, roleCfg.Variant, roleCfg.Effort, false), nil
}

func roleConfigEmpty(roleCfg config.ModelRoleConfig) bool {
	return strings.TrimSpace(roleCfg.Provider) == "" &&
		strings.TrimSpace(roleCfg.Model) == "" &&
		strings.TrimSpace(roleCfg.Effort) == "" &&
		strings.TrimSpace(roleCfg.Variant) == ""
}

func inheritSelection(role Role, main Selection) Selection {
	out := main
	out.Role = role
	out.Inherited = true
	out.ProviderOptions = modelvariant.CloneOptions(main.ProviderOptions)
	return out
}

func buildSelection(role Role, providerName string, provider config.ProviderConfig, model, variant, effort string, inherited bool) Selection {
	model = strings.TrimSpace(model)
	provider.Model = model
	ruleProviderName, ruleProvider := modelcatalog.EnrichProvider(providerName, provider, model)
	apiModel := modelcatalog.APIModel(ruleProvider, model)
	selection := modelvariant.ResolveForProvider(ruleProviderName, ruleProvider, model, variant, effort)
	capabilities, behavior := BuildFacts(providerName, provider, model)
	return Selection{
		Role:               role,
		Provider:           providerName,
		Model:              model,
		APIModel:           apiModel,
		Effort:             selection.DisplayEffort,
		LegacyEffort:       selection.LegacyEffort,
		Variant:            selection.Variant,
		ProviderOptions:    modelvariant.CloneOptions(selection.ProviderOptions),
		Inherited:          inherited,
		ProviderConfig:     provider,
		RuleProvider:       ruleProviderName,
		RuleProviderConfig: ruleProvider,
		Capabilities:       capabilities,
		Behavior:           behavior,
	}
}

func applyProviderLimits(profile *modelprofile.Profile, provider config.ProviderConfig, modelCfg config.ProviderModelConfig) {
	if profile == nil {
		return
	}
	if provider.ContextWindow > 0 {
		profile.Context.WindowTokens = provider.ContextWindow
	}
	if modelCfg.ContextWindow > 0 {
		profile.Context.WindowTokens = modelCfg.ContextWindow
	}
	if modelCfg.Limit != nil {
		if modelCfg.Limit.Context > 0 {
			profile.Context.WindowTokens = modelCfg.Limit.Context
		}
		if modelCfg.Limit.Output > 0 {
			profile.Context.MaxOutputTokens = modelCfg.Limit.Output
		}
	}
}

func capabilitiesFromProfile(providerName string, provider config.ProviderConfig, modelCfg config.ProviderModelConfig, profile modelprofile.Profile) Capabilities {
	toolCalling := string(profile.APIShape.ToolCalling)
	tools := profile.APIShape.ToolCalling != modelprofile.ToolCallingNone
	reasoning := profile.Reasoning.Budget != modelprofile.ReasoningBudgetNone ||
		profile.Reasoning.HiddenReasoning ||
		profile.Reasoning.VisibleReasoning
	if modelCfg.Reasoning != nil {
		reasoning = *modelCfg.Reasoning
	}
	if modelCfg.ToolCall != nil {
		tools = *modelCfg.ToolCall
		if !tools {
			toolCalling = string(modelprofile.ToolCallingNone)
		}
	}
	structuredOutput := tools
	if modelCfg.StructuredOutput != nil {
		structuredOutput = *modelCfg.StructuredOutput
	}
	imageInput := false
	fileInput := false
	if modelCfg.Modalities != nil {
		imageInput = containsString(modelCfg.Modalities.Input, "image")
		fileInput = containsString(modelCfg.Modalities.Input, "pdf")
	}
	inputLimit := 0
	outputLimit := profile.Context.MaxOutputTokens
	if modelCfg.Limit != nil {
		inputLimit = modelCfg.Limit.Input
		if modelCfg.Limit.Output > 0 {
			outputLimit = modelCfg.Limit.Output
		}
	}
	cacheGranularity := ""
	if profile.Context.CacheGranularity != modelprofile.CacheGranularityNone {
		cacheGranularity = string(profile.Context.CacheGranularity)
	}
	return Capabilities{
		Chat:                     profile.APIShape.Chat,
		Responses:                profile.APIShape.Responses,
		Tools:                    tools,
		ToolCalling:              toolCalling,
		StructuredOutput:         structuredOutput,
		Streaming:                true,
		StreamingToolArgs:        profile.APIShape.StreamingToolArgs,
		FreeformTool:             profile.APIShape.FreeformTool,
		ParallelToolCalls:        profile.APIShape.ParallelToolCalls,
		SystemRole:               profile.APIShape.SystemRole,
		DeveloperRole:            profile.APIShape.DeveloperRole,
		Reasoning:                reasoning,
		ContextWindow:            profile.Context.WindowTokens,
		InputLimit:               inputLimit,
		OutputLimit:              outputLimit,
		ImageInput:               imageInput,
		FileInput:                fileInput,
		PromptCache:              profile.Context.SupportsPromptCache,
		CacheGranularity:         cacheGranularity,
		ProtocolFamily:           protocolFamily(providerName, provider),
		RetrySafeErrorCategories: []string{"rate_limit", "server_error", "connection_error", "timeout"},
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), needle) {
			return true
		}
	}
	return false
}

func behaviorFromProfile(profile modelprofile.Profile) Behavior {
	jsonReliability := jsonReliabilityFor(profile)
	return Behavior{
		Family:                    string(profile.Family),
		DefaultWriteMode:          string(profile.Workflow.DefaultWriteMode),
		PreferredEditPrimitive:    string(profile.Code.PreferredEditPrimitive),
		PreferredPatchGrammar:     profile.Code.PreferredPatchGrammar,
		PatchReliability:          profile.Code.PatchReliability,
		ExactEditReliability:      profile.Code.ExactEditReliability,
		WholeFileReliability:      profile.Code.WholeFileReliability,
		JSONReliability:           jsonReliability,
		LongHorizonScore:          profile.Reasoning.LongHorizonScore,
		DefaultMaxAutonomousSteps: profile.Workflow.DefaultMaxAutonomousSteps,
		DefaultSearchBudget:       profile.Workflow.DefaultSearchBudget,
		NeedsReadBeforeWrite:      profile.Workflow.NeedsReadBeforeWrite,
		AllowParallelReadOnly:     profile.Workflow.AllowParallelReadOnly,
		AllowDirectShell:          profile.Workflow.AllowDirectShell,
		Latency:                   string(profile.Economics.Latency),
		Suitable:                  suitabilityFor(profile, jsonReliability),
	}
}

func jsonReliabilityFor(profile modelprofile.Profile) int {
	switch profile.APIShape.ToolCalling {
	case modelprofile.ToolCallingProviderNative:
		return 5
	case modelprofile.ToolCallingStrictJSON:
		return 4
	case modelprofile.ToolCallingJSON:
		return 3
	default:
		return 1
	}
}

func suitabilityFor(profile modelprofile.Profile, jsonReliability int) Suitability {
	return Suitability{
		Main:     true,
		Review:   profile.Code.TestDebugScore >= 3 && jsonReliability >= 3,
		Compact:  profile.Context.WindowTokens >= 32_000 || profile.Family != modelprofile.FamilyLocal,
		Title:    true,
		Memory:   jsonReliability >= 3,
		Worker:   profile.Workflow.DefaultMaxAutonomousSteps >= 10,
		Fallback: true,
	}
}

func protocolFamily(providerName string, provider config.ProviderConfig) string {
	wire := strings.ToLower(strings.TrimSpace(provider.WireAPI))
	if wire != "" {
		return wire
	}
	providerType := strings.ToLower(strings.TrimSpace(provider.Type))
	providerType = strings.ReplaceAll(providerType, "_", "-")
	switch providerType {
	case "anthropic", "claude", "anthropic-official":
		return "anthropic_messages"
	case "openai-codex", "codex-subscription", "chatgpt-codex", "codex-cli", "codex":
		return "openai_responses"
	}
	name := strings.ToLower(strings.TrimSpace(providerName))
	if strings.Contains(name, "anthropic") || strings.Contains(name, "claude") {
		return "anthropic_messages"
	}
	if strings.Contains(name, "codex") {
		return "openai_responses"
	}
	return "openai_chat"
}
