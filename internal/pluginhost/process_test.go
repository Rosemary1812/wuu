package pluginhost

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func TestMergeEnvOverridesBaseDeterministically(t *testing.T) {
	got := mergeEnv([]string{"B=old", "A=one"}, map[string]string{"B": "new", "C": "three"})
	want := []string{"A=one", "B=new", "C=three"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("env = %v", got)
	}
}
