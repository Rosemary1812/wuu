# 记忆系统重设计 — 实施契约

日期：2026-07-04
状态：设计定稿，实施中
性质：**本文档是给实施 agent 的强约束契约。** 与本文档冲突的旧文档以本文档为准；修改本设计需先改本文档再改代码。
关联：取代 `internal/memory/store` 注入链路与 `# Persistent Memory` 提示词区块；修订 `2026-07-03-resident-named-agents.md` §5 的记忆相关段落（该文档其余部分继续有效）。

## 0. 背景与动机（为什么重做）

调查结论（2026-07-04，运行时证据）：现有记忆系统从未产生过一条真实记忆。三个根因：
1. 记忆工具（read_memory/write_memory/session_memory）全部 deferred，模型看不见；提示词却引导直用。
2. 常驻 agent 被注入的记忆文件（`participants/<id>/MEMORY.md`）与被引导写入的文件（`agents/<id>/home/MEMORY.md`）不是同一份，且前者不在其文件白名单内——写读两头断。
3. 后台蒸馏（dream/reviewer）fire-and-forget，进程退出即死，从未成功完成。

设计原则来自 Claude Code 的记忆架构（`thirdparty/claude-code-sourcemap/src/memdir/`）：
- 记忆 = 文件目录，不是数据库。写记忆 = 普通文件工具，无专用工具。
- `MEMORY.md` 是**索引**（一行一条：`- [标题](文件名.md) — 一句话钩子`），正文在独立主题文件里。只有索引进上下文。
- LLM 只站在"写"和"整理"的路径上，绝不站在"读"的路径上。

## 1. 决策记录（用户已拍板，不再讨论）

| # | 决策 |
|---|---|
| D1 | 存储 = CC 索引架构：MEMORY.md 索引 + 独立记忆 md 文件（带 frontmatter） |
| D2 | **只做两本笔记本**：用户记忆 + 每个 named agent 的身份记忆。**项目记忆 v1 不做** |
| D3 | 展示与存储解耦：设置页面板展示的是 agent 现场调一次 LLM 生成的**结构化小短文**（骨架屏等待）；用户通过面板 chat 修改时，管理 agent 调工具改**真实的索引记忆**（索引 + 具体记忆文件都改） |
| D4 | 管理 agent 复用基础 agent runtime，仅收紧提示词与工具；权限默认放开，但**文件范围只允许两个记忆目录** |
| D5 | 小短文按层分开：用户记忆一篇、每个 named agent 一篇，各自可通过 chat 修改 |
| D6 | 权限阶梯：named agent 读写自己的身份记忆；session agent 读写用户记忆；临时 worker 只读注入 |
| D7 | 生命周期：记忆身份键 = participant ID / workspace UUID（名字与路径只是标签）；退休 = 归档不删除 |

## 2. 存储布局与格式

```
~/.wuu/memory/                      用户笔记本（沿用现有目录，内容迁移见 §7）
  MEMORY.md                         索引，上限 200 行 / 25KB，超限截断加警告
  <topic>.md                        一条记忆一个文件
~/.wuu/participants/<id>/memory/    身份笔记本（每个 named agent 一个）
  MEMORY.md
  <topic>.md
```

记忆文件格式（与 CC 一致）：

```markdown
---
name: <短横线小写标识>
description: <一句话描述——未来判断相关性用，要具体>
type: user | feedback | reference | lesson
---

<正文；feedback/lesson 类型建议结构：结论一句 + **Why:** + **How to apply:**>
```

索引行格式：`- [标题](<topic>.md) — 一句话钩子`（≤150 字符，索引里绝不放正文）。

**什么不该记**（写进提示词，对用户显式要求也适用）：能从代码/git/AGENTS.md 查到的、一周内会过期的、任务进度、PR 号/commit SHA、原始转录。

## 3. 写入路径与权限

写入 = 普通文件工具（write_file/edit_file），两步：写主题文件 + 更新索引行。**无专用记忆工具。**

| 执行体 | 用户笔记本 | 身份笔记本 |
|---|---|---|
| session 主 agent | 读写（文件白名单加入 `~/.wuu/memory/`） | 无 |
| named agent（DM/群聊） | 只读（索引注入 prompt，目录**不进**白名单） | 读写（白名单加入自己的 `participants/<id>/memory/`） |
| 面板管理 agent | 读写 | 读写（仅当前选中的 agent） |
| 临时 worker | 只读注入 | 无 |

安全：注入前对文件内容跑现有威胁扫描（`internal/tools/memory.go` 的 `scanMemoryContent` 模式迁移到注入侧）；文件范围由现有 `FileScopeRoots` 机制硬约束，不靠提示词自觉。

## 4. 注入规则与缓存红线

**缓存红线（任何实现不得违反）：**
1. 前缀内容（系统提示词、记忆教学文字、索引快照）只允许在**会话/线程创建**和 **compact** 两个时刻变化；
2. 会话中途的记忆活动只允许以**末尾追加**形式出现（保存回执等）；
3. 后台整理任务要么低频（夜间），要么分叉复用主对话缓存；不允许每 turn 重读文件重拼前缀以外的新内容。

具体：
- **session agent**：会话启动时注入"记忆教学区块 + 用户 MEMORY.md 索引内容"（替换现有 `# Persistent Memory` 区块，`internal/prompt/builder.go` `AddProfileMemory*` 退役）。会话内冻结；中途写入下个会话生效。
- **resident agent**：人设提示词（`participant_prompt.go`）追加"记忆教学 + 自己身份 MEMORY.md 索引 + 用户 MEMORY.md 索引（只读知识）"。v1 保持现有每 turn 重建机制（文件不变则字节稳定，缓存命中不变差）；v2（另立任务）改为创建时冻结 + compact 时刷新 + 中途写入尾部声明。

## 5. 提示词改动清单

1. `prompts/system_main.md:13` 的 write_memory/read_memory 行删除。
2. `internal/prompt/builder.go` 的 `AddProfileMemoryWithLimits` 替换为新的 `AddMemdir` 区块：教学文字（四型分类、两步保存、What NOT to save、目录已存在直接写）+ 索引内容 + 截断警告。教学文字参考 CC `memdir.ts buildMemoryLines`，中文语境适配。
3. `participant_prompt.go`：
   - "Keep durable notes in MEMORY.md" 改为指向自己的身份记忆目录（绝对路径写进提示词）；
   - **补回 `## Wrapping up discussions (only when asked)` 段**（resident 文档 §5 @379-388 逐字，此为红线 6 欠账）；
   - `## Memory` 段改为读 `participants/<id>/memory/MEMORY.md` 索引（迁移期兼容旧平文件，见 §7）；
   - 追加用户索引只读注入段 `## What you know about the user`。
4. 按 resident 文档红线 6：本文档即为"先改文档"的载体，resident 文档 §5 加一行指向本文档的修订说明（docs 清理任务负责）。

## 6. 旧机制退役

- `internal/runtime/session.go`：不再构造/挂载 `newProfileMemoryProvider` / `newWorkspaceMemoryProvider`（工具因无 provider 自动隐藏——现有门控机制免费完成工具下线）；`profileMemoryReviewScheduler` 停用（reviewer 的职责由面板整理与后续 dream v2 承接）。
- `internal/modelprofile/compiler.go`：`addMemoryTools` 中 read/write_memory 两行删除（session_memory 保留现状，v1 不动 dream/sessionmemory）。
- `internal/memory/store` 包保留代码不删（一个版本周期后清理），但无任何生产接线。
- `<ws-state>/memory-store/` 不再创建。

## 7. 迁移（一次性，启动时惰性执行）

均为小文件，风险低；迁移器幂等（存在标记文件即跳过）：
1. `~/.wuu/memory/entries.jsonl`（如有条目）→ 逐条转为主题文件 + 索引行；原文件改名 `.migrated`；旧模板 MEMORY.md 被新索引覆盖。
2. `participants/<id>/MEMORY.md`（旧平文件）→ 内容移入 `memory/legacy-profile.md` + 索引行；原文件改名 `.migrated`。
3. `agents/<id>/home/MEMORY.md`（agent 被误导写的孤儿）→ 同上并入该 agent 身份笔记本。

## 8. 设置页记忆面板

### 8.1 交互（用户已拍板）

```
设置 → 记忆
├─ Tab「我」                     用户笔记本
└─ Tab「同事」+ agent 选择器      身份笔记本
每个 Tab：
  打开 → 骨架屏 → 调 memory/overview（概览 agent 一次 LLM 调用）→ 渲染结构化小短文
  底部 chat 输入框 → memory/chat（管理 agent）→ 改真实索引+文件 → 返回回复与变更清单 → 短文重新生成
  「查看原文」开关 → memory/read 直接渲染真实 MEMORY.md 索引与文件清单（无 LLM，兜底与审计用）
```

概览短文结构模板（概览 agent 提示词固定）：用户篇=身份背景/协作偏好/沟通风格/当前关注；同事篇=与用户的相处之道/协作教训/技艺笔记/承诺与定案。短文按（笔记本, 索引 mtime）缓存，未变更时二次打开秒出。

### 8.2 RPC 契约（前后端共同遵守，先于实现固定）

```
memory/overview  params: {scope:"user"|"participant", participant_id?}
                 result: {essay_md, generated_at, source_mtime, cached:bool}
memory/chat      params: {scope, participant_id?, message}
                 result: {reply_md, changed_files:[{path, action:"created"|"modified"|"deleted"}]}
memory/read      params: {scope, participant_id?}
                 result: {index_md, files:[{name, description, type, mtime}]}
```

### 8.3 两个面板 agent（复用基础 runtime）

- **概览 agent**：一次性调用；工具 = 只读（read_file/glob，范围限当前笔记本目录）；输出 = 按模板的结构化短文；禁写。
- **管理 agent**：工具 = read/write/edit 文件（`FileScopeRoots` 限定为用户笔记本 + 当前 agent 身份笔记本两个目录，目录内权限全开）；提示词教它：改正文必同步索引、删条目必删索引行、遵守"什么不该记"；单轮往返，返回自然语言回复 + 实际变更文件清单。

## 9. 生命周期规则

- 身份键 = participant ID / workspace UUID（后者已由 2026-07-04 稳定 ID 改造落地）。
- **退休 named agent**：身份笔记本整目录移入 `participants/.archived/<id>/`；退休清单同时执行（移出群成员、丢弃待处理信封、DM 冻结只读）——清单实现在修补总纲第三波，本文档只约束记忆部分：**任何清理实现不得直接删除记忆目录**。
- **同名重建**：新 ID、空笔记本（默认）；创建入口检测归档区同名前任，提供"复职（恢复原 ID 全部状态）/继承笔记本（拷贝）/全新"三选一（复职 UI 为 v2，本期仅保证归档数据完好）。
- **忘掉 X**：v1 = 面板 chat 单笔记本删除；跨笔记本"到处忘掉"为 v2。

## 10. 实施切分与验收

| 任务 | 范围 | 文件属地 |
|---|---|---|
| M1 后端基座 | `internal/memdir` 新包（路径/索引读取/截断/教学文本/迁移）；§5 提示词改动；§3 白名单；§6 退役 | internal/memdir, prompts/, internal/prompt/builder.go, internal/runtime/session.go, internal/appserver/participant_prompt.go, turn_handlers.go(白名单行), internal/modelprofile/compiler.go |
| M2 面板后端 | §8.2 三个 RPC + 两个面板 agent | internal/appserver/(新文件 memory_panel*.go), protocol.go, server.go |
| F1 面板前端 | §8.1 全部 UI | desktop/src/(SettingsView, 新组件), preload, shared/protocol.ts |

验收标准：
1. 新开 session，对模型说"记住我偏好中文简短回复"→ agent 用文件工具写入用户笔记本（主题文件+索引行）→ 下个 session 的提示词含该索引行。
2. DM 里 named agent 学到教训 → 写入自己身份笔记本 → 下个 turn 注入的索引可见；`participants/<id>/memory/` 在其白名单内，`~/.wuu/memory/` 不在。
3. 设置页两个 Tab 均可：骨架屏 → 短文展示；chat "删掉关于 X 的记忆" → 真实文件与索引变更 → 变更清单回显。
4. `go build ./...` 与相关包测试全绿；旧 read/write_memory 不再出现在任何工具面。
