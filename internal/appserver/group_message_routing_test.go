package appserver

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

// Issue #2 (#all 群聊消息被错误路由到单聊) defensive tests.
//
// Background: when a user posts "有人在吗" to the #all group, every group
// member receives a MessageEnvelope carrying the group's source thread. The
// bug surface is any code path that could:
//   - mark the envelope Addressed=true without an explicit @-mention
//     (routeUserMessageToResidents → deliverEnvelopeToMembers stamps
//     Addressed from the `mentioned` map, which is built from explicit
//     params.Mentions or textMentionsName regex matches);
//   - mis-label the source thread (group title vs. a DM title);
//   - send the message through the DM-resident fallback path
//     (fallbackReplyTargetThreadID already silences unaddressed batches;
//     these tests pin the addressed branch and the unaddressed fan-out).
//
// The three tests below lock down the invariants at three layers:
//   1. textMentionsName regex (pure unit) — substring-isolation contract.
//   2. End-to-end group fan-out with no mentions — bug-report case verbatim.
//   3. End-to-end group fan-out with one explicit mention — the only
//      mentioned member gets Addressed=true, others stay Addressed=false.
// If any test fails, the bug is back and the failing assertion points at
// the offending envelope attribute.

// TestIssue2TextMentionsNameDoesNotFalsePositive pins the most likely
// regression surface: if textMentionsName ever relaxed to substring matching
// without the @ prefix or without word-boundary requirements, a user
// message in #all containing a resident's name would silently address that
// resident and route its reply as if it were a DM. The regex at
// resident_router.go:642 requires the literal @name with non-letter /
// non-number / non-underscore boundaries; the table below locks that
// contract in for ASCII names and Chinese display names alike.
//
// Why pure unit: this function is the lowest-cost detector of the bug. If
// any of these cases ever flips, the integration tests below will also
// fail, but the unit test fails first and faster.
func TestIssue2TextMentionsNameDoesNotFalsePositive(t *testing.T) {
	cases := []struct {
		name, text, target string
		want               bool
	}{
		// The exact bug-report input: a Chinese ambient message in #all.
		{"ambient Chinese no @", "有人在吗", "Andy", false},
		// Substring of a larger word must not match.
		{"AndyDoe is not Andy (no @)", "AndyDoe 在吗", "Andy", false},
		{"@Andy_x is not Andy (_ blocks right boundary)", "@Andy_x 在吗", "Andy", false},

		// Positive cases that must keep working.
		{"explicit @Andy at start", "@Andy 在吗", "Andy", true},
		{"explicit @Andy mid-sentence", "大家 @Andy 在吗", "Andy", true},
		{"case-insensitive @andy matches Andy", "@andy 在吗", "Andy", true},
		{"Chinese @张三 matches 张三", "@张三 在吗", "张三", true},

		// Boundary protection — letters/digits/underscore on either side
		// block a hit; @ must be present.
		{"Andy without @ is not a hit", "Andy 在吗", "Andy", false},
		{"Chinese 张三 without @ is not a hit", "张三 在吗", "张三", false},
		{"@AndyDoe is not Andy (letter blocks right boundary)", "@AndyDoe 在吗", "Andy", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := textMentionsName(c.text, c.target); got != c.want {
				t.Fatalf("textMentionsName(%q, %q) = %v, want %v", c.text, c.target, got, c.want)
			}
		})
	}
}

// TestIssue2GroupMessageUnmentionedMembersAreNotAddressed reproduces the
// bug-report case verbatim: user posts "有人在吗" to a group with two named
// members, no explicit @-mention. Every member must receive a MessageEnvelope
// with addressed="false" and the group's source title — i.e. NOT a DM
// surface, NOT addressed. If a regression caused the bug, the model would
// treat this ambient group message as if it were a DM, and a non-resident
// helper or post_message call would route the reply into the resident's
// own DM context.
func TestIssue2GroupMessageUnmentionedMembersAreNotAddressed(t *testing.T) {
	// Two members → two resident turns after the fan-out, so the fake
	// provider needs enough dummy responses to satisfy both.
	client := &fakeClient{responses: []providers.ChatResponse{
		{Content: "ok"}, {Content: "ok"}, {Content: "ok"}, {Content: "ok"},
	}}
	srv, _, _, _ := newFocusTestServer(t, client)

	andyID := saveNamedParticipant(t, srv.rt, "Issue2AmbientAndy", "general-purpose", "")
	bobID := saveNamedParticipant(t, srv.rt, "Issue2AmbientBob", "general-purpose", "")

	group := startNamedGroupThreadForTest(t, srv, "all")
	if err := session.AddThreadMember(srv.rt.SessionDir, group.ID, andyID); err != nil {
		t.Fatalf("AddThreadMember andy: %v", err)
	}
	if err := session.AddThreadMember(srv.rt.SessionDir, group.ID, bobID); err != nil {
		t.Fatalf("AddThreadMember bob: %v", err)
	}

	// Bug-report case verbatim: user message in #all, no mentions in params.
	// The frontend only sets "mentions" when the user typed @someone.
	raw := fmt.Sprintf(`{"id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"有人在吗"}}`, group.ID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	andyHistory := waitForResidentDMHistoryContains(t, srv, andyID, "有人在吗")
	bobHistory := waitForResidentDMHistoryContains(t, srv, bobID, "有人在吗")

	assertGroupEnvelope(t, "Andy", andyHistory, "all", false)
	assertGroupEnvelope(t, "Bob", bobHistory, "all", false)
}

// TestIssue2GroupMessageExplicitMentionAddressesOnlyThatMember defends the
// other half of the invariant: when params.Mentions explicitly names Bob,
// Andy's envelope must still arrive as addressed="false". A regression that
// used params.Mentions as a substring / case-folding filter (e.g. to
// "auto-include" names found in text) would falsely address Andy and route
// Andy's reply as if it were a DM. params.Mentions is participant-ID-keyed
// (not display-name-keyed), and textMentionsName requires an @ prefix, so a
// false-positive should be impossible — but the test pins it.
func TestIssue2GroupMessageExplicitMentionAddressesOnlyThatMember(t *testing.T) {
	client := &fakeClient{responses: []providers.ChatResponse{
		{Content: "ok"}, {Content: "ok"}, {Content: "ok"}, {Content: "ok"},
	}}
	srv, _, _, _ := newFocusTestServer(t, client)

	andyID := saveNamedParticipant(t, srv.rt, "Issue2MentionAndy", "general-purpose", "")
	bobID := saveNamedParticipant(t, srv.rt, "Issue2MentionBob", "general-purpose", "")

	group := startNamedGroupThreadForTest(t, srv, "release")
	session.AddThreadMember(srv.rt.SessionDir, group.ID, andyID)
	session.AddThreadMember(srv.rt.SessionDir, group.ID, bobID)

	// params.Mentions is the explicit list of participant IDs the frontend
	// resolved from the user's @-mentions. Only Bob is in it. Andy's
	// participant ID is absent, so he must NOT receive an addressed envelope.
	raw := fmt.Sprintf(`{"id":"turn","method":"turn/start","params":{"thread_id":%q,"prompt":"Bob 在吗","mentions":[%q]}}`, group.ID, bobID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	andyHistory := waitForResidentDMHistoryContains(t, srv, andyID, "Bob 在吗")
	bobHistory := waitForResidentDMHistoryContains(t, srv, bobID, "Bob 在吗")

	assertGroupEnvelope(t, "Andy", andyHistory, "release", false)
	assertGroupEnvelope(t, "Bob", bobHistory, "release", true)
}

// assertGroupEnvelope inspects the resident's DM history for a user-role
// record whose content matches the expected envelope attributes:
//   - thread attribute = expectedThread (the group, NOT a DM label)
//   - thread_id attribute present (source reference)
//   - addressed attribute = expectedAddressed, AND the opposite value is
//     absent (catches wiring mix-ups that would render both values in one
//     block — that single failure mode would let one bug satisfy "has
//     addressed=true" by accident while the truth is the opposite)
//   - from attribute = "user" (matches the user-to-group routing branch)
//
// If the bug ever resurfaces, one of these assertions fails with a precise
// pointer to the offending attribute.
func assertGroupEnvelope(t *testing.T, who string, history []session.HistoryRecord, expectedThread string, expectedAddressed bool) {
	t.Helper()
	expectedAttr := fmt.Sprintf(`addressed=%q`, strconv.FormatBool(expectedAddressed))
	oppositeAttr := fmt.Sprintf(`addressed=%q`, strconv.FormatBool(!expectedAddressed))
	threadAttr := fmt.Sprintf(`thread=%q`, expectedThread)
	fromAttr := `from="user"`

	found := false
	for _, rec := range history {
		if rec.Role != "user" {
			continue
		}
		if !strings.Contains(rec.Content, expectedAttr) {
			continue
		}
		if strings.Contains(rec.Content, oppositeAttr) {
			t.Fatalf("%s envelope has BOTH addressed values: %q", who, rec.Content)
		}
		if !strings.Contains(rec.Content, threadAttr) {
			t.Fatalf("%s envelope missing %s attribute; got %q", who, threadAttr, rec.Content)
		}
		if !strings.Contains(rec.Content, "thread_id=") {
			t.Fatalf("%s envelope missing thread_id attribute; got %q", who, rec.Content)
		}
		if !strings.Contains(rec.Content, fromAttr) {
			t.Fatalf("%s envelope missing %s attribute; got %q", who, fromAttr, rec.Content)
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("%s: no user-role record matching expected envelope (expected %s); history=%+v", who, expectedAttr, history)
	}
}
