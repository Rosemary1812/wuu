# Idle Unread Wake Implementation Plan

> **Execution note:** Use `executing-plans` to implement this plan task-by-task, or another equivalent execution workflow supported by the current agent runtime.

**Goal:** Add a delayed idle wake path so ordinary agent group messages do not leave other named agents permanently unread, without returning to immediate full-roster wakeups.

**Architecture:** Keep the current history-first model. User messages still wake group members immediately; ordinary agent messages without @ remain ambient unread at write time, but schedule a quiet-period sweep that wakes at most one eligible reader through the existing resident drain path. The selected resident pulls normal messages from history, keeps read-cursor / `basis_seq` / held-draft semantics, and gets prompt guidance to consider `inception` for large delayed batches or held drafts without runtime-forcing it.

**Tech Stack:** Go appserver/session runtime, existing `MessageEnvelope` pull inbox, resident prompt text, appserver unit tests, Markdown design docs.

---

### Task 1: Candidate Selection Unit Tests

**Files:**
- Modify: `internal/appserver/resident_router.go`
- Test: `internal/appserver/resident_router_test.go`

**Step 1: Write failing tests**

Add tests for a pure helper that chooses one idle-unread candidate:

```go
func TestIdleUnreadWakeCandidatePrefersMostUnreadAndSkipsIneligible(t *testing.T) {
	candidates := []idleUnreadCandidate{
		{ParticipantID: "ada", UnreadCount: 2},
		{ParticipantID: "bea", UnreadCount: 5},
		{ParticipantID: "cyd", UnreadCount: 9, Busy: true},
		{ParticipantID: "dan", UnreadCount: 7, LastSpeaker: true},
	}
	got, ok := chooseIdleUnreadCandidate(candidates, rand.New(rand.NewSource(1)))
	if !ok || got.ParticipantID != "bea" {
		t.Fatalf("candidate = %+v, %v; want bea", got, ok)
	}
}

func TestIdleUnreadWakeCandidateRandomizesTies(t *testing.T) {
	candidates := []idleUnreadCandidate{
		{ParticipantID: "ada", UnreadCount: 3},
		{ParticipantID: "bea", UnreadCount: 3},
	}
	seen := map[string]bool{}
	for seed := int64(1); seed <= 20; seed++ {
		got, ok := chooseIdleUnreadCandidate(candidates, rand.New(rand.NewSource(seed)))
		if !ok {
			t.Fatal("expected a candidate")
		}
		seen[got.ParticipantID] = true
	}
	if !seen["ada"] || !seen["bea"] {
		t.Fatalf("tie did not randomize across seeds: %v", seen)
	}
}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/appserver -run 'TestIdleUnreadWakeCandidate' -count=1
```

Expected: FAIL because `idleUnreadCandidate` / `chooseIdleUnreadCandidate` do not exist.

**Step 3: Implement minimal pure helper**

In `resident_router.go`, add:

```go
type idleUnreadCandidate struct {
	ParticipantID string
	UnreadCount   int
	Busy          bool
	Draining      bool
	Retired       bool
	LastSpeaker   bool
}

func chooseIdleUnreadCandidate(candidates []idleUnreadCandidate, rng *rand.Rand) (idleUnreadCandidate, bool) {
	var best []idleUnreadCandidate
	maxUnread := 0
	for _, c := range candidates {
		if strings.TrimSpace(c.ParticipantID) == "" || c.UnreadCount <= 0 || c.Busy || c.Draining || c.Retired || c.LastSpeaker {
			continue
		}
		if c.UnreadCount > maxUnread {
			maxUnread = c.UnreadCount
			best = best[:0]
		}
		if c.UnreadCount == maxUnread {
			best = append(best, c)
		}
	}
	if len(best) == 0 {
		return idleUnreadCandidate{}, false
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return best[rng.Intn(len(best))], true
}
```

Add imports for `math/rand` if needed.

**Step 4: Run tests**

Run:

```bash
go test ./internal/appserver -run 'TestIdleUnreadWakeCandidate' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/appserver/resident_router.go internal/appserver/resident_router_test.go
git commit -m "test: define idle unread wake candidate selection"
```

---

### Task 2: Unread Count Builder

**Files:**
- Modify: `internal/appserver/resident_router.go`
- Test: `internal/appserver/resident_router_test.go`

**Step 1: Write failing test**

Add a test that seeds a group with three named members, marks different read watermarks, and checks that candidate counts use main-stream unread chat only:

```go
func TestIdleUnreadWakeCandidatesUseReadWatermarks(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	cyd := saveNamedParticipant(t, rt, "Cyd", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	for _, id := range []string{ada, bea, cyd} {
		if err := session.AddThreadMember(rt.SessionDir, groupID, id); err != nil {
			t.Fatalf("AddThreadMember: %v", err)
		}
	}
	s1 := appendMainChatForIdleWakeTest(t, rt.SessionDir, groupID, "participant", ada, "1")
	s2 := appendMainChatForIdleWakeTest(t, rt.SessionDir, groupID, "participant", bea, "2")
	_ = appendSubthreadChatForIdleWakeTest(t, rt.SessionDir, groupID, "cth-x", ada, "hidden from main")
	if err := session.MarkMessageSeen(rt.SessionDir, groupID, s1, cyd, session.SeenStatusCompleted, "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	candidates := srv.idleUnreadCandidates(groupID, bea)
	got := idleCandidateCounts(candidates)
	if got[ada] != 1 || got[cyd] != 1 {
		t.Fatalf("counts = %v, want Ada=1 Cyd=1", got)
	}
	if _, ok := got[bea]; ok {
		t.Fatalf("last speaker Bea should be skipped: %v", got)
	}
	_ = s2
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/appserver -run 'TestIdleUnreadWakeCandidatesUseReadWatermarks' -count=1
```

Expected: FAIL because helper does not exist.

**Step 3: Implement candidate builder**

Add `idleUnreadCandidates(threadID, lastSpeakerID string) []idleUnreadCandidate`:

- Load explicit group members with `session.ListThreadMembers`.
- Skip last speaker.
- Skip retired participants with `s.participantRetired`.
- Skip busy with `s.participantIsBusy`.
- Skip currently draining with `s.residentDraining`.
- For each remaining participant, read `session.ThreadReadWatermark`.
- Count `session.ChatMessagesSince` records where `ThreadID == ""` and `ParticipantID != participantID`.
- Return candidates with `UnreadCount > 0`.

Keep this helper read-only. It should not enqueue or mark read.

**Step 4: Run test**

Run:

```bash
go test ./internal/appserver -run 'TestIdleUnreadWakeCandidatesUseReadWatermarks|TestIdleUnreadWakeCandidate' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/appserver/resident_router.go internal/appserver/resident_router_test.go
git commit -m "test: count idle unread wake candidates"
```

---

### Task 3: Quiet Timer State

**Files:**
- Modify: `internal/appserver/server.go`
- Modify: `internal/appserver/resident_router.go`
- Test: `internal/appserver/resident_router_test.go`

**Step 1: Write failing scheduling test**

Use a small test-only delay hook so the test does not wait real production delays:

```go
func TestAmbientAgentMessageSchedulesIdleUnreadWake(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	srv.idleUnreadWakeDelayForTest = func(int) time.Duration { return 10 * time.Millisecond }
	t.Cleanup(func() { waitForResidentQuiesce(t, srv) })
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	groupID := startGroupThreadForTest(t, srv)
	session.AddThreadMember(rt.SessionDir, groupID, ada)
	session.AddThreadMember(rt.SessionDir, groupID, bea)

	if err := srv.publishParticipantMessage(groupID, agentcontrol.ParticipantMessage{
		AgentID: ada, ParticipantID: ada, Kind: "result", Text: "1", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	_, history := waitForResidentDMHistory(t, srv, bea, 1)
	meta := findEnvelopeMetaRecord(t, history)
	if meta.SourceThreadID != groupID || meta.SenderParticipantID != ada {
		t.Fatalf("idle wake meta = %+v", meta)
	}
}
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/appserver -run 'TestAmbientAgentMessageSchedulesIdleUnreadWake' -count=1
```

Expected: FAIL because no timer is scheduled.

**Step 3: Add server state**

In `Server`:

```go
idleUnreadWakeMu           sync.Mutex
idleUnreadWakeTimers       map[string]*time.Timer
idleUnreadWakeWaveByThread map[string]int
idleUnreadWakeLastSpeaker  map[string]string
idleUnreadWakeDelayForTest func(wave int) time.Duration
idleUnreadWakeRand         *rand.Rand
```

Initialize maps and rand in `New`.

Add constants:

```go
const (
	idleUnreadWakeBaseDelay = 30 * time.Second
	idleUnreadWakeMaxDelay  = 5 * time.Minute
)
```

Add `idleUnreadWakeDelay(wave int) time.Duration`: wave 0 => 30s, wave 1 => 60s, wave 2 => 120s, capped at 5m; tests can override with `idleUnreadWakeDelayForTest`.

**Step 4: Implement schedule/cancel**

Add:

```go
func (s *Server) scheduleIdleUnreadWake(threadID, lastSpeakerID string)
func (s *Server) resetIdleUnreadWake(threadID string)
func (s *Server) runIdleUnreadWake(threadID string)
```

Rules:

- `scheduleIdleUnreadWake` resets one timer per group thread.
- `resetIdleUnreadWake` stops timer and clears wave/lastSpeaker; call after user messages.
- `runIdleUnreadWake` chooses one candidate and calls `s.kickResidentAgent(candidate.ParticipantID)`.
- If no candidate exists, clear timer state.
- If a candidate is kicked, increment wave and leave future scheduling to the next ambient agent message. Do not recursively schedule when nobody speaks.

**Step 5: Wire scheduling**

In `routeUserMessageToResidents`, after routing or before routing, reset the thread's idle state because human speech is a fresh active window and already immediately wakes members.

In `routeParticipantMessageToResidents`, after route, schedule idle unread wake only when:

- message is non-decline and non-empty;
- source is a group, not DM;
- this is main-stream traffic;
- it is not a human/system sender;
- ordinary routing did not already push due to @/sole-member. Simpler first version: call schedule unconditionally for participant main-stream messages; candidate builder will skip addressed readers that already read, last speaker, busy, etc. Tests can tighten later if duplicate wake appears.

**Step 6: Run tests**

Run:

```bash
go test ./internal/appserver -run 'TestAmbientAgentMessageSchedulesIdleUnreadWake|TestResidentRouterParticipantMessageHonorsMentionsAndRoutesDeepRelays|TestIdleUnreadWake' -count=1
```

Expected: PASS. The older "ambient post must not wake Ada immediately" test should still pass because scheduling is delayed and its fake quiesce assertions may need to distinguish immediate vs delayed. If the old test now observes delayed wake, update it to assert no pending push and use a delay override only in the new test.

**Step 7: Commit**

```bash
git add internal/appserver/server.go internal/appserver/resident_router.go internal/appserver/resident_router_test.go
git commit -m "feat: add delayed idle unread wake"
```

---

### Task 4: Freshness and Held-Draft Regression

**Files:**
- Test: `internal/appserver/held_draft_test.go`
- Modify only if needed: `internal/appserver/resident_speech.go`

**Step 1: Write regression test**

Add a test proving idle-woken speech still uses the read cursor basis and can be held when the room moves:

```go
func TestIdleWokenReplyStillUsesHeldDraftFreshness(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{response: providersResponse("")})
	srv := New(rt, &lockedBuffer{})
	ada := saveNamedParticipant(t, rt, "Ada", "reviewer", "")
	bea := saveNamedParticipant(t, rt, "Bea", "reviewer", "")
	cyd := saveNamedParticipant(t, rt, "Cyd", "reviewer", "")
	groupID := startNamedGroupThreadForTest(t, srv, "idle-held").ID
	for _, id := range []string{ada, bea, cyd} {
		session.AddThreadMember(rt.SessionDir, groupID, id)
	}
	s5 := appendMainChatForIdleWakeTest(t, rt.SessionDir, groupID, "participant", ada, "5")
	if err := session.MarkMessageSeen(rt.SessionDir, groupID, s5, bea, session.SeenStatusCompleted, "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_ = appendMainChatForIdleWakeTest(t, rt.SessionDir, groupID, "participant", cyd, "6")

	posted, err := srv.residentParticipantSpeechForTurn(bea, nil, map[string]bool{groupID: true}).
		PostMessage(context.Background(), "result", "6", groupID, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !posted.Held {
		t.Fatalf("expected stale idle-woken draft to be held, got %+v", posted)
	}
}
```

**Step 2: Run test**

Run:

```bash
go test ./internal/appserver -run 'TestIdleWokenReplyStillUsesHeldDraftFreshness|TestHeldDraft' -count=1
```

Expected: PASS without implementation changes. If it fails, fix only the basis/read cursor plumbing, not the idle wake scheduler.

**Step 3: Commit**

```bash
git add internal/appserver/held_draft_test.go internal/appserver/resident_speech.go
git commit -m "test: preserve held draft freshness for idle wakes"
```

---

### Task 5: Prompt and Tool Result Guidance

**Files:**
- Modify: `internal/appserver/participant_prompt.go`
- Test: `internal/appserver/participant_prompt_test.go`
- Modify: `internal/tools/tool_participant_message.go`
- Test: existing `internal/tools/tool_*` tests if needed

**Step 1: Write failing prompt test**

Extend `TestResidentParticipantSystemPrompt...` assertions to include:

- delayed/idle unread batches are not direct summons;
- read the full batch before responding;
- when a delayed batch has several messages or changed room state, consider `inception`;
- `inception` is model-chosen, not runtime-forced.

Expected snippets:

```go
"Delayed unread messages are a chance to catch up, not a summons"
"If a delayed batch contains several messages or changes your view of the room, consider inception"
```

**Step 2: Update prompt**

In `residentParticipantSystemPrompt`, add concise wording near "How messages reach you" and "Whether to reply":

```text
Some ambient group messages may arrive later through an idle unread wake after the room has gone quiet. Treat that as a chance to catch up, not as a summons. Read the full delayed batch before deciding. If several delayed messages or a held draft change your view of the room, consider using inception to fold the new room state into your working context before posting; the right outcome may still be silence.
```

**Step 3: Tighten held result text**

In `PostMessageTool.Execute` held response, keep the existing `inception` wording but align it with the new contract:

```text
If the arrivals change your view of the room or would otherwise bloat this resident context, consider inception before deciding whether to revise, resend with force=true, or stay silent.
```

**Step 4: Run tests**

Run:

```bash
go test ./internal/appserver -run 'TestResidentParticipantSystemPrompt' -count=1
go test ./internal/tools -run 'Test.*PostMessage|Test.*Participant' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/appserver/participant_prompt.go internal/appserver/participant_prompt_test.go internal/tools/tool_participant_message.go
git commit -m "prompt: guide delayed unread wake context handling"
```

---

### Task 6: Documentation Drift Repair

**Files:**
- Modify: `docs/group-chat-and-task-rail-zh.md`
- Modify: `docs/plans/2026-07-03-resident-named-agents.md`
- Optional: create a short design note if needed under `docs/plans/2026-07-08-idle-unread-wake-design.md`

**Step 1: Update mechanism doc**

Change the message journey to say:

- all visible main-stream messages are persisted first;
- user messages immediately wake members;
- directed pushes (`@`, one-member group, cth/system/task wakes) enqueue resident envelopes and kick;
- ordinary agent main-stream messages leave ambient unread and schedule idle unread wake;
- idle unread wake waits for a quiet period, selects at most one eligible reader, and kicks it so it pulls unread;
- publishing still goes through `basis_seq`/held freshness.

**Step 2: Update resident design doc**

Remove or amend stale language that says every group message wakes every member. Document the current default and the new delayed idle wake:

```text
Human room messages wake immediately. Ambient agent room messages are written once to history and become pullable unread; after the room goes quiet, idle unread wake may kick one eligible reader. This is intentionally delayed and single-reader to avoid thundering herd behavior.
```

**Step 3: Commit**

```bash
git add docs/group-chat-and-task-rail-zh.md docs/plans/2026-07-03-resident-named-agents.md docs/plans/2026-07-08-idle-unread-wake.md
git commit -m "docs: document idle unread wake semantics"
```

---

### Task 7: Final Verification

**Files:**
- No source edits expected.

**Step 1: Focused tests**

Run:

```bash
go test ./internal/appserver -run 'TestIdleUnreadWake|TestAmbientAgentMessageSchedulesIdleUnreadWake|TestResidentRouter|TestHeldDraft|TestIssue2|TestResidentPostMessage' -count=1
```

Expected: PASS.

**Step 2: Broader package tests**

Run:

```bash
go test ./internal/appserver ./internal/session ./internal/tools -count=1
```

Expected: PASS. If `internal/appserver` hits a known asynchronous `t.TempDir` cleanup race, rerun once and inspect whether the failing assertion is unrelated to idle wake behavior.

**Step 3: Commit any verification-only doc/test fixes**

Only if changes were needed:

```bash
git add <changed-files>
git commit -m "test: stabilize idle unread wake verification"
```

**Step 4: Report**

Summarize:

- user-visible behavior: agent ambient messages can resume after quiet periods;
- safety behavior: only one eligible reader is kicked; user messages remain immediate; held draft still protects stale speech;
- verification commands and results.
