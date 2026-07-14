package exec

import (
	"bytes"
	"context"
	"testing"

	"github.com/blueberrycongee/wuu/internal/appserver"
)

func TestReproVerifyFix(t *testing.T) {
	controller := newFakeController(
		notification(appserver.NotificationTurnError, appserver.TurnErrorNotification{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Error:    "context canceled",
			Turn:     appserver.Turn{ID: "turn-1", Status: appserver.TurnStatusFailed},
		}),
	)
	var stdout bytes.Buffer

	err := Run(context.Background(), Options{
		Prompt:     "do work",
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
	})
	t.Logf("Run returned err=%v, exit code=%d", err, ExitCode(err))
	t.Logf("Events:\n%s", stdout.String())

	if ExitCode(err) != ExitInterrupted {
		t.Fatalf("expected ExitInterrupted, got %d", ExitCode(err))
	}

	events := parseJSONLines(t, stdout.String())
	last := events[len(events)-1]
	if last["type"] != "result" {
		t.Fatalf("last event type=%v, want result", last["type"])
	}
	if last["status"] != "interrupted" {
		t.Fatalf("last result.status=%v, want interrupted (this is the user-reported bug)", last["status"])
	}
	for _, ev := range events {
		if ev["type"] == "turn_failed" {
			t.Fatalf("turn_failed in events - this is the user-reported bug: %+v", ev)
		}
	}
}
