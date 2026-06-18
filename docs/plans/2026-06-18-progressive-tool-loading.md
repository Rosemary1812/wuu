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

## Stage 2 / Anthropic native route

The Anthropic adapter now supports the Claude Code style native route behind a
strict gate:

- Native tool search is enabled only when the request includes `tool_search`,
  the model name is not a Haiku model, and either the base URL is first-party
  Anthropic (`api.anthropic.com`) or the caller explicitly sets
  `anthropicToolSearch` / `toolSearch` / `tool_search` to true.
- When enabled, the request adds Anthropic's first-party tool-search beta
  header: `advanced-tool-use-2025-11-20`.
- `tool_search` tool results are mapped to Anthropic `tool_result` blocks whose
  content is an array of `tool_reference` blocks:
  `{"type":"tool_reference","tool_name":"..."}`.
- Discovered tools are included in Anthropic `tools` with
  `defer_loading:true`, so Anthropic can expand the references while keeping
  deferred schemas out of the stable prompt cache surface.
- Haiku models and third-party/proxy Anthropic-compatible endpoints stay on the
  existing string `tool_result` fallback unless explicitly enabled.
- Streaming and non-streaming Anthropic tool-use parsing marks `tool_search`
  calls with `ToolCallKindToolSearch`, so the tool result can be reconstructed
  after history persistence and replay.

## Next stages

Compact-boundary recovery still needs to preserve discovered tool names after
summaries replace the original `tool_search` result messages. Resume and fork
are covered for normal history because the tool-call kind and tool result kind
are now persisted.

MCP exposure should later adopt a size-aware policy: small direct sets can stay
inline, while large or volatile sets should enter the deferred search pool.
