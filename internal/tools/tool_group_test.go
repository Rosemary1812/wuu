package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

type fakeGroupManager struct {
	createdTitles []string
	added         [][2]string
	members       map[string][]GroupMember
}

func (f *fakeGroupManager) CreateGroup(_ context.Context, title string) (string, error) {
	f.createdTitles = append(f.createdTitles, title)
	return "thr-created", nil
}

func (f *fakeGroupManager) AddGroupMember(_ context.Context, threadID, participantID string) error {
	f.added = append(f.added, [2]string{threadID, participantID})
	return nil
}

func (f *fakeGroupManager) ListGroupMembers(_ context.Context, threadID string) ([]GroupMember, error) {
	return f.members[threadID], nil
}

// Group management folded into manage_participant (create_group / add_member
// actions). Resident-only-ness is now enforced at execute time by the presence
// of an attached GroupManager: task runs get participant speech (so
// manage_participant is on their surface) but never receive a GroupManager, so
// the group actions fail closed.
func TestManageParticipantGroupActionsRequireGroupManager(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	kit.SetParticipantSpeechEnabled(true)
	kit.SetParticipantIdentity("prt-task")

	cases := []struct {
		args string
	}{
		{`{"action":"create_group","title":"x"}`},
		{`{"action":"add_member","thread_id":"t","participant_id":"p"}`},
	}
	for _, c := range cases {
		_, err := kit.Execute(context.Background(), providers.ToolCall{Name: "manage_participant", Arguments: c.args})
		if err == nil || !strings.Contains(err.Error(), "group management not configured") {
			t.Fatalf("group action %s without a GroupManager should fail closed, got err=%v", c.args, err)
		}
	}
}

func TestManageParticipantGroupActionsExecuteThroughGroupManager(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	manager := &fakeGroupManager{}
	kit.SetParticipantSpeechEnabled(true)
	kit.SetResidentParticipantEnabled(true)
	kit.SetParticipantIdentity("prt-resident")
	kit.SetGroupManager(manager)

	out, err := kit.Execute(context.Background(), providers.ToolCall{Name: "manage_participant", Arguments: `{"action":"create_group","title":"launch"}`})
	if err != nil {
		t.Fatalf("create_group: %v", err)
	}
	if !strings.Contains(out, "thr-created") {
		t.Fatalf("create_group result should carry the new thread id, got %q", out)
	}
	if len(manager.createdTitles) != 1 || manager.createdTitles[0] != "launch" {
		t.Fatalf("manager.CreateGroup calls = %+v", manager.createdTitles)
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{Name: "manage_participant", Arguments: `{"action":"add_member","thread_id":"thr-created","participant_id":"prt-x"}`}); err != nil {
		t.Fatalf("add_member: %v", err)
	}
	if len(manager.added) != 1 || manager.added[0] != [2]string{"thr-created", "prt-x"} {
		t.Fatalf("manager.AddGroupMember calls = %+v", manager.added)
	}
}

func TestManageParticipantGroupActionsRequireConfiguredManager(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	kit.SetParticipantSpeechEnabled(true)
	kit.SetResidentParticipantEnabled(true)
	kit.SetParticipantIdentity("prt-resident")

	if _, err := kit.Execute(context.Background(), providers.ToolCall{Name: "manage_participant", Arguments: `{"action":"create_group","title":"x"}`}); err == nil || !strings.Contains(err.Error(), "group management not configured") {
		t.Fatalf("create_group without a manager should fail, got err=%v", err)
	}
}
