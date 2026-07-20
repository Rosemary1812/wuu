package tools

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/blueberrycongee/wuu/internal/statepath"
)

func sensitivePathReason(path string) (string, bool) {
	normalized := filepath.ToSlash(strings.TrimSpace(path))
	if normalized == "" {
		return "", false
	}
	lower := strings.ToLower(normalized)
	parts := strings.Split(lower, "/")
	for _, part := range parts {
		part = strings.Trim(part, `"'`)
		switch {
		case part == ".git" || part == ".hg" || part == ".svn":
			return "version-control metadata", true
		case part == ".wuu" || part == ".wuu-state" || part == ".wuu-home":
			return "wuu runtime state", true
		case part == ".env" || strings.HasPrefix(part, ".env.") || strings.Contains(part, ".env"):
			return ".env file", true
		case part == ".netrc":
			return ".netrc credentials", true
		case part == ".npmrc" || part == ".pypirc" || part == ".pgpass":
			return "credential configuration", true
		case strings.Contains(part, "credential") || strings.Contains(part, "secret"):
			return "credential or secret path", true
		case strings.Contains(part, "private") && strings.Contains(part, "key"):
			return "private key path", true
		}
	}
	return "", false
}

func isSensitivePath(path string) bool {
	_, ok := sensitivePathReason(path)
	return ok
}

// isAgentRuntimeMetadataPath reports whether the absolute path lives under
// the agent's own runtime metadata directory (statepath.Home, i.e. ~/.wuu
// or $WUU_HOME). These paths hold agent-owned state — the user memory
// notebook, session artifacts, runtime caches — not user content.
//
// Treating them with the same rules as workspace files makes the agent
// forgetful across sessions, which is a product defect rather than a
// safety property. They are exempt from the sensitive-path gate and the
// workspace-root gate when the active boundary permits mutations.
// Read-only mode keeps the gate to preserve strict side-effect isolation.
func isAgentRuntimeMetadataPath(absPath string) bool {
	if strings.TrimSpace(absPath) == "" {
		return false
	}
	home, err := statepath.Home("")
	if err != nil {
		return false
	}
	runtimeDir := filepath.ToSlash(filepath.Clean(home))
	if runtimeDir == "" || runtimeDir == "." {
		return false
	}
	normalized := filepath.ToSlash(filepath.Clean(absPath))
	return normalized == runtimeDir ||
		strings.HasPrefix(normalized, runtimeDir+"/")
}

// isAgentMemoryNotebookPath reports whether an absolute path belongs to a
// user or named-agent memory notebook. Unlike the broader runtime metadata
// exemption, this excludes credentials, configuration, session artifacts,
// and caches stored elsewhere under WUU_HOME.
func isAgentMemoryNotebookPath(absPath string) bool {
	if strings.TrimSpace(absPath) == "" {
		return false
	}
	home, err := statepath.Home("")
	if err != nil {
		return false
	}
	if pathWithinRoot(filepath.Join(home, "memory"), absPath) {
		return true
	}

	participantsDir := filepath.Join(home, "participants")
	rel, err := filepath.Rel(participantsDir, absPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return len(parts) >= 3 && strings.TrimSpace(parts[0]) != "" && parts[1] == "memory"
}

func rejectSensitiveReadPath(env *Env, toolName, absPath string) error {
	if env.BypassToolHardProtections() {
		return nil
	}
	if env.AllowMutations && isAgentMemoryNotebookPath(absPath) {
		return nil
	}
	displayPath := env.NormalizeDisplayPath(absPath)
	if reason, ok := sensitivePathReason(displayPath); ok {
		return fmt.Errorf("%s refuses to read sensitive path %q (%s). Use a safer metadata command or ask the user for explicit secret handling", toolName, displayPath, reason)
	}
	return nil
}

func rejectSensitiveToolPath(env *Env, toolName, action, absPath string) error {
	if env.BypassToolHardProtections() {
		return nil
	}
	// Agent's own runtime metadata is allowed when the boundary permits
	// mutations. Read-only mode keeps the gate (env.AllowMutations == false).
	if env.AllowMutations && isAgentRuntimeMetadataPath(absPath) {
		return nil
	}
	displayPath := env.NormalizeDisplayPath(absPath)
	if reason, ok := sensitivePathReason(displayPath); ok {
		return fmt.Errorf("%s refuses to %s sensitive path %q (%s). Use dedicated metadata-safe tools or ask the user for explicit secret handling", toolName, action, displayPath, reason)
	}
	return nil
}
