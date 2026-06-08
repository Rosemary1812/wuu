package tools

import "testing"

func TestPolicyForProfile(t *testing.T) {
	tests := []struct {
		profile       ToolPolicyProfile
		wantDefault   ToolPolicyAction
		wantLowRisk   ToolPolicyAction
		wantHighRisk  ToolPolicyAction
		wantSupported bool
	}{
		{profile: "", wantSupported: true},
		{profile: ToolPolicyProfileSafe, wantDefault: ToolPolicyDeny, wantLowRisk: ToolPolicyAllow, wantHighRisk: ToolPolicyRequireApproval, wantSupported: true},
		{profile: ToolPolicyProfileBalanced, wantDefault: ToolPolicyAllow, wantLowRisk: ToolPolicyAllow, wantHighRisk: ToolPolicyRequireApproval, wantSupported: true},
		{profile: ToolPolicyProfileAutonomous, wantDefault: ToolPolicyAllow, wantSupported: true},
		{profile: ToolPolicyProfileEnterpriseRestricted, wantDefault: ToolPolicyDeny, wantLowRisk: ToolPolicyAllow, wantHighRisk: ToolPolicyDeny, wantSupported: true},
		{profile: "unknown", wantSupported: false},
	}
	for _, tt := range tests {
		policy, ok := PolicyForProfile(tt.profile)
		if ok != tt.wantSupported {
			t.Fatalf("PolicyForProfile(%q) supported = %t, want %t", tt.profile, ok, tt.wantSupported)
		}
		if !ok {
			continue
		}
		if policy.DefaultAction != tt.wantDefault {
			t.Fatalf("PolicyForProfile(%q).DefaultAction = %s, want %s", tt.profile, policy.DefaultAction, tt.wantDefault)
		}
		if tt.wantLowRisk != "" && policy.RiskActions[ToolRiskLow] != tt.wantLowRisk {
			t.Fatalf("PolicyForProfile(%q) low risk = %s, want %s", tt.profile, policy.RiskActions[ToolRiskLow], tt.wantLowRisk)
		}
		if tt.wantHighRisk != "" && policy.RiskActions[ToolRiskHigh] != tt.wantHighRisk {
			t.Fatalf("PolicyForProfile(%q) high risk = %s, want %s", tt.profile, policy.RiskActions[ToolRiskHigh], tt.wantHighRisk)
		}
	}
}
