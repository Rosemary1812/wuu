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
)

// Task is one deterministic local evaluation scenario.
type Task struct {
	ID            string
	Name          string
	Description   string
	Prompt        string
	RequiredTools []string
	Setup         func(root string) error
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
