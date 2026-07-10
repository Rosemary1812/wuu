package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeProjectSettings writes a project-scoped settings layer file
// (<workdir>/.wuu/<name>) and returns its path.
func writeProjectSettings(t *testing.T, workdir, name, contents string) string {
	t.Helper()
	dir := filepath.Join(workdir, projectSettingsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// writeBaseConfig writes a workspace .wuu.json base config and returns its path.
func writeBaseConfig(t *testing.T, workdir, contents string) string {
	t.Helper()
	path := filepath.Join(workdir, localPrimaryConfig)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write base config: %v", err)
	}
	return path
}

// isolatedHome returns an empty temp dir usable as HOME with no global config,
// and neutralizes WUU_HOME so statepath does not point at a real user home.
func isolatedHome(t *testing.T) string {
	t.Helper()
	t.Setenv("WUU_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

const layerBaseConfigJSON = `{
  "default_provider": "main",
  "providers": {
    "main": {
      "type": "openai-compatible",
      "base_url": "https://base.example/v1",
      "api_key_env": "MAIN_KEY",
      "model": "base-model"
    }
  },
  "agent": {
    "max_steps": 5,
    "append_system_prompt": "base prompt"
  },
  "memory": {
    "filenames": ["BASE.md"]
  }
}`

// TestLoadFrom_NoSettingsLayers_MatchesBase verifies that when neither project
// settings layer file exists, LoadFrom returns exactly the base config and its
// path, i.e. the pre-layering behavior is unchanged.
func TestLoadFrom_NoSettingsLayers_MatchesBase(t *testing.T) {
	home := isolatedHome(t)
	workdir := t.TempDir()
	basePath := writeBaseConfig(t, workdir, layerBaseConfigJSON)

	cfg, path, err := LoadFrom(workdir, home)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if path != basePath {
		t.Fatalf("path = %q, want base %q", path, basePath)
	}
	if got := cfg.Providers["main"].Model; got != "base-model" {
		t.Fatalf("model = %q, want base-model (base must be untouched)", got)
	}
	if cfg.Agent.MaxSteps != 5 {
		t.Fatalf("max_steps = %d, want 5", cfg.Agent.MaxSteps)
	}
	if cfg.Agent.Effort != "" {
		t.Fatalf("effort = %q, want empty (no layer applied)", cfg.Agent.Effort)
	}
	if len(cfg.Memory.Filenames) != 1 || cfg.Memory.Filenames[0] != "BASE.md" {
		t.Fatalf("memory.filenames = %v, want [BASE.md]", cfg.Memory.Filenames)
	}
}

func TestLoadFrom_LocalSettingsCarriesExtensionGrants(t *testing.T) {
	home := isolatedHome(t)
	workdir := t.TempDir()
	writeBaseConfig(t, workdir, layerBaseConfigJSON)
	writeProjectSettings(t, workdir, localSettingsFile, `{
  "extensions": {
    "grants": {
      "mcp:project:docs": {
        "subject_id": "mcp:project:docs",
        "fingerprint": "abc123",
        "scope": "project",
        "permissions": ["network.connect"],
        "approved_at": "2026-07-10T08:00:00Z"
      }
    }
  }
}`)

	cfg, _, err := LoadFrom(workdir, home)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Extensions == nil {
		t.Fatal("Extensions is nil")
	}
	grant, ok := cfg.Extensions.FindGrant("mcp:project:docs", "abc123")
	if !ok || grant.Scope != "project" {
		t.Fatalf("grant = %+v, ok=%v", grant, ok)
	}
	if _, ok := cfg.Extensions.FindGrant("mcp:project:docs", "changed"); ok {
		t.Fatal("changed fingerprint reused a local grant")
	}
}

func TestLoadFrom_SharedSettingsCannotGrantExtensions(t *testing.T) {
	home := isolatedHome(t)
	workdir := t.TempDir()
	writeBaseConfig(t, workdir, layerBaseConfigJSON)
	writeProjectSettings(t, workdir, sharedSettingsFile, `{
  "extensions": {
    "grants": {
      "hook:project:test": {
        "subject_id": "hook:project:test",
        "fingerprint": "shared",
        "scope": "project",
        "approved_at": "2026-07-10T08:00:00Z"
      }
    }
  }
}`)

	cfg, _, err := LoadFrom(workdir, home)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Extensions != nil {
		if _, ok := cfg.Extensions.FindGrant("hook:project:test", "shared"); ok {
			t.Fatal("shared settings granted an executable extension")
		}
	}
}

// TestLoadFrom_SettingsLayerDeepMerge verifies deep-merge priority: objects
// merge recursively, arrays replace wholesale, and settings.local.json outranks
// settings.json which outranks the base config.
func TestLoadFrom_SettingsLayerDeepMerge(t *testing.T) {
	home := isolatedHome(t)
	workdir := t.TempDir()
	basePath := writeBaseConfig(t, workdir, layerBaseConfigJSON)

	shared := `{
  "providers": {
    "main": { "model": "shared-model" },
    "team": {
      "type": "anthropic",
      "base_url": "https://team.example",
      "api_key_env": "TEAM_KEY",
      "model": "team-model"
    }
  },
  "agent": { "effort": "high" },
  "memory": { "filenames": ["SHARED.md", "SHARED2.md"] }
}`
	local := `{
  "providers": {
    "main": { "model": "local-model" }
  },
  "agent": {
    "effort": "low",
    "append_system_prompt": "local prompt"
  }
}`
	writeProjectSettings(t, workdir, sharedSettingsFile, shared)
	writeProjectSettings(t, workdir, localSettingsFile, local)

	cfg, path, err := LoadFrom(workdir, home)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	// The returned path stays the writable base config, not a layer.
	if path != basePath {
		t.Fatalf("path = %q, want base %q", path, basePath)
	}

	// Leaf precedence: local > shared > base.
	if got := cfg.Providers["main"].Model; got != "local-model" {
		t.Fatalf("providers.main.model = %q, want local-model", got)
	}
	// Deep object merge: base-only fields on providers.main survive.
	if got := cfg.Providers["main"].APIKeyEnv; got != "MAIN_KEY" {
		t.Fatalf("providers.main.api_key_env = %q, want MAIN_KEY (base field must survive merge)", got)
	}
	// Shared layer introduced a whole new provider.
	team, ok := cfg.Providers["team"]
	if !ok {
		t.Fatalf("providers.team missing; shared layer provider not merged")
	}
	if team.Model != "team-model" || team.Type != "anthropic" {
		t.Fatalf("providers.team = %+v, want anthropic/team-model", team)
	}
	// agent.effort: local wins over shared.
	if cfg.Agent.Effort != "low" {
		t.Fatalf("agent.effort = %q, want low (local outranks shared)", cfg.Agent.Effort)
	}
	// agent.append_system_prompt: local wins over base.
	if cfg.Agent.AppendSystemPrompt != "local prompt" {
		t.Fatalf("agent.append_system_prompt = %q, want 'local prompt'", cfg.Agent.AppendSystemPrompt)
	}
	// agent.max_steps: only set in base, preserved through both layers.
	if cfg.Agent.MaxSteps != 5 {
		t.Fatalf("agent.max_steps = %d, want 5 (base value preserved)", cfg.Agent.MaxSteps)
	}
	// Arrays replace wholesale — shared's 2-element list replaces base's, not merged.
	if len(cfg.Memory.Filenames) != 2 ||
		cfg.Memory.Filenames[0] != "SHARED.md" || cfg.Memory.Filenames[1] != "SHARED2.md" {
		t.Fatalf("memory.filenames = %v, want [SHARED.md SHARED2.md] (array replace)", cfg.Memory.Filenames)
	}
}

// TestLoadFrom_SharedLayerStripsCredentials verifies inline provider secrets in
// the shared settings.json are ignored (with a stderr warning) while env-based
// indirection fields are preserved.
func TestLoadFrom_SharedLayerStripsCredentials(t *testing.T) {
	home := isolatedHome(t)
	workdir := t.TempDir()
	writeBaseConfig(t, workdir, layerBaseConfigJSON)

	shared := `{
  "providers": {
    "main": {
      "api_key": "sk-shared-should-be-stripped",
      "auth_token": "tok-shared-should-be-stripped",
      "api_key_env": "SHARED_MAIN_KEY"
    }
  }
}`
	sharedPath := writeProjectSettings(t, workdir, sharedSettingsFile, shared)

	var cfg Config
	out := captureStderr(t, func() {
		var err error
		cfg, _, err = LoadFrom(workdir, home)
		if err != nil {
			t.Fatalf("LoadFrom: %v", err)
		}
	})

	if got := cfg.Providers["main"].APIKey; got != "" {
		t.Fatalf("providers.main.api_key = %q, want empty (must be stripped from shared layer)", got)
	}
	if got := cfg.Providers["main"].AuthToken; got != "" {
		t.Fatalf("providers.main.auth_token = %q, want empty (must be stripped from shared layer)", got)
	}
	// The env-indirection field is allowed and must override the base value.
	if got := cfg.Providers["main"].APIKeyEnv; got != "SHARED_MAIN_KEY" {
		t.Fatalf("providers.main.api_key_env = %q, want SHARED_MAIN_KEY (env fields are honored)", got)
	}
	if !strings.Contains(out, "providers.main.api_key") || !strings.Contains(out, "providers.main.auth_token") {
		t.Fatalf("expected credential-strip warnings on stderr, got %q", out)
	}
	if !strings.Contains(out, sharedPath) {
		t.Fatalf("warning should name the shared layer path %q, got %q", sharedPath, out)
	}
}

// TestLoadFrom_LocalLayerHonorsCredentials verifies settings.local.json (the
// machine-local layer) is trusted for inline secrets and emits no warning.
func TestLoadFrom_LocalLayerHonorsCredentials(t *testing.T) {
	home := isolatedHome(t)
	workdir := t.TempDir()
	writeBaseConfig(t, workdir, layerBaseConfigJSON)

	local := `{
  "providers": {
    "main": { "api_key": "sk-local-trusted" }
  }
}`
	writeProjectSettings(t, workdir, localSettingsFile, local)

	var cfg Config
	out := captureStderr(t, func() {
		var err error
		cfg, _, err = LoadFrom(workdir, home)
		if err != nil {
			t.Fatalf("LoadFrom: %v", err)
		}
	})

	if got := cfg.Providers["main"].APIKey; got != "sk-local-trusted" {
		t.Fatalf("providers.main.api_key = %q, want sk-local-trusted (local layer is trusted)", got)
	}
	if strings.Contains(out, "ignoring") {
		t.Fatalf("local layer credentials must not be stripped or warned about, got %q", out)
	}
}

// TestLoadFrom_LocalLayerCanRestoreStrippedSecret verifies that a secret
// stripped from the shared layer can still be supplied by the trusted local
// layer, which has the highest priority.
func TestLoadFrom_LocalLayerCanRestoreStrippedSecret(t *testing.T) {
	home := isolatedHome(t)
	workdir := t.TempDir()
	writeBaseConfig(t, workdir, layerBaseConfigJSON)

	writeProjectSettings(t, workdir, sharedSettingsFile, `{
  "providers": { "main": { "api_key": "sk-shared-stripped" } }
}`)
	writeProjectSettings(t, workdir, localSettingsFile, `{
  "providers": { "main": { "api_key": "sk-local-wins" } }
}`)

	cfg, _, err := LoadFrom(workdir, home)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got := cfg.Providers["main"].APIKey; got != "sk-local-wins" {
		t.Fatalf("providers.main.api_key = %q, want sk-local-wins", got)
	}
}

// TestLoadFrom_SettingsLayerRejectsUnknownField verifies layers are parsed with
// the same strict DisallowUnknownFields decoder as config.json.
func TestLoadFrom_SettingsLayerRejectsUnknownField(t *testing.T) {
	home := isolatedHome(t)
	workdir := t.TempDir()
	writeBaseConfig(t, workdir, layerBaseConfigJSON)

	sharedPath := writeProjectSettings(t, workdir, sharedSettingsFile, `{
  "agent": { "not_a_real_field": true }
}`)

	_, _, err := LoadFrom(workdir, home)
	if err == nil {
		t.Fatalf("expected error for unknown field in settings layer")
	}
	if !strings.Contains(err.Error(), sharedPath) {
		t.Fatalf("error should name the offending layer %q, got %v", sharedPath, err)
	}
}

// TestLoadFrom_EmptyLayerIsNoOp verifies an empty/whitespace layer file behaves
// like a missing one instead of failing to parse.
func TestLoadFrom_EmptyLayerIsNoOp(t *testing.T) {
	home := isolatedHome(t)
	workdir := t.TempDir()
	writeBaseConfig(t, workdir, layerBaseConfigJSON)
	writeProjectSettings(t, workdir, sharedSettingsFile, "   \n")

	cfg, _, err := LoadFrom(workdir, home)
	if err != nil {
		t.Fatalf("LoadFrom with empty layer: %v", err)
	}
	if cfg.Providers["main"].Model != "base-model" {
		t.Fatalf("empty layer changed config: %+v", cfg.Providers["main"])
	}
}

// TestLoadFrom_SettingsLayerDebugLog verifies the applied layers are surfaced on
// stderr when WUU_DEBUG is set, and stay quiet otherwise.
func TestLoadFrom_SettingsLayerDebugLog(t *testing.T) {
	home := isolatedHome(t)
	workdir := t.TempDir()
	writeBaseConfig(t, workdir, layerBaseConfigJSON)
	sharedPath := writeProjectSettings(t, workdir, sharedSettingsFile, `{"agent": {"effort": "high"}}`)

	// Quiet by default.
	quiet := captureStderr(t, func() {
		if _, _, err := LoadFrom(workdir, home); err != nil {
			t.Fatalf("LoadFrom: %v", err)
		}
	})
	if strings.Contains(quiet, "layered with") {
		t.Fatalf("layer debug line must be gated behind WUU_DEBUG, got %q", quiet)
	}

	// Visible with WUU_DEBUG.
	t.Setenv("WUU_DEBUG", "1")
	loud := captureStderr(t, func() {
		if _, _, err := LoadFrom(workdir, home); err != nil {
			t.Fatalf("LoadFrom: %v", err)
		}
	})
	if !strings.Contains(loud, "layered with") || !strings.Contains(loud, sharedPath) {
		t.Fatalf("expected layer provenance in debug output, got %q", loud)
	}
}
