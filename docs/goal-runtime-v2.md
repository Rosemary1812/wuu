# Goal Runtime v2 Plan

Status: staged implementation. The design is not complete; the initial
state model, JSON store, continuation decision runtime, usage accounting,
idle app-server continuation path, model-facing tool wiring, app-server user
controls, and desktop composer control surface now exist. Each `ThreadRuntime`
owns one GoalRuntime instance.

This document records the intended Goal redesign before the runtime work starts.
It exists to keep future changes pointed at the same product model instead of
adding local patches around the current Goal store.

## User Outcome

When a user starts a Goal, Wuu should keep pushing that objective forward until
one of these states is true:

- the objective is actually complete
- the user pauses, cancels, clears, or edits the Goal
- the Goal reaches a token, time, or usage limit
- the same real blocker has repeated enough times that the Goal is genuinely
  blocked

The model may stop after any individual turn. That is normal. The runtime, not
the model's memory or a desktop timer, should decide whether an active Goal can
continue with another turn.

## Current State

Wuu already has useful Goal pieces:

- `internal/goalruntime` defines the initial Goal v2 runtime state model,
  status ownership rules, budget accounting, blocked-audit threshold, and
  thread-scoped JSON store. It also has a runtime owner that can decide
  whether an active Goal is allowed to continue and can account usage only when
  the current Goal is active. It is attached to `internal/runtime.ThreadRuntime`,
  and app-server turns now account active Goal usage when a turn stops.
- `internal/appserver` can now start an internal continuation turn after a
  successful turn completes, after queued user work and agent-completion work
  have had priority. The continuation prompt is injected through request-only
  context, not persisted as a fake user message. Budget-limited, paused,
  blocked, complete, cancelled, read-only, busy, queued-user, and queued-agent
  states stop automatic continuation.
- `goal/active-summary` now prefers the thread GoalRuntime over the older
  durable ledger and includes runtime status, stop reason, usage, blocker, and
  recent ledger progress. App-server user controls can pause, resume, edit,
  cancel, or clear the runtime Goal while keeping legacy ledger evidence from
  reappearing as a fake active Goal.
- The model-visible `start_goal`, `update_goal`, `complete_goal`, and
  `goal_status` tools now attach to the thread GoalRuntime when one is
  available. The older durable Goal ledger remains as workflow/subagent
  evidence, while the runtime Goal is the source for auto-continuation status.
- `internal/goal` stores durable Goal state in `state.json`, `events.jsonl`,
  artifacts, and markdown views.
- `internal/tools/tool_goal.go` exposes `start_goal`, `update_goal`,
  `complete_goal`, and `goal_status`.
- `internal/tools/tool_workflow.go` can create a workflow-owned Goal or bind a
  workflow run to a broader Goal.
- `internal/tools/tool_agents.go` and `internal/goal/agentcontrol_sink.go` let
  subagent reports update Goal state.
- `internal/runtime/session.go` can create scheduled Goal state for cron-driven
  work.
- `internal/appserver/goal_handlers.go` exposes Goal snapshots, worktree
  review/cleanup/rollback/merge, approval resolution, active summary, cancel,
  and text update endpoints.
- `desktop/src/renderer/ComposerGoalStrip.tsx` shows the thread runtime Goal
  in the composer surface with status/progress/usage detail and pause, resume,
  edit, clear, and cancel controls backed by app-server IPC.

The current design is closer to a durable task ledger than to a thread-level
runtime objective. Its state model includes task-run concepts such as steps,
approvals, worktrees, verification policies, retry policy, progress, decisions,
failures, artifacts, modified files, and test results. That is valuable as an
execution ledger, but it is not the missing continuation loop.

The main remaining gap is end-to-end product-path verification and cleanup of
the old evidence model boundary. The older durable Goal ledger still exists as
a broad evidence model, so future work should keep clarifying which fields
belong to runtime state and which belong to execution evidence.

The app-server path now has product-path tests for positive idle
auto-continuation plus negative gates for queued user work, queued agent
completion work, read-only threads, paused, blocked, budget-limited,
usage-limited, complete, and cancelled Goals. Wuu currently has `update_plan`
as a planning tool, not a separate thread Plan mode; review/subagent threads
are represented as read-only threads in the app-server gate.

Current model-visible semantics have been narrowed, but the old evidence model
is still visible:

- `update_goal` can still write progress, decisions, and failures into the
  durable ledger. Model-owned status changes are limited to blocker reporting;
  when a runtime Goal exists, the runtime applies the repeated-blocker
  threshold before the Goal becomes blocked.
- `complete_goal` now completes the active runtime Goal when one exists, but
  prompts and UI still need to keep teaching that completion requires the
  original user-visible objective to be actually done.
- `goal_status` now includes the runtime Goal when one exists, while still
  exposing the broader workspace/system snapshot.

These capabilities are now part of the closed loop, but the UI and older
evidence fields still need cleanup.

## Target Model

Goal v2 should make Goal a thread-scoped active objective owned by Go core
runtime.

Core invariants:

- A thread has at most one unfinished Goal.
- `Active` is the only status that can auto-continue.
- The model can create, read, complete, or block the current Goal.
- The model cannot pause, resume, cancel, clear, edit, budget-limit, or
  usage-limit a Goal. Those transitions belong to the user or system.
- `complete` means the original user-visible objective is done and verified.
- `blocked` means the same blocker has repeated across enough Goal turns and
  the agent cannot make meaningful progress without user input or external
  state change.
- Workflow runs, subagents, and cron tasks can contribute progress, failures,
  artifacts, and evidence, but they do not own automatic continuation.

Recommended v2 statuses:

- `active`
- `paused`
- `blocked`
- `usage_limited`
- `budget_limited`
- `complete`
- `cancelled` or `cleared` for user-controlled termination, depending on the
  final protocol shape

Existing ledger details can remain as execution evidence, but the active
thread Goal state should be a small runtime object: objective, status, budget,
tokens used, elapsed time, turn counters, blocker audit, created/updated
timestamps, and optional links to the broader ledger.

## Closed Loop

The intended loop is:

1. User creates or resumes a Goal.
2. The app-server/session creates or restores the thread runtime.
3. GoalRuntime records the active Goal for that thread.
4. A user turn or automatic continuation turn runs through the normal model and
   tool loop.
5. Turn lifecycle hooks account for token/time usage and tool progress.
6. The turn reaches final, error, or interrupt.
7. Runtime marks the thread idle after queued user/client work is considered.
8. If the Goal is still active and the thread is safe to use, GoalRuntime
   injects internal continuation context and starts the next turn.
9. UI and protocol surfaces show whether the Goal is running, paused,
   continued, blocked, complete, or waiting for user/system action.

The continuation gate must reject automatic work when:

- there is already an active turn
- a user or client-triggered turn is queued
- the thread is read-only or otherwise unavailable
- the current mode is Plan/review or another non-execution mode
- the Goal is not active
- token, time, usage, or loop limits are exceeded
- provider/tool protocol state is not safe for another turn

This gate belongs in Go core/app-server runtime, not Electron renderer or main.

## Implementation Order

1. Audit and document the current Goal system.
   This document is the first pass. Keep it updated when the implementation
   proves or disproves an assumption.

2. Define the Goal v2 product model and state machine.
   Decide the runtime-owned state fields, allowed transitions, and migration
   relationship to the existing `internal/goal` ledger.

3. Add a GoalRuntime foundation in Go core.
   It should be attached to thread/session runtime lifecycle, not to desktop.
   It needs hooks for turn start, tool finish, turn stop, turn error/abort,
   thread resume, and thread idle.

4. Add accounting.
   Attribute token usage, elapsed time, and turn counts to the active Goal.
   Enforce budget-limited and usage-limited stop conditions before adding
   open-ended continuation. Done for app-server turns.

5. Add the idle continuation gate.
   Implement a shared internal path that starts a model turn only when the
   thread is idle and no user/client work is waiting. The injected context
   should be internal steering, not a fake user message. Done for successful
   app-server turns, queued user work, queued agent-completion work,
   read-only/busy state, and active-status gating.

6. Tighten model-facing tools and guidance.
   Prefer a small contract such as `get_goal`, `create_goal`, and
   `update_goal(status=complete|blocked)`, or keep existing names only if their
   semantics are narrowed to the same contract. Existing names are now wired to
   GoalRuntime in app-server threads: `start_goal` creates the active runtime
   Goal, `complete_goal` completes it, and `update_goal(status=blocked)` records
   blocker audits instead of letting the model directly force arbitrary status
   changes. Outside runtime-backed threads, `update_goal(kind=status)` is also
   limited to `status=blocked` so the older durable ledger cannot be used as a
   fake stop/complete path.
   Prompt guidance still needs to be audited against this narrower contract.

7. Reconnect workflows and subagents as evidence producers.
   Keep workflow/subagent Goal updates, but make them write progress, reports,
   failures, and artifacts into the Goal evidence path instead of owning the
   continuation loop.

8. Expand app-server and desktop controls.
   The UI should show active Goal text, status, elapsed time, recent progress,
   and stop reason. User controls should cover pause, resume, edit, clear or
   cancel, without moving Goal logic into Electron. Done for the composer
   surface and app-server IPC; larger Goal history/detail surfaces still need a
   separate product decision.

9. Verify through the real product path.
   Unit tests should cover transitions and accounting. Integration or protocol
   probes should prove active Goal auto-continuation and negative cases such as
   paused Goal, queued user work, completed Goal, and budget-limited Goal.

## Codex Reference Map

Use Codex as a close reference for problem decomposition, not as source code to
copy blindly.

Start with these files:

- `thirdparty/codex/codex-rs/ext/goal/src/extension.rs`
  - thread lifecycle hooks
  - turn lifecycle hooks
  - tool lifecycle accounting
  - idle continuation entry point
- `thirdparty/codex/codex-rs/ext/goal/src/runtime.rs`
  - active Goal runtime handle
  - resume handling
  - `continue_if_idle`
  - active-turn steering
- `thirdparty/codex/codex-rs/ext/goal/src/tool.rs`
  - `get_goal`, `create_goal`, `update_goal`
  - completion and blocked update flow
- `thirdparty/codex/codex-rs/ext/goal/src/spec.rs`
  - model-facing tool contract
  - blocked and complete semantics
- `thirdparty/codex/codex-rs/ext/goal/src/accounting.rs`
  - token/time accounting state
  - budget-limited handling
- `thirdparty/codex/codex-rs/ext/goal/src/steering.rs`
  - continuation and budget steering items
- `thirdparty/codex/codex-rs/state/src/model/thread_goal.rs`
  - compact thread-scoped Goal state
- `thirdparty/codex/codex-rs/state/src/runtime/goals.rs`
  - persistence and state operations
- `thirdparty/codex/codex-rs/core/src/session/inject.rs`
  - shared idle work gate
- `thirdparty/codex/codex-rs/core/src/tasks/lifecycle.rs`
  - thread idle lifecycle dispatch
- `thirdparty/codex/codex-rs/core/src/tasks/mod.rs`
  - active turn cleanup and idle lifecycle emission
- `thirdparty/codex/codex-rs/app-server/src/request_processors/thread_goal_processor.rs`
  - external app-server goal set/get/clear flow

When adapting these ideas to Wuu, inspect the Wuu app-server turn path first:

- `internal/appserver/turn_handlers.go`
  - `handleTurnStart`
  - `runTurn`
  - queued turn draining
  - agent completion draining
- `internal/runtime/session.go`
  - per-thread runtime construction
  - tool/prompt/model surface construction
- `internal/tools/toolkit.go`
  - current Goal tool registration

## Design Decisions To Preserve

- Go core owns the Goal runtime. Electron only displays and controls it through
  app-server IPC.
- Automatic continuation is a runtime decision, not a model tool.
- Prompt changes are not enough unless backend runtime behavior exists.
- Tool changes are not enough unless prompt guidance and protocol surfaces
  agree with them.
- Workflow and subagent completion is evidence for a Goal, not automatic proof
  that the broader Goal is complete.
- The first implementation should be small, but the product model should not
  preserve the old ledger-only behavior as the primary path.

## Verification Plan

Required positive coverage:

- create an active thread Goal
- run a model turn that ends without completing the Goal
- observe the thread become idle
- observe GoalRuntime start a continuation turn through the same app-server or
  `wuu exec --json` path users rely on

Required negative coverage:

- queued user/client work prevents automatic Goal continuation
- paused Goal does not continue
- completed Goal does not continue
- blocked Goal does not continue
- budget-limited or usage-limited Goal does not continue
- Plan/review mode does not continue; in current Wuu this is represented by
  read-only review/subagent threads rather than a separate thread mode
- provider tool-call/result ordering stays valid after continuation steering

The implementation is not complete until these paths are backed by current
code and tests or protocol probes.
