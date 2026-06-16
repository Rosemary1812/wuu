# Codex Approval Alignment Design

## Goal

Align Wuu's permission and approval model with the current Codex design for
Default, Approve for me, Read Only, and Full Access modes.

The target is not an allow-all auto mode. Codex's Approve for me mode keeps the
same sandbox and approval boundary as Default mode, then routes only approval
requests to an automated reviewer. Wuu should preserve that distinction.

## Codex Reference Model

The latest Codex reference under `thirdparty/codex` shows this model:

- Read Only: read-only sandbox, approvals requested when a write or other
  blocked action is needed.
- Default: workspace-write sandbox plus on-request approval. Actions inside the
  allowed workspace boundary can run; actions outside that boundary request
  approval.
- Approve for me: same permission profile and approval policy as Default, but
  `approvals_reviewer=auto_review`. Existing approval requests are reviewed by
  a locked-down reviewer instead of the user.
- Full Access: no sandbox and no approvals. This is a danger mode, not the
  implementation of Approve for me.

Important Codex properties to preserve:

- Approval policy, permission profile, and approval reviewer are separate
  settings.
- The automated reviewer only sees requests that already require approval.
- The automated reviewer runs in a restricted review session and fails closed.
- Approval decisions can be cached for the session when the selected decision
  explicitly allows that.
- The tool runtime has one central approval and sandbox orchestrator instead of
  scattered per-tool checks.

## Current Wuu State

Wuu currently has a simpler tool policy layer:

- `agent.tool_policy.profile` supports `safe`, `balanced`, `auto`,
  `autonomous`, and `enterprise_restricted`.
- `auto` uses `auto_classify`: low-risk calls run directly, while medium/high
  calls are passed to a local `AutoModeClassifier`.
- `require_approval` creates a redacted approval artifact and returns
  `approval_required` to the model. It does not pause the turn, wait for a user
  decision, and then resume the same tool call.
- Wuu has tool-level guards and workspace path checks, but it does not yet have
  Codex's OS-level workspace-write sandbox orchestration.
- The desktop permission menu exposes only Manual, Auto, and Full Access.

Because of that, "fully aligning" by renaming Wuu's current auto mode would be
misleading. The product model has to be split first.

## Target Wuu Model

Add an explicit runtime permission configuration:

```text
permission_profile:
  read_only | workspace_write | danger_full_access

approval_policy:
  on_request | never

approvals_reviewer:
  user | auto_review
```

Expose four user-facing modes:

```text
Read Only       => read_only + on_request + user
Default         => workspace_write + on_request + user
Approve for me  => workspace_write + on_request + auto_review
Full Access     => danger_full_access + never + user
```

The existing risk-based `tool_policy` should remain as an advanced override
layer during migration, but the everyday desktop menu should map to the new
Codex-shaped modes.

## Implementation Plan

### 1. Foundation: config and summaries

- Add config fields for permission profile, approval policy, and approval
  reviewer.
- Add a preset resolver that converts the four user-facing modes into those
  three fields.
- Preserve existing `tool_policy` config for compatibility.
- Return the resolved permission summary through app-server initialize/config
  responses.
- Update desktop types and the permission menu to show Read Only, Default,
  Approve for me, and Full Access.

Acceptance:

- Existing configs still load.
- Selecting a permission mode updates the saved runtime config.
- Desktop shows the Codex-shaped mode labels.

### 2. Manual approval queue

- Replace "approval artifact only" behavior with a pending approval request
  that can be resolved by the UI or app-server API.
- Keep redacted approval artifacts for replay and audit.
- Add `Approved`, `Denied`, and `ApprovedForSession` decisions.
- Cache `ApprovedForSession` by a stable per-tool approval key.
- Keep model guidance clear when a request is denied or times out.

Acceptance:

- A `require_approval` call pauses on a pending request instead of only
  returning an artifact.
- A user approval resumes the original tool call.
- A denial returns a clear tool result and telemetry record.
- `ApprovedForSession` prevents repeated prompts for the same stable request.

### 3. Runtime orchestrator

- Move policy, approval, execution, telemetry, and retry behavior into a central
  tool execution orchestrator.
- Keep per-tool input classification, but make the orchestrator responsible for
  deciding whether approval is needed.
- Add permission-profile checks for read-only and workspace-write behavior.
- Keep existing hard tool refusals, such as unsafe shell/package/git paths,
  until a stronger sandbox layer exists.

Acceptance:

- Read-only mode blocks mutating tools before execution.
- Default and Approve for me share the same permission boundary.
- Full Access is the only mode that skips approvals by policy.

### 4. Automated approval reviewer

- Implement an `auto_review` approval reviewer after manual approval works.
- The reviewer should receive a compact approval request, current workspace
  context, and the Codex-style risk policy.
- The reviewer must be read-only: no writes, no shell mutation, no network
  escalation.
- Reviewer errors, malformed output, and timeouts deny the request.

Acceptance:

- Approve for me routes existing approval requests to the reviewer.
- The reviewer can approve low/medium-risk scoped actions.
- The reviewer denies destructive, credential-seeking, exfiltration, and
  persistent security-weakening actions.
- Failures fail closed.

### 5. Verification

- Add Go tests for config validation, preset resolution, approval routing,
  approval caching, and reviewer fail-closed behavior.
- Add desktop tests for the permission menu labels and selected mode mapping.
- Run focused Go tests and focused desktop tests after each independent step.
- Before completion, verify the real app-server initialize/config response
  includes the new permission summary.

## Non-goals For The First Pass

- Do not present Approve for me as equivalent to Full Access.
- Do not remove existing advanced `tool_policy` overrides until migration is
  complete.
- Do not claim OS-level sandbox parity until Wuu has an equivalent sandbox
  implementation.
- Do not make the automated reviewer silently allow requests when it fails.
