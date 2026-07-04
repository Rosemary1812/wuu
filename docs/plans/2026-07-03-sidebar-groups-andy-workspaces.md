# 侧栏基线修复、群聊入口、Andy 与工作区范围 — 设计

日期：2026-07-03
状态：已实施
上游：`2026-07-03-resident-named-agents.md`（公理与 §5 常驻提示词——本文新增的提示词段已同步进该文档，红线 6 适用）、`2026-07-03-chat-style-threads-design.md`（聊天视图与群契约——本文 §2 修订其侧栏描述，§3-§4 兑现其 "out of scope" 段）。

四项已裁决的取舍（2026-07-03，与用户确认）：

1. **Andy = 普通 named resident**，首次启动预置，可改名/改人设/删除，机制上零特权。
2. **群管理工具（`create_group` / `add_group_member`）授予所有 resident**，Andy 只是人设上以组队为专长（公理 7 精神：无特权通道）。
3. **工作区清单注入所有 resident 的提示词**（与文件范围联动）。
4. **文件操作范围由文件工具在 runtime 强制**（家目录 + 工作区白名单）；bash 无法完全拦截，靠 CWD 限定 + 提示词约束，缝隙如实记录。

---

## 1. 侧栏基线修复（回归与不一致）

### 1.1 DM 入口与 session 入口的视觉统一

现状：Agents section 下的 DM 入口是 roster participant 行（头像 + 名字 + 忙碌/未读点，右键菜单），项目/对话 section 下的 session 入口是 thread 行（MessageSquare 图标 + 标题 + hover 操作），两套行规格互不一致。

决定：**统一行规格，保留身份差异**。DM 行保留头像（它是"人"的入口，且与聊天视图的 28px 头像同源），但行高、内边距、字号、hover 背景、未读徽标的视觉语言、右键菜单的结构与 thread 行对齐——共用同一套 CSS token（提取共享行样式，禁止两份平行的行规格）。验收：同一侧栏里两类行肉眼属于同一家族，只有"头像 vs 图标"的身份差异。

### 1.2 hover 新建会话按钮（回归修复）

原设计：对话 section 与每个项目 section 的头部 hover 时出现 MessageSquarePlus 按钮，点击在该 section 的 CWD 下新建 session。现状：项目行的按钮代码仍在（`ThreadSidebar.tsx:227`），但用户观察到按钮不出现——按**回归**排查（CSS 隐藏、条件分支变更、对话区从未接上，均在排查范围内）。

验收：对话 section 与每个项目 section 头部 hover 均出现该按钮；点击分别在 `<wuuHome>/scratch` 与项目根目录下开新 session。

### 1.3 对话区 CWD 契约（实现已有，升为契约）

对话（scratch）区 = **无项目的纯聊天区，以 session 组织**，与项目同构。其 CWD 固定为 `<wuuHome>/scratch`（后端 `thread_handlers.go` 已按此目录归类 scratch threads；这就是"每台机器必有的默认目录"）。本条不是新实现，是把既有行为写成契约，防止后续改动漂移。

---

## 2. 群聊入口（修订 chat 设计 §1 的侧栏描述）

- **群聊 section 与其他 section 同权**：加入 `sectionOrder`（可拖拽重排）、可折叠展开。撤销"固定锚在置顶与对话之间"的现状（`AppSidebar.tsx` 注释 "Not part of sectionOrder" 作废）。原则：**置顶下方的所有 section（Agents、群聊、对话、各项目）一律可重排**，置顶自身固定。
- **hover 建群**：群聊 section 头部 hover 出现 + 按钮，点击 → 输入标题 → `thread/start {group: true, title}`。与 Agents section 的"新建 Agent"、项目的"新建会话"交互同族。
- **`# all` 从占位变真实**：后端幂等保证 `all` 存在（chat 设计 §3.2 已定）后，前端不再渲染禁用占位行，直接渲染真实入口。群聊行保留 `#` 前缀。
- 新群初始成员 = 空（创建者是用户时）；成员通过 composer @ 自动入群（T6 语义）、roster 拉人、或 agent 的 `add_group_member`（§4）。

---

## 3. Andy — 默认组队 agent

**身份**：首次启动时自动创建的**普通 named resident**。预置名字 Andy、组队人设、默认头像；用户可改名、改人设、删除。创建用幂等标记（state 记 `default-agent-seeded`，仅首次启动创建一次）——**删除后不自动复活**，用户可在 roster 手动重建任意 agent。机制上 Andy 无任何特权：不是保留角色，不走特殊通道，公理体系不为它开例外。

**职责（纯人设层）**：帮用户把团队拉起来。它的工具与所有 resident 相同——`manage_participant` 创建新 named agent、`create_group` 建群、`add_group_member` 拉人。"首次引导"不做机制：用户第一次在 `# all` 说话时，Andy 作为隐式成员被唤醒（chat 设计 §3.2），由人设指引它自我介绍、询问目标、提议一个最小团队配置。

**预置人设（草案，实施时可润色，UI 文案中文）**：

> 名字：Andy
> 角色：团队组建者
> tagline：帮你把合适的 agent 拉进合适的房间
> 人设补充：你擅长按用户的目标组建 agent 团队——创建新的 named agent、建群、把相关成员拉进群。用户第一次在 # all 说话时，主动自我介绍并询问想做什么，然后提议一个最小可用的团队配置。先问清目标再动手，宁缺毋滥：不要一口气创建一堆用不上的 agent，不要为一次性问题建群。

---

## 4. 群管理工具（conversation-native bundle 新成员，所有 resident）

```
create_group(title) -> {thread_id}
  创建 group thread（等价 thread/start {group:true}）。调用者自动成为成员。

add_group_member(thread_id, participant_id)
  目标必须是 group thread；调用者必须是其成员；participant 必须 kind=named。
  语义同现有 AddThreadMember（自动入群路径复用）。
```

约束：

- **不给 agent 移人/解散/改名工具**。移除成员走用户 UI（`thread/members/remove`，后端 handler 于 2026-07-04 补齐，此前仅前端接线）；`# all` 是系统保证的频道，agent 与用户都不可解散或改名。
- **频率预算**（runtime 强制）：沿用 `post_message` 的速率限制器；每 turn 建群 ≤ 1、拉人 ≤ 8 次；群成员上限 8（resident 文档 §6 已定）不变，超限报错。
- **提示词约束**（进 §5，见 §6）：建群须有持续性目的，优先复用既有群，禁止为一次性问题建群。
- 任务 run（`participant/start`）与普通 subagent **不授予**这两个工具（resident 文档 §6 矩阵已增行）。

---

## 5. 工作区注入与文件操作范围

**心智模型（OpenClaw / Hermes 式）**：resident 有一个"居住"的家目录（`statepath.AgentHomeDir`，resident thread 的 CWD），但它的价值在于替用户"跨出去"操作真实项目。跨出去的可达域必须有边界：**边界 = 用户添加过的工作区**（侧栏"添加工作区"的项目清单，即项目根目录）。

### 5.1 注入（所有 resident）

§5 常驻提示词每 turn 重建时注入工作区清单：项目名 + 根目录绝对路径，每行一条；清单为空时写 `(none yet)`。提示词段全文见 resident 文档 §5（红线 6：以该文档为准）。

缓存留意：工作区清单与 MEMORY.md 同为 system prompt 前缀的失效源，同受 resident 文档 §5"缓存纪律"约束——清单变更频率低（添加/移除工作区时），接受变更时一次性缓存重建。

### 5.2 范围强制（文件工具 runtime 校验）

- **白名单** = 家目录（递归）+ 所有工作区根目录（递归）+ 系统临时目录。
- **读写同权**：`read` / `write` / `edit` / `glob` / `grep` 在工具层解析路径（清洗 `..`、解析符号链接后比对），越界一律拒绝——读也拒绝，防止翻越用户未授权的私人目录。错误信息须说明范围并指引："该路径不在工作区内，请用户在侧栏添加该目录为工作区后重试"。
- **装配范围**：白名单校验只装配给 resident turn 与 participant 任务 run 的 toolset。普通 work session（项目内）的工具行为**不变**——它本来就工作在用户显式选择的项目里。
- **bash 缝隙（如实记录）**：bash 天然可越界，v1 不做命令级路径沙箱（易误伤管道/相对路径/符号链接，性价比低，见 §8）。约束 = CWD 恒为家目录 + 提示词明令禁止绕行（§5 提示词段）+ 后续方向：越界访问审计。

---

## 6. §5 提示词增补（镜像，正文以 resident 文档 §5 为准）

新增两段（位于 "Wrapping up discussions" 与 "Context discipline" 之间）：

```
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
```

---

## 7. 分任务拆解（每个可独立提交，顺序即依赖）

| # | 任务 | 端 | 验收 |
|---|---|---|---|
| S1 | DM 行与 thread 行的行规格统一（共享样式/token） | 前端 | 视觉走查：同族行规格，仅头像/图标差异 |
| S2 | hover 新建会话按钮回归修复（对话 + 项目两处） | 前端 | 两处 hover 出按钮，新 session 落对应 CWD |
| S3 | 群聊 section 入 sectionOrder + 可折叠 + hover 建群按钮 | 前端 | 群聊可拖拽重排、可折叠；+ 建群走 thread/start {group:true} |
| S4 | `create_group` / `add_group_member` 工具 + 频率预算 | 后端 | resident turn 内建群拉人成功；任务 run/subagent 调用报错；预算生效 |
| S5 | Andy 首次预置（幂等标记；预置人设） | 后端 | 全新 state 首启创建 Andy；删除后重启不复活 |
| S6 | 工作区清单注入 §5 提示词 | 后端 | provider request 的 system prompt 含清单；空清单渲染 (none yet) |
| S7 | 文件工具白名单校验（家目录+工作区+temp；读写同权；仅 resident/任务 run 装配） | 后端 | 越界读与写均报错且错误信息含指引；work session 不受影响 |
| S8 | e2e：`# all` 首聊 → Andy 自我介绍并提议团队；Andy 建群拉人 | 全链路 | 人工验证脚本 |

---

## 8. 刻意不做（本期）

- agent 的移人/解散/改名工具（破坏性高，走用户 UI）。
- bash 命令级路径沙箱（误伤面大；先 CWD + 提示词 + 审计方向）。
- per-agent 的工作区授权（所有 resident 同一白名单；有真实需求再分级）。
- Andy 的专用 onboarding 机制（纯人设 + 现有信封机制承载）。
- 工作区清单的 per-项目描述注入（项目暂无描述字段，只注入名字 + 路径）。
