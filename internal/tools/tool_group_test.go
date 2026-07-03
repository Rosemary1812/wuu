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
}

func (f *fakeGroupManager) CreateGroup(_ context.Context, title string) (string, error) {
	f.createdTitles = append(f.createdTitles, title)
	return "thr-created", nil
}

func (f *fakeGroupManager) AddGroupMember(_ context.Context, threadID, participantID string) error {
	f.added = append(f.added, [2]string{threadID, participantID})
	return nil
}

func TestGroupToolsRequireResidentParticipantCapability(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	// Task runs get participant speech but never resident capability: both
	// group tools must stay unavailable (resident doc §6 matrix).
	kit.SetParticipantSpeechEnabled(true)
	kit.SetParticipantIdentity("prt-task")
	kit.SetGroupManager(&fakeGroupManager{})

	for _, name := range []string{"create_group", "add_group_member"} {
		_, err := kit.Execute(context.Background(), providers.ToolCall{Name: name, Arguments: `{"title":"x","thread_id":"t","participant_id":"p"}`})
		if err == nil || !strings.Contains(err.Error(), "resident participant capability") {
			t.Fatalf("%s without resident capability should fail, got err=%v", name, err)
		}
	}
}

func TestGroupToolsExecuteThroughGroupManager(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	manager := &fakeGroupManager{}
	kit.SetResidentParticipantEnabled(true)
	kit.SetParticipantIdentity("prt-resident")
	kit.SetGroupManager(manager)

	out, err := kit.Execute(context.Background(), providers.ToolCall{Name: "create_group", Arguments: `{"title":"launch"}`})
	if err != nil {
		t.Fatalf("create_group: %v", err)
	}
	if !strings.Contains(out, "thr-created") {
		t.Fatalf("create_group result should carry the new thread id, got %q", out)
	}
	if len(manager.createdTitles) != 1 || manager.createdTitles[0] != "launch" {
		t.Fatalf("manager.CreateGroup calls = %+v", manager.createdTitles)
	}

	if _, err := kit.Execute(context.Background(), providers.ToolCall{Name: "add_group_member", Arguments: `{"thread_id":"thr-created","participant_id":"prt-x"}`}); err != nil {
		t.Fatalf("add_group_member: %v", err)
	}
	if len(manager.added) != 1 || manager.added[0] != [2]string{"thr-created", "prt-x"} {
		t.Fatalf("manager.AddGroupMember calls = %+v", manager.added)
	}
}

func TestGroupToolsRequireConfiguredManager(t *testing.T) {
	kit, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("tools.New: %v", err)
	}
	kit.SetResidentParticipantEnabled(true)
	kit.SetParticipantIdentity("prt-resident")

	if _, err := kit.Execute(context.Background(), providers.ToolCall{Name: "create_group", Arguments: `{"title":"x"}`}); err == nil || !strings.Contains(err.Error(), "group management not configured") {
		t.Fatalf("create_group without a manager should fail, got err=%v", err)
	}
}
