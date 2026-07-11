---
name: cua-mac
description: Observe and control native macOS apps through Accessibility, ScreenCaptureKit, and native input. Use when a task requires interacting with a Mac app that has no safer CLI, API, or dedicated connector.
---

# Computer Use for Mac

Use the `computer` tool from the `cua-mac` plugin for native macOS app interaction.

## Control model

- Prefer a dedicated API, connector, or CLI when it can complete the task.
- Start with `list_apps` when the target app is unclear, otherwise call `observe` with its display name or bundle identifier.
- Include `app` on every `observe`, click, drag, key, typing, scroll, value, selection, action, and wait call. The target is not inherited from the previous call.
- Prefer fresh `element_id` values from the latest Accessibility snapshot.
- Use `press`, `set_value`, and other semantic AX actions before coordinate input.
- Re-observe after every group of actions. Never reuse stale element ids or screenshot coordinates.
- Semantic AX actions are preferred. When they are unavailable, the tool automatically activates the target app and uses native mouse or keyboard events.
- If background screenshot capture fails, `observe` activates the target app and retries once in the foreground. Treat an eventual screenshot error as a capture failure, not proof that the app cannot be controlled.
- Coordinate, drag, key, typing, and scroll actions may take over the foreground app, pointer, or keyboard. This is normal CUA behavior and does not require a separate focus-control confirmation.

## Permissions

If the tool reports missing Accessibility or Screen Recording access, explain exactly which macOS permission is missing and use the returned Settings URL. Do not claim an action succeeded when permission was denied.

## Risk

Ordinary observation and reversible edits do not need extra confirmation. Confirm immediately before an irreversible or externally consequential action such as deleting data, sending a message, publishing, changing security settings, or completing a payment.
