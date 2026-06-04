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

	memoryTargetMemory = "memory"
	memoryTargetUser   = "user"
	memoryTargetPrefix = "target:"
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
	Target    string   `json:"target,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Source    string   `json:"source,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

func toMemoryEntryDTO(e store.Entry) memoryEntryDTO {
	out := memoryEntryDTO{
		ID:      string(e.ID),
		Content: e.Content,
		Target:  memoryEntryTarget(e),
		Tags:    memoryUserTags(e.Tags),
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

var allowedMemoryTargets = []string{
	memoryTargetMemory,
	memoryTargetUser,
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

func normalizeMemoryTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return memoryTargetMemory, nil
	}
	if target == memoryTargetMemory || target == memoryTargetUser {
		return target, nil
	}
	return "", fmt.Errorf("target %q is not one of %v", target, allowedMemoryTargets)
}

func normalizeOptionalMemoryTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", nil
	}
	return normalizeMemoryTarget(target)
}

func memoryEntryTarget(entry store.Entry) string {
	for _, tag := range entry.Tags {
		switch strings.TrimSpace(tag) {
		case memoryTargetPrefix + memoryTargetUser:
			return memoryTargetUser
		case memoryTargetPrefix + memoryTargetMemory:
			return memoryTargetMemory
		}
	}
	return memoryTargetMemory
}

func memoryUserTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || strings.HasPrefix(tag, memoryTargetPrefix) {
			continue
		}
		out = append(out, tag)
	}
	return out
}

func memoryStoreTags(target string, tags []string) []string {
	out := memoryUserTags(tags)
	out = append(out, memoryTargetPrefix+target)
	return out
}

// write_memory

type writeMemoryArgs struct {
	Target  string   `json:"target,omitempty"`
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
		Description: "Save compact, durable information to this agent profile's long-term memory. " +
			"Use proactively when the user corrects you, shares a stable preference, asks you to remember something, " +
			"or when you learn a durable environment fact, project convention, or tool quirk that will matter in future sessions. " +
			"Do not save task progress, completed-work logs, PR numbers, commit SHAs, temporary TODOs, raw data dumps, or facts likely to go stale within a week. " +
			"Write declarative facts, not instructions to yourself.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"target", "content"},
			"properties": map[string]any{
				"target": map[string]any{
					"type":        "string",
					"enum":        allowedMemoryTargets,
					"description": "\"user\" for who the user is and how they prefer you to work; \"memory\" for agent notes such as environment facts, project conventions, tool quirks, and lessons learned.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Self-contained durable fact. Phrase as a declarative statement, not as an imperative instruction.",
				},
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional tags for later filtering, such as a project name, subsystem, tool, or preference category.",
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
	target, err := normalizeMemoryTarget(a.Target)
	if err != nil {
		return "", fmt.Errorf("write_memory: %w", err)
	}
	source := store.Source(a.Source)
	if source == "" {
		source = store.SourceAssistant
	}
	entry := store.Entry{
		ID:      store.ID(strings.TrimSpace(a.ID)),
		Content: content,
		Tags:    memoryStoreTags(target, a.Tags),
		Source:  source,
	}
	id, err := mem.Store(ctx, entry)
	if err != nil {
		return "", fmt.Errorf("write_memory: %w", err)
	}
	out := map[string]any{
		"id":      string(id),
		"written": true,
		"target":  target,
		"source":  string(source),
		"tags":    memoryUserTags(entry.Tags),
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
	Target string   `json:"target,omitempty"`
	Query  string   `json:"query,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Limit  int      `json:"limit,omitempty"`
	Since  string   `json:"since,omitempty"`
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
		Description: "Read this agent profile's long-term memory. With a query, searches entry content; without a query, returns recent entries. " +
			"Use this when the user refers to remembered preferences, prior durable facts, or stable project/environment conventions. " +
			"Use target=\"user\" for user profile facts and target=\"memory\" for agent notes; omit target to search both.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"target": map[string]any{
					"type":        "string",
					"enum":        allowedMemoryTargets,
					"description": "Optional memory target to search: \"user\" or \"memory\". Omit to search both.",
				},
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
	target, err := normalizeOptionalMemoryTarget(a.Target)
	if err != nil {
		return "", fmt.Errorf("read_memory: %w", err)
	}
	query := strings.TrimSpace(a.Query)

	var entries []store.Entry
	providerLimit := limit
	if target != "" {
		providerLimit = 0
	}
	if query == "" {
		entries, err = mem.Recall(ctx, store.RecallQuery{
			Tags:  append([]string(nil), a.Tags...),
			Limit: providerLimit,
			Since: since,
		})
	} else {
		entries, err = mem.Search(ctx, store.SearchQuery{
			Text:  query,
			Tags:  append([]string(nil), a.Tags...),
			Since: since,
			Limit: providerLimit,
		})
	}
	if err != nil {
		return "", fmt.Errorf("read_memory: %w", err)
	}
	if target != "" {
		entries = filterMemoryEntriesByTarget(entries, target)
		entries = limitMemoryEntries(entries, limit)
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
	if target != "" {
		out["target"] = target
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("read_memory: encode result: %w", err)
	}
	return string(b), nil
}

func filterMemoryEntriesByTarget(entries []store.Entry, target string) []store.Entry {
	out := entries[:0]
	for _, entry := range entries {
		if memoryEntryTarget(entry) == target {
			out = append(out, entry)
		}
	}
	return out
}

func limitMemoryEntries(entries []store.Entry, limit int) []store.Entry {
	if limit <= 0 {
		limit = memoryDefaultLimit
	}
	if limit > memoryMaxLimit {
		limit = memoryMaxLimit
	}
	if len(entries) > limit {
		return entries[:limit]
	}
	return entries
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
