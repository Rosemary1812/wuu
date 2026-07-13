package appserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// Held draft (task-rail design §8.3): when a resident composes a reply against
// a version of a thread that has since moved — a teammate or the user posted
// while it was thinking — the post is held (not published) and returned with
// what arrived, so the agent can revise, resend with force, or stay silent.

func TestHeldDraftHoldsWhenRoomMovedAndForcePublishes(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	groupID := startNamedGroupThreadForTest(t, srv, "held").ID
	for _, id := range []string{ada, bea} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, id); err != nil {
			t.Fatalf("AddThreadMember: %v", err)
		}
	}

	// Seed a message so the group has a tail, and record Ada's read receipt up
	// to it — the durable cursor says she has read this far.
	seq, err := session.AppendHistoryRecordReturningSeq(rt.SessionDir, groupID, session.HistoryRecord{
		Role: "user", Content: "有人吗", At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := session.MarkMessageSeen(rt.SessionDir, groupID, seq, ada, session.SeenStatusCompleted, "", time.Now().UTC()); err != nil {
		t.Fatalf("mark Ada seen: %v", err)
	}
	// The room MOVES while Ada is still composing this turn: Bea's answer lands
	// on the main stream but Ada has not consumed it yet (her read cursor is
	// still at `seq`). Append it to history directly so it advances the tail
	// without routing/marking it seen for Ada — the faithful mid-turn moment.
	if _, err := session.AppendHistoryRecordReturningSeq(rt.SessionDir, groupID, session.HistoryRecord{
		Role: "participant", ParticipantID: bea, PostKind: "result", Content: "我在,我来看。", At: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Bea answers: %v", err)
	}

	// Ada's speech for this turn engaged the group. She posts with an explicit
	// basis of `seq` — the message she generated her draft against.
	speech := srv.residentParticipantSpeechForTurn(ada, nil, map[string]bool{groupID: true}, nil, nil)

	// Un-forced post: held, not published (Bea posted after Ada's basis), and
	// the note names Bea's arrival. The held result echoes the basis.
	held, err := speech.PostMessage(context.Background(), "result", "我来看这个问题。", groupID, seq, false)
	if err != nil {
		t.Fatalf("PostMessage (held path): %v", err)
	}
	if !held.Held {
		t.Fatalf("expected the draft to be held (Bea answered after Ada's basis), got %+v", held)
	}
	if held.BasisSeq != seq {
		t.Fatalf("held result should echo basis %d, got %d", seq, held.BasisSeq)
	}
	if !strings.Contains(held.HeldNote, "Bea") {
		t.Fatalf("held note should name what arrived, got %q", held.HeldNote)
	}

	// Forced resend: publishes despite the room having moved past the basis.
	posted, err := speech.PostMessage(context.Background(), "result", "补一句:我也看到了。", groupID, seq, true)
	if err != nil {
		t.Fatalf("PostMessage (force): %v", err)
	}
	if posted.Held {
		t.Fatalf("force=true must publish, not hold: %+v", posted)
	}
	if posted.Text == "" || posted.ThreadID != groupID {
		t.Fatalf("forced post not published correctly: %+v", posted)
	}
}

func TestIdleWokenReplyStillUsesHeldDraftFreshness(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	cyd := saveNamedParticipant(t, rt, "Cyd", "reviewer", "")
	groupID := startNamedGroupThreadForTest(t, srv, "idle-held").ID
	for _, id := range []string{ada, bea, cyd} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, id); err != nil {
			t.Fatalf("AddThreadMember: %v", err)
		}
	}
	s5 := appendMainChatForIdleWakeTest(t, rt.SessionDir, groupID, "participant", ada, "5")
	if err := session.MarkMessageSeen(rt.SessionDir, groupID, s5, bea, session.SeenStatusCompleted, "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_ = appendMainChatForIdleWakeTest(t, rt.SessionDir, groupID, "participant", cyd, "6")

	posted, err := srv.residentParticipantSpeechForTurn(bea, nil, map[string]bool{groupID: true}, nil, nil).
		PostMessage(context.Background(), "result", "6", groupID, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !posted.Held {
		t.Fatalf("expected stale idle-woken draft to be held, got %+v", posted)
	}
	if posted.BasisSeq != s5 {
		t.Fatalf("idle-woken draft should fall back to read cursor basis %d, got %d", s5, posted.BasisSeq)
	}
}

// A reply subthread carries its own freshness scope (T3): a cth draft is held
// only against new messages in the SAME cth, and a main-stream draft only
// against new main messages. The two scopes never cross — a main post cannot
// hold a cth draft, and a cth post cannot hold a main draft. Uses the scoped
// per-cth basis (the cth's own read watermark) exactly as PostMessage does.
func TestHeldDraftCthAndMainStreamScopesAreIsolated(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	groupID := startNamedGroupThreadForTest(t, srv, "scope").ID
	for _, id := range []string{ada, bea} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, id); err != nil {
			t.Fatalf("AddThreadMember: %v", err)
		}
	}
	cthHeld := createStoredOpenThreadForTest(t, srv, groupID, ada, "anchor-held", 1)
	cthClean := createStoredOpenThreadForTest(t, srv, groupID, ada, "anchor-clean", 2)

	appendMsg := func(threadTag, pid, text string) int {
		seq, err := session.AppendHistoryRecordReturningSeq(rt.SessionDir, groupID, session.HistoryRecord{
			Role: "participant", ParticipantID: pid, PostKind: "result", ThreadID: threadTag, Content: text, At: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("append message: %v", err)
		}
		return seq
	}
	markSeen := func(seq int, threadTag string) {
		if err := session.MarkMessageSeen(rt.SessionDir, groupID, seq, bea, session.SeenStatusCompleted, threadTag, time.Now().UTC()); err != nil {
			t.Fatalf("mark Bea seen: %v", err)
		}
	}
	// A fresh turn's speech (Bea engaged the group), so the held check applies.
	post := func(target, text string) tools.PostedMessage {
		posted, err := srv.residentParticipantSpeechForTurn(bea, nil, map[string]bool{groupID: true}, nil, nil).
			PostMessage(context.Background(), "result", text, target, 0, false)
		if err != nil {
			t.Fatalf("PostMessage to %q: %v", target, err)
		}
		return posted
	}

	// (1) cth-internal freshness: Bea read cthHeld up to s0; Ada posts again in
	// the SAME cth. Bea's cth-scoped basis is now stale -> held.
	s0 := appendMsg(cthHeld.ID, ada, "cth kickoff Bea reads")
	markSeen(s0, cthHeld.ID)
	_ = appendMsg(cthHeld.ID, ada, "Ada moves the cth")
	if held := post(cthHeld.ID, "我在这个 thread 里补一句"); !held.Held {
		t.Fatalf("a cth draft on a stale cth basis must be held, got %+v", held)
	}

	// (2) a new MAIN message must NOT hold a cth draft. Bea's cthClean cursor is
	// current; a main post is a different scope.
	sClean := appendMsg(cthClean.ID, ada, "clean cth kickoff")
	markSeen(sClean, cthClean.ID)
	_ = appendMsg("", ada, "unrelated main-stream chatter")
	if posted := post(cthClean.ID, "clean cth 里我先发"); posted.Held {
		t.Fatalf("a new main-stream message must not hold a cth draft (scope isolation), got %+v", posted)
	}

	// (3) a new CTH message must NOT hold a MAIN draft. Bea's main cursor is
	// current; a cth post is a different scope.
	sMain := appendMsg("", ada, "main kickoff Bea reads")
	markSeen(sMain, "")
	_ = appendMsg(cthHeld.ID, ada, "more cth traffic")
	if posted := post(groupID, "主流我说一句"); posted.Held {
		t.Fatalf("a new cth message must not hold a main-stream draft (scope isolation), got %+v", posted)
	}
}

// §6.1 (full-scenario checklist): three named agents answer the SAME user
// question concurrently, each against the question as its basis. One lands
// first; the other two, whose basis is now stale (the room moved when the first
// answer published), are HELD — exactly the wake-gating + basis/rebase contract
// that turns a parallel burst into "one lands, the rest revise or fall silent".
// A held agent then rebases (consumes the landed answer, re-bases its cursor)
// and its next post publishes cleanly — the natural convergence.
func TestHeldDraftThreePartyConcurrentAnswersHoldStaleBases(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	cyd := saveNamedParticipant(t, rt, "Cyd", "reviewer", "")
	groupID := startNamedGroupThreadForTest(t, srv, "trio").ID
	for _, id := range []string{ada, bea, cyd} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, id); err != nil {
			t.Fatalf("AddThreadMember: %v", err)
		}
	}

	// The user asks. All three agents read it — this seq is the shared basis they
	// each generate their answer against (the "concurrent" moment: nobody has
	// posted yet, everyone is composing off the same question).
	question, err := session.AppendHistoryRecordReturningSeq(rt.SessionDir, groupID, session.HistoryRecord{
		Role: "user", Content: "选方案甲还是方案乙?", At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed question: %v", err)
	}
	for _, id := range []string{ada, bea, cyd} {
		if err := session.MarkMessageSeen(rt.SessionDir, groupID, question, id, session.SeenStatusCompleted, "", time.Now().UTC()); err != nil {
			t.Fatalf("mark %s seen: %v", id, err)
		}
	}

	speech := func(pid string) tools.ParticipantSpeech {
		return srv.residentParticipantSpeechForTurn(pid, nil, map[string]bool{groupID: true}, nil, nil)
	}
	tailSeq := func() int {
		recs, err := session.ChatMessagesSince(rt.SessionDir, groupID, 0)
		if err != nil {
			t.Fatalf("ChatMessagesSince: %v", err)
		}
		max := 0
		for _, r := range recs {
			if r.Seq > max {
				max = r.Seq
			}
		}
		return max
	}

	// Ada answers first, against the question. Nothing landed after her basis, so
	// she publishes and takes the floor.
	adaPost, err := speech(ada).PostMessage(context.Background(), "result", "我选方案甲:本地优先。", groupID, question, false)
	if err != nil {
		t.Fatalf("Ada PostMessage: %v", err)
	}
	if adaPost.Held {
		t.Fatalf("the first answer must publish, not hold: %+v", adaPost)
	}
	adaLanded := tailSeq()
	if adaLanded <= question {
		t.Fatalf("Ada's answer should have landed past the question (%d), tail=%d", question, adaLanded)
	}

	// Bea and Cyd were composing against the SAME question basis. Ada landed
	// first, so the room moved past their basis — both are held, and the held
	// note names what arrived (Ada's answer).
	beaHeld, err := speech(bea).PostMessage(context.Background(), "result", "我选方案乙:协作强。", groupID, question, false)
	if err != nil {
		t.Fatalf("Bea PostMessage: %v", err)
	}
	if !beaHeld.Held {
		t.Fatalf("Bea's stale-basis answer must be held after Ada landed: %+v", beaHeld)
	}
	if !strings.Contains(beaHeld.HeldNote, "Ada") {
		t.Fatalf("Bea's held note should name what arrived (Ada), got %q", beaHeld.HeldNote)
	}
	cydHeld, err := speech(cyd).PostMessage(context.Background(), "result", "两个都行,看团队。", groupID, question, false)
	if err != nil {
		t.Fatalf("Cyd PostMessage: %v", err)
	}
	if !cydHeld.Held {
		t.Fatalf("Cyd's stale-basis answer must be held after Ada landed: %+v", cydHeld)
	}
	if !strings.Contains(cydHeld.HeldNote, "Ada") {
		t.Fatalf("Cyd's held note should name what arrived (Ada), got %q", cydHeld.HeldNote)
	}

	// Only Ada's answer is on the stream — the two held drafts never published.
	visible, err := session.ChatMessagesSince(rt.SessionDir, groupID, question)
	if err != nil {
		t.Fatalf("ChatMessagesSince(question): %v", err)
	}
	if len(visible) != 1 || visible[0].ParticipantID != ada {
		t.Fatalf("only Ada's answer should be on the stream after the burst, got %d records: %+v", len(visible), visible)
	}

	// Bea rebases: she consumes Ada's landed answer (advances her read cursor)
	// and re-posts a genuinely different angle against the new basis. Nothing
	// arrived after Ada's answer, so the rebase publishes cleanly.
	if err := session.MarkMessageSeen(rt.SessionDir, groupID, adaLanded, bea, session.SeenStatusCompleted, "", time.Now().UTC()); err != nil {
		t.Fatalf("Bea rebase mark seen: %v", err)
	}
	rebased, err := speech(bea).PostMessage(context.Background(), "result", "补 Ada:要多人协作再看方案乙。", groupID, adaLanded, false)
	if err != nil {
		t.Fatalf("Bea rebased PostMessage: %v", err)
	}
	if rebased.Held {
		t.Fatalf("a rebased post against the current tail must publish, not hold: %+v", rebased)
	}
	if rebased.ThreadID != groupID || rebased.Text == "" {
		t.Fatalf("Bea's rebased post did not publish correctly: %+v", rebased)
	}
}

func TestHeldDraftDoesNotHoldAFreshPost(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	groupID := startNamedGroupThreadForTest(t, srv, "fresh").ID
	if err := session.AddThreadMember(rt.SessionDir, groupID, ada); err != nil {
		t.Fatalf("AddThreadMember: %v", err)
	}
	// No seen-seq for this thread: the agent is initiating, not replying, so
	// the freshness check does not apply even though the thread is non-empty.
	if _, err := session.AppendHistoryRecordReturningSeq(rt.SessionDir, groupID, session.HistoryRecord{
		Role: "user", Content: "kickoff", At: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	speech := srv.residentParticipantSpeechForTurn(ada, nil, nil, nil, nil)
	posted, err := speech.PostMessage(context.Background(), "result", "开工。", groupID, 0, false)
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if posted.Held {
		t.Fatalf("a fresh initiating post (no basis) must not be held: %+v", posted)
	}
}
