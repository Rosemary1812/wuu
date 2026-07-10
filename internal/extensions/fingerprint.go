package extensions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

var envPlaceholderPattern = regexp.MustCompile(`\$\{[^}]+\}`)

type ExecutableSpec struct {
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	URL         string            `json:"url,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Permissions []string          `json:"permissions,omitempty"`
}

func Fingerprint(spec ExecutableSpec) (string, error) {
	normalized := ExecutableSpec{
		Command:     strings.TrimSpace(spec.Command),
		Args:        append([]string(nil), spec.Args...),
		URL:         strings.TrimSpace(spec.URL),
		Env:         redactSecretMap(spec.Env),
		Headers:     redactSecretMap(spec.Headers),
		Permissions: normalizedStrings(spec.Permissions),
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func redactSecretMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		placeholders := envPlaceholderPattern.FindAllString(value, -1)
		if len(placeholders) == 0 {
			out[key] = "<redacted>"
			continue
		}
		out[key] = strings.Join(placeholders, ",")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}
