# Progressive tool loading foundation

## Reference findings

Claude Code uses `defer_loading` on tool schemas and returns `tool_reference`
blocks from `ToolSearchTool`. It reconstructs discovered tool names from
message history and compact metadata, keeps built-in tools as a stable cache
prefix, and excludes deferred tool schemas from prompt-cache break detection.

Codex uses Responses `tool_search_call` and `tool_search_output.tools`.
Deferred tool definitions carry `defer_loading: true`; search results stay in
conversation history and follow-up requests do not inject matched tools into the
ordinary top-level `tools` array. Codex also exposes small MCP sets directly and
defers larger sets behind `tool_search`.

## Current wuu gap

wuu already has `tool_search`, deferred MCP and low-frequency tools, a stable
built-in tool prefix, and thread-scoped prompt cache keys. The gap is that
`tool_search` currently stores matches in `Toolkit.activatedDeferredTools`.
That fallback makes matched tools visible on the next turn by changing the
ordinary `tools` array, so discovered tool state is not represented in provider
protocol history.

## Stage 0 / foundation boundary

This stage adds provider-neutral structure without enabling provider-native
progressive loading by default:

- `providers.ToolDefinition.DeferLoading` lets provider adapters serialize
  native deferred tool declarations when a future planner supplies them.
- `providers.LoadableToolDefinition` is the shared shape for full tool schemas
  returned by discovery.
- `tool_search` returns `loadable_tools` while keeping `exposed_tools` and the
  existing next-turn activation fallback.
- OpenAI Responses serializes `defer_loading` only when a tool definition is
  explicitly marked. Chat Completions and Anthropic continue to ignore it.

## Stage 1 / OpenAI Responses native route

The Responses adapter now has a provider-native tool-search loop aligned with
Codex's `tool_search_call` and `tool_search_output.tools` design:

- Top-level `tool_search` is serialized as a native Responses tool:
  `{"type":"tool_search","execution":"client",...}` rather than as an ordinary
  function tool.
- Non-streaming and streaming Responses output items of type
  `tool_search_call` are parsed into a normal executable wuu tool call named
  `tool_search`, tagged with `ToolCallKindToolSearch`.
- Tool execution preserves that kind on the follow-up tool result message.
- Responses history replays a `tool_search` result as
  `tool_search_output` with `status:"completed"`, `execution:"client"`, and a
  `tools` array converted from `loadable_tools`.
- If a searched tool appears in `tool_search_output.tools`, the Responses
  adapter omits that same tool from the ordinary top-level `tools` array on
  follow-up requests. The model should rely on the provider-native history
  item instead of a changing top-level tool list.
- Failed or empty `tool_search` results still replay as
  `tool_search_output.tools: []`, keeping tool-call/tool-output pairing valid.
- The kind marker is stored in JSONL/session history so resume and fork paths
  can reconstruct the native output shape.

Chat Completions, Anthropic without native support, and the existing
`Toolkit.activatedDeferredTools` fallback remain available. The fallback still
matters for non-Responses providers and for later Anthropic gating.

## Next stages

Anthropic should add gated support for `defer_loading` plus `tool_reference`
only when the selected provider and model support the required beta behavior.
It also needs message-history and compact-boundary recovery for discovered tool
names before the native route is enabled.

MCP exposure should later adopt a size-aware policy: small direct sets can stay
inline, while large or volatile sets should enter the deferred search pool.
