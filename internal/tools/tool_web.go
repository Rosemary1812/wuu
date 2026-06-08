package tools

import (
	"context"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// ---------------------------------------------------------------------------
// web_search
// ---------------------------------------------------------------------------

type WebSearchTool struct{ env *Env }

func NewWebSearchTool(env *Env) *WebSearchTool { return &WebSearchTool{env: env} }

func (t *WebSearchTool) Name() string            { return "web_search" }
func (t *WebSearchTool) IsReadOnly() bool        { return true }
func (t *WebSearchTool) IsConcurrencySafe() bool { return true }

func (t *WebSearchTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "web_search",
		Description: "Search the web using DuckDuckGo. Returns evidence metadata plus titles, URLs, and snippets.\n\n" +
			"Usage:\n" +
			"- Use for finding documentation, examples, or researching APIs\n" +
			"- Treat results as web evidence, not repository truth; match claims against the repo's dependency versions before editing\n" +
			"- Prefer official documentation or primary sources when choosing a URL to fetch\n" +
			"- Returns up to 10 results with title, URL, and snippet\n" +
			"- For fetching a specific URL's content, use web_fetch instead",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query.",
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	return webSearchExecute(ctx, argsJSON)
}

// ---------------------------------------------------------------------------
// web_fetch
// ---------------------------------------------------------------------------

type WebFetchTool struct{ env *Env }

func NewWebFetchTool(env *Env) *WebFetchTool { return &WebFetchTool{env: env} }

func (t *WebFetchTool) Name() string            { return "web_fetch" }
func (t *WebFetchTool) IsReadOnly() bool        { return true }
func (t *WebFetchTool) IsConcurrencySafe() bool { return true }

func (t *WebFetchTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "web_fetch",
		Description: "Fetch a URL and return evidence metadata plus readable text.\n\n" +
			"Usage:\n" +
			"- Treat fetched content as web evidence; match claims against the repo's dependency versions before editing\n" +
			"- Prefer official documentation, changelogs, or primary sources for implementation decisions\n" +
			"- HTML is automatically converted to readable text (scripts, nav, footer stripped)\n" +
			"- JSON responses are pretty-printed\n" +
			"- Content is truncated at 1MB\n" +
			"- 30 second timeout",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "URL to fetch.",
				},
			},
			"required": []string{"url"},
		},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	return webFetchExecute(ctx, argsJSON)
}
