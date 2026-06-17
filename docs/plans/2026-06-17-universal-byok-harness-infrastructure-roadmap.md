# Universal BYOK Harness Infrastructure Roadmap

## Goal

Wuu should become a general-purpose BYOK coding-agent harness. The target is
not to clone Codex, Claude Code, or OpenCode. The target is to absorb the parts
of each system that are structurally useful, then express them through Wuu's
own provider-neutral runtime.

This roadmap exists because `approve for me` exposed a broader architecture
truth: the best permission and reviewer designs depend on infrastructure that
Wuu only partially has today. If Wuu implements the surface feature first, it
will keep accumulating local special cases. If Wuu builds the shared
infrastructure first, `approve for me`, subagents, compaction, memory review,
and future shells can all reuse the same foundation.

## Product Motivation

Users want a coding agent that can run strong models from many providers, keep
the tool loop productive, and still preserve understandable control over risky
actions. They do not want a harness that only works well with one provider's
model family.

The current market evidence is mixed:

- Codex has the strongest safety and approval reference design: sandbox
  profiles, approval routing, guardian review sessions, event tracking, and
  fail-closed behavior.
- Claude Code is a high-quality practical coding harness. Although its core
  experience is Claude-shaped, many non-ChatGPT models work well in it, so it
  is an important reference for agent loops, tool UX, permissions, and
  multi-agent workflows.
- OpenCode is closer to a general BYOK provider/model base. It has useful
  provider and model catalog ideas, but its own security document explicitly
  says the permission system is not a sandbox.

The conclusion is that Wuu should not pick one reference as authority. Wuu
should make the underlying concepts explicit: model capabilities, model
behavior profiles, runtime permission boundaries, tool protocol strategy,
review sessions, context budgets, and observable approval events.

## Product Principle

Wuu should be model-neutral but not model-blind.

Provider-neutral does not mean pretending all models behave the same. A useful
BYOK harness needs to know, or learn, how a model behaves:

- tool-call reliability
- structured-output reliability
- patch vs full-file editing preference
- context-window and long-context stability
- reasoning controls and compatibility
- shell/tool overuse tendency
- suitability for main agent, reviewer, compact, title, memory, and worker
  roles
- provider-specific protocol limits, message ordering rules, and retry
  behavior

These should be represented as runtime facts, not hidden in prompt wording or
provider-specific branches.

## Reference Posture

Use references as evidence, not authority. The references should guide future
agents, but this document intentionally does not pin every implementation
detail. When a later task needs details, inspect the relevant `thirdparty/`
source and tests at that time.

### Codex

Use Codex as the primary reference for safety closure:

- permission profiles and approval policy separation
- guardian / auto-review semantics
- read-only reviewer sessions
- fail-closed timeout and parse behavior
- approval assessment events and telemetry
- sandbox and network-boundary design

Do not copy OpenAI-specific assumptions into Wuu core. Where Codex has a
preferred OpenAI review model, Wuu should express the same idea as a generic
role model selection mechanism.

### Claude Code

Use Claude Code as a practical harness reference:

- strong coding-agent loop behavior
- model-facing tool design
- permission request UX
- subagent and multi-agent workflows
- task planning and verification habits
- command and file editing ergonomics

Treat Claude-specific prompts, model names, and behavioral assumptions as
examples, not core defaults.

### OpenCode

Use OpenCode as the provider/model and BYOK configuration reference:

- provider and model catalog shape
- endpoint and variant modeling
- provider plugin structure
- permission rule syntax as a UX/reference layer
- session and UI data modeling

Do not treat OpenCode permissions as a security boundary. Its security posture
explicitly says there is no sandbox.

### Other References

Cline, Goose, cmux, and Hermes can be used as secondary references when a task
touches IDE integration, local automation, workflows, or UI state. They should
not override the main direction unless they have a closer analogue for the
specific problem.

## Current Wuu Foundation

Wuu already has useful pieces:

- provider abstractions under `internal/providers`
- provider construction under `internal/providerfactory`
- model catalog, profile, and variant logic under `internal/modelcatalog`,
  `internal/modelprofile`, and `internal/modelvariant`
- permission mode presets under `internal/config/permissions.go`
- tool policy and classification under `internal/tools/tool_policy.go`
- guardian reviewer, prompt, transcript, and breaker under `internal/guardian`
- context packing and repo context under `internal/context`
- compaction under `internal/compact`
- hooks under `internal/hooks`
- session trace under `internal/sessiontrace`
- Electron desktop shell that talks to the Go app-server

This means Wuu is not starting from zero. The next work is mostly about
turning these pieces into explicit platform boundaries.

## Infrastructure Gaps

### 1. Model Capability Layer

Wuu needs a normalized model capability record that can be resolved for every
configured provider/model.

It should describe:

- tool support
- structured output support
- streaming support
- reasoning parameter support
- context/input/output limits
- image/file input support
- cache support if applicable
- retry-safe error categories
- provider protocol family

This layer should be stable enough that the runtime does not need to inspect
provider names for common behavior.

### 2. Model Behavior Profile

Capabilities describe what a model can do. Behavior profiles describe how the
model tends to behave in a coding harness.

This should include:

- default edit strategy: patch, replace, or model-dependent
- tool-use strictness needed in prompts
- expected JSON reliability
- whether the model needs shorter tool schemas
- whether the model handles long transcripts well
- whether it is suitable for review, compaction, title, memory, or worker
  roles

These profiles can start as curated defaults and later be refined by evals and
telemetry.

### 3. Role-Based Model Selection

Wuu should stop treating "the current model" as the only model.

The runtime should support role model selection:

```text
main_model
review_model
compact_model
title_model
memory_model
worker_model
fallback_model
```

Each role can inherit from the main model, use a provider/catalog
recommendation, or be explicitly configured by the user.

This is the generic version of Codex's auto-review preferred model and Claude
Code's practical model-role choices.

### 4. Runtime-Enforced Permission Boundaries

Wuu has permission modes and tool policy, but the next foundation is a harder
runtime boundary:

```text
read_only
workspace_write
danger_full_access
network policy
secret/env policy
filesystem boundary
process boundary
```

Tool policy should decide whether an action is allowed, denied, or requires
approval. The permission boundary should decide what the runtime is physically
able to do.

This distinction matters because a reviewer can be wrong. The runtime boundary
must still hold.

### 5. Restricted SubSession / ReviewSession

Wuu needs a generic restricted child-runtime concept. Guardian should be one
use case, not the only one.

A restricted review session should support:

- read-only permission profile
- approval policy set to never
- no write tools
- no shell mutation
- no MCP by default
- no hooks by default
- no plugins by default
- no skills by default unless explicitly allowed
- no durable memory writes
- strict output schema
- timeout and cancellation
- fail-closed result

This can later power:

- guardian approval review
- memory review
- compaction review
- plan review
- security review
- worker output verification

### 6. Context Budget and Transcript Packing

Wuu needs role-aware context packing. The context sent to a main agent is not
the same context needed by a reviewer.

For guardian review, the packer should preserve:

- recent user intent
- relevant assistant plan or claim
- current tool request
- argument preview
- file or diff summary where relevant
- recent tool results that affect the decision
- risk and policy metadata

The packer should have separate budgets for messages, tool results, action
payloads, and repository facts. Token-aware packing is preferable, but a
character-budget first pass is acceptable if it is measured and conservative.

### 7. Approval and Guardian Event Model

The product needs observable approval events, not just tool errors.

Wuu should expose structured events such as:

```text
approval_requested
approval_approved
approval_denied
guardian_started
guardian_approved
guardian_denied
guardian_timed_out
guardian_failed_closed
guardian_turn_interrupted
```

Each event should carry enough detail for UI and trace:

- approval id
- tool name
- action summary
- reviewer source
- review model
- risk level
- decision reason
- failure kind
- elapsed time

This makes "approve for me" explainable instead of magical.

### 8. Plugin, MCP, Hook, and Skill Trust Boundaries

Wuu should define which extension surfaces are available to which runtime
roles.

Default direction:

- main agent can use configured tools according to permission mode
- reviewer sessions start with no MCP, hooks, plugins, or skills
- worker sessions inherit only the capabilities declared by their role
- production desktop debug surfaces remain hidden unless explicitly gated
- external tools must be classified before they can participate in approval or
  reviewer flows

This should be enforced by runtime construction, not only by prompts.

### 9. Provider Protocol Compatibility Layer

Different provider APIs have different invariants. A general harness must
normalize them before the agent loop sees them.

The layer should own:

- tool-call/result pairing rules
- message ordering repair or refusal
- system/developer/user role compatibility
- structured output modes
- streaming event normalization
- retry and fallback behavior
- provider-specific model id translation

This reduces the chance that a prompt or tool change works on one provider and
breaks another.

### 10. Evaluation and Trace Feedback

The infrastructure should be measurable. For each model/profile/role, Wuu
should be able to answer:

- did tool calls follow provider protocol
- did the model choose the expected edit strategy
- did reviewer decisions match policy
- did context packing preserve user intent
- did the runtime boundary block unsafe actions
- did the user have to intervene repeatedly

Session trace and eval replay should become the feedback loop for model
behavior profiles.

## How This Changes `Approve For Me`

`Approve for me` should be treated as a product scenario built on top of the
shared infrastructure:

```text
permission boundary: workspace_write
approval policy: on_request
reviewer: auto_review
review runtime: restricted ReviewSession
review model: role-selected review_model
failure behavior: fail closed
events: guardian assessment lifecycle
```

It should not have its own local rule-based approval path.

The current Wuu implementation has the right product semantics after removing
the local approval fallback: guardian success can approve, guardian failure
blocks. The remaining gap is that the reviewer is still lighter than Codex's
locked-down review session.

## Phased Execution Plan

### Phase 1: Model Capability and Behavior Profiles

Deliverables:

- normalized model capability struct
- model behavior profile struct
- role model selection config
- provider/model summary exposed through app-server
- tests for provider-neutral fallback behavior

Acceptance:

- Wuu can answer which model is used for main, review, compact, title, memory,
  and worker roles.
- Review model selection does not require OpenAI-specific code.
- Existing configs continue to work by inheriting from the main model.

### Phase 2: Permission Boundary Runtime

Deliverables:

- runtime permission boundary abstraction
- read-only enforcement for mutating tools
- workspace-write boundary for file and process tools
- network and secret policy model
- trace records for boundary decisions

Acceptance:

- Read-only mode cannot write even if a tool or reviewer tries to.
- Workspace-write mode cannot write outside the workspace boundary.
- Full Access remains explicit and visibly dangerous.

### Phase 3: Generic Restricted SubSession

Deliverables:

- SubSession / ReviewSession runtime construction
- per-role capability gates
- disabled extension surfaces by default
- timeout/cancel/fail-closed contract
- session trace linkage to parent turn

Acceptance:

- A reviewer can run without access to mutating tools, MCP, hooks, plugins, or
  durable memory writes.
- Parent and child sessions have clear trace relationships.
- Child session setup is provider-neutral.

### Phase 4: Guardian Review Session

Deliverables:

- guardian moved from single Chat call to restricted ReviewSession
- strict structured output contract
- role-selected review model
- context packer for approval review
- guardian assessment events
- retry policy for transient review failures

Acceptance:

- `approve for me` routes approval requests to guardian ReviewSession.
- Guardian timeout, parse failure, and provider failure all block execution.
- UI and trace explain why an action was approved or denied.

### Phase 5: Extension Trust Boundaries

Deliverables:

- trust policy for MCP, hooks, plugins, skills, and external tools
- tool classification requirements for extension tools
- reviewer session extension deny-by-default behavior
- config and UI summary for extension trust state

Acceptance:

- Extension surfaces cannot silently enter reviewer sessions.
- External tools cannot bypass permission classification.
- Users can understand which extensions are active in the main session.

### Phase 6: Optimization and Reuse

Deliverables:

- review session reuse where safe
- forked review sessions for concurrent approvals
- prompt/cache stability work
- eval-driven model behavior profile updates

Acceptance:

- Review latency improves without weakening isolation.
- Concurrent approvals do not mutate shared reviewer state unsafely.
- Behavior profile changes are backed by trace/eval evidence.

## Non-Goals

- Do not implement GPT-only guardian infrastructure.
- Do not copy Claude-specific prompts into core as universal defaults.
- Do not treat OpenCode permission rules as a security boundary.
- Do not add local rule-based auto approval as a replacement for guardian.
- Do not claim sandbox parity until runtime boundaries are actually enforced.
- Do not make every reference detail part of this roadmap; later tasks should
  inspect the relevant source again when needed.

## Guidance For Future Agents

When implementing any item from this roadmap:

1. Start from Wuu's product goal: a general BYOK coding harness.
2. Inspect Wuu's current code before proposing changes.
3. Inspect the closest `thirdparty/` reference for the specific subsystem.
4. Prefer generic abstractions over provider-specific branches.
5. Keep security behavior fail-closed.
6. Preserve provider protocol invariants: message ordering, tool-call/result
   pairing, structured output contracts, and retry safety.
7. Add tests at the boundary being changed.
8. Update user-facing summaries only when the behavior is real.

## Verification Plan

Short-term verification for this roadmap document:

- Confirm Wuu has the modules named in "Current Wuu Foundation".
- Confirm `thirdparty/` has Codex, Claude Code sourcemap, OpenCode, Cline, and
  Goose references available for future exploration.
- Confirm the roadmap does not depend on OpenAI-only or Claude-only model
  assumptions.

Implementation verification for later phases should be defined phase by phase.
