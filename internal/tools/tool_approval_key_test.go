package tools

import (
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

// TestToolApprovalKeyIgnoresNonBindingCommandFields pins the stable-key
// contract behind session approvals and exec --approve grants: retrying
// the same command with a rephrased purpose or a different timeout must
// not mint a new approval key.
func TestToolApprovalKeyIgnoresNonBindingCommandFields(t *testing.T) {
	base := toolApprovalKey(providers.ToolCall{
		Name:      "bash",
		Arguments: `{"action":"run","command":"go vet ./...","purpose":"first try"}`,
	})
	rephrased := toolApprovalKey(providers.ToolCall{
		Name:      "bash",
		Arguments: `{"action":"run","command":"go vet ./...","purpose":"retry now that it is approved","timeout_seconds":600}`,
	})
	if base != rephrased {
		t.Fatalf("same command should share an approval key: %q vs %q", base, rephrased)
	}

	otherCommand := toolApprovalKey(providers.ToolCall{
		Name:      "bash",
		Arguments: `{"action":"run","command":"go vet ./internal/...","purpose":"first try"}`,
	})
	if base == otherCommand {
		t.Fatal("different commands must not share an approval key")
	}

	otherAction := toolApprovalKey(providers.ToolCall{
		Name:      "bash",
		Arguments: `{"action":"kill","command":"go vet ./...","purpose":"first try"}`,
	})
	if base == otherAction {
		t.Fatal("different actions must not share an approval key")
	}

	// Non-command tools keep hashing their full arguments.
	write1 := toolApprovalKey(providers.ToolCall{Name: "write_file", Arguments: `{"path":"a.txt","content":"x"}`})
	write2 := toolApprovalKey(providers.ToolCall{Name: "write_file", Arguments: `{"path":"a.txt","content":"y"}`})
	if write1 == write2 {
		t.Fatal("non-command tools must still key on full arguments")
	}
}
