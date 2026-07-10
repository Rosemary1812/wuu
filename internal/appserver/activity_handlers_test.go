package appserver

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/activity"
)

func TestServerActivityLifecycleRequestsAndNotifications(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.ActivityRegistry = activity.NewRegistry()
	out := &lockedBuffer{}
	srv := New(rt, out)

	started, _, err := rt.ActivityRegistry.Start(activity.StartOptions{
		ID:       "activity-1",
		Kind:     activity.KindBrowser,
		ThreadID: "thread-1",
		Workdir:  rt.RootDir,
		PluginID: "browser-use",
		Target:   "https://example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.ActivityRegistry.Update("thread-1", started.ID, activity.UpdateOptions{State: activity.StateActive, Preview: "preview://activity-1"}); err != nil {
		t.Fatal(err)
	}

	requests := []string{
		`{"id":"list","method":"activity/list","params":{"thread_id":"thread-1"}}`,
		`{"id":"takeover","method":"activity/takeover","params":{"thread_id":"thread-1","activity_id":"activity-1"}}`,
		`{"id":"release","method":"activity/release","params":{"thread_id":"thread-1","activity_id":"activity-1"}}`,
		`{"id":"stop","method":"activity/stop","params":{"thread_id":"thread-1","activity_id":"activity-1"}}`,
	}
	for _, raw := range requests {
		if err := srv.handleLine(context.Background(), []byte(raw)); err != nil {
			t.Fatalf("handleLine %s: %v", raw, err)
		}
	}

	messages := parseOutput(t, out.String())
	listed := remarshal[ActivityListResult](t, responseByID(t, messages, "list")["result"])
	if len(listed.Activities) != 1 || listed.Activities[0].ID != started.ID || listed.Activities[0].Workdir != rt.RootDir {
		t.Fatalf("activity/list = %+v", listed)
	}
	takeover := remarshal[ActivityActionResult](t, responseByID(t, messages, "takeover")["result"])
	if takeover.Activity.Controller != string(activity.ControllerUser) || takeover.Activity.State != string(activity.StateUserControlled) {
		t.Fatalf("takeover = %+v", takeover)
	}
	release := remarshal[ActivityReleaseResult](t, responseByID(t, messages, "release")["result"])
	if release.Activity.Controller != string(activity.ControllerAgent) || strings.TrimSpace(release.LeaseToken) == "" {
		t.Fatalf("release = %+v", release)
	}
	stopped := remarshal[ActivityActionResult](t, responseByID(t, messages, "stop")["result"])
	if stopped.Activity.State != string(activity.StateStopped) || stopped.Activity.Controller != string(activity.ControllerNone) {
		t.Fatalf("stop = %+v", stopped)
	}

	for _, method := range []string{
		NotificationActivityStarted,
		NotificationActivityUpdated,
		NotificationActivityControlChanged,
		NotificationActivityStopped,
	} {
		notification := notificationByMethod(t, messages, method)
		payload := remarshal[ActivitySession](t, notification["params"])
		if payload.Workdir != rt.RootDir || payload.ThreadID != "thread-1" || payload.ID != started.ID || payload.Kind != string(activity.KindBrowser) {
			t.Fatalf("%s payload = %+v", method, payload)
		}
	}
}

func TestServerActivityRejectsCrossThreadControl(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.ActivityRegistry = activity.NewRegistry()
	if _, _, err := rt.ActivityRegistry.Start(activity.StartOptions{ID: "activity-1", Kind: activity.KindCUA, ThreadID: "thread-1", Workdir: rt.RootDir}); err != nil {
		t.Fatal(err)
	}
	out := &lockedBuffer{}
	srv := New(rt, out)
	if err := srv.handleLine(context.Background(), []byte(`{"id":"takeover","method":"activity/takeover","params":{"thread_id":"thread-2","activity_id":"activity-1"}}`)); err != nil {
		t.Fatal(err)
	}
	response := responseByID(t, parseOutput(t, out.String()), "takeover")
	if response["error"] == nil || !strings.Contains(fmt.Sprint(response["error"]), activity.ErrThreadMismatch.Error()) {
		t.Fatalf("cross-thread response = %+v", response)
	}
}
