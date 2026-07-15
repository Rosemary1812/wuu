package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blueberrycongee/wuu/internal/stringutil"
	"github.com/blueberrycongee/wuu/internal/toolresult"
)

// Result budgeting: large tool outputs are persisted to disk so the
// model receives a compact reference (file path + preview) instead of
// a truncated blob. The model can read_file the full result if needed.

const (
	// defaultResultBudget is the per-result char threshold above which
	// the result is persisted to disk. 50K chars ≈ ~12K tokens — large
	// enough that most tool outputs pass through unchanged, small enough
	// to prevent prompt bloat from runaway grep/shell output.
	defaultResultBudget = 50_000
	// defaultResultMaxLines catches highly fragmented output that is cheap in
	// bytes but still noisy and expensive for a model to reason over.
	defaultResultMaxLines  = 2_000
	projectionPreviewLines = 200
	projectionPreviewBytes = 4_096
)

// finalizeGenericToolResult is the settlement boundary for results that did
// not take a tool-specific projection. It bounds only the model-visible text,
// keeps structured metadata private and intact, and preserves native media.
// The full omitted text must be persisted before the result is changed; if an
// artifact cannot be written, the result fails open unchanged.
func finalizeGenericToolResult(sessionDir, callID string, raw toolresult.Result, threshold int) (toolresult.Result, string, bool) {
	if threshold <= 0 {
		threshold = defaultResultBudget
	}
	contextual := raw.TextProjection()
	if len(contextual) <= threshold && resultLineCount(contextual) <= defaultResultMaxLines {
		return raw, "", false
	}
	if strings.TrimSpace(sessionDir) == "" {
		return raw, "", false
	}
	path, err := persistResult(sessionDir, callID, contextual)
	if err != nil {
		return raw, "", false
	}
	preview := buildBoundedResultReference(path, contextual, raw, threshold)
	return replaceToolResultContext(raw, preview), path, true
}

func resultLineCount(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

// replaceToolResultContext swaps all provider-textual parts for one bounded
// preview at the first textual position while retaining media order and all
// non-provider metadata. Resource bodies are textual provider context in Wuu,
// so they are archived with text instead of surviving as an unbounded bypass.
func replaceToolResultContext(raw toolresult.Result, preview string) toolresult.Result {
	out := raw.Clone()
	content := make([]toolresult.ContentPart, 0, len(raw.Content)+1)
	inserted := false
	for _, part := range raw.Content {
		switch part.Type {
		case toolresult.ContentTypeText, toolresult.ContentTypeResource, toolresult.ContentTypeResourceLink:
			if !inserted {
				content = append(content, toolresult.ContentPart{Type: toolresult.ContentTypeText, Text: preview})
				inserted = true
			}
		default:
			content = append(content, part)
		}
	}
	if !inserted {
		content = append([]toolresult.ContentPart{{Type: toolresult.ContentTypeText, Text: preview}}, content...)
	}
	out.Content = content
	return out
}

func buildBoundedResultReference(path, contextual string, raw toolresult.Result, threshold int) string {
	if len(raw.Content) == 0 && len(raw.StructuredContent) > 0 {
		if indexed := buildStructuredResultIndex(path, raw.StructuredContent, len(contextual)); indexed != "" {
			return indexed
		}
	}
	header := fmt.Sprintf("[Result too large (%d characters). Full content saved for recovery.]\nArtifact: %s\nUse read_file with the artifact path when omitted evidence is needed.\n\n", len(contextual), path)
	limit := projectionPreviewBytes
	if threshold > 0 && threshold < limit {
		limit = threshold
	}
	// The recovery index is mandatory even under an unusually small configured
	// budget. Keep a small evidence allowance instead of truncating the path.
	if limit < len(header)+256 {
		limit = len(header) + 256
	}
	remaining := limit - len(header)
	sampled := sampleResultLines(contextual, projectionPreviewLines)
	middle := "\n\n--- omitted; see artifact ---\n\n"
	evidenceBudget := remaining - len(middle)
	if evidenceBudget < 0 {
		evidenceBudget = 0
	}
	head := evidenceBudget * 2 / 3
	tail := evidenceBudget - head
	body := stringutil.HeadTail(sampled, head, tail, middle)
	return header + body
}

func sampleResultLines(text string, maximum int) string {
	if maximum <= 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= maximum {
		return text
	}
	head := (maximum + 1) / 2
	tail := maximum / 2
	return strings.Join(lines[:head], "\n") +
		"\n\n--- omitted lines; see artifact ---\n\n" +
		strings.Join(lines[len(lines)-tail:], "\n")
}

func buildStructuredResultIndex(path string, raw json.RawMessage, originalCharacters int) string {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return ""
	}
	index := map[string]any{
		"kind":                "archived_structured_tool_result",
		"artifact_ref":        path,
		"original_characters": originalCharacters,
		"instruction":         "Use read_file with artifact_ref to inspect the complete result.",
	}
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		index["shape"] = "object"
		index["key_count"] = len(keys)
		if len(keys) > 64 {
			index["keys"] = keys[:64]
			index["keys_omitted"] = len(keys) - 64
		} else {
			index["keys"] = keys
		}
	case []any:
		index["shape"] = "array"
		index["item_count"] = len(typed)
	default:
		index["shape"] = fmt.Sprintf("%T", typed)
	}
	encoded, err := json.Marshal(index)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// persistResult writes content to the session artifact directory
// and returns the absolute path.
func persistResult(sessionDir, callID, content string) (string, error) {
	dir := filepath.Join(sessionDir, "tool-results")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, callID+".txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
