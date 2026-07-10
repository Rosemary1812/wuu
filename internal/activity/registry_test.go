package activity

import (
	"errors"
	"testing"
)

func TestRegistryControlLeaseLifecycle(t *testing.T) {
	registry := NewRegistry()
	session, lease, err := registry.Start(StartOptions{
		ID:       "activity-1",
		Kind:     KindBrowser,
		ThreadID: "thread-1",
		Workdir:  "/repo",
		PluginID: "browser-use",
		Target:   "https://example.test",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if session.State != StateStarting || session.Controller != ControllerAgent || lease.Token == "" {
		t.Fatalf("start = %+v lease=%+v", session, lease)
	}
	if err := registry.CheckControl("thread-1", session.ID, lease.Token); err != nil {
		t.Fatalf("CheckControl: %v", err)
	}

	session, err = registry.Update("thread-1", session.ID, UpdateOptions{State: StateActive, Preview: "preview://activity-1"})
	if err != nil || session.State != StateActive || session.Preview != "preview://activity-1" {
		t.Fatalf("Update = %+v, %v", session, err)
	}

	session, err = registry.Takeover("thread-1", session.ID)
	if err != nil {
		t.Fatalf("Takeover: %v", err)
	}
	if session.Controller != ControllerUser || session.State != StateUserControlled {
		t.Fatalf("takeover session = %+v", session)
	}
	if err := registry.CheckControl("thread-1", session.ID, lease.Token); !errors.Is(err, ErrControlRevoked) {
		t.Fatalf("stale lease after takeover = %v, want ErrControlRevoked", err)
	}

	session, nextLease, err := registry.Release("thread-1", session.ID)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if session.Controller != ControllerAgent || session.State != StateActive || nextLease.Token == "" || nextLease.Token == lease.Token {
		t.Fatalf("release = %+v lease=%+v", session, nextLease)
	}
	if err := registry.CheckControl("thread-1", session.ID, nextLease.Token); err != nil {
		t.Fatalf("new lease: %v", err)
	}
}

func TestRegistryStopIsIdempotentAndRevokesControl(t *testing.T) {
	registry := NewRegistry()
	session, lease, err := registry.Start(StartOptions{ID: "activity-1", Kind: KindCUA, ThreadID: "thread-1", Workdir: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := registry.Stop("thread-1", session.ID)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	second, err := registry.Stop("thread-1", session.ID)
	if err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if first.State != StateStopped || first.Controller != ControllerNone || second != first {
		t.Fatalf("stops = %+v then %+v", first, second)
	}
	if err := registry.CheckControl("thread-1", session.ID, lease.Token); !errors.Is(err, ErrControlRevoked) {
		t.Fatalf("lease after stop = %v", err)
	}
}

func TestRegistryRejectsCrossThreadOperationsAndFiltersList(t *testing.T) {
	registry := NewRegistry()
	session, lease, err := registry.Start(StartOptions{ID: "activity-1", Kind: KindBrowser, ThreadID: "thread-1", Workdir: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := registry.Start(StartOptions{ID: "activity-2", Kind: KindCUA, ThreadID: "thread-2", Workdir: "/repo"}); err != nil {
		t.Fatal(err)
	}
	if got := registry.List("thread-1"); len(got) != 1 || got[0].ID != session.ID {
		t.Fatalf("List(thread-1) = %+v", got)
	}
	if _, err := registry.Takeover("thread-2", session.ID); !errors.Is(err, ErrThreadMismatch) {
		t.Fatalf("cross-thread takeover = %v", err)
	}
	if _, _, err := registry.Release("thread-2", session.ID); !errors.Is(err, ErrThreadMismatch) {
		t.Fatalf("cross-thread release = %v", err)
	}
	if _, err := registry.Stop("thread-2", session.ID); !errors.Is(err, ErrThreadMismatch) {
		t.Fatalf("cross-thread stop = %v", err)
	}
	if err := registry.CheckControl("thread-2", session.ID, lease.Token); !errors.Is(err, ErrThreadMismatch) {
		t.Fatalf("cross-thread control check = %v", err)
	}
}

func TestRegistryValidatesKindsStatesAndDuplicateIDs(t *testing.T) {
	registry := NewRegistry()
	if _, _, err := registry.Start(StartOptions{Kind: "unknown", ThreadID: "thread-1", Workdir: "/repo"}); err == nil {
		t.Fatal("expected invalid kind error")
	}
	if _, _, err := registry.Start(StartOptions{ID: "activity-1", Kind: KindBrowser, ThreadID: "thread-1", Workdir: "/repo"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := registry.Start(StartOptions{ID: "activity-1", Kind: KindBrowser, ThreadID: "thread-1", Workdir: "/repo"}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate Start = %v", err)
	}
	if _, err := registry.Update("thread-1", "activity-1", UpdateOptions{State: "unknown"}); err == nil {
		t.Fatal("expected invalid state error")
	}
}
