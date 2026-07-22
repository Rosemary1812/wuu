<h1 align="center">wuu</h1>

<p align="center">面向本地开发的开源 AI Coding Agent。</p>

<div align="center">
  <p>
    <a href="README.md">English</a> |
    <a href="README_zh.md">简体中文</a>
  </p>
  <p>
    <a href="https://github.com/blueberrycongee/wuu/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/blueberrycongee/wuu/ci.yml?branch=main&style=flat-square&label=ci"></a>
    <a href="https://github.com/blueberrycongee/wuu/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/github/license/blueberrycongee/wuu?style=flat-square"></a>
    <a href="https://github.com/blueberrycongee/wuu/releases"><img alt="GitHub release downloads" src="https://img.shields.io/github/downloads/blueberrycongee/wuu/total?style=flat-square&label=downloads"></a>
  </p>
</div>

---

<img width="2272" height="2494" alt="wuu 桌面应用" src="https://github.com/user-attachments/assets/2d9030aa-ca03-42b1-9333-f79cc5aff95b" />

**wuu** 直接在本地代码仓库中工作。它可以读取和修改文件、运行命令、审查改动，并通过你配置的模型提供商把开发任务推进到完成。

桌面应用适合日常交互。面对更大的任务，wuu 可以制定计划、把工作委派给子智能体、使用任务专属技能，并通过持久会话继续之前的工作。`wuu exec` 则为脚本、CI 和其他 agent 提供非交互入口。

> [!WARNING]
> wuu 仍处于早期预览阶段，正在快速迭代。打包好的桌面版目前支持 Apple 芯片 Mac。

## 下载

从 [GitHub Releases](https://github.com/blueberrycongee/wuu/releases/latest) 下载最新的 macOS 桌面版，把 `wuu.app` 移到 `/Applications` 后打开。

当前预览版尚未签名和公证。如果 macOS 阻止打开，并且你确认安装包来自官方 Release，可以运行：

```bash
xattr -dr com.apple.quarantine /Applications/wuu.app && open /Applications/wuu.app
```

## 开始使用

1. 打开 `wuu.app`，选择一个本地项目文件夹。
2. 打开 **设置 → 模型服务**，连接 Anthropic 或 OpenAI 兼容提供商。
3. 开始一个对话，直接描述你想要的结果。

例如：

```text
解释一下这个仓库的结构。
修复失败的测试并验证结果。
检查当前改动是否引入了回归。
```

模型配置、附件、会话、权限和常见问题见[用户指南](docs/zh-cn/getting-started/index.md)。

## 主要能力

- **直接处理代码仓库** —— 读取和修改文件、搜索代码、运行命令，并检查最终差异。
- **完成更长的任务** —— 规划多步工作、跟踪持久目标，并跨越上下文限制继续执行。
- **委派工作** —— 使用子智能体完成专项研究、并行任务或隔离实现。
- **保留有用的上下文** —— 恢复会话、从检查点 fork，并使用持久记忆和技能。
- **自选模型** —— 接入 Anthropic 或任何 OpenAI 兼容端点，包括本地网关。
- **使用文件和图片** —— 在任务需要时添加文档和截图。
- **自动化工作流** —— 将结构化输出流式传给脚本、CI、review 工具和其他 agent。

## 自动化

`wuu exec` 以非交互命令的形式提供智能体运行能力：

```bash
wuu exec --json "审查当前 diff"
wuu exec --file plan.md "实现这个计划"
wuu exec review --uncommitted
```

环境准备、JSONL 事件、附件、会话控制和 review 选项见 [`wuu exec`](docs/en/automation/exec.md)。

## 模型与数据

- 模型提供商和凭据由你选择；提示词和相关上下文会发送给该提供商。
- 提供商设置和 API Key 由用户控制，不会从代码仓库中读取。
- 会话、配置、日志和其他本地状态默认保存在 `~/.wuu`。
- 文件改动和命令在选定的本地工作区内执行，并受当前权限模式控制。

在处理不受信任的仓库或敏感数据前，请阅读[安全模型](docs/en/reference/security-model.md)。

## 文档

- [用户指南](docs/zh-cn/getting-started/index.md)
- [`wuu exec` 自动化指南](docs/en/automation/exec.md)
- [安全模型](docs/en/reference/security-model.md)
- [路线图](ROADMAP_zh.md)
- [公开评测](evals/)
- [更新记录](CHANGELOG.md)
- [文档索引](docs/README.md)

## 参与贡献

欢迎参与贡献。开发和 review 规范见 [CONTRIBUTING.md](CONTRIBUTING.md)，安全漏洞报告方式见 [SECURITY.md](SECURITY.md)。

遇到问题可以[提交 issue](https://github.com/blueberrycongee/wuu/issues)。

## 致谢

wuu 的设计受益于以下项目在 coding agent、工具循环、任务编排和开发者体验方面的探索：

- [Codex](https://github.com/openai/codex)
- [OpenCode](https://github.com/anomalyco/opencode)
- [pi](https://github.com/badlogic/pi-mono)
- [Kimi Code](https://github.com/MoonshotAI/kimi-cli)

## 许可证

[MIT](LICENSE)
