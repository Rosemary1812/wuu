---
name: cua-mac
description: Observe and control native macOS apps through Accessibility, ScreenCaptureKit, and native input. Use when a task requires interacting with a Mac app that has no safer CLI, API, or dedicated connector.
---

# Computer Use for Mac

Use the `computer` tool from the `cua-mac` plugin for native macOS app interaction.

## Control model

- Prefer a dedicated API, connector, or CLI when it can complete the task.
- Start with `list_apps` when the target app is unclear, otherwise call `observe` with its display name or bundle identifier.
- `observe` returns a `Target app="..."` header. Copy that exact canonical value into `app` on every later call; do not replace it with the window title, localized display name, or a guessed English name.
- Include `app` on every observe, click, drag, key, typing, scroll, value, selection, action, and wait call. The target is not inherited from the previous call.
- Prefer fresh `element_id` values from the latest Accessibility snapshot.
- Use semantic element actions before coordinate input. When two or more ordered keys can express the interaction, represent the whole sequence with one `press_keys` call. Do not expand that sequence into multiple click or `press_key` calls. Use `press_key` only for one key. Never issue ordered UI actions as parallel tool calls; parallel completion order is not execution order.
- Action calls return a short acknowledgement, not fresh UI state. Perform a small ordered group of actions, then call `observe` to verify the result and obtain fresh element IDs and screenshot coordinates.
- Reuse element IDs only within the state established by the latest successful `observe`. If an app restarts, an action fails, or the visible layout changes unexpectedly, observe again before choosing the next action.
- Prefer `element_id` or a known keyboard control. For coordinate input, set `coordinate_space="screenshot"` for pixels measured in the latest captured image, or `coordinate_space="screen"` for global values copied from an AX `frame=(x,y,w,h)`. Never mix the two spaces.
- Semantic AX actions are preferred. When they are unavailable, the tool automatically activates the target app and uses native mouse or keyboard events.
- If background screenshot capture fails, `observe` activates the target app and retries once in the foreground. Treat an eventual screenshot error as a capture failure, not proof that the app cannot be controlled.
- Coordinate, drag, key, typing, and scroll actions may take over the foreground app, pointer, or keyboard. This is normal CUA behavior and does not require a separate focus-control confirmation.

## Permissions

Do not call `permission_status` proactively. Start with the requested observation or action. If the tool reports missing Accessibility or Screen Recording access, explain exactly which macOS permission is missing and use the returned Settings URL. Do not claim an action succeeded when permission was denied.

## Risk

Ordinary observation and reversible edits do not need extra confirmation. Confirm immediately before an irreversible or externally consequential action such as deleting data, sending a message, publishing, changing security settings, or completing a payment.
