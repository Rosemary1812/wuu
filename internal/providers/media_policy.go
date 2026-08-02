package providers

import (
	"fmt"
	"strings"
	"sync"
)

// MediaInputState is the admission state for one media kind on a request.
//
// The state machine is three-valued on purpose: the model catalog cannot
// distinguish "this model is text-only" from "we have no modality data for
// this model", so a boolean would force every unknown model into one error
// class. Auto preserves the unknown case so it can be probed once against
// the real provider and then cached.
type MediaInputState string

const (
	// MediaInputAuto means capability evidence is missing. Media is attached
	// on the first real request; an explicit provider rejection downgrades
	// the state to unsupported (see ReliableStreamClient).
	MediaInputAuto MediaInputState = "auto"
	// MediaInputSupported means catalog/config evidence says the model
	// accepts this media kind.
	MediaInputSupported MediaInputState = "supported"
	// MediaInputUnsupported means catalog/config/probe evidence says the
	// model rejects this media kind. Media is stripped at the request
	// boundary and replaced by a short marker.
	MediaInputUnsupported MediaInputState = "unsupported"
)

// MediaInputPolicy carries the resolved admission states for user-supplied
// media on one request. It is request metadata only: provider clients must
// never serialize it on the wire.
type MediaInputPolicy struct {
	Image MediaInputState
	File  MediaInputState
}

// NormalizeMediaInputState maps empty/unknown values to MediaInputAuto.
func NormalizeMediaInputState(state MediaInputState) MediaInputState {
	switch state {
	case MediaInputSupported, MediaInputUnsupported:
		return state
	default:
		return MediaInputAuto
	}
}

// HasAuto reports whether any media kind is still unprobed.
func (p MediaInputPolicy) HasAuto() bool {
	return NormalizeMediaInputState(p.Image) == MediaInputAuto ||
		NormalizeMediaInputState(p.File) == MediaInputAuto
}

// DowngradeUnsupported returns a copy with every auto state pinned to
// unsupported. The provider error that triggers a media-strip retry does not
// identify which media kind it rejected, so the downgrade is deliberately
// conservative across kinds; cached evidence refines future requests.
func (p MediaInputPolicy) DowngradeUnsupported() MediaInputPolicy {
	if NormalizeMediaInputState(p.Image) == MediaInputAuto {
		p.Image = MediaInputUnsupported
	}
	if NormalizeMediaInputState(p.File) == MediaInputAuto {
		p.File = MediaInputUnsupported
	}
	return p
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
// policy rejects replaced by the fixed omission marker. Auto and supported
// states pass media through untouched. The input slice and messages are
// never mutated; stored history keeps the original media for UI and for
// other agents/models that can read it.
func ProjectMediaForPolicy(msgs []ChatMessage, policy MediaInputPolicy) []ChatMessage {
	imageState := NormalizeMediaInputState(policy.Image)
	fileState := NormalizeMediaInputState(policy.File)
	if imageState != MediaInputUnsupported && fileState != MediaInputUnsupported {
		return msgs
	}
	out := make([]ChatMessage, len(msgs))
	copy(out, msgs)
	for i := range out {
		if imageState == MediaInputUnsupported && len(out[i].Images) > 0 {
			omitted := len(out[i].Images)
			out[i].Images = nil
			out[i].Content = appendMediaMarker(out[i].Content, omitted, "image", "images")
		}
		if fileState == MediaInputUnsupported && len(out[i].Files) > 0 {
			omitted := len(out[i].Files)
			out[i].Files = nil
			out[i].Content = appendMediaMarker(out[i].Content, omitted, "file", "files")
		}
	}
	return out
}

// messagesContainMedia reports whether any message still carries the given
// media kinds. Used to decide whether a probe result is attributable.
func messagesContainMedia(msgs []ChatMessage) (hasImages, hasFiles bool) {
	for i := range msgs {
		if len(msgs[i].Images) > 0 {
			hasImages = true
		}
		if len(msgs[i].Files) > 0 {
			hasFiles = true
		}
	}
	return hasImages, hasFiles
}

// mediaCapabilityStore is a process-local cache of probed media capabilities.
// Keys are the configured provider identity plus model, which already scopes
// entries per provider instance; catalog or credential changes take effect
// on process restart. Entries only ever refine the auto state.
type mediaCapabilityStore struct {
	mu     sync.Mutex
	states map[string]MediaInputPolicy
}

var probedMediaCapabilities = &mediaCapabilityStore{states: make(map[string]MediaInputPolicy)}

func mediaCapabilityKey(provider, model string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + "\x00" + strings.ToLower(strings.TrimSpace(model))
}

// ResolveMediaInputPolicy overlays probed capability evidence onto the
// policy's auto states. Explicit supported/unsupported states always win
// over the probe cache.
func ResolveMediaInputPolicy(provider, model string, policy MediaInputPolicy) MediaInputPolicy {
	policy.Image = NormalizeMediaInputState(policy.Image)
	policy.File = NormalizeMediaInputState(policy.File)
	if !policy.HasAuto() {
		return policy
	}
	probedMediaCapabilities.mu.Lock()
	cached, ok := probedMediaCapabilities.states[mediaCapabilityKey(provider, model)]
	probedMediaCapabilities.mu.Unlock()
	if !ok {
		return policy
	}
	if policy.Image == MediaInputAuto && cached.Image != "" && cached.Image != MediaInputAuto {
		policy.Image = cached.Image
	}
	if policy.File == MediaInputAuto && cached.File != "" && cached.File != MediaInputAuto {
		policy.File = cached.File
	}
	return policy
}

// RecordMediaInputUnsupported caches unsupported evidence for the media
// kinds the caller marks auto in the probe argument. The argument must name
// exactly the kinds the rejected request actually carried: a rejection
// tells us nothing about media kinds that were not present, so they must
// stay uncached. Only kinds set to MediaInputAuto in policy are recorded;
// zero values mean "not part of this probe".
func RecordMediaInputUnsupported(provider, model string, policy MediaInputPolicy) {
	recordProbedMediaCapability(provider, model, policy, MediaInputUnsupported)
}

// RecordMediaInputSuccess caches supported evidence for media kinds that
// were auto on a request which actually carried that media and completed
// successfully.
func RecordMediaInputSuccess(provider, model string, policy MediaInputPolicy, msgs []ChatMessage) {
	hasImages, hasFiles := messagesContainMedia(msgs)
	if !hasImages && !hasFiles {
		return
	}
	probe := MediaInputPolicy{}
	if hasImages && NormalizeMediaInputState(policy.Image) == MediaInputAuto {
		probe.Image = MediaInputAuto
	}
	if hasFiles && NormalizeMediaInputState(policy.File) == MediaInputAuto {
		probe.File = MediaInputAuto
	}
	recordProbedMediaCapability(provider, model, probe, MediaInputSupported)
}

func recordProbedMediaCapability(provider, model string, policy MediaInputPolicy, state MediaInputState) {
	key := mediaCapabilityKey(provider, model)
	if key == "\x00" {
		return
	}
	probedMediaCapabilities.mu.Lock()
	defer probedMediaCapabilities.mu.Unlock()
	entry := probedMediaCapabilities.states[key]
	// Compare against the raw constant, not the normalized value: the probe
	// argument uses the zero value for "not part of this probe", which must
	// not be mistaken for an auto kind awaiting evidence.
	if policy.Image == MediaInputAuto {
		entry.Image = state
	}
	if policy.File == MediaInputAuto {
		entry.File = state
	}
	if entry.Image != "" || entry.File != "" {
		probedMediaCapabilities.states[key] = entry
	}
}

// resetProbedMediaCapabilities clears the probe cache. Test-only.
func resetProbedMediaCapabilities() {
	probedMediaCapabilities.mu.Lock()
	probedMediaCapabilities.states = make(map[string]MediaInputPolicy)
	probedMediaCapabilities.mu.Unlock()
}

// unsupportedMediaEvidence lists lowercase provider-error fragments that
// explicitly state the model rejected the media payload itself. The list is
// a whitelist on purpose: a miss surfaces the real error to the user (who
// can fix it via configuration), while a false hit would silently swallow a
// legitimate failure behind a media-strip retry.
var unsupportedMediaEvidence = []string{
	"does not support image",
	"image input is not supported",
	"images are not supported",
	"image is not supported",
	"unsupported image",
	"does not support vision",
	"vision is not supported",
	"not a vision model",
	"does not support multimodal",
	"multimodal is not supported",
	"unsupported modality",
	"modality is not supported",
	"cannot process image",
	"does not support pdf",
	"pdf is not supported",
	"document input is not supported",
}

// IsUnsupportedMediaFailure reports whether a normalized failure is explicit
// provider evidence that the request's media payload is unsupported. Only
// client-side (4xx) request rejections whose body or provider code names
// image/modality support qualify; rate limits, quota, auth, and generic 400s
// never do.
func IsUnsupportedMediaFailure(failure NormalizedFailure) bool {
	if failure.HTTPStatus != 0 && (failure.HTTPStatus < 400 || failure.HTTPStatus >= 500) {
		return false
	}
	haystack := strings.ToLower(failure.RawBody + "\x00" + failure.ProviderCode)
	// Provider codes use snake_case (e.g. "unsupported_modality") while
	// message bodies use spaces; normalize both to the evidence list's form.
	haystack = strings.ReplaceAll(haystack, "_", " ")
	for _, fragment := range unsupportedMediaEvidence {
		if strings.Contains(haystack, fragment) {
			return true
		}
	}
	return false
}
