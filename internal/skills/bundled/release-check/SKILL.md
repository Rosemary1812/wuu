---
name: release-check
description: Run final release gates before publishing or handing off a build.
trigger-condition: Use before release, packaging, publish, install, or production build claims.
allowed-tools: [read_file, grep, glob, run_shell, run_test]
required-context: [target version, changed files, release command, verification policy, known risks]
examples: [run make install verification, check desktop production debug gates, summarize release blockers]
verification-checklist: [build command passed, version identity checked, release blockers documented]
progressive-disclosure: Load release-specific docs and commands only after identifying the release target.
---

# Release Check

Use a release gate before claiming a build is ready.

1. Identify the release target and required commands.
2. Confirm production-only constraints such as hidden debug UI.
3. Run build, test, lint, and packaging checks that apply.
4. Verify version or binary identity when a local install is requested.
5. Record blockers and residual risks.

Do not publish, push, or mutate external systems without an explicit approval gate.
