package appserver

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/blueberrycongee/wuu/internal/memory"
	"github.com/blueberrycongee/wuu/internal/runtime"
)

func TestInstructionFileScope(t *testing.T) {
	cases := map[string]string{
		"user":        "global",
		"USER":        "global",
		"  user  ":    "global",
		"project":     "project",
		"local":       "project",
		"claude_auto": "project",
		"":            "project",
	}
	for source, want := range cases {
		if got := instructionFileScope(source); got != want {
			t.Errorf("instructionFileScope(%q) = %q, want %q", source, got, want)
		}
	}
}

func TestHandleInstructionsList(t *testing.T) {
	var buf bytes.Buffer
	srv := &Server{
		out: &buf,
		rt: &runtime.Session{
			Memory: []memory.File{
				{Path: "/home/u/.config/wuu/AGENTS.md", Name: "AGENTS.md", Source: "user", Content: "global rules"},
				{Path: "/repo/AGENTS.md", Name: "AGENTS.md", Source: "project", Content: "project rules here"},
			},
		},
	}

	if err := srv.handleInstructionsList(Request{ID: json.RawMessage(`1`)}); err != nil {
		t.Fatalf("handleInstructionsList: %v", err)
	}

	var resp struct {
		Result InstructionsListResult `json:"result"`
		Error  *ResponseError         `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (raw: %s)", err, buf.String())
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if len(resp.Result.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(resp.Result.Files))
	}

	first := resp.Result.Files[0]
	if first.Scope != "global" || first.Source != "user" {
		t.Errorf("first file scope/source = %q/%q, want global/user", first.Scope, first.Source)
	}
	if first.Content != "global rules" || first.Bytes != len("global rules") {
		t.Errorf("first file content/bytes mismatch: %q / %d", first.Content, first.Bytes)
	}

	second := resp.Result.Files[1]
	if second.Scope != "project" {
		t.Errorf("second file scope = %q, want project", second.Scope)
	}
	if second.Name != "AGENTS.md" {
		t.Errorf("second file name = %q, want AGENTS.md", second.Name)
	}
}

func TestHandleInstructionsListEmpty(t *testing.T) {
	var buf bytes.Buffer
	srv := &Server{out: &buf, rt: &runtime.Session{}}
	if err := srv.handleInstructionsList(Request{ID: json.RawMessage(`1`)}); err != nil {
		t.Fatalf("handleInstructionsList: %v", err)
	}
	var resp struct {
		Result InstructionsListResult `json:"result"`
	}
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Result.Files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(resp.Result.Files))
	}
	// The list is always a non-nil slice so it serializes to [] rather than null.
	if resp.Result.Files == nil {
		t.Errorf("expected non-nil empty slice")
	}
}
