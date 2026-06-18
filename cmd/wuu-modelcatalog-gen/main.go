package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/modelcatalog"
)

type rawCatalog map[string]rawProvider

type rawProvider struct {
	ID     string              `json:"id"`
	Name   string              `json:"name"`
	API    string              `json:"api"`
	NPM    string              `json:"npm"`
	Env    []string            `json:"env"`
	Models map[string]rawModel `json:"models"`
}

type rawModel struct {
	ID               string                      `json:"id"`
	Name             string                      `json:"name"`
	Family           string                      `json:"family"`
	Status           string                      `json:"status"`
	ReleaseDate      string                      `json:"release_date"`
	Reasoning        bool                        `json:"reasoning"`
	ReasoningOptions []map[string]any            `json:"reasoning_options"`
	Attachment       *bool                       `json:"attachment"`
	ToolCall         *bool                       `json:"tool_call"`
	StructuredOutput *bool                       `json:"structured_output"`
	Temperature      *bool                       `json:"temperature"`
	Interleaved      any                         `json:"interleaved"`
	Modalities       *modelcatalog.Modalities    `json:"modalities"`
	Cost             map[string]any              `json:"cost"`
	Provider         *modelcatalog.ModelProvider `json:"provider"`
	Limit            *modelcatalog.Limit         `json:"limit"`
	Options          map[string]any              `json:"options"`
	Headers          map[string]string           `json:"headers"`
	Experimental     *rawExperimental            `json:"experimental"`
}

type rawExperimental struct {
	Modes map[string]rawMode `json:"modes"`
}

type rawMode struct {
	Cost     map[string]any   `json:"cost"`
	Provider *rawModeProvider `json:"provider"`
}

type rawModeProvider struct {
	Body    map[string]any    `json:"body"`
	Headers map[string]string `json:"headers"`
}

func main() {
	input := flag.String("input", "https://models.dev/api.json", "models.dev api.json URL or local path")
	output := flag.String("output", "internal/modelcatalog/models_dev_catalog.json", "output catalog JSON path")
	flag.Parse()

	data, err := readInput(*input)
	if err != nil {
		fatal(err)
	}
	var raw rawCatalog
	if err := json.Unmarshal(data, &raw); err != nil {
		fatal(fmt.Errorf("decode %s: %w", *input, err))
	}
	catalog := buildCatalog(raw)
	encoded, err := json.Marshal(catalog)
	if err != nil {
		fatal(err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(*output, encoded, 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func readInput(input string) ([]byte, error) {
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		client := http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(input)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("fetch %s: %s", input, resp.Status)
		}
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(input)
}

type outputCatalog struct {
	Providers []modelcatalog.Provider `json:"providers"`
}

func buildCatalog(raw rawCatalog) outputCatalog {
	providerIDs := make([]string, 0, len(raw))
	for id := range raw {
		providerIDs = append(providerIDs, id)
	}
	sort.Strings(providerIDs)

	out := outputCatalog{Providers: make([]modelcatalog.Provider, 0, len(providerIDs))}
	for _, providerID := range providerIDs {
		provider := raw[providerID]
		if provider.ID == "" {
			provider.ID = providerID
		}
		models := buildModels(provider)
		if len(models) == 0 {
			continue
		}
		out.Providers = append(out.Providers, modelcatalog.Provider{
			ID:     provider.ID,
			Name:   provider.Name,
			API:    provider.API,
			NPM:    provider.NPM,
			Env:    append([]string(nil), provider.Env...),
			Models: models,
		})
	}
	return out
}

func buildModels(provider rawProvider) []modelcatalog.Model {
	modelIDs := make([]string, 0, len(provider.Models))
	for id := range provider.Models {
		modelIDs = append(modelIDs, id)
	}
	sort.Strings(modelIDs)

	models := make([]modelcatalog.Model, 0, len(modelIDs))
	for _, key := range modelIDs {
		model := provider.Models[key]
		if model.ID == "" {
			model.ID = key
		}
		base := convertModel(provider, model, model.ID, model.ID, model.Name)
		if filtered(provider.ID, base.ID, base.Status) {
			continue
		}
		models = append(models, base)
		for _, mode := range sortedModeIDs(model.Experimental) {
			modeModel := modeModel(base, mode, model.Experimental.Modes[mode])
			if filtered(provider.ID, modeModel.ID, modeModel.Status) {
				continue
			}
			models = append(models, modeModel)
		}
	}
	return models
}

func sortedModeIDs(experimental *rawExperimental) []string {
	if experimental == nil || len(experimental.Modes) == 0 {
		return nil
	}
	out := make([]string, 0, len(experimental.Modes))
	for mode := range experimental.Modes {
		out = append(out, mode)
	}
	sort.Strings(out)
	return out
}

func filtered(providerID, modelID, status string) bool {
	switch strings.TrimSpace(status) {
	case "deprecated", "alpha":
		return true
	}
	if modelID == "gpt-5-chat-latest" {
		switch providerID {
		case "openai", "github-copilot", "openrouter":
			return true
		}
	}
	return providerID == "openrouter" && modelID == "openai/gpt-5-chat"
}

func convertModel(provider rawProvider, model rawModel, id, apiID, name string) modelcatalog.Model {
	status := strings.TrimSpace(model.Status)
	if status == "active" {
		status = ""
	}
	out := modelcatalog.Model{
		ID:               id,
		APIID:            apiIDForModel(id, apiID),
		Name:             name,
		Family:           model.Family,
		Status:           status,
		ReleaseDate:      model.ReleaseDate,
		Reasoning:        model.Reasoning,
		ReasoningOptions: cloneOptionList(model.ReasoningOptions),
		Attachment:       cloneBool(model.Attachment),
		ToolCall:         cloneBool(model.ToolCall),
		StructuredOutput: cloneBool(model.StructuredOutput),
		Temperature:      cloneBool(model.Temperature),
		Interleaved:      cloneAny(model.Interleaved),
		Modalities:       cloneModalities(model.Modalities),
		Cost:             cloneOptions(model.Cost),
		Provider:         modelProvider(provider, model),
		Limit:            cloneLimit(model.Limit),
		Options:          cloneOptions(model.Options),
		Headers:          cloneHeaders(model.Headers),
	}
	return out
}

func apiIDForModel(id, apiID string) string {
	if strings.TrimSpace(apiID) == "" || apiID == id {
		return ""
	}
	return apiID
}

func modelProvider(provider rawProvider, model rawModel) *modelcatalog.ModelProvider {
	if model.Provider == nil {
		return nil
	}
	out := *model.Provider
	if out.API == provider.API {
		out.API = ""
	}
	if out.NPM == provider.NPM {
		out.NPM = ""
	}
	if strings.TrimSpace(out.API) == "" && strings.TrimSpace(out.NPM) == "" {
		return nil
	}
	return &out
}

func modeModel(base modelcatalog.Model, mode string, opts rawMode) modelcatalog.Model {
	out := base
	out.ID = base.ID + "-" + mode
	out.APIID = base.ID
	if base.Name != "" {
		out.Name = base.Name + " " + strings.ToUpper(mode[:1]) + mode[1:]
	}
	if len(opts.Cost) > 0 {
		out.Cost = mergeOptions(base.Cost, opts.Cost)
	}
	if opts.Provider != nil {
		if len(opts.Provider.Body) > 0 {
			out.Options = camelCaseOptions(opts.Provider.Body)
		}
		if len(opts.Provider.Headers) > 0 {
			out.Headers = cloneHeaders(opts.Provider.Headers)
		}
	}
	return out
}

func camelCaseOptions(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[snakeToLowerCamel(key)] = cloneAny(value)
	}
	return out
}

func snakeToLowerCamel(input string) string {
	var b strings.Builder
	upperNext := false
	for _, r := range input {
		if r == '_' {
			upperNext = true
			continue
		}
		if upperNext && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		upperNext = false
		b.WriteRune(r)
	}
	return b.String()
}

func mergeOptions(base, override map[string]any) map[string]any {
	out := cloneOptions(base)
	if out == nil {
		out = map[string]any{}
	}
	for key, value := range override {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneBool(input *bool) *bool {
	if input == nil {
		return nil
	}
	value := *input
	return &value
}

func cloneLimit(input *modelcatalog.Limit) *modelcatalog.Limit {
	if input == nil {
		return nil
	}
	out := *input
	return &out
}

func cloneModalities(input *modelcatalog.Modalities) *modelcatalog.Modalities {
	if input == nil {
		return nil
	}
	return &modelcatalog.Modalities{
		Input:  append([]string(nil), input.Input...),
		Output: append([]string(nil), input.Output...),
	}
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

func cloneOptions(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneOptionList(input []map[string]any) []map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(input))
	for _, item := range input {
		out = append(out, cloneOptions(item))
	}
	return out
}

func cloneAny(input any) any {
	if input == nil {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return input
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var out any
	if err := decoder.Decode(&out); err != nil {
		return input
	}
	return out
}
