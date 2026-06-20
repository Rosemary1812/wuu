---
name: safe-edit
description: Make focused edits without overwriting unrelated work.
trigger-condition: Use before editing files in a dirty worktree or shared project.
allowed-tools: [read_file, apply_patch, bash]
required-context: [git status, target files, user-owned changes, verification command]
examples: [patch a Go package, update a protocol type, avoid renderer changes]
verification-checklist: [only intended files changed, user changes preserved, tests or formatting run]
progressive-disclosure: Read each target file and nearby tests before editing it.
---

# Safe Edit

Use precise patches and preserve unrelated changes.

1. Check git status.
2. Read the target file and nearby tests.
3. Patch only the intended lines.
4. Run formatters and targeted tests for touched code.
5. Inspect the final diff before reporting completion.

Never reset, checkout, or overwrite user changes unless explicitly asked.
