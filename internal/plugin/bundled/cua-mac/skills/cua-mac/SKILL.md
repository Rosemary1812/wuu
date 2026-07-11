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
- Action calls return a short acknowledgement, not fresh UI state. Perform a small ordered group of actions, then call `observe` to refresh both the AX tree and screenshot, verify the visible result, and obtain fresh element IDs and screenshot coordinates. Repeating the same `observe` input after different UI actions is normal verification, not a reason to reuse an old screenshot.
- Reuse element IDs only within the state established by the latest successful `observe`. If an app restarts, an action fails, or the visible layout changes unexpectedly, observe again before choosing the next action.
- Prefer `element_id` or a known keyboard control. For visually located targets, use `coordinate_space="normalized"` on a 0-1000 grid (`x/visible_width*1000`, `y/visible_height*1000`); this remains valid if a provider resizes or compresses the image while preserving its aspect ratio. Use `coordinate_space="screenshot"` only for pixels known to come from the original screenshot dimensions, or `coordinate_space="screen"` for global values copied from an AX `frame=(x,y,w,h)`. The tool handles Retina and window mapping. Never mix coordinate spaces or infer a display scale.
- Treat AX and pixels as separate evidence. An AX tree can remain unchanged while a weak-AX app visibly changes. After a coordinate action, use a fresh `observe` and compare the new screenshot; do not use `wait_for_change` when the expected change may be visual-only.
- If a coordinate click produces no visible result, do not repeatedly click guessed nearby points. Refresh once, re-read the screenshot dimensions and visible target, then choose a materially different semantic, keyboard, menu, or corrected-coordinate path. Stop and report the unverified step if no such path is grounded in fresh evidence.
- Semantic AX actions are preferred. When they are unavailable, the tool automatically activates the target app and uses native mouse or keyboard events.
- Leave `foreground_policy` omitted (or set it to `avoid`) by default. Background AX actions continue without changing the user's active app. If an action or screenshot requires native foreground access, the tool returns `requires_foreground` instead of silently activating the app.
- Set `foreground_policy="allow"` only when the task cannot be completed through AX and a temporary foreground takeover is acceptable. Use `require` only when the user explicitly asked to show or take over the target app. After a foreground action, observe again because focus and layout may have changed.

## Permissions

Do not call `permission_status` proactively. Start with the requested observation or action. If the tool reports missing Accessibility or Screen Recording access, explain exactly which macOS permission is missing and use the returned Settings URL. Do not claim an action succeeded when permission was denied.

## Risk

Ordinary observation and reversible edits do not need extra confirmation. Confirm immediately before an irreversible or externally consequential action such as deleting data, sending a message, publishing, changing security settings, or completing a payment.
