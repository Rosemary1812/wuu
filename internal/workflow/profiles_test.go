package workflow

import (
	"os"
	"testing"
)

func TestResolveProfilesPausesMissingRequiredProfiles(t *testing.T) {
	resolutions, err := ResolveProfiles(ProfileResolutionOptions{
		WuuHome: t.TempDir(),
		Definition: Definition{
			Name: "feature",
			Profiles: []ProfileRef{
				{Name: "frontend_owner", Required: true},
				{Name: "qa_reviewer"},
			},
			AllowProfileCreation: "ask",
		},
		CreateMissing: false,
	})
	if err != nil {
		t.Fatalf("ResolveProfiles: %v", err)
	}
	if len(resolutions) != 2 {
		t.Fatalf("resolutions = %+v", resolutions)
	}
	if resolutions[0].Action != "pause_missing_required" || resolutions[0].Exists {
		t.Fatalf("required missing profile should pause: %+v", resolutions[0])
	}
	if resolutions[1].Action != "spawn_ephemeral" || resolutions[1].Exists {
		t.Fatalf("optional missing profile should use ephemeral worker: %+v", resolutions[1])
	}
	missing := MissingRequiredProfiles(resolutions)
	if len(missing) != 1 || missing[0].Name != "frontend_owner" {
		t.Fatalf("missing required profiles = %+v", missing)
	}
}

func TestResolveProfilesCanAutoCreateRequiredProfiles(t *testing.T) {
	wuuHome := t.TempDir()
	resolutions, err := ResolveProfiles(ProfileResolutionOptions{
		WuuHome: wuuHome,
		Definition: Definition{
			Name:                 "feature",
			Profiles:             []ProfileRef{{Name: "release_manager", Required: true}},
			AllowProfileCreation: "auto",
		},
		CreateMissing: AutoCreateProfiles("auto"),
	})
	if err != nil {
		t.Fatalf("ResolveProfiles: %v", err)
	}
	if len(resolutions) != 1 || !resolutions[0].Exists || !resolutions[0].Created || resolutions[0].Action != "created_profile" {
		t.Fatalf("required profile should be created: %+v", resolutions)
	}
	if _, err := os.Stat(resolutions[0].ProfileDir); err != nil {
		t.Fatalf("expected profile dir: %v", err)
	}

	resolvedAgain, err := ResolveProfiles(ProfileResolutionOptions{
		WuuHome:    wuuHome,
		Definition: Definition{Profiles: []ProfileRef{{Name: "release_manager", Required: true}}},
	})
	if err != nil {
		t.Fatalf("ResolveProfiles second: %v", err)
	}
	if len(resolvedAgain) != 1 || resolvedAgain[0].Action != "use_existing" || resolvedAgain[0].Created {
		t.Fatalf("existing profile should be reused: %+v", resolvedAgain)
	}
}
