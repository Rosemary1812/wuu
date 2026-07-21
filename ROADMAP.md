# Roadmap

[简体中文](ROADMAP_zh.md)

wuu is a pre-1.0 project. The current priority is to make its existing coding
workflows reliable and easy to inspect before expanding into larger workspace
and multi-agent features.

This is a direction, not a release schedule. Follow the linked issues for full
designs and progress. See the [changelog](CHANGELOG.md) for shipped work.

## Current focus

- **Make background work and interruption predictable.** Background commands
  and processes that survive an app-server restart currently have conflicting
  ownership and recovery rules, making it hard to know whether work is still
  alive or controllable. Interrupting a response can also delete messages the
  user already queued. We want one clear lifecycle that preserves user intent.
  ([#157](https://github.com/blueberrycongee/wuu/issues/157),
  [#31](https://github.com/blueberrycongee/wuu/issues/31))

- **Make changes and command history easier to review.** Patch results currently
  serve the model, desktop UI, and durable audit history in one payload, even
  though each needs different detail. Command output is also difficult to revisit
  after a turn. We want compact model feedback alongside a complete user-facing
  record in the change and terminal workspaces.
  ([#151](https://github.com/blueberrycongee/wuu/issues/151),
  [#103](https://github.com/blueberrycongee/wuu/issues/103))

- **Close gaps in everyday desktop workflows.** Files cannot be dragged into the
  composer, scheduled tasks have no central management screen, and the environment
  panel does not show enough upstream, PR, or CI state.
  ([#130](https://github.com/blueberrycongee/wuu/issues/130),
  [#135](https://github.com/blueberrycongee/wuu/issues/135),
  [#57](https://github.com/blueberrycongee/wuu/issues/57))

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

These problems are important, but the solutions are not scheduled:

- **Repository knowledge is fragmented** across code, docs, issues,
  conversations, and people's memory, so humans and agents repeatedly reconstruct
  the same context. Explore a shared codebase knowledge workspace.
  ([#36](https://github.com/blueberrycongee/wuu/issues/36))

- **Chat-shaped multi-agent collaboration breaks down as the group grows.** Agents
  compete to write, produce overlapping wall-of-text replies, and confuse group
  chats with task threads. Explore task-graph collaboration with named agents.
  ([#138](https://github.com/blueberrycongee/wuu/issues/138))

- **The embedded webview cannot reuse a user's existing browser profile and
  limits deeper agent integration.** Explore a fuller browser surface with
  explicit credential and permission controls.
  ([#96](https://github.com/blueberrycongee/wuu/issues/96))

Priorities may change when core bugs, security issues, or user feedback reveal a
more important problem. Suggestions are welcome in
[GitHub Issues](https://github.com/blueberrycongee/wuu/issues).
