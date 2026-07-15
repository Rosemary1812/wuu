package tools

import (
	"strings"
	"testing"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestLatestUserGoalFromHistorySkipsProcessCompletion(t *testing.T) {
	history := []providers.ChatMessage{
		{Role: "user", Content: "build the project"},
		{Role: "assistant", Content: "started it in the background"},
		{Role: "user", Name: wuucontext.ProcessNotificationMessageName, Content: `<process_notification>{"process_id":"proc-1"}</process_notification>`},
	}
	if got := latestUserGoalFromHistory(history); got != "build the project" {
		t.Fatalf("latest user goal = %q, want original user directive", got)
	}
}

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
