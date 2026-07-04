# 工作焦点(Workspace Focus)— 设计与实施计划

日期:2026-07-03
状态:设计定稿(DM、群聊、信封携带焦点、压缩后重声明均已实施)
性质:**本文档同时是给实施 agent 的强约束提示词。**

## 0. 动机:缓存纪律优先于便利

具名常驻 agent(如 Andy)的 DM thread 就是它的大脑:`internal/agent/cache_hint.go`
头注释写明 `PromptCacheKey` 落到 thread ID,整条历史都指望这一点做 prompt-cache
命中。产品上想让用户按天/按话题把 agent 的注意力钉在某个工作区("现在只看
acme 这个项目"),最直接的实现是把"当前项目"写进 system prompt —— 但那样做,
每次切换焦点都会让 `system` 消息哈希变化,直接打穿 Anthropic/OpenAI 的稳定前缀
缓存,整条历史重新计费。这是本设计唯一不能妥协的约束:

> **焦点绝不进 system prompt。** `internal/appserver/participant_prompt.go`
> 一个字节都不动。

代替方案:焦点是**消息流的属性**——像一条普通的历史消息一样持久化,只在真正
变化时声明一次,声明之后的所有轮次(包括进程重启后的重放)都能从历史里"重新
推导"出当前焦点,而不需要额外的可变系统提示。这正是 `envelope_meta` 已经在用
的模式(见 §3),本设计照抄它的骨架。

## 1. 三态编码

单个字符串字段 `focus_workspace`,贯穿 wire 协议、session 持久化、turn 参数:

| 值 | 含义 | 工具 cwd |
|---|---|---|
| `""` / 缺省 | 全部注册工作区(现状行为,未使用本功能时完全不变) | agent home |
| `"~"` | 仅个人空间(agent home 目录) | agent home |
| 其他值 | 必须是 `internal/workspaces/workspaces.go` 里 `Workspace.Name`(来自 `<wuuHome>/projects.json`) | 该工作区 `Root` |

不新增第二个字段、不新增枚举类型——单字符串足以表达三态,且能直接当作
`session_messages`/`sessions` 表的一列存,和 `dm_participant_id` 的风格一致。

## 2. 协议改动

- `Thread.focus_workspace,omitempty`(`internal/appserver/protocol.go`):只读快照,
  跟 `DMParticipantID` 一样从 `threadState`/`session.Session` 镜像出来。
- `TurnStartParams.focus_workspace`(protocol.go:961 附近),类型 `*string`,跟
  `PermissionMode *string` 同一风格:
  - `nil`(JSON 里完全不带这个字段)= **不改变焦点**,这是绝大多数轮次走的路径,
    历史字节不变,缓存不受影响。
  - 非 nil(哪怕指向空字符串)= **请求切换**到该焦点。空字符串显式请求切回"全部
    工作区"。
  - 用 `*string` 而不是"空字符串当默认值"的写法,是因为空字符串本身是一个合法
    且有意义的目标态(切回全部),必须能和"没传"区分开,否则用户没法从工作区
    焦点切回默认视图。

## 3. 幂等判定 + 声明项注入

`turn/start` 落到某个 thread 时:

1. 只有 chat 风格 thread(`th.DMParticipantID != "" || th.Group`)才处理
   `focus_workspace`;工作会话(project/scratch)完全忽略该字段——它们本来就没有
   "焦点"概念。**本次实施只接线 DM 分支**;判定/构造声明文本的辅助函数按 thread-
   kind 无关的方式书写(输入是"当前焦点字符串"+"请求焦点字符串",不依赖
   `threadState`),群聊 worker 可以直接复用,不需要重写。
2. 请求值先对 `workspaces.List(wuuHome)` 校验:非法工作区名(不在 roster 里、也
   不是 `""`/`"~"`)→ 直接返回明确的 JSON-RPC 错误,不触碰线程状态、不注入任何
   东西。
3. 合法值和 session 里持久化的当前焦点比较:
   - **相同**→ 什么都不做(幂等)。这是切换后续每一轮的常态路径:前端/模型可以
     无脑在每个 `turn/start` 里都带上 `focus_workspace`,只要没变就不会产生任何
     额外历史条目,不影响缓存。
   - **不同**→ (a) 持久化新焦点到 `session.Session.FocusWorkspace`(见 §4);
     (b) 在本轮真正的用户消息**之前**,往历史里插入一条焦点声明项(见 §3.1),
     作为一个独立的、已完成的合成 turn(仿照 `group_thread.go` 的
     `handleGroupTurnStart` / `participant_handlers.go` 的
     `RecordUserMessage` 分支:锁 → 持久化 → `appendUserMessageTurnLocked` →
     解锁 → `NotificationTurnStarted`),然后真正的用户 turn 才照常走
     `startResidentTurn`。

### 3.1 声明项:双内容 + 结构化元数据

仿照 `internal/appserver/envelope.go` / `resident_router.go` 里
`MessageEnvelope` 的双内容模式(模型可见 `Content` vs 前端渲染用
`DisplayContent` + `envelope_meta`):

- **模型可见 `Content`**(同时作为 `DisplayContent`,文案短到不值得区分):
  - 全部工作区:`[focus: all registered workspaces]`
  - 个人空间:`[focus: home directory only]`
  - 具体工作区:`[focus: <name> — <root>]`
- **结构化元数据 `focus_meta`**(新字段,和 `envelope_meta` 平行,不复用同一列——
  语义不同,`envelope_meta` 是路由簿记,`focus_meta` 是焦点声明):
  ```json
  {"kind": "all" | "home" | "workspace", "name"?: "...", "root"?: "..."}
  ```
  `kind="all"` 不带 `name`/`root`;`kind="home"` 带 `root`(agent home 路径,方便
  前端不用另查);`kind="workspace"` 两者都带。前端渲染分割线是**后续工作**(本
  实施不碰 `desktop/`),这里只保证数据管道通到 `session_messages` 表、往返
  (load→save→load)字节一致。

**必须持久化进历史,不允许做成临时/运行时注入。** 缓存正确性的前提是"每次从
`session_messages` 重建 prompt,字节完全一致"——如果声明项只在内存里、进程重
启或 thread/resume 后就消失,重放出来的历史会和原来发给模型的不一致,等于悄悄
破坏了已经产生的 prompt cache 条目对应的"事实"。

## 4. Session 持久化层

跟 `DMParticipantID` 的存取模式对齐(`internal/session/session.go`):

- `Session.FocusWorkspace string`(`json:"focus_workspace,omitempty"`)。
- `sessions` 表新增列 `focus_workspace TEXT NOT NULL DEFAULT ''`,走
  `addColumnIfMissing` 迁移(不改 `CREATE TABLE IF NOT EXISTS` 字面量,跟
  `dm_participant_id`/`is_group` 的演进方式一致)。
- `SetFocusWorkspace(sessDir, id, focus string) (Session, error)`:**区别于**
  `BindDMParticipant`——`DMParticipantID` 建线程时定死、终身不变;
  `FocusWorkspace` 反之,预期在线程生命周期里反复改,所以是个普通的可重复调用
  setter,不是"只准调一次"的绑定操作。
- `session_messages` 表新增列 `focus_meta TEXT NOT NULL DEFAULT ''`,
  `HistoryRecord.FocusMeta json.RawMessage`,插入/查询路径完全比照
  `envelope_meta` 那一列。

## 5. 工具 cwd

DM thread 的 turn 开始时,`internal/tools.Toolkit` 的执行根需要跟着焦点走:

- 焦点 = 具体工作区 → cwd = 该工作区 `Root`。
- 焦点 = `"~"` 或 `""` → cwd = agent home(现状不变)。

`Toolkit` 目前没有"运行时可改根目录"的入口——`RootDir` 只在 `New()`/
`CloneForRoot()` 里设一次,后续所有工具(bash cwd、search root、file 显示路径)
都直接读 `t.env.RootDir`。给 `Toolkit` 加一个 `SetRootDir(dir string)` setter
(跟 `SetFileScopeRoots`/`SetStateDir` 同风格的字段更新,不重建 Toolkit、不影响
其余可变状态),挂到 `turn_handlers.go` 里已经存在的
`configureResidentThreadRuntime`——这个函数本来就在每个 DM turn 开始前(线程
非 running 时)跑一次、设置该轮的 `FileScopeRoots`,现在同一处根据
`th.FocusWorkspace` 多设一次 `RootDir` 即可,改动面很小。

注意 `FileScopeRoots`(文件工具白名单)**不**跟着收窄——焦点只改变"默认在哪干
活"(bash cwd、相对路径解析、搜索根),不改变"能不能读/写别的注册工作区"。收紧
读写范围不在本次范围内,契约里也没有要求。

## 6. 后续工作的状态

- **群聊分支**(已实施):`th.Group` 线程复用与 DM 完全相同的
  `applyTurnWorkspaceFocus`(§3 的判定/声明函数本就是 thread-kind 无关)。群
  没有"每个成员各自的焦点"这回事——焦点是 thread 的属性,和 DM 一样是单个
  `session.Session.FocusWorkspace` 值,声明项落进群自己的持久历史,群里的每个
  成员读群 transcript 时都能看到同一条分割线。`handleGroupTurnStart`
  (`internal/appserver/group_thread.go`)在记录调用方自己的消息之前调用它,和
  `handleTurnStart` 的 DM 分支顺序一致。
- **信封携带焦点**(已实施):`MessageEnvelope`(`internal/appserver/envelope.go`)
  加了 `Workspace string` 字段,值 = 来源 thread 路由那一刻的存储焦点
  (`""`/`"~"`/工作区名三态,同 §1)。`routeUserMessageToResidents`(用户消息路由)
  和 `routeParticipantMessageToResidents`(agent 消息路由,`internal/appserver/resident_router.go`)
  都在读 `source.FocusWorkspace` 时把它顺手填进信封——这两处是全部信封构造点。
  `MessageEnvelope.Prompt()` 渲染的 `<incoming_message>` 属性列表相应增加可选的
  `workspace="..."`:`""` 不带该属性(信封已经很紧凑,没有焦点就不占字节);
  `"~"` 渲染成 `workspace="home"`(比 `"~"` 对模型更好读);具体工作区名原样输出。
  信封是自包含的,每条都带,不做"只在变化时声明"的差量——常驻大脑的收件箱
  交错着来自多个来源 thread 的消息,各自的焦点互相独立,没有单一"当前焦点"可
  比对,差量设计在这里没有意义。
  另见 `docs/plans/2026-07-03-resident-named-agents.md` §4.1 "2026-07-03 增补三"
  (信封格式改动的权威记录,红线 6 要求先改那份文档)。
  - **信封驱动 turn 的 cwd**(已实施,`drainResidentAgent` /
    `applyEnvelopeBatchCWD`,均在 `internal/appserver/resident_router.go`):
    排空一批信封起 turn 前,收集这批信封的 `Workspace` 值,去重后如果恰好剩
    一个非 `""`/非 `"~"` 的工作区名,就把这次 turn 的工具执行根切到该工作区;
    否则(没有信封声明工作区、多个信封分歧、或唯一剩下的值就是 `"~"`)保持
    agent home。这条规则只影响*这一个*信封驱动 turn 的 cwd——它既不会覆盖、
    也不会读取该常驻 DM thread 自己持久化的 `focus_workspace`(那是用户直接
    对话时声明的焦点,语义上和"我这次醒来是因为收到了别人的消息"完全不同),
    也不会往该 DM 的历史里注入任何声明项。一旦这个信封批次处理完、下一次
    `ensureThreadRuntime` 在空闲态运行,cwd 会按 §5 的规则重新落回该 DM 自己
    的持久焦点(通常是 home,因为常驻 agent 的自身焦点很少被设置)。
- **压缩(compact)时的重声明**:见下方状态更新。
- **前端渲染**:分割线 UI、`focus_workspace` 选择器等,`desktop/` 完全不碰。
  注(2026-07-04):前端焦点选择器与分割线已于后续实现(ChatFocusChip、
  ChatThreadView 的 focus 行),本节保留为历史记录。
- **文件作用域收紧**:见 §5 末尾。

### 6.1 压缩后重声明——状态更新

见 §7"压缩后重声明"一节的完整设计与实施记录。

## 7. 压缩后重声明(已实施)

### 7.1 调查结论

`internal/agent/loop.go` 的压缩(proactive 或 overflow 触发,`cfg.Compact`)完成
后会调用 `cfg.OnCompact(CompactInfo{...})`(loop.go 三处调用点);
`internal/agent/stream_runner.go` 把 `OnCompact` 接到 `effectiveOnEvent`,产出一
个 `providers.StreamEvent{Type: providers.EventCompact, ...}`。这个事件和其余
流式事件走同一条管道到 appserver:`turn_handlers.go` 主 turn 循环
(`runner.RunWithCallback` 的回调,约 1058-1071 行)和 `agent_threads.go`
(子 agent 流转发,约 101-103 行)都在 `th.mu.Lock()` 之下调用
`th.applyStreamEventLocked(turnID, ev, now)`(`model.go`),该函数已有
`case providers.EventCompact:` 分支,专门用来把压缩记成一条
`ThreadItemContextCompaction` 时间线条目。

结论:**存在现成的、已在 `th.mu` 保护下运行的压缩完成事件钩子**,不需要用
"历史根是否为压缩摘要"这种事后启发式替代方案。

### 7.2 实施

- `threadState`(`internal/appserver/server.go`)新增字段
  `focusDeclarationStale bool`,和 `FocusWorkspace` 一样受 `th.mu` 保护。
- `model.go` 的 `applyStreamEventLocked`,`case providers.EventCompact:` 分支
  开头置位:`th.focusDeclarationStale = true`。压缩只会发生在该 thread 自己
  正在跑的 turn 内,而 `applyTurnWorkspaceFocus` 只在 turn 未运行时被调用
  (`handleTurnStart`/`handleGroupTurnStart` 的忙碌检查已保证互斥),所以读写
  该标志不需要额外同步。
- `workspace_focus.go` 的 `applyTurnWorkspaceFocus`:在最初读 `current`/
  `homeRoot` 的同一把锁下多读一次 `stale := th.focusDeclarationStale`;两处
  幂等短路判断(`requested == current`、`focus == current`)都追加
  `&& !stale` 条件——有该标志时即使请求值和存储值相同也照常往下走完整声明
  流程(校验、持久化、注入声明项);声明真正落地时(`th.FocusWorkspace = focus`
  那一行旁)把 `th.focusDeclarationStale = false` 清掉。
- 不新增字段以外的持久化:该标志只活在内存 `threadState` 里,不落
  `session.Session`/`session_messages`——这是有意的,压缩后重声明是"下一次进
  程内的判定"要做的事,不是需要跨进程重启存活的状态(重启后历史从存储重放,
  重放路径本来就不会带着一个"待重声明"位;若重放后历史里确实丢了声明项,
  下一次真正的 `turn/start` 请求焦点时会自然按当前值声明一次,不依赖这个
  标志)。
- 测试:`internal/appserver/workspace_focus_test.go`
  `TestDMTurnFocusRedeclaresAfterCompaction`——先正常声明一次焦点,再直接调用
  `th.applyStreamEventLocked` 喂一个 `EventCompact` 事件模拟压缩(不构造真实
  会溢出上下文的历史,因为这里要测的是 appserver 侧的反应,不是
  `internal/agent` 的压缩触发时机启发式),断言标志置位;随后用**相同**焦点值
  再发一次 `turn/start`,断言声明项数量从 1 变成 2(若无本次修复,这一步应为
  幂等空操作,数量仍是 1);最后再发一次相同值,断言标志已清、行为恢复幂等
  (数量仍是 2,不再新增)。

## 8. 红线

1. `internal/appserver/participant_prompt.go` 一个字节都不动——焦点绝不进
   system prompt。
   - **豁免说明(2026-07-04)**:participant_prompt.go 后因
     `2026-07-03-sidebar-groups-andy-workspaces.md` §5/§6 的工作区清单特性被
     修改,且 `2026-07-04-memory-redesign.md` 进一步修订其记忆段——"焦点不进
     system prompt"的实质约束不变,字面意义的"一个字节不动"废止。
2. 声明项必须走持久化路径(`appendChatMessage` + `session.AppendHistoryRecord`
   等价物),不允许只存在于内存 `threadState.History` 里。
3. `focus_workspace` 请求为 `nil`(未传)时,历史必须与不传这个字段时完全一样
   ——不能因为"支持了这个功能"就在无关轮次里多写一个字节。
4. 判定/校验函数保持 thread-kind 无关,方便群聊 worker 复用。
5. 不改 `desktop/`。
