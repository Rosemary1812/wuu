package context

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const defaultRecentDiffMaxFiles = 40

type RecentDiffOptions struct {
	MaxFiles int
}

// RecentDiffBlock returns a compact dirty-worktree summary for per-turn typed
// context. It intentionally omits diff bodies; agents should call git diff or
// read_file when they need exact evidence.
func RecentDiffBlock(root string, opts RecentDiffOptions) (Block, bool) {
	root = strings.TrimSpace(root)
	if root == "" {
		return Block{}, false
	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = defaultRecentDiffMaxFiles
	}

	statusLines, err := gitStatusLines(root)
	if err != nil || len(statusLines) == 0 {
		return Block{}, false
	}

	revision := gitShortRevision(root)
	unstagedShortstat := gitShortstat(root, false)
	stagedShortstat := gitShortstat(root, true)

	listed := statusLines
	truncated := false
	if len(listed) > opts.MaxFiles {
		listed = listed[:opts.MaxFiles]
		truncated = true
	}

	var b strings.Builder
	if revision != "" {
		fmt.Fprintf(&b, "base_revision: %s\n", revision)
	}
	fmt.Fprintf(&b, "changed_files: %d\n", len(statusLines))
	if truncated {
		b.WriteString("truncated: true\n")
	}
	if stagedShortstat != "" {
		fmt.Fprintf(&b, "staged_shortstat: %s\n", stagedShortstat)
	}
	if unstagedShortstat != "" {
		fmt.Fprintf(&b, "unstaged_shortstat: %s\n", unstagedShortstat)
	}
	b.WriteString("files:\n")
	for _, line := range listed {
		fmt.Fprintf(&b, "- %s\n", formatRecentDiffStatusLine(line))
	}
	if omitted := len(statusLines) - len(listed); omitted > 0 {
		fmt.Fprintf(&b, "omitted_files: %d\n", omitted)
	}
	b.WriteString("note: diff bodies are omitted; use git diff or read_file for exact evidence.\n")

	return Block{
		Kind:        BlockRecentDiff,
		Title:       "Current workspace diff summary",
		Source:      "runtime.recent_diff",
		TokenBudget: 700,
		Content:     strings.TrimRight(b.String(), "\n"),
	}, true
}

func gitStatusLines(root string) ([]string, error) {
	cmd := exec.Command("git", "status", "--porcelain", "--short")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	raw := strings.Split(trimmed, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

func gitShortRevision(root string) string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitShortstat(root string, staged bool) string {
	args := []string{"diff", "--shortstat"}
	if staged {
		args = append(args, "--cached")
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(strings.TrimSpace(string(out))), " ")
}

func formatRecentDiffStatusLine(line string) string {
	line = strings.TrimSpace(line)
	if len(line) <= 3 {
		return redactRecentDiffPath(line)
	}
	status := strings.TrimSpace(line[:2])
	path := strings.TrimSpace(line[2:])
	if status == "" {
		return redactRecentDiffPath(path)
	}
	return status + " " + redactRecentDiffPath(path)
}

func redactRecentDiffPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	parts := strings.Split(path, " -> ")
	redacted := make([]string, 0, len(parts))
	changed := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if recentDiffSensitivePath(part) {
			redacted = append(redacted, "[REDACTED sensitive path]")
			changed = true
			continue
		}
		redacted = append(redacted, part)
	}
	out := strings.Join(redacted, " -> ")
	if changed {
		return out
	}
	return path
}

func recentDiffSensitivePath(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	base := filepath.Base(lower)
	if lower == "" {
		return false
	}
	if strings.HasPrefix(base, ".env") || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") {
		return true
	}
	for _, marker := range []string{"secret", "token", "credential", "private_key", "id_rsa", "id_ed25519"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
