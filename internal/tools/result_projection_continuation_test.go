package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/toolresult"
)

func TestRecordProjectorContinuationAdvancesByDisplayedRecords(t *testing.T) {
	makeRaw := func(offset, count int) string {
		files := make([]string, count)
		for i := range files {
			files[i] = fmt.Sprintf("file-%05d-%s.go", offset+i, strings.Repeat("path", 8))
		}
		return mustMarshalMap(map[string]any{
			"action":             "glob",
			"pattern":            "**/*.go",
			"workspace_revision": "revision-1",
			"offset":             offset,
			"has_more":           false,
			"files":              files,
		})
	}
	pc := projectorContext{CallID: "page", BudgetTokens: defaultProjectionTokenBudget, ArtifactRef: "/s/page.txt"}
	firstOut, _, ok := projectGlobResult(makeRaw(0, 1000), pc)
	if !ok {
		t.Fatal("first projection declined")
	}
	first := parseOut(t, firstOut)
	firstFiles := first["files"].([]any)
	next := first["page"].(map[string]any)["next"].(map[string]any)
	nextOffset := int(next["offset"].(float64))
	if nextOffset != len(firstFiles) || next["expected_revision"] != "revision-1" {
		t.Fatalf("cursor did not advance by displayed records: next=%+v displayed=%d", next, len(firstFiles))
	}

	secondOut, _, ok := projectGlobResult(makeRaw(nextOffset, 1000), pc)
	if !ok {
		t.Fatal("second projection declined")
	}
	second := parseOut(t, secondOut)
	secondFiles := second["files"].([]any)
	if len(secondFiles) == 0 || secondFiles[0] == firstFiles[len(firstFiles)-1] {
		t.Fatalf("second page repeated first-page evidence: first_last=%v second_first=%v", firstFiles[len(firstFiles)-1], secondFiles[0])
	}
}

func TestSearchHelpersSortFullSetBeforePaging(t *testing.T) {
	root := t.TempDir()
	for i := 519; i >= 0; i-- {
		mustWriteFile(t, filepath.Join(root, fmt.Sprintf("file-%03d.txt", i)), "needle\n")
	}
	files, err := globWithFallback(root, root, "**/*.txt", 0)
	if err != nil {
		t.Fatalf("glob fallback: %v", err)
	}
	if len(files) != 520 || files[0] != "file-000.txt" || files[519] != "file-519.txt" {
		t.Fatalf("glob did not return globally sorted full set: len=%d first=%q last=%q", len(files), files[0], files[len(files)-1])
	}
	first, more := pageWindow(files, 0, globPageSize)
	second, _ := pageWindow(files, len(first), globPageSize)
	if !more || len(first) != globPageSize || len(second) != 20 || first[len(first)-1] == second[0] {
		t.Fatalf("unstable page split: first=%d second=%d more=%v", len(first), len(second), more)
	}
}

func TestStableOffsetRejectsMissingOrChangedRevision(t *testing.T) {
	if err := validateStableOffset("glob", 10, "", "rev-1"); err == nil {
		t.Fatal("non-zero offset without revision was accepted")
	}
	if err := validateStableOffset("glob", 10, "rev-1", "rev-2"); err == nil {
		t.Fatal("stale revision was accepted")
	}
	if err := validateStableOffset("glob", 10, "rev-1", "rev-1"); err != nil {
		t.Fatalf("matching snapshot was rejected: %v", err)
	}
}

func TestContinuationSnapshotRevisionIncludesResultContent(t *testing.T) {
	first := continuationSnapshotRevision("git:same", "grep:content", []string{"match one"})
	second := continuationSnapshotRevision("git:same", "grep:content", []string{"match two"})
	if first == second {
		t.Fatal("result content changed without changing the continuation snapshot")
	}
}

func TestReadFileByteContinuationReturnsDisjointWindowsAndRejectsMutation(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("new toolkit: %v", err)
	}
	kit.env.SessionDir = t.TempDir()
	artifact := filepath.Join(kit.env.SessionDir, "tool-results", "blob.txt")
	mustWriteFile(t, artifact, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	tool := NewReadFileTool(kit.env)
	firstRaw, err := tool.Execute(context.Background(), fmt.Sprintf(`{"path":%q,"byte_range":{"offset":0,"limit":16}}`, artifact))
	if err != nil {
		t.Fatalf("first byte read: %v", err)
	}
	first := parseOut(t, firstRaw)
	next := first["continuation"].(map[string]any)["next"].(map[string]any)
	nextArgs, _ := json.Marshal(next)
	secondRaw, err := tool.Execute(context.Background(), string(nextArgs))
	if err != nil {
		t.Fatalf("second byte read: %v", err)
	}
	second := parseOut(t, secondRaw)
	if first["content"].(string)+second["content"].(string) != "0123456789abcdefghijklmnopqrstuv" {
		t.Fatalf("byte windows overlapped or skipped: first=%q second=%q", first["content"], second["content"])
	}
	if err := os.WriteFile(artifact, []byte("changed artifact content"), 0o644); err != nil {
		t.Fatalf("mutate artifact: %v", err)
	}
	if _, err := tool.Execute(context.Background(), string(nextArgs)); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("mutated artifact continuation was not rejected: %v", err)
	}
}

func TestReadFileProjectorPointsAtOnlyOmittedMiddle(t *testing.T) {
	rawText, _, _ := readFileEnvelope(5000)
	raw := parseOut(t, rawText)
	raw["content_sha256"] = "file-hash"
	pc := projectorContext{CallID: "read", BudgetTokens: defaultProjectionTokenBudget, ArtifactRef: "/s/read.txt"}
	out, _, ok := projectReadFileResult(mustMarshalMap(raw), pc)
	if !ok {
		t.Fatal("read_file projector declined")
	}
	m := parseOut(t, out)
	next := m["continuation"].(map[string]any)["next"].(map[string]any)
	continuation, err := decodeReadFileContinuation(next["continuation"].(string))
	if err != nil || continuation.ExpectedSHA256 != "file-hash" || continuation.Offset <= 1 || continuation.Limit <= 0 {
		t.Fatalf("invalid omitted-middle continuation: %+v", next)
	}
}

func TestBashProjectorExposesRankedNonOverlappingArtifactRanges(t *testing.T) {
	raw := bashEnvelope(map[string]any{
		"output":          strings.Repeat("combined\n", 5000),
		"stdout_tail":     strings.Repeat("stdout tail\n", 1000),
		"stderr_tail":     strings.Repeat("stderr tail\n", 1000),
		"full_log_sha256": "shell-hash",
		"full_log_sections": map[string]any{
			"stdout_start": 100,
			"stdout_end":   100000,
			"stderr_start": 100100,
			"stderr_end":   200000,
		},
	})
	pc := projectorContext{CallID: "bash", BudgetTokens: defaultProjectionTokenBudget, ArtifactRef: "/s/shell.log"}
	out, _, ok := projectBashResult(raw, pc)
	if !ok {
		t.Fatal("bash projector declined")
	}
	m := parseOut(t, out)
	ranges := m["continuation"].(map[string]any)["ranges"].([]any)
	if len(ranges) != 2 || ranges[0].(map[string]any)["stream"] != "stderr" || ranges[1].(map[string]any)["stream"] != "stdout" {
		t.Fatalf("bash recovery ranges are not failure-first: %+v", ranges)
	}
	for _, value := range ranges {
		next := value.(map[string]any)["next"].(map[string]any)
		continuation, err := decodeReadFileContinuation(next["continuation"].(string))
		if err != nil || continuation.ExpectedSHA256 != "shell-hash" {
			t.Fatalf("bash recovery range is not snapshot-bound: %+v", next)
		}
		if continuation.ByteOffset == nil || continuation.ByteEndOffset == nil || *continuation.ByteEndOffset <= *continuation.ByteOffset {
			t.Fatalf("empty or reversed recovery range: %+v", continuation)
		}
	}
}

func TestGenericProjectionContinuationCoversOnlyOmittedBytes(t *testing.T) {
	text := strings.Repeat("head-tail-evidence-", 5000)
	out := buildBoundedResultReference("/s/generic.txt", text, toolresult.FromText(text), defaultResultBudget)
	m := parseOut(t, out)
	head := m["preview_head"].(string)
	tail := m["preview_tail"].(string)
	next := m["continuation"].(map[string]any)["next"].(map[string]any)
	continuation, err := decodeReadFileContinuation(next["continuation"].(string))
	if err != nil || continuation.ExpectedSHA256 == "" {
		t.Fatalf("generic continuation is not snapshot-bound: %+v", next)
	}
	if continuation.ByteOffset == nil || continuation.ByteEndOffset == nil || *continuation.ByteOffset != len(head) || *continuation.ByteEndOffset != len(text)-len(tail) {
		t.Fatalf("generic continuation overlaps preview: head=%d tail=%d range=%+v", len(head), len(tail), continuation)
	}
}

func TestGenericProjectionLineLimitPreviewDoesNotOverlapItself(t *testing.T) {
	text := strings.Repeat("x\n", defaultResultMaxLines+100)
	out := buildBoundedResultReference("/s/lines.txt", text, toolresult.FromText(text), defaultResultBudget)
	m := parseOut(t, out)
	head := m["preview_head"].(string)
	tail := m["preview_tail"].(string)
	if len(head)+len(tail) > len(text) || head != text[:len(head)] || tail != text[len(text)-len(tail):] {
		t.Fatalf("line-limit preview overlapped or selected unstable evidence: head=%d tail=%d total=%d", len(head), len(tail), len(text))
	}
	continuation := m["continuation"].(map[string]any)
	if continuation["has_more"].(bool) != (len(head)+len(tail) < len(text)) {
		t.Fatalf("line-limit continuation did not match omitted bytes: %+v", continuation)
	}
}

func TestStructuredGenericProjectionHasSnapshotBoundByteContinuation(t *testing.T) {
	raw := toolresult.Result{StructuredContent: json.RawMessage(`{"records":["one","two","three"]}`)}
	contextual := raw.TextProjection()
	out := buildBoundedResultReference("/s/structured.json", contextual, raw, defaultResultBudget)
	m := parseOut(t, out)
	if m["kind"] != "archived_structured_tool_result" {
		t.Fatalf("structured result did not use an index envelope: %+v", m)
	}
	next := m["continuation"].(map[string]any)["next"].(map[string]any)
	continuation, err := decodeReadFileContinuation(next["continuation"].(string))
	if err != nil || continuation.ExpectedSHA256 == "" || continuation.ByteEndOffset == nil || *continuation.ByteEndOffset != len(contextual) {
		t.Fatalf("structured continuation is not an exact snapshot range: %+v", next)
	}
}
