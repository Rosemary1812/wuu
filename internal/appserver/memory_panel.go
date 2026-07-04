package appserver

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/memdir"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
)

// Memory panel backend (memory-redesign contract §8): scope resolution and
// the no-LLM memory/read RPC. The two panel agents (memory/overview and
// memory/chat) live in memory_panel_agent.go.

// memoryNotebook resolves a memory-panel RPC scope to the notebook
// directory it targets. "user" is the user notebook; "participant" requires
// participant_id naming an active (named, non-retired) participant and
// yields that agent's identity notebook. Everything else is rejected — the
// scope whitelist is the security boundary for which directories the panel
// can ever touch.
func (s *Server) memoryNotebook(scope, participantID string) (string, error) {
	wuuHome := strings.TrimSpace(s.rt.WuuHome)
	if wuuHome == "" {
		return "", errors.New("wuu home directory is not configured")
	}
	id := strings.TrimSpace(participantID)
	switch scope {
	case MemoryScopeUser:
		if id != "" {
			return "", errors.New(`participant_id is only valid with scope "participant"`)
		}
		return memdir.UserMemdir(wuuHome), nil
	case MemoryScopeParticipant:
		if id == "" {
			return "", errors.New(`participant_id is required with scope "participant"`)
		}
		p, err := session.GetParticipant(s.rt.SessionDir, id)
		if err != nil {
			return "", err
		}
		if p.Kind != participant.KindNamed {
			return "", fmt.Errorf("participant %q is not a named agent (kind=%s)", id, p.Kind)
		}
		if p.RetiredAt != nil {
			return "", fmt.Errorf("participant %q is retired; its memory notebook is archived", id)
		}
		return memdir.ParticipantMemdir(wuuHome, id), nil
	default:
		return "", fmt.Errorf("unknown memory scope %q (allowed: %q, %q)", scope, MemoryScopeUser, MemoryScopeParticipant)
	}
}

// handleMemoryRead serves the panel's "查看原文" audit view: the raw
// MEMORY.md index text plus a frontmatter inventory of every topic file. No
// LLM is involved and no sanitization is applied — the audit view must show
// the true bytes on disk.
func (s *Server) handleMemoryRead(req Request) error {
	var params MemoryReadParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	dir, err := s.memoryNotebook(params.Scope, params.ParticipantID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	result, err := readMemoryNotebook(dir)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, result, nil)
}

// readMemoryNotebook builds the MemoryReadResult for one notebook
// directory. A missing directory (notebook never written to) is not an
// error: it yields an empty index and inventory.
func readMemoryNotebook(dir string) (MemoryReadResult, error) {
	result := MemoryReadResult{Files: []MemoryFileInfo{}}
	raw, err := os.ReadFile(filepath.Join(dir, memdir.EntrypointName))
	if err == nil {
		result.IndexMD = string(raw)
	} else if !os.IsNotExist(err) {
		return MemoryReadResult{}, fmt.Errorf("read %s: %w", memdir.EntrypointName, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return MemoryReadResult{}, fmt.Errorf("read notebook directory: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() ||
			strings.HasPrefix(name, ".") ||
			!strings.HasSuffix(strings.ToLower(name), ".md") ||
			strings.EqualFold(name, memdir.EntrypointName) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		fm := readTopicFrontmatter(filepath.Join(dir, name))
		if fm.name == "" {
			fm.name = strings.TrimSuffix(name, filepath.Ext(name))
		}
		result.Files = append(result.Files, MemoryFileInfo{
			Name:        fm.name,
			Description: fm.description,
			Type:        fm.memType,
			Mtime:       info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Name < result.Files[j].Name })
	return result, nil
}

// topicFrontmatter is the subset of a topic file's frontmatter the panel
// inventory surfaces (memory-redesign contract §2).
type topicFrontmatter struct {
	name        string
	description string
	memType     string
}

// readTopicFrontmatter extracts name/description/type from a topic file's
// leading `---` frontmatter block. Missing files, missing frontmatter, and
// missing keys all degrade to empty fields — the inventory never fails on a
// malformed topic file.
func readTopicFrontmatter(path string) topicFrontmatter {
	var fm topicFrontmatter
	raw, err := os.ReadFile(path)
	if err != nil {
		return fm
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fm
	}
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			break
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "name":
			fm.name = value
		case "description":
			fm.description = value
		case "type":
			fm.memType = value
		}
	}
	return fm
}

// memoryFileStamp is the change-detection fingerprint for one notebook file.
type memoryFileStamp struct {
	mtime time.Time
	size  int64
}

// snapshotMemoryRoots records (path → mtime,size) for every regular file
// under the given notebook roots. Taken before and after a memory/chat run,
// the two snapshots are diffed into the changed_files list.
func snapshotMemoryRoots(roots []string) map[string]memoryFileStamp {
	snapshot := make(map[string]memoryFileStamp)
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr // best-effort: unreadable entries are simply absent
			}
			info, err := d.Info()
			if err != nil {
				return nil //nolint:nilerr
			}
			snapshot[path] = memoryFileStamp{mtime: info.ModTime(), size: info.Size()}
			return nil
		})
	}
	return snapshot
}

// diffMemorySnapshots computes the changed_files list between two notebook
// snapshots. Paths are reported relative to wuuHome (e.g. "memory/topic.md",
// "participants/<id>/memory/MEMORY.md") with forward slashes; files outside
// wuuHome fall back to their absolute path. The result is sorted by path and
// never nil, so the wire shape is always a JSON array.
func diffMemorySnapshots(before, after map[string]memoryFileStamp, wuuHome string) []MemoryChangedFile {
	changed := []MemoryChangedFile{}
	for path, stamp := range after {
		prev, ok := before[path]
		switch {
		case !ok:
			changed = append(changed, MemoryChangedFile{Path: memoryDisplayPath(wuuHome, path), Action: "created"})
		case !prev.mtime.Equal(stamp.mtime) || prev.size != stamp.size:
			changed = append(changed, MemoryChangedFile{Path: memoryDisplayPath(wuuHome, path), Action: "modified"})
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			changed = append(changed, MemoryChangedFile{Path: memoryDisplayPath(wuuHome, path), Action: "deleted"})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Path < changed[j].Path })
	return changed
}

func memoryDisplayPath(wuuHome, path string) string {
	wuuHome = strings.TrimSpace(wuuHome)
	if wuuHome != "" {
		if rel, err := filepath.Rel(wuuHome, path); err == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}
