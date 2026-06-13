---
name: long-running-workflow
description: Run durable multi-phase work without relying on one context window.
trigger-condition: Use for large tasks that need research, plan, execution, verification, review, and resumability.
allowed-tools: [read_file, apply_patch, run_shell, run_test, spawn_agent]
required-context: [goal, durable state, progress log, failure log, verification policy, worktree status]
examples: [resume loop workflow, coordinate agent team, recover after failed verification]
verification-checklist: [state updated, artifacts written, failures captured, reviewer or verifier separated from worker]
progressive-disclosure: Always read .loop/state.json and recent failures before loading detailed artifacts.
---

# Long Running Workflow

Do not rely on model memory for long tasks.

1. Initialize durable state.
2. Run phases in order: research, plan, approval when needed, execution, verification, review, integration, summary.
3. Write an artifact for each phase.
4. Append progress, decisions, events, and failures after each agent round.
5. Require reviewer or verifier evidence before final completion.

If blocked, persist the blocker and the next useful action.
