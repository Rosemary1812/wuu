package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func (t *Toolkit) applyDefaultCommandPolicyDecision(call providers.ToolCall, info ToolInfo, base ToolPolicyDecision) ToolPolicyDecision {
	surface := t.activeCompiledSurface()
	if surface.ProfileName == "" {
		return base
	}
	capabilityName, subject, ok := defaultCommandPolicySubject(surface, call, info)
	if !ok {
		return base
	}
	policyDecision, ok := decideDefaultCommandPolicy(capabilityName, subject)
	if !ok {
		return base
	}
	return t.commandPolicyToolPolicyDecision(base, capabilityName, subject, policyDecision)
}

func defaultCommandPolicySubject(surface capability.Surface, call providers.ToolCall, info ToolInfo) (capability.Capability, string, bool) {
	capabilityName := defaultCommandPolicyCapabilityForCall(surface, call, info)
	if capabilityName == "" {
		return "", "", false
	}
	subject := commandPolicySubjectFromArgs(call, capabilityName)
	if subject == "" {
		return "", "", false
	}
	return capabilityName, subject, true
}

func defaultCommandPolicyCapabilityForCall(surface capability.Surface, call providers.ToolCall, info ToolInfo) capability.Capability {
	if strings.TrimSpace(call.Name) == "bash" {
		var args bashArgs
		if err := json.Unmarshal([]byte(strings.TrimSpace(call.Arguments)), &args); err == nil {
			switch normalizeBashAction(args) {
			case bashActionStartBackground, bashActionListBackground, bashActionReadBackground, bashActionWriteBackground, bashActionStopBackground:
				return capability.CapabilityCommandBackground
			default:
				return capability.CapabilityCommandBash
			}
		}
	}
	return defaultCommandPolicyCapability(surface, call.Name, info)
}

func defaultCommandPolicyCapability(surface capability.Surface, toolName string, info ToolInfo) capability.Capability {
	if capName, ok := surface.Tools[toolName]; ok {
		return capName
	}
	if capName, ok := surface.HiddenTools[toolName]; ok {
		return capName
	}
	switch strings.TrimSpace(toolName) {
	case "bash", "run_shell", "run_test", "git":
		return capability.CapabilityCommandBash
	case "start_process", "list_processes", "read_process_output", "write_stdin", "stop_process":
		return capability.CapabilityCommandBackground
	case "read_file":
		return capability.CapabilityFileRead
	case "list_files":
		return capability.CapabilityFileList
	case "write_file", "edit_file", "apply_patch", "checkpoint":
		return capability.CapabilityFileEdit
	}
	switch info.Kind {
	case ToolKindFile:
		if info.ReadOnly {
			return capability.CapabilityFileRead
		}
		return capability.CapabilityFileEdit
	case ToolKindShell, ToolKindTest, ToolKindGit:
		return capability.CapabilityCommandBash
	case ToolKindProcess:
		return capability.CapabilityCommandBackground
	default:
		return ""
	}
}

func commandPolicySubjectFromArgs(call providers.ToolCall, capName capability.Capability) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(call.Arguments)), &payload); err != nil {
		return ""
	}
	switch capName {
	case capability.CapabilityCommandBash, capability.CapabilityCommandBackground:
		return stringField(payload, "command")
	case capability.CapabilityFileRead, capability.CapabilityFileEdit, capability.CapabilityFileList:
		for _, key := range []string{"path", "file", "target", "cwd"} {
			if value := stringField(payload, key); value != "" {
				return value
			}
		}
	}
	for _, key := range []string{"command", "path", "file", "target", "cwd"} {
		if value := stringField(payload, key); value != "" {
			return value
		}
	}
	return ""
}

func stringField(payload map[string]any, key string) string {
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func decideDefaultCommandPolicy(capName capability.Capability, subject string) (CommandPolicyDecision, bool) {
	rules := DefaultCommandPolicyRules()
	switch capName {
	case capability.CapabilityCommandBash, capability.CapabilityCommandBackground:
		return DecideShellCommandPolicy(rules, capName, subject)
	default:
		return DecideNamedCommandPolicy(rules, capName, subject)
	}
}

func (t *Toolkit) commandPolicyToolPolicyDecision(base ToolPolicyDecision, capabilityName capability.Capability, subject string, policyDecision CommandPolicyDecision) ToolPolicyDecision {
	originalReason := strings.TrimSpace(base.Reason)
	base.Reason = commandPolicyReason(policyDecision)
	base.Capability = capabilityName
	base.CapabilityObject = strings.TrimSpace(subject)
	base.CapabilityAction = capabilityActionVerb(capabilityName)
	base.CapabilityRule = strings.TrimSpace(policyDecision.Rule)
	switch policyDecision.Action {
	case CommandPolicyAllow:
		base.Action = ToolPolicyAllow
	case CommandPolicyAsk:
		switch base.Action {
		case ToolPolicyDeny:
			base.Reason = originalReason
			if strings.TrimSpace(base.Reason) == "" {
				base.Reason = "policy denied"
			}
		case ToolPolicyAllow:
			if t != nil && t.toolPolicy.Profile == ToolPolicyProfileFullAccess {
				base.Action = ToolPolicyAllow
			} else {
				base.Action = ToolPolicyRequireApproval
			}
		default:
			base.Action = ToolPolicyRequireApproval
		}
	case CommandPolicyDeny, CommandPolicyExplain:
		base.Action = ToolPolicyDeny
	}
	return base
}

func capabilityActionVerb(capabilityName capability.Capability) string {
	switch capabilityName {
	case capability.CapabilityCommandBash, capability.CapabilityCommandBackground:
		return "execute"
	case capability.CapabilityFileRead:
		return "read"
	case capability.CapabilityFileList:
		return "list"
	case capability.CapabilityFileEdit:
		return "edit"
	case capability.CapabilitySearchGrep, capability.CapabilitySearchGlob, capability.CapabilitySearchAST, capability.CapabilitySearchSemantic:
		return "search"
	case capability.CapabilityWebFetch:
		return "fetch"
	case capability.CapabilityWebSearch:
		return "search"
	default:
		parts := strings.Split(string(capabilityName), ".")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[len(parts)-1])
		}
		return ""
	}
}

func commandPolicyReason(decision CommandPolicyDecision) string {
	reason := strings.TrimSpace(decision.Reason)
	rule := strings.TrimSpace(decision.Rule)
	switch {
	case rule != "" && reason != "":
		return fmt.Sprintf("command policy %s: %s", rule, reason)
	case rule != "":
		return "command policy " + rule
	case reason != "":
		return "command policy: " + reason
	default:
		return "command policy"
	}
}

func shellCommandPackageOrNetworkMutationCoveredByCommandPolicy(command string) bool {
	segments, ok := splitShellCommandSegmentsQuoted(command)
	if !ok {
		segments = splitShellCommandSegments(command)
	}
	if len(segments) == 0 {
		segments = []string{command}
	}
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		fields, ok := splitShellFields(segment)
		if !ok {
			fields = strings.Fields(segment)
		}
		fields = normalizeShellCommandFields(fields)
		if !shellFieldsLookLikePackageOrNetworkMutation(fields) {
			continue
		}
		decision, ok := DecideNamedCommandPolicy(DefaultCommandPolicyRules(), capability.CapabilityCommandBash, segment)
		if !ok || decision.Action == CommandPolicyDeny || decision.Action == CommandPolicyExplain {
			return false
		}
	}
	return true
}
