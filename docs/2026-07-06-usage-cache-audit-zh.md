# 2026-07-06 工具面瘦身与上下文占用/缓存改动审计

审计对象是最近两个提交：

- `561083c4` tools: 工具面机械瘦身 -15 工具
- `1d9e6ad3` providers+agent+runtime: 上下文占用通用修 + 缓存日期冻结

方法：源码走读 + thirdparty 参照（claude-code-sourcemap、pi、codex）+ MiniMax anthropic 兼容端点 live 探测 + 本机 07-03/07-05 历史 session-trace 复盘。链路背景见 `docs/tool-surface-assembly-zh.md` 与 `docs/context-usage-and-caching-zh.md`。

## 结论速览

| 改动 | 判定 |
|---|---|
| `561083c4` 工具面瘦身 | 良性，可保留 |
| `1d9e6ad3` B：SessionDate 会话冻结 | 良性（与 Claude Code 做法一致），但对 44% 命中率的归因不成立 |
| `1d9e6ad3` A2：helpme loop 外压缩显式失效 usage | 良性，设计正确 |
| `1d9e6ad3` A1：MiniMax inclusive 归一化 + minimaxi 自动识别 | **前提经实测不成立，且实现有两个缺口，建议撤销自动识别** |

## 561083c4：工具面瘦身——良性

- 合并出的 5 个工具（`goal(action)`、`cron(action)`、`manage_participant(action)`、`post_message(kind)`、`send_message` 并入 followup）schema 全部静态扁平枚举，无时间戳/cwd/动态 roster，不破坏缓存前缀。动态的 subagent roster 刻意放静态系统段而非 schema，处理正确。
- 残留引用审计：模型可见面（embed 进 system prompt 的 `prompts/system.md` / `system_main.md`、bundled skills、presets、evalharness）无任何已删工具名。不存在"看得到指引调不到工具"。
- workflow 指引的 capability 门控（`HasAvailableCapability(CapabilityWorkflow)`）自洽：普通 project agent 既无指引也无工具。
- 前端 `ToolActivityHelpers.ts` 保留旧工具名 label 映射，仅用于渲染历史会话，无害且必要。
- 遗留小尾巴（非模型可见，建议顺手清理）：`prompts/design.md` 仍引用 `create_goal`；`docs/jsonl-events.md` 仍示例 `run_shell` / `run_test`。

## 1d9e6ad3 B：日期冻结——良性，但归因不成立

冻结本身方向正确，与参照系一致：Claude Code 的日期是 memoize 的会话级值（"captures the date once at session start"），跨午夜靠对话尾部追加 date_change 通知而非改前缀；codex 的 environment context 走 append-only diff。wuu 把 `Session.SessionDate` 冻结并贯穿所有 system prompt 构建路径，实现干净，回退路径（空值取当前日期）合理。

但提交信息里"44% 命中率真凶是 Environment 段日期每轮漂移"这个归因与事实不符：

- `wuucontext.Snapshot` 的 Date 是**天粒度**（`2006-01-02`），同一天内无论多少轮、多少次 rebuild，字节都相同。漂移只发生在跨午夜时刻。
- Anthropic 系 prompt 缓存 TTL 只有约 5 分钟，跨午夜漂移打掉的缓存本来也早已过期。

即冻结是好的卫生习惯，但对命中率的影响接近零。**真正的 44% 命中率成因当日已定位为服务端缓存的路由局部性**（统计实验，每组 8-16 发裸请求）：MiniMax 兼容端点**实现了前缀/断点匹配语义**（换 user 消息的请求 3/8 命中过 system 前缀，cr≈system 大小），但缓存呈**分布式节点本地 + 弱粘性路由**形态——字节完全相同的请求也偶发 miss（4/5），同一前缀被请求次数越多、暖节点越多、命中率越高；`x-session-affinity` 头不被识别（固定头 0/8 无改善）。这解释了全部观测：线程内命中 91-97%（高频重复 + 连接保活粘住节点），新线程首发大概率冷 miss（暖节点少），聚合被新线程数量拉低到四成——与日期漂移无关，与 wuu 前缀组装无关（跨线程前缀哈希字节一致已验证）。wuu 侧无可修，等待端点改进路由粘性或提供亲和机制。注意：单次实验极易误判——n=1 时曾误结论为"只做精确匹配无断点语义"，被 n=16 矩阵推翻。

## 1d9e6ad3 A2：loop 外压缩显式失效——良性

HelpMe joint compact 发生在 agent loop 之外，不经过 loop 自身的 `usage.Reset()` 路径。修复用 `StreamRunner.ResetConversationUsage(rewritten)` 显式重建基线，并当场落一条带压缩后 `ContextTokens` 的 token_usage 行让前端 RetainedTokens 立刻下降，取代原先"tracked 长度大于 history 才重置"的脆弱启发式（压缩产物可能字节更小但消息数不减，启发式会漏）。锁下 rewritten 快照再在锁外操作，避免与并发读者竞态。实现与既有 loop 内路径镜像，判定良性。

一个可忽略的副作用：压缩落的 token_usage 行 usage 四字段全零，会给用量页多计一个零消耗 turn。

## 1d9e6ad3 A1：inclusive 归一化——前提不成立，建议撤销自动识别

### 实测：MiniMax 是 exclusive，不是 inclusive

2026-07-06 对 `api.minimaxi.com/anthropic` 用唯一前缀做两次相同请求（`MiniMax-M3`，带 `cache_control`）：

```
r1（冷缓存）: input=2500, cache_read=64      → 和 2564
r2（相同请求）: input=68,   cache_read=2496   → 和 2564
```

两次 `input + cache_read` 之和严格相等，且 r2 的 `input(68) < cache_read(2496)` 直接排除 inclusive（inclusive 语义下 input 恒大于等于 cache_read）。多轮加长请求与非流式请求同样自洽。

本机 07-05（提交之前）的 session-trace 同样是 exclusive：某 7 步 turn 的 step1 `input=1553 < cache_read=14848`。**即提交当时前提就不成立，不是厂商后来改了行为。**

### "约 4 倍高估"的真因是 turn 级加总

同一份 07-05 trace，turn 级记录 `input=56322, cache_read=163328` 恰好等于 7 个 step 的逐字段加总（分毫不差）。加总值 219,650 除以真实上下文（最后一步 `input+cache_read+output` = 53,993）约等于 4.07——"约 4 倍"来自**缓存前缀在 7 次 API 调用里被重复计数**，这是消耗口径与占用口径的混用问题，与 input 语义无关。而占用口径本身（UsageTracker 取最新响应 → 压缩触发、环形表 RetainedTokens）在改动前就是正确的。

### 实现的两个缺口

即使前提成立，现有实现也修不到主路径：

1. **流式覆盖点错位**。MiniMax 流式的 usage 全量只在 `message_delta` 报（`message_start` 的 input 恒为 0），而 `normalizeInclusiveInput` 只挂在 `Chat`（非流式）和 `message_start` 两站；`message_delta` 分支直接用原始载荷覆写 `usage.InputTokens`，归一化被覆盖丢失。GUI 主路径全部走流式——所以这个"修复"在主路径上是空转的（也正因空转，当前 MiniMax 流式数字碰巧保持正确）。
2. **非流式路径被真实污染**。`minimaxi` 自动识别无条件生效，exclusive 语义下 `fresh = input - cache_read` 被地板到 0（实测 `76 - 2560 → 0`）。走非流式 `Chat()` 的辅助链路（compact 摘要器、memory review / memory panel、hooks、无流式 provider 适配器）的 usage 记账把新鲜输入吃掉了。

### 建议（2026-07-06 当日已落地 1、2；见文末"后续"）

1. 撤销 base_url 含 `minimaxi` 的自动识别；`InputTokensIncludeCacheRead` 开关本身可保留（未来真有 inclusive 端点时用），但默认必须 false 且只由配置显式开启。
2. 若保留开关，把归一化补到 `message_delta` 站点（仅当该事件重报了 `input_tokens` 时执行，避免对"只更新 cache_read"的 delta 重复减）。
3. 给任何端点打语义标记前，先跑两次相同请求实测（方法见上；`docs/context-usage-and-caching-zh.md` 附判别法）。
4. `internal/insight/types_test.go` 的 `TestModelUsage_NormalizedMiniMaxInputPreservesHitRate` 注释把"MiniMax input inclusive"写成了事实，随修复一并更正，避免误导后人。
5. 若想让用量页之外的地方不再出现 4 倍观感，需要的不是改 usage 语义，而是区分口径：占用类显示一律走 `ContextTokens` / `RetainedTokens`（已如此），turn 加总行只用于消耗计量。

## 后续（2026-07-06 修复落地）

同日完成配置模型重构，本审计的主要建议已落地：minimaxi 自动识别已删除（开关只认显式配置）、`message_delta` 站点已补归一化（带"仅当重报 input"守卫 + 流式测试）；连带完成上下文窗口双源合一（阈值 384k → 967k）、用户配置优先级修复、resume 播种改用持久化 ContextTokens、估算器系数标定。现行架构见 `docs/compaction-chains-zh.md` 与 `docs/context-usage-and-caching-zh.md`。第 4 条（insight 测试注释与命名）亦已更正。

## 顺带发现（先于本次改动，非本次引入）

- `TotalContextTokens` 不含 `CacheCreationTokens`，注释理由"cache_creation 是 InputTokens 子集"对 Anthropic 原生不成立（原生三字段互斥；Claude Code 四项全加，pi 计入 cacheWrite，codex 用 total_tokens）。后果是原生 Anthropic 上占用每轮低估约一轮增量，下一轮由 cache_read 自动补回。量级小，但公式注释是错的，建议修正。
- Claude Code 的 OpenAI 桥（`OPENAI_BRIDGE.md` 所述脚本）把 OpenAI inclusive 的 `input_tokens` 原样透传、又把 `cached_tokens` 记为 `cache_creation`，在其加法公式下重复计数——参照 cc 时注意这个桥不是可靠范本；wuu 自己 openai client 的减法处理是对的。

## 参照系速记

| 问题 | Claude Code | pi | codex | wuu 现状 |
|---|---|---|---|---|
| 占用取数 | 最后一次 usage + 新增粗估 | 最后一条 assistant usage + 估算 | 最近响应 total_tokens + 估算 | 同（UsageTracker） |
| Anthropic input 语义 | exclusive，不归一 | exclusive，不归一 | 无 anthropic 客户端 | exclusive + 可选归一开关 |
| OpenAI input 语义 | 桥未归一（缺陷） | 减 cached | inclusive 原样用 total | 减 cached |
| 系统提示里的日期 | 不在 system，会话 memoize，尾部通知跨天 | system 末尾，实例缓存 | 不在 instructions，env 段 diff 追加 | Environment 段，会话冻结 |
| 工具 schema 稳定性 | session 冻结 | 静态 | 每轮重建（Responses 缓存模型不同） | 静态 |
