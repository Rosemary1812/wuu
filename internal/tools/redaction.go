package tools

import "regexp"

var toolOutputRedactors = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|token|authorization|password|passwd|secret)\s*[:=]\s*["']?[^"'\s,;]+`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`),
	regexp.MustCompile(`\b[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
}

var (
	toolOutputKeyValueSep = regexp.MustCompile(`\s*[:=]\s*`)
	toolOutputBearer      = regexp.MustCompile(`(?i)^bearer\s+`)
)

func redactToolOutput(value string) string {
	if value == "" {
		return value
	}
	out := value
	for _, re := range toolOutputRedactors {
		out = re.ReplaceAllStringFunc(out, redactToolOutputMatch)
	}
	return out
}

// RedactToolOutput masks common credential patterns in user-visible tool text.
func RedactToolOutput(value string) string {
	return redactToolOutput(value)
}

func redactToolOutputMatch(match string) string {
	if loc := toolOutputKeyValueSep.FindStringIndex(match); loc != nil {
		sep := match[loc[0]:loc[1]]
		return match[:loc[0]] + sep + "[REDACTED]"
	}
	if bearer := toolOutputBearer.FindString(match); bearer != "" {
		return bearer + "[REDACTED]"
	}
	return "[REDACTED]"
}
