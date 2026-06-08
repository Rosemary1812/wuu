package tools

import (
	"strings"
	"testing"
)

func TestRedactToolOutput(t *testing.T) {
	input := strings.Join([]string{
		"API_KEY=secret-value-1234567890",
		"Authorization: Bearer abcdefghijklmnop",
		"sk-testsecret123456",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signaturepart",
	}, "\n")

	got := redactToolOutput(input)
	for _, leaked := range []string{"secret-value", "abcdefghijklmnop", "sk-testsecret", "eyJhbGci"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted output leaked %q: %s", leaked, got)
		}
	}
	if strings.Count(got, "[REDACTED]") < 4 {
		t.Fatalf("expected redaction markers, got: %s", got)
	}
}
