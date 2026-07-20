# wuu documentation

wuu is a local-workspace AI agent that starts with software development. It
works directly in a folder you choose: reading and editing files, running
commands, checking results, and continuing work across sessions.

Use the desktop app for interactive work or `wuu exec` from terminals, scripts,
CI, and other agents. Both surfaces share the same Go core, while the desktop
ships its own private core and does not depend on a separately installed CLI.

## Start here

- [User guide](getting-started/index.md) — install wuu, connect a provider, and
  complete a first task in a local workspace.
- [`wuu exec`](automation/exec.md) — drive wuu from scripts, CI, or another agent.
- [JSONL events](automation/jsonl-events.md) — consume structured streaming output.
- [App-server protocol](integrations/app-server-protocol.md) — build another shell
  around the core.
- [Security model](reference/security-model.md) — understand trust and data boundaries.
- [Development guide](project/development.md) — build and test the project.

## Files are the durable result

Conversation is how you state goals, add context, and inspect progress. The
durable result lives in workspace files, where it remains available to editors,
Git, and other tools instead of being locked inside one chat.

The current public product focuses first on software-development workflows.
These docs describe behavior available today rather than presenting planned
writing, knowledge-management, or publishing features as shipped.

---

[简体中文文档](../zh-cn/index.md)
