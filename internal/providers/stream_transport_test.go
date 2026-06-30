package providers

import (
	"net/http"
	"testing"
	"time"
)

func TestResolveStreamTransportConfig_Defaults(t *testing.T) {
	t.Setenv("WUU_STREAM_CONNECT_TIMEOUT_MS", "")
	t.Setenv("WUU_STREAM_IDLE_TIMEOUT_MS", "")

	cfg := ResolveStreamTransportConfig(nil)
	if cfg.ConnectTimeout != 15*time.Second {
		t.Fatalf("expected 15s connect timeout, got %s", cfg.ConnectTimeout)
	}
	if cfg.IdleTimeout != 300*time.Second {
		t.Fatalf("expected 300s idle timeout, got %s", cfg.IdleTimeout)
	}
}

func TestNormalizeStreamTransportMode(t *testing.T) {
	cases := []struct {
		in   string
		want StreamTransportMode
		ok   bool
	}{
		{"", "", true},
		{"auto", StreamTransportAuto, true},
		{"sse", StreamTransportSSE, true},
		{"websocket", StreamTransportWebSocket, true},
		{"websocket-cached", StreamTransportWebSocketCached, true},
		{"websocket_cached", StreamTransportWebSocketCached, true},
		{"invalid", "", false},
	}
	for _, tc := range cases {
		got, ok := NormalizeStreamTransportMode(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("NormalizeStreamTransportMode(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestResolveStreamTransportConfig_EnvOverrides(t *testing.T) {
	t.Setenv("WUU_STREAM_CONNECT_TIMEOUT_MS", "1500")
	t.Setenv("WUU_STREAM_IDLE_TIMEOUT_MS", "2500")

	cfg := ResolveStreamTransportConfig(&StreamTransportConfig{
		ConnectTimeout: 5 * time.Second,
		IdleTimeout:    6 * time.Second,
	})
	if cfg.ConnectTimeout != 1500*time.Millisecond {
		t.Fatalf("expected env connect timeout, got %s", cfg.ConnectTimeout)
	}
	if cfg.IdleTimeout != 2500*time.Millisecond {
		t.Fatalf("expected env idle timeout, got %s", cfg.IdleTimeout)
	}
}

func TestBuildStreamingHTTPClient_SetsConnectStageDeadlines(t *testing.T) {
	base := &http.Client{Transport: http.DefaultTransport}
	cfg := StreamTransportConfig{ConnectTimeout: 1234 * time.Millisecond}

	streamClient := BuildStreamingHTTPClient(base, cfg)
	if streamClient == base {
		t.Fatal("expected cloned client")
	}
	if streamClient.Timeout != 0 {
		t.Fatalf("expected streaming client timeout disabled, got %s", streamClient.Timeout)
	}

	transport, ok := streamClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", streamClient.Transport)
	}
	if transport.ResponseHeaderTimeout != cfg.ConnectTimeout {
		t.Fatalf("expected response header timeout %s, got %s", cfg.ConnectTimeout, transport.ResponseHeaderTimeout)
	}
	if transport.TLSHandshakeTimeout != cfg.ConnectTimeout {
		t.Fatalf("expected TLS handshake timeout %s, got %s", cfg.ConnectTimeout, transport.TLSHandshakeTimeout)
	}
	if transport.DialContext == nil {
		t.Fatal("expected DialContext to be configured")
	}
}
