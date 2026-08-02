package providers

import (
	"fmt"
	"strings"
)

// MediaInputPolicy carries the resolved admission decision for user-supplied
// media on one request: true attaches the media kind, false strips it at the
// request boundary and replaces it with a short marker. The decision is a
// plain boolean by design: catalog or user-configured modalities are the
// only evidence, and a model without modality data is treated as text-only
// (conservative), matching the behavior of other mature agent runtimes.
// User configuration always wins because it feeds the same modalities field.
//
// The policy is request metadata only: provider clients must never serialize
// it on the wire.
type MediaInputPolicy struct {
	Image bool
	File  bool
}

// MediaOmissionMarker renders the fixed short marker that replaces stripped
// media in the model context. It intentionally carries no OCR, description,
// base64, or dimensions, matching the chat_read marker agents already see.
func MediaOmissionMarker(count int, singular, plural string) string {
	label := plural
	if count == 1 {
		label = singular
	}
	return fmt.Sprintf("[%d %s omitted: unsupported]", count, label)
}

func appendMediaMarker(content string, count int, singular, plural string) string {
	marker := MediaOmissionMarker(count, singular, plural)
	if strings.TrimSpace(content) == "" {
		return marker
	}
	return content + "\n" + marker
}

// ProjectMediaForPolicy returns a request-scoped copy of msgs with media the
// policy rejects replaced by the fixed omission marker. Admitted media
// passes through untouched. The input slice and messages are never mutated;
// stored history keeps the original media for UI and for other agents/models
// that can read it.
func ProjectMediaForPolicy(msgs []ChatMessage, policy MediaInputPolicy) []ChatMessage {
	if policy.Image && policy.File {
		return msgs
	}
	out := make([]ChatMessage, len(msgs))
	copy(out, msgs)
	for i := range out {
		if !policy.Image && len(out[i].Images) > 0 {
			omitted := len(out[i].Images)
			out[i].Images = nil
			out[i].Content = appendMediaMarker(out[i].Content, omitted, "image", "images")
		}
		if !policy.File && len(out[i].Files) > 0 {
			omitted := len(out[i].Files)
			out[i].Files = nil
			out[i].Content = appendMediaMarker(out[i].Content, omitted, "file", "files")
		}
	}
	return out
}
