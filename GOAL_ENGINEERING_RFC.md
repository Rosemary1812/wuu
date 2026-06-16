# Goal / Goal Engineering RFC

## Architecture Audit

Wuu already has the pieces of a goal-driven agent system, but they are not yet
one durable product goal.

Existing capabilities:

- `internal/agent` owns the core model/tool loop: model calls, tool results,
  streaming, truncation recovery, context overflow compaction, usage tracking,
  and concurrent read-only tools.
- `internal/tools` is the harness surface: tool registry, risk classification,
  permission profiles, shell safety blocks, telemetry, result budgeting, patch
  journals, process tools, workflow tools, cron tools, MCP tools, and skill
  loading.
- `internal/hooks` supports PreToolUse, PostToolUse, PostToolUseFailure,
  UserPromptSubmit, SessionStart, SessionEnd, Stop, and FileChanged.
- `internal/workflow` persists workflow runs, phases, agent runs, team plans,
  checkpoints, memory candidates, final reports, and an event log.
- `internal/harness` persists task graph state for spawned agents: tasks, runs,
  reports, artifacts, queue items, and events.
- `internal/worktree` creates detached git worktrees for subagent isolation.
- `internal/agentcontrol` wires subagents, worktrees, thread metadata, reports,
  mailbox notifications, queued workers, and harness state together.
- `internal/skills` discovers `SKILL.md` and frontmatter-based skills, and
  exposes bundled skills.
- `internal/cron` and `internal/evalharness` provide scheduled prompt/workflow
  triggers and local task fixtures with verification and trace replay.
- The control plane exists through CLI, app-server, and Electron, with current
  desktop panels for files, diff review, terminal, and browser preview.

Main gaps:

- There is no single `GoalRunner` abstraction. Core agent loop, workflow state,
  harness state, worktree isolation, verifier policy, retry policy, escalation,
  and failure feedback are spread across tools and prompt instructions.
- Durable task state is fragmented under `.wuu/...`; there is no stable
  Wuu-managed `goals/<goal_id>/state.json`,
  `goals/<goal_id>/events.jsonl`, and `goals/<goal_id>/views/*.md`
  contract.
- Workflows are still mostly model-driven. Agent-managed runs create durable
  records, but the model manually chooses when to spawn, await, verify, review,
  retry, and complete.
- Worktree support creates and preserves isolated directories, but it does not
  yet provide a unified manifest, branch binding, status snapshot, merge
  preview, conflict check, or rollback API.
- Skill routing exists, but project-native `.wuu/skills` discovery, richer
  skill metadata, verification checklists, and the core goal-engineering skills
  are incomplete.
- Maker/checker separation is available through the `verification` worker, but
  it is not enforced by workflow state. A coding worker can still finish without
  an independent verifier or reviewer being recorded.
- Failures are visible in tool telemetry and hook events, but they are not
  always written into a durable failure ledger that the next goal step must
  read before acting.
- The desktop control plane does not yet expose goal runs, agent teams,
  worktree leases, verifier results, failure logs, or approval queues.

## Target Architecture

Wuu should expose four clean layers.

### Core Agent Loop

Package: `internal/agent`

Responsibilities:

- Call the selected provider.
- Execute model-requested tools.
- Append tool results and continue reasoning.
- Compact or recover from model context/output limits.
- Emit stream and usage events.

Non-responsibilities:

- Durable workflow planning.
- Worktree lifecycle.
- Verifier/reviewer gates.
- Human approval queues.

### Harness Layer

Packages: `internal/tools`, `internal/hooks`, `internal/harness`,
`internal/sessiontrace`, `internal/eventbus`, `internal/mcp`,
`internal/plugin`.

Responsibilities:

- Tool registry and permission decisions.
- Sandbox and destructive-action policy.
- Hooks and failure capture.
- Tool telemetry, trace, result references, and redaction.
- External tool/connectors such as MCP and future GitHub/CI/browser/log
  connectors.

### Workflow Goal

Packages: new `internal/goal`, existing `internal/workflow`,
`internal/worktree`, `internal/agentcontrol`.

Responsibilities:

- Represent a long-running goal as durable state.
- Maintain current step, completed steps, blockers, failures, decisions, and
  artifacts outside model context.
- Assign work to subagents and worktrees.
- Run verifier policies and record results.
- Retry or escalate based on policy.
- Produce final artifacts and replayable event logs.

### Control Plane

Packages/surfaces: `cmd/wuu`, `internal/appserver`, `desktop`.

Responsibilities:

- Start, inspect, resume, cancel, and summarize goals.
- Show current task state, agent team, worktrees, diff review, verifier result,
  failures, and approval requests.
- Bridge manual, scheduled, git/CI/mock, and future connector triggers into
  workflow goals.

## Core Abstractions

### GoalRunner

`GoalRunner` is the internal durable workflow executor for a user-visible goal,
not a model wrapper. It
does not replace `internal/agent.RunToolLoop`; it sits above it.

State fields:

- `goal`, `task`, `trigger`
- `status`, `current_step`, `assigned_agent`
- `permissions`
- `worktree`
- `verification_policy`
- `retry_policy`
- `escalation_policy`
- `final_artifact`
- `artifacts`
- `progress`
- `failures`
- `decisions`
- `modified_files`
- `test_results`
- `next_steps`
- `needs_human`

Durable files:

```text
<Wuu workspace state>/
  goals/
    <goal_id>/
      state.json
      events.jsonl
      artifacts/
        research.md
        plan.md
        todo.md
        verification.md
        review.md
        final.md
      views/
        progress.md
        decisions.md
        failures.md
        approvals.md
```

`state.json` and `events.jsonl` are the source of truth. Files under `views/`
are derived human-readable summaries.

### WorktreeManager

`internal/worktree.Manager` should grow from create/cleanup into lease
management:

- Create worktree per task/session/agent.
- Record base repo, base HEAD, branch, task id, session id, agent id.
- Snapshot status and changed files.
- Write a manifest for replay.
- Produce a diff for review.
- Preview merge/conflict risk.
- Roll back a lease to its base state.
- Clean up only when policy says it is safe.

### Skill Registry

Skills should be routable and progressively disclosed.

Supported metadata:

- `name`
- `description`
- `trigger_condition`
- `allowed_tools`
- `required_context`
- `examples`
- `verification_checklist`
- `progressive_disclosure`
- `paths`

Discovery sources:

- bundled skills
- project `.wuu/skills`
- legacy project `.claude/skills`
- user `$WUU_HOME/skills`
- legacy user `~/.claude/skills`
- plugin skill dirs

### Subagent Roles

Minimum roles:

- Lead / Planner
- Researcher
- Worker
- Reviewer
- QA / Verifier
- Debugger
- Integrator

Each role needs a name, role prompt, allowed tools, context scope, output
schema, and success criteria. Coding workers may produce implementation
artifacts, but goal completion requires reviewer or verifier evidence.

### Verification Policy

Each workflow declares checks such as:

- typecheck
- lint
- unit test
- integration test
- build
- browser/UI smoke
- screenshot/DOM check
- diff review
- regression test generation
- manual approval gate

The first implementation should support command checks and manual gates; UI and
browser checks can be connector-backed later.

### Failure Feedback

Failures become durable events, not terminal-only logs.

Capture:

- command/test/type/lint/runtime/browser failures
- agent timeout
- tool permission denial
- git conflict
- user rejection

Write:

- `views/failures.md`
- `events.jsonl`
- `state.json.current_blocker`
- `state.json.next_steps`

The next goal step must read state and failures before selecting action.

## Workflow Example

```text
manual trigger:
  goal: "Fix flaky Electron startup test"

init:
  write state.json and events.jsonl in the Wuu-managed goal store

research:
  researcher reads app-server, desktop main, failing logs
  artifact: artifacts/research.md

plan:
  planner writes scoped implementation plan
  artifact: artifacts/plan.md and todo.md

execution:
  worker gets a worktree lease and edits only assigned files
  artifact: worktree manifest and diff

verification:
  verifier runs configured checks
  artifact: verification.md

review:
  reviewer checks diff and risk
  artifact: review.md

integration:
  integrator merges or reports conflicts

summary:
  final.md records outcome, limitations, and next steps
```

## P0 / P1 / P2 Todo

### P0

- Add `internal/goal` with durable state store, event log, markdown ledgers,
  failure feedback, verifier pipeline, role registry, and demo runner.
- Add CLI `wuu goal demo/status` as a minimal control-plane entry point.
- Upgrade `internal/worktree` with lease manifest, status snapshot, diff review,
  merge preview, and rollback helpers.
- Extend skill discovery for project `.wuu/skills` and user `$WUU_HOME/skills`.
- Add bundled goal-engineering skills:
  `codebase-research`, `implementation-plan`, `safe-edit`,
  `regression-test`, `ci-failure-triage`, `diff-review`, `browser-task`,
  `electron-debug`, `long-running-workflow`, `release-check`.
- Add subagent role configs for planner, researcher, worker, reviewer, QA,
  debugger, and integrator.
- Add tests for state persistence, verifier failure logging, worktree lease
  metadata, skill routing, and role registry.

### P1

- Wire GoalRunner into `start_workflow` so agent-managed workflows can run
  declarative phases instead of relying on manual prompt sequencing.
- Add app-server methods for goal list/status/events/artifacts.
- Add desktop goal control plane: active goals, team topology, worktrees,
  verification results, failure log, approval queue, and final artifact.
- Add browser/UI verification connector with console error capture and
  screenshot/DOM evidence.
- Add GitHub/CI mock trigger and real GitHub issue/PR connector interface.
- Enforce maker/checker gate before marking workflow runs complete.

### P2

- Add resumable background daemon mode for scheduled and event-triggered goals.
- Add real GitHub, Linear/Jira, Slack/Discord, CI, logs, database, and browser
  connectors behind one connector interface.
- Add regression eval suites for UI changes, Electron debugging, browser
  workflows, long-running refactors, failing-test repair, and docs updates.
- Add cost/latency budget dashboards and goal replay views.
- Add policy-backed approval UI for destructive git, install, network,
  credential, mass rewrite, and external mutation actions.

## Migration Plan

1. Keep `internal/agent` stable. Do not rewrite the core tool loop.
2. Add `internal/goal` as an additive package with a demo CLI path.
3. Reuse existing `workflow.Store`, `harness.Store`, and `worktree.Manager`
   where possible; only add missing APIs.
4. Make bundled skills available without forcing project-local files.
5. Expose additional subagent roles through existing `spawn_agent`.
6. Move workflow tools onto GoalRunner once the minimal goal is tested.
7. Add app-server and Electron read-only goal views before adding mutation UI.

## Risks

- Over-abstracting before the product path is proven. Mitigation: P0 runs as a
  concrete demo workflow and has tests.
- Mixing runtime state with repo source. Mitigation: generated run state uses
  workspace state under `$WUU_HOME` and does not write goal state into the
  project tree.
- Breaking existing subagent behavior. Mitigation: keep `general-purpose` and
  `verification` semantics unchanged; add new roles as opt-in values.
- Treating verification as documentation. Mitigation: verifier pipeline records
  command exit codes and writes failures into durable state.
