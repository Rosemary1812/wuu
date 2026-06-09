package tools

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/blueberrycongee/wuu/internal/providers"
)

type ToolSearchTool struct {
	toolkit *Toolkit
}

func NewToolSearchTool(toolkit *Toolkit) *ToolSearchTool {
	return &ToolSearchTool{toolkit: toolkit}
}

func (t *ToolSearchTool) Name() string            { return "tool_search" }
func (t *ToolSearchTool) IsReadOnly() bool        { return false }
func (t *ToolSearchTool) IsConcurrencySafe() bool { return false }

func (t *ToolSearchTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "tool_search",
		Description: "Search deferred tools and expose matching tools for the next model turn.\n\n" +
			"Use this when you need a tool that is not currently visible, especially MCP tools or low-frequency scheduling tools.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search terms describing the tool you need.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of tools to expose. Default 8, maximum 20.",
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t *ToolSearchTool) Execute(_ context.Context, argsJSON string) (string, error) {
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", errors.New("tool_search requires query")
	}
	if t.toolkit == nil {
		return "", errors.New("tool_search is not attached to a toolkit")
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 8
	}
	if limit > 20 {
		limit = 20
	}

	matches := t.toolkit.searchDeferredTools(query, limit)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match.Name)
	}
	t.toolkit.activateDeferredTools(names...)

	return mustJSON(map[string]any{
		"action":            "tool_search",
		"query":             query,
		"matched":           len(matches),
		"exposed_tools":     names,
		"tools":             matches,
		"visible_next_turn": len(matches) > 0,
	})
}

type toolSearchMatch struct {
	Name            string   `json:"name"`
	Kind            ToolKind `json:"kind"`
	Description     string   `json:"description"`
	ReadOnly        bool     `json:"read_only"`
	ConcurrencySafe bool     `json:"concurrency_safe"`
	Score           int      `json:"score"`
}

func (t *Toolkit) searchDeferredTools(query string, limit int) []toolSearchMatch {
	tokens := searchTokens(query)
	if len(tokens) == 0 || limit <= 0 {
		return nil
	}
	var matches []toolSearchMatch
	for _, tool := range t.allKnownTools() {
		if t.toolExposure(tool.Name()) != ToolExposureDeferred {
			continue
		}
		def := tool.Definition()
		score := scoreToolSearchMatch(def, tokens)
		if score == 0 {
			continue
		}
		desc, _ := truncate(def.Description, 600)
		matches = append(matches, toolSearchMatch{
			Name:            tool.Name(),
			Kind:            classifyToolKind(tool.Name()),
			Description:     desc,
			ReadOnly:        tool.IsReadOnly(),
			ConcurrencySafe: tool.IsConcurrencySafe(),
			Score:           score,
		})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Name < matches[j].Name
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

func (t *Toolkit) allKnownTools() []Tool {
	all := t.registry.All()
	if t.mcpManager != nil {
		for _, tool := range t.mcpManager.AllTools() {
			all = append(all, tool)
		}
	}
	return all
}

func (t *Toolkit) activateDeferredTools(names ...string) {
	if len(names) == 0 {
		return
	}
	t.exposureMu.Lock()
	defer t.exposureMu.Unlock()
	if t.activatedDeferredTools == nil {
		t.activatedDeferredTools = make(map[string]struct{}, len(names))
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || !isDeferredByDefault(name) || t.isToolDisabled(name) {
			continue
		}
		t.activatedDeferredTools[name] = struct{}{}
	}
}

func (t *Toolkit) isDeferredToolActive(name string) bool {
	t.exposureMu.RLock()
	defer t.exposureMu.RUnlock()
	_, ok := t.activatedDeferredTools[name]
	return ok
}

func (t *Toolkit) cloneActivatedDeferredTools() map[string]struct{} {
	t.exposureMu.RLock()
	defer t.exposureMu.RUnlock()
	if len(t.activatedDeferredTools) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(t.activatedDeferredTools))
	for name := range t.activatedDeferredTools {
		out[name] = struct{}{}
	}
	return out
}

func searchTokens(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, " \t\n\r.,:;()[]{}\"'")
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func scoreToolSearchMatch(def providers.ToolDefinition, tokens []string) int {
	name := strings.ToLower(def.Name)
	haystack := name + " " + strings.ToLower(def.Description)
	score := 0
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if strings.Contains(name, token) {
			score += 20
		}
		if strings.Contains(haystack, token) {
			score += 10
		}
	}
	return score
}
