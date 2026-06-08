package evalharness

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/blueberrycongee/wuu/internal/config"
)

func TestCatalogHasStableTaskIDs(t *testing.T) {
	tasks := Catalog()
	if len(tasks) < 4 {
		t.Fatalf("expected at least 4 tasks, got %d", len(tasks))
	}
	seen := map[string]bool{}
	for _, task := range tasks {
		if task.ID == "" {
			t.Fatal("task id must not be empty")
		}
		if seen[task.ID] {
			t.Fatalf("duplicate task id %q", task.ID)
		}
		seen[task.ID] = true
		if task.Prompt == "" || task.Setup == nil || task.Verify == nil {
			t.Fatalf("task %q is incomplete: %+v", task.ID, task)
		}
		if len(task.RequiredTools) == 0 {
			t.Fatalf("task %q should declare required tools", task.ID)
		}
	}
}

func TestTestFailureFixVerification(t *testing.T) {
	task, ok := ByID("test_failure_fix")
	if !ok {
		t.Fatal("missing test_failure_fix task")
	}
	root := t.TempDir()
	if err := SetupTask(task, root); err != nil {
		t.Fatalf("SetupTask: %v", err)
	}

	failed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask failed module: %v", err)
	}
	if failed.Passed {
		t.Fatal("buggy fixture should fail verification")
	}

	fixed := `package evaltask

func Add(a, b int) int {
	return a + b
}
`
	if err := os.WriteFile(filepath.Join(root, "calc.go"), []byte(fixed), 0o644); err != nil {
		t.Fatalf("write fixed file: %v", err)
	}
	passed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask fixed module: %v", err)
	}
	if !passed.Passed {
		t.Fatalf("fixed fixture should pass verification: %s", passed.Reason)
	}
}

func TestLongProcessOutputVerification(t *testing.T) {
	task, ok := ByID("long_process_output")
	if !ok {
		t.Fatal("missing long_process_output task")
	}
	root := t.TempDir()
	if err := SetupTask(task, root); err != nil {
		t.Fatalf("SetupTask: %v", err)
	}

	failed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask missing marker: %v", err)
	}
	if failed.Passed {
		t.Fatal("missing observed file should fail verification")
	}

	if err := os.WriteFile(filepath.Join(root, "observed.txt"), []byte("READY_FOR_EVAL\n"), 0o644); err != nil {
		t.Fatalf("write observed file: %v", err)
	}
	passed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask observed marker: %v", err)
	}
	if !passed.Passed {
		t.Fatalf("observed marker should pass verification: %s", passed.Reason)
	}
}

func TestToolSearchDeferredVerification(t *testing.T) {
	task, ok := ByID("tool_search_deferred")
	if !ok {
		t.Fatal("missing tool_search_deferred task")
	}
	root := t.TempDir()
	if err := SetupTask(task, root); err != nil {
		t.Fatalf("SetupTask: %v", err)
	}

	failed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask missing marker: %v", err)
	}
	if failed.Passed {
		t.Fatal("missing deferred marker should fail verification")
	}

	if err := os.WriteFile(filepath.Join(root, "tool_search_result.txt"), []byte("DEFERRED_TOOL_FOUND\n"), 0o644); err != nil {
		t.Fatalf("write marker file: %v", err)
	}
	passed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask deferred marker: %v", err)
	}
	if !passed.Passed {
		t.Fatalf("deferred marker should pass verification: %s", passed.Reason)
	}
}

func TestStaleReadGuardVerification(t *testing.T) {
	task, ok := ByID("stale_read_guard")
	if !ok {
		t.Fatal("missing stale_read_guard task")
	}
	root := t.TempDir()
	if err := SetupTask(task, root); err != nil {
		t.Fatalf("SetupTask: %v", err)
	}

	failed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask missing stale marker: %v", err)
	}
	if failed.Passed {
		t.Fatal("missing stale marker should fail verification")
	}

	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("version: final\n"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "stale_read_result.txt"), []byte("STALE_READ_GUARD_DONE\n"), 0o644); err != nil {
		t.Fatalf("write stale marker: %v", err)
	}
	passed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask stale marker: %v", err)
	}
	if !passed.Passed {
		t.Fatalf("stale marker should pass verification: %s", passed.Reason)
	}
}

func TestMCPReadOnlyConcurrencyVerification(t *testing.T) {
	task, ok := ByID("mcp_readonly_concurrency")
	if !ok {
		t.Fatal("missing mcp_readonly_concurrency task")
	}
	root := t.TempDir()
	if err := SetupTask(task, root); err != nil {
		t.Fatalf("SetupTask: %v", err)
	}

	failed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask missing marker: %v", err)
	}
	if failed.Passed {
		t.Fatal("missing MCP marker should fail verification")
	}

	if err := os.WriteFile(filepath.Join(root, "mcp_readonly_result.txt"), []byte("MCP_READONLY_CONCURRENT\n"), 0o644); err != nil {
		t.Fatalf("write marker file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "mcp_max_concurrency.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatalf("write concurrency file: %v", err)
	}
	failed, err = VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask serial MCP calls: %v", err)
	}
	if failed.Passed {
		t.Fatal("serial MCP calls should fail verification")
	}

	if err := os.WriteFile(filepath.Join(root, "mcp_max_concurrency.txt"), []byte("2\n"), 0o644); err != nil {
		t.Fatalf("write concurrency file: %v", err)
	}
	passed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask concurrent MCP calls: %v", err)
	}
	if !passed.Passed {
		t.Fatalf("concurrent MCP calls should pass verification: %s", passed.Reason)
	}
}

func TestMCPLiveDiscoveryVerification(t *testing.T) {
	task, ok := ByID("mcp_live_discovery")
	if !ok {
		t.Fatal("missing mcp_live_discovery task")
	}
	root := t.TempDir()
	if err := SetupTask(task, root); err != nil {
		t.Fatalf("SetupTask: %v", err)
	}

	failed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask missing marker: %v", err)
	}
	if failed.Passed {
		t.Fatal("missing live MCP marker should fail verification")
	}

	if err := os.WriteFile(filepath.Join(root, "mcp_live_result.txt"), []byte("MCP_LIVE_DISCOVERY_DONE\n"), 0o644); err != nil {
		t.Fatalf("write incomplete marker file: %v", err)
	}
	failed, err = VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask missing MCP result text: %v", err)
	}
	if failed.Passed {
		t.Fatal("missing MCP tool result should fail verification")
	}

	if err := os.WriteFile(filepath.Join(root, "mcp_live_result.txt"), []byte("MCP_LIVE_DISCOVERY_DONE\nslow_a_OK\n"), 0o644); err != nil {
		t.Fatalf("write marker file: %v", err)
	}
	passed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask live MCP marker: %v", err)
	}
	if !passed.Passed {
		t.Fatalf("live MCP marker should pass verification: %s", passed.Reason)
	}
}

func TestMCPReadOnlyConcurrencyConfiguresLocalServer(t *testing.T) {
	task, ok := ByID("mcp_readonly_concurrency")
	if !ok {
		t.Fatal("missing mcp_readonly_concurrency task")
	}
	if task.Configure == nil {
		t.Fatal("mcp_readonly_concurrency should configure an MCP server")
	}

	root := t.TempDir()
	base := config.Config{
		MCPServers: map[string]config.MCPServerConfig{
			"existing": {Command: "existing-mcp"},
		},
	}
	cfg := task.Configure(root, base)
	if cfg.MCPServers["existing"].Command != "existing-mcp" {
		t.Fatalf("existing MCP server not preserved: %+v", cfg.MCPServers)
	}
	evalServer, ok := cfg.MCPServers["eval"]
	if !ok {
		t.Fatalf("eval MCP server not configured: %+v", cfg.MCPServers)
	}
	if evalServer.Command != "go" || len(evalServer.Args) != 3 || evalServer.Args[0] != "run" {
		t.Fatalf("unexpected eval MCP command: %+v", evalServer)
	}
	if _, mutated := base.MCPServers["eval"]; mutated {
		t.Fatal("Configure must not mutate the base config map")
	}
}

func TestMCPReadOnlyConcurrencyServerSourceCompiles(t *testing.T) {
	task, ok := ByID("mcp_readonly_concurrency")
	if !ok {
		t.Fatal("missing mcp_readonly_concurrency task")
	}
	root := t.TempDir()
	if err := SetupTask(task, root); err != nil {
		t.Fatalf("SetupTask: %v", err)
	}

	cmd := exec.Command("go", "test", filepath.Join(root, "mcp_eval_server.go"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("MCP eval server source should compile: %v\n%s", err, output)
	}
}

func TestMultiAgentWorkerVerification(t *testing.T) {
	task, ok := ByID("multi_agent_worker")
	if !ok {
		t.Fatal("missing multi_agent_worker task")
	}
	root := t.TempDir()
	if err := SetupTask(task, root); err != nil {
		t.Fatalf("SetupTask: %v", err)
	}

	failed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask missing worker marker: %v", err)
	}
	if failed.Passed {
		t.Fatal("missing worker marker should fail verification")
	}

	if err := os.WriteFile(filepath.Join(root, "worker_result.txt"), []byte("SUBAGENT_EVAL_DONE\n"), 0o644); err != nil {
		t.Fatalf("write worker marker: %v", err)
	}
	passed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask worker marker: %v", err)
	}
	if !passed.Passed {
		t.Fatalf("worker marker should pass verification: %s", passed.Reason)
	}
}

func TestDynamicWorkflowTeamVerification(t *testing.T) {
	task, ok := ByID("dynamic_workflow_team")
	if !ok {
		t.Fatal("missing dynamic_workflow_team task")
	}
	if !task.IsolateWuuHome {
		t.Fatal("dynamic workflow team eval should isolate WUU_HOME")
	}
	root := t.TempDir()
	if err := SetupTask(task, root); err != nil {
		t.Fatalf("SetupTask: %v", err)
	}

	failed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask missing markers: %v", err)
	}
	if failed.Passed {
		t.Fatal("missing team markers should fail verification")
	}

	if err := os.WriteFile(filepath.Join(root, "team_alpha.txt"), []byte("TEAM_ALPHA_DONE\n"), 0o644); err != nil {
		t.Fatalf("write alpha marker: %v", err)
	}
	failed, err = VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask missing beta marker: %v", err)
	}
	if failed.Passed {
		t.Fatal("missing beta marker should fail verification")
	}

	if err := os.WriteFile(filepath.Join(root, "team_beta.txt"), []byte("TEAM_BETA_DONE\n"), 0o644); err != nil {
		t.Fatalf("write beta marker: %v", err)
	}
	passed, err := VerifyTask(context.Background(), task, root, "")
	if err != nil {
		t.Fatalf("VerifyTask team markers: %v", err)
	}
	if !passed.Passed {
		t.Fatalf("team markers should pass verification: %s", passed.Reason)
	}
}
