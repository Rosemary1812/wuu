package tools

import (
	"strings"
	"testing"
)

// TestAgentMessageToolsDescribeQueueOrResume locks the shared queue-or-resume
// contract into both tool descriptions: a running target queues the message
// for its next model round, and a finished target is revived with full
// context plus the new message.
func TestAgentMessageToolsDescribeQueueOrResume(t *testing.T) {
	descriptions := map[string]string{
		"send_message":  NewSendAgentMessageTool(nil).Definition().Description,
		"followup_task": NewFollowupTaskTool(nil).Definition().Description,
	}
	for name, desc := range descriptions {
		lower := strings.ToLower(desc)
		if !strings.Contains(lower, "queue") {
			t.Errorf("%s description should explain running->queued delivery: %q", name, desc)
		}
		if !strings.Contains(lower, "resume") && !strings.Contains(lower, "revive") {
			t.Errorf("%s description should explain terminal->resume/revive: %q", name, desc)
		}
		if !strings.Contains(lower, "model round") {
			t.Errorf("%s description should state queued messages ride the next model round: %q", name, desc)
		}
	}
}
