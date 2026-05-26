# Wuu 双端产品方向与 Wuu Browser 迁移计划

## 一句话方向

Wuu 要做的不是“浏览器里塞一个聊天机器人”，也不是用浏览器替代桌面端，而是把同一个 Wuu 本机 coding agent 同时运行在 Electron Desktop 和 Chromium Browser 两个产品外壳里。

Wuu Desktop 继续承担稳定的本地 coding agent 体验。Wuu Browser 则成为一个可安装的 Chromium 浏览器，让用户一边正常上网、查文档、登录网页、调试本地应用，一边让 Wuu 读项目、改代码、跑命令、看页面、操作标签页，并把这些动作组织成一个完整的开发工作流。

## 我们为什么要做这个

现有 coding agent 通常有一个断点：它能改本地代码，但离真实浏览器太远。

开发者实际工作时，经常需要在这些场景之间来回切：

- 看文档、GitHub、Stack Overflow、API 控制台。
- 打开本地开发页面，检查 UI、console、network、截图。
- 让 agent 修改代码、跑测试、修 bug。
- 回到浏览器确认行为是否真的变好。

如果 Wuu 只有桌面端，它可以写代码，但浏览器仍然像外部世界。如果 Wuu 只有浏览器端，又会丢掉当前桌面端已经形成的稳定本地工作流。正确做法是保留双端：让桌面端继续服务本地 coding，让浏览器端把真实网页纳入 Wuu 的工作环境。

## 产品判断

正确路线是：

**Wuu = 一个 Wuu core + 两个产品外壳**

两个产品外壳是：

- **Wuu Desktop**：Electron shell，面向稳定的本地 coding agent 工作流。
- **Wuu Browser**：Chromium shell，面向带真实网页操作和网页验证能力的 coding agent 工作流。

Wuu Browser 的组成是：

**Chromium 浏览器 + 浏览器操作桥 + Wuu 本机 runtime + Wuu workbench**

不应该走的路线：

- 不做纯网页 coding agent，因为纯网页拿不到完整本地开发能力。
- 不把浏览器端做成 Electron 桌面端的替代品；两端应该长期共存。
- 不继续保留 BrowserOS 原本的 AI 产品层，否则产品里会同时存在两套 agent 心智。
- 不把 Chrome 扩展当最终主产品，因为扩展权限、入口和体验都不够完整。

## 双端产品边界

双端共存的前提是：**一套 Wuu core，两个 shell adapter**。

Wuu core 负责：

- agent loop。
- 项目、会话、历史和恢复。
- 文件读写、diff、Git、终端、进程。
- 工具调用、权限提示、错误解释。
- 模型/provider 设置。
- 子 agent 和任务活动。

Electron adapter 负责：

- 桌面窗口、菜单、托盘、文件选择器。
- 桌面端本地 API 和系统集成。
- 当前 Wuu Desktop 的稳定本地工作流。

Browser adapter 负责：

- `chrome://wuu` 产品入口。
- 浏览器 tab、地址栏上下文和当前页面信息。
- Browser Bridge，包括截图、DOM、console、network、点击、输入、滚动和导航。
- Chromium 与本机 runtime 的连接。

两端可以有不同的外壳能力，但不能有两套 AI 大脑。Electron 端和 Browser 端都应该调用同一个 Wuu runtime protocol。

## BrowserOS 里应该保留什么

BrowserOS 的价值不是它自己的 AI 聊天界面，而是它已经做了一部分难的浏览器底座。

应该保留：

- Chromium 产品壳。
- 标签页、地址栏、下载、设置、扩展、DevTools 等正常浏览器能力。
- 浏览器启动本地 server 的机制。
- `chrome://wuu` 这种浏览器内产品入口。
- `chrome.browserOS.*` 这类浏览器原生能力接口。
- CDP/browser bridge 能力，包括读取 tab、截图、DOM、console、network、点击、输入、滚动、导航。
- 本地 server 和 Chromium 之间的连接机制。
- 构建、打包、启动、验证浏览器的基础流程。

这些东西是浏览器能力层。它们解决的是“Wuu 怎么进入浏览器、怎么控制真实网页”的问题。

## BrowserOS 里应该砍掉什么

应该砍掉 BrowserOS 自己的 AI 产品层，让 Wuu 接管 agent 大脑。

应该移除或下线：

- BrowserOS 原本的新标签页 AI chat 主体验。
- BrowserOS 自己的 side panel agent 产品逻辑。
- BrowserOS 的 AI SDK agent loop。
- BrowserOS 自己的工具注册和 agent 会话模型。
- OpenClaw / Hermes / Lima / VM 作为默认工作路径。
- BrowserOS 自己的记忆、agent 设置、AI provider 设置。
- 任何会让用户误以为“有两个 AI 助手”的入口。

不是所有代码都要立刻删除。早期可以先隐藏、绕开、停用。最终产品里，用户应该只感知到一个主 agent：Wuu。

## Wuu 应该接管什么

Wuu 负责所有 coding agent 的核心体验。

Wuu 应该负责：

- 项目选择和项目切换。
- 会话、线程、历史、恢复。
- 本地文件读取、编辑、diff、Git 操作。
- 终端、进程、开发服务器。
- 模型/provider 设置。
- 工具调用、工具活动、子 agent 活动。
- 用户确认、权限提示、错误解释。
- coding 任务的完整闭环：理解目标、改代码、跑验证、看结果、继续修。

浏览器能力应该作为 Wuu 的工具，而不是另起一套 BrowserOS agent。桌面端和浏览器端都应该复用同一套 Wuu agent 能力，只是在不同 shell 里暴露不同工具。

## 最终用户体验

用户打开 Wuu Desktop 后，看到的是熟悉的本地 Wuu workbench，可以继续选择项目、写代码、跑命令、管理会话。

用户打开 Wuu Browser 后，默认看到浏览器里的 Wuu workbench。

在 Wuu Browser 里，用户可以：

- 像普通浏览器一样打开任意网页。
- 在 Wuu workbench 里选择本地项目。
- 让 Wuu 修改项目代码。
- 让 Wuu 打开本地 dev server 页面。
- 让 Wuu 查看页面截图、DOM、console 和 network。
- 让 Wuu 点击页面、输入表单、切换标签页、刷新页面。
- 让 Wuu 根据真实页面结果继续改代码。
- 在敏感操作前看到清楚的权限提示。

用户不应该需要理解 BrowserOS、OpenClaw、Hermes、Lima、CDP、MCP 这些内部概念。

用户只需要理解一件事：

**Wuu 是一个能写代码、能看浏览器、能操作浏览器的本地开发助手。**

## 产品原则

### 一个 Wuu core，两个产品外壳

Wuu Desktop 和 Wuu Browser 应该共享同一套 Wuu core。

共享的是 agent、项目、会话、工具、权限、终端、Git 和模型设置。分开的只是 shell adapter：Electron adapter 处理桌面能力，Browser adapter 处理浏览器能力。

如果两个端各自实现 agent loop、设置、工具注册和会话模型，产品会很快分叉，后续维护成本也会失控。

### 一个主助手

产品里只能有一个 AI 主体：Wuu。

BrowserOS 的 AI 入口、BrowserOS 的 agent 设置、BrowserOS 的聊天记录都不应该和 Wuu 并存。并存会让用户困惑，也会让权限、日志、错误处理变复杂。

### 本机 runtime 是默认路径

默认开发体验必须跑在用户本机，不依赖 VM 或远程沙箱。

本机 runtime 负责读写文件、跑 Git、跑测试、启动终端和开发服务器。VM 或容器以后可以作为可选隔离能力，但不能成为默认启动路径。

### 浏览器能力是工具，不是第二个产品

tab、截图、DOM、console、network、点击、输入、滚动，都应该作为 Wuu 的工具能力出现。

用户不需要看到“Browser Bridge”这个概念。用户看到的是 Wuu 能够检查网页、操作网页、验证代码效果。

### 权限必须可见

Wuu 会同时拥有本地代码能力和浏览器操作能力，这比普通网页或普通扩展权限更高。

因此产品必须让用户知道：

- Wuu 当前选中了哪个项目。
- Wuu 正在操作哪个 tab。
- Wuu 将要执行什么高风险动作。
- Wuu 是否会修改文件、提交代码、打开网页、点击按钮、输入内容。

高风险动作不能偷偷发生。

### 保留真实浏览器体验

Wuu Browser 首先仍然应该是一个好用的浏览器。

不能为了 agent 牺牲基本浏览体验：标签页、地址栏、下载、扩展、登录态、DevTools、性能和启动速度都要保留。

## 阶段计划

### 阶段 1：统一产品心智

目标：Electron 和 Browser 两端都只表达 Wuu，不再让用户看到 BrowserOS 的 AI 产品层。

要做：

- 默认入口固定为 Wuu workbench。
- 隐藏或移除 BrowserOS 原来的 AI chat、新标签 AI、side panel agent 入口。
- 保留浏览器基础功能和浏览器 bridge。
- 保留 BrowserOS 能力层里必要的设置和权限能力。
- 文案、品牌、设置页逐步替换为 Wuu Browser。
- 明确 Wuu Desktop 不是被替代的旧产品，而是长期共存的桌面 shell。

验收：

- 新用户打开产品时，不会看到两个 AI 助手。
- 用户不会被要求理解 BrowserOS agent、OpenClaw、Hermes 或 Lima。
- 默认工作流从选择项目开始，而不是从 BrowserOS AI chat 开始。
- 桌面端和浏览器端都指向同一个 Wuu agent 心智。

### 阶段 2：让 Wuu 控制浏览器

目标：Wuu agent 可以把浏览器当作自己的工具使用。

要做：

- 把 tab 列表、当前 tab、页面截图、DOM、console、network 暴露给 Wuu agent。
- 让 Wuu 可以打开、激活、导航、刷新、关闭 tab。
- 让 Wuu 可以点击、输入、滚动页面。
- 在 Wuu UI 中展示浏览器操作活动。
- 对敏感页面和高风险操作加权限提示。

验收：

- 用户可以让 Wuu 打开本地开发页面并检查 UI。
- 用户可以让 Wuu 读取 console/network 错误并修代码。
- 用户可以让 Wuu 修改代码后刷新页面验证结果。
- 用户能看懂 Wuu 对浏览器做了什么。

### 阶段 3：形成开发闭环

目标：Wuu 能在双端完成真实 coding 任务，而不是只做局部网页自动化。

要做：

- 把代码修改、测试、终端、浏览器验证串成一个自然流程。
- 支持 Wuu 在一个任务里多次修改、运行、查看页面、继续修。
- 支持把浏览器证据放进任务上下文，例如截图、DOM 片段、console 错误。
- 支持本地 dev server 的启动、检测和复用。
- 做一套稳定的任务完成判断：代码是否改了、测试是否跑了、页面是否验证了。
- 确保 Wuu Desktop 可以继续完成纯本地任务，Wuu Browser 可以额外完成网页验证任务。

验收：

- 用户可以说“修一下这个页面的布局问题”，Wuu 能改代码并打开页面确认。
- 用户可以说“这个登录流程报错，帮我查”，Wuu 能看 network/console 并定位代码。
- 用户可以说“实现这个前端交互”，Wuu 能完成代码和浏览器验证。

### 阶段 4：收紧安全和发布边界

目标：产品可以稳定给真实用户使用。

要做：

- 本地 server 默认只接受安全来源。
- 高风险本地命令和真实网页操作有明确权限策略。
- 用户能查看最近的文件修改、命令执行、网页操作记录。
- 生产构建隐藏所有开发调试入口。
- 清理 BrowserOS 残留品牌、残留 AI 设置和无用菜单。
- 明确许可证和发布策略。
- 明确 Wuu Desktop 和 Wuu Browser 的发布节奏、设置迁移和兼容边界。

验收：

- 外部网页不能随便调用 Wuu 本地能力。
- 用户能知道 Wuu 是否正在操作浏览器或本地项目。
- 生产包里没有 BrowserOS AI 产品残留。
- 产品名、设置、帮助、日志入口都围绕 Wuu Browser。

## 明确不做

短期不做：

- 不做一个新的通用 AI 浏览器。
- 不继续维护 BrowserOS 自己的 agent 产品。
- 不砍掉 Wuu Desktop。
- 不把 Wuu Browser 当成 Wuu Desktop 的替代品。
- 不把 VM 当默认开发环境。
- 不把 Wuu 做成普通 Chrome 扩展。
- 不优先做大量第三方 app 连接。
- 不为了保留 BrowserOS 现有功能而牺牲 Wuu 的 coding 主线。

这些可以以后再评估，但当前目标应该集中在“浏览器里的本地 coding agent”。

更准确地说，当前目标不是把所有体验都迁到浏览器里，而是让 Wuu Browser 成为 Wuu 双端体系里的浏览器 shell。

## 主要风险

### 安全风险

Wuu 同时能操作本地文件和浏览器页面，权限比普通网页大得多。

如果本地 server 暴露过宽，或者网页能绕过授权调用能力，就会变成严重问题。安全边界必须尽早收紧。

### 产品混乱风险

如果 BrowserOS AI 和 Wuu AI 同时存在，用户会不知道该用哪个。更严重的是，开发团队也会维护两套 agent 逻辑。

必须尽早把 BrowserOS AI 产品层降级为内部能力或移除。

### 双端分叉风险

如果 Electron 端和 Browser 端各自实现一套 agent、设置、工具和权限逻辑，短期看起来进展快，长期会变成两个产品。

必须把共同能力放进 Wuu core，把端差异限制在 adapter 层。两端 UI 可以不同，但任务模型、权限模型和工具语义应该一致。

### 迁移成本风险

BrowserOS 代码量大，里面有很多功能和 Wuu 目标无关。不能无差别继承，否则会让产品变重、设置变乱、构建变复杂。

迁移时应该按产品价值保留能力，而不是按代码现状保留功能。

### 发布和许可证风险

BrowserOS 相关代码使用 AGPL。Wuu 根项目现在标的是 MIT。最终怎么发布、哪些代码属于哪个许可证，需要单独确认，不能到发布前才处理。

当前边界记录在 [browser/BROWSEROS_SOURCE.md](/Users/blueberrycongee/wuu/browser/BROWSEROS_SOURCE.md)：Wuu core 仍按根项目许可证判断；包含 BrowserOS 派生代码的 Wuu Browser 发布物，不能按 MIT-only 发布。

## 成功标准

这个方向成功，不是因为我们做了一个“AI 浏览器”，而是因为它让开发者少切换上下文。

短期成功标准：

- 用户打开浏览器后，第一入口就是 Wuu workbench。
- 用户能选择本地项目并让 Wuu 正常开始 coding 任务。
- Wuu 能读取和操作真实浏览器 tab。
- Wuu 能用浏览器结果继续修本地代码。
- Wuu Desktop 的现有本地 coding agent 路径继续成立。

中期成功标准：

- 常见前端任务可以在 Wuu Browser 里完成：改代码、跑服务、看页面、读错误、再修改。
- 常见本地 coding 任务可以继续在 Wuu Desktop 里完成。
- 用户不需要知道 BrowserOS 的存在。
- 用户对 Wuu 的浏览器操作有清楚的可见性和控制感。
- 两端共享同一个 Wuu core，不出现两套 agent 心智。

长期成功标准：

- Wuu Desktop 成为稳定的本地 coding agent。
- Wuu Browser 成为开发者写代码和调试网页时的浏览器工作环境。
- 浏览器不是外部工具，而是 Wuu agent 的一个 shell 和工具集合。

## 下一步建议

接下来应该先做一轮产品收敛，而不是继续扩散功能：

1. 明确 Wuu core、Electron adapter、Browser adapter 的产品边界。
2. 清点 BrowserOS AI 产品入口，决定隐藏、删除或替换。
3. 明确 Wuu Browser 的默认导航结构，只保留 Wuu 主体验。
4. 把 Browser Bridge 作为 Wuu agent 的工具接入。
5. 收紧本地 server 的安全边界。
6. 做一个端到端样例：Wuu 修改前端代码，然后用浏览器验证页面结果。

这六件事完成后，Wuu Browser 的产品方向就会从“迁移中的浏览器项目”变成“浏览器里的本地 coding agent”。
