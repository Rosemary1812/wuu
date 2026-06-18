# 渐进式工具加载与 KV/prompt cache 成熟度复盘

> 日期：2026-06-18
> 范围：KV/prompt cache 利用率、provider-native 渐进式工具加载、MCP 工具暴露策略、compact/resume 恢复能力。

## 结论

这次闭环后，wuu 的缓存利用率仍有继续提升空间，但最容易破坏 cache 的一类问题已经被收住：动态 MCP 工具和已发现 deferred 工具不会再插入或扰动稳定工具前缀，也不会在 OpenAI Responses 路径里重新塞回普通顶层 `tools` 数组。

现在的设计更接近 Codex 和 Claude Code 的成熟路径：

- OpenAI/Codex 方向：用原生 `tool_search_call` 和 `tool_search_output.tools` 承载可加载工具，而不是把所有动态工具预先发送给模型。
- Anthropic/Claude Code 方向：只有确认支持 `defer_loading`/`tool_reference` 的路径才启用原生能力；不支持时保留本地 fallback。
- Cache 方向：稳定工具前缀、线程级 prompt cache key、cache read/create token 观测已经打通；缺口主要是还没有 Claude Code 那种运行时 prompt cache break detector。

无法给出真实线上提升百分比，因为这需要同一用户、同一模型、同一任务集下的 `cache_read_tokens` 和 `cache_creation_tokens` 对比。代码层面可以确认的是，工具 schema 变化对 cache key 的扰动显著减少，尤其是 MCP 工具很多或经常变化的项目。

## 参考实现

### Codex

本次主要参考：

- `thirdparty/codex/codex-rs/tools/src/tool_search.rs`
- `thirdparty/codex/codex-rs/tools/src/responses_api.rs`
- `thirdparty/codex/codex-rs/core/tests/common/responses.rs`
- `thirdparty/codex/codex-rs/app-server/README.md`

Codex 的核心设计是：

1. 把可延迟工具转换成 `LoadableToolSpec`，并设置 `defer_loading: true`。
2. `tool_search` 返回可加载工具定义，Responses 请求历史中用 `tool_search_output` 承载。
3. 严格测试 Responses input 里的 call/output 配对，避免 provider API 因消息顺序或缺失输出而失败。
4. dynamic tools 在 app-server 协议中持久化 `defer_loading` 信息。

wuu 已经吸收的部分：

- provider-neutral `LoadableToolDefinition`
- OpenAI Responses 原生 `tool_search_call`/`tool_search_output.tools`
- tool_search 历史回放和 compact 后恢复
- 顶层 tools 数组不重复注入已发现工具
- call/output 配对和 stream/non-stream 测试

仍未完全追平的部分：

- Codex 支持 namespace 型 loadable tools；wuu 当前主要覆盖 function 型工具。
- Codex app-server 协议对 dynamic tools 的表达更完整；wuu 现在够用，但还没有同等通用的动态工具协议层。

### Claude Code

本次主要参考：

- `thirdparty/claude-code-sourcemap/src/services/api/claude.ts`
- `thirdparty/claude-code-sourcemap/src/services/api/promptCacheBreakDetection.ts`
- `thirdparty/claude-code-sourcemap/src/Tool.ts`
- `thirdparty/claude-code-sourcemap/src/cost-tracker.ts`

Claude Code 的核心设计是：

1. 支持 `defer_loading` 工具，并用 `tool_reference` 在消息历史中恢复已发现工具。
2. 对模型和 provider 做严格 gate；不支持 tool search 的路径不会发送 `defer_loading`。
3. prompt cache break detector 会排除 `defer_loading` 工具，避免把 API 实际不计入 prompt 的工具误判成 cache break。
4. 成本统计里直接展示 cache read/write。

wuu 已经吸收的部分：

- Anthropic 原生 `defer_loading`/`tool_reference` 路径
- Haiku、代理、第三方兼容路径的安全 fallback
- cache read/create token 在 provider、agent、appserver、session 中贯穿
- compact summary 不把完整工具 schema 塞进正文，而是放在 `DiscoveredTools` 元数据中

仍未完全追平的部分：

- wuu 还没有运行时 prompt cache break detector。
- wuu 还没有把 cache read/create 做成面向用户的对比面板或调试报告。

## wuu 当前实现状态

| 能力 | 当前状态 | 成熟度判断 |
|---|---|---|
| provider-neutral deferred 工具表达 | `ToolDefinition.DeferLoading`、`LoadableToolDefinition`、`ToolCallKindToolSearch` 已具备 | 接近成熟 |
| OpenAI Responses 原生渐进加载 | 支持 `tool_search_call`、`tool_search_output.tools`、stream/non-stream、compact restore | 接近 Codex 主路径 |
| Anthropic 原生渐进加载 | 支持 `defer_loading`、`tool_reference`、安全 gate、fallback | 接近 Claude Code 基础路径 |
| fallback | 不支持原生渐进加载时仍走本地 `tool_search` 激活 deferred 工具 | 成熟 |
| compact/resume 恢复 | `DiscoveredTools` 进入消息元数据、SQLite/JSONL/appserver roundtrip、fork/subagent clone | 基本成熟 |
| MCP 工具暴露 | 少量稳定 MCP 直出，大量或超大 schema MCP 延迟搜索；MCP 不破坏稳定前缀 | 比之前明显成熟，但低于 Codex namespace 能力 |
| cache 稳定性 | 稳定工具前缀、OpenAI 顶层 tools 不抖动、prompt cache key 保持稳定 | 基本成熟 |
| cache 观测 | `CacheCreationTokens`、`CacheReadTokens` 贯穿 usage、UI 通知、历史记录 | 基础成熟 |
| cache break 诊断 | 主要靠测试和 token 观测，暂无运行时 detector | 仍落后 Claude Code |

## 已完成的阶段

1. 基线确认：确认 wuu 已有 deferred 工具表达、tool_search fallback、prompt cache key、cache token 观测字段。
2. OpenAI：完成 Responses 原生 `tool_search_call`/`tool_search_output.tools` 路径。
3. Anthropic：完成 `defer_loading`/`tool_reference` 的安全启用路径。
4. 状态恢复：把已发现工具从内存状态升级为可 compact/resume/fork/subagent 恢复的元数据。
5. MCP 策略：小集合稳定 MCP 可以直出，大集合和动态 MCP 默认 deferred。
6. Cache 验证：增加测试，证明搜索前后顶层 tools 稳定，MCP 激活后仍追加在稳定前缀之后。

## 验证覆盖

已覆盖的关键测试点：

- OpenAI Responses 普通函数工具不会带 `defer_loading`。
- OpenAI Responses `tool_search` 会序列化为原生工具。
- `tool_search_call` 能从 stream/non-stream 响应中解析。
- `tool_search` 历史能渲染为 `tool_search_output.tools`。
- 已发现工具不会重新注入 OpenAI 顶层 `tools`。
- compact 后的 `DiscoveredTools` 能恢复为 provider 合法输入。
- Anthropic 原生路径会生成 `tool_reference`。
- Anthropic 不支持路径不会发送 `defer_loading`。
- MCP 小集合直出、大集合 deferred、超大 schema deferred。
- deferred MCP 被 `tool_search` 激活后追加到稳定前缀之后，且 `CacheStable=false`。
- cache read/create token 能从 provider 进入 agent、appserver 通知和 session history。

本轮验收命令：

```bash
go test ./internal/providers/openai ./internal/tools
go test ./internal/agent ./internal/appserver ./internal/session ./internal/compact
```

## 与 thirdparty 的差距

| 方向 | wuu 现状 | 与 Codex/Claude Code 的差距 |
|---|---|---|
| OpenAI Responses | 主链路已经对齐：原生 tool_search、历史回放、compact restore、顶层 tools 稳定 | 缺 namespace loadable tools 和更完整的 app-server dynamic tool 协议 |
| Anthropic | 安全 gate 和 fallback 已到位 | 缺 Claude Code 那种更细的 provider/header 组合策略和 cache break detector |
| MCP | 已有实用策略：小集合直出，大集合 deferred | Codex/Claude Code 对 MCP namespace、连接状态、pending server 反馈更完整 |
| Cache | 稳定前缀和 token 观测已完成 | 缺自动诊断：比如 cache read tokens 突降时给出具体 break 原因 |
| 文档/运维 | 当前文档能说明设计边界 | 还没有面向用户的 cache health 报告或 debug UI |

## 后续可选优化

1. 增加 prompt cache break detector：记录 system、稳定工具 schema、cache key、cache read tokens；当 cache read 明显下降时输出原因。
2. 支持 OpenAI Responses namespace loadable tools：对齐 Codex 的 `LoadableToolSpec::Namespace`。
3. 给 MCP 搜索结果增加 pending server 信息：当 MCP server 仍在连接时，提示稍后重试。
4. 在桌面 UI 或 session 报告中展示 cache read/create 走势，帮助比较变更前后命中率。
5. 建一组固定 benchmark conversation，用同一模型跑改动前后 `cache_read_tokens / cache_creation_tokens`，得到真实提升百分比。

