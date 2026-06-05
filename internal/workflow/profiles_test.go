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

func TestEnsureAndListProfiles(t *testing.T) {
	wuuHome := t.TempDir()
	profile, created, err := EnsureProfile(ProfileEnsureOptions{
		WuuHome:      wuuHome,
		Name:         "qa_laowang",
		Source:       "agent",
		WorkflowName: "release-qa",
		Role:         "QA reviewer",
		Description:  "Remembers recurring release QA checks.",
	})
	if err != nil {
		t.Fatalf("EnsureProfile: %v", err)
	}
	if !created {
		t.Fatal("expected profile to be created")
	}
	if profile.Name != "qa_laowang" || profile.Role != "QA reviewer" || profile.WorkflowName != "release-qa" {
		t.Fatalf("unexpected profile summary: %+v", profile)
	}

	profile, created, err = EnsureProfile(ProfileEnsureOptions{
		WuuHome:     wuuHome,
		Name:        "qa_laowang",
		Description: "Updated description",
	})
	if err != nil {
		t.Fatalf("EnsureProfile second: %v", err)
	}
	if created {
		t.Fatal("existing profile should not be marked created")
	}
	if profile.Description != "Updated description" || profile.Role != "QA reviewer" {
		t.Fatalf("existing profile metadata should update without clearing role: %+v", profile)
	}

	profiles, err := ListProfiles(wuuHome)
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].Name != "qa_laowang" {
		t.Fatalf("profiles = %+v", profiles)
	}
}

func TestEnsureProfileRejectsDefaultProfile(t *testing.T) {
	if _, _, err := EnsureProfile(ProfileEnsureOptions{WuuHome: t.TempDir(), Name: "default"}); err == nil {
		t.Fatal("expected default profile to be rejected")
	}
}
