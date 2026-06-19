# Tool-Call History Architecture

> 日期：2026-06-19
> 背景会话：`20260619-033010-68f92eb64dcb5092`
> 状态：已落地第一版命名和边界整理

## 背景

这次事故的直接原因是一次 assistant 工具调用被持久化时携带了不完整的
JSON 参数，后续恢复会话时这条历史被重新发给 provider，导致请求在真正
进入模型前就失败。表面上看是某个 provider 的报错，实际问题是 Wuu 在
工具调用历史边界上没有把“可修复的历史问题”和“不可持久化的坏工具调用”
分清楚。

目标不是给 Anthropic、OpenAI 或任何单个 provider 打补丁，而是建立一套
所有 provider 共享的工具调用历史规则，再在请求边界保留必要的
model/provider 兼容转换。

## 设计结论

工具调用历史不变量是 provider 无关的：

- assistant 发出的每个工具调用必须有合法 ID。
- 同一条 assistant 消息内不能出现重复工具调用 ID。
- 已持久化的 assistant 工具调用参数必须是合法 JSON。
- 每个 assistant 工具调用在发送给模型时必须紧跟对应的 tool result。
- 缺失的 tool result 可以合成为中断错误结果。
- 孤儿 tool result 应该被移除，不能继续污染历史。
- 可确认由本地输入校验产生的坏工具调用，可以和对应错误结果一起移除。
- 修复后仍不满足顺序规则的历史必须拒绝发送。

model/provider 兼容转换只属于请求边界：

- Claude/Mistral 这类 ID 字符限制只在发请求前处理。
- Mistral 需要的 tool 后 assistant 分隔消息只在发请求前插入。
- lone surrogate 等文本清洗属于请求编码兼容，不应该改写用户的原始历史。
- 这些转换不能替代工具调用历史修复，也不能把不合法历史写回存储。

## Wuu 命名规范

为了避免以后继续把不同概念都叫 normalize：

- `RepairToolCallHistory`：只做可安全合成的工具历史修复。
- `ValidateToolCallHistory`：检查工具历史不变量，不能修时返回错误。
- `RepairAndValidateToolCallHistory`：先修复，再校验，用于恢复、fork、turn
  完成后的历史落盘前检查。
- `ValidateAssistantToolCalls`：在 provider 返回结果进入持久化历史前校验
  assistant 工具调用本身。
- `PrepareMessagesForModelRequest`：发请求前的统一入口，包含历史修复、
  校验、以及请求专用 model/provider 兼容转换。
- `ApplyModelMessageCompatibility`：只做请求专用兼容转换，不改持久化历史。

`normalize` 仍可用于配置、枚举、phase、retry 等普通规范化场景，但不再用来
描述工具调用历史修复。

## 参考实现

### Codex

Codex 在发送 prompt 前会对历史做工具调用配对修复：

- `thirdparty/codex/codex-rs/core/src/context_manager/normalize.rs`
- `ensure_call_outputs_present` 为缺失工具输出插入 aborted 结果。
- `remove_orphan_outputs` 移除没有对应调用的输出。
- 工具执行或解析错误会以模型可见的失败结果返回，而不是留下半截调用。

Wuu 采用同样的基本方向：请求前修复历史，缺失结果合成中断错误，孤儿结果
不继续发送给模型。

### Claude Code

Claude Code 在 API 边界整理消息：

- `thirdparty/claude-code-sourcemap/services/api/claude.ts`
- `thirdparty/claude-code-sourcemap/utils/messages.ts`
- `normalizeMessagesForAPI` 和 `ensureToolResultPairing` 确保 tool_use /
  tool_result 配对。
- unresolved tool_use 会被过滤或配上合成错误结果。
- 工具输入 schema 校验失败会转成 tool_result，而不是让坏调用继续污染后续
  请求。

Wuu 的差异是内部 `ToolCall.Arguments` 存为 JSON 字符串，因此必须在持久化
前额外校验 JSON 字符串本身，避免恢复时重新解析失败。

### OpenCode

OpenCode 把通用历史不变量和 provider/model 兼容层分开：

- `thirdparty/opencode/packages/opencode/src/session/message-v2.ts`
- `thirdparty/opencode/packages/opencode/src/session/processor.ts`
- `thirdparty/opencode/packages/opencode/src/provider/transform.ts`
- pending/running 工具调用在重新发送给模型时会转成 interrupted 错误结果。
- provider transform 只处理 Anthropic、Mistral 等模型消息格式差异。
- `experimental_repairToolCall` 可以把不可识别的工具调用改写成 invalid 工具，
  让模型看到明确错误。

Wuu 采用这个架构分层：工具历史修复属于共享 runtime，ID scrub、Mistral
separator、文本清洗属于请求边界兼容层。

## 工程边界

代码边界保持在 core 内部，不依赖 Electron shell：

- 共享类型和修复逻辑在 `internal/providers`。
- agent loop 在新消息进入持久化历史前调用工具调用校验。
- app-server 在 resume/fork/turn 完成后修复并必要时重写历史。
- OpenAI Chat Completions、OpenAI Responses、Anthropic 都通过同一个
  `PrepareMessagesForModelRequest` 入口。

这意味着修复适用于所有 provider。新增 provider 时不应该重新实现工具历史
修复，只需要接入统一请求准备入口，并在必要时补充 model/provider 专用兼容
转换。

## 非目标

- 不把 provider 专用消息格式规则写进 agent loop。
- 不为了某个 provider 静默吞掉不可恢复的坏历史。
- 不把请求专用兼容转换写回持久化历史。
- 不把这次事故修成只针对 `update_plan` 的特殊分支。
