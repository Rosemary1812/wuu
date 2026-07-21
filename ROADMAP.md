# Roadmap

[简体中文](ROADMAP_zh.md)

wuu is a pre-1.0 project. The current priority is to make its existing coding
workflows reliable and easy to inspect before expanding into larger workspace
and multi-agent features.

This is a direction, not a release schedule. Follow the linked issues for scope
and progress. See the [changelog](CHANGELOG.md) for shipped work.

## Current focus

| Area | Goal | Tracking |
|---|---|---|
| Runtime | Make background commands, interruption, cancellation, and recovery follow one clear lifecycle | [#157](https://github.com/blueberrycongee/wuu/issues/157), [#31](https://github.com/blueberrycongee/wuu/issues/31) |
| Changes | Keep agent edits and command activity easy to inspect without bloating model context | [#151](https://github.com/blueberrycongee/wuu/issues/151), [#103](https://github.com/blueberrycongee/wuu/issues/103) |
| Desktop | Complete common file, Git, scheduled-task, PR, and CI workflows | [#130](https://github.com/blueberrycongee/wuu/issues/130), [#135](https://github.com/blueberrycongee/wuu/issues/135), [#57](https://github.com/blueberrycongee/wuu/issues/57), [#56](https://github.com/blueberrycongee/wuu/issues/56) |
| Providers | Keep compatible model information current and make request usage easier to understand | [#148](https://github.com/blueberrycongee/wuu/issues/148), [#119](https://github.com/blueberrycongee/wuu/issues/119) |

## Planned

- Reuse compatible settings and project assets from other coding agents without
  silently copying credentials or enabling executable extensions
  ([#153](https://github.com/blueberrycongee/wuu/issues/153)).
- Build a first-class artifact workspace for code, web previews, DOCX, and PPTX
  while keeping files as the source of truth
  ([#154](https://github.com/blueberrycongee/wuu/issues/154),
  [#20](https://github.com/blueberrycongee/wuu/issues/20)).
- Continue strengthening `app-server` as the shared contract for the desktop,
  automation, and future clients.

## Exploring

These ideas are not scheduled:

- A codebase knowledge workspace for humans and agents
  ([#36](https://github.com/blueberrycongee/wuu/issues/36)).
- Task-graph collaboration with named agents
  ([#138](https://github.com/blueberrycongee/wuu/issues/138)).
- A deeper browser surface with controlled credential reuse
  ([#96](https://github.com/blueberrycongee/wuu/issues/96)).
- More client shells and broader packaged desktop platform support.

Priorities may change when core bugs, security issues, or user feedback reveal a
more important problem. Suggestions are welcome in
[GitHub Issues](https://github.com/blueberrycongee/wuu/issues).
