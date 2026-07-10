package tools

import (
	"context"
	"strings"
	"testing"
)

type workflowManagerStub struct{}

func (workflowManagerStub) OpenThread(context.Context, string, int, string) (TaskView, error) {
	return TaskView{}, nil
}
func (workflowManagerStub) PromoteThread(context.Context, string, string) (TaskView, error) {
	return TaskView{}, nil
}
func (workflowManagerStub) ConcludeTask(context.Context, string, string) (TaskView, error) {
	return TaskView{}, nil
}
func (workflowManagerStub) NeedHuman(context.Context, string, string) (TaskView, error) {
	return TaskView{}, nil
}
func (workflowManagerStub) NeedUpstream(context.Context, string, string, string) (TaskView, error) {
	return TaskView{}, nil
}
func (workflowManagerStub) UnfollowTask(context.Context, string) error { return nil }
func (workflowManagerStub) ListWorkflowThreads(context.Context, string) ([]TaskView, error) {
	return nil, nil
}
func (workflowManagerStub) SetPlan(context.Context, string, []TaskPiece) (TaskView, error) {
	return TaskView{}, nil
}
func (workflowManagerStub) PieceDone(context.Context, string, string, *TaskHandoff) (TaskView, error) {
	return TaskView{}, nil
}

func TestManageTaskDefinitionExposesOnlyGroupThreadWorkflow(t *testing.T) {
	definition := NewManageTaskTool(&Env{}).Definition()
	properties := definition.InputSchema["properties"].(map[string]any)
	action := properties["action"].(map[string]any)
	enum := action["enum"].([]string)
	for _, want := range []string{"open_thread", "promote", "conclude", "list", "set_plan"} {
		if !containsString(enum, want) {
			t.Fatalf("manage_task actions = %v, missing %q", enum, want)
		}
	}
	for _, removed := range []string{"create", "escalate", "claim", "unclaim", "update_status"} {
		if containsString(enum, removed) {
			t.Fatalf("manage_task still exposes removed action %q: %v", removed, enum)
		}
	}
	for _, removedProperty := range []string{"claim", "ack_collision_id"} {
		if _, ok := properties[removedProperty]; ok {
			t.Fatalf("manage_task still exposes removed property %q", removedProperty)
		}
	}
}

func TestManageTaskRejectsRemovedActionsAndStandaloneOpen(t *testing.T) {
	tool := NewManageTaskTool(&Env{
		ParticipantID: "prt-ada",
		TaskManager:   workflowManagerStub{},
	})
	for _, action := range []string{"create", "escalate", "claim", "unclaim", "update_status"} {
		_, err := tool.Execute(context.Background(), `{"action":"`+action+`"}`)
		if err == nil || !strings.Contains(err.Error(), "unknown action") {
			t.Fatalf("removed action %q error = %v", action, err)
		}
	}
	if _, err := tool.Execute(context.Background(), `{"action":"open_thread","thread_id":"group"}`); err == nil || !strings.Contains(err.Error(), "anchor_seq is required") {
		t.Fatalf("standalone open_thread error = %v", err)
	}
}
