// Package context provides per-turn dynamic context injection for the
// agent loop. It generates environment information (CWD, date, git
// status) that gets injected as <system-reminder> blocks in user
// messages, keeping the system prompt stable for prompt caching.
//
// Design aligned with Claude Code's getSystemContext() +
// getUserContext() dual-path injection architecture:
//   - System prompt = static role, rules, instructions (cacheable)
//   - User context = dynamic environment info, memory, skills (per-turn)
package context

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SystemReminderMessageName marks internal per-step environment context
// injections so callers can keep them out of persisted chat history.
const SystemReminderMessageName = "wuu_system_reminder"

// EnvInfo holds the dynamic environment snapshot for one turn.
type EnvInfo struct {
	CWD       string
	Date      string
	GitBranch string
	GitStatus string // short summary, not full porcelain
}

type BlockKind string

const (
	BlockSystemContract    BlockKind = "SYSTEM_CONTRACT"
	BlockProjectRules      BlockKind = "PROJECT_RULES"
	BlockTask              BlockKind = "TASK"
	BlockWorkflowState     BlockKind = "WORKFLOW_STATE"
	BlockRepoMap           BlockKind = "REPO_MAP"
	BlockActiveFiles       BlockKind = "ACTIVE_FILES"
	BlockRecentDiff        BlockKind = "RECENT_DIFF"
	BlockTestFailures      BlockKind = "TEST_FAILURES"
	BlockWebEvidence       BlockKind = "WEB_EVIDENCE"
	BlockMemory            BlockKind = "MEMORY"
	BlockToolResultSummary BlockKind = "TOOL_RESULT_SUMMARY"
	BlockEnvironment       BlockKind = "ENVIRONMENT"
	BlockAdditionalContext BlockKind = "ADDITIONAL_CONTEXT"
)

type Block struct {
	Kind        BlockKind
	Title       string
	Source      string
	Content     string
	TokenBudget int
}

type Compiler struct {
	Blocks []Block
}

// defaultSnapshotCacheTTL is the time-to-live for cached git snapshots.
// In a tight multi-step tool loop this avoids spawning git subprocesses on
// every model round (~20 ms saved per hit). 2 s is short enough that a user
// running shell commands between turns will see fresh state, long enough to
// amortize the cost across the typical 3–8 rounds in one turn.
const defaultSnapshotCacheTTL = 2 * time.Second

// snapshotCache holds the most recent EnvInfo so that repeated Snapshot
// calls inside the same turn can reuse it without shelling out to git.
var snapshotCache struct {
	mu       sync.RWMutex
	info     EnvInfo
	captured time.Time
	ttl      time.Duration
}

func snapshotCacheTTL() time.Duration {
	if v := os.Getenv("WUU_CONTEXT_CACHE_TTL_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return defaultSnapshotCacheTTL
}

// Snapshot captures the current environment state. Safe to call from
// any goroutine; all data comes from the OS / git CLI.
//
// Results are cached per-CWD for a short TTL (default 2 s) so that a single
// user turn that triggers multiple model rounds only pays the git overhead
// once.
func Snapshot(cwd string) EnvInfo {
	ttl := snapshotCacheTTL()
	if ttl <= 0 {
		return snapshotFresh(cwd)
	}

	snapshotCache.mu.RLock()
	if !snapshotCache.captured.IsZero() &&
		time.Since(snapshotCache.captured) < ttl &&
		snapshotCache.info.CWD == cwd {
		info := snapshotCache.info
		snapshotCache.mu.RUnlock()
		// Date is cheap to refresh and may have rolled over since caching.
		info.Date = time.Now().Format("2006-01-02")
		return info
	}
	snapshotCache.mu.RUnlock()

	info := snapshotFresh(cwd)

	snapshotCache.mu.Lock()
	snapshotCache.info = info
	snapshotCache.captured = time.Now()
	snapshotCache.ttl = ttl
	snapshotCache.mu.Unlock()

	return info
}

func snapshotFresh(cwd string) EnvInfo {
	info := EnvInfo{
		CWD:  cwd,
		Date: time.Now().Format("2006-01-02"),
	}
	if branch, err := gitBranch(cwd); err == nil {
		info.GitBranch = branch
	}
	if status, err := gitStatusSummary(cwd); err == nil {
		info.GitStatus = status
	}
	return info
}

func (c Compiler) Compile() string {
	return CompileBlocks(c.Blocks)
}

func CompileBlocks(blocks []Block) string {
	var b strings.Builder
	for _, block := range blocks {
		rendered := renderBlock(block)
		if rendered == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(rendered)
	}
	return b.String()
}

func renderBlock(block Block) string {
	content := strings.TrimSpace(block.Content)
	if content == "" {
		return ""
	}
	kind := block.Kind
	if strings.TrimSpace(string(kind)) == "" {
		kind = BlockAdditionalContext
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[%s]\n", kind)
	if title := strings.TrimSpace(block.Title); title != "" {
		fmt.Fprintf(&b, "title: %s\n", title)
	}
	if source := strings.TrimSpace(block.Source); source != "" {
		fmt.Fprintf(&b, "source: %s\n", source)
	}
	if block.TokenBudget > 0 {
		fmt.Fprintf(&b, "token_budget: %d\n", block.TokenBudget)
	}
	if b.Len() > len(kind)+3 {
		b.WriteString("\n")
	}
	b.WriteString(content)
	return strings.TrimRight(b.String(), "\n")
}

func EnvironmentBlock(env EnvInfo) Block {
	var b strings.Builder
	b.WriteString("# Environment\n")
	b.WriteString(fmt.Sprintf("- CWD: %s\n", env.CWD))
	b.WriteString(fmt.Sprintf("- Date: %s\n", env.Date))
	if env.GitBranch != "" {
		b.WriteString(fmt.Sprintf("- Git branch: %s\n", env.GitBranch))
	}
	if env.GitStatus != "" {
		b.WriteString(fmt.Sprintf("- Git status: %s\n", env.GitStatus))
	}
	return Block{
		Kind:    BlockEnvironment,
		Title:   "Runtime environment",
		Source:  "runtime.snapshot",
		Content: strings.TrimRight(b.String(), "\n"),
	}
}

// FormatSystemReminder formats environment info and optional extra
// context sections (memory, skills) into a <system-reminder> block
// suitable for injection into a user message.
func FormatSystemReminder(env EnvInfo, sections ...string) string {
	blocks := []Block{EnvironmentBlock(env)}
	for i, sec := range sections {
		sec = strings.TrimSpace(sec)
		if sec != "" {
			blocks = append(blocks, Block{
				Kind:    BlockAdditionalContext,
				Title:   fmt.Sprintf("Additional context %d", i+1),
				Source:  "runtime.injector",
				Content: sec,
			})
		}
	}

	return "<system-reminder>\n" + CompileBlocks(blocks) + "\n</system-reminder>"
}

// IsSystemReminder reports whether the given metadata/content belongs to an
// internal system-reminder block rather than a durable conversation turn.
func IsSystemReminder(name, content string) bool {
	if strings.TrimSpace(name) == SystemReminderMessageName {
		return true
	}
	trimmed := strings.TrimSpace(content)
	return strings.HasPrefix(trimmed, "<system-reminder>") &&
		strings.HasSuffix(trimmed, "</system-reminder>")
}

// ── git helpers ────────────────────────────────────────────────────

func gitBranch(cwd string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return "(detached)", nil
	}
	return branch, nil
}

func gitStatusSummary(cwd string) (string, error) {
	cmd := exec.Command("git", "status", "--porcelain", "--short")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return "clean", nil
	}
	if len(lines) > 10 {
		return fmt.Sprintf("%d changed files", len(lines)), nil
	}
	// For small diffs, show the summary
	modified, added, deleted, other := 0, 0, 0, 0
	for _, line := range lines {
		if len(line) < 2 {
			continue
		}
		switch {
		case line[0] == '?' || line[1] == '?':
			added++
		case line[0] == 'M' || line[1] == 'M':
			modified++
		case line[0] == 'D' || line[1] == 'D':
			deleted++
		case line[0] == 'A' || line[1] == 'A':
			added++
		default:
			other++
		}
	}
	var parts []string
	if modified > 0 {
		parts = append(parts, fmt.Sprintf("%d modified", modified))
	}
	if added > 0 {
		parts = append(parts, fmt.Sprintf("%d added", added))
	}
	if deleted > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted", deleted))
	}
	if other > 0 {
		parts = append(parts, fmt.Sprintf("%d other", other))
	}
	if len(parts) == 0 {
		return "clean", nil
	}
	return strings.Join(parts, ", "), nil
}
