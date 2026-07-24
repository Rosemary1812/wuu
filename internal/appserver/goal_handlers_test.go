package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
	"github.com/blueberrycongee/wuu/internal/goalruntime"
	"github.com/blueberrycongee/wuu/internal/session"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

func quoteGoalHandlerJSON(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestGoalActiveSummaryIgnoresLegacyGoalLedger(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	legacy := goalrunner.NewStore(statepath.GoalDir(rt.StateDir, "legacy"))
	if _, err := legacy.Init(goalrunner.Spec{ID: "legacy", Goal: "legacy goal"}); err != nil {
		t.Fatalf("Init legacy: %v", err)
	}
	if _, err := legacy.AddProgress(goalrunner.StepExecution, "legacy progress"); err != nil {
		t.Fatalf("AddProgress legacy: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"sum","method":"goal/active-summary"}`)); err != nil {
		t.Fatalf("goal/active-summary: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "sum")
	result := remarshal[GoalActiveSummaryResult](t, msg["result"])
	if result.Summary != nil {
		t.Fatalf("legacy ledger must not drive active summary: %+v", result.Summary)
	}
}

func TestGoalActiveSummaryPrefersThreadRuntimeGoal(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"thread","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "thread")["result"]).Thread.ID
	threadRuntime, err := srv.ensureThreadRuntime(srv.thread(threadID))
	if err != nil {
		t.Fatalf("ensureThreadRuntime: %v", err)
	}
	if _, err := threadRuntime.GoalRuntime.Create(goalruntime.Spec{
		ThreadID:  threadID,
		GoalID:    "runtime-goal",
		Objective: "runtime objective",
	}); err != nil {
		t.Fatalf("Create runtime goal: %v", err)
	}
	if _, err := threadRuntime.GoalRuntime.AccountUsage(goalruntime.UsageDelta{Tokens: 7, Turns: 2}, time.Now().UTC()); err != nil {
		t.Fatalf("AccountUsage: %v", err)
	}
	raw := `{"id":"sum-runtime","method":"goal/active-summary","params":{"thread_id":` + quoteGoalHandlerJSON(threadID) + `}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/active-summary: %v", err)
	}
	msg := responseByID(t, parseOutput(t, out.String()), "sum-runtime")
	result := remarshal[GoalActiveSummaryResult](t, msg["result"])
	if result.Summary == nil {
		t.Fatalf("expected runtime summary, got %+v", result)
	}
	if result.Summary.ID != "runtime-goal" || result.Summary.Text != "runtime objective" || result.Summary.Status != string(goalruntime.StatusActive) {
		t.Fatalf("unexpected runtime summary: %+v", result.Summary)
	}
	if result.Summary.TokensUsed != 7 || result.Summary.GoalTurns != 2 {
		t.Fatalf("runtime usage missing from summary: %+v", result.Summary)
	}
	if result.Summary.RecentProgress != "" || result.Summary.Step != "" {
		t.Fatalf("runtime summary should not borrow legacy progress: %+v", result.Summary)
	}
	if !result.Summary.CanPause || result.Summary.CanResume {
		t.Fatalf("unexpected runtime controls: %+v", result.Summary)
	}
}

func TestGoalActiveSummaryIncludesCurrentTurnRunningSince(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"thread","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "thread")["result"]).Thread.ID
	threadRuntime, err := srv.ensureThreadRuntime(srv.thread(threadID))
	if err != nil {
		t.Fatalf("ensureThreadRuntime: %v", err)
	}
	goal, err := threadRuntime.GoalRuntime.Create(goalruntime.Spec{
		ThreadID:  threadID,
		GoalID:    "running-goal",
		Objective: "show live runtime",
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	th := srv.thread(threadID)
	th.mu.Lock()
	th.startInternalTurnLocked("live-turn", goal.CreatedAt.Add(-time.Minute))
	th.mu.Unlock()

	raw := `{"id":"sum-live","method":"goal/active-summary","params":{"thread_id":` + quoteGoalHandlerJSON(threadID) + `}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/active-summary: %v", err)
	}
	result := remarshal[GoalActiveSummaryResult](t, responseByID(t, parseOutput(t, out.String()), "sum-live")["result"])
	if result.Summary == nil {
		t.Fatal("expected active goal summary")
	}
	runningSince, err := time.Parse(time.RFC3339Nano, result.Summary.RunningSince)
	if err != nil {
		t.Fatalf("parse running_since %q: %v", result.Summary.RunningSince, err)
	}
	if !runningSince.Equal(goal.CreatedAt) {
		t.Fatalf("running_since = %s, want goal creation %s", runningSince, goal.CreatedAt)
	}
}

func TestGoalLookupDoesNotConstructThreadRuntime(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	sess, err := session.CreateWithMetadata(rt.SessionDir, "thread-goal-read", rt.RootDir)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	goalRuntime := goalruntime.NewRuntime(goalruntime.NewStore(statepath.ThreadGoalRuntimePath(rt.StateDir, sess.ID)))
	if _, err := goalRuntime.Create(goalruntime.Spec{
		ThreadID:  sess.ID,
		GoalID:    "read-only-goal",
		Objective: "inspect without runtime side effects",
	}); err != nil {
		t.Fatalf("create goal: %v", err)
	}

	srv := New(rt, &lockedBuffer{})
	t.Cleanup(srv.Close)
	th, err := srv.ensureThreadLoaded(sess.ID)
	if err != nil {
		t.Fatalf("load thread: %v", err)
	}
	goal, ok, err := srv.currentRuntimeGoal(sess.ID)
	if err != nil {
		t.Fatalf("lookup goal: %v", err)
	}
	if !ok || goal.GoalID != "read-only-goal" {
		t.Fatalf("lookup goal = (%+v, %t), want read-only-goal", goal, ok)
	}
	th.mu.Lock()
	threadRuntime := th.execRuntime
	th.mu.Unlock()
	if threadRuntime != nil {
		t.Fatal("read-only goal lookup constructed a thread runtime")
	}
}

func TestGoalActiveSummarySkipsTerminalGoals(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	runtime := goalruntime.NewRuntime(goalruntime.NewStore(statepath.ThreadGoalRuntimePath(rt.StateDir, "thread-done")))
	if _, err := runtime.Create(goalruntime.Spec{ThreadID: "thread-done", GoalID: "done", Objective: "done goal"}); err != nil {
		t.Fatalf("Create runtime goal: %v", err)
	}
	if _, err := runtime.Complete(time.Now().UTC()); err != nil {
		t.Fatalf("Complete runtime goal: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"sum","method":"goal/active-summary","params":{"thread_id":"thread-done"}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/active-summary: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "sum")
	result := remarshal[GoalActiveSummaryResult](t, msg["result"])
	if result.Summary != nil {
		t.Fatalf("expected nil summary when only terminal goal exists, got %+v", result.Summary)
	}
}

func TestGoalActiveSummaryCollapsesMultilineText(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	runtime := goalruntime.NewRuntime(goalruntime.NewStore(statepath.ThreadGoalRuntimePath(rt.StateDir, "thread-multi")))
	if _, err := runtime.Create(goalruntime.Spec{ThreadID: "thread-multi", GoalID: "multi", Objective: "first line\nsecond line\nthird"}); err != nil {
		t.Fatalf("Create runtime goal: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"sum","method":"goal/active-summary","params":{"thread_id":"thread-multi"}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/active-summary: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "sum")
	result := remarshal[GoalActiveSummaryResult](t, msg["result"])
	if result.Summary == nil {
		t.Fatalf("expected summary, got %+v", result)
	}
	if result.Summary.Text != "first line" {
		t.Fatalf("expected text collapsed to first line, got %+v", result.Summary)
	}
}

func TestGoalActiveSummaryLeavesLongTextForRendererEllipsis(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	longGoal := strings.Repeat("a", 320)
	runtime := goalruntime.NewRuntime(goalruntime.NewStore(statepath.ThreadGoalRuntimePath(rt.StateDir, "thread-long")))
	if _, err := runtime.Create(goalruntime.Spec{ThreadID: "thread-long", GoalID: "long", Objective: longGoal}); err != nil {
		t.Fatalf("Create runtime goal: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"sum","method":"goal/active-summary","params":{"thread_id":"thread-long"}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/active-summary: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "sum")
	result := remarshal[GoalActiveSummaryResult](t, msg["result"])
	if result.Summary == nil {
		t.Fatalf("expected summary, got %+v", result)
	}
	if result.Summary.Text != longGoal {
		t.Fatalf("expected untruncated summary text, got %d chars", len(result.Summary.Text))
	}
}

func TestGoalActiveSummaryScopesToThread(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	firstRuntime := goalruntime.NewRuntime(goalruntime.NewStore(statepath.ThreadGoalRuntimePath(rt.StateDir, "thread-one")))
	if _, err := firstRuntime.Create(goalruntime.Spec{ThreadID: "thread-one", GoalID: "shared", Objective: "first thread goal"}); err != nil {
		t.Fatalf("Create first runtime goal: %v", err)
	}
	secondRuntime := goalruntime.NewRuntime(goalruntime.NewStore(statepath.ThreadGoalRuntimePath(rt.StateDir, "thread-two")))
	if _, err := secondRuntime.Create(goalruntime.Spec{ThreadID: "thread-two", GoalID: "shared", Objective: "second thread goal"}); err != nil {
		t.Fatalf("Create second runtime goal: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"sum","method":"goal/active-summary","params":{"thread_id":"thread-one"}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/active-summary: %v", err)
	}
	msg := responseByID(t, parseOutput(t, out.String()), "sum")
	result := remarshal[GoalActiveSummaryResult](t, msg["result"])
	if result.Summary == nil {
		t.Fatalf("expected active summary, got %+v", result)
	}
	if result.Summary.Text != "first thread goal" || result.Summary.ThreadID != "thread-one" {
		t.Fatalf("summary leaked across threads: %+v", result.Summary)
	}
}

func TestGoalClearScopesToThread(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	first := goalruntime.NewRuntime(goalruntime.NewStore(statepath.ThreadGoalRuntimePath(rt.StateDir, "thread-one")))
	if _, err := first.Create(goalruntime.Spec{ThreadID: "thread-one", GoalID: "shared", Objective: "first thread goal"}); err != nil {
		t.Fatalf("Create first runtime goal: %v", err)
	}
	second := goalruntime.NewRuntime(goalruntime.NewStore(statepath.ThreadGoalRuntimePath(rt.StateDir, "thread-two")))
	if _, err := second.Create(goalruntime.Spec{ThreadID: "thread-two", GoalID: "shared", Objective: "second thread goal"}); err != nil {
		t.Fatalf("Create second runtime goal: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"clear","method":"goal/clear","params":{"goal_id":"shared","thread_id":"thread-one","confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/clear: %v", err)
	}
	msg := responseByID(t, parseOutput(t, out.String()), "clear")
	if msg["error"] != nil {
		t.Fatalf("unexpected clear error: %+v", msg["error"])
	}
	if _, err := first.CurrentGoal(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first goal should be cleared, got err=%v", err)
	}
	secondState, err := second.CurrentGoal()
	if err != nil {
		t.Fatalf("CurrentGoal second: %v", err)
	}
	if secondState.Status != goalruntime.StatusActive {
		t.Fatalf("second goal should remain active: %+v", secondState)
	}
}

func TestGoalClearRequiresConfirmation(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	runtime := goalruntime.NewRuntime(goalruntime.NewStore(statepath.ThreadGoalRuntimePath(rt.StateDir, "thread-live")))
	if _, err := runtime.Create(goalruntime.Spec{ThreadID: "thread-live", GoalID: "live", Objective: "live goal"}); err != nil {
		t.Fatalf("Create runtime goal: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"clear","method":"goal/clear","params":{"goal_id":"live","thread_id":"thread-live"}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/clear: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "clear")
	if msg["error"] == nil {
		t.Fatalf("expected error when confirm_user_approved missing, got %+v", msg)
	}
	if _, err := runtime.CurrentGoal(); err != nil {
		t.Fatalf("goal must not be cleared without confirmation: %v", err)
	}
}

func TestGoalClearRequiresRuntimeGoal(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	store := goalrunner.NewStore(statepath.GoalDir(rt.StateDir, "live"))
	if _, err := store.Init(goalrunner.Spec{ID: "live", Goal: "live goal"}); err != nil {
		t.Fatalf("Init live: %v", err)
	}
	if _, err := store.AddProgress(goalrunner.StepExecution, "running"); err != nil {
		t.Fatalf("AddProgress: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"clear","method":"goal/clear","params":{"goal_id":"live","confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/clear: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "clear")
	if msg["error"] == nil {
		t.Fatalf("expected missing runtime goal error, got %+v", msg)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.Goal != "live goal" {
		t.Fatalf("legacy ledger must not be changed by goal/clear: %+v", state)
	}
}

func TestGoalPauseResumeClearRuntimeGoal(t *testing.T) {
	client := &fakeClient{response: providersResponse("resumed goal turn done")}
	rt := newTestRuntime(t, client)
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"thread","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "thread")["result"]).Thread.ID
	threadRuntime, err := srv.ensureThreadRuntime(srv.thread(threadID))
	if err != nil {
		t.Fatalf("ensureThreadRuntime: %v", err)
	}
	if _, err := threadRuntime.GoalRuntime.Create(goalruntime.Spec{ThreadID: threadID, GoalID: "runtime-controls", Objective: "control me"}); err != nil {
		t.Fatalf("Create runtime goal: %v", err)
	}

	pauseRaw := `{"id":"pause","method":"goal/pause","params":{"thread_id":` + quoteGoalHandlerJSON(threadID) + `,"goal_id":"runtime-controls","confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(pauseRaw)); err != nil {
		t.Fatalf("goal/pause: %v", err)
	}
	paused, err := threadRuntime.GoalRuntime.CurrentGoal()
	if err != nil {
		t.Fatalf("CurrentGoal paused: %v", err)
	}
	if paused.Status != goalruntime.StatusPaused {
		t.Fatalf("goal should be paused: %+v", paused)
	}
	sumRaw := `{"id":"sum-paused","method":"goal/active-summary","params":{"thread_id":` + quoteGoalHandlerJSON(threadID) + `}}`
	if err := srv.handleLine(context.Background(), []byte(sumRaw)); err != nil {
		t.Fatalf("goal/active-summary paused: %v", err)
	}
	pausedSummary := remarshal[GoalActiveSummaryResult](t, responseByID(t, parseOutput(t, out.String()), "sum-paused")["result"])
	if pausedSummary.Summary == nil || pausedSummary.Summary.Status != string(goalruntime.StatusPaused) || !pausedSummary.Summary.CanResume || pausedSummary.Summary.StopReason != "paused" {
		t.Fatalf("unexpected paused summary: %+v", pausedSummary.Summary)
	}

	resumeRaw := `{"id":"resume","method":"goal/resume","params":{"thread_id":` + quoteGoalHandlerJSON(threadID) + `,"goal_id":"runtime-controls","confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(resumeRaw)); err != nil {
		t.Fatalf("goal/resume: %v", err)
	}
	resumed, err := threadRuntime.GoalRuntime.CurrentGoal()
	if err != nil {
		t.Fatalf("CurrentGoal resumed: %v", err)
	}
	if resumed.Status != goalruntime.StatusActive {
		t.Fatalf("goal should be active after resume: %+v", resumed)
	}
	waitForMethod(t, out, NotificationTurnCompleted)
	if got := fakeClientRequestCount(client); got != 1 {
		t.Fatalf("goal resume should kick one continuation turn, got %d provider requests", got)
	}

	clearRaw := `{"id":"clear","method":"goal/clear","params":{"thread_id":` + quoteGoalHandlerJSON(threadID) + `,"goal_id":"runtime-controls","confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(clearRaw)); err != nil {
		t.Fatalf("goal/clear: %v", err)
	}
	if _, err := threadRuntime.GoalRuntime.CurrentGoal(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("goal should be cleared, got err=%v", err)
	}
}

func TestGoalUpdateTextRequiresConfirmation(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	runtime := goalruntime.NewRuntime(goalruntime.NewStore(statepath.ThreadGoalRuntimePath(rt.StateDir, "thread-live")))
	if _, err := runtime.Create(goalruntime.Spec{ThreadID: "thread-live", GoalID: "live", Objective: "live goal"}); err != nil {
		t.Fatalf("Create runtime goal: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"upd","method":"goal/update-text","params":{"goal_id":"live","thread_id":"thread-live","text":"new goal"}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/update-text: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "upd")
	if msg["error"] == nil {
		t.Fatalf("expected error when confirm_user_approved missing, got %+v", msg)
	}
	state, err := runtime.CurrentGoal()
	if err != nil {
		t.Fatalf("CurrentGoal: %v", err)
	}
	if state.Objective != "live goal" {
		t.Fatalf("goal text must not change without confirmation, got %q", state.Objective)
	}
}

func TestGoalUpdateTextRejectsEmptyText(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"upd","method":"goal/update-text","params":{"goal_id":"live","text":"   ","confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/update-text: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "upd")
	if msg["error"] == nil {
		t.Fatalf("expected error for empty text, got %+v", msg)
	}
}

func TestGoalUpdateTextRequiresRuntimeGoal(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")

	store := goalrunner.NewStore(statepath.GoalDir(rt.StateDir, "live"))
	if _, err := store.Init(goalrunner.Spec{ID: "live", Goal: "live goal"}); err != nil {
		t.Fatalf("Init live: %v", err)
	}

	out := &lockedBuffer{}
	srv := New(rt, out)
	raw := `{"id":"upd","method":"goal/update-text","params":{"goal_id":"live","text":"updated goal","confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/update-text: %v", err)
	}
	msgs := parseOutput(t, out.String())
	msg := responseByID(t, msgs, "upd")
	if msg["error"] == nil {
		t.Fatalf("expected missing runtime goal error, got %+v", msg)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.Goal != "live goal" {
		t.Fatalf("legacy ledger must not be updated by goal/update-text, got %q", state.Goal)
	}
}

func TestGoalUpdateTextUpdatesRuntimeGoal(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.StateDir = filepath.Join(rt.RootDir, ".wuu-state")
	out := &lockedBuffer{}
	srv := New(rt, out)

	if err := srv.handleLine(context.Background(), []byte(`{"id":"thread","method":"thread/start"}`)); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := remarshal[ThreadStartResult](t, responseByID(t, parseOutput(t, out.String()), "thread")["result"]).Thread.ID
	threadRuntime, err := srv.ensureThreadRuntime(srv.thread(threadID))
	if err != nil {
		t.Fatalf("ensureThreadRuntime: %v", err)
	}
	if _, err := threadRuntime.GoalRuntime.Create(goalruntime.Spec{ThreadID: threadID, GoalID: "runtime-edit", Objective: "old runtime goal"}); err != nil {
		t.Fatalf("Create runtime goal: %v", err)
	}

	raw := `{"id":"upd-runtime","method":"goal/update-text","params":{"thread_id":` + quoteGoalHandlerJSON(threadID) + `,"goal_id":"runtime-edit","text":"new runtime goal","confirm_user_approved":true}}`
	if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("goal/update-text: %v", err)
	}
	msg := responseByID(t, parseOutput(t, out.String()), "upd-runtime")
	if msg["error"] != nil {
		t.Fatalf("unexpected update error: %+v", msg["error"])
	}
	runtimeGoal, err := threadRuntime.GoalRuntime.CurrentGoal()
	if err != nil {
		t.Fatalf("CurrentGoal: %v", err)
	}
	if runtimeGoal.Objective != "new runtime goal" {
		t.Fatalf("runtime objective not updated: %+v", runtimeGoal)
	}
}
