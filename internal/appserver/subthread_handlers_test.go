package appserver

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

// openSub/listSub/resolveSub must work on a group thread: opening a reply is
// create-or-find by anchor, so the same anchor never spawns a second subthread.
func TestOpenConversationSubthreadOnGroupThreadCreatesThenFinds(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	groupID := startGroupThreadForTest(t, srv)

	first, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: "seq-3",
		Title:        "reply",
		CreatedBy:    "user",
	})
	if err != nil {
		t.Fatalf("openConversationSubthread (create): %v", err)
	}
	if !strings.HasPrefix(first.ID, "cth-") {
		t.Fatalf("expected a cth-* subthread id, got %q", first.ID)
	}
	if first.SessionID != groupID {
		t.Fatalf("subthread parent = %q, want group %q", first.SessionID, groupID)
	}

	// Same anchor must resolve to the existing subthread, not create a new one.
	again, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: "seq-3",
	})
	if err != nil {
		t.Fatalf("openConversationSubthread (find): %v", err)
	}
	if again.ID != first.ID {
		t.Fatalf("create-or-find broke: anchor %q produced %q then %q", "seq-3", first.ID, again.ID)
	}

	threads, err := session.ListConversationThreads(srv.rt.SessionDir, groupID)
	if err != nil {
		t.Fatalf("ListConversationThreads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("expected exactly one subthread for the anchor, got %d", len(threads))
	}
}

// One layer, no nesting: a message that already lives inside a reply subthread
// cannot itself anchor another reply. The backend refuses even when handed the
// cth-scoped anchor id directly.
func TestOpenConversationSubthreadRejectsNestedReply(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Iris", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(srv.rt.SessionDir, groupID, participantID); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}

	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: "seq-3",
		CreatedBy:    participantID,
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}

	// Fold a message into the subthread so it renders a cth-scoped item.
	kit := residentToolkitForTest(t, srv, dmID)
	if _, err := kit.Execute(context.Background(), providers.ToolCall{
		Name:      "post_message",
		Arguments: fmt.Sprintf(`{"kind":"update","text":"looking into it","thread_id":%q}`, cth.ID),
	}); err != nil {
		t.Fatalf("post_message into subthread: %v", err)
	}

	view, err := srv.conversationSubthreadView(groupID, cth, true)
	if err != nil {
		t.Fatalf("conversationSubthreadView: %v", err)
	}
	var nestedAnchor string
	for _, turn := range view.Turns {
		for _, item := range turn.Items {
			if item.ID != "" {
				nestedAnchor = item.ID
				break
			}
		}
		if nestedAnchor != "" {
			break
		}
	}
	if nestedAnchor == "" {
		t.Fatalf("subthread rendered no anchorable item: %+v", view.Turns)
	}

	if _, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: nestedAnchor,
		CreatedBy:    participantID,
	}); err == nil || !strings.Contains(err.Error(), "already inside reply thread") {
		t.Fatalf("opening a reply on a subthread message should be rejected, got %v", err)
	}

	// The rejection must not have created a stray subthread for the nested anchor.
	threads, err := session.ListConversationThreads(srv.rt.SessionDir, groupID)
	if err != nil {
		t.Fatalf("ListConversationThreads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("nested-reply rejection leaked a subthread: have %d, want 1", len(threads))
	}
}

// Escalating a reply to a task advances the subthread from the discussion state
// (open) to the execution state (task) and hangs a task_card off it. The task
// stays bound to the same cth (no new thread spawned).
func TestEscalateConversationSubthreadAttachesTaskCard(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	groupID := startGroupThreadForTest(t, srv)

	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: "seq-3",
		CreatedBy:    "user",
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}

	raw := fmt.Sprintf(`{"id":"esc","method":"thread/escalateSub","params":{"thread_id":%q,"subthread_id":%q,"title":"Ship the fix","created_by":"user"}}`, groupID, cth.ID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/escalateSub: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "esc")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/escalateSub returned error: %v", errMsg)
	}
	view := remarshal[ThreadEscalateSubResult](t, resp["result"]).Subthread
	if view.Status != string(session.ConversationThreadTask) {
		t.Fatalf("subthread status = %q, want task", view.Status)
	}
	if view.Task == nil {
		t.Fatalf("escalated subthread must carry a task_card: %+v", view)
	}
	if view.Task.SubthreadID != cth.ID {
		t.Fatalf("task_card subthread_id = %q, want %q (task must bind the same cth)", view.Task.SubthreadID, cth.ID)
	}
	if view.Task.Status != "running" {
		t.Fatalf("task_card status = %q, want running", view.Task.Status)
	}
	if view.Task.Name != "Ship the fix" {
		t.Fatalf("task_card name = %q, want Ship the fix", view.Task.Name)
	}
	// A human-only escalation (created_by="user", no picked lead) leaves the task
	// with no lead — "user" is not a named agent, so no orchestration authority is
	// granted until a named lead is picked.
	if view.LeadParticipantID != "" {
		t.Fatalf("lead_participant_id = %q, want empty for a human-only escalation", view.LeadParticipantID)
	}
}

// Escalation grants task-lead authority to the picked named member. A picked lead
// that is not an active named participant is rejected; when no lead is picked the
// backend falls back to the reply's parent message author (T3), NOT whoever
// clicked escalate — so a named created_by no longer reassigns the lead.
func TestEscalateConversationSubthreadRecordsPickedLead(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	// Escalation now wakes the picked lead for planning; drain that resident
	// turn before the temp dir is torn down.
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	leadID := saveNamedParticipant(t, srv.rt, "Iris", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)

	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: "seq-3",
		CreatedBy:    "user",
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}

	// A picked named lead is recorded even though the escalator (created_by) is the
	// human.
	raw := fmt.Sprintf(`{"id":"esc","method":"thread/escalateSub","params":{"thread_id":%q,"subthread_id":%q,"title":"Ship it","created_by":"user","lead_participant_id":%q}}`, groupID, cth.ID, leadID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/escalateSub: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "esc")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/escalateSub returned error: %v", errMsg)
	}
	view := remarshal[ThreadEscalateSubResult](t, resp["result"]).Subthread
	if view.LeadParticipantID != leadID {
		t.Fatalf("lead_participant_id = %q, want picked lead %q", view.LeadParticipantID, leadID)
	}
	if view.EscalatedBy != "user" {
		t.Fatalf("escalated_by = %q, want human provenance user (kept distinct from lead)", view.EscalatedBy)
	}

	// A picked lead that is not an active named participant is rejected.
	raw = fmt.Sprintf(`{"id":"bad","method":"thread/escalateSub","params":{"thread_id":%q,"subthread_id":%q,"lead_participant_id":"prt-ghost"}}`, groupID, cth.ID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/escalateSub (bad lead): %v", err)
	}
	resp = responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "bad")
	if _, ok := resp["error"]; !ok {
		t.Fatalf("escalating with a non-named lead must error, got %+v", resp)
	}

	// When no lead is picked, created_by is NO LONGER the fallback (T3): the lead
	// defaults to the reply's parent message author, not whoever clicked escalate.
	// This cth has no bound parent author (synthetic anchor), so a leadless
	// re-escalation naming a named created_by leaves the previously-recorded lead
	// in place rather than reassigning it to created_by.
	otherNamed := saveNamedParticipant(t, srv.rt, "Milo", "worker", "")
	raw = fmt.Sprintf(`{"id":"fb","method":"thread/escalateSub","params":{"thread_id":%q,"subthread_id":%q,"created_by":%q}}`, groupID, cth.ID, otherNamed)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/escalateSub (no picked lead): %v", err)
	}
	resp = responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "fb")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/escalateSub (no picked lead) returned error: %v", errMsg)
	}
	view = remarshal[ThreadEscalateSubResult](t, resp["result"]).Subthread
	if view.LeadParticipantID != leadID {
		t.Fatalf("lead_participant_id = %q, want the prior picked lead %q kept — created_by is no longer the fallback (T3: parent author is)", view.LeadParticipantID, leadID)
	}
}

// Bubbling wraps the task up: it posts the one-line conclusion to the main group
// stream (full-roster visible) as a participant_message and marks the cth
// resolved with that summary (task_card -> resolved + summary).
func TestBubbleConversationSubthreadPostsToMainStreamAndResolves(t *testing.T) {
	srv, rt := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Iris", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(srv.rt.SessionDir, groupID, participantID); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}

	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: "seq-3",
		CreatedBy:    participantID,
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}
	if _, err := session.EscalateConversationThread(srv.rt.SessionDir, cth.ID, participantID, participantID, "Ship the fix"); err != nil {
		t.Fatalf("EscalateConversationThread: %v", err)
	}

	group := srv.thread(groupID)
	group.mu.Lock()
	turnsBefore := len(group.Turns)
	group.mu.Unlock()

	raw := fmt.Sprintf(`{"id":"bub","method":"thread/bubbleSub","params":{"thread_id":%q,"subthread_id":%q,"summary":"Fixed and deployed.","participant_id":%q}}`, groupID, cth.ID, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/bubbleSub: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "bub")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/bubbleSub returned error: %v", errMsg)
	}
	view := remarshal[ThreadBubbleSubResult](t, resp["result"]).Subthread
	if view.Status != string(session.ConversationThreadResolved) {
		t.Fatalf("subthread status = %q, want resolved", view.Status)
	}
	if view.Summary != "Fixed and deployed." {
		t.Fatalf("subthread summary = %q, want the bubbled line", view.Summary)
	}
	if view.Task == nil || view.Task.Status != "completed" || view.Task.Description != "Fixed and deployed." {
		t.Fatalf("resolved task_card must show completed + summary: %+v", view.Task)
	}

	// The conclusion must land in the main group stream as a visible participant
	// item (ThreadID empty), not folded back into the cth.
	group.mu.Lock()
	turnsAfter := len(group.Turns)
	lastItem := group.Turns[len(group.Turns)-1].Items[len(group.Turns[len(group.Turns)-1].Items)-1]
	group.mu.Unlock()
	if turnsAfter == turnsBefore {
		t.Fatalf("bubble did not append to the main stream: turns %d -> %d", turnsBefore, turnsAfter)
	}
	if lastItem.Type != ThreadItemParticipantMsg {
		t.Fatalf("bubbled main-stream item type = %q, want participant message", lastItem.Type)
	}

	history, err := session.LoadHistoryRecords(rt.sessionDir, groupID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	var bubbled *session.HistoryRecord
	for i := range history {
		if history[i].Role == "participant" && strings.TrimSpace(history[i].ThreadID) == "" && history[i].Content == "Fixed and deployed." {
			bubbled = &history[i]
			break
		}
	}
	if bubbled == nil {
		t.Fatalf("bubbled conclusion not persisted on the main stream: %+v", history)
	}
	if bubbled.ParticipantID != participantID {
		t.Fatalf("bubbled message author = %q, want lead %q", bubbled.ParticipantID, participantID)
	}
}

// A human posting into a reply subthread from the split panel folds the message
// into the cth (thread_id-tagged) — it appears in the subthread stream and does
// NOT touch the main group stream (weak isolation). The message is attributed to
// the stable "human" identity, and the refreshed view carries it back so the
// panel updates immediately.
func TestMessagePostSubthreadFoldsIntoReplyNotMainStream(t *testing.T) {
	srv, rt := newResidentSpeechTestServer(t)
	groupID := startGroupThreadForTest(t, srv)

	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: "seq-3",
		CreatedBy:    "user",
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}

	group := srv.thread(groupID)
	group.mu.Lock()
	turnsBefore := len(group.Turns)
	group.mu.Unlock()

	raw := fmt.Sprintf(`{"id":"post","method":"message/postSubthread","params":{"thread_id":%q,"subthread_id":%q,"text":"what about the retry path?"}}`, groupID, cth.ID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("message/postSubthread: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "post")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("message/postSubthread returned error: %v", errMsg)
	}
	view := remarshal[MessagePostSubthreadResult](t, resp["result"]).Subthread
	if view.ReplyCount != 1 {
		t.Fatalf("subthread reply_count = %d, want 1 after the human post", view.ReplyCount)
	}
	var foundText bool
	for _, turn := range view.Turns {
		for _, item := range turn.Items {
			if strings.Contains(item.Text, "what about the retry path?") {
				foundText = true
			}
		}
	}
	if !foundText {
		t.Fatalf("human post not rendered in the subthread stream: %+v", view.Turns)
	}

	// The message must NOT have appended to the main group stream (weak isolation:
	// stored tagged cth, kept out of the broadcast turns).
	group.mu.Lock()
	turnsAfter := len(group.Turns)
	group.mu.Unlock()
	if turnsAfter != turnsBefore {
		t.Fatalf("human reply post leaked into the main stream: turns %d -> %d", turnsBefore, turnsAfter)
	}

	// The persisted record is tagged with the cth thread_id and attributed to the
	// stable human identity.
	history, err := session.LoadHistoryRecords(rt.sessionDir, groupID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	var posted *session.HistoryRecord
	for i := range history {
		if strings.TrimSpace(history[i].ThreadID) == cth.ID && history[i].Content == "what about the retry path?" {
			posted = &history[i]
			break
		}
	}
	if posted == nil {
		t.Fatalf("human post not persisted tagged with cth %q: %+v", cth.ID, history)
	}
	if posted.ParticipantID != humanReactionParticipantID {
		t.Fatalf("human post author = %q, want %q", posted.ParticipantID, humanReactionParticipantID)
	}
}

// The reused full composer can attach screenshots/PDFs to a cth post; the
// attachments ride through message/postSubthread, persist tagged with the cth,
// and render back on the participant message item so the split panel shows them.
func TestMessagePostSubthreadCarriesAttachments(t *testing.T) {
	srv, rt := newResidentSpeechTestServer(t)
	groupID := startGroupThreadForTest(t, srv)

	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: "seq-3",
		CreatedBy:    "user",
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}

	// Attachments-only (no caption) must be accepted — a screenshot with no text.
	raw := fmt.Sprintf(`{"id":"post","method":"message/postSubthread","params":{"thread_id":%q,"subthread_id":%q,"text":"","images":[{"media_type":"image/png","data":"AAAB"}],"files":[{"media_type":"application/pdf","data":"JVBER","filename":"spec.pdf"}]}}`, groupID, cth.ID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("message/postSubthread: %v", err)
	}
	resp := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "post")
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("attachments-only post returned error: %v", errMsg)
	}
	view := remarshal[MessagePostSubthreadResult](t, resp["result"]).Subthread

	var img *ThreadItemImage
	var file *ThreadItemFile
	for _, turn := range view.Turns {
		for i := range turn.Items {
			if len(turn.Items[i].Images) > 0 {
				img = &turn.Items[i].Images[0]
			}
			if len(turn.Items[i].Files) > 0 {
				file = &turn.Items[i].Files[0]
			}
		}
	}
	if img == nil || img.Data != "AAAB" || img.MediaType != "image/png" {
		t.Fatalf("cth post image not rendered on the item: %+v", view.Turns)
	}
	if file == nil || file.Data != "JVBER" || file.Filename != "spec.pdf" {
		t.Fatalf("cth post file not rendered on the item: %+v", view.Turns)
	}

	// The attachments must persist on the cth-tagged history record.
	history, err := session.LoadHistoryRecords(rt.sessionDir, groupID, false)
	if err != nil {
		t.Fatalf("LoadHistoryRecords: %v", err)
	}
	var posted *session.HistoryRecord
	for i := range history {
		if strings.TrimSpace(history[i].ThreadID) == cth.ID {
			posted = &history[i]
		}
	}
	if posted == nil {
		t.Fatalf("cth post not persisted: %+v", history)
	}
	if len(posted.Images) == 0 || !strings.Contains(string(posted.Images), "AAAB") {
		t.Fatalf("cth post images not persisted: %s", string(posted.Images))
	}
	if len(posted.Files) == 0 || !strings.Contains(string(posted.Files), "spec.pdf") {
		t.Fatalf("cth post files not persisted: %s", string(posted.Files))
	}
}

// message/postSubthread rejects a subthread that does not belong to the named
// parent thread, and rejects an empty body.
func TestMessagePostSubthreadRejectsBadInput(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	groupID := startGroupThreadForTest(t, srv)
	cth, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: "seq-3",
		CreatedBy:    "user",
	})
	if err != nil {
		t.Fatalf("openConversationSubthread: %v", err)
	}

	empty := fmt.Sprintf(`{"id":"empty","method":"message/postSubthread","params":{"thread_id":%q,"subthread_id":%q,"text":"   "}}`, groupID, cth.ID)
	if err := srv.handleLine(context.Background(), []byte(empty)); err != nil {
		t.Fatalf("message/postSubthread (empty): %v", err)
	}
	if _, ok := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "empty")["error"]; !ok {
		t.Fatalf("empty text must be rejected")
	}

	unknown := fmt.Sprintf(`{"id":"nf","method":"message/postSubthread","params":{"thread_id":%q,"subthread_id":"cth-does-not-exist","text":"hi"}}`, groupID)
	if err := srv.handleLine(context.Background(), []byte(unknown)); err != nil {
		t.Fatalf("message/postSubthread (unknown): %v", err)
	}
	if _, ok := responseByID(t, parseOutput(t, srv.out.(*lockedBuffer).String()), "nf")["error"]; !ok {
		t.Fatalf("posting to a subthread outside the parent thread must be rejected")
	}
}

// A main-stream anchor (not inside any subthread) must still be allowed to open
// a reply even when other subthreads already exist on the same group thread.
func TestOpenConversationSubthreadAllowsMainStreamAnchorAlongsideSubthreads(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	groupID := startGroupThreadForTest(t, srv)

	if _, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: "seq-3",
	}); err != nil {
		t.Fatalf("open first subthread: %v", err)
	}

	// A different main-stream message (its anchor belongs to no subthread scope).
	second, err := srv.openConversationSubthread(ThreadOpenSubParams{
		ThreadID:     groupID,
		AnchorItemID: "seq-7",
	})
	if err != nil {
		t.Fatalf("main-stream anchor should open a reply, got %v", err)
	}
	if !strings.HasPrefix(second.ID, "cth-") {
		t.Fatalf("expected a cth-* subthread id, got %q", second.ID)
	}
}
