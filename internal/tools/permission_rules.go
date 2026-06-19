package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
)

type ToolPermissionAction string

const (
	ToolPermissionAllow ToolPermissionAction = "allow"
	ToolPermissionDeny  ToolPermissionAction = "deny"
	ToolPermissionAsk   ToolPermissionAction = "ask"
)

type ToolPermissionRule struct {
	Permission string               `json:"permission"`
	Pattern    string               `json:"pattern"`
	Action     ToolPermissionAction `json:"action"`
	Source     string               `json:"source,omitempty"`
}

type ToolPermissionRuleSet []ToolPermissionRule

type ToolPermissionRequest struct {
	Permission string            `json:"permission"`
	Patterns   []string          `json:"patterns"`
	Always     []string          `json:"always,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type ToolPermissionDecision struct {
	Action ToolPermissionAction `json:"action"`
	Rule   ToolPermissionRule   `json:"rule,omitempty"`
	Reason string               `json:"reason,omitempty"`
}

type InputValidatingTool interface {
	ValidateInput(argsJSON string) error
}

type PermissionRequestingTool interface {
	PermissionRequests(argsJSON string) []ToolPermissionRequest
}

func (r ToolPermissionRuleSet) Decide(req ToolPermissionRequest) (ToolPermissionDecision, bool) {
	req = normalizeToolPermissionRequest(req)
	if req.Permission == "" {
		return ToolPermissionDecision{}, false
	}
	for i := len(r) - 1; i >= 0; i-- {
		rule := normalizeToolPermissionRule(r[i])
		if rule.Permission == "" || rule.Pattern == "" || rule.Action == "" {
			continue
		}
		if !permissionNameMatches(rule.Permission, req.Permission) {
			continue
		}
		for _, pattern := range req.Patterns {
			if permissionPatternMatches(rule.Pattern, pattern) {
				return ToolPermissionDecision{
					Action: rule.Action,
					Rule:   rule,
					Reason: fmt.Sprintf("permission rule %s %s", rule.Permission, rule.Pattern),
				}, true
			}
		}
	}
	return ToolPermissionDecision{}, false
}

func normalizeToolPermissionRule(rule ToolPermissionRule) ToolPermissionRule {
	rule.Permission = strings.TrimSpace(rule.Permission)
	rule.Pattern = strings.TrimSpace(rule.Pattern)
	rule.Source = strings.TrimSpace(rule.Source)
	switch ToolPermissionAction(strings.TrimSpace(string(rule.Action))) {
	case ToolPermissionAllow, ToolPermissionDeny, ToolPermissionAsk:
		rule.Action = ToolPermissionAction(strings.TrimSpace(string(rule.Action)))
	default:
		rule.Action = ""
	}
	return rule
}

func normalizeToolPermissionRequest(req ToolPermissionRequest) ToolPermissionRequest {
	req.Permission = strings.TrimSpace(req.Permission)
	req.Patterns = normalizePermissionPatterns(req.Patterns)
	req.Always = normalizePermissionPatterns(req.Always)
	if len(req.Patterns) == 0 {
		req.Patterns = []string{"*"}
	}
	if len(req.Always) == 0 {
		req.Always = append([]string(nil), req.Patterns...)
	}
	return req
}

func normalizePermissionPatterns(patterns []string) []string {
	out := make([]string, 0, len(patterns))
	seen := make(map[string]struct{}, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if _, ok := seen[pattern]; ok {
			continue
		}
		seen[pattern] = struct{}{}
		out = append(out, pattern)
	}
	return out
}

func permissionNameMatches(rulePermission, requestedPermission string) bool {
	rulePermission = strings.TrimSpace(rulePermission)
	requestedPermission = strings.TrimSpace(requestedPermission)
	if rulePermission == "" || requestedPermission == "" {
		return false
	}
	if rulePermission == "*" || rulePermission == requestedPermission {
		return true
	}
	return permissionGlobMatches(rulePermission, requestedPermission)
}

func permissionPatternMatches(rulePattern, requestedPattern string) bool {
	rulePattern = strings.TrimSpace(rulePattern)
	requestedPattern = strings.TrimSpace(requestedPattern)
	if rulePattern == "" || requestedPattern == "" {
		return false
	}
	if rulePattern == "*" || rulePattern == requestedPattern {
		return true
	}
	return permissionGlobMatches(rulePattern, requestedPattern)
}

func permissionGlobMatches(pattern, value string) bool {
	re, err := regexp.Compile(globToRegexp(pattern))
	if err != nil {
		return false
	}
	return re.MatchString(value)
}

func (t *Toolkit) SetPermissionRules(rules ToolPermissionRuleSet) {
	if t == nil {
		return
	}
	t.permissionRulesMu.Lock()
	defer t.permissionRulesMu.Unlock()
	t.permissionRules = cloneToolPermissionRules(rules)
}

func (t *Toolkit) PermissionRules() ToolPermissionRuleSet {
	if t == nil {
		return nil
	}
	t.permissionRulesMu.RLock()
	defer t.permissionRulesMu.RUnlock()
	base := cloneToolPermissionRules(t.permissionRules)
	base = append(base, t.permissionSessionRules...)
	return base
}

func (t *Toolkit) addSessionPermissionAllow(req ToolPermissionRequest, review ToolApprovalReview) {
	if t == nil {
		return
	}
	req = normalizeToolPermissionRequest(req)
	if req.Permission == "" {
		return
	}
	source := ApprovalSourceUser
	if review.Source != "" {
		source = review.Source
	}
	t.permissionRulesMu.Lock()
	defer t.permissionRulesMu.Unlock()
	for _, pattern := range req.Always {
		t.permissionSessionRules = append(t.permissionSessionRules, ToolPermissionRule{
			Permission: req.Permission,
			Pattern:    pattern,
			Action:     ToolPermissionAllow,
			Source:     source + ":session",
		})
	}
}

func cloneToolPermissionRules(rules ToolPermissionRuleSet) ToolPermissionRuleSet {
	if len(rules) == 0 {
		return nil
	}
	out := make(ToolPermissionRuleSet, len(rules))
	copy(out, rules)
	return out
}

func (t *Toolkit) permissionRuleSetSnapshot() ToolPermissionRuleSet {
	if t == nil {
		return nil
	}
	t.permissionRulesMu.RLock()
	defer t.permissionRulesMu.RUnlock()
	rules := cloneToolPermissionRules(t.permissionRules)
	rules = append(rules, t.permissionSessionRules...)
	return rules
}

func toolPermissionRequests(tool Tool, call providers.ToolCall, info ToolInfo) []ToolPermissionRequest {
	if requester, ok := tool.(PermissionRequestingTool); ok {
		requests := requester.PermissionRequests(call.Arguments)
		if len(requests) > 0 {
			return normalizeToolPermissionRequests(requests)
		}
	}
	return normalizeToolPermissionRequests(defaultToolPermissionRequests(call, info))
}

func (t *Toolkit) applyPermissionRuleDecision(
	ctx context.Context,
	call providers.ToolCall,
	tool Tool,
	info ToolInfo,
	base ToolPolicyDecision,
	startedAt time.Time,
	revision string,
) (ToolPolicyDecision, bool, string, ToolApprovalReview, error) {
	rules := t.permissionRuleSetSnapshot()
	if len(rules) == 0 {
		return base, false, "", ToolApprovalReview{}, nil
	}
	requests := toolPermissionRequests(tool, call, info)
	matchedAllow := false
	allowDecision := base
	for _, req := range requests {
		decision, ok := rules.Decide(req)
		if !ok {
			continue
		}
		switch decision.Action {
		case ToolPermissionDeny:
			policyDecision := permissionToolPolicyDecision(decision)
			return policyDecision, true, "", ToolApprovalReview{}, toolPermissionDeniedError{
				ToolName:   call.Name,
				Permission: req.Permission,
				Pattern:    strings.Join(req.Patterns, ","),
				Reason:     decision.Reason,
			}
		case ToolPermissionAsk:
			policyDecision := permissionToolPolicyDecision(decision)
			approvalRef := t.persistApprovalRequest(call, info, policyDecision, startedAt, revision)
			reqCopy := req
			decisionCopy := decision
			review, err := t.requestToolApproval(ctx, call, info, policyDecision, startedAt, revision, approvalRef, &reqCopy, &decisionCopy)
			if err != nil {
				if errors.Is(err, errToolApprovalReviewerUnavailable) {
					err = policyDecision.blockingError(call.Name)
				}
				return policyDecision, true, approvalRef, review, err
			}
			if review.Decision == ToolApprovalDecisionApprovedForSession {
				t.addSessionPermissionAllow(req, review)
			}
			return policyDecision, true, approvalRef, review, nil
		case ToolPermissionAllow:
			matchedAllow = true
			allowDecision = permissionToolPolicyDecision(decision)
		}
	}
	if matchedAllow {
		return allowDecision, true, "", ToolApprovalReview{}, nil
	}
	return base, false, "", ToolApprovalReview{}, nil
}

func permissionToolPolicyDecision(decision ToolPermissionDecision) ToolPolicyDecision {
	switch decision.Action {
	case ToolPermissionAllow:
		return ToolPolicyDecision{Action: ToolPolicyAllow, Reason: decision.Reason}
	case ToolPermissionDeny:
		return ToolPolicyDecision{Action: ToolPolicyDeny, Reason: decision.Reason}
	case ToolPermissionAsk:
		return ToolPolicyDecision{Action: ToolPolicyRequireApproval, Reason: decision.Reason}
	default:
		return ToolPolicyDecision{}
	}
}

type toolPermissionDeniedError struct {
	ToolName   string
	Permission string
	Pattern    string
	Reason     string
}

func (e toolPermissionDeniedError) Error() string {
	reason := strings.TrimSpace(e.Reason)
	if reason == "" {
		reason = "permission rule denied tool call"
	}
	return fmt.Sprintf(
		"tool %q blocked by permission rule: error_kind=permission_rule_denied permission=%q pattern=%q policy_reason=%q model_next_action=%q",
		e.ToolName,
		e.Permission,
		e.Pattern,
		reason,
		"choose a lower-risk alternative, change the command/path, or ask the user to change permission rules",
	)
}

func normalizeToolPermissionRequests(requests []ToolPermissionRequest) []ToolPermissionRequest {
	out := make([]ToolPermissionRequest, 0, len(requests))
	for _, req := range requests {
		req = normalizeToolPermissionRequest(req)
		if req.Permission == "" {
			continue
		}
		out = append(out, req)
	}
	return out
}

func defaultToolPermissionRequests(call providers.ToolCall, info ToolInfo) []ToolPermissionRequest {
	permission := defaultPermissionName(call.Name, info)
	patterns := defaultPermissionPatterns(call)
	return []ToolPermissionRequest{{
		Permission: permission,
		Patterns:   patterns,
		Always:     patterns,
		Metadata: map[string]string{
			"tool": call.Name,
			"kind": string(info.Kind),
		},
	}}
}

func defaultPermissionName(toolName string, info ToolInfo) string {
	switch strings.TrimSpace(toolName) {
	case "run_shell", "start_process":
		return "bash"
	}
	switch info.Kind {
	case ToolKindFile:
		if info.ReadOnly {
			return "read"
		}
		return "edit"
	case ToolKindSearch, ToolKindDiscovery:
		return "read"
	case ToolKindMCP:
		return toolName
	default:
		if toolName != "" {
			return toolName
		}
		return string(info.Kind)
	}
}

func defaultPermissionPatterns(call providers.ToolCall) []string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(call.Arguments)), &payload); err != nil {
		return []string{"*"}
	}
	for _, key := range []string{"path", "file", "target", "cwd", "command"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return []string{strings.TrimSpace(value)}
		}
	}
	return []string{"*"}
}

func shellPermissionRequest(toolName, command string) ToolPermissionRequest {
	command = strings.TrimSpace(command)
	patterns := []string{command}
	if command == "" {
		patterns = []string{"*"}
	}
	return ToolPermissionRequest{
		Permission: "bash",
		Patterns:   patterns,
		Always:     []string{shellPermissionAlwaysPattern(command)},
		Metadata: map[string]string{
			"tool":    toolName,
			"command": command,
		},
	}
}

func shellPermissionAlwaysPattern(command string) string {
	fields := shellPermissionFields(command)
	if len(fields) == 0 {
		return "*"
	}
	prefixLen := 1
	if len(fields) >= 2 && shellPermissionKeepsSecondToken(fields[0], fields[1]) {
		prefixLen = 2
	}
	return strings.Join(fields[:prefixLen], " ") + " *"
}

func shellPermissionFields(command string) []string {
	fields := strings.Fields(strings.TrimSpace(command))
	for len(fields) > 0 && looksLikeEnvAssignment(fields[0]) {
		fields = fields[1:]
	}
	return fields
}

func shellPermissionKeepsSecondToken(first, second string) bool {
	switch first {
	case "cargo", "go", "git", "make", "npm", "pnpm", "yarn", "bun", "deno", "python", "python3", "pip", "pip3", "uv":
		return second != ""
	default:
		return false
	}
}
