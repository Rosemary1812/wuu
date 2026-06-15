package agentcontrol

import (
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/harness"
)

func TestAwaitAgentsNextStepsDoNotMentionWorkflowForPlainResults(t *testing.T) {
	result := AwaitAgentsResult{
		Results: []AwaitAgentResult{{Status: string(harness.TaskStatusCompleted)}},
	}
	steps := strings.Join(awaitAgentsNextSteps(result), "\n")
	if strings.Contains(steps, "workflow_control") || strings.Contains(steps, "Workflow Run") {
		t.Fatalf("plain await next steps should not mention workflow binding:\n%s", steps)
	}
	if !strings.Contains(steps, "agent reports") {
		t.Fatalf("plain await next steps should still guide synthesis:\n%s", steps)
	}
}
