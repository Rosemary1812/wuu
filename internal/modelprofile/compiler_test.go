package modelprofile

import (
	"sort"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/capability"
)

// allProfileKeys is the canonical set the harness is expected to
// surface. Adding a new ProfileKey must also add a test case here.
var allProfileKeys = []ProfileKey{
	ProfileOpenAICodex,
	ProfileOpenAIGPT,
	ProfileAnthropicClaude,
	ProfileGeneric,
}

func TestResolveProfileKey(t *testing.T) {
	cases := []struct {
		provider string
		model    string
		want     ProfileKey
	}{
		{provider: "openai", model: "gpt-5-codex", want: ProfileOpenAICodex},
		{provider: "openai", model: "gpt-5.5", want: ProfileOpenAIGPT},
		{provider: "openai", model: "gpt-4.1-mini", want: ProfileOpenAIGPT},
		{provider: "openai", model: "gpt-oss-120b", want: ProfileOpenAIGPT},
		{provider: "anthropic", model: "claude-sonnet-4-5", want: ProfileAnthropicClaude},
		{provider: "anthropic", model: "claude-opus-4-7", want: ProfileAnthropicClaude},
		{provider: "google", model: "gemini-2.5-pro", want: ProfileGeneric},
		{provider: "moonshot", model: "kimi-k2", want: ProfileGeneric},
		{provider: "deepseek", model: "deepseek-v3.2", want: ProfileGeneric},
		{provider: "dashscope", model: "qwen3-coder-plus", want: ProfileGeneric},
		{provider: "ollama", model: "llama-coder", want: ProfileGeneric},
		{provider: "custom", model: "local-model", want: ProfileGeneric},
	}
	for _, tt := range cases {
		got := ResolveProfileKey(Resolve(tt.provider, tt.model))
		if got != tt.want {
			t.Fatalf("ResolveProfileKey(%s, %s) = %s, want %s", tt.provider, tt.model, got, tt.want)
		}
	}
}

func TestCompilerReturnsAllFourProfiles(t *testing.T) {
	c := DefaultCompiler{}
	cases := []struct {
		provider string
		model    string
		want     ProfileKey
	}{
		{provider: "openai", model: "gpt-5-codex", want: ProfileOpenAICodex},
		{provider: "openai", model: "gpt-5.5", want: ProfileOpenAIGPT},
		{provider: "anthropic", model: "claude-sonnet-4-5", want: ProfileAnthropicClaude},
		{provider: "google", model: "gemini-2.5-pro", want: ProfileGeneric},
	}
	for _, tt := range cases {
		s := c.Compile(Resolve(tt.provider, tt.model))
		if s.ProfileName != string(tt.want) {
			t.Fatalf("Compile(%s, %s).ProfileName = %s, want %s", tt.provider, tt.model, s.ProfileName, tt.want)
		}
		if s.Provider == "" || s.Model == "" {
			t.Fatalf("compile must carry provider+model, got %+v", s)
		}
		if s.SystemFragment == "" {
			t.Fatalf("compile must emit a system fragment for %s", s.ProfileName)
		}
	}
}

func TestOpenAICodexSurface(t *testing.T) {
	s := DefaultCompiler{}.Compile(Resolve("openai", "gpt-5-codex"))

	// Editing primitive is apply_patch. edit_file and write_file
	// must not be visible on this surface.
	if tool, ok := s.ToolForCapability(capability.CapabilityFileEdit); !ok || tool != "apply_patch" {
		t.Fatalf("Codex file.edit must map to apply_patch, got tool=%q ok=%v", tool, ok)
	}
	for _, hidden := range []string{"edit_file", "write_file"} {
		if _, visible := s.Tools[hidden]; visible {
			t.Fatalf("Codex surface must not advertise %s", hidden)
		}
	}

	// Bash-first: bash is visible; old command tools are not part
	// of the compiled profile surface.
	if _, ok := s.Tools["bash"]; !ok {
		t.Fatalf("Codex surface must include bash as a visible tool")
	}
	if !hasCapability(s.Capabilities, capability.CapabilityCommandBackground) {
		t.Fatalf("Codex surface must advertise command.background through bash, got caps=%v", s.Capabilities)
	}
	if !hasCapability(s.Capabilities, capability.CapabilityMCP) {
		t.Fatalf("Codex surface must advertise mcp capability for tool_search-gated extensions, got caps=%v", s.Capabilities)
	}
	for _, hidden := range []string{"run_test", "git"} {
		if _, visible := s.Tools[hidden]; visible {
			t.Fatalf("Codex surface must not advertise %s as a default tool", hidden)
		}
		if _, ok := s.HiddenTools[hidden]; ok {
			t.Fatalf("Codex surface must not keep legacy command tool %s in hidden profile output", hidden)
		}
	}

	// Read / list / search / web / task / plan / workflow / schedule / skill / ports
	// must all be visible.
	mustVisible := []string{
		"read_file", "list_files",
		"grep", "glob", "ast_search", "semantic_search",
		"web_search", "web_fetch",
		"spawn_agent", "session_memory",
		"update_plan", "create_goal", "get_goal", "update_goal", "list_workflows",
		"schedule_cron", "load_skill",
		"report_listening_ports",
	}
	for _, name := range mustVisible {
		if _, ok := s.Tools[name]; !ok {
			t.Fatalf("Codex surface must include %s, got tools=%v", name, sortedKeys(s.Tools))
		}
	}

	// Prompt fragment must call out apply_patch + bash explicitly.
	if !contains(s.SystemFragment, "apply_patch") || !contains(s.SystemFragment, "bash") {
		t.Fatalf("Codex prompt fragment must reference apply_patch and bash; got: %s", s.SystemFragment)
	}
	if !contains(s.SystemFragment, "command.bash") && !contains(s.SystemFragment, "terminal") {
		t.Fatalf("Codex prompt fragment must mention bash/terminal mental model; got: %s", s.SystemFragment)
	}
}

func TestOpenAIGPTSurfaceDefaultsToApplyPatch(t *testing.T) {
	s := DefaultCompiler{}.Compile(Resolve("openai", "gpt-5.5"))
	if tool, ok := s.ToolForCapability(capability.CapabilityFileEdit); !ok || tool != "apply_patch" {
		t.Fatalf("GPT surface must default to apply_patch, got tool=%q ok=%v", tool, ok)
	}
	if _, ok := s.Tools["bash"]; !ok {
		t.Fatalf("GPT surface must include bash")
	}
	if _, ok := s.Tools["run_test"]; ok {
		t.Fatalf("GPT surface must not advertise run_test")
	}
}

func TestOpenAIGPTSurfaceUsesApplyPatchForAllOpenAIModels(t *testing.T) {
	for _, model := range []string{"gpt-5.5", "gpt-4.1-mini", "openai/gpt-oss-120b"} {
		s := DefaultCompiler{}.Compile(Resolve("openai", model))
		tool, ok := s.ToolForCapability(capability.CapabilityFileEdit)
		if !ok {
			t.Fatalf("%s: expected file.edit capability to be visible", model)
		}
		if tool != "apply_patch" {
			t.Fatalf("%s: OpenAI GPT surface must use apply_patch, got %q", model, tool)
		}
		if _, hasEdit := s.Tools["edit_file"]; hasEdit {
			t.Fatalf("%s: OpenAI GPT surface must not advertise edit_file", model)
		}
		if _, hasWrite := s.Tools["write_file"]; hasWrite {
			t.Fatalf("%s: OpenAI GPT surface must not advertise write_file", model)
		}
	}
}

func TestAnthropicClaudeSurface(t *testing.T) {
	s := DefaultCompiler{}.Compile(Resolve("anthropic", "claude-sonnet-4-5"))

	// Editing primitive is edit_file (+ write_file as whole-file fallback).
	// apply_patch is hidden, never visible.
	tool, ok := s.ToolForCapability(capability.CapabilityFileEdit)
	if !ok || tool != "edit_file" {
		t.Fatalf("Claude file.edit must map to edit_file, got tool=%q ok=%v", tool, ok)
	}
	if _, has := s.Tools["write_file"]; !has {
		t.Fatalf("Claude surface must include write_file")
	}
	if _, has := s.Tools["apply_patch"]; has {
		t.Fatalf("Claude surface must not advertise apply_patch")
	}

	// Bash-first: bash visible; run_test / start_process / git must
	// not be model-visible tools.
	if _, ok := s.Tools["bash"]; !ok {
		t.Fatalf("Claude surface must include bash")
	}
	if !hasCapability(s.Capabilities, capability.CapabilityCommandBackground) {
		t.Fatalf("Claude surface must advertise command.background through bash, got caps=%v", s.Capabilities)
	}
	if !hasCapability(s.Capabilities, capability.CapabilityMCP) {
		t.Fatalf("Claude surface must advertise mcp capability for tool_search-gated extensions, got caps=%v", s.Capabilities)
	}
	for _, hidden := range []string{"run_test", "git"} {
		if _, visible := s.Tools[hidden]; visible {
			t.Fatalf("Claude surface must not advertise %s as a default tool", hidden)
		}
	}

	// The same core capabilities as Codex, minus apply_patch.
	for _, name := range []string{
		"read_file", "list_files", "grep", "glob",
		"web_search", "web_fetch", "update_plan",
		"session_memory", "load_skill", "report_listening_ports",
	} {
		if _, ok := s.Tools[name]; !ok {
			t.Fatalf("Claude surface must include %s, got tools=%v", name, sortedKeys(s.Tools))
		}
	}

	// Prompt fragment must call out read_file / edit_file / write_file +
	// bash and not advertise run_test or git.
	if !contains(s.SystemFragment, "read_file") || !contains(s.SystemFragment, "edit_file") {
		t.Fatalf("Claude prompt must reference read_file + edit_file; got: %s", s.SystemFragment)
	}
	if !contains(s.SystemFragment, "bash") {
		t.Fatalf("Claude prompt must reference bash; got: %s", s.SystemFragment)
	}
}

func TestGenericSurfaceForOpenAISHapedBYOK(t *testing.T) {
	for _, tt := range []struct {
		provider string
		model    string
	}{
		{provider: "google", model: "gemini-2.5-pro"},
		{provider: "moonshot", model: "kimi-k2"},
		{provider: "deepseek", model: "deepseek-v3.2"},
		{provider: "dashscope", model: "qwen3-coder-plus"},
	} {
		s := DefaultCompiler{}.Compile(Resolve(tt.provider, tt.model))
		if s.ProfileName != string(ProfileGeneric) {
			t.Fatalf("%s/%s: ProfileName = %s, want generic", tt.provider, tt.model, s.ProfileName)
		}
		tool, ok := s.ToolForCapability(capability.CapabilityFileEdit)
		if !ok || tool != "edit_file" {
			t.Fatalf("%s/%s: generic file.edit must map to edit_file, got tool=%q ok=%v", tt.provider, tt.model, tool, ok)
		}
		if _, has := s.Tools["bash"]; !has {
			t.Fatalf("%s/%s: generic surface must include bash", tt.provider, tt.model)
		}
		if _, has := s.Tools["run_test"]; has {
			t.Fatalf("%s/%s: generic surface must not advertise run_test", tt.provider, tt.model)
		}
		if _, has := s.Tools["git"]; has {
			t.Fatalf("%s/%s: generic surface must not advertise git as a default tool", tt.provider, tt.model)
		}
	}
}

func TestGenericSurfaceDropsBashForLocal(t *testing.T) {
	s := DefaultCompiler{}.Compile(Resolve("ollama", "llama-coder"))
	if s.ProfileName != string(ProfileGeneric) {
		t.Fatalf("local profile must compile under generic, got %s", s.ProfileName)
	}
	// The local profile should not expose command.bash as a VISIBLE
	// capability. HasCapability returns true for hidden capabilities
	// too, so we iterate s.Capabilities directly.
	if hasCapability(s.Capabilities, capability.CapabilityCommandBash) {
		t.Fatalf("local profile must not advertise command.bash, got caps=%v", s.Capabilities)
	}
	if _, has := s.Tools["bash"]; has {
		t.Fatalf("local profile must not include bash, got tools=%v", sortedKeys(s.Tools))
	}
	if _, has := s.Tools["edit_file"]; !has {
		t.Fatalf("local generic profile must include edit_file so prompt and write_file guidance remain usable, got tools=%v", sortedKeys(s.Tools))
	}
	if _, has := s.Tools["write_file"]; !has {
		t.Fatalf("local generic profile must include write_file, got tools=%v", sortedKeys(s.Tools))
	}
	for _, legacy := range []string{"bash", "start_process", "run_shell", "run_test", "git"} {
		if _, ok := s.HiddenTools[legacy]; ok {
			t.Fatalf("local profile must not keep %s in hidden profile output", legacy)
		}
	}
}

func TestLocalNoShellFragmentDoesNotNameUnavailableTools(t *testing.T) {
	s := DefaultCompiler{}.Compile(Resolve("ollama", "llama-coder"))
	fragment := strings.ToLower(s.SystemFragment)
	for _, banned := range []string{
		"bash",
		"run_shell",
		"run_test",
		"start_process",
		"test-runner",
		"background-process",
		"terminal",
		"shell",
		"git",
	} {
		if strings.Contains(fragment, banned) {
			t.Fatalf("local/no-shell fragment must not name unavailable tool path %q:\n%s", banned, s.SystemFragment)
		}
	}
}

func TestCompilerEmitsStableCapabilityOrder(t *testing.T) {
	c := DefaultCompiler{}
	prev := DefaultCompiler{}.Compile(Resolve("openai", "gpt-5-codex"))
	for i := 0; i < 8; i++ {
		got := c.Compile(Resolve("openai", "gpt-5-codex"))
		if !sameStringSlice(toCapabilityStrings(prev.Capabilities), toCapabilityStrings(got.Capabilities)) {
			t.Fatalf("compiler must emit deterministic capability order, prev=%v got=%v", prev.Capabilities, got.Capabilities)
		}
	}
}

func TestSummarizeIsJSONFriendlyAndOmitsRawFragmentsInTests(t *testing.T) {
	s := DefaultCompiler{}.Compile(Resolve("anthropic", "claude-sonnet-4-5"))
	summary := s.Summarize()
	if summary.ProfileName != string(ProfileAnthropicClaude) {
		t.Fatalf("summary ProfileName = %s, want %s", summary.ProfileName, ProfileAnthropicClaude)
	}
	if !summary.BashFirst {
		t.Fatalf("Claude summary.BashFirst must be true")
	}
	if summary.EditPrimitive != "edit_file" {
		t.Fatalf("summary EditPrimitive = %s, want edit_file", summary.EditPrimitive)
	}
	if summary.ToolCapabilityMap["edit_file"] != string(capability.CapabilityFileEdit) {
		t.Fatalf("summary must map edit_file → file.edit, got %q", summary.ToolCapabilityMap["edit_file"])
	}
	if summary.ToolCapabilityMap["bash"] != string(capability.CapabilityCommandBash) {
		t.Fatalf("summary must map bash → command.bash, got %q", summary.ToolCapabilityMap["bash"])
	}
}

func TestSurfaceHasCapabilityAndToolForCapability(t *testing.T) {
	s := DefaultCompiler{}.Compile(Resolve("openai", "gpt-5-codex"))
	if !s.HasCapability(capability.CapabilityFileEdit) {
		t.Fatal("Codex surface must have file.edit capability (via apply_patch)")
	}
	if !s.HasCapability(capability.CapabilityCommandBash) {
		t.Fatal("Codex surface must have command.bash")
	}
	if !hasCapability(s.Capabilities, capability.CapabilityCommandBackground) {
		t.Fatal("Codex surface must expose command.background through bash")
	}
	if got, ok := s.ToolForCapability(capability.CapabilityFileEdit); !ok || got != "apply_patch" {
		t.Fatalf("ToolForCapability(file.edit) = %q,%v, want apply_patch,true", got, ok)
	}
	if hidden, ok := s.HiddenToolForCapability(capability.CapabilityCommandBash); ok {
		t.Fatalf("Codex surface must not keep hidden command.bash tools, got %s", hidden)
	}
}

func TestNoProfileAdvertisesRunTestStartProcessOrGitAsDefault(t *testing.T) {
	c := DefaultCompiler{}
	cases := []Profile{
		Resolve("openai", "gpt-5-codex"),
		Resolve("openai", "gpt-5.5"),
		Resolve("openai", "gpt-4.1-mini"),
		Resolve("anthropic", "claude-sonnet-4-5"),
		Resolve("google", "gemini-2.5-pro"),
		Resolve("deepseek", "deepseek-v3.2"),
	}
	for _, p := range cases {
		s := c.Compile(p)
		for _, name := range []string{"run_shell", "run_test", "git", "start_process", "list_processes", "read_process_output", "write_stdin", "stop_process"} {
			if _, visible := s.Tools[name]; visible {
				t.Fatalf("%s/%s: surface must not advertise %s as a default tool", p.ProviderName, p.Model, name)
			}
			if _, hidden := s.HiddenTools[name]; hidden {
				t.Fatalf("%s/%s: surface must not keep %s in hidden profile output", p.ProviderName, p.Model, name)
			}
		}
		for _, hiddenName := range []string{"run_shell", "run_test", "start_process", "list_processes", "read_process_output", "write_stdin", "stop_process", "structured git tool"} {
			if contains(s.SystemFragment, hiddenName) {
				t.Fatalf("%s/%s: prompt must not advertise hidden command tool %q:\n%s", p.ProviderName, p.Model, hiddenName, s.SystemFragment)
			}
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────

func hasCapability(caps []capability.Capability, want capability.Capability) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}

func contains(haystack, needle string) bool {
	return len(haystack) > 0 && len(needle) > 0 && (haystack == needle ||
		indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	// Avoid pulling in strings just for a containment check; we
	// also want a deterministic error message in the failure path.
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func toCapabilityStrings(caps []capability.Capability) []string {
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		out = append(out, string(c))
	}
	return out
}

func sortedKeys(m map[string]capability.Capability) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
