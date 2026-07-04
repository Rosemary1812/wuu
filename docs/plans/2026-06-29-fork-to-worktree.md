# Fork to Worktree — 状态报告

> **状态报告（不是未来计划）。** 截至 `main` 当前状态：session schema 扩展、
> 协议 `Mode` 字段、core fork handler 创建 worktree、tool/runtime 切 CWD
> （步骤 5，2026-07-04 已实施）已经落地；桌面 `MessageForkDialog`、sidebar
> ⎇ 图标与环境警告尚未实现。下文按"已落地 / 部分落地 / 开放"三类列出证据
> 与剩余工作。

## 目标

让 `thread/fork` 支持两种派生模式：

- **派生到本地**（`mode: "local"`，默认）：在当前 CWD 复制一份截断历史继续。
- **派生到新工作树**（`mode: "worktree"`）：基于当前 git 仓库 HEAD 开一个
  worktree，新 session 在里面跑，文件改动物理隔离。

绑定了 worktree 的 session 应该在 sidebar 显 ⎇ 图标，并在 session 视图
顶部显示"文件已隔离，运行时环境（进程、网络、env）未继承"的诚实警告。
agent 工具在执行时如果发现 session 绑了 worktree，要把 CWD 切到 worktree
路径。

## 落地状态

| 步骤 | 状态 | 证据 |
| --- | --- | --- |
| 1. Session schema 加 worktree 字段 | **已落地** | `internal/session/session.go:47-49`（`WorktreePath`、`WorktreeBaseHEAD`、`WorktreeBaseRepo` 字段）、`:142-145`（`CreateWithWorktree`）、`:151-174`（`createWithMetadataAndWorktree` 写入三列）、`:323-331`（`BindWorktree`）、`:343-353`（`Session.WorktreeInfo` 方法）；`internal/session/session_test.go:106,140,168` 的 round-trip 测试 |
| 2. 协议 `ThreadForkParams.Mode` + `WorktreeInfo` 类型 | **已落地** | `internal/appserver/protocol.go:657`（`ThreadForkParams.Mode`）、`:660-662`（`ThreadForkResult.Worktree *WorktreeInfo`）、`:1102`（`Thread.Worktree` 投影）、`:1116-1122`（`WorktreeInfo` 结构体） |
| 3. Core fork handler 在 `mode=worktree` 时创建 worktree | **已落地** | `internal/appserver/thread_handlers.go:265-274`（`mode == "worktree"` 分支调 `manager.Create`）、`:291-292`（成功后 `session.CreateWithWorktree`）、`:678`（`worktree.NewManager(parentRepo, statepath.WorktreeRoot(stateDir))`） |
| 4. Worktree 状态通过 `thread/get` 回流 | **部分落地** | `Thread` 投影已带 `Worktree *WorktreeInfo`（`internal/appserver/protocol.go:1102`）。原计划提到的独立 `MethodSessionGet = "session/get"` **未添加**；`internal/appserver/protocol.go:18-69` 中没有 `session/get` 方法常量。`thread/get` 当前返回的 `WorktreeInfo` 也不会在 handler 里调 `worktree.Status` 刷新 `dirty` / `changed_files` |
| 5. `toolctx` 注入 worktree 路径 + 文件 / shell 工具切 CWD | **已实施（2026-07-04）** | `internal/toolctx/toolctx.go`（`WithWorktreePath` / `WorktreePath(ctx)`）；`internal/appserver/turn_handlers.go` 的 `runTurnWithRequestContext`（所有 turn 变体的共同入口）把 `th.WorktreePath`（源自 session 元数据）注入 ctx；`internal/tools/env.go`（`ExecPath` / `ExecRootDir` / `NormalizeDisplayPathExec` / `RevisionRoot`）在 sandbox / 白名单 / 敏感路径检查**通过后**才把已批准的工作区路径重定位到 checkout——白名单语义不放宽，checkout 缺失（未 ready）时报错而不是静默回落 parent repo；`read_file` / `write_file` / `edit_file` / `list_files` / `grep` / `glob` / `run_shell` / `bash` / `git` / `apply_patch` / `run_test` 全部切换。测试：`internal/tools/tool_worktree_exec_test.go`、`internal/appserver/server_test.go` 的 `TestServerWorktreeBoundTurnExecutesInWorktree` |
| 6. 桌面 `MessageForkDialog` 组件 | **未实现** | `desktop/src/renderer/MessageForkDialog.tsx`（及对应测试文件）不存在；`MessageActions.tsx:47` 的 `onFork` 仍直接调用 |
| 7. Sidebar ⎇ 图标 + `WorktreeEnvironmentWarning` 顶部警告 | **未实现** | `WorktreeEnvironmentWarning`、`isLastAgentMessage` 在仓库内零命中。`ThreadSidebar` 不感知 worktree 状态 |

## 剩余工作的风险敞口

> 2026-07-04：步骤 5 已实施（证据见上表）。以下两段保留为实施前的风险分析
> 存档，其中步骤 5 的描述不再反映现状。

**步骤 5** 是最大的未交付项：worktree 隔离只在 fork 时物理创建目录并把
session 的 `CWD` 切过去，但 agent 工具在 turn 执行时仍从 `session.CWD`
取工作目录，绑了 worktree 的 session 当前不会让文件 / shell 工具切换到
worktree 路径——文件读到的仍是 parent repo，不绑 worktree 的 fork 在容器
里也会写到 parent repo。

要做到这点需要先在 `internal/toolctx` 提供 `WithWorktreePath` /
`WorktreePath`，再让 `internal/agent/tool_runtime.go`（或具体工具）在入口
读 worktree 路径注入 `cmd.Dir` 和文件 resolver。worktree 路径必须**在
sandbox 检查通过后**应用——避免 sandbox 把它当成越界。

**步骤 6 / 7** 是 UX 增量：绑了 worktree 的 session 不可被一眼识别，
agent 也不会被诚实警告工作区已隔离。`internal/worktree.Manager` 已经
有 `Status` 与 `Dirty / ChangedFiles` 字段可以驱动这两个 UI，但调用链
从协议层流到 Electron renderer 还没有接通。

## 影响文件清单

**Core（Go）—— 已经改动**：

- `internal/session/session.go`（schema + `CreateWithWorktree` +
  `BindWorktree` + `WorktreeInfo` 方法）
- `internal/session/session_test.go`（round-trip 测试）
- `internal/appserver/protocol.go`（`ThreadForkParams.Mode`、
  `ThreadForkResult.Worktree`、`Thread.Worktree`、`WorktreeInfo`）
- `internal/appserver/thread_handlers.go`（`mode == "worktree"` 分支 +
  `worktree.NewManager`)

**Core（Go）—— 步骤 5 已改（2026-07-04）**：

- `internal/toolctx/toolctx.go`（`WithWorktreePath` / `WorktreePath`）
- `internal/tools/`（`env.go` 的执行根助手 + 文件 / shell / 搜索 / git /
  apply_patch / run_test 工具在检查通过后切 CWD）
- `internal/appserver/turn_handlers.go`（turn 启动时把 `th.WorktreePath`
  注入 ctx）

**Desktop（Electron）—— 步骤 6 / 7 待改**：

- `desktop/src/renderer/MessageForkDialog.tsx`（新建）
- `desktop/src/renderer/MessageForkDialog.test.tsx`（新建）
- `desktop/src/renderer/WorktreeEnvironmentWarning.tsx`（新建）
- `desktop/src/renderer/MessageActions.tsx`（`onFork` 接对话框）
- `desktop/src/renderer/App.tsx`（fork 弹层 state + `isLastAgentMessage`
  判断）
- `desktop/src/renderer/ThreadSidebar.tsx`（⎇ 图标）
- `desktop/src/renderer/SessionTabs.tsx`（⎇ 图标，如适用）
- `desktop/src/renderer/ThreadSidebar.test.tsx`（图标测试）
- `desktop/src/shared/protocol.ts`（`WorktreeInfo` 类型）

## 不在范围内（v1 显式不做）

- Codex "Apply working tree diff"（detached HEAD + 把 parent repo 当前
  dirty diff apply 到新 worktree）。`session` 模型不存文件快照，做这步
  要 fork 时序化 `git diff` + `git apply --3way` + 失败回滚，v1 风险 /
  收益不对等
- sub-agent fork 路径的 worktree 改造。`internal/subagent/manager.go`
  已经在用 `worktree.NewManager`，走它自己的 worktree 机制
- worktree 的 merge-back UI。`worktree.Manager` 已经有 `MergePreview` /
  `ApplyToTarget` 但未暴露给用户；先用 CLI 兜底
- worktree 的定时清理，用户手动清理
- worktree 的可视化文件 diff。`worktree.Status` 暴露 dirty 状态，可视化
  留给以后
- desktop debug 控件。仍 gated 在 debug controls 开关下
