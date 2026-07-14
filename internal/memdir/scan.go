package memdir

import (
	"fmt"
	"regexp"
	"strings"
)

// Injection-side security scan. It preserves the threat patterns and
// invisible-character checks from the retired indexed-memory tool, applying
// them when notebook index content is about to enter a prompt. Writes now use
// ordinary file tools, so prompt injection is the boundary that must scan.

var indexThreatPatterns = []struct {
	pattern *regexp.Regexp
	id      string
}{
	{regexp.MustCompile(`(?i)ignore\s+(previous|all|above|prior)\s+instructions`), "prompt_injection"},
	{regexp.MustCompile(`(?i)you\s+are\s+now\s+`), "role_hijack"},
	{regexp.MustCompile(`(?i)do\s+not\s+tell\s+the\s+user`), "deception_hide"},
	{regexp.MustCompile(`(?i)system\s+prompt\s+override`), "sys_prompt_override"},
	{regexp.MustCompile(`(?i)disregard\s+(your|all|any)\s+(instructions|rules|guidelines)`), "disregard_rules"},
	{regexp.MustCompile(`(?i)act\s+as\s+(if|though)\s+you\s+(have\s+no|don'?t\s+have)\s+(restrictions|limits|rules)`), "bypass_restrictions"},
	{regexp.MustCompile(`(?i)curl\s+[^\n]*\$\{?\w*(KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|API)`), "exfil_curl"},
	{regexp.MustCompile(`(?i)wget\s+[^\n]*\$\{?\w*(KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|API)`), "exfil_wget"},
	{regexp.MustCompile(`(?i)cat\s+[^\n]*(\.env|credentials|\.netrc|\.pgpass|\.npmrc|\.pypirc)`), "read_secrets"},
	{regexp.MustCompile(`(?i)authorized_keys`), "ssh_backdoor"},
	{regexp.MustCompile(`(?i)(\$HOME/\.ssh|~/\.ssh)`), "ssh_access"},
	{regexp.MustCompile(`(?i)(\$HOME/\.wuu/\.env|~/\.wuu/\.env)`), "wuu_env"},
}

var indexInvisibleChars = []rune{
	'\u200b', '\u200c', '\u200d', '\u2060', '\ufeff',
	'\u202a', '\u202b', '\u202c', '\u202d', '\u202e',
}

// scanIndexLine reports why a single index line must not be injected, or nil
// when the line is safe.
func scanIndexLine(line string) error {
	for _, ch := range indexInvisibleChars {
		if strings.ContainsRune(line, ch) {
			return fmt.Errorf("memdir: line contains invisible unicode character U+%04X", ch)
		}
	}
	for _, threat := range indexThreatPatterns {
		if threat.pattern.MatchString(line) {
			return fmt.Errorf("memdir: line matches threat pattern %q", threat.id)
		}
	}
	return nil
}
