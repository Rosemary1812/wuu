# 压缩链路：触发、执行、usage 一致性

本文描述 wuu 的上下文压缩（compaction）体系：几条触发路径、阈值推导、摘要请求自身的爆窗保护、inception 与 helpme 两条工具侧压缩、以及每条路径压缩后 usage 记账如何保持一致。占用计算本身见 `docs/context-usage-and-caching-zh.md`。文末附与 Claude Code / pi / codex 的设计对照和已知偏差清单。

## 触发路径：三条自动 + 一条手动

压缩不是"pre-turn 与 turn 内"两条链路，而是同一个主动检查跑在每个 step 开头，外加两条特殊路径（`internal/agent/loop.go`）：

1. **主动压缩（proactive）**。每个 step 迭代开头、发 provider 请求之前检查（step 0 即"turn 开始前"，后续 step 即"工具结果后的续写请求前"——同一代码路径）。条件：`usage.EstimateCurrent() >= threshold` 且 `canProactivelyCompact`（存在可折叠边界）。失败或无变化则本次 Run 内抑制（`proactiveSuppressed`），runner 级另有连续失败熔断（`proactiveCompactCircuitOpen`）。
2. **反应式溢出兜底（overflow）**。step 执行返回上下文超限错误时压缩一次并重发；**每个 Run 最多一次**，第二次溢出直接上抛。
3. **工具后重写（post-tool rewrite）**。工具结果追加进 history 之后，检查 helpme / inception 工具产出的 `history_rewrite` 信封并执行整段重写（见下文两节）。
4. **手动 `/compact`**（`ForceInitialCompact`）：循环前强制跑一遍，绕过阈值。

三条自动路径压缩后都同步执行 `usage.Reset() + RecordPendingMessages(messages)` 重建估算基线，下一次真实响应把基线校准回 ground truth。

## 阈值推导：锚定输入上限，不是上下文窗口

默认（`compact_threshold_pct` 未设）阈值 = `MaxInputTokens − min(输出预留, 20000)`。而 `MaxInputTokens` 来自 `modelbudget.Resolve`：

```
input  = provider.Models[model].Limit.Input   （用户配置或 modelcatalog/catwalk 灌入）
usable = input > 0 ? input − min(output, 20000) : context − output
CompactThresholdTokens = usable   （所以 trace 里两个字段恒相等）
```

**这意味着压缩阈值锚定在"输入上限"而非"上下文窗口"**。实测（MiniMax-M3）：catalog 声明 context 1M、input 512k，输出预留 8k 时阈值 = 504k——只有窗口的一半。如果 catalog 的 input limit 数据保守或错误，压缩就系统性提前，且用户从"1M 窗口"的直觉出发会觉得莫名其妙。参照系全部锚定窗口：Claude Code 阈值 = 窗口 − 20k（摘要输出预留）− 13k（缓冲）；pi = 窗口 − 16384；codex = 窗口 × 90%。wuu 的 input-limit 锚定语义上也说得通（部分渠道的 prompt 上限确实低于窗口），但 catalog 数值的正确性直接决定触发点，值得在 UI/trace 里可见化。

`agent.compact_threshold_pct ∈ (0,1)` 可覆盖为百分比（乘以 min(窗口, 输入上限)）。

## 摘要请求自身的爆窗保护

压缩要把待折叠历史发给模型做摘要，这个请求自己不能爆窗。wuu 的保护（`internal/compact/compact.go`）：

- **分块 map-reduce**：待摘要历史按 `compactSummaryChunkSize` 切块（单块 ≤ 80k tokens 且 ≤ usable 的一半），滚动更新摘要，而不是一次性发全量。
- **溢出重试**：单块摘要请求若仍超限，丢掉块内最老一条重试，最多 3 次。
- 摘要用主模型，`MaxTokens=4096`，输出截断到 80KB 字符；流式中断时用已收 partial（≥200 字符）或回退非流式；整体 20 分钟超时。

这与三家等价甚至更细（cc：strip 图片 + PTL 头截断重试 3 次；pi：只保留最近 20k 原文其余进摘要；codex：从最旧逐条删 + token-budget 模式干脆不做模型摘要直接开新窗口）。**此处评估为达标，无爆窗风险**。

## inception：模型自压缩工具

`inception` 工具（`internal/tools/tool_inception.go`）本身不发 LLM 请求：模型在正常一轮里把结构化 summary 写进工具参数，工具打包成 `history_rewrite` 信封返回，loop 的 post-tool rewrite 在**锚点**（每步注入的 `CHECKPOINT` 消息）处丢弃 suffix、替换为一条 hidden 续写消息（保留未被回复的可见用户消息）。

- 在 loop 内执行，随后显式 `usage.Reset() + RecordPendingMessages`——与基础压缩同一失效模式，**不依赖长度启发式**，usage 记账自洽。
- 连续失败 3 次后本会话禁用（熔断）。
- 参照：codex 的 `new_context` 工具是同类物（模型请求开新窗口，由压缩路径消费）；cc/pi 无直接等价。

## helpme：双会话联合压缩

流程：`helpme` 工具调用时**同步**用主模型抽取父线程决策日志（`BuildHelpMeParentJournal`，分块 map-reduce + 溢出丢老重试，与基础压缩同一套保护，**不会爆窗**），异步 spawn 恢复子 agent；子 agent 完成后、合成唤醒 turn 之前，`applyHelpMeCompletionRewrite` 做 joint compact——把父历史整段替换为"系统前缀 + joint-compact 系统消息（父 journal + 子 agent report/result，逐字段截断 section ≤ 6000 字节）+ 新锚点 + 未回复的可见用户消息"。

- **joint compact 是纯字符串拼装，不发 LLM 请求**，不存在爆窗风险；担心的"最后压两条 session 上下文会爆窗"实际由两处上游保护消化（journal 抽取的分块、字段级截断）。
- 发生在 loop 外，因此需要显式 `runner.ResetConversationUsage(rewritten)` + 落一条带新 ContextTokens 的 token_usage 行（2026-07-06 `1d9e6ad3` A2 补上，此前靠脆弱的长度启发式）。
- **与基础压缩的关系**：等待子 agent 期间父线程若又跑了 turn 并被基础压缩折叠过，joint compact 会在已压缩历史上再做整段替换；父侧此期间的 assistant 产出只以"发起时快照的 journal"体现（旧快照），suffix 中 assistant 工作被丢弃、只保可见用户消息。这是设计内的信息收敛，不是竞态 bug，但等待期越长丢失越多——若将来观察到恢复质量问题，改进方向是唤醒时增量补一段"等待期间的父进展"。
- 并发窗口：history 在锁内替换、`ResetConversationUsage` 在锁外，二者之间读 `EstimateCurrent` 会短暂看到"新历史 + 旧基线"（偏高，一次读数级别）。

## usage 一致性：哪里可能高估/低估

稳态下占用以最新响应的 provider usage 为 ground truth，估算只覆盖"上次响应之后新增消息"的 delta，误差有界且每步归零。**估算独自扛全场的窗口只有三个**：resume 后首个响应前、压缩后到下一响应前、helpme 重写后到下一响应前。

估算器（`internal/contextbudget`）实测偏差（MiniMax-M3 分词，2026-07-06 探针）：

| 内容 | 公式 | 估/真 |
|---|---|---|
| 英文日志 | 非 CJK 字符/4 | 1.06 |
| Go 代码 | 同上 | 1.11 |
| 中文散文 | CJK 字符/2 | **0.80（低估）** |
| 工具 JSON 参数 | runes/2 | **1.51（高估）** |

结构性偏置：每消息 +4、每 tool call +8、带工具整批 +500（刻意悲观）；图片按视觉 patch 估（准），非图片附件按 base64/4（高估）。

方向汇总：
- **高估（压缩提前）**：阈值锚定 input limit（最大、结构性）；工具 JSON 参数 1.5-2 倍；resume 全量悲观播种（触发闸门不检查是否有 ground truth）；大附件。
- **低估（压缩推迟，反向风险）**：CJK 内容约 -20%——对国产模型中文负载，这个方向反而更值得警惕（延迟触发 → 靠 overflow 兜底，而 overflow 每 Run 只有一次）。
- **不构成偏差**：thinking/reasoning——wuu 在 anthropic 兼容路径重发历史 thinking 块，provider 计数也保留（MiniMax 实测 E2E 验证），本地记账与真实 input 自洽；assistant 输出不被双计（只经 output_tokens 计一次）。

## 端到端观测（2026-07-06，MiniMax-M3 live）

- 多步 turn 内缓存命中率 91-95%（step1 起），冷启动 step0 除外；一次观测到中间 step 缓存服务端丢失（cr 从 12k 跌到 114 再逐步恢复），wuu 侧前缀哈希全程字节稳定——命中率波动主要在服务端。
- **强制低阈值（pct=0.02，约 10k）下，turn 内主动压缩反复正确触发**：trace 里 message_count 多次骤降（10→5、11→8→5），input 稳定在 14-18k 不再单调增长，任务最终正确完成——proactive 链路在真实负载下工作正常。副作用同时可见：每次压缩重写前缀后缓存归零（cr 恒 ~128），频繁压缩的会话实际是"零缓存 + 每次额外一笔摘要请求"的成本形态，阈值设得过低会得不偿失。
- resume 场景：51k 真实历史（英文日志为主）重播种后的估算未越过 61k 阈值（未误触发）——文本类内容估算足够准；理论高估场景需要大 JSON 工具参数占主导。
- 单轮 turn 从零涨到 70k+ 未触发主动压缩（阈值 25.6k）的一次观测最终归因为该轮模型改用 `wc`/局部读，占用未过阈值——闸门行为正常。
- 遥测盲区两处：named/participant turn 不落 `context_requests` 明细（无法复盘 named 链路每步数字）；`wuu exec` 的 JSONL 输出不转发 compact 事件（`emitTurnStreamEvent` 的 switch 无 compact 分支，`internal/exec/runner.go`），压缩发生与否只能从 trace 的 message_count 骤降倒推。

## 与参照系的对照速览

| 维度 | wuu | Claude Code | pi | codex |
|---|---|---|---|---|
| 主触发 | 每 step 开头（proactive） | 发请求前（snip→micro→collapse→autocompact 链） | 响应后/下次提交前 | pre-turn + mid-turn + 流内 reactive |
| 阈值锚 | **input limit** − min(reserve,20k) | 窗口 − 33k | 窗口 − 16384 | 窗口 × 90% |
| 摘要爆窗保护 | 分块 map-reduce + 丢老重试×3 | strip 图片 + PTL 头截断×3 | 只保最近 20k 原文 | 删最旧重试 / 不做摘要开新窗 |
| 压后计数 | Reset + 悲观估算，下一响应校准 | 估算重建 + 下一响应校准 | 同左 + stale usage 守卫 | 立即 recompute + 服务端 usage |
| 反复压缩防护 | Run 内抑制 + runner 熔断 | 连续 3 败熔断 + 递归源判别 | overflow 单次恢复 | 分级错误 + 退避 |
| 工具触发压缩 | inception + helpme joint | Session Memory 联合 | 扩展 hook / handoff | `new_context` 工具 |

wuu 缺少而三家有的：pi 的 **stale-usage 守卫**（压缩刚完成时忽略压缩前旧 usage，防复触发——wuu 靠 Reset 语义天然规避大半，但 resume 播种没有等价物）；cc 的 **microcompact**（工具结果层的细粒度清理，先于全量摘要）；codex 的 **压缩后立即 recompute**。
