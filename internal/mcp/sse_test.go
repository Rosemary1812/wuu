package mcp

import (
	"net/http"
	"testing"
)

func TestSSEHTTPClientDoesNotSetWholeRequestTimeout(t *testing.T) {
	client := newSSEHTTPClient()
	if client.Timeout != 0 {
		t.Fatalf("http.Client.Timeout = %v, want 0 for a long-lived SSE response", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("http client transport = %T, want *http.Transport", client.Transport)
	}
	if transport.ResponseHeaderTimeout <= 0 {
		t.Fatalf("ResponseHeaderTimeout = %v, want a bounded connection handshake", transport.ResponseHeaderTimeout)
	}
}
