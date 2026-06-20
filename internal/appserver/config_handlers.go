package appserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/hooks"
	"github.com/blueberrycongee/wuu/internal/modelbudget"
	"github.com/blueberrycongee/wuu/internal/modelcatalog"
	"github.com/blueberrycongee/wuu/internal/modelroles"
	"github.com/blueberrycongee/wuu/internal/modelvariant"
	"github.com/blueberrycongee/wuu/internal/providerfactory"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/providers/codex"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/skills"
	"github.com/blueberrycongee/wuu/internal/tools"
	"github.com/blueberrycongee/wuu/internal/version"
)

func (s *Server) handleInitialize(req Request) error {
	core := version.Info()
	modelProfile, toolSurface := s.currentModelSurfaceSummaries()
	return s.writeResponse(req.ID, InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Core: CoreBuildInfo{
			Version: core.Version,
			Commit:  core.Commit,
			Date:    core.Date,
			Dirty:   core.Dirty,
		},
		Provider:       s.rt.ProviderName,
		Model:          s.rt.Model,
		Effort:         s.currentDisplayEffort(),
		Variant:        s.currentVariant(),
		WorkspaceRoot:  s.rt.RootDir,
		ToolPolicy:     s.currentToolPolicySummary(),
		Permissions:    s.currentPermissionSummary(),
		ExtensionTrust: s.currentExtensionTrustSummary(),
		ModelProfile:   modelProfile,
		ToolSurface:    toolSurface,
		ModelRoles:     s.currentModelRoleSummaries(),
		Providers:      s.providerSummaries(),
	}, nil)
}

func (s *Server) handleConfigRead(req Request) error {
	modelProfile, toolSurface := s.currentModelSurfaceSummaries()
	return s.writeResponse(req.ID, ConfigReadResult{
		Provider:       s.rt.ProviderName,
		Model:          s.rt.Model,
		Effort:         s.currentDisplayEffort(),
		Variant:        s.currentVariant(),
		ConfigPath:     s.rt.ConfigPath,
		WorkspaceRoot:  s.rt.RootDir,
		SessionDir:     s.rt.SessionDir,
		ToolPolicy:     s.currentToolPolicySummary(),
		Permissions:    s.currentPermissionSummary(),
		ExtensionTrust: s.currentExtensionTrustSummary(),
		ModelProfile:   modelProfile,
		ToolSurface:    toolSurface,
		ModelRoles:     s.currentModelRoleSummaries(),
		Providers:      s.providerSummaries(),
	}, nil)
}

func (s *Server) currentModelRoleSummaries() []ModelRoleSummary {
	if s == nil || s.rt == nil || s.rt.ModelRoles.Empty() {
		return nil
	}
	selections := s.rt.ModelRoles.List()
	out := make([]ModelRoleSummary, 0, len(selections))
	for _, selection := range selections {
		out = append(out, ModelRoleSummary{
			Role:         string(selection.Role),
			Provider:     selection.Provider,
			Model:        selection.Model,
			APIModel:     selection.APIModel,
			Effort:       selection.Effort,
			Variant:      selection.Variant,
			Inherited:    selection.Inherited,
			Capabilities: selection.Capabilities,
			Behavior:     selection.Behavior,
		})
	}
	return out
}

func (s *Server) currentToolPolicySummary() ToolPolicySummary {
	if s == nil || s.rt == nil {
		return ToolPolicySummary{}
	}
	policy := s.rt.ToolPolicy
	return ToolPolicySummary{
		Profile:       strings.TrimSpace(policy.Profile),
		DefaultAction: strings.TrimSpace(policy.DefaultAction),
		Tools:         cloneStringMap(policy.Tools),
		Kinds:         cloneStringMap(policy.Kinds),
		Risks:         cloneStringMap(policy.Risks),
	}
}

func (s *Server) currentPermissionSummary() PermissionSummary {
	if s == nil || s.rt == nil {
		return PermissionSummary{}
	}
	permissions := s.rt.Permissions
	if strings.TrimSpace(permissions.Mode) == "" {
		permissions, _ = config.PermissionPresetForMode(config.PermissionModeDefault)
	}
	return PermissionSummary{
		Mode:              strings.TrimSpace(permissions.Mode),
		PermissionProfile: strings.TrimSpace(permissions.PermissionProfile),
		ApprovalPolicy:    strings.TrimSpace(permissions.ApprovalPolicy),
		ApprovalsReviewer: strings.TrimSpace(permissions.ApprovalsReviewer),
	}
}

func (s *Server) currentModelSurfaceSummaries() (*ModelProfileSummary, *ToolSurfaceSummary) {
	if s == nil || s.rt == nil || s.rt.Toolkit == nil {
		return nil, nil
	}
	surface := s.rt.Toolkit.ActiveSurface()
	if surface.ProfileName == "" {
		return nil, nil
	}
	summary := surface.Summarize()
	profile := &ModelProfileSummary{
		ProfileName:   summary.ProfileName,
		Provider:      summary.Provider,
		Model:         summary.Model,
		EditPrimitive: summary.EditPrimitive,
		BashFirst:     summary.BashFirst,
	}
	return profile, &summary
}

func (s *Server) currentExtensionTrustSummary() ExtensionTrustSummary {
	main := ExtensionSessionTrustSummary{}
	if s != nil && s.rt != nil {
		toolStates := tools.ExtensionToolSurfaceStates{}
		if s.rt.Toolkit != nil {
			toolStates = s.rt.Toolkit.ExtensionToolSurfaceStates()
		}
		main.MCP = extensionSurfaceFromToolState(toolStates.MCP, toolStates.MCP.KnownTools > 0, toolStates.MCP.KnownTools)
		main.Skills = extensionSurfaceFromToolState(toolStates.Skills, len(s.rt.Skills) > 0, len(s.rt.Skills))
		main.Workflows = extensionSurfaceFromToolState(toolStates.Workflows, len(s.rt.Workflows) > 0, len(s.rt.Workflows))
		main.Hooks = ExtensionSurfaceTrustSummary{Allowed: s.rt.HookDispatcher != nil, Active: hookDispatcherHasAny(s.rt.HookDispatcher)}
		main.Plugins = ExtensionSurfaceTrustSummary{Allowed: true, Active: len(s.rt.Plugins) > 0, Count: len(s.rt.Plugins)}
		main.ExternalTools = main.MCP
	}
	reviewer := ExtensionSessionTrustSummary{
		MCP:           ExtensionSurfaceTrustSummary{Allowed: false, Active: false},
		Hooks:         ExtensionSurfaceTrustSummary{Allowed: false, Active: false},
		Plugins:       ExtensionSurfaceTrustSummary{Allowed: false, Active: false},
		Skills:        ExtensionSurfaceTrustSummary{Allowed: false, Active: false},
		Workflows:     ExtensionSurfaceTrustSummary{Allowed: false, Active: false},
		ExternalTools: ExtensionSurfaceTrustSummary{Allowed: false, Active: false},
	}
	return ExtensionTrustSummary{
		MainSession:     main,
		ReviewerSession: reviewer,
	}
}

func extensionSurfaceFromToolState(state tools.ExtensionToolSurfaceState, active bool, count int) ExtensionSurfaceTrustSummary {
	return ExtensionSurfaceTrustSummary{
		Allowed:      state.Allowed,
		Active:       active,
		Count:        count,
		KnownTools:   state.KnownTools,
		VisibleTools: state.VisibleTools,
	}
}

func hookDispatcherHasAny(dispatcher *hooks.Dispatcher) bool {
	if dispatcher == nil {
		return false
	}
	for _, event := range []hooks.Event{
		hooks.PreToolUse,
		hooks.PostToolUse,
		hooks.PostToolUseFailure,
		hooks.UserPromptSubmit,
		hooks.SessionStart,
		hooks.SessionEnd,
		hooks.Stop,
		hooks.FileChanged,
	} {
		if dispatcher.HasHooks(event) {
			return true
		}
	}
	return false
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *Server) handleConfigModelUpdate(req Request) error {
	var params ConfigModelUpdateParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	providerName := strings.TrimSpace(params.Provider)
	if providerName == "" {
		providerName = s.rt.ProviderName
	}
	model := strings.TrimSpace(params.Model)
	if model == "" {
		return s.writeResponse(req.ID, nil, errors.New("model is required"))
	}
	if s.hasRunningThread() {
		return s.writeResponse(req.ID, nil, errors.New("cannot change model while a turn is running"))
	}
	cfg, _, err := config.LoadFrom(s.rt.RootDir, os.Getenv("HOME"))
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	var providerCfg config.ProviderConfig
	var resolvedName string
	creatingProvider := params.CreateProvider
	if creatingProvider {
		if providerName == "" {
			return s.writeResponse(req.ID, nil, errors.New("provider is required"))
		}
		if _, _, err := cfg.ResolveProvider(providerName); err == nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("provider %q already exists", providerName))
		}
		baseURL := strings.TrimSpace(stringValue(params.BaseURL))
		apiKey := strings.TrimSpace(stringValue(params.APIKey))
		if baseURL == "" {
			return s.writeResponse(req.ID, nil, errors.New("base_url is required"))
		}
		if apiKey == "" {
			return s.writeResponse(req.ID, nil, errors.New("api_key is required"))
		}
		providerCfg = config.ProviderConfig{
			Type:    "openai-compatible",
			BaseURL: baseURL,
			APIKey:  apiKey,
			Model:   model,
		}
		resolvedName = providerName
	} else {
		providerCfg, resolvedName, err = cfg.ResolveProvider(providerName)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		providerCfg = s.withCachedCodexModels(resolvedName, providerCfg)
	}
	previousProviderCfg := providerCfg
	previousModel := strings.TrimSpace(providerCfg.Model)
	providerCfg.Model = model
	connectionChanged := creatingProvider
	connectionLocked := isCodexProviderType(providerCfg.Type)
	if connectionLocked && (params.BaseURL != nil || strings.TrimSpace(stringValue(params.APIKey)) != "") {
		return s.writeResponse(req.ID, nil, errors.New("connection settings are managed by OpenAI OAuth for this provider"))
	}
	if params.BaseURL != nil {
		baseURL := strings.TrimSpace(*params.BaseURL)
		if baseURL == "" {
			return s.writeResponse(req.ID, nil, errors.New("base_url is required"))
		}
		if baseURL != strings.TrimSpace(providerCfg.BaseURL) {
			connectionChanged = true
		}
		providerCfg.BaseURL = baseURL
	}
	apiKeyForConfig := params.APIKey
	authKeyForStore := ""
	if params.APIKey != nil {
		apiKey := strings.TrimSpace(*params.APIKey)
		if apiKey != "" {
			connectionChanged = true
			authKeyForStore = apiKey
			providerCfg.APIKey = apiKey
			providerCfg.APIKeyEnv = ""
			empty := ""
			apiKeyForConfig = &empty
		} else {
			apiKeyForConfig = nil
		}
	}
	variant := s.currentVariant()
	legacyEffort := s.currentEffort()
	selectionTouched := params.Variant != nil || params.Effort != nil
	if params.Variant != nil {
		variant = strings.TrimSpace(*params.Variant)
		if variant == "" && params.Effort == nil {
			legacyEffort = ""
		}
	}
	if params.Effort != nil {
		legacyEffort = strings.TrimSpace(*params.Effort)
		if params.Variant == nil {
			variant = legacyEffort
		}
	}
	ruleProviderName, ruleProviderCfg := modelcatalog.EnrichProvider(resolvedName, providerCfg, model)
	_, previousRuleProviderCfg := modelcatalog.EnrichProvider(resolvedName, previousProviderCfg, previousModel)
	modelHeadersChanged := !reflect.DeepEqual(previousRuleProviderCfg.Headers, ruleProviderCfg.Headers)
	if params.Variant != nil && variant != "" {
		if _, ok := modelvariant.OptionsForProvider(ruleProviderName, ruleProviderCfg, model, variant); !ok {
			return s.writeResponse(req.ID, nil, fmt.Errorf("model %s does not support variant %s", model, variant))
		}
	}
	if params.Effort != nil && params.Variant == nil && variant != "" && len(modelvariant.SummariesForProvider(ruleProviderName, ruleProviderCfg, model)) > 0 {
		if _, ok := modelvariant.OptionsForProvider(ruleProviderName, ruleProviderCfg, model, variant); !ok {
			return s.writeResponse(req.ID, nil, fmt.Errorf("model %s does not support effort %s", model, variant))
		}
	}
	selection := modelvariant.ResolveForProvider(ruleProviderName, ruleProviderCfg, model, variant, legacyEffort)
	effort := selection.DisplayEffort
	effortForConfig, variantForConfig := selectionConfigPointers(selection, selectionTouched, s.currentVariant())

	var client providers.StreamClient
	if resolvedName != s.rt.ProviderName || connectionChanged || modelHeadersChanged {
		client, err = providerfactory.BuildStreamClient(ruleProviderCfg, resolvedName)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	if authKeyForStore != "" {
		if err := config.SaveAuthKey(os.Getenv("HOME"), resolvedName, authKeyForStore); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	if creatingProvider {
		err = config.CreateProviderRuntime(s.rt.ConfigPath, resolvedName, model, params.BaseURL, apiKeyForConfig, effortForConfig, variantForConfig, params.ToolPolicyProfile, params.PermissionMode)
	} else {
		err = config.UpdateProviderRuntime(s.rt.ConfigPath, resolvedName, model, params.BaseURL, apiKeyForConfig, effortForConfig, variantForConfig, params.ToolPolicyProfile, params.PermissionMode)
	}
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	previousRuntimeProvider := s.rt.ProviderName
	s.rt.ProviderName = resolvedName
	s.rt.Model = model
	roleSelections, err := modelroles.Resolve(cfg, modelroles.ResolveOptions{
		ProviderName:   resolvedName,
		ProviderConfig: providerCfg,
		Model:          model,
		Effort:         selection.LegacyEffort,
		Variant:        selection.Variant,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	s.rt.ModelRoles = roleSelections
	apiModel := modelcatalog.APIModel(ruleProviderCfg, model)
	if roleSelections.Title.Inherited {
		if client != nil {
			s.rt.TitleClient = client
		}
	} else {
		titleClient, titleErr := providerfactory.BuildStreamClient(roleSelections.Title.RuleProviderConfig, roleSelections.Title.Provider)
		if titleErr != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("build title client: %w", titleErr))
		}
		s.rt.TitleClient = titleClient
	}
	if s.rt.Toolkit != nil && (!roleSelections.Worker.Inherited || connectionChanged || modelHeadersChanged || resolvedName != previousRuntimeProvider) {
		workerRetry := providerfactory.SubAgentRetryConfig()
		workerClient, workerErr := providerfactory.BuildStreamClientWithRetry(roleSelections.Worker.RuleProviderConfig, roleSelections.Worker.Provider, &workerRetry)
		if workerErr != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("build worker client: %w", workerErr))
		}
		s.rt.WorkerClient = workerClient
	}
	if s.rt.Toolkit != nil {
		s.rt.Toolkit.ConfigureSurfaceForProviderModel(ruleProviderName, apiModel)
	}
	if params.PermissionMode != nil {
		permissions, _ := config.PermissionPresetForMode(*params.PermissionMode)
		s.rt.Permissions = permissions
		s.rt.ToolPolicy = config.ToolPolicyConfig{}
		if s.rt.Toolkit != nil {
			s.rt.Toolkit.SetToolPolicy(runtime.ToolPolicyFromConfigAndPermissions(s.rt.ToolPolicy, s.rt.Permissions))
			s.rt.Toolkit.SetPermissionBoundary(tools.PermissionBoundaryForProfile(s.rt.Permissions.PermissionProfile))
			s.installToolApprovalReviewer(s.rt.Toolkit)
		}
	} else if params.ToolPolicyProfile != nil {
		profile := strings.TrimSpace(*params.ToolPolicyProfile)
		s.rt.ToolPolicy = config.ToolPolicyConfig{Profile: profile}
		if s.rt.Toolkit != nil {
			s.rt.Toolkit.SetToolPolicy(runtime.ToolPolicyFromConfig(s.rt.ToolPolicy))
			s.rt.Toolkit.SetPermissionBoundary(tools.PermissionBoundaryForProfile(s.rt.Permissions.PermissionProfile))
			s.installToolApprovalReviewer(s.rt.Toolkit)
		}
	}
	systemPrompt := s.rt.RefreshSystemPrompt(resolvedName, apiModel)
	modelBudget := runtime.ResolveModelBudget(model, ruleProviderCfg, cfg.Agent.MaxContextTokens)
	s.rt.ModelBudget = modelBudget
	if s.rt.StreamRunner != nil {
		if client != nil {
			s.rt.StreamRunner.Client = client
		}
		s.rt.StreamRunner.Model = model
		s.rt.StreamRunner.APIModel = apiModel
		s.rt.StreamRunner.Effort = selection.LegacyEffort
		s.rt.StreamRunner.Variant = selection.Variant
		s.rt.StreamRunner.ProviderOptions = modelvariant.CloneOptions(selection.ProviderOptions)
		s.rt.StreamRunner.ContextWindowOverride = modelBudget.ContextWindowTokens
		s.rt.StreamRunner.MaxInputTokens = modelBudget.InputLimitTokens
		s.rt.StreamRunner.OutputReserveTokens = modelBudget.OutputReserveTokens
	}
	s.updateIdleThreadRuntime(resolvedName, ruleProviderName, model, apiModel, systemPrompt)

	modelProfile, toolSurface := s.currentModelSurfaceSummaries()
	return s.writeResponse(req.ID, ConfigModelUpdateResult{
		Provider:       resolvedName,
		Model:          model,
		Effort:         effort,
		Variant:        selection.Variant,
		ToolPolicy:     s.currentToolPolicySummary(),
		Permissions:    s.currentPermissionSummary(),
		ExtensionTrust: s.currentExtensionTrustSummary(),
		ModelProfile:   modelProfile,
		ToolSurface:    toolSurface,
		ModelRoles:     s.currentModelRoleSummaries(),
		Providers:      s.providerSummaries(),
	}, nil)
}

func (s *Server) handleConfigCodexModels(ctx context.Context, req Request) error {
	var params ConfigCodexModelsParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	providerName := strings.TrimSpace(params.Provider)
	if providerName == "" {
		providerName = s.rt.ProviderName
	}
	cfg, _, err := config.LoadFrom(s.rt.RootDir, os.Getenv("HOME"))
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	providerCfg, resolvedName, err := cfg.ResolveProvider(providerName)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if !isCodexProviderType(providerCfg.Type) {
		return s.writeResponse(req.ID, nil, fmt.Errorf("provider %s uses type %s; Codex models require openai-codex", resolvedName, providerCfg.Type))
	}
	client, err := codex.New(codex.ClientConfig{
		BaseURL: providerCfg.BaseURL,
		APIKey:  explicitProviderAPIKey(providerCfg),
		Headers: providerCfg.Headers,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	models, err := client.Models(ctx)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	s.cacheCodexModels(resolvedName, models)
	out := make([]CodexModelSummary, 0, len(models))
	for _, model := range models {
		out = append(out, CodexModelSummary{
			Slug:                  model.Slug,
			DisplayName:           model.DisplayName,
			DefaultReasoningLevel: model.DefaultReasoningLevel,
			SupportedReasoning:    append([]string(nil), model.SupportedReasoning...),
			SupportedInAPI:        model.SupportedInAPI,
		})
	}
	providerCfg = s.withCachedCodexModels(resolvedName, providerCfg)
	ruleProviderName, ruleProviderCfg := modelcatalog.EnrichProvider(resolvedName, providerCfg, providerCfg.Model)
	selection := modelvariant.ResolveForProvider(ruleProviderName, ruleProviderCfg, providerCfg.Model, cfg.Agent.Variant, cfg.Agent.Effort)
	effort := selection.DisplayEffort
	variant := selection.Variant
	if resolvedName == s.rt.ProviderName {
		effort = s.currentDisplayEffort()
		variant = s.currentVariant()
	}
	return s.writeResponse(req.ID, ConfigCodexModelsResult{
		Provider: resolvedName,
		Model:    providerCfg.Model,
		Effort:   effort,
		Variant:  variant,
		Models:   out,
	}, nil)
}

func (s *Server) handleSkillList(req Request) error {
	return s.writeResponse(req.ID, SkillListResult{Skills: skillSummaries(s.rt.Skills)}, nil)
}

func skillSummaries(items []skills.Skill) []SkillSummary {
	out := make([]SkillSummary, 0, len(items))
	for _, item := range items {
		out = append(out, SkillSummary{
			Name:                  item.Name,
			Description:           item.Description,
			WhenToUse:             item.WhenToUse,
			TriggerCondition:      item.TriggerCondition,
			Source:                item.Source,
			Path:                  item.Path,
			ArgumentHint:          item.ArgumentHint,
			Model:                 item.Model,
			Context:               item.Context,
			Agent:                 item.Agent,
			AllowedTools:          append([]string(nil), item.AllowedTools...),
			RequiredContext:       append([]string(nil), item.RequiredContext...),
			Examples:              append([]string(nil), item.Examples...),
			VerificationChecklist: append([]string(nil), item.VerificationChecklist...),
			ProgressiveDisclosure: item.ProgressiveDisclosure,
			UserInvocable:         item.UserInvocable,
			DisableModelInvoke:    item.DisableModelInvoke,
			Paths:                 append([]string(nil), item.Paths...),
			Effort:                item.Effort,
			Version:               item.Version,
		})
	}
	return out
}

func (s *Server) updateIdleThreadRuntime(providerName, ruleProviderName, model, apiModel, systemPrompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, th := range s.threads {
		th.mu.Lock()
		if !th.running {
			th.ModelProvider = providerName
			th.Model = model
			if strings.TrimSpace(systemPrompt) != "" {
				th.History = replaceBaseSystemPrompt(th.History, systemPrompt)
				if strings.TrimSpace(th.MemoryPath) != "" {
					if err := rewriteChatHistory(th.MemoryPath, th.History); err != nil {
						providers.DebugLogf("rewrite thread %q system prompt after model update: %v", th.ID, err)
					}
				}
			}
			if th.execRuntime != nil {
				if th.execRuntime.StreamRunner != nil && s.rt != nil && s.rt.StreamRunner != nil {
					th.execRuntime.StreamRunner.Client = s.rt.StreamRunner.Client
					th.execRuntime.StreamRunner.Model = model
					th.execRuntime.StreamRunner.APIModel = apiModel
					th.execRuntime.StreamRunner.Effort = s.currentEffort()
					th.execRuntime.StreamRunner.Variant = s.currentVariant()
					th.execRuntime.StreamRunner.ProviderOptions = s.currentProviderOptions()
					th.execRuntime.StreamRunner.ContextWindowOverride = s.rt.StreamRunner.ContextWindowOverride
					th.execRuntime.StreamRunner.MaxInputTokens = s.rt.StreamRunner.MaxInputTokens
					th.execRuntime.StreamRunner.OutputReserveTokens = s.rt.StreamRunner.OutputReserveTokens
				}
				th.execRuntime.ModelBudget = s.rt.ModelBudget
				if th.execRuntime.Toolkit != nil {
					th.execRuntime.Toolkit.ConfigureSurfaceForProviderModel(ruleProviderName, apiModel)
					if s.rt != nil {
						th.execRuntime.Toolkit.SetToolPolicy(runtime.ToolPolicyFromConfigAndPermissions(s.rt.ToolPolicy, s.rt.Permissions))
						th.execRuntime.Toolkit.SetPermissionBoundary(tools.PermissionBoundaryForProfile(s.rt.Permissions.PermissionProfile))
						s.installToolApprovalReviewer(th.execRuntime.Toolkit)
					}
				}
			}
		}
		th.mu.Unlock()
	}
}

func (s *Server) currentEffort() string {
	if s == nil || s.rt == nil || s.rt.StreamRunner == nil {
		return ""
	}
	return strings.TrimSpace(s.rt.StreamRunner.Effort)
}

func (s *Server) currentVariant() string {
	if s == nil || s.rt == nil || s.rt.StreamRunner == nil {
		return ""
	}
	return strings.TrimSpace(s.rt.StreamRunner.Variant)
}

func (s *Server) currentDisplayEffort() string {
	if variant := s.currentVariant(); variant != "" {
		return variant
	}
	return s.currentEffort()
}

func (s *Server) currentProviderOptions() map[string]any {
	if s == nil || s.rt == nil || s.rt.StreamRunner == nil {
		return nil
	}
	return modelvariant.CloneOptions(s.rt.StreamRunner.ProviderOptions)
}

func selectionConfigPointers(selection modelvariant.Selection, touched bool, previousVariant string) (*string, *string) {
	if !touched && (strings.TrimSpace(previousVariant) == "" || selection.Variant != "") {
		return nil, nil
	}
	if selection.Variant != "" {
		empty := ""
		variant := selection.Variant
		return &empty, &variant
	}
	if selection.LegacyEffort != "" {
		effort := selection.LegacyEffort
		empty := ""
		return &effort, &empty
	}
	emptyEffort := ""
	emptyVariant := ""
	return &emptyEffort, &emptyVariant
}

func (s *Server) providerSummaries() []ProviderSummary {
	cfg, _, err := config.LoadFrom(s.rt.RootDir, os.Getenv("HOME"))
	if err != nil {
		return nil
	}
	return providerSummariesFromConfig(cfg, os.Getenv("HOME"))
}

func providerSummariesFromConfig(cfg config.Config, home string) []ProviderSummary {
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ProviderSummary, 0, len(names))
	for _, name := range names {
		provider := cfg.Providers[name]
		out = append(out, ProviderSummary{
			Name:             name,
			Type:             provider.Type,
			Model:            provider.Model,
			BaseURL:          provider.BaseURL,
			APIKeyConfigured: providerHasAuth(name, provider, home),
			ConnectionLocked: isCodexProviderType(provider.Type),
			Models:           providerModelSummaries(name, provider),
		})
	}
	return out
}

func providerHasAuth(name string, provider config.ProviderConfig, home string) bool {
	if provider.APIKey != "" || provider.APIKeyEnv != "" || provider.AuthToken != "" || provider.AuthTokenEnv != "" {
		return true
	}
	key, err := config.LoadAuthKey(home, name)
	return err == nil && strings.TrimSpace(key) != ""
}

func providerModelSummaries(providerName string, provider config.ProviderConfig) []ProviderModelSummary {
	ruleProviderName := providerName
	ruleProvider := provider
	var catalogProvider modelcatalog.Provider
	hasCatalog := false
	if matched, ok := modelcatalog.MatchProvider(providerName, provider); ok {
		catalogProvider = matched
		hasCatalog = true
		ruleProviderName = matched.ID
		ruleProvider = modelcatalog.MergeProvider(provider, matched)
	}

	models := make(map[string]ProviderModelSummary, len(ruleProvider.Models)+1)
	disabled := map[string]bool{}
	for id, model := range provider.Models {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if model.Disabled {
			disabled[id] = true
			continue
		}
		model = ruleProvider.Models[id]
		capabilities, behavior := modelroles.BuildFacts(ruleProviderName, ruleProvider, id)
		models[id] = ProviderModelSummary{
			ID:               id,
			DisplayName:      strings.TrimSpace(model.Name),
			DefaultEffort:    strings.TrimSpace(model.DefaultEffort),
			DefaultVariant:   strings.TrimSpace(model.DefaultVariant),
			SupportedEfforts: modelvariant.SupportedEffortsForProvider(ruleProviderName, ruleProvider, id, model),
			Variants:         providerVariantSummaries(modelvariant.SummariesForProvider(ruleProviderName, ruleProvider, id)),
			Capabilities:     capabilities,
			Behavior:         behavior,
			Source:           "config",
		}
	}
	if hasCatalog {
		for _, catalogModel := range catalogProvider.Models {
			id := strings.TrimSpace(catalogModel.ID)
			if id == "" || disabled[id] {
				continue
			}
			if _, ok := models[id]; ok {
				continue
			}
			model := ruleProvider.Models[id]
			capabilities, behavior := modelroles.BuildFacts(ruleProviderName, ruleProvider, id)
			models[id] = ProviderModelSummary{
				ID:               id,
				DisplayName:      strings.TrimSpace(model.Name),
				DefaultEffort:    strings.TrimSpace(model.DefaultEffort),
				DefaultVariant:   strings.TrimSpace(model.DefaultVariant),
				SupportedEfforts: modelvariant.SupportedEffortsForProvider(ruleProviderName, ruleProvider, id, model),
				Variants:         providerVariantSummaries(modelvariant.SummariesForProvider(ruleProviderName, ruleProvider, id)),
				Capabilities:     capabilities,
				Behavior:         behavior,
				Source:           "models.dev",
			}
		}
	}
	current := strings.TrimSpace(provider.Model)
	if current != "" {
		if _, ok := models[current]; !ok {
			selectedRuleProviderName, selectedRuleProvider := modelcatalog.EnrichProvider(providerName, provider, current)
			capabilities, behavior := modelroles.BuildFacts(selectedRuleProviderName, selectedRuleProvider, current)
			models[current] = ProviderModelSummary{
				ID:               current,
				SupportedEfforts: modelvariant.SupportedEffortsForProvider(selectedRuleProviderName, selectedRuleProvider, current, selectedRuleProvider.Models[current]),
				Variants:         providerVariantSummaries(modelvariant.SummariesForProvider(selectedRuleProviderName, selectedRuleProvider, current)),
				Capabilities:     capabilities,
				Behavior:         behavior,
				Source:           "selected",
			}
		}
	}
	out := make([]ProviderModelSummary, 0, len(models))
	for _, model := range models {
		if len(model.Variants) == 0 {
			model.Variants = providerVariantSummaries(modelvariant.SummariesForProvider(ruleProviderName, ruleProvider, model.ID))
		}
		if len(model.SupportedEfforts) == 0 {
			model.SupportedEfforts = modelvariant.EffortIDs(modelVariantSummaries(model.Variants))
		}
		if model.DefaultVariant == "" && model.DefaultEffort != "" {
			model.DefaultVariant = model.DefaultEffort
		}
		if model.DefaultVariant == "" {
			model.DefaultVariant = modelvariant.DefaultVariantForProvider(ruleProviderName, ruleProvider, model.ID)
		}
		out = append(out, model)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == current {
			return true
		}
		if out[j].ID == current {
			return false
		}
		return strings.ToLower(out[i].ID) < strings.ToLower(out[j].ID)
	})
	return out
}

func providerVariantSummaries(variants []modelvariant.Variant) []ProviderModelVariantSummary {
	if len(variants) == 0 {
		return nil
	}
	out := make([]ProviderModelVariantSummary, 0, len(variants))
	for _, variant := range variants {
		out = append(out, ProviderModelVariantSummary{
			ID:      variant.ID,
			Options: modelvariant.CloneOptions(variant.Options),
		})
	}
	return out
}

func modelVariantSummaries(variants []ProviderModelVariantSummary) []modelvariant.Variant {
	if len(variants) == 0 {
		return nil
	}
	out := make([]modelvariant.Variant, 0, len(variants))
	for _, variant := range variants {
		out = append(out, modelvariant.Variant{
			ID:      variant.ID,
			Options: modelvariant.CloneOptions(variant.Options),
		})
	}
	return out
}

func isCodexProviderType(providerType string) bool {
	s := strings.ToLower(strings.TrimSpace(providerType))
	s = strings.ReplaceAll(s, "_", "-")
	return s == "openai-codex" || s == "codex-subscription" || s == "chatgpt-codex"
}

func (s *Server) cacheCodexModels(providerName string, models []codex.ModelInfo) {
	providerName = strings.TrimSpace(providerName)
	if s == nil || providerName == "" {
		return
	}
	configs := codexLiveModelConfigs(models)
	s.codexModelsMu.Lock()
	defer s.codexModelsMu.Unlock()
	if s.codexModelCache == nil {
		s.codexModelCache = make(map[string]map[string]config.ProviderModelConfig)
	}
	s.codexModelCache[providerName] = configs
}

func (s *Server) withCachedCodexModels(providerName string, provider config.ProviderConfig) config.ProviderConfig {
	if s == nil || !isCodexProviderType(provider.Type) {
		return provider
	}
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return provider
	}
	s.codexModelsMu.Lock()
	cached := cloneProviderModelConfigs(s.codexModelCache[providerName])
	s.codexModelsMu.Unlock()
	if len(cached) == 0 {
		return provider
	}
	out := provider
	out.Models = cloneProviderModelConfigs(provider.Models)
	if out.Models == nil {
		out.Models = make(map[string]config.ProviderModelConfig, len(cached))
	}
	for id, live := range cached {
		out.Models[id] = modelcatalog.MergeModelConfig(live, out.Models[id])
	}
	return out
}

func codexLiveModelConfigs(models []codex.ModelInfo) map[string]config.ProviderModelConfig {
	out := make(map[string]config.ProviderModelConfig, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.Slug)
		if id == "" {
			continue
		}
		cfg := codexCatalogFallbackModelConfig(id)
		cfg.ID = id
		if name := strings.TrimSpace(model.DisplayName); name != "" {
			cfg.Name = name
		}
		efforts := normalizedCodexEfforts(model.SupportedReasoning)
		if len(efforts) > 0 || strings.TrimSpace(model.DefaultReasoningLevel) != "" {
			reasoning := true
			cfg.Reasoning = &reasoning
		}
		if len(efforts) > 0 {
			cfg.SupportedEfforts = efforts
			cfg.Variants = codexReasoningVariants(efforts)
		}
		if effort := strings.TrimSpace(model.DefaultReasoningLevel); effort != "" {
			cfg.DefaultEffort = effort
			cfg.DefaultVariant = effort
		}
		applyCodexSubscriptionLimit(id, &cfg)
		out[id] = cfg
	}
	return out
}

func codexCatalogFallbackModelConfig(modelID string) config.ProviderModelConfig {
	provider, ok := modelcatalog.MatchProvider("openai", config.ProviderConfig{Type: "openai"})
	if !ok {
		return config.ProviderModelConfig{ID: modelID}
	}
	for _, model := range provider.Models {
		if strings.TrimSpace(model.ID) == modelID {
			return modelcatalog.ModelConfig(model)
		}
	}
	return config.ProviderModelConfig{ID: modelID}
}

func applyCodexSubscriptionLimit(modelID string, cfg *config.ProviderModelConfig) {
	if cfg == nil {
		return
	}
	id := strings.ToLower(strings.TrimSpace(modelID))
	if !strings.Contains(id, "gpt-5.5") {
		return
	}
	cfg.ContextWindow = 400_000
	cfg.Limit = &config.ProviderModelLimitConfig{
		Context: 400_000,
		Input:   modelbudget.CodexSubscriptionGPT5InputCap,
		Output:  128_000,
	}
}

func normalizedCodexEfforts(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func codexReasoningVariants(efforts []string) map[string]map[string]any {
	if len(efforts) == 0 {
		return nil
	}
	out := make(map[string]map[string]any, len(efforts))
	for _, effort := range efforts {
		effort = strings.TrimSpace(effort)
		if effort == "" {
			continue
		}
		out[effort] = map[string]any{
			"reasoningEffort":  effort,
			"reasoningSummary": "auto",
			"include":          []any{"reasoning.encrypted_content"},
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneProviderModelConfigs(input map[string]config.ProviderModelConfig) map[string]config.ProviderModelConfig {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]config.ProviderModelConfig, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func explicitProviderAPIKey(provider config.ProviderConfig) string {
	if key := strings.TrimSpace(provider.APIKey); key != "" {
		return key
	}
	if envKey := strings.TrimSpace(provider.APIKeyEnv); envKey != "" {
		return strings.TrimSpace(os.Getenv(envKey))
	}
	return ""
}
