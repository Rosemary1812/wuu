# Read-only system event model

## Goal

System events in the conversation timeline are status facts, not controls. They must present one clear user-facing message without mixing diagnostic identifiers or actions into the same row.

## Scope

The model covers turn outcomes, missing replies, stream reconnects, context compaction, and agent handoffs. Workspace-focus and cross-conversation dividers remain conversation metadata, but follow the same read-only visual contract.

## Product contract

- A system event has no clickable action.
- The row shows exactly one visible label between two quiet divider lines.
- A machine code such as `internal_error` remains diagnostic data and is never rendered beside the label.
- Error events use the error tone; warning and neutral lifecycle events use their corresponding tones.
- In-progress events may use motion and live-region announcements, but remain non-interactive.
- Optional detail is available as hover and accessibility text. It is not a second visible label.
- Narrow layouts may truncate the label; the complete label and detail remain available through the host description.

Example:

```text
---------------- 404 资源不存在 ----------------
```

## Architecture

The Go core owns error facts: raw message, category, provider, status code, and machine code. It does not prescribe shell actions. The shared protocol carries those facts without a `TurnErrorAction`.

The Electron renderer maps turn and item facts into a shell-level `SystemEventDisplay` view model:

- `kind`: stable semantic event kind.
- `tone`: neutral, warning, auth, or error.
- `state`: settled or in progress.
- `label`: the only visible user-facing text.
- `detail`: optional hover and accessibility description.

All timeline system events render through one read-only primitive. Specialized mapping functions may still derive context-compaction and reconnect copy, but they produce the same display shape.

## Data flow

1. The core classifies a failed turn and emits diagnostic facts.
2. The renderer maps those facts to a short Chinese label and optional detail.
3. The system event component renders one non-interactive label.
4. Diagnostic codes remain available in state and logs, but never appear in the event row.

## Migration

- Remove `TurnErrorAction` and `action` from Go and TypeScript protocol types.
- Remove renderer `UserFacingErrorAction`, `recommendedActions`, and action translation.
- Remove `onNoticeAction` prop plumbing and the runtime action handler.
- Replace the code/title/action composition with the unified read-only event display.
- Remove obsolete action and machine-code CSS.
- Update the debug gallery and tests to enforce the read-only contract.

## Validation

- Core error classification tests continue to cover category, code, provider, and status.
- Protocol serialization tests confirm no action field is emitted.
- Renderer tests confirm system event rows contain no links or buttons and expose one visible label.
- Existing event tests continue to cover interruption, missing reply, reconnect, compaction, and handoff states.
- Desktop tests and a real development render verify the final appearance.
