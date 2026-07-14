package host

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExpoPusherSendsRequest(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotCT     string
		gotBody   []byte
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"status":"ok","id":"ticket-1"}]`))
	}))
	defer ts.Close()

	p := &ExpoPusher{
		Endpoint:   ts.URL + "/--/api/v2/push/send",
		HTTPClient: ts.Client(),
	}
	p.Push(context.Background(), HostPushEvent{
		Device:   "device-1",
		Token:    "ExponentPushToken[abc123]",
		Platform: "ios",
		Hint:     "agent_done",
		ThreadID: "thread-42",
		At:       time.Unix(0, 0).UTC(),
	})

	if gotMethod != http.MethodPost {
		t.Errorf("method: want POST, got %s", gotMethod)
	}
	if !strings.HasSuffix(gotPath, "/--/api/v2/push/send") {
		t.Errorf("path: want suffix /--/api/v2/push/send, got %s", gotPath)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("content-type: want application/json, got %s", gotCT)
	}
	var batch []ExpoPushRequest
	if err := json.Unmarshal(gotBody, &batch); err != nil {
		t.Fatalf("decode body: %v (body=%s)", err, gotBody)
	}
	if len(batch) != 1 {
		t.Fatalf("batch size: want 1, got %d", len(batch))
	}
	got := batch[0]
	if got.To != "ExponentPushToken[abc123]" {
		t.Errorf("to: want ExponentPushToken[abc123], got %q", got.To)
	}
	if got.Title == "" {
		t.Errorf("title: want non-empty")
	}
	if got.Body == "" {
		t.Errorf("body: want non-empty")
	}
	if got.Data["thread_id"] != "thread-42" {
		t.Errorf("data.thread_id: want thread-42, got %v", got.Data["thread_id"])
	}
	if got.Data["hint"] != "agent_done" {
		t.Errorf("data.hint: want agent_done, got %v", got.Data["hint"])
	}
}

func TestExpoPusherSkipsNonExpoToken(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	p := &ExpoPusher{Endpoint: ts.URL, HTTPClient: ts.Client()}
	p.Push(context.Background(), HostPushEvent{
		Token: "raw-apns-token-no-prefix",
		Hint:  "agent_done",
	})
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("HTTP calls: want 0, got %d (non-Expo token should be skipped)", calls)
	}
}

func TestExpoPusherIsExpoToken(t *testing.T) {
	cases := []struct {
		token string
		want  bool
	}{
		{"ExponentPushToken[abc123XYZ-_]", true},
		{"ExpoPushToken[abc123XYZ-_]", true},
		{"ExponentPushToken[]", false}, // too short
		{"plain", false},
		{"", false},
		{"ExponentPushToken[", false}, // no closing bracket, too short
	}
	for _, c := range cases {
		if got := isExpoToken(c.token); got != c.want {
			t.Errorf("isExpoToken(%q): want %v, got %v", c.token, c.want, got)
		}
	}
}

func TestExpoPusherCopyForHints(t *testing.T) {
	cases := []struct {
		hint     string
		wantBody string
	}{
		{"agent_done", "Agent finished a turn"},
		{"needs_input", "Agent needs your input"},
		{"something_else", "wuu activity"},
	}
	for _, c := range cases {
		_, body := expoCopy(c.hint, "thread-1")
		if body != c.wantBody {
			t.Errorf("expoCopy(%q): want body=%q, got %q", c.hint, c.wantBody, body)
		}
	}
}

func TestExpoPusherIgnores5xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	p := &ExpoPusher{Endpoint: ts.URL, HTTPClient: ts.Client()}
	// Must not panic or hang on a non-2xx response.
	p.Push(context.Background(), HostPushEvent{
		Token: "ExponentPushToken[abc123]",
		Hint:  "agent_done",
	})
}

func TestNewExpoPusherDefaults(t *testing.T) {
	p := NewExpoPusher()
	if p.Endpoint != DefaultExpoEndpoint {
		t.Errorf("Endpoint: want %q, got %q", DefaultExpoEndpoint, p.Endpoint)
	}
	if p.HTTPClient == nil {
		t.Errorf("HTTPClient: want non-nil")
	}
	if p.HTTPClient.Timeout <= 0 {
		t.Errorf("HTTPClient.Timeout: want > 0, got %v", p.HTTPClient.Timeout)
	}
}
