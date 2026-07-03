# Subagent 追加消息与失败续跑 — 设计与实施计划

日期:2026-07-03
状态:设计定稿(未实施)
性质:**本文档同时是给实施 agent 的强约束提示词。**

目标:对齐 Claude Code subagent 系统的两个交互语义——(1) 父 agent 可随时向运行中的子 agent 追加消息,在其下一个模型轮次之间注入;(2) 子 agent 失败(API 终态错误)或被停止后,父 agent 发一条消息即可**带全量上下文续跑**,包括进程重启之后。

## 0. 现状结论(2026-07-03 代码调查,实施前请自行核对行号)

两个能力都已"半建成",**本计划是补全,不是新建**:

| 已有 | 位置 |
|---|---|
| `pendingMessages` 队列 + `runner.BeforeStep` 每轮排空 | `internal/subagent/manager.go:343-347, 628-663` |
| `Manager.Followup`:running→入队;completed/**failed**→从 `sa.history` 复活新 turn(fall-through,无专门测试) | `manager.go:501-568` |
| `send_message`(仅入队)/ `followup_task`(入队或续跑)工具 | `internal/tools/tool_agents.go:891-945` |
| 失败传播:mailbox 带 `Error`+`ErrorClass`;嵌套父via `Followup` 注入,根 agent via `enqueueAgentCompletionTurn` | `agentcontrol/mailbox.go`、`agent_control.go:2275-2331`、`appserver/agent_threads.go:62-74` |
| 每 run JSON 快照(status/error/messages)写盘 | `subagent/history.go`,`historyDir/<workerID>.json` |
| 结果投递台账(防重复注入,重启可恢复) | `agentcontrol/delivery.go` |

**缺口**:(a) 死 run 的 JSON 从不被重载进 Manager——续跑不能跨进程重启;(b) `StatusCancelled` 被硬拒续跑(`manager.go:519`、`agent_control.go:1211`);(c) failed-续跑无回归测试;(d) JSON 快照缺少重建运行时所需的 spawn 参数;(e) 失败 mailbox 没告诉父 agent"这个 run 可以续跑";(f) `send_message` 与 `followup_task` 语义分裂,父 agent 需要记两套心智模型。

## 1. 目标语义(对齐 CC,逐条验收)

1. **追加**:目标 run 在 running/pending → 消息入队,该 run 下一轮 `BeforeStep` 排空合批;不打断当前工具调用。(已有,补测试锁定。)
2. **续跑**:目标 run 在 completed/failed/**cancelled** → 以 `sa.history` + 排空的 pending + 新消息开新 turn,保留模型 pin。cancelled 解除硬拒:用户停止后主动发消息 = 显式复活请求。
3. **跨重启**:进程重启后,向已死 run 发消息 → 从 JSON 快照**惰性重建**(不在启动时批量重建)→ 续跑。worktree 已被清理等不可恢复场景 → 明确报错,不静默降级。
4. **失败传播的可续跑提示**:失败 mailbox 在 `Error`/`ErrorClass` 之外附一句提示(英文,面向模型):该 run 的上下文完整保留,可用 `followup_task`/`send_message` 携新指令续跑。父 agent 因此"知道自己能救活它"。
5. **统一心智模型**:`send_message` 升级为与 `followup_task` 相同的 queue-or-resume 语义(running 入队、终态复活)。两工具保留,描述文档同步,均写明投递与续跑语义。

## 2. 设计要点

### 2.1 快照扩展(跨重启的前提)

`subagent/history.go` 的 `historyRecord` 增加重建运行时所需字段:`worker_type`、`task_name`、`agent_path`、`parent_id`、`participant_id`、模型覆盖(client/model pin 的可序列化形式)、`cwd`(含 worktree 路径)、`agent_profile`。记录加 `version` 字段;旧版本记录缺字段时续跑请求返回清晰错误("snapshot predates resume support"),不猜测。

### 2.2 惰性重建(agentcontrol 层)

寻址链 `resolveAgentIDFrom` 未命中活 run 时,新增 fallback:读 `historyDir/<id>.json` → 校验 version 与 cwd/worktree 仍存在 → 用**现有** spawn 机械(toolkit 构建、thread 注册复用,禁止复制粘贴平行实现)重组 `SubAgent{history, status, ...}` 插回 `m.agents` → 走正常 `Followup`。重建仅发生在显式寻址时;启动时不扫描。

### 2.3 投递台账互操作

续跑产生**新的**终态快照(completedAt/result 变化 → 新 deliveryID),台账天然把它当新结果协调,无需特殊处理;文档化这一点并用测试锁定(同一 run 失败→续跑→完成,父收到两次投递,各恰一次)。

### 2.4 红线

1. 复用 `Followup`/`pendingMessages`/spawn 机械,出现第二份 turn 循环或平行 run 池即返工。
2. 不动 primary agent、resident/participant 通道(另一台机器在施工 `internal/appserver` 群聊契约,**本计划禁止触碰 `internal/appserver`、`internal/session`、`desktop/`**;S6 UI 入口显式延后,待协调)。
3. TDD、原子提交、代码注释与 commit message 英文;不新增依赖。
4. 工作树有他人 WIP:严禁 `git add -A`,一律路径限定提交。
5. 测试失败先怀疑实现;禁止改测试迁就实现。

## 3. 分任务(顺序即依赖,每任务一提交)

| # | 任务 | 触及 | 验收 |
|---|---|---|---|
| S1 | failed-续跑回归测试 + cancelled 解除硬拒(`manager.go:519`、`agent_control.go:1211`) | subagent、agentcontrol | 失败 run `Followup` 续跑且 history 完整;cancelled run 同;pending 消息先于新指令合批 |
| S2 | `send_message` 升级 queue-or-resume;两工具 schema/描述重写(投递语义 + 续跑语义) | tools | running 入队不打断;终态复活;描述含语义说明 |
| S3 | `historyRecord` 扩展 spawn 参数 + version;写入点补全 | subagent/history.go、agentcontrol | 新快照含全部重建字段;旧记录读取不崩 |
| S4 | 惰性重建:寻址 fallback → 校验 → 重组 → Followup | agentcontrol | 新 Manager 实例(模拟重启)对同 historyDir 续跑成功;worktree 缺失报清晰错误;旧版本快照报清晰错误 |
| S5 | 失败 mailbox 附可续跑提示;根路径同等;台账双投递测试 | agentcontrol | mailbox JSON 含提示字段;失败→续跑→完成产生两次各一次的投递 |
| S6(延后) | 桌面端失败行"续跑"入口 + appserver RPC | appserver、desktop | **本期不做**,与群聊施工协调后另行排期 |

## 4. 刻意不做(本期)

- 启动时批量重建死 run(惰性足够,避免启动风暴)。
- 续跑时修改 worker 类型/工具集(保持原配置,降低语义复杂度)。
- 子 agent 的"孙代存活则不通知"聚合语义(wuu 通知路径已有台账去重,先观察)。
- S6 UI(见上)。
