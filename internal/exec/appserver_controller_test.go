package exec

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalAppServerControllerInitializeAndResumeThread(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".wuu.json")
	if err := os.WriteFile(configPath, []byte(`{
		"default_provider": "test",
		"providers": {
			"test": {
				"type": "openai-compatible",
				"base_url": "https://example.test/v1",
				"api_key": "sk-test",
				"model": "gpt-test"
			}
		},
		"agent": {
			"tool_policy": {
				"profile": "auto"
			}
		}
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	controller, err := NewLocalAppServerController(ctx, Options{
		Workdir:    root,
		AllowTools: []string{"run_shell"},
		DenyTools:  []string{"write_file"},
	})
	if err != nil {
		t.Fatalf("NewLocalAppServerController: %v", err)
	}
	defer controller.Shutdown(context.Background())

	init, err := controller.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if init.WorkspaceRoot != root || init.Provider != "test" || init.Model != "gpt-test" {
		t.Fatalf("unexpected initialize result: %+v", init)
	}
	if init.ToolPolicy.Tools["run_shell"] != "allow" || init.ToolPolicy.Tools["write_file"] != "deny" {
		t.Fatalf("tool overrides were not applied: %+v", init.ToolPolicy)
	}

	thread, err := controller.StartThread(ctx, false)
	if err != nil {
		t.Fatalf("StartThread: %v", err)
	}
	if thread.ID == "" || thread.Ephemeral {
		t.Fatalf("unexpected started thread: %+v", thread)
	}
	resumed, err := controller.ResumeThread(ctx, thread.ID)
	if err != nil {
		t.Fatalf("ResumeThread: %v", err)
	}
	if resumed.ID != thread.ID {
		t.Fatalf("resumed thread = %q, want %q", resumed.ID, thread.ID)
	}
}
