---
name: diff-review
description: Review a diff for real bugs, regressions, and missing verification.
trigger-condition: Use after implementation and before declaring work complete.
allowed-tools: [read_file, grep, glob, run_shell, run_test]
required-context: [final diff, acceptance criteria, verification results, touched tests]
examples: [review worker role changes, inspect appserver schema diff, check test coverage]
verification-checklist: [findings are actionable, no style-only nits, verification gaps stated]
progressive-disclosure: Read changed files plus the nearest callers and tests only when needed to judge behavior.
---

# Diff Review

Review from a maker/checker stance.

1. Inspect the final diff.
2. Look for behavior regressions, protocol mismatches, data loss, security issues, and missing tests.
3. Check that verification covers the changed behavior.
4. Report findings first, ordered by severity.
5. Say "no issues found" when there are no real issues.

Do not invent issues to look thorough.
