package appserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	"github.com/blueberrycongee/wuu/internal/session"
)

type failOnceTurnStartedWriter struct {
	mu      sync.Mutex
	failed  bool
	output  lockedBuffer
	failErr error
}

func (w *failOnceTurnStartedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if !w.failed && bytes.Contains(p, []byte(`"method":"turn/started"`)) {
		w.failed = true
		err := w.failErr
		w.mu.Unlock()
		return 0, err
	}
	w.mu.Unlock()
	return w.output.Write(p)
}

func TestResidentPullAdmissionRetriesAfterTurnStartedNotificationFailure(t *testing.T) {
	client := &fakeClient{response: providersResponse("pull handled")}
	rt := newTestRuntime(t, client)
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() {
		waitForResidentQuiesce(t, srv)
		srv.Close()
	})
	residentID := saveNamedParticipant(t, rt, "Pull Reader", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	if err := session.AddThreadMember(rt.SessionDir, groupID, residentID); err != nil {
		t.Fatal(err)
	}

	base := time.Now().UTC().Add(-time.Minute)
	firstSeq := appendResidentAdmissionSourceMessage(t, rt.SessionDir, groupID, "user", "", "pull-first", base)
	secondSeq := appendResidentAdmissionSourceMessage(t, rt.SessionDir, groupID, "user", "", "pull-second", base.Add(time.Second))

	writer := &failOnceTurnStartedWriter{failErr: errors.New("injected turn-start notification failure")}
	srv.out = writer
	srv.drainResidentAgent(residentID)

	if got := residentAdmissionMatchingRequests(client, "pull-first", "pull-second"); len(got) != 0 {
		t.Fatalf("provider ran before turn-start notification succeeded: %d requests", len(got))
	}
	if watermark, err := session.ThreadReadWatermark(rt.SessionDir, groupID, residentID); err != nil || watermark != 0 {
		t.Fatalf("watermark after failed admission = %d, %v; want unread", watermark, err)
	}
	assertResidentAdmissionBatchHistory(t, srv, residentID, []string{"pull-first", "pull-second"}, 1)

	// Retry synchronously; the scheduled fallback retry may also fire later, but
	// the read receipts and stable client id make it a no-op.
	srv.drainResidentAgent(residentID)
	req := waitForResidentAdmissionRequest(t, client, "pull-first", "pull-second")
	assertResidentAdmissionRequestOrder(t, req, "pull-first", "pull-second")
	waitForResidentAdmissionReceipts(t, rt.SessionDir, groupID, residentID, []int{firstSeq, secondSeq})
	assertResidentAdmissionBatchHistory(t, srv, residentID, []string{"pull-first", "pull-second"}, 1)
	assertStableResidentAdmissionRequestCount(t, client, []string{"pull-first", "pull-second"}, 1)
}

func TestResidentMixedAdmissionRetriesAfterTaskAttemptStartFailure(t *testing.T) {
	client := &fakeClient{response: providersResponse("mixed handled")}
	rt := newTestRuntime(t, client)
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(func() {
		waitForResidentQuiesce(t, srv)
		srv.Close()
	})
	ownerID := saveNamedParticipant(t, rt, "Task Owner", "lead", "")
	residentID := saveNamedParticipant(t, rt, "Task Worker", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	for _, participantID := range []string{ownerID, residentID} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, participantID); err != nil {
			t.Fatal(err)
		}
	}

	base := time.Now().UTC().Add(-time.Minute)
	anchorSeq := appendResidentAdmissionSourceMessage(t, rt.SessionDir, groupID, "participant", ownerID, "task-anchor", base)
	if err := session.MarkMessageSeen(rt.SessionDir, groupID, anchorSeq, residentID, session.SeenStatusCompleted, "", base); err != nil {
		t.Fatal(err)
	}
	firstSeq := appendResidentAdmissionSourceMessage(t, rt.SessionDir, groupID, "participant", ownerID, "mixed-pull-first", base.Add(time.Second))
	secondSeq := appendResidentAdmissionSourceMessage(t, rt.SessionDir, groupID, "participant", ownerID, "mixed-pull-second", base.Add(2*time.Second))

	open := createStoredOpenThreadForTest(t, srv, groupID, ownerID, "mixed-anchor", anchorSeq)
	task, err := session.EscalateConversationThread(rt.SessionDir, open.ID, ownerID, "mixed retry task")
	if err != nil {
		t.Fatal(err)
	}
	task, err = session.SetConversationThreadPlan(rt.SessionDir, task.ID, []session.TaskPiece{{
		ID: "mixed-node", Title: "mixed push task", Assignee: residentID, Status: session.TaskPiecePending,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetConversationThreadExecState(rt.SessionDir, task.ID, session.ExecStateExecuting); err != nil {
		t.Fatal(err)
	}
	attempt, _, err := session.ReserveTaskAttempt(rt.SessionDir, task.ID, "mixed-node", residentID)
	if err != nil {
		t.Fatal(err)
	}
	setResidentAdmissionAttemptStatus(t, rt.SessionDir, attempt.ID, session.TaskAttemptFailed)

	push := MessageEnvelope{
		ID: "mixed-push", SourceThreadID: groupID, SourceTitle: "mixed group",
		SourceSubthreadID: task.ID, TaskAttemptID: attempt.ID, TaskNodeID: "mixed-node",
		SenderKind: "system", SenderName: "task plan", Text: "mixed-push-task",
		CreatedAt: base.Add(3 * time.Second),
	}
	payload, err := json.Marshal(push)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.EnqueueResidentEnvelope(rt.SessionDir, session.ResidentEnvelope{
		ID: push.ID, ParticipantID: residentID, EnvelopeJSON: payload, CreatedAt: push.CreatedAt,
	}); err != nil {
		t.Fatal(err)
	}

	srv.drainResidentAgent(residentID)
	if got := residentAdmissionMatchingRequests(client, "mixed-pull-first", "mixed-pull-second", "mixed-push-task"); len(got) != 0 {
		t.Fatalf("provider ran after rejected task-attempt start: %d requests", len(got))
	}
	if watermark, err := session.ThreadReadWatermark(rt.SessionDir, groupID, residentID); err != nil || watermark != anchorSeq {
		t.Fatalf("watermark after failed mixed admission = %d, %v; want %d", watermark, err, anchorSeq)
	}
	if pending, err := session.PendingResidentEnvelopes(rt.SessionDir, residentID, 0); err != nil || len(pending) != 1 || pending[0].ID != push.ID {
		t.Fatalf("push inbox after failed mixed admission = %+v, %v", pending, err)
	}
	if got, err := session.TaskAttemptByID(rt.SessionDir, attempt.ID); err != nil || got.Status != session.TaskAttemptFailed {
		t.Fatalf("terminal attempt changed by compensation = %+v, %v", got, err)
	}
	assertResidentAdmissionBatchHistory(t, srv, residentID, []string{"mixed-pull-first", "mixed-pull-second", "mixed-push-task"}, 1)

	// Repair the injected task-attempt state, then retry the exact durable batch.
	setResidentAdmissionAttemptStatus(t, rt.SessionDir, attempt.ID, session.TaskAttemptQueued)
	srv.drainResidentAgent(residentID)
	req := waitForResidentAdmissionRequest(t, client, "mixed-pull-first", "mixed-pull-second", "mixed-push-task")
	assertResidentAdmissionRequestOrder(t, req, "mixed-pull-first", "mixed-pull-second", "mixed-push-task")
	waitForResidentAdmissionReceipts(t, rt.SessionDir, groupID, residentID, []int{firstSeq, secondSeq})
	assertResidentAdmissionBatchHistory(t, srv, residentID, []string{"mixed-pull-first", "mixed-pull-second", "mixed-push-task"}, 1)
	assertStableResidentAdmissionRequestCount(t, client, []string{"mixed-pull-first", "mixed-pull-second", "mixed-push-task"}, 1)
}

func TestResidentAdmissionCompensationRetriesWithoutReleasingOwnership(t *testing.T) {
	scenario := newResidentCompensationScenario(t)
	t.Cleanup(func() {
		waitForResidentQuiesce(t, scenario.srv)
		scenario.srv.Close()
	})

	const failures = 2
	var mu sync.Mutex
	resolveCalls := 0
	retryPaused := make(chan int, failures)
	retryAllowed := make(chan struct{})
	t.Cleanup(func() { close(retryAllowed) })
	scenario.srv.resolveResidentCompensationForTest = func(pending session.ResidentAdmissionCompensation) error {
		mu.Lock()
		resolveCalls++
		call := resolveCalls
		mu.Unlock()
		if call <= failures {
			return errors.New("injected resident compensation failure")
		}
		return session.ResolveResidentAdmissionCompensation(scenario.rt.SessionDir, pending)
	}
	scenario.srv.beforeResidentCompensationRetryForTest = func(phase string, attempt int, _ error) {
		if phase != "resolve" {
			return
		}
		retryPaused <- attempt
		<-retryAllowed
	}

	scenario.srv.kickResidentAgent(scenario.residentID)
	for attempt := 1; attempt <= failures; attempt++ {
		select {
		case got := <-retryPaused:
			if got != attempt {
				t.Fatalf("paused retry = %d, want %d", got, attempt)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("compensation retry %d did not pause", attempt)
		}
		assertResidentCompensationPending(t, scenario, false)
		retryAllowed <- struct{}{}
	}

	req := waitForResidentAdmissionRequest(t, scenario.client, scenario.pullText, scenario.pushText)
	assertResidentAdmissionRequestOrder(t, req, scenario.pullText, scenario.pushText)
	waitForResidentAdmissionReceipts(t, scenario.rt.SessionDir, scenario.groupID, scenario.residentID, []int{scenario.pullSeq})
	waitForResidentCompensationJournalEmpty(t, scenario.rt.SessionDir)
	waitForResidentInboxEmpty(t, scenario.rt.SessionDir, scenario.residentID)
	assertResidentAdmissionBatchHistory(t, scenario.srv, scenario.residentID, []string{scenario.pullText, scenario.pushText}, 1)
	assertStableResidentAdmissionRequestCount(t, scenario.client, []string{scenario.pullText, scenario.pushText}, 1)
}

func TestResidentAdmissionCompensationDefersDurablyDuringClose(t *testing.T) {
	scenario := newResidentCompensationScenario(t)
	retryPaused := make(chan struct{}, 1)
	retryAllowed := make(chan struct{})
	var allowOnce sync.Once
	allowRetry := func() { allowOnce.Do(func() { close(retryAllowed) }) }
	t.Cleanup(func() {
		allowRetry()
		scenario.srv.Close()
	})
	scenario.srv.resolveResidentCompensationForTest = func(session.ResidentAdmissionCompensation) error {
		return errors.New("injected persistent resident compensation failure")
	}
	scenario.srv.beforeResidentCompensationRetryForTest = func(phase string, _ int, _ error) {
		if phase != "resolve" {
			return
		}
		select {
		case retryPaused <- struct{}{}:
		case <-time.After(3 * time.Second):
			return
		}
		<-retryAllowed
	}

	scenario.srv.kickResidentAgent(scenario.residentID)
	select {
	case <-retryPaused:
	case <-time.After(3 * time.Second):
		t.Fatal("first compensation failure did not reach retry state")
	}
	assertResidentCompensationPending(t, scenario, false)

	closed := make(chan struct{})
	go func() {
		scenario.srv.Close()
		close(closed)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !scenario.srv.closed.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !scenario.srv.closed.Load() {
		t.Fatal("server did not enter closing state")
	}
	allowRetry()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked on a journaled resident compensation")
	}

	assertResidentCompensationPending(t, scenario, true)
	scenario.th.mu.Lock()
	deferred := scenario.th.compensationDeferred
	leaseHeld := scenario.th.executionLease != nil
	scenario.th.mu.Unlock()
	if !deferred || leaseHeld {
		t.Fatalf("close state deferred=%t lease=%t, want durable handoff with OS lease released", deferred, leaseHeld)
	}

	restarted := New(scenario.rt, &lockedBuffer{})
	t.Cleanup(restarted.Close)
	if restarted.startupErr != nil {
		t.Fatalf("restart recovery failed: %v", restarted.startupErr)
	}
	waitForResidentCompensationJournalEmpty(t, scenario.rt.SessionDir)
	assertResidentCompensationSettledAfterRestart(t, scenario)
	if got := residentAdmissionMatchingRequests(scenario.client, scenario.pullText, scenario.pushText); len(got) != 0 {
		t.Fatalf("boot recovery replayed a model turn: %d requests", len(got))
	}
}

func TestResidentAdmissionCompensationHandoffUnblocksLivePeer(t *testing.T) {
	scenario := newResidentCompensationScenario(t)
	peer := New(scenario.rt, &lockedBuffer{})
	t.Cleanup(peer.Close)
	if peer.startupErr != nil {
		t.Fatal(peer.startupErr)
	}
	peer.startResidentCompensationRecovery()
	retryPaused := make(chan struct{}, 1)
	retryAllowed := make(chan struct{})
	var allowOnce sync.Once
	allowRetry := func() { allowOnce.Do(func() { close(retryAllowed) }) }
	t.Cleanup(func() {
		allowRetry()
		scenario.srv.Close()
	})
	scenario.srv.resolveResidentCompensationForTest = func(session.ResidentAdmissionCompensation) error {
		return errors.New("injected persistent resident compensation failure")
	}
	scenario.srv.beforeResidentCompensationRetryForTest = func(phase string, _ int, _ error) {
		if phase == "resolve" {
			retryPaused <- struct{}{}
			<-retryAllowed
		}
	}

	scenario.srv.kickResidentAgent(scenario.residentID)
	select {
	case <-retryPaused:
	case <-time.After(3 * time.Second):
		t.Fatal("owner did not reach resident compensation retry")
	}
	// A recovery probe must not touch the journal while the owner still holds
	// the durable thread lease. No manual resident kick is issued: the watcher
	// itself must retain the recovery wake-up across the handoff.
	peer.recoverPendingResidentCompensations()
	assertResidentCompensationPending(t, scenario, false)

	closed := make(chan struct{})
	go func() {
		scenario.srv.Close()
		close(closed)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for !scenario.srv.closed.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	allowRetry()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("owner Close did not hand compensation to the journal")
	}

	req := waitForResidentAdmissionRequest(t, scenario.client, scenario.pullText, scenario.pushText)
	assertResidentAdmissionRequestOrder(t, req, scenario.pullText, scenario.pushText)
	waitForResidentCompensationJournalEmpty(t, scenario.rt.SessionDir)
	waitForResidentAdmissionReceipts(t, scenario.rt.SessionDir, scenario.groupID, scenario.residentID, []int{scenario.pullSeq})
	assertStableResidentAdmissionRequestCount(t, scenario.client, []string{scenario.pullText, scenario.pushText}, 1)
}

func TestResolvedResidentCompensationWakeSurvivesOwnerClose(t *testing.T) {
	scenario := newResidentCompensationScenario(t)
	peer := New(scenario.rt, &lockedBuffer{})
	if peer.startupErr != nil {
		t.Fatal(peer.startupErr)
	}
	t.Cleanup(peer.Close)

	resolved := make(chan struct{})
	allowResolveReturn := make(chan struct{})
	var resolveOnce sync.Once
	scenario.srv.resolveResidentCompensationForTest = func(pending session.ResidentAdmissionCompensation) error {
		if err := session.ResolveResidentAdmissionCompensation(scenario.rt.SessionDir, pending); err != nil {
			return err
		}
		resolveOnce.Do(func() { close(resolved) })
		<-allowResolveReturn
		return nil
	}
	var allowOnce sync.Once
	allowReturn := func() { allowOnce.Do(func() { close(allowResolveReturn) }) }
	t.Cleanup(func() {
		allowReturn()
		scenario.srv.Close()
	})

	scenario.srv.kickResidentAgent(scenario.residentID)
	select {
	case <-resolved:
	case <-time.After(3 * time.Second):
		t.Fatal("owner did not resolve resident compensation")
	}
	if journal, err := session.PendingResidentAdmissionCompensations(scenario.rt.SessionDir); err != nil || len(journal) != 0 {
		t.Fatalf("resolved compensation journal = %+v, %v", journal, err)
	}
	if wakes, err := session.PendingResidentWakeIntents(scenario.rt.SessionDir); err != nil || len(wakes) != 1 || wakes[0].ParticipantID != scenario.residentID {
		t.Fatalf("durable resident wake after resolve = %+v, %v", wakes, err)
	}

	closed := make(chan struct{})
	go func() {
		scenario.srv.Close()
		close(closed)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for !scenario.srv.closed.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !scenario.srv.closed.Load() {
		t.Fatal("owner did not enter Close")
	}
	allowReturn()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("owner Close did not finish after compensation resolved")
	}

	req := waitForResidentAdmissionRequest(t, scenario.client, scenario.pullText, scenario.pushText)
	assertResidentAdmissionRequestOrder(t, req, scenario.pullText, scenario.pushText)
	waitForResidentWakeIntentsEmpty(t, scenario.rt.SessionDir)
	waitForResidentAdmissionReceipts(t, scenario.rt.SessionDir, scenario.groupID, scenario.residentID, []int{scenario.pullSeq})
	assertStableResidentAdmissionRequestCount(t, scenario.client, []string{scenario.pullText, scenario.pushText}, 1)
}

func TestResidentAdmissionCompensationRecoveryMultiplePeersRunsOnce(t *testing.T) {
	scenario := newResidentCompensationScenario(t)
	peers := []*Server{
		New(scenario.rt, &lockedBuffer{}),
		New(scenario.rt, &lockedBuffer{}),
	}
	for _, peer := range peers {
		peer := peer
		if peer.startupErr != nil {
			t.Fatal(peer.startupErr)
		}
		peer.startResidentCompensationRecovery()
		t.Cleanup(peer.Close)
	}

	retryPaused := make(chan struct{}, 1)
	retryAllowed := make(chan struct{})
	var allowOnce sync.Once
	allowRetry := func() { allowOnce.Do(func() { close(retryAllowed) }) }
	t.Cleanup(func() {
		allowRetry()
		scenario.srv.Close()
	})
	scenario.srv.resolveResidentCompensationForTest = func(session.ResidentAdmissionCompensation) error {
		return errors.New("injected persistent resident compensation failure")
	}
	scenario.srv.beforeResidentCompensationRetryForTest = func(phase string, _ int, _ error) {
		if phase == "resolve" {
			retryPaused <- struct{}{}
			<-retryAllowed
		}
	}

	scenario.srv.kickResidentAgent(scenario.residentID)
	select {
	case <-retryPaused:
	case <-time.After(3 * time.Second):
		t.Fatal("owner did not reach resident compensation retry")
	}
	for _, peer := range peers {
		peer.recoverPendingResidentCompensations()
	}
	assertResidentCompensationPending(t, scenario, false)

	closed := make(chan struct{})
	go func() {
		scenario.srv.Close()
		close(closed)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for !scenario.srv.closed.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	allowRetry()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("owner Close did not hand compensation to the journal")
	}

	req := waitForResidentAdmissionRequest(t, scenario.client, scenario.pullText, scenario.pushText)
	assertResidentAdmissionRequestOrder(t, req, scenario.pullText, scenario.pushText)
	waitForResidentCompensationJournalEmpty(t, scenario.rt.SessionDir)
	waitForResidentAdmissionReceipts(t, scenario.rt.SessionDir, scenario.groupID, scenario.residentID, []int{scenario.pullSeq})
	assertStableResidentAdmissionRequestCount(t, scenario.client, []string{scenario.pullText, scenario.pushText}, 1)
}

func TestResidentAdmissionCompensationRecoveryStopsWithServerClose(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	srv := New(rt, &lockedBuffer{})
	srv.startResidentCompensationRecovery()
	if srv.residentCompensationDone == nil {
		t.Fatal("resident compensation recovery watcher did not start")
	}

	closed := make(chan struct{})
	go func() {
		srv.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not wait for resident compensation recovery watcher to stop")
	}
	select {
	case <-srv.residentCompensationDone:
	default:
		t.Fatal("Close returned before resident compensation recovery watcher stopped")
	}
}

func TestThreadMutationLeaseResolvesResidentCompensationBarrier(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	srv := New(rt, &lockedBuffer{})
	t.Cleanup(srv.Close)
	sess, err := session.CreateWithMetadata(rt.SessionDir, "mutation-compensation-barrier", rt.RootDir)
	if err != nil {
		t.Fatal(err)
	}
	pending := session.ResidentAdmissionCompensation{
		ID:            "mutation-compensation-intent",
		ParticipantID: "mutation-participant",
		ThreadID:      sess.ID,
		CreatedAt:     time.Now().UTC(),
	}
	if err := session.SaveResidentAdmissionCompensation(rt.SessionDir, pending); err != nil {
		t.Fatal(err)
	}
	lease, err := srv.tryAcquireThreadMutationLease(sess.ID)
	if err != nil {
		t.Fatalf("mutation lease did not resolve compensation barrier: %v", err)
	}
	releaseThreadMutationLease(sess.ID, lease)
	if journal, err := session.PendingResidentAdmissionCompensations(rt.SessionDir); err != nil || len(journal) != 0 {
		t.Fatalf("mutation lease left compensation journal = %+v, %v", journal, err)
	}
}

type residentCompensationScenario struct {
	srv        *Server
	rt         *runtime.Session
	client     *fakeClient
	th         *threadState
	residentID string
	groupID    string
	pullSeq    int
	pullText   string
	pushText   string
	pushID     string
	attemptID  string
}

func newResidentCompensationScenario(t *testing.T) residentCompensationScenario {
	t.Helper()
	client := &fakeClient{response: providersResponse("compensation handled")}
	rt := newTestRuntime(t, client)
	srv := New(rt, &lockedBuffer{})
	ownerID := saveNamedParticipant(t, rt, "Compensation Owner", "lead", "")
	residentID := saveNamedParticipant(t, rt, "Compensation Worker", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	for _, participantID := range []string{ownerID, residentID} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, participantID); err != nil {
			t.Fatal(err)
		}
	}

	base := time.Now().UTC().Add(-time.Minute)
	anchorSeq := appendResidentAdmissionSourceMessage(t, rt.SessionDir, groupID, "participant", ownerID, "compensation-anchor", base)
	if err := session.MarkMessageSeen(rt.SessionDir, groupID, anchorSeq, residentID, session.SeenStatusCompleted, "", base); err != nil {
		t.Fatal(err)
	}
	pullText := "compensation-pull"
	pullSeq := appendResidentAdmissionSourceMessage(t, rt.SessionDir, groupID, "participant", ownerID, pullText, base.Add(time.Second))

	open := createStoredOpenThreadForTest(t, srv, groupID, ownerID, "compensation-task", anchorSeq)
	task, err := session.EscalateConversationThread(rt.SessionDir, open.ID, ownerID, "compensation retry task")
	if err != nil {
		t.Fatal(err)
	}
	task, err = session.SetConversationThreadPlan(rt.SessionDir, task.ID, []session.TaskPiece{{
		ID: "compensation-node", Title: "compensation push task", Assignee: residentID, Status: session.TaskPiecePending,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetConversationThreadExecState(rt.SessionDir, task.ID, session.ExecStateExecuting); err != nil {
		t.Fatal(err)
	}
	attempt, _, err := session.ReserveTaskAttempt(rt.SessionDir, task.ID, "compensation-node", residentID)
	if err != nil {
		t.Fatal(err)
	}
	pushText := "compensation-push"
	pushID := "compensation-push-envelope"
	push := MessageEnvelope{
		ID: pushID, SourceThreadID: groupID, SourceTitle: "compensation group",
		SourceSubthreadID: task.ID, TaskAttemptID: attempt.ID, TaskNodeID: "compensation-node",
		SenderKind: "system", SenderName: "task plan", Text: pushText,
		CreatedAt: base.Add(2 * time.Second),
	}
	payload, err := json.Marshal(push)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.EnqueueResidentEnvelope(rt.SessionDir, session.ResidentEnvelope{
		ID: pushID, ParticipantID: residentID, EnvelopeJSON: payload, CreatedAt: push.CreatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	th, err := srv.ensureResidentDMThread(residentID)
	if err != nil {
		t.Fatal(err)
	}
	srv.out = &failOnceTurnStartedWriter{failErr: errors.New("injected turn-start notification failure")}
	return residentCompensationScenario{
		srv: srv, rt: rt, client: client, th: th, residentID: residentID,
		groupID: groupID, pullSeq: pullSeq, pullText: pullText, pushText: pushText,
		pushID: pushID, attemptID: attempt.ID,
	}
}

func assertResidentCompensationPending(t *testing.T, scenario residentCompensationScenario, wantDeferred bool) {
	t.Helper()
	journal, err := session.PendingResidentAdmissionCompensations(scenario.rt.SessionDir)
	if err != nil || len(journal) != 1 {
		t.Fatalf("compensation journal = %+v, %v; want one intent", journal, err)
	}
	scenario.th.mu.Lock()
	running := scenario.th.running
	turnID := scenario.th.currentTurn
	leaseHeld := scenario.th.executionLease != nil
	deferred := scenario.th.compensationDeferred
	scenario.th.mu.Unlock()
	if !running || turnID == "" || leaseHeld == wantDeferred || deferred != wantDeferred {
		t.Fatalf("pending compensation state running=%t turn=%q lease=%t deferred=%t (want %t)", running, turnID, leaseHeld, deferred, wantDeferred)
	}
	probe, acquired, err := session.TryAcquireThreadExecutionLease(scenario.rt.SessionDir, scenario.th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if acquired != wantDeferred {
		if acquired {
			_ = probe.Release()
		}
		t.Fatalf("execution lease acquired=%t, want %t for deferred=%t", acquired, wantDeferred, wantDeferred)
	}
	if acquired {
		if err := probe.Release(); err != nil {
			t.Fatal(err)
		}
	}
	if pending, err := session.PendingResidentEnvelopes(scenario.rt.SessionDir, scenario.residentID, 0); err != nil || len(pending) != 0 {
		t.Fatalf("admitted push during compensation = %+v, %v; want consumed until rollback commits", pending, err)
	}
	marks, err := session.ListMessageMarks(scenario.rt.SessionDir, scenario.groupID)
	if err != nil {
		t.Fatal(err)
	}
	foundInProgress := false
	for _, mark := range marks {
		if mark.Seq == scenario.pullSeq && mark.ParticipantID == scenario.residentID && mark.Kind == session.MessageMarkKindSeen {
			foundInProgress = mark.Status == session.SeenStatusInProgress
		}
	}
	if !foundInProgress {
		t.Fatalf("pull receipt was changed before compensation committed: %+v", marks)
	}
	attempt, err := session.TaskAttemptByID(scenario.rt.SessionDir, scenario.attemptID)
	if err != nil || attempt.Status != session.TaskAttemptRunning {
		t.Fatalf("task attempt during compensation = %+v, %v; want running", attempt, err)
	}
	if got := residentAdmissionMatchingRequests(scenario.client, scenario.pullText, scenario.pushText); len(got) != 0 {
		t.Fatalf("provider ran while compensation was pending: %d requests", len(got))
	}
}

func waitForResidentCompensationJournalEmpty(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		pending, err := session.PendingResidentAdmissionCompensations(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("resident compensation journal did not clear")
}

func waitForResidentWakeIntentsEmpty(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		pending, err := session.PendingResidentWakeIntents(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("resident wake intents did not clear")
}

func assertResidentCompensationSettledAfterRestart(t *testing.T, scenario residentCompensationScenario) {
	t.Helper()
	db, err := session.OpenStore(scenario.rt.SessionDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var consumedAt, expiredAt any
	if err := db.QueryRow(`SELECT consumed_at, expired_at FROM resident_inbox WHERE id = ?`, scenario.pushID).Scan(&consumedAt, &expiredAt); err != nil {
		t.Fatal(err)
	}
	if consumedAt != nil || expiredAt == nil {
		t.Fatalf("recovered push consumed_at=%v expired_at=%v, want preserved then boot-settled", consumedAt, expiredAt)
	}
	marks, err := session.ListMessageMarks(scenario.rt.SessionDir, scenario.groupID)
	if err != nil {
		t.Fatal(err)
	}
	for _, mark := range marks {
		if mark.Seq == scenario.pullSeq && mark.ParticipantID == scenario.residentID && mark.Kind == session.MessageMarkKindSeen {
			t.Fatalf("prelaunch receipt survived boot recovery: %+v", mark)
		}
	}
	attempt, err := session.TaskAttemptByID(scenario.rt.SessionDir, scenario.attemptID)
	if err != nil || attempt.Status != session.TaskAttemptInterrupted {
		t.Fatalf("recovered attempt after boot settlement = %+v, %v; want interrupted", attempt, err)
	}
}

func appendResidentAdmissionSourceMessage(t *testing.T, dir, threadID, role, participantID, content string, at time.Time) int {
	t.Helper()
	seq, err := session.AppendHistoryRecordReturningSeq(dir, threadID, session.HistoryRecord{
		Role: role, ParticipantID: participantID, PostKind: "result", Content: content, At: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

func setResidentAdmissionAttemptStatus(t *testing.T, dir, attemptID, status string) {
	t.Helper()
	db, err := session.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	startedAt := int64(0)
	if status == session.TaskAttemptRunning {
		startedAt = time.Now().UTC().UnixMilli()
	}
	if _, err := db.Exec(`UPDATE task_attempts SET status = ?, started_at = ? WHERE id = ?`, status, startedAt, attemptID); err != nil {
		t.Fatal(err)
	}
}

func residentAdmissionMatchingRequests(client *fakeClient, needles ...string) []providers.ChatRequest {
	client.mu.Lock()
	defer client.mu.Unlock()
	var matched []providers.ChatRequest
	for _, req := range client.requests {
		payload := residentAdmissionRequestPayload(req)
		all := true
		for _, needle := range needles {
			if !strings.Contains(payload, needle) {
				all = false
				break
			}
		}
		if all {
			matched = append(matched, req)
		}
	}
	return matched
}

func residentAdmissionRequestPayload(req providers.ChatRequest) string {
	var parts []string
	for _, msg := range req.Messages {
		if msg.Role == "user" {
			parts = append(parts, msg.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func waitForResidentAdmissionRequest(t *testing.T, client *fakeClient, needles ...string) providers.ChatRequest {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		matched := residentAdmissionMatchingRequests(client, needles...)
		if len(matched) > 0 {
			return matched[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for resident request containing %q", needles)
	return providers.ChatRequest{}
}

func assertResidentAdmissionRequestOrder(t *testing.T, req providers.ChatRequest, ordered ...string) {
	t.Helper()
	payload := residentAdmissionRequestPayload(req)
	previous := -1
	for _, value := range ordered {
		if count := strings.Count(payload, value); count != 1 {
			t.Fatalf("request contains %q %d times, want once:\n%s", value, count, payload)
		}
		index := strings.Index(payload, value)
		if index <= previous {
			t.Fatalf("request order for %q is wrong:\n%s", ordered, payload)
		}
		previous = index
	}
}

func waitForResidentAdmissionReceipts(t *testing.T, dir, threadID, participantID string, seqs []int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		marks, err := session.ListMessageMarks(dir, threadID)
		if err != nil {
			t.Fatal(err)
		}
		completed := make(map[int]bool, len(seqs))
		for _, mark := range marks {
			if mark.ParticipantID == participantID && mark.Kind == session.MessageMarkKindSeen && mark.Status == session.SeenStatusCompleted {
				completed[mark.Seq] = true
			}
		}
		all := true
		for _, seq := range seqs {
			all = all && completed[seq]
		}
		if all {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("receipts for seqs %v did not complete", seqs)
}

func assertResidentAdmissionBatchHistory(t *testing.T, srv *Server, participantID string, contents []string, want int) {
	t.Helper()
	_, history := findDMHistoryIfExists(t, srv, participantID)
	matched := 0
	for _, rec := range history {
		if rec.Role != "user" || len(rec.EnvelopeMeta) == 0 {
			continue
		}
		all := true
		for _, content := range contents {
			if strings.Count(rec.Content, content) != 1 {
				all = false
				break
			}
		}
		if all {
			matched++
		}
	}
	if matched != want {
		t.Fatalf("resident batch history count = %d, want %d; history=%+v", matched, want, history)
	}
}

func assertStableResidentAdmissionRequestCount(t *testing.T, client *fakeClient, needles []string, want int) {
	t.Helper()
	deadline := time.Now().Add(threadExecutionLeaseRetryDelay + 150*time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := len(residentAdmissionMatchingRequests(client, needles...)); got != want {
		t.Fatalf("matching resident requests = %d, want %d", got, want)
	}
}
