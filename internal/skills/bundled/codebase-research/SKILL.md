---
name: codebase-research
description: Inspect the repository before planning or editing.
trigger-condition: Use when a task requires architecture understanding, relevant file discovery, or current behavior audit.
allowed-tools: [read_file, list_files, grep, glob, run_shell]
required-context: [task goal, repo rules, current git status, relevant files, existing tests]
examples: [audit current architecture, find where tool calling is implemented, inspect Electron entrypoints]
verification-checklist: [relevant files cited, current behavior separated from assumptions, open questions recorded]
progressive-disclosure: Start with file and symbol search, then read only the files that explain the target behavior.
---

# Codebase Research

Use this before non-trivial implementation.

1. Record the task goal and repository rules.
2. Check git status and avoid unrelated user changes.
3. Search broadly for relevant packages, symbols, commands, and tests.
4. Read the smallest set of files that explains the behavior.
5. Summarize existing capabilities, gaps, risks, and the next concrete step.

Do not propose changes to code you have not inspected.
