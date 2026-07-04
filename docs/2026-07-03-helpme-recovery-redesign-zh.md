# HelpMe 重设计：把「救援」还原为 handoff + compact 两个基座原语的组合

HelpMe 的本质是一次 subagent handoff 加一次 compact。当前实现在这两个基座**之外**各自重造了一套：自己的交接简报、自己的状态传递（trace 文件当 IPC）、自己的历史重写函数、自己的触发通道（按工具名模式匹配工具结果 JSON）。本次审查（含 MiniMax-M3 实测）发现的问题几乎全部源于这个"旁路"结构。本文先审计两个基座，再给出派生设计。

## 基座审计一：subagent 系统

completion contract 重设计（P1-P7，2026-07-03）之后，subagent 系统已经具备 HelpMe 需要的全部原语，但 HelpMe 尚未接入：

| HelpMe 需要什么 | 基座现在提供什么 | HelpMe 现在怎么做 |
| --- | --- | --- |
| 帮手必须交结构化报告（重写的前置条件） | `WorkerType.RequiresReport`：运行时在完成时强制一轮机械收尾补交 `agent_report` | 在 prompt 文本里"恳求"模型交报告；模型不交则重写永不触发，静默降级 |
| 交接上下文（goal/ask/reason）在完成时可取回 | `PersistedRun` 快照已持久化 Prompt/Description 等 spawn 参数，跨重启可复水 | 写进 `$SESSION_DIR/helpme/*.json` trace 文件，await 侧再解析回来；且只存原始 args，spawn 时从历史解析出的真实用户目标丢失（实测确认 compact 里是占位符） |
| 结果可重复读取 | 投递账本改为"去重注入、不限读取"，重读返回全文并标注 `ResultConsumed` | 依赖旧的 claim 语义写的门控，未用 `ResultConsumed` 防止重复重写 |
| 跨重启定位帮手 | `rehydrateAgent` 懒复水（send_message/followup 已接入） | `await_agents` 的目标解析未接入复水，resume 后 `not_found`（实测确认），文档承诺的 helpme→await 闭环断裂 |

结论：subagent 基座本身是健康的，问题是 HelpMe 停留在重设计之前的假设上，加上 await 复水这一个真实缺口。

## 基座审计二：compact 系统

compact 系统里有**两套并行的历史重写原语**，HelpMe 用的是弱的那套：

| 维度 | inception 重写（强） | HelpMe 重写（弱） |
| --- | --- | --- |
| 重写范围 | 锚定：保留 `messages[:anchor+1]`，从命名检查点起重写 | 整体：只保留开头 system 前缀，其余全弃 |
| 未应答的用户消息 | `unansweredVisibleUserSuffix` 显式保留 | 直接吞掉（await 期间用户插话会丢） |
| 注入形态 | 隐藏 user 消息（`ContextContinuationName`） | 裸 system 消息（另一种惯例） |
| 失败语义 | 锚点不存在则显式报错 | 无锚点概念，不会失败也不可定位 |
| 触发通道 | 工具结果 JSON 信封，按 kind 校验 | 同左，但凡名为 `helpme` 的工具消息都会被 `json.Unmarshal`，解析失败会让整个 turn 失败（无 `"history_rewrite"` 子串预检） |

结论：应当只保留一套重写原语（inception 的锚定版），HelpMe 降级为"内容构造者"。

## 实测与代码级问题清单（现状）

1. joint compact 丢失真实用户目标（实测）：trace 只存原始 args，await 侧无历史可回退，落到占位符。
2. 同会话重复 await 已完成帮手会二次重写，清空恢复后的全部进度（代码级；新投递语义下带全文重写，更隐蔽）。
3. `HelpMeTool.Execute` 的同步完成分支不可达且引用从未赋值的字段（约 50 行死代码）。
4. resume 后 await 找不到帮手（实测）：复水未接入 await。
5. trace 写盘失败会让整个 helpme 调用报错，但帮手已 spawn，模型重试即产生孤儿加重复帮手。
6. ephemeral 运行无 SessionDir，trace 不落盘，重写永不触发，无提示静默降级。

## 派生设计：HelpMe v2

核心：引入一个由 agentcontrol 持有的一等状态对象，替代 trace-文件-当-IPC。

```
recovery := {
  id, thread_id,
  anchor_id,          // helpme 调用时种下的 context anchor
  brief,              // 已解析的 goal/ask/reason/constraints/attempts/evidence
  helper_id,
  state: spawned -> helper_done -> applied | abandoned
}
```

流程：

1. `helpme(args)`：解析 brief（args 优先，历史回退，占位符兜底）；种锚点（复用 checkpoint 机制）；以 `helpme_recovery` worker 类型 spawn 帮手，该类型声明 `RequiresReport: true`（prompt 不再恳求报告）；recovery 对象随 run 快照持久化。异步返回，同今天。可选 `wait: true` 走同步 Spawn，一次调用完成全流程（取代死代码分支）。
2. 帮手完成：运行时按 RequiresReport 契约保证 agent_report 存在。
3. 父代理 await（或收到完成通知）：agentcontrol 发现该线程有 `helper_done` 状态的 recovery，用 brief + report + result 构造 joint compact 内容（现有 `BuildHelpMeJointCompactContent` 保留），以 **inception 锚定重写**的形式返回（anchor = recovery.anchor_id），随后置 `state = applied`。重复 await 正常返回结果但不再携带重写（结构性消灭问题 2）。锚定重写同时保证：卡住之前的历史与未应答用户消息不再被吞。
4. 跨重启：recovery 随快照复水；await 目标解析接入 `rehydrateAgent`（顺带修复问题 4，此项是 subagent 层通用修复）。
5. trace 文件降级为纯审计产物：写失败只在响应 next_steps 注明，不再使调用失败；ephemeral 运行因 recovery 在内存中而不再依赖它。

删除清单：`RewriteHistoryWithHelpMeCompact`（并入 inception 原语）、helpme 侧的载体名模式匹配与无预检的 JSON 解析、`HelpMeTool.Execute` 死分支、trace 回读路径（`readHelpMeMainTraceForAgent` 仅保留给旧文件的迁移读取）。

## 迁移顺序

1. 先落 subagent 层通用修复：await 复水接入（独立价值，非 HelpMe 专属）。
2. 新增 `helpme_recovery` worker 类型（RequiresReport）+ recovery 状态对象与持久化。
3. 切换重写通道到 inception 锚定原语，加一次性消费门。
4. 删旧路径与死代码，迁移测试。

与 completion contract 重设计（另一线并行工作）的关系：本设计只消费其产出（RequiresReport、无条件投递、快照 spawn 参数），无方向冲突。
