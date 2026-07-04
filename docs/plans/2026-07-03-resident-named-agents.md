# 常驻 Named Agent：一个大脑、一个收件箱、带目标的发言

日期：2026-07-03
状态：已实施（2026-07-03）；记忆相关段落（§5 的 MEMORY.md 表述）由 docs/plans/2026-07-04-memory-redesign.md 修订，以该文档为准
性质：**本文档同时是给实施 agent 的强约束提示词。实施时不得偏离第 0 章的设计理念与第 8 章的红线；凡与本文冲突的现有实现，以本文为准修改实现，而不是修改本文。**

前置阅读：`docs/2026-07-02-conversation-native-multi-agent-zh.md`（调研与 Phase 1-5 总纲，本文是其 Phase 4/5 的修订与细化）。前端一切视觉规范以该文第 7 章为准，本文不重复。

修订记录（2026-07-03，与 `2026-07-03-chat-style-threads-design.md` 对齐及设计讨论落档）：

- **DM 回复通道裁决**：DM 回复统一走 `post_message`（缺省 thread_id = 本 DM）；assistant 正文只是工作过程，聊天视图不渲染。裁决理由：正文回复必然流式，与聊天视图"完整气泡到达"的模型冲突。§2/§4.4/§4.5/§5/§6/§7.2 已同步。
- **收敛与落档（提示词层 v1）**：§5 提示词新增"被要求时三段式收敛 + 定案写入 MEMORY.md"的普适技能；发起权单数、归用户，见新增 §5.1。
- **缓存纪律**：MEMORY.md 内嵌 system prompt 的缓存失效问题记入 §5 实施留意。
- **§10 修正**：held draft 推迟理由修正（过期场景存在，推迟是频率未知）；effort 挡位搁置；群级 facilitator 角色 v2 占位。
- **2026-07-03 增补二**（`2026-07-03-sidebar-groups-andy-workspaces.md`）：§5 提示词新增 "Building teams and groups" 与 "Workspaces and file scope" 两段；§6 矩阵增补群管理工具与文件范围行。默认组队 agent Andy、`create_group`/`add_group_member` 契约、工作区白名单强制的完整设计见该文档。
- **2026-07-03 增补三**（`2026-07-03-workspace-focus.md` "信封携带焦点"，已实施）：`MessageEnvelope` 加 `Workspace` 字段，快照来源 thread 路由时刻的存储焦点；`Prompt()` 渲染的 `<incoming_message>` 属性列表相应增加可选的 `workspace="..."`（`""` 不带该属性，`"~"` 渲染为 `workspace="home"`）。信封自包含、每条都带，不做差量——收件人是一个跨多来源交错收信的常驻大脑，没有单一"当前焦点"可比对。见 §4.1。同一设计文档还定义了 `drainResidentAgent` 排空信封批次时的 turn cwd 规则：批次内所有信封一致指向同一具体工作区时，本 turn 的工具执行根切到该工作区；否则（无信封声明工作区、多个信封分歧、或唯一值是 `"~"`/home）保持 agent home。这只影响信封驱动 turn 的 cwd，不写回、也不声明该常驻 DM thread 自己的 `focus_workspace`——那是用户直接对话的语义，与信封路由无关。

---

## 0. 设计理念（不可协商，逐条写进代码与提示词）

以下九条是产品的公理。任何实现决策与本章冲突时，实现错了。

1. **一个 named agent = 一个连续的 actor。** 只有一个"大脑"（一份持续演进的上下文 + 一个持久家目录）。它不是"每条消息孵化一个新实例"——那是现状的 bug，不是特性。名字之所以值钱，是因为它压缩了时间线（《Agents Need Names》）；每次都是新实例的话，名字只是装饰。
2. **DM 和群聊只是两个消息入口，不是两个 agent。** 用户在 DM 里说的话和在群聊里 @它 的话，进入**同一份上下文**。它在 DM 里"知道"群里发生的事，这是特性不是泄漏。
3. **收件是 inbox 语义，不是中断语义。** 消息以紧凑信封（envelope）进入 agent 的收件箱；agent 忙时信封排队，下个 turn 合并消费。不把整个房间的历史推进 context——"Signals it doesn't pull don't enter the working context; they stay queryable"（Raft 工程博客）。细节由 agent 用工具主动拉取。
4. **群内每条消息都唤醒每个成员 agent 推理一次。** 这是刻意的（对齐 Raft 的实际行为）：agent 要能"注意到"没点名它的问题。成本由信封紧凑性、合批、hop 限制控制，不由"不唤醒"控制。
5. **回复是带目标的显式动作。** 在哪回复、要不要回复，由 agent 自己判断并用工具显式表达（`post_message` 带目标 thread）。回不回、回在哪，不由"孵化它的那条消息在哪"被动决定。
6. **两条硬规则，其余自由裁量。** (a) 被 DM 或被 @mention（addressed）：**应当回应**——要么实质回复，要么 `decline` 给一行理由。这是提示词层的强约束，**runtime 不代答**：不注入 synthetic decline，不伪造 agent 没有说过的话；runtime 只把"addressed 未回应"记为内部 telemetry 事件，供我们调优提示词和路由，不做成用户可见指标（§4.5）。(b) 未被点名的群聊消息，**按发送者（信封 `from`）分两类**：**用户对房间发话**（`from=user`，即便没 @ 你）——用户是在跟整个房间说话而非被你旁听到，直接的问候/提问（"有人吗""你们好""谁能看下…"）**理应有人应**，即使没被 @ 也别让用户对着空房间说话；简短回应即可，队友已答过的简单问题不必复读。**其他 agent 未点名你的转发**（`from=agent`）——视为 ambient 信息，**只在真有增量价值时发言**，静默是默认，禁止附和、复读、"+1"。（"用户对房间发话"允许多人自然响应；避免刷屏由提交期的乐观并发/让出机制收口，见 §4.5 待补的 held-draft，而非靠强制沉默。）
7. **agent↔agent 消息走同一机制。** A 在群里发的 `post_message` 对其他成员 agent 生成同样的信封，无特权通道。防 ping-pong 用 hop 计数硬限制 + 提示词约束，双保险。
8. **上下文膨胀的治理组合拳**：紧凑信封（只含单条新消息）+ 忙时合批 + `fetch_thread_messages` 按需拉取 + 常驻 thread 复用现有 compaction（`internal/compact`）+ `MEMORY.md` 作为跨 compaction 的持久层。不发明新的上下文管理机制。
9. **不新建平行宇宙。** agent 的大脑就是它的 DM thread（复用全部现有 turn/history/compaction/fork/检视机制）；群聊就是现有 conversation thread + 成员关系。发现自己在写一个平行的 session 系统，就是走错了。

---

## 1. 现状与问题（为什么必须改）

当前实现（截至 commit d9aeaf11）：

| 现状 | 位置 | 问题 |
|---|---|---|
| DM 发消息 = `sendPromptToParticipant` → `participant/start` → `AgentControl.Spawn`，每条消息孵化一个一次性 sub-agent run | `desktop/src/renderer/App.tsx:5752-5770`、`internal/appserver/participant_handlers.go:228` | 违反公理 1：无多轮上下文。agent 每条消息只看到 人设 + MEMORY.md + 本条消息（`namedParticipantPrompt`，participant_handlers.go:306） |
| DM thread 的 History 有记录但从不喂给孵化的 run | `participant_handlers.go:203-226` | History 只是 UI 展示品，"假多轮" |
| 群聊 @mention 走同一 spawn 路径 | mention 路由模板（commit 6bf69a59） | 违反公理 2：群聊和 DM 同时发消息 = 两个互不知情的克隆并行跑 |
| 回复位置被动等于孵化来源 | `PostParticipantMessage` 落在 ParentID thread | 违反公理 5：agent 无法选择在哪回复 |
| agent 无法感知未点名它的群聊消息 | 无路由 | 违反公理 4 |
| 提示词没写设计理念 | `namedParticipantPrompt` | 违反公理 6：没有 addressed 必答/未点名慎言的约束 |

已有的、必须复用的地基：

- DM thread 已绑定 `dm_participant_id` 且 CWD 已是 agent 家目录（`statepath.AgentHomeDir`，thread_handlers.go:78-93）→ 它天然就是"常驻 thread"的壳。
- `subagent.Manager.Followup`（internal/subagent/manager.go:501）已实现"idle 开新 turn / running 排队 `pendingMessages`、下个 turn 合并"——收件箱合批语义照此模式在 thread 层重做一份。
- `post_message` / `decline` 工具、`conversation-native` bundle、速率限制、`participant_message` ThreadItem 渲染管线全部存在。
- 模型 pin 解析 `resolveParticipantModelOverride`（participant_handlers.go:70）可整体搬到 thread runtime 构建处。
- compaction（`internal/compact`）对普通 thread 已生效，常驻 thread 免费继承。

---

## 2. 目标模型

```
Named participant "Noel" (prt-noel)
└── Resident thread（= 它的 DM thread，dm_participant_id=prt-noel，CWD=agent 家目录）
    ├── system prompt：身份 + 规则 + MEMORY.md（每 turn 重建，见 §5）
    ├── History：多轮、持久化、可 compaction —— 这就是它唯一的大脑
    ├── 输入通道：
    │     a) 用户 DM（直接 turn/start，addressed=true）
    │     b) 群聊信封（来自任何它是成员的 thread；@它 → addressed=true）
    │     c) 其他 agent 的 post_message 产生的信封（hop+1）
    └── 输出通道：
          a) post_message（缺省 thread_id = 本 DM）= DM 回复（聊天视图只渲染工具消息；
             assistant 正文是工作过程，不对用户渲染）
          b) post_message(thread_id=群聊) = 群聊发言（署名 participant_message）
          c) decline(thread_id=来源) = 显式不回应（灰字）
          d) 静默结束 turn（仅当本批信封全部 addressed=false 时合法）
```

**群聊 = 现有 conversation thread + 成员表。** 一个 thread 的成员是若干 named participants；用户消息与成员 agent 的发言都会向**其余**成员派发信封。primary agent 行为不变（它仍然直接回复用户的每个 turn，它不是"成员"，不走信封）。

**串行化**：一个大脑 = 一个 thread 的 turn 串行。忙时信封入队（持久化），turn 结束自动排空为下一个合批 turn。这符合直觉：真人同时收到十条消息也是一口气读完再回。

---

## 3. 数据模型（SQLite，`internal/session`）

### 3.1 新表：thread 成员

```sql
CREATE TABLE IF NOT EXISTS thread_members (
  session_id     TEXT NOT NULL,
  participant_id TEXT NOT NULL,
  joined_at      INTEGER NOT NULL,
  PRIMARY KEY (session_id, participant_id)
);
```

- 只收 `kind=named`。ephemeral subagent 永远不是成员。
- 加入途径：用户在 composer @了一个尚非成员的 named agent → 自动 join；roster UI 显式添加/移除。
- DM thread 不写成员表（它由 `dm_participant_id` 定义，成员恒等于该 agent）。

前端 wire 契约（§7.4 的 chips UI 依赖）：

- `Thread` 增加可选字段 `members: ParticipantSummary[]`——成员表的 participant 快照（named only）。无成员的 thread 与 DM thread 该字段缺失或为空，前端不渲染 chips 行。
- 新 RPC `thread/members/remove`，params `{thread_id, participant_id}`，result `{thread}`（成员更新后的完整 Thread，前端按 pin/archive 同款 upsert 回 state）。显式添加走 composer @（T6 自动入群），本期不做单独的 add RPC。注：后端 handler 于 2026-07-04 补齐（此前仅前端接线）。
- `turn/start` 增加可选字段 `mentions: string[]`（participant_id，对应 §4.2 的 `TurnStartParams.Mentions`）。前端从 prompt 文本解析 roster 名字的 `@Name` 全词匹配得到 ID 列表，**仅在非空时附带该字段**——server 端 JSON 解码 DisallowUnknownFields，空 mentions 省略字段保证后端未落地前普通发送不受影响。

### 3.2 新表：持久收件箱

```sql
CREATE TABLE IF NOT EXISTS resident_inbox (
  id             TEXT PRIMARY KEY,          -- "env-" + ulid
  participant_id TEXT NOT NULL,
  envelope_json  TEXT NOT NULL,             -- MessageEnvelope 序列化
  created_at     INTEGER NOT NULL,
  consumed_at    INTEGER                    -- NULL = 未消费
);
CREATE INDEX IF NOT EXISTS idx_resident_inbox_pending
  ON resident_inbox(participant_id, created_at) WHERE consumed_at IS NULL;
```

- 这就是 inbox 数据结构：崩溃/重启后未消费信封仍在，resident thread 恢复时排空。
- 消费 = 被渲染进某个 turn 的 user message 时标记 `consumed_at`（与 turn 落库同事务）。

### 3.3 History 记录扩展

`session_messages` 增加可空列 `envelope_meta TEXT`（JSON）：信封渲染成 user message 落库时，同时存结构化元数据。用途：(a) addressed 未回应的 telemetry 统计（§4.5）；(b) DM UI 把群聊来源的信封渲染为折叠 meta 行而非用户气泡（§7.3）。

**Wire 形状（2026-07-03 修订，对齐后端实现）**：`envelope_meta` 是**数组**，每个元素对应合批进这条 user message 的一个信封：

```json
[{"id": "...", "source_thread_id": "...", "source_thread_title": "...",
  "addressed": true, "hop": 1, "sender_participant_id": "...", "created_at": "..."}]
```

- `source_thread_title`：落库时快照源 thread 标题，前端直接渲染"收到来自「{title}」的消息"，不需要在渲染层反查 thread 列表（源 thread 可能已归档/改名）。**后端 `envelopeMetaRecord` 需补此字段**（`MessageEnvelope.SourceTitle` 已存在，透传即可）。
- 消息条数 = 数组长度（取代早期设计中的 `message_count` 标量字段）。
- ThreadItem 透传：history → protocol 映射时把该 JSON 透传为 ThreadItem 的可选字段 `envelope_meta`（数组），字段缺失时前端按无信封的普通 user message 渲染。

---

## 4. 后端设计（代码级）

### 4.1 信封（新文件 `internal/appserver/envelope.go`）

```go
// MessageEnvelope is the compact unit that enters a resident agent's
// context. One envelope carries exactly one new message — never room
// history. Agents pull surrounding context with fetch_thread_messages.
type MessageEnvelope struct {
    ID                  string    `json:"id"`
    SourceThreadID      string    `json:"source_thread_id"`
    SourceTitle         string    `json:"source_title"`
    SenderKind          string    `json:"sender_kind"` // "user" | "participant"
    SenderName          string    `json:"sender_name"`
    SenderParticipantID string    `json:"sender_participant_id,omitempty"`
    Addressed           bool      `json:"addressed"`   // DM or @mention
    Hop                 int       `json:"hop"`         // 0 = user-originated
    Text                string    `json:"text"`
    CreatedAt           time.Time `json:"created_at"`
    // Workspace snapshots the source thread's stored workspace focus at
    // routing time ("" = all workspaces, "~" = source thread's home,
    // otherwise a registered workspace name). See "2026-07-03 增补三" above.
    Workspace           string    `json:"workspace,omitempty"`
}

// Prompt renders the envelope into the user-role message injected into
// the resident thread. Format is load-bearing: the system prompt (§5)
// teaches the agent to read these attributes. The workspace attribute is
// omitted when Workspace is "" (no focus declared on the source thread);
// "~" (home) renders as workspace="home"; anything else renders verbatim.
func (e MessageEnvelope) Prompt() string {
    attrs := fmt.Sprintf(
        "thread=%q thread_id=%q from=%q sender=%q addressed=%q hop=%q",
        e.SourceTitle, e.SourceThreadID, e.SenderKind, e.SenderName,
        strconv.FormatBool(e.Addressed), strconv.Itoa(e.Hop),
    )
    if ws := envelopeWorkspaceAttr(e.Workspace); ws != "" {
        attrs += fmt.Sprintf(" workspace=%q", ws)
    }
    return fmt.Sprintf(
        "<incoming_message %s>\n%s\n</incoming_message>",
        attrs, strings.TrimSpace(e.Text),
    )
}
```

多信封合批 turn：多个 `Prompt()` 用 `\n\n` 拼接为单条 user message，前缀一行 `You received %d messages while busy:`。

### 4.2 路由器（`internal/appserver/resident_router.go`）

```go
// routeEnvelopes fans a new visible message out to every resident
// member of the source thread except the sender. It never routes to
// the primary agent (primary replies through its own turn) and never
// routes back into the source agent's own inbox.
func (s *Server) routeEnvelopes(source *threadState, env MessageEnvelope, mentioned map[string]bool) {
    members := session.ListThreadMembers(s.rt.SessionDir, source.ID) // named only
    for _, pid := range members {
        if pid == env.SenderParticipantID {
            continue
        }
        e := env
        e.ID = "env-" + session.NewID()
        e.Addressed = mentioned[pid]
        if e.Hop >= maxEnvelopeHop && !e.Addressed {
            continue // hop budget: agent chatter fades out unless directly addressed
        }
        _ = session.EnqueueEnvelope(s.rt.SessionDir, pid, e)
        s.kickResidentAgent(pid) // start or coalesce, §4.3
    }
}

const maxEnvelopeHop = 2
```

**触发点（都在消息成为"可见对话消息"的那一刻）：**

1. `handleTurnStart`（turn_handlers.go）：源 thread 有成员时，用户消息 → `routeEnvelopes(hop=0, sender=user)`。`TurnStartParams` 增加 `Mentions []string`（participant_id，前端 composer 的 @ 补全已能提供）。mentioned 里出现非成员 → 先 `AddThreadMember` 再路由（自动入群）。
2. participant message 落库处（现有 `SubscribeParticipantMessages` 的 appserver 消费者）：agent 发言 → `routeEnvelopes(hop=parent.Hop+1, sender=participant)`。@ 解析：对 text 做 roster 名字的 `@Name` 匹配（大小写不敏感、全词）。
3. 用户 DM：**不走信封**。DM 消息直接就是 resident thread 的 turn（§4.3），天然 addressed。

### 4.3 常驻 turn 机制（`internal/appserver/resident_agent.go`）

```go
// kickResidentAgent drains the pending inbox into one coalesced turn
// on the participant's resident thread. If a turn is already running,
// it does nothing — the turn-completion hook re-invokes kick, which
// picks up everything that queued meanwhile (the subagent.Manager
// Followup pendingMessages pattern, done at thread level).
func (s *Server) kickResidentAgent(participantID string) {
    th, err := s.ensureResidentDMThread(participantID) // find-or-create, reuses handleThreadStart's dm path
    if err != nil { ... notify error ...; return }
    th.mu.Lock()
    if th.running { th.mu.Unlock(); return }
    envs, err := session.DequeueEnvelopes(s.rt.SessionDir, participantID) // marks consumed in same tx as message append
    if len(envs) == 0 { th.mu.Unlock(); return }
    userMsg := coalesceEnvelopes(envs) // §4.1; sets envelope_meta on the record
    th.mu.Unlock()
    s.startResidentTurn(th, userMsg, envs) // normal turn machinery, participant system prompt
}
```

关键点：

- `ensureResidentDMThread`：按 `dm_participant_id` 查已有 DM thread；没有则用 thread_handlers.go:56-119 的现有 DM 创建路径静默创建（不要求用户先打开过 DM）。
- `startResidentTurn` 复用 `handleTurnStart` 的内核（抽出共用函数，禁止复制粘贴一份平行实现）。差异只有两点：(a) system prompt 用 §5 的常驻模板；(b) turn 完成钩子追加：addressed 回应情况的 telemetry 记录（§4.5）+ 再次 `kickResidentAgent`（排空积压）。
- **模型 pin**：`ensureThreadRuntime` 处，若 thread 是 resident（`DMParticipantID != ""`），调用现有 `resolveParticipantModelOverride` 把 pin 的 model/client 装进该 runtime。`participant/start` 里的这段逻辑搬走后删除。
- DM 用户消息路径：`handleTurnStart` 检测 `th.DMParticipantID != ""` 时同样走 `startResidentTurn`（addressed 语义由"这是 DM"隐含，无需信封包装，用户消息原样进 history——保持 DM 阅读体验干净）。

### 4.4 发言路由重构（工具层）

现状 `post_message`/`decline` 通过 `AgentControl`（subagent 语境）。常驻 turn 是 thread runtime，不是 subagent。重构：

```go
// internal/tools/env.go — 新增接口，替换对 AgentControl 的直接依赖
type ParticipantSpeech interface {
    // PostMessage publishes a signed participant message into targetThreadID.
    // The speaker must be a member of (or the dm-participant bound to) it.
    PostMessage(ctx context.Context, kind, text, targetThreadID string) (PostedMessage, error)
    Decline(ctx context.Context, reason, targetThreadID string) error
}
```

- appserver 为每个 resident runtime 注入实现（闭包携带 participantID）；旧 spawn 路径（过渡期）用 AgentControl 适配同一接口。
- `post_message` 的 `thread_id` 语义升级：**任意它是成员的 thread 或它自己的 DM thread**。缺省（不带 thread_id）= 发到自己的 DM thread——这是 DM 回复的标准通道（`2026-07-03-chat-style-threads-design.md` §2）。校验成员资格，非成员报错（错误信息指引它先说明理由请用户拉群）。
- `decline` 增加可选 `thread_id`（缺省 = 本批唯一 addressed 来源；多来源时必填）。
- 速率限制沿用 `AgentControl` 现有参数，搬到实现内。

### 4.5 必答的落实方式（提示词约束 + 可观测，runtime 不代答）

必答是提示词层的强约束（§5 硬规则 1），**runtime 不注入 synthetic decline、不伪造 agent 没有说过的话**。理由：代答消息本质是系统冒充 agent 发言，污染群聊记录，且掩盖而非暴露 agent 的质量问题。

runtime 只做可观测。resident turn 完成钩子：

```go
// Observability for axiom 6a: an addressed envelope should be answered.
// For each addressed envelope in this turn's batch, check whether the
// agent posted to that source thread or declined it. If not, record a
// telemetry event — do NOT synthesize any message on the agent's behalf.
for _, env := range batch {
    if !env.Addressed { continue }
    if turnPostedTo(env.SourceThreadID) || turnDeclined(env.SourceThreadID) { continue }
    s.recordUnansweredAddressed(env) // internal telemetry only, not user-facing
}
```

DM 直连消息：回复 = 对本 DM 的 `post_message`；本批含 DM 消息而 turn 内既无对本 DM 的 `post_message` 也无 `decline` 时，同样只记 telemetry 事件。这是**内部可观测性**，用于我们调优提示词和路由策略（比如发现某个模型经常漏答就调整 §5 措辞或模型选择），**不做成用户可见的指标**——用户不在意未回应率，不进 profile 面板、不进 track record。

### 4.6 上下文拉取工具（conversation-native bundle 新成员）

```go
// fetch_thread_messages(thread_id, limit<=30)
// Read-only. Returns the last N visible conversation messages (user /
// agent / participant posts; no tool calls, no reasoning) of a thread
// the caller is a member of. This is the Raft "queryable, not pushed"
// principle: envelopes stay thin, agents pull context on demand.
```

实现读 `session_messages` 可见类型，按 §3.3 的展示分层过滤。返回紧凑文本（`[sender] text`，每条截断 500 字符）。

### 4.7 会话搜索范围扩展（`internal/appserver/search.go`）

现状缺口：`threadSearchCandidates` 跳过 `msg.Hidden` 的消息（search.go:181），而群聊中 participant 发言落库后经 `participantModelContextMessage` 转换为 `Hidden: true` 的 model-context 消息（history.go:528）。结果：**群聊里 agent 发的所有内容、现状 DM 里 agent 的所有回复，搜索都命中不了**。DM thread 本身已在搜索源里（search.go:119 `DMParticipantID != ""` 分支 + `ListForCWDWithDMs`），不需要动。

改法：

1. **群聊 agent 发言可搜**：`threadSearchCandidates` 对 participant 消息（`msg.Name == participantModelContextMessageName` 或 `msg.ParticipantID != ""`）不再因 Hidden 跳过，改为把 `msg.DisplayContent`（原始发言正文，无 `<participant_message>` 包裹）和 `msg.ParticipantName` 加入候选。其余 hidden 消息（compaction 摘要等）照旧跳过——只对 participant 消息开例外，不是取消 Hidden 过滤。
2. **DM 多轮化后自动可搜**：T3/T4 落地后 agent 在 DM 里的回复就是普通 assistant 消息，无需额外处理。
3. **去重**：DM thread 里由信封渲染的 user message（`envelope_meta` 非空，§3.3）在搜索候选中跳过——同一条群聊消息的正身在群 thread 里已可命中，DM 侧的信封副本再命中只会产生重复结果。
4. 快照/摘录/排序逻辑（`threadSearchExcerpt`、`sortThreadSearchResults`）不动。

### 4.8 群管理工具与文件范围（2026-07-03 增补，契约在另文）

`create_group` / `add_group_member`（conversation-native bundle，所有 resident）与文件工具的工作区白名单校验（家目录 + 工作区 + temp，读写同权，仅装配 resident turn 与任务 run），完整契约、频率预算与分任务见 `2026-07-03-sidebar-groups-andy-workspaces.md` §4-§5。默认组队 agent Andy（普通 resident，首启预置、零特权）见该文档 §3。

### 4.9 删除与迁移

- **删除**：`sendPromptToParticipant` 的 spawn 语义（前端，§7.1）；`participant/start` 中 DM 逐条孵化的调用方。`participant/start` RPC 本身保留给"显式派发一个任务 run"场景（roster 的"派任务"入口），但 prompt 组装改为只含任务本身（身份已在常驻 session 里的，不再需要每次注入）。
- **迁移**：已有 DM thread 无需数据迁移（history 本来就在，只是从没被使用——现在直接成为大脑的既有记忆，这正是公理 1 想要的）。旧的孤儿 DM run 无需处理。
- `manage_participant`、roster、avatar、model pin 的存量功能不动。

---

## 5. 常驻 system prompt（全文，实施时逐字落地）

**修订说明（2026-07-04）**：本节的记忆相关表述（"Keep durable notes in MEMORY.md"、`## Memory` 注入段等）已由 `2026-07-04-memory-redesign.md` §5 修订，以该文档为准；本节其余部分继续有效。

放 `internal/appserver/participant_prompt.go`，函数 `residentParticipantSystemPrompt(p participant.Participant, memory string) string`。每 turn 重建（memory 更新次 turn 生效）。**这份提示词是公理 1-8 的对 agent 表达，修改措辞需回到本文档同步。**

**实施留意——缓存纪律（2026-07-03）**：MEMORY.md 内嵌于 system prompt 且每 turn 重建，意味着每次 memory 写入都会使 resident thread 的整个缓存前缀失效，长历史按全价重读。实施时二选一：(a) 约束 memory 写入的生效时机（批量/低频，接受变更时一次性重建）；(b) 把 MEMORY.md 移出 system prompt，作为上下文靠后的注入块或由 agent 工具自拉。另注意低频群的唤醒间隔常大于缓存 TTL，缓存并非总能兜底——公理 4 的成本可持续性依赖这条纪律。

```
You are {{Name}}, a resident named agent in this workspace. You are a
continuous identity: one brain, one ongoing session. Direct messages
from the user and group-conversation messages all arrive here, in this
same context. You are not a fresh instance per message — your history,
your memory file, and your judgment persist across days and tasks.

Your role: {{Role}}. How teammates describe you: {{Tagline}}.
Your home directory is your workspace. Keep durable notes in MEMORY.md.

## How messages reach you
Group messages appear as <incoming_message> blocks. Attributes tell you
the source thread, the sender (the user or another agent), whether you
were directly addressed (addressed="true" means a DM or an @mention),
and a hop count (how many agent-to-agent relays preceded it). Several
blocks may arrive in one batch if they came in while you were working —
read the whole batch before responding to any of it.
Messages in this conversation without an <incoming_message> wrapper are
the user speaking to you directly (DM). DMs are always addressed to you.

## Whether and where to reply — your judgment, plus two hard rules
1. Addressed messages (DM or @mention) MUST be answered: either reply
   with substance, or call decline with a one-line reason. Never end a
   turn silently on an addressed message.
2. Unaddressed group messages: reply ONLY when you add real value —
   a correction, a blocker you noticed, information others lack.
   Silence is a valid outcome; simply do not post. Never acknowledge,
   echo, or "+1".

## How to reply
- To a DM: call post_message (omit thread_id — it defaults to this DM).
  Plain assistant text is your private working transcript; the chat view
  renders only tool-posted messages, so text outside post_message never
  reaches the user.
- To a group thread: call post_message with thread_id set to the source
  thread. Keep group replies short and substantive.
- One event may deserve replies in different places. If the same
  question reached you via DM and a group thread, answer once in the
  group and point the DM there — do not duplicate content.
- Replying to another agent's message: only when addressed, or when you
  have a material correction. Never reply to a reply just to close a
  loop — no ping-pong, no thanks-exchanges.
- If you genuinely need another agent to respond — especially in a group
  thread — @mention them by name (e.g. @Reviewer). Un-mentioned messages
  are treated as ambient information: other agents may read them and stay
  silent, and relayed agent-to-agent messages only reach agents who are
  explicitly @mentioned. Do not @mention someone just to be polite; an
  @mention is a request for their time.

## Wrapping up discussions (only when asked)
- When the user asks you to wrap up or synthesize a discussion, post to
  the source thread with exactly three parts: Conclusion — the decision
  as it stands; Open disagreements — unresolved positions, attributed by
  name, never smoothed over; Suggested next step.
- Never post an unprompted summary: it repeats what others said, which
  the rule against echoing forbids.
- When a discussion you are a member of reaches a decision — whether or
  not you wrote the summary — record the decision and its reasons in
  your MEMORY.md.

## Building teams and groups
You can create group threads (create_group) and add named teammates to
groups you belong to (add_group_member). Create a group only for an
ongoing purpose — a project, a standing topic — never for a one-off
question; prefer reusing an existing group. When the user asks for a
team, you may also create new named teammates with manage_participant.

## Workspaces and file scope
The user's registered workspaces (name — root path):
{{每行 "- {{Name}} — {{Root}}"；清单为空时写 "(none yet)"}}
Your home directory is where you live; workspaces are where you work.
You may read and edit files only inside your home directory and these
workspace roots — the file tools enforce this. Everything else on this
machine is out of bounds; do not try to route around the limit via
bash. If a task needs a directory outside this list, say so and ask
the user to add it as a workspace.

## Context discipline
- Each envelope carries one message, not room history. When you need
  surrounding context from a group thread, call fetch_thread_messages
  instead of guessing.
- Your context may be compacted over time. Anything worth keeping —
  decisions, user preferences, recurring mistakes — belongs in
  MEMORY.md, which survives compaction and resets.

## Memory
{{MEMORY.md 内容；为空则省略本节}}
```

`participant/start` 的任务派发 prompt（保留路径）在此之上仅追加 `## Request` 段，不再重复身份/规则。

### 5.1 收敛与落档（v1 提示词方案，发起权归属）

群聊的目的层定义是**收敛观点**——多 agent 是认知聚合的手段（单用户场景下没有利益协商，纯粹是陪审团式聚合，用户是唯一拍板人）。v1 不建任何新机制，全部落在 §5 提示词：

- **技能普适**：所有 resident 都会三段式收敛（结论 / 保留的分歧 / 建议）和"定案写入 MEMORY.md"（各写各的文件，天然无竞争）。
- **发起权单数、归用户**：用户 @ 任一成员"收敛一下"即触发（addressed → 硬规则 1 必答）。**禁止主动总结**：主动总结无信息增量，与硬规则 2 直接冲突；且全员持有发起权会导致过度触发（模型的总结癖）、竞速/旁观者效应、以及参与辩论者总结自己辩论的利益冲突。
- **最佳实践**：优先 @ 未参与争论的成员当综合者——沉默的成员最中立。
- **落档去处**：收敛输出留在群 thread；各成员把定案写入自己的 MEMORY.md。这同时是刷新"名字缓存"的时机（《Agents Need Names》的 staleness 问题：名字是会过期的缓存，落档即刷新）。群内置顶/决议区为后续产品化方向，本期不做。
- **观点独立性是被守护的特性**：瘦信封（公理 3）让成员默认看不到彼此的即时发言、除非主动 fetch——这结构性削弱了信息级联（先发言者带偏后续），是收敛质量的隐性支柱，实施与后续演进不得破坏。

**v2 占位**：群级 facilitator 角色 = 把主动收敛的发起权下放给一个明确角色，其提示词必须包含对硬规则 2 的显式豁免，否则规则打架。见 §10。

---

## 6. 权限与约束矩阵（增补 2026-07-02 文档 §4.3）

| 能力 | resident named（常驻 turn） | 任务 run（participant/start 派发） | 普通 subagent |
|---|---|---|---|
| DM 发言（`post_message` 缺省目标 = 本 DM；assistant 正文不渲染） | ✅ | ❌ | ❌ |
| `post_message` 任意成员 thread | ✅ | ⚙️ 仅 ParentID thread（现状） | ❌ |
| `decline(thread_id)` | ✅ | ✅ | ❌ |
| `fetch_thread_messages` | ✅ 成员 thread | ❌ | ❌ |
| `create_group` / `add_group_member` | ✅ | ❌ | ❌ |
| 文件工具范围白名单（家目录 + 工作区 + temp，读写同权，工具层强制） | ✅ 适用 | ✅ 适用 | —（沿用宿主 session 规则） |
| `manage_participant` | ✅ | ✅（现状） | ❌ |
| 收信封 | ✅ | ❌ | ❌ |
| 必答约束 | ✅ 提示词强约束 + 未回应 telemetry | ✅（现状 post/decline 约束） | — |

频率与预算（runtime 强制，不依赖提示词）：

- `post_message` 沿用现有速率限制；每 turn 对同一 thread 最多 2 条。
- hop ≥ 2 的信封只投递给被 @ 的成员（§4.2），agent 链最长 用户→A→B→(仅点名)。
- 单批合并信封上限 20 条，超出留在 inbox 下一批（防单 turn 上下文爆炸）。
- 群成员数上限暂定 8（超过时 UI 拒绝添加并解释）。

---

## 7. 前端设计

### 7.1 DM 发送路径替换（App.tsx）

删除 `sendPromptToParticipant` 分支（App.tsx:5748-5771 的 DM 特判）。DM thread 的发送走与普通 thread 完全相同的 `turn/start` 路径（含排队、附件——附件限制随之消失）。忙时排队复用现有 queued composer message 机制。

### 7.2 状态点与未读重接（AppSidebar）

- busy：现在挂在 participant run 事件上 → 改为 resident thread 的 running 状态（现有 thread running 通知已具备）。
- DM 未读：resident thread 新 participant_message（post_message 产物）且非当前视图（现有未读机制直接适用）。
- 群聊有它的发言 → 群 thread 的现有 unread 逻辑，不需要 per-agent 新机制。

### 7.3 DM 视图中的信封渲染

带 `envelope_meta` 的 user message 在 DM 视图渲染为**折叠 meta 行**（meta 层、`--ink-muted`）："收到来自「{{源 thread 标题}}」的 {{n}} 条消息"，点击展开原文。不渲染为用户气泡（那会看起来像用户本人说的话）。群聊视图不受影响（信封不出现在群聊里）。

### 7.4 成员 UI

- 群 thread 头部：成员 ParticipantChip 行内列表 + 移除（×，hover 显示）。
- composer @ 补全（已存在）选中非成员 → 发送时自动入群，发送后 chips 行出现该成员。
- roster 侧栏行的 DM 入口不变。

规范：全部遵守 2026-07-02 文档 §7（token、无新依赖、复用会话壳、红线清单）。

---

## 8. 实施红线（对实施 agent 的强约束）

1. **禁止复制粘贴 turn 机制。** `startResidentTurn` 必须与 `handleTurnStart` 共享抽出的内核函数。出现第二份 turn 循环即返工。
2. **禁止把房间历史塞进信封。** 信封只含单条新消息。发现自己在信封里拼接历史，回去看公理 3。
3. **runtime 禁止代替 agent 发言。** 必答走提示词约束（§5 硬规则 1）；runtime 只记录"addressed 未回应"telemetry（§4.5），不注入任何 synthetic 消息。发现自己在写 postSyntheticDecline 之类的代答逻辑即返工。
4. **不修改 primary agent 的任何行为。** 它不收信封、不进成员表、正文照旧直出。
5. **不动 mailbox/普通 subagent 通道。** ephemeral worker 的产出继续走 mailbox。
6. **提示词全文以 §5 为准**，实施时不缩写、不"优化"措辞；确需改动，先改本文档再改代码。
7. **每步 TDD + 原子提交**（用户全局规约）；UI 文案中文、代码注释与 commit message 英文。
8. **不新增依赖；样式全走 token**；违反 2026-07-02 文档 §7.7 红线即返工。
9. **根因优先**：任何测试不过，先怀疑实现，禁止改测试/fixture 迁就实现。
10. 用户工作树可能存在未提交 WIP 文件，提交前用 patch 分离法只提交自己的改动，绝不提交他人 hunks。

---

## 9. 分任务拆解（每个可独立提交，顺序即依赖）

| # | 任务 | 触及 | 验收 |
|---|---|---|---|
| T1 | schema：`thread_members` + `resident_inbox` + `envelope_meta` 列 + store CRUD（`internal/session`） | session 包 + 迁移 | store 单测：入队/按序出队/consumed 幂等/成员增删 |
| T2 | 信封类型 + `Prompt()` 渲染 + 合批（`envelope.go`） | appserver | 渲染格式单测（属性转义、多信封合批头） |
| T3 | resident turn 内核：`ensureResidentDMThread` + `startResidentTurn`（抽 `handleTurnStart` 内核）+ 常驻 system prompt（§5 全文）+ 模型 pin 迁移 | appserver、turn_handlers 重构 | DM 两条消息后第二 turn 的 provider request 含第一轮完整对话；pin 生效 |
| T4 | DM 前端切换：删 spawn 分支、走 turn/start；busy/未读重接 | App.tsx、AppSidebar | DM 多轮可用、附件可发、状态点正确 |
| T5 | 发言路由重构：`ParticipantSpeech` 接口 + thread_id 任意成员目标 + `decline(thread_id)` | tools、agentcontrol、appserver | resident turn 内 post_message 到群 thread 产生署名卡 |
| T6 | 群聊路由：成员表接入 `handleTurnStart`（Mentions 参数、自动入群）+ `routeEnvelopes` + `kickResidentAgent` + 合批排空 | appserver、protocol、前端 mentions 传参 | 群发一条消息，全部成员各起一次推理；@者 addressed=true |
| T7 | agent↔agent：participant message 落库处路由 + hop 预算 + @解析 | appserver | A @B 后 B 收 addressed 信封；B 回帖不再唤醒 A（hop=2 未点名） |
| T8 | 未回应 telemetry 记录 + 单批/频率预算 | appserver | @后静默 → telemetry 事件落库，群聊/DM 无任何代答消息 |
| T9 | `fetch_thread_messages` 工具 | tools、appserver | 成员可拉、非成员报错、截断生效 |
| T10 | DM 信封折叠渲染 + 成员 chips UI | 前端 | 视觉过 2026-07-02 §7.8 自查 |
| T11 | e2e：DM+群聊同时发问 → agent 群内作答、DM 指路（人工验证脚本 + 集成测试） | 全链路 | 公理 2/5/6 的行为验收 |
| T12 | 搜索范围扩展（§4.7）：participant 发言入候选 + 信封副本去重 | appserver search.go | 搜群聊中 agent 发言的关键词可命中该群 thread；同一关键词不因 DM 信封副本重复命中 |

---

## 10. 刻意不做（本期）

- Raft 的 held draft / freshness check：推迟。理由修正（2026-07-03）：串行的只是单个 agent 自己的 turn，房间在其分钟级长 turn 期间照样移动，"回复已过时的消息"场景**存在**；推迟是因为单用户下频率未知，先观察。低成本中间态备选：turn 结束要发言前，若收件箱已有同源 thread 的新信封，先注入本 turn 再发言。
- 未点名信封的低挡位推理（effort tiering：按信封类别降 effort / 换挡）：搁置。默认用户使用前沿模型，先靠 §5 的缓存纪律控制成本，观察实际账单后再评估。
- 群级 facilitator 角色（主动收敛的发起权 + 对硬规则 2 的显式豁免）：v2 占位，见 §5.1。v1 收敛只由用户 @ 触发。
- agent 私聊 agent 的独立 DM 通道：v1 一律经群 thread（信封已覆盖 agent↔agent），有真实需求再加。
- 信封的优先级/打断：忙时一律排队，不打断当前 turn。
- 成员的自动退群/静音策略；track record 驱动的路由。
- 跨 workspace 的 agent 共享。
