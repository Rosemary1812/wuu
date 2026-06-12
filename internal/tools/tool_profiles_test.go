package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestAgentProfileToolsCreateAndListProfiles(t *testing.T) {
	root := t.TempDir()
	wuuHome := filepath.Join(t.TempDir(), "wuu-home")
	t.Setenv("WUU_HOME", wuuHome)

	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	createResp, err := kit.Execute(context.Background(), providers.ToolCall{
		Name: "create_agent_profile",
		Arguments: `{
			"name":"qa_laowang",
			"role":"QA reviewer",
			"description":"Remembers recurring QA checks.",
			"workflow_name":"release-qa"
		}`,
	})
	if err != nil {
		t.Fatalf("create_agent_profile: %v", err)
	}
	var created struct {
		Action  string `json:"action"`
		Created bool   `json:"created"`
		Profile struct {
			Name         string `json:"name"`
			Role         string `json:"role"`
			Description  string `json:"description"`
			WorkflowName string `json:"workflow_name"`
		} `json:"profile"`
	}
	if err := json.Unmarshal([]byte(createResp), &created); err != nil {
		t.Fatalf("parse create response: %v", err)
	}
	if created.Action != "create_agent_profile" {
		t.Fatalf("create action = %q, want create_agent_profile", created.Action)
	}
	if !created.Created || created.Profile.Name != "qa_laowang" || created.Profile.Role != "QA reviewer" || created.Profile.WorkflowName != "release-qa" {
		t.Fatalf("unexpected create response: %+v", created)
	}

	listResp, err := kit.Execute(context.Background(), providers.ToolCall{Name: "list_agent_profiles", Arguments: `{}`})
	if err != nil {
		t.Fatalf("list_agent_profiles: %v", err)
	}
	var listed struct {
		Action   string `json:"action"`
		Count    int    `json:"count"`
		Profiles []struct {
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(listResp), &listed); err != nil {
		t.Fatalf("parse list response: %v", err)
	}
	if listed.Action != "list_agent_profiles" {
		t.Fatalf("list action = %q, want list_agent_profiles", listed.Action)
	}
	if listed.Count != 1 || listed.Profiles[0].Name != "qa_laowang" || listed.Profiles[0].Role != "QA reviewer" {
		t.Fatalf("unexpected profile list: %+v", listed)
	}
	records := kit.ToolTelemetry()
	if len(records) != 2 || records[0].ResultAction != "create_agent_profile" || records[1].ResultAction != "list_agent_profiles" {
		t.Fatalf("profile telemetry actions mismatch: %+v", records)
	}
}

func TestAgentProfileToolDescriptionsAreGeneralDelegationProfiles(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	descriptions := map[string]string{}
	for _, def := range kit.Definitions() {
		switch def.Name {
		case "list_agent_profiles", "create_agent_profile":
			descriptions[def.Name] = def.Description
		}
	}
	for _, name := range []string{"list_agent_profiles", "create_agent_profile"} {
		desc := descriptions[name]
		if desc == "" {
			t.Fatalf("%s definition missing", name)
		}
		for _, want := range []string{"subagent", "recurring", "saved memory"} {
			if !strings.Contains(desc, want) {
				t.Fatalf("%s description missing %q: %q", name, want, desc)
			}
		}
		for _, bad := range []string{"memory-bearing", "memoryless"} {
			if strings.Contains(desc, bad) {
				t.Fatalf("%s description should avoid awkward memory wording %q: %q", name, bad, desc)
			}
		}
		if strings.Contains(desc, "recurring workflow roles") || strings.Contains(desc, "dynamic workflow team") {
			t.Fatalf("%s description should not make profiles workflow-only: %q", name, desc)
		}
	}
}
