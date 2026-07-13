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
