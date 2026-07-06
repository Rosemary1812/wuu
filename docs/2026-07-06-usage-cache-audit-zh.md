# 2026-07-06 工具面与 usage/缓存链路审计（决策记录）

对工具面瘦身与上下文占用/缓存两批改动做的一次审计，方法是源码走读 + thirdparty 参照（claude-code-sourcemap、pi、codex）+ 对 MiniMax anthropic 兼容端点的 live 实测。本文只保留结论与决策；现行架构见 `docs/compaction-chains-zh.md`、`docs/context-usage-and-caching-zh.md`、`docs/tool-surface-assembly-zh.md`、`docs/tool-loading-modes-zh.md`。

## 结论

| 审计项 | 判定 | 处置 |
|---|---|---|
| 工具面瘦身（-15 工具，action 合并） | 良性：schema 全静态、无残留指引、门控自洽 | 保留 |
| Environment 日期会话冻结 | 良性（与 cc 的会话 memoize 同型），但对命中率的归因不成立（日期天粒度 vs 缓存分钟级 TTL） | 保留 |
| helpme 完成后显式失效 usage 基线 | 良性，设计正确 | 保留 |
| MiniMax inclusive usage 归一化 + base_url 自动识别 | **前提证伪**：端点实测为 exclusive 语义（判别法见下）；且流式归一化挂错站点 | 同日修复：删自动识别，开关只认显式配置，补 `message_delta` 站点 |

## 实测确立的事实（复测方法随附）

1. **MiniMax anthropic 兼容端点的 `input_tokens` 是 exclusive 语义**（不含 cache_read，与 Anthropic 原生一致）。判别法：两次相同的带 `cache_control` 请求，第二次若 `input < cache_read` 即为 exclusive；两次 `input + cache_read` 之和相等亦指向 exclusive。任何"anthropic 兼容"端点接入时都应先跑这个判别，再决定是否配置 `input_tokens_include_cache_read`。
2. **曾观测到的"占用约 4 倍高估"来自 turn 级跨 step 加总**（缓存前缀在一个 agentic turn 的多次 API 调用中被重复计数），是消耗口径与占用口径的混用，与 input 语义无关；占用口径（UsageTracker 取最新响应）一直正确。区分两种口径的约定见 usage 文档。
3. **MiniMax 端点的缓存命中是概率性的**：前缀/断点匹配语义存在，但缓存呈节点本地 + 弱粘性路由形态——字节相同的请求也偶发 miss，同一前缀请求越多暖节点越多命中率越高；`x-session-affinity` 头不被识别。因此线程内命中很高（append-only 前缀 + 高频重复），新线程首个请求大概率冷 miss，会话级聚合命中率随新线程数量下降。wuu 侧前缀已字节稳定（跨线程哈希一致验证过），无可修，属服务端行为。
4. **上下文窗口是随渠道、随时间变化的数据**（MiniMax-M3 上线 512k、后升 1M，live 仲裁确认现网收 >512k），不能硬编码；由此引出同日的预算单源化重构（见压缩文档"设计约束"一节）。

## 教训

- 给厂商行为下结论前必须多次采样：缓存命中一类的概率性行为，单次实验（n=1）曾导致"端点只做精确匹配"的错误结论，扩大样本后推翻。
- 厂商行为的适配一律走显式配置（ProviderConfig / per-model options），不做 base_url 特判；默认值可以用启发（首方 URL、模型版本），但配置必须能覆盖。
