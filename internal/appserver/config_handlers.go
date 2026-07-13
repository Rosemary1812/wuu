package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/blueberrycongee/wuu/internal/authstorage"
	"github.com/blueberrycongee/wuu/internal/config"
	"github.com/blueberrycongee/wuu/internal/extensions"
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
	"github.com/blueberrycongee/wuu/internal/subagent"
	"github.com/blueberrycongee/wuu/internal/version"
)

func (s *Server) handleInitialize(req Request) error {
	core := version.Info()
	modelProfile, toolSurface := s.currentModelSurfaceSummaries()
	status := "ready"
	issues := make([]RuntimeIssue, 0, len(s.rt.ReadinessIssues))
	for _, issue := range s.rt.ReadinessIssues {
		status = "needs_setup"
		issues = append(issues, RuntimeIssue{Code: issue.Code, Provider: issue.Provider, Message: issue.Message})
	}
	return s.writeResponse(req.ID, InitializeResult{
		Status:             status,
		Issues:             issues,
		ProtocolVersion:    ProtocolVersion,
		AgentTemplateCount: len(s.rt.AgentTemplates),
		Core: CoreBuildInfo{
			Version: core.Version,
			Commit:  core.Commit,
			Date:    core.Date,
			Dirty:   core.Dirty,
		},
		Provider:           s.rt.ProviderName,
		Model:              s.rt.Model,
		Effort:             s.currentDisplayEffort(),
		Variant:            s.currentVariant(),
		WorkspaceRoot:      s.rt.RootDir,
		Permissions:        s.currentPermissionSummary(),
		ExtensionTrust:     s.currentExtensionTrustSummary(),
		ExtensionInventory: s.currentExtensionInventory(),
		ModelProfile:       modelProfile,
		ToolSurface:        toolSurface,
		ModelRoles:         s.currentModelRoleSummaries(),
		Providers:          s.providerSummaries(),
		AdvancedSettings:   s.currentAdvancedSettingsSummary(),
		GeneralSettings:    s.currentGeneralSettingsSummary(),
	}, nil)
}

func (s *Server) handleConfigRead(req Request) error {
	modelProfile, toolSurface := s.currentModelSurfaceSummaries()
	return s.writeResponse(req.ID, ConfigReadResult{
		Provider:           s.rt.ProviderName,
		AgentTemplateCount: len(s.rt.AgentTemplates),
		Model:              s.rt.Model,
		Effort:             s.currentDisplayEffort(),
		Variant:            s.currentVariant(),
		ConfigPath:         s.rt.ConfigPath,
		WorkspaceRoot:      s.rt.RootDir,
		SessionDir:         s.rt.SessionDir,
		Permissions:        s.currentPermissionSummary(),
		ExtensionTrust:     s.currentExtensionTrustSummary(),
		ExtensionInventory: s.currentExtensionInventory(),
		ModelProfile:       modelProfile,
		ToolSurface:        toolSurface,
		ModelRoles:         s.currentModelRoleSummaries(),
		Providers:          s.providerSummaries(),
		AdvancedSettings:   s.currentAdvancedSettingsSummary(),
		GeneralSettings:    s.currentGeneralSettingsSummary(),
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

func (s *Server) currentPermissionSummary() PermissionSummary {
	if s == nil || s.rt == nil {
		return PermissionSummary{}
	}
	permissions := s.rt.Permissions
	if strings.TrimSpace(permissions.Mode) == "" {
		permissions = config.ResolvedPermissions{Mode: config.PermissionModeStandard}
	}
	return PermissionSummary{
		Mode: strings.TrimSpace(permissions.Mode),
	}
}

func (s *Server) currentAdvancedSettingsSummary() AdvancedSettingsSummary {
	if s == nil || s.rt == nil {
		return AdvancedSettingsSummary{}
	}
	summary := AdvancedSettingsSummary{}
	if s.rt.StreamRunner != nil {
		summary.MaxSteps = s.rt.StreamRunner.MaxSteps
		summary.Temperature = s.rt.StreamRunner.Temperature
		summary.CompactThresholdPct = s.rt.StreamRunner.CompactThresholdPct
		summary.CompactKeepRecentTokens = s.rt.StreamRunner.CompactKeepRecentTokens
		summary.DisableAutoCompact = s.rt.StreamRunner.DisableAutoCompact
	}
	if cfg, _, err := s.rt.LoadEffectiveConfig(); err == nil {
		summary.MaxSteps = cfg.Agent.MaxSteps
		summary.MaxContextTokens = cfg.Agent.MaxContextTokens
		summary.Temperature = cfg.Agent.Temperature
		summary.CompactThresholdPct = cfg.Agent.CompactThresholdPct
		summary.CompactKeepRecentTokens = cfg.Agent.CompactKeepRecentTokens
		summary.DisableAutoCompact = cfg.Agent.DisableAutoCompact
		if provider, _, err := cfg.ResolveProvider(s.rt.ProviderName); err == nil {
			summary.ProviderContextWindow = provider.ContextWindow
		}
	}
	budget := s.rt.ModelBudget
	contextWindowTokens, contextWindowSource := budget.EffectiveContextWindow()
	summary.ContextWindowTokens = contextWindowTokens
	summary.ContextWindowSource = string(contextWindowSource)
	summary.InputLimitTokens = budget.InputLimitTokens
	summary.OutputReserveTokens = budget.OutputReserveTokens
	summary.CompactThresholdTokens = advancedCompactThresholdTokens(budget, summary.CompactThresholdPct)
	return summary
}

func (s *Server) currentGeneralSettingsSummary() GeneralSettingsSummary {
	if s == nil || s.rt == nil {
		return GeneralSettingsSummary{}
	}
	summary := GeneralSettingsSummary{
		MCPServerEnabled: map[string]bool{},
	}
	if cfg, _, err := s.rt.LoadEffectiveConfig(); err == nil {
		summary.AppendSystemPrompt = cfg.Agent.AppendSystemPrompt
		summary.MemoryDisabled = cfg.Memory.Disable
		for name, server := range cfg.MCPServers {
			if server.Enabled == nil || *server.Enabled {
				summary.MCPServerEnabled[name] = true
			} else {
				summary.MCPServerEnabled[name] = false
			}
		}
	}
	return summary
}

func advancedCompactThresholdTokens(budget modelbudget.Budget, pct float64) int {
	if pct <= 0 || pct >= 1 {
		return budget.CompactThresholdTokens
	}
	baseWindow := budget.ContextWindowTokens
	if baseWindow <= 0 || (budget.InputLimitTokens > 0 && budget.InputLimitTokens < baseWindow) {
		baseWindow = budget.InputLimitTokens
	}
	if baseWindow <= 0 {
		return 0
	}
	threshold := int(float64(baseWindow) * pct)
	if budget.ContextWindowTokens > 0 {
		outputReserved := budget.ContextWindowTokens - budget.OutputReserveTokens
		if outputReserved > 0 && outputReserved < threshold {
			threshold = outputReserved
		}
	}
	return threshold
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
		mcpKnownTools := 0
		if s.rt.Toolkit != nil {
			if manager := s.rt.Toolkit.MCPManager(); manager != nil {
				mcpKnownTools = len(manager.AllTools())
			}
		}
		main.MCP = extensionSurfaceSummary(mcpKnownTools > 0, mcpKnownTools)
		main.Skills = extensionSurfaceSummary(len(s.rt.Skills) > 0, len(s.rt.Skills))
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

func (s *Server) currentExtensionInventory() []ExtensionInventoryRecord {
	if s == nil || s.rt == nil {
		return nil
	}
	cfg := s.currentExtensionConfig()
	grants := extensions.Settings{}
	if cfg.Extensions != nil {
		grants = *cfg.Extensions
	}

	pluginsByID := make(map[string]struct {
		scope    string
		official bool
	}, len(s.rt.Plugins))
	for _, item := range s.rt.Plugins {
		pluginsByID[item.ID] = struct {
			scope    string
			official bool
		}{scope: normalizedExtensionScope(item.Source, item.ManifestPath, s.rt.RootDir), official: item.Official}
	}

	records := make([]ExtensionInventoryRecord, 0, len(s.rt.Skills)+len(s.rt.AgentTemplates)+len(s.rt.Plugins))
	for _, skill := range s.rt.Skills {
		source := strings.TrimSpace(skill.Source)
		scope := normalizedExtensionScope(source, skill.Path, s.rt.RootDir)
		pluginID := strings.TrimPrefix(source, "plugin:")
		if pluginID == source {
			pluginID = ""
		} else if plugin, ok := pluginsByID[pluginID]; ok {
			scope = plugin.scope
		}
		records = append(records, ExtensionInventoryRecord{
			ID:   extensionSubjectID("skill", source, skill.Name),
			Name: skill.Name,
			Kind: extensions.KindSkill,
			Provenance: extensions.Provenance{
				Kind:     extensions.KindSkill,
				Source:   source,
				Scope:    scope,
				Path:     skill.Path,
				PluginID: pluginID,
				Official: strings.EqualFold(source, "bundled"),
			},
			State: ExtensionStateActive,
		})
	}
	for _, template := range s.rt.AgentTemplates {
		source := strings.TrimSpace(template.Source)
		records = append(records, ExtensionInventoryRecord{
			ID:   extensionSubjectID("agent_template", source, template.Name),
			Name: template.Name,
			Kind: extensions.KindAgentTemplate,
			Provenance: extensions.Provenance{
				Kind:   extensions.KindAgentTemplate,
				Source: source,
				Scope:  normalizedExtensionScope(source, template.Path, s.rt.RootDir),
				Path:   template.Path,
			},
			State: ExtensionStateReadOnly,
		})
	}

	for _, item := range s.rt.Plugins {
		scope := normalizedExtensionScope(item.Source, item.ManifestPath, s.rt.RootDir)
		pluginSource := pluginManifestSource(item.ManifestPath)
		records = append(records, ExtensionInventoryRecord{
			ID:          extensionSubjectID("plugin", scope, item.ID),
			Name:        item.ID,
			Description: item.Description,
			Kind:        extensions.KindPlugin,
			Provenance: extensions.Provenance{
				Kind:     extensions.KindPlugin,
				Source:   pluginSource,
				Scope:    scope,
				Path:     item.ManifestPath,
				PluginID: item.ID,
				Official: item.Official,
			},
			State:                ExtensionStateReadOnly,
			RequestedPermissions: cloneSortedStrings(item.RequestedPermissions),
			UnsupportedFields:    cloneSortedStrings(item.UnsupportedFields),
		})

		mcpNames := sortedMapKeys(item.MCPServers)
		for _, name := range mcpNames {
			server := item.MCPServers[name]
			permissions := executablePermissions(item.RequestedPermissions, server.Command != "", server.URL != "", false)
			subjectID := extensionSubjectID("mcp", "plugin", item.ID, name)
			fingerprint, _ := extensions.Fingerprint(extensions.ExecutableSpec{
				Command:     server.Command,
				Args:        server.Args,
				URL:         server.URL,
				Env:         server.Env,
				Headers:     server.Headers,
				Permissions: permissions,
			})
			state, grantScope := executableExtensionState(grants, subjectID, fingerprint, server.Enabled, item.Official)
			records = append(records, ExtensionInventoryRecord{
				ID:                   subjectID,
				Name:                 name,
				Kind:                 extensions.KindMCP,
				Provenance:           extensions.Provenance{Kind: extensions.KindMCP, Source: "plugin:" + item.ID, Scope: scope, Path: item.ManifestPath, PluginID: item.ID, Official: item.Official},
				State:                state,
				Executable:           true,
				Fingerprint:          fingerprint,
				GrantScope:           grantScope,
				RequestedPermissions: permissions,
			})
		}
		appendHookInventory := func(event string, index int, entry config.HookEntry) {
			permissions := executablePermissions(item.RequestedPermissions, entry.Command != "", false, strings.EqualFold(strings.TrimSpace(entry.Type), "prompt"))
			subjectID := extensionSubjectID("hook", "plugin", item.ID, event, strconv.Itoa(index))
			fingerprint := hookFingerprint(event, index, entry, permissions)
			state, grantScope := executableExtensionState(grants, subjectID, fingerprint, nil, item.Official)
			records = append(records, ExtensionInventoryRecord{
				ID:                   subjectID,
				Name:                 event,
				Kind:                 extensions.KindHook,
				Provenance:           extensions.Provenance{Kind: extensions.KindHook, Source: "plugin:" + item.ID, Scope: scope, Path: item.ManifestPath, PluginID: item.ID, Official: item.Official},
				State:                state,
				Executable:           true,
				Fingerprint:          fingerprint,
				GrantScope:           grantScope,
				RequestedPermissions: permissions,
			})
		}
		for _, event := range sortedMapKeys(item.Hooks) {
			for index, entry := range item.Hooks[event] {
				appendHookInventory(event, index, entry)
			}
		}
	}

	for _, name := range sortedMapKeys(cfg.MCPServers) {
		server := cfg.MCPServers[name]
		permissions := executablePermissions(nil, server.Command != "", server.URL != "", false)
		subjectID := extensionSubjectID("mcp", "config", name)
		fingerprint, _ := extensions.Fingerprint(extensions.ExecutableSpec{
			Command: server.Command, Args: server.Args, URL: server.URL, Env: server.Env, Headers: server.Headers, Permissions: permissions,
		})
		state, grantScope := executableExtensionState(grants, subjectID, fingerprint, server.Enabled, false)
		records = append(records, ExtensionInventoryRecord{
			ID: subjectID, Name: name, Kind: extensions.KindMCP,
			Provenance: extensions.Provenance{Kind: extensions.KindMCP, Source: "wuu_config", Scope: "project", Path: s.rt.ConfigPath},
			State:      state, Executable: true, Fingerprint: fingerprint, GrantScope: grantScope, RequestedPermissions: permissions,
		})
	}
	for _, event := range sortedMapKeys(cfg.Hooks) {
		for index, entry := range cfg.Hooks[event] {
			permissions := executablePermissions(nil, entry.Command != "", false, strings.EqualFold(strings.TrimSpace(entry.Type), "prompt"))
			subjectID := extensionSubjectID("hook", "config", event, strconv.Itoa(index))
			fingerprint := hookFingerprint(event, index, entry, permissions)
			state, grantScope := executableExtensionState(grants, subjectID, fingerprint, nil, false)
			records = append(records, ExtensionInventoryRecord{
				ID: subjectID, Name: event, Kind: extensions.KindHook,
				Provenance: extensions.Provenance{Kind: extensions.KindHook, Source: "wuu_config", Scope: "project", Path: s.rt.ConfigPath},
				State:      state, Executable: true, Fingerprint: fingerprint, GrantScope: grantScope, RequestedPermissions: permissions,
			})
		}
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Kind != records[j].Kind {
			return records[i].Kind < records[j].Kind
		}
		if records[i].Name != records[j].Name {
			return records[i].Name < records[j].Name
		}
		return records[i].ID < records[j].ID
	})
	return records
}

func (s *Server) currentExtensionConfig() config.Config {
	if s == nil || s.rt == nil {
		return config.Config{}
	}
	if cfg, _, err := s.rt.LoadEffectiveConfig(); err == nil {
		return cfg
	}
	return config.Config{}
}

func executableExtensionState(grants extensions.Settings, subjectID, fingerprint string, enabled *bool, official bool) (ExtensionState, extensions.GrantScope) {
	if enabled != nil && !*enabled {
		return ExtensionStateRejected, ""
	}
	if official {
		return ExtensionStateGranted, ""
	}
	if grant, ok := grants.FindGrant(subjectID, fingerprint); ok {
		return ExtensionStateGranted, grant.Scope
	}
	if _, ok := grants.Grants[subjectID]; ok {
		return ExtensionStateChanged, ""
	}
	return ExtensionStatePending, ""
}

func hookFingerprint(event string, index int, entry config.HookEntry, permissions []string) string {
	fingerprint, _ := extensions.Fingerprint(extensions.ExecutableSpec{
		Command:     entry.Command,
		Args:        []string{event, strconv.Itoa(index), entry.Matcher, entry.Type, entry.Prompt, entry.Model, strconv.Itoa(entry.Timeout)},
		Permissions: permissions,
	})
	return fingerprint
}

func executablePermissions(declared []string, process, network, model bool) []string {
	permissions := append([]string(nil), declared...)
	if process {
		permissions = append(permissions, "process.spawn")
	}
	if network {
		permissions = append(permissions, "network.connect")
	}
	if model {
		permissions = append(permissions, "model.invoke")
	}
	return cloneSortedStrings(permissions)
}

func normalizedExtensionScope(source, path, projectRoot string) string {
	if strings.EqualFold(strings.TrimSpace(source), "project") {
		return "project"
	}
	if strings.EqualFold(strings.TrimSpace(source), "bundled") {
		return "bundled"
	}
	if path != "" && projectRoot != "" {
		if rel, err := filepath.Rel(projectRoot, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "project"
		}
	}
	return "user"
}

func pluginManifestSource(path string) string {
	slashPath := filepath.ToSlash(path)
	switch {
	case strings.Contains(slashPath, "/.codex-plugin/"):
		return "codex"
	case strings.Contains(slashPath, "/.claude-plugin/"):
		return "claude"
	default:
		return "wuu"
	}
}

func extensionSubjectID(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, ":")
}

func cloneSortedStrings(values []string) []string {
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
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func extensionSurfaceSummary(active bool, count int) ExtensionSurfaceTrustSummary {
	return ExtensionSurfaceTrustSummary{
		Allowed:      true,
		Active:       active,
		Count:        count,
		KnownTools:   count,
		VisibleTools: count,
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

func (s *Server) handleConfigAdvancedUpdate(req Request) error {
	var params ConfigAdvancedUpdateParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if s.hasRunningThread() {
		return s.writeResponse(req.ID, nil, errors.New("cannot change advanced settings while a turn is running"))
	}
	if s.rt == nil {
		return s.writeResponse(req.ID, nil, errors.New("runtime is not initialized"))
	}
	if err := config.UpdateAdvancedRuntime(s.rt.ConfigPath, s.rt.ProviderName, config.AdvancedRuntimeUpdate{
		MaxSteps:                params.MaxSteps,
		MaxContextTokens:        params.MaxContextTokens,
		Temperature:             params.Temperature,
		CompactThresholdPct:     params.CompactThresholdPct,
		CompactKeepRecentTokens: params.CompactKeepRecentTokens,
		DisableAutoCompact:      params.DisableAutoCompact,
		ProviderContextWindow:   params.ProviderContextWindow,
	}); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	cfg, _, err := s.rt.LoadEffectiveConfig()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	providerCfg, resolvedName, err := cfg.ResolveProvider(s.rt.ProviderName)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	providerCfg = s.withCachedCodexModels(resolvedName, providerCfg)
	ruleProviderName, ruleProviderCfg := modelcatalog.EnrichProvider(resolvedName, providerCfg, s.rt.Model)
	apiModel := modelcatalog.APIModel(ruleProviderCfg, s.rt.Model)
	roleSelections, err := modelroles.Resolve(cfg, modelroles.ResolveOptions{
		ProviderName:   resolvedName,
		ProviderConfig: providerCfg,
		Model:          s.rt.Model,
		Effort:         s.currentEffort(),
		Variant:        s.currentVariant(),
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	s.rt.ModelRoles = roleSelections
	modelBudget := runtime.ResolveModelBudget(s.rt.Model, ruleProviderCfg, cfg.Agent.MaxContextTokens)
	s.rt.ModelBudget = modelBudget
	workerBudget := runtime.ResolveModelBudget(roleSelections.Worker.Model, roleSelections.Worker.RuleProviderConfig, cfg.Agent.MaxContextTokens)
	s.rt.WorkerModelBudget = workerBudget
	if s.rt.StreamRunner != nil {
		s.rt.StreamRunner.MaxSteps = cfg.Agent.MaxSteps
		s.rt.StreamRunner.Temperature = cfg.Agent.Temperature
		s.rt.StreamRunner.CompactThresholdPct = cfg.Agent.CompactThresholdPct
		s.rt.StreamRunner.CompactKeepRecentTokens = cfg.Agent.CompactKeepRecentTokens
		s.rt.StreamRunner.DisableAutoCompact = cfg.Agent.DisableAutoCompact
		s.rt.StreamRunner.ContextWindowOverride = modelBudget.ContextWindowTokens
		s.rt.StreamRunner.MaxInputTokens = modelBudget.InputLimitTokens
		s.rt.StreamRunner.OutputReserveTokens = modelBudget.OutputReserveTokens
		s.rt.StreamRunner.CompactThresholdTokens = modelBudget.CompactThresholdTokens
	}
	s.updateRootAgentControlWorkerDefaults()
	s.updateIdleThreadAdvancedRuntime()
	if s.rt.Toolkit != nil {
		s.rt.Toolkit.ConfigureSurfaceForProviderModel(ruleProviderName, apiModel, true)
	}
	return s.writeResponse(req.ID, ConfigAdvancedUpdateResult{
		AdvancedSettings: s.currentAdvancedSettingsSummary(),
		Providers:        s.providerSummaries(),
	}, nil)
}

func (s *Server) handleConfigGeneralUpdate(req Request) error {
	var params ConfigGeneralUpdateParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if s.hasRunningThread() {
		return s.writeResponse(req.ID, nil, errors.New("cannot change general settings while a turn is running"))
	}
	if s.rt == nil {
		return s.writeResponse(req.ID, nil, errors.New("runtime is not initialized"))
	}
	if err := config.UpdateGeneralSettings(s.rt.ConfigPath, config.GeneralSettingsUpdate{
		AppendSystemPrompt: params.AppendSystemPrompt,
		MemoryDisable:      params.MemoryDisable,
		MCPEnabledToggles:  params.MCPEnabledToggles,
	}); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	cfg, _, err := s.rt.LoadEffectiveConfig()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	systemPrompt := s.rt.ApplyGeneralConfig(cfg, os.Getenv("HOME"))
	s.resetThreadRuntimesForGeneralSettings(systemPrompt)
	return s.writeResponse(req.ID, ConfigGeneralUpdateResult{
		GeneralSettings: s.currentGeneralSettingsSummary(),
	}, nil)
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
	cfg, _, err := s.rt.LoadEffectiveConfig()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	var providerCfg config.ProviderConfig
	var resolvedName string
	// providerTypeValue is populated below when creatingProvider is true;
	// it is hoisted out of the if-block so the later CreateProviderRuntime
	// call can pass it as the new optional provider-type argument.
	providerTypeValue := ""
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
		authToken := strings.TrimSpace(stringValue(params.AuthToken))
		if baseURL == "" {
			return s.writeResponse(req.ID, nil, errors.New("base_url is required"))
		}
		if apiKey == "" && authToken == "" {
			return s.writeResponse(req.ID, nil, errors.New("api_key or auth_token is required"))
		}
		// Resolve the requested provider type. Empty / missing falls back to
		// "openai-compatible" (preserves the historical default). Unknown
		// values are rejected so the UI cannot accidentally create a
		// provider that the factory cannot service.
		providerTypeValue = "openai-compatible"
		if params.Type != nil {
			requested := strings.ToLower(strings.TrimSpace(*params.Type))
			switch requested {
			case "", "openai", "openai-compatible":
				if requested != "" {
					providerTypeValue = requested
				}
			case "anthropic", "claude", "anthropic-official":
				providerTypeValue = requested
			default:
				return s.writeResponse(req.ID, nil, fmt.Errorf("unsupported provider type %q", requested))
			}
		}
		providerCfg = config.ProviderConfig{
			Type:      providerTypeValue,
			BaseURL:   baseURL,
			APIKey:    apiKey,
			AuthToken: authToken,
			Model:     model,
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
	if connectionLocked && (params.BaseURL != nil || strings.TrimSpace(stringValue(params.APIKey)) != "" || strings.TrimSpace(stringValue(params.AuthToken)) != "") {
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
	authTokenForConfig := params.AuthToken
	authKeyForStore := ""
	authTokenForStore := ""
	if params.APIKey != nil {
		apiKey := strings.TrimSpace(*params.APIKey)
		if apiKey != "" {
			connectionChanged = true
			authKeyForStore = apiKey
			providerCfg.APIKey = apiKey
			providerCfg.APIKeyEnv = ""
			providerCfg.AuthToken = ""
			providerCfg.AuthTokenEnv = ""
			empty := ""
			apiKeyForConfig = &empty
		} else {
			apiKeyForConfig = nil
		}
	}
	if params.AuthToken != nil {
		authToken := strings.TrimSpace(*params.AuthToken)
		if authToken != "" {
			connectionChanged = true
			authTokenForStore = authToken
			providerCfg.AuthToken = authToken
			providerCfg.AuthTokenEnv = ""
			providerCfg.APIKey = ""
			providerCfg.APIKeyEnv = ""
			empty := ""
			authTokenForConfig = &empty
			apiKeyForConfig = &empty
		} else {
			authTokenForConfig = nil
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
		store, storeErr := authstorage.ForHome(os.Getenv("HOME"))
		if storeErr != nil {
			return s.writeResponse(req.ID, nil, storeErr)
		}
		if err := store.Update(resolvedName, func(credentials *authstorage.Credentials) {
			credentials.Type = "api_key"
			credentials.APIKey = authKeyForStore
			credentials.AuthToken = ""
		}); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	if authTokenForStore != "" {
		store, storeErr := authstorage.ForHome(os.Getenv("HOME"))
		if storeErr != nil {
			return s.writeResponse(req.ID, nil, storeErr)
		}
		if err := store.Update(resolvedName, func(credentials *authstorage.Credentials) {
			credentials.Type = "auth_token"
			credentials.AuthToken = authTokenForStore
			credentials.APIKey = ""
		}); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	if creatingProvider {
		err = config.CreateProviderRuntime(s.rt.ConfigPath, resolvedName, &providerTypeValue, model, params.BaseURL, apiKeyForConfig, authTokenForConfig, effortForConfig, variantForConfig, params.PermissionMode)
	} else {
		err = config.UpdateProviderRuntime(s.rt.ConfigPath, resolvedName, model, params.BaseURL, apiKeyForConfig, authTokenForConfig, effortForConfig, variantForConfig, params.PermissionMode)
	}
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}

	previousRuntimeProvider := s.rt.ProviderName
	s.rt.ProviderName = resolvedName
	s.rt.Model = model
	if client != nil || len(s.rt.ReadinessIssues) == 0 {
		s.rt.ReadinessIssues = nil
	}
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
		workerClient, workerErr := providerfactory.BuildStreamClient(roleSelections.Worker.RuleProviderConfig, roleSelections.Worker.Provider)
		if workerErr != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("build worker client: %w", workerErr))
		}
		s.rt.WorkerClient = workerClient
	}
	if s.rt.Toolkit != nil {
		s.rt.Toolkit.ConfigureSurfaceForProviderModel(ruleProviderName, apiModel, true)
	}
	if params.PermissionMode != nil {
		permissions := config.ResolvedPermissions{Mode: config.NormalizePermissionMode(*params.PermissionMode)}
		s.rt.Permissions = permissions
		if s.rt.Toolkit != nil {
			runtime.ConfigureToolkitPermissions(s.rt.Toolkit, s.rt.Permissions)
		}
	}
	systemPrompt := s.rt.RefreshSystemPrompt(resolvedName, apiModel)
	modelBudget := runtime.ResolveModelBudget(model, ruleProviderCfg, cfg.Agent.MaxContextTokens)
	s.rt.ModelBudget = modelBudget
	workerBudget := runtime.ResolveModelBudget(roleSelections.Worker.Model, roleSelections.Worker.RuleProviderConfig, cfg.Agent.MaxContextTokens)
	s.rt.WorkerModelBudget = workerBudget
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
		s.rt.StreamRunner.CompactThresholdTokens = modelBudget.CompactThresholdTokens
	}
	s.updateRootAgentControlWorkerDefaults()
	s.updateThreadRuntimeForModelUpdate(resolvedName, ruleProviderName, model, apiModel, systemPrompt)

	modelProfile, toolSurface := s.currentModelSurfaceSummaries()
	return s.writeResponse(req.ID, ConfigModelUpdateResult{
		Provider:         resolvedName,
		Model:            model,
		Effort:           effort,
		Variant:          selection.Variant,
		Permissions:      s.currentPermissionSummary(),
		ExtensionTrust:   s.currentExtensionTrustSummary(),
		ModelProfile:     modelProfile,
		ToolSurface:      toolSurface,
		ModelRoles:       s.currentModelRoleSummaries(),
		Providers:        s.providerSummaries(),
		AdvancedSettings: s.currentAdvancedSettingsSummary(),
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
	cfg, _, err := s.rt.LoadEffectiveConfig()
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
		BaseURL:               providerCfg.BaseURL,
		APIKey:                explicitProviderAPIKey(providerCfg),
		Headers:               providerCfg.Headers,
		ReuseCodexCredentials: providerCfg.ReuseCodexCredentials,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	models, err := client.Models(ctx)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	s.cacheCodexModels(resolvedName, models)
	out := codexModelSummaries(models)
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

func (s *Server) handleConfigProviderRemove(req Request) error {
	var params ConfigProviderRemoveParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	providerName := strings.TrimSpace(params.Provider)
	if providerName == "" {
		return s.writeResponse(req.ID, nil, errors.New("provider is required"))
	}
	cfg, _, err := s.rt.LoadEffectiveConfig()
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	existing, resolvedName, lookupErr := cfg.ResolveProvider(providerName)
	if lookupErr != nil {
		return s.writeResponse(req.ID, nil, lookupErr)
	}
	if isCodexProviderType(existing.Type) {
		return s.writeResponse(req.ID, nil, fmt.Errorf("provider %q is managed by OpenAI OAuth and cannot be removed", providerName))
	}
	if threadID, inUse := s.runningTurnUsingProvider(resolvedName); inUse {
		return s.writeResponse(req.ID, nil, fmt.Errorf("cannot remove provider %q while it is used by a running turn in thread %q", resolvedName, threadID))
	}
	// Refuse to remove the last remaining provider when no fallback is
	// supplied — config.RemoveProvider would surface a similar error but
	// doing the check here gives the renderer a clearer 4xx-style error.
	if len(cfg.Providers) <= 1 && strings.TrimSpace(params.FallbackProvider) == "" {
		return s.writeResponse(req.ID, nil, errors.New("cannot remove the last configured provider"))
	}
	removedWasDefault := cfg.DefaultProvider == providerName
	authStore, authErr := authstorage.ForHome(os.Getenv("HOME"))
	if authErr != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("prepare credential cleanup: %w", authErr))
	}
	if authErr := authStore.DeleteProvider(resolvedName); authErr != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("remove provider credential: %w", authErr))
	}
	newDefault, removeErr := config.RemoveProvider(s.rt.ConfigPath, providerName, params.FallbackProvider, params.FallbackModel)
	if removeErr != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("credential removed but provider config update failed; re-enter the credential before retrying: %w", removeErr))
	}
	if !removedWasDefault {
		// Removed an inactive provider; nothing in the runtime needs
		// reconfiguring because the active selection is unchanged.
		return s.writeResponse(req.ID, ConfigProviderRemoveResult{
			Provider:         s.rt.ProviderName,
			Model:            s.rt.Model,
			Variant:          s.currentVariant(),
			Permissions:      s.currentPermissionSummary(),
			ExtensionTrust:   s.currentExtensionTrustSummary(),
			ModelProfile:     nil,
			ToolSurface:      nil,
			ModelRoles:       s.currentModelRoleSummaries(),
			Providers:        s.providerSummaries(),
			AdvancedSettings: s.currentAdvancedSettingsSummary(),
		}, nil)
	}
	// Default provider changed — reload the config and rebuild the
	// runtime for the new default. Falling back to the removed
	// provider's model when the caller did not supply fallbackModel
	// keeps the renderer from showing an empty model after the swap.
	fallbackName := strings.TrimSpace(newDefault)
	if fallbackName == "" {
		return s.writeResponse(req.ID, nil, errors.New("provider removal left no default_provider configured"))
	}
	fallbackModel := strings.TrimSpace(params.FallbackModel)
	if fallbackModel == "" {
		if fb, ok := cfg.Providers[fallbackName]; ok {
			fallbackModel = fb.Model
		}
	}
	synthetic := req
	synthetic.Method = MethodConfigModelUpdate
	synthetic.Params, err = json.Marshal(ConfigModelUpdateParams{
		Provider: fallbackName,
		Model:    fallbackModel,
	})
	if err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("marshal fallback params: %w", err))
	}
	return s.handleConfigModelUpdate(synthetic)
}

func (s *Server) runningTurnUsingProvider(providerName string) (string, bool) {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, th := range s.threads {
		th.mu.Lock()
		running := th.running
		runningProvider := strings.TrimSpace(th.runningProviderName)
		if running && runningProvider == "" {
			runningProvider = strings.TrimSpace(th.ModelProvider)
		}
		threadID := th.ID
		th.mu.Unlock()
		if running && runningProvider == providerName {
			return threadID, true
		}
	}
	return "", false
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

func (s *Server) updateThreadRuntimeForModelUpdate(providerName, ruleProviderName, model, apiModel, systemPrompt string) {
	update := threadRuntimeUpdate{
		ProviderName:     providerName,
		RuleProviderName: ruleProviderName,
		Model:            model,
		APIModel:         apiModel,
		SystemPrompt:     systemPrompt,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, th := range s.threads {
		th.mu.Lock()
		s.updateThreadRuntimeLocked(th, update)
		th.mu.Unlock()
	}
}

func (s *Server) resetThreadRuntimesForGeneralSettings(systemPrompt string) {
	if s == nil || s.rt == nil {
		return
	}
	var release []*threadState
	s.mu.Lock()
	for _, th := range s.threads {
		th.mu.Lock()
		if strings.TrimSpace(systemPrompt) != "" {
			th.History = replaceBaseSystemPrompt(th.History, systemPrompt)
			if th.PersistHistory {
				if err := rewriteChatHistory(s.rt.SessionDir, th.ID, th.History); err != nil {
					providers.DebugLogf("rewrite thread %q system prompt after general settings update: %v", th.ID, err)
				}
			}
		}
		if th.execRuntime != nil {
			release = append(release, th)
		}
		th.mu.Unlock()
	}
	s.mu.Unlock()
	for _, th := range release {
		releaseThreadRuntime(th)
	}
}

func (s *Server) updateThreadRuntimeLocked(th *threadState, update threadRuntimeUpdate) {
	th.ModelProvider = update.ProviderName
	th.Model = update.Model
	if strings.TrimSpace(update.SystemPrompt) != "" {
		th.History = replaceBaseSystemPrompt(th.History, update.SystemPrompt)
		if th.PersistHistory {
			if err := rewriteChatHistory(s.rt.SessionDir, th.ID, th.History); err != nil {
				providers.DebugLogf("rewrite thread %q system prompt after model update: %v", th.ID, err)
			}
		}
	}
	if th.running {
		pending := update
		th.pendingRuntimeUpdate = &pending
		return
	}
	s.applyThreadRuntimeUpdateLocked(th, update)
}

func (s *Server) applyPendingThreadRuntime(th *threadState) {
	if th == nil {
		return
	}
	th.mu.Lock()
	defer th.mu.Unlock()
	if th.running || th.pendingRuntimeUpdate == nil {
		return
	}
	s.applyPendingThreadRuntimeLocked(th)
}

func (s *Server) applyPendingThreadRuntimeLocked(th *threadState) {
	if th == nil || th.pendingRuntimeUpdate == nil {
		return
	}
	update := *th.pendingRuntimeUpdate
	s.applyThreadRuntimeUpdateLocked(th, update)
}

func (s *Server) applyThreadRuntimeUpdateLocked(th *threadState, update threadRuntimeUpdate) {
	th.pendingRuntimeUpdate = nil
	if th.execRuntime == nil {
		return
	}
	if th.execRuntime.StreamRunner != nil && s.rt != nil && s.rt.StreamRunner != nil {
		th.execRuntime.StreamRunner.Client = s.rt.StreamRunner.Client
		th.execRuntime.StreamRunner.Model = update.Model
		th.execRuntime.StreamRunner.APIModel = update.APIModel
		th.execRuntime.StreamRunner.UpdateSystemPromptWithSections(update.SystemPrompt, s.rt.StreamRunner.SystemPromptSections)
		th.execRuntime.StreamRunner.Effort = s.currentEffort()
		th.execRuntime.StreamRunner.Variant = s.currentVariant()
		th.execRuntime.StreamRunner.ProviderOptions = s.currentProviderOptions()
		th.execRuntime.StreamRunner.ContextWindowOverride = s.rt.StreamRunner.ContextWindowOverride
		th.execRuntime.StreamRunner.MaxInputTokens = s.rt.StreamRunner.MaxInputTokens
		th.execRuntime.StreamRunner.OutputReserveTokens = s.rt.StreamRunner.OutputReserveTokens
		th.execRuntime.StreamRunner.CompactThresholdTokens = s.rt.StreamRunner.CompactThresholdTokens
		th.execRuntime.StreamRunner.CompactThresholdPct = s.rt.StreamRunner.CompactThresholdPct
		th.execRuntime.StreamRunner.CompactKeepRecentTokens = s.rt.StreamRunner.CompactKeepRecentTokens
		th.execRuntime.StreamRunner.DisableAutoCompact = s.rt.StreamRunner.DisableAutoCompact
		th.execRuntime.StreamRunner.MaxSteps = s.rt.StreamRunner.MaxSteps
		th.execRuntime.StreamRunner.Temperature = s.rt.StreamRunner.Temperature
	}
	th.execRuntime.ModelBudget = s.rt.ModelBudget
	th.execRuntime.WorkerModelBudget = s.rt.WorkerModelBudget
	if th.execRuntime.AgentControl != nil {
		th.execRuntime.AgentControl.UpdateWorkerDefaults(
			s.rt.WorkerClient,
			s.rt.ModelRoles.Worker.APIModel,
			s.currentWorkerManagerOptions(),
		)
	}
	if th.execRuntime.Toolkit != nil {
		th.execRuntime.Toolkit.ConfigureSurfaceForProviderModel(update.RuleProviderName, update.APIModel, true)
		if s.rt != nil {
			runtime.ConfigureToolkitPermissions(th.execRuntime.Toolkit, s.rt.Permissions)
		}
	}
}

func (s *Server) updateIdleThreadAdvancedRuntime() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, th := range s.threads {
		th.mu.Lock()
		if !th.running && th.execRuntime != nil {
			if th.execRuntime.StreamRunner != nil && s.rt != nil && s.rt.StreamRunner != nil {
				th.execRuntime.StreamRunner.MaxSteps = s.rt.StreamRunner.MaxSteps
				th.execRuntime.StreamRunner.Temperature = s.rt.StreamRunner.Temperature
				th.execRuntime.StreamRunner.ContextWindowOverride = s.rt.StreamRunner.ContextWindowOverride
				th.execRuntime.StreamRunner.MaxInputTokens = s.rt.StreamRunner.MaxInputTokens
				th.execRuntime.StreamRunner.OutputReserveTokens = s.rt.StreamRunner.OutputReserveTokens
				th.execRuntime.StreamRunner.CompactThresholdTokens = s.rt.StreamRunner.CompactThresholdTokens
				th.execRuntime.StreamRunner.CompactThresholdPct = s.rt.StreamRunner.CompactThresholdPct
				th.execRuntime.StreamRunner.CompactKeepRecentTokens = s.rt.StreamRunner.CompactKeepRecentTokens
				th.execRuntime.StreamRunner.DisableAutoCompact = s.rt.StreamRunner.DisableAutoCompact
			}
			if s.rt != nil {
				th.execRuntime.ModelBudget = s.rt.ModelBudget
				th.execRuntime.WorkerModelBudget = s.rt.WorkerModelBudget
				if th.execRuntime.AgentControl != nil {
					th.execRuntime.AgentControl.UpdateWorkerDefaults(
						s.rt.WorkerClient,
						s.rt.ModelRoles.Worker.APIModel,
						s.currentWorkerManagerOptions(),
					)
				}
			}
		}
		th.mu.Unlock()
	}
}

func (s *Server) updateRootAgentControlWorkerDefaults() {
	if s == nil || s.rt == nil || s.rt.AgentControl == nil {
		return
	}
	s.rt.AgentControl.UpdateWorkerDefaults(
		s.rt.WorkerClient,
		s.rt.ModelRoles.Worker.APIModel,
		s.currentWorkerManagerOptions(),
	)
}

func (s *Server) currentWorkerManagerOptions() subagent.ManagerOptions {
	if s == nil || s.rt == nil {
		return subagent.ManagerOptions{}
	}
	compactPct := 0.0
	keepRecent := 0
	disableCompact := false
	temperature := 0.0
	if s.rt.StreamRunner != nil {
		compactPct = s.rt.StreamRunner.CompactThresholdPct
		keepRecent = s.rt.StreamRunner.CompactKeepRecentTokens
		disableCompact = s.rt.StreamRunner.DisableAutoCompact
		temperature = s.rt.StreamRunner.Temperature
	}
	return subagent.ManagerOptions{
		DefaultEffort:           s.rt.ModelRoles.Worker.LegacyEffort,
		DefaultProviderOptions:  s.rt.ModelRoles.Worker.ProviderOptions,
		ContextWindowOverride:   s.rt.WorkerModelBudget.ContextWindowTokens,
		MaxInputTokens:          s.rt.WorkerModelBudget.InputLimitTokens,
		OutputReserveTokens:     s.rt.WorkerModelBudget.OutputReserveTokens,
		CompactThresholdTokens:  s.rt.WorkerModelBudget.CompactThresholdTokens,
		Temperature:             temperature,
		CompactThresholdPct:     compactPct,
		CompactKeepRecentTokens: keepRecent,
		DisableAutoCompact:      disableCompact,
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
	cfg, _, err := s.rt.LoadEffectiveConfig()
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
	if isCodexProviderType(provider.Type) && provider.ReuseCodexCredentials {
		return true
	}
	store, err := authstorage.ForHome(home)
	if err != nil {
		return false
	}
	credentials, err := store.Get(name)
	return err == nil && (strings.TrimSpace(credentials.APIKey) != "" ||
		strings.TrimSpace(credentials.AuthToken) != "" || strings.TrimSpace(credentials.AccessToken) != "")
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
	} else if isCodexProviderType(provider.Type) {
		if matched, ok := modelcatalog.CodexSubscriptionCatalogProvider(codexCatalogModelIDs(provider)...); ok {
			catalogProvider = matched
			hasCatalog = true
			ruleProvider = modelcatalog.MergeProvider(provider, matched)
		}
	}
	current := strings.TrimSpace(provider.Model)
	selectedRuleProviderName := ruleProviderName
	selectedRuleProvider := ruleProvider
	if current != "" {
		selectedRuleProviderName, selectedRuleProvider = modelcatalog.EnrichProvider(providerName, provider, current)
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
		modelRuleProviderName := ruleProviderName
		modelRuleProvider := ruleProvider
		if id == current {
			modelRuleProviderName = selectedRuleProviderName
			modelRuleProvider = selectedRuleProvider
		}
		model = modelRuleProvider.Models[id]
		capabilities, behavior := modelroles.BuildFacts(modelRuleProviderName, modelRuleProvider, id)
		models[id] = ProviderModelSummary{
			ID:               id,
			DisplayName:      strings.TrimSpace(model.Name),
			DefaultEffort:    strings.TrimSpace(model.DefaultEffort),
			DefaultVariant:   strings.TrimSpace(model.DefaultVariant),
			SupportedEfforts: modelvariant.SupportedEffortsForProvider(modelRuleProviderName, modelRuleProvider, id, model),
			Variants:         providerVariantSummaries(modelvariant.SummariesForProvider(modelRuleProviderName, modelRuleProvider, id)),
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
	if current != "" {
		if _, ok := models[current]; !ok {
			model := selectedRuleProvider.Models[current]
			capabilities, behavior := modelroles.BuildFacts(selectedRuleProviderName, selectedRuleProvider, current)
			models[current] = ProviderModelSummary{
				ID:               current,
				DisplayName:      strings.TrimSpace(model.Name),
				DefaultEffort:    strings.TrimSpace(model.DefaultEffort),
				DefaultVariant:   strings.TrimSpace(model.DefaultVariant),
				SupportedEfforts: modelvariant.SupportedEffortsForProvider(selectedRuleProviderName, selectedRuleProvider, current, model),
				Variants:         providerVariantSummaries(modelvariant.SummariesForProvider(selectedRuleProviderName, selectedRuleProvider, current)),
				Capabilities:     capabilities,
				Behavior:         behavior,
				Source:           "selected",
			}
		}
	}
	out := make([]ProviderModelSummary, 0, len(models))
	for _, model := range models {
		modelRuleProviderName := ruleProviderName
		modelRuleProvider := ruleProvider
		if model.ID == current {
			modelRuleProviderName = selectedRuleProviderName
			modelRuleProvider = selectedRuleProvider
		}
		if len(model.Variants) == 0 {
			model.Variants = providerVariantSummaries(modelvariant.SummariesForProvider(modelRuleProviderName, modelRuleProvider, model.ID))
		}
		if len(model.SupportedEfforts) == 0 {
			model.SupportedEfforts = modelvariant.EffortIDs(modelVariantSummaries(model.Variants))
		}
		if model.DefaultVariant == "" && model.DefaultEffort != "" {
			model.DefaultVariant = model.DefaultEffort
		}
		if model.DefaultVariant == "" {
			model.DefaultVariant = modelvariant.DefaultVariantForProvider(modelRuleProviderName, modelRuleProvider, model.ID)
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
		out[id] = codexLiveModelConfig(model)
		for _, alias := range modelcatalog.CodexSubscriptionModelAliases(id) {
			aliasID := strings.TrimSpace(alias.ID)
			if aliasID == "" {
				continue
			}
			cfg := modelcatalog.MergeModelConfig(modelcatalog.ModelConfig(alias), out[id])
			applyCodexSubscriptionLimit(aliasID, &cfg)
			out[aliasID] = cfg
		}
	}
	return out
}

func codexLiveModelConfig(model codex.ModelInfo) config.ProviderModelConfig {
	id := strings.TrimSpace(model.Slug)
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
	return cfg
}

func codexCatalogModelIDs(provider config.ProviderConfig) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(provider.Models)+1)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	add(provider.Model)
	for id, model := range provider.Models {
		add(id)
		add(model.ID)
	}
	return out
}

func codexModelSummaries(models []codex.ModelInfo) []CodexModelSummary {
	out := make([]CodexModelSummary, 0, len(models))
	seen := map[string]bool{}
	for _, model := range models {
		id := strings.TrimSpace(model.Slug)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, codexModelSummaryFromLive(model))
		for _, alias := range modelcatalog.CodexSubscriptionModelAliases(id) {
			aliasID := strings.TrimSpace(alias.ID)
			if aliasID == "" || seen[aliasID] {
				continue
			}
			seen[aliasID] = true
			out = append(out, codexModelSummaryFromCatalogAlias(alias, model))
		}
	}
	return out
}

func codexModelSummaryFromLive(model codex.ModelInfo) CodexModelSummary {
	return CodexModelSummary{
		Slug:                  model.Slug,
		DisplayName:           model.DisplayName,
		DefaultReasoningLevel: model.DefaultReasoningLevel,
		SupportedReasoning:    append([]string(nil), model.SupportedReasoning...),
		SupportedInAPI:        model.SupportedInAPI,
	}
}

func codexModelSummaryFromCatalogAlias(alias modelcatalog.Model, base codex.ModelInfo) CodexModelSummary {
	efforts := normalizedCodexEfforts(base.SupportedReasoning)
	if len(efforts) == 0 {
		efforts = codexReasoningEffortsFromCatalog(alias)
	}
	return CodexModelSummary{
		Slug:                  strings.TrimSpace(alias.ID),
		DisplayName:           strings.TrimSpace(alias.Name),
		DefaultReasoningLevel: base.DefaultReasoningLevel,
		SupportedReasoning:    efforts,
		SupportedInAPI:        base.SupportedInAPI,
	}
}

func codexReasoningEffortsFromCatalog(model modelcatalog.Model) []string {
	for _, option := range model.ReasoningOptions {
		if kind, _ := option["type"].(string); strings.TrimSpace(kind) != "effort" {
			continue
		}
		switch values := option["values"].(type) {
		case []any:
			out := make([]string, 0, len(values))
			for _, value := range values {
				if effort, ok := value.(string); ok {
					out = append(out, effort)
				}
			}
			return normalizedCodexEfforts(out)
		case []string:
			return normalizedCodexEfforts(values)
		}
	}
	return nil
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
