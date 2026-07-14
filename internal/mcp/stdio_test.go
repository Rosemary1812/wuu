package mcp

import "testing"

func TestStdioTransportCloseReapsProcessAndIsIdempotent(t *testing.T) {
	transport, err := NewStdioTransport("sh", "-c", "cat >/dev/null")
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}

	if err := transport.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if transport.cmd.ProcessState == nil || !transport.cmd.ProcessState.Exited() {
		t.Fatalf("Close did not reap the child process: %+v", transport.cmd.ProcessState)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
