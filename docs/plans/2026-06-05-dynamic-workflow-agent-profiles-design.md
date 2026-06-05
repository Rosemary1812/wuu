# Dynamic Workflow and Agent Profiles Design

> Date: 2026-06-05
> Status: goal design
> Purpose: define the target product and architecture for reusable dynamic workflows that coordinate long-lived named agents with persistent memory.

---

## 1. Goal

Build a wuu workflow system for long-running and repeatable agent work.

The key product difference from Claude Code dynamic workflows is identity. Claude Code uses dynamic workflows mainly to orchestrate many mostly memoryless subagents from a script. wuu should orchestrate a smaller set of named, memory-bearing Agent Profiles, plus temporary workers when useful.

The desired user experience:

1. A normal conversation starts with a temporary Session Agent.
2. The user asks for a large, scheduled, or repeatable task.
3. The Session Agent can start a workflow, either from a reusable workflow definition or from a generated plan.
4. The workflow wakes named Agent Profiles such as `frontend_owner`, `qa_reviewer`, or `release_manager`.
5. Those profiles use their own durable memory and update it through structured handoffs.
6. The workflow runtime owns state, retries, pause/resume, progress, and final synthesis.

This should feel like asking a lead colleague to organize a team that already knows its domains, not like launching hundreds of anonymous workers.

---

## 2. Product Model

### 2.1 Session Agent

The Session Agent is the ordinary agent in a new chat.

It should stay memoryless by default. It can understand the current conversation and decide when to start a workflow, but it should not be the durable store of workflow state. If a long task survives across turns, restarts, or scheduled triggers, its state belongs to a Workflow Run.

Current repository alignment:

- Tests already assert that the default profile is memoryless and does not expose `read_memory` or `write_memory`.
- Ordinary spawned workers are also memoryless unless `agent_profile` is set.

### 2.2 Agent Profile

An Agent Profile is a named long-lived agent identity.

The name is a stable index into latent context: responsibility, project knowledge, past lessons, preferences, and collaboration patterns. A profile is not a single run. It is closer to "the frontend owner" or "the QA reviewer" than to "check button bug 3".

An Agent Profile should have:

- Stable name.
- Description and responsibility.
- Memory scope.
- Optional default tools or constraints.
- Optional reusable skills.
- Optional default model or effort.
- Durable memory store.

Current repository alignment:

- `spawn_agent` already accepts `agent_profile`.
- A profile worker receives persistent memory and memory tools.
- A normal root session remains memoryless even when profile memory exists.

### 2.3 Agent Run

An Agent Run is one execution of a profile or temporary worker inside a workflow.

It has:

- Run id.
- Task name.
- Parent workflow run id.
- Optional `agent_profile`.
- Prompt and inherited context.
- Status.
- Structured report.
- Artifacts and evidence.

Runs may be ephemeral even when they use a durable profile. The durable identity is the profile; the run is just one task instance.

### 2.4 Workflow Definition

A Workflow Definition is a reusable orchestration asset.

It is similar in spirit to a skill: it can live in the project or user directory, can be discovered, can be invoked by name, can accept arguments, and can be shared with other users. Unlike a skill, it creates and drives a durable Workflow Run instead of only injecting instructions into the current conversation.

Proposed locations:

```text
.claude/workflows/<name>/WORKFLOW.md
~/.claude/workflows/<name>/WORKFLOW.md
```

The `.claude` path keeps cross-compatibility with Claude-style assets. wuu may later add `.wuu/workflows/` aliases, but the first design should not require it.

### 2.5 Workflow Run

A Workflow Run is a durable execution instance created from a definition or generated plan.

It owns:

- State machine.
- Phase graph.
- Agent run records.
- Event log.
- Intermediate results.
- Retry and pause state.
- Final report.

The Session Agent can observe and steer it, but the Session Agent should not need to carry the run's full transcript in context.

---

## 3. Reusable Workflow Assets

### 3.1 Relationship to Skills

Skills and workflows should share discovery and distribution ideas, but not runtime behavior.

| Asset | User job | Runtime behavior |
|---|---|---|
| Skill | Teach an agent how to do a repeatable task | Load markdown instructions into a session |
| Workflow | Run a repeatable multi-agent process | Create a durable Workflow Run |

wuu already parses Claude-compatible `SKILL.md` frontmatter with fields such as `name`, `description`, `when-to-use`, `allowed-tools`, `model`, `context`, `agent`, `paths`, `effort`, and `version`.

Workflow definitions should copy the useful parts:

- Project overrides user.
- Markdown plus frontmatter.
- Slash-command style invocation.
- Optional model invocation disable flag.
- Argument substitution.
- Directory format for portability.

But workflows need extra fields:

- Required or recommended Agent Profiles.
- Whether missing profiles may be created.
- Phase definitions.
- State and retry policy.
- Memory write policy.
- Safety and rollback policy.

### 3.2 Proposed Workflow File Shape

```markdown
---
name: feature-delivery
description: Deliver a product feature through planning, implementation, review, QA, and release notes.
when-to-use: Use when the user asks to build a feature that spans product behavior, code changes, tests, and review.
version: 0.1.0
argument-hint: "<feature request>"
user-invocable: true
disable-model-invocation: false
max-agents: 12
max-concurrency: 4
profiles:
  - name: product_planner
    required: false
  - name: frontend_owner
    required: false
  - name: qa_reviewer
    required: false
allow-profile-creation: ask
memory-policy: report-candidates-only
---

## Intent

Turn a feature request into shipped, reviewed behavior.

## Phases

1. Clarify product intent
2. Inspect relevant code
3. Implement scoped change
4. Review and test
5. Summarize release impact

## Agent Roles

- `product_planner`: clarify scope and user impact.
- `frontend_owner`: inspect and implement UI behavior.
- `qa_reviewer`: verify behavior and report risks.

## State Rules

- If implementation fails due to code errors, retry once with the same profile.
- If review finds a real bug, return to implementation.
- If a required profile is missing and cannot be created, pause.
- If file conflicts are detected, pause and ask the Session Agent to resolve.

## Output

The final report must include shipped behavior, changed files, verification, open risks, and memory candidates.
```

The exact parser can start simpler than full YAML. The important product point is that workflows are portable files, not hidden session state.

---

## 4. Dynamic Workflow Behavior

### 4.1 How a Workflow Starts

There are three start paths:

1. User invokes a saved workflow, for example `/feature-delivery build settings search`.
2. Session Agent chooses a saved workflow because its description matches the task.
3. Session Agent creates an ad hoc workflow plan for a one-off long task.

The runtime should create a Workflow Run in all three cases.

The Session Agent should not manually loop over `spawn_agent` forever. It can produce the first plan, but the runtime tracks and resumes the run.

### 4.2 How Agents Are Selected

The workflow can reference named profiles directly.

If a named profile exists:

- Wake it.
- Inject its memory.
- Run the assigned task.

If a named profile is missing:

- If the workflow says `required: true`, pause.
- If profile creation is allowed, create a memory-bearing profile only when the role is stable and reusable.
- Otherwise, spawn an ephemeral worker.

Profile creation must be conservative. Do not create durable profiles for one-off task slices such as `grep_routes_agent_3`.

### 4.3 How Memory Is Updated

Agent Profiles should not write arbitrary long-term memory during every run.

The safe flow:

```text
Agent Run
  -> agent_report
  -> memory_candidates
  -> validation / dedup / merge
  -> profile memory
```

Good memory:

- Stable project facts.
- Role-specific domain lessons.
- Durable workflow lessons.
- Repeated user preferences.
- Known environment quirks.

Bad memory:

- Temporary task progress.
- Raw transcript summaries.
- PR numbers, issue numbers, or commit hashes that will go stale.
- Unverified conclusions.
- Duplicate facts.

This matches the existing memory review rule that only durable facts likely to help future sessions should be saved.

### 4.4 Scale

wuu should not optimize first for thousands of agents.

Initial target:

- Typical workflow: 3 to 12 Agent Runs.
- Practical max: under 100 Agent Runs.
- Default concurrency: 3 to 5.
- Hard cap should exist to prevent runaway workflows.

The product value is not cluster size. The value is durable named collaborators with clean state transitions and reusable process.

---

## 5. State Machine

The runtime should own state transitions. The model can propose semantic decisions, but code must enforce valid transitions.

### 5.1 Workflow Run States

```text
draft
  -> approval_pending
  -> running
  -> paused
  -> completed
  -> failed
  -> cancelled
```

Meanings:

- `draft`: plan exists but has not started.
- `approval_pending`: user or policy approval is needed.
- `running`: phases are executing.
- `paused`: blocked on user input, permission, missing profile, credentials, conflict, or explicit pause.
- `completed`: final report accepted.
- `failed`: unrecoverable runtime or semantic failure.
- `cancelled`: user or system stopped the run.

### 5.2 Phase States

```text
pending
  -> runnable
  -> running
  -> completed
  -> blocked
  -> failed
  -> skipped
```

Phases are the workflow's coarse units. They should be visible in the UI.

### 5.3 Agent Run States

```text
queued
  -> starting
  -> running
  -> awaiting_report
  -> completed
  -> failed
  -> retrying
  -> cancelled
```

`awaiting_report` is important. A worker that returns text but does not call `agent_report` has not completed a clean handoff.

Current repository alignment:

- `await_agents` already reports `awaiting_report` when a completed worker lacks a structured report.
- `agent_report` already captures summary, changed files, work done, blockers, risks, verification, next steps, evidence, and artifacts.

---

## 6. Recovery and Rollback

Do not let the model be the only recovery mechanism.

### 6.1 Runtime-owned Recovery

The runtime should automatically handle:

- Transient provider failures.
- Network timeouts.
- Tool execution timeouts.
- Agent process cancellation.
- Missing structured report follow-up.
- Concurrency limits.
- Pause and resume.

Runtime retry policy should be bounded. Example:

```text
transient_error: retry up to 2 times with backoff
missing_report: ask the same agent once to submit agent_report
permission_blocked: pause
profile_missing: pause or use ephemeral worker based on workflow policy
file_conflict: pause
```

### 6.2 Model-owned Recovery

The model or workflow planner should decide:

- Whether a failed implementation needs a different approach.
- Whether review findings require another implementation pass.
- Whether two Agent Profiles disagree and need arbitration.
- Whether a task should be split differently.

This is semantic recovery, not runtime recovery.

### 6.3 Rollback

File rollback must be a runtime ability, not a promise in prose.

Preferred strategies:

1. Worktree isolation for broad, destructive, or overlapping changes.
2. Checkpoints for direct working-tree edits.
3. Explicit changed-file tracking from `agent_report`.

If a workflow runs multiple writing agents in parallel, worktree isolation should be the default unless the workflow declares non-overlapping file scopes.

---

## 7. User Experience

### 7.1 What Users See

Users should see:

- Workflow name.
- Current phase.
- Running profiles and temporary workers.
- Whether an agent is using durable memory.
- Status: running, paused, waiting for report, failed, completed.
- Final report.
- Memory candidates that were saved or rejected when that matters.

Users should not see:

- Raw provider payloads.
- Hidden prompts.
- Full intermediate transcripts by default.
- Internal stack traces.

### 7.2 Controls

Minimum controls:

- Start workflow.
- Pause / resume.
- Cancel.
- Open phase details.
- Open agent report.
- Retry failed agent.
- Save an ad hoc workflow as reusable definition.

Later controls:

- Edit workflow before running.
- Approve profile creation.
- Approve memory candidates.
- Export workflow bundle.

### 7.3 Scheduled Workflows

Scheduled tasks should start Workflow Runs, not ordinary chat loops, when the task is workflow-shaped.

Examples:

- Daily dependency review.
- Weekly visual QA.
- Release readiness check.
- Regression sweep before deploy.

The scheduler should not need to remember workflow state in chat context. It should trigger a new or resumed Workflow Run.

---

## 8. Implementation Shape

### 8.1 Existing Pieces to Reuse

Reuse:

- `AgentControl` as the control plane.
- `spawn_agent` with `agent_profile`.
- `await_agents`.
- `agent_report`.
- `harness` store for task/run/report/event concepts.
- `memory` and profile memory review.
- `skills` discovery conventions.
- `cron` for scheduled triggers.

Do not replace the current agent runtime. Build Workflow Run orchestration on top of it.

### 8.2 New Concepts

Likely new core package:

```text
internal/workflow/
  definition.go
  discovery.go
  run.go
  state.go
  runner.go
  events.go
  store.go
```

Likely new tools:

```text
create_workflow
list_workflows
workflow_status
workflow_control
```

The tool names can change later. The important split is:

- `spawn_agent`: still starts one child task.
- `create_workflow`: starts or drafts a durable multi-agent run.

### 8.3 Storage

Workflow definitions:

```text
.claude/workflows/<name>/WORKFLOW.md
~/.claude/workflows/<name>/WORKFLOW.md
```

Workflow run state:

```text
<session-or-state-dir>/workflows/<run-id>/
  run.json
  events.jsonl
  plan.md
  final-report.md
  agents/
    <agent-run-id>.json
```

The exact root directory should follow existing session artifact conventions.

### 8.4 Workflow Script or Declarative Plan

Claude Code uses JavaScript scripts for dynamic workflows. wuu does not need to copy that first.

Recommended first version:

- Markdown definition.
- Structured phase list.
- Runtime state machine.
- Model-assisted replanning at phase boundaries.

Later version:

- Optional executable workflow script.
- Sandboxed runtime.
- Script can orchestrate agents but cannot directly read/write files or run shell.

This avoids making the first release depend on a new scripting sandbox.

---

## 9. Non-goals for First Version

- Do not support 1000-agent runs.
- Do not let every temporary worker become a durable profile.
- Do not expose full transcripts by default.
- Do not allow workflow scripts to bypass tool permissions.
- Do not implement cross-machine distributed execution.
- Do not require complex visual workflow editing.
- Do not store raw conversation history as profile memory.

---

## 10. Open Questions

1. Should project workflows use `.claude/workflows/` only, or should wuu also support `.wuu/workflows/`?
2. Should users approve every new Agent Profile, or can trusted workflows create profiles automatically?
3. Should memory candidates be auto-accepted for trusted profiles, or always reviewed?
4. Should workflow definitions allow tool restrictions per phase, per profile, or both?
5. Should scheduled workflows resume an existing run or always create a fresh run?
6. What is the right UI for profile identity and memory visibility?

---

## 11. First Shippable Slice

The smallest useful version:

1. Discover reusable workflow definitions from `.claude/workflows/`.
2. Add a `create_workflow` tool that creates a durable run from a simple phase plan.
3. Use existing `spawn_agent` with optional `agent_profile` for agent execution.
4. Require every workflow agent to submit `agent_report`.
5. Track run, phase, and agent states in an event log.
6. Add `workflow_status` so the Session Agent and UI can inspect progress.
7. Generate a final report from structured agent reports.
8. Store memory candidates, but do not auto-write them without a first review path.

This slice proves the core product model: temporary Session Agent, durable Workflow Run, named memory-bearing Agent Profiles, reusable workflow definition.

---

## 12. Reference Notes

- Claude Code dynamic workflows move orchestration into a background script and keep intermediate results out of the main conversation. Source: https://code.claude.com/docs/en/workflows
- Claude Code subagents run separate contexts and return final results to the parent. Source: https://code.claude.com/docs/en/agent-sdk/subagents
- wuu already has memoryless default sessions and profile-bound memory workers, covered by `internal/runtime/session_test.go`.
- wuu already has structured agent reports through `agent_report`, covered by `internal/tools/tool_agents.go` and `internal/agentcontrol/report.go`.
- wuu skills already use Claude-compatible `SKILL.md` discovery and frontmatter, covered by `internal/skills/skills.go`.
