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
	ID            string
	Name          string
	Description   string
	Prompt        string
	RequiredTools []string
	Setup         func(root string) error
	Configure     func(root string, cfg config.Config) config.Config
	Verify        func(ctx context.Context, root, answer string) (Verification, error)
}

type Verification struct {
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

type Result struct {
	TaskID             string   `json:"task_id"`
	TaskName           string   `json:"task_name"`
	Success            bool     `json:"success"`
	DurationMS         int64    `json:"duration_ms"`
	Turns              int      `json:"turns"`
	ToolCalls          int      `json:"tool_calls"`
	ToolNames          []string `json:"tool_names,omitempty"`
	MissingTools       []string `json:"missing_tools,omitempty"`
	InputTokens        int      `json:"input_tokens"`
	OutputTokens       int      `json:"output_tokens"`
	VerificationReason string   `json:"verification_reason,omitempty"`
	Error              string   `json:"error,omitempty"`
	Workdir            string   `json:"workdir,omitempty"`
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
			ID:            "multi_agent_worker",
			Name:          "Delegate work to a sub-agent",
			Description:   "Main agent must spawn an async worker and wait for it to produce a marker file.",
			Prompt:        "Spawn an async worker named eval_worker with fork_turns='none'. Ask it to write worker_result.txt containing SUBAGENT_EVAL_DONE, then call wait_agent until the worker completes. Do not write worker_result.txt yourself.",
			RequiredTools: []string{"spawn_agent", "wait_agent"},
			Setup:         setupEmptyTask,
			Verify:        verifySubAgentWorkerFile,
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
