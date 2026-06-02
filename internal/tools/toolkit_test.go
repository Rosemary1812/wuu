package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	proc "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	return string(content)
}

func TestToolkit_WriteAndReadFile(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	writeResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"dir/a.txt","content":"hello"}`,
	})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if !strings.Contains(writeResp, "written_bytes") {
		t.Fatalf("unexpected write response: %s", writeResp)
	}

	readResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"dir/a.txt"}`,
	})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(readResp, "hello") {
		t.Fatalf("unexpected read response: %s", readResp)
	}
}

func TestToolkit_ReadFileStreamsLargeFileRange(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var big strings.Builder
	for i := 1; i <= 5000; i++ {
		fmt.Fprintf(&big, "line-%04d %s\n", i, strings.Repeat("x", 80))
	}
	if big.Len() <= defaultMaxFileBytes {
		t.Fatalf("fixture must exceed max file bytes: got %d", big.Len())
	}
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(big.String()), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"big.txt"}`,
	})
	if err == nil {
		t.Fatal("expected no-limit read to reject oversized file")
	}
	if !strings.Contains(err.Error(), "too large") || !strings.Contains(err.Error(), "offset and limit") {
		t.Fatalf("expected oversized guidance, got: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"big.txt","offset":3001,"limit":3}`,
	})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}

	var parsed struct {
		Content    string `json:"content"`
		NumLines   int    `json:"num_lines"`
		StartLine  int    `json:"start_line"`
		TotalLines int    `json:"total_lines"`
		Truncated  bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if parsed.NumLines != 3 || parsed.StartLine != 3001 || parsed.TotalLines != 5000 || !parsed.Truncated {
		t.Fatalf("unexpected metadata: %+v", parsed)
	}
	for _, want := range []string{"  3001\tline-3001", "  3002\tline-3002", "  3003\tline-3003"} {
		if !strings.Contains(parsed.Content, want) {
			t.Fatalf("expected content to include %q, got: %q", want, parsed.Content)
		}
	}
	for _, unwanted := range []string{"line-3000", "line-3004"} {
		if strings.Contains(parsed.Content, unwanted) {
			t.Fatalf("content included line outside requested range %q: %q", unwanted, parsed.Content)
		}
	}
}

func TestToolkit_ReadFileRejectsDirectory(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"dir"}`,
	})
	if err == nil {
		t.Fatal("expected directory rejection")
	}
	if !strings.Contains(err.Error(), "path is a directory") {
		t.Fatalf("expected directory guidance, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Use list_files") {
		t.Fatalf("expected list_files guidance, got: %v", err)
	}
}

func TestToolkit_EditFileRejectsStaleRead(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"a.txt"}`,
	}); err != nil {
		t.Fatalf("read_file: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	originalModTime := info.ModTime()

	if err := os.WriteFile(path, []byte("bravo\n"), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	if err := os.Chtimes(path, originalModTime, originalModTime); err != nil {
		t.Fatalf("preserve modtime: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"a.txt","old_text":"bravo","new_text":"BRAVO"}`,
	})
	if err == nil {
		t.Fatal("expected stale-read rejection")
	}
	if !strings.Contains(err.Error(), "changed since last read") {
		t.Fatalf("expected stale-read guidance, got: %v", err)
	}

	readResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"a.txt"}`,
	})
	if err != nil {
		t.Fatalf("read_file after stale rejection: %v", err)
	}
	if !strings.Contains(readResp, "bravo") || strings.Contains(readResp, `"unchanged":true`) {
		t.Fatalf("expected fresh read after external edit, got: %s", readResp)
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"a.txt","old_text":"bravo","new_text":"BRAVO"}`,
	}); err != nil {
		t.Fatalf("edit_file after fresh read: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	if string(got) != "BRAVO\n" {
		t.Fatalf("unexpected final content: %q", got)
	}
}

func TestToolkit_ApplyPatchEditsAddsDeletesAndMoves(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetEditToolMode(EditToolModePatch)

	mustWriteFile(t, filepath.Join(root, "a.txt"), "line one\nline two\nline three\n")
	mustWriteFile(t, filepath.Join(root, "remove.txt"), "remove me\n")
	mustWriteFile(t, filepath.Join(root, "oldname.txt"), "old name\n")

	patchText := `*** Begin Patch
*** Update File: a.txt
@@
 line one
-line two
+line 2
 line three
*** Add File: dir/new.txt
+created
*** Delete File: remove.txt
*** Update File: oldname.txt
*** Move to: renamed.txt
@@
-old name
+new name
*** End Patch`
	args, err := json.Marshal(map[string]string{"patchText": patchText})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "apply_patch",
		Arguments: string(args),
	})
	if err != nil {
		t.Fatalf("apply_patch: %v", err)
	}
	if !strings.Contains(resp, `"action":"update"`) || !strings.Contains(resp, `"action":"add"`) ||
		!strings.Contains(resp, `"action":"delete"`) || !strings.Contains(resp, `"action":"move"`) {
		t.Fatalf("expected per-file actions in response: %s", resp)
	}

	if got := mustReadFile(t, filepath.Join(root, "a.txt")); got != "line one\nline 2\nline three\n" {
		t.Fatalf("unexpected updated content: %q", got)
	}
	if got := mustReadFile(t, filepath.Join(root, "dir/new.txt")); got != "created\n" {
		t.Fatalf("unexpected added content: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "remove.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected remove.txt to be deleted, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "oldname.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected oldname.txt to be moved, stat err=%v", err)
	}
	if got := mustReadFile(t, filepath.Join(root, "renamed.txt")); got != "new name\n" {
		t.Fatalf("unexpected moved content: %q", got)
	}
}

func TestToolkit_ApplyPatchRejectsAmbiguousUpdate(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetEditToolMode(EditToolModePatch)
	mustWriteFile(t, filepath.Join(root, "a.txt"), "same\nsame\n")

	args, err := json.Marshal(map[string]string{"patchText": `*** Begin Patch
*** Update File: a.txt
@@
-same
+different
*** End Patch`})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "apply_patch",
		Arguments: string(args),
	})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous update error, got %v", err)
	}
}

func TestToolkit_ApplyPatchAppendsAtEndOfFile(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetEditToolMode(EditToolModePatch)
	mustWriteFile(t, filepath.Join(root, "a.txt"), "first\n")

	args, err := json.Marshal(map[string]string{"patchText": `*** Begin Patch
*** Update File: a.txt
@@
*** End of File
+second
*** End Patch`})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "apply_patch",
		Arguments: string(args),
	}); err != nil {
		t.Fatalf("apply_patch: %v", err)
	}
	if got := mustReadFile(t, filepath.Join(root, "a.txt")); got != "first\nsecond\n" {
		t.Fatalf("unexpected appended content: %q", got)
	}
}

func TestToolkit_ListFilesRejectsFile(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "list_files",
		Arguments: `{"path":"a.txt"}`,
	})
	if err == nil {
		t.Fatal("expected file rejection")
	}
	if !strings.Contains(err.Error(), "path is not a directory") {
		t.Fatalf("expected file guidance, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Use read_file") {
		t.Fatalf("expected read_file guidance, got: %v", err)
	}
}

func TestToolkit_PathEscapeBlocked(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"../secret.txt"}`,
	})
	if err == nil {
		t.Fatal("expected path escape error")
	}
}

// fakeAskBridge is a stub AskUserBridge for tests — it returns a
// canned response for any request without involving a GUI client.
type fakeAskBridge struct {
	resp tools_internal_response // sentinel; set by helper below
}

type tools_internal_response struct {
	answers map[string]string
}

func (f *fakeAskBridge) AskUser(_ context.Context, req AskUserRequest) (AskUserResponse, error) {
	answers := map[string]string{}
	for _, q := range req.Questions {
		// Always pick the first option in tests.
		if len(q.Options) > 0 {
			answers[q.Question] = q.Options[0].Label
		}
	}
	return AskUserResponse{Answers: answers}, nil
}

func TestToolkit_GrepIncludeMatchesRelativePaths(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	withRGTestHooks(t, func(string) (string, error) { return "", exec.ErrNotFound }, nil)

	if err != nil {
		t.Fatalf("New: %v", err)
	}

	files := map[string]string{
		"internal/a.go":   "package internal\nvar target = true\n",
		"internal/a.txt":  "target\n",
		"src/app/main.ts": "const target = true;\n",
		"src/app/util.js": "const target = true;\n",
		"pkg/nested/x.go": "package nested\nvar target = true\n",
		"main.go":         "package main\nvar target = true\n",
	}
	for path, content := range files {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "grep",
		Arguments: `{"pattern":"target","include":"internal/*.go"}`,
	})
	if err != nil {
		t.Fatalf("grep internal/*.go: %v", err)
	}
	var parsed struct {
		Matches []struct {
			File string `json:"file"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse grep response: %v", err)
	}
	if len(parsed.Matches) != 1 || parsed.Matches[0].File != "internal/a.go" {
		t.Fatalf("unexpected matches for internal/*.go: %+v", parsed.Matches)
	}

	resp, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "grep",
		Arguments: `{"pattern":"target","include":"src/**/*.ts"}`,
	})
	if err != nil {
		t.Fatalf("grep src/**/*.ts: %v", err)
	}
	parsed.Matches = nil
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse grep response: %v", err)
	}
	if len(parsed.Matches) != 1 || parsed.Matches[0].File != "src/app/main.ts" {
		t.Fatalf("unexpected matches for src/**/*.ts: %+v", parsed.Matches)
	}
}

func TestToolkit_GrepReturnsScannerErrors(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	withRGTestHooks(t, func(string) (string, error) { return "", exec.ErrNotFound }, nil)

	if err != nil {
		t.Fatalf("New: %v", err)
	}

	path := filepath.Join(root, "huge.txt")
	tooLongLine := strings.Repeat("a", bufio.MaxScanTokenSize+1)
	if err := os.WriteFile(path, []byte(tooLongLine), 0o644); err != nil {
		t.Fatalf("write huge.txt: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "grep",
		Arguments: `{"pattern":"needle","path":"huge.txt"}`,
	})
	if err == nil {
		t.Fatal("expected scanner error")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("expected bufio.ErrTooLong, got: %v", err)
	}
}

func TestToolkit_AskUser_RegisteredInDefinitions(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defs := kit.Definitions()
	found := false
	for _, d := range defs {
		if d.Name == "ask_user" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("ask_user must be present in tool definitions")
	}
}

func TestToolkit_AskUser_FailsWithoutBridge(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Don't call SetAskUserBridge — simulates a worker toolkit.
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "ask_user",
		Arguments: `{"questions":[{"question":"Which?","header":"Pick","options":[{"label":"A","description":"a"},{"label":"B","description":"b"}]}]}`,
	})
	if err == nil {
		t.Fatal("expected error when bridge is not configured (worker isolation)")
	}
	if !strings.Contains(err.Error(), "main agent") {
		t.Fatalf("expected isolation message, got: %v", err)
	}
}

func TestToolkit_AskUser_RoutesThroughBridge(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetAskUserBridge(&fakeAskBridge{})

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "ask_user",
		Arguments: `{"questions":[{"question":"Which auth?","header":"Auth","options":[{"label":"OAuth","description":"delegate"},{"label":"JWT","description":"self-signed"}]}]}`,
	})
	if err != nil {
		t.Fatalf("ask_user: %v", err)
	}
	var parsed AskUserResponse
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Answers["Which auth?"] != "OAuth" {
		t.Fatalf("expected first option, got %v", parsed.Answers)
	}
}

func TestToolkit_AskUser_ValidatesInput(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetAskUserBridge(&fakeAskBridge{})

	// Header too long should error before reaching the bridge.
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "ask_user",
		Arguments: `{"questions":[{"question":"Q?","header":"this header is way too long","options":[{"label":"A","description":"a"},{"label":"B","description":"b"}]}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "header") {
		t.Fatalf("expected header validation error, got: %v", err)
	}
}

func TestToolkit_TaskAddressedAgentTools_RegisteredInDefinitions(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want := map[string]bool{
		"send_message":  false,
		"followup_task": false,
		"wait_agent":    false,
		"await_agents":  false,
		"close_agent":   false,
		"agent_report":  false,
	}
	defs := kit.Definitions()
	for _, d := range defs {
		if d.Name == "send_message_to_agent" || d.Name == "stop_agent" {
			t.Fatalf("legacy agent tool %s must not be registered", d.Name)
		}
		if _, ok := want[d.Name]; ok {
			if strings.Contains(strings.ToLower(d.Description), "currently unavailable") {
				t.Fatalf("%s description should not say unavailable: %q", d.Name, d.Description)
			}
			want[d.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("%s must be present in tool definitions", name)
		}
	}
}

func TestToolkit_ListAgents_RegisteredInDefinitions(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, d := range kit.Definitions() {
		if d.Name == "list_agents" {
			return
		}
	}
	t.Fatal("list_agents must be present in tool definitions")
}

func TestToolkit_UpdatePlan_RegisteredInDefinitions(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, d := range kit.Definitions() {
		if d.Name == "update_plan" {
			return
		}
	}
	t.Fatal("update_plan must be present in tool definitions")
}

func TestToolkit_UpdatePlan_ValidatesSingleInProgress(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "update_plan",
		Arguments: `{"plan":[{"step":"one","status":"in_progress"},{"step":"two","status":"in_progress"}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "only one") {
		t.Fatalf("expected single in_progress validation error, got: %v", err)
	}
}

func TestToolkit_UpdatePlan_RequiresInProgressUntilComplete(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "update_plan",
		Arguments: `{"plan":[{"step":"one","status":"pending"},{"step":"two","status":"pending"}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "must be in_progress") {
		t.Fatalf("expected required in_progress validation error, got: %v", err)
	}

	if _, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "update_plan",
		Arguments: `{"plan":[{"step":"one","status":"completed"},{"step":"two","status":"completed"}]}`,
	}); err != nil {
		t.Fatalf("completed plan should allow zero in_progress: %v", err)
	}
}

func TestToolkit_UpdatePlan_RejectsUnknownFields(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "update_plan",
		Arguments: `{"plan":[{"step":"one","status":"in_progress","extra":true}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field validation error, got: %v", err)
	}
}

func TestToolkit_UpdatePlan_ReturnsConciseResult(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "update_plan",
		Arguments: `{"explanation":"starting work","plan":[{"step":"inspect","status":"completed"},{"step":"edit","status":"in_progress"}]}`,
	})
	if err != nil {
		t.Fatalf("update_plan: %v", err)
	}
	var parsed struct {
		Status      string     `json:"status"`
		Explanation string     `json:"explanation"`
		Plan        []PlanItem `json:"plan"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if parsed.Status != "updated" || parsed.Explanation != "" {
		t.Fatalf("unexpected response metadata: %+v", parsed)
	}
	if len(parsed.Plan) != 0 {
		t.Fatalf("tool result should not echo plan: %+v", parsed.Plan)
	}
}

func TestToolkit_UpdatePlan_StoresCurrentPlan(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "update_plan",
		Arguments: `{"explanation":"starting work","plan":[{"step":"inspect","status":"completed"},{"step":"edit","status":"in_progress"}]}`,
	})
	if err != nil {
		t.Fatalf("update_plan: %v", err)
	}
	got, ok := kit.CurrentPlan()
	if !ok {
		t.Fatal("expected stored plan")
	}
	if got.Explanation != "starting work" || len(got.Plan) != 2 || got.Plan[1].Status != PlanStatusInProgress {
		t.Fatalf("unexpected stored plan: %+v", got)
	}
	got.Plan[1].Status = PlanStatusCompleted
	gotAgain, ok := kit.CurrentPlan()
	if !ok || gotAgain.Plan[1].Status != PlanStatusInProgress {
		t.Fatalf("current plan should return a defensive copy: %+v", gotAgain)
	}
}

func TestToolkit_UpdatePlan_NotifiesPlanUpdated(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var notified PlanSnapshot
	kit.SetOnPlanUpdated(func(snapshot PlanSnapshot) {
		notified = snapshot
	})
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "update_plan",
		Arguments: `{"plan":[{"step":"inspect","status":"in_progress"},{"step":"report","status":"pending"}]}`,
	})
	if err != nil {
		t.Fatalf("update_plan: %v", err)
	}
	if len(notified.Plan) != 2 || notified.Plan[0].Status != PlanStatusInProgress {
		t.Fatalf("unexpected notification: %+v", notified)
	}
}

func TestToolkit_RestorePlanFromHistory(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	history := []providers.ChatMessage{
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{{
				ID:        "call-old",
				Name:      "update_plan",
				Arguments: `{"plan":[{"step":"old","status":"in_progress"}]}`,
			}},
		},
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{{
				ID:        "call-new",
				Name:      "update_plan",
				Arguments: `{"explanation":"latest","plan":[{"step":"inspect","status":"completed"},{"step":"report","status":"in_progress"}]}`,
			}},
		},
	}
	restored, err := kit.RestorePlanFromHistory(history)
	if err != nil {
		t.Fatalf("RestorePlanFromHistory: %v", err)
	}
	if !restored {
		t.Fatal("expected plan to be restored")
	}
	got, ok := kit.CurrentPlan()
	if !ok || got.Explanation != "latest" || len(got.Plan) != 2 || got.Plan[1].Step != "report" {
		t.Fatalf("unexpected restored plan: ok=%v plan=%+v", ok, got)
	}
}

func TestToolkit_RestorePlanFromHistoryDoesNotNotify(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	notified := false
	kit.SetOnPlanUpdated(func(snapshot PlanSnapshot) {
		notified = true
	})
	restored, err := kit.RestorePlanFromHistory([]providers.ChatMessage{{
		Role: "assistant",
		ToolCalls: []providers.ToolCall{{
			ID:        "call-plan",
			Name:      "update_plan",
			Arguments: `{"plan":[{"step":"inspect","status":"in_progress"}]}`,
		}},
	}})
	if err != nil {
		t.Fatalf("RestorePlanFromHistory: %v", err)
	}
	if !restored {
		t.Fatal("expected plan to be restored")
	}
	if notified {
		t.Fatal("restore should not fire fresh plan update notification")
	}
}

func TestToolkit_ToolInfo_ClassifiesBuiltIns(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tests := []struct {
		name            string
		kind            ToolKind
		exposure        ToolExposure
		risk            ToolRisk
		readOnly        bool
		concurrencySafe bool
	}{
		{name: "read_file", kind: ToolKindFile, exposure: ToolExposureDirect, risk: ToolRiskLow, readOnly: true, concurrencySafe: true},
		{name: "tool_search", kind: ToolKindDiscovery, exposure: ToolExposureDirect, risk: ToolRiskLow, readOnly: false, concurrencySafe: false},
		{name: "run_shell", kind: ToolKindShell, exposure: ToolExposureDirect, risk: ToolRiskHigh, readOnly: false, concurrencySafe: false},
		{name: "spawn_agent", kind: ToolKindAgent, exposure: ToolExposureDirect, risk: ToolRiskHigh, readOnly: false, concurrencySafe: true},
		{name: "wait_agent", kind: ToolKindAgent, exposure: ToolExposureDirect, risk: ToolRiskMedium, readOnly: true, concurrencySafe: true},
		{name: "await_agents", kind: ToolKindAgent, exposure: ToolExposureDirect, risk: ToolRiskMedium, readOnly: true, concurrencySafe: true},
		{name: "close_agent", kind: ToolKindAgent, exposure: ToolExposureDirect, risk: ToolRiskHigh, readOnly: false, concurrencySafe: true},
		{name: "write_stdin", kind: ToolKindProcess, exposure: ToolExposureDirect, risk: ToolRiskHigh, readOnly: false, concurrencySafe: true},
		{name: "schedule_cron", kind: ToolKindSchedule, exposure: ToolExposureDeferred, risk: ToolRiskHigh, readOnly: false, concurrencySafe: false},
		{name: "update_plan", kind: ToolKindPlan, exposure: ToolExposureDirect, risk: ToolRiskLow, readOnly: false, concurrencySafe: false},
		{name: "apply_patch", kind: ToolKindFile, exposure: ToolExposureHidden, risk: ToolRiskHigh, readOnly: false, concurrencySafe: false},
	}
	for _, tt := range tests {
		info, ok := kit.ToolInfo(tt.name)
		if !ok {
			t.Fatalf("ToolInfo(%q) not found", tt.name)
		}
		if info.Name != tt.name || info.Kind != tt.kind || info.Exposure != tt.exposure || info.Risk != tt.risk ||
			info.ReadOnly != tt.readOnly || info.ConcurrencySafe != tt.concurrencySafe {
			t.Fatalf("ToolInfo(%q) = %+v, want kind=%s exposure=%s risk=%s readOnly=%t concurrencySafe=%t",
				tt.name, info, tt.kind, tt.exposure, tt.risk, tt.readOnly, tt.concurrencySafe)
		}
	}

	if _, ok := kit.ToolInfo("not_a_tool"); ok {
		t.Fatal("unknown tool should not return metadata")
	}
}

func TestToolkit_EditToolModeForModelMatchesOpenCodeRule(t *testing.T) {
	tests := []struct {
		model string
		want  EditToolMode
	}{
		{model: "gpt-5.5", want: EditToolModePatch},
		{model: "openai/gpt-5-codex", want: EditToolModePatch},
		{model: "gpt-4.1-mini", want: EditToolModeText},
		{model: "openai/gpt-oss-120b", want: EditToolModeText},
		{model: "anthropic/claude-sonnet-4-5", want: EditToolModeText},
	}
	for _, tt := range tests {
		if got := EditToolModeForModel(tt.model); got != tt.want {
			t.Fatalf("EditToolModeForModel(%q) = %s, want %s", tt.model, got, tt.want)
		}
	}
}

func TestToolkit_EditToolModeControlsDefinitionsAndExecution(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	defs := definitionNames(kit.Definitions())
	if defs["apply_patch"] {
		t.Fatal("apply_patch should be hidden in default text edit mode")
	}
	if !defs["edit_file"] || !defs["write_file"] {
		t.Fatalf("edit_file and write_file should be visible in text edit mode: %+v", defs)
	}

	kit.ConfigureEditToolsForModel("gpt-5.5")
	defs = definitionNames(kit.Definitions())
	if !defs["apply_patch"] {
		t.Fatal("apply_patch should be visible for GPT patch edit mode")
	}
	if defs["edit_file"] || defs["write_file"] {
		t.Fatalf("edit_file and write_file should be hidden in patch edit mode: %+v", defs)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{Name: "edit_file", Arguments: `{}`})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected hidden edit_file to be blocked, got %v", err)
	}

	kit.ConfigureEditToolsForModel("claude-sonnet-4-5")
	defs = definitionNames(kit.Definitions())
	if defs["apply_patch"] || !defs["edit_file"] || !defs["write_file"] {
		t.Fatalf("text edit mode should restore edit_file/write_file and hide apply_patch: %+v", defs)
	}
}

func TestToolkit_ToolMetadata_ClassifiesGitByInput(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	meta, ok := kit.ToolMetadata(providers.ToolCall{
		Name:      "git",
		Arguments: `{"subcommand":"status"}`,
	})
	if !ok {
		t.Fatal("git metadata not found")
	}
	if !meta.ReadOnly || !meta.ConcurrencySafe || meta.Risk != string(ToolRiskLow) {
		t.Fatalf("git status metadata = %+v, want read-only low-risk concurrent", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "git",
		Arguments: `{"subcommand":"push"}`,
	})
	if !ok {
		t.Fatal("git metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || !meta.Destructive || meta.Risk != string(ToolRiskHigh) {
		t.Fatalf("git push metadata = %+v, want destructive high-risk serial", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "git",
		Arguments: `{"subcommand":"branch","args":["new-branch"]}`,
	})
	if !ok {
		t.Fatal("git metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Risk != string(ToolRiskHigh) {
		t.Fatalf("invalid git branch metadata = %+v, want conservative high-risk serial", meta)
	}
}

func TestToolkit_ToolMetadata_ClassifiesShellByInput(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	meta, ok := kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"ls -la"}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if !meta.ReadOnly || !meta.ConcurrencySafe || meta.Risk != string(ToolRiskLow) {
		t.Fatalf("ls metadata = %+v, want read-only low-risk concurrent", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"echo hi > out.txt"}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Risk != string(ToolRiskHigh) {
		t.Fatalf("redirecting shell metadata = %+v, want high-risk serial", meta)
	}
}

func TestToolkit_ToolInfos_IncludesHiddenDisabledTools(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.DisableTools("run_shell")

	info, ok := kit.ToolInfo("run_shell")
	if !ok {
		t.Fatal("disabled known tool should still return metadata")
	}
	if info.Exposure != ToolExposureHidden {
		t.Fatalf("disabled tool exposure = %s, want %s", info.Exposure, ToolExposureHidden)
	}

	found := false
	for _, info := range kit.ToolInfos() {
		if info.Name == "run_shell" {
			found = true
			if info.Exposure != ToolExposureHidden {
				t.Fatalf("ToolInfos run_shell exposure = %s, want %s", info.Exposure, ToolExposureHidden)
			}
		}
	}
	if !found {
		t.Fatal("ToolInfos should include hidden disabled tools")
	}

	for _, d := range kit.Definitions() {
		if d.Name == "run_shell" {
			t.Fatal("hidden disabled tool should not appear in definitions")
		}
	}
}

func TestToolkit_DefersLowFrequencyAndMCPToolsFromDefinitions(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.registry = NewRegistry(
		NewReadFileTool(kit.env),
		NewToolSearchTool(kit),
		NewScheduleCronTool(kit.env),
		&stubTool{
			name: "mcp_docs_search",
			def:  providers.ToolDefinition{Name: "mcp_docs_search", Description: "Search docs through MCP"},
		},
	)

	defs := definitionNames(kit.Definitions())
	for _, name := range []string{"read_file", "tool_search"} {
		if !defs[name] {
			t.Fatalf("%s should be directly exposed", name)
		}
	}
	for _, name := range []string{"schedule_cron", "mcp_docs_search"} {
		if defs[name] {
			t.Fatalf("%s should be deferred from definitions", name)
		}
		info, ok := kit.ToolInfo(name)
		if !ok {
			t.Fatalf("ToolInfo(%q) not found", name)
		}
		if info.Exposure != ToolExposureDeferred {
			t.Fatalf("%s exposure = %s, want %s", name, info.Exposure, ToolExposureDeferred)
		}
	}
}

func TestToolkit_ToolSearchActivatesDeferredTool(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.registry = NewRegistry(
		NewToolSearchTool(kit),
		&stubTool{
			name: "mcp_docs_search",
			def:  providers.ToolDefinition{Name: "mcp_docs_search", Description: "Search docs through MCP"},
		},
	)

	if definitionNames(kit.Definitions())["mcp_docs_search"] {
		t.Fatal("mcp_docs_search should start deferred")
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "mcp_docs_search",
		Arguments: `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "deferred") {
		t.Fatalf("expected deferred execution error, got %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "tool_search",
		Arguments: `{"query":"docs search"}`,
	})
	if err != nil {
		t.Fatalf("tool_search: %v", err)
	}
	var parsed struct {
		ExposedTools []string `json:"exposed_tools"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse tool_search response: %v", err)
	}
	if !reflect.DeepEqual(parsed.ExposedTools, []string{"mcp_docs_search"}) {
		t.Fatalf("exposed tools = %+v, want mcp_docs_search", parsed.ExposedTools)
	}
	if !definitionNames(kit.Definitions())["mcp_docs_search"] {
		t.Fatal("mcp_docs_search should be exposed after tool_search")
	}
	info, ok := kit.ToolInfo("mcp_docs_search")
	if !ok {
		t.Fatal("ToolInfo(mcp_docs_search) not found")
	}
	if info.Exposure != ToolExposureDirect {
		t.Fatalf("mcp_docs_search exposure = %s, want %s", info.Exposure, ToolExposureDirect)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{Name: "mcp_docs_search", Arguments: `{}`}); err != nil {
		t.Fatalf("activated deferred tool should execute: %v", err)
	}
}

func TestToolkit_ToolTelemetry_RecordsSuccess(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-read",
		Name:      "read_file",
		Arguments: `{"path":"a.txt"}`,
	})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}

	records := kit.ToolTelemetry()
	if len(records) != 1 {
		t.Fatalf("expected 1 telemetry record, got %d", len(records))
	}
	record := records[0]
	if record.Name != "read_file" || record.CallID != "call-read" {
		t.Fatalf("unexpected record identity: %+v", record)
	}
	if record.Kind != ToolKindFile || record.Exposure != ToolExposureDirect {
		t.Fatalf("unexpected record classification: %+v", record)
	}
	if record.Risk != ToolRiskLow || record.PolicyAction != ToolPolicyAllow {
		t.Fatalf("unexpected policy metadata: %+v", record)
	}
	if !record.ReadOnly || !record.ConcurrencySafe || !record.Success || record.Error != "" {
		t.Fatalf("unexpected record status: %+v", record)
	}
	if record.StartedAt.IsZero() || record.DurationMS < 0 {
		t.Fatalf("unexpected timing: %+v", record)
	}
	if record.RawOutputBytes != len(resp) || record.ReturnedOutputBytes != len(resp) || record.ResultBudgeted {
		t.Fatalf("unexpected output sizing: %+v response_len=%d", record, len(resp))
	}
}

func TestToolkit_ToolTelemetry_RecordsToolError(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-missing",
		Name:      "read_file",
		Arguments: `{"path":"missing.txt"}`,
	})
	if err == nil {
		t.Fatal("expected read_file error")
	}

	records := kit.ToolTelemetry()
	if len(records) != 1 {
		t.Fatalf("expected 1 telemetry record, got %d", len(records))
	}
	record := records[0]
	if record.Name != "read_file" || record.CallID != "call-missing" {
		t.Fatalf("unexpected record identity: %+v", record)
	}
	if record.Success || record.Error == "" {
		t.Fatalf("expected failed telemetry record with error, got %+v", record)
	}
	if record.RawOutputBytes != 0 || record.ReturnedOutputBytes != 0 || record.ResultBudgeted {
		t.Fatalf("unexpected failed output sizing: %+v", record)
	}
}

func TestToolkit_ToolPolicy_DeniesByRisk(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetToolPolicy(ToolPolicy{
		RiskActions: map[ToolRisk]ToolPolicyAction{
			ToolRiskHigh: ToolPolicyDeny,
		},
	})

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-shell",
		Name:      "run_shell",
		Arguments: `{"command":"printf hi"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "denied by policy") {
		t.Fatalf("expected policy denial, got %v", err)
	}

	records := kit.ToolTelemetry()
	if len(records) != 1 {
		t.Fatalf("expected 1 telemetry record, got %d", len(records))
	}
	record := records[0]
	if record.Name != "run_shell" || record.Risk != ToolRiskHigh || record.PolicyAction != ToolPolicyDeny {
		t.Fatalf("unexpected policy record: %+v", record)
	}
	if record.Success || record.Error == "" || record.RawOutputBytes != 0 || record.ReturnedOutputBytes != 0 {
		t.Fatalf("denied tool should be recorded as failed without output: %+v", record)
	}
}

func TestToolkit_ToolPolicy_ToolOverrideBeatsRisk(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetToolPolicy(ToolPolicy{
		ToolActions: map[string]ToolPolicyAction{
			"run_shell": ToolPolicyAllow,
		},
		RiskActions: map[ToolRisk]ToolPolicyAction{
			ToolRiskHigh: ToolPolicyDeny,
		},
	})

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-shell",
		Name:      "run_shell",
		Arguments: `{"command":"printf hi"}`,
	})
	if err != nil {
		t.Fatalf("run_shell: %v", err)
	}
	if !strings.Contains(resp, "hi") {
		t.Fatalf("unexpected response: %s", resp)
	}

	records := kit.ToolTelemetry()
	if len(records) != 1 {
		t.Fatalf("expected 1 telemetry record, got %d", len(records))
	}
	if records[0].PolicyAction != ToolPolicyAllow || !records[0].Success {
		t.Fatalf("unexpected policy record: %+v", records[0])
	}
}

func TestToolkit_ToolPolicy_UsesInputClassification(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetToolPolicy(ToolPolicy{
		RiskActions: map[ToolRisk]ToolPolicyAction{
			ToolRiskHigh: ToolPolicyDeny,
		},
	})

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-pwd",
		Name:      "run_shell",
		Arguments: `{"command":"pwd"}`,
	}); err != nil {
		t.Fatalf("read-only shell command should not be denied as high risk: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-write",
		Name:      "run_shell",
		Arguments: `{"command":"echo hi > out.txt"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "denied by policy") {
		t.Fatalf("write-like shell command should be denied by high-risk policy, got %v", err)
	}
}

func TestToolkit_RunShellDefinition_RequiresNonInteractiveCommands(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defs := kit.Definitions()
	for _, d := range defs {
		if d.Name != "run_shell" {
			continue
		}
		if !strings.Contains(strings.ToLower(d.Description), "non-interactive") {
			t.Fatalf("run_shell description must mention non-interactive use: %q", d.Description)
		}
		props, ok := d.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("run_shell schema properties missing or wrong type: %#v", d.InputSchema["properties"])
		}
		commandProp, ok := props["command"].(map[string]any)
		if !ok {
			t.Fatalf("run_shell command schema missing or wrong type: %#v", props["command"])
		}
		desc, _ := commandProp["description"].(string)
		if !strings.Contains(strings.ToLower(desc), "non-interactive") {
			t.Fatalf("run_shell command description must mention non-interactive use: %q", desc)
		}
		return
	}
	t.Fatal("run_shell must be present in tool definitions")
}

func TestToolkit_ReadProcessOutputWaitsFromOffset(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	manager, err := proc.NewManager(root, filepath.Join(root, "state", "runtime"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	kit.SetProcessManager(manager)
	p, err := manager.Start(context.Background(), proc.StartOptions{
		Command:   "sleep 0.2; printf ready; sleep 1",
		OwnerKind: proc.OwnerMainAgent,
		OwnerID:   "main",
		Lifecycle: proc.LifecycleSession,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer manager.Stop(p.ID)

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_process_output",
		Arguments: `{"process_id":"` + p.ID + `","offset_bytes":0,"wait_ms":2000}`,
	})
	if err != nil {
		t.Fatalf("read_process_output: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if parsed["timed_out"].(bool) {
		t.Fatalf("expected output before timeout: %+v", parsed)
	}
	if !strings.Contains(parsed["output"].(string), "ready") {
		t.Fatalf("unexpected output: %v", parsed["output"])
	}
	if parsed["end_offset"].(float64) <= 0 || parsed["total_bytes"].(float64) != parsed["end_offset"].(float64) {
		t.Fatalf("unexpected offsets: %+v", parsed)
	}
	if parsed["status"].(string) == "" {
		t.Fatalf("missing status: %+v", parsed)
	}
}

func TestToolkit_StartProcessSupportsTTY(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	manager, err := proc.NewManager(root, filepath.Join(root, "state", "runtime"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	kit.SetProcessManager(manager)

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "start_process",
		Arguments: `{"command":"if test -t 1; then echo MODE_TTY; else echo MODE_PIPE; fi; sleep 1","owner_kind":"main_agent","lifecycle":"session","tty":true}`,
	})
	if err != nil {
		t.Fatalf("start_process: %v", err)
	}
	var started proc.Process
	if err := json.Unmarshal([]byte(resp), &started); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	defer manager.Stop(started.ID)
	if !started.TTY {
		t.Fatalf("expected tty process metadata: %+v", started)
	}

	outResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_process_output",
		Arguments: `{"process_id":"` + started.ID + `","offset_bytes":0,"wait_ms":2000}`,
	})
	if err != nil {
		t.Fatalf("read_process_output: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(outResp), &parsed); err != nil {
		t.Fatalf("parse output response: %v", err)
	}
	if !strings.Contains(parsed["output"].(string), "MODE_TTY") {
		t.Fatalf("expected TTY output, got %+v", parsed)
	}
	processMeta := parsed["process"].(map[string]any)
	if processMeta["tty"] != true {
		t.Fatalf("expected nested process tty metadata: %+v", processMeta)
	}
}

func TestToolkit_SpawnAgentDefinitionIncludesForkTurns(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, d := range kit.Definitions() {
		if d.Name != "spawn_agent" {
			continue
		}
		props, _ := d.InputSchema["properties"].(map[string]any)
		if _, ok := props["fork_turns"]; !ok {
			t.Fatalf("spawn_agent schema must expose fork_turns: %#v", d.InputSchema)
		}
		return
	}
	t.Fatal("spawn_agent must be present in tool definitions")
}

func TestToolkit_SpawnAgentDescriptionDoesNotForceStopAfterSpawn(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, d := range kit.Definitions() {
		if d.Name != "spawn_agent" {
			continue
		}
		if strings.Contains(d.Description, "END YOUR TURN") {
			t.Fatalf("spawn_agent description must not force stopping after async spawn: %q", d.Description)
		}
		for _, want := range []string{"non-overlapping", "Do not loop checking status", "synchronous=true"} {
			if !strings.Contains(d.Description, want) {
				t.Fatalf("spawn_agent description missing %q: %q", want, d.Description)
			}
		}
		return
	}
	t.Fatal("spawn_agent must be present in tool definitions")
}

func TestToolkit_SpawnAgentDescriptionIncludesDelegationDecisionRules(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, d := range kit.Definitions() {
		if d.Name != "spawn_agent" {
			continue
		}
		for _, want := range []string{
			"delegation materially improves",
			"Keep work local",
			"concrete brief",
			"destructive or broad experiments",
			"overlapping or uncertain concurrent writes",
			"generated outputs/formatters",
			"fork_turns='none'",
			"Preserve fork_turns='all'",
			"user intent",
			"prior analysis",
		} {
			if !strings.Contains(d.Description, want) {
				t.Fatalf("spawn_agent description missing decision guidance %q: %q", want, d.Description)
			}
		}
		props, _ := d.InputSchema["properties"].(map[string]any)
		for field, wants := range map[string][]string{
			"message":    {"Concrete task brief", "acceptance criteria", "fully self-contained"},
			"isolation":  {"destructive or broad experiments", "overlapping or uncertain concurrent writes", "explicit sandbox requests"},
			"fork_turns": {"inherited user intent", "fully self-contained", "recent context"},
		} {
			prop, _ := props[field].(map[string]any)
			desc, _ := prop["description"].(string)
			for _, want := range wants {
				if !strings.Contains(desc, want) {
					t.Fatalf("spawn_agent %s description missing %q: %q", field, want, desc)
				}
			}
		}
		return
	}
	t.Fatal("spawn_agent must be present in tool definitions")
}

func TestToolkit_WaitAgentUsesV2MailboxSchema(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, d := range kit.Definitions() {
		if d.Name != "wait_agent" {
			continue
		}
		props, _ := d.InputSchema["properties"].(map[string]any)
		if _, ok := props["timeout_ms"]; !ok {
			t.Fatalf("wait_agent schema must expose timeout_ms: %#v", d.InputSchema)
		}
		if _, ok := props["target"]; ok {
			t.Fatalf("wait_agent v2 schema must not expose target: %#v", d.InputSchema)
		}
		if _, ok := d.InputSchema["required"]; ok {
			t.Fatalf("wait_agent v2 schema must not require fields: %#v", d.InputSchema)
		}
		return
	}
	t.Fatal("wait_agent must be present in tool definitions")
}

func TestToolkit_SpawnAgent_FailsWithoutAgentControl(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Don't call SetAgentControl — simulates a worker toolkit.
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "spawn_agent",
		Arguments: `{"task_name":"test","message":"do thing"}`,
	})
	if err == nil {
		t.Fatal("expected error when agent control is not configured")
	}
	if !strings.Contains(err.Error(), "agent control not configured") {
		t.Fatalf("expected agent-control-not-configured error, got: %v", err)
	}
}

func TestWrapForkPrompt_OverridesParentReadOnlyClaims(t *testing.T) {
	prompt := wrapForkPrompt("fix the bug")
	if !strings.Contains(prompt, "main interactive") || !strings.Contains(prompt, "read-only") {
		t.Fatalf("fork override must cancel inherited main-agent read-only guidance: %q", prompt)
	}
	if !strings.Contains(prompt, "If a tool is in") {
		t.Fatalf("fork override must restore worker authority to use its tools: %q", prompt)
	}
	if !strings.Contains(prompt, "call agent_report exactly once") || !strings.Contains(prompt, "evidence/artifact paths") {
		t.Fatalf("fork override must preserve structured handoff discipline: %q", prompt)
	}
}

func TestStripDanglingToolUses(t *testing.T) {
	// Last message is an assistant turn with tool_calls — should be stripped.
	with := []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u"},
		{Role: "assistant", Content: "ok", ToolCalls: []providers.ToolCall{{Name: "spawn_agent"}}},
	}
	got := stripDanglingToolUses(with)
	if len(got) != 2 {
		t.Fatalf("expected last assistant w/ tool_calls stripped, got %d msgs", len(got))
	}
	if got[len(got)-1].Role != "user" {
		t.Fatalf("expected last remaining message to be user, got %s", got[len(got)-1].Role)
	}

	// Last message is a tool result — should NOT be stripped (the
	// previous tool_use already has its matching result).
	clean := []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u"},
		{Role: "assistant", Content: "ok", ToolCalls: []providers.ToolCall{{Name: "read_file"}}},
		{Role: "tool", Name: "read_file", Content: "result"},
	}
	got = stripDanglingToolUses(clean)
	if len(got) != 4 {
		t.Fatalf("clean history should pass through unchanged, got %d msgs", len(got))
	}

	// Last message is an assistant turn WITHOUT tool_calls — should
	// not be stripped (it's a normal text reply).
	textOnly := []providers.ChatMessage{
		{Role: "user", Content: "u"},
		{Role: "assistant", Content: "ok"},
	}
	got = stripDanglingToolUses(textOnly)
	if len(got) != 2 {
		t.Fatalf("text-only assistant should not be stripped, got %d msgs", len(got))
	}

	// Empty history — should pass through.
	if got := stripDanglingToolUses(nil); got != nil {
		t.Fatal("nil history should pass through unchanged")
	}
}

func TestParseSpawnForkTurns(t *testing.T) {
	mode, n, err := parseSpawnForkTurns("", nil)
	if err != nil || mode != spawnForkAll || n != 0 {
		t.Fatalf("default fork turns = mode %d n %d err %v", mode, n, err)
	}
	mode, _, err = parseSpawnForkTurns("none", nil)
	if err != nil || mode != spawnForkNone {
		t.Fatalf("none fork turns = mode %d err %v", mode, err)
	}
	mode, n, err = parseSpawnForkTurns("3", nil)
	if err != nil || mode != spawnForkLastN || n != 3 {
		t.Fatalf("last-n fork turns = mode %d n %d err %v", mode, n, err)
	}
	if _, _, err := parseSpawnForkTurns("0", nil); err == nil {
		t.Fatal("expected zero fork_turns to fail")
	}
	legacy := true
	if _, _, err := parseSpawnForkTurns("", &legacy); err == nil {
		t.Fatal("expected fork_context to fail")
	}
}

func TestTruncateHistoryToLastUserTurnsPreservesSystemPrefix(t *testing.T) {
	history := []providers.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "u3"},
	}
	got := truncateHistoryToLastUserTurns(history, 2)
	if len(got) != 4 {
		t.Fatalf("expected system + last two user turns, got %d: %+v", len(got), got)
	}
	if got[0].Role != "system" || got[1].Content != "u2" || got[3].Content != "u3" {
		t.Fatalf("unexpected truncated history: %+v", got)
	}
}

func definitionNames(defs []providers.ToolDefinition) map[string]bool {
	out := make(map[string]bool, len(defs))
	for _, def := range defs {
		out[def.Name] = true
	}
	return out
}

func TestToolkit_DisableTools_HidesDefinitionsAndBlocksExecute(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.DisableTools("write_file", "edit_file", "run_shell")

	defs := kit.Definitions()
	for _, d := range defs {
		if d.Name == "write_file" || d.Name == "edit_file" || d.Name == "run_shell" {
			t.Fatalf("disabled tool %q should not appear in definitions", d.Name)
		}
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"a.txt","content":"x"}`,
	})
	if err == nil {
		t.Fatal("expected disabled write_file to error")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"echo hi"}`,
	})
	if err == nil {
		t.Fatal("expected disabled run_shell to error")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got: %v", err)
	}
}

func TestToolkit_RunShell(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"echo hi"}`,
	})
	if err != nil {
		t.Fatalf("run_shell: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if parsed["exit_code"].(float64) != 0 {
		t.Fatalf("unexpected exit code: %v", parsed["exit_code"])
	}
	if !strings.Contains(parsed["output"].(string), "hi") {
		t.Fatalf("unexpected output: %v", parsed["output"])
	}
	if !strings.Contains(parsed["stdout_tail"].(string), "hi") {
		t.Fatalf("unexpected stdout tail: %v", parsed["stdout_tail"])
	}
	if parsed["stderr_tail"].(string) != "" {
		t.Fatalf("unexpected stderr tail: %v", parsed["stderr_tail"])
	}
	if parsed["duration_ms"].(float64) < 0 {
		t.Fatalf("unexpected duration: %v", parsed["duration_ms"])
	}
}

func TestToolkit_RunShellStructuredFailureOutput(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"printf out; printf err >&2; exit 7"}`,
	})
	if err != nil {
		t.Fatalf("run_shell: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if parsed["exit_code"].(float64) != 7 {
		t.Fatalf("unexpected exit code: %v", parsed["exit_code"])
	}
	if got := parsed["stdout_tail"].(string); got != "out" {
		t.Fatalf("stdout_tail = %q, want out", got)
	}
	if got := parsed["stderr_tail"].(string); got != "err" {
		t.Fatalf("stderr_tail = %q, want err", got)
	}
	if got := parsed["stdout_bytes"].(float64); got != 3 {
		t.Fatalf("stdout_bytes = %v, want 3", got)
	}
	if got := parsed["stderr_bytes"].(float64); got != 3 {
		t.Fatalf("stderr_bytes = %v, want 3", got)
	}
	if parsed["stdout_tail_truncated"].(bool) || parsed["stderr_tail_truncated"].(bool) {
		t.Fatalf("short output should not be tail-truncated: %+v", parsed)
	}
}

func TestToolkit_RunShellSetsNonInteractiveEnv(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"printf '%s|%s|%s|%s|%s|%s|%s|%s' \"$GIT_EDITOR\" \"$GIT_SEQUENCE_EDITOR\" \"$EDITOR\" \"$VISUAL\" \"$PAGER\" \"$GIT_PAGER\" \"$GH_PAGER\" \"$GIT_TERMINAL_PROMPT\""}`,
	})
	if err != nil {
		t.Fatalf("run_shell: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if got := parsed["output"].(string); got != "true|true|true|true|cat|cat|cat|0" {
		t.Fatalf("unexpected shell env: %q", got)
	}
}

func TestToolkit_RunShellGitCommitEditUsesNonInteractiveEditor(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cmd := strings.Join([]string{
		"git init -q",
		"git config user.email test@example.com",
		"git config user.name test",
		"printf 'one\\n' > note.txt",
		"git add note.txt",
		"git commit -qm init",
		"printf 'two\\n' > note.txt",
		"git add note.txt",
		"git commit -e -m second",
		"git log -1 --format=%s",
	}, " && ")
	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"` + cmd + `","timeout_seconds":10}`,
	})
	if err != nil {
		t.Fatalf("run_shell: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if parsed["exit_code"].(float64) != 0 {
		t.Fatalf("unexpected exit code: %v output=%q", parsed["exit_code"], parsed["output"])
	}
	lines := strings.Split(strings.TrimSpace(parsed["output"].(string)), "\n")
	if got := lines[len(lines)-1]; got != "second" {
		t.Fatalf("unexpected git log output: %q", got)
	}
}

type execCommandFunc func(context.Context, string, ...string) *exec.Cmd

func withRGTestHooks(t *testing.T, lookup func(string) (string, error), cmd execCommandFunc) {
	t.Helper()
	origLookup := rgLookupPath
	origCmd := rgCommand
	rgLookupPath = lookup
	if cmd != nil {
		rgCommand = cmd
	}
	resetRGForTests()
	t.Cleanup(func() {
		rgLookupPath = origLookup
		rgCommand = origCmd
		resetRGForTests()
	})
}

func TestToolkit_GlobRipgrepIncludesHiddenFiles(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for path, content := range map[string]string{
		".env":          "TOKEN=abc\n",
		"visible.env":   "TOKEN=visible\n",
		"dir/.env.test": "TOKEN=nested\n",
	} {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "glob",
		Arguments: `{"pattern":"*.env*"}`,
	})
	if err != nil {
		t.Fatalf("glob *.env*: %v", err)
	}
	var parsed struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse glob response: %v", err)
	}
	if !reflect.DeepEqual(parsed.Files, []string{".env", "dir/.env.test", "visible.env"}) {
		t.Fatalf("unexpected hidden glob matches: %+v", parsed.Files)
	}
}

func TestToolkit_GrepRipgrepIncludesHiddenFiles(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for path, content := range map[string]string{
		".env":        "API_KEY=secret\n",
		"visible.env": "API_KEY=visible\n",
	} {
		fullPath := filepath.Join(root, path)
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "grep",
		Arguments: `{"pattern":"API_KEY","include":"*.env"}`,
	})
	if err != nil {
		t.Fatalf("grep *.env: %v", err)
	}
	var parsed struct {
		Matches []grepMatch `json:"matches"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse grep response: %v", err)
	}
	want := []grepMatch{{File: ".env", Line: 1, Content: "API_KEY=secret"}, {File: "visible.env", Line: 1, Content: "API_KEY=visible"}}
	if !reflect.DeepEqual(parsed.Matches, want) {
		t.Fatalf("unexpected hidden grep matches: got %+v want %+v", parsed.Matches, want)
	}
}

func TestToolkit_GlobRipgrepFirst(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	files := map[string]string{
		"src/app/main.ts": "export const main = true\n",
		"src/lib/util.ts": "export const util = true\n",
		"src/lib/util.js": "export const util = true\n",
		"README.md":       "# readme\n",
	}
	for path, content := range files {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "glob",
		Arguments: `{"pattern":"*.md"}`,
	})
	if err != nil {
		t.Fatalf("glob *.md: %v", err)
	}
	var parsed struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse glob response: %v", err)
	}
	if len(parsed.Files) != 1 || parsed.Files[0] != "README.md" {
		t.Fatalf("unexpected matches for *.md: %+v", parsed.Files)
	}

	resp, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "glob",
		Arguments: `{"pattern":"src/**/*.ts"}`,
	})
	if err != nil {
		t.Fatalf("glob src/**/*.ts: %v", err)
	}
	parsed.Files = nil
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse glob response: %v", err)
	}
	want := []string{"src/app/main.ts", "src/lib/util.ts"}
	if !reflect.DeepEqual(parsed.Files, want) {
		t.Fatalf("unexpected matches for src/**/*.ts: got %+v want %+v", parsed.Files, want)
	}
}

func TestToolkit_GlobFallbackWithoutRG(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	withRGTestHooks(t, func(string) (string, error) { return "", exec.ErrNotFound }, nil)

	for path, content := range map[string]string{
		"src/app/main.ts": "main\n",
		"src/app/main.js": "main\n",
	} {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "glob",
		Arguments: `{"pattern":"src/**/*.ts"}`,
	})
	if err != nil {
		t.Fatalf("glob fallback: %v", err)
	}
	var parsed struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse glob response: %v", err)
	}
	if !reflect.DeepEqual(parsed.Files, []string{"src/app/main.ts"}) {
		t.Fatalf("unexpected fallback matches: %+v", parsed.Files)
	}
}

func TestToolkit_GlobFallbackIncludesHiddenDirectories(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	withRGTestHooks(t, func(string) (string, error) { return "", exec.ErrNotFound }, nil)

	for path, content := range map[string]string{
		".hidden/config/app.yaml": "name: hidden\n",
		"visible/config/app.yaml": "name: visible\n",
		".git/config/app.yaml":    "name: skipped\n",
	} {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "glob",
		Arguments: `{"pattern":"**/*.yaml"}`,
	})
	if err != nil {
		t.Fatalf("glob hidden fallback: %v", err)
	}
	var parsed struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse glob response: %v", err)
	}
	want := []string{".hidden/config/app.yaml", "visible/config/app.yaml"}
	if !reflect.DeepEqual(parsed.Files, want) {
		t.Fatalf("unexpected hidden fallback matches: got %+v want %+v", parsed.Files, want)
	}
}

func TestToolkit_GrepFallbackIncludesHiddenDirectories(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	withRGTestHooks(t, func(string) (string, error) { return "", exec.ErrNotFound }, nil)

	for path, content := range map[string]string{
		".hidden/app.env":    "API_KEY=hidden\n",
		"visible/app.env":    "API_KEY=visible\n",
		"node_modules/x.env": "API_KEY=skipped\n",
	} {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "grep",
		Arguments: `{"pattern":"API_KEY","include":"**/*.env"}`,
	})
	if err != nil {
		t.Fatalf("grep hidden fallback: %v", err)
	}
	var parsed struct {
		Matches []grepMatch `json:"matches"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse grep response: %v", err)
	}
	want := []grepMatch{
		{File: ".hidden/app.env", Line: 1, Content: "API_KEY=hidden"},
		{File: "visible/app.env", Line: 1, Content: "API_KEY=visible"},
	}
	if !reflect.DeepEqual(parsed.Matches, want) {
		t.Fatalf("unexpected hidden grep fallback matches: got %+v want %+v", parsed.Matches, want)
	}
}

func TestToolkit_GrepIncludeMatchesRelativePaths_Ripgrep(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	files := map[string]string{
		"internal/a.go":   "package internal\nvar target = true\n",
		"internal/a.txt":  "target\n",
		"src/app/main.ts": "const target = true;\n",
		"src/app/util.js": "const target = true;\n",
	}
	for path, content := range files {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "grep",
		Arguments: `{"pattern":"target","include":"internal/*.go"}`,
	})
	if err != nil {
		t.Fatalf("grep internal/*.go: %v", err)
	}
	var parsed struct {
		Matches []grepMatch `json:"matches"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse grep response: %v", err)
	}
	if len(parsed.Matches) != 1 || parsed.Matches[0].File != "internal/a.go" {
		t.Fatalf("unexpected matches for internal/*.go: %+v", parsed.Matches)
	}

	resp, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "grep",
		Arguments: `{"pattern":"target","include":"src/**/*.ts"}`,
	})
	if err != nil {
		t.Fatalf("grep src/**/*.ts: %v", err)
	}
	parsed.Matches = nil
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse grep response: %v", err)
	}
	if len(parsed.Matches) != 1 || parsed.Matches[0].File != "src/app/main.ts" {
		t.Fatalf("unexpected matches for src/**/*.ts: %+v", parsed.Matches)
	}
}
