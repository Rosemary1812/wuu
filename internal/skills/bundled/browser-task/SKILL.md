---
name: browser-task
description: Verify local web or Electron renderer behavior with browser evidence.
trigger-condition: Use when a task changes UI, browser automation, DOM state, screenshots, or visual behavior.
allowed-tools: [read_file, run_shell, run_test, browser, screenshot]
required-context: [target URL or app path, viewport, changed UI files, expected user behavior]
examples: [open localhost, smoke test a settings panel, capture screenshot after UI change]
verification-checklist: [page opened, console errors checked, DOM or screenshot evidence recorded]
progressive-disclosure: Start the local app only after the relevant code path is known.
---

# Browser Task

Use browser evidence for UI-facing changes.

1. Identify the route, component, and expected state.
2. Start or reuse the correct local app server.
3. Open the target in the in-app browser when available.
4. Check page load, console errors, DOM state, and screenshot framing.
5. Record failures in durable loop state.

Do not claim UI verification from code inspection alone when the running app matters.
