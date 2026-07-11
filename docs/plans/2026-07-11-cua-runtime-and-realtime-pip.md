# Wuu CUA 运行时与实时画中画重构

日期：2026-07-11  
状态：设计定稿，分阶段实施  
范围：macOS CUA 原生运行时、Agent 工具协议、Activity 生命周期、Electron 实时画中画  

本文是 CUA 后续实现和验收的产品与技术约束。实施必须按里程碑做小步原子提交；每一步独立验证。现有行为若与本文的目标体验冲突，应修改实现，而不是保留错误模型。

## 0. 决策摘要

Wuu CUA 从“模型驱动的截图点击工具”改为“运行时驱动的本机应用控制系统”。

不可协商的产品原则：

1. 默认不抢用户当前前台、鼠标和键盘。
2. 画中画展示目标窗口的实时画面，不依赖模型调用 `observe`。
3. 用户监控流与模型观察快照是两条独立数据通道。
4. AX、截图几何、焦点、等待、动作验证、重试和降级由运行时负责，不交给模型猜。
5. 优先使用公开、可发布的 macOS 能力；参考产品中的私有接口只作为问题分解证据。
6. 无法后台完成时必须进入显式的前台控制状态，不能悄悄抢焦点。
7. 同一应用内动作串行；不同应用在不争用真实前台输入时可以并行。

目标不是让 MiniMax 学会像强视觉模型一样使用复杂 CUA，而是让能力较弱的模型也能通过确定性的运行时完成任务。

## 1. 用户问题

### 1.1 当前体验

- 操作计算器时，Wuu 会把计算器切到前台，打断用户正在使用的软件。
- 坐标操作曾要求模型理解 Retina、截图像素和屏幕坐标，容易点偏并循环。
- 微信等弱 AX 应用中，AX 树可能不变化，但画面已经变化；当前等待和验证无法可靠处理。
- 画中画只显示最近一次工具结果中的截图。两次 `observe` 之间画面冻结，不是真实监控。
- 多个 CUA 会话若同时需要鼠标和键盘，会争用同一套全局输入。
- 弱模型需要自己决定观察、等待、点击、验证和恢复，消耗大量上下文，且容易陷入重复工具调用。

### 1.2 目标体验

用户可以让多个 Agent 同时操作不同应用，并继续使用自己的电脑：

- AX 完整的应用在后台完成语义操作。
- 画中画持续显示每个受控窗口的实时状态。
- 用户当前前台应用保持不变，真实鼠标不跳动。
- 必须使用真实前台输入时，Wuu 明确进入前台控制状态，并在画中画中可见。
- Agent 每次得到的是可解释的动作结果和最小状态变化，不需要从旧截图推断成功与否。

## 2. 证据、推断与未知

### 2.1 已确认的 Wuu 事实

- `MacComputerBackend.click` 对 AXPress 和坐标点击都会调用 `activate(app)`。
- `press_key`、`press_keys`、`type_text` 使用全局原生输入前会激活目标应用。
- `observe` 同时生成 AX 树和单帧窗口截图。
- Activity 只在 MCP 工具结果包含图片时持久化 `preview.png`。
- Electron 画中画重新加载这个静态文件；没有连续捕获流。
- CUA 已支持 `normalized`、`screenshot`、`screen` 三种坐标空间，但动作策略仍主要由模型选择。

### 2.2 已确认的本机 Sky 行为

通过本机包、二进制符号和无副作用动态测试确认：

- Sky 运行在本机原生 `SkyComputerUseService` 中，通过 XPC/JSON-RPC 提供能力。
- 它同时具备 AX element action 与物理输入模拟路径。
- ChatGPT 保持系统前台时，Sky 能对后台计算器执行 AX 点击。
- ChatGPT 保持系统前台时，Sky 能向后台计算器发送按键，计算器状态实际变化。
- 动作后的状态可以返回 AX diff，而不是完整重复树。
- 二进制包含前台应用追踪、焦点保护、合成焦点、UI settle、窗口捕获和 remote-hosted PiP 相关组件。

### 2.3 合理推断

- Sky 将“系统真实前台”和“目标应用接收合成输入所需的活动状态”分开建模。
- Sky 的实时 PiP 与控制服务共享目标窗口生命周期，而不是读取模型工具截图。
- AX 通知、debounce 和 settle 共同减少了模型侧盲等与重复观察。

### 2.4 尚未确认

- Sky 每一种动作具体经过哪个内部焦点类。
- 物理点击策略开关的默认值和远程配置。
- 普通状态截图与 remote-hosted PiP 是否完全共享同一捕获管线。
- 私有 `SAI*` 和 remote layer 接口的可发布性。

这些未知不能成为 Wuu 引入私有 API 或声称完全复制 Sky 的依据。

## 3. 产品模型

### 3.1 控制等级

每个动作由运行时选择最低干扰等级：

| 等级 | 名称 | 实现 | 是否允许抢前台 |
|---|---|---|---|
| 0 | 专用接口 | 应用 API、插件、浏览器协议 | 否 |
| 1 | 后台 AX | AXPress、AXSetValue、AX action、选择范围 | 否 |
| 2 | 后台定向输入 | 公开能力允许时向目标应用定向发送输入，并保护用户前台 | 否 |
| 3 | 前台原生输入 | 激活目标应用后使用 CGEvent 鼠标/键盘 | 是，必须显式进入该状态 |
| 4 | 隔离桌面 | 独立 macOS 会话、虚拟机或未来隔离环境 | 不影响用户桌面 |

运行时不得为了减少工具调用次数而从等级 1 跳到等级 3。例如计算器应优先后台按 AX 按钮，而不是先激活应用再批量发送按键。

### 3.2 会话状态

CUA Activity 使用以下用户可理解状态：

- `starting`：正在解析目标应用和窗口，PiP 显示液态玻璃。
- `background_controlled`：Agent 在后台控制，用户前台不受影响。
- `foreground_controlled`：Agent 暂时占用前台输入。
- `user_controlled`：用户接管目标应用，Agent 停止发送动作但监控继续。
- `recovering`：窗口、元素或捕获流失效，运行时正在重新绑定。
- `error`：无法恢复，显示简短可行动错误。
- `stopped`：控制与捕获生命周期结束。

现有 `active` 可以在迁移期映射到 `background_controlled`，但最终协议应使用明确状态，不长期保留含糊状态。

### 3.3 并发规则

- 每个目标应用 PID 有一条串行动作队列。
- 后台 AX 操作可以跨不同 PID 并发。
- 前台原生输入使用全局独占锁。
- 同一目标的观察快照与动作按 revision 关联，旧 revision 的元素不得静默执行。
- 用户在目标应用产生真实输入时，可在安全停止点抢占 Agent 动作队列。
- 实时监控流不占用动作队列，也不触发模型 turn。

## 4. 运行时架构

```text
用户请求
  -> Agent 选择目标和高层动作
  -> CUA Session Coordinator
       -> Target Resolver（app / pid / window）
       -> Action Planner（专用接口 / AX / 后台输入 / 前台降级）
       -> Focus & Input Controller
       -> UI Settle Observer
       -> State Differ（AX diff + visual revision）
       -> Window Capture Stream
            -> Model Snapshot（按需单帧、有界时间一致性）
            -> User PiP（持续低延迟帧）
  -> Verified Action Result
  -> Agent 决定下一项高层动作
```

### 4.1 Target Resolver

目标身份至少包含：

```text
bundle_id
process_id
window_id
window_frame
display_id
capture_revision
ax_revision
```

窗口标题不是稳定身份。应用重启、窗口切换和多窗口场景必须更新 target revision。

### 4.2 Action Planner

模型提交目标与动作，运行时决定执行机制：

```json
{
  "target": { "app": "com.apple.calculator" },
  "action": "activate_control",
  "element_id": 6,
  "foreground_policy": "avoid"
}
```

`foreground_policy`：

- `avoid`：默认。只允许等级 0–2，无法完成则返回需要前台。
- `allow`：用户任务允许前台降级。
- `require`：只用于用户明确要求展示或接管目标应用。

弱模型不需要指定 AXPress 或 CGEvent。运行时根据元素能力和目标状态选择。

### 4.3 Focus & Input Controller

首阶段只使用公开 API：

- AX action 不主动调用 `NSRunningApplication.activate`。
- AX settable value、selection、secondary action 在后台执行。
- 真实 CGEvent 仍视为前台输入，不伪装成后台安全操作。
- 后台定向键盘输入需要独立实验和兼容矩阵；未验证应用不得默认启用。
- 每次可能改变真实前台的动作，执行前后记录 frontmost PID，并验证策略是否满足。
- 监听前台应用变化和真实鼠标/键盘事件；排除 Wuu 自己合成的事件。
- 用户将目标应用切到前台或在目标窗口产生真实输入时，将会话切到 `user_controlled`。
- 正在执行的 sequence 只在当前不可分割动作完成后停止，不启动下一步；未执行步骤保留为部分完成结果。
- 用户明确“交还 Agent”后重新观察目标状态、建立新 revision，再恢复队列，不能沿用接管前元素 ID。

Sky 的合成焦点实现作为研究方向，但不直接复制私有接口。

### 4.4 UI Settle Observer

动作完成不等于 UI 已稳定。运行时组合以下信号：

- AX value、focused element、window、layout 通知。
- 目标元素或相关区域的属性变化。
- 窗口帧稳定。
- 视觉帧哈希或差分稳定。
- 最大等待上限。

不要让模型反复调用相同 `observe` 来等待。动作结果应返回：

```json
{
  "status": "verified",
  "mechanism": "background_ax",
  "foreground_changed": false,
  "ax_revision": 18,
  "visual_revision": 42,
  "changes": ["display: 0 -> 42"]
}
```

若没有可验证变化，返回 `unverified`，而不是谎报成功。

### 4.5 State Differ

- 首次观察返回完整 AX 树。
- 后续默认返回相对上次模型可见 revision 的 diff。
- diff 过大或基线缺失时返回完整树。
- 元素 ID 绑定 revision；失效时运行时可使用稳定属性重新定位一次。
- 多个候选匹配时失败并要求新观察，不能猜。
- 弱 AX 应用允许 AX diff 为空但 visual revision 变化。

## 5. 实时画中画

### 5.1 数据源

PiP 必须消费目标窗口的持续捕获流，不再消费 Activity 的 `preview.png`。

macOS 第一版使用 ScreenCaptureKit `SCStream`：

- 绑定 window ID。
- 目标 8–15 FPS，按窗口可见变化自适应降帧。
- PiP 分辨率按显示尺寸和设备 scale 计算，不固定使用原始 Retina 尺寸。
- 不包含系统鼠标指针；需要时渲染独立虚拟指针。
- 窗口移动、缩放、显示器切换时保持流并更新几何。
- 应用退出或窗口销毁时进入 `recovering`，尝试按 PID/bundle/window 规则重新绑定。

### 5.2 两条图像通道

**用户监控流**：

- 持续、低延迟、可丢帧。
- 不进入模型上下文。
- PiP 关闭不停止 Agent。

**模型观察快照**：

- 按需、带精确几何和 revision。
- 在 UI settle 后采集 AX 与帧，分别记录时间戳和 revision。
- 两者不是系统级原子事务。采集时间偏差超过经过实验确定的上限时，重新采集一次或标记 `inconsistent`，不能声称一致。
- 允许为 provider 预处理尺寸，但工具坐标必须引用模型实际收到的图像。

不得将实时视频帧持续发送给模型。

### 5.3 帧传输协议

实时帧不得走现有 MCP JSON-RPC/stdin-stdout 控制通道，也不得以 8–15 FPS 持续发送 base64 JSON。控制消息和视频帧必须分离：

- MCP/Activity 通道只传 stream endpoint、目标身份、geometry、revision、状态和错误。
- 原生捕获服务为每个 Activity 建立本地二进制帧通道。首选 Unix domain socket；若后续验证共享 IOSurface/共享内存能在签名和发布约束下稳定工作，可替换传输实现而不改变上层协议。
- 帧头至少包含 activity ID、capture revision、frame sequence、时间戳、宽高、像素/编码格式和 payload 长度。
- 消费端只保留最新完整帧。旧帧可丢弃，禁止无界排队。
- 写端遇到背压时覆盖待发送旧帧或降低提交频率，不能阻塞 CUA 控制请求。
- endpoint 仅允许当前本机用户访问，Activity 停止后立即关闭并清理。
- Electron 断开时捕获服务暂停或降帧；PiP 重新打开后可重新订阅。

M3 在实现 SCStream 前必须先落地并测试该帧通道；M4 只能消费该通道，不能从 `preview.png` 轮询。

### 5.4 PiP 交互

- `starting`：只显示液态玻璃，不显示冗余文字。
- 有首帧后立即切换为实时画面。
- 默认吸附主对话区域四角；主窗口不可用时吸附当前屏幕四角。
- 拖动期间跟手，释放后使用连续动画吸附。
- 用户接管、交还和停止控制保持悬浮操作，默认隐藏。
- 错误只显示一条简短可恢复信息。
- 窗口尺寸保持目标窗口纵横比，并在用户手动缩放后记住偏好。

### 5.5 生命周期

```text
Activity acquire
  -> resolve target window
  -> start capture stream
  -> first frame -> show PiP
  -> actions and observations reuse target binding
  -> target change -> rebind stream
  -> user closes PiP -> stream may pause, activity continues
  -> activity stop -> stop stream and release resources
```

## 6. 弱模型友好协议

### 6.1 原则

弱模型最容易失败的部分必须确定化：

- 不要求理解 Retina scale。
- 不要求判断当前截图是否过期。
- 不要求猜 wait 时间。
- 不要求知道 AXPress 与物理点击的差异。
- 不要求重复整棵 AX 树。
- 不要求在动作失败后无限尝试附近坐标。

### 6.2 高层批处理

在保留低层工具用于恢复的同时，增加顺序动作批处理：

```json
{
  "action": "sequence",
  "app": "com.apple.calculator",
  "foreground_policy": "avoid",
  "steps": [
    { "press_element": { "label": "全部清除" } },
    { "press_element": { "label": "3" } },
    { "press_element": { "label": "7" } },
    { "press_element": { "label": "乘" } },
    { "press_element": { "label": "2" } },
    { "press_element": { "label": "4" } },
    { "press_element": { "label": "等于" } }
  ],
  "verify": { "text": "888" }
}
```

运行时串行执行、在必要节点 settle、失败即停，并返回完成到哪一步。模型不应自己并行发出有顺序依赖的 UI 动作。

每个 step 在执行前必须独立经过与单次工具调用相同的风险分类和确认策略。sequence 不是绕过确认的容器：

- 遇到发送消息、删除、付款或其他需要确认的 step，sequence 在该 step 之前暂停。
- 用户确认后只授权该具体 step，不默认授权剩余步骤。
- 拒绝或超时时返回 `policy_paused` / `policy_denied` 和已完成步骤，不执行后续动作。
- 不可逆 step 不能与前一步合并成无法中断的原生批处理。

## 7. 权限与安全边界

- Screen Recording 和 Accessibility 权限仍由 macOS 管理。
- PiP 只对当前 CUA Activity 的目标窗口捕获。
- 实时帧默认只留在本机内存，不写入 session 历史。
- 模型观察快照遵循现有 provider 数据路径和隐私说明。
- 接管、发送消息、删除、付款等风险动作继续使用现有确认策略。
- 前台控制状态必须用户可见，但不增加每个普通动作的确认弹窗。

## 8. 实施计划

### M0：文档与基线（已完成）

- 本文档。
- 固化计算器、TextEdit、微信的行为基线。
- 记录每项测试的前台 PID、鼠标位置、AX revision、视觉 revision 和结果。

验收：文档可直接指导实现；证据与推断分开。

### M1：后台 AX（已完成）

- AXPress、AXSetValue、select、secondary action 不激活应用。
- 动作结果增加执行机制与前台是否变化。
- 计算器完整 AX 按钮序列在后台完成。
- 前台应用和真实鼠标保持不变。

验收：ChatGPT/Wuu 保持前台时，后台计算器完成运算并清零。

实施证据（2026-07-11）：后台计算器通过 8 次 AXPress 完成 `37 × 24 = 888` 并清零；每次动作均报告 `mechanism=background_ax` 和 `foreground_changed=false`。操作前后 frontmost bundle 均为 `com.openai.codex`，鼠标坐标均为 `(911, 804)`，最终 AX 值为 `0`。TextEdit 打开对话框的搜索字段完成后台 AXSetValue、select_text、AXConfirm 和清空，值变化得到 AX 验证；全程 frontmost bundle 为 `com.openai.codex`，鼠标坐标保持 `(508, 794)`。

### M2：前台控制策略

- 引入 `foreground_policy`。
- 坐标、键盘、输入和滚动按能力选择后台或前台路径。
- 前台输入使用全局锁。
- Activity 显示 `background_controlled` / `foreground_controlled`。
- 监听用户真实前台/输入，支持 `user_controlled` 抢占、sequence 安全停止点和交还后的新 revision。

验收：无法后台执行时返回清晰降级状态；不再悄悄切前台；用户在动作序列中接管目标应用后，Agent 不再启动下一步。

### M3：原生实时捕获流

- 先实现与 MCP 控制面分离的本地二进制帧通道、最新帧背压和生命周期。
- 在 `cua-mac` 原生层增加 window stream 生命周期。
- 输出低延迟帧及 geometry/revision 元数据。
- 捕获失败、窗口销毁和重绑定有明确事件。

验收：独立探针持续捕获计算器窗口，操作变化无需 `observe` 即出现在帧流中。

### M4：实时 PiP

- Electron 创建/销毁 capture stream。
- PiP 消费实时帧，不再读取静态 preview。
- 保留液态玻璃首帧状态和现有吸附交互。
- 关闭 PiP 与停止 Activity 解耦。

验收：Agent 两次工具调用之间，PiP 仍实时显示目标应用变化。

### M5：settle、diff 与元素恢复

- AX notification + debounce。
- visual revision 和稳定判定。
- AX diff 与过大回退。
- stale element 单次确定性重新定位。

验收：计算器只返回相关值变化；微信 AX 不变但视觉变化时不会误报无变化。

### M6：弱模型动作协议

- sequence 高层动作。
- 运行时验证与部分完成结果。
- 每个嵌套 step 独立经过风险策略，支持 `policy_paused`。
- 更新 CUA skill 和工具描述。
- 增加 MiniMax 真实 `wuu exec --json` eval。

验收：弱模型不需要自己处理 Retina、重复观察和逐步等待，也能稳定完成代表任务。

### M7：多应用并发

- per-PID 动作队列。
- 全局前台输入锁。
- 多 PiP 流资源预算。
- 后台并发与前台串行调度。

验收：计算器与 TextEdit 后台并行；微信需要前台时不会与其他真实输入冲突。

## 9. 测试矩阵

| 应用 | AX | 任务 | 预期控制等级 | 验证 |
|---|---|---|---|---|
| 计算器 | 强 | 37×24、确认、清零 | 后台 AX | 值变化、前台 PID 不变、鼠标不动 |
| TextEdit | 强 | 临时文档输入、读取、清空 | 后台 AX/定向输入 | 文本值、无持久文件、前台不变 |
| 微信 | 弱 | 搜索并打开文件传输助手，不发送 | 视觉/前台降级 | 标题截图、返回列表、无消息发送 |
| Finder | 中 | 打开目录并返回 | 后台 AX 优先 | 窗口路径与 history |
| 多应用 | 混合 | 计算器 + TextEdit + 微信 | 后台并发、前台串行 | 无交叉输入、各自 PiP 正确 |
| 接管 | 混合 | sequence 中途用户操作目标窗口 | 用户抢占 | 当前动作后停止、后续未执行、交还后新 revision |
| 风险批处理 | 任意 | 普通步骤后包含发送/删除步骤 | 策略暂停 | 危险 step 前暂停，未确认不执行后续 |

每项记录：

- 初始和结束 frontmost PID。
- 初始和结束鼠标坐标。
- 动作机制。
- 是否发生真实前台切换。
- AX/视觉 revision。
- PiP 首帧延迟、帧率、CPU 和内存。
- 是否由运行时验证完成。
- AX 与视觉采集时间差及一致性状态。

## 10. 性能预算

首版目标，不作为未经测量的永久常量：

- PiP 首帧：目标 < 500 ms。
- 活跃 PiP：8–15 FPS。
- 静止窗口：自适应降到 1–2 FPS 或仅变化时提交。
- 单个 PiP 捕获 CPU：目标平均 < 5%。
- 同时 3 个 PiP 时不影响输入响应。
- 模型观察只发送最新快照，不发送监控流历史。

每个数字必须通过真实设备测量后调整，不能只为了通过测试写死。

## 11. 非目标

- 不复制或依赖 Sky 的私有系统接口。
- 不承诺所有 macOS 应用都能完全后台控制。
- 不把视频流持续发给模型。
- 不用高频 `observe` 轮询伪造实时 PiP。
- 不在第一阶段实现虚拟机或独立 macOS 登录会话。
- 不为了保留当前静态 preview 模型而增加双写兼容层；实时流稳定后删除旧主路径。

## 12. 风险与降级

### 后台输入兼容性

部分应用只有真实前台输入才能工作。运行时记录应用能力，不将一次成功泛化为全局保证。失败时返回 `requires_foreground`。

### ScreenCaptureKit 资源占用

多流可能消耗 GPU/内存。采用按 PiP 尺寸捕获、自适应帧率、不可见时暂停，以及会话级资源上限。

### 窗口身份变化

应用重启或创建新窗口会使 window ID 失效。用 bundle/PID/窗口特征重新解析，并增加 revision，避免旧坐标落到新窗口。

### 私有接口诱惑

Sky 的焦点和 remote layer 能力可能依赖私有接口。Wuu 只复制语义和用户体验，不复制不可发布实现。公开能力做不到的部分如实降级。

## 13. 完成定义

本重构只有同时满足以下条件才算完成：

- AX 完整应用默认后台完成，不抢用户前台和鼠标。
- PiP 在无 `observe` 调用时仍持续更新。
- 前台降级是显式状态并受全局调度。
- 模型工具结果包含运行时验证，而非仅动作 ack。
- 弱模型代表任务达到可重复成功，而不是偶然跑通一次。
- 微信等弱 AX 应用失败时可解释、可恢复，不进入无限循环。
- 多应用会话不会发生交叉输入或 PiP 串台。
- 文档中的实施状态随每个原子提交更新。
