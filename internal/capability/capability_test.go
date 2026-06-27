package capability

import "testing"

func TestAllCapabilitiesReturnsStableClosedSet(t *testing.T) {
	got := All()
	if len(got) == 0 {
		t.Fatal("All() must return a non-empty closed set")
	}
	seen := map[Capability]struct{}{}
	for _, c := range got {
		if _, dup := seen[c]; dup {
			t.Fatalf("All() contains duplicate capability %q", c)
		}
		seen[c] = struct{}{}
	}
	// The product spec calls out at least file.read, file.edit,
	// search.grep, search.glob, command.bash, command.background,
	// web.fetch, web.search, task.spawn, memory, plan. All of them
	// must be in the closed set.
	mustHave := []Capability{
		CapabilityFileRead, CapabilityFileEdit,
		CapabilitySearchGrep, CapabilitySearchGlob,
		CapabilityCommandBash, CapabilityCommandBackground,
		CapabilityWebFetch, CapabilityWebSearch,
		CapabilityTaskSpawn,
		CapabilityMemorySession, CapabilityPlan,
		CapabilityContextRewrite,
	}
	for _, c := range mustHave {
		if _, ok := seen[c]; !ok {
			t.Fatalf("All() must contain %q", c)
		}
	}
}

func TestSurfaceToolForCapabilityRoundTrip(t *testing.T) {
	s := Surface{
		ProfileName: "test",
		Tools: map[string]Capability{
			"read_file":   CapabilityFileRead,
			"edit_file":   CapabilityFileEdit,
			"apply_patch": CapabilityFileEdit,
		},
		DeferredTools: map[string]Capability{
			"web_search": CapabilityWebSearch,
		},
		HiddenTools: map[string]Capability{
			"internal_helper": CapabilityCommandBash,
		},
		Capabilities:         []Capability{CapabilityFileRead, CapabilityFileEdit, CapabilityCommandBash},
		DeferredCapabilities: []Capability{CapabilityWebSearch},
		HiddenCapabilities:   nil,
	}
	if got, ok := s.ToolForCapability(CapabilityFileEdit); !ok {
		t.Fatalf("ToolForCapability(file.edit) = _,%v, want true (map iteration order is non-deterministic)", ok)
	} else if got != "edit_file" && got != "apply_patch" {
		t.Fatalf("ToolForCapability(file.edit) = %q, want edit_file or apply_patch", got)
	}
	if got, ok := s.HiddenToolForCapability(CapabilityCommandBash); !ok || got != "internal_helper" {
		t.Fatalf("HiddenToolForCapability(command.bash) = %q,%v, want internal_helper,true", got, ok)
	}
	if got, ok := s.DeferredToolForCapability(CapabilityWebSearch); !ok || got != "web_search" {
		t.Fatalf("DeferredToolForCapability(web.search) = %q,%v, want web_search,true", got, ok)
	}
	if !s.HasCapability(CapabilityFileRead) {
		t.Fatal("HasCapability must report visible capabilities")
	}
	if !s.HasCapability(CapabilityWebSearch) {
		t.Fatal("HasCapability must report deferred capabilities")
	}
}

func TestSummarizeExposesAllContractFields(t *testing.T) {
	s := Surface{
		ProfileName: "openai_codex",
		Provider:    "openai",
		Model:       "gpt-5-codex",
		Tools: map[string]Capability{
			"apply_patch": CapabilityFileEdit,
			"bash":        CapabilityCommandBash,
		},
		DeferredTools: map[string]Capability{
			"web_search": CapabilityWebSearch,
		},
		HiddenTools: map[string]Capability{
			"internal_helper": CapabilityCommandBash,
		},
		Capabilities:         []Capability{CapabilityFileEdit, CapabilityCommandBash},
		DeferredCapabilities: []Capability{CapabilityWebSearch},
		HiddenCapabilities:   []Capability{},
		SystemFragment:       "fragment",
	}
	sum := s.Summarize()
	if sum.ProfileName != "openai_codex" {
		t.Fatalf("ProfileName = %s, want openai_codex", sum.ProfileName)
	}
	if !sum.BashFirst {
		t.Fatal("Summarize().BashFirst must be true when command.bash is present")
	}
	if sum.EditPrimitive != "apply_patch" {
		t.Fatalf("EditPrimitive = %s, want apply_patch", sum.EditPrimitive)
	}
	if sum.ToolCapabilityMap["apply_patch"] != string(CapabilityFileEdit) {
		t.Fatalf("ToolCapabilityMap[apply_patch] = %q, want %q", sum.ToolCapabilityMap["apply_patch"], CapabilityFileEdit)
	}
	if sum.DeferredCapabilityMap["web_search"] != string(CapabilityWebSearch) {
		t.Fatalf("DeferredCapabilityMap[web_search] = %q, want %q", sum.DeferredCapabilityMap["web_search"], CapabilityWebSearch)
	}
	if sum.HiddenCapabilityMap["internal_helper"] != string(CapabilityCommandBash) {
		t.Fatalf("HiddenCapabilityMap[internal_helper] = %q, want %q", sum.HiddenCapabilityMap["internal_helper"], CapabilityCommandBash)
	}
}
