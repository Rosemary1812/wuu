package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	PermissionRuleActionAllow = "allow"
	PermissionRuleActionDeny  = "deny"
	PermissionRuleActionAsk   = "ask"
)

// PermissionRulesConfig mirrors OpenCode's permission object shape:
// permission name -> pattern -> action. A permission value may also be a string,
// which is treated as the action for "*".
type PermissionRulesConfig map[string]PermissionRulePatternsConfig

type PermissionRulePatternsConfig map[string]string

func (c *PermissionRulePatternsConfig) UnmarshalJSON(data []byte) error {
	var shorthand string
	if err := json.Unmarshal(data, &shorthand); err == nil {
		*c = PermissionRulePatternsConfig{"*": strings.TrimSpace(shorthand)}
		return nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := make(PermissionRulePatternsConfig, len(raw))
	for pattern, value := range raw {
		var action string
		if err := json.Unmarshal(value, &action); err == nil {
			out[pattern] = strings.TrimSpace(action)
			continue
		}
		var object struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(value, &object); err != nil {
			return fmt.Errorf("permission rule %q must be an action string or object with action: %w", pattern, err)
		}
		out[pattern] = strings.TrimSpace(object.Action)
	}
	*c = out
	return nil
}

func validatePermissionRulesConfig(rules PermissionRulesConfig) error {
	for permission, patterns := range rules {
		if strings.TrimSpace(permission) == "" {
			return fmt.Errorf("agent.permission_rules contains an empty permission")
		}
		if len(patterns) == 0 {
			return fmt.Errorf("agent.permission_rules.%s must contain at least one pattern", permission)
		}
		for pattern, action := range patterns {
			if strings.TrimSpace(pattern) == "" {
				return fmt.Errorf("agent.permission_rules.%s contains an empty pattern", permission)
			}
			switch strings.TrimSpace(action) {
			case PermissionRuleActionAllow, PermissionRuleActionDeny, PermissionRuleActionAsk:
			default:
				return fmt.Errorf("agent.permission_rules.%s.%s must be one of allow, deny, ask", permission, pattern)
			}
		}
	}
	return nil
}
