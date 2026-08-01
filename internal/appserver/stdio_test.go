package appserver

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestRunStdioScannerAcceptsAttachmentSizedRequests(t *testing.T) {
	const payloadBytes = 5 * 1024 * 1024

	var out bytes.Buffer
	server := &Server{out: &out}
	input := fmt.Sprintf(
		"{\"id\":\"large\",\"method\":\"test/unknown\",\"params\":{\"data\":\"%s\"}}\n",
		strings.Repeat("a", payloadBytes),
	)

	if err := runStdioScanner(context.Background(), server, strings.NewReader(input)); err != nil {
		t.Fatalf("runStdioScanner rejected attachment-sized request: %v", err)
	}
	if !strings.Contains(out.String(), `"id":"large"`) {
		t.Fatalf("response did not preserve request id: %s", out.String())
	}
}
