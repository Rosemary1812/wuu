package runtime

import (
	"testing"

	"github.com/blueberrycongee/wuu/internal/coordinator"
	"github.com/blueberrycongee/wuu/internal/tools"
)

func TestApplyWorkerToolFilter_HidesOrchestrationTools(t *testing.T) {
	kit, err := tools.New(t.TempDir())
	if err != nil {
		t.Fatalf("New toolkit: %v", err)
	}
	wt, err := coordinator.LookupWorkerType("worker")
	if err != nil {
		t.Fatalf("worker type: %v", err)
	}

	applyWorkerToolFilter(kit, wt)

	defs := map[string]bool{}
	for _, def := range kit.Definitions() {
		defs[def.Name] = true
	}
	for _, blocked := range []string{
		"ask_user",
		"spawn_agent",
		"fork_agent",
		"send_message",
		"followup_task",
		"wait_agent",
		"close_agent",
		"list_agents",
	} {
		if defs[blocked] {
			t.Fatalf("worker toolkit should hide %s", blocked)
		}
	}
	for _, allowed := range []string{"read_file", "write_file", "run_shell", "update_plan"} {
		if !defs[allowed] {
			t.Fatalf("worker toolkit should keep %s", allowed)
		}
	}
}
