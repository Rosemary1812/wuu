// Package memdir implements the file-directory long-term memory layout
// defined by docs/plans/2026-07-04-memory-redesign.md.
//
// A memory notebook is a plain directory: MEMORY.md is an INDEX (one line
// per memory: `- [Title](topic.md) — one-line hook`) and each memory lives
// in its own topic markdown file with a small frontmatter header. Only the
// index is ever injected into model context; writing memories happens
// through the ordinary file tools, guided by the teaching text built here.
// There are exactly two notebook kinds: the user notebook (one per wuu
// home) and one identity notebook per resident named agent.
package memdir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	// EntrypointName is the index file inside every notebook directory.
	EntrypointName = "MEMORY.md"
	// MaxIndexLines caps how many index lines are injected into context.
	MaxIndexLines = 200
	// MaxIndexBytes caps the injected index size; it catches long-line
	// indexes that slip past the line cap.
	MaxIndexBytes = 25_000

	// removedLineNotice replaces an index line that failed the injection-side
	// security scan (threat patterns or invisible unicode).
	removedLineNotice = "[memory line removed: security]"
)

// UserMemdir returns the user notebook directory (~/.wuu/memory).
func UserMemdir(wuuHome string) string {
	home := strings.TrimSpace(wuuHome)
	if home == "" {
		return ""
	}
	return filepath.Join(home, "memory")
}

// ParticipantMemdir returns the identity notebook directory for one resident
// named agent (~/.wuu/participants/<id>/memory). An empty participant id has
// no notebook and yields "".
func ParticipantMemdir(wuuHome, participantID string) string {
	home := strings.TrimSpace(wuuHome)
	id := strings.TrimSpace(participantID)
	if home == "" || id == "" {
		return ""
	}
	return filepath.Join(home, "participants", id, "memory")
}

// EnsureDir creates a notebook directory (with parents) if missing. The
// harness calls this before teaching the model that the directory "already
// exists — write to it directly".
func EnsureDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("memdir: directory path is required")
	}
	return os.MkdirAll(dir, 0o755)
}

// IndexSnapshot is the injection-ready view of a notebook's MEMORY.md.
type IndexSnapshot struct {
	// Content is the sanitized, truncated index text (with a trailing
	// truncation warning when a cap fired). Empty when the index file is
	// missing or blank.
	Content string
	// LineCount and ByteCount describe the sanitized content BEFORE
	// truncation, so the warning names the real size.
	LineCount int
	ByteCount int
	// LineTruncated / ByteTruncated report which cap fired.
	LineTruncated bool
	ByteTruncated bool
	// SecurityFiltered reports whether any line was replaced by the
	// injection-side security scan.
	SecurityFiltered bool
}

// ReadIndex reads dir/MEMORY.md and returns its injection-ready snapshot:
// each line is security-scanned (threat patterns and invisible unicode;
// offending lines are replaced with a removal notice) and the result is
// truncated to the line/byte caps with a warning naming the cap that fired.
// A missing index file is not an error — it yields an empty snapshot.
func ReadIndex(dir string) (IndexSnapshot, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return IndexSnapshot{}, nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, EntrypointName))
	if err != nil {
		if os.IsNotExist(err) {
			return IndexSnapshot{}, nil
		}
		return IndexSnapshot{}, fmt.Errorf("memdir: read index: %w", err)
	}
	sanitized, filtered := sanitizeIndexContent(string(raw))
	snapshot := truncateIndexContent(sanitized)
	snapshot.SecurityFiltered = filtered
	return snapshot, nil
}

// sanitizeIndexContent runs the injection-side security scan line by line,
// replacing every line that trips a threat pattern or carries invisible
// unicode with removedLineNotice. It reports whether any line was replaced.
func sanitizeIndexContent(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	lines := strings.Split(raw, "\n")
	filtered := false
	for i, line := range lines {
		if scanIndexLine(line) != nil {
			lines[i] = removedLineNotice
			filtered = true
		}
	}
	if !filtered {
		return raw, false
	}
	return strings.Join(lines, "\n"), true
}

// truncateIndexContent applies the line cap first (natural boundary), then
// the byte cap at the last newline before the limit so no line is cut in
// half. Ported from Claude Code's truncateEntrypointContent
// (thirdparty/claude-code-sourcemap/src/memdir/memdir.ts).
func truncateIndexContent(raw string) IndexSnapshot {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return IndexSnapshot{}
	}
	lines := strings.Split(trimmed, "\n")
	snapshot := IndexSnapshot{
		LineCount:     len(lines),
		ByteCount:     len(trimmed),
		LineTruncated: len(lines) > MaxIndexLines,
		ByteTruncated: len(trimmed) > MaxIndexBytes,
	}
	if !snapshot.LineTruncated && !snapshot.ByteTruncated {
		snapshot.Content = trimmed
		return snapshot
	}

	truncated := trimmed
	if snapshot.LineTruncated {
		truncated = strings.Join(lines[:MaxIndexLines], "\n")
	}
	if len(truncated) > MaxIndexBytes {
		if cut := strings.LastIndexByte(truncated[:MaxIndexBytes], '\n'); cut > 0 {
			truncated = truncated[:cut]
		} else {
			cut := MaxIndexBytes
			for cut > 0 && !utf8.RuneStart(truncated[cut]) {
				cut--
			}
			truncated = truncated[:cut]
		}
	}

	var reason string
	switch {
	case snapshot.ByteTruncated && !snapshot.LineTruncated:
		reason = fmt.Sprintf("%s (limit: %s) — index entries are too long", formatByteSize(snapshot.ByteCount), formatByteSize(MaxIndexBytes))
	case snapshot.LineTruncated && !snapshot.ByteTruncated:
		reason = fmt.Sprintf("%d lines (limit: %d)", snapshot.LineCount, MaxIndexLines)
	default:
		reason = fmt.Sprintf("%d lines and %s", snapshot.LineCount, formatByteSize(snapshot.ByteCount))
	}
	snapshot.Content = truncated + fmt.Sprintf(
		"\n\n> WARNING: %s is %s. Only part of it was loaded. Keep index entries to one line under ~200 chars; move detail into topic files.",
		EntrypointName, reason,
	)
	return snapshot
}

func formatByteSize(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	return fmt.Sprintf("%.1fKB", float64(n)/1024)
}
