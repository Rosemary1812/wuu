package tools

import "testing"

func TestPolicyForProfile(t *testing.T) {
	tests := []struct {
		profile        ToolPolicyProfile
		wantDefault    ToolPolicyAction
		wantLowRisk    ToolPolicyAction
		wantMediumRisk ToolPolicyAction
		wantHighRisk   ToolPolicyAction
		wantSupported  bool
	}{
		{profile: "", wantSupported: true},
		{profile: ToolPolicyProfileReadOnly, wantDefault: ToolPolicyAllow, wantSupported: true},
		{profile: ToolPolicyProfileAgent, wantDefault: ToolPolicyAllow, wantSupported: true},
		{profile: ToolPolicyProfileAutoReview, wantDefault: ToolPolicyAllow, wantSupported: true},
		{profile: ToolPolicyProfileFullAccess, wantDefault: ToolPolicyAllow, wantSupported: true},
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
