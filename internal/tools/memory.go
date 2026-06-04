package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/memory/store"
	"github.com/blueberrycongee/wuu/internal/providers"
)

// Memory tool naming. Three separate tools keep each one focused on
// a single operation; a "command" dispatcher (store | forget | ...) was
// the alternative but it forces the model to remember subcommand names
// and to branch on its own, which is exactly what tool boundaries exist
// to avoid. The names match the in-tree Hermes-equivalent contract.
const (
	memoryStoreName  = "memory_store"
	memoryRecallName = "memory_recall"
	memorySearchName = "memory_search"

	// memoryDefaultLimit is the cap the tools apply when the model
	// does not specify one. It matches the existing RecallSearch
	// tooling's typical cap and keeps the response budget predictable.
	memoryDefaultLimit = 20
)

// memoryToolDeps gathers the parts of Env the memory tools actually
// read. Keeping a small struct of dependencies makes the tools
// trivially testable without constructing the full Env (which would
// require AgentControl, ProcessMgr, Skills, and other subsystems).
type memoryToolDeps struct {
	Memory store.Provider
}

func depsForMemory(env *Env) memoryToolDeps {
	if env == nil {
		return memoryToolDeps{}
	}
	return memoryToolDeps{Memory: env.Memory}
}

// errMemoryUnavailable is returned by every memory tool when the
// Env has no Provider configured. It is deliberately an exported
// sentinel that callers can errors.Is against if they want to
// silently degrade the prompt.
var errMemoryUnavailable = fmt.Errorf("memory: no Provider configured on Env; set Env.Memory before registering memory tools")

// memoryEntryDTO is the JSON-friendly view of a store.Entry. The
// on-disk struct uses time.Time which json.Marshal renders as a
// quoted is what RFC3339 string by default — that we want — but we
// keep an explicit DTO so the tool output shape is stable even if
// the store package adds or renames fields later.
type memoryEntryDTO struct {
	ID        string   `json:"id"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags,omitempty"`
	Source    string   `json:"source,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

func toMemoryEntryDTO(e store.Entry) memoryEntryDTO {
	out := memoryEntryDTO{
		ID:        string(e.ID),
		Content:   e.Content,
		Tags:      append([]string(nil), e.Tags...),
		Source:    string(e.Source),
	}
	if !e.CreatedAt.IsZero() {
		out.CreatedAt = e.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !e.UpdatedAt.IsZero() {
		out.UpdatedAt = e.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

// allowedMemorySources mirrors store.Source* constants but is
// duplicated here so the JSON schema is self-contained and tooling
// that inspects the schema does not need to import the store
// package. The values must stay in sync with store/store.go.
var allowedMemorySources = []string{
	string(store.SourceUser),
	string(store.SourceAssistant),
	string(store.SourceTool),
	string(store.SourceSystem),
	string(store.SourceImport),
}

// isAllowedMemorySource reports whether s is a recognized Source
// value. Empty is treated as "unset" and resolved to a default
// (SourceAssistant) at write time, so the schema does not require
// the model to send a source.
func isAllowedMemorySource(s string) bool {
	if s == "" {
		return true
	}
	for _, v := range allowedMemorySources {
		if v == s {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// memory_store
// -----------------------------------------------------------------------------

// memoryStoreArgs is the decoded shape of memory_store's input.
type memoryStoreArgs struct {
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
	Source  string   `json:"source,omitempty"`
	ID      string   `json:"id,omitempty"`
}

// memoryStoreTool implements the Tool interface for memory_store.
// memoryStoreTool implements the Tool interface for memory_store.
// It holds a *Env rather than a captured Provider so the toolkit can
// swap providers late (kit.SetMemory) without re-registering the tool.
type memoryStoreTool struct {
	env *Env
}

// NewMemoryStoreTool returns a tool that inserts a new entry into the
// configured memory Provider. It is safe to call even when env.Memory
// is nil — Execute will return a clear error in that case.
func NewMemoryStoreTool(env *Env) *memoryStoreTool {
	return &memoryStoreTool{env: env}
}

func (t *memoryStoreTool) Name() string { return memoryStoreName }

// IsReadOnly is false: memory_store mutates persistent state.
func (t *memoryStoreTool) IsReadOnly() bool { return false }

// IsConcurrencySafe: store.Provider documents its methods as safe for
// concurrent use; the tool itself only delegates.
func (t *memoryStoreTool) IsConcurrencySafe() bool { return true }

// Definition returns the model-facing contract for memory_store. The
// Description is intentionally explicit about when NOT to call the
// tool (transient scratch, derivable from files) so the model does
// not bloat the long-term store with low-value writes.
func (t *memoryStoreTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: memoryStoreName,
		Description: "Persist a fact, preference, or durable observation to long-term memory. " +
			"Use this for information the user has asked you to remember across sessions, " +
			"recurring project conventions, or decisions that should survive a context " +
			"compaction. Do NOT use for transient scratch notes, in-progress work, or " +
			"anything derivable from files in the repo. The stored entry is searchable " +
			"by future invocations of memory_recall and memory_search.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"content"},
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "The durable fact or observation to remember. Write it as a self-contained statement that will still make sense when read in isolation, weeks from now.",
				},
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional categorization tags. Useful for grouping related entries (e.g. project name, subsystem). Tags are matched with logical AND by memory_recall/memory_search.",
				},
				"source": map[string]any{
					"type":        "string",
					"enum":        allowedMemorySources,
					"description": "Who originated this fact. Defaults to \"assistant\" when omitted.",
				},
				"id": map[string]any{
					"type":        "string",
					"description": "Optional caller-supplied identifier. Omit to let the Provider assign one; the returned id is what future recall/search calls will reference.",
				},
			},
		},
	}
}

func (t *memoryStoreTool) Execute(ctx context.Context, args string) (string, error) {
	mem := depsForMemory(t.env).Memory
	if mem == nil {
		return "", errMemoryUnavailable
	}
	var a memoryStoreArgs
	if err := decodeArgs(args, &a); err != nil {
		return "", fmt.Errorf("memory_store: %w", err)
	}
	content := strings.TrimSpace(a.Content)
	if content == "" {
		return "", fmt.Errorf("memory_store: content is required and must be non-empty")
	}
	if !isAllowedMemorySource(a.Source) {
		return "", fmt.Errorf("memory_store: source %q is not one of %v", a.Source, allowedMemorySources)
	}
	source := store.Source(a.Source)
	if source == "" {
		source = store.SourceAssistant
	}
	entry := store.Entry{
		ID:      store.ID(a.ID),
		Content: content,
		Tags:    append([]string(nil), a.Tags...),
		Source:  source,
	}
	id, err := mem.Store(ctx, entry)
	if err != nil {
		return "", fmt.Errorf("memory_store: %w", err)
	}
	out := map[string]any{
		"id":      string(id),
		"stored":  true,
		"source":  string(source),
		"tags":    entry.Tags,
		"length":  len(content),
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("memory_store: encode result: %w", err)
	}
	return string(b), nil
}

// -----------------------------------------------------------------------------
// memory_recall
// -----------------------------------------------------------------------------

// memoryRecallArgs is the decoded shape of memory_recall's input.
type memoryRecallArgs struct {
	Tags  []string `json:"tags,omitempty"`
	Limit int      `json:"limit,omitempty"`
	Since string   `json:"since,omitempty"` // RFC3339
}

// memoryRecallTool implements the Tool interface for memory_recall.
// See memoryStoreTool for why this holds *Env instead of a captured
// Provider snapshot.
type memoryRecallTool struct {
	env *Env
}

// NewMemoryRecallTool returns a tool that lists recent entries from
// the configured memory Provider, optionally filtered by tags and a
// time floor.
func NewMemoryRecallTool(env *Env) *memoryRecallTool {
	return &memoryRecallTool{env: env}
}

func (t *memoryRecallTool) Name() string { return memoryRecallName }

func (t *memoryRecallTool) IsReadOnly() bool        { return true }
func (t *memoryRecallTool) IsConcurrencySafe() bool { return true }

// Definition: keep the description short. memory_recall is the cheap
// index-only path; if the model needs text matching, it should reach
// for memory_search instead.
func (t *memoryRecallTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: memoryRecallName,
		Description: "List recent entries from long-term memory in newest-first order. " +
			"Use this to surface what you already know about the user, the project, " +
			"or a tag namespace. For free-text lookup use memory_search.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional tag filter; only entries carrying ALL listed tags are returned.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     200,
					"description": "Maximum number of entries to return. Defaults to 20.",
				},
				"since": map[string]any{
					"type":        "string",
					"format":      "date-time",
					"description": "Optional RFC3339 timestamp. Only entries created at or after this time are returned.",
				},
			},
		},
	}
}

func (t *memoryRecallTool) Execute(ctx context.Context, args string) (string, error) {
	mem := depsForMemory(t.env).Memory
	if mem == nil {
		return "", errMemoryUnavailable
	}
	var a memoryRecallArgs
	if err := decodeArgs(args, &a); err != nil {
		return "", fmt.Errorf("memory_recall: %w", err)
	}
	limit := a.Limit
	if limit <= 0 {
		limit = memoryDefaultLimit
	}
	if limit > 200 {
		limit = 200
	}
	var since time.Time
	if a.Since != "" {
		parsed, err := time.Parse(time.RFC3339, a.Since)
		if err != nil {
			// Tolerate RFC3339Nano too — the model sometimes emits
			// sub-second precision. We do not advertise that in the
			// schema because most callers should not care.
			parsed, err = time.Parse(time.RFC3339Nano, a.Since)
			if err != nil {
				return "", fmt.Errorf("memory_recall: since must be RFC3339: %w", err)
			}
		}
		since = parsed
	}
	entries, err := mem.Recall(ctx, store.RecallQuery{
		Tags:  append([]string(nil), a.Tags...),
		Limit: limit,
		Since: since,
	})
	if err != nil {
		return "", fmt.Errorf("memory_recall: %w", err)
	}
	dtos := make([]memoryEntryDTO, 0, len(entries))
	for _, e := range entries {
		dtos = append(dtos, toMemoryEntryDTO(e))
	}
	out := map[string]any{
		"count":   len(dtos),
		"entries": dtos,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("memory_recall: encode result: %w", err)
	}
	return string(b), nil
}

// -----------------------------------------------------------------------------
// memory_search
// -----------------------------------------------------------------------------

// memorySearchArgs is the decoded shape of memory_search's input.
type memorySearchArgs struct {
	Query string   `json:"query"`
	Tags  []string `json:"tags,omitempty"`
	Limit int      `json:"limit,omitempty"`
}

// memorySearchTool implements the Tool interface for memory_search.
// See memoryStoreTool for why this holds *Env instead of a captured
// Provider snapshot.
type memorySearchTool struct {
	env *Env
}

// NewMemorySearchTool returns a tool that performs text search over
// the configured memory Provider.
func NewMemorySearchTool(env *Env) *memorySearchTool {
	return &memorySearchTool{env: env}
}

func (t *memorySearchTool) Name() string { return memorySearchName }

func (t *memorySearchTool) IsReadOnly() bool        { return true }
func (t *memorySearchTool) IsConcurrencySafe() bool { return true }

func (t *memorySearchTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: memorySearchName,
		Description: "Search long-term memory by free-text query. Returns entries whose " +
			"content contains the query (substring match). Use this when memory_recall's " +
			"tag/recency filter is not enough — e.g. when the model needs to look up a " +
			"specific term, error string, or earlier decision.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"minLength":   1,
					"description": "Substring to look for inside entry content. Case-sensitive.",
				},
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional tag filter; only entries carrying ALL listed tags are considered.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     200,
					"description": "Maximum number of entries to return. Defaults to 20.",
				},
			},
		},
	}
}

func (t *memorySearchTool) Execute(ctx context.Context, args string) (string, error) {
	mem := depsForMemory(t.env).Memory
	if mem == nil {
		return "", errMemoryUnavailable
	}
	var a memorySearchArgs
	if err := decodeArgs(args, &a); err != nil {
		return "", fmt.Errorf("memory_search: %w", err)
	}
	q := strings.TrimSpace(a.Query)
	if q == "" {
		return "", fmt.Errorf("memory_search: query is required and must be non-empty")
	}
	limit := a.Limit
	if limit <= 0 {
		limit = memoryDefaultLimit
	}
	if limit > 200 {
		limit = 200
	}
	entries, err := mem.Search(ctx, store.SearchQuery{
		Text:  q,
		Tags:  append([]string(nil), a.Tags...),
		Limit: limit,
	})
	if err != nil {
		return "", fmt.Errorf("memory_search: %w", err)
	}
	dtos := make([]memoryEntryDTO, 0, len(entries))
	for _, e := range entries {
		dtos = append(dtos, toMemoryEntryDTO(e))
	}
	out := map[string]any{
		"query":   q,
		"count":   len(dtos),
		"entries": dtos,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("memory_search: encode result: %w", err)
	}
	return string(b), nil
}

// NewMemoryTools returns the full set of memory tools in a fixed
// order. Wiring this into the toolkit is the job of the caller; this
// function only constructs the tools.
func NewMemoryTools(env *Env) []Tool {
	return []Tool{
		NewMemoryStoreTool(env),
		NewMemoryRecallTool(env),
		NewMemorySearchTool(env),
	}
}
