---
name: ci-failure-triage
description: Diagnose failing CI by preserving logs and finding the root cause first.
trigger-condition: Use when tests, lint, typecheck, build, or CI jobs fail.
allowed-tools: [read_file, grep, glob, run_shell, run_test]
required-context: [failing command, stderr tail, affected files, recent diff]
examples: [triage go test failure, inspect npm typecheck error, reproduce CI locally]
verification-checklist: [failure captured, root cause stated, fix verified or blocker recorded]
progressive-disclosure: Summarize long logs and load only the failing stack, package, or file.
---

# CI Failure Triage

Treat failure output as state, not transient terminal noise.

1. Capture the failing command and relevant output.
2. Reproduce with the narrowest local command.
3. Inspect implicated files and recent changes.
4. Form one concrete hypothesis before patching.
5. Rerun the same command and record the result.

If a failure cannot be reproduced or fixed, record the blocker and next command to run.
