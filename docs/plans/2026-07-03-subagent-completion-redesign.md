# Subagent 完成契约重构:生命周期归事实,报告归产物

日期:2026-07-03
状态:已实施(验证于 2026-07-04 契约审计)
性质:**本文档同时是给实施 agent 的强约束提示词。**
上游:`2026-07-03-subagent-followup-resume.md`(已实施——本文的失败传播/续跑提示与之协同)。

## 0. 问题陈述

现状:子代理循环无错结束后,harness 层查一次结构化报告,**没调过 `agent_report` 就把 completed 降级为 `awaiting_report`**(`agent_control.go` recordHarnessStatus、`await.go` 组装处)。这个状态与 completed/failed/cancelled 同居一个枚举,于是 workflow 状态机、await、工具文档、父 agent 提示词全部被迫特判它——三段指引散文、防轮询逻辑、"只表面化一次"机制都是这个类型错误的持续利息。此外 `harnessStatusFromReportOutcome` 允许模型自报 outcome 覆盖 runtime 事实。

实测故障(2026-07-03,MiniMax M3 端到端):子代理把活干完、结果正确,但没按协议调 `agent_report`,状态停在 `awaiting_report`;叠加投递路径纠缠,父 agent 拿不到结果正文。BYOK 多模型前提下这是**类别性故障**,不是个别模型的毛病。

**病根**:(a) 报告合规性(产物的质量属性)被编码成生命周期状态;(b) 协议是社会性的(依赖模型自觉),不是机械性的。

## 1. 同行证据(2026-07-03 实地调查 thirdparty/)

| 实现 | 父收到什么 | 报告协议 | "完成但未交报告"状态 | 失败时 |
|---|---|---|---|---|
| Claude Code | 最终 assistant 正文(尾部回退步行);空输出→合成占位符"(Subagent completed but returned no output.)" | 无。仅两个窄例外且均机械执行:verification 类型的 `VERDICT:` 字面行(调用方解析);非交互模式 StructuredOutput 强制工具 + schema 校验 | **无**。状态仅 pending/running/completed/failed/killed | 有部分产出则连部分结果一起交给父;被杀也打捞最后文本 |
| opencode | 最后 text part,XML 包裹 | 无 | 无(空文本照样 completed) | 真实错误信息透传 |
| crush | 最终消息文本 | 无 | 无(压根没有任务状态) | ⚠️ 反面教材:错误吞成一句 "error generating response" |
| hermes-agent | `final_response` 原文 | 无;父侧文档明示"子代理总结是自报,不是核实过的事实" | 无(空文本→failed) | timeout/error/interrupted 带诊断文本 |
| pi(示例扩展) | 最后 assistant 文本 | 无 | 无(空→"(no output)") | exitCode/stopReason 归因,stderr 透传 |
| mimocode | `<actor_result status=…>` 包裹正文 + 解析出的 `**Status**:` 头 | **有,但机械执行**:postStop 钩子发现未交报告 → **拉回子代理重入一轮**(≤3 次);解析失败 fail-soft 为 `unknown`;完成门用 DB 事实**降级**自报状态 | **无终态**——未交报告是终态前的重入循环,从不作为状态暴露给父 | run 路径工具级报错;后台走 inbox 通知 |

结论:六家中五家是纯软契约("最终正文即结果");唯一的强协议派(mimocode)也把"未交报告"实现为**重入纠偏**而非生命周期状态,并且用事实压制自报。**没有任何一家把报告合规性做成终态。**

## 2. 设计原则与方案

**原则:生命周期只由 runtime 事实决定;报告是产物,不是状态;结构靠机械约束,不靠模型美德。**

1. **删除 `awaiting_report` 状态**。枚举收敛为 pending/queued/running/completed/failed/cancelled。循环无错结束 = completed,无条件。旧持久化记录读取时映射为 completed。
2. **result 无条件送达**。mailbox 与 await 永远携带子代理最终正文;结果提取采用尾部回退步行(最后一条 assistant 消息若纯 tool_use,向前走到最近的带文本者——CC 模式);空输出填入**明确标注来源的合成占位符** `"(Subagent completed but returned no output.)"`(占位符不是伪造发言,不违反"runtime 不代答")。投递台账只负责去重注入,不得阻挡可得性。
3. **报告降格为 completed 上的类型化元数据** `report: {kind: structured | final_text, path}`。调过 `agent_report` → structured;未调 → runtime 从既有事实合成:最终正文 + 变更文件 + 已写工件 + token/时长,`kind: final_text` 如实标注。`mailbox.ReportMissing` 替换为 `report_kind`(过渡期保留 `report_missing` 为派生别名,desktop 现状只认识五个基础状态、不认识 awaiting_report,无需前端改动——实施时验证)。
4. **需要结构时机械强制**(mimocode postStop 先例,收紧为一次):worker 类型或 spawn 参数声明 `requires_report` 时,模型产出最终正文而未调 `agent_report` → runtime 追加**一次**收尾请求,`tool_choice` 强制指向 `agent_report`(复用 Followup/BeforeStep 机械)。仍未交 → completed + `kind: final_text`,**永不产生新的生命周期状态**。
5. **自报 outcome 只记录、不裁决**。删除 `harnessStatusFromReportOutcome` 对状态的覆盖;报告里的 outcome 作为"agent 的主张"存档,与 runtime 事实并列暴露,矛盾本身是给父 agent 的信号。(mimocode 完成门的方向一致且更强;v1 只做"不覆盖",事实降级留作后续。)
6. **失败也交部分结果**(CC 模式):failed/cancelled 的 mailbox 携带最后可用文本 + `ErrorClass` + 既有的 `resumable`/`resume_hint`——父 agent 既看到干到哪了,又知道能救活。禁止 crush 式的吞错误。
7. **父侧保留"自报非事实"的提醒**(hermes 措辞):spawn 工具描述加一句"子代理的总结是自报;要求它返回可验证的把手(路径/命令/ID)并自行核实"。
8. **final text 的信息量约束,分层且温和降级**(2026-07-03 补充)。约束不因去状态化而取消,而是各归其位,每层失败只往下垮一格,永不上升为生命周期事故:
   - 提示词层(主杠杆):P1 重写为正面教学——"最终正文即唯一交付物,写清楚做了什么/发现什么/改了哪些文件/什么没做成,给出可验证把手"。失败模式 = 质量差,不再是状态悬空。
   - 父侧义务(信息量由需求方定义):spawn 描述明确"派活时写清楚要求子代理在最终消息里返回什么"(六家同行一致把最重义务放父侧)。
   - 机械最低线:唯一可机械判定的质量指标是"空"。最终正文为空 → **先一次机械重入**("给出你的最终总结"),再空才落占位符。字段化需求走 §2.4 的 schema 强制收尾。
   - 事实兜底:合成报告携带 runtime 观察到的客观行为(变更文件/工件/时长),模型的话再空洞,行为记录仍在。
   - **followup 协同**:信息量不足不再是终局——父 agent 可对子代理直接追问(续跑带全量上下文),"报告不清楚"从事故降级为多一轮对话。

## 3. 提示词层全面改动清单(与代码同 PR,缺一即返工)

| # | 位置 | 改动 |
|---|---|---|
| P1 | `agentcontrol/worker_types.go` 全部 8 处 `agent_report` 教学文本(各类型 SystemPrompt 的 "Output format" 段 + `OutputSchema` 字段) | 重写:默认契约 = "你的最终正文就是交给父 agent 的结果,直接写清楚 outcome/变更/阻塞";`agent_report` 仅在该类型 `requires_report` 时保留义务表述(verifier/reviewer 类),且措辞从"必须记得调"改为"收尾时会被要求提交"(机械强制兜底,见 §2.4) |
| P2 | `tools/tool_agents.go:974` spawn_agent 描述 | 删除 "Results can include status='awaiting_report'…" 整句;加入 P7 的自报提醒 |
| P3 | `agent_control.go:2707`、`await.go:170`、`await.go:267` 三段 await 指引 | **整段删除**(状态不存在了,无需教学);next-steps 构造器相应收缩 |
| P4 | `tools/tool_workflow_control.go:39` 与 `:1013`、`tool_workflow_status.go:611` | 删除 awaiting_report 分支与表述;goal 状态更新改挂在报告元数据上 |
| P5 | `tools/tool_agents.go:1236+` agent_report 工具 description | 改为:"可选的结构化交接。不调用不影响完成状态;requires_report 的 worker 会在收尾时被要求提交。" |
| P6 | helpme 路径(`tool_agents.go:358-`、`:566-`)的 report_missing 语义与主轨迹文案 | 随 `report_kind` 语义更新 |
| P7 | `prompts/system_main.md` spawn/helpme 段 | 核对确认不载协议(现状如此)则不动;若实施时发现引用,同步删除 |

验收标准:`grep -r "awaiting_report" internal/ prompts/` 在非迁移代码中零命中;三段指引散文是**删除**而非改写——需要散文来教父 agent 应对的状态,就是不该存在的状态。

## 4. 分任务(顺序即依赖,每任务一原子提交,TDD)

| # | 任务 | 触及 | 验收 |
|---|---|---|---|
| T1 | 枚举收敛 + 旧值读取映射(harness/workflow/await/mailbox);recordHarnessStatus 重写(事实独占状态,报告查询只填充元数据);删除 outcome 覆盖 | harness、workflow、agentcontrol | 全部消费方编译;旧 awaiting_report 持久化记录读为 completed |
| T2 | result 无条件化:尾部回退步行 + 空输出占位符 + await/mailbox 永远带正文;台账只管注入去重 | subagent、agentcontrol | 无报告的 completed run,await 与 mailbox 均含正文;空输出得占位符 |
| T3 | `report_kind` 元数据 + 事实合成报告(final_text 路径) | agentcontrol、mailbox | 未调 agent_report 的 run 有 kind=final_text 的合成报告与 path |
| T4 | `requires_report` 机械收尾:一次 tool_choice 强制 agent_report 的重入(复用 Followup 机械,禁止平行循环) | agentcontrol、subagent、providers(tool_choice 透传) | verifier 类型忘调 → 被强制收尾一次;再不交 → completed+final_text |
| T5 | 提示词清单 P1-P7 全量落地 | worker_types、tools | §3 验收标准 |
| T6 | 失败部分结果:failed/cancelled mailbox 带最后文本(与既有 resume_hint 并列) | agentcontrol | 中途失败的 run,父可见部分产出 + 可续跑提示 |
| T7 | e2e 复验:重跑 2026-07-03 的 MiniMax M3 实验一 | — | 子代理状态 completed(而非 awaiting_report),父 agent 直接拿到正文 |

红线(沿用 followup-resume 文档 §2.4):复用现有机械、不碰 `internal/appserver`/`internal/session`/`desktop/`(群聊施工区;desktop 不认识 awaiting_report,后端收敛后无需前端改动)、TDD、原子提交、路径限定提交、注释与 commit message 英文、测试失败先怀疑实现。

## 5. 刻意不做(本期)

- mimocode 式的完成门(DB 事实降级自报状态):方向认可,等 workflow 的任务事实源稳定后再评估。
- 多次重入纠偏(mimocode 是 ≤3 次):v1 只强制一次,失败即 final_text 兜底——重入越多,越接近社会协议的老路。
- hermes 式"空输出 = failed":拒绝——空输出不是 runtime 错误,伪造失败违反事实原则;CC 式占位符 + 父 agent 自行判断(还可用 followup 续跑追问)。
- 桌面端展示 `report_kind` 的 UI:等群聊施工完毕统一排。
