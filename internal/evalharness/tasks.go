package evalharness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/config"
)

// Task is one deterministic local evaluation scenario.
type Task struct {
	ID             string
	Name           string
	Description    string
	Prompt         string
	RequiredTools  []string
	RequiredErrors []ToolErrorRequirement
	IsolateWuuHome bool
	Setup          func(root string) error
	Configure      func(root string, cfg config.Config) config.Config
	Verify         func(ctx context.Context, root, answer string) (Verification, error)
}

type ToolErrorRequirement struct {
	ToolName      string `json:"tool_name"`
	ErrorContains string `json:"error_contains"`
}

type Verification struct {
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

type Result struct {
	TaskID             string         `json:"task_id"`
	TaskName           string         `json:"task_name"`
	Success            bool           `json:"success"`
	DurationMS         int64          `json:"duration_ms"`
	Turns              int            `json:"turns"`
	ToolCalls          int            `json:"tool_calls"`
	ToolNames          []string       `json:"tool_names,omitempty"`
	MissingTools       []string       `json:"missing_tools,omitempty"`
	MissingErrors      []string       `json:"missing_errors,omitempty"`
	InputTokens        int            `json:"input_tokens"`
	OutputTokens       int            `json:"output_tokens"`
	VerificationReason string         `json:"verification_reason,omitempty"`
	Error              string         `json:"error,omitempty"`
	Workdir            string         `json:"workdir,omitempty"`
	Observability      *Observability `json:"observability,omitempty"`
}

type Observability struct {
	SessionID          string                     `json:"session_id,omitempty"`
	StateDir           string                     `json:"state_dir,omitempty"`
	SessionDir         string                     `json:"session_dir,omitempty"`
	HarnessDir         string                     `json:"harness_dir,omitempty"`
	WorkflowDir        string                     `json:"workflow_dir,omitempty"`
	TaskWorkdir        string                     `json:"task_workdir,omitempty"`
	TaskWorkdirKept    bool                       `json:"task_workdir_kept,omitempty"`
	FinalAnswerPreview string                     `json:"final_answer_preview,omitempty"`
	ToolRecords        []ToolObservation          `json:"tool_records,omitempty"`
	WorkflowRuns       []WorkflowRunObservation   `json:"workflow_runs,omitempty"`
	HarnessTasks       []HarnessTaskObservation   `json:"harness_tasks,omitempty"`
	HarnessReports     []HarnessReportObservation `json:"harness_reports,omitempty"`
	Warnings           []string                   `json:"warnings,omitempty"`
}

type ToolObservation struct {
	Name                string    `json:"name"`
	CallID              string    `json:"call_id,omitempty"`
	Kind                string    `json:"kind,omitempty"`
	Exposure            string    `json:"exposure,omitempty"`
	Risk                string    `json:"risk,omitempty"`
	PolicyAction        string    `json:"policy_action,omitempty"`
	ReadOnly            bool      `json:"read_only"`
	ConcurrencySafe     bool      `json:"concurrency_safe"`
	StartedAt           time.Time `json:"started_at,omitempty"`
	DurationMS          int64     `json:"duration_ms"`
	Success             bool      `json:"success"`
	Error               string    `json:"error,omitempty"`
	RawOutputBytes      int       `json:"raw_output_bytes,omitempty"`
	ReturnedOutputBytes int       `json:"returned_output_bytes,omitempty"`
	ResultBudgeted      bool      `json:"result_budgeted,omitempty"`
}

type WorkflowRunObservation struct {
	ID              string                        `json:"id"`
	DefinitionName  string                        `json:"definition_name,omitempty"`
	Status          string                        `json:"status"`
	Error           string                        `json:"error,omitempty"`
	ScriptPath      string                        `json:"script_path,omitempty"`
	FinalReportPath string                        `json:"final_report_path,omitempty"`
	Phases          []WorkflowPhaseObservation    `json:"phases,omitempty"`
	AgentRuns       []WorkflowAgentRunObservation `json:"agent_runs,omitempty"`
	EventCount      int                           `json:"event_count,omitempty"`
}

type WorkflowPhaseObservation struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Status      string   `json:"status"`
	Error       string   `json:"error,omitempty"`
	AgentRunIDs []string `json:"agent_run_ids,omitempty"`
}

type WorkflowAgentRunObservation struct {
	ID            string   `json:"id"`
	PhaseID       string   `json:"phase_id,omitempty"`
	AgentID       string   `json:"agent_id,omitempty"`
	AgentPath     string   `json:"agent_path,omitempty"`
	TaskName      string   `json:"task_name,omitempty"`
	AgentProfile  string   `json:"agent_profile,omitempty"`
	Status        string   `json:"status"`
	ReportPath    string   `json:"report_path,omitempty"`
	ReportMissing bool     `json:"report_missing,omitempty"`
	ChangedFiles  []string `json:"changed_files,omitempty"`
	Artifacts     []string `json:"artifacts,omitempty"`
	WorktreePath  string   `json:"worktree_path,omitempty"`
	InputTokens   int      `json:"input_tokens,omitempty"`
	OutputTokens  int      `json:"output_tokens,omitempty"`
	DurationMS    int64    `json:"duration_ms,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type HarnessTaskObservation struct {
	ID            string   `json:"id"`
	ParentID      string   `json:"parent_id,omitempty"`
	Path          string   `json:"path,omitempty"`
	Name          string   `json:"name,omitempty"`
	Role          string   `json:"role,omitempty"`
	Status        string   `json:"status"`
	ReportPath    string   `json:"report_path,omitempty"`
	ArtifactPaths []string `json:"artifact_paths,omitempty"`
	InputTokens   int      `json:"input_tokens,omitempty"`
	OutputTokens  int      `json:"output_tokens,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type HarnessReportObservation struct {
	ID           string   `json:"id"`
	TaskID       string   `json:"task_id,omitempty"`
	RunID        string   `json:"run_id,omitempty"`
	AgentID      string   `json:"agent_id,omitempty"`
	AgentPath    string   `json:"agent_path,omitempty"`
	Outcome      string   `json:"outcome"`
	Summary      string   `json:"summary,omitempty"`
	ChangedFiles []string `json:"changed_files,omitempty"`
	Verification []string `json:"verification,omitempty"`
	Artifacts    []string `json:"artifacts,omitempty"`
	ReportPath   string   `json:"report_path,omitempty"`
}

func Catalog() []Task {
	return []Task{
		{
			ID:            "test_failure_fix",
			Name:          "Fix a failing unit test",
			Description:   "Small Go module with one implementation bug and a failing test.",
			Prompt:        "Run the tests, find the implementation bug, fix the source code without changing tests, and verify the tests pass.",
			RequiredTools: []string{"run_shell"},
			Setup:         setupTestFailureFix,
			Verify:        verifyGoTests,
		},
		{
			ID:            "multi_file_pricing",
			Name:          "Fix behavior across two files",
			Description:   "Go package where the final behavior requires edits in two implementation files.",
			Prompt:        "Fix the pricing package so all tests pass. The bug spans multiple source files. Do not change tests.",
			RequiredTools: []string{"run_shell"},
			Setup:         setupMultiFilePricing,
			Verify:        verifyGoTests,
		},
		{
			ID:            "long_process_output",
			Name:          "Read a long-running process log",
			Description:   "Script prints a readiness marker after a delay and keeps running.",
			Prompt:        "Start ./dev.sh as a managed background process, read its output until READY_FOR_EVAL appears, write observed.txt containing READY_FOR_EVAL, then stop the process.",
			RequiredTools: []string{"start_process", "read_process_output", "stop_process"},
			Setup:         setupLongProcessOutput,
			Verify:        verifyObservedReadyFile,
		},
		{
			ID:            "tool_search_deferred",
			Name:          "Discover and use a deferred tool",
			Description:   "The cron listing tool starts deferred and must be exposed through tool_search.",
			Prompt:        "Find the tool for listing scheduled tasks using tool_search, call that tool, then write tool_search_result.txt containing DEFERRED_TOOL_FOUND.",
			RequiredTools: []string{"tool_search", "list_cron"},
			Setup:         setupEmptyTask,
			Verify:        verifyDeferredToolFoundFile,
		},
		{
			ID:          "stale_read_guard",
			Name:        "Recover from a stale file read",
			Description: "Simulates an external file change after read_file; eval requires edit_file to reject the stale read before recovery.",
			Prompt: "Read target.txt, then run ./mutate_after_read.sh to simulate an external edit. Next, try to use edit_file to change " +
				"\"version: external\" to \"version: final\". If edit_file reports that the file changed since last read, read target.txt " +
				"again and retry the edit. Finally write stale_read_result.txt containing STALE_READ_GUARD_DONE.",
			RequiredTools:  []string{"read_file", "run_shell", "edit_file", "write_file"},
			RequiredErrors: []ToolErrorRequirement{{ToolName: "edit_file", ErrorContains: "changed since last read"}},
			Setup:          setupStaleReadGuard,
			Verify:         verifyStaleReadGuard,
		},
		{
			ID:          "mcp_readonly_concurrency",
			Name:        "Run read-only MCP tools concurrently",
			Description: "Local MCP server exposes readOnlyHint=true slow tools; eval passes only if their calls overlap.",
			Prompt: "Use tool_search to find the MCP eval read-only slow tools. Expose both of them, then call " +
				"mcp_eval_slow_a and mcp_eval_slow_b together in the same assistant response before writing " +
				"mcp_readonly_result.txt containing MCP_READONLY_CONCURRENT.",
			RequiredTools: []string{"tool_search", "mcp_eval_slow_a", "mcp_eval_slow_b", "write_file"},
			Setup:         setupMCPReadOnlyConcurrency,
			Configure:     configureMCPReadOnlyConcurrency,
			Verify:        verifyMCPReadOnlyConcurrency,
		},
		{
			ID:          "mcp_live_discovery",
			Name:        "Discover and call an MCP tool",
			Description: "Live-friendly MCP task: model discovers a deferred MCP tool, calls it, and writes a marker.",
			Prompt: "Use tool_search to find the MCP eval read-only slow tool A. Call mcp_eval_slow_a, then write " +
				"mcp_live_result.txt containing MCP_LIVE_DISCOVERY_DONE and the tool result text.",
			RequiredTools: []string{"tool_search", "mcp_eval_slow_a", "write_file"},
			Setup:         setupMCPReadOnlyConcurrency,
			Configure:     configureMCPReadOnlyConcurrency,
			Verify:        verifyMCPLiveDiscovery,
		},
		{
			ID:            "multi_agent_worker",
			Name:          "Delegate work to a sub-agent",
			Description:   "Main agent must spawn an async worker and wait for it to produce a marker file.",
			Prompt:        "Spawn an async worker named eval_worker with fork_turns='none'. Ask it to write worker_result.txt containing SUBAGENT_EVAL_DONE, then call wait_agent until the worker completes. Do not write worker_result.txt yourself.",
			RequiredTools: []string{"spawn_agent", "wait_agent"},
			Setup:         setupEmptyTask,
			Verify:        verifySubAgentWorkerFile,
		},
		{
			ID:          "dynamic_workflow_team",
			Name:        "Run a dynamic workflow team",
			Description: "Main agent must record a workflow team plan, create a durable profile member, spawn workers, await them, and complete the run.",
			Prompt: "Create a manual workflow run with run_id='eval_dynamic_team' and one phase id='team_work'. " +
				"First call list_agent_profiles. Then call create_workflow for the run. Next, record a dynamic team plan with workflow_control action=record_team_plan containing exactly two members: " +
				"one create_profile member with role='Marker writer', agent_profile='eval_team_marker_writer', task_name='alpha_writer', phase_id='team_work'; " +
				"and one ephemeral member with role='Independent verifier', task_name='beta_writer', phase_id='team_work'. " +
				"Spawn both workers with fork_turns='none' and self-contained briefs from the Base Agent Brief Contract plus the Workflow Context Extension. " +
				"The create_profile worker must use agent_profile='eval_team_marker_writer' and write team_alpha.txt containing TEAM_ALPHA_DONE. " +
				"The ephemeral worker must omit agent_profile and write team_beta.txt containing TEAM_BETA_DONE. " +
				"Require both workers to call agent_report. Await both with await_agents, bind the results back to the workflow using workflow_control action=record_await_results, then write a final workflow report with complete_run=true. Do not write team_alpha.txt or team_beta.txt yourself.",
			RequiredTools:  []string{"list_agent_profiles", "create_workflow", "workflow_control", "spawn_agent", "await_agents"},
			IsolateWuuHome: true,
			Setup:          setupEmptyTask,
			Verify:         verifyDynamicWorkflowTeam,
		},
	}
}

func ByID(id string) (Task, bool) {
	id = strings.TrimSpace(id)
	for _, task := range Catalog() {
		if task.ID == id {
			return task, true
		}
	}
	return Task{}, false
}

func SetupTask(task Task, root string) error {
	if strings.TrimSpace(task.ID) == "" {
		return errors.New("task id is required")
	}
	if task.Setup == nil {
		return fmt.Errorf("task %q has no setup", task.ID)
	}
	return task.Setup(root)
}

func VerifyTask(ctx context.Context, task Task, root, answer string) (Verification, error) {
	if task.Verify == nil {
		return Verification{}, fmt.Errorf("task %q has no verifier", task.ID)
	}
	return task.Verify(ctx, root, answer)
}

func setupTestFailureFix(root string) error {
	files := map[string]string{
		"go.mod": `module evaltask

go 1.22
`,
		"calc.go": `package evaltask

func Add(a, b int) int {
	return a - b
}
`,
		"calc_test.go": `package evaltask

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d, want 5", got)
	}
}
`,
	}
	return writeFiles(root, files)
}

func setupMultiFilePricing(root string) error {
	files := map[string]string{
		"go.mod": `module pricingeval

go 1.22
`,
		"pricing/subtotal.go": `package pricing

func Subtotal(cents []int) int {
	return 0
}
`,
		"pricing/tax.go": `package pricing

func TotalWithTax(cents []int, taxBasisPoints int) int {
	return Subtotal(cents)
}
`,
		"pricing/pricing_test.go": `package pricing

import "testing"

func TestSubtotal(t *testing.T) {
	if got := Subtotal([]int{1200, 350, 450}); got != 2000 {
		t.Fatalf("Subtotal() = %d, want 2000", got)
	}
}

func TestTotalWithTax(t *testing.T) {
	if got := TotalWithTax([]int{1000, 1000}, 875); got != 2175 {
		t.Fatalf("TotalWithTax() = %d, want 2175", got)
	}
}
`,
	}
	return writeFiles(root, files)
}

func setupLongProcessOutput(root string) error {
	files := map[string]string{
		"dev.sh": `#!/usr/bin/env bash
sleep 0.2
echo READY_FOR_EVAL
sleep 20
`,
	}
	if err := writeFiles(root, files); err != nil {
		return err
	}
	return os.Chmod(filepath.Join(root, "dev.sh"), 0o755)
}

func setupStaleReadGuard(root string) error {
	files := map[string]string{
		"target.txt": `version: original
`,
		"mutate_after_read.sh": `#!/usr/bin/env bash
printf 'version: external\n' > target.txt
`,
	}
	if err := writeFiles(root, files); err != nil {
		return err
	}
	return os.Chmod(filepath.Join(root, "mutate_after_read.sh"), 0o755)
}

func setupEmptyTask(root string) error {
	return writeFiles(root, map[string]string{".keep": ""})
}

func setupMCPReadOnlyConcurrency(root string) error {
	return writeFiles(root, map[string]string{
		"mcp_eval_server.go": mcpEvalServerSource,
		".keep":              "",
	})
}

func configureMCPReadOnlyConcurrency(root string, cfg config.Config) config.Config {
	servers := make(map[string]config.MCPServerConfig, len(cfg.MCPServers)+1)
	for name, server := range cfg.MCPServers {
		servers[name] = server
	}
	servers["eval"] = config.MCPServerConfig{
		Command: "go",
		Args: []string{
			"run",
			filepath.Join(root, "mcp_eval_server.go"),
			filepath.Join(root, "mcp_max_concurrency.txt"),
		},
	}
	cfg.MCPServers = servers
	return cfg
}

func verifyGoTests(ctx context.Context, root, _ string) (Verification, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "go", "test", "./...")
	cmd.Dir = root
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if cmdCtx.Err() != nil {
		return Verification{Passed: false, Reason: "go test timed out"}, nil
	}
	if err != nil {
		return Verification{Passed: false, Reason: strings.TrimSpace(output.String())}, nil
	}
	return Verification{Passed: true, Reason: strings.TrimSpace(output.String())}, nil
}

func verifyObservedReadyFile(_ context.Context, root, _ string) (Verification, error) {
	data, err := os.ReadFile(filepath.Join(root, "observed.txt"))
	if err != nil {
		return Verification{Passed: false, Reason: "observed.txt was not written"}, nil
	}
	if !strings.Contains(string(data), "READY_FOR_EVAL") {
		return Verification{Passed: false, Reason: "observed.txt does not contain READY_FOR_EVAL"}, nil
	}
	return Verification{Passed: true, Reason: "observed readiness marker"}, nil
}

func verifyDeferredToolFoundFile(_ context.Context, root, _ string) (Verification, error) {
	data, err := os.ReadFile(filepath.Join(root, "tool_search_result.txt"))
	if err != nil {
		return Verification{Passed: false, Reason: "tool_search_result.txt was not written"}, nil
	}
	if !strings.Contains(string(data), "DEFERRED_TOOL_FOUND") {
		return Verification{Passed: false, Reason: "tool_search_result.txt does not contain DEFERRED_TOOL_FOUND"}, nil
	}
	return Verification{Passed: true, Reason: "observed deferred tool marker"}, nil
}

func verifyStaleReadGuard(_ context.Context, root, _ string) (Verification, error) {
	target, err := os.ReadFile(filepath.Join(root, "target.txt"))
	if err != nil {
		return Verification{Passed: false, Reason: "target.txt was not readable"}, nil
	}
	if !strings.Contains(string(target), "version: final") {
		return Verification{Passed: false, Reason: "target.txt does not contain final version"}, nil
	}
	data, err := os.ReadFile(filepath.Join(root, "stale_read_result.txt"))
	if err != nil {
		return Verification{Passed: false, Reason: "stale_read_result.txt was not written"}, nil
	}
	if !strings.Contains(string(data), "STALE_READ_GUARD_DONE") {
		return Verification{Passed: false, Reason: "stale_read_result.txt does not contain STALE_READ_GUARD_DONE"}, nil
	}
	return Verification{Passed: true, Reason: "observed stale-read recovery marker"}, nil
}

func verifyMCPReadOnlyConcurrency(_ context.Context, root, _ string) (Verification, error) {
	data, err := os.ReadFile(filepath.Join(root, "mcp_readonly_result.txt"))
	if err != nil {
		return Verification{Passed: false, Reason: "mcp_readonly_result.txt was not written"}, nil
	}
	if !strings.Contains(string(data), "MCP_READONLY_CONCURRENT") {
		return Verification{Passed: false, Reason: "mcp_readonly_result.txt does not contain MCP_READONLY_CONCURRENT"}, nil
	}

	maxData, err := os.ReadFile(filepath.Join(root, "mcp_max_concurrency.txt"))
	if err != nil {
		return Verification{Passed: false, Reason: "MCP server did not record concurrency"}, nil
	}
	if strings.TrimSpace(string(maxData)) != "2" {
		return Verification{Passed: false, Reason: "MCP read-only tools did not overlap; max concurrency was " + strings.TrimSpace(string(maxData))}, nil
	}
	return Verification{Passed: true, Reason: "observed overlapping read-only MCP tool calls"}, nil
}

func verifyMCPLiveDiscovery(_ context.Context, root, _ string) (Verification, error) {
	data, err := os.ReadFile(filepath.Join(root, "mcp_live_result.txt"))
	if err != nil {
		return Verification{Passed: false, Reason: "mcp_live_result.txt was not written"}, nil
	}
	text := string(data)
	if !strings.Contains(text, "MCP_LIVE_DISCOVERY_DONE") {
		return Verification{Passed: false, Reason: "mcp_live_result.txt does not contain MCP_LIVE_DISCOVERY_DONE"}, nil
	}
	if !strings.Contains(text, "slow_a_OK") {
		return Verification{Passed: false, Reason: "mcp_live_result.txt does not contain the MCP tool result"}, nil
	}
	return Verification{Passed: true, Reason: "observed live MCP discovery marker"}, nil
}

func verifySubAgentWorkerFile(_ context.Context, root, _ string) (Verification, error) {
	data, err := os.ReadFile(filepath.Join(root, "worker_result.txt"))
	if err != nil {
		return Verification{Passed: false, Reason: "worker_result.txt was not written"}, nil
	}
	if !strings.Contains(string(data), "SUBAGENT_EVAL_DONE") {
		return Verification{Passed: false, Reason: "worker_result.txt does not contain SUBAGENT_EVAL_DONE"}, nil
	}
	return Verification{Passed: true, Reason: "observed sub-agent worker marker"}, nil
}

func verifyDynamicWorkflowTeam(_ context.Context, root, _ string) (Verification, error) {
	required := map[string]string{
		"team_alpha.txt": "TEAM_ALPHA_DONE",
		"team_beta.txt":  "TEAM_BETA_DONE",
	}
	for name, marker := range required {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return Verification{Passed: false, Reason: name + " was not written"}, nil
		}
		if !strings.Contains(string(data), marker) {
			return Verification{Passed: false, Reason: name + " does not contain " + marker}, nil
		}
	}
	return Verification{Passed: true, Reason: "observed dynamic workflow team markers"}, nil
}

func writeFiles(root string, files map[string]string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("root is required")
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

const mcpEvalServerSource = `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type rpcRequest struct {
	JSONRPC string          ` + "`json:\"jsonrpc\"`" + `
	ID      any             ` + "`json:\"id,omitempty\"`" + `
	Method  string          ` + "`json:\"method\"`" + `
	Params  json.RawMessage ` + "`json:\"params,omitempty\"`" + `
}

type rpcResponse struct {
	JSONRPC string ` + "`json:\"jsonrpc\"`" + `
	ID      any    ` + "`json:\"id,omitempty\"`" + `
	Result  any    ` + "`json:\"result,omitempty\"`" + `
	Error   any    ` + "`json:\"error,omitempty\"`" + `
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "missing max concurrency path")
		os.Exit(2)
	}
	maxPath := os.Args[1]
	_ = os.WriteFile(maxPath, []byte("0\n"), 0o644)

	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	var sendMu sync.Mutex
	send := func(resp rpcResponse) {
		sendMu.Lock()
		defer sendMu.Unlock()
		_ = encoder.Encode(resp)
	}

	var current int64
	var maxSeen int64
	recordMax := func(value int64) {
		for {
			old := atomic.LoadInt64(&maxSeen)
			if value <= old || atomic.CompareAndSwapInt64(&maxSeen, old, value) {
				_ = os.WriteFile(maxPath, []byte(fmt.Sprintf("%d\n", atomic.LoadInt64(&maxSeen))), 0o644)
				return
			}
		}
	}

	for scanner.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			send(rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities": map[string]any{
						"tools": map[string]any{"listChanged": false},
					},
					"serverInfo": map[string]any{"name": "wuu-eval-mcp", "version": "0.1.0"},
				},
			})
		case "notifications/initialized":
			// Notification only.
		case "tools/list":
			send(rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"tools": []map[string]any{
						{
							"name":        "slow_a",
							"description": "MCP eval read-only slow tool A. Use with slow_b to test concurrency.",
							"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
							"annotations": map[string]any{"readOnlyHint": true},
						},
						{
							"name":        "slow_b",
							"description": "MCP eval read-only slow tool B. Use with slow_a to test concurrency.",
							"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
							"annotations": map[string]any{"readOnlyHint": true},
						},
					},
				},
			})
		case "tools/call":
			var params struct {
				Name string ` + "`json:\"name\"`" + `
			}
			_ = json.Unmarshal(req.Params, &params)
			go func(id any, name string) {
				active := atomic.AddInt64(&current, 1)
				recordMax(active)
				time.Sleep(400 * time.Millisecond)
				atomic.AddInt64(&current, -1)
				send(rpcResponse{
					JSONRPC: "2.0",
					ID:      id,
					Result: map[string]any{
						"content": []map[string]string{{"type": "text", "text": name + "_OK"}},
					},
				})
			}(req.ID, params.Name)
		default:
			send(rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]any{
					"code":    -32601,
					"message": "method not found",
				},
			})
		}
	}
}
`
