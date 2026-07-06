# 压缩链路：触发、执行、usage 一致性

本文描述 wuu 的上下文压缩（compaction）体系：触发路径、阈值推导、摘要请求自身的爆窗保护、inception 与 helpme 两条工具侧压缩、以及每条路径压缩后 usage 记账如何保持一致。占用计算本身见 `docs/context-usage-and-caching-zh.md`。文末附与 Claude Code / pi / codex 的设计对照。

## 触发路径：三条自动 + 一条手动

压缩不是"pre-turn 与 turn 内"两条链路，而是同一个主动检查跑在每个 step 开头，外加两条特殊路径（`internal/agent/loop.go`）：

1. **主动压缩（proactive）**。每个 step 迭代开头、发 provider 请求之前检查（step 0 即"turn 开始前"，后续 step 即"工具结果后的续写请求前"——同一代码路径）。条件：`usage.EstimateCurrent() >= threshold` 且 `canProactivelyCompact`（存在可折叠边界）。失败或无变化则本次 Run 内抑制（`proactiveSuppressed`），runner 级另有连续失败熔断（`proactiveCompactCircuitOpen`）。
2. **反应式溢出兜底（overflow）**。step 执行返回上下文超限错误时压缩一次并重发；**每个 Run 最多一次**，第二次溢出直接上抛。
3. **工具后重写（post-tool rewrite）**。工具结果追加进 history 之后，检查 helpme / inception 工具产出的 `history_rewrite` 信封并执行整段重写（见下文两节）。
4. **手动 `/compact`**（`ForceInitialCompact`）：循环前强制跑一遍，绕过阈值。

三条自动路径压缩后都同步执行 `usage.Reset() + RecordPendingMessages(messages)` 重建估算基线，下一次真实响应把基线校准回 ground truth。

## 阈值推导：modelbudget 是单一所有者

`modelbudget.Resolve` 是有效上下文窗口与压缩阈值的唯一所有者：

```
ceiling = 有效上下文窗口（provider/model 配置 > 目录数据 > 内置注册表 fallback），
          有独立输入上限时作为 clamp（min），不是独立锚
usable  = ceiling − min(输出预留, 20k) − 13k 缓冲（小窗口按 ceiling/8 缩放）
CompactThresholdTokens = usable
```

该值经 StreamRunner/LoopConfig 直接下发给 loop（`CompactThresholdTokens`），主 agent、worker、named pinned 预算三条路径同源；trace/UI 展示 `EffectiveContextWindow()` 同一数值。公式对齐 Claude Code（窗口 − 20k 输出预留 − 13k 缓冲）。例：窗口 1M、输出上限 128k 的模型，阈值 = 967k。

`agent.compact_threshold_pct ∈ (0,1)` 可覆盖为百分比（乘以 min(窗口, 输入上限)），优先级高于 budget 下发的绝对阈值。

设计约束（历史教训的固化）：

- **窗口是随渠道、随时间变化的数据**（同一模型不同 provider 窗口可以不同，同一 provider 也会升级），必须走"配置 > 目录数据"的数据通道，禁止在代码里按厂商特判——曾因硬编码覆盖表只修显示、预算路径读过时目录值，导致阈值只有真实窗口的一半。
- **显示与预算必须同源**：任何地方需要窗口值，从 budget 取，不允许第二个解析点。
- **用户显式配置必须赢**：`models.<m>.limit.context` 与 `context_window` 是同一事实的两种拼法，目录 enrich 对二者做联合守卫（用户写任一种，目录不填另一种）。

## 摘要请求自身的爆窗保护

压缩要把待折叠历史发给模型做摘要，这个请求自己不能爆窗。wuu 的保护（`internal/compact/compact.go`）：

- **分块 map-reduce**：待摘要历史按 `compactSummaryChunkSize` 切块（单块 ≤ 80k tokens 且 ≤ usable 的一半），滚动更新摘要，而不是一次性发全量。
- **溢出重试**：单块摘要请求若仍超限，丢掉块内最老一条重试，最多 3 次。
- 摘要用主模型，`MaxTokens=4096`，输出截断到 80KB 字符；流式中断时用已收 partial（≥200 字符）或回退非流式；整体 20 分钟超时。

与三家等价甚至更细（cc：strip 图片 + PTL 头截断重试 3 次；pi：只保留最近 20k 原文其余进摘要；codex：从最旧逐条删 + token-budget 模式干脆不做模型摘要直接开新窗口）。

注意压缩与 prompt 缓存的经济性：压缩重写历史前缀，必然打穿该线程的缓存并额外付一笔摘要请求。阈值设得过低会把会话变成"零缓存 + 高频摘要"的成本形态，得不偿失。

## inception：模型自压缩工具

`inception` 工具（`internal/tools/tool_inception.go`）本身不发 LLM 请求：模型在正常一轮里把结构化 summary 写进工具参数，工具打包成 `history_rewrite` 信封返回，loop 的 post-tool rewrite 在**锚点**（每步注入的 `CHECKPOINT` 消息）处丢弃 suffix、替换为一条 hidden 续写消息（保留未被回复的可见用户消息）。

- 在 loop 内执行，随后显式 `usage.Reset() + RecordPendingMessages`——与基础压缩同一失效模式，**不依赖长度启发式**，usage 记账自洽。
- 连续失败 3 次后本会话禁用（熔断）。
- 参照：codex 的 `new_context` 工具是同类物（模型请求开新窗口，由压缩路径消费）；cc/pi 无直接等价。

## helpme：双会话联合压缩

流程：`helpme` 工具调用时**同步**用主模型抽取父线程决策日志（`BuildHelpMeParentJournal`，分块 map-reduce + 溢出丢老重试，与基础压缩同一套保护），异步 spawn 恢复子 agent；子 agent 完成后、合成唤醒 turn 之前，`applyHelpMeCompletionRewrite` 做 joint compact——把父历史整段替换为"系统前缀 + joint-compact 系统消息（父 journal + 子 agent report/result，逐字段截断 section ≤ 6000 字节）+ 新锚点 + 未回复的可见用户消息"。

- **joint compact 是纯字符串拼装，不发 LLM 请求**，不存在爆窗风险；上游两处保护（journal 抽取分块、字段级截断）保证输入有界。
- 发生在 loop 外，因此需要显式 `runner.ResetConversationUsage(rewritten)` + 落一条带新 ContextTokens 的 token_usage 行，不能依赖 loop 内的重置路径。
- **与基础压缩的关系**：等待子 agent 期间父线程若又跑了 turn 并被基础压缩折叠过，joint compact 会在已压缩历史上再做整段替换；父侧此期间的 assistant 产出只以"发起时快照的 journal"体现（旧快照），suffix 中 assistant 工作被丢弃、只保可见用户消息。这是设计内的信息收敛，不是竞态 bug，但等待期越长丢失越多——若观察到恢复质量问题，改进方向是唤醒时增量补一段"等待期间的父进展"。
- 并发窗口：history 在锁内替换、`ResetConversationUsage` 在锁外，二者之间读 `EstimateCurrent` 会短暂看到"新历史 + 旧基线"（偏高，一次读数级别）。

## usage 一致性与估算器

稳态下占用以最新响应的 provider usage 为 ground truth，估算只覆盖"上次响应之后新增消息"的 delta，误差有界且每步归零。**估算独自扛全场的窗口只有三个**：resume 后首个响应前、压缩后到下一响应前、helpme 重写后到下一响应前。其中 resume 场景不再全量重估：线程 runtime 重建时用最近一条持久化 `ContextTokens`（真实 provider 口径）播种基线（`StreamRunner.SeedConversationUsageBaseline`）。

估算器（`internal/contextbudget`）系数按真实分词标定（2026-07，MiniMax-M3；改系数前先重测）：

| 内容 | 公式 | 依据 |
|---|---|---|
| 非 CJK 文本 | 字符/4 | 实测轻微高估（+6~11%），符合"宁可略悲观"约定 |
| CJK 文本 | 字符 × 0.7 | 实测约 0.61 token/字，×0.7 留轻微悲观；曾用 /2 低估约 20%——低估会延迟压缩，而 overflow 兜底每 Run 只有一次，是更危险的方向 |
| 工具 JSON 参数 | runes/3 | 实测约 3.0 字符/token；曾用 /2 高估 1.5-2 倍，工具密集会话会被提前压缩 |
| 图片 | 视觉 patch 数 | 不按 base64 字节 |
| 非图片附件 | base64/4 | 已知偏高估，未标定 |

结构性偏置：每消息 +4、每 tool call +8、带工具整批 +500（刻意悲观）。thinking/reasoning 不构成偏差：wuu 在 anthropic 兼容路径重发历史 thinking 块、provider 计数保留，本地记账与真实 input 自洽；assistant 输出只经 output_tokens 计一次，不双计。

## 已知遥测盲区

- named/participant turn 不落 `context_requests` 明细，无法复盘该链路每步的 usage/缓存数字。
- `wuu exec` 的 JSONL 输出不转发 compact 事件（`emitTurnStreamEvent` 无该分支，`internal/exec/runner.go`），压缩发生与否只能从 trace 的 message_count 骤降倒推。

## 与参照系的对照速览

| 维度 | wuu | Claude Code | pi | codex |
|---|---|---|---|---|
| 主触发 | 每 step 开头（proactive） | 发请求前（snip→micro→collapse→autocompact 链） | 响应后/下次提交前 | pre-turn + mid-turn + 流内 reactive |
| 阈值 | 窗口 − min(reserve,20k) − 13k | 窗口 − 33k | 窗口 − 16384 | 窗口 × 90% |
| 摘要爆窗保护 | 分块 map-reduce + 丢老重试×3 | strip 图片 + PTL 头截断×3 | 只保最近 20k 原文 | 删最旧重试 / 不做摘要开新窗 |
| 压后计数 | Reset + 估算，下一响应校准；resume 用持久化值播种 | 估算重建 + 下一响应校准 | 同左 + stale usage 守卫 | 立即 recompute + 服务端 usage |
| 反复压缩防护 | Run 内抑制 + runner 熔断 | 连续 3 败熔断 + 递归源判别 | overflow 单次恢复 | 分级错误 + 退避 |
| 工具触发压缩 | inception + helpme joint | Session Memory 联合 | 扩展 hook / handoff | `new_context` 工具 |

wuu 缺少而三家有的：pi 的 **stale-usage 守卫**（压缩刚完成时忽略压缩前旧 usage，防复触发）；cc 的 **microcompact**（工具结果层的细粒度清理，先于全量摘要，且不打穿缓存前缀）；codex 的 **压缩后立即 recompute**。三者都是候选改进。
