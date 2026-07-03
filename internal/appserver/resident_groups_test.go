package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/tools"
)

func executeGroupTool(t *testing.T, kit *tools.Toolkit, name, args string) (string, error) {
	t.Helper()
	return kit.Execute(context.Background(), providers.ToolCall{Name: name, Arguments: args})
}

func startResidentDMWithRequestID(t *testing.T, srv *Server, participantID, requestID string) string {
	t.Helper()
	raw := fmt.Sprintf(`{"id":%q,"method":"thread/start","params":{"dm_participant_id":%q}}`, requestID, participantID)
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("thread/start dm: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	resp := responseByID(t, msgs, requestID)
	if errMsg, ok := resp["error"]; ok {
		t.Fatalf("thread/start dm returned error: %v", errMsg)
	}
	return remarshal[ThreadStartResult](t, resp["result"]).Thread.ID
}

func createGroupThreadIDFromResult(t *testing.T, raw string) string {
	t.Helper()
	var result struct {
		ThreadID string `json:"thread_id"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal create_group result %q: %v", raw, err)
	}
	if strings.TrimSpace(result.ThreadID) == "" {
		t.Fatalf("create_group result missing thread_id: %q", raw)
	}
	return result.ThreadID
}

func TestResidentCreateGroupJoinsCallerAndPersistsGroupThread(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Iris", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)
	kit := residentToolkitForTest(t, srv, dmID)

	raw, err := executeGroupTool(t, kit, "create_group", `{"title":"release-planning"}`)
	if err != nil {
		t.Fatalf("create_group: %v", err)
	}
	groupID := createGroupThreadIDFromResult(t, raw)

	sess, ok, err := session.Find(srv.rt.SessionDir, groupID)
	if err != nil {
		t.Fatalf("session.Find: %v", err)
	}
	if !ok || !sess.Group || sess.Title != "release-planning" {
		t.Fatalf("expected persisted group thread titled release-planning, got %+v (ok=%v)", sess, ok)
	}
	members, err := session.ListThreadMembers(srv.rt.SessionDir, groupID)
	if err != nil {
		t.Fatalf("ListThreadMembers: %v", err)
	}
	if len(members) != 1 || members[0] != participantID {
		t.Fatalf("creator should be the first member, got %+v", members)
	}
}

func TestResidentCreateGroupBudgetAndReservedTitle(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	participantID := saveNamedParticipant(t, srv.rt, "Iris", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, participantID)
	kit := residentToolkitForTest(t, srv, dmID)

	if _, err := executeGroupTool(t, kit, "create_group", `{"title":"All"}`); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("create_group with reserved title should fail, got err=%v", err)
	}
	if _, err := executeGroupTool(t, kit, "create_group", `{"title":"first"}`); err != nil {
		t.Fatalf("first create_group: %v", err)
	}
	if _, err := executeGroupTool(t, kit, "create_group", `{"title":"second"}`); err == nil || !strings.Contains(err.Error(), "max 1") {
		t.Fatalf("second create_group should exceed the per-turn budget, got err=%v", err)
	}
}

func TestResidentAddGroupMemberSemantics(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	iris := saveNamedParticipant(t, srv.rt, "Iris", "reviewer", "")
	bea := saveNamedParticipant(t, srv.rt, "Bea", "reviewer", "")
	outsider := saveNamedParticipant(t, srv.rt, "Out", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, iris)
	kit := residentToolkitForTest(t, srv, dmID)

	raw, err := executeGroupTool(t, kit, "create_group", `{"title":"team"}`)
	if err != nil {
		t.Fatalf("create_group: %v", err)
	}
	groupID := createGroupThreadIDFromResult(t, raw)

	if _, err := executeGroupTool(t, kit, "add_group_member", fmt.Sprintf(`{"thread_id":%q,"participant_id":%q}`, groupID, bea)); err != nil {
		t.Fatalf("add_group_member: %v", err)
	}
	members, err := session.ListThreadMembers(srv.rt.SessionDir, groupID)
	if err != nil {
		t.Fatalf("ListThreadMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 members after add, got %+v", members)
	}

	// Re-adding an existing member is a no-op success.
	if _, err := executeGroupTool(t, kit, "add_group_member", fmt.Sprintf(`{"thread_id":%q,"participant_id":%q}`, groupID, bea)); err != nil {
		t.Fatalf("idempotent re-add should succeed, got %v", err)
	}

	// Non-group target is rejected.
	workID := startGroupThreadForTest(t, srv) // plain conversation thread despite the helper name
	if _, err := executeGroupTool(t, kit, "add_group_member", fmt.Sprintf(`{"thread_id":%q,"participant_id":%q}`, workID, bea)); err == nil || !strings.Contains(err.Error(), "not a group thread") {
		t.Fatalf("adding into a non-group thread should fail, got err=%v", err)
	}

	// #all rejects explicit membership.
	allID, err := srv.ensureAllChannel()
	if err != nil {
		t.Fatalf("ensureAllChannel: %v", err)
	}
	if _, err := executeGroupTool(t, kit, "add_group_member", fmt.Sprintf(`{"thread_id":%q,"participant_id":%q}`, allID, bea)); err == nil || !strings.Contains(err.Error(), "all channel") {
		t.Fatalf("adding into #all should fail, got err=%v", err)
	}

	// A caller who is not a member of the target group is rejected.
	outsiderDM := startResidentDMWithRequestID(t, srv, outsider, "dm-outsider")
	outsiderKit := residentToolkitForTest(t, srv, outsiderDM)
	if _, err := executeGroupTool(t, outsiderKit, "add_group_member", fmt.Sprintf(`{"thread_id":%q,"participant_id":%q}`, groupID, outsider)); err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("non-member caller should be rejected, got err=%v", err)
	}
}

func TestAllChannelCannotBeRenamedOrArchived(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	saveNamedParticipant(t, srv.rt, "Iris", "reviewer", "")
	allID, err := srv.ensureAllChannel()
	if err != nil {
		t.Fatalf("ensureAllChannel: %v", err)
	}

	rename := fmt.Sprintf(`{"id":"rename","method":"thread/rename","params":{"thread_id":%q,"title":"everything"}}`, allID)
	if err := srv.handleLine(context.Background(), []byte(rename)); err != nil {
		t.Fatalf("thread/rename: %v", err)
	}
	archive := fmt.Sprintf(`{"id":"archive","method":"thread/archive","params":{"thread_id":%q,"archived":true}}`, allID)
	if err := srv.handleLine(context.Background(), []byte(archive)); err != nil {
		t.Fatalf("thread/archive: %v", err)
	}
	msgs := parseOutput(t, srv.out.(*lockedBuffer).String())
	for _, id := range []string{"rename", "archive"} {
		resp := responseByID(t, msgs, id)
		errMsg, ok := resp["error"]
		if !ok || !strings.Contains(fmt.Sprint(errMsg), "all channel") {
			t.Fatalf("%s of #all should be rejected, got %+v", id, resp)
		}
	}
}

func TestResidentAddGroupMemberBudgetAndCap(t *testing.T) {
	srv, _ := newResidentSpeechTestServer(t)
	iris := saveNamedParticipant(t, srv.rt, "Iris", "reviewer", "")
	dmID := startResidentDMForTest(t, srv, iris)
	kit := residentToolkitForTest(t, srv, dmID)

	// Two groups so the 8-adds-per-turn budget can trip before the
	// 8-members-per-group cap.
	rawA, err := executeGroupTool(t, kit, "create_group", `{"title":"team-a"}`)
	if err != nil {
		t.Fatalf("create_group team-a: %v", err)
	}
	groupA := createGroupThreadIDFromResult(t, rawA)
	groupB := startNamedGroupThreadForTest(t, srv, "team-b").ID
	if err := session.AddThreadMember(srv.rt.SessionDir, groupB, iris); err != nil {
		t.Fatalf("AddThreadMember iris to team-b: %v", err)
	}

	ids := make([]string, 0, 9)
	for i := 0; i < 9; i++ {
		ids = append(ids, saveNamedParticipant(t, srv.rt, fmt.Sprintf("Mate%d", i), "reviewer", ""))
	}

	// 4 adds into A, 4 into B consume the whole budget.
	for i := 0; i < 4; i++ {
		if _, err := executeGroupTool(t, kit, "add_group_member", fmt.Sprintf(`{"thread_id":%q,"participant_id":%q}`, groupA, ids[i])); err != nil {
			t.Fatalf("add %d to team-a: %v", i, err)
		}
	}
	for i := 4; i < 8; i++ {
		if _, err := executeGroupTool(t, kit, "add_group_member", fmt.Sprintf(`{"thread_id":%q,"participant_id":%q}`, groupB, ids[i])); err != nil {
			t.Fatalf("add %d to team-b: %v", i, err)
		}
	}
	if _, err := executeGroupTool(t, kit, "add_group_member", fmt.Sprintf(`{"thread_id":%q,"participant_id":%q}`, groupA, ids[8])); err == nil || !strings.Contains(err.Error(), "max 8") {
		t.Fatalf("9th add should exceed the per-turn budget, got err=%v", err)
	}

	// Member cap: fill team-a (currently iris + 4 members) up to 8 members
	// using a fresh manager, then the next add must hit the cap.
	manager := srv.residentGroupManager(iris)
	for i := 4; i < 7; i++ {
		if err := manager.AddGroupMember(context.Background(), groupA, ids[i]); err != nil {
			t.Fatalf("fill member %d: %v", i, err)
		}
	}
	members, err := session.ListThreadMembers(srv.rt.SessionDir, groupA)
	if err != nil {
		t.Fatalf("ListThreadMembers: %v", err)
	}
	if len(members) != maxGroupMembers {
		t.Fatalf("expected team-a at the member cap (%d), got %d: %+v", maxGroupMembers, len(members), members)
	}
	if err := manager.AddGroupMember(context.Background(), groupA, ids[8]); err == nil || !strings.Contains(err.Error(), "members (max 8)") {
		t.Fatalf("add beyond the member cap should fail, got err=%v", err)
	}
}
