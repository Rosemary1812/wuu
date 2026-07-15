package appserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/process"
)

func TestProcessCompletionChatMessageIncludesOutputTail(t *testing.T) {
	root := t.TempDir()
	manager, err := process.NewManager(root, filepath.Join(root, "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan process.Event, 8)
	manager.Subscribe(events)
	started, err := manager.Start(context.Background(), process.StartOptions{
		Command:   "printf 'hello-tail\\n'",
		OwnerKind: process.OwnerMainAgent,
		OwnerID:   "thread-1",
		Lifecycle: process.LifecycleManaged,
	})
	if err != nil {
		t.Fatal(err)
	}

	var terminal process.Event
	deadline := time.After(5 * time.Second)
	for terminal.Process.ID == "" {
		select {
		case event := <-events:
			if event.Process.ID == started.ID && event.Cause == process.EventCauseNaturalExit {
				terminal = event
			}
		case <-deadline:
			t.Fatal("timed out waiting for natural process exit")
		}
	}

	msg := processCompletionChatMessage(manager, terminal)
	if msg.Role != "user" || msg.Name != wuucontext.ProcessNotificationMessageName {
		t.Fatalf("unexpected process completion message metadata: %+v", msg)
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(msg.Content, "<process_notification>"), "</process_notification>")
	var payload processCompletionPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode process payload: %v\n%s", err, msg.Content)
	}
	if payload.ProcessID != started.ID || payload.Status != process.StatusStopped || payload.ExitCode != 0 {
		t.Fatalf("unexpected terminal payload: %+v", payload)
	}
	if !strings.Contains(payload.OutputTail, "hello-tail") || !strings.Contains(payload.Instruction, "do not poll") {
		t.Fatalf("completion payload omitted output or guidance: %+v", payload)
	}
}

func TestForwardProcessNotificationsQueuesOnlyNaturalOwnerExit(t *testing.T) {
	threadID := "thread-owner"
	thread := &threadState{ID: threadID, running: true}
	server := &Server{
		threads:                      map[string]*threadState{threadID: thread},
		pendingAgentCompletionTurns:  make(map[string][]agentCompletionTurn),
		drainingAgentCompletionTurns: make(map[string]bool),
	}
	events := make(chan process.Event, 4)
	done := make(chan struct{})
	forwardDone := make(chan struct{})
	go func() {
		server.forwardProcessNotifications(threadID, nil, nil, events, done)
		close(forwardDone)
	}()

	events <- process.Event{
		Type:  process.EventStopped,
		Cause: process.EventCauseRequestedStop,
		Process: process.Process{
			ID: "proc-requested", OwnerKind: process.OwnerMainAgent, OwnerID: threadID, Status: process.StatusStopped,
		},
	}
	events <- process.Event{
		Type:  process.EventStopped,
		Cause: process.EventCauseNaturalExit,
		Process: process.Process{
			ID: "proc-other", OwnerKind: process.OwnerMainAgent, OwnerID: "other-thread", Status: process.StatusStopped,
		},
	}
	events <- process.Event{
		Type:  process.EventStopped,
		Cause: process.EventCauseNaturalExit,
		Process: process.Process{
			ID: "proc-owned", OwnerKind: process.OwnerMainAgent, OwnerID: threadID, Status: process.StatusStopped,
		},
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		server.agentCompletionMu.Lock()
		pending := cloneAgentCompletionTurns(server.pendingAgentCompletionTurns[threadID])
		server.agentCompletionMu.Unlock()
		if len(pending) == 1 {
			if pending[0].agentID != "proc-owned" || pending[0].msg.Name != wuucontext.ProcessNotificationMessageName {
				t.Fatalf("unexpected queued completion: %+v", pending[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("natural owner exit was not queued; pending=%+v", pending)
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(done)
	<-forwardDone
	server.backgroundWG.Wait()
}
