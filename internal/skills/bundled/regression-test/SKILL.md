---
name: regression-test
description: Add or run focused tests that prove a behavior stays fixed.
trigger-condition: Use after a bugfix, behavior change, or loop workflow change.
allowed-tools: [read_file, apply_patch, run_test, run_shell]
required-context: [changed behavior, failure mode, target package, existing test style]
examples: [cover state persistence, test worktree conflict detection, verify skill routing]
verification-checklist: [test fails for the old bug when feasible, test passes after fix, command recorded]
progressive-disclosure: Read the closest existing tests and mirror their style before adding new coverage.
---

# Regression Test

Prefer a small test that exercises the real code path.

1. Identify the exact behavior that could regress.
2. Locate the nearest existing test file.
3. Add the smallest useful case.
4. Run the targeted package test.
5. Record the command and result in the workflow state or final report.

Do not fake a green result by weakening assertions or fixtures.
