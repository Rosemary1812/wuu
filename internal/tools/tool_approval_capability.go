package tools

import (
	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/providers"
)

type toolApprovalCapabilityFields struct {
	Capability capability.Capability
	Object     string
	Action     string
	Rule       string
}

func (t *Toolkit) approvalCapabilityFields(callName string, args string, info ToolInfo, decision ToolPolicyDecision) toolApprovalCapabilityFields {
	fields := toolApprovalCapabilityFields{
		Capability: decision.Capability,
		Object:     decision.CapabilityObject,
		Action:     decision.CapabilityAction,
		Rule:       decision.CapabilityRule,
	}
	call := providersToolCall(callName, args)
	if fields.Capability == "" {
		fields.Capability = defaultCommandPolicyCapabilityForCall(t.activeCompiledSurface(), call, info)
	}
	if fields.Object == "" && fields.Capability != "" {
		fields.Object = commandPolicySubjectFromArgs(call, fields.Capability)
	}
	if fields.Action == "" && fields.Capability != "" {
		fields.Action = capabilityActionVerb(fields.Capability)
	}
	fields.Object = redactToolOutput(fields.Object)
	fields.Rule = redactToolOutput(fields.Rule)
	return fields
}

func providersToolCall(name, arguments string) providers.ToolCall {
	return providers.ToolCall{Name: name, Arguments: arguments}
}
