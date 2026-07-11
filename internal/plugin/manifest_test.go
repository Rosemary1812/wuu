package plugin

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadManifestNormalizesSupportedFormats(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantID     string
		wantSkills []string
		wantMCP    string
	}{
		{
			name:       "wuu",
			path:       filepath.Join("testdata", "wuu", "plugin.json"),
			wantID:     "wuu-demo",
			wantSkills: []string{"skills"},
			wantMCP:    "wuu-docs",
		},
		{
			name:       "codex camel case",
			path:       filepath.Join("testdata", "codex", ".codex-plugin", "plugin.json"),
			wantID:     "codex-demo",
			wantSkills: []string{"skills"},
			wantMCP:    "codex-docs",
		},
		{
			name:       "claude defaults and aliases",
			path:       filepath.Join("testdata", "claude", ".claude-plugin", "plugin.json"),
			wantID:     "claude-demo",
			wantSkills: nil,
			wantMCP:    "claude-docs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadManifest(tt.path, "project")
			if err != nil {
				t.Fatalf("LoadManifest: %v", err)
			}
			if got.ID != tt.wantID {
				t.Fatalf("ID = %q, want %q", got.ID, tt.wantID)
			}
			if !reflect.DeepEqual(got.Skills, tt.wantSkills) {
				t.Fatalf("Skills = %#v, want %#v", got.Skills, tt.wantSkills)
			}
			if got.MCPServers["docs"].Command != tt.wantMCP {
				t.Fatalf("MCPServers = %+v", got.MCPServers)
			}
			if got.ManifestPath != tt.path {
				t.Fatalf("ManifestPath = %q, want %q", got.ManifestPath, tt.path)
			}
		})
	}
}

func TestLoadManifestNormalizesExecutableRuntime(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ManifestFilename)
	writeFile(t, path, `{
  "id": "runtime-demo",
  "runtime": {
    "protocol": "wuu-plugin-v1",
    "command": "bin/plugin",
    "args": ["--stdio"],
    "env": {"PLUGIN_MODE": "test"},
    "timeout": 12
  }
}`)
	plugin, err := LoadManifest(path, "project")
	if err != nil {
		t.Fatal(err)
	}
	if plugin.Runtime == nil {
		t.Fatal("runtime missing")
	}
	if plugin.Runtime.Command != filepath.Join(root, "bin", "plugin") || plugin.Runtime.Protocol != "wuu-plugin-v1" {
		t.Fatalf("runtime = %+v", plugin.Runtime)
	}
	if plugin.Runtime.Timeout != 12 || !reflect.DeepEqual(plugin.Runtime.Args, []string{"--stdio"}) || plugin.Runtime.Env["PLUGIN_MODE"] != "test" {
		t.Fatalf("runtime = %+v", plugin.Runtime)
	}
}

func TestLoadManifestRejectsInvalidRuntime(t *testing.T) {
	for _, test := range []struct {
		name    string
		runtime string
	}{
		{name: "unknown protocol", runtime: `{"protocol":"other","command":"node"}`},
		{name: "missing command", runtime: `{"protocol":"wuu-plugin-v1"}`},
		{name: "escaping command", runtime: `{"protocol":"wuu-plugin-v1","command":"../plugin"}`},
		{name: "invalid env", runtime: `{"protocol":"wuu-plugin-v1","command":"node","env":{"BAD=NAME":"x"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ManifestFilename)
			writeFile(t, path, `{"id":"bad","runtime":`+test.runtime+`}`)
			if _, err := LoadManifest(path, "project"); err == nil {
				t.Fatal("expected runtime validation error")
			}
		})
	}
}

func TestLoadManifestReportsUnsupportedFields(t *testing.T) {
	got, err := LoadManifest(filepath.Join("testdata", "codex", ".codex-plugin", "plugin.json"), "project")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	want := []string{"bundledContentVariant", "commands"}
	if !reflect.DeepEqual(got.UnsupportedFields, want) {
		t.Fatalf("UnsupportedFields = %#v, want %#v", got.UnsupportedFields, want)
	}
}

func TestLoadManifestRejectsPathsOutsidePluginRoot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".codex-plugin", "plugin.json")
	writeFile(t, path, `{"name":"escape","skills":"../skills"}`)

	_, err := LoadManifest(path, "project")
	if err == nil || !strings.Contains(err.Error(), "plugin root") {
		t.Fatalf("LoadManifest error = %v, want plugin root rejection", err)
	}
}

func TestLoadManifestRejectsCommunityOfficialNativeHelper(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	writeFile(t, path, `{"id":"native","official_native_helper":{"path":"helper"}}`)

	if _, err := LoadManifest(path, "user"); err == nil || !strings.Contains(err.Error(), "official_native_helper") {
		t.Fatalf("LoadManifest error = %v, want official helper rejection", err)
	}

	got, err := LoadManifestWithOptions(path, LoadOptions{Source: "bundled", Official: true})
	if err != nil {
		t.Fatalf("official LoadManifestWithOptions: %v", err)
	}
	if !got.Official || len(got.OfficialNativeHelper) == 0 {
		t.Fatalf("official provenance/helper not preserved: %+v", got)
	}
}
