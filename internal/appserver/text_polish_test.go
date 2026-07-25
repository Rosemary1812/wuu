package appserver

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestTextPolishUsesConfiguredBYOKRuntime(t *testing.T) {
	client := &fakeClient{response: providersResponse("这是润色后的文本。")}
	rt := newTestRuntime(t, client)
	rt.StreamRunner.ProviderName = "test-provider"
	rt.StreamRunner.APIModel = "test-api-model"
	rt.StreamRunner.Effort = "low"
	rt.StreamRunner.ProviderOptions = map[string]any{"thinking": "disabled"}
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(
		`{"id":"polish","method":"text/polish","params":{"text":"这是 原始 文本"}}`,
	)); err != nil {
		t.Fatal(err)
	}

	result := remarshal[TextPolishResult](
		t,
		responseByID(t, parseOutput(t, out.String()), "polish")["result"],
	)
	if result.Text != "这是润色后的文本。" {
		t.Fatalf("text = %q", result.Text)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(client.requests))
	}
	request := client.requests[0]
	if request.Provider != "test-provider" || request.Model != "test-api-model" {
		t.Fatalf("runtime selection = %s/%s", request.Provider, request.Model)
	}
	if request.Effort != "low" || request.ProviderOptions["thinking"] != "disabled" {
		t.Fatalf("runtime options = effort %q, options %#v", request.Effort, request.ProviderOptions)
	}
	if len(request.Messages) != 2 || request.Messages[1].Content != "这是 原始 文本" {
		t.Fatalf("messages = %#v", request.Messages)
	}
	if !strings.Contains(request.Messages[0].Content, "Return only the polished text") {
		t.Fatalf("system prompt = %q", request.Messages[0].Content)
	}
}

func TestTextPolishValidationAndUnavailableRuntime(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StreamRunner = nil
	out := &lockedBuffer{}
	srv := New(rt, out)

	for _, raw := range []string{
		`{"id":"empty","method":"text/polish","params":{"text":"  "}}`,
		`{"id":"unavailable","method":"text/polish","params":{"text":"hello"}}`,
	} {
		if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
			t.Fatal(err)
		}
	}

	responses := parseOutput(t, out.String())
	if got := responseByID(t, responses, "empty")["error"]; !strings.Contains(strings.ToLower(strings.TrimSpace(toString(got))), "text is required") {
		t.Fatalf("empty error = %v", got)
	}
	if got := responseByID(t, responses, "unavailable")["error"]; !strings.Contains(toString(got), "BYOK model runtime") {
		t.Fatalf("unavailable error = %v", got)
	}
}

func toString(value any) string {
	return strings.TrimSpace(fmt.Sprint(value))
}
