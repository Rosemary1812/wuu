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
		case part == "id_rsa" || part == "id_ed25519" || part == "id_ecdsa":
			return "SSH private key", true
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

// wuuCredentialFileNames are the app's own credential files at the root of
// the wuu home directory. They are floor-protected in every permission
// mode, including unconfined: no agent tool may read or write them. The
// agent never needs their contents to do its job, and the runtime-metadata
// exemption below exists for the memory notebook and session artifacts —
// not for these files.
var wuuCredentialFileNames = map[string]struct{}{
	"auth.json":        {},
	"credentials.json": {},
	"remote.json":      {},
	"phone.json":       {},
}

// isWuuCredentialPath reports whether absPath is one of the app's own
// credential files directly under the wuu home directory.
func isWuuCredentialPath(absPath string) bool {
	if strings.TrimSpace(absPath) == "" {
		return false
	}
	if _, ok := wuuCredentialFileNames[filepath.Base(filepath.Clean(absPath))]; !ok {
		return false
	}
	home, err := statepath.Home("")
	if err != nil {
		return false
	}
	return filepath.Dir(filepath.Clean(absPath)) == filepath.Clean(home)
}

func wuuCredentialRefusal(toolName, action, absPath string) error {
	return fmt.Errorf("%s refuses to %s wuu credential file %q: it stores the app's own login credentials and is never accessible to the agent in any permission mode. Ask the user to manage it outside the session", toolName, action, absPath)
}

// redactSensitiveReadContent masks credential values in content read from a
// sensitive path while unconfined. Confined modes never reach this helper:
// the read itself is refused by rejectSensitiveReadPath instead.
func redactSensitiveReadContent(env *Env, absPath, content string) string {
	if content == "" || env == nil || !env.BypassToolHardProtections() {
		return content
	}
	if _, ok := sensitivePathReason(env.NormalizeDisplayPath(absPath)); !ok {
		return content
	}
	return redactToolOutput(content)
}

func rejectSensitiveReadPath(env *Env, toolName, absPath string) error {
	if isWuuCredentialPath(absPath) {
		return wuuCredentialRefusal(toolName, "read", absPath)
	}
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
	if isWuuCredentialPath(absPath) {
		return wuuCredentialRefusal(toolName, action, absPath)
	}
	// Sensitive-path writes stay blocked in every mode, including
	// unconfined: lifting the path boundary does not lift secret guards.
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
