package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/agentcontrol"
	"github.com/blueberrycongee/wuu/internal/agentthread"
	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	proc "github.com/blueberrycongee/wuu/internal/process"
	"github.com/blueberrycongee/wuu/internal/providers"

	memstore "github.com/blueberrycongee/wuu/internal/memory/store"
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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
	var writeCreated struct {
		WorkspaceRevision string `json:"workspace_revision"`
	}
	if err := json.Unmarshal([]byte(writeResp), &writeCreated); err != nil {
		t.Fatalf("parse write response: %v", err)
	}
	if !strings.HasPrefix(writeCreated.WorkspaceRevision, "fs:worktree:") {
		t.Fatalf("write_file response missing filesystem workspace revision: %+v", writeCreated)
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
	var readParsed struct {
		FileSHA           string `json:"file_sha"`
		WorkspaceRevision string `json:"workspace_revision"`
		Content           string `json:"content"`
		Range             struct {
			StartLine int `json:"start_line"`
			EndLine   int `json:"end_line"`
		} `json:"range"`
		OmittedRanges []map[string]int `json:"omitted_ranges"`
		Suggestions   []string         `json:"next_suggestions"`
	}
	if err := json.Unmarshal([]byte(readResp), &readParsed); err != nil {
		t.Fatalf("parse read response: %v", err)
	}
	if !strings.HasPrefix(readParsed.FileSHA, "sha256:") {
		t.Fatalf("read_file response missing file_sha: %+v", readParsed)
	}
	if !strings.HasPrefix(readParsed.WorkspaceRevision, "fs:worktree:") {
		t.Fatalf("read_file response missing filesystem workspace revision: %+v", readParsed)
	}
	if readParsed.Range.StartLine != 1 || readParsed.Range.EndLine != 1 || len(readParsed.OmittedRanges) != 0 {
		t.Fatalf("unexpected read range metadata: %+v", readParsed)
	}
	if len(readParsed.Suggestions) == 0 || !strings.Contains(strings.Join(readParsed.Suggestions, " "), "file_sha") {
		t.Fatalf("read_file response missing evidence suggestion: %+v", readParsed.Suggestions)
	}

	unchangedResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"dir/a.txt"}`,
	})
	if err != nil {
		t.Fatalf("second read_file: %v", err)
	}
	var unchangedParsed struct {
		FileSHA           string   `json:"file_sha"`
		WorkspaceRevision string   `json:"workspace_revision"`
		Unchanged         bool     `json:"unchanged"`
		Suggestions       []string `json:"next_suggestions"`
	}
	if err := json.Unmarshal([]byte(unchangedResp), &unchangedParsed); err != nil {
		t.Fatalf("parse unchanged read response: %v", err)
	}
	if !unchangedParsed.Unchanged || unchangedParsed.FileSHA != readParsed.FileSHA || unchangedParsed.WorkspaceRevision == "" || len(unchangedParsed.Suggestions) == 0 {
		t.Fatalf("unexpected unchanged read metadata: %+v", unchangedParsed)
	}
}

func TestToolkit_ReadFileAllowsSessionArtifactRefs(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sessionDir := filepath.Join(t.TempDir(), "session-artifacts")
	kit.SetSessionDir(sessionDir)
	artifactPath := filepath.Join(sessionDir, "harness", "artifacts", "worker-1", "result.md")
	mustWriteFile(t, artifactPath, "artifact result\nsecond line\n")

	readResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"$SESSION_DIR/harness/artifacts/worker-1/result.md"}`,
	})
	if err != nil {
		t.Fatalf("read_file session artifact: %v", err)
	}
	var parsed struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(readResp), &parsed); err != nil {
		t.Fatalf("parse read response: %v", err)
	}
	if parsed.Path != "$SESSION_DIR/harness/artifacts/worker-1/result.md" || !strings.Contains(parsed.Content, "artifact result") {
		t.Fatalf("unexpected session artifact read: %+v", parsed)
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: fmt.Sprintf(`{"path":%q,"content":"bad"}`, artifactPath),
	}); err == nil || !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("write_file should not write session artifacts, got err=%v", err)
	}
}

func TestToolkit_WriteFileGuardsExistingFiles(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "a.txt"), "old\n")

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"a.txt","content":"new\n"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "read_file") || !strings.Contains(err.Error(), "expected_old_sha") {
		t.Fatalf("expected existing-file guard, got: %v", err)
	}
	if !strings.Contains(err.Error(), "error_kind=missing_file_baseline") || !strings.Contains(err.Error(), "safe_retry=") {
		t.Fatalf("expected structured baseline guidance, got: %v", err)
	}

	readResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"a.txt"}`,
	})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	var readParsed struct {
		FileSHA string `json:"file_sha"`
	}
	if err := json.Unmarshal([]byte(readResp), &readParsed); err != nil {
		t.Fatalf("parse read response: %v", err)
	}
	if readParsed.FileSHA == "" {
		t.Fatalf("read_file missing file_sha: %s", readResp)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"a.txt","content":"new\n","expected_old_sha":"sha256:0000"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "expected_old_sha") {
		t.Fatalf("expected expected_old_sha mismatch, got: %v", err)
	}
	if !strings.Contains(err.Error(), "error_kind=expected_old_sha_mismatch") || !strings.Contains(err.Error(), "current_file_sha=sha256:") {
		t.Fatalf("expected structured expected_old_sha guidance, got: %v", err)
	}

	writeResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"a.txt","content":"new\n","expected_old_sha":"` + readParsed.FileSHA + `"}`,
	})
	if err != nil {
		t.Fatalf("write_file with expected_old_sha: %v", err)
	}
	var writeParsed struct {
		OldFileSHA        string `json:"old_file_sha"`
		NewFileSHA        string `json:"new_file_sha"`
		WorkspaceRevision string `json:"workspace_revision"`
	}
	if err := json.Unmarshal([]byte(writeResp), &writeParsed); err != nil {
		t.Fatalf("parse write response: %v", err)
	}
	if writeParsed.OldFileSHA != readParsed.FileSHA || !strings.HasPrefix(writeParsed.NewFileSHA, "sha256:") {
		t.Fatalf("unexpected write sha metadata: %+v", writeParsed)
	}
	if !strings.HasPrefix(writeParsed.WorkspaceRevision, "fs:worktree:") {
		t.Fatalf("write_file response missing filesystem workspace revision: %+v", writeParsed)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"a.txt","content":"again\n","create_only":true}`,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected create_only to reject existing file, got: %v", err)
	}

	mustWriteFile(t, filepath.Join(root, "b.txt"), "first\n")
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"b.txt"}`,
	}); err != nil {
		t.Fatalf("read b.txt: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "b.txt"), "external\n")
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"b.txt","content":"agent\n"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "changed since last read") {
		t.Fatalf("expected stale read rejection, got: %v", err)
	}
	if !strings.Contains(err.Error(), "error_kind=stale_file_baseline") || !strings.Contains(err.Error(), "model_next_action=") {
		t.Fatalf("expected structured stale baseline guidance, got: %v", err)
	}

	largeContent := strings.Repeat("large existing file line\n", 1800)
	largePath := filepath.Join(root, "large.txt")
	mustWriteFile(t, largePath, largeContent)
	largeReadResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"large.txt","range":{"start_line":1,"end_line":1}}`,
	})
	if err != nil {
		t.Fatalf("read large.txt: %v", err)
	}
	var largeRead struct {
		FileSHA string `json:"file_sha"`
	}
	if err := json.Unmarshal([]byte(largeReadResp), &largeRead); err != nil {
		t.Fatalf("parse large read response: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"large.txt","content":"replacement\n","expected_old_sha":"` + largeRead.FileSHA + `"}`,
	})
	if err == nil ||
		!strings.Contains(err.Error(), "error_kind=broad_overwrite") ||
		!strings.Contains(err.Error(), "overwrite_policy") ||
		!strings.Contains(err.Error(), "scoped file editing tool exposed in this session") {
		t.Fatalf("expected broad overwrite rejection, got: %v", err)
	}
	if got := mustReadFile(t, largePath); got != largeContent {
		t.Fatalf("broad overwrite rejection should not mutate file: got %d bytes", len(got))
	}
	writeLargeResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"large.txt","content":"replacement\n","expected_old_sha":"` + largeRead.FileSHA + `","overwrite_policy":"explicit_user_requested"}`,
	})
	if err != nil {
		t.Fatalf("write_file explicit broad overwrite: %v", err)
	}
	var writeLarge struct {
		OldFileSHA string     `json:"old_file_sha"`
		Diff       DiffResult `json:"diff"`
	}
	if err := json.Unmarshal([]byte(writeLargeResp), &writeLarge); err != nil {
		t.Fatalf("parse broad overwrite response: %v\n%s", err, writeLargeResp)
	}
	if writeLarge.OldFileSHA != largeRead.FileSHA || mustReadFile(t, largePath) != "replacement\n" {
		t.Fatalf("explicit broad overwrite did not apply: %+v", writeLarge)
	}
	if !writeLarge.Diff.Truncated || writeLarge.Diff.OldLines <= 0 || writeLarge.Diff.NewLines != 1 || len(writeLarge.Diff.Hunks) != 0 {
		t.Fatalf("broad overwrite should return compact diff summary: %+v", writeLarge.Diff)
	}
}

func TestToolkit_WriteFileRejectsSensitivePaths(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":".env","content":"API_KEY=secret\n"}`,
	})
	if err == nil {
		t.Fatal("expected sensitive path rejection")
	}
	if !strings.Contains(err.Error(), "write_file refuses") || !strings.Contains(err.Error(), "sensitive path") {
		t.Fatalf("expected write_file sensitive path guidance, got: %v", err)
	}
	if strings.Contains(err.Error(), "API_KEY") {
		t.Fatalf("write_file sensitive path error leaked content: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".env")); !os.IsNotExist(statErr) {
		t.Fatalf("write_file should not create sensitive file, stat err=%v", statErr)
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
		FileSHA string `json:"file_sha"`
		Content string `json:"content"`
		Range   struct {
			StartLine int `json:"start_line"`
			EndLine   int `json:"end_line"`
		} `json:"range"`
		NumLines      int              `json:"num_lines"`
		StartLine     int              `json:"start_line"`
		TotalLines    int              `json:"total_lines"`
		Truncated     bool             `json:"truncated"`
		OmittedRanges []map[string]int `json:"omitted_ranges"`
		Suggestions   []string         `json:"next_suggestions"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if parsed.NumLines != 3 || parsed.StartLine != 3001 || parsed.TotalLines != 5000 || !parsed.Truncated {
		t.Fatalf("unexpected metadata: %+v", parsed)
	}
	if !strings.HasPrefix(parsed.FileSHA, "sha256:") {
		t.Fatalf("read_file response missing file_sha: %+v", parsed)
	}
	if parsed.Range.StartLine != 3001 || parsed.Range.EndLine != 3003 {
		t.Fatalf("unexpected range metadata: %+v", parsed.Range)
	}
	wantOmitted := []map[string]int{
		{"start_line": 1, "end_line": 3000},
		{"start_line": 3004, "end_line": 5000},
	}
	if !reflect.DeepEqual(parsed.OmittedRanges, wantOmitted) {
		t.Fatalf("omitted_ranges = %+v, want %+v", parsed.OmittedRanges, wantOmitted)
	}
	if len(parsed.Suggestions) == 0 || !strings.Contains(strings.Join(parsed.Suggestions, " "), "omitted range") {
		t.Fatalf("read_file response missing omitted-range suggestion: %+v", parsed.Suggestions)
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

	rangeResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"big.txt","range":{"start_line":42,"end_line":44}}`,
	})
	if err != nil {
		t.Fatalf("read_file with range: %v", err)
	}
	var rangeParsed struct {
		Content string `json:"content"`
		Range   struct {
			StartLine int `json:"start_line"`
			EndLine   int `json:"end_line"`
		} `json:"range"`
		NumLines int `json:"num_lines"`
	}
	if err := json.Unmarshal([]byte(rangeResp), &rangeParsed); err != nil {
		t.Fatalf("parse range response: %v", err)
	}
	if rangeParsed.NumLines != 3 || rangeParsed.Range.StartLine != 42 || rangeParsed.Range.EndLine != 44 {
		t.Fatalf("unexpected range response metadata: %+v", rangeParsed)
	}
	for _, want := range []string{"    42\tline-0042", "    43\tline-0043", "    44\tline-0044"} {
		if !strings.Contains(rangeParsed.Content, want) {
			t.Fatalf("range response missing %q:\n%s", want, rangeParsed.Content)
		}
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"big.txt","offset":42,"range":{"start_line":42,"end_line":44}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "either range or offset/limit") {
		t.Fatalf("expected mixed range and offset rejection, got %v", err)
	}
}

func TestToolkit_ReadFileBySymbolAndContext(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	source := strings.Join([]string{
		"package demo",
		"",
		"func helper() string {",
		`	return "helper"`,
		"}",
		"",
		"func Target() string {",
		`	value := "target"`,
		"	if value != \"\" {",
		"		return value",
		"	}",
		`	return "empty"`,
		"}",
		"",
		"func after() string {",
		`	return "after"`,
		"}",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"main.go","symbol":{"name":"Target","kind":"function"},"context_lines":0}`,
	})
	if err != nil {
		t.Fatalf("read_file by symbol: %v", err)
	}
	var parsed struct {
		Content string `json:"content"`
		Range   struct {
			StartLine int `json:"start_line"`
			EndLine   int `json:"end_line"`
		} `json:"range"`
		Symbol struct {
			Name             string `json:"name"`
			RequestedKind    string `json:"requested_kind"`
			MatchedKind      string `json:"matched_kind"`
			StartLine        int    `json:"start_line"`
			EndLine          int    `json:"end_line"`
			ContextStartLine int    `json:"context_start_line"`
			ContextEndLine   int    `json:"context_end_line"`
			ContextLines     int    `json:"context_lines"`
		} `json:"symbol"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse symbol response: %v", err)
	}
	if parsed.Symbol.Name != "Target" || parsed.Symbol.RequestedKind != "function" || parsed.Symbol.MatchedKind != "function" {
		t.Fatalf("unexpected symbol metadata: %+v", parsed.Symbol)
	}
	if parsed.Range.StartLine != 7 || parsed.Range.EndLine != 13 || parsed.Symbol.StartLine != 7 || parsed.Symbol.EndLine != 13 || parsed.Symbol.ContextLines != 0 {
		t.Fatalf("unexpected symbol range metadata: range=%+v symbol=%+v", parsed.Range, parsed.Symbol)
	}
	if !strings.Contains(parsed.Content, "Target() string") || strings.Contains(parsed.Content, "func helper") || strings.Contains(parsed.Content, "func after") {
		t.Fatalf("symbol content should be limited to target function:\n%s", parsed.Content)
	}

	rangeResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"main.go","range":{"start_line":8,"end_line":9},"context_lines":1}`,
	})
	if err != nil {
		t.Fatalf("read_file range with context: %v", err)
	}
	var rangeParsed struct {
		Content string `json:"content"`
		Range   struct {
			StartLine int `json:"start_line"`
			EndLine   int `json:"end_line"`
		} `json:"range"`
	}
	if err := json.Unmarshal([]byte(rangeResp), &rangeParsed); err != nil {
		t.Fatalf("parse context response: %v", err)
	}
	if rangeParsed.Range.StartLine != 7 || rangeParsed.Range.EndLine != 10 {
		t.Fatalf("unexpected context-expanded range: %+v", rangeParsed.Range)
	}
	for _, want := range []string{"     7\tfunc Target", "     8\t\tvalue", "     9\t\tif value", "    10\t\t\treturn value"} {
		if !strings.Contains(rangeParsed.Content, want) {
			t.Fatalf("context-expanded response missing %q:\n%s", want, rangeParsed.Content)
		}
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"main.go","symbol":{"name":"Target"},"range":{"start_line":7,"end_line":13}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "symbol instead of range") {
		t.Fatalf("expected symbol/range rejection, got %v", err)
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

func TestToolkit_ReadFileRejectsSensitivePaths(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, ".env"), "API_KEY=secret\n")

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":".env"}`,
	})
	if err == nil {
		t.Fatal("expected sensitive path rejection")
	}
	if !strings.Contains(err.Error(), "sensitive path") || !strings.Contains(err.Error(), "explicit secret handling") {
		t.Fatalf("expected sensitive path guidance, got: %v", err)
	}
}

func TestToolkit_FileToolsRejectProtectedMetadataPaths(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	gitConfigContent := "remote = origin\n"
	mustWriteFile(t, filepath.Join(root, ".git", "config"), gitConfigContent)
	mustWriteFile(t, filepath.Join(root, ".wuu", "sessions", "trace.jsonl"), "{}\n")
	gitConfigSHA := formatFileSHA(sha256Hex([]byte(gitConfigContent)))

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":".git/config"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "version-control metadata") {
		t.Fatalf("expected read_file to reject VCS metadata, got: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":".wuu/sessions/new.json","content":"{}\n"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "wuu runtime state") {
		t.Fatalf("expected write_file to reject wuu runtime state, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".wuu", "sessions", "new.json")); !os.IsNotExist(statErr) {
		t.Fatalf("write_file should not create protected runtime state file, stat err=%v", statErr)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":".git/config","old_text":"remote = origin","new_text":"remote = changed","expected_old_sha":"` + gitConfigSHA + `"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "version-control metadata") {
		t.Fatalf("expected edit_file to reject VCS metadata, got: %v", err)
	}
	if got := mustReadFile(t, filepath.Join(root, ".git", "config")); got != gitConfigContent {
		t.Fatalf("edit_file should not mutate protected VCS metadata: %q", got)
	}

	kit.SetEditToolMode(EditToolModePatch)
	patchArgs, err := json.Marshal(map[string]any{
		"patchText": "*** Begin Patch\n*** Add File: .wuu/sessions/new-from-patch.json\n+{}\n*** End Patch",
	})
	if err != nil {
		t.Fatalf("marshal patch args: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "apply_patch",
		Arguments: string(patchArgs),
	})
	if err == nil || !strings.Contains(err.Error(), "wuu runtime state") {
		t.Fatalf("expected apply_patch to reject wuu runtime state, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".wuu", "sessions", "new-from-patch.json")); !os.IsNotExist(statErr) {
		t.Fatalf("apply_patch should not create protected runtime state file, stat err=%v", statErr)
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
	if !strings.Contains(err.Error(), "error_kind=stale_file_baseline") || !strings.Contains(err.Error(), "safe_retry=") {
		t.Fatalf("expected structured stale-read guidance, got: %v", err)
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

func TestToolkit_EditFileAcceptsExpectedOldSHA(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "a.txt"), "alpha\n")

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"a.txt","old_text":"alpha","new_text":"bravo"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "expected_old_sha") {
		t.Fatalf("expected edit_file to require read or expected_old_sha, got: %v", err)
	}
	if !strings.Contains(err.Error(), "error_kind=missing_file_baseline") || !strings.Contains(err.Error(), "model_next_action=") {
		t.Fatalf("expected structured edit baseline guidance, got: %v", err)
	}

	oldSHA := formatFileSHA(sha256Hex([]byte("alpha\n")))
	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"a.txt","old_text":"alpha","new_text":"bravo","expected_old_sha":"` + oldSHA + `"}`,
	})
	if err != nil {
		t.Fatalf("edit_file with expected_old_sha: %v", err)
	}
	var parsed struct {
		OldFileSHA        string   `json:"old_file_sha"`
		NewFileSHA        string   `json:"new_file_sha"`
		WorkspaceRevision string   `json:"workspace_revision"`
		Suggestions       []string `json:"next_suggestions"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse edit response: %v", err)
	}
	if parsed.OldFileSHA != oldSHA || !strings.HasPrefix(parsed.NewFileSHA, "sha256:") {
		t.Fatalf("unexpected edit sha metadata: %+v", parsed)
	}
	if !strings.HasPrefix(parsed.WorkspaceRevision, "fs:worktree:") {
		t.Fatalf("edit_file response missing filesystem workspace revision: %+v", parsed)
	}
	if len(parsed.Suggestions) == 0 || !strings.Contains(strings.Join(parsed.Suggestions, " "), "terminal command tool") {
		t.Fatalf("edit_file response missing validation suggestion: %+v", parsed.Suggestions)
	}
	if got := mustReadFile(t, filepath.Join(root, "a.txt")); got != "bravo\n" {
		t.Fatalf("unexpected edited content: %q", got)
	}

	mustWriteFile(t, filepath.Join(root, "b.txt"), "one\n")
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"b.txt","old_text":"one","new_text":"two","expected_old_sha":"sha256:0000"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "expected_old_sha") {
		t.Fatalf("expected expected_old_sha mismatch, got: %v", err)
	}
	if !strings.Contains(err.Error(), "error_kind=expected_old_sha_mismatch") || !strings.Contains(err.Error(), "current_file_sha=sha256:") {
		t.Fatalf("expected structured expected_old_sha guidance, got: %v", err)
	}
}

func TestToolkit_EditFileReportsRecoverableTextMatchErrors(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	content := "alpha\nbravo\ncharlie\nbravo\n"
	mustWriteFile(t, filepath.Join(root, "a.txt"), content)
	oldSHA := formatFileSHA(sha256Hex([]byte(content)))

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"a.txt","old_text":"bravx","new_text":"BRAVX","expected_old_sha":"` + oldSHA + `"}`,
	})
	if err == nil {
		t.Fatal("expected old_text not found error")
	}
	if !strings.Contains(err.Error(), "old_text_not_found") ||
		!strings.Contains(err.Error(), "candidates") ||
		!strings.Contains(err.Error(), "2| bravo") ||
		!strings.Contains(err.Error(), "safe_retry") {
		t.Fatalf("expected recoverable old_text guidance, got: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"a.txt","old_text":"bravo","new_text":"BRAVO","expected_old_sha":"` + oldSHA + `"}`,
	})
	if err == nil {
		t.Fatal("expected ambiguous old_text error")
	}
	if !strings.Contains(err.Error(), "ambiguous_old_text") ||
		!strings.Contains(err.Error(), "matched 2 locations") ||
		!strings.Contains(err.Error(), "lines 2-2") ||
		!strings.Contains(err.Error(), "lines 4-4") ||
		!strings.Contains(err.Error(), "replace_all=true") {
		t.Fatalf("expected ambiguous old_text guidance, got: %v", err)
	}
	if got := mustReadFile(t, filepath.Join(root, "a.txt")); got != content {
		t.Fatalf("failed edit should not mutate file: %q", got)
	}
}

func TestToolkit_EditFileRejectsSensitivePaths(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, ".env"), "API_KEY=secret\n")
	oldSHA := formatFileSHA(sha256Hex([]byte("API_KEY=secret\n")))

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":".env","old_text":"API_KEY=secret","new_text":"API_KEY=changed","expected_old_sha":"` + oldSHA + `"}`,
	})
	if err == nil {
		t.Fatal("expected sensitive path rejection")
	}
	if !strings.Contains(err.Error(), "edit_file refuses") || !strings.Contains(err.Error(), "sensitive path") {
		t.Fatalf("expected edit_file sensitive path guidance, got: %v", err)
	}
	if strings.Contains(err.Error(), "API_KEY") {
		t.Fatalf("edit_file sensitive path error leaked content: %v", err)
	}
	if got := mustReadFile(t, filepath.Join(root, ".env")); got != "API_KEY=secret\n" {
		t.Fatalf("edit_file should not mutate sensitive file: %q", got)
	}
}

func TestToolkit_ApplyPatchEditsAddsDeletesAndMoves(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetEditToolMode(EditToolModePatch)
	sessionDir := filepath.Join(t.TempDir(), "session")
	kit.SetSessionID("session-apply-patch")
	kit.SetSessionDir(sessionDir)

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
	expectedOldSHAs := map[string]string{
		"a.txt":       formatFileSHA(sha256Hex([]byte("line one\nline two\nline three\n"))),
		"remove.txt":  formatFileSHA(sha256Hex([]byte("remove me\n"))),
		"oldname.txt": formatFileSHA(sha256Hex([]byte("old name\n"))),
	}
	args, err := json.Marshal(map[string]any{
		"patchText":         patchText,
		"expected_old_shas": expectedOldSHAs,
	})
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
	var parsed struct {
		DryRun            bool                  `json:"dry_run"`
		HunkCount         int                   `json:"hunk_count"`
		ChangedFiles      []string              `json:"changed_files"`
		RiskSummary       applyPatchRiskSummary `json:"risk_summary"`
		WorkspaceRevision string                `json:"workspace_revision"`
		Suggestions       []string              `json:"next_suggestions"`
		Provenance        struct {
			Tool   string `json:"tool"`
			Source string `json:"source"`
		} `json:"provenance"`
		PatchJournalPath string                    `json:"patch_journal_path"`
		ManifestPath     string                    `json:"manifest_path"`
		PatchPath        string                    `json:"patch_path"`
		PatchJournal     applyPatchJournalManifest `json:"patch_journal"`
		Files            []struct {
			Path       string `json:"path"`
			Action     string `json:"action"`
			OldFileSHA string `json:"old_file_sha"`
			NewFileSHA string `json:"new_file_sha"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse apply_patch response: %v", err)
	}
	if parsed.DryRun || parsed.HunkCount != 4 {
		t.Fatalf("unexpected patch summary: %+v", parsed)
	}
	if parsed.RiskSummary.FileCount != 4 ||
		parsed.RiskSummary.HunkCount != 4 ||
		parsed.RiskSummary.AddedLines != 3 ||
		parsed.RiskSummary.DeletedLines != 3 ||
		parsed.RiskSummary.Actions["update"] != 1 ||
		parsed.RiskSummary.Actions["add"] != 1 ||
		parsed.RiskSummary.Actions["delete"] != 1 ||
		parsed.RiskSummary.Actions["move"] != 1 ||
		!parsed.RiskSummary.MultiFile ||
		!parsed.RiskSummary.ContainsDelete ||
		!parsed.RiskSummary.ContainsMove ||
		parsed.RiskSummary.RiskLevel != "high" {
		t.Fatalf("unexpected patch risk summary: %+v", parsed.RiskSummary)
	}
	if !strings.HasPrefix(parsed.WorkspaceRevision, "fs:worktree:") {
		t.Fatalf("apply_patch response missing filesystem workspace revision: %+v", parsed)
	}
	wantChanged := []string{"a.txt", "dir/new.txt", "remove.txt", "renamed.txt"}
	if !reflect.DeepEqual(parsed.ChangedFiles, wantChanged) {
		t.Fatalf("changed_files = %+v, want %+v", parsed.ChangedFiles, wantChanged)
	}
	if parsed.Provenance.Tool != "apply_patch" || parsed.Provenance.Source != "model_tool_call" {
		t.Fatalf("unexpected provenance: %+v", parsed.Provenance)
	}
	if len(parsed.Suggestions) == 0 || !strings.Contains(strings.Join(parsed.Suggestions, " "), "terminal command tool") {
		t.Fatalf("apply_patch response missing validation suggestion: %+v", parsed.Suggestions)
	}
	if parsed.PatchJournalPath == "" || parsed.ManifestPath != parsed.PatchJournalPath || parsed.PatchPath == "" {
		t.Fatalf("apply_patch response missing journal artifact paths: %+v", parsed)
	}
	if !strings.HasPrefix(parsed.PatchJournalPath, filepath.Join(sessionDir, "patch-journal")) ||
		!strings.HasPrefix(parsed.PatchPath, filepath.Join(sessionDir, "patch-journal")) {
		t.Fatalf("journal paths should be session-scoped: manifest=%q patch=%q", parsed.PatchJournalPath, parsed.PatchPath)
	}
	if parsed.PatchJournal.ID == "" || parsed.PatchJournal.SessionID != "session-apply-patch" ||
		parsed.PatchJournal.HunkCount != 4 || parsed.PatchJournal.PatchPath != parsed.PatchPath {
		t.Fatalf("unexpected inline patch journal: %+v", parsed.PatchJournal)
	}
	if parsed.PatchJournal.RiskSummary.RiskLevel != "high" || parsed.PatchJournal.RiskSummary.AddedLines != 3 {
		t.Fatalf("patch journal missing risk summary: %+v", parsed.PatchJournal.RiskSummary)
	}
	if got := mustReadFile(t, parsed.PatchPath); got != patchText {
		t.Fatalf("patch artifact mismatch:\n%s", got)
	}
	var manifest applyPatchJournalManifest
	data, err := os.ReadFile(parsed.PatchJournalPath)
	if err != nil {
		t.Fatalf("read patch journal manifest: %v", err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse patch journal manifest: %v\n%s", err, data)
	}
	if manifest.ID != parsed.PatchJournal.ID || manifest.Tool != "apply_patch" ||
		manifest.WorkspaceRevisionBefore == "" || manifest.WorkspaceRevisionAfter != parsed.WorkspaceRevision ||
		!reflect.DeepEqual(manifest.ChangedFiles, wantChanged) || len(manifest.Snapshots) != 5 {
		t.Fatalf("unexpected patch journal manifest: %+v", manifest)
	}
	for _, snapshot := range manifest.Snapshots {
		if snapshot.Path == "a.txt" && (!snapshot.Existed || snapshot.FileSHA != expectedOldSHAs["a.txt"] || snapshot.SnapshotPath == "") {
			t.Fatalf("unexpected a.txt snapshot: %+v", snapshot)
		}
		if snapshot.Path == "dir/new.txt" && snapshot.Existed {
			t.Fatalf("added file should be recorded as absent before patch: %+v", snapshot)
		}
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || !containsString(records[0].ArtifactRefs, parsed.PatchJournalPath) || !containsString(records[0].ArtifactRefs, parsed.PatchPath) {
		t.Fatalf("patch telemetry missing journal artifacts: %+v", records)
	}
	if records[0].PatchRiskSummary == nil ||
		records[0].PatchRiskSummary.RiskLevel != "high" ||
		records[0].PatchRiskSummary.FileCount != 4 ||
		records[0].PatchRiskSummary.Actions["move"] != 1 {
		t.Fatalf("patch telemetry missing risk summary: %+v", records[0].PatchRiskSummary)
	}
	seenSHA := map[string]struct {
		old string
		new string
	}{}
	for _, file := range parsed.Files {
		seenSHA[file.Path] = struct {
			old string
			new string
		}{old: file.OldFileSHA, new: file.NewFileSHA}
	}
	if got := seenSHA["a.txt"]; got.old != expectedOldSHAs["a.txt"] || !strings.HasPrefix(got.new, "sha256:") {
		t.Fatalf("update sha metadata = %+v", got)
	}
	if got := seenSHA["dir/new.txt"]; got.old != "" || !strings.HasPrefix(got.new, "sha256:") {
		t.Fatalf("add sha metadata = %+v", got)
	}
	if got := seenSHA["remove.txt"]; got.old != expectedOldSHAs["remove.txt"] || got.new != "" {
		t.Fatalf("delete sha metadata = %+v", got)
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

func TestToolkit_ApplyPatchDryRunDoesNotMutate(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetEditToolMode(EditToolModePatch)
	changedHookCalls := 0
	kit.SetOnFileChanged(func(string) {
		changedHookCalls++
	})

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
	args, err := json.Marshal(map[string]any{"patchText": patchText, "dry_run": true})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "apply_patch",
		Arguments: string(args),
	})
	if err != nil {
		t.Fatalf("apply_patch dry-run: %v", err)
	}
	var parsed struct {
		DryRun            bool     `json:"dry_run"`
		HunkCount         int      `json:"hunk_count"`
		ChangedFiles      []string `json:"changed_files"`
		WorkspaceRevision string   `json:"workspace_revision"`
		Suggestions       []string `json:"next_suggestions"`
		Files             []struct {
			Action string `json:"action"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse apply_patch response: %v", err)
	}
	if !parsed.DryRun || parsed.HunkCount != 4 || len(parsed.Files) != 4 {
		t.Fatalf("unexpected dry-run summary: %+v", parsed)
	}
	if !strings.HasPrefix(parsed.WorkspaceRevision, "fs:worktree:") {
		t.Fatalf("apply_patch dry-run response missing filesystem workspace revision: %+v", parsed)
	}
	wantChanged := []string{"a.txt", "dir/new.txt", "remove.txt", "renamed.txt"}
	if !reflect.DeepEqual(parsed.ChangedFiles, wantChanged) {
		t.Fatalf("changed_files = %+v, want %+v", parsed.ChangedFiles, wantChanged)
	}
	if len(parsed.Suggestions) == 0 || !strings.Contains(strings.Join(parsed.Suggestions, " "), "without dry_run") {
		t.Fatalf("apply_patch dry-run response missing apply suggestion: %+v", parsed.Suggestions)
	}
	if changedHookCalls != 0 {
		t.Fatalf("dry-run should not fire file-change hooks, got %d", changedHookCalls)
	}
	if got := mustReadFile(t, filepath.Join(root, "a.txt")); got != "line one\nline two\nline three\n" {
		t.Fatalf("dry-run mutated update file: %q", got)
	}
	if got := mustReadFile(t, filepath.Join(root, "remove.txt")); got != "remove me\n" {
		t.Fatalf("dry-run deleted file content: %q", got)
	}
	if got := mustReadFile(t, filepath.Join(root, "oldname.txt")); got != "old name\n" {
		t.Fatalf("dry-run moved source content: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "dir/new.txt")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create new file, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create move target, stat err=%v", err)
	}
}

func TestToolkit_ApplyPatchRejectsInvalidPatchAtomically(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetEditToolMode(EditToolModePatch)
	changedHookCalls := 0
	kit.SetOnFileChanged(func(string) {
		changedHookCalls++
	})

	mustWriteFile(t, filepath.Join(root, "a.txt"), "alpha\n")
	mustWriteFile(t, filepath.Join(root, "b.txt"), "beta\n")

	patchText := `*** Begin Patch
*** Update File: a.txt
@@
-alpha
+ALPHA
*** Update File: b.txt
@@
-missing
+BETA
*** End Patch`
	args, err := json.Marshal(map[string]any{
		"patchText": patchText,
		"expected_old_shas": map[string]string{
			"a.txt": formatFileSHA(sha256Hex([]byte("alpha\n"))),
			"b.txt": formatFileSHA(sha256Hex([]byte("beta\n"))),
		},
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "apply_patch",
		Arguments: string(args),
	})
	if err == nil || !strings.Contains(err.Error(), "apply_patch verification failed") ||
		!strings.Contains(err.Error(), "b.txt") {
		t.Fatalf("expected failed verification for b.txt, got: %v", err)
	}
	if !strings.Contains(err.Error(), "anchor_not_found") ||
		!strings.Contains(err.Error(), "candidates") ||
		!strings.Contains(err.Error(), "1| beta") ||
		!strings.Contains(err.Error(), "safe_retry") {
		t.Fatalf("expected recoverable patch guidance, got: %v", err)
	}
	if changedHookCalls != 0 {
		t.Fatalf("failed patch should not fire file-change hooks, got %d", changedHookCalls)
	}
	if got := mustReadFile(t, filepath.Join(root, "a.txt")); got != "alpha\n" {
		t.Fatalf("failed patch mutated first file: %q", got)
	}
	if got := mustReadFile(t, filepath.Join(root, "b.txt")); got != "beta\n" {
		t.Fatalf("failed patch mutated second file: %q", got)
	}
}

func TestToolkit_ApplyPatchGuardsExistingFiles(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetEditToolMode(EditToolModePatch)
	mustWriteFile(t, filepath.Join(root, "a.txt"), "alpha\n")

	patchText := `*** Begin Patch
*** Update File: a.txt
@@
-alpha
+bravo
*** End Patch`
	args, err := json.Marshal(map[string]string{"patchText": patchText})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "apply_patch",
		Arguments: string(args),
	})
	if err == nil || !strings.Contains(err.Error(), "read_file") || !strings.Contains(err.Error(), "expected_old_shas") {
		t.Fatalf("expected apply_patch baseline guard, got: %v", err)
	}

	readResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"a.txt"}`,
	})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	var readParsed struct {
		FileSHA string `json:"file_sha"`
	}
	if err := json.Unmarshal([]byte(readResp), &readParsed); err != nil {
		t.Fatalf("parse read response: %v", err)
	}
	if readParsed.FileSHA == "" {
		t.Fatalf("read_file missing file_sha: %s", readResp)
	}
	mustWriteFile(t, filepath.Join(root, "a.txt"), "changed\n")
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "apply_patch",
		Arguments: string(args),
	})
	if err == nil || !strings.Contains(err.Error(), "changed since last read") {
		t.Fatalf("expected stale read rejection, got: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name: "apply_patch",
		Arguments: `{"patchText":` + strconv.Quote(`*** Begin Patch
*** Update File: a.txt
@@
-changed
+bravo
*** End Patch`) + `,"expected_old_sha":"sha256:0000"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "expected_old_sha") {
		t.Fatalf("expected expected_old_sha mismatch, got: %v", err)
	}

	currentSHA := formatFileSHA(sha256Hex([]byte("changed\n")))
	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "apply_patch",
		Arguments: `{"patchText":` + strconv.Quote(`*** Begin Patch
*** Update File: a.txt
@@
-changed
+bravo
*** End Patch`) + `,"expected_old_sha":"` + currentSHA + `"}`,
	})
	if err != nil {
		t.Fatalf("apply_patch with expected_old_sha: %v", err)
	}
	var parsed struct {
		Files []struct {
			OldFileSHA string `json:"old_file_sha"`
			NewFileSHA string `json:"new_file_sha"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse apply_patch response: %v", err)
	}
	if len(parsed.Files) != 1 || parsed.Files[0].OldFileSHA != currentSHA || !strings.HasPrefix(parsed.Files[0].NewFileSHA, "sha256:") {
		t.Fatalf("unexpected patch sha metadata: %+v", parsed.Files)
	}
	if got := mustReadFile(t, filepath.Join(root, "a.txt")); got != "bravo\n" {
		t.Fatalf("unexpected patched content: %q", got)
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
*** End Patch`, "expected_old_sha": formatFileSHA(sha256Hex([]byte("same\nsame\n")))})
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
	if !strings.Contains(err.Error(), "ambiguous_anchor") ||
		!strings.Contains(err.Error(), "lines 1-1") ||
		!strings.Contains(err.Error(), "lines 2-2") ||
		!strings.Contains(err.Error(), "safe_retry") {
		t.Fatalf("expected ambiguous anchor recovery guidance, got %v", err)
	}
}

func TestToolkit_ApplyPatchRejectsSensitivePaths(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetEditToolMode(EditToolModePatch)
	mustWriteFile(t, filepath.Join(root, ".env"), "API_KEY=secret\n")

	cases := []struct {
		name    string
		patch   string
		dryRun  bool
		wantOp  string
		leakKey string
	}{
		{
			name: "dry-run update",
			patch: `*** Begin Patch
*** Update File: .env
@@
-API_KEY=secret
+API_KEY=changed
*** End Patch`,
			dryRun:  true,
			wantOp:  "update",
			leakKey: "API_KEY=secret",
		},
		{
			name: "add",
			patch: `*** Begin Patch
*** Add File: secrets/config.txt
+token=secret
*** End Patch`,
			wantOp:  "add",
			leakKey: "token=secret",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]any{"patchText": tc.patch, "dry_run": tc.dryRun})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}
			_, err = kit.Execute(context.Background(), providers.ToolCall{
				Name:      "apply_patch",
				Arguments: string(args),
			})
			if err == nil {
				t.Fatalf("expected sensitive path rejection for %s", tc.name)
			}
			if !strings.Contains(err.Error(), "sensitive path") || !strings.Contains(err.Error(), tc.wantOp) {
				t.Fatalf("expected sensitive path %s guidance, got: %v", tc.wantOp, err)
			}
			if strings.Contains(err.Error(), tc.leakKey) {
				t.Fatalf("apply_patch sensitive path error leaked content for %s: %v", tc.name, err)
			}
		})
	}
}

func TestToolkit_ToolMetadata_ClassifiesApplyPatchDryRun(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetEditToolMode(EditToolModePatch)

	meta, ok := kit.ToolMetadata(providers.ToolCall{
		Name:      "apply_patch",
		Arguments: `{"patchText":"*** Begin Patch\n*** End Patch","dry_run":true}`,
	})
	if !ok {
		t.Fatal("apply_patch metadata not found")
	}
	if !meta.ReadOnly || !meta.ConcurrencySafe || meta.Risk != string(ToolRiskLow) || meta.Reason != "patch dry-run preview" {
		t.Fatalf("dry-run apply_patch metadata = %+v, want low-risk read-only preview", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "apply_patch",
		Arguments: `{"patchText":"*** Begin Patch\n*** End Patch"}`,
	})
	if !ok {
		t.Fatal("apply_patch metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Risk != string(ToolRiskHigh) || meta.Reason != "patch applies workspace changes" {
		t.Fatalf("mutating apply_patch metadata = %+v, want high-risk write", meta)
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
*** End Patch`, "expected_old_sha": formatFileSHA(sha256Hex([]byte("first\n")))})
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

func TestToolkit_CheckpointCreateListRestore(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetStateDir(filepath.Join(t.TempDir(), "state"))
	changed := map[string]int{}
	kit.SetOnFileChanged(func(absPath string) {
		changed[kit.env.NormalizeDisplayPath(absPath)]++
	})

	mustWriteFile(t, filepath.Join(root, "a.txt"), "before\n")
	createArgs, err := json.Marshal(map[string]any{
		"action":        "create",
		"checkpoint_id": "before-edit",
		"paths":         []string{"a.txt", "created_later.txt"},
		"reason":        "before risky edit",
	})
	if err != nil {
		t.Fatalf("marshal create args: %v", err)
	}
	createResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "checkpoint",
		Arguments: string(createArgs),
	})
	if err != nil {
		t.Fatalf("checkpoint create: %v", err)
	}
	var created struct {
		Action            string                  `json:"action"`
		ManifestPath      string                  `json:"manifest_path"`
		WorkspaceRevision string                  `json:"workspace_revision"`
		Checkpoint        workspaceFileCheckpoint `json:"checkpoint"`
	}
	if err := json.Unmarshal([]byte(createResp), &created); err != nil {
		t.Fatalf("parse create response: %v\n%s", err, createResp)
	}
	if created.Action != "create" || created.Checkpoint.ID != "before-edit" || created.ManifestPath == "" {
		t.Fatalf("unexpected create response: %+v", created)
	}
	if !strings.HasPrefix(created.WorkspaceRevision, "fs:worktree:") || created.Checkpoint.WorkspaceRevision != created.WorkspaceRevision {
		t.Fatalf("checkpoint response missing workspace revision: %+v", created)
	}
	if _, err := os.Stat(created.ManifestPath); err != nil {
		t.Fatalf("checkpoint manifest missing: %v", err)
	}

	mustWriteFile(t, filepath.Join(root, "a.txt"), "after\n")
	mustWriteFile(t, filepath.Join(root, "created_later.txt"), "new\n")
	restoreResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "checkpoint",
		Arguments: `{"action":"restore","checkpoint_id":"before-edit","reason":"rollback"}`,
	})
	if err != nil {
		t.Fatalf("checkpoint restore: %v", err)
	}
	var restored struct {
		Action            string                    `json:"action"`
		WorkspaceRevision string                    `json:"workspace_revision"`
		RestoredFiles     []checkpointRestoreResult `json:"restored_files"`
	}
	if err := json.Unmarshal([]byte(restoreResp), &restored); err != nil {
		t.Fatalf("parse restore response: %v\n%s", err, restoreResp)
	}
	if restored.Action != "restore" || len(restored.RestoredFiles) != 2 || !strings.HasPrefix(restored.WorkspaceRevision, "fs:worktree:") {
		t.Fatalf("unexpected restore response: %+v", restored)
	}
	if got := mustReadFile(t, filepath.Join(root, "a.txt")); got != "before\n" {
		t.Fatalf("checkpoint restore did not restore a.txt: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "created_later.txt")); !os.IsNotExist(err) {
		t.Fatalf("checkpoint restore should remove file absent at checkpoint, stat err=%v", err)
	}
	if changed["a.txt"] == 0 || changed["created_later.txt"] == 0 {
		t.Fatalf("restore should notify changed paths, got %+v", changed)
	}

	listResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "checkpoint",
		Arguments: `{"action":"list"}`,
	})
	if err != nil {
		t.Fatalf("checkpoint list: %v", err)
	}
	var listed struct {
		Count       int                       `json:"count"`
		Checkpoints []workspaceFileCheckpoint `json:"checkpoints"`
	}
	if err := json.Unmarshal([]byte(listResp), &listed); err != nil {
		t.Fatalf("parse list response: %v\n%s", err, listResp)
	}
	if listed.Count != 1 || listed.Checkpoints[0].ID != "before-edit" {
		t.Fatalf("unexpected checkpoint list: %+v", listed)
	}
	if listed.Checkpoints[0].RestoredAt.IsZero() {
		t.Fatalf("checkpoint restore should persist restored_at: %+v", listed.Checkpoints[0])
	}
	records := kit.ToolTelemetry()
	if len(records) != 3 ||
		records[0].ResultAction != "create" ||
		records[1].ResultAction != "restore" ||
		records[2].ResultAction != "list" {
		t.Fatalf("checkpoint telemetry missing result actions: %+v", records)
	}
}

func TestToolkit_CheckpointRestorePatchJournal(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetEditToolMode(EditToolModePatch)
	sessionDir := filepath.Join(t.TempDir(), "session")
	kit.SetSessionDir(sessionDir)
	changed := map[string]int{}
	kit.SetOnFileChanged(func(absPath string) {
		changed[kit.env.NormalizeDisplayPath(absPath)]++
	})

	mustWriteFile(t, filepath.Join(root, "a.txt"), "before\n")
	patchText := `*** Begin Patch
*** Update File: a.txt
@@
-before
+after
*** Add File: scratch.txt
+temporary
*** End Patch`
	args, err := json.Marshal(map[string]any{
		"patchText": patchText,
		"expected_old_shas": map[string]string{
			"a.txt": formatFileSHA(sha256Hex([]byte("before\n"))),
		},
	})
	if err != nil {
		t.Fatalf("marshal patch args: %v", err)
	}
	patchResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "apply_patch",
		Arguments: string(args),
	})
	if err != nil {
		t.Fatalf("apply_patch: %v", err)
	}
	var patched struct {
		PatchJournalPath string `json:"patch_journal_path"`
	}
	if err := json.Unmarshal([]byte(patchResp), &patched); err != nil {
		t.Fatalf("parse apply_patch response: %v\n%s", err, patchResp)
	}
	if patched.PatchJournalPath == "" {
		t.Fatalf("apply_patch response missing patch journal path: %s", patchResp)
	}
	if got := mustReadFile(t, filepath.Join(root, "a.txt")); got != "after\n" {
		t.Fatalf("fixture patch did not update a.txt: %q", got)
	}
	if got := mustReadFile(t, filepath.Join(root, "scratch.txt")); got != "temporary\n" {
		t.Fatalf("fixture patch did not create scratch.txt: %q", got)
	}

	restoreArgs, err := json.Marshal(map[string]any{
		"action":             "restore_patch_journal",
		"patch_journal_path": patched.PatchJournalPath,
		"reason":             "rollback applied patch",
	})
	if err != nil {
		t.Fatalf("marshal restore args: %v", err)
	}
	restoreResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "checkpoint",
		Arguments: string(restoreArgs),
	})
	if err != nil {
		t.Fatalf("restore patch journal: %v", err)
	}
	var restored struct {
		Action            string                    `json:"action"`
		PatchJournalPath  string                    `json:"patch_journal_path"`
		ManifestPath      string                    `json:"manifest_path"`
		WorkspaceRevision string                    `json:"workspace_revision"`
		RestoredFiles     []checkpointRestoreResult `json:"restored_files"`
		PatchJournal      applyPatchJournalManifest `json:"patch_journal"`
	}
	if err := json.Unmarshal([]byte(restoreResp), &restored); err != nil {
		t.Fatalf("parse restore response: %v\n%s", err, restoreResp)
	}
	if restored.Action != "restore_patch_journal" || restored.PatchJournalPath != patched.PatchJournalPath ||
		restored.ManifestPath != patched.PatchJournalPath || len(restored.RestoredFiles) != 2 ||
		!strings.HasPrefix(restored.WorkspaceRevision, "fs:worktree:") || restored.PatchJournal.RestoredAt.IsZero() {
		t.Fatalf("unexpected restore response: %+v", restored)
	}
	if got := mustReadFile(t, filepath.Join(root, "a.txt")); got != "before\n" {
		t.Fatalf("patch journal restore did not restore a.txt: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "scratch.txt")); !os.IsNotExist(err) {
		t.Fatalf("patch journal restore should remove scratch.txt, stat err=%v", err)
	}
	if changed["a.txt"] == 0 || changed["scratch.txt"] == 0 {
		t.Fatalf("restore should notify changed paths, got %+v", changed)
	}

	var manifest applyPatchJournalManifest
	data, err := os.ReadFile(patched.PatchJournalPath)
	if err != nil {
		t.Fatalf("read patch journal manifest: %v", err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse restored journal manifest: %v\n%s", err, data)
	}
	if manifest.RestoredAt.IsZero() {
		t.Fatalf("patch journal manifest should be marked restored: %+v", manifest)
	}
	records := kit.ToolTelemetry()
	if len(records) != 2 || records[1].ResultAction != "restore_patch_journal" || !containsString(records[1].ArtifactRefs, patched.PatchJournalPath) {
		t.Fatalf("restore telemetry missing patch journal artifact: %+v", records)
	}
}

func TestToolkit_CheckpointRestorePatchJournalRejectsExternalSnapshot(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetEditToolMode(EditToolModePatch)
	kit.SetSessionDir(filepath.Join(t.TempDir(), "session"))

	mustWriteFile(t, filepath.Join(root, "a.txt"), "before\n")
	patchText := `*** Begin Patch
*** Update File: a.txt
@@
-before
+after
*** End Patch`
	args, err := json.Marshal(map[string]any{
		"patchText": patchText,
		"expected_old_shas": map[string]string{
			"a.txt": formatFileSHA(sha256Hex([]byte("before\n"))),
		},
	})
	if err != nil {
		t.Fatalf("marshal patch args: %v", err)
	}
	patchResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "apply_patch",
		Arguments: string(args),
	})
	if err != nil {
		t.Fatalf("apply_patch: %v", err)
	}
	var patched struct {
		PatchJournalPath string `json:"patch_journal_path"`
	}
	if err := json.Unmarshal([]byte(patchResp), &patched); err != nil {
		t.Fatalf("parse apply_patch response: %v\n%s", err, patchResp)
	}
	manifest, err := loadApplyPatchJournalManifest(patched.PatchJournalPath)
	if err != nil {
		t.Fatalf("load patch journal: %v", err)
	}
	if len(manifest.Snapshots) == 0 {
		t.Fatalf("patch journal missing snapshots: %+v", manifest)
	}
	externalSnapshot := filepath.Join(t.TempDir(), "outside.snapshot")
	mustWriteFile(t, externalSnapshot, "leaked\n")
	manifest.Snapshots[0].SnapshotPath = externalSnapshot
	if err := writeApplyPatchJournalManifest(patched.PatchJournalPath, manifest); err != nil {
		t.Fatalf("rewrite patch journal: %v", err)
	}

	restoreArgs, err := json.Marshal(map[string]any{
		"action":             "restore_patch_journal",
		"patch_journal_path": patched.PatchJournalPath,
	})
	if err != nil {
		t.Fatalf("marshal restore args: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "checkpoint",
		Arguments: string(restoreArgs),
	})
	if err == nil || !strings.Contains(err.Error(), "outside the journal files directory") {
		t.Fatalf("expected external snapshot rejection, got: %v", err)
	}
	if got := mustReadFile(t, filepath.Join(root, "a.txt")); got != "after\n" {
		t.Fatalf("failed restore should not mutate workspace: %q", got)
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

func TestToolkit_ListFilesRejectsAndFiltersProtectedPaths(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "visible.txt"), "hello\n")
	mustWriteFile(t, filepath.Join(root, ".env"), "API_KEY=secret\n")
	mustWriteFile(t, filepath.Join(root, ".git", "config"), "remote = origin\n")
	mustWriteFile(t, filepath.Join(root, ".wuu", "sessions", "trace.jsonl"), "{}\n")

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "list_files",
		Arguments: `{"path":".wuu"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "wuu runtime state") {
		t.Fatalf("expected direct .wuu list rejection, got: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "list_files",
		Arguments: `{}`,
	})
	if err != nil {
		t.Fatalf("list_files root: %v", err)
	}
	var parsed struct {
		OmittedProtected int `json:"omitted_protected"`
		Entries          []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse list_files response: %v\n%s", err, resp)
	}
	if parsed.OmittedProtected != 3 {
		t.Fatalf("expected three protected entries omitted, got %+v from %s", parsed, resp)
	}
	if len(parsed.Entries) != 1 || parsed.Entries[0].Name != "visible.txt" || parsed.Entries[0].Path != "visible.txt" {
		t.Fatalf("list_files should only return visible entries: %+v", parsed.Entries)
	}
	if strings.Contains(resp, ".wuu") || strings.Contains(resp, ".git") || strings.Contains(resp, ".env") || strings.Contains(resp, "secret") {
		t.Fatalf("list_files leaked protected entry names or content: %s", resp)
	}
}

func TestToolkit_ListFilesReturnsEntryPathsAndSuggestions(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "dir", "a.txt"), "hello\n")
	if err := os.MkdirAll(filepath.Join(root, "dir", "sub"), 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "list_files",
		Arguments: `{"path":"dir"}`,
	})
	if err != nil {
		t.Fatalf("list_files: %v", err)
	}
	var parsed struct {
		Path              string `json:"path"`
		WorkspaceRevision string `json:"workspace_revision"`
		Total             int    `json:"total"`
		OmittedEntryCount int    `json:"omitted_entry_count"`
		Entries           []struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			IsDir bool   `json:"is_dir"`
			Size  int64  `json:"size,omitempty"`
		} `json:"entries"`
		Suggestions []string `json:"next_suggestions"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse list_files response: %v", err)
	}
	if parsed.Path != "dir" || parsed.Total != 2 || parsed.OmittedEntryCount != 0 {
		t.Fatalf("unexpected list_files metadata: %+v", parsed)
	}
	if !strings.HasPrefix(parsed.WorkspaceRevision, "fs:worktree:") {
		t.Fatalf("list_files response missing filesystem workspace revision: %+v", parsed)
	}
	wantPaths := []string{"dir/a.txt", "dir/sub"}
	gotPaths := make([]string, 0, len(parsed.Entries))
	for _, entry := range parsed.Entries {
		gotPaths = append(gotPaths, entry.Path)
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("entry paths = %+v, want %+v", gotPaths, wantPaths)
	}
	if len(parsed.Suggestions) == 0 || !strings.Contains(strings.Join(parsed.Suggestions, " "), "read_file") {
		t.Fatalf("list_files response missing next suggestion: %+v", parsed.Suggestions)
	}
}

func TestToolkit_FileToolTelemetryRecordsResultActions(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "a.txt"), "one\n")

	if _, err := kit.Execute(context.Background(), providers.ToolCall{Name: "read_file", Arguments: `{"path":"a.txt"}`}); err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{Name: "list_files", Arguments: `{}`}); err != nil {
		t.Fatalf("list_files: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{Name: "write_file", Arguments: `{"path":"new.txt","content":"new\n"}`}); err != nil {
		t.Fatalf("write_file create: %v", err)
	}
	oneSHA := formatFileSHA(sha256Hex([]byte("one\n")))
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"a.txt","old_text":"one","new_text":"two","expected_old_sha":"` + oneSHA + `"}`,
	}); err != nil {
		t.Fatalf("edit_file: %v", err)
	}
	twoSHA := formatFileSHA(sha256Hex([]byte("two\n")))
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"a.txt","content":"three\n","expected_old_sha":"` + twoSHA + `"}`,
	}); err != nil {
		t.Fatalf("write_file overwrite: %v", err)
	}

	records := kit.ToolTelemetry()
	if len(records) != 5 {
		t.Fatalf("expected five telemetry records, got %+v", records)
	}
	got := make([]string, 0, len(records))
	for _, record := range records {
		got = append(got, record.Name+":"+record.ResultAction)
	}
	want := []string{"read_file:read", "list_files:list", "write_file:create", "edit_file:edit", "write_file:overwrite"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("file tool result actions = %+v, want %+v", got, want)
	}
}

func TestToolkit_AgentTeamTelemetryRecordsResultActions(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	control, err := agentcontrol.New(agentcontrol.Config{
		Client:       &workflowFakeClient{content: "agent done"},
		DefaultModel: "fake-model",
		ParentRepo:   root,
		WorktreeRoot: filepath.Join(root, ".wuu", "worktrees"),
		SessionID:    "agent-telemetry-session",
		HistoryDir:   filepath.Join(root, ".wuu", "sessions", "agent-telemetry-session", "workers"),
		ThreadDir:    filepath.Join(root, ".wuu", "sessions", "agent-telemetry-session", "threads"),
		HarnessDir:   filepath.Join(root, ".wuu", "sessions", "agent-telemetry-session", "harness"),
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return workflowNoopExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("AgentControl New: %v", err)
	}
	defer stopWorkflowAgentControl(control)
	kit.SetAgentControl(control)
	kit.SetAgentIdentity("root", agentthread.RootPath)

	spawnedJSON, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "spawn_agent",
		Arguments: `{"name":"inspect_team","description":"Inspect team","prompt":"Finish the agent task.","subagent_type":"general-purpose","goal_id":"workflow-run-1","goal_dir":"/tmp/workflow-run-1-goal"}`,
	})
	if err != nil {
		t.Fatalf("spawn_agent: %v", err)
	}
	var spawned struct {
		Action    string `json:"action"`
		AgentID   string `json:"agent_id"`
		AgentPath string `json:"agent_path"`
	}
	if err := json.Unmarshal([]byte(spawnedJSON), &spawned); err != nil {
		t.Fatalf("decode spawn_agent result: %v", err)
	}
	if spawned.Action != "spawn_agent" || spawned.AgentID == "" || spawned.AgentPath == "" {
		t.Fatalf("unexpected spawn_agent result: %s", spawnedJSON)
	}
	tasks, err := control.HarnessStore().ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].GoalID != "workflow-run-1" || tasks[0].GoalDir != "/tmp/workflow-run-1-goal" {
		t.Fatalf("spawn_agent did not pass goal binding to harness task: %+v", tasks)
	}

	childKit, err := New(root)
	if err != nil {
		t.Fatalf("New child kit: %v", err)
	}
	childKit.SetAgentControl(control)
	childKit.SetAgentIdentity(spawned.AgentID, spawned.AgentPath)
	if _, err := childKit.Execute(context.Background(), providers.ToolCall{
		Name:      "agent_report",
		Arguments: `{"outcome":"completed","summary":"Worker submitted structured handoff.","changed_files":["internal/tools/tool_agents.go"],"work_done":["Recorded the handoff."]}`,
	}); err != nil {
		t.Fatalf("agent_report: %v", err)
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "await_agents",
		Arguments: fmt.Sprintf(`{"targets":["%s"],"timeout_ms":1000}`, spawned.AgentID),
	}); err != nil {
		t.Fatalf("await_agents: %v", err)
	}
	listJSON, err := kit.Execute(context.Background(), providers.ToolCall{Name: "list_agents", Arguments: `{}`})
	if err != nil {
		t.Fatalf("list_agents: %v", err)
	}
	var listed struct {
		Action string           `json:"action"`
		Agents []map[string]any `json:"agents"`
	}
	if err := json.Unmarshal([]byte(listJSON), &listed); err != nil {
		t.Fatalf("decode list_agents result: %v", err)
	}
	if listed.Action != "list_agents" || len(listed.Agents) != 1 {
		t.Fatalf("unexpected list_agents result: %s", listJSON)
	}

	rootActions := map[string]string{}
	for _, record := range kit.ToolTelemetry() {
		rootActions[record.Name] = record.ResultAction
	}
	for toolName, want := range map[string]string{
		"spawn_agent":  "spawn_agent",
		"await_agents": "await_agents",
		"list_agents":  "list_agents",
	} {
		if rootActions[toolName] != want {
			t.Fatalf("%s telemetry action = %q, want %q (records=%+v)", toolName, rootActions[toolName], want, kit.ToolTelemetry())
		}
	}
	childRecords := childKit.ToolTelemetry()
	if len(childRecords) != 1 || childRecords[0].Name != "agent_report" || childRecords[0].ResultAction != "agent_report" {
		t.Fatalf("agent_report telemetry action mismatch: %+v", childRecords)
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
		Action      string     `json:"action"`
		Status      string     `json:"status"`
		Explanation string     `json:"explanation"`
		Plan        []PlanItem `json:"plan"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if parsed.Action != "update_plan" || parsed.Status != "updated" || parsed.Explanation != "" {
		t.Fatalf("unexpected response metadata: %+v", parsed)
	}
	if len(parsed.Plan) != 0 {
		t.Fatalf("tool result should not echo plan: %+v", parsed.Plan)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || records[0].ResultAction != "update_plan" {
		t.Fatalf("update_plan telemetry missing result action: %+v", records)
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

func TestToolkit_UpdatePlan_StoresConstraintLedger(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name: "update_plan",
		Arguments: `{
			"explanation":"track constraints",
			"plan":[{"step":"edit","status":"in_progress"}],
			"constraints":[{"id":"c1","text":"Do not add dependencies","source":"user","status":"active"}],
			"pre_write_check":["c1"],
			"pre_finish_check":["tests passed"]
		}`,
	})
	if err != nil {
		t.Fatalf("update_plan: %v", err)
	}

	got, ok := kit.CurrentPlan()
	if !ok {
		t.Fatal("expected stored plan")
	}
	if len(got.Constraints) != 1 || got.Constraints[0].Text != "Do not add dependencies" {
		t.Fatalf("constraint ledger not stored: %+v", got)
	}
	if len(got.PreWriteCheck) != 1 || got.PreWriteCheck[0] != "c1" {
		t.Fatalf("pre_write_check not stored: %+v", got)
	}
	got.Constraints[0].Text = "mutated"
	gotAgain, _ := kit.CurrentPlan()
	if gotAgain.Constraints[0].Text != "Do not add dependencies" {
		t.Fatalf("current plan should defensively copy constraints: %+v", gotAgain)
	}
}

func TestToolkit_UpdatePlan_RejectsEmptyConstraintText(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "update_plan",
		Arguments: `{"plan":[{"step":"edit","status":"in_progress"}],"constraints":[{"id":"c1","text":"  "}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "requires text") {
		t.Fatalf("expected constraint text validation error, got: %v", err)
	}
}

func TestToolkit_PlanContextBlocksIncludeConstraintLedger(t *testing.T) {
	blocks := PlanSnapshotContextBlocks(PlanSnapshot{
		Explanation: "track constraints",
		Plan: []PlanItem{
			{Step: "inspect", Status: PlanStatusCompleted},
			{Step: "edit", Status: PlanStatusInProgress},
		},
		Constraints: []PlanConstraint{{
			ID:     "c1",
			Text:   "Do not add dependencies",
			Source: "user",
			Status: "active",
		}},
		PreWriteCheck:  []string{"c1"},
		PreFinishCheck: []string{"tests passed"},
	})

	if len(blocks) != 2 {
		t.Fatalf("expected task state and constraint ledger blocks, got %+v", blocks)
	}
	rendered := blocks[0].Content + "\n" + blocks[1].Content
	for _, want := range []string{"track constraints", "[completed] inspect", "[in_progress] edit", "constraints:", "c1 [active source=user]", "pre_write_check:", "pre_finish_check:"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("plan context block missing %q:\n%s", want, rendered)
		}
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
		{name: "checkpoint", kind: ToolKindFile, exposure: ToolExposureDirect, risk: ToolRiskHigh, readOnly: false, concurrencySafe: false},
		{name: "repo_map", kind: ToolKindSearch, exposure: ToolExposureDirect, risk: ToolRiskLow, readOnly: true, concurrencySafe: true},
		{name: "ast_search", kind: ToolKindSearch, exposure: ToolExposureDirect, risk: ToolRiskLow, readOnly: true, concurrencySafe: true},
		{name: "semantic_search", kind: ToolKindSearch, exposure: ToolExposureDirect, risk: ToolRiskLow, readOnly: true, concurrencySafe: true},
		{name: "tool_search", kind: ToolKindDiscovery, exposure: ToolExposureDirect, risk: ToolRiskLow, readOnly: false, concurrencySafe: false},
		{name: "bash", kind: ToolKindShell, exposure: ToolExposureDirect, risk: ToolRiskHigh, readOnly: false, concurrencySafe: false},
		{name: "run_shell", kind: ToolKindShell, exposure: ToolExposureHidden, risk: ToolRiskHigh, readOnly: false, concurrencySafe: false},
		{name: "run_test", kind: ToolKindTest, exposure: ToolExposureHidden, risk: ToolRiskHigh, readOnly: false, concurrencySafe: false},
		{name: "spawn_agent", kind: ToolKindAgent, exposure: ToolExposureDirect, risk: ToolRiskHigh, readOnly: false, concurrencySafe: true},
		{name: "wait_agent", kind: ToolKindAgent, exposure: ToolExposureDirect, risk: ToolRiskMedium, readOnly: true, concurrencySafe: true},
		{name: "await_agents", kind: ToolKindAgent, exposure: ToolExposureDirect, risk: ToolRiskMedium, readOnly: true, concurrencySafe: true},
		{name: "close_agent", kind: ToolKindAgent, exposure: ToolExposureDirect, risk: ToolRiskHigh, readOnly: false, concurrencySafe: true},
		{name: "write_stdin", kind: ToolKindProcess, exposure: ToolExposureHidden, risk: ToolRiskHigh, readOnly: false, concurrencySafe: true},
		{name: "schedule_cron", kind: ToolKindSchedule, exposure: ToolExposureDeferred, risk: ToolRiskHigh, readOnly: false, concurrencySafe: false},
		{name: "session_memory", kind: ToolKindMemory, exposure: ToolExposureDirect, risk: ToolRiskMedium, readOnly: false, concurrencySafe: false},
		{name: "start_goal", kind: ToolKindGoal, exposure: ToolExposureDirect, risk: ToolRiskLow, readOnly: false, concurrencySafe: false},
		{name: "update_goal", kind: ToolKindGoal, exposure: ToolExposureDirect, risk: ToolRiskLow, readOnly: false, concurrencySafe: false},
		{name: "complete_goal", kind: ToolKindGoal, exposure: ToolExposureDirect, risk: ToolRiskLow, readOnly: false, concurrencySafe: false},
		{name: "goal_status", kind: ToolKindGoal, exposure: ToolExposureDirect, risk: ToolRiskLow, readOnly: true, concurrencySafe: true},
		{name: "list_agent_profiles", kind: ToolKindWorkflow, exposure: ToolExposureDirect, risk: ToolRiskLow, readOnly: true, concurrencySafe: true},
		{name: "create_agent_profile", kind: ToolKindWorkflow, exposure: ToolExposureDirect, risk: ToolRiskHigh, readOnly: false, concurrencySafe: true},
		{name: "start_workflow", kind: ToolKindWorkflow, exposure: ToolExposureDirect, risk: ToolRiskHigh, readOnly: false, concurrencySafe: false},
		{name: "create_workflow", kind: ToolKindWorkflow, exposure: ToolExposureDeferred, risk: ToolRiskHigh, readOnly: false, concurrencySafe: true},
		{name: "run_workflow", kind: ToolKindWorkflow, exposure: ToolExposureDeferred, risk: ToolRiskHigh, readOnly: false, concurrencySafe: false},
		{name: "save_workflow", kind: ToolKindWorkflow, exposure: ToolExposureDirect, risk: ToolRiskHigh, readOnly: false, concurrencySafe: false},
		{name: "workflow_control", kind: ToolKindWorkflow, exposure: ToolExposureDirect, risk: ToolRiskHigh, readOnly: false, concurrencySafe: false},
		{name: "workflow_status", kind: ToolKindWorkflow, exposure: ToolExposureDirect, risk: ToolRiskLow, readOnly: true, concurrencySafe: true},
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

func TestToolkit_EditToolModeForModelUsesOpenAIPatchFirstSurface(t *testing.T) {
	tests := []struct {
		model string
		want  EditToolMode
	}{
		{model: "gpt-5.5", want: EditToolModePatch},
		{model: "openai/gpt-5-codex", want: EditToolModePatch},
		{model: "gpt-4.1-mini", want: EditToolModePatch},
		{model: "openai/gpt-oss-120b", want: EditToolModePatch},
		{model: "anthropic/claude-sonnet-4-5", want: EditToolModeText},
	}
	for _, tt := range tests {
		if got := EditToolModeForModel(tt.model); got != tt.want {
			t.Fatalf("EditToolModeForModel(%q) = %s, want %s", tt.model, got, tt.want)
		}
	}
}

func TestToolkit_EditToolModeForProviderModelUsesProfileFamily(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		want     EditToolMode
	}{
		{provider: "openai", model: "gpt-5-codex", want: EditToolModePatch},
		{provider: "anthropic", model: "claude-sonnet-4-5", want: EditToolModeText},
		{provider: "google", model: "gemini-2.5-pro", want: EditToolModeText},
		{provider: "ollama", model: "llama-coder", want: EditToolModeText},
	}
	for _, tt := range tests {
		if got := EditToolModeForProviderModel(tt.provider, tt.model); got != tt.want {
			t.Fatalf("EditToolModeForProviderModel(%q, %q) = %s, want %s", tt.provider, tt.model, got, tt.want)
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
		Arguments: `{"subcommand":"add","args":["hello.txt"]}`,
	})
	if !ok {
		t.Fatal("git metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Destructive || meta.Risk != string(ToolRiskMedium) || meta.Reason != "git add writes the repository index" {
		t.Fatalf("git add metadata = %+v, want non-destructive medium-risk index write", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "git",
		Arguments: `{"subcommand":"restore --staged","args":["hello.txt"]}`,
	})
	if !ok {
		t.Fatal("git metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Destructive || meta.Risk != string(ToolRiskMedium) || meta.Reason != "git restore --staged writes the repository index" {
		t.Fatalf("git restore --staged metadata = %+v, want non-destructive medium-risk index write", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "git",
		Arguments: `{"subcommand":"commit","args":["-m","update files"]}`,
	})
	if !ok {
		t.Fatal("git metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Destructive || meta.Risk != string(ToolRiskMedium) || meta.Reason != "git commit writes local repository history" {
		t.Fatalf("git commit metadata = %+v, want non-destructive medium-risk local write", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "git",
		Arguments: `{"subcommand":"push"}`,
	})
	if !ok {
		t.Fatal("git metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Destructive || meta.Risk != string(ToolRiskMedium) || meta.Reason != "git push writes remote branch state" {
		t.Fatalf("git push metadata = %+v, want non-destructive medium-risk remote write", meta)
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

func TestToolkit_ToolMetadata_ClassifiesCheckpointByInput(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	meta, ok := kit.ToolMetadata(providers.ToolCall{
		Name:      "checkpoint",
		Arguments: `{"action":"list"}`,
	})
	if !ok {
		t.Fatal("checkpoint metadata not found")
	}
	if !meta.ReadOnly || !meta.ConcurrencySafe || meta.Risk != string(ToolRiskLow) {
		t.Fatalf("checkpoint list metadata = %+v, want read-only low-risk concurrent", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "checkpoint",
		Arguments: `{"action":"create","paths":["a.txt"]}`,
	})
	if !ok {
		t.Fatal("checkpoint metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Risk != string(ToolRiskLow) {
		t.Fatalf("checkpoint create metadata = %+v, want low-risk artifact write", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "checkpoint",
		Arguments: `{"action":"restore","checkpoint_id":"before-edit"}`,
	})
	if !ok {
		t.Fatal("checkpoint metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || !meta.Destructive || meta.Risk != string(ToolRiskHigh) {
		t.Fatalf("checkpoint restore metadata = %+v, want destructive high-risk serial", meta)
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

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"rm -rf tmp && echo done"}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || !meta.Destructive || meta.Risk != string(ToolRiskHigh) || meta.Reason != "destructive shell command" {
		t.Fatalf("destructive shell metadata = %+v, want destructive high-risk serial", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"go test ./..."}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Risk != string(ToolRiskMedium) || meta.Reason != "local verification command" {
		t.Fatalf("go test metadata = %+v, want medium-risk verification", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"cd pkg && go test ./..."}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Risk != string(ToolRiskMedium) || meta.Reason != "local verification command" {
		t.Fatalf("directory-scoped go test metadata = %+v, want medium-risk verification", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"cd .. && go test ./..."}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if meta.ReadOnly || meta.Risk != string(ToolRiskHigh) {
		t.Fatalf("parent-directory shell metadata = %+v, want high-risk", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"cat .env"}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if meta.ReadOnly || meta.Risk != string(ToolRiskHigh) || meta.Reason != "shell command may read secrets" {
		t.Fatalf("secret-reading shell metadata = %+v, want high-risk secret classification", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"env | grep TOKEN"}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if meta.ReadOnly || meta.Risk != string(ToolRiskHigh) || meta.Reason != "shell command may expose environment secrets" {
		t.Fatalf("environment dump shell metadata = %+v, want high-risk environment classification", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"git status"}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if !meta.ReadOnly || !meta.ConcurrencySafe || meta.Risk != string(ToolRiskLow) || meta.Reason != "git shell read-only command" {
		t.Fatalf("git status shell metadata = %+v, want read-only low-risk git shell", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"cd pkg && git status"}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if !meta.ReadOnly || !meta.ConcurrencySafe || meta.Risk != string(ToolRiskLow) || meta.Reason != "git shell read-only command" {
		t.Fatalf("nested git status shell metadata = %+v, want read-only low-risk git shell", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"git add hello.txt"}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Risk != string(ToolRiskMedium) || meta.Reason != "git shell add writes the repository index" {
		t.Fatalf("git add shell metadata = %+v, want medium-risk index write", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"git commit -m \"update files\""}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Risk != string(ToolRiskMedium) || meta.Reason != "git shell commit writes local repository history" {
		t.Fatalf("git commit shell metadata = %+v, want medium-risk local history write", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"git push"}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Risk != string(ToolRiskHigh) || meta.Reason != "git shell push writes remote branch state" {
		t.Fatalf("git push shell metadata = %+v, want high-risk remote write", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"git reset --hard"}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if meta.ReadOnly || !meta.Destructive || meta.Risk != string(ToolRiskHigh) || meta.Reason != "destructive git shell command" {
		t.Fatalf("destructive git shell metadata = %+v, want destructive high-risk git shell", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"git config user.name tester"}`,
	})
	if !ok {
		t.Fatal("run_shell metadata not found")
	}
	if meta.ReadOnly || meta.Destructive || meta.Risk != string(ToolRiskHigh) || meta.Reason != "unsupported git shell command" {
		t.Fatalf("unsupported git shell metadata = %+v, want high-risk unsupported git shell", meta)
	}
}

func TestToolkit_ToolMetadata_ClassifiesStartProcessByInput(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	meta, ok := kit.ToolMetadata(providers.ToolCall{
		Name:      "start_process",
		Arguments: `{"command":"npm run dev","owner_kind":"main_agent"}`,
	})
	if !ok {
		t.Fatal("start_process metadata not found")
	}
	if meta.ReadOnly || meta.ConcurrencySafe || meta.Risk != string(ToolRiskHigh) || !strings.Contains(meta.Reason, "process command") {
		t.Fatalf("start_process metadata = %+v, want high-risk managed process", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "start_process",
		Arguments: `{"command":"cat .env","owner_kind":"main_agent"}`,
	})
	if !ok {
		t.Fatal("start_process metadata not found")
	}
	if meta.ReadOnly || meta.Risk != string(ToolRiskHigh) || !strings.Contains(meta.Reason, "read secrets") {
		t.Fatalf("secret-reading start_process metadata = %+v, want high-risk secret classification", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "start_process",
		Arguments: `{"command":"env | grep TOKEN","owner_kind":"main_agent"}`,
	})
	if !ok {
		t.Fatal("start_process metadata not found")
	}
	if meta.ReadOnly || meta.Risk != string(ToolRiskHigh) || !strings.Contains(meta.Reason, "environment secrets") {
		t.Fatalf("environment dump start_process metadata = %+v, want high-risk environment classification", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "start_process",
		Arguments: `{"command":"git status","owner_kind":"main_agent"}`,
	})
	if !ok {
		t.Fatal("start_process metadata not found")
	}
	if meta.ReadOnly || meta.Risk != string(ToolRiskHigh) || !strings.Contains(meta.Reason, "git shell read-only command") {
		t.Fatalf("git start_process metadata = %+v, want high-risk managed git process", meta)
	}

	meta, ok = kit.ToolMetadata(providers.ToolCall{
		Name:      "start_process",
		Arguments: `{"command":"command rm -rf tmp","owner_kind":"main_agent"}`,
	})
	if !ok {
		t.Fatal("start_process metadata not found")
	}
	if meta.ReadOnly || !meta.Destructive || meta.Risk != string(ToolRiskHigh) || !strings.Contains(meta.Reason, "destructive shell command") {
		t.Fatalf("destructive start_process metadata = %+v, want destructive high-risk guidance", meta)
	}
}

func TestToolkit_RunTestToolExecutesVerificationCommand(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/run-test\n\ngo 1.22\n")
	mustWriteFile(t, filepath.Join(root, "pkg_test.go"), `package runtest

import "testing"

func TestOK(t *testing.T) {}
`)
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_test",
		Arguments: `{"command":"go test ./...","scope":"targeted","purpose":"verify test wrapper"}`,
	})
	if err != nil {
		t.Fatalf("run_test: %v", err)
	}
	var got struct {
		Action         string             `json:"action"`
		Passed         bool               `json:"passed"`
		ExitCode       int                `json:"exit_code"`
		Scope          string             `json:"scope"`
		Classification ToolClassification `json:"classification"`
		FailureSummary testFailureSummary `json:"failure_summary"`
		Suggestions    []string           `json:"next_suggestions"`
	}
	if err := json.Unmarshal([]byte(resp), &got); err != nil {
		t.Fatalf("parse run_test response: %v\n%s", err, resp)
	}
	if got.Action != "run" || !got.Passed || got.ExitCode != 0 || got.Scope != "targeted" {
		t.Fatalf("unexpected run_test success response: %+v", got)
	}
	if got.Classification.Risk != ToolRiskMedium || got.Classification.Reason != "local verification command" {
		t.Fatalf("unexpected run_test classification: %+v", got.Classification)
	}
	if got.FailureSummary.Failed {
		t.Fatalf("passing test should not report failure summary: %+v", got.FailureSummary)
	}
	if len(got.Suggestions) == 0 || !strings.Contains(strings.Join(got.Suggestions, " "), "final response") {
		t.Fatalf("passing run_test response missing finish suggestion: %+v", got.Suggestions)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || records[0].ResultAction != "run" {
		t.Fatalf("run_test telemetry missing result action: %+v", records)
	}
}

func TestToolkit_RunTestToolExecutesDirectoryScopedVerification(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "pkg", "go.mod"), "module example.com/pkg\n\ngo 1.22\n")
	mustWriteFile(t, filepath.Join(root, "pkg", "pkg_test.go"), `package pkg

import "testing"

func TestOK(t *testing.T) {}
`)
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_test",
		Arguments: `{"command":"cd pkg && go test ./...","scope":"targeted"}`,
	})
	if err != nil {
		t.Fatalf("run_test directory-scoped verification: %v", err)
	}
	var got struct {
		Passed         bool               `json:"passed"`
		Classification ToolClassification `json:"classification"`
	}
	if err := json.Unmarshal([]byte(resp), &got); err != nil {
		t.Fatalf("parse run_test response: %v\n%s", err, resp)
	}
	if !got.Passed || got.Classification.Risk != ToolRiskMedium || got.Classification.Reason != "local verification command" {
		t.Fatalf("unexpected directory-scoped run_test response: %+v", got)
	}
}

func TestToolkit_RunTestResolvesNpxVitestToLocalRunner(t *testing.T) {
	root := t.TempDir()
	localVitest := filepath.Join(root, "node_modules", ".bin", "vitest")
	mustWriteFile(t, localVitest, "#!/usr/bin/env sh\nprintf 'vitest local ok\\n'\n")
	if err := os.Chmod(localVitest, 0o755); err != nil {
		t.Fatalf("chmod local vitest: %v", err)
	}
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_test",
		Arguments: `{"command":"npx vitest --run","scope":"targeted"}`,
	})
	if err != nil {
		t.Fatalf("run_test npx vitest should resolve local runner: %v", err)
	}
	var got struct {
		Passed           bool               `json:"passed"`
		Command          string             `json:"command"`
		RequestedCommand string             `json:"requested_command"`
		ResolvedCommand  string             `json:"resolved_command"`
		Output           string             `json:"output"`
		Classification   ToolClassification `json:"classification"`
	}
	if err := json.Unmarshal([]byte(resp), &got); err != nil {
		t.Fatalf("parse run_test response: %v\n%s", err, resp)
	}
	if !got.Passed || got.RequestedCommand != "npx vitest --run" || got.ResolvedCommand != "./node_modules/.bin/vitest --run" || got.Command != got.ResolvedCommand {
		t.Fatalf("unexpected resolved npx response: %+v", got)
	}
	if !strings.Contains(got.Output, "vitest local ok") {
		t.Fatalf("run_test output missing local runner evidence: %+v", got)
	}
	if got.Classification.Risk != ToolRiskMedium || got.Classification.Reason != "local verification command" {
		t.Fatalf("unexpected npx vitest classification: %+v", got.Classification)
	}
}

func TestToolkit_RunTestResolvesDirectoryScopedNpxVitest(t *testing.T) {
	root := t.TempDir()
	localVitest := filepath.Join(root, "desktop", "node_modules", ".bin", "vitest")
	mustWriteFile(t, localVitest, "#!/usr/bin/env sh\nprintf 'desktop vitest ok\\n'\n")
	if err := os.Chmod(localVitest, 0o755); err != nil {
		t.Fatalf("chmod local vitest: %v", err)
	}
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_test",
		Arguments: `{"command":"cd desktop && npx vitest","scope":"targeted"}`,
	})
	if err != nil {
		t.Fatalf("run_test directory npx vitest should resolve local runner: %v", err)
	}
	var got struct {
		Passed           bool   `json:"passed"`
		Command          string `json:"command"`
		RequestedCommand string `json:"requested_command"`
		ResolvedCommand  string `json:"resolved_command"`
		Output           string `json:"output"`
	}
	if err := json.Unmarshal([]byte(resp), &got); err != nil {
		t.Fatalf("parse run_test response: %v\n%s", err, resp)
	}
	if !got.Passed || got.RequestedCommand != "cd desktop && npx vitest" || got.ResolvedCommand != "cd desktop && ./node_modules/.bin/vitest" || got.Command != got.ResolvedCommand {
		t.Fatalf("unexpected directory resolved npx response: %+v", got)
	}
	if !strings.Contains(got.Output, "desktop vitest ok") {
		t.Fatalf("run_test output missing directory runner evidence: %+v", got)
	}
}

func TestToolkit_RunTestRejectsMissingLocalNpxRunner(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_test",
		Arguments: `{"command":"npx vitest","scope":"targeted"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=local_test_runner_missing") || !strings.Contains(err.Error(), "node_modules/.bin/vitest") {
		t.Fatalf("expected missing local runner rejection, got %v", err)
	}
}

func TestToolkit_RunTestToolSummarizesFailures(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/run-test\n\ngo 1.22\n")
	mustWriteFile(t, filepath.Join(root, "pkg_test.go"), `package runtest

import "testing"

func TestBroken(t *testing.T) {
	t.Fatalf("expected 1 got 2 API_KEY=secret-value-1234567890")
}
`)
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sessionDir := filepath.Join(t.TempDir(), "session")
	kit.SetSessionDir(sessionDir)

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_test",
		Arguments: `{"command":"go test ./...","scope":"targeted"}`,
	})
	if err != nil {
		t.Fatalf("run_test should return failed test output, not tool error: %v", err)
	}
	var got struct {
		Passed         bool               `json:"passed"`
		ExitCode       int                `json:"exit_code"`
		FailureSummary testFailureSummary `json:"failure_summary"`
		Suggestions    []string           `json:"next_suggestions"`
		Revision       string             `json:"workspace_revision"`
		FullLogRef     string             `json:"full_log_ref"`
		FullLogBytes   int                `json:"full_log_bytes"`
	}
	if err := json.Unmarshal([]byte(resp), &got); err != nil {
		t.Fatalf("parse run_test response: %v\n%s", err, resp)
	}
	if got.Passed || got.ExitCode == 0 || !got.FailureSummary.Failed {
		t.Fatalf("unexpected failed run_test response: %+v", got)
	}
	if len(got.FailureSummary.FailingTests) == 0 || got.FailureSummary.FailingTests[0] != "TestBroken" {
		t.Fatalf("failure summary missing failing test: %+v", got.FailureSummary)
	}
	if len(got.FailureSummary.Locations) == 0 ||
		got.FailureSummary.Locations[0].Path != "pkg_test.go" ||
		got.FailureSummary.Locations[0].Line <= 0 ||
		strings.Contains(got.FailureSummary.Locations[0].Text, "secret-value") {
		t.Fatalf("failure summary missing redacted file location: %+v", got.FailureSummary)
	}
	if !strings.HasPrefix(got.Revision, "fs:worktree:") {
		t.Fatalf("run_test response missing filesystem workspace revision: %+v", got)
	}
	if got.FullLogRef == "" || got.FullLogBytes <= 0 {
		t.Fatalf("run_test response missing full log artifact: %+v", got)
	}
	if !strings.HasPrefix(got.FullLogRef, filepath.Join(sessionDir, "tool-results", "run-test-logs")) {
		t.Fatalf("full log ref outside session dir: %q", got.FullLogRef)
	}
	logData, err := os.ReadFile(got.FullLogRef)
	if err != nil {
		t.Fatalf("read full log artifact: %v", err)
	}
	logText := string(logData)
	if strings.Contains(logText, "secret-value") || !strings.Contains(logText, "[REDACTED]") {
		t.Fatalf("full log artifact should be redacted:\n%s", logText)
	}
	if !strings.Contains(logText, "TestBroken") || !strings.Contains(logText, "exit_code: 1") {
		t.Fatalf("full log artifact missing failure evidence:\n%s", logText)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || !containsString(records[0].ArtifactRefs, got.FullLogRef) {
		t.Fatalf("tool telemetry missing full log artifact ref: records=%+v full_log_ref=%q", records, got.FullLogRef)
	}
	envelope := records[0].ResultEnvelope()
	artifactRefs, ok := envelope.Data["artifact_refs"].([]string)
	if !ok || !containsString(artifactRefs, got.FullLogRef) {
		t.Fatalf("result envelope missing full log artifact ref: %+v", envelope)
	}
	if strings.Contains(resp, "secret-value") || !strings.Contains(resp, "[REDACTED]") {
		t.Fatalf("run_test response should redact secret-like output: %s", resp)
	}
	if len(got.Suggestions) == 0 || !strings.Contains(strings.Join(got.Suggestions, " "), "hypothesis") {
		t.Fatalf("failed run_test response missing debug suggestion: %+v", got.Suggestions)
	}
}

func TestToolkit_BashFailureContextBlockTracksStaleness(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/failure-context\n\ngo 1.22\n")
	mustWriteFile(t, filepath.Join(root, "pkg_test.go"), `package failurecontext

import "testing"

func TestBroken(t *testing.T) {
	t.Fatalf("expected 1 got 2 API_KEY=secret-value-1234567890")
}
`)
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sessionDir := filepath.Join(t.TempDir(), "session")
	kit.SetSessionDir(sessionDir)

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "bash",
		Arguments: `{"command":"go test ./...","scope":"targeted","purpose":"capture failure context API_KEY=secret-value-1234567890"}`,
	})
	if err != nil {
		t.Fatalf("bash should return failed test output, not tool error: %v", err)
	}
	if !strings.Contains(resp, "[REDACTED]") {
		t.Fatalf("bash response should redact secret-like output: %s", resp)
	}

	block, ok := kit.TestFailureContextBlock()
	if !ok {
		t.Fatal("expected test failure context block")
	}
	if block.Kind != wuucontext.BlockTestFailures || block.Source != "bash" {
		t.Fatalf("unexpected context block metadata: %+v", block)
	}
	if strings.Contains(block.Source, "run_test") || strings.Contains(block.Content, "run_test") {
		t.Fatalf("failure context must not expose legacy test tool name:\nsource=%s\n%s", block.Source, block.Content)
	}
	if !strings.Contains(block.Content, "status: current") ||
		!strings.Contains(block.Content, "command: go test ./...") ||
		!strings.Contains(block.Content, "scope: targeted") ||
		!strings.Contains(block.Content, "purpose: capture failure context API_KEY=[REDACTED]") ||
		!strings.Contains(block.Content, "failure_revision: fs:worktree:") ||
		!strings.Contains(block.Content, "current_revision: fs:worktree:") ||
		!strings.Contains(block.Content, "full_log_ref: "+sessionDir) ||
		!strings.Contains(block.Content, "failing_tests:\n- TestBroken") ||
		!strings.Contains(block.Content, "next_suggestion: inspect implicated files") {
		t.Fatalf("unexpected current failure context:\n%s", block.Content)
	}
	if strings.Contains(block.Content, "secret-value") {
		t.Fatalf("failure context should redact secret-like text:\n%s", block.Content)
	}

	blocks := kit.ContextBlocks()
	found := false
	for _, candidate := range blocks {
		if candidate.Kind == wuucontext.BlockTestFailures {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ContextBlocks missing TEST_FAILURES block: %+v", blocks)
	}

	mustWriteFile(t, filepath.Join(root, "pkg.go"), "package failurecontext\n")
	block, ok = kit.TestFailureContextBlock()
	if !ok {
		t.Fatal("expected stale test failure context block")
	}
	if !strings.Contains(block.Content, "status: possibly_stale") ||
		!strings.Contains(block.Content, "next_suggestion: workspace changed since this failure") {
		t.Fatalf("unexpected stale failure context:\n%s", block.Content)
	}
}

func TestToolkit_RunTestToolBlocksRepeatedFailuresAtSameRevision(t *testing.T) {
	root := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, string(out))
		}
	}
	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test User")
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/repeat-test\n\ngo 1.22\n")
	mustWriteFile(t, filepath.Join(root, "pkg_test.go"), `package repeattest

import "testing"

func TestBroken(t *testing.T) {
	t.Fatalf("expected 1 got 2")
}
`)
	runGit("add", "go.mod", "pkg_test.go")
	runGit("commit", "-m", "initial")

	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	call := providers.ToolCall{
		Name:      "run_test",
		Arguments: `{"command":"go test ./...","scope":"targeted"}`,
	}
	for i := 0; i < maxRepeatedRunTestFailures; i++ {
		resp, err := kit.Execute(context.Background(), call)
		if err != nil {
			t.Fatalf("run_test attempt %d should execute: %v", i+1, err)
		}
		var got struct {
			Passed       bool               `json:"passed"`
			RepeatGuard  map[string]any     `json:"repeat_guard"`
			Revision     string             `json:"workspace_revision"`
			FailureState testFailureSummary `json:"failure_summary"`
		}
		if err := json.Unmarshal([]byte(resp), &got); err != nil {
			t.Fatalf("parse run_test response: %v\n%s", err, resp)
		}
		if got.Passed || got.Revision == "" || !got.FailureState.Failed {
			t.Fatalf("unexpected failed run_test response: %+v", got)
		}
	}
	_, err = kit.Execute(context.Background(), call)
	if err == nil || !strings.Contains(err.Error(), "error_kind=repeated_failure_same_revision") ||
		!strings.Contains(err.Error(), "workspace_revision=") ||
		!strings.Contains(err.Error(), "command_hash=") ||
		!strings.Contains(err.Error(), "safe_retry=") ||
		!strings.Contains(err.Error(), "model_next_action=") {
		t.Fatalf("expected repeated failure guard, got: %v", err)
	}

	mustWriteFile(t, filepath.Join(root, "pkg_test.go"), `package repeattest

import "testing"

func TestBroken(t *testing.T) {}
`)
	resp, err := kit.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("run_test after workspace change should execute: %v", err)
	}
	var got struct {
		Passed bool `json:"passed"`
	}
	if err := json.Unmarshal([]byte(resp), &got); err != nil {
		t.Fatalf("parse run_test response: %v\n%s", err, resp)
	}
	if !got.Passed {
		t.Fatalf("expected passing run_test after workspace change: %s", resp)
	}
}

func TestToolkit_RunTestToolRejectsNonVerificationCommands(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_test",
		Arguments: `{"command":"pwd"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "local verification commands") {
		t.Fatalf("expected non-verification command rejection, got %v", err)
	}
}

func TestToolkit_RunTestToolRejectsSensitivePaths(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_test",
		Arguments: `{"command":"go test ./.env"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "sensitive paths") {
		t.Fatalf("expected sensitive path rejection, got %v", err)
	}
}

func TestToolkit_RunTestToolRejectsEnvironmentDumps(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_test",
		Arguments: `{"command":"env | grep TOKEN"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "environment variables") {
		t.Fatalf("expected environment dump rejection, got %v", err)
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

func TestToolkit_DefersLowFrequencyAndLargeMCPToolSetsFromDefinitions(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	registered := []Tool{
		NewReadFileTool(kit.env),
		NewToolSearchTool(kit),
		NewScheduleCronTool(kit.env),
	}
	for _, name := range []string{"mcp_docs_search", "mcp_docs_read", "mcp_docs_write", "mcp_docs_list", "mcp_docs_status"} {
		registered = append(registered, &stubTool{
			name: name,
			def:  providers.ToolDefinition{Name: name, Description: "Search docs through MCP"},
		})
	}
	kit.registry = NewRegistry(registered...)

	defs := definitionNames(kit.Definitions())
	for _, name := range []string{"read_file", "tool_search"} {
		if !defs[name] {
			t.Fatalf("%s should be directly exposed", name)
		}
	}
	for _, name := range []string{"schedule_cron", "mcp_docs_search", "mcp_docs_read", "mcp_docs_write", "mcp_docs_list", "mcp_docs_status"} {
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

func TestToolkit_ExposesSmallStableMCPToolSetAfterStablePrefix(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.registry = NewRegistry(
		NewReadFileTool(kit.env),
		NewToolSearchTool(kit),
		&stubTool{
			name:   "mcp_docs_search",
			def:    providers.ToolDefinition{Name: "mcp_docs_search", Description: "Search docs through MCP", InputSchema: map[string]any{"type": "object"}},
			result: `{"action":"mcp_docs_search"}`,
		},
		&stubTool{
			name:   "mcp_docs_read",
			def:    providers.ToolDefinition{Name: "mcp_docs_read", Description: "Read docs through MCP", InputSchema: map[string]any{"type": "object"}},
			result: `{"action":"mcp_docs_read"}`,
		},
	)

	defs := kit.Definitions()
	names := definitionNames(defs)
	if !names["mcp_docs_search"] || !names["mcp_docs_read"] {
		t.Fatalf("small stable MCP tool set should be directly visible: %+v", defs)
	}
	if len(defs) != 4 {
		t.Fatalf("expected 4 definitions, got %+v", defs)
	}
	if defs[0].Name != "read_file" || defs[1].Name != "tool_search" {
		t.Fatalf("stable built-in prefix should remain first, got %+v", defs)
	}
	if !defs[0].CacheStable || !defs[1].CacheStable {
		t.Fatalf("built-in prefix should stay cache-stable: %+v", defs)
	}
	if defs[2].CacheStable || defs[3].CacheStable {
		t.Fatalf("direct MCP tools must stay outside cache-stable prefix: %+v", defs)
	}
	info, ok := kit.ToolInfo("mcp_docs_search")
	if !ok || info.Exposure != ToolExposureDirect {
		t.Fatalf("small MCP tool exposure = %+v, ok=%v", info, ok)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{Name: "mcp_docs_search", Arguments: `{}`}); err != nil {
		t.Fatalf("direct small MCP tool should execute without tool_search: %v", err)
	}
}

func TestToolkit_DefersOversizedMCPToolEvenInSmallSet(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.registry = NewRegistry(
		NewToolSearchTool(kit),
		&stubTool{
			name: "mcp_docs_search",
			def: providers.ToolDefinition{
				Name:        "mcp_docs_search",
				Description: strings.Repeat("verbose ", directMCPDescriptionMaxRunes),
				InputSchema: map[string]any{"type": "object"},
			},
		},
	)

	if definitionNames(kit.Definitions())["mcp_docs_search"] {
		t.Fatal("oversized MCP tool should stay deferred")
	}
	info, ok := kit.ToolInfo("mcp_docs_search")
	if !ok || info.Exposure != ToolExposureDeferred {
		t.Fatalf("oversized MCP exposure = %+v, ok=%v", info, ok)
	}
}

func TestToolkit_DefinitionsKeepStableCachePrefixBeforeActivatedDeferredTools(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.registry = NewRegistry(
		NewReadFileTool(kit.env),
		NewScheduleCronTool(kit.env),
		NewToolSearchTool(kit),
	)
	kit.activateDeferredTools("schedule_cron")

	defs := kit.Definitions()
	if len(defs) != 3 {
		t.Fatalf("expected 3 definitions, got %+v", defs)
	}
	wantNames := []string{"read_file", "tool_search", "schedule_cron"}
	for i, want := range wantNames {
		if defs[i].Name != want {
			t.Fatalf("definition %d = %q, want %q; all=%+v", i, defs[i].Name, want, defs)
		}
	}
	if !defs[0].CacheStable || !defs[1].CacheStable {
		t.Fatalf("stable built-in tools should be cache-stable: %+v", defs)
	}
	if defs[2].CacheStable {
		t.Fatalf("activated deferred tool should stay out of cache-stable prefix: %+v", defs[2])
	}
}

func TestToolkit_ToolSearchActivatesDeferredTool(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.registry = NewRegistry(
		NewReadFileTool(kit.env),
		NewToolSearchTool(kit),
		&stubTool{
			name: "mcp_docs_search",
			def: providers.ToolDefinition{
				Name:        "mcp_docs_search",
				Description: "Search docs through MCP",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
					},
				},
			},
		},
		&stubTool{name: "mcp_other_one", def: providers.ToolDefinition{Name: "mcp_other_one", Description: "Other MCP tool"}},
		&stubTool{name: "mcp_other_two", def: providers.ToolDefinition{Name: "mcp_other_two", Description: "Other MCP tool"}},
		&stubTool{name: "mcp_other_three", def: providers.ToolDefinition{Name: "mcp_other_three", Description: "Other MCP tool"}},
		&stubTool{name: "mcp_other_four", def: providers.ToolDefinition{Name: "mcp_other_four", Description: "Other MCP tool"}},
	)

	initialDefs := kit.Definitions()
	if len(initialDefs) != 2 || initialDefs[0].Name != "read_file" || initialDefs[1].Name != "tool_search" {
		t.Fatalf("large MCP set should start behind stable built-ins, got %+v", initialDefs)
	}
	if !initialDefs[0].CacheStable || !initialDefs[1].CacheStable {
		t.Fatalf("initial built-in tools should be cache-stable: %+v", initialDefs)
	}
	if definitionNames(initialDefs)["mcp_docs_search"] {
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
		Action        string                             `json:"action"`
		ExposedTools  []string                           `json:"exposed_tools"`
		LoadableTools []providers.LoadableToolDefinition `json:"loadable_tools"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse tool_search response: %v", err)
	}
	if parsed.Action != "tool_search" {
		t.Fatalf("tool_search action = %q, want tool_search", parsed.Action)
	}
	if !reflect.DeepEqual(parsed.ExposedTools, []string{"mcp_docs_search"}) {
		t.Fatalf("exposed tools = %+v, want mcp_docs_search", parsed.ExposedTools)
	}
	if len(parsed.LoadableTools) != 1 {
		t.Fatalf("loadable tools = %+v, want one entry", parsed.LoadableTools)
	}
	loadable := parsed.LoadableTools[0]
	if loadable.Type != "function" || loadable.Name != "mcp_docs_search" || !loadable.DeferLoading {
		t.Fatalf("unexpected loadable tool: %+v", loadable)
	}
	if loadable.InputSchema["type"] != "object" {
		t.Fatalf("loadable tool lost input schema: %+v", loadable.InputSchema)
	}
	records := kit.ToolTelemetry()
	if len(records) == 0 || records[len(records)-1].ResultAction != "tool_search" {
		t.Fatalf("tool_search telemetry missing result action: %+v", records)
	}
	if !definitionNames(kit.Definitions())["mcp_docs_search"] {
		t.Fatal("mcp_docs_search should be exposed after tool_search")
	}
	activatedDefs := kit.Definitions()
	if len(activatedDefs) != 3 {
		t.Fatalf("expected activated definition appended after stable prefix, got %+v", activatedDefs)
	}
	wantNames := []string{"read_file", "tool_search", "mcp_docs_search"}
	for i, want := range wantNames {
		if activatedDefs[i].Name != want {
			t.Fatalf("definition %d = %q, want %q; all=%+v", i, activatedDefs[i].Name, want, activatedDefs)
		}
	}
	if !activatedDefs[0].CacheStable || !activatedDefs[1].CacheStable {
		t.Fatalf("activated built-in prefix should stay cache-stable: %+v", activatedDefs)
	}
	if activatedDefs[2].CacheStable {
		t.Fatalf("activated MCP tool should stay outside cache-stable prefix: %+v", activatedDefs[2])
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

func TestToolkit_ToolSearchActivatesWorkflowDriverOverride(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if definitionNames(kit.Definitions())["run_workflow"] {
		t.Fatal("run_workflow should start deferred")
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_workflow",
		Arguments: `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "deferred") {
		t.Fatalf("expected deferred execution error, got %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "tool_search",
		Arguments: `{"query":"script workflow driver override"}`,
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
	if !containsString(parsed.ExposedTools, "run_workflow") {
		t.Fatalf("tool_search did not expose run_workflow: %s", resp)
	}
	if !definitionNames(kit.Definitions())["run_workflow"] {
		t.Fatal("run_workflow should be visible after tool_search")
	}
	info, ok := kit.ToolInfo("run_workflow")
	if !ok {
		t.Fatal("ToolInfo(run_workflow) not found")
	}
	if info.Exposure != ToolExposureDirect {
		t.Fatalf("run_workflow exposure = %s, want %s", info.Exposure, ToolExposureDirect)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_workflow",
		Arguments: `{}`,
	})
	if err == nil || strings.Contains(err.Error(), "deferred") {
		t.Fatalf("expected argument validation after activation, got %v", err)
	}
}

func TestToolkit_MCPToolResultsAreRedacted(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.registry = NewRegistry(
		NewToolSearchTool(kit),
		&stubTool{
			name:   "mcp_docs_search",
			def:    providers.ToolDefinition{Name: "mcp_docs_search", Description: "Search docs through MCP"},
			result: "API_KEY=mcp-secret-value-1234567890",
		},
	)

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "tool_search",
		Arguments: `{"query":"docs search"}`,
	}); err != nil {
		t.Fatalf("tool_search: %v", err)
	}
	resp, err := kit.Execute(context.Background(), providers.ToolCall{Name: "mcp_docs_search", Arguments: `{}`})
	if err != nil {
		t.Fatalf("mcp_docs_search: %v", err)
	}
	if strings.Contains(resp, "mcp-secret-value") || !strings.Contains(resp, "[REDACTED]") {
		t.Fatalf("MCP result should be redacted: %s", resp)
	}
	records := kit.ToolTelemetry()
	if len(records) == 0 || records[len(records)-1].Kind != ToolKindMCP {
		t.Fatalf("expected MCP telemetry record, got %+v", records)
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
	if record.ArgumentsSHA256 != toolArgumentsSHA256(`{"path":"a.txt"}`) {
		t.Fatalf("unexpected argument fingerprint: %+v", record)
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
	if !strings.HasPrefix(record.RevisionBefore, "fs:worktree:") || record.RevisionBefore != record.RevisionAfter {
		t.Fatalf("read-only non-git record should preserve filesystem revision: %+v", record)
	}
}

func TestToolkit_RepeatedToolInputGuardBlocksThirdIdenticalCall(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "a.txt"), "hello\n")

	call := providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"a.txt"}`,
	}
	for i := 0; i < 2; i++ {
		call.ID = fmt.Sprintf("call-read-%d", i+1)
		if _, err := kit.Execute(context.Background(), call); err != nil {
			t.Fatalf("read_file attempt %d: %v", i+1, err)
		}
	}
	call.ID = "call-read-3"
	_, err = kit.Execute(context.Background(), call)
	if err == nil ||
		!strings.Contains(err.Error(), "error_kind=repeated_tool_input") ||
		!strings.Contains(err.Error(), "model_next_action") {
		t.Fatalf("expected repeated input guard, got %v", err)
	}

	records := kit.ToolTelemetry()
	if len(records) != 3 {
		t.Fatalf("expected 3 telemetry records, got %d", len(records))
	}
	record := records[2]
	if record.Name != "read_file" ||
		record.CallID != "call-read-3" ||
		record.Success ||
		record.ErrorKind != "repeated_tool_input" ||
		record.ArgumentsSHA256 != toolArgumentsSHA256(call.Arguments) ||
		record.RawOutputBytes != 0 ||
		record.ReturnedOutputBytes != 0 {
		t.Fatalf("unexpected repeated input telemetry: %+v", record)
	}
	if !strings.Contains(strings.Join(record.ResultEnvelope().NextSuggestions, " "), "prior observations") {
		t.Fatalf("repeated input envelope missing recovery guidance: %+v", record.ResultEnvelope())
	}
	block, ok := kit.ToolResultSummaryContextBlock()
	if !ok ||
		!strings.Contains(block.Content, "repeated_arguments:") ||
		!strings.Contains(block.Content, "error_kind=repeated_tool_input") {
		t.Fatalf("tool summary missing repeated input guard evidence:\n%s", block.Content)
	}
}

func TestToolkit_RepeatedToolInputGuardExemptsPollingTools(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	revision := workspaceRevision(context.Background(), kit.env.RootDir)
	hash := toolArgumentsSHA256(`{"run_id":"run-1"}`)
	kit.env.toolTelemetry.record(ToolExecutionRecord{Name: "workflow_status", ArgumentsSHA256: hash, RevisionBefore: revision})
	kit.env.toolTelemetry.record(ToolExecutionRecord{Name: "workflow_status", ArgumentsSHA256: hash, RevisionBefore: revision})

	got := kit.repeatedToolInputCount(providers.ToolCall{
		Name:      "workflow_status",
		Arguments: `{"run_id":"run-1"}`,
	}, revision)
	if got != 0 {
		t.Fatalf("workflow_status should be exempt from repeated input guard, got count %d", got)
	}
}

func TestToolkit_ToolTelemetry_RecordsClassificationReason(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-shell",
		Name:      "run_shell",
		Arguments: `{"command":"pwd"}`,
	}); err != nil {
		t.Fatalf("run_shell: %v", err)
	}

	records := kit.ToolTelemetry()
	if len(records) != 1 {
		t.Fatalf("expected 1 telemetry record, got %d", len(records))
	}
	if records[0].ClassificationReason != "simple read-only shell command" {
		t.Fatalf("classification reason not recorded: %+v", records[0])
	}
}

func TestToolkit_ToolTelemetry_RecordsWorkspaceRevision(t *testing.T) {
	root := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, string(out))
		}
	}
	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test User")
	mustWriteFile(t, filepath.Join(root, "a.txt"), "hello\n")
	runGit("add", "a.txt")
	runGit("commit", "-m", "initial")

	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-read",
		Name:      "read_file",
		Arguments: `{"path":"a.txt"}`,
	}); err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-write",
		Name:      "write_file",
		Arguments: `{"path":"a.txt","content":"hello again\n"}`,
	}); err != nil {
		t.Fatalf("write_file: %v", err)
	}

	records := kit.ToolTelemetry()
	if len(records) != 2 {
		t.Fatalf("expected two records, got %+v", records)
	}
	if records[0].RevisionBefore == "" || records[0].RevisionAfter == "" {
		t.Fatalf("read record missing revisions: %+v", records[0])
	}
	if records[0].RevisionBefore != records[0].RevisionAfter {
		t.Fatalf("read-only record should not change revision: %+v", records[0])
	}
	if records[1].RevisionBefore == "" || records[1].RevisionAfter == "" || records[1].RevisionBefore == records[1].RevisionAfter {
		t.Fatalf("write record should change worktree revision: %+v", records[1])
	}
}

func TestToolkit_ToolTelemetry_RecordsFilesystemWorkspaceRevision(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "hello\n")

	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-read",
		Name:      "read_file",
		Arguments: `{"path":"a.txt"}`,
	}); err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-write",
		Name:      "write_file",
		Arguments: `{"path":"a.txt","content":"hello again\n"}`,
	}); err != nil {
		t.Fatalf("write_file: %v", err)
	}

	records := kit.ToolTelemetry()
	if len(records) != 2 {
		t.Fatalf("expected two records, got %+v", records)
	}
	if !strings.HasPrefix(records[0].RevisionBefore, "fs:worktree:") || records[0].RevisionBefore != records[0].RevisionAfter {
		t.Fatalf("read record should have stable fs revision: %+v", records[0])
	}
	if !strings.HasPrefix(records[1].RevisionBefore, "fs:worktree:") ||
		!strings.HasPrefix(records[1].RevisionAfter, "fs:worktree:") ||
		records[1].RevisionBefore == records[1].RevisionAfter {
		t.Fatalf("write record should change fs revision: %+v", records[1])
	}
}

func TestWorkspaceRevisionIgnoresInternalStateDirs(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "hello\n")

	before := workspaceRevision(context.Background(), root)
	if !strings.HasPrefix(before, "fs:worktree:") {
		t.Fatalf("expected filesystem revision, got %q", before)
	}
	mustWriteFile(t, filepath.Join(root, ".wuu-home", "sessions", "eval", "trace.jsonl"), "{}\n")
	mustWriteFile(t, filepath.Join(root, ".wuu", "sessions", "eval", "trace.jsonl"), "{}\n")
	afterState := workspaceRevision(context.Background(), root)
	if afterState != before {
		t.Fatalf("internal state dirs should not change workspace revision: before=%s after=%s", before, afterState)
	}
	mustWriteFile(t, filepath.Join(root, "a.txt"), "hello again\n")
	afterUserFile := workspaceRevision(context.Background(), root)
	if afterUserFile == before || !strings.HasPrefix(afterUserFile, "fs:worktree:") {
		t.Fatalf("workspace file change should update filesystem revision: before=%s after=%s", before, afterUserFile)
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

func TestToolkit_ToolResultSummaryContextBlockOmitsToolBodies(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "API_KEY=secret-value-1234567890\n")
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-read",
		Name:      "read_file",
		Arguments: `{"path":"a.txt"}`,
	})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(resp, "secret-value") {
		t.Fatalf("fixture should prove read_file body contained secret-like text: %s", resp)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-missing",
		Name:      "read_file",
		Arguments: `{"path":"missing.txt"}`,
	})
	if err == nil {
		t.Fatal("expected missing read_file error")
	}
	kit.env.toolTelemetry.record(ToolExecutionRecord{
		Name:                "run_test",
		ArgumentsSHA256:     strings.Repeat("b", 64),
		ResultAction:        "run",
		Kind:                ToolKindTest,
		Exposure:            ToolExposureDirect,
		Risk:                ToolRiskMedium,
		PolicyAction:        ToolPolicyAllow,
		DurationMS:          123,
		RevisionBefore:      "fs:worktree:before",
		RevisionAfter:       "fs:worktree:after",
		Success:             true,
		RawOutputBytes:      4096,
		ReturnedOutputBytes: 512,
		ResultBudgeted:      true,
		ResultRef:           "/tmp/result-API_KEY=secret-value-1234567890.json",
		ArtifactRefs:        []string{"/tmp/artifact-API_KEY=secret-value-1234567890.log"},
		PatchRiskSummary: &ToolPatchRisk{
			FileCount:    2,
			HunkCount:    2,
			AddedLines:   7,
			DeletedLines: 2,
			MultiFile:    true,
			RiskLevel:    "medium",
		},
	})
	kit.env.toolTelemetry.record(ToolExecutionRecord{
		Name:             "run_test",
		ArgumentsSHA256:  strings.Repeat("b", 64),
		Kind:             ToolKindTest,
		Exposure:         ToolExposureDirect,
		Risk:             ToolRiskMedium,
		PolicyAction:     ToolPolicyAllow,
		DurationMS:       99,
		Success:          true,
		RawOutputBytes:   2048,
		ResultBudgeted:   true,
		PatchRiskSummary: nil,
	})

	block, ok := kit.ToolResultSummaryContextBlock()
	if !ok {
		t.Fatal("expected tool result summary context block")
	}
	if block.Kind != wuucontext.BlockToolResultSummary || block.Source != "tool_telemetry" {
		t.Fatalf("unexpected context block metadata: %+v", block)
	}
	for _, want := range []string{
		"recent_tool_calls:",
		"name=read_file kind=file status=ok",
		"name=read_file kind=file status=error",
		"name=run_test kind=test status=ok risk=medium",
		"args_sha256=" + strings.Repeat("b", 64),
		"result_action=run",
		"evidence_status=current",
		"evidence_status=possibly_stale",
		"raw_output_bytes=4096 returned_output_bytes=512",
		"result_budgeted=true",
		"patch_risk=level=medium,files=2,hunks=2,+7/-2,multi_file=true",
		"repeated_arguments:",
		"name=run_test args_sha256=" + strings.Repeat("b", 64) + " count=2",
		"repeated identical tool inputs can indicate a loop",
		"result_ref=/tmp/result-API_KEY=[REDACTED]",
		"artifact_refs=/tmp/artifact-API_KEY=[REDACTED]",
		"tool arguments and output bodies are intentionally omitted",
	} {
		if !strings.Contains(block.Content, want) {
			t.Fatalf("tool summary missing %q:\n%s", want, block.Content)
		}
	}
	if strings.Contains(block.Content, "secret-value") {
		t.Fatalf("tool result summary should not expose tool output or unredacted refs:\n%s", block.Content)
	}

	blocks := kit.ContextBlocks()
	found := false
	for _, candidate := range blocks {
		if candidate.Kind == wuucontext.BlockToolResultSummary {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ContextBlocks missing TOOL_RESULT_SUMMARY block: %+v", blocks)
	}
}

func TestToolkit_ToolResultSummaryContextBlockShortensArtifactRefs(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	root = kit.env.RootDir
	stateDir := filepath.Join(root, ".wuu-state")
	sessionDir := filepath.Join(stateDir, "sessions", "session-1")
	kit.SetStateDir(stateDir)
	kit.SetSessionDir(sessionDir)

	kit.env.toolTelemetry.record(ToolExecutionRecord{
		Name:                "workflow_status",
		Kind:                ToolKindWorkflow,
		Exposure:            ToolExposureDirect,
		Risk:                ToolRiskLow,
		PolicyAction:        ToolPolicyAllow,
		Success:             true,
		ResultRef:           filepath.Join(sessionDir, "tool-results", "large.json"),
		RawOutputBytes:      1200,
		ReturnedOutputBytes: 400,
		ArtifactRefs: []string{
			filepath.Join(sessionDir, "harness", "reports", "worker.md"),
			filepath.Join(stateDir, "workflows", "run-1", "final-report.md"),
			filepath.Join(root, "local-artifacts", "note.txt"),
		},
	})

	block, ok := kit.ToolResultSummaryContextBlock()
	if !ok {
		t.Fatal("expected tool result summary context block")
	}
	for _, want := range []string{
		"result_ref=$SESSION_DIR/tool-results/large.json",
		"artifact_refs=$SESSION_DIR/harness/reports/worker.md,$STATE_DIR/workflows/run-1/final-report.md,$WORKSPACE/local-artifacts/note.txt",
	} {
		if !strings.Contains(block.Content, want) {
			t.Fatalf("tool summary missing shortened ref %q:\n%s", want, block.Content)
		}
	}
	for _, leaked := range []string{sessionDir, stateDir, root} {
		if strings.Contains(block.Content, leaked) {
			t.Fatalf("tool summary should not include absolute artifact path %q:\n%s", leaked, block.Content)
		}
	}
}

func TestToolkit_ActiveFilesContextBlockTracksReadFiles(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "dir", "a.txt"), "line one\nAPI_KEY=secret-value-1234567890\nline three\nline four\n")
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if block, ok := kit.ActiveFilesContextBlock(); ok {
		t.Fatalf("active files block should be absent before read_file: %+v", block)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"dir/a.txt","offset":2,"limit":2}`,
	})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(resp, "secret-value") {
		t.Fatalf("fixture should prove read_file body contained secret-like text: %s", resp)
	}

	block, ok := kit.ActiveFilesContextBlock()
	if !ok {
		t.Fatal("expected active files context block")
	}
	if block.Kind != wuucontext.BlockActiveFiles || block.Source != "read_file" {
		t.Fatalf("unexpected active files metadata: %+v", block)
	}
	for _, want := range []string{
		"read_files:",
		"path=dir/a.txt",
		"status=current",
		"file_sha=sha256:",
		"read_range=2-3",
		"file bodies are omitted",
	} {
		if !strings.Contains(block.Content, want) {
			t.Fatalf("active files context missing %q:\n%s", want, block.Content)
		}
	}
	if strings.Contains(block.Content, "secret-value") || strings.Contains(block.Content, "API_KEY") {
		t.Fatalf("active files context should omit file bodies:\n%s", block.Content)
	}

	blocks := kit.ContextBlocks()
	found := false
	for _, candidate := range blocks {
		if candidate.Kind == wuucontext.BlockActiveFiles {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ContextBlocks missing ACTIVE_FILES block: %+v", blocks)
	}

	mustWriteFile(t, filepath.Join(root, "dir", "a.txt"), "changed content with different size\n")
	block, ok = kit.ActiveFilesContextBlock()
	if !ok {
		t.Fatal("expected stale active files context block")
	}
	if !strings.Contains(block.Content, "status=possibly_stale") {
		t.Fatalf("active files context should mark changed file stale:\n%s", block.Content)
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
	if err == nil || !strings.Contains(err.Error(), "error_kind=policy_denied") ||
		!strings.Contains(err.Error(), "policy_action=deny") ||
		!strings.Contains(err.Error(), "model_next_action") {
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
	if record.Success || record.Error == "" || record.ErrorKind != "policy_denied" || record.RawOutputBytes != 0 || record.ReturnedOutputBytes != 0 {
		t.Fatalf("denied tool should be recorded as failed without output: %+v", record)
	}
	envelope := record.ResultEnvelope()
	if !strings.Contains(strings.Join(envelope.NextSuggestions, " "), "lower-risk tool") {
		t.Fatalf("denied policy envelope missing recovery guidance: %+v", envelope)
	}
}

func TestToolkit_ToolPolicyContextBlock(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := kit.ToolPolicyContextBlock(); ok {
		t.Fatal("empty policy should not inject a context block")
	}

	policy, ok := PolicyForProfile(ToolPolicyProfileBalanced)
	if !ok {
		t.Fatal("balanced policy missing")
	}
	policy.ToolActions = map[string]ToolPolicyAction{
		"run_shell": ToolPolicyRequireApproval,
	}
	policy.KindActions = map[ToolKind]ToolPolicyAction{
		ToolKindWeb: ToolPolicyDeny,
	}
	kit.SetToolPolicy(policy)

	block, ok := kit.ToolPolicyContextBlock()
	if !ok {
		t.Fatal("expected tool policy context block")
	}
	if block.Kind != wuucontext.BlockToolPolicy || block.Source != "runtime.tool_policy" {
		t.Fatalf("unexpected policy block metadata: %+v", block)
	}
	for _, want := range []string{
		"profile: balanced",
		"default_action: allow",
		"risk_actions:",
		"high: require_approval",
		"kind_actions:",
		"web: deny",
		"tool_actions:",
		"run_shell: require_approval",
		"require_approval means ask the user",
	} {
		if !strings.Contains(block.Content, want) {
			t.Fatalf("policy block missing %q:\n%s", want, block.Content)
		}
	}
	blocks := kit.ContextBlocks()
	if len(blocks) == 0 || blocks[0].Kind != wuucontext.BlockToolPolicy {
		t.Fatalf("ContextBlocks should inject tool policy first: %+v", blocks)
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
	if err == nil || !strings.Contains(err.Error(), "error_kind=policy_denied") {
		t.Fatalf("write-like shell command should be denied by high-risk policy, got %v", err)
	}
}

type recordingAutoModeClassifier struct {
	result   AutoModeClassifyResult
	err      error
	requests []AutoModeClassifyRequest
}

func (c *recordingAutoModeClassifier) Classify(ctx context.Context, request AutoModeClassifyRequest) (AutoModeClassifyResult, error) {
	c.requests = append(c.requests, request)
	return c.result, c.err
}

func TestToolkit_AutoMode_AllowsLowRiskWithoutClassifier(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	policy, ok := PolicyForProfile(ToolPolicyProfileAuto)
	if !ok {
		t.Fatal("auto policy missing")
	}
	classifier := &recordingAutoModeClassifier{
		result: AutoModeClassifyResult{Decision: AutoModeDecisionDeny, Reason: "should not be used for low-risk calls"},
	}
	kit.SetToolPolicy(policy)
	kit.SetAutoModeClassifier(classifier)

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-pwd",
		Name:      "run_shell",
		Arguments: `{"command":"pwd"}`,
	})
	if err != nil {
		t.Fatalf("low-risk shell command should bypass auto classifier: %v", err)
	}
	if !strings.Contains(resp, root) {
		t.Fatalf("unexpected response: %s", resp)
	}
	if len(classifier.requests) != 0 {
		t.Fatalf("low-risk tool should not call auto classifier: %+v", classifier.requests)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || records[0].PolicyAction != ToolPolicyAllow || records[0].AutoModeDecision != "" {
		t.Fatalf("unexpected low-risk auto mode telemetry: %+v", records)
	}
}

func TestToolkit_AutoMode_UsesClassifierForWorkspaceWrite(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	policy, ok := PolicyForProfile(ToolPolicyProfileAuto)
	if !ok {
		t.Fatal("auto policy missing")
	}
	classifier := &recordingAutoModeClassifier{
		result: AutoModeClassifyResult{Decision: AutoModeDecisionAllow, Reason: "workspace edit is scoped"},
	}
	kit.SetToolPolicy(policy)
	kit.SetAutoModeClassifier(classifier)

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-write",
		Name:      "write_file",
		Arguments: `{"path":"notes.txt","content":"hello\n","create_only":true}`,
	})
	if err != nil {
		t.Fatalf("write_file should be auto-approved by classifier: %v", err)
	}
	if !strings.Contains(resp, `"action":"create"`) {
		t.Fatalf("unexpected response: %s", resp)
	}
	if len(classifier.requests) != 1 {
		t.Fatalf("expected one auto classifier request, got %+v", classifier.requests)
	}
	request := classifier.requests[0]
	if request.ToolName != "write_file" || request.Info.Kind != ToolKindFile || request.Info.Risk != ToolRiskHigh {
		t.Fatalf("unexpected auto classifier request: %+v", request)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 {
		t.Fatalf("expected 1 telemetry record, got %d", len(records))
	}
	record := records[0]
	if record.PolicyAction != ToolPolicyAutoClassify || record.AutoModeDecision != AutoModeDecisionAllow || record.AutoModeReason != "workspace edit is scoped" || !record.Success {
		t.Fatalf("unexpected auto-approved telemetry: %+v", record)
	}
}

func TestToolkit_AutoMode_BlocksClassifierDeniedShell(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	policy, ok := PolicyForProfile(ToolPolicyProfileAuto)
	if !ok {
		t.Fatal("auto policy missing")
	}
	classifier := &recordingAutoModeClassifier{
		result: AutoModeClassifyResult{Decision: AutoModeDecisionDeny, Reason: "destructive shell command"},
	}
	kit.SetToolPolicy(policy)
	kit.SetAutoModeClassifier(classifier)

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-rm",
		Name:      "run_shell",
		Arguments: `{"command":"rm -rf build"}`,
	})
	if err == nil ||
		!strings.Contains(err.Error(), "error_kind=auto_mode_denied") ||
		!strings.Contains(err.Error(), "destructive shell command") {
		t.Fatalf("expected auto mode denial, got %v", err)
	}
	if len(classifier.requests) != 1 {
		t.Fatalf("expected one auto classifier request, got %+v", classifier.requests)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 {
		t.Fatalf("expected 1 telemetry record, got %d", len(records))
	}
	record := records[0]
	if record.PolicyAction != ToolPolicyAutoClassify || record.AutoModeDecision != AutoModeDecisionDeny || record.ErrorKind != "auto_mode_denied" || record.RawOutputBytes != 0 || record.Success {
		t.Fatalf("unexpected auto-denied telemetry: %+v", record)
	}
	envelope := record.ResultEnvelope()
	if !strings.Contains(strings.Join(envelope.NextSuggestions, " "), "approval") {
		t.Fatalf("auto-denied envelope missing recovery guidance: %+v", envelope)
	}
}

func TestToolkit_AutoMode_FailsClosedWithoutClassifier(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	policy, ok := PolicyForProfile(ToolPolicyProfileAuto)
	if !ok {
		t.Fatal("auto policy missing")
	}
	kit.SetToolPolicy(policy)
	kit.SetAutoModeClassifier(nil)

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-write",
		Name:      "write_file",
		Arguments: `{"path":"notes.txt","content":"hello\n","create_only":true}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=auto_classifier_unavailable") {
		t.Fatalf("expected auto classifier unavailable error, got %v", err)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || records[0].ErrorKind != "auto_classifier_unavailable" || records[0].AutoModeDecision != AutoModeDecisionDeny {
		t.Fatalf("unexpected fail-closed telemetry: %+v", records)
	}
}

func TestToolkit_ToolPolicy_ApprovalRequiredGuidesModel(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sessionDir := filepath.Join(t.TempDir(), "session")
	kit.SetSessionDir(sessionDir)
	kit.SetToolPolicy(ToolPolicy{
		RiskActions: map[ToolRisk]ToolPolicyAction{
			ToolRiskHigh: ToolPolicyRequireApproval,
		},
	})

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-shell",
		Name:      "run_shell",
		Arguments: `{"command":"printf API_KEY=secret-value-1234567890 > out.txt"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=approval_required") ||
		!strings.Contains(err.Error(), "approval_options") ||
		!strings.Contains(err.Error(), "ask the user for approval") {
		t.Fatalf("expected approval guidance, got %v", err)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 {
		t.Fatalf("expected 1 telemetry record, got %d", len(records))
	}
	record := records[0]
	if record.ErrorKind != "approval_required" {
		t.Fatalf("approval-required record missing error kind: %+v", record)
	}
	if record.ApprovalRef == "" {
		t.Fatalf("approval-required record missing approval ref: %+v", record)
	}
	if !strings.HasPrefix(record.ApprovalRef, filepath.Join(sessionDir, "approvals")) {
		t.Fatalf("approval ref outside session dir: %q", record.ApprovalRef)
	}
	if !containsString(record.ArtifactRefs, record.ApprovalRef) {
		t.Fatalf("approval ref should be included as an artifact ref: %+v", record.ArtifactRefs)
	}
	rawApproval := mustReadFile(t, record.ApprovalRef)
	if strings.Contains(rawApproval, "secret-value") || !strings.Contains(rawApproval, "[REDACTED]") {
		t.Fatalf("approval artifact should redact sensitive arguments:\n%s", rawApproval)
	}
	var request map[string]any
	if err := json.Unmarshal([]byte(rawApproval), &request); err != nil {
		t.Fatalf("decode approval artifact: %v", err)
	}
	if request["tool_name"] != "run_shell" || request["policy_action"] != string(ToolPolicyRequireApproval) || request["model_next_action"] == "" {
		t.Fatalf("approval artifact missing policy details: %+v", request)
	}
	if request["capability"] != "command.bash" || request["capability_action"] != "execute" || request["capability_object"] == "" {
		t.Fatalf("approval artifact missing capability details: %+v", request)
	}
	envelope := record.ResultEnvelope()
	if !strings.Contains(strings.Join(envelope.NextSuggestions, " "), "approval") {
		t.Fatalf("approval policy envelope missing recovery guidance: %+v", envelope)
	}
	if envelope.Data["approval_ref"] != record.ApprovalRef {
		t.Fatalf("approval policy envelope missing approval ref: %+v", envelope)
	}
	block, ok := kit.ToolResultSummaryContextBlock()
	if !ok ||
		!strings.Contains(block.Content, "error_kind=approval_required") ||
		!strings.Contains(block.Content, "approval_ref=$SESSION_DIR/approvals/call-shell.json") {
		t.Fatalf("tool result context missing approval ref:\n%s", block.Content)
	}
}

type recordingToolApprovalReviewer struct {
	reviews  []ToolApprovalReview
	errs     []error
	requests []ToolApprovalReviewRequest
}

func (r *recordingToolApprovalReviewer) ReviewToolApproval(_ context.Context, request ToolApprovalReviewRequest) (ToolApprovalReview, error) {
	r.requests = append(r.requests, request)
	if len(r.errs) > 0 {
		err := r.errs[0]
		r.errs = r.errs[1:]
		return ToolApprovalReview{}, err
	}
	if len(r.reviews) == 0 {
		return ToolApprovalReview{Decision: ToolApprovalDecisionDenied, Reason: "no scripted review"}, nil
	}
	review := r.reviews[0]
	r.reviews = r.reviews[1:]
	return review, nil
}

func TestToolkit_ToolApprovalReviewerApprovesAndExecutes(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sessionDir := filepath.Join(t.TempDir(), "session")
	kit.SetSessionDir(sessionDir)
	kit.SetToolPolicy(ToolPolicy{
		ToolActions: map[string]ToolPolicyAction{
			"write_file": ToolPolicyRequireApproval,
		},
	})
	reviewer := &recordingToolApprovalReviewer{
		reviews: []ToolApprovalReview{{
			Decision:                 ToolApprovalDecisionApproved,
			Reason:                   "user approved",
			Source:                   ApprovalSourceGuardian,
			RiskLevel:                GuardianRiskLow,
			ReviewModel:              "review-model",
			ReviewRole:               "guardian",
			ReviewOutcome:            "completed",
			ReviewRequestFingerprint: strings.Repeat("c", 64),
			ReviewDurationMS:         42,
		}},
	}
	kit.SetToolApprovalReviewer(reviewer)

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-write",
		Name:      "write_file",
		Arguments: `{"path":"notes.txt","content":"hello\n","create_only":true}`,
	})
	if err != nil {
		t.Fatalf("write_file after approval: %v", err)
	}
	if !strings.Contains(resp, `"action":"create"`) {
		t.Fatalf("unexpected response: %s", resp)
	}
	if len(reviewer.requests) != 1 {
		t.Fatalf("expected one approval request, got %+v", reviewer.requests)
	}
	request := reviewer.requests[0]
	if request.ToolName != "write_file" || request.ApprovalRef == "" || request.ApprovalKey == "" {
		t.Fatalf("approval request missing details: %+v", request)
	}
	if string(request.Capability) != "file.edit" || request.CapabilityObject != "notes.txt" || request.CapabilityAction != "edit" {
		t.Fatalf("approval request missing capability details: %+v", request)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 {
		t.Fatalf("expected 1 telemetry record, got %d", len(records))
	}
	record := records[0]
	if !record.Success || record.PolicyAction != ToolPolicyRequireApproval ||
		record.ApprovalDecision != ToolApprovalDecisionApproved ||
		record.ApprovalReason != "user approved" ||
		record.ApprovalSource != ApprovalSourceGuardian ||
		record.ApprovalRiskLevel != GuardianRiskLow ||
		record.ApprovalReviewModel != "review-model" ||
		record.ApprovalReviewRole != "guardian" ||
		record.ApprovalReviewOutcome != "completed" ||
		record.ApprovalReviewRequestFingerprint != strings.Repeat("c", 64) ||
		record.ApprovalReviewDurationMS != 42 ||
		record.ApprovalRef == "" {
		t.Fatalf("unexpected approved telemetry: %+v", record)
	}
}

func TestToolkit_ToolApprovalReviewerDeniesWithoutExecuting(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetToolPolicy(ToolPolicy{
		ToolActions: map[string]ToolPolicyAction{
			"write_file": ToolPolicyRequireApproval,
		},
	})
	kit.SetToolApprovalReviewer(&recordingToolApprovalReviewer{
		reviews: []ToolApprovalReview{{Decision: ToolApprovalDecisionDenied, Reason: "outside requested scope"}},
	})

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-write",
		Name:      "write_file",
		Arguments: `{"path":"notes.txt","content":"hello\n","create_only":true}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=approval_denied") ||
		!strings.Contains(err.Error(), "outside requested scope") {
		t.Fatalf("expected approval denial, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "notes.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("denied write_file should not create file, stat err=%v", statErr)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || records[0].Success || records[0].ApprovalDecision != ToolApprovalDecisionDenied {
		t.Fatalf("unexpected denied telemetry: %+v", records)
	}
}

func TestToolkit_ToolApprovalReviewerCachesApprovedForSession(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetToolPolicy(ToolPolicy{
		ToolActions: map[string]ToolPolicyAction{
			"run_shell": ToolPolicyRequireApproval,
		},
	})
	reviewer := &recordingToolApprovalReviewer{
		reviews: []ToolApprovalReview{{Decision: ToolApprovalDecisionApprovedForSession, Reason: "same command allowed"}},
	}
	kit.SetToolApprovalReviewer(reviewer)
	call := providers.ToolCall{
		ID:        "call-shell",
		Name:      "run_shell",
		Arguments: `{"command":"printf hi > out.txt"}`,
	}

	if _, err := kit.Execute(context.Background(), call); err != nil {
		t.Fatalf("first approved shell: %v", err)
	}
	call.ID = "call-shell-2"
	if _, err := kit.Execute(context.Background(), call); err != nil {
		t.Fatalf("second cached shell: %v", err)
	}
	if len(reviewer.requests) != 1 {
		t.Fatalf("expected one approval request due session cache, got %+v", reviewer.requests)
	}
	records := kit.ToolTelemetry()
	if len(records) != 2 {
		t.Fatalf("expected 2 telemetry records, got %d", len(records))
	}
	if records[0].ApprovalDecision != ToolApprovalDecisionApprovedForSession ||
		records[1].ApprovalDecision != ToolApprovalDecisionApprovedForSession {
		t.Fatalf("approval cache telemetry missing approved_for_session: %+v", records)
	}
}

func TestToolkit_BashDefinition_RequiresNonInteractiveCommands(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defs := kit.Definitions()
	for _, d := range defs {
		if d.Name != "bash" {
			continue
		}
		if !strings.Contains(strings.ToLower(d.Description), "non-interactive") {
			t.Fatalf("bash description must mention non-interactive use: %q", d.Description)
		}
		for _, profileSpecificEditTool := range []string{"apply_patch", "edit_file", "write_file"} {
			if strings.Contains(d.Description, profileSpecificEditTool) {
				t.Fatalf("bash description must not mention profile-specific edit tool %s: %q", profileSpecificEditTool, d.Description)
			}
		}
		props, ok := d.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("bash schema properties missing or wrong type: %#v", d.InputSchema["properties"])
		}
		commandProp, ok := props["command"].(map[string]any)
		if !ok {
			t.Fatalf("bash command schema missing or wrong type: %#v", props["command"])
		}
		desc, _ := commandProp["description"].(string)
		if !strings.Contains(strings.ToLower(desc), "non-interactive") {
			t.Fatalf("bash command description must mention non-interactive use: %q", desc)
		}
		return
	}
	t.Fatal("bash must be present in tool definitions")
}

func TestShellNextSuggestions_TimedOutLongRunningCommandUsesBashBackground(t *testing.T) {
	suggestions := strings.Join(shellNextSuggestions(0, true, ToolClassification{}), " ")
	if !strings.Contains(suggestions, "bash action=start_background") || !strings.Contains(suggestions, "bash action=read_background") {
		t.Fatalf("timed-out shell guidance should mention bash background actions: %q", suggestions)
	}
	if !strings.Contains(suggestions, "dev server") || !strings.Contains(suggestions, "long-lived") {
		t.Fatalf("timed-out shell guidance should identify long-lived commands: %q", suggestions)
	}
	for _, legacy := range []string{"start_process", "run_shell", "run_test"} {
		if strings.Contains(suggestions, legacy) {
			t.Fatalf("timed-out shell guidance must not mention legacy command tool %s: %q", legacy, suggestions)
		}
	}
}

func TestShellNextSuggestions_FailureStaysBashFirst(t *testing.T) {
	suggestions := strings.Join(shellNextSuggestions(1, false, ToolClassification{}), " ")
	if !strings.Contains(suggestions, "bash action=run") {
		t.Fatalf("failed shell guidance should stay on bash action=run: %q", suggestions)
	}
	for _, legacy := range []string{"start_process", "run_shell", "run_test"} {
		if strings.Contains(suggestions, legacy) {
			t.Fatalf("failed shell guidance must not mention legacy command tool %s: %q", legacy, suggestions)
		}
	}
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

func TestToolkit_StartProcessDefaultsOwnerAndReturnsInitialOutput(t *testing.T) {
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
	kit.SetSessionID("thread-start-process")
	kit.SetAgentIdentity("agent-start-process", agentthread.RootPath)

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "start_process",
		Arguments: `{"command":"sleep 0.2; printf 'READY_FROM_START\n'; sleep 1","wait_ms":2000,"max_bytes":4096}`,
	})
	if err != nil {
		t.Fatalf("start_process: %v", err)
	}
	var parsed struct {
		proc.Process
		InitialOutput    string   `json:"initial_output"`
		InitialEndOffset int64    `json:"initial_end_offset"`
		InitialTimedOut  bool     `json:"initial_timed_out"`
		NextSuggestions  []string `json:"next_suggestions"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	defer manager.Stop(parsed.ID)
	if parsed.OwnerKind != proc.OwnerMainAgent || parsed.OwnerID != "agent-start-process" {
		t.Fatalf("owner defaults not applied: %+v", parsed.Process)
	}
	if parsed.InitialTimedOut {
		t.Fatalf("initial output should arrive before timeout: %+v", parsed)
	}
	if !strings.Contains(parsed.InitialOutput, "READY_FROM_START") {
		t.Fatalf("missing initial output: %s", resp)
	}
	if parsed.InitialEndOffset <= 0 {
		t.Fatalf("missing initial end offset: %+v", parsed)
	}
	if len(parsed.NextSuggestions) == 0 || !strings.Contains(strings.Join(parsed.NextSuggestions, " "), "read_process_output") {
		t.Fatalf("missing follow-up guidance: %+v", parsed.NextSuggestions)
	}
}

func TestToolkit_StartProcessDefinitionIsHiddenFromModelSurfaces(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Phase 5: start_process is advanced / hidden. The model
	// never sees it; the registry still holds it for internal
	// callers (tool_search, replay, the bash background-mode
	// backend).
	for _, def := range kit.Definitions() {
		if def.Name == "start_process" {
			t.Fatalf("start_process must NOT be present in tool definitions (Phase 5: advanced/hidden), got %v", def.Name)
		}
	}
	if kit.LookupTool("start_process") == nil {
		t.Fatal("start_process must remain in the registry for internal callers")
	}
	_ = root
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

func TestToolkit_ProcessAndPortTelemetryRecordsResultActions(t *testing.T) {
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

	startResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "start_process",
		Arguments: `{"command":"sleep 5","owner_kind":"main_agent","lifecycle":"session"}`,
	})
	if err != nil {
		t.Fatalf("start_process: %v", err)
	}
	var started proc.Process
	if err := json.Unmarshal([]byte(startResp), &started); err != nil {
		t.Fatalf("parse start_process response: %v", err)
	}
	defer manager.Stop(started.ID)
	if started.Action != "start_process" || started.ID == "" {
		t.Fatalf("unexpected start_process response: %+v", started)
	}

	listResp, err := kit.Execute(context.Background(), providers.ToolCall{Name: "list_processes", Arguments: `{}`})
	if err != nil {
		t.Fatalf("list_processes: %v", err)
	}
	var listed struct {
		Action    string         `json:"action"`
		Processes []proc.Process `json:"processes"`
	}
	if err := json.Unmarshal([]byte(listResp), &listed); err != nil {
		t.Fatalf("parse list_processes response: %v", err)
	}
	if listed.Action != "list_processes" || len(listed.Processes) != 1 {
		t.Fatalf("unexpected list_processes response: %+v", listed)
	}

	readResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_process_output",
		Arguments: `{"process_id":"` + started.ID + `","offset_bytes":0,"wait_ms":1}`,
	})
	if err != nil {
		t.Fatalf("read_process_output: %v", err)
	}
	var readParsed map[string]any
	if err := json.Unmarshal([]byte(readResp), &readParsed); err != nil {
		t.Fatalf("parse read_process_output response: %v", err)
	}
	if readParsed["action"] != "read_process_output" {
		t.Fatalf("read_process_output action mismatch: %+v", readParsed)
	}

	stopResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "stop_process",
		Arguments: `{"process_id":"` + started.ID + `"}`,
	})
	if err != nil {
		t.Fatalf("stop_process: %v", err)
	}
	var stopped proc.Process
	if err := json.Unmarshal([]byte(stopResp), &stopped); err != nil {
		t.Fatalf("parse stop_process response: %v", err)
	}
	if stopped.Action != "stop_process" {
		t.Fatalf("stop_process action mismatch: %+v", stopped)
	}

	portResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "report_listening_ports",
		Arguments: `{"ports":[3000,8080]}`,
	})
	if err != nil {
		t.Fatalf("report_listening_ports: %v", err)
	}
	var portParsed map[string]any
	if err := json.Unmarshal([]byte(portResp), &portParsed); err != nil {
		t.Fatalf("parse report_listening_ports response: %v", err)
	}
	if portParsed["action"] != "report_listening_ports" {
		t.Fatalf("report_listening_ports action mismatch: %+v", portParsed)
	}

	records := kit.ToolTelemetry()
	gotActions := make([]string, 0, len(records))
	for _, record := range records {
		gotActions = append(gotActions, record.Name+":"+record.ResultAction)
	}
	wantActions := []string{
		"start_process:start_process",
		"list_processes:list_processes",
		"read_process_output:read_process_output",
		"stop_process:stop_process",
		"report_listening_ports:report_listening_ports",
	}
	if !reflect.DeepEqual(gotActions, wantActions) {
		t.Fatalf("process telemetry actions = %+v, want %+v", gotActions, wantActions)
	}
}

func TestToolkit_StartProcessRejectsSecretBearingCommands(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "start_process",
		Arguments: `{"command":"env | grep TOKEN","owner_kind":"main_agent"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "environment variables") {
		t.Fatalf("expected environment dump rejection, got %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "start_process",
		Arguments: `{"command":"cat .env","owner_kind":"main_agent"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "sensitive paths") {
		t.Fatalf("expected sensitive path rejection, got %v", err)
	}
}

func TestToolkit_StartProcessRejectsGitCommandBypass(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, command := range []string{
		"git status",
		"cd pkg && git status",
		"env FOO=bar git status",
	} {
		_, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "start_process",
			Arguments: fmt.Sprintf(`{"command":%q,"owner_kind":"main_agent"}`, command),
		})
		if err == nil || !strings.Contains(err.Error(), "Use bash action=run for normal git") {
			t.Fatalf("expected short-lived git guidance for %q, got %v", command, err)
		}
	}
}

func TestToolkit_StartProcessRejectsDestructiveCommands(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "tmp", "keep.txt"), "keep\n")

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "start_process",
		Arguments: `{"command":"command rm -rf tmp","owner_kind":"main_agent"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "destructive shell commands") {
		t.Fatalf("expected destructive command rejection, got %v", err)
	}
	if got := mustReadFile(t, filepath.Join(root, "tmp", "keep.txt")); got != "keep\n" {
		t.Fatalf("destructive start_process should not mutate workspace: %q", got)
	}
}

func TestToolkit_StartProcessRejectsPackageNetworkMutationCommands(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, command := range []string{
		"npm install left-pad",
		"printf ok && curl https://example.com",
		"env FOO=bar npx some-tool",
	} {
		_, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "start_process",
			Arguments: fmt.Sprintf(`{"command":%q,"owner_kind":"main_agent"}`, command),
		})
		if err == nil || !strings.Contains(err.Error(), "package, network, or external mutation commands") {
			t.Fatalf("expected package/network mutation rejection for %q, got %v", command, err)
		}
	}
}

func TestToolkit_ProcessOutputRedactsSecrets(t *testing.T) {
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
		Arguments: `{"command":"printf 'API_KEY=process-secret-value-1234567890\n'; sleep 0.1","owner_kind":"main_agent","lifecycle":"session"}`,
	})
	if err != nil {
		t.Fatalf("start_process: %v", err)
	}
	if strings.Contains(resp, "process-secret-value") || !strings.Contains(resp, "[REDACTED]") {
		t.Fatalf("start_process response should redact command metadata: %s", resp)
	}
	var started proc.Process
	if err := json.Unmarshal([]byte(resp), &started); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	defer manager.Stop(started.ID)

	outResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_process_output",
		Arguments: `{"process_id":"` + started.ID + `","offset_bytes":0,"wait_ms":2000}`,
	})
	if err != nil {
		t.Fatalf("read_process_output: %v", err)
	}
	if strings.Contains(outResp, "process-secret-value") || !strings.Contains(outResp, "[REDACTED]") {
		t.Fatalf("read_process_output should redact process output and metadata: %s", outResp)
	}
}

func TestToolkit_SpawnAgentDefinitionUsesCCAgentSchema(t *testing.T) {
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
		for _, field := range []string{"description", "prompt", "subagent_type", "name", "run_in_background", "isolation", "goal_id", "goal_dir"} {
			if _, ok := props[field]; !ok {
				t.Fatalf("spawn_agent schema must expose %s: %#v", field, d.InputSchema)
			}
		}
		for _, old := range []string{"task_name", "message", "agent_type", "synchronous", "fork_turns", "base_repo"} {
			if _, ok := props[old]; ok {
				t.Fatalf("spawn_agent schema should not expose old field %s: %#v", old, d.InputSchema)
			}
		}
		required, _ := d.InputSchema["required"].([]string)
		if !reflect.DeepEqual(required, []string{"description", "prompt"}) {
			t.Fatalf("spawn_agent required = %#v, want description/prompt", d.InputSchema["required"])
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
		for _, want := range []string{"non-overlapping", "Do not sleep, poll", "run_in_background=true"} {
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
			"Base Agent Brief Contract",
			"destructive or broad experiments",
			"overlapping or uncertain concurrent writes",
			"generated outputs/formatters",
			"subagent_type",
			"fork yourself",
			"general-purpose",
			"verification",
			"Ordinary child agents are temporary",
			"saved profile memory",
			"agent_profile",
		} {
			if !strings.Contains(d.Description, want) {
				t.Fatalf("spawn_agent description missing decision guidance %q: %q", want, d.Description)
			}
		}
		props, _ := d.InputSchema["properties"].(map[string]any)
		for field, wants := range map[string][]string{
			"subagent_type": {"general-purpose", "verification", "fork yourself"},
			"prompt":        {"Concrete task brief", "Base Agent Brief Contract", "owned files/modules", "Fresh subagents"},
			"agent_profile": {"Agent Profile name with saved memory", "workflow/profile policy", "ordinary temporary child tasks"},
			"isolation":     {"worktree", "current repo"},
		} {
			prop, _ := props[field].(map[string]any)
			desc, _ := prop["description"].(string)
			for _, want := range wants {
				if !strings.Contains(desc, want) {
					t.Fatalf("spawn_agent %s description missing %q: %q", field, want, desc)
				}
			}
		}
		if strings.Contains(d.Description, "memoryless") || strings.Contains(d.Description, "memory-bearing") || strings.Contains(d.Description, "long-lived identity") {
			t.Fatalf("spawn_agent description should avoid awkward memory wording: %q", d.Description)
		}
		return
	}
	t.Fatal("spawn_agent must be present in tool definitions")
}

func TestToolkit_WaitAgentUsesNotificationSignalSchema(t *testing.T) {
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
			t.Fatalf("wait_agent signal schema must not expose target: %#v", d.InputSchema)
		}
		if _, ok := d.InputSchema["required"]; ok {
			t.Fatalf("wait_agent signal schema must not require fields: %#v", d.InputSchema)
		}
		return
	}
	t.Fatal("wait_agent must be present in tool definitions")
}

func TestToolkit_WaitAgentReturnsNarrowTimeoutSignal(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	control, err := agentcontrol.New(agentcontrol.Config{
		Client:       &workflowFakeClient{content: "unused"},
		DefaultModel: "fake-model",
		ParentRepo:   root,
		WorktreeRoot: filepath.Join(root, ".wuu", "worktrees"),
		SessionID:    "wait-signal-session",
		WorkerFactory: func(string, agentcontrol.WorkerType, agentthread.Metadata) (agent.ToolExecutor, error) {
			return workflowNoopExecutor{}, nil
		},
	})
	if err != nil {
		t.Fatalf("AgentControl New: %v", err)
	}
	defer stopWorkflowAgentControl(control)
	kit.SetAgentControl(control)
	kit.SetAgentIdentity("root", agentthread.RootPath)

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "wait_agent",
		Arguments: `{"timeout_ms":1}`,
	})
	if err != nil {
		t.Fatalf("wait_agent: %v", err)
	}
	var parsed struct {
		Action     string `json:"action"`
		TimedOut   bool   `json:"timed_out"`
		SignalType string `json:"signal_type"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("decode wait_agent result: %v\n%s", err, resp)
	}
	if parsed.Action != "wait_agent" || !parsed.TimedOut || parsed.SignalType != agentcontrol.WaitAgentSignalTimeout {
		t.Fatalf("unexpected wait_agent result: %+v", parsed)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(resp), &raw); err != nil {
		t.Fatalf("decode wait_agent result map: %v\n%s", err, resp)
	}
	for _, key := range []string{"next_steps", "await_target", "details_available_via", "requires_await_agents_for_output"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("wait_agent result should stay narrow, found %s in %s", key, resp)
		}
	}
}

func TestWaitAgentResultFromCompletedSignalIsNarrow(t *testing.T) {
	result := waitAgentResultFromSignal(agentcontrol.WaitAgentSignal{
		Received:   true,
		SignalType: agentcontrol.WaitAgentSignalCompleted,
		AgentID:    "agent-1",
		AgentPath:  "/root/review",
		TaskName:   "review",
		Status:     "completed",
	})
	if result.TimedOut || result.SignalType != agentcontrol.WaitAgentSignalCompleted {
		t.Fatalf("unexpected completed wait result: %+v", result)
	}
	if result.AgentPath != "/root/review" || result.AgentID != "agent-1" || result.Status != "completed" {
		t.Fatalf("completed signal should keep only identity/status fields: %+v", result)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal completed wait result: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode completed wait result: %v\n%s", err, raw)
	}
	for _, key := range []string{"next_steps", "await_target", "details_available_via", "requires_await_agents_for_output"} {
		if _, ok := decoded[key]; ok {
			t.Fatalf("completed wait result should stay narrow, found %s in %s", key, raw)
		}
	}
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
		Arguments: `{"description":"Do thing","prompt":"Do thing.","subagent_type":"general-purpose"}`,
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
	if !strings.Contains(prompt, "cannot spawn or manage other agents") {
		t.Fatalf("fork override must match worker tool filtering: %q", prompt)
	}
	if strings.Contains(prompt, "You may use spawn_agent") {
		t.Fatalf("fork override must not promise recursive delegation: %q", prompt)
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

func TestDeriveAgentTaskName(t *testing.T) {
	got := deriveAgentTaskName("Audit payment API!")
	if !strings.HasPrefix(got, "audit_payment_api_") {
		t.Fatalf("derived name = %q, want audit_payment_api_*", got)
	}
	if err := agentthread.ValidateAgentName(got); err != nil {
		t.Fatalf("derived name should be a valid agent path segment: %v", err)
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
		Arguments: `{"command":"echo hi","purpose":"confirm shell purpose metadata"}`,
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
	if parsed["action"].(string) != "run" {
		t.Fatalf("unexpected run_shell action: %+v", parsed)
	}
	if !strings.Contains(parsed["output"].(string), "hi") {
		t.Fatalf("unexpected output: %v", parsed["output"])
	}
	if parsed["purpose"].(string) != "confirm shell purpose metadata" {
		t.Fatalf("unexpected purpose: %+v", parsed)
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
	if revision, _ := parsed["workspace_revision"].(string); !strings.HasPrefix(revision, "fs:worktree:") {
		t.Fatalf("run_shell response missing filesystem workspace revision: %+v", parsed)
	}
	if suggestions, ok := parsed["next_suggestions"].([]any); !ok || len(suggestions) == 0 {
		t.Fatalf("run_shell response missing next_suggestions: %+v", parsed)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || records[0].ResultAction != "run" {
		t.Fatalf("run_shell telemetry missing result action: %+v", records)
	}
}

func TestToolkit_RunShellRedactsSensitiveOutput(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sessionDir := filepath.Join(t.TempDir(), "session")
	kit.SetSessionDir(sessionDir)

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"printf 'API_KEY=secret-value-1234567890\nAuthorization: Bearer abcdefghijklmnop\nsk-testsecret123456\n'","purpose":"diagnose TOKEN=purpose-secret-value-1234567890"}`,
	})
	if err != nil {
		t.Fatalf("run_shell: %v", err)
	}

	for _, leaked := range []string{"secret-value", "abcdefghijklmnop", "sk-testsecret", "purpose-secret"} {
		if strings.Contains(resp, leaked) {
			t.Fatalf("run_shell response leaked %q: %s", leaked, resp)
		}
	}
	if strings.Count(resp, "[REDACTED]") < 3 {
		t.Fatalf("expected redaction markers, got: %s", resp)
	}
	var parsed struct {
		FullLogRef        string `json:"full_log_ref"`
		FullLogBytes      int    `json:"full_log_bytes"`
		WorkspaceRevision string `json:"workspace_revision"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse run_shell response: %v\n%s", err, resp)
	}
	if !strings.HasPrefix(parsed.WorkspaceRevision, "fs:worktree:") {
		t.Fatalf("run_shell response missing filesystem workspace revision: %+v", parsed)
	}
	if parsed.FullLogRef == "" || parsed.FullLogBytes <= 0 {
		t.Fatalf("run_shell response missing full log artifact: %+v", parsed)
	}
	if !strings.HasPrefix(parsed.FullLogRef, filepath.Join(sessionDir, "tool-results", "shell-logs")) {
		t.Fatalf("full log ref outside session dir: %q", parsed.FullLogRef)
	}
	logData, err := os.ReadFile(parsed.FullLogRef)
	if err != nil {
		t.Fatalf("read full log artifact: %v", err)
	}
	logText := string(logData)
	for _, leaked := range []string{"secret-value", "abcdefghijklmnop", "sk-testsecret", "purpose-secret"} {
		if strings.Contains(logText, leaked) {
			t.Fatalf("run_shell full log leaked %q:\n%s", leaked, logText)
		}
	}
	if !strings.Contains(logText, "purpose: diagnose TOKEN=[REDACTED]") {
		t.Fatalf("full log artifact missing redacted purpose:\n%s", logText)
	}
	if strings.Count(logText, "[REDACTED]") < 3 || !strings.Contains(logText, "exit_code: 0") {
		t.Fatalf("full log artifact missing redacted evidence:\n%s", logText)
	}
	if !strings.Contains(logText, "workspace_revision: "+parsed.WorkspaceRevision) {
		t.Fatalf("full log artifact missing workspace revision:\n%s", logText)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || !containsString(records[0].ArtifactRefs, parsed.FullLogRef) {
		t.Fatalf("tool telemetry missing shell log artifact ref: records=%+v full_log_ref=%q", records, parsed.FullLogRef)
	}
}

func TestToolkit_RunShellRejectsSensitivePathAccess(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, ".env"), "API_KEY=secret\n")

	for _, command := range []string{"cat .env", "cat .env && echo done"} {
		_, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "run_shell",
			Arguments: `{"command":"` + command + `"}`,
		})
		if err == nil {
			t.Fatalf("expected sensitive path rejection for %q", command)
		}
		if !strings.Contains(err.Error(), "sensitive paths") || !strings.Contains(err.Error(), "explicit secret handling") {
			t.Fatalf("expected sensitive path guidance for %q, got: %v", command, err)
		}
		if strings.Contains(err.Error(), "API_KEY") || strings.Contains(err.Error(), "API_KEY=secret") {
			t.Fatalf("run_shell sensitive path error leaked file content for %q: %v", command, err)
		}
	}
}

func TestToolkit_RunShellRejectsEnvironmentDump(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, command := range []string{"env", "printenv PATH", "env | grep TOKEN"} {
		_, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "run_shell",
			Arguments: `{"command":"` + command + `"}`,
		})
		if err == nil {
			t.Fatalf("expected environment dump rejection for %q", command)
		}
		if !strings.Contains(err.Error(), "environment variables") || !strings.Contains(err.Error(), "secrets") {
			t.Fatalf("expected environment dump guidance for %q, got: %v", command, err)
		}
	}
}

func TestToolkit_RunShellRejectsDestructiveCommands(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "tmp", "keep.txt"), "keep\n")

	for _, command := range []string{"rm -rf tmp", "printf ok && command rm -rf tmp", "env FOO=bar rm -rf tmp"} {
		_, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "run_shell",
			Arguments: fmt.Sprintf(`{"command":%q}`, command),
		})
		if err == nil || !strings.Contains(err.Error(), "destructive shell commands") {
			t.Fatalf("expected destructive command rejection for %q, got %v", command, err)
		}
		if got := mustReadFile(t, filepath.Join(root, "tmp", "keep.txt")); got != "keep\n" {
			t.Fatalf("destructive run_shell should not mutate workspace after %q: %q", command, got)
		}
	}
}

func TestToolkit_BashRejectsDestructiveCommandsWithProfileNeutralGuidance(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "tmp", "keep.txt"), "keep\n")

	for _, args := range []string{
		`{"command":"rm -rf tmp"}`,
		`{"action":"start_background","command":"rm -rf tmp"}`,
	} {
		_, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "bash",
			Arguments: args,
		})
		if err == nil || !strings.Contains(err.Error(), "destructive shell commands") {
			t.Fatalf("expected destructive bash rejection for %s, got %v", args, err)
		}
		for _, toolName := range []string{"apply_patch", "edit_file", "write_file"} {
			if strings.Contains(err.Error(), toolName) {
				t.Fatalf("bash destructive guidance must be profile-neutral, got %v", err)
			}
		}
		if !strings.Contains(err.Error(), "file editing tool exposed in this session") {
			t.Fatalf("bash destructive guidance should point to the active edit capability, got %v", err)
		}
	}

	if got := mustReadFile(t, filepath.Join(root, "tmp", "keep.txt")); got != "keep\n" {
		t.Fatalf("destructive bash should not mutate workspace: %q", got)
	}
}

func TestToolkit_RunShellRejectsPackageNetworkMutationCommands(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, command := range []string{
		"npm install left-pad",
		"uv sync",
		"curl https://example.com",
		"printf ok && wget https://example.com",
		"env FOO=bar npx some-tool",
	} {
		_, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "run_shell",
			Arguments: fmt.Sprintf(`{"command":%q}`, command),
		})
		if err == nil || !strings.Contains(err.Error(), "package, network, or external mutation commands") {
			t.Fatalf("expected package/network mutation rejection for %q, got %v", command, err)
		}
	}
}

func TestToolkit_RunShellGuidesNpxVerificationToBash(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"npx vitest --run"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=wrong_tool_for_verification") || !strings.Contains(err.Error(), "retry with bash action=run") {
		t.Fatalf("expected run_shell to guide npx verification to bash action=run, got %v", err)
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

func TestToolkit_RunShellAllowsSafeGitCommands(t *testing.T) {
	kit, root := setupGitRepo(t)

	for _, command := range []string{
		"git status --short",
		"command git status --short",
		"env FOO=bar git status --short",
		"cd . && git status --short",
	} {
		resp, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "run_shell",
			Arguments: fmt.Sprintf(`{"command":%q,"timeout_seconds":10}`, command),
		})
		if err != nil {
			t.Fatalf("expected safe git shell command %q to run: %v", command, err)
		}
		var parsed struct {
			ExitCode       int                `json:"exit_code"`
			Classification ToolClassification `json:"classification"`
		}
		if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
			t.Fatalf("parse run_shell response for %q: %v\n%s", command, err, resp)
		}
		if parsed.ExitCode != 0 || !parsed.Classification.ReadOnly || parsed.Classification.Risk != ToolRiskLow {
			t.Fatalf("safe git shell response for %q = %+v, want successful low-risk read-only", command, parsed)
		}
	}

	mustWriteFile(t, filepath.Join(root, "hello.txt"), "updated\n")
	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"git add hello.txt && git commit -m \"update hello\"","timeout_seconds":10}`,
	})
	if err != nil {
		t.Fatalf("expected explicit git add + commit shell command to run: %v", err)
	}
	var parsed struct {
		ExitCode       int                `json:"exit_code"`
		Classification ToolClassification `json:"classification"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse add+commit response: %v\n%s", err, resp)
	}
	if parsed.ExitCode != 0 || parsed.Classification.ReadOnly || parsed.Classification.Risk != ToolRiskMedium {
		t.Fatalf("git add+commit shell response = %+v, want successful medium-risk write", parsed)
	}
}

func TestToolkit_RunShellRejectsUnsafeGitCommands(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, command := range []string{
		"git add .",
		"git reset --hard",
		"git config user.name tester",
		"git commit --no-verify -m test",
		"cd .. && git status",
		"printf hi && `git status`",
	} {
		_, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "run_shell",
			Arguments: fmt.Sprintf(`{"command":%q,"timeout_seconds":10}`, command),
		})
		if err == nil || !strings.Contains(err.Error(), "unsupported_git_shell") {
			t.Fatalf("expected unsafe git shell rejection for %q, got %v", command, err)
		}
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
	want := []grepMatch{
		{File: ".env", Line: 1, Content: "[REDACTED: sensitive file content]"},
		{File: "visible.env", Line: 1, Content: "[REDACTED: sensitive file content]"},
	}
	if !reflect.DeepEqual(parsed.Matches, want) {
		t.Fatalf("unexpected hidden grep matches: got %+v want %+v", parsed.Matches, want)
	}
	if strings.Contains(resp, "API_KEY=secret") || strings.Contains(resp, "API_KEY=visible") {
		t.Fatalf("grep response leaked sensitive file content: %s", resp)
	}
}

func TestToolkit_SearchResultsIncludeNextSuggestions(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc target() {}\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	grepResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "grep",
		Arguments: `{"pattern":"target"}`,
	})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	var grepParsed struct {
		Action            string      `json:"action"`
		Matches           []grepMatch `json:"matches"`
		WorkspaceRevision string      `json:"workspace_revision"`
		Suggestions       []string    `json:"next_suggestions"`
	}
	if err := json.Unmarshal([]byte(grepResp), &grepParsed); err != nil {
		t.Fatalf("parse grep response: %v", err)
	}
	if len(grepParsed.Matches) != 1 {
		t.Fatalf("unexpected grep matches: %+v", grepParsed.Matches)
	}
	if grepParsed.Action != "grep" {
		t.Fatalf("grep action = %q, want grep", grepParsed.Action)
	}
	if !strings.HasPrefix(grepParsed.WorkspaceRevision, "fs:worktree:") {
		t.Fatalf("grep response missing filesystem workspace revision: %+v", grepParsed)
	}
	if len(grepParsed.Suggestions) == 0 || !strings.Contains(strings.Join(grepParsed.Suggestions, " "), "read_file") {
		t.Fatalf("grep response missing read_file suggestion: %+v", grepParsed.Suggestions)
	}

	globResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "glob",
		Arguments: `{"pattern":"*.missing"}`,
	})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var globParsed struct {
		Action            string   `json:"action"`
		Files             []string `json:"files"`
		WorkspaceRevision string   `json:"workspace_revision"`
		Suggestions       []string `json:"next_suggestions"`
	}
	if err := json.Unmarshal([]byte(globResp), &globParsed); err != nil {
		t.Fatalf("parse glob response: %v", err)
	}
	if len(globParsed.Files) != 0 {
		t.Fatalf("unexpected glob matches: %+v", globParsed.Files)
	}
	if globParsed.Action != "glob" {
		t.Fatalf("glob action = %q, want glob", globParsed.Action)
	}
	if !strings.HasPrefix(globParsed.WorkspaceRevision, "fs:worktree:") {
		t.Fatalf("glob response missing filesystem workspace revision: %+v", globParsed)
	}
	if len(globParsed.Suggestions) == 0 || !strings.Contains(strings.Join(globParsed.Suggestions, " "), "broader glob") {
		t.Fatalf("empty glob response missing broaden suggestion: %+v", globParsed.Suggestions)
	}
	for _, mode := range []string{"files_with_matches", "count"} {
		resp, err := kit.Execute(context.Background(), providers.ToolCall{
			Name:      "grep",
			Arguments: `{"pattern":"target","output_mode":"` + mode + `"}`,
		})
		if err != nil {
			t.Fatalf("grep %s: %v", mode, err)
		}
		var parsed struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
			t.Fatalf("parse grep %s response: %v", mode, err)
		}
		if parsed.Action != "grep" {
			t.Fatalf("grep %s action = %q, want grep", mode, parsed.Action)
		}
	}
	records := kit.ToolTelemetry()
	gotActions := make([]string, 0, len(records))
	for _, record := range records {
		gotActions = append(gotActions, record.Name+":"+record.ResultAction)
	}
	wantActions := []string{"grep:grep", "glob:glob", "grep:grep", "grep:grep"}
	if !reflect.DeepEqual(gotActions, wantActions) {
		t.Fatalf("search telemetry missing result actions: %+v", records)
	}
}

func TestToolkit_ASTSearchFindsDefinitionsImportsAndCalls(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	files := map[string]string{
		"main.go": strings.Join([]string{
			"package demo",
			"",
			`import "fmt"`,
			"",
			"func Target() string {",
			`	return fmt.Sprintf("%s", "target")`,
			"}",
			"",
			"func caller() string {",
			"	return Target()",
			"}",
			"",
		}, "\n"),
		"web/app.ts": strings.Join([]string{
			`import { createRoot } from "react-dom/client";`,
			"",
			"export function renderApp() {",
			"  return createRoot(document.body);",
			"}",
			"",
		}, "\n"),
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

	defResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "ast_search",
		Arguments: `{"query":"Target","kind":"function","include":"*.go"}`,
	})
	if err != nil {
		t.Fatalf("ast_search definition: %v", err)
	}
	var defParsed struct {
		Action  string `json:"action"`
		Query   string `json:"query"`
		Kind    string `json:"kind"`
		Matches []struct {
			File               string         `json:"file"`
			Line               int            `json:"line"`
			EndLine            int            `json:"end_line"`
			Kind               string         `json:"kind"`
			Name               string         `json:"name"`
			ReadFileRange      map[string]int `json:"read_file_range"`
			ReadFileSuggestion string         `json:"read_file_suggestion"`
		} `json:"matches"`
		Suggestions []string `json:"next_suggestions"`
	}
	if err := json.Unmarshal([]byte(defResp), &defParsed); err != nil {
		t.Fatalf("parse definition response: %v", err)
	}
	if defParsed.Action != "ast_search" {
		t.Fatalf("ast_search action = %q, want ast_search", defParsed.Action)
	}
	if defParsed.Query != "Target" || defParsed.Kind != "function" || len(defParsed.Matches) != 1 {
		t.Fatalf("unexpected definition response: %+v", defParsed)
	}
	def := defParsed.Matches[0]
	if def.File != "main.go" || def.Line != 5 || def.EndLine != 7 || def.Kind != "function" || def.Name != "Target" {
		t.Fatalf("unexpected definition match: %+v", def)
	}
	if def.ReadFileRange["start_line"] != 5 || def.ReadFileRange["end_line"] != 7 || !strings.Contains(def.ReadFileSuggestion, "read_file") {
		t.Fatalf("definition match missing read_file range: %+v", def)
	}
	if len(defParsed.Suggestions) == 0 || !strings.Contains(strings.Join(defParsed.Suggestions, " "), "read_file") {
		t.Fatalf("definition response missing read_file suggestion: %+v", defParsed.Suggestions)
	}

	importResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "ast_search",
		Arguments: `{"query":"react-dom","kind":"import","include":"**/*.ts"}`,
	})
	if err != nil {
		t.Fatalf("ast_search import: %v", err)
	}
	var importParsed struct {
		Matches []struct {
			File string `json:"file"`
			Line int    `json:"line"`
			Kind string `json:"kind"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(importResp), &importParsed); err != nil {
		t.Fatalf("parse import response: %v", err)
	}
	if len(importParsed.Matches) != 1 || importParsed.Matches[0].File != "web/app.ts" || importParsed.Matches[0].Line != 1 || importParsed.Matches[0].Kind != "import" {
		t.Fatalf("unexpected import matches: %+v", importParsed.Matches)
	}

	callResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "ast_search",
		Arguments: `{"query":"Target","kind":"call","include":"*.go"}`,
	})
	if err != nil {
		t.Fatalf("ast_search call: %v", err)
	}
	var callParsed struct {
		Matches []struct {
			File string `json:"file"`
			Line int    `json:"line"`
			Kind string `json:"kind"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(callResp), &callParsed); err != nil {
		t.Fatalf("parse call response: %v", err)
	}
	if len(callParsed.Matches) != 1 || callParsed.Matches[0].File != "main.go" || callParsed.Matches[0].Line != 10 || callParsed.Matches[0].Kind != "call" {
		t.Fatalf("unexpected call matches: %+v", callParsed.Matches)
	}
}

func TestToolkit_RepoMapReturnsStructuredWorkspaceSummary(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "AGENTS.md"), "rules\n")
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/repo\n")
	mustWriteFile(t, filepath.Join(root, "checkout", "discount.go"), "package checkout\n")
	mustWriteFile(t, filepath.Join(root, "checkout", "discount_test.go"), "package checkout\n")
	mustWriteFile(t, filepath.Join(root, "web", "app.tsx"), "export const App = () => null\n")
	mustWriteFile(t, filepath.Join(root, ".wuu", "sessions", "trace.jsonl"), "{}\n")
	mustWriteFile(t, filepath.Join(root, "node_modules", "pkg", "index.js"), "ignored\n")

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "repo_map",
		Arguments: `{"max_listed_files":10}`,
	})
	if err != nil {
		t.Fatalf("repo_map: %v", err)
	}
	if strings.Contains(resp, "node_modules") || strings.Contains(resp, ".wuu") {
		t.Fatalf("repo_map should skip generated/internal state paths: %s", resp)
	}
	var parsed struct {
		Action            string `json:"action"`
		WorkspaceRevision string `json:"workspace_revision"`
		Summary           struct {
			FilesScanned int `json:"files_scanned"`
			Languages    []struct {
				Name  string `json:"name"`
				Count int    `json:"count"`
			} `json:"languages"`
			TestMappings []struct {
				Source string `json:"source"`
				Test   string `json:"test"`
			} `json:"test_mappings"`
			RepresentativeFiles []string `json:"representative_files"`
		} `json:"summary"`
		Suggestions []string `json:"next_suggestions"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse repo_map response: %v\n%s", err, resp)
	}
	if parsed.Action != "repo_map" {
		t.Fatalf("repo_map action = %q, want repo_map", parsed.Action)
	}
	if !strings.HasPrefix(parsed.WorkspaceRevision, "fs:worktree:") {
		t.Fatalf("repo_map response missing workspace revision: %+v", parsed)
	}
	if parsed.Summary.FilesScanned != 5 {
		t.Fatalf("unexpected files scanned: %+v", parsed.Summary)
	}
	if len(parsed.Summary.TestMappings) != 1 || parsed.Summary.TestMappings[0].Source != "checkout/discount.go" || parsed.Summary.TestMappings[0].Test != "checkout/discount_test.go" {
		t.Fatalf("unexpected test mappings: %+v", parsed.Summary.TestMappings)
	}
	if len(parsed.Summary.RepresentativeFiles) == 0 || parsed.Summary.RepresentativeFiles[0] != "AGENTS.md" {
		t.Fatalf("unexpected representative files: %+v", parsed.Summary.RepresentativeFiles)
	}
	if len(parsed.Suggestions) == 0 || !strings.Contains(strings.Join(parsed.Suggestions, " "), "read_file") {
		t.Fatalf("repo_map response missing confirmation suggestion: %+v", parsed.Suggestions)
	}
}

func TestToolkit_SemanticSearchRanksConceptCandidates(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "checkout", "discount.go"), `package checkout

// CartDiscountTotal computes the final checkout total after promo discounts.
func CartDiscountTotal(subtotalCents int, discountCents int) int {
	return subtotalCents - discountCents
}
`)
	mustWriteFile(t, filepath.Join(root, "auth", "session.go"), `package auth

func ExpireSession() {}
`)
	mustWriteFile(t, filepath.Join(root, ".env"), "CHECKOUT_DISCOUNT_TOTAL=secret\n")

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "semantic_search",
		Arguments: `{"query":"checkout discount total","include":"*.go","max_results":5}`,
	})
	if err != nil {
		t.Fatalf("semantic_search: %v", err)
	}
	if strings.Contains(resp, "secret") || strings.Contains(resp, ".env") {
		t.Fatalf("semantic_search response leaked sensitive path/content: %s", resp)
	}
	var parsed struct {
		Action            string                `json:"action"`
		Query             string                `json:"query"`
		Terms             []string              `json:"terms"`
		WorkspaceRevision string                `json:"workspace_revision"`
		Total             int                   `json:"total"`
		Returned          int                   `json:"returned"`
		Matches           []semanticSearchMatch `json:"matches"`
		Suggestions       []string              `json:"next_suggestions"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("parse semantic_search response: %v\n%s", err, resp)
	}
	if parsed.Action != "semantic_search" {
		t.Fatalf("semantic_search action = %q, want semantic_search", parsed.Action)
	}
	if parsed.Query != "checkout discount total" || len(parsed.Terms) != 3 {
		t.Fatalf("unexpected query metadata: %+v", parsed)
	}
	if !strings.HasPrefix(parsed.WorkspaceRevision, "fs:worktree:") {
		t.Fatalf("semantic_search response missing filesystem workspace revision: %+v", parsed)
	}
	if parsed.Total == 0 || parsed.Returned == 0 || len(parsed.Matches) == 0 {
		t.Fatalf("semantic_search should return at least one candidate: %+v", parsed)
	}
	top := parsed.Matches[0]
	if top.File != "checkout/discount.go" || top.Score <= 0 || len(top.LineMatches) == 0 {
		t.Fatalf("unexpected top semantic candidate: %+v", top)
	}
	if !containsString(top.MatchedTerms, "checkout") || !containsString(top.MatchedTerms, "discount") || !containsString(top.MatchedTerms, "total") {
		t.Fatalf("top candidate missing matched query terms: %+v", top.MatchedTerms)
	}
	if top.ReadFileRange["start_line"] < 1 || !strings.Contains(top.ReadFileSuggestion, "read_file") {
		t.Fatalf("top candidate missing read_file guidance: %+v", top)
	}
	if len(parsed.Suggestions) == 0 || !strings.Contains(strings.Join(parsed.Suggestions, " "), "confirm") {
		t.Fatalf("semantic_search response missing confirmation suggestion: %+v", parsed.Suggestions)
	}
}

func TestToolkit_GrepLargeContentReturnsValidBudgetedJSON(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var content strings.Builder
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&content, "target-%03d %s\n", i, strings.Repeat("x", 500))
	}
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(content.String()), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	resp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "grep",
		Arguments: `{"pattern":"target"}`,
	})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if len(resp) > maxGrepOutputBytes {
		t.Fatalf("grep response length = %d, want <= %d", len(resp), maxGrepOutputBytes)
	}
	var parsed struct {
		Total              int         `json:"total"`
		Truncated          bool        `json:"truncated"`
		Matches            []grepMatch `json:"matches"`
		OmittedMatchCount  int         `json:"omitted_match_count"`
		ReturnedMatchCount int         `json:"returned_match_count"`
		Suggestions        []string    `json:"next_suggestions"`
	}
	if err := json.Unmarshal([]byte(resp), &parsed); err != nil {
		t.Fatalf("grep response must stay valid JSON after budgeting: %v\n%s", err, resp)
	}
	if !parsed.Truncated || parsed.OmittedMatchCount == 0 || parsed.ReturnedMatchCount != len(parsed.Matches) || parsed.ReturnedMatchCount >= parsed.Total {
		t.Fatalf("unexpected budgeted grep metadata: %+v", parsed)
	}
	if len(parsed.Suggestions) == 0 || !strings.Contains(strings.Join(parsed.Suggestions, " "), "narrow") {
		t.Fatalf("budgeted grep response missing narrowing suggestion: %+v", parsed.Suggestions)
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
		{File: ".hidden/app.env", Line: 1, Content: "[REDACTED: sensitive file content]"},
		{File: "visible/app.env", Line: 1, Content: "[REDACTED: sensitive file content]"},
	}
	if !reflect.DeepEqual(parsed.Matches, want) {
		t.Fatalf("unexpected hidden grep fallback matches: got %+v want %+v", parsed.Matches, want)
	}
	if strings.Contains(resp, "API_KEY=hidden") || strings.Contains(resp, "API_KEY=visible") {
		t.Fatalf("grep fallback response leaked sensitive file content: %s", resp)
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

func TestToolkit_MemoryTools_HiddenWithoutProvider(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	names := map[string]bool{}
	for _, d := range kit.Definitions() {
		names[d.Name] = true
	}
	for _, want := range []string{"read_memory", "write_memory"} {
		if names[want] {
			t.Errorf("memory tool %q should be hidden without provider", want)
		}
	}
}

func TestToolkit_MemoryTools_NoProviderReturnsError(t *testing.T) {
	// Without a provider, every memory tool returns a Go error so the
	// model observes the failure as a tool error rather than silently
	// reading an empty list.
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_memory",
		Arguments: `{"content":"a note"}`,
	})
	if err == nil {
		t.Fatalf("expected error from write_memory without provider, got nil")
	}
	if !strings.Contains(err.Error(), "no Provider") {
		t.Fatalf("expected no-provider error, got: %v", err)
	}

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_memory",
		Arguments: `{"limit":5}`,
	})
	if err == nil {
		t.Fatalf("expected error from read_memory without provider, got nil")
	}
}

func TestToolkit_MemoryTools_SetMemorySwapsProvider(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Attach a real FileProvider so the registry dispatches to a working
	// backend that actually persists to disk.
	memDir := t.TempDir()
	provider, err := memstore.NewFileProvider(memDir)
	if err != nil {
		t.Fatalf("NewFileProvider: %v", err)
	}
	kit.SetMemory(provider)
	names := definitionNames(kit.Definitions())
	for _, want := range []string{"read_memory", "write_memory"} {
		if !names[want] {
			t.Fatalf("memory tool %q missing from Definitions() after attaching provider", want)
		}
	}

	writeResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_memory",
		Arguments: `{"content":"prefer tabs over spaces","tags":["go","formatting"]}`,
	})
	if err != nil {
		t.Fatalf("write_memory with provider: %v", err)
	}
	if !strings.Contains(writeResp, `"written":true`) {
		t.Fatalf("unexpected write_memory response: %s", writeResp)
	}

	readResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_memory",
		Arguments: `{"query":"tabs","limit":5}`,
	})
	if err != nil {
		t.Fatalf("read_memory: %v", err)
	}
	if !strings.Contains(readResp, "prefer tabs over spaces") {
		t.Fatalf("expected read to find the stored note, got: %s", readResp)
	}

	// Detaching returns the tools to the "no provider" error branch.
	kit.SetMemory(nil)
	if _, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_memory",
		Arguments: `{"content":"a note"}`,
	}); err == nil || !strings.Contains(err.Error(), "no Provider") {
		t.Fatalf("expected no-provider error after detach, got: %v", err)
	}
}
