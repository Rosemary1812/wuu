package pluginhost

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestProcessClientLifecycleAndInvoke(t *testing.T) {
	if os.Getenv("WUU_PLUGINHOST_HELPER") == "1" {
		runPluginHelper()
		return
	}
	root := t.TempDir()
	client, err := Start(context.Background(), ProcessConfig{
		ID:          "test-plugin",
		Command:     os.Args[0],
		Args:        []string{"-test.run=TestProcessClientLifecycleAndInvoke"},
		Env:         map[string]string{"WUU_PLUGINHOST_HELPER": "1"},
		PluginRoot:  root,
		ProjectRoot: filepath.Dir(root),
		WuuHome:     t.TempDir(),
		Timeout:     2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Status().State != StateActive || !hasHook(client.Hooks(), HookChatMessage) {
		t.Fatalf("status = %+v", client.Status())
	}
	input, _ := json.Marshal(map[string]string{"session_id": "s1"})
	output, _ := json.Marshal(map[string]string{"message": "hello"})
	result, err := client.Invoke(context.Background(), InvokeParams{Hook: HookChatMessage, Input: input, Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.Output), "hello from plugin") {
		t.Fatalf("output = %s", result.Output)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.Status().State != StateStopped {
		t.Fatalf("status = %+v", client.Status())
	}
}

func runPluginHelper() {
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			os.Exit(2)
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			var params InitializeParams
			_ = json.Unmarshal(req.Params, &params)
			if params.ProtocolVersion != ProtocolVersion {
				os.Exit(3)
			}
			result = InitializeResult{Hooks: []Hook{HookChatMessage}}
		case "hook.invoke":
			var params InvokeParams
			_ = json.Unmarshal(req.Params, &params)
			var out map[string]string
			_ = json.Unmarshal(params.Output, &out)
			out["message"] += " from plugin"
			data, _ := json.Marshal(out)
			result = InvokeResult{Output: data}
		case "shutdown":
			_ = enc.Encode(map[string]any{"id": req.ID, "result": result})
			return
		default:
			_ = enc.Encode(map[string]any{"id": req.ID, "error": map[string]string{"message": fmt.Sprintf("unknown method %s", req.Method)}})
			continue
		}
		_ = enc.Encode(map[string]any{"id": req.ID, "result": result})
	}
}

func TestProcessClientRejectsUnknownDeclaredHook(t *testing.T) {
	if os.Getenv("WUU_PLUGINHOST_BAD_HELPER") == "1" {
		enc := json.NewEncoder(os.Stdout)
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			var req rpcRequest
			_ = json.Unmarshal(scanner.Bytes(), &req)
			_ = enc.Encode(map[string]any{"id": req.ID, "result": InitializeResult{Hooks: []Hook{"not.real"}}})
		}
		return
	}
	_, err := Start(context.Background(), ProcessConfig{
		ID:         "bad-plugin",
		Command:    os.Args[0],
		Args:       []string{"-test.run=TestProcessClientRejectsUnknownDeclaredHook"},
		Env:        map[string]string{"WUU_PLUGINHOST_BAD_HELPER": "1"},
		PluginRoot: t.TempDir(),
		Timeout:    2 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "unknown hook") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildEnvUsesBaselineAndExplicitOverrides(t *testing.T) {
	// Set baseline variables that should be inherited.
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HOME", "/home/test")
	t.Setenv("LANG", "en_US.UTF-8")

	// A parent secret must not leak into the plugin environment.
	t.Setenv("WUU_PARENT_SECRET_TOKEN", "super-secret")

	got := buildEnv(map[string]string{"EXPLICIT": "yes", "PATH": "/override"})
	m := envSliceToMap(got)

	if m["PATH"] != "/override" {
		t.Fatalf("explicit override lost: PATH=%q", m["PATH"])
	}
	if m["HOME"] != "/home/test" {
		t.Fatalf("baseline HOME missing: %q", m["HOME"])
	}
	if m["LANG"] != "en_US.UTF-8" {
		t.Fatalf("baseline LANG missing: %q", m["LANG"])
	}
	if m["EXPLICIT"] != "yes" {
		t.Fatalf("explicit env missing: %q", m["EXPLICIT"])
	}
	if _, ok := m["WUU_PARENT_SECRET_TOKEN"]; ok {
		t.Fatalf("parent secret inherited: %v", m)
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("env slice not sorted: %v", got)
	}
}

func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		key, value, _ := strings.Cut(e, "=")
		m[key] = value
	}
	return m
}

func TestProcessEnvDoesNotInheritSecrets(t *testing.T) {
	if os.Getenv("WUU_PLUGINHOST_ENV_HELPER") == "1" {
		runEnvHelper()
		return
	}
	t.Setenv("WUU_PARENT_SECRET_TOKEN", "super-secret")

	root := t.TempDir()
	client, err := Start(context.Background(), ProcessConfig{
		ID:          "env-plugin",
		Command:     os.Args[0],
		Args:        []string{"-test.run=TestProcessEnvDoesNotInheritSecrets"},
		Env:         map[string]string{"WUU_PLUGINHOST_ENV_HELPER": "1"},
		PluginRoot:  root,
		ProjectRoot: filepath.Dir(root),
		WuuHome:     t.TempDir(),
		Timeout:     2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Logf("close: %v", err)
		}
	}()

	input, _ := json.Marshal(map[string]string{"name": "WUU_PARENT_SECRET_TOKEN"})
	output, _ := json.Marshal(map[string]string{})
	result, err := client.Invoke(context.Background(), InvokeParams{Hook: HookChatMessage, Input: input, Output: output})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	if err := json.Unmarshal(result.Output, &out); err != nil {
		t.Fatal(err)
	}
	if out["value"] != "" {
		t.Fatalf("parent secret token was inherited: %q", out["value"])
	}
}

func runEnvHelper() {
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			os.Exit(2)
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = InitializeResult{Hooks: []Hook{HookChatMessage}}
		case "hook.invoke":
			var params InvokeParams
			_ = json.Unmarshal(req.Params, &params)
			var query map[string]string
			_ = json.Unmarshal(params.Input, &query)
			value := os.Getenv(query["name"])
			data, _ := json.Marshal(map[string]string{"value": value})
			result = InvokeResult{Output: data}
		case "shutdown":
			_ = enc.Encode(map[string]any{"id": req.ID, "result": result})
			return
		}
		_ = enc.Encode(map[string]any{"id": req.ID, "result": result})
	}
}

func TestProcessOversizeResponse(t *testing.T) {
	if os.Getenv("WUU_PLUGINHOST_OVERSIZE_HELPER") == "1" {
		runOversizeHelper()
		return
	}
	root := t.TempDir()
	client, err := Start(context.Background(), ProcessConfig{
		ID:          "oversize-plugin",
		Command:     os.Args[0],
		Args:        []string{"-test.run=TestProcessOversizeResponse"},
		Env:         map[string]string{"WUU_PLUGINHOST_OVERSIZE_HELPER": "1"},
		PluginRoot:  root,
		ProjectRoot: filepath.Dir(root),
		WuuHome:     t.TempDir(),
		Timeout:     5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Close(context.Background()); err != nil {
			t.Logf("close: %v", err)
		}
	}()

	input, _ := json.Marshal(map[string]string{})
	output, _ := json.Marshal(map[string]string{})
	_, err = client.Invoke(context.Background(), InvokeParams{Hook: HookChatMessage, Input: input, Output: output})
	if err == nil {
		t.Fatal("expected oversize response error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func runOversizeHelper() {
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			os.Exit(2)
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = InitializeResult{Hooks: []Hook{HookChatMessage}}
		case "hook.invoke":
			// Produce one JSON-lines token larger than maxResponseLineSize.
			big := strings.Repeat("x", maxResponseLineSize+1024)
			payload, _ := json.Marshal(map[string]string{"message": big})
			result = InvokeResult{Output: payload}
		case "shutdown":
			_ = enc.Encode(map[string]any{"id": req.ID, "result": result})
			return
		}
		_ = enc.Encode(map[string]any{"id": req.ID, "result": result})
	}
}

func TestBaselineEnvKeyListHasNoDuplicates(t *testing.T) {
	// Duplicate keys should not appear in the baseline key list; it must be
	// stable so buildEnv output is deterministic across platforms.
	seen := make(map[string]int, len(baselineEnvKeys))
	for _, key := range baselineEnvKeys {
		seen[key]++
	}
	for key, count := range seen {
		if count != 1 {
			t.Fatalf("baseline key %q appears %d times", key, count)
		}
	}
}
