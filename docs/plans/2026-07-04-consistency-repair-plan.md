# 全项目自洽性修补总纲

日期：2026-07-04
状态：施工中
性质：本文档是四路全仓审计（前后端协议 / 工具面 / 后台生命周期 / 文档契约）的结论固化 + 施工顺序。审计原始证据在各条目内联 file:line。

## 0. 四条自洽不变量（修完后作为回归标准，长期有效）

1. **每条通路两端接通**：有写必有读、有按钮必有 handler、有工具必有引导（或明确 deferred 且目录可查）。
2. **每个后台任务可观测、可恢复**：状态落盘 + 启动时对账；禁止裸 fire-and-forget goroutine 做持久化副作用。
3. **每个对象的删除/退休有完整清理协议**：衍生物清单化处置；记忆类数据归档不硬删。
4. **每份契约文档与代码一致**：状态栏如实；红线可审计；过时文档转为"思想架构"标注，不误导后续 agent。
5. **执行能力对等**（2026-07-04 增补）：worker 与大脑的执行工具面（文件/搜索/命令/网络/技能）保持一致，差异只允许出现在编排层。
6. **编排权只属大脑**（2026-07-04 增补）：spawn_agent/helpme 与子 agent 管理套件只存在于 session 主 agent 与常驻 named agent 的工具面；worker 是纯执行体，不递归派发。
7. **记忆写权跟随上下文完整性**（2026-07-04 增补）：谁持有完整上下文，谁才持有对应记忆的写权；临时执行体只读注入。

## 1. 修补清单

### P0（坏了或在骗用户）

| # | 问题 | 证据 | 修法 | 归属 |
|---|---|---|---|---|
| 1 | 群成员移除按钮点击必报错：前端全链路已接（App.tsx:6047→preload→`thread/members/remove`），后端无此方法（server.go 无 case、protocol.go 无常量；`resident_store.go:72 RemoveThreadMember` 零调用方） | 审计④D2 | 补 Method 常量 + server.go case + handler（校验群线程/成员→RemoveThreadMember→返回 wrap 成员的 thread） | 波1-B |
| 2 | worktree 会话写主仓：fork-to-worktree 步骤 5（工具 CWD 切换）未做，绑 worktree 的线程文件工具仍写 parent repo | 审计④/fork 文档自列 | `internal/toolctx` 加 WithWorktreePath；turn 启动注入；文件/shell 工具 sandbox 检查后切 CWD | 波3 |
| 3 | resident 提示词双残缺：①`UpdateSystemPrompt` 清空全部 sections（stream_runner.go:168-179），resident 失去工具引导/发现说明/deferred 目录，却继承主 agent 全部工具面（含 helpme/inception/goal/workflow）；②缺失 `## Wrapping up discussions` 段（resident 文档 §5@379-388，红线 6 违反） | 审计②§5、④D1 | ②随记忆重设计 M1 补段；①resident 提示词 v2：追加精简工具引导 + deferred 目录，或裁剪 resident 工具面（波3 决策） | ②=M1；①=波3 |
| 4 | 记忆系统整体断链 | 见 `2026-07-04-memory-redesign.md` §0 | 按该文档实施 | M1/M2/F1 |
| 5 | 子 agent 崩溃永久 running：detached goroutine（subagent/manager.go:280,702）+ 恢复路径拒绝非终态（agent_control.go:2784）+ 无启动对账；污染 CountRunning | 审计③#1 | 启动 sweep：无活 goroutine 的 running 快照 → failed/interrupted | 波3 |
| 6 | Reset 的 `restart` 档是空 case（participant_profile_handlers.go:193-194），按钮却在 | 审计①#4 | 实现（重建该 DM ThreadRuntime）或撤按钮——**决策：撤按钮**（restart 语义与"一个大脑"公理冲突） | 波1-B（后端删 case）+F 后续 |

### P1（功能失灵或语义撒谎）

| # | 问题 | 证据 | 修法 | 归属 |
|---|---|---|---|---|
| 7 | retire 是软隐藏：GetParticipant 不过滤 retired，DM 照聊；群成员/待处理信封/磁盘目录全不清理（CASCADE FK 只在 DELETE 生效，retire 是 UPDATE） | 审计①#4、③矩阵 | 退休清单：移出 thread_members、清 resident_inbox、DM 线程冻结只读+UI 标注、目录归档（记忆目录按 memory-redesign §9 归档不删） | 波3 |
| 8 | 后台 workflow fire-and-forget（tool_workflow.go:647、tool_workflow_control.go:374），崩溃卡 RunStateRunning、错误双吞 | 审计③#1 | 纳入统一后台任务框架（状态落盘+启动对账） | 波3 |
| 9 | dream/reviewer 生命周期（dream 卡 running 到下个 interval；reviewer 计数器先清零后启动，崩一次静默跳过整周期） | 审计③#6/7 | reviewer 已随 M1 退役；dream 纳入后台框架 | 波3 |
| 10 | cron 无守护：workspace 关闭任务静默不跑；FindMissedOneShots（tasks.go:280）死代码未接 | 审计③#5 | 接补跑 + 定义关闭期语义 | 波3 |
| 11 | 头像进不了聊天主界面：wire `participant.Summary`（participant.go:37-42）无 avatar 字段，前端 ChatAvatar 读取永远落空；成员 chip 连首字母都不画 | 审计①#3/4 | Summary 加 avatar 字段（或 id→avatar 查询）+ ParticipantChip 补头像 | 波4 |
| 12 | 信封语义不可见：addressed/hop/sender 后端逐条填（resident_router.go:346-348），前端零渲染 | 审计①#5 | EnvelopeNotice 增加点名/转发标注 | 波4 |
| 13 | worker deferred 目录为空（session.go:300-301 未填 DeferredToolCatalog）但提示词让它查目录；system.go:24-27/config.go:866-873 注释谎称 worker 无 spawn_agent/update_plan/记忆工具（实际全有） | 审计②§6/7 | 填充 worker 目录；改注释；给 worker 可见工具补最小引导 | 波3 |
| 14 | `checkpoint` 工具孤儿：注册+完整描述但无任何 profile 分桶，永不可达（toolkit.go:224 vs compiler.go） | 审计②§3 | 决策：接入 file 分桶 deferred，或删注册——**倾向删**（patch-journal 另有恢复路径） | 波4 |
| 15 | `#all` 首播不带成员（group_thread.go:87 裸 snapshot）；thread/updated 覆盖可能抹掉 members | 审计①#1 | 广播统一过 threadWithGroupMembers | 波4 |

### P2（卫生）

| # | 问题 | 修法 | 归属 |
|---|---|---|---|
| 16 | 全仓无 GC：线程只能归档不能删（含工件目录）、项目移除留孤儿状态树、insight 缓存无淘汰、fork worktree 泄漏 | 删除协议 + 归档区 + 设置页"清理"入口；**项目移除的状态目录处置必须给"保留记忆归档"选项** | 波3/4 |
| 17 | `insight.Run` 全流水线死代码（无任何触发入口） | 决策：接入设置页或删——倾向删除 HTML 报告路径、保留 usage 统计 | 波4 |
| 18 | OS 级通知缺失（后台 agent 回复只有应用内红点） | Electron Notification 接入 | 波4 |
| 19 | 文档漂移：resident/sidebar-groups/completion-redesign/followup-resume 四份顶栏"未实施"实为已实施；workspace-focus 红线 1 需豁免说明；resident §3.1/sidebar §4 谎称 members/remove"已有" | 见波1-A 清单 | 波1-A |
| 20 | 死代码：FindMissedOneShots、statepath.ProfileMemoryDir、sessionmemory.ContextBlocks（含项目记忆版）、legacy emoji avatar | 随各波顺手清 | 各波 |
| 21 | web_search/web_fetch/list_files 零提示词提及；tool-discovery 段误导 inception 需 tool_search（实际 visible） | system prompt 微调 | 波4 |

## 2. 施工波次

- **波1（并行，进行中）**：A=文档清理回写；B=appserver 快修（#1、#6 后端部分）；M1=记忆后端基座；F1=记忆面板前端。
- **波2**：M2 面板后端 RPC（依赖 M1 落地 + 波1-B 让出 protocol.go/server.go）。
- **波3**：统一后台任务框架（#5/8/9/10）+ 退休清单（#7）+ worker/resident 工具面重构（#3①/13）+ worktree CWD（#2）。
- **波4**：可见性（#11/12/15/18）+ 存储治理（#16/17）+ 卫生（#14/20/21）。

## 3. 协调与提交纪律（所有实施 agent 必读）

1. **原子提交直接进 main**：一个逻辑改动一个 commit；风格与仓库一致（`组件: 摘要`小写），结尾加 `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`。
2. **只 stage 自己属地的文件**（逐一 `git add <path>`，禁止 `-A`）；提交前 `go build ./...` + 属地包测试必须绿。
3. **文件属地表**（波1）：A=docs/**；B=protocol.go、server.go、group_thread.go、participant_profile_handlers.go；M1=memory-redesign §10 表；F1=desktop/src/**。属地外文件需要改动时，在 commit message 里注明并保持最小。
4. 桌面端本环境无 node：F1 的代码无法在本机跑 tsc/vitest，须严格镜像现有组件模式并自审；最终由用户在有 node 的机器上回归。
5. workspace 稳定 ID 改造（2026-07-04 另一 agent）已落地 main；涉及 session 建列/statepath 的改动先 rebase 再动。
