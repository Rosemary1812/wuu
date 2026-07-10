package exec

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
)

// groupScriptWorker is a deterministic, request-driven provider for the named
// agents in the end-to-end group-chat regression. A single worker serves every
// named turn in the script; it decides which tool to emit purely from a marker
// woven into the turn prompt (POST_GROUP <id> / POST_REPLY <cth> / FORK <name>),
// and once a tool result is already present in the request it returns a final
// message to close the turn. This keeps the whole multi-turn sequence
// reproducible with no live API and no order-sensitive response queue.
type groupScriptWorker struct{ mu sync.Mutex }

func (w *groupScriptWorker) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Second step of a tool loop: the tool result is back, so wrap up.
	for _, m := range req.Messages {
		if strings.EqualFold(m.Role, "tool") {
			return providers.ChatResponse{Content: "Done."}, nil
		}
	}
	// First step: pick the tool from the instruction marker. Scan every message
	// so the choice does not depend on which role carries the prompt.
	var instruction string
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "POST_GROUP") ||
			strings.Contains(m.Content, "POST_REPLY") ||
			strings.Contains(m.Content, "FORK") {
			instruction = m.Content
		}
	}
	switch {
	case strings.Contains(instruction, "POST_GROUP"):
		target := tokenAfter(instruction, "POST_GROUP")
		return toolCallResponse("post_message", fmt.Sprintf(
			`{"kind":"result","text":"We shipped the fix.","thread_id":%q}`, target)), nil
	case strings.Contains(instruction, "POST_REPLY"):
		target := tokenAfter(instruction, "POST_REPLY")
		return toolCallResponse("post_message", fmt.Sprintf(
			`{"kind":"result","text":"Answering inside the reply.","thread_id":%q}`, target)), nil
	case strings.Contains(instruction, "FORK"):
		name := tokenAfter(instruction, "FORK")
		return toolCallResponse("manage_participant", fmt.Sprintf(
			`{"action":"fork","name":%q}`, name)), nil
	}
	return providers.ChatResponse{Content: "Nothing to do."}, nil
}

func toolCallResponse(name, args string) providers.ChatResponse {
	return providers.ChatResponse{ToolCalls: []providers.ToolCall{{
		ID:        "call_" + name,
		Name:      name,
		Arguments: args,
	}}}
}

// tokenAfter returns the first whitespace-delimited token following marker.
func tokenAfter(text, marker string) string {
	idx := strings.Index(text, marker)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(text[idx+len(marker):])
	if rest == "" {
		return ""
	}
	return strings.Fields(rest)[0]
}

// TestExecGroupChatEndToEndRegression is the CI-reproducible headless group-chat
// regression: it drives the complete named group-chat lifecycle through exec's
// --input-json action sequence against the REAL app server (over pipes, with a
// deterministic worker), asserting each documented behavior at every step:
//
//  1. build a group (create_group RPC)
//  2. pull named members Ada and Ben into it (add_group_member RPC)
//  3. a named member responds in the group (post_message named turn) — lands on
//     the main group stream
//  4. open a reply seeded with only Ada (open_reply RPC) — weak-isolation push
//     alerts the member subset, not the whole group
//  5. Ada answers inside that reply (post_message named turn, target cth) — the
//     message folds into the reply and stays OUT of the main stream
//  6. a human folds a question into the same reply (post_subthread RPC)
//  7. escalate the reply to a task with Ada as lead (escalate_task RPC) —
//     the lead is recorded: Ada leads the task cth, Ben does not. The lead
//     is what subthread.lead_participant_id exposes for named turns on the
//     task.
//  8. Ada forks a memory-backed copy of herself (manage_participant named turn)
func TestExecGroupChatEndToEndRegression(t *testing.T) {
	rt := newExecForkTestRuntime(t, providers.AdaptStreamClient(&groupScriptWorker{}))
	// The app server's resident routing and agent workers keep writing into the
	// temp tree on background goroutines after Run returns. Registered here (so it
	// runs LIFO before t.TempDir's own RemoveAll), this lets those writes settle
	// before the dirs are torn down, avoiding a "directory not empty" cleanup race.
	t.Cleanup(func() {
		waitForTreeQuiesce(rt.RootDir)
		waitForTreeQuiesce(rt.WuuHome)
	})
	// Ada gets an on-disk memory notebook so her fork starts from a real memory
	// snapshot; Ben is a plain named roster member.
	ada := seedMotherParticipant(t, rt, "ada")
	ben := seedGroupMember(t, rt, "ben")

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	controller := newLocalControllerForRuntime(ctx, rt)

	var stdout bytes.Buffer
	err := Run(ctx, Options{
		JSON:       true,
		Stdout:     &stdout,
		Controller: controller,
		Actions: []GroupAction{
			// 1. Build the group.
			{Action: "create_group", Params: map[string]any{"title": "Ship the fix"}, SaveAs: map[string]string{"group": "thread.id"}},
			// 2. Pull both named members into the group.
			{Action: "add_group_member", Params: map[string]any{"thread_id": "$group", "participant_id": ada.ID}},
			{Action: "add_group_member", Params: map[string]any{"thread_id": "$group", "participant_id": ben.ID}},
			// 3. A named member speaks in the group (main stream). A distinct
			// task_name per turn keeps each run's agent path unique for the same
			// named identity.
			{Action: "participant_turn", As: ada.ID, Params: map[string]any{
				"thread_id": "$group",
				"task_name": "ada_status",
				"prompt":    "The user asked for a status. Post a result. POST_GROUP $group",
			}, SaveAs: map[string]string{"anchor": "anchor_item_id"}},
			// 4. Open a reply seeded with only Ada (weak-isolation subset).
			{
				Action: "open_reply",
				Params: map[string]any{
					"thread_id":      "$group",
					"anchor_item_id": "$anchor",
				},
				SaveAs: map[string]string{"cth": "subthread.id"},
			},
			// 5. Ada answers inside the reply (folds into cth, not main stream).
			{Action: "participant_turn", As: ada.ID, Params: map[string]any{
				"thread_id": "$group",
				"task_name": "ada_reply",
				"prompt":    "Answer the reviewer inside the reply thread. POST_REPLY $cth",
			}},
			// 6. A human folds a question into the same reply.
			{
				Action: "post_subthread",
				Params: map[string]any{"thread_id": "$group", "subthread_id": "$cth", "text": "what about the retry path?"},
			},
			// 7. Escalate the reply to a task with Ada as lead.
			{
				Action: "escalate_task",
				Params: map[string]any{"thread_id": "$group", "subthread_id": "$cth", "title": "Ship the retry fix"},
				Expect: map[string]any{"subthread.lead_participant_id": ada.ID, "subthread.status": "task"},
			},
			// 8. Ada forks a memory-backed copy of herself.
			{Action: "participant_turn", As: ada.ID, Params: map[string]any{
				"thread_id": "$group",
				"task_name": "ada_fork",
				"prompt":    "You are short-handed on the task. Fork a copy of yourself. FORK ada",
			}},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, stdout.String())
	}

	events := parseJSONLines(t, stdout.String())

	// Resolve the runtime ids the script threaded through save_as.
	groupID := actionResultPath(t, events, "create_group", "thread", "id")
	cthID := actionResultPath(t, events, "open_reply", "subthread", "id")
	if !strings.HasPrefix(cthID, "cth-") {
		t.Fatalf("open_reply did not return a cth id: %q", cthID)
	}

	// The whole sequence completed (one result event, status completed).
	final := events[len(events)-1]
	if final["type"] != "result" || final["status"] != "completed" {
		t.Fatalf("sequence did not complete cleanly: %+v\n%s", final, stdout.String())
	}

	// Step 4: weak-isolation push — the reply was seeded with only Ada, so Ben is
	// not part of the alerted subset.
	openEvent := actionCompletedEvent(t, events, "open_reply")
	sub := openEvent["result"].(map[string]any)["subthread"].(map[string]any)
	parts, _ := sub["participants"].([]any)
	if !containsAny(parts, ada.ID) {
		t.Fatalf("reply subset should include Ada %q: %+v", ada.ID, sub["participants"])
	}
	if containsAny(parts, ben.ID) {
		t.Fatalf("reply subset should NOT include Ben %q (weak-isolation push subset): %+v", ben.ID, sub["participants"])
	}

	// Steps 3 & 5: both named posts are persisted synchronously by the post_message
	// tool, so history is deterministic once the turns reach terminal. Ada's group
	// post lands on the MAIN stream (untagged); her reply answer FOLDS into the cth
	// (tagged thread_id=cth) and never touches the main stream.
	history, err := session.LoadHistoryRecords(rt.SessionDir, groupID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	// A main-stream post has no subthread tag; a folded reply post is tagged with
	// the cth. The load-bearing weak-isolation distinction is that the group post
	// is NOT tagged with the cth and the reply posts ARE.
	var sawGroupPost, sawReplyFold bool
	for i := range history {
		tag := strings.TrimSpace(history[i].ThreadID)
		switch strings.TrimSpace(history[i].Content) {
		case "We shipped the fix.":
			if tag == cthID {
				t.Fatalf("group post should be on the main stream, but it folded into cth %q", cthID)
			}
			if tag != "" {
				t.Fatalf("group post should have no subthread tag, got thread_id=%q", tag)
			}
			sawGroupPost = true
		case "Answering inside the reply.":
			if tag != cthID {
				t.Fatalf("reply answer should fold into cth %q, got thread_id=%q", cthID, tag)
			}
			sawReplyFold = true
		case "what about the retry path?":
			if tag != cthID {
				t.Fatalf("human reply post should fold into cth %q, got thread_id=%q", cthID, tag)
			}
		}
	}
	if !sawGroupPost {
		t.Fatalf("named group post not found on main stream: %+v", historyContents(history))
	}
	if !sawReplyFold {
		t.Fatalf("named reply answer not folded into cth: %+v", historyContents(history))
	}

	// Exec observed the named agent invoke post_message on its JSONL tool stream
	// for both the group post and the reply answer.
	if got := countToolCompleted(events, "post_message"); got < 2 {
		t.Fatalf("expected >=2 post_message tool calls (group post + reply answer), got %d\n%s", got, stdout.String())
	}
	if got := countToolCompleted(events, "manage_participant"); got < 1 {
		t.Fatalf("expected the fork turn to invoke manage_participant, got %d\n%s", got, stdout.String())
	}

	// Step 7: task lead ownership. The escalate recorded Ada as the lead of
	// the task cth; Ben leads nothing. The recorded lead is the durable
	// fact named turns on the task can inspect via
	// subthread.lead_participant_id to decide who owns the work.
	adaLeads, err := session.LeadTaskThreads(rt.SessionDir, ada.ID)
	if err != nil {
		t.Fatalf("LeadTaskThreads(ada): %v", err)
	}
	if len(adaLeads) != 1 || adaLeads[0].ID != cthID {
		t.Fatalf("Ada should lead exactly the escalated task cth %q, got %+v", cthID, adaLeads)
	}
	if adaLeads[0].Status != session.ConversationThreadTask {
		t.Fatalf("escalated cth should be a task, got status %q", adaLeads[0].Status)
	}
	benLeads, err := session.LeadTaskThreads(rt.SessionDir, ben.ID)
	if err != nil {
		t.Fatalf("LeadTaskThreads(ben): %v", err)
	}
	if len(benLeads) != 0 {
		t.Fatalf("Ben leads no task and must not be authorized to orchestrate, got %+v", benLeads)
	}

	// Step 8: Ada's fork tool call produced a memory-backed copy (ForkedFrom Ada).
	fork := waitForForkOf(t, rt.SessionDir, ada.ID)
	if fork.Kind != participant.KindNamed {
		t.Fatalf("fork kind = %s, want named", fork.Kind)
	}
	if !strings.Contains(fork.Name, "ada") {
		t.Fatalf("fork should be named after its mother, got %q", fork.Name)
	}
}

// seedGroupMember registers a plain active named agent (no notebook) as a roster
// member available to be pulled into a group.
func seedGroupMember(t *testing.T, rt *runtime.Session, name string) participant.Participant {
	t.Helper()
	id := participant.NewID()
	p := participant.Participant{
		ID:        id,
		Kind:      participant.KindNamed,
		Name:      name,
		Role:      "engineer",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := session.UpsertParticipant(rt.SessionDir, p); err != nil {
		t.Fatalf("seed group member %q: %v", name, err)
	}
	return p
}

// waitForTreeQuiesce blocks until the file tree under path stops changing (no
// new/removed/modified files for a short settle window), capped by a deadline.
// It exists to let the app server's background goroutines finish writing before
// the test's temp dirs are torn down.
func waitForTreeQuiesce(path string) {
	deadline := time.Now().Add(5 * time.Second)
	const settle = 250 * time.Millisecond
	last := ""
	var stableSince time.Time
	for time.Now().Before(deadline) {
		sig := treeSignature(path)
		if sig == last {
			if stableSince.IsZero() {
				stableSince = time.Now()
			} else if time.Since(stableSince) >= settle {
				return
			}
		} else {
			last = sig
			stableSince = time.Time{}
		}
		time.Sleep(30 * time.Millisecond)
	}
}

// treeSignature summarizes a directory tree as entry count, total size, and the
// latest modification time so waitForTreeQuiesce can detect ongoing writes.
func treeSignature(path string) string {
	var count, size int64
	var latest int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		count++
		if info, statErr := d.Info(); statErr == nil {
			size += info.Size()
			if ms := info.ModTime().UnixNano(); ms > latest {
				latest = ms
			}
		}
		return nil
	})
	return fmt.Sprintf("%d:%d:%d", count, size, latest)
}

// countToolCompleted counts tool_completed JSONL events for a given tool name.
func countToolCompleted(events []map[string]any, name string) int {
	n := 0
	for _, event := range events {
		if event["type"] == "tool_completed" && event["name"] == name {
			n++
		}
	}
	return n
}

func historyContents(records []session.HistoryRecord) []string {
	out := make([]string, 0, len(records))
	for i := range records {
		out = append(out, records[i].Role+":"+strings.TrimSpace(records[i].Content))
	}
	return out
}
