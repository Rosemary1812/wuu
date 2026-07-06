package tools

import (
	"strings"
	"testing"
)

// TestSendMessageToolDescribesQueueOrResume locks the queue-or-resume contract
// into the merged send_message description: a running target queues the message
// for its next model round, and a finished target is revived with full context
// plus the new message. followup_task folded into this tool via trigger_turn.
func TestSendMessageToolDescribesQueueOrResume(t *testing.T) {
	desc := NewSendAgentMessageTool(nil).Definition().Description
	lower := strings.ToLower(desc)
	if !strings.Contains(lower, "queue") {
		t.Errorf("send_message description should explain running->queued delivery: %q", desc)
	}
	if !strings.Contains(lower, "resume") && !strings.Contains(lower, "revive") {
		t.Errorf("send_message description should explain terminal->resume/revive: %q", desc)
	}
	if !strings.Contains(lower, "interim") {
		t.Errorf("send_message description should note the interim-note mode: %q", desc)
	}
	if !strings.Contains(lower, "trigger_turn") {
		t.Errorf("send_message description should document the trigger_turn hand-off mode: %q", desc)
	}
}
