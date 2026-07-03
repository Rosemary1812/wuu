package exec

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/config"
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
			"permission_mode": "agent"
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

func TestLocalAppServerControllerEffortOverrideClearsConfiguredVariant(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".wuu.json")
	if err := os.WriteFile(configPath, []byte(`{
		"default_provider": "test",
		"providers": {
			"test": {
				"type": "openai-compatible",
				"base_url": "https://example.test/v1",
				"api_key": "sk-test",
				"model": "gpt-5.5"
			}
		},
		"agent": {
			"effort": "xhigh",
			"variant": "xhigh"
		}
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	controller, err := NewLocalAppServerController(ctx, Options{
		Workdir: root,
		Effort:  "low",
		NoTools: true,
	})
	if err != nil {
		t.Fatalf("NewLocalAppServerController: %v", err)
	}
	defer controller.Shutdown(context.Background())

	init, err := controller.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if init.Effort != "low" || init.Variant != "low" {
		t.Fatalf("effort override should select low variant, got effort=%q variant=%q", init.Effort, init.Variant)
	}
}

func TestApplyConfigOverridesEffortClearsConfiguredVariant(t *testing.T) {
	cfg := config.Config{
		Agent: config.AgentConfig{
			Effort:  "xhigh",
			Variant: "xhigh",
		},
	}

	if err := applyConfigOverrides(&cfg, Options{Effort: "low"}); err != nil {
		t.Fatalf("applyConfigOverrides: %v", err)
	}

	if cfg.Agent.Effort != "low" {
		t.Fatalf("Effort = %q, want low", cfg.Agent.Effort)
	}
	if cfg.Agent.Variant != "" {
		t.Fatalf("Variant should be cleared by explicit effort override, got %q", cfg.Agent.Variant)
	}
}

func TestApplyConfigOverridesExplicitVariantWinsOverEffort(t *testing.T) {
	cfg := config.Config{
		Agent: config.AgentConfig{
			Effort:  "medium",
			Variant: "medium",
		},
	}

	if err := applyConfigOverrides(&cfg, Options{Effort: "low", Variant: "high"}); err != nil {
		t.Fatalf("applyConfigOverrides: %v", err)
	}

	if cfg.Agent.Effort != "low" {
		t.Fatalf("Effort = %q, want low", cfg.Agent.Effort)
	}
	if cfg.Agent.Variant != "high" {
		t.Fatalf("Variant = %q, want high", cfg.Agent.Variant)
	}
}

// TestApplyConfigOverridesApprovalsMode pins exec's approvals contract:
// a delegated run flows by default (approval_policy resolves to never;
// ask-classified actions run, destructive stays denied), and on_request
// review flows exist only as explicit opt-ins - --approvals
// strict/prompt, an approval handler or socket, or a configured
// auto_review reviewer.
func TestApplyConfigOverridesApprovalsMode(t *testing.T) {
	for name, tc := range map[string]struct {
		agent config.AgentConfig
		opts  Options
		want  string
	}{
		"default flows": {
			agent: config.AgentConfig{},
			opts:  Options{},
			want:  config.ApprovalPolicyNever,
		},
		"permission mode preset still flows": {
			agent: config.AgentConfig{},
			opts:  Options{PermissionMode: config.PermissionModeAgent},
			want:  config.ApprovalPolicyNever,
		},
		"config on_request is superseded by the default auto mode": {
			// config load fills approval_policy from the permission
			// mode preset, so a file value is indistinguishable from a
			// derived one; strict mode is the way to keep review flows.
			agent: config.AgentConfig{ApprovalPolicy: config.ApprovalPolicyOnRequest},
			opts:  Options{},
			want:  config.ApprovalPolicyNever,
		},
		"strict keeps the review flow": {
			agent: config.AgentConfig{},
			opts:  Options{ApprovalsMode: ApprovalsModeStrict},
			want:  config.ApprovalPolicyOnRequest,
		},
		"prompt keeps the review flow": {
			agent: config.AgentConfig{},
			opts:  Options{ApprovalsMode: ApprovalsModePrompt},
			want:  config.ApprovalPolicyOnRequest,
		},
		"handler keeps the review flow": {
			agent: config.AgentConfig{},
			opts:  Options{ApprovalHandler: "approve.sh"},
			want:  config.ApprovalPolicyOnRequest,
		},
		"socket keeps the review flow": {
			agent: config.AgentConfig{},
			opts:  Options{ApprovalSocket: "/tmp/approvals.sock"},
			want:  config.ApprovalPolicyOnRequest,
		},
		"auto_review reviewer keeps the review flow": {
			agent: config.AgentConfig{ApprovalsReviewer: config.ApprovalsReviewerAutoReview},
			opts:  Options{},
			want:  "",
		},
		"explicit auto wins over auto_review": {
			agent: config.AgentConfig{ApprovalsReviewer: config.ApprovalsReviewerAutoReview},
			opts:  Options{ApprovalsMode: ApprovalsModeAuto},
			want:  config.ApprovalPolicyNever,
		},
	} {
		cfg := config.Config{Agent: tc.agent}
		if err := applyConfigOverrides(&cfg, tc.opts); err != nil {
			t.Fatalf("%s: applyConfigOverrides: %v", name, err)
		}
		if got := cfg.Agent.ApprovalPolicy; got != tc.want {
			t.Fatalf("%s: ApprovalPolicy = %q, want %q", name, got, tc.want)
		}
	}
}

func TestApplyConfigOverridesRejectsInvalidApprovalsMode(t *testing.T) {
	cfg := config.Config{}
	if err := applyConfigOverrides(&cfg, Options{ApprovalsMode: "yolo"}); err == nil {
		t.Fatal("invalid approvals mode should be rejected")
	}
}
