package providers

import (
	"context"
	"strings"
	"testing"
)

func testImage() InputImage {
	return InputImage{MediaType: "image/png", Data: "aW1hZ2U=", Width: 2, Height: 2}
}

func testFile() InputFile {
	return InputFile{MediaType: "application/pdf", Data: "cGRm", Filename: "spec.pdf"}
}

func TestNormalizeMediaInputState(t *testing.T) {
	t.Parallel()
	if got := NormalizeMediaInputState(""); got != MediaInputAuto {
		t.Fatalf("empty state = %q, want auto", got)
	}
	if got := NormalizeMediaInputState("bogus"); got != MediaInputAuto {
		t.Fatalf("unknown state = %q, want auto", got)
	}
	if got := NormalizeMediaInputState(MediaInputSupported); got != MediaInputSupported {
		t.Fatalf("supported state = %q", got)
	}
	if got := NormalizeMediaInputState(MediaInputUnsupported); got != MediaInputUnsupported {
		t.Fatalf("unsupported state = %q", got)
	}
}

func TestProjectMediaForPolicyUnsupportedStripsMediaWithMarker(t *testing.T) {
	t.Parallel()
	msgs := []ChatMessage{
		{Role: "user", Content: "look at this", Images: []InputImage{testImage(), testImage()}, Files: []InputFile{testFile()}},
		{Role: "user", Content: "", Images: []InputImage{testImage()}},
		{Role: "assistant", Content: "no media here"},
	}
	policy := MediaInputPolicy{Image: MediaInputUnsupported, File: MediaInputUnsupported}
	out := ProjectMediaForPolicy(msgs, policy)

	if len(out[0].Images) != 0 || len(out[0].Files) != 0 {
		t.Fatalf("media not stripped: %+v", out[0])
	}
	if !strings.Contains(out[0].Content, "look at this") {
		t.Fatalf("original text lost: %q", out[0].Content)
	}
	if !strings.Contains(out[0].Content, "[2 images omitted: unsupported]") {
		t.Fatalf("missing plural image marker: %q", out[0].Content)
	}
	if !strings.Contains(out[0].Content, "[1 file omitted: unsupported]") {
		t.Fatalf("missing singular file marker: %q", out[0].Content)
	}
	if out[1].Content != "[1 image omitted: unsupported]" {
		t.Fatalf("empty content should become marker only, got %q", out[1].Content)
	}
	if out[2].Content != "no media here" {
		t.Fatalf("media-free message changed: %q", out[2].Content)
	}
	// Input must stay untouched: stored history keeps media for other readers.
	if len(msgs[0].Images) != 2 || len(msgs[0].Files) != 1 {
		t.Fatal("input messages mutated")
	}
}

func TestProjectMediaForPolicyPassThroughStates(t *testing.T) {
	t.Parallel()
	msgs := []ChatMessage{
		{Role: "user", Content: "hi", Images: []InputImage{testImage()}, Files: []InputFile{testFile()}},
	}
	for name, policy := range map[string]MediaInputPolicy{
		"zero value auto": {},
		"explicit auto":   {Image: MediaInputAuto, File: MediaInputAuto},
		"supported":       {Image: MediaInputSupported, File: MediaInputSupported},
		"mixed":           {Image: MediaInputUnsupported, File: MediaInputSupported},
	} {
		out := ProjectMediaForPolicy(msgs, policy)
		if policy.Image == MediaInputUnsupported {
			if len(out[0].Images) != 0 || len(out[0].Files) != 1 {
				t.Fatalf("%s: expected only images stripped, got %+v", name, out[0])
			}
			continue
		}
		if len(out[0].Images) != 1 || len(out[0].Files) != 1 {
			t.Fatalf("%s: media should pass through, got %+v", name, out[0])
		}
	}
}

func TestPrepareMessagesForProviderRequestWithPolicyStripsBeforeValidation(t *testing.T) {
	t.Parallel()
	msgs := []ChatMessage{
		{Role: "user", Content: "see attached", Images: []InputImage{testImage()}},
		{Role: "assistant", Content: "ok"},
	}
	prepared, err := PrepareMessagesForProviderRequestWithPolicy("p", "m", msgs, MediaInputPolicy{Image: MediaInputUnsupported})
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}
	if len(prepared[0].Images) != 0 {
		t.Fatal("images survived the request boundary")
	}
	if !strings.Contains(prepared[0].Content, "[1 image omitted: unsupported]") {
		t.Fatalf("marker missing: %q", prepared[0].Content)
	}
}

func TestResolveMediaInputPolicyOverlaysProbeCacheOnAutoOnly(t *testing.T) {
	resetProbedMediaCapabilities()
	t.Cleanup(resetProbedMediaCapabilities)

	// Nothing cached: auto stays auto.
	policy := ResolveMediaInputPolicy("prov", "model", MediaInputPolicy{})
	if policy.Image != MediaInputAuto || policy.File != MediaInputAuto {
		t.Fatalf("uncached policy changed: %+v", policy)
	}

	RecordMediaInputUnsupported("prov", "model", MediaInputPolicy{Image: MediaInputAuto, File: MediaInputSupported})
	resolved := ResolveMediaInputPolicy("prov", "model", MediaInputPolicy{})
	if resolved.Image != MediaInputUnsupported {
		t.Fatalf("cached unsupported not applied: %+v", resolved)
	}
	if resolved.File != MediaInputAuto {
		t.Fatalf("non-auto kind should not be cached: %+v", resolved)
	}

	// Explicit states always win over the cache.
	explicit := ResolveMediaInputPolicy("prov", "model", MediaInputPolicy{Image: MediaInputSupported})
	if explicit.Image != MediaInputSupported {
		t.Fatalf("explicit supported overridden by cache: %+v", explicit)
	}

	// Cache keys are provider+model scoped and case-insensitive.
	other := ResolveMediaInputPolicy("prov", "other-model", MediaInputPolicy{})
	if other.Image != MediaInputAuto {
		t.Fatalf("cache leaked across models: %+v", other)
	}
	upper := ResolveMediaInputPolicy("PROV", "MODEL", MediaInputPolicy{})
	if upper.Image != MediaInputUnsupported {
		t.Fatalf("cache key should be case-insensitive: %+v", upper)
	}
}

func TestRecordMediaInputSuccessRequiresMediaAndAuto(t *testing.T) {
	resetProbedMediaCapabilities()
	t.Cleanup(resetProbedMediaCapabilities)

	msgs := []ChatMessage{{Role: "user", Content: "hi", Images: []InputImage{testImage()}}}
	RecordMediaInputSuccess("prov", "vision-model", MediaInputPolicy{}, msgs)
	resolved := ResolveMediaInputPolicy("prov", "vision-model", MediaInputPolicy{})
	if resolved.Image != MediaInputSupported {
		t.Fatalf("successful probe should cache supported: %+v", resolved)
	}
	if resolved.File != MediaInputAuto {
		t.Fatalf("absent media kind must not be cached: %+v", resolved)
	}

	// No media in the request: nothing to attribute.
	RecordMediaInputSuccess("prov", "text-model", MediaInputPolicy{}, []ChatMessage{{Role: "user", Content: "hi"}})
	if got := ResolveMediaInputPolicy("prov", "text-model", MediaInputPolicy{}); got.Image != MediaInputAuto {
		t.Fatalf("media-free success polluted cache: %+v", got)
	}

	// Already-unsupported policy does not get flipped back.
	RecordMediaInputSuccess("prov", "pinned-model", MediaInputPolicy{Image: MediaInputUnsupported}, msgs)
	if got := ResolveMediaInputPolicy("prov", "pinned-model", MediaInputPolicy{Image: MediaInputUnsupported}); got.Image != MediaInputUnsupported {
		t.Fatalf("explicit unsupported flipped: %+v", got)
	}
}

func TestIsUnsupportedMediaFailureWhitelist(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		failure NormalizedFailure
		want    bool
	}{
		{"explicit image rejection", NormalizedFailure{HTTPStatus: 400, RawBody: `{"error":{"message":"this model does not support image inputs"}}`}, true},
		{"modality rejection via provider code", NormalizedFailure{HTTPStatus: 422, ProviderCode: "unsupported_modality"}, true},
		{"pdf rejection", NormalizedFailure{HTTPStatus: 400, RawBody: "model does not support PDF input"}, true},
		{"rate limit never matches", NormalizedFailure{HTTPStatus: 429, RawBody: "rate limit exceeded"}, false},
		{"quota never matches", NormalizedFailure{HTTPStatus: 403, RawBody: "quota exhausted"}, false},
		{"generic 400 never matches", NormalizedFailure{HTTPStatus: 400, RawBody: "invalid request: bad temperature"}, false},
		{"server error never matches", NormalizedFailure{HTTPStatus: 500, RawBody: "internal error does not support image"}, false},
		{"non-http error with evidence still matches", NormalizedFailure{RawBody: "image input is not supported for this model"}, true},
	}
	for _, tc := range cases {
		if got := IsUnsupportedMediaFailure(tc.failure); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// mediaProbeMockClient records the requests it receives and fails the first
// call with an explicit unsupported-media HTTP error.
type mediaProbeMockClient struct {
	requests []ChatRequest
	failWith error
}

func (m *mediaProbeMockClient) Chat(context.Context, ChatRequest) (ChatResponse, error) {
	return ChatResponse{}, nil
}

func (m *mediaProbeMockClient) StreamChat(_ context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	m.requests = append(m.requests, req)
	if len(m.requests) == 1 && m.failWith != nil {
		return nil, m.failWith
	}
	ch := make(chan StreamEvent, 1)
	ch <- StreamEvent{Type: EventDone}
	close(ch)
	return ch, nil
}

func TestReliableStreamStripsMediaOnceOnExplicitRejection(t *testing.T) {
	resetProbedMediaCapabilities()
	t.Cleanup(resetProbedMediaCapabilities)

	inner := &mediaProbeMockClient{failWith: &HTTPError{
		ProviderFamily: "test",
		StatusCode:     400,
		Body:           `{"error":{"message":"this model does not support image inputs"}}`,
	}}
	client := newReliableTestClient(inner, nil)
	req := reliableTestRequest()
	req.Provider = "prov"
	req.Model = "probe-model"
	req.MediaInput = MediaInputPolicy{} // fully auto
	req.Messages = []ChatMessage{
		{Role: "user", Content: "describe this", Images: []InputImage{testImage()}},
	}

	ch, err := client.StreamChat(context.Background(), req)
	if err != nil {
		t.Fatalf("stream open: %v", err)
	}
	events := collectReliableEvents(t, ch)
	for _, ev := range events {
		if ev.Type == EventError {
			t.Fatalf("probe should recover transparently, got error event: %v", ev.Error)
		}
	}

	if len(inner.requests) != 2 {
		t.Fatalf("expected probe + stripped retry, got %d attempts", len(inner.requests))
	}
	first, second := inner.requests[0], inner.requests[1]
	if len(first.Messages[0].Images) != 1 {
		t.Fatal("first attempt should carry the image")
	}
	if len(second.Messages[0].Images) != 0 {
		t.Fatal("retry attempt must strip the image")
	}
	if !strings.Contains(second.Messages[0].Content, "[1 image omitted: unsupported]") {
		t.Fatalf("retry missing marker: %q", second.Messages[0].Content)
	}

	// The probe result is cached: the next request resolves to unsupported
	// before it reaches the provider.
	resolved := ResolveMediaInputPolicy("prov", "probe-model", MediaInputPolicy{})
	if resolved.Image != MediaInputUnsupported {
		t.Fatalf("probe result not cached: %+v", resolved)
	}
}

func TestReliableStreamDoesNotStripOnNonWhitelistedFailure(t *testing.T) {
	resetProbedMediaCapabilities()
	t.Cleanup(resetProbedMediaCapabilities)

	inner := &mediaProbeMockClient{failWith: &HTTPError{
		ProviderFamily: "test",
		StatusCode:     400,
		Body:           "invalid request: temperature out of range",
	}}
	client := newReliableTestClient(inner, nil)
	req := reliableTestRequest()
	req.Provider = "prov"
	req.Model = "generic-400-model"
	req.Messages = []ChatMessage{
		{Role: "user", Content: "describe this", Images: []InputImage{testImage()}},
	}

	ch, err := client.StreamChat(context.Background(), req)
	if err != nil {
		t.Fatalf("stream open: %v", err)
	}
	var sawError bool
	for _, ev := range collectReliableEvents(t, ch) {
		if ev.Type == EventError {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("non-whitelisted failure must surface to the caller")
	}
	if len(inner.requests) != 1 {
		t.Fatalf("no media-strip retry allowed for generic 400, got %d attempts", len(inner.requests))
	}
	if got := ResolveMediaInputPolicy("prov", "generic-400-model", MediaInputPolicy{}); got.Image != MediaInputAuto {
		t.Fatalf("generic failure must not pollute the probe cache: %+v", got)
	}
}

func TestReliableStreamCachesSupportedAfterSuccessfulProbe(t *testing.T) {
	resetProbedMediaCapabilities()
	t.Cleanup(resetProbedMediaCapabilities)

	inner := &mediaProbeMockClient{}
	client := newReliableTestClient(inner, nil)
	req := reliableTestRequest()
	req.Provider = "prov"
	req.Model = "vision-probe-model"
	req.Messages = []ChatMessage{
		{Role: "user", Content: "describe this", Images: []InputImage{testImage()}},
	}

	ch, err := client.StreamChat(context.Background(), req)
	if err != nil {
		t.Fatalf("stream open: %v", err)
	}
	collectReliableEvents(t, ch)

	if len(inner.requests) != 1 {
		t.Fatalf("successful probe should not retry, got %d attempts", len(inner.requests))
	}
	if got := ResolveMediaInputPolicy("prov", "vision-probe-model", MediaInputPolicy{}); got.Image != MediaInputSupported {
		t.Fatalf("successful probe should cache supported: %+v", got)
	}
}
