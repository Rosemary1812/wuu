package process

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var localhostURLPattern = regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])(?::\d{1,5})?(?:/[^\s"'<>)]*)?`)

func PreviewURLsFromText(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return NormalizePreviewURLs(localhostURLPattern.FindAllString(text, -1))
}

func PreviewURLsFromPorts(ports []int) []string {
	out := make([]string, 0, len(ports))
	seen := make(map[int]struct{}, len(ports))
	for _, port := range ports {
		if port < 1 || port > 65535 {
			continue
		}
		if _, ok := seen[port]; ok {
			continue
		}
		seen[port] = struct{}{}
		out = append(out, fmt.Sprintf("http://localhost:%d", port))
	}
	return out
}

func NormalizePreviewURLs(urls []string) []string {
	if len(urls) == 0 {
		return nil
	}
	out := make([]string, 0, len(urls))
	seen := make(map[string]struct{}, len(urls))
	for _, raw := range urls {
		normalized := NormalizePreviewURL(raw)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func NormalizePreviewURL(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimRight(value, ".,;")
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "0.0.0.0" && host != "::1" {
		return ""
	}
	if port := parsed.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return ""
		}
		parsed.Host = net.JoinHostPort("localhost", port)
	} else {
		parsed.Host = "localhost"
	}
	parsed.Scheme = scheme
	parsed.User = nil
	return parsed.String()
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
