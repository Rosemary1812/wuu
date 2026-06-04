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

const (
	readMemoryName  = "read_memory"
	writeMemoryName = "write_memory"

	memoryDefaultLimit = 20
	memoryMaxLimit     = 200
)

var errMemoryUnavailable = fmt.Errorf("memory: no Provider configured on Env; set Env.Memory before registering memory tools")

func memoryProvider(env *Env) store.Provider {
	if env == nil {
		return nil
	}
	return env.Memory
}

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
		ID:      string(e.ID),
		Content: e.Content,
		Tags:    append([]string(nil), e.Tags...),
		Source:  string(e.Source),
	}
	if !e.CreatedAt.IsZero() {
		out.CreatedAt = e.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !e.UpdatedAt.IsZero() {
		out.UpdatedAt = e.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

var allowedMemorySources = []string{
	string(store.SourceUser),
	string(store.SourceAssistant),
	string(store.SourceTool),
	string(store.SourceSystem),
	string(store.SourceImport),
}

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

// write_memory

type writeMemoryArgs struct {
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
	Source  string   `json:"source,omitempty"`
	ID      string   `json:"id,omitempty"`
}

type writeMemoryTool struct {
	env *Env
}

func NewWriteMemoryTool(env *Env) *writeMemoryTool {
	return &writeMemoryTool{env: env}
}

func (t *writeMemoryTool) Name() string { return writeMemoryName }

func (t *writeMemoryTool) IsReadOnly() bool        { return false }
func (t *writeMemoryTool) IsConcurrencySafe() bool { return true }

func (t *writeMemoryTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: writeMemoryName,
		Description: "Persist a durable fact, preference, or project convention to long-term memory. " +
			"Use only for information that should survive across sessions or context compaction. " +
			"Do not store transient scratch notes, in-progress work, or facts that can be read from files.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"content"},
			"properties": map[string]any{
				"content": map[string]any{
					"type":        "string",
					"description": "Self-contained fact or observation to remember.",
				},
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional tags for later filtering.",
				},
				"source": map[string]any{
					"type":        "string",
					"enum":        allowedMemorySources,
					"description": "Who originated this fact. Defaults to \"assistant\".",
				},
				"id": map[string]any{
					"type":        "string",
					"description": "Optional caller-supplied identifier. Omit to let the provider assign one.",
				},
			},
		},
	}
}

func (t *writeMemoryTool) Execute(ctx context.Context, args string) (string, error) {
	mem := memoryProvider(t.env)
	if mem == nil {
		return "", errMemoryUnavailable
	}
	var a writeMemoryArgs
	if err := decodeArgs(args, &a); err != nil {
		return "", fmt.Errorf("write_memory: %w", err)
	}
	content := strings.TrimSpace(a.Content)
	if content == "" {
		return "", fmt.Errorf("write_memory: content is required and must be non-empty")
	}
	if !isAllowedMemorySource(a.Source) {
		return "", fmt.Errorf("write_memory: source %q is not one of %v", a.Source, allowedMemorySources)
	}
	source := store.Source(a.Source)
	if source == "" {
		source = store.SourceAssistant
	}
	entry := store.Entry{
		ID:      store.ID(strings.TrimSpace(a.ID)),
		Content: content,
		Tags:    append([]string(nil), a.Tags...),
		Source:  source,
	}
	id, err := mem.Store(ctx, entry)
	if err != nil {
		return "", fmt.Errorf("write_memory: %w", err)
	}
	out := map[string]any{
		"id":      string(id),
		"written": true,
		"source":  string(source),
		"tags":    entry.Tags,
		"length":  len(content),
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("write_memory: encode result: %w", err)
	}
	return string(b), nil
}

// read_memory

type readMemoryArgs struct {
	Query string   `json:"query,omitempty"`
	Tags  []string `json:"tags,omitempty"`
	Limit int      `json:"limit,omitempty"`
	Since string   `json:"since,omitempty"`
}

type readMemoryTool struct {
	env *Env
}

func NewReadMemoryTool(env *Env) *readMemoryTool {
	return &readMemoryTool{env: env}
}

func (t *readMemoryTool) Name() string { return readMemoryName }

func (t *readMemoryTool) IsReadOnly() bool        { return true }
func (t *readMemoryTool) IsConcurrencySafe() bool { return true }

func (t *readMemoryTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: readMemoryName,
		Description: "Read long-term memory. With a query, searches entry content; without a query, returns recent entries. " +
			"Use tags and since to narrow the result set when useful.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Optional text to search for inside memory content. Omit to list recent entries.",
				},
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional tag filter; only entries carrying all listed tags are returned.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     memoryMaxLimit,
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

func (t *readMemoryTool) Execute(ctx context.Context, args string) (string, error) {
	mem := memoryProvider(t.env)
	if mem == nil {
		return "", errMemoryUnavailable
	}
	var a readMemoryArgs
	if err := decodeArgs(args, &a); err != nil {
		return "", fmt.Errorf("read_memory: %w", err)
	}
	limit := normalizeMemoryLimit(a.Limit)
	since, err := parseMemorySince(a.Since)
	if err != nil {
		return "", fmt.Errorf("read_memory: %w", err)
	}
	query := strings.TrimSpace(a.Query)

	var entries []store.Entry
	if query == "" {
		entries, err = mem.Recall(ctx, store.RecallQuery{
			Tags:  append([]string(nil), a.Tags...),
			Limit: limit,
			Since: since,
		})
	} else {
		entries, err = mem.Search(ctx, store.SearchQuery{
			Text:  query,
			Tags:  append([]string(nil), a.Tags...),
			Since: since,
			Limit: limit,
		})
	}
	if err != nil {
		return "", fmt.Errorf("read_memory: %w", err)
	}

	dtos := make([]memoryEntryDTO, 0, len(entries))
	for _, e := range entries {
		dtos = append(dtos, toMemoryEntryDTO(e))
	}
	out := map[string]any{
		"count":   len(dtos),
		"entries": dtos,
	}
	if query != "" {
		out["query"] = query
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("read_memory: encode result: %w", err)
	}
	return string(b), nil
}

func normalizeMemoryLimit(limit int) int {
	if limit <= 0 {
		return memoryDefaultLimit
	}
	if limit > memoryMaxLimit {
		return memoryMaxLimit
	}
	return limit
}

func parseMemorySince(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return parsed, nil
	}
	parsed, nanoErr := time.Parse(time.RFC3339Nano, value)
	if nanoErr == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("since must be RFC3339: %w", err)
}

func NewMemoryTools(env *Env) []Tool {
	return []Tool{
		NewReadMemoryTool(env),
		NewWriteMemoryTool(env),
	}
}
