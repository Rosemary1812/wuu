package workflow

import (
	"strings"
	"testing"
)

func TestProcessBodySupportsWuuAndLegacyVariables(t *testing.T) {
	got := ProcessBody(
		"Args=${ARGUMENTS}\nWuuDir=${WUU_WORKFLOW_DIR}\nWuuSession=${WUU_SESSION_ID}\nLegacyDir=${CLAUDE_WORKFLOW_DIR}\nLegacySession=${CLAUDE_SESSION_ID}",
		ProcessOptions{Arguments: "ship settings", WorkflowDir: "/repo/.wuu/workflows/release", SessionID: "session-1"},
	)
	for _, want := range []string{
		"Args=ship settings",
		"WuuDir=/repo/.wuu/workflows/release",
		"WuuSession=session-1",
		"LegacyDir=/repo/.wuu/workflows/release",
		"LegacySession=session-1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("processed workflow body missing %q:\n%s", want, got)
		}
	}
}
