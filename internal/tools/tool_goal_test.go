package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/goalruntime"
	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestGoalToolsLifecycle(t *testing.T) {
	env := &Env{
		RootDir:     t.TempDir(),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		SessionID:   "thread-goal-tools",
		GoalRuntime: goalruntime.NewRuntime(goalruntime.NewStore(filepath.Join(t.TempDir(), "goal_runtime.json"))),
	}

	createdRaw, err := NewGoalTool(env).Execute(context.Background(), `{"action":"create","objective":"Ship goal tools"}`)
	if err != nil {
		t.Fatalf("goal create: %v", err)
	}
	var created struct {
		Goal goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(createdRaw), &created); err != nil {
		t.Fatalf("parse goal create: %v\n%s", err, createdRaw)
	}
	if !strings.HasPrefix(created.Goal.GoalID, "goal-") ||
		created.Goal.Objective != "Ship goal tools" ||
		created.Goal.Status != goalruntime.StatusActive {
		t.Fatalf("unexpected goal create result: %+v", created)
	}
	if _, err := os.Stat(filepath.Join(env.RootDir, ".goal")); !os.IsNotExist(err) {
		t.Fatalf("goal create should not create project .goal directory: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(env.StateDir, "goals")); err == nil && len(entries) > 0 {
		t.Fatalf("goal create must not create legacy goal ledger entries: %+v", entries)
	}
	if _, err := NewGoalTool(env).Execute(context.Background(), `{"action":"create","objective":"Duplicate"}`); err == nil {
		t.Fatal("goal create should reject an unfinished runtime goal")
	}

	if _, err := NewGoalTool(env).Execute(context.Background(), `{"action":"update","status":"active"}`); err == nil {
		t.Fatal("goal update should reject unsupported statuses")
	}

	if _, err := NewGoalTool(env).Execute(context.Background(), `{}`); err == nil {
		t.Fatal("goal should reject a missing action")
	}

	statusRaw, err := NewGoalTool(env).Execute(context.Background(), `{"action":"get"}`)
	if err != nil {
		t.Fatalf("goal get: %v", err)
	}
	var status struct {
		Goal goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(statusRaw), &status); err != nil {
		t.Fatalf("parse goal get: %v\n%s", err, statusRaw)
	}
	if status.Goal.GoalID != created.Goal.GoalID || status.Goal.Status != goalruntime.StatusActive {
		t.Fatalf("unexpected goal get result: %+v", status)
	}

	completeRaw, err := NewGoalTool(env).Execute(context.Background(), `{"action":"update","status":"complete"}`)
	if err != nil {
		t.Fatalf("goal update: %v", err)
	}
	var completed struct {
		Goal goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(completeRaw), &completed); err != nil {
		t.Fatalf("parse complete goal update: %v\n%s", err, completeRaw)
	}
	if completed.Goal.GoalID != created.Goal.GoalID || completed.Goal.Status != goalruntime.StatusComplete {
		t.Fatalf("unexpected complete result: %+v", completed)
	}

	replacementRaw, err := NewGoalTool(env).Execute(context.Background(), `{"action":"create","objective":"Handle a blocker"}`)
	if err != nil {
		t.Fatalf("create replacement goal: %v", err)
	}
	var replacement struct {
		Goal goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(replacementRaw), &replacement); err != nil {
		t.Fatalf("parse replacement goal create: %v\n%s", err, replacementRaw)
	}
	if replacement.Goal.GoalID == created.Goal.GoalID || replacement.Goal.Status != goalruntime.StatusActive {
		t.Fatalf("unexpected replacement goal: %+v", replacement)
	}

	blockRaw, err := NewGoalTool(env).Execute(context.Background(), `{"action":"update","status":"blocked"}`)
	if err != nil {
		t.Fatalf("blocked goal update: %v", err)
	}
	var blocked struct {
		Goal goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(blockRaw), &blocked); err != nil {
		t.Fatalf("parse blocked goal update: %v\n%s", err, blockRaw)
	}
	if blocked.Goal.GoalID != replacement.Goal.GoalID || blocked.Goal.Status != goalruntime.StatusBlocked {
		t.Fatalf("unexpected blocked result: %+v", blocked)
	}
}

func TestCreateGoalRequiresRuntime(t *testing.T) {
	env := &Env{RootDir: t.TempDir(), StateDir: filepath.Join(t.TempDir(), "state")}
	if _, err := NewGoalTool(env).Execute(context.Background(), `{"action":"create","objective":"No runtime"}`); err == nil {
		t.Fatal("goal create should require GoalRuntime")
	}
}

func TestGetGoalWithoutRuntimeGoal(t *testing.T) {
	env := &Env{
		RootDir:     t.TempDir(),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		GoalRuntime: goalruntime.NewRuntime(goalruntime.NewStore(filepath.Join(t.TempDir(), "goal_runtime.json"))),
	}
	raw, err := NewGoalTool(env).Execute(context.Background(), `{"action":"get"}`)
	if err != nil {
		t.Fatalf("goal get: %v", err)
	}
	var status struct {
		Goal *goalruntime.Goal `json:"goal"`
	}
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatalf("parse goal get: %v\n%s", err, raw)
	}
	if status.Goal != nil {
		t.Fatalf("expected nil goal, got %+v", status.Goal)
	}
}

func TestUpdateGoalBlocksDirectly(t *testing.T) {
	env := &Env{
		RootDir:     t.TempDir(),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		SessionID:   "thread-goal-blocked",
		GoalRuntime: goalruntime.NewRuntime(goalruntime.NewStore(filepath.Join(t.TempDir(), "goal_runtime.json"))),
	}
	if _, err := NewGoalTool(env).Execute(context.Background(), `{"action":"create","objective":"Handle repeated blocker"}`); err != nil {
		t.Fatalf("goal create: %v", err)
	}

	var updated struct {
		Goal goalruntime.Goal `json:"goal"`
	}
	raw, err := NewGoalTool(env).Execute(context.Background(), `{"action":"update","status":"blocked"}`)
	if err != nil {
		t.Fatalf("goal update blocker: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &updated); err != nil {
		t.Fatalf("parse goal update: %v\n%s", err, raw)
	}
	if updated.Goal.Status != goalruntime.StatusBlocked {
		t.Fatalf("runtime goal should block directly: %+v", updated.Goal)
	}
}

func TestGoalUpdateBlockedCannotBeMarkedCompleteDirectly(t *testing.T) {
	env := &Env{
		RootDir:     t.TempDir(),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		SessionID:   "thread-goal-blocked-complete",
		GoalRuntime: goalruntime.NewRuntime(goalruntime.NewStore(filepath.Join(t.TempDir(), "goal_runtime.json"))),
	}
	if _, err := NewGoalTool(env).Execute(context.Background(), `{"action":"create","objective":"Recover after blocker"}`); err != nil {
		t.Fatalf("goal create: %v", err)
	}
	if _, err := NewGoalTool(env).Execute(context.Background(), `{"action":"update","status":"blocked"}`); err != nil {
		t.Fatalf("goal update blocked: %v", err)
	}

	_, err := NewGoalTool(env).Execute(context.Background(), `{"action":"update","status":"complete"}`)
	if err == nil {
		t.Fatal("blocked goal should require resume before complete")
	}
	message := err.Error()
	if strings.Contains(message, "invalid goal runtime transition") {
		t.Fatalf("goal tool should not expose state-machine transition errors: %v", err)
	}
	if !strings.Contains(message, "currently blocked") || !strings.Contains(message, "resume") {
		t.Fatalf("goal tool should explain how to recover from blocked status: %v", err)
	}
}

func TestGoalToolDescriptionsDefineDurableBoundary(t *testing.T) {
	def := NewGoalTool(&Env{}).Definition()
	desc := def.Description
	for _, want := range []string{
		"action=get",
		"action=create",
		"action=update",
		"explicitly requested",
		"fails if an unfinished goal exists",
		"status=complete",
		"status=blocked",
		"pause or resume",
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("goal description missing %q: %q", want, desc)
		}
	}
	assertToolSchemaOmits(t, def, "goal_id", "task", "trigger_type", "trigger_source", "next_steps", "goal_dir", "token_budget", "kind", "message", "summary", "final_artifact", "step", "reason")

	props, ok := def.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("goal schema properties have unexpected type %T", def.InputSchema["properties"])
	}
	for _, want := range []string{"action", "objective", "status"} {
		if _, ok := props[want]; !ok {
			t.Fatalf("goal schema should expose %q", want)
		}
	}
}

func assertToolSchemaOmits(t *testing.T, def providers.ToolDefinition, names ...string) {
	t.Helper()
	schema := def.InputSchema
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s schema properties have unexpected type %T", def.Name, schema["properties"])
	}
	for _, name := range names {
		if _, ok := properties[name]; ok {
			t.Fatalf("%s schema should not expose %q", def.Name, name)
		}
	}
}
