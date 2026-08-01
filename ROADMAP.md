# Roadmap

[简体中文](ROADMAP_zh.md)

wuu is a pre-1.0 project. The current priority is to make its existing coding
workflows reliable and easy to inspect before expanding into larger workspace
and multi-agent features.

This is a direction, not a release schedule. Follow the linked issues for full
designs and progress. See the [changelog](CHANGELOG.md) for shipped work.

## Current focus

- **Make background-work lifecycles predictable.** Background commands and
  processes that survive an app-server restart currently have conflicting
  ownership and recovery rules, making it hard to know whether work is still
  alive or controllable. We want one clear lifecycle.
  ([#157](https://github.com/blueberrycongee/wuu/issues/157))

- **Make background commands easier to review.** Command output can now be
  revisited in the terminal workspace, but the environment panel still cannot
  list live background processes for the current session or open their terminal
  resources directly.
  ([#103](https://github.com/blueberrycongee/wuu/issues/103))

- **Complete repository state in the environment panel.** The environment panel
  still does not show enough upstream, PR, or CI state.
  ([#57](https://github.com/blueberrycongee/wuu/issues/57))

- **Keep model support current and usage understandable.** The bundled model
  catalog is fixed at build time, so new or corrected model information requires
  another wuu release. Provider token totals also cannot explain which request
  components produced fresh input. We want runtime catalog updates and useful
  attribution without storing prompt content.
  ([#148](https://github.com/blueberrycongee/wuu/issues/148),
  [#119](https://github.com/blueberrycongee/wuu/issues/119))

## Planned

- **Reduce setup work when moving from another coding agent.** Existing project
  instructions, preferences, and other useful settings currently have to be
  found and recreated by hand. wuu should discover compatible settings, explain
  their source and destination, and let the user choose what to import without
  silently copying credentials or enabling executable extensions.
  ([#153](https://github.com/blueberrycongee/wuu/issues/153))

- **Give generated work a persistent place beside the conversation.** Today,
  interactive results are limited to the message flow and office documents do
  not have a first-class preview workspace. We want chat-driven web, DOCX, and
  PPTX creation with the current artifact visible beside the conversation while
  files remain the source of truth.
  ([#154](https://github.com/blueberrycongee/wuu/issues/154),
  [#20](https://github.com/blueberrycongee/wuu/issues/20))

## Exploring

This problem is important, but the solution is not scheduled:

- **The embedded webview cannot reuse a user's existing browser profile and
  limits deeper agent integration.** Explore a fuller browser surface with
  explicit credential and permission controls.
  ([#96](https://github.com/blueberrycongee/wuu/issues/96))

Priorities may change when core bugs, security issues, or user feedback reveal a
more important problem. Suggestions are welcome in
[GitHub Issues](https://github.com/blueberrycongee/wuu/issues).
