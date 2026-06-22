package tools

import (
	"strings"

	"github.com/blueberrycongee/wuu/internal/capability"
)

// CommandPolicyAction is the user-facing decision the harness makes
// for one capability call. The legacy tool-name policy and the new
// capability + pattern policy both lower-case to one of these.
//
// The four actions in the contract:
//
//   - allow  — proceed silently; the call is part of routine work.
//   - ask    — surface an approval request; the call may proceed
//     once the user (or an auto-mode classifier) decides.
//   - deny   — block the call and return an error that tells the
//     model how to recover (e.g. "use bash to run npx vitest").
//   - explain — same as deny but the recovery message is the
//     primary payload; the harness never tries to rerun the call.
type CommandPolicyAction string

const (
	CommandPolicyAllow   CommandPolicyAction = "allow"
	CommandPolicyAsk     CommandPolicyAction = "ask"
	CommandPolicyDeny    CommandPolicyAction = "deny"
	CommandPolicyExplain CommandPolicyAction = "explain"
)

// IsValidCommandPolicyAction reports whether the given action is
// one of the four the contract recognises. The capability + pattern
// ruleset uses these to drive approval, denial, and recovery.
func IsValidCommandPolicyAction(a CommandPolicyAction) bool {
	switch a {
	case "", CommandPolicyAllow, CommandPolicyAsk, CommandPolicyDeny, CommandPolicyExplain:
		return true
	}
	return false
}

// CommandPolicyRule names a single capability + pattern → action
// decision. Rules are matched in declaration order; the first match
// wins. Name is the stable identifier used in telemetry and
// approval UI ("blocked by command policy rule 'bash-git-push'").
type CommandPolicyRule struct {
	Name       string
	Capability capability.Capability
	Pattern    string
	Action     CommandPolicyAction
	Reason     string
}

// DefaultCommandPolicyRules returns the canonical Wuu bash-first
// policy ruleset. The rules are intentionally broad: the per-call
// policy in the existing string-denied shell functions narrows the
// set further, but those denials are now expressed as a deny
// pattern here so the model sees a consistent allow/ask/deny
// surface across all tools.
//
// Pattern grammar: a trailing "*" wildcard in the pattern matches
// any non-empty continuation, so "git status *" matches both
// "git status" (bare) and "git status --short" (with args). This
// keeps the rule count small and avoids a duplicate "git status"
// entry that would only differ from "git status *" by an empty
// argument list.
func DefaultCommandPolicyRules() []CommandPolicyRule {
	return []CommandPolicyRule{
		// Read-only shell commands: allow silently.
		{Name: "bash-readonly-listing", Capability: capability.CapabilityCommandBash, Pattern: "ls *", Action: CommandPolicyAllow, Reason: "read-only directory listing"},
		{Name: "bash-readonly-pwd", Capability: capability.CapabilityCommandBash, Pattern: "pwd *", Action: CommandPolicyAllow, Reason: "read-only path query"},
		{Name: "bash-readonly-echo", Capability: capability.CapabilityCommandBash, Pattern: "echo *", Action: CommandPolicyAllow, Reason: "read-only echo"},
		{Name: "bash-readonly-which", Capability: capability.CapabilityCommandBash, Pattern: "which *", Action: CommandPolicyAllow, Reason: "read-only command lookup"},
		{Name: "bash-readonly-env-print", Capability: capability.CapabilityCommandBash, Pattern: "env *", Action: CommandPolicyAllow, Reason: "read-only environment listing"},

		// Git: status, diff, log, show, branch are read-only → allow.
		{Name: "bash-git-status", Capability: capability.CapabilityCommandBash, Pattern: "git status *", Action: CommandPolicyAllow, Reason: "git status is read-only"},
		{Name: "bash-git-diff", Capability: capability.CapabilityCommandBash, Pattern: "git diff *", Action: CommandPolicyAllow, Reason: "git diff is read-only"},
		{Name: "bash-git-log", Capability: capability.CapabilityCommandBash, Pattern: "git log *", Action: CommandPolicyAllow, Reason: "git log is read-only"},
		{Name: "bash-git-show", Capability: capability.CapabilityCommandBash, Pattern: "git show *", Action: CommandPolicyAllow, Reason: "git show is read-only"},
		{Name: "bash-git-branch-list", Capability: capability.CapabilityCommandBash, Pattern: "git branch *", Action: CommandPolicyAllow, Reason: "git branch listing"},

		// Git: stage / commit / push need approval.
		{Name: "bash-git-add", Capability: capability.CapabilityCommandBash, Pattern: "git add *", Action: CommandPolicyAsk, Reason: "git add stages the working tree for commit"},
		{Name: "bash-git-commit", Capability: capability.CapabilityCommandBash, Pattern: "git commit *", Action: CommandPolicyAsk, Reason: "git commit writes local history"},
		{Name: "bash-git-push", Capability: capability.CapabilityCommandBash, Pattern: "git push *", Action: CommandPolicyAsk, Reason: "git push writes to the remote"},
		{Name: "bash-git-checkout", Capability: capability.CapabilityCommandBash, Pattern: "git checkout *", Action: CommandPolicyAsk, Reason: "git checkout switches the working tree"},
		{Name: "bash-git-merge", Capability: capability.CapabilityCommandBash, Pattern: "git merge *", Action: CommandPolicyAsk, Reason: "git merge combines histories"},

		// Test and verification runners: ask. The legacy harness used
		// to reject npx vitest inside run_shell and redirect to
		// run_test. Under the bash-first surface bash is the only test
		// entry point, and these patterns route the call to an approval
		// request instead of a hard error so the user can opt in.
		{Name: "bash-vitest", Capability: capability.CapabilityCommandBash, Pattern: "npx vitest *", Action: CommandPolicyAsk, Reason: "vitest test runner"},
		{Name: "bash-jest", Capability: capability.CapabilityCommandBash, Pattern: "npx jest *", Action: CommandPolicyAsk, Reason: "jest test runner"},
		{Name: "bash-tsc-noemit", Capability: capability.CapabilityCommandBash, Pattern: "npx tsc --noEmit *", Action: CommandPolicyAsk, Reason: "TypeScript no-emit typecheck runner"},
		{Name: "bash-pytest", Capability: capability.CapabilityCommandBash, Pattern: "pytest *", Action: CommandPolicyAsk, Reason: "pytest test runner"},
		{Name: "bash-go-test", Capability: capability.CapabilityCommandBash, Pattern: "go test *", Action: CommandPolicyAsk, Reason: "go test runner"},
		{Name: "bash-cargo-test", Capability: capability.CapabilityCommandBash, Pattern: "cargo test *", Action: CommandPolicyAsk, Reason: "cargo test runner"},

		// Build commands: ask.
		{Name: "bash-go-build", Capability: capability.CapabilityCommandBash, Pattern: "go build *", Action: CommandPolicyAsk, Reason: "go build emits binaries"},
		{Name: "bash-npm-build", Capability: capability.CapabilityCommandBash, Pattern: "npm run build *", Action: CommandPolicyAsk, Reason: "npm build runs the project build script"},
		{Name: "bash-npm-test", Capability: capability.CapabilityCommandBash, Pattern: "npm test *", Action: CommandPolicyAsk, Reason: "npm test runs the project test script"},
		{Name: "bash-npm-typecheck", Capability: capability.CapabilityCommandBash, Pattern: "npm run typecheck *", Action: CommandPolicyAsk, Reason: "npm typecheck runs the project typecheck script"},

		// Package install: ask. Install commands can mutate the
		// network, the lockfile, and the dependency graph; the
		// approval step lets the user opt in once per session.
		{Name: "bash-npm-install", Capability: capability.CapabilityCommandBash, Pattern: "npm install *", Action: CommandPolicyAsk, Reason: "npm install mutates node_modules and lockfile"},
		{Name: "bash-npm-i", Capability: capability.CapabilityCommandBash, Pattern: "npm i *", Action: CommandPolicyAsk, Reason: "npm i mutates node_modules and lockfile"},
		{Name: "bash-pnpm-install", Capability: capability.CapabilityCommandBash, Pattern: "pnpm install *", Action: CommandPolicyAsk, Reason: "pnpm install mutates node_modules and lockfile"},
		{Name: "bash-pnpm-add", Capability: capability.CapabilityCommandBash, Pattern: "pnpm add *", Action: CommandPolicyAsk, Reason: "pnpm add mutates node_modules and lockfile"},
		{Name: "bash-yarn-add", Capability: capability.CapabilityCommandBash, Pattern: "yarn add *", Action: CommandPolicyAsk, Reason: "yarn add mutates node_modules and lockfile"},
		{Name: "bash-pip-install", Capability: capability.CapabilityCommandBash, Pattern: "pip install *", Action: CommandPolicyAsk, Reason: "pip install mutates site-packages"},
		{Name: "bash-uv-add", Capability: capability.CapabilityCommandBash, Pattern: "uv add *", Action: CommandPolicyAsk, Reason: "uv add mutates project dependencies"},

		// Long-lived processes: ask. The bash-first surface still
		// routes long-running commands through bash, but the user
		// gets an approval step so they can opt in or redirect to
		// a managed background process.
		{Name: "bash-long-running-dev", Capability: capability.CapabilityCommandBash, Pattern: "npm run dev *", Action: CommandPolicyAsk, Reason: "dev server may run indefinitely"},
		{Name: "bash-long-running-start", Capability: capability.CapabilityCommandBash, Pattern: "npm run start *", Action: CommandPolicyAsk, Reason: "long-lived process"},
		{Name: "bash-long-running-vite", Capability: capability.CapabilityCommandBash, Pattern: "vite *", Action: CommandPolicyAsk, Reason: "vite dev server"},
		{Name: "bash-long-running-next", Capability: capability.CapabilityCommandBash, Pattern: "next dev *", Action: CommandPolicyAsk, Reason: "next dev server"},
		{Name: "bash-long-running-pnpm", Capability: capability.CapabilityCommandBash, Pattern: "pnpm dev *", Action: CommandPolicyAsk, Reason: "pnpm dev server"},
		{Name: "bash-long-running-yarn", Capability: capability.CapabilityCommandBash, Pattern: "yarn dev *", Action: CommandPolicyAsk, Reason: "yarn dev server"},

		// File edit: sensitive paths ask or deny outright.
		{Name: "edit-env", Capability: capability.CapabilityFileEdit, Pattern: "*.env", Action: CommandPolicyAsk, Reason: "sensitive environment file"},
		{Name: "edit-env-local", Capability: capability.CapabilityFileEdit, Pattern: "*.env.local", Action: CommandPolicyAsk, Reason: "sensitive environment file"},
		{Name: "edit-env-rc", Capability: capability.CapabilityFileEdit, Pattern: ".envrc *", Action: CommandPolicyAsk, Reason: "sensitive environment file"},
		{Name: "edit-ssh-config", Capability: capability.CapabilityFileEdit, Pattern: ".ssh/*", Action: CommandPolicyDeny, Reason: "ssh credential directory"},
		{Name: "edit-aws-creds", Capability: capability.CapabilityFileEdit, Pattern: ".aws/credentials", Action: CommandPolicyDeny, Reason: "aws credential file"},
		{Name: "edit-netrc", Capability: capability.CapabilityFileEdit, Pattern: ".netrc *", Action: CommandPolicyDeny, Reason: "netrc credential file"},
		{Name: "edit-pem", Capability: capability.CapabilityFileEdit, Pattern: "*.pem", Action: CommandPolicyAsk, Reason: "PEM key/certificate"},
		{Name: "edit-key", Capability: capability.CapabilityFileEdit, Pattern: "*.key", Action: CommandPolicyAsk, Reason: "private key material"},

		// External directory access: ask.
		{Name: "file-read-external", Capability: capability.CapabilityFileRead, Pattern: "**/../*", Action: CommandPolicyAsk, Reason: "path escapes the workspace root"},
		{Name: "file-edit-external", Capability: capability.CapabilityFileEdit, Pattern: "**/../*", Action: CommandPolicyAsk, Reason: "path escapes the workspace root"},

		// Background processes: every long-lived start is a separate
		// approval so the user can see the command before it begins.
		{Name: "background-start", Capability: capability.CapabilityCommandBackground, Pattern: "*", Action: CommandPolicyAsk, Reason: "managed background process is long-lived"},
	}
}

// DecideCommandPolicy returns the first matching rule's action and
// reason for a capability call against a command string. Returns
// ("", "", false) when no rule matches so the caller can apply its
// own fallback (typically the runtime tool policy or an approval
// request).
func DecideCommandPolicy(rules []CommandPolicyRule, cap capability.Capability, command string) (CommandPolicyAction, string, bool) {
	decision, ok := DecideNamedCommandPolicy(rules, cap, command)
	if !ok {
		return "", "", false
	}
	return decision.Action, decision.Reason, true
}

// DecideNamedCommandPolicy returns the full matching rule decision,
// including the stable rule name used by telemetry and approval UI.
func DecideNamedCommandPolicy(rules []CommandPolicyRule, cap capability.Capability, command string) (CommandPolicyDecision, bool) {
	command = strings.TrimSpace(command)
	for _, rule := range rules {
		if rule.Capability != cap {
			continue
		}
		if !matchCommandPolicyPattern(rule.Pattern, command) {
			continue
		}
		return CommandPolicyDecision{
			Action: rule.Action,
			Reason: rule.Reason,
			Rule:   rule.Name,
		}, true
	}
	return CommandPolicyDecision{}, false
}

// DecideShellCommandPolicy evaluates a shell command as a sequence of
// command segments and returns the strictest matching decision. This
// prevents a composite command such as "git status && git add file"
// from being allowed just because the first segment is read-only.
func DecideShellCommandPolicy(rules []CommandPolicyRule, cap capability.Capability, command string) (CommandPolicyDecision, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return CommandPolicyDecision{}, false
	}
	segments, ok := splitShellCommandSegmentsQuoted(command)
	if !ok {
		segments = splitShellCommandSegments(command)
	}
	if len(segments) == 0 {
		segments = []string{command}
	}
	var best CommandPolicyDecision
	matched := false
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		decision, ok := DecideNamedCommandPolicy(rules, cap, commandPolicySubjectForShellSegment(segment))
		if !ok {
			continue
		}
		if !matched || commandPolicyActionRank(decision.Action) > commandPolicyActionRank(best.Action) {
			best = decision
			matched = true
		}
	}
	if matched {
		return best, true
	}
	return DecideNamedCommandPolicy(rules, cap, command)
}

func commandPolicySubjectForShellSegment(segment string) string {
	fields, ok := splitShellFields(segment)
	if !ok {
		fields = strings.Fields(segment)
	}
	if shellFieldsUseUnsupportedWrapper(fields) {
		return strings.TrimSpace(segment)
	}
	fields = normalizeShellCommandFields(fields)
	if len(fields) == 0 {
		return strings.TrimSpace(segment)
	}
	return strings.Join(fields, " ")
}

func commandPolicyActionRank(action CommandPolicyAction) int {
	switch action {
	case CommandPolicyDeny, CommandPolicyExplain:
		return 3
	case CommandPolicyAsk:
		return 2
	case CommandPolicyAllow:
		return 1
	default:
		return 0
	}
}

// matchCommandPolicyPattern implements a small glob matcher for the
// bash-first policy ruleset. The matcher understands two wildcard
// forms:
//
//   - "X *" — trailing wildcard. Matches any value that starts with
//     the literal "X " prefix (or with "X" if the value is bare
//     "X" with no trailing args).
//   - "*X" — leading wildcard. Matches any value that ends with the
//     literal "X" suffix. Useful for file-extension rules
//     ("*.env", "*.pem", "*.key").
//
// Combined, the matcher can speak in command-language ("git status *")
// and path-language ("*.env") without bringing in filepath.Match.
//
// The bare-command case ("git status" with no args) is intentionally
// covered by the "X *"-suffixed pattern: trimming the trailing space
// from the prefix lets "git status *" match the bare form. This
// keeps the rule count small and avoids duplicate "X" + "X *"
// entries that share the same intent.
func matchCommandPolicyPattern(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	value = strings.TrimSpace(value)
	if pattern == "" {
		return value == ""
	}
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		rawPrefix := strings.TrimSuffix(pattern, "*")
		prefix := strings.TrimRight(rawPrefix, " ")
		if prefix == "" {
			return true
		}
		if strings.HasSuffix(rawPrefix, " ") {
			return value == prefix || strings.HasPrefix(value, prefix+" ")
		}
		return strings.HasPrefix(value, prefix)
	}
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimLeft(strings.TrimPrefix(pattern, "*"), " ")
		if suffix == "" {
			return true
		}
		return strings.HasSuffix(value, suffix)
	}
	return value == pattern
}

// CommandPolicyDecision is the result of looking up a rule. The
// Runtime tool policy and the capability + pattern policy both
// produce a value of this shape, so downstream approval / telemetry
// code can stay uniform.
type CommandPolicyDecision struct {
	Action CommandPolicyAction
	Reason string
	Rule   string
}
