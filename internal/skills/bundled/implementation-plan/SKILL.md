---
name: implementation-plan
description: Turn research into a scoped, verifiable implementation plan.
trigger-condition: Use after research and before broad or multi-file edits.
allowed-tools: [read_file, grep, glob]
required-context: [research summary, acceptance criteria, touched areas, verification commands]
examples: [plan a refactor, split a workflow upgrade, define P0 changes]
verification-checklist: [steps are ordered, each step has verification, irreversible choices are called out]
progressive-disclosure: Include only the constraints needed for the next implementation step.
---

# Implementation Plan

Create a short plan that can be executed and verified.

1. State the intended user-visible behavior.
2. List the smallest implementation steps in dependency order.
3. Attach a verification command or manual check to each step.
4. Identify files likely to change and files that must not be touched.
5. Mark any approval gate before high-risk actions.

Do not enter large edits without a plan artifact.
