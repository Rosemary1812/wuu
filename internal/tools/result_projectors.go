package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Per-tool projectors. Each understands its tool's JSON envelope and reduces it
// to fit the token budget by dropping WHOLE records or WHOLE lines — never by
// slicing the serialized JSON, which would corrupt structure. Every projector
// preserves the envelope's scalar evidence (counts, revision, flags) and adds a
// "projection" object pointing at the recoverable artifact.
//
// Projectors are deterministic and fail open (return ok=false) on any malformed
// payload so the finalizer keeps the full result.

func init() {
	toolProjectors["glob"] = projectGlobResult
	toolProjectors["list_files"] = projectListFilesResult
	toolProjectors["grep"] = projectGrepResult
	toolProjectors["read_file"] = projectReadFileResult
	toolProjectors["bash"] = projectBashResult

	// bash embeds a recoverable full log in its own envelope; reuse it instead
	// of persisting a duplicate copy of the raw result.
	projectionArtifactExtractors["bash"] = extractBashFullLogRef
}

// parseToolEnvelope decodes a tool's JSON envelope while preserving numbers
// exactly (json.Number) so re-serialization is faithful and deterministic.
func parseToolEnvelope(rawText string) (map[string]any, bool) {
	dec := json.NewDecoder(strings.NewReader(rawText))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil || m == nil {
		return nil, false
	}
	return m, true
}

// marshalEnvelope serializes a projected envelope. Map keys are sorted by
// encoding/json, so identical input yields identical bytes.
func marshalEnvelope(m map[string]any) (string, bool) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return "", false
	}
	return strings.TrimRight(buf.String(), "\n"), true
}

// largestFitting returns the greatest keep in [0,total] whose candidate size is
// within budget. size(keep) must be non-decreasing in keep.
func largestFitting(total, budget int, size func(keep int) int) int {
	lo, hi, best := 0, total, 0
	for lo <= hi {
		mid := (lo + hi) / 2
		if size(mid) <= budget {
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return best
}

// setProjectionMeta records that the envelope was projected, points the model
// at the recoverable artifact, and marks the result truncated. omitted carries
// tool-specific omission counts already surfaced elsewhere in the envelope.
func setProjectionMeta(m map[string]any, budget int, artifactRef, recover string, omitted map[string]any) {
	proj := map[string]any{
		"projected":     true,
		"budget_tokens": budget,
		"artifact_ref":  artifactRef,
		"recover":       recover,
	}
	for k, v := range omitted {
		proj[k] = v
	}
	m["projection"] = proj
	m["truncated"] = true
}

// projectRecordArray is the shared reducer for envelopes whose bulk is a single
// list of records (glob "files", list_files "entries", grep "matches"/"files"/
// "counts"). It keeps the largest prefix of records that fits the budget and
// records how many were dropped.
func projectRecordArray(rawText, arrayKey, recover string, pc projectorContext, extraOmitted func(kept, omitted int) map[string]any) (string, projectionOmission, bool) {
	m, ok := parseToolEnvelope(rawText)
	if !ok {
		return "", projectionOmission{}, false
	}
	arr, ok := m[arrayKey].([]any)
	if !ok {
		return "", projectionOmission{}, false
	}
	total := len(arr)

	size := func(keep int) int {
		candidate := cloneShallow(m)
		candidate[arrayKey] = arr[:keep]
		om := map[string]any{"omitted_" + singular(arrayKey): total - keep}
		if extraOmitted != nil {
			for k, v := range extraOmitted(keep, total-keep) {
				om[k] = v
			}
		}
		setProjectionMeta(candidate, pc.BudgetTokens, pc.ArtifactRef, recover, om)
		s, ok := marshalEnvelope(candidate)
		if !ok {
			return pc.BudgetTokens + 1
		}
		return estimateResultTokens(s)
	}

	keep := largestFitting(total, pc.BudgetTokens, size)
	omitted := total - keep
	m[arrayKey] = arr[:keep]
	om := map[string]any{"omitted_" + singular(arrayKey): omitted}
	if extraOmitted != nil {
		for k, v := range extraOmitted(keep, omitted) {
			om[k] = v
		}
	}
	setProjectionMeta(m, pc.BudgetTokens, pc.ArtifactRef, recover, om)
	out, ok := marshalEnvelope(m)
	if !ok {
		return "", projectionOmission{}, false
	}
	return out, projectionOmission{Records: omitted}, true
}

func cloneShallow(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// singular maps a plural array key to a compact singular label for the
// omitted-count field ("files" -> "file", "entries" -> "entry").
func singular(arrayKey string) string {
	switch arrayKey {
	case "entries":
		return "entry"
	case "matches":
		return "match"
	case "files":
		return "file"
	case "counts":
		return "count"
	default:
		return arrayKey
	}
}

func projectGlobResult(rawText string, pc projectorContext) (string, projectionOmission, bool) {
	return projectRecordArray(rawText, "files",
		fmt.Sprintf("narrow the glob pattern or path; the full match list is saved at %s", pc.ArtifactRef),
		pc, nil)
}

func projectListFilesResult(rawText string, pc projectorContext) (string, projectionOmission, bool) {
	return projectRecordArray(rawText, "entries",
		fmt.Sprintf("narrow the directory or read specific entries; the full listing is saved at %s", pc.ArtifactRef),
		pc, nil)
}

// projectGrepResult handles all three grep output modes by reducing whichever
// record array the envelope carries: content ("matches"), files_with_matches
// ("files"), or count ("counts").
func projectGrepResult(rawText string, pc projectorContext) (string, projectionOmission, bool) {
	m, ok := parseToolEnvelope(rawText)
	if !ok {
		return "", projectionOmission{}, false
	}
	arrayKey := ""
	for _, key := range []string{"matches", "files", "counts"} {
		if _, ok := m[key].([]any); ok {
			arrayKey = key
			break
		}
	}
	if arrayKey == "" {
		return "", projectionOmission{}, false
	}
	recover := fmt.Sprintf("narrow the grep pattern, path, or include filter; the full matches are saved at %s", pc.ArtifactRef)
	// grep content mode also carries returned/omitted match counters that the
	// model relies on; keep them consistent with what actually remains.
	extra := func(kept, omitted int) map[string]any {
		if arrayKey != "matches" {
			return nil
		}
		return map[string]any{
			"returned_match_count": kept,
			"omitted_match_count":  omitted,
		}
	}
	out, om, ok := projectRecordArray(rawText, arrayKey, recover, pc, extra)
	if !ok {
		return "", projectionOmission{}, false
	}
	return out, om, true
}

// contentLines splits a read_file line-numbered content blob into whole lines,
// dropping the trailing empty element produced by the terminating newline.
func contentLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// projectReadFileResult keeps whole head and tail lines of the read content,
// never cutting mid-line, and marks the omitted middle range explicitly.
func projectReadFileResult(rawText string, pc projectorContext) (string, projectionOmission, bool) {
	m, ok := parseToolEnvelope(rawText)
	if !ok {
		return "", projectionOmission{}, false
	}
	content, ok := m["content"].(string)
	if !ok {
		return "", projectionOmission{}, false
	}
	lines := contentLines(content)
	numLines := len(lines)
	path, _ := m["path"].(string)
	recover := fmt.Sprintf("re-run read_file on %s with an offset/limit for the omitted lines, or open the full saved result at %s", path, pc.ArtifactRef)

	build := func(keep int) (string, int) {
		if keep >= numLines {
			return content, 0
		}
		head := (keep + 1) / 2
		tail := keep - head
		omitted := numLines - head - tail
		var b strings.Builder
		for i := 0; i < head; i++ {
			b.WriteString(lines[i])
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "... omitted %d lines; %s ...\n", omitted, recover)
		for i := numLines - tail; i < numLines; i++ {
			b.WriteString(lines[i])
			b.WriteByte('\n')
		}
		return b.String(), omitted
	}

	size := func(keep int) int {
		c, omitted := build(keep)
		cand := cloneShallow(m)
		cand["content"] = c
		setProjectionMeta(cand, pc.BudgetTokens, pc.ArtifactRef, recover, map[string]any{
			"omitted_lines": omitted,
			"shown_lines":   keep,
		})
		s, ok := marshalEnvelope(cand)
		if !ok {
			return pc.BudgetTokens + 1
		}
		return estimateResultTokens(s)
	}

	keep := largestFitting(numLines, pc.BudgetTokens, size)
	c, omitted := build(keep)
	m["content"] = c
	setProjectionMeta(m, pc.BudgetTokens, pc.ArtifactRef, recover, map[string]any{
		"omitted_lines": omitted,
		"shown_lines":   keep,
	})
	out, ok := marshalEnvelope(m)
	if !ok {
		return "", projectionOmission{}, false
	}
	return out, projectionOmission{Lines: omitted}, true
}

func extractBashFullLogRef(rawText string) string {
	m, ok := parseToolEnvelope(rawText)
	if !ok {
		return ""
	}
	ref, _ := m["full_log_ref"].(string)
	return ref
}

// lastLines returns the last n whole lines of s.
func lastLines(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if n >= len(lines) {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

func lineCount(s string) int {
	if strings.TrimRight(s, "\n") == "" {
		return 0
	}
	return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1
}

// projectBashResult prioritizes failure evidence. It drops the redundant
// combined "output" field (recoverable via full_log_ref, and duplicated by the
// tails), always keeps exit code / timeout / duration / revision / verification,
// and trims stdout then stderr tails by whole lines — most recent first — to fit
// the budget. Verification evidence is never dropped, so a result dominated by a
// large verification failure may still exceed the budget by design.
func projectBashResult(rawText string, pc projectorContext) (string, projectionOmission, bool) {
	m, ok := parseToolEnvelope(rawText)
	if !ok {
		return "", projectionOmission{}, false
	}
	droppedOutput := 0
	if out, ok := m["output"].(string); ok {
		droppedOutput = len(out)
	}
	delete(m, "output")

	stdout, _ := m["stdout_tail"].(string)
	stderr, _ := m["stderr_tail"].(string)

	build := func(so, se string) (string, int) {
		cand := cloneShallow(m)
		cand["stdout_tail"] = so
		cand["stderr_tail"] = se
		setProjectionMeta(cand, pc.BudgetTokens, pc.ArtifactRef,
			fmt.Sprintf("open the full command log at %s", pc.ArtifactRef),
			map[string]any{
				"dropped_output_bytes": droppedOutput,
				"stdout_tail_trimmed":  lineCount(so) < lineCount(stdout),
				"stderr_tail_trimmed":  lineCount(se) < lineCount(stderr),
			})
		s, ok := marshalEnvelope(cand)
		if !ok {
			return "", pc.BudgetTokens + 1
		}
		return s, estimateResultTokens(s)
	}

	// Fast path: dropping the redundant output may already fit.
	if s, tok := build(stdout, stderr); tok <= pc.BudgetTokens {
		return s, projectionOmission{Bytes: droppedOutput}, true
	}

	// Reduce stdout first (lower priority than stderr).
	soLines := lineCount(stdout)
	keepSo := largestFitting(soLines, pc.BudgetTokens, func(k int) int {
		_, tok := build(lastLines(stdout, k), stderr)
		return tok
	})
	so := lastLines(stdout, keepSo)
	if s, tok := build(so, stderr); tok <= pc.BudgetTokens {
		return s, projectionOmission{Bytes: droppedOutput}, true
	}

	// Still over budget: reduce stderr too (keep the most recent lines).
	seLines := lineCount(stderr)
	keepSe := largestFitting(seLines, pc.BudgetTokens, func(k int) int {
		_, tok := build("", lastLines(stderr, k))
		return tok
	})
	se := lastLines(stderr, keepSe)
	s, _ := build(so, se)
	// Return best-effort even if still over budget: verification/metadata is the
	// evidence we refuse to drop.
	return s, projectionOmission{Bytes: droppedOutput}, true
}
