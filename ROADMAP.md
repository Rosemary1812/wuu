# wuu roadmap

[简体中文](ROADMAP_zh.md)

wuu is working toward a dependable, open, BYOK coding agent that can be used
interactively from the desktop or driven through a stable core by scripts, CI,
other agents, and future shells.

This roadmap describes **outcomes**, not a fixed release calendar. Priorities may
change as we learn from real use. GitHub issues hold implementation details;
the [changelog](CHANGELOG.md) records what has actually shipped.

## How to read this roadmap

- **Now** — the current focus. These outcomes should become dependable before
  wuu expands much further.
- **Next** — the next product layer, after the current foundation is sound.
- **Later** — directions we want to explore, but have not scheduled.

An item appearing here is not a promise of a date or an exact design. It is a
statement of product direction.

## Product principles

- **Finish real work.** Optimize for verified changes in the workspace, not for
  impressive-looking conversations.
- **Make agent work inspectable.** Users should be able to understand what ran,
  what changed, what it cost, and what needs attention.
- **Recover instead of restart.** Long tasks, background work, interruptions,
  and app restarts should have explicit, durable states.
- **Fit existing environments.** Reuse project instructions, skills, models,
  and automation where it is safe instead of forcing a wuu-only setup.
- **Keep one reusable core.** Desktop, CLI automation, and future shells should
  share behavior through the Go core and `app-server` protocol.
- **Earn trust before adding power.** New integrations and native capabilities
  need clear sources, permissions, lifecycle controls, and failure states.

## Now — make daily coding work dependable

### Reliable execution and recovery

Long-running commands, subagents, interruptions, queued input, cancellation,
and resume should behave as one understandable lifecycle. A task should not
silently disappear, continue after its owner is gone, or leave the user unsure
whether work is still running.

Related work: [#157](https://github.com/blueberrycongee/wuu/issues/157),
[#156](https://github.com/blueberrycongee/wuu/issues/156), and
[#31](https://github.com/blueberrycongee/wuu/issues/31).

### Reviewable, accountable changes

Make file edits, commands, and agent-produced change sets easy to inspect and
hand off to an independent review session. Keep model-facing tool output small
without losing the durable audit trail a user needs.

Related work: [#151](https://github.com/blueberrycongee/wuu/issues/151) and
[#103](https://github.com/blueberrycongee/wuu/issues/103).

### Complete the everyday desktop loop

Reduce friction around adding files, following command activity, switching Git
contexts safely, managing scheduled work, and seeing repository, PR, and CI
state. These should feel like parts of one workspace rather than disconnected
panels.

Related work: [#130](https://github.com/blueberrycongee/wuu/issues/130),
[#135](https://github.com/blueberrycongee/wuu/issues/135),
[#57](https://github.com/blueberrycongee/wuu/issues/57), and
[#56](https://github.com/blueberrycongee/wuu/issues/56).

### Keep BYOK providers understandable

Make model availability and request usage easier to understand without storing
private prompt content. Keep compatible model catalogs current without requiring
a new wuu release for every model change.

Related work: [#148](https://github.com/blueberrycongee/wuu/issues/148) and
[#119](https://github.com/blueberrycongee/wuu/issues/119).

## Next — fit the user's existing development environment

### Safe migration and extension compatibility

Open existing Codex and Claude Code projects with useful text assets available
immediately, while requiring explicit trust for external connections and
executable extensions. Never migrate secrets silently or rewrite another
tool's source configuration.

Related work: [#153](https://github.com/blueberrycongee/wuu/issues/153).

### A workspace for generated artifacts

Let a conversation create and refine durable outputs in a first-class workspace:
code and Markdown first, then interactive web previews and document formats such
as DOCX and PPTX. Files remain the source of truth and stay usable outside wuu.

Related work: [#154](https://github.com/blueberrycongee/wuu/issues/154) and
[#20](https://github.com/blueberrycongee/wuu/issues/20).

### A stable core for more clients

Strengthen `app-server` as the contract for desktop, automation, and future
editor integrations. New shells should reuse sessions, tools, permissions, and
provider behavior rather than reimplementing the agent runtime.

## Later — expand from coding agent to collaborative workspace

These are promising directions, not scheduled commitments:

- A human-and-agent codebase knowledge workspace with durable links and views
  ([#36](https://github.com/blueberrycongee/wuu/issues/36)).
- Task-graph collaboration with named agents instead of chat-room-shaped
  orchestration ([#138](https://github.com/blueberrycongee/wuu/issues/138)).
- A deeper browser surface with controlled credential reuse and agent-native
  interaction ([#96](https://github.com/blueberrycongee/wuu/issues/96)).
- Additional shells and broader packaged desktop platform support, built on the
  same core rather than separate implementations.

## What 1.0 means

1.0 does **not** require every Later item. It means the supported core workflow
is dependable enough that users can build habits and integrations around it.
Before calling wuu 1.0, we expect:

- supported desktop and `wuu exec` tasks have explicit running, completed,
  failed, interrupted, and recoverable states;
- no known high-severity data-loss or cross-workspace isolation failures remain
  in supported workflows;
- users can inspect material file changes and command activity before accepting
  the result;
- provider setup, common failure states, security boundaries, and automation
  contracts are documented and covered by release checks;
- `app-server` has a documented compatibility policy for external clients;
- packaged releases pass reproducible product gates, and platform, signing, and
  preview limitations are stated plainly.

Until then, wuu remains pre-1.0 and may change quickly.

## How priorities are maintained

- The roadmap is reviewed when a release changes product direction or completes
  a major outcome.
- Bugs involving data safety, security, or broken core workflows can override
  the order above.
- Concrete work belongs in a GitHub issue with a user problem, scope, and
  acceptance criteria. The roadmap links to issues; it does not duplicate their
  task lists.
- Completed work moves to the [changelog](CHANGELOG.md) instead of remaining here
  as a growing checklist.

Ideas and corrections are welcome in
[GitHub Issues](https://github.com/blueberrycongee/wuu/issues).
