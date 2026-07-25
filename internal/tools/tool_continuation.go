package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// validateStableOffset binds offset-based continuation to the exact sorted
// result set that produced the preceding page. Without that binding, insertions
// or removals before the cursor can silently repeat or skip records.
func validateStableOffset(tool string, offset int, expectedRevision, currentRevision string) error {
	if offset < 0 {
		return fmt.Errorf("%s offset must be non-negative", tool)
	}
	expectedRevision = strings.TrimSpace(expectedRevision)
	if offset > 0 && expectedRevision == "" {
		return fmt.Errorf("%s expected_revision is required when offset is non-zero", tool)
	}
	if expectedRevision != "" && expectedRevision != currentRevision {
		return fmt.Errorf("%s continuation is stale: result revision changed from %q to %q; restart at offset 0", tool, expectedRevision, currentRevision)
	}
	return nil
}

func continuationResultRevision(kind string, records any) string {
	encoded, _ := json.Marshal(records)
	hash := sha256.New()
	_, _ = hash.Write([]byte(kind))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(encoded)
	return "result:" + hex.EncodeToString(hash.Sum(nil))
}

func continuationSnapshotRevision(workspaceRevision, kind string, records any) string {
	resultRevision := continuationResultRevision(kind, records)
	if strings.TrimSpace(workspaceRevision) == "" {
		return resultRevision
	}
	return workspaceRevision + ";" + resultRevision
}

func continuationPage(offset, returned int, hasMore bool, revision string) map[string]any {
	page := map[string]any{
		"offset":         offset,
		"returned":       returned,
		"has_more":       hasMore,
		"snapshot_token": revision,
	}
	if hasMore && returned > 0 {
		page["next"] = map[string]any{
			"offset":            offset + returned,
			"expected_revision": revision,
		}
	}
	return page
}

func pageWindow[T any](records []T, offset, pageSize int) (page []T, hasMore bool) {
	if offset >= len(records) {
		return []T{}, false
	}
	end := offset + pageSize
	if end < offset || end > len(records) {
		end = len(records)
	}
	return records[offset:end], end < len(records)
}
