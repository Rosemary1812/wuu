package tools

import (
	"path/filepath"
	"strings"
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
