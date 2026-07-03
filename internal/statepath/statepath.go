package statepath

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	envHomeVar = "WUU_HOME"
)

// Home returns the user-level wuu state directory.
func Home(homeDir string) (string, error) {
	if override := strings.TrimSpace(os.Getenv(envHomeVar)); override != "" {
		return filepath.Abs(override)
	}

	home := strings.TrimSpace(homeDir)
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(home) == "" {
		return "", errors.New("home directory is unavailable")
	}
	return filepath.Join(home, ".wuu"), nil
}

// LogDir returns the user-level directory for runtime logs.
func LogDir(wuuHome string) string {
	return filepath.Join(wuuHome, "log")
}

// SessionsDir returns the user-level directory for conversation sessions.
func SessionsDir(wuuHome string) string {
	return filepath.Join(wuuHome, "sessions")
}

// UsageDataDir returns the user-level directory for usage reports and caches.
func UsageDataDir(wuuHome string) string {
	return filepath.Join(wuuHome, "usage-data")
}

// WorkspaceDir returns a stable user-level state directory for one workspace.
func WorkspaceDir(wuuHome, rootDir string) (string, error) {
	if strings.TrimSpace(wuuHome) == "" {
		return "", errors.New("wuu home is required")
	}
	if strings.TrimSpace(rootDir) == "" {
		return "", errors.New("workspace root is required")
	}
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return "", err
	}
	if ev, err := filepath.EvalSymlinks(abs); err == nil {
		abs = ev
	}

	sum := sha256.Sum256([]byte(abs))
	slug := sanitizeSlug(filepath.Base(abs))
	if slug == "" {
		slug = "workspace"
	}
	return filepath.Join(wuuHome, "workspaces", slug+"-"+hex.EncodeToString(sum[:])[:16]), nil
}

// AgentHomeDir returns the per-agent home directory used for direct-message
// threads with a named participant. The directory lives inside wuu's own state
// root so DM conversations are anchored to a stable location regardless of the
// project the user happens to have open. Callers are responsible for creating
// the directory with os.MkdirAll.
func AgentHomeDir(wuuHome, participantID string) string {
	id := strings.TrimSpace(participantID)
	if id == "" {
		id = "default"
	}
	return filepath.Join(wuuHome, "agents", id, "home")
}

// ProfileDir returns a stable user-level state directory for one agent profile.
func ProfileDir(wuuHome, agentName string) (string, error) {
	if strings.TrimSpace(wuuHome) == "" {
		return "", errors.New("wuu home is required")
	}
	name := strings.TrimSpace(agentName)
	if name == "" {
		name = "default"
	}
	sum := sha256.Sum256([]byte(name))
	slug := sanitizeSlug(name)
	if slug == "" {
		slug = "profile"
	}
	return filepath.Join(wuuHome, "profiles", slug+"-"+hex.EncodeToString(sum[:])[:16]), nil
}

// RuntimeDir returns the workspace-scoped process runtime directory.
func RuntimeDir(workspaceStateDir string) string {
	return filepath.Join(workspaceStateDir, "runtime")
}

// WorktreeRoot returns the workspace-scoped sub-agent worktree directory.
func WorktreeRoot(workspaceStateDir string) string {
	return filepath.Join(workspaceStateDir, "worktrees")
}

// GoalRoot returns the workspace-scoped durable goal state directory.
func GoalRoot(workspaceStateDir string) string {
	return filepath.Join(workspaceStateDir, "goals")
}

// GoalDir returns the workspace-scoped durable state directory for one goal.
func GoalDir(workspaceStateDir, goalID string) string {
	return filepath.Join(GoalRoot(workspaceStateDir), goalID)
}

// SharedDir returns the workspace-scoped shared agent directory.
func SharedDir(workspaceStateDir string) string {
	return filepath.Join(workspaceStateDir, "shared")
}

// SessionArtifactDir returns workspace-scoped artifacts for one conversation.
func SessionArtifactDir(workspaceStateDir, sessionID string) string {
	return filepath.Join(workspaceStateDir, "sessions", sessionID)
}

// ThreadGoalRuntimePath returns the thread-scoped Goal runtime state file.
func ThreadGoalRuntimePath(workspaceStateDir, sessionID string) string {
	return filepath.Join(SessionArtifactDir(workspaceStateDir, sessionID), "goal_runtime.json")
}

// ScheduledTasksPath returns the workspace-scoped durable scheduled task file.
func ScheduledTasksPath(workspaceStateDir string) string {
	return filepath.Join(workspaceStateDir, "scheduled_tasks.json")
}

// ScheduledTasksLockPath returns the workspace-scoped scheduled task lock file.
func ScheduledTasksLockPath(workspaceStateDir string) string {
	return filepath.Join(workspaceStateDir, "scheduled_tasks.lock")
}

// GlobalMemoryDir returns the user-level, single global directory for the
// LLM-writable durable memory. Unlike ProfileMemoryDir, this directory is
// not scoped to a named agent profile: every wuu session — default or
// named — shares the same long-term store. This matches the Claude Code
// convention of one durable memory document per user, with no
// profile-name dimension.
func GlobalMemoryDir(wuuHome string) string {
	return filepath.Join(wuuHome, "memory")
}

// ProfileMemoryDir returns the profile-scoped directory for durable memory.
// The file-backed store keeps an indexed log plus the human-facing MEMORY.md
// document inside this directory; the directory is created lazily by the store.
func ProfileMemoryDir(profileStateDir string) string {
	return filepath.Join(profileStateDir, "memory")
}

func sanitizeSlug(input string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range input {
		ok := r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' ||
			r == '.' || r == '_' || r == '-'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	s := strings.Trim(b.String(), ".-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], ".-")
	}
	return s
}
