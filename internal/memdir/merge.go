package memdir

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MergeResult reports what a MergeNotebook call did, for logging and tests.
type MergeResult struct {
	// TopicsAdded counts src topic files written into dst as new memories.
	TopicsAdded int
	// TopicsSkipped counts src topics already present in dst (byte-identical
	// content) — the common case when merging a fork whose snapshot still
	// holds the母体's unchanged topics.
	TopicsSkipped int
	// Conflicts counts src topics whose file name already existed in dst with
	// DIFFERENT content (both sides edited the same topic). The母体's version
	// stays authoritative; the fork's copy is preserved under a "-fork" name
	// so nothing is lost. These are the only real conflicts (decision six:
	// concurrent replace/remove of the same pre-existing topic) and the
	// reason merge-back must be serialized per母体 by the caller.
	Conflicts int
}

// MergeNotebook append-merges the memory notebook at srcDir into the notebook
// at dstDir: the union of their topic files, de-duplicated by content, plus
// the corresponding index lines in dst/MEMORY.md. It is the memory回流 step of
// a named-agent fork (decision six): a fork accumulates its own topic files
// while it runs, and on retire those new topics are merged back into the母体.
//
// Semantics (name-centric, append-first, per internal/memory add/replace/remove
// being append-dominant):
//
//   - A src topic whose content is byte-identical to any dst topic is skipped
//     (union de-dup). A fork's snapshot carries all of the母体's topics
//     unchanged, so this is what keeps the merge from duplicating them.
//   - A src topic whose file name is free in dst (or unused) is written as a
//     new topic + index line.
//   - A src topic whose file name already exists in dst with different content
//     is the sole real conflict (both sides touched the same topic). The dst
//     (母体) version is kept authoritative; the src (fork) version is written
//     under a unique "<name>-fork" file so no experience is dropped.
//
// MergeNotebook performs no locking. Concurrent merges into the same母体 must
// be serialized by the caller (the app-server holds a per-母体 merge lock) so
// the same-topic conflict path is deterministic. A missing srcDir is not an
// error (nothing to merge).
func MergeNotebook(srcDir, dstDir string) (MergeResult, error) {
	srcDir = strings.TrimSpace(srcDir)
	dstDir = strings.TrimSpace(dstDir)
	var result MergeResult
	if srcDir == "" || dstDir == "" {
		return result, nil
	}
	srcTopics, err := topicFiles(srcDir)
	if err != nil {
		return result, err
	}
	if len(srcTopics) == 0 {
		return result, nil
	}
	if err := EnsureDir(dstDir); err != nil {
		return result, fmt.Errorf("memdir: merge: %w", err)
	}

	// Index dst by lowercased base name and by trimmed content so we can
	// de-dup by content and detect same-name conflicts.
	dstByName := map[string]string{}     // lower(base) -> trimmed content
	dstContents := map[string]struct{}{} // trimmed content set
	dstTopics, err := topicFiles(dstDir)
	if err != nil {
		return result, err
	}
	for _, base := range dstTopics {
		raw, rerr := os.ReadFile(filepath.Join(dstDir, base))
		if rerr != nil {
			return result, fmt.Errorf("memdir: merge: read dst topic %s: %w", base, rerr)
		}
		trimmed := strings.TrimSpace(string(raw))
		dstByName[strings.ToLower(base)] = trimmed
		dstContents[trimmed] = struct{}{}
	}
	used := map[string]struct{}{strings.ToLower(EntrypointName): {}}
	for base := range dstByName {
		used[base] = struct{}{}
	}

	var indexLines []string
	for _, base := range srcTopics {
		raw, rerr := os.ReadFile(filepath.Join(srcDir, base))
		if rerr != nil {
			return result, fmt.Errorf("memdir: merge: read src topic %s: %w", base, rerr)
		}
		content := string(raw)
		trimmed := strings.TrimSpace(content)
		if trimmed == "" {
			continue
		}
		if _, dup := dstContents[trimmed]; dup {
			result.TopicsSkipped++
			continue // union de-dup: identical memory already in the母体
		}

		targetBase := base
		if existing, ok := dstByName[strings.ToLower(base)]; ok && existing != trimmed {
			// Same topic name, different content: keep the母体's version,
			// preserve the fork's under a unique name.
			stem := strings.TrimSuffix(base, ".md")
			targetBase = uniqueTopicName(stem+"-fork", used) + ".md"
			result.Conflicts++
		}

		if err := os.WriteFile(filepath.Join(dstDir, targetBase), []byte(content), 0o644); err != nil {
			return result, fmt.Errorf("memdir: merge: write topic %s: %w", targetBase, err)
		}
		dstByName[strings.ToLower(targetBase)] = trimmed
		dstContents[trimmed] = struct{}{}
		used[strings.ToLower(targetBase)] = struct{}{}
		result.TopicsAdded++

		title, hook := topicTitleHook(content, targetBase)
		indexLines = append(indexLines, indexLine(title, targetBase, hook))
	}

	if err := appendIndexLines(dstDir, indexLines); err != nil {
		return result, err
	}
	return result, nil
}

// topicFiles returns the sorted base names of the topic markdown files in a
// notebook directory (every *.md except the MEMORY.md index and dotfiles).
// A missing directory yields an empty list, not an error.
func topicFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("memdir: read notebook %s: %w", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		if strings.EqualFold(name, EntrypointName) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// topicTitleHook derives an index-line title and one-line hook for a topic
// file, preferring its frontmatter name/description and falling back to the
// file stem when the frontmatter is absent.
func topicTitleHook(content, base string) (string, string) {
	name, description := parseTopicFrontmatter(content)
	title := strings.TrimSpace(name)
	if title == "" {
		title = strings.TrimSuffix(base, ".md")
	}
	return title, strings.TrimSpace(description)
}

// parseTopicFrontmatter reads the leading `--- ... ---` YAML-ish header of a
// topic file and returns its name and description values. Missing keys yield
// empty strings.
func parseTopicFrontmatter(content string) (name, description string) {
	trimmed := strings.TrimLeft(content, "\ufeff \t\r\n")
	if !strings.HasPrefix(trimmed, "---") {
		return "", ""
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", ""
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(key)) {
		case "name":
			name = strings.TrimSpace(value)
		case "description":
			description = strings.TrimSpace(value)
		}
	}
	return name, description
}
