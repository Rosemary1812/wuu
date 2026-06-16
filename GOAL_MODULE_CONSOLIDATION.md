# Goal / Goal Module Consolidation

## Problem

The P0 goal work added durable goal state, but Wuu already had durable
workflow and harness stores. Without an explicit consolidation boundary, the
system can drift into three parallel task systems:

- `internal/goal`: goal state, phase step, event log, artifacts, failures, test
  results.
- `internal/workflow`: workflow runs, phases, agent runs, team plans,
  checkpoints, memory candidates, final reports, workflow events.
- `internal/harness`: spawned-agent task graph, agent attempts, reports,
  artifacts, queue, harness events.

The target is not to delete existing stores immediately. Existing product
paths depend on them. The target is to make ownership explicit, then migrate
read and write paths toward a single goal-level view.

## Source Of Truth

### `internal/goal`

Owns long-running task state:

- Durable goal/run state.
- Phase state machine at the product level.
- Progress, decisions, failures, and event ledger.
- Verification result and failure feedback.
- Resume/recovery entry point.
- Cross-store snapshot API used by eval, app-server, and desktop.

It must not own:

- Workflow definition parsing.
- Subagent spawning details.
- Worktree git implementation.
- Skill discovery.

### `internal/workflow`

Owns workflow definitions and workflow-local execution records:

- `WORKFLOW.md` / `WORKFLOW.js` discovery and parsing.
- Phase DSL / policy / trigger description.
- Workflow run record as a compatibility projection until migrated.
- Workflow-specific artifacts such as script, plan, team plan, checkpoints, and
  memory candidates.

It should stop growing independent product-level completion logic. When a
workflow needs durable product state, it should sync through `internal/goal`.

### `internal/harness`

Owns spawned-agent execution facts:

- Task queue.
- Agent attempt.
- Structured handoff report.
- Evidence artifacts.
- Harness event log.

It does not decide whether the user-level goal is complete. It reports facts
for `internal/goal` and control-plane consumers.

### `internal/worktree`

Owns all git worktree concerns:

- Lease creation.
- Manifest.
- Status and changed files.
- Diff and merge preview.
- Rollback and cleanup.

No workflow or harness package should reimplement git isolation.

### `internal/skills`

Owns skill discovery, registry, routing, frontmatter metadata, and bundled
skills. Other packages may request routed skills but should not parse skill
files.

### `internal/agentcontrol`

Owns subagent roles and lifecycle:

- `spawn/fork/await/close/list`.
- Role contracts.
- Maker/checker tool filtering.
- Harness recording of spawned-agent facts.

It should not define a separate workflow state machine.

### Control Plane

CLI, app-server, desktop, cron, and eval should use goal APIs to inspect or
drive long-running work. They should not grow new long-running state models.

## Current Duplicates

| Concern | Current places | Consolidation |
| --- | --- | --- |
| Run status | `goal.Status`, `workflow.RunState`, `harness.TaskStatus` | `goal.Status` is product state; workflow/harness statuses are projections. |
| Phase status | `goal.Step`, `workflow.PhaseState` | `goal.Step` is product phase; workflow phases remain workflow-local. |
| Agent status | `workflow.AgentRunState`, `harness.TaskStatus` | `agentcontrol` and `harness` own execution facts; goal consumes summary. |
| Artifacts | goal `artifacts/`, workflow plan/script/final-report, harness artifacts/reports | Goal owns final product artifacts; workflow/harness artifacts are evidence refs. |
| Events | goal `events.jsonl`, workflow `events.jsonl`, harness `events.jsonl` | Goal event log is the product replay log; other logs are source evidence. |
| Failure feedback | `goal.failures`, workflow `Error`, harness `Error`/`Blockers` | Goal owns blocker/failure ledger; other stores feed it. |
| Verification | `goal.TestResult`, eval verification, harness report verification strings | Goal owns reusable verification result; eval is benchmark-specific. |
| Worktree | `goal.WorktreeLease`, `harness.WorkspaceLease`, `workflow.AgentRun.WorktreePath`, `worktree.Lease` | `internal/worktree` is the implementation; goal/harness/workflow store references only. |

## Migration Plan

### P0.1: Unified Read Projection

Add a goal-level snapshot API that reads workflow and harness stores and emits
one product-neutral view:

- workflow run summary
- workflow phase summary
- workflow agent summary
- harness task summary
- harness report summary
- warnings from unreadable stores
- attention items such as failed runs, missing reports, open agents, and
  changed-file overlaps

Use this API in eval first. Eval should stop manually walking workflow and
harness stores as separate systems.

Implemented pieces:

- Eval observability calls `goal.SnapshotSystem` and converts the projection to
  compatibility workflow/harness observations.
- Eval trace writes and replays `goal_attention` as its own event type.
- Eval validation now derives workflow issues from goal attention and projected
  workflow compatibility fields through one goal-level validator, so old traces
  keep their diagnostics without making eval a separate state owner.

### P0.2: Failure Sync

When workflow or harness records a terminal failure, add a goal-level failure
entry for the active goal when one is known.

The first implementation is opt-in. Callers must pass an explicit goal store or
failure sink. The system does not discover or guess an active goal from the
workspace.

Implemented pieces:

- `goal.SyncSnapshotFailures` converts workflow/harness snapshot attention
  items into goal failures.
- `goal.Failure` carries `source` and `source_id` so external failures are
  idempotent across repeated syncs.
- `agentcontrol.FailureSink` lets the control plane receive spawned-agent
  failure facts without importing `internal/goal`.
- `goal.NewAgentControlFailureSink` adapts those facts into the goal failure
  view when an explicit goal store is configured.
- `AgentControl` calls the sink for failed harness tasks and for `agent_report`
  submissions with blockers, `stuck`, or `error` outcomes.

### P0.3: Workflow Start Bridge

`start_workflow` should create or attach a goal state record. Workflow run id
and goal id should be linked in metadata. Workflow artifacts become goal
artifacts or evidence refs.

Implemented pieces:

- `workflow.Run` stores `goal_id` and `goal_dir` as compatibility metadata.
- Workflow-created goal state lives at `stateDir/goals/<workflow-run-id>` so
  parallel workflow runs and CLI demos share the same Wuu-managed storage model.
- `start_workflow` reaches this bridge through both concrete creation paths:
  `create_workflow` and `run_workflow`.
- Script workflow completion and failure sync back to the bound goal status for
  foreground runs and best-effort background runs.
- Goal bindings are projected through `goal.SystemSnapshot` and eval
  observability.
- `workflow.Store` exposes an artifact sink and emits workflow artifact facts
  from the shared `WritePlan`, `WriteScript`, and `WriteFinalReport` write
  paths.
- `goal.NewWorkflowArtifactSink` records those facts with
  `Store.RecordExternalArtifact`, so workflow plan/script/final report files
  appear in goal state as external artifact refs instead of living only in the
  workflow run record.
- `tools.Env.WorkflowStore` installs the goal artifact sink for workflow tool
  execution paths.

### P0.4: Harness Report Bridge

Agent reports submitted through `agent_report` should update goal progress,
modified files, artifacts, verification strings, and failures when associated
with a goal.

Implemented pieces:

- `harness.Task` stores `goal_id` and `goal_dir` so each spawned-agent task can
  route handoff facts to the correct goal.
- `workflow.ScriptRuntime` passes the workflow run's goal binding into
  workflow-spawned `SpawnRequest`s.
- `spawn_agent` accepts optional `goal_id` and `goal_dir` so agent-managed
  workflows can pass the binding returned by `start_workflow`.
- `agentcontrol.ReportSink` emits structured `agent_report` handoff facts
  without importing `internal/goal`.
- `goal.AgentControlFailureSink` also implements report sync. When a report has
  `goal_dir`, it updates goal progress, modified files, external artifact refs,
  verification results, next steps, and the existing failure path.
- Runtime installs the goal sink for both report and failure sync. Reports
  without `goal_dir` are ignored by this sink, so ordinary non-workflow agents
  do not write to a guessed goal.
- Harness task goal bindings are projected through `goal.SystemSnapshot` and
  eval observability.

### P1: Control Plane

Expose goal snapshot/status through app-server and desktop. Workflow/harness UI
panels should read through goal-level status first and then drill down to source
stores.

Implemented pieces:

- Local `main` desktop split commits are merged into this goal branch. New goal
  control-plane wiring uses the extracted main/preload/shared protocol
  boundary instead of adding more state to `desktop/src/renderer/App.tsx`.
- App-server exposes `goal/snapshot` with optional `thread_id`.
- `goal/snapshot` reads the workspace `workflow.Store` and, when `thread_id`
  is provided, the thread-scoped `harness.Store` under
  `stateDir/sessions/<thread-id>/harness`.
- If a thread is live, app-server prefers the live `AgentControl` harness
  store; if it is not live, it falls back to the durable harness files. This
  keeps long-running workflow state recoverable after UI or process restart.
- Desktop main process proxies `wuu:goal-snapshot` to app-server.
- Desktop preload exposes `window.wuu.getGoalSnapshot(threadId?)`.
- `desktop/src/shared/protocol.ts` defines the goal snapshot wire types so
  renderer panels can consume goal-level status first.
- Desktop workspace tools include a read-only `Goal` panel that calls
  `getGoalSnapshot(threadId?)` and renders workflow runs, attention items,
  thread-scoped harness tasks, reports, warnings, team counts, agent counts,
  and event counts from the goal projection.
- Scheduled cron triggers initialize a durable goal record under
  `stateDir/goals/cron-goal-*` when they fire. The cron package still owns only
  schedule storage; runtime records trigger execution, metadata, prompt
  artifact, success, and failure through `internal/goal`.
- `goal.ReviewWorktree` exposes a read-only worktree review surface that
  validates the worktree path is inside the managed workspace worktree root,
  then delegates status, diff, and merge preview to `internal/worktree`.
- App-server exposes `goal/worktree/review`, and desktop main/preload proxy it
  as `getGoalWorktreeReview(worktreePath)`. Desktop does not run git diff or
  merge-preview logic itself.
- `goal.CleanupWorktreeIfClean` exposes the first mutation surface with an
  explicit approval gate. It validates the path is inside the managed worktree
  root, requires both `confirm_user_approved` and
  `confirm_remove_clean_worktree`, delegates cleanup to `internal/worktree`,
  and preserves dirty worktrees for review.
- App-server exposes `goal/worktree/cleanup`, and desktop main/preload proxy it
  as `cleanupGoalWorktree(...)` without auto-confirming on the renderer's
  behalf.
- `goal.RollbackWorktree` exposes a second gated mutation. It validates the
  managed worktree root, requires both `confirm_user_approved` and
  `confirm_discard_worktree_changes`, delegates reset/clean to
  `internal/worktree`, and returns before/after dirty state for the control
  plane.
- App-server exposes `goal/worktree/rollback`, and desktop main/preload proxy
  it as `rollbackGoalWorktree(...)` without auto-confirming.
- `internal/worktree.ApplyToTarget` is the sole implementation for applying a
  managed worktree diff into the target repository. It rejects untracked
  worktree files because they are not represented in the tracked git diff.
- `goal.MergeWorktree` exposes the gated merge surface. It requires
  `confirm_user_approved`, `confirm_apply_worktree_diff`, and
  `confirm_target_repo_mutation`, verifies the merge preview, delegates apply
  to `internal/worktree`, and returns the applied file list.
- App-server exposes `goal/worktree/merge`, and desktop main/preload proxy it
  as `mergeGoalWorktree(...)` without auto-confirming.
- `internal/goal` owns a durable human approval queue through
  `RequestApproval` / `ResolveApproval`. Pending approvals live in
  `state.json`, render to `views/approvals.md`, emit `approval_requested` /
  `approval_resolved` events, and move the goal into `needs_human` until the
  last pending approval is resolved.
- `goal.SnapshotSystem` projects workspace goal states and pending approvals
  into the control-plane snapshot. App-server exposes
  `goal/approval/resolve` behind `confirm_user_approved`, and desktop proxies
  it as `resolveGoalApproval(...)`; the Goal panel renders pending approvals
  read-only from the snapshot.

Remaining work: add mutation surfaces behind explicit approval gates, such as
retry queues. Those should still call goal/worktree APIs instead of writing
directly to workflow or harness stores.

### P2: Store Reduction

After all read paths use goal projections and write paths sync to goal, reduce
duplicated workflow/harness product fields. Keep compatibility readers for old
state directories.

## First Code Step

The first implementation step is intentionally narrow:

1. Add `internal/goal.SystemSnapshot`.
2. Add projection helpers that read `workflow.Store` and `harness.Store`.
3. Update eval observability collection to call the goal snapshot API before
   converting to existing eval trace structs.
4. Keep existing workflow/harness files and JSON schemas intact.

This moves consumers toward one goal-level read model without breaking current
workflow or subagent behavior.

## Second Code Step

The second step adds failure feedback without collapsing module ownership:

1. Goal remains the owner of the durable failure ledger.
2. Workflow/harness facts are converted through explicit sync functions or
   sink interfaces.
3. AgentControl emits failure facts but does not depend on goal internals.
4. Repeated sync of the same external failure is deduplicated by source and
   source id.
