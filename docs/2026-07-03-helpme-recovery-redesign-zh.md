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
3. 父代理 await（或收到完成通知）：agentcontrol 发现该线程有 `helper_done` 状态的 recovery，用 brief + report + result 构造 joint compact 内容（现有 `BuildHelpMeJointCompactContent` 保留），随后置 `state = applied`。重复 await 正常返回结果但不再携带重写（结构性消灭问题 2）。
   重写窗口语义修正：实施时保留现有的**整体替换**（system 前缀 + joint compact + 锚点），不迁移到 inception 的锚定保留——helpme 的目的就是丢弃被污染的上下文，从旧锚点起保留反而把污染留下。真正要从 inception 对齐过来的行为是**保留未应答的可见用户消息**（`unansweredVisibleUserSuffix`），防止 await 期间用户插话被吞。锚点字段从 recovery 对象中移除。
4. 跨重启：recovery 随快照复水；await 目标解析接入 `rehydrateAgent`（顺带修复问题 4，此项是 subagent 层通用修复）。
5. trace 文件降级为纯审计产物：写失败只在响应 next_steps 注明，不再使调用失败；ephemeral 运行因 recovery 在内存中而不再依赖它。

删除清单：`RewriteHistoryWithHelpMeCompact`（并入 inception 原语）、helpme 侧的载体名模式匹配与无预检的 JSON 解析、`HelpMeTool.Execute` 死分支、trace 回读路径（`readHelpMeMainTraceForAgent` 仅保留给旧文件的迁移读取）。

## 内容层（phase 2）：双源决策日志，替代自报简报

自报简报是结构性弱点：被污染的 agent 写出的简报带着它的错误框架（让病人自己写病历），且实测里模型会把可选字段留空导致 compact 退化为占位符。方向修正——joint compact 不是"父的自我陈述 + 帮手的报告"，而是**两个 agent 执行过程的决策日志**：目标 / 决策原因 / 执行路径（失败与成功都保留，失败路径显式标注为负知识）。与传统 compact 的两点差异：双源；负知识必须显式保留（传统摘要倾向把失败尝试当噪音丢弃）。

实现为三份有界产物的组装，任何时刻不需要把两份长历史放进同一个窗口：

1. **父侧执行摘要**：helpme 调用时用现有 compact 分块机制（块 + 滚动摘要）对父历史做机器抽取，抽取 prompt 按决策日志结构（目标/决策点/路径与结局）。产物同时喂给帮手（比自报更可信的交接）和 recovery 对象。args 降级为可选提示。
   时序修正（实施定案）：抽取在 spawn **之前同步**完成——产物必须进帮手的 handoff brief，并行就做不到这一点。代价是 helpme 调用前置一次抽取（硬超时 90s，失败/超时降级为 args+历史回退的 brief，救援永不因此失败）。已知限界：抽取沿用 compact 的每消息 500 字符截断，超长工具输出的尾部错误可能不进日志，放宽属后续项。
2. **帮手侧执行摘要**：即其收尾 `agent_report`（运行时强制、schema 有界、帮手在自己完整上下文在场时自己写）。父方永不摄入帮手原始历史。
3. **joint compact**：纯字符串组装（1）+（2）+ 目标 + 验证指针，不调模型、不受窗口约束。

边界情形（双方上下文均逼近极限、同 provider）：父侧靠分块 map-reduce（每次调用只有一块+滚动摘要）；帮手侧干净起步、自带 auto-compact、交付物被 report schema 限界；块预算从 provider 窗口推导（`compact.Budget`）。可选杠杆：`model_roles` 加 `recovery_summarizer` 角色换长窗口/便宜模型做抽取，零架构改动。降级链：抽取失败退回 args+report 组装，救援永不因摘要失败而阻塞。

触发机制维持人类反馈为主触发（提示词引导"用户说还是不对时考虑 helpme"）；机械信号（连续验证失败、同文件反复改-测循环）将来可作为 reminder 级建议，不做自动触发。

## 迁移顺序

1. 先落 subagent 层通用修复：await 复水接入（独立价值，非 HelpMe 专属）。【已落地 b0ab47d5】
2. 新增 `helpme_recovery` worker 类型（RequiresReport）+ recovery 状态对象与持久化 + 一次性消费门 + trace 降级审计 + compact 加固。【已落地 beb4d873】
3. 修复 await 与 RequiresReport 收尾轮的完成竞态（live 实测发现：await 在通知消费者启动收尾轮之前的窗口放行，exec 进程退出杀死收尾轮，报告未入库、rewrite 静默不触发、harness 任务卡 running）。修复为 spawn 时刻置位的结算状态机 + 复水对账合成被吞报告。【已落地 05c62c2c】
4. 内容层 phase 2：父侧执行摘要抽取（BuildHelpMeParentJournal）+ recovery.ParentExecutionJournal + 组装器主记录渲染 + 抽取 prompt。【已落地 6bfe757f，live 验证：日志逐字引用用户原话并进入 joint compact】
5. 删旧路径尾款（trace 回读仅保留迁移读取）与文档收敛。【本文档即收敛记录】

后续项备查：model_roles 的 recovery_summarizer 角色（长窗口/便宜模型做抽取）；机械触发信号（reminder 级建议）；wait:true 同步模式；抽取的每消息 500 字符截断放宽。

与 completion contract 重设计（另一线并行工作）的关系：本设计只消费其产出（RequiresReport、无条件投递、快照 spawn 参数），无方向冲突。
