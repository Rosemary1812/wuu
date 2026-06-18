# Agent-Friendly Text Entrypoint 目标

## 核心目标

Wuu 需要一个 agent-friendly 的文本入口，让 agent、脚本、CI、自动化系统，以及 Wuu 自己，都能通过稳定的文本和机器可读协议驱动 Wuu。

这个目标不是恢复旧 TUI，也不是给 Electron 做一个低配替代品。核心目标是：

> 让 Wuu 可以被 agent 稳定地用文本驱动，从而让 Wuu 参与检查、修改、验证、调试、恢复和继续开发 Wuu 自己。

最终产品形态应该是：

- 人使用 Electron desktop。
- agent、脚本、CI、自动化系统使用 `wuu exec`。
- Electron、`wuu exec`、未来 IDE shell、远程控制入口共享同一个 Go core、app-server protocol、session store、thread model、tool system 和 permission model。

## 为什么要做

Agent 做软件工程时最强的操作界面是文本，而不是 GUI。代码、diff、日志、命令输出、测试失败、JSON 事件、session trace、patch 摘要，本质上都是文本。GUI 对人友好，但对 agent 来说不稳定、难解析、难组合、难恢复，也难在失败后精确继续。

当前 Wuu 已经有核心能力，但缺少一等的 agent-facing 入口：

- Electron 是当前主要的人用界面。
- `wuu app-server` 是 Electron 使用的可复用后端。
- `wuu run` 存在，但它是旧的一次性 runner，不走 app-server thread 主路径。
- 旧 TUI 已经移除，并且不应该恢复。

我们要补上的产品闭环是：

```text
agent 给 Wuu 一个任务
-> Wuu 通过自己的 core runtime 执行任务
-> Wuu 读代码、改文件、跑检查、记录事件
-> Wuu 输出稳定文本或 JSONL
-> 同一个 agent 或另一个 agent 可以从同一个 thread 继续
-> Electron 也能看到同一条 session
```

这就是 Wuu 的自举能力。Wuu 应该能通过自己的运行路径参与自己的开发，而不是只能被外部工具或 GUI 间接验证。

## 产品原则

- 不做 TUI。不要重建全屏终端 UI、键盘交互模型或终端渲染层。
- 文本优先，不追求 terminal pretty。优先保证 stdout、stderr、JSONL、文件、exit code 稳定。
- 一个 core runtime。Electron、CLI、IDE、未来 shell 不能各自长出不同的 agent 行为。
- app-server 是交互边界。`wuu exec` 应该走 Electron 同一套 thread/turn 协议。
- 机器可读输出是正式产品表面。JSONL 事件和 exit code 必须稳定到可以被其他 agent 依赖。
- 人类可读输出不能破坏自动化。默认模式下 stdout 只输出最终回答，过程和诊断写 stderr。
- 权限默认 fail closed。非交互入口不能默默批准风险操作。
- 可恢复是核心能力，不是附加功能。每次持久运行都应该产生可恢复的 thread id。

## 用户任务

### 外部 agent 驱动 Wuu

外部 agent 想让 Wuu 在仓库里执行任务、解析过程、读取最终状态，并在失败或超时后继续。

目标体验：

```bash
wuu exec --json "fix the failing test and verify it"
wuu exec resume --last "continue from the failure"
```

### Wuu dogfood 自己

开发 Wuu 时，我们希望 Wuu 通过自己的 core runtime 修改 Wuu，而不是走一条绕开桌面主路径的旧 runner。

目标体验：

```bash
wuu exec --json "implement the next step in docs/plans/..."
wuu session show --json --last
```

### CI 和自动化

脚本希望让 Wuu 执行一个任务，并根据成功、失败、超时或权限拒绝决定流水线状态。

目标体验：

```bash
wuu exec --json --timeout 20m --output-last-message result.md "review this PR"
```

### 人和 agent 交接

用户可以在 Electron 里开始或查看任务，然后让 agent 通过文本入口继续；反过来也一样。

目标结果：

- Electron 创建的 session 可以被 `wuu exec` resume。
- `wuu exec` 创建的 session 可以被 Electron 看到。

## 命令入口

### 主入口

```bash
wuu exec [PROMPT]
wuu exec -
wuu exec --json [PROMPT]
wuu exec --workdir <dir> [PROMPT]
wuu exec --provider <name> --model <model> [PROMPT]
wuu exec --timeout 10m [PROMPT]
wuu exec --max-turns 20 [PROMPT]
wuu exec --ephemeral [PROMPT]
wuu exec --output-last-message <file> [PROMPT]
```

### 恢复和 fork

```bash
wuu exec resume --last [PROMPT]
wuu exec resume <thread-id> [PROMPT]
wuu exec resume --all <thread-id> [PROMPT]
wuu exec fork <thread-id> [PROMPT]
```

### Review 和结构化任务

```bash
wuu exec review --uncommitted
wuu exec review --base main
wuu exec review --commit <sha>
wuu exec --output-schema <schema.json> [PROMPT]
```

### Session 检查

```bash
wuu session list --json
wuu session show --json <thread-id>
wuu session search --json <query>
wuu session archive <thread-id>
wuu session delete <thread-id>
```

### Debug 和协议检查

```bash
wuu debug app-server initialize
wuu debug app-server send <method> <json>
wuu debug protocol events <thread-id>
```

这些命令不是 UI。它们是给 agent 和脚本使用的稳定控制面。

## 输入协议

`wuu exec` 必须支持：

- 位置参数 prompt。
- `-` 强制从 stdin 读取 prompt。
- 没有位置参数时，piped stdin 作为主 prompt。
- 同时有位置参数和 piped stdin 时，stdin 作为附加上下文。
- 本地文件附件。
- 本地图片附件。
- PDF 附件。
- 未来的 `--input-json` 机器输入。

示例：

```bash
wuu exec "fix the failing test"
wuu exec - < task.md
wuu exec "use this log to fix the bug" < error.log
wuu exec --file report.pdf "summarize and update the code"
wuu exec --image screenshot.png "find the UI problem"
```

同时存在 prompt 和 piped stdin 时，stdin 应该被包装成附加上下文，而不是被忽略：

```text
<stdin>
...
</stdin>
```

空输入必须在启动 turn 之前失败。

## 输出协议

### 默认输出

默认模式必须适合自动化：

- stdout 只包含最终 agent message。
- stderr 包含过程、配置摘要、工具状态、trace path、token usage、warning 和诊断信息。
- stdout 不能包含 banner、debug log、progress line 或 terminal control code。

这让调用方可以可靠地写：

```bash
result="$(wuu exec "summarize this repo")"
```

### JSONL 输出

`wuu exec --json` 必须把 JSONL 写到 stdout。stdout 每一行都必须是合法 JSON object。所有非 JSON 诊断都写 stderr。

必须稳定支持这些事件族：

- `session_configured`
- `thread_started`
- `thread_resumed`
- `thread_forked`
- `turn_started`
- `agent_message_delta`
- `agent_message_final`
- `reasoning_delta`
- `reasoning_final`
- `plan_updated`
- `tool_started`
- `tool_output_delta`
- `tool_completed`
- `command_started`
- `command_output_delta`
- `command_completed`
- `file_changed`
- `subagent_started`
- `subagent_updated`
- `subagent_completed`
- `approval_requested`
- `approval_resolved`
- `usage_updated`
- `turn_completed`
- `turn_failed`
- `turn_interrupted`
- `error`
- `result`

最终 JSONL event 必须总结本次运行：

```json
{
  "type": "result",
  "status": "completed",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "final_message": "final answer",
  "trace_path": "/path/to/trace.jsonl"
}
```

## Exit code 协议

Exit code 是产品 API 的一部分：

- `0`: 成功完成。
- `1`: agent turn 失败。
- `2`: CLI 参数、配置或输入校验错误。
- `3`: 权限拒绝，或非交互场景无法获得审批。
- `4`: 超时。
- `5`: 被中断。
- `6`: 协议错误。
- `7`: provider 或 model 错误。
- `8`: 工具执行失败，且 agent 没能恢复。

脚本和其他 agent 不应该需要解析自然语言来判断成败。

## App-server 和 runtime 要求

`wuu exec` 必须复用 Electron 同一条 core 路径：

- 启动或嵌入 app-server runtime。
- 调用 `initialize`。
- 新持久线程使用 `thread/start`。
- 现有线程使用 `thread/resume`。
- 任务执行使用 `turn/start`。
- 消费 app-server notifications，直到 turn 进入终态。
- 将 notifications 映射为 human stderr 或 JSONL stdout。
- 干净 shutdown。

长期实现不能复制旧 `wuu run` 里直接调用 `StreamRunner.RunWithCallback` 的路径。旧路径可以作为兼容层保留，但目标路径必须是 app-server thread/turn。

## Session 和恢复要求

持久 exec run 必须创建或更新正常 Wuu session：

- 每次持久运行都返回 thread id。
- `resume --last` 恢复当前 workdir 最新可见 session。
- `resume <thread-id>` 恢复指定 session。
- Electron 创建的 session 可以从 exec 恢复。
- exec 创建的 session 可以在 Electron 里看到。
- `--ephemeral` 不创建持久 session。
- session 检查命令暴露足够信息，方便 agent 继续。

必需检查命令：

```bash
wuu session list --json
wuu session show --json <thread-id>
wuu session trace <thread-id>
```

## 权限和审批要求

文本入口必须使用 Wuu 同一套权限模型：

- 加载正常的项目和用户配置。
- 尊重 tool policy 和 permission mode。
- 支持 `--permission-mode`。
- 支持 `--no-tools`。
- 支持显式 allow / deny 覆盖。
- 非交互运行需要人工审批但无法获得审批时，必须 fail closed。

未来审批控制应该支持：

```bash
wuu exec --approval-handler <command>
wuu exec --approval-socket <path>
```

这样外部 agent 或编排器可以响应审批请求，而不需要 GUI。

## 结构化结果要求

Wuu 长期必须支持结构化输出：

```bash
wuu exec --output-schema schema.json "review this change"
```

必需行为：

- 读取并校验 schema 文件。
- 要求模型最终结果匹配 schema。
- 本地校验最终结果。
- 结果不匹配时在有限次数内重试。
- 无法产出结构化结果时返回明确失败。
- JSONL `result` 中包含结构化结果。

这对 review findings、CI 判断、PR 描述、release notes 和外部 workflow handoff 都是基础能力。

## 工具和文件事件要求

文本入口必须暴露 Wuu 做过的工作：

- 命令开始和完成。
- 命令输出 delta。
- 安全可暴露的工具调用参数。
- 工具结果或摘要。
- 文件新增、更新、删除或 patch。
- 子 agent 生命周期和摘要。
- managed long-running process 生命周期。
- 测试和验证状态。

事件默认必须安全。不能输出 secret、credential、hidden reasoning、未脱敏 provider payload 或敏感内部状态。

## Trace 和 debug 要求

每次持久 exec run 都应该可以调试：

- 输出 `thread_id`、`turn_id` 和 `trace_path`。
- trace 数据落在正常 session/trace 位置。
- 提供 JSON 方式后续查看 trace。
- debug log 不能污染 JSONL stdout。
- 协议错误要容易诊断，但不能暴露 secret。

## 进程生命周期要求

入口必须干净处理进程生命周期：

- `Ctrl+C` 应该 interrupt 当前 turn。
- timeout 应该 interrupt turn，并以 exit code `4` 退出。
- runtime 必须干净 shutdown。
- managed process 应该能通过 process 检查命令继续可见。
- 除非显式要求，一次性 exec run 不能留下孤儿 app-server 进程。

## 配置要求

`wuu exec` 应该支持桌面端重要的 runtime 选择参数：

- `--workdir`
- `--provider`
- `--model`
- `--effort`
- `--variant`
- `--config`
- `--profile`
- `--ignore-user-config`
- `--strict-config`
- `--env KEY=VALUE`

配置结果必须在 human mode 的 stderr 可见，并在 JSON mode 的 `session_configured` event 中可见。

## 文档要求

需要补齐：

- `docs/exec.md`: 面向用户的命令指南。
- `docs/app-server-protocol.md`: 稳定协议边界。
- `docs/jsonl-events.md`: JSONL event schema。
- README 更新：没有 TUI，Electron 给人用，`wuu exec` 给 agent 用。
- AGENTS 指南：agent 修改 Wuu 时，优先用 `wuu exec --json` 验证产品路径。

文档必须统一表达：

> Wuu has no TUI. Use Electron for human interaction and `wuu exec` for agents,
> scripts, CI, and automation.

## 测试要求

必须覆盖：

- CLI 参数解析。
- 位置参数 prompt。
- stdin prompt。
- prompt 加 piped context。
- 默认 stdout 只包含最终回答。
- JSONL stdout 每行都是合法 JSON object。
- 成功 turn 返回 `0`。
- 失败 turn 返回非零。
- timeout 返回 `4`。
- 权限拒绝返回 `3`。
- 按 id resume。
- resume 最近 session。
- ephemeral run 不创建持久 session。
- tool event 映射。
- file change event 映射。
- subagent event 映射。
- app-server fake client 测试。
- 真实 app-server 集成测试。
- 迁移期 `wuu run` 兼容测试。

## 迁移计划

1. 新增 command-line 用 Go app-server client。
2. 新增 `internal/exec` 作为文本入口实现包。
3. 新增 `wuu exec`，先完成通过 app-server 的基础一次性执行。
4. 加入自动化安全的 stdout/stderr 行为。
5. 加入 JSONL 输出模式。
6. 加入 resume 和 session 检查能力。
7. 加入 timeout、interrupt、exit code 和 clean shutdown。
8. 加入权限 fail-closed 行为。
9. 加入附件支持。
10. 加入结构化输出。
11. 将 `wuu run` 改为 `wuu exec` 的 legacy wrapper。
12. 更新 docs 和 README。

## 非目标

- 不做 TUI。
- 不做全屏终端 renderer。
- 不做键盘快捷键模型。
- 第一阶段不做交互式终端审批 UI。
- 不替代 Electron。
- 不做第二套 agent runtime。
- 不复制 tool system。
- 不为非交互任务做隐藏自动批准。

## 成功标准

这项工作完成时，应该满足：

- 外部 agent 可以通过 `wuu exec --json` 驱动 Wuu，并可靠解析进度和最终状态。
- Wuu 可以通过 `wuu exec` 修改和验证 Wuu 仓库本身。
- Electron 和 `wuu exec` 共享 session 和 resume 行为。
- 默认 stdout 可以安全用于 shell 组合。
- JSONL 输出稳定到可以作为自动化协议。
- 权限和审批行为明确，并且 fail closed。
- 旧 TUI 保持移除状态。

## Active Goal Pointer

后续实现目标应该指向这份文档：

```text
GOAL_AGENT_FRIENDLY_TEXT_ENTRYPOINT.md
```

未来实现工作应把这份文档作为完整产品和工程目标的 source of truth。
