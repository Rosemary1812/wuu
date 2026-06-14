# Loop Module Consolidation

## Problem

The P0 loop work added durable `.loop` state, but Wuu already had durable
workflow and harness stores. Without an explicit consolidation boundary, the
system can drift into three parallel task systems:

- `internal/loop`: goal state, phase step, event log, artifacts, failures, test
  results.
- `internal/workflow`: workflow runs, phases, agent runs, team plans,
  checkpoints, memory candidates, final reports, workflow events.
- `internal/harness`: spawned-agent task graph, agent attempts, reports,
  artifacts, queue, harness events.

The target is not to delete existing stores immediately. Existing product
paths depend on them. The target is to make ownership explicit, then migrate
read and write paths toward a single loop-level view.

## Source Of Truth

### `internal/loop`

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
workflow needs durable product state, it should sync through `internal/loop`.

### `internal/harness`

Owns spawned-agent execution facts:

- Task queue.
- Agent attempt.
- Structured handoff report.
- Evidence artifacts.
- Harness event log.

It does not decide whether the user-level loop is complete. It reports facts
for `internal/loop` and control-plane consumers.

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

CLI, app-server, desktop, cron, and eval should use loop APIs to inspect or
drive long-running work. They should not grow new long-running state models.

## Current Duplicates

| Concern | Current places | Consolidation |
| --- | --- | --- |
| Run status | `loop.Status`, `workflow.RunState`, `harness.TaskStatus` | `loop.Status` is product state; workflow/harness statuses are projections. |
| Phase status | `loop.Step`, `workflow.PhaseState` | `loop.Step` is product phase; workflow phases remain workflow-local. |
| Agent status | `workflow.AgentRunState`, `harness.TaskStatus` | `agentcontrol` and `harness` own execution facts; loop consumes summary. |
| Artifacts | `.loop/artifacts`, workflow plan/script/final-report, harness artifacts/reports | Loop owns final product artifacts; workflow/harness artifacts are evidence refs. |
| Events | `.loop/events.jsonl`, workflow `events.jsonl`, harness `events.jsonl` | Loop event log is the product replay log; other logs are source evidence. |
| Failure feedback | `loop.failures`, workflow `Error`, harness `Error`/`Blockers` | Loop owns blocker/failure ledger; other stores feed it. |
| Verification | `loop.TestResult`, eval verification, harness report verification strings | Loop owns reusable verification result; eval is benchmark-specific. |
| Worktree | `loop.WorktreeLease`, `harness.WorkspaceLease`, `workflow.AgentRun.WorktreePath`, `worktree.Lease` | `internal/worktree` is the implementation; loop/harness/workflow store references only. |

## Migration Plan

### P0.1: Unified Read Projection

Add a loop-level snapshot API that reads workflow and harness stores and emits
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

### P0.2: Failure Sync

When workflow or harness records a terminal failure, add a loop-level failure
entry for the active loop when one is known.

The first implementation is opt-in. Callers must pass an explicit loop store or
failure sink. The system does not discover or guess an active loop from the
workspace.

Implemented pieces:

- `loop.SyncSnapshotFailures` converts workflow/harness snapshot attention
  items into loop failures.
- `loop.Failure` carries `source` and `source_id` so external failures are
  idempotent across repeated syncs.
- `agentcontrol.FailureSink` lets the control plane receive spawned-agent
  failure facts without importing `internal/loop`.
- `loop.NewAgentControlFailureSink` adapts those facts into `.loop/failures.md`
  when an explicit loop store is configured.
- `AgentControl` calls the sink for failed harness tasks and for `agent_report`
  submissions with blockers, `stuck`, or `error` outcomes.

### P0.3: Workflow Start Bridge

`start_workflow` should create or attach a loop state record. Workflow run id
and loop id should be linked in metadata. Workflow artifacts become loop
artifacts or evidence refs.

Implemented pieces:

- `workflow.Run` stores `loop_id` and `loop_dir` as compatibility metadata.
- Workflow-created loop state lives at `stateDir/loops/<workflow-run-id>` so
  parallel workflow runs do not overwrite the CLI demo's workspace `.loop`.
- `start_workflow` reaches this bridge through both concrete creation paths:
  `create_workflow` and `run_workflow`.
- Script workflow completion and failure sync back to the bound loop status for
  foreground runs and best-effort background runs.
- Loop bindings are projected through `loop.SystemSnapshot` and eval
  observability.

Remaining work: promote workflow plan/script/final report files into loop
artifact evidence refs instead of only linking the run to a loop.

### P0.4: Harness Report Bridge

Agent reports submitted through `agent_report` should update loop progress,
modified files, artifacts, verification strings, and failures when associated
with a loop.

### P1: Control Plane

Expose loop snapshot/status through app-server and desktop. Workflow/harness UI
panels should read through loop-level status first and then drill down to source
stores.

### P2: Store Reduction

After all read paths use loop projections and write paths sync to loop, reduce
duplicated workflow/harness product fields. Keep compatibility readers for old
state directories.

## First Code Step

The first implementation step is intentionally narrow:

1. Add `internal/loop.SystemSnapshot`.
2. Add projection helpers that read `workflow.Store` and `harness.Store`.
3. Update eval observability collection to call the loop snapshot API before
   converting to existing eval trace structs.
4. Keep existing workflow/harness files and JSON schemas intact.

This moves consumers toward one loop-level read model without breaking current
workflow or subagent behavior.

## Second Code Step

The second step adds failure feedback without collapsing module ownership:

1. Loop remains the owner of the durable failure ledger.
2. Workflow/harness facts are converted through explicit sync functions or
   sink interfaces.
3. AgentControl emits failure facts but does not depend on loop internals.
4. Repeated sync of the same external failure is deduplicated by source and
   source id.
