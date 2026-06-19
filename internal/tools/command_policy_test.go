package tools

import (
	"testing"

	"github.com/blueberrycongee/wuu/internal/capability"
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
		{command: "pytest -k smoke", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-pytest"},
		{command: "go test ./...", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-go-test"},
		{command: "cargo test --workspace", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-cargo-test"},
		{command: "npm test", cap: capability.CapabilityCommandBash, want: CommandPolicyAsk, wantName: "bash-npm-test"},
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
}

func TestDecideCommandPolicyReturnsNoMatchForUnrelatedCapability(t *testing.T) {
	rules := DefaultCommandPolicyRules()
	// command.bash pattern "git status" should not match file.edit.
	_, _, ok := DecideCommandPolicy(rules, capability.CapabilityFileEdit, "git status")
	if ok {
		t.Fatal("file.edit should not match a command.bash rule")
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
