package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestDefaultCommandPolicyRulesCoversRequiredBashPatterns(t *testing.T) {
	rules := DefaultCommandPolicyRules()
	cases := []struct {
		command  string
		cap      capability.Capability
		want     CommandPolicyAction
		wantName string
	}{
		// Bash: read-only shell.
		{command: "ls -la", cap: capability.CapabilityCommandBash, want: CommandPolicyAllow, wantName: "bash-readonly-listing"},
		{command: "pwd", cap: capability.CapabilityCommandBash, want: CommandPolicyAllow, wantName: "bash-readonly-pwd"},
		{command: "echo hello", cap: capability.CapabilityCommandBash, want: CommandPolicyAllow, wantName: "bash-readonly-echo"},

		// Bash: git read-only.
		{command: "git status", cap: capability.CapabilityCommandBash, want: CommandPolicyAllow, wantName: "bash-git-status"},
		{command: "git status --short", cap: capability.CapabilityCommandBash, want: CommandPolicyAllow, wantName: "bash-git-status"},
		{command: "git diff HEAD", cap: capability.CapabilityCommandBash, want: CommandPolicyAllow, wantName: "bash-git-diff"},
		{command: "git log --oneline -5", cap: capability.CapabilityCommandBash, want: CommandPolicyAllow, wantName: "bash-git-log"},
		{command: "git show HEAD", cap: capability.CapabilityCommandBash, want: CommandPolicyAllow, wantName: "bash-git-show"},
		{command: "git branch", cap: capability.CapabilityCommandBash, want: CommandPolicyAllow, wantName: "bash-git-branch-list"},

		// Bash: git mutating.
		{command: "git add .", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-git-add"},
		{command: "git commit -m \"x\"", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-git-commit"},
		{command: "git push origin main", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-git-push"},
		{command: "git checkout main", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-git-checkout"},
		{command: "git merge feature", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-git-merge"},

		// Bash: tests.
		{command: "npx vitest run", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-vitest"},
		{command: "npx vitest run --reporter=verbose", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-vitest"},
		{command: "npx jest", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-jest"},
		{command: "npx tsc --noEmit", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-tsc-noemit"},
		{command: "npx tsc -p tsconfig.json --noEmit", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-tsc-noemit"},
		{command: "pytest -k smoke", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-pytest"},
		{command: "go test ./...", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-go-test"},
		{command: "cargo test --workspace", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-cargo-test"},
		{command: "npm test", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-npm-test"},
		{command: "npm run typecheck", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-npm-typecheck"},
		{command: "npm run build", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-npm-build"},

		// Bash: package install.
		{command: "npm install", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-npm-install"},
		{command: "npm install lodash", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-npm-install"},
		{command: "npm i lodash", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-npm-i"},
		{command: "pnpm install", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-pnpm-install"},
		{command: "pnpm add react", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-pnpm-add"},
		{command: "yarn add react", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-yarn-add"},

		// Bash: long-running.
		{command: "npm run dev", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-long-running-dev"},
		{command: "npm run dev -- --port 4000", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-long-running-dev"},
		{command: "vite", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-long-running-vite"},
		{command: "next dev", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-long-running-next"},
		{command: "pnpm dev", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-long-running-pnpm"},
		{command: "yarn dev", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-long-running-yarn"},

		// File edit: sensitive paths.
		{command: ".env", cap: capability.CapabilityFileEdit, want: CommandPolicyAsk, wantName: "edit-env"},
		{command: ".env.local", cap: capability.CapabilityFileEdit, want: CommandPolicyAsk, wantName: "edit-env-local"},
		{command: ".envrc", cap: capability.CapabilityFileEdit, want: CommandPolicyAsk, wantName: "edit-env-rc"},
		{command: ".ssh/id_rsa", cap: capability.CapabilityFileEdit, want: CommandPolicyDeny, wantName: "edit-ssh-config"},
		{command: ".aws/credentials", cap: capability.CapabilityFileEdit, want: CommandPolicyDeny, wantName: "edit-aws-creds"},
		{command: ".netrc", cap: capability.CapabilityFileEdit, want: CommandPolicyDeny, wantName: "edit-netrc"},
		{command: "certs/server.pem", cap: capability.CapabilityFileEdit, want: CommandPolicyAsk, wantName: "edit-pem"},
		{command: "keys/private.key", cap: capability.CapabilityFileEdit, want: CommandPolicyAsk, wantName: "edit-key"},

		// Background processes.
		{command: "npm run dev", cap: capability.CapabilityCommandBackground, want: CommandPolicyAsk, wantName: "background-start"},
		{command: "vite preview", cap: capability.CapabilityCommandBackground, want: CommandPolicyAsk, wantName: "background-start"},
	}
	for _, tt := range cases {
		got, _, name, ok := lookupNamedRule(rules, tt.cap, tt.command)
		if !ok {
			t.Errorf("%s/%s: no matching rule", tt.cap, tt.command)
			continue
		}
		if got != tt.want || name != tt.wantName {
			t.Errorf("%s/%s: got (%s, %s), want (%s, %s)", tt.cap, tt.command, got, name, tt.want, tt.wantName)
		}
	}
	if _, _, _, ok := lookupNamedRule(rules, capability.CapabilityCommandBash, "npx tsc --init"); ok {
		t.Fatal("npx tsc --init must not match the no-emit typecheck policy")
	}
	if _, _, _, ok := lookupNamedRule(rules, capability.CapabilityCommandBash, "npx tsc --noEmit --init"); ok {
		t.Fatal("npx tsc --noEmit --init must not match the no-emit typecheck policy")
	}
}

func TestDecideCommandPolicyReturnsNoMatchForUnrelatedCapability(t *testing.T) {
	rules := DefaultCommandPolicyRules()
	// command.bash pattern "git status" should not match file.edit.
	_, _, ok := DecideCommandPolicy(rules, capability.CapabilityFileEdit, "git status")
	if ok {
		t.Fatal("file.edit should not match a command.bash rule")
	}
}

func TestCommandPolicyTrailingWildcardUsesCommandBoundary(t *testing.T) {
	rules := DefaultCommandPolicyRules()
	if _, _, ok := DecideCommandPolicy(rules, capability.CapabilityCommandBash, "git statusx"); ok {
		t.Fatal("git status * must not match git statusx")
	}
	if got, _, ok := DecideCommandPolicy(rules, capability.CapabilityFileEdit, ".ssh/id_rsa"); !ok || got != CommandPolicyDeny {
		t.Fatalf(".ssh/* should still match path prefixes, got action=%s ok=%t", got, ok)
	}
}

func TestDecideShellCommandPolicyChoosesStrictestSegment(t *testing.T) {
	rules := DefaultCommandPolicyRules()
	decision, ok := DecideShellCommandPolicy(rules, capability.CapabilityCommandBash, "git status --short && git add internal/tools/command_policy.go")
	if !ok {
		t.Fatal("expected composite shell command policy match")
	}
	if decision.Action != CommandPolicyAsk || decision.Rule != "bash-git-add" {
		t.Fatalf("decision = %+v, want ask from bash-git-add", decision)
	}

	decision, ok = DecideShellCommandPolicy(rules, capability.CapabilityCommandBash, "timeout 10 npx vitest run")
	if !ok {
		t.Fatal("expected wrapped vitest command policy match")
	}
	if decision.Action != CommandPolicyAsk || decision.Rule != "bash-vitest" {
		t.Fatalf("wrapped decision = %+v, want ask from bash-vitest", decision)
	}

	decision, ok = DecideShellCommandPolicy(rules, capability.CapabilityCommandBash, "nice git status --short")
	if !ok {
		t.Fatal("expected wrapped git status command policy match")
	}
	if decision.Action != CommandPolicyAllow || decision.Rule != "bash-git-status" {
		t.Fatalf("wrapped git decision = %+v, want allow from bash-git-status", decision)
	}
}

func TestShellPackageNetworkMutationRequiresCoveredCommandPolicyRule(t *testing.T) {
	if !shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("npx vitest run") {
		t.Fatal("npx vitest should be covered by the default command policy")
	}
	if !shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("timeout 10 npx vitest run") {
		t.Fatal("wrapped npx vitest should be covered by the default command policy")
	}
	if !shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("cd desktop && npx tsc --noEmit") {
		t.Fatal("directory-scoped npx tsc should be covered by the default command policy")
	}
	if !shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("cd desktop && npx tsc -p tsconfig.json --noEmit") {
		t.Fatal("directory-scoped npx tsc with project option should be covered by the default command policy")
	}
	if shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("npx tsc --init") {
		t.Fatal("npx tsc --init should not be covered by the typecheck policy")
	}
	if shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("npx tsc --noEmit --init") {
		t.Fatal("npx tsc --noEmit --init should not be covered by the typecheck policy")
	}
	if shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("npx tsc --noEmit --build") {
		t.Fatal("npx tsc --noEmit --build should not be covered by the typecheck policy")
	}
	if shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("npx tsc --build") {
		t.Fatal("npx tsc --build should not be covered by the typecheck policy")
	}
	if shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("npx tsc") {
		t.Fatal("bare npx tsc should not be covered because it may emit files")
	}
	if shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("cd desktop && npx tsc --noEmit 2>&1 | tail -30") {
		t.Fatal("piped npx tsc should not be covered because it cannot be safely rewritten to a local runner")
	}
	if !shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("nice npm install left-pad") {
		t.Fatal("wrapped npm install should be covered by the default command policy")
	}
	if shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("npx vitest run && curl https://example.com") {
		t.Fatal("mixed covered and uncovered network commands should not be covered")
	}
	if shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("curl https://example.com") {
		t.Fatal("uncovered network commands should not be covered")
	}
	if !shellCommandInvokesPackageOrNetworkMutation("nice curl https://example.com") {
		t.Fatal("wrapped curl should be detected as a network mutation")
	}
	if shellCommandPackageOrNetworkMutationCoveredByCommandPolicy("nice curl https://example.com") {
		t.Fatal("wrapped uncovered network commands should not be covered")
	}
}

func TestToolkitAppliesDefaultCommandPolicyBeforeBashExecution(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.ConfigureSurfaceForProviderModel("openai", "gpt-5-codex")

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		ID:        "call-vitest",
		Name:      "bash",
		Arguments: `{"command":"npx vitest run"}`,
	})
	if err == nil {
		t.Fatal("npx vitest should require approval before execution")
	}
	if !strings.Contains(err.Error(), "error_kind=approval_required") {
		t.Fatalf("expected approval_required, got %v", err)
	}
	if strings.Contains(err.Error(), "refuses to execute package") {
		t.Fatalf("default command policy should intercept before bash hard guard, got %v", err)
	}

	records := kit.ToolTelemetry()
	if len(records) != 1 {
		t.Fatalf("expected one telemetry record, got %d", len(records))
	}
	if records[0].PolicyAction != ToolPolicyRequireApproval || !strings.Contains(records[0].PolicyReason, "bash-vitest") {
		t.Fatalf("unexpected command policy telemetry: %+v", records[0])
	}
}

func TestFullAccessProfileDoesNotAskForDefaultCommandPolicyReview(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.ConfigureSurfaceForProviderModel("openai", "gpt-5-codex")
	policy, ok := PolicyForProfile(ToolPolicyProfileFullAccess)
	if !ok {
		t.Fatal("missing full_access policy")
	}
	kit.SetToolPolicy(policy)

	info := ToolInfo{Name: "bash", Kind: ToolKindShell, Risk: ToolRiskHigh}
	base := kit.toolPolicy.Decide(info)
	got := kit.applyDefaultCommandPolicyDecision(
		providers.ToolCall{
			Name:      "bash",
			Arguments: `{"command":"git push origin main"}`,
		},
		info,
		base,
	)

	if got.Action != ToolPolicyAllow {
		t.Fatalf("full_access profile should allow command policy ask without approval, got %+v", got)
	}
	if got.Capability != capability.CapabilityCommandBash ||
		got.CapabilityObject != "git push origin main" ||
		got.CapabilityRule != "bash-git-push" {
		t.Fatalf("full_access command policy should still annotate capability fields, got %+v", got)
	}
}

func TestDefaultCommandPolicyAskFollowsApprovalPolicyAxis(t *testing.T) {
	tests := []struct {
		name   string
		policy ToolPolicy
		want   ToolPolicyAction
	}{
		{
			name: "full access profile still asks when approval policy is on request",
			policy: ToolPolicy{
				Profile:        ToolPolicyProfileFullAccess,
				ApprovalPolicy: ToolApprovalPolicyOnRequest,
			},
			want: ToolPolicyRequireApproval,
		},
		{
			name: "agent profile skips ask when approval policy is never",
			policy: ToolPolicy{
				Profile:        ToolPolicyProfileAgent,
				ApprovalPolicy: ToolApprovalPolicyNever,
			},
			want: ToolPolicyAllow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kit, err := New(t.TempDir())
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			kit.ConfigureSurfaceForProviderModel("openai", "gpt-5-codex")
			kit.SetToolPolicy(tt.policy)

			info := ToolInfo{Name: "bash", Kind: ToolKindShell, Risk: ToolRiskHigh}
			got := kit.applyDefaultCommandPolicyDecision(
				providers.ToolCall{
					Name:      "bash",
					Arguments: `{"command":"git push origin main"}`,
				},
				info,
				kit.toolPolicy.Decide(info),
			)
			if got.Action != tt.want {
				t.Fatalf("Action = %s, want %s: %+v", got.Action, tt.want, got)
			}
		})
	}
}

func TestDefaultCommandPolicyAskDoesNotOverrideExplicitDeny(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.ConfigureSurfaceForProviderModel("openai", "gpt-5-codex")
	kit.SetToolPolicy(ToolPolicy{
		Profile: ToolPolicyProfileFullAccess,
		ToolActions: map[string]ToolPolicyAction{
			"bash": ToolPolicyDeny,
		},
	})

	info := ToolInfo{Name: "bash", Kind: ToolKindShell, Risk: ToolRiskHigh}
	base := kit.toolPolicy.Decide(info)
	got := kit.applyDefaultCommandPolicyDecision(
		providers.ToolCall{
			Name:      "bash",
			Arguments: `{"command":"git push origin main"}`,
		},
		info,
		base,
	)

	if got.Action != ToolPolicyDeny || got.Reason != "tool policy" {
		t.Fatalf("explicit deny should not be softened by command policy ask, got %+v", got)
	}
}

func TestDefaultCommandPolicyRulesNamesAreUnique(t *testing.T) {
	rules := DefaultCommandPolicyRules()
	seen := map[string]struct{}{}
	for _, rule := range rules {
		if _, dup := seen[rule.Name]; dup {
			t.Errorf("duplicate rule name %q", rule.Name)
		}
		seen[rule.Name] = struct{}{}
	}
}

func TestDefaultCommandPolicyRulesActionsAreValid(t *testing.T) {
	rules := DefaultCommandPolicyRules()
	for _, rule := range rules {
		if !IsValidCommandPolicyAction(rule.Action) {
			t.Errorf("rule %q has invalid action %q", rule.Name, rule.Action)
		}
		if rule.Pattern == "" {
			t.Errorf("rule %q has empty pattern", rule.Name)
		}
		if rule.Reason == "" {
			t.Errorf("rule %q has empty reason", rule.Name)
		}
	}
}

func TestDefaultCommandPolicyRulesCapabilitiesAreKnown(t *testing.T) {
	rules := DefaultCommandPolicyRules()
	for _, rule := range rules {
		if rule.Capability == "" {
			t.Errorf("rule %q has empty capability", rule.Name)
			continue
		}
		found := false
		for _, c := range capability.All() {
			if c == rule.Capability {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("rule %q uses unknown capability %q", rule.Name, rule.Capability)
		}
	}
}

// lookupNamedRule is a test-only helper that returns the action,
// reason, name, and match-status of the first rule that matches
// the given capability and command.
func lookupNamedRule(rules []CommandPolicyRule, cap capability.Capability, command string) (CommandPolicyAction, string, string, bool) {
	action, reason, ok := DecideCommandPolicy(rules, cap, command)
	if !ok {
		return "", "", "", false
	}
	for _, rule := range rules {
		if rule.Capability != cap {
			continue
		}
		if !matchCommandPolicyPattern(rule.Pattern, command) {
			continue
		}
		return action, reason, rule.Name, true
	}
	return action, reason, "", true
}
