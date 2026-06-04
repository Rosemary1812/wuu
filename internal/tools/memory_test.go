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

func newTestProvider(t *testing.T) *store.FileProvider {
	t.Helper()
	p, err := store.NewFileProvider(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	return p
}

func mustProvider(t *testing.T, dir string) *store.FileProvider {
	t.Helper()
	p, err := store.NewFileProvider(dir)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	return p
}

func seedProvider(t *testing.T, p *store.FileProvider, contents []string, tags []string) []store.ID {
	t.Helper()
	ids := make([]store.ID, 0, len(contents))
	for i, content := range contents {
		id, err := p.Store(context.Background(), store.Entry{
			Content: content,
			Tags:    tags,
			Source:  store.SourceAssistant,
		})
		if err != nil {
			t.Fatalf("seed Store #%d: %v", i, err)
		}
		ids = append(ids, id)
		time.Sleep(2 * time.Millisecond)
	}
	return ids
}

type stubProvider struct {
	storeErr  error
	recallErr error
	searchErr error

	storeCalls  int
	recallCalls int
	searchCalls int

	stored []store.Entry
}

func (s *stubProvider) Name() string { return "stub" }

func (s *stubProvider) Available(context.Context) error { return nil }

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
		out = append(out, s.stored[i])
	}
	return out, nil
}

func (s *stubProvider) Search(_ context.Context, q store.SearchQuery) ([]store.Entry, error) {
	s.searchCalls++
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	out := make([]store.Entry, 0)
	needle := strings.ToLower(strings.TrimSpace(q.Text))
	for i := len(s.stored) - 1; i >= 0; i-- {
		e := s.stored[i]
		if needle != "" && !strings.Contains(strings.ToLower(e.Content), needle) {
			continue
		}
		if !testTagsMatch(e.Tags, q.Tags) {
			continue
		}
		if !q.Since.IsZero() && e.CreatedAt.Before(q.Since) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *stubProvider) Delete(context.Context, store.ID) error { return nil }

func testTagsMatch(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func newEnvWith(p store.Provider) *Env {
	return &Env{Memory: p}
}

func decodeMemoryResult(t *testing.T, raw string) struct {
	Target  string           `json:"target"`
	Query   string           `json:"query"`
	Count   int              `json:"count"`
	Entries []memoryEntryDTO `json:"entries"`
} {
	t.Helper()
	var resp struct {
		Target  string           `json:"target"`
		Query   string           `json:"query"`
		Count   int              `json:"count"`
		Entries []memoryEntryDTO `json:"entries"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal result: %v (raw: %s)", err, raw)
	}
	return resp
}

func TestWriteMemoryTool_Definition(t *testing.T) {
	tool := NewWriteMemoryTool(newEnvWith(newTestProvider(t)))
	def := tool.Definition()
	if def.Name != "write_memory" {
		t.Fatalf("def.Name = %q, want write_memory", def.Name)
	}
	props, ok := def.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type: %T", def.InputSchema["properties"])
	}
	for _, key := range []string{"target", "content", "tags", "source", "id"} {
		if _, ok := props[key].(map[string]any); !ok {
			t.Errorf("schema missing property %q", key)
		}
	}
	required, _ := def.InputSchema["required"].([]string)
	if strings.Join(required, ",") != "target,content" {
		t.Errorf("required = %v, want [target content]", required)
	}
	if v, _ := def.InputSchema["additionalProperties"].(bool); v != false {
		t.Errorf("additionalProperties = %v, want false", v)
	}
}

func TestWriteMemoryTool_Execute(t *testing.T) {
	p := newTestProvider(t)
	tool := NewWriteMemoryTool(newEnvWith(p))

	out, err := tool.Execute(context.Background(), `{"content":"user prefers dark mode","tags":["ui","pref"]}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal result: %v (raw: %s)", err, out)
	}
	if id, _ := resp["id"].(string); id == "" {
		t.Errorf("id missing or empty: %s", out)
	}
	if written, _ := resp["written"].(bool); !written {
		t.Errorf("written = %v, want true", resp["written"])
	}
	if target, _ := resp["target"].(string); target != "memory" {
		t.Errorf("default target = %q, want memory", target)
	}
	if src, _ := resp["source"].(string); src != "assistant" {
		t.Errorf("default source = %q, want assistant", src)
	}

	entries, err := p.Recall(context.Background(), store.RecallQuery{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(entries) != 1 || entries[0].Content != "user prefers dark mode" {
		t.Fatalf("stored entries = %+v", entries)
	}
	if !testTagsMatch(entries[0].Tags, []string{"target:memory", "ui", "pref"}) {
		t.Fatalf("stored tags = %+v", entries[0].Tags)
	}
}

func TestWriteMemoryTool_Execute_WithCallerIDAndSource(t *testing.T) {
	p := newTestProvider(t)
	tool := NewWriteMemoryTool(newEnvWith(p))

	out, err := tool.Execute(context.Background(), `{"target":"user","content":"x","id":"caller-123","source":"user"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp map[string]any
	_ = json.Unmarshal([]byte(out), &resp)
	if got, _ := resp["id"].(string); got != "caller-123" {
		t.Errorf("id = %q, want caller-123", got)
	}
	if got, _ := resp["source"].(string); got != "user" {
		t.Errorf("source = %q, want user", got)
	}
	if got, _ := resp["target"].(string); got != "user" {
		t.Errorf("target = %q, want user", got)
	}
}

func TestWriteMemoryTool_Execute_ErrorBranches(t *testing.T) {
	if _, err := NewWriteMemoryTool(&Env{}).Execute(context.Background(), `{"content":"x"}`); !errors.Is(err, errMemoryUnavailable) {
		t.Fatalf("no provider err = %v, want errMemoryUnavailable", err)
	}
	if _, err := NewWriteMemoryTool(nil).Execute(context.Background(), `{"content":"x"}`); !errors.Is(err, errMemoryUnavailable) {
		t.Fatalf("nil env err = %v, want errMemoryUnavailable", err)
	}

	p := newTestProvider(t)
	tool := NewWriteMemoryTool(newEnvWith(p))
	for _, args := range []string{`{"content":""}`, `{"content":"   "}`, `{}`} {
		if _, err := tool.Execute(context.Background(), args); err == nil {
			t.Fatalf("args %s: expected empty-content error", args)
		}
	}
	if _, err := tool.Execute(context.Background(), `{"content":"x","source":"unknown"}`); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("bad source err = %v", err)
	}
	if _, err := tool.Execute(context.Background(), `{"target":"session","content":"x"}`); err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("bad target err = %v", err)
	}
	if _, err := tool.Execute(context.Background(), `{not json`); err == nil {
		t.Fatal("expected malformed JSON error")
	}

	stub := &stubProvider{storeErr: fmt.Errorf("disk full")}
	if _, err := NewWriteMemoryTool(newEnvWith(stub)).Execute(context.Background(), `{"content":"x"}`); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("provider err = %v", err)
	}
	if stub.storeCalls != 1 {
		t.Fatalf("Store called %d times, want 1", stub.storeCalls)
	}
}

func TestReadMemoryTool_Definition(t *testing.T) {
	tool := NewReadMemoryTool(newEnvWith(newTestProvider(t)))
	def := tool.Definition()
	if def.Name != "read_memory" {
		t.Fatalf("def.Name = %q, want read_memory", def.Name)
	}
	props, ok := def.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type: %T", def.InputSchema["properties"])
	}
	for _, key := range []string{"target", "query", "tags", "limit", "since"} {
		if _, ok := props[key].(map[string]any); !ok {
			t.Errorf("schema missing property %q", key)
		}
	}
	if _, present := def.InputSchema["required"]; present {
		t.Errorf("required should be absent, got %v", def.InputSchema["required"])
	}
	if v, _ := def.InputSchema["additionalProperties"].(bool); v != false {
		t.Errorf("additionalProperties = %v, want false", v)
	}
}

func TestReadMemoryTool_Execute_RecentEntries(t *testing.T) {
	p := newTestProvider(t)
	seedProvider(t, p, []string{"first", "second", "third"}, nil)
	tool := NewReadMemoryTool(newEnvWith(p))

	out, err := tool.Execute(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	resp := decodeMemoryResult(t, out)
	want := []string{"third", "second", "first"}
	if resp.Count != len(want) || len(resp.Entries) != len(want) {
		t.Fatalf("count=%d len=%d want=%d", resp.Count, len(resp.Entries), len(want))
	}
	for i, entry := range resp.Entries {
		if entry.Content != want[i] {
			t.Errorf("entry[%d].Content = %q, want %q", i, entry.Content, want[i])
		}
	}
}

func TestReadMemoryTool_Execute_QuerySearch(t *testing.T) {
	p := newTestProvider(t)
	seedProvider(t, p, []string{"the quick brown fox", "hello world", "FOX wins"}, nil)
	tool := NewReadMemoryTool(newEnvWith(p))

	out, err := tool.Execute(context.Background(), `{"query":"fox"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	resp := decodeMemoryResult(t, out)
	if resp.Query != "fox" {
		t.Errorf("query = %q, want fox", resp.Query)
	}
	if resp.Count != 2 {
		t.Errorf("count = %d, want 2 (raw: %s)", resp.Count, out)
	}
}

func TestReadMemoryTool_Execute_TargetFilter(t *testing.T) {
	p := newTestProvider(t)
	writer := NewWriteMemoryTool(newEnvWith(p))
	if _, err := writer.Execute(context.Background(), `{"target":"memory","content":"Project uses make install","tags":["project"]}`); err != nil {
		t.Fatalf("write memory: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := writer.Execute(context.Background(), `{"target":"user","content":"User prefers concise Chinese replies","tags":["preference"]}`); err != nil {
		t.Fatalf("write user: %v", err)
	}

	reader := NewReadMemoryTool(newEnvWith(p))
	out, err := reader.Execute(context.Background(), `{"target":"user","limit":5}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	resp := decodeMemoryResult(t, out)
	if resp.Target != "user" {
		t.Fatalf("target echo = %q, want user", resp.Target)
	}
	if resp.Count != 1 || resp.Entries[0].Content != "User prefers concise Chinese replies" || resp.Entries[0].Target != "user" {
		t.Fatalf("target-filtered entries = %+v", resp.Entries)
	}
	if testTagsMatch(resp.Entries[0].Tags, []string{"target:user"}) {
		t.Fatalf("internal target tag leaked to tool output: %+v", resp.Entries[0].Tags)
	}
}

func TestReadMemoryTool_Execute_TagLimitAndSince(t *testing.T) {
	p := newTestProvider(t)
	if _, err := p.Store(context.Background(), store.Entry{Content: "old", Tags: []string{"x", "y"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	cutoff := time.Now().UTC()
	time.Sleep(30 * time.Millisecond)
	if _, err := p.Store(context.Background(), store.Entry{Content: "new-a", Tags: []string{"x", "y"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := p.Store(context.Background(), store.Entry{Content: "new-b", Tags: []string{"x", "y"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Store(context.Background(), store.Entry{Content: "wrong-tag", Tags: []string{"x"}}); err != nil {
		t.Fatal(err)
	}

	tool := NewReadMemoryTool(newEnvWith(p))
	args := fmt.Sprintf(`{"tags":["x","y"],"since":%q,"limit":1}`, cutoff.Format(time.RFC3339Nano))
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	resp := decodeMemoryResult(t, out)
	if resp.Count != 1 || len(resp.Entries) != 1 || resp.Entries[0].Content != "new-b" {
		t.Fatalf("filtered result = %+v", resp.Entries)
	}
}

func TestReadMemoryTool_Execute_SearchWithSince(t *testing.T) {
	p := newTestProvider(t)
	seedProvider(t, p, []string{"old fox"}, nil)
	time.Sleep(30 * time.Millisecond)
	cutoff := time.Now().UTC()
	time.Sleep(30 * time.Millisecond)
	seedProvider(t, p, []string{"new fox"}, nil)

	tool := NewReadMemoryTool(newEnvWith(p))
	args := fmt.Sprintf(`{"query":"fox","since":%q}`, cutoff.Format(time.RFC3339Nano))
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	resp := decodeMemoryResult(t, out)
	if resp.Count != 1 || resp.Entries[0].Content != "new fox" {
		t.Fatalf("search since result = %+v", resp.Entries)
	}
}

func TestReadMemoryTool_Execute_ErrorBranches(t *testing.T) {
	if _, err := NewReadMemoryTool(&Env{}).Execute(context.Background(), `{}`); !errors.Is(err, errMemoryUnavailable) {
		t.Fatalf("no provider err = %v, want errMemoryUnavailable", err)
	}
	if _, err := NewReadMemoryTool(nil).Execute(context.Background(), `{}`); !errors.Is(err, errMemoryUnavailable) {
		t.Fatalf("nil env err = %v, want errMemoryUnavailable", err)
	}

	p := newTestProvider(t)
	tool := NewReadMemoryTool(newEnvWith(p))
	if _, err := tool.Execute(context.Background(), `{"since":"yesterday"}`); err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("bad since err = %v", err)
	}
	if _, err := tool.Execute(context.Background(), `{"target":"session"}`); err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("bad target err = %v", err)
	}
	if _, err := tool.Execute(context.Background(), `{`); err == nil {
		t.Fatal("expected malformed JSON error")
	}

	if _, err := NewReadMemoryTool(newEnvWith(&stubProvider{recallErr: fmt.Errorf("io error")})).Execute(context.Background(), `{}`); err == nil || !strings.Contains(err.Error(), "io error") {
		t.Fatalf("recall provider err = %v", err)
	}
	if _, err := NewReadMemoryTool(newEnvWith(&stubProvider{searchErr: fmt.Errorf("index corrupted")})).Execute(context.Background(), `{"query":"x"}`); err == nil || !strings.Contains(err.Error(), "index corrupted") {
		t.Fatalf("search provider err = %v", err)
	}
}

func TestNewMemoryTools(t *testing.T) {
	tools := NewMemoryTools(newEnvWith(newTestProvider(t)))
	if len(tools) != 2 {
		t.Fatalf("NewMemoryTools returned %d tools, want 2", len(tools))
	}
	want := []string{"read_memory", "write_memory"}
	for i, tool := range tools {
		if got := tool.Name(); got != want[i] {
			t.Errorf("tools[%d].Name() = %q, want %q", i, got, want[i])
		}
		if def := tool.Definition(); def.Name != tool.Name() {
			t.Errorf("Definition().Name = %q, Name() = %q", def.Name, tool.Name())
		}
	}

	nilTools := NewMemoryTools(nil)
	if len(nilTools) != 2 {
		t.Fatalf("NewMemoryTools(nil) returned %d tools, want 2", len(nilTools))
	}
}

func TestMemoryTools_ReadOnlyClassification(t *testing.T) {
	env := newEnvWith(newTestProvider(t))
	if !NewReadMemoryTool(env).IsReadOnly() {
		t.Error("read_memory should be read-only")
	}
	if NewWriteMemoryTool(env).IsReadOnly() {
		t.Error("write_memory must not be read-only")
	}
}

func TestMemoryTools_EndToEndWithFileProvider(t *testing.T) {
	dir := t.TempDir()

	writer := NewWriteMemoryTool(newEnvWith(mustProvider(t, dir)))
	out, err := writer.Execute(context.Background(), `{"target":"user","content":"end-to-end test","tags":["e2e"],"source":"user"}`)
	if err != nil {
		t.Fatalf("writer.Execute: %v", err)
	}
	if !strings.Contains(out, `"written":true`) {
		t.Fatalf("missing written:true in result: %s", out)
	}

	reader := NewReadMemoryTool(newEnvWith(mustProvider(t, dir)))
	out, err = reader.Execute(context.Background(), `{"target":"user","tags":["e2e"]}`)
	if err != nil {
		t.Fatalf("reader.Execute: %v", err)
	}
	resp := decodeMemoryResult(t, out)
	if resp.Count != 1 || resp.Entries[0].Content != "end-to-end test" || resp.Entries[0].Source != "user" || resp.Entries[0].Target != "user" {
		t.Fatalf("readback result = %+v", resp.Entries)
	}

	data, err := os.ReadFile(filepath.Join(dir, "entries.jsonl"))
	if err != nil {
		t.Fatalf("read entries.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("entries.jsonl has %d lines, want 1", len(lines))
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("entries.jsonl line is not JSON: %v", err)
	}
	if rec["content"] != "end-to-end test" {
		t.Fatalf("on-disk content = %v", rec["content"])
	}
}
