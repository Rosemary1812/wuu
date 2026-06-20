package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestPermissionBoundaryReadOnlyBlocksWorkspaceWrites(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetPermissionBoundary(PermissionBoundaryForProfile(PermissionProfileReadOnly))

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "write_file",
		Arguments: `{"path":"notes.txt","content":"blocked"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=permission_boundary_denied") {
		t.Fatalf("expected permission boundary error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "notes.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("read-only boundary should prevent file creation, stat err=%v", statErr)
	}
	records := kit.ToolTelemetry()
	if len(records) != 1 || records[0].ErrorKind != "permission_boundary_denied" || records[0].PolicyAction != ToolPolicyDeny {
		t.Fatalf("unexpected telemetry: %+v", records)
	}
}

func TestPermissionBoundaryReadOnlyAllowsObservationAndInternalPlanning(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetPermissionBoundary(PermissionBoundaryForProfile(PermissionProfileReadOnly))

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "read_file",
		Arguments: `{"path":"notes.txt"}`,
	}); err != nil {
		t.Fatalf("read_file should be allowed: %v", err)
	}
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "update_plan",
		Arguments: `{"plan":[{"step":"inspect","status":"in_progress"}]}`,
	}); err != nil {
		t.Fatalf("update_plan should be allowed as internal planning state: %v", err)
	}
}

func TestPermissionBoundaryReadOnlyUsesInputClassification(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetPermissionBoundary(PermissionBoundaryForProfile(PermissionProfileReadOnly))

	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"pwd"}`,
	}); err != nil {
		t.Fatalf("read-only shell command should be allowed: %v", err)
	}
	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"go test ./..."}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=permission_boundary_denied") {
		t.Fatalf("expected read-only boundary to block verification process, got %v", err)
	}
}

func TestPermissionBoundaryReadOnlyBlocksBashWrites(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetPermissionBoundary(PermissionBoundaryForProfile(PermissionProfileReadOnly))

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "bash",
		Arguments: `{"command":"printf blocked > notes.txt"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=permission_boundary_denied") {
		t.Fatalf("expected read-only boundary to block bash write, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "notes.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("read-only boundary should prevent bash file creation, stat err=%v", statErr)
	}
}

func TestPermissionBoundaryWorkspaceWriteBlocksDestructiveActions(t *testing.T) {
	root := t.TempDir()
	kit, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	kit.SetPermissionBoundary(PermissionBoundaryForProfile(PermissionProfileWorkspaceWrite))

	_, err = kit.Execute(context.Background(), providers.ToolCall{
		Name:      "run_shell",
		Arguments: `{"command":"rm -rf build"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "error_kind=permission_boundary_denied") {
		t.Fatalf("expected workspace_write boundary to block destructive shell, got %v", err)
	}
}
