# wuu 文档

wuu 是一个从软件开发开始的本地工作区 AI Agent。它直接在你选择的文件夹中工作，
可以阅读和修改文件、运行命令、检查结果，并在后续会话中继续任务。

wuu 的桌面应用适合交互式工作；`wuu exec` 适合终端、脚本、CI 和其他 Agent。
两种入口使用同一个 Go 核心，但桌面应用自带私有 core，不依赖单独安装的 CLI。

## 从这里开始

- **第一次使用：**按照[用户指南](getting-started/index.md)安装 wuu、连接模型提供商，
  并在本地工作区中完成第一个任务。
- **理解配置：**阅读[配置模型](reference/configuration.md)，了解用户配置、项目规则、
  凭据和权限之间的边界。
- **接入自动化：**阅读 [`wuu exec`](../en/automation/exec.md) 和
  [JSONL 事件](../en/automation/jsonl-events.md)（英文）。
- **构建其他客户端：**阅读 [`app-server` 协议](../en/integrations/app-server-protocol.md)
  （英文）。
- **处理敏感内容：**先阅读[安全模型](../en/reference/security-model.md)（英文）。

## 工作区是成果的来源

wuu 的对话用于表达目标、补充信息和检查进度，真正的成果保存在工作区文件中。这些
文件可以是代码、Markdown 文档、图片或其他任务材料。你可以继续用熟悉的编辑器、
Git 和文件工具处理它们，而不必把内容锁在一次对话里。

当前公开版本首先服务软件开发工作流。文档只描述已经可以使用的行为；仍在设计中的
写作、知识管理和发布能力不会作为现有功能提前写入指南。

## 当前入口

### 桌面应用

桌面端提供工作区选择、多会话、附件、改动查看和设置界面，适合日常交互式工作。

### CLI 和自动化

`wuu exec` 提供自动化安全的文本和 JSONL 输出，可用于脚本、CI、评审任务或由其他
Agent 调用。

### App-server

新的桌面端、编辑器插件或其他 Shell 可以把 wuu core 作为子进程启动，并通过
JSON-RPC 协议复用会话、工具和模型能力。

---

[English documentation](../en/index.md)
