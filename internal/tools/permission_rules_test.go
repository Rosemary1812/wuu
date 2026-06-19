package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

type countingApprovalReviewer struct {
	calls int
}

func (r *countingApprovalReviewer) ReviewToolApproval(context.Context, ToolApprovalReviewRequest) (ToolApprovalReview, error) {
	r.calls++
	return ToolApprovalReview{Decision: ToolApprovalDecisionApproved, Reason: "approved"}, nil
}

func TestToolPermissionRuleSetLastMatchWins(t *testing.T) {
	rules := ToolPermissionRuleSet{
		{Permission: "bash", Pattern: "*", Action: ToolPermissionDeny, Source: "test"},
		{Permission: "bash", Pattern: "git status *", Action: ToolPermissionAllow, Source: "test"},
	}
	decision, ok := rules.Decide(ToolPermissionRequest{
		Permission: "bash",
		Patterns:   []string{"git status --short"},
	})
	if !ok {
		t.Fatal("expected rule match")
	}
	if decision.Action != ToolPermissionAllow {
		t.Fatalf("decision action = %s, want allow", decision.Action)
	}
}

func TestToolkitPermissionRuleAllowBypassesCoarseApproval(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetToolPolicy(ToolPolicy{
		ToolActions: map[string]ToolPolicyAction{
			"run_shell": ToolPolicyRequireApproval,
		},
	})
	kit.SetPermissionRules(ToolPermissionRuleSet{
		{Permission: "bash", Pattern: "pwd", Action: ToolPermissionAllow, Source: "test"},
	})

	result, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"pwd"}`,
	})
	if err != nil {
		t.Fatalf("run_shell should be allowed by permission rule: %v", err)
	}
	if !strings.Contains(result, `"exit_code":0`) {
		t.Fatalf("run_shell result missing success: %s", result)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || records[0].PolicyAction != ToolPolicyAllow || records[0].PolicyReason == "" {
		t.Fatalf("telemetry should record permission allow: %+v", records)
	}
}

func TestToolkitPermissionRuleDenyBlocksSpecificPath(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetPermissionRules(ToolPermissionRuleSet{
		{Permission: "read", Pattern: "secret.txt", Action: ToolPermissionDeny, Source: "test"},
	})
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"secret.txt","limit":1}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=permission_rule_denied") {
		t.Fatalf("read_file should be denied by permission rule, got %v", err)
	}
}

func TestShellPermissionAlwaysPattern(t *testing.T) {
	tests := map[string]string{
		"git status --short":        "git status *",
		"npm run test -- --watch":   "npm run *",
		"FOO=bar go test ./...":     "go test *",
		"custom-command --flag abc": "custom-command *",
	}
	for command, want := range tests {
		if got := shellPermissionAlwaysPattern(command); got != want {
			t.Fatalf("shellPermissionAlwaysPattern(%q) = %q, want %q", command, got, want)
		}
	}
}

func TestDefaultToolPermissionPatternsFallsBackToWildcard(t *testing.T) {
	got := defaultPermissionPatterns(providers.ToolCall{Name: "apply_patch", Arguments: `{"patch":"x"}`})
	if len(got) != 1 || got[0] != "*" {
		t.Fatalf("defaultPermissionPatterns = %+v, want wildcard", got)
	}
}

func TestToolkitValidateInputRunsBeforeApproval(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetToolPolicy(ToolPolicy{
		ToolActions: map[string]ToolPolicyAction{
			"run_shell": ToolPolicyRequireApproval,
		},
	})
	reviewer := &countingApprovalReviewer{}
	kit.SetToolApprovalReviewer(reviewer)

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"   "}`,
	})
	if err == nil || !strings.Contains(err.Error(), "run_shell requires command") {
		t.Fatalf("expected validation error, got %v", err)
	}
	if reviewer.calls != 0 {
		t.Fatalf("approval reviewer should not run for invalid input, got %d calls", reviewer.calls)
	}
}

func TestToolkitRejectsNonObjectArgumentsBeforeApproval(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetToolPolicy(ToolPolicy{
		ToolActions: map[string]ToolPolicyAction{
			"run_shell": ToolPolicyRequireApproval,
		},
	})
	reviewer := &countingApprovalReviewer{}
	kit.SetToolApprovalReviewer(reviewer)

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `[]`,
	})
	if err == nil || !strings.Contains(err.Error(), "tool arguments must be a JSON object") {
		t.Fatalf("expected JSON object validation error, got %v", err)
	}
	if reviewer.calls != 0 {
		t.Fatalf("approval reviewer should not run for malformed input, got %d calls", reviewer.calls)
	}
}

func TestGitPermissionRequestUsesSubcommandPattern(t *testing.T) {
	tool := NewGitTool(&Env{})
	requests := tool.PermissionRequests(`{"subcommand":"status","args":["--short"]}`)
	if len(requests) != 1 {
		t.Fatalf("PermissionRequests returned %d requests", len(requests))
	}
	req := normalizeToolPermissionRequest(requests[0])
	if req.Permission != "git" || len(req.Patterns) != 1 || req.Patterns[0] != "status --short" {
		t.Fatalf("unexpected git permission request: %+v", req)
	}
	if len(req.Always) != 1 || req.Always[0] != "status *" {
		t.Fatalf("unexpected git always pattern: %+v", req.Always)
	}
}
