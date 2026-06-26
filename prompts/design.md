# Wuu base system prompt design

This document explains the built-in base prompt in `prompts/system.md`. The base prompt is only the first section of the full runtime prompt; `internal/runtime/session.go` still appends harness adapter, tool surface, user custom prompt, memory, skills, and workflows after it.

## Design goals

The prompt should steer wuu as a local-first GUI coding agent without making the model depend on one active tool profile. It should make routine coding work direct, keep larger work durable when needed, and avoid naming unavailable command paths in profiles that do not expose command execution.

## Behavior changes

- It adds "Ambition vs. precision" so new greenfield work can be creative while existing-code work stays surgical.
- It separates validation from approval: verification is model responsibility, approval flow is harness responsibility.
- It adds hard final-answer verbosity caps so output length is easier to check.
- It splits local commit guidance from remote push guidance and keeps comment rules in three durable buckets.

## Section trace

### Section 1: Identity

This section exists to define wuu as a local-first GUI coding agent, not a terminal-first or read-only assistant. It prevents the old "GUI-first" phrase from implying that GUI paths outrank agent and automation paths. The Codex source is the opening identity block in `thirdparty/codex/codex-rs/protocol/src/prompts/base_instructions/default.md`, but Wuu diverges because its core is shared by desktop and agent-facing execution. The Wuu alignment point is `config.DefaultSystemPrompt()` as the base prompt source.

### Section 2: Default tone

This section exists to keep user-facing text useful, honest, and plain. It prevents sycophantic agreement, overlong progress chatter, and inflated wording. The Codex source is the concise personality guidance in `default.md`. The Wuu alignment point is the root `AGENTS.md` requirement to stay neutral, critical, and direct.

### Section 3: Workspace instructions and memory

This section exists to tell the model how to rank workspace rules, tool rules, and remembered facts. It prevents stale memory or summaries from overriding current files, and it keeps instruction files scoped to the files they govern. The Codex source is the AGENTS.md and responsiveness guidance in `default.md`. The Wuu alignment points are `internal/prompt/builder.go` memory injection and `internal/memory/memory.go` discovery behavior.

### Section 4: Doing tasks

This section exists to preserve the default coding-agent loop: inspect, change, and verify. It prevents answers that only describe a fix, edits made before reading the relevant code, and narrow symptom patches that miss the root cause. The Codex source is the task execution and validation guidance in `default.md`. The Wuu alignment point is the root `AGENTS.md` instruction to complete tasks end to end and ask only for irreversible or high-impact choices.

### Section 5: Ambition vs. precision

This section exists because the prior Wuu prompt over-weighted minimal edits. It prevents the model from treating greenfield product work as a tiny patch, while still protecting existing code from broad rewrites. The Codex source is the "Ambition vs. precision" section in `default.md`. The Wuu alignment point is the product-stage guidance in `AGENTS.md`: ship coherent user-facing behavior, but avoid unrelated changes.

### Section 6: Orchestration

This section exists to map work size and risk to actual Wuu paths: Direct, Skill, `update_plan`, `create_goal`, `start_workflow`, `spawn_agent`, and `write_memory`/`read_memory`. It prevents forced durable state for simple work and prevents missing durable state for work that must survive context loss. The Codex source is the path-selection pattern in `default.md`, adapted to Wuu's tool names. The Wuu alignment points are `internal/tools/tool_goal.go`, `internal/tools/tool_plan.go`, workflow tools, sub-agent tools, and memory-provider-gated memory tools.

### Section 7: Validating your work

This section exists to separate the model's duty to verify from the harness's duty to approve or block tool execution. It prevents the model from asking the user whether routine checks should run when checks are practical, and it prevents the model from pretending checks ran when the active profile lacks command execution. The Codex source is validation guidance in `default.md`; Wuu diverges because permission modes and approval policy live in `internal/config/permissions.go`.

### Section 8: Using tools

This section exists to keep tool use capability-neutral. It prevents base prompt text from naming tools that are unavailable in a given profile and keeps file edits on the dedicated edit surface. The Codex source is the tool-use discipline in `default.md`. The Wuu alignment point is the existing `config.go` wording that command execution is available only when the active tool surface exposes it.

### Section 9: Final answer structure

This section exists to make final responses easier for users to scan and easier for tests to anchor. It prevents long unstructured close-outs, missing verification notes, and file references that the UI cannot open cleanly. The Codex source is the final-answer guidance in `default.md`, with additional verbosity caps inspired by current GPT-5.2 prompt guidance. The Wuu alignment point is the desktop app's user-facing output contract.

### Section 10: Don't

This section exists to collect high-risk negatives that must stay short and operational. It prevents remote writes without explicit request, accidental local commits when no user, project, or workflow rule asks for them, invented evidence, added license text, silent fallbacks, and noisy comments. The Codex source is the never-style guidance in `default.md`. The Wuu alignment points are the root `AGENTS.md` atomic-workflow rules and the old `config.go` three-bucket comment guidance.

## Non-goals

This redesign does not change runtime prompt assembly, tool schemas, permissions, model profiles, memory storage, workflow execution, or desktop UI. Those layers must stay consistent with the base prompt, but they are not redesigned here.
