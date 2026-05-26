## Execution Autonomy

- Must complete tasks end-to-end without asking the user for confirmation on small or medium-sized implementation decisions.
- Must make local naming, refactoring, implementation, and routine workflow decisions independently when they are reversible and do not change security, architecture, or product scope.
- Must create atomic commits during multi-step work as each independent step is completed and verified. Must not bundle unrelated steps into one final commit.
- Must apply existing knowledge, tacit engineering knowledge, and established best practices directly. Must not ask about standard implementation details that a competent engineer should already know.
- Must not interrupt execution for trivial questions, obvious choices, or routine best-practice decisions. Ask only when the choice is irreversible or materially affects security, architecture, or product behavior.

## Product Stage and Development Bias

- This project is in a high-velocity product iteration stage. Optimize for quickly shipping coherent user-facing behavior, not for preserving existing implementation details by default.
- Product intent from the user is the primary source of truth. Existing behavior is evidence, not authority; if the current behavior conflicts with the intended user experience, change the behavior directly.
- Default to developing directly on the current `main` branch in this repository. Do not create a new branch, worktree, or detached work area unless the user explicitly asks for one, the user asks for parallel isolated work, or a concrete safety reason requires isolation.
- Work in small, atomic steps on `main`: each independent behavior change should be implemented, verified, and committed separately. Do not accumulate unrelated changes for one large final commit.
- Prefer decisive product fixes over narrow patches that only silence the immediate symptom. If the root problem is a mismatched product model, fix the model rather than adding local guardrails around the symptom.
- Avoid unrelated changes, not necessary product changes. If the intended behavior requires changing a broader product model, do that directly and keep unrelated refactors out of the commit.
- Keep engineering discipline proportional to risk: inspect the relevant code first, preserve data safety, avoid avoidable regressions, and verify the actual running product path before claiming completion.
- When validation matters, verify against the real app or runtime the user is using, not only an isolated worktree, stale build, or inferred code path.

## Intent First

- Must start from the user's real goal before optimizing local implementation details. The current codebase is context, not the primary definition of what should be built.
- When a request affects product behavior, must reason first about interaction design, visual design, and the broader project vision before choosing detailed code changes.
- Must not assume the existing implementation is correct just because it already exists. Evaluate whether it actually serves the intended user experience and product direction.
- Must not get trapped in minor technical details when they do not materially affect the user's outcome. Prioritize the highest-leverage product and UX decisions first.
- Must still inspect and understand the relevant code before changing it, but that inspection must support the intended outcome rather than let the current implementation define the goal.

## Third-Party Reference Code

- The `thirdparty/` directory contains reference implementations from related agent, CLI, and product codebases. Treat it as a local research library when the user asks for "industry best practices", "how others do this", "reference implementations", or similar guidance.
- When investigating a best-practice question, inspect relevant `thirdparty/` code, docs, and tests with targeted searches before deciding on an implementation. Prefer close analogues over generic assumptions.
- Use third-party code as evidence, not authority. Adapt useful ideas to this repository's existing patterns; do not blindly copy behavior just because another project does it.

## Agent Design Methodology

- When modifying or reviewing core agent behavior, evaluate the design as a closed loop: if an LLM-facing tool is added or changed, verify that prompts teach the model when to use it and when not to use it, and verify that the tool implementation cannot break provider API invariants such as message ordering, tool-call/result pairing, or other protocol rules that would prevent the API from returning a valid response.

## User-Facing Output

- Must use plain and common words. Must not use obscure words, inflated wording, or needless jargon.
- Must explain progress and completed work from the product view and the user view whenever possible.
- Must tell the user what was changed, what user problem it addresses, and what experience or behavior is now different.
- Must not focus the explanation on internal code details unless those details are necessary to explain impact, risk, or next steps.
- When summarizing work, prefer user impact and product outcome over implementation trivia.

## Desktop Debug Controls

- Desktop debug UI must not appear in production builds. This includes the run debug button/panel, launch animation preview, development conversation fixtures, style preview toggles, and any future developer-only shortcut buttons.
- In development builds, debug UI must still be hidden by default. Expose it only through the debug controls switch in Settings.
- Future desktop debug buttons or developer-only shortcuts must be gated by the same debug controls setting instead of checking development mode directly.
- The debug controls switch itself must only be visible in development builds. Production builds must not show either the switch or the debug buttons it controls.
- If an e2e test needs the run debug panel, enable it explicitly through the test/build path, and keep production e2e coverage that asserts debug controls are not exposed.

## Local Build and Symlink Refresh

- When the user asks to compile or update the local CLI to the latest source, run `make install` in the repo root.
- Treat `~/.local/bin/wuu -> ~/go/bin/wuu` as the default local path, and refresh the binary at the symlink target. Do not repoint the symlink unless the user explicitly asks.
- After install, verify with:
1. `command -v wuu` and `ls -l ~/.local/bin/wuu ~/go/bin/wuu`
2. `go version -m ~/go/bin/wuu` and confirm `vcs.revision` matches current HEAD
3. `wuu --version` (fallback `wuu version`)
- Explicitly tell the user that running `wuu` now uses the latest local build.

## Hydra Orchestration Toolkit

Hydra is a Lead-driven orchestration toolkit. You (the Lead) make strategic
decisions at decision points; Hydra handles operational management.
`result.json` is the only completion evidence.

Why this design (vs. other coding-agent products):
- **SWF decider pattern, specialized for LLM deciders.** Hydra is the AWS SWF / Cadence / Temporal decider pattern. `hydra watch` is `PollForDecisionTask`; the Lead is the decider; `lead_terminal_id` enforces single-decider semantics.
- **Parallel-first, not bolted on.** `dispatch` + worktree + `merge` are first-class. Lead sequences nodes manually and passes context explicitly via `--context-ref`. Other products treat parallelism as open research; Hydra makes it the default.
- **Typed result contract.** Workers publish a schema-validated `result.json` (`outcome: completed | stuck | error`, optional `stuck_reason: needs_clarification | needs_credentials | needs_context | blocked_technical`). Other products return free-text final messages and require downstream parsing.
- **Lead intervention points.** `hydra reset --feedback` lets the Lead actually intervene at decision points instead of being block-and-join. A stale or wrong run is one `reset` away.

Core rules:
- Root cause first. Fix the implementation problem before changing tests.
- Do not hack tests, fixtures, or mocks to force a green result.
- Do not add silent fallbacks or swallowed errors.
- An assignment run is only complete when `result.json` exists and passes schema validation.

Workflow patterns:
1. Do the task directly when it is simple, local, or clearly faster without workflow overhead.
2. Use Hydra for ambiguous, risky, parallel, or multi-step work:
   ```
   hydra init --intent "<task>" --repo .
   hydra dispatch --workbench W --dispatch <id> --role <role> --intent "<desc>" --repo .
   hydra watch --workbench W --repo .
   # → DecisionPoint returned, decide next step
   hydra complete --workbench W --repo .
   ```
3. Use a direct isolated worker when only a separate worker is needed:
   `hydra spawn --task "<specific task>" --repo . [--worktree .]`

Agent launch rule:
- When dispatching Claude/Codex through TermCanvas CLI, start a fresh agent terminal with `termcanvas terminal create --prompt "..."`
- Do not use `termcanvas terminal input` for task dispatch; it is not a supported automation path

Workflow control:
- After dispatching, always call `hydra watch`. It returns at decision points.
1. Watch until decision point: `hydra watch --workbench <workbenchId> --repo .`
2. Inspect structured state: `hydra status --workbench <workbenchId> --repo .`
3. Reset a dispatch for rework: `hydra reset --workbench W --dispatch N --feedback "..." --repo .`
4. Approve a dispatch's output: `hydra approve --workbench W --dispatch N --repo .`
5. Merge parallel branches: `hydra merge --workbench W --dispatches A,B --repo .`
6. View event log: `hydra ledger --workbench <workbenchId> --repo .`
7. Clean up: `hydra cleanup --workbench <workbenchId> --repo .`

Telemetry polling:
1. Treat `hydra watch` as the main polling loop; do not infer progress from terminal prose alone.
2. Before deciding wait / retry / takeover, query:
   - `termcanvas telemetry get --workbench <workbenchId> --repo .`
   - `termcanvas telemetry get --terminal <terminalId>`
   - `termcanvas telemetry events --terminal <terminalId> --limit 20`
3. Trust `derived_status` and `task_status` as the primary decision signals.

`result.json` must contain (slim, schema_version `hydra/result/v0.1`):
- `schema_version`, `workbench_id`, `assignment_id`, `run_id` (passthrough IDs)
- `outcome` (completed/stuck/error — Hydra routes on this)
- `report_file` (path to a `report.md` written alongside `result.json`)

All human-readable content (summary, outputs, evidence, reflection) lives in
`report.md`. Hydra rejects any extra fields in `result.json`. Write `report.md`
first, then publish `result.json` atomically as the final artifact of the run.

When NOT to use: simple fixes, high-certainty tasks, or work that is faster to do directly in the current agent.
