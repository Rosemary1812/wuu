package modelcatalog

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"strings"
	"sync"

	"github.com/blueberrycongee/wuu/internal/config"
)

//go:embed models_dev_catalog.json
var catalogJSON []byte

type Provider struct {
	ID     string  `json:"id"`
	Name   string  `json:"name,omitempty"`
	API    string  `json:"api,omitempty"`
	NPM    string  `json:"npm,omitempty"`
	Models []Model `json:"models"`
}

type Model struct {
	ID          string            `json:"id"`
	APIID       string            `json:"api_id,omitempty"`
	Name        string            `json:"name,omitempty"`
	ReleaseDate string            `json:"release_date,omitempty"`
	Reasoning   bool              `json:"reasoning"`
	Provider    *ModelProvider    `json:"provider,omitempty"`
	Limit       *Limit            `json:"limit,omitempty"`
	Options     map[string]any    `json:"options,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

type ModelProvider struct {
	API string `json:"api,omitempty"`
	NPM string `json:"npm,omitempty"`
}

type Limit struct {
	Context int `json:"context,omitempty"`
	Input   int `json:"input,omitempty"`
	Output  int `json:"output,omitempty"`
}

type catalogData struct {
	Providers []Provider `json:"providers"`
}

var (
	loadOnce sync.Once
	loaded   catalogData
	loadErr  error
)

func Providers() ([]Provider, error) {
	loadOnce.Do(func() {
		loadErr = json.Unmarshal(catalogJSON, &loaded)
	})
	if loadErr != nil {
		return nil, loadErr
	}
	return loaded.Providers, nil
}

func MatchProvider(providerName string, provider config.ProviderConfig) (Provider, bool) {
	providers, err := Providers()
	if err != nil {
		return Provider{}, false
	}

	endpoints := endpointCandidates(provider)
	for _, endpoint := range endpoints {
		for _, item := range providers {
			if normalizeEndpoint(item.API) == endpoint {
				return item, true
			}
		}
	}

	for _, candidate := range providerIDCandidates(providerName, provider) {
		for _, item := range providers {
			if normalizeID(item.ID) == candidate {
				return item, true
			}
		}
	}

	return Provider{}, false
}

func EnrichProvider(providerName string, provider config.ProviderConfig, modelIDs ...string) (string, config.ProviderConfig) {
	catalogProvider, ok := MatchProvider(providerName, provider)
	if !ok {
		return providerName, provider
	}
	return catalogProvider.ID, MergeProvider(provider, catalogProvider, modelIDs...)
}

func MergeProvider(provider config.ProviderConfig, catalogProvider Provider, modelIDs ...string) config.ProviderConfig {
	out := provider
	if strings.TrimSpace(out.API) == "" {
		out.API = catalogProvider.API
	}
	if strings.TrimSpace(out.NPM) == "" {
		out.NPM = catalogProvider.NPM
	}

	modelSet := make(map[string]bool, len(modelIDs))
	for _, modelID := range modelIDs {
		modelID = strings.TrimSpace(modelID)
		if modelID != "" {
			modelSet[modelID] = true
		}
	}
	includeAll := len(modelSet) == 0

	models := make(map[string]config.ProviderModelConfig, len(out.Models)+len(catalogProvider.Models))
	for id, model := range out.Models {
		models[id] = model
	}
	for _, model := range catalogProvider.Models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if !includeAll && !modelSet[id] {
			continue
		}
		models[id] = MergeModelConfig(models[id], ModelConfig(model))
	}
	if len(models) > 0 {
		out.Models = models
	}
	if len(modelSet) == 1 && out.ContextWindow == 0 {
		for id := range modelSet {
			if model := models[id]; model.ContextWindow > 0 {
				out.ContextWindow = model.ContextWindow
			}
		}
	}
	if len(modelSet) == 1 {
		for id := range modelSet {
			if model := models[id]; len(model.Headers) > 0 {
				out.Headers = mergeHeaders(out.Headers, model.Headers)
			}
		}
	}
	return out
}

func ModelConfig(model Model) config.ProviderModelConfig {
	apiID := strings.TrimSpace(model.APIID)
	if apiID == "" {
		apiID = strings.TrimSpace(model.ID)
	}
	reasoning := model.Reasoning
	out := config.ProviderModelConfig{
		ID:          apiID,
		Name:        strings.TrimSpace(model.Name),
		ReleaseDate: strings.TrimSpace(model.ReleaseDate),
		Reasoning:   &reasoning,
	}
	if model.Provider != nil && (strings.TrimSpace(model.Provider.API) != "" || strings.TrimSpace(model.Provider.NPM) != "") {
		out.Provider = &config.ProviderModelProviderConfig{
			API: strings.TrimSpace(model.Provider.API),
			NPM: strings.TrimSpace(model.Provider.NPM),
		}
	}
	if model.Limit != nil {
		out.Limit = &config.ProviderModelLimitConfig{
			Context: model.Limit.Context,
			Input:   model.Limit.Input,
			Output:  model.Limit.Output,
		}
		if model.Limit.Context > 0 {
			out.ContextWindow = model.Limit.Context
		}
	}
	out.Options = cloneOptions(model.Options)
	out.Headers = cloneHeaders(model.Headers)
	return out
}

func APIModel(provider config.ProviderConfig, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if modelCfg, ok := provider.Models[model]; ok {
		if apiID := strings.TrimSpace(modelCfg.ID); apiID != "" {
			return apiID
		}
	}
	return model
}

func MergeModelConfig(primary, fallback config.ProviderModelConfig) config.ProviderModelConfig {
	out := primary
	if strings.TrimSpace(out.ID) == "" {
		out.ID = fallback.ID
	}
	if strings.TrimSpace(out.Name) == "" {
		out.Name = fallback.Name
	}
	if strings.TrimSpace(out.ReleaseDate) == "" {
		out.ReleaseDate = fallback.ReleaseDate
	}
	if out.Reasoning == nil {
		out.Reasoning = fallback.Reasoning
	}
	if out.Provider == nil {
		out.Provider = fallback.Provider
	} else if fallback.Provider != nil {
		provider := *out.Provider
		if strings.TrimSpace(provider.API) == "" {
			provider.API = fallback.Provider.API
		}
		if strings.TrimSpace(provider.NPM) == "" {
			provider.NPM = fallback.Provider.NPM
		}
		out.Provider = &provider
	}
	if out.Limit == nil {
		out.Limit = fallback.Limit
	} else if fallback.Limit != nil {
		limit := *out.Limit
		if limit.Context == 0 {
			limit.Context = fallback.Limit.Context
		}
		if limit.Input == 0 {
			limit.Input = fallback.Limit.Input
		}
		if limit.Output == 0 {
			limit.Output = fallback.Limit.Output
		}
		out.Limit = &limit
	}
	if out.ContextWindow == 0 {
		out.ContextWindow = fallback.ContextWindow
	}
	if len(out.Options) == 0 {
		out.Options = cloneOptions(fallback.Options)
	}
	if len(out.Headers) == 0 {
		out.Headers = cloneHeaders(fallback.Headers)
	}
	return out
}

func cloneOptions(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneHeaders(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func mergeHeaders(base, override map[string]string) map[string]string {
	out := cloneHeaders(base)
	if len(override) == 0 {
		return out
	}
	if out == nil {
		out = make(map[string]string, len(override))
	}
	for key, value := range override {
		out[key] = value
	}
	return out
}

func providerIDCandidates(providerName string, provider config.ProviderConfig) []string {
	raw := []string{providerName}
	if allowProviderTypeCatalogMatch(provider) {
		raw = append(raw, provider.Type)
	}
	aliases := map[string]string{
		"anthropic-official": "anthropic",
		"bedrock":            "amazon-bedrock",
		"claude":             "anthropic",
		"gemini":             "google",
	}
	var out []string
	seen := map[string]bool{}
	for _, value := range raw {
		normalized := normalizeID(value)
		if normalized == "" || normalized == "openai-compatible" {
			continue
		}
		if alias := aliases[normalized]; alias != "" {
			normalized = alias
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func allowProviderTypeCatalogMatch(provider config.ProviderConfig) bool {
	endpoints := endpointCandidates(provider)
	if len(endpoints) == 0 {
		return true
	}
	providerType := normalizeID(provider.Type)
	for _, endpoint := range endpoints {
		if isOfficialEndpointForType(providerType, endpoint) {
			return true
		}
	}
	return false
}

func isOfficialEndpointForType(providerType, endpoint string) bool {
	switch providerType {
	case "openai", "codex":
		return endpointHost(endpoint) == "api.openai.com"
	case "anthropic", "claude", "anthropic-official":
		return endpointHost(endpoint) == "api.anthropic.com"
	case "gemini", "google":
		return strings.HasSuffix(endpointHost(endpoint), "generativelanguage.googleapis.com")
	default:
		return false
	}
}

func endpointCandidates(provider config.ProviderConfig) []string {
	raw := []string{provider.API, provider.BaseURL}
	var out []string
	seen := map[string]bool{}
	for _, value := range raw {
		normalized := normalizeEndpoint(value)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func normalizeID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

func normalizeEndpoint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		parsed.Host = strings.ToLower(parsed.Host)
		parsed.RawQuery = ""
		parsed.Fragment = ""
		value = parsed.String()
	}
	return strings.TrimRight(strings.ToLower(value), "/")
}

func endpointHost(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Host)
}
