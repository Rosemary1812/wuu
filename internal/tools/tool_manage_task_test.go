package tools

import (
	"context"
	"strings"
	"testing"
)

type workflowManagerStub struct{}

type pieceDoneManagerStub struct {
	workflowManagerStub
	subthreadID string
}

func (s *pieceDoneManagerStub) PieceDone(_ context.Context, subthreadID, _ string, _ *TaskHandoff) (TaskView, error) {
	s.subthreadID = subthreadID
	return TaskView{}, nil
}

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
func (workflowManagerStub) TraceTask(context.Context, string) ([]TaskEvent, error) { return nil, nil }
func (workflowManagerStub) SetPlan(context.Context, string, []TaskPiece) (TaskView, error) {
	return TaskView{}, nil
}
func (workflowManagerStub) AddTaskPiece(context.Context, string, TaskPiece) (TaskView, error) {
	return TaskView{}, nil
}
func (workflowManagerStub) ReviseTaskPiece(context.Context, string, string, string, string, []string) (TaskView, error) {
	return TaskView{}, nil
}
func (workflowManagerStub) ReassignTaskPiece(context.Context, string, string, string) (TaskView, error) {
	return TaskView{}, nil
}
func (workflowManagerStub) RetryTaskPiece(context.Context, string, string, string) (TaskView, error) {
	return TaskView{}, nil
}
func (workflowManagerStub) CancelTaskPiece(context.Context, string, string, string) (TaskView, error) {
	return TaskView{}, nil
}
func (workflowManagerStub) ResumeTask(context.Context, string, string) (TaskView, error) {
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
	for _, want := range []string{"open_thread", "promote", "conclude", "list", "trace", "set_plan", "add_piece", "revise_piece", "reassign_piece", "retry_piece", "cancel_piece", "resume"} {
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
	plan := properties["plan"].(map[string]any)
	items := plan["items"].(map[string]any)
	required := items["required"].([]string)
	if !containsString(required, "prompt") {
		t.Fatalf("set_plan piece schema must require prompt so scope limits reach workers: %v", required)
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
	if _, err := tool.Execute(context.Background(), `{"action":"set_plan","subthread_id":"cth-1","plan":[{"id":"p1","title":"inspect only","assignee":"prt-mia"}]}`); err == nil || !strings.Contains(err.Error(), "preserves the authorized outcome and scope limits") {
		t.Fatalf("set_plan without worker scope prompt error = %v", err)
	}
}

func TestManageTaskLetsActiveWorkerOmitRedundantTaskIDForPieceDone(t *testing.T) {
	manager := &pieceDoneManagerStub{}
	tool := NewManageTaskTool(&Env{ParticipantID: "prt-ada", TaskManager: manager})
	if _, err := tool.Execute(context.Background(), `{"action":"piece_done","piece_id":"p1"}`); err != nil {
		t.Fatalf("piece_done without redundant task id: %v", err)
	}
	if manager.subthreadID != "" {
		t.Fatalf("tool should let runtime infer task id, got %q", manager.subthreadID)
	}
}
