package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/memory/store"
)

// -----------------------------------------------------------------------------
// Test fixtures
// -----------------------------------------------------------------------------

// newTestProvider returns a FileProvider rooted at a fresh tempdir, with
// one entry pre-loaded so tests have something to read. The caller is
// responsible for cleanup; t.TempDir() handles that automatically.
func newTestProvider(t *testing.T) *store.FileProvider {
	t.Helper()
	dir := t.TempDir()
	p, err := store.NewFileProvider(dir)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	return p
}

// seedProvider inserts entries through the public Store API so each
// entry has a real CreatedAt and real on-disk record. Sleep is used to
// guarantee a strict recency order between successive seeds.
func seedProvider(t *testing.T, p *store.FileProvider, contents []string, tags []string) []store.ID {
	t.Helper()
	ids := make([]store.ID, 0, len(contents))
	for i, c := range contents {
		id, err := p.Store(context.Background(), store.Entry{
			Content: c,
			Tags:    tags,
			Source:  store.SourceAssistant,
		})
		if err != nil {
			t.Fatalf("seed Store #%d: %v", i, err)
		}
		ids = append(ids, id)
		// 2ms is more than enough to disambiguate CreatedAt on every
		// reasonable filesystem; the FileProvider uses time.Now() with
		// nanosecond precision.
		time.Sleep(2 * time.Millisecond)
	}
	return ids
}

// stubProvider is a hand-rolled Provider used to inject errors into
// specific call paths. The default behavior is success with sensible
// return values; tests override only the methods they need to fail.
type stubProvider struct {
	storeErr    error
	recallErr   error
	searchErr   error
	availableOk bool

	storeCalls  int
	recallCalls int
	searchCalls int

	stored []store.Entry
}

func (s *stubProvider) Available(_ context.Context) error {
	if !s.availableOk {
		return fmt.Errorf("stub: unavailable")
	}
	return nil
}

func (s *stubProvider) Name() string { return "stub" }

func (s *stubProvider) Store(_ context.Context, e store.Entry) (store.ID, error) {
	s.storeCalls++
	if s.storeErr != nil {
		return "", s.storeErr
	}
	s.stored = append(s.stored, e)
	if e.ID != "" {
		return e.ID, nil
	}
	return store.ID(fmt.Sprintf("stub-%d", s.storeCalls)), nil
}

func (s *stubProvider) Recall(_ context.Context, _ store.RecallQuery) ([]store.Entry, error) {
	s.recallCalls++
	if s.recallErr != nil {
		return nil, s.recallErr
	}
	out := make([]store.Entry, 0, len(s.stored))
	for i := len(s.stored) - 1; i >= 0; i-- {
		cp := s.stored[i]
		out = append(out, cp)
	}
	return out, nil
}

func (s *stubProvider) Search(_ context.Context, q store.SearchQuery) ([]store.Entry, error) {
	s.searchCalls++
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	out := make([]store.Entry, 0)
	for i := len(s.stored) - 1; i >= 0; i-- {
		e := s.stored[i]
		if q.Text != "" && !strings.Contains(e.Content, q.Text) {
			continue
		}
		if len(q.Tags) > 0 {
			match := true
			for _, want := range q.Tags {
				found := false
				for _, have := range e.Tags {
					if have == want {
						found = true
						break
					}
				}
				if !found {
					match = false
					break
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *stubProvider) Delete(_ context.Context, _ store.ID) error { return nil }

func newEnvWith(p store.Provider) *Env {
	return &Env{Memory: p}
}

// -----------------------------------------------------------------------------
// memory_store
// -----------------------------------------------------------------------------

func TestMemoryStoreTool_Name(t *testing.T) {
	tool := NewMemoryStoreTool(newEnvWith(newTestProvider(t)))
	if got, want := tool.Name(), "memory_store"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestMemoryStoreTool_Definition(t *testing.T) {
	tool := NewMemoryStoreTool(newEnvWith(newTestProvider(t)))
	def := tool.Definition()
	if def.Name != "memory_store" {
		t.Errorf("def.Name = %q, want memory_store", def.Name)
	}
	if def.Description == "" {
		t.Error("def.Description is empty")
	}
	props, ok := def.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("def.InputSchema[properties] missing or wrong type: %T", def.InputSchema["properties"])
	}
	for _, key := range []string{"content", "tags", "source", "id"} {
		if _, ok := props[key].(map[string]any); !ok {
			t.Errorf("schema missing property %q", key)
		}
	}
	// content is the only required field.
	required, _ := def.InputSchema["required"].([]string)
	if len(required) != 1 || required[0] != "content" {
		t.Errorf("required = %v, want [content]", required)
	}
	// Source enum is exposed.
	src, _ := props["source"].(map[string]any)
	enum, _ := src["enum"].([]string)
	wantEnum := []string{"user", "assistant", "tool", "system", "import"}
	if len(enum) != len(wantEnum) {
		t.Errorf("source enum = %v, want %v", enum, wantEnum)
	}
	// additionalProperties: false is the model contract.
	raw, ok := def.InputSchema["additionalProperties"]
	if !ok {
		t.Error("additionalProperties key missing from schema")
	} else if v, _ := raw.(bool); v != false {
		t.Errorf("additionalProperties = %v, want false", v)
	}
}

func TestMemoryStoreTool_Execute_HappyPath(t *testing.T) {
	p := newTestProvider(t)
	tool := NewMemoryStoreTool(newEnvWith(p))

	out, err := tool.Execute(context.Background(), `{"content":"user prefers dark mode","tags":["ui","pref"]}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal result: %v (raw: %s)", err, out)
	}
	if id, _ := resp["id"].(string); id == "" {
		t.Errorf("id missing or empty in result: %s", out)
	}
	if stored, _ := resp["stored"].(bool); !stored {
		t.Errorf("stored = %v, want true (raw: %s)", resp["stored"], out)
	}
	if src, _ := resp["source"].(string); src != "assistant" {
		t.Errorf("default source = %q, want assistant", src)
	}

	// And the entry is actually in the provider.
	entries, err := p.Recall(context.Background(), store.RecallQuery{})
	if err != nil {
		t.Fatalf("provider Recall: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("provider has %d entries, want 1", len(entries))
	}
	if entries[0].Content != "user prefers dark mode" {
		t.Errorf("stored content = %q", entries[0].Content)
	}
}

func TestMemoryStoreTool_Execute_WithCallerID(t *testing.T) {
	p := newTestProvider(t)
	tool := NewMemoryStoreTool(newEnvWith(p))

	out, err := tool.Execute(context.Background(), `{"content":"x","id":"caller-123"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp map[string]any
	_ = json.Unmarshal([]byte(out), &resp)
	if id, _ := resp["id"].(string); id != "caller-123" {
		t.Errorf("id = %q, want caller-123", id)
	}
}

func TestMemoryStoreTool_Execute_DefaultSource(t *testing.T) {
	p := newTestProvider(t)
	tool := NewMemoryStoreTool(newEnvWith(p))

	// Omit source entirely; tool should default to "assistant".
	out, err := tool.Execute(context.Background(), `{"content":"x"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp map[string]any
	_ = json.Unmarshal([]byte(out), &resp)
	if src, _ := resp["source"].(string); src != "assistant" {
		t.Errorf("default source = %q, want assistant", src)
	}
}

func TestMemoryStoreTool_Execute_ExplicitSource(t *testing.T) {
	p := newTestProvider(t)
	tool := NewMemoryStoreTool(newEnvWith(p))

	out, err := tool.Execute(context.Background(), `{"content":"x","source":"user"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp map[string]any
	_ = json.Unmarshal([]byte(out), &resp)
	if src, _ := resp["source"].(string); src != "user" {
		t.Errorf("source = %q, want user", src)
	}
}

func TestMemoryStoreTool_Execute_NoProvider(t *testing.T) {
	tool := NewMemoryStoreTool(&Env{}) // no Memory set
	_, err := tool.Execute(context.Background(), `{"content":"x"}`)
	if !errors.Is(err, errMemoryUnavailable) {
		t.Errorf("err = %v, want errMemoryUnavailable", err)
	}
}

func TestMemoryStoreTool_Execute_NilEnv(t *testing.T) {
	tool := NewMemoryStoreTool(nil)
	_, err := tool.Execute(context.Background(), `{"content":"x"}`)
	if !errors.Is(err, errMemoryUnavailable) {
		t.Errorf("err = %v, want errMemoryUnavailable", err)
	}
}

func TestMemoryStoreTool_Execute_EmptyContent(t *testing.T) {
	p := newTestProvider(t)
	tool := NewMemoryStoreTool(newEnvWith(p))

	cases := []string{
		`{"content":""}`,
		`{"content":"   "}`,
		`{}`,
	}
	for _, args := range cases {
		_, err := tool.Execute(context.Background(), args)
		if err == nil {
			t.Errorf("args %s: expected error, got nil", args)
		}
	}
}

func TestMemoryStoreTool_Execute_BadSource(t *testing.T) {
	p := newTestProvider(t)
	tool := NewMemoryStoreTool(newEnvWith(p))

	_, err := tool.Execute(context.Background(), `{"content":"x","source":"unknown"}`)
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
	if !strings.Contains(err.Error(), "source") {
		t.Errorf("error %q should mention source", err)
	}
}

func TestMemoryStoreTool_Execute_BadJSON(t *testing.T) {
	p := newTestProvider(t)
	tool := NewMemoryStoreTool(newEnvWith(p))

	_, err := tool.Execute(context.Background(), `{not json`)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestMemoryStoreTool_Execute_ProviderError(t *testing.T) {
	stub := &stubProvider{storeErr: fmt.Errorf("disk full")}
	tool := NewMemoryStoreTool(newEnvWith(stub))

	_, err := tool.Execute(context.Background(), `{"content":"x"}`)
	if err == nil {
		t.Fatal("expected provider error to surface")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error %q should contain 'disk full'", err)
	}
	if stub.storeCalls != 1 {
		t.Errorf("Store called %d times, want 1", stub.storeCalls)
	}
}

// -----------------------------------------------------------------------------
// memory_recall
// -----------------------------------------------------------------------------

func TestMemoryRecallTool_Name(t *testing.T) {
	tool := NewMemoryRecallTool(newEnvWith(newTestProvider(t)))
	if got, want := tool.Name(), "memory_recall"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestMemoryRecallTool_Definition(t *testing.T) {
	tool := NewMemoryRecallTool(newEnvWith(newTestProvider(t)))
	def := tool.Definition()
	if def.Name != "memory_recall" {
		t.Errorf("def.Name = %q, want memory_recall", def.Name)
	}
	props, ok := def.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("def.InputSchema[properties] missing or wrong type: %T", def.InputSchema["properties"])
	}
	for _, key := range []string{"tags", "limit", "since"} {
		if _, ok := props[key].(map[string]any); !ok {
			t.Errorf("schema missing property %q", key)
		}
	}
	// Recall has no required fields — a bare {} is valid.
	if req, present := def.InputSchema["required"]; present {
		t.Errorf("required should be absent, got %v", req)
	}
	raw, ok := def.InputSchema["additionalProperties"]
	if !ok {
		t.Error("additionalProperties key missing from schema")
	} else if v, _ := raw.(bool); v != false {
		t.Errorf("additionalProperties = %v, want false", v)
	}
}

func TestMemoryRecallTool_Execute_NoProvider(t *testing.T) {
	tool := NewMemoryRecallTool(&Env{})
	_, err := tool.Execute(context.Background(), `{}`)
	if !errors.Is(err, errMemoryUnavailable) {
		t.Errorf("err = %v, want errMemoryUnavailable", err)
	}
}

func TestMemoryRecallTool_Execute_NilEnv(t *testing.T) {
	tool := NewMemoryRecallTool(nil)
	_, err := tool.Execute(context.Background(), `{}`)
	if !errors.Is(err, errMemoryUnavailable) {
		t.Errorf("err = %v, want errMemoryUnavailable", err)
	}
}

func TestMemoryRecallTool_Execute_AllEntries(t *testing.T) {
	p := newTestProvider(t)
	seedProvider(t, p, []string{"a", "b", "c"}, nil)
	tool := NewMemoryRecallTool(newEnvWith(p))

	out, err := tool.Execute(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp struct {
		Count   int            `json:"count"`
		Entries []memoryEntryDTO `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v (raw: %s)", err, out)
	}
	if resp.Count != 3 || len(resp.Entries) != 3 {
		t.Errorf("count=%d, len(entries)=%d", resp.Count, len(resp.Entries))
	}
}

func TestMemoryRecallTool_Execute_NewestFirst(t *testing.T) {
	p := newTestProvider(t)
	seedProvider(t, p, []string{"first", "second", "third"}, nil)
	tool := NewMemoryRecallTool(newEnvWith(p))

	out, err := tool.Execute(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp struct {
		Entries []memoryEntryDTO `json:"entries"`
	}
	_ = json.Unmarshal([]byte(out), &resp)
	want := []string{"third", "second", "first"}
	if len(resp.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(resp.Entries), len(want))
	}
	for i, e := range resp.Entries {
		if e.Content != want[i] {
			t.Errorf("entry[%d].Content = %q, want %q", i, e.Content, want[i])
		}
	}
}

func TestMemoryRecallTool_Execute_TagFilterAND(t *testing.T) {
	p := newTestProvider(t)
	// Three entries with overlapping tag sets.
	if _, err := p.Store(context.Background(), store.Entry{Content: "a", Tags: []string{"x", "y"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := p.Store(context.Background(), store.Entry{Content: "b", Tags: []string{"y"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := p.Store(context.Background(), store.Entry{Content: "c", Tags: []string{"x", "y", "z"}}); err != nil {
		t.Fatal(err)
	}

	tool := NewMemoryRecallTool(newEnvWith(p))
	out, err := tool.Execute(context.Background(), `{"tags":["x","y"]}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp struct {
		Entries []memoryEntryDTO `json:"entries"`
	}
	_ = json.Unmarshal([]byte(out), &resp)
	// Only "a" and "c" have BOTH x and y. "b" is excluded.
	if len(resp.Entries) != 2 {
		t.Fatalf("got %d entries, want 2 (raw: %s)", len(resp.Entries), out)
	}
	got := map[string]bool{}
	for _, e := range resp.Entries {
		got[e.Content] = true
	}
	if !got["a"] || !got["c"] {
		t.Errorf("missing entries: %v (want a,c)", got)
	}
}

func TestMemoryRecallTool_Execute_Limit(t *testing.T) {
	p := newTestProvider(t)
	seedProvider(t, p, []string{"a", "b", "c", "d", "e"}, nil)
	tool := NewMemoryRecallTool(newEnvWith(p))

	out, err := tool.Execute(context.Background(), `{"limit":2}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp struct {
		Count   int               `json:"count"`
		Entries []memoryEntryDTO  `json:"entries"`
	}
	_ = json.Unmarshal([]byte(out), &resp)
	if resp.Count != 2 || len(resp.Entries) != 2 {
		t.Errorf("count=%d, len=%d, want 2 each", resp.Count, len(resp.Entries))
	}
	// Limit is applied after ordering, so we get the two newest.
	if resp.Entries[0].Content != "e" || resp.Entries[1].Content != "d" {
		t.Errorf("got %q, %q; want e, d", resp.Entries[0].Content, resp.Entries[1].Content)
	}
}

func TestMemoryRecallTool_Execute_LimitClamped(t *testing.T) {
	p := newTestProvider(t)
	seedProvider(t, p, []string{"a", "b", "c"}, nil)
	tool := NewMemoryRecallTool(newEnvWith(p))

	// 999 should be clamped to 200 by the tool; with only 3 entries we
	// just get 3 back.
	out, err := tool.Execute(context.Background(), `{"limit":999}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal([]byte(out), &resp)
	if resp.Count != 3 {
		t.Errorf("count = %d, want 3 (limit clamped to 200)", resp.Count)
	}
}

func TestMemoryRecallTool_Execute_Since(t *testing.T) {
	p := newTestProvider(t)
	seedProvider(t, p, []string{"old"}, nil)
	time.Sleep(50 * time.Millisecond)
	cutoff := time.Now().UTC()
	time.Sleep(50 * time.Millisecond)
	seedProvider(t, p, []string{"new"}, nil)

	tool := NewMemoryRecallTool(newEnvWith(p))
	args := fmt.Sprintf(`{"since":%q}`, cutoff.Format(time.RFC3339Nano))
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp struct {
		Entries []memoryEntryDTO `json:"entries"`
	}
	_ = json.Unmarshal([]byte(out), &resp)
	if len(resp.Entries) != 1 || resp.Entries[0].Content != "new" {
		t.Errorf("since filter wrong: got %+v, want [new]", resp.Entries)
	}
}

func TestMemoryRecallTool_Execute_BadSince(t *testing.T) {
	p := newTestProvider(t)
	tool := NewMemoryRecallTool(newEnvWith(p))

	_, err := tool.Execute(context.Background(), `{"since":"yesterday"}`)
	if err == nil {
		t.Fatal("expected error for invalid since")
	}
	if !strings.Contains(err.Error(), "RFC3339") {
		t.Errorf("error %q should mention RFC3339", err)
	}
}

func TestMemoryRecallTool_Execute_BadJSON(t *testing.T) {
	p := newTestProvider(t)
	tool := NewMemoryRecallTool(newEnvWith(p))

	_, err := tool.Execute(context.Background(), `{`)
	if err == nil {
		t.Fatal("expected JSON error")
	}
}

func TestMemoryRecallTool_Execute_ProviderError(t *testing.T) {
	stub := &stubProvider{recallErr: fmt.Errorf("io error")}
	tool := NewMemoryRecallTool(newEnvWith(stub))

	_, err := tool.Execute(context.Background(), `{}`)
	if err == nil {
		t.Fatal("expected provider error")
	}
	if !strings.Contains(err.Error(), "io error") {
		t.Errorf("error %q should contain 'io error'", err)
	}
}

// -----------------------------------------------------------------------------
// memory_search
// -----------------------------------------------------------------------------

func TestMemorySearchTool_Name(t *testing.T) {
	tool := NewMemorySearchTool(newEnvWith(newTestProvider(t)))
	if got, want := tool.Name(), "memory_search"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestMemorySearchTool_Definition(t *testing.T) {
	tool := NewMemorySearchTool(newEnvWith(newTestProvider(t)))
	def := tool.Definition()
	if def.Name != "memory_search" {
		t.Errorf("def.Name = %q, want memory_search", def.Name)
	}
	props, ok := def.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("def.InputSchema[properties] missing or wrong type")
	}
	for _, key := range []string{"query", "tags", "limit"} {
		if _, ok := props[key].(map[string]any); !ok {
			t.Errorf("schema missing property %q", key)
		}
	}
	required, _ := def.InputSchema["required"].([]string)
	if len(required) != 1 || required[0] != "query" {
		t.Errorf("required = %v, want [query]", required)
	}
	raw, ok := def.InputSchema["additionalProperties"]
	if !ok {
		t.Error("additionalProperties key missing from schema")
	} else if v, _ := raw.(bool); v != false {
		t.Errorf("additionalProperties = %v, want false", v)
	}
}

func TestMemorySearchTool_Execute_NoProvider(t *testing.T) {
	tool := NewMemorySearchTool(&Env{})
	_, err := tool.Execute(context.Background(), `{"query":"x"}`)
	if !errors.Is(err, errMemoryUnavailable) {
		t.Errorf("err = %v, want errMemoryUnavailable", err)
	}
}

func TestMemorySearchTool_Execute_NilEnv(t *testing.T) {
	tool := NewMemorySearchTool(nil)
	_, err := tool.Execute(context.Background(), `{"query":"x"}`)
	if !errors.Is(err, errMemoryUnavailable) {
		t.Errorf("err = %v, want errMemoryUnavailable", err)
	}
}

func TestMemorySearchTool_Execute_SubstringMatch(t *testing.T) {
	p := newTestProvider(t)
	seedProvider(t, p, []string{
		"the quick brown fox",
		"hello world",
		"foxes are not foxes",
	}, nil)
	tool := NewMemorySearchTool(newEnvWith(p))

	out, err := tool.Execute(context.Background(), `{"query":"fox"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp struct {
		Query   string           `json:"query"`
		Count   int              `json:"count"`
		Entries []memoryEntryDTO `json:"entries"`
	}
	_ = json.Unmarshal([]byte(out), &resp)
	if resp.Query != "fox" {
		t.Errorf("echoed query = %q, want fox", resp.Query)
	}
	if resp.Count != 2 {
		t.Errorf("count = %d, want 2 (raw: %s)", resp.Count, out)
	}
}

func TestMemorySearchTool_Execute_TagAndText(t *testing.T) {
	p := newTestProvider(t)
	seedProvider(t, p, []string{"alpha", "beta"}, []string{"k1"})
	time.Sleep(2 * time.Millisecond)
	if _, err := p.Store(context.Background(), store.Entry{Content: "gamma", Tags: []string{"k2"}}); err != nil {
		t.Fatal(err)
	}
	tool := NewMemorySearchTool(newEnvWith(p))

	// Tag filter excludes gamma, then text filter matches alpha/beta.
	out, err := tool.Execute(context.Background(), `{"query":"a","tags":["k1"]}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp struct {
		Entries []memoryEntryDTO `json:"entries"`
	}
	_ = json.Unmarshal([]byte(out), &resp)
	if len(resp.Entries) != 2 {
		t.Errorf("got %d entries, want 2 (raw: %s)", len(resp.Entries), out)
	}
}

func TestMemorySearchTool_Execute_Limit(t *testing.T) {
	p := newTestProvider(t)
	seedProvider(t, p, []string{"aa", "ab", "ac", "ad"}, nil)
	tool := NewMemorySearchTool(newEnvWith(p))

	out, err := tool.Execute(context.Background(), `{"query":"a","limit":2}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal([]byte(out), &resp)
	if resp.Count != 2 {
		t.Errorf("count = %d, want 2", resp.Count)
	}
}

func TestMemorySearchTool_Execute_CaseInsensitive(t *testing.T) {
	p := newTestProvider(t)
	seedProvider(t, p, []string{"Fox", "fox", "FOX"}, nil)
	tool := NewMemorySearchTool(newEnvWith(p))

	// Search is case-insensitive: "Fox" matches all three casings.
	out, err := tool.Execute(context.Background(), `{"query":"Fox"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp struct {
		Count   int              `json:"count"`
		Entries []memoryEntryDTO `json:"entries"`
	}
	_ = json.Unmarshal([]byte(out), &resp)
	if resp.Count != 3 {
		t.Errorf("case-insensitive search count = %d, want 3 (raw: %s)", resp.Count, out)
	}
}

func TestMemorySearchTool_Execute_NoMatches(t *testing.T) {
	p := newTestProvider(t)
	seedProvider(t, p, []string{"a", "b"}, nil)
	tool := NewMemorySearchTool(newEnvWith(p))

	out, err := tool.Execute(context.Background(), `{"query":"nope"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp struct {
		Count   int              `json:"count"`
		Entries []memoryEntryDTO `json:"entries"`
	}
	_ = json.Unmarshal([]byte(out), &resp)
	if resp.Count != 0 {
		t.Errorf("count = %d, want 0", resp.Count)
	}
	// entries should be present as an empty array, not null, so the
	// model always gets a consistent shape.
	raw := map[string]any{}
	_ = json.Unmarshal([]byte(out), &raw)
	if _, present := raw["entries"]; !present {
		t.Error("entries field missing from empty result")
	}
}

func TestMemorySearchTool_Execute_EmptyQuery(t *testing.T) {
	p := newTestProvider(t)
	tool := NewMemorySearchTool(newEnvWith(p))

	cases := []string{
		`{"query":""}`,
		`{"query":"   "}`,
		`{}`,
	}
	for _, args := range cases {
		_, err := tool.Execute(context.Background(), args)
		if err == nil {
			t.Errorf("args %s: expected error", args)
		}
	}
}

func TestMemorySearchTool_Execute_BadJSON(t *testing.T) {
	p := newTestProvider(t)
	tool := NewMemorySearchTool(newEnvWith(p))

	_, err := tool.Execute(context.Background(), `not json`)
	if err == nil {
		t.Fatal("expected JSON error")
	}
}

func TestMemorySearchTool_Execute_ProviderError(t *testing.T) {
	stub := &stubProvider{searchErr: fmt.Errorf("index corrupted")}
	tool := NewMemorySearchTool(newEnvWith(stub))

	_, err := tool.Execute(context.Background(), `{"query":"x"}`)
	if err == nil {
		t.Fatal("expected provider error")
	}
	if !strings.Contains(err.Error(), "index corrupted") {
		t.Errorf("error %q should contain 'index corrupted'", err)
	}
}

// -----------------------------------------------------------------------------
// NewMemoryTools + Tool interface compliance
// -----------------------------------------------------------------------------

func TestNewMemoryTools_ReturnsAllThree(t *testing.T) {
	env := newEnvWith(newTestProvider(t))
	tools := NewMemoryTools(env)
	if len(tools) != 3 {
		t.Fatalf("NewMemoryTools returned %d tools, want 3", len(tools))
	}
	want := []string{"memory_store", "memory_recall", "memory_search"}
	for i, tool := range tools {
		if got := tool.Name(); got != want[i] {
			t.Errorf("tools[%d].Name() = %q, want %q", i, got, want[i])
		}
	}
}

func TestNewMemoryTools_NilEnvIsSafe(t *testing.T) {
	// All three tools should be constructable from a nil env. They
	// won't function, but the constructors must not panic.
	tools := NewMemoryTools(nil)
	if len(tools) != 3 {
		t.Errorf("got %d tools, want 3", len(tools))
	}
}

func TestNewMemoryTools_SatisfyToolInterface(t *testing.T) {
	// Compile-time check: the return type of NewMemoryTools is []Tool,
	// so this assertion is just to lock in the contract at runtime in
	// case the interface grows a method.
	env := newEnvWith(newTestProvider(t))
	tools := NewMemoryTools(env)
	for _, tool := range tools {
		// Calling every interface method must not panic.
		_ = tool.Name()
		_ = tool.IsReadOnly()
		_ = tool.IsConcurrencySafe()
		def := tool.Definition()
		if def.Name != tool.Name() {
			t.Errorf("Definition().Name = %q, Name() = %q", def.Name, tool.Name())
		}
	}
}

// -----------------------------------------------------------------------------
// Tool risk classification
// -----------------------------------------------------------------------------

func TestMemoryTools_ReadOnlyClassification(t *testing.T) {
	env := newEnvWith(newTestProvider(t))
	if !NewMemoryRecallTool(env).IsReadOnly() {
		t.Error("memory_recall should be read-only")
	}
	if !NewMemorySearchTool(env).IsReadOnly() {
		t.Error("memory_search should be read-only")
	}
	if NewMemoryStoreTool(env).IsReadOnly() {
		t.Error("memory_store must NOT be read-only")
	}
}

// -----------------------------------------------------------------------------
// Sanity check: real on-disk state via FileProvider integration
// -----------------------------------------------------------------------------

func TestMemoryStoreTool_EndToEndWithFileProvider(t *testing.T) {
	// Integration: write via the tool, read back via a fresh provider
	// instance pointed at the same directory. This exercises the
	// JSONL log end-to-end and proves the on-disk format is what the
	// loader expects.
	dir := t.TempDir()

	writer := NewMemoryStoreTool(newEnvWith(mustProvider(t, dir)))
	out, err := writer.Execute(context.Background(),
		`{"content":"end-to-end test","tags":["e2e"],"source":"user"}`)
	if err != nil {
		t.Fatalf("writer.Execute: %v", err)
	}
	if !strings.Contains(out, `"stored":true`) {
		t.Errorf("missing stored:true in result: %s", out)
	}

	// A second FileProvider pointed at the same dir should see the
	// entry on first call to Recall (the provider is lazy-loaded).
	reader, err := store.NewFileProvider(dir)
	if err != nil {
		t.Fatalf("NewFileProvider reader: %v", err)
	}
	entries, err := reader.Recall(context.Background(), store.RecallQuery{Tags: []string{"e2e"}})
	if err != nil {
		t.Fatalf("reader.Recall: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("reader.Recall got %d, want 1", len(entries))
	}
	if entries[0].Content != "end-to-end test" {
		t.Errorf("reader content = %q", entries[0].Content)
	}
	if entries[0].Source != "user" {
		t.Errorf("reader source = %q, want user", entries[0].Source)
	}

	// Sanity: the on-disk file is JSONL with a single line.
	data, err := os.ReadFile(filepath.Join(dir, "entries.jsonl"))
	if err != nil {
		t.Fatalf("read entries.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Errorf("entries.jsonl has %d lines, want 1", len(lines))
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Errorf("entries.jsonl line is not valid JSON: %v", err)
	}
}

func mustProvider(t *testing.T, dir string) *store.FileProvider {
	t.Helper()
	p, err := store.NewFileProvider(dir)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	return p
}
