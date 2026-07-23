package appserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/memdir"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
)

// newMemoryPanelServer builds a server whose runtime has a real WuuHome so
// the memory-panel scope resolution has notebooks to point at.
func newMemoryPanelServer(t *testing.T, client *fakeClient) (*Server, string) {
	t.Helper()
	rt := newTestRuntime(t, client)
	srv := New(rt, &lockedBuffer{})
	rt.WuuHome = t.TempDir()
	return srv, rt.WuuHome
}

func saveNamedParticipant(t *testing.T, rt *runtime.Session, name, role, model string) string {
	t.Helper()
	now := time.Now().UTC()
	p := participant.Participant{
		ID:        participant.NewID(),
		Kind:      participant.KindNamed,
		Name:      name,
		Role:      role,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := session.UpsertParticipant(rt.SessionDir, p); err != nil {
		t.Fatalf("upsert participant: %v", err)
	}
	return p.ID
}

func callMemoryRPC(t *testing.T, srv *Server, id, method, paramsJSON string) map[string]any {
	t.Helper()
	raw := fmt.Sprintf(`{"id":%q,"method":%q,"params":%s}`, id, method, paramsJSON)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), id)
}

func memoryRPCError(t *testing.T, resp map[string]any) string {
	t.Helper()
	errVal, ok := resp["error"]
	if !ok {
		t.Fatalf("expected an error response, got %+v", resp)
	}
	return fmt.Sprint(errVal)
}

func writeMemoryFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestMemoryReadUserScopeInventory(t *testing.T) {
	srv, home := newMemoryPanelServer(t, &fakeClient{})
	userDir := memdir.UserMemdir(home)
	index := "- [用户角色](user-role.md) — 后端工程师\n"
	writeMemoryFile(t, userDir, "MEMORY.md", index)
	writeMemoryFile(t, userDir, "user-role.md",
		"---\nname: user-role\ndescription: 用户是后端工程师\ntype: user\n---\n\n后端工程师，主攻 Go。\n")
	writeMemoryFile(t, userDir, "plain.md", "no frontmatter here\n")
	writeMemoryFile(t, userDir, "notes.txt", "not markdown")
	writeMemoryFile(t, userDir, ".hidden.md", "hidden")
	writeMemoryFile(t, userDir, "old.md.migrated", "retired source")

	resp := callMemoryRPC(t, srv, "read-1", MethodMemoryRead, `{"scope":"user"}`)
	result := remarshal[MemoryReadResult](t, resp["result"])
	if result.IndexMD != index {
		t.Fatalf("index_md = %q, want the raw MEMORY.md content %q", result.IndexMD, index)
	}
	if len(result.Files) != 2 {
		t.Fatalf("files = %+v, want exactly the two topic .md files", result.Files)
	}
	// Sorted by name: fallback name "plain" (no frontmatter) < "user-role".
	if result.Files[0].Name != "plain" || result.Files[0].Description != "" || result.Files[0].Type != "" {
		t.Fatalf("frontmatter-less file should degrade to file-name fallback: %+v", result.Files[0])
	}
	got := result.Files[1]
	if got.Name != "user-role" || got.Description != "用户是后端工程师" || got.Type != "user" {
		t.Fatalf("frontmatter not surfaced: %+v", got)
	}
	for _, f := range result.Files {
		if _, err := time.Parse(time.RFC3339, f.Mtime); err != nil {
			t.Fatalf("mtime %q is not RFC3339: %v", f.Mtime, err)
		}
	}
}

func TestMemoryReadMissingNotebookIsEmptyNotError(t *testing.T) {
	srv, _ := newMemoryPanelServer(t, &fakeClient{})

	resp := callMemoryRPC(t, srv, "read-empty", MethodMemoryRead, `{"scope":"user"}`)
	rawResult, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected a result, got %+v", resp)
	}
	if rawResult["index_md"] != "" {
		t.Fatalf("index_md should be empty for a missing notebook: %+v", rawResult)
	}
	files, ok := rawResult["files"].([]any)
	if !ok {
		t.Fatalf(`files must be a JSON array (never null) for the renderer: %+v`, rawResult["files"])
	}
	if len(files) != 0 {
		t.Fatalf("files should be empty: %+v", files)
	}
}

func TestMemoryReadParticipantScope(t *testing.T) {
	srv, home := newMemoryPanelServer(t, &fakeClient{})
	ivy := saveNamedParticipant(t, srv.rt, "Ivy", "reviewer", "")
	notebook := memdir.ParticipantMemdir(home, ivy)
	index := "- [代码评审教训](review.md) — 先跑测试再点赞\n"
	writeMemoryFile(t, notebook, "MEMORY.md", index)
	writeMemoryFile(t, notebook, "review.md",
		"---\nname: review\ndescription: 评审前先跑测试\ntype: lesson\n---\n\n先跑测试。\n")

	resp := callMemoryRPC(t, srv, "read-p", MethodMemoryRead,
		fmt.Sprintf(`{"scope":"participant","participant_id":%q}`, ivy))
	result := remarshal[MemoryReadResult](t, resp["result"])
	if result.IndexMD != index {
		t.Fatalf("index_md = %q, want %q", result.IndexMD, index)
	}
	if len(result.Files) != 1 || result.Files[0].Name != "review" || result.Files[0].Type != "lesson" {
		t.Fatalf("unexpected inventory: %+v", result.Files)
	}
}

// The scope checks are shared by all three memory RPCs through
// (*Server).memoryNotebook; memory/read is the cheapest way to exercise the
// whole table end-to-end.
func TestMemoryReadScopeValidation(t *testing.T) {
	srv, _ := newMemoryPanelServer(t, &fakeClient{})
	retired := saveNamedParticipant(t, srv.rt, "Old", "reviewer", "")
	if err := session.RetireParticipant(srv.rt.SessionDir, retired); err != nil {
		t.Fatalf("retire participant: %v", err)
	}
	ephemeral := participant.Participant{
		ID:        participant.NewID(),
		Kind:      participant.KindEphemeral,
		Name:      "Temp",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := session.UpsertParticipant(srv.rt.SessionDir, ephemeral); err != nil {
		t.Fatalf("upsert ephemeral participant: %v", err)
	}

	cases := []struct {
		name    string
		params  string
		wantErr string
	}{
		{"unknown scope", `{"scope":"workspace"}`, "unknown memory scope"},
		{"empty scope", `{"scope":""}`, "unknown memory scope"},
		{"user scope with participant_id", `{"scope":"user","participant_id":"prt-x"}`, `only valid with scope "participant"`},
		{"participant scope without id", `{"scope":"participant"}`, "participant_id is required"},
		{"participant not found", `{"scope":"participant","participant_id":"prt-missing"}`, "participant not found"},
		{"retired participant", fmt.Sprintf(`{"scope":"participant","participant_id":%q}`, retired), "retired"},
		{"not a named agent", fmt.Sprintf(`{"scope":"participant","participant_id":%q}`, ephemeral.ID), "not a named agent"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := callMemoryRPC(t, srv, fmt.Sprintf("read-val-%d", i), MethodMemoryRead, tc.params)
			if errStr := memoryRPCError(t, resp); !containsString(errStr, tc.wantErr) {
				t.Fatalf("error = %q, want it to mention %q", errStr, tc.wantErr)
			}
		})
	}
}

func TestMemoryReadRequiresWuuHome(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	srv := New(rt, &lockedBuffer{}) // WuuHome stays empty

	resp := callMemoryRPC(t, srv, "read-nohome", MethodMemoryRead, `{"scope":"user"}`)
	if errStr := memoryRPCError(t, resp); !containsString(errStr, "wuu home") {
		t.Fatalf("error = %q, want it to mention the missing wuu home", errStr)
	}
}

func TestDiffMemorySnapshots(t *testing.T) {
	home := t.TempDir()
	dir := memdir.UserMemdir(home)
	keep := writeMemoryFile(t, dir, "keep.md", "unchanged")
	change := writeMemoryFile(t, dir, "change.md", "old body")
	gone := writeMemoryFile(t, dir, "gone.md", "to be deleted")
	// Age the to-be-modified file so a same-second rewrite is still caught
	// by the size component of the stamp.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(change, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	_ = keep

	before := snapshotMemoryRoots([]string{dir})
	if err := os.WriteFile(change, []byte("new body, different length"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}
	writeMemoryFile(t, dir, "new.md", "fresh")
	after := snapshotMemoryRoots([]string{dir})

	changed := diffMemorySnapshots(before, after, home)
	want := []MemoryChangedFile{
		{Path: "memory/change.md", Action: "modified"},
		{Path: "memory/gone.md", Action: "deleted"},
		{Path: "memory/new.md", Action: "created"},
	}
	if len(changed) != len(want) {
		t.Fatalf("changed = %+v, want %+v", changed, want)
	}
	for i := range want {
		if changed[i] != want[i] {
			t.Fatalf("changed[%d] = %+v, want %+v", i, changed[i], want[i])
		}
	}

	// No changes → empty (not nil) list.
	if again := diffMemorySnapshots(after, after, home); again == nil || len(again) != 0 {
		t.Fatalf("no-op diff should be an empty non-nil slice, got %+v", again)
	}
}

func containsString(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func requestToolNames(req providers.ChatRequest) []string {
	names := make([]string, 0, len(req.Tools))
	for _, def := range req.Tools {
		names = append(names, def.Name)
	}
	sort.Strings(names)
	return names
}

func requestMessagesContain(req providers.ChatRequest, needle string) bool {
	for _, msg := range req.Messages {
		if strings.Contains(msg.Content, needle) {
			return true
		}
	}
	return false
}

func TestMemoryOverviewCachesForTwelveHoursAndSupportsForcedRefresh(t *testing.T) {
	essay := "## 身份背景\n后端工程师。\n\n## 协作偏好\n喜欢简短回复。\n\n## 沟通风格\n直接。\n\n## 当前关注\n记忆系统重构。"
	client := &fakeClient{response: providersResponse(essay)}
	srv, home := newMemoryPanelServer(t, client)
	userDir := memdir.UserMemdir(home)
	indexPath := writeMemoryFile(t, userDir, "MEMORY.md", "- [用户角色](user-role.md) — 后端工程师\n")

	resp := callMemoryRPC(t, srv, "ov-1", MethodMemoryOverview, `{"scope":"user"}`)
	result := remarshal[MemoryOverviewResult](t, resp["result"])
	if result.EssayMD != essay || result.Cached {
		t.Fatalf("first overview should be freshly generated: %+v", result)
	}
	if _, err := time.Parse(time.RFC3339, result.GeneratedAt); err != nil {
		t.Fatalf("generated_at %q is not RFC3339: %v", result.GeneratedAt, err)
	}
	if _, err := time.Parse(time.RFC3339, result.SourceMtime); err != nil {
		t.Fatalf("source_mtime %q is not RFC3339: %v", result.SourceMtime, err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("expected exactly one model call, got %d", len(client.requests))
	}
	req := client.requests[0]
	wantTools := []string{"glob", "list_files", "read_file"}
	if got := requestToolNames(req); strings.Join(got, ",") != strings.Join(wantTools, ",") {
		t.Fatalf("overview toolset = %v, want read-only %v", got, wantTools)
	}
	system := req.Messages[0].Content
	for _, section := range []string{"## 身份背景", "## 协作偏好", "## 沟通风格", "## 当前关注"} {
		if !strings.Contains(system, section) {
			t.Fatalf("user-scope system prompt is missing template section %q:\n%s", section, system)
		}
	}
	if !requestMessagesContain(req, "用户角色") {
		t.Fatalf("index content should be injected into the first request: %+v", req.Messages)
	}

	// Second call with an unchanged index: served from cache, no new model call.
	resp2 := callMemoryRPC(t, srv, "ov-2", MethodMemoryOverview, `{"scope":"user"}`)
	result2 := remarshal[MemoryOverviewResult](t, resp2["result"])
	if !result2.Cached || result2.EssayMD != essay {
		t.Fatalf("second overview should be a cache hit: %+v", result2)
	}
	if result2.GeneratedAt != result.GeneratedAt || result2.SourceMtime != result.SourceMtime {
		t.Fatalf("cache hit must return the original generation metadata: %+v vs %+v", result2, result)
	}
	if len(client.requests) != 1 {
		t.Fatalf("cache hit must not call the model again, got %d calls", len(client.requests))
	}

	// Index mtime moves during the 12-hour window → opening the panel still
	// serves the cached essay and does not spend another inference.
	if err := os.WriteFile(indexPath, []byte("- [新条目](new.md) — 新钩子\n"), 0o644); err != nil {
		t.Fatalf("rewrite index: %v", err)
	}
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(indexPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	resp3 := callMemoryRPC(t, srv, "ov-3", MethodMemoryOverview, `{"scope":"user"}`)
	result3 := remarshal[MemoryOverviewResult](t, resp3["result"])
	if !result3.Cached || result3.EssayMD != essay {
		t.Fatalf("index change inside the cooldown should keep the cache: %+v", result3)
	}
	if len(client.requests) != 1 {
		t.Fatalf("cooldown cache must not call the model again, got %d calls", len(client.requests))
	}

	// The explicit panel action bypasses the automatic cooldown.
	resp4 := callMemoryRPC(t, srv, "ov-4", MethodMemoryOverview, `{"scope":"user","force_refresh":true}`)
	result4 := remarshal[MemoryOverviewResult](t, resp4["result"])
	if result4.Cached {
		t.Fatalf("forced refresh should generate a fresh overview: %+v", result4)
	}
	if len(client.requests) != 2 {
		t.Fatalf("forced refresh should make a second model call, got %d", len(client.requests))
	}
}

func TestMemoryOverviewCacheSurvivesServerRestart(t *testing.T) {
	client := &fakeClient{response: providersResponse("持久化概览")}
	srv, home := newMemoryPanelServer(t, client)
	writeMemoryFile(t, memdir.UserMemdir(home), "MEMORY.md", "- [条目](topic.md) — 钩子\n")

	first := remarshal[MemoryOverviewResult](t,
		callMemoryRPC(t, srv, "persist-1", MethodMemoryOverview, `{"scope":"user"}`)["result"])
	if first.Cached || len(client.requests) != 1 {
		t.Fatalf("first overview should be generated: result=%+v calls=%d", first, len(client.requests))
	}

	restartedClient := &fakeClient{response: providersResponse("不应生成")}
	restartedRuntime := newTestRuntime(t, restartedClient)
	restarted := New(restartedRuntime, &lockedBuffer{})
	restartedRuntime.WuuHome = home
	second := remarshal[MemoryOverviewResult](t,
		callMemoryRPC(t, restarted, "persist-2", MethodMemoryOverview, `{"scope":"user"}`)["result"])
	if !second.Cached || second.EssayMD != first.EssayMD {
		t.Fatalf("restart should reuse the persisted cache: %+v", second)
	}
	if len(restartedClient.requests) != 0 {
		t.Fatalf("restart cache hit must not call the model, got %d calls", len(restartedClient.requests))
	}
}

func TestMemoryOverviewParticipantScopeSectionsAndFileScope(t *testing.T) {
	essay := "## 与用户的相处之道\n简短。\n\n## 协作教训\n先跑测试。\n\n## 技艺笔记\nGo。\n\n## 承诺与定案\n无。"
	srv, home := newMemoryPanelServer(t, &fakeClient{})
	ivy := saveNamedParticipant(t, srv.rt, "Ivy", "reviewer", "")
	userIndexPath := writeMemoryFile(t, memdir.UserMemdir(home), "MEMORY.md", "- [秘密](secret.md) — 用户笔记本内容\n")
	writeMemoryFile(t, memdir.ParticipantMemdir(home, ivy), "MEMORY.md", "- [评审教训](review.md) — 先跑测试\n")

	// First model round tries to read the USER notebook from the
	// participant-scope overview agent — the FileScopeRoots whitelist must
	// reject it; the second round returns the essay.
	fake := &fakeClient{responses: []providers.ChatResponse{
		{ToolCalls: []providers.ToolCall{{
			ID:        "t1",
			Name:      "read_file",
			Arguments: fmt.Sprintf(`{"path":%q}`, userIndexPath),
		}}},
		providersResponse(essay),
	}}
	srv.rt.StreamRunner.Client = providers.AdaptStreamClient(fake)

	resp := callMemoryRPC(t, srv, "ov-p", MethodMemoryOverview,
		fmt.Sprintf(`{"scope":"participant","participant_id":%q}`, ivy))
	result := remarshal[MemoryOverviewResult](t, resp["result"])
	if result.EssayMD != essay || result.Cached {
		t.Fatalf("unexpected participant overview result: %+v", result)
	}
	if len(fake.requests) != 2 {
		t.Fatalf("expected two model rounds, got %d", len(fake.requests))
	}
	system := fake.requests[0].Messages[0].Content
	for _, section := range []string{"## 与用户的相处之道", "## 协作教训", "## 技艺笔记", "## 承诺与定案"} {
		if !strings.Contains(system, section) {
			t.Fatalf("participant-scope system prompt is missing section %q:\n%s", section, system)
		}
	}
	if !requestMessagesContain(fake.requests[1], "outside the allowed file scope") {
		t.Fatalf("reading the user notebook from participant scope must be rejected by FileScopeRoots; second request: %+v",
			fake.requests[1].Messages)
	}
}

func TestMemoryOverviewEmptyNotebook(t *testing.T) {
	emptyEssay := "这本笔记本还没有积累记忆——多与 agent 协作，它就会开始记录。"
	client := &fakeClient{response: providersResponse(emptyEssay)}
	srv, _ := newMemoryPanelServer(t, client)

	resp := callMemoryRPC(t, srv, "ov-empty", MethodMemoryOverview, `{"scope":"user"}`)
	result := remarshal[MemoryOverviewResult](t, resp["result"])
	if result.EssayMD != emptyEssay || result.Cached {
		t.Fatalf("unexpected empty-notebook overview: %+v", result)
	}
	if result.SourceMtime != "" {
		t.Fatalf("source_mtime should be empty when MEMORY.md does not exist, got %q", result.SourceMtime)
	}
	if len(client.requests) != 1 || !requestMessagesContain(client.requests[0], "索引为空") {
		t.Fatalf("empty index marker should be injected: %+v", client.requests)
	}
}

func TestMemoryChatUserScopeSavesMemoryAndReportsChangedFiles(t *testing.T) {
	topic := "---\nname: reply-style\ndescription: 用户喜欢简短回复\ntype: user\n---\n\n喜欢简短的中文回复。\n"
	index := "- [回复风格](reply-style.md) — 喜欢简短回复\n"
	client := &fakeClient{responses: []providers.ChatResponse{
		{ToolCalls: []providers.ToolCall{
			{ID: "t1", Name: "write_file", Arguments: fmt.Sprintf(`{"path":"reply-style.md","content":%q}`, topic)},
			{ID: "t2", Name: "write_file", Arguments: fmt.Sprintf(`{"path":"MEMORY.md","content":%q}`, index)},
		}},
		providersResponse("已保存你的回复风格偏好。"),
	}}
	srv, home := newMemoryPanelServer(t, client)

	resp := callMemoryRPC(t, srv, "chat-1", MethodMemoryChat,
		`{"scope":"user","message":"记住我喜欢简短回复"}`)
	result := remarshal[MemoryChatResult](t, resp["result"])
	if result.ReplyMD != "已保存你的回复风格偏好。" {
		t.Fatalf("reply_md = %q", result.ReplyMD)
	}
	want := []MemoryChangedFile{
		{Path: "memory/MEMORY.md", Action: "created"},
		{Path: "memory/reply-style.md", Action: "created"},
	}
	if len(result.ChangedFiles) != len(want) {
		t.Fatalf("changed_files = %+v, want %+v", result.ChangedFiles, want)
	}
	for i := range want {
		if result.ChangedFiles[i] != want[i] {
			t.Fatalf("changed_files[%d] = %+v, want %+v", i, result.ChangedFiles[i], want[i])
		}
	}
	userDir := memdir.UserMemdir(home)
	if got, err := os.ReadFile(filepath.Join(userDir, "reply-style.md")); err != nil || string(got) != topic {
		t.Fatalf("topic file on disk = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(userDir, "MEMORY.md")); err != nil || string(got) != index {
		t.Fatalf("index on disk = %q, %v", got, err)
	}

	// Manager toolset and prompt discipline.
	req := client.requests[0]
	wantTools := []string{"edit_file", "glob", "list_files", "read_file", "write_file"}
	if got := requestToolNames(req); strings.Join(got, ",") != strings.Join(wantTools, ",") {
		t.Fatalf("manager toolset = %v, want %v", got, wantTools)
	}
	system := req.Messages[0].Content
	for _, needle := range []string{"What NOT to save", "两步保存纪律", userDir, "中文一句话"} {
		if !strings.Contains(system, needle) {
			t.Fatalf("manager system prompt is missing %q:\n%s", needle, system)
		}
	}
}

func TestMemoryChatReportsModifiedIndex(t *testing.T) {
	client := &fakeClient{}
	srv, home := newMemoryPanelServer(t, client)
	userDir := memdir.UserMemdir(home)
	indexPath := writeMemoryFile(t, userDir, "MEMORY.md",
		"- [回复风格](reply-style.md) — 喜欢简短回复\n- [旧条目](old.md) — 已过时\n")
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(indexPath, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	client.responses = []providers.ChatResponse{
		{ToolCalls: []providers.ToolCall{
			{ID: "t1", Name: "read_file", Arguments: `{"path":"MEMORY.md"}`},
		}},
		{ToolCalls: []providers.ToolCall{
			{ID: "t2", Name: "write_file", Arguments: `{"path":"MEMORY.md","content":"- [回复风格](reply-style.md) — 喜欢简短回复\n"}`},
		}},
		providersResponse("已删除旧条目的索引行。"),
	}

	resp := callMemoryRPC(t, srv, "chat-mod", MethodMemoryChat,
		`{"scope":"user","message":"删掉旧条目"}`)
	result := remarshal[MemoryChatResult](t, resp["result"])
	if len(result.ChangedFiles) != 1 ||
		result.ChangedFiles[0] != (MemoryChangedFile{Path: "memory/MEMORY.md", Action: "modified"}) {
		t.Fatalf("changed_files = %+v, want one modified memory/MEMORY.md", result.ChangedFiles)
	}
}

func TestMemoryChatParticipantScopeRootsAndEscapeRejection(t *testing.T) {
	client := &fakeClient{}
	srv, home := newMemoryPanelServer(t, client)
	ivy := saveNamedParticipant(t, srv.rt, "Ivy", "reviewer", "")
	userDir := memdir.UserMemdir(home)
	notebook := memdir.ParticipantMemdir(home, ivy)
	escapePath := filepath.Join(home, "escape.md")
	client.responses = []providers.ChatResponse{
		{ToolCalls: []providers.ToolCall{
			// Relative path → participant notebook (the chat root dir).
			{ID: "t1", Name: "write_file", Arguments: `{"path":"lesson.md","content":"---\nname: lesson\ndescription: 教训\ntype: lesson\n---\n\n先跑测试。\n"}`},
			// The user notebook is inside the whitelist in participant scope.
			{ID: "t2", Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q,"content":"note\n"}`, filepath.Join(userDir, "user-note.md"))},
			// Outside both notebooks → FileScopeRoots must reject.
			{ID: "t3", Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q,"content":"escaped\n"}`, escapePath)},
		}},
		providersResponse("已为 Ivy 记下教训，并更新了用户笔记。"),
	}

	resp := callMemoryRPC(t, srv, "chat-p", MethodMemoryChat,
		fmt.Sprintf(`{"scope":"participant","participant_id":%q,"message":"记一条教训"}`, ivy))
	result := remarshal[MemoryChatResult](t, resp["result"])
	want := []MemoryChangedFile{
		{Path: "memory/user-note.md", Action: "created"},
		{Path: filepath.ToSlash(filepath.Join("participants", ivy, "memory", "lesson.md")), Action: "created"},
	}
	if len(result.ChangedFiles) != len(want) {
		t.Fatalf("changed_files = %+v, want %+v", result.ChangedFiles, want)
	}
	for i := range want {
		if result.ChangedFiles[i] != want[i] {
			t.Fatalf("changed_files[%d] = %+v, want %+v", i, result.ChangedFiles[i], want[i])
		}
	}
	if _, err := os.Stat(filepath.Join(notebook, "lesson.md")); err != nil {
		t.Fatalf("participant topic file missing: %v", err)
	}
	if _, err := os.Stat(escapePath); !os.IsNotExist(err) {
		t.Fatalf("escape write outside the notebooks must not land, stat err=%v", err)
	}
	if !requestMessagesContain(client.requests[1], "outside the allowed file scope") {
		t.Fatalf("escape write should surface a file-scope rejection to the model: %+v", client.requests[1].Messages)
	}
	if !strings.Contains(client.requests[0].Messages[0].Content, notebook) {
		t.Fatalf("participant-scope manager prompt should name the identity notebook %q", notebook)
	}
}

func TestMemoryChatKeepsOverviewCacheUntilForcedRefresh(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{
		providersResponse("第一版概览"),
		providersResponse("没有需要修改的内容。"),
		providersResponse("第二版概览"),
	}}
	srv, home := newMemoryPanelServer(t, client)
	writeMemoryFile(t, memdir.UserMemdir(home), "MEMORY.md", "- [条目](topic.md) — 钩子\n")

	first := remarshal[MemoryOverviewResult](t,
		callMemoryRPC(t, srv, "inv-1", MethodMemoryOverview, `{"scope":"user"}`)["result"])
	if first.Cached || first.EssayMD != "第一版概览" {
		t.Fatalf("unexpected first overview: %+v", first)
	}
	hit := remarshal[MemoryOverviewResult](t,
		callMemoryRPC(t, srv, "inv-2", MethodMemoryOverview, `{"scope":"user"}`)["result"])
	if !hit.Cached {
		t.Fatalf("second overview should hit the cache: %+v", hit)
	}

	chatResp := callMemoryRPC(t, srv, "inv-chat", MethodMemoryChat, `{"scope":"user","message":"看看有什么"}`)
	rawChat, ok := chatResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected chat result, got %+v", chatResp)
	}
	if files, ok := rawChat["changed_files"].([]any); !ok || len(files) != 0 {
		t.Fatalf(`changed_files must be an empty JSON array when nothing changed: %+v`, rawChat["changed_files"])
	}

	// A chat does not spend another overview inference automatically.
	again := remarshal[MemoryOverviewResult](t,
		callMemoryRPC(t, srv, "inv-3", MethodMemoryOverview, `{"scope":"user"}`)["result"])
	if !again.Cached || again.EssayMD != "第一版概览" {
		t.Fatalf("overview after chat should stay cached: %+v", again)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected 2 model calls (overview, chat), got %d", len(client.requests))
	}

	fresh := remarshal[MemoryOverviewResult](t,
		callMemoryRPC(t, srv, "inv-4", MethodMemoryOverview, `{"scope":"user","force_refresh":true}`)["result"])
	if fresh.Cached || fresh.EssayMD != "第二版概览" {
		t.Fatalf("forced overview after chat should regenerate: %+v", fresh)
	}
	if len(client.requests) != 3 {
		t.Fatalf("expected 3 model calls after forced overview, got %d", len(client.requests))
	}
}

func TestMemoryChatValidation(t *testing.T) {
	client := &fakeClient{response: providersResponse("ok")}
	srv, _ := newMemoryPanelServer(t, client)
	retired := saveNamedParticipant(t, srv.rt, "Old", "reviewer", "")
	if err := session.RetireParticipant(srv.rt.SessionDir, retired); err != nil {
		t.Fatalf("retire participant: %v", err)
	}

	cases := []struct {
		name    string
		params  string
		wantErr string
	}{
		{"empty message", `{"scope":"user","message":"  "}`, "message is required"},
		{"unknown scope", `{"scope":"all","message":"hi"}`, "unknown memory scope"},
		{"participant scope without id", `{"scope":"participant","message":"hi"}`, "participant_id is required"},
		{"participant not found", `{"scope":"participant","participant_id":"prt-missing","message":"hi"}`, "participant not found"},
		{"retired participant", fmt.Sprintf(`{"scope":"participant","participant_id":%q,"message":"hi"}`, retired), "retired"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := callMemoryRPC(t, srv, fmt.Sprintf("chat-val-%d", i), MethodMemoryChat, tc.params)
			if errStr := memoryRPCError(t, resp); !containsString(errStr, tc.wantErr) {
				t.Fatalf("error = %q, want it to mention %q", errStr, tc.wantErr)
			}
		})
	}
	if len(client.requests) != 0 {
		t.Fatalf("validation failures must never reach the model, got %d calls", len(client.requests))
	}
}

func TestMemoryOverviewRejectsInvalidScopes(t *testing.T) {
	client := &fakeClient{response: providersResponse("essay")}
	srv, _ := newMemoryPanelServer(t, client)
	retired := saveNamedParticipant(t, srv.rt, "Old", "reviewer", "")
	if err := session.RetireParticipant(srv.rt.SessionDir, retired); err != nil {
		t.Fatalf("retire participant: %v", err)
	}

	cases := []struct {
		name    string
		params  string
		wantErr string
	}{
		{"unknown scope", `{"scope":"project"}`, "unknown memory scope"},
		{"participant scope without id", `{"scope":"participant"}`, "participant_id is required"},
		{"participant not found", `{"scope":"participant","participant_id":"prt-missing"}`, "participant not found"},
		{"retired participant", fmt.Sprintf(`{"scope":"participant","participant_id":%q}`, retired), "retired"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := callMemoryRPC(t, srv, fmt.Sprintf("ov-val-%d", i), MethodMemoryOverview, tc.params)
			if errStr := memoryRPCError(t, resp); !containsString(errStr, tc.wantErr) {
				t.Fatalf("error = %q, want it to mention %q", errStr, tc.wantErr)
			}
		})
	}
	if len(client.requests) != 0 {
		t.Fatalf("validation failures must never reach the model, got %d calls", len(client.requests))
	}
}
