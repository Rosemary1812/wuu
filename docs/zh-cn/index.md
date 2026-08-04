# wuu 文档

wuu 是一个从软件开发开始的本地工作区 AI Agent。它直接在你选择的文件夹中工作，
可以阅读和修改文件、运行命令、检查结果，并在后续会话中继续任务。

wuu 的桌面应用适合交互式工作；`wuu exec` 适合终端、脚本、CI 和其他 Agent。
两种入口使用同一个 Go 核心，但桌面应用自带私有 core，不依赖单独安装的 CLI。

## 第一次使用

沿着[快速开始](getting-started/index.md)完成一条真实路径：

1. 安装桌面应用；
2. 连接模型服务；
3. 添加一个本地工作区；
4. 交代一个范围明确的小任务；
5. 检查文件、diff 和验证结果。

桌面应用首次启动后会直接进入共享的“对话”区域，不会先经过账号注册或强制的新手
向导。要让 Agent 读写项目文件或运行项目命令，请添加真实工作区。

## 按你要做的事阅读

- **管理项目和会话：**[工作区与项目](desktop/workspaces.md)、
  [会话与分支](desktop/conversations.md)。
- **检查 Agent 的成果：**[文件、改动、终端与浏览器](desktop/workspace-tools.md)。
- **理解复杂任务怎样推进：**[Agent 协作与子代理](desktop/subagents.md)和
  [命令与后台任务](reference/agent-command-system.md)。
- **复用工作方式：**[Skills](customize/skills.md) 和[记忆](customize/memory.md)。
- **按计划运行：**[Automations](automation/scheduled-tasks.md)。
- **接入外部工具：**[MCP 服务器](customize/mcp.md)。
- **控制本地权限：**[权限模式](reference/permissions.md)和[安全模型](reference/security-model.md)。
- **接入脚本和 CI：**[用 `wuu exec` 做自动化](automation/exec.md)和
  [JSONL 事件](../en/automation/jsonl-events.md)（英文）。
- **遇到问题：**从[故障排查](help/troubleshooting.md)开始。

## 工作区是成果的来源

wuu 的对话用于表达目标、补充信息和检查进度，真正的成果保存在工作区文件中。这些
文件可以是代码、Markdown 文档、图片或其他任务材料。你可以继续用熟悉的编辑器、
Git 和文件工具处理它们，而不必把内容锁在一次对话里。

wuu 目前主要服务软件开发工作流，包括阅读和修改代码、运行命令、检查改动以及跨会话
继续任务。

## 产品入口

### 桌面应用

桌面端提供工作区选择、多会话、附件、改动查看和设置界面，适合日常交互式工作。

### CLI 和自动化

`wuu exec` 提供自动化安全的文本和 JSONL 输出，可用于脚本、CI、评审任务或由其他
Agent 调用。

### App-server 集成

新的桌面端、编辑器插件或其他 Shell 可以把 wuu core 作为子进程启动，并通过
基于标准输入输出的逐行 JSON 协议复用会话、工具和模型能力。当前 wire protocol 仍是 `v0.1`，适合受控
集成，不应假设所有字段已经长期稳定。

---

[English documentation](../en/index.md)
