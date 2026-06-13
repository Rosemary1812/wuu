package agentcontrol

import "testing"

func TestLookupWorkerType_GeneralPurpose(t *testing.T) {
	wt, err := LookupWorkerType(DefaultSubagentType)
	if err != nil {
		t.Fatalf("LookupWorkerType(general-purpose) failed: %v", err)
	}
	if wt.Name != DefaultSubagentType {
		t.Errorf("got name %q, want %s", wt.Name, DefaultSubagentType)
	}
	if wt.SystemPrompt == "" {
		t.Error("general-purpose agent has empty SystemPrompt")
	}
}

func TestLookupWorkerType_DefaultsToGeneralPurpose(t *testing.T) {
	wt, err := LookupWorkerType("")
	if err != nil {
		t.Fatal(err)
	}
	if wt.Name != DefaultSubagentType {
		t.Fatalf("expected default = %s, got %q", DefaultSubagentType, wt.Name)
	}
}

func TestLookupWorkerType_RemovedTypesRejected(t *testing.T) {
	for _, name := range []string{"research", "explorer", "verifier"} {
		if _, err := LookupWorkerType(name); err == nil {
			t.Errorf("LookupWorkerType(%q) should error after aligning to subagent_type definitions", name)
		}
	}
}

func TestLookupWorkerType_LoopRoles(t *testing.T) {
	for _, name := range []string{"planner", "researcher", "worker", "reviewer", "qa", "debugger", "integrator"} {
		wt, err := LookupWorkerType(name)
		if err != nil {
			t.Fatalf("LookupWorkerType(%q): %v", name, err)
		}
		if wt.Role == "" || wt.ContextScope == "" || wt.OutputSchema == "" || len(wt.SuccessCriteria) == 0 {
			t.Fatalf("%s missing role contract: %+v", name, wt)
		}
	}
}

func TestLookupWorkerType_Unknown(t *testing.T) {
	_, err := LookupWorkerType("nope")
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestFilterToolsForWorker_BlocksRecursiveAgentControls(t *testing.T) {
	wt, _ := LookupWorkerType(DefaultSubagentType)
	full := []string{
		"read_file", "write_file", "edit_file", "run_shell", "run_test",
		"grep", "glob", "spawn_agent", "send_message", "followup_task",
		"wait_agent", "await_agents", "close_agent", "list_agents", "agent_report",
	}
	filtered := FilterToolsForWorker(wt, full)
	allowed := map[string]bool{}
	for _, n := range filtered {
		allowed[n] = true
	}
	for _, expected := range []string{"read_file", "write_file", "edit_file", "run_shell", "run_test", "grep", "glob", "agent_report"} {
		if !allowed[expected] {
			t.Errorf("general-purpose agent missing %s", expected)
		}
	}
	for _, blocked := range []string{"spawn_agent", "send_message", "followup_task", "wait_agent", "await_agents", "close_agent", "list_agents"} {
		if allowed[blocked] {
			t.Errorf("general-purpose agent should not receive recursive control tool %s", blocked)
		}
	}
}

func TestFilterToolsForWorker_VerificationDisallowsProjectWrites(t *testing.T) {
	wt, _ := LookupWorkerType("verification")
	if !wt.Background {
		t.Fatal("verification agent should run in the background")
	}
	full := []string{"read_file", "write_file", "edit_file", "apply_patch", "run_shell", "run_test", "agent_report"}
	filtered := FilterToolsForWorker(wt, full)
	allowed := map[string]bool{}
	for _, n := range filtered {
		allowed[n] = true
	}
	for _, blocked := range []string{"write_file", "edit_file", "apply_patch"} {
		if allowed[blocked] {
			t.Errorf("verification agent should not receive write tool %s", blocked)
		}
	}
	for _, expected := range []string{"read_file", "run_shell", "run_test", "agent_report"} {
		if !allowed[expected] {
			t.Errorf("verification agent missing %s", expected)
		}
	}
}

func TestFilterToolsForWorker_CheckerRolesDisallowProjectWrites(t *testing.T) {
	full := []string{"read_file", "write_file", "edit_file", "apply_patch", "run_shell", "run_test", "grep", "glob", "agent_report"}
	for _, name := range []string{"planner", "researcher", "reviewer", "qa", "debugger", "integrator"} {
		t.Run(name, func(t *testing.T) {
			wt, err := LookupWorkerType(name)
			if err != nil {
				t.Fatal(err)
			}
			filtered := FilterToolsForWorker(wt, full)
			allowed := map[string]bool{}
			for _, n := range filtered {
				allowed[n] = true
			}
			for _, blocked := range []string{"write_file", "edit_file", "apply_patch"} {
				if allowed[blocked] {
					t.Errorf("%s should not receive write tool %s", name, blocked)
				}
			}
			if !allowed["read_file"] || !allowed["agent_report"] {
				t.Errorf("%s missing expected read/report tools: %v", name, filtered)
			}
		})
	}
}

func TestWorkerType_DefaultIsolation(t *testing.T) {
	wt, err := LookupWorkerType(DefaultSubagentType)
	if err != nil {
		t.Fatal(err)
	}
	if wt.DefaultIsolation != IsolationInplace {
		t.Errorf("general-purpose: want default isolation %q, got %q",
			IsolationInplace, wt.DefaultIsolation)
	}
	worker, err := LookupWorkerType("worker")
	if err != nil {
		t.Fatal(err)
	}
	if worker.DefaultIsolation != IsolationWorktree {
		t.Errorf("worker: want default isolation %q, got %q",
			IsolationWorktree, worker.DefaultIsolation)
	}
}

func TestNormalizeIsolation(t *testing.T) {
	agent, _ := LookupWorkerType(DefaultSubagentType)

	cases := []struct {
		name    string
		raw     string
		wt      WorkerType
		want    IsolationMode
		wantErr bool
	}{
		{"empty falls back to type default", "", agent, IsolationInplace, false},
		{"explicit inplace", "inplace", agent, IsolationInplace, false},
		{"explicit worktree", "worktree", agent, IsolationWorktree, false},
		{"case insensitive", "InPlace", agent, IsolationInplace, false},
		{"empty type with empty default falls back to inplace", "", WorkerType{}, IsolationInplace, false},
		{"unknown rejected", "yolo", agent, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeIsolation(tc.raw, tc.wt)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}
