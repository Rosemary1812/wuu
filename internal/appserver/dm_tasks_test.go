package appserver

import (
	"context"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/session"
)

func dmTaskFixture(t *testing.T) (srv *Server, dmThreadID, ada, bea string) {
	t.Helper()
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv = New(rt, &lockedBuffer{})
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada = saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea = saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	th, err := srv.ensureResidentDMThread(ada)
	if err != nil {
		t.Fatalf("ensureResidentDMThread: %v", err)
	}
	th.mu.Lock()
	dmThreadID = th.ID
	th.mu.Unlock()
	return srv, dmThreadID, ada, bea
}

func TestDMHasNoThreadOrTaskWorkflow(t *testing.T) {
	srv, dmThreadID, ada, _ := dmTaskFixture(t)
	manager := srv.residentTaskManager(ada)
	if _, err := manager.OpenThread(context.Background(), dmThreadID, 1, "nope"); err == nil || !strings.Contains(err.Error(), "not a group") {
		t.Fatalf("open_thread in DM = %v, want group-only refusal", err)
	}
	if _, err := manager.ListWorkflowThreads(context.Background(), dmThreadID); err == nil || !strings.Contains(err.Error(), "not a group") {
		t.Fatalf("list in DM = %v, want group-only refusal", err)
	}
}

func TestOrdinarySessionHasNoThreadOrTaskWorkflow(t *testing.T) {
	srv, _, ada, _ := dmTaskFixture(t)
	workID := "work-session"
	if _, err := session.CreateWithMetadata(srv.rt.SessionDir, workID, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.residentTaskManager(ada).OpenThread(context.Background(), workID, 1, "nope"); err == nil || !strings.Contains(err.Error(), "not a group") {
		t.Fatalf("open_thread in ordinary session = %v, want group-only refusal", err)
	}
}
