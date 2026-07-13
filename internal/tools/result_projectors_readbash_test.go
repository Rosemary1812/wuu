package tools

import (
	"fmt"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/toolresult"
)

func readFileEnvelope(numLines int) (rawText string, firstLine, lastLine string) {
	var content strings.Builder
	for i := 1; i <= numLines; i++ {
		line := fmt.Sprintf("%6d\tfunc handler%04d(ctx context.Context) error { return process(ctx, %d) }", i, i, i)
		content.WriteString(line)
		content.WriteByte('\n')
		if i == 1 {
			firstLine = line
		}
		if i == numLines {
			lastLine = line
		}
	}
	raw := mustMarshalMap(map[string]any{
		"action":             "read",
		"path":               "internal/pkg/handlers.go",
		"workspace_revision": "git:abc123:worktree:deadbeef",
		"content":            content.String(),
		"num_lines":          numLines,
		"start_line":         1,
		"total_lines":        numLines,
		"truncated":          false,
	})
	return raw, firstLine, lastLine
}

func TestProjectReadFileResult(t *testing.T) {
	const numLines, budget = 5000, defaultProjectionTokenBudget
	raw, firstLine, lastLine := readFileEnvelope(numLines)
	pc := projectorContext{CallID: "c1", BudgetTokens: budget, ArtifactRef: "/s/tool-results/c1.txt"}

	out, om, ok := projectReadFileResult(raw, pc)
	if !ok {
		t.Fatalf("read_file projector declined")
	}
	if got := estimateResultTokens(out); got > budget {
		t.Fatalf("projected read_file = %d tokens, over budget %d", got, budget)
	}
	if om.Lines <= 0 {
		t.Fatalf("expected omitted lines, got %d", om.Lines)
	}
	m := parseOut(t, out)
	content, _ := m["content"].(string)
	// Whole lines preserved head and tail (no mid-line cut).
	if !strings.HasPrefix(content, firstLine+"\n") {
		t.Fatalf("head line not preserved whole:\n%s", snip(content, 120))
	}
	if !strings.HasSuffix(strings.TrimRight(content, "\n"), lastLine) {
		t.Fatalf("tail line not preserved whole:\n...%s", content[len(content)-120:])
	}
	if !strings.Contains(content, "... omitted ") {
		t.Fatalf("missing explicit omission marker")
	}
	// File facts preserved.
	if fmt.Sprint(m["total_lines"]) != fmt.Sprint(numLines) || m["path"] != "internal/pkg/handlers.go" {
		t.Fatalf("read_file metadata not preserved: %v", m)
	}
	proj := m["projection"].(map[string]any)
	if fmt.Sprint(proj["omitted_lines"]) != fmt.Sprint(om.Lines) {
		t.Fatalf("omitted_lines mismatch: %v vs %d", proj["omitted_lines"], om.Lines)
	}
	// Deterministic.
	out2, _, _ := projectReadFileResult(raw, pc)
	if out2 != out {
		t.Fatalf("read_file projection not deterministic")
	}
}

func TestProjectReadFile_MalformedFailOpen(t *testing.T) {
	pc := projectorContext{CallID: "c", BudgetTokens: defaultProjectionTokenBudget, ArtifactRef: "/s/c.txt"}
	if _, _, ok := projectReadFileResult(`{"action":"read","content":42}`, pc); ok {
		t.Fatalf("non-string content must fail open")
	}
	if _, _, ok := projectReadFileResult("not json", pc); ok {
		t.Fatalf("invalid json must fail open")
	}
}

func bashEnvelope(m map[string]any) string {
	base := map[string]any{
		"action":             "run",
		"command":            "go test ./...",
		"classification":     map[string]any{"category": "test"},
		"exit_code":          0,
		"duration_ms":        1234,
		"timed_out":          false,
		"truncated":          true,
		"workspace_revision": "git:abc123:worktree:deadbeef",
		"stdout_bytes":       100000,
		"stderr_bytes":       0,
		"full_log_ref":       "/s/tool-results/shell-logs/x.log",
		"full_log_bytes":     100000,
	}
	for k, v := range m {
		base[k] = v
	}
	return mustMarshalMap(base)
}

func TestProjectBash_DropsRedundantOutput(t *testing.T) {
	const budget = defaultProjectionTokenBudget
	bigOutput := strings.Repeat("noise line of build output\n", 3000) // ~80KB, redundant with tails
	raw := bashEnvelope(map[string]any{
		"output":      bigOutput,
		"stdout_tail": "last few stdout lines\nok\n",
		"stderr_tail": "",
	})
	pc := projectorContext{CallID: "c1", BudgetTokens: budget, ArtifactRef: "/s/tool-results/shell-logs/x.log"}
	out, om, ok := projectBashResult(raw, pc)
	if !ok {
		t.Fatalf("bash projector declined")
	}
	if got := estimateResultTokens(out); got > budget {
		t.Fatalf("projected bash = %d tokens, over budget %d", got, budget)
	}
	if om.Bytes != len(bigOutput) {
		t.Fatalf("dropped-output bytes = %d, want %d", om.Bytes, len(bigOutput))
	}
	m := parseOut(t, out)
	if _, present := m["output"]; present {
		t.Fatalf("redundant output field must be dropped")
	}
	// Failure/facts evidence preserved.
	for _, key := range []string{"exit_code", "duration_ms", "timed_out", "workspace_revision", "full_log_ref"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("bash projection dropped required field %q", key)
		}
	}
	if !strings.Contains(m["stdout_tail"].(string), "ok") {
		t.Fatalf("small stdout tail should be preserved")
	}
}

func TestProjectBash_TrimsStdoutButKeepsStderrAndVerification(t *testing.T) {
	const budget = defaultProjectionTokenBudget
	hugeStdout := strings.Repeat("stdout progress line\n", 4000)
	stderr := "FAIL: TestThing at handlers_test.go:42\nexpected 3 got 4\n"
	raw := bashEnvelope(map[string]any{
		"exit_code":   1,
		"output":      hugeStdout,
		"stdout_tail": hugeStdout,
		"stderr_tail": stderr,
		"verification": map[string]any{
			"passed":        false,
			"failing_tests": []string{"TestThing"},
			"summary":       "1 test failed at handlers_test.go:42",
		},
	})
	pc := projectorContext{CallID: "c2", BudgetTokens: budget, ArtifactRef: "/s/tool-results/shell-logs/x.log"}
	out, _, ok := projectBashResult(raw, pc)
	if !ok {
		t.Fatalf("bash projector declined")
	}
	if got := estimateResultTokens(out); got > budget {
		t.Fatalf("projected bash = %d tokens, over budget %d", got, budget)
	}
	m := parseOut(t, out)
	// stderr (higher priority) fully preserved.
	if m["stderr_tail"].(string) != stderr {
		t.Fatalf("stderr tail must be preserved intact, got %q", m["stderr_tail"])
	}
	// stdout trimmed to fit.
	if lineCount(m["stdout_tail"].(string)) >= lineCount(hugeStdout) {
		t.Fatalf("stdout tail should have been trimmed")
	}
	// verification evidence kept.
	if _, ok := m["verification"]; !ok {
		t.Fatalf("verification evidence must never be dropped")
	}
}

func TestProjectBash_EndToEndReusesFullLogRef(t *testing.T) {
	dir := t.TempDir()
	bigOutput := strings.Repeat("x", 300000) // force over-budget
	raw := toolresult.FromText(bashEnvelope(map[string]any{
		"output":      bigOutput,
		"stdout_tail": strings.Repeat("tail line\n", 2000),
		"stderr_tail": "",
	}))
	got, d := finalizeBuiltInToolResult(dir, "bash", "c-e2e", raw, 0)
	if !d.Applied || d.Reason != reasonProjected {
		t.Fatalf("bash finalize diag = %+v", d)
	}
	if !d.ArtifactReused || d.ArtifactWritten {
		t.Fatalf("bash must reuse full_log_ref, not persist a duplicate: %+v", d)
	}
	if d.ArtifactRef != "/s/tool-results/shell-logs/x.log" {
		t.Fatalf("artifact ref = %q, want the embedded full_log_ref", d.ArtifactRef)
	}
	if !strings.Contains(got.TextProjection(), "/s/tool-results/shell-logs/x.log") {
		t.Fatalf("projected bash should reference the full log")
	}
}
