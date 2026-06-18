# 消息展示策略：reasoning、commentary、final

本文收敛当前关于桌面端消息流展示的产品约定。目标不是重写消息协议，而是把用户实际看到的内容规则定清楚：哪些内容默认展示，哪些内容默认收起，以及 phase 缺失时如何兜底。

## 产品目标

桌面端应该把一次 agent 回复拆成两层体验：

1. 用户正在等待时，需要看到有用的进展。
2. 最终答案出现后，默认只突出最终答案。

因此，运行中的 `commentary` 应该直接展示给用户，因为它通常是 agent 主动发出的进度说明、阶段说明或需要用户理解的上下文。`reasoning` 则默认收起，因为它可能很长、重复，并且不是用户完成任务所必需的内容。

最终答案出现后，前面的过程内容不应该从数据里删除，但默认应该折叠起来。用户可以通过“查看过程”之类的入口重新展开。

## 当前数据模型

后端给前端的线程项大致分为这些类型：

- `agent_message`：assistant 文本消息。
- `reasoning`：模型思考内容。
- `tool_call`：工具调用。
- `collab_agent_tool_call`：协作 agent 工具调用。
- `context_compaction`：上下文压缩事件。
- `error`：错误。
- `user_message`：用户消息。

其中 `agent_message` 可以带 `phase`：

- `commentary`：过程说明，应该在运行中展示。
- `final_answer`：最终答案，应该作为主要答案展示。
- 空或无法解析：未知 phase。

`reasoning` 本身不是 `agent_message phase=reasoning`，而是独立的线程项类型。也就是说，`reasoning` 和 `commentary/final_answer` 不在同一个维度上。

## phase 的来源

`phase` 来自上游 provider 的结构化输出，不是前端自己生成的，也不应该从自然语言文本里猜。

对支持 Codex 风格 phase 的 provider，后端会读取 provider 返回的 `phase`，并规范化成 `commentary` 或 `final_answer`。如果 provider 没给，或者给了无法识别的值，后端会把它当成未知。

对不支持结构化 phase 的 provider，后端只能根据事件和完成态做保守兜底。

结论：`phase` 是有用信号，但不是所有 provider 都稳定提供。前端和后端都不能假设它永远存在。

## phase 兜底规则

空 phase 和无法解析 phase 不需要区分。它们在产品语义上都表示“未知”。

推荐规则：

- 如果 provider 给出合法 `phase`，保留它。
- 如果 provider 给出空 phase，视为未知。
- 如果 provider 给出无法解析的 phase，视为未知。
- 不从文本内容推断 phase。

后端完成态兜底：

- 如果一条完成后的 assistant 消息带工具调用，则视为 `commentary`。
- 如果一条完成后的 assistant 消息不带工具调用，则视为 `final_answer`。
- 如果一段未知 phase 的 live 文本后面马上进入工具调用，则这段文本应落为 `commentary`。

前端展示兜底：

- 运行中的未知 `agent_message` 可以临时展示为 live 文本。
- 未知 `agent_message` 不能触发“最终答案已出现”的展示模式。
- 只有明确的 `final_answer` 才能触发最终答案主展示。

## 运行中展示

在 final 出现前，前端应按顺序展示用户真正需要看的进度。

推荐默认效果：

- `reasoning`：默认折叠，只显示“正在思考”或类似状态。
- `tool_call`：显示正在使用什么工具，保持现在的工具展示逻辑即可。
- `commentary`：不折叠，按顺序直接显示。
- 未知 live 文本：可以直接显示，但不把它标记成 final。

这允许出现交替结构：

```text
reasoning     -> 默认只显示“正在思考”
tool_call     -> 显示正在读取/执行的工具
commentary    -> 直接展示给用户
tool_call     -> 显示工具状态
commentary    -> 继续直接展示
```

这个阶段的重点是“可读进度”。不能因为 `reasoning` 很长，就把真正有用的 `commentary` 一起藏掉。

## final 出现后的展示

当明确的 `final_answer` 出现后，默认展示应切换成最终答案优先：

- 主区域显示 final 内容。
- final 之前的 reasoning、工具调用、commentary 默认折叠。
- 数据仍保留完整过程。
- 用户可以点开查看过程内容。

这和“删除过程内容”不同。删除会丢历史，折叠只是改变默认视图。

如果一个 turn 里有多个 final 片段，前端应把它们作为 answer 区域连续展示，而不是和过程内容混在一起。

## reasoning 的可见性

`reasoning` 的默认态应该是收起。

可见状态建议：

- 运行中：显示“正在思考”。
- 已完成且没有展开：显示简短入口，例如“查看思考过程”。
- 用户展开后：展示 reasoning 内容。

这样做的原因不是隐藏信息，而是避免冗长思考挤掉真正面向用户的进度和答案。

## 工具调用的可见性

工具调用不需要大改。

默认规则：

- 运行中显示正在用什么工具。
- 完成后可以显示已完成状态。
- final 出现后，工具调用进入可展开过程区。

工具调用属于过程信息，不应该在 final 出现后继续抢主答案的位置。

## 前端判定规则

前端可以按以下规则组织一个 assistant turn：

1. 把 `agent_message phase=final_answer` 放入 answer 区域。
2. 把 `agent_message phase=commentary` 放入 process 区域，并在 final 出现前默认展开。
3. 把 `reasoning` 放入 process 区域，但 reasoning 内容默认折叠。
4. 把工具调用放入 process 区域，显示工具状态。
5. 把空 phase 或无法解析 phase 都视为 unknown。
6. 对运行中的 unknown 文本，可以临时展示在当前输出里。
7. unknown 不触发 final-only 模式。
8. 一旦 answer 区域非空，process 区域默认折叠，但保留展开入口。

这套规则可以兼容有 phase 的 provider，也可以兼容没有 phase 的 provider。

## 不做的事

这次设计不要求：

- 改 provider 协议。
- 在前端通过文本内容猜测 phase。
- 删除 reasoning、commentary 或工具调用历史。
- 重做工具调用 UI。
- 把所有 provider 强行统一成完全相同的流式事件。

## 验证点

实现后至少需要验证这些场景：

- 只有 reasoning：默认只显示思考状态，内容可展开。
- reasoning 后接 commentary：commentary 在 final 前直接展示。
- commentary 和工具调用交替：顺序正确，工具状态可见。
- final 出现：默认只突出 final，前面的过程折叠但可展开。
- phase 为空：运行中可显示，但不触发 final-only。
- phase 无法解析：和空 phase 一样处理。
- provider 完成态无 phase、无工具调用：后端兜底为 final。
- provider 完成态无 phase、有工具调用：后端兜底为 commentary。
