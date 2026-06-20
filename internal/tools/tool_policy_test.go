package tools

import "testing"

func TestPolicyForProfile(t *testing.T) {
	tests := []struct {
		profile        ToolPolicyProfile
		wantApproval   ToolApprovalPolicy
		wantDefault    ToolPolicyAction
		wantLowRisk    ToolPolicyAction
		wantMediumRisk ToolPolicyAction
		wantHighRisk   ToolPolicyAction
		wantSupported  bool
	}{
		{profile: "", wantSupported: true},
		{profile: ToolPolicyProfileReadOnly, wantApproval: ToolApprovalPolicyOnRequest, wantDefault: ToolPolicyAllow, wantSupported: true},
		{profile: ToolPolicyProfileAgent, wantApproval: ToolApprovalPolicyOnRequest, wantDefault: ToolPolicyAllow, wantSupported: true},
		{profile: ToolPolicyProfileAutoReview, wantApproval: ToolApprovalPolicyOnRequest, wantDefault: ToolPolicyAllow, wantSupported: true},
		{profile: ToolPolicyProfileFullAccess, wantApproval: ToolApprovalPolicyNever, wantDefault: ToolPolicyAllow, wantSupported: true},
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
		if tt.profile != "" && tt.wantSupported && policy.Profile != tt.profile {
			t.Fatalf("PolicyForProfile(%q).Profile = %s, want %s", tt.profile, policy.Profile, tt.profile)
		}
		if policy.DefaultAction != tt.wantDefault {
			t.Fatalf("PolicyForProfile(%q).DefaultAction = %s, want %s", tt.profile, policy.DefaultAction, tt.wantDefault)
		}
		if policy.ApprovalPolicy != tt.wantApproval {
			t.Fatalf("PolicyForProfile(%q).ApprovalPolicy = %s, want %s", tt.profile, policy.ApprovalPolicy, tt.wantApproval)
		}
		if tt.wantLowRisk != "" && policy.RiskActions[ToolRiskLow] != tt.wantLowRisk {
			t.Fatalf("PolicyForProfile(%q) low risk = %s, want %s", tt.profile, policy.RiskActions[ToolRiskLow], tt.wantLowRisk)
		}
		if tt.wantMediumRisk != "" && policy.RiskActions[ToolRiskMedium] != tt.wantMediumRisk {
			t.Fatalf("PolicyForProfile(%q) medium risk = %s, want %s", tt.profile, policy.RiskActions[ToolRiskMedium], tt.wantMediumRisk)
		}
		if tt.wantHighRisk != "" && policy.RiskActions[ToolRiskHigh] != tt.wantHighRisk {
			t.Fatalf("PolicyForProfile(%q) high risk = %s, want %s", tt.profile, policy.RiskActions[ToolRiskHigh], tt.wantHighRisk)
		}
	}
}
