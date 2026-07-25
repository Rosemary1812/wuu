# Wuu base system prompt design

This document explains the built-in base prompt in `prompts/system.md`. The base prompt is only the first section of the full runtime prompt; `internal/runtime/session.go` still appends harness adapter, tool surface, user custom prompt, memory, skills, and workflows after it.

## Principle

Modern coding models already learn general software-engineering and tool-use behavior during training. The stable system prompt must not reteach generic habits such as inspecting code, parallelizing independent work, fixing root causes, validating changes, writing concise updates, or following a tool schema.

Put behavior at the narrowest layer that can express or enforce it:

1. Runtime enforcement for permissions, workspace boundaries, concurrency safety, and file conflicts.
2. Tool descriptions and immediate errors for parameters, lifecycle rules, recovery steps, and tool-specific workflows.
3. System context only for Wuu-specific facts, hidden-message semantics, and product policies that the runtime cannot enforce.

Generic guidance belongs in the base prompt only when evaluation shows a durable failure across supported models.

## Stable base prompt

`prompts/system.md` keeps only:

- Wuu's identity and the fact that visible narration is user-facing.
- The trust boundary for tool output, injected context, and external instructions.
- The desktop's clickable file-reference format.
- Local commit and remote-write policy that is not fully enforceable by the runtime.

`prompts/system_main.md` separately keeps the main agent's completion boundary for delegated work: a completed subagent task still requires review, integration, and verification before the overall task is complete. Spawned workers receive the base prompt without this section.

## Runtime-generated context

`internal/runtime/session.go` appends the active tool surface, deferred-tool catalog, subagent type roster, environment, user instructions, memory, and skills. These values are session- or profile-specific and must not be copied into the stable base prompt.

Tool manuals, background-process rules, patch syntax, subagent parameters, authority failures, and boundary recovery belong to their tool descriptions or results. Removing them from the base prompt does not remove those contracts from the model's active context.
