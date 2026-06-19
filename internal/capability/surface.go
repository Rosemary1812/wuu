package capability

import "sort"

// Surface is the per-model compilation of internal capabilities to
// model-visible tool entries plus a profile-specific system prompt
// fragment.
//
// The toolkit consumes a Surface to decide which ToolDefinitions to
// expose to the model and which to keep as internal / advanced
// capabilities. The frontend reads the same Surface to render
// capability-aware activity and approval UI. Telemetry attaches the
// Surface to a session so post-hoc analysis can compare how different
// models actually used the harness.
type Surface struct {
	// ProfileName is the stable compiler key (e.g. "openai_codex",
	// "anthropic_claude"). It is safe to log, persist, and compare.
	ProfileName string

	// Provider and Model are the resolved values that produced this
	// surface. They are informational; capability decisions are based
	// on ProfileName plus the underlying profile fields.
	Provider string
	Model    string

	// Tools maps the external model-visible tool name (the value
	// sent in providers.ToolDefinition.Name) to its owning
	// capability. Tools not present in the map are NOT exposed to
	// the model under this surface.
	Tools map[string]Capability

	// HiddenTools maps tool names that the toolkit still
	// implements but does not advertise to the model under this
	// surface. They remain reachable through tool_search
	// (progressive disclosure) or through internal callers (e.g.
	// run_test as a bash result post-processor, start_process as a
	// managed-process backend for bash background mode).
	HiddenTools map[string]Capability

	// Capabilities is the ordered set of capabilities exposed to
	// the model. The order is stable per ProfileName and is used
	// to drive capability-first rendering, permission routing, and
	// approval UI.
	Capabilities []Capability

	// HiddenCapabilities lists capabilities that exist but are not
	// surfaced on this profile. They still exist for permission
	// routing when advanced tools are activated and for telemetry
	// that wants to reason about a missing capability.
	HiddenCapabilities []Capability

	// SystemFragment is the profile-specific addition to the
	// system prompt. It tells the model which tool set it has and
	// which mental model to follow (bash-first, apply_patch
	// preferred, exact-edit preferred, etc.).
	SystemFragment string
}

// HasCapability reports whether the surface advertises a capability
// either directly (Capabilities) or as a hidden implementation
// (HiddenCapabilities). Tooling that needs to check whether a
// capability is *available* should use this; tooling that needs to
// check whether the model can *see* the capability should consult
// Capabilities directly.
func (s Surface) HasCapability(c Capability) bool {
	for _, existing := range s.Capabilities {
		if existing == c {
			return true
		}
	}
	for _, existing := range s.HiddenCapabilities {
		if existing == c {
			return true
		}
	}
	return false
}

// VisibleCapabilities returns the advertised capability list. It is
// the slice to use for permission-routing checks that ask "can the
// model see X?"; HasCapability is for "does X exist for this
// surface?".
func (s Surface) VisibleCapabilities() []Capability {
	out := make([]Capability, len(s.Capabilities))
	copy(out, s.Capabilities)
	return out
}

// ToolNames returns the model-visible tool names in alphabetical
// order. The ordering is stable so test assertions and UI lists
// stay deterministic.
func (s Surface) ToolNames() []string {
	out := make([]string, 0, len(s.Tools))
	for name := range s.Tools {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ToolForCapability returns the model-visible tool name that
// implements the given capability, plus true. Returns "", false if
// no model-visible tool implements the capability on this surface
// (it may still exist as a hidden tool — see HasCapability).
//
// Iteration is sorted by tool name so the return value is stable
// across runs even when a capability has more than one model-visible
// tool (e.g. file.edit on Claude exposes both edit_file and
// write_file; this method always returns edit_file). Callers that
// need the full list should iterate Tools directly.
func (s Surface) ToolForCapability(c Capability) (string, bool) {
	for _, name := range s.ToolNames() {
		if s.Tools[name] == c {
			return name, true
		}
	}
	return "", false
}

// HiddenToolForCapability returns a hidden tool name (if any) that
// implements the capability. Used by internal callers (e.g. the
// bash result post-processor) that need to reach a tool the model
// cannot see.
func (s Surface) HiddenToolForCapability(c Capability) (string, bool) {
	hidden := make([]string, 0, len(s.HiddenTools))
	for name := range s.HiddenTools {
		hidden = append(hidden, name)
	}
	sort.Strings(hidden)
	for _, name := range hidden {
		if s.HiddenTools[name] == c {
			return name, true
		}
	}
	return "", false
}

// Summary is the compact, JSON-friendly view of a Surface used by
// the app-server protocol. InitializeResult, runtime setting
// updates, and Settings debug views all return this struct.
type Summary struct {
	ProfileName         string              `json:"profile_name"`
	Provider            string              `json:"provider"`
	Model               string              `json:"model"`
	ToolNames           []string            `json:"tool_names"`
	HiddenToolNames     []string            `json:"hidden_tool_names"`
	Capabilities        []string            `json:"capabilities"`
	HiddenCapabilities  []string            `json:"hidden_capabilities"`
	EditPrimitive       string              `json:"edit_primitive"`
	BashFirst           bool                `json:"bash_first"`
	ToolCapabilityMap   map[string]string   `json:"tool_capability_map"`
	HiddenCapabilityMap map[string]string   `json:"hidden_capability_map"`
	SystemFragment      string              `json:"system_fragment,omitempty"`
}

// Summarize returns a compact summary of the surface. The summary
// is what flows over the app-server protocol and into the frontend
// debug UI; the full Surface stays server-side because it carries
// the implementation map and the system prompt fragment.
func (s Surface) Summarize() Summary {
	toolCaps := make(map[string]string, len(s.Tools))
	for name, c := range s.Tools {
		toolCaps[name] = string(c)
	}
	hiddenCaps := make(map[string]string, len(s.HiddenTools))
	for name, c := range s.HiddenTools {
		hiddenCaps[name] = string(c)
	}
	caps := make([]string, 0, len(s.Capabilities))
	for _, c := range s.Capabilities {
		caps = append(caps, string(c))
	}
	hidden := make([]string, 0, len(s.HiddenCapabilities))
	for _, c := range s.HiddenCapabilities {
		hidden = append(hidden, string(c))
	}
	hiddenTools := make([]string, 0, len(s.HiddenTools))
	for name := range s.HiddenTools {
		hiddenTools = append(hiddenTools, name)
	}
	sort.Strings(hiddenTools)
	return Summary{
		ProfileName:         s.ProfileName,
		Provider:            s.Provider,
		Model:               s.Model,
		ToolNames:           s.ToolNames(),
		HiddenToolNames:     hiddenTools,
		Capabilities:        caps,
		HiddenCapabilities:  hidden,
		EditPrimitive:       s.editPrimitive(),
		BashFirst:           s.HasCapability(CapabilityCommandBash),
		ToolCapabilityMap:   toolCaps,
		HiddenCapabilityMap: hiddenCaps,
		SystemFragment:      s.SystemFragment,
	}
}

// editPrimitive returns a short, stable string describing the
// editing primitive this surface advertises. It is informational
// and surfaces in the debug UI; the toolkit still consults
// CapabilityFileEdit + the profile.Workflow.DefaultWriteMode to
// route edits to the right tool.
func (s Surface) editPrimitive() string {
	if patch, ok := s.ToolForCapability(CapabilityFileEdit); ok {
		return patch
	}
	return ""
}
