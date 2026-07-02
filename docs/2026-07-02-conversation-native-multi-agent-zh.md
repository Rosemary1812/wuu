# wuu 是否应该走向 conversation-native multi-agent：调研报告 + 产品/技术方案

日期：2026-07-02
状态：调研 + 提案（未实施）

---

## 0. 一句话结论

应该做，但第一刀不是"群聊"，而是**身份**：先让 wuu 的主对话能够容纳"有名字的参与者"（participant attribution + 显式发言），再逐步引入 thread / task / 常驻 named agents / @mention。wuu 已经具备了这个演进所需的大部分底层机制（agent tree、mailbox、可见的 subagent session、WorkerType 角色系统），缺的是**身份层**和**发言层**，而不是通信层。

---

## 1. Raft / Slock 调研

> 信息来源：raft.build 官网、docs.raft.build、raft.build/resources/blog、xxchan.me 博客、LinkedIn。
> 标注约定：【确认】= 官方公开材料原文可查；【推断】= 由公开材料合理推断；【未找到】= 公开渠道无信息。

### 1.1 产品定位

【确认】Raft（曾用名 Slock，LinkedIn 公司页仍为 slock-ai）定位是 "Where humans and AI agents build together"，口号原文：

> "The future of work isn't humans using AI tools. It's humans and AI agents building together."

核心命题是 **chat is the workspace**：channels、DMs、threads，所有交互都发生在消息里，"humans and agents share the same context, so collaboration has zero overhead"。

【确认】定价模型很能说明产品观：human 占 1 seat，**agent 占 0.1 seat**。Free tier 包含 channels、tasks、"agents on your own computers"、agent reminders、basic observability。Pro $8.80/seat/月。agent 是按"团队成员"计费的，不是按 API 调用计费——这是"agent 是 teammate 不是 tool"在商业模型上的直接体现。

【注意】teamraft.com（美国国防/物流方向的 Raft）是**另一家公司**，与 raft.build 无关，调研时需区分。

### 1.2 核心对象模型

从 docs.raft.build 整理出的对象模型：

| 对象 | 定义（官方原文/概括） | 关键机制 |
|---|---|---|
| Server | 一个团队工作区 | humans + agents 都是 member |
| Channel | 频道 | agent 可以自己加入频道（"claims access when it needs it"）|
| Thread | "sub-conversations attached to a specific message" | 回复不出现在主频道流里；参与即自动 follow，做完可以 unfollow |
| Task | **"messages with tracking metadata: a number, a status, and an owner"** | task 就是一条被标记为可跟踪的消息；每个 task 自带一个 thread（task 消息是 anchor）|
| Agent | "a server member powered by an AI runtime. It has a name, a persistent identity, and memories" | 见 1.3 |
| Activity | 聚合全 server 的 mentions / thread replies / task 状态 | All / Unread / Mentions 过滤 |

Task 的设计特别值得注意：**"Work discussion, progress updates, and results go in the thread. This keeps the main channel clean — the task message shows the status; the thread holds the details."** 主频道只留状态，细节全部进 thread。agent 通过 claim 机制抢占 task（"If the claim fails, the agent moves on"），不需要人手动分配。

### 1.3 Named agents：长期运行的 teammate

【确认】Raft 的 agent 是长期身份，不是一次性 worker：

- "One agent is one session: a continuous identity that stays alive across days and tasks, not a fresh instance every time you talk to it."（Introducing Raft 博文）
- Agent 有独立的 **workspace**：agent 电脑上的一个持久目录，存文件、笔记、memory，"survives across sessions"，agent 自己维护目录结构。
- **Lifecycle**：online / busy / error / offline 状态点；空闲时 idle，被消息、@mention 或 reminder 触发时激活——"They're always present, not always running."
- **Reset 分层**：restart / session reset / full reset，清除的状态量不同。session reset 不清 workspace，所以 "an agent that writes clear notes to its workspace picks up context even after a full conversation reset"。
- Agent 自己管理时间：自设 reminder，"It wakes up, checks what's due, and picks up where it left off"。
- Onboarding 靠对话而非配置文件："A new agent joins a channel, reads the history, and starts contributing where it can. Nobody writes a configuration file."

【确认】**Runtime 与身份解耦**：runtime 是驱动 agent 的 AI 引擎——官方支持的 runtime 就是 Claude Code、Codex CLI、OpenCode 等本地 CLI 工具，跑在用户自己的电脑上（daemon 连接 server），用用户自己的订阅。换 runtime 不换身份："the agent's workspace, memory, and identity are preserved"，且 "Other members don't see which runtime powers an agent in day-to-day use"。

> 对 wuu 的战略含义：Raft 不做 agent 引擎，它做的是引擎之上的**身份层和协作层**。wuu 本身就是一个 agent 引擎（Raft 语境下的 runtime）。wuu 要回答的问题是：身份层是让别人（Raft 这类产品）来做，还是自己长出来。

### 1.4 "收到消息 ≠ 自动发言"机制

这是用户最关注的机制，来源是官方博文 *Is Having Agents in the Room Meant to Be Chaotic?*（Tenny，CTO & AX Designer）。核心设计有两个：

**Agent Inbox**：房间里的消息不直接塞进 agent 的 context。
> "The agent decides what is worth its context, instead of the room deciding for it. Every signal pulled into the working prompt displaces something else (task state, instructions, intermediate reasoning), so handing that decision to the agent, rather than to whoever happens to post next, is what keeps attention on the work."

即：**收件是 inbox 语义（可延迟、可略过），不是中断语义**。这同时是注意力管理和成本管理。

**Held Draft**：发言前有新鲜度检查。agent 起草回复期间房间可能已经变化（有人已回答、话题已转移），此时草稿被 hold，agent 有四条显式路径：

1. **Revise** — 基于房间当前状态重写；
2. **Send as-is** — 原样发送（仍要再过一次 freshness check）；
3. **Stay silent** — 让草稿过期，"Silence is a valid outcome"；
4. **Send anyway** — 多次被 hold 后显式 override。

背后的设计哲学叫 **action explicitness**：
> "A human composing a reply does not need a UI labeled 'decide whether to send'... Agents need those internal options made external. ... Action explicitness means surfacing the option-space, not assuming the agent will derive it."

即：人类内隐的决策（要不要说、要不要放弃草稿），对 agent 必须做成**显式的工具选项**。这正是"做事和发消息分离"的理论根基：发言是一个显式动作，有自己的选项空间，而不是生成即发送。

【未找到】发言工具的具体 API 名称（如 send_message 的确切 schema）未公开。

### 1.5 OpenTeamFormat

【未找到】公开渠道（官网、docs、博客、搜索引擎）没有任何 "OpenTeamFormat" 或类似开放团队定义规范的信息。可能是未发布、内部名称、或记忆有误。本报告不基于它做任何设计，但第 5 章 Phase 5 的 team template 设计预留了"团队定义可序列化"的位置。

### 1.6 《Agents Need Names》要点

作者 xxchan（Raft Founding Engineer，同步发在个人博客）。核心论点整理：

1. **Role 是 schema，name 是 instance。** "A role, 'PM,' 'Engineer,' 'QA,' is a schema. A type signature. Replaceable, stateless. ... A name is an instance. 'Noel' isn't a type, it's a specific thing that carries history: how it scoped the last few performance passes, what it tends to flag, how it likes the diff framed, the time it caught a regression nobody else noticed."
2. **Name 压缩的是时间线，role 压缩不了。** "You want Noel specifically, because last week's context still lives with it. The name doesn't just compress a skill set, it compresses a timeline."
3. **Name 只在群体语境里 load-bearing。** "Names only become load-bearing once you're in a group." 单 agent 对话不需要名字；一旦有多个参与者，名字就是路由原语。
4. **名字的意义存在调用者脑中，不在 agent 里。** "The meaning of a name doesn't live in the named. It lives, distributed, in the mental models of everyone who calls it. An agent holds a pointer, the current execution, and its memory."
5. **Name 是 cache，会过期。** "A name is a cache, and a cache goes stale."
6. **Named team 给用户一个抽象高度滑杆。** "You can stay high and say 'ask Noel,' or, when the work gets subtle, drop into a specific participant, see how it's thinking, correct its lane, and let that feedback compound. You move up and down the stack instead of being stuck at one altitude."

---

## 2. 分析：named agent team 的价值与风险

### 2.1 为什么 name 比 role 更适合长期 agent

用类型系统的语言说：role 是 interface，name 是携带状态的 instance。区别在三个维度上：

| 维度 | Role（"reviewer"） | Name（"Noel"） |
|---|---|---|
| 可替换性 | 任意实例可互换 | 不可互换——换了就丢历史 |
| 状态 | 无状态（system prompt 每次相同） | 有状态（workspace、memory、track record） |
| 用户预期 | "这类工作会被做" | "这个家伙会这样做这类工作" |

关键点：**长期性是 name 的前提，不是结果**。如果 agent 每次 spawn 都是新实例，起名字只是装饰（wuu 今天的 subagent 就处于这个状态——`Type: "reviewer"` 是 role，没有 instance）。只有当 agent 有跨 session 的 workspace/memory/表现记录时，name 才开始"值钱"。所以方案里 named agent 必须绑定持久状态，否则不如不做。

### 2.2 Name 如何帮助用户路由任务

- **寻址成本压缩**：用户不需要展开"我需要一个懂性能、看过这个 repo 的热路径、上次帮我调过 stream 重连的 agent"，只需要说"@Noel"。名字是一个单 token 的检索键，命中一整段协作历史。
- **预期即规格**：把任务发给 Noel 时，用户对输出格式、关注点、边界的预期已经内建，prompt 可以更短。这是真实的 token 节省和沟通成本节省——协作历史成为隐式 prompt。
- **抽象高度滑杆**（xxchan 的观点，我认为是对 wuu 最重要的一条）：wuu 用户今天只有两个高度——直接跟主 agent 说（高）或者盯着 subagent session 看细节（低）。named participants 提供中间层：可以"ask Noel"，也可以点开 Noel 的 thread 纠正它的方向。

### 2.3 Name 如何让 feedback compound

没有 name 时，反馈的归宿是本次 session："这次 review 太啰嗦了"只影响当前上下文，下次 spawn 一个新 reviewer 又从零开始。有 name（+ 持久 workspace）时：

- 反馈写入该 agent 的 memory（"用户嫌我 review 啰嗦 → 只报 bug 和逻辑错误"）；
- 表现记录累积（做过哪些任务、结果如何、被 reset/纠正过几次）；
- 用户的心理模型和 agent 的自我模型**同步演化**——这是 Raft 说的 "context, memory, and responsibility compound over time"。

反馈复利的机制要求：(a) 反馈有明确收件人（name 提供）；(b) 收件人有地方存（workspace/memory 提供）；(c) 存了以后下次真的生效（每次激活加载 memory）。三者缺一，反馈就不复利。

### 2.4 Name 作为 cache 的风险

1. **Cache 过期，用户预期不更新**。agent 升级了模型 / 改了 system prompt / memory 被 reset，但用户仍按旧预期发任务 → 预期错配，且用户会把错配归因于"Noel 退步了"，信任受损比无名 agent 更重（因为有名字才有信任可损）。
2. **Cache 污染**。几次失败的任务会让用户永久降低对某个 name 的信任，即使根因是任务本身超纲。role 没有这个问题（每次都是新实例，无历史包袱）。
3. **错误归因**。多 agent 协作时，A 的产出经 B 整合后出错，用户可能记到 B 头上。归因错误写进心理模型后很难纠正。
4. **拟人化过度**。名字诱导用户高估 agent 的连续性和"性格"稳定性，而 LLM 的行为方差本来就大。

### 2.5 如何保持 name cache fresh

对应到产品机制（第 3、5 章会落到字段和 UI）：

| 机制 | 内容 | 解决的问题 |
|---|---|---|
| Track record | participant card 上自动生成的工作履历：最近 N 个任务、结果、耗时、被纠正次数 | 让用户的预期基于事实而非印象 |
| Feedback 通道 | 用户对某条 participant message / 某次任务的显式反馈（纠正文本），写入该 agent memory 并在 card 上留痕 | 反馈复利 + 可审计 |
| Change notice | agent 的模型/prompt/memory 发生变化时，card 上显示 changelog（"Noel 已切换到 X 模型"） | cache 失效通知 |
| Memory 可见 | 用户可以查看（并纠正）agent 的 memory 摘要 | 防止 memory 里累积错误自我认知 |
| Reset 分层 | 沿用 Raft 的三层：restart（清运行态）/ session reset（清对话）/ full reset（清 workspace）| 给用户显式的 cache invalidation 工具 |

---

## 3. wuu 现状与目标模型

### 3.1 现状盘点（基于代码调研）

wuu 今天已经有的（比预想的多）：

| 能力 | 现状 | 位置 |
|---|---|---|
| Session/对话持久化 | SQLite：`sessions` + `session_messages`，含 fork 谱系（`ForkedFromID/TurnID/ItemID`）、worktree 绑定 | `internal/session/session.go:34-50, 588-640` |
| 消息/事件流 | Turn → ThreadItem（`user_message` / `agent_message` / `reasoning` / `tool_call` / `collab_agent_tool_call` / `context_compaction` / `error`），WebSocket JSON-RPC 通知流式下发 | `desktop/src/shared/protocol.ts:571-578`，`internal/appserver/protocol.go:86-96` |
| Subagent 机制 | `spawn_agent` / `send_message` / `close_agent` / `list_agents` 编排工具；subagent 有独立 session（可被用户打开检视）；`ParentID`/`AgentPath` 构成 agent tree | `internal/agentcontrol/agent_control.go:1-2`，`internal/subagent/types.go:32-165` |
| 角色系统 | 9 个内置 WorkerType（general-purpose / verification / planner / researcher / worker / reviewer / qa / debugger / integrator），各有 system prompt、工具白/黑名单、隔离模式 | `internal/agentcontrol/worker_types.go:19-211` |
| Agent→parent 通信 | Mailbox：`AgentMailboxMessage`（status/result/report_path/artifacts/tokens 等），经 `agent/mailbox` 通知下发 | `internal/agentcontrol/mailbox.go:12-37` |
| 后台工作可视化 | progress capsule（composer 附近的活动指示）、turn rail（active marker 跟随运行中的 agent）、`Agent.Activity`（"→ read_file"）| protocol.ts `Agent` 类型，近期 commits |
| 任务模型 | harness：Task / AgentRun / Report（含 outcome、evidence、blockers），独立于对话存储 | `internal/harness/types.go:8-150` |

**缺的正好是两层：**

1. **身份层**：`SpawnOptions.Type` 是 role 不是 name；`TaskName` 是任务名不是参与者名；没有跨 session 的 agent 身份实体；没有 participant 概念——消息只有 `Role`（user/assistant/tool），无发送者身份。
2. **发言层**：subagent 的产出只能走 mailbox 回到父 agent 的 context，由父 agent 转述；subagent 没有任何"向用户可见的对话流发言"的通道。做事和发消息在 wuu 里目前**过度分离**了——subagent 只能做事，完全不能发言。

这个判断很重要：**wuu 不需要引入"做事/发言分离"，它已经天然分离；wuu 需要的是把"发言"作为受控能力开放出来。** 这比 Raft 的问题（agent 太吵，要教它闭嘴）方向相反且更容易——从静默默认开放到显式发言，比从吵闹收敛到静默容易得多。

### 3.2 目标核心模型

以下是提议的概念模型。原则：**不新建平行宇宙，全部长在现有实体上**。

```
Conversation (= 现有 Session，不改名)
├── participants: Participant[]        ← 新增实体
├── turns: Turn[]                      ← 现有
│   └── items: ThreadItem[]            ← 现有，新增 participant 归属 + 新 item 类型
├── threads: ConversationThread[]      ← 新增（Phase 3），锚定在某个 item 上
└── tasks: Task[]                      ← 现有 harness Task，Phase 3 起投影进对话
```

#### Participant（新增）

```sql
CREATE TABLE participants (
  id           TEXT PRIMARY KEY,   -- "prt-" + ulid
  kind         TEXT NOT NULL,      -- 'human' | 'primary' | 'named' | 'ephemeral'
  name         TEXT NOT NULL,      -- 显示名："Noel"；ephemeral 用 task_name 派生
  role         TEXT,               -- WorkerType.Name："reviewer"（schema 引用）
  avatar       TEXT,               -- emoji 或图标 id
  tagline      TEXT,               -- 一句话人设："If it's slow, it's a bug."
  workspace    TEXT,               -- named agent 的持久 memory 目录；ephemeral 为空
  model        TEXT,               -- 当前绑定模型（可为空 = 跟随全局）
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  retired_at   INTEGER             -- 软删除：退役不销毁历史
);
-- named participant 全局唯一（跨 session），存全局 DB 而非 per-session
CREATE UNIQUE INDEX idx_participants_name ON participants(name) WHERE retired_at IS NULL AND kind = 'named';
```

四种 kind 的语义：

| kind | 生命周期 | 例子 | 发言权（详见第 4 章） |
|---|---|---|---|
| `human` | 永久 | 用户本人 | 全部 |
| `primary` | 每 session 一个 | 主 agent | 隐式发言（assistant 文本即发言） |
| `named` | 跨 session 持久 | Noel / QA | 显式工具发言，默认允许 |
| `ephemeral` | 单任务 | 今天的 subagent | 默认不能发言，产出走 mailbox |

#### Role vs Name 的对应

- **Role = `WorkerType`（已存在，不动）**：schema——system prompt、工具白名单、隔离模式。
- **Name = `Participant`（新增）**：instance——身份 + workspace + track record。
- 关系：`Participant.role → WorkerType.Name`。Noel 是 reviewer role 的一个 instance；spawn 时 role 提供能力包，participant 提供身份、memory 和历史。同一 role 可以有多个 named instance，一个 named instance 换 role 应视为异常操作（等于换人，需显式确认）。

#### Message（扩展现有 HistoryRecord / ThreadItem）

```
HistoryRecord 新增字段：
  participant_id  TEXT      -- 发送者；NULL = 兼容旧数据（视为 primary/user 按 Role 推断）
  thread_id       TEXT      -- 所属 thread；NULL = 主流（Phase 3）
  post_kind       TEXT      -- 'chat'（默认）| 'progress' | 'result' | 'question'

ThreadItem 新增类型：
  "participant_message"     -- 非 primary agent 的显式发言
  渲染时携带 participant: { id, name, kind, role, avatar }
```

#### Thread（Phase 3 新增）

采纳 Raft "thread 锚定在消息上" 的模型，但锚定对象是 wuu 的 item：

```sql
CREATE TABLE conversation_threads (
  id             TEXT PRIMARY KEY,
  session_id     TEXT NOT NULL,
  anchor_item_id TEXT NOT NULL,   -- 锚点：一条 user_message / participant_message / task card
  title          TEXT,
  status         TEXT NOT NULL,   -- 'open' | 'resolved'
  created_by     TEXT NOT NULL,   -- participant_id
  created_at     INTEGER NOT NULL
);
```

thread 内的消息不出现在主流；主流只显示 anchor + 回复计数徽章（Raft 的 reply-count badge 模式）。

#### Task（复用 harness Task，投影进对话）

不新建任务实体。harness `Task` 已有 `ID/Name/Role/Intent/Status/ReportPath`。Phase 3 做的是**投影**：task 创建时在对话主流插入一张 task card（一种 ThreadItem），task 的 thread 即上文 ConversationThread（anchor = task card）。状态变更更新 card，过程消息进 thread。这正是 Raft "task 是带跟踪元数据的消息" 的反向实现：wuu 的 task 已存在，补上它的消息形态。

#### Agent Event（现状保留，明确定位）

`agent/updated`、`agent/mailbox`、`turn/event`、`Activity` 心跳——这些是**工作事件流**，不是对话流。它们驱动 progress capsule / turn rail / agent tree 面板，永不进入 `session_messages`。两条流的分界是本方案的地基：

```
可见对话流（persisted, chat）        工作事件流（ephemeral/audit, work）
─────────────────────────────       ─────────────────────────────
user_message                        turn/event（provider 原始事件）
agent_message（primary 发言）        agent/updated（状态心跳）
participant_message（显式发言）      agent/mailbox（结果投递，父 agent 的 context）
task card / 状态变更                 Activity（"→ read_file"）
context_compaction 标记              token usage / telemetry
                                    subagent 的完整内部 session（可跳转检视）
```

### 3.3 展示分层策略

| 层 | 内容 | 交互 |
|---|---|---|
| **默认展示** | user_message、primary 的 agent_message、participant_message（含 result card、question）、task card 及状态变更 | 直接可读 |
| **默认折叠** | tool_call、reasoning、subagent 中间过程（thread 内消息在主流折叠为徽章）、progress 类 post | 点击展开；progress capsule 常驻显示最新一条 |
| **仅 debug/audit** | turn/event 原始事件、mailbox 原始 payload、token 明细、被丢弃的草稿/静默决策 | 开发者面板 / `--debug`；不进对话 UI |

### 3.4 Agent 如何收消息、决定回应、显式发言

**收消息（inbox 语义，不是中断语义）**：

- 用户消息默认只进 primary 的 turn。@mention 某 named participant 时，路由给它（Phase 5）。
- named/ephemeral agent 通过两条通道感知对话：(a) spawn 时的 prompt；(b) 运行中父 agent 用现有 `send_message` 注入（"queue a message without triggering a new turn"——已经是 inbox 语义，保留）。
- **不做广播**：不把每条用户消息推给所有 agent 的 context。这是 Raft agent inbox 的核心教训——"the agent decides what is worth its context"。wuu 的形态（单用户、任务导向）下更简单：**路由是显式的（@mention / spawn / send_message），没有环境广播**。Phase 5 之前甚至不需要 inbox 数据结构。

**决定是否回应（action explicitness）**：

被 @mention 的 agent 在 turn 结束时必须处于以下三态之一，UI 有对应呈现：

1. 调用了发言工具 → 消息进流；
2. 调用了 `decline`（携带一行原因）→ 主流显示灰色小字"Noel 认为无需回应：…"；
3. 什么都没调 → 视为异常（turn 结束时由 runtime 兜底为 decline），progress capsule 标记。

不被 @mention、只是被 spawn 做事的 agent：**默认静默**，产出走 mailbox。

**显式发言**：见第 4 章工具设计。

**做事但不发言时用户看到什么**：现有机制已覆盖，只需加上身份——progress capsule 显示 `Noel · 正在 review · → read_file (2m)`；turn rail 的 active marker 定位到对应 thread；agent tree 面板可下钻到它的完整 session。即：**静默 ≠ 不可见，静默 = 不占用对话流**。

---

## 4. 核心设计：做事与发消息分离

### 4.1 原则

1. **发言是显式工具调用，做事是其余一切。** agent 的 assistant 文本输出（除 primary 外）不进对话流——只有工具调用能发言。这从机制上杜绝"生成即发送"。
2. **静默是合法结果。** 工具描述中明确写入 "Silence is a valid outcome"，并提供 decline 工具让静默也成为显式动作。
3. **进流的消息是承诺，不是日志。** 工具语义按"值不值得占用用户注意力"分级，而不是按信息类型分级。

### 4.2 工具集

```
post_message(text, kind, thread_id?)
  kind: 'result' | 'question' | 'update'
  → 在对话流产生一条 participant_message
  - result:   任务结束时的结论卡。一个任务最多一条。
  - question: 阻塞性提问，UI 高亮并等待用户输入（连接现有 approval/steer 机制）。
  - update:   中途关键进展。默认折叠进 thread（无 thread 则折叠在 capsule 历史里），
              不推给用户注意力。

report_progress(status_line)
  → 只更新工作事件流（Activity/capsule），永不进对话流。廉价、无限制。
  （现状的 Activity 心跳是 runtime 自动的；此工具让 agent 能主动写一句人话状态，
   如 "发现 3 处竞态，正在确认第 2 处"。）

decline(reason)
  → 显式选择不发言。仅在被 @mention / 被要求回应时有意义。

（Phase 3 起）post_message 的 thread_id 生效：任务过程消息进 task thread，主流零污染。
```

**刻意不做的**：Raft 的 held draft / freshness check。那是多人多 agent 高并发房间的问题。wuu 是单用户产品，对话流由 turn 串行化，草稿过期场景基本不存在。等 Phase 5 出现多 agent 并发发言时再评估。

### 4.3 权限矩阵

| 能力 | primary | named | ephemeral |
|---|---|---|---|
| 隐式发言（assistant 文本即消息） | ✅ 唯一 | ❌ | ❌ |
| `post_message(result)` | —（不需要） | ✅ 默认 | ⚙️ 默认关，spawn 时 `can_post: true` 开启 |
| `post_message(question)` | — | ✅ 默认 | ⚙️ 同上（开启后 question 经父 agent 转发或直达，见 4.4） |
| `post_message(update)` | — | ✅ 默认（折叠） | ❌ 永远走 report_progress |
| `report_progress` | ✅ | ✅ | ✅ |
| `decline` | — | ✅ | —（无发言义务） |
| mailbox 回报父 agent | — | ✅ | ✅（唯一产出通道，现状） |

**频率约束**：named agent 每任务 `result` ≤ 1、`question` 不限但每条都阻塞、`update` 软限额（如每任务 5 条，超出自动降级为 report_progress）。约束由 runtime 执行，不依赖 prompt 自觉。

### 4.4 三类 agent 的发言边界

**Primary agent**：不变。它的 assistant 文本天然就是对用户的发言。它同时是 ephemeral agent 的**代言人**：mailbox 结果由它决定如何呈现（引用 result card / 汇总 / 忽略）。

**Named long-running agent**：有直接发言权，但语义是"同事在群里说话"——低频、有署名、可被单独反馈。它的 result card 是它 track record 的原材料。question 直达用户（不经 primary 转述），因为向具体的人提问正是 name 的价值所在。

**Temporary subagent**：默认哑巴，和今天一样。开 `can_post` 的场景：父 agent 判断这个任务的结论用户应该直接看到原文而不是转述（例如 review 报告）。ephemeral 的 question 默认经父 agent：它没有跨 session 身份，用户对它没有心理模型，直接对话的价值低。

**为什么 primary 保持隐式发言**：曾考虑过统一到显式工具（所有 agent 一致），否决。理由：(a) primary 与用户是一对一主对话，每条输出本来就是给用户的，强制过一层工具徒增 token 和失败面；(b) 现有全部渲染/流式管线为此构建，改造成本大收益为零。分离原则的适用对象是"房间里的其他人"，不是对话的主持人。

### 4.5 端到端流程示例

```
用户: "@Noel 看一下这个 diff 的性能影响"
 1. 路由：mention 解析 → 定位 participant Noel → 以 Noel 的 role(reviewer) + workspace 启动 turn
 2. capsule: "Noel · 正在分析 diff"；turn rail 出现 Noel 的 active marker
 3. Noel 做事：read_file / bash（全部 tool_call 折叠在它自己的 session；主流无输出）
 4. Noel 中途: report_progress("热路径在 stream.go 的重连循环") → 仅 capsule 更新
 5. Noel 结束: post_message(kind=result, "重连循环里的指数退避没有上限，...")
    → 主流出现署名 Noel 的 result card，可展开完整 session
 6. 用户对 card 点"纠正"："以后这类结论直接给数字，别给形容词"
    → 写入 Noel workspace 的 memory + Noel 的 track record 记一笔
```

---

## 5. 分阶段实施计划

每阶段独立可发布、可回退，前一阶段是后一阶段的数据基础。

### Phase 1 — 身份归属（participant attribution）

**目标**：不改交互，让"谁在做/谁做的"在现有 UI 上可见。

- **用户体验**：subagent 相关的一切展示（progress capsule、turn rail、agent tree、collab_agent_tool_call 项）从 "worker-3 / reviewer" 变成 "身份芯片"：avatar + 名字 + role 小字。ephemeral agent 自动获得可读名（从 task_name 派生，如 "Explorer·auth-flow"）。无新交互。
- **数据模型**：新增全局 `participants` 表（本阶段只有 kind=human/primary/ephemeral）；`HistoryRecord` / `ThreadItem` / `Agent` / `AgentMailboxMessage` 增加 `participant_id`；旧数据 NULL 兼容（按 Role 推断渲染）。
- **后端**：`internal/participant` 新包（实体 + store）；spawn 路径创建 ephemeral participant；protocol.go 各通知带上 participant 摘要 `{id,name,kind,role,avatar}`。
- **前端**：`ParticipantChip` 组件；接入 capsule / rail / ThreadItemView / agent tree。
- **风险**：低。主要是协议字段兼容（旧客户端忽略新字段即可）。
- **验证**：跑一个多 subagent 任务，capsule/rail/tree 全程显示正确身份；fork session、resume 旧 session 不损坏渲染。

### Phase 2 — 结果卡（subagent 产出以 participant message 进入主对话）

**目标**：把"做事/发言分离"的发言端建起来，先只开 result 一种。

- **用户体验**：subagent 完成后，主对话流出现一张署名 result card（结论摘要 + "查看完整过程"跳到其 session + report 链接）。primary 不再全文转述 subagent 结论，改为引用 card 补充观点。用户第一次能"看到 agent 本人说话"。
- **数据模型**：新 ThreadItem 类型 `participant_message`（persisted，进 `session_messages`）；`post_kind` 字段；mailbox 保持不变（父 agent 的 context 通道照旧）。
- **后端**：实现 `post_message`（本阶段仅 kind=result，仅对 ephemeral 开放且由 spawn 的 `can_post` 控制，默认对 reviewer/verification/qa 类 role 开）；runtime 在 agent 完成时若 can_post 且未调用过则**不**自动补发（静默是合法的，由父 agent 决定要不要引用 mailbox 内容）；primary 的 system prompt 更新引用策略。
- **前端**：`ParticipantMessageView`（card 形态：署名头 + markdown 正文 + 跳转链）；流式路径复用 item/started + delta 通知。
- **风险**：中。(a) 双通道重复——card 和 primary 转述说同一件事，需要 prompt 策略 + 观察调优；(b) card 时序——subagent 完成时 primary turn 可能仍在进行，card 插入位置需定义（提议：作为独立 item 追加在当时的 turn 内，位置即完成时刻）。
- **验证**：spawn reviewer → 得到署名 card；primary 的回复引用而非复读；`can_post: false` 的 agent 行为与今天完全一致（回归）。

### Phase 3 — Thread 与 Task 投影

**目标**：过程细节离开主流。主流 = 决策与结论，thread = 过程。

- **用户体验**：spawn 一个任务时主流出现 task card（名称、owner 芯片、状态）；过程消息（update、progress 历史、subagent 的中间 question）进该 task 的 thread，主流只显示 card 上的状态变化 + 回复计数徽章；点击展开 thread 侧栏。用户可以在 thread 里直接对该 agent steer（现有 send_message 的 UI 化）。
- **数据模型**：`conversation_threads` 表（3.2 节 schema）；`HistoryRecord.thread_id`；harness Task ↔ task card item 的关联字段（`Task.CardItemID`）。
- **后端**：thread CRUD RPC（`thread/openSub`、`thread/listSub`——注意与现有 thread/* 命名区分，现有 "thread" 指整个 session，建议内部命名 `subthread` 避免混淆）；`post_message` 开放 update kind + thread_id；task 状态变更钩子更新 card。
- **前端**：thread 侧栏面板（复用现有 subagent session 查看器的壳）；task card 组件；主流徽章。
- **风险**：中高。这是 UI 信息架构改动最大的一步。"session 内 thread" 与 wuu 现有 "thread = session" 术语冲突，必须先定命名；主流折叠过度会让用户失去掌控感——需要 capsule 与徽章的信息密度调优。
- **验证**：一个 3-subagent 并行任务，主流条目数 ≤ 用户消息数 + 任务数 × 2（card + result）左右；thread 内可完整回放过程；旧 session 无 thread 数据时渲染不变。

### Phase 4 — 常驻 named agents

**目标**：name 开始携带跨 session 的状态。这是"role → name"的真正跨越。

- **用户体验**：用户（或 primary 建议）把某个高频 role "固化"为 named agent：起名、选 avatar、写一句 tagline。此后 spawn 该角色的任务默认路由给它。participant card 打开后是完整档案：track record（最近任务 + 结果）、memory 摘要（可编辑/纠正）、模型与 role、changelog、reset 按钮（三层）。反馈入口挂在它的每条 result card 上。
- **数据模型**：participants 表启用 kind=named + workspace 字段；新增 `participant_runs`（participant_id, task_id, session_id, outcome, feedback, at——track record 的原始数据，可从 harness AgentRun 派生）；workspace 目录布局：`~/.wuu/participants/<id>/{MEMORY.md, notes/}`。
- **后端**：spawn 路径支持 `participant_id`（加载其 workspace 与 memory 进 system prompt）；任务结束写 run 记录；feedback RPC（写 memory + run 记录）；track record 摘要生成（廉价模型定期或惰性生成）。
- **前端**：participant 档案面板；roster 入口（sidebar 一节，列常驻 agents 及状态点——借 Raft 的 status dots）；"固化为常驻"入口。
- **风险**：高，但主要是产品风险而非技术风险。(a) memory 质量——写入垃圾会让 named agent 越用越差，需要 memory 写入的克制策略（借 wuu 自身 auto-memory 的规则）；(b) 用户根本不想管一支 roster——所以入口必须是"从用过的 role 里长出来"（用了 3 次 reviewer 后提示"要不要固化成一个常驻 reviewer？"），而不是先建团队再干活；(c) name cache 过期问题从这里开始真实存在，2.5 节机制必须同步上线，不能后补。
- **验证**：同一 named agent 跨 3 个 session 执行同类任务，第 3 次的输出体现前两次的反馈；full reset 后行为回到基线；track record 与实际历史一致。

### Phase 5 — @mention、路由与 team template

**目标**：寻址成为一等交互；团队结构可复用。

- **用户体验**：composer 里 `@` 弹出 roster；@mention 直接把 turn 路由给该 named agent（不经 primary）；被 mention 的 agent 回应或显式 decline；多 mention = 并行任务。team template：把当前 roster（名字、role、tagline、初始 memory 种子）导出为可分享的定义，新项目一键铺开。
- **数据模型**：消息的 mentions 字段（participant_id[]）；template 文件格式（YAML：participants[] + 各自 role/model/初始 prompt——若届时 OpenTeamFormat 已公开，评估对齐）。
- **后端**：mention 解析与 turn 路由（turn/start 增加 target_participant_id）；decline 工具；并行 mention 的 turn 编排（复用现有 subagent 并行机制）。
- **前端**：composer mention 自动补全；decline 的灰字渲染；template 导入导出 UI。
- **风险**：路由歧义（mention + 正文指令与 primary 的职责边界）；并行发言的时序展示。此阶段设计细节应基于 Phase 2-4 的真实使用数据再定，本文不过度展开。
- **验证**：@Noel 的 turn 不经过 primary 的模型调用；decline 路径可达；template 在空项目还原出等价 roster（memory 除外）。

---

## 6. 明确建议

**1. wuu 应该做 named agents 吗？——应该，但按"身份先于社交"的顺序做。**
判断依据：(a) wuu 的 subagent session 本来就可见，用户已经在"看见多个 agent 干活"，缺的只是这些 agent 没有脸和名字——补身份是顺势，不是转型；(b) 竞品验证：Raft 用整个产品验证了 named persistent agents 的需求存在，且其 runtime 层（Claude Code/Codex CLI）恰是 wuu 所在的位置——wuu 若不长出身份层，就只能做别人身份层下面的引擎；(c) 反馈复利（2.3 节）是 agent coding 产品的真实留存机制：用户在 wuu 里养出来的 Noel，是搬不走的。

**2. 第一刀切哪里？——Phase 1 + 2（身份归属 + 结果卡），一起做，规模约等于一个中型 feature。**
这两步不改变任何现有交互习惯，纯增量，但完成了最关键的概念迁移：用户开始把 subagent 当作"参与者"而非"过程"。Phase 3 以后的每一步是否值得做，都能从 Phase 2 的使用数据里读出来（用户点开 result card 的完整过程吗？对 card 的反馈率？）。

**3. 现在不要做的：**
- 多人协作 / server / channel——wuu 是单用户产品，这是 Raft 的战场；
- agent 之间的自由聊天——agent↔agent 通信保持结构化（mailbox/task），没有证据表明自由对话提升产出；
- held draft / freshness check——串行 turn 下不存在此问题；
- 环境广播式 inbox——路由保持显式（spawn/@mention/send_message）；
- 表情回应、已读回执、在线状态等聊天软件器官——见下条。

**4. 如何避免变成 Slack clone：**
判据只有一条：**每个新对象必须锚定在工作产物上**。thread 锚定 task，task 锚定 harness report 和 diff，participant 的 track record 锚定真实 run 历史。Slack 的对象锚定在"人际沟通"上，wuu 的对象锚定在"代码工作"上。凡是无法回答"这东西指向哪个工作产物"的功能（比如自由闲聊频道），就是越界信号。Raft 自己也是这么做的——task 是"带跟踪元数据的消息"，本质是把消息锚定到承诺上。

**5. 如何保持 wuu 的核心（coding agent 工具）：**
- primary agent 的主对话体验一个字节都不降级——所有新机制是它的外延，不是它的替代；
- 发言权限默认从紧（4.3 矩阵），对话流的信噪比是不可退让的底线；
- named agents 的价值必须表现为**代码产出质量**（review 更准、QA 更严），而不是"聊天更热闹"——Phase 4 的验证标准写的是"第 3 次输出体现前两次反馈"，就是这个意思。

---

## 附录 A：本报告引用的关键源文件

| 文件 | 内容 |
|---|---|
| `internal/session/session.go:34-50, 588-640` | Session 实体与 SQLite schema |
| `desktop/src/shared/protocol.ts:571-578, 593-617, 766-786` | ThreadItem 类型、Agent 类型、Turn 类型 |
| `internal/subagent/types.go:32-165` | SpawnOptions、SubAgent 生命周期 |
| `internal/agentcontrol/worker_types.go:19-211` | WorkerType（role schema）与 9 个内置角色 |
| `internal/agentcontrol/agent_control.go:1-2, 2223-2227` | 编排工具：spawn_agent/send_message/close_agent/list_agents |
| `internal/agentcontrol/mailbox.go:12-37` | AgentMailboxMessage（agent→parent 结果投递） |
| `internal/harness/types.go:8-150` | Task / AgentRun / Report |
| `internal/appserver/protocol.go:86-96, 1046-1048` | 通知协议（turn/item/agent 事件流） |

## 附录 B：外部资料

- https://raft.build — 官网（定位、定价、团队页含 named agents 实例：Noel/Bugen/XX/DD）
- https://docs.raft.build — features/agents（basics/lifecycle/runtime/workspace）、messaging/threads、collaboration/tasks
- https://raft.build/resources/blog/agents-need-names/ 及 https://xxchan.me/blog/2026-05-29-agents-need-names — 《Agents Need Names》
- https://raft.build/resources/blog/is-having-agents-in-the-room-meant-to-be-chaotic/ — agent inbox 与 held draft 机制
- https://raft.build/resources/blog/introducing-raft-where-humans-and-agents-build-together/ — "one agent is one session"
- OpenTeamFormat：公开渠道未找到任何信息（截至 2026-07-02）
