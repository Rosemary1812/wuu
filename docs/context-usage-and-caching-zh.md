# 上下文占用与 prompt 缓存链路

本文描述 wuu 如何计算"当前上下文占用了多少 token"、这个数字驱动哪些行为（压缩触发、前端显示、用量页），以及 prompt 缓存前缀的稳定性约束。跨 provider 的 usage 字段语义差异是这条链路最容易出错的地方，文末附对照表。

## usage 字段语义：exclusive 与 inclusive

一次 API 响应的 usage 里，`input_tokens` 与缓存字段的关系存在两种流派：

- **exclusive（Anthropic 原生）**：`input_tokens` 只计未命中缓存的新鲜输入。完整 prompt = `input_tokens + cache_creation_input_tokens + cache_read_input_tokens`，三者互斥。
- **inclusive（OpenAI 系）**：`prompt_tokens` / `input_tokens` 包含缓存命中部分，`cached_tokens` 是它的子集。非缓存输入 = `prompt_tokens - cached_tokens`。

wuu 的内部统一约定是 **exclusive**：各 provider client 负责把自家语义归一到"`TokenUsage.InputTokens` = 新鲜输入，`CacheReadTokens` = 缓存命中"，上层不再关心来源。openai client 在 `asTokenUsage` 里做 `PromptTokens - cached` 减法；anthropic client 原样透传（原生语义即 exclusive）。

anthropic client 另有一个 `InputTokensIncludeCacheRead` 配置开关（`ProviderConfig`，默认 false），用于标记"声称 anthropic 兼容但 usage 报 inclusive"的端点，开启后 `normalizeInclusiveInput` 做减法归一。**给任何端点开这个开关前，必须先用两次相同请求实测语义**（方法见 `docs/2026-07-06-usage-cache-audit-zh.md`，那次审计实测 MiniMax 实为 exclusive）。对 exclusive 端点误开此开关会把新鲜输入减到 0，导致占用低估、压缩延迟触发。

## 占用公式与三个消费口径

核心公式在 `TokenUsage.TotalContextTokens()`（`internal/providers/types.go`）：

```
TotalContextTokens = InputTokens + CacheReadTokens + OutputTokens
```

缓存命中的 token 仍然占上下文窗口，所以必须计入。注意此式**不含 `CacheCreationTokens`**，理由注释写的是"cache_creation 是 InputTokens 的子集"——这对某些兼容端点成立，但对 Anthropic 原生**不成立**（原生三字段互斥，Claude Code 的同类公式四项全加）。后果是原生 Anthropic 上每轮占用会低估约一轮增量（新写入缓存的部分当轮不被计入，下一轮变成 cache_read 后自动补上）。这是已知偏差，量级为单轮 delta，尚未修。

这个数字有三个消费口径，**语义不同，不要混用**：

1. **UsageTracker（压缩触发 + 环形表）**：`internal/agent/usage.go`。以最近一次成功响应的 `TotalContextTokens()` 为 ground truth，加上其后新增消息的本地估算（`pendingDelta`）。`EstimateCurrent()` 是"当前对话占多少窗口"的权威答案，`loop.go` 的主动压缩触发（`EstimateCurrent() >= threshold`）和 `res.ContextTokens`（落库后成为前端 `RetainedTokens`，即 composer 环形表读数）都用它。**这是取最新、不是累加。**
2. **token_usage 行（用量页）**：`turn_handlers.go` 的 `appendTokenUsage` 每 turn 落一行，其中 `InputTokens` 等字段是**该 turn 内所有 API 调用（每个工具往返一次）的加总**。加总语义适合计量消耗（billing 口径），但把它当"上下文占用"读会得到数倍虚高——一个 7 步的 agentic turn，缓存前缀被重复计数 7 次，加总可达真实上下文的约 4 倍。用量页（`aggregateUsageRows`）按消耗口径展示是正确的；任何"占用"类显示都必须走 `ContextTokens` / `RetainedTokens`。
3. **insight 聚合（命中率等）**：`internal/insight/types.go`。`CacheHitRate = CacheRead / (Input + CacheRead)`，基于 turn 加总行，是消耗加权命中率。

## 压缩（compaction）触发

主触发在 `internal/agent/loop.go`：`usage.EstimateCurrent() >= proactiveCompactThreshold(cfg)`，阈值默认是可用输入窗口（`MaxInputTokens - reserve`）或 `CompactThresholdPct`。`contextbudget.ShouldCompact`（字节估算，0.8 * max）只做可行性辅助判断，不是主触发。

loop 内压缩后由 loop 自己 `usage.Reset() + RecordPendingMessages` 重置基线。**loop 外的历史重写**（目前只有 HelpMe joint compact，走 completion wakeup 路径）必须显式调用 `StreamRunner.ResetConversationUsage(rewritten)` 并落一条带新 `ContextTokens` 的 token_usage 行，否则 runner 的跨轮基线保留压缩前的膨胀值、前端 RetainedTokens 不降。`prepareUsageTracker` 里的长度启发式（tracked 长度大于当前 history 才重置）只是安全网，不要依赖它——压缩产物可能字节更小但消息数不减。

参照系：Claude Code 的规范口径是"最后一次 usage 四项相加 + 新增消息粗估"，阈值为有效窗口减 13k buffer；pi 是 `totalTokens`（或四项相加）对比 `contextWindow - 16384`；codex 直接用最近一次响应的 `total_tokens`（inclusive 语义下天然等于完整上下文）对比窗口 90%。四家的共同点：**占用永远取最新一次响应，绝不跨调用累加**。

## prompt 缓存前缀

### 断点布局（anthropic wire）

- 整段 system（包括 memory / memdir / skills / workflows 等动态段，它们都在前缀内）作为单条 system message，**最后一个 block 打 `cache_control`**（`buildAnthropicSystem`）。工具 schema 不打 per-tool 断点，靠 Anthropic 的缓存顺序（tools → system → messages）被 system 末尾断点隐式覆盖。
- 消息侧有 sliding tail 断点 + 可选 compact anchor（`applyAnthropicCacheHint`），合计控制在 Anthropic 4 断点预算内。
- 每轮变化的注入内容（`<subagent_status>` reminder 等 request-only 块）放在 sliding tail 断点**之后**的消息尾部，不进前缀也不进 durable history。

### 前缀字节稳定性

缓存命中的前提是前缀跨请求字节一致。wuu 的稳定性来源：

- 工具 schema 全静态（见 `docs/tool-surface-assembly-zh.md` 的缓存红线一节）；
- system prompt 在 thread runner 上冻结，只在 thread rebuild / clear 时重建，不逐轮重算；
- **Environment 段日期会话冻结**：`Session.SessionDate` 在 `NewSession` 时定格（`YYYY-MM-DD`），此后所有 system prompt 构建（base / worker / thread rebuild / `RefreshSystemPrompt`）复用冻结值。长会话跨午夜不再churn 前缀；实时时钟需求走每轮消息流，永远不进缓存前缀。

参照系：Claude Code 完全一致——日期不在 system prompt 里，而在 memoize 的会话级 user 上下文里（"captures the date once at session start"），跨午夜通过在对话**尾部**追加 date_change attachment 通知模型，绝不改写前缀；工具 schema 同样 session 冻结。codex 更进一步，environment context（cwd / date / network）是独立 user 分段且只做 append-only diff。pi 的日期在 system prompt 末尾，靠实例缓存达到事实上的会话冻结。业界共识：**前缀只增不改，动态信息尾部追加**。

已知残留漂移源：memory / skills 等动态段在前缀内，会话中途这些文件变化会打穿前缀缓存（当前接受此代价，因为变更频率低且内容确实需要模型看到）。

## provider usage 语义对照表

| 系统 | input 语义 | 占用公式 | 归一化做法 |
|---|---|---|---|
| Anthropic 原生 | exclusive（三字段互斥） | — | 无需 |
| OpenAI（Completions/Responses） | inclusive（cached 是子集） | — | wuu/pi/cc-bridge 各自减法或直接用 total |
| MiniMax anthropic 兼容端点 | **exclusive（2026-07-06 实测）**；usage 只在 `message_delta` 报全量，`message_start` 的 input 恒为 0；`cache_creation` 恒报 0 | — | 不应减法 |
| wuu 内部 | exclusive 统一 | `Input + CacheRead + Output`（不含 CacheCreation，见上文偏差说明） | openai client 减法；anthropic client 可选开关 |
| Claude Code | exclusive 假定，无任何归一 | `input + cache_creation + cache_read + output` | 无（OpenAI 桥未归一，桥内有重复计数缺陷） |
| pi | Anthropic 原样、OpenAI 减法 | `totalTokens` 或四项相加 | per-API 硬编码，无 per-provider 开关 |
| codex | inclusive 原样（纯 OpenAI 系） | `total_tokens` 直接用 | 不需要（单一语义） |

维护提示：接入新的"anthropic 兼容"端点时，不要相信文档或直觉，用两次相同的带 `cache_control` 请求实测：第二次若 `input_tokens < cache_read_input_tokens`，则必为 exclusive；两次 `input + cache_read` 之和相等也指向 exclusive。同时确认流式 usage 到底在 `message_start` 还是 `message_delta` 报数——兼容端点常与原生不同。
