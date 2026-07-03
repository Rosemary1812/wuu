package exec

import (
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/appserver"
)

func TestRenderApprovalPromptFull(t *testing.T) {
	got := renderApprovalPrompt(appserver.ToolApprovalRequest{
		ToolName:         "bash",
		Risk:             "high",
		ArgumentsPreview: "  cmd=rm -rf /tmp/foo  ",
		PolicyReason:     "writes outside the workspace",
	})
	want := "" +
		"\nwuu: approval required\n" +
		"  tool: bash (risk=high)\n" +
		"  args: cmd=rm -rf /tmp/foo\n" +
		"  reason: writes outside the workspace\n" +
		"approve? [y]es once / [a]lways this session / [N]o: "
	if got != want {
		t.Fatalf("renderApprovalPrompt(full) mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderApprovalPromptMinimal(t *testing.T) {
	got := renderApprovalPrompt(appserver.ToolApprovalRequest{
		ToolName: "bash",
	})
	want := "" +
		"\nwuu: approval required\n" +
		"  tool: bash\n" +
		"approve? [y]es once / [a]lways this session / [N]o: "
	if got != want {
		t.Fatalf("renderApprovalPrompt(minimal) mismatch\n got: %q\nwant: %q", got, want)
	}
	// Defensive: the minimal request must not leak lines from optional fields.
	if strings.Contains(got, "risk=") {
		t.Fatalf("minimal prompt should not mention risk: %q", got)
	}
	if strings.Contains(got, "args:") {
		t.Fatalf("minimal prompt should not include args line: %q", got)
	}
	if strings.Contains(got, "reason:") {
		t.Fatalf("minimal prompt should not include reason line: %q", got)
	}
}