package memdir

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// One-time migration from the three retired memory surfaces
// (memory-redesign contract §7):
//
//  1. ~/.wuu/memory/entries.jsonl (the retired memstore.FileProvider log)
//     — each live entry becomes a topic file + index line in the user
//     notebook; the store-template MEMORY.md is replaced by the new index.
//  2. ~/.wuu/participants/<id>/MEMORY.md (legacy named-agent memory) — content
//     moves into <id>/memory/legacy-profile.md + an index line.
//  3. ~/.wuu/agents/<id>/home/MEMORY.md (orphan files older named-agent prompts
//     misdirected) — merged into that agent's identity notebook
//     as legacy-home-notes.md.
//
// Every consumed source file is renamed with a ".migrated" suffix. The whole
// migration is idempotent: a marker file in the user notebook short-circuits
// subsequent runs, and each step also tolerates re-execution (topic writes
// overwrite, index appends deduplicate) in case an earlier run failed after
// partial progress. Empty sources are consumed without generating files.

const (
	migratedMarkerName = ".memdir-migrated"
	migratedSuffix     = ".migrated"

	legacyEntriesFile      = "entries.jsonl"
	legacyProfileTopic     = "legacy-profile.md"
	legacyHomeNotesTopic   = "legacy-home-notes.md"
	legacyStoreTemplateTag = "# Profile Memory"
)

// Migrate converts the retired memory surfaces under wuuHome into notebook
// topic files and index lines. Safe to call on every session start; after
// the first successful run it is a single stat() no-op.
func Migrate(wuuHome string) error {
	home := strings.TrimSpace(wuuHome)
	if home == "" {
		return nil
	}
	userDir := UserMemdir(home)
	marker := filepath.Join(userDir, migratedMarkerName)
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	if err := EnsureDir(userDir); err != nil {
		return fmt.Errorf("memdir: migrate: %w", err)
	}

	var errs []error
	if err := migrateUserEntries(userDir); err != nil {
		errs = append(errs, err)
	}
	if err := migrateFlatFiles(filepath.Join(home, "participants"), home, "", legacyProfileTopic,
		"Legacy profile notes", "notes migrated from the retired flat MEMORY.md"); err != nil {
		errs = append(errs, err)
	}
	if err := migrateFlatFiles(filepath.Join(home, "agents"), home, "home", legacyHomeNotesTopic,
		"Legacy home notes", "notes migrated from the agent home MEMORY.md"); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		// Leave the marker unwritten so the next start retries; each step is
		// individually re-runnable.
		return fmt.Errorf("memdir: migrate: %w", errors.Join(errs...))
	}
	if err := os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		return fmt.Errorf("memdir: migrate: write marker: %w", err)
	}
	return nil
}

// legacyLogRecord mirrors the retired memstore JSONL line shape.
type legacyLogRecord struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Deleted   bool      `json:"_deleted,omitempty"`
}

func migrateUserEntries(userDir string) error {
	logPath := filepath.Join(userDir, legacyEntriesFile)
	raw, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", legacyEntriesFile, err)
	}

	// Replay the log with the retired store's semantics: last record per ID
	// wins, a tombstone permanently deletes the ID, malformed lines are
	// skipped.
	entries := map[string]legacyLogRecord{}
	tombstones := map[string]struct{}{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec legacyLogRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil || rec.ID == "" {
			continue
		}
		entries[rec.ID] = rec
		if rec.Deleted {
			tombstones[rec.ID] = struct{}{}
		}
	}
	live := make([]legacyLogRecord, 0, len(entries))
	for id, rec := range entries {
		if _, gone := tombstones[id]; gone {
			continue
		}
		if strings.TrimSpace(rec.Content) == "" {
			continue // empty content must not become a garbage topic file
		}
		live = append(live, rec)
	}
	sort.Slice(live, func(i, j int) bool {
		if !live[i].CreatedAt.Equal(live[j].CreatedAt) {
			return live[i].CreatedAt.Before(live[j].CreatedAt)
		}
		return live[i].ID < live[j].ID
	})

	var indexLines []string
	used := map[string]struct{}{
		strings.ToLower(EntrypointName): {},
	}
	for _, rec := range live {
		content := strings.TrimSpace(rec.Content)
		name := uniqueTopicName(topicSlug(content), used)
		memType := "lesson"
		for _, tag := range rec.Tags {
			if strings.TrimSpace(tag) == "target:user" {
				memType = "user"
				break
			}
		}
		if err := writeTopicFile(userDir, name+".md", name, topicHook(content), memType, content); err != nil {
			return err
		}
		indexLines = append(indexLines, indexLine(topicTitle(content), name+".md", topicHook(content)))
	}

	// The retired store kept a rendered "# Profile Memory" template at
	// MEMORY.md; the new index replaces it wholesale. A hand-written
	// MEMORY.md (no template header) is preserved and appended to.
	entrypoint := filepath.Join(userDir, EntrypointName)
	if existing, err := os.ReadFile(entrypoint); err == nil && strings.HasPrefix(strings.TrimSpace(string(existing)), legacyStoreTemplateTag) {
		if err := os.WriteFile(entrypoint, []byte(joinIndexLines(indexLines)), 0o644); err != nil {
			return fmt.Errorf("rewrite %s: %w", EntrypointName, err)
		}
	} else if err := appendIndexLines(userDir, indexLines); err != nil {
		return err
	}
	return retireSource(logPath)
}

// migrateFlatFiles converts <root>/<id>[/subdir]/MEMORY.md flat files into
// the matching identity notebook (ParticipantMemdir(home, id)) as one topic
// file + index line each.
func migrateFlatFiles(root, home, subdir, topicFile, title, hook string) error {
	dirents, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", root, err)
	}
	var errs []error
	for _, d := range dirents {
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		id := d.Name()
		flat := filepath.Join(root, id)
		if subdir != "" {
			flat = filepath.Join(flat, subdir)
		}
		flat = filepath.Join(flat, EntrypointName)
		raw, err := os.ReadFile(flat)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			errs = append(errs, fmt.Errorf("read %s: %w", flat, err))
			continue
		}
		content := strings.TrimSpace(string(raw))
		if content != "" {
			notebook := ParticipantMemdir(home, id)
			if err := EnsureDir(notebook); err != nil {
				errs = append(errs, err)
				continue
			}
			name := strings.TrimSuffix(topicFile, ".md")
			if err := writeTopicFile(notebook, topicFile, name, hook, "lesson", content); err != nil {
				errs = append(errs, err)
				continue
			}
			if err := appendIndexLines(notebook, []string{indexLine(title, topicFile, hook)}); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if err := retireSource(flat); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func writeTopicFile(dir, file, name, description, memType, body string) error {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", name)
	fmt.Fprintf(&b, "description: %s\n", description)
	fmt.Fprintf(&b, "type: %s\n", memType)
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")
	if err := os.WriteFile(filepath.Join(dir, file), []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write topic file %s: %w", file, err)
	}
	return nil
}

// appendIndexLines appends the given index lines to dir/MEMORY.md, skipping
// lines the index already contains so re-runs stay idempotent.
func appendIndexLines(dir string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	path := filepath.Join(dir, EntrypointName)
	existing := ""
	if raw, err := os.ReadFile(path); err == nil {
		existing = string(raw)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", EntrypointName, err)
	}
	out := strings.TrimRight(existing, "\n")
	added := false
	for _, line := range lines {
		if strings.Contains(existing, line) {
			continue
		}
		if out != "" {
			out += "\n"
		}
		out += line
		added = true
	}
	if !added {
		return nil
	}
	if err := os.WriteFile(path, []byte(out+"\n"), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", EntrypointName, err)
	}
	return nil
}

func joinIndexLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func retireSource(path string) error {
	if err := os.Rename(path, path+migratedSuffix); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}

func indexLine(title, file, hook string) string {
	line := fmt.Sprintf("- [%s](%s)", title, file)
	if hook != "" {
		line += " — " + hook
	}
	return truncateRunes(line, 150)
}

// topicSlug derives a kebab-case file name from the first words of content.
func topicSlug(content string) string {
	words := strings.Fields(strings.ToLower(content))
	if len(words) > 5 {
		words = words[:5]
	}
	var b strings.Builder
	for _, w := range words {
		var cleaned strings.Builder
		for _, r := range w {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				cleaned.WriteRune(r)
			}
		}
		if cleaned.Len() == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('-')
		}
		b.WriteString(cleaned.String())
	}
	slug := b.String()
	if len(slug) > 48 {
		slug = strings.Trim(slug[:48], "-")
	}
	if slug == "" {
		slug = "legacy-memory"
	}
	return slug
}

func uniqueTopicName(base string, used map[string]struct{}) string {
	name := base
	for i := 2; ; i++ {
		if _, taken := used[strings.ToLower(name+".md")]; !taken {
			break
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
	used[strings.ToLower(name+".md")] = struct{}{}
	return name
}

func topicTitle(content string) string {
	title := strings.Join(strings.Fields(content), " ")
	return truncateRunes(title, 60)
}

func topicHook(content string) string {
	hook := strings.Join(strings.Fields(content), " ")
	return truncateRunes(hook, 100)
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}
