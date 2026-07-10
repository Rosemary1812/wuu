---
name: cua-mac
description: Observe and control native macOS apps through background-first Accessibility actions. Use when a task requires interacting with a Mac app that has no safer CLI, API, or dedicated connector.
---

# Computer Use for Mac

Use the `computer` tool from the `cua-mac` plugin for native macOS app interaction.

## Control model

- Prefer a dedicated API, connector, or CLI when it can complete the task.
- Start with `list_apps` when the target app is unclear, otherwise call `observe` with its display name or bundle identifier.
- Prefer fresh `element_id` values from the latest Accessibility snapshot.
- Use `press`, `set_value`, and other semantic AX actions before coordinate input.
- Re-observe after every group of actions. Never reuse stale element ids or screenshot coordinates.
- Background-safe actions do not activate the target app or move the user's pointer.
- If an action returns `foreground_required`, report the limitation. Never silently activate an app or synthesize global input.

## Permissions

If the tool reports missing Accessibility or Screen Recording access, explain exactly which macOS permission is missing and use the returned Settings URL. Do not claim an action succeeded when permission was denied.

## Risk

Ordinary observation and reversible edits do not need extra confirmation. Confirm immediately before an irreversible or externally consequential action such as deleting data, sending a message, publishing, changing security settings, or completing a payment.
